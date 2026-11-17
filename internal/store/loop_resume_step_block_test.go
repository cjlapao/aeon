package store

import (
	"strings"
	"testing"
)

func TestResumingAStepThatFinishedBlockedClearsItsOutcome(t *testing.T) {
	st, err := Open(Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	root := t.TempDir()
	l, err := st.CreatePinnedLoop(NewLoop{Slug: "blk", ComponentRoot: root, Root: root, Goal: "g", Status: "blocked"}, "brainstorm", 1, "raw", `{"lastOutcome":"blocked","position":{"lane":"capture"}}`)
	if err != nil {
		t.Fatal(err)
	}
	got, refusal, err := st.ResumeLoop(l.ID, "user", "tester")
	if err != nil || refusal.Kind != "" || !got.Changed || got.Status != "running" {
		t.Fatalf("resume: %+v %+v %v", got, refusal, err)
	}
	var raw string
	if err := st.r.QueryRow(`SELECT runtime_json FROM loop_blueprint WHERE loop_id=?`, l.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if want := `"lastOutcome"`; strings.Contains(raw, want) {
		t.Fatalf("the attempt's outcome must not outlive it: %s", raw)
	}
}
