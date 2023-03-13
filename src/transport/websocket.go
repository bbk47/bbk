package transport

import (
	"fmt"
	"github.com/gorilla/websocket"
)

type WebsocketTransport struct {
	conn *websocket.Conn
}

func (wst *WebsocketTransport) SendPacket(data []byte) (err error) {
	return SendWsSocket(wst.conn, data)
}

func (wst *WebsocketTransport) Close() (err error) {
	return wst.conn.Close()
}

func (wst *WebsocketTransport) ReadPacket() ([]byte, error) {
	_, packet, err := wst.conn.ReadMessage()
	return packet, err
}

func NewWebsocketTransport(host, port, path string, secure bool) (transport *WebsocketTransport, err error) {
	wsUrl := ""
	if secure {
		wsUrl = fmt.Sprintf("wss://%s%s", host, path)
	} else {
		wsUrl = fmt.Sprintf("ws://%s:%s%s", host, port, path)
	}
	fmt.Println("transport wsurl:" + wsUrl)
	ws, _, err := websocket.DefaultDialer.Dial(wsUrl, nil)
	if err != nil {
		// send error event
		return nil, err
	}
	wst := &WebsocketTransport{conn: ws}
	return wst, nil
}
