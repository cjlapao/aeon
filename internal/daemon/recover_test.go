package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/runtime"
	"github.com/cjlapao/aeon/internal/store"
)

func recoveryStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func seedRecoveryStep(t *testing.T, s *store.Store, root, tool string, pending bool) int64 {
	t.Helper()
	slug := "recovery"
	if _, err := s.CreateLoop(store.NewLoop{Slug: slug, ComponentRoot: root, Goal: "test"}); err != nil && !strings.Contains(err.Error(), "UNIQUE") {
		t.Fatal(err)
	}
	step, err := s.CreateStep(store.Scope{LoopSlug: slug, ComponentRoot: root}, store.NewStep{Agent: "test", Status: store.StepRunning})
	if err != nil {
		t.Fatal(err)
	}
	grants := []string{"read"}
	if tool != "" {
		grants = []string{tool}
	}
	caller, specs, err := (StepRunner{}).BuildTools(grants, root, "test", runtime.FrozenManifest{})
	_ = caller
	if err != nil {
		t.Fatal(err)
	}
	compiled := (StepRunner{}).Compile(context.Background(), runtime.CompileInput{Handler: runtime.DefaultHandler("test", grants), Granted: grants,
		Route: model.Route{ID: "r", Provider: "anthropic", Model: "m", MaxOutputTokens: 1024}, WorkspaceRoot: root, ToolSpecs: specs}, runtime.ContextDeps{})
	if !compiled.OK {
		t.Fatal(compiled.Reason)
	}
	msgs := compiled.Request.Messages
	if pending {
		msgs = append(msgs, model.Message{Role: model.RoleAssistant, Content: []model.Block{{Kind: model.BlockToolUse, ID: "u1", Name: tool, Input: json.RawMessage(`{"command":"touch marker"}`)}}})
	}
	raw, err := json.Marshal(protocol.StepSnapshot{ContextManifest: compiled.Request.Manifest, Request: compiled.Request, LoopSlug: slug})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveRunningCheckpoint(context.Background(), step.ID, msgs, raw, model.Cost{}); err != nil {
		t.Fatal(err)
	}
	return step.ID
}

func recoveryDaemon(s *store.Store) (*Daemon, *wire) {
	w := &wire{}
	d := New(Options{Store: s, Logger: w, Notify: w.notify, Models: &scriptedModel{turns: []modelTurn{{text: `{"status":"ok","summary":"recovered"}`}}}})
	return d, w
}

func TestScanSkipsSuspended(t *testing.T) {
	s := recoveryStoreBound(t)
	root := t.TempDir()
	id := seedRecoveryStep(t, s, root, "question", true)
	if err := s.SetStepStatus(id, store.StepSuspended, ""); err != nil {
		t.Fatal(err)
	}
	d, _ := recoveryDaemon(s)
	got, err := d.Recover(context.Background(), false)
	if err != nil || got.Total != 0 {
		t.Fatalf("summary=%+v err=%v", got, err)
	}
	step, err := s.ReadStep(id)
	if err != nil || step.Status != "suspended" {
		t.Fatalf("step=%+v err=%v", step, err)
	}
}

func TestNoRecoverLeavesStatusRunning(t *testing.T) {
	s := recoveryStoreBound(t)
	id := seedRecoveryStep(t, s, t.TempDir(), "", false)
	d, w := recoveryDaemon(s)
	got, err := d.Recover(context.Background(), true)
	if err != nil || got.Total != 1 || got.Skipped != 1 {
		t.Fatalf("summary=%+v err=%v", got, err)
	}
	step, _ := s.ReadStep(id)
	if step.Status != "running" {
		t.Fatalf("status=%s", step.Status)
	}
	if _, ok, _ := s.ReadRecovery(id); ok {
		t.Fatal("dry run wrote a recovery record")
	}
	if !strings.Contains(w.logLines(), "recovering 1 of 1") {
		t.Fatal(w.logLines())
	}
}

func TestScanRecoversCapturedResult(t *testing.T) {
	s := recoveryStoreBound(t)
	root := t.TempDir()
	id := seedRecoveryStep(t, s, root, "bash", true)
	use := model.Block{Kind: model.BlockToolUse, ID: "u1", Name: "bash", Input: json.RawMessage(`{"command":"touch marker"}`)}
	if err := s.BeginReceipt(context.Background(), id, "u1", "bash", store.ArgDigest("bash", use.Input)); err != nil {
		t.Fatal(err)
	}
	if err := s.CompleteReceipt(context.Background(), id, "u1", "already completed", false); err != nil {
		t.Fatal(err)
	}
	d, _ := recoveryDaemon(s)
	got, err := d.Recover(context.Background(), false)
	if err != nil || got.Recovered != 1 {
		t.Fatalf("summary=%+v err=%v", got, err)
	}
	if _, err := os.Stat(filepath.Join(root, "marker")); !os.IsNotExist(err) {
		t.Fatalf("bash ran again: %v", err)
	}
	step, _ := s.ReadStep(id)
	if step.Status != "done" || step.Outcome != "ok" {
		t.Fatalf("step=%+v", step)
	}
	transcript := s.ReadTranscript(id)
	if !transcript.OK || len(transcript.Messages) < 4 || transcript.Messages[3].Content[0].Content != "already completed" {
		t.Fatalf("transcript=%+v", transcript)
	}
	again, err := d.Recover(context.Background(), false)
	if err != nil || again.Total != 0 {
		t.Fatalf("second scan=%+v err=%v", again, err)
	}
}

func TestScanFailsUnknownOutcomeAndContinues(t *testing.T) {
	s := recoveryStoreBound(t)
	root := t.TempDir()
	id := seedRecoveryStep(t, s, root, "bash", true)
	use := model.Block{Kind: model.BlockToolUse, ID: "u1", Name: "bash", Input: json.RawMessage(`{"command":"touch marker"}`)}
	if err := s.BeginReceipt(context.Background(), id, "u1", "bash", store.ArgDigest("bash", use.Input)); err != nil {
		t.Fatal(err)
	}
	second := seedRecoveryStep(t, s, root, "", false)
	d, _ := recoveryDaemon(s)
	got, err := d.Recover(context.Background(), false)
	if err != nil || got.Total != 2 {
		t.Fatalf("summary=%+v err=%v", got, err)
	}
	step, _ := s.ReadStep(id)
	record, ok, _ := s.ReadRecovery(id)
	if step.Outcome != "failed" || !ok || !strings.Contains(record.Reason, "started but not completed") {
		t.Fatalf("step=%+v record=%+v", step, record)
	}
	other, _ := s.ReadStep(second)
	if other.Status != "done" {
		t.Fatalf("second=%+v", other)
	}
	if _, err := os.Stat(filepath.Join(root, "marker")); !os.IsNotExist(err) {
		t.Fatalf("bash ran again: %v", err)
	}
}

func TestScanIsIdempotent(t *testing.T) {
	s := recoveryStoreBound(t)
	seedRecoveryStep(t, s, t.TempDir(), "", false)
	d, _ := recoveryDaemon(s)
	first, err := d.Recover(context.Background(), false)
	if err != nil || first.Total != 1 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := d.Recover(context.Background(), false)
	if err != nil || second.Total != 0 {
		t.Fatalf("second=%+v err=%v", second, err)
	}
}

func TestCaseBSeparatesReissuedCostAndKeepsInterruptedChargeUnknown(t *testing.T) {
	s := recoveryStoreBound(t)
	id := seedRecoveryStep(t, s, t.TempDir(), "", false)
	d, _ := recoveryDaemon(s)
	if _, err := d.Recover(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	record, ok, err := s.ReadRecovery(id)
	if err != nil || !ok || record.Case != "B" || record.PriorCost == nil || record.ReissueCost == nil {
		t.Fatalf("case B costs were not stored: record=%+v err=%v", record, err)
	}
	if record.PriorCost.State != model.CostUnknown || record.PriorCost.Reason != model.ReasonStreamAbortedEarly {
		t.Fatalf("interrupted charge became certain: %+v", record.PriorCost)
	}
	step, err := s.ReadStep(id)
	if err != nil || step.Cost.State != model.CostUnknown {
		t.Fatalf("step total hid the interrupted charge: %+v err=%v", step.Cost, err)
	}
	view, err := NewMemory(s).ShowStep(id)
	if err != nil || view.Recovery == nil || view.Recovery.PriorCost == nil || view.Recovery.ReissueCost == nil {
		t.Fatalf("costs did not reach step.show: %+v err=%v", view.Recovery, err)
	}
}
