package proxy

import (
	"bytes"
	"io"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/bbk47/toolbox"
)

func TestSocks5AddrLen(t *testing.T) {
	ipv4 := toolbox.BuildSocks5AddrData("127.0.0.1", "53") // atyp(1)+ip(4)+port(2)=7
	if n, err := Socks5AddrLen(ipv4); err != nil || n != 7 {
		t.Fatalf("ipv4 len: got %d,%v want 7,nil", n, err)
	}

	domain := toolbox.BuildSocks5AddrData("example.com", "80") // 1+1+11+2=15
	if n, err := Socks5AddrLen(domain); err != nil || n != 15 {
		t.Fatalf("domain len: got %d,%v want 15,nil", n, err)
	}

	ipv6 := append([]byte{0x04}, make([]byte, 18)...) // atyp(1)+ip(16)+port(2)=19
	if n, err := Socks5AddrLen(ipv6); err != nil || n != 19 {
		t.Fatalf("ipv6 len: got %d,%v want 19,nil", n, err)
	}

	if _, err := Socks5AddrLen([]byte{0x09, 0x01}); err == nil {
		t.Fatal("invalid atyp should error")
	}
	if _, err := Socks5AddrLen(nil); err == nil {
		t.Fatal("empty addr should error")
	}
	// 域名长度声明超过实际字节，应判定截断
	if _, err := Socks5AddrLen([]byte{0x03, 0x05, 'a'}); err == nil {
		t.Fatal("truncated domain should error")
	}
}

func TestDatagramRoundTrip(t *testing.T) {
	var b bytes.Buffer
	want := []byte("hello-udp-record")
	if err := writeDatagram(&b, want); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := readDatagram(&b)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("round trip mismatch: got %q want %q", got, want)
	}

	// 超过上限应报错（不分片）
	if err := writeDatagram(io.Discard, make([]byte, maxUDPDatagram+1)); err == nil {
		t.Fatal("oversize datagram should error")
	}
}

func TestUDPMarker(t *testing.T) {
	if !IsUDPMarker(UDPMarkerAddr()) {
		t.Fatal("marker should be recognized")
	}
	// 普通 socks5 地址不应被误判为 UDP 哨兵
	if IsUDPMarker(toolbox.BuildSocks5AddrData("127.0.0.1", "80")) {
		t.Fatal("tcp addr misdetected as udp marker")
	}
}

// newUDPEcho 启动一个回显 UDP 服务，返回其地址与关闭函数。
func newUDPEcho(t *testing.T) (*net.UDPAddr, func()) {
	t.Helper()
	c, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("listen echo: %v", err)
	}
	go func() {
		buf := make([]byte, 64*1024)
		for {
			n, src, err := c.ReadFromUDP(buf)
			if err != nil {
				return
			}
			_, _ = c.WriteToUDP(buf[:n], src)
		}
	}()
	return c.LocalAddr().(*net.UDPAddr), func() { _ = c.Close() }
}

// TestServeUDPEcho 覆盖 server 侧完整 UDP 路径：
// 读记录 -> DialUDP -> 发往目标 -> 回包读循环 -> 写回 stream。
func TestServeUDPEcho(t *testing.T) {
	echoAddr, closeEcho := newUDPEcho(t)
	defer closeEcho()

	streamEnd, testEnd := net.Pipe()
	defer testEnd.Close()
	go ServeUDP(streamEnd, nil)

	addr := toolbox.BuildSocks5AddrData("127.0.0.1", strconv.Itoa(echoAddr.Port))
	payload := []byte("ping-over-udp")
	rec := append(append([]byte(nil), addr...), payload...)

	_ = testEnd.SetWriteDeadline(time.Now().Add(3 * time.Second))
	if err := writeDatagram(testEnd, rec); err != nil {
		t.Fatalf("write request: %v", err)
	}

	_ = testEnd.SetReadDeadline(time.Now().Add(3 * time.Second))
	got, err := readDatagram(testEnd)
	if err != nil {
		t.Fatalf("read reply: %v", err)
	}
	alen, err := Socks5AddrLen(got)
	if err != nil {
		t.Fatalf("reply addr len: %v", err)
	}
	if !bytes.Equal(got[alen:], payload) {
		t.Fatalf("echo mismatch: got %q want %q", got[alen:], payload)
	}
	if !bytes.Equal(got[:alen], addr) {
		t.Fatalf("reply addr mismatch: got %x want %x", got[:alen], addr)
	}
}

// TestServeUDPMultiTarget 验证一条关联流可发往多个目标（NAT 会话表）。
func TestServeUDPMultiTarget(t *testing.T) {
	echo1, close1 := newUDPEcho(t)
	defer close1()
	echo2, close2 := newUDPEcho(t)
	defer close2()

	streamEnd, testEnd := net.Pipe()
	defer testEnd.Close()
	go ServeUDP(streamEnd, nil)

	send := func(port int, payload string) []byte {
		addr := toolbox.BuildSocks5AddrData("127.0.0.1", strconv.Itoa(port))
		rec := append(append([]byte(nil), addr...), []byte(payload)...)
		_ = testEnd.SetWriteDeadline(time.Now().Add(3 * time.Second))
		if err := writeDatagram(testEnd, rec); err != nil {
			t.Fatalf("write: %v", err)
		}
		_ = testEnd.SetReadDeadline(time.Now().Add(3 * time.Second))
		got, err := readDatagram(testEnd)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		alen, _ := Socks5AddrLen(got)
		return got[alen:]
	}

	if g := send(echo1.Port, "to-one"); !bytes.Equal(g, []byte("to-one")) {
		t.Fatalf("target1 mismatch: %q", g)
	}
	if g := send(echo2.Port, "to-two"); !bytes.Equal(g, []byte("to-two")) {
		t.Fatalf("target2 mismatch: %q", g)
	}
}

// TestClientUDP 覆盖 client 侧：app 的 SOCKS5 UDP 数据报 <-> 隧道记录互转，
// 以及不支持分片(FRAG!=0)时丢弃。
func TestClientUDP(t *testing.T) {
	relay, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("listen relay: %v", err)
	}
	relayAddr := relay.LocalAddr().(*net.UDPAddr)

	streamEnd, testEnd := net.Pipe()
	defer testEnd.Close()
	go ClientUDP(relay, streamEnd, nil)

	app, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("listen app: %v", err)
	}
	defer app.Close()

	target := toolbox.BuildSocks5AddrData("1.2.3.4", "53")

	// 1) FRAG!=0 的数据报应被丢弃：隧道侧读不到任何东西
	frag := append([]byte{0x00, 0x00, 0x01}, target...)
	frag = append(frag, []byte("frag")...)
	if _, err := app.WriteToUDP(frag, relayAddr); err != nil {
		t.Fatalf("app write frag: %v", err)
	}

	// 2) 正常数据报：app -> relay -> 隧道记录(=target+data)
	dgram := append([]byte{0x00, 0x00, 0x00}, target...)
	dgram = append(dgram, []byte("query")...)
	if _, err := app.WriteToUDP(dgram, relayAddr); err != nil {
		t.Fatalf("app write: %v", err)
	}

	_ = testEnd.SetReadDeadline(time.Now().Add(3 * time.Second))
	rec, err := readDatagram(testEnd)
	if err != nil {
		t.Fatalf("read tunnel record: %v", err)
	}
	wantRec := append(append([]byte(nil), target...), []byte("query")...)
	if !bytes.Equal(rec, wantRec) {
		t.Fatalf("tunnel record mismatch: got %x want %x (frag datagram may have leaked)", rec, wantRec)
	}

	// 3) 隧道回包(=target+data) -> relay 加 RSV+FRAG 头 -> app
	replyRec := append(append([]byte(nil), target...), []byte("answer")...)
	_ = testEnd.SetWriteDeadline(time.Now().Add(3 * time.Second))
	if err := writeDatagram(testEnd, replyRec); err != nil {
		t.Fatalf("write reply: %v", err)
	}

	buf := make([]byte, 1024)
	_ = app.SetReadDeadline(time.Now().Add(3 * time.Second))
	n, _, err := app.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("app read reply: %v", err)
	}
	if n < 3 || buf[0] != 0 || buf[1] != 0 || buf[2] != 0 {
		t.Fatalf("reply missing RSV+FRAG header: %x", buf[:n])
	}
	if !bytes.Equal(buf[3:n], replyRec) {
		t.Fatalf("reply body mismatch: got %x want %x", buf[3:n], replyRec)
	}
}
