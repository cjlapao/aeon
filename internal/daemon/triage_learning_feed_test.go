package daemon

import (
	"testing"

	"github.com/cjlapao/aeon/internal/learnings"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/triage"
)

func openTriageFeedStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("open the test store (a failure naming fts5 means the build lacks the sqlite_fts5 tag — build through make, B6): %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func triageFeedBug() triage.Bug {
	return triage.Bug{
		ID:            7,
		ComponentRoot: "/srv/projects/ledger",
		Title:         "retry counter over-counts on the second attempt",
		Body:          "the retry counter adds two on the second attempt; expected one",
	}
}

// TestAttemptConclusionFiledAsObservedLearning — D10: a completed validation
// attempt produces exactly one new learning row, provenance 'observed'. The
// attempt's conclusion is filed by the reactor after the bug moves out of
// unvalidated; the feed is the one call site, so one attempt files one
// learning — no more (a second Append would be a second mechanism), no less.
func TestAttemptConclusionFiledAsObservedLearning(t *testing.T) {
	s := openTriageFeedStore(t)
	d := &Daemon{store: s}
	bug := triageFeedBug()

	got, ref := d.feedValidationLearning(bug, triage.ValidationCouldNotTell)
	if ref != nil {
		t.Fatalf("the feed refused the conclusion: %s", ref.Reason)
	}
	if got.Provenance != learnings.ProvenanceObserved {
		t.Errorf("provenance = %q, want observed — the attempt saw it (D10); a told/assumed learning would lie about how the fact came", got.Provenance)
	}

	ls, err := learnings.New(s).List(learnings.ScopeProject, bug.ComponentRoot)
	if err != nil {
		t.Fatalf("list learnings: %v", err)
	}
	if len(ls) != 1 {
		t.Errorf("learnings filed for %s = %d, want exactly 1 — a completed attempt files one learning, no more (a second mechanism) and no less (the feed is the one call site)", bug.ComponentRoot, len(ls))
	}
	if got.ObservedCount == nil || *got.ObservedCount != 1 {
		t.Errorf("observed count = %v, want 1 — the attempt ran once, the strength is its count (M3, set once at write)", got.ObservedCount)
	}
}

// TestFeedIsProjectScopedNeverAgentScoped — the written learning's scope is
// always 'project', never 'agent': the hook does not infer which agent to
// credit. A validation attempt is a fact about the project's defect, not
// about an agent's competence; the scope_key is the bug's component root,
// not an agent name.
func TestFeedIsProjectScopedNeverAgentScoped(t *testing.T) {
	s := openTriageFeedStore(t)
	d := &Daemon{store: s}
	bug := triageFeedBug()

	got, ref := d.feedValidationLearning(bug, triage.ValidationReproduced)
	if ref != nil {
		t.Fatalf("the feed refused: %s", ref.Reason)
	}
	if got.Scope != learnings.ScopeProject {
		t.Errorf("scope = %q, want project — the hook files what a PROJECT learned; it does not credit an agent", got.Scope)
	}
	if got.Scope == learnings.ScopeAgent {
		t.Fatalf("the feed wrote an agent-scoped learning — it never infers which agent to credit (the task's own gate)")
	}
	// the scope_key is the bug's project root, never an agent name.
	if got.ScopeKey != bug.ComponentRoot {
		t.Errorf("scope_key = %q, want the bug's component root %q (a project scope's key is a root, not a name)", got.ScopeKey, bug.ComponentRoot)
	}
}
