package desk

// ════════════════════════════════════════════════════════════════════════════
// The decision orchestration (S45 T5, PM7/PM8): Commit and Reject — the exact
// step order S35 TD4 specified for `aeon proposal commit`/`reject`, MOVED here
// unchanged so the CLI door and the socket door (internal/daemon, T5) run the
// one implementation (PM4). The daemon cannot import cmd/aeon, which is why
// this file exists at all; the CLI becomes a caller of it (cmd/aeon/
// proposal.go, T5), printing and flag-parsing staying there.
//
// # The step order (S35 TD1/TD4, PM7.4 — semantics moved, not redesigned)
//
// Commit: load row → digest verify (PM7.1: the commit is bound to what was
// READ — sha256 of the canonical payload; a mismatch refuses "the proposal
// changed since you read it") → status gate (a decided row is immutable
// history, TD1; the refusal names the winner's stamp) → AlreadyApplied → if not applied: Validate
// (failure ⇒ SetProposalAppliedError, row STAYS proposed, the refusal names
// the reason) → capture the target's BEFORE answer → Apply (failure ⇒
// applied_error, row STAYS proposed — TD4's retry path) → capture AFTER →
// DecideProposal (decideGuarded's claimed-FROM WHERE is the single-writer
// claim) → the policy_change audit row (PM8, op='commit', ids-only summary,
// via the T3 store accessor) — EXCEPT for `agent_import` (S45 T10): that
// table is the policy AUTHORING audit (T0's vocabulary names no agent_import
// op, and an agent import authors no policy row of its own beyond what the
// import body carries), so its decision is recorded in the proposal row's
// own decided_by_* fields and nowhere else.
//
// Reject: row must be `proposed`; reason REQUIRED, non-empty, ≤ 512 bytes
// (TD5's stricter half — a refusal to widen may be unreasoned, an approval
// never is); DecideProposal; policy_change (op='reject'). A reject applies NO
// effect — the registry is never consulted.
//
// # Concurrency (the crash-safety S35 chose)
//
// Two simultaneous commits of one id have exactly one winner because the
// flip is `update … where status='proposed'`: the loser's flip changes zero
// rows and refuses naming the winner. The effect does not apply twice
// because the loser re-reads the row first (its status gate fires against
// the committed row) and, in the narrower window where both read `proposed`,
// the loser's flip loses at the WHERE clause. The desk adds no second lock —
// the store's single-writer claim IS the guard (asserted by
// TestConcurrentCommitsApplyTheEffectOnce under -race).
//
// # What reaches the store
//
// `DecideDeps.Store` is the three accessors a decision writes (flip,
// applied_error, audit row) and `DecideDeps.Get` the single-row read —
// seams the two doors fill with their own *store.Store handle, the same
// shape as Deps.Boundary above: the desk names the store's TYPES through
// these seams only and opens nothing itself (the PL15 pin, carried).
// ════════════════════════════════════════════════════════════════════════════

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/policy"
)

// Actor is the deciding person: the OL7 actor pair the store's decided_by_*
// CHECKs admit only as {kind:"user", non-empty id} (T0 item 3.4). The CLI
// fills {user, "cli"}; the socket door fills {user, <peer uid>} from the
// kernel's peer credential via table.ActorFor — never from params, and never
// a placeholder (PM7.3).
type Actor struct {
	Kind string
	ID   string
}

// Row is the proposal the decision operates on — the store row as fields, so
// the doors map to their own wire shape freely (protocol never imports the
// store; the daemon adapter maps this to protocol.ProposalRow).
type Row struct {
	ID            int64
	Kind          string
	Payload       string
	Status        string
	CreatedAt     string
	CreatedByKind string
	CreatedByID   string
	DecidedAt     string
	DecidedByKind string
	DecidedByID   string
	DecideReason  string
	AppliedError  string
}

// ActorPair is the store's actor token spelled locally: the desk names the
// SHAPE the flip carries (kind/id, the schema's decided_by_* pair) without
// importing internal/store — the desk's dependency rule (its own
// TestDeskImportsNothingForbidden) keeps the store out of this package, and
// a consumer-side mirror is exactly how the Boundary seam above already
// reaches the authoring door. Both doors hand their own *store.Store to
// Store below; Go's structural typing checks the shapes at the call site.
type ActorPair struct {
	Kind string
	ID   string
}

// PolicyChangeRow is the audit row the decision writes — the store's
// PolicyChange fields, mirrored for the same import rule. A door's real
// *store.Store satisfies Store with its own types.
type PolicyChangeRow struct {
	Op         string
	PolicyID   string
	ProposalID *int64
	ActorKind  string
	ActorID    string
	Summary    string
	OccurredAt string
}

// Store is the write half of the store as the decision orchestration needs
// it: exactly the three accessors a decision performs, nothing else. Both
// doors pass their own *store.Store (the methods exist on it), so this is a
// capability narrowing, not a second implementation.
type Store interface {
	DecideProposal(id int64, status string, by ActorPair, reason, at string) error
	SetProposalAppliedError(id int64, msg string) error
	PolicyChangeInsert(ctx context.Context, c PolicyChangeRow) error
}

// DecideDeps are what a decision needs from its caller beyond the Registry's
// effect table.
type DecideDeps struct {
	// Get reads the row by id (found=false ⇒ the honest "does not exist"
	// refusal). Usually store.GetProposal bound to the caller's handle.
	Get func(id int64) (Row, bool, error)

	// Store is the write half (flip, applied_error, audit). The daemon and
	// the CLI adapt their *store.Store to it in one place (their door
	// files); the desk writes through the seam only.
	Store Store

	// InForce answers one target's current in-force sentences for the
	// before/after pair (PM7.2). The daemon hands policy.NewStore(s).InForce
	// answers mapped to strings; the CLI the same over its opened store. The
	// desk assembles NOTHING: whatever the store renders travels verbatim
	// (D14). An error is infrastructure — the door maps it, never a
	// fabricated answer.
	InForce func(ctx context.Context, target string) ([]InForceRow, error)

	// RemoveTarget answers a policy_remove proposal's target by reading the
	// row it names BEFORE the effect runs (the last moment it exists). nil
	// ⇒ the remove kinds carry an empty before/after, never a wrong target.
	RemoveTarget func(ctx context.Context, policyID string) (string, error)

	// Now stamps decided_at/occurred_at; nil means time.Now (D5: the desk
	// reads the clock it was given, never invents one).
	Now func() time.Time

	// ReasonRequired makes Reject's reason mandatory — the SOCKET door's
	// ruling (TD5's stricter half, PM7.5: the door a person faces through
	// the app states the reason or the reject does not happen). The CLI
	// leaves it false: TD5's CLI ruling ("rejecting loosens nothing, so no
	// reason is required") is golden-pinned bytes since S35, and the
	// ≤512-byte cap guards the audit column at BOTH doors regardless.
	ReasonRequired bool

	// ApplyWriter is where an effect's human-facing print goes (S45 T10):
	// `agent_import`'s Apply re-reads the package and renders its consent
	// to this writer (S37's AA4 — the text a person decides against). The
	// CLI door hands its own output writer; the daemon door captures into
	// a buffer and ships the text as DATA on the result (a daemon must
	// never stream to a terminal). nil ⇒ io.Discard: an unwired writer
	// silences the print, it never fails an otherwise-good commit.
	ApplyWriter io.Writer
}

// InForceRow is one rule's in-force answer as the store RENDERED it — the
// protocol door maps it 1:1 to its PolicyInForceRow (D14: Render() bytes,
// nothing re-assembled on the way).
type InForceRow struct {
	PolicyID string `json:"policyID"`
	State    string `json:"state"`
	Render   string `json:"render"`
}

// InForceView is one target's in-force answer — the same {target, answers}
// shape the authoring doors carry (D14: answers are Render() sentences,
// nothing re-assembled here).
type InForceView struct {
	Target  string       `json:"target"`
	Answers []InForceRow `json:"answers"`
}

// CommitResult is what the commit door answers. EffectApplied is false on
// the crash-recovery path (the effect had already landed; this commit
// completed the flip) — the distinction PM7.4 keeps honest. Before/After are
// the target's in-force Render() sentences either side of the effect (empty
// for kinds with no policy target); they travel as the store rendered them
// (D14). Row is the post-decision row as the doors present it.
type CommitResult struct {
	Row           Row         `json:"proposal"`
	EffectApplied bool        `json:"effectApplied"`
	Before        InForceView `json:"before"`
	After         InForceView `json:"after"`
	Warnings      []string    `json:"warnings"`
}

// RejectResult is the reject door's answer: the row, post-decision. No
// effect, no before/after — a reject wrote nothing to decide about.
type RejectResult struct {
	Row Row `json:"proposal"`
}

// MaxRejectReasonBytes is TD5's discipline made concrete: a reject's reason
// is required, non-empty, and bounded — the audit column's budget.
const MaxRejectReasonBytes = 512

// ProposalPayloadDigest is PM7.1's binding: sha256 over the CANONICAL form
// of the payload JSON (re-marshalled through encoding/json, which sorts
// object keys — so a re-read of the same content always digests identically
// across key orderings, and a payload that is not valid JSON digests its raw
// bytes rather than failing closed, which would make a malformed row
// undecidable-by-construction). Pure: no store, no clock, no I/O.
// proposal.get returns it; proposal.commit demands it back and a mismatch
// refuses naming the change, never silently re-reading.
func ProposalPayloadDigest(payload string) string {
	canonical := payload
	var v any
	if json.Unmarshal([]byte(payload), &v) == nil {
		if b, err := json.Marshal(v); err == nil {
			canonical = string(b)
		}
	}
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])
}

// Commit decides a proposal by applying its effect through the registry and
// flipping the row — the exact order in this file's header. Refusals are
// plain errors carrying the sentence the doors must present (the protocol
// door wraps them as ErrProposalDecideRefused's reason VERBATIM; the CLI
// prints them); a store/infrastructure failure travels as its own error and
// the doors treat it as internal, never as a refusal.
func Commit(ctx context.Context, d DecideDeps, reg *Registry, id int64, digest string, by Actor) (CommitResult, error) {
	if d.Get == nil || d.Store == nil || reg == nil {
		return CommitResult{}, errors.New("refused: the commit door is not wired (no row read, no store half, no effect registry)")
	}
	if by.Kind != "user" || by.ID == "" {
		return CommitResult{}, fmt.Errorf("refused: a commit needs the deciding person as {user, <id>}, got {%q, %q}", by.Kind, by.ID)
	}
	if digest == "" {
		return CommitResult{}, errors.New("refused: commit requires the digest proposal.get returned — a commit not bound to what was read commits nothing (PM7.1)")
	}
	now := time.Now
	if d.Now != nil {
		now = d.Now
	}
	at := now().UTC().Format(time.RFC3339Nano)

	row, found, err := d.Get(id)
	if err != nil {
		return CommitResult{}, err
	}
	if !found {
		return CommitResult{}, fmt.Errorf("refused: proposal %d does not exist", id)
	}
	// PM7.1 first: the digest binds the decision to what the caller READ,
	// verified before any refusal that quotes row content.
	if got := ProposalPayloadDigest(row.Payload); got != digest {
		return CommitResult{}, fmt.Errorf("refused: the proposal changed since you read it — its digest is %s, the commit carried %s", got, digest)
	}
	// TD1: a decided row is immutable history; the refusal carries the
	// winner's own stamp, re-read from the row (never re-stamped here).
	if err := decidedRefusal("commit", row); err != nil {
		return CommitResult{}, err
	}
	// S45 T10: `agent_import` is committed like every other kind — the
	// by-name refusal T5 left here until the socket door carried the
	// effect is gone. The kind's Validate/AlreadyApplied/Apply arrive
	// through the registry exactly as for the MCP kinds; the consent print
	// reaches the door through DecideDeps.ApplyWriter.
	if _, ok := reg.Effect(row.Kind); !ok {
		return CommitResult{}, fmt.Errorf("refused: %s", reg.Missing(row.Kind))
	}

	warnings := warningsFor(row.Kind, row.Payload)

	// TD4: the idempotence gate BEFORE re-validation — an effect that
	// already landed skips to the flip, so a crash between "applied" and
	// "flipped" completes on retry without reapplying (EffectApplied:false
	// says so, and the flip below stays the one write that moves the row).
	already, err := reg.AlreadyApplied(ctx, row.Kind, row.Payload)
	if err != nil {
		return CommitResult{}, fmt.Errorf("check whether %s's effect already landed: %w", row.Kind, err)
	}
	var target string
	if kindsWithTarget[Kind(row.Kind)] && !already {
		if Kind(row.Kind) == KindPolicyRemove {
			if d.RemoveTarget != nil {
				var p PolicyRemovePayload
				if json.Unmarshal([]byte(row.Payload), &p) != nil {
					return CommitResult{}, fmt.Errorf("refused: proposal %d's payload is not a readable policy_remove — %s", id, row.Payload)
				}
				t, terr := d.RemoveTarget(ctx, p.ID)
				if terr != nil {
					return CommitResult{}, terr
				}
				target = t
			}
		} else {
			t, terr := policyAddTarget(row.Payload)
			if terr != nil {
				return CommitResult{}, fmt.Errorf("refused: proposal %d names no readable target — %s", id, terr)
			}
			target = t
		}
	}
	var before, after InForceView
	if !already {
		// TD3: the SAME validator the create door ran, re-run at commit —
		// the preview is advisory, the commit trusts nothing read earlier
		// (S37 AA4). A validation failure records applied_error and leaves
		// the row proposed for a human to reconsider (S35 test map #5).
		if verr := reg.ValidateProposal(ctx, row.Kind, row.Payload); verr != nil {
			_ = d.Store.SetProposalAppliedError(id, verr.Error())
			return CommitResult{}, fmt.Errorf("refused: proposal %d failed re-validation at commit — %s (nothing was applied; the row stays proposed for a human to reconsider)", id, verr.Error())
		}
		if target != "" {
			b, berr := d.inForce(ctx, target)
			if berr != nil {
				return CommitResult{}, berr
			}
			before = b
		}
		// S56 T8: the identity the effect stamps arrives on the context —
		// the committer (the actor at this door) and the proposal's author
		// (who filed it). ONE injection here covers both commit doors,
		// because both run through this body; the autonomy Apply halves
		// read it with DeciderFrom and fail closed if it is absent.
		ctx = WithDecider(ctx, Decider{Kind: by.Kind, ID: by.ID, AuthorKind: row.CreatedByKind, AuthorID: row.CreatedByID})
		// S46 T6: the effects that write audit rows (the MCP doors) stamp
		// the COMMITTING person, never the proposer. The channel carries
		// the actor Commit already holds; an effect that cannot find it
		// refuses outright — a placeholder actor is forbidden (T5's law,
		// carried through the desk). S56's merge amended this line to
		// carry BOTH injections (the S46 T6 record's planned follow-up).
		// S59 T7 adds the third: the id of the row being committed, so an
		// effect that writes a change audit can name the proposal behind
		// it instead of guessing (proposal_ctx.go; fail-closed like the
		// other two).
		if aerr := reg.Apply(WithProposalID(WithCommitActor(WithDecider(ctx, Decider{Kind: by.Kind, ID: by.ID, AuthorKind: row.CreatedByKind, AuthorID: row.CreatedByID}), by), id), d.applyWriter(), row.Kind, row.Payload); aerr != nil {
			_ = d.Store.SetProposalAppliedError(id, aerr.Error())
			return CommitResult{}, fmt.Errorf("refused: proposal %d's effect could not be applied — %s (the row stays proposed for retry)", id, aerr.Error())
		}
		if target != "" {
			a, aerr := d.inForce(ctx, target)
			if aerr != nil {
				return CommitResult{}, aerr
			}
			after = a
		}
	}

	// The flip — decideGuarded's claimed-FROM WHERE. Zero rows changed means
	// someone else won between the re-read above and here; the refusal names
	// them from the row they decided.
	if err := d.Store.DecideProposal(id, "committed", ActorPair{Kind: by.Kind, ID: by.ID}, "", at); err != nil {
		return CommitResult{}, winnerRefusal(err, d, id)
	}

	policyID := auditTargetOf(row.Kind, row.Payload)
	// PM8, amended by S45 T10: the policy_change table is the policy
	// AUTHORING audit — its op vocabulary (T0/T3: add · remove · seed ·
	// commit · reject over policy rows) has no agent_import op, and an
	// agent import is not policy authoring. So `agent_import` writes NO
	// audit row here: the decision lives, completely, in the proposal
	// row's own decided_by_* fields (the row the flip just stamped), and
	// the import body's ledger rows are the agent's audit (S15/S43). The
	// kind is detected the same way the door's step order names it.
	if Kind(row.Kind) != KindAgentImport {
		if aerr := d.Store.PolicyChangeInsert(ctx, PolicyChangeRow{
			Op:         "commit",
			PolicyID:   policyID,
			ProposalID: &id,
			ActorKind:  by.Kind,
			ActorID:    by.ID,
			Summary:    fmt.Sprintf("committed %s proposal %d", row.Kind, id),
			OccurredAt: at,
		}); aerr != nil {
			// The decision stands; the audit gap is the gap Behaviour 6 (T4) is
			// allowed to NAME, never hide — the same rule the authoring doors
			// carry. The flip and the effect are the truth; the audit row is the
			// bookkeeping, and a lost audit never turns a committed row into a
			// lie.
			_ = aerr
		}
	}

	out, found, err := d.Get(id)
	if err != nil {
		return CommitResult{}, err
	}
	if !found {
		out = row
		out.Status = "committed"
		out.DecidedAt, out.DecidedByKind, out.DecidedByID = at, by.Kind, by.ID
	}
	return CommitResult{
		Row:           out,
		EffectApplied: !already,
		Before:        before,
		After:         after,
		Warnings:      warnings,
	}, nil
}

// Reject decides a proposal as refused. Reason discipline follows the
// direction of risk (S35 TD5) — and the stricter rule applies HERE: the
// reason is required, non-empty, ≤ MaxRejectReasonBytes. Reject consults the
// effect registry for NOTHING: no Validate, no AlreadyApplied, no Apply.
// It flips the row and writes the audit row. That is all it does.
func Reject(ctx context.Context, d DecideDeps, id int64, reason string, by Actor) (RejectResult, error) {
	if d.Get == nil || d.Store == nil {
		return RejectResult{}, errors.New("refused: the reject door is not wired (no row read, no store half)")
	}
	if by.Kind != "user" || by.ID == "" {
		return RejectResult{}, fmt.Errorf("refused: a reject needs the deciding person as {user, <id>}, got {%q, %q}", by.Kind, by.ID)
	}
	if d.ReasonRequired && strings.TrimSpace(reason) == "" {
		return RejectResult{}, errors.New("refused: reject requires a reason — say why the proposal is refused (it is the audit trail's only sentence for a decision that widens nothing)")
	}
	if len(reason) > MaxRejectReasonBytes {
		return RejectResult{}, fmt.Errorf("refused: reject reason must be at most %d bytes, got %d", MaxRejectReasonBytes, len(reason))
	}
	now := time.Now
	if d.Now != nil {
		now = d.Now
	}
	at := now().UTC().Format(time.RFC3339Nano)

	row, found, err := d.Get(id)
	if err != nil {
		return RejectResult{}, err
	}
	if !found {
		return RejectResult{}, fmt.Errorf("refused: proposal %d does not exist", id)
	}
	if err := decidedRefusal("reject", row); err != nil {
		return RejectResult{}, err
	}

	if err := d.Store.DecideProposal(id, "rejected", ActorPair{Kind: by.Kind, ID: by.ID}, reason, at); err != nil {
		return RejectResult{}, winnerRefusal(err, d, id)
	}

	policyID := auditTargetOf(row.Kind, row.Payload)
	if aerr := d.Store.PolicyChangeInsert(ctx, PolicyChangeRow{
		Op:         "reject",
		PolicyID:   policyID,
		ProposalID: &id,
		ActorKind:  by.Kind,
		ActorID:    by.ID,
		Summary:    fmt.Sprintf("rejected %s proposal %d", row.Kind, id),
		OccurredAt: at,
	}); aerr != nil {
		_ = aerr // same rule as commit: the decision stands, the gap is nameable
	}

	out, found, err := d.Get(id)
	if err != nil {
		return RejectResult{}, err
	}
	if !found {
		out = row
		out.Status = "rejected"
		out.DecidedAt, out.DecidedByKind, out.DecidedByID, out.DecideReason = at, by.Kind, by.ID, reason
	}
	return RejectResult{Row: out}, nil
}

// decidedRefusal is TD1's sentence: a decided row is immutable history, and
// the refusal carries the winner's stamp exactly as the row records it.
func decidedRefusal(verb string, row Row) error {
	if row.Status == "proposed" {
		return nil
	}
	if row.Status == "committed" || row.Status == "rejected" {
		return fmt.Errorf("refused: proposal %d is already %s by %s:%s at %s", row.ID, row.Status, row.DecidedByKind, row.DecidedByID, row.DecidedAt)
	}
	return fmt.Errorf("refused: proposal %d is %s, not proposed — a decided row is immutable history", row.ID, row.Status)
}

// winnerRefusal maps a lost claimed-FROM flip onto the winner's identity.
// decideGuarded answers "refused: proposal N is <status>, not proposed" when
// the row moved under us; the re-read pulls the decided-by stamp so the
// sentence names who won. Any other error travels unchanged — it is
// infrastructure, not a refusal.
func winnerRefusal(err error, d DecideDeps, id int64) error {
	msg := err.Error()
	if strings.Contains(msg, "not proposed") || strings.Contains(msg, "already ") {
		if r, found, gerr := d.Get(id); gerr == nil && found {
			if r.Status == "committed" || r.Status == "rejected" {
				return fmt.Errorf("refused: proposal %d is already %s by %s:%s at %s", id, r.Status, r.DecidedByKind, r.DecidedByID, r.DecidedAt)
			}
		}
	}
	return err
}

// applyWriter is DecideDeps.ApplyWriter with its silence default: an effect's
// human-facing print (S37 AA4's consent) goes where the DOOR chose; an
// unwired door silences it rather than failing an otherwise-good commit.
func (d DecideDeps) applyWriter() io.Writer {
	if d.ApplyWriter != nil {
		return d.ApplyWriter
	}
	return io.Discard
}

// inForce answers one target with the caller's seam, assembling nothing
// (D14). A nil seam answers an empty view — doors without one (a harness)
// still commit; the pair is the confirmation, never the gate.
func (d DecideDeps) inForce(ctx context.Context, target string) (InForceView, error) {
	if d.InForce == nil {
		return InForceView{Target: target, Answers: []InForceRow{}}, nil
	}
	rows, err := d.InForce(ctx, target)
	if err != nil {
		return InForceView{}, err
	}
	if rows == nil {
		rows = []InForceRow{}
	}
	return InForceView{Target: target, Answers: rows}, nil
}

// kindsWithTarget names the kinds whose effect changes a policy target's
// in-force answer. The pair means nothing for the seed corpus (its effect
// spans many targets — the seed door's own answer is the confirmation), and
// nothing for the MCP/import kinds (no policy target exists).
var kindsWithTarget = map[Kind]bool{
	KindPolicyAdd:    true,
	KindPolicyRemove: true,
}

// policyAddTarget renders the affected target of a policy_add payload
// (already validated when this runs — called after ValidateProposal for the
// pair, and pre-gated here for the shape).
func policyAddTarget(payload string) (string, error) {
	var p policy.Policy
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		return "", err
	}
	t, rej := policy.ParseTarget(p.Target.Kind, p.Target.ID)
	if rej != nil {
		return "", errors.New(rej.Reason)
	}
	return t.String(), nil
}

// auditTargetOf names the policy row a decision of this kind is about (the
// audit's policy_id): policy_add is keyed by ITS OWN id (the row the effect
// writes); policy_remove by the row it removes; seed_corpus is a run naming
// no single rule (NULL policy_id — the same rule the T4 seed door's run row
// follows); the MCP and import kinds author no policy row at all.
func auditTargetOf(kind, payload string) string {
	switch Kind(kind) {
	case KindPolicyAdd:
		var p policy.Policy
		if json.Unmarshal([]byte(payload), &p) == nil && p.ID != "" {
			return p.ID
		}
		return ""
	case KindPolicyRemove:
		var p PolicyRemovePayload
		if json.Unmarshal([]byte(payload), &p) == nil && p.ID != "" {
			return p.ID
		}
		return ""
	default:
		return ""
	}
}

// warningsFor reproduces, at the decision door, the two advisory lines the
// CLI authoring verbs print on stderr (T4's warn.go): DEF-038's
// unmatchable-target line for a target kind that can never match yet, and
// SEC-014's broad-allow line. Advisory data only (S37 AA4) — a warning never
// blocks; carrying them on the commit result is what lets a human see the
// shape of what they approve. Pure, like both authors.
func warningsFor(kind, payload string) []string {
	if Kind(kind) != KindPolicyAdd {
		return nil
	}
	var p policy.Policy
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		return nil
	}
	var out []string
	if w := UnmatchableTargetWarn(string(p.Target.Kind)); w != "" {
		out = append(out, w)
	}
	if w, _ := BroadAllowWarning(p.Action, p.Spec); w != "" {
		out = append(out, w)
	}
	return out
}

// --- the commit actor channel (S46 T6) -------------------------------------------------

// commitActorKey carries the deciding person to effects that write their own
// audit rows (the MCP row doors, T5). It is set by Commit itself — never by a
// caller, never from a payload: the person who commits is the person the
// effect stamps, and the PROPOSER's identity stays where it belongs, in the
// row's created_by_*. An effect reading this channel must refuse when the
// stamp is absent; a placeholder actor is forbidden.
type commitActorKey struct{}

// WithCommitActor stamps the deciding person for effect implementations.
// Effects must not call it — Commit does, exactly once per commit.
func WithCommitActor(ctx context.Context, a Actor) context.Context {
	return context.WithValue(ctx, commitActorKey{}, a)
}

// CommitActor answers the deciding person when Commit stamped one.
func CommitActor(ctx context.Context) (Actor, bool) {
	a, ok := ctx.Value(commitActorKey{}).(Actor)
	return a, ok
}
