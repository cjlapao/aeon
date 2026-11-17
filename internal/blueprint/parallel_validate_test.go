package blueprint

import "testing"

// S32 T2 (PA11): a parallel_group is a declared-independent set; overlapping
// produces across it is an author-time `invalid`, not a runtime race. The
// runtime's real safety is PA5's worktree-per-unit; the validator is the
// guard rail that tells the author the group is wrong before anything runs.

func TestParallelGroupRejectsOverlappingProduces(t *testing.T) {
	raw := []byte(`id: overlap
schemaVersion: 1
version: 1
lanes:
  - id: a
    parallel_group: survey
    step: {kind: model-step, agent: worker, produces: [out.md]}
  - id: b
    parallel_group: survey
    step: {kind: model-step, agent: worker, produces: [out.md]}
`)
	b, err := Load(raw)
	if err != nil {
		t.Fatal(err)
	}
	r := Validate(b, ValidationOptions{Agents: []string{"worker"}})
	var found bool
	for _, d := range r.Diagnostics {
		if d.Kind == "parallel-produces-overlap" {
			found = true
		}
	}
	if !found {
		t.Fatalf("want a parallel-produces-overlap diagnostic, got %+v", r.Diagnostics)
	}
}

func TestParallelGroupAllowsDisjointProduces(t *testing.T) {
	raw := []byte(`id: disjoint
schemaVersion: 1
version: 1
lanes:
  - id: a
    parallel_group: survey
    step: {kind: model-step, agent: worker, produces: [a.md]}
  - id: b
    parallel_group: survey
    step: {kind: model-step, agent: worker, produces: [b.md]}
`)
	b, err := Load(raw)
	if err != nil {
		t.Fatal(err)
	}
	r := Validate(b, ValidationOptions{Agents: []string{"worker"}})
	for _, d := range r.Diagnostics {
		if d.Kind == "parallel-produces-overlap" {
			t.Fatalf("did not want a parallel-produces-overlap diagnostic, got %q: %s", d.Path, d.Msg)
		}
	}
}

// A lane with no parallel_group is never checked — it opts out by omission,
// the same convention every other optional blueprint field uses.
func TestNoParallelGroupIsNeverChecked(t *testing.T) {
	raw := []byte(`id: solo
schemaVersion: 1
version: 1
lanes:
  - id: a
    step: {kind: model-step, agent: worker, produces: [shared.md]}
  - id: b
    step: {kind: model-step, agent: worker, produces: [shared.md]}
`)
	b, err := Load(raw)
	if err != nil {
		t.Fatal(err)
	}
	r := Validate(b, ValidationOptions{Agents: []string{"worker"}})
	for _, d := range r.Diagnostics {
		if d.Kind == "parallel-produces-overlap" {
			t.Fatalf("lanes without a parallel_group must not be checked, got %q: %s", d.Path, d.Msg)
		}
	}
}
