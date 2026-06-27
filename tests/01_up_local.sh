#!/usr/bin/env bash
# 01_up_local.sh —— 按 server -> client 顺序在本地拉起两端。
# 用法: PROTO=ws ./01_up_local.sh   (PROTO 可选 ws|tcp，默认 ws)
set -euo pipefail
source "$(dirname "$0")/lib/common.sh"
require_bbk

PROTO="${PROTO:-ws}"
CFG="$TESTS_DIR/configs/local"
case "$PROTO" in
  ws)  SERVER_CFG="$CFG/server.ws.json";  CLIENT_CFG="$CFG/client.ws.json" ;;
  tcp) SERVER_CFG="$CFG/server.tcp.json"; CLIENT_CFG="$CFG/client.tcp.json" ;;
  *)   log_fail "unknown PROTO=$PROTO (ws|tcp)"; exit 1 ;;
esac
echo "$PROTO" >"$RESULTS_DIR/proto"

log_info "starting local 2-tier (proto=$PROTO)"
start_bg server -- "$BBK_BIN" -c "$SERVER_CFG"
wait_port 127.0.0.1 "$SERVER_PORT" 15 || { log_fail "server :$SERVER_PORT not up"; exit 1; }
pass "server up :$SERVER_PORT"

start_bg client -- "$BBK_BIN" -c "$CLIENT_CFG"
wait_port 127.0.0.1 "$SOCKS_PORT" 15 || { log_fail "client socks :$SOCKS_PORT not up"; exit 1; }
pass "client socks5 up :$SOCKS_PORT"

log_ok "local 2-tier ready. socks5=$SOCKS_ADDR"
