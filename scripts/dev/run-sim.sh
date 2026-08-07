#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(CDPATH= cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
REPO_ROOT=$(CDPATH= cd "$SCRIPT_DIR/../.." && pwd -P)
API_BIN=${SIMPLUS_DEV_API_BIN:-$REPO_ROOT/.dev/bin/simplusd-dev}
DATA_ROOT=${SIMPLUS_DATA_ROOT:-$REPO_ROOT/.dev/data}
LISTEN_ADDR=${SIMPLUS_LISTEN_ADDR:-127.0.0.1:8080}
WEB_HOST=${SIMPLUS_DEV_WEB_HOST:-127.0.0.1}
WEB_PORT=${SIMPLUS_DEV_WEB_PORT:-5173}
BACKEND=${SIMPLUS_DEV_BACKEND:-simulator}
AGENT_SOCKET=${SIMPLUS_AGENT_SOCKET:-/run/simplus-agent-dev/simplus-agent.sock}
READY_URL="http://$LISTEN_ADDR/api/v1/system/health"
case $BACKEND in
  simulator) runtime_label=Simulator ;;
  hardware) runtime_label=Hardware ;;
  *) printf 'error: unsupported development backend: %s\n' "$BACKEND" >&2; exit 2 ;;
esac

if [[ ! $WEB_HOST =~ ^[A-Za-z0-9._:-]+$ ]]; then
  printf 'error: invalid SIMPLUS_DEV_WEB_HOST: %s\n' "$WEB_HOST" >&2
  exit 2
fi
if [[ ! $WEB_PORT =~ ^[0-9]+$ ]] || (( WEB_PORT < 1 || WEB_PORT > 65535 )); then
  printf 'error: invalid SIMPLUS_DEV_WEB_PORT: %s\n' "$WEB_PORT" >&2
  exit 2
fi
if [[ ! -x $API_BIN ]]; then
  printf 'error: simulator API binary is missing or not executable: %s\n' "$API_BIN" >&2
  exit 1
fi
if [[ ! -d $DATA_ROOT ]]; then
  printf 'error: simulator data root is missing: %s\n' "$DATA_ROOT" >&2
  exit 1
fi
if ! command -v curl >/dev/null 2>&1; then
  printf 'error: curl is required for simulator readiness checks\n' >&2
  exit 1
fi
if ! command -v node >/dev/null 2>&1; then
  printf 'error: Node.js is required to start Vite\n' >&2
  exit 1
fi
if ! command -v setsid >/dev/null 2>&1; then
  printf 'error: setsid is required to isolate the Vite process group\n' >&2
  exit 1
fi
if ! node - "$WEB_HOST" "$WEB_PORT" <<'NODE'
const net = require('node:net')
const host = process.argv[2]
const port = Number(process.argv[3])
const server = net.createServer()
server.once('error', () => process.exit(1))
server.listen(port, host, () => server.close(() => process.exit(0)))
NODE
then
  printf 'error: Web listen address is unavailable: %s:%s\n' "$WEB_HOST" "$WEB_PORT" >&2
  exit 1
fi
if [[ $BACKEND == hardware ]]; then
  [[ $AGENT_SOCKET == /* ]] || { printf 'error: SIMPLUS_AGENT_SOCKET must be absolute\n' >&2; exit 2; }
  if ! curl --fail --silent --show-error --max-time 2 --unix-socket "$AGENT_SOCKET" http://unix/v1/hello >/dev/null; then
    printf 'error: hardware Agent is unavailable at %s; run make dev-agent-deploy first\n' "$AGENT_SOCKET" >&2
    exit 1
  fi
fi

api_pid=''
web_pid=''
cleanup_done=0
wait_with_kill_timeout() {
  local pid=$1
  local timeout_seconds=$2
  local kill_target=${3:-$pid}
  local watchdog_pid
  if [[ -z $pid ]]; then
    return
  fi
  (
    sleep "$timeout_seconds"
    kill -KILL -- "$kill_target" 2>/dev/null || true
  ) &
  watchdog_pid=$!
  wait "$pid" 2>/dev/null || true
  kill -TERM "$watchdog_pid" 2>/dev/null || true
  wait "$watchdog_pid" 2>/dev/null || true
}
cleanup() {
  local pid
  if (( cleanup_done == 1 )); then
    return
  fi
  cleanup_done=1
  if [[ -n $web_pid ]] && kill -0 "$web_pid" 2>/dev/null; then
    # pnpm and Vite form a child tree. Keep it in a dedicated
    # session so interrupted smoke tests cannot orphan memory-heavy workers.
    kill -TERM -- "-$web_pid" 2>/dev/null || true
  fi
  if [[ -n $api_pid ]] && kill -0 "$api_pid" 2>/dev/null; then
    kill -TERM "$api_pid" 2>/dev/null || true
  fi
  # Vite gets a short exit budget. simplusd receives SIGTERM at the same time
  # and retains its full 10-second HTTP/SQLite shutdown budget plus margin.
  wait_with_kill_timeout "$web_pid" 3 "-$web_pid"
  wait_with_kill_timeout "$api_pid" 12
}
trap cleanup EXIT
trap 'exit 130' HUP INT TERM

SIMPLUS_BACKEND="$BACKEND" \
SIMPLUS_AGENT_SOCKET="$AGENT_SOCKET" \
SIMPLUS_DATA_ROOT="$DATA_ROOT" \
SIMPLUS_LISTEN_ADDR="$LISTEN_ADDR" \
  "$API_BIN" &
api_pid=$!

ready=0
for _ in $(seq 1 80); do
  if ! kill -0 "$api_pid" 2>/dev/null; then
    if wait "$api_pid"; then status=0; else status=$?; fi
    api_pid=''
    printf 'error: %s API exited before readiness (status %d)\n' "$runtime_label" "$status" >&2
    if (( status == 0 )); then status=1; fi
    exit "$status"
  fi
  if curl --fail --silent --show-error --max-time 1 \
    --header 'Accept: application/json' "$READY_URL" >/dev/null 2>&1; then
    ready=1
    break
  fi
  sleep 0.1
done
if (( ready != 1 )); then
  printf 'error: %s API did not become ready at %s\n' "$runtime_label" "$READY_URL" >&2
  exit 1
fi
printf '%s API ready: %s\n' "$runtime_label" "$READY_URL"

(
  cd "$REPO_ROOT/web"
  exec setsid --wait env VITE_API_PROXY_TARGET="http://$LISTEN_ADDR" HOST="$WEB_HOST" PORT="$WEB_PORT" \
    corepack pnpm dev
) &
web_pid=$!

while :; do
  if ! kill -0 "$api_pid" 2>/dev/null; then
    if wait "$api_pid"; then status=0; else status=$?; fi
    api_pid=''
    printf 'error: %s API exited; stopping Vite (status %d)\n' "$runtime_label" "$status" >&2
    if (( status == 0 )); then status=1; fi
    exit "$status"
  fi
  if ! kill -0 "$web_pid" 2>/dev/null; then
    if wait "$web_pid"; then status=0; else status=$?; fi
    web_pid=''
    printf 'error: Vite exited; stopping %s API (status %d)\n' "$runtime_label" "$status" >&2
    if (( status == 0 )); then status=1; fi
    exit "$status"
  fi
  sleep 0.2
done
