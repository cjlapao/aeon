package policyseed_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/policyseed"
	"github.com/cjlapao/aeon/internal/seed"
	"github.com/cjlapao/aeon/internal/store"
)

var cli = seed.Actor{Kind: "cli", ID: "cli"}

func open(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(store.Options{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func engine(t *testing.T, s *store.Store) *seed.Engine {
	t.Helper()
	e, err := daemon.NewSeedEngine(s, seed.Options{})
	if err != nil {
		t.Fatalf("NewSeedEngine: %v", err)
	}
	return e
}

func seedRows(t *testing.T, s *store.Store) []policy.Row {
	t.Helper()
	rows, err := s.PolicyListAll(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	out := make([]policy.Row, 0, len(rows))
	for _, r := range rows {
		out = append(out, policy.Row{ID: r.ID, Origin: r.Origin, SpecJSON: r.SpecJSON, CreatedAt: r.CreatedAt})
	}
	return out
}

func seedAuditRows(t *testing.T, s *store.Store) []store.PolicyChange {
	t.Helper()
	cs, err := s.PolicyChangeList(context.Background(), "", 1000)
	if err != nil {
		t.Fatalf("changes: %v", err)
	}
	return cs
}

// TestEngineMountsTheCorpusSet is the S45 T12 mount read back through the
// one engine every door holds: the set is declared, class guard, with the
// version its records carry. (The daemon test file carries the startup
// half — TestStartupNeverAppliesPolicyCorpus — this is the door half.)
func TestEngineMountsTheCorpusSet(t *testing.T) {
	s := open(t)
	e := engine(t, s)
	st, err := e.Status(context.Background(), policyseed.SetID)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if len(st) != 1 {
		t.Fatalf("status returned %d sets for the corpus id", len(st))
	}
	if st[0].ID != policyseed.SetID {
		t.Fatalf("set id %q", st[0].ID)
	}
	if st[0].AvailableVersion < 1 || len(st[0].Records) == 0 {
		t.Fatalf("the mounted set is empty: %+v", st[0])
	}
}

// TestApplyCorpusWritesTheWholeSetThenIdempotenceIsZero is PM15/PM16's
// core pair: the first run authors every corpus rule as origin=system with
// its authored event and ONE policy_change seed row; the second run writes
// NOTHING — no rows, no ledger churn, no second audit row (idempotence is
// the ledger's, not an existence check's).
func TestApplyCorpusWritesTheWholeSetThenIdempotenceIsZero(t *testing.T) {
	s := open(t)
	e := engine(t, s)
	ctx := context.Background()
	run, err := policyseed.ApplyCorpus(ctx, e, s, nil, nil, false, cli, time.Now())
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if run.Seeded == 0 || run.Wrote != true {
		t.Fatalf("the first run seeded %d (wrote=%v)", run.Seeded, run.Wrote)
	}
	rows := seedRows(t, s)
	if len(rows) != run.Seeded {
		t.Fatalf("%d rows for %d seeded", len(rows), run.Seeded)
	}
	for _, r := range rows {
		if r.Origin != policy.OriginSystem {
			t.Fatalf("row %s carries origin %q; the seed writes system rows", r.ID, r.Origin)
		}
	}
	if n := countSeedAudit(t, s); n != 1 {
		t.Fatalf("%d seed audit rows after one run; exactly one per run (PM8)", n)
	}

	second, err := policyseed.ApplyCorpus(ctx, e, s, nil, nil, false, cli, time.Now())
	if err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if second.Seeded != 0 || second.Wrote {
		t.Fatalf("the second run wrote %d (wrote=%v) — idempotence is the ledger's", second.Seeded, second.Wrote)
	}
	if second.Unchanged != run.Seeded {
		t.Fatalf("second run unchanged=%d, want %d", second.Unchanged, run.Seeded)
	}
	if n := countSeedAudit(t, s); n != 1 {
		t.Fatalf("%d seed audit rows after a no-op run; a run that wrote nothing owes no row (PM8)", n)
	}
}

// TestApplyCorpusDryRunWritesNothing: the plan's counts, zero rows, zero
// ledger, zero audit — the same claim the CLI verb's dry-run makes.
func TestApplyCorpusDryRunWritesNothing(t *testing.T) {
	s := open(t)
	e := engine(t, s)
	run, err := policyseed.ApplyCorpus(context.Background(), e, s, nil, nil, true, cli, time.Now())
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if run.Seeded == 0 || !run.DryRun {
		t.Fatalf("dry run seeded %d (dry=%v)", run.Seeded, run.DryRun)
	}
	if rows := seedRows(t, s); len(rows) != 0 {
		t.Fatalf("the dry run wrote %d rows", len(rows))
	}
	if n := countSeedAudit(t, s); n != 0 {
		t.Fatalf("the dry run wrote %d audit rows", n)
	}
}

// TestApplyCorpusNamedAgentsOnlyWritesThoseRecords is the per-agent filter:
// naming agents seeds exactly their records — no other agent's rule
// appears. (Agent EXISTENCE is the other gate — TestApplyCorpusRefusesAnAgent
// NobodyHas covers it.)
func TestApplyCorpusNamedAgentsOnlyWritesThoseRecords(t *testing.T) {
	s := open(t)
	e := engine(t, s)
	agent := corpusAgents(t)[0]
	run, err := policyseed.ApplyCorpus(context.Background(), e, s, []string{agent}, nil, false, actorWithAgentRow(t, s, agent), time.Now())
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if run.Seeded == 0 {
		t.Fatal("the named agent seeded nothing")
	}
	for _, r := range seedRows(t, s) {
		if !strings.HasPrefix(r.ID, "corpus."+agent+".") {
			t.Fatalf("row %s belongs to another agent — the filter wrote past its name", r.ID)
		}
	}
}

// TestApplyCorpusRefusesUnknownAgent: an agent the corpus does not carry
// refuses the WHOLE run by name (the P31 answer, reused through
// desk.ResolveSeedTargets — never "seeded zero rules", which reads as
// success).
func TestApplyCorpusRefusesUnknownAgent(t *testing.T) {
	s := open(t)
	e := engine(t, s)
	_, err := policyseed.ApplyCorpus(context.Background(), e, s, []string{"nobody-in-the-corpus"}, nil, false, cli, time.Now())
	if err == nil || !strings.Contains(err.Error(), "nobody-in-the-corpus") {
		t.Fatalf("err = %v; the refusal must name the agent it refused", err)
	}
	if rows := seedRows(t, s); len(rows) != 0 {
		t.Fatalf("the refused run wrote %d rows", len(rows))
	}
}

// TestApplyCorpusRefusesAnAgentNobodyHas is T12 behaviour 2: the corpus
// carries the rules, the AGENT TABLE must carry the agent — seeding
// permissions for a nonexistent agent is refused by name.
func TestApplyCorpusRefusesAnAgentNobodyHas(t *testing.T) {
	s := open(t)
	e := engine(t, s)
	agent := corpusAgents(t)[0] // in the corpus, NOT inserted into the agent table
	_, err := policyseed.ApplyCorpus(context.Background(), e, s, []string{agent}, nil, false, cli, time.Now())
	if err == nil || !strings.Contains(err.Error(), "no such agent in the agent table") {
		t.Fatalf("err = %v; the door must refuse an agent the table does not carry", err)
	}
	if rows := seedRows(t, s); len(rows) != 0 {
		t.Fatalf("the refused run wrote %d rows", len(rows))
	}
}

// TestApplyCorpusRefusesASystemActor: policy_change admits user|cli, and
// D4 admits no more — a system actor at the corpus door is the automatic
// seeder in a costume, refused before any write.
func TestApplyCorpusRefusesASystemActor(t *testing.T) {
	s := open(t)
	e := engine(t, s)
	_, err := policyseed.ApplyCorpus(context.Background(), e, s, nil, nil, false,
		seed.Actor{Kind: "system", ID: "startup"}, time.Now())
	if err == nil || !strings.Contains(err.Error(), "D4 in a costume") {
		t.Fatalf("err = %v; a system actor must be refused by name", err)
	}
	if rows := seedRows(t, s); len(rows) != 0 {
		t.Fatalf("the refused run wrote %d rows", len(rows))
	}
}

// TestApplyCorpusUnknownKeyRefuses: a key that is not a record of the set
// is the door refusing to pretend an unknown record was seeded.
func TestApplyCorpusUnknownKeyRefuses(t *testing.T) {
	s := open(t)
	e := engine(t, s)
	_, err := policyseed.ApplyCorpus(context.Background(), e, s, nil, []string{"corpus.nope.000000000000"}, false, cli, time.Now())
	if err == nil || !strings.Contains(err.Error(), "not a record") {
		t.Fatalf("err = %v", err)
	}
}

// TestRemovedRowStaysRemoved is behaviour 3's second half: an operator's
// removal (through the boundary) is NOT undone by the next apply — the
// ledger says applied, the row is absent, the plan says removed and skips.
// Only the engine's restore door brings it back.
func TestRemovedRowStaysRemoved(t *testing.T) {
	s := open(t)
	e := engine(t, s)
	ctx := context.Background()
	if _, err := policyseed.ApplyCorpus(ctx, e, s, nil, nil, false, cli, time.Now()); err != nil {
		t.Fatalf("apply: %v", err)
	}
	b := boundary(t, s)
	rows := seedRows(t, s)
	victim := rows[len(rows)-1].ID
	if _, err := b.Remove(ctx, victim); err != nil {
		t.Fatalf("boundary remove: %v", err)
	}
	run, err := policyseed.ApplyCorpus(ctx, e, s, nil, nil, false, cli, time.Now())
	if err != nil {
		t.Fatalf("re-apply: %v", err)
	}
	if run.Seeded != 0 {
		t.Fatalf("the re-run re-seeded %d rows — a removed row must STAY removed (SD6)", run.Seeded)
	}
	for _, r := range seedRows(t, s) {
		if r.ID == victim {
			t.Fatal("the removed row came back on its own")
		}
	}
	if _, err := e.Restore(ctx, policyseed.SetID, victim, cli); err != nil {
		t.Fatalf("restore: %v", err)
	}
	var back bool
	for _, r := range seedRows(t, s) {
		if r.ID == victim {
			back = true
			if r.Origin != policy.OriginSystem {
				t.Fatalf("the restored row carries origin %q", r.Origin)
			}
		}
	}
	if !back {
		t.Fatal("the restore door did not bring the row back")
	}
}

// TestHandEditedRowStaysDiverged: an operator's edit of a seeded rule is
// left alone (skipped diverged) by every later run — no silent overwrite.
func TestHandEditedRowStaysDiverged(t *testing.T) {
	s := open(t)
	e := engine(t, s)
	ctx := context.Background()
	if _, err := policyseed.ApplyCorpus(ctx, e, s, nil, nil, false, cli, time.Now()); err != nil {
		t.Fatalf("apply: %v", err)
	}
	rows := seedRows(t, s)
	victim := rows[0].ID
	editedSpec := `{"command":"hand-edited*","state":"deny"}`
	// The divergence: delete + re-add through the boundary with a new spec
	// (the same effect a hand edit has on the content columns).
	b := boundary(t, s)
	if _, err := b.Remove(ctx, victim); err != nil {
		t.Fatalf("remove: %v", err)
	}
	p := policy.Policy{
		ID: victim, Type: policy.CommandAccess,
		Target: policy.Target{Kind: policy.AgentKind, ID: "someone"},
		Action: policy.Deny, Spec: []byte(editedSpec), Source: policy.Operator,
	}
	if rej := b.Add(ctx, p, time.Now()); rej != nil {
		t.Fatalf("add edited row: %s", rej.Reason)
	}
	run, err := policyseed.ApplyCorpus(ctx, e, s, nil, nil, false, cli, time.Now())
	if err != nil {
		t.Fatalf("re-apply: %v", err)
	}
	if run.Seeded != 0 {
		t.Fatalf("the seed overwrote the operator's edit (%d seeded)", run.Seeded)
	}
	var spec string
	for _, r := range seedRows(t, s) {
		if r.ID == victim {
			spec = r.SpecJSON
		}
	}
	if spec != editedSpec {
		t.Fatalf("the edited spec was replaced by the seed: %s", spec)
	}
}

// --- helpers ---------------------------------------------------------------

func countSeedAudit(t *testing.T, s *store.Store) int {
	t.Helper()
	n := 0
	for _, c := range seedAuditRows(t, s) {
		if c.Op == "seed" {
			n++
		}
	}
	return n
}

// boundary builds the write boundary over the same store (the same bridge
// the daemon and CLI wire; test-local because the production bridges are
// unexported wiring).
func boundary(t *testing.T, s *store.Store) *policy.Store {
	t.Helper()
	return policy.NewStore(policy.RepoFuncs{
		Add: func(ctx context.Context, r policy.Row) error {
			return s.PolicyAdd(ctx, store.PolicyRow{
				ID: r.ID, Type: r.Type, TargetKind: r.TargetKind, TargetID: r.TargetID,
				Action: r.Action, Source: r.Source, Subject: r.Subject,
				SpecJSON: r.SpecJSON, NotAfter: r.NotAfter, CreatedAt: r.CreatedAt,
			})
		},
		Remove:      s.PolicyRemove,
		EventInsert: s.PolicyEventInsert,
	})
}

// corpusAgents reads the corpus's agent names (through the same parse the
// door uses — the test asks the corpus, never a remembered name).
func corpusAgents(t *testing.T) []string {
	t.Helper()
	corpus, rej := policy.ParseCorpus(policy.EmbeddedCorpus())
	if rej != nil {
		t.Fatalf("corpus: %s", rej.Reason)
	}
	out := make([]string, 0, len(corpus.Agents))
	for _, a := range corpus.Agents {
		out = append(out, a.Agent)
	}
	return out
}

// actorWithAgentRow inserts an agent row (through the store's own seed
// path — the applier's writers, not a test-side INSERT) so the named agent
// EXISTS for the door's table check.
func actorWithAgentRow(t *testing.T, s *store.Store, agent string) seed.Actor {
	t.Helper()
	if err := ensureAgent(t, s, agent); err != nil {
		t.Fatalf("ensure agent %q: %v", agent, err)
	}
	return cli
}

func ensureAgent(t *testing.T, s *store.Store, name string) error {
	t.Helper()
	return s.SeedWithTx(context.Background(), func(tx *sql.Tx) error {
		// S73 T3: the seed trio carries the engine's own stamp, the way
		// mcpseed stamps mcp_change — a corpus row is a machine's act, and
		// the ledger says so instead of guessing later.
		return s.SeedAgentInsert(context.Background(), tx, store.AgentContent{
			Slug: name, Name: name, Description: "test row for the corpus door",
			Prompt: "", Tools: "[]", Handoff: "{}", Metadata: "{}",
			BodyHash: "ensureAgent", Origin: "system",
		}, store.AgentActor{Kind: store.AgentActorSystem, ID: "seed-engine"}, "2026-11-06T09:00:00Z")
	})
}
