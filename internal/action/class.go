// Package action governs the code actions: the closed registry (L8), the
// effect classes that close the governance gap (L22), and the asks that
// carry what is affected (O1).
//
// The gap this package exists to close was invisible until it was written
// down: S11's evaluator guards `bash`, and a blueprint naming `forge.merge`
// reaches the SAME effect through a path with no guard at all — expressing
// an action as a code action rather than a command would launder it past
// everything S11 built. Effect classes are the answer, and they are CODE,
// deliberately (N7): a config-defined class is a way to downgrade
// `irreversible` to `local`, and the whole value of the classification is
// that nobody can. TestClassesAreNotConfigurable greps for the attempt.
//
// The classes suffice because the registry is CLOSED: `bash` needs a
// pattern matcher because its input is unbounded; the action registry needs
// classes because its input is eight things. Closed is not the same as
// safe to run unattended — which is the whole point of the classes (G7,
// G14: unattended resolves outward and irreversible to deny).
package action

import (
	"fmt"
	"strings"
)

// EffectClass is the closed set (L22).
type EffectClass string

const (
	// Effects confined to the working tree.
	EffectLocal EffectClass = "local"

	// Visible to other people: a push, an issue, a PR, a review.
	EffectOutward EffectClass = "outward"

	// Cannot be undone by another action: a merge.
	EffectIrreversible EffectClass = "irreversible"
)

// Valid is the exhaustive decision over the closed set — an unknown value,
// including the zero value, is false. No switch here can silently accept a
// new string; the default IS the refusal.
func Valid(c EffectClass) bool {
	switch c {
	case EffectLocal, EffectOutward, EffectIrreversible:
		return true
	default:
		return false
	}
}

// The sixteen names — nine from the design's original table, three from the
// S60 merge queue's git half (MQ8, T5), three more from its landing half
// (MQ8/MQ12, T6), plus audit — exactly as the designs name them.
// T4–T7 fill the handlers; T10 assembles the closed registry around these
// same constants, so there is one definition of a name.
const (
	GitCreateBranch        = "git.createBranch"
	GitCommitAndPush       = "git.commitAndPush"
	GitMergeBranch         = "git.mergeBranch"
	GitIntegrate           = "git.integrate"           // S60 MQ8: merge-forward onto a new ref, in the worktree
	GitConcludeIntegration = "git.concludeIntegration" // S60 MQ8: commit a resolved merge, assert clean
	GitPublishIntegration  = "git.publishIntegration"  // S60 MQ8: the ONE outward git half — a plain push of a new integrate ref
	QueueEnqueue           = "queue.enqueue"           // S60 MQ8/MQ14.1: a loop asks its own queue to hold its branch (local; OD11: mode=off still enqueues)
	ForgeOpenIssue         = "forge.openIssue"
	ForgeOpenPR            = "forge.openPR"
	ForgeOpenLandingPR     = "forge.openLandingPR" // S60 MQ8/T6: the landing PR — head is an integrate ref
	ForgePostReview        = "forge.postReview"
	ForgeMerge             = "forge.merge"
	ForgeLand              = "forge.land" // S60 MQ12: the queue's irreversible action — merge's guard family, the queue's assertions
	ForgeCloseIssue        = "forge.closeIssue"
	VerifyRun              = "verify.run" // S57 T6: the local gate run, recorded as a run row
	Audit                  = "audit"
	// S61 T4 (GI8): the two bootstrap names. PERSON-DOOR actions — the
	// registry lists them, Names() omits them, and only a person Caller
	// dispatches them (see Entry.Door and DispatchFrom).
	RepoScaffold    = "repo.scaffold"    // GI9: lay a template as a new git repo, locally
	ForgeCreateRepo = "forge.createRepo" // GI10: outward repository creation, staged off by three keys
)

// classes is package-private BY DESIGN: the only definition, no setter, no
// configuration path (N7/I4). The table mirrors the design's nine-plus-audit
// strategy table exactly, plus the S60 queue's three (MQ8: integrate and
// conclude are LOCAL — a worktree is visible to nobody outside the tree;
// publish is the queue's only outward name, and it is a plain push).
var classes = map[string]EffectClass{
	GitCreateBranch:        EffectLocal,
	GitCommitAndPush:       EffectOutward,
	GitMergeBranch:         EffectLocal,
	GitIntegrate:           EffectLocal, // the merge lands in a worktree, nobody else's tree
	GitConcludeIntegration: EffectLocal, // same tree, still nothing sent
	GitPublishIntegration:  EffectOutward,
	QueueEnqueue:           EffectLocal, // a row in our own database; nothing sent anywhere (MQ8)
	ForgeOpenIssue:         EffectOutward,
	ForgeOpenPR:            EffectOutward,
	ForgeOpenLandingPR:     EffectOutward, // a PR people can see — the queue's second outward name
	ForgePostReview:        EffectOutward,
	ForgeMerge:             EffectIrreversible,
	ForgeLand:              EffectIrreversible, // MQ12: the queue's own irreversible action
	ForgeCloseIssue:        EffectOutward,
	VerifyRun:              EffectLocal, // the gates run in the tree; nothing outward, nothing undoable
	Audit:                  EffectLocal,
	// S61 T4 (GI8): repo.scaffold writes files and commits locally — a
	// LOCAL effect (the outward half is the separate publish action);
	// forge.createRepo publishes a repository — OUTWARD, and the person
	// door behind it means unattended never reaches even the class.
	RepoScaffold:    EffectLocal,
	ForgeCreateRepo: EffectOutward,
}

// Classes returns a copy — the CLI and tests read the table; nobody edits
// it.
func Classes() map[string]EffectClass {
	out := make(map[string]EffectClass, len(classes))
	for k, v := range classes {
		out[k] = v
	}
	return out
}

// Verdict is the three-shaped outcome of governance: run (local), ask (a
// human must route it), deny (unattended resolving an outward or
// irreversible class).
type Verdict string

const (
	VerdictRun   Verdict = "run"
	VerdictAsk   Verdict = "ask"
	VerdictDeny  Verdict = "deny"
	VerdictError Verdict = "error" // never returned by this package's pure checks
)

// CheckResult is what `aeon action check` prints and what the loop's gate
// consults. Sentence is already written for a human — the one renderer of
// these outcomes is here, so no surface paraphrases a verdict.
type CheckResult struct {
	Action   string
	Class    EffectClass
	Verdict  Verdict
	Sentence string
}

func unknown(name string) error {
	return fmt.Errorf("unknown action %q — the registry is closed (L8): eight actions plus audit", name)
}

// Check answers what the action WOULD produce in an attended loop: local
// runs, outward and irreversible ask (L22).
func Check(name string) (CheckResult, error) {
	c, ok := classes[name]
	if !ok {
		return CheckResult{}, unknown(name)
	}
	if !Valid(c) {
		return CheckResult{}, fmt.Errorf("action %q carries an invalid class %q", name, c)
	}
	if c == EffectLocal {
		return CheckResult{Action: name, Class: c, Verdict: VerdictRun,
			Sentence: fmt.Sprintf("%s · %s · run — effects confined to the working tree", name, c)}, nil
	}
	return CheckResult{Action: name, Class: c, Verdict: VerdictAsk,
		Sentence: fmt.Sprintf("%s · %s · ask — %s", name, c, classReason(c))}, nil
}

// Resolve applies the loop's attendedness (G7, G14 — it follows from
// G14's pinned attendedness, not a new rule): an unattended loop DENIES
// both outward and irreversible, naming the class in the sentence so the
// operator learns which door said no and why. Local runs either way;
// attended asks exactly as Check does.
//
// allowOutward is S19 T6's ONE opt-in: the loop's own declaration, made at
// start, read from the loop's pinned row (D10, O2) — never a key, never a
// caller attribute, never a configuration file, because a permanent grant
// is a grant whose theft is unbounded and a per-loop flag makes each grant
// a decision someone made at the one moment the question was in front of
// them. It lifts OUTWARD only. irreversible is untouched by it (no field
// here reaches merging — S16's ask exists for that, and merging is a
// separate matter someone should have thought harder about): when an
// unattended loop with the declaration still reaches one, the sentence
// SAYS the flag did not lift it — the operator who set it learns which
// door said no, in the block reason, not by reading this paragraph.
//
// The unattended-outward WITHOUT the declaration keeps T2's sentence
// VERBATIM — the loop does not paraphrase its own refusal, and S16's
// pinned test stays true by not being touched.
func Resolve(name string, attended, allowOutward bool) (CheckResult, error) {
	c, ok := classes[name]
	if !ok {
		return CheckResult{}, unknown(name)
	}
	// S61 GI8: a person door DENIES before any class arithmetic — not ask,
	// not run, regardless of attendedness or allowOutward. The flag lifts
	// outward for the LOOP-executable set; a person-door action is not in
	// that set at all, and "the flag said so" must never reach it.
	if e, registered := Lookup(name); registered && e.Door == DoorPerson {
		return CheckResult{Action: name, Class: c, Verdict: VerdictDeny,
			Sentence: personDoorSentence(name)}, nil
	}
	if c == EffectLocal {
		return CheckResult{Action: name, Class: c, Verdict: VerdictRun,
			Sentence: fmt.Sprintf("%s · %s · run — effects confined to the working tree", name, c)}, nil
	}
	if !attended {
		if c == EffectOutward && allowOutward {
			return CheckResult{Action: name, Class: c, Verdict: VerdictRun,
				Sentence: fmt.Sprintf("%s · %s · run — outward effect runs on the loop's own allowOutward declaration at start (per loop, per moment; never a key)", name, c)}, nil
		}
		if c == EffectIrreversible && allowOutward {
			return CheckResult{Action: name, Class: c, Verdict: VerdictDeny,
				Sentence: fmt.Sprintf("%s denied — effect class '%s' resolves to deny (loop is unattended; allowOutward does not grant it)", name, c)}, nil
		}
		return CheckResult{Action: name, Class: c, Verdict: VerdictDeny,
			Sentence: fmt.Sprintf("%s denied — effect class '%s' resolves to deny (loop is unattended)", name, c)}, nil
	}
	return CheckResult{Action: name, Class: c, Verdict: VerdictAsk,
		Sentence: fmt.Sprintf("%s · %s · ask — %s", name, c, classReason(c))}, nil
}

func classReason(c EffectClass) string {
	switch c {
	case EffectOutward:
		return "visible to other people — a human decides"
	case EffectIrreversible:
		return "cannot be undone by another action — a human decides"
	default:
		return ""
	}
}

// CheckAnswer renders the line `aeon action check` prints — the §3 demo
// pastes these beside `aeon policy why`, so both routes read alike. This is
// the one renderer; the CLI prints it verbatim.
func CheckAnswer(name string) (string, error) {
	r, err := Check(name)
	if err != nil {
		return "", err
	}
	return r.Sentence, nil
}

// Target is what an ask must be ABOUT before a human can answer it (O1).
// The builder requires the identifying fields per action: an ask naming
// only the action tells a human nothing about WHAT is being merged or
// WHERE, and this package refuses to build one.
type Target struct {
	Repository  string
	PRNumber    int
	BaseRef     string
	Head        string
	Title       string
	IssueNumber int
	Branch      string
	// S60 T6 (MQ12.6): the landing ask names the QUEUE's facts too — an
	// ask that reads "merge this PR" without the item, the position or
	// the verified SHA asks a human to approve a sentence, not a merge.
	// Every value here is code's (queue rows, gate evidence), never prose.
	Item        string
	Position    int
	VerifiedSHA string
	Behind      int
	Ahead       int
	GateSummary string
}

// AskRequest is the question payload for an outward/irreversible action.
//
// Q5, answered: this REUSES S11's `kind: "permission"` — the affordance,
// the answer verbs and the dispatch are all identical (approve routes to
// execution, deny routes to refusal, and U14 renders it through the same
// affordance a step question and a gate use). A fourth kind is only
// warranted when something must DISPATCH differently, and nothing does:
// what differs here is the BODY (it must carry the target), which the
// permission row already carries as free text plus payload.
type AskRequest struct {
	Kind    string
	Action  string
	Class   EffectClass
	Prompt  string
	Body    string
	Options []string
	Payload map[string]string
}

var prompts = map[string]string{
	GitCommitAndPush:   "Commit and push?",
	ForgeOpenIssue:     "Open an issue?",
	ForgeOpenPR:        "Open a pull request?",
	ForgeOpenLandingPR: "Open the merge queue's landing PR?", // S60 T6 (MQ8)
	ForgePostReview:    "Post a review?",
	ForgeMerge:         "Merge this pull request?",
	ForgeLand:          "Land this pull request? It merges into its base branch and cannot be undone by another action.", // S60 T6 (MQ12.6)
	ForgeCloseIssue:    "Close the issue?",
}

// BuildAsk assembles the ask. Local actions have no ask — BuildAsk refuses
// them; asking about a working-tree change would train operators to wave
// asks through.
func BuildAsk(name string, tgt Target) (AskRequest, error) {
	c, ok := classes[name]
	if !ok {
		return AskRequest{}, unknown(name)
	}
	if c == EffectLocal {
		return AskRequest{}, fmt.Errorf("%s is %s — local actions do not ask, and an ask nobody needs trains operators to wave asks through", name, c)
	}
	missing := requiredMissing(name, tgt)
	if len(missing) > 0 {
		return AskRequest{}, fmt.Errorf("the %s ask must say what it affects (O1) — missing: %s", name, strings.Join(missing, ", "))
	}

	var lines []string
	payload := map[string]string{"action": name, "effect": string(c)}
	switch name {
	case ForgeMerge:
		lines = append(lines, fmt.Sprintf("%s  #%d  →  %s", name, tgt.PRNumber, tgt.BaseRef))
		payload["pr"] = fmt.Sprintf("%d", tgt.PRNumber)
		payload["base"] = tgt.BaseRef
	case ForgeLand:
		// MQ12.6: the ask is built from ids, enums and numbers — the
		// verified SHA and the queue's position are the two the person
		// is actually vouching for; behind/ahead and the gate summary
		// come along when the queue has them (a re-ask after a
		// re-integrate shows the fresh shape).
		lines = append(lines, fmt.Sprintf("%s  #%d  →  %s", name, tgt.PRNumber, tgt.BaseRef))
		lines = append(lines, fmt.Sprintf("  item: %s  (queue position %d)", tgt.Item, tgt.Position))
		lines = append(lines, fmt.Sprintf("  head: %s", tgt.Head))
		lines = append(lines, fmt.Sprintf("  verified sha: %s", tgt.VerifiedSHA))
		if tgt.Behind > 0 || tgt.Ahead > 0 {
			lines = append(lines, fmt.Sprintf("  behind %d · ahead %d", tgt.Behind, tgt.Ahead))
		}
		if tgt.GateSummary != "" {
			lines = append(lines, fmt.Sprintf("  gate: %s", tgt.GateSummary))
		}
		payload["pr"] = fmt.Sprintf("%d", tgt.PRNumber)
		payload["base"] = tgt.BaseRef
		payload["item"] = tgt.Item
		payload["position"] = fmt.Sprintf("%d", tgt.Position)
		payload["verified_sha"] = tgt.VerifiedSHA
	case ForgeOpenLandingPR:
		lines = append(lines, name,
			fmt.Sprintf("  head: %s", tgt.Head),
			fmt.Sprintf("  base: %s", tgt.BaseRef),
			fmt.Sprintf("  item: %s  (queue position %d)", tgt.Item, tgt.Position))
		payload["head"] = tgt.Head
		payload["base"] = tgt.BaseRef
		payload["item"] = tgt.Item
		payload["position"] = fmt.Sprintf("%d", tgt.Position)
	case ForgeOpenPR:
		lines = append(lines, name,
			fmt.Sprintf("  head: %s", tgt.Head),
			fmt.Sprintf("  base: %s", tgt.BaseRef),
			fmt.Sprintf("  title: %s", tgt.Title))
		payload["head"] = tgt.Head
		payload["base"] = tgt.BaseRef
		payload["title"] = tgt.Title
	case ForgeOpenIssue:
		lines = append(lines, name, fmt.Sprintf("  title: %s", tgt.Title))
		payload["title"] = tgt.Title
	case ForgeCloseIssue:
		lines = append(lines, fmt.Sprintf("%s  #%d", name, tgt.IssueNumber))
		payload["issue"] = fmt.Sprintf("%d", tgt.IssueNumber)
	case ForgePostReview:
		lines = append(lines, fmt.Sprintf("%s  on #%d", name, tgt.PRNumber))
		payload["pr"] = fmt.Sprintf("%d", tgt.PRNumber)
	case GitCommitAndPush:
		lines = append(lines, name, fmt.Sprintf("  branch: %s", tgt.Branch))
		payload["branch"] = tgt.Branch
	}
	lines = append(lines, fmt.Sprintf("repository:  %s", tgt.Repository))
	payload["repository"] = tgt.Repository
	lines = append(lines, "", fmt.Sprintf("effect: %s — %s", c, effectPlain(c)))

	return AskRequest{
		Kind:    "permission",
		Action:  name,
		Class:   c,
		Prompt:  prompts[name],
		Body:    strings.Join(lines, "\n"),
		Options: []string{"approve", "deny"},
		Payload: payload,
	}, nil
}

func effectPlain(c EffectClass) string {
	switch c {
	case EffectOutward:
		return "other people will see this"
	case EffectIrreversible:
		return "this cannot be undone by another action"
	default:
		return ""
	}
}

func requiredMissing(name string, t Target) []string {
	var miss []string
	need := func(cond bool, field string) {
		if !cond {
			miss = append(miss, field)
		}
	}
	if name != GitCommitAndPush {
		need(t.Repository != "", "repository")
	}
	switch name {
	case ForgeMerge:
		need(t.PRNumber > 0, "pr number")
		need(t.BaseRef != "", "base ref")
	case ForgeLand:
		// The verified SHA is not decoration: an ask to land WITHOUT
		// naming what was verified asks a human to approve an unknown
		// (MQ11 is exactly the thing the person is vouching for).
		need(t.PRNumber > 0, "pr number")
		need(t.BaseRef != "", "base ref")
		need(t.Head != "", "head")
		need(t.VerifiedSHA != "", "verified sha")
	case ForgeOpenLandingPR:
		need(t.Head != "", "head")
		need(t.BaseRef != "", "base")
		need(t.Item != "", "item")
	case ForgeOpenPR:
		need(t.Head != "", "head")
		need(t.BaseRef != "", "base")
		need(t.Title != "", "title")
	case ForgeOpenIssue:
		need(t.Title != "", "title")
	case ForgeCloseIssue:
		need(t.IssueNumber > 0, "issue number")
	case ForgePostReview:
		need(t.PRNumber > 0, "pr number")
	case GitCommitAndPush:
		need(t.Branch != "", "branch")
		need(t.Repository != "", "repository")
	}
	return miss
}
