package container

// F-1, found by S59 T12's walk against a real daemon and pinned here.
//
// The seam between the daemon and a non-host backend used to hand over an
// environment and a child environment and nothing else, so Backend.Executor
// built an Executor with no LoopRef. Every container-bound step the daemon ran
// then died at the ledger's identity check — "a provision needs an environment,
// a component root and a loop slug" — while this package's own suite stayed
// green, because its tests hand-built the Executor with a LoopRef and never
// went through the door the daemon actually walks through.
//
// So this test goes through the door. It is the smallest assertion that would
// have failed before the fix: what the daemon hands in must be what the
// provision is created with.

import (
	"context"
	"testing"

	"github.com/cjlapao/aeon/internal/execenv"
)

func TestBackendExecutorCarriesTheProvisionIdentity(t *testing.T) {
	// A runtime is present because the door refuses a backend with no engine wired
	// (EV13) — nothing here calls it; identity is decided before any engine call.
	b := Backend{P: &Provisioner{Runtime: &fakeRuntime{}}}
	ident := execenv.ProvisionIdent{
		ComponentRoot: "/srv/components/api",
		LoopSlug:      "s59-walk-1",
		Owner:         "42",
	}

	ex, refusal := b.Executor(context.Background(), testEnv("unenforced", execenv.PullNever), execenv.ChildEnv{}, ident)
	if refusal != nil {
		t.Fatalf("the backend refused before it was asked to create anything: %s", refusal.Sentence())
	}
	got, ok := ex.(*Executor)
	if !ok {
		t.Fatalf("the backend answered %T, want *Executor", ex)
	}
	if got.Loop.ComponentRoot != ident.ComponentRoot || got.Loop.LoopSlug != ident.LoopSlug || got.Loop.Owner != ident.Owner {
		t.Errorf("the identity did not survive the seam: got %#v, want %#v", got.Loop, ident)
	}
	if miss := ident.Missing(); len(miss) != 0 {
		t.Errorf("a complete identity reports missing fields: %v", miss)
	}
	if miss := (execenv.ProvisionIdent{ComponentRoot: "/x"}).Missing(); len(miss) != 2 {
		t.Errorf("an incomplete identity must name both gaps, got %v", miss)
	}
}
