#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo 'usage: build-container-release-bundle.sh <vX.Y.Z> <40-hex-commit> <source-date-epoch> <existing-output-directory>' >&2
  exit 2
}

fail() {
  echo "container release bundle: $*" >&2
  exit 2
}

[[ $# -eq 4 ]] || usage

release_tag=$1
commit=$2
source_date_epoch=$3
output_argument=$4

[[ $release_tag =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail 'release tag must match vX.Y.Z'
[[ $commit =~ ^[0-9a-f]{40}$ ]] || fail 'commit must be exactly 40 lowercase hexadecimal characters'
[[ $source_date_epoch =~ ^[0-9]+$ ]] || fail 'source date epoch must be a non-negative integer'
[[ -d $output_argument && ! -L $output_argument ]] || fail 'output directory must already exist and must not be a symbolic link'

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
repository_root=$(cd -- "$script_dir/../.." && pwd -P)
output_dir=$(cd -- "$output_argument" && pwd -P)
bundle_name="simplus-compose-${release_tag}-linux-amd64"
archive_name="${bundle_name}.tar.gz"
checksum_name="${archive_name}.sha256"
image_placeholder='${SIMPLUS_IMAGE_TAG:?set SIMPLUS_IMAGE_TAG for source-tree development validation}'

umask 077
temporary_root=$(mktemp -d "${TMPDIR:-/tmp}/simplus-container-release.XXXXXX")
archive_temporary="$output_dir/.${archive_name}.$$"
checksum_temporary="$output_dir/.${checksum_name}.$$"
cleanup() {
  rm -rf -- "$temporary_root"
  rm -f -- "$archive_temporary" "$checksum_temporary"
}
trap cleanup EXIT HUP INT TERM

bundle_root="$temporary_root/$bundle_name"
install -d -m 0755 "$bundle_root"

placeholder_count=$(grep -F -c "$image_placeholder" "$repository_root/compose.yaml" || true)
[[ $placeholder_count -eq 5 ]] || fail "source Compose must contain exactly five controlled image tag placeholders (found $placeholder_count)"
sed 's/${SIMPLUS_IMAGE_TAG:?set SIMPLUS_IMAGE_TAG for source-tree development validation}/'"$release_tag"'/g' \
  "$repository_root/compose.yaml" >"$bundle_root/compose.yaml"

expected_images=$(printf '%s\n' \
  "ghcr.io/leonfox28/simplus-netd:$release_tag" \
  "ghcr.io/leonfox28/simplus-agent:$release_tag" \
  "ghcr.io/leonfox28/simplus-netd:$release_tag" \
  "ghcr.io/leonfox28/simplus-control:$release_tag" \
  "ghcr.io/leonfox28/simplus-control:$release_tag")
actual_images=$(sed -n 's/^    image: //p' "$bundle_root/compose.yaml")
[[ $actual_images == "$expected_images" ]] || fail 'rendered Compose does not reference the expected five versioned GHCR images'
! grep -Fq "$image_placeholder" "$bundle_root/compose.yaml" || fail 'rendered Compose still contains the development image placeholder'
! grep -Eq '(^|[[:space:]])build:|:latest([[:space:]]|$)' "$bundle_root/compose.yaml" || fail 'rendered Compose contains a build directive or latest tag'

install -m 0644 "$repository_root/packaging/container/.env.example" "$bundle_root/.env.example"
install -m 0644 "$repository_root/packaging/container/README.md" "$bundle_root/README.md"
install -m 0755 "$repository_root/scripts/release/prepare-container-host.sh" "$bundle_root/prepare-container-host.sh"
install -m 0755 "$repository_root/scripts/release/check-container-host.sh" "$bundle_root/check-container-host.sh"
install -m 0644 "$repository_root/LICENSE" "$bundle_root/LICENSE"
install -m 0644 "$repository_root/THIRD_PARTY_NOTICES.md" "$bundle_root/THIRD_PARTY_NOTICES.md"
printf 'version=%s\ncommit=%s\nplatform=linux/amd64\nsource_date_epoch=%s\n' \
  "$release_tag" "$commit" "$source_date_epoch" >"$bundle_root/VERSION"
chmod 0644 "$bundle_root/compose.yaml" "$bundle_root/VERSION"

actual_files=$(find "$bundle_root" -mindepth 1 -maxdepth 1 -printf '%f\n' | LC_ALL=C sort)
expected_files=$(printf '%s\n' \
  .env.example LICENSE README.md THIRD_PARTY_NOTICES.md VERSION \
  check-container-host.sh compose.yaml prepare-container-host.sh | LC_ALL=C sort)
[[ $actual_files == "$expected_files" ]] || fail 'bundle staging directory does not match the public file allowlist'

tar --sort=name --format=ustar --mtime="@$source_date_epoch" \
  --owner=0 --group=0 --numeric-owner -C "$temporary_root" -cf - "$bundle_name" |
  gzip -n >"$archive_temporary"
archive_sha256=$(sha256sum "$archive_temporary")
archive_sha256=${archive_sha256%% *}
[[ $archive_sha256 =~ ^[0-9a-f]{64}$ ]] || fail 'sha256sum returned an invalid archive digest'
printf '%s  %s\n' "$archive_sha256" "$archive_name" >"$checksum_temporary"
chmod 0644 "$archive_temporary" "$checksum_temporary"
mv -f -- "$archive_temporary" "$output_dir/$archive_name"
mv -f -- "$checksum_temporary" "$output_dir/$checksum_name"
