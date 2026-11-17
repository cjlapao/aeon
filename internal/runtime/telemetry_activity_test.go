package runtime

import (
	"context"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

// S33 T6 (TL8, D5+D6) — the activity rows come from what the tools ALREADY
// reported, read at the one dispatch choke. These run the real runTool;
// the spy's scripted outcomes stand in for the tools' fields (the tools'
// own population is pinned on the tools' side in internal/tools).

func activityState(t *testing.T, h StepHandler) *LoopState {
	t.Helper()
	st := testState(h, nil)
	st.Request.StepID = 77
	return st
}

func runToolRound(t *testing.T, tool, input string, outcome ToolOutcome, granted ...string) *telemetryRecorder {
	t.Helper()
	spy := newSpyTools(granted...).with(tool, outcome)
	client := newFakeClient(
		scriptedTurn{ToolUses: []model.Block{toolUse("t1", tool, input)}},
		scriptedTurn{Text: okHandoff},
	)
	rec := &telemetryRecorder{}
	st := activityState(t, testHandler(tool))
	res := RunLoop(context.Background(), st, LoopDeps{
		Client: client, Tools: spy,
		HandoffSpec: HandoffSpec{Required: []string{"status"}},
		Telemetry:   rec,
	})
	if res.Outcome != OutcomeOK {
		t.Fatalf("outcome %s: %s", res.Outcome, res.Reason)
	}
	return rec
}

func TestFileActivityCapturedFromExistingFields(t *testing.T) {
	added, removed := 7, 2
	rec := runToolRound(t, "write", `{"path":"a.md","content":"x"}`, ToolOutcome{
		Content: "wrote a.md (1 bytes)", Path: "a.md", LinesAdded: added, LinesRemoved: removed,
	}, "write")
	if len(rec.fileRows) != 1 {
		t.Fatalf("%d file rows for one write call, want exactly one (no double-count): %+v", len(rec.fileRows), rec.fileRows)
	}
	row := rec.fileRows[0]
	if row.stepID != 77 || row.path != "a.md" || row.action != "write" {
		t.Errorf("row identity wrong: %+v", row)
	}
	// The tool's OWN counts ride verbatim — the pointers the loop passed
	// carry the numbers the result carried, not re-derived guesses.
	if row.added == nil || *row.added != 7 || row.removed == nil || *row.removed != 2 {
		t.Errorf("line counts did not ride across: added=%v removed=%v", row.added, row.removed)
	}
}

func TestReadToolNeverProducesFileActivity(t *testing.T) {
	// The Path-is-write-shaped assumption, pinned from the OTHER side: a
	// read reports no write-shape fields, so no row. If `read` ever started
	// setting Path, this test fires before the readout starts reporting
	// reads as writes.
	rec := runToolRound(t, "read", `{"path":"a.md"}`, ToolOutcome{Content: "the file's text"}, "read")
	if len(rec.fileRows) != 0 {
		t.Fatalf("a read produced %d file rows — reads are not writes: %+v", len(rec.fileRows), rec.fileRows)
	}
	if rec.commands != 0 {
		t.Errorf("a read produced %d command rows", rec.commands)
	}
}

func TestEditDoesNotYetEmitWriteShape(t *testing.T) {
	// The FINDING, pinned so its eventual fix is a decision and not an
	// accident: `edit` populates no Path/line fields today (tools/edit.go's
	// success return), so edits land NO file-activity row. Reusing existing
	// fields means capturing what tools REPORT — making edit report them is
	// a tools-side change (S31's home), not this task's zero-new-plumbing
	// half. A future tools change SHOULD flip this test to a row assertion.
	rec := runToolRound(t, "edit", `{"path":"a.md","old_string":"x","new_string":"y"}`,
		ToolOutcome{Content: "edited a.md (1 bytes)"}, "edit")
	if len(rec.fileRows) != 0 {
		t.Fatalf("edit started emitting write-shaped results — flip this pin deliberately: %+v", rec.fileRows)
	}
}

func TestCommandActivityCapturedForBash(t *testing.T) {
	for _, tc := range []struct {
		name string
		exit int
	}{
		{"failure exits are data", 2},
		{"spawned-and-failed commands are runs", 127},
	} {
		t.Run(tc.name, func(t *testing.T) {
			exit := tc.exit
			rec := runToolRound(t, "bash", `{"command":"make test"}`, ToolOutcome{
				Content: "make: *** [test] Error 1", ExitCode: &exit, WallMs: 150,
			}, "bash")
			if len(rec.commandRows) != 1 {
				t.Fatalf("%d command rows, want exactly 1: %+v", len(rec.commandRows), rec.commandRows)
			}
			row := rec.commandRows[0]
			if row.stepID != 77 || row.command != "make test" {
				t.Errorf("row identity wrong: %+v", row)
			}
			if row.exitCode == nil || *row.exitCode != tc.exit {
				t.Errorf("exit code did not ride: %v, want %d", row.exitCode, tc.exit)
			}
			if row.wallMs != 150 {
				t.Errorf("the spawn-measured wall did not ride: %d, want 150", row.wallMs)
			}
		})
	}
}

func TestSuccessfulBashStillRecorded(t *testing.T) {
	// The Do-NOT made executable: an `exit 0` is the MOST important row D6
	// wants (commands_run, not just commands_failed). Gating on anything
	// resembling "something interesting happened" skips exactly this.
	exit := 0
	rec := runToolRound(t, "bash", `{"command":"ls"}`, ToolOutcome{
		Content: "a.md\nb.md\n", ExitCode: &exit, WallMs: 12,
	}, "bash")
	if len(rec.commandRows) != 1 {
		t.Fatalf("a successful bash command recorded %d rows, want 1 — success is not a reason to skip", len(rec.commandRows))
	}
	row := rec.commandRows[0]
	if row.exitCode == nil || *row.exitCode != 0 || row.command != "ls" {
		t.Errorf("exit-0 row corrupted: %+v", row)
	}
}

func TestNeverStartedCommandIsNotARow(t *testing.T) {
	// nil ExitCode = the command never STARTED (tool-level refusal to
	// spawn): a policy/no-shell outcome is not a run (T4's refusal
	// principle), and counting it would inflate the run number D6 exists
	// to measure.
	rec := runToolRound(t, "bash", `{"command":"rm -rf /"}`, ToolOutcome{
		Content: "bash could not run the command: no token", IsError: true,
	}, "bash")
	if len(rec.commandRows) != 0 {
		t.Fatalf("a command that never started produced %d command rows: %+v", len(rec.commandRows), rec.commandRows)
	}
}

func TestActivityCaptureIsNilSafe(t *testing.T) {
	// The same write+bash-shaped rounds with no sink at all: the step runs,
	// nothing panics on the nil interface, no clock bracket is taken (I2
	// — telemetry-off costs nothing it did not cost before T6 existed).
	spy := newSpyTools("write").with("write", ToolOutcome{Content: "wrote a.md", Path: "a.md", LinesAdded: 1})
	client := newFakeClient(
		scriptedTurn{ToolUses: []model.Block{toolUse("t1", "write", `{"path":"a.md"}`)}},
		scriptedTurn{Text: okHandoff},
	)
	st := activityState(t, testHandler("write"))
	res := RunLoop(context.Background(), st, LoopDeps{
		Client: client, Tools: spy,
		HandoffSpec: HandoffSpec{Required: []string{"status"}},
	})
	if res.Outcome != OutcomeOK {
		t.Fatalf("nil-sink activity round failed: %s — %s", res.Outcome, res.Reason)
	}
}
