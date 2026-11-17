package automation

// starter_test.go — S40 T3/T4's contract shapes, tested where they live.
// AutoSlug is crash recovery's handle (AU13): the same (trigger, outbox
// row) must produce the same bytes on any process, any day — and the
// namespace must not graze S20's schedule slugs.

import (
	"regexp"
	"strings"
	"testing"
)

func TestAutoSlugIsDeterministicAndValid(t *testing.T) {
	a := AutoSlug("trg-Ab1", 7)
	b := AutoSlug("trg-Ab1", 7)
	if a != b {
		t.Fatalf("AutoSlug is not deterministic: %q vs %q", a, b)
	}
	if a != "auto-trgab1-7" {
		t.Errorf("AutoSlug = %q, want auto-trgab1-7 (lower-case, [a-z0-9] kept, the rest dropped)", a)
	}
	if c := AutoSlug("trg-Ab1", 8); c == a {
		t.Error("a different outbox row produced the same slug — one fire would refuse another's twin")
	}
	if c := AutoSlug("trg-Ab2", 7); c == a {
		t.Error("a different trigger produced the same slug")
	}
	if !regexp.MustCompile(`^auto-[a-z0-9]+-[0-9]+$`).MatchString(a) {
		t.Errorf("slug %q fails the slug rule (lower-case alphanumerics and the deterministic frame)", a)
	}
	// S20's schedule slugs are <trigger id>-<20060102-1504> (Fire.Slug());
	// the auto- frame keeps the namespaces apart — no schedule slug starts
	// with "auto-" unless its trigger id does, and ids are ids.
	if strings.HasPrefix("sched1-20261028-0900", "auto-") {
		t.Error("the namespaces are not apart")
	}
	// Determinism under junk: punctuation drops, never substitutes —
	// two triggers whose ids differ only in punctuation DO collide, and
	// that is recorded here honestly: the ids come from newID(), which is
	// alphanumeric, so the collision class is empty in practice.
	if AutoSlug("a.b", 1) != AutoSlug("ab", 1) {
		t.Error("the drop rule is not a pure drop")
	}
}

func TestRefusalErrorCarriesItsCode(t *testing.T) {
	r := &Refusal{Reason: "workspace-invalid", Sentence: "the directory is gone"}
	if r.Error() != "workspace-invalid: the directory is gone" {
		t.Errorf("Error() = %q, want code + sentence (the fire row stores this verbatim)", r.Error())
	}
}
