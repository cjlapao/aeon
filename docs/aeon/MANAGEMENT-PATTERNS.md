# Management patterns — the recurring task shapes of the S43–S54 sequence

Written 2026-10-30, extracted from S43-extra, S45–S49 after they were planned. [`CONFIGURATION-ORDER.md`](CONFIGURATION-ORDER.md) holds the **standing rules**; this file holds the **task shapes** that
implement them, so a slice's task file can say "the P5 shape, with these specifics" and a builder knows exactly what that means. A slice may deviate **only** by recording why in its design. S50–S54 cite this file by section.

## P1 — The investigation task (T0): no code, an inventory, a baseline

Every slice opens with a T0 that produces **only Notes**: (a) re-walk each fact in the design against the tree as it is today and record *same / drifted*; (b) an **inventory** of every reader/writer the slice will touch, file:line, daemon vs CLI/offline
context; (c) the **CLI↔socket parity table** (every verb × flag → socket method or GAP, with the closing task); (d) the neighbour slices' merge state, **read-only**; (e) the next free **error-code block**, the handshake/conformance/fixture/`protogen` touch points, trigger-body support in `.sql` migrations
(S44 T0 3.1); (f) a **baseline** `go test` of the affected packages with every red named. T0 never edits a design, brief or another slice's file.

## P2 — Characterize, then move

Any task that changes *where* something is read from (file → row, code → row, name → slug) is **characterization-first**: Phase A commits golden tests (bytes of CLI output and wire results, store state) green on the **untouched** code; Phase B changes the source and the goldens must stay
byte-identical, with each *intentional* difference listed in Notes with its golden diff. No fallback to the old source in a production path (two truths); an offline-tool path is named and caller-pinned (`UseEmbeddedForOfflineTools` shape).

## P3 — Rows, revisions, audit

A managed record is a row. A change worth attributing writes an **append-only audit/revision** row **in the same transaction** as the write — one per *changing* write, none for reads, refusals, dry-runs or no-ops — with the actor from `table.ActorFor` (kernel peer), never from params. Tables are
append-only **by trigger** (fallback: no update/delete accessor + a package grep test). Summaries are **fixed templates from ids and enums**, never raw user text (SEC-033). Revision numbers never reset. History outlives the record (no FK from history to the record).

## P4 — The human-door guard amendment (sibling tests, canaries, hash pins)

Writes of a guard-affecting domain are socket methods on the person's 0600 + peer-credential surface and are **absent** from: the narrow surface (`narrow.Actions()`, `BoundaryStatement()`), agent services, the tool registry, the records catalog (except a named propose twin), and any non-test file under
`internal/narrow`, `internal/agentsvc`, `internal/tools`, `internal/records` (literal and constant grep). The task adds **sibling** guard tests in `cmd/aeon` — **never edits an existing guard body**; the pre-amendment logic is pinned by SHA-256 recorded in T0 — with a positive half first (the methods *are* registered, `RequiresInitialize`, not `Debug`, absent when the service is nil — V13) and a
**canary** (`…GuardsHaveTeeth`) feeding each negative predicate a synthetic violation. Walks assert they saw ≥ 50 files. Where S47 has merged, a guard asserts each method's surface classification. The rule text goes into `aeon/AGENTS.md` §4 (one row) at close-out.

## P5 — The CLI as a client, with an offline fallback

Every verb is a client of a socket method (**parity is a test in both directions**, fixture = T0's table). `socketDoor` is the default; `directDoor` builds the **same** adapter/write service over a directly opened store (actor `{cli,cli}`, audited `actor_kind='cli'`); `selectDoor()` dials once —
"no daemon" ⇒ direct door + the fixed stderr line `daemon not running — applied directly to the store`; **any other dial failure refuses**. `directDoor` may not contain a copy of logic (`go/parser` test); `internal/store` is imported only there. The CLI **re-validates nothing**: flag parsing refuses only what cannot be a value, everything else is
the daemon's sentence verbatim; `--json` is the wire result verbatim. `--yes` is a **seatbelt**, not a security control, required where a verb grants authority or is hard to reverse. No flag, stdin or prompt accepts a secret value (C17). Existing verbs keep flags and output (goldens); intentional changes are listed.

## P6 — Agent tools: one definition, two doors

Catalog specs in `internal/records/catalog.go` (family per domain) with socket twins; `registerRecords` skips a twin whose dot-name already stands. **Reads** are open (or own-view where the domain has a privacy rule — MC2). **Writes are propose-only and restrictive-only** where the domain widens a guard: an **inert proposal** of a new
closed kind created through the `proposal.create` path (`desk.Validate` at create — S35 TD3 — so nothing inert-and-invalid is persisted), committed by a person through S45's generic `proposal.commit` with the **committer** as actor; a new kind is a `proposal.kind` CHECK **table rebuild** (S37/S46/S48/S49 precedent — read the **current** DDL, change only the CHECK, update the Go mirror,
lock test and the literal migration-count assertions). Schemas are `additionalProperties:false` with **no** identity/status/source/kind fields; the daemon stamps the agent identity. Registered-not-granted; the `TestRegistryExactNames` bump is **counted and dated**; descriptions ≥ 40 chars; per-agent open-proposal limit (S45's constant).

## P7 — The Settings panel (descriptor-driven, P34-guarded)

A panel on the real Settings screen (not the bench): lists with details and daemon-side filters; forms generated from a daemon `vocabulary`/descriptor read (no closed set, grammar, sentence or authority text typed in the UI); **refusals shown verbatim with the draft kept**; **authority-granting actions confirm with the daemon's own statement**
(`useConfirm`; declining sends nothing); rows updated **from the response**, never optimistically; offline ⇒ writes disabled, reads show last data; en/pt parity; no new dependency; no `localStorage` for domain data; no `fetch`; existing panels and bench pages untouched. A `*Rpc.guard.test.ts` extracts every literal passed to the rpc helpers and asserts each is a registered method in the generated
`contract.ts`, found ≥ 1 per tab (never vacuous), with a canary that flags a bogus method. A source grep test forbids hardcoded vocabulary literals.

## P8 — The seed-set declaration (rule 6)

A domain that ships system records declares `internal/seed/decl_<area>.go` + a domain `…seed` package: `Source() []seed.Record` (content verbatim, per-record `Version` guarded by a golden — "content changed but version did not — bump it"), an `Applier` (`Upsert/Fingerprint/Retire/GuardAffecting/Operator`) writing through the domain's **one write path**, class **inert** unless it widens a guard (then **guard**: never at startup), `origin` column on the table,
operator rights stated (`{Edit, Remove}`), and tests: provenance, idempotence, divergence never overwritten, removal never resurrected, restore explicit, startup never reaches a guard set. **Exempt** domains say so in their design (authority records: triggers — S49 AN2).

## P9 — The close-out task

Re-run every acceptance criterion **fresh** (evidence table: criterion → test names and walk steps); the **migration drills** (fresh apply; upgrade from the previous vintage with row-count/checksum preservation; idempotent re-open; append-only proof; rollback not offered); the **demo walk** in the real app against a real daemon (a table of steps with expectations, the build command, date and lane recorded; "not run — reason" where not run; a two-window
staleness finding); then — **after** the evidence — SLICES.md (Built only if every task is accounted for, else Partial), each task's Notes, the brief's handoff, CONFIGURATION-ORDER status, DEMO.md and the cumulative §4b, and the `aeon/AGENTS.md` row. Full-suite reds are compared to T0's baseline and each proved pre-existing; `make fmt-check` is known red at base (record, never reformat untouched files).

## P10 — The standard command block (every task)

```bash
cd aeon
ROOT="$(git rev-parse --show-toplevel)"
export GOPRIVATE='github.com/cjlapao/*' CGO_ENABLED=1 CGO_CFLAGS="-I$ROOT/.tmp/cgo-shim" XDG_STATE_HOME="$(mktemp -d)"
go test ./<packages> -run '<names>' -count=1 -race -v
make protogen && make check-protogen && make guard      # whenever the wire changes
cd ui && npx vitest run src/features/<area> && npm run lint && make check-deps
```

## P11 — The common "Do NOT" list

Do not edit `schema.go` or a shipped migration (timestamped file, `date +%Y%m%d%H%M%S`); do not edit an existing guard-test body or another slice's file beyond a **listed** adaptation; do not accept `actor`/identity/origin in params; do not stamp a placeholder actor; do not put user text in an audit summary; do not cache (N8); do not add a fallback to an old source; do not hardcode a vocabulary or sentence in a client; do not print or store a credential value; do not add a dependency;
do not edit `contract.ts` by hand; do not soften a failed or skipped check.
