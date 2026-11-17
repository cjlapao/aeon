# AGENTS.md — building Aeon

## First: maintain the slice record

**[SLICES.md](docs/aeon/SLICES.md) is the single maintained completion tracker.** Read it before choosing work.
These instructions replace older requirements to mirror completion across STATUS.md, TASKS.md, PLAN.md, and ROADMAP.md, including instructions embedded in existing task specs.

- In the same change as implementation, update the task’s Notes and its task/slice rows in SLICES.md.
- Record what actually exists: implementation paths, decisions, deviations, and dated validation commands/results. Link evidence instead of copying long narratives or suite totals.
- Keep build and validation separate. Uncommitted implementation is not merged work; fixture, replay, native desktop, real provider, and external-service checks are different scopes. State failed, skipped, blocked, or unrun checks explicitly.
- Mark a slice built only after all its specified implementation tasks are accounted for. Preserve open follow-ups and validation gaps; partial slices stay partial.
- At close-out, record the walked DEMO.md with findings and extend the cumulative demo under §4b. A demo script existing on disk does not establish that it ran.
- Keep the four older documents as historical planning/evidence references. Update planning contracts when their scope changes, but do not mirror completion tables or append progress journals there.
- Add slices/tasks to SLICES.md when their files exist. Resolve ID collisions before assigning work. Never infer completion from a spec, checkbox, remembered count, or neighbouring slice.


**Read this before touching anything in `aeon/`.** It says what the project is, where things go, what
arrives free from the framework, and the handful of rules that are not negotiable.

The *reasoning* lives in `docs/aeon/` — slice designs, briefs, plans and
tasks. This file is the map; that is the territory.

| You want | Read |
| --- | --- |
| **The vision, in pictures** | [`docs/aeon/VISION.md`](docs/aeon/VISION.md) — diagrams, mockups, the end state |
| What gets built, in what order | [`docs/aeon/ROADMAP.md`](docs/aeon/ROADMAP.md) |
| **Slice/task completion and validation** | [`docs/aeon/SLICES.md`](docs/aeon/SLICES.md) |
| **What to do, and in what order** | [`docs/aeon/PLAN.md`](docs/aeon/PLAN.md) — prerequisites, phases, the ui-kit |
| The invariants every slice argues from | [`docs/aeon/ARCHITECTURE.md`](docs/aeon/ARCHITECTURE.md) |
| How to turn a slice into a running loop | [`docs/aeon/SEEDING.md`](docs/aeon/SEEDING.md) |
| Your slice's contract | `docs/aeon/S<n>-<name>/brief.md` |
| Your task | `docs/aeon/S<n>-<name>/tasks/T<n>.md` |
| The framework's own architecture bible | `go-backend-scaffolding/AGENTS.md` (1,800 lines; the service contract, the onion model, the standard package layout) |

---

> **Where the work is up to:** [`docs/aeon/SLICES.md`](docs/aeon/SLICES.md) — build state, task evidence,
> implementation locations, and remaining validation gaps. Read it before picking up a task.

## 1. What Aeon is

A daemon plus a desktop app that runs **loops**: long-lived, resumable units of agent work. It replaces
OpenCode for this project's own development.

Three facts shape every decision:

- **The daemon owns loop execution.** Nothing is detached (P29). A daemon crash stops every loop, and a
  crashed loop is recovered on restart — not resumed from memory, replayed from persisted state.
- **A suspended step has no process.** Suspension is *exiting* (P2). Resume is cold, from rows.
- **The socket carries the protocol; HTTP is a narrow, enumerated projection** (P25). One protocol, N
  projections — not one API with two front doors.

## 2. Where Aeon sits

```
go-backend-scaffolding/framework   ← a PINNED MODULE DEPENDENCY (framework/v0.10.1)
          │                          never forked, never vendored, never edited
          │ require
          ▼
aeon/                              ← this folder. Aeon's code and nothing else
  cmd/aeon/                          one binary: daemon AND every client verb (B4)
  internal/
    bootstrap/                       THE ALLOW-LIST + the pins (B8, B11)
    appenv/                          env defaults set BEFORE system.Initialize
    secrets/                         the cursor signing key, generated once
    paths/                           where things live on disk (B10)
    database/                        the driver + the framework's DB contracts
    endpoint/                        single-instance claim, with stale reclaim
    policy/                          the S10 engine: the closed set, the write boundary, the one
                                     formatter, the audit (§4d) — guarded by tests, not by convention
    …                                every other package arrives with its slice
  ui/                                the app — Vite + React + Tauri
    src/                               the web layer
    src-tauri/                         the Rust shell: endpoint, rpc, one command
    Makefile                           the app's own targets; the root one delegates
  scripts/                           the guards CI runs
  docs/BUILD.md                      why six targets need six runners
  .vscode/                           launch configs, tasks, tool settings
  .github/workflows/                 PARKED — see §7
  Makefile                           the only entry point a contributor needs (B6)
  AGENTS.md  UPSTREAM.md             this, and the dependency record
```

**Two Makefiles, one of each thing.** `aeon/Makefile` is the entry point (B6) and `aeon/ui/Makefile` owns
the app; the root one's `ui-*` targets delegate with `$(MAKE) -C ui`. Neither repeats the other, so there
is no second definition of how the app is built that can drift. `make -C ui help` lists what the root does
not forward — `test-watch`, `typecheck`, `tauri-dev`, `check-deps`.

**The debug configs are in `aeon/.vscode/`, not the repository root**, for the same reason as everything
else here: they refer to nothing outside this folder, so the extraction takes them along. Opening the
monorepo root hides them — `aeon.code-workspace` at the repository root exists to make `aeon/` a workspace
folder so you get both.

**`aeon/` must never contain upstream code.** Not a copy, not a vendor directory, not a `replace`
directive. `make guard` enforces all three, because the eventual extraction to its own repository is a
`git subtree split` (P24) and a copy turns that into a provenance argument.

**Nothing under `aeon/` may import from `../packages/`.** Same reason.

## 3. What the framework brings

The importable module is **`github.com/cjlapao/go-backend-scaffolding/framework`**, pinned at
**`v0.10.1`** (tag `framework/v0.10.1` — *not* the repo root, which is a different module).

### Free, from `system.Initialize`

`app`, `config`, `logging`. You do not register these — `system.Initialize` does, and `ctx.Log()` works
from that point. **`config` registers only if its validation passes**, which matters more than it sounds:
see the signing key below.

### Registered by name, if Aeon asks

**v0.10.1 added nine** to v0.10.0's twenty-three. The full set:

`analytics` · **`api`** · `cache` · `consent` · **`database`** · `device` · `email` · `encryption` ·
`events` · `feedback` · `health` · `honeytrap` · `i18n` · `idempotency` · `jobs` · **`localrpc`** ·
`media` · `metrics` · `notification` · `object_storage` · `osutils` · `otp` · `profiling` · `ratelimit` ·
`redis` · `session` · `sms` · `swagger` · `tracing` · `vaults` · `websocket` · plus `core/flipt`,
`core/validation`

**Aeon registers two: `localrpc` and `api`.** `internal/bootstrap/register.go` says what each of the
others is waiting for and why the rest are refused.

### Registered is not running — the pins

`system.WithForcedServices` takes **code-level enablement decisions that outrank configuration
everywhere**. `bootstrap.Pins()` holds Aeon's, and they are the answer to a question registration alone
cannot settle:

| Pin | Why |
| --- | --- |
| `ForceOn("localrpc")` | The framework defaults it **off** — right for a general-purpose framework, wrong here. A daemon with no endpoint starts, reports healthy, and can never be spoken to. The pin deletes that state instead of documenting it |
| `ForceOff("api")` | The framework defaults it **on, at `:8080`**. Registering it alone would open a port nobody asked for. `ForceOff` is *stronger* than S19's N15 asked for: configuration cannot turn it on at all, and the orchestrator will not auto-enable it to satisfy another service's dependency |

**S19 owns removing the `api` pin**, together with the explicit-address and API-key requirements that make
binding deliberate. Four tests in `register_test.go` assert both pins; if `TestAPIIsPinnedOff` fails, the
daemon is listening.

### The cursor signing key — v0.10.1 requires it, and nothing starts without it

`cursor.signing_key` is now mandatory. Without it the **config service fails validation and is never
registered**, so every service declaring `config` as a dependency fails behind it with
`service "localrpc" requires "config" which is not registered` — which names config, not the real cause.
**`aeon serve` does not start at all.**

`internal/secrets.EnsureCursorSigningKey()` handles it, and **must run before `system.Initialize`** —
config reads the environment as it initialises, so a key set afterwards is one it never saw. Three cases:
an explicitly set env var is respected (an operator with a vault always wins), a key on disk is loaded,
otherwise one is generated and written `0600` at `<store dir>/cursor.key`.

It is generated rather than prompted for because the key must be **stable across restarts** — it signs
pagination cursors so a client cannot forge one, and rotating it invalidates every cursor already handed
out. An unreadable or empty key file is therefore **reported, never replaced**.

`internal/appenv` has the same ordering constraint for a non-secret reason: without it the endpoint
resolves under the *framework's* namespace (`/run/user/<uid>/go-backend-scaffolding/rpc.sock`), which
names the wrong product and lets two apps on one host collide.

### `localrpc` — the one that matters

The daemon's reason to exist, and it arrived from a request this project filed. A unix socket (POSIX) or
named pipe (Windows) dispatching **JSON-RPC 2.0**, behind one API that does not branch for callers:

```go
localrpc.Register(method string, h Handler) error   // the method table — S2 fills this
localrpc.Notify(connID, method string, params any)  // server → client: the event stream
localrpc.Connections() []ConnInfo                   // peer credentials, per connection
localrpc.EndpointPath() string                      // where it bound
localrpc.Shutdown(ctx) error
```

Two behaviours to know before building on it:

- **It binds and accepts in `Init`, not `Serve`.** The endpoint is reachable while services registered
  after it are still initialising — exactly the window a daemon most needs to be inspectable. Unknown
  methods answer `CodeServerNotReady` until Serve, then `CodeMethodNotFound`.
- **It chmods the endpoint's parent directory to `0700`.** Verified: socket `srw-------`, directory
  `drwx------`. That is P26 — filesystem permissions plus peer credentials, no claim, role or tenant in
  this path. **Consequence: the endpoint cannot live in a directory you do not own.** `/tmp/x.sock` fails
  with `chmod /tmp: operation not permitted`; `/tmp/yourdir/x.sock` works.

### `api` — a real HTTP server, as of v0.10.1

**This reverses an earlier finding in this file.** Before v0.10.1 the framework had no registerable HTTP
server — `framework/core/api` was response/error/code helpers only and the real server was the
scaffolding's `internal/api`, unreachable across the module boundary. So S19 was going to have to build
one. **It no longer does.**

```go
api.New(svc).
    WithTokenValidator(tv).        // S19's one-key-one-user check goes here
    WithActivityRecorder(ar).
    WithGlobalPreMiddlewares(mws)
```

It declares only `config` and `logging` as dependencies — **deliberately not `database`** — so
registering it drags no store into the boot. Config section `server.api`; defaults
`Enabled:true, Port:8080, BindAddress:localhost, Prefix:/api`.

`api_keys`, `role` and `claim` are **still** scaffolding-internal. S19 needs one key for one user, so it
supplies its own validator through `WithTokenValidator` rather than waiting for services built for
multi-tenant claims.

### The store uses the framework's contracts — and expands them

**Direct SQLite access and the framework's contract are not a choice.** Both, and the seam is
`(*gorm.DB).DB()`, which hands you the raw `*sql.DB`.

So the framework owns **opening, configuring and pooling**; Aeon owns **the schema, the migrations and
everything above the connection**. What the framework already publishes, do not rebuild:

| Concern | Where it lives |
| --- | --- |
| Where the file is | `SQLiteConfig{StoragePath, FileName}` — `internal/appenv` feeds Aeon's XDG default in |
| Pragmas | `SQLiteConfig.DSN`, a documented raw-DSN escape hatch, opened as-is |
| Pooling | `PoolConfig{MaxIdleConns, MaxOpenConns, ConnMaxLifetime}` |
| The connection | the `database` service; `Store.Init(ctx, *gorm.DB)` receives it |
| Aeon's tables | implement `Store`: `Name · Init · Health · IsEnabled · Dependencies · Migrate` |

**One DDL source, forward-only, and a count the tests hold.** `internal/store/schema.go`'s `Migrations()` is
the only place the schema is written; `migrate.go` walks a database forward through that same list, and a
shipped entry is **immutable** — SQLite cannot tighten a CHECK in place, so new shape means a **new number**,
never an edit. Editing a shipped `CREATE TABLE` fixes every fresh database and leaves every existing one on the
old shape with a ledger row swearing it is current, which is undetectable from inside the process. **The count
is asserted in `internal/store`'s own tests, so read the number there and not here** — S10 added two
(`0016_policy_governance`, which rebuilt the provably-empty `policy` table to the closed vocabulary, and
`0018_policy_authorship_net`, the PL1 authorship CHECK plus the per-target acceptance index), and **S11 added
`0019_policy_action_ask_and_loop_attended`** (the `policy` action CHECK widened to admit `ask`, and
`loop.attended` declared at pin time), taking it to **19** at S11's close. Add one the way S10 and S11 did: a
new numbered entry, its CHECK vocabulary mirrored in `internal/policy/types.go`, and the lock test that
compares the two. **Rebuilding a table that another table references is where this gets subtle:** `0019`'s
`DROP TABLE policy` must **not** recreate `idx_policy_acceptance_target`, because `policy_acceptance` is parked
rather than dropped and its index rides out — re-creating it fails with `index … already exists` (measured in
S11 T7, and the reason `0019`'s index list is shorter than `0018`'s).

**Amended 2026-10-30, for parallel worktrees: the numbered literal above is frozen at 43, and a migration
from here on is a file, not a new entry in that slice.** With several slices now routinely built in separate
worktrees, two agents picking "the next number" at the same moment is a real collision, not a hypothetical
one — it happened once already (S26 and a concurrent session both reached for 43). `Migrations()` still
returns the full ordered list the rest of this section describes, but it now does so by appending
`internal/store/migrations/*.sql` (parsed by `migrations_dir.go`, embedded via `go:embed`) to the frozen 1–43
block in `schema.go`. A new migration is a file named `<14-digit-YYYYMMDDHHMMSS>_<slug>.sql` — the timestamp
IS the migration's `ID` (the real ordering key `migrate.go`'s runner sorts and applies by), so two worktrees
each adding a migration add two different files, which Git merges with no conflict, instead of two edits to
the same slice literal. A leading `-- ` comment line in the file becomes its `Description`; everything else
is forward-only, immutable DDL exactly as above. See `internal/store/migrations/README.md` for the full
convention — read it before adding the next one. The same collision shape exists for `routes.ts`'s bench-page
array and `TestBench.tsx`'s dispatch switch; see this file's §5 "Adding a bench page" for that one.

**sqlite-vec works through this path — proven, not assumed.** `gorm.io/driver/sqlite` imports
`mattn/go-sqlite3`, the same CGO driver, so the extension registers on the framework's own connections:

```
sqlite_vec.Auto()        ← MUST BE CALLED; a blank import does nothing
  vec_version() through GORM:   v0.1.6
  vec0 virtual table:           created
```

**Two traps measured here, both silent:**

- **`sqlite_vec.Auto()` must be called**, before the database service initialises — the same ordering
  constraint `appenv` and `secrets` have.
- **The DSN pragma syntax is driver-specific.** `?_journal_mode=WAL&_foreign_keys=1&_busy_timeout=5000`
  works. `?_pragma=journal_mode(WAL)` is another driver's form and **silently does nothing** — journal
  stays `delete`, foreign keys stay off. **Assert pragmas by querying them, never by inspecting the DSN.**

**Still open, for S4 to settle with a measurement:** `PoolConfig` is one pool and DB15 wanted two (a
single-connection writer plus readers). Accept one and measure, or make the case upstream — do not
reimplement pooling to preserve the old design.

## 4. The rules that are not negotiable

| | |
| --- | --- |
| **Registration is an allow-list** (B8) | Name what you want. Never subtract from the framework's defaults. A deny-list fails open on every upstream addition; an allow-list makes a new framework service inert by default |
| **Adding a service is two files** (B11) | `register.go` *and* `services.golden`, in the same commit. The test compares them, so widening the daemon is always a reviewable diff and never a side effect of `go get -u` |
| **Anything the framework reads at init runs before `system.Initialize`** | `appenv` and `secrets` both exist for this. A value set afterwards is one config never saw, and the failure names `config` rather than the ordering |
| **A pin is a security decision** | `ForceOff("api")` is what keeps the daemon from listening. Changing a pin changes what the process exposes, so it belongs in a reviewed diff with its test |
| **CGO is on** (B3) | `sqlite-vec` is a C extension and the store needs it. Do not "temporarily" switch to a pure-Go driver — that converts a known build problem into a driver swap eighteen slices deep |
| **One binary** (B4) | `aeon serve` is the daemon; every other verb is a client. Not two executables |
| **The daemon surface is the primary interface** (P34) | Anything built reaches its user **first** as a method on the daemon's own surfaces — the socket protocol, and the narrow HTTP / MCP-stdio projection where it belongs there (P25). The CLI is a secondary client of that surface and never the sole door: **a capability that exists only as a CLI verb is not accepted as finished.** The exception is deliberately narrow, and S45 re-drew its line: **PL15 governs policy authoring as a HUMAN DOOR, not as a CLI monopoly** — authoring and the proposal commit ride the socket (the surface a person holds: `0600`, peer credentials, P26) and are absent from every surface a non-person reaches; what stays human-only is the *caller*, not the transport. See [ARCHITECTURE.md P34](docs/aeon/ARCHITECTURE.md) and the PL15 row below |
| **The Makefile is the entry point** (B6) | A capability reachable only by a raw `go build -ldflags …` is one most people will not use |
| **Never edit the scaffolding** | It is a dependency. Read it; raise what it needs as a request |
| **Never accept a credential as a flag value** | The environment, or a path to a file. A value in `argv` is in the process list and in shell history, and that failure is silent |
| **The store is the source of truth; files are a disposable mirror** | Anything rendered is regenerable from rows |
| **Finishing a task means updating the record** | See §4a. Nothing tracks this automatically — the whole point of Aeon is that one day it will, and until then the record is only as good as the last person who finished something |
| **A slice extends the cumulative demo** | See §4b — `docs/aeon/DEMO.md`, appended per slice |
| **A credential goes in and never comes out** | See §4c. Enforced by having two types rather than one redaction |
| **The policy vocabulary is closed** (PL3) | Seven types, five targets, four sources, **three actions** (`Allow · Deny · Ask` — `Ask` arrived with S11's migration `0019`), every one named in `internal/policy/types.go` and mirrored in migration `0016`'s CHECKs, widened by `0019`'s. A value nobody compiled is a permission nobody granted, so widening the set is a schema + validator + evaluator change and never a string into a flag. See §4d |
| **Policy authoring is a human door** (PL15, amended by S45) | **Amended 2026-10-30 by the operator's ruling (S45 PM1); the old row read "Policy authoring is CLI-only".** Authoring (`policy.add` · `policy.remove` · `policy.seedCorpus`) and proposal decision (`proposal.commit` · `proposal.reject`) are **socket methods on the human surface** — the `0600` endpoint with peer credentials (P26), no claim or role in the path — registered iff their service is wired (nil seam ⇒ absent, `ErrUnknownMethod`: V13), each `RequiresInitialize`, none `Debug`. The CLI (`aeon policy`, `aeon proposal`) stays a **caller of the one implementation** in `internal/desk` (PM4), never a second one. What PL15 always meant still holds, now provably: the five are **absent from the narrow HTTP/MCP-stdio projection, from the agent-services enumeration, from the tool registry and from the records catalog** — the four caller-classes that are not the person; every such write is **audited** (`policy_change`, PM8) and stamped with the peer; and a proposal **never constrains anything until a human commits it** (S35 TD2), the enforcement gate still never learning the word `proposal` (PM11). Guards: `TestPolicyAuthoringIsAHumanDoor`, `TestAuthoringNeverReachesTheNarrowOrAgentSurfaces`, `TestAuthoringGuardsHaveTeeth`, `TestOnlyTheDeskAndTheDaemonSeamsCallTheWriteBoundary`, `TestOriginalGuardsAreByteIdentical`, `TestAmendmentDoesNotLoosenOtherHumanDoors` (all `cmd/aeon/policy_amend_test.go`), beside `TestProposalCommitExistsOnlyAsAHumanDoor` and T4's amended siblings. See §4d |
| **An `ask` is not a permission** (S11) | It is a command routed to a human. The engine folds the three actions **deny over ask over allow** (PL4), so an `ask` outranks a broad `allow` and the sentence names the allow it looked past. On an **attended** loop the step suspends with a question naming agent, exact command and deciding rule; on an **unattended or never-declared** loop it denies **at once, with no question row**. Answering `approve` delivers the answer to the **model** — it does not mint an allow; what spawns a command is an `allow` and the `AllowToken` it mints. See the walk: [`docs/aeon/DEMO.md`](docs/aeon/DEMO.md) §S11 |
| **A spawn needs a token, and the order is structural** (S11) | `internal/policy/token.go`'s `AllowToken` has unexported fields and **one** mint — `Verdict.Token(command)` — and `internal/tools/spawn.go`'s first statement checks it, before the directory bound and before `exec`. So the order is **verdict → receipt → spawn**, a token is bound to one exact command string, and a denied command leaves no receipt row (recovery would otherwise report an unknown outcome for something that provably never started). The forgeries are compiler-refused, not convention-refused: `internal/policy/token_forge_test` |
| **Registered is not granted** (S11) | `tools.Registry()` is ten tools (`bash, edit, glob, grep, list, question, read, todo, webfetch, write`, pinned by `TestRegistryExactNames`); `tools.DefaultGrant()` is `read, list, write`. `bash`, `webfetch` and `todo` are registered and handed to nobody, and the hidden `write_file` alias is in neither list — a compatibility key for pinned bytes, filtered from every listing. **A handler's `Policy` field is a declaration and never a grant:** `handler.Build` refuses an entry whose policy names a registered tool |
| **Settings are declared, never ad hoc** (S44) | A daemon-wide, UI-writable value is a `Setting` declared in `internal/config/settings_decl_<area>.go`, read only through `config.Reader` (env → database → default) and written only through `settings.set`. There is no second mechanism and no table touched outside `internal/config` and the daemon adapter (T6 `TestOnlyConfigAndTheDaemonAdapterTouchTheSettingTables`, `TestNoSQLLivesOutsideTheStore`); the `setting` / `setting_event` tables are append-only by trigger (T1 `TestSettingEventIsAppendOnly`); the `settings.*` writes are the human door and never reach the narrow surface (T6 `TestNoSettingsWriteReachesTheNarrowSurface`); and a credential-shaped key is refused at the declaration boundary (T2 `TestMustDeclareRefusesCredentialShapedKey`). S44 ships zero production settings; T10 adopted the first (`agents.allowUnsignedImport`) from S43. |
| **Loop control is a human door** (S55 LC5) | Stop/resume/cancel/raise and budget/watchdog writes are socket methods on the 0600 + peer-credential surface, absent from the narrow surface, agent services, the tool registry and the records catalog (the T5/T8/T9 P4 guard amendments pin the set and the canaries); an agent may only record a stop *request* (`loop_request_stop` — inert until a person acts). The one-writer rule (LC4) is the engine's `endStatus`; a cancel never runs the `on_abort` lane (OD1: certain and free); a cap hit parks `blocked/cap-hit` and `loop.raise` is the one door off it — unattended loops have no question row (G14). The six S55 tables (`loop_control_event`, `loop_control_request`, `budget`, `budget_event`, `watchdog_rule`, `watchdog_event`) follow the store discipline of §3: forward-only migrations (the three S55 files), append-only event tables by trigger, the store the only writer, CAS on the loop row. |
| **Autonomy is granted by a person on the socket (S56)** | A grant is per **root component**, per action (or the one `outward` class); `forge.merge` is its own grant with a forced merge-rule condition and a mandatory expiry. A loop started under a grant is **pinned** with the grants that applied (the ceiling) and re-checked live (the floor); `allow_outward` stays 0 for policy-pinned loops — the pin never moves the legacy flags. The ten `autonomy.*` methods are registered only over a wired seam (T6 `TestAutonomyWritesAreAHumanDoor`) and are absent from the narrow surface, the agent services, the tool registry and the records catalog — except the two restrictive propose twins (T7 `TestAutonomyNeverReachesTheNarrowOrAgentSurfaces`, `TestNoStandingGrantInTheNarrowSurface`). No agent can author an allow: no `autonomy_grant` proposal kind, no tool twinned to a grant, the socket refuses a non-`user` actor, and the store refuses before SQL (T7, the three AL11 locks). The grant tables have exactly their contracted callers (T7 `TestOnlyTheServiceAndTheStartSitesTouchTheGrantTables`); the pre-slice guards stay byte-identical (T7 `TestOriginalGuardBodiesAreByteIdentical` — S45's T6 test holds the spec's `TestOriginalGuardsAreByteIdentical` name in the same package). |
| **MCP configuration is a record, authored through a human door (S46; amends S35 TD4)** | The configuration lives in the database (`mcp_server`, `mcp_server_tool`, `mcp_agent`, `mcp_declare`, the append-only `mcp_change` audit and the import ledger); the legacy `mcp.json` is adopted once — all-or-nothing, zero fabrication, hash-idempotent — and afterwards **no door modifies it** (drift is reported by hash, never silently re-adopted). The one validator `Config.Validate` (in the store-free `internal/mcp`) gates every write verbatim: candidate first, rows never reached on a refusal. The eight writes (`mcp.server.add` · `mcp.server.update` · `mcp.server.remove` · `mcp.server.setEnabled` · `mcp.grant.declare` · `mcp.grant.revoke` · `mcp.core.set` · `mcp.importFile`) and the seven management reads (`mcp.servers` · `mcp.server` · `mcp.status` · `mcp.check` · `mcp.vocabulary` · `mcp.grants` · `mcp.changes`) are **socket methods on the human door** — registered iff the seam is wired (V13), `RequiresInitialize`, never `Debug`, the actor from the peer and never a param (uid 0 refused) — and **absent** from the narrow projection, the agent-services enumeration, the tool registry and the records catalog. The proposal path remains (S46 T6: `mcp_server_add`/`mcp_server_remove` plus the seventh kind `mcp_grant_revoke`, which can only narrow — MG9); agents get own-view reads and narrowing-only proposals (MC2: a peer's private is invisible, even in error text; MC5: declaring grants nothing). `internal/mcp` is importable only by `cmd/aeon` (MC11, a NAMED importer list). Guards: `TestMcpWritesAreAHumanDoor`, `TestMcpNegativeDoors` (six subtests, each canaried by `TestMcpGuardsHaveTeeth`), `TestMcpLayerStillDeletable`, `TestAgentReadToolsNeverCallTheUnscopedReads`, `TestMcpRowsAreWrittenOnlyThroughTheAdminSeam`, `TestOriginalMcpGuardsAreByteIdentical` (all `cmd/aeon/mcp_amend_test.go`), beside the UNEDITED `internal/mcp/absent_test.go` (S12) and S45's `TestAmendmentDoesNotLoosenOtherHumanDoors` — pinned by hash, not by re-statement. |
| **The tool catalog is read-only and the surface is classified (S47)** | Everything the app exposes answers on **nine reads and nothing else** — `tools.list` · `tools.get` · `tools.byAgent` · `tools.status` · `tools.vocabulary` for the person, `tools.mine.list` · `tools.mine.show` for the agent itself (the subject is the step's, never a param — OL3), and `surface.list` · `surface.get` for the daemon's own methods. **No write method, no tool and no CLI verb exists** (TC1, `TestNoWriteVerbExists`); the only thing that ever writes a catalog row is the **inert `tools.core` seed set** through S43-extra's engine, and drift is **reported and never repaired** (TC11) — a catalog that could fix a row would eventually fix one nobody agreed to. The row is a display copy: the runtime's model-facing description and schema are `CanonicalSchema(Spec)` **in code**, never read from the database (TC2, `TestExecutorAndSchemaAreNeverReadFromTheDatabase`, proven against a tampered row that the read still refuses to call current). Every registry tool declares capabilities from a **closed vocabulary** and a name without an entry fails a test (TC3, `TestEveryRegistryNameHasMeta`/`TestEveryToolDeclaresCapabilities`); every registered method carries exactly one classification entry and every door flag is **computed from the live structures and compared against them** (TC6, `TestSurfaceEveryRegisteredMethodIsClassified`/`TestSurfaceDoorsEqualTheRealStructures`/`TestEveryMethodIsClassified`). "To whom" is one pure function over `tools.GrantedSet` — **extracted from `BuildToolset`**, so agreement is structural rather than hoped for — and it is differential-tested against `policy.Evaluate` and the builder, never against `toolcatalog` itself (TC5, `TestEffectiveEqualsTheGateForEveryPair`); a verdict sentence is quoted byte for byte from the engine, never paraphrased. An agent's own view is MC2-clean in success **and** in error text (TC7, `TestPeerPrivateMcpToolNeverAppears`/`TestToolShowOfUnheldEqualsNonexistentSentence`/`TestAgentToolsAreOwnViewOnly`), the catalog holds no cache across calls (N8, `TestToolsReadsAreLive`), and the whole set is absent unless its seam is wired (V13). The eight guards of `cmd/aeon/tools_guard_test.go` + `tools_guard_predicates_test.go` carry 14 canaries, each asserting the offender by name (`TestToolsGuardsHaveTeeth`). Guards beside them: `TestToolsCatalogNeverReachesNarrowOrAgentSurfaces`, `TestRegistryCountIsExactlyAccountedFor`, `TestSocketAndDirectDoorsProduceTheSameOutput`, `TestJSONVerbPrintsTheDoorsBytesAndNothingElse`, `TestSurfaceOfflineRefusesRatherThanList` (TC12's daemon-first), and in the app `ui/src/features/settings/tools/toolsRpc.guard.test.ts` — the panels ask registered methods only, send no subject the daemon must not choose, and offer **zero controls** (TC13's read-only panels). |
| **Gate authoring is a human door (S57 VG17)** | The gate plane's nine writes (`verify.add · verify.update · verify.archive · verify.restore · verify.applyTemplate · verify.try · mergerule.set · mergerule.clear · dod.mark`) and its thirteen reads are **socket methods on the human door** — registered iff the three gate seams (`Table.Verify · Table.MergeRule · Table.DoD`) are wired (nil seam ⇒ all twenty-two doors ABSENT, not refusing: V13), `RequiresInitialize`, never `Debug`, the actor from the peer and never a param (the reflection guard `TestNoMethodAcceptsAnActorParam` holds the whole plane's wire params and service inputs free of any author-shaped field). `verify.try` runs its one gate under the policy gate (`CommandVerdictFor` — the empty set denies, an explicit deny beats the allow) with the scrubbed gate environment (the closed allowlist, `CI` constant), reads the root's active loops BEFORE the run so the answer names the race (OD8), and records the run `trigger='manual'` stamped with the peer. Agents may only ASK, through the two restrictive proposal kinds (`verify_command` op-add-only; `merge_rule` `Tightens`-only at create, current = baseline when no row) — a proposal applies nothing until a person commits it (S35 TD3, T8 `TestProposeKindsAreInertUntilCommitted`). The nine writes are absent from the narrow projection, the agent-services enumeration, the tool registry and the records catalog (T9 adds the nine agent catalog specs, twinned only to the three safe no-ops). Guards: `TestGateAuthoringIsAHumanDoor`, `TestGateAuthoringNeverReachesTheNarrowOrAgentSurfaces`, `TestOnlyTheServicesCallTheGateWriteBoundary`, `TestGateGuardsHaveTeeth`, `TestProposalKindsAreRestrictiveOnly`, `TestProposeKindsAreInertUntilCommitted` (all `cmd/aeon/verify_amend_test.go` + `verify_amend_propose_test.go` — new siblings beside the UNEDITED pre-slice guards), beside `daemon/gate_service_test.go`'s seven service tests (the try's policy-and-env honesty, the typed refusals with their alternatives, the peer stamps, the V13 absence). |
| **The store is backed up, and restore is a CLI direct door (S62)** | The online backup is a single file published atomically into the backup dir — a killed backup leaves fenced partials only, never a final-name file, and the start sweep removes them. The manifest is immutable, and its `verified` token is the file's own claim, not the fact: the fact lives in the append-only `backup_run` ledger, so daemon-down `doctor` shows the manifest token while `backup list` and the daemon-joined doctor show the ledger (the header comment in `internal/doctor/backup.go` says so). A non-empty store with pending migrations **cannot migrate without a verified pre-migration copy or an explicit `--no-backup`** — the gate sits in `store.Open`, so the daemon and every CLI direct door take it alike, and the hook is parser-pinned on `NewEnvSettings` (env and default only, never a row); a fresh store with nothing applied takes no backup at all. **Restore is a CLI direct-door command** (`aeon backup restore FILE [--yes]`, daemon down, the exclusive file lock, staging inside the state dir, a drill on a scratch copy, the replaced set kept in a `state.db.pre-restore-*` dir) — deliberately **not a socket method, not a tool, not a catalog spec** (the canary `backup.restore` is refused by name). The five doors `backup.status · backup.list · backup.run · backup.verify · backup.prune` are human-door methods absent from the narrow projection, the agent-services enumeration, the tool registry and the records catalog, and exactly two S62 read-only agent tools exist (`backup_status`, `export_sets`). The ten `backup.*` settings are S44-declared (env → row → default, none Sensitive). `aeon export` is deterministic, never selects a credential-shaped column, and reports an unbuilt record set as *unavailable*, never empty — and a backup **never contains credentials and never leaves the machine**. The scheduled backup is a daemon maintenance task, not an automation: the ticker wakes at start then hourly, and the interval is the due threshold, not the cadence — a week of downtime is one catch-up, not seven. Guards: `TestBackupMethodsAreAHumanDoor`, `TestBackupNeverReachesTheNarrowOrAgentSurfaces`, `TestNoRestoreMethodOrToolExists`, `TestBackupGuardsHaveTeeth`, `TestTickerIsNotATrigger` (`cmd/aeon/backup_guard_test.go`) |
| **Environment and secret-reference writes are a human door (S59)** | The execution-environment plane — the thirteen `environment.*` doors (`list · show · effective · vocabulary · changes · provisions · create · update · archive · restore · bind · unbind · teardown`) and the seven `secretref.*` doors (`list · show · check · create · update · archive · restore`) — is **socket methods on the human door**: registered iff their seams are wired (nil seam ⇒ all twenty ABSENT, not refusing: V13), every one `RequiresInitialize`, none `Debug`, the actor taken from the connection's peer (`Table.environmentActor` refuses a nil `ActorFor`, uid 0 and a blank id BEFORE any service runs) and never from params. **Params are refused by key, before semantic validation**: the identity vocabulary (`actor · actorKind · origin · legacy · source · sourceKind · revision · createdAt · updatedAt · createdBy · updatedBy`) is refused on the writes and — minus the filters a read legitimately takes — on the reads; and the credential vocabulary (`value · secret · secretValue · token · apiKey · key · password · credential · contents · data · raw · bytes · envValue`) is refused on **all seven** secret-reference doors, reads included (§4c: a credential goes IN and never comes out). A reference is a **pointer** (`sourceKind` + `source` + `injectAs`); `secretref.check` answers two booleans and the refusal, reading the value only to decide resolvability and dropping it. `environment.effective` answers **names and sources, never values**, and `environment.bind {dryRun}` answers in the same words the applied write will use — one formatter, `execenv.DescribeBind`, four callers. Every changing write is audited (`environment_change`, keyed by the row's own id, secret references included) and refuses rather than answering empty; `environment.teardown` walks the ledger (`provisioning → ready → tearing_down → removed/failed`) and refuses to mark an unknown provision removed. `execenv.provisionIdleTTL` is a **declared S44 setting** (`internal/config/settings_decl_execenv.go`, read only through `config.Reader`, its **source word** carried in the vocabulary answer — **two states as shipped**,
`setting` and `unreadable-default`: `config.Reader`'s four methods cannot tell a declared default from a row
holding the same value, so `declared-default` is declared in `internal/daemon/execenv_settings.go:41` and
**never assigned** (S59 T12's walk measured `provisionIdleTtlFrom:"setting"` with `24h0m0s` and no row at
all). The third state needs the reader to report its layer — S44's `Get`/`View` does, `Reader` does not — and
claiming three when the code answers two is the shape of claim this row exists to prevent). The twenty doors are **absent** from the narrow HTTP/MCP-stdio projection and the agent-services enumeration; in the tool registry and the records catalog they are absent **except the five S59 T7 grants** (`environment_list · environment_show · environment_effective · secretref_list`, all class `safe`, plus the one ask `environment_propose_restrict`, class `detectable`, `Mutates:true`, twinned to the plane's only new method `environment.proposeRestrict` — answered by the SAME services the person's doors answer through, its reference list a TYPE with no `source` and no value field at all (EV7), and its ask able only to TIGHTEN: `execenv.Restricts` must come back EMPTY at create and again at commit, every loosening named, nothing enforced until a person commits it as `environment_restrict` with the COMMITTER as actor and the proposal's id in the audit line (EV16, TD3, TD4). None of the five is in `DefaultGrant()` — registered is not granted (S11) — and none of the thirteen environment writes or four secretref writes has a tool, a spec, or a spec twinned to it. **No process inherits the daemon's environment**: every spawn site is in a named registry with its class, `envtest` is imported only by tests, and the seed never lays `host-inherit`. Guards: `TestEnvironmentWritesAreAHumanDoor`, `TestEnvironmentPlaneNeverReachesNarrowOrAgentSurfaces` (+ its `TheirSources` walk, per-directory floors and a measured 46-file total), `TestEnvironmentGuardsHaveTeeth` (canaries per predicate, plus the two planes wired out separately) — all `cmd/aeon/environment_guard_test.go`; `TestNoSpawnInheritsTheDaemonEnvironment`, `TestSpawnSiteGuardHasTeeth`, `TestOnlyTestsImportEnvtest`, `TestNoEnvironmentParamCarriesAValue`, `TestTheSeedNeverLaysTheLegacyBackend` — `cmd/aeon/spawn_guard_test.go`; the door shape in `internal/protocol/environment_test.go` (six tests) with `TestEnvironmentConformanceFixturesMatchTheWire` over 25 fixtures in `conformance/environment` + `conformance/secretref` (17 + 8, the pair S59 T7 added being the ask's door refusal and its INERT success row); T7's own guards `TestEnvironmentAgentSurfaceIsExactlyTheFive`, `TestEnvironmentProposeTwinIsClassifiedNotAHumanDoor`, `TestSecretRefListCarriesNoSource`, `TestEnvironmentProposeGuardHasTeeth` (`cmd/aeon/environment_propose_guard_test.go`, which also owns the three predicates T6's two predicates consult and the closed one-file literal exemption), the catalog half in `internal/records/catalog_environment_test.go` (four), the arms in `internal/daemon/records_environment_test.go` (nine) and the kind's two-door behaviour in `cmd/aeon/environment_proposal_test.go` (eight); the service in `internal/daemon/environment_service_test.go` (twelve) and `environment_teardown_test.go` (seven); the setting in `execenv_settings_test.go` (four). See `docs/aeon/S59-execution-environment/` |
| **Notification channels are human-door authority (S58 SV7)** | The attention plane's eleven writes — `inbox.ack · inbox.snooze · inbox.unack · notify.class.update · notify.channel.create · notify.channel.update · notify.channel.enable · notify.channel.disable · notify.channel.archive · notify.channel.test · notify.subscription.set` — are **socket methods on the 0600 + peer-credential surface**, registered iff their seams are wired (nil seam ⇒ absent, `ErrUnknownMethod`: V13), every one `RequiresInitialize`, none `Debug`; **absent from the narrow surface, the agent services, the tool registry and the records catalog** — the eleven `TestSupervisionWritesNeverReachOtherDoors` negatives include the source-literal walk and a step with *every* tool granted (`tools.GrantedSet`, S47's projection, cannot name one). Channels are **created disabled**, the HMAC secret is **by reference** (`secrets.SaveChannelSecret`, `<state>/aeon/credentials/notify-<id>.key`, 0600 — never a column, never an answer field), the outbound **envelope carries ids and enums only**, and the seed **never lays a channel**. The eight reads (`inbox.list · inbox.summary · mission.status · notify.vocabulary · notify.class.list · notify.channel.list · notify.channel.show · notify.delivery.list`) are human-door too, with five read twins in the records catalog's `supervision` family — every one `Mutates:false` (SV16). Guards: `TestSupervisionWritesAreAHumanDoor`, `TestSupervisionWritesNeverReachOtherDoors` (six subtests), `TestSupervisionGuardsHaveTeeth`, `TestSupervisionDoesNotLoosenOtherDoors`, `TestOriginalNarrowRecordsGuardsAreByteIdentical` (S45's mechanism, sibling name — its pin owns the spec name in this package), `TestSupervisionMethodsAreClassifiedNotNarrow` — all `cmd/aeon/supervision_guard_test.go`. **N1 holds with the plane added (SV14):** an enabled channel's configured host joins the allowed list and nothing else connects — `TestZeroChannelsMeansZeroNotificationConnections` (notify), `TestZeroChannelsMakeNoConnectionAtTheTransport` and `TestOneChannelMeansExactlyThatHost` (nettest, permanent; `TestNotificationNetworkInvariantsExist` fails their deletion). See `docs/SUPERVISION.md` and `docs/aeon/S58-supervision/` |
| **Automation is one human-managed view, and triggers are never seeded (S49; closes S40 AU1’s schedule half)** | Schedules, webhooks and event triggers become **one Automation view** — a unified fire **view** (`automation_fire_view`: the count equals both engine fire tables, proven in sqlite), plain-words descriptions from the ONE daemon formatter (`automationview.Describe`, every surface verbatim — AN11), and per-row **health that is advice only and never gates a fire** (“advice only — the fire log owns every outcome (AN8)”, V13 unverifiable ≠ invalid). **All eleven writes of both families are the human door**: peer-stamped (actor from the socket, never a param), audited exactly once into the **append-only `automation_change`** (`UPDATE`/`DELETE` die with `automation_change is append-only (S49 AN4)` — walk-proven at the sqlite prompt), and **archive ≠ delete** with a required reason — unarchive returns the row **disarmed** (AN6/AN3; every add arrives disarmed: `trigger.add`+`trigger.pause` / create+disable, both writes visible in the audit). The creator gap closes with a column on schedules (old rows keep `’’`, never a guess); `trigger.update` fills the S20 gap validating exactly what `trigger.add` did and **never enabling**; the enabling verbs (enable/unarchive/fire-now) print the daemon’s **authority statement** first and act only on the explicit `--yes`/click. `trigger.add`/`trigger.remove` stay internal-only — a T5 guard set (`cmd/aeon/automation_guard_test.go`, nine guards, canaries in `TestAutomationGuardsHaveTeeth`) proves the eleven names reach **neither** the narrow projection, the agent services, the tool registry, nor the records catalog, and the UI’s own guard (`ui/src/features/settings/automation/automationRpc.guard.test.ts`) pins the panels to the daemon’s **24 doors** and nothing else (census == allow-list, zero storage/fetch/invoke — AN10/P34). Agents get four read twins over the SAME rendering plus the one restrictive kind `automation_disable` (narrowing-only, committed by a person — S35 TD3; inert-until-commit proven T6). **Rule 6’s named exception stands: the seed never lays a trigger row** (S40 AU1 lock 3, UNEDITED). `aeon automation` (T8: 21 verbs) is a pure socket client — the first human CLI for event triggers, `aeon trigger` untouched. Guards beside the T5 set: `TestEveryAutomationWriteIsAPersonAtTheEndpoint`, `TestAttendednessAndOutwardRulesUnchangedByTheAutomationPlane` (re-pinned, not edited), the T3 health-never-gates proof, and the T2 differential (previews == the real clock across DST windows). See `docs/aeon/S49-automation-management/` |
| **Roadmap writes are a human door; dispatch is a person's act (S50)** | The plane answers **fifteen doors**: eight writes (`roadmap.add · estimate · priority · update · cancel` + the dispatches `ideate · plan · materialize`) on the person's socket, six reads (`list · show · board · history · vocabulary · health`) with the one deliberate inversion — `roadmap.proposeIdea` is the **agent's** door and over the human socket it REFUSES (`roadmap.add is the person's door; … proposal.create with kind roadmap_idea is the person's door`). RM11's negatives hold by guard, not by hope: none of these names reaches the narrow projection, the agent-services enumeration, the tool registry or the records catalog (`cmd/aeon/roadmap_guard_test.go` — seven guards, `TestRoadmapWritesAreAHumanDoor` … `TestEveryRoadmapMethodIsClassified`, canaries in `TestRoadmapGuardsHaveTeeth`; the **eleven lifecycle edges are pinned by rendered hash** `945ffea7…` — no door adds an edge, the lands belong to the **reactor**). Every write audits **exactly once** into the **append-only `roadmap_change`** (`UPDATE`/`DELETE` die at the trigger; the no-DELETE path and the trigger itself are guard-checked), the actor stamped from the endpoint (`user:uid` socket, `{cli,cli}` direct door, `system:reactor` the one machine edge; an `agent` actor is refused by CHECK **and** door — RM4), and `roadmap.update` is RM1's narrow set — title/epic/paths, the paths **editable only until `running`**, from there they are history — with an honest `changed:false` line and no no-op audit. The two blueprint choices are **S44-declared settings** (`roadmap.ideationBlueprint`, `roadmap.planBlueprint`) read **at each dispatch** through S48 and **refused by name — never a fallback, no blueprint-name constant left in the code** (RM3; the plan host itself is the seeded `roadmap-plan` record, its bytes characterized byte-identical to the deleted string constant); `roadmap.health` answers `ok · missing · invalid · unverifiable` with the analyzer's own counts and is **advice, never a gate** (RM6). The dispatching three print the daemon's **authority statement** + the item's `Describe` first and move only on the explicit `--yes`/click (RM9; the sentence rides `roadmap.vocabulary.dispatchStatement`, so CLI and UI quote ONE string — the UI fills its `<item>` slot from the daemon's own `describe`). Agents keep exactly three tools — two safe reads answering the socket doors **byte-identically** and the `roadmap_idea` filing, **inert until a person commits it** (TD3; the commit lands the item under the COMMITTER with one frozen `add` audit row) — and the UI's guard (`ui/src/features/roadmap/roadmapRpc.guard.test.ts`) pins the panels to their **15 doors** (census == allow-list, zero storage/fetch/invoke, en/pt parity), with `ui/test/policy.test.tsx` naming `features/roadmap/` as the proposal-decision doors' **second named caller** (the ban split per pair; the policy authoring trio untouched). Walked rc=0 2026-11-04: `aeon/scripts/qa-s50-roadmap.sh`, recorded at [DEMO §S50](docs/aeon/DEMO.md). See `docs/aeon/S50-roadmap-management/` |
| **Triage writes are a human door; an agent may only file an unvalidated claim (S51)** | The bug plane answers **fourteen doors**: seven reads (`triage.board · health · history · list · show · suggest-duplicates · vocabulary`) and seven writes, split by WHO — `triage.file` is the **person's** self-file (`filed_by` derived from the peer, there is no `--filed-by` to lie with, TM3) and `triage.fileAs` is the **agent's** twin, which over the human socket REFUSES (`filing a bug claim is a person's own act`), while the five others (`update · set-priority · validate · handoff · confirm-duplicate`) are the person's socket and reach **neither** the narrow projection, the agent-services enumeration, the tool registry nor the records catalog (`cmd/aeon/triage_guard_test.go` — ten guards, `TestTriageWritesAreAHumanDoorExceptFile`, `TestNoAgentReachablePathSetsDuplicateOrPriority`, `TestBugChangeAgentActorOnlyForFile`, canaries in `TestTriageGuardsHaveTeeth`). **TM6's exception is exactly one thing:** an agent may file a claim, and it arrives `unvalidated`/`unknown` under its slug — indistinguishable in shape from a person's filing (`TestSelfFiledBugIndistinguishableFromHumanFiled`) and **never movable by an agent**: the schema CHECK refuses an `agent` actor on any op but `file`, `confirm-duplicate` refuses a machine confirmer **by name** (D5/N3 — `isPersonOriginated`: empty/`agent`/`agent:…`/`model`/`loop`/`tool`), and the two enabling verbs print the daemon's **authority statement** and move only on `--yes`/click (TM9; the statements ride `triage.vocabulary` so CLI and UI quote ONE string, RM9's law kept). Every write audits **exactly once** into the **append-only `bug_change`** in the SAME transaction as the row (`TestBugAuditAndWriteAreOneTransaction`; the one named exception: a `validate` dispatch moves no bug row, so its audit row rides alone), and `update` is the narrow door — title/body only, an honest `changed:false` with no no-op audit. The two state sets stay **two** (`unvalidated` ≠ `could-not-tell`, never one crossed badge — the brief's property test) and a validation attempt is an **ordinary loop** (`triage-validate-<id>`) under the S44-declared `triage.validationBlueprint`, read through S48 and **refused by a missing name — never a fallback** (TM4); the bug moves only when its reactor's terminal lands. `filed_by` pre-slice values resolve **agent names → slugs, all-or-nothing, refusing and NAMING every orphan** (S43's law re-walked, idempotent); the upgraded store's fingerprint equals a fresh one (`TestS51CloseoutUpgradePreservesBugRowsAndAudit`). `triage.list` is the direct door only — the socket carries `triage.board` (P34); agents get exactly the S29 set, unchanged. The UI's guard (`ui/src/features/settings/triage/triageRpc.guard.test.ts`) pins the panels to **11 doors** (9 triage + `settings.get/set`; census == allow-list, zero storage/fetch/invoke, en/pt parity, tone-distinctness for TM5, no authority sentence DEFINED in code). Walked rc=0 2026-11-04: `aeon/scripts/qa-s51-triage.sh`, recorded at [DEMO §S51](docs/aeon/DEMO.md). See `docs/aeon/S51-triage-management/` |
| **Memory is append-only and retractable; agents add observed/assumed and may only propose retraction (S52)** | Retract is a **stamp, never a delete** (LM2): the body, its audit and its history survive and every default read — board, list, show, query, for-agent and the **vec similarity index** — excludes the retracted until a restore; `learnings.retract`/`learnings.restore` are **socket methods on the human door** (LM7/LM8; `--yes`/click on the daemon’s own `RetractLaw` string, RM9 kept — one sentence, CLI and UI quote it), audited into the **append-only `learning_change`** in the same transaction (LM3: UPDATE/DELETE die at the trigger), and the agent ask `learning_retract` is **inert until a person commits it** — the commit stamps the COMMITTER, never the proposer. **LM4** closes provenance: an agent may file only `observed`/`assumed` (`told` needs a person kind, `relayed` the system hook — refused by the law, not by convention); **LM1** keys agent scope on the **slug**, and the name→slug move is all-or-nothing, **refusing the whole OPEN and naming** every unresolvable key (`TestS52CloseoutPlantedUnresolvedScopeKeyRefusesTheOpen`, fresh-vs-upgrade folds byte-equal beside it); **LM7** refuses `agent.delete` while memory is scoped to it, **naming the count** (retracted ones count — they are still its memory, nobody’s to inherit; there is no cascade, LM2). Guards: `TestMemoryRetractionIsAHumanDoor`, `TestAgentActorMayOnlyAddObservedOrAssumed`, `TestRelayedIsStillWriteProtected`, `TestEveryLearningReaderExcludesRetracted`, `TestLearningRowsAreWrittenOnlyThroughTheDoor`, `TestNoLearningDeletePath`, `TestLearningChangeIsAppendOnly`, `TestAgentScopedReadsNeverCrossAgents`, `TestEveryMemoryMethodIsClassified`, `TestS28AndS29MemoryPinsStillHold` (all `cmd/aeon/memory_guard_test.go`), the UI’s `ui/src/features/settings/memory/memoryRpc.guard.test.ts` pins the panel to its **14 doors** (census == allow-list, four provenances distinct in text *and* tone, no authority sentence DEFINED in code, en/pt parity). Walked rc=0 2026-11-04: `aeon/scripts/qa-s52-memory.sh`, recorded at [DEMO §S52](docs/aeon/DEMO.md). See `docs/aeon/S52-memory-management/` |
| **History is a read surface with explicit coverage; export is person-only (S53)** | The history plane answers **five doors** — S25's three reads (`list · show · export`) plus `coverage · vocabulary` — and all of it is **derived and read-only** (HV3: no migration, no new table, no write: the guards scan the package for SQL-words and every way out, pin the facade's method set (`List · Show · Coverage`) and the CLI's verb table CLOSED, and a **read-only drill** fingerprints every ordinary table before and after EVERY history call — including the drill-down on a deliberately ROTTEN archive — and proves nothing moved, `internal/history/read_only_drill_test.go`; `cmd/aeon/history_guard_test.go`). `history.coverage` answers the three states **in three words** — `shown · cannot say · not shown` — and **never hides a row**: undecodable archives are NAMED in cannot-say while the headers keep counting (T1's ruling, walked 2026-11-04 with a planted corrupt blob: `cannot say  1 archive(s) whose content could not be read — <id>` while the rest of the view renders untouched — and differential-tested against the raw tables). Lanes, outcomes and filters are the daemon's **closed sets** (`history.vocabulary`), drawn from the answer by CLI and UI — **nothing recomputed client-side** (HV2; the UI's guard reads the forbidden words OUT of the conformance fixture, `ui/src/features/projects/history/historyRpc.guard.test.ts`); cost is the P32 floor statement VERBATIM and every row carries the ONE formatter's `describe` (HV6). Agents get **exactly two tools** — `history_list · history_show` — answering the socket doors **byte-identically** (`TestHistoryListAnswersTheDoorRenderingByteForByte`, `TestHistoryShowAnswersTheDoorRenderingByteForByte`, the catalog exactly the two: `TestHistoryCatalogSpecsAreExactlyTheTwoReads`), and **`history.export` reaches neither the tool registry, the records catalog, the agent-services enumeration nor the narrow projection** (HV5: `TestHistoryExportIsPersonOnly`, `TestNoHistoryMethodIsOnTheNarrowSurface`). `aeon history coverage|vocabulary` are socket-first clients on the **P5 offline door** — daemon down ⇒ the fixed notice alone on stderr, then the DIRECT door running `protocol.AnswerHistoryCoverage`/`history.Vocabulary` — zero logic copy (source-scan asserted); S25's three verbs keep their exact direct road, goldens byte-identical. **The walk's F-1 stands as the lesson:** `b5620631` ("wip") had deleted BOTH the boot wiring (T3 restored) AND the `case "history":` dispatch — five history doors were dead to the real binary for five days while every test stayed green because the goldens call `cmdHistory` DIRECTLY; dispatch, callee and usage line are now pinned (`TestHistoryVerbIsDispatchedByTheBinary`). Walked rc=0 2026-11-04: `aeon/scripts/qa-s53-history.sh`, recorded at [DEMO §S53](docs/aeon/DEMO.md). See `docs/aeon/S53-history-views/` |
| **Index and graph operations are a person's act, previewed and audited (S54 SP3/SP4)** | The four platform operations — `retrieval.reindex` and the graph trio `graph.build · graph.reparse · graph.delete` — are **socket methods on the human door**: registered iff their seams are wired (nil seam ⇒ ABSENT, V13), `RequiresInitialize`, never `Debug`, the actor taken from the connection's peer and NEVER from params (`platformActor` admits `user`/`cli`, refuses uid 0, blank and the `agent` kind with `ErrPlatformNoActor` −31956); every STARTED op mints **one `platform_change` row in the same transaction** the worker's ledger row lives in (`InsertAsyncWorkerAudited` — the collision rolls back BOTH), the ledger **append-only by trigger** (`platform_change is append-only (S54 SP3)`), and an already-flying pass ANSWERS rather than double-spending, naming its starter (`startedBy`/`startedAt` — the panel prints the daemon's words, never a sentence of its own). Three reads compose the panel: `platform.status` is ONE daemon-composed answer (each block `available|stale|building|unwired|error`, `unwired` is the ABSENT seam only — a wired store with nothing indexed is `available` and empty; `building` reads the live ledger; usage unrecorded is data, not fault — `TestNoSecondStatusComputation`, `TestUnwiredIsNeverRenderedAsEmpty`), `retrieval.reindexPlan` previews chunks, tokens and the cost **with its state** (`known|partial|unknown` — never a figure without a state, an unpriced route answers `unknown` WITH its reason, an `--embed-route` override is never priced by the configured route's figure, and the planner NEVER calls the embedder), `platform.changes` renders the ledger. All seven doors are **absent** from the narrow projection and the agent-services enumeration; agents get exactly the read-only `index_status` twin — **no control tool exists** (`TestPlatformOperationsAreAHumanDoor`, `TestOperationsAreWrittenOnlyThroughTheAuditedPath`, `TestPlatformChangeIsAppendOnly`, `TestEveryPlatformMethodIsClassified`, the per-variant door matrix, the teeth set — all `cmd/aeon/platform_guard_test.go` + siblings). CLI `aeon platform status|plan|changes` is a pure socket client with **no offline arm** (a rebuilt client-side panel would be the second implementation index.go:9-12 forbids); the seatbelts — `index rebuild`, `graph reparse`, `graph delete` — print the daemon's OWN plan/statement first and write nothing without `--yes`. Zero S44 settings (SP5: nothing runtime needs a knob). The UI's Settings → System (`ui/src/features/settings/system/`) renders ONLY what the responses answer — the state words live in `systemModel.ts` alone, cost pills never outlive their state, controls sit disabled while the daemon reports a flying pass, and refusals print verbatim; its guard (`platformRpc.guard.test.ts`) pins the family to its **seven doors** (census == allow-list, zero storage/fetch/invoke, canary non-vacuous, en/pt parity). Walked rc=0 2026-11-04: `aeon/scripts/qa-s54-platform.sh` (real embed-stub route: the V19 refusal, the plan, the actual pass, a SECOND socket joining the flight and hearing `startedBy`, three audited graph ops, staleness after an edit), recorded at [DEMO §S54](docs/aeon/DEMO.md). See `docs/aeon/S54-platform-status/` |
| **Merge-queue authority is a human door (S60 MQ13)** | The queue plane answers **ten doors** — the six writes `mergequeue.enqueue · dequeue · reorder · land · retry · settings` and the four reads `list · show · status · vocabulary` — **socket methods on the human door**, registered iff the queue seam is wired (nil seam ⇒ all ten ABSENT, not refusing: V13), every one `RequiresInitialize`, none `Debug`, the actor from the peer and never a param; `TestMergeQueueAuthorityNeverReachesOtherDoors` holds those six names **and the six loop actions** `queue.enqueue · git.integrate · verify.run · git.publishIntegration · forge.openLandingPR · forge.land` out of the narrow projection, the agent-services enumeration, the tool registry and the records catalog. The two seatbelts ride the daemon's own words: a `settings --mode auto` widening prints the daemon's **AUTHORITY WIDENING** statement and writes NOTHING until the caller resends `acknowledged "<digest>"` (the digest is `sha256(s60.settings|…)` computed at the door — CLI and bench panel echo the refusal's own quoted sentence, never re-derive it), and `land` shows the row, refuses without `--yes` ("the land is bound to the head shown, and nothing was landed") and binds the land to **that row's digest** — a moved head or a stale view refuses with zero forge calls. Agents get exactly **five names** — `mergequeue_list · mergequeue_show · mergequeue_status` (safe reads answering the doors through the SAME service; own-view by `AgentProjectRoots`: foreign rows are simply absent and a foreign id earns one no-oracle sentence), `mergequeue_dequeue` (own loop, `queued`/`conflicted` only) and the one ask `mergequeue_propose_enqueue` → the V13 twin `mergequeue.proposeEnqueue` + the **seventeenth kind `mergequeue_enqueue`** — **inert until a person commits it** (the commit enqueues under the COMMITTER, never the proposer; the validator behind create/commit/arm is byte-equal on a real repo, base resolved by the daemon). None of the five is in `DefaultGrant()` — registered is not granted (S11). The queue's `mode`/`conflictPolicy` are the queue's own settings rows, widened only through that door — never S44 config keys, never a flag. Guards: `TestMergeQueueWritesAreAHumanDoor`, `TestMergeQueueAuthorityNeverReachesOtherDoors`, `TestMergeQueueGuardsHaveTeeth` (canaries per predicate), `TestMergeQueueAmendmentLeavesThePriorGuardsByteIdentical` (ten prior guard files hash-pinned) — all `cmd/aeon/mergequeue_amend_test.go`; the ten verbs of `aeon mergequeue` are pure socket clients (the slice's ONE named P5 deviation: **no offline arm**, EV18's no-daemon words instead), asserted against the doors in `mergequeue_parity_test.go` both ways. See `docs/aeon/S60-merge-queue/` |
| **The scheduler is an authority record (S27 CN2)** | The continuous scheduler\u2019s controls \u2014 `scheduler.enable \u00b7 disable \u00b7 pause \u00b7 resume \u00b7 root.set \u00b7 root.disable \u00b7 enableTree \u00b7 stopAll` \u2014 are **socket methods on the human door** beside its four reads (`status \u00b7 preview \u00b7 decisions \u00b7 attention`): registered iff the continuous seam is wired (nil seam \u21d2 all twelve ABSENT, `ErrUnknownMethod` \u2014 `scheduler.stopAll` included, so the off switch can never stand without S55 under it: V13), every one `RequiresInitialize`, none `Debug`, the actor from the connection\u2019s peer and **never** a param (identity keys refused by name before the decode; `TestActorIsThePeerNotParams`); refusals render the refuser\u2019s sentence verbatim into `data.what` (`ErrSchedulerRefused` \u221231935, `ErrSchedulerDisabled` \u221231936 \u2014 the composition refusals: enabling a repository under an off master, a pause with no master to pause). The twelve are **absent from the narrow surface, the agent services, the tool registry and the records catalog** (reads only where a later task twins them \u2014 CN20); the plane is **default-off, never seeded, and never inherits authority down the tree** (CN6: `enableTree` expands once, a later child keeps its own). The reads answer the board with the **descriptor** \u2014 the closed vocabularies (priority pair, reason census, window grammar) UI forms draw from, never recompute (CN22) \u2014 and `preview` is the tick RUN as a side-effect-free read (CN15, row counts equal). Guards: `TestSchedulerControlsAreAHumanDoor`, `TestSchedulerWritesNeverReachOtherSurfaces` (six subtests), `TestSchedulerGuardsHaveTeeth` (canaries per predicate), `TestSchedulerRowsHaveNoNonPersonWriter`, `TestOnlyTheWorkerStartsWork` \u2014 all `cmd/aeon/continuous_guard_test.go`, beside the UNEDITED S40 AU1 locks (whose design doc carries the S27 CN2 annotation) and S45\u2019s amendment family. See `docs/aeon/S27-continuous/` |
| **Landing is by a queue of loops; no rebase, no force-push, `integrate/*` refs are never deleted (S60 MQ6)** | Nothing lands a branch directly: an item is enqueued, and while it holds **the one landing slot per base branch** (a guarded UPDATE the store refuses to a second holder even under raw SQL — `TestOneSlotPerBaseBranchRawSQL`; a held row names its holder, the reactor's own sentence, walked) its **landing loop** — an ordinary pinned loop on blueprint `land-feature` — drives catch-up (`git.integrate`: merge the item's branch onto a fresh `integrate/mq-<item>` ref cut at the base tip, in its own worktree under the S32 ledger's `integration` owner), verification, publish, PR and land. **Merge-forward, never rebase** — a rebase of a pushed branch would need the banned force-push (`TestNoForcePushAnywhere` re-run unedited at close; stale refs listed, never deleted, N9) — and the **person's checkout never moves**: HEAD and tree fingerprinted byte-identical across clean and conflicted catch-ups (`TestIntegrateCleanCutsMergesAndReports`, `TestIntegrateConflictIsTypedAndMergeStays`). Verification is **bound and fail-closed**: green means a gate run recorded against exactly this integration sha, so with no evidence the item HOLDS at `verifying` ("no gate evidence yet") instead of landing unevidenced (walked: `scripts/qa-s60-mergequeue.sh` D3); a gate green for sha A never lands sha B. A conflict ends the item **`conflicted` carrying paths and counts only — never hunk text** (SEC-033: a planted secret in a conflicting hunk greps out of every table, event and log — `TestNoHunkTextAnywhere`), and the exit is the policy's: `ask` (attended ⇒ a question naming the paths; unattended ⇒ hold and notify), `resolve` (the resolution loop merges and re-verifies the new head), `abort` (stops landing). With queue mode `auto` and **no S56 merge grant** an unattended land is **denied at `forge.land` with the G14 sentence** and the item ends `failed(denied)` — `--allow-outward` never grants land, and every G14 test passed unedited at close; `forge.land` consumes S57's merge rule as a floor and re-binds head/verified/integration shes or refuses `head-moved`. Recovery is the ledger's: restart reconciles (a killed daemon's rungs reopen at the same rung — `TestRecoveryAtEveryState`), no double merge, terminal rows stay terminal. |
| **Scorecards and retrospectives are measurements: they are never inputs to dispatch, routing, grants, budgets or blueprint choice (S63 RT3)** | The sentence — "A score is a measurement of what happened. It does not change what any agent, blueprint or project is allowed or asked to do." — exists **once on the wire** (`protocol.ScorecardStatement`) and every card, digest and vocabulary answer **quotes the field**, never a copy (`TestRT3SentenceExistsOnce`). The outcome plane records itself: a daemon reactor writes **exactly one outcome row per terminal event** (D1, walked with real cancels), the milestone writer files S28 learnings **only** at `n = 1, 3, 10, 30 …` with `observed` provenance from **one template** whose body is enums and numbers — **no loop free text anywhere** in `retro_outcome`/`retro_agent`/`retro_learning` or the learnings (hostile-goal greps, walked); the three fact tables are **append-only by trigger** (raw `UPDATE`/`DELETE` die at the prompt, walked on live rows). Cards and the learned digest are **derived reads** answering `too-few` / `cannot say` / coverage-first with the P32 cost floor verbatim; the two agent tools (`scorecard_mine`, `digest_learned`) answer the SAME services as the person's doors with the subject from the step — **no parameter exists** to ask for another agent. `retro.backfill` is the human door (preview first; a write needs `--yes` resent, lands `source=backfill`, files **no learning**). The model retrospective (T8) is unbuilt by choice — the `retrospective` blueprint and `retrospective-author` agent seeds belong to S48/S55/S44/S52 — and `retro.status` answers `not enabled`, never a guess. The law is structural, not conventional: the **guard** is `TestScorecardNeverReachedByDispatch` (a source-import walk — importing `internal/scorecard` from the engine is refused even when it compiles; scratch-import canary walked 2026-11-05, naming the offender), beside `TestNoSettingOrGrantIsDerivedFromAScore`, `TestScorecardMethodsAreReadsOnly` and the teeth set `TestRetroGuardsHaveTeeth`. Walked rc=0 2026-11-05: `aeon/scripts/qa-s63-outcome.sh`, recorded at [DEMO §S63](docs/aeon/DEMO.md) |
| **Intake, charter and template writes are a human door; the charter grants nothing; bootstrap actions are person-door only (S61)** | The goal-intake plane turns one interviewed sentence into directories, repositories, a component tree, a charter and records through **one digest-bound, resumable door a person commits**: `intake.commit` verifies the digest first (a changed plan stops before any write — "the plan changed since you saw it"), then walks the ledger `intake_step` rung by rung (verify-digest → per component scaffold → verify-git → publish? → register → tree → charter → records → link), each step a Do with a Verify that must still hold — a step DONE once is re-VERIFIED, never re-run, a `failed` row is failed EVIDENCE and the run refuses rather than repairs (GI9); a mid-write death of the daemon's own half is taken back only when every byte of it matches the preview and a repo there is exactly the run's own single commit (`takeBackOwnHalf`, pinned both ways: `TestKillMidScaffoldResumesOwnHalf`, `TestKillMidScaffoldStrayBytesStillRefuse`), and a kill between a store effect and its ledger row is **adopted on the verify read's own proof** — same root, same planned slug, no second project ever promoted (`TestReEntryAdoptsTheCreateThatLandedWithoutItsRow`, `TestTreeAdoptsThePromoteThatLandedWithoutItsRow`, `TestForeignRegistrationStillRefusesAdoption`); the between-steps matrix is `TestCrashMatrix` over nineteen kill points; walked for real with `kill -9` at three timings — DEMO §S61, rc=0 52 assertions 2026-11-05). Fifteen socket doors — `charter save · vocabulary · history · show`, `template save · vocabulary · show · list`, `intake plan · commit · status · start · abandon · vocabulary` + `proposal commit` reuse — are `RequiresInitialize`, actor from the peer, **absent from** the narrow surface, agent services, the tool registry and the records catalog (`TestHumanDoorWritesNeverReachNarrowOrAgentServices`, `TestUnwiredIntakePlaneRefusesByName`); agents get the reads plus **two inert proposes** (`charter_propose`, `intake_propose` — the kind validators byte-equal with their create twins, `intake_propose` only inside an intake loop), and `repo.scaffold` / `forge.createRepo` are **person-door actions invisible to blueprints** (`TestPersonDoorActionsAreInvisibleToBlueprints`; `TestDispatchAndCallersRefuseTheDoors`). The scaffold guard is the second wall — never writes into a non-empty path, never through a symlink at ANY edge of the target, refuses HOME/state/component roots, refuses when no git identity resolves (GI12: nothing created) — and remote repo creation is **off until three independent keys hold**, private by default, public only with the daemon's own confirm statement. Every goal an intake files lands **`in_review`** and every roadmap item `idea`/`unknown` — approval is always a person (`goal decide`), provenance rows are ids+enums through one fixed template (SEC-033 greps), and a plan **lands exactly once**: the proposal commits and the census moves zero. See `docs/aeon/S61-goal-intake/` |
| **A file stays under 1000 lines** (SIZE1) | The project is now complex, and a file is read whole by the agent that changes it and by the person who reviews it — past 1000 lines every reader pays for the whole file, and cohesion decays, unrelated functions piling up in one place. **A change that would push a file past 1000 splits it in the same change; a file already past 1000 is broken when the next substantial change lands in it (or earlier, if cheap); a new file is designed under the line from the start**, holding only the functions that make sense together, so a reader opens exactly the files the job needs and no more. Split by cohesion, not by size. See §4e |

## 4a. When you finish a task, update the record

**Do this in the same commit as the work.** Not afterwards, not in a batch at the end of the slice.

### Why it is a rule and not a courtesy

**Aeon is not building Aeon yet.** These tasks are being done by different agents, in different sessions, on
different days — a loop here, a hand-written slice there, sometimes two people on adjacent slices. None of
them shares a memory, and **nothing in this repository notices that a task is done**. There is no CI job
reading a checkbox, no daemon watching the tree. The only thing that knows is whoever just finished.

So an un-updated record is not untidiness. It is the next person re-reading a task spec to work out whether
it was already built, finding code that half-matches it, and guessing. That guess is how a task gets done
twice, or skipped because it *looks* done.

### The records to update

| Where | What |
| --- | --- |
| **[`docs/aeon/SLICES.md`](docs/aeon/SLICES.md)** | Update the task’s build/validation cells and the slice’s build state, implementation location, and remaining gaps. Link dated evidence; do not copy long journals or global test totals. |
| **The slice’s `tasks/T*.md`** | Add Notes describing what exists, what changed from the spec and why, and the exact dated validation commands/results, including failures and unrun checks. |
| **The slice’s `DEMO.md` and cumulative demo** | At the demo task, record the actual walk, its lane, and findings under §4b. |

STATUS.md, TASKS.md, PLAN.md, and ROADMAP.md retain historical evidence and planning decisions; do not mirror completion updates there.

### What a Notes block is for

**Write down what the task file could not know.** A deviation, a contradiction between two specs, a number
you measured, a test that turned out not to test anything. S2's T6 notes record that `client.go` could not
live where the task said *and* honour T11, and how that was resolved; S3's T4 notes record that reaching 100%
coverage meant deleting an unreachable branch rather than testing it.

Those are the notes that save the next person an hour. "Done" saves nobody anything.

### Two habits that keep the record honest

**Say what you did not verify.** S1 shipped `main.rs` marked *"has never been compiled"* for a day, and that
is the only reason anyone knew to compile it when the packages arrived. A gap named is a gap someone can
close; a gap assumed away is a gap that surfaces four slices later.

**Update counts by measuring, not by adding.** `go test ./... -v | grep -c '^--- PASS'` rather than your
recollection of how many tests you wrote. A count in a status document that nobody re-measures drifts, and
then the document stops being worth reading.

## 4b. A slice extends the cumulative demo

**Superseded 2026-10-19, at the user's direction (S10).** The 2026-10-15 amendment made each slice add a
tab to a **test bench** in the app. That mandate is replaced — not dropped: every slice now **extends the
single iterable, cumulative `docs/aeon/DEMO.md`** — run the app, it loads what the latest slice built,
here are the numbered steps. A slice **appends a section; it never restarts or replaces the document**.
The walkthrough accumulates where the tabs accumulated, so one run reaches everything the slices have
built. The bench tabs the earlier rule produced stay in the app as history; the document is what grows.

### Why, in the user's words: *"this is to test what we need in the place where it is consumed"*

A capability reachable only from a terminal is a capability nobody checks the way they will use it. S3
configured providers through environment variables and a restart; that works for a developer with a shell
open and is useless for deciding whether the thing is any good. The quote is kept from the 2026-10-15
amendment because the intent is kept, and the new rule serves that same intent by different means: where
the tab put one slice's surface in front of a user, the cumulative demo puts every slice's surface in
front of them, in one run of the app.

### What it is NOT

**It is not an early draft of S9.** S9 builds the real app — loops, the ladder, the thread, chat,
questions answered inline, a designed experience. The cumulative demo **is the test bench** — what the
tabbed bench was, in one iterable document: each slice's section exercises its capability in the running
app and names what it does not show. That labelling is what stops the bench quietly becoming the product
and having to be undone later. S9's designed experience is unchanged in scope.

### What the amendment overrides

Two non-goals, in S2's and S3's briefs, both reading *"No UI capability beyond the version line"* (their
N8). They were right when the UI was deferred wholesale to S9 and remain superseded: the demo grows per
slice, and S9's scope is unchanged.

### The shape to follow

Four rules from the 2026-10-15 amendment survive **verbatim in force** — the generic bridge, the honest
three-state rendering, "say what it does not do", and no secrets in the web layer (§4c is unchanged). The
replacement retargets how a slice proves itself; it relaxes nothing a surface must obey:

| | |
| --- | --- |
| **One numbered section per slice, in the cumulative demo** | `docs/aeon/DEMO.md` is one document for the whole project, seeded during S10. Each slice appends its section — run the app, it loads what the latest slice built, here are the numbered steps — and never restarts or replaces what is already there |
| **The generic bridge, never a new Tauri command** | `rpc(method, params)` exists so a slice that adds a protocol method adds no Rust. A command per method is a second copy of the method table that somebody will forget to update |
| **Render the honest states** | Three connection states, three cost states, errors in three parts. A surface that cannot show *unknown* is a surface that will show a confident wrong answer |
| **Say what it does not do** | "nothing runs it yet — tools arrive at S5" next to a tool call. A bench that looks finished invites somebody to rely on it |
| **Never store a secret in the web layer** | No `localStorage`, no echoing a credential back. See §4c |

## 4c. Credentials in the UI

**A credential goes IN and never comes OUT.** The rule and how it is enforced, because this is the part a
careless change breaks silently:

- The request type has an `apiKey` field. **The response type has nowhere to put one** — `routes.list`
  returns a `RouteView` with `hasKey` and a four-character `keyHint`, and no key field at all. Not a
  redaction applied on the way out: *a different type*, so leaking is not something a forgotten call site
  can do.
- **An empty key on an edit means keep the existing one.** That is what lets the UI change a model name
  without ever holding a credential, and it is why `clearKey` is a separate flag — "unset" and "do not
  change" are different intentions that one empty string cannot express.
- The hint is the **last** four characters. A prefix identifies a key's type and sometimes its project; a
  suffix only answers *which key is this*, which is the actual question.
- **Nothing goes in browser storage.** A credential there would survive the window, outlive the daemon, and
  be readable by anything that can read that profile. Asserted by a test.
- It crosses the local socket once — `0600`, in a `0700` directory, peer credentials checked against the
  running user (P26). The transport logs method names and **never params**, which was verified in the
  framework's source before this was built.

## 4d. The policy subsystem (S10)

**Aeon decides before it acts, and the decision is one sentence.** `internal/policy` is the engine;
`docs/aeon/S10-policy/design.md` is the argument, `brief.md` the rulings it ran on (R1 authorized the one
migration N4 otherwise forbids; R4/R6 kept authoring off the socket and gave the socket its four reads), and the
slice's `DEMO.md` the walk. What the next slice that touches policy has to know:

| Piece | Where | The property it owns |
| --- | --- | --- |
| The closed set | `internal/policy/types.go` | The vocabulary above, reachable only through `AllPolicyTypes · AllTargetKinds · AllSources · AllActions`. A bad value is a `*Reject` naming the value it refused (DB19), never an `error` |
| The evaluator | `internal/policy/evaluate.go` | Pure and total — `Evaluate(ps, req, at)` reads no clock, store or disk. **Deny beats allow regardless of specificity** (PL4); an empty set denies all seven types (PL5); `NotAfter == nil` means **no expiry**, never a far-future sentinel (PL7) |
| The one formatter | `internal/policy/verdict.go` · `inforce.go` | `Verdict.Sentence()` is the only renderer of a decision and `InForceAnswer.Render()` the only one of an in-force answer; every surface prints them **verbatim**. A second formatter is a second truth |
| The write boundary | `internal/policy/store.go` | `Add` and `DecodePolicy` are the only two callers of `checkAuthorship`, the single expression PL1 is enforced by: an **Allow only from `operator`**, a Deny from any of the four sources. A test counts the definitions and the calls so a third enforcement point cannot appear |
| The frozen set | `internal/runtime/context.go` · `internal/daemon/policy_gate.go` | A step is judged by the set its manifest froze, not by today's table (D12). `PoliciesFrozen` separates the two empties — *nothing exists* from *never consulted* (DEF-042) — and `NewStepPolicyGate` **recomputes `PoliciesHash` from the entries and compares it with the recorded digest** before anything is decided; a mismatch poisons the step to deny-everything |
| The audit | `internal/policy/record.go` · `retention.go` | `policy_event` rows carry a **90-day cap** (`PolicyEventAgeCap`), trimmed at daemon start (`TrimPolicyEventsAtStartup`, called from `serve`) and on demand by `aeon policy events`. A trim failure is spoken, never fatal, and **an empty listing may never read as "nothing was refused"** — the third state is `unaudited` |

**Three surfaces, one boundary.** `aeon policy` — `list · add · remove · seed-corpus · why · in-force · events`
(**seven verbs since S11 added `seed-corpus`**) — is the only authoring door; it opens the store directly (the
justified second access path, which WAL serialises behind the daemon's writer) and re-validates nothing,
presenting the boundary's refusals verbatim. `events` takes
`--subject KIND:ID`, and a listing that matches nothing says so as the *filter's* answer rather than the log's —
an empty audit never presents as "nothing was refused". The socket carries four **read-only** methods —
`policy.list · policy.why · policy.inForce · policy.events` — reading the *same* `*policy.Store` the gate reads,
so the app is never shown a set the runtime is not deciding with;
`ui/src/features/test-bench/pages/PolicyPage.tsx` renders them through the generic `rpc()` bridge and adds no
Rust. None of the four writes an audit row: a refusal by `why` is not a refusal that happened.

> **Amended 2026-10-30 by S45 (PM1/PM10), read together with the PL15 row above.** The paragraph's
> "the only authoring door" and "four read-only methods" were the pre-S45 truth and are kept as the
> historical record. What exists now: `policy.add · policy.remove · policy.seedCorpus`
> (`internal/daemon/policy_authoring.go`) and `proposal.commit · proposal.reject`
> (`internal/daemon/proposal_decide.go`, over `internal/desk`'s one implementation) ride the socket as
> the **human door** — registered iff wired, `RequiresInitialize`, never `Debug`, the actor from the
> peer and never a param — and `policy.get · policy.vocabulary · policy.changes` plus `proposal.get`
> joined the reads. "The socket reads only" is no longer true; **"the socket's authoring half belongs to
> the person at the endpoint, and to nobody else"** is, and that half is what the guards now prove
> positively rather than by banning the names (`cmd/aeon/policy_amend_test.go`).

**How to add a policy type** (the way `ToolAccess` was done, S10 T5): name it in `types.go` and add it to
`AllPolicyTypes` · give `0016`'s CHECK its twin in a **new** migration, never an edit (§3) · register an
evaluator, or leave the declared stub that answers *could-not-determine* while still enforcing — only the tests
pinning which types still answer that way keep a real evaluator from claiming stub status (**five placeholders
today** — `RouteAccess · EffectClass · CostCap · SkillAccess · TriggerAccess`; **`ToolAccess` and `CommandAccess`
are the two real evaluators**, `CommandAccess` having filled in at S11 with the matcher over its measured
corpus, which deleted its `Stub()` method and its registration in the same diff and let the in-force answer
move on its own), so say which one you filled · update `aeon policy` help and the
`policy.*` conformance fixtures, then `make protogen`.

**What S11 added to this subsystem, and where each piece lives.** `ask` is the third action (§4's rule); the
command matcher is `internal/policy/match*.go` with segment splitting in the same package (any denied segment
denies the whole command), its specification is the committed
`internal/policy/testdata/corpus.json` extracted from **this repository's own agent definitions** by
`tools/corpusgen` (`make corpusgen`, and CI fails on a diff), and **no test in the slice asserts a count** —
the corpus's shape is asserted as properties (`wildcard_chars`, mid-`*` presence, exact-pattern presence,
conflicting verdicts) because a count fails the day an agent is added, which is the day the corpus is meant to
absorb it silently. `aeon policy seed-corpus --agent NAME… [--dry-run]` turns those rules into stored
`command_access` rows authored as `operator`: explicit, audited (one authored event per rule), idempotent (an
existing rule is **left alone**, so a re-run seeds nothing twice and reverts nothing an operator edited), and
**never automatic** — it runs when you type it. The `--command` sugar writes the spec's `state` as the row's
action restated, and `--spec` beside it is **refused, never precedence-resolved**. `policy.why` answers the
command question only under `--type command_access`: its default is `tool_access`, and a command asked of the
tool question reads as an unmatched default deny — a plausible wrong answer, so name the type (the walked
record's F-3). **The broad-allow warning's threshold has no constant:** the name
`broadAllowWarnFraction` exists only in the comment above `broadAllowWarning`/`broadAllowWarn` in
`cmd/aeon/policy.go`; the rule itself is the inline `matched*4 < total` — **a quarter of the corpus's distinct
patterns** — so a wildcard `allow` that covers 17 of 51 patterns warns on stderr and is still stored (warn,
never refuse), while one matching nothing stays quiet, because breadth it cannot measure is not breadth.
**Known improvement, recorded not fixed:** `runtime.ContextManifest` has no `FrozenSet()` accessor, so the
frozen entries are re-shaped by hand at each reader (`recover.go`, `answer.go`'s resume path and
`daemon.frozenManifest`). One accessor on the manifest type would give the three readers one definition of
"the set this step is frozen to", which is what D12's whole argument rests on.

**Tool-name matching decides in the honest name (SEC-059, closed 2026-10-24).** ToolAccess `Match`
(`spec_toolaccess.go`) compares the **canonical** form of **both** sides — the row's `toolName` and the
request's tool — through a canonicalizer **injected from `internal/tools` at init**: `SetToolNameCanonicalizer`
is an INIT-TIME seam with exactly **one production call** (`toolset.go`'s `init` wiring `CanonicalToolName`;
`policy` never imports `tools` — the direction is `tools→policy`, so alias knowledge is injected, never
duplicated), and one-definition-plus-one-call is machine-pinned by
`TestTheCanonicalizerWiringAppearsInExactlyOnePlace` (`internal/policy/spec_toolaccess_test.go`). **`nil` is
identity, so a new production entry point that does not link `internal/tools` runs unwired and alias-authored
rows go inert** — a `write_file` row then reaches only calls literally spelled `write_file`, never a `write`
one (deny-leaning, never a permission). `Store.Add` **stays permissive** on alias rows (legacy pinned stores
must keep loading them), and the gate, `aeon policy why` and the socket's `policy.why` answer both write
spellings identically — because all three judge through the one `ev.Match` (`evaluate.go`), which is where
fixing the comparison buys gate/why parity structurally rather than per-surface.

## 4e. Files stay under 1000 lines (SIZE1)

**Why it is a rule and not a tidiness preference.** The project is now complex, and a file is read whole — by
the agent that has to modify it and by the person who reviews it. Past 1000 lines, every reader pays for the
whole file to reach one part of it, and cohesion decays silently: unrelated functions pile up in one place, and
the reader can no longer trust that the file they opened contains the whole story for what they need. A
2,000-line file is not a bigger file; it is several files that someone refused to name.

**The line is 1000, measured, not estimated.** `wc -l` (or the editor's own count) on the file **after** the
change. "It's basically there" is not a measurement.

### The three obligations

| | |
| --- | --- |
| **Designing a new file: under the line from the start** | Size it to one concern at design time. A design that parks three unrelated responsibilities in one small file today is designing the 900-line file for six slices from now — and a split costs more the later it happens |
| **A change that would push a file past 1000 lines: split in the same change** | There is no "leave it for a later refactor". A later refactor is how the file becomes the 4,000-line one nobody touches |
| **A file already past 1000 lines: a standing violation** | It is broken when the next substantial change lands in it — or earlier, if the split is cheap. Not grandfathered, not "revisit someday" |

### Split by cohesion, not by size

Each resulting file holds the functions that **make sense together** — one concern, one responsibility. The
test: *if I opened file X for reason R, does X contain all of R?* A split that just slices a 2,400-line file
into three 800-line grab-bags fails that test even though every number is green — the reader still cannot tell
which file holds the whole of what they need, and that is exactly the failure the rule exists to prevent.

### How, per language

- **Go:** same package, multiple files by concern. The existing patterns in this codebase are the model:
  `settings_decl_<area>.go`, `spec_<kind>.go`, `match*.go` — the filename says what the file is for, and the
  concern is what holds it together.
- **TypeScript / React:** one feature, one folder; a component and the types, hooks and data that only it uses
  live with it.
- **Docs:** one topic, one file; a parent doc is a map of pointers, not a container for every topic. (The
  tracked docs already over the line at this rule's adoption are recorded as deferred debt in
  [`docs/aeon/README.md`](docs/aeon/README.md), not exempted from it.)

### After the split, in the same change

- Update every link and reference to the moved content — a split that leaves a dead pointer is a split that
  has not happened.
- Record the measured line counts of the resulting files in the task's Notes, so the next agent inherits the
  fact rather than the guess.

### State at adoption, measured 2026-11-02

**Re-measure before relying on this list** — it goes stale the moment any file is split, and this rule is
what makes that happen. The command: `find internal cmd ui/src ui/src-tauri/src -type f \( -name '*.go' -o -name '*.ts' -o -name '*.tsx' -o -name '*.rs' \) -exec wc -l {} +` from `aeon/`, filter `> 1000`.

- **One file is out of the split obligation because it is generated:** `ui/src/proto/contract.ts`
  (4,228) — *"Generated by go run ./tools/protogen. Do not edit"*. Its size is a property of the protocol's
  breadth; the rule applies to its **source** (the conformance fixtures), not its output.
- **Thirty-two hand-written code files were over the line at adoption.** They are standing violations under
  the third obligation above: each is broken **at its next substantial change, one file per change** — never
  in a mass-split diff. Splitting all of them at once is the same failure the rule exists to prevent, aimed
  at the diff instead of the file.

| Lines | File |
| --- | --- |
| 2,470 | `internal/store/schema.go` |
| 2,391 | `cmd/aeon/policy_test.go` |
| 2,238 | `internal/store/schema_test.go` |
| 1,717 | `internal/protocol/method.go` |
| 1,634 | `internal/daemon/records.go` |
| 1,568 | `cmd/aeon/policy.go` |
| 1,471 | `internal/runtime/loop.go` |
| 1,392 | `internal/daemon/daemon.go` |
| 1,352 | `internal/daemon/blueprint.go` |
| 1,345 | `internal/protocol/step.go` |
| 1,329 | `internal/store/policy_test.go` |
| 1,298 | `internal/policy/store_test.go` |
| 1,291 | `internal/protocol/errors.go` |
| 1,252 | `internal/daemon/budget_doors_test.go` |
| 1,233 | `cmd/aeon/autonomy_guard_test.go` |
| 1,222 | `ui/src/features/test-bench/pages/DemosPage.tsx` |
| 1,208 | `internal/protocol/conformance_test.go` |
| 1,182 | `internal/runtime/loop_test.go` |
| 1,142 | `internal/store/steers_test.go` |
| 1,136 | `internal/daemon/autonomy_demo_walk_test.go` |
| 1,130 | `internal/daemon/policy_replay_test.go` |
| 1,114 | `cmd/aeon/policy_amend_test.go` |
| 1,112 | `internal/policy/inforce_test.go` |
| 1,109 | `cmd/aeon/store_verbs.go` |
| 1,095 | `internal/policy/evaluate_test.go` |
| 1,085 | `internal/policy/types_test.go` |
| 1,062 | `internal/daemon/policy_authoring_test.go` |
| 1,046 | `internal/store/budget.go` |
| 1,023 | `ui/src/lib/protocol.ts` |
| 1,019 | `internal/store/store.go` |
| 1,017 | `cmd/aeon/policy_golden_test.go` |
| 1,005 | `internal/store/telemetry.go` |
| 1,004 | `internal/model/client_test.go` |

## 5. Conventions

**The UI toolkit is `@cjlapao/ui-kit@^0.2.0`.** `aeon/ui/` imports components from it rather than
hand-rolling them; `react-router-dom` is a peer dependency and `@cjlapao/ui-kit/styles.css` is imported once
at the entry (prebuilt Tailwind v4.3.3). Two things it cannot express yet — the ladder's `not-needed`/
`skipped` states and socket liveness — are settled in S9 T4 by extending the kit or composing locally,
**never by reusing the nearest existing type.**

**Adding a bench page** (amended 2026-10-30, for the same parallel-worktree reason as the migrations split
above). Today's **nineteen** pre-registry pages still have their own hand-written entry in `ui/src/app/
routes.ts`'s `LEGACY_BENCH_PAGES` and their own branch in `ui/src/features/test-bench/TestBench.tsx` —
untouched, and never a model to copy. **A new bench page, from here on, touches no shared array, no shared
import list, and no shared switch:**

1. Write the page's own file under `ui/src/features/test-bench/pages/`, exporting the component as always.
2. At that same file's top level, call `registerBenchPage({ id, label, path, order, group?, needsConnection, render })`
   from `ui/src/features/test-bench/lib/benchRegistry.ts` — `order` is the page's menu position among bench
   pages (lower first; pick a number past whatever the last slice used), `needsConnection: false` only for a
   page that renders usefully with no daemon (most pages are `true`).
3. Add **one line** to `ui/src/features/test-bench/pages/registerAll.ts`: `import "./YourPage";`. This is the
   only file two slices can still both touch, and it only ever grows by appending a line — a trivially
   mergeable, near-zero-conflict edit, not an array entry plus an import plus a switch case.

`routes.ts`'s `BENCH_PAGES` (what the menu renders) and `TestBench.tsx`'s generic fallback branch (what
actually mounts the page) both read the registry automatically — nothing else to wire. The `BenchPage` TS
type stays sourced from the legacy array alone (a dynamically-registered id can't be a compile-time string
literal); `isBenchPage` is the one runtime gate that already covers both, unchanged. See `ui/src/features/
test-bench/lib/benchRegistry.ts`'s own doc comment for the full reasoning, and `ui/test/bench-registry*.test.ts`
for the proof this actually works end to end.

**Package layout.** The framework's own `AGENTS.md` specifies a strict per-service layout
(`plugin.go` + `service/` + `models/` + `interfaces/`). **Follow it for anything you register as a
service.** Aeon's own internal packages — `store`, `runtime`, `protocol` — are plain Go packages and
follow their slice's design instead.

**Errors are discriminated results; `error` is for infrastructure failure.** *"This transition is not
allowed"* and *"the disk is full"* need different handling, and a single `error` return makes every
caller guess which it got. This is S4's DB19, applied everywhere.

**A refusal names its reason, and the thing refused.** `refused: bash · no-shell-in-review · lane-type:review`
is actionable. `permission denied` is not.

**Three-state honesty.** Where verification can be unavailable, the answer has three values, never two
(P31). Cost is `known` / `partial` / `unknown`. A capability is `verified` / `declared` / `contradicted`.
Collapsing the third into one of the others is how a lower bound silently becomes a fact.

**Tests never point at the real store**, a live provider, a real repository or a real forge. Recorded
fixtures and fakes.

## 6. Getting started

```bash
make help              # the default target; a bare `make` teaches
make dev               # run the daemon with the endpoint on, in ./.dev
make ci                # fmt-check, vet, guard, test, build — what CI runs
```

`make dev` prints where it lives (B10), which is where every `DEMO.md` begins:

```
endpoint  /…/aeon/.dev/aeon.sock
store     ~/.local/state/aeon/state.db  (not created yet — S4)
```

**A plain `aeon serve` needs no configuration at all.** On a clean machine it generates the signing key,
binds the endpoint under Aeon's own namespace, and reports zero errors — verified. The endpoint is on
because it is pinned on, not because `make dev` sets a variable.

Then, from another terminal:

```bash
printf '{"jsonrpc":"2.0","id":1,"method":"nope"}\n' \
  | socat -T3 STDIO,ignoreeof UNIX-CONNECT:.dev/aeon.sock
# {"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"method \"nope\" is not registered"}}
```

**Use `ignoreeof`.** `socat -t2 -` and `nc -U -q2` half-close at stdin EOF and lose the reply — empty
output, exit 0, indistinguishable from a daemon that ignored you. Measured; see
`docs/aeon/S1-bootstrap/TESTSPEC.md` §5.3.

**That `-32601` is the whole of the ground floor working.** The transport is live and the dispatcher is
real; S2's job is to register methods, not to build a socket.

**`GOPRIVATE=github.com/cjlapao/*` is required** — the checksum database cannot verify the framework's
module. The Makefile exports it, so use `make` rather than bare `go` commands, or export it yourself.
`CGO_ENABLED=1` is exported beside it for the same reason (B3).

**The preamble a bare `go` command needs, measured on this box.** Both facts below fail silently or
misleadingly if missed, so they belong in the record rather than in one contributor's shell history:

```bash
# sqlite-vec.h includes <sqlite3.h> and this machine has no system copy: without the header on the
# include path the build dies with `sqlite-vec.h:7:10: fatal error: sqlite3.h` — a C error, from a Go
# build, in a package you did not touch. The driver ships the header; copy it under its real name.
# The version must match go.mod's mattn/go-sqlite3 (v1.14.52 when this was measured).
# From the repository root:
mkdir -p .tmp/cgo-shim
cp "$(go env GOMODCACHE)/github.com/mattn/go-sqlite3@v1.14.52/sqlite3-binding.h" .tmp/cgo-shim/sqlite3.h

# Then, from aeon/, the form every S10 evidence line used:
ROOT="$(git rev-parse --show-toplevel)"
GOPRIVATE='github.com/cjlapao/*' CGO_ENABLED=1 CGO_CFLAGS="-I$ROOT/.tmp/cgo-shim" \
  timeout 600 go build ./...
GOPRIVATE='github.com/cjlapao/*' CGO_ENABLED=1 CGO_CFLAGS="-I$ROOT/.tmp/cgo-shim" \
  timeout 900 go test ./... -timeout 120s -count=1
```

`.tmp/` is gitignored and machine-local: a fresh checkout has no shim, which is why the copy is part of the
preamble and not an assumption. **Anything that opens the store** — every `aeon` CLI verb, the CLI fixture tests,
a `DEMO.md` walk — **exports `XDG_STATE_HOME` at a throwaway directory first** (this box uses `/tmp/opencode/…`)
and never at the real store, which is §5's "tests never point at the real store" made executable. The state
directory is `$XDG_STATE_HOME/aeon` when that variable is **absolute**, and `$HOME/.local/state/aeon` otherwise;
a *relative* value is ignored on purpose so the store cannot follow the working directory (`internal/paths`, and
a test that says so).

### Debugging

`aeon/.vscode/launch.json` has the configurations; `aeon.code-workspace` at the repository root makes them
visible without closing the monorepo.

| Pick | For |
| --- | --- |
| **Stack: daemon + desktop app** | the usual one — both halves, breakpoints live, over `.dev/aeon.sock` |
| **Daemon: serve (dev endpoint)** | the daemon alone, isolated: `.dev/aeon.sock` and `.dev/state` |
| **Daemon: serve (REAL state)** | labelled because it is not isolated. For a bug that only happens against your actual store |
| **Go: debug tests in this package** | the open file's package. For one test, the codelens above the func is faster |
| **App: debug tests (vitest)** | in-band, so `.tsx` breakpoints bind |
| **App: web (Chrome)** | layout and state. **No Tauri bridge in a browser**, so the pill reads "not connected" — correct, not a fault |

Two things the configs do that are easy to miss, and painful to debug if missed: they set `CGO_ENABLED=1`,
because delve builds its own binary and a debugged daemon without sqlite-vec diverges from the one `make
build` produces; and they point the app at the same `.dev/aeon.sock` as the daemon, because an app looking
at a different socket from the daemon it should be talking to is the most confusing failure available here.

**Verified**: the Go configs, under delve 1.26 — breakpoints bind in `cmd/aeon` and `internal/`.
**Unverified**: `App: desktop shell (Rust)`, which needs Tauri's system libraries this machine lacks
(`make -C ui check-deps`).

## 7. CI is parked, on purpose

`aeon/.github/workflows/` is **inactive**. GitHub only reads workflows from the repository root, and this
is a subdirectory of a monorepo whose root `.github/` belongs to the v1 system still building Aeon.

They are written now, and kept here, so that extracting Aeon to its own repository is a move rather than
an authoring job. See [`.github/workflows/README.md`](.github/workflows/README.md) for how to activate
them and what must be true first.

## 8. State of the ground floor

**Working, verified on a clean machine with no configuration:** the module builds; `aeon version` and
`aeon help` answer; `aeon serve` generates the cursor key at `0600`, binds
`/run/user/<uid>/aeon/rpc.sock` as `srw-------` in a `drwx------` directory, prints both paths, completes
a real JSON-RPC round trip (`-32601` for an unknown method), **leaves `:8080` closed**, and shuts down
gracefully removing its endpoint. Zero errors. `make ci` green, 20 tests.

**Not built — these are the slices:** the JSON-RPC method table and event stream (S2) · the model layer
(S3) · the store (S4) · the step runtime (S5) · everything after.

**Known gap, measured:** `make build-all` passes **1 of 6** targets once a real cgo dependency is in the
tree (5 of 6 today only because nothing yet needs a C compiler). Cross-compiling CGO needs a toolchain
per target. **That is S1 T4's work, and the number to beat is 1.**

**What the v0.10.1 upgrade proved.** Nine new registerable services arrived and **the daemon did not
widen by one line** — the allow-list made them inert and `services.golden` proved it. That is B8 and B11
working on their first real upgrade, which is the whole argument for an allow-list over a deny-list.

**What it also proved.** The guard checks the registered *set*; it does **not** catch a new *runtime
config requirement*. `cursor.signing_key` broke `aeon serve` completely while `make ci` stayed green,
because building and testing never boot the daemon. **An upgrade is not verified until something has
run `serve` on a clean machine** — `make framework-upgrade` runs the fixture test, and that is necessary
but not sufficient.
