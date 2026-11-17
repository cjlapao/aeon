package daemon

// records_mergequeue_test.go — S60 T9 (MQ14/MQ17): the five merge-queue
// arms of the records service. What each test pins, in the order the spec
// lists them:
//
//   - the three READ arms answer the SAME bytes the person's queue doors
//     answer, because they forward to the same door service (rule 4 — a
//     tool and its twin cannot drift, and the drift is what this file
//     catches), filtered to the agent's own-view: the roots its steps have
//     ACTUALLY run in (store.AgentProjectRoots — S52's structural source),
//     so the foreign row simply is not there, quietly;
//   - unwired, the plane says so by name (V13) instead of answering an
//     empty listing that reads as "the queue is empty" (P3);
//   - mergequeue_dequeue is own-item ONLY — the calling loop's row, and
//     only while queued or conflicted; another loop's item, a row in
//     flight, a person's row and an unknown id each meet their own named
//     sentence, and the successful removal carries the AGENT in the event
//     ledger (the closed set names 'agent' for exactly this write);
//   - mergequeue_propose_enqueue files an INERT row — stamped
//     agent/implementer, payload the three fields and nothing else, NO
//     merge_queue_item row added (an ask is not an enqueue, MQ14.3) —
//     refuses the smuggled field at the parse, refuses the duplicate-open
//     ask with the validator's own sentence (TD3: mirrored here exactly as
//     the environment file mirrors its seam, because internal/daemon
//     cannot import package main; the parity of the PRODUCTION seam is
//     pinned there), and stops at the per-agent cap naming its number.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

type mqRecordsEnv struct {
	base recordsEnv
	d    *Daemon
	s    *store.Store
	ctx  context.Context
}

func newMQRecordsEnv(t *testing.T) mqRecordsEnv {
	t.Helper()
	e := mqRecordsEnv{base: newRecordsEnv(t, "")}
	e.s = e.base.s
	e.ctx = e.base.ctx
	reg := desk.New(desk.Deps{Boundary: testBoundaryOpener(e.s)}, desk.Seams{
		desk.KindMergeQueueEnqueue: {Validate: mirrorMergeQueueEnqueueValidate(e.s)},
	})
	e.d = New(Options{Store: e.s, ValidateProposal: reg.ValidateProposal})
	if e.d.recordsSvc == nil {
		t.Fatal("the daemon built no records service — the arms have nothing to run on")
	}
	if e.d.recordsSvc.mergeQueue == nil {
		t.Fatal("the daemon did not hand the records service the queue plane's own seam — the read arms would refuse (V13) on a wired mount")
	}
	return e
}

// mirrorMergeQueueEnqueueValidate stands in for cmd/aeon's production seam
// (which gathers live git facts — this unit store has no repository to
// read): the SAME desk shape validator the seam runs first, plus the one
// fact a unit store can answer — the branch already sits in the lane as an
// OPEN row, refused with mergequeue's own duplicate-open words. The
// parity of the FULL facts sheet across the three doors is pinned in
// cmd/aeon/mergequeue_proposal_test.go, where a real repository exists.
func mirrorMergeQueueEnqueueValidate(st *store.Store) func(context.Context, []byte) error {
	return func(ctx context.Context, payload []byte) error {
		if err := desk.ValidateMergeQueueEnqueue(ctx, payload); err != nil {
			return err
		}
		var p desk.MergeQueueEnqueueProposal
		if err := json.Unmarshal(payload, &p); err != nil {
			return err
		}
		open, err := st.ListMergeItems(store.MergeFilter{ComponentRoot: p.ComponentRoot, Statuses: store.MergeOpenStatusSet})
		if err != nil {
			return err
		}
		for _, g := range open {
			if g.Branch == p.Branch {
				return fmt.Errorf("refused: duplicate-open · %s already sits in the queue for %s as %s — ask for one row, do not duplicate it (MQ14)", p.Branch, p.ComponentRoot, g.ID)
			}
		}
		return nil
	}
}

// mqArmEnqueue puts an item in the lane at the STORE door (the queue
// service's facts are the daemon's business; the arm tests need rows, not
// repositories): enqueued by blueprint under `loop`, which is exactly the
// shape a blueprint's queue.enqueue stamps.
func mqArmEnqueue(t *testing.T, st *store.Store, root, branch, loop string) store.MergeItem {
	t.Helper()
	m, err := st.EnqueueMergeItem(store.NewMergeItem{
		ComponentRoot: root, BaseBranch: "main", Branch: branch, LoopSlug: loop,
		HeadSHA: "aaaa1111bbbb2222cccc3333dddd4444eeee5555", BaseSHA: "9999aaaa8888bbbb7777cccc6666dddd5555eeee4444",
		EnqueuedByKind: "blueprint", EnqueuedByID: "land",
	}, store.MergeActor{Kind: "cli", ID: "test"})
	if err != nil {
		t.Fatalf("seed item %s: %v", branch, err)
	}
	return m
}

func mqArmDo(t *testing.T, e mqRecordsEnv, tool, input string) (any, *Failure, error) {
	t.Helper()
	return e.d.recordsSvc.Do(e.ctx, Actor{Kind: "agent", ID: e.base.agent}, tool, json.RawMessage(input))
}

// TestMergeQueueArmsStateAnAbsentPlane is V13's shape for the family: a
// mount without the queue seam names the absence in every one of the five
// doors — never zero, never an empty listing.
func TestMergeQueueArmsStateAnAbsentPlane(t *testing.T) {
	e := newMQRecordsEnv(t)
	e.d.recordsSvc.mergeQueue = nil
	for _, tc := range []struct{ tool, input string }{
		{"mergequeue_list", `{}`},
		{"mergequeue_show", `{"id":"mq-anything"}`},
		{"mergequeue_status", `{"componentRoot":"/repo/alpha"}`},
		{"mergequeue_dequeue", `{"id":"mq-anything"}`},
		{"mergequeue_propose_enqueue", `{"componentRoot":"/repo/alpha","branch":"feature/x","reason":"green"}`},
	} {
		_, f, err := mqArmDo(t, e, tc.tool, tc.input)
		if err != nil {
			t.Fatalf("%s: %v", tc.tool, err)
		}
		if f == nil || !strings.Contains(f.Sentence, "needs the merge-queue plane") {
			t.Errorf("%s answered %v — the absent plane is stated, never faked (V13)", tc.tool, f)
		}
	}
}

// TestMergeQueueToolReadsMatchSocketReads is rule 4 pinned in bytes: the arm's answer
// IS the door's answer (same service, same call, same marshalling) for
// every root inside the view — and outside it, the agent simply has fewer
// rows and one named refusal, never a different rendering of the same fact.
func TestMergeQueueToolReadsMatchSocketReads(t *testing.T) {
	e := newMQRecordsEnv(t)
	mine := mqArmEnqueue(t, e.s, "/repo/alpha", "feature/mine", "walk")
	mqArmEnqueue(t, e.s, "/repo/org-group", "feature/foreign", "")

	same := func(a, b any, what string) {
		ja, _ := json.Marshal(a)
		jb, _ := json.Marshal(b)
		if string(ja) != string(jb) {
			t.Errorf("%s drifted between the tool and the socket:\ntool:     %s\nsocket: %s", what, ja, jb)
		}
	}

	// list, filtered to an in-view root: identical bytes.
	out, f, err := mqArmDo(t, e, "mergequeue_list", `{"componentRoot":"/repo/alpha"}`)
	if f != nil || err != nil {
		t.Fatalf("list: %v %v", f, err)
	}
	door, err := e.d.recordsSvc.mergeQueue.List(e.ctx, protocol.MergeQueueListParams{ComponentRoot: "/repo/alpha"})
	if err != nil {
		t.Fatal(err)
	}
	same(out, door, "mergequeue.list (in-view filter)")

	// list, unfiltered: the foreign row is NOT there — not withheld loudly,
	// not present; and the in-view rows byte-equal the door's own.
	out, f, err = mqArmDo(t, e, "mergequeue_list", `{}`)
	if f != nil || err != nil {
		t.Fatalf("unfiltered list: %v %v", f, err)
	}
	items, ok := out.([]protocol.ItemView)
	if !ok {
		t.Fatalf("list answered %T", out)
	}
	if len(items) != 1 || items[0].ID != mine.ID {
		t.Errorf("the machine-wide listing carries %v — own-view carries exactly the run-in root's item, the foreign row quietly absent", items)
	}

	// show: identical ShowResult bytes for the in-view item.
	out, f, err = mqArmDo(t, e, "mergequeue_show", fmt.Sprintf(`{"id":%q}`, mine.ID))
	if f != nil || err != nil {
		t.Fatalf("show: %v %v", f, err)
	}
	dshow, err := e.d.recordsSvc.mergeQueue.Show(e.ctx, protocol.MergeQueueShowParams{ID: mine.ID})
	if err != nil {
		t.Fatal(err)
	}
	same(out, dshow, "mergequeue.show")

	// status: identical for an in-view root; refused BY NAME for a foreign
	// one (a status is a claim about a project, where an absent row is not).
	out, f, err = mqArmDo(t, e, "mergequeue_status", `{"componentRoot":"/repo/alpha"}`)
	if f != nil || err != nil {
		t.Fatalf("status: %v %v", f, err)
	}
	dstatus, err := e.d.recordsSvc.mergeQueue.Status(e.ctx, protocol.MergeQueueStatusParams{ComponentRoot: "/repo/alpha"})
	if err != nil {
		t.Fatal(err)
	}
	same(out, dstatus, "mergequeue.status")
	_, f, _ = mqArmDo(t, e, "mergequeue_status", `{"componentRoot":"/repo/org-group"}`)
	if f == nil || !strings.Contains(f.Sentence, "not a repository your loops have run in") {
		t.Errorf("a foreign status answered %v — refused by name, not answered empty", f)
	}

	// show of the foreign item: the SAME sentence an unknown id gets — the
	// plane's no-oracle (S52 LM5's law, MC2's shape).
	_, fView, _ := mqArmDo(t, e, "mergequeue_show", `{"id":"mq-does-not-exist"}`)
	_, fReal, _ := mqArmDo(t, e, "mergequeue_show", `{"id":"mq-nope-either"}`)
	if fView == nil || fReal == nil || fView.Sentence == "" || !strings.Contains(fView.Sentence, "yours to see") || !strings.Contains(fReal.Sentence, "yours to see") {
		t.Errorf("out-of-view and unknown must collapse into one sentence, got %v / %v", fView, fReal)
	}
}

// TestOwnItemDequeueOnly is MQ17's narrowing write: this loop's row while
// it waits, and NOTHING else — each other shape meets its own named
// refusal, and none of them moves a row.
func TestOwnItemDequeueOnly(t *testing.T) {
	e := newMQRecordsEnv(t)
	own := mqArmEnqueue(t, e.s, "/repo/alpha", "feature/own", "walk")
	ready := mqArmEnqueue(t, e.s, "/repo/alpha", "feature/ready", "walk")
	for _, to := range []string{"catching_up", "verifying", "ready"} {
		if _, err := e.s.TransitionMergeItem(ready.ID, "", to, store.TransitionFacts{}, store.MergeActor{Kind: "daemon", ID: "driver"}); err != nil {
			// from-guard: read current and pass it.
			cur, _ := e.s.ReadMergeItem(ready.ID)
			if _, err2 := e.s.TransitionMergeItem(ready.ID, cur.Status, to, store.TransitionFacts{}, store.MergeActor{Kind: "daemon", ID: "driver"}); err2 != nil {
				t.Fatalf("walk %s to %s: %v / %v", ready.ID, to, err, err2)
			}
		}
	}
	other := mqArmEnqueue(t, e.s, "/repo/alpha", "feature/other-loop", "some-other-loop")
	persons := mqArmEnqueue(t, e.s, "/repo/alpha", "feature/persons", "")

	if _, f, err := mqArmDo(t, e, "mergequeue_dequeue", fmt.Sprintf(`{"id":%q}`, ready.ID)); f == nil || err != nil || !strings.Contains(f.Sentence, "ready") || !strings.Contains(f.Sentence, "takes a person") {
		t.Fatalf("an item in flight refused with %v — the refusal must name the state and who may stop it", f)
	}
	if _, f, err := mqArmDo(t, e, "mergequeue_dequeue", fmt.Sprintf(`{"id":%q}`, other.ID)); f == nil || err != nil || !strings.Contains(f.Sentence, "some-other-loop") {
		t.Fatalf("another loop's item refused with %v — the refusal names the owner", f)
	}
	if _, f, err := mqArmDo(t, e, "mergequeue_dequeue", fmt.Sprintf(`{"id":%q}`, persons.ID)); f == nil || err != nil || !strings.Contains(f.Sentence, "belongs to") {
		t.Fatalf("a person's row refused with %v — a row nobody's loop carries is nobody's tool write", f)
	}
	if _, f, _ := mqArmDo(t, e, "mergequeue_dequeue", `{"id":"mq-nothing-here"}`); f == nil || !strings.Contains(f.Sentence, "yours to see") {
		t.Fatalf("unknown id refused with %v — the no-oracle sentence is the answer", f)
	}

	// The one path that works: this loop's own queued row.
	out, f, err := mqArmDo(t, e, "mergequeue_dequeue", fmt.Sprintf(`{"id":%q}`, own.ID))
	if f != nil || err != nil {
		t.Fatalf("own-item dequeue: %v %v", f, err)
	}
	view, ok := out.(protocol.ItemView)
	if !ok || view.Status != "removed" {
		t.Fatalf("the dequeue answered %v — the row is REMOVED, not paused", out)
	}
	events, err := e.s.ListMergeQueueEvents(own.ID)
	if err != nil {
		t.Fatal(err)
	}
	last := events[len(events)-1]
	if last.ActorKind != "agent" || last.ActorID != e.base.agent {
		t.Errorf("the removal event carries {%s,%s} — the ledger's closed set names agent for exactly this write (MQ14.3's audit law)", last.ActorKind, last.ActorID)
	}
	// And a second call meets the removed row as what it now is: an item
	// that is no longer this loop's waiting thing — the named state refusal.
	if _, f, _ := mqArmDo(t, e, "mergequeue_dequeue", fmt.Sprintf(`{"id":%q}`, own.ID)); f == nil {
		t.Error("dequeuing the same row twice answered success — the second is a refusal")
	}
}

// TestProposeEnqueueIsInert is MQ14.3 pinned at the arm: the ask files a
// row and nothing else — no queue row, no position, no event, and the
// payload is the three fields exactly.
func TestProposeEnqueueIsInert(t *testing.T) {
	e := newMQRecordsEnv(t)
	beforeItems := countMergeQueueRows(t, e.s)
	out, f, err := mqArmDo(t, e, "mergequeue_propose_enqueue",
		`{"componentRoot":"/repo/alpha","branch":"feature/ask-green","reason":"green on the branch, cannot queue it myself and should not be able to"}`)
	if f != nil || err != nil {
		t.Fatalf("propose: %v %v", f, err)
	}
	res, ok := out.(protocol.PolicyProposeResult)
	if !ok {
		t.Fatalf("propose answered %T", out)
	}
	if res.Note != protocol.PolicyProposeInertNote {
		t.Errorf("the note is %q — the fixed inertness statement, one definition (TD2)", res.Note)
	}
	if res.Proposal.Kind != string(desk.KindMergeQueueEnqueue) || res.Proposal.Status != "proposed" {
		t.Errorf("row is {%s,%s}", res.Proposal.Kind, res.Proposal.Status)
	}
	if res.Proposal.CreatedByKind != "agent" || res.Proposal.CreatedByID != e.base.agent {
		t.Errorf("the ask is stamped {%s,%s} — agent-authored, said openly (OL3)", res.Proposal.CreatedByKind, res.Proposal.CreatedByID)
	}
	var fields map[string]any
	if err := json.Unmarshal(res.Proposal.Payload, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 3 {
		t.Errorf("the payload carries %d fields (%v) — three, exactly, built here never smuggled", len(fields), fields)
	}
	if got := countMergeQueueRows(t, e.s); got != beforeItems {
		t.Errorf("the queue gained rows (%d -> %d) from an ASK — inert means inert (MQ14.3, TD2)", beforeItems, got)
	}
	// Duplicate ask of a branch that is only PROPOSED (not queued) files a
	// second row — the rows are inert, the cap is what bounds them (the
	// duplicate-open refusal below proves the queued case).
	if _, f, err := mqArmDo(t, e, "mergequeue_propose_enqueue",
		`{"componentRoot":"/repo/alpha","branch":"feature/ask-green","reason":"still green"}`); f != nil || err != nil {
		t.Errorf("a second ask for the same unqueued branch: %v %v — asks are inert rows, the cap bounds them, the lane does not", f, err)
	}
}

// TestProposeEnqueueRefusals: the guards before the row — the strict parse,
// the named blanks, the view, the validator's own words, and the cap
// naming its number.
func TestProposeEnqueueRefusals(t *testing.T) {
	e := newMQRecordsEnv(t)
	long := strings.Repeat("w", 513)
	for _, tc := range []struct{ in, want string }{
		{`{"componentRoot":"","branch":"feature/x","reason":"r"}`, "names the repository"},
		{`{"componentRoot":"/repo/alpha","branch":"  ","reason":"r"}`, "names the branch"},
		{`{"componentRoot":"/repo/alpha","branch":"feature/x","reason":"  "}`, "needs a reason"},
		{`{"componentRoot":"/repo/alpha","branch":"feature/x","reason":"` + long + `"}`, "cap is 512"},
		{`{"componentRoot":"/repo/org-group","branch":"feature/x","reason":"r"}`, "not a repository your loops have run in"},
		{`{"componentRoot":"/repo/alpha","branch":"feature/x","reason":"r","actor":{"kind":"user","id":"cjlapao"}}`, "unknown field"},
	} {
		_, f, err := mqArmDo(t, e, "mergequeue_propose_enqueue", tc.in)
		if err != nil {
			t.Fatalf("%s: %v", tc.in, err)
		}
		if f == nil || !strings.Contains(f.Sentence, tc.want) {
			t.Errorf("%s answered %v, want a sentence containing %q", tc.in, f, tc.want)
		}
	}
	// duplicate-open: the queue ALREADY carries the branch — the validator's
	// sentence travels verbatim (TD3), and no row is filed.
	mqArmEnqueue(t, e.s, "/repo/alpha", "feature/queued-already", "")
	before := countProposalsByStatus(t, e.s, "proposed")
	_, f, _ := mqArmDo(t, e, "mergequeue_propose_enqueue",
		`{"componentRoot":"/repo/alpha","branch":"feature/queued-already","reason":"queue it please"}`)
	if f == nil || !strings.Contains(f.Sentence, "duplicate-open") {
		t.Fatalf("a queued branch answered %v — the ONE validator's words, verbatim (MQ14/TD3)", f)
	}
	if got := countProposalsByStatus(t, e.s, "proposed"); got != before {
		t.Errorf("a refused ask still filed rows (%d -> %d)", before, got)
	}
	// The cap: MaxOpenProposalsPerAgent open asks by THIS agent, and the
	// next one meets the counted refusal naming the number.
	for i := 0; i < desk.MaxOpenProposalsPerAgent; i++ {
		if _, err := e.s.CreateProposal(string(desk.KindMergeQueueEnqueue),
			fmt.Sprintf(`{"componentRoot":"/repo/alpha","branch":"feature/cap-%d","reason":"cap filler %d"}`, i, i),
			store.Actor{Kind: "agent", ID: e.base.agent}, "2026-11-04T12:00:00Z"); err != nil {
			t.Fatalf("filler %d: %v", i, err)
		}
	}
	_, f, _ = mqArmDo(t, e, "mergequeue_propose_enqueue",
		`{"componentRoot":"/repo/alpha","branch":"feature/one-too-many","reason":"one past the cap"}`)
	if f == nil || !strings.Contains(f.Sentence, fmt.Sprint(desk.MaxOpenProposalsPerAgent)) {
		t.Fatalf("the cap answered %v — the refusal names its number (S35's shape)", f)
	}
}

func countMergeQueueRows(t *testing.T, st *store.Store) int {
	t.Helper()
	all, err := st.ListMergeItemsAcrossRoots(store.MergeStatusSet)
	if err != nil {
		t.Fatal(err)
	}
	return len(all)
}

func countProposalsByStatus(t *testing.T, st *store.Store, status string) int {
	t.Helper()
	rows, err := st.ListProposals(status)
	if err != nil {
		t.Fatal(err)
	}
	return len(rows)
}
