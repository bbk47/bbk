package bbk

import (
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/avast/retry-go"
	"github.com/bbk47/bbk/v3/src/proxy"
	"github.com/bbk47/bbk/v3/src/serializer"
	"github.com/bbk47/bbk/v3/src/stub"
	"github.com/bbk47/bbk/v3/src/transport"
	"github.com/bbk47/bbk/v3/src/utils"
	"github.com/bbk47/toolbox"
)

type BrowserObj struct {
	Cid         uint32
	proxysocket proxy.ProxySocket
	stream_ch   chan *stub.Stream
}

type Client struct {
	opts      *ClientOpts
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
	browserProxy map[uint32]*BrowserObj //线程共享变量
}

func NewClient(opts *ClientOpts) Client {
	cli := Client{}

	cli.opts = opts
	cli.tunnelOps = opts.TunnelOpts
	cli.browserProxy = make(map[uint32]*BrowserObj)
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
		stream, err := cli.stubclient.Accept()
		if err != nil {
			// transport error
			cli.tunnelStatus = TUNNEL_DISCONNECT
			return
		}
		//fmt.Println("=====>get stream")
		browerobj := cli.browserProxy[stream.Cid]
		if browerobj != nil {
			select {
			case browerobj.stream_ch <- stream:
			// send to proxy

			case <-time.After(15 * time.Second):
				cli.logger.Warnf("===timeout===!")
				continue
			}
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

	select {
	case stream := <-browserobj.stream_ch: // 收到信号才开始读
		cli.logger.Infof("EST success:%s \n", remoteaddr)
		defer func() {
			stream.Close()
			delete(cli.browserProxy, stream.Cid)
		}()
		stub.Relay(socket, stream)
	case <-time.After(15 * time.Second):
		cli.logger.Warnf("connect %s timeout 15000ms exceeded!", remoteaddr)
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
				st := cli.stubclient.StartStream(ref.proxysocket.GetAddr())
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
	var wg sync.WaitGroup
	if opt.ListenHttpPort > 1080 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cli.initProxyServer(opt.ListenHttpPort, true)
		}()
	}
	if opt.ListenPort != 0 && opt.ListenPort > 1024 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cli.initProxyServer(opt.ListenPort, false)
		}()
	}
	wg.Wait()
	cli.logger.Infof("All goroutine finished!\n")
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
