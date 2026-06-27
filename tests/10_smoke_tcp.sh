#!/usr/bin/env bash
# 10_smoke_tcp.sh —— SOCKS5/TCP 冒烟：经代理访问 H1/H2 站点并校验状态码。
set -uo pipefail
source "$(dirname "$0")/lib/common.sh"
require_bbk

wait_port 127.0.0.1 "$SOCKS_PORT" 5 || { log_fail "socks5 $SOCKS_ADDR not up (先 ./01_up_local.sh)"; exit 1; }

# H1 明文
code=$(socks_curl "http://example.com/")
[ "$code" = "200" ] && pass "H1 http://example.com -> $code" || fail "H1 http://example.com -> $code"

# H2 over TLS（--http2 强制协商）
code=$(socks_curl "https://www.cloudflare.com/cdn-cgi/trace" --http2)
[ "$code" = "200" ] && pass "H2 https cloudflare trace -> $code" || fail "H2 https cloudflare trace -> $code"

# 经代理看出口 IP
echo "---- 出口 trace (cloudflare) ----"
curl -sS --connect-timeout 10 --max-time 30 --socks5-hostname "$SOCKS_ADDR" \
  https://www.cloudflare.com/cdn-cgi/trace 2>/dev/null | tee "$RESULTS_DIR/egress_trace.txt" || true
echo "--------------------------------"

finish_report
