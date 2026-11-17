# Plan — S54 Platform status

### T0 — Re-verify, inventories, parity, mechanical items (P1) — Depends: nothing. Specifics: `retrieval.status`/`reindex`/`graph.*`/`telemetry.workspace` seams and param shapes; the single-flight trackers and their records; where the embedding price/token estimate can come from; the constants/knobs inventory (SP5); the per-component visibility model for `index_status` (SP4); build variants (retrieval/no-retrieval, graph) and how `unwired` shows today; parity for `aeon index|graph|telemetry`; S22/S30/S33 guard hashes; S45/S47 state; error block; baseline `go test ./internal/retrieval/ ./internal/search/ ./internal/graph/ ./internal/store/ ./internal/protocol/ ./internal/daemon/ ./cmd/aeon/ -count=1` (and the `noretrieval` tag run).
### T1 — `platform_change`, the audit threading, single-flight message from the audit ([`tasks/T1.md`](tasks/T1.md)) — Depends: T0.
### T2 — `platform.status`, `retrieval.reindexPlan`, `platform.changes` ([`tasks/T2.md`](tasks/T2.md)) — Depends: T1.
### T3 — Guards (P4) ([`tasks/T3.md`](tasks/T3.md)) — Depends: T2. Gates T4–T6.
### T4 — `index_status` tool (P6, one read) ([`tasks/T4.md`](tasks/T4.md)) — Depends: T3.
### T5 — CLI (P5) ([`tasks/T5.md`](tasks/T5.md)) — Depends: T2, T3.
### T6 — Settings → System and close-out (P7, P9) ([`tasks/T6.md`](tasks/T6.md)) — Depends: T2–T5, S45 T9.
**Numbers:** 1 migration (`platform_change`); 1 table; 3 new reads; 0 new writes (4 existing operations audited); 1 agent tool (read) granted to 0 by default; 0 seed sets; settings only if SP5's test passes.
