package daemon

// The production policy gate (S10, D11/PL9): the adapter that turns the
// runtime's `PolicyGate` question into a `policy.Evaluate` over the stored set
// and lands a refusal in `policy_event`.
//
// # Why it lives HERE and not in internal/protocol/step.go
//
// The task allows either home "if the wiring site demands a new file" — and it
// does. The Table is constructed and wired in this package (`daemon.New` is
// where `table.Models`/`table.Runner` are assigned), the concrete store is in
// hand only here, and `internal/protocol` must not import `internal/store`
// (TestTheFourPackagesCannotSeeEachOther guards exactly that). The protocol
// table therefore carries the gate as the runtime's own interface
// (`Table.PolicyGate`) and this file supplies the implementation.
//
// # The trust boundary (PL1, carried HIGH from the T6 gate)
//
// The adapter assigns every trust-bearing field IT PERSISTS: the Request
// arrives from `runTool`, which fills it from the server-side compiled step
// request, and the persisted event's kind, subject and detail are assigned by
// the record boundary (`internal/policy/record.go`) from the verdict it was
// handed — `Source` is never read from an untrusted payload here, and the
// adapter invents no text the sentence does not already carry.

import (
	"github.com/cjlapao/aeon/internal/handler"

	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/config"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/runtime"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/tools"
)

// policyGateTimeout bounds one gate decision. The gate sits on the step's
// dispatch path, so the store read and the refusal record must be bounded —
// an unbounded Decide is a step that hangs instead of being denied.
const policyGateTimeout = 5 * time.Second

// NewPolicyGate builds the production gate over the daemon's store. It is
// wired by daemon.New whenever a store exists, and `aeon serve` always passes
// one — the nil gate is the daemon's storeless test mode, never production.
func NewPolicyGate(s *store.Store, log Logger, opts ...GateOption) runtime.PolicyGate {
	if log == nil {
		log = discardLogger{}
	}
	g := &storePolicyGate{boundary: policy.NewStore(newPolicyRepo(s)), log: log}
	for _, o := range opts {
		o(g)
	}
	return g
}

// GateOption adjusts a gate at construction.
type GateOption func(*storePolicyGate)

// WithUnmatchedSetting lets the gate read permissions.unmatched from the settings store.
func WithUnmatchedSetting(settings *config.Store) GateOption {
	return func(g *storePolicyGate) { g.settings = settings }
}

// evaluate is the ONE evaluation the gate makes on its ordinary path: plain Evaluate under the strict
// setting, and the ask-when-unmatched amendment of PL5 under the default one. The fail-closed paths
// (an unreadable or poisoned set) never call it; they call policy.Evaluate(nil, …) and deny.
func (g *storePolicyGate) evaluate(ctx context.Context, ps []policy.Policy, req policy.Request, at time.Time) policy.Verdict {
	if g.settings != nil && config.PolicyUnmatchedAsks(ctx, g.settings) {
		return policy.EvaluateAskingWhenUnmatched(ps, req, at)
	}
	return policy.Evaluate(ps, req, at)
}

// storePolicyGate is the caller-side implementation: the runtime asks
// "may this agent call this tool at this instant", and this answers with the
// engine over the stored set.
//
// The frozen half (S10, D12): when `frozen` is non-nil the gate answers from
// the manifest-carried set instead of the store — a replay is decided against
// the policies the step originally saw, never against today's table. A
// readable frozen set — INCLUDING one frozen EMPTY, the DEF-042 marker's
// case — still audits LIVE: the boundary writes the refusal (and
// disagreement) rows, because a replayed refusal is a refusal that happened
// now, and the audit records what the engine did, not what it was told. The
// poisoned half is the exception (DEF-079, measured 0 live rows): a set that
// failed to decode, or that contradicts the digest its manifest recorded
// (frozenErr), denies from the early return BEFORE anything is recorded —
// neither a policy_event row nor a live read happens on that path.
// Enforcement never depends on the audit, in either direction.
type storePolicyGate struct {
	// settings, when set, lets the gate read permissions.unmatched: with "ask" a tool call or command no
	// rule covers asks a person instead of being refused. Nil keeps the strict PL5 default.
	settings  *config.Store
	boundary  *policy.Store
	log       Logger
	frozen    []policy.Policy
	frozenErr *policy.Reject
}

// Decide implements runtime.PolicyGate.
//
// The question is chosen by the ONE rule (S11): a bash block carrying a
// command asks policy.CommandAccess over that command; everything else asks
// policy.ToolAccess. The set to decide against and the audit below are
// unchanged by that choice — one engine, one formatter, one event writer.
//
// The order is the engine's order (evaluate.go): read the stored set through
// the write boundary's reader (every row re-read through the closed-set
// parsers — a tampered row refuses the whole read, fail-closed), evaluate,
// and record a refusal through the boundary's one event writer. A question
// (the engine's ask tier) records nothing: it is not a refusal, and the
// step decides what to do with someone who may answer.
//
// Fail-closed twice: a policy set that cannot be READ denies with the PL5
// default-deny sentence (no allow can be vouched for, so nothing is
// permitted), and a refusal whose audit row cannot be written still DENIES —
// enforcement never depends on the audit, and an unwritten row is logged, not
// allowed around.
func (g *storePolicyGate) Decide(req runtime.PolicyGateRequest, at time.Time) runtime.PolicyGateDecision {
	ctx, cancel := context.WithTimeout(context.Background(), policyGateTimeout)
	defer cancel()

	preq := gateQuestion(req)

	// The set to decide against: the frozen one for a replay (D12 — the set
	// the step saw, reconstructed from its manifest, never re-queried), the
	// live store otherwise. The audit below is LIVE in both of those cases:
	// the row records what this engine just decided, and an audit that
	// rewrote history to match a frozen set would be the drift with a stamp
	// on it. The frozenErr early return underneath takes NEITHER set and
	// writes NO row (DEF-079): a poisoned record is refused outright.
	var ps []policy.Policy
	if g.frozenErr != nil {
		g.log.Infof("policy gate: the frozen policy set could not be read (%s); denying", g.frozenErr.Reason)
		v := policy.Evaluate(nil, preq, at)
		return runtime.PolicyGateDecision{Allowed: false, Sentence: v.Sentence()}
	}
	if g.frozen != nil {
		ps = g.frozen
	} else {
		var rej *policy.Reject
		ps, rej = g.boundary.List(ctx, at)
		if rej != nil {
			// The set cannot be read. Deny with the engine's own fail-closed
			// rendering rather than inventing prose, and say why in the log —
			// the detail column would not be writable either, and a refusal needs
			// no audit row to be a refusal.
			g.log.Infof("policy gate: the policy set could not be read (%s); denying", rej.Reason)
			v := policy.Evaluate(nil, preq, at)
			return runtime.PolicyGateDecision{Allowed: false, Sentence: v.Sentence()}
		}
	}

	v := g.evaluate(ctx, ps, preq, at)
	if v.Asked {
		// A question, not a refusal (S11): the ask writes NO policy_event
		// row and no disagreement row — record.go's writers stay on the
		// genuine-deny path below, because an audit row calling a question
		// a refusal is the row that lies. The decision carries the question
		// in plain strings (K22): Asked, and the deciding rule's ID and
		// subject — the same rendering the stored row carries its subject
		// in, target.String() (store.go's derivation) — so the step that
		// suspends on it can name the rule that asked (QAP-12: the question
		// that does not name its rule is unauditable). Enforcement still
		// permits nothing: Allowed is false, and the loop dispatches only
		// on Allowed.
		d := runtime.PolicyGateDecision{
			Allowed:  false,
			Asked:    true,
			Sentence: v.Sentence(),
			Command:  req.Command,
		}
		if v.By != nil {
			d.RuleID = v.By.ID
			d.RuleSubject = v.By.Target.String()
		}
		return d
	}
	if !v.Allowed {
		if r := g.boundary.RecordRefusal(ctx, v.Subject, v, at); r != nil {
			// The refusal stands; the audit is missing a row. That is an
			// operator problem (log it), never a reason to permit.
			g.log.Infof("policy gate: the refusal of %s was NOT recorded (%s); the denial still stands",
				v.Subject, r.Reason)
		}
		if req.ModelRequested && req.StepID > 0 {
			// The model itself asked for this, and the manifest (PL9's
			// informing half) is the reason the row is interesting: a rising
			// disagreement rate is the policy text not being read. An
			// unattributed attempt (StepID 0) records no disagreement — the
			// boundary would refuse the row anyway (T10's attribution gate);
			// checking here keeps the log honest instead of noisy.
			if r := g.boundary.RecordDisagreement(ctx, v.Subject, req.StepID, req.Agent, v, at); r != nil {
				g.log.Infof("policy gate: the disagreement of %s (step %d) was NOT recorded (%s); the denial still stands",
					v.Subject, req.StepID, r.Reason)
			}
		}
	}
	return runtime.PolicyGateDecision{Allowed: v.Allowed, Sentence: v.Sentence()}
}

// gateQuestion is the ONE question-selection rule (S11), extracted so the
// enforcement gate (Decide) and the spawn-key read (commandVerdict) cannot
// drift apart on WHICH question gets asked. A bash block WITH a command
// asks the command question; everything else asks the tool question. A bash
// block with no parsable command asks the tool question and the tool rules
// govern — the bash tool itself refuses the bad arguments (G20). The command
// crosses as the plain string it is (K22); the engine, not this adapter,
// decides what its rules say.
//
// The tool name is asked in its CANONICAL form (T19, SEC-051): the hidden
// write_file alias and write bind the SAME tool, so a deny row naming
// "write" must reach a call spelled write_file — otherwise blocking the
// write CAPABILITY would depend on the operator remembering to deny both
// spellings, and deny rows are supposed to be the enforcement, not a memory
// test. The dispatcher-name invariant survives: loop.go still executes
// exactly the name it asked about, and both spellings reach one tool body,
// so deduping the QUESTION cannot answer for a different tool than the one
// dispatched. An unknown name passes through unchanged — BuildToolset
// refuses unknown grants upstream, and PL5's default deny answers here.
// bindPolicySubject closes PL5's naming gap (T10): the blueprint dispatches
// under the HANDLER name ("golang-engineer") while the corpus rows — and
// whatever the operator-side seeder ever stored — are keyed by the corpus
// agent the rewrites declared ("golang-engineer-loop"). Without this the
// gate asks a question no row can answer, every handler command meets the
// default deny, and the enforced policy silently disagrees with the
// reported one. The binding is ONE lookup through the registry the loader
// already validated — a name with no body (a preset, an operator-authored
// agent) binds to itself, because there is nothing to translate.
func bindPolicySubject(agent string) string {
	if h, ok := handler.Lookup(agent); ok && h.PolicyAgent != "" {
		return h.PolicyAgent
	}
	return agent
}

func gateQuestion(req runtime.PolicyGateRequest) policy.Request {
	preq := policy.Request{
		Type:      policy.ToolAccess,
		Agent:     bindPolicySubject(req.Agent),
		Loop:      req.Loop,
		LaneType:  req.LaneType,
		MCPServer: req.MCPServer,
		Tool:      tools.CanonicalToolName(req.Tool),
	}
	if preq.Tool == "bash" && req.Command != "" {
		preq.Type = policy.CommandAccess
		preq.Command = req.Command
	}
	return preq
}

// commandVerdict is the SPAWN-KEY read (S11, T13): the same engine, the
// same question (gateQuestion), the same frozen/live choice as Decide — and
// NO audit write. The audit obligation is Decide's: a refusal that happened
// is a refusal the boundary records, and the spawn key re-asking the same
// question for the same block must not double-count the same refusal (an
// audit that inflates its own denials is the record that lies). It answers
// (verdict, ok): ok is false only when no policy source can answer at all;
// an unreadable set answers the engine's own fail-closed denial (ok true,
// Allowed false), exactly as Decide denies on it — enforcement never
// depends on the audit, and the spawn key never depends on being asked
// nicely. This reads ONLY the gate's own state and the step's command: no
// question row, no Kind string, nothing the model wrote (SEC-054 — the
// token path never parses model-influenced text for authority).
func (g *storePolicyGate) commandVerdict(req runtime.PolicyGateRequest, at time.Time) (policy.Verdict, bool) {
	if g.boundary == nil {
		// No boundary at all: no source, no verdict, no spawn. The caller's
		// honest refusal names the missing policy source.
		return policy.Verdict{}, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), policyGateTimeout)
	defer cancel()

	preq := gateQuestion(req)
	var ps []policy.Policy
	switch {
	case g.frozenErr != nil:
		// Poisoned frozen set: deny from the record's contradiction, exactly
		// as Decide does from its early return — and again, no row written.
		return policy.Evaluate(nil, preq, at), true
	case g.frozen != nil:
		ps = g.frozen
	default:
		var rej *policy.Reject
		ps, rej = g.boundary.List(ctx, at)
		if rej != nil {
			// The set cannot be read; the engine's fail-closed line answers,
			// no token can come of it, and the block that asked will say so.
			return policy.Evaluate(nil, preq, at), true
		}
	}
	return g.evaluate(ctx, ps, preq, at), true
}

// CommandVerdictFor builds the step-side spawn-key seam for a store
// (T13): the closure `BuildToolset` callers wire into
// `tools.ToolContext.CommandVerdict`. It answers the command question the
// way the step's gate answers it — the frozen set when the step's manifest
// carried one (digest re-checked, poisoned set denies), the live store when
// it did not — through `newStepPolicyGateWithBoundary`, the SAME
// construction the gate itself gets, so the gate's verdict and the token's
// verdict are one code path over one set.
//
// The two reads can still disagree in exactly one direction, and that
// direction loses the spawn: the gate decides at the step's snapshotted
// `policyAt`, this reads at call time. A policy moved between the two
// instants means the spawn key re-asked and the answer came back without a
// token — the command does not run. Where the two agree (nobody moved a
// policy mid-step, the overwhelmingly common case) the verdicts are
// identical because they are literally the same function over the same set.
// A storeless daemon answers nil, and nil is the honest answer: there is
// nothing to ask.
func CommandVerdictFor(s *store.Store, log Logger, opts ...GateOption) func(agent string, frozen runtime.FrozenManifest, command string) (policy.Verdict, bool) {
	if s == nil {
		return nil
	}
	if log == nil {
		log = discardLogger{}
	}
	return func(agent string, frozen runtime.FrozenManifest, command string) (policy.Verdict, bool) {
		if strings.TrimSpace(command) == "" {
			// No verdict for no command. bash refuses the empty command
			// before it ever asks; asking for "" is a caller defect, and the
			// answer it deserves is the one that spawns nothing.
			return policy.Verdict{}, false
		}
		g := newStepPolicyGateWithBoundary(policy.NewStore(newPolicyRepo(s)), log, frozen)
		for _, o := range opts {
			o(g)
		}
		return g.commandVerdict(runtime.PolicyGateRequest{Agent: agent, Tool: "bash", Command: command}, time.Now())
	}
}

// newPolicyRepo bridges the boundary's Repo seam to the concrete store. The
// conversion is boring and total on purpose — policy.Row ⇄ store.PolicyRow are
// identically shaped structs in two packages (I2 keeps the SQL down here and
// the rule upstairs), and Go method identity means the wiring is where the two
// shapes meet. Every hook is filled: a missing one surfaces as the bridge's
// own wiring-defect error, and T12/T13 reuse this same bridge for the CLI and
// the read-only socket methods.
func newPolicyRepo(s *store.Store) policy.RepoFuncs {
	return policy.RepoFuncs{
		Add: func(ctx context.Context, r policy.Row) error {
			return s.PolicyAdd(ctx, store.PolicyRow{
				ID: r.ID, Type: r.Type, TargetKind: r.TargetKind, TargetID: r.TargetID,
				Action: r.Action, Source: r.Source, Subject: r.Subject,
				SpecJSON: r.SpecJSON, NotAfter: r.NotAfter, CreatedAt: r.CreatedAt,
			})
		},
		Remove: func(ctx context.Context, id string) (bool, error) {
			return s.PolicyRemove(ctx, id)
		},
		ListAll: func(ctx context.Context) ([]policy.Row, error) {
			rows, err := s.PolicyListAll(ctx)
			if err != nil {
				return nil, err
			}
			out := make([]policy.Row, 0, len(rows))
			for _, r := range rows {
				out = append(out, policy.Row{
					ID: r.ID, Type: r.Type, TargetKind: r.TargetKind, TargetID: r.TargetID,
					Action: r.Action, Source: r.Source, Subject: r.Subject,
					SpecJSON: r.SpecJSON, NotAfter: r.NotAfter, CreatedAt: r.CreatedAt,
					Origin: r.Origin,
				})
			}
			return out, nil
		},
		// AddTx and RemoveTx are the seed door's transaction hooks (S45 T12,
		// PM15). The transaction the seed engine opened (and the daemon's
		// seedRepo boxed as a seed.Tx, unwrapped by the daemon applier
		// adapter) arrives here as the policy-side Tx seam; the concrete
		// *sql.Tx passes through the interface unchanged, so unwrapping it
		// back is a type assertion, not a re-open. A value that is not the
		// daemon's own transaction is the bridge misuse it names — never a
		// write on a stranger's transaction.
		AddTx: func(ctx context.Context, tx policy.Tx, r policy.Row) error {
			sqlTx, ok := tx.(*sql.Tx)
			if !ok {
				return fmt.Errorf("daemon: the policy seed door was handed a transaction it did not open")
			}
			return s.PolicyAddTx(ctx, sqlTx, store.PolicyRow{
				ID: r.ID, Type: r.Type, TargetKind: r.TargetKind, TargetID: r.TargetID,
				Action: r.Action, Source: r.Source, Subject: r.Subject,
				SpecJSON: r.SpecJSON, NotAfter: r.NotAfter, CreatedAt: r.CreatedAt,
				Origin: r.Origin,
			})
		},
		RemoveTx: func(ctx context.Context, tx policy.Tx, id string) (bool, error) {
			sqlTx, ok := tx.(*sql.Tx)
			if !ok {
				return false, fmt.Errorf("daemon: the policy seed door was handed a transaction it did not open")
			}
			return s.PolicyDeleteTx(ctx, sqlTx, id)
		},
		EventInsertTx: func(ctx context.Context, tx policy.Tx, kind, subject, policyID, detail string, at time.Time) error {
			sqlTx, ok := tx.(*sql.Tx)
			if !ok {
				return fmt.Errorf("daemon: the policy seed door was handed a transaction it did not open")
			}
			return s.PolicyEventInsertTx(ctx, sqlTx, kind, subject, policyID, detail, at)
		},
		AcceptanceRecord: func(ctx context.Context, policyID, targetKind, targetID, acceptedBy string, at time.Time) error {
			return s.PolicyAcceptanceRecord(ctx, policyID, targetKind, targetID, acceptedBy, at)
		},
		AcceptanceList: func(ctx context.Context, targetKind, targetID string) ([]policy.AcceptanceRow, error) {
			rows, err := s.PolicyAcceptanceListFor(ctx, targetKind, targetID)
			if err != nil {
				return nil, err
			}
			out := make([]policy.AcceptanceRow, 0, len(rows))
			for _, r := range rows {
				out = append(out, policy.AcceptanceRow{
					PolicyID: r.PolicyID, TargetKind: r.TargetKind, TargetID: r.TargetID,
					AcceptedBy: r.AcceptedBy, AcceptedAt: r.AcceptedAt,
				})
			}
			return out, nil
		},
		// EventInsert is reached ONLY through record.go's recordEvent, the sole
		// writer (legal constraint 1). The hook is the store method bound
		// directly — a BINDING, not a forwarding call: the sole-writer grep
		// (TestPolicyEventInsertHasExactlyOneCaller) counts CALLS of the store
		// method and tolerates none outside record.go, and pointing this hook
		// at anything else is the second writer that grep forbids.
		EventInsert: s.PolicyEventInsert,
		EventQuery: func(ctx context.Context, subject string, limit int) ([]policy.EventRow, error) {
			rows, err := s.PolicyEventQuery(ctx, subject, limit)
			if err != nil {
				return nil, err
			}
			out := make([]policy.EventRow, 0, len(rows))
			for _, r := range rows {
				out = append(out, policy.EventRow{
					ID: r.ID, PolicyID: r.PolicyID, SubjectKind: r.SubjectKind, SubjectID: r.SubjectID,
					Outcome: r.Outcome, Detail: r.Detail, At: r.At,
				})
			}
			return out, nil
		},
		EventTrim: func(ctx context.Context, olderThan time.Time) (int, error) {
			return s.PolicyEventTrim(ctx, olderThan)
		},
		EventStats: func(ctx context.Context, subject string) (int, time.Time, error) {
			// The subject travels with the read (F-13): the depth the CLI
			// footer prints is the listing's, not the table's.
			return s.PolicyEventStats(ctx, subject)
		},
	}
}

// TrimPolicyEventsAtStartup is DEF-030's FIRST trigger: the 90-day
// policy_event cap fires once per boot, HERE, and on demand in
// `aeon policy events` (T12) — never on the record path (PL11: a refusal
// never pays for housekeeping), and never left to never happen ("every
// refusal forever" is how an audit table becomes the reason somebody
// deletes the database instead of reading it).
//
// Bounded by construction: one DELETE through the seam, on the startup path
// before the endpoint reports ready. A failure is SPOKEN and not fatal —
// retention.go's own rule: the daemon starts, the audit keeps recording,
// and the next boot retries; a missed trim is a housekeeping miss, never a
// permission. `now` arrives from the caller (D5: the boundary reads no
// clock; the startup path owns the wall).
func TrimPolicyEventsAtStartup(ctx context.Context, s *store.Store, log Logger, now time.Time) int {
	if log == nil {
		log = discardLogger{}
	}
	if s == nil {
		return 0 // the storeless test daemon has no audit to trim
	}
	n, err := policy.NewStore(newPolicyRepo(s)).TrimEvents(ctx, now)
	if err != nil {
		// Spoken, not fatal (above). This is the whole of the failure
		// handling DEF-030 asked for: silence would let the cap rot.
		log.Infof("policy events: the 90-day cap could not be enforced at startup (the audit keeps "+
			"recording; the next boot retries): %v", err)
		return 0
	}
	if n > 0 {
		log.Infof("policy events: trimmed %d row(s) past the %v cap", n, policy.PolicyEventAgeCap)
	}
	return n
}

// NewStepPolicyGate builds the gate for ONE step (S10, D12): the set is
// handed in, not consulted, TOGETHER WITH the digest its manifest recorded
// (DEF-039) — and that digest is RECOMPUTED from the entries and compared
// here, at load, before anything is decided.
//
//   - no marker AND no entries ⇒ the live store answers — the genuinely
//     pre-S10 fallback. A manifest that never saw a policy source has no
//     frozen set to betray it, and the live gate is the only honest answer.
//     A MARKED empty set (frozen.Frozen, DEF-042) is NOT this case: it says
//     a store was consulted and found empty, and it decides below over that
//     empty set — the engine's own default deny, with no live read.
//   - pinned non-empty, digest matching ⇒ the gate decides against THAT set,
//     reconstructed once at construction and never re-read. This is what
//     makes replay honest: the step is decided against the policies it
//     originally saw, so revoking or adding a policy between the crash and
//     the recovery changes nothing about what the recovery decides.
//   - pinned non-empty, digest contradicting ⇒ the WHOLE gate denies
//     (frozenErr), and nothing falls back to the live store: a half-updated
//     governance record is not a record to re-decide against.
//
// A pinned entry that will not decode poisons the WHOLE gate (frozenErr →
// every Decide denies): the same all-or-nothing List and decodeRow enforce
// on the live path, and for the same reason — silently dropping an entry can
// drop a Deny, and a replay that quietly loses a deny is the drift wearing
// the manifest's clothes. A READABLE frozen set — empty or not — audits
// through the live boundary: rows written during a replay record what the
// engine decided now. The poisoned gate is the exception: it denies from the
// early return and writes no audit row at all (measured 0 policy_event
// writes, DEF-079) — enforcement never depends on the audit, in either
// direction.
//
// It is a CONSTRUCTION extension, not an interface change — runtime.PolicyGate
// and Decide are exactly as T9 wired them (the seed's constraint, and the
// right one: honesty in how the gate is built, not in what it can be asked).
func NewStepPolicyGate(s *store.Store, log Logger, frozen runtime.FrozenManifest, opts ...GateOption) runtime.PolicyGate {
	g := newStepPolicyGateWithBoundary(policy.NewStore(newPolicyRepo(s)), log, frozen)
	for _, o := range opts {
		o(g)
	}
	return g
}

// newStepPolicyGateWithBoundary is the production constructor with the
// read path in hand rather than a store handle: the unit tests count reads
// through the same RepoFuncs bridge the boundary uses, so "a digest mismatch
// never consults the live store" (DEF-039's hard half) is a measured zero,
// not a reading of the code. NewStepPolicyGate hands it the production
// bridge; nothing else builds a step gate.
func newStepPolicyGateWithBoundary(boundary *policy.Store, log Logger, frozen runtime.FrozenManifest) *storePolicyGate {
	if log == nil {
		log = discardLogger{}
	}
	g := &storePolicyGate{boundary: boundary, log: log}
	if !frozen.Frozen && len(frozen.Entries) == 0 {
		return g // genuinely pre-S10: no marker, no entries — no frozen set to betray it, the live store answers
	}
	// DEF-039, honestly scoped. This is a CHANGE DETECTOR, not an integrity
	// proof: an unkeyed sha256 over a file a same-user process can rewrite
	// (P26's trust domain) catches the writer who updated Policies and forgot
	// PoliciesHash, and catches truncation. It cannot catch a consistent
	// forgery — HMAC would, and legal constraint 6 keeps it out. runtime owns
	// the canonical line form (context.go's policyCanonicalLine), so the
	// daemon recomputes through runtime.PoliciesHashOf and never re-derives a
	// digest of its own: two digesters is two answers, and D12's whole
	// argument is that a step saw exactly one. The comparison runs BEFORE
	// decodeFrozenSet: it is cheaper, and a tampered set that still decodes
	// is exactly the case the digest is for.
	//
	// An EMPTY recorded hash is not a mismatch: a hand-built manifest (a
	// test's own set) has no digest on record to contradict, and production
	// always has one because AssembleContext mints it whenever it freezes a
	// set. The check therefore catches "entries changed, hash not" and
	// truncation — not omission.
	if frozen.Hash != "" {
		if got := runtime.PoliciesHashOf(frozen.Entries); got != frozen.Hash {
			g.frozenErr = &policy.Reject{Reason: fmt.Sprintf(
				"refused: the frozen policy set does not match the digest its manifest recorded (recorded %s, "+
					"recomputed %s); a step whose governance record is half-updated is not a record to replay "+
					"against, and re-deciding it against today's store is the drift D12 forbids",
				digestForLog(frozen.Hash), digestForLog(got))}
			g.log.Infof("policy gate: the frozen set's recorded digest %s does not match the set it carries (%s); denying everything",
				digestForLog(frozen.Hash), digestForLog(got))
			return g
		}
	}
	ps, rej := decodeFrozenSet(frozen.Entries)
	if rej != nil {
		g.frozenErr = rej
	} else {
		g.frozen = ps
	}
	return g
}

// isHex64 reports whether s has exactly the shape runtime.PoliciesHashOf
// mints: 64 lowercase hexadecimal characters. It is a SHAPE check, written
// without regexp or fmt on the hot comparison path.
func isHex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// digestForLog gates a digest before it is echoed into a log line or a
// reject reason (SEC-066). The recorded digest arrives from a stored manifest
// a same-user process can rewrite (P26's trust domain), so it is UNTRUSTED
// input on the log path: only a value with the exact hex64 shape is ever
// echoed, and then only through %q, which escapes control characters — a
// hostile value cannot smuggle ANSI escapes or forged lines into the
// operator's terminal. Anything else is named by shape, never quoted back.
func digestForLog(h string) string {
	if isHex64(h) {
		return fmt.Sprintf("%q", h)
	}
	return `"<not a hex digest>"`
}

// DecodeFrozenSet rebuilds []policy.Policy from a step's frozen manifest
// entries. It is decodeFrozenSet's exported twin for the ONE legitimate
// outside reader — cmd/aeon's StepMCP adapter (S46 T3), which partitions
// the offer against the set the step is frozen to, the same D12 rule the
// gate decides by. A non-nil Reject means "judge nothing": every offered
// candidate then withholds deny-leaning (MC10), which is safe; it must
// never fall back to the live table.
func DecodeFrozenSet(entries []runtime.PolicyEntry) ([]policy.Policy, *policy.Reject) {
	return decodeFrozenSet(entries)
}

// decodeFrozenSet rebuilds []policy.Policy from manifest entries the way
// every other input to this package is read: marshalled to the canonical
// envelope and taken THROUGH policy.DecodePolicy — the closed-set parsers,
// the same path decodeRow takes. Filling the struct by hand is the bypass
// the boundary's whole file argues against; a manifest entry claiming a
// kind outside the closed set must REFUSE, not evaluate.
func decodeFrozenSet(entries []runtime.PolicyEntry) ([]policy.Policy, *policy.Reject) {
	out := make([]policy.Policy, 0, len(entries))
	for _, e := range entries {
		spec := string(e.Spec)
		if spec == "" {
			spec = "{}"
		}
		env := map[string]any{
			"id":     e.ID,
			"type":   e.Type,
			"action": e.Action,
			"source": e.Source,
			"target": map[string]string{"kind": e.TargetKind, "id": e.TargetID},
			"spec":   json.RawMessage(spec),
		}
		if e.NotAfter != nil {
			env["not_after"] = e.NotAfter.Format(time.RFC3339Nano)
		}
		raw, err := json.Marshal(env)
		if err != nil {
			return nil, &policy.Reject{Reason: fmt.Sprintf("refused: the frozen policy entry %q could not be re-read: %v", e.ID, err)}
		}
		p, rej := policy.DecodePolicy(raw)
		if rej != nil {
			return nil, &policy.Reject{Reason: fmt.Sprintf(
				"refused: the frozen policy entry %q is not a policy the engine accepts (%s); a manifest that "+
					"names a shape Add would refuse is not a set to replay against, and dropping the row "+
					"silently can drop a Deny with it", e.ID, rej.Reason)}
		}
		out = append(out, p)
	}
	return out, nil
}

// PolicyEntryFrom renders one evaluated policy as its manifest entry — the
// frozen shape (S10, D12). It carries exactly what a step needs to re-decide
// with: identity, target, action, source, expiry, spec. Subject and created
// are the record's columns, not the step's question.
func PolicyEntryFrom(p policy.Policy) runtime.PolicyEntry {
	return runtime.PolicyEntry{
		ID:         p.ID,
		Type:       string(p.Type),
		TargetKind: string(p.Target.Kind),
		TargetID:   p.Target.ID,
		Action:     string(p.Action),
		Source:     string(p.Source),
		NotAfter:   p.NotAfter,
		Spec:       append(json.RawMessage(nil), p.Spec...),
	}
}

// PolicyEntriesAt is the ONE fetch a step gets (S10): the evaluated set read
// once, at one instant, by the caller assembling the step — and then
// PASS-THROUGH everywhere (manifest freeze, prompt render, replay). A second
// fetch is a second instant, and two instants is two sets the step could
// have seen; D12 is the rule that a step saw exactly one.
//
// The boundary refuses the whole read on any bad row (fail-closed, store.go),
// and the caller fails the step's construction on the reject — a step that
// cannot say what bound it must not run claiming to.
func PolicyEntriesAt(ctx context.Context, s *store.Store, at time.Time) ([]runtime.PolicyEntry, *policy.Reject) {
	boundary := policy.NewStore(newPolicyRepo(s))
	ps, rej := boundary.List(ctx, at)
	if rej != nil {
		return nil, rej
	}
	out := make([]runtime.PolicyEntry, 0, len(ps))
	for _, p := range ps {
		out = append(out, PolicyEntryFrom(p))
	}
	return out, nil
}
