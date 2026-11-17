// Package forge is the typed boundary to the code forge — GitHub, and only
// GitHub until somebody needs otherwise.
//
// Typed named methods, not a generic call: the design's argument (S16) is
// that **a typed interface with per-forge implementations beats discovering
// a tool by function and hoping** — which is what v1's runtime MCP
// tool-name discovery did, and TestInterfaceIsTypedNotGeneric keeps it from
// creeping back through a Call(name, payload) convenience.
//
// The interface has NO implementation in this package until T11 (O7/I7):
// every automated test runs against Fake, and the suite never needs a
// network or a token. TestNoNetworkInAnyTest is the machine pin of that.
package forge

import (
	"context"
	"regexp"
	"strconv"

	"github.com/cjlapao/aeon/internal/errs"
)

// Issue is a tracker issue as the actions need to see it. State is the
// closed pair the loop reasons over; anything richer stays on the forge.
type Issue struct {
	Number int
	Title  string
	Body   string
	Labels []string
	State  string // "open" | "closed"
}

// IssueRequest is what forge.openIssue sends. The label is what the
// open-issue search keys on together with the EXACT title — finding prior
// work by remembering a number is not finding.
type IssueRequest struct {
	Title  string
	Body   string
	Labels []string
}

// PR is a pull request.
//
// BaseRef is REQUIRED (L26, D5): the phantom-merge guard reads it before
// merging — a PR merged into the wrong base looks merged, reports merged,
// and its work is not where the loop believes it is. An interface that
// omitted this one field would make the guard untestable, which is why it
// is named in the design rather than left to be discovered.
type PR struct {
	Number   int
	Head     string
	BaseRef  string
	Merged   bool
	MergeSHA string
	// HeadSHA is the head commit's SHA (GitHub `pull.head.sha`), S57 T2.
	// It is the key the checks are read by — a PR head now, the base
	// branch after merge. Empty when the server does not report it; the
	// omitempty keeps the struct's JSON shape byte-identical for servers
	// that do not (T1's wire goldens pin the pre-T2 shape).
	HeadSHA string `json:"headSHA,omitempty"`
}

// PRRequest is what forge.openPR sends. Head and Base are branch names;
// the response's BaseRef carries the base back for the guard's sake.
type PRRequest struct {
	Title string
	Body  string
	Head  string
	Base  string
}

// MergeMethod is the closed set of merge strategies. The design's guard
// reasons about squash specifically — a squash rewrites history, which is
// why only the merge SHA it produces can be an ancestor of the feature
// branch, never the task branch's original commits.
type MergeMethod string

const (
	MergeSquash MergeMethod = "squash"
	MergeCommit MergeMethod = "merge"
	MergeRebase MergeMethod = "rebase"
)

// MergeResult reports what the merge produced. SHA is the commit the
// feature branch now contains — the thing the ancestor assertion takes.
type MergeResult struct {
	SHA    string
	Merged bool
}

// Verdict is the closed set of review verdicts.
type Verdict string

const (
	VerdictApprove        Verdict = "approve"
	VerdictRequestChanges Verdict = "request_changes"
	VerdictComment        Verdict = "comment"
)

// Review is a posted review. ID is the only thing that distinguishes two
// reviews with identical bodies — the property that makes forge.postReview
// the receipt-first exception (reconciled BY ID, never by body text).
type Review struct {
	ID      int64
	PR      int
	Body    string
	Verdict Verdict
	Author  string
	// SubmittedAt is what the SERVER said when the review landed
	// (RFC 3339). S17 T8 adds it because the merge gate's evidence must
	// record WHEN (O6/D10) — a gate that passed on a state nobody can
	// timestamp is a gate nobody can audit. Empty when the server does
	// not report it, and the gate records the absence rather than
	// inventing a clock reading. S57 T2 sets it from GitHub's
	// `submitted_at`. No tag on purpose: T1's wire goldens pin the
	// untagged `"SubmittedAt":""` for servers that omit it, and the
	// pinned bytes stay pinned — only the value starts to fill in.
	SubmittedAt string
}

// The closed State vocabulary of a Check (S57 VG8). A string the forge
// cannot say is `unknown` — never `success`: an optimistic mapping turns
// a misbehaving API into a green gate right before something irreversible.
const (
	CheckSuccess = "success"
	CheckFailure = "failure"
	CheckPending = "pending"
	CheckUnknown = "unknown"
)

// Check is one CI status as the server reports it. State carries the
// server's verdict ("success", "failure", "pending", …); the merge gate
// passes only on success and records the whole list verbatim.
type Check struct {
	Name  string
	State string
}

// Thread is an unresolved review thread — honoured at the gate where a
// server reports it, never resolved by Aeon (T8's not-in-scope line).
type Thread struct {
	Path   string
	Line   int
	Author string
}

// StatusLister and ThreadLister are OPTIONAL capabilities, asserted —
// not required — because the protocol's whole argument is
// server-agnosticism: "the granular reference server has NO
// resolve_thread / pending-review workflow", so a merge path that
// REQUIRED either would be unreachable on the servers that lack it.
// Absence answers N/A there, and N/A is not a failure.
type StatusLister interface {
	ListStatuses(ctx context.Context, pr int) ([]Check, error)
}

type ThreadLister interface {
	ListUnresolvedThreads(ctx context.Context, pr int) ([]Thread, error)
}

// ChecksReport is the answer to "what does CI say about this commit?"
// (S57 VG8). Checks is the flattened, mapped list — check runs and legacy
// commit statuses alike, each with a Name and a closed State. State is
// the Aggregate over Checks (same four values); Complete is false when
// the report could not be read in full (the page cap) — an incomplete
// report is `unknown`, never `success`. Reason is the one-line account
// (the zero-checks answer is `none-reported`, VG8's honest absence).
type ChecksReport struct {
	SHA      string
	Checks   []Check
	State    string
	Complete bool
	Source   string
	Reason   string
}

// CheckReader reads the checks of a COMMIT (S57 VG8) — keyed by SHA, so
// the same call serves a PR head now and the base branch after merge.
// Like StatusLister it is OPTIONAL and asserted, not required: a server
// that cannot say is not a failing server, and N/A is not a failure.
type CheckReader interface {
	ReadChecks(ctx context.Context, sha string) (ChecksReport, error)
}

// --- S61 T4 (GI10): repository creation is OPTIONAL capability -----------
//
// Creating a repository is outward publication of content, so the
// capability is NOT in the shared Forge interface (the Do-NT): a caller
// must assert RepoCreator/RepoReader and a forge that cannot create says
// so in one sentence. Both are optional and asserted, the StatusLister
// pattern.

// RepoRequest is the closed creation shape: an owner, a name, visibility
// defaulting PRIVATE, and a SHORT description. There is deliberately no
// field for free-form text beyond Description — charter or plan prose has
// no door into a remote request (GI18), and the name is validated by a
// closed regex before anything is sent.
type RepoRequest struct {
	Owner       string
	Name        string
	Private     bool
	Description string
	// OwnerKind names the endpoint explicitly — "user" creates under the
	// authenticated identity (POST /user/repos), "org" under the
	// organization (POST /orgs/{owner}/repos). T0's fact is that GitHub
	// cannot answer "is this login you or an org" from the login alone,
	// so the REQUEST says which door it knocks on instead of guessing.
	OwnerKind string
}

// Repo is the read-back of a repository: the human URL, the clone URL the
// remote will point at, the default branch, and visibility. Empty is the
// honesty field for the adopt rule: a repository with no commits cannot be
// told apart from a missing one by URL alone, and the action adopts an
// EMPTY existing repo only when told to, never a populated one.
type Repo struct {
	URL           string
	CloneURL      string
	DefaultBranch string
	Private       bool
	Empty         bool
}

// The closed name rules (GI10): a repository name is GitHub's own
// permissive set and an owner login is letters, digits and single dashes.
// Anything else is refused HERE, in the one place both the GitHub client
// and every fake share, because a name that reaches the wire unchecked is
// a path segment, and a path segment is an attack surface.
var (
	repoNameRule   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)
	ownerNameRule  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}$`) // RE2 has no lookahead; the closed floor is ours to choose
	ownerKindsRule = regexp.MustCompile(`^(?:user|org)$`)
)

// Validate is the shape check a request must pass before anyone is
// called: closed name rules, and an OwnerKind that names the endpoint
// explicitly (GI10's own words). The short description is length-checked
// (GitHub caps it at 300; a longer one is a plan leaking into a request,
// which is the leak this whole type exists to make impossible).
func (r RepoRequest) Validate() error {
	if !ownerNameRule.MatchString(r.Owner) {
		return NewMsg(KindRefused, "refused: owner "+r.Owner+" is not a login-shaped name")
	}
	if !repoNameRule.MatchString(r.Name) {
		return NewMsg(KindRefused, "refused: repository name "+r.Name+" breaks the closed name rule (letters, digits, dot, dash, underscore; ≤ 100)")
	}
	if !ownerKindsRule.MatchString(r.OwnerKind) {
		return NewMsg(KindRefused, "refused: owner kind "+r.OwnerKind+" names no endpoint — a repository is created under \"user\" (you) or \"org\" (that organization), never a guess")
	}
	if len(r.Description) > 300 {
		return NewMsg(KindRefused, "refused: a repository description is ≤ 300 characters — a longer text is not a description")
	}
	return nil
}

// RepoCreator creates repositories. Optional, asserted.
type RepoCreator interface {
	CreateRepo(ctx context.Context, req RepoRequest) (*Repo, error)
}

// RepoReader reads one repository — the GET of the check-then-act; absence
// is (nil, nil), the Find* answer discipline of this package.
type RepoReader interface {
	GetRepo(ctx context.Context, owner, name string) (*Repo, error)
}

// Aggregate is the ONE rule that turns a list of mapped checks into a
// state (S57 VG8), shared by every implementation so GitHub and Fake
// cannot drift:
//
//	any failure                ⇒ failure
//	else any pending           ⇒ pending
//	else incomplete or any unknown ⇒ unknown
//	else                       ⇒ success
//	zero checks                ⇒ unknown, reason `none-reported`
//
// The precedence is the rule, not a preference: a pending check beside a
// failure is still a failure (the red is the fact that matters), and an
// incomplete report is unknown even when everything it did read was green.
func Aggregate(checks []Check, complete bool) (state, reason string) {
	if len(checks) == 0 {
		return CheckUnknown, "none-reported"
	}
	var firstFailure, firstPending, firstUnknown = -1, -1, -1
	for i, c := range checks {
		switch c.State {
		case CheckFailure:
			if firstFailure < 0 {
				firstFailure = i
			}
		case CheckPending:
			if firstPending < 0 {
				firstPending = i
			}
		case CheckUnknown:
			if firstUnknown < 0 {
				firstUnknown = i
			}
		}
	}
	if firstFailure >= 0 {
		return CheckFailure, "check " + checks[firstFailure].Name + " is " + CheckFailure
	}
	if firstPending >= 0 {
		return CheckPending, "check " + checks[firstPending].Name + " is " + CheckPending
	}
	if !complete {
		return CheckUnknown, "incomplete — the report exceeded the page cap"
	}
	if firstUnknown >= 0 {
		return CheckUnknown, "check " + checks[firstUnknown].Name + " is " + CheckUnknown
	}
	return CheckSuccess, "all " + strconv.Itoa(len(checks)) + " reported checks are " + CheckSuccess
}

// The five discriminated error kinds (O5). A loop's response differs for
// each: retry, stop and tell a human, treat as absent, reconcile, or
// report. "GitHub is down" and "GitHub said no" must never be conflated —
// conflating them sends someone to check their permissions when their
// network is out.
const (
	KindTransport = "transport"
	KindAuth      = "auth"
	KindNotFound  = "not-found"
	KindConflict  = "conflict"
	KindRefused   = "refused"
)

// ForgeError is the domain error of the forge boundary. Infrastructure
// failure outside the boundary stays a plain error (DB19's division). It
// carries its kind AND the three parts (S23 C16): the kind is what a
// caller branches on (asForge reads it), the three parts are what a human
// reads, in the same value. The parts live in an errs.Error, whose
// fields are unexported, so a ForgeError cannot be built with a missing
// part.
type ForgeError struct {
	Kind string
	Err  errs.Error
}

// New is the only constructor.
func NewErr(kind, what, why, do string) *ForgeError {
	return &ForgeError{Kind: kind, Err: errs.New(what, why, do)}
}

// NewMsg is the convenience constructor for a call site with one rich
// message: the message is the "what", the "why"/"do" come from the
// kind's meaning — an actionable, kind-specific next step, never "check
// your configuration" (O4).
func NewMsg(kind, detail string) *ForgeError {
	why, do := kindParts(kind)
	return &ForgeError{Kind: kind, Err: errs.New(detail, why, do)}
}

// kindParts is the ONE place a kind's meaning becomes an actionable why
// and do, table-driven so TestDoIsAnActionNotAdvice reads the same table
// a construction site does.
func kindParts(k string) (why, do string) {
	switch k {
	case KindAuth:
		return "the forge token did not authenticate",
			"set the forge token in the environment or a key file (aeon init --key-file)"
	case KindNotFound:
		return "nothing with that identity exists in the forge",
			"list what exists and name an existing one (aeon forge list)"
	case KindConflict:
		return "a concurrent change the forge refused to reconcile",
			"re-read the forge and retry; the conflict is the current state, not a fault"
	case KindRefused:
		return "the forge understood the call and declined it — a domain refusal, not a transient fault",
			"fix the request so it satisfies the refusal; resending it unchanged will fail again"
	default: // KindTransport
		return "the forge could not be reached — a transient fault",
			"retry; if it persists, run aeon doctor"
	}
}

func (e *ForgeError) Error() string { return e.Kind + ": " + e.Err.Error() }

// What/Why/Do expose the three parts a renderer or a caller can reach
// without the kind; the parts ride the same value as the kind.
func (e *ForgeError) What() string { return e.Err.What() }
func (e *ForgeError) Why() string  { return e.Err.Why() }
func (e *ForgeError) Do() string   { return e.Err.Do() }

// Forge is the whole forge surface the eight actions need. Find* answer
// absence with (nil, nil) — absence is the normal answer that routes
// check-then-act to its create step, not an error; Read* answer absence
// with KindNotFound because something read by identity was expected to be
// there.
type Forge interface {
	FindOpenIssue(ctx context.Context, title string, label string) (*Issue, error)
	OpenIssue(ctx context.Context, req IssueRequest) (*Issue, error)
	ReadIssue(ctx context.Context, number int) (*Issue, error)
	CloseIssue(ctx context.Context, number int) error

	FindOpenPR(ctx context.Context, head string) (*PR, error)
	OpenPR(ctx context.Context, req PRRequest) (*PR, error)
	ReadPR(ctx context.Context, number int) (*PR, error)
	MergePR(ctx context.Context, number int, method MergeMethod) (*MergeResult, error)

	ListReviews(ctx context.Context, pr int) ([]Review, error)
	PostReview(ctx context.Context, pr int, body string, verdict Verdict) (*Review, error)
}
