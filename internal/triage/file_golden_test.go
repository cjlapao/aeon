package triage

// file_golden_test.go — S57 T1 (characterization, P2 Phase A). Triage
// filing, pinned on the untouched tree (1ae086c0) with a throwaway store
// and a fixed clock: the stored DEFAULTS an agent-filed bug enters with
// (`unvalidated`, `unknown` priority, the filer verbatim), the DEDUPE
// answer — there is none at filing: an identical AND a near-identical
// title each create a fresh bug (the vector suggestion detector is a
// separate, human-confirmed path — T0 item 8), and the REFUSALS verbatim
// (empty filer, the D2 label-on-unknown contradiction, a missing component
// root).
//
// T7 adds the post-merge watch's automatic filing through this same door
// with `FiledBy = "system:gates"`; it must not change any default, add
// filing-time dedupe, or move any refusal sentence — the diff is the
// report.
//
// Generation (run once, on the untouched tree):
//
//	AEON_WRITE_GOLDEN=1 go test ./internal/triage/ -run TestTriageFileGoldens -count=1 -tags sqlite_fts5

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/store"
)

const goldTriageDir = "file_golden"

// goldTriageRow pins one filing: the stored bug (or the refusal sentence).
type goldTriageRow struct {
	Case        string `json:"case"`
	Refused     bool   `json:"refused"`
	Sentence    string `json:"sentence,omitempty"`
	Bug         any    `json:"bug,omitempty"`
	SecondID    int64  `json:"secondId,omitempty"` // for the dedupe cases
	SecondDupOf *int64 `json:"secondDupOf,omitempty"`
}

// openGoldenStore opens a throwaway store on a FIXED clock so the pinned
// timestamps are reproducible byte for byte.
func openGoldenStore(t *testing.T, root string) *Triage {
	t.Helper()
	s, err := store.Open(store.Options{
		Dir:   t.TempDir(),
		Env:   func(string) string { return "" },
		Clock: func() time.Time { return time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("open the test store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if _, err := s.CreateComponent(store.NewComponent{Name: "r", Slug: "r", Root: root}); err != nil {
		t.Fatalf("seed the component for %s: %v", root, err)
	}
	return New(s)
}

type triageCase struct {
	name string
	run  func(t *testing.T, r *Triage) goldTriageRow
}

func goldTriageCases() []triageCase {
	const root = "/repo/golden"
	agentFiling := NewBug{Title: "loop stalls on empty input", Body: "the loop stalls when the input is empty", FiledBy: "loop-agent-7"}
	return []triageCase{
		{
			name: "agent-filed-defaults",
			run: func(t *testing.T, r *Triage) goldTriageRow {
				b, ref := r.Create(root, agentFiling)
				return rowOf(b, ref)
			},
		},
		{
			// The DEDUPE answer, pin one: an IDENTICAL title (and body)
			// filed twice creates TWO bugs. Nothing at filing time matches.
			name: "identical-title-creates-both",
			run: func(t *testing.T, r *Triage) goldTriageRow {
				first, ref := r.Create(root, agentFiling)
				if ref != nil {
					return goldTriageRow{Refused: true, Sentence: ref.Reason}
				}
				second, ref := r.Create(root, agentFiling)
				if ref != nil {
					return goldTriageRow{Refused: true, Sentence: ref.Reason}
				}
				row := rowOf(first, nil)
				row.SecondID = second.ID
				row.SecondDupOf = second.DuplicateOf
				return row
			},
		},
		{
			// Pin two: a NEAR-identical title likewise creates a fresh bug
			// — the difference the filing door sees is none at all.
			name: "near-identical-title-creates-both",
			run: func(t *testing.T, r *Triage) goldTriageRow {
				first, ref := r.Create(root, agentFiling)
				if ref != nil {
					return goldTriageRow{Refused: true, Sentence: ref.Reason}
				}
				near := agentFiling
				near.Title = "loop stalls on empty inputs"
				second, ref := r.Create(root, near)
				if ref != nil {
					return goldTriageRow{Refused: true, Sentence: ref.Reason}
				}
				row := rowOf(first, nil)
				row.SecondID = second.ID
				row.SecondDupOf = second.DuplicateOf
				return row
			},
		},
		{
			name: "empty-filer-refused",
			run: func(t *testing.T, r *Triage) goldTriageRow {
				in := agentFiling
				in.FiledBy = ""
				_, ref := r.Create(root, in)
				return rowOf(Bug{}, ref)
			},
		},
		{
			name: "label-on-unknown-priority-refused",
			run: func(t *testing.T, r *Triage) goldTriageRow {
				in := agentFiling
				in.PriorityLabel = "high" // state left unknown — the D2 contradiction
				_, ref := r.Create(root, in)
				return rowOf(Bug{}, ref)
			},
		},
		{
			name: "missing-component-refused",
			run: func(t *testing.T, r *Triage) goldTriageRow {
				_, ref := r.Create("/repo/not-seeded", agentFiling)
				return rowOf(Bug{}, ref)
			},
		},
	}
}

func rowOf(b Bug, ref *Refusal) goldTriageRow {
	if ref != nil {
		return goldTriageRow{Refused: true, Sentence: ref.Reason}
	}
	return goldTriageRow{Bug: b}
}

// normJSON canonicalizes a value to a key-sorted JSON object string so the
// comparison is order-insensitive (a Bug struct marshals in field order; the
// golden round-trips as a map, which marshals sorted).
func normJSON(t *testing.T, v any) (string, error) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return "", err
	}
	out, err := json.Marshal(m) // map keys marshal sorted: canonical order
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func writeGoldTriageRow(t *testing.T, row goldTriageRow) {
	t.Helper()
	dir := filepath.Join("testdata", goldTriageDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(row, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, row.Case+".json"), append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func compareGoldTriageRow(t *testing.T, want, got goldTriageRow) {
	t.Helper()
	if want.Refused != got.Refused {
		t.Fatalf("%s: refused=%v, golden says %v", got.Case, got.Refused, want.Refused)
	}
	if want.Refused {
		if want.Sentence != got.Sentence {
			t.Fatalf("%s: refusal drifted\n  now:  %q\n  gold: %q", got.Case, got.Sentence, want.Sentence)
		}
		return
	}
	wj, _ := normJSON(t, want.Bug)
	gj, _ := normJSON(t, got.Bug)
	if wj != gj {
		t.Fatalf("%s: stored bug drifted\n  now:  %s\n  gold: %s", got.Case, gj, wj)
	}
	if want.SecondID != got.SecondID || (want.SecondDupOf == nil) != (got.SecondDupOf == nil) {
		t.Fatalf("%s: second-filing shape drifted", got.Case)
	}
	if want.SecondDupOf != nil && got.SecondDupOf != nil && *want.SecondDupOf != *got.SecondDupOf {
		t.Fatalf("%s: second bug's duplicate_of drifted", got.Case)
	}
}

// TestTriageFileGoldens — the filing door's current behaviour, pinned.
// The AEON_WRITE_GOLDEN run on the untouched tree generated the fixtures;
// afterwards this test only READS them and fails on any drift.
func TestTriageFileGoldens(t *testing.T) {
	write := os.Getenv("AEON_WRITE_GOLDEN") == "1"
	for _, tc := range goldTriageCases() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			r := openGoldenStore(t, "/repo/golden")
			row := tc.run(t, r)
			row.Case = tc.name
			if write {
				writeGoldTriageRow(t, row)
				return
			}
			data, err := os.ReadFile(filepath.Join("testdata", goldTriageDir, tc.name+".json"))
			if err != nil {
				t.Fatalf("golden %s missing (generate with AEON_WRITE_GOLDEN=1 on the untouched tree): %v", tc.name, err)
			}
			var want goldTriageRow
			if err := json.Unmarshal(data, &want); err != nil {
				t.Fatal(err)
			}
			compareGoldTriageRow(t, want, row)
		})
	}
}
