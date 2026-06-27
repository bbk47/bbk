package protocol

import (
	"fmt"
	"github.com/bbk47/toolbox"
	"log"
	"testing"
)

func TestFrameStatic(t *testing.T) {

	frame1 := Frame{Cid: 172738123, Type: 1, Data: []byte{0x1, 0x2, 0x3, 0x4}}
	result := Encode(&frame1)
	fmt.Println(len(result))
	// 头部 = mask(2)+version(1)+type(1)+cid(4) = 8 字节，加 4 字节负载 = 12
	if len(result) != 8+4 {
		t.Errorf("test derialize failed! assert len=12!")
	}
	log.Println(toolbox.GetBytesHex(result))

	frame2, err := Decode(result)
	if err != nil {
		t.Error(err)
	}

	if frame2.Cid != frame1.Cid || frame2.Type != frame1.Type || toolbox.GetBytesHex(frame2.Data) != toolbox.GetBytesHex(frame1.Data) {
		t.Errorf("test derialize failed!")
	}
}

func TestFrameType(t *testing.T) {

	frame := Frame{Cid: 12123, Type: 2, Data: []byte{0x1, 0x2, 0x3, 0x4}}
	result := Encode(&frame)

	frame2, err := Decode(result)
	if err != nil {
		t.Error(err)
	}

	if frame2.Cid == frame.Cid && frame2.Type == frame.Type {
		// success
	} else {
		t.Errorf("test derialize failed!")
	}
}

func TestFrameDynamicData(t *testing.T) {

	randata := toolbox.GetRandByte(20)
	frame := Frame{Cid: 12128, Type: 1, Data: randata}
	result := Encode(&frame)
	// 头部 8 字节 + 20 字节负载 = 28
	if len(result) != 8+20 {
		t.Errorf("test derialize failed! assert len=8+20!")
	}
	frame2, err := Decode(result)
	if err != nil {
		t.Error(err)
	}

	if frame2.Cid == frame.Cid && frame2.Type == frame.Type && toolbox.GetBytesHex(frame2.Data) == toolbox.GetBytesHex(randata) {
		// success
	} else {
		t.Errorf("test derialize failed!")
	}
}
