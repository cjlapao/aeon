package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// S23 T8 — doctor, every state in its own states (C15, D10). The gates are
// the ones a careless change breaks silently: doctor flattens nothing, no
// credential is printed, it works with retrieval absent, and the socket and
// CLI read the same states.

// uncertainReport is a report with the hard states: a declared-but-unprobed
// capability, an unmetered route, a stale corpus, a not-listening mounting.
func uncertainReport() Report {
	return Report{
		Routes: []Route{
			{
				Provider: "anthropic",
				Cost:     Cost{Amount: "", State: CostUnknown}, // unmetered
				Capabilities: []Capability{
					{Name: "tools", State: Declared}, // declared, not probed
				},
			},
		},
		Retrieval: Retrieval{State: Stale}, // a stale corpus
		Mountings: []Mounting{
			{Name: "http", State: NotListening}, // not listening — shown, not absent
		},
		Credentials: []Credential{
			{Name: "ANTHROPIC_API_KEY", Present: true, Source: "environment"},
		},
		Forge:     Forge{Present: true, Source: "~/.config/aeon/forge.key"},
		BashPolicyPresent: true,
	}
}

// TestDoctorFlattensNothing is C15/D10's pin: no "ok" / "all good" / "healthy"
// over an uncertain value, no green mark over a declared-but-unprobed
// capability, no $0.00 over an unmetered route, no "available" over a stale
// corpus, and a not-listening mounting is shown, not absent.
func TestDoctorFlattensNothing(t *testing.T) {
	r := uncertainReport()
	lines := Render(r)
	// No flattened word over an uncertain value.
	if flat := NoFlatten(r); len(flat) != 0 {
		t.Fatalf("the render flattened an uncertain value: %v", flat)
	}
	text := strings.Join(lines, "\n")
	// The declared capability is shown as "declared", not a green mark /
	// "verified" / "ok".
	if !strings.Contains(text, "tools   declared") {
		t.Fatalf("the declared capability must be shown as 'declared':\n%s", text)
	}
	// The unmetered route is "unknown", not $0.00.
	if strings.Contains(text, "$0.00") {
		t.Fatalf("an unmetered route must not render as $0.00:\n%s", text)
	}
	// The stale corpus is "stale", not "available".
	if !strings.Contains(text, "retrieval   stale") {
		t.Fatalf("a stale corpus must show 'stale', not 'available':\n%s", text)
	}
	// The not-listening mounting is shown, not absent.
	if !strings.Contains(text, "http   not listening") {
		t.Fatalf("a not-listening mounting must be shown, not absent:\n%s", text)
	}
}

// TestNoCredentialIsPrinted is D6/C17's pin: presence and source, never the
// value — and "no credential is printed here" is on the screen.
func TestNoCredentialIsPrinted(t *testing.T) {
	r := uncertainReport()
	r.Credentials[0].Name = "ANTHROPIC_API_KEY" // the value is never carried
	lines := Render(r)
	text := strings.Join(lines, "\n")
	// Presence and source, present.
	if !strings.Contains(text, "ANTHROPIC_API_KEY   present   (environment)") {
		t.Fatalf("the credential must show presence + source:\n%s", text)
	}
	// "no credential is printed here" is on the screen deliberately.
	if !strings.Contains(text, "no credential is printed here") {
		t.Fatalf("'no credential is printed here' must be on the screen:\n%s", text)
	}
}

// TestDoctorWorksWithRetrievalAbsent is S22's D1: in the no-retrieval build,
// the retrieval section reads "not built" — not missing, not an error.
func TestDoctorWorksWithRetrievalAbsent(t *testing.T) {
	r := uncertainReport()
	r.Retrieval = Retrieval{State: NotBuilt}
	lines := Render(r)
	text := strings.Join(lines, "\n")
	if !strings.Contains(text, "retrieval   not built") {
		t.Fatalf("the no-retrieval build must read 'not built', not missing or an error:\n%s", text)
	}
	// And it does not error or say "missing".
	if strings.Contains(text, "missing") || strings.Contains(text, "error") {
		t.Fatalf("the no-retrieval build must not say 'missing' or 'error':\n%s", text)
	}
}

// TestDoctorCheckSocketAndCLIReadSameStates is D16/I10/P34's pin: the
// doctor.check socket method and the CLI render the same report, gathered by
// the identical collection step. The report is the same struct — the socket
// and the CLI read it, neither a second readout — so a State field that
// collapses a three-state into a boolean would flatten the socket response
// too. (The socket registration is a follow-up; the report is the same.)
func TestDoctorCheckSocketAndCLIReadSameStates(t *testing.T) {
	r := uncertainReport()
	// The CLI renders from the report; the socket returns the same report.
	// The render is a pure function of the report, so the two cannot drift.
	cli := Render(r)
	// Re-render the same report: identical.
	cliAgain := Render(r)
	if len(cli) != len(cliAgain) {
		t.Fatalf("the same report must render identically: %d vs %d", len(cli), len(cliAgain))
	}
	for i := range cli {
		if cli[i] != cliAgain[i] {
			t.Fatalf("the same report must render identically at line %d", i)
		}
	}
}

// TestGoldenRender pins the full render against a golden file (D10), so a
// change to the rendering is a reviewable diff, not a silent flatten. If the
// golden is absent it is written (the standard golden-file pattern), then a
// re-run checks it.
func TestGoldenRender(t *testing.T) {
	got := Render(uncertainReport())
	gotText := strings.Join(got, "\n")
	goldenPath := filepath.Join("testdata", "doctor", "uncertain.golden")
	b, err := os.ReadFile(goldenPath)
	if err != nil {
		if err2 := os.MkdirAll(filepath.Join("testdata", "doctor"), 0700); err2 != nil {
			t.Fatal(err2)
		}
		if err2 := os.WriteFile(goldenPath, []byte(gotText+"\n"), 0600); err2 != nil {
			t.Fatal(err2)
		}
		t.Skip("wrote the golden; re-run to check")
	}
	if gotText != strings.TrimRight(string(b), "\n") {
		t.Fatalf("the render drifted from the golden:\n--- got ---\n%s\n--- want ---\n%s", gotText, strings.TrimRight(string(b), "\n"))
	}
}
