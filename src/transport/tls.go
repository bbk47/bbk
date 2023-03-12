package transport

import (
	"crypto/tls"
	"fmt"
	"net"
)

type TlsTransport struct {
	conn net.Conn
}

func (ts *TlsTransport) SendPacket(data []byte) (err error) {

	length := len(data)
	data2 := append([]byte{uint8(length >> 8), uint8(length % 256)}, data...)
	_, err = ts.conn.Write(data2)
	//err = ts.conn.WriteMessage(websocket.BinaryMessage, data)
	return err
}
func (wst *TlsTransport) Close() (err error) {
	return wst.conn.Close()
}
func (ts *TlsTransport) ReadPacket() ([]byte, error) {
	// 接收数据
	lenbuf := make([]byte, 2)
	_, err := ts.conn.Read(lenbuf)
	if err != nil {
		return nil, err
	}
	leng := int(lenbuf[0])*256 + int(lenbuf[1])
	databuf := make([]byte, leng)
	_, err = ts.conn.Read(databuf)
	if err != nil {
		return nil, err
	}
	return databuf, nil
}

func NewTlsTransport(host, port string) (transport *TlsTransport, err error) {
	remoteAddr := fmt.Sprintf("%s:%s", host, port)
	conf := &tls.Config{
		InsecureSkipVerify: true,
	}
	tlsSocket, err := tls.Dial("tcp", remoteAddr, conf)
	if err != nil {
		return nil, err
	}

	ts := &TlsTransport{conn: tlsSocket}
	return ts, nil
}
