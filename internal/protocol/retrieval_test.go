package protocol

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/search"
	"github.com/cjlapao/aeon/internal/tools"
)

type searchCall struct {
	root, corpus, query string
	limit               int
}

func newSearchTable(seam search.SearchResolver) *Table {
	tbl := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	tbl.Search = seam
	tbl.RegisterSearchQuery()
	return tbl
}

func TestSearchQueryAbsentWhenRetrievalIsNot(t *testing.T) {
	// The no-retrieval shape, seen from the method table: the daemon wires
	// seams after NewTable and RegisterSearchQuery with a nil seam leaves
	// the method OUT — absent, not registered-and-refusing (V13, V21).
	tbl := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	tbl.Search = nil
	if tbl.RegisterSearchQuery() {
		t.Fatal("RegisterSearchQuery claimed success with no seam — the nil answer is the V13 build speaking")
	}
	for _, m := range append(tbl.Methods(), tbl.PublicMethods()...) {
		if m == MethodSearchQuery {
			t.Fatal("search.query is registered without a resolver — absence means it does not answer at all")
		}
	}
	// And WITH the seam it is there — the same one line of wiring the
	// daemon does, no more.
	live := newSearchTable(func(ctx context.Context, root, corpus, query string, limit int) (search.SearchAnswer, error) {
		return search.SearchAnswer{}, nil
	})
	var found bool
	for _, m := range live.Methods() {
		if m == MethodSearchQuery {
			found = true
		}
	}
	if !found {
		t.Fatal("search.query is missing although the seam is wired")
	}
	for _, m := range live.PublicMethods() {
		if m == MethodSearchQuery {
			return // product surface, not a debug method
		}
	}
	t.Error("search.query is Debug-classified — a surface the app depends on must not be excludable by accident")
}

func TestSearchQuerySocketAndToolCallSameResolver(t *testing.T) {
	// D15/P34's proof is one function value: the socket door and the tool
	// door call THE SAME resolver, and both are driven from here through
	// their real entries (the daemon's dispatch and the registry's Run).
	var calls []searchCall
	seam := func(ctx context.Context, root, corpus, query string, limit int) (search.SearchAnswer, error) {
		calls = append(calls, searchCall{root, corpus, query, limit})
		return search.SearchAnswer{Hits: []search.SearchHit{{Source: "a.go", LineStart: 1, LineEnd: 9, Score: 0.5}}}, nil
	}
	tbl := newSearchTable(seam)
	tools.SetSearchResolver(seam)
	t.Cleanup(func() { tools.SetSearchResolver(nil) })

	if _, err := tbl.handleSearchQuery(context.Background(), nil, json.RawMessage(`{"componentRoot":"/r","corpus":"code","query":"retry"}`)); err != nil {
		t.Fatalf("socket door failed: %v", err)
	}
	tool, ok := tools.Registry()["search"]
	if !ok {
		t.Fatal("the tool door is not in the registry although the same seam is wired — one condition must drive both")
	}
	res := tool.Run(context.Background(), tools.ToolContext{WorkspaceRoot: "/r"}, json.RawMessage(`{"corpus":"code","query":"retry"}`))
	if res.IsError {
		t.Fatalf("tool door failed: %s", res.Content)
	}
	if len(calls) != 2 {
		t.Fatalf("the resolver saw %d calls, want 2 — a door invented its own path", len(calls))
	}
	if calls[0] != calls[1] {
		t.Fatalf("the doors disagree on the call — socket %+v vs tool %+v (D15: one implementation, two transports)", calls[0], calls[1])
	}
}

func TestSearchQueryNamesCorpusAndRoot(t *testing.T) {
	called := false
	tbl := newSearchTable(func(ctx context.Context, root, corpus, query string, limit int) (search.SearchAnswer, error) {
		called = true
		return search.SearchAnswer{}, nil
	})
	for _, bad := range []string{
		`{"corpus":"code","query":"x"}`,                      // no componentRoot
		`{"componentRoot":"/r","query":"x"}`,                 // no corpus
		`{"componentRoot":"/r","corpus":"wiki","query":"x"}`, // unknown corpus
		`{"componentRoot":"/r","corpus":"code","query":" "}`, // empty query
	} {
		if _, err := tbl.handleSearchQuery(context.Background(), nil, json.RawMessage(bad)); err == nil {
			t.Errorf("%s answered — the refusals must fire before the resolver", bad)
		}
	}
	if called {
		t.Error("a refused params set reached the resolver")
	}
	// The refusals name what they refuse (house rule): root-less and the
	// two corpora are both spoken.
	_, err := tbl.handleSearchQuery(context.Background(), nil, json.RawMessage(`{"componentRoot":"/r","corpus":"wiki","query":"x"}`))
	// The three-part shape (T10/C9): the specifics ride in Data.What, and
	// Error() alone prints code+message — so the O8 check reads the part
	// that carries the naming.
	var e *Error
	if err == nil || !errors.As(err, &e) || !strings.Contains(e.Data.What, "code") || !strings.Contains(e.Data.What, "history") {
		t.Errorf("the corpus refusal does not name both options in its What (O8): %v", err)
	}
}

func TestSearchQueryPassesTheRefusalThrough(t *testing.T) {
	// D2: index-empty reaches the caller as the resolver's own sentence —
	// the reason, not a shrug, and never a confident empty list.
	tbl := newSearchTable(func(ctx context.Context, root, corpus, query string, limit int) (search.SearchAnswer, error) {
		return search.SearchAnswer{}, errors.New("retrieval: refused the query at /r (code) — index-empty: nothing indexed yet")
	})
	_, err := tbl.handleSearchQuery(context.Background(), nil, json.RawMessage(`{"componentRoot":"/r","corpus":"code","query":"anything"}`))
	if err == nil || !strings.Contains(err.Error(), "index-empty") {
		t.Fatalf("the named emptiness was swallowed (D2): %v", err)
	}
}
