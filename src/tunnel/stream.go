package tunnel

import "github.com/hashicorp/yamux"

const (
	statusOK   byte = 0x00 // 目标连接已就绪（取代旧的 EST 帧）
	statusFail byte = 0x01 // 目标连接失败
)

// Stream 封装 yamux.Stream，补上 bbk 需要的目标地址与半关闭语义。
type Stream struct {
	*yamux.Stream
	// Addr 是该流的目标地址（socks5 地址字节，或 UDP 关联哨兵），
	// 由流级握手在建立时传递，取代旧协议的 INIT 帧负载。
	Addr []byte
}

// Cid 返回流 ID（沿用旧 stub.Stream.Cid 的语义，便于日志/调用方过渡）。
func (s *Stream) Cid() uint32 {
	return s.Stream.StreamID()
}

// SetReady 由 server 端在目标连接就绪后调用，回送 1 字节就绪状态（取代 EST 帧）。
func (s *Stream) SetReady() error {
	_, err := s.Stream.Write([]byte{statusOK})
	return err
}

// CloseWrite 关闭本端写端：发送 FIN 通知对端，本端读端仍可继续读，实现半关闭。
// yamux 的 Stream.Close() 语义恰为"发 FIN 进入 localClose，读端正常工作"
// （见 yamux stream.go: streamLocalClose 只禁写不禁读），故直接映射。
// 该方法使 *Stream 满足 utils.Relay 的 halfCloser 接口。
func (s *Stream) CloseWrite() error {
	return s.Stream.Close()
}
