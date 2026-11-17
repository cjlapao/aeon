package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/database"
	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/runtime"
	"github.com/cjlapao/aeon/internal/store"
)

// T17's tests — the `todo` tool and the W20 property it must prove.
//
// The property is not "the code looks stateless"; it is three separate
// claims, each with its own test:
//
//  1. WITHIN STEP: one toolset instance carries the list; two toolsets (and
//     two Todo() calls) never share one.
//  2. THE TRANSCRIPT IS THE RECORD: every result — set and list alike —
//     carries the full rendered checklist, and the stored transcript of a
//     real step contains it as tool_result TEXT.
//  3. NEVER A ROW: a scripted step that used todo through a REAL store (a
//     throwaway XDG_STATE_HOME) leaves the task and loop tables byte-for-byte
//     the same count — and the counters are proved to be counters by planted
//     violations that DO move them.

// sixItemSetup is the spec's own example list: 3 pending, 1 in progress, 2
// done — so the pinned counts line is asserted with the pinned example.
const sixItemJSON = `{"action":"set","items":[` +
	`{"id":"t1","content":"gather requirements","status":"pending"},` +
	`{"id":"t2","content":"write the draft","status":"in_progress"},` +
	`{"id":"t3","content":"review the draft","status":"pending"},` +
	`{"id":"t4","content":"create the outline","status":"done"},` +
	`{"id":"t5","content":"publish","status":"pending"},` +
	`{"id":"t6","content":"collect sources","status":"done"}]}`

const (
	countsLineSix    = "3 pending, 1 in progress, 2 done"
	emptyCountsLine  = "0 pending, 0 in progress, 0 done"
	renderedSixLines = "[ ] \"t1\": gather requirements\n" +
		"[~] \"t2\": write the draft\n" +
		"[ ] \"t3\": review the draft\n" +
		"[x] \"t4\": create the outline\n" +
		"[ ] \"t5\": publish\n" +
		"[x] \"t6\": collect sources"
)

func todoCall(t *testing.T, set Toolset, name string, input string) Result {
	t.Helper()
	return set.Call(context.Background(), name, json.RawMessage(input))
}

func mustTodoToolset(t *testing.T) (Toolset, string) {
	t.Helper()
	root := t.TempDir()
	set, err := BuildToolset([]string{"todo"}, ToolContext{WorkspaceRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	return set, root
}

// --- within-step behaviour --------------------------------------------------

func TestTodoSetThenListRoundTripWithinOneToolset(t *testing.T) {
	set, _ := mustTodoToolset(t)

	res := todoCall(t, set, "todo", sixItemJSON)
	if res.IsError {
		t.Fatalf("a valid set was refused: %s", res.Content)
	}
	if !strings.Contains(res.Content, renderedSixLines) || !strings.Contains(res.Content, countsLineSix) {
		t.Errorf("the set result does not carry the rendered checklist:\n%s", res.Content)
	}

	again := todoCall(t, set, "todo", `{"action":"list"}`)
	if again.IsError {
		t.Fatalf("a valid list was refused: %s", again.Content)
	}
	if again.Content != res.Content {
		t.Errorf("list did not render the same checklist set returned:\n%s\n----\n%s", again.Content, res.Content)
	}
	for _, marker := range []string{"[ ] \"t1\"", "[~] \"t2\"", "[x] \"t4\""} {
		if !strings.Contains(again.Content, marker) {
			t.Errorf("missing marker line %q in:\n%s", marker, again.Content)
		}
	}
}

func TestTodoTwoToolsetsNeverShareTheList(t *testing.T) {
	// Per-step isolation: one Todo() call builds one toolset's instance, so
	// two BuildToolset calls must not share the slice. This is the regression
	// guard against the forbidden design — package-level state keyed by step
	// would keep this test red forever.
	first, _ := mustTodoToolset(t)
	if res := todoCall(t, first, "todo", sixItemJSON); res.IsError {
		t.Fatalf("setup set failed: %s", res.Content)
	}

	second, _ := mustTodoToolset(t)
	res := todoCall(t, second, "todo", `{"action":"list"}`)
	if res.IsError {
		t.Fatalf("list on a fresh toolset was refused: %s", res.Content)
	}
	if !strings.Contains(res.Content, "no items.") || !strings.Contains(res.Content, emptyCountsLine) {
		t.Errorf("a fresh toolset does not start EMPTY — the two toolsets share state:\n%s", res.Content)
	}

	// And the first one still has its own list.
	if back := todoCall(t, first, "todo", `{"action":"list"}`); !strings.Contains(back.Content, countsLineSix) {
		t.Errorf("the first toolset lost its list to the second:\n%s", back.Content)
	}
}

func TestTodoTwoTodoCallsNeverShareTheList(t *testing.T) {
	// The same isolation one level down: two direct Todo() calls are two
	// instances, not one hidden map.
	tc := ToolContext{WorkspaceRoot: t.TempDir()}
	a, b := Todo(), Todo()
	if res := a.Run(context.Background(), tc, json.RawMessage(sixItemJSON)); res.IsError {
		t.Fatalf("setup set failed: %s", res.Content)
	}
	res := b.Run(context.Background(), tc, json.RawMessage(`{"action":"list"}`))
	if res.IsError || !strings.Contains(res.Content, "no items.") {
		t.Errorf("two Todo() instances share state:\n%s", res.Content)
	}
}

func TestTodoSetReplacesTheWholeList(t *testing.T) {
	set, _ := mustTodoToolset(t)
	if res := todoCall(t, set, "todo", sixItemJSON); res.IsError {
		t.Fatalf("setup failed: %s", res.Content)
	}
	res := todoCall(t, set, "todo", `{"action":"set","items":[{"id":"only","content":"the one item","status":"in_progress"}]}`)
	if res.IsError {
		t.Fatalf("the replacing set was refused: %s", res.Content)
	}
	if res.Content != "[~] \"only\": the one item\n0 pending, 1 in progress, 0 done" {
		t.Errorf("set did not REPLACE the whole list:\n%s", res.Content)
	}
	if back := todoCall(t, set, "todo", `{"action":"list"}`); back.Content != res.Content {
		t.Errorf("the replacement did not stick — list renders:\n%s", back.Content)
	}
}

func TestTodoSetAcceptsAnExplicitEmptyListAndRefusesAnOmittedOne(t *testing.T) {
	set, _ := mustTodoToolset(t)
	if res := todoCall(t, set, "todo", sixItemJSON); res.IsError {
		t.Fatalf("setup failed: %s", res.Content)
	}
	// An explicit empty list clears: the model said "the list is now empty".
	if res := todoCall(t, set, "todo", `{"action":"set","items":[]}`); res.IsError {
		t.Fatalf("an explicit empty set was refused: %s", res.Content)
	} else if !strings.Contains(res.Content, "no items.") || !strings.Contains(res.Content, emptyCountsLine) {
		t.Errorf("an explicit empty set did not clear:\n%s", res.Content)
	}
	// An omitted items field has no meaning for set, and is refused, not
	// silently read as empty — an omission must never destroy the record.
	if res := todoCall(t, set, "todo", `{"action":"set"}`); !res.IsError ||
		!strings.Contains(res.Content, "requires an items list") {
		t.Errorf("an omitted items field was not refused as a refusal: %+v", res)
	}
}

// --- refusals ----------------------------------------------------------------

func TestTodoRefusesAnInvalidStatusNamingIt(t *testing.T) {
	// The pinned behaviour: "a bad status is a refusal naming it".
	cases := []struct {
		name   string
		status string
	}{
		{"unknown", "blocked"},
		{"wrong case", "PENDING"},
		{"empty", ""},
		{"todo-as-status", "todo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			set, _ := mustTodoToolset(t)
			// A known-good list first, so the refusal can be checked for
			// leaving it alone.
			if res := todoCall(t, set, "todo", sixItemJSON); res.IsError {
				t.Fatalf("setup failed: %s", res.Content)
			}
			bad := fmt.Sprintf(`{"action":"set","items":[`+
				`{"id":"ok1","content":"fine","status":"pending"},`+
				`{"id":"bad1","content":"wrong","status":%q}]}`, tc.status)
			res := todoCall(t, set, "todo", bad)
			if !res.IsError {
				t.Fatalf("status %q was accepted", tc.status)
			}
			if !strings.Contains(res.Content, fmt.Sprintf("status %q", tc.status)) {
				t.Errorf("the refusal does not name the value it refused: %s", res.Content)
			}
			if !strings.Contains(res.Content, "pending, in_progress, or done") {
				t.Errorf("the refusal does not say what the statuses are: %s", res.Content)
			}
			// All-or-nothing: the refused set must not have replaced anything.
			if back := todoCall(t, set, "todo", `{"action":"list"}`); !strings.Contains(back.Content, countsLineSix) {
				t.Errorf("a refused set mutated the list:\n%s", back.Content)
			}
		})
	}
}

func TestTodoRefusesMalformedArguments(t *testing.T) {
	big, err := json.Marshal(func() []todoItem {
		items := make([]todoItem, maxTodoItems+1)
		for i := range items {
			items[i] = todoItem{ID: fmt.Sprintf("i%d", i), Content: "c", Status: "pending"}
		}
		return items
	}())
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"not JSON", `{"action":`, "not valid JSON"},
		{"unknown action", `{"action":"clear"}`, `"clear"`},
		{"missing action", `{"items":[]}`, `""`},
		{"list with items", `{"action":"list","items":[]}`, "takes no items"},
		{"blank id", `{"action":"set","items":[{"id":"  ","content":"c","status":"pending"}]}`, "id is empty"},
		{"blank content", `{"action":"set","items":[{"id":"a","content":" ","status":"pending"}]}`, "content is empty"},
		{"oversized names the bound", fmt.Sprintf(`{"action":"set","items":%s}`, big), fmt.Sprintf("%d", maxTodoItems)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			set, _ := mustTodoToolset(t)
			res := todoCall(t, set, "todo", tc.input)
			if !res.IsError {
				t.Fatalf("%s was accepted: %s", tc.name, res.Content)
			}
			if !strings.Contains(res.Content, tc.want) {
				t.Errorf("the refusal does not name %q: %s", tc.want, res.Content)
			}
		})
	}
}

// --- the transcript is the record ---------------------------------------------

func TestTodoEveryResultCarriesTheRenderedList(t *testing.T) {
	// The spec pins the assertion on the CONTENT TEXT, not a struct: the
	// tool results in the transcript ARE the record, so both calls — not
	// just list — must repeat the full checklist.
	set, _ := mustTodoToolset(t)
	setRes := todoCall(t, set, "todo", sixItemJSON)
	listRes := todoCall(t, set, "todo", `{"action":"list"}`)
	for name, res := range map[string]Result{"set": setRes, "list": listRes} {
		if res.IsError {
			t.Fatalf("%s errored: %s", name, res.Content)
		}
		for _, line := range strings.Split(renderedSixLines, "\n") {
			if !strings.Contains(res.Content, line) {
				t.Errorf("the %s result is missing the rendered line %q:\n%s", name, line, res.Content)
			}
		}
		if !strings.HasSuffix(res.Content, countsLineSix) {
			t.Errorf("the %s result does not end with the counts line:\n%s", name, res.Content)
		}
	}
}

func TestTodoDescriptionTellsTheTruth(t *testing.T) {
	// The model is told, in the description it receives, what the list is and
	// how it ends. S75 (2026-11-07) reversed W20 for the loop: the list stays
	// active across steps until it is closed, so the old "this step only /
	// not a durable tracker" phrases are replaced by the closing rules.
	description := Todo().Spec.Description
	for _, phrase := range []string{
		"conversation or loop", "set replaces the whole list",
		"renders the checklist back", "active across steps",
		"empty set", "person closes it", "pending, in_progress, or done",
	} {
		if !strings.Contains(description, phrase) {
			t.Errorf("the description omits %q: %s", phrase, description)
		}
	}
}

func TestTodoIsGrantableButNotDefault(t *testing.T) {
	// Registered is not granted (the same split question/bash obey): the
	// tenth tool must not widen DefaultGrant.
	if _, ok := Registry()["todo"]; !ok {
		t.Fatal("todo missing from registry")
	}
	for _, name := range DefaultGrant() {
		if name == "todo" {
			t.Fatal("todo granted by default — registration is an allow-list decision, not a side effect")
		}
	}
	set, err := BuildToolset([]string{"todo"}, ToolContext{WorkspaceRoot: "/workspace"})
	if err != nil || !set.Has("todo") {
		t.Fatalf("cannot grant todo: %v", err)
	}
}

func TestTodoIsRerunDetectableThroughTheClassSeam(t *testing.T) {
	// The class seam reports rerun-detectable (class.go's vocabulary), and
	// the committed fixture carries the SAME class as a built entry, not a
	// pending one — TestRerunClassesMatchCommittedList already refuses a
	// present-and-pending entry, so the pair here is the whole claim.
	set, _ := mustTodoToolset(t)
	if got := set.RerunClass("todo"); got != RerunDetectable {
		t.Fatalf("RerunClass = %q, want %q", got, RerunDetectable)
	}
	entry, ok := loadClasses(t)["todo"]
	if !ok {
		t.Fatal("todo missing from the committed class list")
	}
	if entry.Pending {
		t.Error("the fixture still marks todo pending while Registry() holds it")
	}
	if entry.Class != RerunDetectable {
		t.Errorf("the committed fixture says %q, want %q", entry.Class, RerunDetectable)
	}
}

func TestTodoFileNeverTouchesTheDurableLayers(t *testing.T) {
	// The structural half of W20, in the two shapes the slice trusts:
	//
	// (a) the spec's own greps — `grep -n "store\." todo.go` (and the 'os.'
	//     sweep from the corpus-reader precedent) must come up EMPTY, which
	//     means not even a comment may carry the literals; and
	// (b) a go/parser walk pinning the import set and refusing package-level
	//     variables — a package-level map keyed by step would be a durable
	//     layer by another name (Learnings 2026-10-21: guards are AST walks,
	//     never text greps, and this test is the grep half ONLY because the
	//     literals it forbids cannot appear in the file it reads).
	src := packageSources(t)["todo.go"]
	if src == "" {
		t.Fatal("todo.go not found by packageSources")
	}
	for _, banned := range []string{"store.", "os."} {
		if strings.Contains(src, banned) {
			t.Errorf("todo.go contains %q — the W20 grep over the file must come up empty", banned)
		}
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "todo.go", src, 0)
	if err != nil {
		t.Fatalf("parse todo.go: %v", err)
	}
	allowed := map[string]bool{
		"context":                                true,
		"encoding/json":                          true,
		"errors":                                 true, // S75: errors.As for the stale-write refusal — a pure error package, same class as fmt
		"fmt":                                    true,
		"strconv":                                true, // T19 / SEC-T17-02: strconv.Quote for the render — a pure text package, same class as fmt
		"strings":                                true,
		"github.com/cjlapao/aeon/internal/model": true,
	}
	for _, imp := range file.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		if !allowed[path] {
			t.Errorf("todo.go imports %s — nothing in this file may reach a durable layer, the filesystem, or the environment", path)
		}
	}
	for _, decl := range file.Decls {
		if gd, ok := decl.(*ast.GenDecl); ok && gd.Tok == token.VAR {
			t.Errorf("todo.go declares a package-level var — package state keyed by step is the second memory W20 refuses")
		}
	}
	// The guard must not be able to pass against nothing: if these names ever
	// move, this test fails instead of grepping an empty string.
	if !strings.Contains(src, "func Todo()") {
		t.Fatal("todo.go no longer contains func Todo() — the file this test is about has moved")
	}
}

// --- the store proof ----------------------------------------------------------

// todoScriptTurn is one scripted model turn — the loop_test.go harness shape,
// rebuilt here because that fake lives inside internal/runtime.
type todoScriptTurn struct {
	text     string
	toolUses []model.Block
}

type todoScript struct {
	mu    sync.Mutex
	turns []todoScriptTurn
	calls int
}

func (c *todoScript) Stream(_ context.Context, _ model.ModelRequest) (<-chan model.Delta, <-chan model.StreamResult, <-chan error) {
	c.mu.Lock()
	i := c.calls
	c.calls++
	var turn todoScriptTurn
	if i < len(c.turns) {
		turn = c.turns[i]
	} else {
		turn = todoScriptTurn{text: "```json\n{\"status\":\"ok\",\"summary\":\"ran out of script\"}\n```"}
	}
	c.mu.Unlock()

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
		for j := range turn.toolUses {
			block := turn.toolUses[j]
			deltas <- model.Delta{Kind: model.DeltaToolUse, Block: &block}
		}
		results <- model.StreamResult{Attempts: 1}
	}()
	return deltas, results, errs
}

// todoCaller is the daemon/step.go adapter: two identical-looking result
// types converted at the seam.
type todoCaller struct{ set Toolset }

func (c todoCaller) Call(ctx context.Context, name string, input json.RawMessage) runtime.ToolOutcome {
	res := c.set.Call(ctx, name, input)
	return runtime.ToolOutcome{Content: res.Content, IsError: res.IsError, Summary: res.Summary}
}

func (c todoCaller) Granted() []string { return c.set.Granted() }

// todoCheckpoint wires the step to the REAL store, the way the daemon's
// recovery writer does (daemon/recover.go Save): every checkpoint this step
// takes crosses disk.
type todoCheckpoint struct {
	st     *store.Store
	stepID int64
}

func (c todoCheckpoint) Save(ctx context.Context, cp runtime.Checkpoint) error {
	raw, err := json.Marshal(cp.Manifest)
	if err != nil {
		return err
	}
	return c.st.SaveRunningCheckpoint(ctx, c.stepID, cp.Messages, raw, cp.Cost)
}

// countTodoRows counts a WHOLE table through the same file the step wrote
// to — an unfiltered COUNT(*), because a scoped read that filtered the rows
// away would count nothing and call it proof.
func countTodoRows(t *testing.T, path, table string) int {
	t.Helper()
	db, err := sql.Open(database.DriverName, "file:"+path+"?mode=ro&_busy_timeout=5000")
	if err != nil {
		t.Fatalf("open the file for counting: %v", err)
	}
	defer db.Close()
	var n int
	// The table name is a literal from this test, never a caller value —
	// there is nothing to parameterise and nothing else this file reads.
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		t.Fatalf("count the %s table: %v", table, err)
	}
	return n
}

func TestTodoNeverReachesTheStore(t *testing.T) {
	// W20's proof: the row count, not a code review. A scripted step whose
	// model emits a todo `set` and then a `list` runs through a real store
	// in a throwaway XDG_STATE_HOME. The step WRITES to that store (its
	// checkpoints and transcript land, and the assertions below read them
	// back), so the store is a live participant — and the task and loop
	// tables still gained nothing.
	xdg := t.TempDir()
	t.Setenv("XDG_STATE_HOME", xdg)
	t.Setenv("AEON_STATE_DIR", "")

	s, err := store.Open(store.Options{})
	if err != nil {
		t.Fatalf("open a real store at the throwaway XDG_STATE_HOME: %v", err)
	}
	defer s.Close()
	if !strings.HasPrefix(s.Path(), xdg) {
		t.Fatalf("the store resolved to %s, not under the throwaway XDG_STATE_HOME %s", s.Path(), xdg)
	}

	// Seed the baseline the counters count: a loop, one task under it, and
	// the running step the checkpoints belong to. "0 == 0" would prove
	// nothing; 1 == 1 against seeded rows is the same claim with teeth.
	componentRoot := t.TempDir()
	sc := store.Scope{LoopSlug: "todo-proof", ComponentRoot: componentRoot}
	if _, err := s.CreateLoop(store.NewLoop{Slug: sc.LoopSlug, Title: "todo proof",
		Goal: "prove the todo list never becomes rows", ComponentRoot: componentRoot}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask(sc, store.NewTask{TaskID: "T1", Ord: 1, Title: "the seeded baseline"}); err != nil {
		t.Fatal(err)
	}
	step, err := s.CreateStep(sc, store.NewStep{TaskID: "T1", Agent: "todo-proof"})
	if err != nil {
		t.Fatal(err)
	}
	beforeTask := countTodoRows(t, s.Path(), "task")
	beforeLoop := countTodoRows(t, s.Path(), "loop")
	if beforeTask != 1 || beforeLoop != 1 {
		t.Fatalf("baseline is task=%d loop=%d, want 1 and 1 — the harness itself is broken", beforeTask, beforeLoop)
	}

	set, root := mustTodoToolset(t)
	handler := runtime.StepHandler{
		Name:            "todo-proof-handler",
		System:          "you are a test",
		Sections:        []runtime.SectionSpec{{Title: "Task", Text: "keep a working list", Category: runtime.CategorySystemPrompt, Destination: runtime.SectionTranscript}},
		Tools:           []string{"todo"},
		HandoffSchema:   `{"type":"object","required":["status"]}`,
		MaxOutputTokens: 1024,
	}
	manifest := runtime.ContextManifest{MaxIterations: runtime.MaxIterations, DeadlineAt: time.Now().Add(time.Hour)}
	req := runtime.StepRequest{
		Handler:       handler,
		Messages:      runtime.OpeningTranscript(handler, "", "keep a working list", nil),
		Manifest:      manifest,
		Route:         model.Route{ID: "test", Provider: "anthropic", Model: "claude-test", MaxOutputTokens: 1024},
		Tools:         set.Specs(),
		WorkspaceRoot: root,
	}
	st := &runtime.LoopState{Request: req, Messages: req.Messages, Manifest: &req.Manifest}

	client := &todoScript{turns: []todoScriptTurn{
		{toolUses: []model.Block{
			model.ToolUseBlock("u1", "todo", json.RawMessage(sixItemJSON)),
			model.ToolUseBlock("u2", "todo", json.RawMessage(`{"action":"list"}`)),
		}},
		{text: "```json\n{\"status\":\"ok\",\"summary\":\"kept the list\"}\n```"},
	}}

	res := runtime.RunLoop(context.Background(), st, runtime.LoopDeps{
		Client:      client,
		Tools:       todoCaller{set: set},
		Checkpoint:  todoCheckpoint{st: s, stepID: step.ID},
		HandoffSpec: runtime.HandoffSpec{Required: []string{"status"}},
	})
	if res.Outcome != runtime.OutcomeOK {
		t.Fatalf("the scripted step did not complete: outcome %q, reason %q", res.Outcome, res.Reason)
	}

	afterTask := countTodoRows(t, s.Path(), "task")
	afterLoop := countTodoRows(t, s.Path(), "loop")
	if afterTask != beforeTask {
		t.Fatalf("W20 BROKEN: a step that used todo added %d task rows — the list must never become rows", afterTask-beforeTask)
	}
	if afterLoop != beforeLoop {
		t.Fatalf("W20 BROKEN: a step that used todo added %d loop rows", afterLoop-beforeLoop)
	}

	// Non-vacuity of the counters: plant a real violation and prove the
	// counts move (a counter that could not see a planted row made the
	// identical-count claim above meaningless).
	if _, err := s.CreateTask(sc, store.NewTask{TaskID: "T-planted", Ord: 2, Title: "planted control"}); err != nil {
		t.Fatal(err)
	}
	if n := countTodoRows(t, s.Path(), "task"); n != beforeTask+1 {
		t.Fatalf("the task counter did not notice a planted row (got %d, want %d) — the proof above proves nothing", n, beforeTask+1)
	}
	if _, err := s.CreateLoop(store.NewLoop{Slug: "planted-loop", Title: "planted", Goal: "control",
		ComponentRoot: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if n := countTodoRows(t, s.Path(), "loop"); n != beforeLoop+1 {
		t.Fatalf("the loop counter did not notice a planted row (got %d, want %d)", n, beforeLoop+1)
	}

	// The step really used the store: the transcript rows are on disk, and
	// BOTH tool results in them carry the rendered checklist as TEXT — the
	// transcript carries the state (the spec pins the assertion on content
	// text, not a struct).
	tr := s.ReadTranscript(step.ID)
	if !tr.OK || tr.Count == 0 {
		t.Fatalf("the step's transcript never reached the store: %+v", tr)
	}
	answers := map[string]int{}
	for _, msg := range tr.Messages {
		for _, b := range msg.Content {
			if b.Kind == model.BlockToolResult && (b.ToolUseID == "u1" || b.ToolUseID == "u2") {
				// A tool_result's payload is Block.Content (Block.Text is
				// the text-block field — model/types.go).
				if strings.Contains(b.Content, countsLineSix) && strings.Contains(b.Content, "[~] \"t2\": write the draft") {
					answers[b.ToolUseID]++
				}
			}
		}
	}
	if answers["u1"] != 1 || answers["u2"] != 1 {
		t.Fatalf("the stored transcript does not carry the rendered list as the tool results of u1/u2: %+v", answers)
	}

	// And the returned transcript agrees with the stored one (same record,
	// two views of it).
	sawReturned := 0
	for _, msg := range res.Messages {
		for _, b := range msg.Content {
			if b.Kind == model.BlockToolResult && strings.Contains(b.Content, countsLineSix) {
				sawReturned++
			}
		}
	}
	if sawReturned != 2 {
		t.Errorf("%d returned tool_results carry the rendered list, want 2", sawReturned)
	}
}

// --- T19: SEC-T17-01 bounds and SEC-T17-02 render quoting --------------------

func TestTodoRefusesACallPastTheByteBoundBeforeDecoding(t *testing.T) {
	// SEC-T17-01: the count gate fired only AFTER Unmarshal had built the
	// slice (~7× the raw bytes allocated on the way to a refusal). The byte
	// gate runs on len(input) BEFORE json.Unmarshal, so the refused payload
	// never reaches a decoder — and the refusal names the bound.
	set, _ := mustTodoToolset(t)
	huge := strings.Repeat("x", maxTodoInputBytes+1)
	res := todoCall(t, set, "todo", `{"action":"set","items":[{"id":"a","content":"`+huge+`","status":"pending"}]}`)
	if !res.IsError {
		t.Fatal("a call past the byte bound was accepted")
	}
	if !strings.Contains(res.Content, fmt.Sprintf("%d bytes", maxTodoInputBytes)) {
		t.Errorf("the refusal does not name the bound: %s", res.Content)
	}
	// And the list it refused to decode is untouched (all-or-nothing holds
	// for the new gate exactly as it holds for every other).
	back := todoCall(t, set, "todo", `{"action":"list"}`)
	if back.Content != "no items.\n"+emptyCountsLine {
		t.Errorf("the byte-gate refusal mutated the list:\n%s", back.Content)
	}
}

func TestTodoRefusesOversizedStringsNamingTheirBound(t *testing.T) {
	// SEC-T17-01's per-item half: id and content carried NO length at all.
	// Each oversized string is refused naming which item, which field, and
	// which bound — and stays under the call bound so this exercises the
	// item gate, not the byte gate.
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"content past the per-item bound",
			`{"action":"set","items":[{"id":"a","content":"` + strings.Repeat("y", maxTodoItemContentBytes+1) + `","status":"pending"}]}`,
			fmt.Sprintf("bound is %d", maxTodoItemContentBytes)},
		{"id past the per-item bound",
			`{"action":"set","items":[{"id":"` + strings.Repeat("z", maxTodoItemIDBytes+1) + `","content":"c","status":"pending"}]}`,
			fmt.Sprintf("bound is %d", maxTodoItemIDBytes)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			set, _ := mustTodoToolset(t)
			res := todoCall(t, set, "todo", tc.input)
			if !res.IsError {
				t.Fatalf("%s was accepted", tc.name)
			}
			if !strings.Contains(res.Content, tc.want) {
				t.Errorf("the refusal does not name %q: %s", tc.want, res.Content)
			}
		})
	}

	// The control: exactly-at-bound strings still set.
	set, _ := mustTodoToolset(t)
	res := todoCall(t, set, "todo", `{"action":"set","items":[{"id":"`+strings.Repeat("z", maxTodoItemIDBytes)+
		`","content":"`+strings.Repeat("y", maxTodoItemContentBytes)+`","status":"pending"}]}`)
	if res.IsError {
		t.Errorf("at-bound strings were refused: %s", res.Content)
	}
}

func TestTodoRenderCannotBeForgedIntoExtraLines(t *testing.T) {
	// SEC-T17-02: content "x\n[x] fake" used to render an EXTRA checklist
	// line — the model could forge lines into the record its own words
	// author. With quoting, the render is structurally items+1 lines no
	// matter what an item claims, the counts line stays the LAST line and
	// the only one of its kind, and an authored newline survives visibly
	// (escaped), not invisibly (as a line break).
	set, _ := mustTodoToolset(t)
	res := todoCall(t, set, "todo", `{"action":"set","items":[{"id":"real","content":"x\n[x] fake task\n0 pending, 9 in progress, 9 done","status":"pending"}]}`)
	if res.IsError {
		t.Fatalf("the forged content was refused instead of quoted: %s", res.Content)
	}
	lines := strings.Split(res.Content, "\n")
	if len(lines) != 2 {
		t.Fatalf("the render is %d lines, want exactly 2 (1 item + counts):\n%s", len(lines), res.Content)
	}
	if lines[1] != "1 pending, 0 in progress, 0 done" {
		t.Errorf("the counts line is not the authentic one: %q", lines[1])
	}
	if !strings.Contains(lines[0], `x\n[x] fake task\n0 pending, 9 in progress, 9 done`) {
		t.Errorf("the authored newlines were not escaped in place: %q", lines[0])
	}
	// An authored BACKSLASH escape cannot impersonate a real escaped
	// newline: the two contents must render differently.
	res2 := todoCall(t, set, "todo", `{"action":"set","items":[{"id":"real","content":"x\\n[x] fake","status":"pending"}]}`)
	if res2.IsError {
		t.Fatalf("the backslash control was refused: %s", res2.Content)
	}
	if strings.Contains(res2.Content, "x\n[x]") {
		t.Errorf("an authored backslash-n read as an escaped newline: %q", res2.Content)
	}
	if l := strings.Split(res2.Content, "\n"); len(l) != 2 {
		t.Errorf("the backslash control renders %d lines, want 2:\n%s", len(l), res2.Content)
	}
}

func TestTodoSchemaStatesTheBoundTheValidatorEnforces(t *testing.T) {
	// The one-formatter rule applied to bounds: the model reads the schema,
	// and a schema silent about a bound is a trap. The schema's maxItems and
	// the parser's constant must not drift (the handoff-schema-agreement
	// pattern, applied to todo).
	var schema struct {
		Properties struct {
			Items struct {
				MaxItems int `json:"maxItems"`
			} `json:"items"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(Todo().Spec.Parameters, &schema); err != nil {
		t.Fatal(err)
	}
	if schema.Properties.Items.MaxItems != maxTodoItems {
		t.Errorf("the schema says maxItems %d, the parser enforces %d", schema.Properties.Items.MaxItems, maxTodoItems)
	}
}
