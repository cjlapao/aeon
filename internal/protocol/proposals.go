package protocol

// proposals.go — S35 T3: `proposal.create`/`proposal.list`/`proposal.discard`
// on the socket. `internal/store` is not banned from this package (unlike
// `internal/mcp` — see absent_test.go's list), so this follows the
// StepStore/QuestionStore local-interface pattern, not S34 T3/T4's func
// seam: ProposalStore is a package-local mirror of T1's real store
// functions, adapted by a small function in internal/daemon (the same split
// stepStore(s *store.Store) protocol.StepStore already uses).
//
// Validation is different: T2's registry lives in cmd/aeon (it calls
// cmd/aeon/policy.go's functions and internal/mcp's Config.Validate, and
// this package must not import either), so it arrives through the
// ValidateProposal func seam on Table — the same shape S34 T3/T4 used for
// MCP. handleProposalCreate calls BOTH: ValidateProposal first, Proposals
// only if that passes (TD3: refused at create, never carried to commit as
// a written row).
//
// Amended 2026-10-30 by S45 PM1 (the operator's ruling; the guard that
// makes it provable is internal/protocol/proposals_test.go's
// TestProposalCommitExistsOnlyAsAHumanDoor, beside the superseded
// TestNoProposalCommitMethodExists, and the door's own statement is
// cmd/aeon/policy_amend_test.go's TestPolicyAuthoringIsAHumanDoor). What
// used to be written here — "deliberately absent from this file, from
// method.go and from every other file in this package: `MethodProposalCommit`
// and `MethodProposalReject`; commit and reject are the human-only CLI door"
// — is no longer the truth. They EXIST on this table now, registered by
// proposals_decide.go: `proposal.get`/`proposal.commit`/`proposal.reject`
// are the socket's CONDITIONAL human door (nil seam ⇒ absent, V13;
// RequiresInitialize; never Debug; the actor stamped from the peer, never a
// param; the audit row beside the write). Read S35 brief §7 for what that
// amendment did NOT remove: a proposal is still inert until a human commits
// it, the decision still reaches the same desk orchestration the CLI
// reached (PM4 — one implementation, two transports), the enforcement gate
// still never learns the word `proposal` (PM11), and the socket is still not
// an agent-reachable surface — the four caller-classes that are not the
// person (the narrow projection, the agent services, the tool registry, this
// catalog) name none of the five doors, by walk. Everything THIS file owns —
// create/list/discard, the inert half — is unchanged, and the names it
// registers are still spelled once, in method.go.

import (
	"context"
	"encoding/json"
	"fmt"
)

// ProposalRow is the wire shape for one proposal — a mirror of
// internal/store.Proposal's fields (flat CreatedByKind/ID, same convention
// Goal/Task/EventTrigger already use on their own wire types), plus Command
// (TD6/TD7's pattern): every row carries the exact CLI command that decides
// it, as copyable text, never a structured action the UI could invoke.
// Payload rides as json.RawMessage — the stored column is JSON text, and a
// caller reading this field wants a nested object, not a double-encoded
// string.
type ProposalRow struct {
	ID            int64           `json:"id"`
	Kind          string          `json:"kind"`
	Payload       json.RawMessage `json:"payload"`
	Status        string          `json:"status"`
	CreatedAt     string          `json:"createdAt"`
	CreatedByKind string          `json:"createdByKind"`
	CreatedByID   string          `json:"createdById"`
	DecidedAt     string          `json:"decidedAt,omitempty"`
	DecidedByKind string          `json:"decidedByKind,omitempty"`
	DecidedByID   string          `json:"decidedById,omitempty"`
	DecideReason  string          `json:"decideReason,omitempty"`
	AppliedError  string          `json:"appliedError,omitempty"`
	Command       string          `json:"command"`
	// Preview is S37 T4's agent_import preview (design §2), populated only for
	// kind agent_import — advisory (AA4): `aeon proposal commit` re-reads and
	// re-renders, so a stale preview is never authoritative. omitempty keeps
	// the other kinds' responses unchanged (the field is nil for them).
	Preview *AgentImportPreview `json:"preview,omitempty"`
}

// ProposalStore is this subject's local interface onto internal/store's
// proposal functions (T1). CreateProposal/DiscardProposal take the actor's
// Kind/ID as plain strings rather than *Session — the handler resolves the
// actor once, through the EXISTING table.ActorFor seam (set in
// internal/daemon's New(), the same uid-from-peer stamp triggers.go's own
// handler already uses), so this interface does not duplicate that
// resolution a second time. A proposal's author is the kernel-reported
// peer, never a claim in params — ActorFor is where that rule lives.
type ProposalStore interface {
	CreateProposal(ctx context.Context, kind string, payload json.RawMessage, byKind, byID string) (ProposalRow, error)
	ListProposals(status string) ([]ProposalRow, error)
	DiscardProposal(ctx context.Context, id int64, byKind, byID string) error
}

// ErrProposalRefused is proposal.create's validation-refusal shape: the
// payload itself is bad (T2's registry said so), with its reason verbatim —
// never carried to commit as a written row (TD3).
var ErrProposalRefused = &Error{
	Code:    -31710,
	Message: "the proposal was refused",
	Data: Data{
		What:  "the payload failed the same validator `aeon proposal commit` would re-run at commit time",
		Means: "nothing was written — a refused proposal leaves no row, never one carrying an error flag",
		Todo:  "the `what` names the exact reason; fix the payload and create again",
		Fatal: false,
	},
}

type proposalCreateParams struct {
	Kind    string          `json:"kind"`
	Payload json.RawMessage `json:"payload"`
}

func commandFor(id int64) string {
	return fmt.Sprintf("aeon proposal commit %d", id)
}

// ProposalCommand is commandFor's exported spelling (S45 T7, PM9): the one
// copyable CLI command that decides a proposal, defined once. The records
// tool's propose twins fill it on their result exactly as proposal.create
// and proposal.list do — a second spelling of the command is a second
// truth about the human door.
func ProposalCommand(id int64) string { return commandFor(id) }

// ProposalListAnswer is proposal.list's answer shape (S45 T7, PM9): every
// row carries THE commit command as copyable text, wrapped in the envelope
// the wire states. Exported so the records tool's proposal_list answers
// through these same lines — one implementation, two doors.
func ProposalListAnswer(rows []ProposalRow) map[string]any {
	out := make([]ProposalRow, len(rows))
	for i, r := range rows {
		r.Command = commandFor(r.ID)
		out[i] = r
	}
	return map[string]any{"proposals": out}
}

// handleProposalCreate validates (T2's seam) before writing (T1's store,
// through the local interface) — in that order, always; see this file's
// header.
func (t *Table) handleProposalCreate(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	if t.Proposals == nil {
		return nil, ErrNotWired.WithWhat("proposals are not wired — `aeon serve` wires Table.Proposals, a hand-built Table does not")
	}
	var p proposalCreateParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if p.Kind == "" {
		return nil, ErrProposalRefused.WithWhat("proposal.create needs a non-empty kind")
	}
	if len(p.Payload) == 0 {
		return nil, ErrProposalRefused.WithWhat("proposal.create needs a non-empty payload")
	}
	if t.ValidateProposal != nil {
		if err := t.ValidateProposal(ctx, p.Kind, string(p.Payload)); err != nil {
			return nil, ErrProposalRefused.WithWhat("%s", err.Error())
		}
	}
	// S37 T4: for kind agent_import the create composes the preview through the
	// seam (never importing internal/plugin) and stores the bare normalised
	// path, not a snapshot (AA2). Do not branch on any other kind.
	storedPayload := p.Payload
	var preview *AgentImportPreview
	if p.Kind == "agent_import" {
		prev, ap, refusal, err := t.validateAgentImport(ctx, p.Payload)
		if err != nil {
			return nil, err
		}
		if refusal != "" {
			return nil, ErrProposalRefused.WithWhat("%s", refusal)
		}
		preview = prev
		if b, e := json.Marshal(agentImportPayload{PkgPath: ap.PkgPath}); e == nil {
			storedPayload = b
		}
	}
	actor := t.ActorFor(ctx, sess)
	row, err := t.Proposals.CreateProposal(ctx, p.Kind, storedPayload, actor.Kind, actor.ID)
	if err != nil {
		return nil, err
	}
	row.Command = commandFor(row.ID)
	row.Preview = preview
	return row, nil
}

type proposalListParams struct {
	Status string `json:"status"`
}

func (t *Table) handleProposalList(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	if t.Proposals == nil {
		return nil, ErrNotWired.WithWhat("proposals are not wired — `aeon serve` wires Table.Proposals, a hand-built Table does not")
	}
	var p proposalListParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	rows, err := t.Proposals.ListProposals(p.Status)
	if err != nil {
		return nil, err
	}
	// S45 T7 (PM9): the mapping/Command/envelope moved to the exported
	// ProposalListAnswer so the records tool's proposal_list speaks these
	// same lines (one definition, two doors).
	return ProposalListAnswer(rows), nil
}

type proposalDiscardParams struct {
	ID int64 `json:"id"`
}

// handleProposalDiscard answers with an ok:false RESULT (never a thrown RPC
// error) when the row was already decided — design.md §2's own worked
// example for this method: a racing decider is an outcome the UI renders
// inline, not an exceptional failure the app has to catch.
func (t *Table) handleProposalDiscard(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	if t.Proposals == nil {
		return nil, ErrNotWired.WithWhat("proposals are not wired — `aeon serve` wires Table.Proposals, a hand-built Table does not")
	}
	var p proposalDiscardParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if p.ID == 0 {
		return nil, ErrProposalRefused.WithWhat("proposal.discard needs a non-zero id")
	}
	actor := t.ActorFor(ctx, sess)
	if err := t.Proposals.DiscardProposal(ctx, p.ID, actor.Kind, actor.ID); err != nil {
		return map[string]any{"ok": false, "error": err.Error()}, nil
	}
	return map[string]any{"id": p.ID, "status": "discarded"}, nil
}
