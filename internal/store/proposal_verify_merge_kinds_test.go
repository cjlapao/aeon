package store

import (
	"strings"
	"testing"
)

// proposalVerifyMergeMigrationIndex is this file's position in Migrations(),
// found by NAME (the S62 identity-based vintage pattern): a later slice
// adding a file cannot shift a positional cut out from under this test.
func proposalVerifyMergeMigrationIndex() int {
	for i, m := range Migrations() {
		if strings.HasSuffix(m.Name, "_proposal_verify_merge_kinds") {
			return i
		}
	}
	panic("the proposal_verify_merge_kinds migration is missing from Migrations()")
}

// TestMigrationM4 — the 33rd timestamped file (total 78, the count the
// sibling count-pins test holds): a fresh database and a database upgraded
// from the previous vintage come out with the SAME shape; the staged
// proposals SURVIVE the rebuild row-for-row (the copy-guard's point); and
// the kind CHECK widened by exactly the two S57 words — a widening-shaped
// name is still refused after the rebuild.
func TestMigrationM4(t *testing.T) {
	idx := proposalVerifyMergeMigrationIndex()
	all := Migrations()

	// The fresh shape first — the fingerprint the upgrade must reach.
	fresh := openTemp(t)
	freshFp, err := fresh.SchemaFingerprint()
	if err != nil {
		t.Fatalf("fresh fingerprint: %v", err)
	}

	// The upgrade: a store at the previous vintage (everything but the
	// proposal_verify_merge_kinds file, cut by name) holds proposals in
	// every kind the vintage admitted. The rebuild must carry them all.
	dir := t.TempDir()
	old, err := Open(Options{Dir: dir, SkipMigrations: true, Env: emptyEnv, Clock: fixedClock()})
	if err != nil {
		t.Fatalf("open for seeding: %v", err)
	}
	applyMigrations(t, old.GormDB(), all[:idx])
	staged := []struct {
		kind string
		byID string
	}{
		{"policy_add", "u1"},
		{"seed_corpus", "u1"},
		{"mcp_server_add", "manny"},
		{"agent_import", "manny"},
		{"autonomy_revoke", "manny"},
		{"mcp_grant_revoke", "manny"},
	}
	for _, p := range staged {
		if _, cerr := old.CreateProposal(p.kind, `{"x":1}`, Actor{Kind: actorKindFor(p.byID), ID: p.byID}, "2026-11-02T00:00:00Z"); cerr != nil {
			t.Fatalf("seed the staged %s: %v", p.kind, cerr)
		}
	}
	// Two of them carry decision state — the copy must move it, not just
	// the kind (a rebuild that drops decide_reason drops the audit).
	if err := old.GormDB().Exec(`UPDATE proposal SET status='committed', decided_at='2026-11-02T01:00:00Z', decided_by_kind='user', decided_by_id='u1', decide_reason='yes' WHERE created_by_id='u1' AND kind='seed_corpus'`).Error; err != nil {
		t.Fatalf("stage the committed row: %v", err)
	}
	if err := old.GormDB().Exec(`UPDATE proposal SET status='rejected', decide_reason='not now' WHERE kind='agent_import'`).Error; err != nil {
		t.Fatalf("stage the rejected row: %v", err)
	}
	if err := old.Close(); err != nil {
		t.Fatalf("close after seeding: %v", err)
	}
	s, err := Open(Options{Dir: dir, Env: emptyEnv, Clock: fixedClock()})
	if err != nil {
		t.Fatalf("migrating open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	upFp, err := s.SchemaFingerprint()
	if err != nil {
		t.Fatalf("upgraded fingerprint: %v", err)
	}
	if !freshFp.Equal(upFp) {
		t.Fatalf("fresh and upgraded shapes differ:\n%s", strings.Join(freshFp.Diff(upFp), "\n"))
	}

	// The copy was total: every staged row is present with its decision
	// state intact (the copy-guard's point, re-proven from the outside).
	var n int
	if err := s.r.QueryRow(`SELECT COUNT(*) FROM proposal`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != len(staged) {
		t.Fatalf("proposal count after the rebuild = %d, want %d — the copy lost rows", n, len(staged))
	}
	var st, rej string
	if err := s.r.QueryRow(`SELECT status, decide_reason FROM proposal WHERE kind='seed_corpus'`).Scan(&st, &rej); err != nil {
		t.Fatal(err)
	}
	if st != "committed" || rej != "yes" {
		t.Fatalf("the committed row read status=%q reason=%q after the rebuild — the decision state did not survive", st, rej)
	}

	// The CHECK widened by exactly the two S57 words: both are admitted
	// now, and a WIDENING-shaped name is still refused (VG17: an agent may
	// propose add and tighten, never remove/loosen).
	if _, cerr := s.CreateProposal(ProposalKindVerifyCommand, `{"op":"add"}`, Actor{Kind: "agent", ID: "manny"}, "2026-11-02T02:00:00Z"); cerr != nil {
		t.Fatalf("verify_command must be admitted: %v", cerr)
	}
	if _, cerr := s.CreateProposal(ProposalKindMergeRule, `{"rule":{}}`, Actor{Kind: "agent", ID: "manny"}, "2026-11-02T02:00:00Z"); cerr != nil {
		t.Fatalf("merge_rule must be admitted: %v", cerr)
	}
	for _, widening := range []string{"verify_remove", "verify_archive", "merge_rule_loosen", "dod_mark"} {
		if _, cerr := s.CreateProposal(widening, `{}`, Actor{Kind: "user", ID: "u1"}, "2026-11-02T02:00:00Z"); cerr == nil {
			t.Fatalf("a WIDENING-shaped kind named %s must be refused — the CHECK widened by exactly two words", widening)
		}
	}

	// And below the rebuild both kinds were still refused: proves THIS
	// migration did the widening.
	dir2 := t.TempDir()
	g2, closeDB2 := rawGorm(t, dir2+"/state.db")
	applyMigrations(t, g2, all[:idx])
	for _, kind := range []string{"verify_command", "merge_rule"} {
		if err := g2.Exec(`INSERT INTO proposal (kind,payload,status,created_at,created_by_kind,created_by_id)
			VALUES (?,?,?,?,?,?)`, kind, "{}", "proposed", "2026-11-02T00:00:00Z", "agent", "manny").Error; err == nil {
			t.Fatalf("%s was accepted BEFORE the T8 rebuild — the vintage is wrong", kind)
		}
	}
	closeDB2()

	// proposal carries no index (0045's T0 Q1, re-verified after the
	// rebuild — and the rebuild must not have LEFT one either).
	var idxCount int
	if err := s.r.QueryRow(`SELECT COUNT(*) FROM pragma_index_list('proposal')`).Scan(&idxCount); err != nil {
		t.Fatal(err)
	}
	if idxCount != 0 {
		t.Errorf("proposal carries %d indexes; the rebuild copies the table exactly — it had none to keep", idxCount)
	}
}

// actorKindFor maps a staged by-id to its kind: 'u1' is the user, the
// others are agent names (the two author kinds the seeded rows carry).
func actorKindFor(byID string) string {
	if byID == "u1" {
		return "user"
	}
	return "agent"
}
