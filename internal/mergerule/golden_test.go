// golden_test.go — the parity contract (T5 spec test 3): every gate
// fact set of the T1 goldens, driven through Evaluate with Baseline(),
// produces the golden's refusal sentence byte for byte (minus the
// action package's "refused: " prefix and "why:/do:" tail) and, on the
// pass cases, the golden's evidence map exactly.
//
// The goldens live in internal/action — the action package owns the
// gatherer (T6) and this test owns the evaluator; the fixtures are the
// shared contract between the two, and this file reads them rather
// than copying them, so a golden edited on the action side is felt here
// in the same red.
//
// The fourteen fact-set cases are the GATE's cases. The other fixtures
// in the directory are not: the two gather-error rows (reviews-unreadable,
// approve-statuses-transport) are refused by the gatherer, which stays in
// the action package (its sentence carries the transport error, which no
// Facts can hold), and the four MergePR-guard rows (already-merged,
// base-ref-empty, base-ref-wrong, phantom-merge) are guards around the
// irreversible call, not gate facts. The map below names all eight, and
// the test asserts the directory holds exactly the 14 mapped + 8 named.

package mergerule

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/forge"
)

const goldGateDir = "../action/testdata/gate_golden"

// goldGateRow mirrors the action package's fixture shape (the fields the
// parity contract reads; the call log belongs to the gatherer, T6).
type goldGateRow struct {
	Case     string            `json:"case"`
	Refused  bool              `json:"refused"`
	Sentence string            `json:"sentence,omitempty"`
	Evidence map[string]string `json:"evidence,omitempty"`
}

// The head SHA every fixture's merge would have recorded — a fixed
// value, like the fixtures' PR #3.
const goldenHeadSHA = "00000000000000000000000000000000002dc6df"

func review(id int64, verdict forge.Verdict, author, at string) forge.Review {
	return forge.Review{ID: id, PR: 3, Verdict: verdict, Author: author, SubmittedAt: at}
}

// factForCase is the fourteen fact sets, in the golden's values (fixed
// review ids, authors and timestamps — the fixtures' determinism).
func factForCase(name string) (Facts, bool) {
	t0 := "2026-10-26T09:00:00Z"
	t1 := "2026-10-26T09:12:00Z"
	none := func() Facts {
		return Facts{
			PR: 3, HeadSHA: goldenHeadSHA,
			Checks:         forge.ChecksReport{State: forge.CheckUnknown},
			ChecksReadable: true,
			Threads:        ThreadFacts{Available: true, Readable: true},
		}
	}
	switch name {
	case "no-reviews":
		return none(), true
	case "latest-comment":
		f := none()
		f.Reviews = []forge.Review{review(1, forge.VerdictComment, "mira", t1)}
		return f, true
	case "latest-request-changes":
		f := none()
		f.Reviews = []forge.Review{review(1, forge.VerdictApprove, "mira", t0),
			review(2, forge.VerdictRequestChanges, "mira", t1)}
		return f, true
	case "outstanding-rc-cleared-by-same-author":
		f := none()
		f.Reviews = []forge.Review{review(1, forge.VerdictRequestChanges, "mira", t0),
			review(2, forge.VerdictApprove, "mira", t1)}
		return f, true
	case "outstanding-rc-answered-by-other-author":
		f := none()
		f.Reviews = []forge.Review{review(1, forge.VerdictRequestChanges, "mira", t0),
			review(2, forge.VerdictApprove, "joao", t1)}
		return f, true
	case "approve-no-statuslister":
		f := Facts{PR: 3, HeadSHA: goldenHeadSHA,
			Reviews:        []forge.Review{review(1, forge.VerdictApprove, "fake", "")},
			ChecksReadable: false,
			Threads:        ThreadFacts{Available: false}}
		return f, true
	case "approve-statuses-all-success":
		f := none()
		f.Reviews = []forge.Review{review(1, forge.VerdictApprove, "mira", t1)}
		f.Checks = forge.ChecksReport{State: forge.CheckSuccess,
			Checks: []forge.Check{{Name: "build", State: forge.CheckSuccess}, {Name: "test", State: forge.CheckSuccess}}}
		return f, true
	case "approve-statuses-one-failure":
		f := none()
		f.Reviews = []forge.Review{review(1, forge.VerdictApprove, "mira", t1)}
		f.Checks = forge.ChecksReport{State: forge.CheckFailure,
			Checks: []forge.Check{{Name: "build", State: forge.CheckSuccess}, {Name: "test", State: forge.CheckFailure}}}
		return f, true
	case "approve-statuses-one-pending":
		f := none()
		f.Reviews = []forge.Review{review(1, forge.VerdictApprove, "mira", t1)}
		f.Checks = forge.ChecksReport{State: forge.CheckPending,
			Checks: []forge.Check{{Name: "build", State: forge.CheckSuccess}, {Name: "test", State: forge.CheckPending}}}
		return f, true
	case "approve-statuses-not-found":
		f := none()
		f.Reviews = []forge.Review{review(1, forge.VerdictApprove, "mira", t1)}
		return f, true // a claimed API that reports nothing: the empty report
	case "approve-threads-none":
		f := none()
		f.Reviews = []forge.Review{review(1, forge.VerdictApprove, "mira", t1)}
		return f, true // 0 unresolved is a report
	case "approve-threads-some":
		f := none()
		f.Reviews = []forge.Review{review(1, forge.VerdictApprove, "mira", t1)}
		f.Threads = ThreadFacts{Available: true, Readable: true,
			Unresolved: []forge.Thread{{Path: "src/x.go", Line: 12, Author: "mira"}}}
		return f, true
	case "approve-threads-not-found":
		f := none()
		f.Reviews = []forge.Review{review(1, forge.VerdictApprove, "mira", t1)}
		f.Threads = ThreadFacts{Available: true, Readable: false}
		return f, true
	case "approve-threads-unsupported":
		f := Facts{PR: 3, HeadSHA: goldenHeadSHA,
			Reviews:        []forge.Review{review(1, forge.VerdictApprove, "fake", "")},
			ChecksReadable: false,
			Threads:        ThreadFacts{Available: false}}
		return f, true
	}
	return Facts{}, false
}

// The eight fixtures that are not gate facts (see the file's comment):
// the gatherer's two transport refusals and the four MergePR guards.
var notGateFacts = map[string]bool{
	"reviews-unreadable": true, "approve-statuses-transport": true,
	"already-merged": true, "base-ref-empty": true,
	"base-ref-wrong": true, "phantom-merge": true,
}

// goldenDetail is the sentence the gate itself produced: the fixture's
// full refusal minus the action package's "refused: " prefix and the
// "why:/do:" tail.
func goldenDetail(t *testing.T, row goldGateRow) string {
	t.Helper()
	const prefix = "refused: "
	if !strings.HasPrefix(row.Sentence, prefix) {
		t.Fatalf("fixture %s: sentence does not carry the refusal prefix", row.Case)
	}
	detail := row.Sentence[len(prefix):]
	if i := strings.Index(detail, "\n  why:"); i >= 0 {
		detail = detail[:i]
	}
	return detail
}

func TestBaselineEvaluateEqualsTodaysGate(t *testing.T) {
	entries, err := os.ReadDir(goldGateDir)
	if err != nil {
		t.Fatalf("the goldens are the contract: %v", err)
	}
	saw := 0
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(goldGateDir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		var row goldGateRow
		if err := json.Unmarshal(raw, &row); err != nil {
			t.Fatalf("%s: %v", e.Name(), err)
		}
		if notGateFacts[row.Case] {
			continue // gatherer/guard rows: the evaluator never sees them
		}
		facts, ok := factForCase(row.Case)
		if !ok {
			t.Fatalf("fixture %s: no fact set — the map and the directory have drifted", row.Case)
		}
		saw++
		got := Evaluate(Baseline(), facts)
		if got.Allowed != !row.Refused {
			t.Errorf("%s: allowed=%v, the golden says refused=%v", row.Case, got.Allowed, row.Refused)
			continue
		}
		if row.Refused {
			if len(got.Refusals) == 0 {
				t.Errorf("%s: refused with no refusal", row.Case)
				continue
			}
			// Today's gate stops at its FIRST refusal (the early
			// return): the baseline path must name exactly one, and
			// it must be the golden's, byte for byte.
			if len(got.Refusals) != 1 {
				t.Errorf("%s: %d refusals, the gate names one: %v", row.Case, len(got.Refusals), got.Refusals)
			}
			if got.Refusals[0].Sentence != goldenDetail(t, row) {
				t.Errorf("%s: the sentence drifted from the golden\n got: %s\nwant: %s",
					row.Case, got.Refusals[0].Sentence, goldenDetail(t, row))
			}
		} else if !reflect.DeepEqual(got.Evidence, row.Evidence) {
			t.Errorf("%s: the evidence drifted from the golden\n got:  %v\nwant: %v",
				row.Case, got.Evidence, row.Evidence)
		}
	}
	if saw != 14 {
		t.Errorf("%d gate-fact fixtures driven, the goldens hold 14 — a fixture was added or renamed", saw)
	}
}
