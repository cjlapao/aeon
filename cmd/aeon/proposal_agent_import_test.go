package main

// proposal_agent_import_test.go — S37 T5, the commit dispatch for agent_import.
// The agent_import effect is applied through cmd/aeon's commit door (the ONLY
// door, T3/T4) by calling the ONE extracted import body (importAgentPackage,
// AD4) — never a second implementation. A refusal leaves the row proposed with
// applied_error set (AD5); a crash between apply and flip is NOT idempotent
// (Q14), because the plugin_agent_live unique index refuses the second commit.
//
// The tests never point at the real store: XDG_STATE_HOME at a throwaway, the
// second handle (openStoreDirectly) behind the daemon's absent writer (WAL).

import (
	"os"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/store"
)

// createAgentImportProposal makes a proposed agent_import row whose payload is
// the bare absolute path the create half stored.
func createAgentImportProposal(t *testing.T, st *store.Store, pkgPath string) store.Proposal {
	t.Helper()
	return createTestProposal(t, st, "agent_import", map[string]any{"pkgPath": pkgPath})
}

// TestAgentImportCommitRunsTheExtractedBodyAndFlips — the happy path: the
// commit runs importAgentPackage (the extracted body), the agent lands, and
// the row flips to committed. The agent is DISABLED (AP2), not enabled.
func TestAgentImportCommitRunsTheExtractedBodyAndFlips(t *testing.T) {
	agentHome(t)
	dir := writeAgentPackage(t, "groot", nil)
	st := proposalHome(t)
	p := createAgentImportProposal(t, st, dir)

	out, err := runProposal(t, "commit", itoa(p.ID))
	if err != nil {
		t.Fatalf("commit: %v\n%s", err, out)
	}
	if !strings.Contains(out, "imported groot as DISABLED") {
		t.Fatalf("the commit must run the extracted body, printing its consent: %q", out)
	}
	if !strings.Contains(out, "committed") {
		t.Fatalf("the commit must report the flip: %q", out)
	}
	// The agent landed DISABLED — the same as the CLI verb (AP2).
	listOut, err := runAgent(t, "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(listOut, "groot") || !strings.Contains(listOut, "disabled") || strings.Contains(listOut, "enabled") {
		t.Fatalf("AP2 — the commit imported the agent DISABLED, like the CLI: %q", listOut)
	}
	// The row flipped to committed.
	row, found, err := st.GetProposal(p.ID)
	if err != nil || !found {
		t.Fatalf("read the row: %v", err)
	}
	if row.Status != "committed" {
		t.Fatalf("the row must flip to committed: got %q", row.Status)
	}
}

// TestAgentImportCommitRefusalLeavesTheRowProposed — a stored path that is
// relative (predates the absolute rule) refuses at the commit door; the row
// stays proposed with applied_error set (AD5). Nothing was imported.
func TestAgentImportCommitRefusalLeavesTheRowProposed(t *testing.T) {
	agentHome(t)
	st := proposalHome(t)
	p := createAgentImportProposal(t, st, "relative/path") // not absolute

	out, err := runProposal(t, "commit", itoa(p.ID))
	if err == nil {
		t.Fatalf("a relative stored path must refuse at the commit door; output was %q", out)
	}
	// The row stays proposed with applied_error set.
	row, found, err := st.GetProposal(p.ID)
	if err != nil || !found {
		t.Fatalf("read the row: %v", err)
	}
	if row.Status != "proposed" {
		t.Fatalf("AD5 — a refused commit must leave the row proposed, got %q", row.Status)
	}
	if row.AppliedError == "" {
		t.Fatalf("AD5 — the refusal must be recorded in applied_error, got empty")
	}
	// Nothing was imported.
	listOut, err := runAgent(t, "list")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(listOut, "groot") {
		t.Fatalf("a refused commit must import nothing: %q", listOut)
	}
}

// TestAgentImportCommitCrashRetryNotIdempotent — Q14: a crash between "the
// import succeeded" and "the flip is written" must NOT re-import on retry. The
// agent is already live (the CLI imported it), the row is still proposed, and
// the re-commit refuses with the second-import text (the plugin_agent_live
// unique index), the row stays proposed.
func TestAgentImportCommitCrashRetryNotIdempotent(t *testing.T) {
	agentHome(t)
	dir := writeAgentPackage(t, "groot", nil)
	// The CLI imports the agent first — this is the "import succeeded" half of
	// the crash. The proposal below is created but never committed.
	if _, err := runAgent(t, "import", dir); err != nil {
		t.Fatalf("the CLI import: %v", err)
	}
	// openAgentStore, NOT proposalHome: this test's whole premise is that the
	// CLI import and the commit share ONE store (agentHome set XDG_STATE_HOME
	// once). proposalHome re-runs agentHome and resets XDG_STATE_HOME to a
	// fresh temp dir, so the commit would read a store the CLI never wrote to
	// and the second-import refusal could not fire.
	st := openAgentStore(t)
	p := createAgentImportProposal(t, st, dir)

	// The re-commit refuses: the plugin_agent_live unique index (the second
	// import of a live agent) reports it. The refusal is the returned error,
	// not the (empty) output — the effect door returns it and the commit
	// records it in applied_error (AD5), it does not print a pretty line.
	_, err := runProposal(t, "commit", itoa(p.ID))
	if err == nil {
		t.Fatalf("a second import of the same agent must refuse; it did not")
	}
	// The row stays proposed (AD5) with the refusal recorded (Q14's durable
	// half) — that is the fact the retry depends on, not the transient text.
	row, found, err := st.GetProposal(p.ID)
	if err != nil || !found {
		t.Fatalf("read the row: %v", err)
	}
	if row.Status != "proposed" {
		t.Fatalf("Q14 — the refused re-commit must leave the row proposed, got %q", row.Status)
	}
	if !strings.Contains(row.AppliedError, "already has a live record") {
		t.Fatalf("Q14 — the second-import text must be recorded in applied_error, got %q", row.AppliedError)
	}
}

// TestAgentImportCommitPrintsFreshConsent — the commit prints the FRESH
// consent (from importAgentPackage's RenderConsent), which is the body the CLI
// verb prints — so it carries the "read the consent above; when decided" line
// the create-time preview never has. The create-time preview (T4) is advisory;
// the commit's consent is the authoritative text.
func TestAgentImportCommitPrintsFreshConsent(t *testing.T) {
	agentHome(t)
	dir := writeAgentPackage(t, "groot", nil)
	st := proposalHome(t)
	p := createAgentImportProposal(t, st, dir)

	out, err := runProposal(t, "commit", itoa(p.ID))
	if err != nil {
		t.Fatalf("commit: %v\n%s", err, out)
	}
	// The fresh consent (the body the CLI verb prints) carries these lines —
	// the create-time preview (T4) never does.
	if !strings.Contains(out, "read the consent above; when decided: aeon agent enable groot") {
		t.Fatalf("the commit must print the fresh consent, not the advisory preview: %q", out)
	}
	if !strings.Contains(out, "imported groot as DISABLED") {
		t.Fatalf("the commit's consent must name the DISABLED import: %q", out)
	}
}

// TestAgentImportCommitPrintsTheRevokedNotice — AP4: a REVOKED life that comes
// back must be VISIBLE in the consent. The CLI revokes groot; the commit
// re-imports it and the NOTICE line must appear (the body's own words).
func TestAgentImportCommitPrintsTheRevokedNotice(t *testing.T) {
	agentHome(t)
	dir := writeAgentPackage(t, "groot", nil)
	// Import, then revoke — groot's live row is gone but its revoked transition
	// row remains (the history the NOTICE is built from).
	if _, err := runAgent(t, "import", dir); err != nil {
		t.Fatalf("import: %v", err)
	}
	if _, err := runAgent(t, "revoke", "groot", "--reason", "testing"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	// openAgentStore, NOT proposalHome: the NOTICE is built from the revoked
	// transition rows the CLI wrote, so the commit must read the SAME store the
	// CLI import and revoke wrote to. proposalHome re-runs agentHome and resets
	// XDG_STATE_HOME to a fresh temp dir — a store with no history, so no
	// notice could appear.
	st := openAgentStore(t)
	p := createAgentImportProposal(t, st, dir)

	out, err := runProposal(t, "commit", itoa(p.ID))
	if err != nil {
		t.Fatalf("commit (re-import): %v\n%s", err, out)
	}
	if !strings.Contains(out, "was REVOKED") {
		t.Fatalf("AP4 — a re-import after a revocation must print the NOTICE line: %q", out)
	}
}

// TestAgentImportCarriedDenialStoreRefusalPrintsAndStillFlips — a carried
// denial that cannot be stored (the policy boundary refuses it) is PRINTED, not
// swallowed (the CLI's own words, AD5's "same as the CLI"), and the import
// still completes and the row still flips — a stored denial is a hole, an
// unstored one must be seen, never a silent success.
func TestAgentImportCarriedDenialStoreRefusalPrintsAndStillFlips(t *testing.T) {
	agentHome(t)
	// A carried denial whose spec is malformed enough that the policy boundary
	// refuses to store it (an authoring boundary that would not store it).
	badCarried := map[string][]byte{
		"policies/bad.json": []byte(`{"id":"bad-` + "groot" + `","type":"tool_access","target":{"kind":"agent","id":"groot"},"action":"deny","spec":{"toolName":"bash","state":"nope"},"source":"operator"}`),
	}
	dir := writeAgentPackage(t, "groot", badCarried)
	st := proposalHome(t)
	p := createAgentImportProposal(t, st, dir)

	out, err := runProposal(t, "commit", itoa(p.ID))
	if err != nil {
		t.Fatalf("commit (a carried-denial store refusal must not fail the import): %v\n%s", err, out)
	}
	// The refusal to store the denial is printed, not swallowed.
	if !strings.Contains(out, "denial") {
		t.Fatalf("a carried-denial store refusal must be printed, not swallowed: %q", out)
	}
	// And the import still completed and the row still flipped.
	if !strings.Contains(out, "imported groot as DISABLED") || !strings.Contains(out, "committed") {
		t.Fatalf("the import still completes and the row still flips: %q", out)
	}
	row, found, err := st.GetProposal(p.ID)
	if err != nil || !found {
		t.Fatalf("read the row: %v", err)
	}
	if row.Status != "committed" {
		t.Fatalf("the row must flip despite the unstored denial: %q", row.Status)
	}
}

// TestAgentImportCommitNoSecondImportBody — the whole slice's safety (AA5's
// machine-enforced half): exactly ONE import body exists. The only call to
// plugin.ImportChecked in this package's PRODUCTION source is inside
// importAgentPackage (agent.go); the agent_import case in proposal.go calls
// that body, it does not call ImportChecked or the ledger directly. A second
// body is exactly what S37's AD4/AA5 forbids.
//
// The proof greps the package's own source (the test's working directory is
// the package dir under `go test`), counting the import-call token in
// production files only — a test calling ImportChecked is a caller, not a
// second body.
func TestAgentImportCommitNoSecondImportBody(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read the package dir: %v", err)
	}
	checked := 0
	proposalCallsImportChecked := false
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		// Count only production source: a test calling ImportChecked is a
		// caller, not a second body (the body is the one definition).
		if strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		data, err := os.ReadFile(e.Name())
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		src := string(data)
		// The token, not a bare substring: a receiver name or a comment would
		// be a false match.
		checked += strings.Count(src, "ImportChecked(")
		if e.Name() == "proposal.go" {
			proposalCallsImportChecked = strings.Contains(src, "ImportChecked(")
		}
	}
	if checked != 1 {
		t.Fatalf("AA5 — exactly one ImportChecked call must exist in production source, got %d", checked)
	}
	if proposalCallsImportChecked {
		t.Fatalf("AA5 — proposal.go must call the one body (importAgentPackage), not ImportChecked directly")
	}
}
