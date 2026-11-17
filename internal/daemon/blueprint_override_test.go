package daemon

import (
	"context"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
)

// A shipped blueprint is locked; the route a lane runs on is the one thing a
// person sets beside it. These pin the door's refusals (each before a row
// moves) and that the blueprint's own bytes never change.

func overrideEnv(t *testing.T) (*blueprintOverrides, *blueprintWrites, context.Context) {
	t.Helper()
	_, s, w := bpWritesEnv(t)
	return &blueprintOverrides{store: s, routes: func() []string { return []string{"fast", "smart"} }}, w, context.Background()
}

func TestOverrideSetsReplacesAndClearsARoute(t *testing.T) {
	o, _, ctx := overrideEnv(t)
	p := protocol.BlueprintOverrideParams{Name: "two-step", Lane: "survey", Field: "route", Value: "fast"}
	res, err := o.Override(ctx, bpActor, p)
	if err != nil || !res.Changed {
		t.Fatalf("first set: %+v %v", res, err)
	}
	if res, _ = o.Override(ctx, bpActor, p); res.Changed {
		t.Fatal("setting the same value again reported a change")
	}
	p.Value = "smart"
	if res, err = o.Override(ctx, bpActor, p); err != nil || !res.Changed {
		t.Fatalf("replace: %+v %v", res, err)
	}
	rows, _ := o.store.BlueprintOverrides(ctx, "two-step")
	if len(rows) != 1 || rows[0].Value != "smart" {
		t.Fatalf("rows after replace: %+v", rows)
	}
	p.Value = ""
	if res, err = o.Override(ctx, bpActor, p); err != nil || !res.Changed {
		t.Fatalf("clear: %+v %v", res, err)
	}
	if rows, _ = o.store.BlueprintOverrides(ctx, "two-step"); len(rows) != 0 {
		t.Fatalf("clear left rows: %+v", rows)
	}
}

func TestOverrideRefusesWhatIsNotAllowedBeforeAnyRowMoves(t *testing.T) {
	o, w, ctx := overrideEnv(t)
	if _, err := w.Duplicate(ctx, bpActor, protocol.BlueprintDuplicateParams{From: "two-step", Name: "mine"}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		kind string
		p    protocol.BlueprintOverrideParams
	}{
		{"from-unknown", protocol.BlueprintOverrideParams{Name: "nope", Lane: "survey", Field: "route", Value: "fast"}},
		{"not-locked", protocol.BlueprintOverrideParams{Name: "mine", Lane: "survey", Field: "route", Value: "fast"}},
		{"lane-unknown", protocol.BlueprintOverrideParams{Name: "two-step", Lane: "ghost", Field: "route", Value: "fast"}},
		{"not-overridable", protocol.BlueprintOverrideParams{Name: "two-step", Lane: "survey", Field: "agent", Value: "x"}},
		{"not-overridable", protocol.BlueprintOverrideParams{Name: "two-step", Lane: "approve", Field: "route", Value: "fast"}},
		{"route-unknown", protocol.BlueprintOverrideParams{Name: "two-step", Lane: "survey", Field: "route", Value: "missing"}},
	} {
		_, err := o.Override(ctx, bpActor, c.p)
		mustRefused(t, err, c.kind)
	}
	for _, name := range []string{"two-step", "mine"} {
		if rows, _ := o.store.BlueprintOverrides(ctx, name); len(rows) != 0 {
			t.Fatalf("a refused override wrote rows for %s: %+v", name, rows)
		}
	}
}

func TestAnOverrideNeverChangesTheShippedBytes(t *testing.T) {
	o, w, ctx := overrideEnv(t)
	before, _, _ := o.store.BlueprintGet(ctx, "two-step")
	if _, err := o.Override(ctx, bpActor, protocol.BlueprintOverrideParams{Name: "two-step", Lane: "survey", Field: "route", Value: "fast"}); err != nil {
		t.Fatal(err)
	}
	after, _, _ := o.store.BlueprintGet(ctx, "two-step")
	if after.ContentHash != before.ContentHash || after.UpdatedAt != before.UpdatedAt {
		t.Fatal("an override moved the shipped blueprint's row")
	}
	_, err := w.Revert(ctx, bpActor, protocol.BlueprintRevertParams{Name: "two-step", Rev: 1})
	mustRefused(t, err, "system")
}
