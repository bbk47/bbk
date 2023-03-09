package server

import (
	"fmt"
	"github.com/gorilla/websocket"
	"log"
	"net"
	"net/http"
)

var upgrader = websocket.Upgrader{} // use default options

type AbcWssServer struct {
	addr        string
	server      *http.Server
	listener    net.Listener
	path        string
	httpHandler func(http.ResponseWriter, *http.Request)
	tunHandler  func(*TunnelConn)
}

func (wss *AbcWssServer) ServeHTTP(writer http.ResponseWriter, req *http.Request) {
	pathname := req.URL.Path
	if pathname == wss.path {
		wsconn, err := upgrader.Upgrade(writer, req, nil)
		if err != nil {
			writer.Write([]byte(err.Error()))
			return
		}
		wrapConn := &TunnelConn{Tuntype: "ws", Wsocket: wsconn}
		if wss.tunHandler != nil {
			wss.tunHandler(wrapConn)
		}
	} else if wss.tunHandler != nil {
		wss.httpHandler(writer, req)
	}
}

func (wss *AbcWssServer) ListenHttpConn(httpHandler func(http.ResponseWriter, *http.Request)) {
	wss.httpHandler = httpHandler
}

func (wss *AbcWssServer) ListenConn(handler func(conn *TunnelConn)) {
	defer func() {
		wss.server = nil
		wss.listener = nil
	}()
	wss.tunHandler = handler
	err := wss.server.Serve(wss.listener)
	if err != nil {
		log.Fatal(err)
	}
}

func (wss *AbcWssServer) GetAddr() string {
	return wss.addr
}

func NewAbcWssServer(host string, port int, path string) (wss *AbcWssServer, err error) {
	address := fmt.Sprintf("%s:%d", host, port)
	wss = &AbcWssServer{path: path}
	server := &http.Server{Addr: address, Handler: wss}

	ln, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return nil, err
	}
	wss.addr = fmt.Sprintf("ws://%s:%d%s", host, port, path)
	wss.listener = ln
	wss.server = server

	return wss, nil
}
