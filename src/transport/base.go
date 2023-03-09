package transport

import (
	"bbk/src/protocol"
	"bbk/src/serializer"
	"bbk/src/server"
	"bbk/src/utils"
	"errors"
	"fmt"
	"github.com/bbk47/toolbox"
	"strconv"
	"sync"
	"time"
)

type Transport interface {
	SendPacket([]byte) (err error)
	ReadPacket() ([]byte, error)
	ReadFirstPacket() ([]byte, error)
	Close() error
}

func WrapTunnel(tunnel *server.TunnelConn) Transport {
	if tunnel.Tuntype == "ws" {
		return &WebsocketTransport{conn: tunnel.Wsocket}
	} else if tunnel.Tuntype == "h2" {
		return &Http2Transport{h2socket: tunnel.H2socket}
	} else if tunnel.Tuntype == "tcp" {
		return &TcpTransport{conn: tunnel.TcpSocket}
	} else {
		return &TlsTransport{conn: tunnel.TcpSocket}
	}
}

type TunnelStub struct {
	serizer  *serializer.Serializer
	tsport   Transport
	streamch chan *Stream
	close    chan uint8
	streams  map[string]*Stream
	wlock    sync.Mutex
}

func NewTunnelStub(tsport Transport, serizer *serializer.Serializer) *TunnelStub {
	stub := TunnelStub{tsport: tsport, serizer: serizer}
	stub.streamch = make(chan *Stream, 256)
	stub.streams = make(map[string]*Stream)
	return &stub
}

func (ts *TunnelStub) SetSerializer(serizer *serializer.Serializer) {
	ts.serizer = serizer
}

func (ts *TunnelStub) sendFrame(frame *protocol.Frame) error {
	//fmt.Println("send frame:", frame)
	// 序列化数据
	binaryData := ts.serizer.Serialize(frame)
	ts.wlock.Lock()
	defer ts.wlock.Unlock()
	// 发送数据
	// 发送数据
	fmt.Println("send packet len:", len(binaryData))
	return ts.tsport.SendPacket(binaryData)
}

func (ts *TunnelStub) sendFinFrame(streamId string) error {
	rstFrame := protocol.Frame{Cid: streamId, Type: protocol.FIN_FRAME, Data: []byte{0x1, 0x1}}
	err := ts.sendFrame(&rstFrame)
	return err
}

func (ts *TunnelStub) sendRstFrame(streamId string) error {
	rstFrame := protocol.Frame{Cid: streamId, Type: protocol.RST_FRAME, Data: []byte{0x1, 0x2}}
	err := ts.sendFrame(&rstFrame)
	return err
}

func (ts *TunnelStub) ListenPacket() {
	fmt.Println("ListenPacket====")
	for {
		packet, err := ts.tsport.ReadPacket()
		//fmt.Printf("transport read data:len:%d\n", len(packet))
		if err != nil {
			fmt.Println("transport read packet err;", err.Error())
			//cli.logger.Infof("tunnel error event:%s.", err.Error())
			//cli.logger.Errorf("tunnel event:%v\n", message)
			//cli.tunnelStatus = TUNNEL_INIT
			return
		}
		respFrame, err := ts.serizer.Derialize(packet)
		if err != nil {
			fmt.Errorf("protol error:%v\n", err)
			return
		}

		fmt.Printf("read tunnel cid:%s, data[%d]bytes, frame type:%d\n", respFrame.Cid, len(packet), respFrame.Type)
		if respFrame.Type == protocol.PING_FRAME {
			timebs := toolbox.GetNowInt64Bytes()
			data := append(respFrame.Data, timebs...)
			pongFrame := protocol.Frame{Cid: "00000000000000000000000000000000", Type: protocol.PONG_FRAME, Data: data}
			ts.sendFrame(&pongFrame)
		} else if respFrame.Type == protocol.PONG_FRAME {
			stByte := respFrame.Data[:13]
			atByte := respFrame.Data[13:26]
			nowst := time.Now().UnixNano() / 1e6
			st, err1 := strconv.Atoi(string(stByte))
			at, err2 := strconv.Atoi(string(atByte))

			if err1 != nil || err2 != nil {
				//cli.logger.Warn("invalid ping pong format")
				continue
			}
			upms := int64((at)) - int64(st)
			downms := nowst - int64(at)
			fmt.Printf("tunnel health！ up:%dms, down:%dms, rtt:%dms\n", upms, downms, nowst-int64(st))

		} else if respFrame.Type == protocol.INIT_FRAME {
			fmt.Println("init stream ====")
			// create stream for server
			st := NewStream(respFrame.Cid, respFrame.Data)
			ts.streams[st.Cid] = st
			ts.streamch <- st
		} else if respFrame.Type == protocol.STREAM_FRAME {
			// find stream , write stream
			streamId := respFrame.Cid
			stream := ts.streams[streamId]
			if stream == nil {
				continue
			}
			fmt.Println("stream frame==== Get", len(respFrame.Data))
			err := stream.produce(respFrame.Data) // data => stream => target socket
			if err != nil {
				delete(ts.streams, streamId)
			}
		} else if respFrame.Type == protocol.FIN_FRAME {
			streamId := respFrame.Cid
			delete(ts.streams, streamId)
		} else if respFrame.Type == protocol.RST_FRAME {
			//destory stream
			streamId := respFrame.Cid
			delete(ts.streams, streamId)
		} else if respFrame.Type == protocol.EST_FRAME {

			fmt.Println("est frame===")
			streamId := respFrame.Cid
			stream := ts.streams[streamId]
			if stream == nil {
				continue
			}
			fmt.Println("<<<<<<<")
			ts.streamch <- stream
		}
	}
}

func (ts *TunnelStub) StartStream(addr []byte) *Stream {
	fmt.Println("start stream===>")
	streamId := utils.GetUUID()
	stream := NewStream(streamId, addr)
	ts.streams[streamId] = stream
	initframe := protocol.Frame{Cid: streamId, Type: protocol.INIT_FRAME, Data: addr}
	ts.sendFrame(&initframe)
	return stream
}
func (ts *TunnelStub) SetReady(stream *Stream) {
	estframe := protocol.Frame{Cid: stream.Cid, Type: protocol.EST_FRAME, Data: stream.Addr}
	ts.sendFrame(&estframe)
}

func (ts *TunnelStub) Ping() error {
	data := toolbox.GetNowInt64Bytes()
	pingFrame := protocol.Frame{Cid: "00000000000000000000000000000000", Type: protocol.PING_FRAME, Data: data}
	err := ts.sendFrame(&pingFrame)
	return err
}

func (ts *TunnelStub) Accept() (*Stream, error) {
	fmt.Println("acceept on stream===")

	select {
	case st := <-ts.streamch: // 收到stream
		go ts.ForwardToTunnel(st)
		return st, nil
	case <-ts.close:
		// close transport
		return nil, errors.New("transport closed")
	}
}

func (ts *TunnelStub) ForwardToTunnel(stream *Stream) {
	for {
		select {
		case data, ok := <-stream.wbuf: // target write data=>stream=>transport
			if !ok {
				return
			}
			dataFrame := protocol.Frame{Cid: stream.Cid, Type: protocol.STREAM_FRAME, Data: data}
			err := ts.sendFrame(&dataFrame)
			if err != nil {
				fmt.Println("f===", err.Error())
				return
			}
		}
	}
}
