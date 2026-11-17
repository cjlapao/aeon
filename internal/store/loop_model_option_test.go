package store

import (
	"context"
	"testing"
)

// A loop's options are kept apart from its pin: set, read back, replaced, and cleared when both are empty.
func TestLoopModelOptionsRoundTrip(t *testing.T) {
	s, err := Open(Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	l, err := s.CreateLoop(NewLoop{Slug: "a", ComponentRoot: "/r", Title: "t", Goal: "g"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, present, _ := s.LoopModelOptionGet(ctx, l.ID); present {
		t.Fatal("a loop nobody changed has no options")
	}
	if err := s.LoopModelOptionSet(ctx, l.ID, "low", "off"); err != nil {
		t.Fatal(err)
	}
	o, present, _ := s.LoopModelOptionGet(ctx, l.ID)
	if !present || o.ReasoningEffort != "low" || o.Thinking != "off" {
		t.Fatalf("read back %+v", o)
	}
	if err := s.LoopModelOptionSet(ctx, l.ID, "", ""); err != nil {
		t.Fatal(err)
	}
	if _, present, _ := s.LoopModelOptionGet(ctx, l.ID); present {
		t.Fatal("both empty must clear the row")
	}
	if err := s.LoopModelOptionSet(ctx, l.ID, "x", "maybe"); err == nil {
		t.Fatal("a thinking value other than on/off must be refused by the column")
	}
}
