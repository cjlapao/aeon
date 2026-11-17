package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// This uses a real store and a fresh daemon instance on resume. The only fake
// is the provider stream; the question, transcript and status cross disk.
func TestQuestionSurvivesDaemonRestartAndAnswerResumes(t *testing.T) {
	dir := t.TempDir()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	route := model.Route{ID: "r1", Provider: "anthropic", Model: "test", MaxOutputTokens: 1024}
	open := func() *store.Store {
		s, err := store.Open(store.Options{Dir: dir, Env: func(string) string { return "" }})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	s := open()
	d := New(Options{Store: s, Routes: protocol.NewStaticRoutes(route), Models: &scriptedModel{turns: []modelTurn{
		{toolUses: []model.Block{toolUse("q1", "question", `{"text":"Which approach?","options":["one","two"]}`)}},
	}}})
	initConn(t, d, context.Background(), "c1", "test")
	raw, _ := json.Marshal(protocol.StepParams{Prompt: "choose", Tools: []string{"question"}, WorkspaceRoot: root, ContextFiles: []string{"README.md"}})
	response, err := d.Table.Dispatch(context.Background(), "c1", protocol.MethodStep, raw)
	if err != nil {
		t.Fatal(err)
	}
	first := response.(protocol.StepResult)
	if first.Kind != "suspended" || first.Question == nil {
		t.Fatalf("suspension: %+v", first)
	}
	st, err := s.ReadStep(first.StepID)
	if err != nil || st.Status != "suspended" {
		t.Fatalf("stored step: %+v, %v", st, err)
	}
	if tr := s.ReadTranscript(first.StepID); !tr.OK || tr.Count == 0 {
		t.Fatalf("stored transcript: %+v", tr)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}

	s = open()
	defer s.Close()
	d = New(Options{Store: s, Routes: protocol.NewStaticRoutes(route), Models: &scriptedModel{turns: []modelTurn{
		{text: `{"status":"ok","summary":"chosen"}`},
	}}})
	initConn(t, d, context.Background(), "c2", "test")
	answerRaw, _ := json.Marshal(protocol.AnswerParams{StepID: first.StepID, QuestionID: first.Question.ID, Answer: "one"})
	if _, err := d.Table.Dispatch(context.Background(), "c2", protocol.MethodAnswer, answerRaw); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		st, err = s.ReadStep(first.StepID)
		if err != nil {
			t.Fatal(err)
		}
		if st.Status == "done" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if st.Status != "done" {
		t.Fatalf("resumed step remained %q", st.Status)
	}
	tr := s.ReadTranscript(first.StepID)
	if !tr.OK || tr.Count < 4 {
		t.Fatalf("resumed transcript: %+v", tr)
	}
	changedNote := false
	for _, msg := range tr.Messages {
		for _, b := range msg.Content {
			if msg.Role == model.RoleUser && b.Kind == model.BlockText && strings.Contains(b.Text, "Changed while this step waited: README.md") {
				changedNote = true
			}
		}
	}
	if !changedNote {
		t.Fatal("the changed-file note was not stored as a user message")
	}
	var snapshot protocol.StepSnapshot
	if err := json.Unmarshal(st.ContextManifestRaw, &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.ContextManifest.ObservedChanges) != 1 ||
		snapshot.ContextManifest.ObservedChanges[0].Path != "README.md" ||
		snapshot.ContextManifest.ObservedChanges[0].Kind != "changed" {
		t.Fatalf("resume observation missing from structured snapshot: %+v", snapshot.ContextManifest.ObservedChanges)
	}
	q, err := s.ReadQuestion(first.Question.ID)
	if err != nil || q.Answer != "one" || q.AnsweredAt == "" {
		t.Fatalf("answered question: %+v, %v", q, err)
	}
}
