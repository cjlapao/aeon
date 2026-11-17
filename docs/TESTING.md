# Go tests

Use `make` from `aeon/`. The Makefile supplies `CGO_ENABLED=1`, `GOPRIVATE` and
`sqlite_fts5`, including the tag environment used by subprocess builds.

```sh
make test-unit                       # short internal/tool suite
make test-go                         # full Go suite, including CLI and daemon checks
make test-go TEST_PACKAGES=./internal/loop TEST_ARGS='-run TestFanOutTerminalRemovesWorktree -count=1'
make test-race                       # race detector and coverage
make test-noretrieval                # full suite with retrieval compiled out
make test-profile                   # uncached JSON timings in test-results.json
make test                           # full Go, plus installed UI/Rust test dependencies
```

`test-unit` runs `-short ./internal/... ./tools/...`. It excludes the CLI package and
skips the expensive retrieval subprocess gates and autonomy demo. It still exercises real
SQLite files, local fake servers, and some subprocess fixtures. It is a faster development lane,
not a substitute for `test-go` or `test-race`. Skips name the full command to run.

## Prerequisites and editor runs

Install the Go version in `go.mod`, a native C compiler, SQLite development headers, and Git.
On Linux, the SQLite header package is commonly `libsqlite3-dev`; macOS needs Xcode command-line
tools; Windows needs a working CGO compiler such as MinGW-w64. Keep custom `CC` and `CGO_CFLAGS`
settings in the calling environment. Tests must not assume a checkout-local `.tmp/cgo-shim` exists.
Private framework dependencies must already be cached or accessible with the contributor's credentials.

The runner needs local TCP/Unix socket access, temporary files, and permission to execute child
processes. No live model provider is required. A sandbox that blocks listeners cannot run the full
suite; do not hide those failures with broad skips. Platform-specific tests retain their explicit skips.
Native runners are required to validate each supported OS; a Linux pass does not establish Windows or
macOS runtime behavior. See [BUILD.md](BUILD.md) for the native runner matrix.

`make check-test-prereqs` probes the linked SQLite driver in memory before tests touch state.
The store also checks required features before migrations. Missing FTS5 reports the build flag
instead of retrying a migration that cannot succeed.

Open `aeon.code-workspace` or `aeon/` in VS Code. Checked-in settings supply `sqlite_fts5` to
Go build/test CodeLens, gopls and package test debugging. Other editors and bare shell commands need:

```sh
CGO_ENABLED=1 GOPRIVATE='github.com/cjlapao/*' go test -tags sqlite_fts5 ./...
```

A Go module cannot force a dependency's build tags on raw `go test`. Do not change the SQLite driver,
remove the FTS schema, or skip store tests to make an incorrectly built test binary pass.

## Why full tests take longer

Full tests cover schema migrations, real daemon startup, Git repositories, subprocess build contracts,
SQLite locking, process groups, and provider replay over local HTTP. Race builds add instrumentation
and C compilation. A cold build and a warm uncached test run measure different costs.

Ordinary store/loop row fixtures migrate one empty database per package and clone its closed bytes.
Every test gets its own private file and connections; upgrade, migration and durability checks continue
using their own fresh databases. The fixture closes all connections before copying, so committed WAL
pages are checkpointed. Never clone an open SQLite main file without its WAL.

Tool alias matching avoids constructing every tool schema per policy comparison. The immutable
embedded policy corpus is parsed once, while each `Source()` call receives independent record bytes.
CLI startup checks report an exited process immediately rather than spending the readiness timeout.

Use `test-profile` to separate package time, individual test time and compilation overhead. Package
times overlap under Go's scheduler; do not sum them as wall time. Do not compare concurrent suite runs.
The dated maintenance record holds observed failures, commands, timings and platform gaps:
[2026-11-02 test portability](../docs/aeon/maintenance/2026-11-02-test-portability.md).
