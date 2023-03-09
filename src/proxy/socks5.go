package proxy

import (
	"errors"
	"io"
	"net"
)

type ProxySocket interface {
	Read(b []byte) (n int, err error)
	Write(b []byte) (n int, err error)
	Close() error
	GetAddr() []byte
}

type Socks5Proxy struct {
	addrBuf []byte
	conn    net.Conn
}

func (s *Socks5Proxy) Read(buf []byte) (n int, err error) {
	return s.conn.Read(buf)
}
func (s *Socks5Proxy) Write(buf []byte) (n int, err error) {
	return s.conn.Write(buf)
}
func (s *Socks5Proxy) Close() error {
	return s.conn.Close()
}

func (s *Socks5Proxy) GetAddr() []byte {
	return s.addrBuf
}

func NewSocks5Proxy(conn net.Conn) (sss *Socks5Proxy, err error) {
	buf := make([]byte, 256)

	// 读取 VER 和 NMETHODS
	n, err := io.ReadFull(conn, buf[:2])
	if n != 2 {
		return nil, errors.New("socks5 ver/method read failed!" + err.Error())
	}

	ver, nMethods := int(buf[0]), int(buf[1])
	if ver != 5 {
		return nil, errors.New("socks5 ver invalid!")
	}

	// 读取 METHODS 列表
	n, err = io.ReadFull(conn, buf[:nMethods])
	if n != nMethods {
		return nil, errors.New("socks5 method err!")
	}
	// INIT
	//无需认证
	n, err = conn.Write([]byte{0x05, 0x00})
	if n != 2 || err != nil {
		return nil, errors.New("socks5 write noauth err!")
	}

	//119 119 119 46 103 111 111 103 108 101 46 99 111 109 1 187

	n, err = io.ReadFull(conn, buf[:4])
	if n != 4 {
		return nil, errors.New("protol error:!" + err.Error())
	}

	ver, cmd, _, atyp := int(buf[0]), buf[1], buf[2], buf[3]
	if ver != 5 || cmd != 1 {
		return nil, errors.New("invalid ver/cmd")
	}
	addrLen := 0
	if atyp == 0x1 {
		addrLen = 7
		_, err = io.ReadFull(conn, buf[4:10])
	} else if atyp == 0x3 {
		_, err = io.ReadFull(conn, buf[4:5])
		domainLen := int(buf[4])
		addrLen = domainLen + 4
		_, err = io.ReadFull(conn, buf[5:5+domainLen+2])
	}

	addBuf := buf[3 : addrLen+3]
	//addrInfo, err := toolbox.ParseAddrInfo(addBuf)
	//cli.logger.Infof("SOCKS5[COMMAND]===%s:%d\n", addrInfo.Addr, addrInfo.Port)

	// COMMAND RESP
	n, err = conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
	if err != nil {
		return nil, errors.New(err.Error())
	}
	sss = &Socks5Proxy{conn: conn, addrBuf: addBuf}
	return sss, nil
}
