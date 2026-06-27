# shellcheck shell=bash
# tests/lib/common.sh —— bbk 稳定性测试通用函数库。
# 用法: 在脚本顶部 `source "$(dirname "$0")/lib/common.sh"`。

set -o pipefail

# 目录约定（以 tests/ 为根）。
TESTS_DIR="${TESTS_DIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
REPO_DIR="${REPO_DIR:-$(cd "$TESTS_DIR/.." && pwd)}"
BIN_DIR="$TESTS_DIR/bin"
RESULTS_DIR="$TESTS_DIR/results"
PIDS_DIR="$RESULTS_DIR/pids"
BBK_BIN="$BIN_DIR/bbk"

# 本地端口约定（与 tests/configs/local/*.json 保持一致）。
# bbk 是 2 端拓扑：client(socks) -> server(tunnel)。
SOCKS_PORT="${SOCKS_PORT:-1090}"
SOCKS_ADDR="127.0.0.1:${SOCKS_PORT}"
SERVER_PORT="${SERVER_PORT:-5900}"

mkdir -p "$RESULTS_DIR" "$PIDS_DIR" "$BIN_DIR"

# ---- 彩色日志 ----
_c() { printf '\033[%sm%s\033[0m' "$1" "$2"; }
log_info() { echo "$(_c '0;36' '[INFO]') $*"; }
log_ok()   { echo "$(_c '0;32' '[ OK ]') $*"; }
log_warn() { echo "$(_c '0;33' '[WARN]') $*"; }
log_fail() { echo "$(_c '0;31' '[FAIL]') $*"; }

# 全局通过/失败计数（脚本结束用 finish_report 汇总）。
PASS_COUNT=0
FAIL_COUNT=0
pass() { PASS_COUNT=$((PASS_COUNT+1)); log_ok "$*"; }
fail() { FAIL_COUNT=$((FAIL_COUNT+1)); log_fail "$*"; }
finish_report() {
  echo "----------------------------------------"
  echo "PASS=$PASS_COUNT FAIL=$FAIL_COUNT"
  [ "$FAIL_COUNT" -eq 0 ]
}

# wait_port HOST PORT [TIMEOUT_SEC] —— 轮询等待 TCP 端口可连。
wait_port() {
  local host="$1" port="$2" timeout="${3:-15}" i=0
  while [ "$i" -lt "$((timeout*5))" ]; do
    if (exec 3<>"/dev/tcp/$host/$port") 2>/dev/null; then
      exec 3>&- 3<&- 2>/dev/null
      return 0
    fi
    sleep 0.2; i=$((i+1))
  done
  return 1
}

# start_bg NAME -- CMD... —— 后台启动进程，pid 记到 results/pids/NAME.pid，日志到 results/NAME.log。
start_bg() {
  local name="$1"; shift
  [ "$1" = "--" ] && shift
  local logf="$RESULTS_DIR/$name.log"
  : >"$logf"
  # exec 让子 shell 被进程替换，记录的 pid 即真实进程，stop_bg 才能真正杀掉。
  ( cd "$REPO_DIR" && exec "$@" >>"$logf" 2>&1 ) &
  local pid=$!
  echo "$pid" >"$PIDS_DIR/$name.pid"
  log_info "started $name pid=$pid log=$logf"
}

# stop_bg NAME —— 关闭由 start_bg 启动的进程。
stop_bg() {
  local name="$1" pidf="$PIDS_DIR/$1.pid"
  [ -f "$pidf" ] || return 0
  local pid; pid="$(cat "$pidf")"
  if kill -0 "$pid" 2>/dev/null; then
    kill "$pid" 2>/dev/null
    for _ in 1 2 3 4 5 6 7 8 9 10; do kill -0 "$pid" 2>/dev/null || break; sleep 0.2; done
    kill -9 "$pid" 2>/dev/null || true
    log_info "stopped $name pid=$pid"
  fi
  rm -f "$pidf"
}

# stop_all —— 关闭所有已记录的后台进程。
stop_all() {
  local f
  for f in "$PIDS_DIR"/*.pid; do
    [ -e "$f" ] || continue
    stop_bg "$(basename "$f" .pid)"
  done
}

# socks_curl URL [EXTRA curl args...] —— 经 SOCKS5(含远端 DNS) 发起请求，仅返回 HTTP 状态码。
socks_curl() {
  local url="$1"; shift
  curl -sS -o /dev/null -w '%{http_code}' \
    --connect-timeout 10 --max-time 30 \
    --socks5-hostname "$SOCKS_ADDR" "$@" "$url"
}

# need_bin BIN —— 检查可执行是否存在，不存在返回非 0。
need_bin() { command -v "$1" >/dev/null 2>&1; }

# require_bbk —— 确认已构建 bbk 二进制。
require_bbk() {
  if [ ! -x "$BBK_BIN" ]; then
    log_fail "bbk binary not found: $BBK_BIN  (先运行 ./00_build.sh)"
    exit 1
  fi
}
