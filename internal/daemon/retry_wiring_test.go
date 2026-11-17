package daemon

// TestRetryOpIsWired — T10's daemon claim, made where only the daemon can
// make it: the store-backed table carries the retry seam (a nil seam is a
// registered method that answers store-unavailable forever), refusals leave
// as DATA the app renders, a storeless daemon carries no seam, and an
// unknown loop id is an error the socket speaks, not a panic.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

func TestRetryOpIsWired(t *testing.T) {
	s := recoveryStore(t)
	d := New(Options{Store: s})
	if d.Table.RetryHandlerBlock == nil {
		t.Fatal("loop.retry_handler_block is registered but the store-backed daemon wires no seam — the method exists only to fail closed")
	}
	if d0 := New(Options{}); d0.Table.RetryHandlerBlock != nil {
		t.Error("the storeless daemon grew a retry seam — with no store there is no loop to retry, and the read must fail closed")
	}
	if _, err := d.Table.RetryHandlerBlock(context.Background(), protocol.LoopRetryHandlerBlockParams{ID: "no-such-loop"}); err == nil {
		t.Error("retrying an unknown loop returned success — the store's own refusal must travel")
	}

	// The S9-era refusal, end to end at the seam — now carried by a name
	// that will never ship (loop-doctor: not registered, not in
	// futureAgents, not one of the follow-up five), so the guard keeps
	// being tested after T12. The refusal must arrive as renderable
	// data, not a JSON-RPC error the UI would toast away.
	dir := t.TempDir()
	path := filepath.Join(dir, "hist.yaml")
	raw := "id: hist\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: triage\n    step: {kind: model-step, agent: loop-doctor}\n"
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	// Pin-time validation refuses task-coordinator TODAY; the historical
	// population pinned before that rule existed. Create the loop row and
	// write its pin directly — the retry judges the pin, not preflight.
	bp, err := blueprint.Load([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	st := loop.LoopState{Slug: "hist", Status: "blocked", Goal: "survey", ComponentRoot: "/repo", Blueprint: &bp, RuntimeState: loop.InitialState(bp)}
	st.LastReason = `invalid params: unknown handler "loop-doctor"`
	js, err := st.RuntimeJSON()
	if err != nil {
		t.Fatal(err)
	}
	l, err := s.CreatePinnedLoop(store.NewLoop{Slug: "hist", ComponentRoot: "/repo", Goal: "survey"}, bp.ID, bp.Version, raw, js)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SavePinnedRuntime(l.ID, "blocked", js); err != nil {
		t.Fatal(err)
	}
	got, err := d.Table.RetryHandlerBlock(context.Background(), protocol.LoopRetryHandlerBlockParams{ID: l.ID})
	if err != nil {
		t.Fatalf("the refusal came back as an error: %v — eligibility and refusal are DATA the app shows verbatim", err)
	}
	m, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("refusal payload is %T", got)
	}
	refused, ok := m["refused"].(map[string]string)
	if !ok || refused["kind"] != "handler-still-missing" {
		t.Fatalf("refusal = %#v, want kind handler-still-missing — loop-doctor has no body and never will (T12 reseed, 2026-10-26)", m)
	}
	if refused["detail"] == "" {
		t.Error("the refusal names no remedy — refusals name the thing and the fix")
	}
	// and the loop stayed blocked
	saved, err := loop.Pinned(context.Background(), s, l.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Status != "blocked" {
		t.Errorf("a refused retry moved the loop to %q", saved.Status)
	}

	// The S9 flip itself (T12, 2026-10-26): the same seam, the same
	// historical shape — but task-coordinator now HAS a body, so the
	// block is eligible. The retry is no longer a refusal; it comes back
	// as a retry that happened (retried), and no refusal rides along.
	// QA claim (QAC-36 in the T17 suite): a blocked-missing-handler loop
	// retries once the body ships — no re-pin, no loop deletion.
	rawTC := "id: flip\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: triage\n    step: {kind: model-step, agent: task-coordinator}\n"
	bpTC, err := blueprint.Load([]byte(rawTC))
	if err != nil {
		t.Fatal(err)
	}
	stTC := loop.LoopState{Slug: "flip", Status: "blocked", Goal: "survey", ComponentRoot: "/repo", Blueprint: &bpTC, RuntimeState: loop.InitialState(bpTC)}
	stTC.LastReason = `invalid params: unknown handler "task-coordinator"`
	jsTC, err := stTC.RuntimeJSON()
	if err != nil {
		t.Fatal(err)
	}
	lTC, err := s.CreatePinnedLoop(store.NewLoop{Slug: "flip", ComponentRoot: "/repo", Goal: "survey"}, bpTC.ID, bpTC.Version, rawTC, jsTC)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SavePinnedRuntime(lTC.ID, "blocked", jsTC); err != nil {
		t.Fatal(err)
	}
	gotTC, err := d.Table.RetryHandlerBlock(context.Background(), protocol.LoopRetryHandlerBlockParams{ID: lTC.ID})
	if err != nil {
		t.Fatalf("an eligible retry came back as an error: %v", err)
	}
	mTC, ok := gotTC.(map[string]any)
	if !ok {
		t.Fatalf("retry payload is %T", gotTC)
	}
	if mTC["retried"] != true {
		t.Fatalf("retried = %#v, want true — the shipped body must make the historical block eligible (S9 flip)", mTC)
	}
	if _, refused := mTC["refused"]; refused {
		t.Fatalf("an eligible retry still carries a refusal: %#v", mTC)
	}
}
