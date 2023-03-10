package transport

import (
	"errors"
	"io"
	"log"
)

type Stream struct {
	Cid   string
	Addr  []byte
	wbuf  chan []byte
	rbuf  chan []byte
	buff  []byte
	ts    *TunnelStub
	state uint8
}

func NewStream(cid string, addr []byte, ts *TunnelStub) *Stream {
	s := &Stream{}
	s.Cid = cid
	s.Addr = addr
	s.ts = ts
	s.wbuf = make(chan []byte)
	s.rbuf = make(chan []byte)
	//s.buff =make([]byte 1024*8)
	s.state = 0
	//s.
	return s
}

//func (s *Stream) receiver() {
//	for {
//		select {
//		case data, ok := <-s.rbuf: // target write data=>stream=>transport
//			if !ok {
//				return
//			}
//
//		}
//	}
//}

func (s *Stream) isClose() bool {
	return s.state == 2
}

func (s *Stream) Read(data []byte) (n int, err error) {
	bts, ok := <-s.rbuf
	if !ok {
		return 0, io.EOF
	}
	if len(bts) > len(data) {
		log.Fatalln("overflow read from rbuf====>")
	}
	n = copy(data, bts)
	log.Println("browser read stream data:", n, "bts len:", len(bts))
	return n, nil
}

func (s *Stream) Write(p []byte) (n int, err error) {
	if s.state == 2 {
		return 0, errors.New("cannot write, stream closed")
	}
	s.ts.sendDataFrame(s.Cid, p)
	return len(p), nil
}

func (s *Stream) Close() error {
	log.Println("closeing ch")
	if s.state == 2 {
		return nil
	}
	s.state = 2
	close(s.wbuf)
	close(s.rbuf)
	return nil
}

func SocketPipe(src io.Reader, dest io.Writer) {
	// func Copy(dst Writer, src Reader), src->pipe->dest
	_, err := io.Copy(dest, src)
	if err != nil {
		log.Println("copy ==> err:", err.Error())
	}
}
