#!/bin/sh
set -eu

fail() {
    printf 'simplus-netd container: %s\n' "$*" >&2
    exit 1
}

[ "$(id -u)" = 0 ] || fail 'entrypoint must run as root'
[ ! -L /run/simplus-netd ] && [ -d /run/simplus-netd ] || \
    fail '/run/simplus-netd must be a real runtime volume'
[ ! -L /var/lib/simplus ] && [ -d /var/lib/simplus ] || \
    fail '/var/lib/simplus must be a real data bind mount'

chgrp 10001 /run/simplus-netd
chmod 0750 /run/simplus-netd
/usr/local/libexec/simplus/netd-preflight

exec /usr/local/bin/simplus-netd \
    --socket /run/simplus-netd/mihomo.sock \
    --mihomo-root /var/lib/simplus/mihomo \
    --vowifi-root /run/simplus-netd/vowifi \
    --service-uid 10001 \
    --service-gid 10001
