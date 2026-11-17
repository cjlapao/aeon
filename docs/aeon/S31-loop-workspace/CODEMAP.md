# S31 · Code map — the tree as measured at slice start (2026-10-24)

Produced by a read-only survey of `aeon/` at the S31 baseline commit (`70cc5a0`),
before any S31 code landed. This is the "what exists" table the brief's §2 asserts,
with exact seams. Line numbers were verified against that commit; re-check before
editing. This file is working material for the slice — it is not the contract
(`brief.md` is) and it will go stale as the tasks land.

**Baseline repairs already on this commit** (before any S31 work):
`fix(store): upgrade databases that built step.pause_reason as 0019; test(cmd):
package-wide throwaway XDG_STATE_HOME` (`065c22f`) — `hasLegacyPauseReason` in
`internal/store/migrate.go` mirrors `hasLegacyProjectIcon` for the pre-renumber
`0019_step_pause_reason` ledger row, and `cmd/aeon`'s TestMain sets `XDG_STATE_HOME`
package-wide (an in-process verb that forgot isolation had migrated the real store).

---

## 1. internal/protocol/step.go (681 lines)

- **handleStep** — `func (t *Table) handleStep(ctx context.Context, sess *Session, params json.RawMessage) (any, error)` (:260). Registered as `MethodStep` (method.go:303).
- **DiscardSink** at :311: `var sink runtime.EventSink = runtime.DiscardSink{}` — replaced only when `p.SubscriptionID != ""` (312–324: `t.Subs.Find(sess.ConnID, id)` → `sink = t.stepSink(sub)`; unknown id → `ErrUnknownSubscription`). Blueprint steps never subscribe (blueprint.go:123 sends no SubscriptionID), so their events hit the DiscardSink.
- **stepSink** :646–663: `t.OnEvent(e.Kind, StepEventPayload{Tool, Text, Summary, Result, IsError, Iteration, Outcome, Reason, Start, End, EstimateBefore, EstimateAfter, Cost})` — **StepID is NOT set here**.
- **Resume sink that DOES set StepID** — `continueAnsweredStep` (511–598), RunOpts sink at :532–541: `runtime.SinkFunc(func(e){ t.OnEvent(e.Kind, StepEventPayload{StepID: stepID, Tool: e.Tool, ...}) })`. Other StepID-bearing envelopes: `step.paused {stepId}` (447, 582), `step.saved {stepId, outcome}` (587), `step.continued` (step_stop.go:183).
- **RunStepForLoop** — `func (t *Table) RunStepForLoop(ctx context.Context, p StepParams, onCreated func(int64) error) (StepResult, error)` (:243–258): `ctx = context.WithValue(ctx, blueprintStepCreatedKey{}, onCreated)` (244; key type at 241) → `json.Marshal(p)` → `t.handleStep(ctx, nil, raw)` — sess is nil, safe only because sess is read at :313 when SubscriptionID is non-empty → re-assert to `StepResult`.
- **onCreated seam call site**: after `compiled.Request.StepID = stepID` (:375), :376–380 calls the callback; refusal aborts with `ErrProviderFailed` "the blueprint could not attach step %d".
- **CreateRunningStep**: interface `CreateRunningStep(loopSlug, projectRoot, agent string) (int64, error)` (StepStore, :148); called at :365 **before** Run (K22 "row FIRST"); implemented at daemon/step.go:320–340 (creates the loop row if absent, `store.CreateStep(sc, store.NewStep{Phase:"build", Agent:agent, Status:store.StepRunning})`). Step id known at :365, stamped server-side at :375.
- Tracking mandatory: `if t.TrackStep == nil { return nil, ErrStepUnavailable… }` (399–401); `runCtx, doneTracking := t.TrackStep(ctx, stepID); defer doneTracking()` (402–403); `t.Runner.Run(runCtx, compiled.Request, runtime.RunOpts{Client: t.Models, Tools: toolset, Sink: sink, HandoffSpec, Receipts, Checkpoint, Policy: t.PolicyGate})` (405–413); `stopped := t.StepWasStopped != nil && t.StepWasStopped(stepID)` (417, read ONCE). `recoveryWriter` (:202–237) implements CheckpointWriter/ReceiptWriter against `SaveCheckpoint/BeginReceipt/CompleteReceipt`.

## 2. internal/protocol/step_stop.go (199 lines)

- **step.stop**: `StepStopParams{StepID int64 "stepId"}` (47–49); `StepStopResult{StepID, Stopped bool}` (52–55); `handleStepStop` (57–75) requires `t.CancelStep` (65–67); "not found" → `Stopped:false, nil`, not an error.
- **step.continue**: `StepContinueParams{StepID; Route string; ReasoningEffort string}` (79–93). `handleStepContinue` (95–199): effort validation (103), `t.Recovering` refusal (111–113), precondition `Questions.StepState` → `status=="suspended" && pauseReason=="user"` (119–127, `ErrStepNotPaused`), defensive `t.StepIsRunning` check (132–135), `LoadResume` (142), `stepSnapshot` unmarshal + WorkspaceRoot guard (146–149). Route/effort override at :155–169 (explicit route wins; else re-resolve through current route config; empty effort keeps saved value). `BuildTools` from frozen manifest (171–177), `MarkRunning` (179), publish `step.continued {stepId, route, reasoningEffort}` (182–184), `runtime.ResumeRequest{… Recovery: true}` (186–193), `go t.continueAnsweredStep(…)` (196), answer `AnswerResponse{StepID, AnsweredAt:"", Status:"running"}` (198).
- **"What this does NOT do"** doc (:35–44) — T2 removes it. Verbatim: it does not stop a step running inside a blueprint loop (`loop.start`); that path drives `AdvanceLoop` through `internal/loop.Runner`, a different caller of the same runtime.
- `TrackStep` called at step.go:399–403 (first run) and :526–529 (resume; tolerated when nil there). Field contracts at method.go:173–196.

## 3. daemon.go / lifecycle.go / stepcancel.go

- **publish**: `func (d *Daemon) publish(kind, projectRoot string, payload any)` (lifecycle.go:139–148) → `d.Table.Events.Append(kind, projectRoot, payload)` (log-and-return on encode error) → `for _, sub := range d.Table.Subs.Matching(ev.Kind, ev.ProjectRoot) { d.deliver(sub, ev) }`. Exported `Publish` (151–153). `deliver` (155–162) stamps `ev.SubscriptionID`, `d.notify(sub.ConnID, "event", ev)`, advances the cursor only on success.
- **Empty project root**: daemon.go:202 `table.OnEvent = func(kind string, payload any) { d.publish(kind, "", payload) }` — every domain event lands with empty projectRoot → NULL `event.project_root`. `Table.OnEvent` field: method.go:169.
- **Filter**: protocol/types.go:73–80 — `ProjectRoot string` (exact match, empty = any), `Kinds []string` (exact or single trailing `*`). `matchesProject` (filter.go:86–94): an event with empty projectRoot matches EVERY subscriber, including project-scoped ones — how `publish(kind, "", …)` still reaches project-filtered subs.
- **Subscriptions**: `handleSubscribe` (subscribe.go:213–236): `Events.Resume(p.After, p.Filter)` → `Subs.Add(connID, filter, from)` → `t.OnSubscribed(sub, replay)`; daemon `onSubscribed` (lifecycle.go:96–107) delivers replay then `subscription.created`. Registry (subscribe.go:40–143): `Subscription{ID, ConnID, Filter, Next}`; Add validates filter + caps; `Matching` stable order; close reaps via `Subs.RemoveConn` (lifecycle.go:116–120).
- **stepCancels** (stepcancel.go): `{mu sync.Mutex; byID map[int64]*trackedStep}` (31–34); `trackedStep{cancel context.CancelFunc; stopped bool}` (36–39); `track(parent, id)` (51–65) wraps its OWN cancellable ctx, **panics on double-track**; `stop(id)` (71–81) sets stopped then cancels; `wasStopped(id)` (86–96) read-AND-CLEAR (DB19 anti-sniffing); `isRunning` (101–105). Wired at daemon.go:204–207.

## 4. daemon/blueprint.go (495 lines)

- **RunStepForLoop call** at :122–135 inside the `Model` callback of `loop.Deps` (built by `initBlueprints`, 92–292): `d.Table.RunStepForLoop(ctx, protocol.StepParams{Prompt: prompt, Agent: step.Agent, WorkspaceRoot: st.Root, Route: st.Route, LoopSlug: st.Slug}, func(stepID int64) error { return loop.MarkStepStarted(ctx, loop.Deps{Store: d.store}, st.ID, st.Position, stepID) })` → `loop.ModelResult{StepID, Kind, Outcome, Reason, Cost, QuestionID}`. No SubscriptionID/Tools/ReasoningEffort passed — loop steps stream nothing.
- Other Deps: `Event: func(kind, payload){ d.publishLoop(kind, payload) }`; `RecoveredStep` (96–121) reads the step row, `done→"complete"`, `suspended→"suspended"+QuestionID`; `d.loopRunner = loop.NewRunner(dep)` (136).
- **Loop start**: `StartBlueprintLoop` (159–186) — workspace validated (71–90), `loop.Start(…)` (:179), `loop.started {loopId, blueprint, version}` (:183), `d.loopRunner.Start(context.Background(), st.ID)` (:184). Answer path `AnswerBlueprintLoop` (194–207); `OnStepContinued` (137–158) resumes waiting loops whose `RuntimeState.LastStepID` matches, re-`Start`s the runner. `StartLoops` (481–495) restarts "running" pins post-recovery.
- **publishLoop** (296–317) enriches loop events with `snapshot` + `row` when `loopId` present, then `d.publish(kind, "", copyFields)`.
- **Loop event emission sites**: advance.go — `loop.finished` (84, 136, 215), `loop.blocked` (96, 106, 160, 218), `loop.waiting` (122, 176), `loop.skipped` (134), `loop.advanced` (213); runner.go — `loop.blocked` runner-error (50), `loop.advanced` gate answer (127), `loop.blocked` gate-answer (129); blueprint.go — `loop.started` (183), `loop.blocked` store failures (140, 150). Kind constants: events.go:25–30; `Kinds()` (39–61).

## 5. runtime/loop.go (893) + compile.go + runstep.go

- **RunLoop** — `func RunLoop(ctx context.Context, st *LoopState, d LoopDeps) StepResult` (:163); main iteration `for {` (166):
  - Bounds BEFORE the call: MaxIterations (172–174), DeadlineAt vs `d.Clock()` (175–178), `ctx.Err()` → failed "the step was cancelled" (179–184), compaction check (185–193). `st.Manifest.IterationsUsed++` (195).
  - Model request 198–207 (Route, Messages, `st.Request.Tools`, MaxOutputTokens from handler, `ReasoningEffort: st.Request.ReasoningEffort` on EVERY call); `st.stream(…)` (209; deltas forwarded as they happen, 418–497); checkpoint (210–212).
  - **Assistant turn saved before any tool** (217–227) + checkpoint.
  - `if len(turn.toolUses) == 0 { return st.finish(ctx, req, d, iteration) }` (229–231) → handoff + ONE tool-free retry (`Tools: nil`, 677–692).
  - Tools: question/ordinary split (238–246); >1 question → failed (247–249); serial loop 250–318 (K9) with `runTool` (500–643): `step.tool.start` (505), unparseable-args debt (512–521), policy gate (529–613), ask → `pendingPermission` + `suspendWith` (256–304 / 341–346; closes unrun siblings via `closeUnrunTool` 361–371), deny → IsError sentence, unsafe tools need Receipts (614–625), **single dispatch `res := d.Tools.Call(ctx, use.Name, use.Input)`** (627), `step.tool.end` (637–640); per-result checkpoint (309–317); question suspend (322–328).
- **LoopDeps** (:121–144): `Client Streamer; Tools ToolCaller; Sink EventSink; Receipts ReceiptWriter; Checkpoint CheckpointWriter; Policy PolicyGate; Clock func() time.Time; HandoffSpec; StatusField`. `withDefaults()` (146–157): Clock→time.Now, Sink→DiscardSink, StatusField→"status". Checkpoint nil → no-op (receipt.go:45–51); Policy nil → no enforcement.
- **st.Request**: `StepRequest` (types.go:315–385) — `Route model.Route` (:327), `ReasoningEffort string` (:339), `StepID int64` (:363, server-assigned), `HasOperator bool` (:384).
- **Checkpoints**: `st.checkpoint` at runstep.go:144/149, loop.go:210/224/299/314/696/706, resume.go:75/84/91 — writer = recoveryWriter → store `SaveRunningCheckpoint`.
- **LoadResume**: `QuestionStore.LoadResume(stepID) (ResumeSnapshot, error)` (step.go:167); `stepSnapshot` (step.go:189–198: ContextManifest + Request + LoopSlug + Usage + Compactions); step.continue re-hydrates route/effort/grants from `saved.Request`. `ResumeStep` (resume.go:33–96) re-enters `runGuarded`.
- **compile.go tools-vs-reasoning**: `reasoningFor(in CompileInput)` (:181–209) — explicit call effort literal (182–184); else lowers to `"none"` only when tools offered AND route effort non-empty/non-none AND `in.Route.ToolsRefuseReasoning()` (187–200); emits a visible Note (202–208). Runs again on resume (step_stop.go:88–92).
- **RunStep** (runstep.go:87–154): guardedSink panic boundary (194–225, failures → `result.SinkFailures`); `runGuarded` converts panics to failed (169–180).

## 6. loop/advance.go (391) + runner.go (155)

- **Deps** (advance.go:17–32): `ModelResult{StepID int64; Kind, Outcome, Reason string; QuestionID int64; Cost json.RawMessage}`; `ModelRunner func(context.Context, LoopState, blueprint.Step, string) (ModelResult, error)`; `RecoveredStep`; `Event func(string, any)`.
- **Kinds today**: only `"suspended"` is interpreted (advance.go:166); everything else = completed. Producers use `"complete"`/`"suspended"`. Outcomes (179–209): `"ok"` (moveNext), `"reject"` (advisory→moveNext+LaneStates, else routeReject), default → blocked `BlockedReason{Kind:"step-"+outcome}`.
- **AdvanceLoop** (:61): Pinned → Decide → one action → save → emit. Switch (77–222): ActDone (78–84), ActWaiting (85–86), ActBlocked (87–96), ActRunCodeAction (97–106, blocked kind `"unimplemented-action"`, Slice S16), ActAskGate (107–122: status waiting, CommitGate, emit), ActSkip (123–137), ActRunModelStep (138–219: `st.StepInFlight` → RecoveredStep else Model; model error → blocked `"model-error"` 153–161; suspended → waiting 166–177; missingArtifact demotes ok→reject 179–184; `loop.advanced` 213).
- **loop.waiting payload shapes**: gate → `{loopId, questionId, prompt, answers}` (advance.go:122); step-suspension → `{loopId, questionId}` (:176). `loop.advanced` → `{loopId, lane, item, step, outcome, cost}` (:213).
- **Runner** (runner.go:28–60): `active` dedupe; `Start(ctx,id)` spawns `Run` (13–26: advance until waiting|blocked|done). `AnswerGate` (64–132) closed answer set. `MarkStepStarted` (continue.go:13–27) sets `StepInFlight`; `CompleteSuspendedStep` (:29+); `BlockCorruptPin` (:120). State types: state.go:14–60.

## 7. protocol/events.go (327)

- `Backing` (84–98): `Append(kind, loopSlug, projectRoot, payload) (Event, error)` — the ONLY allocator (DB20); `Range(after, filter, limit)`; `Head()`. Daemon adapter: daemon/memory.go:491–530 (`NewEventBacking`).
- **Append** (189–231): with backing the event-table AUTOINCREMENT rowid is the seq (durable; `l.seq` tracks max); without, in-memory `l.seq++` (test mode). Ring trim (226–229); `DefaultRetention = 1024` (:174). `WithBacking` (159–171) seeds from `Head()`.
- **Resume** (278–313): nil→(head+1, nil); beyond head→(head+1, nil); older than ring → `backing.Range(*after, filter, 0)` when available (297–301) else gap REPORTED (`from = ring[0].Seq`). Filter via `filter.Matches` (308).
- Filter validation filter.go:35–67 (only trailing `*`); matching 74–116. Event envelope types.go:115–131.

## 8. tools/spawn.go (292) + write/list

- **Spawn** — `func Spawn(ctx context.Context, command string, opts SpawnOptions, verdict policy.AllowToken) (RunResult, error)` (:106); FIRST statement is the token check (107–110); `SpawnOptions{Dir required absolute; Timeout ≤0 → BashTimeout (600s); StdoutCap/StderrCap → 65536}` (75–88). `RunResult` (41–72): ExitCode, Signal, TimedOut (via `errors.Is(runCtx.Err(), DeadlineExceeded)` 204–207), OOMKilled heuristic (216–219), Stdout/Stderr + true byte counts + truncated flags, Duration.
- **Process-group kill uses `Setpgid`, NOT Setsid**: `cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}` (:159); `terminateGroup(pgid)` (239–245): `Kill(-pgid, SIGTERM)` → sleep BashKillGrace 2s → unconditional `Kill(-pgid, SIGKILL)`. Own `context.WithTimeout` (131) — not `exec.CommandContext`, which kills only the process (128–130). stdin nil → /dev/null (145); env inherited (147–154, stated limitation); `Shell()` = `bash --noprofile --norc -c`. `cappedBuffer` (258–292) counts past the cap.
- **Killed-group measurement** (spawn_test.go): test binary re-executes itself as a TCP port holder via `AEON_TEST_PORT_HOLDER`; group-kill ⇒ port freed; parent-only kill ⇒ port still held (23–28).
- **write_file result**: [tools/write.go](../../../internal/tools/write.go) (NOT files.go — files.go is `list` only). `runWrite` returns (write.go:58) `Result{Content: "wrote %s (%d bytes)", Summary: "write %s · %s"}`. `Result{Content /*model sees*/, IsError, Summary /*event stream*/}` at tools/read.go:74+. Metadata seam without touching model-facing content: new fields on `tools.Result` mapped by the dispatcher (runtime/loop.go:633–640) onto `step.tool.end`; `StepEvent` has structured fields (types.go:414–418). `write_file` is a hidden alias (toolset.go:61–69).

## 9. Method registration

- **Name table**: protocol/method.go — `type MethodName string` (23) + const block (25–120); `Method{Name, RequiresInitialize, Debug, Handle}` (123–137).
- **Centralized in ONE function**: `NewTable(...)` (268–329) — all ~37 methods via `t.register(&Method{...})` (dupes panic, 331–338). Adding a method = const + one register line + handler on `*Table` in a per-feature file + conformance fixture + `make protogen`.
- Handlers split per feature: handshake/session, subscribe.go, ask.go, routes.go, memory.go, blueprint_loop.go, projects.go, step.go, answer.go, step_stop.go, policy.go. Several loop/blueprint handlers are **daemon-wired function fields** (method.go:252–262: StartBlueprintLoop, ShowBlueprintLoop, ListLoopFiles, ReadLoopFile, AnswerBlueprintLoop, ListBlueprints, BlueprintFields, ShowBlueprint, ValidateBlueprint, SaveBlueprint, OnStepContinued) — assigned in blueprint.go / loop_files.go.
- Mount: `Daemon.Register()` (daemon.go:232–240) → `localrpc.Register(name, d.handler(name))`; `Dispatch` (method.go:375–395) initialize gate; `PublicMethods()` (352–360) is the S19 narrow-surface list — **terminal.\*, step.steer\* must never be added there (LW8)**.

## 10. internal/store

- **Migrations**: `Migrations()` (schema.go:49) — **20 entries** at baseline (`{1,"0001_core_tables"} … {20,"0020_step_pause_reason"}`); naming `NNNN_snake_name`, const `migrationNNNN`. One transaction each; shipped entries immutable. **Count-pinned test**: schema_test.go:851 `if len(all) != 20` and :860–861 wording — a new migration bumps both. Ledger cross-checks at 418/464/1277.
- **step table** (migration0002, schema.go:607–632): `id INTEGER PRIMARY KEY AUTOINCREMENT, loop_slug, project_root, taskid, phase, agent TEXT NOT NULL (:614), attempt, status CHECK IN ('running','suspended','orphaned','done'), started_at, agent_returned_at, ended_at, agent_ms, wall_ms, outcome, ended_by 'open', source 'live', cost_usd, cost_state CHECK, cost_detail`, FK → loop. `pause_reason` via 0020 (`SetStepPauseReason` steps.go:302). Indexes `idx_step_loop`, `idx_step_status`. `step_message`: `UNIQUE (step_id, seq)`.
- **Checkpoint**: `SaveRunningCheckpoint(ctx, stepID, msgs []model.Message, manifest json.RawMessage, cost model.Cost) error` (checkpoint.go:13–60) — validates transcript (`model.ValidateTranscript`), ONE `BeginTx(ctx,nil)`: DELETE+re-INSERT step_message (seq=i+1), cost update, `UPDATE step SET context_manifest=? WHERE id=? AND status='running'`.
- **Receipts**: `BeginReceipt`/`CompleteReceipt` (receipts.go:39/53).
- **Transactions**: raw database/sql on the single writer: `tx, err := s.w.BeginTx(ctx, nil)` (checkpoint.go:21, recovery.go:112), `defer tx.Rollback()`, Commit. Reads use `s.r` (never the writer, O6/I6). Store struct open.go:116–134: `w` writer (MaxOpenConns 1, DB15), `r` read pool, `gw` gorm over the writer.

## 11. Conformance fixtures & protogen

- `internal/protocol/conformance/` — one dir per method (answer/ ask/ blueprint/ debug/ errors/ events/ initialize/ ping/ policy/ recovery/ routes/ step/ store/ subscribe/ unsubscribe/ version/) + `forward-compat.json` (never delete) + `README.md`. `step/` has 11 fixtures (incl. the new stop/continue ones); `events/` has 15 (loop-*, step-tool.json, ask-*, subscription-created, …).
- Fixture shape (conformance_test.go:25–32): `{description, method?, rawRequest?, request?, response?, notification?}`; semantic JSON comparison; directory-walked (conformance_test.go:34+).
- `make protogen` = `go run ./tools/protogen` → rewrites `ui/src/proto/contract.ts`; `make check-protogen` = `--check` (runs in ci). New event kind also needs a `Kinds()` entry (events.go:39–61) + a `conformance/events/` fixture.

## 12. ui/src-tauri/src/session.rs (606 lines)

- **handle_frame** (530–565): frames with `id` → resolve pending (pong short-circuit 547–551); frames WITHOUT id → **only `method == "event"` is forwarded** (556–562) — params verbatim. Unknown notification methods: logged/dropped, never forwarded blindly.
- Channels: `EVENT_CHANNEL: "aeon://event"` (55), `CLOSED_CHANNEL: "aeon://closed"` (58).
- **EventSink trait** (121–126): `fn event(&self, params: Value); fn closed(&self, reason: String);` — trait because AppHandle can't be built outside a running app. `AppSink(pub AppHandle)` (133) — "reports a failed emit rather than discarding it" (129–148: `eprintln!` on emit failure, stderr on purpose).
- Tests: `ui/src-tauri/tests/session.rs` — FakeDaemon/Script (58–217), TestClient (219–292); tests incl. `initialize_is_the_first_frame_on_the_wire` (294), `a_notification_is_not_mistaken_for_a_response` (374), `the_shell_talks_to_a_real_daemon` (484). `tests/live_stream.rs` — `Recorder` implements `session::EventSink` (34–48) with `kinds()`/`wait_for` (live tests `#[ignore]`d behind `AEON_LIVE_ENDPOINT`). Run: `make -C ui rust-test` (cd src-tauri && cargo test).

## 13. ui/src/lib/bridge.ts (256 lines)

- `onEvent(handler)` (221–227) via `subscriber(EVENT_CHANNEL, …)` + `isKnownEventKind` gate (generated from contract.ts); `onClosed` (230–232). `setSubscriber(next)` (114–116) / `resetBridge()` (118–121) test seam; `Subscriber` type (25). `rpc()` (159–165) generic bridge — new methods need no bridge change. `listenerUnavailable` three-part error (199–212).
- Vitest: `ui/test/*.test.ts(x)` (~29 files); run `make -C ui test`; config `ui/vite.config.ts`.

## 14. Test conventions

- Daemon-level loop/blueprint tests vs fake model — fake = `scriptedModel` (daemon/step_test.go:32–76, scripted `modelTurn`s over Stream, records requests; `runRealStep` :78):
  - `daemon/permission_ask_test.go` — TestAttendedAskCreatesTheHonestQuestionRow (:76), TestUnattendedAndNeverDeclaredAskDenyWithNoQuestionRow (:148); daemon via `New(Options{Store, Routes: protocol.NewStaticRoutes(route), Models: &scriptedModel{…}})` (:57).
  - `daemon/suspend_integration_test.go` — TestQuestionSurvivesDaemonRestartAndAnswerResumes (:19) — two daemons over one store.
  - `daemon/recover_test.go` (scriptedModel :67), `daemon/blueprint_workspace_test.go` (drives `d.Table.StartBlueprintLoop`).
  - loop level: `loop/advance_test.go` — TestTwoStepReplayStopsAtGateAndFinishesAfterAnswer (:14), TestSuspendedStepContinuationStartsTheFollowingGate (:76), TestMissingProducedFileRejectsStep (:123), TestIterateRunsEveryLadderStepPerStableItem (:147), TestRecoveredInFlightStepDoesNotCallModelAgain (:203). Fake model = `Deps.Model` closure returning `ModelResult{Kind:"completed", Outcome:"ok"}` + writing declared artifacts.
- **Recorded provider fixtures**: `internal/model/testdata/anthropic/*.sse` (15) + `internal/model/testdata/openai/*.sse` (7, responses-* variants) + `manifest.json`; `model/fixtures_test.go` pins fixture↔manifest (79), labels (105), no credentials (141), parsability (214), scenario coverage (250). **Request-shape assertions**: adapter tests capture the outgoing `*http.Request` — `wireBody(t, req)` (openai_test.go:30) unmarshals the body and tests assert on that map (e.g. TestOpenAISystemStaysInTheMessageArray :74, TestOpenAITheBodyKeySetIsExactlyTheMappedSet :286; anthropic_test.go:26 hoisted-system check); responses come from a fixture-backed fake server ("fixture-server", boundary_test.go:66). Adapters: internal/model/adapters/{anthropic,openai,openai_responses,registry}.go.
- **Guard test**: `TestNoPolicyAuthoringReachesTheNarrowSurface` at **cmd/aeon/policy_test.go:483** — regexes ban policy authoring names anywhere; `"policy.<name>"` literals allowed only for the four reads; `.Add/.Remove/…` calls only in sanctioned files. LW8's terminal/steer guard mirrors this test.

## 15. Payload shapes

- **StepEventPayload** (step.go:666–681) verbatim: `StepID int64 "stepId,omitempty"; Tool; Text; Summary; Result; IsError bool; Iteration int; Outcome; Reason; Start; End; EstimateBefore; EstimateAfter int; Cost *model.Cost` — all omitempty. Published from stepSink (:651, StepID absent) and the resume sink (step.go:534–539, StepID set).
- **step.tool.end**: built as `runtime.StepEvent{Kind: EventToolEnd, Tool, Summary, Iteration, Result, IsError}` in runtime/loop.go at 637–640 (normal), 516–519 (unparseable args), 584–587 (ask-suspended), 599–602/607–611 (deny), 362–366 (closeUnrunTool); reaches the wire via sink → stepSink/resume-sink → `t.OnEvent(kind, StepEventPayload)` → publish → EventLog.Append → subs → deliver → `notify(connID, "event", Event)`. Step-event kind constants EventToolStart/EventToolEnd/EventModelDelta/EventStepStarted/EventStepDone/EventStepCompacted: types.go:427–434; `EventKinds()` 438–447.
- **model.delta**: runtime/loop.go `stream()` — text deltas 427–430 (`StepEvent{Kind: EventModelDelta, Text, Iteration}`), thinking deltas 434–438 (same kind, `Tool: "thinking"`). Emission as-they-happen (K19); guardedSink panic boundary runstep.go:194–225 counts failures into `StepResult.SinkFailures`. Conformance: conformance/events/ask-delta.json; step-tool.json for tool shapes.

## 16. go.mod

- `module github.com/cjlapao/aeon`, `go 1.26.6`. Direct deps (8): asg017/sqlite-vec-go-bindings v0.1.6, cjlapao/go-backend-scaffolding/framework v0.10.1, mattn/go-sqlite3 v1.14.52, sirupsen/logrus v1.9.4, golang.org/x/sys v0.49.0, gopkg.in/yaml.v3 v3.0.1, gorm.io/driver/sqlite v1.6.0, gorm.io/gorm v1.31.2. **creack/pty is NOT present** (T7 adds it — it is cached in GOMODCACHE: v1.1.18 and v1.1.24). No vendor dir → proxy mode with `GOPRIVATE=github.com/cjlapao/*`.

## 17. UPSTREAM.md (aeon/UPSTREAM.md, 124 lines)

Records the FRAMEWORK dependency only (pinned module, B1/B8/B11 guards, GOPRIVATE, "What Aeon imports" table enumerates framework imports only — "Nothing else."; gorm/logrus/yaml deliberately not listed; upgrade procedure + log). **Adding creack/pty does NOT need a UPSTREAM.md entry**; the obligation is go.mod/go.sum via `make tidy` and naming the dep + licence in the slice's tasks/T7.md Notes (AGENTS §4a). `make guard` only rejects scaffolding-internal imports.

## 18. Makefile targets (aeon/Makefile)

- `test` (:168–169) `go test ./...`; `test-race` (:197–198) `go test -race -coverprofile=coverage.txt -covermode=atomic ./...`; UI: `make -C ui test` (vitest); Rust: `make -C ui rust-test`.
- `ci` (:275) = `fmt-check vet guard check-protogen check-corpusgen test build` in order. fmt-check (:220–221) `gofmt -l cmd internal`; vet (:206–207) `go vet ./...` + UI typecheck; build (:105–108) builds + ldflags binary into dist/.
- `protogen`/`check-protogen` (:256–260); `corpusgen`/`check-corpusgen` (:268–271).
- Makefile exports `GOPRIVATE := github.com/cjlapao/*` (:28) and `CGO_ENABLED := 1` (:39) — **no CGO_CFLAGS handling**. The machine needs the AGENTS §6 preamble: `<repo-root>/.tmp/cgo-shim/sqlite3.h` (copied from `$(go env GOMODCACHE)/github.com/mattn/go-sqlite3@v1.14.52/sqlite3-binding.h`) + `CGO_CFLAGS="-I$ROOT/.tmp/cgo-shim"`. On this box `GOCACHE` must also point inside the workspace (`$ROOT/.tmp/gocache`).

---

## Cross-cutting notes for the plan

- (a) blueprint steps publish NO step-level events (RunStepForLoop passes no SubscriptionID → DiscardSink step.go:311; only loop.* and step rows reach clients) — **T1**.
- (b) `step.stop` cannot reach loop steps (step_stop.go:35–44) — stepCancels keyed by stepID, the loop's ctx comes from `d.loopRunner.Start(context.Background(), …)` (blueprint.go:184) with no per-step hook — **T2**.
- (c) new socket method = method.go const + NewTable register line + per-feature handler file + conformance fixture + `make protogen`;
- (d) new event kind = events.go const + `Kinds()` entry (+ `EventKinds()` in types.go for step-family kinds) + conformance/events fixture + protogen; a new Tauri CHANNEL needs session.rs handle_frame arm + AppSink method + bridge.ts const (T8).
- (e) new store table = new numbered migration (next: **0021**) + bump the pinned count in schema_test.go (currently 20);
- (f) pty: no creack/pty yet; proxy mode; CGO already on; `Setpgid` (not Setsid) is the spawn.go discipline — the brief's LW9 "Setsid" means the same observable (own process group, group-killed): match spawn.go's actual mechanism.
- (g) `Filter.ProjectRoot` matching: an event with EMPTY projectRoot already matches every subscriber — so D1 needs the step events appended with the REAL root AND a test that a foreign root's subscriber receives none (empty-root events would leak).
- (h) the real store was repaired at baseline (0020|failed → applied via the new guard) — do not re-point tests at it; AGENTS §6 throwaway XDG_STATE_HOME (now enforced package-wide in cmd/aeon's TestMain).
