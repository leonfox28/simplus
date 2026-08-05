#!/bin/sh
set -eu

/usr/sbin/modprobe option
new_id=/sys/bus/usb-serial/drivers/option1/new_id
if [ ! -w "$new_id" ]; then
    printf 'ML307A option driver new_id is unavailable\n' >&2
    exit 1
fi

# The kernel returns EEXIST when this boot already registered the fixed ID.
# That is the only expected failure for this official VID/PID pair.
if ! printf '2ecc 3012\n' >"$new_id" 2>/dev/null; then
    exit 0
fi
