# Aeon — the app.
#
# The web layer and the Tauri shell. `../Makefile` delegates its `ui-*` targets
# here, so there is exactly one place that knows how this half is built.
#
#   make help
#
# Why a Makefile over the npm scripts it wraps: dependency tracking. `make
# build` reinstalls when package-lock.json has moved and does nothing when it
# has not — `npm run build` cannot know, so it either always reinstalls or
# silently builds against a stale tree.

SHELL      := /bin/bash
.SHELLFLAGS:= -eu -o pipefail -c
.DEFAULT_GOAL := help

# The dev endpoint, matching ../Makefile's `dev` target exactly. An app looking
# at a different socket from the daemon it is meant to be talking to is the
# single most confusing failure available here, so both halves derive it from
# one default and both accept the same override.
AEON_ENDPOINT ?= $(abspath $(CURDIR)/../.dev/aeon.sock)

STAMP := node_modules/.aeon-install-stamp

# The sidecar daemon binary `tauri.conf.json`'s `externalBin` expects, at
# binaries/aeon-<this machine's target triple>[.exe] (S41). `cargo build`
# refuses to run at all without a file matching the host triple present
# here — which is deliberate: a stale or missing sidecar becomes a build
# failure a person reads, not a runtime surprise discovered only once the
# app tries to auto-start a daemon that isn't there.
HOST_TRIPLE := $(shell rustc --print host-tuple)
SIDECAR_EXT := $(if $(filter %-pc-windows-msvc %-pc-windows-gnu,$(HOST_TRIPLE)),.exe,)
SIDECAR     := src-tauri/binaries/aeon-$(HOST_TRIPLE)$(SIDECAR_EXT)
# The CGO shim `../AGENTS.md` §6 documents — sqlite-vec's header, copied once
# under its real name so this build doesn't need a system sqlite3.h. Shared
# with the root daemon build; left in place once fetched.
CGO_SHIM    := $(abspath $(CURDIR)/../.tmp/cgo-shim)

.PHONY: help install dev build test test-watch typecheck rust-test rust-check \
        sidecar tauri-dev tauri-build install-dev-icon check-deps clean distclean

help: ## Show this help
	@echo "Aeon app — targets:"
	@grep -hE '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
	  | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'
	@echo ""
	@echo "endpoint: $(AEON_ENDPOINT)"
	@echo "override with: make tauri-dev AEON_ENDPOINT=/path/to/rpc.sock"

# The stamp is the install. It depends on both manifests, so editing either one
# makes every target that needs dependencies reinstall — and nothing else does.
$(STAMP): package.json package-lock.json
	npm ci
	@mkdir -p $(dir $@) && touch $@

install: $(STAMP) ## Install dependencies (only if the lockfile moved)

dev: $(STAMP) ## Run vite alone, in a browser, on :5173
	@echo "note: a browser has no Tauri bridge, so the daemon pill will read"
	@echo "      'not connected'. That is correct here — use 'make tauri-dev'"
	@echo "      (or '../Makefile' dev) to exercise the socket."
	npm run dev

build: $(STAMP) ## Type-check and build the web bundle into dist/
	npm run build

test: $(STAMP) ## Run the tests once
	npm test

test-watch: $(STAMP) ## Run the tests and re-run on change
	npx vitest

typecheck: $(STAMP) ## Type-check without emitting
	npm run lint

rust-test: sidecar ## Run the Rust shell's tests (needs Tauri's system libraries)
	cd src-tauri && cargo test

rust-check: sidecar ## Compile the Rust shell without producing a bundle
	cd src-tauri && cargo build

sidecar: ## Build the bundled daemon binary for this machine's target triple (S41)
	@mkdir -p $(CGO_SHIM) src-tauri/binaries
	@SQLITE3_VER=$$(awk '/mattn\/go-sqlite3/ {print $$2}' ../go.mod); \
	 [ -f $(CGO_SHIM)/sqlite3.h ] || cp "$$(go env GOMODCACHE)/github.com/mattn/go-sqlite3@$$SQLITE3_VER/sqlite3-binding.h" $(CGO_SHIM)/sqlite3.h
	# sqlite_fts5 is mandatory from S22: migration 0040 creates an FTS5 table,
	# and a binary built WITHOUT the tag dies at store open with "no such
	# module: fts5". The root Makefile's dev/dev-daemon pass it via $(GO_TAGS);
	# this target must too, or `dev-tauri` builds an FTS-less daemon.
	cd .. && GOPRIVATE='github.com/cjlapao/*' CGO_ENABLED=1 CGO_CFLAGS="-I$(CGO_SHIM)" \
	  go build -tags sqlite_fts5 -o ui/$(SIDECAR) ./cmd/aeon
	@echo "sidecar: $(SIDECAR)"

check-deps: ## Report whether Tauri's system libraries are present
	@if [ "$$(uname)" != "Linux" ]; then \
	  echo "not Linux — nothing to check here"; exit 0; \
	fi; \
	missing=""; \
	for pkg in webkit2gtk-4.1 gtk+-3.0 librsvg-2.0; do \
	  pkg-config --exists $$pkg 2>/dev/null || missing="$$missing $$pkg"; \
	done; \
	if [ -n "$$missing" ]; then \
	  echo "missing Tauri system libraries:$$missing"; \
	  echo ""; \
	  echo "  Debian/Ubuntu: sudo apt install libwebkit2gtk-4.1-dev libgtk-3-dev \\"; \
	  echo "                   libayatana-appindicator3-dev librsvg2-dev patchelf"; \
	  echo "  Fedora:        sudo dnf install webkit2gtk4.1-devel gtk3-devel \\"; \
	  echo "                   libappindicator-gtk3-devel librsvg2-devel patchelf"; \
	  echo ""; \
	  echo "the web layer (dev, build, test) works without these;"; \
	  echo "tauri-dev and tauri-build do not."; \
	  exit 1; \
	fi; \
	echo "Tauri system libraries present"

install-dev-icon: ## Register the development icon with the Linux desktop
	@if [ "$$(uname)" = "Linux" ]; then ./scripts/install-dev-icon.sh; fi

tauri-dev: $(STAMP) check-deps install-dev-icon sidecar ## Run the desktop window against the dev endpoint
	LOCALRPC_ENDPOINT_PATH=$(AEON_ENDPOINT) npm run tauri dev

tauri-build: $(STAMP) check-deps sidecar ## Bundle the desktop app for this platform
	npm run tauri build

clean: ## Remove build output, keeping dependencies
	rm -rf dist tsconfig.tsbuildinfo
	rm -rf src-tauri/target/debug/aeon-ui src-tauri/target/release/aeon-ui

distclean: clean ## Also remove node_modules, the Rust target directory, and the sidecar binary
	rm -rf node_modules src-tauri/target src-tauri/binaries
