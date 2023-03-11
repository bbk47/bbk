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
	sendch   chan *protocol.Frame
	close    chan uint8
	streams  map[string]*Stream
	wlock    sync.Mutex
}

func NewTunnelStub(tsport Transport, serizer *serializer.Serializer) *TunnelStub {
	stub := TunnelStub{tsport: tsport, serizer: serizer}
	stub.streamch = make(chan *Stream, 1024)
	stub.sendch = make(chan *protocol.Frame, 1024)
	stub.streams = make(map[string]*Stream)
	go stub.readWorker()
	go stub.writeWorker()
	return &stub
}

func (ts *TunnelStub) SetSerializer(serizer *serializer.Serializer) {
	ts.serizer = serizer
}

func (ts *TunnelStub) sendTinyFrame(frame *protocol.Frame) error {
	// 序列化数据
	binaryData := ts.serizer.Serialize(frame)
	//ts.wlock.Lock()
	//defer ts.wlock.Unlock()
	// 发送数据
	//log.Printf("write tunnel cid:%s, data[%d]bytes, frame type:%d\n", frame.Cid, len(binaryData), frame.Type)
	return ts.tsport.SendPacket(binaryData)
}

func (ts *TunnelStub) sendDataFrame(streamId string, data []byte) {
	frame := &protocol.Frame{Cid: streamId, Type: protocol.STREAM_FRAME, Data: data}
	ts.sendch <- frame
}

func (ts *TunnelStub) sendFrame(frame *protocol.Frame) error {
	//fmt.Printf("send[%s] len:[%d] type:%d tunnel:%x\n", frame.Cid, len(frame.Data), frame.Type, frame.Data)
	binaryData := ts.serizer.Serialize(frame)
	//ts.wlock.Lock()
	//defer ts.wlock.Unlock()
	// 发送数据
	//log.Printf("write tunnel cid:%s, data[%d]bytes, frame type:%d\n", frame.Cid, len(binaryData), frame.Type)
	return ts.tsport.SendPacket(binaryData)
	//return nil
}

func (ts *TunnelStub) closeStream(streamId string) {
	ts.destroyStream(streamId)
	frame := &protocol.Frame{Cid: streamId, Type: protocol.FIN_FRAME, Data: []byte{0x1, 0x1}}
	ts.sendch <- frame
}

func (ts *TunnelStub) resetStream(streamId string) {
	ts.destroyStream(streamId)
	frame := &protocol.Frame{Cid: streamId, Type: protocol.RST_FRAME, Data: []byte{0x1, 0x2}}
	ts.sendch <- frame
}

func (ts *TunnelStub) writeWorker() {
	fmt.Println("writeWorker====")
	for {
		select {
		case ref := <-ts.sendch:
			ts.sendFrame(ref)
		case <-ts.close:
			return
		}
	}
}

func (ts *TunnelStub) readWorker() {
	fmt.Println("readworker====")
	defer func() {
		ts.close <- 1
	}()
	for {
		packet, err := ts.tsport.ReadPacket()
		//fmt.Printf("receive====:%x\n", packet)
		//fmt.Printf("transport read data:len:%d\n", len(packet))
		if err != nil {
			fmt.Println("transport read packet err;", err.Error())
			return
		}
		respFrame, err := ts.serizer.Derialize(packet)
		if err != nil {
			fmt.Errorf("protol error:%v\n", err)
			return
		}

		//log.Printf("read  tunnel cid:%s, data[%d]bytes, frame type:%d\n", respFrame.Cid, len(packet), respFrame.Type)
		if respFrame.Type == protocol.PING_FRAME {
			timebs := toolbox.GetNowInt64Bytes()
			data := append(respFrame.Data, timebs...)
			pongFrame := &protocol.Frame{Cid: respFrame.Cid, Type: protocol.PONG_FRAME, Data: data}
			ts.sendch <- pongFrame
		} else if respFrame.Type == protocol.PONG_FRAME {
			stByte := respFrame.Data[:13]
			atByte := respFrame.Data[13:26]
			nowst := time.Now().UnixNano() / 1e6
			st, err1 := strconv.Atoi(string(stByte))
			at, err2 := strconv.Atoi(string(atByte))
			if err1 != nil || err2 != nil {
				continue
			}
			upms := int64((at)) - int64(st)
			downms := nowst - int64(at)
			fmt.Printf("tunnel health！ up:%dms, down:%dms, rtt:%dms\n", upms, downms, nowst-int64(st))

		} else if respFrame.Type == protocol.INIT_FRAME {
			//fmt.Println("init stream ====")
			// create stream for server
			st := NewStream(respFrame.Cid, respFrame.Data, ts)
			ts.streams[st.Cid] = st
			ts.streamch <- st
		} else if respFrame.Type == protocol.STREAM_FRAME {
			// find stream , write stream
			streamId := respFrame.Cid
			stream := ts.streams[streamId]
			if stream == nil {
				ts.resetStream(streamId)
				continue
			}
			err := stream.produce(respFrame.Data)
			//fmt.Println("produce okok")
			if err != nil {
				fmt.Println(err)
				ts.closeStream(streamId)
			}
		} else if respFrame.Type == protocol.FIN_FRAME {
			ts.destroyStream(respFrame.Cid)
		} else if respFrame.Type == protocol.RST_FRAME {
			//destory stream
			ts.destroyStream(respFrame.Cid)
		} else if respFrame.Type == protocol.EST_FRAME {
			streamId := respFrame.Cid
			stream := ts.streams[streamId]
			if stream == nil {
				ts.resetStream(streamId)
				continue
			}
			ts.streamch <- stream
		} else {
			fmt.Println("eception frame type:", respFrame.Type)
		}
	}
}

func (ts *TunnelStub) InitStream(addr []byte) *Stream {
	//fmt.Println("start stream===>")
	streamId := utils.GetUUID()
	stream := NewStream(streamId, addr, ts)
	ts.streams[streamId] = stream
	frame := &protocol.Frame{Cid: streamId, Type: protocol.INIT_FRAME, Data: addr}
	ts.sendch <- frame
	return stream
}
func (ts *TunnelStub) SetReady(stream *Stream) {
	frame := &protocol.Frame{Cid: stream.Cid, Type: protocol.EST_FRAME, Data: stream.Addr}
	ts.sendch <- frame
}

func (ts *TunnelStub) destroyStream(streamId string) {
	stream := ts.streams[streamId]
	if stream != nil {
		stream.Close()
		delete(ts.streams, streamId)
	}
}

func (ts *TunnelStub) Ping() {
	data := toolbox.GetNowInt64Bytes()
	frame := &protocol.Frame{Cid: "00000000000000000000000000000000", Type: protocol.PING_FRAME, Data: data}
	ts.sendch <- frame
}

func (ts *TunnelStub) Accept() (*Stream, error) {
	//fmt.Println("acceept on stream===")

	select {
	case st := <-ts.streamch: // 收到stream
		return st, nil
	case <-ts.close:
		// close transport
		return nil, errors.New("transport closed")
	}
}
