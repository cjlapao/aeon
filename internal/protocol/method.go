package protocol

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/cjlapao/aeon/internal/history"
	"github.com/cjlapao/aeon/internal/learnings"
	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/queue"
	"github.com/cjlapao/aeon/internal/roadmap"
	"github.com/cjlapao/aeon/internal/runtime"
	"github.com/cjlapao/aeon/internal/search"
	"github.com/cjlapao/aeon/internal/triage"
	"sort"
	"sync"
)

// # The method table (T2)
//
// Five entries at S2. **Every later slice adds to it, additively** — a new
// method does not bump PROTOCOL_VERSION (see version.go's rule).
//
// An unknown method is **JSON-RPC's own** method-not-found, not an Aeon code: it
// is a fact about JSON-RPC rather than about Aeon. The transport already answers
// that way, so this table does not need to.

// MethodName is the wire name of a method. A named type so the table's keys
// cannot be confused with arbitrary strings at a call site.
type MethodName string

const (
	MethodInitialize  MethodName = "initialize"
	MethodPing        MethodName = "ping"
	MethodVersion     MethodName = "version"
	MethodSubscribe   MethodName = "subscribe"
	MethodUnsubscribe MethodName = "unsubscribe"

	// MethodDebugSubscriptions lists the registry.
	//
	// # A sixth method, and why it is marked rather than hidden
	//
	// The design lists five. This one arrives with T6 for a specific reason:
	// D6 asserts that dropping a connection empties the registry, and **a
	// registry that empties is a claim where a command that prints it empty is
	// a demonstration**. Adding a method is additive and does not bump
	// PROTOCOL_VERSION (T2).
	//
	// It is marked `Debug` so **S19 can exclude it from the narrowed surface**.
	// A debug method on a public HTTP surface is operational control by another
	// name, and the narrow surface exists to not have any (R6). Marking it is
	// what makes that exclusion mechanical rather than a thing somebody has to
	// remember.
	MethodDebugSubscriptions MethodName = "debug.subscriptions"

	// MethodDebugGoroutines dumps the daemon's goroutine stacks (S14 T3).
	// One demonstration justifies it: with an async agent Task suspended,
	// `aeon debug goroutines | grep -c agentsvc` answers 0 — AS4 proven from
	// outside the process. Debug-marked exactly as debug.subscriptions is.
	MethodDebugGoroutines MethodName = "debug.goroutines"

	// MethodAsk streams a reply from a model (S3).
	//
	// **A new method is additive and does not bump PROTOCOL_VERSION** — which
	// is the rule beside that constant, demonstrated here for the first time:
	// this is the first slice to add a method since the protocol existed.
	MethodAsk MethodName = "ask"

	// Route management, so a client can configure a provider without editing
	// the environment and restarting the daemon (S3, amended).
	//
	// **`routes.set` accepts a credential and `routes.list` cannot return
	// one** — different types, not a redaction. See routes.go.
	MethodRoutesList   MethodName = "routes.list"
	MethodRoutesSet    MethodName = "routes.set"
	MethodRoutesDelete MethodName = "routes.delete"

	// routes.probe (S18 T3): the operator's one minimal request that checks
	// a route's DECLARED wire capabilities, cached in the store so verdicts
	// survive a restart (D7, O2). It NEVER fires on its own — no loop step,
	// no list call, no startup path may call it (M32); `aeon routes probe`
	// and the bench's Probe button are its only callers.
	MethodRoutesProbe MethodName = "routes.probe"
	MethodRoutesTrace MethodName = "routes.trace"

	// The store (S4). Additive, like every method since S2 — a new method does
	// not bump PROTOCOL_VERSION.
	//
	// These exist so the app can exercise the store WHERE IT WILL BE CONSUMED
	// rather than only from a terminal (AGENTS §4b). `store.info` is the Store
	// tab's whole content; `loop.*` and `step.*` are what makes the test bench
	// able to create something and read it back.
	MethodStoreInfo     MethodName = "store.info"
	MethodStoreAwaiting MethodName = "store.awaiting"
	// MethodQuestionPending is S19 T5's read: the one question a loop is
	// blocked on, with the kind and the declared answer set. `store.awaiting`
	// answers WHICH loops wait; this answers WHY — "waiting on a gate"
	// without the prompt is not something a polling CI can act on. It is the
	// only question-facing method the narrow surface mounts: it READS. The
	// answering half (loop.answer) stays off the surface by permanence, not
	// by omission — see internal/narrow/questions.go.
	MethodQuestionPending MethodName = "question.pending"
	// MethodInboxList and MethodInboxSummary are S58 T2 (SV1, SV13): the
	// attention plane's two reads. `inbox.list` evaluates the source
	// registry over live state — nothing is stored, nothing is cached, and
	// the answer names which sources reported, failed, or were never wired,
	// so "nothing needs you" can never mean "nothing was looking".
	// `inbox.summary` is its counting twin (acked items count: the number
	// never lies, SV3). Both are the HUMAN DOOR's reads: registered iff the
	// Inbox seam is wired (V13), RequiresInitialize, never Debug, and NOT
	// in narrow.Actions() (SV-D3 — the guard stays pinned at nine until a
	// later slice makes the argument).
	MethodInboxList    MethodName = "inbox.list"
	MethodInboxSummary MethodName = "inbox.summary"
	// MethodInboxAck, MethodInboxSnooze and MethodInboxUnack are S58 T3
	// (SV3, SV12, P3): the visibility writes. The actor is the peer the
	// socket authenticated and NEVER a param; an agent-kind peer is
	// refused ("acknowledging is a person's act"); the key must name an
	// item currently needing attention (no ghost rows); a no-op writes no
	// row. Registered iff the InboxWrite seam is wired (V13),
	// RequiresInitialize, never Debug, not in narrow.Actions().
	MethodInboxAck    MethodName = "inbox.ack"
	MethodInboxSnooze MethodName = "inbox.snooze"
	MethodInboxUnack  MethodName = "inbox.unack"

	// MethodNotifyClassList, MethodNotifyClassUpdate and
	// MethodNotifyVocabulary are S58 T4 (SV4, SV13): the event-class
	// table's two reads and its one write. The class table decides what
	// notifies and how it reads; that is a person's plane — the update is
	// human-door like the ack writes, the list and the vocabulary are the
	// descriptor reads the Settings panel drives (SV18).
	MethodNotifyClassList   MethodName = "notify.class.list"
	MethodNotifyClassUpdate MethodName = "notify.class.update"
	MethodNotifyVocabulary  MethodName = "notify.vocabulary"

	// The S58 T5 channel plane (SV7, SV8): seven writes and three reads.
	// A webhook channel is standing authority to send machine facts off
	// the machine, so every write — create, edit, enable, disable,
	// archive, test, and the subscription that scopes delivery — is the
	// human door, refused for an agent or unattributed peer and absent
	// without the seam (V13). The reads answer what is configured (host,
	// never url) and what happened (the delivery log).
	MethodNotifyChannelCreate   MethodName = "notify.channel.create"
	MethodNotifyChannelUpdate   MethodName = "notify.channel.update"
	MethodNotifyChannelEnable   MethodName = "notify.channel.enable"
	MethodNotifyChannelDisable  MethodName = "notify.channel.disable"
	MethodNotifyChannelArchive  MethodName = "notify.channel.archive"
	MethodNotifyChannelTest     MethodName = "notify.channel.test"
	MethodNotifySubscriptionSet MethodName = "notify.subscription.set"
	MethodNotifyChannelList     MethodName = "notify.channel.list"
	MethodNotifyChannelShow     MethodName = "notify.channel.show"
	MethodNotifyDeliveryList    MethodName = "notify.delivery.list"
	// MethodMissionStatus is S58 T6 (SV11): the per-root overview — loop
	// counts by live status, the needs-you summary (the SAME evaluation
	// inbox.summary answers with), the CT7 spend floor against the cap,
	// the next-ready relay, and the instant of last movement. Unwired
	// seams answer `not-wired`, never a zero; no composite health number
	// exists anywhere in the result. Registered iff the Mission seam is
	// wired (V13), RequiresInitialize, never Debug, not in narrow.
	MethodMissionStatus MethodName = "mission.status"
	// MethodWebhookDeliver is S20 T6 (TR11): a webhook is a mounting on
	// THIS surface, not a listener of its own — the delivery is a method
	// with a fixture, and the signature rides as a PARAM because stdio has
	// no headers (N18); the HTTP mounting's X-Hub-Signature-256 header is
	// sugar for the same field.
	MethodWebhookDeliver MethodName = "webhook.deliver"
	// MethodRemoteComplete is S21 T6 (RA7, RA8): the completion of an
	// async remote Task arrives as a call to THIS surface, not a held
	// connection — the mounting webhook.deliver established, with one
	// addition that carries the security: the completion is not merely
	// verified (WHO sent it) but BOUND (what they were asked) — the task
	// must be one this daemon created, still awaiting, whose callee is
	// the sender. The signature rides as a param (N18), as the webhook's
	// does.
	MethodRemoteComplete MethodName = "remote.complete"
	// S20 T8: the operational plane over the socket — `aeon trigger
	// list|pause|resume|fire-now` and the app's buttons are the SAME four
	// methods; the CLI holds no control logic to drift (P34). fire-now
	// fires AS A TRIGGER (unattended, TR2) over the socket too: a person
	// watching is not a party the loop runs for.
	MethodTriggerList    MethodName = "trigger.list"
	MethodTriggerPause   MethodName = "trigger.pause"
	MethodTriggerResume  MethodName = "trigger.resume"
	MethodTriggerFireNow MethodName = "trigger.fireNow"
	// trigger.history is the same reporting the CLI's `trigger history`
	// prints: fires AND gaps (0034's range-and-count silence), the five
	// answers and the honest absence between them (TR4).
	MethodTriggerHistory MethodName = "trigger.history"
	// trigger.add is T9's completion of the control plane (the finding:
	// the slice built every door except the one that adds — no production
	// code wrote a schedule row). Validation answers in T1's own sentences;
	// an existing name is refused, never silently rewritten.
	MethodTriggerAdd   MethodName = "trigger.add"
	MethodLoopCreate   MethodName = "loop.create"
	MethodLoopList     MethodName = "loop.list"
	MethodLoopWrite    MethodName = "loop.write"
	MethodLoopRead     MethodName = "loop.read"
	MethodLoopStart    MethodName = "loop.start"
	MethodLoopShow     MethodName = "loop.show"
	MethodLoopFiles    MethodName = "loop.files"
	MethodLoopFileRead MethodName = "loop.file.read"
	MethodLoopAnswer   MethodName = "loop.answer"
	// MethodLoopClose ends a RESTING loop (S39 CR1): idle -> done, the
	// conversation over. Socket-only human door (CR10 — the narrow surface's
	// action set is closed); the daemon claims from idle and refuses
	// anything else with a sentence, never a silent success.
	MethodLoopClose MethodName = "loop.close"

	// MethodLoopArchive puts a loop and its documents into cold storage
	// (S33 T9, the bench's Delete door; D8's claim lives here — the
	// rollups are NOT part of the loop's live life and do not move).
	MethodLoopArchive MethodName = "loop.archive"
	// MethodLoopMessage sends a message to a RESTING loop (S39 CR3): one
	// conversation turn — a thread row plus one ordinary model step by the
	// loop's conversation agent, streamed and steerable like any step. While
	// a step runs the answer is the step.steer remedy as data; the narrow
	// surface never carries it (CR10 — human door).
	MethodLoopMessage MethodName = "loop.message"

	// MethodAgentMessages lists a loop's mailbox rows (S70 MB7): the ONE
	// agent.messages surface is this READ. There is deliberately no
	// authoring method — a row is filed only by a step's own run through
	// the ask door behind the policy gate (MB1); a person speaks to a loop
	// by steering (S39), never by writing its agents' words for them.
	MethodAgentMessages MethodName = "agent.messages"
	// MethodAttachmentPut writes an attachment's bytes ONCE (S69 AT3): the
	// only socket crossing bytes ever make. MethodAttachmentRead brings a
	// pinned revision back for RENDERING (the bubble's preview) — never for
	// re-sending: the transcript is references all the way down (AT5).
	MethodAttachmentPut  MethodName = "attachment.put"
	MethodAttachmentRead MethodName = "attachment.read"
	// MethodLoopRun starts a NEW run of a blueprint inside a resting loop
	// (S39 T9, CR2/CR4): the human door — no ask, the typing is the
	// confirmation. The agent's twin arrives with T10 as a catalog action.
	MethodLoopRun MethodName = "loop.run"
	// MethodLoopRunOptions lists what the loop could run next and why each
	// one could not (the dialogue before the start, D6).
	MethodLoopRunOptions MethodName = "loop.run.options"
	// MethodLoopRetryHandlerBlock retries a historical missing-handler
	// block ONCE (S13 T10): one durable transition, one dispatch, and the
	// daemon — not the app — decides eligibility and states the refusal.
	MethodLoopRetryHandlerBlock MethodName = "loop.retry_handler_block"
	// MethodLoopRecover takes a blocked loop back to the step that blocked and runs it again: any block, not only the
	// historical missing handler. The daemon decides eligibility and states the refusal.
	MethodLoopRecover MethodName = "loop.recover"
	// MethodHandlersList and MethodHandlersVerify are the handlers' socket
	// READS for the bench demo runner: the registry and the negative-control
	// verify answered as renderable data. Reads only — the registry's door
	// stays the CLI (PL15's discipline, applied one layer over).
	MethodHandlersList        MethodName = "handlers.list"
	MethodHandlersVerify      MethodName = "handlers.verify"
	MethodComponentCreate     MethodName = "component.create"
	MethodComponentUpdate     MethodName = "component.update"
	MethodComponentList       MethodName = "component.list"
	MethodComponentShow       MethodName = "component.show"
	MethodComponentPromote    MethodName = "component.promote"
	MethodComponentAttach     MethodName = "component.attach"
	MethodComponentDetach     MethodName = "component.detach"
	MethodComponentArchive    MethodName = "component.archive"
	MethodComponentRestore    MethodName = "component.restore"
	MethodBlueprintList       MethodName = "blueprint.list"
	MethodBlueprintFields     MethodName = "blueprint.fields"
	MethodBlueprintModel      MethodName = "blueprint.model"
	MethodBlueprintRender     MethodName = "blueprint.render"
	MethodBlueprintVocabulary MethodName = "blueprint.vocabulary"
	MethodBlueprintShow       MethodName = "blueprint.show"
	MethodBlueprintValidate   MethodName = "blueprint.validate"
	MethodBlueprintSave       MethodName = "blueprint.save"
	// S48 T6 (BP4/BP7/BP10/BP12): the three write doors that join the
	// rewritten save — registered CONDITIONALLY over the daemon's write
	// service (V13), each RequiresInitialize, none Debug, the actor the
	// connection's person and never a param. blueprint.save keeps its line
	// above and its registration below; only its implementation moved
	// (files → rows, over the same BlueprintPutTx single write path).
	MethodBlueprintDuplicate MethodName = "blueprint.duplicate"
	MethodBlueprintDelete    MethodName = "blueprint.delete"
	MethodBlueprintRevert    MethodName = "blueprint.revert"
	// blueprint.override sets or clears ONE allowed field on one lane of a
	// LOCKED (shipped) blueprint — the only write a shipped blueprint takes.
	MethodBlueprintOverride MethodName = "blueprint.override"
	// S48 T7 (BP7/BP8/BP11): the READ doors — registered CONDITIONALLY
	// over the daemon's read service (V13 nil⇒absent), each
	// RequiresInitialize, none Debug, no actor (reads take none, BP12 in
	// reverse: nobody stamps anybody). list and show keep their lines
	// above and grow their answers ADDITIVELY (names/path stay).
	MethodBlueprintUsage    MethodName = "blueprint.usage"
	MethodBlueprintHistory  MethodName = "blueprint.history"
	MethodBlueprintRevision MethodName = "blueprint.revision"
	MethodBlueprintDiff     MethodName = "blueprint.diff"
	MethodBlueprintLint     MethodName = "blueprint.lint"
	// MethodBlueprintPropose is S48 T10's ONE new socket method: the
	// agent's door to file an inert blueprint_save proposal (BP9). It is
	// registered CONDITIONALLY on the records service (the propose-twins'
	// V13 rule) — an unwired runtime leaves it ABSENT, never
	// registered-and-refusing, because the records service is what honours
	// its safety (stamped actor, shared validator, per-agent cap).
	MethodBlueprintPropose MethodName = "blueprint.propose"
	// S40's automation desk (AU14): the catalogue and fire log, and the
	// four human-only write doors (AU1 — the actor is the connection's
	// peer, never a params claim; an agent is refused before anything is
	// judged). No delete: a trigger's history stays where its fires are.
	MethodAutomationTriggerList    MethodName = "automation.trigger.list"
	MethodAutomationTriggerShow    MethodName = "automation.trigger.show"
	MethodAutomationTriggerFires   MethodName = "automation.trigger.fires"
	MethodAutomationTriggerCreate  MethodName = "automation.trigger.create"
	MethodAutomationTriggerUpdate  MethodName = "automation.trigger.update"
	MethodAutomationTriggerEnable  MethodName = "automation.trigger.enable"
	MethodAutomationTriggerDisable MethodName = "automation.trigger.disable"
	// MethodStep runs one bounded step (S5). The skateboard's entry point.
	MethodStep   MethodName = "step"
	MethodAnswer MethodName = "answer"

	// MethodStepStop interrupts a RUNNING step — a person's choice, never
	// the model's — and MethodStepContinue resumes one it stopped, with an
	// optional route/reasoning-effort override. Added for the chat view: see
	// `step_stop.go`'s own doc comment for why a stop is NOT a question and
	// does not touch `internal/loop`, `internal/tools` or `internal/policy`.
	MethodStepStop     MethodName = "step.stop"
	MethodStepContinue MethodName = "step.continue"

	MethodStepShow   MethodName = "step.show"
	MethodStepVerify MethodName = "step.verify"
	MethodStepList   MethodName = "step.list"

	// The loop-wide FIFO transcript window — see loop_transcript.go. Reads
	// `step_message` directly, across every step a loop ran, for the loop
	// workspace's history view: what the live event ring has already
	// forgotten, or everything, for a loop that finished before this window
	// was ever opened.
	MethodLoopTranscript MethodName = "loop.transcript"
	// MethodLoopEvents is one loop's journal as a window — see loop_events.go.
	MethodLoopEvents MethodName = "loop.events"
	// MethodLoopSteps is one loop's step summaries, newest first, with the loop's totals — see loop_steps.go.
	MethodLoopSteps MethodName = "loop.steps"

	// The policy reads (S10). These four exist **for the app only** (ruling
	// R4): the user keeps a read-side window onto policy state — the list,
	// the why, the three in-force answers, the audit log — and nothing more.
	// **Authoring is CLI-only and never arrives here** (R4c/PL15): the
	// write verbs are not registered anywhere, ever, because exposing them
	// over the socket would hand a caller the means to widen the very
	// guards written to constrain it. Additive like every method since S2
	// — no PROTOCOL_VERSION bump (the rule beside that constant, as
	// MethodAsk's comment demonstrates).
	// MethodSearchQuery searches a NAMED corpus through the same resolver
	// the `search` tool calls (S22 T6, D15/I10/P34 — the human's door to
	// what an agent step already can). Registered ONLY when the resolver
	// seam is wired: the no-retrieval build never wires it and the method
	// is absent (V13, V21), mirroring the tool's own absence.
	MethodSearchQuery MethodName = "search.query"

	// MethodRetrievalStatus reports availability and PER-CORPUS staleness
	// for one root (S22 T8, O4/D15/P34) — the socket twin of `aeon index
	// status`, calling the identical functions. It starts no work: a status
	// read that reindexed is the auto-fire V21 forbids.
	MethodRetrievalStatus MethodName = "retrieval.status"

	// MethodRetrievalReindex runs one DELIBERATE pass over one NAMED
	// corpus — the socket twin of `aeon index rebuild`, the same function
	// (D15). Nothing else may reach it: not startup, not status, not a
	// stale query result (V21, asserted against a spy).
	MethodRetrievalReindex MethodName = "retrieval.reindex"

	// MethodPlatformStatus composes the derived-store panel — retrieval,
	// graph and usage blocks — over the SAME read seams the per-subsystem
	// doors use (S54 T2, SP1: one implementation; the panel computes
	// nothing a subsystem also computes). Every block speaks its own
	// unwired state; the door exists only where a daemon built a composer.
	MethodPlatformStatus MethodName = "platform.status"

	// MethodRetrievalReindexPlan previews one corpus's rebuild — stale
	// chunks, estimated tokens, three-state cost — from the SAME estimate
	// walk the priced refusal runs, WITHOUT calling the embedder (S54 T2,
	// SP2: previewing is not doing, and a preview that embedded to price
	// itself charged for the preview).
	MethodRetrievalReindexPlan MethodName = "retrieval.reindexPlan"

	// MethodPlatformChanges reads the S54 T1 audit — who started which
	// derived-store operation, what it started, when — newest first
	// (S54 T2, SP6: the ledger is a read; the writes and their actors
	// were T1's).
	MethodPlatformChanges MethodName = "platform.changes"

	MethodPolicyList    MethodName = "policy.list"
	MethodPolicyWhy     MethodName = "policy.why"
	MethodPolicyInForce MethodName = "policy.inForce"
	MethodPolicyEvents  MethodName = "policy.events"

	// The detail reads (S45 T2, PM5/PM6): the list with its details. Two
	// more READS — `policy.get` carries the whole row (the raw spec rides
	// here, never on the identity list) and `policy.vocabulary` serves the
	// closed sets so no client hardcodes them. Additive like every method
	// since S2 — no PROTOCOL_VERSION bump. (Amended by S45 T4/PM1: the
	// authoring doors below now ride the socket as the human surface; the
	// READS above never were and are not writes, and the grep guard in
	// cmd/aeon/policy_guards_cases_test.go keeps naming exactly the read set plus the
	// sanctioned authoring set below and nothing else under `policy.`.)
	MethodPolicyGet        MethodName = "policy.get"
	MethodPolicyVocabulary MethodName = "policy.vocabulary"

	// S74: which tools a ToolAccess pattern names today (a read over the tools
	// catalog; registered with it, absent when the catalog seam is).
	MethodToolsMatchPattern MethodName = "tools.matchPattern"

	// The authoring audit read (S45 T3, PM8): `policy.changes` answers the
	// append-only audit — who added/removed/seeded/committed/rejected a
	// guard, when, and from which door. One more READ (zero writes, D9) on
	// the read whitelist. (Amended by S45 T4/PM1: the write doors below
	// are the operator's human door on this surface — peer-stamped,
	// audited, before/after confirmed; the cmd/aeon grep guard whitelists
	// exactly these seven read names plus the three authoring names below,
	// and nothing else under `policy.`.)
	MethodPolicyChanges MethodName = "policy.changes"

	// The authoring doors (S45 T4, PM1): `policy.add`, `policy.remove`,
	// `policy.seedCorpus` — the three authoring verbs as the socket's
	// HUMAN door (the operator ruling of 2026-10-30). Registered
	// CONDITIONALLY, only when Table.PolicyAuthoring is wired (V13:
	// absent, not registered-and-refusing — the RegisterSeed pattern);
	// RequiresInitialize true, Debug false (product surface: what keeps
	// these off the narrow projection is PM10's negative guards, never an
	// accident). The actor is the connection's peer credential (never a
	// param), PL1 and every boundary refusal travel verbatim, every
	// successful write carries before/after in-force and lands exactly one
	// policy_change audit row. PM10 (T6) owns the positive restatement of
	// what must stay ABSENT: none of these reaches the narrow surface,
	// agent services, the tool registry or the records catalog.
	MethodPolicyAdd        MethodName = "policy.add"
	MethodPolicyRemove     MethodName = "policy.remove"
	MethodPolicySeedCorpus MethodName = "policy.seedCorpus"

	// The history reads (S25 T4): the app's window onto a project's history —
	// three read-only methods, mirroring policy.*'s four (S10 T13). Additive,
	// like every method since S2 — no PROTOCOL_VERSION bump. Each requires
	// Initialize (the table is not complete without its store) and is a read
	// (no audit row — a read is not an event).
	MethodHistoryList   MethodName = "history.list"
	MethodHistoryShow   MethodName = "history.show"
	MethodHistoryExport MethodName = "history.export"
	// S53 T1 (HV5): the two new history READS, same family, same rules — each
	// RequiresInitialize, none Debug, riding the same history seam (unwired
	// answers ErrStoreUnavailable, like its siblings), classified read in S47's
	// table and OFF the narrow surface. coverage is HV1's honesty read,
	// vocabulary HV2's closed sets.
	MethodHistoryCoverage   MethodName = "history.coverage"
	MethodHistoryVocabulary MethodName = "history.vocabulary"

	// The loop-workspace surface (S31). Additive, like every method since S2 —
	// no PROTOCOL_VERSION bump. The shapes are the brief's §5 table, and every
	// one is keyed by stepId, never by loop (D12/P33: nothing here assumes one
	// running step per loop).
	//
	// **`step.steer*` and `terminal.*` must never reach the narrow HTTP/MCP
	// surface (LW8)** — a terminal is the person's own shell on a P26 socket,
	// and steering is product control for the connected app, not for a remote
	// trigger. The guard test (S31 T9) mirrors
	// `TestNoPolicyAuthoringReachesTheNarrowSurface`; it exists because the
	// S19 projection is built from `PublicMethods()` and these are not Debug.
	//
	// Handlers are per-feature files: `steer.go`, `workspace_git.go`,
	// `terminal.go`. They arrive with T4, T6 and T7 respectively; until then
	// each stub refuses with `ErrNotWired`, naming its task — half-wired is
	// visible, never silently permissive.
	MethodStepSteer       MethodName = "step.steer"
	MethodStepSteers      MethodName = "step.steers"
	MethodStepSteerCancel MethodName = "step.steer.cancel"

	MethodWorkspaceGitStatus MethodName = "workspace.git.status"
	MethodWorkspaceGitDiff   MethodName = "workspace.git.diff"
	// MethodWorkspaceGitChanges is every changed path with its added/removed line counts — see workspace_git.go.
	MethodWorkspaceGitChanges MethodName = "workspace.git.changes"

	MethodTerminalOpen   MethodName = "terminal.open"
	MethodTerminalInput  MethodName = "terminal.input"
	MethodTerminalResize MethodName = "terminal.resize"
	MethodTerminalAttach MethodName = "terminal.attach"
	MethodTerminalDetach MethodName = "terminal.detach"
	MethodTerminalClose  MethodName = "terminal.close"
	MethodTerminalList   MethodName = "terminal.list"

	// The agent-services surface (S14, T10). Additive, like every method
	// since S2 — no PROTOCOL_VERSION bump (the rule beside MethodAsk).
	//
	// **One table: these names are registered here and nowhere else.**
	// `agent.go` holds the ONE handler that forwards them all through
	// Table.Agents (injected by cmd/aeon — AS11's deletability:
	// `rm -r internal/agentsvc` leaves this package building, and the
	// methods answer ErrNotWired naming the slice rather than pretending).
	// The enumeration that documents what each one CANNOT do — S19's N12
	// discipline, because this is the same kind of list — lives beside that
	// ONE forwarding handler, and a test pins that the enumeration, this
	// registration, and agentsvc's own dispatch cover exactly these names.
	//
	// The three WRITES (invoke, task.complete, task.fail) are Debug-marked
	// like step.steer and terminal.* are: they open or settle work on a
	// person's loops, which is product control for the connected app over a
	// P26 socket — never a remote trigger for the S19 projection (LW8's
	// guard). The four READS follow policy.*: the app's window, public.
	MethodAgentDiscover MethodName = "agent.discover"
	MethodAgentCard     MethodName = "agent.card"
	MethodAgentInvoke   MethodName = "agent.invoke"
	MethodAgentTaskList MethodName = "agent.task.list"
	MethodAgentTaskShow MethodName = "agent.task.show"

	// The callee's reports, and the app's ask. Debug: true — see above.
	MethodAgentTaskComplete MethodName = "agent.task.complete"
	MethodAgentTaskFail     MethodName = "agent.task.fail"

	// S38 T15 (OL10/OL11): the five doors a person alone may knock on. The
	// catalog's dot-name twins are registered from the catalog itself
	// (records.go — one definition, two doors); these five have no Spec,
	// because a decision is never a tool and the board read and the goal
	// picture are composed from rows, never stored.
	MethodGoalDecide  MethodName = "goal.decide"
	MethodBriefDecide MethodName = "brief.decide"
	MethodTaskDecide  MethodName = "task.decide"
	MethodTaskList    MethodName = "task.list"
	MethodGoalShow    MethodName = "goal.show"
	// MethodGoalList is the records catalog's goal.list, named so the app's
	// contract types it (its fixture already exists: records/goal-list.json).
	MethodGoalList MethodName = "goal.list"

	// S15's ONE read: what is installed, in which state, from where. The
	// MANAGEMENT (import, enable, disable, revoke) is deliberately absent —
	// exposing it over a socket would hand a caller the means to install the
	// thing that bypasses the guards (R6, exactly S10's authoring rule and
	// S12's MCP one). READ is fine; managing is CLI and app only.
	//
	// Answered by the ENTRY POINT (Table.Plugins), not by Table.Agents: the
	// agent-services layer must not know the plugin registry exists (the
	// absent-layer sweep counts this package's and its own imports — the
	// direction plugin←cmd is the only one that builds without cycles).
	MethodAgentPlugins MethodName = "agent.plugins"

	// S34 T1's one read: the roster as identity — first-party bodies and
	// imported plugins alike, with provenance and the REAL state vocabulary
	// (see agent_roster.go) — never the plugin ledger's management shape,
	// which stays MethodAgentPlugins's own (different fact, different door,
	// TB1).
	MethodAgentList MethodName = "agent.list"

	// S34 T2's read: what an agent may use, already decided once — the S12
	// authority view (internal/mcp.Registry.GrantView) verbatim, per tool:
	// available / unavailable / refused / ask, with the exact reason. Never
	// a second visibility×authority join (S36 MD2's rule, applied here too).
	MethodToolsGrants MethodName = "tools.grants"

	// S34 T3's read: the MCP config exactly as internal/mcp sees it — one of
	// three states (absent / valid / broken, never a per-server `refused`;
	// the real loader is all-or-nothing) plus the probe cache T4 fills.
	MethodMCPStatus MethodName = "mcp.status"

	// S34 T4's one genuinely new daemon behavior: an operator-triggered,
	// single-flight, bounded-timeout dial of one configured server —
	// initialize + tools/list, never tools/call. Joins Table.MCP's switch
	// beside mcp.status rather than a second seam.
	MethodMCPCheck MethodName = "mcp.check"

	// S46 T3's five — the PERSON's MCP management reads (MG5-reads, MG8):
	// the list with details, the one-server read, the per-agent grants
	// view, the vocabulary taken from the validator, and the audit. These
	// are the MANAGEMENT view — unscoped by design, because the human door
	// sees everything (MG8: "the person sees all; agents do not"). The
	// agent-scoped reads are T8's tools and a DIFFERENT code path
	// (Registry.ToolsFor(self) over the identity the daemon stamps), so no
	// agent stands behind these names; a test pins that no tool handler
	// calls through them. Registered ONLY by RegisterMCPAdmin, conditional
	// on Table.MCPAdmin (V13: nil seam ⇒ absent, not registered-and-
	// refusing); RequiresInitialize true, Debug false; the narrow
	// projection's allow-list never names them and never will. Reads only
	// — the write doors are T5's, and their names appear NOWHERE in this
	// file.
	MethodMCPServers    MethodName = "mcp.servers"
	MethodMCPServer     MethodName = "mcp.server"
	MethodMCPGrants     MethodName = "mcp.grants"
	MethodMCPVocabulary MethodName = "mcp.vocabulary"
	MethodMCPChanges    MethodName = "mcp.changes"
	// MethodMCPImportFile is S46 T4's HUMAN door (the first MCP WRITE
	// door): deliberately adopt a legacy mcp.json (mode dry-run | replace).
	// Registered with the five reads iff the MCPAdmin seam AND the actor
	// stamp are wired (RegisterMCPImportFile) — the actor rule is what
	// separates it from the reads.
	MethodMCPImportFile MethodName = "mcp.importFile"
	// S46 T8 (2026-11-02) — the five AGENT-SCOPED twins (MG8/MC2): the own-
	// view reads (servers/grants/status) and the two NARROWING proposes.
	// Registered by RegisterMCPMine iff the records service AND the MCP
	// admin seam stand (V13); they answer through the records door, scoped
	// by the stamped agent — a person session is refused with a pointer
	// sentence and no param can name another agent.
	MethodMCPMineServers       MethodName = "mcp.mine.servers"
	MethodMCPMineGrants        MethodName = "mcp.mine.grants"
	MethodMCPMineStatus        MethodName = "mcp.mine.status"
	MethodMCPMineProposeRemove MethodName = "mcp.mine.proposeRemove"
	MethodMCPMineProposeRevoke MethodName = "mcp.mine.proposeRevoke"
	// S46 T5 — the seven human write doors (the eighth write, importFile,
	// stands above). RequiresInitialize, not Debug, conditional on the
	// seam; the actor is the peer credential, never a param.
	MethodMCPServerAdd        MethodName = "mcp.server.add"
	MethodMCPServerUpdate     MethodName = "mcp.server.update"
	MethodMCPServerRemove     MethodName = "mcp.server.remove"
	MethodMCPServerSetEnabled MethodName = "mcp.server.setEnabled"
	MethodMCPServerShare      MethodName = "mcp.server.share"
	// MethodMCPServerInspect runs a CANDIDATE server's command, reads the tools it
	// offers (initialize + tools/list, never tools/call) and stops it — the person's
	// way to see what a server is before adding it. Nothing is stored.
	MethodMCPServerInspect MethodName = "mcp.server.inspect"

	// The MCP servers' processes (mcp_process.go): two reads and the person's
	// start / stop / restart. The daemon starts every enabled stdio server when
	// it starts; these are the control over what it started.
	MethodMCPProcesses      MethodName = "mcp.processes"
	MethodMCPProcessLogs    MethodName = "mcp.process.logs"
	MethodMCPProcessStart   MethodName = "mcp.process.start"
	MethodMCPProcessStop    MethodName = "mcp.process.stop"
	MethodMCPProcessRestart MethodName = "mcp.process.restart"
	MethodMCPGrantDeclare   MethodName = "mcp.grant.declare"
	MethodMCPGrantRevoke    MethodName = "mcp.grant.revoke"
	MethodMCPCoreSet        MethodName = "mcp.core.set"

	// S35 T3's three — the inert half of the staging line (brief §4): an
	// operator-clicked proposal row that constrains nothing, writable from
	// the socket because discarding it loosens nothing (TD1/TD7).
	// Deliberately absent, here and everywhere in this file, forever:
	// MethodProposalCommit and MethodProposalReject — commit/reject are the
	// human-only CLI door (T5); a name for either on this table would be
	// exactly the side door brief §7 forbids.
	MethodProposalCreate  MethodName = "proposal.create"
	MethodProposalList    MethodName = "proposal.list"
	MethodProposalDiscard MethodName = "proposal.discard"

	// S45 T5's three — the decision doors (PM1/PM7): an operator decides a
	// proposal from the app, over the socket, AS the human door (the
	// operator ruling of 2026-10-30, the same one policy.add/remove/
	// seedCorpus arrived under at T4). `proposal.get` reads the row with its
	// digest and an advisory preview; `proposal.commit {id, digest}` applies
	// the effect through internal/desk's one registry and flips the row;
	// `proposal.reject {id, reason}` flips it with a required reason and
	// applies nothing. Registered CONDITIONALLY, only when
	// Table.ProposalDecider is wired (V13: absent, not
	// registered-and-refusing — the RegisterSeed pattern);
	// RequiresInitialize true, Debug false (product surface: what keeps
	// these off the narrow projection is PM10's negative guards, never an
	// accident). The actor is the connection's peer credential (never a
	// param, PM7.3); the commit is bound to the digest proposal.get
	// returned (PM7.1); `agent_import` commits are refused BY NAME until
	// T10. The S35 comment above stays TRUE for what it named: no commit
	// door exists without the service, and the methods live in
	// proposals_decide.go, never in proposals.go (whose header the absence
	// story still describes until T6 amends it).
	MethodProposalGet    MethodName = "proposal.get"
	MethodProposalCommit MethodName = "proposal.commit"
	MethodProposalReject MethodName = "proposal.reject"

	// S45 T7 (PM9): the two PROPOSE twins — catalog actions on the socket
	// answered through the records service (the Do the tools call; one
	// implementation, two doors, S38 OL10). They create INERT proposal
	// rows — the agent door for tightening policy, never a policy write
	// (TD2: nothing is enforced until a person commits at T4/T5's human
	// door). Registered CONDITIONALLY, only when Table.Records is wired
	// (V13, the RegisterSeed pattern — registerPolicyPropose in
	// policy_propose.go owns the registration; registerRecords skips these
	// two names); RequiresInitialize true, Debug false. Declared in this
	// file like every wire name (T4/T5 precedent): protogen reads its
	// method list from these declarations, and the names belong to the
	// table, not to one handler file.
	MethodPolicyProposeAdd    MethodName = "policy.proposeAdd"
	MethodPolicyProposeRemove MethodName = "policy.proposeRemove"

	// S56 T8 (AL11, 2026-11-01): the two PROPOSE twins for autonomy — the
	// agent's door to NARROW what it may do, never to widen (the two kinds
	// are restrictive: revoke a grant, add a veto; no kind creates an
	// allow). Answered through the records service like the policy twins
	// (RecordsAPI.Do; one implementation, two doors), registered
	// CONDITIONALLY by RegisterAutonomyPropose (autonomy_propose.go) only
	// when Table.Records is wired (V13, the RegisterSeed pattern);
	// registerRecords skips all five autonomy names (the three reads are
	// owned by T6's RegisterAutonomy, which registers UNCONDITIONALLY and
	// whose t.register panics on a duplicate — the skip must be explicit,
	// because at NewTable time the autonomy names do not stand yet).
	// RequiresInitialize true, Debug false. Declared here like every wire
	// name: protogen reads its method list from these declarations.
	MethodAutonomyProposeRevoke MethodName = "autonomy.proposeRevoke"
	MethodAutonomyProposeDeny   MethodName = "autonomy.proposeDeny"

	// S35 T4's three plain writes (brief §3's line): the imported agent
	// already passed blocklist + consent at import (S15), so moving it
	// between enable/disable/revoke starts something already permitted —
	// unlike import, which widens authority and stays CLI/app-only,
	// reachable from nowhere else. Calls the SAME cmd/aeon functions the
	// CLI verbs call (plugin.Enable/Disable/Revoke), never
	// TransitionPluginAgent one layer further down, so the Rehydrate
	// re-check (enable) and whatever Revoke records beyond the bare
	// transition travel through this door too. Deliberately NOT members of
	// agentServiceMethods (agent.go) — that slice is the S14/S15/S21
	// agent-to-agent TASK lifecycle, whose own tests require an Emits
	// event for every write because agent.invoke spawns an async task a
	// caller must subscribe to find the end of; these three are
	// synchronous (the result returns in the RPC response, like
	// loop.create) and invent no notification to satisfy that unrelated
	// shape.
	//
	// Cannot, per method:
	//   enable — cannot skip the Rehydrate re-check (a package that grew a
	//     request since import is caught HERE too, exactly as `aeon agent
	//     enable` catches it — this seam calls the same cmd/aeon function,
	//     never a bare TransitionPluginAgent that would skip the re-read);
	//     cannot enable a name with no plugin record; cannot move a
	//     revoked agent (terminal at the database itself).
	//   disable — cannot re-check the package the way enable does
	//     (disabling never re-trusts a package, it only stops one); cannot
	//     move a revoked agent; cannot widen anything — stopping narrows,
	//     which is why this is a plain write and not a staged proposal.
	//   revoke — cannot accept an empty reason (refused early here for UX,
	//     AND by the store's own CHECK constraint even if this handler's
	//     guard were ever bypassed, criterion 5); cannot un-revoke
	//     (terminal; a re-import creates a NEW record, AP4); cannot do
	//     more or less than `aeon agent revoke` does — calls plugin.Revoke
	//     verbatim, so whatever it records beyond the bare transition
	//     travels through this door too.
	MethodAgentEnable  MethodName = "agent.enable"
	MethodAgentDisable MethodName = "agent.disable"
	MethodAgentRevoke  MethodName = "agent.revoke"

	// S43 T7 — the agent-management family. Every agent is a row, and the
	// row's one surface is the daemon (P34); the CLI and the app are
	// secondary clients of these methods, never a second implementation.
	// One seam (Table.AgentManage) carries all of them, dispatched by name,
	// the shape agent.enable/disable/revoke already use. Writes (create /
	// import / export / duplicate / delete / edit) are not PL15 policy
	// authoring — they touch the agent table, not the closed policy set — so
	// they are ordinary daemon writes, not the human-only door.
	MethodAgentCreate    MethodName = "agent.create"
	MethodAgentImport    MethodName = "agent.import"
	MethodAgentExport    MethodName = "agent.export"
	MethodAgentDuplicate MethodName = "agent.duplicate"
	MethodAgentDelete    MethodName = "agent.delete"
	MethodAgentEdit      MethodName = "agent.edit"
	MethodAgentView      MethodName = "agent.view"
	MethodAgentMigrate   MethodName = "agent.migrate"

	// S73 T2 — the management window. `agent.rows` is the paged, sorted,
	// searched READ an agents management table asks for, and it is the first
	// adopter of the daemon's paging convention (docs/aeon/PAGING.md); its
	// sibling `agent.changes` (T5) reads the ledger the writes below mint.
	//
	// It is NOT agent.list, and the two stay apart on purpose: that one is
	// S34 T1's identity ROSTER for dispatch — every row, no window, the
	// vocabulary of provenance and state — and this one is a page of the
	// management columns in an order a person chose. Folding one into the
	// other would put a LIMIT on the thing that decides who may be invoked.
	// Both are reads, so neither carries an actor (S73 AR4: the person is
	// stamped at the ACTS, and SP6 draws the line there).
	MethodAgentRows MethodName = "agent.rows"

	// S73 T5 — the ledger's window. `agent.changes` reads the rows AR1's
	// append-only `agent_change` table holds, newest first, narrowed to one
	// agent's slug or to everything, and it is the paging convention's SECOND
	// adopter: PG5's claim is about a shape, and a shape that has held once is
	// an anecdote. It offers no sort, no direction and no search term — a
	// history has one order, and a read refuses a key it cannot honour rather
	// than accepting it and ignoring it (AR6). Same seam, same registration
	// shape as `agent.rows`, and the same absence of an actor: this act is
	// looking, not doing.
	MethodAgentChanges MethodName = "agent.changes"

	// S30 T5 — the code-graph family. The three reads (T5's explain/path/
	// query) and the four maintenance doors the pre-flight identified
	// (build/reparse/delete/status) plus roots for the picker; graph.go
	// argues the socket-write tier (roadmap.go's precedent, PL15
	// untouched). The consts live here with the rest of the vocabulary:
	// tools/protogen reads method.go alone for the generated contract.
	MethodGraphExplain MethodName = "graph.explain"
	MethodGraphPath    MethodName = "graph.path"
	MethodGraphQuery   MethodName = "graph.query"
	MethodGraphBuild   MethodName = "graph.build"
	MethodGraphReparse MethodName = "graph.reparse"
	MethodGraphDelete  MethodName = "graph.delete"
	MethodGraphStatus  MethodName = "graph.status"
	MethodGraphRoots   MethodName = "graph.roots"

	// S33 T8 (TL10/TL11) — the telemetry reads: THE primary surface for
	// loop/component/project/workspace counting. Reads ONLY: no write
	// method may ever join this family (the detail rows are written at
	// their capture points inside the runtime's seams, nowhere else).
	MethodTelemetryLoop      MethodName = "telemetry.loop"
	MethodTelemetryComponent MethodName = "telemetry.component"
	MethodTelemetryProject   MethodName = "telemetry.project"
	MethodTelemetryWorkspace MethodName = "telemetry.workspace"

	// S44 T5 (CS/CS9/CS10/CS12) — the settings surface: the ONLY door the
	// Settings UI (T8) uses and the CLI (T7) is a client of (P34). Registered
	// CONDITIONALLY by RegisterSettings, beside the telemetry block (V13: a
	// nil Settings leaves the five absent, not registered-and-refusing — the
	// S22 search.query precedent). None is Debug, and none is projected onto
	// the narrow surface (CS9's allow-list never names them).
	MethodSettingsList    MethodName = "settings.list"
	MethodSettingsGet     MethodName = "settings.get"
	MethodSettingsSet     MethodName = "settings.set"
	MethodSettingsUnset   MethodName = "settings.unset"
	MethodSettingsHistory MethodName = "settings.history"

	// S43-extra T6 (SD10) — the seed surface: the human door over the one
	// seed engine (P34). The daemon is the primary interface; the CLI (T7)
	// and the bench page (T8) are clients of it. Registered CONDITIONALLY by
	// RegisterSeed (V13: a nil Seed leaves the four absent, not
	// registered-and-refusing — the S22 search.query and S44 settings
	// precedents). status/plan are READS (zero writes); apply/restore are
	// WRITES whose actor comes from the connection (the daemon's ActorFor
	// seam), never params. None is Debug, and none is projected onto the
	// narrow surface (the S43 / S44 CS9 guards keep that true).
	MethodSeedStatus  MethodName = "seed.status"
	MethodSeedPlan    MethodName = "seed.plan"
	MethodSeedApply   MethodName = "seed.apply"
	MethodSeedRestore MethodName = "seed.restore"
	// S64 T3 — the async-worker ledger's reads and cancel. NO start
	// method: the registry holds Go functions, and an untyped start door
	// would let a caller name a type it cannot supply arguments for —
	// starting belongs to the owning service's own method (graph.build
	// starts the graph.build worker). TestAsyncWorkersFamilyCarriesNoStart
	// keeps that true by inspection of the table.
	MethodAsyncWorkersList   MethodName = "asyncworkers.list"
	MethodAsyncWorkersGet    MethodName = "asyncworkers.get"
	MethodAsyncWorkersCancel MethodName = "asyncworkers.cancel"

	// S65 T3 — the Agent Skills catalog (the SKILL.md format — the THIRD
	// meaning of "skill" in this project; internal/skills' package comment
	// keeps the three apart). Three reads and two human writes, registered
	// CONDITIONALLY by RegisterSkills (V13: a nil Skills leaves all five
	// absent, not registered-and-refusing — the settings precedent). None
	// is Debug (they are product surfaces) and none is named by the narrow
	// surface's Actions() allow-list (S44 CS9's precedent; cmd/aeon's
	// skills_guard_test keeps it true).
	MethodSkillsList     MethodName = "skills.list"
	MethodSkillsGet      MethodName = "skills.get"
	MethodSkillsValidate MethodName = "skills.validate"
	MethodSkillsInstall  MethodName = "skills.install"
	MethodSkillsRemove   MethodName = "skills.remove"

	// S56 T6 (AL11) — the autonomy human door: three writes (grant/deny/
	// revoke) and a read set (list/show/vocabulary/effective/pinned/
	// changes/uses). Registered CONDITIONALLY by RegisterAutonomy (V13: a
	// nil Autonomy leaves all ten absent, not registered-and-refusing —
	// the settings and seed precedents). The writes stamp the actor from
	// the connection's peer, never params, and refuse a kind that is not
	// `user` (AL11 lock 2; the store's actor check is lock 3). None is
	// Debug, and none is projected onto the narrow surface (AL17; T7's
	// write-side guards keep that true).
	MethodAutonomyList       MethodName = "autonomy.list"
	MethodAutonomyShow       MethodName = "autonomy.show"
	MethodAutonomyVocabulary MethodName = "autonomy.vocabulary"
	MethodAutonomyEffective  MethodName = "autonomy.effective"
	MethodAutonomyPinned     MethodName = "autonomy.pinned"
	MethodAutonomyChanges    MethodName = "autonomy.changes"
	MethodAutonomyUses       MethodName = "autonomy.uses"
	MethodAutonomyGrant      MethodName = "autonomy.grant"
	MethodAutonomyDeny       MethodName = "autonomy.deny"
	MethodAutonomyRevoke     MethodName = "autonomy.revoke"
	// S55 T5 (LC2–LC5, LC15, LC16) — the loop-control doors: the person's
	// 0600 + peer-credential surface over the T3 control core (stop,
	// resume, cancel), the T2 request desk (requestStop's agent twin is
	// cataloged in T10; requests/dismiss are the person's read and verdict)
	// and the one READ (control: eligibility + open requests + cap-hit +
	// the last ControlEventWindow events). Declared HERE, not in the
	// family file, because tools/protogen reads this file to build the
	// app's contract — every MethodName in the repo is declared in this
	// one const block. All seven are registered CONDITIONALLY by
	// RegisterLoopControl (V13: a nil LoopControl leaves the seven ABSENT,
	// not registered-and-refusing — the settings precedent); none is Debug
	// and none is projected onto the narrow surface (S55's amend test
	// keeps that true by walk). The actor is stamped from the connection
	// (the daemon's ActorFor seam), never params.
	MethodLoopStop            MethodName = "loop.stop"
	MethodLoopResume          MethodName = "loop.resume"
	MethodLoopCancel          MethodName = "loop.cancel"
	MethodLoopControl         MethodName = "loop.control"
	MethodLoopRequestStop     MethodName = "loop.requestStop"
	MethodLoopControlRequests MethodName = "loop.control.requests"
	MethodLoopControlDismiss  MethodName = "loop.control.dismiss"

	// S75: the loop's todo list — a read, and the person's close (human door
	// only). Registered with the seam, absent without a store (V13).
	MethodLoopTodos MethodName = "loop.todos"
	// S76: the block of questions a parked step asked (answered and pending).
	MethodLoopQuestions  MethodName = "loop.questions"
	MethodLoopTodosClose MethodName = "loop.todos.close"

	// The variable manager (vault.*): named values the app holds for processes and agents.
	// Two reads and the person's two writes (human door only); registered with
	// the seam, absent without a store and a key (V13).
	MethodVaultList    MethodName = "vault.list"
	MethodVaultChanges MethodName = "vault.changes"
	MethodVaultSet     MethodName = "vault.set"
	MethodVaultDelete  MethodName = "vault.delete"

	// S55 T8 (LC): the budget plane's doors — four READS (list-with-details,
	// show, effective-for-a-loop, spend.report; vocabulary) and the three
	// HUMAN WRITES (set, remove, and loop.raise — the raise lives here, not
	// on the loop block, because it is the budget's act: it lifts a cap).
	// Registered CONDITIONALLY by RegisterBudget (V13: a nil Budget seam
	// leaves all eight ABSENT, not registered-and-refusing); none is Debug
	// and none is projected onto the narrow surface (the T8 guard
	// amendment, cmd/aeon/budget_amend_test.go, keeps that true by walk —
	// the read twins are the only specs the records catalog may carry,
	// OD10). The actor is stamped from the connection for the three
	// writes, never params.
	MethodBudgetList       MethodName = "budget.list"
	MethodBudgetShow       MethodName = "budget.show"
	MethodBudgetEffective  MethodName = "budget.effective"
	MethodBudgetSet        MethodName = "budget.set"
	MethodBudgetRemove     MethodName = "budget.remove"
	MethodBudgetVocabulary MethodName = "budget.vocabulary"
	MethodSpendReport      MethodName = "spend.report"
	MethodLoopRaise        MethodName = "loop.raise"

	// S55 T9 (LC17/LC18): the watchdog plane's doors — three READS
	// (list-with-details, show, vocabulary — the vocabulary is the closed
	// signal/action set WITH availability, rule 4: the client builds from
	// the daemon, never a local copy) and the three HUMAN WRITES (set,
	// setEnabled, remove — the person stamped from the connection, never
	// params; a loosening — a disable or a remove — confirms against the
	// rendered canonical statement, the T8 PM7.1 pattern). Registered
	// CONDITIONALLY by RegisterWatchdog (V13: a nil seam leaves all six
	// ABSENT, not registered-and-refusing); none is Debug and none is
	// projected onto the narrow surface (the T9 guard amendment,
	// cmd/aeon/watchdog_amend_test.go, keeps that true by walk — the only
	// agent read twin is watchdog_list, OD10; no agent WRITE exists).
	MethodWatchdogList       MethodName = "watchdog.list"
	MethodWatchdogShow       MethodName = "watchdog.show"
	MethodWatchdogVocabulary MethodName = "watchdog.vocabulary"
	MethodWatchdogSet        MethodName = "watchdog.set"
	MethodWatchdogSetEnabled MethodName = "watchdog.setEnabled"
	MethodWatchdogRemove     MethodName = "watchdog.remove"

	// S47 T5 (TC10, TC11): the TOOLS CATALOG reads — what exists, what each one
	// is, who may call it and why not, one agent's whole view, and whether the
	// seeded rows still match the code. All four are READS (TC1: S47 ships zero
	// writes); all four are conditionally registered by RegisterToolsCatalog
	// (V13: a nil `Table.ToolsCatalog` leaves them ABSENT, not
	// registered-and-refusing — the RegisterSettings/RegisterMCPAdmin rule); all
	// four are `RequiresInitialize` and none is `Debug`; and none is projected
	// onto the narrow HTTP/MCP-stdio surface or enumerated for the agent
	// services. This is the PERSON's catalog — a human door, the same class as
	// S46's management reads — while an agent's own view is T7's separate
	// `tools_mine`/`tool_show` pair (TC9/MC2: a different code path on purpose,
	// so no agent can read another agent's grants through this door).
	// `tools.grants` (S34) is UNCHANGED and stays the raw S12 authority view:
	// this slice adds a catalog, it does not rewrite an existing read.
	MethodToolsList    MethodName = "tools.list"
	MethodToolsGet     MethodName = "tools.get"
	MethodToolsByAgent MethodName = "tools.byAgent"
	MethodToolsStatus  MethodName = "tools.status"

	// S47 T10 (TC3 + TC10, operator ruling 2026-11-02): the catalog's own
	// VOCABULARY door. TC10 froze the surface at eight methods, and T10's panel
	// was written against capability meanings "sent by the daemon in the
	// vocabulary data" — which no door answered, because the sentences lived
	// only as doc comments in `internal/tools/meta.go`. Two ways to close that:
	// let the screen author the sentences (a second vocabulary, the thing TC3
	// exists to prevent), or send them from the code that owns them. The
	// operator chose the second, so this ninth read carries what the panel
	// cannot be trusted to write: the capability names with their one-line
	// meanings, plus the family, source and re-run-class spellings a filter may
	// be offered. Same discipline as the other eight: a READ, `RequiresInitialize`,
	// not `Debug`, registered only with the catalog seam (V13), never on the
	// narrow projection and never enumerated for the agent services.
	MethodToolsVocabulary MethodName = "tools.vocabulary"

	// S47 T6 (TC6, TC8): the SURFACE doors — what the daemon exposes and to whom,
	// answered from the closed classification table (surface.go) beside the doors
	// computed from the live structures (surface_doors.go). Both are READS, both
	// are `RequiresInitialize` and neither is `Debug` (product surface: what keeps
	// them off the narrow HTTP/MCP-stdio projection is the allow-list that never
	// names them, never a flag), and neither is enumerated for the agent services
	// or twinned into the tool registry — a person's question about the surface is
	// not an agent's question about its own reach.
	MethodSurfaceList MethodName = "surface.list"
	MethodSurfaceGet  MethodName = "surface.get"

	// S62 T6 (BK7, BK12): the backup HUMAN DOOR — five methods over the daemon's
	// one backup service: run (the person starts a backup now), list (what
	// files exist and what the ledger says about them), verify (the three
	// levels on one file, the person's drill), status (the four-state machine)
	// and prune (retention, dry-run first). Conditionally registered by
	// RegisterBackup (V13: a nil seam leaves all five ABSENT, not
	// registered-and-refusing); all five are RequiresInitialize and none is
	// Debug; run/verify/prune stamp the actor from the connection (never a
	// param) and map backup.RunError onto ErrBackupRefused with the reason in
	// data.what. RESTORE IS ABSENT BY DESIGN (BK9): it is the CLI's direct
	// door and no restore method or tool will exist at this task or any later
	// one — the guard in cmd/aeon/backup_guard_test.go keeps it so by walk.
	MethodBackupRun    MethodName = "backup.run"
	MethodBackupList   MethodName = "backup.list"
	MethodBackupVerify MethodName = "backup.verify"
	MethodBackupStatus MethodName = "backup.status"
	MethodBackupPrune  MethodName = "backup.prune"

	// S62 T8 (BK14): the two export doors. run is the family's one WRITE:
	// it stamps the actor from the connection (never a param, BK13) and
	// writes the one backup_run row (kind=export, reason=manual) when — and
	// only when — the request reaches the engine; a param-shape refusal
	// before the engine writes no row. sets is OPEN (RequiresInitialize
	// false): it answers what the binary can export from the schema it
	// holds, and a not-yet-initialised daemon still knows that answer, so
	// it cannot join the guard's `all` list the backup family does.
	// Conditionally registered by RegisterExport (V13: a nil seam leaves
	// both ABSENT); neither is Debug. run's refusals ride the backup
	// family's voice, ErrBackupRefused (-31863, the reuse T8 records in
	// errors.go — -31864 stays reserved), the closed-set kind in data.kind.
	MethodExportRun  MethodName = "export.run"
	MethodExportSets MethodName = "export.sets"

	// S47 T7 (TC9, MC2, OL3): the agent's OWN tool view. These two are the socket
	// twins of the `tools_mine` and `tool_show` catalog specs, and they are
	// registered by `registerRecords` from the spec's `Method` field — the
	// `loop.get`/`brief.show` shape, one definition and two doors — so unlike
	// T5's four and T6's two they need no explicit registerer: their answers come
	// from the daemon's own store, registry and policy set through the records
	// door, and `handleRecord`'s "records are unavailable in this runtime" is the
	// third state (V13). Both are READS (TC1: S47 still ships zero writes), both
	// `RequiresInitialize`, neither `Debug`, neither projected onto the narrow
	// HTTP/MCP-stdio surface nor enumerated for the agent services. A PERSON is
	// refused by name at `personDoorRefusal` with the unscoped door that does
	// answer them: these two serve SELF, and a person holding the endpoint must
	// not be able to read back what each agent holds by walking them one slug at
	// a time (S46 T8's `mcp.mine.*` ruling, carried to the tool surface).
	MethodToolsMineList MethodName = "tools.mine.list"
	MethodToolsMineShow MethodName = "tools.mine.show"

	// S57 (VG1/VG17/VG18): the gate plane — twenty-two socket doors for the
	// human (twelve verify reads/writes, six merge-rule, two dod) plus the
	// three proposal twins an agent may file (inert until a person commits).
	// All registered CONDITIONALLY over the three gate seams (V13: an
	// unwired runtime leaves the whole plane ABSENT, never registered-and-
	// refusing), each RequiresInitialize, none Debug, the actor from the
	// peer and never a param. The names lived in the domain files until
	// S57 T11's pre-flight moved them HERE — protogen enumerates THIS file
	// alone, and the app's contract could not see a gate method, which is
	// the S48/S59 convention every UI-guarded panel already relies on
	// (S57-verification-gates T11.md records the finding). The constants
	// kept their names and comments; the logic stayed in verify.go,
	// verify_propose.go, mergerule.go and dod.go.
	MethodVerifyList MethodName = "verify.list"
	MethodVerifyShow MethodName = "verify.show"
	MethodVerifyRuns MethodName = "verify.runs"
	// verify.runShow is the one camelCase wire name in the plane (the run's
	// detail door; the CLI verb stays `verify run-show` — T10's parity row).
	MethodVerifyRunShow MethodName = "verify.runShow"
	// verify.vocabulary carries the closed sets AND the tryStatement the UI
	// confirms with — the UI types no vocabulary of its own (VG21).
	MethodVerifyVocabulary    MethodName = "verify.vocabulary"
	MethodVerifyTemplates     MethodName = "verify.templates"
	MethodVerifyAdd           MethodName = "verify.add"
	MethodVerifyUpdate        MethodName = "verify.update"
	MethodVerifyArchive       MethodName = "verify.archive"
	MethodVerifyRestore       MethodName = "verify.restore"
	MethodVerifyApplyTemplate MethodName = "verify.applyTemplate"
	// verify.try is a write wearing a read's clothes: it runs one command
	// under the policy gate with a scrubbed environment and records the
	// run (trigger=manual, peer-stamped). No direct door exists for it
	// (T10's gate_client refuses offline before anything opens).
	MethodVerifyTry MethodName = "verify.try"
	// The six merge-rule doors. get answers the EFFECTIVE rule with its
	// source and the floor checklist; preview runs the same classifier the
	// merge gate runs, so the screen cannot disagree with the merge (VG8).
	MethodMergeRuleGet        MethodName = "mergerule.get"
	MethodMergeRulePreview    MethodName = "mergerule.preview"
	MethodMergeRuleChanges    MethodName = "mergerule.changes"
	MethodMergeRuleVocabulary MethodName = "mergerule.vocabulary"
	MethodMergeRuleSet        MethodName = "mergerule.set"
	MethodMergeRuleClear      MethodName = "mergerule.clear"
	// merge.events / merge.watches: the post-merge watch plane's two reads
	// (VG13/VG14) — filing triage the person reads, watches they list.
	MethodMergeEvents  MethodName = "merge.events"
	MethodMergeWatches MethodName = "merge.watches"
	// The two dod doors the person holds: the ledger list (keyed by LOOP
	// slug — the ledger is the loop's done-ness) and the human mark.
	MethodDoDList MethodName = "dod.list"
	MethodDoDMark MethodName = "dod.mark"
	// The three proposal twins (VG17's restrictive half): an agent may file
	// an ADD proposal for a gate, a TIGHTEN-only proposal for a merge rule,
	// and its own dod marks — all inert until a person commits (S35 TD3),
	// and the only agent-reachable gate writes (T9's catalog specs twin to
	// the safe reads; these three ride the records door, not the tool one).
	MethodVerifyProposeAdd        MethodName = "verify.proposeAdd"
	MethodMergeRuleProposeTighten MethodName = "mergerule.proposeTighten"
	MethodDoDMarkAsAgent          MethodName = "dod.markAsAgent"
	// The "S49 reads" block (S49 T3, AN1/AN7) — the unified automation
	// plane's TEN READS over one view of both families (S20's clock/webhook
	// schedules and S40's event triggers): what exists and what each does
	// in plain words (list/show), what happened and which loops it started
	// (fires/runs), whether it could fire right now and when next
	// (health/preview), what the vocabulary and a sample match answer
	// (vocabulary/test), a pure what-would-have-fired scan (dryrun), and
	// who changed what (changes). All ten register over the one
	// AutomationReads seam and nowhere else (nil ⇒ all ten ABSENT, V13 —
	// RegisterAutomationReads is a no-op, not a refusal), each
	// RequiresInitialize, none Debug, none on the narrow surface (AN7:
	// reads are nothing an operator cannot do — the narrow guard stays
	// pinned and this slice adds no entry).
	MethodAutomationList       MethodName = "automation.list"
	MethodAutomationShow       MethodName = "automation.show"
	MethodAutomationFires      MethodName = "automation.fires"
	MethodAutomationRuns       MethodName = "automation.runs"
	MethodAutomationHealth     MethodName = "automation.health"
	MethodAutomationVocabulary MethodName = "automation.vocabulary"
	MethodAutomationPreview    MethodName = "automation.preview"
	MethodAutomationTest       MethodName = "automation.test"
	MethodAutomationDryRun     MethodName = "automation.dryrun"
	MethodAutomationChanges    MethodName = "automation.changes"

	// S49 T4 — the write side's three NEW doors. trigger.update is the
	// clock-and-webhook family's edit door (add is its creation door,
	// pause/resume/fire-now its switches; the event family already had
	// automation.trigger.update since S40); automation.archive and
	// automation.unarchive are cross-family — the source is part of the
	// name, and archive is a state, never a delete (AN6). All three
	// register over the one AutomationWrite seam and nowhere else
	// (nil ⇒ all three ABSENT, V13 — RegisterAutomationWrites is a no-op,
	// not a refusal), each RequiresInitialize, none Debug, none on the
	// narrow surface (AU1: writes are nothing an agent reaches).
	MethodTriggerUpdate       MethodName = "trigger.update"
	MethodAutomationArchive   MethodName = "automation.archive"
	MethodAutomationUnarchive MethodName = "automation.unarchive"

	// S49 T7 (AN9) — the automation plane's ONE agent door, the socket twin
	// of the automation_propose_disable catalog tool: it files an INERT
	// proposal of kind automation_disable (T6's fourteenth kind — a
	// NARROWING only, the only shape an agent may ask for) and nothing
	// else. Registered CONDITIONALLY on the records service (V13, the
	// RegisterSeed rule the policy, autonomy, blueprint and environment
	// twins all carry: nil seam ⇒ ABSENT, never registered-and-refusing),
	// RequiresInitialize, never Debug, never narrow-projected; the author
	// is the stamped step identity (OL3), never a param, and the proposal
	// constrains nothing until a person commits it (S35 TD2).
	MethodAutomationProposeDisable MethodName = "automation.proposeDisable"

	// S27-continuous T7 (CN18): the scheduler's twelve doors. The four
	// reads answer "what will run, what ran, why not, who is needed"; the
	// eight controls are the HUMAN DOOR — the master, the pause, the
	// per-repository template (CN5), the one-time tree expansion (CN6),
	// and stopAll, the off switch that is not an uninstall. Conditionally
	// registered by RegisterContinuous (V13: a nil seam leaves all twelve
	// ABSENT — and scheduler.stopAll in particular never stands without
	// S55's store behind it); all twelve RequiresInitialize, none Debug;
	// every control stamps the actor from the connection and refuses an
	// identity key in params (CN2: the authority record only hears
	// people). Refusals ride ErrSchedulerRefused/-31935 and
	// ErrSchedulerDisabled/-31936.
	MethodSchedulerStatus      MethodName = "scheduler.status"
	MethodSchedulerPreview     MethodName = "scheduler.preview"
	MethodSchedulerDecisions   MethodName = "scheduler.decisions"
	MethodSchedulerAttention   MethodName = "scheduler.attention"
	MethodSchedulerEnable      MethodName = "scheduler.enable"
	MethodSchedulerDisable     MethodName = "scheduler.disable"
	MethodSchedulerPause       MethodName = "scheduler.pause"
	MethodSchedulerResume      MethodName = "scheduler.resume"
	MethodSchedulerRootSet     MethodName = "scheduler.root.set"
	MethodSchedulerRootDisable MethodName = "scheduler.root.disable"
	MethodSchedulerEnableTree  MethodName = "scheduler.enableTree"
	MethodSchedulerStopAll     MethodName = "scheduler.stopAll"
)

// Method is one entry in the table.
type Method struct {
	Name MethodName
	// RequiresInitialize is false for exactly one method (T7). It is a field
	// rather than a special case inside the dispatcher so that the table itself
	// says which methods are reachable before negotiation — a reader does not
	// have to find the `if`.
	RequiresInitialize bool
	// Debug marks a method that exists for diagnosis rather than for the
	// product. S19 mounts only the non-debug table on HTTP and stdio: a debug
	// method on a public surface is operational control by another name (R6).
	Debug bool
	// Handle runs the method. `params` is raw because the transport does not
	// decode it, and because every handler validates its own (SEC-T6-005).
	Handle func(ctx context.Context, sess *Session, params json.RawMessage) (any, error)
}

// Table is the protocol's method table for one daemon.
//
// It holds the server's dependencies — sessions, subscriptions, events — rather
// than reaching for package-level state, so a test can stand up a complete
// protocol server with no transport and no globals.
type Table struct {
	Sessions *Sessions
	Subs     *Subscriptions
	Events   *EventLog

	// DaemonVersion and friends are injected rather than imported from `main`,
	// which keeps this package free of the build-stamp variables and lets a
	// test assert a version the code could not have guessed.
	DaemonVersion string
	BuildTime     string
	GitCommit     string

	// OnInitialized is called after a session is recorded, so the daemon can
	// emit `client.connected` with the peer credentials only it can see (D12).
	// A hook rather than a call into the daemon, because this package must not
	// know what a peer is (T11).
	OnInitialized func(sess *Session)

	// OnSubscribed is called after a subscription is registered, with the
	// events to replay from its cursor. The daemon delivers them and emits
	// `subscription.created`; this package has no notion of sending (T11).
	OnSubscribed func(sub *Subscription, replay []Event)

	// OnEvent publishes a domain event. The daemon allocates its seq and fans
	// it out to matching subscriptions.
	OnEvent func(kind string, payload any)

	// OnStepQuestionAnswered is told, once, after a person's answer to a step's question is recorded. The daemon
	// files the loop's answers as the `answers.md` document, so a later agent finds what the person said in the
	// database instead of in a chat nobody re-reads. Nil does nothing.
	OnStepQuestionAnswered func(stepID int64)

	// ToolRegistered says whether a tool exists in this daemon (the search index and the code graph are registered
	// only when wired). Nil means no optional tool exists. Set by the daemon, which owns the tool registry.
	ToolRegistered func(name string) bool
	// Recovering names steps in the startup scan whose result is not committed.
	Recovering func(int64) bool

	// TrackStep wraps a run's context so a LATER, separate RPC call
	// (`step.stop`) can cancel it by step id. `handleStep` and the resume
	// path call this right before starting the run, and defer the returned
	// cleanup. Nil is refused rather than silently skipped — a step that
	// cannot be tracked cannot be stopped, and running it anyway would be a
	// stop button that looks present and does nothing.
	TrackStep func(parent context.Context, stepID int64) (context.Context, func())

	// CancelStep cancels a tracked step's run, reporting whether one was
	// found running. `step.stop`'s only use of daemon state.
	CancelStep func(stepID int64) bool

	// StepWasStopped reports and CLEARS whether `CancelStep` was called for
	// this id since the last read. Read once, by the run that id belongs to,
	// right after it returns — the one place that can tell "the step
	// stopped because I was asked to" apart from any other reason a context
	// ends, without sniffing the result's text for it.
	StepWasStopped func(stepID int64) bool

	// StepIsRunning is read-only, unlike the three above: `step.continue`
	// uses it as a defensive second check that a step is not ALREADY being
	// run by a call this one did not know about, before starting a second
	// one on the same id.
	StepIsRunning func(stepID int64) bool

	// StepIsStopped is the NON-consuming twin of StepWasStopped: the event
	// sink consults it on every terminal event (S31 D6 — a stopped step
	// publishes no `step.done`, whatever outcome the interrupted run
	// carried) and may be asked many times per run. Only the run itself
	// reads StepWasStopped, because only its read may clear the fact.
	StepIsStopped func(stepID int64) bool

	// ShutdownCause answers, for a run's context, whether the daemon's
	// graceful shutdown cancelled it (S55 T4, LC19). The daemon wires it to
	// its own typed cause (context.Cause == the loop's daemon-shutdown
	// sentinel) — the seam exists because this package cannot import the
	// loop package (the loop imports this one) and the cause is a daemon
	// fact, not a wire one. The commit path reads it ONCE, right after the
	// run returns, beside StepWasStopped: on a true answer a step
	// interrupted by the shutdown is left running at its last checkpoint
	// for the recovery walk (no FinishStep, no event), so a step is never
	// committed failed because the daemon went away. A person's stop still
	// wins (StepWasStopped is checked first). Nil means this daemon does
	// not drain its loops — the commit path runs exactly as before (I2).
	ShutdownCause func(ctx context.Context) bool

	// Steers is the steer-source factory (S31 T4, D3/D5): the runtime's
	// turn-boundary drain, bound to one step's durable queue, built when the
	// RunOpts are. Nil means this daemon does not wire steering — the step
	// runs exactly as it did before the feature, and `step.steer` refuses by
	// name. The daemon's adapter resolves route names BEFORE returning, so
	// the runtime only ever handles model objects (I2).
	Steers func(stepID int64) runtime.SteerSource

	// MailboxDrain and MailboxAsk are S70's factories (MB4): the peer-
	// message boundary drain and the ask_agent door, each bound to one
	// step's scope, built when the RunOpts are. Nil — either or both —
	// means this daemon does not wire the mailbox; the step runs exactly
	// as it did before S70 (I2), and the ask_agent name, if it was ever
	// offered without the door, is answered by the toolset's own honest
	// refusal like any tool that is not there.
	MailboxDrain func(stepID int64) runtime.MailboxDrain
	MailboxAsk   func(stepID int64) runtime.MailboxAskDoor

	// TodoPolicy is S75's factory (TD9/TD10): the nudge and the completion
	// guard bound to one step and its lane. Nil leaves the step as it was
	// before the feature (I2).
	TodoPolicy func(stepID int64, lane string) runtime.TodoPolicy

	// Telemetry is the S33 capture seam (TL12/T2): the sink every RunOpts
	// the table builds carries into RunStep/ResumeStep, so the socket's
	// runs land their per-turn usage, compactions and tool activity in the
	// store's rollup chain. Nil means this daemon does not wire telemetry —
	// the step runs exactly as it did before the feature (I2), and the
	// telemetry.* reads answer the zero states, never a wrong number. The
	// concrete adapter lives in internal/daemon/telemetry_seam.go (the
	// runtime never learns what a rollup row is).
	Telemetry runtime.TelemetrySink
	// TelemetryReads is S33 T8's store seam for the four telemetry reads
	// (telemetry.go). Named apart from Telemetry above on purpose: that one
	// WRITES from inside the runtime's seams, this one READS for callers —
	// the slice's one rule in two fields. Nil (storeless daemon) answers
	// ErrStoreUnavailable; the methods stay registered the way step.*'s
	// store-backed reads do.
	TelemetryReads TelemetryStore

	// Settings is the S44 T5 seam: the shared *config.Store the five settings.*
	// methods read and write through (settings.go). It is a plain interface,
	// not a *config.Store, for the same reason as TelemetryReads above — the
	// wire types (SettingView/SettingEventView) are THIS package's, field-for-
	// field twins of config's View/Event, and the daemon's adapter is the only
	// layer that may know both. Nil (a storeless daemon, V13) leaves the five
	// methods ABSENT: RegisterSettings is a no-op, so the no-settings build
	// never answers a settings door at all, rather than answering a refusal.
	Settings SettingsService

	// Seed is the S43-extra T6 seam: the ONE *seed.Engine the four seed.*
	// methods read and write through (seed.go). *seed.Engine satisfies
	// SeedService field-for-field (the seam returns the engine's own
	// SetStatus/SetPlan/ApplyResult), so the daemon's adapter is a nil guard,
	// not a translation (the settingsService convention). The wire types
	// (SeedSetStatus et al.) are THIS package's field-for-field twins. Nil (a
	// storeless daemon, V13) leaves the four methods ABSENT: RegisterSeed is
	// a no-op, so a seedless build never answers a seed door at all, rather
	// than answering a refusal — mirroring Search and Settings above.
	Seed SeedService

	// S27-continuous T7: the continuous scheduler service (CN18). The
	// daemon wires it exactly over its store (V13); RegisterContinuous
	// turns it into the twelve doors, and a daemon without it has no
	// scheduler surface at all — absent, not refusing.
	Continuous ContinuousAPI

	// Autonomy is the S56 T6 seam: the service over the autonomy store
	// tables and the pure internal/autonomy core that the ten autonomy.*
	// methods read and write through (autonomy.go). The daemon's adapter is
	// the only layer that may know both (the SettingsService convention:
	// the wire types are THIS package's, the handler maps). Nil (a storeless
	// daemon, V13) leaves the ten methods ABSENT: RegisterAutonomy is a
	// no-op, so the no-store build never answers an autonomy door at all,
	// rather than answering a refusal — mirroring Settings and Seed above.
	Autonomy AutonomyService
	// LoopControl is the S55 T5 seam: the seven loop-control doors (stop,
	// resume, cancel, control, requestStop, control.requests,
	// control.dismiss) over the T3 control core and the T2 store. The daemon
	// implements it (loop_control_service.go) over its own hooks, the
	// request desk and the publish, and the actors it stamps come from the
	// daemon's ActorFor seam — the kernel's peer credential, never a param
	// (LC5). Nil (a storeless daemon, V13) leaves the seven methods ABSENT:
	// RegisterLoopControl is a no-op, mirroring Settings above — a
	// control-less build never answers a control door at all, rather than
	// answering a refusal.
	LoopControl LoopControlService

	// PolicySubject maps a step's handler name to the name policy judges it under (the handler's policy
	// agent when it declares one). Nil means the name itself. The daemon wires it so a rule an answer
	// writes names the same subject the gate asks about.
	PolicySubject func(handlerName string) string

	// LoopAccess is the loop permission-mode seam (loop_access.go). Nil leaves loop.access and
	// loop.setAccess absent.
	LoopAccess LoopAccessService

	// Budget is the S55 T7 in-step budget seam (E2): the check every
	// RunOpts the table builds carries into RunStep/ResumeStep, bound to
	// the STEP's loop by the table (budgetCheck) — the daemon resolves the
	// loop's applicable budgets against current spend, evaluates them with
	// the pure engine (internal/budget), and answers in plain runtime data
	// (BudgetVerdict/CapHit — the runtime imports neither side). The socket
	// door for raising a cap (loop.raise) is T8's; this seam only enforces.
	// Nil (a storeless daemon, or T7 unwired) means unenforced — the pre-T7
	// path, byte for byte (I2).
	Budget func(ctx context.Context, loopID string) (runtime.BudgetVerdict, error)

	// BudgetStore is the S55 T8 seam: the budget plane's EIGHT DOORS
	// (budget.list/show/effective/set/remove/vocabulary, spend.report and
	// loop.raise) over the T6 store and the T7 raise machinery. It is a
	// different field from the in-step `Budget` check above, and the names
	// must stay apart: the check enforces, the doors manage. The wire
	// types (BudgetView et al.) are THIS package's, field-for-field twins
	// of what the daemon assembles from store rows — the daemon's adapter
	// is the only layer that may know both. Nil (a storeless daemon, V13)
	// leaves the eight methods ABSENT: RegisterBudget is a no-op, so a
	// budget-less build never answers a budget door at all, rather than
	// answering a refusal — mirroring LoopControl above.
	BudgetStore BudgetStore

	// Inbox is the S58 T2 seam (SV1): the attention registry evaluated over
	// the live store, behind the daemon's own service. Nil leaves both
	// methods ABSENT (RegisterInbox is a no-op, V13) — a build without the
	// supervision plane answers a method-not-found, not a refusal.
	Inbox InboxService

	// Mission is the S58 T6 seam (SV11): mission.status over the store,
	// the injected inbox summary, and the S55/S27 registrars' seams. Nil
	// leaves the method ABSENT (RegisterMission is a no-op, V13).
	Mission MissionService

	// AutomationReads is the S49 T3 seam (AN1): the unified automation
	// plane's ten reads over BOTH families' rows — one view of the
	// schedules, webhooks and event triggers, their fire log, runs, audit,
	// health and previews. Nil leaves all ten ABSENT
	// (RegisterAutomationReads is a no-op, V13): a storeless daemon has no
	// automations to describe, and registered-and-refusing is the pattern
	// this house retired.
	AutomationReads AutomationReads
	// InboxWrite is the S58 T3 seam (SV3, SV12): the three visibility
	// writes over the append-only marker table. Nil leaves inbox.ack,
	// inbox.snooze and inbox.unack ABSENT (RegisterInboxWrites is a no-op,
	// V13). Separate from Inbox because the reads evaluate and write
	// nothing while these write and attribute — one seam per verb class.
	InboxWrite InboxWriteService
	// NotifyChannel is the S58 T5 seam over webhook channels (V13: the
	// ten channel-plane methods register iff it is wired). The seam owns
	// the store, the secret store and the dispatcher.
	NotifyChannel NotifyChannelService
	// NotifyClass is the S58 T4 seam over the event-class table (V13:
	// nil means the three notify.class/vocabulary doors are ABSENT).
	NotifyClass NotifyClassService

	// Watchdog is the S55 T9 seam: the watchdog plane's SIX DOORS
	// (watchdog.list/show/vocabulary and the human writes set/setEnabled/
	// remove) over the T9 store — the same one daemon service (the watchdog
	// ticker's owner, whose once-per-episode memory the list's
	// loopsInEpisode reads) implements both. A loosening (a disable, a
	// remove) confirms against the rendered canonical statement (the T8
	// PM7.1 pattern); the actor is stamped from the connection, never a
	// param. Nil (a storeless daemon, V13) leaves the six methods ABSENT:
	// RegisterWatchdog is a no-op, so a watchdog-less build never answers a
	// watchdog door at all, rather than answering a refusal — mirroring
	// BudgetStore above.
	Watchdog WatchdogStore

	// Backup is the S62 T6 seam: the five backup doors (run/list/verify/
	// status/prune) over the daemon's one BackupService, which implements
	// BackupStore. The wire types (BackupView et al.) are THIS package's,
	// field-for-field twins of the backup package's views with absolute
	// paths stripped — the daemon's adapter is the only layer that may know
	// both (BK12). Nil (a storeless daemon, V13) leaves the five methods
	// ABSENT: RegisterBackup is a no-op. Restore is deliberately NOT a seam
	// method (BK9): it is the CLI's direct door, and no socket method for it
	// exists or will.
	Backup BackupStore

	// Export is the S62 T8 seam: the two export doors (run/sets) over the
	// daemon's one ExportService, which implements ExportStore. The engine
	// (internal/export) is a pure leaf with its own store-side seam on
	// *store.Store, and the daemon's service is the only layer that may
	// know both the engine's types and this package's wire twins — the same
	// direction as the backup family above. Nil (a storeless daemon, V13)
	// leaves both methods ABSENT: RegisterExport is a no-op.
	Export ExportStore

	// Verify is the S57 T8 seam: the twelve verify doors (the six reads
	// list/show/runs/runShow/vocabulary/templates and the six human-door
	// writes add/update/archive/restore/applyTemplate/try) over the
	// daemon's one verify service, which implements VerifyStore. Nil
	// (a storeless daemon, V13) leaves the twelve methods ABSENT:
	// RegisterVerify is a no-op. The writes stamp the actor from the
	// connection, never from params (VG18's actor rule).
	Verify VerifyStore

	// MergeRule is the S57 T8 seam: the merge plane — the rule's six
	// doors (get/preview/changes/vocabulary/set/clear) plus the two
	// merge reads (merge.events, merge.watches) that ride the same
	// seam, over the daemon's one merge-rule service. Nil leaves all
	// eight ABSENT: RegisterMergeRule is a no-op. The two writes stamp
	// the actor from the connection, never from params.
	MergeRule MergeRuleStore

	// DoD is the S57 T8 seam: the two DoD doors (list/mark) over the
	// daemon's one DoD service. Nil leaves the two ABSENT:
	// RegisterDoD is a no-op. mark stamps the actor from the
	// connection, never from params.
	DoD DoDStore

	// Environment is the S59 T6 seam: the thirteen environment doors (the
	// six reads list/show/effective/vocabulary/changes/provisions and the
	// seven human-door writes create/update/archive/restore/bind/unbind/
	// teardown) over the daemon's one environment service, which implements
	// EnvironmentService. Nil (a storeless daemon, V13) leaves all thirteen
	// ABSENT: RegisterEnvironment is a no-op. The writes stamp the actor from
	// the connection, never from params (EV17's actor rule), and no view in
	// the plane has a field that could hold a secret value (EV7).
	Environment EnvironmentService

	// SecretRefs is the S59 T6 seam: the seven secret-reference doors (the
	// three reads list/show/check and the four human-door writes
	// create/update/archive/restore) over the daemon's one secret-reference
	// service. Nil leaves all seven ABSENT: RegisterSecretRef is a no-op.
	// `check` resolves and answers booleans only — a value neither enters nor
	// leaves this plane (§4c, EV7).
	SecretRefs SecretRefService

	// MergeQueue is the S60 T8 seam: the ten merge-queue doors (the four
	// reads list/show/status/vocabulary and the six human-door writes
	// enqueue/dequeue/reorder/retry/land/settings) over the daemon's one
	// queue door service, which implements MergeQueueService. Nil (a
	// storeless daemon, V13) leaves all ten ABSENT: RegisterMergeQueue is a
	// no-op. The writes stamp the actor from the connection, never from
	// params, and no param may name a state (MQ13).
	MergeQueue MergeQueueService

	// Scorecard is the S63 T5 seam: the five scorecard reads over the
	// daemon's read side of internal/scorecard. Nil (a storeless daemon,
	// V13) leaves all five ABSENT. Every result carries RT3's statement;
	// the plane is reads, always (RT3.3).
	Scorecard ScorecardService

	// Retro is the S63 T5 seam: the three outcome-fact reads plus the
	// one human-door write, retro.backfill (RT13) — dry-run default,
	// `yes:true` to write, the actor the connection's person, and the
	// name absent from every non-person surface (P4).
	Retro RetroService

	// Digest is the S63 T5 seam: digest.learned, the fixed-order RT12
	// content spec S58 will one day render.
	Digest DigestService

	// Charter is the S61 T7 seam: the four charter reads plus the one
	// human-door save (GI8: the charter grants nothing here — the S56
	// hooks ride the body verbatim and this plane runs none of them).
	Charter CharterService

	// Templates is the S61 T7 seam: the two scaffold-template reads plus
	// the one human-door save (GI14: the template set is the person's
	// palette; an edit never moves origin).
	Templates TemplateService

	// Intake is the S61 T7 seam: the six goal-intake doors — start, the
	// reads, the wizard-shaped commit (GI9), the vocabulary. Every write
	// is a person's door and the actor is the connection's (P4).
	Intake IntakeService

	// StepSteerState reads one step's durable row for the steer door
	// (LW4): status and pause_reason decide whether an instruction may be
	// queued AT ALL and which method to name instead — `answer` behind a
	// real question, `step.continue {message}` behind a person-stop. agent,
	// componentRoot and loopSlug decorate `step.steer.queued` like every step
	// event (D1: an event with no root matches every project). found=false
	// is the step that never was, and the refusal names its id.
	StepSteerState func(stepID int64) (status, pauseReason, agent, componentRoot, loopSlug string, found bool)

	// ComponentRootKnown answers whether a path is a REGISTERED project root
	// (S31 T6): the socket's workspace-facing doors — the read-only git
	// methods here, and the PTY terminals of T7 — open only on roots the
	// person registered through `project.create`. Nil means no registry is
	// wired, and every door behind it refuses by name rather than reading,
	// or later exec'ing, in a directory nobody claimed (P26's boundary
	// argument does not extend to paths the daemon has never seen).
	ComponentRootKnown func(componentRoot string) bool

	// GitStatus and GitDiff are the socket's read-only git doors (S31 T6,
	// D7): branch/upstream/ahead-behind/changed paths, and one path's unified
	// diff. The daemon implements them over `internal/workspace` — this
	// package must not exec (import_test names `os/exec`), so the bounded
	// process layer (LW11) lives there. The three states travel as DATA
	// (`ok` | `not-a-repo` | `unavailable` + reason), which is why neither
	// seam answers "could not tell" with an error: the answer itself is the
	// three-state object. GitDiff's error is reserved for the RESOLVER
	// refusal (a path escaping the root) — bad params, named, not a state.
	// Nil seams: the methods refuse by name rather than guess a repository.
	GitStatus func(ctx context.Context, componentRoot string) GitStatusView
	GitDiff   func(ctx context.Context, componentRoot, path string) (GitDiffView, error)
	// GitChanges is `workspace.git.changes`: the same three-state discipline, with per-file line counts.
	GitChanges func(ctx context.Context, componentRoot string) GitChangesView
	// SetLoopOptions is `loop.options`: the person's thinking and effort choice for a loop's later steps.
	SetLoopOptions func(ctx context.Context, p LoopOptionsParams) (LoopOptionsResult, error)
	// SetLoopArchived, DeleteLoop and ListLoopTombstones are loop_lifecycle.go's seams: hide a loop, delete one keeping
	// its totals and its work, and list what deletions left.
	// ListLoopDocuments is `loop.documents`: the documents a loop's agents filed in the database.
	ListLoopDocuments func(ctx context.Context, p LoopDocumentsParams) (LoopDocumentsResult, error)
	SetLoopArchived    func(ctx context.Context, p LoopSetArchivedParams) (LoopSetArchivedResult, error)
	DeleteLoop         func(ctx context.Context, actor Actor, p LoopDeleteParams) (LoopDeleteResult, error)
	ListLoopTombstones func(ctx context.Context, p LoopTombstonesParams) (LoopTombstonesResult, error)

	// Terminals is the daemon-owned PTY registry (S31 T7), the person's own
	// shell under their own uid on the P26 socket — which is why it is NOT
	// policy-governed (LW8: policy governs what AGENTS do) and why it may
	// never reach the narrow HTTP surface (I5). Output and exit are
	// connections' notifications, never events (LW7): the manager notifies
	// attached connections directly, and this table never sees a byte of
	// terminal output.
	Terminals TerminalManager

	// Routes and Models are the model layer, injected. Nil until a daemon
	// wires them, and `ask` says so rather than panicking.
	Routes Routes
	Models Streamer

	// Search is the retrieval resolver (S22 T6), typed in internal/search.
	// A function field, not an
	// import of the retrieval package — this package may not contain the
	// hunted (the module walk at internal/retrieval/index_test.go is the
	// guard); the entry point builds the closure and the daemon injects it.
	// Nil means unregistered: the no-retrieval build never sets it (V13).
	Search search.SearchResolver

	// Graph is the code-graph service (S30 T5). The daemon injects the
	// ONE *internal/graph.Service it built over its store — the same
	// value the `graph` tool holds (identity asserted in internal/daemon).
	// Nil keeps the whole graph method family out of the table (V13).
	Graph Graph

	// AsyncWorkers is the background-work manager (S64 T3). The daemon
	// injects the ONE *internal/asyncworkers.Manager it built over its
	// store; the graph.build handler calls its StartView (the owning
	// service starts, the ledger records), and the asyncworkers family
	// answers reads and cancel from it. Nil keeps the family out of the
	// table AND makes graph.build refuse (V13 — a method that can only
	// refuse is a door onto nothing).
	AsyncWorkers AsyncWorkers

	// Skills is the Agent Skills catalog (S65 T3). The daemon injects the
	// ONE *internal/skills.Service it built over the two roots (the
	// operator's ~/.agents/skills and Aeon's managed folder); every door —
	// socket, CLI client, bench — reads that one scan. Nil keeps the whole
	// family out of the table (V13).
	Skills Skills

	// RetrievalStatus and RetrievalReindex are the T8 status/reindex
	// seams (S22 T8, D15/P34), typed in internal/search for the same
	// module-walk reason as Search above. They are the SAME function
	// values the `aeon index` verbs call — the daemon injects wrappers
	// that merge the async tracker's figures, and the CLI calls the
	// underlying functions directly; there is one implementation of each
	// (TestRetrievalStatusAndReindexSocketCallSameFunctions is the
	// call-site lock). Nil means absent: the no-retrieval build never
	// sets them (V13) and their methods are never registered.
	RetrievalStatus  search.StatusFunc
	RetrievalReindex search.ReindexFunc

	// PlatformStatus, RetrievalPlan and PlatformChanges are the S54 T2
	// read seams (SP1/SP2/SP6): the composed panel (platformview over the
	// daemon's own reads), the priced rebuild preview (the estimate walk,
	// never the embedder), and the T1 audit listing. Each nil keeps its
	// method out of the table (V13): the no-retrieval build never sets the
	// plan seam, a storeless daemon never sets the changes seam, and a
	// daemon with no subsystems at all builds no composer.
	PlatformStatus  PlatformStatusFunc
	RetrievalPlan   RetrievalPlanFunc
	PlatformChanges PlatformChangesFunc

	// Probes is the daemon's cached probe verdicts (S18 T3), injected so the
	// socket can store what a probe earned and render what it found. Nil on
	// a storeless daemon: with no cache there are no earned verdicts, so
	// routes.list carries no states and `ask` has no contradiction to
	// consult — which is the honest zero, not a forgotten wire. The
	// in-flight probe lock lives on the Table beside it (probeFlights),
	// memory-only by design: it guards concurrent requests, not facts.
	Probes ProbeStore

	probeMu      sync.Mutex
	probeFlights map[string]*probeFlight

	// continuationWG tracks the fire-and-forget answer/continue goroutines
	// (continueAnsweredStep, spawned by handleAnswer and step.continue). A
	// continuation keeps writing the transcript after the request returns, so
	// a caller that closes the store while one is in flight closes it out from
	// under the goroutine: the store's *sql.DB is nil after Close, and the
	// next ReadTranscript/BeginTx dereferences it — a panic that lands
	// wherever the goroutine next touches the DB. WaitContinuations is the
	// drain: a caller MUST call it before closing the store.
	continuationWG sync.WaitGroup

	// Memory is the store (S4), injected as an interface so this package
	// imports nothing storage-shaped (T11/I1). Nil until a daemon wires one,
	// and every store method answers ErrStoreUnavailable rather than panicking.
	Memory     Memory
	Components ComponentStore
	// TriggerReads is S40 T7's read seam (AU14) — the trigger catalogue and
	// the fire log.
	TriggerReads EventTriggerReader
	// TriggerWrites is S40 T8's write seam — human-only (AU1), refused
	// before anything is judged.
	TriggerWrites EventTriggerWriter
	// ActorFor stamps who-wrote from the CONNECTION (the daemon wires it
	// from the peer credential, as the records seam does). Nil means no
	// identity is available, which is a refusal, never a default.
	ActorFor func(ctx context.Context, sess *Session) Actor

	// Records is S38 T15's projection seam (OL10): the catalog's socket
	// twin and the three human-only decisions. Nil until the daemon wires
	// one — every method then answers "records are unavailable in this
	// runtime", the third state, rather than answering zero.
	Records RecordsAPI

	// Runner and Steps are S5's runtime and the narrow store surface the
	// `step` handler writes through (K22). Both nil until a daemon wires them,
	// and `step` says so rather than panicking.
	Runner         StepRunner
	Steps          StepStore
	Questions      QuestionStore
	SteerStore     SteerStore
	HandlerForStep func(name, prompt string) (runtime.StepHandler, bool)

	// PolicyGate is the S10 policy gate, threaded to every RunOpts the table
	// builds (handleStep, continueAnsweredStep). Nil means no enforcement —
	// the mode every pre-S10 test uses; the daemon wires a production adapter
	// whenever a store exists (daemon.New), and the gate is the runtime's own
	// interface, so this package gains no policy or storage dependency (K22).
	PolicyGate runtime.PolicyGate

	// PolicyStore is the S10 READ window (T13): the same boundary the gate
	// reads, handed to the four read-only policy methods. The daemon wires it
	// at the same table-building site as the gate pair (daemon.New, whenever
	// a store exists), over the same RepoFuncs bridge — reads of one store,
	// one gate, so the app can never be shown a set the enforcement is not
	// using. Nil on pre-S10 and storeless tables, and every handler answers
	// ErrStoreUnavailable rather than panicking.
	//
	// It is the boundary TYPE rather than a hand-rolled interface because
	// this IS the boundary: the read methods must not be able to drift from
	// the fail-closed decode discipline that List/InForceFor/ReportEvents
	// carry (a hand-written interface here would be a second read path, and
	// second read paths drop rows quietly). internal/policy imports nothing
	// transport- or storage-shaped, so importing it here costs neither the
	// T11 invariant nor the store-blindness guard.
	PolicyStore *policy.Store

	// PolicyChanges is the S45 T3 seam (PM8): the ONE read door onto the
	// append-only `policy_change` authoring audit (`policy.changes`,
	// policy_changes.go). The proposalStore pattern (S35 T3): a package-
	// local interface, the wire view (PolicyChangeView) is THIS package's,
	// the row is the store's, and the daemon's adapter — reads only — is
	// the one layer that knows both. The seam has no write method BY
	// SHAPE: the audit is append-only by trigger (PM8) and a seam with a
	// writer would be that trigger bypassed in Go. Nil (storeless daemon)
	// answers ErrStoreUnavailable — the method stays registered the way
	// every policy read does, and the handler never dereferences.
	PolicyChanges PolicyChangeStore

	// PolicyAuthoring is the S45 T4 seam (PM1): the daemon's authoring
	// service behind the three human-door methods `policy.add`,
	// `policy.remove`, `policy.seedCorpus` (policy_authoring.go). The
	// PolicyStore pattern (S35 T3's ProposalStore): a package-local
	// interface, the wire shapes are THIS package's, the boundary calls
	// are the daemon's, and the seam's first argument — the actor — is
	// stamped by this package from the connection (Table.ActorFor), never
	// from params. Registration is CONDITIONAL (RegisterPolicyAuthoring,
	// the RegisterSeed/V13 rule): nil seam ⇒ the three methods are
	// ABSENT, not registered-and-refusing. The daemon wires it whenever a
	// store exists, over the SAME RepoFuncs bridge the gate and the reads
	// use — one implementation, two doors (PM2/PM4), and every write
	// presents to THE boundary exactly once with its audit row beside it
	// (PM8).
	PolicyAuthoring PolicyAuthoring

	// HistoryStore is the S25 T4 seam to internal/history, the read-only
	// facade the three history.* methods read through (mirroring policy.Store).
	// It wraps the SAME *store.Store the daemon opens, so the app's history
	// read and the CLI's (aeon history, T3) read the same bytes — H3 is
	// structural, not a test. It is a *history.Store, not a *store.Store, so the
	// protocol never imports store (the four-package boundary — S19 mounts the
	// protocol on HTTP). Nil is the storeless shape; every handler says so with
	// ErrStoreUnavailable rather than dereferencing (memory.go's requireMemory).
	HistoryStore *history.Store

	// Agents is the S14 T10 seam to internal/agentsvc (transport.go): the
	// seven agent.* methods forward here. It is a PLAIN func type, not an
	// agentsvc type, because AS11's deletability is the whole argument:
	// `rm -r internal/agentsvc` must leave this package building, and only
	// cmd/aeon may import agentsvc — the daemon wires the seam from the
	// entry point exactly as it wires AgentInvoke for lane skill steps.
	//
	// The contract (pinned on both sides): `method` is one of the seven
	// registered names; the result is the JSON-ready value the fixture
	// pins; refusal sentences (the door already named the fault — this
	// package adds no interpretation) come back in `refusal` and travel
	// verbatim in ErrAgentRefused's `what`; `err` is infrastructure and is
	// never a refusal. The three lifecycle methods (invoke, task.complete,
	// task.fail) must return a result carrying an integer `taskId` —
	// agent.go emits `task.created`/`task.completed`/`task.failed` from it
	// and refuses LOUDLY rather than skipping the event on a wiring defect.
	Agents func(ctx context.Context, method string, params json.RawMessage) (result any, refusal string, err error)

	// Plugins is S15's ONE read seam: `agent.plugins` forwards here, and it
	// is answered by the ENTRY POINT, not by the agent-services layer — the
	// plugin registry is cmd's to import (the absent-layer sweep's allowed
	// set), and a plain func keeps this package building with the directory
	// deleted, the same discipline as Agents above. Read-only by
	// registration; the four state verbs have no socket door at all (R6).
	Plugins func(ctx context.Context, params json.RawMessage) (result any, refusal string, err error)

	// AgentRoster is S34 T1's seam: `agent.list` forwards here. A separate
	// seam from Plugins on purpose (TB1) — this answers identity for EVERY
	// agent, first-party bodies included, in a shape `agent.plugins`
	// deliberately does not carry (that method's own doc comment explains
	// why it stays the import ledger's). Entry-point-owned, same deletability
	// discipline as Plugins above.
	AgentRoster func(ctx context.Context, params json.RawMessage) (result any, refusal string, err error)

	// ToolGrants is S34 T2's seam: `tools.grants` forwards here — the S12
	// authority view (internal/mcp.GrantView) for one named agent, verbatim.
	// Separate from AgentRoster (TB1: a different fact), same deletability
	// discipline.
	ToolGrants func(ctx context.Context, params json.RawMessage) (result any, refusal string, err error)

	// ToolsCatalog is S47 T5's seam: the four catalog reads (`tools.list`,
	// `tools.get`, `tools.byAgent`, `tools.status`) forward here, one method
	// per door on the interface rather than one dispatched func — these four
	// answer four different questions and the seam is a daemon adapter over
	// the registry, the seeded rows, the policy set and the MCP view, not a
	// closure over a package this one may not import. Nil means the daemon
	// started without the catalog (V13): RegisterToolsCatalog returns false and
	// the four names are ABSENT, which a client tells apart from a refusal
	// because an absent door answers JSON-RPC's own -32601.
	//
	// It is NOT `ToolGrants` and not `MCPAdmin`: `tools.grants` is the raw S12
	// authority view for one agent (unchanged since S34, and pinned so),
	// `MCPAdmin` is the MCP management view (this seam CALLS it for the MCP
	// half, through the daemon's copy of the same func value — never
	// `internal/mcp` directly, MC11), and what this seam adds is the JOIN: code
	// (the registry, the source of the schema and the executor, TC2) with rows
	// (the seeded catalog entry, TC10) with policy (the "to whom", TC5) — a
	// join no one client may rebuild, because a client that rebuilt it would
	// own a second answer to "can this agent call this tool".
	ToolsCatalog ToolsCatalog

	// LoopTodos is S75 T3's seam (loop_todos.go): the loop's persisted todo
	// list. Nil leaves `loop.todos` and `loop.todos.close` ABSENT (V13).
	LoopTodos LoopTodos

	// Variables is the variable manager's seam (variables.go). Nil leaves the four
	// `vault.*` doors ABSENT (V13).
	Vault VaultSeam

	// LoopQuestions is S76's seam (loop_questions.go). Nil leaves `loop.questions`
	// ABSENT (V13).
	LoopQuestions LoopQuestions

	// NarrowMethods is the S47 T6 seam (TC8): the narrow projection's own answer
	// to "which of your actions projects this socket method, and under what
	// action name". It is a func value and not an import because internal/narrow
	// imports THIS package — the direction is narrow→protocol, never back — so
	// the daemon or the entry point supplies it where the table is built.
	//
	// nil is a value with meaning: the surface then reports `narrow:"unknown"`
	// for every method, which is not the same answer as `narrow:"none"` and is
	// never silently rendered as one. A daemon that never wired the projection
	// has not earned the right to say a method is absent from it.
	NarrowMethods func() map[MethodName]string

	// MCP is S34 T3/T4's seam: mcp.status and mcp.check both forward here,
	// dispatched by method name — the same one-func shape as Agents, never
	// a second seam per method, because this package must not import
	// internal/mcp at all (internal/mcp/absent_test.go's banned list names
	// this package explicitly).
	MCP func(ctx context.Context, method string, params json.RawMessage) (result any, refusal string, err error)

	// MCPAdmin is S46 T3's seam: the five PERSON's MCP management reads
	// (mcp.servers / mcp.server / mcp.grants / mcp.vocabulary /
	// mcp.changes) all forward here, dispatched by method name — the same
	// one-seam-many-methods shape MCP above already uses (the trust reads
	// stay on MCP; the management reads never join it: a different door,
	// MG8). Entry-point-owned: the seam reads rows, joins the probe cache,
	// and owns every sentence (near-names refusals, vocabulary, the
	// audit's statement) — cmd/aeon wires it as a closure over the store
	// and the assembled config, because this package must import neither
	// internal/mcp nor the assembly (MC11's deletability, same discipline
	// as MCP). Registered CONDITIONALLY by RegisterMCPAdmin — a nil seam
	// leaves the five ABSENT, not registered-and-refusing (V13).
	//
	// The contract (pinned on both sides): `method` is one of the five
	// registered names (bound at registration, never trusted from
	// params); `params` is the caller's raw JSON — this package has
	// already refused an identity key and checked the shape, and the seam
	// owns what the VALUES mean (a nonsense name is the seam's nonsense);
	// a typed *Error returned in `err` travels UNTOUCHED (ErrMcpUnknown's
	// near-names sentence is the seam's — only the seam knows the names);
	// a non-empty `refusal` is an mcp-domain refusal carried VERBATIM in
	// ErrMcpRefused's `what` (this package adds no interpretation); a
	// plain `err` is infrastructure, never a refusal (DB19). Reads never
	// probe, never cache, never print an env value (MG11/MG12/MG13).
	MCPAdmin func(ctx context.Context, method string, params json.RawMessage) (result any, refusal string, err error)

	// AgentTransition is S35 T4's seam: internal/plugin is banned from this
	// package (absent_test.go's own stated invariant — the import is
	// allowed in exactly {cmd/aeon, internal/plugin}), so
	// agent.enable/disable/revoke forward here, dispatched by method name,
	// the same one-seam-many-methods shape MCP already uses.
	AgentTransition func(ctx context.Context, method string, params json.RawMessage) (result any, refusal string, err error)

	// Proposals is S35 T3's store seam: internal/store is not banned from
	// this package (unlike internal/mcp), so proposal.create/list/discard
	// reach T1's store functions through this local interface — the same
	// StepStore/QuestionStore pattern, adapted in internal/daemon, not a
	// func seam.
	Proposals ProposalStore

	// ProposalDecider is S45 T5's seam onto the daemon's decision service
	// (the PolicyAuthoring pattern, V13): nil seam ⇒ the three decision
	// methods are ABSENT, not registered-and-refusing. The daemon wires it
	// whenever a store exists; the desk registry behind it is built from
	// cmd/aeon's one injected seam map (desk.EffectsFor), so the socket's
	// commit and the CLI's commit share one implementation (PM4).
	ProposalDecider ProposalDecider

	// ValidateProposal is T2's seam: validation logic lives in cmd/aeon (it
	// calls cmd/aeon/policy.go's functions and internal/mcp's
	// Config.Validate, both off-limits here), so proposal.create reaches it
	// through this func, wired in serve.go. Half-wired is visible, never
	// silently permissive (the same discipline S34's seams state): nil
	// refuses every create rather than skipping straight to the write — a
	// daemon that forgot to wire this must not accept an unvalidated
	// payload as a proposal.
	ValidateProposal func(ctx context.Context, kind, payload string) error

	// AgentImportPreview is S37's ONE seam for proposal.create{kind:"agent_import"}:
	// the entry point builds a closure over internal/plugin (cmd/aeon is the only
	// package allowed to import it — TestNoUnconditionalImportOfPlugin), the
	// protocol layer holds a plain func. nil means half-wired: the create
	// answers ErrNotWired by name, never silently permissive.
	AgentImportPreview func(ctx context.Context, pkgPath string) (preview *AgentImportPreview, refusal string, err error)

	// BlueprintPreview is S48 T9's seam for proposal.get{kind:
	// "blueprint_save"}: the daemon's read service computes the diff of
	// the proposed bytes against the row, the validation report summary,
	// and the advisory lint — READ-ONLY, over the payload alone (it never
	// writes, and it gates nothing: the commit is decided by the validators
	// and the person, never by this preview — AA4). nil means half-wired:
	// the preview field is simply absent, the row still reads.
	BlueprintPreview func(ctx context.Context, payload []byte) (preview *BlueprintProposalPreview, refusal string, err error)

	// AutomationPreview is S49 T6's seam for proposal.get{kind:
	// "automation_disable"}: the daemon's read service computes the
	// automation's AN5 Describe sentence, its last five fires and its
	// health verdict — READ-ONLY, over the payload alone (it never writes,
	// and it gates nothing: the commit is decided by the validators and the
	// person, never by this preview — S37 AA4, carried). nil means
	// half-wired: the preview field is simply absent, the row still reads.
	AutomationPreview func(ctx context.Context, payload []byte) (preview *AutomationProposalPreview, refusal string, err error)

	// AgentManage is S43's ONE seam for the agent-management methods
	// (agent.create/import/export/duplicate/delete/edit/view/migrate): the
	// entry point builds a closure over internal/agentmgr (cmd/aeon is the
	// only package that may import it, like AgentImportPreview imports
	// internal/plugin), the protocol layer holds a plain func. Nil is
	// half-wired: the methods answer ErrNotWired by name, never silently
	// permissive (P34 — the daemon is the primary surface; the CLI and the
	// app are secondary clients, and a capability that exists only as a CLI
	// verb is not accepted as finished). Dispatched by method name, the
	// one-seam-many-methods shape Agents/AgentTransition already use.
	AgentManage func(ctx context.Context, method string, params json.RawMessage) (result any, refusal string, err error)

	// PolicySet is the S10 replay-honesty counterpart (T10, D12): the
	// constructor the table threads into the RESUME RunOpts so a resumed
	// step is decided against the policy set its manifest froze, not
	// against today's store. It receives the frozen pair (runtime.
	// FrozenManifest) — the set and the digest recorded beside it — because
	// a constructor handed only the set can only trust it (DEF-039). Nil on
	// pre-S10 tables (their manifests froze nothing anyway); the daemon
	// wires both halves or none — a table with PolicyGate and no PolicySet
	// replays against the live store, which is exactly the drift the pair
	// exists to prevent.
	PolicySet          func(frozen runtime.FrozenManifest) runtime.PolicyGate
	StartBlueprintLoop func(context.Context, LoopStartParams) (any, error)

	// S24 T7: the queue's daemon-side seams, filled in initQueue
	// (daemon/queue_handlers.go). Queue is the one internal/queue instance
	// both transports speak through (CG9); QueueAdvance is the daemon's
	// materialization door (the same advanceNext the reactor chains — one
	// materialization, now with a third, explicit caller); QueueValidate-
	// Blueprint is loop.start's OWN loader handed to queue.append so the
	// two doors cannot drift on what counts as a valid blueprint name.
	Queue                  *queue.Queue
	QueueAdvance           func(ctx context.Context, componentRoot string) (started bool, err error)
	QueueValidateBlueprint func(blueprintName string) error
	// S26 T5: the roadmap's daemon-side seams, filled in initRoadmap
	// (daemon/roadmap_dispatch.go). Roadmap is the one internal/roadmap
	// instance both transports speak through (CG9's extension, brief §5's
	// D5); the three func seams are the daemon's dispatch doors (T3/T4) —
	// ideation, planning, and R4's queue-or-direct materialization. A nil
	// seam answers ErrStoreUnavailable, loud, never a silent no-op.
	Roadmap            *roadmap.Roadmap
	RoadmapIdeate      func(ctx context.Context, actor roadmap.Actor, p RoadmapIdeateParams) (any, error)
	RoadmapPlan        func(ctx context.Context, actor roadmap.Actor, p RoadmapPlanParams) (any, error)
	RoadmapMaterialize func(ctx context.Context, actor roadmap.Actor, p RoadmapMaterializeParams) (any, error)
	// RoadmapHealth is S50 T3 (RM6): the daemon's own answer over the two
	// blueprint settings — effective name, state, evaluatedAt. A seam
	// rather than a t.Roadmap call because the facts (settings layering +
	// blueprint source reachability) are the daemon's, not the door's.
	RoadmapHealth func(ctx context.Context) (any, error)
	// S29 T7: the triage's daemon-side seams, filled in initTriage
	// (daemon/triage_dispatch.go). Triage is the one internal/triage instance
	// both transports speak through (CG9's extension carried, the same way
	// roadmap and queue ride the table); the three func seams are the daemon
	// actions — the validation-attempt dispatch (T3), the S26 hand-off (T6),
	// and the dedup suggestion (T4, the one that computes the embedding). A
	// nil seam answers ErrStoreUnavailable, loud, never a silent no-op.
	Triage         *triage.Triage
	TriageValidate func(ctx context.Context, p TriageValidateParams) (any, error)
	// TriageHandoff carries the STAMPED actor (S51 T1, TM2): the hand-off
	// records itself on the bug row, and that write and its audit row commit
	// in one transaction — the actor must be in the room where the write
	// happens, so it rides the seam (the roadmap seam's shape).
	TriageHandoff func(ctx context.Context, actor triage.Actor, p TriageHandoffParams) (any, error)
	TriageSuggest func(ctx context.Context, p TriageSuggestParams) (any, error)
	// TriageBoard is S51 T2's read seam (TM5): the board reads the audit
	// for the attempt digest, the vec index for the SCORED candidates, and
	// the roadmap rows for the hand-off status — a daemon read, seam-shaped
	// like its neighbours; nil seam answers ErrStoreUnavailable, loud.
	TriageBoard func(ctx context.Context, p TriageBoardParams) (any, error)
	// TriageHealth is S51 T3's seam (TM4): the daemon answers
	// triage.health over its settings seam and its own blueprint source.
	// The twin of RoadmapHealth; nil seam ⇒ the door is absent-loud,
	// never an empty answer.
	TriageHealth func(ctx context.Context) (any, error)
	// S28 T4: the learnings' daemon-side seam. Learnings is the one
	// internal/learnings instance both transports speak through (P34's
	// ordering: the capability rides the socket first, the CLI and the
	// bench are secondary clients). The scope validation (§3) and the
	// relayed write-protection (M1) live in that one place, so a second
	// authoring path cannot appear; a nil seam answers ErrStoreUnavailable,
	// loud, never a silent no-op.
	Learnings *learnings.Learnings
	// DeliverWebhook is S20 T6's daemon seam (webhook.deliver): it answers
	// a delivery with the five-answer vocabulary and records it; it does
	// not own authentication — the trigger package does, one layer down.
	DeliverWebhook func(context.Context, WebhookDeliverParams) (any, error)
	// CompleteRemote is S21 T6's daemon seam (remote.complete): it verifies
	// the completion against the CONFIGURED peer's own key, binds it to the
	// task (created-by-us · awaiting · callee==sender), and lands it once.
	// Like DeliverWebhook it is `any`-shaped so the table stays the only
	// seam and the daemon owns the mapping; unlike the webhook it refuses
	// AFTER authentication too — an unbound completion is the failure the
	// whole endpoint risks (RA8).
	CompleteRemote func(context.Context, RemoteCompleteParams) (any, error)
	// S20 T8 — the operational control plane (control.go, CLI and app
	// only; never the narrow surface, R6). Each returns the layer's
	// trigger.TriggerView slice / trigger.ControlAnswer; the handlers
	// keep them as `any` exactly as DeliverWebhook does, so the table
	// stays the only seam and the daemon owns the mapping.
	TriggerList    func(context.Context) (any, error)
	TriggerHistory func(context.Context, string, int) (any, error)
	// S49 T4: the four S20 write seams gained the actor the handler
	// resolved from the CONNECTION (AU1 — the seam may not re-derive it),
	// and the handlers refuse identity-key params before decoding. The
	// results keep S20's shapes byte for byte.
	TriggerPause   func(context.Context, Actor, string) (any, error)
	TriggerResume  func(context.Context, Actor, string) (any, error)
	TriggerFireNow func(context.Context, Actor, string) (any, error)
	TriggerAdd     func(context.Context, Actor, TriggerAddParams) (any, error)
	// AutomationWrite is S49 T4's seam for the three new doors
	// (trigger.update, automation.archive, automation.unarchive). Nil ⇒
	// all three ABSENT (RegisterAutomationWrites), not refusing (V13).
	AutomationWrite     AutomationWrite
	ShowBlueprintLoop   func(context.Context, LoopShowParams) (any, error)
	ListLoopFiles       func(context.Context, LoopFileParams) (any, error)
	ReadLoopFile        func(context.Context, LoopFileParams) (any, error)
	AnswerBlueprintLoop func(context.Context, LoopAnswerParams) (any, error)
	// CloseLoop ends a resting loop (S39 CR1): the store claim (idle→done)
	// is the arbiter, the daemon's refusal is data (refused.kind=not-idle),
	// and the loop.finished event rides only on success.
	CloseLoop func(context.Context, LoopCloseParams) (any, error)

	// ArchiveLoop is S33 T9's loop.archive door (blueprint_loop.go): the
	// honest Delete — cold storage, state preserved, telemetry untouched.
	ArchiveLoop func(context.Context, LoopArchiveParams) (any, error)
	// MessageLoop starts a conversation turn in a resting loop (S39 T5's
	// door beside the close's). The daemon answers refusals as data; the
	// table only bounds and forwards.
	MessageLoop func(context.Context, LoopMessageParams) (any, error)
	// AttachmentExpand is the daemon-wired send-time expansion (S69 AT5):
	// pinned receipts become base64 content blocks at the one join point
	// beside request compilation, and a revision the store no longer has
	// comes back as a spoken absence (the second return) rather than a
	// silent hole. nil means no store: references refuse at the door.
	AttachmentExpand func(componentRoot, loopSlug string, refs []AttachmentReceiptView) ([]model.Block, []string)
	// AgentMessages is the daemon-wired mailbox read (S70 T3, MB7): the
	// loop id resolves to its OWN scope behind the seam (scope from the
	// loop row, never a flag), and a named thread the loop does not have
	// comes back as a SAID absence (the second return) rather than a
	// silence a client reads as empty. nil means no store: the read
	// refuses, never invents an empty mailbox.
	AgentMessages func(loopID string, thread *int64) ([]AgentMessageView, bool, error)
	// RunBlueprint / RunOptions are S39 T9's human run doors (no ask — CR4;
	// the agent's ask-gated twin is T10's catalog action, one row two doors).
	RunBlueprint func(context.Context, *Session, LoopRunParams) (any, error)
	RunOptions   func(context.Context, LoopRunOptionsParams) (any, error)
	// RetryHandlerBlock is the daemon's answer to T10's retry: eligibility
	// and refusal are the DAEMON's computation (loop.CheckHandlerRetry
	// against the live registry), handed to the app whole — the UI renders
	// this, never its own guess about whether a retry would work.
	RetryHandlerBlock func(context.Context, LoopRetryHandlerBlockParams) (any, error)
	// RecoverBlockedStep is loop.recover: run the blocked step again (loop.CheckRecover / loop.RecoverBlockedStep).
	RecoverBlockedStep func(context.Context, LoopRecoverParams) (any, error)
	// HandlersList and HandlersVerify serve the bench demo runner. Verify
	// answers refusals as DATA (refused + findings + reason), never as a
	// socket error — a failing demo step is a verdict the runner renders.
	HandlersList   func(context.Context, HandlersListParams) (any, error)
	HandlersVerify func(context.Context, HandlersVerifyParams) (any, error)
	// ListBlueprints is S48 T7 ADDITIVE: the params {origin?, q?} filter
	// daemon-side and the answer keeps `names` byte-compatibly while
	// gaining `blueprints` summaries (the field name kept; the shape grew).
	ListBlueprints  func(BlueprintListParams) (BlueprintListResult, error)
	BlueprintFields func() any
	// The form half: text to structure, structure to text, and the closed sets a
	// form offers. Pure translations; none judges or writes a flow.
	BlueprintModel      func(BlueprintModelParams) (any, error)
	BlueprintRender     func(BlueprintRenderParams) (any, error)
	BlueprintVocabulary func() any
	// ShowBlueprint is S48 T7 ADDITIVE: name/content/path:"" stay; the
	// summary, the standard-mode Analyze report and the derived refs join.
	ShowBlueprint func(BlueprintShowParams) (BlueprintShowResult, error)
	// BlueprintReads is the daemon's read service behind the five T7
	// doors (usage/history/revision/diff/lint); nil leaves them UNREGISTERED
	// (V13). Reads write nothing, take no actor and cache nothing.
	BlueprintReads    BlueprintReadService
	ValidateBlueprint func(BlueprintValidateParams) (any, error)
	// SaveBlueprint is S48 T6's REWRITTEN save seam: the field name is
	// kept (the registration and the bench keep working) and the
	// implementation replaced — the file writer is gone, and the daemon's
	// write service (internal/daemon/blueprint_write.go) writes row,
	// revision and refs through store.BlueprintPutTx after the UNCHANGED
	// Analyze passes. The actor is the connection's person, stamped by the
	// door from ActorFor; the seam never sees a caller-named one (BP12).
	SaveBlueprint func(context.Context, Actor, BlueprintSaveParams) (BlueprintSaveResult, error)
	// BlueprintWrites is the daemon's one blueprint write service behind
	// the three conditional doors (duplicate/delete/revert — V13: nil ⇒
	// absent). Save rides the seam above; the family is one service.
	BlueprintWrites BlueprintWriteService
	// BlueprintOverrides is the daemon's override service for LOCKED blueprints
	// (blueprint.override — nil ⇒ absent, V13).
	BlueprintOverrides BlueprintOverrideService
	OnStepContinued    func(stepID int64, kind, outcome, reason string, questionID int64, cost json.RawMessage, pendingRoute string)
	// LookupLoopID answers "which loop owns this slug in this project root",
	// so stop and continue events can carry `loopId` when the step belongs to
	// a loop (S31 D6; brief §5 — `step.stopped`/`step.continued` carry
	// `loopId?`). A slug is only unique WITH its root (same slug, different
	// root = different loops — the store's own rule), so both are asked for.
	// Absent seam (nil) or no such loop means the daemon genuinely cannot
	// name one: the field is absent from the event, never guessed.
	LookupLoopID func(loopSlug, componentRoot string) string

	methods map[MethodName]*Method
}

// NewTable builds the table with the five S2 methods registered.
func NewTable(sessions *Sessions, subs *Subscriptions, events *EventLog) *Table {
	t := &Table{
		Sessions: sessions,
		Subs:     subs,
		Events:   events,
		methods:  map[MethodName]*Method{},
	}
	t.register(&Method{Name: MethodInitialize, RequiresInitialize: false, Handle: t.handleInitialize})
	t.register(&Method{Name: MethodPing, RequiresInitialize: true, Handle: t.handlePing})
	t.register(&Method{Name: MethodVersion, RequiresInitialize: true, Handle: t.handleVersion})
	t.register(&Method{Name: MethodSubscribe, RequiresInitialize: true, Handle: t.handleSubscribe})
	t.register(&Method{Name: MethodUnsubscribe, RequiresInitialize: true, Handle: t.handleUnsubscribe})
	t.register(&Method{Name: MethodAsk, RequiresInitialize: true, Handle: t.handleAsk})
	t.register(&Method{Name: MethodRoutesList, RequiresInitialize: true, Handle: t.handleRoutesList})
	t.register(&Method{Name: MethodRoutesSet, RequiresInitialize: true, Handle: t.handleRoutesSet})
	t.register(&Method{Name: MethodRoutesDelete, RequiresInitialize: true, Handle: t.handleRoutesDelete})
	t.register(&Method{Name: MethodRoutesProbe, RequiresInitialize: true, Handle: t.handleRoutesProbe})
	t.register(&Method{Name: MethodRoutesTrace, RequiresInitialize: true, Handle: t.handleRoutesTrace})
	t.register(&Method{Name: MethodStoreInfo, RequiresInitialize: true, Handle: t.handleStoreInfo})
	t.register(&Method{Name: MethodStoreAwaiting, RequiresInitialize: true, Handle: t.handleStoreAwaiting})
	t.register(&Method{Name: MethodQuestionPending, RequiresInitialize: true, Handle: t.handleQuestionPending})
	t.register(&Method{Name: MethodWebhookDeliver, RequiresInitialize: true, Handle: t.handleWebhookDeliver})
	// remote.complete (S21 T6): PUBLIC by construction — it is the peer's
	// inbound door, and the narrow table mounts it; Debug would exclude the
	// one method whose whole purpose is the narrow surface (the key is NOT
	// the authentication here — the peer's own signature is, one layer
	// down, exactly as the webhook route's R-5 exemption argued).
	t.register(&Method{Name: MethodRemoteComplete, RequiresInitialize: true, Handle: t.handleRemoteComplete})
	t.register(&Method{Name: MethodTriggerList, RequiresInitialize: true, Handle: t.handleTriggerList})
	t.register(&Method{Name: MethodTriggerPause, RequiresInitialize: true, Handle: t.handleTriggerPause})
	t.register(&Method{Name: MethodTriggerResume, RequiresInitialize: true, Handle: t.handleTriggerResume})
	t.register(&Method{Name: MethodTriggerFireNow, RequiresInitialize: true, Handle: t.handleTriggerFireNow})
	t.register(&Method{Name: MethodTriggerHistory, RequiresInitialize: true, Handle: t.handleTriggerHistory})
	t.register(&Method{Name: MethodTriggerAdd, RequiresInitialize: true, Handle: t.handleTriggerAdd})
	t.register(&Method{Name: MethodLoopCreate, RequiresInitialize: true, Handle: t.handleLoopCreate})
	t.register(&Method{Name: MethodLoopList, RequiresInitialize: true, Handle: t.handleLoopList})
	t.register(&Method{Name: MethodLoopWrite, RequiresInitialize: true, Handle: t.handleLoopWrite})
	t.register(&Method{Name: MethodLoopRead, RequiresInitialize: true, Handle: t.handleLoopRead})
	t.register(&Method{Name: MethodLoopStart, RequiresInitialize: true, Handle: t.handleLoopStart})
	t.register(&Method{Name: MethodLoopShow, RequiresInitialize: true, Handle: t.handleLoopShow})
	t.register(&Method{Name: MethodLoopFiles, RequiresInitialize: true, Handle: t.handleLoopFiles})
	t.register(&Method{Name: MethodLoopFileRead, RequiresInitialize: true, Handle: t.handleLoopFileRead})
	t.register(&Method{Name: MethodLoopAnswer, RequiresInitialize: true, Handle: t.handleLoopAnswer})
	t.register(&Method{Name: MethodLoopClose, RequiresInitialize: true, Handle: t.handleLoopClose})
	t.register(&Method{Name: MethodLoopArchive, RequiresInitialize: true, Handle: t.handleLoopArchive})
	t.register(&Method{Name: MethodLoopMessage, RequiresInitialize: true, Handle: t.handleLoopMessage})
	t.register(&Method{Name: MethodAgentMessages, RequiresInitialize: true, Handle: t.handleAgentMessages})
	// S69: the attachment doors ride beside the human's message door — bytes
	// enter through put once, and read brings a pinned revision back to
	// render, never to re-send.
	t.register(&Method{Name: MethodAttachmentPut, RequiresInitialize: true, Handle: t.handleAttachmentPut})
	t.register(&Method{Name: MethodAttachmentRead, RequiresInitialize: true, Handle: t.handleAttachmentRead})
	t.register(&Method{Name: MethodLoopRun, RequiresInitialize: true, Handle: t.handleLoopRun})
	t.register(&Method{Name: MethodLoopRunOptions, RequiresInitialize: true, Handle: t.handleLoopRunOptions})
	t.register(&Method{Name: MethodLoopRetryHandlerBlock, RequiresInitialize: true, Handle: t.handleLoopRetryHandlerBlock})
	t.register(&Method{Name: MethodLoopRecover, RequiresInitialize: true, Handle: t.handleLoopRecover})
	t.register(&Method{Name: MethodHandlersList, RequiresInitialize: true, Handle: t.handleHandlersList})
	t.register(&Method{Name: MethodHandlersVerify, RequiresInitialize: true, Handle: t.handleHandlersVerify})
	t.register(&Method{Name: MethodComponentCreate, RequiresInitialize: true, Handle: t.handleComponentCreate})
	t.register(&Method{Name: MethodComponentUpdate, RequiresInitialize: true, Handle: t.handleComponentUpdate})
	t.register(&Method{Name: MethodComponentList, RequiresInitialize: true, Handle: t.handleComponentList})
	t.register(&Method{Name: MethodComponentShow, RequiresInitialize: true, Handle: t.handleComponentShow})
	t.register(&Method{Name: MethodComponentPromote, RequiresInitialize: true, Handle: t.handleComponentPromote})
	t.register(&Method{Name: MethodComponentAttach, RequiresInitialize: true, Handle: t.handleComponentAttach})
	t.register(&Method{Name: MethodComponentDetach, RequiresInitialize: true, Handle: t.handleComponentDetach})
	t.register(&Method{Name: MethodComponentArchive, RequiresInitialize: true, Handle: t.handleComponentArchive})
	t.register(&Method{Name: MethodComponentRestore, RequiresInitialize: true, Handle: t.handleComponentRestore})
	t.register(&Method{Name: MethodBlueprintList, RequiresInitialize: true, Handle: t.handleBlueprintList})
	t.register(&Method{Name: MethodBlueprintFields, RequiresInitialize: true, Handle: t.handleBlueprintFields})
	t.register(&Method{Name: MethodBlueprintModel, RequiresInitialize: true, Handle: t.handleBlueprintModel})
	t.register(&Method{Name: MethodBlueprintRender, RequiresInitialize: true, Handle: t.handleBlueprintRender})
	t.register(&Method{Name: MethodBlueprintVocabulary, RequiresInitialize: true, Handle: t.handleBlueprintVocabulary})
	t.register(&Method{Name: MethodBlueprintShow, RequiresInitialize: true, Handle: t.handleBlueprintShow})
	t.register(&Method{Name: MethodBlueprintValidate, RequiresInitialize: true, Handle: t.handleBlueprintValidate})
	t.register(&Method{Name: MethodBlueprintSave, RequiresInitialize: true, Handle: t.handleBlueprintSave})
	t.register(&Method{Name: MethodAutomationTriggerList, RequiresInitialize: true, Handle: t.handleAutomationTriggerList})
	t.register(&Method{Name: MethodAutomationTriggerShow, RequiresInitialize: true, Handle: t.handleAutomationTriggerShow})
	t.register(&Method{Name: MethodAutomationTriggerFires, RequiresInitialize: true, Handle: t.handleAutomationTriggerFires})
	t.register(&Method{Name: MethodAutomationTriggerCreate, RequiresInitialize: true, Handle: t.handleAutomationTriggerCreate})
	t.register(&Method{Name: MethodAutomationTriggerUpdate, RequiresInitialize: true, Handle: t.handleAutomationTriggerUpdate})
	t.register(&Method{Name: MethodAutomationTriggerEnable, RequiresInitialize: true, Handle: t.handleAutomationTriggerEnable})
	t.register(&Method{Name: MethodAutomationTriggerDisable, RequiresInitialize: true, Handle: t.handleAutomationTriggerDisable})
	t.register(&Method{Name: MethodStep, RequiresInitialize: true, Handle: t.handleStep})
	t.register(&Method{Name: MethodAnswer, RequiresInitialize: true, Handle: t.handleAnswer})
	t.register(&Method{Name: MethodStepStop, RequiresInitialize: true, Handle: t.handleStepStop})
	t.register(&Method{Name: MethodStepContinue, RequiresInitialize: true, Handle: t.handleStepContinue})
	t.register(&Method{Name: MethodStepShow, RequiresInitialize: true, Handle: t.handleStepShow})
	t.register(&Method{Name: MethodStepVerify, RequiresInitialize: true, Handle: t.handleStepVerify})
	t.register(&Method{Name: MethodStepList, RequiresInitialize: true, Handle: t.handleStepList})
	t.register(&Method{Name: MethodLoopTranscript, RequiresInitialize: true, Handle: t.handleLoopTranscript})
	t.register(&Method{Name: MethodLoopEvents, RequiresInitialize: true, Handle: t.handleLoopEvents})
	t.register(&Method{Name: MethodLoopSteps, RequiresInitialize: true, Handle: t.handleLoopSteps})

	// S10 (T13): the four read-only policy methods, registered beside the
	// other product reads (ruling R4 — reads for the app; authoring is
	// CLI-only and never arrives here, R4c). `Debug` stays false: these are
	// product surface, not diagnostics — S19's narrow-surface exclusion
	// exists for the debug methods (R6), and a read the app depends on must
	// not be excluded from a surface by accident.
	t.register(&Method{Name: MethodPolicyList, RequiresInitialize: true, Handle: t.handlePolicyList})
	t.register(&Method{Name: MethodPolicyWhy, RequiresInitialize: true, Handle: t.handlePolicyWhy})
	t.register(&Method{Name: MethodPolicyInForce, RequiresInitialize: true, Handle: t.handlePolicyInForce})
	t.register(&Method{Name: MethodPolicyEvents, RequiresInitialize: true, Handle: t.handlePolicyEvents})
	// S45 T2 (PM5/PM6): the two detail reads ride the same table, the same
	// initialize gate, the same Debug=false (product surface, not
	// diagnostics — the four S10 reads above state the argument and it has
	// not changed), and the same zero-writes rule (a read is not an event,
	// and no read here trims, records, or touches the audit). They reuse
	// the SAME Table.PolicyStore handle the gate decides with.
	t.register(&Method{Name: MethodPolicyGet, RequiresInitialize: true, Handle: t.handlePolicyGet})
	t.register(&Method{Name: MethodPolicyVocabulary, RequiresInitialize: true, Handle: t.handlePolicyVocabulary})
	// S45 T3 (PM8): the authoring audit read rides the same table, the same
	// initialize gate, the same Debug=false, and the same zero-writes rule
	// (a read is not an event). It reaches its own table — never the
	// boundary the gate decides with (PM11: the audit is downstream of
	// every decision) — through the PolicyChanges seam below.
	t.register(&Method{Name: MethodPolicyChanges, RequiresInitialize: true, Handle: t.handlePolicyChanges})
	// S25 T4: the three history reads ride the same table, the same initialize
	// gate, and the same no-audit-row rule (a read is not an event). The
	// handlers live with their shape in history.go.
	t.register(&Method{Name: MethodHistoryList, RequiresInitialize: true, Handle: t.handleHistoryList})
	t.register(&Method{Name: MethodHistoryShow, RequiresInitialize: true, Handle: t.handleHistoryShow})
	t.register(&Method{Name: MethodHistoryExport, RequiresInitialize: true, Handle: t.handleHistoryExport})
	// S53 T1 (HV5): coverage (HV1's honesty read) and vocabulary (HV2's closed
	// sets) ride the same table, the same initialize gate and the same seam —
	// reads, no audit row, off the narrow surface, and no export twin is added
	// anywhere (HV3).
	t.register(&Method{Name: MethodHistoryCoverage, RequiresInitialize: true, Handle: t.handleHistoryCoverage})
	t.register(&Method{Name: MethodHistoryVocabulary, RequiresInitialize: true, Handle: t.handleHistoryVocabulary})
	// S24 T7: the queue verbs ride the same table, same initialize gate
	// (registerQueue lives with its handlers in queue.go).
	t.registerQueue()
	// S26 T5: the roadmap verbs ride the same table, same initialize gate
	// (registerRoadmap lives with its handlers in roadmap.go).
	t.registerRoadmap()
	// S29 T7: the triage verbs ride the same table, same initialize gate
	// (registerTriage lives with its handlers in triage.go).
	t.registerTriage()
	// S28 T4: the learnings verbs ride the same table, same initialize gate
	// (registerLearnings lives with its handlers in learnings.go).
	t.registerLearnings()

	// S31: the loop-workspace surface. Registered at slice open so the method
	// table is ONE reviewable diff (the argument for `services.golden` at
	// AGENTS B11, applied to the socket); each handler lands with its task and
	// replaces its `ErrNotWired` stub. `step.continue` gained its `message`
	// field with T2 — same method name, additive params, no re-registration.
	t.register(&Method{Name: MethodStepSteer, RequiresInitialize: true, Handle: t.handleSteer})
	t.register(&Method{Name: MethodStepSteers, RequiresInitialize: true, Handle: t.handleSteers})
	t.register(&Method{Name: MethodStepSteerCancel, RequiresInitialize: true, Handle: t.handleSteerCancel})
	t.register(&Method{Name: MethodWorkspaceGitStatus, RequiresInitialize: true, Handle: t.handleWorkspaceGitStatus})
	t.register(&Method{Name: MethodWorkspaceGitDiff, RequiresInitialize: true, Handle: t.handleWorkspaceGitDiff})
	t.register(&Method{Name: MethodVoiceTranscribe, RequiresInitialize: true, Handle: t.handleVoiceTranscribe})
	t.register(&Method{Name: MethodVoiceSpeak, RequiresInitialize: true, Handle: t.handleVoiceSpeak})
	t.register(&Method{Name: MethodVoiceTurn, RequiresInitialize: true, Handle: t.handleVoiceTurn})
	t.register(&Method{Name: MethodVoiceEmbed, RequiresInitialize: true, Handle: t.handleVoiceEmbed})
	t.register(&Method{Name: MethodLoopDocuments, RequiresInitialize: true, Handle: t.handleLoopDocuments})
	t.register(&Method{Name: MethodLoopSetArchived, RequiresInitialize: true, Handle: t.handleLoopSetArchived})
	t.register(&Method{Name: MethodLoopDelete, RequiresInitialize: true, Handle: t.handleLoopDelete})
	t.register(&Method{Name: MethodLoopTombstones, RequiresInitialize: true, Handle: t.handleLoopTombstones})
	t.register(&Method{Name: MethodLoopOptions, RequiresInitialize: true, Handle: t.handleLoopOptions})
	t.register(&Method{Name: MethodWorkspaceGitChanges, RequiresInitialize: true, Handle: t.handleWorkspaceGitChanges})
	t.register(&Method{Name: MethodTerminalOpen, RequiresInitialize: true, Handle: t.handleTerminalOpen})
	t.register(&Method{Name: MethodTerminalInput, RequiresInitialize: true, Handle: t.handleTerminalInput})
	t.register(&Method{Name: MethodTerminalResize, RequiresInitialize: true, Handle: t.handleTerminalResize})
	t.register(&Method{Name: MethodTerminalAttach, RequiresInitialize: true, Handle: t.handleTerminalAttach})
	t.register(&Method{Name: MethodTerminalDetach, RequiresInitialize: true, Handle: t.handleTerminalDetach})
	t.register(&Method{Name: MethodTerminalClose, RequiresInitialize: true, Handle: t.handleTerminalClose})
	t.register(&Method{Name: MethodTerminalList, RequiresInitialize: true, Handle: t.handleTerminalList})

	// S14 T10: the agent-services surface. Registered in one block, all
	// seven through the ONE forwarding handler (agent.go), so a method
	// registered in two files — the surface that grows by accident — is
	// structurally unavailable here.
	t.register(&Method{Name: MethodAgentDiscover, RequiresInitialize: true, Handle: t.agentServicesCall(MethodAgentDiscover)})
	t.register(&Method{Name: MethodAgentCard, RequiresInitialize: true, Handle: t.agentServicesCall(MethodAgentCard)})
	t.register(&Method{Name: MethodAgentInvoke, RequiresInitialize: true, Debug: true, Handle: t.agentServicesCall(MethodAgentInvoke)})
	t.register(&Method{Name: MethodAgentTaskList, RequiresInitialize: true, Handle: t.agentServicesCall(MethodAgentTaskList)})
	t.register(&Method{Name: MethodAgentTaskShow, RequiresInitialize: true, Handle: t.agentServicesCall(MethodAgentTaskShow)})
	t.register(&Method{Name: MethodAgentTaskComplete, RequiresInitialize: true, Debug: true, Handle: t.agentServicesCall(MethodAgentTaskComplete)})
	t.register(&Method{Name: MethodAgentTaskFail, RequiresInitialize: true, Debug: true, Handle: t.agentServicesCall(MethodAgentTaskFail)})
	// S15: the plugins read — public (a read, like policy.* and the four
	// agent reads: the app's window), answered by the entry point's injected
	// reader because no layer may import the registry but the seam.
	t.register(&Method{Name: MethodAgentPlugins, RequiresInitialize: true, Handle: t.agentPluginsCall()})
	t.register(&Method{Name: MethodAgentList, RequiresInitialize: true, Handle: t.handleAgentList})
	t.register(&Method{Name: MethodToolsGrants, RequiresInitialize: true, Handle: t.handleToolsGrants})
	t.register(&Method{Name: MethodMCPStatus, RequiresInitialize: true, Handle: t.mcpCall(MethodMCPStatus)})
	t.register(&Method{Name: MethodMCPCheck, RequiresInitialize: true, Handle: t.mcpCall(MethodMCPCheck)})
	t.register(&Method{Name: MethodProposalCreate, RequiresInitialize: true, Handle: t.handleProposalCreate})
	t.register(&Method{Name: MethodProposalList, RequiresInitialize: true, Handle: t.handleProposalList})
	t.register(&Method{Name: MethodProposalDiscard, RequiresInitialize: true, Handle: t.handleProposalDiscard})
	t.register(&Method{Name: MethodAgentEnable, RequiresInitialize: true, Handle: t.agentTransitionCall(MethodAgentEnable)})
	t.register(&Method{Name: MethodAgentDisable, RequiresInitialize: true, Handle: t.agentTransitionCall(MethodAgentDisable)})
	t.register(&Method{Name: MethodAgentRevoke, RequiresInitialize: true, Handle: t.agentTransitionCall(MethodAgentRevoke)})
	// S43 T7: the agent-management family, one seam (AgentManage), dispatched
	// by name — the shape agent.enable/disable/revoke use. Not PL15 policy
	// authoring (they touch the agent table, not the closed policy set), so
	// they are ordinary daemon writes.
	t.register(&Method{Name: MethodAgentCreate, RequiresInitialize: true, Handle: t.agentManageCall(MethodAgentCreate)})
	t.register(&Method{Name: MethodAgentImport, RequiresInitialize: true, Handle: t.agentManageCall(MethodAgentImport)})
	t.register(&Method{Name: MethodAgentExport, RequiresInitialize: true, Handle: t.agentManageCall(MethodAgentExport)})
	t.register(&Method{Name: MethodAgentDuplicate, RequiresInitialize: true, Handle: t.agentManageCall(MethodAgentDuplicate)})
	t.register(&Method{Name: MethodAgentDelete, RequiresInitialize: true, Handle: t.agentManageCall(MethodAgentDelete)})
	t.register(&Method{Name: MethodAgentEdit, RequiresInitialize: true, Handle: t.agentManageCall(MethodAgentEdit)})
	t.register(&Method{Name: MethodAgentView, RequiresInitialize: true, Handle: t.agentManageCall(MethodAgentView)})
	t.register(&Method{Name: MethodAgentMigrate, RequiresInitialize: true, Handle: t.agentManageCall(MethodAgentMigrate)})
	// S73 T2: the management window joins the family's one seam. A read, so
	// it is registered exactly as its sibling reads are — conditional on the
	// seam (V13: absent, not refusing), RequiresInitialize, never Debug.
	t.register(&Method{Name: MethodAgentRows, RequiresInitialize: true, Handle: t.agentManageCall(MethodAgentRows)})
	// S73 T5: the ledger's window, on the same seam and under the same shape —
	// V13's conditional registration, RequiresInitialize, never Debug, and no
	// actor, because a history read changes nothing (AR4's line, SP6's word).
	t.register(&Method{Name: MethodAgentChanges, RequiresInitialize: true, Handle: t.agentManageCall(MethodAgentChanges)})

	t.register(&Method{
		Name:               MethodDebugSubscriptions,
		RequiresInitialize: true,
		Debug:              true,
		Handle:             t.handleDebugSubscriptions,
	})
	t.register(&Method{
		Name:               MethodDebugGoroutines,
		RequiresInitialize: true,
		Debug:              true,
		Handle:             t.handleDebugGoroutines,
	})
	// S38 T15 (OL10/OL11): the five doors a person alone may knock on come
	// FIRST — where a catalog Spec shares a method's name (task_list's twin
	// is task.list, goal_show's is goal.show), the person's richer door is
	// the one that stands, and the projection below skips the Spec rather
	// than shadowing a method that predates it.
	t.register(&Method{Name: MethodGoalDecide, RequiresInitialize: true, Handle: t.handleGoalDecide})
	t.register(&Method{Name: MethodBriefDecide, RequiresInitialize: true, Handle: t.handleBriefDecide})
	t.register(&Method{Name: MethodTaskDecide, RequiresInitialize: true, Handle: t.handleTaskDecide})
	t.register(&Method{Name: MethodTaskList, RequiresInitialize: true, Handle: t.handleTaskList})
	t.register(&Method{Name: MethodGoalShow, RequiresInitialize: true, Handle: t.handleGoalShow})
	// …then the catalog projected one-definition-two-doors.
	// S33 T8: the telemetry reads, registered in one block — four doors
	// onto numbers nobody but the runtime's own seams can write.
	// RequiresInitialize like every product read; NOT Debug (TL10 designs
	// them as the primary surface, not diagnostics).
	t.register(&Method{Name: MethodTelemetryLoop, RequiresInitialize: true, Handle: t.handleTelemetryLoop})
	t.register(&Method{Name: MethodTelemetryComponent, RequiresInitialize: true, Handle: t.handleTelemetryComponent})
	t.register(&Method{Name: MethodTelemetryProject, RequiresInitialize: true, Handle: t.handleTelemetryProject})
	t.register(&Method{Name: MethodTelemetryWorkspace, RequiresInitialize: true, Handle: t.handleTelemetryWorkspace})

	t.registerRecords()
	return t
}

func (t *Table) register(m *Method) {
	if _, dup := t.methods[m.Name]; dup {
		// A duplicate would silently shadow the first, which is the one bug a
		// method table can have that no test of either method would catch.
		panic(fmt.Sprintf("protocol: method %q registered twice", m.Name))
	}
	t.methods[m.Name] = m
}

// Methods returns the method names, sorted, for `--print-methods` and tests.
func (t *Table) Methods() []MethodName {
	out := make([]MethodName, 0, len(t.methods))
	for name := range t.methods {
		out = append(out, name)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// PublicMethods returns the methods a narrowed surface may mount: everything
// except the debug ones. S19's HTTP and stdio surfaces take this, not Methods.
func (t *Table) PublicMethods() []MethodName {
	var out []MethodName
	for _, name := range t.Methods() {
		if m, ok := t.methods[name]; ok && !m.Debug {
			out = append(out, name)
		}
	}
	return out
}

// Lookup returns a method, or false. The caller answers an unknown method with
// JSON-RPC's own code.
func (t *Table) Lookup(name MethodName) (*Method, bool) {
	m, ok := t.methods[name]
	return m, ok
}

// Dispatch runs one method for one connection.
//
// **The gate is here and nowhere else** (I3, T7): every method whose
// RequiresInitialize is true passes through Sessions.Require, and no handler
// re-checks. A second check would drift, the looser one would win, and T7 would
// become a suggestion.
func (t *Table) Dispatch(ctx context.Context, connID string, name MethodName, params json.RawMessage) (any, error) {
	m, ok := t.Lookup(name)
	if !ok {
		// The caller maps this to JSON-RPC's method-not-found. Returning a
		// sentinel rather than an Aeon error keeps the distinction honest.
		return nil, ErrUnknownMethod{Name: name}
	}

	var sess *Session
	if m.RequiresInitialize {
		var err error
		if sess, err = t.Sessions.Require(connID, string(name)); err != nil {
			return nil, err
		}
	} else {
		// `initialize` has no session yet; it creates one. The connID travels
		// in the Session it builds.
		sess = &Session{ConnID: connID}
	}
	return m.Handle(ctx, sess, params)
}

// ErrUnknownMethod is returned for a method not in the table, so the transport
// layer can answer with JSON-RPC's own -32601 rather than an Aeon code.
type ErrUnknownMethod struct{ Name MethodName }

func (e ErrUnknownMethod) Error() string { return fmt.Sprintf("method %q is not registered", e.Name) }

// sortStrings is here rather than imported at each use so the dependency list
// of this package stays short enough to read.
func sortStrings(s []string) { sort.Strings(s) }
