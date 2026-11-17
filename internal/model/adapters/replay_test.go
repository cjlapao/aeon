package adapters

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

// S18 T5 — the thinking-replay rule, keyed on the PRODUCER (M27).
//
// The transcript is the record and keeps every thinking block (M1); what a
// wire carries is a translation decision. Anthropic gets its own signatures
// back byte-identically (M17 — it rejects a tool-use turn whose prior
// thinking was altered or dropped); OpenAI, in either of its two wire
// formats, gets none of anyone's (there is no field for them, and a foreign
// signature is unverifiable here by construction). Every omission is COUNTED
// (M4) and the count travels in its own number (O1).
//
// The rule reads "did THIS target produce it?", never "does the target
// support thinking?" (O6) — a hypothetical reasoning-capable OpenAI still
// cannot receive Anthropic's signatures, and the plausible wrong
// implementation is the one that checks capability flags instead.

func signedBlock(text, sig string) model.Block {
	return model.Block{Kind: model.BlockThinking, Text: text, Signature: sig}
}

func producedBy(route model.Route) *model.Provenance {
	p := model.NewProvenance(route)
	return &p
}

func thinkingDroppedOf(t *testing.T, adapter model.Adapter) int {
	t.Helper()
	td, ok := adapter.(interface{ ThinkingDropped() int })
	if !ok {
		t.Fatalf("%T does not report ThinkingDropped — every adapter must (M4)", adapter)
	}
	return td.ThinkingDropped()
}

func bodyJSON(t *testing.T, req *http.Request) string {
	t.Helper()
	raw, err := json.Marshal(wireBody(t, req))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// TestThinkingReplayedOnlyToItsProducer — the rule's two directions in one
// pair of turns, both carrying the same private text and the same style of
// signature. Only the producer's own comes back.
func TestThinkingReplayedOnlyToItsProducer(t *testing.T) {
	anth := testRoute(ProviderAnthropic)
	open := testRoute(ProviderOpenAI)

	a := newAnthropic()
	req, err := a.ToWire(model.ModelRequest{
		Route: anth,
		Messages: []model.Message{
			{Role: model.RoleUser, Content: []model.Block{model.TextBlock("go")}},
			{Role: model.RoleAssistant, Provenance: producedBy(anth),
				Content: []model.Block{signedBlock("MINE-anthropic", "sig-anthropic"), model.TextBlock("did A")}},
			{Role: model.RoleUser, Content: []model.Block{model.TextBlock("more")}},
			{Role: model.RoleAssistant, Provenance: producedBy(open),
				Content: []model.Block{signedBlock("MINE-openai", "sig-openai"), model.TextBlock("did B")}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	body := bodyJSON(t, req)
	if !strings.Contains(body, "MINE-anthropic") || !strings.Contains(body, "sig-anthropic") {
		t.Error("Anthropic's own signed thinking was not replayed — M17's promise broken on its own turns")
	}
	if strings.Contains(body, "MINE-openai") || strings.Contains(body, "sig-openai") {
		t.Fatal("a FOREIGN signature was offered to Anthropic — replaying it is a bet on somebody else's validator (M27)")
	}
	if n := thinkingDroppedOf(t, a); n != 1 {
		t.Errorf("ThinkingDropped = %d, want the one withheld foreign block (M4)", n)
	}
	// The withheld turn's TEXT still reaches the wire as the conversation
	// record it is — the drop is the thinking block only, not the turn.
	if !strings.Contains(body, "did B") {
		t.Error("dropping the foreign thinking also dropped the turn's answer")
	}
}

// TestCrossProviderDropIsCounted — three Anthropic thinking blocks, one
// OpenAI request: none cross the wire, and the number that says so is 3.
// (The number reaching the STEP'S RECORD is pinned in internal/runtime and
// internal/daemon; this is the adapter's half.)
func TestCrossProviderDropIsCounted(t *testing.T) {
	anth := testRoute(ProviderAnthropic)
	msgs := []model.Message{{Role: model.RoleUser, Content: []model.Block{model.TextBlock("go")}}}
	for i := 0; i < 3; i++ {
		msgs = append(msgs,
			model.Message{Role: model.RoleAssistant, Provenance: producedBy(anth),
				Content: []model.Block{signedBlock("ANTHROPIC-PRIVATE-REASONING", "sig-x")}},
		)
	}

	a := newOpenAI()
	req, err := a.ToWire(model.ModelRequest{Route: testRoute(ProviderOpenAI), Messages: msgs})
	if err != nil {
		t.Fatal(err)
	}
	if body := bodyJSON(t, req); strings.Contains(body, "ANTHROPIC-PRIVATE-REASONING") {
		t.Fatal("a thinking block crossed the OpenAI chat wire")
	}
	if n := thinkingDroppedOf(t, a); n != 3 {
		t.Errorf("ThinkingDropped = %d, want 3 (M4: dropped AND counted)", n)
	}

	// The responses wire, same rule — one provider, two formats, neither
	// accepts a foreign signature.
	r := newOpenAIResponses()
	reqR, err := r.ToWire(model.ModelRequest{Route: responsesRoute(), Messages: msgs})
	if err != nil {
		t.Fatal(err)
	}
	if body := bodyJSON(t, reqR); strings.Contains(body, "ANTHROPIC-PRIVATE-REASONING") {
		t.Fatal("a thinking block crossed the responses wire")
	}
	if n := thinkingDroppedOf(t, r); n != 3 {
		t.Errorf("responses ThinkingDropped = %d, want 3", n)
	}
}

// TestStoreIsUnmodified — the round trip's foundation: a drop is a WIRE
// translation. The record keeps every block, or A→B→A becomes one-way and
// nobody finds out until they try to go back (M1, I1, D2). Asserted on
// BYTES: marshal the transcript, build all three adapters' requests from
// it, marshal again; not one byte may move.
func TestStoreIsUnmodified(t *testing.T) {
	anth := testRoute(ProviderAnthropic)
	open := testRoute(ProviderOpenAI)
	msgs := []model.Message{
		{Role: model.RoleUser, Content: []model.Block{model.TextBlock("go")}},
		{Role: model.RoleAssistant, Provenance: producedBy(anth),
			Content: []model.Block{signedBlock("keep me", "sig==keep+1"), model.TextBlock("A")}},
		{Role: model.RoleAssistant, Provenance: producedBy(open),
			Content: []model.Block{signedBlock("keep me too", "sig==keep+2"), model.TextBlock("B")}},
	}
	before, err := json.Marshal(msgs)
	if err != nil {
		t.Fatal(err)
	}

	for _, build := range []func() error{
		func() error { _, e := newAnthropic().ToWire(model.ModelRequest{Route: anth, Messages: msgs}); return e },
		func() error { _, e := newOpenAI().ToWire(model.ModelRequest{Route: open, Messages: msgs}); return e },
		func() error {
			_, e := newOpenAIResponses().ToWire(model.ModelRequest{Route: responsesRoute(), Messages: msgs})
			return e
		},
	} {
		if err := build(); err != nil {
			t.Fatal(err)
		}
	}

	after, err := json.Marshal(msgs)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("building requests MUTATED the transcript:\n  before %s\n  after  %s\n"+
			"If a drop ever reached the record, A-to-B-to-A would become one-way", before, after)
	}
}

// TestRuleKeysOnProducerNotTargetCapability — THE test against the plausible
// wrong implementation. A target that DECLARES thinking still must not
// receive signatures it did not issue; an implementation that reads "does
// the target support thinking?" passes every other test in the slice and
// breaks the day a second reasoning provider exists (O6, I3).
func TestRuleKeysOnProducerNotTargetCapability(t *testing.T) {
	// A hypothetical: this Anthropic route DECLARES thinking among its wire
	// capabilities — the capability-flag reading of the rule would now
	// replay ANY thinking block to it.
	anth := testRoute(ProviderAnthropic)
	anth.WireCapabilities = []string{"thinking", "tools"}
	open := testRoute(ProviderOpenAI)

	a := newAnthropic()
	req, err := a.ToWire(model.ModelRequest{
		Route: anth,
		Messages: []model.Message{
			{Role: model.RoleUser, Content: []model.Block{model.TextBlock("go")}},
			{Role: model.RoleAssistant, Provenance: producedBy(open),
				Content: []model.Block{signedBlock("FOREIGN-PRIVATE", "foreign-sig"), model.TextBlock("ok")}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if body := bodyJSON(t, req); strings.Contains(body, "FOREIGN-PRIVATE") || strings.Contains(body, "foreign-sig") {
		t.Fatal("a thinking block was replayed because the TARGET declares thinking — the rule keys on the PRODUCER, never on the target (O6)")
	}
	if n := thinkingDroppedOf(t, a); n != 1 {
		t.Errorf("ThinkingDropped = %d, want 1", n)
	}
}

// TestSignatureIsNeverAltered — replayed signatures are byte-identical: no
// re-encoding, no trim, no whitespace diplomacy. The signature is the
// provider's seal; anything done to it here fails verification there (M17).
func TestSignatureIsNeverAltered(t *testing.T) {
	anth := testRoute(ProviderAnthropic)
	// Awkward on purpose: padding characters, slashes, plus signs, and a
	// trailing run that a careless round trip through a map or a trim eats.
	const sig = "ErUB+/==CkYIBBgCKkB3+Zm3/Lx1FIXTURE==signature+/=="

	a := newAnthropic()
	req, err := a.ToWire(model.ModelRequest{
		Route: anth,
		Messages: []model.Message{
			{Role: model.RoleAssistant, Provenance: producedBy(anth),
				Content: []model.Block{signedBlock("deep thought", sig)}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	// The exact bytes, inside the exact JSON encoding: the only acceptable
	// occurrence is the string itself.
	if c := strings.Count(bodyJSON(t, req), sig); c != 1 {
		t.Errorf("the signature appeared %d times, want exactly once, unaltered", c)
	}
}

// TestMixedTranscriptToAnthropic — the S18 round trip in miniature on the
// wire: a transcript with Anthropic turns AND OpenAI turns replays exactly
// the Anthropic thinking, keeps every text turn, and says what it withheld.
func TestMixedTranscriptToAnthropic(t *testing.T) {
	anth := testRoute(ProviderAnthropic)
	open := testRoute(ProviderOpenAI)

	msgs := []model.Message{
		{Role: model.RoleUser, Content: []model.Block{model.TextBlock("weather then time")}},
		{Role: model.RoleAssistant, Provenance: producedBy(anth),
			Content: []model.Block{signedBlock("call the weather tool", "asig-1"), model.TextBlock("weather: rain")}},
		{Role: model.RoleUser, Content: []model.Block{model.TextBlock("and the time?")}},
		{Role: model.RoleAssistant, Provenance: producedBy(open),
			Content: []model.Block{signedBlock("compute the clock", "osig-1"), model.TextBlock("time: nine")}},
	}

	a := newAnthropic()
	req, err := a.ToWire(model.ModelRequest{Route: anth, Messages: msgs})
	if err != nil {
		t.Fatal(err)
	}
	body := bodyJSON(t, req)
	for _, want := range []string{"call the weather tool", "asig-1", "weather: rain", "time: nine"} {
		if !strings.Contains(body, want) {
			t.Errorf("the mixed replay lost %q", want)
		}
	}
	for _, refuse := range []string{"compute the clock", "osig-1"} {
		if strings.Contains(body, refuse) {
			t.Errorf("the mixed replay carried %q — not this provider's", refuse)
		}
	}
	if n := thinkingDroppedOf(t, a); n != 1 {
		t.Errorf("ThinkingDropped = %d, want exactly the OpenAI turn's block", n)
	}
}

// TestDropCountIsPerRequestNotCumulative — the count describes THIS
// request. Production gets a fresh adapter per call (the registry factory),
// which is what makes the number per-request; two adapters report their own
// stories, not a running total.
func TestDropCountIsPerRequestNotCumulative(t *testing.T) {
	anth := testRoute(ProviderAnthropic)
	foreign := []model.Message{{Role: model.RoleAssistant, Provenance: producedBy(anth),
		Content: []model.Block{signedBlock("one", "s1"), signedBlock("two", "s2"), model.TextBlock("answered")}}}

	first := newOpenAI()
	if _, err := first.ToWire(model.ModelRequest{Route: testRoute(ProviderOpenAI), Messages: foreign}); err != nil {
		t.Fatal(err)
	}
	if n := thinkingDroppedOf(t, first); n != 2 {
		t.Fatalf("first request reported %d, want its own 2", n)
	}

	second := newOpenAI()
	one := []model.Message{{Role: model.RoleAssistant, Provenance: producedBy(anth),
		Content: []model.Block{signedBlock("three", "s3"), model.TextBlock("answered again")}}}
	if _, err := second.ToWire(model.ModelRequest{Route: testRoute(ProviderOpenAI), Messages: one}); err != nil {
		t.Fatal(err)
	}
	if n := thinkingDroppedOf(t, second); n != 1 {
		t.Errorf("second request reported %d — it read the first one's story (want 1)", n)
	}
	if n := thinkingDroppedOf(t, first); n != 2 {
		t.Errorf("the first adapter's count moved with someone else's request: %d", n)
	}
}
