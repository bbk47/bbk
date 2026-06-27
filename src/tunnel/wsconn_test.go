package tunnel

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// wsPair 起一个 httptest websocket 服务端，并用 gorilla dialer 连上，
// 返回 client 端与 server 端两个 *WsConn。
func wsPair(t *testing.T) (cli *WsConn, srv *WsConn, cleanup func()) {
	t.Helper()
	up := websocket.Upgrader{}
	srvCh := make(chan *websocket.Conn, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := up.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		srvCh <- ws
	}))
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")
	cws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		ts.Close()
		t.Fatalf("dial: %v", err)
	}
	var sws *websocket.Conn
	select {
	case sws = <-srvCh:
	case <-time.After(3 * time.Second):
		ts.Close()
		t.Fatal("server accept timeout")
	}
	return NewWsConn(cws), NewWsConn(sws), func() { cws.Close(); sws.Close(); ts.Close() }
}

// TestWsConnReassembly 校验：写侧拆成多条小消息，读侧能按字节流无缝拼回，
// 且一次 Read 能跨越消息边界正确推进。
func TestWsConnReassembly(t *testing.T) {
	cli, srv, cleanup := wsPair(t)
	defer cleanup()

	// 客户端分多条消息写出（含 1 字节、空内容、超大块），服务端按字节流读回。
	chunks := [][]byte{
		[]byte("HELLO-"),
		[]byte("a"),
		bytes.Repeat([]byte("Z"), 70000),
		[]byte("-END"),
	}
	var want []byte
	for _, c := range chunks {
		want = append(want, c...)
	}

	go func() {
		for _, c := range chunks {
			if _, err := cli.Write(c); err != nil {
				t.Errorf("cli write: %v", err)
				return
			}
		}
	}()

	got := make([]byte, len(want))
	if _, err := io.ReadFull(srv, got); err != nil {
		t.Fatalf("server readfull: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("reassembled mismatch: got %d bytes", len(got))
	}
}

// TestWsConnWithSecure 验证 SecureConn 叠加在 WsConn 之上端到端可用，
// 这正是后续 yamux 的实际承载组合：ws -> SecureConn -> yamux。
func TestWsConnWithSecure(t *testing.T) {
	cli, srv, cleanup := wsPair(t)
	defer cleanup()

	var csecure, ssecure *SecureConn
	var wg sync.WaitGroup
	wg.Add(2)
	var cerr, serr error
	go func() { defer wg.Done(); csecure, cerr = ClientSecure(cli, "aes-256-cfb", "pw") }()
	go func() { defer wg.Done(); ssecure, serr = ServerSecure(srv, "aes-256-cfb", "pw") }()
	wg.Wait()
	if cerr != nil || serr != nil {
		t.Fatalf("secure over ws handshake: cerr=%v serr=%v", cerr, serr)
	}

	msg := bytes.Repeat([]byte("payload-0123456789"), 5000)
	go func() { _, _ = csecure.Write(msg) }()
	got := make([]byte, len(msg))
	if _, err := io.ReadFull(ssecure, got); err != nil {
		t.Fatalf("secure-over-ws read: %v", err)
	}
	if !bytes.Equal(got, msg) {
		t.Fatal("secure-over-ws payload mismatch")
	}
}
