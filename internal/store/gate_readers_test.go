package store

// gate_readers_test.go — S57 T8's two gate-plane readers: LoopsBySlug
// (the daemon resolves a loop slug to its root — a slug is unique per
// root, never global) and LatestRunForCommand (verify.list's per-gate
// last-run column: a gate never run answers nil, not a row).

import (
	"testing"
	"time"
)

// TestLoopsBySlugResolvesPerRoot — the slug is a per-root identity:
// the same slug in two roots answers BOTH rows (ordered by root), one
// loop answers its one row, and a slug nobody named answers empty, not
// an error — the daemon's three states (one, many, none) are the
// service's typed refusals (loop-unknown, loop-slug-ambiguous) and this
// reader is the fact they stand on.
func TestLoopsBySlugResolvesPerRoot(t *testing.T) {
	g, s := testStore(t)
	seedComponent(t, g, "one")
	seedComponent(t, g, "two")

	// The same slug in two roots: the per-root uniqueness, not a global one.
	seedLoop(t, s, "alpha", "one")
	seedLoop(t, s, "alpha", "two")
	// A slug in only one root, beside another loop with a different slug.
	seedLoop(t, s, "beta", "one")

	both, err := s.LoopsBySlug("alpha")
	if err != nil {
		t.Fatalf("LoopsBySlug(alpha): %v", err)
	}
	if len(both) != 2 {
		t.Fatalf("a slug in two roots answered %d rows — the slug is per-root, not global", len(both))
	}
	if both[0].ComponentRoot != "one" || both[1].ComponentRoot != "two" {
		t.Fatalf("the two rows came back out of root order: %q then %q", both[0].ComponentRoot, both[1].ComponentRoot)
	}

	one, err := s.LoopsBySlug("beta")
	if err != nil {
		t.Fatalf("LoopsBySlug(beta): %v", err)
	}
	if len(one) != 1 || one[0].ComponentRoot != "one" || one[0].Slug != "beta" {
		t.Fatalf("a slug in one root answered %+v — the daemon takes its root from this row", one)
	}

	none, err := s.LoopsBySlug("nowhere")
	if err != nil {
		t.Fatalf("a slug nobody named is the honest empty, not an error: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("a missing slug answered %d rows", len(none))
	}
}

// TestLatestRunForCommand — the per-gate last-run read: a command nobody
// ran answers (zero, false, nil) — the list's nil LastRun, not an empty
// row; the newest FINISHED run over the command wins (the run the list
// shows is the run that last happened); and a run whose results name no
// command is not a run of any command (the join is on the result's
// command_id, not the run's).
func TestLatestRunForCommand(t *testing.T) {
	g, s := testStore(t)
	seedComponent(t, g, "rn-root")
	const cmd = "vcm-last"
	started := time.Date(2026, 11, 2, 1, 0, 0, 0, time.UTC)

	// A command nobody ran: the honest no, no row, no error.
	if vr, ok, err := s.LatestRunForCommand("vcm-never"); err != nil || ok {
		t.Fatalf("a never-run command answered (vr=%+v ok=%v err=%v) — the list wants nil", vr, ok, err)
	}

	// One run per slot: the run slot is unique per
	// (loop, root, position, attempt), so the two runs take two positions.
	// The first finishes at +1m, the second at +2m — the second is the
	// run that last happened.
	finishAt := func(position int, offset time.Duration) {
		draft := s57NewDraft(t, s, NewVerifyRun{
			LoopSlug: "alpha", ComponentRoot: "rn-root", ComponentID: "rn-root", Task: "VT1",
			Position: position, Attempt: 1, HeadSHA: "abc123", Trigger: "loop",
			Actor:     s57System,
			StartedAt: started,
		})
		if err := draft.AppendVerifyResult(VerifyRunResult{
			CommandID: s57ptr(cmd), Name: "unit", Requirement: "required",
			Command: "go test ./...", State: "pass",
		}); err != nil {
			t.Fatalf("append the result: %v", err)
		}
		if _, err := s.FinishVerifyRun(draft, "pass", "", "", "", started.Add(offset)); err != nil {
			t.Fatalf("finish the position %d run: %v", position, err)
		}
	}
	finishAt(0, time.Minute)
	finishAt(1, time.Minute*2)

	vr, ok, err := s.LatestRunForCommand(cmd)
	if err != nil || !ok {
		t.Fatalf("a command with two finished runs answered (ok=%v err=%v)", ok, err)
	}
	// Position 1 is the run that finished later (+2m): the newest
	// finished run is the one the list shows, ordered by ended_at.
	if vr.Position != 1 || vr.EndedAt == nil {
		t.Fatalf("the list showed %+v — the newest finished run (position 1) wins, ordered by ended_at", vr)
	}

	// A run whose result names no command (a manual gate run with no
	// command id) is not a run of any command: the join is on the
	// result's command_id, and an empty one matches nothing.
	other := s57NewDraft(t, s, NewVerifyRun{
		LoopSlug: "alpha", ComponentRoot: "rn-root", ComponentID: "rn-root", Task: "VT1",
		Position: 2, Attempt: 1, HeadSHA: "abc123", Trigger: "manual",
		Actor:     s57System,
		StartedAt: started.Add(time.Hour),
	})
	if err := other.AppendVerifyResult(VerifyRunResult{Name: "loose", Requirement: "required", Command: "true", State: "pass"}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if _, err := s.FinishVerifyRun(other, "pass", "", "", "", started.Add(time.Hour+time.Minute)); err != nil {
		t.Fatalf("finish the command-less run: %v", err)
	}
	vr, ok, err = s.LatestRunForCommand(cmd)
	if err != nil || !ok || vr.Position != 1 {
		t.Fatalf("a command-less run displaced the newest run of the command (vr=%+v ok=%v err=%v)", vr, ok, err)
	}
}
