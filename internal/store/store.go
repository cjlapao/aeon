package store

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
)

// The surface.
//
// # Who reads and writes through here
//
// Every exported method on *Store is named below, with the consumer that asked
// for it. Nothing is exported "so it is there when we need it": an export with
// no caller is surface to maintain for nobody, and a test
// (TestEveryExportedMethodIsNamedInTheSurfaceTable) fails when a method is
// added without an entry here.
//
//	THE LIFECYCLE
//	  Open · Close · Closed · Path · Migrate · Pending · AppliedMigrations · Tables ·
//	  JournalMode · Info · SchemaFingerprint · ReadPoolSize · BusyTimeout · Now
//	                       the CLI (`aeon db info`, `db upgrade --dry-run`),
//	                       and `serve`, which opens the store and hands its
//	                       handle to the framework; `Closed` is what the daemon's
//	                       late background reads ask — a queue-reactor goroutine
//	                       arriving after `Close` must answer an error, not
//	                       dereference a closed pool (`close_test.go`)
//	  GormDB               internal/bootstrap, for database.WithDB — the
//	                       framework's injected-mode seam
//	  Name · Init ·        the framework's `Store` lifecycle contract
//	  Health · IsEnabled · (framework.go). The database service refuses to
//	  Dependencies         start with no store provider installed, and this is
//	                       the store it manages
//	  VecVersion           `aeon db info --vectors` now; S22's retrieval later
//
//	S5  the runtime        CreateStep · ReadStep · ListSteps · SetStepStatus ·
//	                       SetStepPauseReason (a user-initiated stop, protocol's
//	                       step.stop/step.continue — distinct from a model's
//	                       real question, which stays SetStepStatus alone) ·
//	                       SetStepCost · SetContextManifest ·
//	                       SetContextManifestJSON (the step handler, which owns
//	                       the manifest's shape) · AppendMessages ·
//	                       ReadTranscript · ReplaceRange (compaction) ·
//	                       CreateLoop · GetLoop · SetLoopPhase · SetLoopStatus ·
//	                       SetPhase · Phases · CreateTask · GetTask ·
//	                       ListTasks · SetTaskStatus · BumpTaskCounter ·
//	                       BumpLoopCounter (S17's two-level counter rows — the
//	                       whitelist-validated increments behind internal/loop/
//	                       counters.go, which owns the table and the levels)
//	S6  suspension         Ask · Answer · PendingQuestion · PendingStepQuestion · LatestStepQuestion · ReadQuestion ·
//	                       IsAwaiting · AwaitingLoops · Thread · Note
//	S7  recovery           BeginReceipt · CompleteReceipt · Receipts · SaveRunningCheckpoint ·
//	                       StepsByStatus(StepRunning) · RunningStepIDs · ReadRecovery ·
//	                       SaveRecoveryObservation · CommitRecoveredStep (T7 added
//	                       its pauseReason — a suspension with no question, the
//	                       cap's shape) · ReadTranscript ·
//	                       RawTranscriptContent · TranscriptRows ·
//	                       TranscriptDigest · VerifyTranscript · SignatureOf
//	S14 agent services     SuspendStepForTask · CreateAgentTask · GetAgentTask ·
//	                       MarkAgentTaskTerminal · CreateAgentChainEdge ·
//	                       CountAgentChainEdges · CountAgentTasksForCaller ·
//	                       ChainEdgesByCallerAgent · ChainEdgesToCallee ·
//	                       ListAgentTasksForCaller · ListAgentChainEdges — the
//	                       last three serve AS6's ordering proof: the edge is
//	                       countable WITHOUT a task (a recorded attempt) and a
//	                       task is countable per caller step (T4's invoke writes
//	                       the edge before the task, and the counts are the
//	                       evidence a test can read without SQL).
//	                       MarkAgentTaskTerminal — the Task IS a row (AS4/AS5): the
//	                       async creation flips the step row, asks its question and
//	                       writes the task in ONE transaction (a half-suspension is
//	                       what a cold restart could not reason about), and every
//	                       terminal move is guarded on the status it expected, so a
//	                       double completion is visible rather than silent. Read by
//	                       internal/agentsvc; the socket methods (T10) read the same
//	                       rows, and the chain table arrives with 0022 for T5.
//	S15 plugins            CreatePluginAgent · TransitionPluginAgent ·
//	                       PluginAgentState · PluginAgentsAll ·
//	                       PluginAgentTransitions — S15's
//	                       four states as rows (0023): the live-uniqueness is
//	                       the partial unique index (a re-import after revoke
//	                       is a NEW record, the old life stays readable), the
//	                       transition is UPDATE-guarded and ledger-inserted in
//	                       ONE transaction, and revoked is terminal at the
//	                       database itself (no admitted transition pair names
//	                       it as FROM, and a trigger aborts a raw UPDATE out
//	                       of it). Read and written through
//	                       internal/plugin's StoreBinding; the CLI verbs (T7)
//	                       and the daemon boot reach them through it.
//	                       CreatePluginRefusal · PluginRefusals — the arrival
//	                       refusals AP7 demands (0024): the cost RECORDED,
//	                       never recovered, reached_prompt written false
//	                       because a refusal that reached a prompt is a bug,
//	                       not a record. Written by internal/plugin's
//	                       ArrivalGate at the completion point.
//	                       (0024 also widens agent_task's status CHECK to
//	                       admit refused — MarkAgentTaskTerminal is the one
//	                       guarded mover, terminal-with-completed_at like
//	                       completed and failed.)
//	S31 steer              QueueSteer · PendingSteers · ListSteers · ReadSteer ·
//	                       CancelSteer · CheckpointWithSteers — the durable
//	                       steer queue (D4, LW5). A steer typed into a running
//	                       step is a row (`pending` → `delivered` |
//	                       `cancelled`), and its
//	                       delivery is recorded in the SAME transaction as the
//	                       checkpoint containing it, so S7's cold replay meets
//	                       either "not delivered" (re-drain) or "in the
//	                       transcript and delivered" — never both, never
//	                       neither. The runtime drains through PendingSteers and
//	                       persists through CheckpointWithSteers; `step.steer`,
//	                       `step.steers` and `step.steer.cancel` are the door a
//	                       person queues and withdraws through, and that door is
//	                       also where LW4's "the step is not running, use answer /
//	                       step.continue" refusal and LW10's bounds are stated —
//	                       the store keeps the row honest (steers.go) and leaves
//	                       the remedy to the surface that knows it. CancelSteer
//	                       and CheckpointWithSteers answer with RESULTS: a
//	                       refused transition names the state found, and an error
//	                       from this file means the write failed, nothing else.
//	S31 stop               ContinueStepWithMessages — `step.continue {message}`
//	                       (D6): the `suspended`→`running` flip and the
//	                       transcript replacement land in ONE transaction, so
//	                       the person's message is delivered exactly once —
//	                       a crash cannot lose it (the running row already
//	                       carries it for recovery) and a retry cannot double
//	                       it (the same guard refuses the second continue).
//	S8  blueprints         WriteDocument · ReadDocument · ListDocuments ·
//	                       DocumentRevisions · CreatePinnedLoop · ReadPinnedLoop ·
//	                       SavePinnedRuntime · PinnedLoopsByStatus · CommitGate · AnswerGate
//	S11 attendedness       LoopAttended — the daemon adapter's read of the D2
//	                       pin-time declaration (written once by CreatePinnedLoop
//	                       at start; nothing else sets it)
//	S19 T6 outward opt-in  LoopAllowOutward — the loop runtime's read of the
//	                       per-loop, at-start declaration (written once beside
//	                       attendedness by both creation paths; nothing else
//	                       sets it; lifts outward only, never irreversible)
//	S20 trigger rows       SaveTriggerSchedule · TriggerSchedule ·
//	                       TriggerSchedules · SetTriggerCovered ·
//	                       SetTriggerPaused · RecordTriggerFire ·
//	                       TriggerFireHistory · LastFiredLoopBefore ·
//	                       SaveTriggerGap · TriggerGaps —
//	                       the schedule rows the
//	                       scheduler answers (daemon triggerRows adapter)
//	                       and the fire answers it records; pause is the
//	                       row flag the CLI (T8) flips, never a narrow
//	                       surface (TR12). The covered_until frontier is
//	                       what makes each instant ONE question.
//	S20 loop provenance    SetLoopProvenance · LoopStartedBy — who started
//	                       the loop ("trigger"/"webhook" beside the
//	                       schedule id), written by the daemon's fire path
//	                       beside the pin, in columns OWNED apart from
//	                       attendedness (R-3): a fired loop reads attended
//	                       false and started_by trigger as two answers to
//	                       two questions. "" is honestly a person's loop.
//	S3's routes, kept here  UpsertRoute · ListRoutes · ReadRoute · DeleteRoute
//	                       The configuration only. The credential is a 0600
//	                       file written by internal/secrets (C17), and there is
//	                       no column it could go in — a test reads the
//	                       database's raw bytes to prove it
//	S18 T3 probe cache     PutProbeResults · ProbeResults — the `route_probe`
//	                       verdicts that survive a restart (verified and
//	                       contradicted only; `declared` is the absence of a
//	                       row, I5). Consumers: the daemon wires them to the
//	                       socket's routes.probe (which stores the verdicts a
//	                       probe earned) and routes.list (which renders the
//	                       cached states). DeleteRoute clears a route's rows
//	                       with the route itself.
//	S18 T5 drop count      SetStepThinkingDropped — the step's total of
//	                       thinking blocks withheld from a wire because their
//	                       producer is not that provider (M4/O1). The ONE
//	                       writer of `thinking_dropped`; the daemon's step
//	                       runner files it at completion when blocks
//	                       actually fell. Read back by ReadStep/ListSteps.
//	S18 T7 ledger read     LoopCostRows — one row per step carrying its Cost
//	                       (from the columns, never inferred) and the
//	                       provenance of its LAST assistant turn, the shape
//	                       model.AttributeCosts groups into the per-route
//	                       ledger the CLI table and `loop.show` both render
//	                       (M26, D16). A read of rows and nothing else: the
//	                       store computes no money (K22).
//	S10 policy             PolicyAdd · PolicyRemove · PolicyListAll — the SQL
//	                       text for the `policy` table (T6). The authorship
//	                       rule (PL1) is NOT here: its single home is
//	                       internal/policy/store.go, the one write boundary
//	                       (D3, grep-enforced); these methods are the
//	                       infrastructure it stands on. No expiry filter —
//	                       Evaluate owns that (D6). The daemon's reads and
//	                       `aeon policy` (T12) reach them through the
//	                       internal/policy adapter, never directly.
//	                       PolicyAcceptanceRecord · PolicyAcceptanceListFor —
//	                       the SQL text for `policy_acceptance` (T7), the
//	                       "who was told, since when" record: recording is
//	                       idempotent (ON CONFLICT (policy_id,target_kind,
//	                       target_id) DO NOTHING keeps the FIRST instant and
//	                       makes a re-record a no-op, never an error) and the
//	                       columns are identifier+timestamp only (legal
//	                       constraint 2). Enforcement NEVER consults this
//	                       table — PL10, an unaccepted policy is still
//	                       enforced, pinned by a grep in record_test.go;
//	                       these two methods serve the audit question, and
//	                       surfaces reach them only through the internal/
//	                       policy record boundary (record.go), never direct.
//	                       PolicyEventInsert · PolicyEventQuery ·
//	                       PolicyEventTrim · PolicyEventStats — the SQL text
//	                       for `policy_event` (T8), the refusal audit trail:
//	                       the INSERT has exactly ONE caller (record.go's
//	                       unexported recordEvent — the sole writer; detail
//	                       is bound to 256 runes ON WRITE there, content-
//	                       free, legal constraint 1) and no surface reaches
//	                       even these through the internal/policy record/
//	                       retention boundary, never direct. The trim is
//	                       NEVER on the insert path — it runs on daemon
//	                       start and on demand (D15/PL11: age cap is the
//	                       named const policy.PolicyEventAgeCap, 90 days,
//	                       with depth/cap/oldest reportable through Stats).
//	S16 action intents     BeginActionIntent · RecordActionReview ·
//	                       OpenActionIntent — postReview's receipt, BEFORE the
//	                       effect and after it (T7's D7: ONE intent table so
//	                       recovery has one path). The consumer is the action
//	                       package's IntentStore port: the loop dispatches
//	                       forge.postReview through it, resume reads back
//	                       through it, and a crash between posting and
//	                       recording leaves review_id NULL — posted-unknown,
//	                       go-and-look, never posted-false (action_intent.go).
//	                       CountActionIntents — the audit action's read-only
//	                       count, wired by the daemon's action Services.
//	S16 git                UpsertComponent, and the loop branch columns
//	product projects        CreateComponent · UpdateComponent · ReadComponent · ListComponents
//	S32 parallel agents    ComponentMaxParallelAgents · SetComponentMaxParallelAgents — the cap on
//	                       component (PA1, default 1, range 1..16), read and set through the daemon's
//	                       component.list / component.update (P34, no new socket method); and the
//	                       daemon-made worktree ledger CreateLoopWorktree · ListLiveLoopWorktrees ·
//	                       RemoveLoopWorktree — PA5's write-ahead row, written before the git
//	                       worktree, settled once by a guarded UPDATE (removed_at IS NULL).
//	S38 org layers (now the component tree) CreateComponent · UpdateComponent · ReadComponent ·
//	                       ListComponents — a root with a child reads as a project, a root with
//	                       none as a component; a child hangs under its root's ParentID
//	                       (S44-extra). Consumers: the daemon's component wrappers and
//	                       `aeon component`'s verbs.
//	S44-extra component tree PromoteComponent · AttachComponent · DetachComponent ·
//	                       ArchiveComponent · RestoreComponent · ListChildren · RootOf ·
//	                       ComponentWriteFor · EffectiveComponent · EffectiveForRoot — the
//	                       tree (CT1-CT7): a root with a child reads as a project, a root
//	                       with none as a component; the five inheritable columns resolve
//	                       through effective.go only. Consumers: the daemon's component
//	                       wrappers and `aeon component`'s verbs.
//	S38 tasks              CreateManualTask · UpdateManualTask · SetManualTaskStatus ·
//	                       DecideTask · LinkTaskToLoop · ListTaskRows · ReadManualTaskBody —
//	                       OL14's second population: manual tasks share the table, the ladder
//	                       and the board; loop tasks stay the engine's (SetTaskStatus and the
//	                       loop key untouched, the update doors refuse them by sentence).
//	                       Consumers: the daemon's task methods (S38 T8), the stamped catalog
//	                       tools (S38 T9), and the board read's floor-flagged cost (P32).
//	S38 records            CreateEpic · UpdateEpic · GetEpic · ListEpics · CreateGoal ·
//	                       UpdateGoal · SubmitGoalForReview · GetGoal · ListGoals ·
//	                       DecideGoal · CreateBrief · GetBrief · LatestBrief · DecideBrief ·
//	                       ListReviews · Attach · ListAttachments · ComponentForRoot ·
//	                       LoopBlueprintID · GetGoalVersion · GetTaskByID · UpdateLoopTask ·
//	                       SetTaskEvidence · IsLoopWorkerOfTask · CountRecordsByStep ·
//	                       StepAttachments — the records tier
//	                       (OL7/OL8): every goal passes in_review, a decision is CLAIMED
//	                       FROM in one transaction by a human only, and an attachment
//	                       pins a document REV and reports its currency. Consumers: the
//	                       daemon's record methods and `aeon record`'s verbs (S38 T8),
//	                       and the catalog tools that stamp source_step (S38 T9).
//	S39 runs               BeginRun · EndRun · ReadRun · CurrentRun · ListRuns ·
//	                       SetStepRun · SetRunKickoff · RunCost · LoopCost · CloseIdleLoop
//	                       (runs.go; loop.close's door: claims from idle only) — one immutable
//	                       run row per blueprint run (CR2); the pin stays ONE row per
//	                       loop meaning the current run's position; costs are the P32
//	                       floor query, never a stored total. CreatePinnedLoop writes
//	                       run 1 beside the pin, in the same transaction.
//	S40 outbox             ReadAutomationEvents · ReadAutomationEvent · AutomationHead ·
//	                       AutomationCursor · AdvanceAutomationCursor — the transactional
//	                       outbox (AU4): the four events ride their domain write's
//	                       transaction, inserted only by automation.go's one helper
//	                       (TestTheHelperIsTheOnlyWriterOfTheOutbox). Consumers: the S40
//	                       dispatcher (T5 reads after the cursor, T6 owns it) — the cursor
//	                       moves FORWARD ONLY, so a dispatcher that died mid-pass re-claims
//	                       nothing (fire's NULL outcome is the other half of that claim).
//	S40 dispatch reads     EventTriggerSpend · CountRecentEventFires · OverlappingEventFire —
//	                       AU11's three-state spend (a floor is a floor; unknown is NEVER
//	                       summed as zero), the hourly window that counts started fires and
//	                       open claims alike, and S20's StillGoing set copied into SQL — the
//	                       agreement is pinned by a test in internal/automation, not here.
//	                       Consumer: the S40 dispatcher (T5's guards, T6's Reconcile).
//	S40 triggers           CreateEventTrigger · UpdateEventTrigger · SetEventTriggerEnabled ·
//	                       GetEventTrigger · ListEventTriggers · ListEventTriggersFor ·
//	                       ClaimEventFire · DecideEventFire · RecordEventFire · GetEventFire ·
//	                       OpenEventFires · ListEventFires · EventFireCounts — the human
//	                       standing instructions (AU1: every write refuses a non-person BEFORE
//	                       SQL; AU1: disabled with a reason, never deleted) and the fire
//	                       ledger (AU8: the UNIQUE IS the claim — outcome NULL is in flight,
//                       deciding is claimed-FROM; AU12: five outcomes, closed reason codes,
//	                       all five always counted). Consumers: the S40 dispatcher (T5/T6)
//	                       and the automation.* socket methods (T7/T8).
//	S49 automation writes  SaveTriggerScheduleAs · ArchiveAutomation ·
//	                       UnarchiveAutomation · RecordAutomationAudit ·
//	                       LatestTriggerFireID: the human write paths that
//	                       carry their automation_change audit inside the
//	                       write's own transaction (AN3) and refuse a
//	                       non-person BEFORE any SQL (AU1's third lock);
//	                       fire-now audits beside its recorded fire row by
//	                       id. Consumers: the daemon's one write file
//	                       internal/daemon/automation_write.go (T5 pins it
//	                       the sole caller) — the trigger engine itself
//	                       (clock.go, byte-frozen) still reaches only the
//	                       plain SaveTriggerSchedule above.
//	S49 automation mgmt    TriggerSchedulesScoped · AutomationChangeList ·
//	                       AutomationFires · AutomationRuns (+ the package-level
//	                       AutomationChangeInsertTx · Schedule/Event Archive/
//	                       UnarchiveTx · SetScheduleCreatorTx, tx-bound for the
//	                       write service's own transaction): the append-only
//	                       authority audit (AN4), the archive state that makes a
//	                       trigger engine-invisible without the engines learning
//	                       the word (AN6), and the unified fire view / runs reads
//	                       (AN5). Consumers: the S49 write service and the
//	                       automation.* reads (T3/T4), the CLI (T8) — never an
//	                       agent tool, never a non-person actor (AU1).
//	S63 attention          PendingQuestionsForScope · BlockedLoopsForScope ·
//	                       StoppedLoopsForScope · LatestBlockedReasons ·
//	                       LiveComponentTree — the scope-scoped reads of what
//	                       a person's attention is owed: open questions,
//	                       blocked and stopped loops with their newest
//	                       reasons, and the live component tree the scope
//	                       filter walks. Consumers: internal/attention and
//	                       the supervision reads that compose it (S63).
//	S22 index              EnsureRetrievalCorpus · RetrievalCorpusRow · SetRetrievalCorpusEmbedding ·
//	                       RetrievalChunkCount · InsertRetrievalChunk · RetrievalChunkRowsForSource ·
//	                       DeleteRetrievalChunksForSourceExcept · DeleteRetrievalChunksForCorpus ·
//	                       DropRetrievalVecTable · ResetRetrievalCorpusEmbedding · KeywordSearchRetrieval ·
//	                       RetrievalChunksForEmbedding · VecChunkIDs · UpsertRetrievalVec ·
//	                       DeleteRetrievalVec · VectorSearchRetrieval ·
//	                       UpsertSourceMtime · SourceMtime (S71 T1: the per-source mtime
//	                       record the status walk's cheap gate reads and writes — a moved or
//	                       absent record is a candidate, a matching one is assumed fresh, the
//	                       hash not the mtime deciding staleness) (T4: k-NN over vec0,
//	                       distance crossing as its negation for the same
//	                       bigger-is-better reason bm25 crosses) — the vector side T3 grows (delete-then-insert
//	                       is the upsert: two vectors for one chunk would let it retrieve
//	                       twice for the same code; a missing vec0 table in VecChunkIDs
//	                       means "nothing embedded yet", not error) ·
//	                       EnsureRetrievalVecTable — the retrieval index (S22 T1): the corpus row
//	                       carries the KIND (data, not tables — Stage 5's concluded memory is a
//	                       row and a validator entry), the reserved provenance/observation_count
//	                       columns their CHECK makes mandatory for a belief, and the embedding
//	                       model+dimension V6 makes DATA (a query compares the route against
//                       this row; the vec0 table arrives on demand because its dimension is
//                       fixed at CREATE and the migration cannot know a route). The chunk row
//                       carries the file:line range, the content hash (V8) and the indexed
//                       body no query returns (V4, D8 — RetrievalHit has nowhere to put one);
//                       the rows-for-source and prune-except pair are T2's staleness comparison
//                       and its incremental delete (a rewrite that wiped first would report the
//                       file empty between wipe and write).
//                       The root filters every statement (V12 — a worktree is a distinct root).
//                       Consumer: the internal/retrieval domain (T2–T8), which holds no SQL of
//                       its own; with the package unbuilt these rows sit inert (D1 — optional
//                       means the rows are harmless, not absent-by-schema).
//	S24 queue              AppendQueueItem · QueueItemByID · ListQueueItems ·
//	                       TransitionQueueItem · ReorderQueueItems ·
//	                       NextReadyQueueItem · QueueItemByLoopSlug ·
//	                       SetQueueItemLoop · ComponentQueueSettings ·
//	                       SetComponentQueueSettings ·
//	                       LatestLoopGateQuestion · SizeAndReadyQueueItem ·
//	                       FirstPendingQueueItem — the per-project
//                       backlog rows (S24 T1's table; T2's doors plus
//                       T3's reactor reads — the loop-pointer stamp and
//                       the component settings read/write — and T4's
//                       replay discriminator at the answer door). The vocabulary
//                       is NOT re-listed here: T1's CHECK closes it at the
//                       database and internal/queue owns the machine — a
//                       Go mirror of the seven would be the second truth
//                       the slice forbids. Transitions are claimed-FROM
//                       (machine.go's discipline): two reactors, one
//                       winner, an honest loser. Consumers: internal/queue
//                       (the one door), then the reactor, the CLI verbs
//                       and the queue.* socket twins behind it.
//	S26 roadmap            CreateRoadmapItem · RoadmapItemByID ·
//	                       RoadmapItemByLoopSlug · ListRoadmapItems ·
//	                       CountRoadmapItems · TransitionRoadmapItem ·
//	                       SetRoadmapItemArtifacts · SetRoadmapItemEstimate ·
//	                       SetRoadmapItemPriority · SetRoadmapItemLoop —
//                       the forward-Gantt rows (S26 T1's table; T2's door
//                       writers plus T4's scoped loop stamp). As with the
//                       queue, the vocabulary is NOT re-listed: migration
//                       0043's CHECKs close it and internal/roadmap owns
//                       the machine — a Go mirror would be the slice's
//                       forbidden second truth. Transitions are
//                       claimed-FROM: a lost race is Refusal.Moved, an
//                       honest loser. Consumer: internal/roadmap (the one
//                       door), then the dispatch, the reactor, the CLI
//                       verbs and the roadmap.* socket twins behind it.
//	S29 triage             CreateBug · BugByID · ListBugs · CountBugs ·
//	                       TransitionBug · SetBugPriority · SetBugDuplicateOf ·
//	                       SetBugRoadmapItemID — the bug backlog rows (0044's
//	                       table). The two closed sets (validation D1, priority
//	                       D2) are NOT re-listed: migration 0044's CHECKs close
//	                       them and internal/triage owns the machine — a Go
//	                       mirror would be the slice's forbidden second truth.
//	                       SetBugDuplicateOf is the ONE write path (D5/N3 —
//	                       only a human confirmation; the detector never
//	                       writes it), and SetBugRoadmapItemID records the S26
//	                       hand-off (D8) once it lands. Consumer:
//	                       internal/triage (the one door), then the daemon
//	                       dispatch + reactor, the CLI verbs and the triage.*
//	                       socket twins behind it.
//	S29 triage vec0       EnsureBugVecTable · UpsertBugVec · QueryBugVec —
//	                       the on-demand vec0 nearest-neighbour (the bug's
//	                       embedding index, D6/N2: dimension is data, fixed at
//	                       CREATE, never a migration — the S22/0040 pattern).
//	                       A bug with no embedding yet is a normal state, not a
//	                       missing row. Consumer: internal/triage's
//	                       SuggestCandidates (the detector, T4), the one
//	                       reader; the detector is a read and never writes
//	                       duplicate_of (D5/N3).
//	S30 graph              InsertGraphNode · InsertGraphEdge · CountGraphNodes ·
//	                       CountGraphEdges ·
//	                       GraphNodesByName · GraphNodesBySymbol ·
//	                       GraphNodesMatch · GraphNodeByID ·
//	                       GraphEdgesFrom · GraphEdgesTo · GraphEdgesTouchingFile ·
//	                       DeleteGraphFile · DeleteGraph · GraphRoots ·
//	                       GraphFilesIn · DeleteGraphAudited ·
//	                       DeleteGraphFileAudited (S54 T1's human-door
//	                       twins — the deletes that append their
//	                       platform_change in the SAME tx) — the code
//	                       graph's rows (S30 T1's
//	                       graph_node and graph_edge; T1's door). As with the
//	                       queue and the roadmap, the vocabulary is NOT
//	                       re-listed here: the migration's CHECKs close the six
//	                       relations and extracted/inferred, and internal/graph
//	                       owns the meaning (the four-state result is a property
//	                       of the query layer, not of these rows). Consumers:
//	                       internal/graph (the one door), then the graph tool,
//	                       the graph.* socket twins and the bench page behind it.
//	S64 async workers      InsertAsyncWorker · GetAsyncWorker · ListAsyncWorkers ·
//	                       StartAsyncWorker · ProgressAsyncWorker ·
//	                       FinishAsyncWorker · RequestCancelAsyncWorker ·
//	                       MarkAsyncWorkersInterrupted ·
//	                       InsertAsyncWorkerAudited (S54 T1: worker id,
//	                       ledger row and the audit append in ONE tx —
//	                       the collision rolls back both) — the
//	                       async-worker ledger
//	                       (S64 T1's async_worker). The durable single flight is
//	                       the partial unique index, the state vocabulary is the
//	                       migration's CHECK, and internal/asyncworkers owns the
//	                       meaning. Consumers: internal/asyncworkers (the one
//	                       door), then the asyncworkers.* socket reads and the
//	                       daemon's recovery/shutdown hooks behind it.
//	S54 platform audit   PlatformChangeList · PlatformChangeStarter ·
//	                       PlatformWithTx · InsertAsyncWorkerAudited ·
//	                       DeleteGraphFileAudited · DeleteGraphAudited — the
//	                       append-only platform_change pair and the
//	                       audited twins (S54 T1): the insert rides ONE
//	                       transaction with the worker row it explains
//	                       (a collision writes NEITHER), PlatformWithTx is
//	                       the one door that transaction opens, and the
//	                       graph deletes grew actor-stamped siblings so an
//	                       operator's destructive verb is legible in the
//	                       audit. Consumers: the platform_change protocol
//	                       doors (internal/protocol/platform_door.go), the
//	                       asyncworkers manager's audited starts and the
//	                       daemon's ActorFor stamp, and internal/graph's
//	                       audited delete/reparse twins. — NAMED LATE:
//	                       S54 T1 landed these six exports on main without
//	                       this row (its targeted subset missed the guard,
//	                       the same miss this slice paid for
//	                       ListMergeItemsAcrossRoots); S60 T9's landing
//	                       merge carries the payment forward.
//	S70 mailbox            QueueAgentMessage — the ONE door an agent's
//	                       addressed message is filed (S70 MB3): addressed,
//	                       never self, caps counted against the rows in the
//	                       writing transaction, a thread bootstrapped to its
//	                       own id, no update or delete verb anywhere.
//	                       DeliverAgentMessage — the drain's write-once
//	                       stamp (the steer's discipline). PendingForAgent —
//	                       the boundary drain's read. ThreadAgentMessages /
//	                       LoopAgentMessages — the conversation reads behind
//	                       agent.messages (MB7); a missing thread is
//	                       answered, never silently empty.
//	                       CheckpointWithMailbox — the D4 join: a fold of
//	                       peer words and their delivered stamps are ONE
//	                       transaction (CheckpointWithSteers' law, quoted);
//	                       a named row from another loop or another
//	                       addressee rolls the whole thing back naming what
//	                       was wrong (MB2's delivery end). Consumers:
//	                       internal/runtime's ask seam and boundary drain
//	                       (S70 T2), the socket read (S70 T3) and the CLI
//	                       thin client (S70 T5); the lane and policy checks
//	                       live above this door, never beside it.
//	                       RepliesOwedForStep / PendingAsksFromStep — the
//	                       round trip's row reads (S70 T2d): what a
//	                       completed step owes as answers (the delivery
//	                       pair names the receiver; the result_of link is
//	                       the settlement) and which wait:true asks no
//	                       boundary has drained. RunningStepForAgent —
//	                       MB4's dispatch question answered from rows.
//	                       Consumers: internal/daemon's completion hooks
//	                       and peer-turn dispatcher.
//	S69 attachments        PutAttachment — the ONE door attachment bytes
//	                       cross (S69 AT3): caps, allow-list, the magic-byte
//	                       verdict, then a frozen @attachments/ revision
//	                       (dedup by sha answers the same revision).
//	                       ReadAttachment — the pinned-revision read behind
//	                       the attachment.read door and the daemon's
//	                       send-time expansion; it reads nothing outside the
//	                       reserved namespace. Consumers: internal/daemon's
//	                       attachment doors; a missing revision is
//	                       ErrNotFound naming itself, never an empty answer
//	                       (P31).
//	S68 chosen skills      ReadChatMessage — S68 (B6) reloads a launch's
//	                       chosen-skills receipt from the run's own kickoff
//	                       row after a restart, so post-restart dispatch
//	                       renders what the launch chose rather than
//	                       nothing. Consumer: internal/daemon's chosen-skills
//	                       door (chosen_skills.go); absence is ErrNotFound,
//	                       a corrupt receipt an error, never a silent empty
//	                       list (P31).
//	S28 learnings          CreateLearning · GetLearning · ListLearnings ·
//	                       QueryLearnings · EnsureLearningVecTable ·
//	                       UpsertLearningVec · DeleteLearningVec ·
//	                       LearningVecIDs · VectorSearchLearning ·
//	                       ReadLearningVectors (the stored vectors of one
//	                       geometry, read back for the knowledge map —
//	                       consumer: internal/learnings' Map) — the
//	                       classified, queryable record
//	                       of what an agent, a project or a person has
//	                       learned (S28, a third scope, cross-project,
//	                       design D1). The agent scope carries NO foreign
//	                       key (design §3): a learning's scope_key is plain
//	                       TEXT and the Go-level check lives in
//	                       internal/learnings, the one place the check is —
//	                       SQLite cannot express "a valid agent," so the
//	                       handle carries no FK to invent a wrong truth.
//	                       Since S52 the check reads the S43 SLUG (AgentBySlug
//	                       answers for the agent; a plugin_agent's revoked
//	                       state is joined by NAME because plugin rows key
//	                       on names — revoked refuses, no plugin row is not
//	                       "missing," it is "first-party").
//	                       QueryLearnings is the tag-filtered read (D5):
//	                       the filter runs first and always, and the set
//	                       it returns is the one internal/learnings ranks
//	                       by embedding (T3) — the store never widens it,
//	                       so a learning the tag filter excluded is never
//	                       reachable through similarity (D6). The vec0
//	                       (learning_vec) is NOT a migration (its embedding
//	                       dimension is data, fixed at CREATE time) — it
//	                       arrives on demand, S22's retrieval precedent; a
//	                       learning with no configured route has no row,
//	                       and that is the normal state, not a gap (D5).
//	                       Consumer: internal/learnings (the one door),
//	                       then the CLI and the learnings.* socket twins
//	                       behind it.
//	S52 learning management CreateLearning (legacy wrapper, no audit — the
//	                       fixture/legacy shape, listed so its existence is
//	                       a named fact) · CreateLearningAudited (the ONE
//	                       writer: learning + tags + its one audit line in
//	                       one transaction) · ListLearningChanges ·
//	                       CountAllLearningChanges (the append-only
//	                       learning_change audit's reads) · LearningsScopedTo
//	                       (the count agentmgr's delete refuses on) ·
//	                       MigrateLearningAgentScopeKeys (LM1, the Go step
//	                       riding Migrate(), all-or-nothing, refusal naming
//	                       every stray key) · SetLearningRetraction (the
//	                       LM2 stamp + its audit line in one transaction;
//	                       answers found/changed, idempotent without a
//	                       second audit row) · DropLearningVectors (LM5's
//	                       hygiene: one learning's vectors out of every
//	                       learning_vec table, shadows skipped) ·
//	                       AgentProjectRoots (LM5's scope resolution: the
//	                       roots this agent has run under, read from the
//	                       step rows themselves) · LearningWithTx (T3's
//	                       probe seam: the guards' only honest way to
//	                       speak through the learning tables' CHECKs and
//	                       append-only triggers — twins BugWithTx) ·
//	                       LearningAgentAddCountSince (S52 T4's self-log
//	                       rate count: this agent's add audit lines inside
//	                       a window, read off the ledger, never beside
//	                       it).
//	                       Consumers: internal/learnings,
//	                       cmd/aeon/memory_guard_test.go
//	                       (AppendAs/AppendRelayedAs), internal/agentmgr,
//	                       internal/store itself (Migrate).
//	S17 archive            ArchiveLoop · ReadArchive · ListArchives
//	S76 loop questions     StepQuestions · LoopQuestions — the questions one step (or the
//	                       whole loop) asked, answered and pending. Consumer:
//	                       internal/daemon/loop_questions.go.
//	document owner         AdoptDocuments · LoopGoal · GoalDocuments · ReadGoalDocument · LoopPinnedContent —
//	                       a goal owns the documents its work produced (document_owner.go).
//	                       Consumers: internal/daemon (goal_create's adoption, the doc_*
//	                       tools' DocStore and its path claims) and internal/loop (the
//	                       produces check).
//	variables              EnvVarGet · EnvVarList · EnvVarUpsert · EnvVarDelete ·
//	                       EnvVarRecordRead · EnvVarChanges — ciphertext and flags for
//	                       the variable manager (env_var.go). Sole consumer: internal/vault.
//	S75 loop todo          TodoRead · TodoReplace · TodoClose — the loop's persisted
//	                       todo list. Consumers: internal/daemon (the tools.TodoStore
//	                       adapter, the loop.todos reads and the person's close).
//	S19 the narrow surface ListLoops · ReadStep · Thread
//	S20 memory harvest     AppendMemory · ReadMemory
//	S25 history            ListLoops · ListSteps · ReadEvents
//	S42 loop transcript    ReadLoopTranscriptPage — a cross-step, keyset-paginated
//	                       window for a history view (distinct from ReadTranscript's
//	                       single-step replay unit, above). Consumer: internal/daemon/
//	                       memory.go's `loop.transcript` handler.
//	loop context           AllLoopQuestions — every question of a loop, gates included, in order
//	                       asked; the source of the loop's `answers.md` and its context index.
//	loop-replay journal    ReadLoopEvents — one loop's event journal as a window
//	                       (latest, before, after), read by the indexed loop_id column.
//	                       Consumer: internal/daemon/memory.go's `loop.events` handler.
//	step summaries         ReadStepEvents — the same window narrowed to one step; ReadStepSummaries and
//	                       ReadLoopTotals — one row per step (turns, tool calls, tokens, cost, time) kept as
//	                       events are appended, and their sum. Consumer: internal/daemon/memory.go's
//	                       `loop.steps` and step-scoped `loop.events` handlers.
//	loop lifecycle         SetLoopArchived / ArchivedLoops — hide a loop, keep everything; DeleteLoopKeepingStats /
//	                       ListLoopTombstones / DetachedLinkFor / ListDetachedLinks / ListAllDetachedLinks / ListWorkClosures — remove a loop, keep its totals
//	                       and its work. Consumer: internal/daemon/loop_lifecycle.go (`loop.setArchived`,
//	                       `loop.delete`, `loop.tombstones`).
//	loop title             SetLoopTitle — the short name made for a loop after it starts. Consumer:
//	                       internal/daemon/loop_title.go.
//	loop model options     LoopModelOptionGet / LoopModelOptionSet — the thinking and effort a person set on a
//	                       loop after it started. Consumer: internal/daemon/loop_model_option.go (`loop.options`
//	                       and the step dispatch).
//	S33 loop.archive door  ReadLoopByID — resolves a loop from its id alone for
//	                       the archive door (ReadPinnedLoop means BLUEPRINT-pinned,
//	                       and an ordinary loop.create loop has no blueprint row).
//	                       Consumer: internal/daemon/blueprint.go's loop.archive
//	                       wiring; the telemetry writes and reads ride their own
//	                       S33 row below.
//	S57 loop slug resolve  LoopsBySlug — the daemon's gate plane (T8)
//	                       resolves a loop slug to its root before it can
//	                       scope a read (verify.runs · merge.events ·
//	                       dod.list · the preview's loop): a slug is unique
//	                       per root, never global, so zero rows is
//	                       loop-unknown and more than one is
//	                       loop-slug-ambiguous — the service's typed
//	                       refusals, never an empty list. Consumer:
//	                       internal/daemon/verify_service.go ·
//	                       mergerule_service.go · dod_service.go.
//	S35 proposals          CreateProposal · ListProposals · DiscardProposal — T3's
//	                       socket handlers (proposal.create/list/discard); GetProposal ·
//	                       DecideProposal · SetProposalAppliedError — T5's CLI-only
//	                       commit/reject, no socket caller (brief §7). The slice's one
//	                       store addition: an inert row no guard read path may ever
//	                       learn the name of (brief §4).
//	S33 telemetry          RecordStepTurn · RecordStepCompaction · RecordFileActivity ·
//	                       RecordCommandActivity — the loop-tier detail rows, each
//	                       bumping component/project/workspace in its own
//	                       transaction (TL2: the bump lives IN the write, never a
//	                       second pass); ComponentTelemetry · ProjectTelemetry ·
//	                       WorkspaceTelemetry · StepTurns · StepCompactions ·
//	                       StepFileActivities · StepCommandActivities ·
//	                       CumulativeTokensTotal · StepContextManifest — the reads
//	                       (TL10's four tiers and TL11's two numbers; the store
//	                       answers the cumulative from the rows, the protocol
//	                       handler answers the CURRENT estimate through runtime's
//	                       one estimator). Consumers:
//	                       internal/daemon/telemetry_seam.go (the production sink
//	                       adapter the runtime calls), then the telemetry.* socket
//	                       reads and the bench page behind them. The rollups are
//	                       the deliberate second home for SUMS ONLY — the loop
//	                       tier stays authoritative for the detail, and only one
//	                       of the two still exists after an archive (brief §3's
//	                       exception to S25's one-source rule).
//	S43 agent rows       AgentBySlug · AgentByName · AgentsAll · HasAgentSlug ·
//	                       CustomAgentSlugs (the agents a person made or imported — internal/daemon/agent_handler_source.go) ·
//	                       AgentReferencedByPolicy · BlueprintsReferencingAgent ·
//	                       MigrateAgentPolicyReferences — the agent CONTENT table
//	                       (S43 T1) and its row layer (AM1: every agent a row,
//	                       keyed by the immutable slug), plus the AM4 deletion
//	                       checks (a delete refused if a policy or a blueprint
//	                       still names the slug) and T2's name→slug policy
//	                       migration (AM2). The six content WRITERS this row used
//	                       to name (CreateAgent · UpdateAgent · DuplicateAgent ·
//	                       ImportAgentCard · DeleteAgentBySlug ·
//	                       MigrateFirstPartyAgents) are GONE — S73 replaced each
//	                       with an audited door, named in the S73 agent audit row
//	                       below; there is deliberately no un-audited way to write
//	                       an agent row (CU1). Consumer: internal/agentmgr (the one
//	                       domain door), then the agent.* daemon methods and the
//	                       `aeon agent` CLI behind it
//	                       (P34: the daemon is the primary surface).
//	S73 agent window       AgentPage · AgentChangesPage — the two paged WINDOWS
//	                       of the agent plane (S73 T2 and T5,
//	                       docs/aeon/PAGING.md PG1–PG5), in their own file
//	                       because a window query and the row's own shape are
//	                       different reasons to read this package.
//	                       AgentRow is the nine columns a management table may
//	                       carry (no prompt, no skills, no metadata — AR5) and
//	                       AgentRowBounds is the one literal holding the sortable
//	                       wire names, their columns, the size clamp and the
//	                       default sort, shared by the door and the query.
//	                       AgentChangesPage reads the LEDGER beside them —
//	                       newest first, narrowed by one slug or by nothing,
//	                       with AgentChangeBounds deliberately holding NO sort
//	                       vocabulary (a history has one order, AR6).
//	                       Consumers: the agent.rows and agent.changes daemon
//	                       methods (cmd/aeon/agent_rows.go) behind the management
//	                       seam — the Configuration page's table and its History
//	                       tab, and nothing else. The roster above (AgentsAll) is
//	                       a DIFFERENT question — identity for dispatch, whole,
//	                       unpaged — and S73 leaves it alone by design.
//	S73 agent audit      CreateAgentAudited · DuplicateAgentAudited ·
//	                       UpdateAgentAudited · DeleteAgentAudited ·
//	                       ImportAgentCardAudited · MigrateFirstPartyAgentsAudited ·
//	                       AgentWithTx · AgentChangeInsertTx — the six DOORS that
//	                       write an agent row and its agent_change ledger row in
//	                       ONE transaction (S73 CU1/AR1: an agent change nobody
//	                       recorded cannot exist, and the mcp_change precedent
//	                       means the pair commits together or not at all). Each
//	                       door takes the actor as a parameter (store.AgentActor:
//	                       the records tier's store.Actor cannot say `cli` or
//	                       `system`) and validates it BEFORE Begin, so a bad actor
//	                       writes nothing. The unexported createAgentTx ·
//	                       updateAgentTx · deleteAgentTx are the bodies;
//	                       AgentWithTx is the same door open to the seed applier,
//	                       which writes on a transaction the daemon owns, and
//	                       AgentChangeInsertTx is the ledger primitive under it.
//	                       The ledger itself is append-only by trigger, and
//	                       SeedAgentInsert · SeedAgentUpdate · SeedAgentRetire
//	                       mint their own `seed` rows on the caller's transaction.
//	                       Consumer: internal/agentmgr (the domain door for the
//	                       agent.* writes), internal/agentseed (the applier),
//	                       internal/daemon + internal/policyseed (the seed corpus),
//	                       and cmd/aeon's direct doors — never a raw writer.
//	the daemon             AppendEvent · ReadEvents · HeadSeq — the seq S2
//	                       deferred to this slice
//	S43-extra seed ledger  SeedLedgerList · SeedLedgerGet · SeedWithTx ·
//	                       SeedEventList — the seed_ledger and seed_event tables
//	                       (S43-extra T2): the ledger a set's apply is recorded
//	                       in and the append-only audit (SD3, SD10). SeedWithTx
//	                       is the shared transaction a domain row and its ledger
//	                       and audit rows commit on (SD3); the unexported
//	                       putLedger/insertEvent run the writes on it. Consumer:
//	                       internal/seed's Repo (the daemon adapts it, T6) and
//	                       the seed.* daemon methods behind it (P34).
//	S43-extra agent row   SeedAgentInsert · SeedAgentUpdate · SeedAgentRow ·
//	                       SeedAgentExists · SeedAgentRetire — the agent row's
//	                       content write/read (S43-extra T4/T6): the SQL the seed
//	                       applier needs, kept here so no SQL lives outside
//	                       internal/store (the TestNoSQLLivesOutsideTheStore
//	                       guard). The applier (internal/agentseed) holds the
//	                       *Store and calls these on the shared transaction; the
//	                       daemon adapts the transaction.
//	S44 config store     SettingGet · SettingList · SettingPut · SettingDelete ·
//	                       SettingEventList — the SQL text for the `setting` and
//	                       `setting_event` tables (S44 T1): a global key/value store
//	                       with append-only audit and no domain rule (I2 — the rule
//	                       lives upstairs behind a repo seam, and this file imports
//	                       nothing from internal/config), no scope, type or sensitive
//	                       column and no FK from the audit to the row. The key
//	                       grammar (CS3) and the 4096 cap are CHECKs the table
//	                       enforces; the two append triggers (CS8) make the audit
//	                       un-rewritable. Consumer: the config domain (the one door),
//	                       then the daemon methods and the `aeon config` CLI behind it.
//	S46 mcp rows          McpRows · McpServerGet · McpIsEmpty · McpIsShared · McpWithTx ·
//	                       McpChangeList · McpImportByHash ·
//	                       McpImportLatestByPath · McpReferencesFor —
//	                       the SQL text for the six MCP tables (S46 T1, MG1):
//	                       mcp_server / mcp_server_tool / mcp_agent / mcp_declare
//	                       + append-only mcp_change + the mcp_import adoption
//	                       ledger (MG3). McpRows is the ONE consistent read
//	                       Assemble (internal/mcp, T2) consumes; McpWithTx is
//	                       the shared transaction rows and their audit row
//	                       commit on (the SeedWithTx idiom); the package-level
//	                       *Tx helpers run the writes on it. Consumer: S46 T3's
//	                       admin seam in cmd/aeon (the one door holding both
//	                       sides of MG2's slug↔name map) and T5's human-door
//	                       handlers behind it (P34).
//	S45 policy audit     PolicyChangeInsert · PolicyChangeInsertTx ·
//	                       PolicyChangeList · PolicyChangeCount — the SQL text for
//	                       the `policy_change` authoring audit (S45 T3, PM8): one
//	                       append-only row per policy authoring write (add/remove/
//	                       seed/commit/reject) naming the peer-stamped actor, a
//	                       SEPARATE table from policy_event so retention (D15) can
//	                       never trim it (TestPolicyChangeNotTrimmedByRetention),
//	                       no FK to `policy` so a removed rule's history outlives it,
//	                       and UPDATE/DELETE refused by trigger (the S44 setting_event
//	                       precedent). NO PRODUCTION WRITER YET: the two Insert paths
//	                       are created callable for T4/T5's authoring doors to call
//	                       beside the writes they record (a test pins that nothing
//	                       outside tests calls them). The two readers already have a
//	                       consumer: the protocol's `policy.changes` read, reached
//	                       through internal/daemon's read-only seam (the read half is
//	                       PM8's audit door; the write half is the human door).
//	S45 policy seed      PolicyAddTx · PolicyDeleteTx · PolicySeedRow — the
//	                       transaction twins of PolicyAdd/PolicyRemove the
//	                       declared policy.corpus set's applier writes
//	                       through (S45 T12, PM15): the origin column is
//	                       carried into the INSERT only here, the
//	                       delete-then-insert replace runs on the seed
//	                       engine's shared transaction (SD3), and the read
//	                       answers Fingerprint and Adoptable with content
//	                       columns only (origin is provenance, not hashed
//	                       content). PolicySeedAuditTx — the ONE op='seed'
//	                       authoring-audit row an apply run of that set
//	                       appends, a NAMED sibling of PolicyChangeInsertTx
//	                       so TestPolicyChangeWritersAreOnlyTheAuthoringDoors
//	                       keeps pinning the two T3 Insert paths while the
//	                       seed door (internal/policyseed, S45 T12's allow-
//	                       listed caller) writes its own row. Consumers:
//	                       internal/policyseed through the write boundary's
//	                       tx hooks (policy.RepoFuncs AddTx/RemoveTx/
//	                       EventInsertTx — the last feeding
//	                       PolicyEventInsertTx, the authored record the
//	                       seed writes beside its row on one transaction)
//	                       and the corpus door for the audit row.
//	S55 loop control     ParkLoopStopped · ResumeLoop · CancelLoop ·
//	                       RaiseLoopToRunning · RaiseLoopToWaiting ·
//	                       ListControlEvents · AddControlRequest ·
//	                       DecideControlRequest · ListControlRequests — the loop
//	                       control core (T2/T3): ParkLoopStopped is a compare-and-set
//	                       on status='running' (a no-op writes no event, LC10) that
//	                       merges lastReason='stopped' into the pin; ResumeLoop and
//	                       CancelLoop read the status from the row and lastReason/
//	                       Flags from the pin in the SAME transaction (they are not
//	                       loop columns, LC1), and a refused transition is a typed
//	                       ControlRefusal, never an error (DB19). Cancel sets the
//	                       aborted FLAG — the engine's endStatus writes the terminal
//	                       (the one-writer rule, LC4); this file never writes a
//	                       terminal status. RaiseLoopToRunning and
//	                       RaiseLoopToWaiting are T7's two raise CAS doors (LC14):
//	                       both guard on status='blocked' AND lastReason='cap-hit'
//	                       read from the pin in the same transaction, and both
//	                       append a kind-'raise' control event — the running door
//	                       clears lastReason and capHit (an E1 fresh re-dispatch),
//	                       the waiting door keeps lastReason and clears only capHit
//	                       (an E2 same-row re-run; the step-continue machinery
//	                       re-wakes the parked step, no S31 change). ResumeLoop's
//	                       eligibility admits a crashed waiting/cap-hit pin
//	                       (the raise crashed after its CAS) as resummable.
//	                       AddControlRequest / DecideControlRequest /
//	                       ListControlRequests are the inert request desk (LC15): an
//	                       agent may file a stop request (capped), a person honors or
//	                       dismisses it once, and nothing halts until a person acts.
//	                       ListControlEvents is the append-only audit read (LC10).
//	                       Consumer: the daemon's loop control core (T3), the loop.*
//	                       socket methods (T10) and `aeon loop` (T11) behind it.
//	S55 budget         CreateBudget · UpdateBudget · RaiseBudget · RemoveBudget ·
//	                       GetBudget · ListBudgets · FindBudget ·
//	                       OpenLoopsForBudget · ApplicableBudgets ·
//	                       ApplicableBudgetsOrdered · ApplicableBudgetsFor ·
//	                       ApplicableBudgetsForOrdered · SpendFor · SpendSeries ·
//	                       SpendReport · RecordBudgetHit · RecordBudgetWarned ·
//	                       LastBudgetWarned · ListBudgetEvents — the budget records
//	                       (T6): a budget is a
//	                       running aggregate compared against several dimensions at
//	                       once (LC7), its own table, not a policy row. CRUD writes
//	                       a discriminated BudgetRefusal (DB19: a no-dimension, a
//	                       negative cap, a dangling subject, a duplicate, a workspace
//	                       subject name another subject), and every changing write
//	                       appends one budget_event (LC11, append-only; reads and
//	                       no-op updates append nothing). ApplicableBudgets resolves
//	                       the applicability chain (loop → goal → epic, the component
//	                       and its root via S44-extra RootOf, the workspace; disabled
//	                       excluded). SpendFor is the one spend function (LC9/LC11)
//	                       honouring P31 — an unknown is a COUNT, never a zero — a
//	                       lifetime component/workspace total from the telemetry
//	                       rollup (CT7: a root's row is the subtree total), a loop or
//	                       epic total an aggregate over the steps. FindBudget reads
//	                       the (scope, subject, window) key — the set door's
//	                       create-or-update discriminator and the raise door's
//	                       pre-read of the pinned budget. OpenLoopsForBudget is the
//	                       blast radius of a change: the loops still open under a
//	                       budget. ApplicableBudgetsOrdered and
//	                       ApplicableBudgetsForOrdered are the same chains in T7's
//	                       tightest-first order (LC9) — the walk the enforcement
//	                       engine and the budget.effective door both run, so the
//	                       read and the gate cannot disagree about what is in
//	                       force. SpendReport is T8's spend.report aggregation:
//	                       day / trigger / component / epic at the step's grain,
//	                       route at the turn's, each bucket carrying the P31
//	                       marks (the floor, its exactness, the partial and
//	                       unknown counts). ListBudgetEvents is the append-only
//	                       audit read (LC11) — budget.list and budget.show take
//	                       their lastChanged from it, and the history outlives the
//	                       budget. ApplicableBudgetsFor is the
//	                       applicability chain MINUS the loop's own row — the
//	                       budgets that govern a step the loop does not own (the
//	                       daemon's raise op resolves through it). RecordBudgetHit /
//	                       RecordBudgetWarned / LastBudgetWarned are T7's audit and
//	                       once-per-period warn state: a cap hit appends a
//	                       kind-'hit' budget_event carrying the spend floor and
//	                       whether it is exact, a warn appends a kind-'warned' row,
//	                       and LastBudgetWarned reads the newest warn per
//	                       (budget, dimension) so the engine warns at most once a
//	                       window (lifetime any row, day only within 24h). Nothing
//	                       enforces a cap here — that is T7's engine. Consumer: the
//	                       budget engine (T7), the budget.* socket methods (T8) and
//	                       `aeon budget` (T8) behind it.
//	S56 autonomy grants   CreateAutonomyGrant · RevokeAutonomyGrant · ReadAutonomyGrant ·
//	                       ListAutonomyGrants · AutonomyChanges · AutonomyUses · AutonomyRev ·
//	                       CountAutonomyUses · CountAutonomyUsesSince · RecordAutonomyUse ·
//	                       AutonomyLive · PinnedLoopsForGrant · ReadAutonomyPin — the store's
//	                       SQL half for per-root-component autonomy grants (AL10). The grant
//	                       and its change row are written in one transaction; the change
//	                       summary is a fixed template from ids and enums, never the note
//	                       (SEC-033). AutonomyLive is the one read the decision site makes
//	                       (AL9): the pinned ceiling plus the live floor (revoked / expired /
//	                       a veto on the root or the loop's component) — facts, not a verdict
//	                       (the pure side decides). CountAutonomyUsesSince is the decision
//	                       site's uses seam (AL4, R1): the maxPerDay read the blueprint
//	                       adapter calls per grant (CountAutonomyUses is its since-string
//	                       twin, the one the listing uses). PinnedLoopsForGrant names the
//	                       loops a grant pins (revoke/deny report which loops are affected,
//	                       show renders them), and ReadAutonomyPin reads back the pin a loop
//	                       was started with (AL8) — the pinned view's ceiling half; the pin
//	                       row itself is written beside the loop by CreatePinnedLoop (S8's
//	                       row above). A non-user/non-cli actor is refused before any SQL
//	                       (AU1 lock 3). Consumer: the daemon's decision site (T5), the
//	                       autonomy.* human door (T6) and `aeon autonomy` (T9). No Go
//	                       outside the store names a table (I2).
//	S55 watchdog       WatchdogSet · WatchdogSetTx · WatchdogRestoreTx ·
//	                       FindWatchdogRuleTx ·
//	                       WatchdogSetEnabled ·
//	                       WatchdogRemove · ListWatchdogRules · GetWatchdogRule ·
//	                       FindWatchdogRule · ListWatchdogEvents ·
//	                       WatchdogFireStats · ListWatchdogFires ·
//	                       WatchdogLastActivity ·
//	                       WatchdogPositionDispatches · WatchdogReasonRun ·
//	                       WatchdogRunningStep · WatchdogSignalAvailability ·
//	                       BlockLoopStuck · AppendWatchdogFire — the watchdog
//	                       records and their moves (T9): a rule is a record an
//	                       operator can list, edit, disable and remove (LC18), and
//	                       the seeded default set rides the S43-extra seed engine
//	                       through WatchdogSetTx on its own transaction. The write
//	                       paths are create-or-update by the unique key
//	                       (scope, subject_id, signal, action, threshold) — a set
//	                       that changes nothing writes no audit row (LC11), a
//	                       disable appends one kind-'disabled' row, a remove
//	                       disables the row and appends kind-'removed' (it is
//	                       NEVER deleted: the audit and the id must outlive the
//	                       rule, so the seed's ledger and a fire's rule_id keep
//	                       resolving), and the watchdog_event triggers make the
//	                       audit append-only (P3). BlockLoopStuck is the
//	                       unattended move — a compare-and-set of a running loop
//	                       to blocked that merges lastReason='stuck' into the pin
//	                       and appends exactly one kind-'watchdog' control event
//	                       (LC10: a lost race is a no-op that writes nothing);
//	                       AppendWatchdogFire is the notify arm's audit row, which
//	                       moves nothing. A firing is NEVER a watchdog_event row:
//	                       it rides loop_control_event, so a fire is never
//	                       recorded twice (the migration's comment). The
//	                       observation reads (WatchdogLastActivity /
//	                       WatchdogPositionDispatches / WatchdogReasonRun /
//	                       WatchdogRunningStep) are the store half of the four
//	                       signals: a pure read each, returning honest empties
//	                       where a source is absent, and WatchdogSignalAvailability
//	                       is the LIMIT-0 probe that says a signal is genuinely
//	                       off at all (P31) so the vocabulary can serve
//	                       available:false and the set door can refuse a rule on
//	                       an unavailable signal. Nothing here decides a loop is
//	                       stuck (internal/watchdog's Evaluate) or retries
//	                       anything (OD8). Consumer: the daemon's watchdog service
//	                       (T9), the watchdog.* socket methods (T9) and `aeon
//	                       watchdog` (T11) behind it.
//	S47 tool catalog   ToolCount · ToolList · ToolGet · ToolUpsertTx — the **catalog
//	                       entry** of a system tool as a row (TC2): identity, family,
//	                       description, the canonical schema **copy** with its SHA-256,
//	                       the rerun class (never defaulted — an insert omitting it is
//	                       refused), the capability and governed-by JSON arrays, the
//	                       records twin, the aliases, whether the tool is conditional,
//	                       the ≤200-char note and the version. Its executor and the
//	                       schema the runtime parses a call against stay CODE
//	                       (`internal/tools`), and nothing on the step's tool-build path
//	                       reads a row here — `TestToolTableIsNotReadByTheRuntimeSpecPath`
//	                       pins that, because a model offered a spec from a row whose
//	                       executor says otherwise is the drift TC11 exists to catch.
//	                       `ToolUpsertTx` is the ONLY write, takes the caller's
//	                       transaction (the row and its seed ledger row commit together),
//	                       updates content + `updated_at` and never `created_at`;
//	                       `origin`'s CHECK admits only 'system'. There is deliberately
//	                       **no delete accessor** — a tool whose code left the registry
//	                       keeps its row and is reported retired (S43-extra SD6), and a
//	                       history that can be deleted is a history that can be edited.
//	                       MCP tools are NOT rows here (TC4): S46's tables are their one
//	                       truth. Consumer: the `tools.core` seed applier (T3) writes —
//	                       through `ToolUpsertTx` plus the two transaction-scoped reads
//	                       it needs, `SeedToolRow` (the fingerprint of the row as it is
//	                       at that moment, inside the apply's own transaction) and
//	                       `SeedToolRetire` (a row whose code left the set keeps its
//	                       content and moves `updated_at` only: SD6 flags retirement in
//	                       the ledger, and inventing a content edit here would read as
//	                       divergence for a tool nobody touched) — while
//	                       the `tools.*` / `surface.*` reads (T4–T6), `aeon tools` /
//	                       `aeon surface` (T8) and the two Settings panels (T10) read.
//	S57 verify records  AddVerifyCommand · UpdateVerifyCommand ·
//	                       ArchiveVerifyCommand · RestoreVerifyCommand ·
//	                       GetVerifyCommand · ListVerifyCommands ·
//	                       EffectiveGates · ListVerifyCommandEvents ·
//	                       CreateVerifyRun · FinishVerifyRun · ListVerifyRuns ·
//	                       GetVerifyRun · LatestPassingRun ·
//	                       LatestRunForCommand · ListVerifyTemplates ·
//	                       ApplyVerifyTemplates · VerifyWithTx ·
//	                       VerifyTemplateRowTx · VerifyTemplateRetireTx —
//	                       the verification records
//	                       (VG3/VG6/VG21): the gate a component declares
//	                       (verify_command), its append-only audit
//	                       (verify_command_event — one row per changing write,
//	                       same transaction, fixed-template summary from ids
//	                       and enums, never the command text (SEC-033),
//	                       revisions never reset, no FK to the record), the
//	                       append-only evidence (verify_run + verify_run_result
//	                       — a run is visible only finished, in ONE
//	                       transaction with all its results, and the command
//	                       text is SNAPSHOTTED on the result so history
//	                       survives a later edit or archive), and the inert
//	                       templates (verify_template — nothing runs until a
//	                       person applies one; empty until T10's seed). The
//	                       gate writes take a human actor (user|cli) and
//	                       refuse everything else before any SQL (VG23: an
//	                       agent proposes, T8's proposal kind, a human
//	                       commits); a refused write is a discriminated
//	                       VerifyRefusal with a sentence (DB19), never an
//	                       error, and writes nothing. EffectiveGates is the
//	                       one-level replace-not-merge resolver: the node's
//	                       own live gates, else — for a child whose root is
//	                       not archived — the root's, each row source own|root;
//	                       an archived node supplies nothing and takes
//	                       nothing. Runs are written only through the
//	                       Create/Finish pair (the draft is memory-only, so a
//	                       crash before finish leaves no torn evidence), and
//	                       the store never runs a gate — T4's runner is the
//	                       only spawner. Consumers: T4's runner
//	                       (internal/verify) writes the runs, the engine
//	                       (T6/T7) asks LatestPassingRun and the runs reads,
//	                       the verify.* human door (T8, socket + CLI) authors
//	                       and reads the gates and templates — including
//	                       LatestRunForCommand, the list's per-gate last-run
//	                       column (a gate never run answers nil, not a row)
//	                       — T10's seed fills verify_template, and T11's UI
//	                       Gates panel reads.
//	S57 merge rule / DoD / merged  GetMergeRule · EffectiveMergeRule ·
//	                       SetMergeRule · ClearMergeRule ·
//	                       ListMergeRuleEvents · MaterializeDoD · ListDoD ·
//	                       MarkDoD · GetDoDItem · RecordMergeEvent ·
//	                       ListMergeEvents — the merge authority and its two
//	                       facts (VG2/VG3/VG13/VG14): the per-component
//	                       merge_rule row (one per node, judged by
//	                       EffectiveMergeRule with the same
//	                       replace-not-merge own|root|baseline resolution as
//	                       EffectiveGates — a child's own row REPLACES the
//	                       root's whole row, never merges into it) with its
//	                       append-only audit (merge_rule_event — one row per
//	                       changing write, same transaction, fixed-template
//	                       summary from ids and enums, revisions never reset,
//	                       UNIQUE(component_id, revision) the backstop), the
//	                       per-LOOP definition-of-done ledger (dod_item —
//	                       keyed by the loop's Scope (I4), one row per
//	                       (brief_id, idx), a new brief VERSION is new rows at
//	                       the same idx and a `met` carries over ONLY for the
//	                       byte-equal text, changed text is reset and told,
//	                       the store never deletes a version) with its
//	                       append-only audit (dod_event), and the missing
//	                       `merged` fact (merge_event — written only by the
//	                       engine's seam (VG14), idempotent on
//	                       UNIQUE(component_root, pr, merge_sha): the
//	                       re-entry gets the row that stands, and the
//	                       re-merge after a force-push is a new row). The
//	                       rule writes take a human actor (user|cli) and
//	                       refuse everything else before any SQL (VG23); a
//	                       refused write is a discriminated MergeRuleRefusal
//	                       / DodRefusal with a sentence (DB19), never an
//	                       error, and writes nothing. DoD marks take user|
//	                       trigger — a `met` names its evidence (verify_run
//	                       by the engine, manual by a person, the pairing a
//	                       backstop) and a non-met clears it.
//	S57 merge watch / gate filing  EnqueueMergeWatch · DueMergeWatches ·
//	                       UpdateMergeWatch · ExpireStaleMergeWatches ·
//	                       ListMergeWatches · MergeEventsWithoutWatch ·
//	                       ClaimGateFiling · SetGateFilingBug ·
//	                       GateFilingBySubject · ListGateFilings — the
//	                       watch that follows a merged PR to green
//	                       (VG7/VG10/VG16): merge_watch (one row per merge
//	                       event, enqueued idempotently beside
//	                       RecordMergeEvent — the re-entry gets the row that
//	                       stands, born expired when the window had already
//	                       elapsed, due rows oldest-first carrying the
//	                       event's join, the five-state CHECK, and the last
//	                       answer KEPT on expiry: detail is the reason,
//	                       never wiped) and the dedupe ledger (gate_filing
//	                       — a claim per subject BEFORE the filing, the bug
//	                       id stamped only on success so a refused create
//	                       leaves the claim open to re-file, the count the
//	                       truth, three kinds) so a repeating red never
//	                       files a second bug. Consumers: the daemon's
//	                       gatewatch watcher (T7's poller and startup
//	                       sweep) and the terminal reactor's filing
//	                       (gate_reactor.go), both through the daemon.
//	S62 backup/restore  SnapshotTo (BK3: the one-file consistent snapshot, on a
//	                       READER connection — the writer pool is never touched,
//	                       O6) and AppliedNamesAndPending (the F4 empty-store
//	                       predicate the pre-migration hook asks on every open,
//	                       creating nothing). RecordBackupRun, ListBackupRuns
//	                       and LastBackupRun are the append-only backup_run
//	                       ledger (BK10): one writer, "last success" / "last
//	                       verified" / age reads. Package-level CheckFile and
//	                       KnownMigrationNames ride the same file: file
//	                       verification without a store, and the closed name set
//	                       for the newer-schema check. Consumers: internal/backup
//	                       (T4's container and retention, T5's open hook, the
//	                       run records), the restore pre-checks (T7), the
//	                       backup.status read (T6) and `aeon backup` (T8).
//	S48 blueprint records BlueprintGet · BlueprintRowTx · BlueprintList ·
//	                       BlueprintNames · BlueprintWithTx (the one write
//	                       transaction, the SeedWithTx idiom) · BlueprintOwnRefs ·
//	                       BlueprintRefsFor · BlueprintReferences (the BP10
//	                       delete-safety read) · BlueprintMaxRevs ·
//	                       BlueprintOverrides · BlueprintOverrideSet ·
//	                       BlueprintOverrideClear (a locked blueprint's
//	                       per-lane overrides, read at loop start) ·
//	                       BlueprintUsageCounts (the T7 list-summary grouped
//	                       reads: each name's highest revision and its ref
//	                       count, one query each — no N+1 walk) ·
//	                       RevisionList · RevisionGet ·
//	                       ImportByHash · ImportRefusals — the SQL text for the
//	                       four S48 record tables (BP1): blueprint / the
//	                       append-only blueprint_revision / the derived
//	                       blueprint_ref / the blueprint_import adoption ledger.
//	                       Package-level BlueprintPutTx (THE single write path:
//	                       row + revision + refs in one transaction),
//	                       BlueprintDeleteTx (row and refs, never the history)
//	                       and ImportInsertTx ride the same file. Consumers:
//	                       internal/blueprint's row-backed Source (T3), the
//	                       adoption importer (T4), the blueprints.standard seed
//	                       (T5), the daemon's write service and the agent-delete
//	                       protection (T6), the protocol reads (T7) and the
//	                       guards (T8).
//	S62 export          ExportRows and TableExists are the export seam (T8,
//	                       BK14): ExportRows reads ONE table's declared columns
//	                       (the allow-listed projection the engine declares,
//	                       enforced at the SQL boundary) into result maps, and
//	                       TableExists answers whether a table is built on this
//	                       schema (the three-state set answer's probe). Both
//	                       are READS on the reader pool — the export writes
//	                       files, never rows. Consumers: internal/export's
//	                       Run (the engine), reached by the daemon's export
//	                       service (T8) and the CLI's direct door
//	                       (cmd/aeon/export.go), which opens the store
//	                       read-only.
//	S59 execution         environments          through it. Writes, all taking an `Actor` and all
//	                       appending their `environment_change` row IN THE SAME
//	                       transaction (EV17): CreateEnvironment · UpdateEnvironment (the
//	                       optimistic-concurrency guard — a stale `expectedRevision` is a
//	                       refusal naming the current one) · ArchiveEnvironment ·
//	                       RestoreEnvironment · BindEnvironment · UnbindEnvironment (EV2's one
//	                       binding per scope, and no door removes the workspace binding).
//	                       Reads: GetEnvironment · ListEnvironments · ListBindings ·
//	                       BindingFor (EV2's child → root → workspace order, and the `source`
//	                       string that says WHICH answered) · EnvironmentSecretRefs ·
//	                       ListChanges (the audit read the panel and `aeon env history`
//	                       render).
//	                       Seeding (the environment.core set and the adoption pass ride the
//	                       S43-extra engine's transaction — tx-variants, S59 T6):
//	                       SeedEnvironmentTx · EnvironmentRowTx · SeedWorkspaceBindingTx.
//	                       ProvisionsForLoop (S59 T12 W2-2, named here for
//	                       the surface table on 2026-11-04: the terminal
//	                       teardown's read by the slug the event carries —
//	                       the daemon's execenv reconcile hook is its
//	                       consumer).
//	                       S59 T7 (2026-11-03) adds no new store surface: the
//	                       agent's environment_propose_restrict ask is INERT,
//	                       and the committed tightening rides UpdateEnvironment
//	                       above — the plane has exactly one write path for a
//	                       spec, and the proposal's id travels inside its audit
//	                       summary.
//	                       Secret references (EV7 — no method here takes or stores a value,
//	                       and there is no column that could): CreateSecretRef ·
//	                       UpdateSecretRef · ArchiveSecretRef · RestoreSecretRef ·
//	                       GetSecretRef · ListSecretRefs · EnvironmentsReferencingSecretRef
//	                       (T6's secretref.show names the live environments that NAME a
//	                       reference — the refusal is the names, not a count). The archive refuses while a live
//	                       environment still NAMES the reference, and the refusal names the
//	                       environments; `reserved` carries the caller's daemon-credential
//	                       names, so the store never imports config/daemon/forge to learn what
//	                       the daemon holds.
//	                       The provision ledger (EV12, one live row per owner — the row is
//	                       written BEFORE the resource exists): CreateProvision ·
//	                       MarkProvisionReady · MarkProvisionTearingDown · MarkProvisionRemoved ·
//	                       MarkProvisionFailed (settle-once: a repeated mark is a no-op, a
//	                       backward one is a refusal naming the state it found) · GetProvision ·
//	                       ListLiveProvisions · ProvisionsFor · ListProvisions (T6's provision
//	                       listing door, state-filtered).
//	                       The step record (EV9/EV11): RecordStepEnvironment ·
//	                       StepEnvironments — sorted, deduped NAMES only, deduped against the
//	                       stored rows over parsed JSON, and a refusal that never spawned is
//	                       recorded rather than dropped.
//	                       Consumers: the spawn seam (T5 — `BindingFor`, `RecordStepEnvironment`),
//	                       the daemon's human doors (T6 — every write, EV18), the agent tools
//	                       and the `environment_restrict` proposals (T7), `aeon env` and
//	                       `aeon secretref` (T8), the container runtime and its reconcile (T9 —
//	                       the ledger's whole reason to exist), services and the preview
//	                       refusal (T10) and the Environments panel (T11). No consumer exists
//	                       YET — this task's tests are the only callers, which is exactly the
//	                       case the T10 rule watches, so the consumers named above are tasks,
//	                       and T12 verifies each one arrived.
//	S60 merge queue      EnqueueMergeItem (position by gap; duplicate-open and
//	                       unknown-root refused before SQL; the `queued` birth
//	                       event in the same transaction) · ReadMergeItem ·
//	                       MergeItemTerminal (the reconcile read — MQ7's
//	                       terminal check for an integration worktree; loop
//	                       ReconcileWorktrees) ·
//	                       ListItemsNeedingReconcile (every open item, every
//	                       repository — the reactor's one pass; named by
//	                       internal/mergequeue's driver, T7) ·
//	                       ListOpenItemsByLoop (the open items one loop slug
//	                       drives — the conflict door's root/slug to item
//	                       answer, T7) ·
//	                       ListMergeItems (per-repository, queue order) ·
//	                       ListMergeItemsAcrossRoots (every repository at
//	                       once — the blank-root machine-wide listing the
//	                       queue's protocol doors answer through, T8; the
//	                       records arms filter it to the agent's own-view,
//	                       T9) ·
//	                       TransitionMergeItem (THE status writer — guarded
//	                       UPDATE + event, one transaction; stale-state and
//	                       slot-taken are refusals naming the truth) ·
//	                       SetMergeItemFields (facts under a status guard,
//	                       the event from = to) · ReorderMergeItems (queued
//	                       only, gaps not rewrites, one reorder event) ·
//	                       ReadMergeConfig (absent = mode 'off') ·
//	                       WriteMergeConfig (upsert + config event; identical
//	                       input is a no-op) · ListMergeQueueEvents (the
//	                       append-only read — the queue-prefixed name because
//	                       S33's merge watch owns the bare one in this
//	                       package). Consumers: internal/mergequeue and the
//	                       driver (T3/T7), the queue's protocol doors (T8),
//	                       the enqueue tool (T9), `aeon queue` (T10) — tasks,
//	                       per the same T10 rule.
//	S54 platform change   PlatformChangeInsertTx · PlatformChangeStarter ·
//	                       PlatformChangeList · PlatformWithTx ·
//	                       RetrievalComponentRoots. The naming truth,
//	                       amended at T2: T1's commit named only the
//	                       insert — the surface test flagged the rest at
//	                       T2 and the debt is paid here, name by name:
//	                       the audited start's insert rides the worker/
//	                       start tx, the starter read answers the additive
//	                       startedBy on accepted answers (nil,nil is the
//	                       honest no-name), the listing door
//	                       `platform.changes` reads newest-first,
//	                       PlatformWithTx is the tx every lawful audit
//	                       append shares (the tests' own seed rides it, so
//	                       production and proof append the same way), and
//	                       RetrievalComponentRoots is the panel's component
//	                       list — the distinct roots that HAVE corpora, so
//	                       the panel never names a root the index has
//	                       never seen.
//	                       Consumers: the four operation doors' seams
//	                       (T1), internal/protocol platform.go's changes
//	                       read and the daemon's platform_reads adapter
//	                       (T2), `aeon platform` (T5) — tasks, per the
//	                       T10 rule.
//	S58 supervision     The six tables S58 owns (T1). Attention marks:
//	                       AppendAttentionAck · LatestAcks — attention_ack, latest
//	                       mark wins (SV-D3: ack, snooze-until, unack; append-only).
//	                       Event classes: ListNotifyClasses · GetNotifyClass ·
//	                       GetNotifyClassTx · UpsertNotifyClassTx ·
//	                       UpdateNotifyClass — notify_event_class,
//	                       the per-(kind,variant) tone/title/enabled the resolver reads
//	                       and a person edits (the human edit bumps version and audits;
//	                       the seed's applier writes inside a caller's transaction).
//	                       Channels: CreateChannel · UpdateChannel · SetChannelEnabled ·
//	                       ArchiveChannel · AuditChannelSecret · GetChannel · ListChannels ·
//	                       ChannelHosts —
//	                       notify_channel: born disabled, person-only to author
//	                       (ErrNotAPerson before any SQL), secret_ref a pure
//	                       `secret:notify/<id>` and never a value, and ChannelHosts
//	                       the egress allow-list of ENABLED non-archived hosts (SV7).
//	                       Subscriptions: SetSubscription · ListSubscriptionsForRoot ·
//	                       ListSubscriptions — notify_subscription: (channel, root)
//	                       upsert, ''-root means every root, quiet hours all-or-none.
//	                       Deliveries: AppendDelivery · ListDeliveries ·
//	                       RecentDeliveryCount — notify_delivery: every decision,
//	                       suppression included (a suppression never used a send
//	                       slot; only sent|failed count against max_per_hour).
//	                       Every channel/subscription/class write appends its
//	                       notify_change audit row IN THE SAME transaction.
//	                       Attention reads (T2): PendingQuestionsForScope ·
//	                       BlockedLoopsForScope · StoppedLoopsForScope ·
//	                       LatestBlockedReasons · LiveComponentTree — the
//	                       live-state readers the inbox sources evaluate per
//	                       call (SV1: nothing stored, nothing cached); their
//	                       consumers are internal/attention's sources, the
//	                       scope resolver and the daemon's ack service.
//	                       Mission rollup reads (T6, SV11):
//	                       LoopStatusCountsForScope · LastActivityForScope —
//	                       GROUP BY over the loop table scoped by the ONE
//	                       RootScopeSQL path rule (a root includes its
//	                       children; the terminal statuses ride out in the
//	                       counts and internal/mission drops them, so the
//	                       store invents no fourth state), and the tree's
//	                       newest touch as the honest empty string when it
//	                       was never touched. Consumers: internal/mission's
//	                       Reader (mission.Evaluate), reached only through
//	                       the daemon's mission.status door.
//	                       Consumers: the attention seam (T2/T3), the class resolver
//	                       and its seed (T4), the dispatcher (T5), the supervision.*
//	                       doors + CLI + agent tools (T6), the tray and deep links
//	                       (T7) and the Watch panel (T8). No consumer exists YET —
//	                       T1's tests are the only callers; the consumers named are
//	                       tasks, and T10's close-out verifies each arrived.
//	loop access            LoopAccessGet · LoopAccessSet · LoopAccessClear (a loop's permission mode:
//	                       what a person pre-approved — the chat's full-access switch, an unattended
//	                       loop's tool list; no row means ask).
//	S58 attention (more)  FailedGatesForScope (the latest failed gate run per slot) ·
//	                       OpenProposals (undecided proposals) · UnvalidatedBugsForScope
//	                       (bugs awaiting validation, per component) — the `gates`,
//	                       `proposals` and `triage` sources of the inbox.
//	S50 roadmap audit     The roadmap's answer to the S44 gap (T1, RM2):
//	                       RoadmapWithTx (the one write transaction the audited
//	                       writers share — write + audit row, commit or
//	                       rollback together) · RoadmapChangeInsertTx (the
//	                       package-level tx insert the writers call) ·
//	                       RoadmapChangeList (newest-first history for
//	                       `roadmap.history`, T2's read) · RoadmapMoveTime (when
//	                       an item first started / last finished, read from the
//	                       audit rows; the Work page's roadmap bars). The audited writers —
//	                       every production roadmap write — carry their actor
//	                       and their append-only audit row in the SAME
//	                       transaction: CreateRoadmapItemAudited ·
//	                       TransitionRoadmapItemAudited ·
//	                       RoadmapItemLandingAudited (artifacts + status move,
//	                       ONE row) · SetRoadmapItemEstimateAudited ·
//	                       SetRoadmapItemPriorityAudited ·
//	                       SetRoadmapItemLoopAudited ·
//	                       UpdateRoadmapItemAudited (RM1's narrow field set —
//	                       title/epic/paths — with its 'update' row, T2).
//	                       Consumers TODAY, not tasks: internal/roadmap's
//	                       audit.go and update.go (the door's audited half)
//	                       and, through them, the socket handlers, the
//	                       dispatch file, the roadmap reactor and the triage
//	                       hand-off — the plain writers stay for tests only.
//	S51 bug audit          The bug plane's answer, one slice later (T1, TM2):
//	                       BugWithTx (the one write transaction the audited
//	                       bug writers share — write + audit row, commit or
//	                       rollback together) · BugChangeInsertTx (the
//	                       package-level tx insert the writers call) ·
//	                       BugChangeAppend (autocommit — ONLY the validate
//	                       dispatch, the one audit that pairs with no bug-row
//	                       write) · BugChangeList (newest-first history, T2's
//	                       read) · BugExists (does the id have a row — the
//	                       history door telling refusal from empty) ·
//	                       BugChangeStats (the board's ONE grouped digest:
//	                       attempts, last attempt, row count) ·
//	                       CountOpenClaimsByFiler (S51 T5, TM6's cap: the one
//	                       COUNT of an agent's open UNVALIDATED live claims —
//	                       not duplicates — the bug_file arm reads before it
//	                       files). The audited
//	                       writers — every production bug
//	                       write — carry their actor and their append-only
//	                       bug_change row in the SAME transaction, and earn
//	                       no row when nothing moved: CreateBugAudited ·
//	                       TransitionBugAudited · SetBugPriorityAudited ·
//	                       SetBugDuplicateOfAudited ·
//	                       SetBugRoadmapItemIDAudited ·
//	                       UpdateBugFieldsAudited (TM1's title/body-only
//	                       edit, T2, summary naming the FIELD WORDS). Plus
//	                       MigrateBugFiledBySlugs — TM3's name→slug back-fill
//	                       (the MigrateAgentPolicyReferences shape: resolve
//	                       all, refuse naming EVERY orphan, idempotent;
//	                       'person' and 'system:gates' exempt), called at
//	                       daemon start from serve, never from Open.
//	                       Consumers TODAY, not tasks: internal/triage's
//	                       audit.go (the door's audited half) and, through
//	                       it, the socket handlers, the gate filer, the
//	                       reactor and the hand-off, and internal/daemon/
//	                       records_triage.go (the bug_file arm, counting the
//	                       TM6 cap) — the plain writers stay
//	                       for tests and the plain store path.
//	S63 outcome facts    RecordRetroOutcome (ONE transaction: outcome + agent
//	                       rows, ON CONFLICT DO NOTHING on the unique
//	                       terminal key — a replay answers the pre-existing
//	                       id, RT2) · RetroTally (THE count — derived, no
//	                       stored tally exists anywhere, RT4) ·
//	                       RetroOutcomeByID · ListRetroOutcomes (paged,
//	                       id-descended cursor) · RetroAgentsFor ·
//	                       LinkRetroLearning (insert-only — the link is the
//	                       reconcile lock) · HasRetroLearning (T3's row half
//	                       of that lock — does THIS outcome carry its link) ·
//	                       UnlinkedMilestoneOutcomes
//	                       (RT4's crash gap) · RetroLoopHasFacts (T2's
//	                       backfill probe — any row for (root, slug) is the
//	                       per-loop already-recorded skip; the terminal key
//	                       cannot carry it, a backfill row's key differs) ·
//	                       RetroCoverage (the since/
//	                       loops half; the dropped counter is the daemon's
//	                       in-memory number and the store exposes none) ·
//	                       UpsertRetroTemplate · GetRetroTemplate ·
//	                       ListRetroTemplates (the mutable template set the
//	                       seed engine upserts) · CountEscalationQuestions
//	                       (the recorder's only escalation source — question
//	                       rows kind=gate with options exactly
//	                       ["continue","abort"]) · UpsertRetroTemplateTx and
//	                       GetRetroTemplateTx (T3's seed-engine doors over
//	                       the caller's tx — row and ledger commit together)
//	                       · RetroLatestOutcomes · RetroEverBlockedLoops ·
//	                       RetroAgentRowsInWindow (T4's card reads: the
//	                       latest outcome per loop in a window, the
//	                       window-free ever-blocked set, the agent plane
//	                       joined to its outcome) · RetroStatusCounts ·
//	                       RetroLearningLink · LearningsInWindow (T5's door
//	                       reads: the status counters, one row's learning
//	                       link, the digest's window-scoped learnings).
//	                       The three fact tables are
//	                       append-only by trigger and carry no FK to loop or
//	                       learning (P3: facts outlive both — measured at T0:
//	                       archive CASCADEs the live detail away, these rows
//	                       stay). Consumers: S63's own chain — internal/retro
//	                       (T2's recorder + backfill, T3's milestone
//	                       learning), internal/scorecard (T4), the daemon
//	                       seams behind the `scorecard.*` and `retro.*`
//	                       doors (T5), the three read tools (T6) and the CLI
//	                       clients (T7).
//	S61 goal intake      Charter history (T1; consumers internal/charter and
//	                       the daemon's intake plane, S61 T2/T6/T7/T9):
//	                       AppendCharterRevision (max+1 inside the tx, a
//	                       child refused with its project's name, append-only
//	                       by trigger) · CurrentCharter (max(revision) — THE
//	                       charter) · CharterRevisions ·
//	                       ListChartersWithCurrent. Acceptance criteria
//	                       (GI5): ReplaceGoalCriteria (whole set for one
//	                       (goal, version), one tx) · GoalCriteria — and
//	                       UpdateGoal copies the set forward beside
//	                       goal_component (goals.go, P-G). The intake
//	                       session and its resume ledger (GI6/GI13):
//	                       CreateIntake (person-only) · SetIntakeLoop ·
//	                       SetIntakeRoot (once; target path in the same
//	                       write; trigger-guarded) · GetIntake · ListIntakes ·
//	                       AppendIntakeStep · IntakeSteps, and the commit
//	                       run's records step (S61 T6, consumer
//	                       internal/intake): ApplyIntakeRecords files the
//	                       plan's epics, goals (with stakes and criteria)
//	                       and items as ONE transaction. The scaffold
//	                       template set (GI14): SaveTemplate (row+revision in
//	                       one tx, origin never moved by an edit) and its
//	                       seed twins SaveTemplateTx · GetTemplateTx (SD3:
//	                       the record and its ledger row commit together) ·
//	                       GetTemplate · ListTemplates · TemplateRevisions.
//	S27 continuous       The scheduler's own rows and its ONE audit: the
//	                       singleton authority (SchedulerState ·
//	                       SetSchedulerState — enabled/paused are two facts,
//	                       and every change carries the peer in the same
//	                       transaction) · the per-root authority
//	                       (SchedulerRoots · SchedulerRoot ·
//	                       UpsertSchedulerRoot · DisableSchedulerRoot —
//	                       retire, never delete) · the one-attempt claim
//	                       (ClaimStart · StampStart · FinishStart ·
//	                       OpenStarts · StartRecord · StartsSince — the
//	                       PRIMARY KEY is CN14's rule; an open row is the
//	                       crash shape, read honestly) · the append-only
//	                       decision log (AppendDecision — 1,000 identical
//	                       deferrals are ONE row, `started` never dedupes ·
//	                       Decisions · LastDecision · FirstDeferral — the
//	                       CURRENT streak's anchor, CN11 ·
//	                       LastSchedulerStart) · the readiness reads
//	                       (OccupyingLoops · RunningLoopSlots — S20's
//	                       StillGoing set in SQL · SchedulerLoopsToStop —
//	                       stopAll's ledger, the loops started_by
//	                       'scheduler' that are still live ·
//	                       ItemAutonomyCap · SetItemAutonomyCap — the
//	                       item's declared narrowing (grant|outward|
//	                       none), read by S27's seam adapter before it
//	                       pins, written only in the narrowing words) · the
//	                       start-rate instants (StartTimesSince — CN10's
//	                       start-rate window read as times, not counts) ·
//	                       the roadmap graph
//	                       (AddDependency · RemoveDependency ·
//	                       RoadmapDependencies · Dependents ·
//	                       OpenDependencies — the named in the graph
//	                       ARE the edges, CN14; the CTE walk names the
//	                       cycle and the triggers answer raw SQL too).
//	                       Deviation recorded in the slice Notes: the graph
//	                       read ships as RoadmapDependencies — Store.
//	                       Dependencies is the framework's store-init
//	                       contract (framework.go) and was taken.
//	                       Consumers: S27's scheduler core and its daemon
//	                       seams (T3–T5), the socket doors (T7), the
//	                       planner reads (T8–T10).
//
// # No SQL outside this package (I2)
//
// A SQL string anywhere else is a defect, and the mechanism that catches it is
// a grep over `internal/` and `cmd/` in store_test.go — not review. Keeping
// every statement here is what makes a schema change a local change.

// Info is what `aeon db info` prints, gathered in one read.
//
// One struct rather than six calls, because the CLI prints them together and a
// person reading the output is answering one question: is this store the one I
// think it is, and is it up to date.
type Info struct {
	Path  string
	Bytes int64

	// WALBytes is the write-ahead log beside it.
	//
	// # Why it is reported separately rather than added in
	//
	// In WAL mode, committed data lives in `state.db-wal` until a checkpoint
	// moves it. Found by walking the demo: a store with eighteen tables and
	// three thousand events reported **4 KiB**, because almost everything was
	// still in the log. A single number would have read as a broken store, and
	// a summed one would have hidden where the bytes actually are — which is
	// the thing somebody copying the file needs to know.
	WALBytes int64

	JournalMode string
	ForeignKeys bool

	Applied []AppliedMigration
	Pending []Migration
	Tables  []string

	// VecVersion is the statically linked extension's version, reported for
	// both pools because an extension registered for one is a retrieval slice
	// that works in tests and fails under load (Q2).
	VecWriterVersion string
	VecReaderVersion string

	ReadPoolSize   int
	WriterPoolSize int
	BusyTimeoutMS  int
}

// Info gathers everything the CLI and the app report about a store.
func (s *Store) Info() (Info, error) {
	in := Info{
		Path:           s.path,
		ReadPoolSize:   s.readPoolSize,
		WriterPoolSize: writerPoolSize,
		BusyTimeoutMS:  int(s.busyTimeout.Milliseconds()),
	}

	if st, err := os.Stat(s.path); err == nil {
		in.Bytes = st.Size()
	}
	if st, err := os.Stat(s.path + "-wal"); err == nil {
		in.WALBytes = st.Size()
	}
	// A missing file is not an error here: a raw in-memory DSN has no file, and
	// `db info` saying "0 bytes" beside the path it was given is more useful
	// than refusing to report anything.

	mode, err := s.JournalMode()
	if err != nil {
		return Info{}, err
	}
	in.JournalMode = mode

	// Read from the connection, never inferred from the DSN (DB16's rule). The
	// cascade test is what proves it took EFFECT; this is what a person sees.
	var fk int
	if err := s.r.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil {
		return Info{}, fmt.Errorf("store: read foreign_keys: %w", err)
	}
	in.ForeignKeys = fk == 1

	if in.Applied, err = s.AppliedMigrations(); err != nil {
		return Info{}, err
	}
	if in.Pending, err = s.Pending(); err != nil {
		return Info{}, err
	}
	if in.Tables, err = s.Tables(); err != nil {
		return Info{}, err
	}
	if in.VecWriterVersion, in.VecReaderVersion, err = s.VecVersion(); err != nil {
		return Info{}, err
	}
	return in, nil
}

// TranscriptDigest is a stable digest of a step's transcript ON DISK.
//
// # Why this exists rather than comparing structs
//
// It is what the cold-resume test compares ACROSS A PROCESS BOUNDARY (D17). A
// child process opens the file, computes this, and prints it; the parent
// compares. Two parsed structures compared in one process would pass through
// the same encoder twice and prove nothing about what survived on disk — the
// failure being ruled out is precisely something surviving in memory that does
// not survive the file.
//
// It digests the RAW stored content, the role and the seq, so a reordering, a
// re-encoding or a lost signature all change it.
func (s *Store) TranscriptDigest(stepID int64) (string, error) {
	rows, err := s.r.Query(`SELECT seq, role, content FROM step_message WHERE step_id = ? ORDER BY seq`, stepID)
	if err != nil {
		return "", fmt.Errorf("store: digest the transcript of step %d: %w", stepID, err)
	}
	defer rows.Close()

	h := sha256.New()
	var n int
	for rows.Next() {
		var seq int64
		var role, content string
		if err := rows.Scan(&seq, &role, &content); err != nil {
			return "", fmt.Errorf("store: digest the transcript of step %d: %w", stepID, err)
		}
		n++
		// The separator is a byte that cannot appear in any of the three parts,
		// so two different transcripts cannot digest the same by sliding the
		// boundary between fields.
		fmt.Fprintf(h, "%d\x00%s\x00%s\x00", seq, role, content)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if n == 0 {
		return "", fmt.Errorf("store: step %d has no transcript to digest: %w", stepID, ErrNotFound)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// SignatureOf returns the `thinking` signature in a step's transcript, and its
// length.
//
// For `aeon step verify`, which is the human-visible form of the fidelity test:
// somebody diagnosing a rejected replay at S7 runs it first. The length is
// reported because "present" and "the right length" are different claims, and
// a truncated signature is the failure that reads as present.
func (s *Store) SignatureOf(stepID int64) (signature string, length int, err error) {
	res := s.ReadTranscript(stepID)
	if !res.OK {
		return "", 0, fmt.Errorf("store: step %d: %s", stepID, res.Reason())
	}
	for _, m := range res.Messages {
		for _, b := range m.Content {
			if b.Signature != "" {
				return b.Signature, len(b.Signature), nil
			}
		}
	}
	return "", 0, nil
}

// VerifyTranscript reports whether a step's transcript round-trips byte for
// byte.
//
// It re-encodes what was read and compares against the stored bytes. A pass
// means the encoder this build uses produces exactly what is on disk — which
// is the property S7's replay depends on, and the one a format change would
// break silently.
type Verification struct {
	OK              bool
	Messages        int
	SignatureLength int
	Mismatch        string
}

// VerifyTranscript is `aeon step verify`'s implementation.
func (s *Store) VerifyTranscript(stepID int64) (Verification, error) {
	res := s.ReadTranscript(stepID)
	if !res.OK {
		return Verification{Mismatch: res.Reason()}, nil
	}

	v := Verification{OK: true, Messages: len(res.Messages)}
	for i, m := range res.Messages {
		seq := int64(i + 1)
		stored, err := s.RawTranscriptContent(stepID, seq)
		if err != nil {
			return Verification{}, err
		}
		reencoded, err := marshalBlocks(m)
		if err != nil {
			return Verification{}, err
		}
		if stored != reencoded {
			v.OK = false
			v.Mismatch = fmt.Sprintf("message %d (seq %d) does not re-encode to its stored bytes: "+
				"stored %d bytes, re-encoded %d", i, seq, len(stored), len(reencoded))
			return v, nil
		}
		for _, b := range m.Content {
			if b.Signature != "" && v.SignatureLength == 0 {
				v.SignatureLength = len(b.Signature)
			}
		}
	}
	return v, nil
}
