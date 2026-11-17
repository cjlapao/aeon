# S1 Bootstrap — the demo

**Stage 0 · Garage.** *It builds, and it starts.*

> **Walked 2026-10-14 on `linux/amd64`.** Six discrepancies were found and are recorded in
> §Findings — the demo was written before the Makefile split `build` from `build-all`, before
> `--print-services` existed as a top-level verb, and before the endpoint was called an endpoint.
> **One was a real bug in the binary, and it was fixed rather than documented.**
>
> **To verify this yourself, use [`TESTSPEC.md`](TESTSPEC.md)** — this file is the narrative; that one is
> the checklist, with every configuration variable, every payload, and the exact expected output.

---

## 1. It builds

```
make build
```

**You should see one binary** — this platform's — under `dist/`:

```
built dist/aeon
```

```
./dist/aeon version
```

**You should see** the version, the build time, the commit, **and the Go toolchain**:

```
aeon 0.0.0-dev
  built   2026-10-14T19:03:36Z
  commit  2ce3a2a
  go      go1.26.6 linux/amd64
```

**The Go version is there because `cgo` is on** (B3): the toolchain is part of what the binary *is*, and a
bug report that omits it costs a round trip on exactly the class of problem cgo introduces.

### The six targets are a separate target, and it fails here

```
make build-all
```

**You should see one of six build, and the command fail:**

```
  linux/amd64            ok
  linux/arm64            FAILED - no C toolchain
  darwin/amd64           FAILED - no C toolchain
  darwin/arm64           FAILED - no C toolchain
  windows/amd64          FAILED - no C toolchain
  windows/arm64          FAILED - no C toolchain

  1 of 6 usable
  this is expected off CI - see docs/BUILD.md for the runner matrix
```

**That failure is the measurement, not a defect.** `cgo` means six targets need six C toolchains, and the
decision — **a native runner per OS** — is recorded in [`../../../docs/BUILD.md`](../../../docs/BUILD.md)
with the reason: macOS cannot be cross-compiled without its SDK, and that SDK's licence does not permit
redistributing it.

**And `make build-all` checks the artefact, not the exit code.** Measured carelessly this reports *5 of 6*
(no cgo in the tree) or *3 of 6* (cgo in the tree but not in the binary) — and two of those "passes" were
Windows binaries containing no SQLite at all. **A binary that builds and cannot open a database is worse
than one that fails to build**, because the failure moves from CI to a user.

---

## 2. The extension is compiled in, not loaded

```
./dist/aeon --vectors
```

**You should see:**

```
sqlite-vec  v0.1.6  (statically linked)
```

**No load step, and no way to be missing it.** That is the consequence of B3 choosing `cgo`: the extension
is in the binary. It also means **S22 has one less failure mode** than the earlier plan gave it — there is
no "extension absent" state to handle.

**It failed if** this errors. Because the extension is statically linked, a failure here means the binary
was built without cgo — which is a different problem from a misconfigured machine and should read
differently.

---

## 3. The daemon starts and says where it lives

```
./dist/aeon serve
```

**You should see**, on the first two lines:

```
endpoint  /run/user/1000/aeon/rpc.sock
store     /home/you/.local/state/aeon/state.db  (not created yet — S4)
```

**It says `endpoint`, not `socket`**, because on Windows it is a named pipe and one word has to cover both.

**Both paths matter.** Every later slice's demo starts by finding them (B10), and a daemon that hides where
it lives costs one question per demo for the next twenty-two slices.

**And it needs no configuration at all** — the signing key is generated on first run, and the endpoint is
enabled by a code-level pin rather than an env var.

Then, in another terminal:

```
ss -lx | grep aeon
printf '{"jsonrpc":"2.0","id":1,"method":"version"}\n' \
  | socat -T3 STDIO,ignoreeof UNIX-CONNECT:/run/user/1000/aeon/rpc.sock
```

**`ignoreeof` is not decoration.** `socat -t2 -` and `nc -U -q2` half-close the socket at stdin EOF and
**lose the reply** — you get empty output and exit 0, which looks exactly like a daemon that ignored you.
Worse, a *parse error* still comes back, so the plain form shows malformed JSON being answered and
`version` being ignored. Finding 7 below.

**You should see** the socket listed, and the daemon **answer**:

```
{"jsonrpc":"2.0","id":1,"result":{"buildTime":"…","gitCommit":"…","version":"0.0.0-dev"}}
```

**One method exists, and only one.** S2 builds the method table; this is the counterpart of T5's Tauri
command, so that the app has something real to ask. Anything else:

```
{"jsonrpc":"2.0","id":2,"error":{"code":-32601,"message":"method \"nope\" is not registered"}}
```

Check its permissions:

```
ls -l /run/user/1000/aeon/rpc.sock
ls -ld /run/user/1000/aeon
```

**You should see `srw-------` and `drwx------`** — owner only, both. That is Aeon's entire access-control
story for first-party clients (P26), so if either is group- or world-readable this demo has **failed** even
though everything appears to work.

**The directory matters as much as the socket**, and `localrpc` enforces it: an endpoint cannot live in a
directory you do not own. `/tmp/x.sock` fails with `chmod /tmp: operation not permitted`.

Then `Ctrl-C`.

**You should see** `shutdown complete`, and **the endpoint gone**. A leftover socket makes the next start
ambiguous.

---

## 4. A second daemon refuses, and names the first

Start one, then start another:

```
./dist/aeon serve          # terminal A
./dist/aeon serve          # terminal B
```

**You should see B refuse, naming the holder:**

```
aeon: another aeon daemon is already running
  endpoint  /run/user/1000/aeon/rpc.sock
  holder    pid 906062

stop it first, or set LOCALRPC_ENDPOINT_PATH to run a second daemon on a different endpoint
```

**It failed if** the message is a bind error from inside the framework. That error is true and useless — it
does not say another daemon is running, or which one. Aeon claims the endpoint **before** `Initialize` so it
can say something better (O5).

### And a crash does not leave the daemon unstartable

```
kill -9 <pid of A>        # a socket file is left behind
ls -l /run/user/1000/aeon/rpc.sock   # still there
./dist/aeon serve         # starts normally
```

**You should see it start**, reclaiming the stale endpoint.

**It failed if it refuses.** A stale socket with no live holder is **reclaimed, not refused** — otherwise
one bad shutdown makes the daemon unstartable, which is the opposite of what S7's recovery is for, and
turns a crash into a manual `rm` nobody documented.

---

## 5. The registered service list is exactly what was decided

```
./dist/aeon --print-services
```

**A top-level verb, not a flag to `serve`** — the reason to ask *"what would this daemon run"* is usually
that you do not want it running yet, so it answers **without starting anything.**

**You should see:**

```
registered services (2)
  api          pinned OFF  — configuration cannot enable it
  localrpc     pinned ON   — configuration cannot disable it

foundational services (app, config, logging) are wired by system.Initialize
and are deliberately absent from this list.
```

**Read the two columns separately. Registered is not running.**

`api` is registered so the wiring exists from day one, and **pinned off** so nothing listens — the
framework defaults it to `Enabled:true` on `:8080`, and `ForceOff` means **configuration cannot turn it on
at all.** S19 removes that pin together with the explicit-address and key requirements.

Check it:

```
ss -ltn | grep 8080
```

**You should see nothing.**

**It failed if** anything else appears — `redis`, `jobs`, `oauth`, `metrics`, `swagger`. Those are **never
named** in Aeon's registration: it is an allow-list, not a subtraction and not a config flag (B8). A
config-disabled service is one a stray key re-enables, and every registered service is audit surface in a
program that runs shell commands.

**This is also the check that catches a dependency upgrade** (B11). It already has: going from framework
`v0.10.0` to `v0.10.1` added **nine** registerable services, and **the daemon did not widen by one line** —
the allow-list made them inert and `services.golden` proved it.

---

## 6. `make dev` starts both, and stopping it stops both

```
make dev
```

**You should see** the prerequisite check, then the daemon's two startup lines, then the Tauri window
showing:

```
Aeon
daemon    connected · daemon 0.0.0-dev
```

Then `Ctrl-C` in the terminal.

```
pgrep -fa aeon
```

**You should see nothing.** **It failed if** the app is still running or the daemon is orphaned — B7 exists
because two terminals of friction compounds across twenty-two slices, and an orphan is worse than friction:
the next `make dev` then refuses because the endpoint is held.

### On Linux this needs system packages, and `make dev` says so

```
make check-prereqs
```

**You may see:**

```
note: Tauri's Linux deps are missing (webkit2gtk-4.1, gtk+-3.0).
      the daemon runs; the app will not build. see ui/README.md
```

**The check runs before anything starts** (O3). A `make dev` that half-starts — daemon up, app failed
silently — is the worst outcome, because the app's absence reads as the app being broken rather than
absent.

```
make dev-daemon      # the daemon alone, when the app cannot build
```

---

## 7. The tests are green, and they stay green

```
make test
make lint
make fmt && git status --porcelain
```

**You should see** tests pass — **Go and the UI** — lint clean, and the last command print nothing.

```
ok  github.com/cjlapao/aeon/cmd/aeon
ok  github.com/cjlapao/aeon/internal/appenv
ok  github.com/cjlapao/aeon/internal/bootstrap
ok  github.com/cjlapao/aeon/internal/database
ok  github.com/cjlapao/aeon/internal/endpoint
ok  github.com/cjlapao/aeon/internal/paths
ok  github.com/cjlapao/aeon/internal/secrets
✓ ui/test/connection.test.ts (4)
✓ ui/test/app.test.tsx (5)
```

`make test` being green is a standing condition, not an S1 achievement: from here on, **a slice that leaves
it red has not landed.**

---

## Findings from the walk

**Recorded rather than quietly fixed, except finding 4.**

| # | Finding | Resolution |
| :--: | --- | --- |
| 1 | §1 claimed `make build` produced **six** binaries. It produces one — the host's | **Demo corrected.** `build-all` is the six-target one, and it fails off CI by design |
| 2 | §1 referred to `./dist/aeon-linux-amd64`. `make build` writes `dist/aeon` | **Demo corrected** |
| 3 | §2 expected `socket …` on one line with the version. Reality is `endpoint` and `store` on two lines | **Demo corrected.** "Endpoint" because Windows uses a named pipe |
| 4 | §5 documented `serve --print-services`, which **started the daemon** | **BUG, FIXED.** Extra arguments after a verb are now refused with a non-zero exit. A flag accepted and ignored is worse than one rejected — the person believes they asked for something |
| 5 | §5's expected list was the pre-`v0.10.1` kept-set (`config logging core database …`). Reality is two registered services with their pins | **Demo corrected.** The foundational three are wired by `system.Initialize` and deliberately absent |
| 6 | §3 (`make dev`) **could not be walked on this machine** — Tauri's Linux packages are absent | **CLOSED 2026-10-15.** Walked by the operator once the packages were installed: `make dev` starts both, the window shows the daemon's version. `make check-prereqs` and `make -C ui check-deps` report the packages before anything starts; `make dev-daemon` remains the fallback |
| 7 | §2's own `socat -t2 -` command **returns nothing**. Found later, writing `TESTSPEC.md`: socat half-closes at stdin EOF and the response is lost — while a parse error still comes back, so the dispatcher looks selectively broken | **Demo corrected** to `socat -T3 STDIO,ignoreeof`. Not an Aeon defect, but a documented command that does not work is worse than none |

**Three demo steps did not exist and were added**: `--vectors` (§2), the second-instance refusal and
stale-endpoint reclaim (§4), and the prerequisite check (§6).

---

## What this demo does **not** show

- **No protocol.** One method answers — `version` — and it exists so T5's window has something real to
  ask. **S2** builds the method table, subscriptions and the event stream.
- **No store.** `--vectors` proves the extension is linked; **S4** creates the schema, and it does so
  through the framework's `database` service rather than its own connector.
- **No loop.** Nothing runs a step, assembles context, or calls a model. **S5** is the skateboard.
- **No app capability.** The window asks one question. **S9** builds the app — and settles the two things
  `@cjlapao/ui-kit` cannot express: the ladder's `not-needed`/`skipped` states, and socket liveness.
- **Nothing, as of 2026-10-15.** The last gap — `main.rs`, the Tauri command wrapper, which had never
  been compiled here — was closed by the operator walking
  [`TESTSPEC.md`](TESTSPEC.md) §7 on a machine with Tauri's system packages. Every section of that spec
  passes. See [`../../../ui/README.md`](../../../ui/README.md).

The garage has a car in it that starts. **S2 teaches it to talk.**
