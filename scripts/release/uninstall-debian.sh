#!/usr/bin/env bash
set -euo pipefail
[[ $(id -u) == 0 ]] || { printf 'run as root\n' >&2; exit 2; }
systemctl disable --now simplusd.service simplus-netd.service simplus-agent.service simplus-ml307a-bind.service 2>/dev/null || true
rm -f /etc/systemd/system/simplusd.service /etc/systemd/system/simplus-netd.service /etc/systemd/system/simplus-agent.service /etc/systemd/system/simplus-ml307a-bind.service
rm -f /etc/systemd/system/simplus-netd.service.d/20-vowifi-capabilities.conf /etc/systemd/system/simplus-agent.service.d/10-sim-aka-hil.conf
rm -f /usr/local/libexec/simplus/simplusd /usr/local/libexec/simplus/simplus-netd /usr/local/libexec/simplus/simplus-agent /usr/local/libexec/simplus/bind-ml307a
rm -f /usr/local/bin/simplusctl
rm -f /usr/lib/ipsec/plugins/libstrongswan-simplus-simaka.so /usr/lib/ipsec/plugins/libstrongswan-p-cscf.so /etc/sysctl.d/90-simplus-vowifi.conf
rm -rf /usr/local/share/simplus/web
systemctl daemon-reload
if [[ ${1:-} == --purge-data ]]; then rm -rf /var/lib/simplus; else printf 'Preserved /var/lib/simplus (pass --purge-data to remove it).\n'; fi
