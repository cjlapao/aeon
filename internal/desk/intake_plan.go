// intake_plan.go — S61 T5 (GI7/OD4): the nineteenth proposal kind,
// intake_plan, as a registry entry in this package.
//
// The plan arrives whole or not at all: a strict decode of the closed GI7
// schema (a smuggled "path", "targetPath", "token", "secret", "owner",
// "actor" or "publish" is a PARSE refusal, so no payload can name where it
// lands, what credential opens anything, or who commits it — the person
// chooses the target path and the publish choice at intake.commit, which is
// exactly why this kind does not commit generically at all), the caps
// (≤ 8 components, ≤ 50 epics, ≤ 200 goals, ≤ 500 items, ≤ 100 criteria per
// goal, 128 KiB of JSON around them), the reference-integrity rules (slugs
// and epic keys unique, goal epicKey and item componentSlug/epicKey name
// things the plan itself carries, goal componentSlugs ⊆ the plan's
// components), and the role law (root and child come TOGETHER; when the
// plan carries ≥ 2 components there is exactly ONE root and a projectName
// — a multi-component plan without a project is a stack of projects, not a
// project).
//
// The slug rule is the ONE shared function: ValidComponentSlug below holds
// the regex daemon/components.go has always enforced on create (its literal
// moved here, the daemon now calls through), and desk is inside the daemon's
// import set, not the reverse — so there is one definition and no cycle. The
// charter-body validation (charter.Validate) and the template existence
// (a store read) are the seam's halves from cmd/aeon — the charter package
// imports the store and this package imports neither — and the seam's
// Validate (which runs this file's shape first) is what BOTH doors run
// (S35 TD3).
//
// Apply is NOT the generic one. An intake_plan committed through
// proposal.commit meets the fixed sentence below at the desk, before any
// write, at both the arm and the commit door; intake.commit (T6/T7) is the
// one door that carries a target path and a publish choice, and nothing
// about a scaffolded repository, a git identity or a remote lives in this
// payload to apply with.

package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// KindIntakePlan is S61 T5's kind, nineteenth in the store's order
// (migration 20261105110000_proposal_kinds_intake; the store mirror is
// store.ProposalKinds).
const KindIntakePlan Kind = "intake_plan"

// IntakeCommitRefusal is the fixed sentence the generic commit door answers
// an intake_plan with (GI7/OD4, tested verbatim). It is a constant because
// the arm, the commit and the test all quote the SAME string — a refusal a
// person cannot grep is a refusal that reads like a crash.
const IntakeCommitRefusal = "refused: this proposal needs a target path and a publish choice — use `intake.commit`"

// The payload caps (the file header states which are spec literals and
// which are restatements of charter/roadmap bounds for the same kind of
// field): a plan this side of the caps is one a person reads at the desk
// before committing it, and 128 KiB of JSON is the envelope every intake
// payload shares with charter_save.
const (
	maxIntakePlanPayloadBytes = 128 * 1024
	maxIntakeComponents       = 8
	maxIntakeEpics            = 50
	maxIntakeGoals            = 200
	maxIntakeItems            = 500
	maxIntakeCriteriaPerGoal  = 100 // charter.MaxAcceptance, re-spelled (the desk imports neither)
	maxIntakeIDBytes          = 64
	maxIntakeNameBytes        = 300  // component name, every title — what a board renders (roadmap's bound)
	maxIntakeDescriptionBytes = 1024 // a component description, an epic objective — a paragraph, not an essay
	maxIntakeBranchBytes      = 128
	maxIntakeTags             = 30   // charter.MaxTags, re-spelled
	maxIntakeTagBytes         = 64   // charter.MaxTag, re-spelled
	maxIntakeKeyBytes         = 64   // an epic key
	maxIntakeBodyNoteBytes    = 4096 // charter.MaxGoal, re-spelled — the goal's body rides the charter, this is its note
	maxIntakeTextBytes        = 512  // charter.MaxItem, re-spelled — a criterion's text and ref
	maxIntakeLabelBytes       = 32   // priority label, estimate — a word or two
)

// IntakePlanCaps is the validator's own caps as one answer — the intake
// vocabulary door renders them and the vocabulary test compares against
// THIS, never a re-spelling (S61 T7: numbers published must be numbers
// enforced, from one place).
type IntakePlanCaps struct {
	PayloadBytes    int `json:"payloadBytes"`
	Components      int `json:"components"`
	Epics           int `json:"epics"`
	Goals           int `json:"goals"`
	Items           int `json:"items"`
	CriteriaPerGoal int `json:"criteriaPerGoal"`
}

// IntakePlanCapsOf is the one call that reads the constants.
func IntakePlanCapsOf() IntakePlanCaps {
	return IntakePlanCaps{
		PayloadBytes:    maxIntakePlanPayloadBytes,
		Components:      maxIntakeComponents,
		Epics:           maxIntakeEpics,
		Goals:           maxIntakeGoals,
		Items:           maxIntakeItems,
		CriteriaPerGoal: maxIntakeCriteriaPerGoal,
	}
}

// IntakeRoles is the closed role vocabulary a plan component may carry
// (GI7): the root is the project, the children hang under it, standalone is
// a one-component plan that is its own project.
var IntakeRoles = []string{"root", "child", "standalone"}

// intakeChecks is the closed check vocabulary — the same three words the
// goal_criterion CHECK admits (GI5) and charter.CheckKinds mirrors; the desk
// re-spells them because it imports neither the store nor charter.
var intakeChecks = []string{"manual", "command", "test"}

// componentSlugRE is daemon/components.go's slug regex — the literal MOVED
// here (2026-11-05, S61 T5) so the plan validator and the component create
// door enforce one rule from one place; ValidComponentSlug is the shared
// function both call.
var componentSlugRE = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// ValidComponentSlug answers whether s is a component slug — kebab-case,
// one shared function for the intake plan validator (here) and the
// daemon's component create/update doors (internal/daemon/components.go,
// which calls through this one).
func ValidComponentSlug(s string) bool { return componentSlugRE.MatchString(s) }

// IntakePlanProposal is the GI7 closed payload — WHAT to scaffold and file,
// never WHERE on disk, WITH WHICH credential, or AS WHOM. Charter is raw
// JSON decoded and validated by the charter package at the seam; the desk
// asks only that it be an object.
type IntakePlanProposal struct {
	IntakeID    string                `json:"intakeId"`
	Charter     json.RawMessage       `json:"charter"`
	Components  []IntakePlanComponent `json:"components"`
	ProjectName string                `json:"projectName,omitempty"`
	Epics       []IntakePlanEpic      `json:"epics"`
	Goals       []IntakePlanGoal      `json:"goals"`
	Items       []IntakePlanItem      `json:"items"`
	Reason      string                `json:"reason"`
}

// IntakePlanComponent is one component the plan asks to exist: a name, a
// slug, a TEMPLATE SLUG (existence is the seam's store read), a role, and
// nothing else — no path, no remote, no owner.
type IntakePlanComponent struct {
	Name          string   `json:"name"`
	Slug          string   `json:"slug"`
	Template      string   `json:"template"`
	Role          string   `json:"role"`
	Description   string   `json:"description,omitempty"`
	DefaultBranch string   `json:"defaultBranch,omitempty"`
	Tags          []string `json:"tags,omitempty"`
}

// IntakePlanEpic is one epic: its key is what goals and items point at.
type IntakePlanEpic struct {
	Key       string `json:"key"`
	Title     string `json:"title"`
	Objective string `json:"objective"`
}

// IntakePlanGoal is one goal: which epic (optional), which components it
// spans (non-empty, ⊆ the plan), and its criteria.
type IntakePlanGoal struct {
	EpicKey        string                `json:"epicKey,omitempty"`
	Title          string                `json:"title"`
	Type           string                `json:"type"`
	BodyNote       string                `json:"bodyNote,omitempty"`
	ComponentSlugs []string              `json:"componentSlugs"`
	Criteria       []IntakePlanCriterion `json:"criteria,omitempty"`
}

// IntakePlanCriterion is one acceptance criterion — text plus the closed
// check kind and an optional ref (GI5's three words, re-spelled).
type IntakePlanCriterion struct {
	Text  string `json:"text"`
	Check string `json:"check,omitempty"`
	Ref   string `json:"ref,omitempty"`
}

// IntakePlanItem is one roadmap item: WHICH component it belongs to, under
// which epic (optional), a title, and the two labels a board shows.
// Priority names "unknown" or a label — the ranking ladder is a person's
// (RM8), and nothing here sizes or ranks.
type IntakePlanItem struct {
	ComponentSlug string `json:"componentSlug"`
	EpicKey       string `json:"epicKey,omitempty"`
	Title         string `json:"title"`
	Priority      string `json:"priority,omitempty"`
	Estimate      string `json:"estimate,omitempty"`
}

// validateIntakePlanPayload is the desk's pure half of Validate (the shape,
// the caps, the reference integrity, the role law). The charter half
// (charter.Validate over the decoded charter) and the store half (every
// component's template exists) are the seam's, and the seam's Validate
// (which runs this function first) is what BOTH doors run.
func validateIntakePlanPayload(_ context.Context, payload []byte) error {
	if len(payload) > maxIntakePlanPayloadBytes {
		return fmt.Errorf("refused: the intake_plan payload is %d bytes and the cap is %d — a plan a person cannot read at the desk is not a plan",
			len(payload), maxIntakePlanPayloadBytes)
	}
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.DisallowUnknownFields()
	var p IntakePlanProposal
	if err := dec.Decode(&p); err != nil {
		return fmt.Errorf("refused: intake_plan payload is not the closed plan {intakeId, charter, components, projectName, epics, goals, items, reason} and carries no field besides them (no path, no secret, no actor): %v", err)
	}
	if err := dec.Decode(&json.RawMessage{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("refused: intake_plan payload carries trailing JSON: %v", err)
	}
	return validateIntakePlan(p)
}

// validateIntakePlan is the rule walk over the decoded plan — split from the
// decode so the seam, which decodes once for its own halves too, runs the
// exact same walk without a second parse.
func validateIntakePlan(p IntakePlanProposal) error {
	if strings.TrimSpace(p.IntakeID) == "" || len(p.IntakeID) > maxIntakeIDBytes {
		return fmt.Errorf("refused: intake_plan needs an intakeId of 1 to %d bytes — the plan is one run's ledger row, and it must name itself", maxIntakeIDBytes)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(p.Charter, &obj); err != nil || obj == nil {
		return fmt.Errorf("refused: intake_plan charter must be a JSON object — the GI2 body is the shape, and the charter package answers its fields")
	}
	if len(p.Components) == 0 || len(p.Components) > maxIntakeComponents {
		return fmt.Errorf("refused: intake_plan carries %d components; the plan names between 1 and %d", len(p.Components), maxIntakeComponents)
	}
	if len(p.Epics) > maxIntakeEpics {
		return fmt.Errorf("refused: intake_plan carries %d epics and the cap is %d", len(p.Epics), maxIntakeEpics)
	}
	if len(p.Goals) > maxIntakeGoals {
		return fmt.Errorf("refused: intake_plan carries %d goals and the cap is %d", len(p.Goals), maxIntakeGoals)
	}
	if len(p.Items) > maxIntakeItems {
		return fmt.Errorf("refused: intake_plan carries %d items and the cap is %d", len(p.Items), maxIntakeItems)
	}
	if s := ProposeReasonSentence(p.Reason, "intake_plan"); s != "" {
		return errors.New(s)
	}

	slugs := map[string]bool{}
	roots, children := 0, 0
	for i, c := range p.Components {
		if strings.TrimSpace(c.Name) == "" || len(c.Name) > maxIntakeNameBytes {
			return fmt.Errorf("refused: component %d needs a name of 1 to %d bytes", i, maxIntakeNameBytes)
		}
		if !ValidComponentSlug(c.Slug) {
			return fmt.Errorf("refused: component slug %q is not kebab-case (lowercase words joined by single hyphens) — the same rule the component registry enforces", c.Slug)
		}
		if slugs[c.Slug] {
			return fmt.Errorf("refused: component slug %q appears twice — one plan, one component per slug", c.Slug)
		}
		slugs[c.Slug] = true
		if strings.TrimSpace(c.Template) == "" {
			return fmt.Errorf("refused: component %q names no template — the plan says WHAT to scaffold from, and an empty slug asks nothing", c.Slug)
		}
		switch c.Role {
		case "root":
			roots++
		case "child":
			children++
		case "standalone":
		default:
			return fmt.Errorf("refused: component %q has role %q — the closed set is %s", c.Slug, c.Role, strings.Join(IntakeRoles, ", "))
		}
		if len(c.Description) > maxIntakeDescriptionBytes {
			return fmt.Errorf("refused: component %q description is %d bytes and the cap is %d", c.Slug, len(c.Description), maxIntakeDescriptionBytes)
		}
		if len(c.DefaultBranch) > maxIntakeBranchBytes {
			return fmt.Errorf("refused: component %q defaultBranch is %d bytes and the cap is %d", c.Slug, len(c.DefaultBranch), maxIntakeBranchBytes)
		}
		if len(c.Tags) > maxIntakeTags {
			return fmt.Errorf("refused: component %q carries %d tags and the cap is %d", c.Slug, len(c.Tags), maxIntakeTags)
		}
		for _, t := range c.Tags {
			if strings.TrimSpace(t) == "" || len(t) > maxIntakeTagBytes {
				return fmt.Errorf("refused: component %q carries a tag of no bytes or over %d — a tag is a word", c.Slug, maxIntakeTagBytes)
			}
		}
	}
	if (roots > 0) != (children > 0) {
		return fmt.Errorf("refused: root and child roles come together — the plan names %d root and %d child; one project is a root WITH its children, and a lone component is standalone", roots, children)
	}
	if len(p.Components) >= 2 {
		if roots != 1 {
			return fmt.Errorf("refused: a plan of %d components names %d roots — exactly one", len(p.Components), roots)
		}
		if strings.TrimSpace(p.ProjectName) == "" {
			return fmt.Errorf("refused: a plan of %d components needs a projectName — a stack of components without a project is a stack, not a project", len(p.Components))
		}
	} else if roots > 0 || children > 0 {
		return fmt.Errorf("refused: a one-component plan is standalone — root and child roles come together, and this plan has no company for either")
	}

	epicKeys := map[string]bool{}
	for _, e := range p.Epics {
		if strings.TrimSpace(e.Key) == "" || len(e.Key) > maxIntakeKeyBytes {
			return fmt.Errorf("refused: an epic needs a key of 1 to %d bytes", maxIntakeKeyBytes)
		}
		if epicKeys[e.Key] {
			return fmt.Errorf("refused: epic key %q appears twice — goals and items point at one thing per key", e.Key)
		}
		epicKeys[e.Key] = true
		if strings.TrimSpace(e.Title) == "" || len(e.Title) > maxIntakeNameBytes {
			return fmt.Errorf("refused: epic %q needs a title of 1 to %d bytes", e.Key, maxIntakeNameBytes)
		}
		if len(e.Objective) > maxIntakeDescriptionBytes {
			return fmt.Errorf("refused: epic %q objective is %d bytes and the cap is %d", e.Key, len(e.Objective), maxIntakeDescriptionBytes)
		}
	}

	for gi, g := range p.Goals {
		if strings.TrimSpace(g.Title) == "" || len(g.Title) > maxIntakeNameBytes {
			return fmt.Errorf("refused: goal %d needs a title of 1 to %d bytes", gi+1, maxIntakeNameBytes)
		}
		if strings.TrimSpace(g.Type) == "" {
			return fmt.Errorf("refused: goal %d names no type — goal and task are decided at the desk, and the plan must say which", gi+1)
		}
		if len(g.BodyNote) > maxIntakeBodyNoteBytes {
			return fmt.Errorf("refused: goal %d bodyNote is %d bytes and the cap is %d", gi+1, len(g.BodyNote), maxIntakeBodyNoteBytes)
		}
		if g.EpicKey != "" && !epicKeys[g.EpicKey] {
			return fmt.Errorf("refused: goal %d names epicKey %q and the plan carries no such epic", gi+1, g.EpicKey)
		}
		if len(g.ComponentSlugs) == 0 {
			return fmt.Errorf("refused: goal %d names no component — a goal that spans nothing is not the plan's", gi+1)
		}
		for _, s := range g.ComponentSlugs {
			if !slugs[s] {
				return fmt.Errorf("refused: goal %d names component %q and the plan carries no such component", gi+1, s)
			}
		}
		if len(g.Criteria) > maxIntakeCriteriaPerGoal {
			return fmt.Errorf("refused: goal %d carries %d criteria and the cap is %d", gi+1, len(g.Criteria), maxIntakeCriteriaPerGoal)
		}
		for ci, c := range g.Criteria {
			if strings.TrimSpace(c.Text) == "" || len(c.Text) > maxIntakeTextBytes {
				return fmt.Errorf("refused: goal %d criterion %d needs text of 1 to %d bytes", gi+1, ci+1, maxIntakeTextBytes)
			}
			if c.Check != "" && !oneOf(intakeChecks, c.Check) {
				return fmt.Errorf("refused: goal %d criterion %d check %q is not one of %s", gi+1, ci+1, c.Check, strings.Join(intakeChecks, ", "))
			}
			if len(c.Ref) > maxIntakeTextBytes {
				return fmt.Errorf("refused: goal %d criterion %d ref is %d bytes and the cap is %d", gi+1, ci+1, len(c.Ref), maxIntakeTextBytes)
			}
		}
	}

	for ii, it := range p.Items {
		if !slugs[it.ComponentSlug] {
			return fmt.Errorf("refused: item %d names component %q and the plan carries no such component", ii+1, it.ComponentSlug)
		}
		if it.EpicKey != "" && !epicKeys[it.EpicKey] {
			return fmt.Errorf("refused: item %d names epicKey %q and the plan carries no such epic", ii+1, it.EpicKey)
		}
		if strings.TrimSpace(it.Title) == "" || len(it.Title) > maxIntakeNameBytes {
			return fmt.Errorf("refused: item %d needs a title of 1 to %d bytes", ii+1, maxIntakeNameBytes)
		}
		if len(it.Priority) > maxIntakeLabelBytes {
			return fmt.Errorf("refused: item %d priority label is %d bytes and the cap is %d — \"unknown\" or a word", ii+1, len(it.Priority), maxIntakeLabelBytes)
		}
		if len(it.Estimate) > maxIntakeLabelBytes {
			return fmt.Errorf("refused: item %d estimate is %d bytes and the cap is %d — a label, not a decision", ii+1, len(it.Estimate), maxIntakeLabelBytes)
		}
	}
	return nil
}

func oneOf(set []string, v string) bool {
	for _, s := range set {
		if s == v {
			return true
		}
	}
	return false
}

// ValidateIntakePlan is the exported entry the cmd/aeon seam calls — the
// same function the built-in registry entry runs, so the seam's full half
// starts from these exact sentences (TD3's one-validator rule).
func ValidateIntakePlan(ctx context.Context, payload []byte) error {
	return validateIntakePlanPayload(ctx, payload)
}

// ValidateIntakePlanDecoded is the seam's second entry: the full half
// decodes the strict payload once for its own store/charter reads and then
// runs THIS rule walk — the one validator, still one function at both
// doors, without a duplicate decode.
func ValidateIntakePlanDecoded(p IntakePlanProposal) error {
	return validateIntakePlan(p)
}

// intakePlanNeverAlreadyApplied answers false for a reason, not an absence:
// an intake_plan has ALREADY-APPLIED nothing, generically, ever — its apply
// is the refusal below, so no crash-retry can find a footprint to complete
// (intake.commit owns that ledger, T6). A nil half would answer "missing
// AlreadyApplied" and hide the refusal the kind exists to give.
func intakePlanNeverAlreadyApplied(context.Context, []byte) (bool, error) {
	return false, nil
}

// applyIntakePlanIsRefused is the built-in Apply: the fixed sentence, at the
// arm and at the commit door alike, before any write. No seam may REPLACE it
// (cmd/aeon's intake_plan seam supplies Validate only) — the day someone
// wants intake_plan to commit generically is the day they discover why it
// cannot: there is no target path and no publish choice in the payload, and
// inventing one at the apply is exactly what GI7 refuses.
func applyIntakePlanIsRefused(context.Context, io.Writer, []byte) error {
	return errors.New(IntakeCommitRefusal)
}
