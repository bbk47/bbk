package tunnel

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

// sessionPair 在 net.Pipe 上建立 client/server 两个复用会话。
func sessionPair(t *testing.T) (client, server *Session) {
	t.Helper()
	c, s := net.Pipe()
	type res struct {
		sess *Session
		err  error
	}
	cch := make(chan res, 1)
	sch := make(chan res, 1)
	go func() { sess, err := Client(c); cch <- res{sess, err} }()
	go func() { sess, err := Server(s); sch <- res{sess, err} }()

	for i := 0; i < 2; i++ {
		select {
		case r := <-cch:
			if r.err != nil {
				t.Fatalf("client session: %v", r.err)
			}
			client = r.sess
		case r := <-sch:
			if r.err != nil {
				t.Fatalf("server session: %v", r.err)
			}
			server = r.sess
		case <-time.After(3 * time.Second):
			t.Fatal("session handshake timeout")
		}
	}
	t.Cleanup(func() { client.Close(); server.Close() })
	return
}

// serve 起一个接受循环，把每条新流交给 handle 处理。
func serve(s *Session, handle func(*Stream)) {
	go func() {
		for {
			st, err := s.AcceptStream()
			if err != nil {
				return
			}
			go handle(st)
		}
	}()
}

// echoHandler 确认就绪后把流上的数据原样回写。
func echoHandler(st *Stream) {
	_ = st.SetReady()
	_, _ = io.Copy(st, st)
	_ = st.Close()
}

func TestSessionEcho(t *testing.T) {
	client, server := sessionPair(t)
	serve(server, echoHandler)

	stream, err := client.OpenStream([]byte("echo-addr"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	want := []byte("hello yamux world")
	if _, err := stream.Write(want); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := make([]byte, len(want))
	if _, err := io.ReadFull(stream, got); err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("echo mismatch: got %q want %q", got, want)
	}
}

// TestSessionAddrRoundTrip 校验目标地址（含 UDP 哨兵这类二进制）原样送达 server。
func TestSessionAddrRoundTrip(t *testing.T) {
	client, server := sessionPair(t)
	gotAddr := make(chan []byte, 1)
	serve(server, func(st *Stream) {
		_ = st.SetReady()
		gotAddr <- st.Addr
		_ = st.Close()
	})

	addrs := [][]byte{
		{0x01, 10, 0, 0, 1, 0x1f, 0x90},                  // ipv4 1.0.0.1:8080 风格
		{0xFD, 'U', 'D', 'P'},                            // UDP 关联哨兵
		append([]byte{0x03, 5}, []byte("a.com\x00P")...), // domain
	}
	for _, want := range addrs {
		st, err := client.OpenStream(want)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		select {
		case got := <-gotAddr:
			if !bytes.Equal(got, want) {
				t.Fatalf("addr mismatch: got %v want %v", got, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("server 未收到 addr")
		}
		st.Close()
	}
}

// TestSessionLargeTransfer 传输 2MB，覆盖窗口背压/分片/窗口更新端到端流转。
func TestSessionLargeTransfer(t *testing.T) {
	client, server := sessionPair(t)
	serve(server, echoHandler)

	stream, err := client.OpenStream([]byte("big"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	const size = 2 * 1024 * 1024
	payload := make([]byte, size)
	if _, err := rand.Read(payload); err != nil {
		t.Fatalf("rand: %v", err)
	}

	readDone := make(chan error, 1)
	got := make([]byte, 0, size)
	go func() {
		buf := make([]byte, 64*1024)
		for len(got) < size {
			n, err := stream.Read(buf)
			if n > 0 {
				got = append(got, buf[:n]...)
			}
			if err != nil {
				readDone <- err
				return
			}
		}
		readDone <- nil
	}()

	if _, err := stream.Write(payload); err != nil {
		t.Fatalf("write: %v", err)
	}
	select {
	case err := <-readDone:
		if err != nil && err != io.EOF {
			t.Fatalf("read: %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatalf("timeout: read %d of %d", len(got), size)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("payload mismatch: %d vs %d", len(got), size)
	}
}

// TestSessionConcurrentStreams 并发多流大块回显，验证无跨流队头阻塞/背压死锁/串扰。
func TestSessionConcurrentStreams(t *testing.T) {
	client, server := sessionPair(t)
	serve(server, echoHandler)

	const (
		streams = 8
		size    = 256 * 1024
	)
	var wg sync.WaitGroup
	errCh := make(chan error, streams)
	for i := 0; i < streams; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			stream, err := client.OpenStream([]byte("concurrent"))
			if err != nil {
				errCh <- err
				return
			}
			payload := make([]byte, size)
			for j := range payload {
				payload[j] = byte(idx*7 + j)
			}
			readDone := make(chan []byte, 1)
			go func() {
				got := make([]byte, 0, size)
				buf := make([]byte, 16*1024)
				for len(got) < size {
					n, err := stream.Read(buf)
					if n > 0 {
						got = append(got, buf[:n]...)
					}
					if err != nil {
						break
					}
				}
				readDone <- got
			}()
			if _, err := stream.Write(payload); err != nil {
				errCh <- err
				return
			}
			select {
			case got := <-readDone:
				if !bytes.Equal(got, payload) {
					errCh <- errors.New("stream payload mismatch")
				}
			case <-time.After(20 * time.Second):
				errCh <- errors.New("stream timeout")
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
}

// TestSessionHalfClose 半关闭：client CloseWrite 后 server 读到 EOF，
// 但 server 仍可继续写、client 仍能读到（写端不受读端关闭影响）。
func TestSessionHalfClose(t *testing.T) {
	client, server := sessionPair(t)
	serve(server, func(st *Stream) {
		_ = st.SetReady()
		// 读到 EOF（client 半关闭）后回送一段数据。
		_, _ = io.Copy(io.Discard, st)
		_, _ = st.Write([]byte("after-eof"))
		_ = st.Close()
	})

	stream, err := client.OpenStream([]byte("half"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := stream.Write([]byte("ping")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := stream.CloseWrite(); err != nil { // 关写端，保留读端
		t.Fatalf("close write: %v", err)
	}
	got, err := io.ReadAll(stream)
	if err != nil {
		t.Fatalf("read after half-close: %v", err)
	}
	if string(got) != "after-eof" {
		t.Fatalf("half-close 读端数据错误: %q", got)
	}
}

// TestSessionOpenRefused server 不就绪直接关流时，client 的 OpenStream 应返回错误
// （映射旧的"拨号失败→无 EST→超时"为即时错误）。
func TestSessionOpenRefused(t *testing.T) {
	client, server := sessionPair(t)
	serve(server, func(st *Stream) {
		_ = st.Close() // 不 SetReady，直接关闭
	})

	_, err := client.OpenStream([]byte("nope"))
	if err == nil {
		t.Fatal("server 拒绝时 OpenStream 应返回错误")
	}
}

// TestSessionAcceptUnblocksOnClose 会话关闭时 AcceptStream 应返回错误而非永久阻塞。
func TestSessionAcceptUnblocksOnClose(t *testing.T) {
	client, server := sessionPair(t)
	errc := make(chan error, 1)
	go func() {
		_, err := server.AcceptStream()
		errc <- err
	}()
	time.Sleep(50 * time.Millisecond)
	client.Close()
	server.Close()
	select {
	case err := <-errc:
		if err == nil {
			t.Fatal("会话关闭后 AcceptStream 应返回错误")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("会话关闭未唤醒 AcceptStream")
	}
}
