package store

// The tests for the `policy_change` authoring audit (S45 T3, PM8/PM11).
//
// The shape mirrors the S44 setting_event precedent on purpose: the audit
// is proven append-only at the STATEMENT level (the trigger is the
// enforcement, the test is the proof), the CHECK vocabulary is proven as
// the net under the callers, and the audit's two structural claims — it
// outlives the policy, and retention cannot reach it — each get their own
// test because each is a reason the table is SEPARATE from policy_event
// (T0 item 3.3's verdict). A raw write here is the sanctioned way to plant
// a row the accessors refuse to create (I2; the settings_test.go rule).

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// policyChangeAt is a fixed instant for the timestamps a test supplies —
// the store takes no clock, so the instant is the caller's (D5).
const policyChangeAt = "2026-10-15T09:41:12Z"

// policyChangeMigrationName finds the policy_change migration's ledger name
// without freezing the timestamp (the file name is the ID, set at creation
// time, so the test looks it up rather than hard-coding it — the
// settingsMigrationName rule).
func policyChangeMigrationName(t *testing.T) string {
	t.Helper()
	for _, m := range Migrations() {
		if strings.HasSuffix(m.Name, "_policy_change") {
			return m.Name
		}
	}
	t.Fatal("no _policy_change migration in Migrations()")
	return ""
}

// migrationsBefore returns the migration list strictly before the file
// whose Name ends with the given suffix — the vintage a pre-migration
// upgrade test must seed. Pinned BY IDENTITY, never `[:len-1]`: S22 T1
// measured (automation_schema_test.go records it) and S55's appended
// migrations re-proved that a trailing-count slice silently moves the
// vintage when another slice appends a file, turning an upgrade clause
// into a no-op that asserts nothing.
func migrationsBefore(t *testing.T, suffix string) []Migration {
	t.Helper()
	for i, m := range Migrations() {
		if strings.HasSuffix(m.Name, suffix) {
			return Migrations()[:i]
		}
	}
	t.Fatalf("no migration named %q in Migrations() — a vintage pinned to a missing file is a vintage that never existed", suffix)
	return nil
}

// change builds a valid authoring row for the accessor tests; callers set
// the fields the case is about. It is valid BY CONSTRUCTION (op add, a
// named policy) so a test that varies one field varies one field.
func change(op, policyID string) PolicyChange {
	return PolicyChange{
		Op: op, PolicyID: policyID,
		Summary:   fmt.Sprintf("%s policy %q", op, policyID),
		ActorKind: "user", ActorID: "1000",
		OccurredAt: policyChangeAt,
	}
}

// rawInsertPolicyChange writes a row straight through the writer, bypassing
// the accessors — the only way to ask the CHECK net (not the Go guards)
// whether it holds. policyID/proposalID pass as the any they are (nil is
// the column's NULL).
func rawInsertPolicyChange(t *testing.T, s *Store, op string, policyID, proposalID any, summary, actorKind, actorID string) error {
	t.Helper()
	_, err := s.w.Exec(`
		INSERT INTO policy_change (op, policy_id, proposal_id, summary, actor_kind, actor_id, occurred_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		op, policyID, proposalID, summary, actorKind, actorID, policyChangeAt)
	return err
}

// TestPolicyChangeMigrationCreatesTableAndTriggers is the migration's own
// proof: the table with its columns, BOTH indexes the migration wrote, and
// BOTH append triggers are in the catalogue after a fresh apply — and the
// migration lands on an UPGRADE too (a database seeded at
// Migrations()[:len-1], computed dynamically so a later slice's file moves
// the vintage and this test with it, the S22 T1 lesson settings_test.go
// records). It fails when the DDL drifted from the file the migration
// carries, or when the file is not picked up at all.
func TestPolicyChangeMigrationCreatesTableAndTriggers(t *testing.T) {
	want := policyChangeMigrationName(t)

	// Fresh apply: table, columns, indexes, triggers.
	s := openTemp(t)
	assertColumns(t, s, "policy_change", "id", "op", "policy_id", "proposal_id",
		"summary", "actor_kind", "actor_id", "occurred_at")
	assertCatalogue(t, s, "index", "idx_policy_change_policy")
	assertCatalogue(t, s, "index", "idx_policy_change_time")
	assertCatalogue(t, s, "trigger", "policy_change_no_update")
	assertCatalogue(t, s, "trigger", "policy_change_no_delete")
	if err := assertLedgerHas(s.AppliedMigrations, want); err != nil {
		t.Fatalf("fresh apply: %v", err)
	}

	// Upgrade of a database seeded just BEFORE this migration: the runner
	// applies the pending file and records it — the file is not silently
	// skipped on an existing install. The vintage is pinned BY IDENTITY
	// (everything before this file's own migration), not by `len-1`: S22 T1
	// measured that `[:len(Migrations())-1]` silently moved the vintage when
	// another slice appended a file after this one, which would make this
	// upgrade clause check a database that already HAS the table.
	dir := t.TempDir()
	path := filepath.Join(dir, "state.db")
	g, closeDB := rawGorm(t, path)
	applyMigrations(t, g, migrationsBefore(t, "_policy_change"))
	closeDB()

	s = openAt(t, dir)
	if err := assertLedgerHas(s.AppliedMigrations, want); err != nil {
		t.Fatalf("upgraded install: %v", err)
	}
	assertCatalogue(t, s, "trigger", "policy_change_no_update")
	assertCatalogue(t, s, "trigger", "policy_change_no_delete")
}

// TestPolicyChangeIsAppendOnly is PM8's proof, the TestSettingEventIsAppendOnly
// shape: an UPDATE and a DELETE of an audit row each FAIL with the trigger's
// own sentence, and the row survives. If this fails, the trigger did not
// apply or is not enforced — and then the documented fallback (no Update or
// Delete accessor plus TestPolicyChangeHasNoUpdateOrDeletePath) is NOT
// enough and the migration is wrong.
func TestPolicyChangeIsAppendOnly(t *testing.T) {
	s := openTemp(t)
	if err := s.PolicyChangeInsert(ctx(), change("add", "grant-bash")); err != nil {
		t.Fatalf("insert: %v", err)
	}
	id := onlyChangeID(t, s)

	if _, err := s.w.Exec(`UPDATE policy_change SET summary = 'rewritten' WHERE id = ?`, id); err == nil {
		t.Fatal("UPDATE policy_change succeeded; the append-only trigger did not fire")
	} else if !strings.Contains(err.Error(), "policy_change is append-only: an authoring audit row is never rewritten (S45 PM8)") {
		t.Fatalf("UPDATE refused by something other than the trigger's sentence: %v", err)
	}
	if _, err := s.w.Exec(`DELETE FROM policy_change WHERE id = ?`, id); err == nil {
		t.Fatal("DELETE FROM policy_change succeeded; the append-only trigger did not fire")
	} else if !strings.Contains(err.Error(), "policy_change is append-only: an authoring audit row is never deleted (S45 PM8)") {
		t.Fatalf("DELETE refused by something other than the trigger's sentence: %v", err)
	}
	if got := onlyChangeID(t, s); got != id {
		t.Fatalf("the audit row was lost (got id %d, want %d)", got, id)
	}
}

func onlyChangeID(t *testing.T, s *Store) int64 {
	t.Helper()
	var id int64
	if err := s.w.QueryRow(`SELECT id FROM policy_change LIMIT 1`).Scan(&id); err != nil {
		t.Fatalf("read back the audit row: %v", err)
	}
	return id
}

// TestPolicyChangeCheckConstraints is the vocabulary net's proof: the
// CHECKs, not the Go guards, refuse each bad row (the rows arrive through
// the raw writer, the one path that skipped the accessors). Every case is
// one T3.md row: a bad op; add without policy_id; the empty summary; the
// 1025-char summary; a bad actor_kind — plus the two positive controls that
// keep the net from being wider than the DDL (seed carries NULL policy_id;
// commit may).
func TestPolicyChangeCheckConstraints(t *testing.T) {
	s := openTemp(t)

	good := []struct {
		why                         string
		op                          string
		policyID, proposalID        any
		summary, actorKind, actorID string
	}{
		{"seed carries no policy_id", "seed", nil, nil, "seeded 367 corpus rules", "cli", "cli"},
		{"commit may carry no policy_id", "commit", nil, int64(7), "committed proposal 7", "user", "1000"},
	}
	for _, g := range good {
		if err := rawInsertPolicyChange(t, s, g.op, g.policyID, g.proposalID, g.summary, g.actorKind, g.actorID); err != nil {
			t.Fatalf("%s: the CHECK net refused a row the DDL admits: %v", g.why, err)
		}
	}

	bad := []struct {
		why                         string
		op                          string
		policyID, proposalID        any
		summary, actorKind, actorID string
	}{
		{"op outside the five", "edit", "grant-bash", nil, "edited policy", "user", "1000"},
		{"add without policy_id", "add", nil, nil, "added a policy", "user", "1000"},
		{"remove without policy_id", "remove", nil, nil, "removed a policy", "user", "1000"},
		{"empty summary", "add", "grant-bash", nil, "", "user", "1000"},
		{"1025-char summary", "add", "grant-bash", nil, strings.Repeat("s", 1025), "user", "1000"},
		{"actor_kind outside the two", "add", "grant-bash", nil, "added policy", "daemon", "1000"},
		{"empty actor_id", "add", "grant-bash", nil, "added policy", "user", ""},
	}
	for _, b := range bad {
		if err := rawInsertPolicyChange(t, s, b.op, b.policyID, b.proposalID, b.summary, b.actorKind, b.actorID); err == nil {
			t.Fatalf("%s: the CHECK net admitted it; the net should refuse (the DDL is the contract)", b.why)
		}
	}
}

// TestPolicyChangeRefusesShapelessRowsNamingTheField is the accessor half:
// the fail-fast guards name the blank they refuse (the PolicyAdd
// discipline), before the statement and independently of the CHECK net.
func TestPolicyChangeRefusesShapelessRowsNamingTheField(t *testing.T) {
	s := openTemp(t)
	cases := []struct {
		why  string
		mut  func(*PolicyChange)
		want string
	}{
		{"no op", func(c *PolicyChange) { c.Op = "" }, "needs its op"},
		{"no summary", func(c *PolicyChange) { c.Summary = "" }, "no summary"},
		{"no actor kind", func(c *PolicyChange) { c.ActorKind = "" }, "no actor kind"},
		{"no actor id", func(c *PolicyChange) { c.ActorID = "" }, "no actor id"},
		{"no instant", func(c *PolicyChange) { c.OccurredAt = "" }, "no instant"},
	}
	for _, tc := range cases {
		c := change("add", "grant-bash")
		tc.mut(&c)
		err := s.PolicyChangeInsert(ctx(), c)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want an error naming %q", tc.why, err, tc.want)
		}
	}
	// The tx variant refuses a nil transaction too — it exists to JOIN a
	// caller's transaction; nil is the missing commit, not an absent one.
	if err := s.PolicyChangeInsertTx(ctx(), nil, change("add", "grant-bash")); err == nil {
		t.Error("PolicyChangeInsertTx accepted a nil transaction")
	}
}

// TestPolicyChangeInsertAndListNewestFirst is the paging contract: order
// (newest first, id DESC), the per-policy filter, the limit clamp/default —
// and one row per write (a second append of the identical row is a second
// row: the audit records writes, not states, and a duplicate-suppressing
// insert would silently un-record a repeat authoring).
func TestPolicyChangeInsertAndListNewestFirst(t *testing.T) {
	s := openTemp(t)
	inserts := []PolicyChange{
		change("add", "grant-bash"),
		change("remove", "grant-bash"),
		func() PolicyChange { c := change("seed", ""); c.Summary = "seeded the corpus"; return c }(),
		func() PolicyChange {
			c := change("commit", "")
			c.ProposalID = int64ptr(7)
			c.Summary = "committed proposal 7"
			return c
		}(),
	}
	for _, c := range inserts {
		if err := s.PolicyChangeInsert(ctx(), c); err != nil {
			t.Fatalf("insert %s: %v", c.Op, err)
		}
	}
	// One row per write: the identical row appended again is a SECOND row
	// (the SEED row is the one repeated — repeating an id-filtered row
	// would blur the duplicate case into the filter case below).
	if err := s.PolicyChangeInsert(ctx(), inserts[2]); err != nil {
		t.Fatalf("repeat insert: %v", err)
	}

	all, err := s.PolicyChangeList(ctx(), "", 0) // limit 0 → the default 50
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 5 {
		t.Fatalf("got %d rows, want 5 (one per write, the repeat included)", len(all))
	}
	for i := 1; i < len(all); i++ {
		if all[i-1].ID <= all[i].ID {
			t.Fatalf("not newest-first at %d: id %d before %d", i, all[i-1].ID, all[i].ID)
		}
	}

	// The per-policy filter reads that rule's trail only.
	one, err := s.PolicyChangeList(ctx(), "grant-bash", 10)
	if err != nil {
		t.Fatalf("filtered list: %v", err)
	}
	if len(one) != 2 || one[0].Op != "remove" || one[1].Op != "add" {
		ops := []string{}
		for _, c := range one {
			ops = append(ops, c.Op)
		}
		t.Fatalf("filtered list = %v, want [remove add]", ops)
	}
	if one[0].PolicyID != "grant-bash" || one[0].ActorKind != "user" || one[0].ActorID != "1000" ||
		one[0].OccurredAt != policyChangeAt {
		t.Errorf("filtered row did not round-trip its columns: %+v", one[0])
	}
	if one[0].ProposalID != nil {
		t.Errorf("an add carried a proposal id %v; the NULL must read back as nil", *one[0].ProposalID)
	}

	// The commit row's proposal id round-trips (the *int64 is not 0).
	for _, c := range all {
		if c.Op == "commit" {
			if c.ProposalID == nil || *c.ProposalID != 7 {
				t.Errorf("commit proposalID = %v, want 7", c.ProposalID)
			}
		}
	}

	// The clamp, measured (the TestSettingEventListOrderAndLimit shape):
	// plant enough rows that the ceiling is visible, then pin default 50
	// and over-ceiling 500 — an unset limit that read "everything" would
	// be the audit answer with no bound (the same rule PolicyEventQuery
	// states by refusing; this accessor clamps, the SettingEventList
	// contract the task names).
	const total = 600
	for i := 0; i < total; i++ {
		if err := rawInsertPolicyChange(t, s, "seed", nil, nil, fmt.Sprintf("seeded batch %d", i), "cli", "cli"); err != nil {
			t.Fatalf("plant row %d: %v", i, err)
		}
	}
	defaulted, err := s.PolicyChangeList(ctx(), "", 0)
	if err != nil {
		t.Fatalf("default-limit list: %v", err)
	}
	if len(defaulted) != 50 {
		t.Fatalf("limit=0 returned %d; want 50 (the default)", len(defaulted))
	}
	if defaulted[0].ID != int64(total+5) {
		t.Fatalf("newest-first over the planted rows: leading id %d, want %d", defaulted[0].ID, total+5)
	}
	capped, err := s.PolicyChangeList(ctx(), "", 9999)
	if err != nil {
		t.Fatalf("capped list: %v", err)
	}
	if len(capped) != 500 {
		t.Fatalf("limit=9999 returned %d; want 500 (the ceiling)", len(capped))
	}
}

func int64ptr(v int64) *int64 { return &v }

// TestPolicyChangeOutlivesThePolicy is the "no FK / no cascade" proof: the
// audit of a rule SURVIVES the rule. A foreign key (or a cascade) creeping
// into a future migration would delete the history of the very removal an
// auditor is asking about — the record that disappears when you ask.
func TestPolicyChangeOutlivesThePolicy(t *testing.T) {
	s := openTemp(t)
	row := PolicyRow{
		ID: "grant-bash", Type: "tool_access", TargetKind: "agent", TargetID: "boss",
		Action: "allow", Source: "operator", SpecJSON: `{"toolName":"bash","state":"allow"}`,
		CreatedAt: policyChangeAt,
	}
	if err := s.PolicyAdd(ctx(), row); err != nil {
		t.Fatalf("add policy: %v", err)
	}
	if err := s.PolicyChangeInsert(ctx(), change("add", "grant-bash")); err != nil {
		t.Fatalf("insert audit: %v", err)
	}
	removed, err := s.PolicyRemove(ctx(), "grant-bash")
	if err != nil || !removed {
		t.Fatalf("remove policy: removed=%v err=%v", removed, err)
	}

	got, err := s.PolicyChangeList(ctx(), "grant-bash", 10)
	if err != nil {
		t.Fatalf("list after removal: %v", err)
	}
	if len(got) != 1 || got[0].Op != "add" {
		t.Fatalf("the audit did not outlive the policy: %+v", got)
	}
}

// TestPolicyChangeNotTrimmedByRetention is THE reason the audit is a
// separate table (PM8 / T0 item 3.3): run policy_event's own trim far into
// the future — the exact statement the daemon start and `aeon policy
// events` run — and the event rows go while the authoring rows are
// untouched. Same suite: the trim must have actually run (the events went),
// or the proof is vacuous.
func TestPolicyChangeNotTrimmedByRetention(t *testing.T) {
	s := openTemp(t)
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, e := range []string{"agent:boss", "agent:worker", "loop:run-1"} {
		// policy_id is the event table's FK-bearing column — "" (NULL)
		// keeps this retention proof independent of the policy table.
		if err := s.PolicyEventInsert(ctx(), "refusal", e, "", "denied by rule", old); err != nil {
			t.Fatalf("seed policy_event: %v", err)
		}
	}
	if err := s.PolicyChangeInsert(ctx(), change("add", "grant-bash")); err != nil {
		t.Fatalf("insert audit: %v", err)
	}
	before, err := s.PolicyChangeCount(ctx())
	if err != nil {
		t.Fatalf("count: %v", err)
	}

	// The retention cutoff far in the future: every event row is older.
	trims, err := s.PolicyEventTrim(ctx(), time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("trim: %v", err)
	}
	if trims != 3 {
		t.Fatalf("the trim took %d event rows, want 3 — the proof is vacuous if the trim did not run", trims)
	}

	after, err := s.PolicyChangeCount(ctx())
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if after != before || after != 1 {
		t.Fatalf("the authoring audit was trimmed: count went %d → %d (want %d unchanged)", before, after, before)
	}
}

// TestPolicyChangeCountIsHonest counts what is there — including zero,
// which is the answer the policy.changes statement turns on (the protocol
// test for the statement itself lives in internal/protocol).
func TestPolicyChangeCountIsHonest(t *testing.T) {
	s := openTemp(t)
	if n, err := s.PolicyChangeCount(ctx()); err != nil || n != 0 {
		t.Fatalf("fresh store: count=%d err=%v, want 0 (an empty audit is a stated answer, not a fault)", n, err)
	}
	if err := s.PolicyChangeInsert(ctx(), change("seed", "")); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if n, err := s.PolicyChangeCount(ctx()); err != nil || n != 1 {
		t.Fatalf("after one append: count=%d err=%v, want 1", n, err)
	}
}

// TestPolicyChangeTxVariantJoinsTheCallerTransaction proves the tx variant
// writes on the caller's transaction — the PM8 "same transaction" path T4/T5
// will use: an append on a tx that ROLLED BACK leaves no audit row (a
// write that did not commit must never be audited as if it had), and an
// append on a tx that COMMITTED is one row.
func TestPolicyChangeTxVariantJoinsTheCallerTransaction(t *testing.T) {
	s := openTemp(t)

	tx, err := s.w.BeginTx(ctx(), nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := s.PolicyChangeInsertTx(ctx(), tx, change("add", "rolled-back")); err != nil {
		t.Fatalf("tx insert: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if got, err := s.PolicyChangeList(ctx(), "rolled-back", 10); err != nil || len(got) != 0 {
		t.Fatalf("a rolled-back append left %d audit rows (err %v) — the audit must commit WITH the write, never before it", len(got), err)
	}

	tx, err = s.w.BeginTx(ctx(), nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := s.PolicyChangeInsertTx(ctx(), tx, change("add", "committed-row")); err != nil {
		t.Fatalf("tx insert: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if got, err := s.PolicyChangeList(ctx(), "committed-row", 10); err != nil || len(got) != 1 {
		t.Fatalf("a committed append left %d audit rows (err %v), want 1", len(got), err)
	}
}

// TestPolicyChangeWriterHasNoProductionCaller pins, as a fact of the tree
// and not a promise, that T3 created the audit path but no production code
// walks it. Renamed at T11 close-out: T3 shipped the writers callable and
// UNCALLED, and this guard pinned zero production callers until the doors
// existed; T4/T5 wired the doors, so the invariant is no longer "nobody"
// but "the doors, and nobody else" — PM8's own words: the caller that
// writes the row is the door that made the write. A production caller
// outside the whitelist below is a second writer, which is the failure
// this guard exists for. The canary clause keeps the guard from reading
// nothing: the definitions themselves must be visible to the same walk,
// and every whitelisted door must still be walking the writers (a door
// that silently stopped auditing its own write fails the floor clause).
func TestPolicyChangeWritersAreOnlyTheAuthoringDoors(t *testing.T) {
	call := mustCompileRE(t, `\.PolicyChangeInsert(Tx)?\(`)
	def := mustCompileRE(t, `func \(s \*Store\) PolicyChangeInsert(Tx)?\(`)

	door := map[string]bool{
		filepath.Join("internal", "desk", "decide.go"):             true, // the commit/reject audit beside the flip (T5)
		filepath.Join("internal", "daemon", "policy_authoring.go"): true, // the add/remove audit beside the write (T4)
		filepath.Join("internal", "daemon", "proposal_decide.go"):  true, // the daemon's store seam (T5)
		filepath.Join("cmd", "aeon", "policy_client.go"):           true, // the direct door's own audit (T8)
		filepath.Join("cmd", "aeon", "proposal_decide.go"):         true, // the CLI's store seam (T5)
	}
	hits := grepRepo(t, call)
	seen := map[string]int{}
	for _, h := range hits {
		if !door[h.file] {
			t.Errorf("%s calls the authoring-audit writer outside the door whitelist — PM8: the caller that writes the row is the door that made the write; anything else is a second writer", h.file)
			continue
		}
		seen[h.file]++
	}
	for f := range door {
		if seen[f] == 0 {
			t.Errorf("%s is whitelisted but calls no writer — either its write stopped auditing itself or the whitelist is stale; a guard whose list has rotted checks nothing", f)
		}
	}
	defs := grepRepo(t, def)
	if len(defs) != 2 {
		t.Fatalf("the walk saw %d writer definitions, want 2 (the ctx and the tx accessor in internal/store/policy_change.go) — a guard that cannot see the definition cannot see a caller either", len(defs))
	}
	// The readers, by contrast, are PRODUCTION-wired: policy.changes reaches
	// the audit through the daemon adapter (the read method is T3's own
	// deliverable). Counted, not assumed: the adapter is the one caller.
	reader := mustCompileRE(t, `\.PolicyChangeList\(|\.PolicyChangeCount\(`)
	hardcoded := 0
	for _, h := range grepRepo(t, reader) {
		if strings.HasPrefix(h.file, filepath.Join("internal", "daemon")) {
			hardcoded++
		}
	}
	if hardcoded == 0 {
		t.Error("no production reader of the audit — policy.changes would be a door to nothing")
	}
}

// TestEnforcementNeverReadsPolicyChange is PM11 / TD2: the four guard-blind
// files — the evaluator, the in-force renderer, and the MCP grants/config
// readers — contain no reference to the audit, in the DDL spelling or the
// Go one. A policy written through any door is judged by exactly the code
// that judged it before the audit existed; the audit is downstream of the
// decision, never upstream of it. A missing file is fatal: a guard that
// reads nothing checks nothing (the walked files are named, not enumerated
// by a pattern, so renaming one is the failure this test wants).
func TestEnforcementNeverReadsPolicyChange(t *testing.T) {
	blind := []string{
		"../../internal/policy/evaluate.go",
		"../../internal/policy/inforce.go",
		"../../internal/mcp/grants.go",
		"../../internal/mcp/config.go",
	}
	for _, path := range blind {
		src := mustReadBlindFileForAudit(t, path)
		for _, needle := range []string{"policy_change", "PolicyChange"} {
			if strings.Contains(src, needle) {
				t.Errorf("%s references %q — the enforcement path must never read the authoring audit (PM11/TD2: guard-blindness is the invariant the audit must not bend)", path, needle)
			}
		}
	}
}

// mustCompileRE compiles a guard pattern; a bad pattern is a test bug that
// must fail loudly, not a silent nil regexp the walk never fires.
func mustCompileRE(t *testing.T, expr string) *regexp.Regexp {
	t.Helper()
	re, err := regexp.Compile(expr)
	if err != nil {
		t.Fatalf("compile %q: %v", expr, err)
	}
	return re
}

// mustReadBlindFileForAudit reads one of the four guard-blind files whole;
// a missing file is fatal — the blindness test proves nothing about a file
// it could not read.
func mustReadBlindFileForAudit(t *testing.T, path string) string {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v — the blindness guard must not vacuously pass a renamed or deleted file", path, err)
	}
	return string(src)
}
