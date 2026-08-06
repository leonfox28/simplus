SHELL := /bin/bash

GO ?= $(if $(wildcard $(CURDIR)/.tools/go/bin/go),$(CURDIR)/.tools/go/bin/go,go)
GOFMT ?= $(if $(wildcard $(CURDIR)/.tools/go/bin/gofmt),$(CURDIR)/.tools/go/bin/gofmt,gofmt)
PNPM ?= corepack pnpm
GOTOOLCHAIN ?= local
GOFLAGS ?= -mod=readonly
BIN_DIR ?= .dev/bin
DATA_ROOT ?= $(CURDIR)/.dev/data
LISTEN_ADDR ?= 127.0.0.1:8080
WEB_HOST ?= 127.0.0.1
LAN_DATA_ROOT ?= $(HOME)/.simplus-dev/data
HARDWARE_DATA_ROOT ?= $(HOME)/.simplus-hardware-dev/data
AGENT_SOCKET ?= /run/simplus-agent-dev/simplus-agent.sock
STRONGSWAN_PLUGIN_PACKAGE_DIR ?= $(CURDIR)/.dev/packages/strongswan-plugins
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)
COMMANDS := simplusd simplus-agent simplus-netd simplusctl
VOWIFI_HIL_COMMANDS := simplus-vowifi-hil-prepare simplus-vowifi-hil-vici simplus-vowifi-hil-pcscf simplus-vowifi-hil-ims simplus-vowifi-hil-redact simplus-vowifi-hil-stun
GENERATED_PATHS := internal/api/openapi/generated.go internal/storage/sqlite/generated/core/db.go internal/storage/sqlite/generated/core/models.go internal/storage/sqlite/generated/core/state.sql.go web/src/api/schema.d.ts
SQLC_VERSION := v1.31.1
GOVULNCHECK_VERSION := v1.6.0
ACTIONLINT_VERSION := v1.7.12
TOOL_NODE_BIN := $(CURDIR)/.tools/node/bin
TOOL_GO_BIN := $(CURDIR)/.tools/go/bin
PNPM_HOME := $(CURDIR)/.tools/pnpm

ifneq ($(wildcard $(TOOL_NODE_BIN)/node),)
export PATH := $(TOOL_NODE_BIN):$(PATH)
export COREPACK_HOME := $(CURDIR)/.tools/corepack
endif

ifneq ($(wildcard $(TOOL_GO_BIN)/go),)
export GOCACHE := $(CURDIR)/.tools/go-build-cache
export GOMODCACHE := $(CURDIR)/.tools/go-mod-cache
endif

export GOTOOLCHAIN GOFLAGS VERSION COMMIT PNPM_HOME

.PHONY: doctor bootstrap-dev generate verify-generated verify-modules check-docs format check-format lint test test-worktree-manifest test-dev-sim security build build-go build-linux build-vowifi-hil build-strongswan-plugins-deb test-strongswan-plugins-package dev-sim dev-sim-lan dev-hardware dev-hardware-lan dev-hardware-probe dev-agent-deploy dev-toolchain clean

doctor:
	@set -eu; \
	for cmd in git node corepack curl install python3; do \
		command -v "$$cmd" >/dev/null || { echo "missing required command: $$cmd" >&2; exit 1; }; \
	done; \
	if [ ! -x "$(GO)" ]; then command -v "$(GO)" >/dev/null || { echo "missing required command: $(GO)" >&2; exit 1; }; fi; \
	if [ ! -x "$(GOFMT)" ]; then command -v "$(GOFMT)" >/dev/null || { echo "missing required command: $(GOFMT)" >&2; exit 1; }; fi; \
	expected_go="go$$(cat .go-version)"; actual_go="$$($(GO) env GOVERSION)"; \
	expected_node="v$$(cat .node-version)"; actual_node="$$(node --version)"; \
	expected_pnpm="$$(node -p 'require("./package.json").packageManager.split("@").at(-1).split("+")[0]')"; actual_pnpm="$$(corepack pnpm --version)"; \
	[ "$$actual_go" = "$$expected_go" ] || { echo "Go version mismatch: expected $$expected_go, got $$actual_go" >&2; exit 1; }; \
	[ "$$actual_node" = "$$expected_node" ] || { echo "Node version mismatch: expected $$expected_node, got $$actual_node" >&2; exit 1; }; \
	[ "$$actual_pnpm" = "$$expected_pnpm" ] || { echo "pnpm version mismatch: expected $$expected_pnpm, got $$actual_pnpm" >&2; exit 1; }; \
	$(GO) version; node --version; corepack pnpm --version; git --version

bootstrap-dev:
	@install -d -m 0700 .dev .dev/bin .dev/data
	$(PNPM) install --frozen-lockfile
	$(GO) mod download

generate:
	$(GO) generate ./internal/api/openapi
	$(GO) run github.com/sqlc-dev/sqlc/cmd/sqlc@$(SQLC_VERSION) generate
	$(PNPM) api:generate

verify-generated:
	@set -eu; \
	tmp="$$(mktemp -d "$${TMPDIR:-/tmp}/simplus-generated.XXXXXX")"; \
	trap 'rm -rf "$$tmp"' EXIT HUP INT TERM; \
	python3 scripts/dev/worktree-manifest.py >"$$tmp/manifest-before"; \
	for path in $(GENERATED_PATHS); do \
		git ls-files --error-unmatch -- "$$path" >/dev/null 2>&1 || { echo "generated path is not tracked: $$path" >&2; exit 1; }; \
		test -f "$$path" || { echo "generated path is missing: $$path" >&2; exit 1; }; \
		mkdir -p "$$tmp/$$(dirname "$$path")"; \
		cp "$$path" "$$tmp/$$path"; \
	done; \
	$(MAKE) --no-print-directory generate; \
	for path in $(GENERATED_PATHS); do \
		cmp -s "$$tmp/$$path" "$$path" || { echo "generated file is stale: $$path" >&2; exit 1; }; \
	done; \
	python3 scripts/dev/worktree-manifest.py >"$$tmp/manifest-after"; \
	if ! cmp -s "$$tmp/manifest-before" "$$tmp/manifest-after"; then \
		echo 'generation changed the complete worktree content manifest:' >&2; \
		diff -u "$$tmp/manifest-before" "$$tmp/manifest-after" >&2 || true; \
		exit 1; \
	fi

verify-modules:
	$(GO) mod tidy -diff
	$(GO) mod verify

check-docs:
	@python3 scripts/dev/check-docs.py

format:
	$(GO) fmt ./...

check-format:
	@files="$$($(GOFMT) -l cmd internal)"; \
	if [ -n "$$files" ]; then printf '%s\n' "$$files" >&2; echo "Go files need formatting" >&2; exit 1; fi

lint:
	$(GO) vet ./...
	$(GO) run github.com/rhysd/actionlint/cmd/actionlint@$(ACTIONLINT_VERSION) .github/workflows/*.yml

test:
	$(GO) test ./...
	$(PNPM) web:test
	$(PNPM) --dir web typecheck
	$(MAKE) --no-print-directory test-worktree-manifest
	$(MAKE) --no-print-directory test-dev-sim

test-worktree-manifest:
	@scripts/dev/worktree-manifest-test.sh

test-dev-sim:
	@install -d -m 0700 "$(BIN_DIR)"
	@$(GO) build -o "$(BIN_DIR)/simplusd-supervisor-test" ./cmd/simplusd
	@SIMPLUS_DEV_API_BIN="$(BIN_DIR)/simplusd-supervisor-test" scripts/dev/run-sim-test.sh

security:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...
	$(PNPM) audit --audit-level moderate

build: build-go
	$(PNPM) web:build

build-go:
	@install -d -m 0700 "$(BIN_DIR)"
	@set -eu; \
	[[ "$${VERSION}" =~ ^[A-Za-z0-9._+-]+$$ ]] || { echo "invalid VERSION" >&2; exit 2; }; \
	[[ "$${COMMIT}" == unknown || "$${COMMIT}" =~ ^[A-Fa-f0-9]+$$ ]] || { echo "invalid COMMIT" >&2; exit 2; }; \
	ldflags="-s -w -X github.com/leonfox28/simplus/internal/buildinfo.Version=$${VERSION} -X github.com/leonfox28/simplus/internal/buildinfo.Commit=$${COMMIT}"; \
	for cmd in $(COMMANDS); do \
		CGO_ENABLED=0 $(GO) build -buildvcs=false -trimpath -ldflags "$$ldflags" -o "$(BIN_DIR)/$$cmd" "./cmd/$$cmd"; \
	done

build-linux:
	@install -d -m 0700 "$(BIN_DIR)"
	@set -eu; \
	[[ "$${VERSION}" =~ ^[A-Za-z0-9._+-]+$$ ]] || { echo "invalid VERSION" >&2; exit 2; }; \
	[[ "$${COMMIT}" == unknown || "$${COMMIT}" =~ ^[A-Fa-f0-9]+$$ ]] || { echo "invalid COMMIT" >&2; exit 2; }; \
	ldflags="-s -w -X github.com/leonfox28/simplus/internal/buildinfo.Version=$${VERSION} -X github.com/leonfox28/simplus/internal/buildinfo.Commit=$${COMMIT}"; \
	for arch in amd64 arm64; do \
		for cmd in $(COMMANDS); do \
			GOOS=linux GOARCH="$$arch" CGO_ENABLED=0 $(GO) build -buildvcs=false -trimpath -ldflags "$$ldflags" \
				-o "$(BIN_DIR)/$$cmd-linux-$$arch" "./cmd/$$cmd"; \
		done; \
	done

build-vowifi-hil:
	@install -d -m 0700 "$(BIN_DIR)"
	@set -eu; \
	for cmd in $(VOWIFI_HIL_COMMANDS); do \
		$(GO) build -buildvcs=false -trimpath -o "$(BIN_DIR)/$$cmd" "./cmd/$$cmd"; \
	done

build-strongswan-plugins-deb:
	@packaging/strongswan-plugins/build-deb.sh "$(STRONGSWAN_PLUGIN_PACKAGE_DIR)"

test-strongswan-plugins-package: build-strongswan-plugins-deb
	@bash scripts/dev/test-simplus-simaka-c.sh
	@scripts/dev/test-strongswan-plugins-package.sh "$(STRONGSWAN_PLUGIN_PACKAGE_DIR)"

dev-toolchain:
	scripts/dev/setup-toolchain.sh

dev-sim:
	@install -d -m 0700 "$(DATA_ROOT)" "$(BIN_DIR)"
	@$(GO) build -o "$(BIN_DIR)/simplusd-dev" ./cmd/simplusd
	@$(GO) build -o "$(BIN_DIR)/simplusctl-dev" ./cmd/simplusctl
	@SIMPLUS_DEV_API_BIN="$(BIN_DIR)/simplusd-dev" \
		SIMPLUS_DATA_ROOT="$(DATA_ROOT)" \
		SIMPLUS_LISTEN_ADDR="$(LISTEN_ADDR)" \
		SIMPLUS_DEV_WEB_HOST="$(WEB_HOST)" \
		scripts/dev/run-sim.sh

dev-sim-lan:
	@install -d -m 0700 "$(HOME)/.simplus-dev" "$(LAN_DATA_ROOT)"
	@$(MAKE) --no-print-directory dev-sim WEB_HOST=0.0.0.0 DATA_ROOT="$(LAN_DATA_ROOT)"

dev-agent-deploy:
	@scripts/dev/install-agent.sh

dev-hardware:
	@install -d -m 0700 "$(HOME)/.simplus-hardware-dev" "$(HARDWARE_DATA_ROOT)" "$(BIN_DIR)"
	@$(GO) build -o "$(BIN_DIR)/simplusd-hardware-dev" ./cmd/simplusd
	@$(GO) build -o "$(BIN_DIR)/simplusctl-hardware-dev" ./cmd/simplusctl
	@SIMPLUS_DEV_API_BIN="$(BIN_DIR)/simplusd-hardware-dev" \
		SIMPLUS_DEV_BACKEND=hardware \
		SIMPLUS_AGENT_SOCKET="$(AGENT_SOCKET)" \
		SIMPLUS_DATA_ROOT="$(HARDWARE_DATA_ROOT)" \
		SIMPLUS_LISTEN_ADDR="$(LISTEN_ADDR)" \
		SIMPLUS_DEV_WEB_HOST="$(WEB_HOST)" \
		scripts/dev/run-sim.sh

dev-hardware-lan:
	@$(MAKE) --no-print-directory dev-hardware WEB_HOST=0.0.0.0

dev-hardware-probe:
	@install -d -m 0700 "$(BIN_DIR)"
	@$(GO) build -o "$(BIN_DIR)/simplusctl-hardware-dev" ./cmd/simplusctl
	@sudo "$(CURDIR)/$(BIN_DIR)/simplusctl-hardware-dev" hardware probe --socket "$(AGENT_SOCKET)" --json

clean:
	rm -rf .dev/bin web/dist
