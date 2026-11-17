package store

// S60 T1 · MQ characterization goldens for the store.
//
// Header of who changes what:
//
//   - loop_branch_cols — the four branch columns on the loop row exist
//     today and NOTHING writes them; T4 (S60) is the first writer of
//     feature_branch (the name pinned at loop.start) and T7 the writer of
//     the landing's base. A loop created through CreateLoop without them
//     stays exactly as empty as this golden says — T4 must not start
//     writing defaults at creation.
//   - component_branch_settings — unchanged by S60: the read T4/T5 lean on
//     is EffectiveComponent's walk, and this golden is its today-shape for
//     a root with and without the two settings set.
//
// No clock, no ids: slugs are the fixture's own.

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var mqStoreGoldenUpdate = flag.Bool("mq-golden-update", false, "rewrite internal/store/testdata/mq_golden from today's behaviour (Phase A ritual only)")

func mqStoreGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "mq_golden", name+".golden")
	if *mqStoreGoldenUpdate {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("rewrote %s", path)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden %s missing (run `-run Golden -mq-golden-update` on untouched code to mint it): %v", path, err)
	}
	if string(want) != got {
		t.Fatalf("%s drifted from the golden.\nIf the drift is a task this file's header says is expected, update the golden AND say what changed.\n--- got ---\n%s\n--- want ---\n%s", path, got, string(want))
	}
}

// TestLoopBranchColumnsEmptyGolden pins F1/F2: CreateLoop writes none of
// default_branch/base_branch/branch_mode/feature_branch — the row lands
// with all four empty, which is why the engine today cannot know a loop's
// base at all. Changer: T4 (first writer of feature_branch, at start).
func TestLoopBranchColumnsEmptyGolden(t *testing.T) {
	_, s := testStore(t)
	l, err := s.CreateLoop(NewLoop{Slug: "4a17-version", Goal: "g", Root: "/repo", ComponentRoot: "/repo", Mode: "standard"})
	if err != nil {
		t.Fatal(err)
	}
	back, err := s.GetLoop(Scope{LoopSlug: "4a17-version", ComponentRoot: "/repo"})
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "created: defaultBranch=%q baseBranch=%q branchMode=%q featureBranch=%q\n",
		l.DefaultBranch, l.BaseBranch, l.BranchMode, l.FeatureBranch)
	fmt.Fprintf(&b, "readback: defaultBranch=%q baseBranch=%q branchMode=%q featureBranch=%q\n",
		back.DefaultBranch, back.BaseBranch, back.BranchMode, back.FeatureBranch)
	mqStoreGolden(t, "loop_branch_cols", b.String())
}

// TestComponentBranchSettingsGolden pins the settings read S60 leans on:
// a root with both set answers them verbatim through EffectiveComponent;
// a root with neither answers the column defaults (what T4 will pin at
// loop.start so the loop never re-reads a moving setting).
func TestComponentBranchSettingsGolden(t *testing.T) {
	_, s := testStore(t)
	db, br := "trunk", "aeon"
	// F-pin (T0 inventory): CreateComponent CANNOT carry the two settings —
	// insertComponent writes them for children only, a root gets the column
	// default. The settings reach a root through the UPDATE door only, which
	// is exactly what the daemon's component.update calls.
	setW, err := s.CreateComponent(NewComponent{Name: "Set", Slug: "set", Root: "/set"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateComponent(ComponentEdit{ID: setW.Component.ID, DefaultBranch: &db, BranchPrefix: &br}); err != nil {
		t.Fatal(err)
	}
	bareW, err := s.CreateComponent(NewComponent{Name: "Bare", Slug: "bare", Root: "/bare"})
	if err != nil {
		t.Fatal(err)
	}
	set, err := s.EffectiveComponent(setW.Component.ID)
	if err != nil {
		t.Fatal(err)
	}
	bare, err := s.EffectiveComponent(bareW.Component.ID)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "set:  defaultBranch=%q/%s branchPrefix=%q/%s\n", set.DefaultBranch.Value, set.DefaultBranch.Source, set.BranchPrefix.Value, set.BranchPrefix.Source)
	fmt.Fprintf(&b, "bare: defaultBranch=%q/%s branchPrefix=%q/%s (no row setting; the ENGINE-side default is S60 T4's to own)\n", bare.DefaultBranch.Value, bare.DefaultBranch.Source, bare.BranchPrefix.Value, bare.BranchPrefix.Source)
	mqStoreGolden(t, "component_branch_settings", b.String())
}
