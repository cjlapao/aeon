package daemon

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/protocol"
)

func TestModelMetadataAndSecretSurviveDaemonRouteStoreReload(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := t.TempDir()
	first := t13Store(t, dir)
	before := protocol.NewMutableRoutes()
	if err := before.WithPersistence(NewRouteStore(first)); err != nil {
		t.Fatal(err)
	}
	original := model.Route{ID: "research", Provider: "openai", Model: "provider/model-slug", DisplayName: "Research", DisplayDescription: "Careful reasoning", APIKey: "private-test-model-key", MaxOutputTokens: 4096, Capabilities: []string{"text", "thinking-effort"}, ReasoningEfforts: []string{"low", "max"}, ReasoningEffort: "max", NoPrice: true}
	if _, err := before.SetAndPersist(original); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second := t13Store(t, dir)
	restored := protocol.NewMutableRoutes()
	if err := restored.WithPersistence(NewRouteStore(second)); err != nil {
		t.Fatal(err)
	}
	route, ok := restored.Route("research")
	if !ok || route.APIKey != original.APIKey || route.DisplayName != original.DisplayName || len(route.ReasoningEfforts) != 2 || route.ReasoningEfforts[1] != "max" {
		t.Fatalf("model configuration was not restored")
	}
	view := restored.Views()[0]
	raw, _ := json.Marshal(view)
	if !view.HasKey || strings.Contains(string(raw), original.APIKey) {
		t.Fatal("restored credential missing or exposed")
	}
}
