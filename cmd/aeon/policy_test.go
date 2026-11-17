package main

// T12's tests — `aeon policy` over a REAL temp store (store.Open with Dir,
// the daemon-less idiom of db_test.go / loop/pin_test.go) and over a spy
// repo that COUNTS every write (the spec's "a Repo/store spy counts
// writes"). The real user store is never opened: the subprocess walk sets
// XDG_STATE_HOME to a temp dir exactly like TestDBInfoWorksWithTheDaemonDown,
// and the in-process verbs reach only store.Open({Dir: t.TempDir()}) or the
// spy.
//
// Companion files: policy_queries_cases_test.go (reads), policy_authoring_cases_test.go
// (writes), policy_commands_cases_test.go (command rules), policy_audit_cases_test.go
// (events), policy_cli_cases_test.go (process/flags), policy_persistence_cases_test.go
// (store bridge), and policy_guards_cases_test.go (surface locks).

import (
	"context"
	"fmt"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/store"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

// --- the spy repo (the spec's write counter) ----------------------------------

type spyCalls struct {
	add, remove, listAll    int
	accRecord, accList      int
	eventInsert, eventQuery int
	eventTrim, eventStats   int
	lastTrim                time.Time
	lastStatsSubject        string   // F-13: what depth was asked FOR, last call
	statsSubjects           []string // …and in order — the listing's read is the first
}

// spyRepo implements policy.Repo and counts every call. Writes are
// add/remove/accRecord/eventInsert/eventTrim; the read half exists so the
// zero-writes spy can also prove `why` touches no event or acceptance read.
type spyRepo struct {
	rows        []policy.Row
	acceptances []policy.AcceptanceRow
	events      []policy.EventRow
	trimErr     error
	listErr     error
	removeErr   error
	queryErr    error
	// addErrOn / eventErrOn fail exactly the row/event with a matching id —
	// the seed verb's tests drive the two mid-run failure arms (a rule the
	// boundary refuses; a row stored whose audit event does not land) with
	// the store failing selectively, which is how those arms actually break.
	addErrOn   string
	eventErrOn string
	calls      spyCalls
}

func (f *spyRepo) PolicyAdd(_ context.Context, r policy.Row) error {
	f.calls.add++
	if f.addErrOn != "" && r.ID == f.addErrOn {
		return fmt.Errorf("spy: the store refused the row %q", r.ID)
	}
	f.rows = append(f.rows, r)
	return nil
}

func (f *spyRepo) PolicyRemove(_ context.Context, id string) (bool, error) {
	f.calls.remove++
	if f.removeErr != nil {
		return false, f.removeErr
	}
	for i, r := range f.rows {
		if r.ID == id {
			f.rows = append(f.rows[:i], f.rows[i+1:]...)
			return true, nil
		}
	}
	return false, nil
}

func (f *spyRepo) PolicyListAll(_ context.Context) ([]policy.Row, error) {
	f.calls.listAll++
	if f.listErr != nil {
		return nil, f.listErr
	}
	return append([]policy.Row(nil), f.rows...), nil
}

func (f *spyRepo) PolicyAcceptanceRecord(_ context.Context, policyID, targetKind, targetID, acceptedBy string, at time.Time) error {
	f.calls.accRecord++
	f.acceptances = append(f.acceptances, policy.AcceptanceRow{
		PolicyID: policyID, TargetKind: targetKind, TargetID: targetID,
		AcceptedBy: acceptedBy, AcceptedAt: at.UTC().Format(time.RFC3339Nano),
	})
	return nil
}

func (f *spyRepo) PolicyAcceptanceListFor(_ context.Context, targetKind, targetID string) ([]policy.AcceptanceRow, error) {
	f.calls.accList++
	var out []policy.AcceptanceRow
	for _, a := range f.acceptances {
		if a.TargetKind == targetKind && a.TargetID == targetID {
			out = append(out, a)
		}
	}
	return out, nil
}

func (f *spyRepo) PolicyEventInsert(_ context.Context, kind, subject, policyID, detail string, at time.Time) error {
	f.calls.eventInsert++
	if f.eventErrOn != "" && policyID == f.eventErrOn {
		return fmt.Errorf("spy: the store refused the audit row for %q", policyID)
	}
	kindPart, idPart, _ := strings.Cut(subject, ":")
	f.events = append(f.events, policy.EventRow{
		ID: int64(len(f.events) + 1), PolicyID: policyID,
		SubjectKind: kindPart, SubjectID: idPart,
		Outcome: kind, Detail: detail, At: at.UTC().Format(time.RFC3339Nano),
	})
	return nil
}

func (f *spyRepo) PolicyEventQuery(_ context.Context, subject string, limit int) ([]policy.EventRow, error) {
	f.calls.eventQuery++
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	var out []policy.EventRow
	for _, r := range f.events {
		if subject == "" || subject == r.SubjectKind+":"+r.SubjectID {
			out = append(out, r)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (f *spyRepo) PolicyEventTrim(_ context.Context, olderThan time.Time) (int, error) {
	f.calls.eventTrim++
	f.calls.lastTrim = olderThan
	if f.trimErr != nil {
		return 0, f.trimErr
	}
	return 0, nil
}

// PolicyEventStats satisfies the read half of the spy — filtered (F-13): the
// depth and oldest cover exactly the rows the listing shows, and the subject it
// was handed is recorded so a CLI test can assert the filter travelled from the
// --subject flag through the boundary to the seam.
func (f *spyRepo) PolicyEventStats(_ context.Context, subject string) (int, time.Time, error) {
	f.calls.eventStats++
	f.calls.lastStatsSubject = subject
	f.calls.statsSubjects = append(f.calls.statsSubjects, subject)
	depth := 0
	var oldest time.Time
	for _, r := range f.events {
		if subject != "" && subject != r.SubjectKind+":"+r.SubjectID {
			continue
		}
		depth++
		at, err := time.Parse(time.RFC3339Nano, r.At)
		if err != nil {
			return 0, time.Time{}, err
		}
		if oldest.IsZero() || at.Before(oldest) {
			oldest = at
		}
	}
	return depth, oldest, nil
}

// --- harness -------------------------------------------------------------------

func withSpy(t *testing.T, spy *spyRepo) {
	t.Helper()
	prev := policyBoundary
	policyBoundary = func() (*policy.Store, func(), error) {
		return policy.NewStore(spy), func() {}, nil
	}
	t.Cleanup(func() { policyBoundary = prev })
}

// withTempStore points the verbs at a REAL store under t.TempDir() — never
// the user's (the store.Open({Dir: …}) idiom internal/daemon and internal/loop
// tests use).
func withTempStore(t *testing.T) {
	t.Helper()
	s, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("open temp store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	prev := policyBoundary
	policyBoundary = func() (*policy.Store, func(), error) {
		return policy.NewStore(policyRepo(s)), func() {}, nil
	}
	t.Cleanup(func() { policyBoundary = prev })
}

func runPolicy(t *testing.T, args ...string) (string, error) {
	t.Helper()
	// Same appenv-pin guard as runProposal (S45 T10; the T8 Notes #8
	// mechanism): every policy verb starts with selectPolicyDoor's dial, and
	// dial ends at appenv.Apply(), which os.Setenvs the store paths
	// process-wide. Without the restore, the NEXT test's fixture inherits
	// THIS test's store — visible now that agent_import commits actually
	// import (a refused commit never reached the row-writing effect).
	restore := map[string]string{}
	for _, k := range appenvPinnedEnv {
		restore[k] = os.Getenv(k)
	}
	defer func() {
		for k, v := range restore {
			if v == "" {
				_ = os.Unsetenv(k)
			} else {
				_ = os.Setenv(k, v)
			}
		}
	}()
	var b strings.Builder
	err := cmdPolicy(&b, args)
	return b.String(), err
}

func mustPolicy(t *testing.T, args ...string) string {
	t.Helper()
	out, err := runPolicy(t, args...)
	if err != nil {
		t.Fatalf("aeon policy %v: %v\n%s", args, err, out)
	}
	return out
}

// toolRow seeds one stored ToolAccess policy row through the SAME shape the
// write boundary hands the repo (List re-decodes it; the closed set judges
// every field on the way back, so these fixtures must be real).
func toolRow(id, kind, targetID, action, spec, notAfter string) policy.Row {
	subject := kind
	if targetID != "" {
		subject = kind + ":" + targetID
	}
	return policy.Row{
		ID: id, Type: "tool_access", TargetKind: kind, TargetID: targetID,
		Action: action, Source: "operator", Subject: subject,
		SpecJSON: spec, NotAfter: notAfter, CreatedAt: "2026-10-01T00:00:00Z",
	}
}

// TestOnlyProposalIsSocketWritable is S35 T6's amendment (TD10) — a
// SIBLING function, not a growth of TestNoPolicyAuthoringReachesTheNarrowSurface
// above (left byte-for-byte unchanged by this task; `git diff` against the
// parent commit shows only additions below its closing brace). Recorded in
// Notes which of design.md's two drafting options this took, and why.
//
// TD10(b)'s own original sketch ("scan internal/protocol/ for Exec(/INSERT
// INTO/UPDATE") was written before the seam architecture existed and would
// find NOTHING — internal/protocol never contains SQL; handlers call
// through package-local interfaces (ProposalStore) or daemon.Options
// func-seams (AgentTransition), and the real writes live in
// internal/store/*.go and cmd/aeon's seam files. There is also no
// structural read/write marker on protocol.Method itself (ReadOnly exists
// only on the unrelated, narrower AgentServiceMethod) to enumerate "every
// write method" against cleanly.
//
// The real, checkable version: the two functions that actually COMMIT a
// proposal's effect — store.DecideProposal (the flip) and mcp.SaveConfig
// (T4's one config writer) — must have exactly ONE caller anywhere in the
// repository: cmd/aeon/proposal.go (T5's human-only door). This is the
// SAME whitelist-by-caller-file mechanism allowedBoundary above already
// uses for policy's own Add/Remove (which T5 already extended to admit
// cmd/aeon/proposal.go, confirmed unchanged by this task) — applied to the
// two functions this slice adds. If either function is ever called from
// ANY other file — above all a socket handler's seam implementation — the
// side door brief §7 forbids has opened.
var (
	decideProposalCallRE = regexp.MustCompile(`\.DecideProposal\(`)
	saveConfigCallRE     = regexp.MustCompile(`\bSaveConfig\(`)
	allowedCommitEffect  = map[string]bool{
		// S45 T5 (PM4 — two doors, one implementation): the claim moved
		// behind desk.Commit/desk.Reject; the CLI door (cmd/aeon/
		// proposal.go) is now a CALLER of the desk, and the store's
		// claimed-from flip is reached only by the two sanctioned shims —
		// the CLI's (cmd/aeon/proposal_decide.go) and the socket's
		// (internal/daemon/proposal_decide.go) — plus the orchestration
		// that owns the step order (internal/desk/decide.go). SaveConfig
		// is NOT here: the MCP effect stays a seam in cmd/aeon/proposal.go.
		"cmd/aeon/proposal.go":               true,
		"cmd/aeon/proposal_decide.go":        true,
		"internal/daemon/proposal_decide.go": true,
		"internal/desk/decide.go":            true,
		// S61 T6: the intake engine marks its own intake_plan applied when
		// its ledger completes, under the committing person. The generic
		// doors REFUSE that kind (T5 — an intake_plan reaching them is an
		// error), so the intake.commit seam is the one caller it can have;
		// the engine holds no decide logic and calls the store's decide
		// verbatim (GI7's dedicated door, GI13's link).
		"internal/intake/resume.go": true,
	}
)

// commitEffectOffenses reports, for one file's text, every reason it would
// fail the whitelist below — factored out so the canary (TestTheCommitEffectWhitelistHasTeeth)
// can prove the IDENTICAL regexes/whitelist reject a forbidden call, not a
// copy that could silently diverge from what the real walk uses.
func commitEffectOffenses(rel, text string) []string {
	if allowedCommitEffect[rel] {
		return nil
	}
	var out []string
	if decideProposalCallRE.MatchString(text) {
		out = append(out, "calls DecideProposal — committing/rejecting is confined to cmd/aeon/proposal.go (brief §7: the human-only door)")
	}
	for _, m := range saveConfigCallRE.FindAllStringIndex(text, -1) {
		// Exclude the definition line itself (`func SaveConfig(`).
		start := m[0]
		before := text[max(0, start-5):start]
		if strings.HasSuffix(before, "func ") {
			continue
		}
		out = append(out, "calls SaveConfig — the one MCP config writer is confined to cmd/aeon/proposal.go (T5's human-only door)")
	}
	return out
}
