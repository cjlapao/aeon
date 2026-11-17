package protocol

import (
	"encoding/json"
	"github.com/cjlapao/aeon/internal/model"
	"strings"
	"testing"
)

func TestRouteMetadataPatchesPreserveAndClearWithoutExposingKeys(t *testing.T) {
	tbl := routeTable(t)
	name, description, key := "Research model", "For careful work", "secret-model-key"
	caps, efforts := []string{"text", "thinking-effort"}, []string{"low", "xhigh", "max"}
	_, err := setRoute(t, tbl, SetRouteParams{ID: "research", Provider: "openai", Model: "provider-slug", APIKey: key, DisplayName: &name, DisplayDescription: &description, Capabilities: &caps, ReasoningEfforts: &efforts, ReasoningEffort: "max"})
	if err != nil {
		t.Fatal(err)
	}
	// A legacy client edits the old fields only. The new metadata must survive.
	res, err := setRoute(t, tbl, SetRouteParams{ID: "research", Provider: "openai", Model: "updated-slug"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Route.DisplayName != name || res.Route.DisplayDescription != description || res.Route.ReasoningEfforts == nil || len(*res.Route.ReasoningEfforts) != 3 || !res.Route.HasKey {
		t.Fatalf("metadata lost: %+v", res.Route)
	}
	raw, _ := json.Marshal(res)
	if strings.Contains(string(raw), key) {
		t.Fatal("credential exposed")
	}
	empty, emptyList := "", []string{}
	res, err = setRoute(t, tbl, SetRouteParams{ID: "research", Provider: "openai", Model: "updated-slug", DisplayDescription: &empty, Icon: &empty, Capabilities: &emptyList, ReasoningEfforts: &emptyList, ClearReasoningEffort: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Route.DisplayDescription != "" || res.Route.Capabilities == nil || len(*res.Route.Capabilities) != 0 {
		t.Fatalf("explicit clears lost: %+v", res.Route)
	}
}

func TestRouteMetadataInvalidEditsLeaveTheOriginal(t *testing.T) {
	tbl := routeTable(t, model.Route{ID: "r", Provider: "openai", Model: "m", APIKey: "keep-key", MaxOutputTokens: 10})
	caps := []string{"pdf"}
	if _, err := setRoute(t, tbl, SetRouteParams{ID: "r", Provider: "openai", Model: "m", Capabilities: &caps, ClearKey: true}); err == nil {
		t.Fatal("invalid PDF declaration accepted")
	}
	route, _ := tbl.Routes.Route("r")
	if route.APIKey != "keep-key" {
		t.Fatal("invalid edit cleared credential")
	}
}
