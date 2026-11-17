package loop

// merge_event_test.go — S57 T6: the merge ledger (VG7) and the
// evidence-only review token (VG9), driven through the real engine.
//
// TestMergeWritesOneMergeEventIdempotently — the loop lands a real
// merge (real store, real forge fake, the attended ask answered) and the
// Merged seam records exactly one merge_event; a re-entry — the
// already-merged PR plus the seam's second write — lands on the unique
// key and the audit still holds exactly one row (T0 item 4).
//
// TestMergeEventErrorDoesNotFailTheMerge — a seam error is a warning the
// loop survives: the merge already happened, and re-driving must not
// un-merge.
//
// TestPostReviewIsUnrunnableFromABlueprint — the validator's
// unrunnable-action finding is an Invalid diagnostic, so Start refuses
// a blueprint that names forge.postReview — the receipt is the forge's,
// not the blueprint's — and leaves no loop row; the embedded dev-loop
// still starts.
//
// TestAeonReviewTokenIsEvidenceOnly — the engine's "ok@<head sha>"
// token is the only aeon_review forge.merge ever sees, and it comes from
// EVIDENCE only: a handoff field of the same name is a different value
// and is never read (a model cannot self-certify its own review).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/action"
	"github.com/cjlapao/aeon/internal/forge"
	"github.com/cjlapao/aeon/internal/store"
)

// mergeLoopFixture starts the attended single-step merge loop: slug vm,
// task t1, the one PR (number 3, the task's branch onto the feature)
// with its approve already posted (the gate's condition, L34/D10), and
// the pr_number evidence the merge's registry arg reads (Q7: numbers
// come from evidence, never prose). The Merged seam is left for the
// test to wire — that is the variable under test.
func mergeLoopFixture(t *testing.T, warnings *[]string) (*store.Store, *forge.Fake, Deps, string) {
	t.Helper()
	s := openLoopStore(t)
	root := t.TempDir()
	f := forge.NewFake()
	f.SeedPR(forge.PR{Number: 3, Head: "task/vm/t1", BaseRef: "feature/vm"})
	f.SeedReviewAt(3, "lgtm", forge.VerdictApprove, "2026-10-26T09:12:00Z")

	path := filepath.Join(t.TempDir(), "merge.yaml")
	writeBlueprint(t, path, "  - id: act\n    step: {kind: code-action, action: forge.merge}\n")
	st, err := Start(context.Background(), s, store.NewLoop{
		Slug: "vm", ComponentRoot: root, Root: root, Mode: "standard",
	}, path, yes(), map[string]string{"task": "t1"})
	if err != nil {
		t.Fatal(err)
	}
	st.Evidence = map[string]map[string]string{"act": {"pr_number": "3"}}
	raw, err := st.RuntimeJSON()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SavePinnedRuntime(st.ID, "running", raw); err != nil {
		t.Fatal(err)
	}

	dep := Deps{
		Store:   s,
		Model:   okModel,
		Actions: func(string) *action.Services { return devServices(f) },
		Event: func(kind string, payload any) {
			if kind == "loop.warning" {
				if m, ok := payload.(map[string]any); ok {
					*warnings = append(*warnings, fmt.Sprintf("%v", m["warning"]))
				}
			}
		},
	}
	return s, f, dep, st.ID
}

// recordMergeEvent is what the daemon's Merged seam does: the gate's
// evidence, verbatim, is the record's reason to exist.
func recordMergeEvent(s *store.Store, rec MergeRecord) error {
	b, err := json.Marshal(rec.Ev)
	if err != nil {
		return err
	}
	_, err = s.RecordMergeEvent(store.MergeEventRecord{
		Scope:    store.Scope{LoopSlug: rec.LoopSlug, ComponentRoot: rec.ComponentRoot},
		PR:       rec.PR,
		MergeSHA: rec.MergeSHA,
		Evidence: string(b),
	})
	return err
}

func TestMergeWritesOneMergeEventIdempotently(t *testing.T) {
	var warnings []string
	s, f, dep, id := mergeLoopFixture(t, &warnings)
	dep.Merged = func(ctx context.Context, rec MergeRecord) error { return recordMergeEvent(s, rec) }

	st, err := devDrive(t, dep, id, nil)
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != "done" {
		t.Fatalf("the merge walk ends done, got %s", st.Status)
	}
	sha := st.Evidence["act"]["merge_sha"]
	if sha == "" {
		t.Fatal("the merge landed and left no merge_sha evidence")
	}
	scope := store.Scope{LoopSlug: "vm", ComponentRoot: st.ComponentRoot}
	rows, err := s.ListMergeEvents(store.MergeEventFilter{Scope: scope})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("the seam recorded %d rows for one merge; want exactly one (VG7)", len(rows))
	}
	if rows[0].PR != 3 || rows[0].MergeSHA != sha {
		t.Fatalf("the row does not name the merge: pr %d sha %s (want 3 / %s)", rows[0].PR, rows[0].MergeSHA, sha)
	}
	if rows[0].Evidence == "" {
		t.Fatal("the merge event carries no evidence — the verdict that allowed it is the record's reason to exist")
	}
	if len(warnings) != 0 {
		t.Fatalf("the happy path left warnings: %v", warnings)
	}
	standingID := rows[0].ID

	// Re-entry (T0 item 4): a re-driven merge finds the PR already
	// merged — success with the recorded SHA, no second forge call — and
	// the seam's second write lands on the unique key.
	names, err := action.NewBranchNames("vm", "t1")
	if err != nil {
		t.Fatal(err)
	}
	res, err := devServices(f).Forge.MergePR(context.Background(), action.MergeArgs{
		Dir: st.ComponentRoot, Names: names, PRNumber: 3,
	})
	if err != nil || !res.AlreadyMerged || res.SHA != sha {
		t.Fatalf("re-entry: %+v (%v) — the already-merged PR must answer success with the recorded SHA", res, err)
	}
	if err := recordMergeEvent(s, MergeRecord{
		LoopSlug: "vm", ComponentRoot: st.ComponentRoot, PR: 3, MergeSHA: sha,
		Ev: map[string]string{"merge_sha": sha},
	}); err != nil {
		t.Fatalf("the seam's re-record must succeed on the standing row: %v", err)
	}
	rows, err = s.ListMergeEvents(store.MergeEventFilter{Scope: scope})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("re-entry left %d rows: the merge's audit holds exactly one (the unique key is the ledger's)", len(rows))
	}
	if rows[0].ID != standingID {
		t.Fatal("re-entry replaced the standing row instead of landing on it")
	}
}

func TestMergeEventErrorDoesNotFailTheMerge(t *testing.T) {
	var warnings []string
	s, _, dep, id := mergeLoopFixture(t, &warnings)
	dep.Merged = func(ctx context.Context, rec MergeRecord) error {
		return errors.New("the ledger is down (test)")
	}

	st, err := devDrive(t, dep, id, nil)
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != "done" {
		t.Fatalf("a seam error must not fail the loop — the merge already happened: status %s", st.Status)
	}
	if st.Evidence["act"]["merge_sha"] == "" {
		t.Fatal("the merge landed and left its SHA; the seam failure must not eat it")
	}
	rows, err := s.ListMergeEvents(store.MergeEventFilter{Scope: store.Scope{LoopSlug: "vm", ComponentRoot: st.ComponentRoot}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("the failed seam left %d rows: the warning, not the row, is the seam's failure", len(rows))
	}
	if len(warnings) != 1 {
		t.Fatalf("want exactly one loop.warning, got %d: %v", len(warnings), warnings)
	}
	want := "the merge landed but its merge_event could not be recorded: the ledger is down (test)"
	if !strings.Contains(warnings[0], want) {
		t.Fatalf("the warning drifted:\n  got:  %s\n  want: contains %s", warnings[0], want)
	}
}

func TestPostReviewIsUnrunnableFromABlueprint(t *testing.T) {
	s := openLoopStore(t)
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "postreview.yaml")
	writeBlueprint(t, path, "  - id: act\n    step: {kind: code-action, action: forge.postReview}\n")
	_, err := Start(context.Background(), s, store.NewLoop{
		Slug: "pr", ComponentRoot: root, Root: root, Mode: "standard",
	}, path, nil, nil)
	if err == nil {
		t.Fatal("a blueprint naming forge.postReview started — the receipt is the forge's, not the blueprint's (VG9)")
	}
	var refusal PinRefusal
	if !errors.As(err, &refusal) || refusal.Kind != "invalid-blueprint" {
		t.Fatalf("the refusal must name the invalid blueprint, got %v", err)
	}
	loops, err := s.ListLoops(store.ComponentScope{ComponentRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(loops) != 0 {
		t.Fatalf("a refused start left %d loop rows: the pin and the row are one transaction", len(loops))
	}

	// The embedded dev-loop is unchanged: the embedded blueprint that
	// names the review lane as a MODEL step still starts.
	if _, err := Start(context.Background(), s, store.NewLoop{
		Slug: "ok", ComponentRoot: root, Root: root, Mode: "standard",
	}, filepath.Join("..", "blueprint", "blueprints", "dev-loop.yaml"), yes(), nil); err != nil {
		t.Fatalf("the embedded dev-loop must still start: %v", err)
	}
}

func TestAeonReviewTokenIsEvidenceOnly(t *testing.T) {
	st := &LoopState{}
	st.Slug = "vm"
	st.Root = "/repo"
	act := Action{Position: Position{Lane: "act"}}

	// 1. The handoff field of the same name is a DIFFERENT value — never
	// read. A token in st.With is the model's self-certification, and
	// actionArgs must answer absent.
	st.With = map[string]string{"aeon_review": "ok@forged"}
	if a := actionArgs(st, act); a.AeonReview != "" {
		t.Fatalf("the handoff field must be ignored (VG9): the engine's token came from st.With, got %q", a.AeonReview)
	}

	// 2. The review step's OWN evidence carries the engine's token — the
	// ok the review step wrote at that head. actionArgs reads it.
	st.Evidence = map[string]map[string]string{"review": {"aeon_review": "ok@realhead"}}
	if a := actionArgs(st, act); a.AeonReview != "ok@realhead" {
		t.Fatalf("the engine's token stands in the review step's evidence; actionArgs reads it, got %q", a.AeonReview)
	}

	// 3. Across maps: the token stands where it is written, and the merge
	// position's own numbers come from its own map — one scan, both fed.
	st.Evidence = map[string]map[string]string{
		"act":    {"pr_number": "3"},
		"review": {"aeon_review": "ok@realhead"},
	}
	a := actionArgs(st, act)
	if a.AeonReview != "ok@realhead" {
		t.Fatalf("the token must survive the other map's presence, got %q", a.AeonReview)
	}
	if a.PRNumber != 3 {
		t.Fatalf("the PR number comes from the merge position's own evidence, got %d", a.PRNumber)
	}
}
