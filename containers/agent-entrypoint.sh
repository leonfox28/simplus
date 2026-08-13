#!/bin/sh
set -eu

fail() {
    printf 'simplus-agent container: %s\n' "$*" >&2
    exit 1
}

[ "$(id -u)" = 0 ] || fail 'entrypoint must start as root'

device_gid=${SIMPLUS_DEVICE_GID:-20}
case "$device_gid" in
    ''|*[!0-9]*) fail 'SIMPLUS_DEVICE_GID must be a numeric group ID' ;;
esac
[ "$device_gid" -gt 0 ] && [ "$device_gid" -le 4294967295 ] || \
    fail 'SIMPLUS_DEVICE_GID is outside the supported range'

for path in /run/simplus-agent /var/lib/simplus-agent /host/sys/bus/usb/devices /host/dev; do
    [ ! -L "$path" ] && [ -d "$path" ] || fail "$path must be a real directory"
done

state_owner=$(stat -c '%u:%g:%a' /var/lib/simplus-agent)
[ "$state_owner" = '10002:10002:700' ] || \
    fail '/var/lib/simplus-agent must be owned by 10002:10002 with mode 0700; run data-init'

# CAP_CHOWN is intentionally sufficient here: temporarily make root the owner so
# chmod does not require the broader CAP_FOWNER, then hand the directory to the
# unprivileged Agent identity. This also makes restarts idempotent after the
# volume has already been owned by UID 10002.
chown 0:0 /run/simplus-agent
chmod 0750 /run/simplus-agent
chown 10002:10001 /run/simplus-agent
/usr/local/bin/simplus-agent register-option-driver

supplementary_groups=10001
if [ "$device_gid" != 10001 ]; then
    supplementary_groups="$supplementary_groups,$device_gid"
fi

exec setpriv \
    --reuid 10002 \
    --regid 10002 \
    --groups "$supplementary_groups" \
    --inh-caps=-all \
    --ambient-caps=-all \
    --bounding-set=-all \
    --no-new-privs \
    /usr/local/bin/simplus-agent \
      --socket /run/simplus-agent/simplus-agent.sock \
      --sim-aka-socket /run/simplus-agent/sim-aka.sock \
      --identity-key /var/lib/simplus-agent/.identity-key-v1 \
      --state-root /var/lib/simplus-agent/qdc507-sms \
      --sysfs-usb-root /host/sys/bus/usb/devices \
      --dev-root /host/dev \
      --directory-mode 0750 \
      --socket-mode 0660 \
      --socket-gid 10001 \
      --allowed-uid 0 \
      --allowed-uid 10001
