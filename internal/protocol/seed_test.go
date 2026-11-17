package protocol

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/cjlapao/aeon/internal/seed"
)

// seed_test.go — S43-extra T6 (SD10): the four seed.* methods, tested over a
// fake SeedService (the same recipe settings_test uses). A fake that bypassed
// the engine would test only the mapping, not the door; but the protocol
// package cannot import the daemon (the daemon imports the protocol, a
// cycle), so the fake stands in for the engine's one *seed.Engine the daemon
// holds. The real engine over a real store is exercised in
// internal/daemon/seed_test.go; this file exercises the handler, the mapping,
// the actor-from-the-connection guard, the nil-safety and the error mapping.

// seedFake is an in-memory SeedService. It records the actor it is handed (so
// a test can assert the write came from the connection, not params) and the
// path (so a test can assert apply took the explicit door).
type seedFake struct {
	statuses     []seed.SetStatus
	plan         seed.SetPlan
	statusErr    error
	planErr      error
	applyErr     error
	restoreErr   error

	applyCalled  bool
	restoreCalled bool
	applyActor   seed.Actor
	applyVia     seed.Path
	restoreActor seed.Actor
}

func (f *seedFake) Status(_ context.Context, _ string) ([]seed.SetStatus, error) {
	return f.statuses, f.statusErr
}

func (f *seedFake) Plan(_ context.Context, _ string, _ []string) (seed.SetPlan, error) {
	return f.plan, f.planErr
}

func (f *seedFake) Apply(_ context.Context, setID string, _ []string, by seed.Actor, via seed.Path) (seed.ApplyResult, error) {
	f.applyCalled = true
	f.applyActor = by
	f.applyVia = via
	return seed.ApplyResult{
		SetID:   setID,
		Records: []seed.RecordResult{{Key: "a", Op: seed.OpApply, ToVersion: 1, Reason: "apply — a new record"}},
	}, f.applyErr
}

func (f *seedFake) Restore(_ context.Context, _ string, _ string, by seed.Actor) (seed.ApplyResult, error) {
	f.restoreCalled = true
	f.restoreActor = by
	return seed.ApplyResult{
		SetID:   "agents.firstparty",
		Records: []seed.RecordResult{{Key: "a", Op: seed.OpRestore, ToVersion: 1, Reason: "restore"}},
	}, f.restoreErr
}

// seedTable stands up a Table with the fake wired and an actor stamped from
// the connection (the daemon's ActorFor seam).
func seedTable(t *testing.T, actor Actor) (*Table, *seedFake) {
	t.Helper()
	fake := &seedFake{}
	tbl := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	tbl.Seed = fake
	if !tbl.RegisterSeed() {
		t.Fatal("RegisterSeed refused a wired seam")
	}
	if actor.Kind != "" || actor.ID != "" {
		tbl.ActorFor = func(_ context.Context, _ *Session) Actor { return actor }
	}
	initialized(t, tbl, "c1", PROTOCOL_VERSION)
	return tbl, fake
}

// dispatchSeed sends one seed call through the door.
func dispatchSeed(t *testing.T, tbl *Table, m MethodName, params any) (any, error) {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	return tbl.Dispatch(context.Background(), "c1", m, raw)
}

// TestSeedStatusMapsToWire is the read half: a status read maps the engine's
// view to the wire twin, field for field, and writes nothing (the fake records
// no write — status is a read, SD10).
func TestSeedStatusMapsToWire(t *testing.T) {
	fake := &seedFake{
		statuses: []seed.SetStatus{{
			ID:               "agents.firstparty",
			Class:            seed.ClassInert,
			AvailableVersion: 3,
			AppliedVersion:   3,
			Records: []seed.RecordStatus{{
				Key:              "coder",
				State:            seed.StateApplied,
				AvailableVersion: 3,
				AppliedVersion:   3,
				Reason:           "up to date",
			}},
		}},
	}
	tbl := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	tbl.Seed = fake
	if !tbl.RegisterSeed() {
		t.Fatal("RegisterSeed refused a wired seam")
	}
	initialized(t, tbl, "c1", PROTOCOL_VERSION)

	res, err := tbl.Dispatch(context.Background(), "c1", MethodSeedStatus, json.RawMessage(`{"set":"agents.firstparty"}`))
	if err != nil {
		t.Fatalf("seed.status: %v", err)
	}
	got, ok := res.(SeedStatusResult)
	if !ok {
		t.Fatalf("status result = %T, want SeedStatusResult", res)
	}
	if len(got.Sets) != 1 {
		t.Fatalf("status = %d sets, want 1", len(got.Sets))
	}
	s := got.Sets[0]
	if s.ID != "agents.firstparty" || s.Class != "inert" || s.AvailableVersion != 3 || s.AppliedVersion != 3 {
		t.Fatalf("status set = %+v", s)
	}
	if len(s.Records) != 1 {
		t.Fatalf("status = %d records, want 1", len(s.Records))
	}
	r := s.Records[0]
	if r.Key != "coder" || r.State != "applied" || r.AvailableVersion != 3 || r.AppliedVersion != 3 || r.Reason != "up to date" {
		t.Fatalf("status record = %+v", r)
	}
}

// TestSeedApplyTakesActorFromConnection is the human-door guard (SD10): the
// actor comes from the connection (the daemon's ActorFor seam), never from
// params, and the apply takes the explicit path.
func TestSeedApplyTakesActorFromConnection(t *testing.T) {
	tbl, fake := seedTable(t, Actor{Kind: "user", ID: "1000"})

	res, err := dispatchSeed(t, tbl, MethodSeedApply, SeedApplyParams{Set: "agents.firstparty"})
	if err != nil {
		t.Fatalf("seed.apply: %v", err)
	}
	if !fake.applyCalled {
		t.Fatal("apply never reached the seam")
	}
	if fake.applyActor.Kind != "user" || fake.applyActor.ID != "1000" {
		t.Fatalf("apply actor = %+v, want the connection's user:1000", fake.applyActor)
	}
	if fake.applyVia != seed.PathExplicit {
		t.Fatalf("apply via = %q, want explicit (the human door)", fake.applyVia)
	}
	got, ok := res.(SeedApplyResult)
	if !ok {
		t.Fatalf("apply result = %T, want SeedApplyResult", res)
	}
	if got.RequiresConfirmation {
		t.Fatal("apply must not claim a confirmation — the daemon cannot prove a human is present (SD11)")
	}
}

// TestSeedApplyNoActorRefuses is the write boundary (SD10): an unresolvable
// peer is refused (ErrSeedNoActor), never stamped with a placeholder, and
// nothing is written.
func TestSeedApplyNoActorRefuses(t *testing.T) {
	tbl, fake := seedTable(t, Actor{Kind: "user", ID: ""}) // an empty id is refused

	_, err := dispatchSeed(t, tbl, MethodSeedApply, SeedApplyParams{Set: "agents.firstparty"})
	if !errors.Is(err, ErrSeedNoActor) {
		t.Fatalf("seed.apply with no actor = %v, want ErrSeedNoActor", err)
	}
	if fake.applyCalled {
		t.Fatal("apply reached the seam without an actor — the write was not refused")
	}
}

// TestSeedApplyNilActorRefuses is the V13 twin: a daemon with no ActorFor (no
// persons) refuses a seed write rather than stamping a placeholder.
func TestSeedApplyNilActorRefuses(t *testing.T) {
	tbl := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	tbl.Seed = &seedFake{}
	if !tbl.RegisterSeed() {
		t.Fatal("RegisterSeed refused a wired seam")
	}
	// no ActorFor
	initialized(t, tbl, "c1", PROTOCOL_VERSION)

	_, err := dispatchSeed(t, tbl, MethodSeedApply, SeedApplyParams{Set: "agents.firstparty"})
	if !errors.Is(err, ErrSeedNoActor) {
		t.Fatalf("seed.apply with a nil ActorFor = %v, want ErrSeedNoActor", err)
	}
}

// TestSeedErrorMapping is the error-mapping half: the engine's untyped
// refusals map to the protocol's two codes, verbatim (the engine's own
// sentence, via WithWhat). The mapped errors are *Error copies, so the code
// (not the pointer) is the match — the settings_test convention.
func TestSeedErrorMapping(t *testing.T) {
	tbl, _ := seedTable(t, Actor{Kind: "user", ID: "1000"})

	// an unknown set → ErrSeedUnknownSet
	{
		f := &seedFake{}
		f.statusErr = errors.New(`seed: set "nope" is not declared`)
		tbl.Seed = f
		_, err := tbl.Dispatch(context.Background(), "c1", MethodSeedStatus, json.RawMessage(`{"set":"nope"}`))
		var e *Error
		if !errors.As(err, &e) || e.Code != ErrSeedUnknownSet.Code {
			t.Fatalf("unknown set = %v, want ErrSeedUnknownSet (-31830)", err)
		}
	}
	// a plan refusal (a downgrade) → ErrSeedRefused
	{
		f := &seedFake{}
		f.applyErr = errors.New(`seed: apply of "agents.firstparty" refused: a lower version than applied`)
		tbl.Seed = f
		_, err := dispatchSeed(t, tbl, MethodSeedApply, SeedApplyParams{Set: "agents.firstparty"})
		var e *Error
		if !errors.As(err, &e) || e.Code != ErrSeedRefused.Code {
			t.Fatalf("refused apply = %v, want ErrSeedRefused (-31831)", err)
		}
	}
}

// TestSeedPlanIsReadOnly is the read half: a plan read maps the engine's
// decisions and refusal, and writes nothing (SD10).
func TestSeedPlanIsReadOnly(t *testing.T) {
	fake := &seedFake{
		plan: seed.SetPlan{
			SetID: "agents.firstparty",
			Decisions: []seed.Decision{{
				Key: "coder", Op: seed.OpUpdate, State: seed.StateApplied,
				FromVersion: 2, ToVersion: 3, Reason: "update — a higher version over an unmodified row",
			}},
			Refusal: seed.Refusal{Key: "reader", Reason: "a lower version than applied — a downgrade is never silent"},
		},
	}
	tbl := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	tbl.Seed = fake
	if !tbl.RegisterSeed() {
		t.Fatal("RegisterSeed refused a wired seam")
	}
	initialized(t, tbl, "c1", PROTOCOL_VERSION)

	res, err := dispatchSeed(t, tbl, MethodSeedPlan, SeedPlanParams{Set: "agents.firstparty"})
	if err != nil {
		t.Fatalf("seed.plan: %v", err)
	}
	got, ok := res.(SeedPlanResult)
	if !ok {
		t.Fatalf("plan result = %T, want SeedPlanResult", res)
	}
	if got.Plan.SetID != "agents.firstparty" || len(got.Plan.Decisions) != 1 {
		t.Fatalf("plan = %+v", got.Plan)
	}
	d := got.Plan.Decisions[0]
	if d.Key != "coder" || d.Op != "update" || d.ToVersion != 3 || d.Reason == "" {
		t.Fatalf("plan decision = %+v", d)
	}
	if got.Plan.Refusal.Key != "reader" || got.Plan.Refusal.Reason == "" {
		t.Fatalf("plan refusal = %+v", got.Plan.Refusal)
	}
}

// TestSeedMethodsAbsentWhenNil is the V13 configuration: a nil Seed leaves the
// four seed.* methods ABSENT — Dispatch answers the unknown-method error, not
// a refusal (the S22 search.query / S44 settings precedent).
func TestSeedMethodsAbsentWhenNil(t *testing.T) {
	tbl := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	if tbl.RegisterSeed() {
		t.Fatal("RegisterSeed registered with a nil seam — the four methods should be absent (V13)")
	}
	initialized(t, tbl, "c1", PROTOCOL_VERSION)
	for _, m := range []MethodName{MethodSeedStatus, MethodSeedPlan, MethodSeedApply, MethodSeedRestore} {
		if _, err := tbl.Dispatch(context.Background(), "c1", m, nil); err == nil {
			t.Fatalf("%s answered on a nil seam — the method should be absent", m)
		}
	}
}

// TestSeedMethodsAreRegisteredConditional is the positive side of V13: with a
// wired seam the four are registered, RequiresInitialize, and not Debug.
func TestSeedMethodsAreRegisteredConditional(t *testing.T) {
	tbl := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	tbl.Seed = &seedFake{}
	if !tbl.RegisterSeed() {
		t.Fatal("RegisterSeed refused a wired seam")
	}
	for _, m := range []MethodName{MethodSeedStatus, MethodSeedPlan, MethodSeedApply, MethodSeedRestore} {
		e, ok := tbl.methods[m]
		if !ok {
			t.Fatalf("%s was not registered with a wired seam", m)
		}
		if !e.RequiresInitialize {
			t.Fatalf("%s should RequireInitialize (it reads/writes the store)", m)
		}
		if e.Debug {
			t.Fatalf("%s is Debug — a seed method is a product surface, not a debug door", m)
		}
	}
}
