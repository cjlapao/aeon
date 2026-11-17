package daemon

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/learnings"
	"github.com/cjlapao/aeon/internal/policy"
)

// learnings_board.go — the directed board question (S28 T7). One agent asks
// another's answer skill (T6) through the real, already-built agent-service
// invocation; the answer, on completion, is filed as a learning whose
// provenance is `relayed` (design M1) — the one write permitted that provenance,
// never hand-authored (the public Append refuses it). The board is purely a
// caller of the mechanism: it adds zero lines to any chain or cycle logic
// (design M2 — the depth bound is S14's own, already built).
//
// Why the board's three agentsvc things cross as funcs and a minimal view, not
// as agentsvc types. The daemon must not import internal/agentsvc (AS11's
// deletability — `rm -r internal/agentsvc` leaves this package building; the
// same reason AgentInvoke and Agents are func seams the entry point wires, not
// agentsvc fields the daemon holds). So the invoke, the task it returns and the
// completion cross here as injected funcs and a small view of a task, not as the
// agentsvc types. cmd/aeon wires the real ones; a test fakes them. The board
// decides nothing agentsvc cannot remember — it only asks, and it only files.
//
// What the board is, in one sentence. It is the answer of a question addressed
// to a named agent (D8, N2 — never a broadcast, never unaddressed), and the
// record that the answer came by the board, not by a person's word or a loop's
// seeing. The record is the relayed learning; the answer is the body; the
// channel is the provenance.
//
// The completion path. An async task is a suspension, not a wait (P2, AS4):
// AskBoard returns while the task is still waiting, and the answer arrives
// later, on resume. So AskBoard files the learning only when the task it
// created is already completed; a waiting task files nothing here, and the
// daemon's resume path calls fileRelayed when the completion arrives — the same
// Task-completion the agent-ask seam (AgentInvoke) settles. The gate test
// exercises fileRelayed through a completed task; the resume wiring is the
// daemon's, recorded in the task Notes as the seam this slice leaves for the
// next (it needs S14's task-completion handler to name the board's fileRelayed).

const boardTaskCompleted = "completed"

// boardAsk is the one ask the board makes: the answer skill on a named agent,
// with the text. It carries no skill field — scope 2 is structural, not a
// convention: the skill is the answer skill, always, known by the invoke seam,
// and there is no field a caller could set to name another. It carries no
// broadcast (D8): a single named callee, refused before the invoke if empty.
// frozen and evaluatedAt build the SkillAccess judge the invoke needs (D12):
// the caller's own frozen set, judged at the instant the ask is made — a
// hidden clock is how an expired rule quietly revives.
type boardAsk struct {
	callerAgent string
	calleeAgent string
	question    string
	frozen      []policy.Policy
	evaluatedAt time.Time
}

// boardTask is the daemon's view of the created task. The daemon must not
// import agentsvc (AS11), so the invoke seam hands back this minimal view —
// the id, the status, and, when completed, the result text the board files as
// a relayed learning — not the agentsvc row. A waiting task carries no result
// yet (its text arrives with the completion); a completed task's text is the
// answer.
type boardTask struct {
	ID          int64
	Status      string
	ResultText  string
	CallerAgent string
}

// Board is the directed-question capability. Every dependency is injected — the
// daemon must not import agentsvc (AS11), so the invoke and the relayed write
// are the entry point's to wire, and a test's to fake. A nil invoke is reported
// by name (an unwired board states the absence rather than pretending, the
// AgentInvoke discipline), never a panic.
type Board struct {
	// invoke is the seam to agentsvc's Invoker. It names the callee, the
	// answer skill (always — the boardAsk has no skill field) and the text, and
	// returns the created task's view. Nil unless the entry point wired it.
	invoke func(ctx context.Context, a boardAsk) (boardTask, error)
	// relayed is the one write permitted to set provenance:relayed (M1):
	// learnings.AppendRelayedAs with the {system, "learnings-board"}
	// actor — the wiring contract since S52 T1: only a system-kind actor
	// files a relayed learning (LM4), and the board hook is that actor's
	// one production bearer. Reached through the same one write path the
	// socket and CLI speak through (P34). Nil unless wired; as shipped,
	// test-injected only — the production resume wiring is the seam S28
	// left open and S52 does not close it (recorded in the T1 Notes).
	relayed func(ctx context.Context, in learnings.AppendInput) (learnings.Learning, *learnings.Refusal)
	// projectRoot resolves the asking loop's project_root — M6's scope: the
	// learning is scoped to the project, not the answering agent.
	projectRoot func(ctx context.Context, callerAgent string) (string, error)
	// frozen resolves the caller's frozen policy set (D12) and now the instant
	// the ask is judged against; the SkillAccess judge cannot run without both.
	frozen func(ctx context.Context, callerAgent string) ([]policy.Policy, error)
	now    func() time.Time
}

// AskBoard is the directed question: one agent asks another's answer skill, on
// a named agent, with a question. It refuses an empty target before it calls
// the invoke at all (a call with no agent is not a question, it is a broadcast,
// which the board is not — D8, N2), then invokes, and, when the task it created
// is already completed, files the answer as a relayed learning (M1, M6).
//
// There is no skill parameter — scope 2 is structural: the board's only ask is
// the answer skill, and no signature field could name another. The refusal a
// callee with no card returns is surfaced verbatim (the invoke's own words),
// never reworded — a second message would be a second truth.
func (b *Board) AskBoard(ctx context.Context, callerAgent, targetAgent, question string) (boardTask, error) {
	if strings.TrimSpace(targetAgent) == "" {
		return boardTask{}, fmt.Errorf("a board question names its target — a call with no agent is a broadcast, which the board is not (D8, N2); refused before it is made")
	}
	if b.invoke == nil {
		return boardTask{}, fmt.Errorf("the board's invoke is not wired — no agent can be asked (the daemon must not import agentsvc, AS11; the entry point wires the seam)")
	}
	now := time.Now()
	if b.now != nil {
		now = b.now()
	}
	frozen, err := b.frozen(ctx, callerAgent)
	if err != nil {
		return boardTask{}, fmt.Errorf("resolve the caller's frozen policy set (the SkillAccess judge needs it, D12): %w", err)
	}
	t, err := b.invoke(ctx, boardAsk{
		callerAgent: callerAgent,
		calleeAgent: targetAgent,
		question:    question,
		frozen:      frozen,
		evaluatedAt: now,
	})
	if err != nil {
		// The invoke's refusal — a missing card, a denied skill, a no-clock
		// judge — surfaces verbatim. A second, reworded message would be a
		// second truth; the whole point of the refusal is that it names the
		// thing refused (DB19).
		return boardTask{}, err
	}
	// On completion, file the answer as a relayed learning (M1, M6). A waiting
	// task files nothing here: its completion arrives later, on resume (P2,
	// AS4 — suspension is exiting), and the daemon's resume path calls
	// fileRelayed. This call has already returned; it is not a wait.
	if t.Status == boardTaskCompleted {
		if _, err := b.fileRelayed(ctx, t); err != nil {
			return t, err
		}
	}
	return t, nil
}

// fileRelayed is the completion's one write: the answer, on completion, becomes
// a relayed learning (M1), scoped to the asking loop's project (M6). It is the
// only caller permitted to set provenance:relayed — the public Append refuses
// it, and this path is the write-protection's exception in one place, not a
// second authoring door. A completed task with no result text files nothing:
// there is no answer to remember.
func (b *Board) fileRelayed(ctx context.Context, t boardTask) (learnings.Learning, error) {
	if strings.TrimSpace(t.ResultText) == "" {
		return learnings.Learning{}, fmt.Errorf("a completed task with no result text has nothing to file — the answer is empty")
	}
	if b.relayed == nil {
		return learnings.Learning{}, fmt.Errorf("the board's relayed write is not wired — the answer cannot be filed (the entry point wires the one write path, P34)")
	}
	scopeKey, err := b.projectRoot(ctx, t.CallerAgent)
	if err != nil {
		return learnings.Learning{}, fmt.Errorf("resolve the asking loop's project_root (a project-scoped learning names its root, M6): %w", err)
	}
	if strings.TrimSpace(scopeKey) == "" {
		return learnings.Learning{}, fmt.Errorf("the asking loop has no project_root — a project-scoped learning names its root, and a learning with no owner is no learning")
	}
	lr, r := b.relayed(ctx, learnings.AppendInput{
		Scope:      learnings.ScopeProject,
		ScopeKey:   scopeKey,
		Provenance: learnings.ProvenanceRelayed,
		Body:       t.ResultText,
	})
	if r != nil {
		return lr, fmt.Errorf("the relayed write was refused: %s", r.Reason)
	}
	return lr, nil
}
