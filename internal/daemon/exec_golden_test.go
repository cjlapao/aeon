package daemon

// exec_golden_test.go — S59 T1 (characterization, P2 Phase A). The END-TO-END
// shape of one authorized bash call, pinned on the untouched code: a real
// store, a real daemon, a real runtime, a real toolset, a real `bash`
// process — and the record of what the machine wrote down:
//
//   - the transcript rows, in order: who said what, and the tool_result's
//     exact model-facing bytes (the renderRun layout, wall time normalized),
//   - the `step.tool.start` / `step.tool.end` event payloads: the exact
//     KEY SET (an additive event change must show as a golden diff) and the
//     stable VALUES,
//   - the ordering: the start before the end, both on this step, this loop,
//     this tool_use id.
//
// T5 changes what the child SEES (the environment), not what the machine
// RECORDS: T5 must leave every golden here byte-identical. The one thing
// T5 adds to this walk (the environment's refusal, EV2) arrives as a NEW
// golden, not a change to this one.
//
// Generation (run once, on the untouched tree):
//
//	AEON_WRITE_GOLDEN=1 go test ./internal/daemon/ -run TestExecEndToEndShapesGolden -count=1 -tags sqlite_fts5

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/protocol"
)

const goldExecDir = "exec_e2e"

// execWallRe normalizes a rendered duration the way the tools' goldens do:
// every byte but the clock reading is pinned, including the clock's place.
var execWallRe = regexp.MustCompile(`\d+(?:\.\d+)?(?:ns|µs|ms|s)`)

type execGoldTranscript struct {
	Role    string   `json:"role"`
	Kinds   []string `json:"kinds"`
	Content string   `json:"content,omitempty"` // text blocks and the tool_result
}

type execGoldRow struct {
	Transcript []execGoldTranscript `json:"transcript"`
	StartKeys  []string             `json:"startKeys"`
	EndKeys    []string             `json:"endKeys"`
}

// TestExecEndToEndShapesGolden — one authorized `echo hi` through the whole
// stack, holding the record to its shape.
func TestExecEndToEndShapesGolden(t *testing.T) {
	write := os.Getenv("AEON_WRITE_GOLDEN") == "1"
	goldPath := filepath.Join("testdata", goldExecDir, "golden.json")
	if write {
		if err := os.MkdirAll(filepath.Dir(goldPath), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	s := recoveryStoreBound(t)
	root := t.TempDir()
	// Both questions the gate asks of a bash call: the tool question and
	// the command question, seeded through the real write boundary.
	seedRules(t, s, time.Now().UTC(),
		toolRulePolicy("exec-gold-bash", "chat", "bash", policy.Allow),
		commandRulePolicy("exec-gold-echo", "chat", "echo hi", policy.Allow))

	m := &scriptedModel{turns: []modelTurn{
		{toolUses: []model.Block{toolUse("u1", "bash", `{"command":"echo hi"}`)}},
		{text: `{"status":"ok","summary":"ran echo hi"}`},
	}}
	d, w := lcDaemon(t, s, m)
	// The events are a stream: they reach a SUBSCRIBED connection, in order,
	// tagged with the subscription. The watcher connects and subscribes
	// before the loop starts, so it cannot miss the start.
	initConn(t, d, context.Background(), "gold-watcher", "golden")
	sub := subscribe(t, d, "gold-watcher", protocol.SubscribeParams{})
	loopID := lcStart(t, d, lcBlueprint(t, "chat"), root, "exec-golden")
	stepID := waitStepInFlight(t, s, loopID)

	// The loop settles on its own; the drain at the end takes whatever
	// runner it left, so the test leaves no process behind.
	t.Cleanup(func() { _ = d.Drain(context.Background()) })

	// A step that finishes with an `ok` status is a lane that is done,
	// and a done lane's loop is `done`.
	deadline := time.Now().Add(15 * time.Second)
	for {
		st, err := loop.Pinned(context.Background(), s, loopID)
		if err == nil && st.Status == "done" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the loop never settled (status %q): the golden would read a partial record", st.Status)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// --- the transcript -------------------------------------------------
	tr := s.ReadTranscript(stepID)
	if !tr.OK {
		t.Fatalf("the transcript will not read: %s", tr.Reason())
	}

	got := execGoldRow{}
	for _, msg := range tr.Messages {
		row := execGoldTranscript{Role: string(msg.Role)}
		for _, b := range msg.Content {
			row.Kinds = append(row.Kinds, string(b.Kind))
			switch b.Kind {
			case model.BlockText:
				row.Content += b.Text
			case model.BlockToolResult:
				row.Content += b.Content
			}
		}
		row.Content = execWallRe.ReplaceAllString(row.Content, "N")
		got.Transcript = append(got.Transcript, row)
	}

	// Live assertions on the transcript shape: the roles and the block
	// kinds, the ids that tie the call to its result. The row order is the
	// five of a tool step on a "chat" handler: the system prompt (the
	// handler body), the person's instruction (the loop's goal), the
	// assistant's call, the result answering it, the assistant's turn.
	wantRoles := []string{"system", "user", "assistant", "user", "assistant"}
	if tr.Count != len(wantRoles) || len(tr.Messages) != len(wantRoles) {
		t.Fatalf("the transcript has %d rows (%d read), want the five of a tool step: %v", tr.Count, len(tr.Messages), got.Transcript)
	}
	for i, want := range wantRoles {
		if tr.Messages[i].Role != model.Role(want) {
			t.Errorf("row %d: role %q, want %q", i, tr.Messages[i].Role, want)
		}
	}
	if len(tr.Messages[2].Content) != 1 || tr.Messages[2].Content[0].Kind != model.BlockToolUse {
		t.Fatalf("the assistant row is not one tool_use block: %v", tr.Messages[2].Content)
	}
	toolUse := tr.Messages[2].Content[0]
	if toolUse.ID != "u1" || toolUse.Name != "bash" {
		t.Errorf("the assistant's tool_use lost its identity: id %q name %q", toolUse.ID, toolUse.Name)
	}
	if len(tr.Messages[3].Content) != 1 || tr.Messages[3].Content[0].Kind != model.BlockToolResult {
		t.Fatalf("the third row is not one tool_result block: %v", tr.Messages[3].Content)
	}
	toolResult := tr.Messages[3].Content[0]
	if toolResult.ToolUseID != "u1" || toolResult.IsError {
		t.Errorf("the tool_result does not answer its call: id %q isError %v", toolResult.ToolUseID, toolResult.IsError)
	}

	// --- the events -----------------------------------------------------
	events := streamEvents(w, "gold-watcher", sub.SubscriptionID)
	var start, end *protocol.Event
	var startIdx, endIdx = -1, -1
	for i := range events {
		ev := events[i]
		switch ev.Kind {
		case "step.tool.start":
			start = &events[i]
			startIdx = i
		case "step.tool.end":
			end = &events[i]
			endIdx = i
		}
	}
	if start == nil || end == nil {
		kinds := make([]string, 0, len(events))
		for _, ev := range events {
			kinds = append(kinds, ev.Kind)
		}
		t.Fatalf("the stream carried no tool events (start at %d, end at %d — -1 is absent); the stream had: %v", startIdx, endIdx, kinds)
	}
	if startIdx > endIdx {
		t.Fatalf("the tool end (%d) arrived before its start (%d)", endIdx, startIdx)
	}

	var startPayload, endPayload map[string]any
	if err := json.Unmarshal(start.Payload, &startPayload); err != nil {
		t.Fatalf("the start payload is not a JSON object: %v", err)
	}
	if err := json.Unmarshal(end.Payload, &endPayload); err != nil {
		t.Fatalf("the end payload is not a JSON object: %v", err)
	}
	got.StartKeys = sortedKeys(startPayload)
	got.EndKeys = sortedKeys(endPayload)

	// The stable values: this step, this loop, this call, this command.
	if startPayload["stepId"] != float64(stepID) {
		t.Errorf("the start payload's stepId is %v, want %d", startPayload["stepId"], stepID)
	}
	if startPayload["tool"] != "bash" {
		t.Errorf("the start payload's tool is %v", startPayload["tool"])
	}
	if startPayload["command"] != "echo hi" {
		t.Errorf("the start payload's command is %v", startPayload["command"])
	}
	if startPayload["toolUseId"] != "u1" {
		t.Errorf("the start payload's toolUseId is %v", startPayload["toolUseId"])
	}
	if endPayload["toolUseId"] != "u1" {
		t.Errorf("the end payload's toolUseId is %v", endPayload["toolUseId"])
	}
	// outputTruncated is omitempty: absent is the untruncated answer, and a
	// present value that is not false is a contradiction.
	if v, ok := endPayload["outputTruncated"]; ok && v != false {
		t.Errorf("the end payload claims the output was truncated: %v", v)
	}
	// The model-facing bytes of `echo hi`, clock normalized: the render
	// layout, the stream section, the output.
	gotOutput := execWallRe.ReplaceAllString(fmt.Sprint(endPayload["output"]), "N")
	wantOutput := "$ echo hi\nexit 0 · N\n\n--- stdout ---\nhi\n"
	if gotOutput != wantOutput {
		t.Errorf("the end payload's output is not the model-facing bytes:\n  now:  %q\n  gold: %q", gotOutput, wantOutput)
	}

	// --- the golden file ------------------------------------------------
	if write {
		data, err := json.MarshalIndent(got, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldPath, append(data, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	data, rerr := os.ReadFile(goldPath)
	if rerr != nil {
		t.Fatalf("golden missing (generate with AEON_WRITE_GOLDEN=1 on the untouched tree): %v", rerr)
	}
	var want execGoldRow
	if err := json.Unmarshal(data, &want); err != nil {
		t.Fatal(err)
	}
	if jsonEq(want.Transcript) != jsonEq(got.Transcript) {
		t.Errorf("the transcript shape drifted:\n  now:  %v\n  gold: %v", got.Transcript, want.Transcript)
	}
	if jsonEq(want.StartKeys) != jsonEq(got.StartKeys) {
		t.Errorf("the start payload's key set drifted:\n  now:  %v\n  gold: %v", got.StartKeys, want.StartKeys)
	}
	if jsonEq(want.EndKeys) != jsonEq(got.EndKeys) {
		t.Errorf("the end payload's key set drifted:\n  now:  %v\n  gold: %v", got.EndKeys, want.EndKeys)
	}
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func jsonEq(a any) string {
	d, _ := json.Marshal(a)
	return string(d)
}
