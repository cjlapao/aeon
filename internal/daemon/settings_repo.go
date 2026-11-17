package daemon

// settings_repo.go — S44 T4: the adapter behind config.Repo, over the real
// *store.Store. A pure mapping (CS2): no domain rule, no SQL, no validation —
// those live in config (the closed registry and its typing, CS3) and store
// (the SQL, I2). This is the bridge T0 item 3.3 named: the daemon is the one
// layer that may know both, and it maps config's Repo seam to the store's SQL
// half (T1) field for field.
//
// A nil store yields a nil Repo (V13: absent, not registered-and-refusing),
// following newTelemetryReadStore's convention — a storeless daemon leaves
// settings.* unregistered rather than answering a refusal, exactly as the
// search doors do.
//
// The mapping is total and lossless. config.RepoRow/RepoEvent and
// store.SettingRow/SettingEvent share the same fields, so each is copied field
// for field. *string stays *string (nil stays nil — the two NULLs, CS8), bool
// stays bool, strings verbatim. No field is dropped and nil is never
// conflated with "". A caller who skipped the config boundary still cannot
// write a malformed key or an over-long value (the store's CHECK is the net),
// but the good message stays upstairs (the same division policy.go documents).

import (
	"context"

	"github.com/cjlapao/aeon/internal/config"
	"github.com/cjlapao/aeon/internal/store"
)

// settingsRepo is the config.Repo over a real *store.Store. It holds no
// domain rule and no SQL (CS2): it is the seam the daemon owns.
type settingsRepo struct {
	st *store.Store
}

// newSettingsRepo follows newTelemetryReadStore's convention: a nil store
// yields a nil Repo (the typed-nil trap avoided by returning the interface
// only through this function), so a storeless daemon leaves settings.*
// unregistered (V13: absent, not registered-and-refusing).
func newSettingsRepo(s *store.Store) config.Repo {
	if s == nil {
		return nil
	}
	return &settingsRepo{st: s}
}

// SettingGet reads one value. Absence is a normal answer (false), not a fault
// (DB19); a repo read error is returned, never turned into a default (P31).
func (r *settingsRepo) SettingGet(ctx context.Context, key string) (config.RepoRow, bool, error) {
	row, ok, err := r.st.SettingGet(ctx, key)
	if err != nil {
		return config.RepoRow{}, false, err
	}
	return repoRowFrom(row), ok, nil
}

// SettingList reads every stored value, ordered by key (the store's order is
// the UI's, so it is stable — I6).
func (r *settingsRepo) SettingList(ctx context.Context) ([]config.RepoRow, error) {
	rows, err := r.st.SettingList(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]config.RepoRow, len(rows))
	for i, row := range rows {
		out[i] = repoRowFrom(row)
	}
	return out, nil
}

// SettingPut upserts the row AND appends ev in one transaction (CS8). The
// event's OldValue is filled by the store from the row it replaced, inside
// the transaction — so the audit trail can never name a change the table did
// not make. The mapping is lossless: the event the config layer built is the
// event the store writes, field for field.
func (r *settingsRepo) SettingPut(ctx context.Context, row config.RepoRow, ev config.RepoEvent) error {
	return r.st.SettingPut(ctx, settingRowFrom(row), settingEventFrom(ev))
}

// SettingDelete removes the row and appends ev ("unset") in one transaction.
// A key with no row returns (false, nil) and writes no event (CS8); the store
// fills ev.OldValue from the row it removed, inside the transaction.
func (r *settingsRepo) SettingDelete(ctx context.Context, key string, ev config.RepoEvent) (bool, error) {
	return r.st.SettingDelete(ctx, key, settingEventFrom(ev))
}

// SettingEventList reads the audit trail, newest first, clamped by the store
// to [1,500] (default 50). The mapping is lossless: each event is copied
// field for field, *string in and out.
func (r *settingsRepo) SettingEventList(ctx context.Context, key string, limit int) ([]config.RepoEvent, error) {
	evs, err := r.st.SettingEventList(ctx, key, limit)
	if err != nil {
		return nil, err
	}
	out := make([]config.RepoEvent, len(evs))
	for i, ev := range evs {
		out[i] = repoEventFrom(ev)
	}
	return out, nil
}

// --- the total, lossless mapping -------------------------------------------
//
// Each pair shares its fields, so a field-by-field copy is the whole of the
// mapping. The drift detector is the compile itself: rename a field on one
// side and this stops compiling, exactly as the telemetry adapter's sums
// conversion relies on the identical field sets.

// repoRowFrom maps a store row to the seam's row.
func repoRowFrom(s store.SettingRow) config.RepoRow {
	return config.RepoRow{Key: s.Key, Value: s.Value, UpdatedAt: s.UpdatedAt, UpdatedBy: s.UpdatedBy}
}

// settingRowFrom maps the seam's row to a store row.
func settingRowFrom(c config.RepoRow) store.SettingRow {
	return store.SettingRow{Key: c.Key, Value: c.Value, UpdatedAt: c.UpdatedAt, UpdatedBy: c.UpdatedBy}
}

// repoEventFrom maps a store event to the seam's event. OldValue/NewValue are
// *string: nil stays nil (the two NULLs, CS8), and a non-nil pointer is copied
// by value, not shared — a read-back must never alias the store's row.
func repoEventFrom(s store.SettingEvent) config.RepoEvent {
	return config.RepoEvent{
		ID:         s.ID,
		Key:        s.Key,
		Op:         s.Op,
		OldValue:   derefString(s.OldValue),
		NewValue:   derefString(s.NewValue),
		Sensitive:  s.Sensitive,
		ShadowedBy: s.ShadowedBy,
		Actor:      s.Actor,
		OccurredAt: s.OccurredAt,
	}
}

// settingEventFrom maps the seam's event to a store event. It copies the
// pointers by value (never by alias) so the store's own fill of OldValue inside
// its transaction cannot be seen by the config layer that built the event.
func settingEventFrom(c config.RepoEvent) store.SettingEvent {
	return store.SettingEvent{
		ID:         c.ID,
		Key:        c.Key,
		Op:         c.Op,
		OldValue:   derefString(c.OldValue),
		NewValue:   derefString(c.NewValue),
		Sensitive:  c.Sensitive,
		ShadowedBy: c.ShadowedBy,
		Actor:      c.Actor,
		OccurredAt: c.OccurredAt,
	}
}

// derefString copies a *string by value: nil stays nil (the NULL), a non-nil
// pointer becomes a new pointer to a copy, so the mapping is lossless and never
// aliases the source. It is the one place nil is decided, and it decides it
// once.
func derefString(p *string) *string {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
