package handler

// ════════════════════════════════════════════════════════════════════════════
// The handoff — what the NEXT STEP needs, which is not the same question as
// "does the artefact exist" (S8's `produces:` already answers that one).
//
// A5 observed the retired skills' real content is mostly FILE SCHEMAS — the
// fixed section order of prompt.md, discovery.md, plan.md. Each belongs
// either to the one handler that writes it or to a handoff schema like this
// one; a schema invented after twenty-five bodies is a schema that fits none
// of them, which is why this lands before the rewrites.
//
// The outcomes are S8's four (L4) — `ok | reject | blocked | failed` — with
// the routing S8 already implements. NOT a new set: S8's T9 asked this same
// question about `StepResult.Outcome` and answered "reuse it". The type
// identity is asserted (TestFourOutcomesRoundTrip), and TestNoSecondOutcomeSet
// greps for a competing enumeration, because the second set always starts as
// one convenient local constant.
//
// The load-bearing rule is D12: a handler that DECLARES a field and does not
// produce it has not finished its job — that is a `reject` naming the field,
// not an `ok` with a missing key. The next step is entitled to what it was
// promised, and S8's rework machinery (`rework` target, bounded `on_reject`)
// exists precisely to route this. The model's self-claim is an input to the
// check, never the verdict: a step is as done as its fields say it is.
//
// The asymmetry is deliberate (TestUndeclaredFieldIsIgnoredNotAnError):
// missing declared fields fail, EXTRA fields are kept and unpunished. The
// symmetrical-looking rule would turn a model volunteering useful context
// into a step failure — punishing exactly the behaviour a good handoff wants.
// ════════════════════════════════════════════════════════════════════════════

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/cjlapao/aeon/internal/runtime"
)

// Handoff is what a step hands the next one. Outcome is S8's type — this
// package defines no outcomes.
type Handoff struct {
	Outcome runtime.Outcome   `json:"outcome"`
	Reason  string            `json:"reason,omitempty"`
	Fields  map[string]string `json:"fields,omitempty"`
}

// FieldSpec is one declared field. Purpose is REQUIRED at load (O6): a
// field named `notes` with no contract is a field nobody can rely on and
// everybody populates differently — S16 and S17 consume these.
type FieldSpec struct {
	Purpose  string
	Required bool
}

// HandoffSchema maps field name → spec. Built from front matter:
//
//	handoff:
//	  summary: the paragraph a reader of this step's output needs
//	  tests_added?: paths of tests written, or "none"
//
// A `?` suffix on the key marks the field optional — declared, contract
// stated, absence legal (TestOptionalFieldMayBeAbsent). Everything else is
// required: that IS D12's promise.
type HandoffSchema map[string]FieldSpec

// pr_summary arrived WITH its reader (S16 T9 lifted the N4 guard that
// refused it before this slice existed). Its Purpose is the framing
// itself — L3's wording is the specification, because a field described
// as "a summary" gets a restatement of the title. Enforcing the exact
// purpose at load is what keeps twenty bodies from each paraphrasing the
// contract of the one paragraph a human reads.
const (
	PrSummaryField   = "pr_summary"
	PrSummaryPurpose = "the paragraph a human will read on the pull request"
)

// S17 T6 — verify's THREE outcomes ride a FIELD, not a fifth outcome. The
// protocol's wording is the specification: "lint is unverified, which is
// not the same as clean. Do not report verification=pass on an unverified
// gate." L4's four outcomes stand (this package still defines no
// outcomes — TestNoSecondOutcomeSet keeps it that way); what the verifier
// lacked was a STRUCTURED place to say "the check did not run", because a
// reject carrying only prose says "the code is wrong" and sends the redo
// off to fix code that may be fine (D9: fix-the-finding and
// make-the-check-run are different work).
//
// The purpose is pinned exactly as pr_summary's is — a body paraphrasing
// its own contract is twenty bodies with twenty contracts — and Check
// renders the canonical "unverified: " reason from the FIELD, so the
// engine consumes a structured verdict end to end and prose never decides
// anything (W5's oomKilled fact, consumed, never re-derived).
const (
	VerificationField      = "verification"
	VerificationPass       = "pass"
	VerificationFail       = "fail"
	VerificationUnverified = "unverified"
	VerificationPurpose    = "pass when every check ran and found nothing, fail when a check ran and found something, unverified when a check did not run — never pass on an unverified gate"
)

// Verification reads the structured verdict. An off-vocabulary value is
// INERT ("" — never trusted, and IsUnverified will not fire on it); the
// refusal to read it is the producer's problem, and Check's required-field
// discipline is where a missing one surfaces.
func (h Handoff) Verification() string {
	switch strings.TrimSpace(h.Fields[VerificationField]) {
	case VerificationPass:
		return VerificationPass
	case VerificationFail:
		return VerificationFail
	case VerificationUnverified:
		return VerificationUnverified
	}
	return ""
}

// IsUnverified is the structured fact: a REJECT that says verification=
// unverified. Not a fifth outcome — a reject with a distinct reason (L33,
// D8), which routes back like a fail but charges no task budget (O4, Q8).
func (h Handoff) IsUnverified() bool {
	return h.Outcome == runtime.OutcomeReject && h.Verification() == VerificationUnverified
}

// BuildSchema converts loaded front-matter handoff entries into specs,
// enforcing O6 (purpose required) and, for pr_summary, L3's framing verbatim (S16 T9).
func BuildSchema(handoff map[string]string, sourcePath string) (HandoffSchema, *Reject) {
	if len(handoff) == 0 {
		return nil, nil
	}
	s := HandoffSchema{}
	for rawKey, purpose := range handoff {
		key := rawKey
		required := true
		if strings.HasSuffix(key, "?") {
			key = strings.TrimSuffix(key, "?")
			required = false
		}
		if key == "" {
			return nil, reject(sourcePath, "handoff carries an empty field name")
		}
		if key == PrSummaryField && strings.TrimSpace(purpose) != PrSummaryPurpose {
			return nil, reject(sourcePath, "handoff field %q must state L3's framing verbatim — %q — because the wording IS the specification: a field described as \"a summary\" gets a restatement of the title", key, PrSummaryPurpose)
		}
		if key == VerificationField && strings.TrimSpace(purpose) != VerificationPurpose {
			return nil, reject(sourcePath, "handoff field %q must state T6's framing verbatim — %q — because the wording IS the specification: a field paraphrased is a gate half-collapsed", key, VerificationPurpose)
		}
		if strings.TrimSpace(purpose) == "" {
			return nil, reject(sourcePath, "handoff field %q states no purpose (O6) — declare what it is FOR, or do not declare it: %s", key, "S16/S17 consume these fields; an unpurposed field is populated differently by every handler and relied on by none")
		}
		if _, clash := s[key]; clash {
			// `foo` and `foo?` in one front matter: two claims about one field.
			return nil, reject(sourcePath, "handoff declares %q twice (required and optional) — pick one claim", key)
		}
		s[key] = FieldSpec{Purpose: strings.TrimSpace(purpose), Required: required}
	}
	return s, nil
}

// Check enforces D12 against a model's claimed handoff. The claim is an
// INPUT: a step that says `ok` but withheld a declared field is rejected BY
// THIS CHECK, naming the field so S17's redo edges and S8's rework routing
// have something to act on. Non-ok claims pass through — a handler that
// said `blocked` is not additionally punished for the fields a finished run
// would have carried; its reason is the report.
//
// Extra, undeclared fields are KEPT (the schema is a floor, not a ceiling).
func (s HandoffSchema) Check(claimed Handoff) Handoff {
	// T6 first, before anything else can call this ok: an ok handoff
	// whose own field says the gate did not run is self-contradictory,
	// and the RECORD — not the claim — is what the next step reads. This
	// is P31's fifth instance caught at the door: the collapse that looks
	// like a green check never reaches the row.
	if claimed.Outcome == runtime.OutcomeOK && strings.TrimSpace(claimed.Fields[VerificationField]) == VerificationUnverified {
		return Handoff{
			Outcome: runtime.OutcomeReject,
			Reason: "unverified: an ok handoff declaring " + quote(VerificationField) + "=" + VerificationUnverified +
				" contradicts itself — the gate did not run, so nothing was verified (T6; never stored as pass)",
			Fields: claimed.Fields,
		}
	}
	if claimed.Outcome != runtime.OutcomeOK {
		// Non-ok claims pass through — but a reject carrying the
		// structured unverified fact gets its reason RENDERED canonically
		// here, the one place that speaks the engine's clause, so routing
		// downstream consumes a structured verdict, not prose.
		if claimed.IsUnverified() && !strings.HasPrefix(claimed.Reason, "unverified: ") {
			out := claimed
			out.Reason = "unverified: " + claimed.Reason
			return out
		}
		return claimed
	}
	for name, spec := range s {
		if !spec.Required {
			continue
		}
		v, present := claimed.Fields[name]
		if !present || strings.TrimSpace(v) == "" {
			// Deterministic reason: sort-free single-field naming is fine —
			// the FIRST missing field is enough for a redo edge, and
			// re-checks after a fix will name the next one.
			return Handoff{
				Outcome: runtime.OutcomeReject,
				Reason: "handoff field " + quote(name) + " is declared and was not produced — " +
					spec.Purpose + " (the next step was promised this; D12)",
				Fields: claimed.Fields,
			}
		}
	}
	return claimed
}

func quote(s string) string { return `"` + s + `"` }

// MarshalRoundTrip exists for the persistence test to exercise the ONE
// serialisation path — the handoff persists as S8 persists step results, and
// if this type ever grows a field that json drops, the round-trip test finds
// it before a resumed loop does.
func (h Handoff) MarshalRoundTrip() ([]byte, error) { return json.Marshal(h) }

func UnmarshalHandoff(raw []byte) (Handoff, error) {
	var h Handoff
	err := json.Unmarshal(raw, &h)
	return h, err
}

// ── The bridge (T10) ─────────────────────────────────────────────────────
//
// The schema is what the model is SHOWN (StepHandler.HandoffSchema, shown
// verbatim by runtime/compile.go); the HandoffSpec is what VALIDATES
// (RunOpts in the step runtime's RunLoop). Two jobs, one definition: this
// file renders both from the SAME BuildSchema output, so the drift runtime
// pins for its own defaults — a schema promising a field the validator does
// not require is a field models stop sending — cannot open here.
//
// The step runtime currently passes runtime.DefaultHandoffSpec() at its
// RunLoop call site (protocol/step.go); HandoffSpecFor is the drop-in that
// adoption looks like. Until that seam takes it, the SHOWN fields are
// honoured by the model and consumed from the result handoff, while
// enforcement of the per-handler extras rides the default's back — a
// three-state `declared, not enforced` gap, recorded in STATUS, not
// papered over by claiming the validator checks what it does not.

// HandoffSpecFor renders the shown JSON Schema for a handler's declared
// handoff (StepHandler.HandoffSchema). Returns "" when the handler declares
// nothing — the default schema then speaks, unchanged.
func HandoffSpecFor(handoff map[string]string) (string, error) {
	specs, rej := BuildSchema(handoff, "handoff")
	if rej != nil {
		return "", fmt.Errorf("%s: %s", rej.Path, rej.Reason)
	}
	return renderSchema(specs)
}

// ValidatorSpec is the enforcement twin of HandoffSpecFor: same required
// names, same closed status enum. ok=false means the caller keeps
// runtime.DefaultHandoffSpec().
func ValidatorSpec(handoff map[string]string) (runtime.HandoffSpec, bool) {
	specs, rej := BuildSchema(handoff, "handoff")
	if rej != nil || len(specs) == 0 {
		return runtime.HandoffSpec{}, false
	}
	req := []string{"status", "summary"}
	for _, name := range sortedSpecNames(specs) {
		if specs[name].Required && name != "status" && name != "summary" {
			req = append(req, name)
		}
	}
	return runtime.HandoffSpec{Required: req, Enums: map[string][]string{"status": {"ok", "reject", "blocked"}}}, true
}

func sortedSpecNames(s HandoffSchema) []string {
	names := make([]string, 0, len(s))
	for n := range s {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func renderSchema(specs HandoffSchema) (string, error) {
	props := map[string]any{
		"status": map[string]any{
			"enum":        []string{"ok", "reject", "blocked"},
			"description": "ok if you did what was asked; reject if the honest answer is no; blocked if you could not",
		},
		"summary": map[string]any{"type": "string", "description": "what you found or did, in a few sentences"},
	}
	required := []string{"status", "summary"}
	for _, name := range sortedSpecNames(specs) {
		if name == "status" {
			continue // the outcome vocabulary is the runtime's, not the body's
		}
		props[name] = map[string]any{"type": "string", "description": specs[name].Purpose}
		if specs[name].Required && name != "summary" {
			// summary is required by the validator always; a body that
			// re-declares it says what a good summary is HERE, and its
			// purpose travels — the required list stays exactly one entry.
			required = append(required, name)
		}
	}
	doc := map[string]any{
		"type":                 "object",
		"properties":           props,
		"required":             required,
		"additionalProperties": false,
	}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", fmt.Errorf("handoff schema with %d fields: %w", len(specs), err)
	}
	return string(raw), nil
}
