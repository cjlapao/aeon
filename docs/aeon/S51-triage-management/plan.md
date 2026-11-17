# Plan — S51 Triage management

### T0 — Re-verify, inventories, parity, mechanical items (P1) — Depends: nothing. Also: the writer inventory of `bug`; the exact `triage.file` params (any `filedBy`?); how agents file today; the D5/N3 and D1/D2 guard tests + hashes; the distinct `filed_by` values vs `agent.name`; the validate dispatch and its blueprint const; S43/S44/S45/S47/S48/S50 state; parity (8/8 + gaps); baseline.
### T1 — `bug_change`, `filed_by` slug migration, actor+audit threading ([`tasks/T1.md`](tasks/T1.md)) — Depends: T0, S43.
### T2 — `triage.update`, `Describe`, board/history/vocabulary reads ([`tasks/T2.md`](tasks/T2.md)) — Depends: T1.
### T3 — The validation-blueprint setting, health, dispatch ([`tasks/T3.md`](tasks/T3.md)) — Depends: T2, S44, S48.
### T4 — Guards (P4) incl. D5/N3 across agent doors ([`tasks/T4.md`](tasks/T4.md)) — Depends: T2, T3. Gates T5–T7.
### T5 — Agent tools: `bug_list`, `bug_show`, `bug_file` (P6, no proposal kind) ([`tasks/T5.md`](tasks/T5.md)) — Depends: T4.
### T6 — The CLI (P5) ([`tasks/T6.md`](tasks/T6.md)) — Depends: T2–T4.
### T7 — Settings → Triage and close-out (P7, P9) ([`tasks/T7.md`](tasks/T7.md)) — Depends: T2–T6, S45 T9.

```
T0 ─▶ T1 ─▶ T2 ─▶ T3 ─▶ T4 ─┬─▶ T5 ─┐
                            ├─▶ T6 ─┼─▶ T7
                            └───────┘
```
**Numbers:** 1 migration pair (`bug_change` + the slug migration as one file or two — T0 decides) ; 1 table; 4 reads + 1 write new; 3 agent tools (1 direct inert write) granted to 0 by default; 1 setting; 0 new states/edges; 0 agent-reachable priority/duplicate paths.
