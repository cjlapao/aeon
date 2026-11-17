# S1 — manual test spec

**For a person at a terminal.** Everything needed to verify the garage: configuration, payloads, exact
expected output, and what counts as a failure.

> **Written 2026-10-14 against commit `2ce3a2a`.** Every output below was **captured from a real run**, not
> composed — so a mismatch is a real difference, not a typo in this document.

| | |
| --- | --- |
| **Time** | ~25 minutes, or ~10 skipping §7 (Tauri) |
| **Where** | the repository root, then `aeon/` |
| **Prerequisites** | Go ≥1.26, and for §7 only: Node, Rust, and Tauri's system packages |
| **Companion** | [`DEMO.md`](DEMO.md) is the narrative; this is the checklist |

**Two things to know before you start.**

**`GOPRIVATE` is required.** The framework's module cannot be verified by the checksum database. The
Makefile exports it, so **use `make` rather than bare `go` commands**, or export it yourself:

```sh
export GOPRIVATE='github.com/cjlapao/*'
```

**Nothing here needs configuration.** The signing key is generated on first run and the endpoint is enabled
by a code-level pin. Every variable in §1 is an *override*.

---

## 1. Configuration reference

### What Aeon reads

| Variable | Default | Effect |
| --- | --- | --- |
| `XDG_STATE_HOME` | `~/.local/state` | Where the store and the signing key live. **Must be absolute** — a relative value is ignored, deliberately, because the store would otherwise follow the working directory |
| `LOCALRPC_ENDPOINT_PATH` | resolved per platform | The socket / named pipe. **Its parent directory must be one you own** |
| `LOCALRPC_APP_NAMESPACE` | `aeon` (set in code) | Namespaces the resolved endpoint path |
| `CURSOR_SIGNING_KEY` | **generated on first run** | Set it only to supply your own; an explicit value always wins and nothing is written to disk |
| `DATABASE_TYPE` | `sqlite` (set in code) | — |
| `DATABASE_SQLITE_STORAGE_PATH` | `$XDG_STATE_HOME/aeon` (set in code) | The framework's own default is `.` — the *working directory* — which is why Aeon overrides it |
| `DATABASE_SQLITE_FILE_NAME` | `state.db` (set in code) | — |

### What the framework reads, that matters here

| Variable | Default | Note |
| --- | --- | --- |
| `LOCALRPC_ENABLED` | **ignored** | `ForceOn` in code. Configuration cannot turn the endpoint off |
| `SERVER_API_ENABLED` | **ignored** | `ForceOff` in code. Configuration cannot turn HTTP on |
| `LOCALRPC_MAX_CONNECTIONS` | `64` | Reaching it refuses new connections rather than queueing |
| `LOCALRPC_MAX_FRAME_BYTES` | `1048576` | 1 MiB. Everything on this socket comes from another process |

### A scratch environment for the whole run

**Two blocks, and the distinction matters.** §5 and §6 need a second terminal, and the natural move —
re-pasting the setup — would destroy what the first terminal is doing. So the setup is separate from the
environment, and only the setup is destructive.

#### Block A — the environment. Paste in **every** terminal you open, including the second.

Non-destructive and idempotent. Run it as often as you like.

```sh
export GOPRIVATE='github.com/cjlapao/*'
export AEON_TEST=/tmp/aeon-test
export XDG_STATE_HOME="$AEON_TEST/state"
export LOCALRPC_ENDPOINT_PATH="$AEON_TEST/run/rpc.sock"
mkdir -p "$AEON_TEST/run" "$AEON_TEST/state"
chmod 700 "$AEON_TEST/run" "$AEON_TEST/state"
cd aeon      # skip if you are already there
```

#### Block B — the reset. **Only** at the start, and never while a daemon is running.

```sh
if pgrep -x aeon >/dev/null; then
  echo "REFUSING: a daemon is running — stop it first (pgrep -x aeon | xargs -r kill)"
else
  rm -rf "$AEON_TEST"
  echo "reset; now re-run block A"
fi
```

> ### Why block B guards itself
>
> **Measured, by a tester following an earlier draft of this document.** `rm -rf "$AEON_TEST"` in a second
> terminal deleted the socket and the signing key out from under the daemon running in the first. The
> result is quietly confusing: an unlinked unix socket **keeps its listener and its open connections**, so
> the daemon goes on running and `ss -lx` still shows it LISTEN on that path — but the path is gone, so no
> new client can ever reach it, and §5.2's `ls` reports a socket that does not exist.
>
> Nothing reports this. The daemon does not notice its own endpoint has been removed.
>
> **If you are in that state** — `pgrep -x aeon` finds a process, `ss -lx` shows the path, and `ls` does
> not — restart the daemon:
>
> ```sh
> pgrep -x aeon | xargs -r kill
> # re-run block A, then `./dist/aeon serve` again
> ```

**Why `chmod 700`:** `localrpc` sets the endpoint's parent directory to owner-only. A path directly in
`/tmp` fails with `chmod /tmp: operation not permitted` — which is the guard working, not a bug.

---

## 2. Build and test

| # | Command | Expected | Fails if |
| :--: | --- | --- | --- |
| 2.1 | `make build` | `built dist/aeon` — **one** binary, this platform's | more than one, or an error |
| 2.2 | `make test` | 7 Go packages `ok`, then **9 UI tests passed** | anything fails, or the UI is silently skipped |
| 2.3 | `make vet` | no output | any finding |
| 2.4 | `make guard` | `guard ok: no scaffolding internal/ imports, no replace, no vendor` | any hit |
| 2.5 | `make fmt && git status --porcelain -- aeon` | run `make fmt` twice; the **second** run changes nothing | the second run reformats |
| 2.6 | `make ci` | all of the above, green | anything |
| 2.7 | `make -C ui help` | the app's targets, and the endpoint it will use | the endpoint is not `<repo>/aeon/.dev/aeon.sock` |

**2.7 matters because of what it prints last.** The app's `tauri-dev`, the root `make dev`, and the VS Code
launch configs must all name the *same* socket. They each derive it from one default, and an app pointed at
a different socket from the daemon it should be talking to is the most confusing failure in this slice —
the window says *not connected* while a daemon is running two feet away.

### 2.8 The debug configs — optional, but cheap

Only if you use VS Code. Open `aeon.code-workspace` (repository root), or `aeon/` as a folder.

| Pick | Expected |
| --- | --- |
| **Daemon: serve (dev endpoint)** | starts under the debugger; a breakpoint in `internal/endpoint/claim.go` binds and hits |
| **Go: debug tests in this package** | with any `_test.go` open, its package's tests run under the debugger |
| **App: debug tests (vitest)** | a breakpoint in `ui/test/app.test.tsx` binds |
| **Stack: daemon + desktop app** | **needs Tauri's system libraries** — §7 |

| | |
| --- | --- |
| **FAILS if** | the daemon config writes to your real store. It must use `.dev/state` — check the `store` line it prints |
| **FAILS if** | a Go breakpoint does not bind. Delve builds its own binary; if `CGO_ENABLED` were lost the debugged daemon would differ from the built one |
| **Not a failure** | **App: desktop shell (Rust)** missing from the picker — that means CodeLLDB is not installed (`.vscode/extensions.json` recommends it) |

Verified here under delve 1.26: breakpoints bind in both `cmd/aeon` and `internal/`.

**2.4 is the one worth understanding.** It asserts `aeon/` contains no copy of the framework, no `replace`
directive and no `vendor/`. A copy would compile fine and quietly break the eventual `git subtree split`.

---

## 3. The six-target build — **expected to fail**

```sh
make build-all
```

**Expected — this exact shape, with your toolchains deciding the count:**

```
  linux/amd64            ok
  linux/arm64            FAILED - no C toolchain
  darwin/amd64           FAILED - no C toolchain
  darwin/arm64           FAILED - no C toolchain
  windows/amd64          FAILED - no C toolchain
  windows/arm64          FAILED - no C toolchain

  1 of 6 usable
  failing: linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64
  this is expected off CI - see docs/BUILD.md for the runner matrix
```

Exit code **1**.

**There are three outcomes per target, not two**, and the middle one is why this target exists:

| Reported | Meaning |
| --- | --- |
| `ok` | built, **and the artefact contains `sqlite3_auto_extension`** |
| `BUILT BUT EMPTY - cgo was silently dropped` | a binary was produced with **no SQLite in it** |
| `FAILED - no C toolchain` | expected off CI |

| | |
| --- | --- |
| **PASS** | your host target reports `ok` |
| **FAIL** | **any target reports `BUILT BUT EMPTY`.** That is the dangerous case — a binary that runs until it touches the database |
| **Not a failure** | the five cross-target failures. `cgo` means six targets need six C toolchains; the decision is a native runner per OS, recorded in [`../../../docs/BUILD.md`](../../../docs/BUILD.md) |

**Why the artefact is checked and not the exit code.** Measured: carelessly this reports *5 of 6* (no cgo
in the tree) or *3 of 6* (cgo in the tree but not in `./cmd/aeon`) where the truth was **1 of 6** — and two
of those "passes" were Windows binaries containing **no SQLite at all**. A binary that builds and cannot
open a database is worse than one that fails to build, because the failure moves from CI to a user.

---

## 4. The verbs

All four answer **without starting anything** — no endpoint bound, no store created.

### 4.1 `version`

```sh
./dist/aeon version
```

```
aeon 0.0.0-dev
  built   2026-10-14T19:14:52Z
  commit  2ce3a2a
  go      go1.26.6 linux/amd64
```

**The Go line must be present.** `cgo` is on, so the toolchain is part of what the binary *is*.

### 4.2 `--vectors`

```sh
./dist/aeon --vectors
```

```
sqlite-vec  v0.1.6  (statically linked)
```

**FAILS if** this errors. The extension is compiled in, so a failure means **the binary was built without
cgo** — a different problem from a misconfigured machine.

Optional, to prove there is no load step:

```sh
strace -f -e trace=openat ./dist/aeon --vectors 2>&1 | grep -ciE 'sqlite|vec'
# expected: 0
```

**Do not assert "zero shared objects".** A cgo binary legitimately opens `libc.so.6`, `libm.so.6` and
`ld.so.cache`. The claim is that **SQLite is not among them.**

### 4.3 `--print-services`

```sh
./dist/aeon --print-services
```

```
registered services (2)
  api          pinned OFF  — configuration cannot enable it
  localrpc     pinned ON   — configuration cannot disable it

foundational services (app, config, logging) are wired by system.Initialize
and are deliberately absent from this list.
```

Then:

```sh
ss -ltn | grep 8080        # expected: nothing
ls "$XDG_STATE_HOME"       # expected: empty — nothing was created
```

| | |
| --- | --- |
| **FAILS if** | anything else is listed — `redis`, `jobs`, `oauth`, `metrics`, `swagger` |
| **FAILS if** | `:8080` is listening. `api` is **registered and pinned off**; the framework defaults it to `Enabled:true` on `:8080` |
| **FAILS if** | the two columns stop being distinguishable. A reader seeing `api` listed would conclude the daemon listens |

### 4.4 Argument handling

| Command | Expected | Exit |
| --- | --- | :--: |
| `./dist/aeon` | usage | 0 |
| `./dist/aeon help` | usage | 0 |
| `./dist/aeon bogus` | `aeon: unknown command "bogus"` + usage, on **stderr** | **2** |
| `./dist/aeon serve --print-services` | `aeon: "serve" takes no arguments, but 1 were given: [--print-services]` | **2** |
| `./dist/aeon version --anything` | same shape | **2** |

**The third row was a real bug.** It previously **started the daemon** — the dispatch read `os.Args[1]` and
dropped the rest. **FAILS if a daemon starts**: a flag accepted and ignored is worse than one rejected,
because the person believes they asked for something.

---

## 5. The daemon

### 5.1 It starts and says where it lives

Terminal **A**:

```sh
./dist/aeon serve
```

**Expected — the first two lines of stdout, after the banner:**

```
endpoint  /tmp/aeon-test/run/rpc.sock
store     /tmp/aeon-test/state/aeon/state.db  (not created yet — S4)
```

| | |
| --- | --- |
| **FAILS if** | either line is missing. Every later slice's demo begins by finding them (B10) |
| **FAILS if** | any line contains `ERROR` |
| **FAILS if** | the endpoint resolves under `go-backend-scaffolding` rather than `aeon` — unset `LOCALRPC_ENDPOINT_PATH` to check the namespace default |

### 5.2 Permissions — the whole access-control story

Terminal **B** — run **block A** there first (block A only; block B would delete what terminal A just
created):

```sh
ls -l  "$LOCALRPC_ENDPOINT_PATH"
ls -ld "$(dirname "$LOCALRPC_ENDPOINT_PATH")"
ls -l  "$XDG_STATE_HOME/aeon/cursor.key"
```

| Thing | Expected | |
| --- | --- | --- |
| the socket | `srw-------` | owner only |
| its directory | `drwx------` | owner only |
| `cursor.key` | `-rw-------` | it is a secret |

**FAILS if any is group- or world-readable**, even though everything appears to work. This is P26 — Aeon's
entire access control for first-party clients.

### 5.3 Payloads over the socket

**One method exists.** Use the `socat` form below **exactly as written**, or the Python client at the end
of this section.

> ### Use `ignoreeof`, or you will misread every result here
>
> **`socat -t2 - UNIX-CONNECT:...` and `nc -U -q2` both lose the reply.** Measured, on this daemon: both
> half-close the socket when stdin reaches EOF, and the daemon's dispatched response is lost in the race.
> You get **empty output and exit 0** — indistinguishable from a daemon that ignored you.
>
> What makes this genuinely misleading: a **parse error still comes back**, because it is written
> synchronously before the close is noticed. So the plain form shows you malformed JSON being answered and
> `version` being ignored, and the obvious conclusion — *"the dispatcher is broken"* — is wrong.
>
> `-T3 STDIO,ignoreeof` keeps the write side open for 3 seconds. **If a command here returns nothing,
> check you used that form before reporting a failure.**

> ### ⚠️ S2 changed §5.3. Read this before running it.
>
> **`initialize` is now mandatory and first.** S2 made version negotiation a precondition for every other
> method, so the single-frame commands below now answer **`-31001 connection is not initialized`** instead
> of a result. That is S2 working, not S1 regressing.
>
> **On a build that includes S2**, each of these needs a handshake on the same connection first:
>
> ```json
> {"jsonrpc":"2.0","id":0,"method":"initialize","params":{"clientName":"test","clientVersion":"0","protocolVersion":1}}
> ```
>
> which is two frames on one connection — easier through the Python client at the end of this section, or
> simply `./dist/aeon ping` and `./dist/aeon version`, which do the handshake for you.
>
> **§5.3.2 to §5.3.5 are unaffected**: an unknown method, malformed JSON, a notification and an oversized
> frame are all transport-level conditions, answered before any handshake check.
>
> `docs/aeon/S2-protocol/DEMO.md` §2 is the S2-era version of this check, and it asserts the `-31001`
> deliberately.

#### 5.3.1 `version` — the only registered method

```sh
printf '{"jsonrpc":"2.0","id":1,"method":"version"}\n' \
  | socat -T3 STDIO,ignoreeof UNIX-CONNECT:"$LOCALRPC_ENDPOINT_PATH"
```

**Expected** (field order may vary; JSON is unordered):

```json
{"jsonrpc":"2.0","id":1,"result":{"buildTime":"2026-10-14T19:14:52Z","gitCommit":"2ce3a2a","version":"0.0.0-dev"}}
```

**FAILS if** `version` differs from §4.1's. **This must be the daemon's build** — if it is the client's or
a constant, the socket was never consulted.

**On an S2 build** this returns `-31001` without a preceding `initialize` — see the note above.

#### 5.3.2 An unknown method

```sh
printf '{"jsonrpc":"2.0","id":2,"method":"nope"}\n' \
  | socat -T3 STDIO,ignoreeof UNIX-CONNECT:"$LOCALRPC_ENDPOINT_PATH"
```

```json
{"jsonrpc":"2.0","id":2,"error":{"code":-32601,"message":"method \"nope\" is not registered"}}
```

**`-32601` is JSON-RPC's `method not found`.** The dispatcher is live; S2 fills the table.

#### 5.3.3 Malformed JSON

```sh
printf 'not json at all\n' | socat -T3 STDIO,ignoreeof UNIX-CONNECT:"$LOCALRPC_ENDPOINT_PATH"
```

**Expected — captured:**

```json
{"jsonrpc":"2.0","id":null,"error":{"code":-32700,"message":"invalid JSON"}}
```

`id` is `null` because there was no parseable request to take an `id` from — that is correct JSON-RPC.
**FAILS if** the daemon crashes or hangs.

#### 5.3.4 A notification (no `id`)

```sh
printf '{"jsonrpc":"2.0","method":"version"}\n' | socat -T3 STDIO,ignoreeof UNIX-CONNECT:"$LOCALRPC_ENDPOINT_PATH"
```

**Expected:** **no response** — JSON-RPC notifications are not answered. A reply here is a spec violation.

**This is the one case where empty output is the pass**, which is why the `ignoreeof` form matters: with it,
silence here means the daemon chose not to answer. Without it, silence means nothing at all.

#### 5.3.5 Oversized frame — the 1 MiB bound

```sh
python3 -c 'import json;print(json.dumps({"jsonrpc":"2.0","id":5,"method":"version","params":{"x":"A"*2000000}}))' \
  | socat -T5 STDIO,ignoreeof UNIX-CONNECT:"$LOCALRPC_ENDPOINT_PATH"
```

**Expected — captured:**

```json
{"jsonrpc":"2.0","id":null,"error":{"code":-32600,"message":"frame exceeds max_frame_bytes (1048576); connection closed"}}
```

A `Broken pipe` from `socat` or Python **after** that line is correct — the daemon named the limit and then
closed. **FAILS if** the request succeeds, or the daemon dies: everything on this socket arrives from
another process.

#### The Python client — immune to the trap above

```sh
cat > /tmp/rpc.py <<'EOF'
import socket, sys, os
s = socket.socket(socket.AF_UNIX); s.settimeout(5)
s.connect(os.environ["LOCALRPC_ENDPOINT_PATH"])
s.sendall((sys.argv[1] + "\n").encode())
try:    print(s.recv(65536).decode().strip() or "<empty>")
except socket.timeout: print("<no response>")   # for 5.3.4 this is the PASS
EOF
python3 /tmp/rpc.py '{"jsonrpc":"2.0","id":1,"method":"version"}'
```

### 5.4 Clean shutdown

`Ctrl-C` in terminal **A**.

```sh
ls "$LOCALRPC_ENDPOINT_PATH"      # expected: No such file or directory
```

**Expected:** `shutdown complete` in A's output, and **the endpoint gone**.
**FAILS if** the socket remains — a leftover makes the next start ambiguous.

---

## 6. Single instance, and crash recovery

### 6.1 A second daemon refuses and names the first

Terminal **A**: `./dist/aeon serve` · Terminal **B**: block A, then `./dist/aeon serve`

**Expected in B**, exit **1**:

```
aeon: another aeon daemon is already running
  endpoint  /tmp/aeon-test/run/rpc.sock
  holder    pid 906062

stop it first, or set LOCALRPC_ENDPOINT_PATH to run a second daemon on a different endpoint
```

| | |
| --- | --- |
| **FAILS if** | the message is a bind error from inside the framework. That is true and useless — it does not say another daemon is running, or which one |
| **FAILS if** | no pid is named on Linux. `SO_PEERCRED` is available, and *"stop pid 906062"* is actionable where *"something is running"* is not |
| **PASS** | on macOS/Windows, `a live process (pid unavailable on this platform)` is correct — inventing a pid would send somebody to kill the wrong process |

### 6.2 A crash must not make the daemon unstartable

**The most important case in this spec.**

```sh
./dist/aeon serve &            # terminal A, backgrounded
sleep 3
kill -9 %1                     # SIGKILL: no cleanup runs
ls -l "$LOCALRPC_ENDPOINT_PATH"   # the socket file is still there
./dist/aeon serve              # must START, reclaiming it
```

**Expected:** it starts normally and prints its two lines.

**FAILS if it refuses.** A stale socket with no live holder is **reclaimed, not refused** — otherwise one
bad shutdown makes the daemon unstartable, which is the opposite of what crash recovery is for, and turns
a crash into a manual `rm` nobody documented.

> **Do not clean up with `pkill -f 'aeon serve'`.** The pattern matches *your own shell's* command line,
> because that string appears in it — this killed the terminal doing the verification. Use
> `pgrep -x aeon | xargs -r kill` instead.

### 6.3 A second endpoint is allowed

```sh
mkdir -p "$AEON_TEST/run2" && chmod 700 "$AEON_TEST/run2"
LOCALRPC_ENDPOINT_PATH="$AEON_TEST/run2/rpc.sock" ./dist/aeon serve
```

**Expected:** it starts, alongside the first. The refusal is per-endpoint, not per-machine.

---

## 7. The app — **the part I could not verify**

> **CLOSED 2026-10-15 — this section now passes.** It was written as the one genuine unknown in this
> document: `src-tauri/main.rs` had never been compiled, because the machine that wrote the spec lacks
> Tauri's Linux packages. The operator installed them and walked every step here; the window shows the
> daemon's version.
>
> **The heading stays as it was.** The point of the spec is that it distinguishes what ran from what was
> assumed, and the record of which half was which is more useful than a document that reads as though
> everything was always fine. On a fresh machine this is still the section that needs §7.1 first.

### 7.1 Prerequisites

```sh
make check-prereqs      # Go, Node, Rust
make -C ui check-deps   # Tauri's system libraries, with the install line for your distro
```

Missing Go, Node or Rust → `check-prereqs` **refuses, naming which, before starting anything**.
Missing Tauri's libraries → `check-prereqs` notes it and continues (the daemon still runs), while
`check-deps` **fails** — and `make -C ui tauri-dev` depends on it, so it refuses early rather than failing
deep inside `glib-sys`'s build script.

```sh
# Debian / Ubuntu
sudo apt install libwebkit2gtk-4.1-dev libgtk-3-dev \
                 libayatana-appindicator3-dev librsvg2-dev patchelf
# Fedora
sudo dnf install webkit2gtk4.1-devel gtk3-devel \
                 libappindicator-gtk3-devel librsvg2-devel patchelf
```

macOS: Xcode Command Line Tools. Windows: WebView2, which ships with Windows 11.

### 7.2 The web layer alone — verified, re-verifiable in seconds

```sh
make -C ui install    # npm ci, but only if the lockfile moved
make -C ui test       # expected: 9 passed
make -C ui build      # expected: a bundle, ~552 KB CSS, ~502 KB JS
```

`make ui-install`, `ui-test` and `ui-build` from `aeon/` are the same three, delegated. `make -C ui help`
lists what the root does not forward.

**Also check the stamp works**, since it decides whether anything reinstalls:

```sh
make -C ui install    # expected: "Nothing to be done for 'install'."
touch ui/package.json
make -C ui install    # expected: npm ci runs
```

### 7.3 The window

```sh
make dev                 # daemon and window together
# or, with the daemon already running under a debugger:
make -C ui tauri-dev
```

**Expected:** the daemon's two lines, then a window:

```
Aeon
daemon    connected · daemon 0.0.0-dev
```

| | |
| --- | --- |
| **FAILS if** | the version shown is the **app's** rather than the daemon's. An app showing its own version proves the window rendered and **nothing about the socket** — the one thing this exists to demonstrate |
| **FAILS if** | `cargo` errors in `glib-sys` or `gdk-pixbuf-sys` — the system packages are missing (7.1) |

### 7.4 The three states

| How to produce it | Expected |
| --- | --- |
| daemon running | `connected · daemon 0.0.0-dev` |
| **no** daemon (`make ui-install && cd ui && npm run tauri dev`) | `not connected — …` **and it names the path it tried** |
| the moment before the first answer | `connecting…` |

**FAILS if** the disconnected state does not show the socket path. It is the first thing anyone debugging
this needs, and the reason the daemon prints its path too — so the two can be compared without reading
source.

**FAILS if** any two states render identically. **A two-state UI lies** during the interval where it has
asked and does not yet know.

### 7.5 Ctrl-C stops both

```sh
# Ctrl-C in the make dev terminal, then:
pgrep -fa aeon
```

**Expected:** nothing.
**FAILS if** either survives. An orphaned daemon means the next `make dev` refuses because the endpoint is
held — and the person has no idea why.

---

## 8. Result sheet

```
commit:        ____________    machine: ____________    os/arch: ____________

§2  build & test        [x] pass  [ ] fail  ____________________________
§3  six targets         ___ of 6 usable     ____________________________
§4  verbs               [x] pass  [ ] fail  ____________________________
§5  daemon & payloads   [x] pass  [ ] fail  ____________________________
  5.2 permissions       [x] srw-------  [ ] drwx------  [ ] -rw------- key
  5.3 payloads          [x] version  [ ] -32601  [ ] malformed  [ ] notification  [ ] oversized
§6  single instance     [x] refuses+pid   [ ] stale reclaim   [ ] second endpoint
§7  the app             [x] pass  [ ] fail  [ ] BLOCKED (no system packages)
  7.4 three states      [x] all three distinct   [ ] path shown when disconnected
```

**Any failure is a regression.** All nine sections passed at `2ce3a2a` — §1–§6 and §2.8 on 2026-10-14,
§7 on 2026-10-15 once Tauri's system packages were installed.

If §7 fails on your machine, start with `make -C ui check-deps`: a missing system library fails deep inside
`glib-sys`'s build script, which reads as a Rust problem rather than an apt one.

---

## 9. Cleanup

```sh
pgrep -x aeon | xargs -r kill     # before the rm, not after — see block B
sleep 1
rm -rf /tmp/aeon-test /tmp/rpc.py
unset XDG_STATE_HOME LOCALRPC_ENDPOINT_PATH AEON_TEST GOPRIVATE
```

Nothing was written outside `$AEON_TEST` — no system service, no user unit, no entry in your real
`~/.local/state`. That is checkable: `ls ~/.local/state/aeon` should be exactly as it was before you
started.

---

## Appendix — what verifying this spec itself turned up

Walking these steps on `linux/amd64` found two things worth knowing, both now folded in above:

| | |
| --- | --- |
| **`socat -t2 -` loses the reply** | §5.3's callout. Half-close on stdin EOF races the response. The plain form makes a working dispatcher look broken, and *only for valid requests* — parse errors still come back |
| **`pkill -f 'aeon serve'` kills your shell** | §6.2's note. The pattern matches the shell's own command line |
| **The setup block, re-pasted in terminal B, deleted the live socket** | §1's blocks A and B. Found by a tester on the first run of this document. The setup is now separate from the environment, and the destructive half refuses while a daemon is running |

None of the three is a defect in Aeon. They are the kind of thing that turns a passing run into a filed
bug, which is why they are in the spec rather than in somebody's memory.

**One of them is worth more than a note, though.** A daemon whose endpoint has been unlinked keeps running,
keeps its listener, and is unreachable by any new client — and **nothing reports it**, at either end. The
daemon does not watch its own socket, and a client gets a connect failure indistinguishable from "not
running". That is a liveness question, which is **S2's** territory rather than S1's; it is recorded here
because it was observed rather than imagined.
