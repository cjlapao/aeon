package daemon

// S27 T1 — the characterization goldens for the queue reactor, driven
// through the daemon's REAL emission door (publishLoop), on the UNTOUCHED
// code. Where S24's tests ask a question of behaviour, these files just
// RECORD it: items, starts, questions serialized to
// testdata/continuous_golden/*.json, byte-compared, regenerated only with
// S27_GOLDEN_UPDATE=1.
//
// WHO MAY CHANGE THESE AND HOW:
//   - queue_auto_done.json — T5: for an ENABLED repository the `done`/`auto`
//     branch KICKS the scheduler instead of starting the next item itself;
//     the DISABLED-repository golden below must stay byte-identical (CN21,
//     VG14). T5 adds a separate enabled-case golden, never edits this.
//   - queue_ask_done.json — T5: same rule for `ask` (the question stands;
//     the kick replaces only what a YES does). Disabled: byte-identical.
//   - queue_blocked_auto.json — never changes: a blocked item is CG2's
//     halt in both modes, and the scheduler never restarts it (CN13).
//   - queue_untracked_done.json — never changes: an untracked loop is not
//     the queue's news and not the scheduler's either.
//   - queue_failed_start.json — T5 may ADD the refused-claim decision row;
//     the reactor's own bytes stay.
//   - queue_empty_skill.json / queue_empty_manual.json — T5's kick must
//     never reach the sizing branch by accident (F5); these are the pin.
//
// The snapshot deliberately excludes timestamps, ids of runs, and event
// lines: what the reactor WRITES (item rows, question rows) and what it
// SAYS at the door (the LoopStartParams) is the characterization.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/history"
	"github.com/cjlapao/aeon/internal/queue"
	"github.com/cjlapao/aeon/internal/store"
)

type cnItem struct {
	ID       int64  `json:"id"`
	Position int    `json:"position"`
	Goal     string `json:"goal"`
	Status   string `json:"status"`
	LoopSlug string `json:"loopSlug"` // "" for never-stamped
}

type cnStart struct {
	Blueprint     string            `json:"blueprint"`
	Goal          string            `json:"goal"`
	WorkspaceRoot string            `json:"workspaceRoot"`
	Slug          string            `json:"slug"`
	Attended      *bool             `json:"attended"`
	With          map[string]string `json:"with"`
}

type cnGolden struct {
	Items      []cnItem    `json:"items"`
	Starts     []cnStart   `json:"starts"`
	Question   *cnQuestion `json:"question"` // pending question on the just-finished loop
	SizingLoop bool        `json:"sizingLoop"`
}

type cnQuestion struct {
	Kind string `json:"kind"`
	Body string `json:"body"`
}

func cnCheck(t *testing.T, name string, got any) {
	t.Helper()
	raw, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	path := filepath.Join("testdata", "continuous_golden", name+".json")
	if os.Getenv("S27_GOLDEN_UPDATE") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		t.Logf("golden %s rewritten", name)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden %s missing (%v) — generate with S27_GOLDEN_UPDATE=1 on the PRE-change code and commit it", path, err)
	}
	if string(want) != string(raw) {
		t.Errorf("golden %s drifted.\n--- got ---\n%s\n--- want ---\n%s", name, raw, want)
	}
}

// cnClean replaces a fixture's real temporary root with a placeholder:
// the workspace path a start carries is its OWN identity (t.TempDir()
// names differ run to run) — the golden records the SHAPE, and "<test-root>"
// says so honestly.
func cnClean(s, root string) string {
	if root == "" {
		return s
	}
	return strings.ReplaceAll(s, root, "<test-root>")
}

func (f *reactorFixture) snapshot(t *testing.T) cnGolden {
	t.Helper()
	var g cnGolden
	items, err := f.q.List(f.root)
	if err != nil {
		t.Fatal(err)
	}
	// The reactor stamps an item's loop slug when its start RETURNS —
	// the reactor goroutine's last word, written AFTER the start
	// recorder saw the start. The quiet-drain below gates the recorder,
	// not the stamp write, and under load the recorder can go quiet
	// while a stamp is still in flight (measured as an auto_done drift
	// in S27 T11's regression run). The goldens were RECORDED with the
	// stamps present, so waiting for every started item's stamp states
	// the same settled truth — never a weaker one.
	stampDeadline := time.Now().Add(5 * time.Second)
	for {
		pending := false
		for _, it := range items {
			if (it.Status == "running" || it.Status == "done") && it.LoopSlug == "" {
				pending = true
			}
		}
		if !pending {
			break
		}
		if time.Now().After(stampDeadline) {
			t.Fatal("an item started and never received its loop-slug stamp — the reactor's stamp write is missing, not late")
		}
		time.Sleep(time.Millisecond)
		if items, err = f.q.List(f.root); err != nil {
			t.Fatal(err)
		}
	}
	for _, it := range items {
		g.Items = append(g.Items, cnItem{ID: it.ID, Position: it.Position, Goal: it.Goal, Status: string(it.Status), LoopSlug: it.LoopSlug})
	}
	for {
		// Drain the start recorder only once it has gone SILENT: sampling a
		// moving channel is the race this golden exists to forbid from ever
		// being seen (found as a race-only drift in S27 T2's sweep — the
		// await above gates the STATE, and under -race a start can still be
		// in flight when the state has settled). Half a second with nothing
		// on the channel is the record being the record.
		quiet := time.NewTimer(500 * time.Millisecond)
		select {
		case p := <-f.starts:
			quiet.Stop()
			g.Starts = append(g.Starts, cnStart{Blueprint: p.Blueprint, Goal: p.Goal, WorkspaceRoot: cnClean(p.WorkspaceRoot, f.root), Slug: p.Slug, Attended: p.Attended, With: p.With})
			continue
		case <-quiet.C:
		}
		break
	}
	if q, found, err := f.s.PendingQuestion(store.Scope{ComponentRoot: f.root, LoopSlug: "loop-one"}); err == nil && found {
		g.Question = &cnQuestion{Kind: string(q.Kind), Body: q.Body}
	}
	loops, err := f.s.ListLoops(store.ComponentScope{ComponentRoot: f.root})
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range loops {
		if strings.HasPrefix(l.Slug, "size-item-") {
			g.SizingLoop = true
		}
	}
	return g
}

// TestQueueReactorGoldens records the six reactor outcomes named by S27 T1.
// Every drive line below mirrors S24's own green tests — the goldens must
// be green on untouched code, that is the point.
func TestQueueReactorGoldens(t *testing.T) {
	t.Run("auto_done", func(t *testing.T) {
		f := newReactorFixture(t)
		if _, err := f.s.SetComponentQueueSettings(f.root, "auto", "manual"); err != nil {
			t.Fatal(err)
		}
		first := f.appendReady(t, "shipped thing", nil)
		second := f.appendReady(t, "next thing", no())
		f.seedLoop(t, "loop-one")
		if _, err := f.s.SetQueueItemLoop(first.ID, f.root, "loop-one"); err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.TransitionQueueItem(first.ID, "ready", "running"); err != nil {
			t.Fatal(err)
		}
		f.materialize(t, "loop-one", "done", "")
		await(t, "the auto advance started the next item", func() bool {
			return f.itemByID(t, second.ID).Status == "running"
		})
		cnCheck(t, "queue_auto_done", f.snapshot(t))
	})
	t.Run("ask_done", func(t *testing.T) {
		f := newReactorFixture(t)
		if _, err := f.s.SetComponentQueueSettings(f.root, "ask", "manual"); err != nil {
			t.Fatal(err)
		}
		first := f.appendReady(t, "shipped", nil)
		f.appendReady(t, "decide the retry policy", nil)
		f.seedLoop(t, "loop-one")
		if _, err := f.s.SetQueueItemLoop(first.ID, f.root, "loop-one"); err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.TransitionQueueItem(first.ID, "ready", "running"); err != nil {
			t.Fatal(err)
		}
		f.materialize(t, "loop-one", "done", "")
		await(t, "the ask question", func() bool {
			_, found, err := f.s.PendingQuestion(store.Scope{ComponentRoot: f.root, LoopSlug: "loop-one"})
			return err == nil && found
		})
		cnCheck(t, "queue_ask_done", f.snapshot(t))
	})
	t.Run("blocked_auto", func(t *testing.T) {
		f := newReactorFixture(t)
		if _, err := f.s.SetComponentQueueSettings(f.root, "auto", "manual"); err != nil {
			t.Fatal(err)
		}
		first := f.appendReady(t, "stalling thing", nil)
		f.appendReady(t, "waiting thing", nil)
		f.seedLoop(t, "loop-one")
		if _, err := f.s.SetQueueItemLoop(first.ID, f.root, "loop-one"); err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.TransitionQueueItem(first.ID, "ready", "running"); err != nil {
			t.Fatal(err)
		}
		f.materialize(t, "loop-one", "blocked", "loop ceiling reached")
		await(t, "the blocked halt", func() bool {
			return f.itemByID(t, first.ID).Status == "blocked"
		})
		cnCheck(t, "queue_blocked_auto", f.snapshot(t))
	})
	t.Run("untracked_done", func(t *testing.T) {
		f := newReactorFixture(t)
		if _, err := f.s.SetComponentQueueSettings(f.root, "auto", "manual"); err != nil {
			t.Fatal(err)
		}
		f.appendReady(t, "untouched", nil)
		f.seedLoop(t, "someone-elses-loop")
		f.materialize(t, "someone-elses-loop", "done", "")
		// A beat for a goroutine that must NOT act.
		await(t, "silence on the door", func() bool { return true })
		cnCheck(t, "queue_untracked_done", f.snapshot(t))
	})
	t.Run("failed_start", func(t *testing.T) {
		f := newReactorFixture(t)
		if _, err := f.s.SetComponentQueueSettings(f.root, "auto", "manual"); err != nil {
			t.Fatal(err)
		}
		first := f.appendReady(t, "shipped", nil)
		f.appendReady(t, "unstartable", nil)
		f.seedLoop(t, "loop-one")
		if _, err := f.s.SetQueueItemLoop(first.ID, f.root, "loop-one"); err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.TransitionQueueItem(first.ID, "ready", "running"); err != nil {
			t.Fatal(err)
		}
		f.spyErr = fmt.Errorf("blueprint dev-loop not found")
		f.materialize(t, "loop-one", "done", "")
		await(t, "the unstartable item blocked", func() bool {
			items, err := f.q.List(f.root)
			if err != nil {
				return false
			}
			for _, it := range items {
				if it.Goal == "unstartable" {
					return it.Status == queue.StatusBlocked
				}
			}
			return false
		})
		cnCheck(t, "queue_failed_start", f.snapshot(t))
	})
	t.Run("empty_queue_skill_sizes", func(t *testing.T) {
		f := newReactorFixture(t)
		if _, err := f.s.SetComponentQueueSettings(f.root, "auto", "skill"); err != nil {
			t.Fatal(err)
		}
		first := f.appendReady(t, "watched", nil)
		// A PENDING item: nothing is ready, the queue is not empty.
		pending, err := f.q.Append(f.root, "dev-loop", "to be sized", f.root, queue.NewItem{})
		if err != nil {
			t.Fatal(err)
		}
		f.seedLoop(t, "loop-one")
		if _, err := f.s.SetQueueItemLoop(first.ID, f.root, "loop-one"); err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.TransitionQueueItem(first.ID, "ready", "running"); err != nil {
			t.Fatal(err)
		}
		f.materialize(t, "loop-one", "done", "")
		// CG5: with no runner behind the step the sizing dispatch runs and
		// REVERTES — the settle is pending again, sizing loop and all.
		await(t, "the sizing dispatch", func() bool {
			row := f.itemByID(t, pending.ID)
			return (row.Status == "sizing" || row.Status == "pending") && len(f.snapshot(t).Starts) == 0 && f.snapshot(t).SizingLoop
		})
		cnCheck(t, "queue_empty_skill", f.snapshot(t))
	})
	t.Run("empty_queue_manual_nothing", func(t *testing.T) {
		f := newReactorFixture(t)
		if _, err := f.s.SetComponentQueueSettings(f.root, "auto", "manual"); err != nil {
			t.Fatal(err)
		}
		first := f.appendReady(t, "watched", nil)
		pending, err := f.q.Append(f.root, "dev-loop", "never sized", f.root, queue.NewItem{})
		if err != nil {
			t.Fatal(err)
		}
		f.seedLoop(t, "loop-one")
		if _, err := f.s.SetQueueItemLoop(first.ID, f.root, "loop-one"); err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.TransitionQueueItem(first.ID, "ready", "running"); err != nil {
			t.Fatal(err)
		}
		f.materialize(t, "loop-one", "done", "")
		await(t, "first item done", func() bool { return f.itemByID(t, first.ID).Status == "done" })
		if row := f.itemByID(t, pending.ID); row.Status != "pending" {
			t.Fatalf("manual mode moved the pending item to %s — nothing here may size", row.Status)
		}
		cnCheck(t, "queue_empty_manual", f.snapshot(t))
	})
}

// TestQueueHoldDoesNotSeePersonLoops is the F4 fact CN8 fixes: the
// queue's own running/blocked hold is a QUEUE word — a person's loop
// running in the same repository does not stop the next ready item. If
// S27 (or anybody) ever made the hold see loop rows, this test says so
// LOUDLY, and the fix belongs in the hold's OWN gate, not here.
func TestQueueHoldDoesNotSeePersonLoops(t *testing.T) {
	f := newReactorFixture(t)
	if _, err := f.s.SetComponentQueueSettings(f.root, "auto", "manual"); err != nil {
		t.Fatal(err)
	}
	// A person's loop, running in the SAME repository, untracked by the queue.
	if _, err := f.s.CreatePinnedLoop(store.NewLoop{Slug: "persons-own", ComponentRoot: f.root, Root: f.root,
		Goal: "a person is working", Status: "running"}, "dev-loop", 1, queueBlueprintYAML, "{}"); err != nil {
		t.Fatal(err)
	}
	ready := f.appendReady(t, "queue says go", nil)
	item, has, r := f.q.NextReady(f.root)
	if r != nil {
		t.Fatalf("the hold refused with %s — a person's running loop is NOT a queue word (F4)", r.Reason)
	}
	if !has || item.ID != ready.ID {
		t.Fatalf("next ready = (%d,%v), want %d", item.ID, has, ready.ID)
	}
	if !f.d.advanceNext(context.Background(), f.root) {
		t.Fatal("advanceNext honoured the person's loop — the hold must be queue-only")
	}
	select {
	case p := <-f.starts:
		if p.Goal != "queue says go" {
			t.Fatalf("started %q", p.Goal)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the queue hold refused to start the ready item because of a person's loop")
	}
}

// TestHistoryIgnoresProvenance pins F3: two loops identical except for
// their provenance columns render into history BYTE-IDENTICALLY (bar the
// coordinates that name them), and the Entry type cannot even carry the
// provenance — the day S27's `scheduler` word lands in `started_by`, the
// history read side is already proven indifferent to it.
func TestHistoryIgnoresProvenance(t *testing.T) {
	f := newReactorFixture(t) // a real store and component root, nothing else used from it
	for _, p := range []struct{ slug, startedBy, triggerID string }{
		{"prov-plain", "", ""},
		{"prov-word", "some-future-word", "tr-9"},
	} {
		if _, err := f.s.CreatePinnedLoop(store.NewLoop{Slug: p.slug, ComponentRoot: f.root, Root: f.root,
			Goal: "same goal", Status: "running", StartedBy: p.startedBy, TriggerID: p.triggerID},
			"dev-loop", 1, queueBlueprintYAML, "{}"); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := history.Merge(f.s, history.Options{Scope: store.ComponentScope{ComponentRoot: f.root}})
	if err != nil {
		t.Fatal(err)
	}
	render := func(slug string) string {
		for _, e := range entries {
			if e.Slug == slug {
				// The coordinates that NAME the loop are not the rendering —
				// and neither is the wall-clock instant each pin stamped at
				// creation: two loops created a millisecond apart are the
				// same entry. (Found as a race-only flake in S27 T2: under
				// -race the two pins straddled a millisecond, and StartedAt
				// was the only byte that moved — the provenance words were
				// already absent, which is the claim this test owns.)
				e.ID, e.Slug, e.Title = "", "", ""
				e.StartedAt, e.EndedAt, e.Timeline = "", "", ""
				raw, err := json.Marshal(e)
				if err != nil {
					t.Fatal(err)
				}
				return string(raw)
			}
		}
		t.Fatalf("history never listed %s", slug)
		return ""
	}
	if a, b := render("prov-plain"), render("prov-word"); a != b {
		t.Errorf("provenance leaked into the rendering:\nplain: %s\nword:  %s", a, b)
	}
	// And the type cannot carry it: reflection is the lock.
	rt := reflect.TypeOf(history.Entry{})
	for i := 0; i < rt.NumField(); i++ {
		switch rt.Field(i).Name {
		case "StartedBy", "TriggerID", "StartedByKind":
			t.Errorf("history.Entry gained %s — F3 says the read side ignores provenance; adding the field is the argument this test exists to force", rt.Field(i).Name)
		}
	}
}
