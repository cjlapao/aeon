package tools

// bash_golden_test.go — S59 T1 (characterization, P2 Phase A). The model-
// facing bytes of the `bash` tool, pinned on the untouched code: what the
// model reads (the `Content` incl. the renderRun layout), the `Summary`
// that rides the event stream, the three-state `ExitCode`, and the exact
// refusal texts of the boundary paths that never reach a spawn.
//
// T5 changes what the child SEES (the environment), not what the model
// reads: T5 must leave every golden here byte-identical. The only part of
// the bytes a golden cannot pin to the byte is the wall time, which is
// replaced by the fixed token "N" in the copy that is compared — a golden
// that held a real clock would drift with the machine. Every non-clock
// byte, including the clock's POSITION, is pinned.
//
// Generation (run once, on the untouched tree):
//
//	AEON_WRITE_GOLDEN=1 go test ./internal/tools/ -run TestBashResultBytesGolden -count=1 -tags sqlite_fts5

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/policy"
)

const goldBashDir = "bash_result"

// bashWallRe normalizes a rendered duration ("42ms", "1.001s") to the fixed
// token N so the goldens hold every byte except the clock reading.
var bashWallRe = regexp.MustCompile(`\d+(?:\.\d+)?(?:ns|µs|ms|s)`)

func bashVerdictSeam(t *testing.T) func(command string) (policy.Verdict, bool) {
	t.Helper()
	return func(command string) (policy.Verdict, bool) {
		v := policy.Evaluate(
			[]policy.Policy{commandRowT(t, "bash-golden-allow", "*", "allow")},
			policy.Request{Type: policy.CommandAccess, Agent: "bash-golden", Tool: "bash", Command: command},
			time.Now(),
		)
		return v, true
	}
}

type bashGoldRow struct {
	Case     string `json:"case"`
	Summary  string `json:"summary"`
	Content  string `json:"content"`
	IsError  bool   `json:"isError"`
	ExitCode *int   `json:"exitCode"`
}

// TestBashResultBytesGolden — the same commands through `runBash` (the tool
// boundary) rather than `Spawn` (the process layer): the policy seam, the
// boundary refusals, and the model-visible Result, byte for byte.
func TestBashResultBytesGolden(t *testing.T) {
	write := os.Getenv("AEON_WRITE_GOLDEN") == "1"
	goldDir := filepath.Join("testdata", goldBashDir)
	if write {
		if err := os.MkdirAll(goldDir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	type spec struct {
		Case   string
		Input  string
		NoSeam bool
	}
	specs := []spec{
		{Case: "success", Input: `{"command":"printf 'hi\n'"}`},
		{Case: "nonzero-exit", Input: `{"command":"exit 3"}`},
		{Case: "timeout", Input: `{"command":"sleep 30","timeoutSeconds":1}`},
		{Case: "no-seam", Input: `{"command":"true"}`, NoSeam: true},
		{Case: "oversized", Input: `{"command":"` + strings.Repeat("a", 65537) + `"}`},
		{Case: "empty-command", Input: `{"command":"  "}`},
		{Case: "bad-json", Input: `not json`},
	}

	for _, sp := range specs {
		sp := sp
		t.Run(sp.Case, func(t *testing.T) {
			tc := ToolContext{WorkspaceRoot: t.TempDir(), CommandVerdict: bashVerdictSeam(t), Exec: testExecSeam(t)}
			if sp.NoSeam {
				tc.CommandVerdict = nil
			}
			res := runBash(context.Background(), tc, json.RawMessage(sp.Input))

			row := bashGoldRow{Case: sp.Case,
				Summary:  bashWallRe.ReplaceAllString(res.Summary, "N"),
				Content:  bashWallRe.ReplaceAllString(res.Content, "N"),
				IsError:  res.IsError,
				ExitCode: res.ExitCode}

			// Live assertions that do not depend on the golden file:
			switch sp.Case {
			case "success":
				if res.ExitCode == nil || *res.ExitCode != 0 {
					t.Fatalf("exit code %v, want 0 (nil means it never ran)", res.ExitCode)
				}
				if res.IsError {
					t.Fatal("a command that ran is not an error, whatever it returned")
				}
				// The command echoes back as written — its \n is a live newline
				// in the line, and the layout is the dollar line, the exit line,
				// the blank, the stream.
				if !strings.HasPrefix(res.Content, "$ printf 'hi\n'\nexit 0 · ") {
					t.Fatalf("the render layout drifted at its head: %q", res.Content)
				}
			case "nonzero-exit":
				if res.ExitCode == nil || *res.ExitCode != 3 {
					t.Fatalf("exit code %v, want 3", res.ExitCode)
				}
				if res.IsError {
					t.Fatal("a non-zero exit is a fact the model asked for, not a tool error")
				}
			case "timeout":
				if !strings.Contains(res.Content, "TIMED OUT after ") {
					t.Fatalf("the timeout did not render as a timeout: %q", res.Content)
				}
			case "no-seam", "oversized", "empty-command", "bad-json":
				if !res.IsError || res.ExitCode != nil {
					t.Fatalf("the refusal must be an error with no exit code: %+v", res)
				}
			}
			if sp.Case == "no-seam" && !strings.Contains(res.Content, "no command-verdict seam") {
				t.Fatalf("the no-seam refusal does not name the missing seam: %s", res.Content)
			}
			if sp.Case == "oversized" && !strings.Contains(res.Content, "65537-byte command") {
				t.Fatalf("the oversized refusal does not name the size: %s", res.Content)
			}

			path := filepath.Join(goldDir, sp.Case+".json")
			if write {
				data, err := json.MarshalIndent(row, "", "  ")
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			data, rerr := os.ReadFile(path)
			if rerr != nil {
				t.Fatalf("golden missing (generate with AEON_WRITE_GOLDEN=1 on the untouched tree): %v", rerr)
			}
			var want bashGoldRow
			if err := json.Unmarshal(data, &want); err != nil {
				t.Fatal(err)
			}
			if want.Content != row.Content {
				t.Errorf("the model-visible bytes drifted:\n  now:  %q\n  gold: %q", row.Content, want.Content)
			}
			if want.Summary != row.Summary {
				t.Errorf("the event-stream summary drifted:\n  now:  %q\n  gold: %q", row.Summary, want.Summary)
			}
			if want.IsError != row.IsError {
				t.Errorf("IsError %v, golden %v", row.IsError, want.IsError)
			}
			if (want.ExitCode == nil) != (row.ExitCode == nil) ||
				(want.ExitCode != nil && row.ExitCode != nil && *want.ExitCode != *row.ExitCode) {
				t.Errorf("ExitCode %v, golden %v (nil is load-bearing three-state honesty)", row.ExitCode, want.ExitCode)
			}
		})
	}
}
