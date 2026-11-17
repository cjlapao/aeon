package store

import (
	"reflect"
	"strings"
	"testing"
)

func TestTechTagsRideTheMetadataBlobAndKeepItsOtherKeys(t *testing.T) {
	meta, err := WithTech(`{"mode":"subagent"}`, []string{"react", "typescript"})
	if err != nil {
		t.Fatal(err)
	}
	a := Agent{Metadata: meta}
	if got := a.TechTags(); !reflect.DeepEqual(got, []string{"react", "typescript"}) {
		t.Fatalf("TechTags = %v", got)
	}
	if (Agent{Metadata: meta}).Metadata == `{"mode":"subagent"}` {
		t.Fatal("the tags did not reach the blob")
	}
	cleared, err := WithTech(meta, nil)
	if err != nil || cleared != `{"mode":"subagent"}` {
		t.Fatalf("clearing the tags must keep every other key: %q, %v", cleared, err)
	}
	if empty, _ := WithTech(`{"tech":["go"]}`, nil); empty != "" {
		t.Fatalf("a blob with nothing left is the empty blob, got %q", empty)
	}
	if got := (Agent{Metadata: "not json"}).TechTags(); got != nil {
		t.Fatalf("a blob that cannot be read has no tags: %v", got)
	}
}

func TestWithMinionSetsKeepsAndRemoves(t *testing.T) {
	meta, err := WithMinion(`{"tech":["go"],"owner":"x"}`, "noodle")
	if err != nil || !strings.Contains(meta, `"minion":"noodle"`) || !strings.Contains(meta, `"tech":["go"]`) || !strings.Contains(meta, `"owner":"x"`) {
		t.Fatalf("set must keep every other key: %q %v", meta, err)
	}
	cleared, err := WithMinion(meta, "")
	if err != nil || strings.Contains(cleared, "minion") || !strings.Contains(cleared, `"owner":"x"`) {
		t.Fatalf("an empty minion removes only its own key: %q %v", cleared, err)
	}
	if only, _ := WithMinion(`{"minion":"pip"}`, ""); only != "" {
		t.Fatalf("a blob with nothing left is empty: %q", only)
	}
	if _, err := WithMinion(`not json`, "pip"); err == nil {
		t.Fatal("a blob that is not JSON is refused")
	}
}
