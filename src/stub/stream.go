package stub

import (
	"io"
)

type Stream struct {
	Cid  uint32
	Addr []byte
	ts   *TunnelStub
	rp   *io.PipeReader
	wp   *io.PipeWriter
}

func NewStream(cid uint32, addr []byte, ts *TunnelStub) *Stream {
	s := &Stream{}
	s.Cid = cid
	s.Addr = addr
	s.ts = ts
	rp, wp := io.Pipe()
	s.rp = rp
	s.wp = wp
	return s
}

func (s *Stream) produce(data []byte) error {
	//fmt.Printf("produce wp====:%x\n", data)
	_, err := s.wp.Write(data)
	return err
}

func (s *Stream) Read(data []byte) (n int, err error) {
	n, err = s.rp.Read(data)
	if err == io.ErrClosedPipe {
		// ErrClosedPipe emit only stream.Close()  called
		//fmt.Println("ErrClosedPipe received=====")
		return 0, io.EOF
	}
	//fmt.Printf("target read====:%x  len:%d\n", data[:n], n)
	return n, err
}

func (s *Stream) Write(p []byte) (n int, err error) {
	//fmt.Printf("write stream[%s] data:%x\n", s.Cid, p)
	buf2 := make([]byte, len(p))
	// go中使用io.Copy时，底层使用slice作为buffer cache,传入的p一直是同一个切片, 实现的目标 Writer 不能及时消费写入的数据，会导致数据覆盖
	copy(buf2, p) // io.Copy buf must copy data
	s.ts.sendDataFrame(s.Cid, buf2)
	return len(p), nil
}

func (s *Stream) Close() error {
	//log.Println("closeing ch")
	s.rp.Close()
	s.wp.Close()
	return nil
}
