#!/usr/bin/env bash
# 60_stream_integrity.sh —— 并发多流完整性测试：检测 bbk mux/共享 CFB 流在高并发下
# 是否损坏数据或导致隧道重连。
# 用法: CONC=50 BYTES=2097152 ./60_stream_integrity.sh
set -uo pipefail
source "$(dirname "$0")/lib/common.sh"

wait_port 127.0.0.1 "$SOCKS_PORT" 5 || { log_fail "socks5 $SOCKS_ADDR not up (先 ./01_up_local.sh)"; exit 1; }

CONC="${CONC:-50}"
BYTES="${BYTES:-2097152}"
PROBE="$TESTS_DIR/lib/streamintegrity.go"
CLIENT_LOG="$RESULTS_DIR/client.log"

# 记录测试前 client.log 行数，便于只看本次新增的错误。
base=0; [ -f "$CLIENT_LOG" ] && base=$(wc -l <"$CLIENT_LOG")

log_info "并发 $CONC 路 x ${BYTES}B，逐字节校验回显..."
out=$( cd "$REPO_DIR" && go run "$PROBE" -socks "$SOCKS_ADDR" -conc "$CONC" -bytes "$BYTES" 2>&1 )
rc=$?
echo "$out"

# 检查本次新增日志里是否有解密/解码失败、隧道重连等致命信号。
if [ -f "$CLIENT_LOG" ]; then
  newlog=$(tail -n +"$((base+1))" "$CLIENT_LOG")
  bad=$(echo "$newlog" | grep -iE "Decrypt.*failed|Decode.*failed|protol error|no tunnel|reconnect|produce err|transport read packet err" || true)
  if [ -n "$bad" ]; then
    log_warn "client.log 出现致命信号（隧道/密码流可能被打乱）："
    echo "$bad" | sed 's/^/    /'
    fail "检测到隧道层错误"
  else
    pass "client.log 无解密/重连错误"
  fi
fi

if [ "$rc" -eq 0 ]; then
  pass "并发多流字节完全一致 (无 mux/cipher 损坏)"
else
  fail "出现字节损坏或流错误 —— 高度怀疑共享 CFB 流/mux 并发 bug"
fi

finish_report
