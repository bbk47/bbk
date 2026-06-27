#!/usr/bin/env bash
# 25_udp_echo.sh —— UDP 中继端到端压测：经 SOCKS5 UDP ASSOCIATE 对本地 echo server
# 打小包/大包(近64KB)/高并发/多目标/分片丢弃，逐一校验回显。
# 比 20_udp_dns(单次小包DNS) 覆盖面更广，是 udprelay 的主力 e2e 用例。
# 用法: CONC=200 TARGETS=4 ./25_udp_echo.sh
set -uo pipefail
source "$(dirname "$0")/lib/common.sh"

wait_port 127.0.0.1 "$SOCKS_PORT" 5 || { log_fail "socks5 $SOCKS_ADDR not up (先 ./01_up_local.sh)"; exit 1; }

CONC="${CONC:-100}"
TARGETS="${TARGETS:-3}"
PROBE="$TESTS_DIR/lib/udpecho_probe.go"

log_info "并发 $CONC 数据报(多种大小, 含近64KB大包) + $TARGETS 目标 NAT + 分片丢弃..."
out=$( cd "$REPO_DIR" && go run "$PROBE" -socks "$SOCKS_ADDR" -conc "$CONC" -targets "$TARGETS" 2>&1 )
rc=$?
echo "$out"

if [ "$rc" -eq 0 ]; then
  pass "UDP 端到端全部用例通过 (小包/大包/并发/多目标/分片丢弃)"
else
  fail "UDP 端到端存在失败 —— 查 src/proxy/udprelay.go"
fi

finish_report
