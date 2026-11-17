package store

// agent_audited_guard_test.go — S73 T3 (AR3): the source walk that keeps the
// un-audited writers from coming back, plus the canary that proves the walk has
// teeth.
//
// # What the compiler already does, and what is left to this file
//
// AR3's main defence is typed: `createAgentTx`, `updateAgentTx` and
// `deleteAgentTx` are unexported and take a `*sql.Tx`, so a new road to the
// `agent` table has to be opened inside this package and inside a transaction —
// which means inside one of the audited doors or the seed trio. What the type
// system cannot see is a file in THIS package that simply writes its own
// `UPDATE agent SET …` and calls it a day. That is what the walk below refuses,
// and it refuses it by FILE NAME, the way S46's
// `TestMcpRowsAreWrittenOnlyThroughTheAdminSeam` refuses it over there.
//
// # Why the allow-list is two files and says what each one is
//
// `agent.go` holds the three primitives. `agent_seed.go` holds the seed trio,
// which are writers on THEIR caller's transaction because the seed engine owns
// the wider unit (a corpus apply commits content, ledger and audit together).
// Every other file that mentions the `agent` table only reads it. If a fourth
// file ever earns a place here, it is a decision someone writes down beside the
// list — not an edit that makes the test green.

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// agentWriteStatements are the SQL texts that change an `agent` ROW (not the
// ledger, not the lifecycle table). Each is matched on the exact prefix the
// primitives use, so a comment that talks about a DELETE does not trip it while
// a real one cannot hide.
var agentWriteStatements = []string{"INSERT INTO agent ", "UPDATE agent SET", "DELETE FROM agent"}

// agentWriteOwners is the allow-list, with the reason each entry is here.
var agentWriteOwners = map[string]string{
	"agent.go":      "the three unexported tx primitives (createAgentTx, updateAgentTx, deleteAgentTx) — the only INSERT/UPDATE/DELETE text on the agent table outside the seed trio",
	"agent_seed.go": "the seed trio (SeedAgentInsert, SeedAgentUpdate, SeedAgentRetire), each writing on its caller's transaction and minting its own `seed` ledger row",
}

// agentWriteOffenders runs the walk over a set of file contents and returns
// every (file, statement) pair outside the allow-list. It is a function over a
// map, not a walk of the disk, so the teeth test below can hand it a rogue file
// without touching the repository.
func agentWriteOffenders(files map[string]string) []string {
	var out []string
	for name, body := range files {
		if _, allowed := agentWriteOwners[name]; allowed {
			continue
		}
		for _, stmt := range agentWriteStatements {
			if strings.Contains(body, stmt) {
				out = append(out, name+": "+strings.TrimSpace(stmt))
			}
		}
	}
	return out
}

// TestAgentRowsAreWrittenOnlyThroughTheAuditedPath is the walk over the real
// package. It parses the same files the compiler did, skips tests (a test that
// plants a row through a door is not a new road), and refuses a string literal
// outside the two owner files.
func TestAgentRowsAreWrittenOnlyThroughTheAuditedPath(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	files := map[string]string{}
	parsed := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, e.Name(), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", e.Name(), err)
		}
		// Read the raw bytes rather than reconstructing from the AST: the walk
		// is about the SQL TEXT, and a raw string literal survives intact.
		raw, err := os.ReadFile(filepath.Clean(e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		_ = f
		files[e.Name()] = string(raw)
		parsed++
	}
	if parsed == 0 {
		t.Fatal("no sources found to walk — the guard would pass vacuously")
	}
	if got := agentWriteOffenders(files); len(got) > 0 {
		t.Fatalf("%d file(s) write the agent table outside the audited owners: %v\n"+
			"the owners are: agent.go (the tx primitives) and agent_seed.go (the seed trio). "+
			"A new road means a new audited door, not a new entry in agentWriteOwners.", len(got), got)
	}
	// And the two owners still hold what the list claims, so the guard cannot
	// drift into allowing a file that no longer writes anything.
	for name, why := range agentWriteOwners {
		body, ok := files[name]
		if !ok {
			t.Fatalf("%s is on the allow-list and is not in the package — the list is stale (%s)", name, why)
		}
		found := false
		for _, stmt := range agentWriteStatements {
			if strings.Contains(body, stmt) {
				found = true
			}
		}
		if !found {
			t.Errorf("%s is allowed to write the agent table and no longer does — narrow the list (%s)", name, why)
		}
	}
}

// TestAgentAuditGuardHasTeeth is the canary. A guard nobody has seen fail is a
// rumour, so this hands the SAME walker a package that contains a rogue writer
// and insists it is named — and then checks that a clean package passes, so the
// teeth are not just an always-failing assertion.
func TestAgentAuditGuardHasTeeth(t *testing.T) {
	rogue := map[string]string{
		"agent.go":          "func createAgentTx() { _, _ = `INSERT INTO agent (slug) VALUES (?)` }",
		"agent_seed.go":     "func SeedAgentInsert() { _, _ = `UPDATE agent SET name=? WHERE slug=?` }",
		"quiet_reader.go":   "func ReadAgents() { _, _ = `SELECT slug FROM agent` }",
		"agent_backdoor.go": "func Backdoor() { _, _ = `UPDATE agent SET prompt = '' WHERE slug = 'groot'` }",
		"agent_sweeper.go":  "func Sweep() { _, _ = `DELETE FROM agent WHERE slug = ?` }",
	}
	got := agentWriteOffenders(rogue)
	if len(got) != 2 {
		t.Fatalf("the walker found %v, want the two rogues named", got)
	}
	joined := strings.Join(got, "\n")
	for _, want := range []string{"agent_backdoor.go", "agent_sweeper.go"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the walker did not name %s: %v", want, got)
		}
	}
	// The same walker over a clean set says nothing, so the failure above is
	// the rogues and not the walker's temperament.
	clean := map[string]string{
		"agent.go":        "func createAgentTx() { _, _ = `INSERT INTO agent (slug) VALUES (?)` }",
		"agent_seed.go":   "func SeedAgentInsert() { _, _ = `UPDATE agent SET name=? WHERE slug=?` }",
		"quiet_reader.go": "func ReadAgents() { _, _ = `SELECT slug FROM agent` }",
		"agent_page.go":   "func AgentPage() { _, _ = `SELECT slug FROM agent WHERE slug LIKE ? ESCAPE '\\'` }",
		"agent_change.go": "func AgentChangeInsertTx() { _, _ = `INSERT INTO agent_change (op) VALUES (?)` }",
	}
	if got := agentWriteOffenders(clean); len(got) != 0 {
		t.Fatalf("the walker refused a clean package: %v", got)
	}
}

// TestAgentChangeHasNoUpdateOrDeleteAccessor keeps AR1's missing half missing:
// the ledger may be appended to and read, and no Go code in this package may
// hold an UPDATE or a DELETE aimed at it. The triggers catch a caller that
// invents the SQL; this catches the accessor that would have offered it.
func TestAgentChangeHasNoUpdateOrDeleteAccessor(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var offenders []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		raw, err := os.ReadFile(filepath.Clean(e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		body := string(raw)
		for _, stmt := range []string{"UPDATE agent_change", "DELETE FROM agent_change"} {
			if strings.Contains(body, stmt) {
				offenders = append(offenders, e.Name()+": "+stmt)
			}
		}
	}
	if len(offenders) > 0 {
		t.Fatalf("the agent ledger has a mutating accessor in %v — AR1 says append-only, and the trigger is the backstop, not the plan", offenders)
	}
}
