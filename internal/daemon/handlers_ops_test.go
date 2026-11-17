package daemon

// The bench demo runner's daemon side (spec 2026-10-25-bench-demo-runner):
// `handlers.list` and `handlers.verify` as SOCKET READS that answer refusals
// as DATA the app renders verbatim (the T10 retry pattern, not a JSON-RPC
// error the UI would toast away), originals reachable by NAME only with the
// report carrying no path at all (basename-only, report-only), and the
// resolved dispatch agent reaching the loop view from the step row the
// dispatch created.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// repoAgentsDir is the negative-control directory, found the way the daemon
// finds it: walking up from the package directory.
func repoAgentsDir(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		candidate := filepath.Join(dir, "opencode", "agents")
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no opencode/agents above the test package — the repository moved")
		}
		dir = parent
	}
}

func TestHandlersListShape(t *testing.T) {
	d := New(Options{}) // no store: the registry is embedded, so this read needs nothing
	if d.Table.HandlersList == nil {
		t.Fatal("handlers.list is registered but the daemon wires no seam")
	}
	out, err := d.Table.HandlersList(context.Background(), protocol.HandlersListParams{})
	if err != nil {
		t.Fatalf("listing the embedded registry failed closed on a healthy daemon: %v", err)
	}
	view, ok := out.(protocol.HandlersListView)
	if !ok {
		t.Fatalf("the seam answers %T, want protocol.HandlersListView", out)
	}
	if len(view.Resolvers) != 1 || view.Resolvers[0] != "by-tech-tag" {
		t.Errorf("resolvers = %v, want exactly [by-tech-tag]", view.Resolvers)
	}
	kinds := map[string]string{}
	for _, h := range view.Handlers {
		kinds[h.Name] = h.Kind
	}
	if got := kinds["golang-engineer"]; got != "body" {
		t.Errorf(`golang-engineer kind = %q, want "body"`, got)
	}
	if got := kinds["default"]; got != "preset" {
		t.Errorf(`default kind = %q, want "preset" — the 25-vs-27 truth the demo tells`, got)
	}
	if got := kinds["read-only"]; got != "preset" {
		t.Errorf(`read-only kind = %q, want "preset"`, got)
	}
	nBody, nPreset := 0, 0
	for _, k := range kinds {
		switch k {
		case "body":
			nBody++
		case "preset":
			nPreset++
		}
	}
	// 2026-10-27 (S38 T10): + the brainstormer — Count 30 -> 31 DELIBERATE.
	// 2026-10-28 (S39 T6): + the chat body — Count 31 -> 32 DELIBERATE.
	// 2026-11-05 (S61 T9): + the intake-architect — Count 33 -> 34 DELIBERATE.
	if nBody != 34 || nPreset != 2 { // +project-manager at T16; +brainstormer at S38 T10; +chat at S39 T6; +sizing-estimator at S24 T5; +intake-architect at S61 T9 (measured 2026-11-05)
		t.Errorf("kinds: %d bodies and %d presets, want 34 and 2", nBody, nPreset)
	}
	for _, h := range view.Handlers {
		if h.Name == "golang-engineer" && len(h.Tools) == 0 {
			t.Error("the go specialist lists no tools — the runner's verdict text would have nothing to name")
		}
	}
	for i := 1; i < len(view.Handlers); i++ {
		if view.Handlers[i-1].Name > view.Handlers[i].Name {
			t.Fatal("the listing is not sorted — a shuffle each call makes the bench diff-noisy")
		}
	}
}

func TestHandlersVerifyCorpusPasses(t *testing.T) {
	d := New(Options{})
	if d.Table.HandlersVerify == nil {
		t.Fatal("handlers.verify is registered but the daemon wires no seam")
	}
	out, err := d.Table.HandlersVerify(context.Background(), protocol.HandlersVerifyParams{Scope: "corpus"})
	if err != nil {
		t.Fatalf("corpus verify must answer, not error: %v", err)
	}
	view, ok := out.(protocol.HandlersVerifyView)
	if !ok {
		t.Fatalf("the seam answers %T, want protocol.HandlersVerifyView", out)
	}
	// 2026-10-27 (S38 T10): + the brainstormer — Count 30 -> 31 DELIBERATE.
	// 2026-10-28 (S39 T6): + the chat body — Count 31 -> 32 DELIBERATE.
	// 2026-10-29 (S24 T5): + the sizing-estimator — Count 32 -> 33 DELIBERATE.
	// 2026-11-05 (S61 T9): + the intake-architect — Count 33 -> 34 DELIBERATE.
	if view.Scope != "corpus" || view.Passed != 34 || len(view.Findings) != 0 { // +project-manager at T16; +brainstormer at S38 T10; +chat at S39 T6; +sizing-estimator at S24 T5; +intake-architect at S61 T9 (measured 2026-11-05)
		t.Errorf("corpus: passed=%d findings=%d, want 34 and none (the registry panics otherwise)", view.Passed, len(view.Findings))
	}
}

func TestHandlersVerifyOriginalRefusesAsData(t *testing.T) {
	d := New(Options{})
	out, err := d.Table.HandlersVerify(context.Background(), protocol.HandlersVerifyParams{Scope: "original", Name: "golang-engineer-loop"})
	if err != nil {
		t.Fatalf("an original failing verify is DATA, not a socket error: %v", err)
	}
	view, ok := out.(protocol.HandlersVerifyView)
	if !ok {
		t.Fatalf("the seam answers %T, want protocol.HandlersVerifyView", out)
	}
	if view.Scope != "original" || view.Name != "golang-engineer-loop" || !view.Refused {
		t.Fatalf("original verdict: %+v — the demo's whole §4 moment is refused:true", view)
	}
	refs := map[string]bool{}
	sawSkill := false
	for _, f := range view.Findings {
		refs[f.Ref] = true
		if f.Kind == "skill" {
			sawSkill = true
		}
	}
	for _, want := range []string{"loop_get", "memory_append", "event_append"} {
		if !refs[want] {
			t.Errorf("the refusal does not name %q — all 26 originals reference it", want)
		}
	}
	if !sawSkill {
		t.Error("no skill finding — dev-loop-protocol is named by every original")
	}
	// Basename-only: nothing in the answer may be a path. The daemon knows
	// where opencode/ is; the socket answer must not teach callers about it.
	raw, merr := json.Marshal(view)
	if merr != nil {
		t.Fatal(merr)
	}
	if strings.Contains(string(raw), "/") {
		t.Errorf("the verify answer carries a path: %s", raw)
	}
	for _, f := range view.Findings {
		if f.Handler != "golang-engineer-loop" {
			t.Fatalf("finding names %q, want the basename the caller asked for — never a path, never a surprise name", f.Handler)
		}
	}
}

func TestHandlersVerifyRefusalsAsDataAndErrors(t *testing.T) {
	d := New(Options{})

	// An unknown original name refuses AS DATA and names what IS available.
	out, err := d.Table.HandlersVerify(context.Background(), protocol.HandlersVerifyParams{Scope: "original", Name: "nope-loop"})
	if err != nil {
		t.Fatalf("unknown original must refuse as data: %v", err)
	}
	view := out.(protocol.HandlersVerifyView)
	if view.Refused || view.Reason == nil || view.Reason.Kind != "unknown-name" {
		t.Fatalf("unknown-name refusal wrong: %+v", view)
	}
	if !strings.Contains(view.Reason.Detail, "golang-engineer-loop") {
		t.Errorf("the unknown-name refusal must name available originals, got: %s", view.Reason.Detail)
	}

	// A name that is not a bare basename is a PARAMS error — the caller
	// mistyped, this is not a verdict. Path pieces never become lookups.
	isBadParams := func(err error) bool {
		var bad protocol.ErrBadParams
		return errors.As(err, &bad)
	}
	for _, bad := range []string{"../opencode/agents/golang-engineer-loop", "a/b", "", ".", "..", "Golang"} {
		if _, err := d.Table.HandlersVerify(context.Background(), protocol.HandlersVerifyParams{Scope: "original", Name: bad}); !isBadParams(err) {
			t.Errorf("name %q returned %v, want ErrBadParams", bad, err)
		}
	}
	if _, err := d.Table.HandlersVerify(context.Background(), protocol.HandlersVerifyParams{Scope: "sideways"}); !isBadParams(err) {
		t.Errorf("an unknown scope returned %v, want ErrBadParams", err)
	}

	// Away from the source tree the answer is an honest refusal naming the
	// remedy — never a pass, never a silent empty finding list. The real
	// directory is captured BEFORE the chdir: the walk-up is the discovery
	// under test, not a tool the test may still use afterwards.
	agents := repoAgentsDir(t)
	dir := t.TempDir()
	t.Chdir(dir)
	out, err = d.Table.HandlersVerify(context.Background(), protocol.HandlersVerifyParams{Scope: "original", Name: "golang-engineer-loop"})
	if err != nil {
		t.Fatalf("originals-unavailable is data: %v", err)
	}
	view = out.(protocol.HandlersVerifyView)
	if view.Reason == nil || view.Reason.Kind != "originals-unavailable" {
		t.Fatalf("outside the tree: %+v, want refusal kind originals-unavailable", view)
	}
	if !strings.Contains(view.Reason.Detail, "AEON_OPENCODE_DIR") {
		t.Errorf("the refusal must name the remedy, got: %s", view.Reason.Detail)
	}

	// The env override makes it available again from the same directory.
	t.Setenv("AEON_OPENCODE_DIR", agents)
	out, err = d.Table.HandlersVerify(context.Background(), protocol.HandlersVerifyParams{Scope: "original", Name: "golang-engineer-loop"})
	if err != nil {
		t.Fatal(err)
	}
	if view := out.(protocol.HandlersVerifyView); !view.Refused || len(view.Findings) == 0 {
		t.Fatalf("with AEON_OPENCODE_DIR set the original must verify and refuse: %+v", view)
	}
}

func TestStepAgentReachesTheView(t *testing.T) {
	s := recoveryStore(t)
	d := New(Options{Store: s})

	// A tag-only loop whose step row was created by the dispatch — the row
	// carries the RESOLVED name, which is the proof the runner shows.
	raw := "id: tagonly\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: implement\n    step: {kind: model-step, agent: by-tech-tag, produces: OUT.md}\n"
	bp, err := blueprint.Load([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	st := loop.LoopState{Slug: "agentview", Status: "running", Goal: "g", ComponentRoot: "/repo", Blueprint: &bp, RuntimeState: loop.InitialState(bp)}
	js, err := st.RuntimeJSON()
	if err != nil {
		t.Fatal(err)
	}
	l, err := s.CreatePinnedLoop(store.NewLoop{Slug: "agentview", ComponentRoot: "/repo", Goal: "g"}, bp.ID, bp.Version, raw, js)
	if err != nil {
		t.Fatal(err)
	}
	step, err := s.CreateStep(store.Scope{LoopSlug: "agentview", ComponentRoot: "/repo"}, store.NewStep{Phase: "implement", Agent: "golang-engineer"})
	if err != nil {
		t.Fatal(err)
	}
	st.ID = l.ID
	st.StepStates["implement"] = loop.StepState{Status: "running", StepID: step.ID}
	js, err = st.RuntimeJSON()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SavePinnedRuntime(l.ID, "running", js); err != nil {
		t.Fatal(err)
	}

	out, err := d.Table.ShowBlueprintLoop(context.Background(), protocol.LoopShowParams{ID: l.ID})
	if err != nil {
		t.Fatal(err)
	}
	v, ok := out.(LoopView) // the view type is the daemon's own
	if !ok {
		t.Fatalf("loop.show answers %T", out)
	}
	var agent string
	for _, lane := range v.Lanes {
		for _, s := range lane.Steps {
			if s.ID == "implement" {
				agent = s.Agent
			}
		}
	}
	if agent != "golang-engineer" {
		t.Errorf("the loop view carries agent %q, want the resolved golang-engineer — without it the --tech go verdict has nothing to cite", agent)
	}
}
