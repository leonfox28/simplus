#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR=$(CDPATH= cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
REPO_ROOT=$(CDPATH= cd "$SCRIPT_DIR/../.." && pwd -P)
cd "$REPO_ROOT"
mkdir -p .dev/bin
go build -o .dev/bin/simplusd-browser ./cmd/simplusd
go build -tags=integration -o .dev/bin/simulator-browser ./internal/testsupport/simulatorserver
corepack pnpm --dir web build
exec .dev/bin/simulator-browser "$REPO_ROOT/.dev/bin/simplusd-browser" "$REPO_ROOT/web/dist"
