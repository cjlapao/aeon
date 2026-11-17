package attention

import (
	"context"
	"testing"

	"github.com/cjlapao/aeon/internal/store"
)

type fakeGates struct{ rows []store.AttentionFailedGate }

func (f fakeGates) FailedGatesForScope([]string) ([]store.AttentionFailedGate, error) { return f.rows, nil }

type fakeProposals struct{ rows []store.AttentionOpenProposal }

func (f fakeProposals) OpenProposals() ([]store.AttentionOpenProposal, error) { return f.rows, nil }

type fakeBugs struct{ rows []store.AttentionUnvalidatedBugs }

func (f fakeBugs) UnvalidatedBugsForScope([]string) ([]store.AttentionUnvalidatedBugs, error) {
	return f.rows, nil
}

func TestGatesProposalsTriageSourcesReportAsNeedsYou(t *testing.T) {
	r := NewRegistry(func(string) bool { return true }, nil)
	r.Register(GatesSource{Gates: fakeGates{rows: []store.AttentionFailedGate{{RunID: "run-9", LoopSlug: "s1", ComponentRoot: "/w/a", StartedAt: "2026-11-02T01:00:00Z"}}}})
	r.Register(ProposalsSource{Proposals: fakeProposals{rows: []store.AttentionOpenProposal{{ID: 7, Kind: "policy", CreatedAt: "2026-11-02T00:00:00Z"}}}})
	r.Register(TriageSource{Bugs: fakeBugs{rows: []store.AttentionUnvalidatedBugs{{ComponentRoot: "/w/a", Count: 3, Oldest: "2026-11-02T02:00:00Z"}}}})
	res, err := r.Evaluate(context.Background(), Query{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 3 {
		t.Fatalf("want 3 items, got %d: %+v", len(res.Items), res.Items)
	}
	// oldest first inside one severity: proposal 00:00, gate 01:00, bugs 02:00
	for i, kind := range []string{"proposal", "gate-failed", "bugs-unvalidated"} {
		if res.Items[i].Kind != kind || res.Items[i].Severity != NeedsYou {
			t.Fatalf("item %d: want %s needs-you, got %s %s", i, kind, res.Items[i].Kind, res.Items[i].Severity)
		}
	}
	if res.Items[0].DoorMethod != "" || res.Items[0].ItemRef != "7" {
		t.Fatalf("proposal door: %+v", res.Items[0])
	}
	if res.Items[1].ItemRef != "run-9" || res.Items[2].Reason != "3 bugs wait for validation" {
		t.Fatalf("gate/bugs items: %+v %+v", res.Items[1], res.Items[2])
	}
	// `gates` was a reserved name: registering it flips it from not-wired to reporting.
	for _, s := range res.Sources {
		if s.Name == "gates" && s.State != "reporting" {
			t.Fatalf("gates source state = %s, want reporting", s.State)
		}
	}
}

func TestProposalsAreTheDaemonsNotAComponents(t *testing.T) {
	src := ProposalsSource{Proposals: fakeProposals{rows: []store.AttentionOpenProposal{{ID: 1, Kind: "policy", CreatedAt: "2026-11-02T00:00:00Z"}}}}
	scoped, err := src.Items(context.Background(), Query{Scope: Scope{RootPaths: []string{"/w/a"}}})
	if err != nil || len(scoped) != 0 {
		t.Fatalf("a component scope must not carry daemon-level proposals: %v %v", scoped, err)
	}
}

func TestOneBugIsSingular(t *testing.T) {
	items, _ := TriageSource{Bugs: fakeBugs{rows: []store.AttentionUnvalidatedBugs{{ComponentRoot: "/w/a", Count: 1, Oldest: "x"}}}}.Items(context.Background(), Query{})
	if items[0].Reason != "1 bug waits for validation" {
		t.Fatalf("reason = %q", items[0].Reason)
	}
}

func TestMoreDoorsAreGatedAndFallBackToTheGenericSentence(t *testing.T) {
	for _, kind := range []string{"gate-failed", "bugs-unvalidated"} {
		m, s := Door(kind, Item{Kind: kind, ItemRef: "5"}, func(string) bool { return false })
		if m != "" || s != Generic(kind) {
			t.Fatalf("%s unwired: method %q sentence %q", kind, m, s)
		}
		m, _ = Door(kind, Item{Kind: kind, ItemRef: "5"}, func(string) bool { return true })
		if m == "" {
			t.Fatalf("%s wired: no method", kind)
		}
	}
}
