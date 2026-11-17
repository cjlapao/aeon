package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/runtime"
	"github.com/cjlapao/aeon/internal/tools"
)

// The composition point's tests, and **the only place the whole step runs**:
// the real runtime, the real toolset and a scripted model, with nothing
// adapted away.
//
// # Why D1's most important assertion lives here
//
// `internal/runtime` cannot import `internal/tools` and `internal/tools`
// cannot import `internal/runtime` — each is a leaf on purpose. So "a model
// asks for `bash` in a step that was granted only `read`, and no process is
// created" can only be asserted where both are wired together, which is here.
//
// The command is `touch`, and the test looks for the file. "The toolset map
// does not contain bash" is a much weaker claim: it passes against an
// implementation that builds the tool anyway and checks a flag somewhere else.

// scriptedModel plays turns and records what it was asked.
type scriptedModel struct {
	turns    []modelTurn
	calls    int
	requests []model.ModelRequest
}

type modelTurn struct {
	text     string
	toolUses []model.Block
}

func (m *scriptedModel) Stream(ctx context.Context, req model.ModelRequest) (<-chan model.Delta, <-chan model.StreamResult, <-chan error) {
	m.requests = append(m.requests, req)
	index := m.calls
	m.calls++

	var turn modelTurn
	if index < len(m.turns) {
		turn = m.turns[index]
	} else {
		turn = modelTurn{text: `{"status":"ok","summary":"out of script"}`}
	}

	deltas := make(chan model.Delta)
	results := make(chan model.StreamResult, 1)
	errs := make(chan error, 1)

	go func() {
		defer close(deltas)
		defer close(results)
		defer close(errs)
		if turn.text != "" {
			deltas <- model.Delta{Kind: model.DeltaText, Text: turn.text}
		}
		for i := range turn.toolUses {
			block := turn.toolUses[i]
			deltas <- model.Delta{Kind: model.DeltaToolUse, Block: &block}
		}
		results <- model.StreamResult{Attempts: 1}
	}()

	return deltas, results, errs
}

// runRealStep wires the real runtime to the real toolset and runs one step.
func runRealStep(t *testing.T, root string, granted []string, turns ...modelTurn) (runtime.StepResult, *scriptedModel) {
	t.Helper()

	client := &scriptedModel{turns: turns}
	// S59 T5: a step that means to run a command needs an environment, and the
	// only honest source of one is the store's own doors — a binding, read by the
	// same resolver the daemon wires. A `StepRunner` without it builds a bash that
	// refuses to spawn.
	env := recoveryStoreBound(t)
	caller, specs, err := StepRunner{CommandVerdict: permissiveSeam(t), ExecResolver: newStepExecResolver(env)}.BuildTools(granted, root, "test-handler", runtime.FrozenManifest{})
	if err != nil {
		t.Fatalf("build the toolset: %v", err)
	}

	handler := runtime.DefaultHandler("do the thing", granted)
	compiled := StepRunner{}.Compile(context.Background(), runtime.CompileInput{
		Handler:       handler,
		Granted:       granted,
		Route:         model.Route{ID: "r", Provider: "anthropic", Model: "m", MaxOutputTokens: 1024},
		WorkspaceRoot: root,
		ToolSpecs:     specs,
	}, runtime.ContextDeps{})
	if !compiled.OK {
		t.Fatalf("compile: %s", compiled.Reason)
	}

	res := StepRunner{}.Run(context.Background(), compiled.Request, runtime.RunOpts{
		Client:      client,
		Tools:       caller,
		HandoffSpec: runtime.DefaultHandoffSpec(),
		Receipts:    testReceiptWriter{},
	})
	return res, client
}

type testReceiptWriter struct{}

func (testReceiptWriter) Begin(context.Context, string, string, string) error  { return nil }
func (testReceiptWriter) Complete(context.Context, string, string, bool) error { return nil }

func toolUse(id, name, input string) model.Block {
	return model.ToolUseBlock(id, name, json.RawMessage(input))
}

func TestAModelAskingForAnUngrantedBashCreatesNoProcess(t *testing.T) {
	// D1, end to end, by attempt. The step was granted `read`; the model asks
	// for `bash` with a command whose only effect would be to create a file.
	root := t.TempDir()
	sentinel := filepath.Join(root, "PROOF-BASH-RAN")

	res, client := runRealStep(t, root, []string{"read"},
		modelTurn{toolUses: []model.Block{toolUse("t1", "bash", `{"command":"touch PROOF-BASH-RAN"}`)}},
		modelTurn{text: "```json\n{\"status\":\"blocked\",\"summary\":\"no shell in this step\"}\n```"},
	)

	// THE assertion.
	if _, err := os.Stat(sentinel); err == nil {
		t.Fatal("THE COMMAND RAN. A step granted only `read` executed a shell command — the grant " +
			"list is not the boundary this slice documents")
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat the sentinel: %v", err)
	}

	// And the step CONTINUED: the model was told, answered, and the step
	// completed. A step that died on the refusal would be a step that failed
	// for a reason the model could have recovered from.
	if res.Outcome != runtime.OutcomeBlocked {
		t.Errorf("outcome %s, want blocked (reason %q)", res.Outcome, res.Reason)
	}
	if client.calls != 2 {
		t.Errorf("%d model calls; the step should have carried on after the refusal", client.calls)
	}

	// The refusal reached the model as a tool_result naming what IS available.
	var refusal string
	for _, msg := range res.Messages {
		for _, blk := range msg.Content {
			if blk.Kind == model.BlockToolResult && blk.IsError {
				refusal = blk.Content
			}
		}
	}
	if refusal == "" {
		t.Fatal("no error tool_result reached the model")
	}
	if !strings.Contains(refusal, "read") {
		t.Errorf("the refusal does not name what is available: %s", refusal)
	}

	// The model was never OFFERED bash either, which is the half a provider
	// enforces: a tool in the request is a tool the model is entitled to call.
	for _, spec := range client.requests[0].Tools {
		if spec.Name == "bash" {
			t.Error("the request offered bash to a step that was not granted it")
		}
	}
}

func TestAModelGrantedBashCanRunAProcessWhichIsTheControl(t *testing.T) {
	// Without this, the test above passes against a loop that refuses every
	// tool call — including the ones a step was granted.
	root := t.TempDir()
	sentinel := filepath.Join(root, "PROOF-BASH-RAN")

	res, _ := runRealStep(t, root, []string{"read", "bash"},
		modelTurn{toolUses: []model.Block{toolUse("t1", "bash", `{"command":"touch PROOF-BASH-RAN"}`)}},
		modelTurn{text: "```json\n{\"status\":\"ok\",\"summary\":\"touched it\"}\n```"},
	)

	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("a granted bash did not run the command: %v", err)
	}
	if res.Outcome != runtime.OutcomeOK {
		t.Errorf("outcome %s: %s", res.Outcome, res.Reason)
	}
}

func TestAReadThroughTheRealToolsetStaysInsideTheWorkspace(t *testing.T) {
	// The declared-file path and the tool path go through the SAME resolver
	// (W2/I1), and this is where that is wired: `Compile` closes `ReadFile`
	// over `tools.Resolve`.
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "inside.md"), []byte("INSIDE-SENTINEL"), 0o644); err != nil {
		t.Fatal(err)
	}

	outside := t.TempDir()
	secret := filepath.Join(outside, "secret")
	if err := os.WriteFile(secret, []byte("OUTSIDE-SENTINEL"), 0o600); err != nil {
		t.Fatal(err)
	}

	res, _ := runRealStep(t, root, []string{"read"},
		modelTurn{toolUses: []model.Block{
			toolUse("t1", "read", `{"path":"inside.md"}`),
			toolUse("t2", "read", `{"path":"../`+filepath.Base(outside)+`/secret"}`),
			toolUse("t3", "read", `{"path":"`+secret+`"}`),
		}},
		modelTurn{text: "```json\n{\"status\":\"ok\",\"summary\":\"read what I could\"}\n```"},
	)

	transcript := transcriptText(res)
	if !strings.Contains(transcript, "INSIDE-SENTINEL") {
		t.Error("the read inside the workspace did not work")
	}
	if strings.Contains(transcript, "OUTSIDE-SENTINEL") {
		t.Fatal("a file outside the workspace reached the transcript")
	}
}

func TestADeclaredContextFileCannotEscapeTheWorkspace(t *testing.T) {
	// Assembly reads through the same refusals a tool does, rather than
	// through `os.ReadFile` on a path somebody wrote in a handler.
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("OUTSIDE-SENTINEL"), 0o600); err != nil {
		t.Fatal(err)
	}

	handler := runtime.StepHandler{
		Name:            "escaper",
		Sections:        []runtime.SectionSpec{{Title: "Secret", DeclaredFile: "../" + filepath.Base(outside) + "/secret", Category: runtime.CategoryInjectedContext, Destination: runtime.SectionTranscript}},
		MaxOutputTokens: 1024,
	}
	compiled := StepRunner{}.Compile(context.Background(), runtime.CompileInput{
		Handler:       handler,
		Route:         model.Route{ID: "r", Provider: "anthropic", Model: "m", MaxOutputTokens: 1024},
		WorkspaceRoot: root,
	}, runtime.ContextDeps{})

	if compiled.OK {
		t.Fatal("a handler declaring a file outside the workspace compiled")
	}
	if strings.Contains(compiled.Reason, "OUTSIDE-SENTINEL") {
		t.Fatal("the file's contents reached the error message")
	}
	if !strings.Contains(compiled.Reason, "..") && !strings.Contains(compiled.Reason, "refused") {
		t.Errorf("the reason does not name the refusal: %s", compiled.Reason)
	}
}

func TestTheToolResultCarriesIsErrorThroughTheAdapter(t *testing.T) {
	// Two identical-looking result types, converted in this file. O3's whole
	// point is that a tool result which failed and one that succeeded must not
	// serialise identically — so the adapter is the place that could quietly
	// drop it.
	root := t.TempDir()

	res, _ := runRealStep(t, root, []string{"read"},
		modelTurn{toolUses: []model.Block{toolUse("t1", "read", `{"path":"does-not-exist.md"}`)}},
		modelTurn{text: "```json\n{\"status\":\"blocked\",\"summary\":\"no such file\"}\n```"},
	)

	var sawError bool
	for _, msg := range res.Messages {
		for _, blk := range msg.Content {
			if blk.Kind == model.BlockToolResult && blk.IsError {
				sawError = true
			}
		}
	}
	if !sawError {
		t.Error("a failed tool result reached the model with IsError false")
	}
}

func TestTheAdapterPreservesTheSummarySoAWatcherSeesWhatRan(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	var summaries []string
	client := &scriptedModel{turns: []modelTurn{
		{toolUses: []model.Block{toolUse("t1", "read", `{"path":"a.md"}`)}},
		{text: "```json\n{\"status\":\"ok\",\"summary\":\"read it\"}\n```"},
	}}
	caller, specs, err := (StepRunner{}).BuildTools([]string{"read"}, root, "test-handler", runtime.FrozenManifest{})
	if err != nil {
		t.Fatal(err)
	}
	compiled := StepRunner{}.Compile(context.Background(), runtime.CompileInput{
		Handler:       runtime.DefaultHandler("read a.md", []string{"read"}),
		Granted:       []string{"read"},
		Route:         model.Route{ID: "r", Provider: "anthropic", Model: "m", MaxOutputTokens: 1024},
		WorkspaceRoot: root,
		ToolSpecs:     specs,
	}, runtime.ContextDeps{})
	if !compiled.OK {
		t.Fatal(compiled.Reason)
	}

	StepRunner{}.Run(context.Background(), compiled.Request, runtime.RunOpts{
		Client: client,
		Tools:  caller,
		Sink: runtime.SinkFunc(func(e runtime.StepEvent) {
			if e.Kind == runtime.EventToolEnd {
				summaries = append(summaries, e.Summary+" "+e.Result)
			}
		}),
		HandoffSpec: runtime.DefaultHandoffSpec(),
	})

	if len(summaries) == 0 {
		t.Fatal("no tool.end event")
	}
	if !strings.Contains(summaries[0], "a.md") {
		t.Errorf("the event does not name the file: %q", summaries[0])
	}
}

func TestTheGrantedListSurvivesTheAdapter(t *testing.T) {
	caller, _, err := (StepRunner{}).BuildTools([]string{"read"}, "/workspace", "test-handler", runtime.FrozenManifest{})
	if err != nil {
		t.Fatal(err)
	}
	granted := caller.Granted()
	if len(granted) != 1 || granted[0] != "read" {
		t.Errorf("the adapter reports %v", granted)
	}
}

func TestBuildToolsRefusesAnUnknownNameRatherThanOmittingIt(t *testing.T) {
	if _, _, err := (StepRunner{}).BuildTools([]string{"read", "wrtie"}, "/workspace", "test-handler", runtime.FrozenManifest{}); err == nil {
		t.Fatal("an unknown tool name was accepted")
	}
}

func TestTheFourPackagesCannotSeeEachOther(t *testing.T) {
	// The seam this file exists to hold (I1/T11). Each package is a leaf for a
	// different reason: the runtime must persist nothing so S6 and S7 can hand
	// it transcripts, the tools must not depend on the loop so S11 can replace
	// their policy, and the protocol must import no transport so S19 can mount
	// it on HTTP.
	//
	// Asserted by import, because this is the one claim a reader cannot check
	// by reading one file.
	cases := []struct {
		pkg       string
		forbidden []string
	}{
		{"../runtime", []string{"internal/tools", "internal/store", "internal/protocol", "internal/daemon"}},
		{"../tools", []string{"internal/runtime", "internal/store", "internal/protocol", "internal/daemon"}},
		{"../store", []string{"internal/runtime", "internal/tools", "internal/protocol", "internal/daemon"}},
		{"../protocol", []string{"internal/store", "internal/tools", "internal/daemon"}},
	}

	for _, tc := range cases {
		t.Run(tc.pkg, func(t *testing.T) {
			imports := importsOf(t, tc.pkg)
			if len(imports) == 0 {
				t.Fatal("no imports found; this check would pass vacuously")
			}
			for _, bad := range tc.forbidden {
				for file, paths := range imports {
					for _, path := range paths {
						if strings.Contains(path, bad) {
							t.Errorf("%s/%s imports %s", tc.pkg, file, path)
						}
					}
				}
			}
		})
	}
}

// importsOf returns each non-test file's imports in a package directory.
func importsOf(t *testing.T, dir string) map[string][]string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	out := map[string][]string{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		out[name] = importPaths(string(src))
	}
	return out
}

// importPaths pulls the quoted paths out of the import block.
//
// A deliberately small parser rather than go/parser: this test runs over four
// directories and only needs the strings between the parentheses after
// `import`.
func importPaths(src string) []string {
	start := strings.Index(src, "\nimport (")
	if start < 0 {
		if single := strings.Index(src, "\nimport \""); single >= 0 {
			rest := src[single+9:]
			if end := strings.IndexByte(rest, '"'); end > 0 {
				return []string{rest[:end]}
			}
		}
		return nil
	}
	rest := src[start+9:]
	end := strings.Index(rest, "\n)")
	if end < 0 {
		return nil
	}
	var out []string
	for _, line := range strings.Split(rest[:end], "\n") {
		line = strings.TrimSpace(line)
		open := strings.IndexByte(line, '"')
		if open < 0 {
			continue
		}
		closing := strings.IndexByte(line[open+1:], '"')
		if closing < 0 {
			continue
		}
		out = append(out, line[open+1:open+1+closing])
	}
	return out
}

func transcriptText(res runtime.StepResult) string {
	var b strings.Builder
	for _, msg := range res.Messages {
		for _, blk := range msg.Content {
			b.WriteString(blk.Text)
			b.WriteString(blk.Content)
			b.WriteString("\n")
		}
	}
	return b.String()
}

func TestATimeoutInTheRealToolsetDoesNotTakeTheStepDown(t *testing.T) {
	// The real `bash`, a real timeout, through the real adapter: the step gets
	// a tool result and carries on.
	root := t.TempDir()

	client := &scriptedModel{turns: []modelTurn{
		{toolUses: []model.Block{toolUse("t1", "bash", `{"command":"sleep 30","timeoutSeconds":1}`)}},
		{text: "```json\n{\"status\":\"blocked\",\"summary\":\"the command timed out\"}\n```"},
	}}
	env := recoveryStoreBound(t) // S59 T5: the step's bash needs a bound environment
	caller, specs, err := StepRunner{CommandVerdict: permissiveSeam(t), ExecResolver: newStepExecResolver(env)}.BuildTools([]string{"bash"}, root, "test-handler", runtime.FrozenManifest{})
	if err != nil {
		t.Fatal(err)
	}
	compiled := StepRunner{}.Compile(context.Background(), runtime.CompileInput{
		Handler:       runtime.DefaultHandler("run it", []string{"bash"}),
		Granted:       []string{"bash"},
		Route:         model.Route{ID: "r", Provider: "anthropic", Model: "m", MaxOutputTokens: 1024},
		WorkspaceRoot: root,
		ToolSpecs:     specs,
	}, runtime.ContextDeps{})
	if !compiled.OK {
		t.Fatal(compiled.Reason)
	}

	start := time.Now()
	res := StepRunner{}.Run(context.Background(), compiled.Request, runtime.RunOpts{
		Client: client, Tools: caller, HandoffSpec: runtime.DefaultHandoffSpec(), Receipts: testReceiptWriter{},
	})
	if elapsed := time.Since(start); elapsed > 20*time.Second {
		t.Errorf("the step took %s; the tool's own timeout should have bounded it", elapsed)
	}
	if res.Outcome != runtime.OutcomeBlocked {
		t.Errorf("outcome %s: %s", res.Outcome, res.Reason)
	}
	if !strings.Contains(transcriptText(res), "TIMED OUT") {
		t.Error("the model was not told the command timed out")
	}
}

var _ = tools.DefaultGrant
