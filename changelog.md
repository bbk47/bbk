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