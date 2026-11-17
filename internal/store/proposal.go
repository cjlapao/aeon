package store

// proposal.go — S35 T1: the one table P34's second child adds. A row here
// constrains nothing (brief §4's guard-blind invariant) until a human
// commits it through the CLI (T5); this file is the store half only —
// DecideProposal has no caller until then (commit/reject are CLI-only,
// brief §7), and T3's socket handlers call Create/List/Discard alone.

import (
	"database/sql"
	"errors"
	"fmt"
)

// ProposalKinds is the closed set migration 0042's CHECK carries, mirrored
// here so a bad kind is a sentence naming the value, not a bare SQL error —
// the same discipline TransitionPluginAgent applies to plugin states.
//
// 'agent_import' (migration 0043, S37 T1) is the sixth kind: the one
// authority-widening action S35's brief named and left open (brief §0). It
// joins the set beside the five S35 staged, never in place of them.
const ProposalKindAgentImport = "agent_import"

// 'autonomy_revoke' and 'autonomy_deny' (migration 20261101180000, S56 T8)
// are S56's two RESTRICTIVE autonomy kinds: the proposals an agent may
// author (revoke a grant, add a veto). There is deliberately no third
// autonomy kind — no proposal shape can create an allow (AL11).
const (
	ProposalKindAutonomyRevoke = "autonomy_revoke"
	ProposalKindAutonomyDeny   = "autonomy_deny"
)

// ProposalKindMCPGrantRevoke is S46 T6's seventh kind (the design revision
// S35 TD1 required): an agent proposing to take a declaration BACK — the
// narrowing direction only. There is deliberately no mcp_grant_declare, no
// mcp_server_...-widen kind: widening is a human act (MG9). The
// mcp_grant_revoke.sql rebuild added it to the CHECK; this const and the
// list below are the Go mirror the lock test pins against the CHECK.
// (Slices count themselves: mine is the seventh kind DESIGNED; with S56's
// two merged before it in file order it ships ninth in the list.)
const ProposalKindMCPGrantRevoke = "mcp_grant_revoke"

// ProposalKindVerifyCommand and ProposalKindMergeRule are S57 T8's two kinds
// (migration 20261102132731, S57 VG17): the proposal shapes an agent may
// author over the gate and the merge rule. Both are restrictive — a
// verify_command proposal may only add a new gate (adding a required or
// advisory gate can only make landing harder, and it executes only under
// the policy gate), and a merge_rule proposal must tighten every field it
// changes (the desk computes the diff and refuses any loosening naming the
// field). There is deliberately no remove/archive/disable kind, no
// no_checks=allow kind, no method-change kind: those widen what a person
// may do, and widening is a human act (VG17, the AL11 sense).
const (
	ProposalKindVerifyCommand = "verify_command"
	ProposalKindMergeRule     = "merge_rule"
)

// ProposalKindBlueprintSave is S48 T9's kind (migration
// 20261103004700): the design revision S35 TD1 names — an agent proposes a
// blueprint (new or changed) as an inert row; a person commits it through
// S45's generic commit, and the effect is S48 T6's ONE write path stamped
// with the committer, never the proposer. The payload shape is the desk's
// (internal/desk/blueprint_kinds.go); the save door's own refusals are the
// commit's refusals (the validator is the write path's own Analyze).
const ProposalKindBlueprintSave = "blueprint_save"

// ProposalKindEnvironmentRestrict is S59 T7's kind (migration
// 20261103181500): the one thing an agent may ask of the execution
// environment — that it be TIGHTENED. The validator is execenv.Restricts
// (EV16): every way the proposed spec is looser than the row's current one
// is named and refused, at create and again at commit (S35 TD3). The apply
// is the plane's own CAS update with the COMMITTER as actor, and the
// payload's baseline revision makes a proposal written against a row that
// has since moved refuse rather than clobber the edit. There is deliberately
// no environment_widen / environment_create / environment_bind /
// secretref_* kind: strength moves by a person alone (EV16, EV19).
const ProposalKindEnvironmentRestrict = "environment_restrict"

// ProposalKindAutomationDisable is S49 T6's kind (migration
// 20261103193000): the one automation write an agent can ever initiate
// (AN9) — an ask that ONE automation be DISABLED: paused for a schedule or
// webhook row, switched off for an event row. The payload names no target
// state and carries no switch to throw ({"source","id","reason"} — the
// desk's shape in internal/desk/automation_kinds.go); the apply is T4's
// own disable/pause path with the COMMITTER as actor, never the proposer.
// A proposal can only reduce what an automation may do: there is
// deliberately no automation_create / automation_update / automation_enable
// / automation_archive kind, and none will be added — a kind that starts,
// widens, switches on, or hides is not a proposal shape in this plane.
const ProposalKindAutomationDisable = "automation_disable"

// ProposalKindRoadmapIdea is S50 T5's kind (migration 20261104101500): the
// one roadmap ask an agent can ever initiate (RM8) — an ask that ONE IDEA be
// ADDED. The payload is the desk's shape in internal/desk/roadmap_kinds.go
// ({"componentRoot","title","epic","note"} — no status, no estimate, no
// actor); the apply is the person's own create path with the COMMITTER as
// actor and one roadmap_change line, never the proposer. A proposal may ask
// that an idea EXIST, never edit, retire, size, rank or start a row: there
// is deliberately no roadmap_update / roadmap_cancel / roadmap_estimate /
// roadmap_priority kind, and none will be added.
const ProposalKindRoadmapIdea = "roadmap_idea"

// ProposalKindLearningRetract is S52 T4's kind, sixteenth in the store's
// order (migration 20261104171000_proposal_learning_retract). The payload is
// ({"id","reason"} — a pointer at a row and the sentence a person reads at
// the desk); the apply is the human door's OWN retract path with the
// COMMITTER as actor — stamp, one learning_change line, vector drop, all
// carrying the person, never the proposer (LM8). A proposal may ask that a
// memory be TAKEN BACK, never restored, edited, or filed for anyone: there
// is deliberately no learning_restore / learning_edit / learning_add kind,
// and none will be added (LM7 — the ask half of the plane; T3's guards
// refuse each such name by word).
const ProposalKindLearningRetract = "learning_retract"

// ProposalKindMergeQueueEnqueue is S60 T9's kind, seventeenth in the store's
// order (migration 20261104193000_proposal_kind_mergequeue). The payload is
// ({"componentRoot","branch","reason"} — a repository, a branch, and the
// sentence a person reads at the desk); the BASE is resolved by the daemon at
// enqueue, never proposed (MQ15). The apply runs the queue's own
// ValidateEnqueue and then Service.Enqueue stamped with the PROPOSAL ITSELF
// ({proposal, <id>} — T2's enqueued_by CHECK admitted 'proposal' for exactly
// this row) and attended = false (MQ14.3): the agent asks that a branch
// JOIN, a person commits, and the queue treats the landed row exactly as the
// socket door's own. A proposal may ask that a branch be QUEUED, never
// landed, retried, reordered, removed for someone else or settings-widened:
// there is deliberately no mergequeue_land / mergequeue_retry /
// mergequeue_reorder / mergequeue_settings kind, and none will be added
// (MQ14/MQ17 — T8's guards refuse each such name by word).
const ProposalKindMergeQueueEnqueue = "mergequeue_enqueue"

// ProposalKindCharterSave is S61 T5's kind, eighteenth in the store's order
// (migration 20261105110000_proposal_kinds_intake). The payload is
// {"rootId","body","reason"} — a root, the GI2 charter body, and the
// sentence a person reads at the desk. The apply is the charter's OWN Save
// with the COMMITTING PERSON as the revision actor, source 'proposal' and
// the proposal's id as source_ref (GI4): the proposer's identity never
// reaches the revision row, and no note rides the save because the charter
// package's law keeps a proposal's reason on the proposal, not in the
// revision's source_ref. A proposal may ask that a charter be SAVED, never
// reverted, deleted or rewritten by its proposer: there is deliberately no
// charter_revert / charter_delete kind (history is append-only), and none
// will be added.
const ProposalKindCharterSave = "charter_save"

// ProposalKindIntakePlan is S61 T5's kind, nineteenth in the store's order
// (same migration). The payload is the GI7 plan — components, epics, goals,
// items and the reason — and NO path, secret or actor: the person chooses
// the target path and the publish choice at commit time, which is exactly
// why the GENERIC commit door refuses this kind with a fixed sentence naming
// intake.commit (GI7/OD4). intake.commit is a dedicated human-door method,
// not a proposal kind — there is deliberately no intake_commit /
// intake_publish kind, and none will be added.
const ProposalKindIntakePlan = "intake_plan"

var ProposalKinds = []string{"policy_add", "policy_remove", "seed_corpus", "mcp_server_add", "mcp_server_remove", ProposalKindAgentImport, ProposalKindAutonomyRevoke, ProposalKindAutonomyDeny, ProposalKindMCPGrantRevoke, ProposalKindVerifyCommand, ProposalKindMergeRule, ProposalKindBlueprintSave, ProposalKindEnvironmentRestrict, ProposalKindAutomationDisable, ProposalKindRoadmapIdea, ProposalKindLearningRetract, ProposalKindMergeQueueEnqueue, ProposalKindCharterSave, ProposalKindIntakePlan}

// ProposalStatuses is migration 0042's status CHECK, mirrored the same way.
var ProposalStatuses = []string{"proposed", "committed", "rejected", "discarded"}

// Proposal is one row, read back. CreatedByKind/ID and DecidedByKind/ID are
// flat fields (not a nested Actor) — the same shape Goal/Task/EventTrigger
// already use for the OL7 actor pair (goals.go, tasks.go, triggers.go).
type Proposal struct {
	ID            int64  `json:"id"`
	Kind          string `json:"kind"`
	Payload       string `json:"payload"`
	Status        string `json:"status"`
	CreatedAt     string `json:"createdAt"`
	CreatedByKind string `json:"createdByKind"`
	CreatedByID   string `json:"createdById"`
	DecidedAt     string `json:"decidedAt,omitempty"`
	DecidedByKind string `json:"decidedByKind,omitempty"`
	DecidedByID   string `json:"decidedById,omitempty"`
	DecideReason  string `json:"decideReason,omitempty"`
	AppliedError  string `json:"appliedError,omitempty"`
}

// inList (plugin_state.go) is reused here rather than redeclared.

// CreateProposal writes the inert row. by is the OL7 actor pair
// (internal/store/actor.go) — the same token every other authored row in
// this codebase uses (goal.created_by_kind/id, task's, event_trigger's),
// not TD1's original single-column guess (recorded on the migration itself).
func (s *Store) CreateProposal(kind, payloadJSON string, by Actor, at string) (Proposal, error) {
	if !inList(ProposalKinds, kind) {
		return Proposal{}, fmt.Errorf("refused: proposal kind %q is not in %v", kind, ProposalKinds)
	}
	if payloadJSON == "" {
		return Proposal{}, fmt.Errorf("refused: proposal payload is empty")
	}
	if !by.validActor() {
		return Proposal{}, fmt.Errorf("refused: proposal needs a well-formed actor (kind one of user/agent/trigger, non-empty id)")
	}
	res, err := s.w.Exec(`INSERT INTO proposal (kind, payload, status, created_at, created_by_kind, created_by_id)
		VALUES (?, ?, 'proposed', ?, ?, ?)`, kind, payloadJSON, at, by.Kind, by.ID)
	if err != nil {
		return Proposal{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Proposal{}, err
	}
	return Proposal{ID: id, Kind: kind, Payload: payloadJSON, Status: "proposed", CreatedAt: at, CreatedByKind: by.Kind, CreatedByID: by.ID}, nil
}

// ListProposals reads rows newest first. Empty status means every row.
func (s *Store) ListProposals(status string) ([]Proposal, error) {
	if status != "" && !inList(ProposalStatuses, status) {
		return nil, fmt.Errorf("refused: proposal status %q is not in %v", status, ProposalStatuses)
	}
	q := `SELECT id, kind, payload, status, created_at, created_by_kind, created_by_id,
		COALESCE(decided_at, ''), COALESCE(decided_by_kind, ''), COALESCE(decided_by_id, ''),
		COALESCE(decide_reason, ''), COALESCE(applied_error, '')
		FROM proposal`
	args := []any{}
	if status != "" {
		q += ` WHERE status = ?`
		args = append(args, status)
	}
	q += ` ORDER BY id DESC`
	rows, err := s.r.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Proposal
	for rows.Next() {
		var p Proposal
		if err := rows.Scan(&p.ID, &p.Kind, &p.Payload, &p.Status, &p.CreatedAt, &p.CreatedByKind, &p.CreatedByID,
			&p.DecidedAt, &p.DecidedByKind, &p.DecidedByID, &p.DecideReason, &p.AppliedError); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetProposal reads one row by id — T5's own addition (T1 exported
// Create/List/Discard/Decide only; commit needs a single-row read rather
// than scanning ListProposals("") by hand).
func (s *Store) GetProposal(id int64) (Proposal, bool, error) {
	var p Proposal
	err := s.r.QueryRow(`SELECT id, kind, payload, status, created_at, created_by_kind, created_by_id,
		COALESCE(decided_at, ''), COALESCE(decided_by_kind, ''), COALESCE(decided_by_id, ''),
		COALESCE(decide_reason, ''), COALESCE(applied_error, '')
		FROM proposal WHERE id = ?`, id).
		Scan(&p.ID, &p.Kind, &p.Payload, &p.Status, &p.CreatedAt, &p.CreatedByKind, &p.CreatedByID,
			&p.DecidedAt, &p.DecidedByKind, &p.DecidedByID, &p.DecideReason, &p.AppliedError)
	if errors.Is(err, sql.ErrNoRows) {
		return Proposal{}, false, nil
	}
	if err != nil {
		return Proposal{}, false, err
	}
	return p, true, nil
}

// SetProposalAppliedError records a commit attempt's effect failure (disk
// full, a constraint create-time validation didn't catch) WITHOUT moving
// the row off `proposed` — T5's own addition, commit-specific (T1's Notes
// named this correctly as belonging here, not at T1). Guarded the same way
// as decideGuarded: only a still-proposed row may record one, so a race
// against a concurrent commit/reject never overwrites a real decision with
// a stale failure note.
func (s *Store) SetProposalAppliedError(id int64, msg string) error {
	res, err := s.w.Exec(`UPDATE proposal SET applied_error = ? WHERE id = ? AND status = 'proposed'`, msg, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 1 {
		return nil
	}
	return fmt.Errorf("refused: proposal %d is not proposed anymore — an applied_error belongs on an attempt still in flight", id)
}

// decideGuarded is the one guarded-UPDATE shape DiscardProposal and
// DecideProposal both ride — the exact claimed-FROM pattern
// TransitionPluginAgent uses (plugin_state.go:147): zero rows affected means
// somebody else already decided it, reported rather than papered over.
func (s *Store) decideGuarded(id int64, toStatus, byKind, byID, reason, at string) error {
	res, err := s.w.Exec(`UPDATE proposal SET status = ?, decided_at = ?, decided_by_kind = ?, decided_by_id = ?, decide_reason = ?
		WHERE id = ? AND status = 'proposed'`, toStatus, at, byKind, byID, reason, id)
	if err != nil {
		// A CHECK violation surfaces here (bad toStatus, bad byKind).
		return err
	}
	if n, _ := res.RowsAffected(); n == 1 {
		return nil
	}
	var status, decidedBy string
	var decidedAt sql.NullString
	err = s.r.QueryRow(`SELECT status, COALESCE(decided_by_id, ''), decided_at FROM proposal WHERE id = ?`, id).
		Scan(&status, &decidedBy, &decidedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("refused: proposal %d does not exist", id)
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("already %s by %s at %s", status, decidedBy, decidedAt.String)
}

// DiscardProposal moves a proposed row to discarded — the only
// socket-reachable status change (TD1): discarding loosens nothing, so it
// carries no reason requirement the way commit/reject's CLI door does.
func (s *Store) DiscardProposal(id int64, by Actor, at string) error {
	if !by.validActor() {
		return fmt.Errorf("refused: discard needs a well-formed actor")
	}
	return s.decideGuarded(id, "discarded", by.Kind, by.ID, "", at)
}

// DecideProposal commits or rejects — CLI-only (T5 is its only caller; no
// socket handler reaches this, brief §7). by.Kind is constrained to "user"
// at the schema level (migration 0042's decided_by_kind CHECK), mirroring
// review.by_kind exactly, because deciding a proposal is this slice's whole
// thesis: a human at a terminal, never a socket caller.
func (s *Store) DecideProposal(id int64, status string, by Actor, reason, at string) error {
	if status != "committed" && status != "rejected" {
		return fmt.Errorf("refused: decide status %q is not committed or rejected", status)
	}
	if by.Kind != "user" || by.ID == "" {
		return fmt.Errorf("refused: only a user can decide a proposal — caller kind %q is not a person", by.Kind)
	}
	return s.decideGuarded(id, status, by.Kind, by.ID, reason, at)
}
