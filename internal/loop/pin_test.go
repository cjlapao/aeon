package loop

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/database"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/testutil/dbfixture"
)

// The tests here that assert nothing about attendedness pass nil as Start's
// declaration — they make none, and nil is that fact recorded honestly (D2:
// never-declared, its own state, not a stand-in for false). The two declared
// states are Start's own test, below.
func attended(v bool) *bool { return &v }

func sameAttended(got, want *bool) bool {
	if got == nil || want == nil {
		return got == want
	}
	return *got == *want
}

func formatAttended(p *bool) string {
	if p == nil {
		return "never-declared"
	}
	if *p {
		return "attended"
	}
	return "unattended"
}

func TestStartPinsAttendedness(t *testing.T) {
	// D2 / QAP-11: attendedness is DECLARED at start and pinned beside the
	// blueprint bytes, never inferred from anything afterwards. Start writes
	// loop.attended from its explicit argument — 1 / 0 / NULL — and the read
	// side hands the three states back exactly, including never-declared
	// never arriving as false.
	for _, tc := range []struct {
		name     string
		declared *bool
		want     *bool
	}{
		{"attended", attended(true), attended(true)},
		{"unattended", attended(false), attended(false)},
		{"never-declared", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := openLoopStore(t)
			path := filepath.Join(t.TempDir(), "attended.yaml")
			writeBlueprint(t, path, "  - id: first\n    step: {kind: model-step, agent: default}\n")
			started, err := Start(context.Background(), s, store.NewLoop{Slug: tc.name, ComponentRoot: "/repo", Goal: "goal"}, path, tc.declared, nil)
			if err != nil {
				t.Fatal(err)
			}

			// Beside the pin, from ONE read of the created loop: the blueprint
			// bytes and the declaration come from the same row set — there is
			// no second source for either.
			pin, err := s.ReadPinnedLoop(started.ID)
			if err != nil {
				t.Fatal(err)
			}
			if pin.BlueprintID == "" || pin.Content == "" {
				t.Fatal("the blueprint pin did not come from the same read as the attendedness")
			}
			if !sameAttended(pin.Attended, tc.want) {
				t.Fatalf("pinned attendedness is %s, want %s", formatAttended(pin.Attended), formatAttended(tc.want))
			}
			if got, err := s.LoopAttended(context.Background(), started.ID); err != nil || !sameAttended(got, tc.want) {
				t.Fatalf("LoopAttended = %s (%v), want %s", formatAttended(got), err, formatAttended(tc.want))
			}

			// "Post-start connect/disconnect cannot flip it" at the store level
			// (its socket-level proof is the QA suite's): a client connecting
			// or disconnecting touches NO attendedness row, so the honest
			// simulation is to write nothing and re-read. The stamp columns
			// are the guard — if any code path had UPDATEd the loop or its pin
			// row between the reads, the stamps would have moved.
			sc := store.Scope{LoopSlug: tc.name, ComponentRoot: "/repo"}
			before, err := s.GetLoop(sc)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Pinned(context.Background(), s, started.ID); err != nil { // "connect"
				t.Fatal(err)
			}
			if _, err := s.LoopAttended(context.Background(), started.ID); err != nil { // "disconnect"
				t.Fatal(err)
			}
			after, err := s.GetLoop(sc)
			if err != nil {
				t.Fatal(err)
			}
			if after.UpdatedAt != before.UpdatedAt {
				t.Errorf("the loop row was written between the reads (%s → %s); the value that changed "+
					"was not the one pinned at start", before.UpdatedAt, after.UpdatedAt)
			}
			reread, err := Pinned(context.Background(), s, started.ID)
			if err != nil {
				t.Fatal(err)
			}
			if reread.PinnedContent != started.PinnedContent {
				t.Error("the pin itself moved between reads")
			}
			pin2, err := s.ReadPinnedLoop(started.ID)
			if err != nil {
				t.Fatal(err)
			}
			if pin2.UpdatedAt != pin.UpdatedAt {
				t.Errorf("the pin row was written between the reads (%s → %s)", pin.UpdatedAt, pin2.UpdatedAt)
			}
			if !sameAttended(pin2.Attended, tc.want) {
				t.Fatalf("after connect/disconnect the stored attendedness is %s, want the pinned %s",
					formatAttended(pin2.Attended), formatAttended(tc.want))
			}
		})
	}
}

var loopStoreFixture dbfixture.Template

func openLoopStore(t *testing.T) *store.Store {
	t.Helper()
	dir := loopStoreFixture.Dir(t, "state.db", func(dir string) error {
		s, err := store.Open(store.Options{Dir: dir, Env: func(string) string { return "" }})
		if err != nil {
			return err
		}
		return s.Close()
	})
	s, err := store.Open(store.Options{Dir: dir, Env: func(string) string { return "" }, SkipMigrations: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func writeBlueprint(t *testing.T, path string, lanes string) {
	t.Helper()
	raw := "id: editable\nschemaVersion: 1\nversion: 1\nlanes:\n" + lanes
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestRunningLoopKeepsItsPinAndNewLoopSeesTheEdit(t *testing.T) {
	s := openLoopStore(t)
	path := filepath.Join(t.TempDir(), "editable.yaml")
	writeBlueprint(t, path, "  - id: first\n    step: {kind: model-step, agent: default}\n")
	a, err := Start(context.Background(), s, store.NewLoop{Slug: "a", ComponentRoot: "/repo", Goal: "goal"}, path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	before := a.PinnedContent
	writeBlueprint(t, path, "  - id: first\n    step: {kind: model-step, agent: default}\n  - id: second\n    step: {kind: model-step, agent: default}\n")
	saved, err := Pinned(context.Background(), s, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.PinnedContent != before || len(saved.Blueprint.Lanes) != 1 {
		t.Fatal("running loop followed the edit")
	}
	b, err := Start(context.Background(), s, store.NewLoop{Slug: "b", ComponentRoot: "/repo", Goal: "goal"}, path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Blueprint.Lanes) != 2 || b.PinnedContent == before {
		t.Fatal("new loop missed the edit")
	}
}

func TestPinStoresRawText(t *testing.T) {
	s := openLoopStore(t)
	path := filepath.Join(t.TempDir(), "x.yaml")
	writeBlueprint(t, path, "  # keep this comment\n  - id: first\n    step: {kind: model-step, agent: default}\n")
	raw, _ := os.ReadFile(path)
	l, err := Start(context.Background(), s, store.NewLoop{Slug: "raw", ComponentRoot: "/repo"}, path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if l.PinnedContent != string(raw) {
		t.Fatal("pin normalized YAML")
	}
}

func TestUnparseablePinBlocksTheLoop(t *testing.T) {
	s := openLoopStore(t)
	path := filepath.Join(t.TempDir(), "x.yaml")
	writeBlueprint(t, path, "  - id: first\n    step: {kind: model-step, agent: default}\n")
	l, err := Start(context.Background(), s, store.NewLoop{Slug: "bad", ComponentRoot: "/repo"}, path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	info, err := s.Info()
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open(database.DriverName, info.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE loop_blueprint SET content=? WHERE loop_id=?`, `broken: [`, l.ID); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	_, err = Pinned(context.Background(), s, l.ID)
	if err == nil || !strings.Contains(err.Error(), "pin-unparseable") {
		t.Fatal(err)
	}
}

func TestPinValidatesBeforeStoring(t *testing.T) {
	s := openLoopStore(t)
	path := filepath.Join(t.TempDir(), "bad.yaml")
	os.WriteFile(path, []byte("schemaVersion: 9"), 0600)
	_, err := Start(context.Background(), s, store.NewLoop{Slug: "bad", ComponentRoot: "/repo"}, path, nil, nil)
	if err == nil {
		t.Fatal("bad blueprint started")
	}
	loops, err := s.ListLoops(store.ComponentScope{ComponentRoot: "/repo"})
	if err != nil || len(loops) != 0 {
		t.Fatal(loops, err)
	}
}

func TestFutureHandlerIsRefusedBeforeCreatingALoop(t *testing.T) {
	// Two refusal flavours at the creating door, both renderable, both
	// creating ZERO loops:
	//   loop-doctor — unregistered, not futureAgents, not one of the
	//     follow-up five: the unknown-agent path ("unknown agent"), a name
	//     that will never ship so this stays tested after every flip.
	//   project-manager named this door's not-shipped path until T16 shipped
	//     its body (2026-10-26); with futureAgents now EMPTY no real name can
	//     reach it through Start, so the door is pinned directly by
	//     TestFutureHandlerDoorRefusesTheNamedFollowUp (synthetic diagnostic —
	//     generator pinned by blueprint's third-state test) and the
	//     creation-success half moved to TestShippedPipelinePinsNowThatEveryAgentShips.
	// task-coordinator shipped at T12 (2026-10-26) and moved to the
	// creation-success case below.
	for _, tc := range []struct {
		agent string
		wants []string
	}{
		// The two doors refuse in their own words — each case asserts the
		// refusal it ACTUALLY answers with (measured 2026-10-26; an earlier
		// draft demanded "unavailable-handler" from the unknown-agent door
		// and passed nothing but its own wish).
		{"loop-doctor", []string{"invalid-blueprint"}},
	} {
		agent, wants := tc.agent, tc.wants
		t.Run(agent, func(t *testing.T) {
			s := openLoopStore(t)
			path := filepath.Join(t.TempDir(), "future.yaml")
			writeBlueprint(t, path, "  - id: triage\n    step: {kind: model-step, agent: "+agent+"}\n")
			_, err := Start(context.Background(), s, store.NewLoop{Slug: "future", ComponentRoot: "/repo", Goal: "survey packages"}, path, nil, nil)
			if err == nil {
				t.Fatalf("unrunnable loop was accepted for agent %q", agent)
			}
			for _, want := range wants {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("refusal %q lacks %q: %v", err.Error(), want, err)
				}
			}
			loops, listErr := s.ListLoops(store.ComponentScope{ComponentRoot: "/repo"})
			if listErr != nil || len(loops) != 0 {
				t.Fatalf("unrunnable loop was created: %v, %v", loops, listErr)
			}
		})
	}
}

// TestByTechTagBlueprintPinsNow — D13's other half, at the door that
// creates the loop. `agent: by-tech-tag` used to be refused at pin time
// ("unavailable-handler … not shipped") because the resolver registry
// did not exist; T10 built it, so pinning must SUCCEED. A preflight that
// refuses a now-real capability is the same honesty failure as one that
// accepts a fictional one — it hides the capability behind an old refusal.
func TestByTechTagBlueprintPinsNow(t *testing.T) {
	s := openLoopStore(t)
	path := filepath.Join(t.TempDir(), "resolver.yaml")
	writeBlueprint(t, path, "  - id: implement\n    step: {kind: model-step, agent: by-tech-tag}\n")
	st, err := Start(context.Background(), s, store.NewLoop{Slug: "resolver", ComponentRoot: "/repo", Goal: "ship the change"}, path, nil, nil)
	if err != nil {
		t.Fatalf("a blueprint naming the real resolver was refused at pin time: %v — the registry exists (T10); an old refusal is a capability hidden, not a loop protected", err)
	}
	if st.Status == "blocked" {
		t.Fatal("pinned straight into blocked — the resolver reference must not be a pre-created dead end")
	}
}

func TestShippedConversationalMapCreatesNowThatTriageShips(t *testing.T) {
	// T12 flip (measured 2026-10-26): this pin asserted the shipped map was
	// REFUSED at creation because lanes[triage] named task-coordinator — its
	// one not-yet-shipped agent (grep: every other agent: is a shipped body
	// or the by-tech-tag resolver). The body now exists, so pinning must
	// SUCCEED: a preflight that refuses a now-real capability hides a
	// capability behind an old refusal — the test's own inversion, the
	// TestByTechTagBlueprintPinsNow doctrine applied to the follow-up flip.
	s := openLoopStore(t)
	path := filepath.Join("..", "blueprint", "blueprints", "conversational-map.yaml")
	st, err := Start(context.Background(), s, store.NewLoop{Slug: "demo", ComponentRoot: "/repo", Goal: "survey packages"}, path, nil, nil)
	if err != nil {
		t.Fatalf("the shipped map was refused at creation although every agent it names now resolves: %v", err)
	}
	loops, listErr := s.ListLoops(store.ComponentScope{ComponentRoot: "/repo"})
	if listErr != nil || len(loops) != 1 {
		t.Fatalf("a creatable loop did not land: %d loops, %v", len(loops), listErr)
	}
	if st.Blueprint == nil || st.Blueprint.ID != "conversational-map" {
		t.Fatalf("wrong blueprint pinned: %+v", st.Blueprint)
	}
}

// TestShippedPipelinePinsNowThatEveryAgentShips — T16's creation-success
// flip (measured 2026-10-26), the pipeline mirror of the conversational-map
// flip at T12: development-map-pipeline was REFUSED at creation until
// product-manager (T15) and project-manager (T16) shipped; every agent it
// names now resolves, so a preflight that still refused would be hiding a
// real capability behind an old refusal.
func TestShippedPipelinePinsNowThatEveryAgentShips(t *testing.T) {
	s := openLoopStore(t)
	path := filepath.Join("..", "blueprint", "blueprints", "development-map-pipeline.yaml")
	st, err := Start(context.Background(), s, store.NewLoop{Slug: "pipe", ComponentRoot: "/repo", Goal: "ship the feature"}, path, nil, nil)
	if err != nil {
		t.Fatalf("the shipped pipeline was refused at creation although every agent it names now resolves: %v", err)
	}
	loops, listErr := s.ListLoops(store.ComponentScope{ComponentRoot: "/repo"})
	if listErr != nil || len(loops) != 1 {
		t.Fatalf("a creatable loop did not land: %d loops, %v", len(loops), listErr)
	}
	if st.Blueprint == nil || st.Blueprint.ID != "development-map-pipeline" {
		t.Fatalf("wrong blueprint pinned: %+v", st.Blueprint)
	}
}
