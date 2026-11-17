package policy_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/policy"
)

// S45 T12: the seed door's twin writes — AddInTx / RemoveInTx /
// RecordAuthoredInTx — plus the provenance parse and the origin half of the
// listing filter. The tests are the sibling set: the plain door's frozen
// tests stand untouched beside them.

type seedBridge struct {
	added    []policy.Row
	addErr   error
	removed  []string
	removeOK bool
	events   []policyEventCall
}

type policyEventCall struct {
	kind, subject, policyID, detail string
	at                              time.Time
}

func (b *seedBridge) funcs() policy.RepoFuncs {
	return policy.RepoFuncs{
		Add: func(_ context.Context, r policy.Row) error {
			b.added = append(b.added, r)
			return b.addErr
		},
		Remove:      func(_ context.Context, id string) (bool, error) { return id != "", nil },
		ListAll:     func(_ context.Context) ([]policy.Row, error) { return b.added, nil },
		EventInsert: func(context.Context, string, string, string, string, time.Time) error { return nil },
		AddTx: func(_ context.Context, _ policy.Tx, r policy.Row) error {
			b.added = append(b.added, r)
			return b.addErr
		},
		RemoveTx: func(_ context.Context, _ policy.Tx, id string) (bool, error) {
			b.removed = append(b.removed, id)
			return b.removeOK, nil
		},
		EventInsertTx: func(_ context.Context, _ policy.Tx, kind, subject, policyID, detail string, at time.Time) error {
			b.events = append(b.events, policyEventCall{kind, subject, policyID, detail, at})
			return nil
		},
	}
}

// A marker value standing in for the wiring's transaction handle: policy.Tx
// is an opaque seam by design (the store's *sql.Tx rides through untouched),
// so the boundary tests pass anything non-nil and the WIRED bridges — the
// daemon's and the CLI's — are where the type is checked.
var txSeed = policy.Tx("a transaction handle the test does not open")

func TestAddInTxRefusesNilTransactionAndForeignProvenance(t *testing.T) {
	b := &seedBridge{}
	s := policy.NewStore(b.funcs())
	p := boundaryPolicy(t, "seeded", policy.AgentKind, "boss", policy.Allow, policy.Operator, allowSpec())

	if rej := s.AddInTx(context.Background(), nil, p, policy.OriginSystem, addAt); rej == nil ||
		!strings.Contains(rej.Reason, "no transaction") {
		t.Fatalf("nil transaction: %+v", rej)
	}
	if len(b.added) != 0 {
		t.Fatal("a refused write reached the repo")
	}

	rej := s.AddInTx(context.Background(), txSeed, p, "machine", addAt)
	if rej == nil || !strings.Contains(rej.Reason, "machine") {
		t.Fatalf("foreign provenance: %+v", rej)
	}
	rej = s.AddInTx(context.Background(), txSeed, p, "", addAt)
	if rej == nil || !strings.Contains(rej.Reason, "origin") {
		t.Fatalf("empty provenance: %+v", rej)
	}
	if len(b.added) != 0 {
		t.Fatal("a refused provenance reached the repo")
	}
}

// TestAddInTxRunsTheOneJudgement is PL1's arithmetic at the seed door: the
// transaction door holds NO second check — an allow from a non-operator
// source is refused here with the SAME sentence the plain Add says,
// because both call the same ValidateForAdd.
func TestAddInTxRunsTheOneJudgement(t *testing.T) {
	b := &seedBridge{}
	s := policy.NewStore(b.funcs())
	p := boundaryPolicy(t, "seeded", policy.AgentKind, "boss", policy.Allow, policy.Agent, allowSpec())

	txRej := s.AddInTx(context.Background(), txSeed, p, policy.OriginSystem, addAt)
	plainRej := s.Add(context.Background(), p, addAt)
	if txRej == nil || plainRej == nil {
		t.Fatalf("the authorship rule was skipped: tx=%v plain=%v", txRej, plainRej)
	}
	if txRej.Reason != plainRej.Reason {
		t.Fatalf("two doors, two sentences — the seed door holds a second judgement:\n tx: %s\nplain: %s", txRej.Reason, plainRej.Reason)
	}
}

// TestAddInTxRowIsTheOneBuilders is the other half of the shared-builder
// claim: at the same instant the tx door's row is the plain door's row,
// field for field, except the ONE field the tx door exists to name.
func TestAddInTxRowIsTheOneBuilders(t *testing.T) {
	b := &seedBridge{}
	s := policy.NewStore(b.funcs())
	p := boundaryPolicy(t, "seeded", policy.AgentKind, "boss", policy.Allow, policy.Operator, allowSpec())

	if rej := s.Add(context.Background(), p, addAt); rej != nil {
		t.Fatalf("plain add: %s", rej.Reason)
	}
	plainRow := b.added[0]
	if rej := s.AddInTx(context.Background(), txSeed, p, policy.OriginSystem, addAt); rej != nil {
		t.Fatalf("tx add: %s", rej.Reason)
	}
	txRow := b.added[1]
	if txRow.Origin != policy.OriginSystem {
		t.Fatalf("the tx door's row carries origin %q", txRow.Origin)
	}
	plainRow.Origin = txRow.Origin // the ONE admitted difference
	if plainRow != txRow {
		t.Fatalf("the doors build different rows:\n plain: %+v\n   tx: %+v", plainRow, txRow)
	}
}

// TestAddInTxNamesAnAbsentDoor: a wiring without the transaction hooks has
// no seed door, and the refusal says so rather than writing somewhere
// else. (RepoFuncs answers the method; the nil HOOK names the absence.)
func TestAddInTxNamesAnAbsentDoor(t *testing.T) {
	bare := policy.NewStore(policy.RepoFuncs{
		Add: func(context.Context, policy.Row) error { return nil },
	})
	p := boundaryPolicy(t, "seeded", policy.AgentKind, "boss", policy.Allow, policy.Operator, allowSpec())
	rej := bare.AddInTx(context.Background(), txSeed, p, policy.OriginSystem, addAt)
	if rej == nil || !strings.Contains(rej.Reason, "no seed door") {
		t.Fatalf("absent door: %+v", rej)
	}
}

func TestRemoveInTxPassesThroughAndRefusesNil(t *testing.T) {
	b := &seedBridge{removeOK: true}
	s := policy.NewStore(b.funcs())
	if _, err := s.RemoveInTx(context.Background(), nil, "x"); err == nil ||
		!strings.Contains(err.Error(), "no transaction") {
		t.Fatalf("nil transaction: %v", err)
	}
	ok, err := s.RemoveInTx(context.Background(), txSeed, "gone")
	if err != nil || !ok || len(b.removed) != 1 || b.removed[0] != "gone" {
		t.Fatalf("pass-through: %v %v %v", ok, err, b.removed)
	}
	bare := policy.NewStore(policy.RepoFuncs{})
	if _, err := bare.RemoveInTx(context.Background(), txSeed, "x"); err == nil ||
		!strings.Contains(err.Error(), "no seed door") {
		t.Fatalf("absent door: %v", err)
	}
}

// ── the authored record on the caller's transaction ─────────────────────────

func TestRecordAuthoredInTxGatesThenHandsOver(t *testing.T) {
	b := &seedBridge{}
	s := policy.NewStore(b.funcs())
	ctx := context.Background()

	if rej := s.RecordAuthoredInTx(ctx, nil, "agent:boss", "r1", addAt); rej == nil ||
		!strings.Contains(rej.Reason, "no transaction") {
		t.Fatalf("nil transaction: %+v", rej)
	}
	if rej := s.RecordAuthoredInTx(ctx, txSeed, "agent:boss", "", addAt); rej == nil ||
		!strings.Contains(rej.Reason, "names no rule") {
		t.Fatalf("no rule named: %+v", rej)
	}
	if rej := s.RecordAuthoredInTx(ctx, txSeed, "boss-without-kind", "r1", addAt); rej == nil {
		t.Fatal("a half-subject passed the gate")
	}
	if rej := s.RecordAuthoredInTx(ctx, txSeed, "agent:boss", "r1", time.Time{}); rej == nil ||
		!strings.Contains(rej.Reason, "no instant") {
		t.Fatalf("no instant: %+v", rej)
	}
	if len(b.events) != 0 {
		t.Fatal("a refused record reached the repo")
	}

	if rej := s.RecordAuthoredInTx(ctx, txSeed, "agent:boss", "r1", addAt); rej != nil {
		t.Fatalf("good record: %s", rej.Reason)
	}
	if len(b.events) != 1 {
		t.Fatalf("%d event rows for one record", len(b.events))
	}
	e := b.events[0]
	if e.kind != "authored" || e.subject != "agent:boss" || e.policyID != "r1" || !e.at.Equal(addAt) {
		t.Fatalf("handover drifted: %+v", e)
	}
	if !strings.HasPrefix(e.detail, "authored:") {
		t.Fatalf("detail %q is not the constant authored row", e.detail)
	}
}

func TestRecordAuthoredInTxNamesAnAbsentDoor(t *testing.T) {
	bare := policy.NewStore(policy.RepoFuncs{})
	rej := bare.RecordAuthoredInTx(context.Background(), txSeed, "agent:boss", "r1", addAt)
	if rej == nil || !strings.Contains(rej.Reason, "no seed door") {
		t.Fatalf("absent door: %+v", rej)
	}
}

// ── provenance: the parse and the filter ────────────────────────────────────

func TestParseOriginAdmitsThreeAndRefusesByName(t *testing.T) {
	for _, ok := range []string{policy.OriginSystem, policy.OriginUser, policy.OriginImport} {
		got, rej := policy.ParseOrigin(ok)
		if rej != nil || got != ok {
			t.Fatalf("%q: got %q rej %v", ok, got, rej)
		}
	}
	for _, bad := range []string{"", "machine", "SYSTEM", "System", "seed"} {
		if _, rej := policy.ParseOrigin(bad); rej == nil || !strings.Contains(rej.Reason, "origin") {
			t.Fatalf("%q was not refused by name: %+v", bad, rej)
		}
	}
}

func TestFilterListedKeepsProvenanceBesidePolicy(t *testing.T) {
	rows := []policy.Listed{
		{Policy: listedPolicy(t, "seed-1", policy.AgentKind, "boss", policy.Allow, policy.Operator), Origin: "system"},
		{Policy: listedPolicy(t, "hand-1", policy.AgentKind, "boss", policy.Deny, policy.Operator), Origin: "user"},
		{Policy: listedPolicy(t, "seed-2", policy.AgentKind, "other", policy.Deny, policy.Operator), Origin: "system"},
	}
	f := policy.ListFilter{Origin: "system"}
	got := policy.FilterListed(rows, f, addAt)
	if len(got) != 2 || got[0].Policy.ID != "seed-1" || got[1].Policy.ID != "seed-2" {
		t.Fatalf("origin filter: %+v", got)
	}
	f = policy.ListFilter{Origin: "user"}
	if got := policy.FilterListed(rows, f, addAt); len(got) != 1 || got[0].Policy.ID != "hand-1" {
		t.Fatalf("origin filter (user): %+v", got)
	}
	// The two halves COMPOSE: the policy filter never sees origin and the
	// origin half never sees policy.
	f = policy.ListFilter{Origin: "system", Target: &policy.Target{Kind: policy.AgentKind, ID: "boss"}}
	if got := policy.FilterListed(rows, f, addAt); len(got) != 1 || got[0].Policy.ID != "seed-1" {
		t.Fatalf("composed filter: %+v", got)
	}
	// The EMPTY filter is the one that hides nothing: every row, both
	// halves. (The zero ListFilter is NOT it — IncludeExpired false is a
	// criterion, an expiry-hiding filter removes rows; that is T2's frozen
	// semantics, and origin changed nothing.)
	if !(policy.ListFilter{IncludeExpired: true}).Empty() {
		t.Fatal("the keep-everything filter says it filters")
	}
	if got := policy.FilterListed(rows, policy.ListFilter{IncludeExpired: true}, addAt); len(got) != 3 {
		t.Fatalf("empty filter dropped rows: %+v", got)
	}
}

func TestListDetailedCarriesOrigin(t *testing.T) {
	b := &seedBridge{}
	b.added = []policy.Row{
		{ID: "r1", Type: "tool_access", TargetKind: "agent", TargetID: "boss",
			Action: "deny", Source: "operator", SpecJSON: deny(), CreatedAt: "2026-10-19T12:00:00.123456789Z", Origin: "system"},
	}
	s := policy.NewStore(b.funcs())
	rows, rej := s.ListDetailed(context.Background(), addAt)
	if rej != nil {
		t.Fatalf("list: %s", rej.Reason)
	}
	if len(rows) != 1 || rows[0].Origin != "system" {
		t.Fatalf("origin did not ride the listing: %+v", rows)
	}
}

func listedPolicy(t *testing.T, id string, kind policy.TargetKind, targetID string,
	action policy.Action, source policy.Source) policy.Policy {
	t.Helper()
	p := boundaryPolicy(t, id, kind, targetID, action, source, denySpec())
	return p
}
