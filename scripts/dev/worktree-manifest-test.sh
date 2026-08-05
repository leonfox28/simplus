#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(CDPATH= cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
MANIFEST="$SCRIPT_DIR/worktree-manifest.py"
TMP_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/simplus-worktree-manifest-test.XXXXXX")
cleanup() {
  rm -rf -- "$TMP_ROOT"
}
trap cleanup EXIT HUP INT TERM

cd "$TMP_ROOT"
git init -q
git config user.name Simplus
git config user.email test@example.invalid
printf 'ignored.tmp\n' >.gitignore
printf 'tracked\n' >tracked.txt
git add .gitignore tracked.txt
printf 'AAAA\n' >generated-untracked.txt
python3 "$MANIFEST" >.git/manifest-a
printf 'BBBB\n' >generated-untracked.txt
python3 "$MANIFEST" >.git/manifest-b
if cmp -s .git/manifest-a .git/manifest-b; then
  echo 'worktree manifest did not detect a same-size untracked content change' >&2
  exit 1
fi

printf 'AAAA\n' >generated-untracked.txt
python3 "$MANIFEST" >.git/manifest-c
printf 'ignored-one\n' >ignored.tmp
python3 "$MANIFEST" >.git/manifest-d
printf 'ignored-two\n' >ignored.tmp
python3 "$MANIFEST" >.git/manifest-e
if ! cmp -s .git/manifest-d .git/manifest-e; then
  echo 'worktree manifest included an ignored runtime file' >&2
  exit 1
fi
if ! cmp -s .git/manifest-c .git/manifest-d; then
  echo 'creating an ignored runtime file changed the manifest' >&2
  exit 1
fi

printf 'worktree manifest regression tests passed\n'
