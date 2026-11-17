# Brief — S44 Config store

Written 2026-10-30. **Planned the same day:** `design.md` (CS1–CS14, with a "Verified facts" section read from
the real code), `plan.md`, and `tasks/T0.md`–`T9.md` now exist; read them before building. The text below is the
original brief, kept as written except where marked **[resolved]** — and where its description of the code turned
out to be wrong, see §6.

**Stage:** 5 · Fleet, ahead of the policy/MCP settings tier in `CONFIGURATION-ORDER.md` (that tier, and most
of the ones after it, will want this). **Depends on:** S4 (store), S23 (`internal/config` — the thing this
slice extends).

## 0. Why

S43 (agent management) needs one runtime setting (`agents.allowUnsignedImport`) that a person flips from
the Settings UI, live, with no restart. `CONFIGURATION-ORDER.md`'s later tiers (policy defaults, MCP tier
defaults, automation defaults) will each want the same thing. Nothing in this project does that today.

## 1. What exists today, measured

`internal/config` (built S23) is a **five-layer, boot-time-merged** system: defaults → user file → project
file → env → flags (`layers.go`), read once at `aeon serve`/CLI startup, immutable for the process's life.
It also owns the one credential rule (C4: a credential never lives in a config file, only env or a
dedicated file path, `creds.go`) and names which layer/file owns a key for `doctor`/validation (`D3`,
`validate.go`). This is the right place to **extend**, not duplicate — the operator's own instruction: "we
need to extend the current config service as writing another one might get complex."

**The real tension to name, not yet resolved:** the five existing layers are merged **once, at boot**. A
Settings-UI-writable value is the opposite shape — read live, writable while the daemon is running, with no
restart. Bolting a sixth "database" rung onto the existing `Layer` enum would be wrong if `Config.Load`'s
callers expect a value that's fixed once loaded (`VERIFY` every current caller of `Load`/`Config.Get`-
shaped functions before assuming this is safe). The more likely right shape: **the same package, a new,
separate accessor** — e.g. `config.Store` or similar, backed by a new database table, read live on every
call rather than merged at boot — living beside the five layers rather than becoming a sixth one. Whether
it should still participate in the existing precedence story (can an env var or a flag still override a
DB-stored value for one run, the way they already win over the file layers?) is a real product question,
not a given — `VERIFY`/ask the operator.

There is no existing generic settings table. The only precedent is dedicated columns on `component`
(`queue_advance`, `sizing_mode`, migration `0041`) — genuinely per-component, added one at a time by ALTER.
That pattern stays for anything genuinely per-component; this slice is for values that are **not**
per-component (daemon-wide, or scoped some other way a column can't express cleanly).

## 2. What this slice is

A new table (shape TBD — key/value/scope/updated_at is the obvious starting guess, but `VERIFY` against
this project's own table conventions before locking it, same as S43 did for its own table), a `config`-
package accessor over it (read live, no boot-time caching — a Settings UI write must be visible on the very
next read, same process or not), and exposure over the socket (`settings.get`/`settings.set`/`settings.list`
or similar — P34, never file-only; this is the first thing in the slice the app actually talks to). Values
that are genuinely credentials still go through `creds.go`'s existing one-reader rule, never through this
new table in plaintext (C4's whole argument applies here just as much as to a file).

## 3. Out of scope

- Migrating any *existing* `internal/config` layer value (store path, provider credentials, etc.) into the
  new table. Those stay exactly where they are; this slice is for new, UI-writable settings only.
- Deciding the actual settings for S43/policy/MCP/automation — this slice just builds the mechanism. Each
  consuming slice picks its own keys when it needs them.
- Per-component settings — the existing `component`-column pattern is unchanged and still the right tool
  for anything scoped to one component.

## 4. Open questions for the next agent (ask or decide and record, the way S43 did)

1. Sixth layer on the existing `Layer` enum, or a separate accessor beside it? (Leaning separate — see §1 —
   but confirm against every current `Load` caller before locking it.)
2. Does an env var/flag still win over a DB-stored value for one run, the same way they win over the file
   layers today? Or is DB-stored authoritative once set, full stop?
3. Table shape: plain `key TEXT PRIMARY KEY, value TEXT, updated_at` (schema-less, fast to extend, no
   per-key type safety — flagged as a real tradeoff in the conversation this brief came from) vs. something
   with a closed key vocabulary and typed columns (safer, slower to extend). Default to the simple KV shape
   unless a specific setting's risk (e.g. anything security-relevant like `agents.allowUnsignedImport`)
   argues for a real column instead.
4. Socket method shape and seam location — same question S43's own T0 asks for its own methods; likely the
   same `daemon.Options` func-seam pattern S34–S37 all used.
5. Scope model: is every setting daemon-global, or does some need a scope (per-project, per-workspace)? S43
   only needed global; `CONFIGURATION-ORDER.md`'s later tiers may not.

## 5. Acceptance criteria (sketch — the planning agent should firm these up)

1. A value written through the new mechanism is read back correctly on the very next call, same process or
   a fresh one — no boot-time caching anywhere in the new path.
2. A credential-shaped value cannot be written through this mechanism in plaintext — it routes through
   `creds.go`'s existing rule or is refused, named.
3. The new socket methods are the only way the Settings UI touches this — confirmed by the same P34 grep
   every other slice's close-out has run.
4. `internal/config`'s five existing layers and every current caller of them are unchanged and still pass
   their existing tests — this slice adds, it does not edit, that system.

## 6. Resolved by planning (2026-10-30) — and one correction to §1

All five open questions in §4 are answered in `design.md`. They are **documented calls, not operator rulings**;
four are real product decisions the operator may overrule, each marked in the design with what changes if so.

| §4 question | Answer | Design |
| --- | --- | --- |
| 1. Sixth layer or separate accessor? | Separate accessor, new `settings*.go` files in `internal/config`; the five layers are hash-pinned untouched | CS1 |
| 2. Does env/flag beat a DB value? | Environment beats database beats default; **no flags**. A write while the environment shadows the key still succeeds and says so (`shadowedBy`); the UI locks the row | CS4 *(product call)* |
| 3. Table shape | Key/value TEXT **plus a closed registry of declared keys** that types and bounds every write; unknown keys refused naming the nearest; no per-key column, `Sensitive` flag instead for guard-widening keys | CS3 *(product call)*, CS8 |
| 4. Socket methods and seam | `settings.list/get/set/unset/history`, local-interface seam, one shared `*config.Store` handed to the protocol seam and in-process consumers; absent from the narrow surface, test-pinned | CS9, CS10, CS11 |
| 5. Scope model | Global only, no scope column; scoped values are a later slice's migration | CS6 *(product call)* |

**Correction to §1 (checked against the code):** the brief says the five layers are "read once at `aeon serve`/CLI
startup". That is the layers' **design**, not their current wiring: `config.Load` has **no production caller**
(`grep -rn 'config\.Load(' aeon` finds none outside the package), and the only importer of `internal/config` in the
module is `internal/presets/presets.go`, which uses `config.Credential` only. So there is no `Load` caller to break,
which removes the brief's worry — but the five layers still stay untouched (CS1), because the rule stands on its own
and because the first production `Load` caller will arrive eventually.

### Acceptance criteria — firmed up

The four sketched in §5 are now ten (see `tasks/T9.md` for the evidence table): live read on the very next call
(same process, second handle, fresh process); no credential can be admitted; the UI reaches settings only through
registered socket methods; the five layers and their callers unchanged; precedence with the winning layer named on
every read; refusals name what is wrong and write nothing; every change audited exactly once and the audit
append-only; writes absent from the narrow surface, agent tools and agent-services; only `internal/config` and the
daemon adapter touch the tables; zero production declarations and an honest empty state.

## 7. Handoff status (2026-10-30)

**Done:** this brief, `design.md`, `plan.md`, eleven task files (T0–T10), and the implementation. T0–T9
built and re-verified 2026-10-30 (close-out in `tasks/T9.md#notes`): the store + append-only audit + closed
registry + the five `settings.*` socket methods + the CLI + the UI panel, all green; the migration drill
ran against a real store; the ten acceptance criteria re-ran PASS. T10 adopted S43's
`agents.allowUnsignedImport` onto the store (path B: read per-import-call through `config.Reader`, the old
`AEON_AGENTMANAGE_ALLOW_UNSIGNED_IMPORT` kept as a one-release fallback).
**Validation gap (recorded, not a claim):** the desktop (Tauri) app walk was NOT run this session — no
running daemon (the sandbox's `/run/user` endpoint is read-only) and no desktop shell (AGENTS.md §6). The
mechanism is proven by 16 real-daemon CLI tests instead. The pre-existing `narrow_http_test.go` reds
(`TestRealHTTPRequestIsGatedAndAnswers`, `TestLiveRefusalsAnswerWithReason`) are not this slice.
**Coupling (operator ruling 2026-10-30):** S43 is not changed from here. T0–T9 touch no S43 file. T10 is
S44's own adoption of `agents.allowUnsignedImport`, run after S43 merges (design CS15); it changes only how
the boolean is obtained (per-import-call via `config.Reader`), never S43's behaviour.
