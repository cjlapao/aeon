package seed

import (
	"context"
	"fmt"
)

// fakeRepo is an in-memory Repo for engine tests: it records writes (for the
// "zero writes" assertions), can pre-seed ledger rows and inject a failure
// into the domain Upsert. It is the seam T6's daemon would implement over
// internal/store; here it holds no SQL.
type fakeRepo struct {
	ledger   map[string]LedgerRow
	events   []Event
	tx       Tx
	writes   int
	failUpsert map[string]bool // keys whose Upsert fails
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		ledger:       map[string]LedgerRow{},
		tx:           &fakeTx{},
		failUpsert:   map[string]bool{},
	}
}

func (r *fakeRepo) Ledger(_ context.Context, setID string) ([]LedgerRow, error) {
	var out []LedgerRow
	for _, row := range r.ledger {
		if setID == "" || row.SetID == setID {
			out = append(out, row)
		}
	}
	return out, nil
}
func (r *fakeRepo) LedgerGet(_ context.Context, setID, key string) (LedgerRow, bool, error) {
	row, ok := r.ledger[keyOf(setID, key)]
	return row, ok, nil
}
func (r *fakeRepo) Events(_ context.Context, setID, key string, limit int) ([]Event, error) {
	var out []Event
	for i := len(r.events) - 1; i >= 0; i-- {
		e := r.events[i]
		if (setID == "" || e.SetID == setID) && (key == "" || e.RecordKey == key) {
			out = append(out, e)
			if limit > 0 && len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}
func (r *fakeRepo) WithTx(_ context.Context, fn func(tx Tx) error) error {
	return fn(r.tx)
}
func (r *fakeRepo) PutLedger(_ Tx, row LedgerRow) error {
	r.writes++
	r.ledger[keyOf(row.SetID, row.RecordKey)] = row
	return nil
}
func (r *fakeRepo) InsertEvent(_ Tx, ev Event) error {
	r.writes++
	r.events = append(r.events, ev)
	return nil
}
func (r *fakeRepo) failUpsertFor(key string) { r.failUpsert[key] = true }

func keyOf(setID, key string) string { return setID + "/" + key }

// a fakeTx that can fail an Upsert for a named key (to force a record
// failure, exercising the failed-marking path).
type failingTx struct {
	failUpsert map[string]bool
}

func (f failingTx) ExecContext(_ context.Context, _ string, _ ...any) (Result, error) { return fakeResult{}, nil }
func (f failingTx) QueryContext(_ context.Context, _ string, _ ...any) (Rows, error) { return fakeRows{}, nil }

// an applier that fails its Upsert for a named key, so the engine's apply
// transaction fails and the record is marked failed.
type failingApplier struct {
	*fakeApplier
	failUpsert map[string]bool
}

func (a failingApplier) Upsert(_ Tx, r Record, _ bool) error {
	if a.failUpsert[r.Key] {
		return fmt.Errorf("domain upsert refused %q", r.Key)
	}
	a.fakeApplier.upserts++
	return nil
}
