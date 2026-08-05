#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(CDPATH= cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
REPO_ROOT=$(CDPATH= cd "$SCRIPT_DIR/../.." && pwd -P)
INSTALLER="$SCRIPT_DIR/install-toolchain.sh"

[[ -x $INSTALLER ]] || {
  printf 'error: local toolchain installer is missing or not executable: %s\n' "$INSTALLER" >&2
  exit 1
}
[[ -f $REPO_ROOT/.go-version && -f $REPO_ROOT/.node-version && -f $REPO_ROOT/package.json && -f $REPO_ROOT/scripts/dev/toolchain-checksums.txt ]] || {
  printf 'error: run from a complete Simplus source worktree\n' >&2
  exit 1
}

go_version=$(tr -d '[:space:]' <"$REPO_ROOT/.go-version")
node_version=$(tr -d '[:space:]' <"$REPO_ROOT/.node-version")
command -v python3 >/dev/null 2>&1 || {
  printf 'error: python3 is required to read the pinned pnpm version\n' >&2
  exit 1
}
pnpm_version=$(python3 - "$REPO_ROOT/package.json" <<'PY'
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

printf 'Installing pinned repository-local toolchain: Go %s, Node %s, pnpm %s\n' \
  "$go_version" "$node_version" "$pnpm_version"
cd "$REPO_ROOT"
exec "$INSTALLER" "$go_version" "$node_version" "$pnpm_version"
