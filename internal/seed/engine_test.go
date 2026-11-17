package seed

import (
	"context"
	"testing"
	"time"
)

func fixedNow() func() time.Time { return func() time.Time { return time.Unix(0, 0) } }

// newEngine builds an engine over a fresh fake repo and one applier for a
// set. The applier is a fakeApplier whose fingerprints the caller sets.
func newEngine(t *testing.T, s Set, a *fakeApplier) (*Engine, *fakeRepo) {
	t.Helper()
	reg, err := NewRegistry([]Set{s}, []Applier{a})
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	repo := newFakeRepo()
	e, err := NewEngine(repo, Options{
		Registry: reg,
		Appliers: map[string]Applier{s.ID: a},
		Now:      fixedNow(),
	})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	return e, repo
}

// inertSet is one inert set with one record at the given version/content.
func inertSet(key string, version int, content string) (Set, *fakeApplier) {
	s := Set{ID: "agents.firstparty", Class: ClassInert, Version: version,
		Records: func() []Record { return []Record{{Key: key, Version: version, Content: []byte(content)}} }}
	return s, newFakeApplier("agents.firstparty", false)
}

// TestApplyCreatesRowAndLedgerAndEventAtomically is the core: a new record
// (absent) applies — the domain row, the ledger and the event are all
// present (one transaction, SD3).
func TestApplyCreatesRowAndLedgerAndEventAtomically(t *testing.T) {
	s, a := inertSet("k", 1, `{"x":1}`)
	e, repo := newEngine(t, s, a)
	by := Actor{Kind: "user", ID: "u1"}
	res, err := e.Apply(context.Background(), s.ID, nil, by, PathExplicit)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if res.Records[0].Op != OpApply {
		t.Fatalf("expected an apply, got %q", res.Records[0].Op)
	}
	if a.upserts != 1 {
		t.Fatalf("the domain Upsert was called %d times, want 1", a.upserts)
	}
	if _, ok, err := repo.LedgerGet(context.Background(), s.ID, "k"); err != nil {
		t.Fatalf("ledger get: %v", err)
	} else if !ok {
		t.Fatal("no ledger row was written")
	}
	if len(repo.events) != 1 || repo.events[0].Op != "apply" {
		t.Fatalf("the apply event was not written: %+v", repo.events)
	}
}

// TestApplyIsIdempotent: a second apply of an up-to-date set writes zero
// rows (SD10, idempotence observable).
func TestApplyIsIdempotent(t *testing.T) {
	s, a := inertSet("k", 1, `{"x":1}`)
	// Seed the "applied" state: the ledger has the applied hash and the
	// applier's live fingerprint matches it.
	e, repo := newEngine(t, s, a)
	a.fingerprints["k"] = FakeRow{Hash: Hash([]byte(`{"x":1}`)), Present: true}
	by := Actor{Kind: "user", ID: "u1"}
	if _, err := e.Apply(context.Background(), s.ID, nil, by, PathExplicit); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	before := repo.writes
	if _, err := e.Apply(context.Background(), s.ID, nil, by, PathExplicit); err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if repo.writes != before {
		t.Fatalf("a second apply of an up-to-date set wrote %d more rows, want 0 (idempotent)", repo.writes-before)
	}
}

// TestApplyUpdatesHigherVersionInPlace: a higher version over an unmodified
// row is an update in place (op=update).
func TestApplyUpdatesHigherVersionInPlace(t *testing.T) {
	s, a := inertSet("k", 2, `{"x":2}`)
	e, repo := newEngine(t, s, a)
	// The row is at version 1 (unmodified): the live hash equals the applied
	// hash, the code version is 2.
	appliedHash := Hash([]byte(`{"x":1}`))
	repo.ledger["agents.firstparty/k"] = LedgerRow{SetID: "agents.firstparty", RecordKey: "k", AppliedVersion: 1, AppliedHash: appliedHash, State: "applied"}
	a.fingerprints["k"] = FakeRow{Hash: appliedHash, Present: true}
	by := Actor{Kind: "user", ID: "u1"}
	res, err := e.Apply(context.Background(), s.ID, nil, by, PathExplicit)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if res.Records[0].Op != OpUpdate {
		t.Fatalf("expected an update, got %q", res.Records[0].Op)
	}
	row, ok, err := repo.LedgerGet(context.Background(), "agents.firstparty", "k")
	if err != nil {
		t.Fatalf("ledger get: %v", err)
	}
	if !ok || row.AppliedVersion != 2 {
		t.Fatalf("the update did not move the ledger to v2: %+v", row)
	}
}

// TestApplyRefusesDowngradeBeforeAnyWrite: a lower version than applied is
// a refusal that fails the whole apply before any write (SD6).
func TestApplyRefusesDowngradeBeforeAnyWrite(t *testing.T) {
	s, a := inertSet("k", 1, `{"x":1}`)
	e, repo := newEngine(t, s, a)
	repo.ledger["agents.firstparty/k"] = LedgerRow{SetID: "agents.firstparty", RecordKey: "k", AppliedVersion: 5, AppliedHash: Hash([]byte(`{"x":5}`)), State: "applied"}
	a.fingerprints["k"] = FakeRow{Hash: Hash([]byte(`{"x":5}`)), Present: true}
	by := Actor{Kind: "user", ID: "u1"}
	before := repo.writes
	_, err := e.Apply(context.Background(), s.ID, nil, by, PathExplicit)
	if err == nil {
		t.Fatal("a downgrade was not refused")
	}
	if repo.writes != before {
		t.Fatalf("a downgrade wrote %d rows before refusing, want 0", repo.writes-before)
	}
}

// TestApplySkipsDivergedAndRemoved: a modified or removed row is skipped,
// never overwritten (SD6).
func TestApplySkipsDivergedAndRemoved(t *testing.T) {
	t.Run("diverged", func(t *testing.T) {
		s, a := inertSet("k", 1, `{"x":1}`)
		e, repo := newEngine(t, s, a)
		repo.ledger["agents.firstparty/k"] = LedgerRow{SetID: "agents.firstparty", RecordKey: "k", AppliedVersion: 1, AppliedHash: "H1", State: "applied"}
		a.fingerprints["k"] = FakeRow{Hash: "H2", Present: true} // diverged
		by := Actor{Kind: "user", ID: "u1"}
		res, err := e.Apply(context.Background(), s.ID, nil, by, PathExplicit)
		if err != nil {
			t.Fatalf("apply: %v", err)
		}
		if res.Records[0].Op != OpSkip || res.Records[0].Reason == "" {
			t.Fatalf("a diverged row was not skipped with a reason: %+v", res.Records[0])
		}
	})
	t.Run("removed", func(t *testing.T) {
		s, a := inertSet("k", 1, `{"x":1}`)
		repo := newFakeRepo()
		// removed: the ledger says applied but the live row is absent.
		repo.ledger["agents.firstparty/k"] = LedgerRow{SetID: "agents.firstparty", RecordKey: "k", AppliedVersion: 1, AppliedHash: "H", State: "applied"}
		a.fingerprints["k"] = FakeRow{Present: false}
		e2, _ := NewEngine(repo, Options{Registry: mustReg(t, s, a), Appliers: map[string]Applier{s.ID: a}, Now: fixedNow()})
		by := Actor{Kind: "user", ID: "u1"}
		res, err := e2.Apply(context.Background(), s.ID, nil, by, PathExplicit)
		if err != nil {
			t.Fatalf("apply: %v", err)
		}
		if res.Records[0].Op != OpSkip || res.Records[0].Reason == "" {
			t.Fatalf("a removed row was not skipped with a reason: %+v", res.Records[0])
		}
	})
}

// TestDivergedFirstSeenWritesOneEventNotMany: the first detection of a
// diverged state writes one event; a repeat writes none (SD10).
func TestDivergedFirstSeenWritesOneEventNotMany(t *testing.T) {
	s, a := inertSet("k", 1, `{"x":1}`)
	e, repo := newEngine(t, s, a)
	repo.ledger["agents.firstparty/k"] = LedgerRow{SetID: "agents.firstparty", RecordKey: "k", AppliedVersion: 1, AppliedHash: "H1", State: "applied"}
	a.fingerprints["k"] = FakeRow{Hash: "H2", Present: true}
	by := Actor{Kind: "user", ID: "u1"}
	if _, err := e.Apply(context.Background(), s.ID, nil, by, PathExplicit); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	if len(repo.events) != 1 {
		t.Fatalf("first apply wrote %d events, want 1 (the news)", len(repo.events))
	}
	if _, err := e.Apply(context.Background(), s.ID, nil, by, PathExplicit); err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if len(repo.events) != 1 {
		t.Fatalf("second apply wrote more events: %d, want 1 (no news)", len(repo.events))
	}
}

// TestRestoreReappliesAndAudits: a removed record is restored (op=restore).
func TestRestoreReappliesAndAudits(t *testing.T) {
	s, a := inertSet("k", 1, `{"x":1}`)
	e, repo := newEngine(t, s, a)
	repo.ledger["agents.firstparty/k"] = LedgerRow{SetID: "agents.firstparty", RecordKey: "k", AppliedVersion: 1, AppliedHash: "H", State: "removed"}
	a.fingerprints["k"] = FakeRow{Present: false}
	by := Actor{Kind: "user", ID: "u1"}
	res, err := e.Restore(context.Background(), s.ID, "k", by)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if res.Records[0].Op != OpRestore {
		t.Fatalf("expected a restore, got %q", res.Records[0].Op)
	}
	if a.upserts != 1 {
		t.Fatalf("the restore did not re-apply the row: %d upserts", a.upserts)
	}
}

// TestRestoreOfUpToDateIsNoOp: restoring an up-to-date record writes
// nothing, with a reason.
func TestRestoreOfUpToDateIsNoOp(t *testing.T) {
	s, a := inertSet("k", 1, `{"x":1}`)
	e, repo := newEngine(t, s, a)
	repo.ledger["agents.firstparty/k"] = LedgerRow{SetID: "agents.firstparty", RecordKey: "k", AppliedVersion: 1, AppliedHash: Hash([]byte(`{"x":1}`)), State: "applied"}
	a.fingerprints["k"] = FakeRow{Hash: Hash([]byte(`{"x":1}`)), Present: true}
	by := Actor{Kind: "user", ID: "u1"}
	before := repo.writes
	res, err := e.Restore(context.Background(), s.ID, "k", by)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if res.Records[0].Op != OpSkip {
		t.Fatalf("a restore of an up-to-date record was not a no-op: %+v", res.Records[0])
	}
	if repo.writes != before {
		t.Fatalf("a no-op restore wrote %d rows", repo.writes-before)
	}
}

// TestAdoptFillsStubKeepsIdentity: a present stub row with no ledger is
// adopted (op=adopt).
func TestAdoptFillsStubKeepsIdentity(t *testing.T) {
	s, a := inertSet("k", 1, `{"x":1}`)
	e, _ := newEngine(t, s, a)
	a.fingerprints["k"] = FakeRow{Hash: "H", Present: true, Adoptable: true}
	by := Actor{Kind: "user", ID: "u1"}
	res, err := e.Apply(context.Background(), s.ID, nil, by, PathExplicit)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if res.Records[0].Op != OpAdopt {
		t.Fatalf("expected an adopt, got %q", res.Records[0].Op)
	}
	if a.upserts != 1 {
		t.Fatalf("the adopt did not call Upsert(adopt): %d", a.upserts)
	}
}

// TestConflictIsReportedNeverOverwritten: a present row that is not
// adoptable is a conflict skip, never overwritten.
func TestConflictIsReportedNeverOverwritten(t *testing.T) {
	s, a := inertSet("k", 1, `{"x":1}`)
	e, repo := newEngine(t, s, a)
	a.fingerprints["k"] = FakeRow{Hash: "H", Present: true, Adoptable: false}
	by := Actor{Kind: "user", ID: "u1"}
	res, err := e.Apply(context.Background(), s.ID, nil, by, PathExplicit)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if res.Records[0].Op != OpSkip {
		t.Fatalf("a conflict was not skipped: %+v", res.Records[0])
	}
	if a.upserts != 0 {
		t.Fatalf("a conflict row was overwritten: %d upserts", a.upserts)
	}
	if repo.writes != 0 {
		t.Fatalf("a conflict wrote %d rows", repo.writes)
	}
}

// TestRetireMarksNeverDeletes: a record that left the code is retired, its
// row kept.
func TestRetireMarksNeverDeletes(t *testing.T) {
	s, a := inertSet("a", 1, `{"x":1}`) // 'b' left the code
	e, repo := newEngine(t, s, a)
	repo.ledger["agents.firstparty/b"] = LedgerRow{SetID: "agents.firstparty", RecordKey: "b", AppliedVersion: 1, AppliedHash: "H", State: "applied"}
	a.fingerprints["a"] = FakeRow{Hash: Hash([]byte(`{"x":1}`)), Present: true}
	by := Actor{Kind: "user", ID: "u1"}
	res, err := e.Apply(context.Background(), s.ID, nil, by, PathExplicit)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	var retired *RecordResult
	for i := range res.Records {
		if res.Records[i].Key == "b" {
			retired = &res.Records[i]
		}
	}
	if retired == nil || retired.Op != OpRetire {
		t.Fatalf("a missing record was not retired: %+v", res.Records)
	}
}

// TestStartupPathRefusesGuardSets: the class gate — a startup apply of a
// guard set is refused (SD5, the headline invariant).
func TestStartupPathRefusesGuardSets(t *testing.T) {
	guard := Set{ID: "policy.corpus", Class: ClassGuard, Version: 1,
		Records: func() []Record { return []Record{{Key: "k", Version: 1, Content: []byte(`{}`)}} }}
	a := newFakeApplier("policy.corpus", true)
	reg, err := NewRegistry([]Set{guard}, []Applier{a})
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	repo := newFakeRepo()
	e, err := NewEngine(repo, Options{Registry: reg, Appliers: map[string]Applier{"policy.corpus": a}, Now: fixedNow()})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	by := Actor{Kind: "system", ID: "startup"}
	before := repo.writes
	_, err = e.Apply(context.Background(), "policy.corpus", nil, by, PathStartup)
	if err == nil {
		t.Fatal("a startup apply of a guard set was not refused (SD5)")
	}
	if repo.writes != before {
		t.Fatalf("a refused guard apply wrote %d rows", repo.writes-before)
	}
}

// TestApplyInertTouchesOnlyInertSets: ApplyInert applies only the inert
// sets; a guard set in the registry is not touched at startup.
func TestApplyInertTouchesOnlyInertSets(t *testing.T) {
	inert := Set{ID: "agents.firstparty", Class: ClassInert, Version: 1,
		Records: func() []Record { return []Record{{Key: "a", Version: 1, Content: []byte(`{}`)}} }}
	guard := Set{ID: "policy.corpus", Class: ClassGuard, Version: 1,
		Records: func() []Record { return []Record{{Key: "b", Version: 1, Content: []byte(`{}`)}} }}
	aInert := newFakeApplier("agents.firstparty", false)
	aGuard := newFakeApplier("policy.corpus", true)
	reg, err := NewRegistry([]Set{inert, guard}, []Applier{aInert, aGuard})
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	repo := newFakeRepo()
	e, err := NewEngine(repo, Options{Registry: reg, Appliers: map[string]Applier{"agents.firstparty": aInert, "policy.corpus": aGuard}, Now: fixedNow()})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	if _, err := e.ApplyInert(context.Background(), Actor{Kind: "system", ID: "startup"}); err != nil {
		t.Fatalf("ApplyInert: %v", err)
	}
	if aInert.upserts != 1 {
		t.Fatalf("the inert set was not applied: %d upserts", aInert.upserts)
	}
	if aGuard.upserts != 0 {
		t.Fatalf("ApplyInert touched the guard set: %d upserts (the D4 boundary)", aGuard.upserts)
	}
}

// TestEmptyActorRefused: a seed with no actor is refused.
func TestEmptyActorRefused(t *testing.T) {
	s, _ := inertSet("k", 1, `{"x":1}`)
	e, _ := newEngine(t, s, newFakeApplier(s.ID, false))
	_, err := e.Apply(context.Background(), s.ID, nil, Actor{}, PathExplicit)
	if err == nil {
		t.Fatal("an unattributed seed was not refused")
	}
}

// TestNewEngineRefusesSetWithoutApplierOrClassMismatch is the constructor's
// refusal: a set with no applier, and an inert set with a guard-affecting
// applier (rules 6, 7).
func TestNewEngineRefusesSetWithoutApplierOrClassMismatch(t *testing.T) {
	s := Set{ID: "x.y", Class: ClassInert, Version: 1, Records: func() []Record { return []Record{{Key: "k", Version: 1, Content: []byte(`{}`)}} }}
	reg, err := NewRegistry([]Set{s}, []Applier{newFakeApplier("x.y", false)})
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	_, err = NewEngine(newFakeRepo(), Options{Registry: reg, Appliers: map[string]Applier{}, Now: fixedNow()})
	if err == nil {
		t.Fatal("a set with no applier was accepted")
	}
	guard := newFakeApplier("x.y", true)
	_, err = NewEngine(newFakeRepo(), Options{Registry: reg, Appliers: map[string]Applier{"x.y": guard}, Now: fixedNow()})
	if err == nil {
		t.Fatal("an inert set with a guard-affecting applier was accepted (SD5)")
	}
}

// mustReg is a helper for the tests that build a registry inline.
func mustReg(t *testing.T, s Set, a Applier) *Registry {
	t.Helper()
	reg, err := NewRegistry([]Set{s}, []Applier{a})
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	return reg
}
