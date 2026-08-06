#!/usr/bin/env bash
set -euo pipefail

[[ $(id -u) == 0 ]] || {
  printf 'run as root\n' >&2
  exit 2
}
[[ $(uname -s) == Linux && $(uname -m) == x86_64 ]] || {
  printf 'Simplus container deployment currently supports Linux amd64 only\n' >&2
  exit 1
}
[[ -r /etc/os-release ]] || { printf 'missing /etc/os-release\n' >&2; exit 1; }
# shellcheck disable=SC1091
. /etc/os-release
[[ ${ID:-} == debian && ${VERSION_ID:-} == 13* ]] || {
  printf 'Simplus container deployment currently supports Debian 13 only\n' >&2
  exit 1
}
for command_name in install mktemp modprobe; do
  command -v "$command_name" >/dev/null 2>&1 || {
    printf 'missing required host command: %s\n' "$command_name" >&2
    exit 1
  }
done

temporary=$(mktemp "${TMPDIR:-/tmp}/simplus-modules.XXXXXX")
cleanup() {
  rm -f -- "$temporary"
}
trap cleanup EXIT HUP INT TERM
printf 'option\n' >"$temporary"
install -o root -g root -m 0644 "$temporary" /etc/modules-load.d/simplus.conf
modprobe option

new_id=/sys/bus/usb-serial/drivers/option1/new_id
[[ -f $new_id && -w $new_id ]] || {
  printf 'option driver new_id is unavailable after module load\n' >&2
  exit 1
}

if command -v systemctl >/dev/null 2>&1 && systemctl is-active --quiet ModemManager.service; then
  printf 'warning: ModemManager is active; Simplus will fail closed if it owns the target serial port\n' >&2
fi
printf 'Simplus host kernel preparation completed; no USB ID was written on the host\n'
