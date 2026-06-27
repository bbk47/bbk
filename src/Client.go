package bbk

import (
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/avast/retry-go"
	"github.com/bbk47/bbk/v3/src/proxy"
	"github.com/bbk47/bbk/v3/src/tunnel"
	"github.com/bbk47/bbk/v3/src/utils"
	"github.com/bbk47/toolbox"
)

type Client struct {
	opts      *ClientOpts
	logger    *toolbox.Logger
	tunnelOps *TunnelOpts

	smu     sync.Mutex      // 保护 session 的建立/替换
	session *tunnel.Session // 当前复用会话，nil 或已关闭时按需重建
}

func NewClient(opts *ClientOpts) *Client {
	cli := &Client{}
	cli.opts = opts
	cli.tunnelOps = opts.TunnelOpts
	cli.logger = utils.NewLogger("C", opts.LogLevel)
	return cli
}

// setupTunnel 建立一条隧道会话：裸连接 -> 整流加密 -> yamux 复用。
// 失败按固定次数/间隔重试。
func (cli *Client) setupTunnel() (*tunnel.Session, error) {
	tunOpts := cli.tunnelOps
	cli.logger.Infof("creating %s tunnel\n", tunOpts.Protocol)
	var sess *tunnel.Session
	err := retry.Do(
		func() error {
			raw, err := dialRawCarrier(tunOpts)
			if err != nil {
				return err
			}
			secure, err := tunnel.ClientSecure(raw, tunOpts.Method, tunOpts.Password)
			if err != nil {
				_ = raw.Close()
				return err
			}
			s, err := tunnel.Client(secure)
			if err != nil {
				_ = secure.Close()
				return err
			}
			sess = s
			return nil
		},
		retry.OnRetry(func(n uint, err error) {
			cli.logger.Errorf("setup tunnel failed!%s\n", err.Error())
		}),
		retry.Attempts(5),
		retry.Delay(time.Second*5),
	)
	if err != nil {
		return nil, err
	}
	cli.logger.Infof("create tunnel success!\n")
	return sess, nil
}

// getSession 返回当前可用会话；若不存在或已关闭则（线程安全地）重建。
// yamux 自带 keepalive，断链会使会话进入 closed，从而在此触发重连。
func (cli *Client) getSession() (*tunnel.Session, error) {
	cli.smu.Lock()
	defer cli.smu.Unlock()
	if cli.session != nil && !cli.session.IsClosed() {
		return cli.session, nil
	}
	sess, err := cli.setupTunnel()
	if err != nil {
		return nil, err
	}
	cli.session = sess
	return sess, nil
}

func (cli *Client) bindProxySocket(socket proxy.ProxySocket) {
	defer socket.Close()

	var remoteaddr string
	if proxy.IsUDPMarker(socket.GetAddr()) {
		remoteaddr = "udp-associate"
	} else {
		addrInfo, err := toolbox.ParseAddrInfo(socket.GetAddr())
		if err != nil {
			cli.logger.Infof("prase addr info err:%s\n", err.Error())
			return
		}
		remoteaddr = fmt.Sprintf("%s:%d", addrInfo.Addr, addrInfo.Port)
	}
	cli.logger.Infof("COMMAND===%s\n", remoteaddr)

	sess, err := cli.getSession()
	if err != nil {
		cli.logger.Errorf("get tunnel session err:%s\n", err.Error())
		return
	}
	// OpenStream 同步完成"携带地址 + 等待对端就绪(EST)"，返回即可转发。
	stream, err := sess.OpenStream(socket.GetAddr())
	if err != nil {
		cli.logger.Warnf("open stream %s err:%s\n", remoteaddr, err.Error())
		return
	}
	defer stream.Close()
	cli.logger.Infof("EST success:%s \n", remoteaddr)

	if up, ok := socket.(*proxy.Socks5UDPProxy); ok {
		// SOCKS5 规定：控制 TCP 连接关闭即代表 UDP 关联结束。
		go func() { _, _ = io.Copy(io.Discard, up.CtrlConn()); up.Close() }()
		proxy.ClientUDP(up.UDPConn(), stream, cli.logger)
	} else {
		utils.Relay(socket, stream, cli.logger) // 双向转发（支持半关闭），直到两端结束
	}
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

func (cli *Client) Bootstrap() {
	// 预热：尽早建立隧道以暴露配置错误；失败也不致命，后续按需重连。
	if _, err := cli.getSession(); err != nil {
		cli.logger.Errorf("initial tunnel setup failed: %s\n", err.Error())
	}
	cli.initServer()
}
