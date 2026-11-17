package policy_test

import (
	"context"
	"errors"
	"go/ast"
	"go/token"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/policy"
)

// T7 — acceptance: recorded, idempotent, and never a gate.
//
// The load-bearing test here asserts a NEGATIVE (PL10/D13): an unaccepted
// policy is still enforced. Everything else in the file is the bookkeeping
// that makes acceptance worth having — "since when" (the first instant,
// immovable), "per target" (independently queryable rows) — plus the guards
// that keep the record from ever growing into the gate it must never be:
// the evaluator does not read policy_acceptance, and cannot be made to.
//
// The acceptance half of fakeRepo (the methods on its acceptance fields)
// lives in this file too: the fake mirrors the table's PRIMARY KEY faithfully
// (re-record = no-op keeping the first row), and the real ON CONFLICT SQL is
// tested against SQLite in internal/store/policy_test.go.

// ── the acceptance half of the fake ─────────────────────────────────────────

// recordedAcceptance is one RecordAcceptance call as the boundary handed it
// down — the forwarding claim ("the seam gets exactly the validated values")
// is asserted against this, not against the stored rendering.
type recordedAcceptance struct {
	policyID   string
	targetKind string
	targetID   string
	acceptedBy string
	at         time.Time
}

// PolicyAcceptanceRecord satisfies the grown Repo. It mirrors ON CONFLICT
// (policy_id, target_kind, target_id) DO NOTHING: the first row stands, whole
// — instant and accepted_by included — and the re-record is not an error.
func (f *fakeRepo) PolicyAcceptanceRecord(_ context.Context, policyID, targetKind, targetID, acceptedBy string, at time.Time) error {
	f.recordCalls++
	f.recorded = append(f.recorded, recordedAcceptance{
		policyID: policyID, targetKind: targetKind, targetID: targetID, acceptedBy: acceptedBy, at: at,
	})
	if f.recordErr != nil {
		return f.recordErr
	}
	for _, r := range f.acceptances {
		if r.PolicyID == policyID && r.TargetKind == targetKind && r.TargetID == targetID {
			return nil // the first record stands; a re-record is a no-op, not an error
		}
	}
	f.acceptances = append(f.acceptances, policy.AcceptanceRow{
		PolicyID:   policyID,
		TargetKind: targetKind,
		TargetID:   targetID,
		AcceptedBy: acceptedBy,
		AcceptedAt: at.UTC().Format(time.RFC3339Nano),
	})
	return nil
}

// PolicyAcceptanceListFor satisfies the grown Repo: the per-target query, in
// insertion order (the real store orders by policy_id; the ordering claim is
// made there, against SQL).
func (f *fakeRepo) PolicyAcceptanceListFor(_ context.Context, targetKind, targetID string) ([]policy.AcceptanceRow, error) {
	f.listForCalls++
	if f.listForErr != nil {
		return nil, f.listForErr
	}
	var out []policy.AcceptanceRow
	for _, r := range f.acceptances {
		if r.TargetKind == targetKind && r.TargetID == targetID {
			out = append(out, r)
		}
	}
	return out, nil
}

// ── helpers ──────────────────────────────────────────────────────────────────

// The instants every acceptance in this file is recorded at. Fixed (D5: the
// clock is an input), with distinct sub-second tails so a rendering that
// dropped nanoseconds would fail the first-instant assertions too.
var (
	acceptT1     = time.Date(2026, 10, 19, 11, 0, 0, 123456789, time.UTC)
	acceptT2     = time.Date(2026, 10, 19, 12, 30, 0, 987654321, time.UTC)
	acceptT1Text = "2026-10-19T11:00:00.123456789Z"
	acceptT2Text = "2026-10-19T12:30:00.987654321Z"
)

func targetOf(t *testing.T, kind policy.TargetKind, id string) policy.Target {
	t.Helper()
	target, rej := policy.ParseTarget(kind, id)
	if rej != nil {
		t.Fatalf("helper built an illegal target %q/%q: %s", kind, id, rej.Reason)
	}
	return target
}

func mustRecord(t *testing.T, s *policy.Store, policyID string, target policy.Target, acceptedBy string, at time.Time) {
	t.Helper()
	if rej := s.RecordAcceptance(context.Background(), policyID, target, acceptedBy, at); rej != nil {
		t.Fatalf("RecordAcceptance(%q, %s) refused: %s", policyID, target.String(), rej.Reason)
	}
}

func refuseRecord(t *testing.T, s *policy.Store, policyID string, target policy.Target, acceptedBy string, at time.Time) string {
	t.Helper()
	rej := s.RecordAcceptance(context.Background(), policyID, target, acceptedBy, at)
	if rej == nil {
		t.Fatalf("RecordAcceptance(%q, %s) was ACCEPTED; it must be refused", policyID, target.String())
	}
	return rej.Reason
}

func mustAcceptances(t *testing.T, s *policy.Store, target policy.Target) []policy.Acceptance {
	t.Helper()
	got, rej := s.AcceptancesFor(context.Background(), target)
	if rej != nil {
		t.Fatalf("AcceptancesFor(%s) refused: %s", target.String(), rej.Reason)
	}
	return got
}

// ── THE test: an unaccepted policy is still enforced (PL10/D13) ──────────────

func TestUnacceptedPolicyIsStillEnforced(t *testing.T) {
	// The whole security argument of the acceptance record, as a negative:
	// enforcement does not consult acceptance, so declining to accept buys
	// an evader NOTHING. A deny is stored through T6's Add; NO acceptance is
	// recorded; the real evaluator path (List → Evaluate over the stored row)
	// denies. Then the acceptance is recorded — the audit answer gains a row,
	// the verdict changes not one byte. If this test ever passes only because
	// someone kept the two halves in one file and forgot why, the grep below
	// (TestEvaluateDoesNotReadPolicyAcceptance) is the backstop.
	repo := &fakeRepo{}
	s := policy.NewStore(repo)
	p := boundaryPolicy(t, "no-bash", policy.AgentKind, "boss", policy.Deny, policy.Operator, denySpec())
	mustAdd(t, s, p)

	req := policy.Request{Type: policy.ToolAccess, Agent: "boss", Tool: "bash"}

	// Half one: no acceptance exists. The deny must bite.
	if repo.recordCalls != 0 || len(repo.acceptances) != 0 {
		t.Fatalf("the test itself recorded %d acceptances — the point is that none exists", len(repo.acceptances))
	}
	ps1, rej := s.List(context.Background(), evalAt)
	if rej != nil {
		t.Fatalf("List refused its own row: %s", rej.Reason)
	}
	v1 := policy.Evaluate(ps1, req, evalAt)
	if v1.Allowed {
		t.Fatalf("an UNACCEPTED deny did not stop the request — acceptance has become a gate (PL10)")
	}
	if v1.By == nil || v1.By.ID != "no-bash" || v1.DefaultDeny {
		t.Fatalf("the denial did not name the policy: %+v", v1)
	}

	// Half two: the acceptance is recorded. The audit question now has an
	// answer…
	if rej := s.RecordAcceptance(context.Background(), "no-bash", targetOf(t, policy.AgentKind, "boss"), "boss", acceptT1); rej != nil {
		t.Fatalf("recording the acceptance was refused: %s", rej.Reason)
	}
	if len(repo.acceptances) != 1 {
		t.Fatalf("the acceptance did not land: %+v", repo.acceptances)
	}

	// …and the verdict is byte-identical. Not "also denies" — the SAME verdict,
	// field for field, including which allow it overrode and which policy it
	// names. Enforcement read nothing from the record table, because it could
	// not: Evaluate takes ([]Policy, Request, time.Time) and nothing else.
	ps2, rej := s.List(context.Background(), evalAt)
	if rej != nil {
		t.Fatalf("List after the record: %s", rej.Reason)
	}
	v2 := policy.Evaluate(ps2, req, evalAt)
	if !reflect.DeepEqual(v1, v2) {
		t.Errorf("recording an acceptance MOVED the verdict:\n got  %+v\nwant %+v", v2, v1)
	}
	if v1.Sentence() != v2.Sentence() {
		t.Errorf("the rendered sentence drifted: %q vs %q", v1.Sentence(), v2.Sentence())
	}

	// And the acceptance is still queryable as the record it is — the answer
	// to "since when" exists precisely because enforcement never needed it.
	acc := mustAcceptances(t, s, targetOf(t, policy.AgentKind, "boss"))
	if len(acc) != 1 || acc[0].PolicyID != "no-bash" || acc[0].AcceptedAt != acceptT1Text {
		t.Errorf("the record an auditor asks for is not there: %+v", acc)
	}
}

// ── idempotence: the first instant does not move ─────────────────────────────

func TestAcceptanceIsIdempotentAndKeepsTheFirstInstant(t *testing.T) {
	// "Since when" means the FIRST time or it answers nothing: a repeated
	// accept (a retry, a crash, a confused loop) must not rewrite history —
	// T10's disagreement record reads this instant, and the later date would
	// turn "knew in the morning" into "knew at teatime". The second call is
	// not an error either: idempotence that returns an error is just a
	// flakier interface.
	repo := &fakeRepo{}
	s := policy.NewStore(repo)
	mustAdd(t, s, boundaryPolicy(t, "shared", policy.AgentKind, "boss", policy.Deny, policy.Operator, denySpec()))
	target := targetOf(t, policy.AgentKind, "boss")

	mustRecord(t, s, "shared", target, "operator", acceptT1)
	mustRecord(t, s, "shared", target, "someone-else", acceptT2) // second call: no error

	if repo.recordCalls != 2 {
		t.Errorf("the seam saw %d calls, want 2 — the boundary forwards; the NO-OP lives in the write", repo.recordCalls)
	}
	if len(repo.acceptances) != 1 {
		t.Fatalf("%d rows for one (policy, target): %+v", len(repo.acceptances), repo.acceptances)
	}
	row := repo.acceptances[0]
	if row.AcceptedAt != acceptT1Text {
		t.Errorf("the row reports %q; the first instant was %q — a moved instant rewrites the audit", row.AcceptedAt, acceptT1Text)
	}
	if row.AcceptedBy != "operator" {
		t.Errorf("the row reports accepted_by %q; DO NOTHING keeps the FIRST row whole, not just its date", row.AcceptedBy)
	}

	acc := mustAcceptances(t, s, target)
	if len(acc) != 1 || acc[0].AcceptedAt != acceptT1Text || acc[0].AcceptedBy != "operator" {
		t.Errorf("AcceptancesFor drifted from the row: %+v", acc)
	}

	// The forwarding claim, asserted at the seam rather than the table: what
	// crossed was exactly the validated values and the caller's instant —
	// untouched, un-rounded, not "now".
	first := repo.recorded[0]
	if first.policyID != "shared" || first.targetKind != "agent" || first.targetID != "boss" ||
		first.acceptedBy != "operator" || !first.at.Equal(acceptT1) {
		t.Errorf("the seam received %+v", first)
	}
}

// ── per target: two rows, each answerable alone ──────────────────────────────

func TestAcceptanceIsRecordedPerTarget(t *testing.T) {
	// The PK is (policy_id, target_kind, target_id): the same policy known to
	// agent:a and to loop:l is TWO rows, and "was loop:l told?" must be
	// answerable without tripping over agent:a. A table that collapsed them
	// would silently lose one half of "who knew".
	repo := &fakeRepo{}
	s := policy.NewStore(repo)
	mustAdd(t, s, boundaryPolicy(t, "dual", policy.AgentKind, "a", policy.Deny, policy.Operator, denySpec()))

	mustRecord(t, s, "dual", targetOf(t, policy.AgentKind, "a"), "a", acceptT1)
	mustRecord(t, s, "dual", targetOf(t, policy.LoopKind, "l"), "boss", acceptT2)

	if len(repo.acceptances) != 2 {
		t.Fatalf("%d rows, want 2: %+v", len(repo.acceptances), repo.acceptances)
	}

	accA := mustAcceptances(t, s, targetOf(t, policy.AgentKind, "a"))
	if len(accA) != 1 || accA[0].Target.Kind != policy.AgentKind || accA[0].Target.ID != "a" ||
		accA[0].AcceptedBy != "a" || accA[0].AcceptedAt != acceptT1Text {
		t.Errorf("the agent row is wrong or contaminated: %+v", accA)
	}
	accL := mustAcceptances(t, s, targetOf(t, policy.LoopKind, "l"))
	if len(accL) != 1 || accL[0].Target.Kind != policy.LoopKind || accL[0].Target.ID != "l" ||
		accL[0].AcceptedBy != "boss" || accL[0].AcceptedAt != acceptT2Text {
		t.Errorf("the loop row is wrong or contaminated: %+v", accL)
	}
}

func TestAllAgentsAcceptanceCarriesTheEmptyTargetID(t *testing.T) {
	// all-agents carries no id through ParseTarget — so its acceptance row is
	// keyed by the EMPTY target id (the column is NOT NULL; the store-side
	// twin of this test reads the stored bytes). The round trip must survive
	// the parser on the way out, which is exactly what a hand-edited
	// all-agents row with an id would fail.
	s := policy.NewStore(&fakeRepo{})
	mustAdd(t, s, boundaryPolicy(t, "wide", policy.AllAgents, "", policy.Deny, policy.Operator, denySpec()))
	wide := targetOf(t, policy.AllAgents, "")

	mustRecord(t, s, "wide", wide, "operator", acceptT1)
	acc := mustAcceptances(t, s, wide)
	if len(acc) != 1 || acc[0].Target.Kind != policy.AllAgents || acc[0].Target.ID != "" {
		t.Fatalf("the all-agents round trip broke: %+v", acc)
	}
	if acc[0].AcceptedAt != acceptT1Text {
		t.Errorf("AcceptedAt is %q, want the recorded instant", acc[0].AcceptedAt)
	}
}

// ── validation: a hand-cast Target is input, not a guarantee ─────────────────

func TestRecordAcceptanceValidatesTheTargetBeforeTheRepo(t *testing.T) {
	// The target-validation decision, pinned: RecordAcceptance re-runs
	// ParseTarget on whatever struct it is handed (SEC-015/007 — a check
	// downstream is a check with a bypass). Each case is a Target that only a
	// cast could build, and none of them may reach the seam.
	cases := []struct {
		name   string
		target policy.Target
		want   string
	}{
		{"a kind cast from 0005's vocabulary", policy.Target{Kind: policy.TargetKind("global"), ID: "x"}, `unknown target kind "global"`},
		{"an id that repeats its kind", policy.Target{Kind: policy.AgentKind, ID: "agent:boss"}, `repeats its kind`},
		{"a named kind naming nothing", policy.Target{Kind: policy.LoopKind}, `must name the thing it governs`},
		{"all-agents carrying an id", policy.Target{Kind: policy.AllAgents, ID: "boss"}, `must not name an id`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repo := &fakeRepo{}
			got := refuseRecord(t, policy.NewStore(repo), "rule", c.target, "operator", acceptT1)
			if !strings.Contains(got, c.want) {
				t.Errorf("the refusal does not contain %q: %s", c.want, got)
			}
			if repo.recordCalls != 0 {
				t.Errorf("the bad target reached the repo (%d calls)", repo.recordCalls)
			}
		})
	}
}

func TestRecordAcceptanceGatesThirdPartyStringsAndTheInstant(t *testing.T) {
	// policy id, target id and accepted_by are third-party text reaching an
	// operator's console through every "who knew what since when" listing —
	// the same identifier policy Add runs (SEC-013), at the same choke point,
	// with the same refusals. And the instant is a parameter or the record
	// has no "since when" (D5).
	repo := &fakeRepo{}
	s := policy.NewStore(repo)
	good := targetOf(t, policy.AgentKind, "boss")

	for _, c := range []struct {
		name       string
		policyID   string
		target     policy.Target
		acceptedBy string
		at         time.Time
		mustHas    []string
	}{
		{"a policy id with a newline", "line1\nline2", good, "operator", acceptT1,
			[]string{"policy id", "control character U+000A"}},
		{"an accepted_by with a space", "rule", good, "two words", acceptT1,
			[]string{"accepted by", "character U+0020"}},
		{"an accepted_by of pure zero-widths", "rule", good, "\u200b\u200b", acceptT1,
			[]string{"accepted by", "no visible character"}},
		{"a policy id over the cap", strings.Repeat("a", 65), good, "operator", acceptT1,
			[]string{"policy id", "65 runes", "refused, not truncated"}},
		{"a target id with a newline", "rule", policy.Target{Kind: policy.AgentKind, ID: "bo\nss"}, "operator", acceptT1,
			[]string{"target id", "control character U+000A"}},
		{"no instant at all", "rule", good, "operator", time.Time{},
			[]string{"no instant", "since when"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := refuseRecord(t, s, c.policyID, c.target, c.acceptedBy, c.at)
			for _, want := range c.mustHas {
				if !strings.Contains(got, want) {
					t.Errorf("the refusal does not name %q: %s", want, got)
				}
			}
		})
	}
	if repo.recordCalls != 0 {
		t.Errorf("%d refused records reached the repo", repo.recordCalls)
	}
}

func TestRecordAcceptanceFoldsAStoreFailureIntoARefusal(t *testing.T) {
	// Add's single-return-shape discipline, applied to the record boundary:
	// the machine breaking and the input being wrong both arrive as a *Reject
	// that says the record was NOT made and why (DB19's exception, inherited).
	repo := &fakeRepo{recordErr: errors.New("disk on fire")}
	got := refuseRecord(t, policy.NewStore(repo), "doomed", targetOf(t, policy.AgentKind, "boss"), "operator", acceptT1)
	for _, want := range []string{"was NOT recorded", "disk on fire"} {
		if !strings.Contains(got, want) {
			t.Errorf("the fold lost %q: %s", want, got)
		}
	}
}

// ── the read side: rows re-enter through the parsers or the list refuses ─────

func TestAcceptancesForReReadsRowsThroughTheParsers(t *testing.T) {
	// The SEC-015 argument on the record's read side: a row that escaped the
	// boundary — hand-edited table, row from before it existed — refuses the
	// WHOLE answer with its policy id named. Silently dropping a row is how
	// an audit loses the one acceptance that proved someone knew.
	cases := []struct {
		name    string
		row     policy.AcceptanceRow
		mustHas []string
	}{
		{"no policy id",
			policy.AcceptanceRow{PolicyID: "", TargetKind: "agent", TargetID: "boss", AcceptedBy: "operator", AcceptedAt: acceptT1Text},
			[]string{"no policy id"}},
		{"no accepted_by",
			policy.AcceptanceRow{PolicyID: "tampered", TargetKind: "agent", TargetID: "boss", AcceptedBy: "", AcceptedAt: acceptT1Text},
			[]string{"tampered", "no accepted_by"}},
		{"an accepted_at that is not an instant",
			policy.AcceptanceRow{PolicyID: "tampered", TargetKind: "agent", TargetID: "boss", AcceptedBy: "operator", AcceptedAt: "yesterday-ish"},
			[]string{"tampered", "does not read as an RFC3339Nano instant"}},
		{"an empty accepted_at",
			policy.AcceptanceRow{PolicyID: "tampered", TargetKind: "agent", TargetID: "boss", AcceptedBy: "operator", AcceptedAt: ""},
			[]string{"tampered", "RFC3339Nano"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repo := &fakeRepo{}
			repo.acceptances = []policy.AcceptanceRow{
				{PolicyID: "healthy", TargetKind: "agent", TargetID: "boss", AcceptedBy: "operator", AcceptedAt: acceptT1Text},
				c.row,
			}
			_, rej := policy.NewStore(repo).AcceptancesFor(context.Background(), targetOf(t, policy.AgentKind, "boss"))
			if rej == nil {
				t.Fatalf("AcceptancesFor ACCEPTED the tampered row and its neighbours")
			}
			for _, want := range c.mustHas {
				if !strings.Contains(rej.Reason, want) {
					t.Errorf("the refusal does not name %q: %s", want, rej.Reason)
				}
			}
		})
	}
}

func TestAcceptancesForFoldsAStoreFailureIntoARefusal(t *testing.T) {
	repo := &fakeRepo{listForErr: errors.New("reader pool gone")}
	_, rej := policy.NewStore(repo).AcceptancesFor(context.Background(), targetOf(t, policy.AgentKind, "boss"))
	if rej == nil {
		t.Fatal("the store failure vanished")
	}
	for _, want := range []string{"were NOT read", "reader pool gone"} {
		if !strings.Contains(rej.Reason, want) {
			t.Errorf("the fold lost %q: %s", want, rej.Reason)
		}
	}
}

func TestAcceptancesForValidatesItsTargetBeforeTheRepo(t *testing.T) {
	// No repo call for a target that cannot name anything: the boundary trusts
	// no caller, read or write.
	repo := &fakeRepo{}
	_, rej := policy.NewStore(repo).AcceptancesFor(context.Background(), policy.Target{Kind: policy.TargetKind("loop"), ID: "loop:l"})
	if rej == nil || !strings.Contains(rej.Reason, "repeats its kind") {
		t.Fatalf("the bad target was not refused by name: %+v", rej)
	}
	if repo.listForCalls != 0 {
		t.Errorf("the bad target reached the repo (%d calls)", repo.listForCalls)
	}
}

// ── the grep guard: the evaluator cannot be made to depend on the record ─────

// acceptanceCoupling names the tokens that would couple the evaluator to the
// acceptance record: the word "acceptance" (any case — the gate someone adds
// will be spelled however they spell it) and "Repo" (the seam the records
// arrive through). Pure over text, like every guard in this suite.
func acceptanceCoupling(text string) []string {
	var hits []string
	if strings.Contains(strings.ToLower(text), "acceptance") {
		hits = append(hits, `"acceptance" (any case)`)
	}
	if strings.Contains(text, "Repo") {
		hits = append(hits, `"Repo" (the record seam)`)
	}
	return hits
}

// repoShapedPackageState reports the names of package-level vars in src whose
// declaration mentions the Repo type — the shape a later "just wire the store
// in here" refactor would leave behind. (evaluate.go's registry var is legal:
// a code-time evaluator registry, pinned elsewhere — it names no Repo.)
func repoShapedPackageState(src string) ([]string, error) {
	f, _, err := parseSource(src)
	if err != nil {
		return nil, err
	}
	var offenders []string
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			var mentions string
			ast.Inspect(vs, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok && id.Name == "Repo" {
					var names []string
					for _, nm := range vs.Names {
						names = append(names, nm.Name)
					}
					mentions = strings.Join(names, ", ")
					if mentions == "" {
						mentions = "<unnamed>"
					}
				}
				return true
			})
			if mentions != "" {
				offenders = append(offenders, mentions)
			}
		}
	}
	return offenders, nil
}

func TestEvaluateDoesNotReadPolicyAcceptance(t *testing.T) {
	// The guard the next person to touch record.go cannot talk the evaluator
	// past: acceptance answers "who was told, since when" and NOTHING in the
	// enforcement path reads it (PL10). Asserted by grep over the two files
	// the evaluator lives in — evaluate.go and types.go — not by review:
	// a coupling that is possible is a coupling that will happen.
	for _, file := range []string{"evaluate.go", "types.go"} {
		text := readSource(t, file)
		if len(text) < 100 {
			t.Fatalf("%s reads as %d bytes; this guard would pass almost vacuously", file, len(text))
		}
		if hits := acceptanceCoupling(text); len(hits) > 0 {
			t.Errorf("%s references the acceptance record (%v); the evaluator must not see it — "+
				"an unaccepted policy is still enforced, and a word in these files is the first half "+
				"of the `if` that turns the record into the gate (PL10)", file, hits)
		}
	}

	// And the dependency claim as syntax: Repo is not a dependency of
	// Evaluate — no package-level state in evaluate.go reaches for the seam.
	eval := readSource(t, "evaluate.go")
	offenders, err := repoShapedPackageState(eval)
	if err != nil {
		t.Fatalf("parse evaluate.go: %v", err)
	}
	if len(offenders) != 0 {
		t.Errorf("evaluate.go holds package-level state naming Repo: %v — the evaluator takes its "+
			"policies as a parameter; anything it reads from a repo is a gate waiting to be", offenders)
	}
}

func TestTheAcceptanceCouplingGuardCanFail(t *testing.T) {
	// A guard that cannot fire guards nothing (T2's TestTheLockCanFail). The
	// gate someone would add, spelled three ways: caught. Clean sources:
	// quiet — including this suite's own evaluate.go, whose registry the
	// AST guard must not cry wolf on.
	gated := "func Evaluate(ps []Policy, req Request, at time.Time) Verdict {\n" +
		"\tif !hasAcceptance(req) { // the policy_acceptance table says nobody was told\n" +
		"\t\treturn Verdict{Allowed: true}\n\t}\n\treturn Verdict{}\n}\nvar seam Repo\n"
	hits := acceptanceCoupling(gated)
	if len(hits) != 2 {
		t.Errorf("the coupling guard missed the gate: %v", hits)
	}
	if !strings.Contains(strings.ToLower("ACcePtAnCe"), "acceptance") {
		t.Error("the case-insensitive claim is false")
	}
	if len(acceptanceCoupling("func Evaluate(ps []Policy) Verdict { return Verdict{} }")) != 0 {
		t.Error("the guard cried wolf on a clean source")
	}

	state, err := repoShapedPackageState("package policy\n\nvar registry = map[PolicyType]struct{}{}\nvar seam Repo\n")
	if err != nil {
		t.Fatalf("the state guard went vacuous: %v", err)
	}
	if len(state) != 1 {
		t.Errorf("the state guard missed the repo var (or caught the registry): %v", state)
	}
	clean, err := repoShapedPackageState(readSource(t, "evaluate.go"))
	if err != nil {
		t.Fatalf("parse evaluate.go: %v", err)
	}
	if len(clean) != 0 {
		t.Errorf("the state guard cried wolf on the real evaluate.go: %v", clean)
	}
}

// ── the file contract: record.go is the serialization point, declared ────────

func TestRecordGoHeaderDeclaresTheSerializationContract(t *testing.T) {
	// The header is not decoration: T8 and T10 will append here in this exact
	// order, never concurrently, additive only — and the "one `if` away"
	// argument has to be where the person adding that `if` reads it. The
	// phrases are pinned so a rewrite cannot quietly drop the contract.
	text := readSource(t, "record.go")
	lower := strings.ToLower(text)
	for _, want := range []string{
		"single serialization point",
		"acceptance (t7",
		"events (t8",
		"disagreement record (t10",
		"never touch this file concurrently",
		"additive only",
		"one `if` away from being a security hole",
		"an unaccepted policy is still enforced",
	} {
		if !strings.Contains(lower, want) {
			t.Errorf("record.go's header no longer states %q; the serialization contract lives in it", want)
		}
	}

	// The order, too: acceptance before events before disagreement, or the
	// file has been re-organised away from the sequence the tasks depend on.
	iA, iE, iD := strings.Index(lower, "acceptance (t7"), strings.Index(lower, "events (t8"), strings.Index(lower, "disagreement record (t10")
	if !(iA >= 0 && iA < iE && iE < iD) {
		t.Errorf("the header no longer names the tasks in their order: acceptance→events→disagreement (got %d, %d, %d)", iA, iE, iD)
	}

	// And the import story this file has to keep true (I2/D5): the record
	// boundary reaches the table only through the Repo seam.
	for _, banned := range []string{`"github.com/cjlapao/aeon/internal/store"`, `"database/sql"`} {
		if strings.Contains(text, banned) {
			t.Errorf("record.go imports %s; the SQL lives in internal/store and the seam is Repo (I2)", banned)
		}
	}
}

// ── the RepoFuncs bridge, acceptance half (what the wiring fills in) ─────────

func TestRepoFuncsBridgeRecordsAndListsAcceptances(t *testing.T) {
	var recorded []recordedAcceptance
	bridge := policy.RepoFuncs{
		AcceptanceRecord: func(_ context.Context, policyID, targetKind, targetID, acceptedBy string, at time.Time) error {
			recorded = append(recorded, recordedAcceptance{policyID, targetKind, targetID, acceptedBy, at})
			return nil
		},
		AcceptanceList: func(_ context.Context, targetKind, targetID string) ([]policy.AcceptanceRow, error) {
			return []policy.AcceptanceRow{
				{PolicyID: "via-bridge", TargetKind: targetKind, TargetID: targetID, AcceptedBy: "operator", AcceptedAt: acceptT1Text},
			}, nil
		},
	}
	s := policy.NewStore(bridge)
	mustRecord(t, s, "via-bridge", targetOf(t, policy.AgentKind, "boss"), "operator", acceptT1)
	if len(recorded) != 1 || recorded[0].policyID != "via-bridge" || !recorded[0].at.Equal(acceptT1) {
		t.Fatalf("the bridge did not forward the record: %+v", recorded)
	}
	acc := mustAcceptances(t, s, targetOf(t, policy.AgentKind, "boss"))
	if len(acc) != 1 || acc[0].PolicyID != "via-bridge" {
		t.Errorf("the bridge did not forward the read: %+v", acc)
	}
}

func TestRepoFuncsBridgeRefusesMissingAcceptanceHooks(t *testing.T) {
	// A half-wired bridge is a defect that names itself — the same rule Add
	// obeys, extended with the seam, not a nil dereference mid-record.
	empty := policy.NewStore(policy.RepoFuncs{})
	rej := empty.RecordAcceptance(context.Background(), "n", targetOf(t, policy.AgentKind, "boss"), "operator", acceptT1)
	if rej == nil || !strings.Contains(rej.Reason, "no AcceptanceRecord function") {
		t.Errorf("the missing AcceptanceRecord hook did not name itself: %+v", rej)
	}
	if _, rej := empty.AcceptancesFor(context.Background(), targetOf(t, policy.AgentKind, "boss")); rej == nil ||
		!strings.Contains(rej.Reason, "no AcceptanceList function") {
		t.Errorf("the missing AcceptanceList hook did not name itself: %+v", rej)
	}
}
