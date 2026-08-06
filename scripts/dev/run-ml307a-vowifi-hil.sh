#!/usr/bin/env bash
set -euo pipefail

[[ $(id -u) == 0 && $# == 2 && $1 == --expected-country && $2 =~ ^[A-Z]{2}$ ]] || exit 2

repo=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
node_config=/run/simplus-vowifi-hil-node.yaml
expected_loc=$2
ims_home_domain=ims.mnc015.mcc234.3gppnetwork.org
mihomo=/var/lib/simplus/mihomo/versions/v1.19.29/mihomo
vici_control=$repo/.dev/bin/simplus-vowifi-hil-vici
pcscf_probe=$repo/.dev/bin/simplus-vowifi-hil-pcscf
ims_probe=$repo/.dev/bin/simplus-vowifi-hil-ims
hil_dir=/run/simplus-vowifi-hil
runtime_dir=$hil_dir/mihomo
ns=simplus-vowifi-hil
host_if=svwifi-h
peer_if=svwifi-n
nft_table=simplus_vowifi_hil
mark=0x50a
route_table=1510
rule_priority=10510
mihomo_pid=
charon_pid=
logger_pid=

stun_probe=$repo/.dev/bin/simplus-vowifi-hil-stun
redactor=$repo/.dev/bin/simplus-vowifi-hil-redact

cleanup() {
  set +e
  [[ -z $logger_pid ]] || kill -TERM "$logger_pid" 2>/dev/null
  [[ -z $charon_pid ]] || kill -TERM "$charon_pid" 2>/dev/null
  [[ -z $mihomo_pid ]] || kill -TERM "$mihomo_pid" 2>/dev/null
  for _ in $(seq 1 20); do
    all_stopped=true
    [[ -z $logger_pid ]] || ! kill -0 "$logger_pid" 2>/dev/null || all_stopped=false
    [[ -z $charon_pid ]] || ! kill -0 "$charon_pid" 2>/dev/null || all_stopped=false
    [[ -z $mihomo_pid ]] || ! kill -0 "$mihomo_pid" 2>/dev/null || all_stopped=false
    [[ $all_stopped == true ]] && break
    sleep 0.1
  done
  [[ -z $logger_pid ]] || kill -KILL "$logger_pid" 2>/dev/null
  [[ -z $charon_pid ]] || kill -KILL "$charon_pid" 2>/dev/null
  [[ -z $mihomo_pid ]] || kill -KILL "$mihomo_pid" 2>/dev/null
  /usr/sbin/nft delete table inet "$nft_table" 2>/dev/null
  /usr/sbin/ip rule del fwmark "$mark" lookup "$route_table" priority "$rule_priority" 2>/dev/null
  /usr/sbin/ip route del local 0.0.0.0/0 dev lo table "$route_table" 2>/dev/null
  /usr/sbin/ip link delete "$host_if" 2>/dev/null
  /usr/sbin/ip netns del "$ns" 2>/dev/null
  if [[ -d $hil_dir && $hil_dir == /run/simplus-vowifi-hil ]]; then
    find "$hil_dir" -depth -delete 2>/dev/null
  fi
  if [[ -f $node_config && $node_config == /run/simplus-vowifi-hil-node.yaml ]]; then
    find "$node_config" -delete 2>/dev/null
  fi
}
trap cleanup EXIT INT TERM

[[ -x $mihomo && -x $vici_control && -x $pcscf_probe && -x $ims_probe && -x $stun_probe && -x $redactor && -f $node_config && ! -L $node_config && -d $hil_dir ]] || { printf 'PRECHECK artifacts=false\n'; exit 1; }
[[ $(stat -c '%U:%G:%a' "$node_config") == root:root:600 ]] || { printf 'PRECHECK node_config_private=false\n'; exit 1; }
[[ $(stat -c '%U:%G:%a' "$hil_dir") == root:root:700 ]] || { printf 'PRECHECK private_directory=false\n'; exit 1; }
[[ $(stat -c '%U:%G:%a' "$hil_dir/strongswan.conf") == root:root:600 ]] || { printf 'PRECHECK strongswan_config=false\n'; exit 1; }
[[ $(stat -c '%U:%G:%a' "$hil_dir/vici.json") == root:root:600 ]] || { printf 'PRECHECK vici_config=false\n'; exit 1; }
[[ ! -e /var/run/netns/$ns ]] || { printf 'PRECHECK stale_netns=true\n'; exit 1; }
! /usr/sbin/nft list table inet "$nft_table" >/dev/null 2>&1 || { printf 'PRECHECK stale_nft=true\n'; exit 1; }
! /usr/sbin/ip rule show | grep -q "lookup $route_table" || { printf 'PRECHECK stale_rule=true\n'; exit 1; }
install -d -m 0700 "$runtime_dir"

"$mihomo" -t -f "$node_config" -d "$runtime_dir" >/dev/null 2>&1 || { printf 'PRECHECK mihomo_config=false\n'; exit 1; }
"$mihomo" -f "$node_config" -d "$runtime_dir" >/dev/null 2>&1 &
mihomo_pid=$!
for _ in $(seq 1 50); do
  ss -lnt | grep -q '127.0.0.1:21157' && break
  kill -0 "$mihomo_pid" 2>/dev/null || exit 1
  sleep 0.1
done
ss -lnt | grep -q '127.0.0.1:21157' || exit 1

/usr/sbin/ip netns add "$ns"
/usr/sbin/ip link add "$host_if" type veth peer name "$peer_if"
/usr/sbin/ip link set "$peer_if" netns "$ns"
/usr/sbin/ip addr add 169.254.248.1/30 dev "$host_if"
/usr/sbin/ip link set "$host_if" up
/usr/sbin/ip netns exec "$ns" ip link set lo up
/usr/sbin/ip netns exec "$ns" ip addr add 169.254.248.2/30 dev "$peer_if"
/usr/sbin/ip netns exec "$ns" ip link set "$peer_if" up
/usr/sbin/ip netns exec "$ns" ip route add default via 169.254.248.1
/usr/sbin/ip rule add fwmark "$mark" lookup "$route_table" priority "$rule_priority"
/usr/sbin/ip route add local 0.0.0.0/0 dev lo table "$route_table"
/usr/sbin/nft add table inet "$nft_table"
/usr/sbin/nft "add chain inet $nft_table prerouting { type filter hook prerouting priority mangle; policy accept; }"
/usr/sbin/nft add rule inet "$nft_table" prerouting iifname "$host_if" meta l4proto tcp meta mark set "$mark" tproxy ip to 127.0.0.1:21157 accept
/usr/sbin/nft add rule inet "$nft_table" prerouting iifname "$host_if" meta l4proto udp meta mark set "$mark" tproxy ip to 127.0.0.1:21157 accept
/usr/sbin/nft add rule inet "$nft_table" prerouting iifname "$host_if" drop

trace=$(/usr/sbin/ip netns exec "$ns" timeout 15 curl -4 --silent --show-error --max-time 12 https://1.1.1.1/cdn-cgi/trace 2>/dev/null || true)
tcp_ip=$(printf '%s\n' "$trace" | awk -F= '$1=="ip"{print $2;exit}')
tcp_loc=$(printf '%s\n' "$trace" | awk -F= '$1=="loc"{print $2;exit}')
[[ -n $tcp_ip && $tcp_loc == "$expected_loc" ]] || { printf 'PREFLIGHT tcp_exit=false expected=%s observed=%s\n' "$expected_loc" "${tcp_loc:-none}"; exit 1; }

stun_ip=$(/usr/sbin/ip netns exec "$ns" timeout 9 dig +time=6 +tries=1 +short @1.1.1.1 stun.cloudflare.com A 2>/dev/null | awk '/^[0-9]+\./ && $0 !~ /^198\.(1[89]|2[0-9]|3[01])\./{print;exit}' || true)
stun_result=
[[ -z $stun_ip ]] || stun_result=$(/usr/sbin/ip netns exec "$ns" timeout 9 "$stun_probe" --target "$stun_ip:3478" 2>&1 || true)
mapped_ip=$(printf '%s\n' "$stun_result" | awk -F'mapped_ip=' '/ok=true/{print $2;exit}')
[[ -n $mapped_ip && $mapped_ip == "$tcp_ip" ]] || { printf 'PREFLIGHT udp_stun=false\n'; exit 1; }

epdg_count=$(/usr/sbin/ip netns exec "$ns" timeout 9 dig +time=6 +tries=1 +noall +answer @1.1.1.1 epdg.epc.mnc015.mcc234.pub.3gppnetwork.org A 2>/dev/null | awk '$4=="A"{n++}END{print n+0}' || true)
[[ ${epdg_count:-0} -ge 1 ]] || { printf 'PREFLIGHT epdg_dns=false\n'; exit 1; }
printf 'PREFLIGHT tcp_exit=true country=%s udp_stun=true epdg_dns=true rf=off\n' "$expected_loc"

"$redactor" <"$hil_dir/charon.pipe" >"$hil_dir/safe-events.log" &
logger_pid=$!
/usr/sbin/ip netns exec "$ns" env STRONGSWAN_CONF="$hil_dir/strongswan.conf" /usr/sbin/charon-systemd >/dev/null 2>&1 &
charon_pid=$!
for _ in $(seq 1 50); do
  [[ -S $hil_dir/charon.vici ]] && break
  if ! kill -0 "$charon_pid" 2>/dev/null; then
    printf 'STRONGSWAN daemon_ready=false process_exited=true\n'
    kill -TERM "$logger_pid" 2>/dev/null || true
    sleep 0.2
    tail -n 120 "$hil_dir/safe-events.log" 2>/dev/null || true
    exit 1
  fi
  sleep 0.1
done
if [[ ! -S $hil_dir/charon.vici ]]; then
  printf 'STRONGSWAN daemon_ready=false vici_timeout=true\n'
  exit 1
fi
printf 'STRONGSWAN daemon_ready=true\n'

set +e
timeout 60 "$vici_control" >"$hil_dir/safe-vici.json" 2>/dev/null
initiate_rc=$?
set -e
sleep 1

state_count=$(/usr/sbin/ip netns exec "$ns" ip xfrm state count 2>/dev/null | awk '{for(i=1;i<=NF;i++)if($i~/^[0-9]+$/){print $i;exit}}')
policy_counts=$(/usr/sbin/ip netns exec "$ns" ip xfrm policy count 2>/dev/null || true)
policy_in=$(printf '%s\n' "$policy_counts" | awk '{for(i=1;i<NF;i++)if($i=="IN" && $(i+1)~/^[0-9]+$/){print $(i+1);exit}}')
policy_out=$(printf '%s\n' "$policy_counts" | awk '{for(i=1;i<NF;i++)if($i=="OUT" && $(i+1)~/^[0-9]+$/){print $(i+1);exit}}')
inner_ipv4_count=$(/usr/sbin/ip netns exec "$ns" ip -o -4 addr show 2>/dev/null | awk '$4 !~ /^127\./ && $4 !~ /^169\.254\.248\./{n++}END{print n+0}')
inner_ipv4_address=$(/usr/sbin/ip netns exec "$ns" ip -o -4 addr show 2>/dev/null | awk '$4 !~ /^127\./ && $4 !~ /^169\.254\.248\./{split($4,address,"/");print address[1];exit}')
[[ ${inner_ipv4_count:-0} -ge 1 ]] && inner_ipv4=true || inner_ipv4=false
[[ $initiate_rc -eq 0 ]] && ike_established=true || ike_established=false
[[ ${state_count:-0} -ge 2 && ${policy_in:-0} -ge 1 && ${policy_out:-0} -ge 1 ]] && child_installed=true || child_installed=false

pcscf_result='{"targets":0,"reachable":false}'
ims_result='{"stage":"not-run"}'
xfrm_before='unavailable'
xfrm_after='unavailable'
xfrm_errors_before='none'
xfrm_errors_after='none'
if [[ $child_installed == true && -n $inner_ipv4_address ]]; then
  mapfile -t pcscf_addresses < <(awk '/received P-CSCF server IP / && !seen[$NF]++ {print $NF}' "$hil_dir/safe-events.log" | head -4)
	if [[ ${#pcscf_addresses[@]} -ge 1 ]]; then
    pcscf_args=()
    for address in "${pcscf_addresses[@]}"; do
      pcscf_args+=(--target "$address")
    done
		pcscf_result=$(/usr/sbin/ip netns exec "$ns" "$pcscf_probe" --source "$inner_ipv4_address" --home-domain "$ims_home_domain" "${pcscf_args[@]}" 2>/dev/null || printf '{"targets":0,"reachable":false}')
		xfrm_before=$(/usr/sbin/ip netns exec "$ns" ip -s xfrm state 2>/dev/null | awk '/lifetime current:/{getline;gsub(/^[[:space:]]+|[[:space:]]+$/,"");printf "%s%s",separator,$0;separator="|"}' || true)
		xfrm_errors_before=$(/usr/sbin/ip netns exec "$ns" awk '$2 != 0{printf "%s%s=%s",separator,$1,$2;separator="|"}END{if(separator=="")printf "none"}' /proc/net/xfrm_stat 2>/dev/null || true)
		ims_result=$(/usr/sbin/ip netns exec "$ns" "$ims_probe" --source "$inner_ipv4_address" --pcscf "${pcscf_addresses[0]}" 2>/dev/null || true)
		[[ -n $ims_result ]] || ims_result='{"stage":"no-result"}'
		xfrm_after=$(/usr/sbin/ip netns exec "$ns" ip -s xfrm state 2>/dev/null | awk '/lifetime current:/{getline;gsub(/^[[:space:]]+|[[:space:]]+$/,"");printf "%s%s",separator,$0;separator="|"}' || true)
		xfrm_errors_after=$(/usr/sbin/ip netns exec "$ns" awk '$2 != 0{printf "%s%s=%s",separator,$1,$2;separator="|"}END{if(separator=="")printf "none"}' /proc/net/xfrm_stat 2>/dev/null || true)
	fi
fi

kill -TERM "$logger_pid" 2>/dev/null || true
sleep 0.2
sed 's/^/VICI /' "$hil_dir/safe-vici.json" 2>/dev/null | tail -n 20 || true
printf 'PCSCF %s\n' "$pcscf_result"
printf 'IMS %s\n' "$ims_result"
printf 'XFRM_COUNTERS before=%s after=%s\n' "$xfrm_before" "$xfrm_after"
printf 'XFRM_ERRORS before=%s after=%s\n' "$xfrm_errors_before" "$xfrm_errors_after"
awk '!/\[JOB\]/ && !/vici client/ && !/watcher/ && !/events on fds/ && !/requests: (stats|get-authorities|get-pools|get-conns|list-sas)/' "$hil_dir/safe-events.log" 2>/dev/null | tail -n 160 || true
printf 'RESULT initiate_rc=%s ike_established=%s child_installed=%s xfrm_states=%s xfrm_policy_in=%s xfrm_policy_out=%s inner_ipv4=%s\n' \
	"$initiate_rc" "$ike_established" "$child_installed" "${state_count:-0}" "${policy_in:-0}" "${policy_out:-0}" "$inner_ipv4"

[[ $initiate_rc -eq 0 && $ike_established == true && $child_installed == true && $inner_ipv4 == true && $ims_result == *'"registered":true'* ]]
