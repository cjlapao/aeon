package execenv

import (
	"strings"
	"testing"
)

// TestDescribeBindNamesScopeEnvironmentAndStrength is the positive first
// (S28's rule, inherited by EV16): a plain component bind says the scope, the
// component, the environment by slug AND id, the backend with its strength
// number, the direction of the change, the references by name, and EV6.
func TestDescribeBindNamesScopeEnvironmentAndStrength(t *testing.T) {
	got := DescribeBind(BindStatement{
		ScopeKind: "component", ComponentID: "cmp_7",
		EnvironmentSlug: "host-scrubbed", EnvironmentID: "env_1",
		Backend: "host-scrubbed", PreviousBackend: "host-inherit",
		SecretRefNames: []string{"GH_TOKEN", "ANTHROPIC_API_KEY"},
	})
	for _, want := range []string{
		"bind", "scope:component", "component cmp_7",
		"environment host-scrubbed (env_1)",
		"backend host-scrubbed (strength 1)",
		"tighter: host-inherit (0) → host-scrubbed (1)",
		"injects 2 secret reference(s) by name: GH_TOKEN, ANTHROPIC_API_KEY",
		"unattended loops never inherit",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the statement omits %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "dry-run") {
		t.Errorf("an applied bind must not claim to be a dry run:\n%s", got)
	}
}

// TestDescribeBindDryRunDiffersOnlyByItsOwnClause is the formatter half of
// EV18's "the same text": the preview and the applied write are byte-equal
// except for the clause that says which one it is. The daemon's
// TestBindDryRunStatementEqualsAppliedStatement holds the same property
// against real rows.
func TestDescribeBindDryRunDiffersOnlyByItsOwnClause(t *testing.T) {
	in := BindStatement{
		ScopeKind: "workspace", EnvironmentSlug: "host-scrubbed", EnvironmentID: "env_1",
		Backend: "host-scrubbed", PreviousBackend: "host-inherit",
	}
	applied := DescribeBind(in)
	dry := DescribeBind(BindStatement{ScopeKind: in.ScopeKind, EnvironmentSlug: in.EnvironmentSlug,
		EnvironmentID: in.EnvironmentID, Backend: in.Backend, PreviousBackend: in.PreviousBackend,
		SecretRefNames: in.SecretRefNames, DryRun: true})
	if !strings.HasPrefix(dry, applied+" · ") {
		t.Fatalf("the preview is not the applied statement with one clause appended:\napplied %q\ndryRun  %q", applied, dry)
	}
	if !strings.HasSuffix(dry, "dry-run: nothing applied") {
		t.Fatalf("the preview must say nothing was applied: %q", dry)
	}
	if DescribeBind(in) != applied {
		t.Fatal("DescribeBind is not pure — the same inputs must give the same sentence")
	}
}

// TestDescribeBindCallsAWideningAWidening is the audit-facing word: a person
// greps the audit for loosening binds, so the sentence uses the word and never
// a euphemism, and the strength numbers appear on both sides.
func TestDescribeBindCallsAWideningAWidening(t *testing.T) {
	got := DescribeBind(BindStatement{ScopeKind: "component", ComponentID: "c1",
		EnvironmentSlug: "legacy", EnvironmentID: "env_9", Backend: "host-inherit", PreviousBackend: "container"})
	if !strings.Contains(got, "widening: container (") {
		t.Fatalf("a loosening bind must say widening: %q", got)
	}
	if strings.Contains(got, "tighter") {
		t.Fatalf("a loosening bind must not read as tighter: %q", got)
	}
	if !strings.Contains(got, "backend host-inherit (strength 0)") {
		t.Fatalf("the backend and its strength must be named: %q", got)
	}
}

// TestDescribeBindSaysNothingWasBoundRatherThanZeroIs the third state (P3):
// "" is not host-inherit, and a statement that conflated them would tell a
// person the workspace had an environment when it had none.
func TestDescribeBindSaysNothingWasBoundRatherThanZeroIs(t *testing.T) {
	got := DescribeBind(BindStatement{ScopeKind: "workspace", EnvironmentSlug: "host-scrubbed",
		EnvironmentID: "env_1", Backend: "host-scrubbed"})
	if !strings.Contains(got, "nothing was bound at this scope") {
		t.Fatalf("the absence must be stated as itself: %q", got)
	}
	if strings.Contains(got, "host-inherit") {
		t.Fatalf("an unbound scope is not a host-inherit binding: %q", got)
	}

	same := DescribeBind(BindStatement{ScopeKind: "workspace", EnvironmentSlug: "a", EnvironmentID: "e",
		Backend: "host-scrubbed", PreviousBackend: "host-scrubbed"})
	if !strings.Contains(same, "same strength: host-scrubbed (1)") {
		t.Fatalf("equal strengths must say so: %q", same)
	}
}

// TestDescribeBindCountsEveryNameItCollapses keeps the legibility cap honest:
// eight names are listed, the ninth is counted, and the count is the true
// total — a truncated list that reported the truncated length would be a
// number nobody could trust.
func TestDescribeBindCountsEveryNameItCollapses(t *testing.T) {
	names := make([]string, 0, maxStatementNames+3)
	for i := 0; i < maxStatementNames+3; i++ {
		names = append(names, fmtName(i))
	}
	got := DescribeBind(BindStatement{ScopeKind: "workspace", EnvironmentSlug: "s", EnvironmentID: "e",
		Backend: "host-scrubbed", SecretRefNames: names})
	if !strings.Contains(got, "injects 11 secret reference(s) by name") {
		t.Fatalf("the count is the true total, not the shown tail: %q", got)
	}
	if !strings.Contains(got, "(+3 more)") {
		t.Fatalf("the collapsed tail must be counted: %q", got)
	}
	if strings.Contains(got, fmtName(10)) {
		t.Fatalf("only the first %d names are listed: %q", maxStatementNames, got)
	}
}

// TestDescribeBindStatesNoReferencesRatherThanOmittingTheClause pins the
// third state again: no references is a stated fact.
func TestDescribeBindStatesNoReferencesRatherThanOmittingTheClause(t *testing.T) {
	got := DescribeBind(BindStatement{ScopeKind: "workspace", EnvironmentSlug: "s", EnvironmentID: "e",
		Backend: "host-scrubbed"})
	if !strings.Contains(got, "injects no secret references") {
		t.Fatalf("an absence must be stated: %q", got)
	}
}

func fmtName(i int) string {
	return "REF_" + string(rune('A'+i%26)) + string(rune('a'+i%26))
}
