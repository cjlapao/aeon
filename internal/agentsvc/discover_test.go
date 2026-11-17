package agentsvc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// T9 — discovery without dispatch. A13's shape, enforced by grep: candidates
// come out, a NAME goes in, and no exported path runs from a similarity
// score to Invoke.

func TestCardsAndSkillsAreEnumerable(t *testing.T) {
	_, cards, _ := cardedStore(t)
	d := cards.Discover()
	if len(d.Refuses) != 0 {
		t.Fatalf("the committed card must enumerate cleanly: %v", d.Refuses)
	}
	if len(d.Candidates) != 1 || d.Candidates[0].Agent != "reddit-agent" {
		t.Fatalf("the card must enumerate: %+v", d.Candidates)
	}
	got := d.Candidates[0]
	if got.Name != "reddit-agent" || got.Version != "1.0.0" || got.Description == "" {
		t.Fatalf("identity fields must ride: %+v", got)
	}
	if len(got.Skills) != 1 {
		t.Fatalf("the skill must enumerate: %+v", got.Skills)
	}
	sk := got.Skills[0]
	if sk.ID != "summarise-sentiment" || sk.Name == "" || sk.Description == "" {
		t.Fatalf("the reader needs more than an id: %+v", sk)
	}
	// The two fields the task names as the point of enumeration — a reader
	// judges fit from tags and examples; an index (S22) indexes on them.
	if len(sk.Tags) == 0 || len(sk.Examples) == 0 {
		t.Fatalf("tags and examples are what make a skill discoverable (T2/AS9): %+v", sk)
	}
	if sk.CandidateSkillRef() != SkillRef("reddit-agent", "summarise-sentiment") {
		t.Fatal("discovery must hand over the NAME spelled the way the door reads it")
	}
	// Deterministic: the same directory answers the same way twice.
	d2 := cards.Discover()
	if len(d2.Candidates) != len(d.Candidates) || d2.Candidates[0].Agent != d.Candidates[0].Agent {
		t.Fatal("listing order must not depend on directory order")
	}
}

func TestBrokenCardsAreExcludedAndNamed(t *testing.T) {
	// AS9's single truth: what is enumerable is what is invokable. A card
	// the invocation door refuses must not wave from the listing — and it
	// must not vanish silently either; the operator who published it reads
	// the why.
	s, cards, dir := cardedStore(t)
	_ = s
	// A tampered card: the committed bytes with the signature's last byte
	// flipped — loads nowhere, refuses loudly.
	raw, err := os.ReadFile(filepath.Join(dir, "cards", "reddit-agent.json"))
	if err != nil {
		t.Fatal(err)
	}
	var env struct {
		Card      json.RawMessage `json:"card"`
		PublicKey string          `json:"publicKey"`
		Signature string          `json:"signature"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatal(err)
	}
	env.Signature = strings.TrimSuffix(env.Signature, "=") + "A=="
	tampered, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cards", "tampered-agent.json"), tampered, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cards", "garbage-agent.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	d := cards.Discover()
	if len(d.Candidates) != 1 || d.Candidates[0].Agent != "reddit-agent" {
		t.Fatalf("broken cards must not enumerate: %+v", d.Candidates)
	}
	if len(d.Refuses) != 2 {
		t.Fatalf("both broken cards must be named, not swallowed: %v", d.Refuses)
	}
	joined := strings.Join(d.Refuses, "\n")
	if !strings.Contains(joined, "tampered-agent") || !strings.Contains(joined, "garbage-agent") {
		t.Fatalf("a refusal names the thing refused: %v", d.Refuses)
	}
	// And the door agrees with the listing (one verification path): the
	// tampered card is not invokable either.
	if _, err := cards.Card("tampered-agent"); err == nil {
		t.Fatal("the listing excluded it but the door would honour it — two truths")
	}
}

func TestInvocationRequiresAName(t *testing.T) {
	// The ask is spelled in NAME fields; nothing numeric or "best-match"-
	// shaped can stand in for one. Reflection over the real struct, not a
	// paraphrase of it.
	it := reflect.TypeOf(Invocation{})
	var callee, skill reflect.StructField
	found := map[string]bool{}
	for i := 0; i < it.NumField(); i++ {
		f := it.Field(i)
		n := strings.ToLower(f.Name)
		for _, bad := range []string{"score", "rank", "similarity", "best", "match", "candidate"} {
			if strings.Contains(n, bad) {
				t.Fatalf("Invocation carries %q — a name is the only way in", f.Name)
			}
		}
		if f.Name == "CalleeAgent" {
			callee, found["callee"] = f, true
		}
		if f.Name == "SkillID" {
			skill, found["skill"] = f, true
		}
	}
	if !found["callee"] || !found["skill"] {
		t.Fatal("the invocation must name its callee and its skill")
	}
	if callee.Type.Kind() != reflect.String || skill.Type.Kind() != reflect.String {
		t.Fatal("both halves of the name are strings — nothing else addresses a callee")
	}
	// And no exported entry point in the package takes a NUMBER that could
	// smuggle a score: signatures are checked at the source, the one place
	// a parameter hides. (RollupCost RETURNS money as float64 — that is a
	// cost, not a fit; the ban is on similarity-shaped INPUT.)
	src := pkgSources(t)
	bad := regexp.MustCompile(`(?m)^func [A-Z][^(]*\([^)]*\b(float32|float64)\b`)
	if m := bad.FindString(src); m != "" {
		t.Fatalf("an exported entry point takes a float parameter — the shape a score walks in through: %s", m)
	}
}

func TestNoPathFromScoreToInvocation(t *testing.T) {
	// Asserted by grep, over this package AND the CLI's adapter: similarity
	// picks what to READ ABOUT; a name picks what to CALL. There is no
	// embedding here, no scorer, no ranking, and therefore no path — the
	// vocabulary itself is absent.
	src := pkgSources(t)
	// The banned shapes are the ones that dispatch: a closeness measure with
	// a name that turns it into a call. (chain.go's local `best` in the
	// longest-path walk is a graph counter, not a fit figure — banning the
	// bare word would be banning English; the shapes that smuggle a fit in
	// as an API are what is forbidden, plus the vector machinery S22 owns
	// and must not reach in here.)
	for _, banned := range []string{"cosine", "similarity", "Similarity", "embedding", "Embedding",
		"sqlite_vec", "vec0", "rank", "Rank", "bestMatch", "bestAgent", "bestSkill", "nearest", "topK", "top-k", "relevance"} {
		if strings.Contains(src, banned) {
			t.Fatalf("agentsvc carries %q — S22 indexes cards; it does not get to turn a score into a call from here", banned)
		}
	}
	// The CLI half: the adapter is the production call site; it takes a
	// NAMED `skill:` from the lane and nothing else.
	adapter, err := os.ReadFile(filepath.Join("..", "..", "cmd", "aeon", "agent_services.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{"score", "rank", "similarity", "best", "candidate", "embedding", "cosine"} {
		if strings.Contains(strings.ToLower(string(adapter)), banned) {
			t.Fatalf("the CLI adapter mentions %q — dispatch-by-similarity starts exactly here", banned)
		}
	}
}

func TestDiscoveryReturnsCandidatesNotASingleChoice(t *testing.T) {
	// A function that returns THE agent for a request makes every caller of
	// it a dispatch. Discovery returns plural — and there is no singular
	// selection function in the package to reach for.
	rt := reflect.TypeOf((*CardStore).Discover)
	if rt.NumOut() != 1 || rt.Out(0).Kind() != reflect.Struct {
		t.Fatal("Discover answers with one struct of lists")
	}
	cf, ok := rt.Out(0).FieldByName("Candidates")
	if !ok || cf.Type.Kind() != reflect.Slice {
		t.Fatal("Candidates must be a slice — candidates, not a choice")
	}
	// No exported selector shaped like a dispatcher: BestAgent/PickSkill/…
	sel := regexp.MustCompile(`(?m)^func .*\b(Best|Pick|Choose|Select|Match)[A-Z]`)
	if m := sel.FindString(pkgSources(t)); m != "" {
		t.Fatalf("the package exports a chooser — every caller of it is a dispatch: %s", m)
	}
}

// pkgSources concatenates the package's production files for the greps.
func pkgSources(t *testing.T) string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, en := range entries {
		if !strings.HasSuffix(en.Name(), ".go") || strings.HasSuffix(en.Name(), "_test.go") {
			continue
		}
		raw, err := os.ReadFile(en.Name())
		if err != nil {
			t.Fatal(err)
		}
		b.Write(raw)
		b.WriteByte('\n')
	}
	return b.String()
}
