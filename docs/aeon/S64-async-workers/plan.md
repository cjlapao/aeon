# S64 plan — six tasks, each with its own evidence

| Task | Delivers | Acceptance |
| --- | --- | --- |
| T1 | `async_worker` migration + store surface (8 methods) + `NewWorkerID` | dedupe under the partial index; guarded terminal writes; reads on `s.r`; 11 tests |
| T2 | `internal/asyncworkers` manager — Register/Start/Get/List/Cancel/Recover/Shutdown | states + dedupe + events + cancel + timeout + panic recovery + concurrency cap; 16 tests, `-race` |
| T3 | protocol seam + family (no start verb) + `graph.build` → handle + six event fixtures | V13 absent-without-seam; refusals name the thing refused; conformance + protogen |
| T4 | daemon wiring — one manager value behind every door, register `graph.build`, Recover/Shutdown, CLI follow | `reflect.Pointer` pin; build→finished through the real socket; interrupted sweep at boot |
| T5 | `asyncworkers.ts` + AsyncWorkersPage + GraphPage banner/handle/registration | 9 new tests incl. six-states-are-six and no-start-affordance; existing bench-graph suite updated to the handle contract |
| T6 | live walk (CLI + socat), gates, records | walks below; `make ci`-equivalent; SLICES + DEMO records |
