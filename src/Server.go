package bbk

import (
	"bbk/src/serializer"
	"bbk/src/server"
	"bbk/src/transport"
	"bbk/src/utils"
	"fmt"
	"github.com/bbk47/toolbox"
	"io"
	"net"
	"sync"
	"time"
)

type ConnectObj struct {
	Id       string `json:"id"`
	ctype    string
	tconn    *server.TunnelConn
	Mode     string `json:"mode"`
	Delay    int    `json:"delay"`
	Seed     string `json:"seed"`
	RemoteId string `json:"remoteId"`
	wlock    sync.Mutex
}

type Target struct {
	cid       string
	dataCache chan []byte
	closed    chan uint8
	status    string
	socket    net.Conn
}

type Server struct {
	opts    Option
	logger  *toolbox.Logger
	serizer *serializer.Serializer

	connMap   sync.Map
	targetMap sync.Map

	targetDict map[string]*Target
	wsLock     sync.Mutex
	tsLock     sync.Mutex
	mpLock     sync.Mutex
}

func NewServer(opt Option) Server {
	s := Server{}
	s.opts = opt
	s.targetDict = make(map[string]*Target)

	s.logger = utils.NewLogger("S", opt.LogLevel)
	return s
}

func (servss *Server) handleConnection(tunnel *server.TunnelConn) {
	fmt.Println("handle connection===")
	tsport := transport.WrapTunnel(tunnel)
	serverStub := transport.NewTunnelStub(tsport, servss.serizer)
	go serverStub.ListenPacket()
	go func() {
		for {
			stream, err := serverStub.Accept()
			if err != nil {
				// transport error
				continue
			}
			go servss.handleStream(serverStub, stream)
		}
	}()
}

func (servss *Server) handleStream(stub *transport.TunnelStub, stream *transport.Stream) {
	addrInfo, err := toolbox.ParseAddrInfo(stream.Addr)
	servss.logger.Infof("REQ CONNECT=>%s:%d\n", addrInfo.Addr, addrInfo.Port)
	remoteAddr := fmt.Sprintf("%s:%d", addrInfo.Addr, addrInfo.Port)
	//destAddrPort := fmt.Sprintf("%s:%d", addr, port)
	tsocket, err := net.Dial("tcp", remoteAddr)
	if err != nil {
		return
	}
	servss.logger.Infof("dial success===>%s:%d\n", addrInfo.Addr, addrInfo.Port)
	stub.SetReady(stream)
	defer func() {
		tsocket.Close()
		stream.Close()
	}()
	go func() {
		_, err := io.Copy(tsocket, stream)
		if err != nil {
			fmt.Println("111 time:", time.Now().UnixNano(), " =", err)
		}
	}()
	_, err = io.Copy(stream, tsocket)
	if err != nil {
		fmt.Println("222 time:", time.Now().UnixNano(), " =", err)
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
