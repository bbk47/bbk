package protocol

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
 * |<-version[1]->|<--cidLen[1]-->|<---(cid)---->|<--type[1]-->|<--dataLen[2]-->|<-------data------>|
 * |-----s1 ------|-------s2------|-----s3 ------|-------s4----|-------s5 ------|--------s6---------|
 * @returns
 */

type Frame struct {
	Version uint8
	Cid     string
	Type    uint8
	Data    []byte
	Stime   int
	Atime   int
}

func Encode(frame *Frame) []byte {
	if frame.Version == 0 {
		frame.Version = 1
	}
	cidlen := len(frame.Cid)
	datalen := len(frame.Data)
	ret1 := []byte{frame.Version, uint8(cidlen)} // s1,s2
	cidBuf := []byte(frame.Cid)                  // s3
	ret2 := append(ret1, cidBuf...)              // s1+s2+s3
	typeBuf := []byte{frame.Type}
	ret3 := append(ret2, typeBuf[0])                             // s1+s2+s3+s4
	ret4 := append(ret3, uint8(datalen>>8), uint8(datalen&0xff)) // +s5
	ret5 := append(ret4, frame.Data...)                          // +s6
	return ret5
}

func Decode(binaryDt []byte) (frame *Frame, err error) {
	ver := binaryDt[0]                                               // s1
	cidlen := binaryDt[1]                                            // s2
	cid := string(binaryDt[2 : cidlen+2])                            // s3
	typeVal := binaryDt[cidlen+2]                                    // s4
	datalen := int(binaryDt[cidlen+3])*256 + int(binaryDt[cidlen+4]) //s5
	startIndex := int(cidlen + 5)
	dataBuf := binaryDt[startIndex : datalen+startIndex] // s6
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
