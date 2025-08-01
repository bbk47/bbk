package stub

import (
	"io"
	"sync"
)

type Stream struct {
	Cid                uint32
	Addr               []byte
	ts                 *TunnelStub
	rp                 *io.PipeReader
	wp                 *io.PipeWriter
	windowSize         uint32
	sentBytes          uint32
	ackBytes           uint32
	pendingData        [][]byte
	sendWindowUpdateFn func(uint32)
	mu                 sync.Mutex
}

func NewStream(cid uint32, addr []byte, ts *TunnelStub) *Stream {
	s := &Stream{
		Cid:        cid,
		Addr:       addr,
		ts:         ts,
		windowSize: 32 * 1024, // 32KB
	}
	s.rp, s.wp = io.Pipe()
	return s
}

func (s *Stream) produce(data []byte) error {
	_, err := s.wp.Write(data)
	return err
}

func (s *Stream) Read(p []byte) (int, error) {
	n, err := s.rp.Read(p)
	if err == io.ErrClosedPipe {
		return 0, io.EOF
	}
	if n > 0 && s.sendWindowUpdateFn != nil {
		s.sendWindowUpdateFn(uint32(n)) // 调用窗口更新函数，通知可以发送更多数据
	}
	return n, err
}

func (s *Stream) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 如果已用窗口大于限制，缓存数据
	if s.sentBytes-s.ackBytes >= s.windowSize {
		// 缓存数据
		buf := make([]byte, len(p))
		copy(buf, p)
		s.pendingData = append(s.pendingData, buf)
		return len(p), nil
	}

	// 否则立即发送
	s.sentBytes += uint32(len(p))
	buf2 := make([]byte, len(p))
	copy(buf2, p)
	s.ts.sendDataFrame(s.Cid, buf2)
	return len(p), nil
}

// 处理窗口更新帧（即确认数据被对方接收了）
func (s *Stream) HandleWindowUpdate(n uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.ackBytes += n

	// 尝试从 pending 队列中发送数据
	for len(s.pendingData) > 0 && s.sentBytes-s.ackBytes < s.windowSize {
		chunk := s.pendingData[0]
		s.pendingData = s.pendingData[1:]

		s.sentBytes += uint32(len(chunk))
		s.ts.sendDataFrame(s.Cid, chunk)
	}
}

func (s *Stream) Close() error {
	s.rp.Close()
	s.wp.Close()
	return nil
}

// 注册窗口更新回调
func (s *Stream) SetSendWindowUpdateFn(fn func(n uint32)) {
	s.sendWindowUpdateFn = fn
}
