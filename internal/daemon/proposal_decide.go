package daemon

// ════════════════════════════════════════════════════════════════════════════
// The proposal decision door (S45 T5, PM1/PM7): the adapter wiring
// desk.Commit / desk.Reject for the socket. This is the daemon's ONLY file
// that decides a proposal — the second (and only daemon-side) caller of
// store.DecideProposal (T6's whitelist names it) and of the effect Apply —
// reached exclusively through internal/desk's one orchestration, so the
// socket's commit and the CLI's commit are the same code with different
// callers (PM4). proposals.go's create door stays what it was; commit never
// runs policy authoring outside the desk's boundary effects.
//
// # The actor (PM7.3)
//
// The protocol door stamps the Actor from Table.ActorFor (the kernel peer
// credential) and hands it here as the seam's first argument; this file
// trusts NOTHING else and adds nothing: {user, <peer uid>} becomes
// desk.Actor / store.Actor unchanged. An unresolvable peer (uid 0, empty id
// or a missing stamp) is refused HERE too — ErrPolicyNoActor, nothing
// written — so a caller that bypassed the door's own check still cannot
// stamp a placeholder (the check is doubled because the audit row is the
// product, not the protocol layer's courtesy).
//
// # The effect registry (PM4)
//
// The commit's effect table arrives as Options.Effects, a func cmd/aeon
// fills with `func() (*desk.Registry, error) { return
// desk.EffectsFor(openPolicyBoundary) }` — the SAME expression the CLI's
// `deskEffects` variable builds, from the SAME injected seam map
// (desk.RegisterSeams, cmd/aeon/proposal.go). No daemon-side seams map
// exists to drift. The registry is fetched per decision (an opener, like
// the boundary's), so a serve that starts without the bridge answers
// desk's named refusal instead of a half-wired commit.
//
// # Refusals vs infrastructure (DB19)
//
// desk's decision refusals are plain errors carrying the human's sentence;
// they travel as plain errors and the protocol door presents them as
// ErrProposalDecideRefused's `what` VERBATIM. Anything the STORE failed at
// (a read that broke) is typed *protocol.Error here — infrastructure, never
// dressed as a refusal.
// ════════════════════════════════════════════════════════════════════════════

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// proposalDecideService is the daemon's decision door. Like the authoring
// service (T4) it holds the store handle, the boundary over the same
// RepoFuncs bridge (the preview's and before/after's read path) and the
// effect-registry opener; deciding itself lives in desk. The preview
// field is S37's AgentImportPreview seam as threaded through Options —
// the door uses it for the agent_import evidence (S45 T10) and NOTHING
// else; the effect itself runs through the registry's injected seams.
type proposalDecideService struct {
	store   *store.Store
	inForce func(ctx context.Context, target string) ([]desk.InForceRow, error)
	removeT func(ctx context.Context, policyID string) (string, error)
	effects func() (*desk.Registry, error)
	preview protocol.AgentImportRenderer
	now     func() time.Time
}

// newProposalDecideService builds the door. nil store ⇒ nil service ⇒ the
// three methods stay ABSENT on the table (V13, the authoring pattern). A
// nil effects func is honest absence too — every decision then answers
// desk's named "no effect is registered" refusal while reads still stand.
// A nil preview is honest absence as well: the agent_import evidence then
// stays empty (advisory), the commit itself unaffected.
func newProposalDecideService(s *store.Store, effects func() (*desk.Registry, error), preview protocol.AgentImportRenderer) protocol.ProposalDecider {
	if s == nil {
		return nil
	}
	ps := policy.NewStore(newPolicyRepo(s))
	inForce := func(ctx context.Context, target string) ([]desk.InForceRow, error) {
		tgt, rej := parseTargetString(target)
		if rej != nil {
			return nil, fmt.Errorf("refused: %s", rej.Reason)
		}
		answers, rej := ps.InForceFor(ctx, tgt, time.Now().UTC())
		if rej != nil {
			return nil, fmt.Errorf("read the in-force answer for %s: %s", target, rej.Reason)
		}
		rows := make([]desk.InForceRow, 0, len(answers))
		for _, a := range answers {
			rows = append(rows, desk.InForceRow{
				PolicyID: a.Policy.ID,
				State:    string(a.State),
				Render:   a.Render(),
			})
		}
		return rows, nil
	}
	removeTarget := func(ctx context.Context, policyID string) (string, error) {
		rows, rej := ps.ListDetailed(ctx, time.Now().UTC())
		if rej != nil {
			return "", fmt.Errorf("read the row a policy_remove would take: %s", rej.Reason)
		}
		for _, l := range rows {
			if l.Policy.ID == policyID {
				return l.Policy.Target.String(), nil
			}
		}
		return "", nil // already gone — there is no target to answer for
	}
	return &proposalDecideService{
		store:   s,
		inForce: inForce,
		removeT: removeTarget,
		effects: effects,
		preview: preview,
		now:     time.Now,
	}
}

// parseTargetString re-parses a rendered "kind:id" — the desk hands back the
// same rendering policy.Target.String() produced, and ParseTarget is the
// boundary's own gate (fail closed, name the value).
func parseTargetString(s string) (policy.Target, *policy.Reject) {
	// `all-agents` renders BARE (String() names no id — types.go), so a
	// colonless rendering is legal for that kind and only that kind.
	if !strings.Contains(s, ":") {
		kind, r := policy.ParseTargetKind(s)
		if r != nil {
			return policy.Target{}, r
		}
		t, r := policy.ParseTarget(kind, "")
		if r != nil {
			return policy.Target{}, r
		}
		return t, nil
	}
	i := strings.IndexByte(s, ':')
	if i <= 0 || i == len(s)-1 {
		return policy.Target{}, &policy.Reject{
			Reason: fmt.Sprintf("target %q is not rendered as kind:id", s),
		}
	}
	kind, rej := policy.ParseTargetKind(s[:i])
	if rej != nil {
		return policy.Target{}, rej
	}
	return policy.ParseTarget(kind, s[i+1:])
}

// GetProposal reads the row through the store (the door answers
// found=false; the protocol states "does not exist"). The Command field is
// filled exactly as proposal.list fills it.
func (d *proposalDecideService) GetProposal(ctx context.Context, id int64) (protocol.ProposalRow, bool, error) {
	row, found, err := d.store.GetProposal(id)
	if err != nil {
		return protocol.ProposalRow{}, false, protocol.ErrStoreUnavailable.WithWhat(
			"proposal %d could not be read: %s", id, err.Error())
	}
	if !found {
		return protocol.ProposalRow{}, false, nil
	}
	return proposalRowStoreToWire(row), true, nil
}

// ProposalDigest is the one desk canonicaliser, reached through the seam so
// proposal.get answers THE commit door's digest function (PM4 — no second
// implementation the read door could drift from).
func (d *proposalDecideService) ProposalDigest(payload string) string {
	return desk.ProposalPayloadDigest(payload)
}

// CommitProposal stamps the actor (refusing uid 0 / empty / non-user —
// never a placeholder) and hands the whole decision to desk.Commit.
//
// S45 T10's two door duties for `agent_import`: the effect's consent print
// is CAPTURED (the daemon must never stream to a terminal — a socket
// answer is data, not a TTY broadcast) and shipped on the result as data
// (warnings carry it verbatim, the CLI/app print it); and the before/after
// pair speaks for the agent, rendered by S37's own renderers — the preview
// read BEFORE the effect (nothing new is formatted here, D14) and the
// body's post-import print after. Every other kind's before/after stays
// desk's in-force pair, unmoved.
func (d *proposalDecideService) CommitProposal(ctx context.Context, actor protocol.Actor, id int64, digest string) (protocol.ProposalCommitResult, error) {
	if err := decisionActorRefusal(actor); err != nil {
		return protocol.ProposalCommitResult{}, err
	}
	reg, err := d.registry()
	if err != nil {
		return protocol.ProposalCommitResult{}, err
	}
	// The evidence read: the row's kind decides whether the agent pair is
	// asked for at all. This read gates NOTHING — desk re-reads the row
	// behind its own gates; a failure here just means no evidence.
	var agent string
	var agentBefore protocol.InForceView
	if row, found, rerr := d.store.GetProposal(id); rerr == nil && found && row.Kind == "agent_import" {
		agent, agentBefore = protocol.AgentImportBeforeView(ctx, d.preview, []byte(row.Payload))
	}
	deps := d.deps()
	var imported strings.Builder
	deps.ApplyWriter = &imported // the consent, captured — never streamed
	res, err := desk.Commit(ctx, deps, reg, id, digest, desk.Actor{Kind: actor.Kind, ID: actor.ID})
	if err != nil {
		return protocol.ProposalCommitResult{}, err
	}
	warnings := res.Warnings
	captured := strings.TrimSpace(imported.String())
	if captured != "" {
		warnings = append(warnings, captured)
	}
	before, after := inForceViewToWire(res.Before), inForceViewToWire(res.After)
	if agent != "" {
		before = agentBefore
		after = protocol.AgentImportAfterView(agent, captured) // recovery completion: captured is "" ⇒ empty view
	}
	return protocol.ProposalCommitResult{
		Proposal:      proposalRowToWire(res.Row),
		EffectApplied: res.EffectApplied,
		Before:        before,
		After:         after,
		Warnings:      warnings,
	}, nil
}

// RejectProposal stamps the actor and hands the flip-and-audit to
// desk.Reject. No effect registry is fetched, consulted, or even required
// on this path — a reject applies NOTHING.
func (d *proposalDecideService) RejectProposal(ctx context.Context, actor protocol.Actor, id int64, reason string) (protocol.ProposalRow, error) {
	if err := decisionActorRefusal(actor); err != nil {
		return protocol.ProposalRow{}, err
	}
	res, err := desk.Reject(ctx, d.deps(), id, reason, desk.Actor{Kind: actor.Kind, ID: actor.ID})
	if err != nil {
		return protocol.ProposalRow{}, err
	}
	return proposalRowToWire(res.Row), nil
}

// deps assembles desk.DecideDeps over THIS daemon's handles: the row read,
// the write triple, the in-force seam, the policy_remove target resolver,
// the process clock.
func (d *proposalDecideService) deps() desk.DecideDeps {
	return desk.DecideDeps{
		Get: func(id int64) (desk.Row, bool, error) {
			row, found, err := d.store.GetProposal(id)
			if err != nil {
				return desk.Row{}, false, protocol.ErrStoreUnavailable.WithWhat(
					"proposal %d could not be read: %s", id, err.Error())
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
		Store:          decisionStore{d.store},
		InForce:        d.inForce,
		RemoveTarget:   d.removeT,
		Now:            d.now,
		ReasonRequired: true, // the socket door: a reject states its reason (PM7.5)
	}
}

// registry fetches the effect table per decision (an opener, like the
// boundary's — a serve wired without it says so at the call).
func (d *proposalDecideService) registry() (*desk.Registry, error) {
	if d.effects == nil {
		return nil, protocol.ErrNotWired.WithWhat(
			"proposal effects are not wired — `aeon serve` passes Options.Effects (desk.EffectsFor over cmd/aeon's one seam map); this daemon started without it")
	}
	return d.effects()
}

// decisionActorRefusal is the door's own doubling of the protocol check
// (PM7.3): the audit row is the product, so the stamping layer refuses an
// unattributable person even when called from outside the dispatch.
func decisionActorRefusal(a protocol.Actor) error {
	if a.Kind != "user" || a.ID == "" || a.ID == "0" {
		return protocol.ErrPolicyNoActor.WithWhat(
			"a proposal decision needs the deciding person as user:<peer uid> — got %q:%q (uid 0 is the unresolvable peer, refused rather than stamped)", a.Kind, a.ID)
	}
	return nil
}

// decisionStore adapts *store.Store to desk.Store — the shape translation
// (desk.ActorPair → store.Actor, desk.PolicyChangeRow → store.PolicyChange)
// lives HERE, at the door, so internal/desk never imports internal/store
// (its dependency rule, pinned by its own TestDeskImportsNothingForbidden).
// No rule lives in this shim: it forwards, exactly.
type decisionStore struct{ s *store.Store }

func (a decisionStore) DecideProposal(id int64, status string, by desk.ActorPair, reason, at string) error {
	return a.s.DecideProposal(id, status, store.Actor{Kind: by.Kind, ID: by.ID}, reason, at)
}

func (a decisionStore) SetProposalAppliedError(id int64, msg string) error {
	return a.s.SetProposalAppliedError(id, msg)
}

func (a decisionStore) PolicyChangeInsert(ctx context.Context, c desk.PolicyChangeRow) error {
	return a.s.PolicyChangeInsert(ctx, store.PolicyChange{
		Op: c.Op, PolicyID: c.PolicyID, ProposalID: c.ProposalID,
		ActorKind: c.ActorKind, ActorID: c.ActorID,
		Summary: c.Summary, OccurredAt: c.OccurredAt,
	})
}

// proposalRowToWire maps the store/desk row onto the protocol row, filling
// Command exactly as proposal.list does (one spelling, in proposals.go).
func proposalRowToWire(r desk.Row) protocol.ProposalRow {
	return proposalRowStoreToWire(store.Proposal{
		ID: r.ID, Kind: r.Kind, Payload: r.Payload, Status: r.Status,
		CreatedAt: r.CreatedAt, CreatedByKind: r.CreatedByKind, CreatedByID: r.CreatedByID,
		DecidedAt: r.DecidedAt, DecidedByKind: r.DecidedByKind, DecidedByID: r.DecidedByID,
		DecideReason: r.DecideReason, AppliedError: r.AppliedError,
	})
}

// proposalRowStoreToWire maps a store row onto the wire row. The Command
// spelling lives in protocol (commandFor); the adapter fills it the same
// way proposal.list's handler does after the seam returns.
func proposalRowStoreToWire(p store.Proposal) protocol.ProposalRow {
	return protocol.ProposalRow{
		ID: p.ID, Kind: p.Kind, Payload: []byte(p.Payload), Status: p.Status,
		CreatedAt: p.CreatedAt, CreatedByKind: p.CreatedByKind, CreatedByID: p.CreatedByID,
		DecidedAt: p.DecidedAt, DecidedByKind: p.DecidedByKind, DecidedByID: p.DecidedByID,
		DecideReason: p.DecideReason, AppliedError: p.AppliedError,
	}
}

// inForceViewToWire maps the desk view onto the protocol view — 1:1 field
// moves, Render() bytes untouched (D14).
func inForceViewToWire(v desk.InForceView) protocol.InForceView {
	rows := make([]protocol.PolicyInForceRow, 0, len(v.Answers))
	for _, r := range v.Answers {
		rows = append(rows, protocol.PolicyInForceRow{
			PolicyID: r.PolicyID, State: r.State, Render: r.Render,
		})
	}
	return protocol.InForceView{Target: v.Target, Answers: rows}
}
