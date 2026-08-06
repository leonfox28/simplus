#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 3 ]]; then
  echo "usage: $0 <strongswan-source> <strongswan-build> <output-so>" >&2
  exit 2
fi

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
source_root=$(readlink -f "$1")
build_root=$(readlink -f "$2")
output=$(readlink -m "$3")
plugin_source="$root/components/strongswan-simplus-simaka"
library_dir=${SIMPLUS_STRONGSWAN_LIBDIR:-/usr/lib/ipsec}

[[ $library_dir == /* ]] || {
  echo "SIMPLUS_STRONGSWAN_LIBDIR must be an absolute path" >&2
  exit 2
}

if [[ ! -f "$source_root/src/libsimaka/simaka_card.h" || ! -f "$build_root/config.h" ]]; then
  echo "strongSwan source or configured build tree is incomplete" >&2
  exit 1
fi
if ! grep -Fq 'AC_INIT([strongSwan],[6.0.1])' "$source_root/configure.ac"; then
  echo "only the reviewed strongSwan 6.0.1 ABI is supported" >&2
  exit 1
fi
for library in libsimaka.so libcharon.so libstrongswan.so; do
  [[ -e "$library_dir/$library" ]] || {
    echo "missing strongSwan build input: $library_dir/$library" >&2
    exit 1
  }
done

object_dir=$(mktemp -d "${TMPDIR:-/tmp}/simplus-simaka-build.XXXXXX")
cleanup() { [[ -d $object_dir ]] && rm -rf -- "$object_dir"; }
trap cleanup EXIT

common=(
  -std=gnu11 -O2 -fPIC -fstack-protector-strong -D_FORTIFY_SOURCE=3
  -Wall -Wextra -Werror -Wformat -Wformat-security
  -include "$build_root/config.h"
  -I"$plugin_source"
  -I"$source_root/src/libstrongswan"
  -I"$source_root/src/libcharon"
  -I"$source_root/src/libsimaka"
)

cc "${common[@]}" -c "$plugin_source/simplus_simaka_agent.c" -o "$object_dir/agent.o"
cc "${common[@]}" -c "$plugin_source/simplus_simaka_card.c" -o "$object_dir/card.o"
cc "${common[@]}" -c "$plugin_source/simplus_simaka_apn.c" -o "$object_dir/apn.o"
cc "${common[@]}" -c "$plugin_source/simplus_simaka_plugin.c" -o "$object_dir/plugin.o"
mkdir -p "$(dirname "$output")"
cc -shared -Wl,-z,relro,-z,now,-z,defs,-z,noexecstack \
  -o "$output" "$object_dir/agent.o" "$object_dir/card.o" "$object_dir/apn.o" "$object_dir/plugin.o" \
  -L"$library_dir" -Wl,-rpath,/usr/lib/ipsec -lsimaka -lcharon -lstrongswan -pthread

if ! nm -D "$output" | grep -q ' T simplus_simaka_plugin_create$'; then
  echo "plugin constructor is missing" >&2
  exit 1
fi
sha256sum "$output"
