package protocol

import (
	"encoding/json"
	"fmt"
)

// # The error registry (T10, O3, I2)
//
// Every Aeon error code is allocated HERE, in this one file, with its three
// parts and its fatal marker. One place, because a code allocated in two places
// is a code that means two things.
//
// # A code is never reused for a different meaning
//
// Not even after the method that produced it is removed. Reuse breaks the
// promise years later, when nobody remembers the first meaning and a client
// pinned to an older version reads the new one. **To retire a code, leave it in
// this table marked retired — do not free it.**
//
// # The range
//
// Aeon owns -31000 to -31999. JSON-RPC's own codes (-32768 to -32000) are
// RESERVED by the specification and must never appear here: the transport
// already issues -32700 parse, -32600 invalid request, -32601 method not found,
// -32602 invalid params, -32603 internal, and the framework's own -32002 server
// not ready, -32001 connection limit and -32000 batch unsupported.
//
// A protocol-level condition keeps its JSON-RPC code. An unknown method is
// -32601 and not an Aeon code, because it is a fact about JSON-RPC rather than
// about Aeon (T2's method table).
//
// # Bands, so later slices do not collide
//
//	-31001 .. -31009   session and negotiation      (this slice)
//	-31010 .. -31019   subscriptions and filters    (this slice)
//	-31020 .. -31099   reserved, protocol-level
//	-31100 .. -31999   domain codes, allocated by the slice that needs them
//	                   -31100 .. -31103  S3, the model layer
//	                   -31104 .. -31108  S4, the store
//	                   -31109 .. -31110  S19 T7, loop-start domain refusals
//	                                     (the narrow surface must be able to say
//	                                     refused/409, not transport/503-retry,
//	                                     when the daemon understood and declined)
//	                   -31200 .. -31203  S5, the step runtime
//
// The design's §Errors illustrates the shape with `-31004: no pending question`.
// That number is an illustration in a JSON blob, not an allocation: questions
// arrive at S6 and take a code from the domain band. -31004 is left unused so
// the two can never be confused.
const (
	ReservedJSONRPCMin = -32768
	ReservedJSONRPCMax = -32000

	AeonCodeMin = -31999
	AeonCodeMax = -31000
)

// Error is an Aeon protocol error: a JSON-RPC error object whose `data` carries
// the three-part shape (T10, C9).
//
// Three parts and a fatal marker, because error text IS the interface when
// something goes wrong. It arrives in S2 rather than S23 because retrofitting
// it means revisiting every method that already returns errors.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    Data   `json:"data"`
}

// Data is the three-part payload every Aeon error carries.
type Data struct {
	// Kind is an OPTIONAL machine-readable discriminator (S55 T5, LC2):
	// the loop-control plane's refusals ride one code (ErrLoopControlRefused)
	// whose remedy differs by the state found, so a client can branch on
	// data.kind without parsing the sentence. It is omitempty: every error
	// that does not carry a kind renders byte-identically to before S55,
	// and the three parts below remain the contract for every error.
	Kind string `json:"kind,omitempty"`
	// What happened, concretely, with the specifics filled in.
	What string `json:"what"`
	// What it means — the interpretation a person cannot infer from What.
	Means string `json:"means"`
	// What to do about it. An instruction, not a restatement.
	Todo string `json:"todo"`
	// Statement is the canonical confirmation statement for an
	// authority-loosening budget act (S55 T8, the S45 PM7.1 pattern):
	// budget.set that raises or disables an enabled cap, and budget.remove,
	// are refused until the caller commits against the digest of the
	// statement the daemon rendered — the commit is bound to WHAT WAS READ,
	// so a cap that moved between the read and the commit is refused as
	// confirm-mismatch rather than silently loosened. It is omitempty: every
	// error that does not carry a statement renders byte-identically to
	// before S55 T8.
	Statement string `json:"statement,omitempty"`
	// Confirm is the digest the commit requires: the hex SHA-256 of
	// Statement, so a client completes the act in one round trip (read the
	// statement, echo the digest) and a client that saw a different row
	// cannot supply it.
	Confirm string `json:"confirm,omitempty"`
	// Fatal marks an error after which the connection is unusable, so a client
	// knows whether to retry or to reconnect. It is a field rather than a
	// convention because a client cannot guess it from the code.
	Fatal bool `json:"fatal"`
}

func (e *Error) Error() string { return fmt.Sprintf("aeon %d: %s", e.Code, e.Message) }

// WithWhat returns a copy whose What is formatted with the given arguments.
//
// The registry entries below hold the generic text; a handler fills in the
// specifics. A copy rather than a mutation, because the registry entries are
// package-level and a handler mutating one would corrupt every later use —
// the kind of bug that only shows up under concurrency.
func (e *Error) WithWhat(format string, args ...any) *Error {
	out := *e
	out.Data = e.Data
	out.Data.What = fmt.Sprintf(format, args...)
	return &out
}

// MarshalData renders the data payload, for a transport that wants it raw.
func (e *Error) MarshalData() (json.RawMessage, error) { return json.Marshal(e.Data) }

// --- The table. Every code, in one place. ------------------------------------

var (
	// ErrNotInitialized — T7: `initialize` is mandatory and first.
	ErrNotInitialized = &Error{
		Code:    -31001,
		Message: "connection is not initialized",
		Data: Data{
			What:  "a method was called on a connection that has not completed `initialize`",
			Means: "version negotiation has not happened, so the daemon does not know which protocol version to speak",
			Todo:  "call `initialize` first, with the protocol version your client needs",
			Fatal: true,
		},
	}

	// ErrProtocolVersionUnsupported — negotiation refused, in either direction.
	ErrProtocolVersionUnsupported = &Error{
		Code:    -31002,
		Message: "protocol version not supported",
		Data: Data{
			What:  "the client needs a protocol version this daemon does not speak",
			Means: "the client and the daemon are from incompatible releases; nothing further on this connection will work",
			Todo:  "upgrade whichever side is older — the message names both version numbers",
			Fatal: true,
		},
	}

	// ErrAlreadyInitialized — a second `initialize` on one connection.
	//
	// Refused rather than allowed, because re-negotiating mid-connection would
	// change the recorded version underneath live subscriptions.
	ErrAlreadyInitialized = &Error{
		Code:    -31003,
		Message: "connection is already initialized",
		Data: Data{
			What:  "`initialize` was called twice on the same connection",
			Means: "the connection already has a negotiated version; re-negotiating would change it underneath any live subscription",
			Todo:  "open a second connection if a different protocol version is needed",
			Fatal: false,
		},
	}

	// ErrInvalidFilter — O1: a wildcard form that is not a single trailing `*`.
	//
	// A NAMED error rather than a literal match, because a filter that silently
	// matches nothing is the hardest bug in this slice to diagnose from
	// outside: the client is connected, and simply never receives anything.
	ErrInvalidFilter = &Error{
		Code:    -31010,
		Message: "invalid subscription filter",
		Data: Data{
			What:  "a filter was rejected",
			Means: "`kinds` matches exactly or by a single trailing `*`; any other wildcard form is refused rather than treated as a literal, because a filter that matches nothing looks identical to a working subscription",
			Todo:  "use an exact kind (`loop.started`) or one trailing star (`loop.*`)",
			Fatal: false,
		},
	}

	// ErrUnknownSubscription — a subscription id that does not exist, or
	// belongs to another connection.
	//
	// **Named rather than silently ignored.** Ignoring it would leave the
	// client waiting for a stream that will never arrive — which looks
	// identical to a slow model, and is the one failure a timeout cannot
	// distinguish from success.
	ErrUnknownSubscription = &Error{
		Code:    -31012,
		Message: "unknown subscription",
		Data: Data{
			What:  "the subscription id given does not exist on this connection",
			Means: "either it was never created, it has already ended with its connection, or it belongs to a different connection — subscriptions are per-connection and are not shared",
			Todo:  "call `subscribe` on this connection and use the id it returns",
			Fatal: false,
		},
	}

	// ErrSubscriptionLimit — O4: the bounds, refused rather than dropped.
	ErrSubscriptionLimit = &Error{
		Code:    -31011,
		Message: "subscription limit reached",
		Data: Data{
			What:  "the subscription was refused because a limit is already reached",
			Means: "the daemon bounds subscriptions per connection and in total; an unbounded registry on a daemon meant to run for weeks is a leak with a client-shaped cause",
			Todo:  "unsubscribe what is no longer needed, or widen one filter instead of opening many subscriptions",
			Fatal: false,
		},
	}

	// --- the domain band, allocated by S3 (the model layer) -------------------

	// ErrUnknownRoute — the route id is not configured.
	//
	// The failure LISTS what is configured (O6). "route not found" on its own
	// is a support question, and at this stage the list is short enough to
	// print.
	ErrUnknownRoute = &Error{
		Code:    -31100,
		Message: "unknown route",
		Data: Data{
			What:  "no route with that id is configured",
			Means: "a route names a provider, a model and its prices; without one there is nothing to call",
			Todo:  "use one of the configured route ids, which the message lists",
			Fatal: false,
		},
	}

	// ErrAskBufferExceeded — the buffered reply outgrew its bound.
	//
	// Reported rather than silently truncated: a reply that stops mid-sentence
	// with no explanation reads as a model failure.
	ErrAskBufferExceeded = &Error{
		Code:    -31101,
		Message: "buffered reply exceeded its bound",
		Data: Data{
			What:  "the reply grew past the buffer bound for a non-streaming `ask`",
			Means: "`ask` without a subscriptionId buffers the whole reply in memory, and an unbounded buffer is a memory hole with a prompt-shaped cause",
			Todo:  "pass a `subscriptionId` and read the reply as `ask.delta` events, which has no bound",
			Fatal: false,
		},
	}

	// ErrInvalidRoute — a route a client tried to save cannot work.
	//
	// **Refused at save time rather than at call time.** A route saved against
	// a provider nobody implements is a route that fails later, on a different
	// screen, after a prompt has been typed — and the person then has no reason
	// to connect the failure to the settings they entered an hour earlier.
	ErrInvalidRoute = &Error{
		Code:    -31103,
		Message: "invalid route",
		Data: Data{
			What:  "the route could not be saved",
			Means: "a route needs an id, a model name and a provider that has an adapter; it is checked now rather than at call time, so a bad route cannot fail later on a different screen",
			Todo:  "the message says which part is wrong, and lists the registered providers where the provider is at fault",
			Fatal: false,
		},
	}

	// --- the domain band, allocated by S4 (the store) -------------------------

	// ErrStoreUnavailable — this daemon has no store wired.
	//
	// It names the two cases that can produce it, because they need different
	// actions: a daemon built without one (which this build cannot produce),
	// and a store that failed to open (which `aeon serve` reports on stdout
	// before it gets this far). A bare "no store" would send somebody looking
	// for a configuration option that does not exist.
	ErrStoreUnavailable = &Error{
		Code:    -31104,
		Message: "no store is available",
		Data: Data{
			What:  "this daemon has no store, so nothing can be read or written",
			Means: "the store opens before the daemon registers its methods, so reaching this means the open failed and the daemon is running without memory",
			Todo:  "check `aeon serve`'s first lines for the store path and the open error, and `aeon db info` for the file itself",
			Fatal: true,
		},
	}

	// ErrInvalidScope — a scoped call arrived without its scope.
	//
	// Every scoped row is keyed by `(loop_slug, component_root)` TOGETHER, and a
	// call with half of it is refused rather than defaulted: defaulting the
	// project root to the DAEMON's working directory would operate on a
	// project nobody chose, and slugs repeat across checkouts.
	ErrInvalidScope = &Error{
		Code:    -31105,
		Message: "the call needs a complete scope",
		Data: Data{
			What:  "a scoped call arrived without both a loop slug and a component root",
			Means: "the daemon serves every component on the machine, and a loop slug alone is ambiguous — `refactor` exists in two checkouts on most machines",
			Todo:  "send both `slug` and `componentRoot`; for a list, send `componentRoot` or `all: true`",
			Fatal: false,
		},
	}

	// ErrInvalidDocumentName — a document name was refused, not normalised.
	//
	// **The refusal is the feature.** A document name reaching the store comes
	// from a MODEL, and a silent clamp turns an escape attempt into a
	// successful write somewhere unexpected — which looks exactly like the
	// feature working.
	ErrInvalidDocumentName = &Error{
		Code:    -31106,
		Message: "the document name was refused",
		Data: Data{
			What:  "the document name is not one this store will accept",
			Means: "names are confined to the loop: a known name (`plan`), a top-level file, or one level under tasks/ or scratch/, with a .md, .jsonl or .txt extension. An absolute path or a `..` segment is refused rather than resolved, because resolving it would write somewhere else",
			Todo:  "the message names the specific reason; use a known name or a path like `tasks/T1.md`",
			Fatal: false,
		},
	}

	// ErrStepNotFound — the step id does not exist.
	ErrStepNotFound = &Error{
		Code:    -31107,
		Message: "no such step",
		Data: Data{
			What:  "no step with that id is in the store",
			Means: "step ids are allocated by the store and are not reused, so an id that is not there was never created or belongs to a different store",
			Todo:  "`aeon db info` prints the store path; `loop list` and `step list` show what is in it",
			Fatal: false,
		},
	}

	// ErrWorkspaceNotVisible — S19 T7: the daemon understood the start
	// request and declined it because the workspace is not a fact the
	// process can honour (relative, invisible, or not a directory). Typed
	// at the source rather than left bare: a bare error is indistinguishable
	// from infrastructure failure at the surface, and promising RETRY on a
	// refusal — where resending changes nothing — is precisely the wrong
	// advice the taxonomy exists to prevent (O5).
	ErrWorkspaceNotVisible = &Error{
		Code:    -31109,
		Message: "the workspace is not usable by the daemon",
		Data: Data{
			What:  "the given workspaceRoot is not an existing, accessible directory from the daemon's side of the machine",
			Means: "the daemon validates what a loop may touch BEFORE pinning it; a loop pinned to an unreachable root would block on its first step anyway, with less context",
			Todo:  "send an absolute path the daemon's process can see (same filesystem, existing); `aeon loop start` checks the same rule from the CLI",
			Fatal: false,
		},
	}

	// ErrUnsupportedMode — S19 T7: same class, other fact. The mode
	// vocabulary is closed; junk in a known field is a refusal, not an
	// infrastructure failure.
	ErrUnsupportedMode = &Error{
		Code:    -31110,
		Message: "unsupported loop mode",
		Data: Data{
			What:  "mode must be one of simple, standard, poc",
			Means: "modes select which lanes the blueprint's steps actually run; an unknown mode would silently run the wrong subset",
			Todo:  "omit mode for standard, or send simple/standard/poc",
			Fatal: false,
		},
	}

	// ErrRouteNotSaved — the route works now and will not survive a restart.
	//
	// # Why this is its own code rather than a provider failure
	//
	// The route IS usable: it is in memory, and the next `ask` will use it. What
	// failed is DURABILITY — a disk that is full, a permission on the state
	// directory, a store that closed. Reporting that as "the provider failed"
	// would send somebody to check their API key.
	//
	// It is not fatal, and that is the point: the person can carry on and fix
	// the disk afterwards. What they must not do is restart believing their
	// configuration was saved.
	ErrRouteNotSaved = &Error{
		Code:    -31108,
		Message: "the route was not saved",
		Data: Data{
			What:  "the route is usable in this daemon and was not written to the store",
			Means: "the route works until the daemon restarts; the configuration is in memory and the credential may not have been filed. This is a disk or permission problem, not a problem with the route or the provider",
			Todo:  "check `aeon db info` for the store path, and that the state directory is writable — then set the route again",
			Fatal: false,
		},
	}

	// --- the domain band, allocated by S5 (the step runtime) ------------------

	// ErrStepUnavailable — this daemon has no runtime wired.
	ErrStepUnavailable = &Error{
		Code:    -31200,
		Message: "no step runtime is available",
		Data: Data{
			What:  "this daemon cannot run a step: no runtime is wired",
			Means: "`aeon serve` always wires one, so reaching this means the daemon was built or started in a way that skipped it",
			Todo:  "restart the daemon; if it persists, this is a defect — report the version from `aeon version`",
			Fatal: true,
		},
	}

	// ErrNoWorkspace — a step arrived without a workspace root.
	//
	// **Named rather than defaulted** (Q10). The daemon multiplexes projects,
	// so its own working directory is not the client's — a step defaulting to
	// it would read the DAEMON's files, produce a plausible answer, and look
	// like it had worked.
	ErrNoWorkspace = &Error{
		Code:    -31201,
		Message: "the step needs a workspace root",
		Data: Data{
			What:  "no workspaceRoot was given, and there is deliberately no default",
			Means: "the daemon serves every component on the machine, so its own working directory is not yours. A step that defaulted to it would read the daemon's files and the answer would look right",
			Todo:  "send `workspaceRoot`; `aeon step` sends your working directory, and `--workspace` overrides it",
			Fatal: false,
		},
	}

	// ErrUnknownTool — the grant list names a tool that does not exist.
	//
	// Refused rather than ignored: ignoring it gives the step fewer tools than
	// its author intended, and the model works around the absence silently.
	ErrUnknownTool = &Error{
		Code:    -31202,
		Message: "the grant list names a tool that does not exist",
		Data: Data{
			What:  "one of the requested tools is not a tool this build has",
			Means: "the grant list is the only thing that decides what a step can call, so a typo in it would otherwise give the step fewer tools than intended — and the model would work around the absence without saying so",
			Todo:  "the message lists the tools that do exist; `read` is always available and `bash` needs --allow-bash",
			Fatal: false,
		},
	}

	// ErrStepNotCompiled — the step could not be assembled.
	//
	// Every one of these happens BEFORE a model call: a handler asking for a
	// tool the step was not granted, a missing output cap, a declared file that
	// does not exist. The alternative is a failure three turns in, where it
	// looks like a model working around a mistake rather than a mistake.
	ErrStepNotCompiled = &Error{
		Code:    -31203,
		Message: "the step could not be compiled",
		Data: Data{
			What:  "the step failed to assemble, before anything was called",
			Means: "construction checks the tool grants, the output cap and every declared file. Catching it here means no money was spent and the error names the cause rather than looking like a model mistake",
			Todo:  "the message names the specific failure — a missing file, an ungranted tool, or a missing output cap",
			Fatal: false,
		},
	}
	ErrNoPendingQuestion = &Error{Code: -31300, Message: "no pending question", Data: Data{
		What:  "the requested step has no unanswered question",
		Means: "there is no suspended question to attach this answer to",
		Todo:  "inspect the step and supply its pending step id", Fatal: false,
	}}
	ErrQuestionAnswered = &Error{Code: -31301, Message: "question already answered", Data: Data{
		What:  "the question already has an answer and an answer timestamp",
		Means: "the first answer may already have resumed the step, so it cannot be overwritten",
		Todo:  "inspect the recorded answer and its timestamp; start a new question if a correction is needed", Fatal: false,
	}}
	ErrResumeUnavailable = &Error{Code: -31302, Message: "step cannot resume", Data: Data{
		What:  "the saved step cannot be reconstructed or its answer could not be recorded",
		Means: "resuming without its transcript, manifest, route or tools would change the step's meaning",
		Todo:  "inspect step.show and the daemon store; the detailed error names the missing part", Fatal: false,
	}}
	// ErrNoTelemetry — S33 T8: a telemetry read named a subject that has
	// no row. The refusal names the id (TB2's rule at a new surface): an
	// empty-everything result would let a caller mistake "no such thing"
	// for "this thing did nothing", and those are different sentences.
	ErrNoTelemetry = &Error{
		Code:    -31801,
		Message: "no telemetry for that subject",
		Data: Data{
			What:  "the component, project, or step named has no telemetry row in this store",
			Means: "either it has produced no counted work since telemetry began, or the id names nothing here — telemetry is only written by the runtime's own capture seams, so it cannot be backfilled by a caller",
			Todo:  "the message names the id refused; `aeon component list`, `aeon project list` and `aeon step list` name what does exist",
			Fatal: false,
		},
	}

	ErrStepRecovering = &Error{Code: -31303, Message: "step is recovering", Data: Data{
		What:  "the requested step is still being reconciled after a daemon restart",
		Means: "its saved snapshot may change when recovery commits the outcome",
		Todo:  "watch recovery events or retry step.show after recovery.resolved", Fatal: false,
	}}
	// -31304 is deliberately unassigned: "nothing to stop" is NOT an error
	// (`step.stop`'s own doc comment says why — the same courtesy
	// `unsubscribe` gives a client racing its own teardown), so there is no
	// refusal here to carry a code.
	ErrStepNotPaused = &Error{Code: -31305, Message: "step is not paused", Data: Data{
		What: "step.continue named a step that is not suspended because a person stopped it",
		Means: "it may be running right now, done, or waiting on a real question — step.continue " +
			"only resumes the first kind; `answer` is for the second",
		Todo: "step.show names the step's status and, for a real question, its pending question", Fatal: false,
	}}

	// --- the domain band, allocated by S10 (the policy read side) ---------------

	// ErrPolicyReadRefused — the policy boundary refused the read.
	//
	// The four read-only policy methods read through the write boundary's
	// reader (List re-decodes every row; ReportEvents re-decodes every audit
	// row), and that reader REFUSES a whole answer loudly rather than
	// silently dropping the row that escaped it — dropping a row can drop a
	// Deny. The refusal is the boundary's own sentence and it travels here
	// verbatim in `what`: this code adds no interpretation, because the
	// boundary already named the fault.
	// ErrAgentRefused — the agent-services door (S14 T10) refused an ask,
	// a report or a read. Like ErrPolicyReadRefused, the refusal is the
	// boundary's own sentence and it travels here verbatim in `what`:
	// agentsvc already named the fault (AS2's card, the SkillAccess
	// verdict's S10 sentence, the chain refusal, the task-state refusal),
	// and a second interpretation of a refusal is a second truth.
	// ErrRequiresUnmet — the blueprint declares requires: and the start
	// was given nothing that satisfies it (S38 T13, OL9). THE LOOP ROW IS
	// NOT CREATED: a loop that starts against an unmet prerequisite is a
	// ladder standing on air. `what` carries the evaluator's sentence
	// verbatim (no second interpretation), and `todo` names the approved
	// candidates the workspace already holds — bounded, so the refusal
	// stays a pointer, not a dump. S39's start dialogue reads this to
	// offer a pick instead of a dead end.
	ErrRequiresUnmet = &Error{
		Code:    -31612,
		Message: "the blueprint's requires were not met",
		Data: Data{
			What:  "the blueprint declares requires: and neither the goal nor the task given satisfies it",
			Means: "the loop row was NOT created — a loop that starts against a missing prerequisite would reach its first gate with nothing standing behind it",
			Todo:  "start on one of the approved candidates named below, approve one first (goal.review / task.decide), or start a blueprint that declares no requires",
		},
	}
	// ErrStartLinkUnknown — loop.start was given a goalId or taskId that
	// resolves to nothing (S38 T13). The ids are COORDINATES, not free
	// text: a start against a coordinate nobody can name creates a loop
	// whose frame is a guess, so the row is not created and the refusal
	// names the id it could not resolve.
	ErrStartLinkUnknown = &Error{
		Code:    -31613,
		Message: "the start names a goal or task that does not exist",
		Data: Data{
			What:  "the goalId or taskId given to loop.start resolves to no record",
			Means: "the loop row was NOT created — coordinates are checked before anything is pinned, because a loop working on a goal that is not there would read a frame nobody wrote",
			Todo:  "list what exists (goal.list / task.list --manual) and start on one of those, or omit the id to start an unlinked loop",
		},
	}
	ErrAgentRefused = &Error{
		Code:    -31600,
		Message: "the agent-services boundary refused",
		Data: Data{
			What:  "the agent-services boundary refused the ask or the report",
			Means: "an agent ask is judged by doors with names — the card door (AS2), the skill enumeration, the SkillAccess verdict (S10's sentence, PL4/PL5), the caller-identity read, the chain (A13's bound) — and the refusal above is that door's own words, not a summary of them",
			Todo:  "the `what` names the door and the thing refused; `agent.discover` on the socket lists what IS invokable, and `aeon policy list --type skill_access` shows the rules the door judged with",
		},
	}
	// ErrRouteContradicted — the use-time half of a contradiction refusing
	// twice (S18 T3, O3/D6): the probe found a declared capability the
	// server does not have, and the request that would have discovered it
	// again is refused instead. The `what` is model.ContradictionSentence's
	// one sentence, rendered VERBATIM — the same words `aeon routes probe`
	// printed, so the person who probed on Monday and runs on Friday reads
	// the fact, the capability, the when and the which-model, not a
	// paraphrase of them.
	ErrRouteContradicted = &Error{
		Code:    -31610,
		Message: "the route contradicts its own probe",
		Data: Data{
			What:  "a cached probe verdict contradicts this route's declaration for the capability the call needs",
			Means: "the route DECLARES the capability (M28's declared) and a probe MEASURED that this server does not have it (contradicted) — the two are both facts and they disagree, which is the state the three states exist to surface; sending the request anyway reproduces a failure somebody already paid a request to find",
			Todo:  "`aeon routes probe <route>` re-probes (the operator's door, and the only way a verdict moves); if the server has since gained the capability the re-probe verifies and unblocks the call; otherwise `aeon routes set <route> --capabilities …` withdraws the declaration — the probe never edits it (I5)",
		},
	}
	// ErrProbeUnavailable — routes.probe was called on a daemon that cannot
	// ask anything: no model transport. A probe is a request; a daemon with
	// nobody to ask must say so rather than answer "verified" out of thin
	// air or invent a cached verdict (P31).
	ErrProbeUnavailable = &Error{
		Code:    -31611,
		Message: "this daemon cannot probe",
		Data: Data{
			What:  "routes.probe needs a model transport to send its one minimal request, and this daemon has none wired",
			Means: "the probe is not a metadata read — it pays a real request against the route (D7), so a daemon with no model layer cannot probe, and answering `declared` for everything from nothing would dress the absence up as an answer",
			Todo:  "run a daemon with the model layer wired (`aeon serve` with routes configured); `aeon routes list` shows what this daemon has",
		},
	}
	ErrPolicyReadRefused = &Error{
		Code:    -31400,
		Message: "the policy read was refused",
		Data: Data{
			What:  "the policy boundary refused to answer the read",
			Means: "the boundary will not render an answer it cannot vouch for: a policy row, acceptance record or audit row that escaped the write boundary refuses the WHOLE read loudly, rather than the confident wrong answer a silently dropped row would produce",
			Todo:  "the message carries the boundary's own words naming the row or the seam at fault; `aeon policy list` reads the same gate from a terminal, and `aeon policy events` fires the retention trim on demand",
			Fatal: false,
		},
	}

	// ErrHistoryRefused — the history read was refused (S25 T4): the id names
	// no loop or archive in scope, or a live entry was asked of the archived
	// door (or vice versa). The refusal travels verbatim across the wire,
	// never read as empty (DB19): an archived loop read as empty would lie, a
	// live one asked of the archive door would lie.
	ErrHistoryRefused = &Error{
		Code:    -31800,
		Message: "the history read was refused",
		Data: Data{
			What:  "the history read was refused",
			Means: "the id names no loop or archive in scope, or a live entry was asked of the archived door (or vice versa) — the refusal travels verbatim, never read as empty (DB19)",
			Todo:  "the message carries the id or the door at fault; `aeon history show` reads the same drill-down from a terminal",
			Fatal: false,
		},
	}

	// ErrSettingUnknown — the settings read/write named a key that is not a
	// declared setting (S44 T5, CS3/CS10): a refusal that NAMES the nearest
	// declared keys, never an empty success (an empty result would read as
	// "the setting is unset" when the truth is "that key does not exist").
	// -31810 is reserved by T0 item 3.2. The What carries the key and the
	// nearest declared keys via WithWhat (the handler fills them from the
	// store's own ErrSettingUnknown), so the three parts are the store's,
	// spoken on the wire verbatim — the handler adds no interpretation.
	ErrSettingUnknown = &Error{
		Code:    -31810,
		Message: "unknown setting",
		Data: Data{
			What:  "the setting named is not a declared setting",
			Means: "only declared settings can be read or written, so a typo cannot create a silent no-op",
			Todo:  "use one of the declared settings named below",
			Fatal: false,
		},
	}

	// ErrSettingInvalidValue — the settings write's value failed a declared
	// key's type or bounds (S44 T5, CS10): it names the key, the expected form
	// and what was given (CS10's three things), spoken verbatim from the
	// store's own ErrSettingInvalidValue. -31811 is reserved by T0 item 3.2.
	ErrSettingInvalidValue = &Error{
		Code:    -31811,
		Message: "invalid setting value",
		Data: Data{
			What:  "the setting's value was refused by its declared type or bounds",
			Means: "the value was not the form the declared type accepts, so it was refused rather than coerced",
			Todo:  "set a value of the declared type, within its bounds",
			Fatal: false,
		},
	}

	// --- the seed block, allocated by S43-extra T6 (SD10) -------------------

	// ErrSeedUnknownSet — the seed read/write named a set that is not a
	// declared seed set (S43-extra T6, SD10): a refusal that NAMES the set,
	// never an empty success (an empty result would read as "the set is
	// absent" when the truth is "that set does not exist"). -31830 is this
	// slice's block. The What carries the engine's own sentence verbatim
	// (the handler fills it from the engine's error via WithWhat), so the
	// three parts are the engine's, spoken on the wire, the handler adds no
	// interpretation of its own.
	ErrSeedUnknownSet = &Error{
		Code:    -31830,
		Message: "unknown seed set",
		Data: Data{
			What:  "the seed set named is not a declared seed set",
			Means: "only declared seed sets can be read or applied, so a typo cannot create a silent no-op",
			Todo:  "use one of the declared sets named by `seed.status`",
			Fatal: false,
		},
	}

	// ErrSeedRefused — a seed apply or restore was refused by the engine
	// (S43-extra T6, SD10): a plan-level refusal (a downgrade — a lower
	// version than applied is never silent) or an unattributed write that
	// reached the engine. It carries the engine's own sentence VERBATIM in
	// the What (via WithWhat), so the operator reads the engine's words, not
	// a summary. -31831 is this slice's block.
	ErrSeedRefused = &Error{
		Code:    -31831,
		Message: "the seed apply was refused",
		Data: Data{
			What:  "the seed engine refused the apply or restore",
			Means: "the engine's own sentence names the refusal — a downgrade (a lower version than applied is never silent), or an apply the engine would not make; nothing was written",
			Todo:  "read the engine's sentence below; a downgrade is refused, never coerced, and a diverged or removed record is re-applied only by `seed.restore`",
			Fatal: false,
		},
	}

	// ErrSeedNoActor — a seed write named no actor (S43-extra T6, SD10): the
	// actor comes from the connection (the daemon's ActorFor seam), never
	// from params, and an unresolvable or empty peer id is refused rather
	// than stamped with a placeholder. The honest absence (a storeless
	// daemon has no persons) is the refusal, not a default. -31832 is this
	// slice's block.
	ErrSeedNoActor = &Error{
		Code:    -31832,
		Message: "no actor for a seed write",
		Data: Data{
			What:  "the seed write needs a person, and this connection named none",
			Means: "the actor comes from the connection, never from params, and an unresolvable or empty peer id is refused rather than stamped with a placeholder — an unattributed seed is a silent no-op that the audit cannot name",
			Todo:  "apply or restore from a connection the daemon can attribute to a peer; the startup inert apply is the only unattributed path, and it is the daemon's own, not a caller's",
			Fatal: false,
		},
	}

	// ErrSkillsRefused — the skills install/remove refused (S65 T3): the
	// thing refused and why, carried verbatim from internal/skills'
	// *ErrRefused (DB19's discrimination — a refusal is not a disk fault,
	// and the operator reads the installer's own sentence, not a summary
	// of it). -31820 is this slice's block.
	ErrSkillsRefused = &Error{
		Code:    -31820,
		Message: "the skills operation was refused",
		Data: Data{
			What:  "the skills install or remove refused the request",
			Means: "the source is not an installable skill, a managed skill of that name exists without force, or the name lives only in the user root — the refusal carries the installer's own sentence",
			Todo:  "fix the source or add force; files under the user root are never removed here",
			Fatal: false,
		},
	}

	// ErrSkillsUnavailable — the skills door could not DO its act: the
	// disk walk failed, the managed root is unwritable, or the fetch
	// transport refused (S65 T3). The DB19 twin of ErrSkillsRefused: a
	// FAULT is not a refusal and a refusal is not a fault — and neither
	// is the store, so this door speaks its own sentence rather than
	// borrowing ErrStoreUnavailable's (measured live: a git-clone fault
	// printed store-open advice beside the git error, which is how a
	// wrong answer gets believed). -31821 is this slice's block.
	ErrSkillsUnavailable = &Error{
		Code:    -31821,
		Message: "the skills catalog could not be read or fetched",
		Data: Data{
			What:  "the skills disk walk or the install fetch failed",
			Means: "the catalog could not answer — the roots' permissions, the disk, or the network at fault; nothing was written and nothing moved",
			Todo:  "fix what the message names (root permissions, disk, or the fetch transport); skills.list still answers from the last scan",
			Fatal: false,
		},
	}

	// ErrProviderFailed — the provider refused or the stream failed.
	ErrProviderFailed = &Error{
		Code:    -31102,
		Message: "the model provider failed",
		Data: Data{
			What:  "the call to the provider did not complete",
			Means: "the provider refused the request, or the stream was cut; the daemon retried where retrying was safe and did not where a reply had already begun",
			Todo:  "the message carries the provider's own words — check the route's credential and model name first",
			Fatal: false,
		},
	}

	// --- the policy detail block, allocated by S45 T2 (PM5) -------------------

	// ErrPolicyUnknown — a policy read named an id the store does not carry
	// (S45 T2, PM5): a refusal that NAMES the id and the nearest ids (up
	// to three), never an empty success (an empty policy would read as "an
	// empty rule" when the truth is "that id does not exist"), and never
	// ErrPolicyReadRefused — that code is the boundary refusing to ANSWER a
	// row it cannot vouch for; this is a clean read answering that a name
	// is absent, which are different facts (DB19's discrimination again,
	// the reason four other policy refusals keep their own codes). The
	// What carries the id and the nearest ids via WithWhat (the handler
	// fills them from the boundary's own read). -31840 is this slice's
	// block — T0 item 3.2 reserved −31840…−31851 for S45.
	ErrPolicyUnknown = &Error{
		Code:    -31840,
		Message: "no such policy",
		Data: Data{
			What:  "the policy id named is not a policy this store carries",
			Means: "the read answered cleanly and the id is simply absent — this is not the boundary refusing a row it cannot vouch for (that refusal is the policy.read-refused code), and nothing was written",
			Todo:  "read one of the nearest ids named below, or `aeon policy list` (or `policy.list`) to see what exists — ids are authored names, not generated, so typos are the usual cause",
			Fatal: false,
		},
	}

	// ErrPolicyChangesUnavailable — the authoring audit could not be READ
	// (S45 T3, PM8): the fault twin of the audit's three honest states.
	// A store fault while reading policy_change is neither a refusal
	// (nothing was declined — that is the policy.read-refused code) nor an
	// empty audit (nothing was proven absent — that answer belongs to the
	// statement "no authoring changes recorded." and only to a Count that
	// ran). Collapsing a fault into either is how "nobody has ever authored
	// policy" gets believed beside a table full of authoring — DEF-041's
	// rule, carried from policy_event's audit onto this one. The What
	// carries the store's own sentence via WithWhat. -31841 is this slice's
	// block (T0 item 3.2 reserved −31840…−31851 for S45; ErrPolicyUnknown
	// took −31840 at T2).
	ErrPolicyChangesUnavailable = &Error{
		Code:    -31841,
		Message: "the authoring audit could not be read",
		Data: Data{
			What:  "the policy.changes read faulted below the door — the audit was neither refused nor empty, it was unreadable",
			Means: "the audit table still holds whatever it held and nothing was written or trimmed; this is a store fault, not a refusal (the read-refused code) and not the empty-table answer (that one carries a statement, this one carries the store's own sentence)",
			Todo:  "the message names the fault — fix the store and read again; until it answers, no authoring reading beats a wrong one, and an empty listing here must never be read as \"nobody has authored policy\"",
			Fatal: false,
		},
	}

	// ErrPolicyAuthoringRefused — an authoring door refused the write
	// (S45 T4, PM1): the policy write boundary's OWN refusal (PL1's
	// sentence, the closed-set refusal, the identifier gate, the per-type
	// spec refusal) or the corpus seed door's refusal, carried VERBATIM in
	// `what` exactly as policyRefusal carries the reads' boundary refusals
	// — the boundary named the fault, and anything this file added would
	// be an interpretation wearing the boundary's authority (D9). The
	// write is NOT stored and NO audit row is written for a refusal
	// (PM8's rule; TestPolicyAddWritesExactlyOneAuditRow pins the refused
	// side). This is never the code for a store fault — that is the
	// transport's internal error, DB19's discrimination. -31842 is this
	// slice's block (T0 item 3.2 reserved −31840…−31851 for S45;
	// ErrPolicyUnknown took −31840 at T2, ErrPolicyChangesUnavailable
	// took −31841 at T3).
	ErrPolicyAuthoringRefused = &Error{
		Code:    -31842,
		Message: "the authoring door refused the write",
		Data: Data{
			What:  "the policy write boundary (or the corpus seed door) refused this authoring — the `what` below is its own sentence, byte for byte",
			Means: "nothing was stored and no audit row was written — a refusal is the boundary judging the row, not the machine failing; the closed vocabulary, PL1's authorship rule and the per-type specs are enforced exactly once, at the boundary, on every door",
			Todo:  "read the boundary's sentence below — it names the value it refused and usually the form that would be accepted (PL1's refusal names the Deny/Ask that would pass); `policy.vocabulary` answers the closed sets rather than guessing them",
			Fatal: false,
		},
	}

	// ErrPolicyNoActor — an authoring door could not name the person
	// pressing the button (S45 T4, PM7.3): the actor is the kernel's peer
	// credential stamped by Table.ActorFor, and a connection whose peer
	// cannot be resolved (the daemon's uid 0 — T0 item 3.4) is REFUSED
	// rather than stamped with a placeholder. An audit row naming "user 0"
	// would dress an unattributable write in a real-looking identity, and
	// the authoring audit exists to name who widened or narrowed a guard.
	// This is the policy door's twin of ErrSeedNoActor (S43-extra T6,
	// SD10). -31843 is this slice's block.
	ErrPolicyNoActor = &Error{
		Code:    -31843,
		Message: "no actor for a policy authoring write",
		Data: Data{
			What:  "the authoring write needs a person, and this connection resolved to no peer identity (uid 0) — or the runtime wired no actor stamp at all",
			Means: "the actor comes from the connection's peer credential, never from params, and an unresolvable peer is refused rather than stamped with a placeholder — an unattributed policy write is the audit row that cannot name who changed a guard",
			Todo:  "write from a connection the daemon can attribute to a peer (the peer credential, not a claim); params never carry the actor — an actor key is refused as a shape error",
			Fatal: false,
		},
	}

	// ErrProposalDecideRefused — a proposal decision door (proposal.commit /
	// proposal.reject, S45 T5, PM7) refused the decision: a digest mismatch
	// ("the proposal changed since you read it"), a decided row (immutable
	// history — the refusal names the winner's stamp), a failed re-validation
	// or effect, a reject without its reason, or the agent_import refusal
	// that points at T10. The `what` carries the desk's refusal sentence
	// VERBATIM (policyRefusal's rule, carried onto decisions): the human
	// reading a refusal sees the reason the deciding code gave, never a
	// paraphrase of it. A store/infrastructure failure is NOT this error —
	// it travels as Dispatch's internal error, because "the disk broke" is
	// not a decision. -31844 is this slice's block (T0 item 3.2 reserved
	// −31840…−31851; ErrPolicyUnknown took −31840 at T2,
	// ErrPolicyChangesUnavailable −31841 at T3, ErrPolicyAuthoringRefused
	// −31842 and ErrPolicyNoActor −31843 at T4).
	ErrProposalDecideRefused = &Error{
		Code:    -31844,
		Message: "proposal decision refused",
		Data: Data{
			What:  "the commit or reject door refused this decision — the `what` carries the refusing sentence verbatim",
			Means: "a proposal decision is bound to what was read (the digest), decided once (a decided row is immutable history), re-validated at commit, and every refusal leaves the row exactly where it was — a refusal here changed nothing",
			Todo:  "read the sentence in `what`; if the digest moved, re-read with proposal.get and commit what you actually saw; if the row is already decided, the refusal names who decided it",
			Fatal: false,
		},
	}

	// --- the MCP management block, allocated by S46 T0, taken at T3 ----------

	// ErrMcpUnknown — an MCP management read (or, from T5, a write door)
	// named a thing that does not exist: a SERVER or an AGENT slug the
	// store does not carry (S46 T3, MG5). Never an empty success (an
	// empty grants list would read as "this agent grants nothing" when the
	// truth is "that agent does not exist") and never ErrMcpRefused —
	// that code is the domain declining an operation it could perform;
	// this is a clean read answering that a NAME is absent, which are
	// different facts (DB19's discrimination, carried from ErrPolicyUnknown
	// onto the MCP subject). The What carries the named thing AND the
	// near-names sentence via WithWhat — filled by the SEAM, because only
	// the seam knows which names exist (it reads the rows); the typed
	// error arrives through the seam's err pass-through untouched. The
	// near-names pointer turns the usual cause — a typo — into a pick.
	// -31852 opens the block T0 reserved (−31852…−31867; the S45 block
	// ended at −31844, T0 item 3.2 measured it free).
	ErrMcpUnknown = &Error{
		Code:    -31852,
		Message: "no such MCP server or agent",
		Data: Data{
			What:  "the MCP management read named a server or agent this store does not carry",
			Means: "the read answered cleanly and the name is simply absent — this is not the MCP domain declining an operation (that refusal is the mcp.refused code), and nothing was written",
			Todo:  "read one of the near names in `what`, or `mcp.servers` (or `aeon mcp list`) to see what exists — server names and agent slugs are authored, not generated, so typos are the usual cause",
			Fatal: false,
		},
	}

	// ErrMcpRefused — the MCP domain declined what it was asked (S46 T3):
	// the seam's own refusal sentence carried VERBATIM in `what`, exactly
	// as policyRefusal carries the policy reads' and ErrProposalDecide-
	// Refused carries desk's — the layer that judged the request named the
	// fault, and anything this package added would be an interpretation
	// wearing that layer's authority (D9). This is the MCP subject's OWN
	// refusal code, preferred over ErrAgentRefused for these five reads
	// (and named as such there): ErrAgentRefused is the agent-services
	// seam's code, and a client branching on "which domain refused" must
	// be able to tell mcp from agentsvc — the codes are the taxonomy. The
	// spec's reservation stands too: T5's WRITE doors (mcp.server.add/
	// update/remove/setEnabled, mcp.grant.declare/revoke, mcp.core.set,
	// mcp.importFile) arrive with the validator's sentence verbatim as
	// this code's first write-door writers, beside T3's reads. A store/
	// infrastructure failure is NOT this error — that is Dispatch's
	// internal error, DB19's discrimination. -31853 is this block.
	ErrMcpRefused = &Error{
		Code:    -31853,
		Message: "the MCP management door refused",
		Data: Data{
			What:  "the MCP management seam refused this request — the `what` below is its own sentence, byte for byte",
			Means: "nothing was written and no audit row was made — a refusal is the domain judging the request, not the machine failing; the validator, the grammar and the domain's own rules are enforced where the rows live, and this door carries their sentences, never rewrites them",
			Todo:  "read the sentence in `what` — it names what was refused and usually the form that would pass; `mcp.vocabulary` answers the closed sets and grammars rather than guessing them",
			Fatal: false,
		},
	}

	// ErrMcpNoActor — an MCP WRITE door could not name the person pressing
	// the button (S46, T5's code, defined now so the block is complete and
	// the wire contract is pinned while it is fresh): the twin of
	// ErrPolicyNoActor and ErrSeedNoActor. The actor is the kernel's peer
	// credential stamped by Table.ActorFor, and a connection whose peer
	// cannot be resolved (the daemon's uid 0) is REFUSED rather than
	// stamped with a placeholder — an audit row naming "user 0" would
	// dress an unattributable configuration write in a real-looking
	// identity, and the mcp_change audit exists to name who widened or
	// narrowed what agents may do. NO WRITER YET AT T3: the five reads are
	// unscoped by design (MG8) and need no actor; T5's doors are the
	// first. -31854 is this block.
	ErrMcpNoActor = &Error{
		Code:    -31854,
		Message: "no actor for an MCP configuration write",
		Data: Data{
			What:  "the MCP write needs a person, and this connection resolved to no peer identity (uid 0) — or the runtime wired no actor stamp at all",
			Means: "the actor comes from the connection's peer credential, never from params, and an unresolvable peer is refused rather than stamped with a placeholder — an unattributed configuration write is the audit row that cannot name who changed what agents may do",
			Todo:  "write from a connection the daemon can attribute to a peer (the peer credential, not a claim); params never carry the actor — an actor key is refused as a shape error",
			Fatal: false,
		},
	}

	// ErrMcpInUse — an MCP write door declined to remove what still has
	// use (S46, T5/T8's code, defined now for the same contract-first
	// reason as ErrMcpNoActor): removing a server that agents still
	// DECLARE, or narrowing core under a live grant, is not a silent
	// cascade — the refusal names the declaring agents (or the blocking
	// grants) so the operator's next call is informed, and nothing is
	// half-removed (MG1's candidate-config rule: the refused write leaves
	// the rows exactly where they were). NO WRITER YET AT T3. -31855 is
	// this block.
	ErrMcpInUse = &Error{
		Code:    -31855,
		Message: "the MCP thing is still in use",
		Data: Data{
			What:  "the remove or narrow was declined because rows still depend on it — the `what` names who declares it (or which grants block the change)",
			Means: "nothing was removed and no audit row was written — an MCP server that agents still declare cannot vanish underneath them, and a core set narrowed under a live grant would silently revoke a grant nobody revoked",
			Todo:  "read the dependents named in `what`; revoke the declares (mcp.grant.revoke) or set core back, then the remove passes — the refusal is the order of operations, not a permission",
			Fatal: false,
		},
	}

	// --- the domain band, allocated by S31 (the loop workspace) ----------------

	// ErrNotWired — a method registered at slice open whose handler has not
	// landed yet. The S31 method table arrives whole (one reviewable diff —
	// the B11 argument applied to the socket); handlers arrive with their
	// tasks. A stub that REFUSES by name is the honest middle between absent
	// and half-working, the rule S10's stub policy types already follow:
	// refuse rather than permit, and `what` names the task that lands it.
	ErrNotWired = &Error{
		Code:    -31500,
		Message: "this method is not wired yet",
		Data: Data{
			What:  "the method is registered but its handler has not landed",
			Means: "the S31 method table registered at slice open; handlers arrive with their tasks, and this call reached a stub",
			Todo:  "the stub's own `what` names the task that lands this handler — see docs/aeon/S31-loop-workspace/tasks/",
			Fatal: false,
		},
	}

	// S31 T7 — the terminal refusals (-31510..-31519). The four the daemon's
	// adapter maps the manager's typed refusals onto. What is NOT here:
	// delivery (there is none — output travels as connections' own
	// notifications, LW7) and policy (there is none — LW8; the door is the
	// registered root, the uid, and the socket).
	ErrTerminalUnsupported = &Error{
		Code:    -31510,
		Message: "terminals are unsupported on this OS",
		Data: Data{
			What:  "no PTY exists for this operating system",
			Means: "no shell was started — a PTY is the one thing a terminal session cannot be faked without, and a pipe is not a terminal (no job control, no tty lines, no stty)",
			Todo:  "run the daemon on a system with PTYs; the package builds everywhere, only `open` refuses",
			Fatal: false,
		},
	}
	ErrTerminalNotFound = &Error{
		Code:    -31511,
		Message: "no such terminal",
		Data: Data{
			What:  "no live terminal carries that terminalId",
			Means: "nothing was read, typed into, resized or killed — the id names a session that was never opened or has already been closed (a closed session is gone, not parked)",
			Todo:  "`terminal.list {componentRoot}` lists the live sessions with their ids and states",
			Fatal: false,
		},
	}
	ErrTerminalLimit = &Error{
		Code:    -31512,
		Message: "too many terminals for this component root",
		Data: Data{
			What:  "the component root already carries the bound's number of live terminals",
			Means: "no shell was started — the bound is what keeps a churning window from filling the daemon with shells nobody reads",
			Todo:  "close an idle session (`terminal.close`) or reuse one (`terminal.attach`); detached sessions are reaped automatically after their grace",
			Fatal: false,
		},
	}
	ErrTerminalExited = &Error{
		Code:    -31513,
		Message: "the terminal has already exited",
		Data: Data{
			What:  "the shell exited on its own; the session is kept only to serve its scrollback",
			Means: "nothing was typed and nothing will run — the kept session is history, not a shell",
			Todo:  "open a new terminal, or `terminal.attach` to read what the exited one printed",
			Fatal: false,
		},
	}

	// S31 T4 — the steer refusals (-31520..-31529; -31510..-31519 stays
	// reserved for terminals, T7). LW4's rule is that a refusal must name
	// the state found AND the method to use instead, so `what` is written
	// per-call (WithWhat) and these entries carry the shape of the answer.
	ErrSteerTargetNotRunning = &Error{
		Code:    -31520,
		Message: "the step cannot be steered",
		Data: Data{
			What:  "a steer needs a RUNNING step, and this step is not running",
			Means: "the instruction was not queued and nothing will drain it — a steer arrives at a turn boundary, and a step with no boundaries left has nowhere to arrive",
			Todo:  "the refusal names the state found and the method to use instead: `answer` behind a question, `step.continue {message}` behind a person-stop",
			Fatal: false,
		},
	}
	ErrSteerAlreadySettled = &Error{
		Code:    -31521,
		Message: "the steer can no longer change",
		Data: Data{
			What:  "the steer was already delivered or already cancelled",
			Means: "nothing was withdrawn — the row keeps the state it had, and the model may already have acted on those words",
			Todo:  "if it was delivered, its effect is in the transcript; steer again — a second instruction is how you correct a read one",
			Fatal: false,
		},
	}
	ErrSteerUnknown = &Error{
		Code:    -31522,
		Message: "no such steer",
		Data: Data{
			What:  "no steer row carries that steerId",
			Means: "nothing was cancelled — the id named no queue, so no instruction was withdrawn",
			Todo:  "`step.steers {stepId}` lists a step's rows with their ids and states",
			Fatal: false,
		},
	}

	// S31 T6 — the read-only git doors. Note what is NOT an error here:
	// `not-a-repo` and `unavailable` are STATES in the answer (D7/P31), so
	// the only refusals that leave the answer are the ones about WHO may ask.
	ErrGitUnavailable = &Error{
		Code:    -31523,
		Message: "git state cannot be read",
		Data: Data{
			What:  "this daemon has no git reader wired",
			Means: "the answer would have been a guess — `ok`, `not-a-repo` and `unavailable` each claim to KNOW something, and a daemon that cannot exec git knows nothing",
			Todo:  "run the daemon with its workspace tools (`internal/workspace` is the bounded git reader the daemon wires)",
			Fatal: false,
		},
	}
	ErrComponentRootUnknown = &Error{
		Code:    -31524,
		Message: "not a registered component root",
		Data: Data{
			What:  "the path named is not a component root this daemon registered",
			Means: "nothing was read — the socket's workspace doors open only on roots registered through `component.create`, which is what makes their answers about a component rather than about any directory on the machine",
			Todo:  "register the root first (`component.create`), or name a root already in `component.list`",
			Fatal: false,
		},
	}

	// S56 T6 (AL11, AL17) — the autonomy write's domain refusal. Every
	// refusal the pure validator (internal/autonomy) or the store's actor
	// door produces travels here with its sentence verbatim in `what`: the
	// three-part shape is filled with the generic halves, and the handler
	// fills `what` with the refusal's own words (a re-worded refusal is a
	// second truth). The refusal is NOT fatal: the connection still works,
	// the person just asked for something the authority rules do not allow.
	ErrAutonomyRefused = &Error{
		Code:    -31833,
		Message: "autonomy write refused",
		Data: Data{
			What:  "an autonomy write was refused",
			Means: "a grant is a human-authored authority record with closed rules (subject, conditions, expiry, the node it may sit on); a request outside them is refused, not silently corrected — nothing was written",
			Todo:  "the refusal names the rule it broke and the thing it broke it on; `autonomy.vocabulary` lists the closed sets a request may name",
			Fatal: false,
		},
	}

	// --- the S55 loop-control block, -31860..-31863 --------------------------
	//
	// S55 owns this block in allocation order: control, budget, watchdog,
	// spare. T4 declares the first code now (the drain's refusal); T7/T8
	// declare ErrBudgetRefused (-31861) and T9 ErrWatchdogRefused (-31862)
	// beside their own doors. -31863 is held spare so a later S55 door
	// never reaches for a number another slice has already taken.
	//
	// Re-anchored from the design's -31700..-31703 at T4 validation: the
	// design's "next free block" was never checked against the registry,
	// and the S24 queue already owns -31700 (ErrQueueRefused, queue.go).
	// -31860 is the next free multiple-of-10 sub-block after S45's
	// -31840s (its branch uses -31840..-31844) — the two never meet.

	// ErrLoopControlRefused — the loop-control plane declined the operation
	// (S55 T4, T5). Two uses, one meaning — "the loop control plane refused",
	// with Data.What naming which: a drain refusal (the daemon is shutting
	// down, so the start-family doors refuse BEFORE any row exists) and,
	// from T5 on, the stop/resume/cancel eligibility refusals the store's
	// ControlRefusal renders verbatim (DB19).
	ErrLoopControlRefused = &Error{
		Code:    -31860,
		Message: "the loop control refused the operation",
		Data: Data{
			What:  "the loop control plane refused the operation",
			Means: "the daemon understood and declined — nothing was written and nothing moved",
			Todo:  "do what the what sentence names",
			Fatal: false,
		},
	}

	// ErrBudgetRefused — the budget plane declined the operation (S55 T8).
	// The budget's one refusal voice: every store BudgetRefusal the budget
	// doors (set, remove, raise) meet rides this code, with Data.Kind the
	// machine-readable state found (duplicate, lowering, negative-cap,
	// no-dimension, unknown-subject, workspace-subject, warn-pct, or the
	// raise's no-cap / below-floor / terminal) and Data.What the person's
	// sentence (the store's Detail, rendered verbatim — DB19). T7's
	// start-time refusal still rides ErrLoopControlRefused (it is the loop
	// plane refusing a start, not the budget plane refusing a budget
	// write); this code belongs to the budget doors only.
	ErrBudgetRefused = &Error{
		Code:    -31861,
		Message: "the budget refused the operation",
		Data: Data{
			What:  "the budget plane refused the operation",
			Means: "the daemon understood and declined — no budget row was written and nothing moved",
			Todo:  "do what the what sentence names",
			Fatal: false,
		},
	}

	// ErrWatchdogRefused — the watchdog plane declined the operation (S55
	// T9). The watchdog's one refusal voice: every refusal the watchdog
	// doors (set, setEnabled, remove, and the show-of-an-unknown-rule)
	// meets rides this code, with Data.Kind the machine-readable state
	// found (unknown-rule, duplicate, unavailable-signal, confirm-required,
	// confirm-mismatch, workspace-subject or unknown-subject) and
	// Data.What the person's sentence, rendered verbatim (DB19). A
	// confirm-required refusal also carries Data.Statement and
	// Data.Confirm — the canonical statement and its digest — so the client
	// completes the loosening in one round trip (the T8 PM7.1 pattern).
	ErrWatchdogRefused = &Error{
		Code:    -31862,
		Message: "the watchdog refused the operation",
		Data: Data{
			What:  "the watchdog plane refused the operation",
			Means: "the daemon understood and declined — no rule row was written and no loop moved",
			Todo:  "do what the what sentence names",
			Fatal: false,
		},
	}

	// --- the S62 backup block, -31863..-31864 (T0's allocation, taken at T6) --
	//
	// S62 T0 measured the two contiguous codes free and allocated them:
	// -31863 for ErrBackupRefused (taken now) and -31864 held spare for the
	// export doors T8 will add. The S55 header above names -31863 as held
	// spare "so a later S55 door never reaches for a number another slice
	// has already taken" — S55 closed with T9 and has no later doors, and
	// S62 T0's allocation (the newer, slice-scoped one) takes the code
	// here; the hand-off is recorded in T6's Notes.
	//
	// Settled at S62 T8: the export doors do NOT take -31864. Every refusal
	// export.run meets — the engine's closed kinds (dir-relative,
	// dir-symlink, dir-nonempty, dir-unwritable, unknown-set, file-oversize,
	// io, cancelled) — rides ErrBackupRefused, -31863, through
	// exportRefusedError in export.go: export and backup are one family of
	// the person's doors over the database, one refusal voice for the
	// family, and the closed-set kind in Data.Kind already separates what
	// was found. -31864 stays RESERVED, unused and unallocated, for a door
	// the family has not built.
	//
	// ErrBackupRefused — the backup family's one refusal voice (S62 T6,
	// DB19, the watchdogRefusedError shape). Every refusal the backup WRITE
	// doors (run, verify, prune) meet rides this code: the closed-set
	// error_kind (disk-full, dir-unwritable, busy, lock-held,
	// newer-schema, unknown, …) in Data.Kind so a client can branch on the
	// state found, and the person's sentence, printed verbatim, in
	// Data.What. No file was published and no ledger row says success.
	ErrBackupRefused = &Error{
		Code:    -31863,
		Message: "the backup was refused",
		Data: Data{
			What:  "the backup refused the operation",
			Means: "the daemon understood and declined — the database was left exactly as it was, no partial published, and the ledger row (where one was written) says refused with its error_kind",
			Todo:  "do what the what sentence names — free space, fix the dir, wait for the lock, or update the binary",
			Fatal: false,
		},
	}

	// --- the S57 gates block, -31870..-31872 (T0's allocation, taken at T8) -
	//
	// S57 T0 measured the three contiguous codes free and allocated them
	// for the slice's three write families (the neighbours are -31869 and
	// -31873, both unused): one code per family, each the family's one
	// refusal voice (DB19, the backupRefusedError shape). The closed-set
	// kind (the store's refusal vocabulary) rides in Data.Kind so a
	// client can branch on the state found; the person's sentence,
	// printed verbatim, rides in Data.What. Nothing was written and
	// nothing runs: a refused gate write is a gate that does not exist,
	// a refused rule write is a rule that never changed, a refused mark
	// is an item that keeps its state.
	//
	// ErrVerifyRefused — the verify family's one refusal voice (S57 T8,
	// DB19). Every refusal the verify WRITE doors (add, update, archive,
	// restore, applyTemplate, try) meet rides this code: the closed-set
	// kind (actor-not-marking, name-taken, component-unknown,
	// component-archived, gate-unknown, command-invalid, requirement-
	// invalid, timeout-invalid, memory-invalid, template-unknown,
	// name-collision, no-root, …) in Data.Kind, the sentence in
	// Data.What.
	ErrVerifyRefused = &Error{
		Code:    -31870,
		Message: "the verify operation was refused",
		Data: Data{
			What:  "the gate refused the operation",
			Means: "the daemon understood and declined — the gate set is exactly as it was (no row written, no run recorded, no template copied), and the kind in data kind says which law it refused against",
			Todo:  "do what the what sentence names — fix the field it refused, clear the name it says is taken, or ask for the root it says is missing",
			Fatal: false,
		},
	}

	// ErrMergeRuleRefused — the merge rule's one refusal voice (S57 T8,
	// DB19). Every refusal the rule WRITE doors (set, clear) meet rides
	// this code: the closed-set kind (actor-not-human, component-unknown,
	// component-archived, require-dod-invalid, checks-policy-invalid,
	// no-checks-invalid, review-mode-invalid, method-invalid,
	// required-checks-invalid, aeon-review-agents-invalid, merge-rule-
	// unknown) in Data.Kind, the sentence in Data.What.
	ErrMergeRuleRefused = &Error{
		Code:    -31871,
		Message: "the merge rule operation was refused",
		Data: Data{
			What:  "the rule refused the operation",
			Means: "the daemon understood and declined — the rule row and its ledger are exactly as they were (no revision issued, no event written), and the kind in data kind says which closed set it refused",
			Todo:  "do what the what sentence names — put the field in the closed set it names, or clear the component it says has no row",
			Fatal: false,
		},
	}

	// ErrDodRefused — the DoD ledger's one refusal voice (S57 T8, DB19).
	// Every refusal the mark door meets rides this code: the closed-set
	// kind (actor-not-marking, state-invalid, item-unknown,
	// met-without-evidence, evidence-actor-mismatch) in Data.Kind, the
	// sentence in Data.What. The item keeps its state when its mark is
	// refused.
	ErrDodRefused = &Error{
		Code:    -31872,
		Message: "the DoD operation was refused",
		Data: Data{
			What:  "the ledger refused the mark",
			Means: "the daemon understood and declined — the item keeps its state and its evidence (no mark written, no updated stamp), and the kind in data kind says whose mark it is not",
			Todo:  "do what the what sentence names — name the evidence a met item needs, or ask the mark of the actor it says it belongs to",
			Fatal: false,
		},
	}

	// --- the S48 blueprint-writes block, allocated at T6 --------------------
	//
	// S48 takes -31873…-31875, the neighbours of the S57 block (T0 measured
	// them free; the comment above S57's own block says -31873 was unused).
	// One code per refusal voice of the four write doors (save, duplicate,
	// delete, revert — BP4, BP6, BP10, BP12).

	// ErrBlueprintRefused — the blueprint writes' one domain-refusal voice
	// (S48 T6, DB19, the ErrBackupRefused shape). Every refusal the write
	// service meets rides this code: the closed-set kind (diagnostics —
	// the Analyze report refused the candidate and data.what carries the
	// report JSON VERBATIM; id-mismatch — the source's id is not the
	// selected name, today's sentence; collision — createOnly met an
	// existing name, and duplicate met a taken name; from-unknown — the
	// duplicate's source has no row, named with the nearest names;
	// rev-unknown — revert named a revision that does not exist;
	// system — a shipped blueprint cannot be deleted) in Data.Kind, the
	// sentence (or the verbatim report) in Data.What. Nothing half-wrote:
	// a refusal never reaches a row, a revision or a ref.
	ErrBlueprintRefused = &Error{
		Code:    -31873,
		Message: "the blueprint write was refused",
		Data: Data{
			What:  "the write service refused the candidate or the act",
			Means: "the daemon understood and declined — the row, its revision history and its derived refs are exactly as they were; when data.kind is diagnostics the what field IS the Analyze report, byte-verbatim, and the same source validated through blueprint.validate answers it identically",
			Todo:  "do what the what says — fix the named diagnostics, name the id the source declares, pick an unused name, name an existing revision, or restore/duplicate the shipped blueprint instead of deleting it",
			Fatal: false,
		},
	}

	// ErrBlueprintNoActor — a blueprint write whose peer is unresolvable
	// (S48 T6, BP12, the ErrMcpNoActor family): the actor is the
	// connection's person (the daemon's ActorFor seam, the kernel's peer
	// credential), and when it cannot be named the door refuses BEFORE
	// anything is read or written. An anonymous row, an anonymous
	// revision — the audit gap this code exists to close.
	ErrBlueprintNoActor = &Error{
		Code:    -31874,
		Message: "the blueprint write has no resolvable person behind it",
		Data: Data{
			What:  "the connection carries no nameable person (no identity seam, a blank id, or the uid 0 a connection the transport never recorded reads as)",
			Means: "a blueprint write is a person's door — the actor on the row, the revision and the audit is the peer's, never a guess and never a param (S48 BP12); nothing was read as them and nothing was written",
			Todo:  "speak from a connection the daemon can attribute — the socket's peer credential is the identity",
			Fatal: false,
		},
	}

	// ErrBlueprintInUse — the delete refusal while automation still names
	// the blueprint (S48 T6, BP10): every LIVE reference is NAMED in
	// data.what — each trigger_schedule, each event_trigger, each queue
	// item still in a live status. Loops already started never refuse
	// (they carry pinned content) and are reported as a count on success.
	ErrBlueprintInUse = &Error{
		Code:    -31875,
		Message: "the blueprint is still referenced by live automation",
		Data: Data{
			What:  "schedules, event triggers or live queue items still name this blueprint",
			Means: "deleting it would strand automation that can still fire — every refuser is named in the what field (id and kind, and a queue item's status) so the operator closes exactly those doors first; started loops are never in the list (they keep their pinned content)",
			Todo:  "pause or re-point every named schedule/trigger and drain or cancel every named queue item, then delete again",
			Fatal: false,
		},
	}

	// ErrBlueprintUnknown — the READ refusal when no row carries the name
	// AND nothing speaks of it either (S48 T7, BP7): a history query for a
	// name that never existed (a DELETED blueprint's history stays
	// readable — this refusal is for names that never had a revision).
	// data.what names the nearest names, the same voice the write doors
	// use for from-unknown.
	ErrBlueprintUnknown = &Error{
		Code:    -31876,
		Message: "no blueprint by that name — and no history of one either",
		Data: Data{
			What:  "the named blueprint has no row and no revisions",
			Means: "usage, history, revision, diff and lint answer about a NAME the store knows — a deleted blueprint's history still counts as known (the revision table is append-only); this is the never-existed case",
			Todo:  "check the name against blueprint.list; the what field names the nearest names",
			Fatal: false,
		},
	}

	// --- the S59 execution-environment block, allocated by S59 T0, taken at T6 --
	//
	// S59 owns -31880 (and -31881 in reserve). T0 measured both free repo-wide at
	// the time of allocation; the neighbours are S48's -31876 and S47's -31911.
	// One voice for the whole plane (DB19): every refusal the environment or
	// secret-reference service meets — a validator's report, a stale revision, a
	// bind of an unresolvable reference, an unbind of the workspace floor, a
	// teardown with no registered backend — rides THIS code with the sentence
	// that was actually spoken in `data.what`. One code, one voice: a client
	// branches on the plane, and the `kind` the service puts in `data.kind` says
	// which refusal it was, so no second error taxonomy exists to keep in step.
	//
	// ErrEnvironmentRefused — the execution-environment plane's domain refusal
	// (S59 T6, EV18). `what` is the validator's or the store's own words, verbatim
	// — the same sentence `environment.vocabulary`'s honesty line promises a
	// person will get instead of a guess.
	ErrEnvironmentRefused = &Error{
		Code:    -31880,
		Message: "the environment or secret-reference operation was refused",
		Data: Data{
			What:  "the plane understood the request and declined it — the what field is the validator's or the store's own sentence",
			Means: "nothing changed: no row, no binding, no provision and no audit row was written. An environment write is a human door, so a refusal is the plane doing its job — it is what commands may see and reach, and it does not move on a request that cannot be justified",
			Todo:  "do what the what says — fix the named spec member, re-read the row and retry with the revision you saw, check the secret reference with secretref.check before binding it, bind a different environment, or register the backend this build is missing",
			Fatal: false,
		},
	}

	// --- the S47 tools-catalog block, allocated by S47 T0, taken at T5 ------
	//
	// S47 owns -31911…-31922 (T0 item: twelve contiguous, measured free — the
	// neighbours are -31900 in roadmap.go and -31910 in triage.go, and nothing
	// in -31911..-31949 is used anywhere in the tree). T5 takes the first two,
	// both on the READ side; the rest stay reserved so a later S47 task never
	// reaches for a number another slice has already taken.
	//
	// Both are the `ErrMcpUnknown`/`ErrPolicyUnknown` family: a clean read
	// answering that a NAME is absent, which is a different fact from a domain
	// declining an operation it could have performed (DB19's discrimination).
	// The `what` carries the named thing AND the nearest-names sentence, and
	// the SEAM fills it — only the seam reads the rows and the registry, so
	// only it knows which names exist and which are near. The handler passes a
	// typed error through untouched (the mcpCall discipline), because a
	// paraphrase here would be a second authority over a fact the seam owns.

	// ErrToolUnknown — `tools.get` named a tool that does not exist (S47 T5,
	// TC10). Never a bare not-found and never an empty entry: the refusal names
	// the nearest tools, because the usual cause is a typo or a hidden alias
	// spelled from memory, and a listing the caller can pick from turns both
	// into one round trip instead of a guessing game. `mcp.unknown`'s shape
	// carried onto the tool subject (the two subjects are different facts
	// because their repairs are different: one says `mcp.servers`, this one
	// says `tools.list`).
	ErrToolUnknown = &Error{
		Code:    -31911,
		Message: "no such tool",
		Data: Data{
			What:  "the tools read named a tool this build does not carry",
			Means: "the read answered cleanly and the name is simply absent — nothing was written, and this is not the catalog declining to answer (it answers everything it has)",
			Todo:  "pick one of the near names in `what`, or `tools.list` (or `aeon tools list`) to see what exists — tool names are authored in code, not generated, so a spelling from memory is the usual cause",
			Fatal: false,
		},
	}

	// ErrToolAgentUnknown — `tools.byAgent` named an agent the roster does not
	// carry (S47 T5, TC10). Never an empty tool list: an empty view would read
	// as "this agent may call nothing" when the truth is "that agent does not
	// exist", which is the plausible wrong answer a grants screen exists to
	// prevent (S46's `mcp.grants` refused the same way for the same reason, and
	// `tools.grants`' own known-agents sentence is the shape this reuses — the
	// two doors must not disagree about who exists).
	ErrToolAgentUnknown = &Error{
		Code:    -31912,
		Message: "no such agent",
		Data: Data{
			What:  "the tools read named an agent this store does not carry",
			Means: "the read answered cleanly and the agent is simply absent — no grant row exists for it, so no tool view can be built, and nothing was written",
			Todo:  "pick one of the known agent slugs in `what`, or `agent.list` (or `aeon agent list`) to see who exists — slugs are authored, so a typo is the usual cause",
			Fatal: false,
		},
	}

	// ErrSurfaceUnknown — `surface.get` named a method this daemon does not
	// register (S47 T6, TC6). The same discipline as the two tools refusals: a
	// method name a person typed wrong is answered with the nearest names that
	// DO exist, because "no such method" over a surface of nearly three hundred
	// methods is a refusal nobody can act on. It is not a -32601: the dispatcher
	// knew the method it was asked for (`surface.get`); it is the named subject
	// that is absent, which is a fact about the surface, not about the protocol.
	ErrSurfaceUnknown = &Error{
		Code:    -31913,
		Message: "no such method",
		Data: Data{
			What:  "the surface read named a method this daemon does not register",
			Means: "the classification answered cleanly and the method is simply absent from this build — a conditional family whose seam was never wired is ABSENT here, which is the answer and not an error",
			Todo:  "read the names in `what` (the nearest registered methods), or call `surface.list` to see the whole surface and the doors that reach each method",
			Fatal: false,
		},
	}

	// --- the S58 supervision block, allocated by S58 T0, opened at T2 -----
	//
	// S58 owns -31923...-31929 (seven, one per refusal family:
	// channel-refused, secret-missing, class-template, inbox-scope,
	// snooze-horizon, delivery-bad, test-failed) with -31930 held in
	// reserve. T0 measured -31914...-31921 as the free span, but S47's
	// block COMMENT reserves -31914...-31922 for its own later tasks — two
	// slices reaching for one reserved span is the collision rule exists
	// against, so S58 sits after S47's reservation instead (measured free
	// at 2026-11-03: -31914..-31921 taken ONLY by S47 comments, -31923..
	// -31948 used by nothing; the neighbours stay -31922 and -31949). The
	// T5 landed the remaining four (channel-refused, secret-missing,
	// delivery-bad, test-failed): the block is full, -31930 stays reserve.

	// ErrInboxScope is `inbox.list`/`inbox.summary` naming a rootId no live
	// component carries (S58 T2, SV1). An empty answer to a typo'd id is how
	// an inbox starts lying about "nothing needs you" — the refusal says the
	// id is absent, and `what` carries the id itself.
	ErrInboxScope = &Error{
		Code:    -31926,
		Message: "unknown inbox scope",
		Data: Data{
			What:  "the inbox scope named a component this store does not carry live",
			Means: "an empty inbox would have looked like 'nothing needs you' — this refusal says the scope itself was the unknown thing, so an empty list can never be a lie about a typo",
			Todo:  "check the id against `component.list` (archived components are out of scope like unknown ones), or call inbox.list/inbox.summary with no rootId to see every tree",
			Fatal: false,
		},
	}

	// S49 owns -31890…-31893 (T0's measurement: free across -31877…-31899
	// except S59 T0's reservations at -31880/-31881; the neighbours stay
	// -31876 ErrBlueprintUnknown and the -31900 block). T3 declares the two
	// READ refusals; T4 will land -31892/-31893 with the write side.

	// ErrAutomationUnknown is an automation.show/fires/health/preview/dryrun
	// id no family carries (S49 T3, AN5). The id is a name, and a name that
	// misses answers with the names that nearly matched — an empty object
	// would read as "it exists and says nothing".
	ErrAutomationUnknown = &Error{
		Code:    -31890,
		Message: "unknown automation",
		Data: Data{
			What:  "no automation with that source/id exists — `what` names the nearest ids this store carries",
			Means: "the id was the unknown thing, not the answer — an empty result here would be a confident wrong answer about a typo",
			Todo:  "run `automation.list` (add archived:\"include\" to see the archive too) and use one of the ids it prints",
			Fatal: false,
		},
	}

	// ErrAutomationPreview is a preview the CLOCK or the GRAMMAR refused
	// (S49 T3, AN8): a zone that will not load, an expression the engine's
	// loader declines, a rule whose values the validator rejects. The
	// refusal sentence rides in `what` VERBATIM — the same words the write
	// door would answer — because a paraphrase is a second authority over
	// a fact the validator owns.
	ErrAutomationPreview = &Error{
		Code:    -31891,
		Message: "automation preview refused",
		Data: Data{
			What:  "the engine's own validator refused this preview; its sentence rides verbatim in this `what`",
			Means: "the preview runs the SAME loader the clock and the write doors run: what it refuses here it would refuse at save and at fire, so the preview is honest by being the engine",
			Todo:  "fix the expression, the zone, or the rule per the sentence in `what`; `automation.vocabulary` prints the grammar's rules and the zones the loader accepts",
			Fatal: false,
		},
	}

	// ErrAutomationNoActor is an automation WRITE whose connection carries
	// no person to attribute it to (S49 T4, AU1): the identity seam is
	// unwired or the peer id is blank. Nothing was written — the audit
	// cannot name an actor, and an automation nobody signed is exactly
	// the row AN3 exists to make impossible.
	ErrAutomationNoActor = &Error{
		Code:    -31892,
		Message: "no person on this connection",
		Data: Data{
			What:  "the automation write found no person to attribute it to — no identity seam is wired, or the peer id is blank",
			Means: "automations are written by a person at the 0600 endpoint (P26), or by the CLI dialing that same socket as the person; with no id there is no creator stamp and no audit actor, so AN3 refuses the write entirely",
			Todo:  "run `aeon automation` from the person's own session (the socket's peer credentials ARE the identity); a programmatic caller is not a person and is refused by name instead",
			Fatal: false,
		},
	}

	// ErrAutomationRefused is the automation write plane's carrier for the
	// ENGINE'S OWN refusal sentences (S49 T4, AU1/AN2): "triggers are
	// written by a person; this actor is \"agent\"" rides in `what` VERBATIM,
	// beside the door-specific refusals (enabling an archived trigger, a
	// write the validator's grammar declines). A paraphrase would be a
	// second authority over a sentence the door owns.
	ErrAutomationRefused = &Error{
		Code:    -31893,
		Message: "automation write refused",
		Data: Data{
			What:  "the write was refused at the human door; the refusal sentence rides verbatim in this `what`",
			Means: "AU1: no non-person actor writes an automation, and the store refuses the same actor before SQL — the door and the store speak one sentence, and agents get proposals (automation_disable), never writes",
			Todo:  "have the person at the endpoint make the write; for an agent, propose instead — the proposal path narrows, it never opens",
			Fatal: false,
		},
	}

	// ErrSnoozeHorizon is a snooze whose `until` is not inside the allowed
	// window — not in the future, or past now + 7 days (S58 T3, SV-D11,
	// attention.MaxSnooze). A past instant would hide an item forever and
	// an unbounded future makes the inbox a deletion door; a silence longer
	// than a week is a channel schedule (T5), not an invisible problem.
	ErrSnoozeHorizon = &Error{
		Code:    -31927,
		Message: "snooze outside the horizon",
		Data: Data{
			What:  "the snooze instant is not a moment inside the allowed window (future, at most seven days out)",
			Means: "a snooze that never wakes is a deleted problem, and the inbox must stay the truth; expiry is computed at read, so a snooze past its instant is live again with no job and no restart",
			Todo:  "pick an `until` between now and seven days ahead, or use inbox.ack for 'seen' and a channel schedule (notify) for a longer silence",
			Fatal: false,
		},
	}

	// ErrNotifyChannelRefused is the channel plane's one refusal family
	// (S58 T5, SV7): the url shape, a write to an archived or absent
	// channel, an update that would break the https rule, the subscription
	// filter's closed vocabularies. `what` carries the specific sentence
	// the validator produced; the family is one because every member says
	// the same thing about a different field — this channel as written may
	// not exist or may not send.
	ErrNotifyChannelRefused = &Error{
		Code:    -31923,
		Message: "channel refused",
		Data: Data{
			What:  "the channel write was refused — the reason names the field (url scheme, userinfo, size caps, archived state, subscription vocabulary)",
			Means: "a channel is standing authority to send machine facts off this machine, so its shape is refused before any row exists; nothing half-valid is persisted, and the refusal carries the validator's own sentence",
			Todo:  "fix the named field (https url without userinfo, name <= 80 bytes, subscriptions inside the closed vocabularies) and send the write again",
			Fatal: false,
		},
	}

	// ErrSecretMissing is enabling a channel whose signing secret does not
	// resolve in the secrets store (S58 T5, SV7): an enabled-but-unsigned
	// webhook is a forgery with a green light, so enable is the gate.
	ErrSecretMissing = &Error{
		Code:    -31924,
		Message: "channel secret missing",
		Data: Data{
			What:  "the channel has no secret — the reference resolves to nothing in the secrets store",
			Means: "every delivery is signed with HMAC-SHA256 so the receiver can tell Aeon from a forger; an enabled channel without a secret could only send unsigned bodies, which is why enabling is where the absence surfaces",
			Todo:  "write the secret first: notify.channel.update with secretFile naming a 0600 file on the daemon host (aeon channel set-secret --secret-file PATH from the CLI), then enable",
			Fatal: false,
		},
	}

	// ErrDeliveryBad is the delivery log's read refusing a question it
	// cannot answer honestly (S58 T5, SV12): a channel that does not
	// exist, or a limit outside the sane window. The log itself never
	// refuses — it is append-only and always complete for a known channel.
	ErrDeliveryBad = &Error{
		Code:    -31928,
		Message: "delivery query refused",
		Data: Data{
			What:  "the delivery listing names a channel this store does not carry, or a limit outside 1..500",
			Means: "an empty list for a typo'd channel would read as 'this channel delivered nothing' — a lie the delivery log exists to make impossible; the refusal says the query itself was the unknown thing",
			Todo:  "check the id against notify.channel.list (archived channels keep their log — includeArchived covers them)",
			Fatal: false,
		},
	}

	// ErrTestFailed reports that a channel test ran and the delivery did
	// not land (S58 T5, SV7). It is NOT a refusal to run — the attempt
	// happened, is logged like any delivery, and this error is its honest
	// mirror: the outcome, error kind and status ride in `what`.
	ErrTestFailed = &Error{
		Code:    -31929,
		Message: "channel test failed",
		Data: Data{
			What:  "the test envelope was sent and did not land — the outcome and error kind ride in this sentence",
			Means: "a test is a real delivery, logged like any other; its failure is a fact about the receiver or the network, recorded honestly instead of hidden behind a green write",
			Todo:  "read the error kind (notify.delivery.list shows every attempt): a refused-address means the SSRF guard refused the destination, a status means the receiver said no, transport/timeout means the network said no",
			Fatal: false,
		},
	}

	// ErrClassTemplate is an event-class template that reaches outside the
	// closed placeholder set or past its length cap (S58 T4, SV4/SV6). The
	// refusal is the SECURITY door for user text: a template may name ids
	// and enums only — no prompt, goal, name, message or path is
	// placeable anywhere a template can read, and this refusal is where
	// an author learns that before a render could leak anything.
	ErrClassTemplate = &Error{
		Code:    -31925,
		Message: "event class template refused",
		Data: Data{
			What:  "the template names a placeholder outside the closed set {rootSlug, loopSlug, reasonKind, severity, count} or exceeds its byte cap (120 title / 240 body)",
			Means: "a hint can carry ids and enums and nothing else: prompts, goals, names and messages never reach a template, so an outbound notification cannot be made to quote a person's work (SV6)",
			Todo:  "edit the template to use only the closed placeholders (notify.vocabulary lists them), or leave the shipped template as it is",
			Fatal: false,
		},
	}
)

// ErrorRegistry is every Aeon error code, in allocation order.
//
// D9 asserts over this WHOLE table rather than a sample, so a code added at S9
// without its three parts fails a test written at S2. That is the point: the
// shape is enforced by enumeration, not by review.
func ErrorRegistry() []*Error {
	return []*Error{
		ErrNotInitialized,
		ErrProtocolVersionUnsupported,
		ErrAlreadyInitialized,
		ErrInvalidFilter,
		ErrUnknownSubscription,
		ErrSubscriptionLimit,
		ErrUnknownRoute,
		ErrInvalidRoute,
		ErrAskBufferExceeded,
		ErrProviderFailed,
		ErrStoreUnavailable,
		ErrNoTelemetry,
		ErrInvalidScope,
		ErrInvalidDocumentName,
		ErrStepNotFound,
		ErrWorkspaceNotVisible,
		ErrUnsupportedMode,
		ErrRouteNotSaved,
		ErrStepUnavailable,
		ErrNoWorkspace,
		ErrUnknownTool,
		ErrStepNotCompiled,
		ErrNoPendingQuestion,
		ErrQuestionAnswered,
		ErrResumeUnavailable,
		ErrStepRecovering,
		ErrStepNotPaused,
		ErrAgentRefused,
		ErrRequiresUnmet,
		ErrStartLinkUnknown,
		ErrPolicyReadRefused,
		ErrHistoryRefused,
		ErrNotWired,
		ErrSteerTargetNotRunning,
		ErrSteerAlreadySettled,
		ErrSteerUnknown,
		ErrGitUnavailable,
		ErrComponentRootUnknown,
		ErrTerminalUnsupported,
		ErrTerminalNotFound,
		ErrTerminalLimit,
		ErrTerminalExited,
		// S24 T7: the queue's state refusal (declared beside its handlers
		// in queue.go; registered here, where every code is catalogued).
		ErrQueueRefused,
		// S65 T3: the skills install/remove refusal (declared above in this
		// file; its conformance fixture lives in conformance/skills/).
		ErrSkillsRefused,
		// S65 T3: the skills fault twin (disk/transport), declared beside
		// the refusal and registered in the same breath.
		ErrSkillsUnavailable,
		// S35 T3: the proposal-create refusal (declared beside its handler
		// in proposals.go; registered here, same convention).
		ErrProposalRefused,
		// S26 T5: the roadmap's state refusal (declared beside its handlers
		// in roadmap.go; registered here, same convention).
		ErrRoadmapRefused,
		// S50 T1 (RM2): the roadmap write refused for having no nameable
		// author (declared beside its handlers in roadmap.go).
		ErrRoadmapNoActor,
		// S51 T1 (TM2): the triage write refused for having no nameable
		// author (declared beside its handlers in triage.go). S51's block is
		// -31933...-31947 (T0's measurement: the neighbours stay -31932 and
		// -31948; the rest of the block is reserve for the slice's later
		// refusal families).
		ErrTriageNoActor,
		ErrTriageUnknown,
		// S52 T2 (LM2): a retract, restore or history that named an id with
		// no row — the named absence, declared beside its handlers in
		// learnings.go beside its sibling refusal (the T0 record's "no
		// ErrLearningUnknown exists" is stale from here: the T2 task named
		// it and the door needed the distinct answer).
		ErrLearningUnknown,
		// S54 T1: the platform operation doors' no-actor refusal (declared
		// beside its helpers in platform_door.go, registered here, same
		// convention; the first code of the S54 block -31956..-31961, re-anchored in build — S60's merge-queue block landed on main first and owns -31952...-31955, its comment measuring -31956 upward free; T0's drift list anticipated the race). Its
		// conformance fixture lives in conformance/errors/.
		ErrPlatformNoActor,
		// S54 T2: the platform plane's not-found (-31957, second code of
		// the block) — declared beside its handlers in platform.go; today
		// retrieval.reindexPlan's unknown corpus reaches it.
		ErrPlatformUnknown,
		// S44 T5: the settings' two domain refusals (declared above in this
		// file, beside the other errors; registered here, same convention).
		ErrSettingUnknown,
		ErrSettingInvalidValue,
		// S43-extra T6: the seed's three domain refusals (declared above in
		// this file, beside the other errors; registered here, same
		// convention).
		ErrSeedUnknownSet,
		ErrSeedRefused,
		ErrSeedNoActor,
		// S45 T2: the policy detail read's refusal (declared above in this
		// file, beside the other errors; registered here, same convention).
		ErrPolicyUnknown,
		// S45 T3: the authoring audit read's fault twin (declared above in
		// this file; registered here, same convention).
		ErrPolicyChangesUnavailable,
		// S45 T4: the authoring doors' two refusals (declared above in this
		// file; registered here, same convention). -31842 carries the
		// boundary's sentence verbatim; -31843 is the no-peer refusal.
		ErrPolicyAuthoringRefused,
		ErrPolicyNoActor,
		// S45 T5: the decision doors' refusal (declared above in this file;
		// registered here, same convention). -31844 carries the desk's
		// refusal sentence verbatim in `what`.
		ErrProposalDecideRefused,
		// S46 T3: the MCP management block (declared above in this file;
		// registered here, same convention). -31852 is the typed named-
		// thing refusal whose near-names sentence the seam fills via the
		// err pass-through; -31853 carries the MCP domain's refusal
		// sentence verbatim (T3's five reads use it, and T5's write doors
		// are named as its first writers in its own doc); -31854 and
		// -31855 are T5/T8's codes, registered now so the reserved block
		// is complete and the wire contract (with its conformance
		// fixtures) is pinned while it is fresh — they have no writer
		// yet, which is said out loud in their own comments rather than
		// hidden by omission.
		ErrMcpUnknown,
		ErrMcpRefused,
		ErrMcpNoActor,
		ErrMcpInUse,
		// S56 T6: the autonomy write's domain refusal (declared above in
		// this file, beside the other errors; registered here, same
		// convention).
		ErrAutonomyRefused,
		// S55 T4: the loop-control refusal (declared above in this file, in
		// the S55 block -31860..-31863; registered here, same convention).
		// Its conformance fixture lives in conformance/blueprint/.
		ErrLoopControlRefused,
		// S55 T8: the budget refusal (the block's second code, beside its
		// doors; registered here, same convention). Its conformance
		// fixtures live in conformance/store/ and conformance/loop/.
		ErrBudgetRefused,
		ErrWatchdogRefused,
		// S62 T6: the backup family's refusal (declared above in this file,
		// in the S62 block -31863..-31864; registered here, same
		// convention). Its conformance fixtures live in conformance/backup/.
		ErrBackupRefused,
		// S57 T8: the three gate-plane family refusals (declared above in
		// this file, in the S57 block -31870..-31872; registered here,
		// same convention). Their conformance fixtures live in
		// conformance/verify/, conformance/mergerule/ and conformance/dod/.
		ErrVerifyRefused,
		ErrMergeRuleRefused,
		ErrDodRefused,
		// S48 T6: the blueprint writes' three refusal voices (declared above
		// in this file, in the S48 block -31873..-31875; registered here,
		// same convention). Their conformance fixtures live in
		// conformance/blueprint/.
		ErrBlueprintRefused,
		ErrBlueprintNoActor,
		ErrBlueprintInUse,
		// S48 T7: the read doors' never-existed refusal (-31876).
		ErrBlueprintUnknown,
		// S47 T5: the two tools-catalog named-thing refusions (the block T0
		// reserved at -31911…-31922; declared above in this file, registered
		// here, same convention). Their conformance fixtures live in
		// conformance/tools/.
		ErrToolUnknown,
		ErrToolAgentUnknown,
		ErrSurfaceUnknown,
		ErrInboxScope,
		// S49 T3: the unified automation reads' two refusals (the block T0
		// reserved at -31890…-31893; declared above in this file, registered
		// here, same convention). Their conformance fixtures live in
		// conformance/automation-reads/.
		ErrAutomationUnknown,
		ErrAutomationPreview,
		// S49 T4: the write door's two refusals (the same reserved block;
		// fixtures in conformance/automation-write/).
		ErrAutomationNoActor,
		ErrAutomationRefused,
		ErrSnoozeHorizon,
		ErrClassTemplate,
		ErrNotifyChannelRefused,
		ErrSecretMissing,
		ErrDeliveryBad,
		ErrTestFailed,
		// S59 T6: the execution-environment plane's one refusal (declared
		// above in this file, in the S59 block -31880..-31881; registered
		// here, same convention). Its conformance fixtures live in
		// conformance/environment/ and conformance/secretref/.
		ErrEnvironmentRefused,
		// S60 T8: the merge-queue plane's four refusals (declared beside
		// their handlers in mergequeue.go, in the S60 block -31952...-31955
		// — the block MOVED from T0's measured -31941..-31944 when S51's
		// triage comment landed claiming -31933...-31947 as its reserve;
		// two slices reaching for one reserved span is what the collision
		// rule exists against, so S60 sits after the neighbours, measured
		// free again, S58's precedent). Registered here, same convention;
		// conformance fixtures live in conformance/mergequeue/.
		ErrMergeQueueRefused,
		ErrMergeQueueNotFound,
		ErrMergeQueueBaseUnknown,
		ErrMergeQueueConflictState,
		// S63 T5: the outcome-learning plane's two refusals, in S63 T0's
		// measured block -31962...-31963 (-31956...-31961 is S54's, and
		// -31962 upward was measured free at T0 and free again here).
		// One refusal voice plus one unavailability voice for three
		// planes of READS and the plane's one human-door write; the
		// conformance fixtures live in conformance/scorecard/,
		// conformance/retro/ and conformance/digest/.
		ErrScorecardRefused,
		ErrRetroUnavailable,
		// S61 T7: the goal-intake plane's two refusal voices (declared
		// below, registered here, same convention). -31964 and -31965
		// come from the block T0 measured free at -31962-upward; S63
		// landed its two first, so S61's allocation starts at -31964 and
		// the rest of the block (-31966...-31969) stays measured-free
		// reserve behind this plane.
		ErrCharterRefused,
		ErrIntakeRefused,

		// S73 T4 (step 3), SPENT BY T3: the agent-management write's
		// no-actor refusal (declared beside its handlers in
		// agent_management.go; registered here, same convention). The
		// spend landed a task early because T3's audited store doors left
		// cmd/aeon no placeholder actor to compile against — recorded in
		// docs/aeon/S73-agent-crud-page/tasks/T3.md's Notes, not here only.
		// S73's block is -31970...-31972 — measured 2026-11-06 the deepest
		// code registered in the table is -31965, and S61's own comment
		// reserves -31966...-31969 as its reserve, so this slice starts one
		// past the reservation rather than spending inside it. The first
		// code is spent; -31971 and -31972 stay unspent reserve for the
		// plane's later refusals. Its conformance fixture lives in
		// conformance/errors/.
		ErrAgentNoActor,

		// S27 T7 (2026-11-05): the scheduler plane's two voices join the
		// registry the same way the scorecard's did — the S27 block is
		// -31935...-31938, T7 spends the first two, and the conformance
		// fixtures live in conformance/scheduler/.
		ErrSchedulerRefused,
		ErrSchedulerDisabled,	}
}

// S63 owns -31962...-31963, the block S63 T0 measured free (the plane's
// Do-NOT list and its guards are what the codes refuse). They arrive
// beside the table, not inside a plane file, because the plane speaks
// with TWO voices across three files — scorecard.go, retro.go and
// digest.go all reach for them — and a shared voice belongs in the one
// registry the header comment demands.
var (
	// ErrScorecardRefused is the plane's refusal voice: the door
	// understood the request and declined — unknown subject, a bad
	// window, the plane's one write aimed at a caller who is not the
	// person, or a backfill asked to write without `yes`. `what` carries
	// the service's or the validator's own sentence, never a paraphrase
	// (DB19).
	ErrScorecardRefused = &Error{
		Code:    -31962,
		Message: "the scorecard/retro request was refused",
		Data: Data{
			What:  "the plane understood the request and declined it — the what field is the validator's or the service's own sentence",
			Means: "nothing was read or written: no card answered, no fact row moved, no backfill ran. Every metric the door WOULD have carried stays uncomputed rather than guessed",
			Todo:  "do what the what says — name a real subject (retro.list and the component tree hand out ids), widen the window, or resend the backfill with yes:true after reading its preview",
			Fatal: false,
		},
	}

	// ErrRetroUnavailable is the absence voice: the facts this plane
	// reads cannot exist here — no store is wired (V13's shadow), or the
	// recorder's own state makes the answer impossible to give honestly.
	// It is not "zero rows": zero rows answer as rows, with a sentence.
	ErrRetroUnavailable = &Error{
		Code:    -31963,
		Message: "outcome facts are unavailable in this daemon",
		Data: Data{
			What:  "this daemon cannot honestly answer the question — no store is wired, so no outcome facts exist and none could be written",
			Means: "nothing was read or written. An unavailable answer is the plane refusing to dress absence up as zero: zero loops is a measurement, no store is not",
			Todo:  "start the daemon with a store (the facts begin accumulating the moment the recorder is alive); retro.status is the door that says whether recording is running",
			Fatal: false,
		},
	}

	// S27-continuous owns -31935…-31938 (four contiguous, measured free at
	// T0 and re-measured at T7: -31934 sits below and -31948 above, and
	// nothing in -31935…-31947 is spoken anywhere in the tree). T7 spends
	// the first two; -31937 and -31938 stay reserved for the scheduler's
	// later doors (the S47/S59 convention: the slice owns its block, a
	// task spends only what it says). Reads that need a store reuse
	// ErrStoreUnavailable — absence is one voice protocol-wide.

	// ErrSchedulerRefused is the scheduler door's refusal: the door
	// understood the request and declined it, and the `what` carries the
	// validator's or the store's own sentence verbatim — refusal in the
	// refuser's words, never a paraphrase.
	ErrSchedulerRefused = &Error{
		Code:    -31935,
		Message: "the continuous scheduler refused the request",
		Data: Data{
			What:  "the scheduler understood the request and declined it — the what field is the validator's or the store's own sentence",
			Means: "nothing was written: no authority row moved, no control was recorded, no work was started or stopped. The scheduler's next tick behaves exactly as it did before the call",
			Todo:  "do what the what says — name a real component root, fix the template the validator's sentence names, or ask the person whose door the control is (scheduler.status answers what is enabled)",
			Fatal: false,
		},
	}

	// ErrSchedulerDisabled answers a control that needs the master row the
	// scheduler does not have: enabling one repository while the master is
	// off is one act enabling two authorities, and the door refuses the
	// composition rather than writing half of it.
	ErrSchedulerDisabled = &Error{
		Code:    -31936,
		Message: "the continuous scheduler master is off",
		Data: Data{
			What:  "this control needs the master enabled first — the scheduler picks nothing while the master row says off, and a per-repository authority under a master that is off reads as power nobody granted",
			Means: "nothing was written. The repository's setting stands as it was; the master row is one write (scheduler.enable), not this call's job",
			Todo:  "send scheduler.enable first (one act, one authority), then resend this control — scheduler.status shows the master's state and who set it",
			Fatal: false,
		},
	}
)

// S61 owns -31964...-31969, the block S61 T0 measured free at -31962
// upward (S63's two landed first at -31962/-31963, and T7 re-measured the
// tail free before taking it). Two voices carry the whole plane: the
// charter family and the intake family each refuse with one code, and the
// reason rides `what` as the service's OWN sentence — never a paraphrase
// (DB19). They arrive beside the table for the reason every other plane
// states: a shared voice belongs in the one registry the header demands.
var (
	// ErrCharterRefused is the charter family's refusal voice: the door
	// understood and declined — a charter body that will not validate, a
	// save from a caller who is not the person, a root that is a child
	// (the charter lives on the project root), a revision that does not
	// exist. `what` carries the validator's or the store's sentence.
	ErrCharterRefused = &Error{
		Code:    -31964,
		Message: "the charter request was refused",
		Data: Data{
			What:  "the charter door understood the request and declined it — the what field is the validator's or the store's own sentence, never a paraphrase",
			Means: "nothing was written: no revision was appended, no charter moved. A charter is a promise, and a promise that half-lands is not a promise",
			Todo:  "do what the what says — fix the body against charter.vocabulary, ask from a person's connection, name a project root rather than a child, or read charter.revisions for what does exist",
			Fatal: false,
		},
	}

	// ErrIntakeRefused is the intake family's refusal voice, and its
	// reasons are exactly the five the crash drills name: a stale digest
	// (the plan moved under the preview), a target the scaffold laws
	// refuse, a publish with a key missing, a write aimed at a caller who
	// is not the person, and a resume whose recorded evidence no longer
	// holds. `what` carries the engine's or the door's own sentence.
	ErrIntakeRefused = &Error{
		Code:    -31965,
		Message: "the intake request was refused",
		Data: Data{
			What:  "the intake door understood the request and declined it — the what field is the engine's own sentence: a stale digest, a refused target, a missing publish key, a non-person writer, or a resume whose evidence no longer holds",
			Means: "nothing was written and nothing half-wrote: a refused commit is not a paused one — the half-run was taken back or the ledger says exactly where it stands. intake.status is the door that answers where",
			Todo:  "do what the what says — re-preview (intake.plan hands out a fresh digest), aim at an empty target under the person's workspace, turn on the setting the sentence names, or ask from a person's connection",
			Fatal: false,
		},
	}
)
