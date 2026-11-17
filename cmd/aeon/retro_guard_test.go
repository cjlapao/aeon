package main

// retro_guard_test.go — S63 T5: the outcome-learning plane's guards.
// RT3's whole promise is structural, so every claim is a walk or a
// grep over the REAL tree, each with a canary of fake input proving the
// predicate has teeth:
//
//   - internal/scorecard is imported ONLY by its named readers (RT3.1):
//     no dispatch, policy, runtime, tools or engine package can even
//     NAME a scorecard type, so "a score never reaches dispatch" is a
//     compile-time fact, not a review-time one;
//   - the recorder's write side (internal/retro) and the plane's read
//     doors import only what the design names — store, learnings, the
//     cost enums, history's renderer, protocol;
//   - every plane door is `read` (RT3.3) and retro.backfill alone is
//     the person's door (RT13), absent from the four caller-classes
//     that are not the person;
//   - nothing in the plane WRITES a setting, a grant, a budget, a
//     priority — a function reading a card cannot apply one;
//   - RT3's sentence exists exactly once in the non-test tree.

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/narrow"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/records"
	"github.com/cjlapao/aeon/internal/tools"
)

// --- the plane's names ----------------------------------------------------------

var (
	retroPlaneReads = []protocol.MethodName{
		protocol.MethodScorecardAgent, protocol.MethodScorecardBlueprint,
		protocol.MethodScorecardComponent, protocol.MethodScorecardCompare,
		protocol.MethodScorecardVocabulary,
		protocol.MethodRetroList, protocol.MethodRetroShow, protocol.MethodRetroStatus,
		protocol.MethodDigestLearned,
	}
	retroPlaneAll = append(append([]protocol.MethodName{}, retroPlaneReads...), protocol.MethodRetroBackfill)
)

// scorecardImportAllow names, repo-relative, every NON-TEST file allowed
// to import internal/scorecard (RT3.1's list, measured at T5). The CLI
// verbs join with T7 — in this same slice, before close-out.
var scorecardImportAllow = map[string]bool{
	"internal/daemon/scorecard_reads.go": true, // the person's doors
	"internal/daemon/digest_reads.go":    true, // the digest section assembly
}

var engineDirs = []string{
	"internal/loop", "internal/runtime", "internal/policy", "internal/tools",
	"internal/handler", "internal/model", "internal/automation", "internal/queue",
	"internal/roadmap", "internal/triage", "internal/trigger", "internal/narrow",
	"internal/bootstrap",
}

// --- the predicates (pure; the Teeth test feeds each a fake offender) ------------

// scorecardImportViolations: walked maps repo-file → import-path-set for
// NON-TEST files; offenders are files importing `pkg` outside allow, or
// any file under a forbidden engineDir importing it.
func scorecardImportViolations(walked map[string]map[string]bool, pkg string, allow map[string]bool, forbiddenDirs []string) []string {
	var bad []string
	suffix := "/" + pkg
	for file, imports := range walked {
		hit := false
		for imp := range imports {
			if strings.HasSuffix(imp, suffix) {
				hit = true
			}
		}
		if !hit {
			continue
		}
		if allow[file] {
			continue
		}
		bad = append(bad, file+" imports "+pkg+" outside the allow-list")
		for _, d := range forbiddenDirs {
			if strings.HasPrefix(file, d+"/") {
				bad = append(bad, file+" is a dispatch/engine file — RT3.1 forbids "+d+" to name a scorecard type at all")
			}
		}
	}
	return bad
}

// importSetViolations: every aeon import of a narrow file must be in allow.
func importSetViolations(file string, imports map[string]bool, allow map[string]bool) []string {
	var bad []string
	for imp := range imports {
		if !strings.Contains(imp, "/internal/") {
			continue
		}
		if !allow[imp] {
			bad = append(bad, file+" imports "+imp+" — outside the narrow write-side list")
		}
	}
	return bad
}

// mutatesViolation: RT3.3 — the plane reads. kind is a surface kind
// word; mutates a records-spec flag. backfill is the one write and only
// backfill may wear it.
func mutatesViolation(method, kind string, humanDoorOnly bool) []string {
	var bad []string
	if method == string(protocol.MethodRetroBackfill) {
		if kind != "write" {
			bad = append(bad, method+" is classified "+kind+"; the plane's one write must be `write`")
		}
		if !humanDoorOnly {
			bad = append(bad, method+" is not HumanDoorOnly — RT13's write has a second door")
		}
		return bad
	}
	if kind != "read" {
		bad = append(bad, method+" is classified "+kind+"; RT3.3 keeps every scorecard, digest and retro read a read")
	}
	if humanDoorOnly {
		bad = append(bad, method+" is HumanDoorOnly — a read's absence from the agent doors is its own fact, not a door mark")
	}
	return bad
}

// writeWordViolations: a plane file that names an applying write is the
// plane applying a score. The word set is RT3's list of things a score
// must never touch.
var applyingWords = []string{
	"SetSetting", "WriteSetting", "settings.set", "CreateGrant", "SaveGrant",
	"RaiseBudget", "SetBudget", "queue_advance", "max_parallel_agents",
	"SetPriority", "UpdatePriority", "roadmap.dispatch",
}

func writeWordViolations(file, body string) []string {
	var bad []string
	for _, w := range applyingWords {
		if strings.Contains(body, w) {
			bad = append(bad, file+" names "+w+" — a score is a measurement; the plane does not apply it (RT3)")
		}
	}
	return bad
}

// --- the walkers ----------------------------------------------------------------

// walkAeonImports reads every non-test .go under root and returns
// REPO-RELATIVE file → set of aeon import paths, from the file's import
// block only. skipDirs names directory basenames to skip whole (the
// recorder's guard skips its own seed subpackage, which the seed
// engine's guards own).
func walkAeonImports(t *testing.T, root string, skipDirs ...string) map[string]map[string]bool {
	t.Helper()
	base := filepath.Dir(root)
	skip := map[string]bool{}
	for _, d := range skipDirs {
		skip[d] = true
	}
	out := map[string]map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && skip[d.Name()] {
			return filepath.SkipDir
		}
		if d.IsDir() && (d.Name() == "conformance" || d.Name() == "ui") {
			return filepath.SkipDir
		}
		if d.IsDir() && strings.HasSuffix(path, "testdata") {
			return filepath.SkipDir
		}
		if d.IsDir() && (d.Name() == "token_forge_test" || d.Name() == "nettest" || d.Name() == "envtest") {
			return filepath.SkipDir
		}
		if d.IsDir() && len(d.Name()) > 0 && d.Name()[0] == '.' {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		imports := map[string]bool{}
		inBlock := false
		for _, line := range strings.Split(string(raw), "\n") {
			t := strings.TrimSpace(line)
			if !inBlock {
				if strings.HasPrefix(t, "import \"github.com/cjlapao/aeon") {
					imports[strings.Trim(strings.TrimPrefix(t, "import "), "\"")] = true
				}
				if t == "import (" {
					inBlock = true
				}
				continue
			}
			if t == ")" {
				inBlock = false
				continue
			}
			q := strings.Trim(t, "\"")
			if strings.HasPrefix(q, "github.com/cjlapao/aeon") {
				imports[q] = true
			} else if fields := strings.Fields(t); len(fields) == 2 && fields[0] != "//" {
				if strings.HasPrefix(fields[1], "\"github.com/cjlapao/aeon") {
					imports[strings.Trim(fields[1], "\"")] = true
				}
			}
		}
		rel, rerr := filepath.Rel(base, path)
		if rerr != nil {
			rel = path
		}
		out[filepath.ToSlash(rel)] = imports
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return out
}

// --- the guards --------------------------------------------------------------------

func TestScorecardNeverReachedByDispatch(t *testing.T) {
	root := repoRoot(t)
	walked := walkAeonImports(t, filepath.Join(root, "internal"))
	if len(walked) < 50 {
		t.Fatalf("the walk covered %d files, want at least 50 — a walk that covers nothing proves nothing", len(walked))
	}
	if bad := scorecardImportViolations(walked, "internal/scorecard", scorecardImportAllow, engineDirs); len(bad) > 0 {
		for _, b := range bad {
			t.Error(b)
		}
	}
	// The digest's and the cards' OWN read doors stay narrow too: the
	// daemon-side files RT3.1 names, importing only what the design names.
	scAllow := map[string]bool{
		"github.com/cjlapao/aeon/internal/scorecard": true,
		"github.com/cjlapao/aeon/internal/store":     true,
		"github.com/cjlapao/aeon/internal/protocol":  true,
		"github.com/cjlapao/aeon/internal/history":   true, // the ONE renderer (rule 4)
	}
	for _, f := range []string{"internal/daemon/scorecard_reads.go", "internal/daemon/digest_reads.go", "internal/daemon/retro_reads.go"} {
		imports, ok := walked[f]
		if !ok {
			t.Fatalf("%s did not appear in the walk — the plane's door file is gone", f)
		}
		for _, b := range importSetViolations(f, imports, scAllow) {
			t.Error(b)
		}
	}
}

func TestRetroWriteSideImportsAreNarrow(t *testing.T) {
	root := repoRoot(t)
	walked := walkAeonImports(t, filepath.Join(root, "internal", "retro"), "seed", "testdata")
	if len(walked) < 2 {
		t.Fatalf("the walk covered %d files under internal/retro — the recorder's home is missing", len(walked))
	}
	// store (the one writer's door), learnings (the milestone write), and
	// model's COST ENUM WORDS only — measured at T2: internal/retro uses
	// model.CostKnown/Partial, vocabulary, not the model layer's machinery.
	allow := map[string]bool{
		"github.com/cjlapao/aeon/internal/store":     true,
		"github.com/cjlapao/aeon/internal/learnings": true,
		"github.com/cjlapao/aeon/internal/model":     true,
	}
	for file, imports := range walked {
		for _, b := range importSetViolations(file, imports, allow) {
			t.Error(b)
		}
	}
}

func TestScorecardMethodsAreReadsOnly(t *testing.T) {
	for _, m := range retroPlaneAll {
		s, ok := protocol.SurfaceClassified(m)
		if !ok {
			t.Fatalf("%s is unclassified — every method carries exactly one classification (TC6)", m)
		}
		for _, b := range mutatesViolation(string(m), string(s.Kind), s.HumanDoorOnly) {
			t.Error(b)
		}
	}
}

// retroTableStub wires the plane so the live table can be inspected.
type retroTableStub struct{}

func (retroTableStub) Agent(context.Context, protocol.ScorecardAgentParams) (protocol.ScorecardCardResult, error) {
	return protocol.ScorecardCardResult{}, nil
}
func (retroTableStub) Blueprint(context.Context, protocol.ScorecardBlueprintParams) (protocol.ScorecardCardResult, error) {
	return protocol.ScorecardCardResult{}, nil
}
func (retroTableStub) Component(context.Context, protocol.ScorecardComponentParams) (protocol.ScorecardComponentResult, error) {
	return protocol.ScorecardComponentResult{}, nil
}
func (retroTableStub) Compare(context.Context, protocol.ScorecardCompareParams) (protocol.ScorecardCompareResult, error) {
	return protocol.ScorecardCompareResult{}, nil
}
func (retroTableStub) Vocabulary(context.Context) (protocol.ScorecardVocabularyResult, error) {
	return protocol.ScorecardVocabularyResult{}, nil
}
func (retroTableStub) List(context.Context, protocol.RetroListParams) (protocol.RetroListResult, error) {
	return protocol.RetroListResult{}, nil
}
func (retroTableStub) Show(context.Context, protocol.RetroShowParams) (protocol.RetroShowResult, error) {
	return protocol.RetroShowResult{}, nil
}
func (retroTableStub) Status(context.Context) (protocol.RetroStatusResult, error) {
	return protocol.RetroStatusResult{}, nil
}
func (retroTableStub) Backfill(context.Context, protocol.Actor, protocol.RetroBackfillParams) (protocol.RetroBackfillWire, error) {
	return protocol.RetroBackfillWire{}, nil
}
func (retroTableStub) Learned(context.Context, protocol.DigestParams) (protocol.DigestResult, error) {
	return protocol.DigestResult{}, nil
}

func TestBackfillIsAHumanDoor(t *testing.T) {
	tbl := protocol.NewTable(protocol.NewSessions(), protocol.NewSubscriptions(), protocol.NewEventLog())
	tbl.Scorecard = retroTableStub{}
	tbl.Retro = retroTableStub{}
	tbl.Digest = retroTableStub{}
	if !tbl.RegisterScorecard() || !tbl.RegisterRetro() || !tbl.RegisterDigest() {
		t.Fatal("the plane did not register over a wired seam")
	}
	for _, m := range retroPlaneAll {
		got, ok := tbl.Lookup(m)
		if !ok {
			t.Fatalf("%s is not registered over a wired seam — the app has no door (P34)", m)
		}
		if !got.RequiresInitialize {
			t.Errorf("%s answers before the handshake", m)
		}
		if got.Debug {
			t.Errorf("%s is Debug — a debug method is operational control by another name (R6)", m)
		}
	}

	// The four caller-classes that are not the person, one negative each.
	for _, m := range retroPlaneAll {
		for _, a := range narrow.Actions() {
			if a.SocketMethod == m {
				t.Errorf("action %q projects %q — the outcome-learning plane has no non-person door", a.Name, m)
			}
		}
		for _, asm := range protocol.AgentServiceMethods() {
			if asm.Name == m {
				t.Errorf("%s is an agent-service method — an agent reading the scorecard plane with no person in the room", m)
			}
		}
	}
	// S63 T6 rewrote the two predicates below the way T5 wrote them FOR:
	// "until T6 grants named reads" arrived, and the grant is EXACTLY
	// three. A fourth plane-named tool or spec is drift, not design.
	t6GrantedReads := map[string]bool{
		"scorecard_mine":      true,
		"scorecard_component": true,
		"digest_learned":      true,
	}
	planeName := regexp.MustCompile(`(?i)scorecard|digest_learned|retro_`)
	for name := range tools.Registry() {
		if planeName.MatchString(name) && !t6GrantedReads[name] {
			t.Errorf("tool %q wears an outcome-learning plane name — T6 grants EXACTLY the three named reads and this is a fourth (RT10, RT11)", name)
		}
	}
	for _, spec := range records.Catalog() {
		planeTwin := strings.HasPrefix(spec.Method, "scorecard.") || strings.HasPrefix(spec.Method, "retro.") || spec.Method == string(protocol.MethodDigestLearned)
		if !planeTwin {
			continue
		}
		// T6's three twins are the WHOLE allow-list of plane names in the
		// catalog; anything else pointing at the plane is drift, and the
		// family carries no write to hide behind (RT11).
		if !t6GrantedReads[spec.Name] {
			t.Errorf("catalog spec %q twines to %q — only T6's three named reads ever twin to the plane (RT10)", spec.Name, spec.Method)
			continue
		}
		if spec.Mutates {
			t.Errorf("%s twines to a plane read and Mutates — NO WRITE EXISTS IN THIS FAMILY (RT11, OD10)", spec.Name)
		}
	}

	// The literal walk: the door names do not even APPEAR as strings in
	// the four non-person packages' non-test sources. S63 T6's exemption
	// is ONE file and THREE names: records.Catalog is the ledger of twins
	// and must write its specs' dot-names down (the S59 one-file exemption
	// shape, narrowed to exactly the methods T6 granted). The retro names
	// and the other six plane names keep their zero everywhere — the
	// person's doors have no reason to be spelled in an agent package.
	t6TwinLiterals := map[protocol.MethodName]bool{
		protocol.MethodScorecardAgent:     true,
		protocol.MethodScorecardComponent: true,
		protocol.MethodDigestLearned:      true,
	}
	for _, pkg := range []string{"internal/narrow", "internal/tools", "internal/records", "internal/agentsvc"} {
		dir := filepath.Join(repoRoot(t), pkg)
		_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil //nolint // a missing pkg dir proves nothing; the live negatives above carry it
			}
			exemptCatalog := pkg == "internal/records" && filepath.Base(path) == "catalog.go"
			raw, rerr := os.ReadFile(path)
			if rerr != nil {
				return nil //nolint
			}
			for _, m := range retroPlaneAll {
				if exemptCatalog && t6TwinLiterals[m] {
					continue
				}
				if strings.Contains(string(raw), `"`+string(m)+`"`) {
					t.Errorf("%s names %q — the plane's method name must not live in a non-person door even as a string", filepath.Base(path), m)
				}
			}
			return nil
		})
	}
}

func TestNoSettingOrGrantIsDerivedFromAScore(t *testing.T) {
	root := repoRoot(t)
	planeFiles := []string{
		"internal/daemon/scorecard_reads.go", "internal/daemon/digest_reads.go",
		"internal/daemon/retro_reads.go",
	}
	err := filepath.WalkDir(filepath.Join(root, "internal", "scorecard"), func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			rel, rerr := filepath.Rel(root, path)
			if rerr == nil {
				planeFiles = append(planeFiles, filepath.ToSlash(rel))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range planeFiles {
		raw, rerr := os.ReadFile(filepath.Join(root, f))
		if rerr != nil {
			t.Fatalf("read %s: %v", f, rerr)
		}
		for _, b := range writeWordViolations(f, string(raw)) {
			t.Error(b)
		}
	}
}

func TestRetroGuardsHaveTeeth(t *testing.T) {
	t.Run("import walk", func(t *testing.T) {
		clean := map[string]map[string]bool{"internal/daemon/scorecard_reads.go": {"github.com/cjlapao/aeon/internal/scorecard": true}}
		if bad := scorecardImportViolations(clean, "internal/scorecard", scorecardImportAllow, engineDirs); len(bad) != 0 {
			t.Fatalf("the predicate flagged the allowed file: %v", bad)
		}
		fake := map[string]map[string]bool{
			"internal/loop/sneak.go":     {"github.com/cjlapao/aeon/internal/scorecard": true},
			"internal/policy/quiet.go":   {"github.com/cjlapao/aeon/internal/scorecard": true},
			"internal/scorecard/self.go": {},
		}
		bad := scorecardImportViolations(fake, "internal/scorecard", scorecardImportAllow, engineDirs)
		if len(bad) != 4 { // both offenders, each named twice: allow-list breach + engine-dir breach
			t.Fatalf("the canary walked unreported (got %v)", bad)
		}
		if len(scorecardImportViolations(fake, "internal/scorecard", nil, nil)) != 2 {
			t.Error("with no dirs named, each offender should be flagged once")
		}
	})
	t.Run("narrow imports", func(t *testing.T) {
		allow := map[string]bool{"github.com/cjlapao/aeon/internal/store": true}
		if bad := importSetViolations("x.go", map[string]bool{"github.com/cjlapao/aeon/internal/store": true}, allow); len(bad) != 0 {
			t.Fatalf("clean imports flagged: %v", bad)
		}
		bad := importSetViolations("x.go", map[string]bool{"github.com/cjlapao/aeon/internal/policy": true}, allow)
		if len(bad) != 1 {
			t.Fatalf("the canary import walked unreported (got %v)", bad)
		}
	})
	t.Run("reads-only", func(t *testing.T) {
		if bad := mutatesViolation("scorecard.agent", "read", false); len(bad) != 0 {
			t.Fatalf("the honest classification was flagged: %v", bad)
		}
		if bad := mutatesViolation("scorecard.agent", "write", false); len(bad) == 0 {
			t.Error("a fake scorecard METHOD registered write walked free")
		}
		if bad := mutatesViolation("retro.backfill", "read", false); len(bad) == 0 {
			t.Error("backfill classified read walked free — RT13's write mislabeled is the planest worst shape")
		}
	})
	t.Run("applying words", func(t *testing.T) {
		if bad := writeWordViolations("x.go", "func card(c scorecard.Card) { return }"); len(bad) != 0 {
			t.Fatalf("a pure reader flagged: %v", bad)
		}
		if bad := writeWordViolations("x.go", "func apply(c scorecard.Card) { d.SetSetting(ctx, \"roadmap.priority\", 1) }"); len(bad) == 0 {
			t.Error("a fake applying function walked free — the guard is vacuous")
		}
	})
	t.Run("narrow projection", func(t *testing.T) {
		// The live narrow.Actions() answer zero hits today; the canary
		// proves the LOOP's shape would catch a planted projection.
		found := false
		for _, a := range narrow.Actions() {
			if a.SocketMethod == protocol.MethodRetroBackfill {
				found = true
			}
		}
		if found {
			t.Fatal("retro.backfill IS projected today — the live guard's own failure")
		}
		// fake action: the name a projection would wear.
		fakeAction := "retro_backfill"
		if !strings.Contains(fakeAction, "retro") {
			t.Fatal("the canary name does not carry the plane word — the shape-check is broken")
		}
	})
}

// TestEveryScorecardMethodHasAFixture: the plane's ten doors all appear
// in the conformance contract; every fixture is replayed by
// TestOutcomeLearningConformanceFixturesMatchTheWire in the protocol
// package — this guard pins COVERAGE, that one pins BEHAVIOUR.
func TestEveryScorecardMethodHasAFixture(t *testing.T) {
	dir := filepath.Join(repoRoot(t), "internal", "protocol", "conformance")
	seen := map[protocol.MethodName]int{}
	dirs := 0
	for _, plane := range []string{"scorecard", "retro", "digest"} {
		entries, err := os.ReadDir(filepath.Join(dir, plane))
		if err != nil {
			t.Fatalf("conformance/%s missing — the plane's wire contract is its fixtures: %v", plane, err)
		}
		dirs++
		n := 0
		for _, en := range entries {
			if !strings.HasSuffix(en.Name(), ".json") {
				continue
			}
			n++
			raw, err := os.ReadFile(filepath.Join(dir, plane, en.Name()))
			if err != nil {
				t.Fatal(err)
			}
			var f struct {
				Method protocol.MethodName `json:"method"`
			}
			if err := json.Unmarshal(raw, &f); err != nil {
				t.Fatalf("%s/%s: %v", plane, en.Name(), err)
			}
			seen[f.Method]++
		}
		if n < 2 {
			t.Errorf("conformance/%s holds %d fixtures, want at least 2", plane, n)
		}
	}
	if dirs != 3 {
		t.Fatal("the plane's three conformance families are the contract; one is gone")
	}
	for _, m := range retroPlaneAll {
		if seen[m] == 0 {
			t.Errorf("%s has no conformance fixture — the door exists but the contract does not", m)
		}
	}
	for m := range seen {
		known := false
		for _, p := range retroPlaneAll {
			if p == m {
				known = true
			}
		}
		if !known {
			t.Errorf("fixture method %q is not a plane method — the contract names a door that does not exist", m)
		}
	}
}

// TestRT3SentenceExistsOnce: the sentence lives in ONE source of truth.
// A second copy is a second rule, and this grep is what keeps the plane
// quoting its constant instead of retyping the promise.
func TestRT3SentenceExistsOnce(t *testing.T) {
	const sentence = "A score is a measurement of what happened. It does not change what any agent, blueprint or project is allowed or asked to do."
	root := repoRoot(t)
	var hits []string
	_ = filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil //nolint
		}
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil //nolint
		}
		if strings.Contains(string(raw), sentence) {
			rel, _ := filepath.Rel(root, path)
			hits = append(hits, filepath.ToSlash(rel))
		}
		return nil
	})
	if len(hits) != 1 || hits[0] != "internal/protocol/scorecard.go" {
		t.Errorf("RT3's sentence lives in %v — it must be ONE constant's declaration, nowhere else", hits)
	}
}

// TestNewToolsNotInDefaultGrant — S63 T6 (RT11, RT3): the three scorecard
// tools STAND IN THE REGISTRY (they are real doors) and are ABSENT from
// DefaultGrant ({"read","list","write"} unchanged). A score is granted by
// the person wiring the grant, never given by default: the day a scorecard
// tool ships enabled-for-all, "a measurement, not an instruction" becomes
// a caption on an instruction.
func TestNewToolsNotInDefaultGrant(t *testing.T) {
	grant := map[string]bool{}
	for _, name := range tools.DefaultGrant() {
		grant[name] = true
	}
	reg := tools.Registry()
	for _, name := range []string{"scorecard_mine", "scorecard_component", "digest_learned"} {
		if grant[name] {
			t.Errorf("%s is in DefaultGrant — the score is never a default grant (RT11)", name)
		}
		if _, ok := reg[name]; !ok {
			t.Errorf("%s is not in the registry — the tool would be undiscoverable, twin and all", name)
		}
	}
}

// TestNoHandlerBodyMentionsScorecards — S63 T6 (RT11's no-loop law): the
// agent bodies under internal/handler/bodies are what an agent IS, and not
// one of them may mention the scorecard. The moment a body says "check
// your score", the measurement has become a self-improvement instruction
// — the loop RT11 forbids, installed by prose instead of by code.
func TestNoHandlerBodyMentionsScorecards(t *testing.T) {
	root := repoRoot(t)
	hits := []string{}
	_ = filepath.WalkDir(filepath.Join(root, "internal", "handler", "bodies"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil //nolint
		}
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil //nolint
		}
		if strings.Contains(strings.ToLower(string(raw)), "scorecard") {
			rel, _ := filepath.Rel(root, path)
			hits = append(hits, filepath.ToSlash(rel))
		}
		return nil
	})
	if len(hits) != 0 {
		t.Errorf("agent bodies mention the scorecard %v — a score never enters what an agent IS or is asked to do (RT3, RT11)", hits)
	}
}
