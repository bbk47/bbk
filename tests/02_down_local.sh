#!/usr/bin/env bash
# 02_down_local.sh —— 关闭本地两端。
set -euo pipefail
source "$(dirname "$0")/lib/common.sh"
stop_bg client
stop_bg server
log_ok "local 2-tier stopped"
