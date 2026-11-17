package store

// proposal_mcp_grant_revoke_test.go — S46 T6: the seventh proposal kind.
// The rebuild must be invisible to every row it moves (the S37 T1 precedent
// re-run against OUR widening) and the CHECK must accept exactly the Go
// mirror — one list, said twice, drift caught here not in production.

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

// grantRevokeMigrationIndex finds the T6 rebuild in the ordered runner.
// Matching by NAME (not by position) keeps this honest if another slice
// lands a migration beside ours.
func grantRevokeMigrationIndex(t *testing.T) int {
	t.Helper()
	for i, m := range Migrations() {
		if strings.HasSuffix(m.Name, "_proposal_mcp_grant_revoke") {
			return i
		}
	}
	t.Fatal("the S46 T6 rebuild migration is missing from the runner — the seventh kind cannot exist without it")
	return -1
}

func TestProposalKindMigrationPreservesRowsAndIndexes(t *testing.T) {
	idx := grantRevokeMigrationIndex(t)
	dir := t.TempDir()
	path := dir + "/state.db"

	// Stage the world BEFORE the rebuild: one row per pre-existing kind,
	// each with distinct decided-state fields, so a lost column shows up as
	// a mismatch rather than a missing count.
	pre := []struct {
		kind, payload, status, byKind, byID, decKind, decID, reason, applied string
	}{
		{"policy_add", `{"id":"p1"}`, "proposed", "agent", "golang-engineer", "", "", "", ""},
		{"policy_remove", `{"id":"p2"}`, "committed", "user", "1000", "user", "1000", "", ""},
		{"seed_corpus", `{"agent":"manny"}`, "rejected", "agent", "manny", "user", "1000", "not this way", ""},
		{"mcp_server_add", `{"tier":"public","server":{"name":"github"}}`, "discarded", "trigger", "sched-1", "", "", "", ""},
		{"mcp_server_remove", `{"name":"github"}`, "proposed", "agent", "peer", "", "", "", "earlier crash"},
		{"agent_import", `{"pkgPath":"/abs/x"}`, "committed", "agent", "golang-engineer", "user", "9", "", ""},
	}
	g, closeDB := rawGorm(t, path)
	applyMigrations(t, g, Migrations()[:idx]) // everything BEFORE the widening
	for i, p := range pre {
		decided := any(nil)
		decKind, decID, reason, applied := any(nil), any(nil), any(nil), any(nil)
		if p.decKind != "" {
			decided = "2026-10-29T01:00:00Z"
			decKind, decID = p.decKind, p.decID
		}
		if p.reason != "" {
			reason = p.reason
		}
		if p.applied != "" {
			applied = p.applied
		}
		if err := g.Exec(`INSERT INTO proposal (id,kind,payload,status,created_at,created_by_kind,created_by_id,decided_at,decided_by_kind,decided_by_id,decide_reason,applied_error)
			VALUES (?, ?, ?, ?, '2026-10-29T00:00:00Z', ?, ?, ?, ?, ?, ?, ?)`,
			int64(100+i), p.kind, p.payload, p.status, p.byKind, p.byID,
			decided, decKind, decID, reason, applied).Error; err != nil {
			t.Fatalf("stage %s: %v", p.kind, err)
		}
	}
	closeDB()

	// Apply the rest — the rebuild included. Open fresh so the whole list
	// runs through the real runner.
	s, err := Open(Options{Dir: dir, Clock: fixedClock(), Env: emptyEnv})
	if err != nil {
		t.Fatalf("open after the rebuild: %v", err)
	}
	defer func() { _ = s.Close() }()

	got := map[string]struct {
		payload, status, byKind, byID, decKind, decID, reason, applied string
	}{}
	rows, qerr := s.r.Query(`SELECT id,kind,payload,status,created_by_kind,created_by_id,
		COALESCE(decided_by_kind,''),COALESCE(decided_by_id,''),COALESCE(decide_reason,''),COALESCE(applied_error,'') FROM proposal ORDER BY id`)
	if qerr != nil {
		t.Fatal(qerr)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var kind, payload, status, byKind, byID, decKind, decID, reason, applied string
		if serr := rows.Scan(&id, &kind, &payload, &status, &byKind, &byID, &decKind, &decID, &reason, &applied); serr != nil {
			t.Fatal(serr)
		}
		got[kind] = struct {
			payload, status, byKind, byID, decKind, decID, reason, applied string
		}{payload, status, byKind, byID, decKind, decID, reason, applied}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(got) != len(pre) {
		t.Fatalf("the rebuild moved %d kinds, staged %d", len(got), len(pre))
	}
	for _, p := range pre {
		g, ok := got[p.kind]
		if !ok {
			t.Fatalf("kind %s lost its row", p.kind)
		}
		if g.payload != p.payload || g.status != p.status || g.byKind != p.byKind || g.byID != p.byID ||
			g.decKind != p.decKind || g.decID != p.decID || g.reason != p.reason || g.applied != p.applied {
			t.Errorf("row %s drifted:\n staged %+v\n landed   %+v", p.kind, p, g)
		}
	}

	// Indexes: proposal carries none (T0 Q1, re-verified) — and the rebuild
	// must not have LEFT any stray either.
	var idxCount int
	if qerr := s.r.QueryRow(`SELECT COUNT(*) FROM pragma_index_list('proposal')`).Scan(&idxCount); qerr != nil {
		t.Fatal(qerr)
	}
	if idxCount != 0 {
		t.Errorf("proposal carries %d indexes; the rebuild copies the table exactly — it had none to keep", idxCount)
	}

	// The new kind is accepted (Go mirror path), and a ninth kind is not
	// (the CHECK widened by exactly one word).
	if _, cerr := s.CreateProposal(ProposalKindMCPGrantRevoke, `{"agent":"manny","qualified":"gitforge.pr"}`, Actor{Kind: "agent", ID: "manny"}, "2026-11-01T00:00:00Z"); cerr != nil {
		t.Fatalf("mcp_grant_revoke must be admitted: %v", cerr)
	}
	if _, cerr := s.CreateProposal("mcp_grant_declare", "{}", Actor{Kind: "user", ID: "u"}, "2026-11-01T00:00:00Z"); cerr == nil {
		t.Fatal("a WIDENING kind named mcp_grant_declare must be refused (MG9) — the CHECK widened by exactly one word")
	}
	// And below the rebuild the new kind was still refused: proves THIS
	// migration did the widening.
	dir2 := t.TempDir()
	g2, closeDB2 := rawGorm(t, dir2+"/state.db")
	applyMigrations(t, g2, Migrations()[:idx])
	if err := g2.Exec(`INSERT INTO proposal (kind,payload,status,created_at,created_by_kind,created_by_id)
		VALUES ('mcp_grant_revoke','{}','proposed','2026-11-01T00:00:00Z','user','u1')`).Error; err == nil {
		t.Fatal("mcp_grant_revoke must be refused BEFORE the T6 rebuild (the CHECK not yet widened)")
	}
	closeDB2()
}

func TestProposalKindListMatchesGoMirror(t *testing.T) {
	s := openTemp(t)
	var ddl string
	if err := s.r.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='proposal'`).Scan(&ddl); err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?s)kind\s+TEXT NOT NULL\s+CHECK\s*\(kind\s+IN\s*\((.*?)\)`).FindStringSubmatch(ddl)
	if m == nil {
		t.Fatalf("cannot find the kind CHECK in the live DDL:\n%s", ddl)
	}
	quoted := regexp.MustCompile(`'([^']+)'`).FindAllStringSubmatch(m[1], -1)
	var check []string
	for _, q := range quoted {
		check = append(check, q[1])
	}
	sort.Strings(check)
	mirror := append([]string(nil), ProposalKinds...)
	sort.Strings(mirror)
	if strings.Join(check, ",") != strings.Join(mirror, ",") {
		t.Fatalf("the store CHECK and the Go mirror drifted:\n CHECK %v\n Go    %v", check, mirror)
	}
	// The count is NOT pinned here — kinds grow with slices (S56 merged two
	// beside ours); the LOCK is the equality with the Go mirror above, which
	// is what the CHECK-vs-code drift can survive and this test catches.
	if len(check) < 9 {
		t.Errorf("at least the nine kinds (S35's five + agent_import + S56's two + ours) expected, got %d: %v", len(check), check)
	}
	// The mirror guards first: CreateProposal refuses what the CHECK would
	// refuse, with its own named sentence.
	if _, err := s.CreateProposal("mcp_grant_declare", "{}", Actor{Kind: "user", ID: "u"}, "2026-11-01T00:00:00Z"); err == nil || !strings.Contains(err.Error(), "is not in") {
		t.Fatalf("the Go mirror must refuse a non-kind by name: %v", err)
	}
}
