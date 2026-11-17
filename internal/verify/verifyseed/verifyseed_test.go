package verifyseed_test

import (
	"bufio"
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/seed"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/verify/verifyseed"
)

var updateGolden = flag.Bool("update", false, "rewrite the versions golden")

// harness twins of blueprintseed_test.go: the store opened fresh, the
// daemon's own engine (every set mounted), apply as the person's door.
func newSeedStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func seedEngine(t *testing.T, s *store.Store) *seed.Engine {
	t.Helper()
	e, err := daemon.NewSeedEngine(s, seed.Options{})
	if err != nil {
		t.Fatalf("seed engine: %v", err)
	}
	return e
}

// applySet applies the set and returns the records the engine wrote
// (reason empty) — the same count the daemon logs at start (seed.go:
// applied/failed walks RecordResult.Reason, so the test counts the same
// bytes the log prints).
func applySet(t *testing.T, e *seed.Engine) []seed.RecordResult {
	t.Helper()
	res, err := e.Apply(context.Background(), verifyseed.SetID, nil, seed.Actor{Kind: "system", ID: "test"}, seed.PathExplicit)
	if err != nil {
		t.Fatalf("apply %s: %v", verifyseed.SetID, err)
	}
	return res.Records
}

// countApplied counts the records the engine WROTE: Op != skip. (The
// engine's RecordResult.Reason carries descriptive text on the apply
// path — "apply — a new record" — so Op is the honest signal, and OpSkip
// is the one the conflict/diverge plans leave behind.)
func countApplied(recs []seed.RecordResult) int {
	n := 0
	for _, r := range recs {
		if r.Op != seed.OpSkip {
			n++
		}
	}
	return n
}

// --- source shape -----------------------------------------------------

func TestSourceHasOneRecordPerShippedTemplate(t *testing.T) {
	recs := verifyseed.Source()
	if len(recs) != 13 {
		t.Fatalf("shipped templates = %d, want 13 (VG21: 4 go + 3 node + 2 python + 3 rust + 1 terraform)", len(recs))
	}
	seen := map[string]bool{}
	slug := regexp.MustCompile(`^[a-z0-9-]+$`)
	for _, r := range recs {
		if seen[r.Key] {
			t.Fatalf("duplicate key %q", r.Key)
		}
		seen[r.Key] = true
		c, err := verifyseed.ContentOf(r)
		if err != nil {
			t.Fatal(err)
		}
		if r.Key != c.Stack+"/"+c.Name {
			t.Errorf("%s: key is not stack/name", r.Key)
		}
		if !slug.MatchString(c.Stack) || len(c.Stack) > 32 {
			t.Errorf("%s: stack %q violates the migration's slug/length rule", r.Key, c.Stack)
		}
		if !slug.MatchString(c.Name) || len(c.Name) > 64 {
			t.Errorf("%s: name %q violates the migration's slug/length rule", r.Key, c.Name)
		}
		if c.Requirement != "required" && c.Requirement != "advisory" {
			t.Errorf("%s: requirement %q off the CHECK", r.Key, c.Requirement)
		}
		if c.TimeoutS < 1 || c.TimeoutS > 600 {
			t.Errorf("%s: timeout %d off the CHECK (1..600)", r.Key, c.TimeoutS)
		}
		if len(c.Command) < 1 || len(c.Command) > 4096 {
			t.Errorf("%s: command length %d off the CHECK", r.Key, len(c.Command))
		}
	}
}

func TestStacksAreTheVG21Set(t *testing.T) {
	want := map[string]int{"go": 4, "node": 3, "python": 2, "rust": 3, "terraform": 1}
	got := map[string]int{}
	for _, r := range verifyseed.Source() {
		c, err := verifyseed.ContentOf(r)
		if err != nil {
			t.Fatal(err)
		}
		got[c.Stack]++
	}
	for stack, n := range want {
		if got[stack] != n {
			t.Errorf("stack %q has %d templates, want %d", stack, got[stack], n)
		}
	}
	for stack := range got {
		if _, ok := want[stack]; !ok {
			t.Errorf("stack %q is not a VG21 stack", stack)
		}
	}
}

// --- the golden ritual (VG21) ------------------------------------------

func TestVerifyTemplateVersionsBumpWithContent(t *testing.T) {
	path := filepath.Join("testdata", "templates.versions.golden")
	recs := verifyseed.Source()
	var lines []string
	for _, r := range recs {
		lines = append(lines, fmt.Sprintf("%s %d %s", r.Key, r.Version, seed.Hash(r.Content)))
	}
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %d lines to %s", len(lines), path)
		return
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the versions golden (run with -update to create it): %v", err)
	}
	want := map[string][2]string{}
	var order []string
	sc := bufio.NewScanner(strings.NewReader(string(b)))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) != 3 {
			t.Fatalf("golden line %q is not `key version hash`", line)
		}
		want[parts[0]] = [2]string{parts[1], parts[2]}
		order = append(order, parts[0])
	}
	got := map[string][2]string{}
	for _, l := range lines {
		parts := strings.Fields(l)
		got[parts[0]] = [2]string{parts[1], parts[2]}
	}
	for _, key := range order {
		g, ok := got[key]
		if !ok {
			t.Fatalf("golden line for %q has no record — a removed template? the golden is rewritten with -update only", key)
		}
		if g[1] != want[key][1] {
			if g[0] == want[key][0] {
				t.Errorf("%s: content changed but version did not — bump it (versions.go ritual), then -update the golden", key)
			} else {
				t.Errorf("%s: version moved %s→%s with the hash — re-run with -update and commit the golden's diff", key, want[key][0], g[0])
			}
		}
	}
	for key := range got {
		if _, ok := want[key]; !ok {
			t.Fatalf("record %q has no golden line — a new template: add its line with -update", key)
		}
	}
}

// --- class and rights ----------------------------------------------------

func TestVerifyTemplatesIsInertAndNotGuardAffecting(t *testing.T) {
	set, ok := seed.DefaultRegistry().Lookup(verifyseed.SetID)
	if !ok {
		t.Fatalf("set %s is not declared at init", verifyseed.SetID)
	}
	if set.Class != seed.ClassInert {
		t.Fatalf("class %q, want inert (VG21: applying writes candidates, widens nothing)", set.Class)
	}
	ap := verifyseed.NewApplier(nil)
	if ap.GuardAffecting() {
		t.Fatal("GuardAffecting true — an inert set may not claim to change what is permitted (registry rule 6)")
	}
	op := ap.Operator()
	if op.Edit || !op.Remove {
		t.Fatalf("operator rights %+v, want {Edit:false, Remove:true} (VG21: copy-and-edit-the-copy, removal honest)", op)
	}
}

// --- engine behaviours (the seed-set tests) ------------------------------

func TestFreshStoreSeedsEverythingWithSystemOrigin(t *testing.T) {
	s := newSeedStore(t)
	e := seedEngine(t, s)
	recs := applySet(t, e)
	if n := countApplied(recs); n != len(verifyseed.Source()) {
		t.Fatalf("applied %d, want %d: %+v", n, len(verifyseed.Source()), recs)
	}
	rows, err := s.ListVerifyTemplates()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != len(verifyseed.Source()) {
		t.Fatalf("live rows %d, want %d", len(rows), len(verifyseed.Source()))
	}
	for _, row := range rows {
		if row.Origin != "system" {
			t.Errorf("%s: origin %q, want system (VG21)", row.ID, row.Origin)
		}
	}
}

func TestSeedIsIdempotent(t *testing.T) {
	s := newSeedStore(t)
	e := seedEngine(t, s)
	applySet(t, e)
	first, err := s.ListVerifyTemplates()
	if err != nil {
		t.Fatal(err)
	}
	recs := applySet(t, e)
	if n := countApplied(recs); n != 0 {
		t.Fatalf("second apply wrote %d records — the fingerprint says unchanged for unchanged rows", n)
	}
	second, err := s.ListVerifyTemplates()
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != len(first) {
		t.Fatalf("row count moved %d → %d", len(first), len(second))
	}
}

// TestDivergedTemplateIsNeverOverwritten is "divergence never
// overwritten": a row whose content moved away from the shipped record
// stays the operator's — the engine reports the conflict and skips it
// (SD9; Adoptable false is what makes a live row nobody wrote a conflict,
// never a row to fill in place).
func TestDivergedTemplateIsNeverOverwritten(t *testing.T) {
	s := newSeedStore(t)
	e := seedEngine(t, s)
	applySet(t, e)
	// The database's own hand, simulated through the one write path with
	// different bytes (the blueprint test's method — no raw SQL, no door
	// that isn't the store's).
	if err := s.VerifyWithTx(context.Background(), func(tx *sql.Tx) error {
		return store.VerifyTemplateUpsertTx(tx, store.VerifyTemplate{
			ID: "go/build", Stack: "go", Name: "build", Command: "go build .",
			Requirement: "required", TimeoutS: 600, Version: "v1",
		})
	}); err != nil {
		t.Fatal(err)
	}
	recs := applySet(t, e)
	var wrote bool
	for _, r := range recs {
		if r.Key == "go/build" && r.Op != seed.OpSkip {
			wrote = true
		}
	}
	if wrote {
		t.Fatal("the seed re-wrote a diverged row (SD9/Adoptable-false broken)")
	}
	var cmd string
	if err := s.VerifyWithTx(context.Background(), func(tx *sql.Tx) error {
		return tx.QueryRow(`SELECT command FROM verify_template WHERE id = 'go/build'`).Scan(&cmd)
	}); err != nil {
		t.Fatal(err)
	}
	if cmd != "go build ." {
		t.Fatalf("the seed overwrote an operator's row: %q", cmd)
	}
}

// TestRetirementNeverResurrects is VG21's "removal never resurrected":
// a retired (archived) template row stays archived through a re-seed —
// the person's restore door is the only un-retire (T8's daemon test
// covers that door; this covers the seed never opening it).
func TestRetirementNeverResurrects(t *testing.T) {
	s := newSeedStore(t)
	e := seedEngine(t, s)
	applySet(t, e)
	if err := s.VerifyWithTx(context.Background(), func(tx *sql.Tx) error {
		_, err := s.VerifyTemplateRetireTx(context.Background(), tx, "rust/lint")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := s.ListVerifyTemplates()
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.ID == "rust/lint" {
			t.Fatal("ListVerifyTemplates still offers a retired template")
		}
	}
	applySet(t, e)
	var archived int
	if err := s.VerifyWithTx(context.Background(), func(tx *sql.Tx) error {
		return tx.QueryRow(`SELECT count(*) FROM verify_template WHERE id='rust/lint' AND archived_at IS NOT NULL`).Scan(&archived)
	}); err != nil {
		t.Fatal(err)
	}
	if archived != 1 {
		t.Fatal("a re-seed resurrected a retired template (VG21)")
	}
}

// TestNoMergeRuleIsSeeded / TestNoVerifyCommandIsSeeded — VG21 states the
// exemption: a default merge_rule is guard-affecting either way (loosen
// or tighten) and verify_command rows are per-component
// execution-shaping (S40 AU1 lock 3, the trigger precedent). No shipped
// set is named for either.
func TestNoMergeRuleIsSeeded(t *testing.T) {
	for _, set := range seed.DefaultRegistry().All() {
		if strings.Contains(set.ID, "merge") || strings.Contains(set.ID, "rule") {
			t.Fatalf("set %q must not exist: merge_rule is exempt from seeding (VG21)", set.ID)
		}
	}
}

func TestNoVerifyCommandIsSeeded(t *testing.T) {
	for _, set := range seed.DefaultRegistry().All() {
		if strings.Contains(set.ID, "verify_command") || strings.Contains(set.ID, "gate") {
			t.Fatalf("set %q must not exist: verify_command rows are per-component, never seeded (VG21)", set.ID)
		}
	}
}

// TestSeedStatusListsTheSet — the engine's status shows the set as inert
// with its version (the `aeon seed status` row; T3's read).
func TestSeedStatusListsTheSet(t *testing.T) {
	s := newSeedStore(t)
	e := seedEngine(t, s)
	statuses, err := e.Status(context.Background(), verifyseed.SetID)
	if err != nil {
		t.Fatal(err)
	}
	var st *seed.SetStatus
	for i := range statuses {
		if statuses[i].ID == verifyseed.SetID {
			st = &statuses[i]
		}
	}
	if st == nil {
		t.Fatalf("status does not list %s: %+v", verifyseed.SetID, statuses)
	}
	if st.Class != seed.ClassInert {
		t.Fatalf("class %q, want inert", st.Class)
	}
	if st.AvailableVersion != verifyseed.SetVersion() {
		t.Fatalf("available %d, want %d", st.AvailableVersion, verifyseed.SetVersion())
	}
}

// --- provenance (VG21: extracted, not invented) ---------------------------

// provenance maps each shipped command to the shipped agent body (under
// opencode/agents) that runs it. One entry is a design-pinned COMPOSITION
// and lists its fragments with their files: go/test composes the body
// doctrine `timeout 300 go test ./...` (golang-engineer-loop.md:60), the
// run-once `-count=1` (test-automator.md), and the design's pinned
// `-timeout 300s` (T10.md:29) — every shipped command's parts, or the
// command itself, must stand in a shipped body.
type prov struct {
	File      string
	Fragments []string // all must appear; empty means the command verbatim
}

var provenance = map[string][]prov{
	"go/build":           {{File: "verifier-engineer-loop.md"}},
	"go/lint":            {{File: "golang-engineer-loop.md"}},
	"go/test":            {{File: "golang-engineer-loop.md", Fragments: []string{"timeout 300 go test ./..."}}, {File: "test-automator.md", Fragments: []string{"-count=1"}}},
	"go/vet":             {{File: "go-architect.md"}},
	"node/build":         {{File: "react-typescript-engineer-loop.md"}},
	"node/lint":          {{File: "react-typescript-engineer-loop.md"}},
	"node/test":          {{File: "javascript-engineer-loop.md"}},
	"python/lint":        {{File: "python-engineer-loop.md"}},
	"python/test":        {{File: "python-engineer-loop.md"}},
	"rust/build":         {{File: "rust-engineer-loop.md"}},
	"rust/lint":          {{File: "rust-engineer-loop.md"}},
	"rust/test":          {{File: "rust-engineer-loop.md"}},
	"terraform/validate": {{File: "terraform-engineer-loop.md"}},
}

func TestCommandsAreExtractedFromTheHandlerBodies(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "..", "opencode", "agents")
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("shipped agent bodies not present at %s (extracted repo?) — provenance walks from the loop", dir)
	}
	for _, r := range verifyseed.Source() {
		c, err := verifyseed.ContentOf(r)
		if err != nil {
			t.Fatal(err)
		}
		prov, ok := provenance[r.Key]
		if !ok {
			t.Fatalf("%s: no provenance entry — a shipped command must name the body it came from (VG21: extracted, not invented)", r.Key)
		}
		for _, p := range prov {
			raw, err := os.ReadFile(filepath.Join(dir, p.File))
			if err != nil {
				t.Fatalf("%s: provenance body %s unreadable: %v", r.Key, p.File, err)
			}
			body := string(raw)
			if len(p.Fragments) == 0 {
				if !strings.Contains(body, c.Command) {
					t.Errorf("%s: command %q is not the verbatim command of %s — VG21 forbids invented gates", r.Key, c.Command, p.File)
				}
				continue
			}
			for _, frag := range p.Fragments {
				if !strings.Contains(body, frag) {
					t.Errorf("%s: fragment %q not in %s — the composition lost a body it was extracted from", r.Key, frag, p.File)
				}
			}
		}
	}
}

// TestTerraformPlanIsOmitted — VG21 states it and T10.md:29 states it:
// `terraform plan` needs credentials (S59 EV) and is not shipped.
func TestTerraformPlanIsOmitted(t *testing.T) {
	for _, r := range verifyseed.Source() {
		c, _ := verifyseed.ContentOf(r)
		if c.Stack == "terraform" && strings.Contains(c.Command, "terraform plan") {
			t.Fatal("terraform/plan must not ship — it needs credentials (S59 EV)")
		}
	}
}

// TestTemplatesPassTheCorpusPolicy — VG21's gate on the shipped set:
// every command must be one the verifier identity may run under the
// SHIPPED corpus, with no policy edit. The corpus names OD3's verifier
// identity `verifier-engineer-loop`; the fold is PL4 — a matching deny
// outranks a matching allow, and no match at all denies (PL5). A command
// that would be refused is named in the failure — the seed omits it
// rather than shipping a gate that can never run.
func TestTemplatesPassTheCorpusPolicy(t *testing.T) {
	corpus, rej := policy.ParseCorpus(policy.EmbeddedCorpus())
	if rej != nil {
		t.Fatalf("the shipped corpus does not parse: %v", rej)
	}
	rules, ok := corpus.RulesFor("verifier-engineer-loop")
	if !ok {
		t.Fatal("the corpus has no verifier-engineer-loop entry — the seed has no identity to check against")
	}
	var refused []string
	for _, r := range verifyseed.Source() {
		c, err := verifyseed.ContentOf(r)
		if err != nil {
			t.Fatal(err)
		}
		allowed := false
		for _, rule := range rules {
			if !policy.MatchPattern(rule.Pattern, c.Command) {
				continue
			}
			switch rule.Verdict {
			case policy.Deny:
				refused = append(refused, fmt.Sprintf("%s (%q denies)", r.Key, rule.Pattern))
			case policy.Allow:
				allowed = true
			}
		}
		if !allowed {
			refused = append(refused, r.Key+" (no rule allows it — PL5 empty-set denial)")
		}
	}
	if len(refused) > 0 {
		t.Fatalf("these shipped templates would not run for the verifier (the seed must omit them):\n  %s", strings.Join(refused, "\n  "))
	}
}
