# Building Aeon

**`cgo` is on, and that is the whole of this document's reason to exist** (B3).

`sqlite-vec` is a C extension. The store (S4) and retrieval (S22) both need it, pure-Go
`modernc.org/sqlite` exposes no extension loading at all, and the WASM route does not compile against the
maintained driver. So the decision is forced — and the cost is that **six release targets need six C
toolchains.**

---

## The decision: native runners per OS, not a universal cross-compiler

**Build each target on its own operating system.** Do not cross-compile between operating systems.

### Why, and it is not a preference

**macOS cannot be cross-compiled without its SDK**, and the SDK's licence does not permit redistributing
it. Any `zig cc` or `osxcross` approach that covers `darwin/*` requires somebody to obtain and host an
Apple SDK — which is a licensing position a project should not take to save a CI runner.

**And once `darwin/*` needs a native runner, a universal cross-compiler has stopped being universal.** It
would then cover linux and windows only, which is two runners' worth of work to avoid two runners.

`zig cc` is genuinely good at `linux/arm64` and `windows/*`, and it is the right tool if you ever need a
single-host build of those four. **It is recorded here as the considered alternative, with the reason it
was not chosen**, so this is not re-decided at S22 by somebody who does not know it was decided here
(D11).

### The runner matrix

| Target | Runner | Toolchain |
| --- | --- | --- |
| `linux/amd64` | `ubuntu-latest` | system `gcc` |
| `linux/arm64` | `ubuntu-24.04-arm` (native) | system `gcc` |
| `darwin/amd64` | `macos-13` | Xcode CLT |
| `darwin/arm64` | `macos-latest` | Xcode CLT |
| `windows/amd64` | `windows-latest` | mingw-w64 / `gcc` |
| `windows/arm64` | `windows-11-arm` | mingw-w64 |

**Every target builds natively**, so `CGO_ENABLED=1` needs no `CC`, no sysroot and no SDK.

---

## What a single developer machine can build

**One target: its own.** Measured on `linux/amd64`, 2026-10-14, with the cgo dependency genuinely linked
into `./cmd/aeon`:

| Target | Result | Cause |
| --- | --- | --- |
| `linux/amd64` | **ok**, extension present | host `gcc` |
| `linux/arm64` | failed | host `gcc` assembles x86-64: `` Error: no such instruction: `stp x29,x30,[sp,' `` → needs `aarch64-linux-gnu-gcc` |
| `darwin/amd64` | failed | `error: unsupported option '-arch'` → needs Xcode CLT |
| `darwin/arm64` | failed | same |
| `windows/amd64` | failed | `error: unrecognized command-line option '-mthreads'` → needs mingw-w64 |
| `windows/arm64` | failed | same |

**`make build-all` is expected to fail on a developer machine, and that is not a defect.** It is the
measurement, and it is why CI is the only place the six-target claim is made.

### A warning about this measurement, because it misled twice

**Taken carelessly, this number comes out far better than the truth.** Two ways:

**1. No cgo in the tree.** Before `internal/database` existed, `go build ./cmd/aeon` needed no C compiler
at all — Go then builds cross-OS targets happily and **5 of 6 appear to pass.**

**2. cgo in the tree but not in the binary.** `internal/database` imported only by its own tests does not
affect `./cmd/aeon`. Measured that way, **3 of 6 appear to pass** — and two of the "passes" were Windows
binaries containing no SQLite at all.

**So the measurement is only honest if the artefact is checked, not just the exit code:**

```sh
strings dist/aeon-windows-amd64.exe | grep -q sqlite3_auto_extension
```

**A binary that builds and cannot open a database is worse than one that fails to build**, because the
failure moves from CI to a user. `make build-all` performs this check per target.

---

## Commands

```sh
make build        # this platform, into dist/
make build-all    # all six; fails on a developer machine, by design
make ci           # fmt-check, vet, guard, test, build
```

**`GOPRIVATE=github.com/cjlapao/*` is required** — the checksum database cannot verify the framework
module. The Makefile exports it; see [`../UPSTREAM.md`](../UPSTREAM.md).

---

## Windows builds, but cannot serve — and the gap is upstream

**Found at S2, reading the transport.** `framework/localrpc`'s Windows named-pipe implementation is
`ErrPlatformNotImplemented`:

> *"The named-pipe implementation is deliberately deferred (brief O4): CI builds linux only, so a pipe
> implementation would ship unverified. The API does not branch — a caller writes the same code on every
> platform — and this is the one place that knows the difference."*

So a `windows/*` artefact from `make build-all` is a **real binary that cannot run the daemon**:

| | |
| --- | --- |
| `aeon version`, `aeon --vectors` | work |
| `aeon serve` | **cannot bind an endpoint** |
| `aeon ping`, `watch`, `debug` | nothing to connect to; `dial` says so by name rather than handing `net.Dial` a network it does not know |

**This is a different claim from "built but unverified" below.** An unverified binary might work. This one
is known not to, for a reason that is recorded upstream and deliberate. `go-winio v0.6.2` is already in the
module graph, so implementing it needs no new dependency — it needs a Windows CI runner, which is the same
blocker as the rest of this document.

Tracked as **QAP2-12** in `docs/test_suites/aeon-protocol.md`.

## Verification, and what is honestly unverified

**A built binary is not a working binary.** Each target is verified by running `aeon --vectors` on it,
which exercises the statically linked extension rather than merely the Go runtime:

```
sqlite-vec  v0.1.6  (statically linked)
```

| Target | Verified by |
| --- | --- |
| `linux/amd64` | **run natively.** `aeon version` and `aeon --vectors` both answer |
| `linux/arm64` | **built-but-unverified** until a native runner or `qemu-user-static` executes it |
| `darwin/*` | **built-but-unverified** from CI alone — a macOS runner builds it but running it is the same runner, which is adequate |
| `windows/*` | **built-but-unverified** without a Windows runner executing it |

**Anything "built-but-unverified" is listed here deliberately** (T4's step 4): a binary that compiles is a
claim, and the difference between compiling and running for a cgo binary is exactly where a missing shared
library hides.

**What would close each gap:** a native run in CI per target, or `qemu-user-static` for `linux/arm64`. The
six-target CI job should run `--vectors` on every artefact it produces, not just build it.

Go test lanes, FTS5 prerequisites, editor settings and timing guidance: [TESTING.md](TESTING.md).
