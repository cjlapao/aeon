package daemon

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// A long request is cut to a readable one-line title at a word, with an ellipsis; a short one is untouched.
func TestShortTitleCutsAtAWord(t *testing.T) {
	long := "Turn the hello program into a small, tested greeting CLI with flags, a history file and a README that explains how to use it"
	got := shortTitle(long, 60)
	if utf8.RuneCountInString(got) > 60 || !strings.HasSuffix(got, "…") || strings.Contains(got, "  ") {
		t.Fatalf("not a readable cut: %q", got)
	}
	if strings.HasSuffix(strings.TrimSuffix(got, "…"), "a hi") {
		t.Fatalf("cut a word in half: %q", got)
	}
	if shortTitle("Fix the build", 60) != "Fix the build" {
		t.Fatal("a short request is its own title")
	}
	if shortTitle("line one\n\n  line   two", 60) != "line one line two" {
		t.Fatal("whitespace and newlines must collapse to one line")
	}
}

// A model's answer becomes a title: first line, no quotes or prefix, bounded; nothing usable is empty.
func TestCleanTitleReadsAModelsAnswer(t *testing.T) {
	cases := map[string]string{
		"Greeting CLI with history":       "Greeting CLI with history",
		"\"Greeting CLI with history\".":  "Greeting CLI with history",
		"Title: Greeting CLI\nbecause...": "Greeting CLI",
		"   ":                             "",
	}
	for in, want := range cases {
		if got := cleanTitle(in); got != want {
			t.Errorf("cleanTitle(%q) = %q, want %q", in, got, want)
		}
	}
	if got := cleanTitle(strings.Repeat("word ", 40)); utf8.RuneCountInString(got) > titleMaxRunes {
		t.Errorf("an over-long answer must be bounded: %d runes", utf8.RuneCountInString(got))
	}
}
