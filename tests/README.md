# bbk 代理稳定性测试套件

分层测试 `bbk` 代理（Client→Server 两端拓扑）的稳定性，覆盖 **TCP / UDP**。

> bbk 是 2 端拓扑：本地 `client`（SOCKS5/HTTP 代理）经隧道直连 `server`，
> 无 broker / agent。隧道协议支持 `ws / tcp / tls / h2`，本地免依赖证书的
> 自动化用例使用 `ws`（默认）和 `tcp`。

## 测试分层

| 层级 | 位置 | 内容 | 跑法 |
|---|---|---|---|
| 单元测试 | `src/protocol/`、`src/proxy/udprelay_test.go`(部分) | 帧编解码、SOCKS5/UDP 报文等纯函数 | `go test ./src/...` |
| 集成测试(进程内) | `src/stub/mux_test.go`、`src/proxy/udprelay_test.go`(部分) | mux 多流/半关闭、UDP 回环 | `go test ./src/...` |
| 冒烟测试 | `tests/10_smoke_tcp.sh`、`tests/20_udp_dns.sh` | 经 SOCKS5 单次 H1/H2/UDP DNS | 见下方主流程 |
| e2e / 系统测试 | `tests/25/30/60/70` | UDP 端到端多场景 + 并发压测/完整性/HoL | 见下方主流程 |

**先跑 Go 回归（秒级，无需起两端）：**

```bash
go test ./src/...        # 仓库根目录执行
```

> 覆盖盲区（暂未编写）：`src/Client.go`(重连/退避状态机)、`src/transport/`、
> `src/server/`、`src/utils/` 尚无单元测试；项目也未配置 CI 自动触发。

## 目录

```
tests/
  configs/local/        本地两端配置(ws + tcp)
  lib/common.sh         通用函数（起停/等端口/socks 探测）
  lib/udpdns_probe.go   SOCKS5 UDP ASSOCIATE DNS 探针
  lib/udpecho_probe.go  UDP 端到端多场景探针(大包/并发/多目标/分片丢弃)
  lib/streamintegrity.go 并发多流逐字节完整性探针
  lib/holtest.go        队头阻塞延迟探针
  00_build.sh           编译 -> bin/bbk
  01_up_local.sh        起本地两端 (PROTO=ws|tcp)
  02_down_local.sh      停本地两端
  10_smoke_tcp.sh       SOCKS5/TCP 冒烟 (H1/H2)
  20_udp_dns.sh         SOCKS5/UDP DNS 通路 (验证 udprelay 最小用例)
  25_udp_echo.sh        SOCKS5/UDP 端到端 (小包/近64KB大包/并发/多目标NAT/分片丢弃)
  30_tunnel_stability.sh 并发压测 + 断线重连
  60_stream_integrity.sh 并发多流逐字节完整性 (查 cipher/mux 损坏)
  70_hol_latency.sh     队头阻塞延迟 (空闲 vs 大流量下小请求 RTT)
  results/              运行产物(日志/汇总, 已 gitignore)
```

## 依赖

- 必需：`go`、`curl`、`bash`
- UDP DNS / 完整性 / HoL 探针：`go`（`go run` 即可，仅依赖标准库）

## 本地一键流程（SOCKS5/TCP/UDP）

```bash
cd tests
./00_build.sh
PROTO=ws ./01_up_local.sh      # 或 PROTO=tcp
sleep 3                        # 预热隧道，避免冷启动假性失败（见“注意事项”）
./10_smoke_tcp.sh
./20_udp_dns.sh                 # UDP 最小用例 (DNS)
./25_udp_echo.sh               # UDP 端到端多场景 (大包/并发/多目标/分片)
CONC=20 ROUNDS=10 ./30_tunnel_stability.sh
./02_down_local.sh
```

## 完整性 / HoL（数据通路定位三件套之二）

```bash
cd tests && ./01_up_local.sh
CONC=300 BYTES=4194304 ./60_stream_integrity.sh    # 逐字节校验 + 查重连/解密错误
LOAD=6 SAMPLES=40 ./70_hol_latency.sh              # 队头阻塞放大倍数
./02_down_local.sh
```

## 结果判读速查

- `20`/`25`(UDP) 失败 → 查 `src/proxy/udprelay.go`（分片/大包/会话回收/NAT 会话表）。
  `25` 的 `concurrent` 用例是 UDP best-effort，断言投递率 >= 80%（已收到的包逐字节校验）；
  大包与完整性由确定性的 `sizes-sequential` 用例（含 60KB）保证。突发丢包是 UDP 固有特性，非中继 bug。
- `60` 出现损坏/重连 → 查共享 CFB 流 / mux 并发（`src/serializer/serializer.go`, `src/stub/`）。
- `70` HoL 放大大 → 单 `writeWorker` 串行 + 逐帧 CFB 是瓶颈（`src/stub/stub.go writeWorker`）。
- `30` 断线重连不恢复 → 查 `src/Client.go` 重连逻辑。

## 注意事项（影响结果可靠性）

- **冷启动假性失败**：`01_up_local.sh` 只 `wait_port` 了本地 socks 监听端口，
  client→server 隧道是并发建立的。起完两端立刻压（如 `30` 阶段A）可能出现假性
  `0%`；**先 `sleep 3` 或先跑一次 `10` 预热**后再压。
- **`30` 阶段B 仅记录不断言**：中断窗口请求与恢复结果只 `tee` 进汇总、不参与
  PASS/FAIL 判定。中断期间打印的 `code=000000` 等价于“非 200”。
- **缺 `../toolbox` 时 `00_build` 会失败**：`go.mod` 把 `github.com/bbk47/toolbox`
  replace 到同级 `../toolbox`。若该目录缺失，`00_build.sh` 直接报错；此时可直接
  复用已构建好的 `tests/bin/bbk`（`go run` 的探针仅依赖标准库，不受影响）。

## UDP 流量代理说明

bbk 的 UDP 走 SOCKS5 `UDP ASSOCIATE`（`cmd=0x03`），shadowsocks 风格、**不支持分片**：

- client 在控制连接所在本机 IP 上开一个临时 UDP 中继端口，回 `BND.ADDR:BND.PORT`；
- app 的 UDP 数据报剥掉 `RSV+FRAG` 头后，以 `[2字节长度][socks5addr+payload]`
  记录写入复用的 mux stream（用哨兵地址 `0xFD 'U' 'D' 'P'` 标记该流为 UDP 关联）；
- server 侧 `ServeUDP` 按 socks5 目标地址维护 `net.UDPConn` NAT 会话表收发，
  60s 空闲回收；回包再加目标地址写回 stream，client 补回 `RSV+FRAG` 头送给 app。
- `FRAG != 0` 的分片数据报直接丢弃；单条记录上限 64KB。

验证用例：
- `20_udp_dns.sh`：最小用例，经 UDP ASSOCIATE 发真实 DNS 查询。
- `25_udp_echo.sh`：端到端多场景，覆盖小包、近 64KB 大包(逐字节校验)、高并发、
  多目标 NAT 会话表、`FRAG!=0` 分片丢弃。
