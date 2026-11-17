// verify_kinds.go — S57 T8: the two RESTRICTIVE gate proposal kinds
// (verify_command, merge_rule) as registry entries in this package.
//
// The desk keeps only what is pure: the payload SHAPES, the shape
// validators (the create door's and the commit door's shared half, TD3),
// the required-reason rule, and the gate-name slug the store's own CHECK
// re-declares. The store halves — the component exists and is not archived,
// the name is free, the rule's fields are in the closed sets, and the
// proposed rule TIGHTENS the current one field by field (a loosening is
// refused at create, naming each loosened field, so nothing inert-and-
// invalid is persisted) — arrive as `Seams` from `cmd/aeon`: this package's
// dependency rule is stdlib + policy + paths, and importing the verify,
// mergerule or store packages would drag their engines into the decision
// code. The literals below are re-declared on purpose (the
// autonomyClassShorthand pattern): TestDeskImportsNothingForbidden keeps
// them that way, and the seam's store half re-runs the core's own
// validators, so a drift is refused there with the core's own words.
//
// Both kinds are restrictive in the AL11 sense, and this file is the shape
// proof: a verify_command payload may only carry op:"add" (an agent may
// propose a NEW gate — adding a required or advisory gate can only make
// landing harder, and it executes only under the policy gate — never
// remove, archive, disable or re-point an existing one), and a merge_rule
// payload is the full proposed rule plus a reason; whether it tightens is
// the seam's Tightens call, which refuses any loosening naming the field.
// There is deliberately no third gate kind: a proposal that loosens the
// gate or the rule is not a shape the desk knows, and the store's CHECK
// (migration 20261102132731) refuses it before SQL.
package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// The two restrictive gate kinds, in the order the store's ProposalKinds
// declares them (it appends them last, beside the kind list).
const (
	KindVerifyCommand Kind = "verify_command"
	KindMergeRule     Kind = "merge_rule"
)

// The gate's closed requirement classes, re-declared (importer pin — see
// the file header). The seam's store half re-runs the store's own
// requirement check, so a drift is refused there with the store's own
// words.
const (
	requirementRequired = "required"
	requirementAdvisory = "advisory"
)

// The command's table bounds, re-declared the same way (the store's
// verify_command CHECK is the backstop a store bypassed).
const (
	maxCommandBytes = 4096
	minTimeoutS     = 1
	maxTimeoutS     = 600
)

// VerifyCommandProposal is verify_command's payload — the gate an agent may
// ADD, and nothing else. op is "add" by shape: an op other than add is a
// parse-level refusal below (VG17: an agent-created proposal may only be
// op:"add"). memoryMB is deliberately NOT a field — the declaration a human
// may attach to a gate is not a shape an agent may smuggle in, and the
// strict decode refuses one.
type VerifyCommandProposal struct {
	Op          string `json:"op"`
	Name        string `json:"name"`
	Command     string `json:"command"`
	Requirement string `json:"requirement"`
	TimeoutS    int    `json:"timeoutS"`
	ComponentID string `json:"componentId"`
	Reason      string `json:"reason"`
}

// MergeRuleProposal is merge_rule's payload: the FULL proposed rule (every
// field, the store's SetMergeRule shape — the seam decodes it strictly)
// plus the why. There is no "patch" shape: a rule is decided whole, and a
// half-rule would let a loosening hide in the fields the patch does not
// name.
type MergeRuleProposal struct {
	Rule   json.RawMessage `json:"rule"`
	Reason string          `json:"reason"`
}

// gateSlugOK mirrors the store's isGateSlug (verify.go): lowercase
// alphanumerics and hyphens, 1..64 — the same shape the migration's CHECK
// is the backstop for. Re-declared, not imported (the file header).
func gateSlugOK(s string) bool {
	if len(s) < 1 || len(s) > 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '-':
		default:
			return false
		}
	}
	return true
}

// validateVerifyCommandPayload is the desk's pure half of
// verify_command's Validate (the shape): a strict decode — a smuggled
// "memoryMB", "enabled" or "position" key is a parse refusal with this
// sentence, byte for byte the one the create door gives — op:"add" (the
// restrictive-only ruling), the gate-name slug, the command's table
// bounds, the requirement's closed set, the timeout's range, a non-blank
// component id, and the reason rule. The store half (the component exists
// and is not archived, the name is free on that component) is the seam's.
// This half deliberately does NOT refuse an already-taken name: that is a
// CONFLICT the commit door must answer with the store's own sentence, and
// refusing it at create would make the create door a second, drift-prone
// copy of the store's read.
func validateVerifyCommandPayload(_ context.Context, payload []byte) error {
	var p VerifyCommandProposal
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return fmt.Errorf("refused: verify_command payload is {\"op\": \"add\", \"name\": \"<slug>\", \"command\": \"<the command>\", \"requirement\": \"required\"|\"advisory\", \"timeoutS\": <1-600>, \"componentId\": \"<node>\", \"reason\": \"<why>\"} and nothing else — %v", err)
	}
	if p.Op != "add" {
		return fmt.Errorf("refused: a verify_command proposal may only be op:\"add\" — removing, archiving or re-pointing a gate is a person's act, and an op of %q is the widening this kind does not carry", p.Op)
	}
	if !gateSlugOK(p.Name) {
		return fmt.Errorf("refused: %q is not a gate name — a slug is lowercase letters, digits and hyphens, 1 to 64", p.Name)
	}
	if p.Command == "" {
		return fmt.Errorf("refused: a gate with an empty command runs nothing")
	}
	if len(p.Command) > maxCommandBytes {
		return fmt.Errorf("refused: the command is %d bytes; the table bound is %d", len(p.Command), maxCommandBytes)
	}
	if strings.ContainsRune(p.Command, 0) {
		return fmt.Errorf("refused: a gate command may not contain a NUL")
	}
	switch p.Requirement {
	case requirementRequired, requirementAdvisory:
	default:
		return fmt.Errorf("refused: the requirement is %q; the closed set is required|advisory", p.Requirement)
	}
	if p.TimeoutS < minTimeoutS || p.TimeoutS > maxTimeoutS {
		return fmt.Errorf("refused: the timeout is %d s; the closed range is %d to %d", p.TimeoutS, minTimeoutS, maxTimeoutS)
	}
	if strings.TrimSpace(p.ComponentID) == "" {
		return fmt.Errorf("refused: verify_command needs the component's id — a gate needs a node it can sit on")
	}
	if s := ProposeReasonSentence(p.Reason, "verify_propose_add"); s != "" {
		return fmt.Errorf("%s", s)
	}
	return nil
}

// validateMergeRulePayload is the desk's pure half of merge_rule's
// Validate (the shape): a strict decode — the payload is the full rule
// object plus the reason and nothing else — the rule is a JSON object
// (a patch shape is refused: a rule is proposed whole), and the reason
// rule. The field vocabulary and the Tightens call (the loosening refusal
// that names each loosened field) are the seam's: they need the store's
// current rule, which this package may not open.
func validateMergeRulePayload(_ context.Context, payload []byte) error {
	var p MergeRuleProposal
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return fmt.Errorf("refused: merge_rule payload is {\"rule\": {<the full proposed rule>}, \"reason\": \"<why>\"} and nothing else — %v", err)
	}
	if len(p.Rule) == 0 {
		return fmt.Errorf("refused: merge_rule needs the full proposed rule — a rule is proposed whole, and a half-rule would let a loosening hide in the fields the patch does not name")
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(p.Rule, &obj); err != nil || len(obj) == 0 {
		return fmt.Errorf("refused: the proposed rule must be a JSON object with the rule's fields — %v is not one", p.Rule)
	}
	if s := ProposeReasonSentence(p.Reason, "merge_rule_propose_tighten"); s != "" {
		return fmt.Errorf("%s", s)
	}
	return nil
}

// Exported for the seam's full Validate halves (cmd/aeon runs the desk
// half first, then its store half): the create door and the commit door
// both run the SAME function through the registry, so a smuggled payload
// is refused with the same sentence at both (TD3, the autonomy pattern).
var (
	ValidateVerifyCommand = validateVerifyCommandPayload
	ValidateMergeRule     = validateMergeRulePayload
)
