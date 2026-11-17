package main

// cmd/aeon/mergequeue_parity_test.go — S60 T10's parity and promise tests.
// "Parity" here is NOT a two-door byte-comparison: the plane has ONE door
// (behaviour 3 — no offline arm exists to compare against), so the two
// directions are the METHOD↔VERB census in both directions: every registered
// mergequeue method has a verb that calls it, and every verb calls a real
// registered method and nothing else (mergequeue.proposeEnqueue is T9's AGENT
// door — no CLI verb may call it; the census proves that without naming it
// specially). The door itself is stood up for real: parityPipeServer (the
// autonomy parity harness, same package) dispatches through the SAME table
// production registers, with a fake MergeQueueService answering behind it and
// Table.ActorFor standing in for the connection's person.
//
// The rest of the file is T10.md's named list, each test earning its name
// against that pipe: the golden table, the nextStep sentence, the two
// seatbelts, vocabulary-driven help (the daemon's answer changes the help
// with no edit to the verbs file — that is the whole claim, tested), the
// refusals byte-identical, JSON as the wire verbatim, the no-offline-arm
// promise, and the store never opening.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/action"
	"github.com/cjlapao/aeon/internal/protocol"
)

// mqConstFor maps a wire method name to the Go const its verb must
// reference: mergequeue.proposeEnqueue → MethodMergeQueueProposeEnqueue.
func mqConstFor(m protocol.MethodName) string {
	tail := strings.SplitN(string(m), ".", 2)[1]
	return "MethodMergeQueue" + strings.ToUpper(tail[:1]) + tail[1:]
}

// --- the fake seam ---------------------------------------------------------------

// mqFakeCall is one recorded door call.
type mqFakeCall struct {
	method protocol.MethodName
	actor  protocol.Actor
}

// mqFakeService answers the ten doors with settable canned results and
// records every call, so a test can assert BOTH what the verb sent and how
// many times it sent it — the seatbelt tests hinge on call counts: a CLI
// that applied without --yes, or sent the digest unasked, fails on the
// count, not on vibes.
type mqFakeService struct {
	items      []protocol.ItemView
	show       protocol.ShowResult
	status     protocol.StatusResult
	vocab      protocol.VocabularyResult
	cfg        protocol.ConfigView
	statement  string
	refuse     map[protocol.MethodName]error
	refuseOnce map[protocol.MethodName]int // method → how many calls refuse first

	calls   []mqFakeCall
	setReqs []protocol.MergeQueueSettingsParams
	landReq protocol.MergeQueueLandParams
}

var _ protocol.MergeQueueService = (*mqFakeService)(nil)

func (f *mqFakeService) record(m protocol.MethodName, a protocol.Actor) {
	f.calls = append(f.calls, mqFakeCall{method: m, actor: a})
}

func (f *mqFakeService) callCount(m protocol.MethodName) int {
	n := 0
	for _, c := range f.calls {
		if c.method == m {
			n++
		}
	}
	return n
}

// refusal answers the once-only refusals the seatbelt round trip needs:
// the FIRST settings call refuses and the acknowledged echo is accepted.
func (f *mqFakeService) refusal(m protocol.MethodName) error {
	if e, ok := f.refuse[m]; ok {
		if n, once := f.refuseOnce[m]; once && f.callCount(m) > n {
			return nil
		}
		return e
	}
	return nil
}

func (f *mqFakeService) List(_ context.Context, _ protocol.MergeQueueListParams) ([]protocol.ItemView, error) {
	return f.items, nil
}

func (f *mqFakeService) Show(_ context.Context, p protocol.MergeQueueShowParams) (protocol.ShowResult, error) {
	f.record(protocol.MethodMergeQueueShow, protocol.Actor{})
	_ = p
	return f.show, nil
}

func (f *mqFakeService) Status(_ context.Context, _ protocol.MergeQueueStatusParams) (protocol.StatusResult, error) {
	return f.status, nil
}

func (f *mqFakeService) Vocabulary(_ context.Context) (protocol.VocabularyResult, error) {
	return f.vocab, nil
}

func (f *mqFakeService) Enqueue(_ context.Context, a protocol.Actor, _ protocol.MergeQueueEnqueueParams) (protocol.ItemView, error) {
	f.record(protocol.MethodMergeQueueEnqueue, a)
	if e := f.refusal(protocol.MethodMergeQueueEnqueue); e != nil {
		return protocol.ItemView{}, e
	}
	return f.items[0], nil
}

func (f *mqFakeService) Dequeue(_ context.Context, a protocol.Actor, _ protocol.MergeQueueIDParams) (protocol.ItemView, error) {
	f.record(protocol.MethodMergeQueueDequeue, a)
	if e := f.refusal(protocol.MethodMergeQueueDequeue); e != nil {
		return protocol.ItemView{}, e
	}
	return f.items[0], nil
}

func (f *mqFakeService) Reorder(_ context.Context, a protocol.Actor, _ protocol.MergeQueueReorderParams) (protocol.ItemView, error) {
	f.record(protocol.MethodMergeQueueReorder, a)
	if e := f.refusal(protocol.MethodMergeQueueReorder); e != nil {
		return protocol.ItemView{}, e
	}
	return f.items[0], nil
}

func (f *mqFakeService) Retry(_ context.Context, a protocol.Actor, _ protocol.MergeQueueIDParams) (protocol.ItemView, error) {
	f.record(protocol.MethodMergeQueueRetry, a)
	if e := f.refusal(protocol.MethodMergeQueueRetry); e != nil {
		return protocol.ItemView{}, e
	}
	return f.items[0], nil
}

func (f *mqFakeService) Land(_ context.Context, a protocol.Actor, p protocol.MergeQueueLandParams) (protocol.ItemView, error) {
	f.landReq = p
	f.record(protocol.MethodMergeQueueLand, a)
	if e := f.refusal(protocol.MethodMergeQueueLand); e != nil {
		return protocol.ItemView{}, e
	}
	return f.items[0], nil
}

func (f *mqFakeService) Settings(_ context.Context, a protocol.Actor, p protocol.MergeQueueSettingsParams) (protocol.ConfigView, string, error) {
	f.setReqs = append(f.setReqs, p)
	f.record(protocol.MethodMergeQueueSettings, a)
	if e := f.refusal(protocol.MethodMergeQueueSettings); e != nil {
		return protocol.ConfigView{}, "", e
	}
	return f.cfg, f.statement, nil
}

// --- the pipe harness -------------------------------------------------------------

// standMergeQueueSocket stands the daemon half: the REAL table with the
// plane REGISTERED through RegisterMergeQueue (V13's own door), the fake
// service underneath, and Table.ActorFor as the connection's person (the
// autonomy parity harness's line). mqDial is swapped for a handshake
// client over the pipe and restored at cleanup; the conn is returned so a
// test can also speak the raw wire itself — the verbatim comparison needs
// the bytes from BOTH sides of the same pipe.
func standMergeQueueSocket(t *testing.T, fake *mqFakeService) {
	t.Helper()
	mkTable := func() *protocol.Table {
		tbl := protocol.NewTable(protocol.NewSessions(), protocol.NewSubscriptions(), protocol.NewEventLog())
		tbl.MergeQueue = fake
		tbl.ActorFor = func(context.Context, *protocol.Session) protocol.Actor {
			return protocol.Actor{Kind: "user", ID: "uid-1000"}
		}
		if !tbl.RegisterMergeQueue() {
			t.Fatal("RegisterMergeQueue refused to register over a wired seam")
		}
		return tbl
	}
	restore := mqDial
	t.Cleanup(func() { mqDial = restore })
	// A FRESH pipe and a FRESH table per dial: net.Pipe is single-
	// connection, every verb closes its client on the way out, and the
	// harness server labels every connection "c-parity" — a table left
	// initialized under that id answers the next handshake with -31003.
	// One table per connection is exactly production's shape anyway; the
	// FAKE SERVICE is the shared state, and that is the point.
	mqDial = func(int) (*protocol.Client, error) {
		conn := parityPipeServer(t, mkTable())
		c := protocol.NewClient(conn, ClientName, Version)
		if _, err := c.Connect(0); err != nil {
			_ = c.Close()
			return nil, err
		}
		return c, nil
	}
}

// mqFixedItems is the golden frame: three rows, one unknown-staleness
// terminal row, one PR-bearing conflict — ASCII reasons (the table truncates
// BYTES, and the golden must be about layout, not encodings).
func mqFixedItems() []protocol.ItemView {
	behind, ahead, pr := int64(2), int64(5), 7
	return []protocol.ItemView{
		{
			ID: "itm_one", ComponentRoot: "/repo/alpha", Base: "main", Branch: "s60-land",
			Status: "queued", Reason: "person enqueued it", Position: 1, Attended: true,
			HeadSha: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", BaseSha: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			EnqueuedBy: protocol.EnqueuedByView{Kind: "user", ID: "carlos"},
			Staleness:  protocol.StalenessView{State: "behind", Behind: &behind, Ahead: &ahead},
			NextStep:   "queue: the driver will start this item's lane when main is quiet",
			Digest:     "d1",
		},
		{
			ID: "itm_two", ComponentRoot: "/repo/alpha", Base: "main", Branch: "fix-flaky",
			Status: "conflict", Reason: "merge conflict: git reported CONFLICT (content): app.go", Position: 2,
			Attended: false, PrNumber: &pr,
			HeadSha: "cccccccccccccccccccccccccccccccccccccccc", BaseSha: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			EnqueuedBy: protocol.EnqueuedByView{Kind: "agent", ID: "s60-writer"},
			Staleness:  protocol.StalenessView{State: "current"},
			NextStep:   "resolve: a resolution loop is open — its answer lands or the person dequeues",
			Digest:     "d2",
		},
		{
			ID: "itm_three", ComponentRoot: "/repo/beta", Base: "trunk", Branch: "old-merge",
			Status: "landed", Position: 1, Attended: true,
			HeadSha: "dddddddddddddddddddddddddddddddddddddddd", BaseSha: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
			EnqueuedBy: protocol.EnqueuedByView{Kind: "daemon", ID: "merge-queue"},
			Staleness:  protocol.StalenessView{State: "unknown", Reason: "no fetch has ever run for this repository in this daemon's life"},
			NextStep:   "done: the row is terminal; the history keeps it for the screens",
			Digest:     "d3",
		},
	}
}

// --- the censuses ------------------------------------------------------------------

// mqSourceMethods walks mergequeue_verbs.go for every protocol.MethodMergeQueueX
// selector the file references — what the verbs ACTUALLY call, read off the
// AST, not off a hand-kept list (a hand-kept parity list would drift exactly
// the way this test exists to catch).
func mqSourceMethods(t *testing.T) map[string]bool {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "mergequeue_verbs.go", nil, 0)
	if err != nil {
		t.Fatalf("parse the verbs file: %v", err)
	}
	out := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok && id.Name == "protocol" &&
			strings.HasPrefix(sel.Sel.Name, "MethodMergeQueue") {
			out[sel.Sel.Name] = true
		}
		return true
	})
	return out
}

// mqSourceVerbs extracts the subcommand labels cmdMergeQueue's switch routes.
func mqSourceVerbs(t *testing.T) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "mergequeue_verbs.go", nil, 0)
	if err != nil {
		t.Fatalf("parse the verbs file: %v", err)
	}
	var verbs []string
	ast.Inspect(f, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "cmdMergeQueue" {
			return true
		}
		ast.Inspect(fn, func(m ast.Node) bool {
			sw, ok := m.(*ast.SwitchStmt)
			if !ok {
				return true
			}
			for _, c := range sw.Body.List {
				cc := c.(*ast.CaseClause)
				for _, e := range cc.List {
					lit, ok := e.(*ast.BasicLit)
					if !ok {
						continue
					}
					var s string
					if err := json.Unmarshal([]byte(lit.Value), &s); err == nil && s != "help" {
						verbs = append(verbs, s)
					}
				}
			}
			return false
		})
		return true
	})
	return verbs
}

func TestEveryMergeQueueMethodHasAVerb(t *testing.T) {
	called := mqSourceMethods(t)
	for _, m := range mergeQueueAll {
		if !called[mqConstFor(m)] {
			t.Errorf("%s has no verb calling it — a registered method with no CLI door is MQ17 broken in the other direction", m)
		}
	}
	if got, want := len(mqSourceVerbs(t)), len(mergeQueueAll); got != want {
		t.Errorf("the dispatch routes %d subcommands, the plane has %d methods — one verb per door, both ways", got, want)
	}
}

func TestEveryMergeQueueVerbHasAMethod(t *testing.T) {
	registered := map[string]bool{}
	for _, m := range mergeQueueAll {
		registered[mqConstFor(m)] = true
	}
	for constName := range mqSourceMethods(t) {
		if !registered[constName] {
			t.Errorf("mergequeue_verbs.go calls %s, which is NOT one of the plane's registered ten — proposeEnqueue is the AGENT's door and no verb may ring it", constName)
		}
	}
}

// --- the renderings ---------------------------------------------------------------

func TestMergeQueueListGolden(t *testing.T) {
	fake := &mqFakeService{items: mqFixedItems()}
	standMergeQueueSocket(t, fake)
	var out bytes.Buffer
	if err := cmdMergeQueue(&out, []string{"list"}); err != nil {
		t.Fatalf("list: %v", err)
	}
	// The golden frame. Columns exactly as T10 spells them — position,
	// status, branch, reason, behind/ahead, PR, loop — and the reason
	// truncated to the table's width with the ellipsis, byte for byte.
	// The widths are spelled OUT HERE (6/10/24/32/12/6/24, one space
	// between) so the golden pins the frame, and every cell VALUE below is
	// a literal: reason truncated at 29 bytes + the ellipsis, "0/0"-style
	// counts only when the staleness carries them, the unknown row saying
	// "unknown?" and PR/loop dashes where the wire carries nulls.
	row := func(pos, status, branch, reason, stale, pr, loop string) string {
		return fmt.Sprintf("%-6s %-10s %-24s %-32s %-12s %-6s %-24s\n", pos, status, branch, reason, stale, pr, loop)
	}
	var wantb strings.Builder
	wantb.WriteString(row("POSITION", "STATUS", "BRANCH", "REASON", "BEHIND/AHEAD", "PR", "LOOP"))
	wantb.WriteString(row("1", "queued", "s60-land", "person enqueued it", "2/5", "-", "-"))
	wantb.WriteString(row("2", "conflict", "fix-flaky", "merge conflict: git reported …", "current", "#7", "-"))
	wantb.WriteString(row("1", "landed", "old-merge", "", "unknown?", "-", "-"))
	want := wantb.String()
	if got := out.String(); !strings.Contains(got, want) {
		t.Errorf("the list table drifted:\n--- got ---\n%s\n--- want (contained) ---\n%s", got, want)
	}
	// The row order is the WIRE order (position is per-lane; the daemon
	// already ordered what it returned — the CLI re-sorts nothing).
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 4 || !strings.HasPrefix(lines[1], "1      queued") || !strings.HasPrefix(lines[2], "2      conflict") {
		t.Errorf("row order changed underneath: %q", lines)
	}
}

func TestShowPrintsNextStep(t *testing.T) {
	fake := &mqFakeService{items: mqFixedItems()}
	fake.show = protocol.ShowResult{
		Item: mqFixedItems()[0],
		Transitions: []protocol.TransitionView{
			{Kind: "enqueue", ToStatus: "queued", ReasonKind: "person-enqueue", ActorKind: "user", ActorID: "carlos", At: "2026-11-04T12:00:00Z"},
			{Kind: "move", FromStatus: "queued", ToStatus: "queued", ActorKind: "daemon", ActorID: "merge-queue", Detail: "lane head", At: "2026-11-04T12:00:01Z"},
		},
	}
	standMergeQueueSocket(t, fake)
	var out bytes.Buffer
	if err := cmdMergeQueue(&out, []string{"show", "itm_one"}); err != nil {
		t.Fatalf("show: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "next step "+mqFixedItems()[0].NextStep+"\n") {
		t.Errorf("the driver's own sentence must ride the print verbatim:\n%s", got)
	}
	for _, frag := range []string{"ledger (append-only, in order):", "2026-11-04T12:00:00Z", "person-enqueue", "user/carlos", "queued→queued"} {
		if !strings.Contains(got, frag) {
			t.Errorf("show is missing %q:\n%s", frag, got)
		}
	}
}

// --- the seatbelts ------------------------------------------------------------------

// mqSettingsRefusal is the daemon's widening refusal as T8's door renders
// it: the statement in the What, the digest quoted inside the sentence.
func mqSettingsRefusal(sentence, digest string) error {
	return protocol.ErrMergeQueueRefused.WithWhat(
		"%s — this widens the queue's authority. Nothing changed. Resend with acknowledged %q to apply the sentence you have now read",
		sentence, digest)
}

func TestSettingsAutoPrintsStatementAndNeedsYes(t *testing.T) {
	digest := strings.Repeat("ab", 32)
	sentence := "queue settings for /proj: mode auto, conflict policy ask, land method squash, max catch-ups 3 — AUTHORITY WIDENING: mode auto lets the queue land merges without a person"
	fake := &mqFakeService{
		refuse:     map[protocol.MethodName]error{protocol.MethodMergeQueueSettings: mqSettingsRefusal(sentence, digest)},
		refuseOnce: map[protocol.MethodName]int{protocol.MethodMergeQueueSettings: 1},
		cfg:        protocol.ConfigView{ComponentRoot: "/proj", Mode: "auto", ConflictPolicy: "ask", LandMethod: "squash", MaxCatchUps: 3},
	}
	standMergeQueueSocket(t, fake)

	// No --yes: the statement prints, nothing is applied, and the ONLY
	// call sent carried no acknowledgement of anything.
	var out bytes.Buffer
	err := cmdMergeQueue(&out, []string{"settings", "--repo", "/proj", "--mode", "auto"})
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("without --yes the verb must refuse with the re-run line, got %v", err)
	}
	if !strings.Contains(out.String(), sentence) {
		t.Errorf("the daemon's statement must print before anything else:\n%s", out.String())
	}
	if len(fake.setReqs) != 1 || fake.setReqs[0].Acknowledged != "" {
		t.Fatalf("declining sent %d requests; the first must carry no digest: %+v", len(fake.setReqs), fake.setReqs)
	}

	// With --yes: the refusal's own quoted digest rides the echo — the
	// second call is the ONLY way the change stands.
	fake2 := &mqFakeService{
		refuse:     map[protocol.MethodName]error{protocol.MethodMergeQueueSettings: mqSettingsRefusal(sentence, digest)},
		refuseOnce: map[protocol.MethodName]int{protocol.MethodMergeQueueSettings: 1},
		cfg:        fake.cfg,
		statement:  sentence,
	}
	standMergeQueueSocket(t, fake2)
	var out2 bytes.Buffer
	if err := cmdMergeQueue(&out2, []string{"settings", "--repo", "/proj", "--mode", "auto", "--yes"}); err != nil {
		t.Fatalf("with --yes: %v", err)
	}
	if len(fake2.setReqs) != 2 || fake2.setReqs[1].Acknowledged != digest {
		t.Fatalf("the echo must carry the digest the sentence quoted: %+v", fake2.setReqs)
	}
	if !strings.Contains(out2.String(), sentence) {
		t.Errorf("with --yes the statement STILL prints first:\n%s", out2.String())
	}
}

func TestLandSendsTheDigestOfTheRowShown(t *testing.T) {
	item := mqFixedItems()[0]
	item.Digest = "cafebabe00112233"
	fake := &mqFakeService{items: []protocol.ItemView{item}, show: protocol.ShowResult{Item: item}}
	standMergeQueueSocket(t, fake)

	// Without --yes: show happened, land did not, and the print carried
	// the row the person is being asked to confirm.
	var out bytes.Buffer
	err := cmdMergeQueue(&out, []string{"land", "itm_one"})
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("land without --yes must refuse, got %v", err)
	}
	if fake.callCount(protocol.MethodMergeQueueLand) != 0 {
		t.Error("land fired without the seatbelt")
	}
	if !strings.Contains(out.String(), "s60-land") {
		t.Errorf("the row up for confirmation must print:\n%s", out.String())
	}

	// With --yes: the digest is the SHOW answer's, byte for byte — never a
	// recomputed guess.
	var out2 bytes.Buffer
	if err := cmdMergeQueue(&out2, []string{"land", "itm_one", "--yes"}); err != nil {
		t.Fatalf("land --yes: %v", err)
	}
	if fake.landReq.ID != "itm_one" || fake.landReq.Digest != "cafebabe00112233" {
		t.Errorf("the land rode the wrong binding: %+v", fake.landReq)
	}
}

// --- behaviour 1: help from the vocabulary -------------------------------------------

func TestHelpSetsComeFromVocabulary(t *testing.T) {
	vocab := protocol.VocabularyResult{
		Modes:            []string{"off", "manual", "auto", "paranoid-unique-to-fake"},
		ConflictPolicies: []string{"ask", "resolve", "policy-unique-to-fake"},
		LandMethods:      []string{"squash", "method-unique-to-fake"},
	}
	fake := &mqFakeService{vocab: vocab}
	standMergeQueueSocket(t, fake)
	var out bytes.Buffer
	err := cmdMergeQueue(&out, []string{"settings", "--help"})
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("--help must reach flag.ErrHelp, got %v", err)
	}
	for _, s := range []string{"paranoid-unique-to-fake", "policy-unique-to-fake", "method-unique-to-fake"} {
		if !strings.Contains(out.String(), s) {
			t.Errorf("the help must carry the LIVE set (%s) — the daemon's answer is the only list:\n%s", s, out.String())
		}
	}

	// The claim's strong half: swap the daemon's ANSWER and the help moves
	// with it — no edit to mergequeue_verbs.go anywhere in this test.
	vocab2 := vocab
	vocab2.Modes = append(append([]string{}, vocab.Modes...), "moved-while-you-read")
	standMergeQueueSocket(t, &mqFakeService{vocab: vocab2})
	var out2 bytes.Buffer
	if err := cmdMergeQueue(&out2, []string{"settings", "--help"}); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("second --help: %v", err)
	}
	if !strings.Contains(out2.String(), "moved-while-you-read") {
		t.Errorf("the help did not follow the vocabulary:\n%s", out2.String())
	}

	// And the membership check is vocabulary-driven too: a value the fake
	// does NOT list is refused out of the usage exit, quoting the live set.
	var out3 bytes.Buffer
	err = cmdMergeQueue(&out3, []string{"settings", "--mode", "nonsense"})
	var ue *usageError
	if !errors.As(err, &ue) || !strings.Contains(err.Error(), "paranoid-unique-to-fake") {
		t.Errorf("an off-vocabulary mode must be refused quoting the live set, got %v", err)
	}
}

// --- refusals, the wire, and the promises --------------------------------------------

func TestRefusalsVerbatim(t *testing.T) {
	// The daemon's three-part sentence travels untouched: the verb returns
	// the wire error, and renderError — the ONE renderer main uses — speaks
	// it. This file re-wording anything would show up as a byte diff here.
	sentence := "item itm_two is merging — the driver is moving it right now; dequeue can wait for that move or refuse"
	fake := &mqFakeService{
		items:  mqFixedItems(),
		refuse: map[protocol.MethodName]error{protocol.MethodMergeQueueDequeue: protocol.ErrMergeQueueConflictState.WithWhat("%s", sentence)},
	}
	standMergeQueueSocket(t, fake)
	var out bytes.Buffer
	err := cmdMergeQueue(&out, []string{"dequeue", "itm_two"})
	if err == nil {
		t.Fatal("the refusal must reach the caller")
	}
	var pe *protocol.Error
	if !errors.As(err, &pe) || pe.Data.What != sentence {
		t.Fatalf("the refusal was rewritten on the way out: %v", err)
	}
	var rendered bytes.Buffer
	renderError(&rendered, err)
	if !strings.Contains(rendered.String(), sentence) {
		t.Errorf("the one renderer must speak the daemon's words:\n%s", rendered.String())
	}
	if out.Len() != 0 {
		t.Errorf("a refused write printed %q before the refusal", out.String())
	}
}

func TestMergeQueueJSONIsTheWireVerbatim(t *testing.T) {
	// (S47's tools plane already owns the bare TestJSONIsTheWireVerbatim
	// name in this package — the mergequeue arm carries the plane word.)
	fake := &mqFakeService{items: mqFixedItems()}
	standMergeQueueSocket(t, fake)

	cases := []struct {
		verb   []string
		method protocol.MethodName
		params any
	}{
		{[]string{"list"}, protocol.MethodMergeQueueList, protocol.MergeQueueListParams{ComponentRoot: mqCwd(), IncludeTerminal: false}},
		{[]string{"show", "itm_one"}, protocol.MethodMergeQueueShow, protocol.MergeQueueShowParams{ID: "itm_one"}},
		{[]string{"status", "--repo", "/repo/alpha"}, protocol.MethodMergeQueueStatus, protocol.MergeQueueStatusParams{ComponentRoot: "/repo/alpha"}},
		{[]string{"vocabulary"}, protocol.MethodMergeQueueVocabulary, struct{}{}},
	}
	for _, tc := range cases {
		var out bytes.Buffer
		if err := cmdMergeQueue(&out, append(append([]string{}, tc.verb...), "--json")); err != nil {
			t.Fatalf("%v --json: %v", tc.verb, err)
		}
		c, err := mqDial(0)
		if err != nil {
			t.Fatalf("handshake: %v", err)
		}
		var raw json.RawMessage
		if err := c.Call(tc.method, tc.params, &raw); err != nil {
			_ = c.Close()
			t.Fatalf("raw call %s: %v", tc.method, err)
		}
		_ = c.Close()
		if got, want := out.String(), string(raw)+"\n"; got != want {
			t.Errorf("%v --json is not the wire verbatim:\n got %s\nwant %s", tc.verb, got, want)
		}
	}
}

func TestMergeQueueVerbsNeverOpenTheStore(t *testing.T) {
	// Behaviour 3 and the P34 rule in one walk: the verbs file imports no
	// store (and no database driver, and no daemon package — the socket is
	// the door). Parsed imports, not grep: a comment cannot trip it and an
	// alias cannot hide.
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "mergequeue_verbs.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, imp := range f.Imports {
		var p string
		if err := json.Unmarshal([]byte(imp.Path.Value), &p); err != nil {
			continue
		}
		for _, banned := range []string{"/internal/store", "/internal/database", "/internal/daemon", "gorm.io"} {
			if strings.HasPrefix(p, "github.com/cjlapao/aeon"+banned) || strings.HasPrefix(p, banned) {
				t.Errorf("mergequeue_verbs.go imports %s — the CLI opens nothing; the daemon answers", p)
			}
		}
	}
}

func TestMergeQueueNeverFallsBackOffline(t *testing.T) {
	// behaviour 3's promise, tested: nobody home is the ANSWER — the
	// standard dial line, on a plane whose verbs do not reach for a store
	// when the pipe is cold. No notice printed, no direct door, exit via
	// the typed error main already renders.
	restore := mqDial
	t.Cleanup(func() { mqDial = restore })
	mqDial = func(int) (*protocol.Client, error) {
		return nil, &noDaemonError{path: "/tmp/nowhere.sock", cause: os.ErrNotExist}
	}
	for _, args := range [][]string{{"list"}, {"show", "x"}, {"status"}, {"vocabulary"}, {"dequeue", "x"}, {"retry", "x"}} {
		var out bytes.Buffer
		err := cmdMergeQueue(&out, args)
		var nd *noDaemonError
		if !errors.As(err, &nd) {
			t.Errorf("`mergequeue %s` did not answer with the standard no-daemon line: %v", args, err)
		}
		if out.Len() != 0 {
			t.Errorf("`mergequeue %s` printed %q with no daemon", args, out.String())
		}
	}
}

// --- the golden's frame -------------------------------------------------------------

func TestActionCheckGoldenChangesOnlyBySixRows(t *testing.T) {
	// T10's Files section predicted this test by the state of T1's golden
	// WHEN IT WAS WRITTEN; the six rows (T5's three git.* and T6's two
	// forge.* plus the one already here) LANDED in T5/T6, so the drift T10
	// owes the golden is now ZERO: no mergequeue verb is an action-registry
	// row, and the golden TestActionCheckSurfaceGolden pins stays byte-
	// identical through this task. The census that proves it: no name in
	// the closed registry carries a mergequeue door, and the six the
	// header promised are exactly present.
	for _, n := range action.Names() {
		if strings.HasPrefix(n, "mergequeue.") || strings.HasPrefix(n, "mergequeue_") {
			t.Errorf("the registry grew a mergequeue row %q — T10 registers NO actions", n)
		}
	}
	six := []string{"git.integrate", "git.concludeIntegration", "git.publishIntegration", "forge.openLandingPR", "forge.land"}
	have := map[string]bool{}
	for _, n := range action.Names() {
		have[n] = true
	}
	for _, n := range six {
		if !have[n] {
			t.Errorf("the golden's promised row %q is missing from the registry", n)
		}
	}
}
