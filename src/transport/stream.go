package transport

import (
	"errors"
	"fmt"
	"io"
)

type Stream struct {
	Cid   string
	Addr  []byte
	wbuf  chan []byte
	rbuf  chan []byte
	state uint8
}

func NewStream(cid string, addr []byte) *Stream {
	s := &Stream{}
	s.Cid = cid
	s.Addr = addr
	s.wbuf = make(chan []byte, 1024)
	s.rbuf = make(chan []byte, 1024)
	s.state = 0
	return s
}

func (s *Stream) produce(data []byte) error {
	if s.state == 2 {
		return errors.New("stream closed")
	}
	s.rbuf <- data
	return nil
}

func (s *Stream) Read(data []byte) (n int, err error) {
	bts, ok := <-s.rbuf
	if !ok {
		return 0, errors.New("stream closed")
	}
	n = copy(data, bts)
	return n, nil
}

func (s *Stream) Write(p []byte) (n int, err error) {
	fmt.Println("write to stream===", len(p))
	s.wbuf <- p
	return len(p), nil
}

func (s *Stream) Close() error {
	s.state = 2
	//close(s.wbuf)
	//close(s.rbuf)
	return nil
}

func SocketPipe(src io.ReadCloser, dest io.WriteCloser) {
	defer src.Close()
	defer dest.Close()
	// func Copy(dst Writer, src Reader), src->pipe->dest
	_, err := io.Copy(dest, src)
	if err != nil {
		fmt.Println("err:", err.Error())
	}
	return
}
