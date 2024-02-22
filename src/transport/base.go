package transport

import (
	"net"

	"github.com/bbk47/bbk/v3/src/server"
	"github.com/gorilla/websocket"
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

func SendStreamSocket(socket net.Conn, data []byte) (err error) {
	length := len(data)
	data2 := append([]byte{uint8(length >> 8), uint8(length % 256)}, data...)
	_, err = socket.Write(data2)
	return err
}

func SendWsSocket(wss *websocket.Conn, data []byte) (err error) {
	err = wss.WriteMessage(websocket.BinaryMessage, data)
	return err
}
