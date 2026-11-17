package attention

import (
	"encoding/json"
	"strconv"
	"strings"
)

// doors.go — the door-template table (SV2).
//
// Every kind has exactly one door: a fixed sentence naming the action a
// person takes, built from ids and enums only. The table is DATA with a
// golden (TestDoorTableGolden), so a kind added without a door fails, and
// the sentence of an existing door cannot change without the golden — and
// the golden file's header — changing in the same commit.
//
// # The phantom-door rule (S19's F-1)
//
// A template may name only a method that is REGISTERED. Templates that
// depend on a sibling slice's plane are `Gated`: when the wired check says
// the method is absent, the door renders the GENERIC sentence and no method
// — a door that promises `loop.raise` on a build without S55 wired is the
// "boundary statement naming a phantom `aeon loop stop`" lesson, one layer
// in. The daemon passes the real table's method set (protocol.Table.
// Methods()); TestEveryDoorNamesARegisteredMethod holds it against every
// template.

// DoorTemplate is one kind's door: the method it names ("" is a READ-ONLY
// door — a sentence a person acts on in the app, no command claimed),
// whether that method belongs to a gated sibling plane, and the sentence.
type DoorTemplate struct {
	Method   string
	Gated    bool
	Sentence func(it Item) string
}

// doorTable is the closed template set for the kinds T2's three sources
// emit. blocked.<kind> names come from the durable loop.blocked reason
// (SV-D12); an unrecognized reason classifies as blocked.other and lands on
// the generic door — never a guess at a command.
func doorTable() map[string]DoorTemplate {
	return map[string]DoorTemplate{
		"question": {Method: "answer", Sentence: questionDoor},
		"stopped":  {Method: "step.continue", Sentence: stoppedDoor},
		"blocked.action-denied": {Sentence: func(Item) string {
			return "This loop is unattended, so outward actions are denied. Start an attended run, or (S56) grant the action."
		}},
		"blocked.step-blocked": {Method: "loop.retry", Sentence: readAndRetryDoor},
		"blocked.model-error":  {Method: "loop.retry", Sentence: readAndRetryDoor},
		"blocked.runner-error": {Method: "loop.retry", Sentence: readAndRetryDoor},
		"blocked.exhaustion":   {Method: "loop.raise", Gated: true, Sentence: exhaustionDoor},
		// The verification, proposal and triage planes. Each names the READ that opens the evidence (or the
		// real CLI verb for a proposal); the act itself stays behind the person's own door there. Gated: a
		// build without that plane renders the generic sentence, never a phantom method.
		"gate-failed":      {Method: "verify.runShow", Gated: true, Sentence: gateFailedDoor},
		"proposal":         {Sentence: proposalDoor}, // a read-only door: commit and reject are never named as a method (brief §7)
		"bugs-unvalidated": {Method: "triage.list", Gated: true, Sentence: bugsDoor},
	}
}

// Door resolves an item's door (method, sentence) against the table and the
// wired check. The returned method is "" whenever the person's act has no
// command on THIS build — read-only rows and gated-unwired siblings alike.
func Door(kind string, it Item, wired func(method string) bool) (method, sentence string) {
	t, ok := doorTable()[kind]
	if !ok {
		return "", Generic(kind)
	}
	if t.Method != "" && t.Gated && !wired(t.Method) {
		return "", Generic(kind)
	}
	return t.Method, t.Sentence(it)
}

// Generic is the honest fallback sentence (T2's file list names it
// explicitly): the item is real, this build has no single command for its
// kind, and the app is where a person reads it. Naming a kind and no method
// is the point — silence about which kind would hide the gap.
func Generic(kind string) string {
	return "Something needs a person and this build has no single command for kind " +
		kind + ": open the loop in the app and read the reason."
}

// questionDoor is SV2's sentence for a question, with the CLOSED options
// from the row (SV1's door note): the person sees the exact words the
// question offered. A question with no options (clarify, free) gets the
// answer-your-own-words form rather than a fake choice list.
func questionDoor(it Item) string {
	slug := it.LoopSlug
	if slug == "" {
		slug = "<loop>"
	}
	opts := strings.TrimSpace(it.options)
	if opts == "" {
		return "Answer with `aeon answer " + slug + " <your answer>` or in the app."
	}
	return "Answer with `aeon answer " + slug + " <one of: " + strings.Join(splitOptions(opts), " | ") + ">` or in the app."
}

// stoppedDoor names the door that resumes a person-stopped step:
// step.continue with the step id. With no step row to name (the pin says a
// person stopped it and the step row moved first) the sentence says where to
// act instead of inventing an id.
func stoppedDoor(it Item) string {
	if ref, err := strconv.ParseInt(it.ItemRef, 10, 64); err != nil || ref <= 0 {
		return "A person stopped this step. Open the loop in the app to resume it."
	}
	return "A person stopped this step. Resume it with `aeon step continue " + it.ItemRef + "`."
}

// readAndRetryDoor is SV2's step-blocked / model-error / runner-error row:
// read the reason (verbatim above the door), then the retry that exists
// today (F5's missing-handler precedent) or a new run.
func readAndRetryDoor(it Item) string {
	slug := it.LoopSlug
	if slug == "" {
		slug = "<loop>"
	}
	return "The loop could not continue this step. Read the reason; retry with `aeon loop retry " +
		slug + "`, or start a new run."
}

// exhaustionDoor is SV2's ceiling sentence with loop.raise — reachable here
// only when the raise door is actually wired (the Gated check above).
func exhaustionDoor(it Item) string {
	slug := it.LoopSlug
	if slug == "" {
		slug = "<loop>"
	}
	return "This loop reached its run ceiling. Raise it with `aeon loop raise " +
		slug + "`, or read the reason and start a new run."
}

// gateFailedDoor points at the failed run's evidence. The run id is the item's ref.
func gateFailedDoor(it Item) string {
	return "The latest gate run failed. Read run " + it.ItemRef + " (verify.runShow) for what failed, then fix it or run the gates again."
}

// proposalDoor names the one copyable CLI command that decides a proposal.
func proposalDoor(it Item) string {
	return "Read it, then decide with `aeon proposal commit " + it.ItemRef + "` to apply it or reject it in the app."
}

// bugsDoor sends the person to the component's bug list, where validating is a human-door act.
func bugsDoor(it Item) string {
	return "Open the component's triage list (triage.list) and validate the bugs that wait."
}

// splitOptions turns the stored option list (JSON array text, carried
// verbatim by the store read) into display words. The parse lives HERE
// rather than in the store because the store hands the TEXT over untouched
// — one definition of the encoding stays in chat.go's writer, and a row
// whose options are not a JSON array renders as no options, the same way
// every other reader of that column behaves.
func splitOptions(raw string) []string {
	var opts []string
	if json.Unmarshal([]byte(raw), &opts) != nil {
		return nil // not the stored encoding: render as no options, never fake choices
	}
	var out []string
	for _, o := range opts {
		if w := strings.TrimSpace(o); w != "" {
			out = append(out, w)
		}
	}
	return out
}
