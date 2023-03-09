package bbk

import (
	"bbk/src/proxy"
	"bbk/src/serializer"
	"bbk/src/transport"
	"bbk/src/utils"
	"fmt"
	"github.com/avast/retry-go"
	"github.com/bbk47/toolbox"
	"net"
	"sync"
	"time"
)

type BrowserObj struct {
	proxysocket proxy.ProxySocket
	stream      *transport.Stream
	start       chan uint8
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
	//tunlock          sync.Mutex
	maplock sync.RWMutex
}

func NewClient(opts Option) Client {
	cli := Client{}

	cli.opts = opts
	cli.tunnelOps = opts.TunnelOpts
	cli.browserProxy = make(map[string]*BrowserObj)
	cli.reqch = make(chan *BrowserObj, 32)
	// other
	cli.tunnelStatus = TUNNEL_INIT
	cli.lastPong = uint64(time.Now().UnixNano())
	cli.logger = utils.NewLogger("C", opts.LogLevel)
	return cli
}

func (cli *Client) setupwsConnection() error {
	cli.logger.Infof("creating tunnel.")
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
	go func() {
		for {
			stream, err := cli.stubclient.Accept()
			fmt.Println("get stream====>")
			if err != nil {
				// transport error
				cli.tunnelStatus = TUNNEL_DISCONNECT
				return
			}
			cli.handleStream(stream)
		}
	}()
	go cli.stubclient.ListenPacket()

	return nil
}

func (cli *Client) handleStream(stream *transport.Stream) {
	fmt.Println("stream create success")
	browerobj := cli.browserProxy[stream.Cid]
	if browerobj == nil {
		return
	}
	browerobj.stream = stream
	browerobj.start <- 1
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

	newbrowserobj := &BrowserObj{proxysocket: socket, start: make(chan uint8)}
	cli.reqch <- newbrowserobj

	select {
	case <-newbrowserobj.start: // 收到信号才开始读
		fmt.Println("start socket pipe===")
		go transport.SocketPipe(socket, newbrowserobj.stream)
		transport.SocketPipe(newbrowserobj.stream, socket)
	case <-time.After(10 * time.Second):
		cli.logger.Warnf("connect %s timeout 10000ms exceeded!", remoteaddr)
	}
}

func (cli *Client) serviceWorker() {
	tick := time.NewTicker(3 * time.Second)
	go func() {
		for {
			//fmt.Println("check====request===")
			if cli.tunnelStatus != TUNNEL_OK {
				err := cli.setupwsConnection()
				if err != nil {
					fmt.Println(err)
				}
			}

			//select {
			//case ref := <-cli.reqch:
			//	fmt.Println("check====request==11=")
			//
			//	st := cli.stubclient.StartStream(ref.proxysocket.GetAddr())
			//	cli.browserProxy[st.Cid] = ref
			//}
			select {
			case <-tick.C:
				fmt.Println("ping sub client")
				if cli.stubclient != nil {
					cli.stubclient.Ping()
				}
			default:
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
	cli.initServer()
}
