package policy_test

// The property assertions over the committed corpus (T1's corpus.json, read
// through T2's one loader).
//
// Two claims, said apart (discovery risk 3) so nobody later "fixes" one by
// breaking the other:
//
//   - The ORDER the operator authored each agent's rules in is a fidelity
//     property this slice preserves and asserts — TestRuleOrderIsPreserved
//     pins that the loader hands back the file's order, unsorted.
//   - The DECISION rule is the shipped engine's — deny beats allow regardless
//     of specificity (PL4, evaluate.go) — and has nothing to do with that
//     authored order. Order in the file is data the operator wrote, never an
//     evaluation order any reader may infer from it.
//
// And no test in this file asserts a COUNT. The corpus measures live agent
// files — 367 rules / 51 patterns when this was written, where the docs'
// older numbers said 366 / 50, because an agent was simply added since
// (brief D5). A count assertion fails the day someone adds an agent, which
// is the day the corpus is supposed to absorb the change silently. Every
// assertion here is a property: something exists, or a set computed from the
// rules themselves equals what it must be. Where the file carries its own
// measurements in Properties, the test re-derives from Agents rather than
// trusting them — a file agreeing with itself proves nothing.

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/policy"
)

// mustCorpus is the one load every property test shares: the committed bytes,
// through the one loader. A corpus that will not parse is not a red test — it
// is a broken specification, so it fails the helper loudly.
func mustCorpus(tb testing.TB) policy.Corpus {
	tb.Helper()
	c, rej := policy.ParseCorpus(policy.EmbeddedCorpus())
	if rej != nil {
		tb.Fatalf("the committed corpus is the specification and it must parse: %s", rej.Reason)
	}
	if c.Agents == nil {
		tb.Fatal("the committed corpus carries no agents at all")
	}
	return c
}

func TestCorpusHasMidStarPattern(t *testing.T) {
	// The matcher (T3) must treat '*' inside a command, not only at its end.
	// The property: at least one authored pattern has '*' sitting between
	// non-space text — computed by scanning the rules, never by reading
	// Properties.HasMidStarPattern, which would only replay the generator.
	c := mustCorpus(t)
	found := false
	for _, a := range c.Agents {
		for _, r := range a.Rules {
			for i := 0; i < len(r.Pattern); i++ {
				if r.Pattern[i] == '*' &&
					strings.TrimSpace(r.Pattern[:i]) != "" &&
					strings.TrimSpace(r.Pattern[i+1:]) != "" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Error("no pattern carries '*' between non-space text; the matcher's mid-star case " +
			"would have no real data behind it")
	}
}

func TestCorpusHasExactPattern(t *testing.T) {
	// The other matcher arm: a pattern with no '*' at all must exist, so the
	// exact-match path is exercised by real corpus data rather than only by
	// T3's hand-written fixtures.
	c := mustCorpus(t)
	found := false
	for _, a := range c.Agents {
		for _, r := range a.Rules {
			if !strings.Contains(r.Pattern, "*") {
				found = true
			}
		}
	}
	if !found {
		t.Error("no pattern is free of '*'; the exact-match arm of the matcher has no corpus backing")
	}
}

func TestCorpusCarriesConflictingVerdicts(t *testing.T) {
	// Some pattern must appear with more than one verdict across agents, so
	// the fold the engine ships (T6) is exercised against genuine tension
	// rather than a hand-made one. Re-derived from Agents; the generator's
	// claim is then checked against the re-derivation, never the reverse.
	c := mustCorpus(t)
	verdicts := map[string]map[policy.Action]bool{}
	for _, a := range c.Agents {
		for _, r := range a.Rules {
			if verdicts[r.Pattern] == nil {
				verdicts[r.Pattern] = map[policy.Action]bool{}
			}
			verdicts[r.Pattern][r.Verdict] = true
		}
	}
	var computed []string
	for pattern, vs := range verdicts {
		if len(vs) > 1 {
			computed = append(computed, pattern)
		}
	}
	slices.Sort(computed)
	if len(computed) == 0 {
		t.Fatal("no pattern carries conflicting verdicts across agents; the deny-wins fold has no real case")
	}
	for _, claimed := range c.Properties.ConflictingPatterns {
		if !slices.Contains(computed, claimed) {
			t.Errorf("the generator claims %q conflicts across agents but re-deriving from the rules does not: %v",
				claimed, computed)
		}
	}
}

func TestCorpusWildcardSetIsAsteriskOnly(t *testing.T) {
	// The whole matcher is justified by one claim: '*' is the only wildcard
	// in the corpus. So scan EVERY pattern for every metacharacter the usual
	// glob languages carry — ? * [ ] { } — and require the set actually
	// found to be exactly ["*"]. Computed from the rules, deliberately:
	// reading Properties.WildcardChars would only prove the generator agrees
	// with itself, which is the self-consistent verification this repo's
	// identifier-integrity rule exists to forbid.
	c := mustCorpus(t)
	const metaChars = "?*[]{}"
	seen := map[string]bool{}
	for _, a := range c.Agents {
		for _, r := range a.Rules {
			for _, ch := range r.Pattern {
				if strings.ContainsRune(metaChars, ch) {
					seen[string(ch)] = true
				}
			}
		}
	}
	var found []string
	for ch := range seen {
		found = append(found, ch)
	}
	slices.Sort(found)
	if !slices.Equal(found, []string{"*"}) {
		t.Errorf("the corpus's wildcard metacharacters, computed from every pattern, are %v; want exactly [\"*\"] "+
			"— a second one means T3's matcher silently treats it as a literal while the corpus means it as a wildcard", found)
	}
}

func TestCorpusCarriesAskRules(t *testing.T) {
	// Load-bearing for T7 and T12: ask must exist as real corpus data before
	// it becomes a third action, so the migration, the fold and the
	// suspension are all built against rules that actually exist rather than
	// against a fixture somebody typed. "ask" is copied verbatim from the
	// committed corpus.json and from corpusgen's vocabulary.
	c := mustCorpus(t)
	found := false
	for _, a := range c.Agents {
		for _, r := range a.Rules {
			if r.Verdict == policy.Action("ask") {
				found = true
			}
		}
	}
	if !found {
		t.Error("the corpus carries no ask rule; T7 would be adding a third action no data uses")
	}
}

func TestRuleOrderIsPreserved(t *testing.T) {
	// The fidelity claim. Three arms, because they fail for different things:
	//
	//  1. every agent's pattern sequence matches an independent re-parse of
	//     the raw bytes — this catches the loader sorting, deduplicating or
	//     dropping something while parsing;
	//  2. an anchor hand-copied from the committed fixture for one agent —
	//     arm 1 compares the loader to the file, so it agrees with whatever
	//     the file says; this is the arm that FAILS when the fixture's own
	//     authored order is hand-reversed, proving the file's order is pinned
	//     and not just self-consistent;
	//  3. the same sequence is demonstrably NOT lexicographically sorted —
	//     if RulesFor quietly returned a sorted copy, a sorted fixture would
	//     hide arms 1 and 2, and this is what that hiding cannot survive.
	c := mustCorpus(t)

	var doc struct {
		Agents []struct {
			Agent string `json:"agent"`
			Rules []struct {
				Pattern string `json:"pattern"`
			} `json:"rules"`
		} `json:"agents"`
	}
	if err := json.Unmarshal(policy.EmbeddedCorpus(), &doc); err != nil {
		t.Fatalf("re-parsing the raw corpus: %v", err)
	}

	var wantNames, gotNames []string
	for _, a := range doc.Agents {
		wantNames = append(wantNames, a.Agent)
	}
	for _, a := range c.Agents {
		gotNames = append(gotNames, a.Agent)
	}
	if !slices.Equal(wantNames, gotNames) {
		t.Fatalf("the corpus lists agents in %v, the raw file in %v", gotNames, wantNames)
	}

	for _, wa := range doc.Agents {
		got, ok := c.RulesFor(wa.Agent)
		if !ok {
			t.Fatalf("agent %q is in the raw file but RulesFor reports it absent", wa.Agent)
		}
		var want, seq []string
		for _, wr := range wa.Rules {
			want = append(want, wr.Pattern)
		}
		for _, r := range got {
			seq = append(seq, r.Pattern)
		}
		if !slices.Equal(want, seq) {
			t.Errorf("agent %q: RulesFor returned %v, want the raw file's order %v", wa.Agent, seq, want)
		}
	}

	// Arm 2, the anchor: the first patterns of brief-author exactly as the
	// committed fixture authors them, hand-copied. Regenerating the corpus
	// (T1's deliberate act) may move this anchor; hand-reversing the fixture
	// may not survive it.
	anchor, ok := c.RulesFor("brief-author")
	if !ok {
		t.Fatal("brief-author is in the fixture; RulesFor reports it absent")
	}
	first := []string{"*", "pwd", "ls*", "cat*", "head*"}
	if len(anchor) < len(first) {
		t.Fatalf("brief-author carries fewer rules than its anchor; the anchor needs updating with a regeneration, by hand, in the open")
	}
	var head []string
	for _, r := range anchor[:len(first)] {
		head = append(head, r.Pattern)
	}
	if !slices.Equal(head, first) {
		t.Errorf("brief-author's authored order is %v, want the fixture's %v — the authored sequence is the operator's statement",
			head, first)
	}

	// Arm 3: the sequence is not a sorted accident.
	var seq []string
	for _, r := range anchor {
		seq = append(seq, r.Pattern)
	}
	if slices.IsSorted(seq) {
		t.Error("brief-author's rules arrived lexicographically sorted; authoring order is data, not an index to rebuild")
	}
}

func TestParseCorpusRefusesBadShapes(t *testing.T) {
	// The strictness contract: every shape the generator did not emit comes
	// back as a *Reject naming the field and the value it refused (DB19),
	// and the zero Corpus comes back with it — no partial data to misuse.
	// The fixture below is hand-authored, so its own expectations may be
	// exact: it is under this file's control, unlike the committed corpus
	// no assertion here counts.
	head := `{"generated_by":"aeon/tools/corpusgen","source":"opencode/agents"`
	props := `"properties":{"wildcard_chars":["*"],"has_mid_star_pattern":true,"has_exact_pattern":true,"conflicting_patterns":["git push*"]}`
	agents := `"agents":[` +
		`{"agent":"alpha","rules":[{"pattern":"pwd","verdict":"allow"}]},` +
		`{"agent":"beta","rules":[{"pattern":"git push*","verdict":"deny"},{"pattern":"git push*","verdict":"ask"}]}]`
	good := head + `,` + props + `,` + agents + `}`

	cases := []struct {
		name      string
		raw       string
		wantField string
		wantValue string
	}{
		{"accepts a good corpus carrying an ask rule", good, "", ""},
		{"unknown verdict", strings.Replace(good, `"verdict":"ask"`, `"verdict":"granted"`, 1), "verdict", "granted"},
		{"empty pattern", strings.Replace(good, `"pattern":"pwd"`, `"pattern":""`, 1), "pattern", ""},
		{"whitespace pattern", strings.Replace(good, `"pattern":"pwd"`, `"pattern":"   "`, 1), "pattern", ""},
		{"agent with no rules",
			strings.Replace(good,
				`{"agent":"beta","rules":[{"pattern":"git push*","verdict":"deny"},{"pattern":"git push*","verdict":"ask"}]}`,
				`{"agent":"beta","rules":[]}`, 1),
			"rules", "beta"},
		{"duplicate agent name", strings.Replace(good, `"agent":"beta"`, `"agent":"alpha"`, 1), "duplicate", "alpha"},
		{"empty agent name", strings.Replace(good, `"agent":"alpha"`, `"agent":""`, 1), "agent name", ""},
		{"empty generated_by", strings.Replace(good, `"generated_by":"aeon/tools/corpusgen"`, `"generated_by":""`, 1), "generated_by", ""},
		{"empty source", strings.Replace(good, `"source":"opencode/agents"`, `"source":""`, 1), "source", ""},
		{"missing properties", head + `,` + agents + `}`, "properties", ""},
		{"missing agents", head + `,` + props + `}`, "agents", ""},
		{"empty agents array", head + `,` + props + `,"agents":[]}`, "agents", ""},
		{"unknown field", head + `,` + props + `,"nonesuch":1,` + agents + `}`, "nonesuch", ""},
		{"trailing second document", good + `{}`, "trailing", ""},
		{"not JSON at all", `{"generated_by":"x",`, "EOF", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, rej := policy.ParseCorpus([]byte(tc.raw))
			if tc.wantField == "" {
				if rej != nil {
					t.Fatalf("the good corpus must parse, refused: %s", rej.Reason)
				}
				if want := []string{"git push*", "pwd"}; !slices.Equal(got.DistinctPatterns(), want) {
					t.Errorf("DistinctPatterns returned %v, want the fixture's %v", got.DistinctPatterns(), want)
				}
				if !slices.Equal(got.Properties.ConflictingPatterns, []string{"git push*"}) {
					t.Errorf("the properties block did not survive parsing: %v", got.Properties.ConflictingPatterns)
				}
				if r, ok := got.RulesFor("beta"); !ok || r[1].Verdict != policy.Action("ask") {
					t.Errorf("the fixture's ask rule did not survive parsing (ok=%v)", ok)
				}
				return
			}
			if rej == nil {
				t.Fatalf("refused nothing; parsed %v", got)
			}
			if !strings.Contains(rej.Reason, tc.wantField) {
				t.Errorf("refusal %q does not name the field %q", rej.Reason, tc.wantField)
			}
			if tc.wantValue != "" && !strings.Contains(rej.Reason, tc.wantValue) {
				t.Errorf("refusal %q does not name the refused value %q", rej.Reason, tc.wantValue)
			}
			if got.Agents != nil || got.Properties.WildcardChars != nil || got.GeneratedBy != "" || got.Source != "" {
				t.Errorf("a refusal must carry no partial corpus, got %+v", got)
			}
		})
	}
}

func TestRulesForDistinguishesMissingAgent(t *testing.T) {
	// P31's three answers: "this agent authored no rules" and "there is no
	// such agent" may never collapse into one nil. The corpus itself cannot
	// show the first (ParseCorpus refuses a rules-less agent), so the
	// empty-but-present answer is exercised on a constructed Corpus — which
	// is exactly the shape a caller can hold if it built one by hand.
	c := mustCorpus(t)

	if _, ok := c.RulesFor("no-such-agent-xyz"); ok {
		t.Error("RulesFor reported an absent agent as present")
	}

	name := "brief-author"
	entries, ok := c.RulesFor(name)
	if !ok {
		t.Fatalf("RulesFor reported %q absent", name)
	}
	if len(entries) == 0 {
		t.Fatalf("RulesFor returned nothing for %q, which the parser would never produce", name)
	}

	lit := policy.Corpus{Agents: []policy.CorpusAgent{{Agent: "authored-nothing"}}}
	empty, ok := lit.RulesFor("authored-nothing")
	if !ok {
		t.Error(`RulesFor reported a present-but-ruleless agent absent — "no rules" and "no such agent" are different answers (P31)`)
	}
	if len(empty) != 0 {
		t.Errorf("the ruleless agent came back carrying %v", empty)
	}

	// The copy claim: what a caller does to its slice may not reach back
	// into the corpus.
	entries[0].Pattern = "MUTATED"
	again, ok := c.RulesFor(name)
	if !ok || again[0].Pattern == "MUTATED" {
		t.Error("RulesFor handed out the corpus's own backing array instead of a copy")
	}
}
