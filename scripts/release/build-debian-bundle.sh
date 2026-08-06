#!/usr/bin/env bash
set -euo pipefail
umask 077
repo=$(CDPATH= cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)
out=${1:-$repo/.dev/release/debian}
[[ $out == /* ]] || { printf 'bundle path must be absolute\n' >&2; exit 2; }
mkdir -p "$out/bin" "$out/web" "$out/zashboard" "$out/helpers" "$out/packages"
PATH="$repo/.tools/go/bin:$repo/.tools/node/bin:/usr/bin:/bin"
export PATH COREPACK_HOME="$repo/.tools/corepack" GOCACHE="$repo/.tools/go-build-cache" GOMODCACHE="$repo/.tools/go-mod-cache"
go build -trimpath -o "$out/bin/simplusd" "$repo/cmd/simplusd"
go build -trimpath -o "$out/bin/simplus-agent" "$repo/cmd/simplus-agent"
go build -trimpath -o "$out/bin/simplus-netd" "$repo/cmd/simplus-netd"
go build -trimpath -o "$out/bin/simplusctl" "$repo/cmd/simplusctl"
(cd "$repo/web" && corepack pnpm build)
cp -a "$repo/web/dist/." "$out/web/"
cp -a "$repo/third_party/zashboard/." "$out/zashboard/"
bash "$repo/packaging/strongswan-plugins/build-deb.sh" "$out/packages"
bash "$repo/scripts/dev/test-strongswan-plugins-package.sh" "$out/packages"
install -m 0755 "$repo/scripts/release/install-debian.sh" "$out/install-debian.sh"
install -m 0755 "$repo/scripts/release/uninstall-debian.sh" "$out/uninstall-debian.sh"
install -m 0755 "$repo/scripts/release/bind-ml307a.sh" "$out/helpers/bind-ml307a"
printf 'Debian bundle ready: %s\n' "$out"
