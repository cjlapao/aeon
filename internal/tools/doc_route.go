package tools

// doc_route.go — the net under a weaker model: `write`, `edit` and `read` on a
// path the loop's blueprint declares as a deliverable (or that lives under
// .projects/{slug}/) act on the database, not the disk. The `doc_*` tools are
// the front door; this is for the model that reaches for `write` anyway, so a
// deliverable never lands in the project root whichever tool it used.
//
// The daemon decides what is claimed (DocStore.Claim, from the loop's pinned
// blueprint); a path it does not claim — source code, a README — is the
// workspace's, untouched.

import (
	"context"
	"fmt"
	"path/filepath"
)

// claimedDocument reports the document a workspace path stands for, when the
// step's loop claims it.
func claimedDocument(ctx context.Context, tc ToolContext, resolved ResolveResult) (DocStore, string, bool) {
	if tc.Docs == nil || !resolved.OK {
		return nil, "", false
	}
	name, ok := tc.Docs.Claim(ctx, filepath.ToSlash(resolved.Rel))
	if !ok {
		return nil, "", false
	}
	return tc.Docs, name, true
}

func routedWrite(ctx context.Context, docs DocStore, name, content string) Result {
	entry, unchanged, err := docs.Write(ctx, name, content)
	if err != nil {
		return docFailure(err)
	}
	if unchanged {
		return Result{Content: fmt.Sprintf("%s already holds exactly this content (rev %d); it lives in the database, not the workspace.", entry.Name, entry.Rev),
			Summary: "write " + entry.Name + " · database"}
	}
	// No Path: nothing was written to the workspace, so no workspace-write
	// signal is raised.
	return Result{Content: fmt.Sprintf("wrote %s (%d bytes) — filed in the database as rev %d, not in the workspace. Read it back with doc_read.", entry.Name, entry.Bytes, entry.Rev),
		Summary: "write " + entry.Name + " · database"}
}

func routedEdit(ctx context.Context, docs DocStore, name, old, replacement string, replaceAll bool) Result {
	entry, content, err := docs.Read(ctx, name)
	if err != nil {
		return docFailure(err)
	}
	if entry.Inherited {
		return Errorf("%s belongs to goal %s and was written by loop %s; read it with doc_read and file this loop's version with doc_write", entry.Name, entry.Goal, entry.Loop)
	}
	updated, rerr := replaceInDocument(entry.Name, content, old, replacement, replaceAll)
	if rerr != nil {
		return Errorf("%s", rerr)
	}
	written, unchanged, err := docs.Write(ctx, name, updated)
	if err != nil {
		return docFailure(err)
	}
	if unchanged {
		return Result{Content: entry.Name + " is unchanged.", Summary: "edit " + entry.Name + " · database"}
	}
	return Result{Content: "Edited " + describeEntry(written) + " in the database.", Summary: "edit " + entry.Name + " · database"}
}

func routedRead(ctx context.Context, docs DocStore, name string, offset, limit int) Result {
	entry, content, err := docs.Read(ctx, name)
	if err != nil {
		return docFailure(err)
	}
	body, note := pageDocument(content, offset, limit)
	return Result{Content: "# " + describeEntry(entry) + "\n" + body + note, Summary: "read " + entry.Name + " · database"}
}
