#!/usr/bin/env bash
set -euo pipefail
umask 077
repo=$(CDPATH= cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)
out=${1:-$repo/.dev/release/debian}
[[ $out == /* ]] || { printf 'bundle path must be absolute\n' >&2; exit 2; }
mkdir -p "$out/bin" "$out/web" "$out/zashboard" "$out/helpers"
PATH="$repo/.tools/go/bin:$repo/.tools/node/bin:/usr/bin:/bin"
export PATH COREPACK_HOME="$repo/.tools/corepack" GOCACHE="$repo/.tools/go-build-cache" GOMODCACHE="$repo/.tools/go-mod-cache"
go build -trimpath -o "$out/bin/simplusd" "$repo/cmd/simplusd"
go build -trimpath -o "$out/bin/simplus-agent" "$repo/cmd/simplus-agent"
go build -trimpath -o "$out/bin/simplus-netd" "$repo/cmd/simplus-netd"
go build -trimpath -o "$out/bin/simplusctl" "$repo/cmd/simplusctl"
(cd "$repo/web" && corepack pnpm build)
cp -a "$repo/web/dist/." "$out/web/"
cp -a "$repo/third_party/zashboard/." "$out/zashboard/"
: "${SIMPLUS_STRONGSWAN_SOURCE:?set SIMPLUS_STRONGSWAN_SOURCE to the reviewed strongSwan 6.0.1 source tree}"
: "${SIMPLUS_STRONGSWAN_BUILD:?set SIMPLUS_STRONGSWAN_BUILD to its configured build tree}"
bash "$repo/scripts/dev/build-simplus-simaka-plugin.sh" \
  "$SIMPLUS_STRONGSWAN_SOURCE" "$SIMPLUS_STRONGSWAN_BUILD" \
  "$out/helpers/libstrongswan-simplus-simaka.so"
bash "$repo/scripts/dev/build-strongswan-p-cscf-plugin.sh" \
  "$SIMPLUS_STRONGSWAN_SOURCE" "$SIMPLUS_STRONGSWAN_BUILD" \
  "$out/helpers/libstrongswan-p-cscf.so"
install -m 0755 "$repo/scripts/release/install-debian.sh" "$out/install-debian.sh"
install -m 0755 "$repo/scripts/release/uninstall-debian.sh" "$out/uninstall-debian.sh"
install -m 0755 "$repo/scripts/release/bind-ml307a.sh" "$out/helpers/bind-ml307a"
printf 'Debian bundle ready: %s\n' "$out"
