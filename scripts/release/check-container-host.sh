#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'container host check failed: %s\n' "$*" >&2
  exit 1
}

[[ $(uname -s) == Linux && $(uname -m) == x86_64 ]] || \
  fail 'Simplus currently supports Linux amd64 only'
[[ -r /etc/os-release ]] || fail '/etc/os-release is unavailable'
# shellcheck disable=SC1091
. /etc/os-release
[[ ${ID:-} == debian && ${VERSION_ID:-} == 13* ]] || \
  fail "expected Debian 13, found ${ID:-unknown} ${VERSION_ID:-unknown}"

command -v docker >/dev/null 2>&1 || fail 'Docker Engine is not installed'
docker info >/dev/null 2>&1 || fail 'the current user cannot contact Docker Engine'
docker compose version >/dev/null 2>&1 || fail 'the Docker Compose plugin is unavailable'
compose_version=$(docker compose version --short | sed 's/^v//')
[[ $(printf '%s\n%s\n' 2.24.0 "$compose_version" | sort -V | head -n 1) == 2.24.0 ]] || \
  fail "Docker Compose 2.24.0 or newer is required, found $compose_version"

security_options=$(docker info --format '{{json .SecurityOptions}}')
if grep -Eq 'rootless|name=userns' <<<"$security_options"; then
  fail 'rootless Docker and Docker user namespace remapping are unsupported'
fi

new_id=/sys/bus/usb-serial/drivers/option1/new_id
[[ -f $new_id && $(stat -c '%u' "$new_id") == 0 ]] || \
  fail 'option1/new_id is unavailable; run prepare-container-host.sh as root'
[[ -d /sys/devices && ! -L /sys/devices ]] || \
  fail '/sys/devices is unavailable for read-only USB symlink resolution'
new_id_mode=$(stat -c '%a' "$new_id")
(( (8#$new_id_mode & 8#200) != 0 )) || \
  fail 'option1/new_id is not writable by the rootful Agent container'

deployment_root=${1:-$PWD}
[[ $deployment_root == /* ]] || deployment_root=$PWD/$deployment_root
deployment_root=$(realpath -m -- "$deployment_root")
data_path=$deployment_root/data
[[ ! -L $data_path ]] || fail "$data_path must not be a symbolic link"
if [[ -e $data_path && ! -d $data_path ]]; then
  fail "$data_path must be a directory"
fi

if command -v systemctl >/dev/null 2>&1 && systemctl is-active --quiet ModemManager.service; then
  printf 'warning: ModemManager is active; stop or exclude it if Simplus reports serial-port contention\n' >&2
fi
if command -v systemctl >/dev/null 2>&1; then
  for service in simplus-ml307a-bind.service simplus-agent.service simplus-netd.service simplusd.service simplus-agent-dev.service; do
    if systemctl is-active --quiet "$service" || systemctl is-enabled --quiet "$service"; then
      fail "$service is active or enabled; stop and disable the legacy/development service explicitly before starting Compose"
    fi
  done
fi

printf 'Simplus container host check passed\n'
