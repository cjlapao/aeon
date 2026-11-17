package model

import (
	"os"
	"strings"
	"testing"
)

// S18 T2: the wire-behaviour vocabulary, the three states, and the wall
// between this vocabulary and the model catalogue's.

func TestThreeCapabilityStates(t *testing.T) {
	all := []CapabilityState{StateDeclared, StateVerified, StateContradicted}
	for i, a := range all {
		if !ValidCapabilityState(a) {
			t.Errorf("%q is not a valid state", a)
		}
		for j, b := range all {
			if i != j && a == b {
				t.Errorf("%q and %q are the same state — a probe cannot distinguish them", a, b)
			}
		}
	}
	// The zero value means NOTHING. An empty string is not "declared" hiding
	// behind a default: absence of a probe row is rendered `declared` by the
	// view layer saying the word, not by the record going quiet.
	if ValidCapabilityState(CapabilityState("")) {
		t.Error("the empty string is a valid capability state — it must not be")
	}
	if string(StateDeclared) != "declared" || string(StateVerified) != "verified" ||
		string(StateContradicted) != "contradicted" {
		t.Error("the state words drifted from M28's")
	}
}

// TestFlagsExpressAbsences — M13's claim that "the differences are absences,
// not behaviours" IS this table. A flag whose absence has no stated
// consequence is a difference nobody agreed how the product behaves around.
func TestFlagsExpressAbsences(t *testing.T) {
	flags := WireCapabilityFlags()
	if len(flags) != 5 {
		t.Fatalf("%d wire flags (%v), want the closed five of Q8", len(flags), flags)
	}
	for _, f := range flags {
		note, ok := AbsenceNote(Capability(f))
		if !ok || strings.TrimSpace(note) == "" {
			t.Errorf("flag %q has no absence note — its absence would be an undescribed behaviour", f)
		}
		if !strings.Contains(strings.ToLower(note), "no ") &&
			!strings.Contains(strings.ToLower(note), "not ") &&
			!strings.Contains(strings.ToLower(note), "refus") &&
			!strings.Contains(strings.ToLower(note), "at most one") &&
			!strings.Contains(strings.ToLower(note), "must") {
			t.Errorf("absence note for %q does not state what is missing: %q", f, note)
		}
	}
}

func TestUnknownCapabilityIsRejected(t *testing.T) {
	r := Route{ID: "r", WireCapabilities: []string{"vision"}}
	err := r.NormalizeWireCapabilities()
	if err == nil {
		t.Fatal("a flag outside the closed set was accepted")
	}
	for _, want := range []string{"vision", "r", "tools", "streaming-usage", "thinking", "system-role", "parallel-tools"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal is missing %q: %v", want, err)
		}
	}
	// Duplicates and empties are refusals too: a declaration that says a
	// thing twice and a declaration of the empty string are both somebody
	// having mis-typed a flag list.
	if err := (&Route{ID: "d", WireCapabilities: []string{"tools", "tools"}}).NormalizeWireCapabilities(); err == nil {
		t.Error("a flag declared twice was accepted")
	}
	if err := (&Route{ID: "e", WireCapabilities: []string{"tools", " "}}).NormalizeWireCapabilities(); err == nil {
		t.Error("an empty wire flag was accepted")
	}
}

// TestWireCapabilitiesCanonicaliseAndKeepNilFromEmpty — order is canonical
// (the order `routes list` prints must not depend on typing order), and nil
// vs [] keeps UNDECLARED distinct from DECLARED-NONE across the whole path.
func TestWireCapabilitiesCanonicaliseAndKeepNilFromEmpty(t *testing.T) {
	r := Route{ID: "r", WireCapabilities: []string{"thinking", "tools"}}
	if err := r.NormalizeWireCapabilities(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(r.WireCapabilities, ",") != "tools,thinking" {
		t.Errorf("declaration order survived: %v, want canonical tools,thinking", r.WireCapabilities)
	}

	undeclared := Route{ID: "u"}
	if err := undeclared.NormalizeWireCapabilities(); err != nil {
		t.Fatal(err)
	}
	if undeclared.WireCapabilities != nil {
		t.Error("an undeclared route came back with an empty slice — UNDECLARED became DECLARED-NONE")
	}

	none := Route{ID: "n", WireCapabilities: []string{}}
	if err := none.NormalizeWireCapabilities(); err != nil {
		t.Fatal(err)
	}
	if none.WireCapabilities == nil || len(none.WireCapabilities) != 0 {
		t.Error("DECLARED-NONE came back as nil or non-empty")
	}
}

// TestWireAndMetadataCapabilitiesAreDistinct — the two vocabularies both
// contain "thinking" and mean different things by it. Functional half: each
// validator admits its own set and rejects the other's. Structural half:
// capability.go never reaches into metadata.go's `normalizeSelection`, and
// metadata.go never learns the wire names. Asserting the call sites, not
// just the answers — a future "DRY this up" that routes wire flags through
// normalizeSelection would pass every functional test above and still merge
// the vocabularies.
func TestWireAndMetadataCapabilitiesAreDistinct(t *testing.T) {
	// Functional: "thinking" means two different things and both are legal,
	// on two fields, validated twice.
	r := Route{ID: "both", Capabilities: []string{"thinking"}, WireCapabilities: []string{"tools", "thinking"}}
	if err := r.NormalizeMetadata(); err != nil {
		t.Fatalf("the metadata path refused its own vocabulary: %v", err)
	}
	if err := r.NormalizeWireCapabilities(); err != nil {
		t.Fatalf("the wire path refused its own vocabulary: %v", err)
	}
	if strings.Join(r.Capabilities, ",") != "thinking" ||
		strings.Join(r.WireCapabilities, ",") != "tools,thinking" {
		t.Errorf("the two fields interfered: metadata %v, wire %v", r.Capabilities, r.WireCapabilities)
	}

	// Each set rejects the other's words.
	if err := (&Route{ID: "x", Capabilities: []string{"tools"}}).NormalizeMetadata(); err == nil {
		t.Error("the metadata vocabulary admitted the wire-only flag `tools`")
	}
	if err := (&Route{ID: "x", WireCapabilities: []string{"image"}}).NormalizeWireCapabilities(); err == nil {
		t.Error("the wire vocabulary admitted the metadata-only flag `image`")
	}

	// Structural: separate code paths, asserted by the source. Line COMMENTS
	// are stripped first because capability.go's doc comments NAME
	// normalizeSelection to explain why it is never called — the rule is
	// about calls, and a grep that cannot tell prose from code would either
	// false-fail on the honest comment or demand the comment lie.
	stripComments := func(src string) string {
		var kept []string
		for _, line := range strings.Split(src, "\n") {
			if i := strings.Index(line, "//"); i >= 0 {
				line = line[:i]
			}
			kept = append(kept, line)
		}
		return strings.Join(kept, "\n")
	}
	wireRaw, err := os.ReadFile("capability.go")
	if err != nil {
		t.Fatal(err)
	}
	wireSrc := stripComments(string(wireRaw))
	if strings.Contains(wireSrc, "normalizeSelection") {
		t.Error("capability.go calls metadata.go's normalizeSelection — the vocabularies merged")
	}
	metaRaw, err := os.ReadFile("metadata.go")
	if err != nil {
		t.Fatal(err)
	}
	metaSrc := stripComments(string(metaRaw))
	for _, word := range []string{"WireCapabilities", "wire", "parallel-tools", "streaming-usage", "system-role"} {
		if strings.Contains(string(metaSrc), word) {
			t.Errorf("metadata.go learned the wire word %q — the vocabularies merged in the other direction", word)
		}
	}
}

// TestNoPriceIsAFieldOnTheRoute — the declaration must survive the envelope
// (omitempty means a metered route's JSON stays exactly what it was before
// this field existed).
func TestNoPriceIsAFieldOnTheRoute(t *testing.T) {
	r := Route{ID: "free", Provider: "openai", Model: "gpt-4o", NoPrice: true}
	if !r.NoPrice {
		t.Fatal("the declaration did not compile onto the struct")
	}
}
