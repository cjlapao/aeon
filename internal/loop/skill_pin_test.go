package loop

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/store"
)

// T0 / T11 — a lane that targets a skill while agent services are unconfigured
// never becomes a durable loop that is guaranteed to block at its first step.
// The refusal is structured, names the lane, and quotes the daemon's sentence.

func TestStartRefusesSkillLaneWhenServicesUnconfigured(t *testing.T) {
	s := openLoopStore(t)
	path := filepath.Join(t.TempDir(), "ask-reddit.yaml")
	raw := "id: ask-reddit\nschemaVersion: 1\nversion: 1\nlanes:\n" +
		"  - id: survey\n    step: {kind: model-step, agent: golang-engineer, skill: \"reddit-agent/summarise-sentiment\", produces: OUT.md}\n"
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Start(context.Background(), s, store.NewLoop{Slug: "ask", ComponentRoot: "/repo", Goal: "goal"}, path, nil, nil)
	if err == nil {
		t.Fatal("a skill lane started with no agent services configured")
	}
	refusal, ok := err.(PinRefusal)
	if !ok || refusal.Kind != "unavailable-skill" {
		t.Fatalf("want PinRefusal unavailable-skill, got %T %v", err, err)
	}
	for _, want := range []string{"lanes[survey].step.skill", "reddit-agent/summarise-sentiment", "agent services"} {
		if !strings.Contains(refusal.Detail, want) {
			t.Fatalf("refusal must name %q, got %q", want, refusal.Detail)
		}
	}
	// Nothing durable was written.
	loops, lerr := s.ListLoops(store.ComponentScope{ComponentRoot: "/repo"})
	if lerr != nil || len(loops) != 0 {
		t.Fatalf("a refused skill lane created loops: %v %v", loops, lerr)
	}
}

func TestStartPlainLanesUnaffectedBySkillChecks(t *testing.T) {
	s := openLoopStore(t)
	path := filepath.Join(t.TempDir(), "plain.yaml")
	writeBlueprint(t, path, "  - id: first\n    step: {kind: model-step, agent: default}\n")
	if _, err := Start(context.Background(), s, store.NewLoop{Slug: "plain", ComponentRoot: "/repo", Goal: "goal"}, path, nil, nil); err != nil {
		t.Fatalf("a plain lane must pin exactly as before: %v", err)
	}
}

// ── T4: the seam ─────────────────────────────────────────────────────────────

// The pin decision for a skill lane must be INJECTED, never imported: the
// loop package cannot know what a signed card offers, and importing agentsvc
// here would break AS11's direction (and T11's deletability). These two
// tests walk both answers through the same lane.
func TestSkillKnowledgeIsInjectedNotImported(t *testing.T) {
	defer SetSkillKnown(nil)

	// Configured-but-unknown: the lane refuses, naming the target.
	SetSkillKnown(func(callee, skill string) bool { return false })
	if _, err := startSkillLaneLoop(t, "injected-unknown"); err == nil {
		t.Fatal("a registry that does not know the skill must not let the lane pin")
	} else if !strings.Contains(err.Error(), "reddit-agent/summarise-sentiment") {
		t.Fatalf("the refusal must name the target: %v", err)
	}

	// Known: the SAME bytes pin, and the loop exists.
	SetSkillKnown(func(callee, skill string) bool {
		return callee == "reddit-agent" && skill == "summarise-sentiment"
	})
	st, err := startSkillLaneLoop(t, "injected-known")
	if err != nil {
		t.Fatalf("a skill the registry knows must pin: %v", err)
	}
	if st.ID == "" {
		t.Fatal("pinned without an id")
	}

	// nil is NOT "knows nothing quietly" — it is "not configured", which the
	// pin turns into the honest refusal (the daemon-less case).
	SetSkillKnown(nil)
	if _, err := startSkillLaneLoop(t, "injected-nil"); err == nil {
		t.Fatal("an unwired daemon must refuse skill lanes, not pin guaranteed blockers")
	}
}

// One seam, one production call (the SEC-059 discipline reused): the loop
// package counts the wiring sites in the only entry point that wires, so a
// second definition of "who may be asked" cannot appear quietly.
func TestTheSkillKnownWiringAppearsInExactlyOnePlace(t *testing.T) {
	dir := "../../cmd/aeon"
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	hits := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		raw, err := os.ReadFile(dir + "/" + e.Name())
		if err != nil {
			t.Fatal(err)
		}
		hits += strings.Count(string(raw), "loop.SetSkillKnown(")
	}
	if hits != 1 {
		t.Errorf("loop.SetSkillKnown( appears %d times in cmd/aeon, want exactly 1 — two wirings are two truths about who may be asked", hits)
	}
}

func startSkillLaneLoop(t *testing.T, slug string) (LoopState, error) {
	t.Helper()
	s := openLoopStore(t)
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "skilllane.yaml")
	raw := "id: skilllane\nschemaVersion: 1\nversion: 1\nlanes:\n" +
		"  - id: survey\n    step: {kind: model-step, agent: golang-engineer, skill: \"reddit-agent/summarise-sentiment\", produces: SURVEY.md}\n"
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	return Start(context.Background(), s, store.NewLoop{Slug: slug, ComponentRoot: root, Root: root, Goal: "ask"}, path, nil, nil)
}
