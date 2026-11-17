package store

import (
	"strings"
	"testing"
)

func TestCreateProposalRoundTrips(t *testing.T) {
	s := openTemp(t)
	p, err := s.CreateProposal("mcp_server_add", `{"name":"github"}`, Actor{Kind: "user", ID: "cjlapao"}, "2026-10-29T10:00:00Z")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	rows, err := s.ListProposals("")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	got := rows[0]
	if got.ID != p.ID || got.Status != "proposed" || got.CreatedAt != "2026-10-29T10:00:00Z" ||
		got.CreatedByKind != "user" || got.CreatedByID != "cjlapao" {
		t.Fatalf("round trip mismatch: %+v", got)
	}
	if got.DecidedAt != "" || got.DecidedByKind != "" || got.DecidedByID != "" || got.DecideReason != "" || got.AppliedError != "" {
		t.Fatalf("a fresh proposal must have every decided_* field empty, got %+v", got)
	}
}

func TestDecideProposalGuardedFromProposedOnly(t *testing.T) {
	s := openTemp(t)
	p, err := s.CreateProposal("policy_add", `{}`, Actor{Kind: "user", ID: "cjlapao"}, "2026-10-29T10:00:00Z")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.DecideProposal(p.ID, "committed", Actor{Kind: "user", ID: "cjlapao"}, "", "2026-10-29T10:01:00Z"); err != nil {
		t.Fatalf("first decide: %v", err)
	}
	err = s.DecideProposal(p.ID, "rejected", Actor{Kind: "user", ID: "someone-else"}, "", "2026-10-29T10:02:00Z")
	if err == nil {
		t.Fatal("deciding an already-committed row must fail")
	}
	want := "already committed by cjlapao at 2026-10-29T10:01:00Z"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestDiscardOnlyFromProposed(t *testing.T) {
	s := openTemp(t)
	p, err := s.CreateProposal("policy_remove", `{}`, Actor{Kind: "user", ID: "cjlapao"}, "2026-10-29T10:00:00Z")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.DecideProposal(p.ID, "rejected", Actor{Kind: "user", ID: "cjlapao"}, "no longer needed", "2026-10-29T10:01:00Z"); err != nil {
		t.Fatalf("reject: %v", err)
	}
	err = s.DiscardProposal(p.ID, Actor{Kind: "user", ID: "cjlapao"}, "2026-10-29T10:02:00Z")
	if err == nil {
		t.Fatal("discarding an already-rejected row must fail")
	}
	want := "already rejected by cjlapao at 2026-10-29T10:01:00Z"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestProposalKindCheckViolation(t *testing.T) {
	s := openTemp(t)
	// Bypasses CreateProposal's own Go-level validation entirely, to prove
	// the database itself refuses a bad kind — not merely the Go layer
	// ahead of a missing CHECK.
	_, err := s.w.Exec(`INSERT INTO proposal (kind, payload, status, created_at, created_by_kind, created_by_id)
		VALUES ('policy_edit', '{}', 'proposed', '2026-10-29T10:00:00Z', 'user', 'cjlapao')`)
	if err == nil {
		t.Fatal("a kind outside the closed enum must be un-writable")
	}
	if !strings.Contains(err.Error(), "CHECK") {
		t.Fatalf("expected a CHECK constraint failure, got: %v", err)
	}
}

func TestProposalStatusCheckViolation(t *testing.T) {
	s := openTemp(t)
	_, err := s.w.Exec(`INSERT INTO proposal (kind, payload, status, created_at, created_by_kind, created_by_id)
		VALUES ('policy_add', '{}', 'archived', '2026-10-29T10:00:00Z', 'user', 'cjlapao')`)
	if err == nil {
		t.Fatal("a status outside the closed enum must be un-writable")
	}
	if !strings.Contains(err.Error(), "CHECK") {
		t.Fatalf("expected a CHECK constraint failure, got: %v", err)
	}
}
