package server

import (
	"github.com/gorilla/websocket"
	"github.com/posener/h2conn"
	"net"
	"net/http"
)

type Events struct {
	Data   chan []byte
	Status chan string
}

type TunnelConn struct {
	Tuntype   string
	Wsocket   *websocket.Conn
	TcpSocket net.Conn
	H2socket  *h2conn.Conn
}

type FrameServer interface {
	ListenConn(handler func(conn *TunnelConn))
	ListenHttpConn(httpHandler func(http.ResponseWriter, *http.Request))
	GetAddr() string
}
