package protocol

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// I3 — exactly ONE place decides whether a connection has initialised.
//
// A second check drifts, the looser one wins, and T7 becomes a suggestion. The
// assertion is on the call site count: `Sessions.Require` is the decision, so
// it is defined in session.go and called from exactly one place — the
// dispatcher. A handler that re-checked would show up here.
func TestExactlyOnePlaceDecidesWhetherAConnectionHasInitialized(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	callers := map[string]int{}

	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") || file == "session.go" {
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		// Call sites from the AST, not tokens: a token scan counts `Require`
		// and `Sessions.Require` as two hits for one call, which made this test
		// fail on correct code the first time it ran.
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Require" {
				callers[file]++
			}
			return true
		})
	}

	total := 0
	for file, n := range callers {
		total += n
		if file != "method.go" {
			t.Errorf("%s calls Require; the initialise gate belongs in the dispatcher "+
				"alone (I3)", file)
		}
	}
	if total == 0 {
		t.Fatal("nothing calls Sessions.Require — the gate is not wired, or this test " +
			"is looking for the wrong name")
	}
	if total > 1 {
		t.Errorf("Require is called from %d places; exactly one decides (I3)", total)
	}
}

// T5's mechanism, directly: the transport cancels a connection's context on
// disconnect, and that is the ONLY close signal available. If this stops
// working, subscriptions leak and nothing else notices.
func TestCancellingTheConnectionContextClosesTheSession(t *testing.T) {
	sessions := NewSessions()
	closed := make(chan string, 1)
	sessions.OnClose(func(connID string) { closed <- connID })

	ctx, cancel := context.WithCancel(context.Background())
	if err := sessions.Open(ctx, &Session{ConnID: "c1", Version: PROTOCOL_VERSION}); err != nil {
		t.Fatal(err)
	}
	if sessions.Len() != 1 {
		t.Fatalf("Len = %d, want 1", sessions.Len())
	}

	cancel()
	select {
	case got := <-closed:
		if got != "c1" {
			t.Fatalf("closed %q, want c1", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the connection's context was cancelled and the session was never closed — " +
			"this is the only close signal the transport gives, so subscriptions would leak")
	}
	if sessions.Len() != 0 {
		t.Fatalf("Len = %d after close, want 0", sessions.Len())
	}
}

// The teardown callback runs at most once, or a client would see two
// `client.disconnected` events for one disconnect.
func TestTheTeardownCallbackRunsAtMostOnce(t *testing.T) {
	sessions := NewSessions()
	var calls int32
	sessions.OnClose(func(string) { atomic.AddInt32(&calls, 1) })

	ctx, cancel := context.WithCancel(context.Background())
	if err := sessions.Open(ctx, &Session{ConnID: "c1"}); err != nil {
		t.Fatal(err)
	}
	sessions.Close("c1")
	sessions.Close("c1")
	cancel()
	time.Sleep(100 * time.Millisecond)

	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Fatalf("teardown ran %d times, want 1 — a duplicate would emit two "+
			"client.disconnected events for one disconnect", n)
	}
}

func TestRequireNamesTheMethodThatWasTooEarly(t *testing.T) {
	sessions := NewSessions()
	_, err := sessions.Require("c1", "subscribe")
	if err == nil {
		t.Fatal("Require succeeded on an unknown connection")
	}
	if !strings.Contains(err.(*Error).Data.What, "subscribe") {
		t.Errorf("the refusal does not name the method: %q", err.(*Error).Data.What)
	}
}
