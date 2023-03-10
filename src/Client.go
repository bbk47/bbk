package bbk

import (
	"bbk/src/proxy"
	"bbk/src/serializer"
	"bbk/src/transport"
	"bbk/src/utils"
	"fmt"
	"github.com/avast/retry-go"
	"github.com/bbk47/toolbox"
	"log"
	"net"
	"time"
)

type BrowserObj struct {
	Cid         string
	proxysocket proxy.ProxySocket
	stream_ch   chan *transport.Stream
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
	stubclient   *transport.TunnelStub
	transport    transport.Transport
	lastPong     uint64
	browserProxy map[string]*BrowserObj //线程共享变量
}

func NewClient(opts Option) Client {
	cli := Client{}

	cli.opts = opts
	cli.tunnelOps = opts.TunnelOpts
	cli.browserProxy = make(map[string]*BrowserObj)
	cli.reqch = make(chan *BrowserObj, 1024)
	// other
	cli.tunnelStatus = TUNNEL_INIT
	cli.lastPong = uint64(time.Now().UnixNano())
	cli.logger = utils.NewLogger("C", opts.LogLevel)
	return cli
}

func (cli *Client) setupwsConnection() error {
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
		return err
	}
	cli.stubclient = transport.NewTunnelStub(cli.transport, cli.serizer)
	cli.tunnelStatus = TUNNEL_OK
	cli.logger.Infof("create tunnel success!\n")
	go cli.stubclient.Start()
	go cli.listenStream()
	return nil
}

func (cli *Client) listenStream() {
	for {
		stream, err := cli.stubclient.Accept()
		if err != nil {
			// transport error
			cli.tunnelStatus = TUNNEL_DISCONNECT
			return
		}
		browerobj := cli.browserProxy[stream.Cid]
		if browerobj != nil {
			browerobj.stream_ch <- stream
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

	browserobj := &BrowserObj{proxysocket: socket, stream_ch: make(chan *transport.Stream)}
	cli.reqch <- browserobj

	select {
	case stream := <-browserobj.stream_ch: // 收到信号才开始读
		cli.logger.Infof("stream %s create success\n", remoteaddr)
		defer func() {
			stream.Close()
			delete(cli.browserProxy, stream.Cid)
		}()
		go transport.SocketPipe(socket, stream)
		transport.SocketPipe(stream, socket)
	case <-time.After(10 * time.Second):
		cli.logger.Warnf("connect %s timeout 10000ms exceeded!", remoteaddr)
	}
}

func (cli *Client) checkTunnel() {
	if cli.tunnelStatus != TUNNEL_OK {
		err := cli.setupwsConnection()
		if err != nil {
			log.Fatal(err)
		}
	}
}

func (cli *Client) serviceWorker() {
	go func() {
		for {
			cli.checkTunnel()
			select {
			case ref := <-cli.reqch:
				st, _ := cli.stubclient.InitStream(ref.proxysocket.GetAddr())
				cli.browserProxy[st.Cid] = ref
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
