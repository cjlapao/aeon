package action

// The registry — the serialization point (T10). Nine actions plus
// `audit`, each with its effect class (T2), its idempotency strategy
// (T5/T7), an args schema, and the evidence it returns. `render` is
// ABSENT (L25/D13): v1's loop.md/events.jsonl/memory.md writer died at
// S4's ruling — "the mirror is disposable; the store is the source of
// truth" — and S9's app plus `aeon loop show` cover everyone who would
// have read the files. S4's stale deferral to "a separate module (S11)"
// is already corrected (I8); re-editing that record is forbidden.
//
// THE REGISTRY IS CLOSED, and that is asserted, not assumed
// (D14/O2/E2): this map is package-private, no exported function adds to
// it, and TestRegistryIsClosedAsserted proves no blueprint field,
// handler field or config value can introduce an action. It matters more
// here than it did for S8's tools because T2's effect classes are
// sufficient ONLY because the set they govern is known — an open
// registry would make them decoration.

import (
	"context"
	"fmt"
	"sort"

	"github.com/cjlapao/aeon/internal/forge"
)

// ArgSpec declares one argument: its type, whether the action refuses to
// run without it, and — the L3 line again — WHO SUPPLIES it. "code"
// means a handler or the loop fills it; "handoff" means a model wrote
// it; "evidence" means a prior action returned it. No identifier is ever
// sourced from a handoff, and the schema says so where a person can read
// it.
type ArgSpec struct {
	Type     string // "string" | "int" | "int64"
	Required bool
	Source   string // "code" | "manifest" | "handoff" | "evidence"
}

// Entry is one registered action.
type Entry struct {
	Name     string
	Args     map[string]ArgSpec
	Evidence []string // the keys Dispatch guarantees back, non-empty
	// Door is S61 GI8's second gate: "" (or "loop") is the loop-executable
	// default — the action a blueprint may name and Dispatch may run;
	// DoorPerson marks an action INVISIBLE to blueprints (Names() omits
	// it) and executable only through DispatchFrom by a person Caller.
	// One string field, two closed values; the registry is still the only
	// definition.
	Door string
}

// DoorPerson is the one non-default door value (GI8).
const DoorPerson = "person"

// Caller says WHO stands behind a DispatchFrom call. It is deliberately
// not a loop's instrument: the loop's door is Dispatch, which runs the
// loop-executable set and refuses the person doors; Caller exists so the
// person's door (the daemon's intake.commit, built from the socket peer
// and nothing else) can be asserted at the same table. Kind "" and any
// unlisted kind are non-person: the refusal is the default.
type Caller struct {
	Kind string // "person" | anything else is not a person
	ID   string // who the peer was stamped as (audit, not routing)
}

// CallerPerson is the only Kind the person doors admit.
const CallerPerson = "person"

// personDoorSentence is THE fixed refusal (GI8), one string so the daemon,
// the loop and a canary test all quote the same words.
func personDoorSentence(name string) string {
	return name + " is a person-door action — only a person's intake.commit may run it, under the peer-stamped identity; a loop, an agent or a blueprint is refused (S61 GI8)"
}

// registry is THE set. Package-private; no exported mutation (D14).
var registry = map[string]Entry{
	GitCreateBranch: {Name: GitCreateBranch, Args: map[string]ArgSpec{
		// S60 MQ15: the loop’s pinned base — code’s fact, the cut-from source
		// for a fresh branch. Optional: absent is today’s cut-from-HEAD.
		"base_branch": {Type: "string", Required: false, Source: "code"},
		"dir":         {Type: "string", Required: true, Source: "code"},
		"loop":        {Type: "string", Required: true, Source: "code"},
		"task":        {Type: "string", Required: false, Source: "code"}, // task branch needs it; feature does not
		"which":       {Type: "string", Required: false, Source: "code"},
	}, Evidence: []string{"branch"}},
	GitCommitAndPush: {Name: GitCommitAndPush, Args: map[string]ArgSpec{
		"dir":     {Type: "string", Required: true, Source: "code"},
		"loop":    {Type: "string", Required: true, Source: "code"},
		"task":    {Type: "string", Required: true, Source: "code"},
		"message": {Type: "string", Required: true, Source: "code"}, // code composes it; the model may PROSE inside, never the fact
	}, Evidence: []string{"commit_sha"}},
	GitMergeBranch: {Name: GitMergeBranch, Args: map[string]ArgSpec{
		"dir":  {Type: "string", Required: true, Source: "code"},
		"loop": {Type: "string", Required: true, Source: "code"},
		"task": {Type: "string", Required: true, Source: "code"},
	}, Evidence: []string{"branch"}},
	// S60 MQ8: the queue's three. Every identifier is code-supplied (the
	// landing loop's pinned settings and the queue item's row) — a model can
	// never name a branch, an item, or a root to this door (MQ8's schema
	// rule, MQ11's SHA binding upstream of it).
	GitIntegrate: {Name: GitIntegrate, Args: map[string]ArgSpec{
		"root":               {Type: "string", Required: true, Source: "code"}, // the component root the worktree hangs off
		"item":               {Type: "string", Required: true, Source: "code"}, // the queue item id (mq-<hex>)
		"integration_branch": {Type: "string", Required: true, Source: "code"}, // re-derived and COMPARED at dispatch, never trusted alone
		"head_branch":        {Type: "string", Required: true, Source: "code"}, // the item's branch, merged forward
		"base_branch":        {Type: "string", Required: true, Source: "code"}, // where the new ref cuts (MQ6.1)
	}, Evidence: []string{"integration_branch", "integration_sha", "integration_dir"}},
	GitConcludeIntegration: {Name: GitConcludeIntegration, Args: map[string]ArgSpec{
		// dir is the engine's WorkDir — after git.integrate it IS the
		// integration worktree (the evidence→WorkDir seam, not a model
		// pointer). integration_branch rides the landing loop's pin.
		"dir":                {Type: "string", Required: true, Source: "code"},
		"integration_branch": {Type: "string", Required: true, Source: "code"},
	}, Evidence: []string{"integration_sha"}},
	GitPublishIntegration: {Name: GitPublishIntegration, Args: map[string]ArgSpec{
		"dir":                {Type: "string", Required: true, Source: "code"},
		"integration_branch": {Type: "string", Required: true, Source: "code"},
	}, Evidence: []string{"pushed_sha"}},
	// S60 T6 (MQ8/MQ13/MQ14.1): the loop's door INTO the queue. The only
	// inputs are the loop's own pinned facts; the schema states it —
	// there is no handoff field here at all, because there is nothing a
	// model could be allowed to choose.
	QueueEnqueue: {Name: QueueEnqueue, Args: map[string]ArgSpec{
		"root":  {Type: "string", Required: true, Source: "code"}, // the component root the queue is keyed on
		"loop":  {Type: "string", Required: true, Source: "code"}, // the queue's actor: who asked, from the pin
		// Both optional HERE on purpose: an empty pin is not a missing
		// argument, it is a refusal with a sentence — no branch means
		// "there is nothing to land", no base means MQ15's known-base
		// refusal naming the fix. The generic required-arg sentence would
		// hide both.
		"feature_branch": {Type: "string", Required: false, Source: "code"}, // the loop's PINNED feature branch
		"base_branch":    {Type: "string", Required: false, Source: "code"}, // the loop's pinned base; empty ⇒ MQ15 refusal
		"attended":       {Type: "int", Required: false, Source: "code"},    // the loop's pinned attendedness (NULL ⇒ 0), stamped by the engine
	}, Evidence: []string{"queue_item_id", "queue_status"}},
	ForgeOpenIssue: {Name: ForgeOpenIssue, Args: map[string]ArgSpec{
		"dir":        {Type: "string", Required: true, Source: "code"},
		"loop":       {Type: "string", Required: true, Source: "code"},
		"task":       {Type: "string", Required: true, Source: "code"},
		"task_title": {Type: "string", Required: true, Source: "code"}, // the blueprint's task row, not the model
	}, Evidence: []string{"issue_number"}},
	ForgeOpenPR: {Name: ForgeOpenPR, Args: map[string]ArgSpec{
		"dir":        {Type: "string", Required: true, Source: "code"},
		"loop":       {Type: "string", Required: true, Source: "code"},
		"task":       {Type: "string", Required: true, Source: "code"},
		"task_title": {Type: "string", Required: true, Source: "code"},
		"base":       {Type: "string", Required: true, Source: "code"}, // L26: required, never remembered
		"pr_summary": {Type: "string", Required: true, Source: "handoff"},
	}, Evidence: []string{"pr_number"}},
	ForgeOpenLandingPR: {Name: ForgeOpenLandingPR, Args: map[string]ArgSpec{
		// S60 T6 (MQ8): the landing PR of one queue item. Head is the
		// item's integration ref (re-derived and compared at dispatch,
		// exactly as integrate does — the queue opens PRs only for refs it
		// named), base the item's base branch (L26: required, never
		// remembered), and the summary is prose BY DECLARATION.
		"dir":                {Type: "string", Required: true, Source: "code"},
		"item":               {Type: "string", Required: true, Source: "code"},
		"integration_branch": {Type: "string", Required: true, Source: "code"},
		"head_branch":        {Type: "string", Required: true, Source: "code"}, // the carried branch, for the body
		"base_branch":        {Type: "string", Required: true, Source: "code"},
		"pr_summary":         {Type: "string", Required: false, Source: "handoff"}, // marked prose, never a fact slot
	}, Evidence: []string{"pr_number"}},
	ForgePostReview: {Name: ForgePostReview, Args: map[string]ArgSpec{
		"pr":             {Type: "int", Required: true, Source: "evidence"},
		"step_id":        {Type: "int64", Required: true, Source: "code"},
		"attempt":        {Type: "int", Required: true, Source: "code"},
		"loop":           {Type: "string", Required: true, Source: "code"},
		"review_summary": {Type: "string", Required: true, Source: "handoff"},
	}, Evidence: []string{"review_id"}},
	ForgeMerge: {Name: ForgeMerge, Args: map[string]ArgSpec{
		"dir":    {Type: "string", Required: true, Source: "code"},
		"loop":   {Type: "string", Required: true, Source: "code"},
		"task":   {Type: "string", Required: true, Source: "code"},
		"pr":     {Type: "int", Required: true, Source: "evidence"}, // never typed by a human or a model (Q7)
		"method": {Type: "string", Required: false, Source: "code"},
		// The engine's review token — "ok@<head sha>", written by the
		// loop when the review step completes ok, and read from EVIDENCE
		// only (VG9): a handoff field of the same name is a different
		// value that never reaches the gate.
		"aeon_review": {Type: "string", Required: false, Source: "evidence"},
	}, Evidence: []string{"merge_sha"}},
	ForgeLand: {Name: ForgeLand, Args: map[string]ArgSpec{
		// S60 T6 (MQ12): the queue's landing. Every identity is code or
		// evidence — pr from openLandingPR's evidence row (Q7, never
		// typed), verified_sha from the verification's evidence row
		// (MQ11: the binding the irreversible act must honour), the
		// branches from the landing loop's pin. No handoff field exists:
		// a model cannot name, move, or re-time what gets landed.
		"dir":                {Type: "string", Required: true, Source: "code"}, // where fetch + IsAncestor are asked
		"root":               {Type: "string", Required: true, Source: "code"}, // component root (grants + rule scope)
		"item":               {Type: "string", Required: true, Source: "code"},
		"integration_branch": {Type: "string", Required: true, Source: "code"}, // re-derived and COMPARED at dispatch
		"base_branch":        {Type: "string", Required: true, Source: "code"}, // the item's base — D5's assertion, right base
		"pr":                 {Type: "int", Required: true, Source: "evidence"},
		"verified_sha":       {Type: "string", Required: true, Source: "evidence"}, // MQ11's binding, never remembered
		"integration_sha":    {Type: "string", Required: true, Source: "evidence"},
		"loop":               {Type: "string", Required: true, Source: "code"},
		"method":             {Type: "string", Required: false, Source: "code"}, // the repo's land_method; empty ⇒ squash (OD4)
		"aeon_review":        {Type: "string", Required: false, Source: "evidence"},
	}, Evidence: []string{"merge_sha"}},
	ForgeCloseIssue: {Name: ForgeCloseIssue, Args: map[string]ArgSpec{
		"issue_number": {Type: "int", Required: true, Source: "evidence"},
	}, Evidence: []string{"issue_number"}},
	VerifyRun: {Name: VerifyRun, Args: map[string]ArgSpec{
		"dir":  {Type: "string", Required: true, Source: "code"},
		"loop": {Type: "string", Required: true, Source: "code"},
		// Optional code-supplied narrowing: the task a run verifies, one
		// gate of the effective set (by id then name), and the gate's
		// agent identity (the policy verdicts are checked under it).
		"task":  {Type: "string", Required: false, Source: "code"},
		"gate":  {Type: "string", Required: false, Source: "code"},
		"agent": {Type: "string", Required: false, Source: "code"},
	}, Evidence: []string{"verify_state", "verify_run_id", "verify_summary", "verify_head_sha", "verify_digest"}},
	Audit: {Name: Audit, Args: map[string]ArgSpec{}, Evidence: []string{"entries"}},
	// S61 T4 (GI8/GI9/GI10): the two bootstrap actions. Both are
	// PERSON-DOOR: absent from Names(), invisible to the blueprint
	// validator, refused by Dispatch — only DispatchFrom with a person
	// Caller reaches them, and the daemon builds that caller from the
	// socket peer inside intake.commit and nowhere else. Every argument is
	// code-supplied; a model names none of it.
	RepoScaffold: {Name: RepoScaffold, Door: DoorPerson, Args: map[string]ArgSpec{
		"dir":      {Type: "string", Required: true, Source: "code"},  // absolute target, GI9-checked
		"template": {Type: "string", Required: true, Source: "code"},  // slug@version, the plan's pin
		"intake":   {Type: "string", Required: true, Source: "code"},  // the intake id the commit names
		"vars":     {Type: "string", Required: false, Source: "code"}, // JSON of the CLOSED variable set
	}, Evidence: []string{"commit_sha", "branch", "files"}},
	ForgeCreateRepo: {Name: ForgeCreateRepo, Door: DoorPerson, Args: map[string]ArgSpec{
		"dir":             {Type: "string", Required: true, Source: "code"},  // the scaffolded repo
		"owner":           {Type: "string", Required: true, Source: "code"},
		"name":            {Type: "string", Required: true, Source: "code"},
		"owner_kind":      {Type: "string", Required: true, Source: "code"}, // "user" | "org" — the endpoint named, never guessed (GI10)
		"publish":         {Type: "string", Required: true, Source: "code"}, // "private" | "public" — the person's answer, required
		"description":     {Type: "string", Required: false, Source: "code"}, // SHORT description only; charter prose has no door (GI18)
		"confirm_public":  {Type: "string", Required: false, Source: "code"}, // "true" gates publish=public
		"adopt_existing":  {Type: "string", Required: false, Source: "code"}, // "true" alone adopts an empty repo
	}, Evidence: []string{"repo_url", "default_branch", "pushed_sha"}},
}

// Names returns the closed set, sorted — the list the validator checks
// `action:` names against (production passes it to
// blueprint.ValidationOptions.Actions, exactly how the closed RESOLVER
// registry is passed; nil at rest keeps validation honestly unverifiable).
func Names() []string {
	out := make([]string, 0, len(registry))
	for n, e := range registry {
		// S61 GI8: the person doors are NOT in the list the blueprint
		// validator receives — a blueprint naming one reads unknown-action,
		// the same words as a name nobody ever registered.
		if e.Door == DoorPerson {
			continue
		}
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Description is one loop-executable action as a form needs to show it.
type Description struct {
	Name string
	Args map[string]ArgSpec
}

// Describe lists the actions a blueprint may name with their arguments,
// sorted by name. The returned values are copies; the registry stays closed.
func Describe() []Description {
	var out []Description
	for _, n := range Names() {
		e := registry[n]
		args := make(map[string]ArgSpec, len(e.Args))
		for k, v := range e.Args {
			args[k] = v
		}
		out = append(out, Description{Name: n, Args: args})
	}
	return out
}

// PersonNames lists the person-door actions, sorted — the daemon's own
// enumeration (the validator never sees it). Names() plus PersonNames()
// is the registry exactly; the split is the door.
func PersonNames() []string {
	out := make([]string, 0, 2)
	for n, e := range registry {
		if e.Door == DoorPerson {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

// Lookup answers membership; absence is (false), never a zero Entry.
func Lookup(name string) (Entry, bool) {
	e, ok := registry[name]
	return e, ok
}

// Args is the union of what dispatchers may supply; the schema above
// says which entries each action reads and demands.
type Args struct {
	Dir        string
	Loop       string
	Task       string
	TaskTitle  string
	Message    string
	PRSummary  string
	ReviewSumm string
	Base       string
	// S60 MQ15: what loop.start PINNED beside the loop row. Prefix feeds
	// name construction (an empty prefix is byte-identical to the no-prefix
	// door); BaseBranch is the create-cut source; FeatureBranch is the
	// pinned integration name itself — code supplies all three, never a model.
	Prefix        string
	FeatureBranch string
	BaseBranch    string
	// S60 T5 (MQ8): the queue's identifiers, all code-supplied — Root is the
	// COMPONENT root (Args.Dir moves to the worktree once WorkDir is set; the
	// worktree hangs off the root regardless), Item is the queue row's id,
	// IntegrationBranch the <prefix>integrate/<item> ref, HeadBranch the
	// item's branch being merged forward. The dispatch re-derives the
	// integration name from (Prefix, Item) and refuses a mismatch, so a
	// caller cannot name a branch this queue did not build.
	Root              string
	Item              string
	IntegrationBranch string
	HeadBranch        string
	// S61 T4 (GI8–GI12): the person doors' arguments, all code-supplied —
	// the daemon's intake.commit composes every one of them from the plan
	// and the person's own publish answer. A model names none of them.
	Template      string // slug@version pin
	Vars          string // JSON of the CLOSED scaffold variable set
	IntakeID      string // named in the initial commit's message
	Owner         string
	RepoName      string
	OwnerKind     string // "user" | "org" — the endpoint the request names
	Publish       string // "private" | "public"
	Description   string // the SHORT description only (GI18)
	ConfirmPublic bool
	AdoptExisting bool
	// S60 T6 (MQ11/MQ13): the verified/integration SHA binding, both fed
	// from EVIDENCE rows (the verification's head, the integration's tip)
	// — a model handoff of the same name is a different value and never
	// reaches this field, exactly as aeon_review works (VG9). Position is
	// the queue's queue position for the ask/body (code, 0 = unknown).
	// Attended is the loop's PINNED attendedness for queue.enqueue
	// (NULL ⇒ false); ActorKind/ActorID are the pinned blueprint's
	// identity for the queue's audit row (MQ14.1).
	VerifiedSHA    string
	IntegrationSHA string
	Position       int
	Attended       bool
	ActorKind      string
	ActorID        string
	IssueNumber       int
	PRNumber          int
	StepID            int64
	Attempt           int
	Verdict           forge.Verdict
	Method            forge.MergeMethod
	Which             BranchWhich
	// verify.run's optional code-supplied narrowing (registry.go's
	// schema says which read them).
	Gate  string
	Agent string
	// AeonReview is the engine's review token for forge.merge — the
	// "ok@<sha>" the loop writes into EVIDENCE when the review step
	// completes ok (VG9). Only the evidence map feeds this field; a
	// handoff of the same name is a different value the gate never sees.
	AeonReview string
}

// Services are the injected halves Dispatch cannot construct itself: the
// git client (real), the forge (fake in tests, GitHub at T11), the one
// intent table's store side, the verify gate service (S57 T6), and the
// audit read.
type Services struct {
	Git    *GitActions
	Forge  *ForgeActions
	Verify VerifyActions
	// Queue is S60 T6's enqueue door — the seam, not the service: this
	// package imports neither the store nor the merge-queue core, so the
	// daemon wires an adapter over both (I2's shape, kept).
	Queue *QueueActions
	// Scaffold is S61 T4's seam for the person door repo.scaffold: the
	// template source (a store read the daemon wires) and the component-
	// root facts CheckTarget guards against (GI9). Both nil ⇒ the action
	// refuses; an action without its instruments is an action that hopes.
	Scaffold *ScaffoldActions
	// CreateRepo is S61 T4's gate seams for forge.createRepo: the S44
	// setting (intake.allowRemoteCreate) and the S56 autonomy grant
	// (R3). Both nil ⇒ refuse; the keys are asked BEFORE any request.
	CreateRepo *CreateRepoActions
	Audit      func(ctx context.Context) (int, error)
}

// Dispatch is the ONLY execution door for a registered action. Unknown
// names are refused, not defaulted — the closed set again. Evidence
// lands in the returned map: numbers, SHAs, refs — never prose
// (L3), and never empty (TestEveryActionReturnsEvidence).
func Dispatch(ctx context.Context, svc *Services, name string, a Args) (map[string]string, error) {
	entry, ok := Lookup(name)
	if !ok {
		return nil, fmt.Errorf("action %q is not registered — the registry is closed (D14), and unknown actions are refused, never defaulted", name)
	}
	// S61 GI8: Dispatch is the LOOP's door. A person-door action is
	// refused here in its own fixed sentence even before its arguments are
	// examined — the door is the answer, and the answer does not depend on
	// what was asked with. The person's door is DispatchFrom.
	if entry.Door == DoorPerson {
		return nil, fmt.Errorf("%s", personDoorSentence(name))
	}
	// Required args first: a missing demanded argument is a refusal
	// naming it (D12's promise applied to the registry's own schema).
	if err := checkRequiredArgs(entry, a); err != nil {
		return nil, err
	}
	if svc == nil {
		return nil, fmt.Errorf("%s refuses: no services wired — an action without its instruments is an action that hopes", name)
	}
	// Branch identities are built LAZILY where the action needs them:
	// closeIssue and audit carry no branch identity at all, and demanding
	// a loop slug from them would invent a requirement the actions do not
	// have. Where they are needed, the validator still validates.
	makeNames := func() (BranchNames, error) {
		task := a.Task
		if task == "" {
			task = "feature" // loop-level arms: legal slug; the task half is never consulted on the feature path
		}
		return NewBranchNamesWith(a.Prefix, a.Loop, task)
	}
	switch name {
	case GitCreateBranch:
		names, err := makeNames()
		if err != nil {
			return nil, err
		}
		if svc.Git == nil {
			return nil, fmt.Errorf("%s: no git client wired", name)
		}
		which := a.Which
		if which == "" {
			// Feature branches are loop-level (no task needed); a task in
			// hand means the task branch. Code decides, never a guess.
			which = BranchTask
			if a.Task == "" {
				which = BranchFeature
			}
		}
		res, err := svc.Git.CreateBranchFrom(ctx, a.Dir, names, which, a.BaseBranch)
		if err != nil {
			return nil, err
		}
		return mustEvidence(name, map[string]string{"branch": res.Branch}), nil
	case GitCommitAndPush:
		names, err := makeNames()
		if err != nil {
			return nil, err
		}
		if svc.Git == nil {
			return nil, fmt.Errorf("%s: no git client wired", name)
		}
		res, err := svc.Git.CommitAndPush(ctx, a.Dir, names, a.Message)
		if err != nil {
			return nil, err
		}
		return mustEvidence(name, map[string]string{"commit_sha": res.SHA}), nil
	case GitMergeBranch:
		names, err := makeNames()
		if err != nil {
			return nil, err
		}
		if svc.Git == nil {
			return nil, fmt.Errorf("%s: no git client wired", name)
		}
		res, err := svc.Git.MergeBranch(ctx, a.Dir, names)
		if err != nil {
			return nil, err
		}
		return mustEvidence(name, map[string]string{"branch": res.Branch}), nil
	case ForgeOpenIssue:
		names, err := makeNames()
		if err != nil {
			return nil, err
		}
		if svc.Forge == nil {
			return nil, fmt.Errorf("%s: no forge wired", name)
		}
		res, err := svc.Forge.OpenIssue(ctx, ForgeArgs{Dir: a.Dir, Names: names, TaskTitle: a.TaskTitle})
		if err != nil {
			return nil, err
		}
		return mustEvidence(name, map[string]string{"issue_number": fmt.Sprintf("%d", res.Number)}), nil
	case ForgeOpenPR:
		names, err := makeNames()
		if err != nil {
			return nil, err
		}
		if svc.Forge == nil {
			return nil, fmt.Errorf("%s: no forge wired", name)
		}
		res, err := svc.Forge.OpenPR(ctx, ForgeArgs{Dir: a.Dir, Names: names, TaskTitle: a.TaskTitle, Base: a.Base, PRBody: a.PRSummary})
		if err != nil {
			return nil, err
		}
		return mustEvidence(name, map[string]string{"pr_number": fmt.Sprintf("%d", res.Number)}), nil
	case ForgePostReview:
		names, err := makeNames()
		if err != nil {
			return nil, err
		}
		if svc.Forge == nil {
			return nil, fmt.Errorf("%s: no forge wired", name)
		}
		res, err := svc.Forge.PostReview(ctx, ReviewArgs{Names: names, Loop: a.Loop, PR: a.PRNumber, StepID: a.StepID, Attempt: a.Attempt, Body: a.ReviewSumm, Verdict: a.Verdict})
		if err != nil {
			return nil, err
		}
		return mustEvidence(name, map[string]string{"review_id": fmt.Sprintf("%d", res.ReviewID)}), nil
	case ForgeMerge:
		names, err := makeNames()
		if err != nil {
			return nil, err
		}
		if svc.Forge == nil {
			return nil, fmt.Errorf("%s: no forge wired", name)
		}
		res, err := svc.Forge.MergePR(ctx, MergeArgs{Dir: a.Dir, Names: names, PRNumber: a.PRNumber, Method: a.Method, Loop: a.Loop, AeonReview: a.AeonReview})
		if err != nil {
			return nil, err
		}
		ev := map[string]string{"merge_sha": res.SHA}
		// T8/O6: the gate's audit trail rides with the result — which
		// review state, which reviewer, when, and the check answer.
		for k, v := range res.Fields {
			ev[k] = v
		}
		return mustEvidence(name, ev), nil
	case ForgeCloseIssue:
		if svc.Forge == nil {
			return nil, fmt.Errorf("%s: no forge wired", name)
		}
		res, err := svc.Forge.CloseIssue(ctx, CloseArgs{IssueNumber: a.IssueNumber})
		if err != nil {
			return nil, err
		}
		return mustEvidence(name, map[string]string{"issue_number": fmt.Sprintf("%d", res.Issue)}), nil
	case VerifyRun:
		if svc.Verify == nil {
			return nil, fmt.Errorf("%s refuses: no verify service wired (S57 T6)", name)
		}
		res, err := svc.Verify.Run(ctx, VerifyRunInput{
			Dir: a.Dir, Loop: a.Loop, Task: a.Task, Gate: a.Gate, Agent: a.Agent,
			Trigger: "loop",
		})
		if err != nil {
			return nil, err
		}
		return mustEvidence(name, map[string]string{
			"verify_state":    res.State,
			"verify_run_id":   res.RunID,
			"verify_summary":  res.Summary,
			"verify_head_sha": res.HeadSHA,
			"verify_digest":   res.Digest,
		}), nil
	case GitIntegrate:
		if svc.Git == nil {
			return nil, fmt.Errorf("%s: no git client wired", name)
		}
		// The re-derivation (MQ8's schema rule made structural): the
		// integration branch must be exactly what THIS loop's pinned prefix
		// and this item id compose — a caller that supplies a pretty name it
		// invented is refused naming the difference.
		expected, err := BranchNames{Prefix: a.Prefix}.IntegrationBranch(a.Item)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		if a.IntegrationBranch != expected {
			return nil, fmt.Errorf("%s refuses: integration_branch %q is not what the pinned prefix and item compose (%q) — the queue integrates only branches it named", name, a.IntegrationBranch, expected)
		}
		if !safeRefName(a.HeadBranch) || !safeRefName(a.BaseBranch) {
			return nil, fmt.Errorf("%s refuses: head_branch %q / base_branch %q carry characters no ref may hold", name, a.HeadBranch, a.BaseBranch)
		}
		res, err := svc.Git.Integrate(ctx, a.Root, a.Item, a.IntegrationBranch, a.HeadBranch, a.BaseBranch)
		if err != nil {
			return nil, err
		}
		ev := map[string]string{"integration_branch": res.Branch, "integration_sha": res.SHA, "integration_dir": res.Dir}
		if res.AlreadyMerged {
			ev["already_merged"] = "true"
		}
		return mustEvidence(name, ev), nil
	case GitConcludeIntegration:
		if svc.Git == nil {
			return nil, fmt.Errorf("%s: no git client wired", name)
		}
		res, err := svc.Git.ConcludeIntegration(ctx, a.Dir, a.IntegrationBranch)
		if err != nil {
			return nil, err
		}
		return mustEvidence(name, map[string]string{"integration_sha": res.SHA}), nil
	case GitPublishIntegration:
		if svc.Git == nil {
			return nil, fmt.Errorf("%s: no git client wired", name)
		}
		res, err := svc.Git.PublishIntegration(ctx, a.Dir, a.Prefix, a.IntegrationBranch)
		if err != nil {
			return nil, err
		}
		return mustEvidence(name, map[string]string{"pushed_sha": res.SHA}), nil
	case QueueEnqueue:
		if svc.Queue == nil {
			return nil, fmt.Errorf("%s refuses: no queue service wired (S60 T6)", name)
		}
		// The identities are the loop's PINNED ones, filled by the engine
		// from the loop row (MQ13: "the loop pin is the only input").
		// Attendedness and the actor came from the same pin, stamped at
		// composition — nothing here reads a handoff.
		res, err := svc.Queue.Enqueue(ctx, QueueEnqueueInput{
			ComponentRoot: a.Root,
			Branch:        a.FeatureBranch,
			BaseBranch:    a.BaseBranch,
			LoopSlug:      a.Loop,
			Attended:      a.Attended,
			ActorKind:     a.ActorKind,
			ActorID:       a.ActorID,
		})
		if err != nil {
			return nil, err
		}
		return mustEvidence(name, map[string]string{
			"queue_item_id": res.ItemID,
			"queue_status":  res.Status,
		}), nil
	case ForgeOpenLandingPR:
		if svc.Forge == nil {
			return nil, fmt.Errorf("%s: no forge wired", name)
		}
		// The same re-derivation the integrate arm makes (MQ8): the queue
		// opens landing PRs only for integration refs IT named.
		expected, err := BranchNames{Prefix: a.Prefix}.IntegrationBranch(a.Item)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		if a.IntegrationBranch != expected {
			return nil, fmt.Errorf("%s refuses: integration_branch %q is not what the pinned prefix and item compose (%q) — the queue opens PRs only for branches it named", name, a.IntegrationBranch, expected)
		}
		if !safeRefName(a.HeadBranch) || !safeRefName(a.BaseBranch) {
			return nil, fmt.Errorf("%s refuses: head_branch %q / base_branch %q carry characters no ref may hold", name, a.HeadBranch, a.BaseBranch)
		}
		res, err := svc.Forge.OpenLandingPR(ctx, LandingArgs{
			Dir: a.Dir, Item: a.Item, IntegrationBranch: a.IntegrationBranch,
			HeadBranch: a.HeadBranch, BaseBranch: a.BaseBranch, Position: a.Position,
			Summary: a.PRSummary,
		})
		if err != nil {
			return nil, err
		}
		return mustEvidence(name, map[string]string{"pr_number": fmt.Sprintf("%d", res.Number)}), nil
	case ForgeLand:
		if svc.Forge == nil {
			return nil, fmt.Errorf("%s: no forge wired", name)
		}
		expected, err := BranchNames{Prefix: a.Prefix}.IntegrationBranch(a.Item)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		if a.IntegrationBranch != expected {
			return nil, fmt.Errorf("%s refuses: integration_branch %q is not what the pinned prefix and item compose (%q) — the queue lands only branches it named", name, a.IntegrationBranch, expected)
		}
		if !safeRefName(a.BaseBranch) {
			return nil, fmt.Errorf("%s refuses: base_branch %q carries characters no ref may hold", name, a.BaseBranch)
		}
		res, err := svc.Forge.Land(ctx, LandArgs{
			Dir: a.Dir, PRNumber: a.PRNumber, Item: a.Item,
			IntegrationBranch: a.IntegrationBranch, BaseBranch: a.BaseBranch,
			VerifiedSHA: a.VerifiedSHA, IntegrationSHA: a.IntegrationSHA,
			ComponentRoot: a.Root, Position: a.Position, Method: a.Method,
			Loop: a.Loop, AeonReview: a.AeonReview,
		})
		if err != nil {
			return nil, err
		}
		ev := map[string]string{"merge_sha": res.SHA}
		for k, v := range res.Fields {
			ev[k] = v
		}
		return mustEvidence(name, ev), nil
	case Audit:
		if svc.Audit == nil {
			return nil, fmt.Errorf("%s: no audit read wired", name)
		}
		n, err := svc.Audit(ctx)
		if err != nil {
			return nil, err
		}
		return mustEvidence(name, map[string]string{"entries": fmt.Sprintf("%d", n)}), nil
	}
	// Unreachable against the closed set — and said out loud rather than
	// returned as nil, because "nil, nil" is how a silent skip is born.
	return nil, fmt.Errorf("%s is registered but has no dispatch arm — a registry bug, not a user error", name)
}

// checkRequiredArgs is D12's promise applied to the registry's own schema,
// factored so the loop's door and the person's door refuse identically.
func checkRequiredArgs(entry Entry, a Args) error {
	for field, spec := range entry.Args {
		if !spec.Required {
			continue
		}
		if argEmpty(a, field) {
			return fmt.Errorf("%s refuses: required argument %q (supplied by %s) is missing", entry.Name, field, spec.Source)
		}
	}
	return nil
}

// DispatchFrom is the PERSON's door (S61 GI8): the one road to a
// person-door action, open only to a Caller of Kind person. The daemon
// builds that caller — and ONLY there — from the peer-stamped actor of a
// person's intake.commit; nothing else in the process ever constructs one.
// A non-person caller meets the fixed sentence naming the door, before any
// argument is examined; a person caller for a LOOP-executable action is
// routed through Dispatch unchanged, because the loop door is the one
// implementation and a person standing at the side door still gets it.
func DispatchFrom(ctx context.Context, svc *Services, c Caller, name string, a Args) (map[string]string, error) {
	entry, ok := Lookup(name)
	if !ok {
		return nil, fmt.Errorf("action %q is not registered — the registry is closed (D14), and unknown actions are refused, never defaulted", name)
	}
	if entry.Door == DoorPerson {
		if c.Kind != CallerPerson {
			return nil, fmt.Errorf("%s", personDoorSentence(name))
		}
		if err := checkRequiredArgs(entry, a); err != nil {
			return nil, err
		}
		if svc == nil {
			return nil, fmt.Errorf("%s refuses: no services wired — an action without its instruments is an action that hopes", name)
		}
		switch name {
		case RepoScaffold:
			return runScaffold(ctx, svc, a)
		case ForgeCreateRepo:
			return runCreateRepo(ctx, svc, a)
		}
		return nil, fmt.Errorf("%s: a person-door action with no implementation — the registry lists what the code cannot do, and that is a bug", name)
	}
	return Dispatch(ctx, svc, name, a)
}

func mustEvidence(name string, ev map[string]string) map[string]string {
	for k, v := range ev {
		if v == "" {
			ev[k] = "(unset)" // never an empty evidence value — the transcript must not read "ran, no facts"
		}
	}
	_ = name
	return ev
}

func argEmpty(a Args, field string) bool {
	switch field {
	case "dir":
		return a.Dir == ""
	case "template":
		return a.Template == ""
	case "intake":
		return a.IntakeID == ""
	case "owner":
		return a.Owner == ""
	case "name":
		return a.RepoName == ""
	case "owner_kind":
		return a.OwnerKind == ""
	case "publish":
		return a.Publish == ""
	case "loop":
		return a.Loop == ""
	case "task":
		return a.Task == ""
	case "task_title":
		return a.TaskTitle == ""
	case "message":
		return a.Message == ""
	case "base":
		return a.Base == ""
	case "pr_summary":
		return a.PRSummary == ""
	case "review_summary":
		return a.ReviewSumm == ""
	case "pr":
		return a.PRNumber <= 0
	case "step_id":
		return a.StepID <= 0
	case "attempt":
		return a.Attempt < 1
	case "issue_number":
		return a.IssueNumber <= 0
	case "root":
		return a.Root == ""
	case "item":
		return a.Item == ""
	case "integration_branch":
		return a.IntegrationBranch == ""
	case "head_branch":
		return a.HeadBranch == ""
	case "base_branch":
		return a.BaseBranch == ""
	case "feature_branch": // S60 T6: the loop's pinned feature branch (queue.enqueue)
		return a.FeatureBranch == ""
	case "verified_sha": // S60 T6 (MQ11): the binding, evidence-fed
		return a.VerifiedSHA == ""
	case "integration_sha": // S60 T6: the integration tip, evidence-fed
		return a.IntegrationSHA == ""
	}
	return false // "which"/"method"/verdict-style args carry their own defaults
}
