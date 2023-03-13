package transport

import (
	"gitee.com/bbk47/bbk/src/server"
	"github.com/gorilla/websocket"
	"net"
)

type Transport interface {
	SendPacket([]byte) (err error)
	ReadPacket() ([]byte, error)
	Close() error
}

func WrapTunnel(tunnel *server.TunnelConn) Transport {
	if tunnel.Tuntype == "ws" {
		return &WebsocketTransport{conn: tunnel.Wsocket}
	} else if tunnel.Tuntype == "h2" {
		return &Http2Transport{h2socket: tunnel.H2socket}
	} else if tunnel.Tuntype == "tcp" {
		return &TcpTransport{conn: tunnel.TcpSocket}
	} else {
		return &TlsTransport{conn: tunnel.TcpSocket}
	}
}

func BindStreamSocket(socket net.Conn, events *Events) {
	events.Status <- "open"
	// send ready event
	defer func() {
		socket.Close()
		events.Status <- "close"
	}()
	for {
		// 接收数据
		lenbuf := make([]byte, 2)
		_, err := socket.Read(lenbuf)
		if err != nil {
			// send error event
			events.Status <- "read err:" + err.Error()
			return
		}
		length1 := (int(lenbuf[0]))*256 + (int(lenbuf[1]))
		databuf := make([]byte, length1)
		_, err = socket.Read(databuf)
		events.Data <- databuf
		// send data event
	}
}

func SendStreamSocket(socket net.Conn, data []byte) (err error) {
	length := len(data)
	data2 := append([]byte{uint8(length >> 8), uint8(length % 256)}, data...)
	_, err = socket.Write(data2)
	return err
}

func BindWsSocket(wss *websocket.Conn, events *Events) {
	events.Status <- "open"
	// send ready event
	defer func() {
		wss.Close()
		events.Status <- "close"
	}()
	for {
		// 接收数据
		_, packet, err := wss.ReadMessage()
		if err != nil {
			// send error event
			events.Status <- "read ws err:" + err.Error()
			return
		}
		events.Data <- packet
		// send data event
	}
}

func SendWsSocket(wss *websocket.Conn, data []byte) (err error) {
	err = wss.WriteMessage(websocket.BinaryMessage, data)
	return err
}
