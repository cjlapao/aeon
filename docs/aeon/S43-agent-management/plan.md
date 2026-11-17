# Plan — S43 Agent management

## What is settled before planning

`brief.md` and `design.md`'s AM1–AM5. Read both before this. The short version: every agent becomes a
database row, keyed by an immutable, unique `slug` that replaces every current by-name reference across
policy, blueprints, tasks, chains and handler dispatch; the card gains version + signature + the new
display/avatar fields; a Settings page (the real app, not a bench tab) gets CRUD + duplicate + view-content,
all over daemon methods.

## How we will do it

Foundation first (T0–T3): investigate every real call site, land the new table(s) via the timestamped
migration convention (`internal/store/migrations/README.md` — never a `schema.go` edit), migrate existing
name references to slug. Then the card (T4–T5): expand the format, build import with versioning and
signature verification. Then the daemon surface and UI (T6–T8). Then close-out (T9).

This mirrors how S32 sequenced its own foundation-then-feature tasks (T1–T4 primitives, T5 the big feature)
and how S37 opened with a pure-investigation T0.

## Task sequence

### T0 — Investigation: every by-name agent reference, real names and line numbers (no code)

Confirm, file by file, against the grep list in `design.md` AM2: which references are real (an agent's
name used as a lookup/foreign key in all but name) vs incidental (a variable named `agent` that holds
something else, a comment). For each real one, record: the file, the exact field/column, whether it is
read-only or also written, and what a slug migration there would touch. Also confirm: `plugin_agent`'s
exact current migration number and column list (`VERIFY`, it moves); `internal/plugin.Agent`/`CardInfo`'s
exact current fields (re-read `import.go`, this design's own citations may have drifted by the time this
runs); how blueprints are persisted (files, or rows — needed for AM4's deletion check and for deciding
whether blueprint authors type a slug directly); the current slug/id convention this project's *other*
tables use, if any (so the new agent table's PK shape matches house style). Answer `brief.md` §5's five
questions, either by asking the operator directly or by making a documented call the way `design.md` did.

**Depends: nothing (first).**

### T1 — The agent content table, and the slug column everywhere else needs it

The new table (name/shape from T0's findings), via a timestamped `internal/store/migrations/*.sql` file —
`id, slug (unique, immutable), name, description, functionality, metadata (json), version, internal (bool),
avatar_type, avatar_color, avatar_mood, signature, signer_certificate, created_at, updated_at` (adjust to
T0's findings). Backfill: every first-party handler becomes a row (`internal = true`); every existing
`plugin_agent` row gets a slug generated from its `name` (collision policy per T0's answer to brief.md §5.3).
`plugin_agent`'s own schema and state machine are untouched — this table joins it by slug.

**Depends: T0.**

### T2 — The slug migration: policy targets

`internal/policy`'s `AgentKind` targets move from name to slug — the highest-blast-radius item named in
`design.md` AM2. Every existing policy row with an agent-kind target is backfilled (name→slug lookup against
T1's table); a row whose name matches nothing is a named refusal at migration time, never a silent drop.
Every PL1-era test that asserts a policy row's target by name needs re-grounding against slug — expect a
large, mechanical diff across `internal/policy`'s test suite; do not let its size hide a real behavioural
change.

**Depends: T1.**

### T3 — The slug migration: blueprints, tasks, agent-chain edges, handler dispatch

The remaining items from T0's list: `internal/blueprint/types.go`'s lane `Agent` field, task assignment,
`agent_chain` edges, `internal/handler`'s dispatch registry. Each gets the same treatment as T2: backfill
existing data, switch the live read/write path to slug, named refusal on no-match. Blueprint YAML's own
author-facing shape (raw slug vs. name-resolved-at-load) is T0's answer to brief.md §5.5 — build whichever
was decided.

**Depends: T1** (parallel-safe with T2 — different files, no shared touch point once T1's table exists).

### T4 — The card format: fields, version, idempotent import

Expand `CardInfo` (`internal/plugin/import.go:406`) with the new display/avatar/version fields from T1's
table shape. Import logic: `(slug, version)` idempotency — same version is a no-op, higher version updates
the matching row in place, lower version is a named refusal. No new row is ever created for an existing
slug; a genuinely new agent (unseen slug) creates one.

**Depends: T1.**

### T5 — Signature verification

The card carries a signature and an embedded certificate (format from T0's answer to brief.md §5.4). Import
verifies the signature matches the certificate's public key over the card's own bytes — proving
non-tampering only, no trust-root check (`design.md` AM3). A tampered card is refused, named. The UI (T7)
shows verified/tampered/unsigned plus the signer's claimed identity from the certificate.

**Depends: T4.**

### T6 — Duplication and deletion

Duplicate: copy every field of an existing row (internal or not) into a new row with a fresh GUID and a
freshly slug-generated-from-the-copy's-name — refused on collision, never silent-suffixed. `internal` is
always `false` on the copy, regardless of the source. Delete: refused if any blueprint **or any policy
row** (both, locked) references the slug — name every referencing row in the refusal. Edit: refused on
`internal = true` rows, named; otherwise writes any of the new fields except `slug` (AM2 — never changes
after creation) and `id`.

**Depends: T1, T2, T3** (deletion blocks on both a policy reference, built in T2, and a blueprint reference,
built in T3 — needs T3's slug-addressable blueprint/task/chain data to be
real, not the old name-based shape).

### T7 — The daemon methods and the Settings UI

New daemon methods for list/create/duplicate/edit/delete/view-content (seam shape per T0's answer to
brief.md §5.1's method-table convention question). The Settings page: list, create, duplicate, edit (with
the internal-row refusal visibly surfaced), delete (with the blueprint-reference refusal visibly surfaced),
view content read-only, using the avatar component (`ui/src/components/minion-avatar/MinionAvatar.tsx`) for
type/color/mood. This is the real app's Settings, not a bench tab — no bench-registry convention applies.

**Depends: T2, T3, T4, T5, T6** (the UI is the thin layer over everything else; build it last of the
functional tasks).

### T8 — Import/export CLI verb

`aeon agent export <slug>` / `aeon agent import <file>`, matching this project's "the daemon surface is
primary, the CLI is a secondary client" rule (P34) — the CLI calls the same daemon methods T7 built, never
a second implementation of import/signature logic.

**Depends: T7** (reuses its methods) — **or T5** if the CLI ships before the UI; `VERIFY` with the operator
which they want first; default to T7 unless told otherwise, since P34 wants the daemon surface first and a
CLI-only import would be the exact "capability that exists only as a CLI verb" P34 forbids as a finished
state.

### T9 — Close-out: the test map, the migration drills, the docs

Full acceptance-criteria re-confirmation (`brief.md` §6, all seven), each re-run fresh in this task, not
inherited. A migration drill proving the name→slug backfill (T2/T3) is safe to run twice (idempotent) and
refuses loudly on an unmatched name rather than dropping data. `STATUS.md`/`ROADMAP.md`/`SLICES.md` flipped
only after everything above is green, following the same discipline S34–S37's own close-out tasks used.

**Depends: T0–T8.**

## Sequencing summary

```
T0 (investigation)
  └─▶ T1 (table + backfill)
        ├─▶ T2 (policy slug migration)         ─┐
        ├─▶ T3 (blueprint/task/chain/handler)   ─┼─▶ T6 (duplicate/delete) ─┐
        └─▶ T4 (card format) ─▶ T5 (signature) ──────────────────────────────┴─▶ T7 (methods + UI) ─▶ T8 (CLI) ─▶ T9 (close-out)
```

T2 and T3 are parallel-safe with each other once T1 lands (different files — same collision-avoidance
reasoning as the three slices run in parallel earlier this session). T4/T5 are also parallel-safe with
T2/T3 — the card format doesn't touch policy or blueprints.
