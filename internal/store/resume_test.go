package store

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

// T10's cold-resume proof.
//
// # "Resumes cold" means a DIFFERENT PROCESS
//
// Every test before this one has written and read in the same process, holding
// the same pools and the same objects in memory. That proves serialisation, not
// resumption — and the failure being ruled out is precisely something surviving
// in memory that does not survive on disk.
//
// So a real child process opens the file fresh, reads back the transcript and
// the question, and prints a digest. The parent compares.
//
// # Why the child is this test binary, re-executed
//
// It is a real process with its own address space, its own pools and its own
// `sqlite3_auto_extension` registration — which is the whole point. The
// alternative, `go run` on a generated program, adds a compile step (with cgo)
// to every test run and proves nothing more.

// childEnv names the environment variable that turns this binary into the
// child. TestMain dispatches on it before the test framework starts.
const (
	childEnv  = "AEON_STORE_COLD_READ_CHILD"
	childStep = "AEON_STORE_COLD_READ_STEP"
)

// coldReadChild is the child's whole program.
//
// It opens the store at the path it is given, reads the transcript, the
// signature and the pending question, and prints them as JSON. It shares NO
// state with the parent beyond the file.
func coldReadChild() int {
	dir := os.Getenv(childEnv)
	stepID := os.Getenv(childStep)

	s, err := Open(Options{Dir: dir, Env: func(string) string { return "" }})
	if err != nil {
		_, _ = os.Stderr.WriteString("child: open: " + err.Error() + "\n")
		return 1
	}
	defer s.Close()

	var id int64
	if _, err := fmtSscan(stepID, &id); err != nil {
		_, _ = os.Stderr.WriteString("child: step id: " + err.Error() + "\n")
		return 1
	}

	digest, err := s.TranscriptDigest(id)
	if err != nil {
		_, _ = os.Stderr.WriteString("child: digest: " + err.Error() + "\n")
		return 1
	}
	signature, length, err := s.SignatureOf(id)
	if err != nil {
		_, _ = os.Stderr.WriteString("child: signature: " + err.Error() + "\n")
		return 1
	}
	res := s.ReadTranscript(id)
	if !res.OK {
		_, _ = os.Stderr.WriteString("child: transcript: " + res.Reason() + "\n")
		return 1
	}

	st, err := s.ReadStep(id)
	if err != nil {
		_, _ = os.Stderr.WriteString("child: step: " + err.Error() + "\n")
		return 1
	}
	sc := Scope{LoopSlug: st.LoopSlug, ComponentRoot: st.ComponentRoot}
	pending, hasPending, err := s.PendingQuestion(sc)
	if err != nil {
		_, _ = os.Stderr.WriteString("child: question: " + err.Error() + "\n")
		return 1
	}
	awaiting, err := s.IsAwaiting(sc)
	if err != nil {
		_, _ = os.Stderr.WriteString("child: awaiting: " + err.Error() + "\n")
		return 1
	}
	plan, err := s.ReadDocument(sc, "plan")
	if err != nil {
		_, _ = os.Stderr.WriteString("child: document: " + err.Error() + "\n")
		return 1
	}

	out := coldRead{
		Digest:          digest,
		Signature:       signature,
		SignatureLength: length,
		Messages:        len(res.Messages),
		ThinkingText:    firstThinkingText(res.Messages),
		CostState:       string(st.Cost.State),
		CostFloor:       st.Cost.USDAtLeast,
		StepStatus:      st.Status,
		HasPending:      hasPending,
		PendingToolUse:  pending.ToolUseID,
		PendingAnswered: pending.Answered(),
		Awaiting:        awaiting,
		PlanContent:     plan.Content,
		VecPresent:      vecAnswered(s),
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		_, _ = os.Stderr.WriteString("child: encode: " + err.Error() + "\n")
		return 1
	}
	_, _ = os.Stdout.Write(encoded)
	return 0
}

// coldRead is what the child reports.
type coldRead struct {
	Digest          string  `json:"digest"`
	Signature       string  `json:"signature"`
	SignatureLength int     `json:"signatureLength"`
	Messages        int     `json:"messages"`
	ThinkingText    string  `json:"thinkingText"`
	CostState       string  `json:"costState"`
	CostFloor       float64 `json:"costFloor"`
	StepStatus      string  `json:"stepStatus"`
	HasPending      bool    `json:"hasPending"`
	PendingToolUse  string  `json:"pendingToolUse"`
	PendingAnswered bool    `json:"pendingAnswered"`
	Awaiting        bool    `json:"awaiting"`
	PlanContent     string  `json:"planContent"`
	VecPresent      bool    `json:"vecPresent"`
}

func firstThinkingText(msgs []model.Message) string {
	for _, m := range msgs {
		for _, b := range m.Content {
			if b.Kind == model.BlockThinking {
				return b.Text
			}
		}
	}
	return ""
}

func vecAnswered(s *Store) bool {
	w, r, err := s.VecVersion()
	return err == nil && w != "" && r != ""
}

func TestAChildProcessReadsATranscriptBackByteIdentically(t *testing.T) {
	// D17. The honest test of "resumes in a process that never saw the one
	// that wrote it".
	//
	// It implements ARCHITECTURE §3's suspend sequence literally: a loop, a
	// task, a step, a transcript with a signed thinking block and a tool_use
	// with no tool_result, a permission question carrying that tool_use_id, and
	// the step marked suspended. Then the parent closes everything and a child
	// reads it cold.
	dir := t.TempDir()
	s := openAt(t, dir)

	sc := seedLoop(t, s, "resume", "/repo")
	if _, err := s.CreateTask(sc, NewTask{TaskID: "T1", Title: "the suspended task"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.WriteDocument(sc, "plan", "# Plan\n\nthe plan that must survive.\n", ""); err != nil {
		t.Fatal(err)
	}

	st := seedStep(t, s, sc)
	// A transcript ending in an assistant turn with an UNANSWERED tool_use.
	// That is what a suspended step looks like, and S3's validator accepts it
	// deliberately.
	suspended := []model.Message{
		{Role: model.RoleSystem, Content: []model.Block{model.TextBlock("you are terse")}},
		{Role: model.RoleUser, Content: []model.Block{model.TextBlock("delete the build directory")}},
		{Role: model.RoleAssistant, Provenance: testProvenance(), Content: []model.Block{
			model.ThinkingBlock(thinkingText, thinkingSignature),
			model.TextBlock("I need permission for that."),
			model.ToolUseBlock("toolu_danger", "shell", []byte(`{"cmd":"rm -rf build"}`)),
		}},
	}
	if err := s.AppendMessages(st.ID, suspended); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStepCost(st.ID, model.Cost{State: model.CostPartial, USDAtLeast: 0.0012,
		Missing: []string{"cacheWriteUsdPerMTok"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetContextManifest(st.ID, ContextManifest{
		Files:          []ContextFile{{Path: "AGENTS.md", Hash: "sha256:cafe"}},
		IterationsUsed: 1,
		DeadlineAt:     "2026-10-15T23:59:59.000Z",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Ask(sc, NewQuestion{StepID: &st.ID, ToolUseID: "toolu_danger",
		Kind: QuestionPermission, Body: "run `rm -rf build`?",
		Options: []string{"allow once", "deny"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStepStatus(st.ID, StepSuspended, ""); err != nil {
		t.Fatal(err)
	}

	parentDigest, err := s.TranscriptDigest(st.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Closed BEFORE the child runs: anything still in this process's memory is
	// gone, which is the condition the test exists to create.
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	got := runColdReadChild(t, dir, st.ID)

	if got.Digest != parentDigest {
		t.Errorf("the child read a different transcript:\n  child  %s\n  parent %s", got.Digest, parentDigest)
	}
	// The signature as an EXACT STRING across the process boundary. This is
	// the assertion S7's replay depends on.
	if got.Signature != thinkingSignature {
		t.Errorf("the signature did not survive the process boundary:\n  got  %q\n  want %q",
			got.Signature, thinkingSignature)
	}
	if got.SignatureLength != len(thinkingSignature) {
		t.Errorf("the signature is %d bytes in the child, %d in the parent",
			got.SignatureLength, len(thinkingSignature))
	}
	if got.ThinkingText != thinkingText {
		t.Errorf("the thinking text did not survive:\n  got  %q\n  want %q", got.ThinkingText, thinkingText)
	}
	if got.Messages != 3 {
		t.Errorf("the child read %d messages, want 3", got.Messages)
	}

	// The rest of what a resume needs.
	if got.StepStatus != string(StepSuspended) {
		t.Errorf("the child sees the step as %q, want suspended", got.StepStatus)
	}
	if got.CostState != string(model.CostPartial) || got.CostFloor != 0.0012 {
		t.Errorf("the cost did not survive: state %q floor %v", got.CostState, got.CostFloor)
	}
	if !got.HasPending || got.PendingToolUse != "toolu_danger" {
		t.Errorf("the pending question did not survive: %+v", got)
	}
	if got.PendingAnswered {
		t.Error("the child sees the question as answered")
	}
	if !got.Awaiting {
		t.Error("the child does not see the loop as awaiting an answer, so a resume would never happen")
	}
	if !strings.Contains(got.PlanContent, "must survive") {
		t.Errorf("the plan document did not survive: %q", got.PlanContent)
	}
	if !got.VecPresent {
		t.Error("vec_version did not answer in the child; the extension is statically linked, so a " +
			"fresh process must have it too (DB18)")
	}
}

func TestTheColdReadChildFailsWhenTheTranscriptIsMissing(t *testing.T) {
	// The negative control for the harness itself. A child that printed a
	// plausible digest for a step that does not exist would make the test above
	// pass without reading anything.
	dir := t.TempDir()
	s := openAt(t, dir)
	_ = s.Close()

	cmd := exec.Command(os.Args[0], "-test.run", "TestAChildProcessReadsATranscriptBackByteIdentically")
	cmd.Env = append(os.Environ(), childEnv+"="+dir, childStep+"=9999")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("the child succeeded on a step that does not exist: %s", out)
	}
	if !strings.Contains(string(out), "child:") {
		t.Errorf("the child did not report where it failed: %s", out)
	}
}

// runColdReadChild re-executes this binary as the cold reader.
func runColdReadChild(t *testing.T, dir string, stepID int64) coldRead {
	t.Helper()

	cmd := exec.Command(os.Args[0], "-test.run", "TestAChildProcessReadsATranscriptBackByteIdentically")
	cmd.Env = append(os.Environ(),
		childEnv+"="+dir,
		childStep+"="+itoa(stepID),
	)
	out, err := cmd.Output()
	if err != nil {
		var stderr string
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = string(ee.Stderr)
		}
		t.Fatalf("the child process failed: %v\n%s", err, stderr)
	}

	var got coldRead
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("the child printed something that is not its report: %v\n%s", err, out)
	}
	return got
}
