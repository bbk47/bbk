package stub

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/bbk47/bbk/v3/src/protocol"
	"github.com/bbk47/bbk/v3/src/serializer"
	"github.com/bbk47/bbk/v3/src/transport"
)

func newSerializer(t *testing.T) *serializer.Serializer {
	t.Helper()
	s, err := serializer.NewSerializer("aes-256-cfb", "password-123456")
	if err != nil {
		t.Fatalf("serializer: %v", err)
	}
	return s
}

// memTransport 是一个进程内的 transport.Transport 实现，
// 两个 memTransport 通过两条 channel 互联，用于在不走网络的情况下测试 mux。
type memTransport struct {
	rx     chan []byte
	tx     chan []byte
	closed chan struct{}
	once   sync.Once
}

var _ transport.Transport = (*memTransport)(nil)

var errMemClosed = errors.New("mem transport closed")

func (m *memTransport) SendPacket(data []byte) error {
	cp := make([]byte, len(data))
	copy(cp, data)
	select {
	case m.tx <- cp:
		return nil
	case <-m.closed:
		return errMemClosed
	}
}

func (m *memTransport) ReadPacket() ([]byte, error) {
	select {
	case b := <-m.rx:
		return b, nil
	case <-m.closed:
		return nil, errMemClosed
	}
}

func (m *memTransport) Close() error {
	m.once.Do(func() { close(m.closed) })
	return nil
}

func newMemPair() (*memTransport, *memTransport) {
	a2b := make(chan []byte, 2048)
	b2a := make(chan []byte, 2048)
	a := &memTransport{rx: b2a, tx: a2b, closed: make(chan struct{})}
	b := &memTransport{rx: a2b, tx: b2a, closed: make(chan struct{})}
	return a, b
}

func setupPair(t *testing.T) (client, server *TunnelStub, ta, tb *memTransport) {
	t.Helper()
	ta, tb = newMemPair()
	sa, err := serializer.NewSerializer("aes-256-cfb", "password-123456")
	if err != nil {
		t.Fatalf("serializer: %v", err)
	}
	sb, err := serializer.NewSerializer("aes-256-cfb", "password-123456")
	if err != nil {
		t.Fatalf("serializer: %v", err)
	}
	client = NewTunnelStub(ta, sa)
	server = NewTunnelStub(tb, sb)
	t.Cleanup(func() {
		ta.Close()
		tb.Close()
	})
	return
}

// startEchoServer 接受新流、确认 EST，并把读到的数据原样回写（echo）。
func startEchoServer(s *TunnelStub) {
	go func() {
		for {
			st, err := s.Accept()
			if err != nil {
				return
			}
			s.SetReady(st)
			go func(stream *Stream) {
				_, _ = io.Copy(stream, stream)
			}(st)
		}
	}()
}

func TestMuxEcho(t *testing.T) {
	client, server, _, _ := setupPair(t)
	startEchoServer(server)

	stream := client.StartStream([]byte("echo-addr"))
	want := []byte("hello mux world")
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

// TestMuxLargeTransfer 传输 2MB，覆盖：发送窗口背压、跨窗口分片、
// 接收侧阈值窗口更新的端到端流转。
func TestMuxLargeTransfer(t *testing.T) {
	client, server, _, _ := setupPair(t)
	startEchoServer(server)

	stream := client.StartStream([]byte("big-addr"))

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
		t.Fatalf("timeout: read %d of %d bytes", len(got), size)
	}

	if !bytes.Equal(got, payload) {
		t.Fatalf("payload mismatch: got %d bytes, want %d", len(got), size)
	}
}

// TestMuxConcurrentStreams 并发跑多条流的大块回显，验证：
// 没有跨流队头阻塞、没有背压死锁、各流数据互不串扰。
func TestMuxConcurrentStreams(t *testing.T) {
	client, server, _, _ := setupPair(t)
	startEchoServer(server)

	const (
		streams = 8
		size    = 256 * 1024
	)
	var startMu sync.Mutex // 串行化 StartStream，规避与本测试无关的 seq 自增竞争
	var wg sync.WaitGroup
	errCh := make(chan error, streams)

	for i := 0; i < streams; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			startMu.Lock()
			stream := client.StartStream([]byte("concurrent"))
			startMu.Unlock()

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

// 7. 收到未知 cid 的数据帧时，stub 应回发 RST。
func TestUnknownStreamResets(t *testing.T) {
	ta, tb := newMemPair()
	defer ta.Close()
	defer tb.Close()

	peer := newSerializer(t) // 我们这一侧（ta）手工收发
	_ = NewTunnelStub(tb, newSerializer(t))

	const cid uint32 = 999999
	frame := &protocol.Frame{Cid: cid, Type: protocol.STREAM_FRAME, Data: []byte("hello")}
	if err := ta.SendPacket(peer.Serialize(frame)); err != nil {
		t.Fatalf("send: %v", err)
	}

	respCh := make(chan *protocol.Frame, 1)
	go func() {
		pkt, err := ta.ReadPacket()
		if err != nil {
			return
		}
		rf, err := peer.Derialize(pkt)
		if err != nil {
			return
		}
		respCh <- rf
	}()

	select {
	case rf := <-respCh:
		if rf.Type != protocol.RST_FRAME {
			t.Fatalf("expected RST_FRAME, got 0x%x", rf.Type)
		}
		if rf.Cid != cid {
			t.Fatalf("RST cid mismatch: got %d want %d", rf.Cid, cid)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("未知流未收到 RST")
	}
}

// 8. 收到 FIN 时销毁流并唤醒阻塞中的 Read（返回 EOF）。
func TestFinClosesStream(t *testing.T) {
	ta, tb := newMemPair()
	defer ta.Close()
	defer tb.Close()

	client := NewTunnelStub(ta, newSerializer(t))
	peer := newSerializer(t)

	stream := client.StartStream([]byte("addr"))
	go func() { _, _ = tb.ReadPacket() }() // 丢弃 client 发出的 INIT

	readErr := make(chan error, 1)
	go func() {
		buf := make([]byte, 8)
		_, err := stream.Read(buf)
		readErr <- err
	}()
	time.Sleep(50 * time.Millisecond) // 确保已阻塞在 Read

	fin := &protocol.Frame{Cid: stream.Cid, Type: protocol.FIN_FRAME, Data: []byte{0x1, 0x1}}
	if err := tb.SendPacket(peer.Serialize(fin)); err != nil {
		t.Fatalf("send fin: %v", err)
	}

	select {
	case err := <-readErr:
		if err != io.EOF {
			t.Fatalf("expected EOF after FIN, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("FIN 未唤醒阻塞的 Read")
	}
}

// 9. 半关闭：收到 FIN 后读端 EOF，但写端仍可继续发送，对端能收到。
func TestHalfCloseKeepsWriteOpen(t *testing.T) {
	ta, tb := newMemPair()
	defer ta.Close()
	defer tb.Close()

	client := NewTunnelStub(ta, newSerializer(t))
	peer := newSerializer(t)
	stream := client.StartStream([]byte("addr"))

	readErr := make(chan error, 1)
	go func() {
		buf := make([]byte, 16)
		_, err := stream.Read(buf)
		readErr <- err
	}()

	if _, err := tb.ReadPacket(); err != nil { // 收 INIT
		t.Fatalf("read init: %v", err)
	}
	fin := &protocol.Frame{Cid: stream.Cid, Type: protocol.FIN_FRAME, Data: []byte{0x1, 0x1}}
	if err := tb.SendPacket(peer.Serialize(fin)); err != nil {
		t.Fatalf("send fin: %v", err)
	}

	select {
	case err := <-readErr:
		if err != io.EOF {
			t.Fatalf("FIN 后读端应 EOF，got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("FIN 后读端未 EOF")
	}

	// 写端仍可用
	if _, err := stream.Write([]byte("after-fin")); err != nil {
		t.Fatalf("FIN 后写端应仍可用: %v", err)
	}
	got := make(chan string, 1)
	go func() {
		for {
			pkt, err := tb.ReadPacket()
			if err != nil {
				return
			}
			df, err := peer.Derialize(pkt)
			if err != nil {
				continue
			}
			if df.Type == protocol.STREAM_FRAME {
				got <- string(df.Data)
				return
			}
		}
	}()
	select {
	case s := <-got:
		if s != "after-fin" {
			t.Fatalf("FIN 后写出的数据错误: %q", s)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("FIN 后对端未收到写出的数据")
	}
}

// 10. 底层传输关闭时，Accept 应返回错误而不是永久阻塞
//（验证 bbk 之前未初始化 closech 的修复）。
func TestTransportCloseUnblocksAccept(t *testing.T) {
	ta, tb := newMemPair()
	defer tb.Close()

	stub := NewTunnelStub(ta, newSerializer(t))
	errc := make(chan error, 1)
	go func() {
		_, err := stub.Accept()
		errc <- err
	}()
	time.Sleep(50 * time.Millisecond)
	ta.Close()

	select {
	case err := <-errc:
		if err == nil {
			t.Fatal("传输关闭后 Accept 应返回错误")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("传输关闭未唤醒 Accept")
	}
}
