package bbk

import (
	"fmt"
	"gitee.com/bbk47/bbk/v3/src/forward"
	"gitee.com/bbk47/bbk/v3/src/proxy"
	"gitee.com/bbk47/bbk/v3/src/serializer"
	"gitee.com/bbk47/bbk/v3/src/stub"
	"gitee.com/bbk47/bbk/v3/src/transport"
	"gitee.com/bbk47/bbk/v3/src/utils"
	"github.com/avast/retry-go"
	"github.com/bbk47/toolbox"
	"log"
	"net"
	"time"
)

type BrowserObj struct {
	Cid         string
	proxysocket proxy.ProxySocket
	stream_ch   chan *stub.Stream
}

type Client struct {
	opts      Option
	serizer   *serializer.Serializer
	logger    *toolbox.Logger
	tunnelOps *TunnelOpts
	reqch     chan *BrowserObj
	// inner attr
	retryCount   uint8
	tunnelStatus uint8
	stubclient   *stub.TunnelStub
	transport    transport.Transport
	lastPong     uint64
}

func NewClient(opts Option) Client {
	cli := Client{}

	cli.opts = opts
	cli.tunnelOps = opts.TunnelOpts
	cli.reqch = make(chan *BrowserObj, 1024)
	// other
	cli.tunnelStatus = TUNNEL_INIT
	cli.lastPong = uint64(time.Now().UnixNano())
	cli.logger = utils.NewLogger("C", opts.LogLevel)
	return cli
}

func (cli *Client) setupwsConnection() {
	tunOpts := cli.tunnelOps
	cli.logger.Infof("creating %s tunnel\n", tunOpts.Protocol)
	err := retry.Do(
		func() error {
			tsport, err := CreateTransport(tunOpts)
			if err != nil {
				return err
			}
			cli.transport = tsport
			return nil
		},
		retry.OnRetry(func(n uint, err error) {
			cli.logger.Errorf("setup tunnel failed!%s\n", err.Error())
		}),
		retry.Attempts(5),
		retry.Delay(time.Second*5),
	)
	if err != nil {
		log.Fatal(err)
	}
	cli.stubclient = stub.NewTunnelStub(cli.transport, cli.serizer)
	cli.stubclient.NotifyPong(func(up, down int64) {
		cli.logger.Infof("tunnel health！ up:%dms, down:%dms, rtt:%dms\n", up, down, up+down)
	})
	cli.tunnelStatus = TUNNEL_OK
	cli.logger.Infof("create tunnel success!\n")
	go cli.listenStream()
}

func (cli *Client) listenStream() {
	for {
		_, err := cli.stubclient.Accept()
		if err != nil {
			// transport error
			cli.tunnelStatus = TUNNEL_DISCONNECT
			return
		}
	}
}

func (cli *Client) bindProxySocket(socket proxy.ProxySocket) {
	defer socket.Close()

	addrInfo, err := toolbox.ParseAddrInfo(socket.GetAddr())
	if err != nil {
		cli.logger.Infof("prase addr info err:%s\n", err.Error())
		return
	}
	remoteaddr := fmt.Sprintf("%s:%d", addrInfo.Addr, addrInfo.Port)
	cli.logger.Infof("COMMAND===%s\n", remoteaddr)

	browserobj := &BrowserObj{proxysocket: socket, stream_ch: make(chan *stub.Stream)}
	cli.reqch <- browserobj
	cli.logger.Info("create stream.ok.")
	stream := <-browserobj.stream_ch
	cli.logger.Info("create stream.ok2.")
	defer stream.Close()
	cli.logger.Info("start stream handshake..")
	err = forward.ShadowsockHandshake(stream, addrInfo.Addr, addrInfo.Port)
	if err != nil {
		return
	}
	err = stub.Relay(socket, stream)
	if err != nil {
		cli.logger.Errorf("reply  err:%s\n", err.Error())
	}
}

func (cli *Client) serviceWorker() {
	go func() {
		for {
			if cli.tunnelStatus != TUNNEL_OK {
				cli.setupwsConnection()
			}
			select {
			case ref := <-cli.reqch:
				st := cli.stubclient.CreateStream("")
				ref.stream_ch <- st
			}
		}
	}()
}
func (cli *Client) keepPingWs() {
	go func() {
		ticker := time.Tick(time.Second * 15)
		for range ticker {
			if cli.stubclient != nil {
				cli.stubclient.Ping()
			}
		}
	}()
}

func (cli *Client) initProxyServer(port int, isConnect bool) {
	srv, err := proxy.NewProxyServer(cli.opts.ListenAddr, port)
	if err != nil {
		cli.logger.Fatalf("Listen failed: %v\n", err)
		return
	}
	cli.logger.Infof("proxy server listen on %s\n", srv.GetAddr())
	srv.ListenConn(func(conn net.Conn) {
		go func() {
			var proxyConn proxy.ProxySocket
			var err error
			if isConnect == true {
				proxyConn, err = proxy.NewConnectProxy(conn)
			} else {
				proxyConn, err = proxy.NewSocks5Proxy(conn)
			}
			if err != nil {
				cli.logger.Errorf("create proxy err:%s\n", err.Error())
				return
			}
			cli.bindProxySocket(proxyConn)
		}()
	})
}

func (cli *Client) initServer() {
	opt := cli.opts
	if opt.ListenHttpPort > 1080 {
		go cli.initProxyServer(opt.ListenHttpPort, true)
	}
	cli.initProxyServer(opt.ListenPort, false)
}

func (cli *Client) initSerizer() {
	serizer, err := serializer.NewSerializer(cli.tunnelOps.Method, cli.tunnelOps.Password)
	if err != nil {
		cli.logger.Fatal(err)
	}
	cli.serizer = serizer
}

func (cli *Client) Bootstrap() {
	cli.initSerizer()
	cli.serviceWorker()
	cli.keepPingWs()
	cli.initServer()
}
