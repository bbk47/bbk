package tunnel

import (
	"bytes"
	"crypto/rand"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

// pair 用 net.Pipe 造一对全双工内存连接，模拟 client/server 两端的裸载体。
func pair() (net.Conn, net.Conn) { return net.Pipe() }

// setup 在内存连接上完成两端的加密握手，返回包好的 client/server SecureConn。
func setup(t *testing.T, method, password string) (*SecureConn, *SecureConn) {
	t.Helper()
	c, s := pair()
	type res struct {
		sc  *SecureConn
		err error
	}
	cch := make(chan res, 1)
	sch := make(chan res, 1)
	go func() { sc, err := ClientSecure(c, method, password); cch <- res{sc, err} }()
	go func() { sc, err := ServerSecure(s, method, password); sch <- res{sc, err} }()

	var cli, srv *SecureConn
	for i := 0; i < 2; i++ {
		select {
		case r := <-cch:
			if r.err != nil {
				t.Fatalf("client secure handshake: %v", r.err)
			}
			cli = r.sc
		case r := <-sch:
			if r.err != nil {
				t.Fatalf("server secure handshake: %v", r.err)
			}
			srv = r.sc
		case <-time.After(3 * time.Second):
			t.Fatal("secure handshake timeout")
		}
	}
	return cli, srv
}

func methods() []string {
	return []string{"aes-128-cfb", "aes-256-cfb", "aes-128-ctr", "rc4-md5"}
}

// TestSecureRoundTrip 校验双向、分多段写入时的明文一致性。
func TestSecureRoundTrip(t *testing.T) {
	for _, m := range methods() {
		m := m
		t.Run(m, func(t *testing.T) {
			cli, srv := setup(t, m, "s3cr3t-passw0rd")

			payloads := [][]byte{
				[]byte("hello"),
				bytes.Repeat([]byte("A"), 1),
				bytes.Repeat([]byte("x"), 4096),
				randBytes(t, 65537),
			}

			// client -> server
			go func() {
				for _, p := range payloads {
					if _, err := cli.Write(p); err != nil {
						t.Errorf("client write: %v", err)
						return
					}
				}
			}()
			assertStream(t, srv, payloads, "c->s")

			// server -> client
			go func() {
				for _, p := range payloads {
					if _, err := srv.Write(p); err != nil {
						t.Errorf("server write: %v", err)
						return
					}
				}
			}()
			assertStream(t, cli, payloads, "s->c")
		})
	}
}

// TestSecureContinuousKeystream 校验"整流加密"特性：同一条连接连续两次写出
// 相同明文，底层密文必须不同（keystream 持续推进），以区别于旧实现"每帧用固定 IV
// 重置 keystream"导致的相同明文→相同密文。
func TestSecureContinuousKeystream(t *testing.T) {
	c, s := pair()
	const ivLen = 16 // aes-128-cfb
	plain := bytes.Repeat([]byte{0xAB}, 8)

	go func() {
		cli, err := ClientSecure(c, "aes-128-cfb", "pw")
		if err != nil {
			return
		}
		_, _ = cli.Write(plain)
		_, _ = cli.Write(plain)
	}()

	// server 侧手工完成 IV 交换，再直接读裸密文（不经过解密）。
	peerIV := make([]byte, ivLen)
	if _, err := io.ReadFull(s, peerIV); err != nil {
		t.Fatalf("read client iv: %v", err)
	}
	if _, err := s.Write(make([]byte, ivLen)); err != nil {
		t.Fatalf("write server iv: %v", err)
	}
	cipherText := make([]byte, len(plain)*2)
	if _, err := io.ReadFull(s, cipherText); err != nil {
		t.Fatalf("read ciphertext: %v", err)
	}
	if bytes.Equal(cipherText[:len(plain)], cipherText[len(plain):]) {
		t.Fatal("相同明文产生了相同密文，keystream 未连续推进（疑似固定 IV 复用）")
	}
}

func randBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return b
}

// assertStream 从 conn 读满与 payloads 等长的数据并逐段比对。
func assertStream(t *testing.T, conn io.Reader, payloads [][]byte, label string) {
	t.Helper()
	total := 0
	for _, p := range payloads {
		total += len(p)
	}
	got := make([]byte, total)
	var wg sync.WaitGroup
	wg.Add(1)
	var readErr error
	go func() {
		defer wg.Done()
		_, readErr = io.ReadFull(conn, got)
	}()
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s read timeout", label)
	}
	if readErr != nil {
		t.Fatalf("%s read: %v", label, readErr)
	}
	off := 0
	for i, p := range payloads {
		if !bytes.Equal(got[off:off+len(p)], p) {
			t.Fatalf("%s payload[%d] mismatch", label, i)
		}
		off += len(p)
	}
}
