#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo 'usage: inspect-published-container-image.sh <ghcr-image:vX.Y.Z> <vX.Y.Z> <40-hex-commit>' >&2
  exit 2
}

fail() {
  echo "published container image check: $*" >&2
  exit 2
}

[[ $# -eq 3 ]] || usage

image_ref=$1
release_tag=$2
commit=$3

[[ $release_tag =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail 'release tag must match vX.Y.Z'
[[ $commit =~ ^[0-9a-f]{40}$ ]] || fail 'commit must be exactly 40 lowercase hexadecimal characters'

image_repository=${image_ref%:*}
image_tag=${image_ref##*:}
case "$image_repository" in
  ghcr.io/leonfox28/simplus-control | ghcr.io/leonfox28/simplus-agent | ghcr.io/leonfox28/simplus-netd) ;;
  *) fail 'image repository is outside the three Simplus release targets' ;;
esac
[[ $image_ref == "$image_repository:$image_tag" && $image_tag == "$release_tag" ]] || \
  fail 'image reference must use the requested literal release tag'

command -v docker >/dev/null 2>&1 || fail 'docker is required'
command -v jq >/dev/null 2>&1 || fail 'jq is required'

manifest=$(docker buildx imagetools inspect "$image_ref" --format '{{json .Manifest}}')
root_digest=$(jq -er '
  .digest
  | select(type == "string")
  | select(test("^sha256:[0-9a-f]{64}$"))
' <<<"$manifest") || fail 'image index has no valid root digest'
platform_digest=$(jq -er '
  [.manifests[]?
    | select(
        ((.platform.os // "") != "unknown") or
        ((.platform.architecture // "") != "unknown")
      )] as $images
  | if (($images | length) == 1 and
        $images[0].platform.os == "linux" and
        $images[0].platform.architecture == "amd64")
    then $images[0].digest
    else error("expected exactly one linux/amd64 image manifest")
    end
  | select(type == "string")
  | select(test("^sha256:[0-9a-f]{64}$"))
' <<<"$manifest") || fail 'image index does not contain exactly one valid linux/amd64 image'

labels=$(docker buildx imagetools inspect \
  "${image_repository}@${platform_digest}" --format '{{json .Image.Config.Labels}}')
jq -e \
  --arg source 'https://github.com/leonfox28/simplus' \
  --arg version "$release_tag" \
  --arg revision "$commit" \
  --arg license 'LicenseRef-PolyForm-Noncommercial-1.0.0' \
  '."org.opencontainers.image.source" == $source and
   ."org.opencontainers.image.version" == $version and
   ."org.opencontainers.image.revision" == $revision and
   ."org.opencontainers.image.licenses" == $license' \
  <<<"$labels" >/dev/null || fail 'image OCI labels do not match the immutable release metadata'

printf '%s\n' "$root_digest"
