package main

// ════════════════════════════════════════════════════════════════════════════
// The CLI door onto the desk's decision orchestration (S45 T5, PM4):
// `aeon proposal commit/reject` became CALLERS of desk.Commit/desk.Reject —
// the same body the socket door (internal/daemon/proposal_decide.go) runs.
// The CLI keeps flags, printing, and its own status sentence (golden bytes);
// the deciding, the effects, the flip and the audit row are the desk's.
//
// This file also owns the two composition-root moves T5 needs:
//
//   - `desk.RegisterSeams(deskSeams())` — the ONE seam map (defined beside
//     `deskEffects` below) is injected into internal/desk once, so the
//     daemon's registry — built by `desk.EffectsFor` for Options.Effects —
//     and the CLI's `deskEffects` share one seam definition (PM4: no
//     daemon-side table exists to drift from this one);
//   - `openPolicyBoundary` — the boundary opener as a VALUE, over policy.go's
//     own `policyBoundary` variable (read per call, so the fixtures that
//     point the verbs at a temp store or a spy repo still do), handed to
//     `desk.EffectsFor` exactly as `deskEffects` is built with it.
//
// The CLI's reject stays reason-OPTIONAL (TD5's CLI ruling, golden-pinned);
// the required-reason discipline is the socket door's (DecideDeps.
// ReasonRequired). The CLI's commit digest is the one its own read produced
// — the terminal person reads the rows this very command lists; the binding
// rule (commit what you read) holds at both doors.
// ════════════════════════════════════════════════════════════════════════════

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/autonomy"
	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/store"
)

// deskSeams is the seam map `deskEffects` registers and `desk.RegisterSeams`
// injects — ONE map literal, read by both doors' registries. Extracted from
// the deskEffects literal so the injection cannot drift from the registry.
func deskSeams() desk.Seams {
	return desk.Seams{
		desk.KindMCPServerAdd: {
			Validate:       seamValidateMCPServerAdd,
			AlreadyApplied: seamAlreadyAppliedMCPServerAdd,
			Apply:          seamApplyMCPServerAdd,
		},
		desk.KindMCPServerRem: {
			Validate:       seamValidateMCPServerRemove,
			AlreadyApplied: seamAlreadyAppliedMCPServerRemove,
			Apply:          seamApplyMCPServerRemove,
		},
		desk.KindMCPGrantRevoke: {
			Validate:       seamValidateMCPGrantRevoke,
			AlreadyApplied: seamAlreadyAppliedMCPGrantRevoke,
			Apply:          seamApplyMCPGrantRevoke,
		},
		desk.KindAgentImport: {
			// Validate stays the desk's — it is a payload decode, no plugin
			// layer in sight (S37 T4's wording, moved into validate.go).
			AlreadyApplied: seamAlreadyAppliedAgentImport,
			Apply:          seamApplyAgentImport,
		},
		// S56 T8 (AL11): the two RESTRICTIVE autonomy kinds. Each seam
		// REPLACES the desk's built-in shape half with the full
		// desk-then-store half (the merge rule in desk.New), so the create
		// door and the commit door run the same function through the same
		// registry (TD3). There is no third autonomy kind here — a proposal
		// that creates or widens an allow is not a shape this package knows.
		desk.KindAutonomyRevoke: {
			Validate:       seamValidateAutonomyRevoke,
			AlreadyApplied: seamAlreadyAppliedAutonomyRevoke,
			Apply:          seamApplyAutonomyRevoke,
		},
		desk.KindAutonomyDeny: {
			Validate:       seamValidateAutonomyDeny,
			AlreadyApplied: seamAlreadyAppliedAutonomyDeny,
			Apply:          seamApplyAutonomyDeny,
		},
		// S57 T8 (2026-11-02): the two RESTRICTIVE gate kinds, the same
		// autonomy shape (gate_proposal_seams.go) — the built-in desk
		// halves are the pure shape validators, and these REPLACE them
		// with the full desk-then-store Validate plus the AlreadyApplied
		// and Apply halves. There is no third gate kind: a proposal that
		// loosens a gate or a rule is not a shape this package knows.
		desk.KindVerifyCommand: {
			Validate:       seamValidateVerifyCommand,
			AlreadyApplied: seamAlreadyAppliedVerifyCommand,
			Apply:          seamApplyVerifyCommand,
		},
		desk.KindMergeRule: {
			Validate:       seamValidateMergeRule,
			AlreadyApplied: seamAlreadyAppliedMergeRule,
			Apply:          seamApplyMergeRule,
		},
		// S48 T9 (2026-11-03): the design revision (S35 TD1). The desk
		// half is the payload shape (blueprint_kinds.go); this seam
		// REPLACES Validate with the desk-then-save-door-judgment half and
		// supplies AlreadyApplied (the content hash — the crash-retry
		// completes, no second revision) and Apply (daemon.BlueprintSave —
		// THE one write path, actor = the committing person from
		// desk.DeciderFrom, never the proposer).
		desk.KindBlueprintSave: {
			Validate:       seamValidateBlueprintSave,
			AlreadyApplied: seamAlreadyAppliedBlueprintSave,
			Apply:          seamApplyBlueprintSave,
		},
		// S59 T7 (2026-11-03, EV16): the execution-environment tightening
		// revision. The desk half is the payload shape
		// (environment_kinds.go); this seam REPLACES Validate with the
		// desk-then-plane-judgment half — `execenv.Restricts` coming back
		// EMPTY or the row is being widened, every axis named verbatim at
		// both doors (TD3) — and supplies AlreadyApplied (the canonical
		// bytes: an ask that already landed commits clean, no second
		// revision) and Apply (the store's own CAS door, actor = the
		// COMMITTING person from desk.DeciderFrom, never the proposer, with
		// desk.ProposalIDFrom's id carried in the audit summary — two
		// identities, both recorded).
		desk.KindEnvironmentRestrict: {
			Validate:       seamValidateEnvironmentRestrict,
			AlreadyApplied: seamAlreadyAppliedEnvironmentRestrict,
			Apply:          seamApplyEnvironmentRestrict,
		},
		// S49 T6 (2026-11-03, AN9): the automation plane's one ask. The
		// desk half is the payload shape (automation_kinds.go); this seam
		// REPLACES Validate with the desk-then-row half — the automation
		// exists under that source, is not archived, is not already off
		// (refused at create; TD3's nothing-inert-carried-to-commit) — and
		// supplies AlreadyApplied (the row already IS off: the crash-retry
		// completes the flip, no second change, AN4) and Apply (T4's own
		// paths — NewTriggerWrites.DisableEventTrigger for the event row,
		// the Scheduler's Pause for a schedule or webhook row — actor =
		// the COMMITTING person from desk.DeciderFrom, never the
		// proposer; desk.ProposalIDFrom fail-closed beside it).
		desk.KindAutomationDisable: {
			Validate:       seamValidateAutomationDisable,
			AlreadyApplied: seamAlreadyAppliedAutomationDisable,
			Apply:          seamApplyAutomationDisable,
		},
		// S50 T5 (RM8, TD3, TD4): the roadmap plane's one agent ask — an
		// idea to be ADDED. Validate is the desk shape plus the two store
		// facts (root exists, no identical open twin); AlreadyApplied is
		// the crash-retry answer (the twin exists now); Apply is roadmap's
		// own create with the COMMITTER as actor and provenance authored.
		desk.KindRoadmapIdea: {
			Validate:       seamValidateRoadmapIdea,
			AlreadyApplied: seamAlreadyAppliedRoadmapIdea,
			Apply:          seamApplyRoadmapIdea,
		},
		// S52 T4 (LM7, LM8, TD3, TD4): the memory plane's one agent ask —
		// a learning to be TAKEN BACK. Validate is the desk shape plus the
		// two row facts (it exists, it is not already retracted — an ask
		// that changes nothing is refused at create, not committed);
		// AlreadyApplied is the crash-retry answer (the stamp is already
		// on); Apply is learnings' OWN retract with the COMMITTER as actor
		// — stamp, one learning_change line, vector hygiene, never the
		// proposer's name on any of it.
		desk.KindLearningRetract: {
			Validate:       seamValidateLearningRetract,
			AlreadyApplied: seamAlreadyAppliedLearningRetract,
			Apply:          seamApplyLearningRetract,
		},
		// S60 T9 (2026-11-04, MQ14/MQ14.3): the queue plane's one agent
		// ask. Validate is the desk shape THEN mergequeue.ValidateEnqueue
		// over MergeQueueRepoFacts — the same function and the same sheet
		// the socket door and the daemon arm run (TD3: create, arm and
		// commit are one validator); AlreadyApplied is the open row the
		// crash-retry filed itself; Apply is the queue's OWN
		// Service.Enqueue, unattended, the row stamped {proposal, <id>}
		// and the event carried by the committer.
		desk.KindMergeQueueEnqueue: {
			Validate:       seamValidateMergeQueueEnqueue,
			AlreadyApplied: seamAlreadyAppliedMergeQueueEnqueue,
			Apply:          seamApplyMergeQueueEnqueue,
		},
		// S61 T5 (2026-11-05, GI4): a charter body to be SAVED. Validate is
		// the desk shape THEN charter.Validate over the decoded body THEN
		// the root facts (it exists, it is a root — the store's own two
		// sentences quoted, TD3); AlreadyApplied is the crash-retry answer
		// (the current revision IS this body — history never churns);
		// Apply is the charter's OWN Save with the COMMITTER as the
		// revision actor, source 'proposal', the proposal id as
		// source_ref — never the proposer, and no note (GI4, the
		// charter package's note-source law).
		desk.KindCharterSave: {
			Validate:       seamValidateCharterSave,
			AlreadyApplied: seamAlreadyAppliedCharterSave,
			Apply:          seamApplyCharterSave,
		},
		// S61 T5 (2026-11-05, GI7/OD4): a goal-intake plan. ONLY Validate
		// is registered — the desk's built-in AlreadyApplied (always false
		// by law) and Apply (the fixed sentence naming intake.commit) are
		// the kind's DESIGN, and a seam half here would REPLACE the
		// refusal the generic door exists to give. The plan's charter body
		// and the template registry are the seam's reads (TD3: the same
		// function at both doors).
		desk.KindIntakePlan: {
			Validate: seamValidateIntakePlan,
		},
	}
}

// init injects the seam map once per process — BEFORE any daemon or CLI door
// can build a registry (init ordering, not call ordering: an accidental nil
// registry would answer every effect kind with desk's named refusal, which
// is exactly the visible-not-silent rule that makes the accident findable).
func init() {
	desk.RegisterSeams(deskSeams())
}

// cliDecideDeps assembles desk.DecideDeps over the CLI's own store handle:
// the row read, the write triple, the in-force answers over the SAME policy
// store the boundary uses, the policy_remove target resolver, the process
// clock. The CLI's reject reason is optional (ReasonRequired false — TD5's
// CLI ruling, golden-pinned); the socket adapter sets true.
func cliDecideDeps(st *store.Store) desk.DecideDeps {
	ps := policy.NewStore(policyRepo(st))
	return desk.DecideDeps{
		Get: func(id int64) (desk.Row, bool, error) {
			row, found, err := st.GetProposal(id)
			if err != nil {
				return desk.Row{}, false, err
			}
			if !found {
				return desk.Row{}, false, nil
			}
			return desk.Row{
				ID: row.ID, Kind: row.Kind, Payload: row.Payload, Status: row.Status,
				CreatedAt: row.CreatedAt, CreatedByKind: row.CreatedByKind, CreatedByID: row.CreatedByID,
				DecidedAt: row.DecidedAt, DecidedByKind: row.DecidedByKind, DecidedByID: row.DecidedByID,
				DecideReason: row.DecideReason, AppliedError: row.AppliedError,
			}, true, nil
		},
		Store: cliDecisionStore{st},
		InForce: func(ctx context.Context, target string) ([]desk.InForceRow, error) {
			tgt, rej := parseTargetRendered(target)
			if rej != nil {
				return nil, rej
			}
			answers, frej := ps.InForceFor(ctx, tgt, time.Now().UTC())
			if frej != nil {
				return nil, fmt.Errorf("%s", frej.Reason)
			}
			rows := make([]desk.InForceRow, 0, len(answers))
			for _, a := range answers {
				rows = append(rows, desk.InForceRow{PolicyID: a.Policy.ID, State: string(a.State), Render: a.Render()})
			}
			return rows, nil
		},
		RemoveTarget: func(ctx context.Context, policyID string) (string, error) {
			rows, rej := ps.ListDetailed(ctx, time.Now().UTC())
			if rej != nil {
				return "", fmt.Errorf("%s", rej.Reason)
			}
			for _, l := range rows {
				if l.Policy.ID == policyID {
					return l.Policy.Target.String(), nil
				}
			}
			return "", nil
		},
	}
}

// cliDecisionStore adapts *store.Store to desk.Store at the CLI door (the
// same shape translation the daemon's decisionStore does — each door owns
// its own shim; the desk stays store-free).
type cliDecisionStore struct{ s *store.Store }

func (a cliDecisionStore) DecideProposal(id int64, status string, by desk.ActorPair, reason, at string) error {
	return a.s.DecideProposal(id, status, store.Actor{Kind: by.Kind, ID: by.ID}, reason, at)
}

func (a cliDecisionStore) SetProposalAppliedError(id int64, msg string) error {
	return a.s.SetProposalAppliedError(id, msg)
}

func (a cliDecisionStore) PolicyChangeInsert(ctx context.Context, c desk.PolicyChangeRow) error {
	return a.s.PolicyChangeInsert(ctx, store.PolicyChange{
		Op: c.Op, PolicyID: c.PolicyID, ProposalID: c.ProposalID,
		ActorKind: c.ActorKind, ActorID: c.ActorID,
		Summary: c.Summary, OccurredAt: c.OccurredAt,
	})
}

// — the two restrictive autonomy kinds (S56 T8, AL11) —
//
// The store halves open the store PER CALL through openStoreDirectly — the
// CLI's justified second access path (WAL serialises it behind the daemon's
// writer), read per call like policyBoundary so a fixture can still point
// the verbs at a temp store. The daemon door holds its own handle and runs
// these same seam values, which is the point of desk.RegisterSeams: one
// definition, two doors, no daemon-side table to drift from (PM4).

// seamValidateAutonomyRevoke is autonomy_revoke's full Validate: the desk
// half (payload shape, the id, the reason — THE function the create door
// runs, TD3), then the store half: the grant exists, and it is not a veto —
// "a veto is removed by a person, not proposed away" (an agent cannot
// propose to remove a veto, because that would widen, AL11). An
// already-revoked grant is NOT refused here: that is AlreadyApplied's
// answer at commit, and refusing it at create would kill the crash-retry
// path (an effect that landed but did not flip is a retry, not a conflict).
func seamValidateAutonomyRevoke(_ context.Context, payload []byte) error {
	if err := desk.ValidateAutonomyRevoke(context.Background(), payload); err != nil {
		return err
	}
	var p desk.AutonomyRevokePayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return err
	}
	st, err := openStoreDirectly(false)
	if err != nil {
		return err
	}
	defer st.Close()
	g, err := st.ReadAutonomyGrant(p.ID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("refused: %s is not an autonomy grant — it cannot be revoked", p.ID)
		}
		return err
	}
	if g.State == "deny" {
		return fmt.Errorf("refused: %s is a veto — a veto is removed by a person, not proposed away", p.ID)
	}
	return nil
}

// seamValidateAutonomyDeny is autonomy_deny's full Validate: the desk half
// (shape, node, the two-shape subject vocabulary, the reason), then the
// store half: the node exists and is not archived, and the pure core's own
// deny validator runs on the subject (AL4 parity — a veto carries no
// conditions and no expiry; the subject's action-registry check sits in the
// allow branch, so for a veto the desk's pure half pins the shape and the
// core's validator is the drift lock, which is why its words cannot drift
// from the authoring door's).
func seamValidateAutonomyDeny(_ context.Context, payload []byte) error {
	if err := desk.ValidateAutonomyDeny(context.Background(), payload); err != nil {
		return err
	}
	var p desk.AutonomyDenyPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return err
	}
	st, err := openStoreDirectly(false)
	if err != nil {
		return err
	}
	defer st.Close()
	c, err := st.ReadComponent(p.NodeID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("refused: %s is not a component — a veto needs a node it can sit on", p.NodeID)
		}
		return err
	}
	if c.ArchivedAt != "" {
		return fmt.Errorf("refused: %s is archived — its grants cannot be written", p.NodeID)
	}
	if ref := autonomy.ValidateGrant(autonomy.Grant{
		NodeID:  p.NodeID,
		Subject: autonomy.Subject{Kind: p.Subject.Kind, Name: p.Subject.Name},
		State:   autonomy.StateDeny,
	}, autonomy.NodeInfo{IsRoot: c.ParentID == "", Archived: false}, time.Now()); ref != nil {
		return fmt.Errorf("%s", ref.Sentence)
	}
	return nil
}

// seamAlreadyAppliedAutonomyRevoke is TD4's answer for a revoke: the grant
// exists and is already revoked — the effect landed, so the commit flips
// the row without re-applying (a crash between the revoke and the flip
// completes instead of re-revoking; the store's own no-op is the backstop
// for the race). An unknown id answers false, not an error: the validator
// refuses it loudly, and a gate that guessed would paper over that refusal.
func seamAlreadyAppliedAutonomyRevoke(_ context.Context, payload []byte) (bool, error) {
	var p desk.AutonomyRevokePayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return false, err
	}
	st, err := openStoreDirectly(false)
	if err != nil {
		return false, err
	}
	defer st.Close()
	g, err := st.ReadAutonomyGrant(p.ID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	return g.RevokedAt != nil, nil
}

// seamAlreadyAppliedAutonomyDeny is TD4's answer for a veto: an identical
// ACTIVE veto already stands — same node, same subject, not revoked. A
// revoked twin is not the answer (it is not standing authority), and a
// different node or subject answers false so validate+apply run and the
// store's own doors re-decide the real write. The list reads over the
// root (a veto may sit on a child), then the fields pin the node.
func seamAlreadyAppliedAutonomyDeny(_ context.Context, payload []byte) (bool, error) {
	var p desk.AutonomyDenyPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return false, err
	}
	st, err := openStoreDirectly(false)
	if err != nil {
		return false, err
	}
	defer st.Close()
	rootID := p.NodeID
	if c, err := st.ReadComponent(p.NodeID); err == nil && c.ParentID != "" {
		rootID = c.ParentID
	}
	rows, err := st.ListAutonomyGrants(store.AutonomyGrantFilter{RootID: rootID, State: "deny"})
	if err != nil {
		return false, err
	}
	for _, g := range rows {
		if g.NodeID == p.NodeID && g.SubjectKind == p.Subject.Kind && g.Subject == p.Subject.Name {
			return true, nil // the filter keeps revoked rows out
		}
	}
	return false, nil
}

// seamApplyAutonomyRevoke performs the committed revoke through the store's
// own door with the COMMITTER as actor — the decider desk.Commit installed
// on the context immediately before Apply (the only code that sees it).
// The actor on the grant row and in the change row is the person who
// decided, never the agent who proposed: the grant's proposed_by (the
// author) rides on the grant row from creation, and the revoke stamps the
// committer in the revoked_* trio. Fail-closed: a context without the
// decider is not a commit, and a write with a guessed actor is the audit
// gap the decider exists to close.
func seamApplyAutonomyRevoke(ctx context.Context, _ io.Writer, payload []byte) error {
	dec, ok := desk.DeciderFrom(ctx)
	if !ok {
		return fmt.Errorf("refused: autonomy_revoke's apply needs the committer's identity from the commit door — a context without it is not a commit, and a write with a guessed actor is the audit gap (S56 T8)")
	}
	var p desk.AutonomyRevokePayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return err
	}
	st, err := openStoreDirectly(false)
	if err != nil {
		return err
	}
	defer st.Close()
	_, _, ref, err := st.RevokeAutonomyGrant(store.Actor{Kind: dec.Kind, ID: dec.ID}, p.ID, p.Reason)
	if err != nil {
		return err
	}
	if ref != nil {
		return errors.New(ref.Sentence)
	}
	return nil
}

// seamApplyAutonomyDeny performs the committed veto through the store's own
// door with the COMMITTER as actor and the PROPOSAL'S AUTHOR as proposed_by
// — the two identities are different people, and conflating them is the
// failure the audit exists to catch (S56 T8 behaviour 2). The veto carries
// no note (SEC-033) and no conditions (a veto with conditions is not a
// veto). Fail-closed on a missing decider, as the revoke half.
func seamApplyAutonomyDeny(ctx context.Context, _ io.Writer, payload []byte) error {
	dec, ok := desk.DeciderFrom(ctx)
	if !ok {
		return fmt.Errorf("refused: autonomy_deny's apply needs the committer's identity from the commit door — a context without it is not a commit, and a write with a guessed actor is the audit gap (S56 T8)")
	}
	var p desk.AutonomyDenyPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return err
	}
	st, err := openStoreDirectly(false)
	if err != nil {
		return err
	}
	defer st.Close()
	_, ref, err := st.CreateAutonomyGrant(store.Actor{Kind: dec.Kind, ID: dec.ID}, store.NewAutonomyGrant{
		NodeID:      p.NodeID,
		SubjectKind: p.Subject.Kind,
		Subject:     p.Subject.Name,
		State:       "deny",
		ProposedBy:  dec.AuthorKind + ":" + dec.AuthorID,
	})
	if err != nil {
		return err
	}
	if ref != nil {
		return errors.New(ref.Sentence)
	}
	return nil
}

// parseTargetRendered re-parses a rendered "kind:id" back through the
// boundary's own gates (the desk hands back policy.Target.String() output;
// ParseTarget owns the grammar, fail-closed, naming the value).
func parseTargetRendered(s string) (policy.Target, error) {
	// `all-agents` renders BARE (String() names no id — types.go), so a
	// colonless rendering is legal for that kind and only that kind.
	if !strings.Contains(s, ":") {
		kind, r := policy.ParseTargetKind(s)
		if r != nil {
			return policy.Target{}, fmt.Errorf("%s", r.Reason)
		}
		t, r := policy.ParseTarget(kind, "")
		if r != nil {
			return policy.Target{}, fmt.Errorf("%s", r.Reason)
		}
		return t, nil
	}
	i := strings.IndexByte(s, ':')
	if i <= 0 || i == len(s)-1 {
		return policy.Target{}, fmt.Errorf("target %q is not rendered as kind:id", s)
	}
	kind, rej := policy.ParseTargetKind(s[:i])
	if rej != nil {
		return policy.Target{}, fmt.Errorf("%s", rej.Reason)
	}
	t, rej := policy.ParseTarget(kind, s[i+1:])
	if rej != nil {
		return policy.Target{}, fmt.Errorf("%s", rej.Reason)
	}
	return t, nil
}
