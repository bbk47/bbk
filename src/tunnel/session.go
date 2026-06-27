package tunnel

import (
	"encoding/binary"
	"errors"
	"io"
	"time"

	"github.com/hashicorp/yamux"
)

const (
	// maxAddrLen 限制流级握手中目标地址的长度（socks5 地址最大约 262 字节）。
	maxAddrLen = 1024
	// handshakeTimeout 是流级握手（addr/status 交换）的读超时，
	// 防止 accept 侧因对端打开流后不发 addr 而被无限阻塞。
	handshakeTimeout = 15 * time.Second
	// openWaitTimeout 是客户端 OpenStream 后等待对端就绪状态(EST)的超时。
	openWaitTimeout = 15 * time.Second
	// muxWindowSize 与旧 toolbox/mux 的默认窗口保持一致（256KB）。
	muxWindowSize = 256 * 1024
)

// ErrStreamRefused 表示对端拒绝/未能建立该流的目标连接。
var ErrStreamRefused = errors.New("tunnel: remote refused stream")

// Session 封装 yamux.Session，并补上 yamux 不负责的"流级握手"
// （目标地址 + 就绪确认），取代旧的 stub.TunnelStub。
type Session struct {
	sess *yamux.Session
}

func newYamuxConfig() *yamux.Config {
	cfg := yamux.DefaultConfig()
	cfg.EnableKeepAlive = true
	cfg.KeepAliveInterval = 15 * time.Second
	cfg.MaxStreamWindowSize = muxWindowSize
	cfg.LogOutput = io.Discard // 静默 yamux 内部日志，避免污染 stderr
	return cfg
}

// Client 在客户端侧基于一条字节流建立复用会话。
func Client(conn io.ReadWriteCloser) (*Session, error) {
	s, err := yamux.Client(conn, newYamuxConfig())
	if err != nil {
		return nil, err
	}
	return &Session{sess: s}, nil
}

// Server 在服务端侧基于一条字节流建立复用会话。
func Server(conn io.ReadWriteCloser) (*Session, error) {
	s, err := yamux.Server(conn, newYamuxConfig())
	if err != nil {
		return nil, err
	}
	return &Session{sess: s}, nil
}

// OpenStream 打开一条新流并完成握手：写出目标地址，等待对端回送就绪状态。
// 返回时该流已"连接建立"，可直接开始转发（等价于旧的 INIT→EST 流程）。
func (s *Session) OpenStream(addr []byte) (*Stream, error) {
	raw, err := s.sess.OpenStream()
	if err != nil {
		return nil, err
	}
	if err := writeAddr(raw, addr); err != nil {
		raw.Close()
		return nil, err
	}

	_ = raw.SetReadDeadline(time.Now().Add(openWaitTimeout))
	var sb [1]byte
	if _, err := io.ReadFull(raw, sb[:]); err != nil {
		raw.Close()
		return nil, err
	}
	_ = raw.SetReadDeadline(time.Time{})

	if sb[0] != statusOK {
		raw.Close()
		return nil, ErrStreamRefused
	}
	return &Stream{Stream: raw, Addr: append([]byte(nil), addr...)}, nil
}

// AcceptStream 接受一条新流并读取其目标地址头。
// 仅在会话本身关闭时返回错误；单条流的握手失败会被跳过（关闭该流后继续接受），
// 不影响整个会话继续服务。
func (s *Session) AcceptStream() (*Stream, error) {
	for {
		raw, err := s.sess.AcceptStream()
		if err != nil {
			return nil, err // 会话关闭
		}
		_ = raw.SetReadDeadline(time.Now().Add(handshakeTimeout))
		addr, err := readAddr(raw)
		if err != nil {
			raw.Close()
			continue // 握手非法/超时：丢弃该流，继续服务
		}
		_ = raw.SetReadDeadline(time.Time{})
		return &Stream{Stream: raw, Addr: addr}, nil
	}
}

// Close 关闭整个会话。
func (s *Session) Close() error {
	return s.sess.Close()
}

// IsClosed 报告会话是否已关闭（供 client 重连判定）。
func (s *Session) IsClosed() bool {
	return s.sess.IsClosed()
}

// writeAddr 以 [2字节大端长度][addr] 写出目标地址。
func writeAddr(w io.Writer, addr []byte) error {
	if len(addr) > maxAddrLen {
		return errors.New("tunnel: addr too long")
	}
	buf := make([]byte, 2+len(addr))
	binary.BigEndian.PutUint16(buf, uint16(len(addr)))
	copy(buf[2:], addr)
	_, err := w.Write(buf)
	return err
}

// readAddr 读取以长度前缀界定的目标地址。
func readAddr(r io.Reader) ([]byte, error) {
	var lb [2]byte
	if _, err := io.ReadFull(r, lb[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint16(lb[:])
	if int(n) > maxAddrLen {
		return nil, errors.New("tunnel: addr too long")
	}
	addr := make([]byte, n)
	if _, err := io.ReadFull(r, addr); err != nil {
		return nil, err
	}
	return addr, nil
}
