package main

import (
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/action"
)

// TestActionListAndCheck — both CLI surfaces. list prints the whole
// closed set with class, strategy and args; check prints the verdict
// line VERBATIM from the one renderer (a second phrasing of a verdict is
// a second truth).
func TestActionListAndCheck(t *testing.T) {
	var b strings.Builder
	if err := cmdAction(&b, []string{"list"}); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, name := range action.Names() {
		if !strings.Contains(out, name) {
			t.Fatalf("list omitted %s:\n%s", name, out)
		}
	}
	for _, want := range []string{"local", "outward", "irreversible", "check-then-act", "receipt-first", "naturally-idempotent"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the table must carry %s: class and strategy are the point of the listing", want)
		}
	}
	if strings.Contains(out, "render") {
		t.Fatal("render is not in the set (D13)")
	}
	if lines := strings.Split(strings.TrimSpace(out), "\n"); len(lines) != len(action.Names())+1 {
		t.Fatalf("header + one row per registered action, no extras: %d lines\n%s", len(lines), out)
	}

	b.Reset()
	if err := cmdAction(&b, []string{"check", "forge.merge"}); err != nil {
		t.Fatal(err)
	}
	want, err := action.CheckAnswer("forge.merge")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(b.String()) != want {
		t.Fatalf("check prints the one renderer verbatim\ngot:  %q\nwant: %q", strings.TrimSpace(b.String()), want)
	}
	if err := cmdAction(&b, []string{"check", "git.notAnAction"}); err == nil {
		t.Fatal("unknown names are refused naming the set")
	}
	if err := cmdAction(&b, []string{"add", "git.smuggle"}); err == nil {
		t.Fatal("there is no mutation verb — the registry is closed (D14)")
	}
}
