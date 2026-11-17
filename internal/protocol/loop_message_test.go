package protocol

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
)

// loop_message_test.go pins `loop.message`'s door validation (S39 T6, CR5):
// the shape rules fire before any daemon sees the call, and refusals beyond
// shape (a step running, a loop not resting) are the daemon's DATA — this
// layer never invents them.

func rawOf(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestLoopMessageRequiresIDAndText(t *testing.T) {
	cases := []struct {
		what   string
		params LoopMessageParams
	}{
		{"no id", LoopMessageParams{Text: "hi"}},
		{"no text", LoopMessageParams{ID: "l1"}},
		{"whitespace-only text", LoopMessageParams{ID: "l1", Text: "   \n "}},
		{"both empty", LoopMessageParams{}},
	}
	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			called := false
			tbl := &Table{MessageLoop: func(context.Context, LoopMessageParams) (any, error) {
				called = true
				return nil, nil
			}}
			_, err := tbl.handleLoopMessage(context.Background(), nil, rawOf(t, c.params))
			var bad ErrBadParams
			if !errors.As(err, &bad) {
				t.Fatalf("want ErrBadParams, got %v", err)
			}
			if called {
				t.Fatal("the daemon callback ran despite invalid params — the door validates FIRST")
			}
		})
	}
}

func TestLoopMessageTextBound(t *testing.T) {
	long := strings.Repeat("x", maxSteerTextBytes+1)
	tbl := &Table{MessageLoop: func(context.Context, LoopMessageParams) (any, error) { return nil, nil }}
	_, err := tbl.handleLoopMessage(context.Background(), nil, rawOf(t, LoopMessageParams{ID: "l1", Text: long}))
	var bad ErrBadParams
	if !errors.As(err, &bad) {
		t.Fatalf("a %d-byte text answered %v, want ErrBadParams", len(long), err)
	}
	// The bound is steer's bound — the same byte ceiling, named in the
	// message so a client can trim with arithmetic instead of guessing.
	if !strings.Contains(err.Error(), strconv.Itoa(maxSteerTextBytes)) {
		t.Fatalf("the refusal must name the bound: %v", err)
	}
	// One byte under, the door opens.
	justUnder := strings.Repeat("x", maxSteerTextBytes)
	ok := false
	tbl2 := &Table{MessageLoop: func(_ context.Context, p LoopMessageParams) (any, error) {
		ok = len(p.Text) == maxSteerTextBytes
		return nil, nil
	}}
	if _, err := tbl2.handleLoopMessage(context.Background(), nil, rawOf(t, LoopMessageParams{ID: "l1", Text: justUnder})); err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("text at exactly the bound must pass (the bound is inclusive)")
	}
}

func TestLoopMessageWithoutADaemonIsStoreUnavailable(t *testing.T) {
	tbl := &Table{}
	_, err := tbl.handleLoopMessage(context.Background(), nil, rawOf(t, LoopMessageParams{ID: "l1", Text: "hi"}))
	if !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("got %v, want ErrStoreUnavailable", err)
	}
}

func TestLoopMessageTrimsTheTextBeforeTheDaemon(t *testing.T) {
	var seen string
	tbl := &Table{MessageLoop: func(_ context.Context, p LoopMessageParams) (any, error) {
		seen = p.Text
		return map[string]any{"turn": "started"}, nil
	}}
	if _, err := tbl.handleLoopMessage(context.Background(), nil, rawOf(t, LoopMessageParams{ID: "l1", Text: "  hello  \n"})); err != nil {
		t.Fatal(err)
	}
	if seen != "hello" {
		t.Fatalf("the daemon sees %q, want the trimmed text — one trim, at the door", seen)
	}
}
