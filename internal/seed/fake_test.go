package seed

import (
	"context"
)

// fakeApplier is a test applier with an injectable guard-affecting flag and
// operator rights. It records the calls made to it so an engine test can
// assert what a domain did. The production appliers (T4) are over a real
// store; this one is not.
type fakeApplier struct {
	setID          string
	guardAffecting bool
	operator       OperatorRights

	fingerprints map[string]FakeRow
	upserts      int
	retires      int
}

// FakeRow is the fake applier's view of a live row for a key.
type FakeRow struct {
	Hash     string
	Present  bool
	Adoptable bool
}

func newFakeApplier(setID string, guard bool) *fakeApplier {
	return &fakeApplier{
		setID:        setID,
		guardAffecting: guard,
		fingerprints: map[string]FakeRow{},
	}
}
func (f *fakeApplier) Set() string            { return f.setID }
func (f *fakeApplier) GuardAffecting() bool   { return f.guardAffecting }
func (f *fakeApplier) Operator() OperatorRights { return f.operator }
func (f *fakeApplier) Fingerprint(_ Tx, key string) (string, bool, error) {
	r, ok := f.fingerprints[key]
	if !ok {
		return "", false, nil
	}
	return r.Hash, r.Present, nil
}
func (f *fakeApplier) Adoptable(_ Tx, key string) (bool, error) {
	r, ok := f.fingerprints[key]
	if !ok {
		return false, nil
	}
	return r.Adoptable, nil
}
func (f *fakeApplier) Upsert(_ Tx, _ Record, _ bool) error {
	f.upserts++
	return nil
}
func (f *fakeApplier) Retire(_ Tx, _ string) error {
	f.retires++
	return nil
}

// fakeResult is a no-op Tx.Result for the fake applier's Upsert/Retire.
type fakeResult struct{}

func (fakeResult) LastInsertId() (int64, error) { return 0, nil }
func (fakeResult) RowsAffected() (int64, error) { return 0, nil }

// fakeRows is a no-op Tx.Rows for the fake applier's Fingerprint reads.
type fakeRows struct{}

func (fakeRows) Next() bool          { return false }
func (fakeRows) Scan(...any) error   { return nil }
func (fakeRows) Close() error        { return nil }

// fakeTx is a no-op Tx for the fake applier; it records nothing and never
// fails, so an engine test against it is about the engine's logic, not the
// SQL.
type fakeTx struct {
	execCount int
}
func (f *fakeTx) ExecContext(_ context.Context, _ string, _ ...any) (Result, error) {
	f.execCount++
	return fakeResult{}, nil
}
func (f *fakeTx) QueryContext(_ context.Context, _ string, _ ...any) (Rows, error) {
	return fakeRows{}, nil
}
