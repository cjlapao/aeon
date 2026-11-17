package daemon

// The authoring-audit seam in the production daemon (S45 T3, PM8). Two
// claims the protocol-package tests CANNOT make: that the store-backed
// table is actually WIRED (a nil seam is a lying read — store-unavailable
// beside a store that holds the audit), and that the adapter translates
// field-for-field against the REAL store (the wire rows are the stored
// rows, the same bytes the CLI will print at T8).

import (
	"context"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

func TestProductionDaemonWiresThePolicyChangeAudit(t *testing.T) {
	// The TestProductionDaemonWiresThePolicyGate argument, carried onto the
	// audit read: `aeon serve` always passes a store, so this assertion IS
	// the production claim; the storeless daemon keeps the nil (the test
	// mode it always was) and the door answers ErrStoreUnavailable.
	s := recoveryStore(t)
	d := New(Options{Store: s})
	if d.Table.PolicyChanges == nil {
		t.Fatal("the store-backed daemon's table has NO authoring-audit read seam — policy.changes " +
			"would answer store-unavailable while the audit table sits in the very same store (PM8)")
	}
	if d0 := New(Options{}); d0.Table.PolicyChanges != nil {
		t.Error("the storeless (test-mode) daemon grew an audit seam; with no store the read must fail " +
			"closed on every call, not dereference")
	}

	ctx := context.Background()
	initConn(t, d, ctx, "c-pc", "audit-door")

	// The empty audit through the real adapter into the real store: the
	// table's own sentence, changes [] and the always-present [] — nothing
	// null, nothing cached.
	res, err := d.Table.Dispatch(ctx, "c-pc", protocol.MethodPolicyChanges, nil)
	if err != nil {
		t.Fatalf("empty audit: %v", err)
	}
	empty, ok := res.(protocol.PolicyChangesResult)
	if !ok {
		t.Fatalf("dispatch gave %T, want protocol.PolicyChangesResult", res)
	}
	if empty.Statement != protocol.PolicyChangesEmptyStatement {
		t.Errorf("fresh store statement = %q, want the empty-table sentence", empty.Statement)
	}
	if empty.Changes == nil || empty.UnauditedPolicyIDs == nil {
		t.Errorf("fresh store returned nulls: changes=%v unaudited=%v", empty.Changes, empty.UnauditedPolicyIDs)
	}

	// Plant through the store's OWN writer — the door has none, and the
	// seam's interface has none either (PM8) — and read again on the same
	// table: live, translated field for field, newest-first.
	proposal := int64(7)
	if err := s.PolicyChangeInsert(ctx, store.PolicyChange{
		Op: "add", PolicyID: "grant-bash",
		Summary: `added policy "grant-bash"`, ActorKind: "user", ActorID: "1000",
		OccurredAt: "2026-10-31T10:00:00Z",
	}); err != nil {
		t.Fatalf("plant audit row: %v", err)
	}
	if err := s.PolicyChangeInsert(ctx, store.PolicyChange{
		Op: "commit", Summary: "committed proposal 7", ProposalID: &proposal,
		ActorKind: "user", ActorID: "1000", OccurredAt: "2026-10-31T10:05:00Z",
	}); err != nil {
		t.Fatalf("plant commit row: %v", err)
	}

	res, err = d.Table.Dispatch(ctx, "c-pc", protocol.MethodPolicyChanges, nil)
	if err != nil {
		t.Fatalf("populated audit: %v", err)
	}
	full, ok := res.(protocol.PolicyChangesResult)
	if !ok {
		t.Fatalf("dispatch gave %T, want protocol.PolicyChangesResult", res)
	}
	if full.Statement != "" {
		t.Errorf("the populated audit still printed the empty-table sentence %q", full.Statement)
	}
	if len(full.Changes) != 2 || full.Changes[0].Op != "commit" || full.Changes[1].Op != "add" {
		t.Fatalf("newest-first through the adapter broken: %+v", full.Changes)
	}
	if full.Changes[1].PolicyID != "grant-bash" || full.Changes[1].ActorKind != "user" ||
		full.Changes[1].ActorID != "1000" || full.Changes[1].OccurredAt != "2026-10-31T10:00:00Z" {
		t.Errorf("the add row did not translate field-for-field: %+v", full.Changes[1])
	}
	if full.Changes[0].ProposalID == nil || *full.Changes[0].ProposalID != 7 {
		t.Errorf("the commit row lost its proposal id through the adapter: %+v", full.Changes[0])
	}
	if full.Changes[0].PolicyID != "" {
		t.Errorf("the commit row grew a policy id the table never held: %q", full.Changes[0].PolicyID)
	}

	// The filter travels through the adapter too.
	res, err = d.Table.Dispatch(ctx, "c-pc", protocol.MethodPolicyChanges,
		mustJSONT8(t, protocol.PolicyChangesParams{PolicyID: "grant-bash"}))
	if err != nil {
		t.Fatalf("filtered audit: %v", err)
	}
	filtered, ok := res.(protocol.PolicyChangesResult)
	if !ok {
		t.Fatalf("dispatch gave %T, want protocol.PolicyChangesResult", res)
	}
	if len(filtered.Changes) != 1 || filtered.Changes[0].Op != "add" {
		t.Fatalf("the filter did not travel through the adapter: %+v", filtered.Changes)
	}
	// The filtered-empty beside a populated table: NO statement (the
	// table's sentence belongs to the table-empty answer alone).
	res, err = d.Table.Dispatch(ctx, "c-pc", protocol.MethodPolicyChanges,
		mustJSONT8(t, protocol.PolicyChangesParams{PolicyID: "never-authored"}))
	if err != nil {
		t.Fatalf("filtered-empty audit: %v", err)
	}
	if none, ok := res.(protocol.PolicyChangesResult); !ok || none.Statement != "" || len(none.Changes) != 0 {
		t.Fatalf("the filtered-empty answer is not honest through the adapter: %+v", res)
	}
}
