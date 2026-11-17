package main

// mergerule_verbs.go — S57 T10 (VG20): `aeon merge-rule` — the merge
// rule's person's door. Six verbs over mergerule.*; the one seatbelt on
// this plane lives here: a set that LOOSENS the effective rule needs
// --yes, decided by the daemon's OWN classifier (mergerule.Tightens —
// the same pure function the daemon's propose door runs), printed with
// its own field names, sending nothing without the flag (P5: a
// seatbelt, not a control — the real control is the human reading it).

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/cjlapao/aeon/internal/mergerule"
	"github.com/cjlapao/aeon/internal/protocol"
)

// cmdMergeRule dispatches `aeon merge-rule …` (T0 §4's six rows).
func cmdMergeRule(w io.Writer, args []string) error {
	if len(args) == 0 {
		return &usageError{msg: "aeon merge-rule: a verb is required — " + mergeRuleVerbLine()}
	}
	verb, rest := args[0], args[1:]
	switch verb {
	case "get":
		return runMergeRuleGet(w, rest)
	case "set":
		return runMergeRuleSet(w, rest)
	case "clear":
		return runMergeRuleClear(w, rest)
	case "preview":
		return runMergeRulePreview(w, rest)
	case "changes":
		return runMergeRuleChanges(w, rest)
	case "vocabulary":
		return runMergeRuleVocabulary(w, rest)
	case "help", "-h", "--help":
		printMergeRuleUsage(w)
		return nil
	default:
		return &usageError{msg: fmt.Sprintf("aeon merge-rule: unknown verb %q — "+mergeRuleVerbLine(), verb)}
	}
}

func mergeRuleVerbLine() string {
	return "get · set · clear · preview · changes · vocabulary"
}

// printMergeRuleUsage: the closed sets are named offline from the SAME
// Go constants the daemon's mergerule.vocabulary builds from — no set
// typed twice.
func printMergeRuleUsage(w io.Writer) {
	fmt.Fprintf(w, `aeon merge-rule — the component's merge gate (S57)

Verbs: %s

  get --component C [--effective] [--json]
  set --component C [--disable] [--require-local=true|false] [--require-dod %s]
      [--required-check NAME …] [--checks-policy %s] [--no-checks %s]
      [--review-mode %s] [--threads-resolved=true|false] [--method %s]
      [--yes] [--json]
      (--yes is required when the change LOOSENS the effective rule)
  clear --component C --yes [--json]
  preview --component C [--pr N] [--json]
  changes [--component C] [--limit N] [--json]
  vocabulary [--json]

The closed sets above are the daemon's vocabulary; this usage names
them offline from the same Go constants.
`, mergeRuleVerbLine(),
		strings.Join(mergeruleRequireDodWords(), " | "),
		strings.Join(mergeruleChecksPolicyWords(), " | "),
		strings.Join(mergeruleNoChecksWords(), " | "),
		strings.Join(mergeruleReviewModeWords(), " | "),
		strings.Join(mergeruleMethodWords(), " | "))
}

// The vocabulary words, read from their one home in internal/mergerule
// (the daemon's mergerule.vocabulary builds its answer from the same
// constants — this usage has no private copy).
func mergeruleRequireDodWords() []string {
	return []string{mergerule.RequireDoDNone, mergerule.RequireDoDAllMet}
}
func mergeruleChecksPolicyWords() []string {
	return []string{mergerule.ChecksReported, mergerule.ChecksRequired}
}
func mergeruleNoChecksWords() []string {
	return []string{mergerule.NoChecksAllow, mergerule.NoChecksRefuse}
}
func mergeruleReviewModeWords() []string {
	return []string{mergerule.ReviewModeForge, mergerule.ReviewModeAeon, mergerule.ReviewModeEither, mergerule.ReviewModeBoth}
}
func mergeruleMethodWords() []string {
	return []string{mergerule.MethodSquash, mergerule.MethodMerge, mergerule.MethodRebase}
}

func runMergeRuleGet(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("merge-rule get", flag.ContinueOnError)
	fs.SetOutput(w)
	component := fs.String("component", "", "the component (required)")
	effective := fs.Bool("effective", false, "the rule that governs, walking to the root and the baseline")
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *component == "" {
		return &usageError{msg: "aeon merge-rule get: --component is required"}
	}
	door, err := gateSelect()
	if err != nil {
		return err
	}
	defer door.Close()
	res, err := door.MergeRuleGet(context.Background(), *component, *effective)
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(w, res)
	}
	return renderMergeRuleGet(w, res)
}

func renderMergeRuleGet(w io.Writer, res protocol.MergeRuleGetResult) error {
	fmt.Fprintf(w, "component %s   source %s\n", res.ComponentID, res.Source)
	if res.Rule == nil {
		fmt.Fprintln(w, "  (no rule — the source above says who governs: baseline means the standing rule)")
	} else {
		renderRuleWire(w, res.Rule.ComponentID, res.Rule.MergeRuleWire, res.Rule.Revision)
	}
	if res.Floor != nil {
		renderFloor(w, res.Floor)
	}
	if res.LastChange != nil {
		c := res.LastChange
		fmt.Fprintf(w, "last change r%d  %s  %s/%s  %s\n", c.Revision, c.Op, c.ActorKind, c.ActorID, c.Summary)
	}
	return nil
}

func renderRuleWire(w io.Writer, componentID string, r protocol.MergeRuleWire, revision int) {
	fmt.Fprintf(w, "rule for %s (revision %d):\n", componentID, revision)
	fmt.Fprintf(w, "  enabled              %v\n", r.Enabled)
	fmt.Fprintf(w, "  require-local        %v\n", r.RequireLocal)
	fmt.Fprintf(w, "  require-dod          %s\n", r.RequireDoD)
	fmt.Fprintf(w, "  required-checks      %s\n", orNoneList(r.RequiredChecks))
	fmt.Fprintf(w, "  checks-policy        %s\n", r.ChecksPolicy)
	fmt.Fprintf(w, "  no-checks            %s\n", r.NoChecks)
	fmt.Fprintf(w, "  review-mode          %s\n", r.ReviewMode)
	fmt.Fprintf(w, "  aeon-review-agents   %s\n", orNoneList(r.AeonReviewAgents))
	fmt.Fprintf(w, "  threads-resolved     %v\n", r.RequireThreadsResolved)
	fmt.Fprintf(w, "  method               %s\n", r.Method)
}

func orNoneList(ss []string) string {
	if len(ss) == 0 {
		return "(none)"
	}
	return strings.Join(ss, ", ")
}

func renderFloor(w io.Writer, f *protocol.FloorView) {
	if f.OK {
		fmt.Fprintln(w, "floor    ready — the floor's ingredients are all here")
		return
	}
	fmt.Fprintln(w, "floor    NOT ready:")
	for _, m := range f.Missing {
		fmt.Fprintf(w, "  missing %s — %s\n", m.Code, m.Sentence)
	}
}

// stringList is a repeatable string flag (--required-check).
type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}

func runMergeRuleSet(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("merge-rule set", flag.ContinueOnError)
	fs.SetOutput(w)
	component := fs.String("component", "", "the component (required)")
	disable := fs.Bool("disable", false, "switch the rule off (a disabled rule merges freely — the daemon says so)")
	requireLocal := fs.String("require-local", "", "true | false")
	requireDod := fs.String("require-dod", "", "none | all_met")
	var requiredChecks stringList
	fs.Var(&requiredChecks, "required-check", "a required check name (repeatable)")
	checksPolicy := fs.String("checks-policy", "", "reported | required")
	noChecks := fs.String("no-checks", "", "allow | refuse")
	reviewMode := fs.String("review-mode", "", "forge | aeon | either | both")
	threadsResolved := fs.String("threads-resolved", "", "true | false")
	method := fs.String("method", "", "squash | merge | rebase")
	yes := fs.Bool("yes", false, "send the rule even when it loosens the effective rule")
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *component == "" {
		return &usageError{msg: "aeon merge-rule set: --component is required"}
	}
	door, err := gateSelect()
	if err != nil {
		return err
	}
	defer door.Close()

	// The candidate is the EFFECTIVE rule with the typed flags applied —
	// the CLI invents no defaults (the daemon's get is the base), and
	// value words are the daemon's problem (its refusals print verbatim).
	base, err := door.MergeRuleGet(context.Background(), *component, true)
	if err != nil {
		return err
	}
	wire := protocol.MergeRuleWire{}
	if base.Rule != nil {
		wire = base.Rule.MergeRuleWire
	}
	if *disable {
		// --disable present switches the rule off; absent (or
		// --disable=false, the same request either way) keeps the
		// effective rule's own Enabled as read.
		wire.Enabled = false
	}
	if *requireLocal != "" {
		b, err := parseGateBool(*requireLocal)
		if err != nil {
			return err
		}
		wire.RequireLocal = b
	}
	if *requireDod != "" {
		wire.RequireDoD = *requireDod
	}
	if len(requiredChecks) > 0 {
		wire.RequiredChecks = []string(requiredChecks)
	}
	if *checksPolicy != "" {
		wire.ChecksPolicy = *checksPolicy
	}
	if *noChecks != "" {
		wire.NoChecks = *noChecks
	}
	if *reviewMode != "" {
		wire.ReviewMode = *reviewMode
	}
	if *threadsResolved != "" {
		b, err := parseGateBool(*threadsResolved)
		if err != nil {
			return err
		}
		wire.RequireThreadsResolved = b
	}
	if *method != "" {
		wire.Method = *method
	}

	// The seatbelt: the daemon's OWN classifier decides loosening —
	// mergerule.Tightens, the same pure function the propose door
	// refuses non-tightening agent proposals with. The field names it
	// returns are printed, not re-worded. Without --yes, nothing is
	// sent: the flag is the person, not the CLI, deciding.
	//
	// The base is the EFFECTIVE rule: with no row of its own the
	// component is governed by the root's or the baseline's — the get
	// says which by Source, and for the baseline we ask the daemon's
	// vocabulary read (its Baseline is the standing rule's own wire).
	baseWire, err := effectiveWire(context.Background(), door, base)
	if err != nil {
		return err
	}
	current, err := ruleFromWire(baseWire)
	if err != nil {
		return err
	}
	proposed, err := ruleFromWire(wire)
	if err != nil {
		return err
	}
	ok, loosened := mergerule.Tightens(current, proposed)
	if !ok && !*yes {
		if len(loosened) == 0 {
			// The one incomparable pair (a forge↔aeon review-mode
			// switch): no direction proves it tighter or looser —
			// the person decides with eyes open.
			fmt.Fprintln(stderrW, "merge-rule set switches the review mode in a way no direction can be called tighter or looser — nothing was sent; rerun with --yes to send it knowingly")
			return &usageError{msg: "aeon merge-rule set: the review-mode switch needs --yes"}
		}
		sort.Strings(loosened)
		fmt.Fprintf(stderrW, "merge-rule set would LOOSEN the effective rule: %s — nothing was sent; rerun with --yes to loosen it\n", strings.Join(loosened, ", "))
		return &usageError{msg: "aeon merge-rule set: loosening the effective rule needs --yes"}
	}

	in := protocol.MergeRuleSetInput{
		ComponentID:            *component,
		Enabled:                wire.Enabled,
		RequireLocal:           wire.RequireLocal,
		RequireDoD:             wire.RequireDoD,
		RequiredChecks:         wire.RequiredChecks,
		ChecksPolicy:           wire.ChecksPolicy,
		NoChecks:               wire.NoChecks,
		ReviewMode:             wire.ReviewMode,
		AeonReviewAgents:       wire.AeonReviewAgents,
		RequireThreadsResolved: wire.RequireThreadsResolved,
		Method:                 wire.Method,
	}
	res, err := door.MergeRuleSet(context.Background(), in)
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(w, res)
	}
	renderRuleWire(w, res.Rule.ComponentID, res.Rule.MergeRuleWire, res.Rule.Revision)
	if !res.Rule.Enabled {
		fmt.Fprintln(w, "this rule is DISABLED — merges under it are ungated")
	}
	return nil
}

// effectiveWire is the wire the effective read actually governs by: a
// get with a Rule row carries it; with no row the baseline governs (the
// get's Source says so), and the candidate is compared against THAT —
// comparing against nothing would let a first set loosen the standing
// rule quietly. The baseline comes from the daemon's own vocabulary
// read (offline: the direct door serves the same mergerule.Baseline).
func effectiveWire(ctx context.Context, door gateDoor, res protocol.MergeRuleGetResult) (protocol.MergeRuleWire, error) {
	if res.Rule != nil {
		return res.Rule.MergeRuleWire, nil
	}
	v, err := door.MergeRuleVocabulary(ctx)
	if err != nil {
		return protocol.MergeRuleWire{}, err
	}
	return v.Baseline, nil
}

// ruleFromWire reshapes the flat wire into the classifier's Rule via a
// JSON round-trip: the field names are IDENTICAL (MergeRuleWire is the
// Rule's policy half, name for name), so this is a name-preserving
// transport, not a second mapping anyone can drift.
func ruleFromWire(w protocol.MergeRuleWire) (mergerule.Rule, error) {
	raw, err := json.Marshal(w)
	if err != nil {
		return mergerule.Rule{}, err
	}
	var r mergerule.Rule
	err = json.Unmarshal(raw, &r)
	return r, err
}

func parseGateBool(v string) (bool, error) {
	switch v {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	return false, &usageError{msg: fmt.Sprintf("aeon: %q is not a bool — true or false", v)}
}

func runMergeRuleClear(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("merge-rule clear", flag.ContinueOnError)
	fs.SetOutput(w)
	component := fs.String("component", "", "the component (required)")
	yes := fs.Bool("yes", false, "confirm: clearing returns the component to its root's (or the baseline's) rule")
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *component == "" {
		return &usageError{msg: "aeon merge-rule clear: --component is required"}
	}
	if !*yes {
		return &usageError{msg: "aeon merge-rule clear: --yes is required — clearing the rule returns the component to whoever governs above it"}
	}
	door, err := gateSelect()
	if err != nil {
		return err
	}
	defer door.Close()
	res, err := door.MergeRuleClear(context.Background(), *component)
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(w, res)
	}
	renderRuleWire(w, res.Rule.ComponentID, res.Rule.MergeRuleWire, res.Rule.Revision)
	return nil
}

func runMergeRulePreview(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("merge-rule preview", flag.ContinueOnError)
	fs.SetOutput(w)
	component := fs.String("component", "", "the component (required)")
	pr := fs.Int("pr", 0, "the forge PR number (facts come from the live forge when named)")
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *component == "" {
		return &usageError{msg: "aeon merge-rule preview: --component is required"}
	}
	door, err := gateSelect()
	if err != nil {
		return err
	}
	defer door.Close()
	res, err := door.MergeRulePreview(context.Background(), *component, *pr)
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(w, res)
	}
	fmt.Fprintf(w, "preview for %s   effective source %s\n", res.ComponentID, res.Source)
	if res.CannotSay != "" {
		fmt.Fprintf(w, "cannot say: %s\n", res.CannotSay)
	}
	if res.Verdict != nil {
		if res.Verdict.Allowed {
			fmt.Fprintln(w, "verdict: ALLOWED")
		} else {
			fmt.Fprintln(w, "verdict: NOT allowed:")
			for _, r := range res.Verdict.Refusals {
				fmt.Fprintf(w, "  %s — %s\n", r.Code, r.Sentence)
			}
		}
	}
	if res.Floor != nil {
		renderFloor(w, res.Floor)
	}
	return nil
}

func runMergeRuleChanges(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("merge-rule changes", flag.ContinueOnError)
	fs.SetOutput(w)
	component := fs.String("component", "", "filter to one component")
	limit := fs.Int("limit", 0, "most recent N")
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	if err := fs.Parse(args); err != nil {
		return err
	}
	door, err := gateSelect()
	if err != nil {
		return err
	}
	defer door.Close()
	res, err := door.MergeRuleChanges(context.Background(), *component, *limit)
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(w, res)
	}
	if len(res.Changes) == 0 {
		fmt.Fprintln(w, "(no changes)")
		return nil
	}
	for _, c := range res.Changes {
		fmt.Fprintf(w, "  %s  r%d  %s  %s/%s  %s  %s\n", c.ComponentID, c.Revision, c.Op, c.ActorKind, c.ActorID, c.At, c.Summary)
	}
	return nil
}

func runMergeRuleVocabulary(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("merge-rule vocabulary", flag.ContinueOnError)
	fs.SetOutput(w)
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	if err := fs.Parse(args); err != nil {
		return err
	}
	door, err := gateSelect()
	if err != nil {
		return err
	}
	defer door.Close()
	res, err := door.MergeRuleVocabulary(context.Background())
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(w, res)
	}
	fmt.Fprintf(w, "require-dod     %s\n", strings.Join(res.RequireDoD, " | "))
	fmt.Fprintf(w, "checks-policy   %s\n", strings.Join(res.ChecksPolicy, " | "))
	fmt.Fprintf(w, "no-checks       %s\n", strings.Join(res.NoChecks, " | "))
	fmt.Fprintf(w, "review-mode     %s\n", strings.Join(res.ReviewMode, " | "))
	fmt.Fprintf(w, "method          %s\n", strings.Join(res.Method, " | "))
	fmt.Fprintf(w, "refusal codes   %s\n", strings.Join(res.RefusalCodes, ", "))
	fmt.Fprintf(w, "floor codes     %s\n", strings.Join(res.FloorCodes, ", "))
	return nil
}
