# Design — S59 Execution environment

Rules are **EV1–EV22**, cited from `plan.md` and the task files. Task shapes follow
[`MANAGEMENT-PATTERNS.md`](../MANAGEMENT-PATTERNS.md) (P1–P11); a deviation is recorded here with its reason. Facts
below are from a read of the tree on 2026-10-30; **T0 re-walks every one**.

## Verified facts (the ones the design leans on)

| # | Fact | Where |
| --- | --- | --- |
| F1 | `Spawn` builds `exec.Command(shell, …)`, sets `cmd.Dir`, `cmd.Stdin = nil`, **`cmd.Env = nil`** (⇒ inherits the daemon's environment), `Setpgid`, caps output, kills the group on timeout | `tools/spawn.go:128-160` |
| F2 | The comment on `cmd.Env = nil` says scrubbing was deferred as "a policy decision, which is S11's" | `tools/spawn.go:141-153` |
| F3 | `Spawn`'s proof of policy is a **positional** `policy.AllowToken`; the zero value is inert — "an options struct with zero-value defaults is a door; a positional parameter is a compile-time demand" | `spawn.go:96-113` |
| F4 | `bash`: length bound → verdict seam → `SpawnOptions{Dir: tc.WorkspaceRoot}` → `Spawn`; `BashTimeout` 600 s may only be shortened; output caps 64 KiB; non-zero exit is data | `tools/bash.go:103-190` |
| F5 | The command policy does not analyse `bash -c`, `$()`, backticks, heredocs, a single `|` or `&`, `()` — one string is one command to the matcher | `tools/bash.go:65-69` (`BashHonesty`) |
| F6 | `ToolContext` carries function seams (`CommandVerdict`, `Records`); nil ⇒ the tool is inert (fail-closed) | `tools/read.go:38-71` |
| F7 | The daemon builds the toolset per step in `StepRunner.BuildTools(granted, workspaceRoot, agent, frozen)`; the step id rides the context (`stepctx`) | `daemon/step.go:~380-430` |
| F8 | Loop step params: `WorkspaceRoot: st.Root` | `daemon/blueprint.go:645` |
| F9 | `runOneUnit`: `unitSt := st; unitSt.Position = a.Position; dep.Model(ctx, unitSt, …)`; worktree path `st.ComponentRoot + "/.worktrees/" + kind + "-" + id`; `wt.Path` used only to remove | `loop/advance_parallel.go:217-219,280,353-355` |
| F10 | `loop.attended` nullable (NULL = never declared); `Store.LoopAttended` returns `*bool`; execute.go reads NULL/err as unattended | `store/loops.go:333-345`; `loop/execute.go:68-70` |
| F11 | Provider keys read from the daemon environment into env-sourced routes: `ANTHROPIC_API_KEY`/`OPENAI_API_KEY` (constants `EnvAnthropicKey`, `EnvOpenAIKey`) | `daemon/routes.go:77-110` |
| F12 | Forge token read per request from `AEON_FORGE_TOKEN`, `GITHUB_TOKEN`, `GH_TOKEN` (names built with string concatenation to dodge a repo tripwire) | `forge/github.go:83-91` |
| F13 | MCP stdio env = `PATH`, `HOME` + named variables only | `mcp/client.go:273-295` |
| F14 | Terminal env = `os.Environ()` with `TERM` replaced; ungated by design (LW8) | `terminal/session_unix.go:24-26,146-158` |
| F15 | `git.Client` has an injectable `Exec` and a `realExec` with no `cmd.Env` | `git/plumbing.go:41-60` |
| F16 | UI git reads strip `GIT_*` from `os.Environ()` and add hardening; the rest is inherited | `workspace/git.go:559-573` |
| F17 | `narrow.Key` prints `<redacted>` from `String`, `GoString`, `MarshalJSON`; `Reveal` unexported | `narrow/auth.go:35-47` |
| F18 | The seed engine (S43-extra) declares sets with `Source()`/`Applier`, classes inert/guard; a human-door guard pattern with canaries is S45's PM10 | `S43-extra/design.md` SD2/SD5; `S45/design.md` PM10 |
| F19 | `internal/secrets` exported API **not read** (sandbox-blocked); call sites: `SaveRouteKey`, `ReadRouteKey`, `DeleteRouteKey`, `EnsureCursorSigningKey` | `daemon/memory.go:688,706,727`; `cmd/aeon/serve.go:136` |
| F20 | SQLite via `mattn/go-sqlite3`; migrations are timestamped `.sql` files after the frozen legacy block; rebuilds follow the park/guard pattern | `store/open.go`; `migrations/README.md`; `schema.go:479-523` |

## EV1 — What this slice claims, and what it does not

`host-scrubbed` removes **ambient secrets from the environment** a command sees. It is **not a sandbox**: the process
still runs as the daemon's user, can read every file that user can read (`~/.aws/credentials`, the repo's `.env`),
and can reach the network. The container backend is where file and network isolation come from. This is stated, not
implied: one constant, `execenv.Honesty`, holds the sentences; the `bash` tool description, `environment.vocabulary`,
the Settings panel and `aeon env show` all print it verbatim (the `BashHonesty` pattern, one formatter), pinned by a
test that fails if any surface drops it. A limit the model and the person are not told about is one they may cross
without meaning to.

## EV2 — One record: the environment, and where it is bound

`environment` is a row (rule 6): `id` (`env-<uuid>`), `slug` (immutable, unique), `name`, `description`, `backend`
(`host-inherit | host-scrubbed | container | vm`), `spec` (closed JSON, EV4), `origin` (`system | user`), `legacy`
(0/1), `revision`, `created_at`, `updated_at`, `archived_at`. **Binding** is a second table,
`environment_binding(scope_kind, component_id, environment_id)` with `scope_kind ∈ ('workspace','component')`:
exactly one `workspace` row, at most one row per component (a partial unique index — SQLite treats NULLs as distinct,
so uniqueness of the workspace row is an index, not a column constraint).

**Resolution order** for a step: the component the step's root belongs to → if it is a child with no binding, its
**root component** → the **workspace** binding. One function (`execenv.Resolve`, pure over the three lookups). There
is **no code fallback**: no binding anywhere ⇒ the command is **refused** with `environment-unbound` and the sentence
"no execution environment is bound for this workspace — run `aeon env bind`" (fail-closed, the same posture as bash
with no verdict source, F6). A fresh install is never in that state: the inert seed (EV19) binds `host-scrubbed`.

Why a binding table and not a column on `component`: S44-extra's inherited settings are five named columns resolved
by its `EffectiveComponent`; adding a sixth there is a design amendment to a slice not built by this one. A binding
table gives the same "child inherits root" behaviour through this slice's own resolver, and the workspace default
needs a home that is not a component. If S44-extra later wants it inline, the migration is a copy.

Resolution is **per step, read live** (not pinned per loop): the same argument S32 PA15 made for the parallel cap —
tightening an environment must take effect on the next command, and the step's own record (EV9) says what it ran in.
A person widening an environment mid-loop affects that loop's next step; that is recorded in the change audit (EV17).

## EV3 — Backends, and what each can enforce

A backend is registered code with a **declared capability set**; the record may only ask for what the backend can
enforce, and refuses the rest at write time with a sentence.

| Capability | `host-inherit` (legacy) | `host-scrubbed` | `container` | `vm` |
| --- | --- | --- | --- | --- |
| Environment is an explicit allowlist | no | **yes** | yes | yes |
| Secrets injected by reference, output redacted | no | **yes** | yes | yes |
| Filesystem isolation (only the worktree visible) | no | no | **yes** | yes |
| Egress `none` enforced | no | no (`unenforced`) | **yes** | yes |
| Memory / CPU limits enforced | no | no | **yes** | yes |
| Wall limit (shorten `BashTimeout`) | yes | yes | yes | yes |
| Per-loop services (EV14) | no | no | **yes** | yes |
| Built in this slice | yes (kept) | **yes** | **yes** | **no — slot only** |

`execenv.Capabilities(backend)` is data served by `environment.vocabulary` and `environment.show`. S56 reads it to
decide whether a grant's required isolation can be met (EV22). A `vm` environment can be *created* as a record kind
only after a driver registers; with none, `environment.create` refuses with "no `vm` driver is built into this
daemon" (the registry is the truth, not a flag). **Strength order** (`host-inherit < host-scrubbed < container < vm`)
is used by the downgrade rule (EV6) and by `Restricts` (EV16).

## EV4 — The spec (closed shape; every field validated by one function)

```json
{
  "env": {
    "passthrough": ["PATH", "HOME", "LANG", "LC_*"],
    "set":         {"CI": "1", "TERM": "dumb"},
    "secrets":     ["db-url", "stripe-test-key"]
  },
  "home":   "real",
  "limits": {"wallSeconds": 900, "memoryMiB": 2048, "cpus": 2},
  "egress": {"mode": "unenforced"},
  "image":  {"ref": "registry.example/app-ci@sha256:…", "pull": "missing"},
  "services": [{"name": "db", "image": "postgres:16", "port": 5432,
                "env": {"POSTGRES_DB": "app"}, "secrets": ["db-password"],
                "health": {"tcp": 5432, "timeoutSeconds": 30}}],
  "preview": null
}
```

- `env.passthrough` — names (and `PREFIX_*` patterns) copied **from the daemon's environment at spawn time**. Names
  only are stored; values are never persisted.
- `env.set` — literal name→value pairs (≤ 64 entries, value ≤ 1 KiB). **A name that looks like a credential is
  refused** ("that variable is credential-shaped; give it a secret reference") — a literal is for `CI=1`, never for a
  token. (It is a heuristic and the sentence says so; EV7 is the real mechanism.)
- `env.secrets` — secret-reference **names** (EV7); each must exist, be unarchived, and be resolvable at `bind` time
  (a *check*, not a read — EV7).
- `home` — `real` (the daemon user's `HOME`) or `scratch` (a per-provision empty directory; `container` always).
  `host-scrubbed` supports `real` only in this slice (a scratch directory needs the lifecycle ledger; staged with a
  refusal sentence) — decision D5.
- `limits` — `wallSeconds` shortens `BashTimeout` (never lengthens: F4's rule); `memoryMiB`/`cpus` are
  container-only and refused on hosts.
- `egress.mode` — `unenforced` (host), `none`, `unrestricted`; `egress.allow` (host allowlist) is **refused in this
  slice** with "host allowlists need an egress proxy — staged (S59 EV13)".
- `image` — container only: `ref` (digest-pinned recommended; a floating tag is allowed with a warning in the read),
  `pull ∈ never|missing`.
- `services` — container only (EV14).
- `preview` — reserved, **must be null**; non-null is refused "preview deployments are specified, not built".

**Deny wins** (the PL4 posture): a closed list and patterns, served as data by `environment.vocabulary`, refuse
names in `passthrough`, `set` and `inject_as` even when listed explicitly — credential words (`KEY`, `TOKEN`, `SECRET`,
`PASSWORD`, `PASSWD`, `CREDENTIAL`, `AUTH`, `COOKIE`), the prefixes `AEON_`, `ANTHROPIC_`, `OPENAI_`, `GITHUB_`, `GH_`,
`AWS_`, `AZURE_`, `GOOGLE_`, `GCP_`, the agent sockets (`SSH_AUTH_SOCK`, `GPG_AGENT_INFO`, `DOCKER_HOST`,
`KUBECONFIG`), and the loader/shell-injection variables (`LD_*`, `DYLD_*`, `BASH_ENV`, `ENV`, `SHELLOPTS`, `PS4`,
`IFS`, `PATH`-overrides in `set`). `*PROXY*` names are **warned** (they may carry credentials in the URL) rather
than refused, because corporate proxies are common; the warning points at secret references. The credential words
are a **superset** of `config/creds.go`'s `isCredentialName` marks (`creds.go:~34`), asserted by a test so the two
lists cannot drift apart.

## EV5 — The child environment is built, not inherited

`execenv.ChildEnv` is an **opaque value** with unexported fields, constructed only by `execenv.Build(spec,
daemonEnv func(string) string, injected []Injection)` (host backends) or `execenv.LegacyInherit()` (the legacy backend
only). Its zero value is **inert**: `Spawn` refuses it. It is a **positional** parameter of `Spawn` — F3's argument
applied again: *an options struct with zero-value defaults is a door; a positional parameter is a compile-time
demand*. `SpawnOptions` gains **no** environment field. The old `cmd.Env = nil` line is deleted, and a source grep
test (EV10) fails if it returns.

Build is deterministic and testable: the daemon environment is a function argument (never `os.Getenv` inside), the
output is a sorted `[]string`, and every variable carries a **source** (`passthrough | set | secret:<name>`) so the
panel's preview (EV18) and the step record (EV9) can say *where each name came from* without a value.

## EV6 — Unattended loops never run in the legacy environment *(product call)*

**Rule.** If the resolved environment's backend is `host-inherit` and the loop is **not attended** (declared
unattended **or never declared** — F10, the same fail-closed reading `execute.go` uses), the step runs in the system
`host-scrubbed` environment instead, and the step record says so (`downgraded: true`, reason
"loop is unattended; the inherited environment is refused"). A step with no loop at all (a person's `aeon ask` with a
bash grant) is treated the same way. If the system `host-scrubbed` row has been removed, the command is **refused**
(`environment-unavailable`) — never inherited.

**Why this default.** The leak in §1.1 is a *security* defect, not a preference: a model reading the daemon's provider
keys from a command's output is a credential exposure the operator did not authorise, and an unattended loop is by
definition one nobody is watching. Scrubbing is **strictly more restrictive**, so it needs no grant. The cost is that
a loop which relied on an inherited variable (`AWS_PROFILE`, an `nvm` shim) stops seeing it — the symptom is the
failed command's own output, and the fix is one explicit `passthrough` entry. The
alternative (a legacy opt-out for unattended loops) is a *widening* and belongs to S56 as an explicit, audited grant,
not to this slice. **Attended** loops keep the legacy behaviour on upgraded stores until a person binds a stronger
environment (migration continuity, EV20); `aeon doctor` and the Settings panel warn while the workspace default is the
legacy row. **New installs** bind `host-scrubbed` from first start.

## EV7 — Secret references: where a secret comes from, never what it is

`secret_ref`: `id`, `name` (slug, unique), `description`, `source_kind` (`daemon-env | key-file`; `store` is specified
but built only behind the T0 approval below), `source` (an env-var **name** or an absolute file path), `inject_as`
(the child variable name), `origin`, timestamps, `archived_at`. There is **no value column, and a schema test walks
every table and fails on a column whose name is credential-shaped in `environment*`, `secret_ref`, `step_environment`**.

- **Resolution** happens at spawn time in the daemon: read the env var (`os.Getenv` via the injected getenv) or the
  key file (must be a regular file, not a symlink out of the state area, mode ≤ 0600, owned by the daemon user; the
  `config/creds.go` key-file rule). The value lands in an `execenv.Secret` — an opaque type whose `String`,
  `GoString`, `Format` and `MarshalJSON` all answer `<redacted>` (F17's pattern) and whose bytes leave the package only
  through `Inject`/`Redactor`. A secret **shorter than 8 bytes is refused** at check and at injection
  ("too short to redact reliably").
- **`secretref.check {id}`** resolves and answers `{resolvable, lengthOk, sourceKind}` — booleans, never the value and
  never its length.
- **Daemon credentials are not referenceable.** A reference whose `source` names the daemon's own credentials is refused
  at write ("that variable is the daemon's own credential — give the app its own"): the `AEON_` prefix, the provider key
  names (`EnvAnthropicKey`, `EnvOpenAIKey`, F11), the forge token names (F12), the narrow key env (`narrow.KeyEnv`)
  and the card-key env. The list is **assembled from those constants** (a small exported accessor on `forge`, a
  listed adaptation) — not re-typed here.
- **Writes accept only references** (C17): the socket params have no value field; the CLI has no `--value`, no stdin
  secret path and no prompt (a grep test over the CLI source, the S19 T2 precedent). A person provisions a secret by
  putting it in an environment variable or a key file on the machine — the same way provider keys already work.
- **The `store` kind** (a secret held in `internal/secrets`, imported once from a file the person names) needs that
  package's exported API, which was **not readable** when this slice was planned. **T0 lists it as an approval item**
  for the package owner/operator; until it is approved and read, `source_kind='store'` is refused ("not built") and
  nothing else in the slice waits on it.
- **Agents never see a source.** `secretref_list` for an agent returns `name`, `injectAs` and `sourceKind` — not
  `source` (a file path or variable name is infrastructure detail an agent does not need).

## EV8 — Redaction: the one boundary, and its honest limits

Every byte a command prints crosses **one** place on its way to the model and the transcript: `Spawn`'s capture
(F1) and its container twin (EV9). A `Redactor` built from the step's injected `Secret`s wraps that capture as a
**streaming writer with an overlap carry** (it holds back the last `maxSecretLen-1` bytes so a secret split across two
reads, or straddling the 64 KiB cap, is still matched). A match is replaced by `[secret:<name>]`. Forms matched:
exact bytes, standard and URL-safe base64 (padded and unpadded), lowercase hex, and percent-encoding. The command
string echoed back in the result (`renderRun`'s `$ <command>`) and the stored command-activity text pass through the
same redactor.

**Limits, stated in the tool description and `environment.vocabulary`** (`execenv.RedactionLimits`): a command can
transform a secret into any other form (reverse it, split it, XOR it, ROT13 it) and redaction will not see it;
a secret **written to a file in the worktree** and read back by the `read` tool is outside this boundary; a secret
that appears in a *different* tool's result (a model that guesses it) is not tracked. Redaction is a **second line**
behind not putting the secret in places the model can reach; the first line is that only the commands that need a
variable get it (EV5) and that bash is an explicit grant. A sentinel test (EV acceptance 3) scans every sink.

## EV9 — The execution seam

Consumer-side interface in `tools` (the `RecordsInvoker` pattern, F6):

```go
type CommandExecutor interface {
    Exec(ctx context.Context, req ExecRequest) (RunResult, error)
}
type ExecRequest struct {
    Command   string            // the exact command the verdict covers
    Dir       string            // the step's working root (EV15)
    Timeout   time.Duration     // may only shorten the ceiling
    StdoutCap, StderrCap int
    Verdict   policy.AllowToken // policy first, unchanged
}
```

`ToolContext` gains `Exec func(ctx context.Context) (CommandExecutor, error)`; **nil makes bash inert** (fail-closed,
F6). `BuildTools` closes it over the step: it resolves the loop (step id from `stepctx`), reads attendedness (F10),
resolves the environment (EV2, EV6), resolves and checks secrets (EV7), and returns the executor for the bound backend.
Host backends wrap the refactored `tools.runProcess`; the container backend (EV13) implements the interface in its
own package (`execenv/container` imports `tools`; `tools` imports only the pure `execenv` core — no cycle).

- **Policy is first and unchanged.** The verdict seam and `Spawn`'s token check run before any executor is consulted;
  a refused command never reaches a backend. The runtime's receipts for unsafe tools are untouched (they wrap the
  tool call, not the spawn).
- **The step records what it ran in:** `step_environment(id, step_id, environment_id, backend, downgraded,
  downgrade_reason, refusal_kind, refusal, var_names JSON, secret_refs JSON, resolved_at)` — **names only**, one row per
  *distinct outcome* per step (so a step that was refused and then ran differently shows both), append-only. `step.show` (and S53's history) can answer "what could this step see?".
- Errors are three-part sentences with a closed refusal vocabulary (EV11).

## EV10 — Every daemon-side subprocess is classified, and a guard keeps it that way

| Site | Class | After this slice |
| --- | --- | --- |
| `tools/spawn.go` (bash, `verify.run`) | `step-command` | takes a `ChildEnv` (EV5) from the resolved environment |
| `git/plumbing.go` (`git.Client`, daemon code-actions) | `daemon-git` | `Client.Env` set from `execenv.GitEnv` — the base allowlist plus `SSH_AUTH_SOCK` (needed for ssh remotes), `GIT_TERMINAL_PROMPT=0`; hooks the model wrote run **without** provider keys or the forge token |
| `workspace/git.go` (UI read-only `status`/`diff`) | `ui-read` | `gitEnv()` built from the base allowlist + its existing hardening instead of `os.Environ()`; a listed adaptation of S31's file |
| `mcp/client.go` (stdio servers) | `scrubbed-own` | unchanged (already `PATH`, `HOME` + named — F13) |
| `terminal/session_unix.go` (a person's PTY) | `person-shell` | **unchanged and named as the one deliberate inherit** (LW8): it is the person's own shell |

`TestNoSpawnInheritsTheDaemonEnvironment` walks every non-test `.go` file (asserting it saw ≥ 50), finds every
`exec.Command`/`exec.CommandContext`, and requires each to be in the table above with its class **and** — for the
classes that must scrub — to assign `cmd.Env` from `execenv`. An unlisted site fails; `cmd.Env = nil` and
`os.Environ()` outside `person-shell`/`legacy-backend` fail. A canary feeds it a synthetic violating file and asserts
it is reported (a guard that cannot fail is not a guard).

## EV11 — No silent downgrade; the refusal vocabulary

If the bound environment cannot run — the backend is unavailable, a secret does not resolve, provisioning failed, the
daemon has no driver — **the command is refused**: it does not run, and it never runs on a weaker backend. The refusal
is a tool error carrying a three-part sentence (what / why / do) naming the record and the fix, so the model sees why
nothing ran, **and** it is recorded on the step (`step_environment.refusal_kind`/`refusal`) and emitted as an event
(`step.environment`) for S58 and S55. Reasons (a closed set): `environment-unbound`, `environment-unavailable`,
`secret-unresolvable`, `provision-failed`, `service-unhealthy`. Promoting a *repeated* refusal to a loop-level suspend
with a question is the watchdog's job (S55, EV22) — this slice records and emits, it does not own loop status. The one
*tightening* the daemon does on its own is EV6 (inherit → scrubbed for unattended), recorded, never hidden.

## EV12 — Lifecycle: provision ledger, write-ahead, reconcile, teardown

`environment_provision(id, environment_id, loop_slug, component_root, backend, handle, state, created_at, ready_at,
removed_at, last_error)` with `state ∈ provisioning | ready | tearing_down | removed | failed`. The `loop_worktree`
discipline (PA5, F9): **the row first, then the real action**; teardown reverses it; **settle-once** via
`removed_at`. At daemon start, reconcile reads live rows and asks the backend what actually exists (like
`reconcile_worktree.go`): *adopt* a live resource whose row says `ready`; *remove* a resource with no matching row
(orphan, only ever one that carries the daemon's label — EV13); *finish* a `provisioning` row either way; mark `failed`
with `last_error` otherwise. Teardown runs when the loop reaches `done` or `aborted` (the existing `publishLoop` reactor
point, `daemon/blueprint.go:567-612`); a `blocked` loop keeps its provision for `provisionIdleTTL` (default 24 h,
a setting), then is torn down — a blocked loop that is retried re-provisions (the ledger row is new). Host backends
write no provision rows (nothing to provision); `home: scratch` on a host is the staged case.

## EV13 — The container backend (built here, behind the interface)

A `ContainerRuntime` interface (`Create/Start/Exec/Remove/List/NetworkCreate/…`) with one driver that shells out to
the **`docker`-compatible CLI** (`docker` or `podman`, chosen by the record or discovered on `PATH`; the driver is
tested against a fake runtime always and against a real one only when `AEON_TEST_CONTAINER=1`). Properties, each a test
against the fake's recorded command log:

- **One container per loop**, named `aeon-<slug>-<provisionId>`, labelled `aeon.managed=1` and `aeon.loop=<slug>`
  (reconcile only ever touches labelled resources), long-running (`sleep infinity`), each command a `docker exec` into
  it — so build artifacts persist across a loop's steps.
- **Mounts: exactly one** — the step's working root, read-write, at a fixed path. No other host path, no docker
  socket. Extra mounts are refused at write (v1).
- **Hardening:** non-root user, `--cap-drop ALL`, `--security-opt no-new-privileges`, `--pids-limit`, `--memory`,
  `--cpus` from `limits`, `--network none` when `egress.mode=none`.
- **Secrets never reach argv.** Variables are passed as `--env NAME` with the value in the **environment of the
  `docker` CLI process** (docker reads it from there), not `--env NAME=value`, so a value is not in the host process
  list; the `Exec` call does the same. A test asserts no recorded argv contains a sentinel secret.
- **Output is captured by the same `runProcess` and the same `Redactor`** as a host command (EV8), with the same caps,
  group kill and timeout.
- **Image policy:** `pull: never` (default) fails with `provision-failed` if the image is absent; `missing` pulls.
  A digest-pinned `ref` is recommended; a floating tag is shown with a warning.
- **Unavailable runtime ⇒ refused** (`environment-unavailable`), with the exact reason from the driver — never a host
  fallback (EV11).
- **Egress:** `none` and `unrestricted` only; `allow` is refused (needs a proxy — staged).

## EV14 — Services (per-loop dependencies) and preview deployments

A `services[]` entry (container backend only) is a second container on a **per-loop user network**
(`aeon-<slug>-net`, the app container joins it), started during provisioning, health-checked (`tcp` port or `cmd`,
bounded timeout) **before the loop's first command**, stopped with the loop (same ledger, `owner` column
`service:<name>`). The app container sees `AEON_SERVICE_<NAME>_HOST` and `_PORT` — **names the daemon derives**, not
values a person types — plus the service's own `env` and secret references (a database password is a reference,
injected into the *service* container and, where the spec says, the app). An unhealthy service ⇒ `service-unhealthy`
refused. **Preview deployments are specified only:** the `preview` spec member is reserved (EV4) and refused when
non-null; the shape a preview needs (a build step, a URL, a TTL, an egress-in allowance) is recorded as the
follow-up's brief in this task's Notes, not designed here.

## EV15 — The working directory is the step's, and the finding about S32

The executor's `Dir` is **the step's `WorkspaceRoot`** — one source, no second guess. This slice does not change what
the loop hands a step. Measured (F8, F9): a parallel unit's worktree is created and registered but the model step runs
on a state whose `Root` is the shared root, so the worktree is unused. **T0 writes a failing test that proves it** and
records it as a finding against S32; the fix (pass the worktree path as the unit's step root) is S32's follow-up or
S60's, and the moment it lands the container backend mounts the worktree with **no change here**, because it mounts
whatever `Dir` it is given. If the fix does not land, parallel units share one working tree and the container
isolation of *units from each other* is not real — said in the Settings panel's honesty line, not hidden.

## EV16 — Authority: human door writes; agents read, and may only propose to restrict

`environment.create/update/archive/restore/bind/unbind/teardown` and `secretref.create/update/archive/restore` change
what commands may see or reach ⇒ **P4 human door**: socket methods on the 0600 + peer-credential surface; **absent**
from the narrow surface, agent services, the tool registry and the records catalog (except the propose twin below);
sibling guard tests with canaries in `cmd/aeon`; rule text in `aeon/AGENTS.md` §4. `--yes` on the CLI is a seatbelt
for `bind`, `create` of `host-inherit`, and `teardown`.

**Agents:** reads (`environment_list`, `environment_show`, `environment_effective`, `secretref_list` without sources)
and **one** propose-only tool, `environment_propose_restrict`, creating an inert proposal of the new closed kind
`environment_restrict` (S45 desk; `proposal.kind` table rebuild — P6) committed by a person. **`execenv.Restricts(old,
new)`** is a pure function and the validator: the new spec must be **no weaker on any axis** — `passthrough`, `set`,
`secrets`, `services` are subsets; `limits` not higher (adding a limit where none existed is tighter); `egress` rank
not lower (`unenforced`/`unrestricted` < `none`); backend strength not lower; `image`, `home`, `preview` unchanged.
`allow`-shaped widening is unrepresentable: the proposal schema has no `backend` downgrade path and `additionalProperties:
false` with no identity fields.

## EV17 — Audit: append-only, names only

`environment_change(id, at, actor_kind, actor_id, subject_kind, subject_id, action, revision, spec_after, summary)`
— one row **in the same transaction** as each *changing* write to an environment, a secret reference or a binding;
none for reads, refusals, dry-runs or no-ops (P3). The actor is `table.ActorFor` (the kernel peer), never params.
Append-only **by trigger**. `spec_after` holds the spec JSON — names and literals only, **no secret value can be in it**
because none is accepted (EV7) — so any revision can be diffed and `Restricts`-compared. Summaries are fixed
templates from ids and enums (SEC-033). Revision numbers never reset; history outlives the record (no FK).

## EV18 — The doors (P34: daemon first, CLI second, tools alongside, panel last)

**Socket (all `RequiresInitialize`, none `Debug`; `Table` seam nil ⇒ absent, V13).** Reads:
`environment.list {scope?, backend?, includeArchived?}`, `environment.show {id|slug}`,
`environment.effective {componentId?, attended?}` (what a step on that component would run in — backend, capabilities,
**variable names with sources**, secret reference names, downgrade note; never a value), `environment.vocabulary`
(backends + capabilities, default allowlist, deny list/patterns, egress modes, source kinds, `Honesty`,
`RedactionLimits`), `environment.changes {subject?, limit?}`, `environment.provisions {state?, limit?}`,
`secretref.list {includeArchived?}`, `secretref.show {id|name}`, `secretref.check {id}`. Writes (human door):
`environment.create`, `environment.update {id, spec, expectedRevision}`, `environment.archive`, `environment.restore`,
`environment.bind {scope, componentId?, environmentId, dryRun?}` (returns the daemon's own **statement** of what
changes; `dryRun` returns it without applying), `environment.unbind {componentId}` (the workspace binding cannot be
unbound), `environment.teardown {provisionId}`, `secretref.create/update/archive/restore`. Twin: `environment.proposeRestrict`
(records-service, creates the inert proposal). One error code, `ErrEnvironmentRefused` (what/why/do), from the next
free block (T0 records it).

**CLI:** `aeon env list|show|effective|vocabulary|changes|provisions|create|update|archive|restore|bind|unbind|teardown`
and `aeon secretref list|show|check|create|update|archive|restore` — clients of those methods (P5), `--json` is the
wire result verbatim, the CLI re-validates nothing. **Recorded deviation from P5:** no offline "direct door" — every
operation needs the daemon's resolver, the seed engine and the reconcile state, and `aeon` has no first-run need for
it. **Tools:** the five specs of EV16 in `internal/records/catalog.go` (family `environment`), registered not granted.
**Panel:** Settings → **Environments** (T11): a list with details and the capability chips, a **per-component
effective preview** (names + sources), the secret-reference list (no values, no sources for non-humans), provisions,
the honesty line, and a warning banner while the workspace default is the legacy row.

## EV19 — The seed set `environments.standard` (rule 6, P8) — inert

One system environment row, **`host-scrubbed`** (slug, `origin='system'`), with the default allowlist written out
explicitly in its spec (`PATH, HOME, USER, LOGNAME, SHELL, LANG, LC_*, TZ, TMPDIR`; `set: {TERM: "dumb", CI: "1"}`
is **not** included by default — `CI` changes tool behaviour — D4). Class **inert** and the justification: the row is
*shipped unbound*; shipping it changes nothing about what any command can see. The **first-binding rule**: the seed
applier also binds it as the `workspace` default **only if no workspace binding exists** — that binding is strictly
more restrictive than legacy behaviour (EV6), so it is not guard-affecting, and it is never re-applied over a person's
choice (divergence is never overwritten, SD6). The legacy row (`env-host-inherit`, `legacy=1`) is **not** in the seed:
it is created by the migration only for a store that already has components or loops (EV20), because shipping the
permissive row to every fresh install would defeat the point. Operator rights: `{Edit, Remove}` on the row; the
engine's golden pins content and bumps `Version` when it changes.

## EV20 — The migration and continuity

One timestamped migration (`date +%Y%m%d%H%M%S`, after whatever is merged when T2 starts): create `environment`,
`environment_binding`, `secret_ref`, `step_environment`, `environment_provision`, `environment_change` (+ append-only
triggers, indexes, the partial unique index for the workspace binding). **Conditional legacy row:** `INSERT … SELECT`
the `env-host-inherit` row and the workspace binding **only when** `EXISTS (SELECT 1 FROM component)` or `EXISTS
(SELECT 1 FROM loop)` — an upgraded store keeps today's behaviour exactly (attended loops) until a person binds a
stronger environment; a fresh store gets nothing from the migration and `host-scrubbed` from the seed. No existing
table is rebuilt, so no child is parked and no census is needed beyond the conditional (T2 proves both branches).
`schema.go` and every shipped migration are untouched; the migration-count assertions are bumped by exactly one
(T0 lists them, as S44-extra T0 did).

## EV21 — What does not change

The policy gate and command corpus; the receipts for unsafe tools; `BashTimeout` as the ceiling and the output caps;
which tools are granted (`bash` stays an explicit grant); MCP stdio's environment; the person's terminal; provider
route key storage and the forge token source (the forge client is in-process HTTP — its token never enters a child
process); the `read`/`edit` file tools; S32's worktree creation. The wire gains methods; nothing is removed.

## EV22 — Cross-slice requests (each also appears in the relevant task's Notes)

- **S56 AL** — a grant may *require* an environment (minimum strength, egress `none`) and read
  `execenv.Capabilities`; the unattended rule here is the floor, grants only ever add requirements. S56 must not widen
  EV6 (no "inherit for unattended" grant without a human-door, audited decision of its own).
- **S57 VG** — `verify.run` executes through `ToolContext.Exec`/`CommandExecutor` (EV9), never its own `exec`; its
  limits map to `ExecRequest.Timeout` and the environment's `limits`; it records its environment via `step_environment`.
- **S55 LC** — wall/resource limits are the environment's `limits`; a stop must reach a running `docker exec` (the same
  group-kill path) and teardown must run on `loop.stop`; budgets are S55's.
- **S58 SV** — `provision-failed`, `service-unhealthy`, `environment-unavailable` and a stuck provision are events a
  person wants a notification for.
- **S60 MQ** — re-verification after a rebase runs in a session on the **same** environment record.
- **S61 GI** — goal intake proposes the first environment for a new root component (a proposal of this slice's kind
  where restrictive, else a human-door `bind`).
- **S27 CN** — scheduler-started loops are unattended ⇒ EV6 applies without any S27 code.
- **S62 BK** — the six new tables belong in the backup set; none holds a secret value (EV7), so a backup is safe to
  copy; a restore must not restore `environment_provision` rows as live (they are reconciled at start).
- **S32 owner** — the parallel-unit working directory (EV15).

## Open decisions (each has a recommended default; none blocks T0–T5)

| # | Decision | Options | Default and why |
| --- | --- | --- | --- |
| D1 | Which loops are scrubbed immediately | (a) unattended only; (b) all loops; (c) none | **(a)** — secure where nobody is watching, no break for attended workflows on upgrade; doctor/panel warn; new installs get (b) |
| D2 | First non-host backend | container CLI driver; VM (Parallels) | **container**, interface-first; VM later — no product dependency |
| D3 | `secret_ref` kind `store` | build now; stage | **stage** behind T0's owner approval of reading `internal/secrets`; kinds `daemon-env`, `key-file` ship |
| D4 | `CI=1` in the default `set` | include; omit | **omit** — it changes tool behaviour; a person adds it |
| D5 | `home: scratch` on `host-scrubbed` | build with the lifecycle ledger; refuse | **refuse in v1** (container supplies its own HOME); revisit with real demand |
| D6 | Container egress default | `none`; `unrestricted` | **`none`** with a digest-pinned image carrying dependencies; documented trade-off (offline builds need a warm image) |
| D7 | Blocked-loop provision TTL | 24 h; until archive | **24 h**; a declared S44 setting `execenv.provisionIdleTTL` if S44 has merged when T6 runs, otherwise a named constant (T0 decides) |
| D8 | Floating image tags | refuse; warn | **warn** in reads; digest-pinning is guidance, not a gate |
| D9 | `SSH_AUTH_SOCK` for daemon git | pass; omit | **pass for `daemon-git` only** (ssh remotes need it); never for `step-command` |
| D10 | Writing `environment.update` widening a bound environment while loops run | apply next step; require no running loops | **apply next step**, audited (EV2) |
