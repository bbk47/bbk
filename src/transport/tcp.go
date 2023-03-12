package transport

import (
	"fmt"
	"net"
	"time"
)

type TcpTransport struct {
	conn net.Conn
}

func (ts *TcpTransport) SendPacket(data []byte) (err error) {
	return SendStreamSocket(ts.conn, data)
}

func (wst *TcpTransport) Close() (err error) {
	return wst.conn.Close()
}

func (ts *TcpTransport) ReadPacket() ([]byte, error) {
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

func NewTcpTransport(host, port string) (transport *TcpTransport, err error) {
	remoteAddr := fmt.Sprintf("%s:%s", host, port)
	tSocket, err := net.DialTimeout("tcp", remoteAddr, time.Second*10)
	if err != nil {
		return nil, err
	}

	ts := &TcpTransport{conn: tSocket}
	return ts, nil
}
