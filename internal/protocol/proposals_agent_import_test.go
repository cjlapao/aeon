package protocol

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// S37 T4 — the agent_import create half. The unit tests below use a fake
// seam (so the test does not import internal/plugin) and the S35
// fakeProposalStore (proposals_test.go), and prove the create half's
// properties: the bare path is stored, a refusal leaves no row, the preview
// comes from the seam, and the seam is the ONLY thing that decides.

// agentImportTable builds a Table with a fake ProposalStore + a fake
// AgentImportPreview seam. The seam spy counts calls (so the relative-path
// test can prove the seam was never reached).
func agentImportTable(store *fakeProposalStore, seam func(ctx context.Context, pkgPath string) (*AgentImportPreview, string, error), calls *int) *Table {
	tbl := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	tbl.Proposals = store
	tbl.ActorFor = func(context.Context, *Session) Actor { return Actor{Kind: "user", ID: "1000"} }
	tbl.AgentImportPreview = func(ctx context.Context, pkgPath string) (*AgentImportPreview, string, error) {
		if calls != nil {
			*calls++
		}
		if seam != nil {
			return seam(ctx, pkgPath)
		}
		return &AgentImportPreview{Agent: "fake", Consent: "fake consent", CarriedDeny: []AgentImportCarried{}}, "", nil
	}
	return tbl
}

// TestAgentImportCreateStoresTheBarePathAndReturnsPreview is the core
// (criterion 1): the row payload is the bare absolute path exactly, and the
// result carries the preview from the seam.
func TestAgentImportCreateStoresTheBarePathAndReturnsPreview(t *testing.T) {
	store := &fakeProposalStore{}
	tbl := agentImportTable(store, nil, nil)
	res, err := callProposal(t, tbl, MethodProposalCreate, proposalCreateParams{Kind: "agent_import", Payload: json.RawMessage(`{"pkgPath":"/home/operator/agents/reviewer-plus"}`)})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	row, ok := res.(ProposalRow)
	if !ok {
		t.Fatalf("the result must be a ProposalRow, got %T", res)
	}
	// The stored payload is the bare absolute path, exactly (AA2 — no snapshot).
	if string(row.Payload) != `{"pkgPath":"/home/operator/agents/reviewer-plus"}` {
		t.Fatalf("the payload must be the bare path, got %s", row.Payload)
	}
	// The result carries the preview from the seam.
	if row.Preview == nil || row.Preview.Agent != "fake" {
		t.Fatalf("the result must carry the preview from the seam, got %+v", row.Preview)
	}
}

// TestAgentImportRelativePathRefusedBeforeTheSeamIsCalled pins AD2: a
// relative path is refused by the payload decode, before the seam is ever
// called (the seam spy counts zero calls), and leaves no row.
func TestAgentImportRelativePathRefusedBeforeTheSeamIsCalled(t *testing.T) {
	store := &fakeProposalStore{}
	calls := 0
	tbl := agentImportTable(store, nil, &calls)
	_, err := callProposal(t, tbl, MethodProposalCreate, proposalCreateParams{Kind: "agent_import", Payload: json.RawMessage(`{"pkgPath":"agents/reviewer-plus"}`)})
	if err == nil {
		t.Fatal("a relative path must be refused")
	}
	if calls != 0 {
		t.Fatalf("the seam must not be called for a relative path, got %d calls", calls)
	}
	if store.created != 0 {
		t.Fatalf("a refused create must leave no row, got %d", store.created)
	}
}

// TestAgentImportURLReachesThePluginRefusalUnchanged pins AP10: a URL is
// passed through to the seam unchanged, and the seam's refusal string passes
// back into the refusal's `what` byte-for-byte (the CLI's wording, not a
// wrapper's) — the same shape S35's own proposals_test asserts (errors.As +
// the ErrProposalRefused code, the reason in Data.What).
func TestAgentImportURLReachesThePluginRefusalUnchanged(t *testing.T) {
	store := &fakeProposalStore{}
	tbl := agentImportTable(store, func(context.Context, string) (*AgentImportPreview, string, error) {
		return nil, "refused: path · refused: import takes a local path — nothing is fetched (AP10) · remedy: fetch it yourself if you must, and import the directory you fetched", nil
	}, nil)
	_, err := callProposal(t, tbl, MethodProposalCreate, proposalCreateParams{Kind: "agent_import", Payload: json.RawMessage(`{"pkgPath":"https://example.com/agents/x"}`)})
	if err == nil {
		t.Fatal("a URL must be refused")
	}
	var e *Error
	if !errors.As(err, &e) || e.Code != ErrProposalRefused.Code {
		t.Fatalf("the refusal must be an ErrProposalRefused, got %v", err)
	}
	// The seam's refusal string passes back verbatim, in the `what`.
	if e.Data.What != "refused: path · refused: import takes a local path — nothing is fetched (AP10) · remedy: fetch it yourself if you must, and import the directory you fetched" {
		t.Fatalf("the refusal must pass through verbatim, got %q", e.Data.What)
	}
	if store.created != 0 {
		t.Fatalf("a refused create must leave no row, got %d", store.created)
	}
}

// TestAgentImportPayloadWithExtraFieldsRefused pins AA2: a payload with a
// second field (a card, a snapshot) is refused, and leaves no row.
func TestAgentImportPayloadWithExtraFieldsRefused(t *testing.T) {
	store := &fakeProposalStore{}
	tbl := agentImportTable(store, nil, nil)
	_, err := callProposal(t, tbl, MethodProposalCreate, proposalCreateParams{Kind: "agent_import", Payload: json.RawMessage(`{"pkgPath":"/x","card":{"name":"x"}}`)})
	if err == nil {
		t.Fatal("a payload with an extra field must be refused")
	}
	if store.created != 0 {
		t.Fatalf("a refused create must leave no row, got %d", store.created)
	}
}

// TestAgentImportRefusalLeavesNoRow pins that a seam refusal (the package is
// refused at the plugin layer) leaves no row — an inert proposal is never
// carried to commit (TD3).
func TestAgentImportRefusalLeavesNoRow(t *testing.T) {
	store := &fakeProposalStore{}
	tbl := agentImportTable(store, func(context.Context, string) (*AgentImportPreview, string, error) {
		return nil, "refused: card.json · the card could not be verified", nil
	}, nil)
	_, err := callProposal(t, tbl, MethodProposalCreate, proposalCreateParams{Kind: "agent_import", Payload: json.RawMessage(`{"pkgPath":"/abs/x"}`)})
	if err == nil {
		t.Fatal("a seam refusal must be refused")
	}
	if store.created != 0 {
		t.Fatalf("a refused create must leave no row, got %d", store.created)
	}
}

// TestAgentImportUnwiredSeamAnswersNotWired pins half-wired honesty: a nil
// seam answers ErrNotWired by name, never silently permissive (no row).
func TestAgentImportUnwiredSeamAnswersNotWired(t *testing.T) {
	store := &fakeProposalStore{}
	tbl := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	tbl.Proposals = store
	tbl.ActorFor = func(context.Context, *Session) Actor { return Actor{Kind: "user", ID: "1000"} }
	// AgentImportPreview is nil (not wired).
	_, err := callProposal(t, tbl, MethodProposalCreate, proposalCreateParams{Kind: "agent_import", Payload: json.RawMessage(`{"pkgPath":"/abs/x"}`)})
	if err == nil {
		t.Fatal("a nil seam must answer ErrNotWired, not silently permissive")
	}
	if store.created != 0 {
		t.Fatalf("a not-wired create must leave no row, got %d", store.created)
	}
}

// TestAgentImportCreateWritesOnlyTheProposalTable pins that the create writes
// ONLY the proposal row — the fake store has no plugin/policy tables, so a
// create that touched them would have to fail; it does not (PL15: the create
// does not author policy rows, a refusal stores nothing).
func TestAgentImportCreateWritesOnlyTheProposalTable(t *testing.T) {
	store := &fakeProposalStore{}
	tbl := agentImportTable(store, nil, nil)
	_, err := callProposal(t, tbl, MethodProposalCreate, proposalCreateParams{Kind: "agent_import", Payload: json.RawMessage(`{"pkgPath":"/abs/x"}`)})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if store.created != 1 {
		t.Fatalf("the create must write exactly one row, got %d", store.created)
	}
}
