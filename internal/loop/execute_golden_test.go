package loop

// execute_golden_test.go — S56 T1 (characterization). The two outward
// actions, driven through the real loop engine (Start → Run) with the
// forge fake as the witness, pinned on the untouched tree (169cf1ee,
// pre-S56) across all four attended/allowOutward combos each: the block
// reason, the ask's question row, and the fake's call log. The spirit of
// allowoutward_test.go:103-192 in golden form — a later byte-diff is
// trivial instead of a re-derivation.
//
// Later tasks this slice expects to change this file: T4 — and it may
// only ADD rows (new fixtures with a grant); it must not touch these
// eight. T5 adds its own zero-context equivalence test for
// ResolveWithGrant and does not touch this file. No task is expected to
// move an existing row (AL9/AL16).
//
// Determinism: fixed slugs, a fixed FAKE root (the ask renders the root
// as the repository line, so a temp directory would drift the body), a
// fresh store per row (the first question row is always id 1), and
// with=taskIdentity() only where the action can run or be asked — a deny
// that fires before argument validation is the stronger property, and
// the row says so with withTask=false.
//
// Generation (run once, on the untouched tree):
//
//	AEON_WRITE_GOLDEN=1 go test ./internal/loop/ -run TestExecuteMatrixIsTheGolden -count=1 -tags sqlite_fts5

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/action"
	"github.com/cjlapao/aeon/internal/forge"
	"github.com/cjlapao/aeon/internal/store"
)

// goldRoot is the fake root every row pins beside its blueprint: the ask
// renders it as the repository, and the forge fake never touches the
// filesystem, so a fixed string is both honest and stable.
const goldRoot = "/repo/golden"

type goldExecuteRow struct {
	Action       string         `json:"action"`
	Attended     bool           `json:"attended"`
	AllowOutward bool           `json:"allowOutward"`
	WithTask     bool           `json:"withTask"`
	Status       string         `json:"status"`
	LastReason   string         `json:"lastReason"`
	QuestionID   int64          `json:"questionId"`
	QuestionKind string         `json:"questionKind,omitempty"`
	QuestionBody string         `json:"questionBody,omitempty"`
	QuestionOpts []string       `json:"questionOptions,omitempty"`
	FakeCalls    []goldFakeCall `json:"fakeCalls"`
}

type goldFakeCall struct {
	Method string `json:"method"`
	Args   []any  `json:"args"`
}

const goldExecuteMatrix = "testdata/execute_matrix.json"

// goldBranchProbe is openPR's CHECK-ONLY branch probe, wired "yes": it
// exists so the action can reach the forge, and it stands for a head that
// commitAndPush already pushed. The golden pins the loop's behaviour GIVEN
// a pushed head, not the git client's behaviour — and the deny rows never
// consult it at all (the verdict fires before dispatch).
type goldBranchProbe struct{}

func (goldBranchProbe) BranchExistsRemote(context.Context, string, string) (bool, error) {
	return true, nil
}

func TestExecuteMatrixIsTheGolden(t *testing.T) {
	rows := make([]goldExecuteRow, 0, 8)
	for _, name := range []string{action.ForgeOpenPR, action.ForgeMerge} {
		for _, attended := range []bool{false, true} {
			for _, allowOutward := range []bool{false, true} {
				rows = append(rows, goldDriveRow(t, name, attended, allowOutward))
			}
		}
	}
	got, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')

	if os.Getenv("AEON_WRITE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(goldExecuteMatrix), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldExecuteMatrix, got, 0600); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s (%d rows)", goldExecuteMatrix, len(rows))
		return
	}

	want, err := os.ReadFile(goldExecuteMatrix)
	if err != nil {
		t.Fatalf("no golden: %v — run once with AEON_WRITE_GOLDEN=1 on the untouched tree (S56 T1)", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("the execute matrix moved off its golden:\n--- golden ---\n%s--- current ---\n%sThe eight pre-S56 rows are the contract (AL9/AL16); a move here is a deliberate, listed amendment in that task's Notes — not a re-generated file.", want, got)
	}
}

// goldDriveRow drives one combo to its durable end and reads back what
// the pin, the question row and the fake saw.
func goldDriveRow(t *testing.T, actionName string, attended, allowOutward bool) goldExecuteRow {
	t.Helper()
	s := openLoopStore(t)
	f := forge.NewFake()
	path := filepath.Join(t.TempDir(), "gold.yaml")
	writeBlueprint(t, path, "  - id: act\n    step: {kind: code-action, action: "+actionName+"}\n")

	// with=taskIdentity() exactly where the action can run or be asked:
	// openPR on every non-deny row (the run needs its required args, the
	// ask needs its O1 target); merge on the attended rows (the ask
	// derives the base ref from the task identity and then refuses the
	// missing pr number — evidence, never prose). The deny rows carry nil:
	// a denial that fires before argument validation is the stronger
	// property, and the golden records that it did not even need the args.
	var with map[string]string
	withTask := false
	switch {
	case actionName == action.ForgeOpenPR && (attended || allowOutward):
		with, withTask = taskIdentity(), true
	case actionName == action.ForgeMerge && attended:
		with, withTask = taskIdentity(), true
	}

	slug := fmt.Sprintf("gld-%s-att-%v-out-%v", strings.ToLower(strings.ReplaceAll(actionName, "forge.", "")), attended, allowOutward)
	st, err := Start(context.Background(), s, store.NewLoop{
		Slug: slug, ComponentRoot: goldRoot, Root: goldRoot, Mode: "standard",
		AllowOutward: allowOutward,
	}, path, boolPtr(attended), with)
	if err != nil {
		t.Fatal(err)
	}
	// The same wiring svcWith builds, plus the branch probe — the
	// existing harness cannot lift an openPR to the forge (the probe is
	// a type guarantee the loop test file is allowed to stand in for),
	// and the run row's witness is the fake's log, not a block reason.
	svc := &action.Services{Forge: &action.ForgeActions{Forge: f, Git: goldBranchProbe{}}}
	dep := Deps{Store: s, Actions: func(string) *action.Services { return svc }}
	if err := Run(context.Background(), dep, st.ID); err != nil {
		t.Fatal(err)
	}
	saved, err := Pinned(context.Background(), s, st.ID)
	if err != nil {
		t.Fatal(err)
	}

	row := goldExecuteRow{
		Action:       actionName,
		Attended:     attended,
		AllowOutward: allowOutward,
		WithTask:     withTask,
		Status:       saved.Status,
		LastReason:   saved.LastReason,
		QuestionID:   saved.QuestionID,
		FakeCalls:    []goldFakeCall{},
	}
	if saved.QuestionID > 0 {
		q, err := s.ReadQuestion(saved.QuestionID)
		if err != nil {
			t.Fatal(err)
		}
		row.QuestionKind = q.Kind
		row.QuestionBody = q.Body
		row.QuestionOpts = q.Options
	}
	for _, c := range f.Calls() {
		row.FakeCalls = append(row.FakeCalls, goldFakeCall{Method: c.Method, Args: c.Args})
	}
	return row
}

// TestUnattendedMergeNeverRunsTodayEndToEnd — the spec table's pin as a
// hard assertion (the golden records the same row as data): an unattended
// loop WITH allowOutward declared reaches forge.merge and the
// irreversible rule denies it — the sentence names the class AND that the
// flag did not lift it, and the forge fake saw zero merge calls. A policy
// table can say deny while the handler runs anyway; only the drive sees
// that.
func TestUnattendedMergeNeverRunsTodayEndToEnd(t *testing.T) {
	s := openLoopStore(t)
	f := forge.NewFake()
	path := filepath.Join(t.TempDir(), "irrev.yaml")
	writeBlueprint(t, path, "  - id: act\n    step: {kind: code-action, action: forge.merge}\n")
	st, err := Start(context.Background(), s, store.NewLoop{
		Slug: "gld-irrev", ComponentRoot: goldRoot, Root: goldRoot, Mode: "standard",
		AllowOutward: true,
	}, path, boolPtr(false), nil)
	if err != nil {
		t.Fatal(err)
	}
	dep := Deps{Store: s, Actions: func(string) *action.Services { return svcWith(f) }}
	if err := Run(context.Background(), dep, st.ID); err != nil {
		t.Fatal(err)
	}
	saved, err := Pinned(context.Background(), s, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := "forge.merge denied — effect class 'irreversible' resolves to deny (loop is unattended; allowOutward does not grant it)"
	if saved.Status != "blocked" || saved.LastReason != want {
		t.Fatalf("the irreversible rule moved: status %q, reason %q\nwant: %q", saved.Status, saved.LastReason, want)
	}
	for _, c := range f.Calls() {
		if strings.Contains(c.Method, "Merge") {
			t.Fatalf("the fake saw a merge call (%+v) while the loop says blocked — the composition failed between policy and dispatch", c)
		}
	}
}
