package loop

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/tools"
)

// A step's `produces:` is met by a DOCUMENT in the database — what `doc_write`
// files — and, as the net under a weaker model, by a FILE in the workspace: the
// engine files that file into the database on the spot, so what the agent
// produced is in the store whichever tool it reached for. The database is the
// source of truth; the disk copy is never read again after the import.

// importedBy is the writer the engine stamps on a document it filed from disk.
const importedBy = "engine:workspace-import"

// missingProduced answers the first declared artifact the step did not
// produce, or "" when all of them exist. A name ending in "/" is a directory:
// met by any document under it.
func missingProduced(dep Deps, slug, componentRoot, root string, paths blueprint.Paths) string {
	sc := store.Scope{LoopSlug: slug, ComponentRoot: componentRoot}
	for _, rel := range paths {
		if dep.Store != nil && sc.Valid() && documentProduced(dep.Store, sc, rel) {
			continue
		}
		if missingArtifact(root, blueprint.Paths{rel}) != "" {
			return rel
		}
		if dep.Store != nil && sc.Valid() {
			importWorkspaceArtifact(dep.Store, sc, root, rel)
		}
	}
	return ""
}

// documentProduced reports whether the loop's own documents already meet rel.
func documentProduced(st *store.Store, sc store.Scope, rel string) bool {
	if strings.HasSuffix(rel, "/") {
		docs, err := st.ListDocuments(sc)
		if err != nil {
			return false
		}
		for _, d := range docs {
			if strings.HasPrefix(d.File, rel) {
				return true
			}
		}
		return false
	}
	_, err := st.ReadDocument(sc, rel)
	return err == nil
}

// importWorkspaceArtifact files what a step wrote with `write` into the loop's
// documents. A name the document door refuses (a source file, a deeper path) is
// left on disk untouched: only deliverables the store can hold are imported, and
// a refusal here is not the step's failure — the artifact exists.
func importWorkspaceArtifact(st *store.Store, sc store.Scope, root, rel string) {
	if strings.HasSuffix(rel, "/") {
		resolved := tools.Resolve(root, strings.TrimSuffix(rel, "/"))
		if !resolved.OK {
			return
		}
		entries, err := os.ReadDir(resolved.Abs)
		if err != nil {
			return
		}
		for _, e := range entries {
			if !e.IsDir() {
				importWorkspaceFile(st, sc, root, rel+e.Name())
			}
		}
		return
	}
	importWorkspaceFile(st, sc, root, rel)
}

func importWorkspaceFile(st *store.Store, sc store.Scope, root, rel string) {
	if !store.ResolveDocumentName(rel).OK() {
		return
	}
	resolved := tools.Resolve(root, rel)
	if !resolved.OK || !filepath.IsAbs(resolved.Abs) {
		return
	}
	info, err := os.Stat(resolved.Abs)
	if err != nil || info.IsDir() || info.Size() > store.MaxDocumentBytes {
		return
	}
	raw, err := os.ReadFile(resolved.Abs)
	if err != nil {
		return
	}
	_, _ = st.WriteDocument(sc, rel, string(raw), importedBy)
}
