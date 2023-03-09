package server

import (
	"fmt"
	"github.com/posener/h2conn"
	"golang.org/x/net/http2"
	"log"
	"net"
	"net/http"
)

type AbcHttp2Server struct {
	addr        string
	sslcrt      string
	sslkey      string
	server      *http.Server
	listener    net.Listener
	path        string
	httpHandler func(http.ResponseWriter, *http.Request)
	tunHandler  func(*TunnelConn)
}

func (h2a *AbcHttp2Server) ServeHTTP(writer http.ResponseWriter, req *http.Request) {
	pathname := req.URL.Path
	if pathname == h2a.path {
		// We only accept HTTP/2!
		// (Normally it's quite common to accept HTTP/1.- and HTTP/2 together.)
		h2ccc, err := h2conn.Accept(writer, req)
		if err != nil {
			log.Printf("Failed creating connection from %s: %s", req.RemoteAddr, err)
			http.Error(writer, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		wrapConn := &TunnelConn{Tuntype: "h2", H2socket: h2ccc}
		if h2a.tunHandler != nil {
			h2a.tunHandler(wrapConn)
		}
	} else if h2a.tunHandler != nil {
		h2a.httpHandler(writer, req)
	}
}

func (h2a *AbcHttp2Server) ListenHttpConn(httpHandler func(http.ResponseWriter, *http.Request)) {
	h2a.httpHandler = httpHandler
}

func (h2a *AbcHttp2Server) ListenConn(handler func(conn *TunnelConn)) {
	defer func() {
		h2a.server = nil
		h2a.listener = nil
	}()
	h2a.tunHandler = handler
	err := h2a.server.ServeTLS(h2a.listener, h2a.sslcrt, h2a.sslkey)
	if err != nil {
		log.Fatal(err)
	}
}

func (h2a *AbcHttp2Server) GetAddr() string {
	return h2a.addr
}

func NewAbcHttp2Server(host string, port int, path string, sslCrt, sslKey string) (h2a *AbcHttp2Server, err error) {
	address := fmt.Sprintf("%s:%d", host, port)
	h2a = &AbcHttp2Server{path: path, sslkey: sslKey, sslcrt: sslCrt}
	srv := &http.Server{Addr: address, Handler: h2a}
	_ = http2.ConfigureServer(srv, &http2.Server{})

	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		return nil, err
	}
	h2a.addr = fmt.Sprintf("https://%s:%d%s", host, port, path)
	h2a.listener = ln
	h2a.server = srv

	return h2a, nil
}
