package serializer

import (
	"bbk/src/protocol"
	"fmt"
	"github.com/bbk47/toolbox"
)

type Serializer struct {
	encryptor *toolbox.Encryptor
}

func NewSerializer(method, password string) (ss *Serializer, err error) {
	ss = &Serializer{}

	fmt.Printf("method:%s,password:%s\n", method, password)

	encryptWorker, err := toolbox.NewEncryptor(method, password)
	if err != nil {
		return nil, err
	}
	ss.encryptor = encryptWorker
	return ss, nil
}

func (ss Serializer) Serialize(frame *protocol.Frame) []byte {
	dataBytes := protocol.Encode(frame)
	return ss.encryptor.Encrypt(dataBytes)
}

func (ss Serializer) Derialize(data []byte) (frame2 *protocol.Frame, err error) {
	buf, err := ss.encryptor.Decrypt(data)
	if err != nil {
		return nil, err
	}
	frame, err := protocol.Decode(buf)
	if err != nil {
		return nil, err
	}
	return frame, nil
}
