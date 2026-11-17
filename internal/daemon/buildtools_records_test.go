package daemon

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/cjlapao/aeon/internal/runtime"
	"github.com/cjlapao/aeon/internal/tools"
)

// TestBuildToolsWiresRecords pins the copy BuildTools owes every step: the
// StepRunner's Records seam must arrive in the ToolContext the tools read —
// unexported field, so the pointer of the function is compared through
// reflect (reading it is the test's whole job; copying is BuildTools').
func TestBuildToolsWiresRecords(t *testing.T) {
	want := tools.RecordsInvoker(func(ctx context.Context, tool string, input json.RawMessage) (string, bool) {
		return "wired", false
	})
	r := StepRunner{Records: want}
	caller, _, err := r.BuildTools([]string{"read"}, t.TempDir(), "test-handler", runtime.FrozenManifest{})
	if err != nil {
		t.Fatalf("BuildTools: %v", err)
	}
	set := reflect.ValueOf(caller).FieldByName("set") // toolCaller{set: tools.Toolset}
	if !set.IsValid() {
		t.Fatal("toolCaller no longer wraps a set")
	}
	tc := set.FieldByName("tc")
	rec := tc.FieldByName("Records")
	if rec.Pointer() != reflect.ValueOf(want).Pointer() {
		t.Fatalf("BuildTools did not copy StepRunner.Records into the ToolContext")
	}
	// And the zero runner leaves it nil — a storeless daemon stays storeless.
	caller2, _, err := (StepRunner{}).BuildTools([]string{"read"}, t.TempDir(), "test-handler", runtime.FrozenManifest{})
	if err != nil {
		t.Fatalf("BuildTools zero: %v", err)
	}
	tc2 := reflect.ValueOf(caller2).FieldByName("set").FieldByName("tc")
	if !tc2.FieldByName("Records").IsNil() {
		t.Fatal("a storeless StepRunner wired a records door out of nowhere")
	}
}
