#!/usr/bin/env bash
set -euo pipefail

root=$(CDPATH= cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)
artifact_dir=${1:-$root/.dev/packages/strongswan-plugins}
manifest="$artifact_dir/strongswan-plugins.manifest"
lock="$root/packaging/strongswan-plugins/debian-13-amd64.lock"

[[ $artifact_dir == /* ]] || {
  printf 'artifact directory must be absolute\n' >&2
  exit 2
}
[[ -f $manifest && -f $lock ]] || {
  printf 'strongSwan plugin package artifacts are incomplete\n' >&2
  exit 1
}

manifest_value() {
  local key=$1 value
  value=$(sed -n "s/^${key}=//p" "$manifest")
  [[ -n $value && $(grep -c "^${key}=" "$manifest") == 1 ]] || {
    printf 'invalid package manifest field: %s\n' "$key" >&2
    exit 1
  }
  printf '%s' "$value"
}

[[ $(manifest_value FORMAT) == 1 ]] || {
  printf 'unsupported package manifest format\n' >&2
  exit 1
}
package_name=$(manifest_value PACKAGE_NAME)
package_version=$(manifest_value PACKAGE_VERSION)
target_arch=$(manifest_value TARGET_ARCH)
strongswan_source_version=$(manifest_value STRONGSWAN_SOURCE_VERSION)
strongswan_abi=$(manifest_value STRONGSWAN_UPSTREAM_ABI)
strongswan_abi_upper_bound=$(manifest_value STRONGSWAN_ABI_UPPER_BOUND)
source_date_epoch=$(manifest_value SOURCE_DATE_EPOCH)
deb_filename=$(manifest_value DEB_FILENAME)
deb_sha256=$(manifest_value DEB_SHA256)
source_filename=$(manifest_value SOURCE_FILENAME)
source_sha256=$(manifest_value SOURCE_SHA256)

[[ $package_name == simplus-strongswan-plugins &&
   $deb_filename =~ ^[A-Za-z0-9._+-]+\.deb$ &&
   $source_filename =~ ^[A-Za-z0-9._+-]+\.source\.tar\.xz$ &&
   $deb_sha256 =~ ^[0-9a-f]{64}$ && $source_sha256 =~ ^[0-9a-f]{64}$ &&
   $source_date_epoch =~ ^[0-9]+$ ]] || {
  printf 'unsafe or invalid package manifest values\n' >&2
  exit 1
}

deb="$artifact_dir/$deb_filename"
source_archive="$artifact_dir/$source_filename"
printf '%s  %s\n' "$deb_sha256" "$deb" | sha256sum --check --status
printf '%s  %s\n' "$source_sha256" "$source_archive" |
  sha256sum --check --status

[[ $(dpkg-deb --field "$deb" Package) == "$package_name" ]]
[[ $(dpkg-deb --field "$deb" Version) == "$package_version" ]]
[[ $(dpkg-deb --field "$deb" Architecture) == "$target_arch" ]]
[[ $(dpkg-deb --field "$deb" Built-Using) == \
   "strongswan (= $strongswan_source_version)" ]]
depends=$(dpkg-deb --field "$deb" Depends)
for dependency in charon-systemd libcharon-extra-plugins libstrongswan \
  strongswan-libcharon; do
  grep -Fq "$dependency (>= $strongswan_source_version)" <<<"$depends"
  grep -Fq "$dependency (<< $strongswan_abi_upper_bound)" <<<"$depends"
done

work=$(mktemp -d "${TMPDIR:-/tmp}/simplus-strongswan-package-test.XXXXXX")
cleanup() {
  if [[ -n ${work:-} && -d $work ]]; then
    rm -rf -- "$work"
  fi
}
trap cleanup EXIT HUP INT TERM
dpkg-deb --extract "$deb" "$work/root"
dpkg-deb --control "$deb" "$work/control"

simaka="$work/root/usr/lib/ipsec/plugins/libstrongswan-simplus-simaka.so"
pcscf="$work/root/usr/lib/ipsec/plugins/libstrongswan-p-cscf.so"
metadata="$work/root/usr/share/simplus/strongswan-plugins/build-abi"
copyright="$work/root/usr/share/doc/$package_name/copyright"
[[ -f $simaka && -f $pcscf && -f $metadata && -f $copyright ]]
[[ $(stat -c '%a' "$simaka") == 644 && $(stat -c '%a' "$pcscf") == 644 ]]
[[ $(find "$work/control" -maxdepth 1 -type f -printf '%f\n' | sort) == control ]]
[[ $(find "$work/root" -type f | wc -l) == 5 ]]

nm -D "$simaka" | grep -Eq ' T simplus_simaka_plugin_create$'
nm -D "$pcscf" | grep -Eq ' T p_cscf_plugin_create$'
for plugin in "$simaka" "$pcscf"; do
  readelf -d "$plugin" | grep -Fq 'Library runpath: [/usr/lib/ipsec]'
  for dependency in libcharon.so.0 libstrongswan.so.0 libc.so.6; do
    readelf -d "$plugin" | grep -Fq "Shared library: [$dependency]"
  done
done
readelf -d "$simaka" | grep -Fq 'Shared library: [libsimaka.so.0]'
grep -Fxq "source_version=$strongswan_source_version" "$metadata"
grep -Fxq "upstream_abi=$strongswan_abi" "$metadata"
grep -Fxq "abi_upper_bound=$strongswan_abi_upper_bound" "$metadata"
grep -Fxq "target_architecture=$target_arch" "$metadata"
grep -Fxq "package_version=$package_version" "$metadata"
grep -Fxq "source_date_epoch=$source_date_epoch" "$metadata"

archive_listing="$work/source-list"
tar -tJf "$source_archive" | sort >"$archive_listing"
for required in \
  './components/strongswan-simplus-simaka/simplus_simaka_plugin.c' \
  './components/strongswan-simplus-simaka/LICENSE' \
  './packaging/strongswan-plugins/build-deb.sh' \
  './packaging/strongswan-plugins/debian-13-amd64.lock' \
  './rebuild.sh' \
  './scripts/dev/build-simplus-simaka-plugin.sh'; do
  grep -Fxq "$required" "$archive_listing" || {
    printf 'corresponding source is missing %s\n' "$required" >&2
    exit 1
  }
done
tar -xOJf "$source_archive" ./rebuild.sh |
  grep -Fq "export SIMPLUS_DEB_VERSION='$package_version'"
tar -xOJf "$source_archive" ./rebuild.sh |
  grep -Fq "export SOURCE_DATE_EPOCH='$source_date_epoch'"
# shellcheck disable=SC1090
. "$lock"
for input in "$STRONGSWAN_DSC_FILE" "$STRONGSWAN_ORIG_FILE" \
  "$STRONGSWAN_ORIG_SIG_FILE" "$STRONGSWAN_DEBIAN_FILE" \
  "$LIBSTRONGSWAN_DEB_FILE" "$LIBCHARON_DEB_FILE" \
  "$LIBCHARON_EXTRA_DEB_FILE"; do
  grep -Fxq "./packaging/strongswan-plugins/inputs/$input" "$archive_listing" || {
    printf 'corresponding source is missing locked input %s\n' "$input" >&2
    exit 1
  }
done

if grep -aEq '/home/[^/]+/|/Users/[^/]+/|10\.[0-9]+\.[0-9]+\.[0-9]+' \
  "$simaka" "$pcscf" "$manifest"; then
  printf 'package artifacts contain a private build or LAN path\n' >&2
  exit 1
fi

printf 'strongSwan plugin Debian package verified: %s\n' "$deb_filename"
