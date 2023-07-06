package stub

import (
	"io"
	"sync"
)

type Stream struct {
	Cid string
	ts  *TunnelStub
	rp  *io.PipeReader
	wp  *io.PipeWriter
}

func NewStream(cid string, ts *TunnelStub) *Stream {
	s := &Stream{}
	s.Cid = cid
	s.ts = ts
	rp, wp := io.Pipe()
	s.rp = rp
	s.wp = wp
	return s
}

func (s *Stream) produce(data []byte) error {
	//fmt.Printf("produce wp====:%s,%v\n", s.Cid, data)
	_, err := s.wp.Write(data)
	return err
}

func (s *Stream) Read(data []byte) (n int, err error) {
	n, err = s.rp.Read(data)
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

func Relay(left, right io.ReadWriteCloser) error {
	var err, err1 error
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err1 = io.Copy(right, left)
	}()
	_, err = io.Copy(left, right)
	wg.Wait()

	if err != nil {
		return err
	}

	if err1 != nil {
		return err1
	}
	return nil
}
