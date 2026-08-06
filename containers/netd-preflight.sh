#!/bin/sh
set -eu

fail() {
    printf 'simplus-netd preflight: %s\n' "$*" >&2
    exit 1
}

[ "$(id -u)" = 0 ] || fail 'root is required'
[ "$(cat /proc/sys/net/ipv4/ip_forward 2>/dev/null || true)" = 1 ] || \
    fail 'container net.ipv4.ip_forward must equal 1'

for path in \
    /usr/sbin/ip \
    /usr/sbin/nft \
    /usr/sbin/charon-systemd \
    /usr/lib/ipsec/plugins/libstrongswan-eap-aka.so \
    /usr/lib/ipsec/plugins/libstrongswan-simplus-simaka.so \
    /usr/lib/ipsec/plugins/libstrongswan-p-cscf.so; do
    [ -e "$path" ] || fail "required runtime file is missing: $path"
done
[ -S /run/simplus-agent/sim-aka.sock ] || fail 'root-only SIM AKA Agent socket is unavailable'

pid=$$
namespace=sp-pf-$pid
host_interface=sph$pid
peer_interface=spp$pid
table=sp_pf_$pid

cleanup() {
    /usr/sbin/nft delete table inet "$table" >/dev/null 2>&1 || true
    /usr/sbin/ip link delete "$host_interface" >/dev/null 2>&1 || true
    /usr/sbin/ip netns delete "$namespace" >/dev/null 2>&1 || true
}
trap cleanup 0 1 2 15

/usr/sbin/ip netns add "$namespace"
/usr/sbin/ip link add "$host_interface" type veth peer name "$peer_interface"
/usr/sbin/ip link set "$peer_interface" netns "$namespace"
/usr/sbin/ip addr add 192.0.2.1/30 dev "$host_interface"
/usr/sbin/ip link set "$host_interface" up
/usr/sbin/ip netns exec "$namespace" /usr/sbin/ip link set lo up
/usr/sbin/ip netns exec "$namespace" /usr/sbin/ip addr add 192.0.2.2/30 dev "$peer_interface"
/usr/sbin/ip netns exec "$namespace" /usr/sbin/ip link set "$peer_interface" up
/usr/sbin/ip netns exec "$namespace" /usr/sbin/ip xfrm state add \
    src 192.0.2.2 dst 192.0.2.1 proto esp spi 0x6fff mode tunnel reqid 28671 \
    auth-trunc 'hmac(sha256)' \
    0x000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f 128 \
    enc 'cbc(aes)' 0x000102030405060708090a0b0c0d0e0f
/usr/sbin/ip netns exec "$namespace" /usr/sbin/ip xfrm policy add \
    src 198.18.0.1/32 dst 198.18.0.2/32 dir out priority 100 \
    tmpl src 192.0.2.2 dst 192.0.2.1 proto esp mode tunnel reqid 28671
/usr/sbin/ip netns exec "$namespace" /usr/sbin/ip xfrm state list >/dev/null
/usr/sbin/ip netns exec "$namespace" /usr/sbin/ip xfrm policy list >/dev/null

/usr/sbin/nft -f - <<EOF
add table inet $table
add chain inet $table prerouting { type filter hook prerouting priority mangle; policy accept; }
add rule inet $table prerouting iifname "$host_interface" meta l4proto udp meta mark set 0x6fff tproxy ip to 127.0.0.1:19999 accept
EOF

cleanup
trap - 0 1 2 15
printf 'Simplus netd container preflight passed\n'
