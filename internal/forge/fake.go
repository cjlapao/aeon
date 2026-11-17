package forge

import (
	"context"
	"fmt"
	"sync"
)

// checksSeed is the seeded answer for one commit's checks (S57 T2).
type checksSeed struct {
	checks   []Check
	complete bool
}

// Fake is the forge every automated test asserts against (L26: built
// before any action that uses it). Three obligations beyond returning
// plausible values:
//
//  1. It RECORDS EVERY CALL IN ORDER, WITH ARGUMENTS. The slice's central
//     assertion — "run every action twice, the second is a no-op" — lands
//     on this log, not on a returned value: a test that checks the return
//     passes against code that acted twice and returned the same thing.
//  2. It is CONFIGURABLE PER SCENARIO — existing issue, existing PR,
//     mis-based PR, merged PR, many reviews, unreachable — because every
//     later task's test is a scenario plus an assertion.
//  3. It distinguishes TRANSPORT FAILURE from DOMAIN REFUSAL, separately
//     (O5): the loop's response to each is different.
type Fake struct {
	mu      sync.Mutex
	calls   []Call
	issues  map[int]*Issue
	prs     map[int]*PR
	reviews map[int][]Review
	next    map[string]int
	checks  map[string]*checksSeed
	repos   map[string]*Repo // S61 T4: owner/name → seeded repository (GI10)

	unreachable bool

	// CloseSilentlyFails is §5's test surface: CloseIssue returns nil and
	// the issue stays open — the silently-failed close that only a READ-BACK
	// can catch. Set it to pin the action's read-back obligation.
	CloseSilentlyFails bool

	// failures injects a domain error on one method (a repository that
	// refuses a merge, an expired token, a conflicting head).
	failures map[string]*ForgeError
}

// Call is one log entry: the method, then its arguments in order.
type Call struct {
	Method string
	Args   []any
}

// Compile-time proof the fake satisfies the boundary it exists to stand in for —
// and, since S57 T2, the two OPTIONAL capabilities the GitHub
// implementation carries: the fake answers them over its seeds so a test
// can stand in for the real forge on the checks path too.
var _ Forge = (*Fake)(nil)
var _ StatusLister = (*Fake)(nil)
var _ CheckReader = (*Fake)(nil)
var _ RepoCreator = (*Fake)(nil)
var _ RepoReader = (*Fake)(nil)

func NewFake() *Fake {
	return &Fake{
		issues:  map[int]*Issue{},
		prs:     map[int]*PR{},
		reviews: map[int][]Review{},
		next:    map[string]int{"issue": 1, "pr": 1, "review": 1},
		checks:  map[string]*checksSeed{},
		repos:   map[string]*Repo{},
	}
}

// Calls returns a copy of the log — the assertion surface D1 reads.
func (f *Fake) Calls() []Call {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Call, len(f.calls))
	copy(out, f.calls)
	return out
}

// SetUnreachable simulates the forge being down: EVERY method answers a
// transport failure — which is not a refusal, and the loop retries or says
// so instead of sending someone to check their permissions.
func (f *Fake) SetUnreachable() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unreachable = true
}

// FailMethod injects a domain error on one method until cleared.
func (f *Fake) FailMethod(method string, err *ForgeError) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failures == nil {
		f.failures = map[string]*ForgeError{}
	}
	f.failures[method] = err
}

// recordGate appends the log entry FIRST (the call happened, whatever came
// back — a refused call still belongs on the log), then answers the
// injected conditions: unreachable, then the per-method injection.
func (f *Fake) recordGate(method string, args ...any) error {
	f.mu.Lock()
	f.calls = append(f.calls, Call{Method: method, Args: args})
	f.mu.Unlock()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.unreachable {
		return NewMsg(KindTransport, "the forge is unreachable (scenario)")
	}
	if e, ok := f.failures[method]; ok {
		return e
	}
	return nil
}

// --- seeding scenarios ---------------------------------------------------

// SeedIssue places an issue in the store as the scenario needs it. A zero
// Number is assigned the next number; an empty State means open.
func (f *Fake) SeedIssue(in Issue) Issue {
	f.mu.Lock()
	defer f.mu.Unlock()
	issue := copyIssue(in)
	if issue.Number == 0 {
		issue.Number = f.next["issue"]
	}
	if issue.Number >= f.next["issue"] {
		f.next["issue"] = issue.Number + 1
	}
	if issue.State == "" {
		issue.State = "open"
	}
	f.issues[issue.Number] = issue
	return *issue
}

// SeedPR places a PR. BaseRef is REQUIRED — a scenario that cannot say
// what the PR merges INTO cannot be trusted to speak about merges at all
// (L26), so the fake refuses to hold one without it.
func (f *Fake) SeedPR(in PR) PR {
	f.mu.Lock()
	defer f.mu.Unlock()
	if in.BaseRef == "" {
		panic("forge.Fake: SeedPR requires BaseRef — the phantom-merge guard reads it (L26)")
	}
	pr := copyPR(in)
	if pr.Number == 0 {
		pr.Number = f.next["pr"]
	}
	if pr.Number >= f.next["pr"] {
		f.next["pr"] = pr.Number + 1
	}
	f.prs[pr.Number] = pr
	return *pr
}

// SeedChecks places the checks of a commit as the scenario needs them
// (S57 T2). `complete` false is the page-cap scenario: the report is
// truncated, and the aggregate reads it as unknown, never success.
func (f *Fake) SeedChecks(sha string, checks []Check, complete bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checks[sha] = &checksSeed{checks: append([]Check(nil), checks...), complete: complete}
}

// --- issues ---------------------------------------------------------------

func (f *Fake) FindOpenIssue(ctx context.Context, title string, label string) (*Issue, error) {
	if err := f.recordGate("FindOpenIssue", title, label); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, in := range f.issues {
		if in.State == "open" && in.Title == title && hasLabel(in.Labels, label) {
			return copyIssue(*in), nil
		}
	}
	return nil, nil
}

func (f *Fake) OpenIssue(ctx context.Context, req IssueRequest) (*Issue, error) {
	if err := f.recordGate("OpenIssue", req); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	issue := &Issue{Number: f.next["issue"], Title: req.Title, Body: req.Body, Labels: append([]string(nil), req.Labels...), State: "open"}
	f.next["issue"]++
	f.issues[issue.Number] = issue
	return copyIssue(*issue), nil
}

func (f *Fake) ReadIssue(ctx context.Context, number int) (*Issue, error) {
	if err := f.recordGate("ReadIssue", number); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	in, ok := f.issues[number]
	if !ok {
		return nil, NewMsg(KindNotFound, fmt.Sprintf("issue %d does not exist", number))
	}
	return copyIssue(*in), nil
}

func (f *Fake) CloseIssue(ctx context.Context, number int) error {
	if err := f.recordGate("CloseIssue", number); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	in, ok := f.issues[number]
	if !ok {
		return NewMsg(KindNotFound, fmt.Sprintf("issue %d does not exist", number))
	}
	if f.CloseSilentlyFails {
		// The §5 scenario: success at the call site, no state change —
		// exactly what the action's read-back exists to catch.
		return nil
	}
	c := copyIssue(*in)
	c.State = "closed"
	f.issues[number] = c
	return nil
}

// --- pull requests ---------------------------------------------------------

func (f *Fake) FindOpenPR(ctx context.Context, head string) (*PR, error) {
	if err := f.recordGate("FindOpenPR", head); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.prs {
		if !p.Merged && p.Head == head {
			return copyPR(*p), nil
		}
	}
	return nil, nil
}

func (f *Fake) OpenPR(ctx context.Context, req PRRequest) (*PR, error) {
	if err := f.recordGate("OpenPR", req); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.prs {
		if !p.Merged && p.Head == req.Head {
			return nil, NewMsg(KindConflict, "a PR is already open for head " + req.Head)
		}
	}
	pr := &PR{Number: f.next["pr"], Head: req.Head, BaseRef: req.Base}
	f.next["pr"]++
	f.prs[pr.Number] = pr
	return copyPR(*pr), nil
}

func (f *Fake) ReadPR(ctx context.Context, number int) (*PR, error) {
	if err := f.recordGate("ReadPR", number); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.prs[number]
	if !ok {
		return nil, NewMsg(KindNotFound, fmt.Sprintf("PR %d does not exist", number))
	}
	return copyPR(*p), nil
}

func (f *Fake) MergePR(ctx context.Context, number int, method MergeMethod) (*MergeResult, error) {
	if err := f.recordGate("MergePR", number, string(method)); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.prs[number]
	if !ok {
		return nil, NewMsg(KindNotFound, fmt.Sprintf("PR %d does not exist", number))
	}
	if p.Merged {
		// Never merge twice, never fail: the recorded SHA comes back.
		return &MergeResult{SHA: p.MergeSHA, Merged: true}, nil
	}
	c := copyPR(*p)
	c.Merged = true
	// The merge SHA is its own commit — for squash it is NOT any commit
	// that ever existed on the task branch, which is precisely why the
	// post-merge ancestor assertion must take THIS sha.
	c.MergeSHA = fmt.Sprintf("%040x", 1_000_003*uint64(number)+uint64(len(c.Head)))
	f.prs[number] = c
	return &MergeResult{SHA: c.MergeSHA, Merged: true}, nil
}

// --- checks (S57 T2) --------------------------------------------------------
//
// The fake answers the OPTIONAL capabilities over its seeds, shaped
// exactly like the GitHub implementation's behaviour: an unknown SHA is
// the read's honest absence (KindNotFound — never a transport, never a
// refusal), a PR with no head SHA is the same absence one level up, and
// the aggregate is the ONE shared rule (TestFakeImplementsTheInterfaces
// pins the parity, so the two implementations cannot drift).

func (f *Fake) ReadChecks(ctx context.Context, sha string) (ChecksReport, error) {
	if err := f.recordGate("ReadChecks", sha); err != nil {
		return ChecksReport{}, err
	}
	f.mu.Lock()
	seed, ok := f.checks[sha]
	var complete bool
	var checks []Check
	if ok {
		complete = seed.complete
		checks = append([]Check(nil), seed.checks...)
	}
	f.mu.Unlock()
	if !ok {
		return ChecksReport{}, NewMsg(KindNotFound, fmt.Sprintf("commit %s is unknown to the forge — its checks cannot be read (an absence, not a red gate)", sha))
	}
	rep := ChecksReport{SHA: sha, Source: "fake", Complete: complete, Checks: checks}
	rep.State, rep.Reason = Aggregate(rep.Checks, rep.Complete)
	return rep, nil
}

func (f *Fake) ListStatuses(ctx context.Context, pr int) ([]Check, error) {
	if err := f.recordGate("ListStatuses", pr); err != nil {
		return nil, err
	}
	p, err := f.ReadPR(ctx, pr)
	if err != nil {
		return nil, err
	}
	if p.HeadSHA == "" {
		return nil, NewMsg(KindNotFound, fmt.Sprintf("PR %d reports no head SHA — its checks cannot be read (an absence, not a red gate)", pr))
	}
	rep, err := f.ReadChecks(ctx, p.HeadSHA)
	if err != nil {
		return nil, err
	}
	return rep.Checks, nil
}

// --- reviews ---------------------------------------------------------------

func (f *Fake) ListReviews(ctx context.Context, pr int) ([]Review, error) {
	if err := f.recordGate("ListReviews", pr); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	revs := f.reviews[pr]
	out := make([]Review, len(revs))
	copy(out, revs)
	return out, nil
}

// SeedReview places a review in a PR's list as the scenario needs it —
// identical bodies are expected and stay distinct (T1's pin; T7's
// reconcile-by-id scenarios depend on it).
func (f *Fake) SeedReview(pr int, body string, verdict Verdict) Review {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := Review{ID: int64(f.next["review"]), PR: pr, Body: body, Verdict: verdict, Author: "fake"}
	f.next["review"]++
	f.reviews[pr] = append(f.reviews[pr], r)
	return r
}

// SeedReviewAt is SeedReview with the server's own timestamp (S57 T2):
// the scenario where the gate's review_at evidence carries WHEN.
func (f *Fake) SeedReviewAt(pr int, body string, verdict Verdict, submittedAt string) Review {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := Review{ID: int64(f.next["review"]), PR: pr, Body: body, Verdict: verdict, Author: "fake", SubmittedAt: submittedAt}
	f.next["review"]++
	f.reviews[pr] = append(f.reviews[pr], r)
	return r
}

func (f *Fake) PostReview(ctx context.Context, pr int, body string, verdict Verdict) (*Review, error) {
	if err := f.recordGate("PostReview", pr, body, string(verdict)); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	r := Review{ID: int64(f.next["review"]), PR: pr, Body: body, Verdict: verdict, Author: "fake"}
	f.next["review"]++
	f.reviews[pr] = append(f.reviews[pr], r)
	return &r, nil
}

// --- copies ----------------------------------------------------------------

func copyIssue(in Issue) *Issue {
	out := in
	out.Labels = append([]string(nil), in.Labels...)
	return &out
}

func copyPR(in PR) *PR { out := in; return &out }

func hasLabel(labels []string, want string) bool {
	for _, l := range labels {
		if l == want {
			return true
		}
	}
	return false
}

// --- S61 T4 (GI10): the repository doors over the same log ----------------

// SeedRepo stands a repository in front of the fake: GetRepo answers it,
// CreateRepo refuses its name as taken. Empty defaults to TRUE for a
// bare seed — a world where the name exists but holds no commits is the
// common half of the adopt rule's test, and a test that wants a populated
// repo sets Empty false on the seed.
func (f *Fake) SeedRepo(owner, name string, r Repo) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.repos == nil {
		f.repos = map[string]*Repo{}
	}
	seeded := r
	// example.invalid is RFC 2606-reserved: a stand-in address that can
	// never resolve — and the fake names no URL scheme, which the
	// package's own no-network walk would (correctly) refuse.
	if seeded.URL == "" {
		seeded.URL = "example.invalid/" + owner + "/" + name
	}
	if seeded.CloneURL == "" {
		seeded.CloneURL = seeded.URL + ".git"
	}
	f.repos[owner+"/"+name] = &seeded
}

func (f *Fake) GetRepo(ctx context.Context, owner, name string) (*Repo, error) {
	if err := f.recordGate("GetRepo", owner, name); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if r, ok := f.repos[owner+"/"+name]; ok {
		c := *r
		return &c, nil
	}
	return nil, nil // absence is the answer, not an error (Find* discipline)
}

// CreateRepo records the REQUEST as an argument — the test surface GI10
// needs: "the body was inspected and carried no charter text" is an
// assertion on this log entry. An existing name answers the API's own
// conflict (name taken); FailMethod("CreateRepo", …) overrides either.
func (f *Fake) CreateRepo(ctx context.Context, req RepoRequest) (*Repo, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	if err := f.recordGate("CreateRepo", req); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	key := req.Owner + "/" + req.Name
	if _, ok := f.repos[key]; ok {
		return nil, NewMsg(KindConflict, "conflict: name already exists")
	}
	r := &Repo{
		URL:           "example.invalid/" + req.Owner + "/" + req.Name,
		CloneURL:      "example.invalid/" + req.Owner + "/" + req.Name + ".git",
		DefaultBranch: "main",
		Private:       req.Private,
		Empty:         true,
	}
	f.repos[key] = r
	c := *r
	return &c, nil
}
