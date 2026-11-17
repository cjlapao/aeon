package daemon

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// trigger_control_test.go — the operational plane through the DAEMON's own
// wired seams: what the app's buttons actually reach. The cmd/aeon test
// proves the CLI dials THESE methods; a socket `trigger.fireNow` must
// produce an unattended loop IDENTICALLY to the CLI — asserted from the
// loop row itself, not from code similarity (T8's rule).

func seedSchedule(t *testing.T, s *store.Store, id, ws string) {
	t.Helper()
	if err := s.SaveTriggerSchedule(context.Background(), store.TriggerScheduleRow{
		ID: id, Kind: "schedule", Blueprint: "two-step", Workspace: ws,
		Expr: "0 4 * * *", Zone: "UTC", CreatedAt: "2026-10-27T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestSocketFireNowProducesAnUnattendedLoop(t *testing.T) {
	s := recoveryStore(t)
	d := New(Options{Store: s})
	seedTriggerAllow(t, s, "button") // governance says YES (T7) — the
	// point here is that even a PERMITTED fire stays unattended.
	ws := t.TempDir()
	seedSchedule(t, s, "button", ws)

	raw, err := d.Table.TriggerFireNow(context.Background(), protocol.Actor{Kind: "user", ID: "operator"}, "button")
	if err != nil {
		t.Fatal(err)
	}
	r := raw.(protocol.TriggerControlResult)
	if !r.Found || r.Outcome != "fired" || r.LoopID == "" {
		t.Fatalf("socket fire-now did not fire: %+v", r)
	}
	awaitSettled(t, s, r.LoopID)
	// THE assertion: the button pressed nothing about attendedness. Same
	// APIs, same rows, same words the clock-started test reads.
	attended, err := s.LoopAttended(context.Background(), r.LoopID)
	if err != nil {
		t.Fatal(err)
	}
	if attended == nil || *attended {
		t.Fatalf("a socket fire-now started an ATTENDED loop — TR2's back door, over the very seam the app presses (attended=%v)", attended)
	}
	startedBy, triggerID, err := s.LoopStartedBy(context.Background(), r.LoopID)
	if err != nil {
		t.Fatal(err)
	}
	if startedBy != "trigger" || triggerID != "button" {
		t.Fatalf("provenance %q/%q — the button changed nothing about the hand", startedBy, triggerID)
	}
}

func TestSocketPauseAndResumeDriveTheListing(t *testing.T) {
	s := recoveryStore(t)
	d := New(Options{Store: s})
	seedTriggerAllow(t, s, "paused-dial")
	ws := t.TempDir()
	seedSchedule(t, s, "paused-dial", ws)

	raw, err := d.Table.TriggerPause(context.Background(), protocol.Actor{Kind: "user", ID: "operator"}, "paused-dial")
	if err != nil {
		t.Fatal(err)
	}
	p := raw.(protocol.TriggerControlResult)
	if !p.Found || !p.Paused || !strings.Contains(p.Reason, "TR12") {
		t.Fatalf("pause: %+v", p)
	}
	lraw, err := d.Table.TriggerList(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	lr := lraw.(protocol.TriggerListResult)
	if len(lr.Triggers) != 1 || !lr.Triggers[0].Paused {
		t.Fatalf("the listing does not show the off: %+v", lr.Triggers)
	}
	if lr.Triggers[0].NextFire == "" || !strings.Contains(lr.Triggers[0].NextFire, "UTC") {
		t.Fatalf("next fire is printed with its zone, computed not guessed: %q", lr.Triggers[0].NextFire)
	}
	enc, _ := json.Marshal(lr)
	for _, leak := range []string{"trigger_access", "refused", "PL5"} {
		if strings.Contains(string(enc), leak) {
			t.Fatalf("the listing carries governance's word %q — the two planes report apart (TR12's table)", leak)
		}
	}
	rres, err := d.Table.TriggerResume(context.Background(), protocol.Actor{Kind: "user", ID: "operator"}, "paused-dial")
	if err != nil {
		t.Fatal(err)
	}
	if rr := rres.(protocol.TriggerControlResult); rr.Paused {
		t.Fatal("resume did not lift the bar")
	}
	gres, err := d.Table.TriggerPause(context.Background(), protocol.Actor{Kind: "user", ID: "operator"}, "ghost")
	if err != nil {
		t.Fatal(err)
	}
	if g := gres.(protocol.TriggerControlResult); g.Found || g.Reason == "" {
		t.Fatalf("an unknown id answers with a sentence, not an error, and leaks nothing: %+v", g)
	}
}

func TestSocketHistoryShowsFiresAndGapsAsSeparateAnswers(t *testing.T) {
	// The five answers AND the silence between them (0034): fires plus the
	// gap range — TR4 complete, from the seam `aeon trigger history` reads.
	s := recoveryStore(t)
	d := New(Options{Store: s})
	ws := t.TempDir()
	seedSchedule(t, s, "dial-hist", ws)
	ctx := context.Background()
	if err := s.RecordTriggerFire(ctx, store.TriggerFireRow{
		ScheduleID: "dial-hist", Instant: "2026-10-27T04:00:00Z", Outcome: "skipped",
		Reason: "the previous run is still going (loop X) — an overlapping fire is skipped and recorded (TR5)",
		At:     "2026-10-27T04:00:05Z",
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveTriggerGap(ctx, store.TriggerGapRow{
		ScheduleID: "dial-hist", CoveredFrom: "2026-10-25T04:00:00Z", CoveredTo: "2026-10-26T04:00:00Z",
		Missed: 1, At: "2026-10-27T08:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	raw, err := d.Table.TriggerHistory(ctx, "dial-hist", 50)
	if err != nil {
		t.Fatal(err)
	}
	h := raw.(protocol.TriggerHistoryResult)
	if len(h.Fires) != 1 || h.Fires[0].Outcome != "skipped" || !strings.Contains(h.Fires[0].Reason, "TR5") {
		t.Fatalf("the fire answers: %+v", h.Fires)
	}
	if len(h.Gaps) != 1 || h.Gaps[0].Missed != 1 {
		t.Fatalf("the gap: %+v", h.Gaps)
	}
}

func TestSecondFireNowWhileTheLoopRunsIsSkippedAndRecorded(t *testing.T) {
	// Finding #4's other half (T9 walk): fire-now used to truncate its
	// instant to the minute, which collided with the tick's
	// UNIQUE(schedule_id, instant) row AND hid the first fire from the
	// overlap query ("what fired BEFORE this instant?" cannot see a row AT
	// it). Two fire-nows a breath apart over the REAL store must show
	// fired-then-skipped, both recorded — TR5 with the clock's own honesty.
	s := recoveryStore(t)
	d := New(Options{Store: s})
	seedTriggerAllow(t, s, "twice")
	ws := t.TempDir()
	seedSchedule(t, s, "twice", ws)
	first := callNow(t, d, "twice")
	if first.Outcome != "fired" || first.LoopID == "" {
		t.Fatalf("the first fire-now must fire: %+v", first)
	}
	second := callNow(t, d, "twice")
	if second.Outcome != "skipped" || !strings.Contains(second.Reason, "still going") || !strings.Contains(second.Reason, "TR5") {
		t.Fatalf("the second fire-now must be the clock's own overlap answer: %+v", second)
	}
	raw, err := d.Table.TriggerHistory(context.Background(), "twice", 50)
	if err != nil {
		t.Fatal(err)
	}
	h := raw.(protocol.TriggerHistoryResult)
	if len(h.Fires) != 2 {
		t.Fatalf("both answers must be in the record (fire-now is a fire, whoever pressed): %+v", h.Fires)
	}
}

func callNow(t *testing.T, d *Daemon, id string) protocol.TriggerControlResult {
	t.Helper()
	raw, err := d.Table.TriggerFireNow(context.Background(), protocol.Actor{Kind: "user", ID: "operator"}, id)
	if err != nil {
		t.Fatal(err)
	}
	return raw.(protocol.TriggerControlResult)
}
