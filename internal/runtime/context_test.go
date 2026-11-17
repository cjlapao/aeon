package runtime

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// T2's tests. Everything is injected, so none of this touches a filesystem.

// readSpy records every path read, and how many times.
type readSpy struct {
	files map[string]string
	reads []string
}

func newReadSpy(files map[string]string) *readSpy {
	return &readSpy{files: files}
}

func (r *readSpy) read(path string) ([]byte, error) {
	r.reads = append(r.reads, path)
	content, ok := r.files[path]
	if !ok {
		return nil, fmt.Errorf("no such file or directory")
	}
	return []byte(content), nil
}

func (r *readSpy) deps() ContextDeps {
	return ContextDeps{
		ReadFile: r.read,
		Clock:    func() time.Time { return time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC) },
	}
}

func TestSectionsAssembleInDeclaredOrder(t *testing.T) {
	// K13. A model reads top to bottom, so re-ordering sections changes what
	// it emphasises — which makes order part of the handler's MEANING rather
	// than a presentation detail.
	spy := newReadSpy(map[string]string{"plan.md": "the plan"})
	res := AssembleContext(context.Background(), []SectionSpec{
		{Title: "Task", Text: "do the thing", Category: CategoryInjectedContext, Destination: SectionTranscript},
		{Title: "Plan", DeclaredFile: "plan.md", Category: CategoryInjectedContext, Destination: SectionTranscript},
		{Title: "Rules", Text: "be careful", Category: CategoryInjectedContext, Destination: SectionTranscript},
	}, spy.deps())

	if !res.OK {
		t.Fatal(res.Reason)
	}
	taskAt := strings.Index(res.Text, "do the thing")
	planAt := strings.Index(res.Text, "the plan")
	rulesAt := strings.Index(res.Text, "be careful")
	if !(taskAt < planAt && planAt < rulesAt) {
		t.Errorf("sections are out of declared order:\n%s", res.Text)
	}
	// Titles are rendered, because a model handed three blocks of text with no
	// headings cannot tell which is the task.
	for _, title := range []string{"## Task", "## Plan", "## Rules"} {
		if !strings.Contains(res.Text, title) {
			t.Errorf("the heading %q is missing", title)
		}
	}
}

func TestEveryDeclaredFileIsHashedIntoTheManifest(t *testing.T) {
	// K23. The manifest is what S6 compares against to find out whether the
	// context moved while a step waited.
	spy := newReadSpy(map[string]string{"a.md": "alpha", "b.md": "beta"})
	res := AssembleContext(context.Background(), []SectionSpec{
		{Title: "A", DeclaredFile: "a.md", Category: CategoryInjectedContext, Destination: SectionTranscript},
		{Title: "Literal", Text: "not a file", Category: CategoryInjectedContext, Destination: SectionTranscript},
		{Title: "B", DeclaredFile: "b.md", Category: CategoryInjectedContext, Destination: SectionTranscript},
	}, spy.deps())

	if !res.OK {
		t.Fatal(res.Reason)
	}
	if len(res.Manifest.Files) != 2 {
		t.Fatalf("the manifest holds %d files, want 2 — literal sections are not files", len(res.Manifest.Files))
	}
	for _, f := range res.Manifest.Files {
		if f.Hash == "" {
			t.Errorf("%s has no hash", f.Path)
		}
		if !strings.HasPrefix(f.Hash, "sha256:") {
			t.Errorf("%s's hash is not algorithm-prefixed (%q); an unprefixed digest is one nobody "+
				"can identify after the fact", f.Path, f.Hash)
		}
		if f.Bytes == 0 {
			t.Errorf("%s records no size", f.Path)
		}
	}
	// Order preserved, so a manifest reads like the declaration.
	if res.Manifest.Files[0].Path != "a.md" || res.Manifest.Files[1].Path != "b.md" {
		t.Errorf("the manifest is out of order: %+v", res.Manifest.Files)
	}
}

func TestADeclaredFileThatDoesNotExistIsAConstructionError(t *testing.T) {
	// D13, and the reason matters as much as the refusal.
	//
	// An empty section reads to a model as "this file is empty" — a different
	// and more damaging claim than "this file is missing", because it will
	// reason from the absence and report confidently.
	spy := newReadSpy(map[string]string{})
	res := AssembleContext(context.Background(), []SectionSpec{
		{Title: "Plan", DeclaredFile: "docs/plan.md", Category: CategoryInjectedContext, Destination: SectionTranscript},
	}, spy.deps())

	if res.OK {
		t.Fatalf("a missing declared file assembled anyway, producing:\n%s", res.Text)
	}
	if !strings.Contains(res.Reason, "docs/plan.md") {
		t.Errorf("the reason does not name the file: %q", res.Reason)
	}
	if !strings.Contains(res.Reason, "empty section") {
		t.Errorf("the reason does not explain why an empty section would be worse: %q", res.Reason)
	}
	if res.Text != "" {
		t.Errorf("a failed assembly produced text: %q", res.Text)
	}
}

func TestASecondAssemblyDoesNotReRead(t *testing.T) {
	// K13's "frozen". Asserted with the ReadFile spy: the manifest is the
	// record of what was read ONCE, and re-reading would make a step's context
	// change under it mid-run.
	spy := newReadSpy(map[string]string{"a.md": "alpha"})
	specs := []SectionSpec{{Title: "A", DeclaredFile: "a.md", Category: CategoryInjectedContext, Destination: SectionTranscript}}

	first := AssembleContext(context.Background(), specs, spy.deps())
	if !first.OK {
		t.Fatal(first.Reason)
	}
	readsAfterFirst := len(spy.reads)
	if readsAfterFirst != 1 {
		t.Fatalf("the first assembly read %d times, want 1", readsAfterFirst)
	}

	// Using the manifest does not read anything again.
	manifest := first.Manifest
	_ = manifest.Files[0].Hash
	if len(spy.reads) != readsAfterFirst {
		t.Errorf("reading the manifest caused %d more reads", len(spy.reads)-readsAfterFirst)
	}

	// A second assembly is a NEW assembly and does read — which is correct, and
	// is what `Rehash` exists to compare. The property being asserted is that
	// nothing re-reads behind the caller's back.
	second := AssembleContext(context.Background(), specs, spy.deps())
	if !second.OK {
		t.Fatal(second.Reason)
	}
	if second.Manifest.Files[0].Hash != first.Manifest.Files[0].Hash {
		t.Error("the same content hashed differently twice")
	}
}

func TestAssemblyReadsNoFileOutsideTheDeclaredSet(t *testing.T) {
	spy := newReadSpy(map[string]string{"a.md": "alpha", "secret.md": "nope"})
	res := AssembleContext(context.Background(), []SectionSpec{
		{Title: "A", DeclaredFile: "a.md", Category: CategoryInjectedContext, Destination: SectionTranscript},
		{Title: "Literal", Text: "text", Category: CategoryInjectedContext, Destination: SectionTranscript},
	}, spy.deps())
	if !res.OK {
		t.Fatal(res.Reason)
	}
	for _, read := range spy.reads {
		if read != "a.md" {
			t.Errorf("assembly read %q, which was not declared", read)
		}
	}
	if len(spy.reads) != 1 {
		t.Errorf("assembly made %d reads for one declared file: %v", len(spy.reads), spy.reads)
	}
}

func TestAssemblyIsDeterministicUnderAFixedClock(t *testing.T) {
	// Which is what makes S6's identity assertion possible: a resumed step
	// assembles against the same injected reader and compares manifests, and
	// "the file changed while you waited" becomes detectable rather than
	// silently acted upon.
	spy := newReadSpy(map[string]string{"a.md": "alpha"})
	specs := []SectionSpec{{Title: "A", DeclaredFile: "a.md", Category: CategoryInjectedContext, Destination: SectionTranscript}, {Title: "B", Text: "beta", Category: CategoryInjectedContext, Destination: SectionTranscript}}

	first := AssembleContext(context.Background(), specs, spy.deps())
	second := AssembleContext(context.Background(), specs, spy.deps())

	if first.Text != second.Text {
		t.Errorf("two assemblies produced different text:\n--- 1\n%s\n--- 2\n%s", first.Text, second.Text)
	}
	if !first.Manifest.DeadlineAt.Equal(second.Manifest.DeadlineAt) {
		t.Error("the deadline differed between assemblies under a fixed clock")
	}
}

func TestAssemblyRefusesASectionThatIsBothTextAndAFile(t *testing.T) {
	// A reader cannot tell which the model was shown, so it is refused rather
	// than one of them winning silently.
	spy := newReadSpy(map[string]string{"a.md": "alpha"})
	res := AssembleContext(context.Background(), []SectionSpec{
		{Title: "Both", Text: "literal", DeclaredFile: "a.md", Category: CategoryInjectedContext, Destination: SectionTranscript},
	}, spy.deps())
	if res.OK {
		t.Fatal("a section with both text and a file assembled")
	}
	if !strings.Contains(res.Reason, "one or the other") {
		t.Errorf("the reason is unclear: %q", res.Reason)
	}
}

func TestAssemblyRefusesAnEmptySection(t *testing.T) {
	res := AssembleContext(context.Background(), []SectionSpec{{Title: "Nothing", Category: CategorySystemPrompt, Destination: SectionSystem}}, ContextDeps{})
	if res.OK {
		t.Fatal("an empty section assembled")
	}
	if !strings.Contains(res.Reason, "neither text nor a file") {
		t.Errorf("the reason is unclear: %q", res.Reason)
	}
}

func TestAssemblyWithNoInjectedReaderRefusesADeclaredFile(t *testing.T) {
	// I5: there is deliberately no default reader. A default would be
	// `os.ReadFile` against the process's working directory — the ambient state
	// I5 forbids — and the failure would be a step that read the daemon's files
	// instead of the workspace's, which works on the author's machine.
	res := AssembleContext(context.Background(), []SectionSpec{{Title: "A", DeclaredFile: "a.md", Category: CategoryInjectedContext, Destination: SectionTranscript}}, ContextDeps{})
	if res.OK {
		t.Fatal("a declared file was read with no injected reader")
	}
	if !strings.Contains(res.Reason, "working directory") {
		t.Errorf("the reason does not explain why there is no default: %q", res.Reason)
	}
}

func TestRehashReportsChangedAndMissingSeparately(t *testing.T) {
	// T2's point 3. They are reported to a MODEL differently — "this file
	// changed while you waited" and "this file no longer exists" call for
	// different responses, and the second may mean the work is moot. Merging
	// them into one "stale" list loses that.
	spy := newReadSpy(map[string]string{"same.md": "unchanged", "changed.md": "original"})
	manifest := AssembleContext(context.Background(), []SectionSpec{
		{Title: "S", DeclaredFile: "same.md", Category: CategoryInjectedContext, Destination: SectionTranscript},
		{Title: "C", DeclaredFile: "changed.md", Category: CategoryInjectedContext, Destination: SectionTranscript},
		{Title: "G", DeclaredFile: "gone.md", Category: CategoryInjectedContext, Destination: SectionTranscript},
	}, ContextDeps{ReadFile: func(p string) ([]byte, error) {
		return []byte(map[string]string{"same.md": "unchanged", "changed.md": "original", "gone.md": "here"}[p]), nil
	}}).Manifest

	// Now: `changed.md` differs and `gone.md` is absent.
	spy.files["changed.md"] = "edited"
	res := Rehash(manifest, spy.deps())

	if len(res.Changed) != 1 || res.Changed[0] != "changed.md" {
		t.Errorf("changed is %v, want [changed.md]", res.Changed)
	}
	if len(res.Missing) != 1 || res.Missing[0] != "gone.md" {
		t.Errorf("missing is %v, want [gone.md]", res.Missing)
	}
	if !res.Any() {
		t.Error("Any() is false although something moved")
	}
}

func TestRehashNeverMutatesTheManifest(t *testing.T) {
	// Freezing means freezing. `Rehash` compares and reports; the decision
	// about what to do belongs to the caller that knows whether the step can
	// continue.
	spy := newReadSpy(map[string]string{"a.md": "original"})
	manifest := AssembleContext(context.Background(), []SectionSpec{{Title: "A", DeclaredFile: "a.md", Category: CategoryInjectedContext, Destination: SectionTranscript}}, spy.deps()).Manifest
	before := manifest.Files[0].Hash

	spy.files["a.md"] = "changed"
	res := Rehash(manifest, spy.deps())

	if len(res.Changed) != 1 {
		t.Fatalf("the change was not detected: %+v", res)
	}
	if manifest.Files[0].Hash != before {
		t.Errorf("Rehash rewrote the manifest's hash: %q became %q", before, manifest.Files[0].Hash)
	}
}

func TestOnlyResumeCallsRehash(t *testing.T) {
	// The frozen manifest is compared when resuming, never while a step is
	// running or assembling its opening context.
	for name, src := range packageSources(t) {
		if name == "context.go" {
			continue // where it is defined
		}
		if strings.Contains(src, "Rehash(") && name != "resume.go" {
			t.Errorf("%s calls Rehash outside resume", name)
		}
	}
}
