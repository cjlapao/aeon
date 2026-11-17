package protocol

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// deliver is what the daemon does, reduced to what a test can assert on: route
// an event to the subscriptions that match, and record what each received.
type recorder struct {
	t   *testing.T
	tbl *Table
	got map[string][]Event // subscriptionId -> events
}

func newRecorder(t *testing.T) *recorder {
	r := &recorder{t: t, tbl: newTestTable(t), got: map[string][]Event{}}
	r.tbl.OnSubscribed = func(sub *Subscription, replay []Event) {
		for _, ev := range replay {
			ev.SubscriptionID = sub.ID
			r.got[sub.ID] = append(r.got[sub.ID], ev)
		}
	}
	return r
}

// emit appends an event and fans it out exactly as the daemon's publisher does.
func (r *recorder) emit(kind, componentRoot string, payload any) Event {
	r.t.Helper()
	ev, err := r.tbl.Events.Append(kind, componentRoot, payload)
	if err != nil {
		r.t.Fatal(err)
	}
	for _, sub := range r.tbl.Subs.Matching(ev.Kind, ev.ComponentRoot) {
		out := ev
		out.SubscriptionID = sub.ID
		r.got[sub.ID] = append(r.got[sub.ID], out)
		r.tbl.Subs.Advance(sub.ConnID, sub.ID, ev.Seq)
	}
	return ev
}

func (r *recorder) subscribe(connID string, p SubscribeParams) SubscribeResult {
	r.t.Helper()
	initializedOnce(r.t, r.tbl, connID)
	res, err := r.tbl.Dispatch(context.Background(), connID, MethodSubscribe, mustJSON(r.t, p))
	if err != nil {
		r.t.Fatalf("subscribe: %v", err)
	}
	return res.(SubscribeResult)
}

func initializedOnce(t *testing.T, tbl *Table, connID string) {
	t.Helper()
	if _, ok := tbl.Sessions.Get(connID); ok {
		return
	}
	initialized(t, tbl, connID, PROTOCOL_VERSION)
}

// D4 — asserted as ZERO over a run, not as "the right ones arrived".
//
// One daemon multiplexes every component on the machine. A filter that matched
// everything would pass a test that only checked the expected events were
// there.
func TestASubscriberScopedToComponentAReceivesZeroEventsFromComponentB(t *testing.T) {
	r := newRecorder(t)
	a := r.subscribe("c1", SubscribeParams{Filter: Filter{ComponentRoot: "/repo-a"}})
	b := r.subscribe("c2", SubscribeParams{Filter: Filter{ComponentRoot: "/repo-b"}})

	// A run, not one event: twenty events alternating between two projects.
	for i := 0; i < 10; i++ {
		r.emit("loop.started", "/repo-a", nil)
		r.emit("loop.started", "/repo-b", nil)
	}

	for _, ev := range r.got[a.SubscriptionID] {
		if ev.ComponentRoot != "/repo-a" {
			t.Fatalf("the /repo-a subscriber received an event from %q", ev.ComponentRoot)
		}
	}
	for _, ev := range r.got[b.SubscriptionID] {
		if ev.ComponentRoot != "/repo-b" {
			t.Fatalf("the /repo-b subscriber received an event from %q", ev.ComponentRoot)
		}
	}
	if len(r.got[a.SubscriptionID]) != 10 || len(r.got[b.SubscriptionID]) != 10 {
		t.Fatalf("counts = %d and %d, want 10 each",
			len(r.got[a.SubscriptionID]), len(r.got[b.SubscriptionID]))
	}
}

func TestASubscriberToLoopStarReceivesLoopStartedAndNotStepStarted(t *testing.T) {
	r := newRecorder(t)
	s := r.subscribe("c1", SubscribeParams{Filter: Filter{Kinds: []string{"loop.*"}}})

	for i := 0; i < 5; i++ {
		r.emit("loop.started", "/r", nil)
		r.emit("step.started", "/r", nil)
		r.emit("loop.finished", "/r", nil)
	}

	for _, ev := range r.got[s.SubscriptionID] {
		if !strings.HasPrefix(ev.Kind, "loop.") {
			t.Fatalf("a loop.* subscriber received %q", ev.Kind)
		}
	}
	if n := len(r.got[s.SubscriptionID]); n != 10 {
		t.Fatalf("received %d events, want 10 (five loop.started + five loop.finished)", n)
	}
}

// No prefix matching on paths. `/repo` matching `/repo-backup` is the near-miss
// that leaks one project's activity into another's client.
func TestComponentRootMatchesExactly(t *testing.T) {
	f := Filter{ComponentRoot: "/repo"}
	if f.Matches("loop.started", "/repo-backup") {
		t.Error("/repo matched /repo-backup")
	}
	if f.Matches("loop.started", "/repo/sub") {
		t.Error("/repo matched /repo/sub — a subdirectory is a different project root")
	}
	if !f.Matches("loop.started", "/repo") {
		t.Error("/repo did not match /repo")
	}
}

// O1 — one case per invalid form. A filter that silently matches nothing is the
// hardest bug here to diagnose from outside: connected, and nothing arrives.
func TestAnInvalidKindsWildcardIsANamedError(t *testing.T) {
	for _, bad := range []string{
		"*.started",    // leading
		"loop.*.x",     // interior
		"loop.**",      // two stars
		"loop.?tarted", // single-character wildcard
		"loop.[abc]",   // character class
		"loop.{a,b}",   // brace expansion
		"",             // empty
	} {
		t.Run(bad, func(t *testing.T) {
			err := Filter{Kinds: []string{bad}}.Validate()
			if err == nil {
				t.Fatalf("%q was accepted; it would be treated as a literal kind name and "+
					"match nothing", bad)
			}
			var e *Error
			if !errors.As(err, &e) || e.Code != -31010 {
				t.Fatalf("want -31010, got %v", err)
			}
			if e.Data.Todo == "" {
				t.Error("the refusal does not say what to do instead")
			}
		})
	}
}

func TestValidKindPatternsAreAccepted(t *testing.T) {
	for _, ok := range []string{"loop.started", "loop.*", "*", "step.tool.finished"} {
		if err := (Filter{Kinds: []string{ok}}).Validate(); err != nil {
			t.Errorf("%q was rejected: %v", ok, err)
		}
	}
}

// D6 — asserted ON THE REGISTRY. Delivery stopping is what you observe when a
// connection closes regardless; the registry emptying is what proves nothing
// was retained.
func TestDroppingTheConnectionEmptiesTheRegistry(t *testing.T) {
	tbl := newTestTable(t)
	tbl.Sessions.OnClose(func(connID string) { tbl.Subs.RemoveConn(connID) })

	ctx, cancel := context.WithCancel(context.Background())
	if err := tbl.Sessions.Open(ctx, &Session{ConnID: "c1", Version: PROTOCOL_VERSION}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := tbl.Dispatch(ctx, "c1", MethodSubscribe, mustJSON(t, SubscribeParams{})); err != nil {
			t.Fatal(err)
		}
	}
	if tbl.Subs.Len() != 3 {
		t.Fatalf("registry holds %d, want 3", tbl.Subs.Len())
	}

	cancel()
	waitFor(t, "the registry to empty", func() bool { return tbl.Subs.Len() == 0 })
	if tbl.Subs.LenConn("c1") != 0 {
		t.Fatal("the connection's bucket survived")
	}
}

// D7 — two subscriptions on one connection, independently filtered and tagged.
func TestTwoSubscriptionsOnOneConnectionAreIndependentlyFilteredAndTagged(t *testing.T) {
	r := newRecorder(t)
	loops := r.subscribe("c1", SubscribeParams{Filter: Filter{Kinds: []string{"loop.*"}}})
	steps := r.subscribe("c1", SubscribeParams{Filter: Filter{Kinds: []string{"step.*"}}})

	if loops.SubscriptionID == steps.SubscriptionID {
		t.Fatal("two subscriptions got the same id; a client could not tell their streams apart")
	}

	r.emit("loop.started", "/r", nil)
	r.emit("step.started", "/r", nil)
	r.emit("step.finished", "/r", nil)

	if n := len(r.got[loops.SubscriptionID]); n != 1 {
		t.Errorf("the loop.* subscription received %d events, want 1", n)
	}
	if n := len(r.got[steps.SubscriptionID]); n != 2 {
		t.Errorf("the step.* subscription received %d events, want 2", n)
	}
	for _, ev := range r.got[loops.SubscriptionID] {
		if ev.SubscriptionID != loops.SubscriptionID {
			t.Error("an event was not tagged with the subscription that delivered it")
		}
	}
}

// A client racing its own teardown is ordinary; making it an error would mean
// every client needs code for a race it did not cause.
func TestUnsubscribingTwiceIsNotAnError(t *testing.T) {
	tbl := newTestTable(t)
	initialized(t, tbl, "c1", PROTOCOL_VERSION)
	res, err := tbl.Dispatch(context.Background(), "c1", MethodSubscribe, mustJSON(t, SubscribeParams{}))
	if err != nil {
		t.Fatal(err)
	}
	id := res.(SubscribeResult).SubscriptionID

	for i := 0; i < 2; i++ {
		if _, err := tbl.Dispatch(context.Background(), "c1", MethodUnsubscribe,
			mustJSON(t, UnsubscribeParams{SubscriptionID: id})); err != nil {
			t.Fatalf("unsubscribe %d failed: %v", i+1, err)
		}
	}
	if tbl.Subs.Len() != 0 {
		t.Fatalf("registry holds %d after unsubscribe", tbl.Subs.Len())
	}
	// And an id that never existed.
	if _, err := tbl.Dispatch(context.Background(), "c1", MethodUnsubscribe,
		mustJSON(t, UnsubscribeParams{SubscriptionID: "s-never"})); err != nil {
		t.Fatalf("unsubscribing an unknown id errored: %v", err)
	}
}

// O4 — the refusal is REPORTED, not a silent drop. A subscription that vanished
// looks exactly like a filter that matches nothing.
func TestThePerConnectionBoundIsEnforcedAndReported(t *testing.T) {
	tbl := newTestTable(t)
	initialized(t, tbl, "c1", PROTOCOL_VERSION)

	for i := 0; i < MaxSubscriptionsPerConnection; i++ {
		if _, err := tbl.Dispatch(context.Background(), "c1", MethodSubscribe,
			mustJSON(t, SubscribeParams{})); err != nil {
			t.Fatalf("subscription %d of %d was refused early: %v", i+1, MaxSubscriptionsPerConnection, err)
		}
	}
	_, err := tbl.Dispatch(context.Background(), "c1", MethodSubscribe, mustJSON(t, SubscribeParams{}))
	if err == nil {
		t.Fatal("the per-connection bound was not enforced")
	}
	var e *Error
	if !errors.As(err, &e) || e.Code != -31011 {
		t.Fatalf("want -31011, got %v", err)
	}
	if !strings.Contains(e.Data.What, "per-connection") {
		t.Errorf("the refusal does not say which limit was hit: %q", e.Data.What)
	}
}

func TestTheTotalBoundIsEnforcedAndReported(t *testing.T) {
	tbl := newTestTable(t)
	// Spread across connections so the per-connection bound is not what trips.
	conns := MaxSubscriptionsTotal / MaxSubscriptionsPerConnection
	for c := 0; c < conns; c++ {
		id := "c" + itoa(c)
		initialized(t, tbl, id, PROTOCOL_VERSION)
		for i := 0; i < MaxSubscriptionsPerConnection; i++ {
			if _, err := tbl.Dispatch(context.Background(), id, MethodSubscribe,
				mustJSON(t, SubscribeParams{})); err != nil {
				t.Fatalf("conn %s sub %d refused early: %v", id, i, err)
			}
		}
	}
	if tbl.Subs.Len() != MaxSubscriptionsTotal {
		t.Fatalf("registry holds %d, want %d", tbl.Subs.Len(), MaxSubscriptionsTotal)
	}

	initialized(t, tbl, "one-more", PROTOCOL_VERSION)
	_, err := tbl.Dispatch(context.Background(), "one-more", MethodSubscribe, mustJSON(t, SubscribeParams{}))
	if err == nil {
		t.Fatal("the total bound was not enforced")
	}
	var e *Error
	if !errors.As(err, &e) || e.Code != -31011 {
		t.Fatalf("want -31011, got %v", err)
	}
	if !strings.Contains(e.Data.What, "total") {
		t.Errorf("the refusal does not say which limit was hit: %q", e.Data.What)
	}
}

// An invalid filter is refused BEFORE a subscription is registered, or the
// client holds an id for a subscription that will never deliver.
func TestAnInvalidFilterRegistersNothing(t *testing.T) {
	tbl := newTestTable(t)
	initialized(t, tbl, "c1", PROTOCOL_VERSION)
	if _, err := tbl.Dispatch(context.Background(), "c1", MethodSubscribe,
		mustJSON(t, SubscribeParams{Filter: Filter{Kinds: []string{"*.bad"}}})); err == nil {
		t.Fatal("an invalid filter was accepted")
	}
	if tbl.Subs.Len() != 0 {
		t.Fatalf("a refused subscribe left %d subscriptions behind", tbl.Subs.Len())
	}
}

// I6 — the registry is the only place subscriptions live. A second store would
// make D6's assertion meaningless while looking like belt and braces.
func TestThereIsExactlyOneSubscriptionRegistry(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	var constructors []string
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			// A second map of subscriptions anywhere but the registry.
			ts, ok := n.(*ast.TypeSpec)
			if !ok {
				return true
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok || ts.Name.Name == "Subscriptions" {
				return true
			}
			for _, field := range st.Fields.List {
				if mt, ok := field.Type.(*ast.MapType); ok {
					if isSubscriptionType(mt.Value) {
						constructors = append(constructors, file+":"+ts.Name.Name)
					}
				}
			}
			return true
		})
	}
	if len(constructors) > 0 {
		t.Errorf("a second store of subscriptions exists in %v; the registry is the only "+
			"place they live (I6), or D6's assertion means nothing", constructors)
	}
}

func isSubscriptionType(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.StarExpr:
		return isSubscriptionType(v.X)
	case *ast.Ident:
		return v.Name == "Subscription"
	case *ast.MapType:
		return isSubscriptionType(v.Value)
	}
	return false
}

// O4 requires the numbers to be stated as guesses. A bound presented as
// measured is a bound nobody will revisit.
func TestTheBoundsAreDocumentedAsGuesses(t *testing.T) {
	src, err := readSource("subscribe.go")
	if err != nil {
		t.Fatal(err)
	}
	doc := src[:strings.Index(src, "// Subscription is one client's")]
	for _, want := range []string{"guess", "Revisit"} {
		if !strings.Contains(doc, want) {
			t.Errorf("the bounds are not documented as guesses (looking for %q)", want)
		}
	}
}

func readSource(path string) (string, error) {
	b, err := os.ReadFile(path)
	return string(b), err
}

// waitFor polls until cond is true, failing with a message that says what was
// being waited for. Teardown is asynchronous — the transport cancels a context
// and a goroutine reaps — so an immediate assertion would be a race.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// Every event handed to a subscription is numbered 1, 2, 3 for that subscription alone, so a client can tell a dropped
// notification from a filter that simply skips most of the log.
func TestEachSubscriptionNumbersItsOwnDeliveries(t *testing.T) {
	r := NewSubscriptions()
	a, _ := r.Add("c1", Filter{}, 1)
	b, _ := r.Add("c1", Filter{}, 1)
	for want := uint64(1); want <= 3; want++ {
		if got := r.NextDelivery("c1", a.ID); got != want {
			t.Fatalf("a: got %d, want %d", got, want)
		}
	}
	if got := r.NextDelivery("c1", b.ID); got != 1 {
		t.Fatalf("b counts on its own, got %d", got)
	}
	if got := r.NextDelivery("c1", "s-gone"); got != 0 {
		t.Fatalf("an unknown subscription numbers nothing, got %d", got)
	}
}

// A heartbeat is due only for a subscription that has been handed something since its last one, and sending it counts as a
// delivery that does not make the next one due.
func TestAHeartbeatIsDueOnlyAfterADelivery(t *testing.T) {
	r := NewSubscriptions()
	a, _ := r.Add("c1", Filter{}, 1)
	if len(r.DueForTick()) != 0 {
		t.Fatal("nothing delivered: nothing due")
	}
	r.NextDelivery("c1", a.ID)
	if due := r.DueForTick(); len(due) != 1 || due[0].ID != a.ID {
		t.Fatalf("a delivery makes a heartbeat due, got %v", due)
	}
	r.NextDelivery("c1", a.ID) // the heartbeat itself is a delivery
	r.MarkTicked("c1", a.ID)
	if len(r.DueForTick()) != 0 {
		t.Fatal("the heartbeat must not make another one due")
	}
	r.NextDelivery("c1", a.ID)
	if len(r.DueForTick()) != 1 {
		t.Fatal("a later delivery makes it due again")
	}
}

// A notify-only subscription takes hinted events and nothing else, unless a kind is named beside it.
func TestNotifyOnlyTakesHintedEvents(t *testing.T) {
	only := Filter{NotifyOnly: true}
	if only.MatchesEvent("loop.advanced", "", false) {
		t.Error("an unhinted event must not reach a notify-only subscription")
	}
	if !only.MatchesEvent("loop.finished", "", true) {
		t.Error("a hinted event must reach it")
	}
	both := Filter{NotifyOnly: true, Kinds: []string{"loop.*"}}
	if !both.MatchesEvent("loop.advanced", "", false) || both.MatchesEvent("model.delta", "", false) {
		t.Error("named kinds are taken beside the hints, and only those")
	}
	if !(Filter{}).MatchesEvent("anything", "", false) {
		t.Error("a plain empty filter still matches everything")
	}
}
