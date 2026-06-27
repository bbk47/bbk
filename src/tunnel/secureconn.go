package tunnel

import (
	"crypto/cipher"
	"crypto/rand"
	"io"

	"github.com/bbk47/toolbox"
)

// SecureConn 在一条裸字节流之上做整条连接的流式加密。
//
// 与旧的"逐帧、固定 IV"CFB 不同：每条连接握手时双方各自生成一个随机 IV 明文发出，
// 各自用"自己的 IV"初始化出站加密流、用"对端的 IV"初始化入站解密流，
// 两个方向各自一条连续 keystream，既消除了固定 IV 的 keystream 复用隐患，
// 也给 yamux 提供了它要求的"干净有序字节流"。
//
// 并发约定：yamux 内部读/写各自单 goroutine，因此 Read 与 Write 可并发，
// 但不应有多个 goroutine 同时 Write（与 yamux 的使用契约一致），故此处无锁。
type SecureConn struct {
	raw io.ReadWriteCloser
	enc cipher.Stream // 出站连续加密流（基于本端 IV）
	dec cipher.Stream // 入站连续解密流（基于对端 IV）
}

// secure 是 client/server 对称的握手实现：双方各自生成随机 IV 明文交换，
// 然后用本端 IV 建加密流、对端 IV 建解密流。
// 写 IV 在独立 goroutine 中并发进行，因此即使底层是无缓冲的同步载体
// （如 net.Pipe）也不会"两端都阻塞在写"而死锁。
func secure(raw io.ReadWriteCloser, method, password string) (*SecureConn, error) {
	enc, err := toolbox.NewEncryptor(method, password)
	if err != nil {
		return nil, err
	}
	ivLen := enc.IVLen()

	localIV := make([]byte, ivLen)
	if _, err := rand.Read(localIV); err != nil {
		return nil, err
	}
	writeErr := make(chan error, 1)
	go func() {
		_, e := writeFull(raw, localIV)
		writeErr <- e
	}()

	peerIV := make([]byte, ivLen)
	if _, err := io.ReadFull(raw, peerIV); err != nil {
		return nil, err
	}
	if err := <-writeErr; err != nil {
		return nil, err
	}

	encStream, err := enc.NewEncStream(localIV)
	if err != nil {
		return nil, err
	}
	decStream, err := enc.NewDecStream(peerIV)
	if err != nil {
		return nil, err
	}
	return &SecureConn{raw: raw, enc: encStream, dec: decStream}, nil
}

// ClientSecure 在客户端侧用给定方法/口令包裹一条裸连接。
func ClientSecure(raw io.ReadWriteCloser, method, password string) (*SecureConn, error) {
	return secure(raw, method, password)
}

// ServerSecure 在服务端侧用给定方法/口令包裹一条裸连接。
func ServerSecure(raw io.ReadWriteCloser, method, password string) (*SecureConn, error) {
	return secure(raw, method, password)
}

func (c *SecureConn) Read(p []byte) (int, error) {
	n, err := c.raw.Read(p)
	if n > 0 {
		// 仅对真实读到的 n 字节解密，保持 keystream 与有序字节流同步推进。
		c.dec.XORKeyStream(p[:n], p[:n])
	}
	return n, err
}

func (c *SecureConn) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	buf := make([]byte, len(p))
	c.enc.XORKeyStream(buf, p)
	if _, err := writeFull(c.raw, buf); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *SecureConn) Close() error {
	return c.raw.Close()
}

// writeFull 把 b 全部写出；载体可能产生短写，必须循环补齐，
// 否则会与已推进的 keystream 失步。
func writeFull(w io.Writer, b []byte) (int, error) {
	total := 0
	for total < len(b) {
		n, err := w.Write(b[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}
