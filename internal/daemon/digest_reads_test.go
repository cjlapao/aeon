package daemon

// digest_reads_test.go — S63 T5 (RT12): the digest assembled from a
// REAL store, walking the marshalled answer. The three claims RT12
// makes about CONTENT get their teeth here: the fixed order survives an
// empty store (every section at its cannot-say shape, not an error),
// the learnings group by provenance in HONESTY ORDER with retracted
// rows excluded, and the whole answer is ids and enums — a planted
// goal-text sentinel greps out of every byte.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

type dgFixture struct {
	s    *store.Store
	root string
}

func newDgFixture(t *testing.T) *dgFixture {
	t.Helper()
	s, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("open the test store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	root := "/repo/digest-comp"
	if _, err := s.CreateComponent(store.NewComponent{Name: "dc", Slug: "dc", Root: root}); err != nil {
		t.Fatalf("seed component: %v", err)
	}
	return &dgFixture{s: s, root: root}
}

func (f *dgFixture) digest(t *testing.T, p protocol.DigestParams) protocol.DigestResult {
	t.Helper()
	out, err := newDigestReads(f.s).Learned(context.Background(), p)
	if err != nil {
		t.Fatalf("digest.learned: %v", err)
	}
	return out
}

func (f *dgFixture) raw(t *testing.T, p protocol.DigestParams) []byte {
	t.Helper()
	raw, err := json.Marshal(f.digest(t, p))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// TestDigestSectionsAreFixedOrder (daemon side): the order RT12 fixes
// survives the WIRE on a real answer, and an empty store answers with
// every section at its cannot-say shape — not an error, not a missing
// section.
func TestDigestSectionsAreFixedOrder(t *testing.T) {
	f := newDgFixture(t)
	raw := f.raw(t, protocol.DigestParams{ID: f.root})
	keys := jsonTopKeys(t, raw)
	want := []string{"window", "statement", "coverage", "outcomes", "rework", "blocked", "cost", "learned", "attention"}
	if len(keys) != len(want) {
		t.Fatalf("sections = %v, want %v", keys, want)
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Fatalf("sections = %v, want %v — RT12's order is the contract", keys, want)
		}
	}
	out := f.digest(t, protocol.DigestParams{ID: f.root})
	if out.Statement != protocol.ScorecardStatement {
		t.Error("the empty digest lost RT3's sentence")
	}
	// Empty ⇒ cannot-say, stated, not hidden.
	if out.Cost.PerCompletedItem.State != "cannot-say" {
		t.Errorf("empty store cost state = %q, want cannot-say", out.Cost.PerCompletedItem.State)
	}
	if out.Rework.ReviewRounds == nil || out.Rework.ReviewRounds.State != "cannot-say" {
		t.Error("empty store rework rounds must answer cannot-say, not be missing")
	}
	if out.Attention.Statement != "nothing needs you" {
		t.Errorf("empty attention = %q, want the explicit nothing-needs-you", out.Attention.Statement)
	}
	if len(out.Learned.Groups) != 4 {
		t.Fatalf("learned carries %d provenance groups, want the fixed 4 — absence must not reorder the rest", len(out.Learned.Groups))
	}
}

// TestDigestListsLearningsByProvenanceHonestyOrder: the four groups are
// told, observed, assumed, relayed — that order, always; and a
// RETRACTED learning (S52 LM2) is excluded, its body absent from every
// byte of the answer.
func TestDigestListsLearningsByProvenanceHonestyOrder(t *testing.T) {
	f := newDgFixture(t)
	for _, body := range []struct{ provenance, body string }{
		{"relayed", "BODY-RELAYED"},
		{"assumed", "BODY-ASSUMED"},
		{"observed", "BODY-OBSERVED"},
		{"told", "BODY-TOLD"},
	} {
		if _, err := f.s.CreateLearning(store.NewLearningRow{
			Scope: "project", ScopeKey: f.root, Body: body.body, Provenance: body.provenance,
		}); err != nil {
			t.Fatalf("seed learning %s: %v", body.provenance, err)
		}
	}
	hidden, err := f.s.CreateLearning(store.NewLearningRow{
		Scope: "project", ScopeKey: f.root, Body: "BODY-RETRACTED", Provenance: "observed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := f.s.SetLearningRetraction(hidden.ID, true, "walk: digest must not quote it", "user", "peer-1", nil); err != nil {
		t.Fatalf("retract: %v", err)
	}

	out := f.digest(t, protocol.DigestParams{ID: f.root})
	var order []string
	for _, g := range out.Learned.Groups {
		order = append(order, g.Provenance)
	}
	want := []string{"told", "observed", "assumed", "relayed"}
	for i := range want {
		if i >= len(order) || order[i] != want[i] {
			t.Fatalf("honesty order = %v, want %v", order, want)
		}
	}
	if len(out.Learned.Groups[1].Items) != 1 || out.Learned.Groups[1].Items[0].Body != "BODY-OBSERVED" {
		t.Errorf("the observed group does not hold the one ACTIVE observed learning: %+v", out.Learned.Groups[1].Items)
	}
	// Verbatim: a stored body rides whole, byte for byte.
	if out.Learned.Groups[0].Items[0].Body != "BODY-TOLD" {
		t.Error("a learning body was re-rendered — the digest quotes, it does not rewrite")
	}
	raw := f.raw(t, protocol.DigestParams{ID: f.root})
	if strings.Contains(string(raw), "BODY-RETRACTED") {
		t.Error("a retracted learning rode the digest — LM2's exclusion is broken")
	}
}

// TestDigestUsesIdsNotLoopText: a loop whose GOAL carries a sentinel
// phrase; the digest's answer must not contain the phrase anywhere —
// the largest loop rides as a LINK, the rework ladder as ids and
// integers, the cost as history's sentence. (The learning bodies are
// quoted BY DESIGN — they are already-validated words; the sentinel
// lives only in mutable loop text.)
func TestDigestUsesIdsNotLoopText(t *testing.T) {
	f := newDgFixture(t)
	const sentinel = "sentinel-goal-phrase-that-must-never-ride"
	if _, err := f.s.CreatePinnedLoop(store.NewLoop{
		Slug: "digest-loop", ComponentRoot: f.root, Root: f.root,
		Goal: "ship the recorder — " + sentinel, Status: "done",
	}, "dev-loop", 1, "x: y\n", "{}"); err != nil {
		t.Fatalf("seed loop: %v", err)
	}
	wall := int64(60000)
	floor := 1.25
	steps := 3
	if _, _, err := f.s.RecordRetroOutcome(store.RetroOutcome{
		ComponentRoot: f.root, LoopSlug: "digest-loop", TerminalKey: "digest-loop:done:1",
		OutcomeKind: "done", BlueprintID: "dev-loop", WallMS: &wall,
		CostUSDFloor: &floor, CostSteps: &steps, Complete: true, Source: "recorder",
	}, nil); err != nil {
		t.Fatalf("record outcome: %v", err)
	}
	raw := f.raw(t, protocol.DigestParams{ID: f.root})
	if strings.Contains(string(raw), sentinel) {
		t.Errorf("the digest carried mutable loop text — RT12 says ids and enums:\n%s", raw)
	}
	out := f.digest(t, protocol.DigestParams{ID: f.root})
	if out.Cost.LargestLoop == nil || out.Cost.LargestLoop.Loop != "digest-loop" {
		t.Error("the largest loop did not ride as a (root, slug) link")
	}
}

// --- byte-level key walker (the order is a property of the WIRE) ---------------

func jsonTopKeys(t *testing.T, raw []byte) []string {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		t.Fatalf("not an object: %v", err)
	}
	type frame struct{ inArray, wantKey bool }
	stack := []*frame{{wantKey: true}}
	var keys []string
	for {
		tok, err := dec.Token()
		if err != nil {
			return keys // EOF: the document closed
		}
		switch v := tok.(type) {
		case json.Delim:
			switch v {
			case '{':
				stack = append(stack, &frame{})
			case '[':
				stack = append(stack, &frame{inArray: true})
			default:
				stack = stack[:len(stack)-1]
				if len(stack) == 0 {
					return keys
				}
				if top := stack[len(stack)-1]; !top.inArray {
					top.wantKey = true
				}
			}
		case string:
			fr := stack[len(stack)-1]
			if !fr.inArray {
				if fr.wantKey && len(stack) == 1 {
					keys = append(keys, v)
				}
				fr.wantKey = !fr.wantKey
			}
		default:
			fr := stack[len(stack)-1]
			if !fr.inArray {
				fr.wantKey = true
			}
		}
	}
}
