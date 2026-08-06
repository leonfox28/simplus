#!/usr/bin/env bash
set -euo pipefail
umask 022

repo=$(CDPATH= cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)
package_dir="$repo/packaging/strongswan-plugins"
lock_file="$package_dir/debian-13-amd64.lock"
output_dir=${1:-$repo/.dev/packages/strongswan-plugins}

[[ $output_dir == /* ]] || {
  printf 'output directory must be absolute: %s\n' "$output_dir" >&2
  exit 2
}

for command in cc curl dpkg dpkg-deb dpkg-parsechangelog dpkg-source git install \
  mktemp nm readelf sha256sum tar touch xz; do
  command -v "$command" >/dev/null || {
    printf 'missing strongSwan plugin build command: %s\n' "$command" >&2
    exit 1
  }
done

if [[ ! -r /etc/os-release ]]; then
  printf 'the strongSwan plugin package must be built on Debian 13\n' >&2
  exit 1
fi
# shellcheck disable=SC1091
. /etc/os-release
[[ ${ID:-} == debian && ${VERSION_ID:-} == 13* ]] || {
  printf 'unsupported package build host: expected Debian 13, got %s %s\n' \
    "${ID:-unknown}" "${VERSION_ID:-unknown}" >&2
  exit 1
}

[[ -r $lock_file ]] || {
  printf 'missing strongSwan input lock: %s\n' "$lock_file" >&2
  exit 1
}
# The lock is repository-owned build data, not an operator-provided file.
# shellcheck disable=SC1090
. "$lock_file"

required_lock_variables=(
  LOCK_FORMAT TARGET_DISTRIBUTION TARGET_RELEASE TARGET_ARCH
  STRONGSWAN_UPSTREAM_VERSION STRONGSWAN_SOURCE_VERSION
  STRONGSWAN_ABI_UPPER_BOUND
  STRONGSWAN_DSC_FILE STRONGSWAN_DSC_URL STRONGSWAN_DSC_SHA256
  STRONGSWAN_ORIG_FILE STRONGSWAN_ORIG_URL STRONGSWAN_ORIG_SHA256
  STRONGSWAN_ORIG_SIG_FILE STRONGSWAN_ORIG_SIG_URL STRONGSWAN_ORIG_SIG_SHA256
  STRONGSWAN_DEBIAN_FILE STRONGSWAN_DEBIAN_URL STRONGSWAN_DEBIAN_SHA256
  LIBSTRONGSWAN_DEB_FILE LIBSTRONGSWAN_DEB_URL LIBSTRONGSWAN_DEB_SHA256
  LIBCHARON_DEB_FILE LIBCHARON_DEB_URL LIBCHARON_DEB_SHA256
  LIBCHARON_EXTRA_DEB_FILE LIBCHARON_EXTRA_DEB_URL LIBCHARON_EXTRA_DEB_SHA256
)
for variable in "${required_lock_variables[@]}"; do
  [[ -n ${!variable:-} ]] || {
    printf 'strongSwan input lock is missing %s\n' "$variable" >&2
    exit 1
  }
done
[[ $LOCK_FORMAT == 1 && $TARGET_DISTRIBUTION == debian && $TARGET_RELEASE == 13 ]] || {
  printf 'unsupported strongSwan lock format or target\n' >&2
  exit 1
}
dpkg --validate-version "$STRONGSWAN_SOURCE_VERSION" >/dev/null
dpkg --validate-version "$STRONGSWAN_ABI_UPPER_BOUND" >/dev/null
dpkg --compare-versions "$STRONGSWAN_SOURCE_VERSION" lt \
  "$STRONGSWAN_ABI_UPPER_BOUND" || {
  printf 'invalid strongSwan ABI version range\n' >&2
  exit 1
}

architecture=$(dpkg --print-architecture)
[[ $architecture == "$TARGET_ARCH" ]] || {
  printf 'strongSwan lock targets %s but build host is %s\n' \
    "$TARGET_ARCH" "$architecture" >&2
  exit 1
}

for file_variable in STRONGSWAN_DSC_FILE STRONGSWAN_ORIG_FILE \
  STRONGSWAN_ORIG_SIG_FILE STRONGSWAN_DEBIAN_FILE LIBSTRONGSWAN_DEB_FILE \
  LIBCHARON_DEB_FILE LIBCHARON_EXTRA_DEB_FILE; do
  [[ ${!file_variable} =~ ^[A-Za-z0-9._+-]+$ ]] || {
    printf 'unsafe locked input filename in %s\n' "$file_variable" >&2
    exit 1
  }
done
for url_variable in STRONGSWAN_DSC_URL STRONGSWAN_ORIG_URL \
  STRONGSWAN_ORIG_SIG_URL STRONGSWAN_DEBIAN_URL LIBSTRONGSWAN_DEB_URL \
  LIBCHARON_DEB_URL LIBCHARON_EXTRA_DEB_URL; do
  [[ ${!url_variable} == https://deb.debian.org/debian/* ]] || {
    printf 'unapproved locked input URL in %s\n' "$url_variable" >&2
    exit 1
  }
done
for hash_variable in STRONGSWAN_DSC_SHA256 STRONGSWAN_ORIG_SHA256 \
  STRONGSWAN_ORIG_SIG_SHA256 STRONGSWAN_DEBIAN_SHA256 \
  LIBSTRONGSWAN_DEB_SHA256 LIBCHARON_DEB_SHA256 \
  LIBCHARON_EXTRA_DEB_SHA256; do
  [[ ${!hash_variable} =~ ^[0-9a-f]{64}$ ]] || {
    printf 'invalid SHA-256 in %s\n' "$hash_variable" >&2
    exit 1
  }
done

if [[ -n ${SIMPLUS_DEB_VERSION:-} ]]; then
  package_version=$SIMPLUS_DEB_VERSION
elif git -C "$repo" rev-parse --verify HEAD >/dev/null 2>&1; then
  commit=$(git -C "$repo" rev-parse --short=12 HEAD)
  package_version="0.0.0+git${commit}-1"
  if ! git -C "$repo" diff --quiet --no-ext-diff ||
    ! git -C "$repo" diff --cached --quiet --no-ext-diff; then
    package_version="${package_version}.dirty"
  fi
else
  package_version=0.0.0+source1-1
fi
dpkg --validate-version "$package_version" >/dev/null

if [[ -n ${SOURCE_DATE_EPOCH:-} ]]; then
  [[ $SOURCE_DATE_EPOCH =~ ^[0-9]+$ ]] || {
    printf 'SOURCE_DATE_EPOCH must be a non-negative integer\n' >&2
    exit 2
  }
elif git -C "$repo" rev-parse --verify HEAD >/dev/null 2>&1; then
  SOURCE_DATE_EPOCH=$(git -C "$repo" log -1 --format=%ct)
else
  SOURCE_DATE_EPOCH=0
fi
export SOURCE_DATE_EPOCH

cache_dir=${SIMPLUS_STRONGSWAN_CACHE:-$repo/.dev/cache/strongswan-plugins}
[[ $cache_dir == /* ]] || {
  printf 'SIMPLUS_STRONGSWAN_CACHE must be an absolute path\n' >&2
  exit 2
}
install -d -m 0755 "$cache_dir" "$output_dir"

work=$(mktemp -d "${TMPDIR:-/tmp}/simplus-strongswan-plugins.XXXXXX")
cleanup() {
  if [[ -n ${work:-} && -d $work ]]; then
    rm -rf -- "$work"
  fi
}
trap cleanup EXIT HUP INT TERM
install -d -m 0755 "$work/inputs" "$work/sysroot" "$work/objects"

fetch_input() {
  local file=$1 url=$2 expected=$3 source candidate actual
  source="$package_dir/inputs/$file"
  candidate="$cache_dir/$file"
  if [[ -f $source ]]; then
    candidate=$source
  fi
  if [[ -f $candidate ]]; then
    actual=$(sha256sum "$candidate" | awk '{print $1}')
    if [[ $actual != "$expected" ]]; then
      if [[ $candidate == "$source" ]]; then
        printf 'bundled strongSwan input checksum mismatch: %s\n' "$file" >&2
        exit 1
      fi
      rm -f -- "$candidate"
    fi
  fi
  if [[ ! -f $candidate ]]; then
    candidate="$cache_dir/$file"
    local partial="$candidate.partial.$$"
    curl --proto '=https' --tlsv1.2 --fail --location --silent --show-error \
      --retry 3 --retry-all-errors --output "$partial" "$url"
    actual=$(sha256sum "$partial" | awk '{print $1}')
    [[ $actual == "$expected" ]] || {
      rm -f -- "$partial"
      printf 'downloaded strongSwan input checksum mismatch: %s\n' "$file" >&2
      exit 1
    }
    mv -f -- "$partial" "$candidate"
  fi
  printf '%s  %s\n' "$expected" "$candidate" | sha256sum --check --status
  install -m 0644 "$candidate" "$work/inputs/$file"
}

fetch_input "$STRONGSWAN_DSC_FILE" "$STRONGSWAN_DSC_URL" "$STRONGSWAN_DSC_SHA256"
fetch_input "$STRONGSWAN_ORIG_FILE" "$STRONGSWAN_ORIG_URL" "$STRONGSWAN_ORIG_SHA256"
fetch_input "$STRONGSWAN_ORIG_SIG_FILE" "$STRONGSWAN_ORIG_SIG_URL" "$STRONGSWAN_ORIG_SIG_SHA256"
fetch_input "$STRONGSWAN_DEBIAN_FILE" "$STRONGSWAN_DEBIAN_URL" "$STRONGSWAN_DEBIAN_SHA256"
fetch_input "$LIBSTRONGSWAN_DEB_FILE" "$LIBSTRONGSWAN_DEB_URL" "$LIBSTRONGSWAN_DEB_SHA256"
fetch_input "$LIBCHARON_DEB_FILE" "$LIBCHARON_DEB_URL" "$LIBCHARON_DEB_SHA256"
fetch_input "$LIBCHARON_EXTRA_DEB_FILE" "$LIBCHARON_EXTRA_DEB_URL" "$LIBCHARON_EXTRA_DEB_SHA256"

dpkg-source --no-check -x "$work/inputs/$STRONGSWAN_DSC_FILE" "$work/source" \
  >"$work/dpkg-source.log" 2>&1
actual_source_version=$(dpkg-parsechangelog \
  -l"$work/source/debian/changelog" -SVersion)
[[ $actual_source_version == "$STRONGSWAN_SOURCE_VERSION" ]] || {
  printf 'extracted strongSwan source version mismatch: %s\n' \
    "$actual_source_version" >&2
  exit 1
}
grep -Fq "AC_INIT([strongSwan],[$STRONGSWAN_UPSTREAM_VERSION])" \
  "$work/source/configure.ac" || {
  printf 'extracted strongSwan upstream version mismatch\n' >&2
  exit 1
}

for package in "$LIBSTRONGSWAN_DEB_FILE" "$LIBCHARON_DEB_FILE" \
  "$LIBCHARON_EXTRA_DEB_FILE"; do
  package_version_input=$(dpkg-deb --field "$work/inputs/$package" Version)
  package_architecture_input=$(dpkg-deb --field "$work/inputs/$package" Architecture)
  [[ $package_version_input == "$STRONGSWAN_SOURCE_VERSION" &&
     $package_architecture_input == "$TARGET_ARCH" ]] || {
    printf 'locked runtime ABI input does not match source/architecture: %s\n' \
      "$package" >&2
    exit 1
  }
  dpkg-deb --extract "$work/inputs/$package" "$work/sysroot"
done

install -d -m 0755 "$work/build"
if ! (cd "$work/build" && "$work/source/configure" \
  --prefix=/usr --libdir=/usr/lib --libexecdir=/usr/lib --sysconfdir=/etc \
  --disable-defaults --enable-charon --enable-ikev2 --enable-eap-aka \
  --enable-p-cscf --with-capabilities=native >configure.log 2>&1); then
  tail -n 120 "$work/build/configure.log" >&2
  exit 1
fi

SIMPLUS_STRONGSWAN_LIBDIR="$work/sysroot/usr/lib/ipsec" \
  bash "$repo/scripts/dev/build-simplus-simaka-plugin.sh" \
    "$work/source" "$work/build" \
    "$work/libstrongswan-simplus-simaka.so" >/dev/null
SIMPLUS_STRONGSWAN_LIBDIR="$work/sysroot/usr/lib/ipsec" \
  bash "$repo/scripts/dev/build-strongswan-p-cscf-plugin.sh" \
    "$work/source" "$work/build" \
    "$work/libstrongswan-p-cscf.so" >/dev/null

nm -D "$work/libstrongswan-simplus-simaka.so" |
  grep -Eq ' T simplus_simaka_plugin_create$' || {
  printf 'Simplus SIM AKA plugin constructor is missing\n' >&2
  exit 1
}
nm -D "$work/libstrongswan-p-cscf.so" |
  grep -Eq ' T p_cscf_plugin_create$' || {
  printf 'p-cscf plugin constructor is missing\n' >&2
  exit 1
}
for plugin in libstrongswan-simplus-simaka.so libstrongswan-p-cscf.so; do
  for dependency in libcharon.so.0 libstrongswan.so.0 libc.so.6; do
    readelf -d "$work/$plugin" | grep -Fq "Shared library: [$dependency]" || {
      printf '%s dependency is missing: %s\n' "$plugin" "$dependency" >&2
      exit 1
    }
  done
  readelf -d "$work/$plugin" |
    grep -Fq 'Library runpath: [/usr/lib/ipsec]' || {
    printf '%s has an unexpected runtime library path\n' "$plugin" >&2
    exit 1
  }
done
readelf -d "$work/libstrongswan-simplus-simaka.so" |
  grep -Fq 'Shared library: [libsimaka.so.0]' || {
  printf 'Simplus SIM AKA plugin dependency is missing: libsimaka.so.0\n' >&2
  exit 1
}
if grep -aFq "$work" "$work/libstrongswan-simplus-simaka.so" ||
  grep -aFq "$work" "$work/libstrongswan-p-cscf.so"; then
  printf 'strongSwan plugin contains a temporary build path\n' >&2
  exit 1
fi

package_name=simplus-strongswan-plugins
package_root="$work/package"
plugin_dir="$package_root/usr/lib/ipsec/plugins"
doc_dir="$package_root/usr/share/doc/$package_name"
metadata_dir="$package_root/usr/share/simplus/strongswan-plugins"
install -d -m 0755 "$package_root/DEBIAN" "$plugin_dir" "$doc_dir" "$metadata_dir"
install -m 0644 "$work/libstrongswan-simplus-simaka.so" "$plugin_dir/"
install -m 0644 "$work/libstrongswan-p-cscf.so" "$plugin_dir/"
install -m 0644 "$package_dir/copyright" "$doc_dir/copyright"
install -m 0644 "$lock_file" "$doc_dir/debian-13-amd64.lock"
lock_sha256=$(sha256sum "$lock_file" | awk '{print $1}')

cat >"$package_root/DEBIAN/control" <<EOF
Package: $package_name
Version: $package_version
Section: net
Priority: optional
Architecture: $TARGET_ARCH
Maintainer: Simplus contributors
Depends: libc6, charon-systemd (>= $STRONGSWAN_SOURCE_VERSION), charon-systemd (<< $STRONGSWAN_ABI_UPPER_BOUND), libcharon-extra-plugins (>= $STRONGSWAN_SOURCE_VERSION), libcharon-extra-plugins (<< $STRONGSWAN_ABI_UPPER_BOUND), libstrongswan (>= $STRONGSWAN_SOURCE_VERSION), libstrongswan (<< $STRONGSWAN_ABI_UPPER_BOUND), strongswan-libcharon (>= $STRONGSWAN_SOURCE_VERSION), strongswan-libcharon (<< $STRONGSWAN_ABI_UPPER_BOUND)
Built-Using: strongswan (= $STRONGSWAN_SOURCE_VERSION)
Homepage: https://github.com/leonfox28/simplus
Description: strongSwan plugins required by Simplus Host VoWiFi
 Provides the Simplus external SIM AKA bridge and the upstream p-cscf
 configuration-attribute plugin. These plugins target the strongSwan
 $STRONGSWAN_UPSTREAM_VERSION private plugin ABI and are not a general VPN package.
EOF

cat >"$metadata_dir/build-abi" <<EOF
format=1
source_package=strongswan
source_version=$STRONGSWAN_SOURCE_VERSION
upstream_abi=$STRONGSWAN_UPSTREAM_VERSION
abi_upper_bound=$STRONGSWAN_ABI_UPPER_BOUND
target_distribution=$TARGET_DISTRIBUTION
target_release=$TARGET_RELEASE
target_architecture=$TARGET_ARCH
input_lock_sha256=$lock_sha256
package_version=$package_version
source_date_epoch=$SOURCE_DATE_EPOCH
EOF

find "$package_root" -exec touch -h -d "@$SOURCE_DATE_EPOCH" {} +
deb_filename="${package_name}_${package_version}_${TARGET_ARCH}.deb"
deb_output="$output_dir/$deb_filename"
dpkg-deb --root-owner-group -Zxz --build "$package_root" "$deb_output" >/dev/null

source_root="$work/corresponding-source"
install -d -m 0755 "$source_root/components" \
  "$source_root/packaging/strongswan-plugins/inputs" "$source_root/scripts/dev"
cp -a "$repo/components/strongswan-simplus-simaka" \
  "$source_root/components/strongswan-simplus-simaka"
for file in "$package_dir"/*; do
  [[ -f $file ]] || continue
  install -m 0644 "$file" "$source_root/packaging/strongswan-plugins/$(basename "$file")"
done
chmod 0755 "$source_root/packaging/strongswan-plugins/build-deb.sh"
for file in "$work/inputs"/*; do
  install -m 0644 "$file" \
    "$source_root/packaging/strongswan-plugins/inputs/$(basename "$file")"
done
install -m 0755 "$repo/scripts/dev/build-simplus-simaka-plugin.sh" \
  "$repo/scripts/dev/build-strongswan-p-cscf-plugin.sh" \
  "$repo/scripts/dev/test-simplus-simaka-c.sh" "$source_root/scripts/dev/"
cat >"$source_root/rebuild.sh" <<EOF
#!/usr/bin/env bash
set -euo pipefail
root=\$(CDPATH= cd "\$(dirname "\${BASH_SOURCE[0]}")" && pwd -P)
output=\${1:-\$root/out}
[[ \$output == /* ]] || output="\$PWD/\$output"
export SIMPLUS_DEB_VERSION='$package_version'
export SOURCE_DATE_EPOCH='$SOURCE_DATE_EPOCH'
exec "\$root/packaging/strongswan-plugins/build-deb.sh" "\$output"
EOF
chmod 0755 "$source_root/rebuild.sh"
cat >"$source_root/README.md" <<EOF
# Corresponding source for $package_name $package_version

This archive contains the exact Debian strongSwan source package, runtime ABI
inputs, Simplus GPL plugin source, lock file, and build scripts used for the
published binary package. On Debian 13/amd64, rebuild the same package version
and normalized timestamps with:

    ./rebuild.sh /absolute/output/directory

The build verifies every bundled input against the committed SHA-256 lock and
does not install anything on the host.
EOF
find "$source_root" -exec touch -h -d "@$SOURCE_DATE_EPOCH" {} +
source_filename="${package_name}_${package_version}.source.tar.xz"
source_output="$output_dir/$source_filename"
tar --sort=name --mtime="@$SOURCE_DATE_EPOCH" --owner=0 --group=0 \
  --numeric-owner -C "$source_root" -cJf "$source_output" .

deb_sha256=$(sha256sum "$deb_output" | awk '{print $1}')
source_sha256=$(sha256sum "$source_output" | awk '{print $1}')
manifest="$output_dir/strongswan-plugins.manifest"
cat >"$manifest" <<EOF
FORMAT=1
PACKAGE_NAME=$package_name
PACKAGE_VERSION=$package_version
TARGET_ARCH=$TARGET_ARCH
STRONGSWAN_SOURCE_VERSION=$STRONGSWAN_SOURCE_VERSION
STRONGSWAN_UPSTREAM_ABI=$STRONGSWAN_UPSTREAM_VERSION
STRONGSWAN_ABI_UPPER_BOUND=$STRONGSWAN_ABI_UPPER_BOUND
SOURCE_DATE_EPOCH=$SOURCE_DATE_EPOCH
DEB_FILENAME=$deb_filename
DEB_SHA256=$deb_sha256
SOURCE_FILENAME=$source_filename
SOURCE_SHA256=$source_sha256
EOF
touch -h -d "@$SOURCE_DATE_EPOCH" "$deb_output" "$source_output" "$manifest"
(cd "$output_dir" && sha256sum "$deb_filename" "$source_filename") \
  >"$output_dir/strongswan-plugins.sha256"
touch -h -d "@$SOURCE_DATE_EPOCH" "$output_dir/strongswan-plugins.sha256"

printf 'Built %s\n' "$deb_output"
printf 'Corresponding source %s\n' "$source_output"
