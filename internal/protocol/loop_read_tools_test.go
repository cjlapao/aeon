package protocol

import (
	"reflect"
	"testing"
)

// Every step that belongs to a loop can read the loop's documents and index, whatever its body names: the loop's
// files are in the database, so an engineer told to follow tasks/T1.md must be able to open it.
func TestALoopStepAlwaysHoldsTheLoopReadTools(t *testing.T) {
	granted := []string{"read", "write", "doc_read"}
	got := withLoopTools(granted, func(name string) bool { return name == "graph" })
	want := []string{"read", "write", "doc_read", "doc_list", "loop_context", "question", "graph"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("grant %v, want %v", got, want)
	}
	if len(granted) != 3 {
		t.Error("the caller's grant slice was changed")
	}
	if without := withLoopTools(granted, nil); !reflect.DeepEqual(without, []string{"read", "write", "doc_read", "doc_list", "loop_context", "question"}) {
		t.Errorf("with no optional tool wired the code tools must not be named (an unknown tool refuses the compile): %v", without)
	}
	if again := withLoopTools(got, func(name string) bool { return name == "graph" }); !reflect.DeepEqual(again, want) {
		t.Errorf("adding them twice changed the grant: %v", again)
	}
	for _, name := range append(append([]string{}, loopBaseTools...), loopCodeTools...) {
		if name == "doc_write" || name == "doc_edit" {
			t.Errorf("%s is a write tool; the implicit grant is read-only", name)
		}
	}
}
