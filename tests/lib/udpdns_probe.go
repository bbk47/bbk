//go:build ignore

// udpdns_probe —— 经 SOCKS5 UDP ASSOCIATE 发送一次 DNS 查询，校验 udprelay 通路。
// 用法: go run tests/lib/udpdns_probe.go -socks 127.0.0.1:1090 -dns 1.1.1.1:53 -name example.com
// 退出码 0 表示收到合法 DNS 应答(ancount>0)，非 0 表示失败。
package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

func main() {
	socks := flag.String("socks", "127.0.0.1:1090", "socks5 address")
	dns := flag.String("dns", "1.1.1.1:53", "upstream dns server ip:port")
	name := flag.String("name", "example.com", "domain to query (A)")
	flag.Parse()

	if err := run(*socks, *dns, *name); err != nil {
		fmt.Fprintf(os.Stderr, "udpdns_probe FAIL: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("udpdns_probe OK")
}

func run(socks, dns, name string) error {
	// 1) SOCKS5 控制连接 + 握手
	ctrl, err := net.DialTimeout("tcp", socks, 5*time.Second)
	if err != nil {
		return fmt.Errorf("dial socks: %w", err)
	}
	defer ctrl.Close()
	_ = ctrl.SetDeadline(time.Now().Add(10 * time.Second))

	if _, err := ctrl.Write([]byte{0x05, 0x01, 0x00}); err != nil { // VER, NMETHODS=1, NOAUTH
		return fmt.Errorf("write greeting: %w", err)
	}
	rep := make([]byte, 2)
	if _, err := io.ReadFull(ctrl, rep); err != nil {
		return fmt.Errorf("read method: %w", err)
	}
	if rep[0] != 0x05 || rep[1] != 0x00 {
		return fmt.Errorf("bad method reply: %v", rep)
	}

	// 2) UDP ASSOCIATE 请求（DST 用 0.0.0.0:0）
	req := []byte{0x05, 0x03, 0x00, 0x01, 0, 0, 0, 0, 0, 0}
	if _, err := ctrl.Write(req); err != nil {
		return fmt.Errorf("write associate: %w", err)
	}
	bndHost, bndPort, err := readSocks5Reply(ctrl)
	if err != nil {
		return fmt.Errorf("associate reply: %w", err)
	}
	if bndHost == "0.0.0.0" || bndHost == "::" {
		bndHost, _, _ = net.SplitHostPort(socks)
	}
	relay := net.JoinHostPort(bndHost, strconv.Itoa(bndPort))

	// 3) 打开 UDP 收发 socket，连接 relay
	raddr, err := net.ResolveUDPAddr("udp", relay)
	if err != nil {
		return fmt.Errorf("resolve relay: %w", err)
	}
	uc, err := net.DialUDP("udp", nil, raddr)
	if err != nil {
		return fmt.Errorf("dial relay udp: %w", err)
	}
	defer uc.Close()
	_ = uc.SetDeadline(time.Now().Add(8 * time.Second))

	// 4) 构造 SOCKS5 UDP 数据报: RSV(2) FRAG(1) ATYP+DST + DNS payload
	dnsIP, dnsPort, err := splitIPPort(dns)
	if err != nil {
		return err
	}
	queryID := uint16(0x1234)
	payload := buildDNSQuery(queryID, name)

	var dg bytes.Buffer
	dg.Write([]byte{0x00, 0x00, 0x00}) // RSV, FRAG=0
	dg.WriteByte(0x01)                 // ATYP IPv4
	dg.Write(dnsIP.To4())
	_ = binary.Write(&dg, binary.BigEndian, dnsPort)
	dg.Write(payload)

	if _, err := uc.Write(dg.Bytes()); err != nil {
		return fmt.Errorf("send udp: %w", err)
	}

	// 5) 读取应答并剥头
	buf := make([]byte, 4096)
	n, err := uc.Read(buf)
	if err != nil {
		return fmt.Errorf("read udp (relay 不通?): %w", err)
	}
	resp := buf[:n]
	hdr, err := socks5UDPHeaderLen(resp)
	if err != nil {
		return err
	}
	dnsResp := resp[hdr:]
	ancount, rid, err := parseDNSAnswerCount(dnsResp)
	if err != nil {
		return err
	}
	if rid != queryID {
		return fmt.Errorf("dns id mismatch: got %#x want %#x", rid, queryID)
	}
	if ancount == 0 {
		return errors.New("dns ancount=0 (无解析结果)")
	}
	fmt.Printf("dns answer count=%d for %s via %s\n", ancount, name, dns)
	return nil
}

func readSocks5Reply(r io.Reader) (string, int, error) {
	head := make([]byte, 4)
	if _, err := io.ReadFull(r, head); err != nil {
		return "", 0, err
	}
	if head[0] != 0x05 || head[1] != 0x00 {
		return "", 0, fmt.Errorf("reply err ver=%d rep=%d", head[0], head[1])
	}
	var host string
	switch head[3] {
	case 0x01:
		b := make([]byte, 4)
		if _, err := io.ReadFull(r, b); err != nil {
			return "", 0, err
		}
		host = net.IP(b).String()
	case 0x03:
		l := make([]byte, 1)
		if _, err := io.ReadFull(r, l); err != nil {
			return "", 0, err
		}
		b := make([]byte, int(l[0]))
		if _, err := io.ReadFull(r, b); err != nil {
			return "", 0, err
		}
		host = string(b)
	case 0x04:
		b := make([]byte, 16)
		if _, err := io.ReadFull(r, b); err != nil {
			return "", 0, err
		}
		host = net.IP(b).String()
	default:
		return "", 0, fmt.Errorf("unknown atyp %d", head[3])
	}
	pb := make([]byte, 2)
	if _, err := io.ReadFull(r, pb); err != nil {
		return "", 0, err
	}
	return host, int(binary.BigEndian.Uint16(pb)), nil
}

func socks5UDPHeaderLen(b []byte) (int, error) {
	if len(b) < 4 {
		return 0, errors.New("udp resp too short")
	}
	switch b[3] {
	case 0x01:
		return 4 + 4 + 2, nil
	case 0x03:
		if len(b) < 5 {
			return 0, errors.New("udp resp domain short")
		}
		return 4 + 1 + int(b[4]) + 2, nil
	case 0x04:
		return 4 + 16 + 2, nil
	default:
		return 0, fmt.Errorf("udp resp bad atyp %d", b[3])
	}
}

func splitIPPort(s string) (net.IP, uint16, error) {
	h, p, err := net.SplitHostPort(s)
	if err != nil {
		return nil, 0, err
	}
	ip := net.ParseIP(h)
	if ip == nil || ip.To4() == nil {
		return nil, 0, fmt.Errorf("dns must be IPv4: %s", h)
	}
	port, err := strconv.Atoi(p)
	if err != nil {
		return nil, 0, err
	}
	return ip, uint16(port), nil
}

func buildDNSQuery(id uint16, name string) []byte {
	var b bytes.Buffer
	_ = binary.Write(&b, binary.BigEndian, id)
	_ = binary.Write(&b, binary.BigEndian, uint16(0x0100)) // RD
	_ = binary.Write(&b, binary.BigEndian, uint16(1))      // QDCOUNT
	_ = binary.Write(&b, binary.BigEndian, uint16(0))      // ANCOUNT
	_ = binary.Write(&b, binary.BigEndian, uint16(0))      // NSCOUNT
	_ = binary.Write(&b, binary.BigEndian, uint16(0))      // ARCOUNT
	for _, label := range strings.Split(name, ".") {
		b.WriteByte(byte(len(label)))
		b.WriteString(label)
	}
	b.WriteByte(0)
	_ = binary.Write(&b, binary.BigEndian, uint16(1)) // QTYPE A
	_ = binary.Write(&b, binary.BigEndian, uint16(1)) // QCLASS IN
	return b.Bytes()
}

func parseDNSAnswerCount(b []byte) (uint16, uint16, error) {
	if len(b) < 12 {
		return 0, 0, errors.New("dns resp too short")
	}
	id := binary.BigEndian.Uint16(b[0:2])
	ancount := binary.BigEndian.Uint16(b[6:8])
	return ancount, id, nil
}
