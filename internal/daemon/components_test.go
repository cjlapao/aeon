package daemon

import (
	"encoding/base64"
	"path/filepath"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
)

func TestComponentCreateValidatesWorkspaceAndListsEmptyComponent(t *testing.T) {
	s := recoveryStore(t)
	root := t.TempDir()
	projects := NewComponentStore(s)
	if _, err := projects.CreateComponent(protocol.ComponentCreateParams{Name: "Bad", Slug: "bad", Root: filepath.Join(root, "missing")}); err == nil {
		t.Fatal("missing workspace was accepted")
	}
	created, err := projects.CreateComponent(protocol.ComponentCreateParams{Name: "Aeon QA", Slug: "aeon-qa", Root: root + " ", Tags: []string{"go", "go", " qa "}})
	if err != nil {
		t.Fatal(err)
	}
	if created.Root != root || len(created.Tags) != 2 {
		t.Fatalf("project fields were not normalized: %+v", created)
	}
	listed, err := projects.ListComponents()
	if err != nil || len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("project missing without a loop: %+v, %v", listed, err)
	}
}

func TestComponentUpdateValidatesIconAndPreservesWorkspace(t *testing.T) {
	projects := NewComponentStore(recoveryStore(t))
	created, err := projects.CreateComponent(protocol.ComponentCreateParams{Name: "First", Slug: "first", Root: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := projects.UpdateComponent(protocol.ComponentUpdateParams{ID: created.ID, Name: "Second", Slug: "second", Icon: "https://example.test/icon.png"}); err == nil {
		t.Fatal("external icon URL was accepted")
	}
	icon := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("small fixture"))
	updated, err := projects.UpdateComponent(protocol.ComponentUpdateParams{ID: created.ID, Name: "Second", Slug: "second", Icon: icon, Tags: []string{"qa", "qa"}})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Root != created.Root || updated.ID != created.ID || updated.Icon != icon || len(updated.Tags) != 1 {
		t.Fatalf("project edit did not preserve identity or icon: %+v", updated)
	}
}
