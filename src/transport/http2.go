package transport

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"github.com/posener/h2conn"
	"golang.org/x/net/http2"
	"net/http"
)

type Http2Transport struct {
	h2socket *h2conn.Conn
}

func (ts *Http2Transport) SendPacket(data []byte) (err error) {
	length := len(data)
	data2 := append([]byte{uint8(length >> 8), uint8(length % 256)}, data...)
	_, err = ts.h2socket.Write(data2)
	return err
}

func (wst *Http2Transport) Close() (err error) {
	return wst.h2socket.Close()
}

func (ts *Http2Transport) ReadPacket() ([]byte, error) {
	// 接收数据
	lenbuf := make([]byte, 2)
	_, err := ts.h2socket.Read(lenbuf)
	if err != nil {
		return nil, err
	}
	len1 := int(lenbuf[0])*256 + int(lenbuf[1])
	databuf := make([]byte, len1)
	_, err = ts.h2socket.Read(databuf)
	if err != nil {
		return nil, err
	}
	return databuf, nil
}

func NewHttp2Transport(host, port, path string) (transport *Http2Transport, err error) {
	http2Url := fmt.Sprintf("https://%s:%s%s", host, port, path)
	tlsOpts := &tls.Config{
		InsecureSkipVerify: true,
	}
	h2ts := &http2.Transport{
		TLSClientConfig: tlsOpts,
	}
	client := &http.Client{Transport: h2ts}
	c := h2conn.Client{Method: http.MethodPost, Client: client}
	// Connect to the HTTP/2 server
	// The returned conn can be used to:
	//   1. Write - send data to the server.
	//   2. Read - receive data from the server.
	conn, resp, err := c.Connect(context.Background(), http2Url)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == 500 {
		return nil, errors.New("Server Error 500")
	}
	ts := &Http2Transport{h2socket: conn}
	return ts, nil
}
