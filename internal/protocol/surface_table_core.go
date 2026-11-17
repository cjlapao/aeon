// The core of the surface table (S47 T6, TC6): the transport and the connection, the model
// call, the routes, the waiting room, the trigger plane, the loop and step doors, the
// workspace and the terminals. One entry per method, keyed by the declared constant so a
// name that stops existing is a compile error and a stale entry cannot hide.
package protocol

// surfaceTableCore is one slice of the closed classification table, built fresh on every
// call: there is no package-level map to append to, so a method cannot enter the surface
// by mutating shared state — only by editing a table, which the completeness tests then
// check both ways.
func surfaceTableCore() map[MethodName]Surface {
	return map[MethodName]Surface{
		MethodAgentMessages: {
			Kind:    SurfaceRead,
			Summary: "Lists a loop's mailbox rows; there is deliberately no authoring method \u2014 a step's own run files them.",
		},
		MethodAnswer: {
			Kind:          SurfaceControl,
			Summary:       "Answers a step's suspended question on the step surface, the same door the runtime waits on.",
			HumanDoorOnly: true,
		},
		MethodAsk: {
			Kind:    SurfaceControl,
			Summary: "Streams one reply from a model over this connection, on the route the caller names.",
		},
		MethodAttachmentPut: {
			Kind:    SurfaceWrite,
			Summary: "Writes an attachment bytes once \u2014 the only crossing bytes ever make over this socket.",
		},
		MethodAttachmentRead: {
			Kind:    SurfaceRead,
			Summary: "Brings a pinned attachment revision back for rendering, never for re-sending.",
		},
		MethodAutomationTriggerCreate: {
			Kind:          SurfaceWrite,
			Summary:       "Creates an automation trigger; the actor is the connection peer, never a param claim.",
			HumanDoorOnly: true,
		},
		MethodAutomationTriggerDisable: {
			Kind:          SurfaceControl,
			Summary:       "Takes an automation trigger off the scheduler; its history stays where its fires are.",
			HumanDoorOnly: true,
		},
		MethodAutomationTriggerEnable: {
			Kind:          SurfaceControl,
			Summary:       "Puts an automation trigger back on the scheduler.",
			HumanDoorOnly: true,
		},
		MethodAutomationTriggerFires: {
			Kind:    SurfaceRead,
			Summary: "The fire log of one automation trigger, newest first, with each outcome.",
		},
		MethodAutomationTriggerList: {
			Kind:    SurfaceRead,
			Summary: "The automation desk catalogue: every trigger, its kind and whether it is live.",
		},
		MethodAutomationTriggerShow: {
			Kind:    SurfaceRead,
			Summary: "One automation trigger as stored, with the schedule and the agent it fires.",
		},
		MethodAutomationTriggerUpdate: {
			Kind:          SurfaceWrite,
			Summary:       "Updates an automation trigger in place, re-validated before any row is touched.",
			HumanDoorOnly: true,
		},
		MethodDebugGoroutines: {
			Kind:    SurfaceDiagnostic,
			Summary: "Dumps the daemon goroutine stacks, so a suspended agent task can be proven absent from outside the process.",
		},
		MethodDebugSubscriptions: {
			Kind:    SurfaceDiagnostic,
			Summary: "Lists the live subscription registry \u2014 the demonstration that a dropped connection empties it.",
		},
		MethodHandlersList: {
			Kind:    SurfaceRead,
			Summary: "The handler registry as renderable data \u2014 the bench demo runner reads it, the door stays the CLI.",
		},
		MethodHandlersVerify: {
			Kind:    SurfaceRead,
			Summary: "Runs the negative-control verify over the handler registry and reports what it proved.",
		},
		MethodInitialize: {
			Kind:    SurfaceControl,
			Summary: "Opens a session and returns the protocol version, the daemon version and the capability set.",
		},
		MethodLoopAnswer: {
			Kind:          SurfaceControl,
			Summary:       "Answers a loop's suspended question; the answer goes to the model, it never mints a permission.",
			HumanDoorOnly: true,
		},
		MethodLoopArchive: {
			Kind:          SurfaceWrite,
			Summary:       "Puts a loop and its documents into cold storage; the rollups do not move.",
			HumanDoorOnly: true,
		},
		MethodLoopCancel: {
			Kind:          SurfaceControl,
			Summary:       "Cancels a loop: the engine endStatus is the one writer, and a cancel never runs the abort lane.",
			HumanDoorOnly: true,
		},
		MethodLoopClose: {
			Kind:          SurfaceControl,
			Summary:       "Ends a RESTING loop, idle to done; any other state is refused with a sentence, never silence.",
			HumanDoorOnly: true,
		},
		MethodLoopControl: {
			Kind:    SurfaceRead,
			Summary: "A loop's control picture: eligibility, open requests, cap-hit state and the last control events.",
		},
		MethodLoopControlDismiss: {
			Kind:          SurfaceWrite,
			Summary:       "Dismisses a filed stop request with the person verdict, leaving the loop running.",
			HumanDoorOnly: true,
		},
		MethodLoopQuestions: {
			Kind:    SurfaceRead,
			Summary: "The questions a parked loop's step asked, with the answers already given, so a reopened chat resumes where it left off.",
		},
		MethodLoopTodos: {
			Kind:    SurfaceRead,
			Summary: "A loop's todo list: the active list, or the last closed one, with its counts and who closed it.",
		},
		MethodLoopTodosClose: {
			Kind:          SurfaceWrite,
			Summary:       "Closes a loop's active todo list as the person's act; an agent can only finish or clear its own list.",
			HumanDoorOnly: true,
		},
		MethodVaultList: {
			Kind:    SurfaceRead,
			Summary: "Lists the managed variables: name, scope, flags and description; a secret's value is never in the answer.",
		},
		MethodVaultChanges: {
			Kind:    SurfaceRead,
			Summary: "The variable ledger: who set, deleted or read (as an agent) which variable and when; never a value.",
		},
		MethodVaultSet: {
			Kind:          SurfaceWrite,
			Summary:       "Creates or edits a managed variable; a new one is a secret hidden from agents until a person says otherwise.",
			HumanDoorOnly: true,
		},
		MethodVaultDelete: {
			Kind:          SurfaceWrite,
			Summary:       "Deletes a managed variable in one scope.",
			HumanDoorOnly: true,
		},
		MethodLoopControlRequests: {
			Kind:    SurfaceRead,
			Summary: "Lists a loop's stop requests with who filed them and what has been done about each.",
		},
		MethodLoopCreate: {
			Kind:    SurfaceWrite,
			Summary: "Creates a loop row from a blueprint and a goal; nothing runs until a start.",
		},
		MethodLoopFileRead: {
			Kind:    SurfaceRead,
			Summary: "Reads one loop workspace document for rendering; the transcript is references all the way down.",
		},
		MethodLoopFiles: {
			Kind:    SurfaceRead,
			Summary: "Lists a loop workspace documents \u2014 names, sizes, revisions.",
		},
		MethodLoopList: {
			Kind:    SurfaceRead,
			Summary: "Lists loops with state, blueprint and cost so far \u2014 the projection the narrow surface reads.",
		},
		MethodLoopMessage: {
			Kind:          SurfaceControl,
			Summary:       "Sends one conversation turn to a resting loop, streamed and steerable like any step.",
			HumanDoorOnly: true,
		},
		MethodLoopRaise: {
			Kind:          SurfaceWrite,
			Summary:       "Lifts a loop's budget cap \u2014 the budget act that opens the park, never a loop-side knob.",
			HumanDoorOnly: true,
		},
		MethodLoopRead: {
			Kind:    SurfaceRead,
			Summary: "Reads back a loop workspace document by reference.",
		},
		MethodLoopRequestStop: {
			Kind:    SurfaceWrite,
			Summary: "Files an inert stop request an agent may raise; nothing halts until a person acts on it.",
		},
		MethodLoopResume: {
			Kind:          SurfaceControl,
			Summary:       "Resumes a stopped or parked loop from its persisted state.",
			HumanDoorOnly: true,
		},
		MethodLoopRetryHandlerBlock: {
			Kind:          SurfaceControl,
			Summary:       "Retries one historical missing-handler block once; the daemon decides eligibility and states the refusal.",
			HumanDoorOnly: true,
		},
		MethodLoopRecover: {
			Kind:          SurfaceControl,
			Summary:       "Takes a blocked loop back to the step that blocked and runs it again; the daemon decides eligibility and states the refusal.",
			HumanDoorOnly: true,
		},
		MethodLoopRun: {
			Kind:    SurfaceControl,
			Summary: "Starts a NEW run of a blueprint inside a resting loop; an agent reaches the same act through run_start, which asks first.",
		},
		MethodLoopRunOptions: {
			Kind:    SurfaceRead,
			Summary: "Lists what the loop could run next and why each candidate could not \u2014 the dialogue before the start.",
		},
		MethodLoopShow: {
			Kind:    SurfaceRead,
			Summary: "One loop as the rows hold it: state, blueprint, steps, cost, the last event.",
		},
		MethodLoopStart: {
			Kind:    SurfaceControl,
			Summary: "Starts a loop running; the narrow surface carries it alone, as the one trigger action.",
		},
		MethodLoopStop: {
			Kind:          SurfaceControl,
			Summary:       "Stops a running loop on a person request, with the reason it was asked for.",
			HumanDoorOnly: true,
		},
		MethodLoopEvents: {
			Kind:    SurfaceRead,
			Summary: "One loop's event journal as a window, the same payloads a live subscriber received, so a reopened loop replays exactly.",
		},
		MethodVoiceTranscribe: {
			Kind:    SurfaceRead,
			Summary: "Turns a recording into text with a route that declares the asr capability; changes nothing.",
		},
		MethodVoiceSpeak: {
			Kind:    SurfaceRead,
			Summary: "Turns a sentence into audio with a route that declares the tts capability; changes nothing.",
		},
		MethodVoiceEmbed: {
			Kind:    SurfaceRead,
			Summary: "Embeds one short text with a route that declares the embedding capability and reports the vector size; changes nothing.",
		},
		MethodVoiceTurn: {
			Kind:    SurfaceRead,
			Summary: "One model call for the voice operator with the tools the app offers; returns text and the tools asked for, runs none.",
		},
		MethodLoopDocuments: {
			Kind:    SurfaceRead,
			Summary: "Lists the documents a loop's agents filed in the database, with revision, size and writer; read the text with loop.read.",
		},
		MethodLoopSetArchived: {
			Kind:          SurfaceControl,
			Summary:       "Hides a loop from the lists, or shows it again; nothing under it changes. Refused for a running loop.",
			HumanDoorOnly: true,
		},
		MethodLoopDelete: {
			Kind:          SurfaceControl,
			Summary:       "Deletes a loop with its steps, chat and documents while keeping its totals and the work linked to it; confirm must be the loop's slug.",
			HumanDoorOnly: true,
		},
		MethodLoopTombstones: {
			Kind:    SurfaceRead,
			Summary: "Lists the loops deleted from a component with the totals each one earned before it went.",
		},
		MethodLoopOptions: {
			Kind:          SurfaceControl,
			Summary:       "Sets the thinking switch and reasoning effort for a loop's later model steps; empty clears them.",
			HumanDoorOnly: true,
		},
		MethodLoopSteps: {
			Kind:    SurfaceRead,
			Summary: "One loop's step summaries (turns, tool calls, tokens, cost, time per step), newest first, with the loop's totals on the first page.",
		},
		MethodLoopTranscript: {
			Kind:    SurfaceRead,
			Summary: "The loop-wide transcript window across every step it ran, for what the event ring has forgotten.",
		},
		MethodLoopWrite: {
			Kind:    SurfaceWrite,
			Summary: "Writes a loop workspace document through the store, the only door that touches those bytes.",
		},
		MethodPing: {
			Kind:    SurfaceRead,
			Summary: "Answers the transport is alive, with the daemon uptime; changes nothing.",
		},
		MethodQuestionPending: {
			Kind:    SurfaceRead,
			Summary: "The one question a loop is blocked on, with its kind and declared answers \u2014 WHY it waits, not just that it does.",
		},
		MethodRemoteComplete: {
			Kind:    SurfaceControl,
			Summary: "Settles a remote async task this daemon created, bound to what the callee was actually asked.",
		},
		MethodRoutesDelete: {
			Kind:          SurfaceWrite,
			Summary:       "Removes a configured provider route by name.",
			HumanDoorOnly: true,
		},
		MethodRoutesList: {
			Kind:    SurfaceRead,
			Summary: "Lists configured provider routes with hasKey and a four-character suffix hint \u2014 never the key itself.",
		},
		MethodRoutesProbe: {
			Kind:    SurfaceControl,
			Summary: "Dials one route once and caches what it declared; never fires on its own, only when a person asks.",
		},
		MethodRoutesTrace: {
			Kind:    SurfaceControl,
			Summary: "Sends one request on a route and returns the body sent and the raw reply, with per-call overrides; the test bench's way to see the wire.",
		},
		MethodRoutesSet: {
			Kind:          SurfaceWrite,
			Summary:       "Creates or updates a provider route; an empty key keeps the stored one, and no response can carry a credential.",
			HumanDoorOnly: true,
		},
		MethodStep: {
			Kind:    SurfaceControl,
			Summary: "Runs one bounded step and streams its events; the skateboard entry point.",
		},
		MethodStepContinue: {
			Kind:          SurfaceControl,
			Summary:       "Resumes a step a person stopped, with an optional route and reasoning-effort override.",
			HumanDoorOnly: true,
		},
		MethodStepList: {
			Kind:    SurfaceRead,
			Summary: "Lists a loop's steps in order with their status and outcome.",
		},
		MethodStepShow: {
			Kind:    SurfaceRead,
			Summary: "One step as the rows hold it \u2014 status, manifest, cost, the handoff it ended on.",
		},
		MethodStepSteer: {
			Kind:          SurfaceControl,
			Summary:       "Adds steering to a step while it runs, drained at the next turn boundary.",
			HumanDoorOnly: true,
		},
		MethodStepSteerCancel: {
			Kind:          SurfaceControl,
			Summary:       "Withdraws a queued steer before the step reaches it.",
			HumanDoorOnly: true,
		},
		MethodStepSteers: {
			Kind:    SurfaceRead,
			Summary: "Lists a step's queued steering and what has already been drained into it.",
		},
		MethodStepStop: {
			Kind:          SurfaceControl,
			Summary:       "Interrupts a RUNNING step on a person choice, never a model choice; a stop is not a question.",
			HumanDoorOnly: true,
		},
		MethodStepVerify: {
			Kind:    SurfaceRead,
			Summary: "Re-checks a finished step against its own verification spec and reports the verdict.",
		},
		MethodStoreAwaiting: {
			Kind:    SurfaceRead,
			Summary: "Lists the loops waiting, and on what: the waiting room the narrow surface is allowed to read.",
		},
		MethodStoreInfo: {
			Kind:    SurfaceRead,
			Summary: "Reports the store file, its schema state and counters \u2014 the bench Store tab in one answer.",
		},
		MethodSubscribe: {
			Kind:    SurfaceControl,
			Summary: "Attaches this connection to the event stream from a cursor, and replays what it missed.",
		},
		MethodTerminalAttach: {
			Kind:          SurfaceControl,
			Summary:       "Attaches to a running terminal and follows its output stream.",
			HumanDoorOnly: true,
		},
		MethodTerminalClose: {
			Kind:          SurfaceControl,
			Summary:       "Closes a terminal and ends the process behind it.",
			HumanDoorOnly: true,
		},
		MethodTerminalDetach: {
			Kind:          SurfaceControl,
			Summary:       "Detaches from a terminal; the process it holds keeps running.",
			HumanDoorOnly: true,
		},
		MethodTerminalInput: {
			Kind:          SurfaceControl,
			Summary:       "Sends keystrokes to a terminal this connection may see.",
			HumanDoorOnly: true,
		},
		MethodTerminalList: {
			Kind:    SurfaceRead,
			Summary: "Lists the live terminals for a step, with their state and owner.",
		},
		MethodTerminalOpen: {
			Kind:          SurfaceControl,
			Summary:       "Opens the person own shell in a step workspace \u2014 a P26 socket door, never a remote trigger.",
			HumanDoorOnly: true,
		},
		MethodTerminalResize: {
			Kind:          SurfaceControl,
			Summary:       "Resizes a running terminal window.",
			HumanDoorOnly: true,
		},
		MethodTriggerAdd: {
			Kind:          SurfaceWrite,
			Summary:       "Adds a schedule row; an existing name is refused by name, never silently rewritten.",
			HumanDoorOnly: true,
		},
		MethodTriggerFireNow: {
			Kind:          SurfaceControl,
			Summary:       "Fires a trigger now, AS a trigger: unattended, with no person as a party to the run.",
			HumanDoorOnly: true,
		},
		MethodTriggerHistory: {
			Kind:    SurfaceRead,
			Summary: "Reports a trigger fires and its gaps, with the honest absence between them.",
		},
		MethodTriggerList: {
			Kind:    SurfaceRead,
			Summary: "Lists the schedules with their state, the way the CLI prints them.",
		},
		// S49 T4: the clock/webhook edit door, beside the S20 controls it
		// joins. A write, and a human door — a non-person actor is refused
		// before the row is even read, in the registry's own sentence.
		MethodTriggerUpdate: {
			Kind:          SurfaceWrite,
			Summary:       "Edits a schedule or webhook through the validator its maker answered — never an enable; the next instants ride the answer.",
			HumanDoorOnly: true,
		},
		MethodTriggerPause: {
			Kind:          SurfaceControl,
			Summary:       "Pauses a schedule so it stops firing; the row and its history stay.",
			HumanDoorOnly: true,
		},
		MethodTriggerResume: {
			Kind:          SurfaceControl,
			Summary:       "Resumes a paused schedule from its last checkpoint.",
			HumanDoorOnly: true,
		},
		MethodUnsubscribe: {
			Kind:    SurfaceControl,
			Summary: "Detaches this connection from an event subscription; its events stop arriving.",
		},
		MethodVersion: {
			Kind:    SurfaceRead,
			Summary: "Reports the daemon version, build time and git commit as data.",
		},
		MethodWebhookDeliver: {
			Kind:    SurfaceControl,
			Summary: "Mounts a webhook delivery on this surface; the signature rides as a param because stdio has no headers.",
		},
		MethodWorkspaceGitChanges: {
			Kind:    SurfaceRead,
			Summary: "Lists every changed path of a project's working tree with its added and removed line counts, three-state like git status.",
		},
		MethodWorkspaceGitDiff: {
			Kind:    SurfaceRead,
			Summary: "Reports the unified diff of the step workspace against its base revision.",
		},
		MethodWorkspaceGitStatus: {
			Kind:    SurfaceRead,
			Summary: "Reports the step workspace's git status as data for the loop workspace view.",
		},
	}
}
