package daemon

// intake_vocab_t11_test.go — S61 T11: the wizard's descriptor proved
// daemon-side. Every set comes from the package that ENFORCES it; the
// remote-create gate answers as the three states the read can be in,
// each with its own sentence; the public statement names visibility in
// its own words; and routeAvailable tells the truth about routes, so the
// wizard never opens into a refusal the back door was always going to
// speak (the lying descriptor T11 names).

import (
	"context"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/intake"
	"github.com/cjlapao/aeon/internal/model"
)

func TestIntakeVocabularyFromGoSets(t *testing.T) {
	st, cfg := openS61Store(t)
	mustTemplate(t, st, "base")
	ctx := context.Background()
	id := newIntakeDoors(&Daemon{store: st, settings: cfg})
	v, err := id.Vocabulary(ctx)
	if err != nil {
		t.Fatalf("intake.vocabulary: %v", err)
	}
	if len(v.Publish) != len(intake.PublishModes) {
		t.Fatalf("publish answers %d choices, the engine's closed set is %d — typed twice?", len(v.Publish), len(intake.PublishModes))
	}
	for i, pc := range v.Publish {
		if pc.Value != intake.PublishModes[i] {
			t.Errorf("publish[%d] = %q, the engine's set says %q", i, pc.Value, intake.PublishModes[i])
		}
		if pc.Label != intake.PublishLabel(pc.Value) {
			t.Errorf("%q label key %q drifted from the engine's %q", pc.Value, pc.Label, intake.PublishLabel(pc.Value))
		}
		if pc.Statement != intake.PublishStatement(pc.Value) {
			t.Errorf("%q statement drifted from the daemon's own text", pc.Value)
		}
	}
	if strings.Join(v.Roles, ",") != strings.Join(desk.IntakeRoles, ",") {
		t.Errorf("roles %v, want the desk validator's %v", v.Roles, desk.IntakeRoles)
	}
	caps := desk.IntakePlanCapsOf()
	if v.Caps.Components != caps.Components || v.Caps.Goals != caps.Goals || v.Caps.Items != caps.Items {
		t.Errorf("caps %+v drifted from the validator's %+v", v.Caps, caps)
	}
	rows, err := st.ListTemplates()
	if err != nil || len(rows) != len(v.Templates) {
		t.Fatalf("templates %d vs the store's %d rows (%v) — the palette is the store's, live", len(v.Templates), len(rows), err)
	}
	for i, tpl := range v.Templates {
		if tpl.Slug != rows[i].Slug || tpl.Version != rows[i].Version || tpl.Origin != rows[i].Origin {
			t.Errorf("template %d = %s/v%d/%s, the store says %s/v%d/%s", i, tpl.Slug, tpl.Version, tpl.Origin, rows[i].Slug, rows[i].Version, rows[i].Origin)
		}
	}
}

func TestRemoteCreateEnabledReflectsTheThreeKeys(t *testing.T) {
	// The gate has exactly three honest answers — seam unwired, gate off,
	// gate on — and each false one carries the SAME sentence the action
	// itself would speak. A descriptor claiming a door the action would
	// refuse is the lying descriptor T11 was written against.
	st, _ := openS61Store(t)
	ctx := context.Background()

	nilSeam := newIntakeDoors(&Daemon{store: st})
	v, err := nilSeam.Vocabulary(ctx)
	if err != nil {
		t.Fatalf("vocabulary over an unwired settings seam: %v", err)
	}
	if v.RemoteCreateEnabled || !strings.Contains(v.RemoteCreateReason, "not wired") {
		t.Errorf("unwired seam answered %v / %q — an unwired seam is the default FALSE, and must say so", v.RemoteCreateEnabled, v.RemoteCreateReason)
	}

	st2, cfg := openS61Store(t)
	off := newIntakeDoors(&Daemon{store: st2, settings: cfg})
	v, err = off.Vocabulary(ctx)
	if err != nil {
		t.Fatalf("vocabulary with the gate off: %v", err)
	}
	if v.RemoteCreateEnabled || !strings.Contains(v.RemoteCreateReason, "switch it on") {
		t.Errorf("gate off answered %v / %q — the person's own switch is named, or the reason lies", v.RemoteCreateEnabled, v.RemoteCreateReason)
	}

	if _, b, err := cfg.Set(ctx, allowRemoteCreateSetting, "true", "user:tester"); err != nil || !b {
		t.Fatalf("set the gate: %v %v", b, err)
	}
	v, err = off.Vocabulary(ctx)
	if err != nil || !v.RemoteCreateEnabled || v.RemoteCreateReason != "" {
		t.Errorf("gate on answered %v / %q (%v) — an open gate carries no refusal", v.RemoteCreateEnabled, v.RemoteCreateReason, err)
	}
}

func TestPublicStatementNamesVisibility(t *testing.T) {
	var pub, priv, none string
	for _, mode := range intake.PublishModes {
		switch mode {
		case "public":
			pub = intake.PublishStatement(mode)
		case "private":
			priv = intake.PublishStatement(mode)
		default:
			none = intake.PublishStatement(mode)
		}
	}
	if none != "" {
		t.Error("`none` carries a confirmation statement — a mode that touches nothing confirms nothing")
	}
	for _, want := range []string{"PUBLIC", "world", "confirm-public"} {
		if !strings.Contains(pub, want) {
			t.Errorf("the public statement omits %q — a soft confirmation of an outward publication is no confirmation: %q", want, pub)
		}
	}
	if !strings.Contains(priv, "PRIVATE") || strings.Contains(priv, "whole world") {
		t.Errorf("the private statement must say private-outward, not public: %q", priv)
	}
}

func TestRouteAvailableFlag(t *testing.T) {
	st, cfg := openS61Store(t)
	ctx := context.Background()
	id := newIntakeDoors(&Daemon{store: st, settings: cfg})
	v, err := id.Vocabulary(ctx)
	if err != nil {
		t.Fatalf("vocabulary: %v", err)
	}
	if v.RouteAvailable {
		t.Fatal("routeAvailable true with NO route in the store — a wizard that opens into GI16's refusal is the lying descriptor")
	}
	r := model.Route{ID: "r-t11", Provider: "anthropic", Model: "m", MaxOutputTokens: 1024, NoPrice: true}
	if err := st.UpsertRoute(r); err != nil {
		t.Fatalf("seed a route: %v", err)
	}
	v, err = id.Vocabulary(ctx)
	if err != nil || !v.RouteAvailable {
		t.Errorf("routeAvailable = %v (%v) with one route stored — intake.start would pass its gate right now", v.RouteAvailable, err)
	}
}
