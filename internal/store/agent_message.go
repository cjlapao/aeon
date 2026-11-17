package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cjlapao/aeon/internal/model"
)

// agent_message.go is S70's mailbox: the addressed message from one agent to
// another agent of the SAME loop, and its answer. The rulings it carries are
// MB2 (loop-scoped, addressed, never a broadcast) and MB3 (rows are the
// conversation: append-only, a thread is its own header, an answer is a link
// and never an edit).
//
// What this file deliberately does NOT own: who may ask (the runtime checks
// the pinned blueprint's lanes and the frozen policy set before it calls
// QueueAgentMessage — MB1/MB2), how delivery composes into a model turn (the
// runtime's drain join, MB4), and any event (the protocol's, MB6). The store
// holds facts and refuses lies, in the sentences the caller can act on.
//
// The caps (MB3) are counted HERE, at the door that writes, not trusted from
// a caller above: an uncapped model turn is a turn that spam-asks, and a
// refusal that names only "too many" is not a refusal worth reading (AGENTS
// §5). The numbers: a body at most MaxAgentMessageBodyBytes (the steer's
// number, shared because it is the same kind of payload — one addressed
// thing one person/agent typed), MaxMessagesPerThread messages in a thread,
// MaxMessagesPerStep messages authored by one step, and MaxThreadsPerLoop
// threads in a loop's LIFETIME — a lifetime bound, not an active window: a
// long loop should outlive its conversations, not be blocked by them (the
// spec's self-review catch, recorded in the brief §5 #7).

// The mailbox caps (MB3). Each refusal names the number and the thing at it.
const (
	MaxAgentMessageBodyBytes = 32 << 10
	MaxMessagesPerThread     = 16
	MaxMessagesPerStep       = 8
	MaxThreadsPerLoop        = 64
)

// AgentMessage is one row of `agent_message`.
//
// ResultOf is 0 (column NULL) until this row is the answer to a specific
// message; DeliveredAt is empty until the drain stamps it exactly once.
// Both are the third state (P31): "not yet" is a fact the row carries, never
// an absence a reader guesses at.
type AgentMessage struct {
	ID int64 `json:"id"`

	LoopSlug      string `json:"loopSlug"`
	ComponentRoot string `json:"componentRoot"`

	// Thread is the id of the thread's first ask — its own header. A reply
	// carries the SAME id; the codec guarantees it, and the readers require it.
	Thread      int64  `json:"thread"`
	FromAgent   string `json:"fromAgent"`
	ToAgent     string `json:"toAgent"`
	FromStepID  int64  `json:"fromStepId"`
	Body        string `json:"body"`
	ExpectReply bool   `json:"expectReply"`

	// ResultOf names the message this row ANSWERS, and only a message that
	// expected an answer may be answered (the column's CHECK pair). 0 means
	// this row asks (or states); it is never read as a broken link.
	ResultOf int64 `json:"resultOf,omitempty"`

	// DeliveredAt is the ONE delivery stamp: when the callee's boundary
	// drained this message into its turn. Empty until then; a second delivery
	// attempt is refused naming the row and its stamp, because a re-stamped
	// timestamp is how a double delivery hides (the steer's precedent).
	DeliveredAt string `json:"deliveredAt,omitempty"`

	// DeliveredToStepID names WHICH step's boundary took the row — the other
	// half of the delivery pair (the CHECK binds them: both set together by
	// the same statement). It is what lets a step's completion ask the ROWS
	// which replies it owes instead of trusting memory (rows are the state).
	// 0 (column NULL) means never delivered.
	DeliveredToStepID int64 `json:"deliveredToStepId,omitempty"`

	CreatedAt string `json:"createdAt"`
}

// NewAgentMessage is everything a caller may say when it queues a message.
// Thread 0 begins a new conversation (the row becomes its own header);
// Thread n joins an existing one. ResultOf must name a message INSIDE the
// thread that expects an answer — the codec reads that row to prove both,
// so a caller cannot answer a message that asked nothing or link a thread
// from another loop by accident or by lie.
type NewAgentMessage struct {
	LoopSlug      string
	ComponentRoot string

	FromAgent  string
	ToAgent    string
	FromStepID int64

	Body        string
	ExpectReply bool

	// Thread 0 = begin one; otherwise the thread this message joins. When
	// ResultOf is set and Thread is 0, the thread is INHERITED from the
	// answered message — one honest way to answer, zero ways to mismatch.
	Thread    int64
	ResultOf  int64
	CreatedAt string // empty means the store's clock, as every other row does
}

// agentMessageColumns is the one column list every read of this table uses.
const agentMessageColumns = `id, loop_slug, component_root, thread, from_agent, to_agent,
	from_step_id, body, expect_reply, COALESCE(result_of, 0), COALESCE(delivered_at, ''),
	COALESCE(delivered_to_step_id, 0), created_at`

func scanAgentMessage(row interface{ Scan(...any) error }) (AgentMessage, error) {
	var m AgentMessage
	var expect int
	if err := row.Scan(&m.ID, &m.LoopSlug, &m.ComponentRoot, &m.Thread, &m.FromAgent, &m.ToAgent,
		&m.FromStepID, &m.Body, &expect, &m.ResultOf, &m.DeliveredAt, &m.DeliveredToStepID,
		&m.CreatedAt); err != nil {
		return AgentMessage{}, err
	}
	m.ExpectReply = expect != 0
	return m, nil
}

// QueueAgentMessage files one addressed message and returns the row it wrote.
//
// # The contract
//
//   - Addressed, never self, never empty (AT3's store half — the lane check
//     is the runtime's; the store's half is that a message speaks to exactly
//     one OTHER named agent, D8's shape at the column level).
//   - A reply (ResultOf set) must name a message that EXISTS, in the SAME
//     loop, inside the thread it joins, and that EXPECTED an answer. Every
//     mismatch refuses naming both sides of what it compared.
//   - The caps (this file's header). Counted against the rows, at the door,
//     in the writing transaction — a count taken above and then raced is a
//     cap that leaks.
//   - A thread's first row bootstraps thread = its own id inside the SAME
//     transaction. That UPDATE is the only write to that column anywhere,
//     and it happens exactly once per row, before this function returns.
//
// # The crash argument
//
// The message is durable the instant this returns, and "delivered" is a stamp
// the drain must ask for — so a crash after Queue and before the callee's
// boundary leaves the row pending for the next process, exactly as a steer
// does. There is no goroutine, no queue in memory (AS5): the row IS the
// queue.
func (s *Store) QueueAgentMessage(in NewAgentMessage) (AgentMessage, error) {
	from := strings.TrimSpace(in.FromAgent)
	to := strings.TrimSpace(in.ToAgent)
	body := strings.TrimSpace(in.Body)
	if from == "" {
		return AgentMessage{}, errors.New("store: refusing to queue a mailbox message with no sender: every row names the agent it came from")
	}
	if to == "" {
		return AgentMessage{}, fmt.Errorf("store: refusing to queue a mailbox message from %s with no addressee: the mailbox is addressed, never broadcast", from)
	}
	if from == to {
		return AgentMessage{}, fmt.Errorf("store: refusing to queue a mailbox message from %s to %s: an agent asking itself is not a conversation — the loop's other lanes are the peers (D8)", from, to)
	}
	if body == "" {
		return AgentMessage{}, fmt.Errorf("store: refusing to queue a mailbox message from %s to %s with an empty body: a message that says nothing answers nothing", from, to)
	}
	if len(body) > MaxAgentMessageBodyBytes {
		return AgentMessage{}, fmt.Errorf("store: refusing to queue a mailbox message from %s to %s: the body is %d bytes and the cap is %d — a larger payload is a file, and files ride attachments (S69), not bodies",
			from, to, len(body), MaxAgentMessageBodyBytes)
	}
	if strings.TrimSpace(in.LoopSlug) == "" || strings.TrimSpace(in.ComponentRoot) == "" {
		return AgentMessage{}, fmt.Errorf("store: refusing to queue a mailbox message from %s to %s with no loop scope: the loop is the conversation's boundary (MB2)", from, to)
	}

	tx, err := s.w.Begin()
	if err != nil {
		return AgentMessage{}, fmt.Errorf("store: queue a mailbox message from %s to %s: %w", from, to, err)
	}
	defer func() { _ = tx.Rollback() }()

	// The authoring step must exist AND belong to the loop it is speaking
	// for: the per-step cap counts against it, its agent is the message's
	// sender's home, and a step from another loop authoring into this one is
	// the cross-loop lie the thread join already refuses — the FK would only
	// answer "FOREIGN KEY constraint failed", so this names step and scopes.
	var stepSlug, stepRoot, stepAgent string
	err = tx.QueryRow(`SELECT loop_slug, component_root, agent FROM step WHERE id = ?`, in.FromStepID).
		Scan(&stepSlug, &stepRoot, &stepAgent)
	if err == sql.ErrNoRows {
		return AgentMessage{}, fmt.Errorf("store: cannot file a mailbox message authored by step %d: no such step: %w", in.FromStepID, ErrNotFound)
	} else if err != nil {
		return AgentMessage{}, fmt.Errorf("store: look up step %d to file its mailbox message: %w", in.FromStepID, err)
	}
	if stepSlug != in.LoopSlug || stepRoot != in.ComponentRoot {
		return AgentMessage{}, fmt.Errorf("store: refusing a mailbox message scoped to %s/%s authored by step %d, which belongs to %s/%s — a loop's turn speaks only for its own loop (MB2)",
			in.LoopSlug, in.ComponentRoot, in.FromStepID, stepSlug, stepRoot)
	}

	thread := in.Thread
	if in.ResultOf != 0 {
		// The answered message must exist, live in the SAME loop, and have
		// expected an answer. Every refusal names both sides compared.
		var answeredThread int64
		var answeredScopeSlug, answeredScopeRoot string
		var answeredExpect int
		err := tx.QueryRow(`SELECT thread, loop_slug, component_root, expect_reply FROM agent_message WHERE id = ?`, in.ResultOf).
			Scan(&answeredThread, &answeredScopeSlug, &answeredScopeRoot, &answeredExpect)
		if err == sql.ErrNoRows {
			return AgentMessage{}, fmt.Errorf("store: refusing to file a message from %s to %s answering message %d: no such message: %w", from, to, in.ResultOf, ErrNotFound)
		} else if err != nil {
			return AgentMessage{}, fmt.Errorf("store: look up message %d to answer it: %w", in.ResultOf, err)
		}
		if answeredScopeSlug != in.LoopSlug || answeredScopeRoot != in.ComponentRoot {
			return AgentMessage{}, fmt.Errorf("store: refusing to file a message answering %d in loop %s/%s: message %d belongs to %s/%s — a thread does not cross loops (MB2)",
				in.ResultOf, in.LoopSlug, in.ComponentRoot, in.ResultOf, answeredScopeSlug, answeredScopeRoot)
		}
		if answeredExpect != 1 {
			return AgentMessage{}, fmt.Errorf("store: refusing to file a message from %s answering %s's message %d: that message did not expect an answer — answer what asked, or ask in the thread instead",
				from, answeredScopeSlug, in.ResultOf)
		}
		if thread == 0 {
			thread = answeredThread // the one honest inheritance
		} else if thread != answeredThread {
			return AgentMessage{}, fmt.Errorf("store: refusing to file a message from %s answering message %d inside thread %d: message %d lives in thread %d — an answer joins the conversation it answers",
				from, in.ResultOf, thread, in.ResultOf, answeredThread)
		}
	}

	if thread == 0 {
		// Beginning a conversation: the loop's lifetime bound and the step's
		// anti-spam bound, counted against the rows in THIS transaction.
		var loops, perStep int
		if err := tx.QueryRow(`SELECT count(DISTINCT thread) FROM agent_message WHERE loop_slug = ? AND component_root = ?`,
			in.LoopSlug, in.ComponentRoot).Scan(&loops); err != nil {
			return AgentMessage{}, fmt.Errorf("store: count the threads of loop %s/%s: %w", in.LoopSlug, in.ComponentRoot, err)
		}
		if loops >= MaxThreadsPerLoop {
			return AgentMessage{}, fmt.Errorf("store: refusing a new mailbox thread in loop %s/%s: it already holds %d threads and the lifetime cap is %d — long conversations end; loops outlive them",
				in.LoopSlug, in.ComponentRoot, loops, MaxThreadsPerLoop)
		}
		if err := tx.QueryRow(`SELECT count(*) FROM agent_message WHERE from_step_id = ?`, in.FromStepID).Scan(&perStep); err != nil {
			return AgentMessage{}, fmt.Errorf("store: count the messages authored by step %d: %w", in.FromStepID, err)
		}
		if perStep >= MaxMessagesPerStep {
			return AgentMessage{}, fmt.Errorf("store: refusing a new mailbox thread from step %d: it has already authored %d messages and the cap is %d — a turn cannot spam the mailbox",
				in.FromStepID, perStep, MaxMessagesPerStep)
		}
		createdAt := in.CreatedAt
		if createdAt == "" {
			createdAt = s.Now()
		}
		res, err := tx.Exec(`
			INSERT INTO agent_message (loop_slug, component_root, thread, from_agent, to_agent, from_step_id,
				body, expect_reply, result_of, created_at)
			VALUES (?,?,?,?,?,?,?,?,?,?)`,
			in.LoopSlug, in.ComponentRoot, 0, from, to, in.FromStepID, body, b2i(in.ExpectReply), nullableID(in.ResultOf), createdAt)
		if err != nil {
			return AgentMessage{}, fmt.Errorf("store: file a new mailbox thread from %s to %s: %w", from, to, err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return AgentMessage{}, fmt.Errorf("store: read the id of the new mailbox thread from %s to %s: %w", from, to, err)
		}
		// The bootstrap: a conversation is its own header. This is the ONLY
		// write to thread anywhere, it runs once per row, inside this insert's
		// transaction, before the row is visible to any reader.
		if _, err := tx.Exec(`UPDATE agent_message SET thread = id WHERE id = ? AND thread = 0`, id); err != nil {
			return AgentMessage{}, fmt.Errorf("store: bootstrap the header of the new mailbox thread %d: %w", id, err)
		}
		if err := tx.Commit(); err != nil {
			return AgentMessage{}, fmt.Errorf("store: commit the new mailbox thread from %s to %s: %w", from, to, err)
		}
		return s.agentMessageByID(id, fmt.Sprintf("store: read back the new mailbox thread from %s to %s", from, to))
	}

	// Joining an existing thread: it must exist in THIS loop (naming both
	// sides when it does not), be within its message cap, and the authoring
	// step within its own.
	var inLoop int
	if err := tx.QueryRow(`SELECT count(*) FROM agent_message WHERE id = ? AND loop_slug = ? AND component_root = ? AND thread = ?`,
		thread, in.LoopSlug, in.ComponentRoot, thread).Scan(&inLoop); err != nil {
		return AgentMessage{}, fmt.Errorf("store: look up thread %d in loop %s/%s: %w", thread, in.LoopSlug, in.ComponentRoot, err)
	}
	if inLoop == 0 {
		return AgentMessage{}, fmt.Errorf("store: refusing to join thread %d in loop %s/%s: no such thread in this loop — a thread is addressed to agents of one loop (MB2)",
			thread, in.LoopSlug, in.ComponentRoot)
	}
	var inThread, perStep int
	if err := tx.QueryRow(`SELECT count(*) FROM agent_message WHERE thread = ?`, thread).Scan(&inThread); err != nil {
		return AgentMessage{}, fmt.Errorf("store: count the messages of thread %d: %w", thread, err)
	}
	if inThread >= MaxMessagesPerThread {
		return AgentMessage{}, fmt.Errorf("store: refusing to add to thread %d: it holds %d messages and the cap is %d — end the conversation or begin a new thread",
			thread, inThread, MaxMessagesPerThread)
	}
	if err := tx.QueryRow(`SELECT count(*) FROM agent_message WHERE from_step_id = ?`, in.FromStepID).Scan(&perStep); err != nil {
		return AgentMessage{}, fmt.Errorf("store: count the messages authored by step %d: %w", in.FromStepID, err)
	}
	if perStep >= MaxMessagesPerStep {
		return AgentMessage{}, fmt.Errorf("store: refusing a message into thread %d from step %d: that step has authored %d messages and the cap is %d — a turn cannot spam the mailbox",
			thread, in.FromStepID, perStep, MaxMessagesPerStep)
	}
	createdAt := in.CreatedAt
	if createdAt == "" {
		createdAt = s.Now()
	}
	res, err := tx.Exec(`
		INSERT INTO agent_message (loop_slug, component_root, thread, from_agent, to_agent, from_step_id,
			body, expect_reply, result_of, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		in.LoopSlug, in.ComponentRoot, thread, from, to, in.FromStepID, body, b2i(in.ExpectReply), nullableID(in.ResultOf), createdAt)
	if err != nil {
		return AgentMessage{}, fmt.Errorf("store: file a message into thread %d from %s to %s: %w", thread, from, to, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return AgentMessage{}, fmt.Errorf("store: read the id of the message filed into thread %d: %w", thread, err)
	}
	if err := tx.Commit(); err != nil {
		return AgentMessage{}, fmt.Errorf("store: commit the message filed into thread %d from %s to %s: %w", thread, from, to, err)
	}
	return s.agentMessageByID(id, fmt.Sprintf("store: read back the message filed into thread %d", thread))
}

// DeliverAgentMessage stamps the ONE delivery of a message: the instant the
// callee's boundary drained it into a turn. Like the steer's delivery, it is
// write-once and reported, never re-stamped — a double delivery that hides is
// the failure this sentence exists to prevent.
func (s *Store) DeliverAgentMessage(id, toStepID int64) (AgentMessage, error) {
	tx, err := s.w.Begin()
	if err != nil {
		return AgentMessage{}, fmt.Errorf("store: deliver mailbox message %d: %w", id, err)
	}
	defer func() { _ = tx.Rollback() }()

	var current AgentMessage
	err = func() error {
		row := tx.QueryRow(`SELECT `+agentMessageColumns+` FROM agent_message WHERE id = ?`, id)
		m, err := scanAgentMessage(row)
		if err == sql.ErrNoRows {
			return fmt.Errorf("store: cannot deliver mailbox message %d: no such message: %w", id, ErrNotFound)
		}
		if err != nil {
			return fmt.Errorf("store: look up mailbox message %d to deliver it: %w", id, err)
		}
		current = m
		return nil
	}()
	if err != nil {
		return AgentMessage{}, err
	}
	if current.DeliveredAt != "" {
		return AgentMessage{}, fmt.Errorf("store: refusing to deliver mailbox message %d again: it was delivered at %s — a re-stamped timestamp is how a double delivery hides", id, current.DeliveredAt)
	}
	// The pair: the instant and the receiving step, ONE statement (the CHECK
	// refuses either half alone).
	if _, err := tx.Exec(`UPDATE agent_message SET delivered_at = ?, delivered_to_step_id = ?
		WHERE id = ? AND delivered_at IS NULL`, s.Now(), nullableID(toStepID), id); err != nil {
		return AgentMessage{}, fmt.Errorf("store: stamp the delivery of mailbox message %d: %w", id, err)
	}
	if err := tx.Commit(); err != nil {
		return AgentMessage{}, fmt.Errorf("store: commit the delivery of mailbox message %d: %w", id, err)
	}
	return s.agentMessageByID(id, fmt.Sprintf("store: read back the delivered mailbox message %d", id))
}

// PendingForAgent is the drain: the messages addressed to ONE agent of ONE
// loop that no boundary has drained yet, in filing order. The reader pool,
// never the writer (O6, I6) — this runs before a model call.
func (s *Store) PendingForAgent(componentRoot, loopSlug, toAgent string) ([]AgentMessage, error) {
	rows, err := s.r.Query(`SELECT `+agentMessageColumns+`
		FROM agent_message WHERE component_root = ? AND loop_slug = ? AND to_agent = ? AND delivered_at IS NULL
		ORDER BY id`, componentRoot, loopSlug, toAgent)
	if err != nil {
		return nil, fmt.Errorf("store: read the pending mailbox messages for %s in %s/%s: %w", toAgent, loopSlug, componentRoot, err)
	}
	defer func() { _ = rows.Close() }()
	var out []AgentMessage
	for rows.Next() {
		m, err := scanAgentMessage(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan a pending mailbox message for %s: %w", toAgent, err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: read the pending mailbox messages for %s: %w", toAgent, err)
	}
	return out, nil
}

// ThreadAgentMessages is one conversation, start to answer, ordered by filing.
// A thread that does not exist in this loop refuses naming the thread AND the
// loop — a missing conversation is answered, never silently empty (AT5).
func (s *Store) ThreadAgentMessages(componentRoot, loopSlug string, thread int64) ([]AgentMessage, error) {
	var head int
	if err := s.r.QueryRow(`SELECT count(*) FROM agent_message WHERE id = ? AND thread = ? AND loop_slug = ? AND component_root = ?`,
		thread, thread, loopSlug, componentRoot).Scan(&head); err != nil {
		return nil, fmt.Errorf("store: look up thread %d in %s/%s: %w", thread, loopSlug, componentRoot, err)
	}
	if head == 0 {
		return nil, fmt.Errorf("store: thread %d is not a conversation of loop %s/%s: %w", thread, loopSlug, componentRoot, ErrNotFound)
	}
	rows, err := s.r.Query(`SELECT `+agentMessageColumns+` FROM agent_message WHERE thread = ? ORDER BY id`, thread)
	if err != nil {
		return nil, fmt.Errorf("store: read thread %d: %w", thread, err)
	}
	defer func() { _ = rows.Close() }()
	var out []AgentMessage
	for rows.Next() {
		m, err := scanAgentMessage(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan thread %d: %w", thread, err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: read thread %d: %w", thread, err)
	}
	return out, nil
}

// LoopAgentMessages is the whole mailbox of one loop, filing order. `aeon
// loop messages` and `agent.messages` (MB7) read through here; the scope is
// the loop's row's own slug+root, never a caller's flag.
func (s *Store) LoopAgentMessages(componentRoot, loopSlug string) ([]AgentMessage, error) {
	rows, err := s.r.Query(`SELECT `+agentMessageColumns+`
		FROM agent_message WHERE component_root = ? AND loop_slug = ? ORDER BY id`, componentRoot, loopSlug)
	if err != nil {
		return nil, fmt.Errorf("store: read the mailbox of %s/%s: %w", loopSlug, componentRoot, err)
	}
	defer func() { _ = rows.Close() }()
	var out []AgentMessage
	for rows.Next() {
		m, err := scanAgentMessage(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan a mailbox row of %s/%s: %w", loopSlug, componentRoot, err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: read the mailbox of %s/%s: %w", loopSlug, componentRoot, err)
	}
	return out, nil
}

// RepliesOwedForStep is a completed step's honest to-do list: the messages
// its boundary RECEIVED (delivered to THIS step, the pair column, not a
// guess from timestamps) that asked for an answer and have not been answered
// — no row links to them through result_of yet. The daemon's completion hook
// reads rows, never memory (the state is the table), and files each reply
// through QueueAgentMessage with ResultOf set, which is the only door that
// can settle what this list holds.
func (s *Store) RepliesOwedForStep(stepID int64) ([]AgentMessage, error) {
	rows, err := s.r.Query(`SELECT `+agentMessageColumns+`
		FROM agent_message m
		WHERE m.delivered_to_step_id = ? AND m.expect_reply = 1
		  AND NOT EXISTS (SELECT 1 FROM agent_message a WHERE a.result_of = m.id)
		ORDER BY m.id`, stepID)
	if err != nil {
		return nil, fmt.Errorf("store: read the replies owed by step %d: %w", stepID, err)
	}
	defer func() { _ = rows.Close() }()
	var out []AgentMessage
	for rows.Next() {
		m, err := scanAgentMessage(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan a reply owed by step %d: %w", stepID, err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: read the replies owed by step %d: %w", stepID, err)
	}
	return out, nil
}

// PendingAsksFromStep is the dispatcher's scan: the wait:true asks this step
// filed that NO boundary has drained yet — the peers whose turns the daemon
// may need to dispatch (MB4: a running addressee drains at its boundary; a
// not-running one is dispatched off exactly this list).
func (s *Store) PendingAsksFromStep(stepID int64) ([]AgentMessage, error) {
	rows, err := s.r.Query(`SELECT `+agentMessageColumns+`
		FROM agent_message WHERE from_step_id = ? AND expect_reply = 1 AND delivered_at IS NULL
		ORDER BY id`, stepID)
	if err != nil {
		return nil, fmt.Errorf("store: read the pending asks filed by step %d: %w", stepID, err)
	}
	defer func() { _ = rows.Close() }()
	var out []AgentMessage
	for rows.Next() {
		m, err := scanAgentMessage(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan a pending ask of step %d: %w", stepID, err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: read the pending asks filed by step %d: %w", stepID, err)
	}
	return out, nil
}

// RunningStepForAgent answers MB4's dispatch question honestly from rows:
// is this agent RUNNING a step in this loop right now? P33 already bounds a
// loop to one running step, so the count is at most one by the loop's own
// shape; this only asks which agent that step speaks for.
func (s *Store) RunningStepForAgent(componentRoot, loopSlug, agent string) (bool, error) {
	var n int
	if err := s.r.QueryRow(`SELECT count(*) FROM step
		WHERE component_root = ? AND loop_slug = ? AND agent = ? AND status = 'running'`,
		componentRoot, loopSlug, agent).Scan(&n); err != nil {
		return false, fmt.Errorf("store: look for a running step of %s in %s/%s: %w", agent, loopSlug, componentRoot, err)
	}
	return n > 0, nil
}

func (s *Store) agentMessageByID(id int64, what string) (AgentMessage, error) {
	row := s.r.QueryRow(`SELECT `+agentMessageColumns+` FROM agent_message WHERE id = ?`, id)
	m, err := scanAgentMessage(row)
	if err == sql.ErrNoRows {
		return AgentMessage{}, fmt.Errorf("%s: no message %d: %w", what, id, ErrNotFound)
	}
	if err != nil {
		return AgentMessage{}, fmt.Errorf("%s: %w", what, err)
	}
	return m, nil
}

// b2i and nullableID are the codec's two shape helpers; the package's
// nullable family speaks to strings/pointers, and these two answer the
// mailbox's int-boolean column and its optional id without opening a
// caller-editable pointer.
func b2i(v bool) int {
	if v {
		return 1
	}
	return 0
}

func nullableID(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

// CheckpointWithMailboxResult answers CheckpointWithMailbox, mirroring
// CheckpointSteersResult: OK means the transaction committed AND every row
// named was among the rows delivered.
type CheckpointWithMailboxResult struct {
	OK           bool
	Delivered    []int64
	NotDelivered []AgentMessage
	Reason       string
}

// CheckpointWithMailbox is S70 MB4's D4 join: the transcript carrying a
// peer's folded words and the delivered_at stamps of exactly those rows are
// ONE transaction — CheckpointWithSteers' law, quoted, for a different
// queue. The alternative (flip the rows after the commit) is the window
// where `kill -9` leaves a pending row whose text is already in the
// transcript, and the replay injects the same message twice.
//
// The transcript/envelope/guard half is deliberately in CheckpointWithSteers'
// exact shape; the extraction that lets both queues share one copy is a
// recorded follow-up (T2 Notes), not a silent second truth mid-slice — the
// two are covered by the same AT names and drift fails a test.
//
// A named row that is missing, or belongs to another loop, another root, or
// another addressee than this step, refuses the WHOLE checkpoint and rolls
// back naming what was wrong (the steer's "delivered only by the step that
// queued it", MB2's boundary at the delivery end). A row already delivered
// does not refuse: its text is in the transcript the caller just wrote, so
// the honest answer is committed-with-a-spoken-hole (res.OK false, the row
// in NotDelivered), never a silent success and never a lost transcript.
func (s *Store) CheckpointWithMailbox(ctx context.Context, stepID int64, msgs []model.Message,
	snapshot json.RawMessage, deliveredIDs []int64, iteration int) (CheckpointWithMailboxResult, error) {
	if verdict := model.ValidateTranscript(msgs); !verdict.OK {
		return CheckpointWithMailboxResult{}, fmt.Errorf("store: checkpoint with mailbox of step %d: %s",
			stepID, verdict.Reason)
	}
	if iteration < 0 {
		return CheckpointWithMailboxResult{}, fmt.Errorf("store: checkpoint with mailbox of step %d: iteration %d "+
			"is not an iteration the step reached", stepID, iteration)
	}
	if !json.Valid(snapshot) {
		return CheckpointWithMailboxResult{}, fmt.Errorf("store: checkpoint with mailbox of step %d: the saved "+
			"request envelope is not valid JSON, and recovery replays from it", stepID)
	}

	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return CheckpointWithMailboxResult{}, fmt.Errorf("store: begin the checkpoint with mailbox of step %d: %w",
			stepID, err)
	}
	defer func() { _ = tx.Rollback() }()

	// The step must still be running, and its scope is read here — never
	// trusted from the caller (MB2: scope comes from the rows, every time).
	var loopSlug, componentRoot, agent string
	err = tx.QueryRowContext(ctx,
		`SELECT loop_slug, component_root, agent FROM step WHERE id = ?`, stepID).
		Scan(&loopSlug, &componentRoot, &agent)
	if err == sql.ErrNoRows {
		return CheckpointWithMailboxResult{}, fmt.Errorf("store: cannot checkpoint step %d with mailbox: no such step: %w",
			stepID, ErrNotFound)
	}
	if err != nil {
		return CheckpointWithMailboxResult{}, fmt.Errorf("store: look up step %d to checkpoint it with mailbox: %w",
			stepID, err)
	}
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM step WHERE id = ?`, stepID).Scan(&status); err != nil {
		return CheckpointWithMailboxResult{}, fmt.Errorf("store: read the status of step %d to checkpoint it with "+
			"mailbox: %w", stepID, err)
	}
	if status != string(StepRunning) {
		return CheckpointWithMailboxResult{}, fmt.Errorf("store: checkpoint step %d with mailbox refused: it is %s, "+
			"and a checkpoint replaces the transcript of a running step", stepID, status)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM step_message WHERE step_id=?`, stepID); err != nil {
		return CheckpointWithMailboxResult{}, fmt.Errorf("store: replace the transcript of step %d: %w", stepID, err)
	}
	now := s.Now()
	for i, msg := range msgs {
		content, err := marshalBlocks(msg)
		if err != nil {
			return CheckpointWithMailboxResult{}, fmt.Errorf("store: encode message %d of step %d: %w", i, stepID, err)
		}
		prov, err := marshalProvenance(msg)
		if err != nil {
			return CheckpointWithMailboxResult{}, fmt.Errorf("store: encode message %d of step %d: %w", i, stepID, err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO step_message (step_id, seq, format_version, role, content, provenance, created_at)
			VALUES (?,?,?,?,?,?,?)`,
			stepID, i+1, model.FormatVersion, string(msg.Role), content, prov, now); err != nil {
			return CheckpointWithMailboxResult{}, fmt.Errorf("store: write message %d of step %d: %w", i, stepID, err)
		}
	}

	upd, err := tx.ExecContext(ctx,
		`UPDATE step SET context_manifest=? WHERE id=? AND status='running'`, string(snapshot), stepID)
	if err != nil {
		return CheckpointWithMailboxResult{}, fmt.Errorf("store: save the request envelope of step %d with its "+
			"mailbox deliveries: %w", stepID, err)
	}
	if n, err := upd.RowsAffected(); err != nil {
		return CheckpointWithMailboxResult{}, fmt.Errorf("store: count the envelope write of step %d: %w", stepID, err)
	} else if n != 1 {
		return CheckpointWithMailboxResult{}, fmt.Errorf("store: checkpoint step %d with mailbox refused: the step "+
			"stopped being a running step while this checkpoint was being written", stepID)
	}

	res := CheckpointWithMailboxResult{OK: true}
	seen := make(map[int64]bool, len(deliveredIDs))
	for _, id := range deliveredIDs {
		if seen[id] {
			continue
		}
		seen[id] = true

		m, err := scanAgentMessage(tx.QueryRowContext(ctx, `SELECT `+agentMessageColumns+`
			FROM agent_message WHERE id = ?`, id))
		if err == sql.ErrNoRows {
			return CheckpointWithMailboxResult{}, fmt.Errorf("store: checkpoint of step %d names agent message %d, "+
				"which does not exist: a message is delivered only by the checkpoint that folded its words", stepID, id)
		}
		if err != nil {
			return CheckpointWithMailboxResult{}, fmt.Errorf("store: look up agent message %d to deliver it in the "+
				"checkpoint of step %d: %w", id, stepID, err)
		}
		if m.LoopSlug != loopSlug || m.ComponentRoot != componentRoot {
			return CheckpointWithMailboxResult{}, fmt.Errorf("store: checkpoint of step %d (loop %s) names agent "+
				"message %d, which belongs to loop %s: a message is delivered inside its own loop (MB2)",
				stepID, loopSlug, id, m.LoopSlug)
		}
		if m.ToAgent != agent {
			return CheckpointWithMailboxResult{}, fmt.Errorf("store: checkpoint of step %d (agent %s) names agent "+
				"message %d, addressed to %s: a message is delivered to its addressee and nobody else (MB2)",
				stepID, agent, id, m.ToAgent)
		}
		if m.DeliveredAt != "" {
			res.OK = false
			res.NotDelivered = append(res.NotDelivered, m)
			continue
		}

		u, err := tx.ExecContext(ctx,
			`UPDATE agent_message SET delivered_at = ?, delivered_to_step_id = ? WHERE id = ? AND delivered_at IS NULL`,
			now, stepID, id)
		if err != nil {
			return CheckpointWithMailboxResult{}, fmt.Errorf("store: deliver agent message %d in the checkpoint of "+
				"step %d: %w", id, stepID, err)
		}
		n, err := u.RowsAffected()
		if err != nil {
			return CheckpointWithMailboxResult{}, fmt.Errorf("store: deliver agent message %d in the checkpoint of "+
				"step %d: %w", id, stepID, err)
		}
		if n != 1 {
			found, err := scanAgentMessage(tx.QueryRowContext(ctx, `SELECT `+agentMessageColumns+`
				FROM agent_message WHERE id = ?`, id))
			if err != nil {
				return CheckpointWithMailboxResult{}, fmt.Errorf("store: re-read agent message %d after the "+
					"delivery wrote nothing: %w", id, err)
			}
			res.OK = false
			res.NotDelivered = append(res.NotDelivered, found)
			continue
		}
		res.Delivered = append(res.Delivered, id)
	}

	if len(res.NotDelivered) > 0 {
		parts := make([]string, 0, len(res.NotDelivered))
		for _, m := range res.NotDelivered {
			when := m.DeliveredAt
			parts = append(parts, fmt.Sprintf("agent message %d was already delivered at %s", m.ID, when))
		}
		res.Reason = fmt.Sprintf("store: the checkpoint of step %d committed without delivering %d of the %d "+
			"messages it named: %s", stepID, len(res.NotDelivered), len(seen), strings.Join(parts, "; "))
	}

	if err := tx.Commit(); err != nil {
		return CheckpointWithMailboxResult{}, fmt.Errorf("store: commit the checkpoint with mailbox of step %d: %w",
			stepID, err)
	}
	return res, nil
}
