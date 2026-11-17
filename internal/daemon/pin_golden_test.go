package daemon

// pin_golden_test.go — S56 T1 (characterization). The pinned
// {attended, allow_outward, started_by, trigger_id} row of EVERY loop
// start door, driven over real temp stores on the untouched tree
// (169cf1ee, pre-S56):
//
//	schedule_fire             a clock fire (StartFromTrigger, ProvenanceTrigger)
//	webhook_fire              a webhook fire (StartFromTrigger, ProvenanceWebhook)
//	event_fire                an event fire (S40's event door)
//	narrow_body               the narrow surface's trigger body carrying
//	                          attended:true, allowOutward:true through
//	                          TriggerParamsFor (its ONE transform)
//	socket_attended_outward   a socket loop.start declaring both fields
//	socket_neither            a socket loop.start declaring neither (NULL, not false)
//	queue_advance             the queue's advanceNext materialization (D5 carry-through)
//
// Expected today (what the goldens record): schedule/webhook/event
// attended=false, allow_outward=false; narrow attended=false (FORCED),
// allow_outward=true (the body's, passed through); socket = the body's
// (true,true) or NULL when absent; queue attended=false (the item's
// declaration, carried unchanged), allow_outward=false.
//
// Later tasks this slice expects to change this file: NONE — AL16
// promises the pins do not move (T4 pins grants BESIDE these legacy flags
// and never writes them; S27's future scheduler start goes through the
// same door with origin/labels, never authority fields). A moved pin here
// is a deliberate, listed amendment in that task's Notes.
//
// Determinism: fixed clock instant, fixed trigger ids and slugs, a gate
// blueprint that parks without a model, and one fresh store per door. The
// rows record ONLY the pin's four fields — no path-shaped or time-shaped
// value can drift into a golden.
//
// Generation (run once, on the untouched tree):
//
//	AEON_WRITE_GOLDEN=1 go test ./internal/daemon/ -run TestPinnedValuesPerStartPath -count=1 -tags sqlite_fts5

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/automation"
	"github.com/cjlapao/aeon/internal/narrow"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/queue"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/trigger"
)

// goldPinRow is one start door's pin, in the columns' own spellings.
// Attended is a pointer: 0 | 1 | null is the three-state honesty (D2) —
// a golden that collapses NULL into false would hide a defaulted true.
type goldPinRow struct {
	Attended     *bool  `json:"attended"`
	AllowOutward bool   `json:"allow_outward"`
	StartedBy    string `json:"started_by"`
	TriggerID    string `json:"trigger_id"`
}

const goldPinDir = "testdata/pin"

func goldReadPinRow(t *testing.T, s *store.Store, id string) goldPinRow {
	t.Helper()
	ctx := context.Background()
	attended, err := s.LoopAttended(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	outward, err := s.LoopAllowOutward(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	startedBy, triggerID, err := s.LoopStartedBy(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return goldPinRow{Attended: attended, AllowOutward: outward, StartedBy: startedBy, TriggerID: triggerID}
}

func TestPinnedValuesPerStartPath(t *testing.T) {
	drives := []struct {
		name string
		fn   func(*testing.T) goldPinRow
	}{
		{"schedule_fire", goldPinScheduleFire},
		{"webhook_fire", goldPinWebhookFire},
		{"event_fire", goldPinEventFire},
		{"narrow_body", goldPinNarrowBody},
		{"socket_attended_outward", goldPinSocketAttendedOutward},
		{"socket_neither", goldPinSocketNeither},
		{"queue_advance", goldPinQueueAdvance},
	}
	for _, d := range drives {
		d := d
		t.Run(d.name, func(t *testing.T) {
			row := d.fn(t)
			got, err := json.MarshalIndent(row, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, '\n')
			path := filepath.Join(goldPinDir, d.name+".json")
			if os.Getenv("AEON_WRITE_GOLDEN") == "1" {
				if err := os.MkdirAll(goldPinDir, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, got, 0600); err != nil {
					t.Fatal(err)
				}
				t.Logf("wrote %s", path)
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("no golden: %v — run once with AEON_WRITE_GOLDEN=1 on the untouched tree (S56 T1)", err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("the pin moved off its golden:\n--- golden ---\n%s--- current ---\n%sThe pinned start values are the pre-S56 contract (AL16); a move here is a deliberate, listed amendment in that task's Notes — not a re-generated file.", want, got)
			}
		})
	}
}

// goldPinScheduleFire — the clock's own authorship: unattended by the
// start itself, no outward (there is no field to ask with), provenance
// "trigger" beside the pin.
func goldPinScheduleFire(t *testing.T) goldPinRow {
	s := recoveryStore(t)
	d := New(Options{Store: s})
	instant := time.Date(2026, 10, 27, 3, 30, 0, 0, time.UTC)
	id, err := d.StartFromTrigger(context.Background(), trigger.Fire{
		TriggerID: "nightly-survey", Blueprint: writeGateBlueprint(t, "pin-sched"), Workspace: t.TempDir(),
		Provenance: trigger.ProvenanceTrigger, Instant: instant, At: instant,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitGateParked(t, s, d, id)
	return goldReadPinRow(t, s, id)
}

// goldPinWebhookFire — the webhook's twin: same authorship, its own
// provenance word.
func goldPinWebhookFire(t *testing.T) goldPinRow {
	s := recoveryStore(t)
	d := New(Options{Store: s})
	instant := time.Date(2026, 10, 27, 4, 0, 0, 0, time.UTC)
	id, err := d.StartFromTrigger(context.Background(), trigger.Fire{
		TriggerID: "on-push", Blueprint: writeGateBlueprint(t, "pin-webhook"), Workspace: t.TempDir(),
		Provenance: trigger.ProvenanceWebhook, Instant: instant, At: instant,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitGateParked(t, s, d, id)
	return goldReadPinRow(t, s, id)
}

// goldPinEventFire — S40's event door: the one Attended assignment the
// event path carries (AU2), provenance "event", no outward lever at all.
func goldPinEventFire(t *testing.T) goldPinRow {
	s, root := t14Env(t)
	d := New(Options{Store: s})
	gid := seedApprovedGoal(t, s)
	st, err := d.eventStarter().Start(context.Background(), automation.EventStartRequest{
		TriggerID: "tr1", TriggerName: "auto-dev", Blueprint: writeGateBlueprint(t, "pin-event"),
		WorkspaceRoot: root, Slug: automation.AutoSlug("tr1", 7), Event: "goal.approved",
		SubjectKind: "goal", SubjectID: gid, SubjectVersion: 2, Depth: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitGateParked(t, s, d, st.LoopID)
	return goldReadPinRow(t, s, st.LoopID)
}

// goldPinNarrowBody — the narrow surface's body claiming BOTH fields,
// through the surface's own transform: attended is FORCED false (a CI job
// is not present in the sense a gate needs), allowOutward rides through
// (the one outward lever any trigger gets). The JSON round trip is the
// same shape http.go/stdio.go hand the daemon door.
func goldPinNarrowBody(t *testing.T) goldPinRow {
	s := recoveryStore(t)
	d := New(Options{Store: s})
	body := map[string]any{
		"blueprint": writeGateBlueprint(t, "pin-narrow"), "goal": "a narrow trigger's own words",
		"workspaceRoot": t.TempDir(), "slug": "pin-narrow", "attended": true, "allowOutward": true,
	}
	transformed := narrow.TriggerParamsFor(body)
	raw, err := json.Marshal(transformed)
	if err != nil {
		t.Fatal(err)
	}
	var p protocol.LoopStartParams
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	res, err := d.Table.StartBlueprintLoop(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	id, ok := res.(map[string]any)["id"].(string)
	if !ok || id == "" {
		t.Fatalf("the start answered %v — no loop id to read the pin from", res)
	}
	waitGateParked(t, s, d, id)
	return goldReadPinRow(t, s, id)
}

// goldPinSocketAttendedOutward — the person's door with both declarations:
// the pin records exactly what the body said.
func goldPinSocketAttendedOutward(t *testing.T) goldPinRow {
	s := recoveryStore(t)
	d := New(Options{Store: s})
	yes := true
	res, err := d.Table.StartBlueprintLoop(context.Background(), protocol.LoopStartParams{
		Blueprint: writeGateBlueprint(t, "pin-sock-att"), Goal: "a person's attended start",
		WorkspaceRoot: t.TempDir(), Slug: "pin-socket-att", Attended: &yes, AllowOutward: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	id, ok := res.(map[string]any)["id"].(string)
	if !ok || id == "" {
		t.Fatalf("the start answered %v — no loop id to read the pin from", res)
	}
	waitGateParked(t, s, d, id)
	return goldReadPinRow(t, s, id)
}

// goldPinSocketNeither — the person's door with NO declarations: attended
// must read NULL (never-declared), never collapsed into false (P31).
func goldPinSocketNeither(t *testing.T) goldPinRow {
	s := recoveryStore(t)
	d := New(Options{Store: s})
	res, err := d.Table.StartBlueprintLoop(context.Background(), protocol.LoopStartParams{
		Blueprint: writeGateBlueprint(t, "pin-sock-quiet"), Goal: "a person's silent start",
		WorkspaceRoot: t.TempDir(), Slug: "pin-socket-quiet",
	})
	if err != nil {
		t.Fatal(err)
	}
	id, ok := res.(map[string]any)["id"].(string)
	if !ok || id == "" {
		t.Fatalf("the start answered %v — no loop id to read the pin from", res)
	}
	waitGateParked(t, s, d, id)
	return goldReadPinRow(t, s, id)
}

// goldPinQueueAdvance — the queue's materialization: the item's declared
// attendedness is carried UNCHANGED through advanceNext into the pin (D5
// — declared or never-declared, never inferred from the daemon starting
// it), and the queue is not an outward lever.
func goldPinQueueAdvance(t *testing.T) goldPinRow {
	s := recoveryStore(t)
	d := New(Options{Store: s})
	root := t.TempDir()
	if _, err := s.CreateComponent(store.NewComponent{Name: "pin-queue", Slug: "pin-queue", Root: root}); err != nil {
		t.Fatal(err)
	}
	q := queue.New(s)
	unattended := false
	it, err := q.Append(root, writeGateBlueprint(t, "pin-queue-bp"), "a queued item's own words", root, queue.NewItem{Attended: &unattended})
	if err != nil {
		t.Fatal(err)
	}
	it, r := q.Transition(it, queue.StatusReady)
	if r != nil {
		t.Fatalf("ready: %s", r.Reason)
	}
	if !d.advanceNext(context.Background(), root) {
		t.Fatal("advanceNext did not start the ready item")
	}
	loops, err := s.ListLoops(store.ComponentScope{ComponentRoot: root})
	if err != nil || len(loops) != 1 {
		t.Fatalf("loops after advance: %d, %v — want exactly the one the item materialized", len(loops), err)
	}
	waitGateParked(t, s, d, loops[0].ID)
	return goldReadPinRow(t, s, loops[0].ID)
}
