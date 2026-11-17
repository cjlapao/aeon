package charter

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Refusal is the DB19-shaped no a door answers with: a kind to branch on
// and one sentence that names the field and the limit. Validation NEVER
// returns a bare error — the caller shows the sentence.
type Refusal struct {
	Kind     string `json:"kind"`
	Sentence string `json:"sentence"`
}

// The caps, ONE home each (T2: "the caps appear once"). A door that wants
// a number asks Vocabulary(); nothing re-spells these literals.
const (
	MaxBodyBytes   = 64 * 1024 // the whole canonical body (GI2)
	MaxGoal        = 4096
	MaxItem        = 512  // a constraint, non-goal, acceptance text, ref
	MaxList        = 50   // constraints, non-goals
	MaxAcceptance  = 100  // acceptance items
	MaxTags        = 30   // stack tags
	MaxTag         = 64   // one stack tag
	MaxVerify      = 20   // proposed verify commands (chosen: a gate list, not a script)
	MaxVerifyBytes = 512  // one proposed verify command
	MaxEnvRef      = 512  // an S59 record reference
	MaxNote        = 1024 // a person's note (write.go)
)

// CheckKinds is the closed check vocabulary — the same three words the
// goal_criterion CHECK admits (GI5), mirrored here and locked to it by the
// store's vocabulary test.
var CheckKinds = []string{"manual", "command", "test"}

var acceptanceID = regexp.MustCompile(`^ac-[1-9][0-9]{0,4}$`)

// Validate is the ONLY charter validator (Behaviour 1): the person's write,
// the proposal's create and the proposal's commit all answer from these
// lines — parity by construction, not by duplicated prose. It reads only
// the body and the Vocab; it stores nothing and enforces nothing.
func Validate(b Body, v Vocab) *Refusal {
	canonical, err := Canonical(b)
	if err != nil {
		return &Refusal{Kind: "body", Sentence: "charter: the body cannot be canonicalized — it is refused"}
	}
	if len(canonical) > MaxBodyBytes {
		return refusal("body-size", "charter: the body is %d bytes, over the %d byte cap — a charter is a statement, not a document dump",
			len(canonical), MaxBodyBytes)
	}
	if strings.ContainsRune(b.Goal, 0) {
		return refusal("goal", "charter: goal contains a NUL byte")
	}
	if b.Goal == "" {
		return refusal("goal", "charter: goal is required — the one-paragraph statement is what a charter IS")
	}
	if len(b.Goal) > MaxGoal {
		return refusal("goal", "charter: goal is %d bytes, over the %d byte cap", len(b.Goal), MaxGoal)
	}
	if r := validateList("constraints", b.Constraints); r != nil {
		return r
	}
	if r := validateList("nonGoals", b.NonGoals); r != nil {
		return r
	}
	if len(b.Acceptance) > MaxAcceptance {
		return refusal("acceptance", "charter: %d acceptance items, over the %d cap", len(b.Acceptance), MaxAcceptance)
	}
	seen := map[string]bool{}
	for i, a := range b.Acceptance {
		if !acceptanceID.MatchString(a.ID) {
			return refusal("acceptance-id", "charter: acceptance[%d].id %q is not the stable form ac-<n>", i, a.ID)
		}
		if seen[a.ID] {
			return refusal("acceptance-id", "charter: acceptance id %q appears twice — the id is STABLE and must be unique", a.ID)
		}
		seen[a.ID] = true
		if a.Text == "" || len(a.Text) > MaxItem || strings.ContainsRune(a.Text, 0) {
			return refusal("acceptance-text", "charter: acceptance[%d].text must be 1–%d bytes with no NUL", i, MaxItem)
		}
		switch a.Check {
		case "":
			if a.Ref != "" {
				return refusal("acceptance-ref", "charter: acceptance[%d].ref names a check that is not there — give it a check (manual, command, test) or drop the ref", i)
			}
		case "manual":
			if a.Ref != "" {
				return refusal("acceptance-ref", "charter: acceptance[%d].check is manual, which checks by a human reading — a manual item carries no ref", i)
			}
		case "command", "test":
			if a.Ref == "" {
				return refusal("acceptance-ref", "charter: acceptance[%d].check is %q, which must name its command or test in ref — an unnamed check is a phantom", i, a.Check)
			}
			if len(a.Ref) > MaxItem || strings.ContainsRune(a.Ref, 0) {
				return refusal("acceptance-ref", "charter: acceptance[%d].ref must be 1–%d bytes with no NUL", i, MaxItem)
			}
		default:
			return refusal("acceptance-check", "charter: acceptance[%d].check %q is not one of %s", i, a.Check, strings.Join(CheckKinds, ", "))
		}
	}
	if len(b.Stack) > MaxTags {
		return refusal("stack", "charter: %d stack tags, over the %d cap", len(b.Stack), MaxTags)
	}
	for i, tag := range b.Stack {
		if tag == "" || len(tag) > MaxTag || strings.ContainsAny(tag, "\x00\n") {
			return refusal("stack", "charter: stack tag %d must be 1–%d bytes with no NUL or newline", i, MaxTag)
		}
	}
	if b.Proposed == nil {
		return nil
	}
	p := *b.Proposed
	if p.BudgetUSD != nil && *p.BudgetUSD < 0 {
		return refusal("budget", "charter: proposed.budgetUsd is %s — a negative budget is not an unknown (P31: leave it out for unknown)", formatNum(*p.BudgetUSD))
	}
	if p.Autonomy != "" {
		if len(v.Autonomy) == 0 {
			// OD12: the enum is S56's; typing it here would be a second
			// vocabulary. Absent seam ⇒ the field has no meaning yet.
			return refusal("autonomy", "charter: proposed.autonomy cannot be used — there is no autonomy vocabulary (S56) to read from")
		}
		if !contains(v.Autonomy, p.Autonomy) {
			return refusal("autonomy", "charter: proposed.autonomy %q is not one of %s", p.Autonomy, strings.Join(v.Autonomy, ", "))
		}
	}
	if p.EnvironmentRef != "" && (len(p.EnvironmentRef) > MaxEnvRef || strings.ContainsAny(p.EnvironmentRef, "\x00\n")) {
		return refusal("environment-ref", "charter: proposed.environmentRef must be 1–%d bytes with no NUL or newline", MaxEnvRef)
	}
	if len(p.Verify) > MaxVerify {
		return refusal("verify", "charter: %d proposed verify commands, over the %d cap", len(p.Verify), MaxVerify)
	}
	for i, cmd := range p.Verify {
		if cmd == "" || len(cmd) > MaxVerifyBytes || strings.ContainsAny(cmd, "\x00\n") {
			return refusal("verify", "charter: proposed.verify[%d] must be 1–%d bytes with no NUL and no newline — one command line", i, MaxVerifyBytes)
		}
	}
	return nil
}

func validateList(name string, items []string) *Refusal {
	if len(items) > MaxList {
		return refusal(name, "charter: %d %s, over the %d cap", len(items), name, MaxList)
	}
	for i, s := range items {
		if s == "" || len(s) > MaxItem || strings.ContainsRune(s, 0) {
			return refusal(name, "charter: %s[%d] must be 1–%d bytes with no NUL", name, i, MaxItem)
		}
	}
	return nil
}

func refusal(kind, format string, args ...any) *Refusal {
	return &Refusal{Kind: kind, Sentence: fmt.Sprintf(format, args...)}
}

func contains(set []string, want string) bool {
	for _, s := range set {
		if s == want {
			return true
		}
	}
	return false
}

func formatNum(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}
