#!/bin/sh
set -eu

fail() {
    printf 'simplus data initialization: %s\n' "$*" >&2
    exit 1
}

[ "$(id -u)" = 0 ] || fail 'data initialization must run as root'
root=/data
[ ! -L "$root" ] && [ -d "$root" ] || fail '/data must be a real bind-mounted directory'

prepare_directory() {
    path=$1
    owner=$2
    group=$3
    mode=$4
    [ ! -L "$path" ] || fail "$path must not be a symbolic link"
    if [ ! -e "$path" ]; then
        mkdir -m "$mode" "$path"
    fi
    [ -d "$path" ] || fail "$path must be a directory"
    chown "$owner:$group" "$path"
    chmod "$mode" "$path"
}

prepare_directory "$root/core" 10001 10001 0700
prepare_directory "$root/agent" 10002 10002 0700
prepare_directory "$root/core/mihomo" 10001 10001 0700
prepare_directory "$root/core/mihomo/runtime" 10001 10001 0700
prepare_directory "$root/core/mihomo/runtime/ui" 10001 10001 0755
prepare_directory "$root/core/mihomo/versions" 10001 10001 0700

ui=$root/core/mihomo/runtime/ui
[ "$ui" = '/data/core/mihomo/runtime/ui' ] || fail 'refusing an unexpected Zashboard target'
source_ui=/usr/share/simplus/zashboard
source_version=$(cat "$source_ui/VERSION")
installed_version=
if [ -f "$ui/VERSION" ]; then
    installed_version=$(cat "$ui/VERSION")
fi
if [ "$installed_version" != "$source_version" ] || [ ! -f "$ui/index.html" ]; then
    find "$ui" -mindepth 1 -maxdepth 1 -exec rm -rf -- {} +
    cp -a "$source_ui/." "$ui/"
fi

mihomo_root=$root/core/mihomo
mihomo_seed=/usr/share/simplus/mihomo
mihomo_version=$(cat "$mihomo_seed/VERSION")
mihomo_archive_sha256=$(cat "$mihomo_seed/ARCHIVE_SHA256")
mihomo_binary_sha256=$(cat "$mihomo_seed/BINARY_SHA256")
printf '%s' "$mihomo_version" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$' || \
    fail 'bundled Mihomo version metadata is invalid'
printf '%s' "$mihomo_archive_sha256" | grep -Eq '^[0-9a-f]{64}$' || \
    fail 'bundled Mihomo archive digest is invalid'
printf '%s' "$mihomo_binary_sha256" | grep -Eq '^[0-9a-f]{64}$' || \
    fail 'bundled Mihomo binary digest is invalid'
seed_binary=$mihomo_seed/$mihomo_version/mihomo
[ ! -L "$seed_binary" ] && [ -x "$seed_binary" ] || fail 'bundled Mihomo binary is unavailable'
actual_seed_sha256=$(sha256sum "$seed_binary" | cut -d ' ' -f 1)
[ "$actual_seed_sha256" = "$mihomo_binary_sha256" ] || fail 'bundled Mihomo binary digest does not match'

core_manifest=$mihomo_root/current.json
if [ -e "$core_manifest" ] || [ -L "$core_manifest" ]; then
    [ ! -L "$core_manifest" ] && [ -f "$core_manifest" ] || \
        fail 'Mihomo current manifest must be a real regular file'
else
    existing_version=$(find "$mihomo_root/versions" -mindepth 1 -maxdepth 1 -print -quit)
    target_version=$mihomo_root/versions/$mihomo_version
    if [ -z "$existing_version" ]; then
        mkdir -m 0700 "$target_version"
        install -o 10001 -g 10001 -m 0700 "$seed_binary" "$target_version/mihomo"
    else
        [ "$existing_version" = "$target_version" ] && [ ! -L "$target_version" ] && \
            [ -d "$target_version" ] || \
            fail 'Mihomo versions exist without a current manifest; refusing to guess active state'
        version_entries=$(find "$target_version" -mindepth 1 -maxdepth 1 -print | wc -l)
        [ "$version_entries" = 1 ] && [ ! -L "$target_version/mihomo" ] && \
            [ -x "$target_version/mihomo" ] || \
            fail 'incomplete Mihomo seed state cannot be resumed safely'
        actual_target_sha256=$(sha256sum "$target_version/mihomo" | cut -d ' ' -f 1)
        [ "$actual_target_sha256" = "$mihomo_binary_sha256" ] || \
            fail 'existing Mihomo seed binary digest does not match'
    fi
    installed_at=$(date -u '+%Y-%m-%dT%H:%M:%SZ')
    manifest_tmp=$(mktemp "$mihomo_root/.current.json.XXXXXX")
    printf '{"installed":true,"version":"%s","architecture":"amd64","sha256":"%s","installedAt":"%s"}\n' \
        "$mihomo_version" "$mihomo_archive_sha256" "$installed_at" >"$manifest_tmp"
    chown 10001:10001 "$manifest_tmp"
    chmod 0600 "$manifest_tmp"
    mv "$manifest_tmp" "$core_manifest"
fi

chown -R 10001:10001 "$root/core"
chown -R 10002:10002 "$root/agent"
chmod 0700 "$root/core" "$root/agent" "$root/core/mihomo" "$root/core/mihomo/runtime" "$root/core/mihomo/versions"
find "$ui" -type d -exec chmod 0755 {} +
find "$ui" -type f -exec chmod 0644 {} +

printf 'Simplus data directories initialized\n'
