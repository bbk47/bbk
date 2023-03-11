package transport

import (
	"io"
	"log"
	"sync"
)

type Stream struct {
	Cid  string
	Addr []byte
	ts   *TunnelStub
	rp   *io.PipeReader
	wp   *io.PipeWriter
}

func NewStream(cid string, addr []byte, ts *TunnelStub) *Stream {
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
	//fmt.Println("produce has err:", err != nil, "write count:", n)

	return err
}

func (s *Stream) Read(data []byte) (n int, err error) {
	n, err = s.rp.Read(data)
	//fmt.Printf("target read====:%x  len:%d\n", data[:n], n)
	return n, err
}

func (s *Stream) Write(p []byte) (n int, err error) {
	//fmt.Printf("write stream[%s] data:%x\n", s.Cid, p)
	buf2 := make([]byte, len(p))
	copy(buf2, p) // io.Copy buf must copy data
	s.ts.sendDataFrame(s.Cid, buf2)
	return len(p), nil
}

func (s *Stream) Close() error {
	log.Println("closeing ch")
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
