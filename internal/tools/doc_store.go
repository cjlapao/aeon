package tools

// doc_store.go — the seam under the `doc_*` tools.
//
// A step's deliverables (plan.md, legal.md, brief.md, tasks/T1.md …) are rows
// in the database, not files in the workspace. The tools package still must not
// reach a store, so the rows arrive the way every step-scoped door does: a
// field of the step's ToolContext, filled by the daemon, the step's loop read
// from the context the runtime stamps at dispatch (stepctx) — never from the
// model's arguments. A model names a document; it can neither name a loop nor
// reach one it does not work.

import (
	"context"
	"fmt"
)

// DocEntry is one document revision as the tools speak of it.
type DocEntry struct {
	Name      string
	Rev       int
	Bytes     int
	SHA256    string
	WrittenBy string
	WrittenAt string
	// Loop is the loop that wrote the revision. It differs from the step's own
	// loop when the document is the goal's, written by an earlier loop.
	Loop string
	// Goal is the goal that owns the document ("" while none does).
	Goal string
	// Inherited marks a document the step's loop did not write: the goal it
	// works owns it, and an earlier loop (a brainstorm) wrote it.
	Inherited bool
}

// DocRefused is a refusal the model can act on: a name outside the allowed set,
// a document too large, an edit that matched nothing. It is the tool's answer,
// not an infrastructure failure.
type DocRefused struct{ Reason string }

func (e *DocRefused) Error() string { return e.Reason }

// DocStore is the step's door to its loop's documents.
type DocStore interface {
	// Write files a new revision of name. unchanged is true when the content
	// matches the head and nothing was stored.
	Write(ctx context.Context, name, content string) (entry DocEntry, unchanged bool, err error)
	// Read answers the head of name: the step's own loop first, then the goal
	// the loop works. A name neither holds is a *DocRefused.
	Read(ctx context.Context, name string) (entry DocEntry, content string, err error)
	// List answers the head of every document the step can read: its own loop's,
	// then the goal's it did not write itself.
	List(ctx context.Context) ([]DocEntry, error)
	// Claim answers whether a workspace-relative, slash-separated path names one
	// of this loop's deliverables — a name its blueprint declares in `produces:`
	// or a path under .projects/{slug}/ — and the document name it stands for.
	// `write`, `edit` and `read` route a claimed path here instead of the disk.
	Claim(ctx context.Context, rel string) (name string, claimed bool)
}

// docsOrFail is the fail-closed reading of the seam, the shape recordsOrFail
// gives the records door: a runtime with no store says so, and never answers
// "no documents" from an injected nothing.
func docsOrFail(tc ToolContext) (DocStore, *Result) {
	if tc.Docs == nil {
		r := Errorf("documents are unavailable in this runtime: this daemon has no store to file them in")
		return nil, &r
	}
	return tc.Docs, nil
}

func describeEntry(e DocEntry) string {
	s := fmt.Sprintf("%s rev %d (%d bytes)", e.Name, e.Rev, e.Bytes)
	if e.Inherited {
		s += fmt.Sprintf(", written by loop %s for goal %s", e.Loop, e.Goal)
	}
	return s
}
