package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeDocs is an in-memory DocStore: one loop's documents plus the goal's.
type fakeDocs struct {
	own   map[string]DocEntry
	text  map[string]string
	goal  map[string]string // name -> content, owned by goal-1, written by loop "brain"
	nrevs map[string]int
}

func newFakeDocs() *fakeDocs {
	return &fakeDocs{own: map[string]DocEntry{}, text: map[string]string{}, goal: map[string]string{}, nrevs: map[string]int{}}
}

func (f *fakeDocs) Write(_ context.Context, name, content string) (DocEntry, bool, error) {
	if strings.Contains(name, "..") {
		return DocEntry{}, false, &DocRefused{Reason: "a document name cannot contain a .. segment"}
	}
	if f.text[name] == content && f.nrevs[name] > 0 {
		return f.own[name], true, nil
	}
	f.nrevs[name]++
	f.text[name] = content
	f.own[name] = DocEntry{Name: name, Rev: f.nrevs[name], Bytes: len(content)}
	return f.own[name], false, nil
}

func (f *fakeDocs) Read(_ context.Context, name string) (DocEntry, string, error) {
	if e, ok := f.own[name]; ok {
		return e, f.text[name], nil
	}
	if c, ok := f.goal[name]; ok {
		return DocEntry{Name: name, Rev: 1, Bytes: len(c), Loop: "brain", Goal: "goal-1", Inherited: true}, c, nil
	}
	return DocEntry{}, "", &DocRefused{Reason: "this loop has no " + name}
}

func (f *fakeDocs) List(context.Context) ([]DocEntry, error) {
	var out []DocEntry
	for _, e := range f.own {
		out = append(out, e)
	}
	for n, c := range f.goal {
		if _, shadowed := f.own[n]; !shadowed {
			out = append(out, DocEntry{Name: n, Rev: 1, Bytes: len(c), Loop: "brain", Goal: "goal-1", Inherited: true})
		}
	}
	return out, nil
}

// Claim: the fake claims plan.md and anything under .projects/.
func (f *fakeDocs) Claim(_ context.Context, rel string) (string, bool) {
	if rel == "plan.md" || rel == "tasks/T1.md" {
		return rel, true
	}
	if rest, ok := strings.CutPrefix(rel, ".projects/loop-x/"); ok {
		return rest, true
	}
	return "", false
}

func runDoc(t *testing.T, tool Tool, docs DocStore, in string) Result {
	t.Helper()
	return tool.Run(context.Background(), ToolContext{Docs: docs}, json.RawMessage(in))
}

func TestDocWriteFilesARevisionAndSaysWhenNothingChanged(t *testing.T) {
	d := newFakeDocs()
	r := runDoc(t, DocWrite(), d, `{"name":"plan.md","content":"one"}`)
	if r.IsError || !strings.Contains(r.Content, "Filed plan.md rev 1") {
		t.Fatalf("first write: %+v", r)
	}
	r = runDoc(t, DocWrite(), d, `{"name":"plan.md","content":"two"}`)
	if !strings.Contains(r.Content, "rev 2") {
		t.Fatalf("second write: %+v", r)
	}
	r = runDoc(t, DocWrite(), d, `{"name":"plan.md","content":"two"}`)
	if r.IsError || !strings.Contains(r.Content, "nothing was written") {
		t.Fatalf("same content: %+v", r)
	}
}

func TestDocToolsRefuseWithTheStoresReasonAndNeverTakeALoop(t *testing.T) {
	d := newFakeDocs()
	if r := runDoc(t, DocWrite(), d, `{"name":"../x.md","content":"x"}`); !r.IsError || !strings.Contains(r.Content, "refused: a document name cannot contain a ..") {
		t.Fatalf("traversal: %+v", r)
	}
	// The model cannot address a loop or a goal: an unknown field is an error.
	for _, in := range []string{`{"name":"a.md","content":"x","loop":"other"}`, `{"name":"a.md","goal":"g"}`} {
		for _, tool := range []Tool{DocWrite(), DocRead()} {
			if r := runDoc(t, tool, d, in); !r.IsError {
				t.Fatalf("%s accepted %s: %+v", tool.Spec.Name, in, r)
			}
		}
	}
	if r := runDoc(t, DocList(), d, `{"loop":"other"}`); !r.IsError {
		t.Fatalf("doc_list took a field: %+v", r)
	}
}

func TestDocReadFallsBackToTheGoalsDocumentAndSaysWhoseItIs(t *testing.T) {
	d := newFakeDocs()
	d.goal["brief.md"] = "line1\nline2\nline3"
	r := runDoc(t, DocRead(), d, `{"name":"brief.md"}`)
	if r.IsError || !strings.Contains(r.Content, "written by loop brain for goal goal-1") || !strings.Contains(r.Content, "line3") {
		t.Fatalf("read: %+v", r)
	}
	r = runDoc(t, DocRead(), d, `{"name":"brief.md","offset":2,"limit":1}`)
	if !strings.HasSuffix(strings.Split(r.Content, "\n")[1], "line2") || !strings.Contains(r.Content, "call again with offset 3") {
		t.Fatalf("paged read: %q", r.Content)
	}
}

func TestDocEditChangesExactlyOneSpanAndRefusesTheGoalsDocument(t *testing.T) {
	d := newFakeDocs()
	d.goal["brief.md"] = "inherited"
	runDoc(t, DocWrite(), d, `{"name":"plan.md","content":"a b a"}`)

	if r := runDoc(t, DocEdit(), d, `{"name":"plan.md","old":"a","new":"z"}`); !r.IsError || !strings.Contains(r.Content, "occurs 2 times") {
		t.Fatalf("ambiguous: %+v", r)
	}
	if r := runDoc(t, DocEdit(), d, `{"name":"plan.md","old":"q","new":"z"}`); !r.IsError || !strings.Contains(r.Content, "not found") {
		t.Fatalf("absent: %+v", r)
	}
	if r := runDoc(t, DocEdit(), d, `{"name":"plan.md","old":"b","new":"c"}`); r.IsError || d.text["plan.md"] != "a c a" {
		t.Fatalf("one span: %+v %q", r, d.text["plan.md"])
	}
	if r := runDoc(t, DocEdit(), d, `{"name":"plan.md","old":"a","new":"z","replace_all":true}`); r.IsError || d.text["plan.md"] != "z c z" {
		t.Fatalf("replace_all: %+v %q", r, d.text["plan.md"])
	}
	if r := runDoc(t, DocEdit(), d, `{"name":"brief.md","old":"inherited","new":"mine"}`); !r.IsError || !strings.Contains(r.Content, "belongs to goal goal-1") {
		t.Fatalf("inherited: %+v", r)
	}
}

func TestDocListSeparatesTheLoopsDocumentsFromTheGoals(t *testing.T) {
	d := newFakeDocs()
	if r := runDoc(t, DocList(), d, `{}`); r.Content != "No documents yet." {
		t.Fatalf("empty: %+v", r)
	}
	d.goal["brief.md"] = "b"
	runDoc(t, DocWrite(), d, `{"name":"plan.md","content":"p"}`)
	r := runDoc(t, DocList(), d, ``)
	own := strings.Index(r.Content, "This loop's documents:")
	goal := strings.Index(r.Content, "Owned by the goal this loop works:")
	if own < 0 || goal < own || !strings.Contains(r.Content, "plan.md rev 1") || !strings.Contains(r.Content, "brief.md rev 1") {
		t.Fatalf("list: %q", r.Content)
	}
}

func TestDocToolsFailClosedWithoutAStore(t *testing.T) {
	inputs := map[string]string{
		"doc_write": `{"name":"a.md","content":"x"}`,
		"doc_read":  `{"name":"a.md"}`,
		"doc_edit":  `{"name":"a.md","old":"a","new":"b"}`,
		"doc_list":  `{}`,
	}
	for _, tool := range []Tool{DocWrite(), DocRead(), DocEdit(), DocList()} {
		r := tool.Run(context.Background(), ToolContext{}, json.RawMessage(inputs[tool.Spec.Name]))
		if !r.IsError || !strings.Contains(r.Content, "unavailable") {
			t.Fatalf("%s without a store: %+v", tool.Spec.Name, r)
		}
	}
}

// The net: write, edit and read on a claimed path act on the database, leave
// no file in the workspace, and raise no workspace-write signal.
func TestWriteEditAndReadOnAClaimedPathGoToTheDatabase(t *testing.T) {
	d := newFakeDocs()
	root := t.TempDir()
	tc := ToolContext{WorkspaceRoot: root, Docs: d}

	w := Write().Run(context.Background(), tc, json.RawMessage(`{"path":"plan.md","content":"step one\nstep two"}`))
	if w.IsError || !strings.Contains(w.Content, "filed in the database as rev 1") || w.Path != "" {
		t.Fatalf("routed write: %+v", w)
	}
	if _, err := os.Stat(filepath.Join(root, "plan.md")); !os.IsNotExist(err) {
		t.Fatalf("a claimed path must not land on disk: %v", err)
	}
	e := Edit().Run(context.Background(), tc, json.RawMessage(`{"path":"plan.md","old_string":"one","new_string":"1"}`))
	if e.IsError || d.text["plan.md"] != "step 1\nstep two" {
		t.Fatalf("routed edit: %+v %q", e, d.text["plan.md"])
	}
	r := Read().Run(context.Background(), tc, json.RawMessage(`{"path":"plan.md","offset":2}`))
	if r.IsError || !strings.Contains(r.Content, "step two") || strings.Contains(r.Content, "step 1") {
		t.Fatalf("routed read: %+v", r)
	}
	// .projects/{slug}/ is never written to again: it is the same document.
	p := Write().Run(context.Background(), tc, json.RawMessage(`{"path":".projects/loop-x/tasks/T1.md","content":"t"}`))
	if p.IsError || d.text["tasks/T1.md"] != "t" {
		t.Fatalf(".projects write: %+v %v", p, d.text)
	}
	if _, err := os.Stat(filepath.Join(root, ".projects")); !os.IsNotExist(err) {
		t.Fatalf(".projects must stay absent: %v", err)
	}
}

func TestAnUnclaimedPathStaysTheWorkspacesAndNoStoreMeansNoRouting(t *testing.T) {
	d := newFakeDocs()
	root := t.TempDir()
	tc := ToolContext{WorkspaceRoot: root, Docs: d}
	w := Write().Run(context.Background(), tc, json.RawMessage(`{"path":"README.md","content":"hello"}`))
	if w.IsError || w.Path != "README.md" {
		t.Fatalf("unclaimed write: %+v", w)
	}
	if b, err := os.ReadFile(filepath.Join(root, "README.md")); err != nil || string(b) != "hello" {
		t.Fatalf("the file is on disk: %q %v", b, err)
	}
	if len(d.own) != 0 {
		t.Fatalf("nothing is filed for an unclaimed path: %+v", d.own)
	}
	// Without a store the old behaviour is untouched, whatever the name.
	bare := ToolContext{WorkspaceRoot: root}
	if w := Write().Run(context.Background(), bare, json.RawMessage(`{"path":"plan.md","content":"x"}`)); w.IsError || w.Path != "plan.md" {
		t.Fatalf("no store, plain write: %+v", w)
	}
}
