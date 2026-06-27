---
name: proxy-stability
description: Run bbk proxy stability tests covering TCP and UDP (SOCKS5 UDP ASSOCIATE). Use when testing the bbk proxy (client/server), debugging UDP relay instability, validating tunnel reconnection, or checking mux/CFB stream integrity and head-of-line latency — locally over ws/tcp tunnels.
---

# bbk 代理稳定性测试

测试 `bbk` 代理（Client→Server 两端拓扑）的 TCP/UDP 稳定性。
所有脚本在 `tests/` 下，产物在 `tests/results/`（已 gitignore）。主流程免 root。

## 核心认知

`bbk` 是 L4 转发，不改写浏览器 TLS。隧道协议支持 `ws/tcp/tls/h2`，
本地免证书自动化用 `ws`(默认) 与 `tcp`。常见问题定位：

| 现象 | 根因 | 用例 |
|---|---|---|
| 数据损坏/隧道频繁重连 | 共享 CFB 流 / mux 并发 bug | `60` |
| 小请求慢、子请求超时 | 单 writeWorker 队头阻塞 + 吞吐低 | `70` |
| UDP 通路异常 | `src/proxy/udprelay.go` 不稳(不分片/64KB/丢包) | `20` |
| 全部失败 | 隧道本身不稳 | `30` |

## 测试分层

- **Go 单元/集成测试**：`src/protocol`、`src/serializer`、`src/stub`(mux)、`src/proxy`(udprelay)，跑 `go test ./src/...`（秒级，免起两端）。盲区：`src/Client.go` 重连、`transport/server/utils` 暂无单测，无 CI。
- **shell 冒烟/e2e**：本目录 `tests/`（`10/20` 冒烟，`30/60/70` e2e），即下方工作流。

## 工作流（免 root）

```
- [ ] 0. Go 单元/集成回归 (go test ./src/...)
- [ ] 1. 构建并起本地两端 + 预热
- [ ] 2. TCP/UDP 冒烟
- [ ] 3. 隧道并发+断线重连压测
- [ ] 4. 完整性 / HoL
- [ ] 5. 按结果判读定位
```

**0. Go 回归**（仓库根目录，免起两端）
```bash
go test ./src/...
```

**1. 起本地两端**（PROTO=ws 或 tcp）+ 预热（避免冷启动假性失败）
```bash
cd tests && ./00_build.sh && PROTO=ws ./01_up_local.sh && sleep 3
```
> 缺同级 `../toolbox` 时 `00_build.sh` 会失败，可直接复用已有的 `tests/bin/bbk`。

**2. 冒烟**
```bash
./10_smoke_tcp.sh      # SOCKS5/TCP H1/H2
./20_udp_dns.sh        # SOCKS5/UDP DNS -> udprelay 最小用例
./25_udp_echo.sh       # SOCKS5/UDP 端到端: 大包/并发/多目标NAT/分片丢弃
```

**3. 稳定性压测**（阶段A并发 / B杀server重连 / C复压）
```bash
CONC=20 ROUNDS=10 ./30_tunnel_stability.sh
```

**4. 完整性 / HoL**（数据通路定位）
```bash
CONC=300 BYTES=4194304 ./60_stream_integrity.sh    # 逐字节校验+查重连/解密错误
LOAD=6 SAMPLES=40 ./70_hol_latency.sh              # 队头阻塞放大
```

**5. 收尾**
```bash
./02_down_local.sh
```

## 判读

- `20`/`25` UDP 失败 → 查 `src/proxy/udprelay.go`（分片、大包、会话回收、NAT 会话表）。`25` 的并发用例是 UDP best-effort(阈值 80%)，大包/完整性由确定性的 sizes-sequential 保证。
- `60` 损坏/重连 → 查共享 CFB 流/mux 并发（`src/serializer/`, `src/stub/`）。
- `70` HoL 大 → 单 `writeWorker` 串行 + 逐帧 CFB 瓶颈（`src/stub/stub.go`）。
- `30` 重连不恢复 → 查 `src/Client.go`。

## 注意事项

- 冷启动：`01` 只等本地 socks 端口，隧道是并发建的；起完立刻压会假性 0%，先 `sleep 3` 或先跑 `10` 预热。
- `30` 阶段B 仅记录不断言（即使没真断链也会“绿”），以汇总文本为准；`code=000000` 即“非 200”。

## UDP 流量代理（SOCKS5 UDP ASSOCIATE）

shadowsocks 风格、不支持分片：client 开临时 UDP 中继端口，app 数据报剥 `RSV+FRAG`
后以 `[len(2)][socks5addr+payload]` 写入复用的 mux stream（哨兵地址 `0xFD 'U''D''P'`
标记 UDP 关联流）；server 侧 `ServeUDP` 维护 `net.UDPConn` NAT 会话表收发，60s 空闲回收。
`FRAG!=0` 丢弃，单记录上限 64KB。代码：`src/proxy/udprelay.go`、`src/proxy/socks5.go`、
`src/Client.go`(bindProxySocket)、`src/Server.go`(handleStream)。

完整说明见 [tests/README.md](../../../tests/README.md)。
