package daemon

// intake_start_test.go — S61 T9 (2026-11-05): the two doors that open and
// close an intake session. The world is the T7 store plus the machinery a
// real start needs: a route (GI16's precondition), the seeded goal-intake
// blueprint row (the inert set applied the way daemon start applies it),
// and a loop runner with a scripted model — the interview runs against
// script, never a provider.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/agentseed"
	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/intakeseed"
	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/seed"
	"github.com/cjlapao/aeon/internal/store"
)

// intakeStartWorld: store + settings + the seeded plane + (by default) a
// route, a loop runner on a scripted model, and the daemon-lifetime ctx
// every start site rides.
func intakeStartWorld(t *testing.T, withRoute bool, script func() loop.ModelResult) (*store.Store, *intakeDoors) {
	t.Helper()
	st, cfg := openS61Store(t)
	d := New(Options{Store: st, Settings: cfg})
	// The two things Init wires that startLoopWith needs and New leaves to
	// the lifecycle: the daemon-lifetime ctx the runner rides (T4) and the
	// store-backed blueprint source (S48 T3).
	d.loopCtx = context.Background()
	d.bpSrc = StoreBlueprintSource(st)
	if script == nil {
		script = func() loop.ModelResult {
			return loop.ModelResult{StepID: 1, Kind: "completed", Outcome: "ok"}
		}
	}
	d.loopRunner = loop.NewRunner(loop.Deps{
		Store:   st,
		Event:   func(string, any) {},
		Records: recordsReaderFor(st),
		Model: func(_ context.Context, _ loop.LoopState, _ blueprint.Step, _ string, _ string) (loop.ModelResult, error) {
			return script(), nil
		},
	})
	// The interview's own two inert sets, applied the way daemon start
	// applies them (S61 T9's mounts in NewSeedEngine).
	e, err := NewSeedEngine(st, seed.Options{}, seedValidatorPass)
	if err != nil {
		t.Fatalf("seed engine: %v", err)
	}
	for _, setID := range []string{agentseed.IntakeSetID, intakeseed.SetID} {
		if _, err := e.Apply(context.Background(), setID, nil,
			seed.Actor{Kind: "system", ID: "startup"}, seed.PathStartup); err != nil {
			t.Fatalf("apply %s: %v", setID, err)
		}
	}
	if withRoute {
		route := model.Route{ID: "r-intake", Provider: "anthropic", Model: "m", MaxOutputTokens: 1024, NoPrice: true}
		if err := st.UpsertRoute(route); err != nil {
			t.Fatalf("route: %v", err)
		}
	}
	return st, newIntakeDoors(d)
}

func TestIntakeStartOpensTheInterview(t *testing.T) {
	st, doors := intakeStartWorld(t, true, nil)
	ctx := context.Background()
	view, err := doors.Start(ctx, protocol.Actor{Kind: "user", ID: "tester"}, protocol.IntakeStartParams{Name: "Ship the intake slice"})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if view.LoopSlug == "" {
		t.Fatalf("view = %+v — T9's start attaches the interview loop; an empty slug is T7's stub answer", view)
	}
	// The scratch is private, unique, and lives under the state dir's
	// intake root — not beside the database as the T7 stub did.
	fi, err := os.Stat(view.ScratchPath)
	if err != nil {
		t.Fatalf("scratch: %v", err)
	}
	if fi.Mode().Perm() != 0o700 {
		t.Errorf("scratch mode %v — the interview's notes are a person's private paper (GI9)", fi.Mode().Perm())
	}
	if filepath.Base(filepath.Dir(view.ScratchPath)) != "intake" ||
		filepath.Dir(filepath.Dir(view.ScratchPath)) != filepath.Dir(st.Path()) {
		t.Errorf("scratch %q does not live under <stateDir>/intake", view.ScratchPath)
	}
	// The loop: right blueprint, attended PINNED true, outward false, the
	// intake named as its author, and the row linked once.
	loops, err := st.LoopsBySlug(view.LoopSlug)
	if err != nil || len(loops) != 1 {
		t.Fatalf("loops by slug %q: %v %v", view.LoopSlug, len(loops), err)
	}
	l := loops[0]
	stt, perr := loop.Pinned(ctx, st, l.ID)
	if perr != nil {
		t.Fatalf("pinned: %v", perr)
	}
	if stt.Blueprint.ID != "goal-intake" {
		t.Errorf("loop %s runs blueprint %q — intake.start runs goal-intake (GI9)", l.ID, stt.Blueprint.ID)
	}
	if att, err := st.LoopAttended(ctx, l.ID); err != nil || att == nil || !*att {
		word := "never-declared"
		if att != nil {
			word = strconv.FormatBool(*att)
		}
		t.Errorf("attended = %s (%v) — the daemon pins attended true for an interview opened by a person at the socket (P31)", word, err)
	}
	if out, err := st.LoopAllowOutward(ctx, l.ID); err != nil || out {
		t.Errorf("allowOutward = %v (%v) — the interview creates nothing outward (GI12)", out, err)
	}
	if by, _, err := st.LoopStartedBy(ctx, l.ID); err != nil || by != "intake" {
		t.Errorf("startedBy = %q (%v) — the provenance names the plane that started it", by, err)
	}
	row, err := st.GetIntake(view.ID)
	if err != nil || row.LoopSlug != view.LoopSlug {
		t.Errorf("intake row link = %q (%v), want %q", row.LoopSlug, err, view.LoopSlug)
	}
}

func TestIntakeStartRefusesWithoutARoute(t *testing.T) {
	st, doors := intakeStartWorld(t, false, nil)
	ctx := context.Background()
	_, err := doors.Start(ctx, protocol.Actor{Kind: "user", ID: "tester"}, protocol.IntakeStartParams{Name: "Interview nobody"})
	if err == nil {
		t.Fatal("start succeeded with no route — GI16's refusal is the point")
	}
	var pe *protocol.Error
	if !errors.As(err, &pe) || !strings.Contains(pe.Data.What, "aeon routes set") {
		t.Fatalf("refusal = %v — the sentence must name `aeon routes set` (GI16)", err)
	}
	rows, _ := st.ListIntakes()
	if len(rows) != 0 {
		t.Errorf("%d intake rows filed after the refusal — the gate stands BEFORE any row (GI16)", len(rows))
	}
}

func TestIntakeScratchIsPrivateAndNeverReused(t *testing.T) {
	_, doors := intakeStartWorld(t, true, nil)
	ctx := context.Background()
	person := protocol.Actor{Kind: "user", ID: "tester"}
	a, err := doors.Start(ctx, person, protocol.IntakeStartParams{Name: "Same name"})
	if err != nil {
		t.Fatalf("first start: %v", err)
	}
	b, err := doors.Start(ctx, person, protocol.IntakeStartParams{Name: "Same name"})
	if err != nil {
		t.Fatalf("second start: %v", err)
	}
	if a.ScratchPath == b.ScratchPath {
		t.Fatalf("both starts share %s — one session's memory cannot be two sessions' (T9)", a.ScratchPath)
	}
	// An explicit path that already exists is refused; a fresh explicit
	// path is honored (the person chose it) and still 0700.
	chosen := filepath.Join(t.TempDir(), "my-notes")
	c, err := doors.Start(ctx, person, protocol.IntakeStartParams{Name: "Chosen", ScratchPath: chosen})
	if err != nil || c.ScratchPath != chosen {
		t.Fatalf("explicit scratch: %v %v", c.ScratchPath, err)
	}
	if _, err := doors.Start(ctx, person, protocol.IntakeStartParams{Name: "Clash", ScratchPath: chosen}); err == nil {
		t.Error("an existing scratch path was accepted for a second session — the door must refuse it")
	}
}

func TestIntakeAbandonClosesSessionAndRejectsOpenPlans(t *testing.T) {
	st, doors := intakeStartWorld(t, true, nil)
	ctx := context.Background()
	person := protocol.Actor{Kind: "user", ID: "tester"}
	view, err := doors.Start(ctx, person, protocol.IntakeStartParams{Name: "Stalled interview"})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	// One open plan for THIS intake, one decoy plan for another, one
	// open charter proposal: only the first may move.
	planPayload, _ := json.Marshal(map[string]any{"intakeId": view.ID, "projectName": "stalled"})
	otherPayload, _ := json.Marshal(map[string]any{"intakeId": "int-other", "projectName": "other"})
	plan, err := st.CreateProposal(string(desk.KindIntakePlan), string(planPayload), store.Actor{Kind: "user", ID: "tester"}, "2026-11-05T10:00:00Z")
	if err != nil {
		t.Fatalf("plan proposal: %v", err)
	}
	decoy, err := st.CreateProposal(string(desk.KindIntakePlan), string(otherPayload), store.Actor{Kind: "user", ID: "tester"}, "2026-11-05T10:00:00Z")
	if err != nil {
		t.Fatalf("decoy proposal: %v", err)
	}
	charter, err := st.CreateProposal(string(desk.KindCharterSave), `{"rootId":"cmp-1"}`, store.Actor{Kind: "user", ID: "tester"}, "2026-11-05T10:00:00Z")
	if err != nil {
		t.Fatalf("charter proposal: %v", err)
	}

	res, err := doors.Abandon(ctx, person, protocol.IntakeAbandonParams{IntakeID: view.ID, Reason: "we restart next week"})
	if err != nil {
		var pe *protocol.Error
		if errors.As(err, &pe) {
			t.Fatalf("abandon: %v — what: %s", err, pe.Data.What)
		}
		t.Fatalf("abandon: %v", err)
	}
	if !res.LoopClosed {
		t.Errorf("loopClosed = false — the interview loop stood still or stopped; either closes the session (GI13)")
	}
	if len(res.RejectedProposals) != 1 || res.RejectedProposals[0] != strconv.FormatInt(plan.ID, 10) {
		t.Errorf("rejected = %v, want exactly [%s]", res.RejectedProposals, strconv.FormatInt(plan.ID, 10))
	}
	if p, found, _ := st.GetProposal(plan.ID); !found || p.Status != "rejected" ||
		p.DecidedByKind != "user" || p.DecidedByID != "tester" || !strings.Contains(p.DecideReason, "we restart next week") {
		t.Errorf("the plan proposal after abandon = %+v — desk.Reject's own stamp, not a side-write", p)
	}
	for _, untouched := range []int64{decoy.ID, charter.ID} {
		if p, found, _ := st.GetProposal(untouched); !found || p.Status != "proposed" {
			t.Errorf("proposal %d moved to %q — abandon touches only this intake's open plans", untouched, p.Status)
		}
	}
	// The DERIVED state says abandoned (no column lied), and the scratch
	// survives: a person's door closed a session, not erased its notes.
	stat, err := doors.Status(ctx, protocol.IntakeStatusParams{IntakeID: view.ID})
	if err != nil || stat.State != "abandoned" {
		t.Errorf("state = %q (%v), want abandoned — the derivation (internal/intake/status.go) after the reject", stat.State, err)
	}
	if _, err := os.Stat(view.ScratchPath); err != nil {
		t.Errorf("the scratch vanished — abandon may close a session, never delete its workspace (GI13): %v", err)
	}
}

func TestIntakeAbandonWithoutALoopStillClosesTheRow(t *testing.T) {
	st, doors := intakeStartWorld(t, true, nil)
	ctx := context.Background()
	in, ref, err := st.CreateIntake("orphan row", t.TempDir(), store.Actor{Kind: "user", ID: "tester"})
	if err != nil || ref != nil {
		t.Fatalf("row: %v %v", ref, err)
	}
	res, err := doors.Abandon(ctx, protocol.Actor{Kind: "user", ID: "tester"}, protocol.IntakeAbandonParams{IntakeID: in.ID, Reason: "never got a loop"})
	if err != nil || !res.LoopClosed {
		t.Fatalf("abandon of a loopless row: %+v %v", res, err)
	}
}
