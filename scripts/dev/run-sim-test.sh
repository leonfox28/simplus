#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(CDPATH= cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
REPO_ROOT=$(CDPATH= cd "$SCRIPT_DIR/../.." && pwd -P)
API_BIN=${SIMPLUS_DEV_API_BIN:-$REPO_ROOT/.dev/bin/simplusd-dev}
TMP_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/simplus-run-sim-test.XXXXXX")
CONTROL_ROOT="/tmp/simplus-ctrl.$$"
mkdir -m 700 "$CONTROL_ROOT"
SIMPLUS_CONTROL_SOCKET="$CONTROL_ROOT/control.sock"
export SIMPLUS_CONTROL_SOCKET
PIDS=''

cleanup() {
  local pid
  for pid in $PIDS; do
    kill -TERM "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
  done
  rm -rf "$CONTROL_ROOT" "$TMP_ROOT"
}
trap cleanup EXIT HUP INT TERM

fail() {
  printf 'run-sim integration test failed: %s\n' "$*" >&2
  exit 1
}

pick_port() {
  node -e 'const s=require("net").createServer(); s.listen(0,"127.0.0.1",()=>{console.log(s.address().port);s.close()})'
}

wait_for_file() {
  local path=$1
  local attempt
  for attempt in $(seq 1 50); do
    [[ -f $path ]] && return 0
    sleep 0.05
  done
  return 1
}

wait_for_url() {
  local url=$1
  local attempts=${2:-100}
  local attempt
  for attempt in $(seq 1 "$attempts"); do
    if curl --fail --silent --max-time 0.5 "$url" >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.05
  done
  return 1
}

assert_unreachable() {
  local url=$1
  local attempt
  for attempt in $(seq 1 30); do
    if ! curl --fail --silent --max-time 0.2 "$url" >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.05
  done
  fail "URL remained reachable after supervisor exit: $url"
}

start_holder() {
  local port=$1
  local ready_file=$2
  node - "$port" "$ready_file" <<'NODE' &
const fs = require('node:fs')
const net = require('node:net')
const port = Number(process.argv[2])
const readyFile = process.argv[3]
const server = net.createServer(() => {})
server.listen(port, '127.0.0.1', () => fs.writeFileSync(readyFile, 'ready\n'))
for (const signal of ['SIGTERM', 'SIGINT']) {
  process.on(signal, () => server.close(() => process.exit(0)))
}
NODE
  HOLDER_PID=$!
  PIDS="$PIDS $HOLDER_PID"
  wait_for_file "$ready_file" || fail "port holder did not start on $port"
}

wait_status() {
  local pid=$1
  set +e
  wait "$pid"
  WAIT_STATUS=$?
  set -e
  PIDS=$(printf '%s\n' "$PIDS" | tr ' ' '\n' | grep -v "^$pid$" | tr '\n' ' ' || true)
}

[[ -x $API_BIN ]] || fail "missing simulator API binary: $API_BIN"
command -v node >/dev/null 2>&1 || fail 'node is required'
command -v curl >/dev/null 2>&1 || fail 'curl is required'

invalid_host_root="$TMP_ROOT/invalid-host"
mkdir -m 700 "$invalid_host_root"
set +e
SIMPLUS_DEV_API_BIN="$API_BIN" \
SIMPLUS_DATA_ROOT="$invalid_host_root" \
SIMPLUS_DEV_WEB_HOST='invalid host' \
  "$SCRIPT_DIR/run-sim.sh" >"$TMP_ROOT/invalid-host.log" 2>&1
status=$?
set -e
[[ $status == 2 ]] || {
  cat "$TMP_ROOT/invalid-host.log" >&2
  fail "invalid Web host exit status was $status, expected 2"
}

# Normal startup and signal shutdown: both children must be reachable, then gone.
api_port=$(pick_port)
web_port=$(pick_port)
while [[ $web_port == "$api_port" ]]; do web_port=$(pick_port); done
normal_root="$TMP_ROOT/normal"
mkdir -m 700 "$normal_root"
SIMPLUS_DEV_API_BIN="$API_BIN" \
SIMPLUS_DATA_ROOT="$normal_root" \
SIMPLUS_LISTEN_ADDR="127.0.0.1:$api_port" \
SIMPLUS_DEV_WEB_HOST="127.0.0.1" \
SIMPLUS_DEV_WEB_PORT="$web_port" \
  "$SCRIPT_DIR/run-sim.sh" >"$TMP_ROOT/normal.log" 2>&1 &
supervisor_pid=$!
PIDS="$PIDS $supervisor_pid"
wait_for_url "http://127.0.0.1:$api_port/api/v1/system/health" || {
  cat "$TMP_ROOT/normal.log" >&2
  fail 'API did not become reachable during normal startup'
}
wait_for_url "http://127.0.0.1:$api_port/api/v1/setup/status" || {
  cat "$TMP_ROOT/normal.log" >&2
  fail 'setup status API did not become reachable during normal startup'
}
curl --fail --silent "http://127.0.0.1:$api_port/api/v1/setup/status" | node -e '
  const chunks = []
  process.stdin.on("data", (chunk) => chunks.push(chunk))
  process.stdin.on("end", () => {
    const setup = JSON.parse(Buffer.concat(chunks).toString("utf8"))
    if (setup.installationState !== "uninitialized" || setup.phase !== "bootstrap-required") process.exit(1)
    if (setup.setupRequired !== true || setup.businessApiAvailable !== false) process.exit(1)
    if (setup.bootstrapGenerationAvailable !== false) process.exit(1)
    if (setup.supportedFlows?.join(",") !== "create-new") process.exit(1)
  })
' || fail 'setup API did not return the fail-closed first-run boundary'
for locked_path in inventory hardware/topology; do
  locked_body="$TMP_ROOT/locked-${locked_path//\//-}.json"
  locked_status=$(curl --silent --output "$locked_body" --write-out '%{http_code}' \
    "http://127.0.0.1:$api_port/api/v1/$locked_path")
  [[ $locked_status == 409 ]] || fail "$locked_path API status was $locked_status before setup, expected 409"
  node -e '
    const fs = require("node:fs")
    const error = JSON.parse(fs.readFileSync(process.argv[1], "utf8"))
    if (error.code !== "INSTANCE_NOT_INITIALIZED" || error.retryable !== false) process.exit(1)
  ' "$locked_body" || fail "$locked_path API did not return the stable setup lock error"
done
session_body="$TMP_ROOT/setup-session.json"
session_status=$(curl --silent --output "$session_body" --write-out '%{http_code}' \
  "http://127.0.0.1:$api_port/api/v1/setup/session")
[[ $session_status == 401 ]] || fail "setup session status was $session_status without a cookie, expected 401"
node -e '
  const fs = require("node:fs")
  const error = JSON.parse(fs.readFileSync(process.argv[1], "utf8"))
  if (error.code !== "SETUP_SESSION_UNAUTHORIZED" || error.retryable !== false) process.exit(1)
' "$session_body" || fail 'setup session API did not reject an absent restricted session'
wait_for_url "http://127.0.0.1:$web_port/" 300 || {
  cat "$TMP_ROOT/normal.log" >&2
  fail 'Vite did not become reachable during normal startup'
}
kill -TERM "$supervisor_pid"
wait_status "$supervisor_pid"
[[ $WAIT_STATUS == 130 ]] || {
  cat "$TMP_ROOT/normal.log" >&2
  fail "normal supervisor signal exit status was $WAIT_STATUS, expected 130"
}
assert_unreachable "http://127.0.0.1:$api_port/api/v1/system/health"
assert_unreachable "http://127.0.0.1:$web_port/"

# A slow but bounded API shutdown must receive its full graceful budget instead of SIGKILL after one second.
delayed_api="$TMP_ROOT/delayed-api.mjs"
cat >"$delayed_api" <<'NODE'
#!/usr/bin/env node
import fs from 'node:fs'
import http from 'node:http'

const [host, portText] = process.env.SIMPLUS_LISTEN_ADDR.split(':')
const server = http.createServer((_request, response) => {
  response.writeHead(200, { 'Content-Type': 'application/json' })
  response.end('{"status":"ok"}\n')
})
server.listen(Number(portText), host)
let stopping = false
for (const signal of ['SIGTERM', 'SIGINT']) {
  process.on(signal, () => {
    if (stopping) return
    stopping = true
    setTimeout(() => {
      fs.writeFileSync(process.env.SIMPLUS_GRACEFUL_MARKER, 'graceful\n')
      server.close(() => process.exit(0))
    }, 1500)
  })
}
NODE
chmod 0755 "$delayed_api"
api_port=$(pick_port)
web_port=$(pick_port)
while [[ $web_port == "$api_port" ]]; do web_port=$(pick_port); done
delayed_root="$TMP_ROOT/delayed"
mkdir -m 700 "$delayed_root"
graceful_marker="$TMP_ROOT/delayed.graceful"
SIMPLUS_DEV_API_BIN="$delayed_api" \
SIMPLUS_GRACEFUL_MARKER="$graceful_marker" \
SIMPLUS_DATA_ROOT="$delayed_root" \
SIMPLUS_LISTEN_ADDR="127.0.0.1:$api_port" \
SIMPLUS_DEV_WEB_PORT="$web_port" \
  "$SCRIPT_DIR/run-sim.sh" >"$TMP_ROOT/delayed.log" 2>&1 &
supervisor_pid=$!
PIDS="$PIDS $supervisor_pid"
wait_for_url "http://127.0.0.1:$api_port/api/v1/system/health" || {
  cat "$TMP_ROOT/delayed.log" >&2
  fail 'delayed API did not become reachable'
}
wait_for_url "http://127.0.0.1:$web_port/" 300 || {
  cat "$TMP_ROOT/delayed.log" >&2
  fail 'Vite did not become reachable with delayed API'
}
kill -TERM "$supervisor_pid"
wait_status "$supervisor_pid"
[[ $WAIT_STATUS == 130 ]] || fail "delayed supervisor signal exit status was $WAIT_STATUS, expected 130"
[[ -f $graceful_marker ]] || {
  cat "$TMP_ROOT/delayed.log" >&2
  fail 'API was killed before its bounded graceful shutdown completed'
}
assert_unreachable "http://127.0.0.1:$api_port/api/v1/system/health"
assert_unreachable "http://127.0.0.1:$web_port/"

# An occupied API port must fail without leaving Vite running.
api_port=$(pick_port)
web_port=$(pick_port)
while [[ $web_port == "$api_port" ]]; do web_port=$(pick_port); done
start_holder "$api_port" "$TMP_ROOT/api-holder.ready"
api_busy_root="$TMP_ROOT/api-busy"
mkdir -m 700 "$api_busy_root"
set +e
SIMPLUS_DEV_API_BIN="$API_BIN" \
SIMPLUS_DATA_ROOT="$api_busy_root" \
SIMPLUS_LISTEN_ADDR="127.0.0.1:$api_port" \
SIMPLUS_DEV_WEB_PORT="$web_port" \
  "$SCRIPT_DIR/run-sim.sh" >"$TMP_ROOT/api-busy.log" 2>&1
status=$?
set -e
[[ $status != 0 ]] || fail 'occupied API port unexpectedly succeeded'
assert_unreachable "http://127.0.0.1:$web_port/"
kill -TERM "$HOLDER_PID"
wait "$HOLDER_PID" 2>/dev/null || true
PIDS=$(printf '%s\n' "$PIDS" | tr ' ' '\n' | grep -v "^$HOLDER_PID$" | tr '\n' ' ' || true)

# An occupied Vite port must tear down the API process and fail.
api_port=$(pick_port)
web_port=$(pick_port)
while [[ $web_port == "$api_port" ]]; do web_port=$(pick_port); done
start_holder "$web_port" "$TMP_ROOT/web-holder.ready"
web_busy_root="$TMP_ROOT/web-busy"
mkdir -m 700 "$web_busy_root"
set +e
SIMPLUS_DEV_API_BIN="$API_BIN" \
SIMPLUS_DATA_ROOT="$web_busy_root" \
SIMPLUS_LISTEN_ADDR="127.0.0.1:$api_port" \
SIMPLUS_DEV_WEB_PORT="$web_port" \
  "$SCRIPT_DIR/run-sim.sh" >"$TMP_ROOT/web-busy.log" 2>&1
status=$?
set -e
[[ $status != 0 ]] || fail 'occupied Vite port unexpectedly succeeded'
assert_unreachable "http://127.0.0.1:$api_port/api/v1/system/health"
kill -TERM "$HOLDER_PID"
wait "$HOLDER_PID" 2>/dev/null || true
PIDS=$(printf '%s\n' "$PIDS" | tr ' ' '\n' | grep -v "^$HOLDER_PID$" | tr '\n' ' ' || true)

printf 'run-sim supervisor integration tests passed\n'
