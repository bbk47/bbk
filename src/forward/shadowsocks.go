package forward

import (
	"errors"
	"fmt"
	"gitee.com/bbk47/bbk/v3/src/stub"
	"github.com/bbk47/toolbox"
	"io"
	"net"
	"strconv"
)

func ShadowsockForward(conn io.ReadWriteCloser) error {
	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	if err != nil {
		fmt.Errorf("xxx:%v\n", err)
	}
	addbytes := buf[:n]

	addrInfo, err := toolbox.ParseAddrInfo(addbytes)
	if err != nil {
		return err
	}

	destAddrPort := fmt.Sprintf("%s:%d", addrInfo.Addr, addrInfo.Port)
	dest, err := net.Dial("tcp", destAddrPort)
	if err != nil {
		return errors.New("dial dst: " + err.Error())
	}
	defer dest.Close()
	n, err = conn.Write([]byte{0x05, 0x00, 0x00, 0x00})
	if err != nil {
		return errors.New("write rsp: " + err.Error())
	}

	err = stub.Relay(conn, dest)
	return err
}

func ShadowsockHandshake(socket io.ReadWriteCloser, hostname string, port uint16) error {
	port2 := strconv.Itoa(int(port))
	addrbytes := toolbox.BuildSocks5AddrData(hostname, port2)
	n, err := socket.Write(addrbytes)
	if err != nil {
		return err
	}
	buf := make([]byte, 256)
	n, err = io.ReadFull(socket, buf[:4])
	if buf[0] != 0x5 {
		return errors.New("server unsupport ss protocol")
	}
	if n != 4 {
		return errors.New("connect " + hostname + " failed")
	}
	return nil
}
