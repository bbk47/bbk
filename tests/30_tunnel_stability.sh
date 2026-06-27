#!/usr/bin/env bash
# 30_tunnel_stability.sh —— 隧道稳定性压测：
#   阶段A 并发负载: CONC 路并发 x ROUNDS 轮 socks5 请求，统计成功率/耗时。
#   阶段B 断线重连: 压测中途杀掉 server 再拉起，验证 client 自动重连后恢复。
# 用法: CONC=20 ROUNDS=10 URL=https://www.cloudflare.com/cdn-cgi/trace ./30_tunnel_stability.sh
set -uo pipefail
source "$(dirname "$0")/lib/common.sh"
require_bbk

wait_port 127.0.0.1 "$SOCKS_PORT" 5 || { log_fail "socks5 $SOCKS_ADDR not up (先 ./01_up_local.sh)"; exit 1; }

CONC="${CONC:-20}"
ROUNDS="${ROUNDS:-10}"
URL="${URL:-https://www.cloudflare.com/cdn-cgi/trace}"
PROTO="$(cat "$RESULTS_DIR/proto" 2>/dev/null || echo ws)"
CFG="$TESTS_DIR/configs/local"
case "$PROTO" in
  tcp) SERVER_CFG="$CFG/server.tcp.json" ;;
  *)   SERVER_CFG="$CFG/server.ws.json" ;;
esac

SUMMARY="$RESULTS_DIR/stability_$(date +%Y%m%d_%H%M%S).txt"
log_info "proto=$PROTO conc=$CONC rounds=$ROUNDS url=$URL"
log_info "summary -> $SUMMARY"

one_req() { # -> "code time_total"  (-w 末尾 \n 让 read 正常返回)
  curl -sS -o /dev/null -w '%{http_code} %{time_total}\n' \
    --connect-timeout 10 --max-time 30 --socks5-hostname "$SOCKS_ADDR" "$URL" 2>/dev/null || echo "000 0"
}

run_batch() { # $1=label ; 并发 CONC 路，输出成功数
  local label="$1" tmp; tmp="$(mktemp -d)"
  for i in $(seq 1 "$CONC"); do
    ( one_req >"$tmp/$i" ) &
  done
  wait
  local ok=0 total=0 sumt=0
  for i in $(seq 1 "$CONC"); do
    total=$((total+1))
    code=000; t=0
    read -r code t <"$tmp/$i" 2>/dev/null || true   # 无结尾换行时 read 返回1但已赋值，勿覆盖
    [ -n "$code" ] || code=000
    if [ "$code" = "200" ]; then ok=$((ok+1)); sumt=$(awk "BEGIN{print $sumt+$t}"); fi
  done
  rm -rf "$tmp"
  local avg="n/a"
  [ "$ok" -gt 0 ] && avg=$(awk "BEGIN{printf \"%.3f\", $sumt/$ok}")
  echo "$label: ok=$ok/$total avg_time=${avg}s" | tee -a "$SUMMARY"
  echo "$ok"
}

# ---- 阶段A: 持续并发 ----
echo "==== 阶段A 并发负载 ====" | tee "$SUMMARY"
total_ok=0; total_req=0
for r in $(seq 1 "$ROUNDS"); do
  ok=$(run_batch "roundA-$r" | tail -1)
  total_ok=$((total_ok+ok)); total_req=$((total_req+CONC))
done
rateA=$(awk "BEGIN{printf \"%.1f\", $total_ok*100/$total_req}")
echo "阶段A 成功率: $total_ok/$total_req = ${rateA}%" | tee -a "$SUMMARY"
[ "$total_ok" -eq "$total_req" ] && pass "阶段A 全部成功" || log_warn "阶段A 有失败 ($rateA%)"

# ---- 阶段B: 断线重连 ----
echo "==== 阶段B 断线重连 ====" | tee -a "$SUMMARY"
log_info "kill server 模拟链路中断..."
stop_bg server
sleep 2
# 中断期间发一次请求（预期失败/超时）
midcode=$(socks_curl "$URL" || echo 000)
echo "中断期间请求 code=$midcode (预期非200)" | tee -a "$SUMMARY"

log_info "重启 server..."
start_bg server -- "$BBK_BIN" -c "$SERVER_CFG"
wait_port 127.0.0.1 "$SERVER_PORT" 15 || { fail "server 重启失败"; finish_report; exit 1; }

# client 重连有退避，给足恢复时间后重试探测
recovered=0
for i in $(seq 1 20); do
  sleep 2
  code=$(socks_curl "$URL" || echo 000)
  if [ "$code" = "200" ]; then recovered=1; echo "第 $((i*2))s 恢复, code=$code" | tee -a "$SUMMARY"; break; fi
done
if [ "$recovered" = "1" ]; then pass "阶段B 断线后自动重连恢复"; else fail "阶段B 重连未恢复"; fi

# 恢复后再压一轮
echo "==== 阶段C 恢复后复压 ====" | tee -a "$SUMMARY"
ok=$(run_batch "roundC" | tail -1)
[ "$ok" -eq "$CONC" ] && pass "阶段C 恢复后并发全绿" || log_warn "阶段C ok=$ok/$CONC"

finish_report
