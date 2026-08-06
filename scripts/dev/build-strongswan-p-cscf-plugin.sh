#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 3 ]]; then
  echo "usage: $0 <strongswan-source> <strongswan-build> <output-so>" >&2
  exit 2
fi

source_root=$(readlink -f "$1")
build_root=$(readlink -f "$2")
output=$(readlink -m "$3")
plugin_build="$build_root/src/libcharon/plugins/p_cscf"
plugin_source="$source_root/src/libcharon/plugins/p_cscf"
library_dir=${SIMPLUS_STRONGSWAN_LIBDIR:-/usr/lib/ipsec}

[[ $library_dir == /* ]] || {
  echo "SIMPLUS_STRONGSWAN_LIBDIR must be an absolute path" >&2
  exit 2
}

if [[ ! -f "$source_root/configure.ac" || ! -f "$build_root/config.h" || ! -f "$plugin_build/Makefile" ]]; then
  echo "strongSwan source or configured p-cscf build tree is incomplete" >&2
  exit 1
fi
if ! grep -Fq 'AC_INIT([strongSwan],[6.0.1])' "$source_root/configure.ac"; then
  echo "only the reviewed strongSwan 6.0.1 ABI is supported" >&2
  exit 1
fi
for library in libcharon.so libstrongswan.so; do
  [[ -e "$library_dir/$library" ]] || {
    echo "missing strongSwan build input: $library_dir/$library" >&2
    exit 1
  }
done

object_dir=$(mktemp -d "${TMPDIR:-/tmp}/simplus-p-cscf-build.XXXXXX")
cleanup() { [[ -d $object_dir ]] && rm -rf -- "$object_dir"; }
trap cleanup EXIT
common=(
  -std=gnu11 -O2 -fPIC -fstack-protector-strong -D_FORTIFY_SOURCE=3
  -Wall -Wextra -Wno-unused-parameter -Wformat -Wformat-security
  -include "$build_root/config.h"
  -I"$plugin_source"
  -I"$source_root/src/libstrongswan"
  -I"$source_root/src/libcharon"
)
cc "${common[@]}" -c "$plugin_source/p_cscf_plugin.c" -o "$object_dir/plugin.o"
cc "${common[@]}" -c "$plugin_source/p_cscf_handler.c" -o "$object_dir/handler.o"
mkdir -p "$(dirname "$output")"
cc -shared -Wl,-z,relro,-z,now,-z,defs,-z,noexecstack \
  -o "$output" "$object_dir/plugin.o" "$object_dir/handler.o" \
  -L"$library_dir" -Wl,-rpath,/usr/lib/ipsec -lcharon -lstrongswan -pthread
if ! nm -D "$output" | grep -q ' T p_cscf_plugin_create$'; then
  echo "p-cscf plugin constructor is missing" >&2
  exit 1
fi
sha256sum "$output"
