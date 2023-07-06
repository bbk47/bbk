package bbk

import (
	"gitee.com/bbk47/bbk/v3/src/forward"
	"gitee.com/bbk47/bbk/v3/src/serializer"
	"gitee.com/bbk47/bbk/v3/src/server"
	"gitee.com/bbk47/bbk/v3/src/stub"
	"gitee.com/bbk47/bbk/v3/src/transport"
	"gitee.com/bbk47/bbk/v3/src/utils"
	"github.com/bbk47/toolbox"
)

type Server struct {
	opts    Option
	logger  *toolbox.Logger
	serizer *serializer.Serializer
}

func NewServer(opt Option) Server {
	s := Server{}
	s.opts = opt

	s.logger = utils.NewLogger("S", opt.LogLevel)
	return s
}

func (servss *Server) handleConnection(tunnel *server.TunnelConn) {
	tsport := transport.WrapTunnel(tunnel)
	serverStub := stub.NewTunnelStub(tsport, servss.serizer)
	go func() {
		for {
			stream, err := serverStub.Accept()
			if err != nil {
				// transport error
				servss.logger.Errorf("stream accept err:%s\n", err.Error())
				return
			}
			go servss.handleStream(stream)
		}
	}()
}

func (servss *Server) handleStream(stream *stub.Stream) {
	defer stream.Close()
	err := forward.ShadowsockForward(stream)
	if err != nil {
		servss.logger.Errorf("stream err:%s\n", err.Error())
	}
}

func (servss *Server) checkServerOk(srv server.FrameServer, err error) {
	if err != nil {
		servss.logger.Fatalf("create server failed: %v\n", err)
		return
	}
	servss.logger.Infof("server listen %s\n", srv.GetAddr())
	srv.ListenConn(servss.handleConnection)
}

func (servss *Server) initServer() {
	opt := servss.opts
	if opt.WorkMode == "tcp" {
		srv, err := server.NewAbcTcpServer(opt.ListenAddr, opt.ListenPort)
		servss.checkServerOk(srv, err)
	} else if opt.WorkMode == "tls" {
		srv, err := server.NewAbcTlsServer(opt.ListenAddr, opt.ListenPort, opt.SslCrt, opt.SslKey)
		servss.checkServerOk(srv, err)
	} else if opt.WorkMode == "ws" {
		srv, err := server.NewAbcWssServer(opt.ListenAddr, opt.ListenPort, opt.WorkPath)
		servss.checkServerOk(srv, err)
	} else if opt.WorkMode == "h2" {
		srv, err := server.NewAbcHttp2Server(opt.ListenAddr, opt.ListenPort, opt.WorkPath, opt.SslCrt, opt.SslKey)
		servss.checkServerOk(srv, err)
	} else {
		servss.logger.Infof("unsupport work mode [%s]\n", opt.WorkMode)
	}
}

func (servss *Server) initSerizer() {
	opt := servss.opts
	serizer, err := serializer.NewSerializer(opt.Method, opt.Password)
	if err != nil {
		servss.logger.Fatal(err)
	}
	servss.serizer = serizer
}

func (servss *Server) Bootstrap() {
	servss.initSerizer()
	servss.initServer()
}
