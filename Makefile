# Aeon — developer tasks
#
# `make` with no target prints help. That is deliberate: a bare `make` should
# teach, not build (B6).
#
# This Makefile is the ONLY entry point a contributor needs. A capability
# reachable only by a raw `go build -ldflags ... -tags ...` incantation is a
# capability most people will not use, and this repository has three toolchains.

MODULE   := github.com/cjlapao/aeon
BIN      := aeon
MAIN     := ./cmd/aeon
DIST     := dist

VERSION   ?= 0.0.0-dev
BUILDTIME := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
GITCOMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)

LDFLAGS := -s -w \
	-X main.Version=$(VERSION) \
	-X main.BuildTime=$(BUILDTIME) \
	-X main.GitCommit=$(GITCOMMIT)

# The framework is published from a repository the checksum database cannot
# verify, so every go command needs this. Exported here rather than written in
# a README step, because a build that depends on an env var somebody has to
# remember is a build that fails for every new contributor exactly once.
export GOPRIVATE := github.com/cjlapao/*

# CGO is ON from the first commit (B3), before anything needs it.
#
# sqlite-vec is a C extension: the store (S4) and retrieval (S22) both require
# it, pure-Go modernc.org/sqlite exposes no extension loading at all, and the
# WASM route does not compile against the maintained driver. Starting with CGO
# off and switching later converts a known build problem into a driver swap
# eighteen slices deep — which is the risk an earlier plan named and then walked
# into. Setting it now means the cross-compilation cost is discovered by T4,
# while nothing depends on the build.
export CGO_ENABLED := 1

# sqlite_fts5 is not optional from S22 on: FTS5 is the keyword half of the
# index (V3), compiled into the bundled driver behind this tag, and migration
# 0040 creates an FTS5 table — a binary or test built WITHOUT the tag fails
# at store open, loudly, naming fts5. The tag lives HERE rather than in every
# contributor's habit because B6 says the Makefile is the entry point; the
# retrieval package's own message says the same thing when someone bypasses
# make. (sqlite-vec needs no tag — DB18 registers it through
# sqlite3_auto_extension at init.)
GO_TAGS := sqlite_fts5
export AEON_GO_TAGS := $(GO_TAGS)

# Pass normal go test flags and package filters without copying build flags.
TEST_ARGS ?=
# go test's own default is 10 minutes PER PACKAGE, and internal/daemon and
# cmd/aeon each now run for about that long (measured 2026-11-04: 602s and
# 566s) — at the default the daemon package panics "test timed out" and every
# test after the cut never runs, which reads as a pass of nothing. Raise it
# here, in the one entry point (B6), until those packages are split or sped up.
TEST_TIMEOUT ?= 30m
TEST_PACKAGES ?= ./...

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help
	@echo "Aeon — $(VERSION)"
	@echo ""
	@grep -hE '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'
	@echo ""

# ── Running ───────────────────────────────────────────────────────────────────

.PHONY: dev
# Only the PATH is overridden. The endpoint is enabled by a code-level pin
# (bootstrap.Pins), not by an env var — so `aeon serve` works with no setup and
# this target only isolates dev from a real run's socket and store.
#
# It starts BOTH processes and stops BOTH (B7, D8). An orphaned daemon after
# Ctrl-C is worse than friction: the next `make dev` then refuses because the
# endpoint is held, and the person has no idea why.
dev: check-prereqs ## Run the daemon AND the app, stopping both on Ctrl-C
	@mkdir -p .dev
	@chmod 700 .dev
	@echo "starting daemon and app — Ctrl-C stops both"
	@set -m; \
	LOCALRPC_ENDPOINT_PATH=$(CURDIR)/.dev/aeon.sock go run -tags "aeondebug $(GO_TAGS)" $(MAIN) serve & \
	daemon=$$!; \
	( cd ui && LOCALRPC_ENDPOINT_PATH=$(CURDIR)/.dev/aeon.sock npm run tauri dev ) & \
	app=$$!; \
	trap 'kill -TERM $$daemon $$app 2>/dev/null; wait $$daemon 2>/dev/null; exit 0' INT TERM; \
	wait

.PHONY: dev-daemon
dev-daemon: ## Run only the daemon against the isolated dev endpoint
	@mkdir -p .dev
	@chmod 700 .dev
	LOCALRPC_ENDPOINT_PATH=$(CURDIR)/.dev/aeon.sock go run -tags "aeondebug $(GO_TAGS)" $(MAIN) serve

# Check BEFORE starting anything (O3). A `make dev` that half-starts — daemon
# up, app failed silently — is the worst outcome, because the app's absence
# reads as the app being broken rather than absent.
.PHONY: check-prereqs
check-prereqs: ## Verify Go, Node and Rust are present, naming any that are not
	@missing=""; \
	command -v go    >/dev/null 2>&1 || missing="$$missing go(https://go.dev/dl/)"; \
	command -v node  >/dev/null 2>&1 || missing="$$missing node(https://nodejs.org)"; \
	command -v cargo >/dev/null 2>&1 || missing="$$missing rust(https://rustup.rs)"; \
	if [ -n "$$missing" ]; then \
		echo "make dev needs toolchains that are not installed:"; \
		for m in $$missing; do echo "  - $$m"; done; \
		exit 1; \
	fi; \
	if ! pkg-config --exists webkit2gtk-4.1 2>/dev/null && [ "$$(uname)" = "Linux" ]; then \
		echo "note: Tauri's Linux deps are missing (webkit2gtk-4.1, gtk+-3.0)."; \
		echo "      the daemon runs; the app will not build. see ui/README.md"; \
	fi

.PHONY: run
run: ## Run the daemon with no overrides at all (works on a clean machine)
	go run -tags "$(GO_TAGS)" $(MAIN) serve

# ── Building ──────────────────────────────────────────────────────────────────

.PHONY: build
build: ## Build every package, then the binary for THIS platform into dist/
	go build -tags "$(GO_TAGS)" ./...
	@mkdir -p $(DIST)
	go build -tags "$(GO_TAGS)" -ldflags "$(LDFLAGS)" -o $(DIST)/$(BIN) $(MAIN)
	@echo "built $(DIST)/$(BIN)"

# Six targets, and CI must fail on any one of them (B9).
#
# EXPECTED TO FAIL ON A DEVELOPER MACHINE, and that is the measurement rather
# than a defect. See docs/BUILD.md for the runner matrix and the decision
# (native runners per OS, because macOS cannot be cross-compiled without its
# SDK and the SDK's licence does not permit redistributing it).
#
# MEASURED 2026-10-14 on linux/amd64, with cgo genuinely linked into the
# binary: 1 of 6.
#
#   linux/amd64    ok
#   linux/arm64    host gcc assembles x86-64  -> needs aarch64-linux-gnu-gcc
#   darwin/*       unsupported option '-arch' -> needs Xcode CLT
#   windows/*      unrecognized '-mthreads'   -> needs mingw-w64
#
# THE EXIT CODE IS NOT ENOUGH. Measured carelessly this reports 5 of 6 (no cgo
# in the tree) or 3 of 6 (cgo in the tree but not in ./cmd/aeon) — and two of
# those "passes" were Windows binaries containing no SQLite at all.
#
# A binary that builds and cannot open a database is worse than one that fails
# to build, because the failure moves from CI to a user. So each target is
# checked for the extension, not just for a zero exit.
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64

# ── The app, with the daemon inside it ────────────────────────────────────────
#
# The Tauri app ships `aeon` as a sidecar (tauri.conf.json's externalBin: "binaries/aeon"), and starts it itself when it
# opens. Tauri finds it by a name that ends in the Rust host triple, and bundles it into the package (deb, AppImage, dmg,
# nsis) beside the app. So the package contains the daemon only if THIS file exists when `tauri build` runs, built from
# the same source: `make app` does both in order, so a package can never carry a stale or missing daemon.
RUST_TRIPLE = $(shell rustc -vV 2>/dev/null | sed -n 's/^host: //p')
SIDECAR_EXT = $(if $(filter Windows_NT,$(OS)),.exe,)
SIDECAR     = ui/src-tauri/binaries/aeon-$(RUST_TRIPLE)$(SIDECAR_EXT)

.PHONY: sidecar
sidecar: ## Build the daemon into the app's sidecar slot, named for this platform's Rust triple
	@test -n "$(RUST_TRIPLE)" || { echo "sidecar: rustc is not installed, so the platform triple cannot be named (https://rustup.rs)"; exit 1; }
	@mkdir -p ui/src-tauri/binaries
	go build -tags "$(GO_TAGS)" -ldflags "$(LDFLAGS)" -o $(SIDECAR) $(MAIN)
	@echo "sidecar: $(SIDECAR)"

.PHONY: app
app: check-prereqs sidecar ## Build the packaged app (deb/AppImage on Linux) with the daemon bundled inside
	cd ui && npm install && npm run tauri build
	@echo "packages: ui/src-tauri/target/release/bundle/"

.PHONY: build-all
build-all: ## Build all six release targets, verifying each artefact (needs native runners)
	@mkdir -p $(DIST)
	@ok=0; failed=""; \
	for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; ext=""; \
		[ "$$os" = "windows" ] && ext=".exe"; \
		out=$(DIST)/$(BIN)-$$os-$$arch$$ext; \
		printf '  %-22s ' "$$os/$$arch"; \
		if GOOS=$$os GOARCH=$$arch go build -tags "$(GO_TAGS)" -ldflags "$(LDFLAGS)" -o $$out $(MAIN) 2>/dev/null; then \
			if strings $$out 2>/dev/null | grep -q 'sqlite3_auto_extension\|vec_version'; then \
				echo "ok"; ok=$$((ok+1)); \
			else \
				echo "BUILT BUT EMPTY - cgo was silently dropped"; \
				failed="$$failed $$os/$$arch(empty)"; \
			fi; \
		else \
			echo "FAILED - no C toolchain"; failed="$$failed $$os/$$arch"; \
		fi; \
	done; \
	echo ""; \
	echo "  $$ok of 6 usable"; \
	if [ -n "$$failed" ]; then \
		echo "  failing:$$failed"; \
		echo "  this is expected off CI - see docs/BUILD.md for the runner matrix"; \
		exit 1; \
	fi

.PHONY: vectors
vectors: ## Prove the statically linked sqlite-vec extension answers
	go run -tags "$(GO_TAGS)" $(MAIN) --vectors

.PHONY: build-noretrieval
build-noretrieval: ## Build the whole product WITHOUT internal/retrieval (D1 — the optionality claim)
	go build -tags "$(GO_TAGS) noretrieval" ./...

.PHONY: test-noretrieval
test-noretrieval: check-test-prereqs ## Run the Go suite WITHOUT internal/retrieval (D1, O1 — CI runs it, so it is a property)
	AEON_GO_TAGS="$(GO_TAGS) noretrieval" go test -tags "$(GO_TAGS) noretrieval" -timeout $(TEST_TIMEOUT) ./...

.PHONY: check-test-prereqs test-go test-unit test-profile
check-test-prereqs: ## Check the native compiler and required SQLite extensions
	@command -v $(firstword $(shell go env CC)) >/dev/null 2>&1 || \
		{ echo "Go tests need a native C compiler (CGO_ENABLED=1); install $$(go env CC)"; exit 1; }
	go run -tags "$(GO_TAGS)" ./tools/testprereqs

test-go: check-test-prereqs ## Run all Go tests, including CLI and integration checks
	go test -tags "$(GO_TAGS)" -timeout $(TEST_TIMEOUT) $(TEST_ARGS) $(TEST_PACKAGES)

test-unit: check-test-prereqs ## Run short internal/tool tests; omit CLI and selected build/demo checks
	go test -tags "$(GO_TAGS)" -timeout $(TEST_TIMEOUT) -short $(TEST_ARGS) ./internal/... ./tools/...

test-profile: check-test-prereqs ## Save uncached Go test timings to test-results.json
	go test -tags "$(GO_TAGS)" -timeout $(TEST_TIMEOUT) -json -count=1 $(TEST_ARGS) $(TEST_PACKAGES) > test-results.json

.PHONY: test
test: test-go ## Run the Go test suite and the UI's
	@if [ -d ui/node_modules ]; then $(MAKE) -s -C ui test; \
	else echo "  ui: skipped (run 'make ui-install')"; fi
	@# The Rust shell's tests, which exist from S2 and need Tauri's system
	@# libraries. Skipped with a NAMED reason rather than silently, because a
	@# suite that quietly does not run is worse than one that is absent.
	@if command -v cargo >/dev/null 2>&1 && pkg-config --exists webkit2gtk-4.1 2>/dev/null; then \
		$(MAKE) -s -C ui rust-test; \
	else \
		echo "  rust: skipped (cargo or Tauri's system libraries are missing — make -C ui check-deps)"; \
	fi

# The app's half is built by ui/Makefile; these delegate rather than repeat the
# npm invocations, so one place knows how the app is built. `make -C ui help`
# lists more than these three — test-watch, typecheck, tauri-dev, check-deps.
.PHONY: ui-install
ui-install: ## Install the UI's dependencies
	$(MAKE) -C ui install

.PHONY: ui-build
ui-build: ## Build the web layer
	$(MAKE) -C ui build

.PHONY: ui-test
ui-test: ## Run the UI tests
	$(MAKE) -C ui test

.PHONY: test-race
test-race: check-test-prereqs ## Run the test suite with the race detector and a coverage profile
	go test -tags "$(GO_TAGS)" -timeout $(TEST_TIMEOUT) -race -coverprofile=coverage.txt -covermode=atomic ./...

.PHONY: lint
lint: vet guard ## Run vet, the import guard, and golangci-lint if installed
	@command -v golangci-lint >/dev/null 2>&1 && golangci-lint run || \
		echo "  golangci-lint not installed — skipped (CI runs it)"

.PHONY: vet
vet: ## Run go vet and the UI typecheck
	go vet -tags "$(GO_TAGS)" ./...
	@if [ -d ui/node_modules ]; then $(MAKE) -s -C ui typecheck; \
	else echo "  ui typecheck: skipped (run 'make ui-install')"; fi

.PHONY: guard
guard: ## Reject any import of the scaffolding's internal/ packages (B1)
	@scripts/check-no-scaffolding-internal.sh

.PHONY: fmt
fmt: ## Format Go code; running it twice changes nothing
	gofmt -w cmd internal

.PHONY: fmt-check
fmt-check: ## Fail if anything is unformatted
	@out=$$(gofmt -l cmd internal); \
	if [ -n "$$out" ]; then echo "unformatted:"; echo "$$out"; exit 1; fi

# ── Dependencies ──────────────────────────────────────────────────────────────

.PHONY: deps
deps: ## Download and verify module dependencies
	go mod download
	go mod verify

.PHONY: tidy
tidy: ## Tidy go.mod / go.sum
	go mod tidy

# An upgrade must leave the registered service set unchanged (B11). Running the
# registry test immediately afterwards is what makes that a check rather than an
# intention — and it is the ONLY thing standing between a routine version bump
# and a daemon quietly wider than anyone decided.
.PHONY: framework-upgrade
framework-upgrade: ## Upgrade the framework, then prove the service set did not change
	go get github.com/cjlapao/go-backend-scaffolding/framework@$(or $(FRAMEWORK_VERSION),latest)
	go mod tidy
	go test -tags "$(GO_TAGS)" ./internal/bootstrap/ -run 'TestRegisteredSetEqualsGolden|TestRefusedServicesAreNotRegistered' -v
	@echo ""
	@echo "Service set unchanged. Record the version and the REASON in UPSTREAM.md (B2)."

# ── Housekeeping ──────────────────────────────────────────────────────────────

.PHONY: clean
clean: ## Remove build output, coverage and the dev scratch dir
	rm -rf $(DIST) .dev coverage.txt coverage.out cobertura-coverage.xml
	rm -rf ui/dist ui/src-tauri/target
	rm -f $(BIN) $(BIN).exe

.PHONY: protogen check-protogen
protogen: ## Regenerate the app's TypeScript protocol types from conformance fixtures
	go run ./tools/protogen

check-protogen: ## Fail if checked-in app protocol types are stale
	go run ./tools/protogen --check

# The command-policy corpus is extracted from THIS repository's agent
# definitions (opencode/agents, discovered by walking up — no path is passed
# here on purpose; --agents stays for operator overrides). A missing agents
# directory is a NAMED skip inside the tool, not a failure: the input lives
# outside aeon/ and after the P24 extraction it may not exist at all.
.PHONY: corpusgen check-corpusgen
corpusgen: ## Regenerate the command-policy corpus from this repository's agent definitions
	go run ./tools/corpusgen

check-corpusgen: ## Fail if the checked-in command-policy corpus is stale
	go run ./tools/corpusgen --check

.PHONY: ci
ci: fmt-check vet guard check-protogen check-corpusgen test build ## Everything CI runs, in CI's order
