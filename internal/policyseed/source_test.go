package policyseed

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/seed"
)

// TestSourceIsOneRecordPerCorpusRule is the declared-set contract (PM15):
// every corpus rule has a record, keyed by the seed identity S11 minted
// (desk.SeedPolicyID — corpus.<agent>.<hash>, the agent component the
// corpus NAME), at the set's version; no rule is missing, none is doubled,
// and every content decodes at the WRITE BOUNDARY (DecodePolicy is the
// same fail-closed read Add's inputs take — a record the boundary would
// refuse has no business being declared).
func TestSourceIsOneRecordPerCorpusRule(t *testing.T) {
	corpus, rej := policy.ParseCorpus(policy.EmbeddedCorpus())
	if rej != nil {
		t.Fatalf("the committed corpus will not parse: %s", rej.Reason)
	}
	want := 0
	for _, a := range corpus.Agents {
		want += len(a.Rules)
	}
	records := Source()
	if len(records) != want {
		t.Fatalf("the set declares %d records, the corpus carries %d rules", len(records), want)
	}
	seen := map[string]bool{}
	for _, r := range records {
		if seen[r.Key] {
			t.Fatalf("duplicate record key %q", r.Key)
		}
		seen[r.Key] = true
		if r.Version < 1 || r.Version > Declaration().Version {
			t.Fatalf("record %q version %d is outside 1..%d (rule 3)", r.Key, r.Version, Declaration().Version)
		}
		if !regexp.MustCompile(`^corpus\.[^./]+\.[0-9a-f]{12}$`).MatchString(r.Key) {
			t.Fatalf("record key %q is not the seed identity (corpus.<agent>.<12 hex>)", r.Key)
		}
		p, drej := policy.DecodePolicy(r.Content)
		if drej != nil {
			t.Fatalf("record %q does not decode at the boundary: %s", r.Key, drej.Reason)
		}
		if p.ID != r.Key {
			t.Fatalf("record %q carries content for id %q", r.Key, p.ID)
		}
		if p.Type != policy.CommandAccess {
			t.Fatalf("record %q is type %q; the corpus seeds command_access only", r.Key, p.Type)
		}
		if p.Source != policy.Operator {
			t.Fatalf("record %q has source %q; the seed authors as operator (PL1's only allow-authoring source)", r.Key, p.Source)
		}
		if p.NotAfter != nil {
			t.Fatalf("record %q carries an expiry; corpus rules do not expire (PL7: null, never a sentinel)", r.Key)
		}
		if p.Target.Kind != policy.AgentKind || p.Target.ID == "" {
			t.Fatalf("record %q targets %q; every corpus rule targets the agent it is keyed under", r.Key, p.Target)
		}
	}
	// Every corpus rule's key is present (the other half: no silent gaps).
	for _, a := range corpus.Agents {
		for _, r := range a.Rules {
			if !seen[desk.SeedPolicyID(a.Agent, r.Pattern)] {
				t.Fatalf("the corpus rule %s/%q has no record", a.Agent, r.Pattern)
			}
		}
	}
}

// TestContentIsCanonicalAndDecodesToTheBoundarysShape pins the record
// shape itself (not just that it decodes): id, type, target{kind,id},
// action, spec{command,state} with the state the action restated, source
// operator, not_after null — the lowercase wire vocabulary, one canonical
// byte rendering.
func TestContentIsCanonicalAndDecodesToTheBoundarysShape(t *testing.T) {
	corpus, rej := policy.ParseCorpus(policy.EmbeddedCorpus())
	if rej != nil {
		t.Fatalf("the committed corpus will not parse: %s", rej.Reason)
	}
	for _, a := range corpus.Agents {
		for _, r := range a.Rules {
			rec := recordFor(t, desk.SeedPolicyID(a.Agent, r.Pattern))
			var doc map[string]json.RawMessage
			if err := json.Unmarshal(rec.Content, &doc); err != nil {
				t.Fatalf("record %q is not JSON: %v", rec.Key, err)
			}
			for _, k := range []string{"id", "type", "target", "action", "spec", "source", "not_after"} {
				if _, ok := doc[k]; !ok {
					t.Fatalf("record %q lacks the field %q", rec.Key, k)
				}
			}
			if string(doc["not_after"]) != "null" {
				t.Fatalf("record %q has not_after %s; the canonical null states no expiry (PL7)", rec.Key, doc["not_after"])
			}
			var spec struct {
				Command string `json:"command"`
				State   string `json:"state"`
			}
			if err := json.Unmarshal(doc["spec"], &spec); err != nil {
				t.Fatalf("record %q spec: %v", rec.Key, err)
			}
			if spec.Command != r.Pattern {
				t.Fatalf("record %q spec command %q is not the corpus pattern %q", rec.Key, spec.Command, r.Pattern)
			}
			if spec.State != string(r.Verdict) {
				t.Fatalf("record %q spec state %q does not restate the action %q (the boundary's pairing guard)", rec.Key, spec.State, r.Verdict)
			}
			if !json.Valid(rec.Content) || !isCanonical(t, rec.Content) {
				t.Fatalf("record %q is not in canonical form", rec.Key)
			}
		}
	}
}

// TestSourceIsStable is the engine's dependency on determinism: two reads
// of the source agree byte for byte (the golden's hashes are meaningful
// only because of this).
func TestSourceIsStable(t *testing.T) {
	a, b := Source(), Source()
	if len(a) != len(b) {
		t.Fatalf("two reads produced %d and %d records", len(a), len(b))
	}
	for i := range a {
		if a[i].Key != b[i].Key || string(a[i].Content) != string(b[i].Content) || a[i].Version != b[i].Version {
			t.Fatalf("record %d drifted between reads", i)
		}
	}
}

// TestSeedIdentityIsUnchanged is T0's byte fact carried into the engine:
// the key is desk.SeedPolicyID's S11 mint — hash6 of "agent\x00pattern",
// the agent component the corpus name (a re-seed ADOPTS the old verb's
// rows; renaming the identity would orphan them).
func TestSeedIdentityIsUnchanged(t *testing.T) {
	corpus, rej := policy.ParseCorpus(policy.EmbeddedCorpus())
	if rej != nil {
		t.Fatalf("the committed corpus will not parse: %s", rej.Reason)
	}
	if len(corpus.Agents) == 0 || len(corpus.Agents[0].Rules) == 0 {
		t.Fatal("the corpus is empty")
	}
	a := corpus.Agents[0]
	r := a.Rules[0]
	want := desk.SeedPolicyID(a.Agent, r.Pattern)
	rec := recordFor(t, want)
	if rec.Key != want {
		t.Fatalf("no record carries the S11 id %q", want)
	}
}

// TestPolicyForIsTheOldVerbsShape: the policy the engine writes is the
// policy the CLI verb wrote — same id, type, target, action, spec bytes,
// source — the desk builds the id and the spec, and PolicyFor assembles
// the rest.
func TestPolicyForIsTheOldVerbsShape(t *testing.T) {
	corpus, rej := policy.ParseCorpus(policy.EmbeddedCorpus())
	if rej != nil {
		t.Fatalf("the committed corpus will not parse: %s", rej.Reason)
	}
	a := corpus.Agents[0]
	r := a.Rules[0]
	p, err := PolicyFor(a.Agent, r)
	if err != nil {
		t.Fatalf("PolicyFor: %v", err)
	}
	if p.ID != desk.SeedPolicyID(a.Agent, r.Pattern) {
		t.Fatalf("id %q is not the desk mint", p.ID)
	}
	if p.Type != policy.CommandAccess || p.Action != r.Verdict || p.Source != policy.Operator {
		t.Fatalf("shape drifted: %+v", p)
	}
	if p.Target.Kind != policy.AgentKind || p.Target.ID != a.Agent {
		t.Fatalf("target %q is not agent:%s", p.Target, a.Agent)
	}
	want, err := desk.SeedSpecJSON(r)
	if err != nil {
		t.Fatalf("desk spec: %v", err)
	}
	if string(p.Spec) != string(want) {
		t.Fatalf("spec %s is not the desk's bytes %s", p.Spec, want)
	}
}

// --- the versions golden (S43-extra T4's guard, grown for the corpus) ----
//
// Every record's hash is committed. Content changing without the record's
// version bumping is RED (the ledger would keep calling a different thing
// applied); a version bump whose content did not change is RED (a version
// that moves for nothing teaches nobody anything). Regenerate with
// `go test ./internal/policyseed -run TestCorpusVersionsGolden -golden-update`.

var goldenUpdate = flag.Bool("golden-update", false, "rewrite testdata/corpus.versions.golden from the current source")

func TestCorpusVersionsGolden(t *testing.T) {
	const name = "corpus.versions.golden"
	path := filepath.Join("testdata", name)
	var b strings.Builder
	records := Source()
	sort.Slice(records, func(i, j int) bool { return records[i].Key < records[j].Key })
	for _, r := range records {
		fmt.Fprintf(&b, "%s %d %s\n", r.Key, r.Version, seed.Hash(r.Content))
	}
	got := b.String()
	if *goldenUpdate {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("golden update: %v", err)
		}
		t.Logf("wrote %s (%d records)", path, len(records))
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the golden: %v — run the test with -golden-update to create it", err)
	}
	if string(want) != got {
		diff := goldenDiff(string(want), got)
		t.Fatalf("%s does not match the source:\n%s\na content change must be accompanied by bumping that record's version in source.go (and the set's moves with it)", name, diff)
	}
}

func goldenDiff(want, got string) string {
	wl := strings.Split(want, "\n")
	gl := strings.Split(got, "\n")
	var out []string
	for i := 0; i < len(wl) || i < len(gl); i++ {
		var w, g string
		if i < len(wl) {
			w = wl[i]
		}
		if i < len(gl) {
			g = gl[i]
		}
		if w != g {
			out = append(out, fmt.Sprintf("  line %d:\n    golden: %s\n    source: %s", i+1, w, g))
			if len(out) > 20 {
				out = append(out, "  …")
				break
			}
		}
	}
	return strings.Join(out, "\n")
}

func recordFor(t *testing.T, key string) seed.Record {
	t.Helper()
	for _, r := range Source() {
		if r.Key == key {
			return r
		}
	}
	t.Fatalf("no record carries key %q", key)
	return seed.Record{}
}

// TestSetIsAGuardSet is SD5/PM15's declaration, read back: class guard and
// the applier agrees (the registry's rule 6 cross-check would refuse any
// other pairing).
func TestSetIsAGuardSet(t *testing.T) {
	s := Declaration()
	if s.ID != SetID {
		t.Fatalf("the set declares id %q", s.ID)
	}
	if s.Class != seed.ClassGuard {
		t.Fatalf("the set is class %q; the corpus writes allow-rows and must be guard (SD5)", s.Class)
	}
	if s.Records == nil || len(s.Records()) == 0 {
		t.Fatal("the set declares no records")
	}
	if ap := newTestApplier(t); !ap.GuardAffecting() {
		t.Fatal("the applier says it is not guard-affecting; the registry would refuse the pair (rule 6)")
	}
	if _, err := seed.NewRegistry([]seed.Set{s}, []seed.Applier{stubSeedApplier{ap: newTestApplier(t), guard: true}}); err != nil {
		t.Fatalf("the declared pair was refused: %v", err)
	}
}

// TestOperatorRightsAreNoEditRemovable is PM15's pair: a seeded rule is
// not the operator's to edit (divergence, never a correction overwrite),
// and it is theirs to remove (the ledger records removed; only restore
// brings it back).
func TestOperatorRightsAreNoEditRemovable(t *testing.T) {
	op := newTestApplier(t).Operator()
	if op.Edit || !op.Remove {
		t.Fatalf("operator rights %+v; the ruling is Edit:false, Remove:true", op)
	}
}

// newTestApplier builds the applier over a temp store with a fixed clock.
func newTestApplier(t *testing.T) *Applier {
	t.Helper()
	return NewApplier(openStore(t), testBoundary(openStore(t)), func() time.Time {
		return time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	})
}

// isCanonical asks the engine's own canonicaliser whether the bytes are
// already canonical: re-encoding them must be the identity.
func isCanonical(t *testing.T, b []byte) bool {
	t.Helper()
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return false
	}
	c, err := seed.Canonical(v)
	if err != nil {
		return false
	}
	return string(c) == string(b)
}
