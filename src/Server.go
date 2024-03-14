package bbk

import (
	"fmt"
	"net"

	"github.com/bbk47/bbk/v3/src/serializer"
	"github.com/bbk47/bbk/v3/src/server"
	"github.com/bbk47/bbk/v3/src/stub"
	"github.com/bbk47/bbk/v3/src/transport"
	"github.com/bbk47/bbk/v3/src/utils"
	"github.com/bbk47/toolbox"
)

type Server struct {
	opts    *ServerOpts
	logger  *toolbox.Logger
	serizer *serializer.Serializer
}

func NewServer(opt *ServerOpts) Server {
	s := Server{}
	s.opts = opt

	s.logger = utils.NewLogger("S", opt.LogLevel)
	return s
}

func (sir *Server) handleConnection(tunnel *server.TunnelConn) {
	tsport := transport.WrapTunnel(tunnel)
	serverStub := stub.NewTunnelStub(tsport, sir.serizer)
	go func() {
		for {
			stream, err := serverStub.Accept()
			if err != nil {
				// transport error
				sir.logger.Errorf("stream accept err:%s\n", err.Error())
				return
			}
			go sir.handleStream(serverStub, stream)
		}
	}()
}

func (sir *Server) handleStream(serstub *stub.TunnelStub, stream *stub.Stream) {
	defer stream.Close()

	addrInfo, err := toolbox.ParseAddrInfo(stream.Addr)
	if err != nil {
		return
	}
	remoteAddr := fmt.Sprintf("%s:%d", addrInfo.Addr, addrInfo.Port)
	sir.logger.Infof("REQ CONNECT=>%s\n", remoteAddr)
	tsocket, err := net.Dial("tcp", remoteAddr)
	if err != nil {
		return
	}
	defer tsocket.Close()
	sir.logger.Infof("DIAL SUCCESS==>%s\n", remoteAddr)
	serstub.SetReady(stream)
	sir.logger.Infof("Forwarding ==>%s\n", remoteAddr)
	go utils.Forward(stream, tsocket, "stream->tsocket:"+remoteAddr, sir.logger)
	utils.Forward(tsocket, stream, "tsocket->stream:"+remoteAddr, sir.logger)
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

func (sir *Server) initSerizer() {
	opt := sir.opts
	serizer, err := serializer.NewSerializer(opt.Method, opt.Password)
	if err != nil {
		sir.logger.Fatal(err)
	}
	sir.serizer = serizer
}

func (sir *Server) Bootstrap() {
	sir.initSerizer()
	sir.initServer()
}
