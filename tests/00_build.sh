#!/usr/bin/env bash
# 00_build.sh —— 编译 bbk 到 tests/bin/bbk。
set -euo pipefail
source "$(dirname "$0")/lib/common.sh"

if [ ! -d "$REPO_DIR/../toolbox" ]; then
  log_fail "缺少同级依赖目录 ../toolbox (go.mod 里 replace 指向它)"
  exit 1
fi

log_info "go build -> $BBK_BIN"
( cd "$REPO_DIR" && go build -o "$BBK_BIN" ./main.go )
log_ok "build done: $("$BBK_BIN" version 2>/dev/null || echo bbk)"
