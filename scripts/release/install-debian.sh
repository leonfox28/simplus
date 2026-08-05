#!/usr/bin/env bash
set -euo pipefail
[[ $(id -u) == 0 ]] || { printf 'run as root\n' >&2; exit 2; }
bundle=$(CDPATH= cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
[[ -x $bundle/bin/simplusd && -x $bundle/bin/simplus-agent && -x $bundle/bin/simplus-netd && -x $bundle/bin/simplusctl && -x $bundle/helpers/bind-ml307a && -f $bundle/helpers/libstrongswan-simplus-simaka.so && -f $bundle/helpers/libstrongswan-p-cscf.so && -f $bundle/web/index.html && -f $bundle/zashboard/dist/index.html && -f $bundle/zashboard/LICENSE ]] || { printf 'incomplete Simplus bundle\n' >&2; exit 1; }
[[ $(dpkg-query -W -f='${Version}' strongswan-libcharon 2>/dev/null || true) == 6.0.1-* ]] || { printf 'Simplus Host VoWiFi requires strongSwan 6.0.1\n' >&2; exit 1; }
for required_path in /usr/sbin/ip /usr/sbin/nft /usr/sbin/charon-systemd /usr/lib/ipsec/plugins/libstrongswan-eap-aka.so; do
  [[ -e $required_path ]] || { printf 'missing Host VoWiFi dependency: %s\n' "$required_path" >&2; exit 1; }
done
for conflicting_service in ModemManager.service simplus-agent-dev.service; do
  if systemctl is-active --quiet "$conflicting_service"; then
    printf 'refusing installation while %s is active; stop it explicitly first\n' "$conflicting_service" >&2
    exit 1
  fi
done
getent group simplus >/dev/null || groupadd --system simplus
getent passwd simplus >/dev/null || useradd --system --gid simplus --home-dir /nonexistent --shell /usr/sbin/nologin simplus
getent passwd simplus-agent >/dev/null || useradd --system --user-group --home-dir /nonexistent --shell /usr/sbin/nologin simplus-agent
getent group dialout >/dev/null && usermod -a -G dialout simplus-agent
install -d -o root -g root -m 0755 /usr/local/libexec/simplus /usr/local/share/simplus/web
install -o root -g root -m 0755 "$bundle/bin/simplusd" /usr/local/libexec/simplus/simplusd
install -o root -g root -m 0755 "$bundle/bin/simplus-agent" /usr/local/libexec/simplus/simplus-agent
install -o root -g root -m 0755 "$bundle/bin/simplus-netd" /usr/local/libexec/simplus/simplus-netd
install -o root -g root -m 0755 "$bundle/helpers/bind-ml307a" /usr/local/libexec/simplus/bind-ml307a
install -o root -g root -m 0755 "$bundle/bin/simplusctl" /usr/local/bin/simplusctl
install -o root -g root -m 0644 "$bundle/helpers/libstrongswan-simplus-simaka.so" /usr/lib/ipsec/plugins/libstrongswan-simplus-simaka.so
install -o root -g root -m 0644 "$bundle/helpers/libstrongswan-p-cscf.so" /usr/lib/ipsec/plugins/libstrongswan-p-cscf.so
cp -a "$bundle/web/." /usr/local/share/simplus/web/
chown -R root:root /usr/local/share/simplus/web
find /usr/local/share/simplus/web -type d -exec chmod 0755 {} +
find /usr/local/share/simplus/web -type f -exec chmod 0644 {} +
install -d -o simplus -g simplus -m 0700 /var/lib/simplus /var/lib/simplus/mihomo /var/lib/simplus/mihomo/runtime /var/lib/simplus/mihomo/runtime/ui
cp -a "$bundle/zashboard/dist/." /var/lib/simplus/mihomo/runtime/ui/
chown -R simplus:simplus /var/lib/simplus/mihomo/runtime/ui
find /var/lib/simplus/mihomo/runtime/ui -type d -exec chmod 0755 {} +
find /var/lib/simplus/mihomo/runtime/ui -type f -exec chmod 0644 {} +
simplus_uid=$(id -u simplus); simplus_gid=$(getent group simplus|cut -d: -f3)
display_host=${SIMPLUS_LISTEN_HOST:-$(ip -4 route get 1.1.1.1 2>/dev/null | awk '{for (i=1; i<=NF; i++) if ($i == "src") {print $(i+1); exit}}')}
tmp=$(mktemp -d /tmp/simplus-install.XXXXXX); trap 'rm -rf "$tmp"' EXIT
cat >"$tmp/90-simplus-vowifi.conf" <<EOF
# Required only by an explicitly selected direct Host VoWiFi egress.
net.ipv4.ip_forward = 1
EOF
install -o root -g root -m 0644 "$tmp/90-simplus-vowifi.conf" /etc/sysctl.d/90-simplus-vowifi.conf
/usr/sbin/sysctl -p /etc/sysctl.d/90-simplus-vowifi.conf >/dev/null
cat >"$tmp/simplus-agent.service" <<EOF
[Unit]
Description=Simplus read-only hardware Agent
Requires=simplus-ml307a-bind.service
After=local-fs.target simplus-ml307a-bind.service
[Service]
Type=simple
User=simplus-agent
Group=simplus
SupplementaryGroups=dialout
RuntimeDirectory=simplus-agent
RuntimeDirectoryMode=0750
StateDirectory=simplus-agent
StateDirectoryMode=0700
UMask=0077
ExecStart=/usr/local/libexec/simplus/simplus-agent --socket /run/simplus-agent/simplus-agent.sock --sim-aka-socket /run/simplus-agent/sim-aka.sock --identity-key /var/lib/simplus-agent/.identity-key-v1 --directory-mode 0750 --socket-mode 0660 --socket-gid $simplus_gid --allowed-uid 0 --allowed-uid $simplus_uid
Restart=on-failure
NoNewPrivileges=true
CapabilityBoundingSet=
PrivateNetwork=true
ProtectSystem=strict
ProtectHome=true
RestrictAddressFamilies=AF_UNIX
[Install]
WantedBy=multi-user.target
EOF
cat >"$tmp/simplus-ml307a-bind.service" <<EOF
[Unit]
Description=Register the ML307A USB serial interfaces
After=systemd-modules-load.service
Before=simplus-agent.service
[Service]
Type=oneshot
ExecStart=/usr/local/libexec/simplus/bind-ml307a
RemainAfterExit=yes
[Install]
WantedBy=multi-user.target
EOF
cat >"$tmp/simplusd.service" <<EOF
[Unit]
Description=Simplus trusted-LAN control plane
Wants=network-online.target
After=network-online.target simplus-agent.service simplus-netd.service
Requires=simplus-agent.service simplus-netd.service
[Service]
Type=simple
User=simplus
Group=simplus
StateDirectory=simplus
StateDirectoryMode=0700
UMask=0077
Environment=SIMPLUS_BACKEND=hardware
Environment=SIMPLUS_AGENT_SOCKET=/run/simplus-agent/simplus-agent.sock
Environment=SIMPLUS_DATA_ROOT=/var/lib/simplus
Environment=SIMPLUS_LISTEN_ADDR=0.0.0.0:8080
Environment=SIMPLUS_WEB_ROOT=/usr/local/share/simplus/web
Environment=SIMPLUS_MIHOMO_SUPERVISOR_SOCKET=/run/simplus-netd/mihomo.sock
ExecStart=/usr/local/libexec/simplus/simplusd
Restart=on-failure
RestartSec=5s
NoNewPrivileges=true
CapabilityBoundingSet=
AmbientCapabilities=
ProtectSystem=strict
ProtectHome=true
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
[Install]
WantedBy=multi-user.target
EOF
cat >"$tmp/simplus-netd.service" <<EOF
[Unit]
Description=Simplus restricted Mihomo and Host VoWiFi network supervisor
Wants=network-online.target
After=local-fs.target network-online.target
[Service]
Type=simple
User=root
Group=simplus
RuntimeDirectory=simplus-netd
RuntimeDirectoryMode=0750
UMask=0077
ExecStart=/usr/local/libexec/simplus/simplus-netd --socket /run/simplus-netd/mihomo.sock --mihomo-root /var/lib/simplus/mihomo --vowifi-root /run/simplus-netd/vowifi --service-uid $simplus_uid --service-gid $simplus_gid
Restart=on-failure
NoNewPrivileges=true
# UDP TPROXY replies are emitted from the original destination address and
# port. DNS (53) and IKE (500) therefore require the narrowly scoped ability
# to bind a privileged source port in addition to transparent-socket caps.
# SETUID/SETGID are used only by the root supervisor while dropping the
# validated Mihomo child to the simplus identity. They are not included in the
# child's ambient capability set.
CapabilityBoundingSet=CAP_DAC_OVERRIDE CAP_KILL CAP_SETGID CAP_SETUID CAP_NET_ADMIN CAP_NET_RAW CAP_NET_BIND_SERVICE CAP_SYS_ADMIN
AmbientCapabilities=CAP_DAC_OVERRIDE CAP_KILL CAP_SETGID CAP_SETUID CAP_NET_ADMIN CAP_NET_RAW CAP_NET_BIND_SERVICE CAP_SYS_ADMIN
ProtectSystem=strict
ProtectHome=true
PrivateDevices=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
ReadWritePaths=/var/lib/simplus/mihomo /run/simplus-netd
RestrictNamespaces=net mnt
RestrictSUIDSGID=true
LockPersonality=true
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6 AF_NETLINK
[Install]
WantedBy=multi-user.target
EOF
install -o root -g root -m 0644 "$tmp/simplus-agent.service" "$tmp/simplus-ml307a-bind.service" "$tmp/simplus-netd.service" "$tmp/simplusd.service" /etc/systemd/system/
# Remove obsolete development overrides now represented by the production
# units. Leaving the old netd capability reset in place would fail closed on
# namespace creation after an upgrade.
rm -f /etc/systemd/system/simplus-netd.service.d/20-vowifi-capabilities.conf
rm -f /etc/systemd/system/simplus-agent.service.d/10-sim-aka-hil.conf
systemctl daemon-reload
systemctl enable simplus-ml307a-bind.service simplus-agent.service simplus-netd.service simplusd.service
systemctl restart simplus-ml307a-bind.service
systemctl restart simplus-agent.service
systemctl restart simplus-netd.service
systemctl restart simplusd.service
for _ in $(seq 1 50); do
  [[ -S /var/lib/simplus/run/simplusd-control.sock ]] && break
  sleep 0.1
done
[[ -S /var/lib/simplus/run/simplusd-control.sock ]] || { printf 'simplusd control socket did not become ready\n' >&2; exit 1; }
if [[ -n $display_host ]]; then
  printf 'Simplus installed for trusted-LAN access on http://%s:8080\n' "$display_host"
else
  printf 'Simplus installed; open http://<host-lan-ip>:8080 from the trusted LAN\n'
fi
/usr/local/bin/simplusctl provision-admin --socket /var/lib/simplus/run/simplusd-control.sock --username simplus_admin --locale zh-CN
