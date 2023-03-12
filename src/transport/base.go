package transport

import "bbk/src/server"

type Transport interface {
	SendPacket([]byte) (err error)
	ReadPacket() ([]byte, error)
	ReadFirstPacket() ([]byte, error)
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
