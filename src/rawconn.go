package bbk

import (
	"fmt"
	"io"

	"github.com/bbk47/bbk/v3/src/server"
	"github.com/bbk47/bbk/v3/src/transport"
	"github.com/bbk47/bbk/v3/src/tunnel"
)

// dialRawCarrier 按隧道协议建立一条裸字节流(io.ReadWriteCloser)，
// WebSocket 经字节流适配器包装。加密与多路复用由上层(SecureConn + yamux)负责。
func dialRawCarrier(tunOpts *TunnelOpts) (io.ReadWriteCloser, error) {
	switch tunOpts.Protocol {
	case "tcp":
		return transport.DialRawTCP(tunOpts.Host, tunOpts.Port)
	case "tls":
		return transport.DialRawTLS(tunOpts.Host, tunOpts.Port)
	case "h2":
		return transport.DialRawH2(tunOpts.Host, tunOpts.Port, tunOpts.Path)
	case "ws":
		ws, err := transport.DialRawWS(tunOpts.Host, tunOpts.Port, tunOpts.Path, tunOpts.Secure)
		if err != nil {
			return nil, err
		}
		return tunnel.NewWsConn(ws), nil
	default:
		return nil, fmt.Errorf("unsupport tunnel protocol [%s]", tunOpts.Protocol)
	}
}

// serverCarrierRWC 把服务端接受到的隧道连接转成裸字节流(io.ReadWriteCloser)。
func serverCarrierRWC(tunnelConn *server.TunnelConn) io.ReadWriteCloser {
	switch tunnelConn.Tuntype {
	case "ws":
		return tunnel.NewWsConn(tunnelConn.Wsocket)
	case "h2":
		return tunnelConn.H2socket
	default: // tcp / tls
		return tunnelConn.TcpSocket
	}
}
