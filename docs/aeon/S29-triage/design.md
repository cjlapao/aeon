# Design — S29 Triage

**Brief:** [`brief.md`](brief.md). **Plan:** [`plan.md`](plan.md). **Demo:** [`DEMO.md`](DEMO.md).

**Written 2026-10-26**, against S24's and S26's own just-finished designs (both now real enough to ground
Q3/Q4 against, rather than guess), and S28's `design.md` (D10's feed hook, already resolved there against
this slice's own words).

## 1. Context

Three questions remain open after Q1's resolution (D10). All three turn out to be answerable by reading
S24's and S26's actual shapes rather than inventing new ones — this slice's own §3 claim ("cheaper than it
looks") holds for its design work too, not only its build.

## 2. Decisions

### T1 (brief Q2) — the status vocabulary is two separate closed sets, not one crossed product

**Q2's own phrasing half-answers itself incorrectly** — it asks whether validation state is "crossed with"
priority, but D2 already locked the opposite: *"Priority and validation stay separate fields."* There is no
combined enum to design. Two closed sets:

**Validation** (D1): `unvalidated` (no attempt has run) · `reproduced` · `not-reproduced` · `could-not-tell`
— four values, `unvalidated` never collapsing into `could-not-tell`.

**Priority**: `priority_state` (`known` / `assumed` / `unknown`) + `priority_label` (nullable, meaningful
only when `priority_state != 'unknown'`) — **the identical shape `S26-roadmap`'s `roadmap_item` already
uses** for its own priority split, not a coincidence: D2 explicitly names itself as the constraint S26 must
satisfy, so matching S26's actual column shape means the hand-off (T4, below) copies two fields verbatim,
no translation step to get wrong.

### T2 (brief Q3) — validation attempts trigger automatically; no gate needed, unlike S24's

**Not the same answer as S24's `queueAdvance`, and worth saying why.** S24's queue defaulted to `ask`
(R4 there) because a materialized queue item starts a loop that may do real, potentially-irreversible work.
**A validation attempt cannot** — it is an ordinary S5 loop under G14's attendedness pin (D4), and an
unattended loop already denies `outward`/`irreversible` actions structurally (S16's D8, S11's ask-denies-at-
once rule — the same mechanism `S27-continuous/brief.md` leaned on for its own scheduler safety argument).
**A validation attempt is read-and-report by construction**, bounded the same way S24's sizing run is
(brief §3: "nothing about running that loop is new"). There is nothing a gate would be protecting against
that G14 does not already block. **Resolution**: filing a bug (self-filed or human-reported) immediately
dispatches a validation attempt, automatically, no project setting required. A project that wants to review
before validating can still decline to configure self-filing (D7 is permitted, not mandatory) — that is a
coarser, existing control, not a second gate this slice adds.

### T3 (brief Q4) — hand-off targets S26's roadmap exclusively, through its ordinary `idea` entry point

**Not "S24's queue or S26's roadmap," despite D8's own either/or wording** — checked against both slices'
actual designs: `S26-roadmap/design.md` R4 already prefers S24's queue when a project has one, falling back
to a direct `loop.start` otherwise. **S26 already is the single sink that knows how to reach S24's queue
when appropriate.** Handing a triaged bug to S24 directly would duplicate that routing decision in a second
place. **This slice hands off to S26 only**, via `S26-roadmap/design.md`'s own `D5` `triaged` provenance —
written there in anticipation of exactly this hand-off, quoted: *"`triaged` (S29's hand-off, D8 there)."*

**The item starts at S26's ordinary `idea` status — no new starting state, no amendment to S26.** A tempting
shortcut was considered and rejected: creating the roadmap item already at `brief-agreed`, skipping
ideation, since a validated bug report already contains what a brief needs. That would require S26's
`Create` to accept a non-default starting status — a real capability S26's own design does not currently
expose, and adding it would be exactly the kind of cross-slice amendment this project tries to name rather
than smuggle in. **Instead**: the hand-off creates an ordinary `idea` item and immediately starts ideation
(S26's existing `idea → ideating` path, T3 there) with the validated bug record as the ideation loop's
starting context — the ideation step transcribes/lightly edits the existing report into `brief.md` rather
than holding a real back-and-forth conversation, satisfying S26's own `produces: [brief.md]` check quickly
rather than specially. **Zero lines change in S26.**

## 3. Schema shape (T1's job in `plan.md` to make literal)

**`bug`**: `id`, `project_root` (`NOT NULL`), `title`, `body` (`TEXT NOT NULL` — the report itself),
`validation_state` (`TEXT NOT NULL DEFAULT 'unvalidated' CHECK (validation_state IN ('unvalidated',
'reproduced','not-reproduced','could-not-tell'))` — D1's four), `priority_state` (`TEXT NOT NULL DEFAULT
'unknown' CHECK (priority_state IN ('known','assumed','unknown'))`), `priority_label` (`TEXT`, nullable —
both mirroring S26's shape, §2 T1), `filed_by` (`TEXT NOT NULL` — an agent name or `'person'`, D7's
self-filing made literal), `duplicate_of` (`INTEGER`, nullable, self-referencing FK — set only by a human
confirming D5's suggestion, never by the detector), `roadmap_item_id` (`INTEGER`, nullable — set once D8's
hand-off creates the S26 item), `created_at`, `updated_at`.

**`bug_vec`**: a `vec0` virtual table (sqlite-vec), one row per `bug.id`, project-scoped (D6) — **the
second `vec0` table in this schema**, after S28's `learning_vec` (§2 of `S28-memory/design.md`). Confirmed
not a third corpus inside either S22's or S28's own index (N5), a structurally separate table entirely, the
same reasoning S28 used against S22 applied a second time.

**`file_bug` native tool**: the mechanism D7 implies but does not name — an agent cannot self-file a claim
without a tool to call. One new native tool, the same shape S24's `report_size` established (one call, known
schema: `{title, body, priorityLabel?}` — never `validationState` or `priorityState`, both of which the
engine sets, not the caller, so a self-filed claim cannot arrive pre-labeled `reproduced` by a confident
model).

## 4. Architecture

```
  file_bug (native tool, agent)         human report (CLI/socket)
         │                                      │
         └──────────────┬───────────────────────┘
                         ▼
                  bug: unvalidated
                         │
                 (T2 — automatic, always)
                         ▼
          validation attempt (ordinary S5 loop,
          G14-pinned, bounded like any other step)
                         │
                         ▼
         bug: reproduced | not-reproduced | could-not-tell
                         │
              ┌──────────┴──────────┐
              ▼                     ▼
      D10: filed to S28       priority set (known/assumed/
      as an `observed`        unknown + label), duplicate
      learning, automatic,    checked against bug_vec (D5:
      gated by S28's Q3       suggest only, human confirms)
                                     │
                            (once validated AND prioritized)
                                     ▼
                     T3: hand off to S26, provenance='triaged',
                     created at `idea`, ideation transcribes
                     the report into brief.md (fast, not special)
```

## 5. Testing this slice has to do

- **D1 as a property**: `unvalidated` and `could-not-tell` render distinctly everywhere a status appears —
  the same discipline R4's risk names, tested directly rather than hoped for.
- **T2 directly**: filing a bug (via `file_bug` or the human path) results in a validation-attempt loop
  starting with no explicit trigger call — the automatic-dispatch property, not merely documented.
- **D5/N3 as a property**: no code path in this slice can set `duplicate_of` without a human-originated call
  — asserted by checking every write site, the same "prove it by what's not there" discipline used
  elsewhere in this plan.
- **T3's hand-off**: a validated, prioritized bug produces exactly one new S26 `roadmap_item`, `provenance:
  'triaged'`, starting at `idea` — never `brief-agreed`, confirmed by a direct status check immediately
  after creation.
- **`file_bug`'s known shape**: the tool's result never includes `validationState` or `priorityState` keys
  — grep/schema-asserted, so a crafted call cannot pre-label a claim as already reproduced.
- **D6/N5 by absence**: `bug_vec` shares no table, no row, no kind-discriminator column with
  `learning_vec` or S22's corpora — asserted structurally, not just claimed in prose.

## 6. Amendments to earlier slices

**None.** T3 was specifically designed to require zero changes to `S26-roadmap` — the alternative
(S26 gaining a non-default starting status) was considered and rejected in §2 precisely to avoid becoming
this plan's next cross-slice amendment. S28's D10 already anticipated this slice's feed hook; nothing here
asks S28 to change either.

## 7. Open questions this design did not close

- **Whether `file_bug` needs a rate limit or cost-cap default distinct from other tools**, given D7's
  self-filing could in principle be called often by an unattended agent — likely covered by the project's
  existing `CostCap` policy type (S10) rather than a bespoke limit, but that is a measurement for whoever
  builds T1's tool, not a modelling question settled here.
