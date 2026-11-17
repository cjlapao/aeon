package loop

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/store"
)

func producedEnv(t *testing.T) (Deps, store.Scope, string) {
	t.Helper()
	st, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	root := t.TempDir()
	if _, err := st.CreatePinnedLoop(store.NewLoop{Slug: "p", ComponentRoot: root, Root: root, Goal: "g", Status: "running"}, "bp", 1, "raw", `{}`); err != nil {
		t.Fatal(err)
	}
	return Deps{Store: st}, store.Scope{LoopSlug: "p", ComponentRoot: root}, root
}

func TestADocumentInTheDatabaseMeetsProduces(t *testing.T) {
	dep, sc, root := producedEnv(t)
	if got := missingProduced(dep, sc.LoopSlug, sc.ComponentRoot, root, blueprint.Paths{"legal.md"}); got != "legal.md" {
		t.Fatalf("nothing written yet: %q", got)
	}
	if res, err := dep.Store.WriteDocument(sc, "legal.md", "text", "agent"); err != nil || !res.OK {
		t.Fatalf("write: %+v %v", res, err)
	}
	if _, err := dep.Store.WriteDocument(sc, "tasks/T1.md", "t", "agent"); err != nil {
		t.Fatal(err)
	}
	if got := missingProduced(dep, sc.LoopSlug, sc.ComponentRoot, root, blueprint.Paths{"legal.md", "tasks/"}); got != "" {
		t.Fatalf("both are in the database, none on disk: %q", got)
	}
}

func TestAFileTheAgentWroteWithWriteIsFiledIntoTheDatabase(t *testing.T) {
	dep, sc, root := producedEnv(t)
	if err := os.WriteFile(filepath.Join(root, "plan.md"), []byte("the plan"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := missingProduced(dep, sc.LoopSlug, sc.ComponentRoot, root, blueprint.Paths{"plan.md"}); got != "" {
		t.Fatalf("the file exists: %q", got)
	}
	doc, err := dep.Store.ReadDocument(sc, "plan.md")
	if err != nil || doc.Content != "the plan" || doc.WrittenBy != importedBy {
		t.Fatalf("the engine filed the file into the store: %+v %v", doc, err)
	}
}
