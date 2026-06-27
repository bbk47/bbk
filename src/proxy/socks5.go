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
	CloseWrite() error
	GetAddr() []byte
}

// closeWriter 由 *net.TCPConn / *tls.Conn 实现，用于半关闭写端。
type closeWriter interface {
	CloseWrite() error
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

func (s *Socks5Proxy) CloseWrite() error {
	if cw, ok := s.conn.(closeWriter); ok {
		return cw.CloseWrite()
	}
	return nil
}

func (s *Socks5Proxy) GetAddr() []byte {
	return s.addrBuf
}

func NewSocks5Proxy(conn net.Conn) (ProxySocket, error) {
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
	// cmd: 0x01=CONNECT(TCP), 0x03=UDP ASSOCIATE
	if ver != 5 || (cmd != 0x01 && cmd != 0x03) {
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

	if cmd == 0x03 {
		return newSocks5UDPProxy(conn)
	}

	// CONNECT COMMAND RESP
	n, err = conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
	if err != nil {
		return nil, errors.New(err.Error())
	}
	return &Socks5Proxy{conn: conn, addrBuf: addBuf}, nil
}

// Socks5UDPProxy 表示一个 SOCKS5 UDP ASSOCIATE 关联。
// ctrl 是 SOCKS5 控制用的 TCP 连接（其关闭标志关联结束），
// udp 是供 app 收发 UDP 数据报的中继 socket。
type Socks5UDPProxy struct {
	ctrl net.Conn
	udp  *net.UDPConn
}

func (s *Socks5UDPProxy) Read(b []byte) (int, error)  { return 0, io.EOF }
func (s *Socks5UDPProxy) Write(b []byte) (int, error) { return len(b), nil }
func (s *Socks5UDPProxy) Close() error {
	_ = s.udp.Close()
	return s.ctrl.Close()
}
func (s *Socks5UDPProxy) CloseWrite() error     { return nil }
func (s *Socks5UDPProxy) GetAddr() []byte       { return UDPMarkerAddr() }
func (s *Socks5UDPProxy) UDPConn() *net.UDPConn { return s.udp }
func (s *Socks5UDPProxy) CtrlConn() net.Conn    { return s.ctrl }

// newSocks5UDPProxy 在与控制连接相同的本机 IP 上开一个临时 UDP 中继端口，
// 并向 app 回复 BND.ADDR:BND.PORT（app 后续把 UDP 数据报发到这里）。
func newSocks5UDPProxy(ctrl net.Conn) (ProxySocket, error) {
	host := "127.0.0.1"
	if la, ok := ctrl.LocalAddr().(*net.TCPAddr); ok {
		host = la.IP.String()
	}
	laddr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(host, "0"))
	if err != nil {
		return nil, err
	}
	uc, err := net.ListenUDP("udp", laddr)
	if err != nil {
		return nil, err
	}
	bnd := uc.LocalAddr().(*net.UDPAddr)
	if _, err := ctrl.Write(buildUDPAssocReply(bnd)); err != nil {
		_ = uc.Close()
		return nil, err
	}
	return &Socks5UDPProxy{ctrl: ctrl, udp: uc}, nil
}

// buildUDPAssocReply 构造 UDP ASSOCIATE 的成功响应：VER REP RSV ATYP BND.ADDR BND.PORT
func buildUDPAssocReply(bnd *net.UDPAddr) []byte {
	reply := []byte{0x05, 0x00, 0x00}
	if ip4 := bnd.IP.To4(); ip4 != nil {
		reply = append(reply, 0x01)
		reply = append(reply, ip4...)
	} else {
		reply = append(reply, 0x04)
		reply = append(reply, bnd.IP.To16()...)
	}
	reply = append(reply, byte(bnd.Port>>8), byte(bnd.Port&0xff))
	return reply
}
