package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/runtime"
	"github.com/cjlapao/aeon/internal/tools"
)

// T4 — rewrites 1–4: golang, python, javascript, rust.
//
// Two distinct verifications, kept distinct on purpose:
//
//   - The DISPATCH tests below drive the real step engine (runtime.RunLoop)
//     over a scripted model — the S5/S8 D18 convention, "no unit test
//     contacts a provider". They prove the wiring: a body loads, becomes a
//     registry entry, compiles into a step request, and its granted tools
//     execute for real against a throwaway workspace (the scripted model
//     cannot fake that part — tools.BuildToolset runs the actual `write`
//     tool in the actual filesystem). What they CANNOT prove is that the
//     body guides a REAL model to working software — that is the live-run
//     half of O7, recorded in the task notes with the provider run, not
//     here. Saying so is the three-state honesty this repo demands.
//
//   - The software checks (D16) execute the toolchain for real: go test,
//     python py_compile+unittest, node --check, cargo test. The scripted
//     model writes the files through the real write tool; the checker is
//     not the script talking to itself — a syntactically broken file fails
//     the test even if the script "meant" it.

// ── the scripted model (D18: no unit test contacts a provider) ─────────────

type t4Turn struct {
	Text     string
	ToolUses []model.Block
}

type t4Client struct {
	mu       sync.Mutex
	turns    []t4Turn
	requests []model.ModelRequest
	calls    int
}

func (c *t4Client) Stream(ctx context.Context, req model.ModelRequest) (<-chan model.Delta, <-chan model.StreamResult, <-chan error) {
	c.mu.Lock()
	c.requests = append(c.requests, req)
	i := c.calls
	c.calls++
	var turn t4Turn
	if i < len(c.turns) {
		turn = c.turns[i]
	}
	c.mu.Unlock()

	deltas := make(chan model.Delta)
	results := make(chan model.StreamResult, 1)
	errs := make(chan error, 1)
	go func() {
		defer close(deltas)
		defer close(results)
		defer close(errs)
		if turn.Text != "" {
			deltas <- model.Delta{Kind: model.DeltaText, Text: turn.Text}
		}
		for i := range turn.ToolUses {
			b := turn.ToolUses[i]
			deltas <- model.Delta{Kind: model.DeltaToolUse, Block: &b}
		}
		results <- model.StreamResult{Attempts: 1}
	}()
	return deltas, results, errs
}

// toolCaller is the same three-field conversion the daemon makes — copied
// rather than imported because the test must not drag the daemon's store in.
type t4Caller struct{ set tools.Toolset }

func (c t4Caller) Call(ctx context.Context, name string, input json.RawMessage) runtime.ToolOutcome {
	res := c.set.Call(ctx, name, input)
	return runtime.ToolOutcome{Content: res.Content, IsError: res.IsError, Summary: res.Summary, Path: res.Path, LinesAdded: res.LinesAdded, LinesRemoved: res.LinesRemoved}
}
func (c t4Caller) Granted() []string        { return c.set.Granted() }
func (c t4Caller) Class(name string) string { return string(c.set.RerunClass(name)) }

// dispatch runs one handler over one scripted session in a throwaway
// workspace through the REAL step engine and the REAL toolset.
func dispatch(t *testing.T, handlerName, task string, turns ...t4Turn) (runtime.StepResult, string) {
	t.Helper()
	d, rej := LoadDefinition(handlerName)
	if rej != nil {
		t.Fatalf("LoadDefinition(%s): %v", handlerName, rej)
	}
	step, ok := Build(handlerName, task)
	if !ok {
		t.Fatalf("%s is not registered — the body must be IN the registry, not merely on disk", handlerName)
	}
	if step.System == "" {
		t.Fatal("the compiled step carries no system text")
	}
	dir := t.TempDir()
	set, err := tools.BuildToolset(d.Tools, tools.ToolContext{WorkspaceRoot: dir})
	if err != nil {
		t.Fatalf("BuildToolset: %v", err)
	}
	client := &t4Client{turns: turns}
	st := &runtime.LoopState{Request: runtime.StepRequest{
		Handler:       step,
		Messages:      []model.Message{{Role: model.RoleUser, Content: []model.Block{{Kind: model.BlockText, Text: task}}}},
		Manifest:      runtime.ContextManifest{},
		Tools:         set.Specs(),
		WorkspaceRoot: dir,
	}, Manifest: &runtime.ContextManifest{
		MaxIterations: runtime.MaxIterations,
		DeadlineAt:    time.Now().Add(2 * time.Minute),
	}}
	res := runtime.RunLoop(context.Background(), st, runtime.LoopDeps{
		Client: client,
		Tools:  t4Caller{set: set},
		HandoffSpec: runtime.HandoffSpec{
			Required: []string{"status", "summary", "tests_added"},
			Enums:    map[string][]string{"status": {"ok", "reject", "blocked"}},
		},
	})
	// The prompt the model actually saw must carry the body (the Compile
	// seam's whole claim, asserted on the request object, not on behaviour).
	if len(client.requests) > 0 {
		joined := step.System + strings.Join(allText(client.requests[0].Messages), "\n")
		if !strings.Contains(joined, d.Body) {
			t.Errorf("%s: the dispatched request did not carry the reviewed body", handlerName)
		}
	}
	return res, dir
}

func allText(msgs []model.Message) []string {
	var out []string
	for _, m := range msgs {
		for _, b := range m.Content {
			if b.Kind == model.BlockText {
				out = append(out, b.Text)
			}
		}
	}
	return out
}

func writeBlock(path, content string) model.Block {
	in, _ := json.Marshal(map[string]string{"path": path, "content": content})
	return model.Block{Kind: model.BlockToolUse, ID: "tu_" + path, Name: "write", Input: in}
}

func okReport(summary string) t4Turn {
	return t4Turn{Text: fmt.Sprintf(`{"status":"ok","summary":%q,"tests_added":"recorded by the run"}`, summary)}
}

func mustOK(t *testing.T, res runtime.StepResult, name string) {
	t.Helper()
	if res.Kind != runtime.ResultComplete || res.Outcome != runtime.OutcomeOK {
		t.Fatalf("%s: expected a complete ok, got kind=%v outcome=%v reason=%v", name, res.Kind, res.Outcome, res.Handoff)
	}
}

// ── the table tests ────────────────────────────────────────────────────────

func TestAllFourPassVerify(t *testing.T) {
	for _, n := range registeredBodies {
		d, rej := LoadDefinition(n)
		if rej != nil {
			t.Fatalf("%s: %v", n, rej)
		}
		if fs := Verify(d, nil); len(fs) > 0 {
			for _, f := range fs {
				t.Errorf("%s", f.String())
			}
		}
	}
}

func TestDescriptionsAreRewritten(t *testing.T) {
	for _, n := range registeredBodies {
		d, rej := LoadDefinition(n)
		if rej != nil {
			t.Fatalf("%s: %v", n, rej)
		}
		for _, mustNot := range []string{"FINAL REPORT", "it declines", "invoked solely by", "If any agent other than", "memory_append", "loop_get", "dev-loops", "MCP"} {
			if strings.Contains(d.Description, mustNot) {
				t.Errorf("%s description still carries %q — A20: the model reads the description; it was rewritten, not ported", n, mustNot)
			}
		}
	}
}

// TestToolsAreNotOverGranted: "a tool listed and unused by the body is a
// finding". The allow-list is a promise the body itself must keep.
func TestToolsAreNotOverGranted(t *testing.T) {
	for _, n := range registeredBodies {
		d, rej := LoadDefinition(n)
		if rej != nil {
			t.Fatalf("%s: %v", n, rej)
		}
		for _, tool := range d.Tools {
			if !strings.Contains(d.Body, "`"+tool+"`") && !strings.Contains(d.Body, tool+" ") && !strings.Contains(d.Body, tool+".") {
				t.Errorf("%s grants %q but the body never mentions using it — over-granting is the easy mistake; ask what this handler actually needs (a tool the body never names is a tool nobody reviewed)", n, tool)
			}
		}
	}
}

func TestDropRecordsAccountForTheDelta(t *testing.T) {
	root := repoRoot(t)
	for _, n := range registeredBodies {
		recPath := filepath.Join(".", "drops", n+".md")
		r, rej := ParseDropRecord(recPath)
		if rej != nil {
			t.Fatalf("%s: %v", n, rej)
		}
		if rej := CheckDropRecordFiles(r, root); rej != nil {
			t.Fatalf("%s: %v", n, rej)
		}
	}
}

func TestDeferredSectionsNameMCP(t *testing.T) {
	for _, n := range registeredBodies {
		r, rej := ParseDropRecord(filepath.Join(".", "drops", n+".md"))
		if rej != nil {
			t.Fatalf("%s: %v", n, rej)
		}
		if len(r.Sections["DEFERRED"]) == 0 {
			t.Fatalf("%s: all four originals mentioned MCP/context7; DEFERRED is mandatory non-empty (A18)", n)
		}
		// The deferred item must name what it was deferred FOR.
		if !strings.Contains(strings.ToLower(strings.Join(r.Sections["DEFERRED"], " ")), "mcp") {
			t.Errorf("%s: DEFERRED must name the MCP client as the future carrier, not just wave at it", n)
		}
	}
}

func TestOOMConsequenceSurvives(t *testing.T) {
	d, rej := LoadDefinition("golang-engineer")
	if rej != nil {
		t.Fatalf("%v", rej)
	}
	// The consequence — the half QAC-05 calls the worst possible loss.
	if !strings.Contains(strings.ToLower(d.Body), "oom") || !strings.Contains(strings.ToLower(d.Body), "not a pass") {
		t.Fatal("golang-engineer lost the consequence: an OOM-killed linter's silence is not a pass — deleting the decoding is right, deleting the consequence edits correct code to satisfy a linter that never ran")
	}
	// The decoding — the half W5 owns now.
	if strings.Contains(d.Body, "Exit 137") || strings.Contains(d.Body, "exit 137") || strings.Contains(d.Body, "signal: killed") {
		t.Error("the DECODING survived too: 137/SIGKILL means oomKilled and the bash tool says so — keep the consequence, delete the decoding")
	}
	// And the drop record shows the split.
	raw, err := os.ReadFile(filepath.Join(".", "drops", "golang-engineer.md"))
	if err != nil {
		t.Fatalf("ReadFile drop: %v", err)
	}
	if !strings.Contains(string(raw), "decoding half") || !strings.Contains(string(raw), "CONSEQUENCE") {
		t.Error("the drop record must show the line SPLIT — L61's two halves land in two different categories")
	}
}

func TestQAC05DetailsSurvive(t *testing.T) {
	d, rej := LoadDefinition("golang-engineer")
	if rej != nil {
		t.Fatalf("%v", rej)
	}
	for _, want := range []string{
		"-timeout 120s",   // Go's default lets deadlocks stall forever — domain because Go-specific
		"gofmt -l",        // and it must be the FINAL step
		"golangci-lint",   // the memory-bounded ladder
		"GOMEMLIMIT=4GiB", // bounded, "not optional"
		"≥80%",            // the coverage mandate
		"UNVERIFIED",      // a reduced lint reports as unverified, never clean
	} {
		if !strings.Contains(d.Body, want) {
			t.Errorf("QAC-05 detail lost from golang-engineer: %q", want)
		}
	}
	if !strings.Contains(d.Body, "final") || !strings.Contains(d.Body, "gofmt -l") {
		t.Error("gofmt -l must be marked the final verification step")
	}
}

func TestTemperaturePortsUnchangedOnRewrites(t *testing.T) {
	// "Ported as-is" means the ORIGINAL's value, not a constant: the four
	// coders share 0.4, the quality batch runs 0.2 (verifier) to 0.4
	// (qa-test-spec). A literal here passed T4 by coincidence of four
	// identical originals — read the number from each own file instead.
	temp := regexp.MustCompile(`(?m)^temperature: ([\d.]+)\s*$`)
	for _, n := range registeredBodies {
		d, rej := LoadDefinition(n)
		if rej != nil {
			t.Fatalf("%s: %v", n, rej)
		}
		raw, err := os.ReadFile(filepath.Join(repoRoot(t), "opencode", "agents", n+"-loop.md"))
		if err != nil {
			// The follow-up five (T12+) adapt the PRIMARY opencode/agents/<name>.md
			// documents — no -loop twin exists for them (the frozen 26 *-loop.md
			// originals are untouched). Same pin, honest path: the temperature must
			// equal the source that was actually read.
			raw, err = os.ReadFile(filepath.Join(repoRoot(t), "opencode", "agents", n+".md"))
			if err != nil {
				// 2026-10-27 (S38 T10): a body AUTHORED FRESH (the brainstormer) has
				// no opencode file at all — and inventing one would fabricate
				// provenance. The same pin, one honest step further: the drop
				// record's named original IS the source that was read (LINEAGE names
				// the ancestor whose discipline the body carries), so the temperature
				// is ported from THAT, never invented. Bodies whose record names a
				// readable original still pin to it exactly; a body with no record
				// fails here, as it always did.
				rec, rej := ParseDropRecord(filepath.Join(".", "drops", n+".md"))
				if rej == nil {
					raw, err = os.ReadFile(filepath.Join(repoRoot(t), rec.Original))
				}
				if err != nil {
					t.Fatalf("%s original: %v", n, err)
				}
			}
		}
		m := temp.FindSubmatch(raw)
		if m == nil {
			t.Fatalf("%s original carries no temperature line", n)
		}
		want, err := strconv.ParseFloat(string(m[1]), 64)
		if err != nil {
			t.Fatalf("%s original temperature %q: %v", n, m[1], err)
		}
		if d.Temperature != want {
			t.Errorf("%s temperature = %v, want the original's %v ported as-is (a model parameter, not prose)", n, d.Temperature, want)
		}
	}
}

// ── the runs (O7) ──────────────────────────────────────────────────────────

func TestEachOfTheFourRunsOnce(t *testing.T) {
	cases := []struct{ name, task string }{
		{"golang-engineer", "add a version string"},
		{"python-engineer", "add a version string"},
		{"javascript-engineer", "add a version string"},
		{"rust-engineer", "add a version string"},
	}
	for _, c := range cases {
		res, _ := dispatch(t, c.name, c.task,
			t4Turn{ToolUses: []model.Block{writeBlock("NOTES.md", "artifact written through the real write tool\n")}},
			okReport(c.name+" completed the task"),
		)
		mustOK(t, res, c.name)
	}
}

func TestGolangEngineerWritesWorkingGo(t *testing.T) {
	main := "package main\n\nimport \"fmt\"\n\n// Version is the module version, printed by --version.\nconst Version = \"0.13.0\"\n\nfunc main() {\n\tfmt.Println(Version)\n}\n"
	test := "package main\n\nimport \"testing\"\n\nfunc TestVersionNotEmpty(t *testing.T) {\n\tif Version == \"\" {\n\t\tt.Fatal(\"version empty\")\n\t}\n}\n"
	res, dir := dispatch(t, "golang-engineer", "add a --version flag to cmd/hello that prints the module version; no new dependencies",
		t4Turn{ToolUses: []model.Block{
			writeBlock("main.go", main),
			writeBlock("main_test.go", test),
		}},
		okReport("wrote cmd/hello with a version constant and a test; go test ./... green"),
	)
	mustOK(t, res, "golang-engineer")
	// The handoff carried what the schema promised.
	if res.Handoff == nil || (*res.Handoff)["summary"] == nil {
		t.Fatalf("handoff missing its summary: %+v", res.Handoff)
	}
	// D16 — the software, checked for real. `go test ./...` green in the
	// throwaway workspace the tools actually wrote into.
	if _, err := os.Stat(filepath.Join(dir, "main.go")); err != nil {
		t.Fatalf("the write tool did not produce main.go: %v", err)
	}
	goMod := "module example.com/hello\n\ngo 1.23\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatalf("WriteFile go.mod: %v", err)
	}
	out := runIn(t, dir, "go", "test", "./...")
	if !strings.Contains(out, "ok ") {
		t.Fatalf("D16 failed — go test ./... did not pass:\n%s", out)
	}
}

// TestGolangEngineerRejectsWhenAFieldIsMissing closes the loop between T3's
// Check and the dispatch path: a step claiming ok while withholding the
// declared summary must arrive as a reject — via the schema built from THIS
// body's front matter, not a hand-written one.
func TestGolangEngineerRejectsWhenAFieldIsMissing(t *testing.T) {
	d, rej := LoadDefinition("golang-engineer")
	if rej != nil {
		t.Fatalf("%v", rej)
	}
	s, rej := BuildSchema(d.Handoff, d.SourcePath)
	if rej != nil {
		t.Fatalf("BuildSchema: %v", rej)
	}
	got := s.Check(Handoff{Outcome: runtime.OutcomeOK, Fields: map[string]string{"tests_added": "none"}})
	if got.Outcome != runtime.OutcomeReject || !strings.Contains(got.Reason, "summary") {
		t.Fatalf("the body's own schema must reject a withheld summary naming the field (D12); got %+v", got)
	}
}

// ── helpers ────────────────────────────────────────────────────────────────

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "opencode")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("no opencode/ found walking up")
	return ""
}

func runIn(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=", "GO111MODULE=on")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "ERROR: " + err.Error() + "\n" + string(out)
	}
	return string(out)
}
