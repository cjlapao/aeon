# Plan — S28 Memory

**Brief:** [`brief.md`](brief.md). **Design:** [`design.md`](design.md). **Demo:** [`DEMO.md`](DEMO.md).

## What is settled

Everything in `design.md`'s §4 (M1–M6), plus its two corrections (§2, §3). In particular: **this slice's
code is named `learnings`, never `memory`** — `internal/memory` is already a different, real, built
mechanism (S20's per-loop notes, §2); agent-scope validation is a Go-level check, never a SQL foreign key
(§3); the depth/cycle bound on board questions is S14's own mechanism, reused wholesale, not reinvented
(M2); self-logged learnings get no review gate — provenance-honesty at retrieval is the safeguard (M3); the
board only ever invokes one narrow `answer` skill, never an agent's working skills (M4); tags are a join
table with a real index, not a JSON column (M5).

## How we will do it — updated 2026-10-26: S14 is built, all eight tasks are now free

**The learnings core (T1–T5) is buildable now.** It needs S4 (store, built) and nothing else.

**T6's scope grew once S14's real code could be read.** Declaring the `answer` skill in frontmatter is
necessary but not sufficient — `agentsvc.Invoker.Invoke` refuses any callee with no real signed `AgentCard`,
and none of S13's 25 bodies has one. T6 now issues real, self-signed cards alongside the frontmatter
declaration (card signing is a plain Ed25519 signature, no external ceremony — see T6's own Notes).

**T7 (board dispatch) was blocked on S14; it no longer is.** S14's real API (`agentsvc.Invoker.Invoke`,
`ChainDepthBound = 4`) is checked and cited directly in T7's own spec, not guessed at. Its one real
prerequisite is now T6's card-issuance piece, not an unbuilt slice.

**T8 (demo) can now walk the whole slice**, including the board, once T1–T7 land — no longer predicted-only
by construction, though whoever runs it should still confirm T6's `SkillAccess` policy rows (not authored
by any task in this plan, deliberately — see T6's Notes) have actually been granted before expecting a
board question to succeed end to end.

## Task sequence

| Task | What | Depends on | Buildable now? |
| --- | --- | --- | --- |
| **T1** | Migration: `learning`, `learning_tag`, `learning_vec` (the first `vec0` table in this schema — §2's second correction notes the precedent gap). | — | Yes |
| **T2** | `internal/learnings`: `Append`/`Query`, Go-level scope validation (§3), tag filtering (D5), provenance/strength carried on every result (D3/M3). | T1 | Yes |
| **T3** | Embedding integration: `learning_vec` population where a route is configured, ranking within the tag-filtered set only, never choosing it (D6/M5). | T2 | Yes |
| **T4** | `aeon learnings` CLI and `learnings.*` socket methods (learnings core only — append, query, list by scope). | T2 | Yes |
| **T5** | Test-bench UI: a learnings page, provenance/strength rendered honestly (never collapsed), tag filter visible and usable with zero embedding route configured. | T4 | Yes |
| **T6** | The `answer` skill (M4): frontmatter declaration **and** a real signed `AgentCard` for all 25 of S13's handler bodies — scope grew once S14's real API could be checked. | T2 | Yes |
| **T7** | Board dispatch: a directed question is a real `agentsvc.Invoker.Invoke` call targeting `answer`; on completion, the answer is `Append`-ed with `provenance: 'relayed'` (M1). | T6 | **Yes — S14 is built** |
| **T8** | Demo: append this slice's section to `docs/aeon/DEMO.md`. Every step, including the board, can be walked for real. | T1–T7 | Yes |

## Files this plan expects to touch or create

| File | Task |
| --- | --- |
| `internal/store/schema.go`, `schema_test.go` | T1 |
| `internal/learnings/*.go` (new package — deliberately not `internal/memory`, §2 of `design.md`: that name already belongs to S20's `AppendMemory`/`ReadMemory`, a different, built mechanism) | T2, T3 |
| `cmd/aeon/learnings.go`, `internal/protocol/learnings.go` | T4 |
| `ui/src/features/test-bench/pages/LearningsPage.tsx`, `routes.ts` | T5 |
| `internal/handler/bodies/*.md` (all 25), `internal/handler/cards.go` | T6 |
| `internal/daemon/learnings_board.go` (or similar — T7 names the real file) | T7 |
| `docs/aeon/DEMO.md` (§S28 appended) | T8 |

## Ordering rationale

T1 → T2 is the one hard dependency for the learnings core. T3, T4 and T6 are all independent of each other
once T2 exists — three engineers could take them in parallel with zero file collision. T5 depends on T4
(the UI reads through the socket). T7 depends on T6 (needs both the skill declared and the card issued —
no unbuilt slice in the way anymore). T8 is last and, as of 2026-10-26, can close out completely — the
whole slice, board included, is walkable in one pass.

## What this slice does not do

Everything in `brief.md` §6 (N1–N6) stands. No change to S22's corpora, no open/broadcast board, no new
native tool or transport, no change to G14 or the A1 amendment, project memory stays per-root, and no
board answer is ever a mechanism for handing real work to another agent.
