# Plan — S53 History views

### T0 — Re-verify, inventories, parity, mechanical items (P1) — Depends: nothing. Specifics: the `internal/history` entry points and how archived rows are decoded (and what a decode failure does today); the P32 floor sentence source; where the bench History tab lives (`grep -rn "history\." aeon/ui/src`); S38's project/component list methods; parity (3 verbs + gaps coverage/vocabulary); neighbour state (S45 T9, S47); error block; baseline `go test ./internal/history/ ./internal/store/ ./internal/protocol/ ./internal/daemon/ ./cmd/aeon/ -count=1`.
### T1 — `Describe`, vocabulary, `history.coverage` ([`tasks/T1.md`](tasks/T1.md)) — Depends: T0.
### T2 — Guards: read-only, export person-only, classification, canaries (P4) ([`tasks/T2.md`](tasks/T2.md)) — Depends: T1. Gates T3–T5.
### T3 — Agent tools `history_list/show` (P6) ([`tasks/T3.md`](tasks/T3.md)) — Depends: T2.
### T4 — CLI (P5) ([`tasks/T4.md`](tasks/T4.md)) — Depends: T1, T2.
### T5 — The History screen and close-out (P7, P9) ([`tasks/T5.md`](tasks/T5.md)) — Depends: T1–T4, S45 T9.
**Numbers:** 0 migrations, 0 tables, 0 writes; 2 new reads; 2 agent tools (reads) granted to 0 by default.
