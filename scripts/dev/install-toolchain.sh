#!/usr/bin/env bash

set -euo pipefail
umask 077

fail() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

[[ $# -eq 3 ]] || fail 'usage: install-toolchain.sh <go-version> <node-version> <pnpm-version>'
go_version=$1
node_version=$2
pnpm_version=$3

[[ $go_version =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail 'invalid Go version'
[[ $node_version =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail 'invalid Node version'
[[ $pnpm_version =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail 'invalid pnpm version'

for command_name in bash curl flock python3 sha256sum tar xz; do
  command -v "$command_name" >/dev/null 2>&1 || fail "missing required host command: $command_name"
done

repo_root=$(pwd -P)
checksum_manifest=$repo_root/scripts/dev/toolchain-checksums.txt
[[ -f $repo_root/.go-version && -f $repo_root/.node-version && -f $repo_root/package.json && -f $checksum_manifest ]] || \
  fail 'current directory is not a complete Simplus repository'
[[ $(tr -d '[:space:]' <"$repo_root/.go-version") == "$go_version" ]] || fail '.go-version does not match request'
[[ $(tr -d '[:space:]' <"$repo_root/.node-version") == "$node_version" ]] || fail '.node-version does not match request'
manifest_pnpm_version=$(python3 - "$repo_root/package.json" <<'PY'
import json
import sys
with open(sys.argv[1], encoding="utf-8") as stream:
    package_manager = json.load(stream)["packageManager"]
name, version_and_hash = package_manager.rsplit("@", 1)
if name != "pnpm":
    raise SystemExit(f"unsupported package manager: {package_manager}")
print(version_and_hash.split("+", 1)[0])
PY
)
package_manager_spec=$(python3 - "$repo_root/package.json" <<'PY'
import json
import sys
with open(sys.argv[1], encoding="utf-8") as stream:
    print(json.load(stream)["packageManager"])
PY
)
[[ $manifest_pnpm_version == "$pnpm_version" ]] || fail 'package.json pnpm version does not match request'
[[ $package_manager_spec =~ ^pnpm@[0-9]+\.[0-9]+\.[0-9]+\+sha512\.[0-9a-f]{128}$ ]] || fail 'packageManager must pin pnpm with a SHA-512 hash'

case $(uname -m) in
  x86_64|amd64)
    go_arch=amd64
    node_arch=x64
    ;;
  aarch64|arm64)
    go_arch=arm64
    node_arch=arm64
    ;;
  *) fail "unsupported Linux architecture: $(uname -m)" ;;
esac
[[ $(uname -s) == Linux ]] || fail 'repository-local toolchain installation is Linux-only'

tools=$repo_root/.tools
if [[ -L $tools ]]; then
  fail '.tools must not be a symlink'
fi
install -d -m 0700 "$tools"
for child in "$tools/releases" "$tools/corepack" "$tools/pnpm" "$tools/go-build-cache" "$tools/go-mod-cache"; do
  [[ ! -L $child ]] || fail "protected tool path must not be a symlink: $child"
done
install -d -m 0700 "$tools/releases" "$tools/corepack" "$tools/pnpm" "$tools/go-build-cache" "$tools/go-mod-cache"
for owned_path in "$tools" "$tools/releases" "$tools/corepack" "$tools/pnpm" "$tools/go-build-cache" "$tools/go-mod-cache"; do
  [[ $(stat -c '%u' "$owned_path") == "$(id -u)" ]] || fail "tool path is not owned by the current user: $owned_path"
done

exec 9>"$tools/.toolchain-install.lock"
flock -n 9 || fail 'another toolchain installation is already running'

tmp=$(mktemp -d "$tools/.install.XXXXXX")
cleanup() {
  rm -rf -- "$tmp"
  rm -f -- "$tools/.go-link.$$" "$tools/.node-link.$$"
}
trap cleanup EXIT HUP INT TERM

curl_fetch() {
  local output=$1
  local url=$2
  curl --fail --silent --show-error --location \
    --proto '=https' --tlsv1.2 --retry 3 --retry-delay 1 \
    --connect-timeout 15 --max-time 900 \
    --output "$output" "$url"
}

pinned_checksum() {
  local filename=$1
  local checksum
  local count
  checksum=$(awk -v filename="$filename" '$1 !~ /^#/ && $2 == filename { print $1 }' "$checksum_manifest")
  count=$(awk -v filename="$filename" '$1 !~ /^#/ && $2 == filename { count++ } END { print count + 0 }' "$checksum_manifest")
  [[ $count -eq 1 && $checksum =~ ^[0-9a-f]{64}$ ]] || fail "missing or duplicate pinned checksum: $filename"
  printf '%s\n' "$checksum"
}

install_go() {
  local archive="go${go_version}.linux-${go_arch}.tar.gz"
  local release="$tools/releases/go-${go_version}-linux-${go_arch}"
  local archive_path=$tmp/$archive
  local checksum

  [[ ! -L $release ]] || fail 'Go release directory must not be a symlink'
  if [[ ! -x $release/bin/go ]]; then
    checksum=$(pinned_checksum "$archive")
    curl_fetch "$archive_path" "https://go.dev/dl/$archive"
    printf '%s  %s\n' "$checksum" "$archive_path" | sha256sum --check --status || fail 'Go archive checksum mismatch'
    install -d -m 0700 "$tmp/go-unpack"
    tar --extract --gzip --file "$archive_path" --directory "$tmp/go-unpack" --no-same-owner
    [[ -x $tmp/go-unpack/go/bin/go ]] || fail 'Go archive layout is invalid'
    mv -- "$tmp/go-unpack/go" "$release"
  fi
  [[ $($release/bin/go version) == "go version go${go_version} linux/${go_arch}" ]] || fail 'installed Go version is invalid'
  ln -s "releases/$(basename "$release")" "$tools/.go-link.$$"
  mv -Tf -- "$tools/.go-link.$$" "$tools/go"
}

install_node() {
  local archive="node-v${node_version}-linux-${node_arch}.tar.xz"
  local extracted="node-v${node_version}-linux-${node_arch}"
  local release="$tools/releases/node-${node_version}-linux-${node_arch}"
  local archive_path=$tmp/$archive
  local checksum

  [[ ! -L $release ]] || fail 'Node release directory must not be a symlink'
  if [[ ! -x $release/bin/node ]]; then
    checksum=$(pinned_checksum "$archive")
    curl_fetch "$archive_path" "https://nodejs.org/dist/v${node_version}/$archive"
    printf '%s  %s\n' "$checksum" "$archive_path" | sha256sum --check --status || fail 'Node archive checksum mismatch'
    install -d -m 0700 "$tmp/node-unpack"
    tar --extract --xz --file "$archive_path" --directory "$tmp/node-unpack" --no-same-owner
    [[ -x $tmp/node-unpack/$extracted/bin/node ]] || fail 'Node archive layout is invalid'
    mv -- "$tmp/node-unpack/$extracted" "$release"
  fi
  [[ $($release/bin/node --version) == "v$node_version" ]] || fail 'installed Node version is invalid'
  [[ -x $release/bin/corepack ]] || fail 'pinned Node distribution does not contain Corepack'
  ln -s "releases/$(basename "$release")" "$tools/.node-link.$$"
  mv -Tf -- "$tools/.node-link.$$" "$tools/node"
}

install_go
install_node

PATH="$tools/node/bin:$PATH" COREPACK_HOME="$tools/corepack" PNPM_HOME="$tools/pnpm" \
  "$tools/node/bin/corepack" install --global "$package_manager_spec"
[[ $(PATH="$tools/node/bin:$PATH" COREPACK_HOME="$tools/corepack" PNPM_HOME="$tools/pnpm" \
  "$tools/node/bin/corepack" pnpm --version) == "$pnpm_version" ]] || \
  fail 'installed pnpm version is invalid'

printf 'Installed repository-local toolchain:\n'
"$tools/go/bin/go" version
"$tools/node/bin/node" --version
PATH="$tools/node/bin:$PATH" COREPACK_HOME="$tools/corepack" PNPM_HOME="$tools/pnpm" \
  "$tools/node/bin/corepack" pnpm --version
