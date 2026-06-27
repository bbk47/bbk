package transport

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/posener/h2conn"
	"golang.org/x/net/http2"
)

// 本文件提供"裸字节流"拨号：不做任何应用层分帧，直接把载体连接作为
// io.ReadWriteCloser 返回，交由上层 (SecureConn + yamux) 处理。
// 与旧的 New*Transport(面向包的 SendPacket/ReadPacket) 并存，迁移完成后移除旧路径。

// DialRawTCP 建立 TCP 裸连接。
func DialRawTCP(host, port string) (net.Conn, error) {
	return net.DialTimeout("tcp", fmt.Sprintf("%s:%s", host, port), 10*time.Second)
}

// DialRawTLS 建立 TLS 裸连接。
func DialRawTLS(host, port string) (net.Conn, error) {
	return tls.Dial("tcp", fmt.Sprintf("%s:%s", host, port), &tls.Config{InsecureSkipVerify: true})
}

// DialRawH2 建立 HTTP/2 双向流，h2conn.Conn 本身即 io.ReadWriteCloser。
func DialRawH2(host, port, path string) (io.ReadWriteCloser, error) {
	url := fmt.Sprintf("https://%s:%s%s", host, port, path)
	h2ts := &http2.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	c := h2conn.Client{Method: http.MethodPost, Client: &http.Client{Transport: h2ts}}
	conn, resp, err := c.Connect(context.Background(), url)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == 500 {
		return nil, errors.New("server error 500")
	}
	return conn, nil
}

// DialRawWS 建立 WebSocket 连接并返回底层 *websocket.Conn，
// 由调用方用字节流适配器(tunnel.NewWsConn)包成 io.ReadWriteCloser。
func DialRawWS(host, port, path string, secure bool) (*websocket.Conn, error) {
	var wsURL string
	if secure {
		wsURL = fmt.Sprintf("wss://%s%s", host, path)
	} else {
		wsURL = fmt.Sprintf("ws://%s:%s%s", host, port, path)
	}
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return nil, err
	}
	return ws, nil
}
