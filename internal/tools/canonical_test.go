package tools

import "testing"

func TestCanonicalNamesAgreeWithEveryRegistryEntry(t *testing.T) {
	for key, tool := range Registry() {
		if got := CanonicalToolName(key); got != tool.Spec.Name {
			t.Errorf("CanonicalToolName(%q) = %q, registry says %q", key, got, tool.Spec.Name)
		}
	}
}

func BenchmarkCanonicalToolName(b *testing.B) {
	for b.Loop() {
		CanonicalToolName(aliasWriteFile)
		CanonicalToolName("read")
	}
}
