package protocol

import "github.com/bbk47/toolbox"

const (
	INIT_FRAME   uint8 = 0x0
	STREAM_FRAME uint8 = 0x1
	FIN_FRAME    uint8 = 0x2
	RST_FRAME    uint8 = 0x3
	EST_FRAME    uint8 = 0x4
	// ping pong
	PING_FRAME uint8 = 0x6
	PONG_FRAME uint8 = 0x9
)

const DATA_MAX_SIZE = 1024 * 2

/**
 *
 * // required: cid, type,  data
 * @param {*} frame
 * |<-----mask(random)----->|<-version[1]->|<--type[1]-->|<---cid--->|<-------data------>|
 * |---------2--------------|------ 1 -----|-------1-----|-----4-----|-------------------|
 * @returns
 */

type Frame struct {
	Version uint8
	Type    uint8
	Cid     uint32
	Data    []byte
	Stime   int
	Atime   int
}

func Encode(frame *Frame) []byte {
	if frame.Version == 0 {
		frame.Version = 1
	}
	randbs := toolbox.GetRandByte(2)
	cid := frame.Cid
	ret1 := []byte{randbs[0], randbs[1], frame.Version, frame.Type}
	cidBuf := []byte{byte(cid >> 24), byte(cid >> 16), byte(cid >> 8), byte(cid & 0xff)} // s3
	ret2 := append(ret1, cidBuf...)                                                      // s1+s2+s3
	ret3 := append(ret2, frame.Data...)                                                  // +s6
	return ret3
}

func Decode(binaryDt []byte) (frame *Frame, err error) {
	binaryDt = binaryDt[2:]
	ver := binaryDt[0]     // s1
	typeVal := binaryDt[1] // s2
	cid := uint32(binaryDt[2])<<24 | uint32(binaryDt[3])<<16 | uint32(binaryDt[4])<<8 | uint32(binaryDt[5])
	dataBuf := binaryDt[6:] // s6
	frame1 := Frame{Version: ver, Cid: cid, Type: typeVal, Data: dataBuf}

	return &frame1, nil
}

func FrameSegment(frame *Frame) []*Frame {
	var frames []*Frame
	leng := 0
	if frame.Data != nil {
		leng = len(frame.Data)
	}

	if leng <= DATA_MAX_SIZE {
		frames = append(frames, frame)
	} else {
		offset := 0
		ldata := frame.Data
		for {
			offset2 := offset + DATA_MAX_SIZE
			if offset2 > leng {
				offset2 = leng
			}
			buf2 := make([]byte, offset2-offset)
			// 多个切片共享底层数组：当多个切片共享同一个底层数组时，修改其中一个切片的值可能会影响其他切片的值。
			copy(buf2, ldata[offset:offset2])
			frame2 := Frame{Cid: frame.Cid, Type: frame.Type, Data: buf2}
			frames = append(frames, &frame2)
			offset = offset2
			if offset2 == leng {
				break
			}
		}
	}
	return frames
}
