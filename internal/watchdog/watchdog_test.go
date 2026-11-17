package watchdog

import (
	"testing"
	"time"
)

// TestEvaluateTable is the pure half of T9's behaviour table: every signal,
// at and around its threshold, across the episode shapes, with the exact
// firings pinned. It is the surface a wrong threshold comparison, a missed
// signal, or a broken episode would all show up in — so the cases read as the
// spec's own argument rather than a list of asserts.
func TestEvaluateTable(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	fifteenMinAgo := now.Add(-15 * time.Minute)
	anHourAgo := now.Add(-60 * time.Minute)
	deadlineAgo := now.Add(-5 * time.Minute)
	stepStart := now.Add(-30 * time.Minute)
	runSince := now.Add(-20 * time.Minute)

	workspaceNotify15 := Rule{
		ID: "wd-notify15", Scope: ScopeWorkspace, SubjectID: "workspace",
		Signal: SignalNoActivity, Threshold: 15 * 60 * 1000, Action: ActionNotify,
		Enabled: true, Origin: OriginSystem,
	}
	workspaceStop60 := Rule{
		ID: "wd-stop60", Scope: ScopeWorkspace, SubjectID: "workspace",
		Signal: SignalNoActivity, Threshold: 60 * 60 * 1000, Action: ActionStop,
		Enabled: true, Origin: OriginSystem,
	}
	componentSamePos6 := Rule{
		ID: "wd-samepos6", Scope: ScopeComponent, SubjectID: "/repo/alpha",
		Signal: SignalSamePosition, Threshold: 6, Action: ActionStop,
		Enabled: true, Origin: OriginSystem,
	}
	workspaceReason3 := Rule{
		ID: "wd-reason3", Scope: ScopeWorkspace, SubjectID: "workspace",
		Signal: SignalRepeatBlockReason, Threshold: 3, Action: ActionStop,
		Enabled: true, Origin: OriginSystem,
	}
	workspaceDeadline0 := Rule{
		ID: "wd-deadline0", Scope: ScopeWorkspace, SubjectID: "workspace",
		Signal: SignalStepOverDeadline, Threshold: 0, Action: ActionBlock,
		Enabled: true, Origin: OriginSystem,
	}

	base := Observation{
		LoopID: "l1", Status: "running",
		ComponentRoot: "/repo/alpha", Root: "/repo",
	}

	cases := []struct {
		name    string
		rules   []Rule
		obs     Observation
		now     time.Time
		wantIDs []string // the firing rule ids, in rule order; empty = no firing
	}{
		{
			name:  "no-activity under threshold does not fire",
			rules: []Rule{workspaceNotify15},
			obs:   base,
			// ten minutes of silence against a 15-minute line: measurable,
			// under threshold — the honest no.
			wantIDs: nil,
		},
		{
			name:  "no-activity at exactly the threshold fires",
			rules: []Rule{workspaceNotify15},
			obs: Observation{
				LoopID: "l1", Status: "running",
				ComponentRoot: "/repo/alpha", Root: "/repo",
				LastActivityAt: fifteenMinAgo,
			},
			now:     now,
			wantIDs: []string{"wd-notify15"},
		},
		{
			name:  "ladder: an hour of silence fires both the notify and the stop",
			rules: []Rule{workspaceNotify15, workspaceStop60},
			obs: Observation{
				LoopID: "l1", Status: "running",
				ComponentRoot: "/repo/alpha", Root: "/repo",
				LastActivityAt: anHourAgo,
			},
			now:     now,
			wantIDs: []string{"wd-notify15", "wd-stop60"},
		},
		{
			name:    "no-activity with no activity source does not fire",
			rules:   []Rule{workspaceNotify15},
			obs:     base, // LastActivityAt zero: not measurable, not a repeat
			wantIDs: nil,
		},
		{
			name:  "same-position under the count does not fire",
			rules: []Rule{componentSamePos6},
			obs:   base,
			// five dispatches of the same position: under the 6-line.
			// (PositionDispatches defaults to 0 here; set below explicitly.)
			wantIDs: nil,
		},
		{
			name:  "same-position at the count fires, scoped to its component",
			rules: []Rule{componentSamePos6},
			obs: Observation{
				LoopID: "l1", Status: "running",
				ComponentRoot: "/repo/alpha", Root: "/repo",
				Position:           Position{Lane: "main", Item: "t1", Step: "s1"},
				PositionDispatches: 6,
			},
			now:     now,
			wantIDs: []string{"wd-samepos6"},
		},
		{
			name:  "component rule does not match another component",
			rules: []Rule{componentSamePos6},
			obs: Observation{
				LoopID: "l2", Status: "running",
				ComponentRoot: "/repo/beta", Root: "/repo",
				Position:           Position{Lane: "main", Item: "t1", Step: "s1"},
				PositionDispatches: 9,
			},
			now:     now,
			wantIDs: nil,
		},
		{
			name: "root-scoped component rule covers its subtree",
			rules: []Rule{{
				ID: "wd-root", Scope: ScopeComponent, SubjectID: "/repo",
				Signal: SignalSamePosition, Threshold: 2, Action: ActionStop,
				Enabled: true, Origin: OriginUser,
			}},
			obs: Observation{
				LoopID: "l1", Status: "running",
				ComponentRoot: "/repo/alpha", Root: "/repo",
				Position:           Position{Lane: "main", Item: "t1", Step: "s1"},
				PositionDispatches: 3,
			},
			now:     now,
			wantIDs: []string{"wd-root"},
		},
		{
			name:    "repeat-block-reason with no run does not fire",
			rules:   []Rule{workspaceReason3},
			obs:     base, // ReasonRepeats 0: nothing has been blocked
			wantIDs: nil,
		},
		{
			name:  "repeat-block-reason at the count fires",
			rules: []Rule{workspaceReason3},
			obs: Observation{
				LoopID: "l1", Status: "running",
				ComponentRoot: "/repo/alpha", Root: "/repo",
				LastReasonHash: "hash-a", ReasonRepeats: 3, ReasonRunSince: runSince,
			},
			now:     now,
			wantIDs: []string{"wd-reason3"},
		},
		{
			name:  "step-over-deadline with no deadline does not fire",
			rules: []Rule{workspaceDeadline0},
			obs: Observation{
				LoopID: "l1", Status: "running",
				ComponentRoot: "/repo/alpha", Root: "/repo",
				StepRunningSince: stepStart, // no StepDeadline: the signal is off
			},
			now:     now,
			wantIDs: nil,
		},
		{
			name: "step-over-deadline inside the grace does not fire",
			rules: []Rule{{
				ID: "wd-grace10", Scope: ScopeWorkspace, SubjectID: "workspace",
				Signal: SignalStepOverDeadline, Threshold: 10 * 60 * 1000,
				Action: ActionBlock, Enabled: true, Origin: OriginUser,
			}},
			obs: Observation{
				LoopID: "l1", Status: "running",
				ComponentRoot: "/repo/alpha", Root: "/repo",
				StepRunningSince: stepStart,
				StepDeadline:     now.Add(-2 * time.Minute), // 2m over, 10m grace: inside
			},
			now:     now,
			wantIDs: nil,
		},
		{
			name:  "step-over-deadline past the deadline + grace fires",
			rules: []Rule{workspaceDeadline0},
			obs: Observation{
				LoopID: "l1", Status: "running",
				ComponentRoot: "/repo/alpha", Root: "/repo",
				StepRunningSince: stepStart,
				StepDeadline:     deadlineAgo,
			},
			now:     now,
			wantIDs: []string{"wd-deadline0"},
		},
		{
			name: "a disabled rule never fires even when crossed",
			rules: []Rule{
				workspaceNotify15,
				{ID: "wd-off", Scope: ScopeWorkspace, SubjectID: "workspace",
					Signal: SignalNoActivity, Threshold: 0, Action: ActionBlock,
					Enabled: false, Origin: OriginUser},
			},
			obs: Observation{
				LoopID: "l1", Status: "running",
				ComponentRoot: "/repo/alpha", Root: "/repo",
				LastActivityAt: anHourAgo, // an hour of silence: the 15-minute rule is crossed
			},
			now:     now,
			wantIDs: []string{"wd-notify15"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Evaluate(tc.rules, tc.obs, tc.now)
			var gotIDs []string
			for _, f := range got {
				gotIDs = append(gotIDs, f.Rule.ID)
			}
			if len(gotIDs) != len(tc.wantIDs) {
				t.Fatalf("firings = %v, want %v", gotIDs, tc.wantIDs)
			}
			for i := range gotIDs {
				if gotIDs[i] != tc.wantIDs[i] {
					t.Fatalf("firings = %v, want %v", gotIDs, tc.wantIDs)
				}
			}
		})
	}
}

// TestEvaluateEpisodeResetsOnProgress pins the once-per-episode argument at
// the pure level: the same rule and the same threshold produce a DIFFERENT
// episode when the loop has progressed, so the service's dedupe (T9) drops
// the repeat and admits the fresh one.
func TestEvaluateEpisodeResetsOnProgress(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	notify := Rule{
		ID: "wd-notify", Scope: ScopeWorkspace, SubjectID: "workspace",
		Signal: SignalNoActivity, Threshold: 0, Action: ActionNotify,
		Enabled: true, Origin: OriginSystem,
	}

	silent := Observation{
		LoopID: "l1", Status: "running", ComponentRoot: "/r", Root: "/r",
		LastActivityAt: now.Add(-20 * time.Minute),
	}
	f1 := Evaluate([]Rule{notify}, silent, now)
	if len(f1) != 1 {
		t.Fatalf("expected the silence to fire, got %v", f1)
	}

	// The loop does something (LastActivityAt moves) and then goes quiet
	// again past the threshold: a new episode, so the service may fire once
	// more. Same rule, same threshold — only the episode differs.
	progressed := silent
	progressed.LastActivityAt = now.Add(-2 * time.Minute)
	f2 := Evaluate([]Rule{notify}, progressed, now)
	if len(f2) != 1 {
		t.Fatalf("expected the fresh silence to fire, got %v", f2)
	}
	if f1[0].Episode == f2[0].Episode {
		t.Fatalf("progress did not reset the episode: %q == %q", f1[0].Episode, f2[0].Episode)
	}

	// And the same observation twice is the same episode: the dedupe's input.
	f3 := Evaluate([]Rule{notify}, silent, now)
	if f1[0].Episode != f3[0].Episode {
		t.Fatalf("identical observations gave different episodes: %q vs %q", f1[0].Episode, f3[0].Episode)
	}
}

// TestPositionHashIsStableAndDiscriminating pins the position identity the
// same-position signal counts against: the same position hashes the same, a
// different one does not, and the field order is part of the identity (a lane
// and an item that merely swap are not the same place).
func TestPositionHashIsStableAndDiscriminating(t *testing.T) {
	a := Position{Lane: "main", Item: "t1", Step: "s1"}
	b := Position{Lane: "main", Item: "t1", Step: "s1"}
	c := Position{Lane: "main", Item: "t1", Step: "s2"}
	swapped := Position{Lane: "t1", Item: "main", Step: "s1"}
	if a.Hash() != b.Hash() {
		t.Fatalf("equal positions hashed differently: %q vs %q", a.Hash(), b.Hash())
	}
	if a.Hash() == c.Hash() {
		t.Fatalf("different steps hashed the same: %q", a.Hash())
	}
	if a.Hash() == swapped.Hash() {
		t.Fatalf("swapped lane/item hashed the same as the original: %q", a.Hash())
	}
}
