package proxy

import (
	"bufio"
	"errors"
	"github.com/bbk47/toolbox"
	"net"
	"strings"
)

type ConnectProxy struct {
	addrBuf []byte
	conn    net.Conn
}

func (s *ConnectProxy) Read(buf []byte) (n int, err error) {
	return s.conn.Read(buf)
}
func (s *ConnectProxy) Write(buf []byte) (n int, err error) {
	return s.conn.Write(buf)
}
func (s *ConnectProxy) Close() error {
	return s.conn.Close()
}

func (s *ConnectProxy) CloseWrite() error {
	if cw, ok := s.conn.(closeWriter); ok {
		return cw.CloseWrite()
	}
	return nil
}

func (s *ConnectProxy) GetAddr() []byte {
	return s.addrBuf
}

//CONNECT server.example.com:80 HTTP/1.1
//Host: server.example.com:80
//Proxy-Authorization: basic aGVsbG86d29ybGQ=
func NewConnectProxy(conn net.Conn) (sss *ConnectProxy, err error) {
	sss = &ConnectProxy{}
	// 1. receive CONNECT request..
	rd := bufio.NewReader(conn)
	line, err := rd.ReadString('\n')
	if err != nil {
		return nil, errors.New("CONNECT token read failed!" + err.Error())
	}
	words := strings.Split(line, " ")
	if words[0] != "CONNECT" {
		return nil, errors.New("CONNECT token mismatch! get:" + words[0])
	}
	_, err = conn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
	if err != nil {
		return nil, err
	}
	chost := words[1]
	// 3. sends a HEADERS frame containing a 2xx series status code to the client, as defined in [RFC7231], Section 4.3.6
	res1 := strings.Split(chost, ":")
	hostname := res1[0]
	port := res1[1]
	//
	//fmt.Printf("======build socks5 packet...%s\n", req.Host)
	addr := toolbox.BuildSocks5AddrData(hostname, port)
	sss = &ConnectProxy{conn: conn, addrBuf: addr}
	return sss, nil
}
