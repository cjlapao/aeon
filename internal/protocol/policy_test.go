package protocol

// The policy reads (T13): the four methods, held to their payload shapes.
//
// The store beneath them is the REAL boundary (policy.NewStore) over an
// in-memory Repo — the reads run through List's/ReportEvents' fail-closed
// decode, not a hand-rolled double, because what these tests must prove is
// that the socket prints what the boundary answers. The write hooks carry a
// spy: policy.why is asserted at ZERO writes (D9), and the reads at zero
// trims (PL11 — housekeeping fires on daemon start and on CLI demand, never
// on a read).

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/policy"

	// SEC-059's socket parity test asserts the PRODUCTION wiring: the real
	// binary always links internal/tools through the daemon, and a test file
	// that forgot this import would silently exercise the identity default
	// while believing it pinned the fix. The unwired state is covered
	// separately by TestToolAccessMatchWithoutCanonicalizerStaysExact in
	// internal/policy — this file pins the wired one.
	_ "github.com/cjlapao/aeon/internal/tools"
)

// --- the in-memory boundary ---------------------------------------------------

// policySpy counts every call into the WRITE half of the Repo. The seeds use
// the boundary's own writers (Add/RecordAcceptance/RecordRefusal) and then
// reset the counters, so anything a handler writes shows up as a non-zero.
type policySpy struct {
	adds, removes, accepts, inserts, trims int
}

func (s *policySpy) reset() { *s = policySpy{} }

type policyFixture struct {
	rows        map[string]policy.Row
	acceptances []policy.AcceptanceRow
	events      []policy.EventRow
	nextEventID int64
	spy         *policySpy
}

// newPolicyStore builds the real boundary over the map-backed Repo. The
// subject is split the way recordEvent split it on the way in, rows come
// back newest-first, and the stats answer for the subject the caller asked
// about — "" over the whole table, a `kind:id` over that subject's rows
// (F-13: depth is a property of the listing that was asked for).
func newPolicyStore() (*policy.Store, *policyFixture) {
	fx := &policyFixture{rows: map[string]policy.Row{}, spy: &policySpy{}, nextEventID: 1}
	repo := policy.RepoFuncs{
		Add: func(_ context.Context, r policy.Row) error {
			fx.spy.adds++
			fx.rows[r.ID] = r
			return nil
		},
		Remove: func(_ context.Context, id string) (bool, error) {
			fx.spy.removes++
			_, existed := fx.rows[id]
			delete(fx.rows, id)
			return existed, nil
		},
		ListAll: func(_ context.Context) ([]policy.Row, error) {
			out := make([]policy.Row, 0, len(fx.rows))
			for _, r := range fx.rows {
				out = append(out, r)
			}
			sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
			return out, nil
		},
		AcceptanceRecord: func(_ context.Context, policyID, targetKind, targetID, acceptedBy string, at time.Time) error {
			fx.spy.accepts++
			fx.acceptances = append(fx.acceptances, policy.AcceptanceRow{
				PolicyID: policyID, TargetKind: targetKind, TargetID: targetID,
				AcceptedBy: acceptedBy, AcceptedAt: at.Format(time.RFC3339Nano),
			})
			return nil
		},
		AcceptanceList: func(_ context.Context, targetKind, targetID string) ([]policy.AcceptanceRow, error) {
			var out []policy.AcceptanceRow
			for _, a := range fx.acceptances {
				if a.TargetKind == targetKind && a.TargetID == targetID {
					out = append(out, a)
				}
			}
			return out, nil
		},
		EventInsert: func(_ context.Context, kind, subject, policyID, detail string, at time.Time) error {
			fx.spy.inserts++
			k, id, _ := strings.Cut(subject, ":")
			fx.events = append(fx.events, policy.EventRow{
				ID: fx.nextEventID, PolicyID: policyID, SubjectKind: k, SubjectID: id,
				Outcome: kind, Detail: detail, At: at.Format(time.RFC3339Nano),
			})
			fx.nextEventID++
			return nil
		},
		EventQuery: func(_ context.Context, subject string, limit int) ([]policy.EventRow, error) {
			var out []policy.EventRow
			for _, e := range fx.events {
				if subject != "" && e.SubjectKind+":"+e.SubjectID != subject {
					continue
				}
				out = append(out, e)
			}
			sort.Slice(out, func(i, j int) bool {
				ti, _ := time.Parse(time.RFC3339Nano, out[i].At)
				tj, _ := time.Parse(time.RFC3339Nano, out[j].At)
				return ti.After(tj)
			})
			if len(out) > limit {
				out = out[:limit]
			}
			return out, nil
		},
		EventTrim: func(_ context.Context, olderThan time.Time) (int, error) {
			fx.spy.trims++
			kept := fx.events[:0]
			var dropped int
			for _, e := range fx.events {
				at, err := time.Parse(time.RFC3339Nano, e.At)
				if err == nil && at.Before(olderThan) {
					dropped++
					continue
				}
				kept = append(kept, e)
			}
			fx.events = kept
			return dropped, nil
		},
		EventStats: func(_ context.Context, subject string) (int, time.Time, error) {
			// F-13: the depth and oldest answer the subject the listing asked
			// for — the same split the EventQuery hook filters on.
			var (
				depth         int
				oldest        time.Time
				wantK, wantID string
			)
			if subject != "" {
				wantK, wantID, _ = strings.Cut(subject, ":")
			}
			for _, e := range fx.events {
				if subject != "" && (e.SubjectKind != wantK || e.SubjectID != wantID) {
					continue
				}
				depth++
				at, err := time.Parse(time.RFC3339Nano, e.At)
				if err != nil {
					continue
				}
				if oldest.IsZero() || at.Before(oldest) {
					oldest = at
				}
			}
			return depth, oldest, nil
		},
	}
	return policy.NewStore(repo), fx
}

// policyTable wires the four reads over a fresh boundary.
func policyTable(t *testing.T) (*Table, *policy.Store, *policyFixture) {
	t.Helper()
	tbl := newTestTable(t)
	store, fx := newPolicyStore()
	tbl.PolicyStore = store
	initialized(t, tbl, "c1", PROTOCOL_VERSION)
	return tbl, store, fx
}

// --- seeding through the boundary's OWN writers -------------------------------

func addPolicyAt(t *testing.T, s *policy.Store, id string, typ policy.PolicyType, target policy.Target, action policy.Action, spec, notAfter string) {
	t.Helper()
	p := policy.Policy{
		ID: id, Type: typ, Target: target, Action: action,
		Spec: json.RawMessage(spec), Source: policy.Operator,
	}
	if notAfter != "" {
		at, err := time.Parse(time.RFC3339Nano, notAfter)
		if err != nil {
			t.Fatalf("seed notAfter %q: %v", notAfter, err)
		}
		p.NotAfter = &at
	}
	if rej := s.Add(context.Background(), p, time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)); rej != nil {
		t.Fatalf("seeding policy %q: %s", id, rej.Reason)
	}
}

func dispatchPolicy[T any](t *testing.T, tbl *Table, m MethodName, params any) T {
	t.Helper()
	var raw json.RawMessage
	if params != nil {
		raw = mustJSON(t, params)
	}
	res, err := tbl.Dispatch(context.Background(), "c1", m, raw)
	if err != nil {
		t.Fatalf("%s: %v", m, err)
	}
	out, ok := res.(T)
	if !ok {
		t.Fatalf("%s returned %T, want %T", m, res, out)
	}
	return out
}

// mustAeonCode asserts the dispatch failed with exactly this Aeon code.
func mustAeonCode(t *testing.T, tbl *Table, m MethodName, params any, want int) *Error {
	t.Helper()
	var raw json.RawMessage
	if params != nil {
		raw = mustJSON(t, params)
	}
	_, err := tbl.Dispatch(context.Background(), "c1", m, raw)
	if err == nil {
		t.Fatalf("%s: want error code %d, got success", m, want)
	}
	var aeon *Error
	if !errors.As(err, &aeon) {
		t.Fatalf("%s: want *protocol.Error %d, got %T (%v)", m, want, err, err)
	}
	if aeon.Code != want {
		t.Fatalf("%s: error code %d, want %d (what: %s)", m, aeon.Code, want, aeon.Data.What)
	}
	return aeon
}

var bossTarget = policy.Target{Kind: policy.AgentKind, ID: "boss"}
var mainTarget = policy.Target{Kind: policy.AgentKind, ID: "main"}

const (
	allowSpecTmpl = `{"toolName":%q,"state":"allow"}`
	denySpecTmpl  = `{"toolName":%q,"state":"deny"}`
	costSpec      = `{"token":"gpt-tokens","max":1000}`
)

// --- the six -------------------------------------------------------------------

// D4. policy.list with nothing returns the default-deny statement: the empty
// set is a STATED fact, and the sentence is the one the CLI prints, byte for
// byte. A populated list answers with rows and no statement.
func TestPolicyListWithNothingReturnsTheDefaultDenyStatement(t *testing.T) {
	tbl, store, fx := policyTable(t)

	res := dispatchPolicy[PolicyListResult](t, tbl, MethodPolicyList, nil)
	if res.Policies == nil {
		t.Fatal("policies is null; `[]` says \"read and empty\", a missing field says \"cannot answer\"")
	}
	if len(res.Policies) != 0 {
		t.Fatalf("policies = %v, want empty", res.Policies)
	}
	if res.Statement == nil {
		t.Fatal("the empty list carries no statement (D4): a guard whose empty state reads as \"fine\" is the guard that fails open in the reader's head")
	}
	// Byte-for-byte pin — the literal IS the contract, DEMO.md §1 quotes it.
	if want := "no policies. default is DENY (PL5) — nothing is granted until something grants it."; *res.Statement != want {
		t.Errorf("statement = %q, want %q", *res.Statement, want)
	}
	if *res.Statement != PolicyListEmptyStatement {
		t.Errorf("the handler did not print the surface's own const")
	}

	addPolicyAt(t, store, "grant-read", policy.ToolAccess, mainTarget, policy.Allow, `{"toolName":"read","state":"allow"}`, "")
	fx.spy.reset()

	res = dispatchPolicy[PolicyListResult](t, tbl, MethodPolicyList, json.RawMessage(`{}`))
	if res.Statement != nil {
		t.Errorf("a populated list carries a statement: %q", *res.Statement)
	}
	if len(res.Policies) != 1 {
		t.Fatalf("policies = %d rows, want 1", len(res.Policies))
	}
	row := res.Policies[0]
	if row.ID != "grant-read" || row.Type != "tool_access" || row.Target != "agent:main" ||
		row.Action != "allow" || row.Source != "operator" || row.NotAfter != nil {
		t.Errorf("row = %+v, want the stored rule rendered (no expiry as null, not as a gap)", row)
	}
	// The read path never writes.
	if *fx.spy != (policySpy{}) {
		t.Errorf("listing wrote to the boundary: %+v", *fx.spy)
	}

	// The storeless shape answers the framework's store-absence code, never a
	// panic.
	bare := newTestTable(t)
	initialized(t, bare, "c1", PROTOCOL_VERSION)
	mustAeonCode(t, bare, MethodPolicyList, nil, ErrStoreUnavailable.Code)
}

// D9. policy.why mutates NOTHING (spy at zero writes) and accepts `at`: the
// same question on either side of an expiry answers differently, and every
// sentence is Verdict.Sentence() verbatim. The unimplemented answer is
// carried as the FIELD the app branches on (SEC-033), never left to be
// inferred from the shared "refused: " prefix.
func TestPolicyWhyMutatesNothingAndAcceptsTheAskedInstant(t *testing.T) {
	tbl, store, fx := policyTable(t)

	const expiry = "2026-11-01T00:00:00Z"
	addPolicyAt(t, store, "grant-read", policy.ToolAccess, mainTarget, policy.Allow,
		`{"toolName":"read","state":"allow"}`, expiry)
	addPolicyAt(t, store, "per-token-budget", policy.CostCap, mainTarget, policy.Allow, costSpec, "")
	fx.spy.reset()

	// Before the expiry: the grant answers, the sentence whole.
	before := dispatchPolicy[PolicyWhyResult](t, tbl, MethodPolicyWhy, PolicyWhyParams{
		Type: "tool_access", Agent: "main", Tool: "read", At: "2026-10-15T00:00:00Z",
	})
	if !before.Allowed {
		t.Errorf("before the expiry the grant must allow; sentence: %s", before.Sentence)
	}
	if want := `allowed: agent:main · tool_access "grant-read" · target agent:main · source operator · expires 2026-11-01T00:00:00Z`; before.Sentence != want {
		t.Errorf("sentence (verbatim duty) =\n%q\nwant\n%q", before.Sentence, want)
	}

	// After the expiry: the same question, a different answer — `at` is what
	// the boundary changed, nothing else. A policy that stopped existing does
	// not linger as a phantom grant, and the set left over is the default
	// deny.
	after := dispatchPolicy[PolicyWhyResult](t, tbl, MethodPolicyWhy, PolicyWhyParams{
		Type: "tool_access", Agent: "main", Tool: "read", At: "2026-11-02T00:00:00Z",
	})
	if after.Allowed {
		t.Error("an expired grant still allows — the pair of instants did not change the sentence")
	}
	if want := `refused: agent:main · no allow matched · default deny (PL5)`; after.Sentence != want {
		t.Errorf("sentence = %q, want %q", after.Sentence, want)
	}
	if before.Sentence == after.Sentence {
		t.Error("both sides of the expiry render the same sentence; `at` was ignored")
	}

	// The third answer (D10), carried as a FIELD: cost_cap has a registered
	// stub that refuses to decide. "nobody can answer yet" must be
	// distinguishable without parsing prose for "refused: ".
	cannot := dispatchPolicy[PolicyWhyResult](t, tbl, MethodPolicyWhy, PolicyWhyParams{
		Type: "cost_cap", Agent: "main", Cost: "gpt-tokens",
	})
	if cannot.Allowed || !cannot.Unimplemented {
		t.Errorf("cost_cap = %+v, want allowed:false unimplemented:true", cannot)
	}
	if want := `refused: agent:main · cost_cap has no evaluator yet · refusing rather than permitting (D10)`; cannot.Sentence != want {
		t.Errorf("sentence = %q, want %q", cannot.Sentence, want)
	}

	// D9: the explanation wrote NOTHING. Not an audit row (a refusal by
	// `why` is not a refusal that happened), not an acceptance, not a trim.
	if *fx.spy != (policySpy{}) {
		t.Errorf("policy.why wrote to the boundary: %+v", *fx.spy)
	}

	// The handler validates its OWN params (SEC-T6-005), by delegation.
	if e := mustAeonCode(t, tbl, MethodPolicyWhy, PolicyWhyParams{
		Type: "tool_access", Agent: "main", At: "yesterday",
	}, ErrPolicyReadRefused.Code); !strings.Contains(e.Data.What, "yesterday") {
		t.Errorf("the refused clock must be named in the refusal, got %q", e.Data.What)
	}
	if e := mustAeonCode(t, tbl, MethodPolicyWhy, PolicyWhyParams{Type: "everything", Agent: "main"},
		ErrPolicyReadRefused.Code); !strings.Contains(e.Data.What, "everything") {
		t.Errorf("a non-member type is refused BY NAME (the CLI's rule), got %q", e.Data.What)
	}
	// SEC-046: the identity strings reach the sentence the app renders, so
	// they pass the boundary's own alphabet gate — the zero-width name that
	// "looks blank to a human" is refused at the socket, not echoed.
	mustAeonCode(t, tbl, MethodPolicyWhy, PolicyWhyParams{
		Type: "tool_access", Agent: "mai\u200bn", Tool: "read",
	}, ErrPolicyReadRefused.Code)
	mustAeonCode(t, tbl, MethodPolicyWhy, nil, ErrPolicyReadRefused.Code) // no type at all
}

// D14. policy.inForce returns all three states un-collapsed, each `render`
// InForceAnswer.Render() verbatim — three distinct sentences. The two
// in-force lines share their head (that sharing IS the claim); the third
// shares nothing with either, and this test is where "the app could not
// tell them apart" becomes a build failure.
func TestPolicyInForceReturnsAllThreeStatesUncollapsed(t *testing.T) {
	tbl, store, fx := policyTable(t)

	addPolicyAt(t, store, "grant-read", policy.ToolAccess, bossTarget, policy.Allow, `{"toolName":"read","state":"allow"}`, "")
	addPolicyAt(t, store, "no-bash", policy.ToolAccess, bossTarget, policy.Deny, `{"toolName":"bash","state":"deny"}`, "")
	addPolicyAt(t, store, "per-token-budget", policy.CostCap, bossTarget, policy.Allow, costSpec, "")
	if rej := store.RecordAcceptance(context.Background(), "grant-read", bossTarget, "operator",
		time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)); rej != nil {
		t.Fatalf("seeding the acceptance: %s", rej.Reason)
	}
	fx.spy.reset()

	res := dispatchPolicy[PolicyInForceResult](t, tbl, MethodPolicyInForce, PolicyInForceParams{Agent: "boss"})
	if len(res.Answers) != 3 {
		t.Fatalf("answers = %d, want 3 (one per governing policy): %+v", len(res.Answers), res.Answers)
	}
	by := map[string]PolicyInForceRow{}
	for _, a := range res.Answers {
		by[a.PolicyID] = a
	}
	wants := map[string]PolicyInForceRow{
		// The instant printed is the instant the record carries, never
		// now — a line printed today keeps matching its row tomorrow.
		"grant-read":       {State: "enforced_acknowledged", Render: "in force · acknowledged by agent:boss since 2026-10-01T09:00:00Z"},
		"no-bash":          {State: "enforced_unacknowledged", Render: "in force · unacknowledged — still enforced (PL10)"},
		"per-token-budget": {State: "could_not_determine", Render: "could not determine · no evaluator for cost_cap yet (D10)"},
	}
	seen := map[string]bool{}
	for id, want := range wants {
		got, ok := by[id]
		if !ok {
			t.Fatalf("no answer for %q; got %+v", id, res.Answers)
		}
		if got.State != want.State || got.Render != want.Render {
			t.Errorf("%s = {%s, %q}, want {%s, %q} — renders are VERBATIM duties",
				id, got.State, got.Render, want.State, want.Render)
		}
		seen[got.Render] = true
	}
	if len(seen) != 3 {
		t.Errorf("the three states collapsed to %d distinct renders (D14): %v", len(seen), seen)
	}
	if *fx.spy != (policySpy{}) {
		t.Errorf("in-force answered itself by writing: %+v", *fx.spy)
	}

	// Params hygiene: one party per answer, a name that is a name.
	mustAeonCode(t, tbl, MethodPolicyInForce, PolicyInForceParams{Agent: "boss", Loop: "l1"}, ErrPolicyReadRefused.Code)
	mustAeonCode(t, tbl, MethodPolicyInForce, PolicyInForceParams{}, ErrPolicyReadRefused.Code)
	// "agent:boss" as an id repeats its kind — ParseTarget's rule, reached
	// by delegation (SEC-046: one parser, never a copy).
	mustAeonCode(t, tbl, MethodPolicyInForce, PolicyInForceParams{Agent: "agent:boss"}, ErrPolicyReadRefused.Code)
}

// D15. policy.events returns the rows plus {depth, cap, oldest} with
// capDays 90 — the cap travels on the answer so a silently-trimming log
// cannot exist — and the read fires no trim (PL11: housekeeping belongs to
// daemon start and CLI demand, never to a read).
func TestPolicyEventsCarriesRowsAndTheVisibilityTriple(t *testing.T) {
	tbl, store, fx := policyTable(t)

	noBash := &policy.Policy{ID: "no-bash", Type: policy.ToolAccess, Target: bossTarget,
		Action: policy.Deny, Source: policy.Operator}
	v1 := policy.Verdict{Subject: "agent:boss", Type: policy.ToolAccess, By: noBash}
	v2 := policy.Verdict{Subject: "loop:s10", Type: policy.ToolAccess, DefaultDeny: true}
	t1 := time.Date(2026, 10, 18, 14, 5, 12, 500_000_000, time.UTC)
	t0 := time.Date(2026, 10, 17, 9, 31, 0, 0, time.UTC)
	for _, seed := range []struct {
		subject string
		v       policy.Verdict
		at      time.Time
	}{{"agent:boss", v1, t1}, {"loop:s10", v2, t0}} {
		if rej := store.RecordRefusal(context.Background(), seed.subject, seed.v, seed.at); rej != nil {
			t.Fatalf("seeding the refusal: %s", rej.Reason)
		}
	}
	fx.spy.reset()

	res := dispatchPolicy[PolicyEventsResult](t, tbl, MethodPolicyEvents, nil)
	if len(res.Rows) != 2 {
		t.Fatalf("rows = %d, want 2: %+v", len(res.Rows), res.Rows)
	}
	// Newest first — the way a log is browsed.
	if res.Rows[0].Subject != "agent:boss" || res.Rows[1].Subject != "loop:s10" {
		t.Errorf("rows are not newest-first: %+v", res.Rows)
	}
	// Rows are byte-for-byte what decodeEvent re-verified, and the detail is
	// the sentence the recorder stored — the one formatter, verbatim.
	if res.Rows[0].Kind != "refusal" || res.Rows[0].PolicyID != "no-bash" {
		t.Errorf("row0 = %+v", res.Rows[0])
	}
	if want := v1.Sentence(); res.Rows[0].Detail != want {
		t.Errorf("detail = %q, want the verdict sentence verbatim %q", res.Rows[0].Detail, want)
	}
	if want := v2.Sentence(); res.Rows[1].Detail != want {
		t.Errorf("detail = %q, want %q", res.Rows[1].Detail, want)
	}
	if res.Rows[1].PolicyID != "" {
		t.Errorf("the default deny names no rule; policyID = %q, want \"\" (the stated empty)", res.Rows[1].PolicyID)
	}
	if at, err := time.Parse(time.RFC3339Nano, res.Rows[0].At); err != nil || !at.Equal(t1) {
		t.Errorf("at = %q, does not read back as %v", res.Rows[0].At, t1)
	}
	if res.Depth != 2 {
		t.Errorf("depth = %d, want 2 — the WHOLE table, not the page", res.Depth)
	}
	if res.CapDays != 90 {
		t.Errorf("capDays = %d, want 90 (D15: the cap is a visible number, not a WHERE clause nobody reads)", res.CapDays)
	}
	if res.Oldest == nil || *res.Oldest != "2026-10-17T09:31:00Z" {
		t.Errorf("oldest = %v, want the oldest retained instant", res.Oldest)
	}

	// F-13: a subject filter narrows the rows AND the footer together — the
	// depth and oldest are read for the same subject the listing is about, so
	// a filtered answer can never print another subject's count. (Before T22
	// this asserted depth 2 with 1 row: the footer was the whole table's.)
	filtered := dispatchPolicy[PolicyEventsResult](t, tbl, MethodPolicyEvents, PolicyEventsParams{Subject: "agent:boss"})
	if len(filtered.Rows) != 1 || filtered.Depth != 1 {
		t.Errorf("filtered = %d rows / depth %d, want 1 row / depth 1 — the footer is the listing's (F-13)",
			len(filtered.Rows), filtered.Depth)
	}
	if filtered.Oldest == nil || *filtered.Oldest != "2026-10-18T14:05:12Z" {
		got := "<nil>"
		if filtered.Oldest != nil {
			got = *filtered.Oldest
		}
		t.Errorf("filtered oldest = %q, want agent:boss's own oldest instant (2026-10-18T14:05:12Z, the "+
			"handler's RFC3339 rendering), not loop:s10's", got)
	}
	paged := dispatchPolicy[PolicyEventsResult](t, tbl, MethodPolicyEvents, PolicyEventsParams{Limit: 1})
	if len(paged.Rows) != 1 || paged.Depth != 2 {
		t.Errorf("limit 1 = %d rows / depth %d, want 1 row / depth 2 — an unfiltered listing is still the "+
			"whole log, and the page size is not a filter", len(paged.Rows), paged.Depth)
	}

	// A subject that was never refused answers its OWN emptiness: 0 rows,
	// depth 0, oldest null — not the neighbouring subject's depth 1 (F-14's
	// filtered-empty lie).
	absent := dispatchPolicy[PolicyEventsResult](t, tbl, MethodPolicyEvents, PolicyEventsParams{Subject: "agent:nobody"})
	if len(absent.Rows) != 0 || absent.Depth != 0 || absent.Oldest != nil {
		t.Errorf("a never-refused subject answered rows=%d depth=%d oldest=%v; want 0/0/null",
			len(absent.Rows), absent.Depth, absent.Oldest)
	}

	// The empty log states its emptiness (oldest null, depth 0) rather than
	// leaving a hole.
	bareTbl, _, _ := policyTable(t)
	empty := dispatchPolicy[PolicyEventsResult](t, bareTbl, MethodPolicyEvents, nil)
	if empty.Rows == nil || len(empty.Rows) != 0 || empty.Depth != 0 || empty.Oldest != nil {
		t.Errorf("empty log = %+v, want [] / 0 / null", empty)
	}

	// A caller-supplied subject passes parseSubject — the SAME parse the
	// write side used to record it (SEC-046 holds by delegation).
	if e := mustAeonCode(t, tbl, MethodPolicyEvents, PolicyEventsParams{Subject: "whoever"},
		ErrPolicyReadRefused.Code); !strings.Contains(e.Data.What, "whoever") {
		t.Errorf("the refused subject must be named, got %q", e.Data.What)
	}

	// A read never trims and never writes (PL11).
	if *fx.spy != (policySpy{}) {
		t.Errorf("policy.events wrote to the boundary: %+v", *fx.spy)
	}
}

// R4c. NO policy authoring method is registered. The live table is enumerated
// (the truth of what can be dispatched), and the package's own source is
// grepped for the write verbs — the ruling is mechanical, not remembered.
// The needles are built by concatenation so this file can grep for them
// without containing one.
func TestNoPolicyAuthoringMethodIsRegistered(t *testing.T) {
	// S45 T4/T6 (PM1), supersession by the T6 step-4 pattern (sibling
	// functions, never edits): PM1 made add/remove/seedCorpus the socket's
	// HUMAN door, so this test's forbidden-scan (policy.add/policy.remove
	// must NOT be registered) and its source scan (neither verb may be
	// NAMED in this package) are now false by construction — the table
	// MUST name them and the dispatch MUST live here. The body is not
	// edited; the sibling `policy_authoring_guards_test.go`
	// (TestPolicyAuthoringRegistrationIsConditional) re-runs the whole
	// claim with the amended truth — the authoring names confined to the
	// sanctioned files, every read still registered and read-only, no
	// constant beyond reads + the three authoring tails — so nothing
	// weakened. Pre-amendment body pinned in T4's Notes (sha256
	// 59e5135a4eefd1e3ca76d7f73500bfa7d68c347099823cd9dbb930d23ceb0d13 of
	// `awk '/func TestNoPolicyAuthoringMethodIsRegistered/,/^}/'`).
	// T6 (PM10) owns the final positive/negative restatement and consolidates.
	t.Skip("superseded by S45 T4/T6 PM1: policy.add/remove/seedCorpus are the human door (conditional registration) — sibling TestPolicyAuthoringRegistrationIsConditional re-runs the whole claim with the amended truth")

	tbl := newTestTable(t)

	const group = "policy." // never one literal in this file
	reads := map[string]bool{}
	for _, name := range tbl.Methods() {
		if !strings.HasPrefix(string(name), group) {
			continue
		}
		reads[string(name)] = true
	}
	want := map[string]bool{
		group + "list": true, group + "why": true, group + "inForce": true, group + "events": true,
		// S45 T2 (PM5/PM6): the two DETAIL reads join the window — whole
		// row and vocabulary. Still reads, zero writes; the write verbs
		// below stay forbidden forever (R4c/PL15; T4/T5's doors are the
		// operator's human door, never this table).
		group + "get": true, group + "vocabulary": true,
		// S45 T3 (PM8): the authoring AUDIT read joins the window — the
		// append-only policy_change table, read-only through a reads-only
		// seam (the interface has no writer; the trigger has no Go twin).
		// Still a read, zero writes; the write doors stay T4/T5's, and the
		// write verbs below stay forbidden forever (R4c/PL15).
		group + "changes": true,
	}
	if len(reads) != len(want) {
		t.Fatalf("registered policy.* methods = %v, want exactly the four reads", sortedKeys(reads))
	}
	for name := range want {
		if !reads[name] {
			t.Fatalf("%q is not registered; the four reads are the app's window (R4)", name)
		}
	}
	// The write verbs, named without ever appearing.
	for _, forbidden := range []string{group + "add", group + "remove"} {
		if reads[forbidden] {
			t.Fatalf("%q is registered over the socket — authoring is CLI-only and never arrives here (R4c/PL15)", forbidden)
		}
	}

	// The source itself: neither write verb appears anywhere in this
	// package, and no Method-policy constant exists beyond the four.
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	constants := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		raw, err := os.ReadFile(e.Name())
		if err != nil {
			t.Fatal(err)
		}
		src := string(raw)
		for _, forbidden := range []string{`"` + group + "add\"", `"` + group + "remove\""} {
			if strings.Contains(src, forbidden) {
				t.Errorf("%s contains %q — an authoring verb is named in the socket package (R4c)", e.Name(), forbidden)
			}
		}
		for _, rest := range strings.Split(src, "Method"+"Policy") {
			end := 0
			for end < len(rest) && (rest[end] == '_' ||
				rest[end] >= 'A' && rest[end] <= 'Z' ||
				rest[end] >= 'a' && rest[end] <= 'z' ||
				rest[end] >= '0' && rest[end] <= '9') {
				end++
			}
			tail := rest[:end]
			if tail == "" || !isExportedTail(tail) {
				continue
			}
			constants[tail] = true
		}
	}
	for name := range constants {
		// S45 T2 data entries: Get and Vocabulary are the detail reads'
		// constant tails (PM5/PM6); S45 T3's data entry: Changes is the
		// audit read's tail (PM8). Every other tail beyond the window is
		// still the R4c violation this pin exists to catch.
		if !want[group+name] && name != "List" && name != "Why" && name != "InForce" && name != "Events" &&
			name != "Get" && name != "Vocabulary" && name != "Changes" {
			t.Errorf("a MethodPolicy%s constant exists beyond the four reads (R4c)", name)
		}
	}
	// S45 T2 (PM5/PM6): Get and Vocabulary ride along — the detail reads'
	// constants are pinned present too, never merely tolerated. S45 T3
	// (PM8): Changes joins the pinned-present set, the audit read's own
	// constant.
	for _, n := range []string{"List", "Why", "InForce", "Events", "Get", "Vocabulary", "Changes"} {
		if !constants[n] {
			t.Errorf("the read constant MethodPolicy%s is missing", n)
		}
	}
}

func isExportedTail(s string) bool { return s[0] >= 'A' && s[0] <= 'Z' }

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// D16 / PL15 / legal constraint 3: no policy method reaches an HTTP handler.
// TestAPIIsPinnedOff (internal/bootstrap) is unchanged and stays green in the
// same command; what this test adds is the mount-side grep: the bootstrap
// package — which owns the allow-list and the pins — never names a policy
// method, so there is no HTTP (or any other) mount that could dispatch one,
// and the reads reach only the local socket the daemon registers generically
// from the table.
func TestNoPolicyMethodReachesTheHTTPMount(t *testing.T) {
	const quoted = `"policy.` // a wire name as it would appear in a mount table

	entries, err := os.ReadDir("../bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		raw, err := os.ReadFile("../bootstrap/" + e.Name())
		if err != nil {
			t.Fatal(err)
		}
		if i := strings.Index(string(raw), quoted); i >= 0 {
			t.Errorf("bootstrap/%s names a policy method (%s) — the reads mount on the local socket from the table, and bootstrap must be method-blind (D16/PL15)",
				e.Name(), firstQuoted(string(raw), quoted))
		}
	}

	// And the four ARE product surface: public (Debug:false), which S19's
	// narrow surface mounts — reads the app depends on must not be excluded
	// by accident.
	tbl := newTestTable(t)
	public := map[MethodName]bool{}
	for _, n := range tbl.PublicMethods() {
		public[n] = true
	}
	for _, n := range []MethodName{MethodPolicyList, MethodPolicyWhy, MethodPolicyInForce, MethodPolicyEvents} {
		if !public[n] {
			t.Errorf("%s is not on the public table — a read the app depends on is excluded from a surface (R4)", n)
		}
	}
}

func firstQuoted(src, needle string) string {
	i := strings.Index(src, needle)
	end := strings.Index(src[i+len(needle):], `"`)
	return src[i : i+len(needle)+end+1]
}

// TestPolicyWhySocketParityAcrossWriteSpellings is the socket's half of the
// SEC-059 close. handlePolicyWhy passes the caller's "tool" through RAW
// (unlike the gate, which canonicalizes in gateQuestion), so the two
// spellings of the write capability reach the engine as different strings —
// and must still produce the SAME answer, because the canonicalization lives
// in the one Match every surface shares, not in per-surface edits. The deny
// row is authored under the alias through Store.Add (the real boundary): the
// pre-fix engine let a write past it while this very handler claimed the row
// fired — an explanation that diverged from enforcement.
func TestPolicyWhySocketParityAcrossWriteSpellings(t *testing.T) {
	tbl, store, _ := policyTable(t)
	addPolicyAt(t, store, "no-writes-alias", policy.ToolAccess, mainTarget, policy.Deny,
		`{"toolName":"write_file","state":"deny"}`, "")

	call := func(tool string) PolicyWhyResult {
		params := `{"type":"tool_access","agent":"main","tool":"` + tool + `"}`
		res, err := tbl.handlePolicyWhy(context.Background(), nil, json.RawMessage(params))
		if err != nil {
			t.Fatalf("policy.why(%s): %v", tool, err)
		}
		out, ok := res.(PolicyWhyResult)
		if !ok {
			t.Fatalf("policy.why(%s) returned %T, want PolicyWhyResult", tool, res)
		}
		return out
	}

	honest := call("write")
	alias := call("write_file")
	if honest.Allowed != alias.Allowed || honest.Sentence != alias.Sentence {
		t.Errorf("the socket answers the same capability differently by spelling — why ≠ enforcement, the SEC-059 divergence:\n write:      %+v\n write_file: %+v", honest, alias)
	}
	if honest.Allowed || honest.Unimplemented {
		t.Errorf("the alias-authored deny let a write through (or refused to decide): %+v", honest)
	}
	if !strings.Contains(honest.Sentence, "no-writes-alias") {
		t.Errorf("the sentence must name the row that fired, not read as the default deny: %q", honest.Sentence)
	}
}
