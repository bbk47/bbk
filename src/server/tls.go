package server

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
)

type AbcTlsServer struct {
	listener net.Listener
}

func (tcpss *AbcTlsServer) ListenConn(handler func(conn *TunnelConn)) {
	for {
		conn, err := tcpss.listener.Accept()
		if err != nil {
			continue
		}
		wrapConn := &TunnelConn{Tuntype: "tls", TcpSocket: conn}
		handler(wrapConn)
	}
}

func (wss *AbcTlsServer) ListenHttpConn(httpHandler func(http.ResponseWriter, *http.Request)) {
	// nothing to do
}

func (tcpss *AbcTlsServer) GetAddr() string {
	return "tls://" + tcpss.listener.Addr().String()
}

func NewAbcTlsServer(host string, port int, sslCrt, sslKey string) (svc *AbcTlsServer, err error) {
	address := fmt.Sprintf("%s:%d", host, port)
	cer, err := tls.LoadX509KeyPair(sslCrt, sslKey)
	if err != nil {
		return nil, err
	}
	config := &tls.Config{Certificates: []tls.Certificate{cer}}
	ln, err := tls.Listen("tcp", address, config)
	if err != nil {
		return nil, err
	}
	return &AbcTlsServer{listener: ln}, nil
}
