package bbk

import (
	"fmt"
	"net"

	"github.com/bbk47/bbk/v3/src/proxy"
	"github.com/bbk47/bbk/v3/src/server"
	"github.com/bbk47/bbk/v3/src/tunnel"
	"github.com/bbk47/bbk/v3/src/utils"
	"github.com/bbk47/toolbox"
)

type Server struct {
	opts   *ServerOpts
	logger *toolbox.Logger
}

func NewServer(opt *ServerOpts) Server {
	s := Server{}
	s.opts = opt

	s.logger = utils.NewLogger("S", opt.LogLevel)
	return s
}

func (sir *Server) handleConnection(tunnelConn *server.TunnelConn) {
	raw := serverCarrierRWC(tunnelConn)
	secure, err := tunnel.ServerSecure(raw, sir.opts.Method, sir.opts.Password)
	if err != nil {
		sir.logger.Errorf("secure handshake err:%s\n", err.Error())
		_ = raw.Close()
		return
	}
	sess, err := tunnel.Server(secure)
	if err != nil {
		sir.logger.Errorf("yamux server err:%s\n", err.Error())
		_ = secure.Close()
		return
	}
	go func() {
		defer sess.Close()
		for {
			stream, err := sess.AcceptStream()
			if err != nil {
				sir.logger.Errorf("stream accept err:%s\n", err.Error())
				return
			}
			go sir.handleStream(stream)
		}
	}()
}

func (sir *Server) handleStream(stream *tunnel.Stream) {
	defer stream.Close()

	if proxy.IsUDPMarker(stream.Addr) {
		sir.logger.Infof("REQ UDP ASSOCIATE\n")
		if err := stream.SetReady(); err != nil {
			return
		}
		proxy.ServeUDP(stream, sir.logger)
		sir.logger.Infof("UDP ASSOCIATE CLOSE\n")
		return
	}

	addrInfo, err := toolbox.ParseAddrInfo(stream.Addr)
	if err != nil {
		return
	}
	remoteAddr := fmt.Sprintf("%s:%d", addrInfo.Addr, addrInfo.Port)
	sir.logger.Infof("REQ CONNECT=>%s\n", remoteAddr)
	tsocket, err := net.Dial("tcp", remoteAddr)
	if err != nil {
		// 不发就绪状态：client 侧 OpenStream 将收到错误（等价旧的"无 EST"）。
		return
	}
	defer tsocket.Close()
	sir.logger.Infof("DIAL SUCCESS==>%s\n", remoteAddr)
	if err := stream.SetReady(); err != nil {
		return
	}
	sir.logger.Infof("Forwarding ==>%s\n", remoteAddr)
	utils.Relay(stream, tsocket, sir.logger)
	sir.logger.Infof("Stream CLOSE==>%s\n", remoteAddr)
}

func (sir *Server) checkServerOk(srv server.FrameServer, err error) {
	if err != nil {
		sir.logger.Fatalf("create server failed: %v\n", err)
		return
	}
	sir.logger.Infof("server listen %s\n", srv.GetAddr())
	srv.ListenConn(sir.handleConnection)
}

func (sir *Server) initServer() {
	opt := sir.opts
	if opt.WorkMode == "tcp" {
		srv, err := server.NewAbcTcpServer(opt.ListenAddr, opt.ListenPort)
		sir.checkServerOk(srv, err)
	} else if opt.WorkMode == "tls" {
		srv, err := server.NewAbcTlsServer(opt.ListenAddr, opt.ListenPort, opt.SslCrt, opt.SslKey)
		sir.checkServerOk(srv, err)
	} else if opt.WorkMode == "ws" {
		srv, err := server.NewAbcWssServer(opt.ListenAddr, opt.ListenPort, opt.WorkPath)
		sir.checkServerOk(srv, err)
	} else if opt.WorkMode == "h2" {
		srv, err := server.NewAbcHttp2Server(opt.ListenAddr, opt.ListenPort, opt.WorkPath, opt.SslCrt, opt.SslKey)
		sir.checkServerOk(srv, err)
	} else {
		sir.logger.Infof("unsupport work mode [%s]\n", opt.WorkMode)
	}
}

func (sir *Server) Bootstrap() {
	sir.initServer()
}
