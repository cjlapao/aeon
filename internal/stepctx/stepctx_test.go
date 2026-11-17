package stepctx

import (
	"context"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	ctx := WithStepID(context.Background(), 42)
	id, ok := StepID(ctx)
	if !ok || id != 42 {
		t.Fatalf("round trip: %d %v", id, ok)
	}
	// A derived context keeps the fact.
	d, cancel := context.WithCancel(ctx)
	defer cancel()
	if id, ok := StepID(d); !ok || id != 42 {
		t.Fatalf("derived ctx lost the step: %d %v", id, ok)
	}
}

func TestZeroIsAbsent(t *testing.T) {
	// An ad-hoc step before its row exists carries 0 — which reads as NO
	// identity, because a tool that stamps provenance from 0 would stamp a
	// lie.
	ctx := WithStepID(context.Background(), 0)
	if id, ok := StepID(ctx); ok || id != 0 {
		t.Fatalf("zero must read absent: %d %v", id, ok)
	}
	if _, ok := StepID(context.Background()); ok {
		t.Fatal("a bare context carries no step")
	}
	// A wrong-typed value under our key is not an identity either — the key
	// type is unexported, so this is unreachable from outside; the guard
	// stays anyway.
	ctx = context.WithValue(context.Background(), key{}, "42")
	if _, ok := StepID(ctx); ok {
		t.Fatal("a string is not a step id")
	}
}
