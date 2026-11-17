# Plan — S50 Roadmap management

Shapes from [`MANAGEMENT-PATTERNS.md`](../MANAGEMENT-PATTERNS.md). Short version: complete the roadmap's management (edit, history/audit, board with details, vocabulary, health), turn the two code-defined dispatch choices into a seeded blueprint record and two S44 settings, keep dispatch a human act, give agents reads and a propose-an-idea, extend the CLI and the real roadmap screen.

### T0 — Re-verify, inventories, parity, mechanical items (P1) ([`tasks/T0.md`](tasks/T0.md)) — **Depends: nothing.**
### T1 — `roadmap_change` audit, store accessors, actor threading through the existing writes ([`tasks/T1.md`](tasks/T1.md)) — **Depends: T0.**
### T2 — `roadmap.update`, the plan-host blueprint record, board/history/vocabulary reads, `Describe` ([`tasks/T2.md`](tasks/T2.md)) — **Depends: T1** (record half: S48 T5, S43-extra T3).
### T3 — The two blueprint settings, health, dispatch reading them ([`tasks/T3.md`](tasks/T3.md)) — **Depends: T2, S44, S48.**
### T4 — Guards (P4) ([`tasks/T4.md`](tasks/T4.md)) — **Depends: T2, T3.** Gates T5–T7.
### T5 — Agent tools and the `roadmap_idea` proposal kind (P6) ([`tasks/T5.md`](tasks/T5.md)) — **Depends: T4, S45 T1/T5.**
### T6 — The CLI (P5) ([`tasks/T6.md`](tasks/T6.md)) — **Depends: T2, T3, T4.** Parallel with T5, T7.
### T7 — The roadmap screen (P7) and close-out (P9) ([`tasks/T7.md`](tasks/T7.md)) — **Depends: T2–T6 and S45 T9.**

```
T0 ─▶ T1 (audit) ─▶ T2 (update, record, reads) ─▶ T3 (settings, health) ─▶ T4 (guards) ─┬─▶ T5 (tools, proposal kind) ─┐
                                                                                      ├─▶ T6 (CLI) ─────────────────┼─▶ T7 (UI + close-out)
                                                                                      └──────────────────────────────┘
```

**Numbers:** 2 migrations (`roadmap_change`; `proposal` kind rebuild); 1 table; 5 reads (`board/history/health/vocabulary` + the existing list/show kept) and 1 new write (`roadmap.update`); 3 agent tools (2 reads, 1 propose) granted to 0 by default; 2 settings; 1 seeded blueprint record moved; 0 lifecycle edges added; 0 existing write signatures changed beyond actor.
