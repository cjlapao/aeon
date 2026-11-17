// verify_service.go — S57 T8 stage 4: the daemon's verify family — the
// twelve human-door methods of protocol.VerifyStore over the real store.
//
// It is the daemon half of the verify family, and the ONLY place allowed
// to call the store's gate write boundary (AddVerifyCommand ·
// UpdateVerifyCommand · ArchiveVerifyCommand — the T8 guard
// TestOnlyTheServicesCallTheGateWriteBoundary pins the calls by string).
// The adapter knows both shapes — the store's and the wire's — and
// translates between them; the handlers above it only render.
//
// The store's refusals arrive as discriminated results (VerifyWrite's
// Refusal and Reason). The reason's sentence carries the store's own
// "refused: " prefix and the protocol renders ITS prefix too ("verify
// add refused: %s"), so the adapter strips the store's half into Detail —
// kept, the wire would read "refused: refused: …". The read doors without
// a refusal slot (list, show, runShow) build the same *protocol.Error
// themselves for a target that is not there: same family code, same
// closed kinds, the store's own sentence — the client branches on the
// kind exactly as it does on a write's refusal.
//
// verify.try reuses the loop's dispatch construction
// (buildVerifyActionService — ONE builder, two callers, so the manual
// run and the engine's run cannot drift on a seam) with the peer as the
// recorded actor and trigger "manual": the run lands in the ledger as
// loop_slug '' (the DDL's own exemption for manual runs), under the
// policy gate with the scrubbed environment, in the component's primary
// root — and the answer owes the person the two facts OD8 names: RanIn
// and ActiveLoops (the slugs still running in that root when the try
// started).

package daemon

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/cjlapao/aeon/internal/action"
	"github.com/cjlapao/aeon/internal/git"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/runtime"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/tools"
	"github.com/cjlapao/aeon/internal/verify"
)

// VerifyService is the daemon's verify half (see the file header).
type VerifyService struct {
	st  *store.Store
	log Logger
}

// NewVerifyService answers nil for a storeless daemon (V13) — the wiring
// leaves table.Verify nil and RegisterVerify leaves the twelve ABSENT,
// not registered-and-refusing.
func NewVerifyService(st *store.Store, log Logger) *VerifyService {
	if st == nil {
		return nil
	}
	if log == nil {
		log = discardLogger{}
	}
	return &VerifyService{st: st, log: log}
}

var _ protocol.VerifyStore = (*VerifyService)(nil)

// --- the shared gate-plane helpers (mergerule_service.go and dod_service.go
// call these too — one definition, three families) ------------------------------

// gateStoreActor is the wire's actor to the store's: the peer's identity
// is the connection's identity, and the store's closed actor set is the
// backstop (its own refusal names a non-human writer).
func gateStoreActor(a protocol.Actor) store.Actor {
	return store.Actor{Kind: a.Kind, ID: a.ID}
}

// gateDetail drops the store's "refused: " prefix from a refusal sentence
// — the protocol renders the family's prefix itself, and keeping both
// halves would read "refused: refused: …" on the wire.
func gateDetail(reason string) string {
	return strings.TrimPrefix(reason, "refused: ")
}

// --- the reads ------------------------------------------------------------------

// List answers verify.list: the component's gates (its own rows, or the
// effective set with its source), each with its last run and — when the
// daemon could ask the policy — its policy preview. The preview is the
// policy's own sentence (Verdict.Sentence, the one formatter), printed
// verbatim; an empty preview is the honest could-not-ask, not a pass.
func (s *VerifyService) List(ctx context.Context, q protocol.VerifyListQuery) (protocol.VerifyListResult, error) {
	comp, err := s.st.ReadComponent(q.ComponentID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return protocol.VerifyListResult{}, s.readError("list", "component-unknown",
				fmt.Sprintf("%s is not a component — a gate needs a node it can sit on", q.ComponentID))
		}
		return protocol.VerifyListResult{}, err
	}

	cv := CommandVerdictFor(s.st, s.log)
	preview := func(command string) string {
		if cv == nil {
			return ""
		}
		v, ok := cv(action.VerifierAgent, runtime.FrozenManifest{}, command)
		if !ok {
			return ""
		}
		return v.Sentence()
	}
	lastRun := func(id string) *protocol.VerifyRunLite {
		r, ok, err := s.st.LatestRunForCommand(id)
		if err != nil || !ok {
			return nil
		}
		return &protocol.VerifyRunLite{
			ID: r.ID, State: r.State, Reason: r.Reason, Trigger: r.Trigger,
			StartedAt: r.StartedAt, EndedAt: derefOrEmpty(r.EndedAt),
		}
	}

	res := protocol.VerifyListResult{ComponentID: comp.ID, Effective: q.Effective}
	res.Gates = make([]protocol.VerifyGateView, 0)

	if q.Effective {
		eg, err := s.st.EffectiveGates(comp.ID)
		if err != nil {
			return protocol.VerifyListResult{}, err
		}
		for _, g := range eg.Gates {
			v := gateView(g.Command)
			v.Source = g.Source
			v.SourceComponent = eg.SourceID
			v.LastRun = lastRun(g.Command.ID)
			v.PolicyPreview = preview(g.Command.Command)
			res.Gates = append(res.Gates, v)
		}
		return res, nil
	}

	cmds, err := s.st.ListVerifyCommands(store.VerifyCommandFilter{
		ComponentID:     comp.ID,
		IncludeArchived: q.IncludeArchived,
	})
	if err != nil {
		return protocol.VerifyListResult{}, err
	}
	for _, c := range cmds {
		if q.Requirement != "" && c.Requirement != q.Requirement {
			continue
		}
		v := gateView(c)
		v.LastRun = lastRun(c.ID)
		v.PolicyPreview = preview(c.Command)
		res.Gates = append(res.Gates, v)
	}
	return res, nil
}

// Show answers verify.show: the gate's row and its trail (the audit's
// id · op · who · when · summary; the before/after images are deliberately
// not crossed — the gate's full after-image is its own row).
func (s *VerifyService) Show(ctx context.Context, id string) (protocol.VerifyShowResult, error) {
	c, err := s.st.GetVerifyCommand(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return protocol.VerifyShowResult{}, s.readError("show", "command-unknown", fmt.Sprintf("%s is not a gate", id))
		}
		return protocol.VerifyShowResult{}, err
	}
	events, err := s.st.ListVerifyCommandEvents(store.VerifyCommandEventFilter{CommandID: id})
	if err != nil {
		return protocol.VerifyShowResult{}, err
	}
	out := protocol.VerifyShowResult{Gate: gateView(c)}
	out.Events = make([]protocol.VerifyEventView, 0, len(events))
	for _, e := range events {
		out.Events = append(out.Events, protocol.VerifyEventView{
			Op: e.Op, Revision: e.Revision, ActorKind: e.ActorKind, ActorID: e.ActorID,
			At: e.At, Summary: e.Summary,
		})
	}
	return out, nil
}

// Runs answers verify.runs: the run ledger, scoped (component root, loop
// slug, state) and capped. A loop slug alone resolves through the
// loop's own root — the same two-state resolution the merge plane uses
// (unknown and ambiguous are typed refusals, not an empty list).
func (s *VerifyService) Runs(ctx context.Context, q protocol.VerifyRunsQuery) (protocol.VerifyRunsResult, error) {
	f := store.VerifyRunFilter{State: q.State, Limit: limitOrDefault(q.Limit, 50)}
	if q.ComponentID != "" {
		comp, err := s.st.ReadComponent(q.ComponentID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return protocol.VerifyRunsResult{}, s.readError("runs", "component-unknown",
					fmt.Sprintf("%s is not a component — a gate needs a node it can sit on", q.ComponentID))
			}
			return protocol.VerifyRunsResult{}, err
		}
		f.ComponentRoot = comp.Root
	}
	if q.LoopSlug != "" {
		if f.ComponentRoot == "" {
			root, err := s.loopRootFor(q.LoopSlug)
			if err != nil {
				return protocol.VerifyRunsResult{}, err
			}
			f.ComponentRoot = root
		}
		f.LoopSlug = q.LoopSlug
	}
	runs, err := s.st.ListVerifyRuns(f)
	if err != nil {
		return protocol.VerifyRunsResult{}, err
	}
	out := protocol.VerifyRunsResult{Runs: make([]protocol.VerifyRunView, 0, len(runs))}
	for _, r := range runs {
		out.Runs = append(out.Runs, runView(r))
	}
	return out, nil
}

// RunShow answers verify.runShow: one run with its per-gate results — the
// door the TAILS cross the wire through (VG23's output discipline: the
// tails live in no list, no event, no bug, no audit).
func (s *VerifyService) RunShow(ctx context.Context, id string) (protocol.VerifyRunShowResult, error) {
	vr, results, err := s.st.GetVerifyRun(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return protocol.VerifyRunShowResult{}, s.readError("runShow", "run-unknown", fmt.Sprintf("%s is not a verify run", id))
		}
		return protocol.VerifyRunShowResult{}, err
	}
	out := protocol.VerifyRunShowResult{Run: runView(vr)}
	out.Results = make([]protocol.VerifyRunResultView, 0, len(results))
	for _, r := range results {
		out.Results = append(out.Results, protocol.VerifyRunResultView{
			Seq: r.Seq, Name: r.Name, Requirement: r.Requirement, Command: r.Command,
			ExitCode: r.ExitCode, Signal: r.Signal, TimedOut: r.TimedOut, WallMS: r.WallMS,
			StdoutBytes: r.StdoutBytes, StderrBytes: r.StderrBytes, Truncated: r.Truncated,
			StdoutSHA256: r.StdoutSHA256, StderrSHA256: r.StderrSHA256,
			StdoutTail: r.StdoutTail, StderrTail: r.StderrTail, State: r.State, Reason: r.Reason,
		})
	}
	return out, nil
}

// Vocabulary answers verify.vocabulary: the closed sets the client renders
// from — requirements and run states/reasons, the gate env allowlist NAMES
// (the daemon reads the tools package; no client types the list), and the
// tryStatement in the daemon's own words (the verifier's agent name and the
// policy's standing), so the UI's confirmation is the daemon's words.
func (s *VerifyService) Vocabulary(ctx context.Context) (protocol.VerifyVocabularyResult, error) {
	return protocol.VerifyVocabularyResult{
		Requirements: protocol.VerifyRequirementValues,
		States:       protocol.VerifyStateValues,
		Reasons:      protocol.VerifyReasonValues,
		EnvAllowlist: tools.GateEnvNames(),
		TryStatement: fmt.Sprintf("verify.try runs the named gate ONCE, in the component's primary root, "+
			"under the policy gate (agent %q) with a scrubbed environment, and records it as a manual run "+
			"you can read back with verify.runShow — it changes no gate, no rule, no loop", action.VerifierAgent),
	}, nil
}

// Templates answers verify.templates: the library (VG21's inert rows — a
// template does nothing until a person applies it).
func (s *VerifyService) Templates(ctx context.Context) (protocol.VerifyTemplatesResult, error) {
	tpl, err := s.st.ListVerifyTemplates()
	if err != nil {
		return protocol.VerifyTemplatesResult{}, err
	}
	out := protocol.VerifyTemplatesResult{Templates: make([]protocol.VerifyTemplateView, 0, len(tpl))}
	for _, t := range tpl {
		out.Templates = append(out.Templates, protocol.VerifyTemplateView{
			ID: t.ID, Stack: t.Stack, Name: t.Name, Command: t.Command,
			Requirement: t.Requirement, TimeoutS: t.TimeoutS, Origin: t.Origin, Version: t.Version,
		})
	}
	return out, nil
}

// --- the writes ------------------------------------------------------------------

// Add answers verify.add: one gate on the component, the peer as actor.
// The store's closed sets (slug, empty/too-long/NUL command, requirement,
// timeout) answer as a typed refusal, nothing written.
func (s *VerifyService) Add(ctx context.Context, actor protocol.Actor, in protocol.VerifyAddInput) (protocol.VerifyWriteResult, *protocol.VerifyRefusal, error) {
	w, err := s.st.AddVerifyCommand(gateStoreActor(actor), store.NewVerifyCommand{
		ComponentID: in.ComponentID, Name: in.Name, Command: in.Command,
		Requirement: in.Requirement, TimeoutS: in.TimeoutS, MemoryMB: in.MemoryMB,
		Position: in.Position, Enabled: in.Enabled,
	})
	if err != nil {
		return protocol.VerifyWriteResult{}, nil, err
	}
	if !w.OK() {
		return protocol.VerifyWriteResult{}, &protocol.VerifyRefusal{Kind: string(w.Refusal), Detail: gateDetail(w.Reason)}, nil
	}
	return gateWriteResult(w.Command), nil, nil
}

// gateWriteResult is the one shape verify.add and verify.update answer
// with (S57 T10, VG20): the gate view, plus the scrubbed-environment
// note when the STORED command carries a token shape. The note is the
// daemon's sentence, stamped by the daemon, printed verbatim by every
// surface — a person who stored `ghp_…` in a command string is about to
// run a gate with a secret the scrub will mask in the tail but cannot
// mask in the exit code, the test output, or the process list.
func gateWriteResult(row store.VerifyCommand) protocol.VerifyWriteResult {
	res := protocol.VerifyWriteResult{Gate: gateView(row)}
	if verify.CommandIsTokenShaped(row.Command) {
		res.Note = protocol.VerifyEnvNoteSentence
	}
	return res
}

// Update answers verify.update: the named fields on one gate. The
// archived family refuses here (history is not edited in place — restore
// first); the store's no-op check keeps an unchanged update quiet.
func (s *VerifyService) Update(ctx context.Context, actor protocol.Actor, in protocol.VerifyUpdateInput) (protocol.VerifyWriteResult, *protocol.VerifyRefusal, error) {
	w, err := s.st.UpdateVerifyCommand(gateStoreActor(actor), in.ID, store.VerifyCommandUpdate{
		Command: in.Command, Requirement: in.Requirement, TimeoutS: in.TimeoutS,
		MemoryMB: in.MemoryMB, Position: in.Position, Enabled: in.Enabled,
	})
	if err != nil {
		return protocol.VerifyWriteResult{}, nil, err
	}
	if !w.OK() {
		return protocol.VerifyWriteResult{}, &protocol.VerifyRefusal{Kind: string(w.Refusal), Detail: gateDetail(w.Reason)}, nil
	}
	return gateWriteResult(w.Command), nil, nil
}

// Archive answers verify.archiving: the gate's row is kept (history), its
// gates stop running. Archiving an archived gate is a no-op success — the
// store's honest double-state.
func (s *VerifyService) Archive(ctx context.Context, actor protocol.Actor, id string) (protocol.VerifyWriteResult, *protocol.VerifyRefusal, error) {
	w, err := s.st.ArchiveVerifyCommand(gateStoreActor(actor), id)
	if err != nil {
		return protocol.VerifyWriteResult{}, nil, err
	}
	if !w.OK() {
		return protocol.VerifyWriteResult{}, &protocol.VerifyRefusal{Kind: string(w.Refusal), Detail: gateDetail(w.Reason)}, nil
	}
	return protocol.VerifyWriteResult{Gate: gateView(w.Command)}, nil, nil
}

// Restore answers verify.restoring: the gate runs again. Restoring a
// live gate is a no-op success (the same honest double-state).
func (s *VerifyService) Restore(ctx context.Context, actor protocol.Actor, id string) (protocol.VerifyWriteResult, *protocol.VerifyRefusal, error) {
	w, err := s.st.RestoreVerifyCommand(gateStoreActor(actor), id)
	if err != nil {
		return protocol.VerifyWriteResult{}, nil, err
	}
	if !w.OK() {
		return protocol.VerifyWriteResult{}, &protocol.VerifyRefusal{Kind: string(w.Refusal), Detail: gateDetail(w.Reason)}, nil
	}
	return protocol.VerifyWriteResult{Gate: gateView(w.Command)}, nil, nil
}

// ApplyTemplate answers verify.applyTemplate: the named templates copied
// onto the component as origin-'template' gates — one transaction, every
// collision checked before any write (VG21's apply, the human door).
func (s *VerifyService) ApplyTemplate(ctx context.Context, actor protocol.Actor, componentID string, templateIDs []string) (protocol.VerifyApplyResult, *protocol.VerifyRefusal, error) {
	a, err := s.st.ApplyVerifyTemplates(gateStoreActor(actor), componentID, templateIDs)
	if err != nil {
		return protocol.VerifyApplyResult{}, nil, err
	}
	if !a.OK() {
		return protocol.VerifyApplyResult{}, &protocol.VerifyRefusal{Kind: string(a.Refusal), Detail: gateDetail(a.Reason)}, nil
	}
	out := protocol.VerifyApplyResult{Gates: make([]protocol.VerifyGateView, 0, len(a.Commands))}
	for _, c := range a.Commands {
		out.Gates = append(out.Gates, gateView(c))
	}
	return out, nil, nil
}

// Try answers verify.try: ONE gate, run once, in the component's primary
// root, under the policy gate with the scrubbed environment, recorded as
// a manual run under the peer's identity. The loop's dispatch and this
// door build the action through buildVerifyActionService — the same
// seams, the same verdict source, one construction (the T8 spec: verify.try
// never bypasses CommandVerdictFor).
func (s *VerifyService) Try(ctx context.Context, actor protocol.Actor, componentID, gateID string) (protocol.VerifyTryResult, *protocol.VerifyRefusal, error) {
	gate, err := s.st.GetVerifyCommand(gateID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return protocol.VerifyTryResult{}, &protocol.VerifyRefusal{Kind: "command-unknown", Detail: fmt.Sprintf("%s is not a gate", gateID)}, nil
		}
		return protocol.VerifyTryResult{}, nil, err
	}
	if gate.ComponentID != componentID {
		return protocol.VerifyTryResult{}, nil, protocol.ErrBadParams{Err: fmt.Errorf(
			"gate %s belongs to component %s, not %s — the try names the gate's own component", gateID, gate.ComponentID, componentID)}
	}
	if gate.ArchivedAt != nil {
		return protocol.VerifyTryResult{}, &protocol.VerifyRefusal{
			Kind: "command-archived", Detail: fmt.Sprintf("%s is archived — history is not edited in place; restore it first", gateID),
		}, nil
	}
	comp, err := s.st.ReadComponent(componentID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return protocol.VerifyTryResult{}, &protocol.VerifyRefusal{Kind: "component-unknown",
				Detail: fmt.Sprintf("%s is not a component — a gate needs a node it can sit on", componentID)}, nil
		}
		return protocol.VerifyTryResult{}, nil, err
	}
	if comp.Root == "" {
		// CT3: a node without a root cannot run — the typed refusal the
		// spec names, before anything is executed.
		return protocol.VerifyTryResult{}, &protocol.VerifyRefusal{
			Kind: "no-root", Detail: fmt.Sprintf("%s has no root to run in — a gate runs in the tree it sits on", componentID),
		}, nil
	}

	// OD8's second fact, read BEFORE the run: the slugs still running in
	// this root when the try started (the race, named).
	loops, err := s.st.ListLoops(store.ComponentScope{ComponentRoot: comp.Root})
	if err != nil {
		return protocol.VerifyTryResult{}, nil, err
	}
	activeLoops := make([]string, 0)
	for _, l := range loops {
		if l.Status == "active" {
			activeLoops = append(activeLoops, l.Slug)
		}
	}
	sort.Strings(activeLoops)

	svc := buildVerifyActionService(s.st, s.log, git.NewClient(daemonGitEnv()))
	out, err := svc.Run(ctx, action.VerifyRunInput{
		Dir: comp.Root, Loop: "", Task: "", Agent: "",
		Gate: gateID, Trigger: "manual",
		Actor: action.RunActor{Kind: actor.Kind, ID: actor.ID},
	})
	if err != nil {
		return protocol.VerifyTryResult{}, nil, err
	}

	vr, results, err := s.st.GetVerifyRun(out.RunID)
	if err != nil {
		return protocol.VerifyTryResult{}, nil, err
	}
	res := protocol.VerifyTryResult{Run: runView(vr), RanIn: comp.Root, ActiveLoops: activeLoops}
	res.Results = make([]protocol.VerifyRunResultView, 0, len(results))
	for _, r := range results {
		res.Results = append(res.Results, runResultView(r))
	}
	return res, nil, nil
}

// --- the small pieces -----------------------------------------------------------

// readError builds the typed refusal the read doors owe for a target that
// is not there (no refusal slot in the seam signature — the same family,
// kind and sentence the write path's refusal carries).
func (s *VerifyService) readError(verb, kind, detail string) error {
	e := protocol.ErrVerifyRefused.WithWhat("verify %s refused: %s", verb, detail)
	e.Data.Kind = kind
	return e
}

// loopRootFor resolves a loop slug to its root: exactly one loop with the
// slug ⇒ its root; none ⇒ unknown; more than one ⇒ the slug is ambiguous
// (unique per root, never global). Both refusals are typed — an empty
// answer is not honest here, it would read as "no runs".
func (s *VerifyService) loopRootFor(slug string) (string, error) {
	loops, err := s.st.LoopsBySlug(slug)
	if err != nil {
		return "", err
	}
	switch len(loops) {
	case 0:
		e := protocol.ErrVerifyRefused.WithWhat("verify runs refused: %s is not a loop", slug)
		e.Data.Kind = "loop-unknown"
		return "", e
	case 1:
		return loops[0].ComponentRoot, nil
	default:
		e := protocol.ErrVerifyRefused.WithWhat("verify runs refused: %s is a loop slug in %d roots — a slug is unique per root, not global; name the component", slug, len(loops))
		e.Data.Kind = "loop-slug-ambiguous"
		return "", e
	}
}

// gateView is the store's gate row to the wire's view (the list-only
// fields stay zero — a row read off-component has no source, and the
// preview and last run are the list's, not the row's).
func gateView(c store.VerifyCommand) protocol.VerifyGateView {
	return protocol.VerifyGateView{
		ID: c.ID, ComponentID: c.ComponentID, Name: c.Name, Command: c.Command,
		Requirement: c.Requirement, TimeoutS: c.TimeoutS, MemoryMB: c.MemoryMB,
		Position: c.Position, Enabled: c.Enabled, Origin: c.Origin, TemplateID: derefOrEmpty(c.TemplateID),
		ArchivedAt: derefOrEmpty(c.ArchivedAt), CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
}

func runView(r store.VerifyRun) protocol.VerifyRunView {
	return protocol.VerifyRunView{
		ID: r.ID, LoopSlug: r.LoopSlug, ComponentRoot: r.ComponentRoot, ComponentID: r.ComponentID,
		Task: r.Task, Position: r.Position, Attempt: r.Attempt, HeadSHA: r.HeadSHA,
		Trigger: r.Trigger, ActorKind: r.ActorKind, ActorID: r.ActorID,
		StartedAt: r.StartedAt, EndedAt: derefOrEmpty(r.EndedAt),
		State: r.State, Reason: r.Reason, Summary: r.Summary, Digest: r.Digest,
	}
}

func runResultView(r store.VerifyRunResult) protocol.VerifyRunResultView {
	return protocol.VerifyRunResultView{
		Seq: r.Seq, Name: r.Name, Requirement: r.Requirement, Command: r.Command,
		ExitCode: r.ExitCode, Signal: r.Signal, TimedOut: r.TimedOut, WallMS: r.WallMS,
		StdoutBytes: r.StdoutBytes, StderrBytes: r.StderrBytes, Truncated: r.Truncated,
		StdoutSHA256: r.StdoutSHA256, StderrSHA256: r.StderrSHA256,
		StdoutTail: r.StdoutTail, StderrTail: r.StderrTail, State: r.State, Reason: r.Reason,
	}
}

// derefOrEmpty renders a *string as "" for nil (the wire's omitempty
// fields are the honest absent; the package's derefString copies the
// pointer and emptyIfNil normalises slices — this renders the value).
func derefOrEmpty(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// limitOrDefault caps a list read: zero or negative means the default.
func limitOrDefault(limit, def int) int {
	if limit <= 0 {
		return def
	}
	return limit
}
