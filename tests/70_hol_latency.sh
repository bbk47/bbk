#!/usr/bin/env bash
# 70_hol_latency.sh —— 队头阻塞测试：小请求 RTT 在空闲 vs 大流量并发下的放大倍数。
# bbk 所有 stream 共享单 writeWorker，大响应可能堵住小请求。
# 用法: LOAD=4 SAMPLES=30 ./70_hol_latency.sh
set -uo pipefail
source "$(dirname "$0")/lib/common.sh"

wait_port 127.0.0.1 "$SOCKS_PORT" 5 || { log_fail "socks5 $SOCKS_ADDR not up (先 ./01_up_local.sh)"; exit 1; }

LOAD="${LOAD:-4}"
SAMPLES="${SAMPLES:-30}"
PROBE="$TESTS_DIR/lib/holtest.go"

out=$( cd "$REPO_DIR" && go run "$PROBE" -socks "$SOCKS_ADDR" -load "$LOAD" -samples "$SAMPLES" 2>&1 )
echo "$out"
if echo "$out" | grep -q "WARN:"; then
  fail "存在明显队头阻塞（大流量会拖慢小请求）"
else
  pass "队头阻塞不明显"
fi
finish_report
