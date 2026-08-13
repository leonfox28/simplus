#!/usr/bin/env bash

set -euo pipefail
umask 077

SCRIPT_DIR=$(CDPATH= cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
REPO_ROOT=$(CDPATH= cd "$SCRIPT_DIR/../.." && pwd -P)

fail() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

[[ $(uname -s) == Linux ]] || fail 'development Agent deployment is Linux-only'
[[ -x $REPO_ROOT/.tools/go/bin/go ]] || fail 'repository-local Go toolchain is missing; run make dev-toolchain'
for command_name in getent git install python3 sudo systemctl; do
  command -v "$command_name" >/dev/null 2>&1 || fail "missing required command: $command_name"
done

cd "$REPO_ROOT"
version=${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || printf dev)}
commit=${COMMIT:-$(git rev-parse --short=12 HEAD 2>/dev/null || printf unknown)}
[[ $version =~ ^[A-Za-z0-9._+-]+$ ]] || { printf 'error: invalid development version\n' >&2; exit 2; }
[[ $commit == unknown || $commit =~ ^[A-Fa-f0-9]+$ ]] || { printf 'error: invalid development commit\n' >&2; exit 2; }
ldflags="-s -w -X github.com/leonfox28/simplus/internal/buildinfo.Version=$version -X github.com/leonfox28/simplus/internal/buildinfo.Commit=$commit"
developer_user=$(id -un)
developer_uid=$(id -u)
install -d -m 0700 .dev .dev/bin
.tools/go/bin/go build -buildvcs=false -trimpath -ldflags "$ldflags" -o .dev/bin/simplus-agent-deploy ./cmd/simplus-agent
.tools/go/bin/go build -buildvcs=false -trimpath -ldflags "$ldflags" -o .dev/bin/simplusctl-agent-deploy ./cmd/simplusctl

SUDO=(sudo)
if [[ -t 0 ]]; then
  "${SUDO[@]}" -v || fail 'sudo authorization is required for development Agent deployment'
else
  SUDO+=(-n)
  "${SUDO[@]}" true || fail 'non-interactive Agent deployment requires an existing sudo credential or passwordless sudo'
fi
if "${SUDO[@]}" systemctl is-active --quiet ModemManager.service; then
  fail 'ModemManager is active; stop it explicitly before granting the development Agent exclusive modem ownership'
fi

"${SUDO[@]}" groupadd --system --force simplus-dev
if ! getent passwd simplus-agent-dev >/dev/null; then
  "${SUDO[@]}" useradd --system --user-group --home-dir /nonexistent --shell /usr/sbin/nologin simplus-agent-dev
fi
supplemental=simplus-dev
getent group dialout >/dev/null && supplemental+=,dialout
"${SUDO[@]}" usermod -G "$supplemental" simplus-agent-dev
"${SUDO[@]}" usermod -a -G simplus-dev "$developer_user"
unit_groups=${supplemental//,/ }
socket_gid=$(getent group simplus-dev | cut -d: -f3)
[[ -n $socket_gid ]] || fail 'failed to resolve simplus-dev group ID'

"${SUDO[@]}" install -d -o root -g root -m 0755 /usr/local/libexec/simplus-dev
"${SUDO[@]}" install -o root -g root -m 0755 .dev/bin/simplus-agent-deploy /usr/local/libexec/simplus-dev/simplus-agent
tmp=$(mktemp -d "${TMPDIR:-/tmp}/simplus-agent-deploy.XXXXXX")
cleanup() {
  rm -rf -- "$tmp"
}
trap cleanup EXIT HUP INT TERM
unit=$tmp/simplus-agent-dev.service
probe=$tmp/probe.json
cat >"$unit" <<EOF
[Unit]
Description=Simplus bounded development hardware Agent
After=local-fs.target

[Service]
Type=simple
User=simplus-agent-dev
Group=simplus-agent-dev
SupplementaryGroups=$unit_groups
RuntimeDirectory=simplus-agent-dev
RuntimeDirectoryMode=0750
StateDirectory=simplus-agent-dev
StateDirectoryMode=0700
UMask=0077
ExecStart=/usr/local/libexec/simplus-dev/simplus-agent --socket /run/simplus-agent-dev/simplus-agent.sock --sim-aka-socket /run/simplus-agent-dev/sim-aka.sock --identity-key /var/lib/simplus-agent-dev/.identity-key-v1 --state-root /var/lib/simplus-agent-dev/qdc507-sms --directory-mode 0750 --socket-mode 0660 --socket-gid $socket_gid --allowed-uid 0 --allowed-uid $developer_uid --scan-interval 500ms
Restart=on-failure
RestartSec=2s
NoNewPrivileges=true
CapabilityBoundingSet=
AmbientCapabilities=
PrivateTmp=true
PrivateNetwork=true
ProtectSystem=strict
ProtectHome=true
ProtectHostname=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectKernelLogs=true
ProtectControlGroups=true
RestrictAddressFamilies=AF_UNIX
RestrictSUIDSGID=true
RestrictRealtime=true
SystemCallArchitectures=native
LockPersonality=true
MemoryDenyWriteExecute=true

[Install]
WantedBy=multi-user.target
EOF
"${SUDO[@]}" install -o root -g root -m 0644 "$unit" /etc/systemd/system/simplus-agent-dev.service
"${SUDO[@]}" systemctl daemon-reload
"${SUDO[@]}" systemctl enable simplus-agent-dev.service
"${SUDO[@]}" systemctl restart simplus-agent-dev.service
for _ in $(seq 1 50); do
  "${SUDO[@]}" test -S /run/simplus-agent-dev/simplus-agent.sock && break
  sleep .1
done
if ! "${SUDO[@]}" test -S /run/simplus-agent-dev/simplus-agent.sock; then
  "${SUDO[@]}" journalctl -u simplus-agent-dev.service -n 30 --no-pager
  fail 'development Agent socket was not created'
fi
"${SUDO[@]}" .dev/bin/simplusctl-agent-deploy hardware probe \
  --socket /run/simplus-agent-dev/simplus-agent.sock --json >"$probe"
python3 - "$probe" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as stream:
    report = json.load(stream)
print("Agent protocol v%s; topology generation %s; devices %s" % (
    report["hello"]["protocolVersion"], report["snapshot"]["generation"], len(report["snapshot"]["devices"])))
for device in report["snapshot"]["devices"]:
    probe = next((item for item in report["probe"]["devices"] if item["deviceId"] == device["id"]), {})
    print("- %s %s:%s %s" % (device["displayName"], device["usb"]["vendorId"], device["usb"]["productId"], probe.get("state", "missing")))
PY
if ! id -G | tr ' ' '\n' | grep -qx "$socket_gid"; then
  printf 'Development Agent installed. Start a new login session before running make dev-hardware without sudo.\n'
fi
