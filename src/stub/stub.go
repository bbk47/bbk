package stub

import (
	"errors"
	"fmt"
	"gitee.com/bbk47/bbk/v3/src/protocol"
	"gitee.com/bbk47/bbk/v3/src/serializer"
	"gitee.com/bbk47/bbk/v3/src/transport"
	"gitee.com/bbk47/bbk/v3/src/utils"
	"github.com/bbk47/toolbox"
	"strconv"
	"time"
)

type TunnelStub struct {
	serizer  *serializer.Serializer
	tsport   transport.Transport
	streams  map[string]*Stream
	streamch chan *Stream
	sendch   chan *protocol.Frame
	closech  chan uint8
	//wlock    sync.Mutex
	pongFunc func(up, down int64)
}

func NewTunnelStub(tsport transport.Transport, serizer *serializer.Serializer) *TunnelStub {
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

func (ts *TunnelStub) NotifyPong(handler func(up, down int64)) {
	ts.pongFunc = handler
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
	frames := protocol.FrameSegment(frame)
	for _, smallframe := range frames {
		err := ts.sendTinyFrame(smallframe)
		if err != nil {
			return err
		}
	}
	return nil
}

func (ts *TunnelStub) closeStream(streamId string) {
	ts.destroyStream(streamId)
	frame := &protocol.Frame{Cid: streamId, Type: protocol.FIN_FRAME, Data: []byte{0x1, 0x1}}
	ts.sendch <- frame
}

func (ts *TunnelStub) produceData(stream *Stream, frame *protocol.Frame) {
	err := stream.produce(frame.Data)
	if err != nil {
		fmt.Println("produce err:", err)
		ts.closeStream(stream.Cid)
	}
}

func (ts *TunnelStub) writeWorker() {
	//fmt.Println("writeWorker====")
	for {
		select {
		case ref := <-ts.sendch:
			ts.sendFrame(ref)
		case <-ts.closech:
			return
		}
	}
}

func (ts *TunnelStub) readWorker() {
	fmt.Println("readworker====")
	defer func() {
		ts.closech <- 1
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
			if ts.pongFunc == nil {
				continue
			}
			ts.pongFunc(upms, downms)
		} else if respFrame.Type == protocol.STREAM_FRAME {
			// find stream , write stream
			streamId := respFrame.Cid
			stream := ts.streams[streamId]
			if stream == nil {
				st := NewStream(respFrame.Cid, ts)
				ts.streams[st.Cid] = st
				ts.streamch <- st
				stream = st
			}
			ts.produceData(stream, respFrame)
		} else if respFrame.Type == protocol.FIN_FRAME {
			ts.destroyStream(respFrame.Cid)
		} else if respFrame.Type == protocol.RST_FRAME {
			ts.destroyStream(respFrame.Cid)
		} else {
			fmt.Println("eception frame type:", respFrame.Type)
		}
	}
}

func (ts *TunnelStub) CreateStream(streamId string) *Stream {
	if streamId == "" {
		streamId = utils.GetUUID()
	}
	stream := NewStream(streamId, ts)
	ts.streams[streamId] = stream
	return stream
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
	case <-ts.closech:
		// close transport
		return nil, errors.New("transport closed")
	}
}
