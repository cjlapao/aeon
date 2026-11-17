package store

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"
)

// T6's store-side tests: the SQL text, tested as SQL. The domain rule (PL1)
// is not here — it is asserted upstairs in internal/policy/store_test.go, and
// these rows deliberately use only vocabulary the 0016 CHECKs accept, because
// a store test that fights the CHECK never reaches what it means to measure.

// addRow is the least a row can be and still insert: a Deny (the direction
// every source may author), no expiry, spec supplied by the caller.
func addRow(t *testing.T, s *Store, id, kind, targetID, action, source, spec, notAfter string) PolicyRow {
	t.Helper()
	r := PolicyRow{
		ID: id, Type: "tool_access", TargetKind: kind, TargetID: targetID,
		Action: action, Source: source, SpecJSON: spec, NotAfter: notAfter,
		CreatedAt: "2026-10-19T12:00:00.000Z",
	}
	if err := s.PolicyAdd(context.Background(), r); err != nil {
		t.Fatalf("PolicyAdd(%q): %v", id, err)
	}
	return r
}

func TestPolicyAddListRemoveRoundTrip(t *testing.T) {
	// The CRUD circle over the real table, on the fixture every store test
	// uses. One all-agents deny, one named-target allow: the two shapes the
	// engine actually stores.
	s := openTemp(t)
	ctx := context.Background()

	addRow(t, s, "no-shell", "all-agents", "", "deny", "operator", `{"toolName":"bash","state":"deny"}`, "")
	addRow(t, s, "read-only", "agent", "boss", "allow", "operator", `{"toolName":"read","state":"allow"}`, "")

	got, err := s.PolicyListAll(ctx)
	if err != nil {
		t.Fatalf("PolicyListAll: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d rows, want 2: %+v", len(got), got)
	}
	// Ordered by id: "no-shell" < "read-only". The order is what makes a List
	// output stable across runs (mirrors ListRoutes).
	if got[0].ID != "no-shell" || got[1].ID != "read-only" {
		t.Errorf("rows came back in id order %q, %q", got[0].ID, got[1].ID)
	}
	first := got[0]
	if first.Type != "tool_access" || first.TargetKind != "all-agents" || first.Action != "deny" ||
		first.Source != "operator" || first.SpecJSON != `{"toolName":"bash","state":"deny"}` ||
		first.NotAfter != "" || first.CreatedAt != "2026-10-19T12:00:00.000Z" {
		t.Errorf("the round trip changed the row: %+v", first)
	}
	if got[1].TargetID != "boss" {
		t.Errorf("target id survived as %q", got[1].TargetID)
	}

	// Remove reports whether a row existed, both ways.
	existed, err := s.PolicyRemove(ctx, "no-shell")
	if err != nil || !existed {
		t.Fatalf("removing an existing row: existed=%v err=%v", existed, err)
	}
	existed, err = s.PolicyRemove(ctx, "no-shell")
	if err != nil || existed {
		t.Fatalf("removing it again: existed=%v err=%v (must report false, not error)", existed, err)
	}
	got, err = s.PolicyListAll(ctx)
	if err != nil || len(got) != 1 || got[0].ID != "read-only" {
		t.Fatalf("after removal: %+v err=%v", got, err)
	}
}

func TestAllAgentsStoresNULLTargetIDNotTheEmptyString(t *testing.T) {
	// The seed T5's measured rule: the all-agents kind names every agent, so
	// its target_id is NULL — an empty string is a sentinel every later reader
	// has to remember to ignore, which is how sentinels start being read.
	s := openTemp(t)
	addRow(t, s, "wide", "all-agents", "", "deny", "agent", `{"toolName":"bash","state":"deny"}`, "")

	var kind string
	var target sql.NullString
	err := s.r.QueryRow(`SELECT target_kind, target_id FROM policy WHERE id = 'wide'`).Scan(&kind, &target)
	if err != nil {
		t.Fatalf("read the row: %v", err)
	}
	if target.Valid {
		t.Errorf("all-agents stored target_id = %q (Valid); it must be NULL", target.String)
	}
	if kind != "all-agents" {
		t.Errorf("target_kind came back as %q", kind)
	}
}

func TestNoExpiryStoresNULLNotAfter(t *testing.T) {
	// PL7 again as storage semantics: "" on the seam means "never expires",
	// the table says so with NULL, and the read path folds NULL back to "".
	// A stored "" and a stored NULL would be two spellings of no-expiry, and
	// two spellings is how one of them becomes an expiry nobody chose.
	s := openTemp(t)
	ctx := context.Background()
	addRow(t, s, "ever", "all-agents", "", "deny", "operator", `{}`, "")
	addRow(t, s, "brief", "all-agents", "", "deny", "operator", `{}`, "2027-01-01T00:00:00Z")

	var n1, n2 sql.NullString
	if err := s.r.QueryRow(`SELECT not_after FROM policy WHERE id = 'ever'`).Scan(&n1); err != nil {
		t.Fatal(err)
	}
	if err := s.r.QueryRow(`SELECT not_after FROM policy WHERE id = 'brief'`).Scan(&n2); err != nil {
		t.Fatal(err)
	}
	if n1.Valid {
		t.Errorf("no-expiry stored as %q; must be NULL", n1.String)
	}
	if !n2.Valid || n2.String != "2027-01-01T00:00:00Z" {
		t.Errorf("the expiry did not survive: %+v", n2)
	}

	rows, err := s.PolicyListAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.ID == "ever" && r.NotAfter != "" {
			t.Errorf("NULL read back as %q, want \"\"", r.NotAfter)
		}
	}
}

func TestExpiredPoliciesAreStillReturnedByTheStore(t *testing.T) {
	// D6/D8 at the SQL layer: no WHERE on not_after anywhere. Expiry is
	// Evaluate's decision against `at`; a store that hid expired rows would
	// make T3's nanosecond boundary unreachable and make "what existed"
	// depend on when someone asked.
	s := openTemp(t)
	addRow(t, s, "old-news", "all-agents", "", "deny", "operator", `{}`, "2000-01-01T00:00:00Z")

	rows, err := s.PolicyListAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != "old-news" {
		t.Fatalf("the expired row vanished from the store: %+v", rows)
	}
	if rows[0].NotAfter != "2000-01-01T00:00:00Z" {
		t.Errorf("the expired row lost its expiry: %q", rows[0].NotAfter)
	}
}

func TestSubjectIsDerivedFromTheTargetColumnsAndNeverWritten(t *testing.T) {
	// 0016 has no subject column (legal constraint 1; the subject of a
	// refusal lives in policy_event). Subject on the Row is therefore read-
	// side derivation — which means two claims to test: it renders exactly
	// like Target.String() (kind:id, bare kind for all-agents), and whatever
	// a caller puts in it on the way IN is ignored, because a value that
	// silently vanished on insert would read as a stored fact.
	s := openTemp(t)
	ctx := context.Background()
	addRow(t, s, "wide", "all-agents", "", "deny", "operator", `{}`, "")
	addRow(t, s, "named", "lane-type", "review", "deny", "operator", `{}`, "")

	// A row whose Subject the caller tried to set.
	if err := s.PolicyAdd(ctx, PolicyRow{
		ID: "lied", Type: "tool_access", TargetKind: "agent", TargetID: "boss",
		Action: "deny", Source: "agent", Subject: "loop:whatever-I-said", SpecJSON: `{}`,
		CreatedAt: "2026-10-19T12:00:00.000Z",
	}); err != nil {
		t.Fatal(err)
	}

	rows, err := s.PolicyListAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"wide": "all-agents", "named": "lane-type:review", "lied": "agent:boss"}
	if len(rows) != len(want) {
		t.Fatalf("rows: %+v", rows)
	}
	for _, r := range rows {
		if w, ok := want[r.ID]; !ok {
			t.Errorf("unexpected row %q", r.ID)
		} else if r.Subject != w {
			t.Errorf("row %q derived Subject %q, want %q", r.ID, r.Subject, w)
		}
	}
	// And the table itself never saw "lied"'s claim:
	var n int
	if err := s.r.QueryRow(`SELECT COUNT(*) FROM policy WHERE id = 'lied' AND spec = '{}'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("the row itself: n=%d err=%v", n, err)
	}
}

func TestPolicyAddRefusesIncompleteRowsNamingTheField(t *testing.T) {
	// Fail-fast BEFORE the statement (Learnings 2026-08-03): these are not
	// vocabulary refusals (the CHECKs own vocabulary) — they name the missing
	// field rather than letting NOT NULL pass a blank or a CHECK fire with no
	// field named. Each case differs from a complete row in exactly one
	// field.
	s := openTemp(t)
	base := PolicyRow{
		ID: "r", Type: "tool_access", TargetKind: "all-agents",
		Action: "deny", Source: "operator", SpecJSON: `{}`, CreatedAt: "2026-10-19T12:00:00.000Z",
	}
	cases := []struct {
		name   string
		mutate func(*PolicyRow)
		want   string
	}{
		{"no id", func(r *PolicyRow) { r.ID = "" }, "id"},
		{"no type", func(r *PolicyRow) { r.Type = "" }, "type"},
		{"no action", func(r *PolicyRow) { r.Action = "" }, "action"},
		{"no source", func(r *PolicyRow) { r.Source = "" }, "source"},
		{"no target kind", func(r *PolicyRow) { r.TargetKind = "" }, "target kind"},
		{"no created_at", func(r *PolicyRow) { r.CreatedAt = "" }, "created_at"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := base
			r.ID = "row-" + strings.ReplaceAll(c.name, " ", "-")
			c.mutate(&r)
			err := s.PolicyAdd(context.Background(), r)
			if err == nil {
				t.Fatalf("the incomplete row was accepted: %+v", r)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("the error does not name %q: %v", c.want, err)
			}
		})
	}
}

func TestPolicyAddEmptySpecBecomesTheColumnDefault(t *testing.T) {
	// spec is NOT NULL DEFAULT '{}'. The seam says "no spec" by leaving
	// SpecJSON empty; the table gets its documented default, not "".
	s := openTemp(t)
	addRow(t, s, "bare", "all-agents", "", "deny", "operator", "", "")

	var spec string
	if err := s.r.QueryRow(`SELECT spec FROM policy WHERE id = 'bare'`).Scan(&spec); err != nil {
		t.Fatal(err)
	}
	if spec != "{}" {
		t.Errorf("spec is %q, want {}", spec)
	}
}

func TestPolicyAddClashesOnADuplicateIDErrors(t *testing.T) {
	// INSERT, not upsert: "changed a policy" is remove-then-add, so no silent
	// overwrite of a live rule exists at this layer. The clash is an error
	// (infrastructure vocabulary) naming the id — the boundary folds it.
	s := openTemp(t)
	addRow(t, s, "once", "all-agents", "", "deny", "operator", `{}`, "")
	err := s.PolicyAdd(context.Background(), PolicyRow{
		ID: "once", Type: "tool_access", TargetKind: "all-agents",
		Action: "deny", Source: "operator", SpecJSON: `{}`, CreatedAt: "2026-10-19T12:00:00.000Z",
	})
	if err == nil || !strings.Contains(err.Error(), "once") {
		t.Fatalf("the duplicate id was %v; want an error naming the id", err)
	}
}

func TestPolicyMethodsReportAFailedStatement(t *testing.T) {
	// The error channel is real, and it is the only channel: every failure
	// must surface as a wrapped error naming the operation, not a panic and
	// not an empty answer (an empty List from a dead reader would read as
	// "no policies" — "default deny" wearing the costume of success while a
	// deny was in force). A dropped table is the honest way to fire this
	// without breaking the package's Close contract (Close nils the pools;
	// use-after-Close is a framework-ordered impossibility, not a case).
	s := openTemp(t)
	addRow(t, s, "here", "all-agents", "", "deny", "operator", `{}`, "")
	if _, err := s.w.Exec(`DROP TABLE policy`); err != nil {
		t.Fatalf("drop: %v", err)
	}
	ctx := context.Background()

	err := s.PolicyAdd(ctx, PolicyRow{ID: "late", Type: "tool_access", TargetKind: "all-agents",
		Action: "deny", Source: "operator", CreatedAt: "2026-10-19T12:00:00.000Z"})
	if err == nil || !strings.Contains(err.Error(), "add policy") {
		t.Errorf("PolicyAdd: %v", err)
	}
	if _, err := s.PolicyRemove(ctx, "here"); err == nil || !strings.Contains(err.Error(), "remove policy") {
		t.Errorf("PolicyRemove: %v", err)
	}
	if rows, err := s.PolicyListAll(ctx); err == nil || !strings.Contains(err.Error(), "list policies") {
		t.Errorf("PolicyListAll: %d rows, %v", len(rows), err)
	}
}

func TestThePolicyMethodsAddNoMigration(t *testing.T) {
	// N4, as amended by ruling R1: S10 owns exactly ONE migration, 0016
	// (T1 landed it). The ledger later grew a second entry — 0018, the
	// deferred-findings migration (SEC-022(a) + DEF-027) — which arrived at
	// the findings gate, not from this task or any other S10 build task. And
	// it grew a third from OUTSIDE S10 entirely: S11's 0019 (ask in the action
	// CHECK, loop.attended for the pin), the slice's own single migration —
	// and a fourth, 0020_step_pause_reason, from S9's stop/continue — and a
	// fifth, 0021_step_steer, from S31's durable steer queue.
	// Asserting before-and-after still makes "none HERE" the property rather
	// than trusting nobody counted: 0019, 0020 and 0021 moved the count, this
	// task moved it not at all.
	before := len(Migrations())
	// The frozen legacy block is exactly 45; file-based migrations
	// (internal/store/migrations/*.sql, adopted 2026-10-30 for parallel
	// worktrees) append after it and are additive by design, so a total beyond
	// 45 is expected, not a violation of "this task added no legacy migration"
	// (the legacy count and its 0045 last are pinned exactly in counters_test).
	if before != frozenMigrationCount() {
		t.Fatalf("Migrations() has %d entries, want 45 (0016 by ruling R1, 0018 by the deferred-findings "+
			"gate, 0019 by S11's D1/D2, 0020 by S9's stop/continue, 0021 by S31's steer queue, 0022 by S14's "+
			"agent_task/agent_chain, 0023 by S15's plugin_agent/plugin_transition, 0024 by S15's refused/ledger, "+
			"0025 by S16's action_intent, 0026 by the model-management wave's route metadata, 0027 by S18's M26 "+
			"provenance, 0028 by S18's Q8 wire flags and O8 no-price declaration, 0029 by S18 T3's route_probe probe cache, 0030 by S18 T5's step thinking-drop count, 0031 by S19 T6's per-loop outward opt-in, 0032 by S20 T1's trigger_schedule/trigger_fire, 0033–0034 by S20's fire inputs and trigger_gap, 0035–0036 by S38's layers and records, 0037–0038 by S39's runs and message source, 0039 by S40 T1's automation tables, 0040 by S22 T1's retrieval index, 0041 by S24 T1's loop queue, 0042 by S35 T1's proposal table, 0043 by S26 T1's roadmap_item, 0044 by S29 T1's bug — none of them this task's)", before)
	}
	policyMigration := Migrations()[15]
	if policyMigration.ID != 16 || policyMigration.Name != "0016_policy_governance" {
		t.Errorf("the policy migration is %d/%q, want 16/0016_policy_governance", policyMigration.ID, policyMigration.Name)
	}

	s := openTemp(t)
	ctx := context.Background()
	addRow(t, s, "a", "all-agents", "", "deny", "operator", `{}`, "")
	if _, err := s.PolicyListAll(ctx); err != nil {
		t.Fatal(err)
	}
	if after := len(Migrations()); after != before {
		t.Errorf("the ledger moved from %d to %d entries — this task adds NO migration", before, after)
	}
}

// ── T7: policy_acceptance — the SQL tested as SQL ───────────────────────────

// The instants the acceptance tests record at. Fixed, distinct, and carrying
// sub-second tails so a rendering that flattened nanoseconds fails the
// first-instant assertions too.
var (
	acceptSQLT1     = time.Date(2026, 10, 19, 11, 0, 0, 123456789, time.UTC)
	acceptSQLT2     = time.Date(2026, 10, 19, 12, 30, 0, 987654321, time.UTC)
	acceptSQLT1Text = "2026-10-19T11:00:00.123456789Z"
	acceptSQLT2Text = "2026-10-19T12:30:00.987654321Z"
)

// acceptanceRow reads one acceptance row straight from the table — the row as
// stored, not the shape the method hands back, because "keeps the FIRST
// instant" is a claim about the bytes on disk.
func acceptanceRow(t *testing.T, s *Store, policyID, kind, targetID string) (acceptedBy, acceptedAt string, n int) {
	t.Helper()
	err := s.r.QueryRow(`SELECT accepted_by, accepted_at FROM policy_acceptance
		WHERE policy_id = ? AND target_kind = ? AND target_id = ?`, policyID, kind, targetID).
		Scan(&acceptedBy, &acceptedAt)
	if err == sql.ErrNoRows {
		return "", "", 0
	}
	if err != nil {
		t.Fatalf("read the acceptance row: %v", err)
	}
	if err := s.r.QueryRow(`SELECT COUNT(*) FROM policy_acceptance
		WHERE policy_id = ? AND target_kind = ? AND target_id = ?`, policyID, kind, targetID).Scan(&n); err != nil {
		t.Fatalf("count the acceptance rows: %v", err)
	}
	return acceptedBy, acceptedAt, n
}

func TestPolicyAcceptanceRecordIsIdempotentAndKeepsTheFirstInstant(t *testing.T) {
	// THE idempotence claim, at the SQL layer: ON CONFLICT (policy_id,
	// target_kind, target_id) DO NOTHING makes a re-record a no-op that is
	// NOT an error and that moves NOTHING — the first row stands whole,
	// instant and accepted_by included. An upsert here would let a repeated
	// accept rewrite "since when", and T10's disagreement record would
	// inherit the moved date and accuse the wrong instant.
	s := openTemp(t)
	ctx := context.Background()
	addRow(t, s, "no-bash", "agent", "boss", "deny", "operator", `{}`, "")

	if err := s.PolicyAcceptanceRecord(ctx, "no-bash", "agent", "boss", "operator", acceptSQLT1); err != nil {
		t.Fatalf("first record: %v", err)
	}
	if err := s.PolicyAcceptanceRecord(ctx, "no-bash", "agent", "boss", "someone-else", acceptSQLT2); err != nil {
		t.Fatalf("the re-record was an error (%v); a re-record must be a no-op", err)
	}

	by, at, n := acceptanceRow(t, s, "no-bash", "agent", "boss")
	if n != 1 {
		t.Fatalf("%d rows for one (policy, target); the conflict clause is not doing its job", n)
	}
	if at != acceptSQLT1Text {
		t.Errorf("stored accepted_at %q, want the FIRST instant %q", at, acceptSQLT1Text)
	}
	if by != "operator" {
		t.Errorf("stored accepted_by %q; DO NOTHING keeps the first row whole — %q is a rewritten audit", by, by)
	}

	rows, err := s.PolicyAcceptanceListFor(ctx, "agent", "boss")
	if err != nil {
		t.Fatalf("PolicyAcceptanceListFor: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("the method saw %d rows: %+v", len(rows), rows)
	}
	got := rows[0]
	if got.PolicyID != "no-bash" || got.TargetKind != "agent" || got.TargetID != "boss" ||
		got.AcceptedBy != "operator" || got.AcceptedAt != acceptSQLT1Text {
		t.Errorf("the row came back as %+v", got)
	}
}

func TestPolicyAcceptanceIsRecordedPerTarget(t *testing.T) {
	// The PRIMARY KEY is the whole design: (policy_id, target_kind,
	// target_id). One policy known to two targets is two rows, and each is
	// queryable alone — "was loop:l told?" cannot be silenced by agent:a's
	// row, and vice versa.
	s := openTemp(t)
	ctx := context.Background()
	addRow(t, s, "dual", "agent", "a", "deny", "operator", `{}`, "")

	if err := s.PolicyAcceptanceRecord(ctx, "dual", "agent", "a", "a", acceptSQLT1); err != nil {
		t.Fatal(err)
	}
	if err := s.PolicyAcceptanceRecord(ctx, "dual", "loop", "l", "boss", acceptSQLT2); err != nil {
		t.Fatal(err)
	}

	var total int
	if err := s.r.QueryRow(`SELECT COUNT(*) FROM policy_acceptance`).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Fatalf("%d rows in the table, want 2", total)
	}

	accA, err := s.PolicyAcceptanceListFor(ctx, "agent", "a")
	if err != nil {
		t.Fatal(err)
	}
	if len(accA) != 1 || accA[0].TargetKind != "agent" || accA[0].TargetID != "a" ||
		accA[0].AcceptedBy != "a" || accA[0].AcceptedAt != acceptSQLT1Text {
		t.Errorf("the agent:a answer is wrong or contaminated: %+v", accA)
	}
	accL, err := s.PolicyAcceptanceListFor(ctx, "loop", "l")
	if err != nil {
		t.Fatal(err)
	}
	if len(accL) != 1 || accL[0].TargetKind != "loop" || accL[0].TargetID != "l" ||
		accL[0].AcceptedBy != "boss" || accL[0].AcceptedAt != acceptSQLT2Text {
		t.Errorf("the loop:l answer is wrong or contaminated: %+v", accL)
	}

	// And the column set is STILL the five (legal constraint 2) after writes:
	// no sixth field has crept in through this task's INSERT.
	if got := tableColumns(t, s, "policy_acceptance"); len(got) != 5 {
		t.Errorf("policy_acceptance now holds %v — identifier+timestamp columns only, ever", got)
	}
	// The ledger has not moved either: acceptance is SQL over the 0005 table,
	// not a new migration. (29 is the current count: 0016 by ruling R1, 0018
	// by the deferred-findings gate, 0019 by S11's D1/D2, 0020 by S9's
	// stop/continue, 0021 by S31's steer queue, 0026 by the model-management
	// wave, 0027 by S18's M26 provenance, 0028 by S18's Q8 wire flags and O8
	// no-price declaration, 0029 by S18 T3's route_probe probe cache, 0030 by S18 T5's step thinking-drop count — this task added none.)
	if n := len(Migrations()); n != frozenMigrationCount() {
		t.Errorf("Migrations() has %d entries; this task adds no migration (the total is measured, not hardcoded — 45 frozen legacy entries (0044 S29's bug and 0045 S37's widening joined the numbered block on main) plus every additive migrations/*.sql file, pinned by frozenMigrationCount (0026 adds model metadata, "+
			"0027 adds S18's provenance, 0028 adds S18's wire capabilities and no-price, 0029 adds S18 T3's probe cache, 0030 adds S18 T5's thinking-drop count, 0031 adds S19 T6's loop allow_outward, 0032 adds S20 T1's trigger_schedule/trigger_fire, 0033 adds S20 T2's fire inputs and loop provenance, 0034 adds S20 T4's trigger_gap, 0035 and 0036 add S38's layers and records, 0037 and 0038 add S39's runs and message source, 0039 adds S40's automation tables, 0040 adds S22 T1's retrieval_corpus/retrieval_chunk/FTS5 (vec0 on demand — its dimension is data), 0041 adds S24 T1's queue_item plus the component's queue_advance/sizing_mode, 0042 adds S35 T1's proposal table, 0043 adds S26 T1's roadmap_item, 0044 adds S29 T1's bug, 0045 adds S37 T1's agent_import widening: 0016 by "+
			"ruling R1, 0018 by the deferred-findings gate, 0019 by S11's D1/D2, 0020 by S9's stop/continue, "+
			"0021 by S31's steer queue, 0025 by S16's action_intent)", n)
	}
}

func TestAllAgentsAcceptanceStoresEmptyTargetIDNotNull(t *testing.T) {
	// The stated NULL asymmetry: on `policy`, all-agents is NULL; on
	// `policy_acceptance` the key column is NOT NULL and all-agents arrives
	// as the empty string — a primary key has to be a value, not a NULL.
	// The boundary guarantees "" only ever means all-agents.
	s := openTemp(t)
	ctx := context.Background()
	addRow(t, s, "wide", "all-agents", "", "deny", "operator", `{}`, "")

	if err := s.PolicyAcceptanceRecord(ctx, "wide", "all-agents", "", "operator", acceptSQLT1); err != nil {
		t.Fatalf("all-agents acceptance record: %v", err)
	}
	var isNull int
	if err := s.r.QueryRow(`SELECT target_id IS NULL FROM policy_acceptance WHERE policy_id = 'wide'`).
		Scan(&isNull); err != nil {
		t.Fatal(err)
	}
	if isNull != 0 {
		t.Error("all-agents acceptance stored a NULL target_id; the key column is NOT NULL and takes \"\"")
	}
	rows, err := s.PolicyAcceptanceListFor(ctx, "all-agents", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].TargetID != "" || rows[0].AcceptedAt != acceptSQLT1Text {
		t.Errorf("the all-agents key did not round-trip: %+v", rows)
	}
}

func TestPolicyAcceptanceRecordNamesMissingFieldsAndEnforcesTheKey(t *testing.T) {
	// Fail-fast above the statement (Learnings 2026-08-03): each incomplete
	// record arrives as an error naming the missing field — not a NOT NULL
	// pass-through of a blank, not a bare constraint failure. And the two
	// constraints that are NOT preconditions fire at the database: the named
	// conflict target leaves every other failure — the foreign key to
	// `policy` above all — as an error, because an acceptance of a policy
	// that does not exist is a record of nothing.
	s := openTemp(t)
	ctx := context.Background()
	addRow(t, s, "real", "agent", "boss", "deny", "operator", `{}`, "")

	cases := []struct {
		name                                       string
		policyID, targetKind, targetID, acceptedBy string
		at                                         time.Time
		want                                       string
	}{
		{"no policy id", "", "agent", "boss", "operator", acceptSQLT1, "policy it accepts"},
		{"no target kind", "real", "", "boss", "operator", acceptSQLT1, "target kind"},
		{"no accepted_by", "real", "agent", "boss", "", acceptSQLT1, "accepted_by"},
		{"no instant", "real", "agent", "boss", "operator", time.Time{}, "no instant"},
		{"the policy does not exist", "no-such-policy", "agent", "boss", "operator", acceptSQLT1, "no-such-policy"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := s.PolicyAcceptanceRecord(ctx, c.policyID, c.targetKind, c.targetID, c.acceptedBy, c.at)
			if err == nil {
				t.Fatalf("the incomplete record was accepted")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("the error does not name %q: %v", c.want, err)
			}
		})
	}
}

func TestPolicyAcceptanceListForIsDeterministicAndReportsAFailedStatement(t *testing.T) {
	// Two claims: the answer has a stable order (policy_id, so a listing of
	// "what X is under" reads the same twice), and every failure surfaces as
	// a wrapped error naming the operation — an empty list from a dead
	// reader would read as "this target was never told", which is the audit
	// lie this method must never tell. A dropped table is the honest way to
	// fire the failure without the Close contract (per T6's notes).
	s := openTemp(t)
	ctx := context.Background()
	for _, id := range []string{"c-rule", "a-rule", "b-rule"} {
		addRow(t, s, id, "agent", "boss", "deny", "operator", `{}`, "")
		if err := s.PolicyAcceptanceRecord(ctx, id, "agent", "boss", "operator", acceptSQLT1); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := s.PolicyAcceptanceListFor(ctx, "agent", "boss")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[0].PolicyID != "a-rule" || rows[1].PolicyID != "b-rule" || rows[2].PolicyID != "c-rule" {
		t.Fatalf("not ordered by policy_id: %+v", rows)
	}

	// A different target sees none of them (the WHERE is a WHERE).
	none, err := s.PolicyAcceptanceListFor(ctx, "loop", "nope")
	if err != nil || len(none) != 0 {
		t.Errorf("another target saw %d rows (%v)", len(none), err)
	}

	if _, err := s.w.Exec(`DROP TABLE policy_acceptance`); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if err := s.PolicyAcceptanceRecord(ctx, "a-rule", "agent", "boss", "operator", acceptSQLT1); err == nil ||
		!strings.Contains(err.Error(), "record acceptance") {
		t.Errorf("PolicyAcceptanceRecord over a dead table: %v", err)
	}
	if _, err := s.PolicyAcceptanceListFor(ctx, "agent", "boss"); err == nil ||
		!strings.Contains(err.Error(), "list acceptances") {
		t.Errorf("PolicyAcceptanceListFor over a dead table: %v", err)
	}
}

func TestTheAcceptancePerTargetReadUsesTheNewIndex(t *testing.T) {
	// DEF-027, measured rather than hoped: PolicyAcceptanceListFor's OWN
	// statement — the listAcceptanceSQL constant the method runs, so what is
	// planned here is what executes — must name idx_policy_acceptance_target,
	// the index 0018 added because the table's PRIMARY KEY leads with policy_id
	// and could not serve a WHERE on the two target columns. The idiom is
	// TestTheTrimAndAuditQueriesUseThePolicyEventIndex's, control included: a
	// WHERE on accepted_by — a column in NO index — must NOT name it, so the
	// assertion measures the planner rather than trusting one that answers
	// "index" for everything.
	s := openTemp(t)
	ctx := context.Background()
	for _, id := range []string{"a-rule", "b-rule", "c-rule"} {
		addRow(t, s, id, "agent", "boss", "deny", "operator", `{}`, "")
		if err := s.PolicyAcceptanceRecord(ctx, id, "agent", "boss", "operator", acceptSQLT1); err != nil {
			t.Fatal(err)
		}
	}
	addRow(t, s, "other", "lane-type", "review", "deny", "operator", `{}`, "")
	if err := s.PolicyAcceptanceRecord(ctx, "other", "lane-type", "review", "operator", acceptSQLT2); err != nil {
		t.Fatal(err)
	}

	// The method still ANSWERS correctly while the planner is being interrogated
	// (an index that silently filtered wrong would be the worse failure).
	rows, err := s.PolicyAcceptanceListFor(ctx, "agent", "boss")
	if err != nil || len(rows) != 3 {
		t.Fatalf("the per-target read returned %d rows (%v); the plant was 3 for agent:boss", len(rows), err)
	}

	named := 0
	for _, d := range eqpDetails(t, s, listAcceptanceSQL, "agent", "boss") {
		if strings.Contains(d, "idx_policy_acceptance_target") {
			named++
		}
	}
	if named == 0 {
		t.Errorf("the per-target acceptance read does not name idx_policy_acceptance_target in its plan: %v",
			eqpDetails(t, s, listAcceptanceSQL, "agent", "boss"))
	}

	// The discriminating control.
	control := eqpDetails(t, s, `SELECT COUNT(*) FROM policy_acceptance WHERE accepted_by = ?`, "operator")
	for _, d := range control {
		if strings.Contains(d, "idx_policy_acceptance_target") {
			t.Errorf("the control query named the index (plan %v) — the assertion above cannot discriminate", control)
		}
	}

	// Both indexes exist exactly once in the catalogue: the new one (0018
	// added it), and the policy lookup (0018's rebuild dropped it with the
	// parent and renamed it back — the fingerprint test compares shapes; this
	// asserts existence and singleness after that swap).
	for _, idx := range []string{"idx_policy_target", "idx_policy_acceptance_target"} {
		var n int
		if err := s.r.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='index' AND name = ?`, idx).
			Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("%s exists %d times in the catalogue, want 1", idx, n)
		}
	}
}

// ── T8: policy_event — the audit trail's SQL tested as SQL ───────────────────

// The instants the event tests write at. Fixed (D5), distinct, sub-second
// tails — and eventSQLT0 has ZERO nanoseconds on purpose: the stored bytes
// must show the FORCED ".000000000" (the fixed-width property the trim's
// string comparison stands on; RFC3339Nano would drop the fraction entirely).
var (
	eventSQLT0     = time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC)
	eventSQLT1     = time.Date(2026, 10, 19, 11, 0, 0, 123456789, time.UTC)
	eventSQLT2     = time.Date(2026, 10, 19, 12, 30, 0, 987654321, time.UTC)
	eventSQLT0Text = "2026-10-19T09:00:00.000000000Z"
	eventSQLT1Text = "2026-10-19T11:00:00.123456789Z"
	eventSQLT2Text = "2026-10-19T12:30:00.987654321Z"
)

// eventText reads one event row's stored bytes straight off the table —
// "the row as stored" again (the acceptance file's discipline): the claims
// here are about what lands on disk, not about what the query method renders.
func eventText(t *testing.T, s *Store, id int64) (policyID string, isNull bool, kind, subjectID, outcome, detail, at string) {
	t.Helper()
	var psql sql.NullString
	err := s.r.QueryRow(`SELECT policy_id, subject_kind, subject_id, outcome, detail, at
		FROM policy_event WHERE id = ?`, id).Scan(&psql, &kind, &subjectID, &outcome, &detail, &at)
	if err != nil {
		t.Fatalf("read event row %d: %v", id, err)
	}
	return psql.String, !psql.Valid, kind, subjectID, outcome, detail, at
}

// eqpDetails runs EXPLAIN QUERY PLAN and returns the detail lines — the same
// output the sqlite3 shell printed when the trim/stats shapes were measured.
func eqpDetails(t *testing.T, s *Store, query string, args ...any) []string {
	t.Helper()
	rows, err := s.r.Query("EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatalf("EXPLAIN QUERY PLAN: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatalf("scan EQP row: %v", err)
		}
		out = append(out, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("EQP rows: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("EQP produced no rows — this assertion could not fire")
	}
	return out
}

func TestTheTrimAndAuditQueriesUseThePolicyEventIndex(t *testing.T) {
	// The spec's assertion, measured rather than hoped: BOTH the age trim and
	// the audit stats run against idx_policy_event_policy — asserted against
	// the exact constants the production methods execute (they run
	// trimEventsSQL / statsEventsSQL, so what is planned here is what runs).
	// The trim's rowid-IN shape is why the first holds; do not "simplify" it.
	s := openTemp(t)
	ctx := context.Background()
	addRow(t, s, "no-bash", "agent", "boss", "deny", "operator", `{}`, "")
	if err := s.PolicyEventInsert(ctx, "refusal", "agent:boss", "no-bash", "denied", eventSQLT1); err != nil {
		t.Fatal(err)
	}

	trimmed := 0
	for _, d := range eqpDetails(t, s, trimEventsSQL, eventSQLT1Text) {
		if strings.Contains(d, "idx_policy_event_policy") {
			trimmed++
		}
	}
	if trimmed == 0 {
		t.Errorf("the trim does not name idx_policy_event_policy in its plan: %v", eqpDetails(t, s, trimEventsSQL, eventSQLT1Text))
	}

	statsIdx := false
	for _, d := range eqpDetails(t, s, statsEventsSQL) {
		if strings.Contains(d, "idx_policy_event_policy") {
			statsIdx = true
		}
	}
	if !statsIdx {
		t.Errorf("the audit stats do not name idx_policy_event_policy in their plan: %v", eqpDetails(t, s, statsEventsSQL))
	}

	// The constants still hold the MEASURED shapes (a rewrite that "cleans up"
	// the rowid-IN form or drops MIN(at) into a non-indexed position must
	// fail here before it silently degrades a production query):
	if !strings.Contains(trimEventsSQL, "rowid IN (SELECT rowid FROM policy_event WHERE at < ?)") {
		t.Error("trimEventsSQL lost the measured rowid-IN shape — the bare DELETE was measured as a plain table SCAN")
	}
	if !strings.Contains(statsEventsSQL, "MIN(at)") {
		t.Error("statsEventsSQL no longer asks MIN(at), the covering-index column")
	}

	// And the discriminating control: a WHERE on subject_kind — a column in NO
	// index — structurally cannot name idx_policy_event_policy. If this plan
	// ever DOES name it, the assertions above are reading a planner that
	// answers the index question for everything, and this file's index claims
	// are decoration rather than measurement.
	control := eqpDetails(t, s, `SELECT COUNT(*) FROM policy_event WHERE subject_kind = ?`, "agent")
	for _, d := range control {
		if strings.Contains(d, "idx_policy_event_policy") {
			t.Errorf("the control query named the index (plan %v) — the test above cannot discriminate", control)
		}
	}
}

func TestPolicyEventInsertStoresTheRowAsRendered(t *testing.T) {
	// The five stored claims, byte for byte: the subject splits at the FIRST
	// colon into the two NOT NULL columns; "" policy_id becomes NULL (the
	// schema's own "no rule named", and the FK is happy with NULL); detail is
	// stored exactly as sent (the store treats it as opaque text — the
	// sentence's SHAPE is the boundary's business, asserted upstairs); and
	// `at` lands in the FORCED 9-digit fixed width — the zero-nanosecond row
	// proving the padding (RFC3339Nano would have dropped the fraction and
	// broken the trim's ordering), and the offset zone rendering proving the
	// UTC fold.
	s := openTemp(t)
	ctx := context.Background()
	addRow(t, s, "no-bash", "agent", "boss", "deny", "operator", `{}`, "")

	if err := s.PolicyEventInsert(ctx, "refusal", "agent:boss", "no-bash", "bash refused for agent:boss by rule no-bash", eventSQLT0); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := s.PolicyEventInsert(ctx, "refusal", "loop:l", "", "no rule to name; the request was refused anyway", eventSQLT1); err != nil {
		t.Fatalf("insert (no rule named): %v", err)
	}
	inTZ := time.Date(2026, 10, 19, 5, 0, 0, 0, time.FixedZone("x", 2*3600)) // 03:00 UTC
	if err := s.PolicyEventInsert(ctx, "disagreement", "agent:boss", "no-bash", "observed contradicts declared", inTZ); err != nil {
		t.Fatalf("insert (offset zone): %v", err)
	}

	pid, isNull, kind, sid, outcome, detail, at := eventText(t, s, 1)
	if isNull || pid != "no-bash" {
		t.Errorf("row 1 policy_id = %q (NULL=%v), want \"no-bash\" (not NULL)", pid, isNull)
	}
	if kind != "agent" || sid != "boss" || outcome != "refusal" {
		t.Errorf("row 1 split = (%q,%q) outcome %q, want (agent,boss,refusal)", kind, sid, outcome)
	}
	if detail != "bash refused for agent:boss by rule no-bash" {
		t.Errorf("row 1 detail mangled: %q", detail)
	}
	if at != eventSQLT0Text {
		t.Errorf("row 1 stored at %q, want the FORCED fixed width %q (zero nanoseconds must still show 9 digits)", at, eventSQLT0Text)
	}

	_, isNull, _, _, _, _, _ = eventText(t, s, 2)
	if !isNull {
		t.Errorf("row 2 stored policy_id where NULL was sent — \"fits no rule\" and \"rule called x\" must not become two spellings of one state")
	}
	_, _, _, _, _, _, at = eventText(t, s, 3)
	if at != "2026-10-19T03:00:00.000000000Z" {
		t.Errorf("the offset instant stored as %q; the table stores UTC fixed-width (the trim compares strings)", at)
	}

	// AUTOINCREMENT: ids are the rowids, distinct and ascending.
	var ids []int64
	rows, err := s.r.Query(`SELECT id FROM policy_event ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if len(ids) != 3 || ids[0] >= ids[1] || ids[1] >= ids[2] {
		t.Errorf("ids are not ascending: %v", ids)
	}

	// The FK is real: naming a rule that does not exist is refused (an audit
	// row blaming a phantom rule blames nobody), while NULL sails through —
	// proven above with row 2.
	err = s.PolicyEventInsert(ctx, "refusal", "agent:boss", "phantom-rule", "blame game", eventSQLT1)
	if err == nil || !strings.Contains(err.Error(), "record policy event") {
		t.Errorf("the phantom policy_id was %v; want a wrapped FK error", err)
	}

	// The column set is exactly what 0016 declares — seven columns, no eighth
	// (a caller cannot grow the audit table by writing to it).
	if got := tableColumns(t, s, "policy_event"); len(got) != 7 {
		t.Errorf("policy_event holds %v; 0016 declares exactly 7 columns", got)
	}
}

func TestPolicyEventInsertRefusesShapelessRowsNamingTheField(t *testing.T) {
	// Fail-fast above the statement (Learnings 2026-08-03): every shapeless
	// row is refused naming what is missing — blank kind/subject/detail, a
	// subject that will not split, a lost clock (D5). The vocabulary guard
	// (outcome ∈ refusal/disagreement) lives at the boundary above; this
	// layer only refuses the blank that names no column.
	s := openTemp(t)
	ctx := context.Background()
	addRow(t, s, "real", "agent", "boss", "deny", "operator", `{}`, "")

	cases := []struct {
		name                          string
		kind, subject, policy, detail string
		at                            time.Time
		want                          string
	}{
		{"no kind", "", "agent:boss", "real", "d", eventSQLT1, "kind"},
		{"no subject", "refusal", "", "real", "d", eventSQLT1, "subject"},
		{"subject with no colon", "refusal", "agentboss", "real", "d", eventSQLT1, "does not split"},
		{"subject with empty kind", "refusal", ":boss", "real", "d", eventSQLT1, "does not split"},
		{"subject with empty id", "refusal", "agent:", "real", "d", eventSQLT1, "does not split"},
		{"no detail", "refusal", "agent:boss", "real", "", eventSQLT1, "no detail"},
		{"no instant", "refusal", "agent:boss", "real", "d", time.Time{}, "no instant"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := s.PolicyEventInsert(ctx, c.kind, c.subject, c.policy, c.detail, c.at)
			if err == nil {
				t.Fatalf("the shapeless row was accepted")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("the error does not name %q: %v", c.want, err)
			}
		})
	}
	// None of them left a row behind.
	var n int
	if err := s.r.QueryRow(`SELECT COUNT(*) FROM policy_event`).Scan(&n); err != nil || n != 0 {
		t.Errorf("%d rows after %d refusals (err %v)", n, len(cases), err)
	}
}

func TestPolicyEventQueryListsNewestFirstAndRespectsTheLimit(t *testing.T) {
	// The listing contract: ORDER BY at DESC, id DESC is TOTAL (two refusals
	// in the same nanosecond still come back the same way twice), the subject
	// filter sees only its own, "" sees everything, and limit caps the answer
	// — a non-positive limit is a defect and says so rather than answering
	// "no refusals happened".
	s := openTemp(t)
	ctx := context.Background()
	addRow(t, s, "r1", "agent", "boss", "deny", "operator", `{}`, "")

	// agent:boss gets two rows in the SAME nanosecond (ids decide), loop:l one
	// newer than both, and a third agent its own row.
	if err := s.PolicyEventInsert(ctx, "refusal", "agent:boss", "r1", "boss one", eventSQLT1); err != nil {
		t.Fatal(err)
	}
	if err := s.PolicyEventInsert(ctx, "refusal", "agent:boss", "", "boss two", eventSQLT1); err != nil {
		t.Fatal(err)
	}
	if err := s.PolicyEventInsert(ctx, "refusal", "loop:l", "", "loop row", eventSQLT2); err != nil {
		t.Fatal(err)
	}
	if err := s.PolicyEventInsert(ctx, "disagreement", "agent:other", "", "other row", eventSQLT2); err != nil {
		t.Fatal(err)
	}

	own, err := s.PolicyEventQuery(ctx, "agent:boss", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(own) != 2 {
		t.Fatalf("agent:boss saw %d rows: %+v", len(own), own)
	}
	// Same instant → id DESC: "boss two" (the later insert) first.
	if own[0].Detail != "boss two" || own[1].Detail != "boss one" {
		t.Errorf("the id tie-break did not order the same-instant rows: %q then %q", own[0].Detail, own[1].Detail)
	}
	for _, r := range own {
		if r.SubjectKind != "agent" || r.SubjectID != "boss" || r.ID == 0 {
			t.Errorf("contaminated or id-less row: %+v", r)
		}
	}

	all, err := s.PolicyEventQuery(ctx, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 4 {
		t.Fatalf("all-subjects listing: %d rows", len(all))
	}
	// Newest instant first (both T2 rows before either T1 row).
	if all[0].At != eventSQLT2Text || all[1].At != eventSQLT2Text || all[3].At != eventSQLT1Text {
		t.Errorf("not newest-first: %v", []string{all[0].At, all[1].At, all[2].At, all[3].At})
	}

	capped, err := s.PolicyEventQuery(ctx, "", 2)
	if err != nil || len(capped) != 2 {
		t.Errorf("limit 2 returned %d rows (%v)", len(capped), err)
	}

	if _, err := s.PolicyEventQuery(ctx, "", 0); err == nil ||
		!strings.Contains(err.Error(), "not a listing") {
		t.Errorf("limit 0 answered %v; want the defect named", err)
	}
	if _, err := s.PolicyEventQuery(ctx, "nocolon", 5); err == nil ||
		!strings.Contains(err.Error(), "does not split") {
		t.Errorf("an un-splittable subject answered %v; want it named", err)
	}

	// The store-side NULL fold: row 2 had NULL policy_id; it reads as "".
	for _, r := range own {
		if r.Detail == "boss two" && r.PolicyID != "" {
			t.Errorf("NULL policy_id read back as %q", r.PolicyID)
		}
	}
}

func TestPolicyEventTrimCutsAtTheCutoffAndCounts(t *testing.T) {
	// The cap boundary AT the SQL layer, where it is executed: strict `<`
	// means a row exactly AT the cutoff SURVIVES and one nanosecond older is
	// removed (PL11/D15 say "nothing older than the cap"; a `<=` would eat
	// exactly-90-day rows a policy check still needs), the count is what
	// went, and the cutoff itself renders in the same fixed width it compares
	// against. A zero cutoff is refused — a lost clock must not read as a
	// trim that deleted nothing.
	s := openTemp(t)
	ctx := context.Background()
	addRow(t, s, "r", "agent", "boss", "deny", "operator", `{}`, "")

	old1 := eventSQLT0                     // 09:00
	at1 := eventSQLT1                      // 11:00 (== cutoff)
	at2 := eventSQLT1.Add(time.Nanosecond) // cutoff + 1ns
	if err := s.PolicyEventInsert(ctx, "refusal", "agent:boss", "r", "outlived", old1); err != nil {
		t.Fatal(err)
	}
	if err := s.PolicyEventInsert(ctx, "refusal", "agent:boss", "r", "exactly at", at1); err != nil {
		t.Fatal(err)
	}
	if err := s.PolicyEventInsert(ctx, "refusal", "agent:boss", "r", "just inside", at2); err != nil {
		t.Fatal(err)
	}

	if _, err := s.PolicyEventTrim(ctx, time.Time{}); err == nil ||
		!strings.Contains(err.Error(), "no cutoff") {
		t.Errorf("the lost clock was %v; want the error naming it", err)
	}

	n, err := s.PolicyEventTrim(ctx, at1) // cutoff = exactly-at row's instant
	if err != nil {
		t.Fatalf("trim: %v", err)
	}
	if n != 1 {
		t.Errorf("trim removed %d rows, want 1 (only the older one)", n)
	}
	left, err := s.PolicyEventQuery(ctx, "agent:boss", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 2 {
		t.Fatalf("%d rows left: %+v", len(left), left)
	}
	if left[0].Detail != "just inside" || left[1].Detail != "exactly at" {
		t.Errorf("the wrong rows went: %q, %q", left[0].Detail, left[1].Detail)
	}

	// Trimming again with the same cutoff removes nothing (idempotent house-
	// keeping; the daemon runs this on every start).
	n, err = s.PolicyEventTrim(ctx, at1)
	if err != nil || n != 0 {
		t.Errorf("second trim removed %d (%v); want 0", n, err)
	}
}

func TestPolicyEventStatsAnswersDepthAndOldest(t *testing.T) {
	// The D15 audit answer: depth + oldest, read as stored instants. The
	// empty table gives the honest three-way answer (0, zero time, nil) —
	// "no refusals yet" must be tellable apart from "could not read" (P31).
	// And a stored instant that does not parse names the row's text: a depth
	// built on an unreadable date is the audit that lies.
	s := openTemp(t)
	ctx := context.Background()

	depth, oldest, err := s.PolicyEventStats(ctx, "")
	if err != nil || depth != 0 || !oldest.IsZero() {
		t.Errorf("empty table answered (%d, %v, %v); want (0, zero, nil)", depth, oldest, err)
	}

	addRow(t, s, "r", "agent", "boss", "deny", "operator", `{}`, "")
	for _, at := range []time.Time{eventSQLT2, eventSQLT0, eventSQLT1} {
		if err := s.PolicyEventInsert(ctx, "refusal", "agent:boss", "r", "row", at); err != nil {
			t.Fatal(err)
		}
	}
	depth, oldest, err = s.PolicyEventStats(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if depth != 3 {
		t.Errorf("depth %d, want 3", depth)
	}
	if !oldest.Equal(eventSQLT0) {
		t.Errorf("oldest %v, want %v", oldest, eventSQLT0)
	}

	// A neighbour's rows are not this answer's business: the unfiltered
	// numbers above are the WHOLE table, and adding rows for somebody else
	// moves them (that is what "" means) — the filtered half is the next test.
	if err := s.PolicyEventInsert(ctx, "refusal", "agent:other", "r", "row", eventSQLT2); err != nil {
		t.Fatal(err)
	}
	depth, oldest, err = s.PolicyEventStats(ctx, "")
	if err != nil || depth != 4 || !oldest.Equal(eventSQLT0) {
		t.Errorf("unfiltered stats after a second subject: (%d, %v, %v); want (4, %v, nil)", depth, oldest, err, eventSQLT0)
	}

	// A row that did not come through the write boundary (hand-edited): the
	// error names its text rather than answering on top of it. Asserted on
	// BOTH statements — the filtered path parses the same column.
	if _, err := s.w.Exec(`UPDATE policy_event SET at = 'yesterday-ish'`); err != nil {
		t.Fatal(err)
	}
	_, _, err = s.PolicyEventStats(ctx, "")
	if err == nil || !strings.Contains(err.Error(), "yesterday-ish") {
		t.Errorf("the unreadable instant was %v; want the error naming its text", err)
	}
	_, _, err = s.PolicyEventStats(ctx, "agent:boss")
	if err == nil || !strings.Contains(err.Error(), "yesterday-ish") {
		t.Errorf("the filtered unreadable instant was %v; want the error naming its text", err)
	}
}

// The EQP text measured on 2026-10-21 against this exact schema (Go 1.26.6,
// mattn/go-sqlite3 through the framework's pool) — the two statements these
// methods actually execute, planned verbatim:
//
//	EXPLAIN QUERY PLAN SELECT COUNT(*), MIN(at) FROM policy_event
//	  → SCAN policy_event USING COVERING INDEX idx_policy_event_policy
//
//	EXPLAIN QUERY PLAN SELECT COUNT(*), MIN(at) FROM policy_event
//	                                   WHERE subject_kind = ? AND subject_id = ?
//	  → SCAN policy_event
//
// And the control that makes the second line a measurement rather than a
// hope — the SAME aggregate shape, with the WHERE on the indexed column:
//
//	EXPLAIN QUERY PLAN SELECT COUNT(*), MIN(at) FROM policy_event WHERE policy_id = ?
//	  → SEARCH policy_event USING COVERING INDEX idx_policy_event_policy (policy_id=?)
//
// So the planner does reach for idx_policy_event_policy over COUNT(*)/MIN(at)
// when a predicate constrains an indexed column; the filtered stats statement
// does not, because subject_kind/subject_id are in no index.
func TestTheAuditStatsPlansMatchWhatWasMeasured(t *testing.T) {
	// F-13 grew the audit stats into TWO statements, and this file's rule is
	// that a plan is asserted as measured or not at all: the unfiltered one
	// still runs on the covering index (the constant is byte-identical to T8's,
	// so its plan is the one T8 measured), and the filtered one runs as a plain
	// table SCAN — asserted positively and negatively, with controls that prove
	// neither assertion can pass vacuously.
	s := openTemp(t)
	ctx := context.Background()
	addRow(t, s, "r", "agent", "boss", "deny", "operator", `{}`, "")
	if err := s.PolicyEventInsert(ctx, "refusal", "agent:boss", "r", "row", eventSQLT1); err != nil {
		t.Fatal(err)
	}

	// 1. The UNFILTERED statement: still the covering index, asserted against
	// the exact constant PolicyEventStats runs when subject is "".
	unfiltered := eqpDetails(t, s, statsEventsSQL)
	covering := 0
	for _, d := range unfiltered {
		if strings.Contains(d, "idx_policy_event_policy") {
			covering++
		}
	}
	if covering == 0 {
		t.Errorf("the unfiltered audit stats no longer name idx_policy_event_policy in their plan: %v — "+
			"statsEventsSQL was supposed to stay byte-identical to the measured statement", unfiltered)
	}

	// 2. The FILTERED statement: a plain table scan, named honestly.
	filtered := eqpDetails(t, s, statsEventsSubjectSQL, "agent", "boss")
	scan := false
	for _, d := range filtered {
		if strings.Contains(d, "SCAN policy_event") {
			scan = true
		}
		if strings.Contains(d, "idx_policy_event_policy") || strings.Contains(d, "USING INDEX") ||
			strings.Contains(d, "COVERING INDEX") {
			t.Errorf("the filtered stats plan names an index (%v) — the claim under test is a plain table "+
				"SCAN, and a new index on policy_event would make this file's honesty about the scan a lie", d)
		}
	}
	if !scan {
		t.Errorf("the filtered stats do not plan a SCAN of policy_event at all: %v", filtered)
	}

	// 3. The discriminating controls. (a) Identical aggregate shape, WHERE on
	// the INDEXED column: the planner must name the index. If this plan ever
	// does not, assertion 2 above is reading a planner that never names the
	// index for anything, and the scan claim is decoration rather than
	// measurement. (This is the same control idiom
	// TestTheTrimAndAuditQueriesUseThePolicyEventIndex carries.)
	control := eqpDetails(t, s, `SELECT COUNT(*), MIN(at) FROM policy_event WHERE policy_id = ?`, "r")
	namesIdx := false
	for _, d := range control {
		if strings.Contains(d, "idx_policy_event_policy") {
			namesIdx = true
		}
	}
	if !namesIdx {
		t.Errorf("the control (a COUNT/MIN over the same table, WHERE on the indexed policy_id) does not "+
			"name idx_policy_event_policy: %v — the filtered-scan assertion above cannot discriminate", control)
	}
	// (b) The reverse: a WHERE on a genuinely unindexed column, in the same
	// statement family, must NOT name it — the shape assertion 2 depends on.
	control2 := eqpDetails(t, s, `SELECT COUNT(*), MIN(at) FROM policy_event WHERE subject_kind = ?`, "agent")
	for _, d := range control2 {
		if strings.Contains(d, "idx_policy_event_policy") {
			t.Errorf("the unindexed-column control named the index (plan %v) — the filtered-stats assertion "+
				"cannot discriminate", control2)
		}
	}

	// 4. The two constants are still TWO statements: the filter is present in
	// one and provably absent from the other, so a "cleanup" that merged them
	// (dropping the WHERE, or adding it to the covering-index case) fails here
	// before it silently changes what production runs.
	if !strings.Contains(statsEventsSubjectSQL, "WHERE subject_kind = ? AND subject_id = ?") {
		t.Error("statsEventsSubjectSQL lost its measured WHERE — the filter is the whole point of the constant")
	}
	if strings.Contains(statsEventsSQL, "WHERE") {
		t.Error("statsEventsSQL grew a WHERE — it must stay the byte-identical covering-index statement")
	}
	// And no index was added to serve the scan (the 90-day cap is the argument,
	// the schema is the evidence): exactly one index on policy_event, the one
	// 0005 created.
	var n int
	if err := s.r.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='index' AND tbl_name='policy_event' ` +
		`AND name NOT LIKE 'sqlite_%'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("policy_event carries %d index(es), want exactly 1 (idx_policy_event_policy): a second one "+
			"is a migration this slice forbids, and the scan is bounded by policy.PolicyEventAgeCap", n)
	}
}

func TestPolicyEventStatsFiltersBySubject(t *testing.T) {
	// F-13, at the SQL layer: the depth and the oldest are the LISTING's. Three
	// subjects whose counts and instants all differ, so a number leaking across
	// the filter has nowhere to hide — agent:bob has three rows (oldest T0),
	// agent:ann one row (T2), loop:bob one row at T1 (same id as bob, a
	// different KIND: an answer that filtered on subject_id alone would return
	// bob's rows here). "" still means the whole table, and a subject nobody
	// refused answers (0, zero time, nil) — the honest empty, never an error and
	// never a neighbour's count.
	s := openTemp(t)
	ctx := context.Background()
	addRow(t, s, "r", "agent", "boss", "deny", "operator", `{}`, "")

	seed := []struct {
		subject string
		at      time.Time
	}{
		{"agent:bob", eventSQLT0},
		{"agent:bob", eventSQLT1},
		{"agent:bob", eventSQLT2},
		{"agent:ann", eventSQLT2},
		{"loop:bob", eventSQLT1},
	}
	for _, row := range seed {
		if err := s.PolicyEventInsert(ctx, "refusal", row.subject, "r", "row for "+row.subject, row.at); err != nil {
			t.Fatalf("seed %s: %v", row.subject, err)
		}
	}

	for _, c := range []struct {
		name       string
		subject    string
		wantDepth  int
		wantOldest time.Time
	}{
		{"three rows of one subject", "agent:bob", 3, eventSQLT0},
		{"a single row", "agent:ann", 1, eventSQLT2},
		{"the same id under the other kind", "loop:bob", 1, eventSQLT1},
		{"nobody was ever refused (agent)", "agent:nobody", 0, time.Time{}},
		{"nobody was ever refused (loop)", "loop:nobody", 0, time.Time{}},
		{"no subject means the whole table", "", 5, eventSQLT0},
	} {
		t.Run(c.name, func(t *testing.T) {
			depth, oldest, err := s.PolicyEventStats(ctx, c.subject)
			if err != nil {
				t.Fatalf("stats(%q): %v", c.subject, err)
			}
			if depth != c.wantDepth {
				t.Errorf("stats(%q) depth %d, want %d", c.subject, depth, c.wantDepth)
			}
			if c.wantOldest.IsZero() {
				if !oldest.IsZero() {
					t.Errorf("stats(%q) oldest %v, want the zero instant (the honest empty)", c.subject, oldest)
				}
				return
			}
			if !oldest.Equal(c.wantOldest) {
				t.Errorf("stats(%q) oldest %v, want %v", c.subject, oldest, c.wantOldest)
			}
		})
	}

	// The empty answer is NOT an error (a filtered-empty listing is a real
	// answer — F-14's whole complaint was that it printed somebody else's
	// number instead), and a half-subject is refused NAMING the subject, on the
	// same three shapes the listing's read refuses.
	if _, _, err := s.PolicyEventStats(ctx, "agent:nobody"); err != nil {
		t.Errorf("a subject with no rows was answered as a failure: %v", err)
	}
	for _, bad := range []string{"agent", "agent:", ":x", ":"} {
		_, _, err := s.PolicyEventStats(ctx, bad)
		// The %q rendering of the offending subject must appear in the message
		// — a refusal that says "the subject is bad" without the subject is the
		// un-actionable refusal the slice's own rule against.
		if err == nil || !strings.Contains(err.Error(), "does not split") ||
			!strings.Contains(err.Error(), `"`+bad+`"`) {
			t.Errorf("stats(%q) = %v; want the refusal naming the subject and the split it failed", bad, err)
		}
	}

	// ONE Cut at the FIRST colon — the same split PolicyEventInsert and
	// PolicyEventQuery make, so a colon inside an id is not a separator: the row
	// written as "agent:bob:extra" is readable only as ("agent","bob:extra").
	if err := s.PolicyEventInsert(ctx, "refusal", "agent:bob:extra", "r", "colon in the id", eventSQLT0); err != nil {
		t.Fatal(err)
	}
	depth, oldest, err := s.PolicyEventStats(ctx, "agent:bob:extra")
	if err != nil || depth != 1 || !oldest.Equal(eventSQLT0) {
		t.Errorf("colon-in-id subject answered (%d, %v, %v); want (1, %v, nil)", depth, oldest, err, eventSQLT0)
	}
	if depth, _, err := s.PolicyEventStats(ctx, "agent:bob"); err != nil || depth != 3 {
		t.Errorf("the colon-in-id row leaked into agent:bob: depth %d (%v), want 3", depth, err)
	}
}

func TestPolicyEventMethodsReportAFailedStatement(t *testing.T) {
	// Every failure surfaces as a wrapped error naming the operation — the
	// audit's own "never answer zero for can't-read" discipline (a Stats that
	// returned (0, nil) on a dead reader would certify an empty audit). A
	// dropped table is the honest way to fire this (T6's note).
	s := openTemp(t)
	ctx := context.Background()
	addRow(t, s, "r", "agent", "boss", "deny", "operator", `{}`, "")
	if err := s.PolicyEventInsert(ctx, "refusal", "agent:boss", "r", "before the drop", eventSQLT1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.w.Exec(`DROP TABLE policy_event`); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if err := s.PolicyEventInsert(ctx, "refusal", "agent:boss", "r", "after", eventSQLT1); err == nil ||
		!strings.Contains(err.Error(), "record policy event") {
		t.Errorf("insert over a dead table: %v", err)
	}
	if _, err := s.PolicyEventQuery(ctx, "", 5); err == nil ||
		!strings.Contains(err.Error(), "query policy events") {
		t.Errorf("query over a dead table: %v", err)
	}
	if _, err := s.PolicyEventTrim(ctx, eventSQLT1); err == nil ||
		!strings.Contains(err.Error(), "trim policy events") {
		t.Errorf("trim over a dead table: %v", err)
	}
	if _, _, err := s.PolicyEventStats(ctx, ""); err == nil ||
		!strings.Contains(err.Error(), "policy event stats") {
		t.Errorf("stats over a dead table: %v", err)
	}
	// The filtered statement is a SEPARATE constant, so it is failed
	// separately — and its error names the subject it was asked for.
	if _, _, err := s.PolicyEventStats(ctx, "agent:boss"); err == nil ||
		!strings.Contains(err.Error(), "policy event stats (agent:boss)") {
		t.Errorf("filtered stats over a dead table: %v", err)
	}
	// And the ledger still says 0016 is the only migration this slice owns —
	// plus 0018, which arrived at the deferred-findings gate, not from T8,
	// 0019, which arrived with S11 (ask in the action CHECK, loop.attended
	// for the pin), 0020, S9's stop/continue, and 0021, S31's steer queue,
	// 0026, the model-management wave, 0027, S18's provenance, and 0028,
	// S18's Q8 wire flags plus O8's no-price declaration, plus 0029, S18 T3's probe cache —
	// none of them from this task.
	if n := len(Migrations()); n != frozenMigrationCount() {
		t.Errorf("Migrations() has %d entries; T8 adds no migration (the total is measured, not hardcoded — 45 frozen legacy entries (0044 S29's bug and 0045 S37's widening joined the numbered block on main) plus every additive migrations/*.sql file, pinned by frozenMigrationCount (0026 adds model metadata, "+
			"0027 adds S18's provenance, 0028 adds S18's wire capabilities and no-price, 0029 adds S18 T3's probe cache, 0030 adds S18 T5's thinking-drop count, 0031 adds S19 T6's loop allow_outward, 0032 adds S20 T1's trigger_schedule/trigger_fire, 0033 adds S20 T2's fire inputs and loop provenance, 0034 adds S20 T4's trigger_gap, 0035 and 0036 add S38's layers and records, 0037 and 0038 add S39's runs and message source, 0039 adds S40's automation tables, 0040 adds S22 T1's retrieval index, 0041 adds S24 T1's loop queue, 0042 adds S35 T1's proposal table, 0043 adds S26 T1's roadmap_item, 0044 adds S29 T1's bug, 0045 adds S37 T1's agent_import widening): 0016 by "+
			"ruling R1, 0018 by the deferred-findings gate, 0019 by S11's D1/D2, 0020 by S9's stop/continue, "+
			"0021 by S31's steer queue, 0025 by S16's action_intent)", n)
	}
}
