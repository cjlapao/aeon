# S66 plan — the order it was built in

1. **T1 — the watcher.** `watch.go` (+ service fields), tests with the
   real fsnotify watcher over temp dirs. Gate: `go test ./internal/skills/
   -race`.
2. **T2 — the seam.** `runtime/skills_inject.go`, the CompileRequest
   append, `StepRequest.SkillsInjected`, the runner marker onto
   `step.started`; the daemon renderer + provider in
   `daemon/skills_wire.go`; wiring guards.
3. **T3 — the announcements.** `skills.changed` (kind constant, daemon
   late-bound emit, serve `StartWatch`, fixture); `skills` on the
   protocol `StepEventPayload`; the CLI list watch line.
4. **T4 — the app.** ChatItem `injection`, `fromEvents` mapping,
   `InjectionRow`, `useSkills` subscription, SkillsPage watch state,
   vitest for both lanes.
5. **T5 — gates, walk, record.** `check-protogen`, `guard`, full Go
   suite, full vitest, tsc parity vs main; the LIVE walk (drop a
   folder → event < 1 s no restart; real `aeon step` → marker on the
   wire); this record; merge.
