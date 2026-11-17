package daemon

// S27 T1 — characterization goldens for roadmapMaterialize's two branches
// and its refusal sentences, driven through the daemon's REAL door
// (d.Table.RoadmapMaterialize) on the UNTOUCHED code, byte-compared
// against testdata/continuous_golden/roadmap_*.json (regenerate with
// S27_GOLDEN_UPDATE=1).
//
// WHO MAY CHANGE THESE AND HOW:
//   - roadmap_direct_started.json — T5: the direct branch becomes one
//     candidate the scheduler MAY pick instead of the door starting on the
//     person's behalf ONLY for enabled repositories; the disabled-repo
//     bytes below must stay identical (CN21).
//   - roadmap_queue_branch.json — T5: the queue-append shape must survive
//     byte-identically — the queue branch is already "the scheduler
//     decides"; enabling the scheduler changes nothing here (CN21 again).
//   - roadmap_refusals.json — T2 renames nothing in these sentences; the
//     day a scheduler word appears inside one, this golden is the witness.
//   - roadmap_claim_retry.json — T5 may ADD a decision row beside the
//     claim; the claim-before-start behaviour itself (the item keeps
//     `running`, the retry re-enters through the same door with the same
//     deterministic slug) is exactly what S27 keeps, so the bytes stand.

import (
	"context"
	"fmt"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/queue"
	"github.com/cjlapao/aeon/internal/roadmap"
)

type rmGolden struct {
	Answer   map[string]any `json:"answer"`             // the door's answer, or nil
	Error    string         `json:"error"`              // the refusal sentence, verbatim
	Item     rmGoldenItem   `json:"item"`               // the item row after the call
	Starts   []cnStart      `json:"starts"`             // what reached the loop.start door
	Queue    []cnItem       `json:"queue,omitempty"`    // queue rows, when the project has a queue
	Attempts []rmGoldenTry  `json:"attempts,omitempty"` // the retry golden: per-attempt answer/error
}

type rmGoldenItem struct {
	Status   string `json:"status"`
	LoopSlug string `json:"loopSlug"`
}

type rmGoldenTry struct {
	Answer map[string]any `json:"answer"`
	Error  string         `json:"error"`
}

func (f *roadmapFixture) rmSnapshot(t *testing.T, id int64, g *rmGolden) {
	t.Helper()
	it := f.show(t, id)
	g.Item = rmGoldenItem{Status: string(it.Status), LoopSlug: it.LoopSlug}
	for {
		select {
		case p := <-f.starts:
			g.Starts = append(g.Starts, cnStart{Blueprint: p.Blueprint, Goal: p.Goal, WorkspaceRoot: cnClean(p.WorkspaceRoot, f.root), Slug: p.Slug, Attended: p.Attended, With: p.With})
			continue
		default:
		}
		break
	}
	rows, err := f.s.ListQueueItems(f.root)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		slug := ""
		if row.LoopSlug != nil {
			slug = *row.LoopSlug
		}
		g.Queue = append(g.Queue, cnItem{ID: row.ID, Position: row.Position, Goal: row.Goal, Status: row.Status, LoopSlug: slug})
	}
}

func TestRoadmapMaterializeGoldens(t *testing.T) {
	user := roadmap.Actor{Kind: "user", ID: "test-person"}

	t.Run("direct_started", func(t *testing.T) {
		f := newRoadmapFixture(t)
		it := f.create(t, "export statements")
		f.walkToReady(t, it.ID)
		var g rmGolden
		ans, err := f.d.Table.RoadmapMaterialize(context.Background(), user, protocol.RoadmapMaterializeParams{
			ID: it.ID, Blueprint: "dev-loop", Goal: "export the statements", WorkspaceRoot: f.root, Tech: "go",
		})
		if err != nil {
			t.Fatal(err)
		}
		g.Answer, _ = ans.(map[string]any)
		f.rmSnapshot(t, it.ID, &g)
		rmCheck(t, "roadmap_direct_started", g)
	})

	t.Run("queue_branch_appends_and_starts_nothing", func(t *testing.T) {
		f := newRoadmapFixture(t)
		q := queue.New(f.s)
		if _, err := q.Append(f.root, "dev-loop", "an ordinary backlog item", f.root, queue.NewItem{}); err != nil {
			t.Fatal(err)
		}
		it := f.create(t, "export statements")
		f.walkToReady(t, it.ID)
		var g rmGolden
		ans, err := f.d.Table.RoadmapMaterialize(context.Background(), user, protocol.RoadmapMaterializeParams{
			ID: it.ID, Blueprint: "dev-loop", Goal: "export the statements", WorkspaceRoot: f.root, Tech: "go",
		})
		if err != nil {
			t.Fatal(err)
		}
		g.Answer, _ = ans.(map[string]any)
		f.rmSnapshot(t, it.ID, &g)
		// CG4 restated as a check, so the golden can never drift quietly:
		if len(g.Starts) != 0 {
			t.Fatalf("the queue branch reached the start door: %+v", g.Starts)
		}
		// The second call refuses — the sentence is part of the golden.
		_, g2err := f.d.Table.RoadmapMaterialize(context.Background(), user, protocol.RoadmapMaterializeParams{
			ID: it.ID, Blueprint: "dev-loop", Goal: "export the statements", WorkspaceRoot: f.root, Tech: "go",
		})
		g.Attempts = append(g.Attempts, rmGoldenTry{Error: rmErrString(g2err)})
		rmCheck(t, "roadmap_queue_branch", g)
	})

	t.Run("claim_kept_when_the_door_refuses_then_retry_starts", func(t *testing.T) {
		f := newRoadmapFixture(t)
		it := f.create(t, "export statements")
		f.walkToReady(t, it.ID)
		var g rmGolden
		// Attempt 1: the door is down (a daemon without a runner is exactly
		// the S27 story) — the CLAIM survives, the loop does not exist.
		live := f.d.Table.StartBlueprintLoop
		f.d.Table.StartBlueprintLoop = func(context.Context, protocol.LoopStartParams) (any, error) {
			return nil, fmt.Errorf("no runner wired")
		}
		_, err := f.d.Table.RoadmapMaterialize(context.Background(), user, protocol.RoadmapMaterializeParams{
			ID: it.ID, Blueprint: "dev-loop", Goal: "export the statements", WorkspaceRoot: f.root, Tech: "go",
		})
		g.Attempts = append(g.Attempts, rmGoldenTry{Error: rmErrString(err)})
		// The item keeps `running` with NO pointer — that claim is what the
		// retry (and S27's candidate pick) rides on.
		if got := f.show(t, it.ID); got.Status != roadmap.StatusRunning || got.LoopSlug != "" {
			t.Fatalf("the refusal moved the claim: %+v", got)
		}
		// Attempt 2: the door lives again — the deterministic slug means the
		// retry IS the same claim, not a second loop.
		f.d.Table.StartBlueprintLoop = live
		ans2, err2 := f.d.Table.RoadmapMaterialize(context.Background(), user, protocol.RoadmapMaterializeParams{
			ID: it.ID, Blueprint: "dev-loop", Goal: "export the statements", WorkspaceRoot: f.root, Tech: "go",
		})
		a2, _ := ans2.(map[string]any)
		g.Attempts = append(g.Attempts, rmGoldenTry{Answer: a2, Error: rmErrString(err2)})
		f.rmSnapshot(t, it.ID, &g)
		rmCheck(t, "roadmap_claim_retry", g)
	})

	t.Run("refusal_sentences", func(t *testing.T) {
		f := newRoadmapFixture(t)
		it := f.create(t, "an idea only")
		var g rmGolden
		// A status that never earned the door.
		_, err := f.d.Table.RoadmapMaterialize(context.Background(), user, protocol.RoadmapMaterializeParams{
			ID: it.ID, Blueprint: "dev-loop", Goal: "g", WorkspaceRoot: f.root,
		})
		g.Attempts = append(g.Attempts, rmGoldenTry{Error: rmErrString(err)})
		// Ready, then the priority is pulled back — D4 decided NOW.
		f.walkToReady(t, it.ID)
		if _, ref := f.rm.SetPriority(it.ID, roadmap.PriorityUnknown, ""); ref != nil {
			t.Fatal(ref.Reason)
		}
		_, err = f.d.Table.RoadmapMaterialize(context.Background(), user, protocol.RoadmapMaterializeParams{
			ID: it.ID, Blueprint: "dev-loop", Goal: "g", WorkspaceRoot: f.root,
		})
		g.Attempts = append(g.Attempts, rmGoldenTry{Error: rmErrString(err)})
		// A materialized item refuses a second materialization (D6).
		f2 := newRoadmapFixture(t)
		it2 := f2.create(t, "already a loop")
		f2.walkToReady(t, it2.ID)
		if _, err := f2.d.Table.RoadmapMaterialize(context.Background(), user, protocol.RoadmapMaterializeParams{
			ID: it2.ID, Blueprint: "dev-loop", Goal: "g", WorkspaceRoot: f2.root,
		}); err != nil {
			t.Fatal(err)
		}
		<-f2.starts
		f = f2
		_, err = f.d.Table.RoadmapMaterialize(context.Background(), user, protocol.RoadmapMaterializeParams{
			ID: it2.ID, Blueprint: "dev-loop", Goal: "g", WorkspaceRoot: f2.root,
		})
		g.Attempts = append(g.Attempts, rmGoldenTry{Error: rmErrString(err)})
		f.rmSnapshot(t, it2.ID, &g)
		rmCheck(t, "roadmap_refusals", g)
	})
}

func rmCheck(t *testing.T, name string, g rmGolden) {
	cnCheck(t, name, g)
}

// rmErrString keeps the daemon package's existing errString
// (blueprint_t3_test.go) undisturbed: the goldens serialize refusals
// under a private name.
func rmErrString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// TestQueueRoadmapRowsCarryTheDurableLink restates the with-link shape as
// a plain check beside the goldens: the queue row's with_json is the ONLY
// memory of which roadmap item it serves once S24 owns the start.
func TestQueueRoadmapRowsCarryTheDurableLink(t *testing.T) {
	f := newRoadmapFixture(t)
	if _, err := queue.New(f.s).Append(f.root, "dev-loop", "backlog", f.root, queue.NewItem{}); err != nil {
		t.Fatal(err)
	}
	it := f.create(t, "linked item")
	f.walkToReady(t, it.ID)
	if _, err := f.d.Table.RoadmapMaterialize(context.Background(), roadmap.Actor{Kind: "user", ID: "test-person"}, protocol.RoadmapMaterializeParams{
		ID: it.ID, Blueprint: "dev-loop", Goal: "g", WorkspaceRoot: f.root,
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := f.s.ListQueueItems(f.root)
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows: %d %v", len(rows), err)
	}
	id, role := queueRoadmapLink(rows[1])
	if id != it.ID || role != "materialization" {
		t.Errorf("link = (%d,%q), want (%d,materialization)", id, role, it.ID)
	}
	if got := f.show(t, it.ID); got.LoopSlug != "" || got.Status != roadmap.StatusRunning {
		t.Errorf("appending moved the loop pointer: %+v (CG4: appending starts nothing)", got)
	}
}
