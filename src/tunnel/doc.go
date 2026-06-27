// Package tunnel 提供基于 hashicorp/yamux 的隧道多路复用层，替代自研的
// protocol/serializer/stub 三件套。分层职责：
//
//	SecureConn  —— 在裸载体(io.ReadWriteCloser)之上做整条连接的流式加密
//	              (每条连接随机 IV + 连续 cipher.Stream，取代逐帧固定 IV 的 CFB)。
//	Session     —— 封装 yamux.Session，并补上 yamux 不负责的"流级握手"
//	              (目标地址 + 连接就绪确认，取代旧的 INIT/EST 帧)。
//	Stream      —— 封装 yamux.Stream，补上 Addr 与 CloseWrite 半关闭语义。
//
// 载体(tcp/tls/h2/ws)只负责产出一条有序字节流，分帧/复用/流控全部交给 yamux。
package tunnel
