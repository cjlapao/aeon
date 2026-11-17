package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
)

func TestStepShowNamesTheFailedTool(t *testing.T) {
	var out bytes.Buffer
	renderStepFacts(&out, protocol.StepView{ID: 7, Status: "done", Outcome: "failed", Recovery: &protocol.RecoveryView{
		Case: "C", Reason: "unknown-tool-outcome: bash receipt started but not completed",
		Failure: &protocol.RecoveryFailureView{Kind: "unknown-tool-outcome", Tool: "bash", ToolUseID: "u1", Detail: "bash receipt started but not completed"}}})
	for _, part := range []string{"outcome failed", "case C", "bash", "started but not completed"} {
		if !strings.Contains(out.String(), part) {
			t.Fatalf("missing %q in %s", part, out.String())
		}
	}
}

func TestNoRecoverHelpStatesWhatItSkips(t *testing.T) {
	var out bytes.Buffer
	printUsage(&out)
	if !strings.Contains(out.String(), "--no-recover reports cases and leaves crashed steps running") {
		t.Fatal(out.String())
	}
}
