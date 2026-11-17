# Plan — S44 Config store

## What is settled before planning

`brief.md` (the why, the scope, the acceptance sketch) and `design.md` CS1–CS14 (the rulings, plus the
"Verified facts" read from the real code on 2026-10-30). Read both first. Short version: a **separate** live
accessor inside `internal/config` (the five layers untouched), over one new key/value table with an append-only
audit table, a **closed registry of declared keys** that gives the schema-less table its type safety, precedence
**environment → database → default**, five socket methods (`settings.list/get/set/unset/history`) that are the
human door and absent from the narrow/agent surface, and a generic descriptor-driven panel on the real Settings
screen. S44 ships the mechanism and **zero production settings**.

## How we will do it

Foundation in two parallel halves, then the join, then the doors, then the proof:

- **SQL half (T1)** and **pure-Go half (T2)** have no shared file and can run in parallel after T0: T1 is
  `internal/store` + a migration file; T2 is the registry in `internal/config` with no database at all.
- **T3** joins them: `config.Store` over a `Repo` seam, exercised first against an in-memory fake (fast, hermetic)
  and then against the real store in T4's adapter test.
- **T4** is the daemon wiring (adapter + one shared instance). **T5** is the five socket methods, errors,
  fixtures and `protogen`. **T6** is the guards that make the human-door and "adds, does not edit" rules
  mechanical. **T7** is the CLI client; **T8** is the UI; both depend on T5 and are parallel to each other.
- **T9** closes out: re-run every acceptance criterion fresh, walk the demo, update the record.

This mirrors S33's foundation-then-seam-then-reads-then-close-out order and S43's investigation-first T0.
Because this slice ships a mechanism with no production consumer, **every test that needs a key declares its own
test registry** (the `Store` constructor takes the registry as a parameter — CS3), so nothing in this slice's tests
depends on S43 existing.

## Open questions this plan answers

The brief's five, now answered in `design.md`:

| Brief question | Answer | Where |
| --- | --- | --- |
| Q1 sixth layer or separate accessor? | Separate; five layers byte-pinned untouched | CS1, T3 guard |
| Q2 env/flag beat the DB value? | Env > DB > default, no flags; shadowed writes succeed but say so | CS4 |
| Q3 KV or typed columns? | KV table + closed registry; typing in the write boundary | CS3 |
| Q4 method shape and seam? | Five `settings.*` methods; local-interface seam + one shared `*config.Store` | CS10, CS11 |
| Q5 scope? | Global only, no scope column | CS6 |

The T0 items (design "Open items left to T0") are the only questions still open, and none is a product call.

## Task sequence

### T0 — Re-verify the facts, settle the seven mechanical items (no code) ([`tasks/T0.md`](tasks/T0.md))

Re-walk design.md facts 1–13 against the tree as it is when T0 runs (other worktrees move it), pin the hashes
CS1's guard needs, and answer the seven open items. Records drift in its own Notes; does not silently fix the
design. **Depends: nothing (first).**

### T1 — The tables and the store accessors ([`tasks/T1.md`](tasks/T1.md))

One timestamped migration file creating `setting` and `setting_event` (with the two append-only triggers), and
`internal/store/settings.go` — the SQL half only: `SettingGet`, `SettingList`, `SettingPut`, `SettingDelete`,
`SettingEventList`. Put and Delete each write their audit row in the same transaction. **Depends: T0.**

### T2 — The key registry and typed validation ([`tasks/T2.md`](tasks/T2.md))

`internal/config/settings.go`: `Setting`, `SettingType`, `Registry`, `mustDeclare` (all its panics), canonical-form
parse/format per type, env-name derivation, nearest-key suggestion. Pure Go, no database. **Depends: T0**
(parallel-safe with T1 — no shared file).

### T3 — `config.Store`: live reads, the one write boundary, precedence ([`tasks/T3.md`](tasks/T3.md))

`internal/config/settings_store.go`: the `Repo` seam, `Store` (`Get`, `List`, `Set`, `Unset`, `History`),
`Reader` and its typed helpers, the three-part errors, the CS4 precedence with `source`/`shadowedBy`/`envInvalid`/
`storedInvalid`, and the CS1 byte-pin guard. Tested against an in-memory fake `Repo`. **Depends: T1, T2.**

### T4 — Daemon wiring: the adapter and the one shared instance ([`tasks/T4.md`](tasks/T4.md))

`internal/daemon/settings_repo.go` (`config.Repo` over `*store.Store`), an exported constructor, the single
instance held at serve time and given to both the protocol seam and (later) in-process consumers; the
two-handle live-read test and the cross-process test against the real store. Also settles CS12's event if cheap.
**Depends: T3.**

### T5 — The five socket methods ([`tasks/T5.md`](tasks/T5.md))

`internal/protocol/settings.go` (local `SettingsService` interface, params/results, handlers, two typed errors),
registration in `NewTable`, `Table.Settings` field wired in the daemon, conformance cases and fixtures, the
handshake want-list, `make protogen`. **Depends: T4.**

### T6 — The guards ([`tasks/T6.md`](tasks/T6.md))

`TestNoSettingsWriteReachesTheNarrowSurface` (cmd/aeon), the "only `config` and the daemon adapter touch the
`setting` table" grep guard, the P34 grep for the UI (wired in T8, asserted here as a script both run), and the
layers-untouched hash guard promoted into CI. **Depends: T5.**

### T7 — The CLI client ([`tasks/T7.md`](tasks/T7.md))

`aeon settings list|get|set|unset|history` in `cmd/aeon/settings.go` — a pure socket client in the `telemetry`
verb's mould, no store opening, no second validation implementation. **Depends: T5** (parallel-safe with T8).

### T8 — The Settings UI panel ([`tasks/T8.md`](tasks/T8.md))

The generic "Daemon settings" panel on `SettingsScreen.tsx`: descriptor-driven controls, source badges, env lock,
reset, sensitive confirmation, empty state, en/pt strings, vitest. **Depends: T5, T6** (parallel-safe with T7).

### T9 — Close-out ([`tasks/T9.md`](tasks/T9.md))

Re-run the ten acceptance criteria fresh, the migration drill, the DEMO §S44 walk, the cumulative demo, the
record (SLICES.md, task Notes, CONFIGURATION-ORDER.md, the AGENTS.md rule). **Depends: T0–T8.**

### T10 — Adopt S43's unsigned-import setting ([`tasks/T10.md`](tasks/T10.md))

**Not part of the mechanism's close-out.** After S43 has merged to `main`, investigate how S43 actually implemented
`agents.allowUnsignedImport`, then — in S44's own change, on S44's branch — declare it in the registry and move
S43's read site (and any bespoke write path) onto `config.Reader` / `settings.*`, preserving S43's observable
behaviour and tests. **Depends: T9 and S43 merged.** If S43 never merges or never builds the setting, T10 is
closed "not applicable" with the reason; S44's mechanism is unaffected.

## Sequencing summary

```
T0 (re-verify, mechanical items)
  ├─▶ T1 (tables + store SQL) ─┐
  └─▶ T2 (registry + typing) ──┴─▶ T3 (config.Store) ─▶ T4 (daemon wiring) ─▶ T5 (socket methods) ─┬─▶ T6 (guards) ─┐
                                                                                                     ├─▶ T7 (CLI)   ──┼─▶ T9 (close-out)
                                                                                                     └─▶ T8 (UI)  ◀───┘
T9 ─ and S43 merged ─▶ T10 (adopt S43's setting)
```

T1 ∥ T2. T7 ∥ T8 (T8 also waits for T6's script). Everything else is a chain.

## The numbers this slice must hold

| Fact | Count |
| --- | --- |
| New migration files | 1 (`<timestamp>_settings.sql`) |
| New tables | 2 (`setting`, `setting_event`) |
| New socket methods | 5 |
| New typed protocol errors | 2 (`ErrSettingUnknown`, `ErrSettingInvalidValue`) |
| New production setting declarations | **0** through T9; **1** after T10 (`agents.allowUnsignedImport`, only if S43 built it) |
| Existing `internal/config` files edited | **0** (hash-pinned) |
| New `internal/config` files | 4 source (`settings.go`, `settings_store.go`, `settings_env.go`, `settings_errors.go`) + tests |

## Cross-slice coupling — S43 is being built and is not changed by this slice's planning

**S43 is read-only to this slice.** It is being built by another agent; nothing in S44's planning, tasks T0–T9, or
close-out edits an S43 file, asks S43 to wait, or asks S43 to declare anything. S43 will build
`agents.allowUnsignedImport` however its own T0/T5 decide (a `component` column, a daemon flag, an env read, a
hardcoded default — unknown today; `grep -rn allowUnsignedImport aeon` on `main` finds nothing yet). S44 therefore
**ships the mechanism with zero production settings (T0–T9) and stays independent of S43 in both directions.**

Any change to S43's code — moving its setting onto this store, switching its read site to `config.Reader`,
retiring a bespoke write path — **is S44 work, done as S44's own task after S43 has merged:**
[`T10 — Adopt S43's unsigned-import setting`](tasks/T10.md). Until then S43's setting works exactly as S43 built it.

- **CONFIGURATION-ORDER.md Tier 0.5** names this slice; T9 updates its "brief only" wording (that file is not an S43 file).
- Later consumers (policy defaults, MCP defaults, automation defaults) each adopt the mechanism in their own slice.
