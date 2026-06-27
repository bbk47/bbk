## 4.0.0

- refactor!: replace the in-house mux (stub/protocol + toolbox-mux, per-frame fixed-IV CFB) with hashicorp/yamux
- feat: stream-wide encryption (src/tunnel SecureConn) — per-connection random IV + continuous cipher.Stream, removing the fixed-IV keystream reuse
- feat: src/tunnel layer — yamux Session + stream-level handshake (addr/ready) replacing INIT/EST frames, WebSocket byte-stream adapter, CloseWrite half-close mapping
- refactor: transports reduced to raw io.ReadWriteCloser dialers (drop SendPacket/ReadPacket framing)
- refactor: simplify Client — drop reqch/browserProxy/listenStream/serviceWorker/keepPingWs; OpenStream is synchronous; keepalive handled by yamux; on-demand reconnect via session state
- chore!: bump Go toolchain to 1.21 (CI 1.19 -> 1.21); add github.com/hashicorp/yamux
- fix: option.go ListenHttpPort json tag collided with ListenPort
- remove: src/{protocol,serializer,stub}, old packetized transports, dead const.go, orphan src/common/websocket
- BREAKING: wire protocol changed; not compatible with bbk v3 or bbkjs. JS port (bbkjs/abcjs) to follow as a separate task.

## 3.5.0

- feat: support UDP traffic proxy via SOCKS5 UDP ASSOCIATE (shadowsocks-style, no fragmentation)
- fix: guard browserProxy map access in Client.listenStream (concurrent map read/write crash under load)
- test: add udprelay unit/integration tests + tests/ stability suite (TCP/UDP smoke, integrity, HoL, reconnect)
- test: fix stale frame_test length assertions (8-byte header)
- docs: add proxy-stability skill

## 3.4.0

- feat: reuse toolbox/mux, support stream half-close
- fix: stub close/race condition

## 3.0.1

- fix: close connection exception
- cli: github actions for auto build

## 3.0.0

- setup stub worker over transport
- switch workmode to sesison/stream mode
- change go module name

## 2.0.0

- support http2/tls/ws/tcp transport