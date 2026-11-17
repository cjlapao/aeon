package daemon

// scorecard_reads_test.go — S63 T5 (RT3): every result of the plane
// carries RT3's sentence, on EVERY door of the plane — the daemon's
// answer, not just the wire type. A card whose statement field drifted
// (or went empty) is a score reading as an instruction, and this test
// is what catches it on the door results themselves, by reflection over
// every exported door: each result struct must carry a `Statement
// string` whose value equals the constant.

import (
	"context"
	"reflect"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

func newReadsDaemon(t *testing.T) (*store.Store, *Daemon) {
	t.Helper()
	s, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("open the test store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	root := "/repo/reads-statement"
	if _, err := s.CreateComponent(store.NewComponent{Name: "rs", Slug: "rs", Root: root}); err != nil {
		t.Fatalf("seed component: %v", err)
	}
	w := &wire{}
	d := New(Options{Store: s, Logger: w, Notify: w.notify})
	return s, d
}

// TestScorecardResultsAlwaysCarryTheStatement: ten doors, ten sentences.
// The digest is in the walk — its section list is long; its statement
// is one of the two required fields the client must never render
// without.
func TestScorecardResultsAlwaysCarryTheStatement(t *testing.T) {
	s, d := newReadsDaemon(t)
	ctx := context.Background()
	sc := newScorecardReads(s)
	rt := newRetroReads(d)
	dg := newDigestReads(s)

	results := map[string]any{}
	if r, err := sc.Agent(ctx, protocol.ScorecardAgentParams{Agent: "some-agent"}); err == nil {
		results["scorecard.agent"] = r
	}
	if r, err := sc.Blueprint(ctx, protocol.ScorecardBlueprintParams{Blueprint: "some-blueprint"}); err == nil {
		results["scorecard.blueprint"] = r
	}
	if r, err := sc.Component(ctx, protocol.ScorecardComponentParams{ID: "/repo/reads-statement"}); err == nil {
		results["scorecard.component"] = r
	}
	if r, err := sc.Compare(ctx, protocol.ScorecardCompareParams{
		Kind: "component", Subject: "/repo/reads-statement",
		WindowA: protocol.ScorecardWindow{}, WindowB: protocol.ScorecardWindow{Since: "2026-09-01T00:00:00Z", Until: "2026-10-01T00:00:00Z"},
	}); err == nil {
		results["scorecard.compare"] = r
	}
	if r, err := sc.Vocabulary(ctx); err == nil {
		results["scorecard.vocabulary"] = r
	}
	if r, err := rt.List(ctx, protocol.RetroListParams{}); err == nil {
		results["retro.list"] = r
	}
	if r, err := rt.Status(ctx); err == nil {
		results["retro.status"] = r
	}
	if r, err := rt.Backfill(ctx, protocol.Actor{Kind: "user", ID: "peer-1"}, protocol.RetroBackfillParams{}); err == nil {
		results["retro.backfill"] = r
	}
	if r, err := dg.Learned(ctx, protocol.DigestParams{ID: "/repo/reads-statement"}); err == nil {
		results["digest.learned"] = r
	}
	if len(results) < 9 {
		t.Fatalf("only %d doors answered on an empty store — the plane must ANSWER, not error (RT12)", len(results))
	}
	for method, res := range results {
		v := reflect.ValueOf(res)
		var got string
		found := false
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).Name == "Statement" {
				got = v.Field(i).String()
				found = true
			}
		}
		if !found {
			t.Errorf("%s: the result carries no Statement field — RT3's sentence must ride every result", method)
			continue
		}
		if got != protocol.ScorecardStatement {
			t.Errorf("%s: statement = %q, want RT3's sentence verbatim", method, got)
		}
	}
	// retro.show answers once a row exists: the door with the richest
	// payload carries it too.
	id, _, err := s.RecordRetroOutcome(store.RetroOutcome{
		ComponentRoot: "/repo/reads-statement", LoopSlug: "st-loop",
		TerminalKey: "st-loop:done:1", OutcomeKind: "done", Complete: true, Source: "recorder",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	r, err := newRetroReads(d).Show(ctx, protocol.RetroShowParams{OutcomeID: id})
	if err != nil {
		t.Fatal(err)
	}
	if r.Statement != protocol.ScorecardStatement {
		t.Errorf("retro.show: statement = %q", r.Statement)
	}
}
