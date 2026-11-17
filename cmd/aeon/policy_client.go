package main

// policy_client.go — S45 T8 (PM4). The two doors onto the one policy
// implementation, and the selection between them.
//
// # One implementation, two doors (PM4, P34 — the seed_door.go pattern)
//
// `aeon policy …` and `aeon proposal …` are CLIENTS of the daemon's socket
// methods (the human door S45 T4/T5 put on the table). selectPolicyDoor
// dials once; a successful handshake speaks the methods over the socket
// (policySocketDoor); a missing endpoint runs the SAME internal/desk and
// internal/policy functions the daemon's services call, directly over the
// store (policyDirectDoor), saying so first on stderr. Any OTHER dial
// failure (permission denied on the endpoint, a handshake refusal, a version
// skew) REFUSES — it never falls back, because falling back from an error
// that is not "nobody is home" would route around a daemon that exists.
//
// Both doors answer through the protocol package's OWN answer functions
// (PolicyListAnswer, PolicyWhyAnswer, PolicyInForceAnswer, PolicyGetAnswer,
// ProposalPreviewAnswer, UnauditedPolicyIDs) over a boundary the direct door
// opens itself — so a byte printed offline is the byte the daemon would have
// printed, by construction, not by copy. The authoring half of the direct
// door goes through desk.Boundary (the desk's adapter interface, the same
// one the desk's own runners drive) and desk.Commit/desk.Reject — the CLI
// never holds a second implementation of anything (PM2).
//
// # The actor is the connection or the cli — never a param
//
// The socket door's actor is the kernel peer the daemon stamps from the
// connection (the CLI never sends one). The direct door's is the existing
// {user, cli} literal for decisions, and actor_kind='cli' on the authoring
// audit rows it pairs with its own writes (T3's ruling — the offline door
// names itself in the audit; it never impersonates a peer). Neither door
// reads an actor from a flag.
//
// # The CLI re-validates NOTHING
//
// Neither door checks policy. The socket door maps the wire's typed refusal
// to its `what` sentence VERBATIM (the boundary's own words, which is what
// the direct door's *policy.Reject prints too — one refusal, both doors,
// byte for byte); the direct door delegates every rule to the boundary and
// the desk, printing the same `reason` the boundary named. Flag parsing
// refuses only what cannot be a value; everything else is the boundary's
// refusal printed as it arrived.
//
// This file is the ONLY place the policy/proposal VERBS reach the store
// (openStoreDirectly, and openDeskBoundary via policy.go's seam variables),
// which is what TestCLIPolicyNeverImportsStoreOutsideTheFallback pins:
// policy.go and proposal.go keep their imports at parse and print; the
// offline open lives here.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// policyDoor is the two doors onto the one policy implementation. Both
// answer the SAME wire shapes (the protocol result types), so each verb has
// ONE presentation function and the printed bytes cannot depend on which
// door answered (the parity guard's claim).
type policyDoor interface {
	list(ctx context.Context, at time.Time) (protocol.PolicyListResult, error)
	get(ctx context.Context, id string, at time.Time) (protocol.PolicyDetail, error)
	add(ctx context.Context, p protocol.PolicyAddParams) (protocol.PolicyAddResult, error)
	remove(ctx context.Context, id string) (protocol.PolicyRemoveResult, error)
	// seedCorpus returns (result, printed, error): the direct door runs
	// desk.RunSeedCorpus, which PRINTS the run as it goes (T12's decision —
	// the desk runner keeps the bytes), and answers printed=true; the
	// socket door returns the wire counts and printed=false, and the verb
	// prints them through desk's own line builders. The bytes are the same
	// either way because the formatting lives in internal/desk alone.
	seedCorpus(ctx context.Context, w io.Writer, agents []string, dryRun bool) (protocol.SeedCorpusResult, bool, error)
	why(ctx context.Context, p protocol.PolicyWhyParams, fallback time.Time) (protocol.PolicyWhyResult, error)
	inForce(ctx context.Context, p protocol.PolicyInForceParams, at time.Time) (protocol.PolicyInForceResult, error)
	events(ctx context.Context, subject string, limit int, now time.Time) (policyEventsAnswer, error)
	changes(ctx context.Context, policyID string, limit int, now time.Time) (protocol.PolicyChangesResult, error)
	vocabulary(ctx context.Context) (policy.VocabularyView, error)
	proposalList(ctx context.Context, status string) ([]protocol.ProposalRow, error)
	proposalGet(ctx context.Context, id int64) (protocol.ProposalGetResult, bool, error)
	// proposalCommit carries the verb's writer (S45 T10, the seedCorpus
	// precedent): `agent_import`'s Apply prints its consent (S37 AA4) and
	// the DIRECT door streams it to the person's writer while capturing it
	// for the after view; the SOCKET door ignores it — the daemon captures
	// the print and ships it as data (a socket answer never streams).
	proposalCommit(ctx context.Context, id int64, digest string, w io.Writer) (protocol.ProposalCommitResult, error)
	proposalReject(ctx context.Context, id int64, reason string) (protocol.ProposalRow, error)
}

// policyEventsAnswer is the events door's answer: the wire rows plus the
// two lines only the door that HOLDS the store can speak — the on-demand
// trim (DEF-030: the reader is demand; the daemon trimmed at start) and the
// ad-hoc-actor unaudited probe. The socket door answers both empty: over
// the socket the daemon is the one running, its start-time trim already
// happened, and the probe line prints only when the boundary refuses the
// minted actor — in production the minted actor is valid, so the line is
// silence on both doors (policy_test.go's subtests drive both arms through
// runPolicyEvents directly, whose signature and behaviour are unchanged).
type policyEventsAnswer struct {
	Res           protocol.PolicyEventsResult
	TrimLine      string
	UnauditedLine string
}

// policyDial is the dial the selection uses; a var so a test can stand in a
// dial that refuses (a handshake fault) or answers "no endpoint", without
// touching the real endpoint. Production is the one dial(0).
var policyDial = dial

// policyDirectNotice is the fixed stderr line the offline door says BEFORE
// any output (PM4's flagged honesty: three real cases have no daemon —
// first-run seeding, a broken daemon, and make tooling — and the person
// must never wonder which door answered).
const policyDirectNotice = "daemon not running — applied directly to the store"

// selectPolicyDoor is the selection (PM4): dial; a success is the socket
// door, a "no endpoint" is the direct door (with the fixed line on errOut
// before any output), and any other dial failure is a REFUSAL — never a
// fallback. A daemon that exists and disagrees is a state the operator sees,
// not one the CLI routes around.
func selectPolicyDoor(errOut io.Writer) (policyDoor, error) {
	c, err := policyDial(0)
	if err == nil {
		return &policySocketDoor{c: c}, nil
	}
	if isNoDaemon(err) {
		fmt.Fprintln(errOut, policyDirectNotice)
		return &policyDirectDoor{}, nil
	}
	return nil, err
}

// policyVerbatim maps a wire refusal to its `what` sentence VERBATIM — the
// boundary's own words are the refusal (policyRefusal,
// ErrPolicyAuthoringRefused and ErrProposalDecideRefused all carry them in
// `what`); anything else (a transport fault, an unknown method, a shape
// error without a `what`) travels as the error itself.
func policyVerbatim(err error) error {
	var pe *protocol.Error
	if errors.As(err, &pe) {
		if pe.Data.What != "" {
			return errors.New(pe.Data.What)
		}
	}
	return err
}

// --- the socket door ---------------------------------------------------------------

// policySocketDoor is the default door: it calls the registered methods and
// prints what the wire said. It validates NOTHING — the daemon answers,
// this door relays.
type policySocketDoor struct{ c *protocol.Client }

func (d *policySocketDoor) call(method protocol.MethodName, params, out any) error {
	if err := d.c.Call(method, params, out); err != nil {
		return policyVerbatim(err)
	}
	return nil
}

func (d *policySocketDoor) list(ctx context.Context, _ time.Time) (protocol.PolicyListResult, error) {
	// The ZERO request: the CLI's --type is a PRESENTATION selection
	// (runPolicyList's standing claim — the two empty sentences hang on
	// it), so it is applied client-side to the rows and the daemon answers
	// the whole table either way. The filter params exist on the method
	// (T2's parity) for surfaces that want the daemon to filter; the CLI's
	// one flag is deliberately not one of them.
	var res protocol.PolicyListResult
	err := d.call(protocol.MethodPolicyList, protocol.PolicyListParams{}, &res)
	return res, err
}

func (d *policySocketDoor) get(ctx context.Context, id string, _ time.Time) (protocol.PolicyDetail, error) {
	var res protocol.PolicyGetResult
	if err := d.call(protocol.MethodPolicyGet, protocol.PolicyGetParams{ID: id}, &res); err != nil {
		return protocol.PolicyDetail{}, err
	}
	return res.Policy, nil
}

func (d *policySocketDoor) add(ctx context.Context, p protocol.PolicyAddParams) (protocol.PolicyAddResult, error) {
	var res protocol.PolicyAddResult
	err := d.call(protocol.MethodPolicyAdd, p, &res)
	return res, err
}

func (d *policySocketDoor) remove(ctx context.Context, id string) (protocol.PolicyRemoveResult, error) {
	var res protocol.PolicyRemoveResult
	err := d.call(protocol.MethodPolicyRemove, protocol.PolicyRemoveParams{ID: id}, &res)
	return res, err
}

func (d *policySocketDoor) seedCorpus(ctx context.Context, _ io.Writer, agents []string, dryRun bool) (protocol.SeedCorpusResult, bool, error) {
	// The unknown-agent refusal is desk.ResolveSeedTargets' sentence at BOTH
	// doors: the daemon's engine door reuses that resolver (T12), and so —
	// to keep the offline and online refusals byte-identical before paying a
	// round-trip — the socket door consults the same one resolver first.
	// This is input resolution, the step the direct door's runner also
	// takes, never a policy validation.
	corpus, rej := policy.ParseCorpus(policy.EmbeddedCorpus())
	if rej != nil {
		return protocol.SeedCorpusResult{}, false, fmt.Errorf("%s", rej.Reason)
	}
	if _, err := desk.ResolveSeedTargets(corpus, agents); err != nil {
		return protocol.SeedCorpusResult{}, false, err
	}
	var res protocol.SeedCorpusResult
	err := d.call(protocol.MethodPolicySeedCorpus, protocol.SeedCorpusParams{Agents: agents, DryRun: dryRun}, &res)
	return res, false, err
}

func (d *policySocketDoor) why(ctx context.Context, p protocol.PolicyWhyParams, _ time.Time) (protocol.PolicyWhyResult, error) {
	var res protocol.PolicyWhyResult
	err := d.call(protocol.MethodPolicyWhy, p, &res)
	return res, err
}

func (d *policySocketDoor) inForce(ctx context.Context, p protocol.PolicyInForceParams, _ time.Time) (protocol.PolicyInForceResult, error) {
	var res protocol.PolicyInForceResult
	err := d.call(protocol.MethodPolicyInForce, p, &res)
	return res, err
}

func (d *policySocketDoor) events(ctx context.Context, subject string, limit int, _ time.Time) (policyEventsAnswer, error) {
	var res protocol.PolicyEventsResult
	if err := d.call(protocol.MethodPolicyEvents, protocol.PolicyEventsParams{Subject: subject, Limit: limit}, &res); err != nil {
		return policyEventsAnswer{}, err
	}
	return policyEventsAnswer{Res: res}, nil
}

func (d *policySocketDoor) changes(ctx context.Context, policyID string, limit int, _ time.Time) (protocol.PolicyChangesResult, error) {
	var res protocol.PolicyChangesResult
	err := d.call(protocol.MethodPolicyChanges, protocol.PolicyChangesParams{PolicyID: policyID, Limit: limit}, &res)
	return res, err
}

func (d *policySocketDoor) vocabulary(ctx context.Context) (policy.VocabularyView, error) {
	var res protocol.PolicyVocabularyResult
	if err := d.call(protocol.MethodPolicyVocabulary, struct{}{}, &res); err != nil {
		return policy.VocabularyView{}, err
	}
	return res.Vocabulary, nil
}

func (d *policySocketDoor) proposalList(ctx context.Context, status string) ([]protocol.ProposalRow, error) {
	var res struct {
		Proposals []protocol.ProposalRow `json:"proposals"`
	}
	if err := d.call(protocol.MethodProposalList, map[string]any{"status": status}, &res); err != nil {
		return nil, err
	}
	return res.Proposals, nil
}

func (d *policySocketDoor) proposalGet(ctx context.Context, id int64) (protocol.ProposalGetResult, bool, error) {
	var res protocol.ProposalGetResult
	err := d.c.Call(protocol.MethodProposalGet, protocol.ProposalGetParams{ID: id}, &res)
	if err != nil {
		// The daemon's proposal.get names a missing row in its refusal's
		// `what` (proposals_decide.go's handler). Recognising THAT exact
		// sentence — built from the same form the handler builds it with —
		// lets the missing-row answer be the CLI's own golden sentence at
		// both doors; the door test pins the bytes, so daemon drift turns
		// red there rather than into a silent divergence.
		var pe *protocol.Error
		if errors.As(err, &pe) && pe.Code == protocol.ErrProposalDecideRefused.Code &&
			pe.Data.What == fmt.Sprintf("proposal %d does not exist", id) {
			return protocol.ProposalGetResult{}, false, nil
		}
		return protocol.ProposalGetResult{}, false, policyVerbatim(err)
	}
	return res, true, nil
}

func (d *policySocketDoor) proposalCommit(ctx context.Context, id int64, digest string, _ io.Writer) (protocol.ProposalCommitResult, error) {
	// The writer is deliberately unread here (S45 T10): over the socket the
	// daemon captures `agent_import`'s consent print and ships it as data
	// on the result (warnings); the CLI then prints what the wire SHIPPED
	// (printCommitWindow), like every other mandatory operator line on this
	// road. A socket door that streamed a daemon's effect print to its own
	// stdout would be the daemon talking through someone else's terminal.
	var res protocol.ProposalCommitResult
	err := d.call(protocol.MethodProposalCommit, protocol.ProposalCommitParams{ID: id, Digest: digest}, &res)
	return res, err
}

func (d *policySocketDoor) proposalReject(ctx context.Context, id int64, reason string) (protocol.ProposalRow, error) {
	var res protocol.ProposalRejectResult
	if err := d.call(protocol.MethodProposalReject, protocol.ProposalRejectParams{ID: id, Reason: reason}, &res); err != nil {
		return protocol.ProposalRow{}, err
	}
	return res.Proposal, nil
}

// --- the direct door ------------------------------------------------------------------

// policyDirectDoor is the offline fallback (PM4's flagged product call): the
// SAME code paths the daemon's services run, over a store opened directly
// (the justified second access path — WAL serialises it behind the daemon's
// writer; and there is no daemon when this door answers anyway). It opens
// the boundary through policy.go's seam variables so the fixtures that point
// the verbs at a temp store or a spy repo keep working through the door.
type policyDirectDoor struct{}

// boundary opens the policy boundary over the seam variable (the same
// policyBoundary the CLI door always used).
func (policyDirectDoor) boundary() (*policy.Store, func(), error) {
	if policyBoundary == nil {
		return nil, nil, fmt.Errorf("the policy boundary is not configured")
	}
	s, closeFn, err := policyBoundary()
	if err != nil {
		return nil, nil, fmt.Errorf("open the policy store: %w", err)
	}
	return s, closeFn, nil
}

func (d policyDirectDoor) list(ctx context.Context, at time.Time) (protocol.PolicyListResult, error) {
	s, closeFn, err := d.boundary()
	if err != nil {
		return protocol.PolicyListResult{}, err
	}
	defer closeFn()
	// The daemon's own answer function, over the directly opened boundary:
	// fail-closed re-decode, row rendering and all. Its refusals arrive as
	// the same typed *Error the socket carries, mapped identically.
	res, err := protocol.PolicyListAnswer(ctx, s, protocol.PolicyListParams{}, at)
	return res, policyVerbatim(err)
}

func (d policyDirectDoor) get(ctx context.Context, id string, at time.Time) (protocol.PolicyDetail, error) {
	s, closeFn, err := d.boundary()
	if err != nil {
		return protocol.PolicyDetail{}, err
	}
	defer closeFn()
	res, err := protocol.PolicyGetAnswer(ctx, s, protocol.PolicyGetParams{ID: id}, at)
	if err != nil {
		return protocol.PolicyDetail{}, policyVerbatim(err)
	}
	return res.Policy, nil
}

func (d policyDirectDoor) add(ctx context.Context, p protocol.PolicyAddParams) (protocol.PolicyAddResult, error) {
	pol, err := policyFromAddParams(p)
	if err != nil {
		return protocol.PolicyAddResult{}, err
	}
	s, closeFn, err := d.boundary()
	if err != nil {
		return protocol.PolicyAddResult{}, err
	}
	defer closeFn()
	b := deskBoundary{s: s}
	at := time.Now()
	if rej := b.AddPolicy(ctx, pol, at); rej != nil {
		// THE boundary judged, named the value it refused, and stored
		// nothing. Its sentence travels verbatim (no second validator here
		// could improve on it); nothing is audited because nothing happened.
		return protocol.PolicyAddResult{}, fmt.Errorf("%s", rej.Reason)
	}
	// One policy_change row beside the write it records (T3/PM8 — the same
	// ids-only fixed template the daemon's authoring door stamps, SEC-033),
	// actor_kind='cli': the offline door names itself; it never impersonates
	// a peer (the socket's stamp comes from the connection instead).
	st, err := openStoreDirectly(false)
	if err != nil {
		return protocol.PolicyAddResult{}, err
	}
	defer st.Close()
	if err := st.PolicyChangeInsert(ctx, store.PolicyChange{
		Op: "add", PolicyID: pol.ID,
		Summary:   fmt.Sprintf("added %s %s for %s", pol.Type, pol.Action, pol.Target.String()),
		ActorKind: "cli", ActorID: "cli",
		OccurredAt: at.UTC().Format(time.RFC3339Nano),
	}); err != nil {
		// The rule stands — the write committed; the audit is missing it.
		// Spoken, never hidden, never a retraction of the stored rule (the
		// daemon door's rule for its own audit failure, carried).
		return protocol.PolicyAddResult{}, fmt.Errorf(
			"policy %q was stored, but its authoring audit row was NOT written (%s); the rule stands — policy.changes names the gap",
			pol.ID, err)
	}
	// The two authoring-time operator signals, computed by the SAME desk
	// functions the daemon's authoring door runs (daemon/policy_authoring.go
	// 's authoringWarnings) in the SAME order — the presentation prints the
	// list identically whichever door carried it (PM4).
	var warns []string
	if warn, ok := desk.BroadAllowWarning(pol.Action, pol.Spec); ok {
		warns = append(warns, warn)
	}
	switch string(pol.Target.Kind) {
	case string(policy.LoopKind), string(policy.LaneTypeKind), string(policy.MCPServerKind):
		warns = append(warns, desk.UnmatchableTargetWarn(string(pol.Target.Kind)))
	}
	return protocol.PolicyAddResult{Warnings: warns}, nil
}

func (d policyDirectDoor) remove(ctx context.Context, id string) (protocol.PolicyRemoveResult, error) {
	s, closeFn, err := d.boundary()
	if err != nil {
		return protocol.PolicyRemoveResult{}, err
	}
	defer closeFn()
	b := deskBoundary{s: s}
	at := time.Now()
	// Read the row FIRST: a removal owes its audit summary to the row as it
	// was, and the audit must say WHAT it deleted (the evidence the daemon
	// door keeps in its result's Policy field).
	var gone *policy.Policy
	if rows, rej := b.ListPolicies(ctx, at); rej != nil {
		return protocol.PolicyRemoveResult{}, fmt.Errorf("%s", rej.Reason)
	} else {
		for _, r := range rows {
			if r.ID == id {
				row := r
				gone = &row
				break
			}
		}
	}
	existed, rerr := b.RemovePolicy(ctx, id)
	if rerr != nil {
		return protocol.PolicyRemoveResult{}, rerr
	}
	if existed && gone != nil {
		st, err := openStoreDirectly(false)
		if err != nil {
			return protocol.PolicyRemoveResult{}, err
		}
		defer st.Close()
		if err := st.PolicyChangeInsert(ctx, store.PolicyChange{
			Op: "remove", PolicyID: gone.ID,
			Summary:   fmt.Sprintf("removed %s %s for %s", gone.Type, gone.Action, gone.Target.String()),
			ActorKind: "cli", ActorID: "cli",
			OccurredAt: at.UTC().Format(time.RFC3339Nano),
		}); err != nil {
			return protocol.PolicyRemoveResult{}, fmt.Errorf(
				"policy %q was removed, but its authoring audit row was NOT written (%s); the removal stands — policy.changes names the gap",
				gone.ID, err)
		}
	}
	return protocol.PolicyRemoveResult{Removed: existed}, nil
}

func (d policyDirectDoor) seedCorpus(ctx context.Context, w io.Writer, agents []string, dryRun bool) (protocol.SeedCorpusResult, bool, error) {
	// THE desk runner, the one that keeps T1's bytes (T12's decision — the
	// engine's ApplyCorpus owns the socket side; this door does not fork its
	// semantics). It prints through desk.SeedAgentLine/SeedRunTotalsLine,
	// the same formatters the socket door prints the wire counts through.
	if policyBoundary == nil {
		return protocol.SeedCorpusResult{}, false, fmt.Errorf("the policy boundary is not configured")
	}
	s, sClose, err := policyBoundary()
	if err != nil {
		return protocol.SeedCorpusResult{}, false, fmt.Errorf("open the policy store: %w", err)
	}
	defer sClose()
	b := deskBoundary{s: s}
	if rerr := desk.RunSeedCorpus(ctx, w, b, agents, dryRun, time.Now()); rerr != nil {
		return protocol.SeedCorpusResult{}, true, rerr
	}
	return protocol.SeedCorpusResult{DryRun: dryRun}, true, nil
}

func (d policyDirectDoor) why(ctx context.Context, p protocol.PolicyWhyParams, fallback time.Time) (protocol.PolicyWhyResult, error) {
	s, closeFn, err := d.boundary()
	if err != nil {
		return protocol.PolicyWhyResult{}, err
	}
	defer closeFn()
	res, err := protocol.PolicyWhyAnswer(ctx, s, p, fallback)
	return res, policyVerbatim(err)
}

func (d policyDirectDoor) inForce(ctx context.Context, p protocol.PolicyInForceParams, at time.Time) (protocol.PolicyInForceResult, error) {
	s, closeFn, err := d.boundary()
	if err != nil {
		return protocol.PolicyInForceResult{}, err
	}
	defer closeFn()
	res, err := protocol.PolicyInForceAnswer(ctx, s, p, at)
	return res, policyVerbatim(err)
}

func (d policyDirectDoor) events(ctx context.Context, subject string, limit int, now time.Time) (policyEventsAnswer, error) {
	s, closeFn, err := d.boundary()
	if err != nil {
		return policyEventsAnswer{}, err
	}
	defer closeFn()
	out := policyEventsAnswer{}
	// DEF-030/SEC-034: the ON-DEMAND trim, the reader's trigger on the
	// 90-day cap. A failure never hides the report and is never silent —
	// the same line runPolicyEvents prints offline.
	n, terr := s.TrimEvents(ctx, now)
	out.TrimLine = trimLine(n, terr)
	report, rows, rej := s.ReportEvents(ctx, subject, limit)
	if rej != nil {
		return policyEventsAnswer{}, fmt.Errorf("%s", rej.Reason)
	}
	for _, r := range rows {
		out.Res.Rows = append(out.Res.Rows, protocol.PolicyEventsRow{
			ID: r.ID, Kind: r.Outcome, Subject: r.SubjectKind + ":" + r.SubjectID,
			PolicyID: r.PolicyID, Detail: r.Detail, At: r.At,
		})
	}
	if out.Res.Rows == nil {
		out.Res.Rows = []protocol.PolicyEventsRow{}
	}
	out.Res.Depth = report.Depth
	out.Res.CapDays = int(policy.PolicyEventAgeCap / (24 * time.Hour))
	if !report.Oldest.IsZero() {
		oldest := report.Oldest.UTC().Format(time.RFC3339)
		out.Res.Oldest = &oldest
	}
	// The ad-hoc-actor probe (DEF-041's third state): production prints
	// nothing — the minted actor is identifier-valid — but when the
	// boundary refuses it, every refusal of an ad-hoc step is unaudited BY
	// CONSTRUCTION and says so.
	adHoc := "agent:" + policyAdHocAuditActor()
	if _, _, probeRej := s.ReportEvents(ctx, adHoc, 1); probeRej != nil {
		out.UnauditedLine = eventsUnauditedAdHoc(adHoc)
	}
	return out, nil
}

func (d policyDirectDoor) changes(ctx context.Context, policyID string, limit int, now time.Time) (protocol.PolicyChangesResult, error) {
	if policyID != "" {
		// The filter's id passes the boundary's OWN gate (the socket
		// handler's single delegated call, mirrored, not restated): an id
		// that could never have been authored cannot filter the audit.
		if rej := policy.CheckIdentifier("policyId", policyID); rej != nil {
			return protocol.PolicyChangesResult{}, fmt.Errorf("%s", rej.Reason)
		}
	}
	st, err := openStoreDirectly(false)
	if err != nil {
		return protocol.PolicyChangesResult{}, err
	}
	defer st.Close()
	rows, err := st.PolicyChangeList(ctx, policyID, limit)
	if err != nil {
		return protocol.PolicyChangesResult{}, policyChangesFaultText(err)
	}
	total, err := st.PolicyChangeCount(ctx)
	if err != nil {
		return protocol.PolicyChangesResult{}, policyChangesFaultText(err)
	}
	out := protocol.PolicyChangesResult{Changes: []protocol.PolicyChangeView{}, UnauditedPolicyIDs: []string{}}
	for _, r := range rows {
		out.Changes = append(out.Changes, protocol.PolicyChangeView{
			Op: r.Op, PolicyID: r.PolicyID, ProposalID: r.ProposalID,
			Summary: r.Summary, ActorKind: r.ActorKind, ActorID: r.ActorID, OccurredAt: r.OccurredAt,
		})
	}
	if total == 0 {
		out.Statement = protocol.PolicyChangesEmptyStatement
	}
	// THE reconciliation the socket handler runs, exported for exactly this
	// second caller (S45 T8) — never a third copy of the rule.
	s, closeFn, berr := d.boundary()
	if berr != nil {
		return protocol.PolicyChangesResult{}, berr
	}
	defer closeFn()
	unaudited, uerr := protocol.UnauditedPolicyIDs(ctx, s, directPolicyChangeSeam{st}, total, now)
	if uerr != nil {
		return protocol.PolicyChangesResult{}, policyVerbatim(uerr)
	}
	out.UnauditedPolicyIDs = unaudited
	return out, nil
}

func (policyDirectDoor) vocabulary(_ context.Context) (policy.VocabularyView, error) {
	// The compiled engine answers for itself offline — the same
	// policy.Vocabulary() the daemon serves (PM6: never a client-side copy
	// of the closed sets; this is where the sets live, reached as a value).
	return policy.Vocabulary(), nil
}

func (policyDirectDoor) proposalList(_ context.Context, status string) ([]protocol.ProposalRow, error) {
	st, err := openStoreDirectly(false)
	if err != nil {
		return nil, err
	}
	defer st.Close()
	rows, err := st.ListProposals(status)
	if err != nil {
		return nil, err
	}
	out := make([]protocol.ProposalRow, 0, len(rows))
	for _, p := range rows {
		out = append(out, proposalRowFromStore(p))
	}
	return out, nil
}

func (policyDirectDoor) proposalGet(ctx context.Context, id int64) (protocol.ProposalGetResult, bool, error) {
	st, err := openStoreDirectly(false)
	if err != nil {
		return protocol.ProposalGetResult{}, false, err
	}
	defer st.Close()
	row, found, err := st.GetProposal(id)
	if err != nil {
		return protocol.ProposalGetResult{}, false, err
	}
	if !found {
		return protocol.ProposalGetResult{}, false, nil
	}
	res := protocol.ProposalGetResult{
		Proposal: proposalRowFromStore(row),
		Digest:   desk.ProposalPayloadDigest(row.Payload),
	}
	// The SAME advisory preview the socket attaches, through the exported
	// answer (S45 T8): the kinds that carry one speak it at both doors.
	if ps, closeFn, berr := (policyDirectDoor{}).boundary(); berr == nil {
		res.Preview = protocol.ProposalPreviewAnswer(ctx, ps, res.Proposal, time.Now())
		closeFn()
	}
	if res.Preview == nil && res.Proposal.Kind == "agent_import" {
		// S45 T10: the socket's get attaches S37's preview for this kind
		// (proposals_decide.go); the offline door renders the SAME text
		// through the SAME exported builder — one implementation, two
		// doors, so `proposal show` and the card cannot drift by road.
		res.Preview = protocol.AgentImportGetPreview(ctx, agentImportPreviewFor(st), []byte(row.Payload))
	}
	if res.Proposal.Kind == "blueprint_save" {
		// S48 T9: the design revision previews at BOTH doors — the socket
		// through the daemon's read service, this door through the same
		// read-service code over its own store handle (the lint half then
		// states its own unavailability: this door has no mounted catalog,
		// and an unavailable half is said, never silently all-clear).
		if bp, refusal, perr := daemon.BlueprintProposalPreview(ctx, st, []byte(row.Payload)); perr == nil && refusal == "" {
			if res.Preview == nil {
				res.Preview = &protocol.ProposalPreview{Kind: res.Proposal.Kind}
			}
			res.Preview.Blueprint = bp
		}
	}
	return res, true, nil
}

func (policyDirectDoor) proposalCommit(ctx context.Context, id int64, digest string, w io.Writer) (protocol.ProposalCommitResult, error) {
	st, err := openStoreDirectly(false)
	if err != nil {
		return protocol.ProposalCommitResult{}, err
	}
	defer st.Close()
	// THE desk orchestration (T5's one implementation): gate → digest →
	// AlreadyApplied → Validate → Apply → before/after → claimed flip →
	// audit. ReasonRequired stays false here (TD5's CLI ruling,
	// golden-pinned); the socket's adapter sets true.
	//
	// S45 T10 closed the last kind-specific routing through this door:
	// `agent_import` commits here EXACTLY like every other kind — the CLI
	// passes its own writer down (the consent print streams to the person,
	// S37 AA4) and captures the same bytes for the after view. The preview
	// for the before view is S37's one renderer, read before the effect
	// runs; a failed/absent preview answers silence, never a failed commit.
	var agent string
	var agentBefore protocol.InForceView
	if row, found, gerr := st.GetProposal(id); gerr == nil && found && row.Kind == "agent_import" {
		agent, agentBefore = protocol.AgentImportBeforeView(ctx, agentImportPreviewFor(st), []byte(row.Payload))
	}
	deps := cliDecideDeps(st)
	var imported strings.Builder
	if w != nil {
		deps.ApplyWriter = io.MultiWriter(w, &imported)
	} else {
		deps.ApplyWriter = &imported
	}
	res, cerr := desk.Commit(ctx, deps, deskEffects, id, digest, desk.Actor{Kind: "user", ID: "cli"})
	if cerr != nil {
		return protocol.ProposalCommitResult{}, cerr
	}
	out := protocol.ProposalCommitResult{
		Proposal:      proposalRowFromDesk(res.Row),
		EffectApplied: res.EffectApplied,
		Before:        inForceViewFromDesk(res.Before),
		After:         inForceViewFromDesk(res.After),
		Warnings:      res.Warnings,
	}
	if agent != "" {
		// No warnings append HERE: this door's person already SAW the print
		// (it streamed to their writer — the daemon road carries it in
		// warnings instead, and both roads print the same words).
		out.Before = agentBefore
		out.After = protocol.AgentImportAfterView(agent, imported.String())
	}
	return out, nil
}

func (policyDirectDoor) proposalReject(ctx context.Context, id int64, reason string) (protocol.ProposalRow, error) {
	st, err := openStoreDirectly(false)
	if err != nil {
		return protocol.ProposalRow{}, err
	}
	defer st.Close()
	res, rerr := desk.Reject(ctx, cliDecideDeps(st), id, reason, desk.Actor{Kind: "user", ID: "cli"})
	if rerr != nil {
		return protocol.ProposalRow{}, rerr
	}
	return proposalRowFromDesk(res.Row), nil
}

// --- shared mappings (wiring, not rules) ---------------------------------------------

// policyFromAddParams is the pure shaping of the wire params into the
// boundary's input — string casts only, the twin of the daemon door's
// shaping so neither door parses: the boundary owns every rule.
func policyFromAddParams(p protocol.PolicyAddParams) (policy.Policy, error) {
	pol := policy.Policy{
		ID:     p.ID,
		Type:   policy.PolicyType(p.Type),
		Target: policy.Target{Kind: policy.TargetKind(p.Target.Kind), ID: p.Target.ID},
		Action: policy.Action(p.Action),
		Source: policy.Source(p.Source),
		Spec:   p.Spec,
	}
	if p.NotAfter != nil {
		at, err := time.Parse(time.RFC3339Nano, *p.NotAfter)
		if err != nil {
			return policy.Policy{}, fmt.Errorf("--not-after %q does not read as an RFC3339 instant like 2026-10-15T00:00:00Z", *p.NotAfter)
		}
		pol.NotAfter = &at
	}
	return pol, nil
}

// proposalRowFromStore maps a store row to the wire shape — the same plain
// mirror the daemon adapter runs, with the commit command filled by the
// CLI's own proposalCommand (the two halves of one text, kept identical by
// TestCliListMatchesSocketList as always).
func proposalRowFromStore(p store.Proposal) protocol.ProposalRow {
	return protocol.ProposalRow{
		ID: p.ID, Kind: p.Kind, Payload: json.RawMessage(p.Payload), Status: p.Status,
		CreatedAt: p.CreatedAt, CreatedByKind: p.CreatedByKind, CreatedByID: p.CreatedByID,
		DecidedAt: p.DecidedAt, DecidedByKind: p.DecidedByKind, DecidedByID: p.DecidedByID,
		DecideReason: p.DecideReason, AppliedError: p.AppliedError,
		Command: proposalCommand(p.ID),
	}
}

// proposalRowFromDesk maps the desk's store-free row to the same wire shape.
func proposalRowFromDesk(r desk.Row) protocol.ProposalRow {
	return protocol.ProposalRow{
		ID: r.ID, Kind: r.Kind, Payload: json.RawMessage(r.Payload), Status: r.Status,
		CreatedAt: r.CreatedAt, CreatedByKind: r.CreatedByKind, CreatedByID: r.CreatedByID,
		DecidedAt: r.DecidedAt, DecidedByKind: r.DecidedByKind, DecidedByID: r.DecidedByID,
		DecideReason: r.DecideReason, AppliedError: r.AppliedError,
		Command: proposalCommand(r.ID),
	}
}

// inForceViewFromDesk maps the desk's store-free in-force view onto the wire
// shape, sentences untouched (D14 — the mirror is field-for-field).
func inForceViewFromDesk(v desk.InForceView) protocol.InForceView {
	out := protocol.InForceView{Target: v.Target, Answers: []protocol.PolicyInForceRow{}}
	for _, a := range v.Answers {
		out.Answers = append(out.Answers, protocol.PolicyInForceRow{
			PolicyID: a.PolicyID, State: a.State, Render: a.Render,
		})
	}
	return out
}

// directPolicyChangeSeam is the two-method read shim the exported
// reconciliation reads the audit through — the daemon adapter's shape,
// carried by the offline door for its own handle (reads only, as the seam
// is by shape and by right).
type directPolicyChangeSeam struct{ s *store.Store }

func (a directPolicyChangeSeam) PolicyChangeList(ctx context.Context, policyID string, limit int) ([]protocol.PolicyChangeView, error) {
	rows, err := a.s.PolicyChangeList(ctx, policyID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]protocol.PolicyChangeView, 0, len(rows))
	for _, r := range rows {
		out = append(out, protocol.PolicyChangeView{
			Op: r.Op, PolicyID: r.PolicyID, ProposalID: r.ProposalID,
			Summary: r.Summary, ActorKind: r.ActorKind, ActorID: r.ActorID, OccurredAt: r.OccurredAt,
		})
	}
	return out, nil
}

func (a directPolicyChangeSeam) PolicyChangeCount(ctx context.Context) (int, error) {
	return a.s.PolicyChangeCount(ctx)
}

// policyChangesFaultText is the three-state refusal the socket's
// policyChangesFault carries, spelled as a plain error for the offline
// door: a fault READING the audit is neither a refusal nor an empty audit,
// and the store's own sentence travels verbatim.
func policyChangesFaultText(err error) error {
	return fmt.Errorf("the policy changes read could not reach the authoring audit: %s", err)
}
