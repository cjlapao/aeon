package tools

import (
	"sort"
	"strings"
	"testing"
)

// The vocabulary's meanings, pinned to the vocabulary (S47 T10, TC3).
//
// `meta.go` closes the NAMES — a capability nobody reviewed cannot arrive as a
// string somebody typed. This file closes the SENTENCES, which is the half that
// a screen would otherwise write for itself: `tools.vocabulary` sends
// `CapabilityNotes()` to the Settings panel, so a capability that gained a name
// without a meaning would reach a person as a pill with nothing to read, and a
// note naming a capability that no longer exists would be a word the runtime has
// never heard of explained to someone deciding whether to allow it. Both are
// caught here, in the package that owns both lists, rather than four doors later
// in a screen that cannot tell the difference.

func TestCapabilityNotesCoverTheVocabulary(t *testing.T) {
	notes := CapabilityNotes()
	vocab := Capabilities()
	if len(vocab) < 10 {
		t.Fatalf("the vocabulary itself is %d capabilities — this test would pass on a collapsed set", len(vocab))
	}
	if got := missingNotes(notes, vocab); len(got) != 0 {
		t.Errorf("capabilities with no note: %v", got)
	}
	if got := unknownNotes(notes, vocab); len(got) != 0 {
		t.Errorf("notes naming a capability outside the closed vocabulary: %v", got)
	}
	if got := duplicateNotes(notes); len(got) != 0 {
		t.Errorf("capabilities with more than one note: %v — two meanings for one word is the second truth this slice exists to prevent", got)
	}
	for i, n := range notes {
		if i > 0 && notes[i-1].Capability > n.Capability {
			t.Errorf("notes are not sorted (%q after %q) — the door sends this order to the screen", n.Capability, notes[i-1].Capability)
		}
		m := strings.TrimSpace(n.Meaning)
		if m == "" {
			t.Errorf("%s: no meaning", n.Capability)
			continue
		}
		if m == string(n.Capability) {
			t.Errorf("%s: the meaning is the name echoed back — a stub, not a sentence", n.Capability)
		}
		if strings.Contains(m, "_") {
			t.Errorf("%s: the meaning %q spells a wire identifier instead of saying what it permits", n.Capability, m)
		}
		if !strings.HasSuffix(m, ".") {
			t.Errorf("%s: the meaning %q is not a sentence", n.Capability, m)
		}
		if CapabilityMeaning(n.Capability) != n.Meaning {
			t.Errorf("%s: CapabilityMeaning and CapabilityNotes disagree about the sentence", n.Capability)
		}
	}
	if m := CapabilityMeaning(Capability("flurb_read")); m != "" {
		t.Errorf("an unknown capability was handed the meaning %q — no meaning is the honest answer for a word this build does not declare", m)
	}
}

// TestCapabilityNotesCoverTheVocabularyHasTeeth feeds the checks their own
// canaries: a vocabulary that outgrows its notes, and a note that outlives its
// capability, must both be CAUGHT. Without this the test above could be made to
// pass by a comparison that compares nothing.
func TestCapabilityNotesCoverTheVocabularyHasTeeth(t *testing.T) {
	vocab := Capabilities()
	notes := CapabilityNotes()
	if len(notes) != len(vocab) {
		t.Fatalf("the pair is already out of step (%d notes, %d capabilities) — the canaries measure nothing", len(notes), len(vocab))
	}

	dropped := append([]CapabilityNote{}, notes[:len(notes)-1]...)
	if got := missingNotes(dropped, vocab); len(got) != 1 || got[0] != string(notes[len(notes)-1].Capability) {
		t.Errorf("a dropped note was not caught: %v", got)
	}
	if got := missingNotes(notes, vocab); len(got) != 0 {
		t.Errorf("the full note set offends the check — the check is broken, not the data: %v", got)
	}

	stale := append([]CapabilityNote{}, notes...)
	stale[len(stale)-1].Capability = Capability("graph_written")
	if got := unknownNotes(stale, vocab); len(got) != 1 || got[0] != "graph_written" {
		t.Errorf("a note naming a capability nobody declared was not caught: %v", got)
	}
	if got := unknownNotes(notes, vocab); len(got) != 0 {
		t.Errorf("the real notes offend the unknown-name check: %v", got)
	}

	duped := append([]CapabilityNote{}, notes...)
	duped = append(duped, duped[0])
	if got := duplicateNotes(duped); len(got) != 1 {
		t.Errorf("a duplicated note was not caught: %v", got)
	}
}

// TestRerunClassesAreTheCommittedVocabulary pins `RerunClasses()` — the list the
// vocabulary door sends — against the committed golden, so the three spellings a
// screen may offer are the three the rows are held to. A fourth class cannot be
// added to the door without adding it to the code and to the golden, which is
// the review this rule exists to force.
func TestRerunClassesAreTheCommittedVocabulary(t *testing.T) {
	committed := map[string]bool{}
	for _, e := range loadClasses(t) {
		committed[string(e.Class)] = true
	}
	list := RerunClasses()
	if len(list) == 0 {
		t.Fatal("the closed class list is empty")
	}
	for i, c := range list {
		if !c.Valid() {
			t.Errorf("%q is not a valid re-run class", c)
		}
		if !committed[string(c)] {
			t.Errorf("RerunClasses offers %q, which the committed list never used — the door would offer a word no row can carry", c)
		}
		if i > 0 && list[i-1] > c {
			t.Errorf("the class list is not sorted (%q after %q)", c, list[i-1])
		}
	}
	for c := range committed {
		found := false
		for _, want := range list {
			if string(want) == c {
				found = true
			}
		}
		if !found {
			t.Errorf("the committed list uses class %q and RerunClasses does not offer it", c)
		}
	}
}

// --- the comparisons, written so the canaries can feed them directly ---------

func missingNotes(notes []CapabilityNote, vocab []Capability) []string {
	have := map[Capability]bool{}
	for _, n := range notes {
		have[n.Capability] = true
	}
	var out []string
	for _, c := range vocab {
		if !have[c] {
			out = append(out, string(c))
		}
	}
	sort.Strings(out)
	return out
}

func unknownNotes(notes []CapabilityNote, vocab []Capability) []string {
	known := map[Capability]bool{}
	for _, c := range vocab {
		known[c] = true
	}
	var out []string
	for _, n := range notes {
		if !known[n.Capability] {
			out = append(out, string(n.Capability))
		}
	}
	sort.Strings(out)
	return out
}

func duplicateNotes(notes []CapabilityNote) []string {
	seen := map[Capability]int{}
	var out []string
	for _, n := range notes {
		seen[n.Capability]++
		if seen[n.Capability] == 2 {
			out = append(out, string(n.Capability))
		}
	}
	sort.Strings(out)
	return out
}
