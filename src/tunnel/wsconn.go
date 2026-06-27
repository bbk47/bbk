package tunnel

import (
	"io"

	"github.com/gorilla/websocket"
)

// WsConn 把 gorilla 的"消息流" WebSocket 适配成 yamux 需要的"字节流"
// io.ReadWriteCloser。WebSocket 以离散二进制消息为单位，这里在读侧把多条消息
// 拼接成连续字节，在写侧把每次 Write 作为一条二进制消息发出。
//
// 并发约定：gorilla 要求同一时刻至多一个 reader、一个 writer；yamux 内部读/写
// 各自单 goroutine，恰好满足，故此处无需加锁。
type WsConn struct {
	ws *websocket.Conn
	r  io.Reader // 当前正在消费的消息 reader，为 nil 表示需要取下一条消息
}

// NewWsConn 包裹一条已建立的 websocket 连接。
func NewWsConn(ws *websocket.Conn) *WsConn {
	return &WsConn{ws: ws}
}

func (c *WsConn) Read(p []byte) (int, error) {
	for {
		if c.r == nil {
			_, r, err := c.ws.NextReader()
			if err != nil {
				return 0, err
			}
			c.r = r
		}
		n, err := c.r.Read(p)
		if err == io.EOF {
			// 当前消息读尽：切到下一条消息；若本次已读到数据先返回。
			c.r = nil
			if n > 0 {
				return n, nil
			}
			continue
		}
		return n, err
	}
}

func (c *WsConn) Write(p []byte) (int, error) {
	if err := c.ws.WriteMessage(websocket.BinaryMessage, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *WsConn) Close() error {
	return c.ws.Close()
}
