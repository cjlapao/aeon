package protocol

// ════════════════════════════════════════════════════════════════════════════
// The proposal decision doors (S45 T5, PM1/PM7): get, commit, reject —
// the HUMAN door over the socket, the operator ruling of 2026-10-30 that
// policy.add/remove/seedCorpus arrived under at T4, applied to deciding
// proposals. proposals.go stays untouched (its header still documents the
// pre-S45 absence; T6 amends it) — everything the decision needs is THIS
// file, and the diff of the existing door stays empty.
//
// # What each method is
//
// `proposal.get {id}` → {proposal, digest, preview?}: the row exactly as
// proposal.list renders it, plus the sha256 digest of the canonical payload
// (PM7.1's binding token, computed by the service over desk's one digest
// function) and, for policy_add / policy_remove / seed_corpus, a LIVE
// read-only preview: the would-be row's Describe sentence (policy.Describe,
// the one formatter) and the target's current in-force answer through the
// existing Table.PolicyStore handle — the same reads policy.inForce makes,
// assembled by nobody but the boundary. The preview is ADVISORY (S37 AA4):
// proposal.commit re-validates and re-reads, never trusting what was
// displayed.
//
// `proposal.commit {id, digest}` → {proposal, effectApplied, before, after,
// warnings}: the whole decision runs behind the seam in internal/desk —
// digest verify, status gate, AlreadyApplied, Validate, before, Apply,
// after, the claimed-from flip, the audit row. effectApplied:false is the
// crash-recovery path (the effect had landed; this flip completed it) and
// the door SAYS so rather than re-applying.
//
// `proposal.reject {id, reason}` → {proposal}: flips the row with a reason
// (required, ≤ 512 bytes, TD5) and applies NOTHING — the registry is never
// consulted on this path.
//
// # The actor is the kernel's peer, never a param (PM7.3)
//
// Same discipline as the T4 authoring doors: `Table.ActorFor` stamps
// {user, <peer uid>}; an unresolvable peer (uid 0) is refused
// ErrPolicyNoActor, never a placeholder; params carrying an identity key
// are refused as a shape error (rejectIdentityKeys, the package's own
// stand-in for strict decoding). `agent_import` commits run the GENERIC
// path like every other kind (S45 T10): the effect reaches the plugin
// layer through the one cmd/aeon seam map (the daemon itself links no
// plugin code), the consent print is captured and shipped as data on the
// result (a daemon never streams to a terminal), and no `policy_change`
// row is written — the authoring audit is policy's; the import's record
// is the row's own decided_by_* stamp.
//
// # Registered iff wired (V13)
//
// RegisterProposalDecider registers the three when — and only when —
// Table.ProposalDecider is non-nil: absent, not registered-and-refusing
// (a daemon without the decision wiring answers ErrUnknownMethod, the
// RegisterSeed rule). RequiresInitialize true, Debug false.
// ════════════════════════════════════════════════════════════════════════════

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/policy"
)

// ProposalDecider is the package-local seam onto the daemon's decision
// service (the ProposalStore/PolicyAuthoring pattern — no internal/store or
// internal/desk import here; the adapter lives in internal/daemon). The
// actor argument is filled by THIS package from the connection (ActorFor),
// never from params. Errors: a *Error travels untouched (the service's own
// typed refusal); a plain error is a DECISION refusal and is presented as
// ErrProposalDecideRefused's `what` VERBATIM — desk's sentences are the
// refusal, not a paraphrase of it. Infrastructure never arrives plain: the
// adapter wraps store failures as its own typed errors.
type ProposalDecider interface {
	GetProposal(ctx context.Context, id int64) (ProposalRow, bool, error)

	// ProposalDigest is the one desk canonicaliser (desk.ProposalPayload-
	// Digest) reached through the seam — proposal.get answers THE commit
	// door's own digest function, not a second implementation the read
	// door could drift from (PM4). Pure: no store, no clock.
	ProposalDigest(payload string) string

	CommitProposal(ctx context.Context, actor Actor, id int64, digest string) (ProposalCommitResult, error)
	RejectProposal(ctx context.Context, actor Actor, id int64, reason string) (ProposalRow, error)
}

// --- the wire shapes ----------------------------------------------------------

// ProposalGetParams is the read door's params: the one id.
type ProposalGetParams struct {
	ID int64 `json:"id"`
}

// ProposalGetResult carries the row, its digest (PM7.1's token — commit
// demands this exact value back) and the advisory preview (nil for kinds
// the preview means nothing for: mcp_server_add/remove name no policy
// target; `agent_import` carries S37's preview here since S45 T10 — its
// consent is the text the commit is confirmed against).
type ProposalGetResult struct {
	Proposal ProposalRow      `json:"proposal"`
	Digest   string           `json:"digest"`
	Preview  *ProposalPreview `json:"preview"`
}

// ProposalPreview is the advisory before-you-commit answer (S37 AA4 — the
// commit door re-validates and re-reads; nothing here is authoritative):
//
//   - policy_add: the would-be row's Describe sentence (policy.Describe over
//     the payload) and the target's CURRENT in-force answer — what changes
//     when this lands, beside what stands today;
//   - policy_remove: the removed row's Describe sentence (read live; "" when
//     the row is already gone) and the target's current answer;
//   - seed_corpus: the agents named and nothing else — the run spans many
//     targets, and policy.seedCorpus's dry-run IS the real preview, so this
//     preview names its scope and points there rather than inventing a
//     sentence the seed door itself does not print.
type ProposalPreview struct {
	Kind     string             `json:"kind"`
	Target   string             `json:"target,omitempty"`
	Describe string             `json:"describe,omitempty"`
	Agents   []string           `json:"agents,omitempty"`
	InForce  []PolicyInForceRow `json:"inForce"`
	Note     string             `json:"note,omitempty"`
	// Blueprint is S48 T9's preview for kind blueprint_save — the diff of
	// the proposed bytes against what the row holds today (or "new"), the
	// validation report summary, and the ADVISORY tool lint. Computed
	// read-only by the daemon's read service through the BlueprintPreview
	// seam; it gates nothing (AA4), and a read failure omits it rather
	// than failing the read of the row.
	Blueprint *BlueprintProposalPreview `json:"blueprint,omitempty"`
	// Automation is S49 T6's preview for kind automation_disable — the
	// automation's Describe sentence, its current state, its last five
	// fires and its health verdict. Computed read-only by the daemon's
	// read service through the AutomationPreview seam; it gates nothing
	// (AA4), and a read failure omits it rather than failing the read of
	// the row.
	Automation *AutomationProposalPreview `json:"automation,omitempty"`
}

// BlueprintProposalPreview is what a person sees before committing a
// blueprint_save proposal: the unified diff (proposed against current,
// labels included), whether the bytes are equal (a commit would only flip
// the row), the report the save door would render (json.RawMessage — the
// Report's own bytes verbatim, no paraphrase), and the policy lint
// advisories with their dated reasons (or the explicit unavailable
// sentence, never a silent all-clear).
type BlueprintProposalPreview struct {
	Name        string              `json:"name"`
	IsNew       bool                `json:"isNew"`
	Diff        string              `json:"diff"`
	Equal       bool                `json:"equal"`
	Report      json.RawMessage     `json:"report,omitempty"`
	Advisories  []BlueprintAdvisory `json:"advisories"`
	EvaluatedAt string              `json:"evaluatedAt,omitempty"`
	Unavailable string              `json:"unavailable,omitempty"`
}

// AutomationProposalPreview is S49 T6's preview for kind
// automation_disable — what the person sees before committing the ask to
// switch one automation off: the automation's AN5 Describe sentence (the
// one sentence the unified desk prints, never a paraphrase), its CURRENT
// state (Enabled/Paused/Archived — a person commits a disable of a row that
// moved since the ask only knowingly), its last five fires in the log's own
// words (newest first — what the automation has been answering lately is
// the argument for or against the ask), and the health verdict the desk
// renders beside the row (nil when the verdict cannot be computed —
// unknown, never falsely healthy). Computed READ-ONLY by the daemon's read
// service through the AutomationPreview seam; it gates nothing (AA4), and a
// read failure omits it rather than failing the read of the row.
type AutomationProposalPreview struct {
	Source    string                `json:"source"`
	ID        string                `json:"id"`
	Describe  string                `json:"describe"`
	Enabled   bool                  `json:"enabled"`
	Paused    bool                  `json:"paused,omitempty"`
	Archived  bool                  `json:"archived,omitempty"`
	LastFires []AutomationFireView  `json:"lastFires"`
	Health    *AutomationHealthView `json:"health,omitempty"`
}

// ProposalCommitParams is PM7.1 made wire: the id and the digest
// proposal.get returned. There is no digest-less commit.
type ProposalCommitParams struct {
	ID     int64  `json:"id"`
	Digest string `json:"digest"`
}

// ProposalCommitResult is the commit door's answer: the row after the flip,
// whether THIS call applied the effect (false = the crash-recovery
// completion — the effect had already landed), the target's in-force answer
// before and after — the SAME InForceView the T4 authoring results carry
// (Render() bytes verbatim, D14; empty for kinds with no policy target) —
// and the advisory authoring warnings the CLI prints on stderr (DEF-038's
// unmatchable-target, SEC-014's broad-allow) carried as data.
type ProposalCommitResult struct {
	Proposal      ProposalRow `json:"proposal"`
	EffectApplied bool        `json:"effectApplied"`
	Before        InForceView `json:"before"`
	After         InForceView `json:"after"`
	Warnings      []string    `json:"warnings"`
}

// ProposalRejectParams: the id and its REQUIRED reason (TD5's stricter
// half — a reject must say why; ≤ 512 bytes, the audit column's budget).
type ProposalRejectParams struct {
	ID     int64  `json:"id"`
	Reason string `json:"reason"`
}

// ProposalRejectResult: the row, post-decision. No effect ran — there is
// nothing else to answer.
type ProposalRejectResult struct {
	Proposal ProposalRow `json:"proposal"`
}

// --- registration --------------------------------------------------------------

// RegisterProposalDecider registers the three decision doors when (and only
// when) the seam is wired — the RegisterSeed/RegisterPolicyAuthoring
// pattern (V13): a nil seam is the configuration speaking, and the methods
// are ABSENT, not registered-and-refusing. RequiresInitialize true; Debug
// false (product surface — PM10's negative guards own the narrow
// projection's exclusion, never an accident). Returns whether the doors are
// in the table, so the daemon's wiring is asserted, not trusted.
func (t *Table) RegisterProposalDecider() bool {
	if t.ProposalDecider == nil {
		return false
	}
	t.register(&Method{Name: MethodProposalGet, RequiresInitialize: true, Handle: t.handleProposalGet})
	t.register(&Method{Name: MethodProposalCommit, RequiresInitialize: true, Handle: t.handleProposalCommit})
	t.register(&Method{Name: MethodProposalReject, RequiresInitialize: true, Handle: t.handleProposalReject})
	return true
}

func (t *Table) requireProposalDecider() (ProposalDecider, error) {
	if t.ProposalDecider == nil {
		return nil, ErrNotWired.WithWhat(
			"proposal decisions are not wired — `aeon serve` wires Table.ProposalDecider whenever a store exists; a hand-built Table has no decision door")
	}
	return t.ProposalDecider, nil
}

// decisionActor is authoringActor's discipline on the decision doors —
// reused, not restated: the same three ErrPolicyNoActor refusals (no seam,
// empty id, uid 0), the same kernel-credentialed stamp.
func (t *Table) decisionActor(ctx context.Context, sess *Session) (Actor, error) {
	return t.authoringActor(ctx, sess)
}

// --- proposal.get ---------------------------------------------------------------

func (t *Table) handleProposalGet(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	svc, err := t.requireProposalDecider()
	if err != nil {
		return nil, err
	}
	if err := rejectIdentityKeys(string(MethodProposalGet), params); err != nil {
		return nil, err
	}
	var p ProposalGetParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if p.ID <= 0 {
		return nil, ErrProposalDecideRefused.WithWhat("proposal.get needs a positive id")
	}
	row, found, err := svc.GetProposal(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrProposalDecideRefused.WithWhat("proposal %d does not exist", p.ID)
	}
	// The row the seam returned already carries Command (and S37's agent_import
	// Preview); the digest is the seam's — one desk function, no second
	// canonicaliser here.
	row.Command = commandFor(row.ID) // the same spelling proposal.list fills (one definition)
	out := ProposalGetResult{Proposal: row, Digest: svc.ProposalDigest(string(row.Payload))}
	out.Preview = t.proposalPreview(ctx, row)
	if row.Kind == "blueprint_save" {
		// S48 T9: the design revision previews through the daemon's read
		// service (the same Diff/Analyze/Lint the read doors answer with,
		// over this row's payload). Advisory: an absent preview (unwired
		// service, unreadable payload) omits the field, never fails the
		// read of the row.
		if t.BlueprintPreview != nil {
			if bp, refusal, err := t.BlueprintPreview(ctx, row.Payload); err == nil && refusal == "" {
				if out.Preview == nil {
					out.Preview = &ProposalPreview{Kind: row.Kind}
				}
				out.Preview.Blueprint = bp
			}
		}
	} else if out.Preview == nil && row.Kind == "agent_import" {
		// S45 T10: the kind the generic preview says nothing about carries
		// S37's preview instead — the commit confirm and the card read this.
		out.Preview = AgentImportGetPreview(ctx, t.AgentImportPreview, row.Payload)
	} else if row.Kind == "automation_disable" {
		// S49 T6: the disable ask previews the automation it names — the
		// Describe sentence, the current state, the last five fires and the
		// health verdict, through the daemon's read service (the same
		// reads the desk answers with, over this row's payload). Advisory:
		// an absent preview (unwired service, unreadable payload) omits
		// the field, never fails the read of the row.
		if t.AutomationPreview != nil {
			if ap, refusal, err := t.AutomationPreview(ctx, row.Payload); err == nil && refusal == "" {
				if out.Preview == nil {
					out.Preview = &ProposalPreview{Kind: row.Kind}
				}
				out.Preview.Automation = ap
			}
		}
	}
	return out, nil
}

// proposalPreview computes the advisory preview READ-ONLY (Behaviour: nothing
// is written here — the same Table.PolicyStore handle policy.inForce reads
// through). A read failure omits the preview rather than failing the read of
// the row: the row and its digest are the answer; the preview is the
// courtesy. It NEVER gates a commit (S37 AA4).
func (t *Table) proposalPreview(ctx context.Context, row ProposalRow) *ProposalPreview {
	return ProposalPreviewAnswer(ctx, t.PolicyStore, row, now())
}

// ProposalPreviewAnswer is the advisory preview exported (S45 T8, PM4): the
// CLI's offline direct door renders THE SAME preview the socket handler
// attaches to proposal.get — one implementation, two doors, rather than a
// second copy of the kinds a preview speaks for. Same nil-store silence,
// same advisory status: it gates nothing.
func ProposalPreviewAnswer(ctx context.Context, ps *policy.Store, row ProposalRow, instant time.Time) *ProposalPreview {
	if ps == nil {
		return nil
	}
	switch row.Kind {
	case "policy_add":
		var pol policy.Policy
		if err := json.Unmarshal(row.Payload, &pol); err != nil {
			return nil
		}
		tgt, rej := policy.ParseTarget(pol.Target.Kind, pol.Target.ID)
		if rej != nil {
			return nil
		}
		return &ProposalPreview{
			Kind:     row.Kind,
			Target:   tgt.String(),
			Describe: policy.Describe(pol),
			InForce:  previewInForceFor(ctx, ps, tgt, instant),
		}
	case "policy_remove":
		var p struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(row.Payload, &p); err != nil || p.ID == "" {
			return nil
		}
		pol, ok := previewRowFor(ps, p.ID, instant)
		if !ok {
			return &ProposalPreview{
				Kind:    row.Kind,
				InForce: []PolicyInForceRow{},
				Note:    fmt.Sprintf("policy %s is already gone — this commit would flip the row and change nothing", p.ID),
			}
		}
		return &ProposalPreview{
			Kind:     row.Kind,
			Target:   pol.Target.String(),
			Describe: policy.Describe(pol),
			InForce:  previewInForceFor(ctx, ps, pol.Target, instant),
		}
	case "seed_corpus":
		var p struct {
			Agents []string `json:"agents"`
		}
		if err := json.Unmarshal(row.Payload, &p); err != nil {
			return nil
		}
		return &ProposalPreview{
			Kind:    row.Kind,
			Agents:  p.Agents,
			InForce: []PolicyInForceRow{},
			Note:    "the seed run applies the policy.corpus set for these agents through the seed engine (via:explicit); policy.seedCorpus with dryRun is the per-rule preview",
		}
	default:
		return nil
	}
}

// previewRowFor reads one policy row for the preview (the boundary's own list,
// filtered — the same bridge policy.get reads through). Not found ⇒ false,
// which the preview renders honestly (already gone, commit would only flip).
func previewRowFor(ps *policy.Store, id string, instant time.Time) (policy.Policy, bool) {
	rows, rej := ps.ListDetailed(context.Background(), instant)
	if rej != nil {
		return policy.Policy{}, false
	}
	for _, l := range rows {
		if l.Policy.ID == id {
			return l.Policy, true
		}
	}
	return policy.Policy{}, false
}

// previewInForceFor answers the target through the same handle policy.inForce
// uses; a failed read answers the empty set (advisory), never a wrong
// sentence — the rows are Render() bytes verbatim (D14).
func previewInForceFor(ctx context.Context, ps *policy.Store, target policy.Target, instant time.Time) []PolicyInForceRow {
	answers, rej := ps.InForceFor(ctx, target, instant)
	if rej != nil {
		return []PolicyInForceRow{}
	}
	rows := make([]PolicyInForceRow, 0, len(answers))
	for _, a := range answers {
		rows = append(rows, PolicyInForceRow{
			PolicyID: a.Policy.ID,
			State:    string(a.State),
			Render:   a.Render(),
		})
	}
	return rows
}

// --- S45 T10: the agent_import evidence ---------------------------------------

// AgentImportRenderer is the AgentImportPreview seam's shape (S37's ONE
// preview renderer, composed in cmd/aeon — protocol never imports the plugin
// layer). The Table field carries it; the doors receive it as a value.
type AgentImportRenderer func(ctx context.Context, pkgPath string) (preview *AgentImportPreview, refusal string, err error)

// agentImportPkgPath reads the ONE payload field (AA2's shape). Undecodable
// ⇒ "" — the evidence is advisory (AA4), so an unreadable path answers
// silence, never a failed decision.
func agentImportPkgPath(payload []byte) string {
	var p agentImportPayload
	if json.Unmarshal(payload, &p) != nil {
		return ""
	}
	return p.PkgPath
}

// AgentImportBeforeView renders the BEFORE answer of an agent_import commit
// (S45 T10, PM7.2): what the package would bring, rendered by S37's ONE
// preview renderer — the same AgentImportPreview the create door carried,
// its consent bytes VERBATIM (never reformatted, never re-rhymed). Render it
// BEFORE the effect runs, at the door. No seam, no path, refusal or failure
// ⇒ the empty view: absence, never an invented sentence (D9). The returned
// agent name is the renderer's own word; the door hands it back to
// AgentImportAfterView.
func AgentImportBeforeView(ctx context.Context, render AgentImportRenderer, payload []byte) (agent string, before InForceView) {
	before = InForceView{Answers: []PolicyInForceRow{}}
	if render == nil {
		return "", before
	}
	pkg := agentImportPkgPath(payload)
	if pkg == "" {
		return "", before
	}
	prev, refusal, err := render(ctx, pkg)
	if prev == nil || refusal != "" || err != nil || prev.Agent == "" {
		return "", before
	}
	return prev.Agent, InForceView{
		Target:  "agent:" + prev.Agent,
		Answers: []PolicyInForceRow{{PolicyID: prev.Agent, Render: prev.Consent}},
	}
}

// AgentImportAfterView renders the AFTER answer of an agent_import commit:
// what exists once the import landed, rendered by the import body ITSELF —
// the consent re-rendered from the fresh read (AA3) plus the AP2 roster line
// ("imported X as DISABLED …"), captured at the door's Apply writer. That
// capture is the same renderer re-run post-import; re-rendering the create
// preview from the door would only copy a stale view, so the door ships the
// body's own print instead (the T10 ruling, recorded in the task Notes).
// An empty print — the crash-recovery completion, where Apply never ran —
// answers the empty view: the door with no evidence says nothing.
func AgentImportAfterView(agent, importedPrint string) InForceView {
	printed := strings.TrimSpace(importedPrint)
	if agent == "" || printed == "" {
		return InForceView{Answers: []PolicyInForceRow{}}
	}
	return InForceView{
		Target:  "agent:" + agent,
		Answers: []PolicyInForceRow{{PolicyID: agent, Render: printed}},
	}
}

// AgentImportGetPreview is the proposal.get preview for an agent_import row
// (S45 T10): S37's AgentImportPreview through its one renderer, the consent
// carried as DATA in the door's own preview slot — the card confirms the
// commit against exactly the text the person reads. Advisory (AA4): no
// seam, no path, refusal or failure ⇒ nil, and the get answers the row and
// its digest alone.
func AgentImportGetPreview(ctx context.Context, render AgentImportRenderer, payload []byte) *ProposalPreview {
	if render == nil {
		return nil
	}
	pkg := agentImportPkgPath(payload)
	if pkg == "" {
		return nil
	}
	prev, refusal, err := render(ctx, pkg)
	if prev == nil || refusal != "" || err != nil {
		return nil
	}
	return &ProposalPreview{Kind: "agent_import", Describe: prev.Consent, InForce: []PolicyInForceRow{}}
}

// --- proposal.commit ------------------------------------------------------------

func (t *Table) handleProposalCommit(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	svc, err := t.requireProposalDecider()
	if err != nil {
		return nil, err
	}
	if err := rejectIdentityKeys(string(MethodProposalCommit), params); err != nil {
		return nil, err
	}
	actor, err := t.decisionActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	var p ProposalCommitParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if p.ID <= 0 {
		return nil, ErrProposalDecideRefused.WithWhat("proposal.commit needs a positive id")
	}
	if strings.TrimSpace(p.Digest) == "" {
		return nil, ErrProposalDecideRefused.WithWhat(
			"proposal.commit requires the digest proposal.get returned — a commit not bound to what you read commits nothing (PM7.1)")
	}
	res, err := svc.CommitProposal(ctx, actor, p.ID, p.Digest)
	if err != nil {
		return nil, decisionRefusal(err)
	}
	res.Proposal.Command = commandFor(res.Proposal.ID)
	if res.Warnings == nil {
		res.Warnings = []string{}
	}
	if res.Before.Answers == nil {
		res.Before.Answers = []PolicyInForceRow{}
	}
	if res.After.Answers == nil {
		res.After.Answers = []PolicyInForceRow{}
	}
	return res, nil
}

// --- proposal.reject ------------------------------------------------------------

func (t *Table) handleProposalReject(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	svc, err := t.requireProposalDecider()
	if err != nil {
		return nil, err
	}
	if err := rejectIdentityKeys(string(MethodProposalReject), params); err != nil {
		return nil, err
	}
	actor, err := t.decisionActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	var p ProposalRejectParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if p.ID <= 0 {
		return nil, ErrProposalDecideRefused.WithWhat("proposal.reject needs a positive id")
	}
	if strings.TrimSpace(p.Reason) == "" {
		return nil, ErrProposalDecideRefused.WithWhat(
			"proposal.reject requires a reason — say why the proposal is refused (TD5: the audit trail's only sentence for the decision)")
	}
	if len(p.Reason) > 512 {
		return nil, ErrProposalDecideRefused.WithWhat(
			"proposal.reject reason must be at most 512 bytes, got %d", len(p.Reason))
	}
	row, err := svc.RejectProposal(ctx, actor, p.ID, p.Reason)
	if err != nil {
		return nil, decisionRefusal(err)
	}
	row.Command = commandFor(row.ID)
	return ProposalRejectResult{Proposal: row}, nil
}

// decisionRefusal maps the service's PLAIN errors to the refusal the door
// presents: the sentence VERBATIM in `what` (policyRefusal's rule, carried
// onto decisions). A *Error from the service — the infrastructure class the
// adapter types itself — travels unchanged (Dispatch's internal-error
// path), because "the store broke" is not a decision anybody refused.
func decisionRefusal(err error) error {
	if e, ok := err.(*Error); ok {
		return e
	}
	return ErrProposalDecideRefused.WithWhat("%s", err.Error())
}
