package agentsvc

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/handler"
	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/store"
)

// T11 — AS1 as a test rather than a promise (AS11): "an agent with no card,
// no skills and no tools is markdown and a prompt — and the ladder runs on
// it unchanged." S22 earned this discipline for retrieval, S12 reused it for
// MCP (internal/mcp/absent_test.go is the pattern), and this is the third
// exercise of it. Absent means ABSENT: no feature flag (scope 4 — a flag is
// a second state to test and a thing that gets set wrong), just a machine
// where nobody wrote the cards.

const agentsvcImportPath = `"github.com/cjlapao/aeon/internal/agentsvc"`

// absentWalk walks the module the way mcp's moduleFiles does — the seam rule
// needs the WHOLE tree, and a walk that is blind fails open.
func absentWalk(t *testing.T) [][2]string {
	t.Helper()
	var out [][2]string
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "testdata", ".git", ".dev", "dist":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out = append(out, [2]string{path, string(src)})
		return nil
	})
	if err != nil {
		t.Fatalf("walk the module: %v", err)
	}
	if len(out) < 100 {
		t.Fatalf("walked %d Go files — the module root is wrong or the walk is blind", len(out))
	}
	return out
}

// TestNoUnconditionalImport is scope 3's grep. The seam rule, exactly as
// MCP's: **cmd/aeon may import the layer (the CLI verbs are the seam); no
// runtime-side package may import it at all.** One stray `var _ =
// agentsvc.X` in the loop and deleting the layer breaks the daemon — which
// is how "optional" quietly stops being true.
func TestNoUnconditionalImport(t *testing.T) {
	var runtimeImporters, seamImporters []string
	for _, f := range absentWalk(t) {
		path, src := f[0], f[1]
		if !strings.Contains(src, agentsvcImportPath) {
			continue
		}
		clean := filepath.ToSlash(path)
		switch {
		case strings.Contains(clean, "/cmd/aeon/"):
			seamImporters = append(seamImporters, clean)
		case strings.Contains(clean, "/internal/agentsvc/"):
			// the package's own files (none import itself today; tolerated
			// by construction — the layer may always know itself)
		default:
			runtimeImporters = append(runtimeImporters, clean)
		}
	}
	if len(runtimeImporters) > 0 {
		t.Fatalf("the runtime imports the layer — AS11 broken: %v", runtimeImporters)
	}
	if len(seamImporters) == 0 {
		t.Fatal("no seam importer at all — the layer is not wired anywhere, which is not optional but dead")
	}
}

// TestSuitePassesWithAgentServicesOff is the gate in the plan's name, and
// here is what "the whole suite runs with the layer off" MEANS, reproducibly
// — not a build tag, not a flag, not a skip:
//
//	a package's test binary can only execute agentsvc code if some file in
//	that package imports it. So the set of packages whose suites touch the
//	layer at all is exactly the set carrying the import — and that set must
//	be {cmd/aeon (the seam), agentsvc (the layer)}. Every other package's
//	suite — the other twenty-four — runs with the layer provably OFF: the
//	binary cannot link it. `make ci` is therefore already the suite-off run
//	for the whole runtime; this test is what keeps that claim from rotting
//	(one import added to a loop test and the OFF claim was a lie).
func TestSuitePassesWithAgentServicesOff(t *testing.T) {
	linkers := map[string]bool{}
	for _, f := range absentWalk(t) {
		path, src := f[0], f[1]
		if !strings.Contains(src, agentsvcImportPath) {
			continue
		}
		// Package directory, module-relative.
		dir := filepath.ToSlash(filepath.Dir(path))
		dir = strings.TrimPrefix(dir, "../../")
		linkers[dir] = true
	}
	for dir := range linkers {
		if dir != "cmd/aeon" && dir != "internal/agentsvc" {
			t.Fatalf("package %q links the layer into its test binary — the suite no longer runs with agent services off", dir)
		}
	}
	if !linkers["cmd/aeon"] {
		t.Fatal("the seam does not link the layer — the daemon is unwired")
	}
	if !linkers["internal/agentsvc"] {
		t.Fatal("the layer does not test itself")
	}
}

// TestNoFeatureFlagWasAdded is scope 4's grep: the absence is a state of the
// machine (no cards directory), never a switch. AEON_* environment probes
// for the layer, boolean enabled fields, off-switch CLI flags: all are a
// second state to test, and the daemon's real behavior when the layer is
// absent must be the SAME code path a flag-off would take — so a flag could
// only diverge from it. (The words below are composed because the grep
// covers every byte of the module, this test's own bytes included.)
func TestNoFeatureFlagWasAdded(t *testing.T) {
	// The words are COMPOSED, not written out: the sweep greps the module's
	// own bytes, and a ban list spelled in full prosecutes itself (S12's
	// mcp version walks the same wire).
	banned := []string{"AEON_" + "AGENTS", "AGENTSVC_" + "ENABLED", "AGENTS_" + "ENABLED", "Agents" + "Enabled",
		"agents_" + "enabled", "--no-" + "agents", "agentsvc." + "enabled", "Disable" + "AgentServices"}
	for _, f := range absentWalk(t) {
		for _, b := range banned {
			if strings.Contains(f[1], b) {
				t.Fatalf("%s mentions the flag %q — deletability means absent configuration works, not that a switch exists", f[0], b)
			}
		}
	}
}

// TestLayerAbsentIsAStateNotAnError is this package's half of "the whole
// suite passes with agent services off": the off state is the ordinary
// state of a machine where nobody published a card — the register is EMPTY,
// honestly reported, and every question about a skill answers FALSE without
// dereferencing anything. The runtime half of the claim is the import grep
// above plus loop/pin_test.go and blueprint's pin refusal (quoted as
// witnesses, not re-run from the wrong package — S12's lesson).
func TestLayerAbsentIsAStateNotAnError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "never-created", "cards")
	cs := OpenCardStore(missing)
	iv := NewInvoker(nil, cs)
	d := cs.Discover()
	if len(d.Candidates) != 0 {
		t.Fatalf("a directory that does not exist yielded candidates: %v", d.Candidates)
	}
	// Absence is never SILENT (AS9): the empty register says why it is
	// empty — that is the difference between absent and broken.
	if len(d.Refuses) == 0 || !strings.Contains(d.Refuses[0], missing) {
		t.Fatalf("the missing register must name itself: %v", d.Refuses)
	}
	if iv.SkillKnown("reddit-agent", "summarise-sentiment") {
		t.Fatal("a skill from a register that does not exist cannot be known")
	}
	card, err := cs.Card("reddit-agent")
	if card != nil || err != nil {
		t.Fatalf("absent card answers (nil, nil) — three states, three answers: %v %v", card, err)
	}
}

// TestOneOfThe25BodiesDispatchesUnchanged is scope 2's concrete claim: the
// ladder still runs on markdown and a prompt. The SAME body (golang-engineer
// — a first-party handler with no card, no skills and no tools declared) is
// dispatched TWICE: once with the layer fully absent (no SetSkillKnown call
// has touched the process, no invoker exists), once with the layer wired
// from an EMPTY register (cards published nowhere). The prompt the model
// receives must be byte-identical and both runs must complete — the body
// does not know the layer exists, which is exactly what "needs no change"
// means when anybody finally deletes the directory out of the tree.
func TestOneOfThe25BodiesDispatchesUnchanged(t *testing.T) {
	const body = "golang-engineer"
	found := false
	for _, n := range handler.RegisteredBodies() {
		if n == body {
			found = true
		}
	}
	if !found {
		t.Fatalf("%q is no longer a registered body — S13's claim moved and this test was not told", body)
	}

	prompts := map[string]string{}
	calls := map[string]int{}
	advance := func(t *testing.T, label string, wire func()) {
		t.Helper()
		if wire != nil {
			wire()
			t.Cleanup(func() { loop.SetSkillKnown(nil) })
		}
		s, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { s.Close() })
		path := filepath.Join(t.TempDir(), "ladder.yaml")
		if err := os.WriteFile(path, []byte(
			"id: ladder\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: implement\n    step: {kind: model-step, agent: "+body+"}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		st, err := loop.Start(context.Background(), s, store.NewLoop{
			Slug: "absent-" + label, ComponentRoot: "/repo", Goal: "ship it",
		}, path, nil, nil)
		if err != nil {
			t.Fatalf("the plain ladder refused to start with agent services %s: %v", label, err)
		}
		dep := loop.Deps{Store: s, Event: func(string, any) {},
			Model: func(_ context.Context, _ loop.LoopState, _ blueprint.Step, prompt string, _ string)(loop.ModelResult, error) {
				calls[label]++
				prompts[label] = prompt
				return loop.ModelResult{Kind: "completed", Outcome: "ok"}, nil
			}}
		if _, err := loop.AdvanceLoop(context.Background(), dep, st.ID); err != nil {
			t.Fatalf("advance with agent services %s: %v", label, err)
		}
	}
	// Run one: the layer ABSENT — nothing has ever touched the process.
	advance(t, "absent", nil)
	// Run two: the layer WIRED from an empty register (no card published).
	empty := OpenCardStore(filepath.Join(t.TempDir(), "no-cards"))
	iv := NewInvoker(nil, empty)
	advance(t, "wired-empty", func() { loop.SetSkillKnown(iv.SkillKnown) })

	if calls["absent"] != 1 || calls["wired-empty"] != 1 {
		t.Fatalf("the body must dispatch exactly once per ladder: %v", calls)
	}
	if prompts["absent"] == "" {
		t.Fatal("the body dispatched with an empty prompt — S13's dispatch was not exercised")
	}
	if prompts["absent"] != prompts["wired-empty"] {
		t.Fatalf("the layer changed what the model sees for a plain body — the 25 bodies need no change claim is false:\nabsent: %q\nwired:  %q", prompts["absent"], prompts["wired-empty"])
	}
}

// TestSkillLaneWithoutAgentServicesRefusesStructurally is scope 5: a lane
// targeting a skill when no agent services are configured gets a refusal
// that NAMES the absence, at loop start (the pin preflight) — not a nil
// dereference mid-run, not a silent run of the lane as a plain step. The
// refusal text lives in blueprint's validator (the witness package); this
// test drives it through loop.Start so the entry point a user meets is the
// one asserted.
func TestSkillLaneWithoutAgentServicesRefusesStructurally(t *testing.T) {
	loop.SetSkillKnown(nil) // the absent state, asserted explicitly
	s, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	path := filepath.Join(t.TempDir(), "skill.yaml")
	if err := os.WriteFile(path, []byte(
		"id: skillful\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: ask\n    step: {kind: model-step, agent: planner, skill: reddit-agent/do-thing}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = loop.Start(context.Background(), s, store.NewLoop{
		Slug: "skillless", ComponentRoot: "/repo", Goal: "ask around",
	}, path, nil, nil)
	if err == nil {
		t.Fatal("a skill lane started with agent services absent — the refusal went missing")
	}
	if !strings.Contains(err.Error(), "not configured") || !strings.Contains(err.Error(), "reddit-agent/do-thing") {
		t.Fatalf("the refusal must name the absence and the target: %v", err)
	}
	if strings.Contains(err.Error(), "nil pointer") || strings.Contains(err.Error(), "panic") {
		t.Fatalf("a crash is not a refusal: %v", err)
	}
}
