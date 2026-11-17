// Package narrow is S19's enumerated projection of the socket protocol:
// ONE action table, mounted twice (stdio for editors, HTTP for CI) and never
// anywhere else. The table, not a handler, is the security boundary — the
// socket is wide by design (P25) and this package exists to be narrow.
//
// The two permanent refusals (plan.md, R6 and N16) are properties of THIS
// file's data, checked by enumeration in actions_test.go, and they are
// permanent, not deferred: no operational control actions, ever, behind no
// flag and behind no stronger key; and no answering questions, because a
// gate exists to reach a human and an API key is not one.
package narrow

import (
	"strings"

	"github.com/cjlapao/aeon/internal/protocol"
)

// Kind is closed: exactly read and trigger. A third kind would mean a third
// category of thing a caller may ask this surface to do, and the point of
// enumerating is that categories do not grow quietly (TestKindsAreClosed).
type Kind string

const (
	// KindRead answers from state the daemon already holds. It never waits
	// for a loop, never blocks, and holds nothing (T7 bounds it).
	KindRead Kind = "read"
	// KindWebhook is S20 T6 (TR11): a signed third-party delivery is
	// MOUNTED here, on this table — it is not a listener of its own. It
	// shares the trigger's one-way nature (a delivery starts a loop and
	// cannot touch it again) and differs in ONE thing that changes the
	// security shape: its caller is not a key-holder. The route therefore
	// carries no apikey level (R-5 — GitHub does not know AEON_HTTP_KEY,
	// and a key-401'd delivery proves the wrong thing); the handler
	// authenticates by HMAC before anything else, and the payload it
	// carries is stranger text (TR10), labelled through S12's mechanism
	// one layer down.
	KindWebhook Kind = "webhook"
	// KindTrigger is one-way: it starts a loop and cannot touch it again.
	// The table holds exactly one (TestTriggerIsOneWay).
	KindTrigger Kind = "trigger"
	// KindCallback is S21 T6 (RA7, RA8): the completion of an async remote
	// Task, arriving as a delivery to THIS surface because an async Task
	// cannot be a held connection (P2 survives daemon restarts; a socket
	// does not). It shares the webhook's security shape — a caller that is
	// NOT a key-holder (a peer knows its own pinned key and AEON_HTTP_KEY
	// never; R-5's exemption extends to it by the same argument), HMAC
	// verified before anything else — and differs in the one thing that
	// makes it sharper: a delivery must be BOUND as well as verified
	// (created by us · still awaiting · callee == sender), because an
	// authenticated-but-unbound completion injects a skill result into a
	// transcript, which is N6's fabricated prompt with a valid signature.
	KindCallback Kind = "callback"
)

// Valid reports whether k is one of the two closed kinds.
func (k Kind) Valid() bool {
	return k == KindRead || k == KindTrigger || k == KindWebhook || k == KindCallback
}

// Action is one entry of the narrow surface. It is deliberately thin: it is
// a PROJECTION (N13), naming the socket method it rides, so the socket stays
// the single implementation and this table stays the single decision about
// what is exposed.
type Action struct {
	// Name is the tool/route name on both mountings, aeon_* by convention.
	Name string `json:"name"`
	// SocketMethod is the protocol method this action projects. Typed as
	// protocol.MethodName so the dependency is in the type, not a comment,
	// and TestEverySocketMethodExists can assert it against S2's real
	// table — PublicMethods(), the set S2 documented for this exact
	// purpose (method.go:603). Derived, not parallel.
	SocketMethod protocol.MethodName `json:"socketMethod"`
	Kind         Kind                `json:"kind"`
	// Cannot is what THIS entry cannot do (N12). Non-empty is enforced
	// (TestEveryEntryStatesWhatItCannotDo): every narrow action is
	// narrower than the method it projects, and an entry whose author
	// cannot name the narrowing has not thought about the entry.
	//
	// Cannot is exempt from the forbidden-word enumeration — it is where
	// the refusal is WRITTEN ("cannot stop the loop it starts"), and
	// forbidding the word there would forbid saying the thing.
	Cannot []string `json:"cannot"`
}

// ForbiddenOperationalControl is the enumeration (N5, R6, I1): words that
// may not appear in an action's name or socket method, because an action
// carrying them would hand a caller — usually an agent — the means to
// control a loop mid-flight.
//
// It is checked as a WORD set over name tokens and socket-method segments,
// never as a list of specific actions to exclude, and the trigger kind is
// pinned to the one start. Together (actions_test.go) they catch the synonym
// that no one thought to forbid: an operational action needs SOME socket
// method, every existing operational method (step.stop, loop.answer,
// step.steer_cancel) carries a forbidden word, and adding a method is
// forbidden by N8 — the existence test fails closed on a new verb.
//
// Permanent, not deferred. "Handing an agent the means to start loops is
// handing it a way around every bound placed on one" — and the R6 answer to
// "start itself?" is that the one-way start is what CI needs, and everything
// AFTER the start is human authority (TestTriggerIsOneWay).
var ForbiddenOperationalControl = []string{
	"stop", "kill", "pause", "resume", "retry", "answer", "abort", "cancel",
	// "fire" arrived with S20 T8 BEFORE any action needed it: the clock's
	// own verbs (trigger.pause, trigger.resume, trigger.fireNow) carry the
	// operational words, and the whole point of scanning name AND socket
	// method is that the synonym added in a year is forbidden the day it
	// is typed. trigger.* methods exist for CLI and app; on this surface
	// they are violations by construction (R6).
	"fire",
	// "dismiss" and "raise" arrived with S55 T5 (2026-11-01), the "fire"
	// precedent carried: the loop-control plane's remaining verbs (the
	// request desk's dismiss door, and the cap's raise — the latter lands
	// in T8) are forbidden by word the day the plane is typed, so a
	// projection added later is caught by the same enumeration. No action
	// on this surface carries either word today; the guard exists for the
	// one that is never going to be written. (This comment says the verbs
	// by word, never by method name — the text guard in
	// cmd/aeon/loop_control_amend_test.go forbids the method literals here,
	// comments included.)
	"dismiss", "raise",
}

// actions is the table. It is package-level ONLY so Actions() is the single
// place any of it can be read; nothing appends to it at runtime and
// TestActionsIsTheOnlyRegistrationPoint fails anything that tries.
//
// Q8, answered (the full table, with the reasoning per entry recorded in
// aeon/docs/NARROW.md — a human reads that, a tool calls this):
var actions = []Action{
	{
		Name:         "aeon_loop_list",
		SocketMethod: protocol.MethodLoopList,
		Kind:         KindRead,
		Cannot: []string{
			"cannot reveal loops the daemon's own filesystem access control (P26) would not show this caller",
			"cannot return provider credentials or secrets held by a route",
			"cannot enumerate sessions or other callers",
		},
	},
	{
		Name:         "aeon_loop_show",
		SocketMethod: protocol.MethodLoopShow,
		Kind:         KindRead,
		Cannot: []string{
			"cannot expose the step transcript — read it with aeon_step_show",
			"cannot resolve a question the loop is asking — it shows the ask, never the answer",
			"cannot present cost as more than the measured floor it is (D5)",
		},
	},
	{
		Name:         "aeon_step_show",
		SocketMethod: protocol.MethodStepShow,
		Kind:         KindRead,
		Cannot: []string{
			"cannot run, continue or steer the step — the transcript is what it reads",
			"cannot hide dropped thinking — the drop count is reported, never silently omitted (S18 T5)",
		},
	},
	{
		Name:         "aeon_blueprint_validate",
		SocketMethod: protocol.MethodBlueprintValidate,
		Kind:         KindRead,
		Cannot: []string{
			"cannot save or alter a blueprint — no save projection exists on this surface",
			"cannot start a loop with what it validates (that is aeon_loop_start, a separate decision)",
		},
	},
	{
		Name:         "aeon_questions_pending",
		SocketMethod: protocol.MethodStoreAwaiting,
		Kind:         KindRead,
		Cannot: []string{
			"cannot answer a question — a gate exists to reach a human and a key is not one (N16, permanent)",
			"cannot create a question — a fabricated question is a fabricated human (N6, permanent)",
			"cannot tell a caller whom to ask",
		},
	},
	{
		// aeon_questions_open is T5's read: the one question a loop is
		// blocked on — id, kind, prompt, and the declared answer set — so a
		// polling CI can report WHY a loop stopped. "Waiting on a gate"
		// without the prompt is not an answer anyone can carry to a human.
		// It is a read, and it stays a read forever (questions.go).
		Name:         "aeon_questions_open",
		SocketMethod: protocol.MethodQuestionPending,
		Kind:         KindRead,
		Cannot: []string{
			"cannot answer the question it shows — answering requires a person, and a key authenticates a caller rather than one (N16, permanent)",
			"cannot create or alter a question — questions arise from steps, never from callers (N6, permanent)",
			"cannot choose whom to ask, and cannot notify anyone",
		},
	},
	{
		// The webhook delivery (S20 T6): the ONE route on this surface
		// that does not carry the key — because it carries something
		// stronger for exactly one purpose (an HMAC over the exact bytes)
		// and something far weaker for everything else (the sender is a
		// stranger whose payload must never be read as instructions).
		Name:         "aeon_webhook_deliver",
		SocketMethod: protocol.MethodWebhookDeliver,
		Kind:         KindWebhook,
		Cannot: []string{
			"cannot start anything without a signature that verifies over these exact bytes — and a signature that DOES verify proves who sent it, never what they may cause (TR9; governance runs after authentication, not instead of it)",
			"cannot let its payload speak as instructions — the text rides the loop labelled third-party DATA (TR10, the W23/MC7/AS12 rule with no agent in the room)",
			"cannot deliver twice — a replayed delivery is refused and recorded (a resend is not an event)",
			"cannot steer, pause, resume, retry, or stop the loop a delivery starts — deliveries are one-way like every trigger here",
			"cannot reveal whether an unknown id is paused, configured, or real — 404 is the whole answer, so the endpoint cannot be enumerated",
		},
	},
	{
		// The remote completion (S21 T6): the inbound half of a call that
		// went out over a pinned TLS connection. The peer comes back to
		// OUR surface because holding a connection open for the answer
		// would make the suspension die with the daemon (RA7) — which is
		// the one property P2 guarantees.
		Name:         "aeon_remote_complete",
		SocketMethod: protocol.MethodRemoteComplete,
		Kind:         KindCallback,
		Cannot: []string{
			"cannot land a completion whose signature does not verify against the CONFIGURED peer's own key — and a signature that verifies proves who sent it, never what they may cause (TR9's discipline one layer up: the BINDING decides that)",
			"cannot land a completion for a Task this daemon never created — an unsolicited skill result is a fabricated answer, the injection N6 refuses for questions (RA8)",
			"cannot land a completion whose callee is not the sender — peer A answering peer B's task is refused no matter what its signature proves",
			"cannot deliver twice — a completion lands once, the first record wins; a resend is not an event",
			"cannot start, steer, pause, resume, retry or stop any loop — it answers one Task that its own outbound call created, and touches nothing else",
			"cannot accept a peer that was configured by discovery — peers are configured explicitly with a pinned identity and a key, or they do not exist (RA9)",
		},
	},
	{
		Name:         "aeon_loop_start",
		SocketMethod: protocol.MethodLoopStart,
		Kind:         KindTrigger,
		Cannot: []string{
			"cannot steer, pause, resume, retry, or stop the loop it starts — the trigger is one-way (TestTriggerIsOneWay)",
			"cannot answer gates the started loop reaches (N16)",
			"cannot grant outward effects beyond what the start request itself declared (T6, per-loop and per-moment)",
		},
	},
}

// Actions is the ONLY registration point (O7, N12: "an action list that
// grows by accident is how N5 erodes"). Both mountings take exactly this
// slice and nothing else — the set-equality test (T4) is what makes "one
// table, two mountings" rather than a slogan.
//
// The copy is defensive: callers get the table, not the table's backing
// array.
func Actions() []Action {
	out := make([]Action, len(actions))
	for i, a := range actions {
		a.Cannot = append([]string(nil), a.Cannot...)
		out[i] = a
	}
	return out
}

// violations returns every way the given table breaks the operational-control
// boundary. It is a function over tables (not over Actions()) precisely so
// the test can feed it POISONED tables — the enumeration must catch the
// synonym nobody thought to forbid, and a test that can only inspect the
// real table cannot demonstrate that.
func violations(table []Action) []string {
	forbidden := map[string]bool{}
	for _, w := range ForbiddenOperationalControl {
		forbidden[w] = true
	}
	triggers := 0
	var bad []string
	for _, a := range table {
		for _, field := range []struct {
			what string
			s    string
		}{
			{"name", a.Name},
			{"socket method", string(a.SocketMethod)},
		} {
			for _, tok := range tokenize(field.s) {
				if forbidden[tok] {
					bad = append(bad, a.Name+": "+field.what+" token "+tok+" is operational control")
				}
			}
		}
		if a.Kind == KindTrigger {
			triggers++
			if a.Name != "aeon_loop_start" {
				bad = append(bad, a.Name+": the only trigger this surface may carry is aeon_loop_start")
			}
		}
	}
	if triggers != 1 {
		bad = append(bad, "the table must carry exactly one trigger action")
	}
	return bad
}

// tokenize splits a name or method into its words: aeon_loop_stop →
// {aeon, loop, stop}; step.steer_cancel → {step, steer, cancel}.
func tokenize(s string) []string {
	var toks []string
	start := 0
	// camelCase is a word boundary too (S20 T8): the socket methods speak
	// camelCase (trigger.fireNow), and a scanner that read "fireNow" as one
	// token would scan exactly like a guard that never fires — present,
	// green, and blind to the word it exists to catch.
	camel := func(i int) bool {
		return i > start && s[i] >= 'A' && s[i] <= 'Z' && s[i-1] >= 'a' && s[i-1] <= 'z'
	}
	for i := 0; i <= len(s); i++ {
		if i < len(s) && camel(i) {
			if i > start {
				toks = append(toks, strings.ToLower(s[start:i]))
			}
			start = i
		}
		if i == len(s) || s[i] == '_' || s[i] == '.' || s[i] == '-' {
			if i > start {
				toks = append(toks, strings.ToLower(s[start:i]))
			}
			start = i + 1
		}
	}
	return toks
}
