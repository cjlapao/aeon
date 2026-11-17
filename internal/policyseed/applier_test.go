package policyseed

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/seed"
	"github.com/cjlapao/aeon/internal/store"
)

// openStore opens a fully-migrated temp store (the agentseed pattern — the
// tests never point at the real store).
func openStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(store.Options{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// testBoundary is the write boundary over the real store — the same
// RepoFuncs bridge the daemon and the CLI wire (a test-local copy because
// the bridge is wiring and the two production ones are unexported; the
// hooks that matter here are the transaction ones the applier drives).
func testBoundary(s *store.Store) *policy.Store {
	f := policy.RepoFuncs{
		Add: func(ctx context.Context, r policy.Row) error {
			return s.PolicyAdd(ctx, storeRow(r))
		},
		Remove: func(ctx context.Context, id string) (bool, error) {
			return s.PolicyRemove(ctx, id)
		},
		ListAll: func(ctx context.Context) ([]policy.Row, error) {
			rows, err := s.PolicyListAll(ctx)
			if err != nil {
				return nil, err
			}
			out := make([]policy.Row, 0, len(rows))
			for _, r := range rows {
				out = append(out, policyRow(r))
			}
			return out, nil
		},
		EventInsert: s.PolicyEventInsert,
		AddTx: func(ctx context.Context, tx policy.Tx, r policy.Row) error {
			sqlTx, ok := tx.(*sql.Tx)
			if !ok {
				panic("policyseed test: a transaction this wiring did not open")
			}
			return s.PolicyAddTx(ctx, sqlTx, storeRow(r))
		},
		RemoveTx: func(ctx context.Context, tx policy.Tx, id string) (bool, error) {
			sqlTx, ok := tx.(*sql.Tx)
			if !ok {
				panic("policyseed test: a transaction this wiring did not open")
			}
			return s.PolicyDeleteTx(ctx, sqlTx, id)
		},
		EventInsertTx: func(ctx context.Context, tx policy.Tx, kind, subject, policyID, detail string, at time.Time) error {
			sqlTx, ok := tx.(*sql.Tx)
			if !ok {
				panic("policyseed test: a transaction this wiring did not open")
			}
			return s.PolicyEventInsertTx(ctx, sqlTx, kind, subject, policyID, detail, at)
		},
	}
	return policy.NewStore(f)
}

func storeRow(r policy.Row) store.PolicyRow {
	return store.PolicyRow{
		ID: r.ID, Type: r.Type, TargetKind: r.TargetKind, TargetID: r.TargetID,
		Action: r.Action, Source: r.Source, Subject: r.Subject,
		SpecJSON: r.SpecJSON, NotAfter: r.NotAfter, CreatedAt: r.CreatedAt,
		Origin: r.Origin,
	}
}

func policyRow(r store.PolicyRow) policy.Row {
	return policy.Row{
		ID: r.ID, Type: r.Type, TargetKind: r.TargetKind, TargetID: r.TargetID,
		Action: r.Action, Source: r.Source, Subject: r.Subject,
		SpecJSON: r.SpecJSON, NotAfter: r.NotAfter, CreatedAt: r.CreatedAt,
		Origin: r.Origin,
	}
}

// stubSeedApplier lets the registry cross-checks run over the real
// policyseed Applier without the store-backed type itself having to
// satisfy seed.Tx unwrapping (the daemon owns that adapter; tests own this
// stub).
type stubSeedApplier struct {
	ap    *Applier
	guard bool
}

func (s stubSeedApplier) Set() string                   { return s.ap.Set() }
func (s stubSeedApplier) GuardAffecting() bool          { return s.guard }
func (s stubSeedApplier) Operator() seed.OperatorRights { return s.ap.Operator() }
func (s stubSeedApplier) Fingerprint(seed.Tx, string) (string, bool, error) {
	return "", false, nil
}
func (s stubSeedApplier) Adoptable(seed.Tx, string) (bool, error) { return false, nil }
func (s stubSeedApplier) Upsert(seed.Tx, seed.Record, bool) error { return nil }
func (s stubSeedApplier) Retire(seed.Tx, string) error            { return nil }

// --- the round trip (SD8): the hash the source wrote IS the hash the row
// answers — content is compared, not columns.

func TestFingerprintMatchesDeclaredContent(t *testing.T) {
	s := openStore(t)
	ap := NewApplier(s, testBoundary(s), fixed(2026, 11, 1, 12))
	ctx := context.Background()
	for _, rec := range Source()[:25] { // a head sample; TestRoundTripAllRecords carries the whole set
		if err := s.SeedWithTx(ctx, func(tx *sql.Tx) error {
			return ap.Upsert(ctx, tx, rec, false)
		}); err != nil {
			t.Fatalf("upsert %s: %v", rec.Key, err)
		}
		var hash string
		var present bool
		readTx(t, s, func(tx *sql.Tx) error {
			var err error
			hash, present, err = ap.Fingerprint(ctx, tx, rec.Key)
			return err
		})
		if !present {
			t.Fatalf("row %s absent after upsert", rec.Key)
		}
		if want := seed.Hash(rec.Content); hash != want {
			t.Fatalf("fingerprint %s = %s, want %s — the row and the declaration must hash equal", rec.Key, hash, want)
		}
	}
}

func TestRoundTripAllRecords(t *testing.T) {
	s := openStore(t)
	ap := NewApplier(s, testBoundary(s), fixed(2026, 11, 1, 12))
	ctx := context.Background()
	records := Source()
	if len(records) == 0 {
		t.Fatal("no records")
	}
	if err := s.SeedWithTx(ctx, func(tx *sql.Tx) error {
		for _, rec := range records {
			if err := ap.Upsert(ctx, tx, rec, false); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("bulk upsert: %v", err)
	}
	for _, rec := range records {
		var hash string
		var present bool
		readTx(t, s, func(tx *sql.Tx) error {
			var err error
			hash, present, err = ap.Fingerprint(ctx, tx, rec.Key)
			return err
		})
		if !present || hash != seed.Hash(rec.Content) {
			t.Fatalf("round trip lost %s: present=%v hash=%s", rec.Key, present, hash)
		}
	}
}

// --- origin: the column is the seed door's, and only the seed door names it

func TestUpsertWritesSystemOriginAndOneAuthoredEvent(t *testing.T) {
	s := openStore(t)
	ap := NewApplier(s, testBoundary(s), fixed(2026, 11, 1, 12))
	ctx := context.Background()
	rec := Source()[0]
	if err := s.SeedWithTx(ctx, func(tx *sql.Tx) error {
		return ap.Upsert(ctx, tx, rec, false)
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	var row store.PolicyRow
	var present bool
	readTx(t, s, func(tx *sql.Tx) error {
		var err error
		row, present, err = s.PolicySeedRow(ctx, tx, rec.Key)
		return err
	})
	if !present {
		t.Fatalf("read back: absent")
	}
	if row.Origin != policy.OriginSystem {
		t.Fatalf("a seeded row carries origin %q; the seed process writes 'system' (PM15)", row.Origin)
	}
	// One authored audit event, subject = the target, policy_id = the rule.
	p, _ := policy.DecodePolicy(rec.Content)
	events, err := s.PolicyEventQuery(ctx, p.Target.String(), 10)
	if err != nil {
		t.Fatalf("event query: %v", err)
	}
	authored := 0
	for _, e := range events {
		if e.Outcome == "authored" && e.PolicyID == rec.Key {
			authored++
		}
	}
	if authored != 1 {
		t.Fatalf("the seeded row carries %d authored events; the boundary pairs exactly one", authored)
	}
}

// TestPlainAddLeavesTheColumnDefault is the other half of PM15's honesty:
// the plain door names no origin, the DEFAULT answers 'user'.
func TestPlainAddLeavesTheColumnDefault(t *testing.T) {
	s := openStore(t)
	b := testBoundary(s)
	p := policy.Policy{
		ID: "hand-authored", Type: policy.CommandAccess,
		Target: policy.Target{Kind: policy.AgentKind, ID: "someone"},
		Action: policy.Deny, Spec: []byte(`{"command":"rm*","state":"deny"}`),
		Source: policy.Operator,
	}
	if rej := b.Add(context.Background(), p, time.Now()); rej != nil {
		t.Fatalf("add: %s", rej.Reason)
	}
	var row store.PolicyRow
	var present bool
	readTx(t, s, func(tx *sql.Tx) error {
		var err error
		row, present, err = s.PolicySeedRow(context.Background(), tx, "hand-authored")
		return err
	})
	if !present {
		t.Fatalf("read back: absent")
	}
	if row.Origin != policy.OriginUser {
		t.Fatalf("origin %q; a hand-authored row is 'user' by the column default", row.Origin)
	}
}

// TestUpsertRefusesToOverwriteAnOperatorsRow is ErrConflict: a present row
// that is not a system row is the operator's, and the seed walks away.
func TestUpsertRefusesToOverwriteAnOperatorsRow(t *testing.T) {
	s := openStore(t)
	ap := NewApplier(s, testBoundary(s), fixed(2026, 11, 1, 12))
	ctx := context.Background()
	rec := Source()[0]
	p, _ := policy.DecodePolicy(rec.Content)
	// The old verb's row: same id, boundary Add (origin default user).
	if rej := testBoundary(s).Add(ctx, p, time.Now()); rej != nil {
		t.Fatalf("plain add: %s", rej.Reason)
	}
	err := s.SeedWithTx(ctx, func(tx *sql.Tx) error {
		return ap.Upsert(ctx, tx, rec, false) // adopt=false: not adoptable-by-fiat
	})
	if err == nil || !strings.Contains(err.Error(), "never overwritten") {
		t.Fatalf("err = %v; the seed must refuse an operator-authored row (ErrConflict)", err)
	}
}

// TestAdoptFillsTheOldVerbsRow is SD9's policy shape: a present row the
// content already equals (the CLI verb's, origin user, no ledger row) is
// ADOPTABLE, and the adopt keeps the row's created_at — the id was always
// the same — while the origin flips to system through the engine's
// delete-then-insert.
func TestAdoptFillsTheOldVerbsRow(t *testing.T) {
	s := openStore(t)
	ap := NewApplier(s, testBoundary(s), fixed(2026, 11, 1, 12))
	ctx := context.Background()
	rec := Source()[1]
	p, _ := policy.DecodePolicy(rec.Content)
	old := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	if rej := testBoundary(s).Add(ctx, p, old); rej != nil {
		t.Fatalf("plain add: %s", rej.Reason)
	}
	var adoptable bool
	readTx(t, s, func(tx *sql.Tx) error {
		var err error
		adoptable, err = ap.Adoptable(ctx, tx, rec.Key)
		return err
	})
	if !adoptable {
		t.Fatalf("adoptable = %v; a content-equal present row is adoptable", adoptable)
	}
	if err := s.SeedWithTx(ctx, func(tx *sql.Tx) error {
		return ap.Upsert(ctx, tx, rec, true)
	}); err != nil {
		t.Fatalf("adopt: %v", err)
	}
	var row store.PolicyRow
	var present bool
	readTx(t, s, func(tx *sql.Tx) error {
		var err error
		row, present, err = s.PolicySeedRow(ctx, tx, rec.Key)
		return err
	})
	if !present {
		t.Fatalf("read back: absent")
	}
	if row.Origin != policy.OriginSystem {
		t.Fatalf("origin %q after adopt; the ledger now says the seed owns this row", row.Origin)
	}
	if row.CreatedAt != old.UTC().Format(time.RFC3339Nano) {
		t.Fatalf("created_at %q drifted on adopt; the row keeps its written-at instant", row.CreatedAt)
	}
}

// TestAdoptRefusesADivergedRow: content equality is the whole test — an
// operator's hand edit of the same id is a conflict the plan skips, not a
// row the seed adopts.
func TestAdoptRefusesADivergedRow(t *testing.T) {
	s := openStore(t)
	ap := NewApplier(s, testBoundary(s), fixed(2026, 11, 1, 12))
	ctx := context.Background()
	rec := Source()[2]
	p, _ := policy.DecodePolicy(rec.Content)
	p.Spec = []byte(`{"command":"touch*","state":"allow"}`) // the hand edit
	if p.Action == policy.Allow {
		p.Spec = []byte(`{"command":"touch*","state":"allow"}`)
	} else {
		p.Spec = []byte(`{"command":"touch*","state":"deny"}`)
	}
	if rej := testBoundary(s).Add(ctx, p, time.Now()); rej != nil {
		t.Fatalf("plain add: %s", rej.Reason)
	}
	var adoptable bool
	readTx(t, s, func(tx *sql.Tx) error {
		var err error
		adoptable, err = ap.Adoptable(ctx, tx, rec.Key)
		return err
	})
	if adoptable {
		t.Fatal("a hand-edited row reported adoptable; the engine would adopt over the operator's edit")
	}
}

// TestUpdateKeepsTheWrittenInstant: a system row re-upserted (the version
// bump path) replaces through delete-then-insert on one transaction and
// carries the ORIGINAL created_at — a re-version does not forge a new
// authoring instant.
func TestUpdateKeepsTheWrittenInstant(t *testing.T) {
	s := openStore(t)
	first := NewApplier(s, testBoundary(s), fixed(2026, 10, 1, 8))
	later := NewApplier(s, testBoundary(s), fixed(2026, 11, 2, 9))
	ctx := context.Background()
	rec := Source()[3]
	if err := s.SeedWithTx(ctx, func(tx *sql.Tx) error {
		return first.Upsert(ctx, tx, rec, false)
	}); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	if err := s.SeedWithTx(ctx, func(tx *sql.Tx) error {
		return later.Upsert(ctx, tx, rec, false)
	}); err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	var row store.PolicyRow
	readTx(t, s, func(tx *sql.Tx) error {
		var err error
		var present bool
		row, present, err = s.PolicySeedRow(ctx, tx, rec.Key)
		if err == nil && !present {
			t.Fatalf("row absent")
		}
		return err
	})
	if want := fixed(2026, 10, 1, 8)().Format(time.RFC3339Nano); row.CreatedAt != want {
		t.Fatalf("created_at %q, want %q — the update must not forge a new instant", row.CreatedAt, want)
	}
}

// TestRetireKeepsTheRow is SD6's policy half: retirement flags the LEDGER
// (the engine's own row), and the policy row stands — the permission an
// operator may still rely on is never withdrawn by a record leaving code.
func TestRetireKeepsTheRow(t *testing.T) {
	s := openStore(t)
	ap := NewApplier(s, testBoundary(s), fixed(2026, 11, 1, 12))
	ctx := context.Background()
	rec := Source()[4]
	if err := s.SeedWithTx(ctx, func(tx *sql.Tx) error {
		return ap.Upsert(ctx, tx, rec, false)
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := s.SeedWithTx(ctx, func(tx *sql.Tx) error {
		return ap.Retire(ctx, tx, rec.Key)
	}); err != nil {
		t.Fatalf("retire: %v", err)
	}
	var present bool
	readTx(t, s, func(tx *sql.Tx) error {
		var err error
		_, present, err = s.PolicySeedRow(ctx, tx, rec.Key)
		return err
	})
	if !present {
		t.Fatal("retire deleted the row — retirement keeps rows")
	}
}

// --- helpers ---------------------------------------------------------------

func fixed(y, m, d, h int) func() time.Time {
	t := time.Date(y, time.Month(m), d, h, 0, 0, 0, time.UTC)
	return func() time.Time { return t }
}

// readTx runs fn on a short live transaction: the applier's reads
// (Fingerprint, Adoptable) and the store's seed-row read all want the
// store's own *sql.Tx, and this is the one way a test opens one honestly.
func readTx(t *testing.T, s *store.Store, fn func(tx *sql.Tx) error) {
	t.Helper()
	if err := s.SeedWithTx(context.Background(), fn); err != nil {
		t.Fatalf("read tx: %v", err)
	}
}
