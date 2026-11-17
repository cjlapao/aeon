package protocol

// The authoring audit on the wire (S45 T3, PM8/PM11): policy.changes is the
// ONE read door onto the append-only audit, and these tests pin the three
// honest answers (fault ≠ empty-table ≠ filtered-empty), the always-present
// unauditedPolicyIds, the pass-through of limit to the accessor's own clamp,
// the identifier gate on the filter (SEC-046 delegation), and — beside the
// zero-writes rule every policy read carries — the seam's own shape: a
// READS-ONLY interface is the append-only trigger expressed in Go.
//
// The fake stands in for the daemon adapter (same shape store-side:
// internal/store/policy_change_test.go runs the real accessors; this file
// runs the DOOR).

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// policyChangeFake is the seam's test double: rows are planted through the
// test's own `plant` (the daemon adapter has no writer either — that is the
// point), list answers newest-first, and both reads count their calls (the
// pass-through and the "no cache, always live" claims are measured, not
// commented).
type policyChangeFake struct {
	rows   []PolicyChangeView // planted oldest-first; answered newest-first
	nextID int64
	lists  int
	counts int
	// faults, when set, make the corresponding read fail (the three-state
	// proof): a stand-in for the store's own error sentence.
	listErr, countErr error
}

func (f *policyChangeFake) plant(op, policyID, summary, actorKind, actorID string, proposalID *int64) {
	f.nextID++
	f.rows = append(f.rows, PolicyChangeView{
		ID: f.nextID, Op: op, PolicyID: policyID, ProposalID: proposalID,
		Summary: summary, ActorKind: actorKind, ActorID: actorID,
		OccurredAt: "2026-10-31T10:00:00Z",
	})
}

func (f *policyChangeFake) PolicyChangeList(_ context.Context, policyID string, limit int) ([]PolicyChangeView, error) {
	f.lists++
	if f.listErr != nil {
		return nil, f.listErr
	}
	// The accessor's clamp contract, mirrored here because the door passes
	// limit THROUGH (TestPolicyChangesLimitPassesThrough pins that the
	// handler does not restate it; internal/store pins the clamp itself).
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	var out []PolicyChangeView
	for i := len(f.rows) - 1; i >= 0 && len(out) < limit; i-- {
		c := f.rows[i]
		if policyID != "" && c.PolicyID != policyID {
			continue
		}
		out = append(out, c)
	}
	return out, nil
}

func (f *policyChangeFake) PolicyChangeCount(_ context.Context) (int, error) {
	f.counts++
	if f.countErr != nil {
		return 0, f.countErr
	}
	return len(f.rows), nil
}

// ptr64 is the tiny helper the wire tests need for the nullable proposal id
// (NULL is a nil *int64, never 0 — a 0 would name a row that does not
// exist).
func ptr64(v int64) *int64 { return &v }

// policyChangesTable wires a table with the audit seam over a fresh fake
// (and the policy boundary too, so the zero-writes spy speaks).
func policyChangesTable(t *testing.T) (*Table, *policyChangeFake, *policySpy) {
	t.Helper()
	tbl, _, fx := policyTable(t)
	fake := &policyChangeFake{}
	tbl.PolicyChanges = fake
	return tbl, fake, fx.spy
}

// TestPolicyChangeStoreSeamHasNoWriter is PM8 expressed in the type system:
// the seam carries exactly the two reads — no append, no rewrite, no
// remove. An audit this door could edit would be the audit that isn't (the
// trigger is the enforcement store-side; the ABSENT METHOD is the
// enforcement door-side).
func TestPolicyChangeStoreSeamHasNoWriter(t *testing.T) {
	iface := reflect.TypeOf((*PolicyChangeStore)(nil)).Elem()
	if iface.NumMethod() != 2 {
		names := []string{}
		for i := 0; i < iface.NumMethod(); i++ {
			names = append(names, iface.Method(i).Name)
		}
		t.Fatalf("the seam has %d methods %v, want exactly the two reads (PolicyChangeList, PolicyChangeCount)", iface.NumMethod(), names)
	}
	for _, want := range []string{"PolicyChangeList", "PolicyChangeCount"} {
		if _, ok := iface.MethodByName(want); !ok {
			t.Fatalf("the seam is missing its read %s", want)
		}
	}
}

// TestPolicyChangesReadEmptyStatement pins the empty audit's honesty: the
// EMPTY TABLE answers with its own sentence (Count's answer, not "my
// filter matched nothing"), changes [] and unauditedPolicyIds [] — all
// present, none null. A non-empty table answers WITHOUT the statement: the
// sentence belongs to the empty table alone.
func TestPolicyChangesReadEmptyStatement(t *testing.T) {
	tbl, fake, _ := policyChangesTable(t)

	empty := dispatchPolicy[PolicyChangesResult](t, tbl, MethodPolicyChanges, nil)
	if empty.Statement != PolicyChangesEmptyStatement {
		t.Errorf("empty audit statement = %q, want %q", empty.Statement, PolicyChangesEmptyStatement)
	}
	if empty.Changes == nil || len(empty.Changes) != 0 {
		t.Errorf("empty changes = %+v, want an empty non-null list", empty.Changes)
	}
	if empty.UnauditedPolicyIDs == nil {
		t.Error("unauditedPolicyIds is null — the always-present field vanished on the empty answer")
	}

	fake.plant("add", "grant-bash", `added policy "grant-bash"`, "user", "1000", nil)
	full := dispatchPolicy[PolicyChangesResult](t, tbl, MethodPolicyChanges, nil)
	if full.Statement != "" {
		t.Errorf("a non-empty audit carried the empty-table statement %q — the audit's own emptiness is a lie here", full.Statement)
	}
	if len(full.Changes) != 1 || full.Changes[0].Op != "add" || full.Changes[0].PolicyID != "grant-bash" {
		t.Fatalf("the one row did not come back: %+v", full.Changes)
	}

	// The FILTERED-empty answer beside a full table: NO statement — the
	// filter's emptiness is the filter's answer (PolicyListFilterEmptyStatement's
	// discipline: the two empties never read the same).
	filtered := dispatchPolicy[PolicyChangesResult](t, tbl, MethodPolicyChanges,
		PolicyChangesParams{PolicyID: "never-authored"})
	if filtered.Statement != "" {
		t.Errorf("filtered-empty beside a non-empty table carried %q — the table's sentence, printed by the filter", filtered.Statement)
	}
	if filtered.Changes == nil || len(filtered.Changes) != 0 {
		t.Errorf("filtered changes = %+v, want []", filtered.Changes)
	}
}

// TestPolicyChangesReadIsReadOnlyAndLive: the read writes nothing anywhere
// (the boundary's spy stays at zero — the audit table is not the boundary's,
// and the door touches neither), and a row planted between two calls is
// visible to the NEXT call (PM11: re-read per call, no cache may creep
// between an authoring write and the audit read).
func TestPolicyChangesReadIsReadOnlyAndLive(t *testing.T) {
	tbl, fake, spy := policyChangesTable(t)
	fake.plant("add", "grant-bash", `added policy "grant-bash"`, "user", "1000", nil)
	spy.reset()

	first := dispatchPolicy[PolicyChangesResult](t, tbl, MethodPolicyChanges, nil)
	if len(first.Changes) != 1 {
		t.Fatalf("first read: %+v", first.Changes)
	}
	if *spy != (policySpy{}) {
		t.Errorf("policy.changes wrote to the boundary: %+v", *spy)
	}

	// Plant BETWEEN calls: no restart, no pause — the next call sees it.
	fake.plant("remove", "grant-bash", `removed policy "grant-bash"`, "cli", "cli", nil)
	second := dispatchPolicy[PolicyChangesResult](t, tbl, MethodPolicyChanges, nil)
	if len(second.Changes) != 2 {
		t.Fatalf("the second row was not live: %+v", second.Changes)
	}
	if second.Changes[0].ID != 2 || second.Changes[1].ID != 1 {
		t.Errorf("not newest-first on the wire: ids %d,%d", second.Changes[0].ID, second.Changes[1].ID)
	}
	if *spy != (policySpy{}) {
		t.Errorf("the audit read wrote after all: %+v", *spy)
	}
	if fake.lists != 2 || fake.counts != 2 {
		t.Errorf("reads: lists=%d counts=%d, want 2/2 (one pair per call, no cached answer)", fake.lists, fake.counts)
	}
}

// TestPolicyChangesLimitPassesThrough: the door hands `limit` to the seam
// UNTOUCHED (the clamp [1,500]/default 50 is the accessor's contract, one
// definition — the handler restating it would be a second number free to
// drift). The fake answers with the clamped window; the wire agrees.
func TestPolicyChangesLimitPassesThrough(t *testing.T) {
	tbl, fake, _ := policyChangesTable(t)
	for i := 0; i < 5; i++ {
		fake.plant("seed", "", "seeded the corpus", "cli", "cli", nil)
	}

	res := dispatchPolicy[PolicyChangesResult](t, tbl, MethodPolicyChanges, PolicyChangesParams{Limit: 3})
	if len(res.Changes) != 3 {
		t.Fatalf("limit=3 returned %d, want the window the seam gives", len(res.Changes))
	}
	// limit 0 = the accessor's default, not the handler's zero.
	res = dispatchPolicy[PolicyChangesResult](t, tbl, MethodPolicyChanges, PolicyChangesParams{Limit: 0})
	if len(res.Changes) != 5 {
		t.Fatalf("limit=0 read %d rows (the fake defaults to 50; below that the handler must pass 0 through)", len(res.Changes))
	}
}

// TestPolicyChangesFaultIsNotTheEmptyAnswer is the DEF-041 three-state
// refusal (PM8): a FAULT reading the audit arrives as the typed -31841,
// never as the empty statement, never as an empty list — the store's own
// sentence travels in `what` verbatim. Both reads are covered (the list
// and the Count that decides the statement). The storeless table keeps the
// requirePolicyStore idiom: ErrStoreUnavailable, registered but honest.
func TestPolicyChangesFaultIsNotTheEmptyAnswer(t *testing.T) {
	tbl, fake, _ := policyChangesTable(t)
	fake.plant("add", "grant-bash", `added policy "grant-bash"`, "user", "1000", nil)
	fake.listErr = errors.New("database is locked")

	aeon := mustAeonCode(t, tbl, MethodPolicyChanges, nil, ErrPolicyChangesUnavailable.Code)
	if !strings.Contains(aeon.Data.What, "database is locked") {
		t.Errorf("the store's sentence did not travel: %q", aeon.Data.What)
	}
	if strings.Contains(aeon.Data.What, PolicyChangesEmptyStatement) {
		t.Error("the fault answer carried the empty-table sentence — the lie the code exists to refuse")
	}

	fake.listErr = nil
	fake.countErr = errors.New("no such table: audit")
	aeon = mustAeonCode(t, tbl, MethodPolicyChanges, nil, ErrPolicyChangesUnavailable.Code)
	if !strings.Contains(aeon.Data.What, "audit") {
		t.Errorf("the Count fault's sentence did not travel: %q", aeon.Data.What)
	}

	// A door with no seam wired (storeless daemon) is the store-absence
	// answer, not a fault and not an empty audit.
	bare, _, _ := policyTable(t)
	mustAeonCode(t, bare, MethodPolicyChanges, nil, ErrStoreUnavailable.Code)
}

// TestPolicyChangesRefusesUnauthorablePolicyID: the filter's id passes the
// boundary's OWN identifier gate (SEC-046 delegation) — a name that could
// never have been authored cannot be the audit's subject either; the
// refusal names it (the policyRefusal shape, ErrPolicyReadRefused).
func TestPolicyChangesRefusesUnauthorablePolicyID(t *testing.T) {
	tbl, _, _ := policyChangesTable(t)
	aeon := mustAeonCode(t, tbl, MethodPolicyChanges,
		PolicyChangesParams{PolicyID: "not an id; spaces and semicolons"}, ErrPolicyReadRefused.Code)
	if !strings.Contains(aeon.Data.What, "not an id; spaces and semicolons") {
		t.Errorf("the refused id was not named: %q", aeon.Data.What)
	}
}

// TestPolicyChangesUnauditedFieldIsAlwaysPresent: [] even on the empty
// answer, [] beside rows — T4's reconciliation field has no omitempty
// precisely so a client can never forget to check it. (Today [] states
// "this door has not looked"; T4's reconciliation read turns it into the
// measured answer — recorded in the task Notes.)
func TestPolicyChangesUnauditedFieldIsAlwaysPresent(t *testing.T) {
	tbl, fake, _ := policyChangesTable(t)
	empty := dispatchPolicy[PolicyChangesResult](t, tbl, MethodPolicyChanges, nil)
	if empty.UnauditedPolicyIDs == nil || len(empty.UnauditedPolicyIDs) != 0 {
		t.Errorf("empty-table unauditedPolicyIds = %v, want [] (present)", empty.UnauditedPolicyIDs)
	}
	fake.plant("commit", "", "committed proposal 7", "user", "1000", ptr64(7))
	full := dispatchPolicy[PolicyChangesResult](t, tbl, MethodPolicyChanges, nil)
	if full.UnauditedPolicyIDs == nil {
		t.Error("non-empty-table unauditedPolicyIds = null, want [] (present)")
	}
	if full.Changes[0].ProposalID == nil || *full.Changes[0].ProposalID != 7 {
		t.Errorf("the commit row lost its proposal id: %+v", full.Changes[0])
	}
}
