package runtime

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

func suspendedFixture(t *testing.T) (ResumeRequest, *spyTools) {
	t.Helper()
	spy := newSpyTools("read", "question")
	st := testState(testHandler("read", "question"), []model.ToolSpec{toolSpec("read"), toolSpec("question")})
	res := RunLoop(context.Background(), st, LoopDeps{Client: newFakeClient(scriptedTurn{ToolUses: []model.Block{
		toolUse("r1", "read", `{"path":"a.md"}`), toolUse("q1", "question", `{"text":"Proceed?"}`),
	}}), Tools: spy})
	if res.Kind != ResultSuspended {
		t.Fatalf("fixture did not suspend: %+v", res)
	}
	return ResumeRequest{Request: st.Request, Messages: res.Messages, Manifest: res.Manifest,
		Question: *res.Question, Answer: "yes", Usage: res.Usage, Cost: res.Cost}, spy
}

func TestResumeUsesStoredTranscriptAndNeverReassemblesOrRerunsTools(t *testing.T) {
	req, _ := suspendedFixture(t)
	resumedTools := newSpyTools("read", "question")
	req.Manifest.Files = []FileHash{{Path: "a.md", Hash: HashContent([]byte("original"))}}
	req.Request.Handler.Sections = []SectionSpec{{Title: "Original", DeclaredFile: "a.md", Category: CategoryInjectedContext, Destination: SectionTranscript}}
	req.Messages[1].Content = append(req.Messages[1].Content, model.TextBlock("original"))
	reads := 0
	res := ResumeStep(context.Background(), req, RunOpts{
		Client: newFakeClient(scriptedTurn{Text: okHandoff}), Tools: resumedTools,
		Context: ContextDeps{ReadFile: func(path string) ([]byte, error) {
			reads++
			return []byte("changed"), nil
		}},
		HandoffSpec: HandoffSpec{Required: []string{"status", "summary"}},
	})
	if res.Outcome != OutcomeOK {
		t.Fatalf("outcome %s: %s", res.Outcome, res.Reason)
	}
	if reads != 1 {
		t.Errorf("read %d times; resume should only rehash, never assemble", reads)
	}
	if len(resumedTools.recorded()) != 0 {
		t.Fatal("a completed tool was re-run")
	}
	var foundNote, foundOriginal bool
	for _, message := range res.Messages {
		for _, block := range message.Content {
			if block.Kind == model.BlockText && strings.Contains(block.Text, "Changed while") {
				foundNote = true
				if message.Role != model.RoleUser || !strings.Contains(block.Text, "a.md") {
					t.Errorf("changed note: %+v", message)
				}
			}
			if block.Kind == model.BlockText && block.Text == "original" {
				foundOriginal = true
			}
		}
	}
	if !foundNote || !foundOriginal {
		t.Errorf("note=%v original=%v", foundNote, foundOriginal)
	}
	if res.Manifest.Files[0].Hash != req.Manifest.Files[0].Hash {
		t.Fatal("resume mutated the frozen manifest")
	}
}

func TestResumeReportsMissingSeparatelyAndCapsPaths(t *testing.T) {
	req, _ := suspendedFixture(t)
	for i := 0; i < 14; i++ {
		req.Manifest.Files = append(req.Manifest.Files, FileHash{Path: fmt.Sprintf("missing-%02d", i), Hash: "sha256:old"})
	}
	res := ResumeStep(context.Background(), req, RunOpts{Client: newFakeClient(scriptedTurn{Text: okHandoff}), Tools: newSpyTools(),
		Context:     ContextDeps{ReadFile: func(string) ([]byte, error) { return nil, os.ErrNotExist }},
		HandoffSpec: HandoffSpec{Required: []string{"status", "summary"}},
	})
	if res.Outcome != OutcomeOK {
		t.Fatalf("outcome %s: %s", res.Outcome, res.Reason)
	}
	note := res.Messages[len(req.Messages)].Content[0].Text
	if !strings.Contains(note, "Missing while") || strings.Contains(note, "Changed while") || !strings.Contains(note, "4 more omitted") || strings.Contains(note, "missing-13") {
		t.Errorf("note: %s", note)
	}
}

func TestResumeKeepsBoundsAndAcceptsEmptyAnswer(t *testing.T) {
	req, _ := suspendedFixture(t)
	req.Answer = ""
	req.Manifest.MaxIterations = 2
	res := ResumeStep(context.Background(), req, RunOpts{Client: newFakeClient(scriptedTurn{Text: okHandoff}), Tools: newSpyTools(),
		HandoffSpec: HandoffSpec{Required: []string{"status", "summary"}}})
	if res.Outcome != OutcomeOK || res.Manifest.IterationsUsed != 2 {
		t.Fatalf("outcome=%s iterations=%d reason=%s", res.Outcome, res.Manifest.IterationsUsed, res.Reason)
	}
	answer := res.Messages[len(req.Messages)].Content[0]
	if answer.Kind != model.BlockToolResult || answer.Content != "" {
		t.Errorf("empty answer changed: %+v", answer)
	}
	if valid := model.ValidateTranscript(res.Messages); !valid.OK {
		t.Errorf("resumed transcript: %s", valid.Reason)
	}
}

func TestResumeCanSuspendAgainWithoutResettingIterationCount(t *testing.T) {
	req, _ := suspendedFixture(t)
	req.Manifest.MaxIterations = 2
	res := ResumeStep(context.Background(), req, RunOpts{Client: newFakeClient(scriptedTurn{ToolUses: []model.Block{
		toolUse("q2", "question", `{"text":"Again?"}`),
	}}), Tools: newSpyTools()})
	if res.Kind != ResultSuspended || res.Manifest.IterationsUsed != 2 {
		t.Fatalf("kind=%s iterations=%d", res.Kind, res.Manifest.IterationsUsed)
	}
	res2 := ResumeStep(context.Background(), ResumeRequest{Request: req.Request, Messages: res.Messages, Manifest: res.Manifest, Question: *res.Question, Answer: "yes"}, RunOpts{Client: newFakeClient(scriptedTurn{Text: okHandoff}), Tools: newSpyTools()})
	if res2.Outcome != OutcomeBlocked || !strings.Contains(res2.Reason, "MaxIterations (2)") {
		t.Fatalf("outcome=%s reason=%s", res2.Outcome, res2.Reason)
	}
}
