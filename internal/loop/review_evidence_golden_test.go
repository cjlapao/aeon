package loop

// review_evidence_golden_test.go — S57 T1 (characterization, P2 Phase A).
// Where a finished `review` rung's `ok` outcome lives in `LoopState`,
// pinned on the untouched tree (1ae086c0) by driving the real engine
// (`Start` → `Run` → `Pinned`) with a scripted model runner:
//
//   - review-ok-lands: the ok outcome lands in
//     `st.StepStates[stepKey(position)]` as
//     `{status: done, outcome: "ok", stepId: …}` — the EXACT source
//     T6's `ok@<sha>` token will read. The `Reason` field is ABSENT
//     (omitempty on the empty string): a clean pass stores no prose.
//   - review-ok-with-finding: the same stored shape, plus the O7 side
//     effect — an ok WITH a non-empty reason on a step that
//     `sets: in-review` appends the finding to the durable backlog at
//     `<root>/docs/unresolved-pr-comments.md` (date normalized: the
//     entry is stamped `time.Now().UTC()` today, which a golden may not
//     pin).
//   - review-reject-unverified-stores: a reject whose reason carries the
//     `unverified: ` prefix STORES the honest third value
//     (`outcome: "unverified"` in the step state — the record a person
//     reads) while ROUTING still runs on the reject it is (the rework
//     step runs and the loop finishes) — D8's two-value separation, the
//     pinned source T6's unverified charging keys off.
//
// The fake runner's StepIDs are fixed (101/102); the loop id in event
// payloads is normalized to `<loopId>`.
//
// Generation (run once, on the untouched tree):
//
//	AEON_WRITE_GOLDEN=1 go test ./internal/loop/ -run TestReviewEvidenceGoldens -count=1 -tags sqlite_fts5

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/store"
)

const goldReviewEvidence = "testdata/review_evidence_golden.json"

type goldReviewEvent struct {
	Kind    string         `json:"kind"`
	Payload map[string]any `json:"payload"`
}

type goldReviewRow struct {
	Case       string                       `json:"case"`
	Status     string                       `json:"status"`
	LastReason string                       `json:"lastReason"`
	Position   Position                     `json:"position"`
	StepStates map[string]StepState         `json:"stepStates"`
	Evidence   map[string]map[string]string `json:"evidence"`
	Events     []goldReviewEvent            `json:"events"`
	Backlog    string                       `json:"backlog,omitempty"`
}

// reviewLanes is a dev-loop-shaped review rung: the model step
// `code-reviewer` reviews, `sets: in-review`, and — in the reject row —
// routes rework exactly as the shipped blueprint does.
const reviewLanes = "  - id: review\n" +
	"    step: {kind: model-step, agent: code-reviewer, sets: in-review}\n"

const reviewLanesWithRework = "  - id: review\n" +
	"    step: {kind: model-step, agent: code-reviewer, sets: in-review, on_reject: {goto: rework, then: block}}\n" +
	"  - id: rework\n" +
	"    step: {kind: model-step, agent: default}\n"

// goldReviewDriveRow runs one scripted review case to its durable end.
// The runner answers by STEP ID from the script; a step the script does
// not name is a test bug, not an engine answer.
func goldReviewDriveRow(t *testing.T, name, lanes string, root string, script map[string]ModelResult) goldReviewRow {
	t.Helper()
	s := openLoopStore(t)
	path := filepath.Join(t.TempDir(), "re.yaml")
	writeBlueprint(t, path, lanes)
	st, err := Start(context.Background(), s, store.NewLoop{
		Slug: "gld-re-" + name, ComponentRoot: root, Root: root, Mode: "standard",
	}, path, boolPtr(false), nil)
	if err != nil {
		t.Fatal(err)
	}
	events := []goldReviewEvent{}
	dep := Deps{
		Store: s,
		Model: func(ctx context.Context, st LoopState, step blueprint.Step, words, frame string) (ModelResult, error) {
			// A lane-inline step carries no step id of its own: the
			// script keys those by the position's lane, which is the
			// same string the engine keys the step state by.
			key := step.ID
			if key == "" {
				key = st.Position.Lane
			}
			r, ok := script[key]
			if !ok {
				t.Fatalf("the script has no answer for step %q", key)
			}
			return r, nil
		},
		Event: func(kind string, payload any) {
			if m, ok := payload.(map[string]any); ok {
				events = append(events, goldReviewEvent{Kind: kind, Payload: normalizeLoopID(m)})
			} else {
				events = append(events, goldReviewEvent{Kind: kind})
			}
		},
	}
	if err := Run(context.Background(), dep, st.ID); err != nil {
		t.Fatal(err)
	}
	saved, err := Pinned(context.Background(), s, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	row := goldReviewRow{
		Case:       name,
		Status:     saved.Status,
		LastReason: saved.LastReason,
		Position:   saved.Position,
		StepStates: saved.StepStates,
		Evidence:   saved.Evidence,
		Events:     events,
	}
	if b, err := os.ReadFile(BacklogPath(root)); err == nil {
		// The entry's date is time.Now().UTC() today — a golden pins the
		// shape, not the day.
		row.Backlog = normalizeBacklogDate(string(b))
	}
	return row
}

// normalizeBacklogDate rewrites the entry's stamp to a fixed token; every
// other byte of the file is pinned.
var backlogDate = regexp.MustCompile(`20\d{2}-\d{2}-\d{2}`)

func normalizeBacklogDate(s string) string {
	return backlogDate.ReplaceAllString(s, "<date>")
}

func goldReviewRows(t *testing.T) []goldReviewRow {
	tmp := t.TempDir()
	return []goldReviewRow{
		// The pinned source of T6's ok@<sha> token: the ok outcome, under
		// the step key, with no reason stored.
		goldReviewDriveRow(t, "review-ok-lands", reviewLanes, goldRoot,
			map[string]ModelResult{"review": {StepID: 101, Kind: "completed", Outcome: "ok"}}),
		// The same stored shape, plus the O7 backlog append.
		goldReviewDriveRow(t, "review-ok-with-finding", reviewLanes, tmp,
			map[string]ModelResult{"review": {StepID: 101, Kind: "completed", Outcome: "ok", Reason: "the error handler swallows the panic"}}),
		// The stored third value: unverified is stored, reject is routed.
		goldReviewDriveRow(t, "review-reject-unverified-stores", reviewLanesWithRework, goldRoot,
			map[string]ModelResult{
				"review": {StepID: 101, Kind: "completed", Outcome: "reject", Reason: "unverified: the gate could not run: no verifier registered"},
				"rework": {StepID: 102, Kind: "completed", Outcome: "ok"},
			}),
	}
}

func writeGoldReviewEvidence(t *testing.T, rows []goldReviewRow) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(goldReviewEvidence), 0700); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(goldReviewEvidence, append(data, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}

// TestReviewEvidenceGoldens — where the review rung's outcome lives. The
// AEON_WRITE_GOLDEN run on the untouched tree generated the fixture;
// afterwards this test only READS it and fails on any drift, naming the
// row. T6 (VG7) reads `StepStates[stepKey(pos)].Outcome`/`.Reason` for
// the ok@<sha> token and the unverified charging; a move of either byte
// is a deliberate, listed amendment in that task's Notes.
func TestReviewEvidenceGoldens(t *testing.T) {
	rows := goldReviewRows(t)
	got, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')

	if os.Getenv("AEON_WRITE_GOLDEN") == "1" {
		writeGoldReviewEvidence(t, rows)
		t.Logf("wrote %s (%d rows)", goldReviewEvidence, len(rows))
		return
	}
	want, err := os.ReadFile(goldReviewEvidence)
	if err != nil {
		t.Fatalf("no golden: %v — run once with AEON_WRITE_GOLDEN=1 on the untouched tree (S57 T1)", err)
	}
	if string(got) != string(want) {
		t.Fatalf("the review evidence moved off its golden:\n--- golden ---\n%s--- current ---\n%sT6 (VG7) reads the ok outcome and the unverified reason from exactly this shape; a move is a deliberate, listed amendment in that task's Notes.", want, got)
	}
}
