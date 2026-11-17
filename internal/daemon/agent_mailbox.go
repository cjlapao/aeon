package daemon

// agent_mailbox.go is S70's daemon-side wiring: the two runtime seams
// MailboxDrain and MailboxAskDoor get their production implementations
// here, because this is the package that holds the store and the run's
// pinned blueprint (MB2's lanes) at once. The runtime stays store-agnostic
// (I2), the tools package stays effect-free (K18), and the mailbox rows
// move only through doors. The policy question the ask raises is NOT
// decided here — it is decided by the loop through the same gate that
// judges every sibling tool call (D12's parity structural, not a second
// judgement this file could drift from), so the door holds no judge.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/handler"
	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/runtime"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/tools"
)

// CheckpointWithMailbox is the mailbox's D4 join, mirroring
// CheckpointWithSteers' policy exactly where the law is the same and
// saying so where it differs: a committed checkpoint that named an
// already-delivered row is NOT a race the person authored (steers have
// cancellations; the mailbox does not) — it is a bug in a drain, and the
// honest place the bug stays visible is the rows themselves (delivered_at
// is readable through agent.messages) and the step's failure. So unlike
// steers, a short here is RETURNED: the transcript committed and the step
// will die loudly rather than let a double delivery pass quietly.
func (s *StepStore) CheckpointWithMailbox(ctx context.Context, stepID int64, msgs []model.Message,
	manifest json.RawMessage, deliveredIDs []int64, iteration int) error {
	res, err := s.store.CheckpointWithMailbox(ctx, stepID, msgs, manifest, deliveredIDs, iteration)
	if err != nil {
		return err
	}
	if !res.OK {
		return fmt.Errorf("daemon: the checkpoint of step %d committed while naming already-delivered "+
			"mailbox rows: %s — a message delivered twice is a drain bug; the rows carry the truth",
			stepID, res.Reason)
	}
	return nil
}

// mailboxDrainFor is the daemon's factory for the runtime's boundary read
// (S70 MB4), the steerSourceFor shape: a stepless daemon hands out nil and
// the loop runs byte-identical to the pre-mailbox path (I2).
func mailboxDrainFor(steps *store.Store) func(int64) runtime.MailboxDrain {
	if steps == nil {
		return nil
	}
	return func(stepID int64) runtime.MailboxDrain {
		return stepMailbox{store: steps, stepID: stepID}
	}
}

// stepMailbox is one step's MailboxDrain: the boundary read of rows
// addressed to THIS step's agent, in this loop, in this component. The
// scope is read from the step's own row at the boundary — never captured
// at wiring time (a step's row is the truth about its scope; doctrine:
// scope from the row, never a parameter) — and the steer source's honest
// reading is inherited verbatim: a failed read delivers NOTHING this
// boundary rather than pretending, and the rows stay pending for the next
// one. A step no loop owns files and receives nothing: an ask is
// loop-scoped (MB2), and a step outside a loop has no lane to be addressed
// on.
type stepMailbox struct {
	store  *store.Store
	stepID int64
}

func (m stepMailbox) Pending(ctx context.Context) []runtime.MailboxMessage {
	st, err := m.store.ReadStep(m.stepID)
	if err != nil || st.LoopSlug == "" || st.ComponentRoot == "" || st.Agent == "" {
		return nil
	}
	rows, err := m.store.PendingForAgent(st.ComponentRoot, st.LoopSlug, st.Agent)
	if err != nil {
		return nil
	}
	out := make([]runtime.MailboxMessage, 0, len(rows))
	for _, r := range rows {
		out = append(out, runtime.MailboxMessage{ID: r.ID, FromAgent: r.FromAgent, Body: r.Body})
	}
	return out
}

// mailboxAskFor is the daemon's factory for the ask door. Like the drain
// it hands out an adapter over the step's row, read at the moment of the
// ask; scope and lanes come from rows and the run's pinned YAML, never
// from a flag.
func mailboxAskFor(steps *store.Store, announce func(kind, componentRoot string, payload any)) func(int64) runtime.MailboxAskDoor {
	if steps == nil {
		return nil
	}
	return func(stepID int64) runtime.MailboxAskDoor {
		return &mailboxAskDoor{store: steps, authorStepID: stepID, announce: announce}
	}
}

// mailboxAskDoor is the ask_agent door (S70 MB1/MB4): the ONE parser runs
// before anything files (AT1), the addressee is checked against the run's
// PINNED lanes (MB2), and only then does the store see it. The policy
// question was already answered by the loop's gate before this door was
// reached — structurally, through the same object that judged every
// sibling tool call at the same snapshotted instant (D12); a door that
// judged again could only drift.
type mailboxAskDoor struct {
	store *store.Store
	// announce is the daemon's event publish, mounted where d exists. The
	// ASK is part of the visible exchange (MB6): its row is announced the
	// moment the door files it. nil (tests, storeless) announces nothing.
	announce func(kind, componentRoot string, payload any)
	// authorStepID is the step filing the ask; its row is read at Ask time
	// for scope (loop slug, component root) and sender (the step's agent —
	// never the caller's word).
	authorStepID int64
}

func (d *mailboxAskDoor) Ask(ctx context.Context, input json.RawMessage) (runtime.AskOutcome, error) {
	params, err := tools.ParseAskAgent(input)
	if err != nil {
		// AT1: nothing persisted — the parser is the door's first statement.
		return runtime.AskOutcome{}, err
	}
	st, err := d.store.ReadStep(d.authorStepID)
	if err != nil {
		return runtime.AskOutcome{}, fmt.Errorf("ask_agent refused before filing: the authoring step %d could "+
			"not be read: %w", d.authorStepID, err)
	}
	if st.LoopSlug == "" || st.ComponentRoot == "" || st.Agent == "" {
		// MB2's first sentence: an ask is loop-scoped. A step no loop owns
		// has no lanes, and the refusal names the missing thing.
		return runtime.AskOutcome{}, fmt.Errorf("ask_agent refused: step %d belongs to no loop — an ask is "+
			"loop-scoped (MB2); there is no lane to address", d.authorStepID)
	}
	lanes, err := d.lanes(st)
	if err != nil {
		return runtime.AskOutcome{}, fmt.Errorf("ask_agent refused before filing: the loop's pinned lanes could "+
			"not be read: %w", err)
	}
	found := false
	for _, name := range lanes {
		if name == params.To {
			found = true
			break
		}
	}
	if !found {
		return runtime.AskOutcome{}, fmt.Errorf("ask_agent refused: %q is not an agent of this loop — the lanes "+
			"you may ask are: %v (MB2: an ask is loop-scoped; a stranger is refused by name, never reached)",
			params.To, lanes)
	}
	wait := true
	if params.Wait != nil {
		wait = *params.Wait
	}
	thread := int64(0)
	if params.Thread != nil {
		thread = *params.Thread
	}
	if wait {
		// The replay arm (AT8): a crash between filing and the suspension
		// commit asks the SAME question again on replay — same addressee,
		// same words, same thread is not a new message. The undrained ask
		// already filed IS the honest answer to "did I ask this?"; the
		// replay reuses its row, so the question keeps naming the id the
		// dispatcher and the reply ride on, and the callee answers once.
		pending, err := d.store.PendingAsksFromStep(d.authorStepID)
		if err != nil {
			return runtime.AskOutcome{}, err
		}
		for _, prior := range pending {
			sameThread := prior.Thread == thread
			if thread == 0 {
				// A fresh ask joins NO thread — it bootstraps to its own
				// header, so the identity of "the same fresh ask" is a prior
				// undrained ask that IS its thread's header.
				sameThread = prior.Thread == prior.ID
			}
			if prior.ToAgent == params.To && prior.Body == params.Text && sameThread {
				return runtime.AskOutcome{
					MessageID: prior.ID, Thread: prior.Thread, Suspends: true,
					Question: &runtime.PendingQuestion{
						Kind: "free",
						Body: fmt.Sprintf("waiting for %s to answer message %d (thread %d)",
							params.To, prior.ID, prior.Thread),
					},
				}, nil
			}
		}
	}
	row, err := d.store.QueueAgentMessage(store.NewAgentMessage{
		LoopSlug: st.LoopSlug, ComponentRoot: st.ComponentRoot,
		Thread: thread, FromAgent: st.Agent, ToAgent: params.To,
		FromStepID: d.authorStepID, Body: params.Text, ExpectReply: wait,
	})
	if err != nil {
		return runtime.AskOutcome{}, err
	}
	// MB6: the ask is part of the VISIBLE exchange too — its row is
	// posted the moment the door files it (the wait:true arm suspends and
	// the runtime's own asked event rides the step sink beside this).
	if d.announce != nil {
		d.announce(protocol.KindAgentMessagePosted, row.ComponentRoot, map[string]any{
			"messageId": row.ID, "thread": row.Thread, "loopId": mailboxLoopID(d.store, row.ComponentRoot, row.LoopSlug),
			"from": row.FromAgent, "to": row.ToAgent, "fromStepId": row.FromStepID,
			"body": row.Body, "expectReply": row.ExpectReply,
		})
	}
	if !wait {
		return runtime.AskOutcome{
			MessageID: row.ID, Thread: row.Thread,
			Result: fmt.Sprintf("asked %s (message %d in thread %d); it lands at their next turn boundary "+
				"— no answer is awaited", params.To, row.ID, row.Thread),
		}, nil
	}
	// wait:true: the step suspends. Whether the addressee is RUNNING does
	// not change this arm's shape (MB4): running peers drain at their
	// boundary; a not-running peer's turn is dispatched by the daemon's
	// dispatcher off this same row — either way the caller suspends AS A
	// ROW, and the answer resumes the blocked tool_use.
	return runtime.AskOutcome{
		MessageID: row.ID, Thread: row.Thread, Suspends: true,
		Question: &runtime.PendingQuestion{
			Kind: "free",
			Body: fmt.Sprintf("waiting for %s to answer message %d (thread %d)", params.To, row.ID, row.Thread),
		},
	}, nil
}

// lanes reads the loop's CURRENT run's pinned YAML — the run row's own
// Content, not a fresh file read: a loop is addressed by the lanes that
// were pinned when its run started, so an author editing a blueprint file
// mid-run cannot open an ask lane that did not exist when the loop began
// (MB2: the set comes from the pinned manifest, not today's card
// catalogue). Agents are read off the steps (Lane carries steps, Step
// carries the agent); resolvers are excluded — a resolver is a routing
// expression, not a colleague, and an ask must name a real peer.
func (d *mailboxAskDoor) lanes(st store.Step) ([]string, error) {
	l, err := d.store.GetLoop(store.Scope{LoopSlug: st.LoopSlug, ComponentRoot: st.ComponentRoot})
	if err != nil {
		return nil, fmt.Errorf("loop %s in %s: %w", st.LoopSlug, st.ComponentRoot, err)
	}
	run, err := d.store.CurrentRun(l.ID)
	if err != nil {
		return nil, fmt.Errorf("loop %s has no run to pin lanes by: %w", st.LoopSlug, err)
	}
	if run.Content == "" {
		return nil, fmt.Errorf("run %d of loop %s pins no blueprint content to read lanes from", run.RunSeq, st.LoopSlug)
	}
	bp, err := blueprint.Load([]byte(run.Content))
	if err != nil {
		return nil, fmt.Errorf("the pinned blueprint of run %d of loop %s does not parse: %w", run.RunSeq, st.LoopSlug, err)
	}
	var names []string
	seen := map[string]bool{}
	add := func(step blueprint.Step) {
		if step.Agent == "" || seen[step.Agent] || handler.IsResolver(step.Agent) {
			return
		}
		seen[step.Agent] = true
		names = append(names, step.Agent)
	}
	for _, lane := range bp.Lanes {
		if lane.Step != nil {
			add(*lane.Step)
		}
		for _, step := range lane.Ladder {
			add(step)
		}
	}
	return names, nil
}

// ── T2d: the round trip ──────────────────────────────────────────────────
//
// A completed step asks the ROWS which replies it owes (the delivery pair
// names the receiving step); each owed reply files through QueueAgentMessage
// — the only door — and wakes its caller through the ONE answer door
// (Table.AnswerQuestion, the socket's own path: same guards, same answer
// row, same queued continuation; one truth, two doors). An ask whose
// addressee runs nowhere is dispatched as the addressee's own turn —
// the S68 half-split with the MB5 frame in the system half and ONLY the
// message's words in the transcript half (no history, no chosen skills:
// the builder has no channel for them, AT11's shape argument). All of it
// is driven by rows: the scan at recovery, the scan after every dispatch,
// the wake after every reply. No goroutine waits (AS5) — a turn goroutine
// runs a step and exits, the way the converse turn already does; a row
// that nobody acted on survives every restart and the next scan.

// errorText renders an error for the log the way the socket would render it
// for a caller: the base sentence plus the WHAT part of a protocol error's
// three-part payload. A log line that drops the specifics is a refusal that
// names only "failed" — not a refusal worth reading (AGENTS §5).
func errorText(err error) string {
	if pe, ok := err.(*protocol.Error); ok && pe.Data.What != "" {
		return fmt.Sprintf("%s — what: %s", pe.Error(), pe.Data.What)
	}
	return err.Error()
}

// mailboxReplyTurns is the completion hook: file what this step owes and
// wake who it answers. finalText is the turn's own last word (the handoff
// summary); when a completion path cannot carry one, the empty string says
// so honestly and the reply reports that rather than inventing words.
func (d *Daemon) mailboxReplyTurns(stepID int64, finalText string) {
	if d.store == nil {
		return
	}
	owed, err := d.store.RepliesOwedForStep(stepID)
	if err != nil {
		if d.log != nil {
			d.log.Infof("mailbox: the replies owed by step %d could not be read: %v", stepID, err)
		}
		return
	}
	body := strings.TrimSpace(finalText)
	for _, row := range owed {
		text := body
		if text == "" {
			text = "the turn ended without a final message"
		}
		reply, err := d.store.QueueAgentMessage(store.NewAgentMessage{
			LoopSlug: row.LoopSlug, ComponentRoot: row.ComponentRoot,
			FromAgent: row.ToAgent, ToAgent: row.FromAgent, FromStepID: stepID,
			Body: text, ResultOf: row.ID,
		})
		if err != nil {
			if d.log != nil {
				d.log.Infof("mailbox: the reply to message %d from step %d could not be filed: %v", row.ID, stepID, err)
			}
			continue
		}
		loopID := mailboxLoopID(d.store, row.ComponentRoot, row.LoopSlug)
		d.publish(protocol.KindAgentMessagePosted, row.ComponentRoot, map[string]any{
			"messageId": reply.ID, "thread": reply.Thread, "loopId": loopID,
			"from": reply.FromAgent, "to": reply.ToAgent, "fromStepId": stepID,
			"body": reply.Body, "expectReply": reply.ExpectReply,
		})
		d.publish(protocol.KindAgentMessageAnswered, row.ComponentRoot, map[string]any{
			"messageId": row.ID, "thread": row.Thread, "loopId": loopID, "answeredBy": reply.ID,
		})
		// Wake the caller whose wait:true ask this answer answers — through
		// the SAME door a person's answer travels. A refusal (no pending
		// question — the caller may have been answered, failed, or recovered
		// elsewhere) is spoken in the log and the stream, and the rows keep
		// the whole truth either way.
		if row.FromStepID > 0 {
			if _, err := d.Table.AnswerQuestion(protocol.AnswerParams{StepID: row.FromStepID, Answer: text}); err != nil {
				if d.log != nil {
					d.log.Infof("mailbox: waking the caller of message %d (step %d): %s", row.ID, row.FromStepID, errorText(err))
				}
			}
		}
	}
}

// mailboxDispatchTurns answers MB4's dispatch question from rows for every
// wait:true ask this step filed that no boundary has drained: a running
// addressee drains at its own boundary (nothing to do); a not-running one
// gets its turn dispatched. The loop's pinned rows supply the state the
// turn runs on, exactly as the converse turn reads them.
func (d *Daemon) mailboxDispatchTurns(componentRoot, loopSlug string, authorStepID int64) {
	if d.store == nil {
		return
	}
	asks, err := d.store.PendingAsksFromStep(authorStepID)
	if err != nil || len(asks) == 0 {
		return
	}
	l, err := d.store.GetLoop(store.Scope{LoopSlug: loopSlug, ComponentRoot: componentRoot})
	if err != nil {
		if d.log != nil {
			d.log.Infof("mailbox: loop %s/%s could not be read to dispatch asks from step %d: %v",
				loopSlug, componentRoot, authorStepID, err)
		}
		return
	}
	for _, ask := range asks {
		running, err := d.store.RunningStepForAgent(componentRoot, loopSlug, ask.ToAgent)
		if err != nil || running {
			continue // a running peer drains at its boundary; nothing to dispatch
		}
		pin, err := d.store.ReadPinnedLoop(l.ID)
		if err != nil {
			if d.log != nil {
				d.log.Infof("mailbox: loop %s is not pinned — the ask of message %d waits for a pinnable loop: %v",
					l.ID, ask.ID, err)
			}
			continue
		}
		st, err := loop.FromPinned(pin)
		if err != nil {
			if d.log != nil {
				d.log.Infof("mailbox: loop %s state could not be read to dispatch message %d: %v", l.ID, ask.ID, err)
			}
			continue
		}
		go d.runMailboxTurn(st, ask)
	}
}

// runMailboxTurn dispatches one peer's turn off the rows (MB4/MB5). The
// claim is the converse turn's own rule: one loop-spoken turn at a time;
// a busy loop simply keeps the row pending for the next scan (nothing
// waits in memory — AS5 again). When the turn ends, its own completions
// run the same hooks the ladder's do: owed replies filed (its handoff
// summary is its answer), later asks it filed itself dispatched.
func (d *Daemon) runMailboxTurn(st loop.LoopState, ask store.AgentMessage) {
	if !d.claimTurn(st.ID) {
		return // busy: the row is the queue; the next scan retries
	}
	frame := mailboxTurnFrame(st, ask)
	res, err := d.Table.RunStepForLoop(context.Background(),
		turnParams(st, ask.ToAgent, ask.Body, frame, nil, st.Route, nil, nil, ask.ComponentRoot),
		protocol.LoopStepIdentity{LoopID: st.ID, Step: "mailbox"},
		func(stepID int64) error {
			d.publishLoop(protocol.KindLoopStepDispatched, map[string]any{
				"loopId": st.ID, "lane": "", "step": "mailbox",
				"stepId": stepID, "agent": ask.ToAgent, "route": st.Route,
			})
			return nil
		})
	d.releaseTurn(st.ID)
	if err != nil {
		if d.log != nil {
			d.log.Infof("mailbox: the turn for message %d (%s) could not run: %s", ask.ID, ask.ToAgent, errorText(err))
		}
		return
	}
	if res.Question != nil {
		// The answering turn asked someone else — its asks dispatch the
		// same way, from the same rows.
		d.mailboxDispatchTurns(ask.ComponentRoot, ask.LoopSlug, res.StepID)
	}
	d.mailboxReplyTurns(res.StepID, handoffSummary(res.Handoff))
}

// mailboxTurnFrame is MB5's system half: the frame names the caller, its
// lane when the loop's rows still know it, and the loop's goal; the model
// is told its final summary IS the answer and where it lands. There is no
// channel here for chosen skills (AT11's shape test): no operator spoke
// this turn, so the builder cannot carry an operator's picks. Policy still
// speaks last — structural, from the runtime's assembly order (S68).
func mailboxTurnFrame(st loop.LoopState, ask store.AgentMessage) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are %s, a peer agent of loop %s.", ask.ToAgent, st.Slug)
	if goal := strings.TrimSpace(st.Goal); goal != "" {
		fmt.Fprintf(&b, " The loop works toward: %s.", goal)
	}
	lane := ""
	if st.LastStepID == ask.FromStepID && st.Position.Lane != "" {
		lane = st.Position.Lane // the rows still know who stood where
	}
	if lane != "" {
		fmt.Fprintf(&b, " %s, from lane %s, sent you a message (message %d of thread %d).",
			ask.FromAgent, lane, ask.ID, ask.Thread)
	} else {
		fmt.Fprintf(&b, " %s sent you a message (message %d of thread %d).",
			ask.FromAgent, ask.ID, ask.Thread)
	}
	b.WriteString(" Answer only what was asked. Your final handoff summary becomes the answer,")
	fmt.Fprintf(&b, " posted back to %s in thread %d.", ask.FromAgent, ask.Thread)
	return b.String()
}

// handoffSummary reads the model's own final words out of a completion —
// the same structured promise converse mirrors; empty when there is none,
// which the reply says honestly rather than inventing.
func handoffSummary(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var h struct {
		Summary string `json:"summary"`
	}
	if json.Unmarshal(raw, &h) != nil {
		return ""
	}
	return strings.TrimSpace(h.Summary)
}

// mailboxScanWaiting is the restart's honest catch-up: a loop left waiting
// by a step whose wait:true asks found nobody running keeps those asks as
// rows, and rows outlive the crash. Recovery calls this once after the
// step replays, so a peer whose turn died with the previous process is
// dispatched again from what the table says — never from memory.
func (d *Daemon) mailboxScanWaiting() {
	if d.store == nil {
		return
	}
	pins, err := d.store.PinnedLoopsByStatus("waiting")
	if err != nil {
		if d.log != nil {
			d.log.Infof("mailbox: waiting loops could not be listed for the ask scan: %v", err)
		}
		return
	}
	for _, pin := range pins {
		var saved loop.RuntimeState
		if json.Unmarshal([]byte(pin.RuntimeJSON), &saved) != nil || saved.LastStepID == 0 {
			continue
		}
		st, err := d.store.ReadStep(saved.LastStepID)
		if err != nil || st.Status != string(store.StepSuspended) {
			continue // only a suspended waiter dispatches on nobody's behalf
		}
		d.mailboxDispatchTurns(pin.Loop.ComponentRoot, pin.Loop.Slug, saved.LastStepID)
	}
}

// mailboxLoopID resolves the loop's PUBLIC id (the same string every other
// event publishes under `loopId`, via the store's GetLoop) from the row's
// scope. The slug is not the id: the UI scopes events by loop id, and a
// slug under `loopId` would be a plausible wrong answer — invisible
// scoping, the worst kind of lie. An unresolvable loop publishes the empty
// string: no id is honest, a guessed one is not.
func mailboxLoopID(st *store.Store, componentRoot, loopSlug string) string {
	if st == nil {
		return ""
	}
	l, err := st.GetLoop(store.Scope{LoopSlug: loopSlug, ComponentRoot: componentRoot})
	if err != nil {
		return ""
	}
	return l.ID
}

// mailboxReadFor mounts MB7's socket read. Scope comes from the LOOP ROW —
// the id resolves through the pinned loop to its own slug and component
// root, never from anything a caller may set (S69's rule again). A named
// thread the loop does not have is ANSWERED as a said absence, never a
// silence; an unreadable loop refuses naming itself.
func mailboxReadFor(st *store.Store) func(loopID string, thread *int64) ([]protocol.AgentMessageView, bool, error) {
	if st == nil {
		return nil // I2: the handler answers its own unavailability
	}
	return func(loopID string, thread *int64) ([]protocol.AgentMessageView, bool, error) {
		pin, err := st.ReadPinnedLoop(loopID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return nil, false, protocol.ErrBadParams{Err: fmt.Errorf("no loop %q — agent.messages reads the mailbox of a loop that exists (MB7)", loopID)}
			}
			return nil, false, fmt.Errorf("daemon: the loop %q behind agent.messages could not be read: %w", loopID, err)
		}
		var rows []store.AgentMessage
		absent := false
		if thread != nil {
			got, err := st.ThreadAgentMessages(pin.Loop.ComponentRoot, pin.Loop.Slug, *thread)
			if err != nil {
				if errors.Is(err, store.ErrNotFound) {
					absent = true // P31: the absence is SAID, never a silence read as empty
				} else {
					return nil, false, err
				}
			} else {
				rows = got
			}
		} else {
			got, err := st.LoopAgentMessages(pin.Loop.ComponentRoot, pin.Loop.Slug)
			if err != nil {
				return nil, false, err
			}
			rows = got
		}
		views := make([]protocol.AgentMessageView, 0, len(rows))
		for _, m := range rows {
			views = append(views, protocol.AgentMessageView{
				ID: m.ID, Thread: m.Thread, FromAgent: m.FromAgent, ToAgent: m.ToAgent,
				FromStepID: m.FromStepID, Body: m.Body, ExpectReply: m.ExpectReply,
				ResultOf: m.ResultOf, DeliveredAt: m.DeliveredAt, DeliveredToStep: m.DeliveredToStepID,
				CreatedAt: m.CreatedAt,
			})
		}
		return views, absent, nil
	}
}
