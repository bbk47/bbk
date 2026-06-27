#!/usr/bin/env bash
# 20_udp_dns.sh —— SOCKS5/UDP 通路验证：经 UDP ASSOCIATE 发 DNS 查询。
# 这是验证 src/proxy/udprelay.go 是否正常工作的最小用例。
set -uo pipefail
source "$(dirname "$0")/lib/common.sh"

wait_port 127.0.0.1 "$SOCKS_PORT" 5 || { log_fail "socks5 $SOCKS_ADDR not up (先 ./01_up_local.sh)"; exit 1; }

PROBE="$TESTS_DIR/lib/udpdns_probe.go"
DNS="${DNS:-1.1.1.1:53}"
# 允许以空格传入多个域名
read -r -a NAMES <<<"${*:-example.com cloudflare.com google.com}"

for n in "${NAMES[@]}"; do
  out=$( cd "$REPO_DIR" && go run "$PROBE" -socks "$SOCKS_ADDR" -dns "$DNS" -name "$n" 2>&1 )
  rc=$?
  if [ "$rc" -eq 0 ]; then
    pass "UDP DNS $n via $DNS  ($(echo "$out" | tail -1))"
  else
    fail "UDP DNS $n via $DNS  -> $(echo "$out" | tail -1)"
  fi
done

finish_report
