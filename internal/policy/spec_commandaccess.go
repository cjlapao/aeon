package policy

// ════════════════════════════════════════════════════════════════════════════
// CommandAccess — the hole S10 left, filled.
//
// S10 declared this type, pinned its shape, and registered the evaluator that
// answers "could not determine" (spec_stubs.go). S11 brings the two things that
// hole was waiting for — the matcher (pattern.go) and the corpus (corpus.go) —
// and this file is where the type starts deciding. It replaces the placeholder
// registration, and with it the type leaves the could-not-determine set:
// inforce.go's noEvaluatorYet now answers false here, because the Stub() marker
// travelled out with the stub.
//
// # The decision rule is the shipped engine's, and NOT the seed design's
//
// docs/aeon/S11-permissions/tasks/T4.md schedules "Evaluate — pure, total,
// first-match". That is NOT executed here, deliberately: measured over the real
// corpus, an ordered first-match fold disagrees with the shipped deny-wins
// engine on a large minority of probe commands, and every one of the
// permission-direction disagreements points the same way — MORE permissive —
// because most agent files author `"*": allow` as their first rule. First-match
// would hand a reviewing agent an ALLOW for `git push` that no operator wrote.
// So this evaluator reuses `Evaluate` (evaluate.go) unchanged — PL4, deny beats
// allow regardless of specificity; PL5, the empty set denies — and the whole
// file-level deviation is on the record in that task file's Notes block, so a
// reviewer does not "restore" an ordering that was measured unsafe.
//
// # The fold: one command, several segments
//
// A command line is cut on the separators the guard knows (segment.go), and the
// two arms are deliberately NOT mirror images:
//
//   - a Deny row matches when ANY segment matches. One refused part refuses the
//     whole line, which is the only reading under which `cd sub && git push` is
//     refused by a rule about `git push`.
//   - an Allow row matches only when EVERY segment matches. The mirror — "any
//     segment matches" — would let `git status*` permit `git status && git push`,
//     because the half it checked was the half it liked. An allow that leaks
//     across a separator is a grant nobody authored, so the arm is universal
//     where the deny arm is existential.
//
// Both arms answer nothing at all when the command has no segment (the empty
// command asks for nothing) or when the splitter refused to analyse it (more
// segments than MaxSegments). Both resolve toward deny, which is the only safe
// direction for an unanalysable input (D10).
//
// # Inert, never unimplemented, never a panic
//
// This type decides now, so it never sets MatchRes.Unimplemented — that flag
// belongs to the placeholder this file replaced, and a row that cannot be read
// is not the same fact as a type nobody can evaluate. Every refusal to match is
// inert: Matched false, Unimplemented false, so PL5's default deny is what the
// engine answers. And a Note names the SHAPE that failed, never the bytes: a
// Note is a log line and this slice's logs carry no policy content (legal
// constraint 1). What travels onward from a segment is the segment's INDEX, which
// is data T12's permission question needs; its text is not.
//
// # `ask` is in the shape AND in the vocabulary — since migration 0019
//
// The pinned shape always named three states, because T7 (S11's D1) admitted
// three actions and this file was written against the final vocabulary once
// rather than patched twice. Migration 0019 widened `policy.action`'s CHECK
// and `types.go`'s closed set, so `stateAgreesWithAction` now admits an `ask`
// spec whose row wears `ask`, and the action switch below has an arm for it:
// an ask row matches DENY-LIKE — any segment matching the pattern routes the
// whole command to a human — because a line whose parts include something an
// operator must approve is a line that must be approved, and the universal
// arm (the allow arm's) would let a second, unrelated segment dodge the
// question. In the engine's fold it then outranks every allow and loses to
// every deny: deny > ask > allow. What it never is: a permission. `Asked`
// verdicts carry `Allowed == false`, and `verdict.go` renders them their own
// sentence. TestCommandAccessAskIsAcceptedAtAuthoringAndDecidesAtEvaluation
// and the corpus sweep pin both halves.
// ════════════════════════════════════════════════════════════════════════════

import (
	"fmt"
	"reflect"
	"strings"
)

// CommandAccessSpec is the spec of a CommandAccess policy: one command pattern,
// and what the engine does when a command matches it. S11 brings the matcher
// (pattern.go) and the corpus (corpus.go); it matches req.Command — and,
// through the segment fold below, every segment of req.Command.
//
// Command is matcher input, not a name compared for equality, which is why this
// type carries two checks ToolAccess has no equivalent of (see
// commandAccessValidator). It is matched by MatchPattern: '*' anywhere, exact
// without one, every other byte literal — so the bare pattern "gh" governs the
// two-character command and never "gh pr merge".
//
// State repeats the row's action inside the spec, exactly as ToolAccess does:
// the spec is what an operator writes and what the app renders, and a spec whose
// state contradicts its action is a policy whose author meant one thing and
// stored another. The validator refuses the contradiction and the evaluator
// refuses to match a row that escaped the validator.
type CommandAccessSpec struct {
	Command string `json:"command"`
	State   string `json:"state"` // "allow" | "deny" | "ask"
}

// commandAccessShape is the one string every refusal this file produces quotes,
// so the operator sees the shape that was wanted — the same discipline
// spec_toolaccess.go keeps for ToolAccess, and the same one
// TestThePinnedSpecShapesMatchTheirGoStructs locks against the struct above.
const commandAccessShape = `{"command": string, "state": "allow"|"deny"|"ask"}`

// commandAccessSchema is the type's pinned spec shape. The accepted keys come
// from the struct by reflection; the string above is what a refusal renders.
var commandAccessSchema = newSpecSchema(CommandAccess, commandAccessShape, reflect.TypeOf(CommandAccessSpec{}))

// ── the validator ────────────────────────────────────────────────────────────

// commandAccessValidator is the write boundary's per-type check for
// CommandAccess: the shared strict decode, plus the two things a command
// pattern needs that a tool name does not.
type commandAccessValidator struct{}

// Validate refuses any CommandAccess spec that is not exactly
// {"command": string, "state": "allow"|"deny"|"ask"} with a pattern the matcher
// accepts and a state that agrees with the row's action.
//
// The shared half (commandAccessSchema.validate) covers the shape: unknown
// fields, missing fields, empty fields, the state/action contradiction. The two
// below are pattern-specific, and they exist because a command pattern is
// MATCHER input:
//
//   - ValidatePattern (T3) refuses what MatchPattern would otherwise answer
//     surprisingly — an empty pattern, two adjacent '*', edge whitespace. The
//     empty pattern is refused twice over, once by the schema as an empty
//     required field and once here as a pattern that matches nothing, and both
//     refusals quote the shape: a rule that matches nothing reads to every
//     later reader as a rule that permits everything.
//
// What this validator does NOT do is refuse a `"*"` allow targeting all-agents.
// It is the broadest rule in the vocabulary and it is what the corpus actually
// says — most of this repository's own agent files author exactly that first —
// so refusing it would make the real rule set unseedable, and a guard's job is
// not to veto the specification it is written against. It deserves a warning at
// authoring, not a refusal; the warn channel (addUnmatchableTargetWarn,
// cmd/aeon/policy.go) is a CLI-side function with no seam reaching in here —
// Validator's whole answer is a *Reject — so the warning lands with T8, which
// owns that file. Recorded in the task Notes rather than left as a rumour.
func (commandAccessValidator) Validate(p Policy) *Reject {
	// Decode first, then the shared validate. The order costs nothing (validate
	// decodes as its own first act, so every refusal below is the same sentence
	// it would have produced) and buys two things: the pattern check below reads
	// a value that was actually decoded rather than a second decode nobody can
	// make fail, and "can this be read at all" is answered before "is every
	// field there", which is the order a person debugs it in.
	var spec CommandAccessSpec
	if rej := commandAccessSchema.decode(p.Spec, &spec); rej != nil {
		return rej
	}
	if rej := commandAccessSchema.validate(p); rej != nil {
		return rej
	}
	if rej := ValidatePattern(spec.Command); rej != nil {
		return rejectf(`%s spec field "command" is not a pattern the matcher accepts: %s; `+
			`the spec is exactly %s`, string(CommandAccess), rej.Reason, commandAccessShape)
	}
	return nil
}

// SpecSchema reports the pinned CommandAccess shape (SchemaCarrier), so the
// write boundary and `aeon policy add` can tell an operator what to write
// without a help string copied from this file and drifting from it.
func (commandAccessValidator) SpecSchema() SpecSchema { return commandAccessSchema.public() }

// ── the evaluator ────────────────────────────────────────────────────────────

// commandAccessEvaluator answers the type S10 left undeclared. It never reports
// Unimplemented: this type decides now.
//
// Every refusal to match is inert — Matched false, no Unimplemented — which is
// the totality claim D10 hands to the evaluator: a row that cannot be read
// permits nothing and panics nothing, and PL5's default deny is what the caller
// sees. Note is always a fixed string naming the shape or the arm that declined,
// never the bytes that failed it.
type commandAccessEvaluator struct{}

// Match reports whether p's spec names what r wants to run.
//
// The checks run in this order because the order is the argument: read the row,
// then ask whether it is even about this type, then whether its pattern is one
// the matcher can be given, then whether the row says what its action says, and
// only then look at the command. Each step refuses for its own reason, so an
// operator reading a log line learns which of the four was wrong rather than
// "it did not match".
func (commandAccessEvaluator) Match(p Policy, r Request) MatchRes {
	var spec CommandAccessSpec
	if rej := commandAccessSchema.decode(p.Spec, &spec); rej != nil {
		return MatchRes{Note: "the spec is not the pinned CommandAccess shape"}
	}
	if r.Type != CommandAccess {
		// The engine never hands an evaluator another type's request; if it
		// ever did, the answer is still "not me", never "yes".
		return MatchRes{Note: "the request asks about a different type"}
	}
	if strings.TrimSpace(spec.Command) == "" {
		// Belt to the validator's braces. The empty pattern matches nothing by
		// MatchPattern's own rule, and a row that matches nothing while LOOKING
		// like a permission is the failure this whole file is written against.
		return MatchRes{Note: "an empty command pattern matches nothing"}
	}
	if spec.State != string(p.Action) {
		// A row the validator would have refused (hand-written SQL, or stored
		// before this validator existed). Choosing either half of the
		// contradiction would be the evaluator inventing an intention nobody
		// wrote, so it decides nothing.
		return MatchRes{Note: "the spec's state contradicts the row's action"}
	}
	if rej := ValidatePattern(spec.Command); rej != nil {
		// The pattern is matcher input the write boundary would never have
		// admitted: two adjacent '*', or edge whitespace a byte-for-byte
		// comparison can only get wrong. Inert, and the Note names the shape
		// rather than repeating the pattern (the pattern IS command content).
		return MatchRes{Note: "the command pattern is not one the matcher accepts"}
	}

	segs := Split(r.Command) // segment.go
	if segs.OverLimit {
		// Unanalysable, and stated as such by the splitter rather than guessed
		// here. Nothing is permitted: PL5 does the denying, and the truncation
		// is never mistaken for a complete reading.
		return MatchRes{Note: "the command exceeds the segment limit; nothing is permitted"}
	}
	if segs.Count == 0 {
		// The empty command, ";", " && ": nothing was asked for. Matching an
		// allow against zero segments would be vacuously true, and a vacuous
		// true is how every command with no content inherits every grant.
		return MatchRes{Note: "the command has no segment to match"}
	}

	switch p.Action {
	case Deny:
		// ANY denied segment denies the whole command. The Note carries the
		// segment's INDEX and nothing else: `aeon policy why` and T12's
		// question need "which part", and the segment's text is policy content
		// that a log line must never carry.
		for i, s := range segs.Items {
			if MatchPattern(spec.Command, s) {
				return MatchRes{Matched: true, Note: fmt.Sprintf("segment %d of %d", i+1, segs.Count)}
			}
		}
	case Ask:
		// ANY segment matching the asked-for pattern routes the WHOLE command
		// to a human — the deny arm's shape, deliberately: an ask takes the
		// line out of autonomous execution the way a deny does, and the
		// universal (allow) arm would let `git status && git reset --hard`
		// dodge a rule about `git reset --hard*` by carrying a second,
		// innocent segment. Deciding either way is still not this arm's job:
		// the engine's fold ranks the match (deny > ask > allow) and
		// verdict.go renders the ask.
		for i, s := range segs.Items {
			if MatchPattern(spec.Command, s) {
				return MatchRes{Matched: true, Note: fmt.Sprintf("segment %d of %d", i+1, segs.Count)}
			}
		}
	case Allow:
		// EVERY segment must match, or the row grants nothing. This is the
		// asymmetry that stops an allow leaking across a separator:
		// "git status && git push" is not allowed by a rule about "git status*".
		for _, s := range segs.Items {
			if !MatchPattern(spec.Command, s) {
				return MatchRes{Note: "a segment is outside the allowed pattern"}
			}
		}
		return MatchRes{Matched: true}
	}
	// No default branch, on purpose — the same shape evaluate.go's own fold
	// uses. A row whose action is outside the closed set (a hand-written row
	// wearing an invented value — the three real actions all have arms above,
	// since migration 0019 admitted `ask`) matches nothing and permits
	// nothing: inert toward deny, which is the only safe direction for a value
	// the vocabulary does not have.
	return MatchRes{Note: "the row's action is outside the closed set; nothing is permitted"}
}

func init() {
	RegisterType(CommandAccess, commandAccessValidator{}, commandAccessEvaluator{})
}

// --- describing this type (S45 T2, PM5/PM6) -----------------------------------
//
// Additive: Validate and Match above are untouched. The sentence reads the
// spec through the same schema the write passed, and refuses the
// contradiction exactly as the evaluator above refuses to match it.

// describe renders the design's command_access sentence:
// "<allows|denies|asks before> commands matching %q for <target phrase>"
// plus the shared suffixes. "matching" is the word the pattern language
// earns — a command spec is a pattern (the corpus's grammar), not a name.
func (commandAccessValidator) describe(p Policy) string {
	if !describeReadable(p) {
		return unreadableSentence(CommandAccess)
	}
	verb, _ := describeVerb(p.Action)
	var spec CommandAccessSpec
	if rej := commandAccessSchema.decode(p.Spec, &spec); rej != nil {
		return unreadableSentence(CommandAccess)
	}
	if spec.State != string(p.Action) {
		return unreadableSentence(CommandAccess)
	}
	return describeClause(verb+" commands matching "+describeQuoted(spec.Command), p)
}

// describeFields derives the descriptor from commandAccessSchema — the one
// field list the write boundary enforces.
func (commandAccessValidator) describeFields() []SpecField {
	return commandAccessSchema.describeFields()
}
