package action

// resolve_golden_test.go — S56 T1 (characterization). The WHOLE Resolve
// matrix pinned byte-for-byte on the untouched tree (169cf1ee, pre-S56):
// every registry name (Names()) × attended {false, true} × allowOutward
// {false, true} — 36 rows today — with the class, the verdict and the
// sentence each outcome resolves to.
//
// Later tasks this slice expects to change this file: NONE. AL9/AL16
// promise byte-identical Resolve behaviour; the S56 tasks layer grants
// BESIDE it (T5's ResolveWithGrant has its own zero-context equivalence
// test) and must not edit the matrix. A registry addition fails here with
// "a new action needs a deliberate golden row" — a new irreversible
// action has to be a conscious decision (AL2), and the decision shows up
// in this diff.
//
// Generation (run once, on the untouched tree):
//
//	AEON_WRITE_GOLDEN=1 go test ./internal/action/ -run TestResolveMatrixIsTheGolden -count=1
//
// The golden is committed; the test regenerates the matrix in memory and
// compares byte-for-byte. Rows are ordered by sorted registry name, then
// attended false→true, then allowOutward false→true — the only order that
// makes a new name a visible append-or-diff instead of a shuffle.

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// resolveRow is one matrix cell, in the order the file must read it.
type resolveRow struct {
	Action       string `json:"action"`
	Attended     bool   `json:"attended"`
	AllowOutward bool   `json:"allowOutward"`
	Class        string `json:"class"`
	Verdict      string `json:"verdict"`
	Sentence     string `json:"sentence"`
}

const resolveMatrixGolden = "testdata/resolve_matrix.json"

func TestResolveMatrixIsTheGolden(t *testing.T) {
	rows := make([]resolveRow, 0, len(Names())*4)
	for _, name := range Names() { // the closed set, already sorted
		for _, attended := range []bool{false, true} {
			for _, allowOutward := range []bool{false, true} {
				res, err := Resolve(name, attended, allowOutward)
				if err != nil {
					t.Fatalf("Resolve(%q): %v — a registered name that does not resolve is a registry bug, not a golden gap", name, err)
				}
				rows = append(rows, resolveRow{
					Action:       name,
					Attended:     attended,
					AllowOutward: allowOutward,
					Class:        string(res.Class),
					Verdict:      string(res.Verdict),
					Sentence:     res.Sentence,
				})
			}
		}
	}
	got, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')

	if os.Getenv("AEON_WRITE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(resolveMatrixGolden), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(resolveMatrixGolden, got, 0600); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s (%d rows)", resolveMatrixGolden, len(rows))
		return
	}

	want, err := os.ReadFile(resolveMatrixGolden)
	if err != nil {
		t.Fatalf("no golden: %v — run once with AEON_WRITE_GOLDEN=1 on the untouched tree (S56 T1)", err)
	}

	// The deliberate-row check BEFORE the byte diff: a registry name with
	// no golden row must fail in its own words, and a golden row whose
	// action the registry no longer knows is the mirror of the same
	// unreviewed change.
	var golden []resolveRow
	if err := json.Unmarshal(want, &golden); err != nil {
		t.Fatalf("the golden does not parse as a matrix: %v", err)
	}
	inGolden := map[string]bool{}
	for _, r := range golden {
		inGolden[r.Action] = true
	}
	for _, name := range Names() {
		if !inGolden[name] {
			t.Fatalf("a new action needs a deliberate golden row: %q is registered but has no rows in %s — add its four Resolve outcomes on purpose (AL2: a new irreversible action is a conscious decision), never by drifting the slice", name, resolveMatrixGolden)
		}
	}
	for _, r := range golden {
		if _, ok := Lookup(r.Action); !ok {
			t.Fatalf("the golden still rows an action the closed registry no longer knows: %q — delete it deliberately, in the same change as the registry edit", r.Action)
		}
	}

	if !bytes.Equal(got, want) {
		t.Fatalf("the Resolve matrix moved off its golden:\n--- golden ---\n%s--- current ---\n%sThe 36 outcomes and their sentences are the pre-S56 contract (AL9/AL16); a move here is a deliberate, listed amendment in that task's Notes — not a re-generated file.", want, got)
	}
}
