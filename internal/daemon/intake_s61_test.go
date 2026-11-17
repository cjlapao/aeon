package daemon

// intake_s61_test.go — S61 T7: the intake plane's daemon doors over a REAL
// store. The protocol conformance fixtures prove the WIRE; these prove the
// doors answer facts: a charter list with its details, commit refusals that
// arrive typed (the door wraps the engine's sentence, never re-voices it),
// vocabularies that come from the enforcing packages and not from a copy
// written here, and a start that files the row the loop will one day hang
// its scratch on.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/autonomy"
	"github.com/cjlapao/aeon/internal/charter"
	"github.com/cjlapao/aeon/internal/config"
	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// openS61Store: a real store with the person, a component root, and one
// shipped template — the facts every door answer is measured against.
func openS61Store(t *testing.T) (*store.Store, *config.Store) {
	t.Helper()
	st, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st, NewSettings(st, config.Options{})
}

func s61Actor() store.Actor { return store.Actor{Kind: "user", ID: "tester"} }

func mustRoot(t *testing.T, st *store.Store, name string) store.Component {
	t.Helper()
	cw, err := st.CreateComponent(store.NewComponent{Name: name, Slug: name, Root: filepath.Join(t.TempDir(), name)})
	if err != nil {
		t.Fatalf("component: %v", err)
	}
	if !cw.OK() {
		t.Fatalf("component refused: %s — %s", cw.Refusal, cw.Reason)
	}
	return cw.Component
}

func mustTemplate(t *testing.T, st *store.Store, slug string) {
	t.Helper()
	_, ref, err := st.SaveTemplate(store.NewTemplate{
		Slug: slug, Title: "Base", Description: "the shipped floor",
		StackTags:  []string{"golang"},
		Files:      []store.TemplateFile{{Path: "README.md", Mode: 0o644, Content: "# {{.Name}}\n"}},
		SeedOrigin: "system",
	}, s61Actor())
	if err != nil {
		t.Fatalf("template: %v", err)
	}
	if ref != nil {
		t.Fatalf("template refused: %s", ref.Sentence)
	}
}

// TestChartersListWithDetails: charter.list answers every chartered root
// with its revision, its name (resolved through the component registry —
// a missing name stays empty, never invented), the summary line, and the
// proposed presence — details, not bare ids (GI8).
func TestChartersListWithDetails(t *testing.T) {
	st, cfg := openS61Store(t)
	root := mustRoot(t, st, "alpha")
	svc := &charter.Service{Store: st}
	body := charter.Body{Goal: "ship the intake plane", Proposed: &charter.Proposed{Verify: []string{"make test"}}}
	rev, ref, err := svc.Save(context.Background(), s61Actor(), root.ID, body, "first charter", "person", "")
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if ref != nil {
		t.Fatalf("charter refused: %s", ref.Sentence)
	}

	doors := newCharterDoors(&Daemon{store: st, settings: cfg})
	res, err := doors.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(res.Charters) != 1 {
		t.Fatalf("charters = %d, want the one chartered root", len(res.Charters))
	}
	e := res.Charters[0]
	if e.RootID != root.ID || e.Name != "alpha" || e.Revision != rev.Revision {
		t.Errorf("entry = %+v — the list must carry id, name and revision", e)
	}
	if !e.HasProposed {
		t.Error("hasProposed false on a charter whose body.proposed is set")
	}
	if !strings.Contains(e.Summary, "person") {
		t.Errorf("summary %q lost its audit word", e.Summary)
	}

	got, err := doors.Get(context.Background(), protocol.CharterGetParams{RootID: root.ID})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	// The body rides the door in its ONE canonical spelling.
	canonical, _ := charter.Canonical(body)
	if string(got.Body) != string(canonical) {
		t.Errorf("body drifted from canonical:\n got  %s\n want %s", got.Body, canonical)
	}
	// The lines are the charter package's own words (a byte-counted
	// statement summary, never a paraphrase written here).
	if got.Describe == nil || len(got.Describe.Lines) == 0 || !strings.Contains(got.Describe.Summary, "charter r1") {
		t.Errorf("description lines = %+v — the one-line describe must ride the view", got.Describe)
	}
}

// s61PlanPayload renders a desk-valid one-component standalone plan — the
// bytes the resume law reads.
func s61PlanPayload(t *testing.T, intakeID string) string {
	t.Helper()
	plan := desk.IntakePlanProposal{
		IntakeID:   intakeID,
		Charter:    json.RawMessage(`{"goal":"ship the intake plane"}`),
		Components: []desk.IntakePlanComponent{{Name: "Solo", Slug: "solo", Template: "base", Role: "standalone"}},
		Reason:     "The person asked for this project to exist.",
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	return string(raw)
}

func s61Proposal(t *testing.T, st *store.Store, kind, payload string) int64 {
	t.Helper()
	p, err := st.CreateProposal(kind, payload, s61Actor(), "")
	if err != nil {
		t.Fatalf("proposal: %v", err)
	}
	return p.ID
}

// TestIntakeCommitRefusalsAreTyped: every refusal family the commit door
// can meet arrives as the plane's ONE voice (ErrIntakeRefused) with the
// engine's or the door's sentence inside — never a bare error the client
// cannot tell apart from transport noise (GI12/GI13).
func TestIntakeCommitRefusalsAreTyped(t *testing.T) {
	st, cfg := openS61Store(t)
	mustTemplate(t, st, "base")
	doors := newIntakeDoors(&Daemon{store: st, settings: cfg})
	ctx := context.Background()
	target := filepath.Join(t.TempDir(), "solo")
	row, ref, err0 := st.CreateIntake("refusals", t.TempDir(), s61Actor())
	if err0 != nil || ref != nil {
		t.Fatalf("intake row: %v %v", err0, ref)
	}

	assertRefused := func(name string, err error, wantSub string) {
		t.Helper()
		if err == nil {
			t.Fatalf("%s: the commit answered success where a refusal was owed", name)
		}
		var pe *protocol.Error
		if !errors.As(err, &pe) {
			t.Fatalf("%s: refusal arrived as %T (%v), not the plane's typed voice", name, err, err)
		}
		if pe.Code != protocol.ErrIntakeRefused.Code {
			t.Errorf("%s: code %d, want %d", name, pe.Code, protocol.ErrIntakeRefused.Code)
		}
		if wantSub != "" && !strings.Contains(pe.Data.What, wantSub) {
			t.Errorf("%s: what %q lacks %q", name, pe.Data.What, wantSub)
		}
		if pe.Data.Means == "" || pe.Data.Todo == "" {
			t.Errorf("%s: refusal missing means/todo", name)
		}
	}

	// Family 1: a proposal that does not exist.
	_, err := doors.Commit(ctx, protocol.Actor{Kind: "user", ID: "tester"}, protocol.IntakeCommitParams{
		IntakeID: row.ID, ProposalID: "999999", Digest: "d", TargetPath: target, Publish: "none",
	})
	assertRefused("missing proposal", err, "999999")

	// Family 2: the wrong kind riding the door.
	kindPid := s61Proposal(t, st, "policy_add", `{"x":1}`)
	_, err = doors.Commit(ctx, protocol.Actor{Kind: "user", ID: "tester"}, protocol.IntakeCommitParams{
		IntakeID: row.ID, ProposalID: itoa64(kindPid), Digest: "d", TargetPath: target, Publish: "none",
	})
	assertRefused("wrong kind", err, "intake_plan")

	// Family 3: the crossed plan — a plan for another intake (P10's chain).
	crossPid := s61Proposal(t, st, string(desk.KindIntakePlan), s61PlanPayload(t, "int-someone-elses-session"))
	_, err = doors.Commit(ctx, protocol.Actor{Kind: "user", ID: "tester"}, protocol.IntakeCommitParams{
		IntakeID: row.ID, ProposalID: itoa64(crossPid), Digest: "d", TargetPath: target, Publish: "none",
	})
	assertRefused("crossed plan", err, "one chain")

	// Family 4: a stale (bogus) digest over a plan that otherwise stands —
	// the preview moved, re-preview; nothing half-wrote.
	matchPid := s61Proposal(t, st, string(desk.KindIntakePlan), s61PlanPayload(t, row.ID))
	_, err = doors.Commit(ctx, protocol.Actor{Kind: "user", ID: "tester"}, protocol.IntakeCommitParams{
		IntakeID: row.ID, ProposalID: itoa64(matchPid), Digest: "s61.intake.v1:0000", TargetPath: target, Publish: "none",
	})
	assertRefused("stale digest", err, "plan changed")

	// Family 5: no target anywhere — the door refuses naming the SETTING
	// (never inventing a directory), and once the setting is set the same
	// ask sails past the target guard into the digest guard.
	_, err = doors.Commit(ctx, protocol.Actor{Kind: "user", ID: "tester"}, protocol.IntakeCommitParams{
		IntakeID: row.ID, ProposalID: itoa64(matchPid), Digest: "s61.intake.v1:0000", Publish: "none",
	})
	assertRefused("no target", err, workspaceRootSetting)

	if _, b, err := cfg.Set(ctx, workspaceRootSetting, filepath.Join(t.TempDir(), "projects"), "user:tester"); err != nil || !b {
		t.Fatalf("set workspace root: %v %v", b, err)
	}
	_, err = doors.Commit(ctx, protocol.Actor{Kind: "user", ID: "tester"}, protocol.IntakeCommitParams{
		IntakeID: row.ID, ProposalID: itoa64(matchPid), Digest: "s61.intake.v1:0000", Publish: "none",
	})
	assertRefused("resolved target, stale digest", err, "plan changed")

	// And a plan-id that is not a number is a DOOR refusal (ErrIntakeRefused
	// too — the plane's voice, sentence first).
	_, err = doors.Commit(ctx, protocol.Actor{Kind: "user", ID: "tester"}, protocol.IntakeCommitParams{
		IntakeID: row.ID, ProposalID: "not-a-number", Digest: "d", TargetPath: target, Publish: "none",
	})
	assertRefused("non-numeric proposalId", err, "not a proposal number")
}

// TestVocabulariesComeFromThePackages: every closed set a door answers is
// read live from the package that ENFORCES it — the charter caps from the
// charter package, the plan caps from the desk validator, the template
// list from the store, the gate as actually read. A copy written here is
// a second truth waiting to drift.
func TestVocabulariesComeFromThePackages(t *testing.T) {
	st, cfg := openS61Store(t)
	mustTemplate(t, st, "base")
	ctx := context.Background()

	cd := newCharterDoors(&Daemon{store: st, settings: cfg})
	cv, err := cd.Vocabulary(ctx)
	if err != nil {
		t.Fatalf("charter.vocabulary: %v", err)
	}
	seam := func() []string {
		v := autonomy.Vocabulary()
		out := make([]string, 0, len(v.Subjects))
		for _, sub := range v.Subjects {
			if sub.Kind == "action" {
				out = append(out, sub.Name)
			}
		}
		return out
	}
	want := charter.Vocabulary(seam)
	if strings.Join(cv.CheckKinds, ",") != strings.Join(want.CheckKinds, ",") {
		t.Errorf("check kinds %v, want the charter package's %v", cv.CheckKinds, want.CheckKinds)
	}
	if cv.Caps.BodyBytes != want.Caps.BodyBytes || cv.Caps.Verify != want.Caps.Verify ||
		cv.Caps.Acceptance != want.Caps.Acceptance || cv.Caps.Goal != want.Caps.Goal {
		t.Errorf("caps drifted from the charter package: %+v vs %+v", cv.Caps, want.Caps)
	}
	if len(cv.Autonomy) == 0 {
		t.Error("the autonomy half answered empty — it is the S56 plane's live vocabulary, never a cache")
	}

	id := newIntakeDoors(&Daemon{store: st, settings: cfg})
	iv, err := id.Vocabulary(ctx)
	if err != nil {
		t.Fatalf("intake.vocabulary: %v", err)
	}
	caps := desk.IntakePlanCapsOf()
	if iv.Caps.PayloadBytes != caps.PayloadBytes || iv.Caps.Components != caps.Components ||
		iv.Caps.Goals != caps.Goals || iv.Caps.Items != caps.Items {
		t.Errorf("plan caps drifted from the desk validator: %+v vs %+v", iv.Caps, caps)
	}
	if strings.Join(iv.Roles, ",") != strings.Join(desk.IntakeRoles, ",") {
		t.Errorf("roles %v, want the desk's %v", iv.Roles, desk.IntakeRoles)
	}
	if len(iv.Templates) != 1 || iv.Templates[0].Slug != "base" {
		t.Errorf("templates %+v — the palette is the store's, live", iv.Templates)
	}
	if iv.RemoteCreateEnabled {
		t.Error("remoteCreateEnabled read true with the setting never set — the gate must default closed (GI12)")
	}
	if iv.RemoteCreateReason == "" {
		t.Error("the closed gate answered without its sentence — T11: no lying descriptor")
	}
	if _, b, err := cfg.Set(ctx, allowRemoteCreateSetting, "true", "user:tester"); err != nil || !b {
		t.Fatalf("set gate: %v %v", b, err)
	}
	iv, err = id.Vocabulary(ctx)
	if err != nil || !iv.RemoteCreateEnabled {
		t.Errorf("remoteCreateEnabled after setting = %v (%v) — the door reads the gate as read", iv.RemoteCreateEnabled, err)
	}
	if iv.RemoteCreateReason != "" {
		t.Errorf("an open gate carries no refusal to speak: %q", iv.RemoteCreateReason)
	}
}

// TestIntakeStartFilesTheRow: the start door files the row under the
// connection's person with a scratch the daemon created 0700, and the
// intake then lists with its honest state (idea — no plan yet).
func TestIntakeStartFilesTheRow(t *testing.T) {
	// S61 T9 (2026-11-05): the world grew up — a route (GI16's gate), the
	// seeded goal-intake blueprint, and a scripted runner, because start
	// now opens the interview loop through the real start door.
	st, doors := intakeStartWorld(t, true, nil)
	ctx := context.Background()
	scratch := filepath.Join(t.TempDir(), "scratch")

	view, err := doors.Start(ctx, protocol.Actor{Kind: "user", ID: "tester"}, protocol.IntakeStartParams{
		Name: "Goal intake demo", ScratchPath: scratch,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if !strings.HasPrefix(view.ID, "int-") || view.Name != "Goal intake demo" {
		t.Errorf("view = %+v — the row must come back as filed", view)
	}
	if view.LoopSlug == "" {
		t.Errorf("view = %+v — since T9 a filed row carries its interview loop", view)
	}
	fi, err := os.Stat(scratch)
	if err != nil || fi.Mode().Perm() != 0o700 {
		t.Errorf("scratch mode = %v (%v) — a plan draft is a person's private paper (GI9)", fi.Mode(), err)
	}

	// A relative scratch is refused outright: a relative promise is a
	// promise about nothing.
	if _, err := doors.Start(ctx, protocol.Actor{Kind: "user", ID: "tester"}, protocol.IntakeStartParams{
		Name: "rel", ScratchPath: "relative/scratch",
	}); err == nil {
		t.Error("relative scratch answered success — the door must refuse it by name")
	} else {
		var pe *protocol.Error
		if !errors.As(err, &pe) || !strings.Contains(pe.Data.What, "absolute") {
			t.Errorf("relative scratch refusal = %v — the plane's voice, naming absoluteness", err)
		}
	}

	list, err := doors.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	// The state word is the ENGINE's own: a row with no plan proposal is
	// "interviewing" (internal/intake/status.go) — the door copies, never
	// re-spells.
	if len(list.Intakes) != 1 || list.Intakes[0].State != "interviewing" {
		t.Errorf("list = %+v — the engine's word for a session with no plan yet is interviewing", list.Intakes)
	}

	// The default scratch (no param) lands under the state dir's intake
	// root, in its own private directory — T9's ownership of the scratch
	// layout the T7 record promised (2026-11-05).
	def, err := doors.Start(ctx, protocol.Actor{Kind: "user", ID: "tester"}, protocol.IntakeStartParams{Name: "defaulted"})
	if err != nil {
		t.Fatalf("default-scratch start: %v", err)
	}
	if filepath.Dir(def.ScratchPath) != filepath.Join(filepath.Dir(st.Path()), "intake") {
		t.Errorf("default scratch %q does not live under <stateDir>/intake", def.ScratchPath)
	}
}
