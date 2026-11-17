# Brief — S59 Execution environment: what a loop's commands run in, and what they can see

Written 2026-10-30. Prefix **EV**. Covers [`AUTONOMY-GAPS.md`](../AUTONOMY-GAPS.md) **G5**. Task shapes follow
[`MANAGEMENT-PATTERNS.md`](../MANAGEMENT-PATTERNS.md) (P1–P11). The org record is the S44-extra component **tree**
(a root component and its children; a root with children is a project) — this slice says *root component*, never
*project*. Standing rules: [`CONFIGURATION-ORDER.md`](../CONFIGURATION-ORDER.md).

## 1. Why

Aeon is meant to run work with nobody watching (S27, S56, S57). Today every command a loop runs — the `bash` tool,
and the git commands the daemon runs for it — executes **on the host, in the daemon's own process environment**. Four
measured consequences:

1. **The model can read the daemon's secrets.** `tools/spawn.go:155` sets `cmd.Env = nil`, which in Go means *the
   child inherits the daemon's entire environment*. That environment holds the provider keys the daemon was started
   with (`daemon/routes.go:84`: `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`) and the forge token
   (`forge/github.go:83-91`: `AEON_FORGE_TOKEN`, `GITHUB_TOKEN`, `GH_TOKEN`). Any command that runs code — `go test`,
   `npm test`, a script the model just wrote — can print them into its own output, which becomes the model's context
   and the stored transcript. The code comment calls scrubbing "a policy decision, which is S11's" (`spawn.go:141-153`);
   S11 shipped command policy and never decided it. The command policy cannot close this: it matches command *text*,
   and `bash.go:65-69` states it does not parse `bash -c`, `$()`, a single `|` or subshells.
2. **There is no isolation and no place to put one.** `bash.go:46-48`: "there is no sandbox (a standing non-goal)".
   No container, VM or sandbox code exists anywhere (grep of `internal/` and `cmd/`, 2026-10-30). The working
   directory is the only boundary, and it is a convention.
3. **An app's own secrets and services have nowhere to live.** Building a real app needs a database, a test API key,
   a preview URL. Today the only way to give a command a variable is to put it in the daemon's environment — which
   leaks it to every loop and every model — or to type it into a command the model then sees.
4. **Daemon-side subprocesses inherit too.** `git.Client` runs real `git` with no `Env` (`git/plumbing.go:53-54`), so
   a hook the model wrote into `.git/hooks` runs with the daemon's environment the next time the daemon commits;
   the UI's `git diff` (`workspace/git.go:485,559-573`) strips `GIT_*` and inherits the rest, and `git diff` can run
   repository-configured external diff programs.

The operator's brief (AUTONOMY-GAPS G5): isolated runtimes, service dependencies, the app's secrets injected without
being exposed, preview deployments, per-environment egress policy.

## 2. What exists today (measured 2026-10-30; T0 re-walks every line)

| Fact | Where |
| --- | --- |
| `Spawn(ctx, command, opts, verdict)`; `cmd.Dir = opts.Dir`, `cmd.Env = nil` ("INHERITED, and that is a stated limitation"), `Setpgid`, group kill, capped output | `internal/tools/spawn.go:106-260` |
| `SpawnOptions{Dir, Timeout, StdoutCap, StderrCap}` — no environment field | `spawn.go:75-87` |
| `bash` spawns only with a policy-issued allow token; `SpawnOptions{Dir: tc.WorkspaceRoot}`; ceiling `BashTimeout = 600s`, shortened only | `internal/tools/bash.go:127-160` |
| `ToolContext{WorkspaceRoot, CommandVerdict, Records}` — the seams a daemon hands the tools package | `internal/tools/read.go:38-71` |
| The daemon builds the toolset per step; the agent identity and workspace root arrive as arguments; the step id rides the context | `internal/daemon/step.go:~380-430` (`BuildTools`), `stepctx` |
| A loop step's `WorkspaceRoot` is `st.Root`; a parallel unit copies the state with only `Position` changed and runs the model step on it | `daemon/blueprint.go:645`; `loop/advance_parallel.go:217-219` |
| Parallel units get a worktree (`<ComponentRoot>/.worktrees/<kind>-<id>`) written to the `loop_worktree` ledger and added with `git worktree add`; `wt.Path` is used only for settle/remove — **nothing passes it to the model step** | `advance_parallel.go:209-283,353-355` |
| Attendedness is pinned on the loop (`loop.attended`, NULL = never declared ⇒ not attended); read by `Store.LoopAttended` | `store/loops.go:333-345`; `daemon/step.go:337-369` |
| MCP stdio servers already get a **least-privilege environment**: `PATH`, `HOME` and the variables their config named — the in-repo precedent for this slice | `internal/mcp/client.go:273-295` |
| Terminals inherit the full environment with `TERM` replaced — a person's own shell, deliberately ungated (LW8) | `internal/terminal/session_unix.go:24-26,146-158`; `terminal.go:1-20` |
| Every non-test `exec.Command`: `tools/spawn.go:135`, `workspace/git.go:485`, `terminal/session_unix.go:24`, `mcp/client.go:289`, `git/plumbing.go:53` | grep, 2026-10-30 |
| Provider route keys are stored through `internal/secrets` (`SaveRouteKey/ReadRouteKey/DeleteRouteKey`); route rows carry no key; the credential rule is "environment or a key file, never a config file" | `daemon/memory.go:680-735`; `config/creds.go` |
| The narrow surface's `Key` type prints `<redacted>` from `String`/`GoString`/`MarshalJSON` — the redacting-type precedent | `internal/narrow/auth.go:35-47` |
| `internal/secrets` is **not readable in this working environment** (sandbox-blocked); only its call sites are known | agent report 2026-10-30 |
| Component create does no filesystem or git check; each root belongs to one component | `store/components.go:67-112` |
| No backup, container, VM or sandbox concept anywhere | grep, 2026-10-30 |

## 3. What this slice delivers

1. **An environment record** (a DB row, rule 6): a backend, a closed spec (environment variables by *name*, secret
   references, limits, egress mode, services), bound to the workspace, a root component or one of its children
   (a child resolves its own binding, else its root's, else the workspace default). Reads show **what each backend
   can and cannot enforce** — honesty, not aspiration.
2. **Backends behind one interface.** `host-inherit` (today's behaviour, kept as an explicit *legacy* row),
   **`host-scrubbed`** (the default and the first deliverable: same host execution, an explicit variable allowlist,
   no inherited secrets), **`container`** (real isolation, built in this slice behind the same interface with a
   `docker`/`podman` CLI driver), and a `vm` slot that is designed and refused until a driver exists. Aeon does not
   depend on any one product (Parallels VMs are a later driver, not a requirement).
3. **Secret references.** A `secret_ref` names *where* a secret comes from (a daemon environment variable or a key
   file) and *which variable* it is injected as. **No value is ever stored, returned, logged, audited or accepted over
   any door.** Output of every command is redacted of the injected values before it reaches the model or the transcript.
4. **The execution seam.** `bash` (and S57's `verify.run`) run through a `CommandExecutor` the daemon resolves **per
   step** from the loop's component and attendedness. `Spawn` can no longer be called without an explicit child
   environment. **An unattended loop never runs in the legacy inherited environment**; a bound backend that is
   unavailable **refuses the command** (recorded on the step) — it never silently runs somewhere weaker.
5. **Lifecycle.** Provision/teardown **ledger rows** written ahead of the real action, reconciled at startup like
   worktrees (`reconcile_worktree.go`), torn down when the loop ends; per-loop services (a database) started and
   stopped with it. Preview deployments are **specified only**.
6. **Every door, daemon first.** `environment.*` and `secretref.*` socket methods; `aeon env` and `aeon secretref`
   as their clients; read tools plus one propose-only, restrictive-only tool for agents; an **Environments** panel in
   Settings. Writes are a human door with sibling guards and canaries (P4), including a guard that **no non-test
   code path spawns a process that inherits the daemon's environment** except the listed, justified sites.

## 4. Out of scope

- A VM driver (Parallels or otherwise) — the interface and the `vm` slot ship; a driver is a later slice.
- Host egress filtering: an unsandboxed host process cannot be network-restricted by Aeon. The record says
  `unenforced`; only the container backend enforces `none`. Egress **allowlists** need a proxy and are staged
  (refused at write with a sentence).
- Preview deployments (spec reserved, refused at write).
- Protecting files on disk from a host process (`~/.aws/credentials`, `.env` in the repo): host backends scrub the
  *environment*, not the filesystem. That is the honest reason the container backend exists.
- The `read`/`edit` file tools' own path policy, the policy corpus, and the person's terminal.
- Fixing S32's parallel-unit working directory (a cross-slice finding, §5 and EV15).

## 5. Findings surfaced to other slices

1. **S32 (built): parallel units run in the shared root.** `runOneUnit` creates and registers a worktree, then runs
   the model step on `unitSt := st` (only `Position` changed) whose `Root` is the loop's shared root — the worktree
   is created, but nothing runs inside it (`advance_parallel.go:209-221,280`; `daemon/blueprint.go:645`). T0 proves
   it with a failing test. This slice's executor takes its directory from the step request, so it follows whatever
   S32's follow-up fixes; it does not fix it.
2. **AUTONOMY-GAPS G5 understates the leak.** It describes secrets *to be* injected; it does not say the daemon's own
   provider keys and forge token are **already** visible to model-run commands. That is the first thing this slice
   closes.
3. **Daemon-side git and `git diff` inherit the environment** (§1.4) — in scope here (EV10), small.

## 6. Acceptance

1. A loop step's `bash` on a `host-scrubbed` environment cannot see the daemon's provider keys or forge token — proven
   by a sentinel set in the daemon's environment and an actual `env` and `go test`-style child that tries to print it.
2. An **unattended** loop resolves at least `host-scrubbed` even when the bound environment is the legacy
   `host-inherit`; the downgrade is recorded on the step.
3. A secret reference's value is injected into the command and is **absent** from the tool result, the transcript
   rows, events, audit rows, logs and the database file bytes (a sentinel scan of every sink), including when the
   secret straddles the output cap boundary; the limits of redaction are stated in the tool description.
4. `Spawn` cannot compile without an explicit child environment; a guard fails if any non-test code spawns a process
   with an inherited environment other than the two listed, justified sites (person's terminal; legacy backend), with
   a canary that proves the guard can fail.
5. A bound container backend runs the command in a container with only the working directory mounted, `--network
   none` when so declared, resource limits applied; an unavailable runtime **refuses** the command (recorded on the step), never falls back.
6. Provision rows are written before the runtime call; a daemon killed mid-provision reconciles at startup (adopt or
   remove, never leak); teardown runs at loop end.
7. `environment.*` / `secretref.*` writes are on the human socket only, absent from the narrow surface, agent
   services, the tool registry and the records catalog (except the one propose twin); the agent proposal is
   restrictive-only (`Restricts`), committed by a person through S45's generic `proposal.commit`.
8. Every CLI verb has a socket twin and the reverse (parity); no flag, stdin or prompt accepts a secret value.
9. `make protogen && make check-protogen && make guard` green; migration drills (fresh, upgrade, idempotent
   re-open, append-only proof); the S59 behaviour for an upgraded store is unchanged until a person binds a stronger
   environment (goldens), except the unattended rule.

## 7. Depends on

**S44-extra** (the component tree and `rootId`; the resolver follows `parent → root`), **S45** (`internal/desk`,
generic `proposal.commit`, shared UI components — T9), **S43-extra T3** (seed engine, for the inert seed set).
Cross-slice requests to **S56** (autonomy grants must be able to *require* an environment strength/egress and read
`capabilities`), **S57** (`verify.run` uses this executor), **S55** (wall/resource limits), **S58** (provision
failures are events worth a notification), **S60** (the merge queue's re-verification runs in a session), **S61**
(goal intake proposes the first environment), **S27** (scheduler loops are unattended ⇒ scrubbed), **S62** (the new
tables are in the backup set; they hold no secret values). See `design.md` EV22.

## 8. Status

**Built and pushed through T11; T12 close-out in progress (2026-11-04).** Thirteen tasks: T0–T11 built,
committed and merged to `main`; T12 has walked the drills, the CLI lane and the container lane against a real
daemon and rootless docker, and still owes its acceptance evidence table, the dual-backend sentinel scan and
this section's final state. Build state, implementation paths and the open gaps are in
[`../SLICES.md`](../SLICES.md)'s S59 row; the walking evidence and every named gap are in
[`tasks/T12.md`](tasks/T12.md) and [`DEMO.md`](DEMO.md). Deliberate deviations from this brief are listed in
the task Notes (the `attended` input removed from `environment.effective` under G14; `RemoveContainer`
renamed to satisfy S45's write-boundary guards rather than exempting the call; the idle-TTL source word
corrected to two states).
