package daemon

// S60 T1 · MQ characterization golden for the daemon doors S60 widens.
//
// Header of who changes what:
//
//   - loop_start_branch_cols — **T4 is the named changer**: the moment
//     loop.start pins the component's branch settings into the loop row
//     (feature_branch, and the default_branch/base_branch snapshot), this
//     golden's empties become values. Until then this is the proof the
//     socket door writes NONE of them — the columns exist and stay empty
//     through the real dispatch path, not just the store path (the store
//     sibling is internal/store/mq_golden_test.go).
//   - component_update_settings — unchanged by S60: this is the door T4
//     reads through (the settings exist and persist before any loop sees
//     them) and S60 adds NO new write to it.
//
// The walk harness (autonomy_demo_walk_test.go) is reused whole: real
// store, real git repo with a bare origin, blueprint rows already saved.
// No awaitSettled here — the loop row is read straight after start, before
// any advance could touch anything (and no runner is installed anyway).

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/narrow"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

var mqDaemonGoldenUpdate = flag.Bool("mq-golden-update", false, "rewrite internal/daemon/testdata/mq_golden from today's behaviour (Phase A ritual only)")

func mqDaemonGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "mq_golden", name+".golden")
	if *mqDaemonGoldenUpdate {
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

// TestLoopStartBranchColsGolden: loop.start through the socket writes no
// branch shape at all. Changer: T4.
func TestLoopStartBranchColsGolden(t *testing.T) {
	w := newWalkEnv(t)
	ctx := context.Background()
	narrowed := narrow.TriggerParamsFor(map[string]any{
		"blueprint":     "autonomy-walk",
		"workspaceRoot": w.ws,
		"goal":          "mq golden start",
		"attended":      true,
		"with": map[string]any{
			"task":           "walk",
			"task_title":     "mq golden task",
			"commit_message": "mq golden commit",
			"pr_summary":     "mq golden pr",
		},
	})
	raw, err := json.Marshal(narrowed)
	if err != nil {
		t.Fatal(err)
	}
	res, err := w.d.Table.Dispatch(ctx, w.conn, protocol.MethodLoopStart, raw)
	if err != nil {
		t.Fatalf("loop.start: %v", err)
	}
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("loop.start returned %T", res)
	}
	id, _ := m["id"].(string)
	if id == "" {
		t.Fatalf("no loop id in %v", m)
	}
	row, err := w.store.ReadLoopByID(id)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	// T4's changer filled these columns, and the pinned feature name embeds
	// the minted loop id — normalize the id the way the loop package's
	// goldens do, so the golden pins the SHAPE and not the random id.
	feat := strings.ReplaceAll(row.FeatureBranch, row.Slug, "<slug>")
	fmt.Fprintf(&b, "loop.start row: defaultBranch=%q baseBranch=%q branchMode=%q featureBranch=%q\n",
		row.DefaultBranch, row.BaseBranch, row.BranchMode, feat)
	mqDaemonGolden(t, "loop_start_branch_cols", b.String())
}

// TestComponentUpdateSettingsGolden: the settings door exists and persists
// the two values S60 reads; a second update that names neither leaves them
// standing (an omitted field is not a clear).
func TestComponentUpdateSettingsGolden(t *testing.T) {
	w := newWalkEnv(t)
	db, br := "trunk", "aeon"
	v1, err := w.store.UpdateComponent(store.ComponentEdit{ID: w.root.ID, DefaultBranch: &db, BranchPrefix: &br})
	if err != nil || !v1.OK() {
		t.Fatalf("update settings: %v %s", err, v1.Reason)
	}
	after, err := w.store.EffectiveComponent(w.root.ID)
	if err != nil {
		t.Fatal(err)
	}
	v2, err := w.store.UpdateComponent(store.ComponentEdit{ID: w.root.ID})
	if err != nil || !v2.OK() {
		t.Fatalf("update naming nothing: %v %s", err, v2.Reason)
	}
	after2, err := w.store.EffectiveComponent(w.root.ID)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "after set: defaultBranch=%q/%s branchPrefix=%q/%s\n",
		after.DefaultBranch.Value, after.DefaultBranch.Source, after.BranchPrefix.Value, after.BranchPrefix.Source)
	fmt.Fprintf(&b, "after no-field update: defaultBranch=%q branchPrefix=%q (omitted is not a clear)\n",
		after2.DefaultBranch.Value, after2.BranchPrefix.Value)
	mqDaemonGolden(t, "component_update_settings", b.String())
}
