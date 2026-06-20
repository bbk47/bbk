package stub

import (
	"github.com/bbk47/bbk/v3/src/protocol"
	"github.com/bbk47/toolbox/mux"
)

// Stream 复用 toolbox/mux 的通用多路复用流，仅补上 bbk 特有的 uint32 类型 cid。
type Stream struct {
	*mux.Stream
	Cid uint32
}

func NewStream(cid uint32, addr []byte, ts *TunnelStub) *Stream {
	s := &Stream{Cid: cid}
	s.Stream = mux.NewStream(addr, mux.Config{
		WindowSize: mux.DefaultWindowSize,
		MaxChunk:   protocol.DATA_MAX_SIZE,
		SendData:   func(b []byte) { ts.sendDataFrame(cid, b) },
		SendFin:    func() { ts.sendFinFrame(cid) },
		OnClose:    func() { ts.syncDelStream(cid) },
	})
	return s
}
