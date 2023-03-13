package bbk

import (
	"errors"
	"fmt"
	"gitee.com/bbk47/bbk/src/transport"
)

func CreateTransport(tunOpts *TunnelOpts) (tsport transport.Transport, err error) {
	if tunOpts.Protocol == "ws" {
		tsport, err = transport.NewWebsocketTransport(tunOpts.Host, tunOpts.Port, tunOpts.Path, tunOpts.Secure)
	} else if tunOpts.Protocol == "tcp" {
		tsport, err = transport.NewTcpTransport(tunOpts.Host, tunOpts.Port)
	} else if tunOpts.Protocol == "tls" {
		tsport, err = transport.NewTlsTransport(tunOpts.Host, tunOpts.Port)
	} else if tunOpts.Protocol == "h2" {
		tsport, err = transport.NewHttp2Transport(tunOpts.Host, tunOpts.Port, tunOpts.Path)
	} else {
		err = errors.New(fmt.Sprintf("unsupport tunnel protocol [%s]!", tunOpts.Protocol))
	}
	if err != nil {
		return nil, err
	}

	return tsport, nil
}
