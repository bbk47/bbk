package proxy

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/bbk47/toolbox"
)

// UDP 代理整体设计（shadowsocks 风格：不支持分片）：
//
//	app(UDP) --SOCKS5 UDP datagram--> Client relay socket
//	   --(length-prefixed: socks5addr+payload over a mux stream)--> Server
//	   Server 按 socks5addr 维护到各目标的 net.UDPConn(NAT 会话表)并收发
//
// 关键约束：
//   - 不支持分片：SOCKS5 UDP 头里的 FRAG 字段必须为 0，否则丢弃该数据报。
//   - 复用现有 mux stream（字节流），因此每个 UDP 数据报用 2 字节大端长度前缀
//     界定边界：[len(2)][ socks5addr + payload ]。
//   - 不新增任何帧类型/协议字段，server 透明转发。

const (
	// maxUDPDatagram 是单条隧道记录(socks5addr+payload)的上限。
	// 受 2 字节长度前缀限制，最大 65535；超过即丢弃（不分片）。
	maxUDPDatagram = 64 * 1024
	// udpIdleTimeout 是 server 侧到目标的 UDP 会话空闲回收时间，避免 FD 泄漏。
	udpIdleTimeout = 60 * time.Second
)

// udpMarker 是 UDP ASSOCIATE 关联流的哨兵目标地址。
// 普通 TCP 流的 socks5 地址首字节恒为 0x01/0x03/0x04，0xFD 不会与之冲突。
var udpMarker = []byte{0xFD, 'U', 'D', 'P'}

// UDPMarkerAddr 返回 UDP 关联流在 INIT 帧里携带的哨兵地址。
func UDPMarkerAddr() []byte { return append([]byte(nil), udpMarker...) }

// IsUDPMarker 判断一个流地址是否为 UDP 关联哨兵。
func IsUDPMarker(addr []byte) bool { return bytes.Equal(addr, udpMarker) }

// Socks5AddrLen 返回一段 buffer 开头的 socks5 地址(ATYP+ADDR+PORT)所占字节数，
// 用于把 "地址+负载" 的记录切分为地址与负载两部分。
func Socks5AddrLen(b []byte) (int, error) {
	if len(b) < 1 {
		return 0, errors.New("socks5 addr empty")
	}
	switch b[0] {
	case 0x01: // IPv4: atyp(1)+ip(4)+port(2)
		if len(b) < 7 {
			return 0, errors.New("socks5 addr ipv4 too short")
		}
		return 7, nil
	case 0x03: // domain: atyp(1)+len(1)+domain(n)+port(2)
		if len(b) < 2 {
			return 0, errors.New("socks5 addr domain too short")
		}
		n := 1 + 1 + int(b[1]) + 2
		if len(b) < n {
			return 0, errors.New("socks5 addr domain truncated")
		}
		return n, nil
	case 0x04: // IPv6: atyp(1)+ip(16)+port(2)
		if len(b) < 19 {
			return 0, errors.New("socks5 addr ipv6 too short")
		}
		return 19, nil
	default:
		return 0, fmt.Errorf("socks5 addr invalid atyp:0x%x", b[0])
	}
}

// writeDatagram 把一条 UDP 记录以 [2字节大端长度][数据] 写入字节流。
func writeDatagram(w io.Writer, b []byte) error {
	if len(b) > maxUDPDatagram {
		return fmt.Errorf("udp datagram too large: %d > %d", len(b), maxUDPDatagram)
	}
	buf := make([]byte, 2+len(b))
	binary.BigEndian.PutUint16(buf, uint16(len(b)))
	copy(buf[2:], b)
	_, err := w.Write(buf)
	return err
}

// readDatagram 从字节流读出一条以长度前缀界定的 UDP 记录。
func readDatagram(r io.Reader) ([]byte, error) {
	var lb [2]byte
	if _, err := io.ReadFull(r, lb[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint16(lb[:])
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// ServeUDP 运行 server 侧的 UDP 中继：从 stream 读取 "socks5addr+payload" 记录，
// 按目标维护 net.UDPConn 会话表并转发；目标回包再写回 stream。
// stream 关闭或读到错误即结束，并回收所有目标连接。
func ServeUDP(stream io.ReadWriteCloser, logger *toolbox.Logger) {
	defer stream.Close()

	var wmu sync.Mutex // 串行化对 stream 的写，保证一条数据报的字节不被并发写打散
	var mu sync.Mutex
	conns := make(map[string]*net.UDPConn)

	defer func() {
		mu.Lock()
		for k, c := range conns {
			_ = c.Close()
			delete(conns, k)
		}
		mu.Unlock()
	}()

	for {
		rec, err := readDatagram(stream)
		if err != nil {
			return
		}
		alen, err := Socks5AddrLen(rec)
		if err != nil {
			continue // 地址非法，丢弃
		}
		addrBytes := rec[:alen]
		payload := rec[alen:]
		info, err := toolbox.ParseAddrInfo(addrBytes)
		if err != nil {
			continue // 无法解析(如 IPv6 未支持)，丢弃
		}
		target := fmt.Sprintf("%s:%d", info.Addr, info.Port)

		mu.Lock()
		conn := conns[target]
		if conn == nil {
			raddr, err := net.ResolveUDPAddr("udp", target)
			if err != nil {
				mu.Unlock()
				continue
			}
			c, err := net.DialUDP("udp", nil, raddr)
			if err != nil {
				mu.Unlock()
				if logger != nil {
					logger.Debugf("udp dial %s err:%s\n", target, err.Error())
				}
				continue
			}
			conn = c
			conns[target] = c
			// 每个目标一个回包读循环：把回包加上目标 socks5 地址写回 stream。
			ab := append([]byte(nil), addrBytes...)
			go func(target string, ab []byte, c *net.UDPConn) {
				defer func() {
					mu.Lock()
					if conns[target] == c {
						delete(conns, target)
					}
					mu.Unlock()
					_ = c.Close()
				}()
				buf := make([]byte, maxUDPDatagram)
				for {
					_ = c.SetReadDeadline(time.Now().Add(udpIdleTimeout))
					n, err := c.Read(buf)
					if err != nil {
						return // 超时(空闲回收)或连接关闭
					}
					out := make([]byte, 0, len(ab)+n)
					out = append(out, ab...)
					out = append(out, buf[:n]...)
					wmu.Lock()
					werr := writeDatagram(stream, out)
					wmu.Unlock()
					if werr != nil {
						return
					}
				}
			}(target, ab, c)
		}
		mu.Unlock()

		_ = conn.SetReadDeadline(time.Now().Add(udpIdleTimeout)) // 有活动则续期
		if _, err := conn.Write(payload); err != nil && logger != nil {
			logger.Debugf("udp write %s err:%s\n", target, err.Error())
		}
	}
}

// ClientUDP 运行 client 侧的 UDP 中继：把 app 经 relay socket 发来的 SOCKS5 UDP
// 数据报(剥掉 RSV+FRAG，校验不分片)按长度前缀写入 stream；stream 回来的记录再
// 补上 RSV+FRAG 头回送给 app。relay socket 或 stream 任一关闭即结束。
func ClientUDP(udpConn *net.UDPConn, stream io.ReadWriteCloser, logger *toolbox.Logger) {
	defer udpConn.Close()
	defer stream.Close()

	var camu sync.Mutex
	var clientAddr *net.UDPAddr // app 的来源地址，回包发回这里

	go func() {
		buf := make([]byte, maxUDPDatagram)
		for {
			n, src, err := udpConn.ReadFromUDP(buf)
			if err != nil {
				_ = stream.Close()
				return
			}
			if n < 4 { // 至少 RSV(2)+FRAG(1)+ATYP(1)
				continue
			}
			if buf[2] != 0x00 { // FRAG != 0：不支持分片，丢弃
				if logger != nil {
					logger.Debugf("udp drop fragmented datagram frag=%d\n", buf[2])
				}
				continue
			}
			rec := buf[3:n] // ATYP+ADDR+PORT+DATA
			camu.Lock()
			clientAddr = src
			camu.Unlock()
			cp := append([]byte(nil), rec...)
			if err := writeDatagram(stream, cp); err != nil {
				return
			}
		}
	}()

	for {
		rec, err := readDatagram(stream)
		if err != nil {
			return
		}
		camu.Lock()
		ca := clientAddr
		camu.Unlock()
		if ca == nil {
			continue
		}
		out := make([]byte, 0, 3+len(rec))
		out = append(out, 0x00, 0x00, 0x00) // RSV(2)=0, FRAG=0
		out = append(out, rec...)
		_, _ = udpConn.WriteToUDP(out, ca)
	}
}
