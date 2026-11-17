package store

import (
	"strings"
	"testing"
)

// proposalRoadmapIdeaMigrationIndex finds this file's position in
// Migrations() by NAME (the identity-based vintage pattern every rebuild
// test uses): a later slice adding a file cannot shift a positional cut.
func proposalRoadmapIdeaMigrationIndex() int {
	for i, m := range Migrations() {
		if strings.HasSuffix(m.Name, "_proposal_roadmap_idea") {
			return i
		}
	}
	panic("the proposal_roadmap_idea migration is missing from Migrations()")
}

// TestProposalKindMigrationPreservesRowsAndIndexesRoadmapIdea — S50 T5's
// rebuild (the S46/S48/S49 spec-table name was taken the same way; the
// suffix is the recorded collision, the S49 precedent carried), proven the
// S57 T8 way: a fresh database and a database upgraded from the previous
// vintage come out with the SAME shape; proposals staged in EVERY kind the
// previous vintage admitted — with decision state on two of them — SURVIVE
// row-for-row (all fourteen the previous vintage admitted — the S49 test
// named its own thirteen; this vintage carries one more); the kind CHECK
// widened by exactly the one S50 word:
// 'roadmap_idea' is admitted, and the five invented kinds RM8 keeps out —
// roadmap_update, roadmap_cancel, roadmap_estimate, roadmap_priority,
// roadmap_dispatch, the exact verbs that would move a row, retire it, size
// it, rank it or start it — stay refused, beside one shape no desk knows at
// all; BELOW the rebuild the new kind was refused (proves THIS migration did
// the widening); and proposal still carries no index (0045's T0 Q1).
func TestProposalKindMigrationPreservesRowsAndIndexesRoadmapIdea(t *testing.T) {
	idx := proposalRoadmapIdeaMigrationIndex()
	all := Migrations()

	fresh := openTemp(t)
	freshFp, err := fresh.SchemaFingerprint()
	if err != nil {
		t.Fatalf("fresh fingerprint: %v", err)
	}

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
		{"policy_remove", "u1"},
		{"seed_corpus", "u1"},
		{"mcp_server_add", "manny"},
		{"mcp_server_remove", "manny"},
		{"agent_import", "manny"},
		{"autonomy_revoke", "manny"},
		{"autonomy_deny", "manny"},
		{"mcp_grant_revoke", "manny"},
		{"verify_command", "manny"},
		{"merge_rule", "manny"},
		{"blueprint_save", "manny"},
		{"environment_restrict", "manny"},
		{"automation_disable", "manny"},
	}
	for _, p := range staged {
		if _, cerr := old.CreateProposal(p.kind, `{"x":1}`, Actor{Kind: actorKindFor(p.byID), ID: p.byID}, "2026-11-04T00:00:00Z"); cerr != nil {
			t.Fatalf("seed the staged %s: %v", p.kind, cerr)
		}
	}
	if err := old.GormDB().Exec(`UPDATE proposal SET status='committed', decided_at='2026-11-04T01:00:00Z', decided_by_kind='user', decided_by_id='u1', decide_reason='yes' WHERE created_by_id='u1' AND kind='seed_corpus'`).Error; err != nil {
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

	// The CHECK widened by exactly the one S50 word.
	if _, cerr := s.CreateProposal(ProposalKindRoadmapIdea, `{"componentRoot":"/srv/x","title":"carry health into doctor","note":"ops sees the settings nowhere else"}`, Actor{Kind: "agent", ID: "manny"}, "2026-11-04T02:00:00Z"); cerr != nil {
		t.Fatalf("roadmap_idea must be admitted: %v", cerr)
	}
	// And the FIFTEENTH kind is the only new door: the five verbs RM8 keeps
	// out of the proposal plane — the ones that would MOVE a row, RETIRE it,
	// SIZE it, RANK it or START it — stay refused, beside one shape no desk
	// knows at all.
	for _, invented := range []string{"roadmap_update", "roadmap_cancel", "roadmap_estimate", "roadmap_priority", "roadmap_dispatch", "not_a_kind_at_all"} {
		if _, cerr := s.CreateProposal(invented, `{}`, Actor{Kind: "user", ID: "u1"}, "2026-11-04T02:00:00Z"); cerr == nil {
			t.Fatalf("the invented kind %s must be refused — the CHECK widened by exactly one word (RM8)", invented)
		}
	}

	// Below the rebuild the new kind was refused: proves THIS migration did
	// the widening.
	dir2 := t.TempDir()
	g2, closeDB2 := rawGorm(t, dir2+"/state.db")
	applyMigrations(t, g2, all[:idx])
	if err := g2.Exec(`INSERT INTO proposal (kind,payload,status,created_at,created_by_kind,created_by_id)
		VALUES (?,?,?,?,?,?)`, "roadmap_idea", "{}", "proposed", "2026-11-04T00:00:00Z", "agent", "manny").Error; err == nil {
		t.Fatal("roadmap_idea was accepted BEFORE the T5 rebuild — the vintage is wrong")
	}
	closeDB2()

	var idxCount int
	if err := s.r.QueryRow(`SELECT COUNT(*) FROM pragma_index_list('proposal')`).Scan(&idxCount); err != nil {
		t.Fatal(err)
	}
	if idxCount != 0 {
		t.Errorf("proposal carries %d indexes; the rebuild copies the table exactly — it had none to keep", idxCount)
	}
}
