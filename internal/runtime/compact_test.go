package runtime

import (
	"os"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

func TestSelectRangeRefusesWhenThereIsNoMiddle(t *testing.T) {
	messages := []model.Message{
		{Role: model.RoleSystem, Content: []model.Block{model.TextBlock("system")}},
		{Role: model.RoleUser, Content: []model.Block{model.TextBlock("task")}},
		{Role: model.RoleAssistant, Content: []model.Block{model.TextBlock("answer")}},
	}
	selected := SelectRange(messages, 100)
	if selected.OK || selected.Reason == "" {
		t.Fatalf("selected inadmissible range: %+v", selected)
	}
}

func TestEstimateAndRouteContextWindow(t *testing.T) {
	if modelRoute := (model.Route{ContextWindow: 8192}); modelRoute.ContextWindow != 8192 {
		t.Fatal("route lost context window")
	}
	a := EstimateTokens([]model.Message{{Role: model.RoleUser, Content: []model.Block{model.TextBlock("abcd")}}})
	if a != estimatedMessageOverhead+1 {
		t.Fatalf("estimate = %d", a)
	}
	if compactionSafetyFraction >= 1 {
		t.Fatal("missing safety margin")
	}
	source, err := os.ReadFile("compact.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(source), "uncalibrated") {
		t.Fatal("estimate's uncertainty is undocumented")
	}
}
