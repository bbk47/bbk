package stub

import (
	"errors"
	"fmt"
	"gitee.com/bbk47/bbk/v3/src/protocol"
	"gitee.com/bbk47/bbk/v3/src/serializer"
	"gitee.com/bbk47/bbk/v3/src/transport"
	"github.com/bbk47/toolbox"
	"strconv"
	"time"
)

type TunnelStub struct {
	serizer  *serializer.Serializer
	tsport   transport.Transport
	streams  map[uint32]*Stream
	streamch chan *Stream
	sendch   chan *protocol.Frame
	closech  chan uint8
	seq      uint32
	//wlock    sync.Mutex
	pongFunc func(up, down int64)
}

func NewTunnelStub(tsport transport.Transport, serizer *serializer.Serializer) *TunnelStub {
	stub := TunnelStub{tsport: tsport, serizer: serizer}
	stub.streamch = make(chan *Stream, 1024)
	stub.sendch = make(chan *protocol.Frame, 1024)
	stub.streams = make(map[uint32]*Stream)
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
	//log.Printf("write tunnel cid:%d, data[%d]bytes, frame type:%d\n", frame.Cid, len(binaryData), frame.Type)
	return ts.tsport.SendPacket(binaryData)
}

func (ts *TunnelStub) sendDataFrame(streamId uint32, data []byte) {
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

func (ts *TunnelStub) closeStream(streamId uint32) {
	ts.destroyStream(streamId)
	frame := &protocol.Frame{Cid: streamId, Type: protocol.FIN_FRAME, Data: []byte{0x1, 0x1}}
	ts.sendch <- frame
}

func (ts *TunnelStub) resetStream(streamId uint32) {
	ts.destroyStream(streamId)
	frame := &protocol.Frame{Cid: streamId, Type: protocol.RST_FRAME, Data: []byte{0x1, 0x2}}
	ts.sendch <- frame
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

		//log.Printf("read  tunnel cid:%d, data[%d]bytes, frame type:%d\n", respFrame.Cid, len(packet), respFrame.Type)
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
				fmt.Println("produce err:", err)
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

func (ts *TunnelStub) StartStream(addr []byte) *Stream {
	//fmt.Println("start stream===>")
	ts.seq = ts.seq + 1
	if (ts.seq ^ 0x7fffffff) == 0 {
		ts.seq = 1
	}
	streamId := ts.seq
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

func (ts *TunnelStub) destroyStream(streamId uint32) {
	stream := ts.streams[streamId]
	if stream != nil {
		stream.Close()
		delete(ts.streams, streamId)
	}
}

func (ts *TunnelStub) Ping() {
	data := toolbox.GetNowInt64Bytes()
	frame := &protocol.Frame{Cid: 0, Type: protocol.PING_FRAME, Data: data}
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
