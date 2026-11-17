package remote

// cost_test.go — S21 T8 (RA10): unknown stays unknown. The failure this file
// exists to prevent is the easiest one in the product: an empty field rendered
// as $0.00 (P31) — "unknown" becomes `—`, `—` becomes 0, and a total that
// looks confident is wrong in the direction nobody checks. The assertions are
// therefore about ABSENCES: which bytes must NOT appear, which fields must
// stay zero so they omit, which words must ride where a number would.

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

// setupWith adapts the shared two-peer world (callback_test.go's setup) to
// the cost probes; the reserved argument keeps the door open for a future
// per-test book without renaming six call sites.
func setupWith(t *testing.T, _ *fakeBook) (*Callbacks, *fakeBook, map[string]string) {
	t.Helper()
	return setup(t)
}

func mustRead(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// Gate 1 — TestRemoteCostWithNoReportIsUnknown: RA10's headline. The peer's
// silence lands as unknown, and BOTH number fields stay at their zero values
// so they OMIT from the record — an absent number and a zero number are
// different documents, and only one of them is honest here (M9/M10).
func TestRemoteCostWithNoReportIsUnknown(t *testing.T) {
	c := CostFor("docs-peer", nil)
	if c.State != model.CostUnknown {
		t.Fatalf("state %q — RA10: no report is unknown", c.State)
	}
	if c.USD != 0 || c.USDAtLeast != 0 {
		t.Errorf("a number entered the record for a cost nobody reported: %+v", c)
	}
	if c.Reason != ReasonPeerReportedNoUsage {
		t.Errorf("reason %q — must be the closed token", c.Reason)
	}
	doc := mustJSON(t, c)
	if !strings.Contains(doc, `"state":"unknown"`) {
		t.Errorf("state must serialise (O2): %s", doc)
	}
	if strings.Contains(doc, `"usd"`) || strings.Contains(doc, `"usdAtLeast"`) {
		t.Errorf("the zero fields must OMIT (absent is not zero, M9): %s", doc)
	}
	if strings.Contains(doc, "0.00") || strings.Contains(doc, "$0") {
		t.Errorf("the look of free appeared: %s", doc)
	}
	// The no-report silence and the not-yet-asked silence are DIFFERENT
	// facts (scope 2): S14's row-level default for a still-waiting task is
	// "cost-not-reported" (agentsvc toTask — the literal here is a
	// byte-pinned witness to that other fact, not a duplication of it).
	if ReasonPeerReportedNoUsage == "cost-not-reported" {
		t.Error("the two silences collapsed")
	}
	// validateCost on the other side of the seam refuses an unknown with no
	// reason; ours always carries one.
	if c.Reason == "" {
		t.Error("unexplained absence (M10)")
	}
}

// The unknown APPEARS in the rollup rather than being omitted (scope 1): the
// seam is asked to land a cost on EVERY completion — the store receives a
// value whose state says unknown, never a nil that a writer could skip.
func TestUnknownAppearsInTheRollupRatherThanBeingOmitted(t *testing.T) {
	cb, book, secrets := setupWith(t, nil)
	req := complete("docs-peer", "7c2e", noon)
	req.Signature = sign(secrets["docs-peer"], req) // verified; NO cost field — the decision under test
	cb.Deliver(req)
	if book.landCalls != 1 {
		t.Fatalf("land calls: %d", book.landCalls)
	}
	if book.lastCost == nil {
		t.Fatal("the seam was asked to land NO cost — omission is how a row learns to render zero")
	}
	if book.lastCost.State != model.CostUnknown || book.lastCost.Reason != ReasonPeerReportedNoUsage {
		t.Errorf("the landed cost should have said unknown-with-reason: %+v", book.lastCost)
	}
}

// Gate — a reported remote cost is recorded with its PROVENANCE (scope 3):
// whose number it was must ride IN the record, not beside it.
func TestReportedRemoteCostCarriesProvenance(t *testing.T) {
	c := CostFor("docs-peer", &CostReport{USD: 1.5})
	// The best state a peer's claim can buy is partial (RA5 × P32): a known
	// parent resting on a stranger's number is the lie with a signature.
	if c.State != model.CostPartial {
		t.Fatalf("a peer's self-report earned state %q — partial is its ceiling", c.State)
	}
	if c.USDAtLeast != 1.5 {
		t.Errorf("the claim is our floor (at-least): %+v", c)
	}
	if c.USD != 0 {
		t.Error("a claimed total must not enter as a MEASURED total")
	}
	found := false
	for _, m := range c.Missing {
		if m == "remote-cost-unverified:docs-peer" {
			found = true
		}
	}
	if !found {
		t.Errorf("Missing must name whose metering is unverified: %v", c.Missing)
	}
	if !strings.Contains(c.Reason, "docs-peer") {
		t.Errorf("the reason must say whose number it was: %q", c.Reason)
	}
	// validateCost (the seam's write gate) admits partial with a floor ≥ 0.
	if c.USDAtLeast < 0 {
		t.Error("negative floor")
	}
}

// A claim in another currency is NAMED and NOT COUNTED — no silent
// conversion, no silent discard.
func TestForeignCurrencyClaimIsNamedNotCounted(t *testing.T) {
	c := CostFor("docs-peer", &CostReport{USD: 40, Currency: "eur"})
	if c.State != model.CostUnknown || c.USDAtLeast != 0 || c.USD != 0 {
		t.Errorf("the number was counted: %+v", c)
	}
	if !strings.Contains(c.Reason, "currency") {
		t.Errorf("the claim must be named in the record: %q", c.Reason)
	}
	// Upper-case USD still counts (peers mean well; case is not a refusal).
	ok := CostFor("docs-peer", &CostReport{USD: 0.5, Currency: "USD"})
	if ok.State != model.CostPartial {
		t.Errorf("USD spelled upper-case was refused: %+v", ok)
	}
	// A negative claim is a claim no ledger can hold.
	neg := CostFor("docs-peer", &CostReport{USD: -1})
	if neg.State != model.CostUnknown || neg.USDAtLeast != 0 {
		t.Errorf("a negative entered the record: %+v", neg)
	}
}

// Gate 2 — TestUnknownRemoteChildMakesParentNotKnown (P32): the table is
// S14's/S3's and is NOT re-implemented here (the grep gate below pins that);
// what T8 pins is the INPUT shape that makes the existing propagation do its
// job — the no-report child enters S3's Add / S14's fold as a true unknown
// with NO floor, so the parent can never be known AND the total cannot be
// quietly moved by the absence. S14's own gate TestUnknownChildMakesParentNotKnown
// proves the fold; this proves the remote child is THAT shape.
func TestUnknownRemoteChildMakesParentNotKnown(t *testing.T) {
	child := CostFor("docs-peer", nil)
	// The contract foldChain keys on (agentsvc/cost.go): State drives the
	// known=false rule; USDAtLeast feeds the floor — it MUST be zero here,
	// or a nobody-reported number moves the total.
	if child.State != model.CostUnknown || child.USD != 0 || child.USDAtLeast != 0 || child.Reason == "" {
		t.Fatalf("the child is not the unknown-shape S14 folds: %+v", child)
	}
	// Through the exported table itself (model.Add is the same primitive
	// foldChain's arithmetic rides on): known + our unknown → UNKNOWN, never
	// known, and the measured floor survives (S3's "at least" honesty).
	parent := model.Cost{State: model.CostKnown, USD: 1.20}.Add(child)
	if parent.State == model.CostKnown {
		t.Fatal("a parent went KNOWN with an unknown child — P32's exact lie")
	}
	if parent.State != model.CostUnknown {
		t.Errorf("the table said %q, want unknown (S3: unknown absorbs, and the remote child contributes no floor)", parent.State)
	}
	if parent.USDAtLeast != 1.20 {
		t.Errorf("the measured floor must survive: %+v", parent)
	}
	if !strings.Contains(parent.Reason, ReasonPeerReportedNoUsage) {
		t.Errorf("the reason must travel with the total so the reader learns WHICH child to go ask: %q", parent.Reason)
	}
	// And a PEER-CLAIMED child keeps the parent at best partial — never
	// known on a stranger's number.
	withClaim := model.Cost{State: model.CostKnown, USD: 1.20}.Add(CostFor("docs-peer", &CostReport{USD: 1.5}))
	if withClaim.State == model.CostKnown {
		t.Fatal("a parent went known resting on a peer's claim")
	}
	if withClaim.State != model.CostPartial || withClaim.USDAtLeast != 2.70 {
		t.Errorf("floors must sum under partial: %+v", withClaim)
	}
}

// Gate 3 — TestUnknownNeverRendersAsANumber: including inside a total. The
// unknown's OWN rendering carries no digits at all (the reason tokens are
// words by design); the total that carries an unknown child still says the
// state word, and the only number in it is the FLOOR that was measured —
// never a number for what nobody reported.
func TestUnknownNeverRendersAsANumber(t *testing.T) {
	own := RenderCost(CostFor("docs-peer", nil))
	if strings.ContainsAny(own, "0123456789") {
		t.Errorf("the unknown rendered as a number: %q", own)
	}
	if !strings.Contains(own, "unknown") || !strings.Contains(own, ReasonPeerReportedNoUsage) {
		t.Errorf("unknown must say WHAT it is and WHY: %q", own)
	}
	awaiting := RenderCost(model.Cost{State: model.CostUnknown, Reason: "cost-not-reported"})
	if strings.ContainsAny(awaiting, "0123456789") {
		t.Errorf("the other silence rendered as a number: %q", awaiting)
	}
	// Inside a total: the measured floor may show (it WAS measured — that is
	// S3's "at least", not a lie), but the state word rides with it and no
	// number attaches to the unknown child itself.
	total := model.Cost{State: model.CostKnown, USD: 1.20}.Add(CostFor("docs-peer", nil))
	line := RenderCost(total)
	if !strings.Contains(strings.ToLower(line), "unknown") {
		t.Errorf("a total over an unknown child that does not SAY unknown is P32's failure re-rendered: %q", line)
	}
	if strings.Contains(line, "known)") || strings.Contains(strings.ToLower(line), "(known") {
		t.Errorf("the total rendered as known: %q", line)
	}
	// And nothing this file produces claims the known state: every
	// construction path caps at partial.
	for _, probe := range []*CostReport{nil, {}, {USD: 1}, {USD: -1}, {USD: 1, Currency: "eur"}, {USD: 2, Currency: "usd"}} {
		if c := CostFor("docs-peer", probe); c.State == model.CostKnown {
			t.Errorf("a known state was produced from %v — RA5: a stranger's number does not earn known", probe)
		}
	}
}

// Validation list item 7 — no second rollup was added (scope 5). S14's
// RollupCost is the rollup; this package DECIDES the remote child's shape
// and nothing else. Banned tokens are assembled so this hunter cannot
// contain the hunted.
func TestNoSecondRollupWasAdded(t *testing.T) {
	raw := mustRead(t, "cost.go")
	// Comments are stripped before the sweep: the prose LEGITIMATELY names
	// the rollup it extends (the second claim below pins that it does); the
	// ban is on CODE that walks or sums.
	var code strings.Builder
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}
		code.WriteString(line)
		code.WriteByte('\n')
	}
	codeOnly := code.String()
	seen := 0
	banned := []string{
		"Rollup" + "Cost",
		"fold" + "Chain",
		"List" + "AgentTasks",
		"Chain" + "Edges",
		"wal" + "k(",
		"USD" + "AtLeast +=",
	}
	for _, b := range banned {
		if strings.Contains(codeOnly, b) {
			t.Errorf("cost.go contains %q — S14's rollup is the rollup; this file decides the child's shape, it does not walk or sum", b)
		}
		seen++
	}
	if seen < 6 {
		t.Fatalf("the ban list shrank to %d — the hunter must stay wider than the hunted", seen)
	}
	// The claim it makes instead: it cites the rollup it extends.
	if !strings.Contains(raw, "agentsvc/cost.go") {
		t.Error("cost.go must name the rollup it extends, not merely imply it")
	}
}

// RA10 × RA8 — the cost claim rides the SIGNATURE. A completion whose number
// was edited in flight (same signature, different usd) is refused exactly as
// a forged result would be — and provably never touches the store.
func TestCostClaimIsBoundToSignature(t *testing.T) {
	s, book, secrets := setupWith(t, nil)
	_ = book
	with := complete("docs-peer", "7c2e", noon)
	with.Cost = &CostReport{USD: 1.5}
	with.Signature = sign(secrets["docs-peer"], with) // sign the CLAIM, cost included
	tampered := with
	tampered.Cost = &CostReport{USD: 0.01} // the edit in flight
	tampered.Signature = with.Signature    // the signature stays the peer's
	ans := s.Deliver(tampered)
	if ans.Outcome != OutcomeRefused || ans.HTTPStatus != statusAuth {
		t.Fatalf("a tampered cost answer was %q/%d — a cost not bound to the signature is not a claim", ans.Outcome, ans.HTTPStatus)
	}
	if book.awaitingCall != 0 || book.landCalls != 0 {
		t.Errorf("the store was touched for an unverified number: awaiting=%d land=%d", book.awaitingCall, book.landCalls)
	}
	// The honestly-signed cost lands, and the seam receives the DECIDED
	// state (partial-with-provenance), not the raw claim.
	s2, book2, secrets2 := setupWith(t, nil)
	with2 := complete("docs-peer", "7c2e", noon)
	with2.Cost = &CostReport{USD: 1.5}
	with2.Signature = sign(secrets2["docs-peer"], with2)
	ans2 := s2.Deliver(with2)
	if ans2.Outcome != OutcomeLanded {
		t.Fatalf("honest signed cost refused: %s", ans2.Reason)
	}
	if book2.lastCost == nil || book2.lastCost.State != model.CostPartial || book2.lastCost.USDAtLeast != 1.5 {
		t.Errorf("the seam should have received the decided claim: %+v", book2.lastCost)
	}
}
