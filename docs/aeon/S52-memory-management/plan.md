# Plan — S52 Memory management

### T0 — Re-verify, inventories, parity, mechanical items (P1) — Depends: nothing. Also: the full writer/reader inventory of `learning` (store, `Append`, `AppendRelayed`, board hook, triage feed, `Query`, prompt retrieval path, CLI); how learnings are indexed in S22 (corpus/`learning_vec`) and what a retraction must do there; the agent write door today (is `learnings.add` reachable by an agent? how do agents self-log?); distinct `scope_key` values vs `agent.name`; S28's M1/M3 tests + hashes; query cost behaviour; S43 agent state fields (archived/revoked); S45/S47 state; parity (4 verbs + gaps); baseline.
### T1 — Slug migration, retraction columns, `learning_change`, actor+audit, scope validation on agent rows ([`tasks/T1.md`](tasks/T1.md)) — Depends: T0, S43.
### T2 — `retract/restore`, index handling, `forAgent`, board/history/vocabulary, `Describe` ([`tasks/T2.md`](tasks/T2.md)) — Depends: T1.
### T3 — Guards (P4, provenance honesty, MC-style scoping) ([`tasks/T3.md`](tasks/T3.md)) — Depends: T2. Gates T4–T6.
### T4 — Agent tools and the `learning_retract` kind (P6) ([`tasks/T4.md`](tasks/T4.md)) — Depends: T3, S45 T1/T5.
### T5 — The CLI (P5) ([`tasks/T5.md`](tasks/T5.md)) — Depends: T2, T3.
### T6 — Settings → Memory and close-out (P7, P9) ([`tasks/T6.md`](tasks/T6.md)) — Depends: T2–T5, S45 T9.

```
T0 ─▶ T1 ─▶ T2 ─▶ T3 ─┬─▶ T4 ─┐
                      ├─▶ T5 ─┼─▶ T6
                      └───────┘
```
**Numbers:** 2 migrations (`learning` management; `proposal` kind rebuild); 1 table (`learning_change`); 3 columns; 2 human-door writes; 5 reads new; 4 agent tools (1 direct M3 write, 1 propose) granted to 0 by default; 0 provenance values added; 0 deletes.
