package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/runtime"
)

func TestSuspendedResultPrintsQuestionOptionsAndCopyableAnswerCommand(t *testing.T) {
	var out bytes.Buffer
	res := protocol.StepResult{StepID: 42, Kind: "suspended", Question: &runtime.PendingQuestion{
		Body: "Which approach?", Options: []string{"in-place", "new-table"},
	}}
	if err := renderStepResult(&out, res, false); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Which approach?", "in-place", "new-table", "suspended · waiting for you", "aeon answer 42"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q from %s", want, out.String())
		}
	}
}

func TestAnswerChoiceListsTheAllowedValuesAndAcceptsEmptyFreeForm(t *testing.T) {
	q := &runtime.PendingQuestion{Options: []string{"one", "two"}}
	if err := validateAnswerChoice(7, q, "third"); err == nil || !strings.Contains(err.Error(), "one | two") {
		t.Fatalf("invalid choice: %v", err)
	}
	if err := validateAnswerChoice(7, q, "one"); err != nil {
		t.Fatal(err)
	}
	if err := validateAnswerChoice(7, &runtime.PendingQuestion{}, ""); err != nil {
		t.Fatalf("empty free-form answer: %v", err)
	}
	pos, _ := splitPositional([]string{"7", ""}, 2)
	if len(pos) != 2 || pos[1] != "" {
		t.Fatalf("empty CLI argument was lost: %q", pos)
	}
}

func TestChangedFileAndResumeCostAreVisible(t *testing.T) {
	msgs := []model.Message{{Role: model.RoleUser, Content: []model.Block{model.TextBlock("Changed while this step waited: README.md. The original file content remains in the earlier transcript.")}}}
	if note := changedFileNote(msgs); !strings.Contains(note, "README.md") {
		t.Fatalf("changed note: %q", note)
	}
	line := resumedCostLine(protocol.CostView{State: "known", USD: 0.0043}, []runtime.CompactionRecord{{Cost: model.Cost{State: model.CostKnown, USD: 0.00062}}})
	// S18 T7: the pinned figures used to be "$0.00430" and "+$0.00062" —
	// strings that begin "$0.00", which D9 forbids in ANY cost rendering
	// (a real 0.43-cent charge must not wear the look of free; sub-cent
	// figures now print scientific). The test's point — the figures ARE
	// visible on the line — is unchanged.
	for _, want := range []string{"$4.30e-03", "1 resume", "1 compaction", "+$6.20e-04"} {
		if !strings.Contains(line, want) {
			t.Errorf("cost line misses %q: %s", want, line)
		}
	}
}

func TestCompactionsViewShowsRangeEstimateAndSummary(t *testing.T) {
	raw, err := json.Marshal(map[string]any{"compactions": []runtime.CompactionRecord{{Start: 2, End: 8, EstimateBefore: 900, EstimateAfter: 350, Summary: "The user chose the new table."}}})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := renderStepCompactions(&out, raw); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"2..8", "900", "350", "The user chose the new table."} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("compaction view misses %q: %s", want, out.String())
		}
	}
}

func TestAnswerCLIThroughRealDaemonAndReplayProvider(t *testing.T) {
	handoff := filepath.Join(repoRoot(t), "internal/model/testdata/anthropic/step-handoff.sse")
	d, _ := startDaemonWithRoute(t, "anthropic/step-question.sse", "-fixture", handoff)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("S6 replay read"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := d.mustRun("step", "--allow-question", "--workspace", root, "--route", "anthropic", "choose a path")
	if !strings.Contains(out, "Which approach?") || !strings.Contains(out, "new-table") {
		t.Fatalf("suspension did not show the question:\n%s", out)
	}
	match := regexp.MustCompile(`aeon answer ([0-9]+)`).FindStringSubmatch(out)
	if len(match) != 2 {
		t.Fatalf("no copyable answer command:\n%s", out)
	}
	id := match[1]
	showPending := d.mustRun("step", "show", id, "--json")
	jq := exec.Command("jq", "[.[] | .content[]? | select(.kind==\"tool_result\")] | length")
	jq.Stdin = strings.NewReader(showPending)
	count, err := jq.CombinedOutput()
	if err != nil || strings.TrimSpace(string(count)) != "1" {
		t.Fatalf("sibling tool result count: %v %s\n%s", err, count, showPending)
	}
	var pending []model.Message
	if err := json.Unmarshal([]byte(showPending), &pending); err != nil {
		t.Fatal(err)
	}
	uses, results := questionBlocks(pending)
	if uses != 1 || results != 0 {
		t.Fatalf("before answer: %d question uses, %d answers", uses, results)
	}
	if process, err := exec.Command("pgrep", "-fa", regexp.QuoteMeta(aeonBinary)+".*step").CombinedOutput(); err == nil || len(strings.TrimSpace(string(process))) > 0 {
		t.Fatalf("a step client process remained after suspension: %v\n%s", err, process)
	}
	if invalid, err := d.run("answer", id, "neither"); err == nil || !strings.Contains(invalid, "in-place | new-table") {
		t.Fatalf("invalid choice: %v\n%s", err, invalid)
	}
	resumed := d.mustRun("answer", id, "new-table")
	if !strings.Contains(resumed, "resumed") || !strings.Contains(resumed, "1 resume") || !strings.Contains(resumed, "cost") {
		t.Fatalf("resume output:\n%s", resumed)
	}
	var after []model.Message
	if err := json.Unmarshal([]byte(d.mustRun("step", "show", id, "--json")), &after); err != nil {
		t.Fatal(err)
	}
	uses, results = questionBlocks(after)
	if uses != 1 || results != 1 {
		t.Fatalf("after answer: %d question uses, %d answers", uses, results)
	}
	if again, err := d.run("answer", id, "in-place"); err == nil || !strings.Contains(again, "already answered at") {
		t.Fatalf("second answer: %v\n%s", err, again)
	}
	show := d.mustRun("step", "show", id, "--compactions")
	if !strings.Contains(show, "no compactions recorded") {
		t.Fatalf("compaction view:\n%s", show)
	}
}

func questionBlocks(messages []model.Message) (uses, results int) {
	for _, msg := range messages {
		for _, b := range msg.Content {
			if b.Kind == model.BlockToolUse && b.Name == "question" {
				uses++
			}
			if b.Kind == model.BlockToolResult && b.ToolUseID == "toolu_QUESTION_FIXTURE" {
				results++
			}
		}
	}
	return
}
