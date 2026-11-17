package main

// roadmap_proposal_test.go — S50 T5: the roadmap_idea kind, an ask at rest
// and a person's act at commit. Every assertion is about rows: the create
// door REFUSES the unknown root, the identical open twin and every widening
// shape before a byte is stored (TD3's nothing-inert carried; an ask that
// adds what the board carries is a nothing-ask, RM8), the two doors run ONE
// function, the commit ADDS the item through roadmap's own audited create
// with the COMMITTER as actor — provenance authored, one `add` change row
// in the same transaction (the store's frozen "added item %d" sentence is
// the fingerprint a paraphrase cannot forge) — the crash-retry COMPLETES
// the add without a second row (TD4), no payload can rank, size, state or
// sign, a proposal at rest is inert, and the desk half keeps its imports.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/roadmap"
	"github.com/cjlapao/aeon/internal/store"
)

type roadmapPropEnv struct {
	t    *testing.T
	st   *store.Store
	d    *daemon.Daemon
	root string
}

func newRoadmapPropEnv(t *testing.T) roadmapPropEnv {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	st := openAgentStore(t)
	d := daemon.New(daemon.Options{
		Store:            st,
		ValidateProposal: validateProposalPayload,
		Effects:          func() (*desk.Registry, error) { return deskEffects, nil },
	})
	ws := t.TempDir()
	pc, err := st.CreateComponent(store.NewComponent{Name: "idea-plane", Slug: "idea-plane", Root: ws})
	if err != nil || !pc.OK() {
		t.Fatalf("component: %v", err)
	}
	return roadmapPropEnv{t: t, st: st, d: d, root: ws}
}

func (e roadmapPropEnv) initialize(conn string) {
	e.t.Helper()
	raw, _ := json.Marshal(protocol.InitializeParams{ClientName: "idea", ClientVersion: "0", ProtocolVersion: protocol.PROTOCOL_VERSION})
	if _, err := e.d.Table.Dispatch(context.Background(), conn, protocol.MethodInitialize, raw); err != nil {
		e.t.Fatal(err)
	}
}

func roadmapIdeaPayload(t *testing.T, fields map[string]any) string {
	t.Helper()
	b, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestIdeaProposalCreateRefusesUnknownRootOrTwin: the create door refuses
// every ask the board makes meaningless — a root that is no component, the
// identical OPEN item already standing, the shape that smuggles a rung —
// and it refuses naming what is true: the root through roadmap.board's
// listing, the twin through the title and the root, the rung through the
// field by name. The RETIRED twin is NOT refused (D7: terminal rows are
// history, not a lock). Nothing inert reaches a commit.
func TestIdeaProposalCreateRefusesUnknownRootOrTwin(t *testing.T) {
	e := newRoadmapPropEnv(t)
	e.initialize("c-refuse")
	e.seedOpenIdea("carry the roadmap health line into the doctor report")
	e.seedRetiredIdea("an idea the person retired")
	before := e.proposalCount()
	cases := []struct {
		name   string
		fields map[string]any
		wants  []string
	}{
		{"unknown root", map[string]any{"componentRoot": "/nowhere-at-all", "title": "a fresh idea", "note": "why it matters"}, []string{`no component is rooted at "/nowhere-at-all"`, "roadmap.board"}},
		{"blank root", map[string]any{"componentRoot": "   ", "title": "a fresh idea", "note": "why"}, []string{"names the project root"}},
		{"open twin", map[string]any{"componentRoot": e.root, "title": "carry the roadmap health line into the doctor report", "note": "again"}, []string{"is already an open roadmap item", "refused at create"}},
		{"smuggled status", map[string]any{"componentRoot": e.root, "title": "a fresh idea", "note": "why", "status": "ready"}, []string{`unknown field "status"`}},
		{"smuggled priorityState", map[string]any{"componentRoot": e.root, "title": "a fresh idea", "note": "why", "priorityState": "essential"}, []string{`unknown field "priorityState"`}},
		{"smuggled estimate", map[string]any{"componentRoot": e.root, "title": "a fresh idea", "note": "why", "estimate": "small"}, []string{"unknown field"}},
		{"smuggled provenance", map[string]any{"componentRoot": e.root, "title": "a fresh idea", "note": "why", "provenance": "imported"}, []string{"unknown field"}},
		{"smuggled actor", map[string]any{"componentRoot": e.root, "title": "a fresh idea", "note": "why", "actor": "u9"}, []string{"unknown field"}},
		{"no note", map[string]any{"componentRoot": e.root, "title": "a fresh idea"}, []string{"needs a note"}},
		{"blank title", map[string]any{"componentRoot": e.root, "title": "  ", "note": "why"}, []string{"needs a title"}},
	}
	for _, c := range cases {
		if _, err := e.createViaSocket("c-refuse", roadmapIdeaPayload(t, c.fields)); err == nil {
			t.Errorf("%s: the create door stored a nothing-ask", c.name)
			continue
		} else if c.wants != nil {
			msg := err.Error()
			var perr *protocol.Error
			if errors.As(err, &perr) {
				msg = perr.Data.What
			}
			for _, w := range c.wants {
				if !strings.Contains(msg, w) {
					t.Errorf("%s: refusal %q does not name %q", c.name, msg, w)
				}
			}
		}
	}
	// A re-proposed RETIRED idea stands: history is not a lock (D7).
	if _, err := e.createViaSocket("c-refuse", roadmapIdeaPayload(t, map[string]any{"componentRoot": e.root, "title": "an idea the person retired", "note": "the cost fell; the question is live again"})); err != nil {
		t.Errorf("the re-proposed retired idea was refused: %v", err)
	}
	// And one honest ask stands, inert at rest.
	row, err := e.createViaSocket("c-refuse", roadmapIdeaPayload(t, map[string]any{"componentRoot": e.root, "title": "a fresh idea", "note": "the board owes a row and only a person gives it"}))
	if err != nil {
		t.Fatalf("an honest ask should stand at the door: %v", err)
	}
	if row.Status != "proposed" {
		t.Fatalf("staged status = %q, want proposed", row.Status)
	}
	// The refusals stored nothing; the two standing asks stored exactly
	// themselves.
	if n := e.proposalCount(); n != before+2 {
		t.Fatalf("the door stored %d rows, want the two honest asks (refusals must not write)", n-before)
	}
	if items := e.boardItems(); len(items) != 2 {
		t.Fatalf("the board holds %d rows after the asks, want the seeded 2 — an ask files on the DESK, never the roadmap (TD2)", len(items))
	}
}

// TestRoadmapIdeaCreateAndCommitValidatorsAreOneFunction is TD3 for the new
// kind: the create door's refusal and the validator carry the SAME bytes,
// because one function stands behind both doors — and the identical-twin
// refusal is that function's store half, so the commit door re-runs it
// verbatim (an idea added by hand between the ask and the commit makes the
// COMMIT refuse, the same words create used).
func TestRoadmapIdeaCreateAndCommitValidatorsAreOneFunction(t *testing.T) {
	e := newRoadmapPropEnv(t)
	e.initialize("c-one")
	bad := roadmapIdeaPayload(t, map[string]any{"componentRoot": "/nowhere-at-all", "title": "a fresh idea", "note": "why"})
	_, doorErr := e.createViaSocket("c-one", bad)
	if doorErr == nil {
		t.Fatal("the create door accepted an ask about a root that is no component")
	}
	direct := validateRoadmapIdeaProposal([]byte(bad))
	if direct == nil {
		t.Fatal("the validator accepted what the door refused")
	}
	var perr *protocol.Error
	if !errors.As(doorErr, &perr) {
		t.Fatalf("the door did not answer the refused shape: %v", doorErr)
	}
	if perr.Data.What != direct.Error() {
		t.Fatalf("the doors drifted:\n door:  %s\n valid: %s", perr.Data.What, direct)
	}
	// The open twin is the door's other shared half: one sentence both ways.
	twin := roadmapIdeaPayload(t, map[string]any{"componentRoot": e.root, "title": "hand-added before the ask landed", "note": "why"})
	if ref := e.addByHand("hand-added before the ask landed"); ref != nil {
		t.Fatal(ref.Reason)
	}
	twinDirect := validateRoadmapIdeaProposal([]byte(twin))
	if twinDirect == nil || !strings.Contains(twinDirect.Error(), "already an open roadmap item") {
		t.Fatalf("the validator's twin refusal is %v", twinDirect)
	}
	if _, err := e.createViaSocket("c-one", twin); err == nil {
		t.Fatal("the create door stored an ask the validator refuses")
	}
}

// TestIdeaCommitAddsAsTheCommitterAndAudits: the committed ask ADDS the
// item with the COMMITTER as actor — the proposer stays in the proposal's
// created_by_* — provenance AUTHORED (RM8: the person's decision, whatever
// asked), and AN4's sibling counts the ONE roadmap_change row the create
// wrote in the same transaction: op add, actor user/u-committ, the store's
// own frozen "added item %d" summary — a second write service could not
// produce those words.
func TestIdeaCommitAddsAsTheCommitterAndAudits(t *testing.T) {
	e := newRoadmapPropEnv(t)
	e.initialize("c-commit")
	payload := roadmapIdeaPayload(t, map[string]any{"componentRoot": e.root, "title": "carry the board filters into the CLI listing", "epic": "roadmap honesty", "note": "the board answers them; the CLI does not read them"})
	row, err := e.createViaSocket("c-commit", payload)
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.commit(row.ID, payload, "u-committ")
	if err != nil {
		t.Fatalf("commit of an honest ask: %v", err)
	}
	if res.Row.Status != "committed" {
		t.Fatalf("status = %q, want committed", res.Row.Status)
	}
	items := e.boardItems()
	var it roadmap.Item
	for _, x := range items {
		if x.Title == "carry the board filters into the CLI listing" {
			it = x
		}
	}
	if it.ID == 0 {
		t.Fatal("the committed add did not land an item on the board")
	}
	if it.Status != roadmap.StatusIdea {
		t.Errorf("the new item reads status %q, want idea — an idea enters life at the bottom rung, never where the ask pointed (RM8)", it.Status)
	}
	if it.Provenance != roadmap.ProvenanceAuthored {
		t.Errorf("provenance = %q, want authored — the COMMIT is authored, whatever asked", it.Provenance)
	}
	if it.Epic != "roadmap honesty" {
		t.Errorf("the epic tag was lost: %q", it.Epic)
	}
	if it.Priority != roadmap.PriorityUnknown {
		t.Errorf("the new item arrived ranked (%v), want unknown — the ask carried no rank and the create grants none (RM8)", it.Priority)
	}
	// AN4 counts the ONE change this commit made: exactly one row for the
	// new item, op add, the COMMITTER as actor, the store's frozen summary.
	changes, err := e.st.RoadmapChangeList(context.Background(), it.ID, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 {
		t.Fatalf("the add left %d audit rows, want exactly the ONE the create wrote (RM2)", len(changes))
	}
	c := changes[0]
	if c.Op != "add" {
		t.Errorf("audit op = %q, want add", c.Op)
	}
	if c.ActorKind != "user" || c.ActorID != "u-committ" {
		t.Errorf("audit actor = %s/%s — the COMMITTER acts, the proposer stays on the ask", c.ActorKind, c.ActorID)
	}
	if !strings.Contains(c.Summary, fmt.Sprintf("added item %d", it.ID)) {
		t.Errorf("audit summary = %q — the store's own frozen sentence must ride (a paraphrase cannot forge it)", c.Summary)
	}
}

// TestIdeaAlreadyAppliedCompletesAdd: the item landed (the person added it
// by hand, or a crash lost the row between write and decision) and the
// retry COMPLETES: the proposal flips to committed, AlreadyApplied answers
// the standing twin, and NO second item and NO second audit row appear —
// AN4 counts changes, not touches. This is also why the twin refusal sits
// in ALREADY-APPLIED's reach and not only in validate: decide runs the
// already-landed check BEFORE the validator, so a crash-retry completes
// instead of trapping the ask behind a refusal create already made.
func TestIdeaAlreadyAppliedCompletesAdd(t *testing.T) {
	e := newRoadmapPropEnv(t)
	e.initialize("c-retry")
	payload := roadmapIdeaPayload(t, map[string]any{"componentRoot": e.root, "title": "a board the CLI can read", "note": "operator habit: the terminal first"})
	row, err := e.createViaSocket("c-retry", payload)
	if err != nil {
		t.Fatal(err)
	}
	// The crash window: the add lands through the same door, the decision
	// is lost.
	if ref := e.addByHand("a board the CLI can read"); ref != nil {
		t.Fatal(ref.Reason)
	}
	auditBefore := e.boardItems()
	res, err := e.commit(row.ID, payload, "u-committ")
	if err != nil {
		t.Fatalf("the retry must COMPLETE, not replay: %v", err)
	}
	if res.Row.Status != "committed" {
		t.Fatalf("status = %q — a landed effect completes the flip", res.Row.Status)
	}
	after := e.boardItems()
	n := 0
	for _, x := range after {
		if x.Title == "a board the CLI can read" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("the retry left %d items with the same title, want the ONE already added — AlreadyApplied failed its job", n)
	}
	if len(after) != len(auditBefore) {
		t.Fatalf("the board grew %d rows on a retry of a landed effect", len(after)-len(auditBefore))
	}
	var it roadmap.Item
	for _, x := range after {
		if x.Title == "a board the CLI can read" {
			it = x
		}
	}
	changes, err := e.st.RoadmapChangeList(context.Background(), it.ID, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].ActorID != "person" {
		t.Fatalf("the retry re-audited a change already made: %+v — the hand-add's row must be the only one", changes)
	}
}

// TestRoadmapIdeaApplyRefusesADoorWithNoCommitter: the fail-closed pair.
// Apply takes the committing person and the proposal id from the COMMIT
// context — from the door, never the payload — and an Apply reached without
// one half of that pair refuses and writes nothing, rather than defaulting
// an actor (an item nobody committed is exactly the row RM4 keeps out).
func TestRoadmapIdeaApplyRefusesADoorWithNoCommitter(t *testing.T) {
	e := newRoadmapPropEnv(t)
	eff, ok := deskEffects.Effect(string(desk.KindRoadmapIdea))
	if !ok {
		t.Fatal("roadmap_idea is not a complete effect in the production registry")
	}
	var out strings.Builder
	if err := eff.Apply(context.Background(), &out, []byte(roadmapIdeaPayload(t, map[string]any{"componentRoot": e.root, "title": "a title", "note": "a note"}))); err == nil {
		t.Fatal("Apply ran with NO decider and NO proposal id — the fail-closed pair is a comment, not a check")
	}
	if items := e.boardItems(); len(items) != 0 {
		t.Fatalf("the no-committer Apply wrote %d board rows, want none", len(items))
	}
}

// TestNoPayloadCanRank: the payload's grammar is WHICH IDEA and NOTE and
// nothing else. Every rung, size, source and signature — smuggled, typed
// wrong, blank, oversized, or trailing — is a refusal at the desk, before
// any store opens, and none of them stores a row.
func TestNoPayloadCanRank(t *testing.T) {
	e := newRoadmapPropEnv(t)
	e.initialize("c-rank")
	before := e.proposalCount()
	widening := []string{
		`{"componentRoot":"` + e.root + `","title":"t","note":"n","status":"ready"}`,
		`{"componentRoot":"` + e.root + `","title":"t","note":"n","priorityState":"essential"}`,
		`{"componentRoot":"` + e.root + `","title":"t","note":"n","estimate":{"min":1,"max":3,"basis":"S"},"epic":""}`,
		`{"componentRoot":"` + e.root + `","title":"t","note":"n","provenance":"imported"}`,
		`{"componentRoot":"` + e.root + `","title":"t","note":"n","actor":{"kind":"user","id":"u9"}}`,
		`{"componentRoot":"` + e.root + `","title":"t","note":"n","kind":"roadmap_update"}`,
		`{"componentRoot":"` + e.root + `","title":"t"}`,
		`{"componentRoot":"` + e.root + `","title":"t","note":""}`,
		`{"componentRoot":"` + e.root + `","title":7,"note":"n"}`,
		`{"componentRoot":"` + e.root + `","title":["t"],"note":"n"}`,
		`{"componentRoot":` + e.root + `,"title":"t","note":"n"}`,
		`{"componentRoot":"` + e.root + `","title":"t","note":"n"} {"componentRoot":"` + e.root + `","title":"t2","note":"n2"}`,
		`{"componentRoot":"` + e.root + `","title":"` + strings.Repeat("x", 301) + `","note":"n"}`,
		`{"componentRoot":"` + e.root + `","title":"t","note":"` + strings.Repeat("x", 1025) + `"}`,
		`{"componentRoot":"` + e.root + `","title":"t","note":"n","epic":"` + strings.Repeat("e", 201) + `"}`,
		`{"componentRoot":"` + strings.Repeat("r", 513) + `","title":"t","note":"n"}`,
	}
	for _, p := range widening {
		if err := validateRoadmapIdeaProposal([]byte(p)); err == nil {
			t.Errorf("the validator accepted a widening: %s", p)
		}
		if _, err := e.createViaSocket("c-rank", p); err == nil {
			t.Errorf("the create door stored a widening: %s", p)
		}
	}
	if n := e.proposalCount(); n != before {
		t.Fatalf("the widening attempts stored %d rows — none may exist", n-before)
	}
	if items := e.boardItems(); len(items) != 0 {
		t.Fatalf("the widening attempts touched the board (%d rows)", len(items))
	}
}

// TestRoadmapIdeaProposalsAreInert: an ask at rest changes NOTHING. The
// board and every item's trail answer byte-identically with proposals
// staged (and one rejected) in every state — the S35 pattern carried to
// the roadmap plane (RM8's asks add no row until a person commits).
func TestRoadmapIdeaProposalsAreInert(t *testing.T) {
	e := newRoadmapPropEnv(t)
	e.initialize("c-inert")
	e.seedOpenIdea("the seeded open item")
	snapshot := func() string {
		items, err := roadmap.New(e.st).List(e.root, roadmap.ListFilter{})
		if err != nil {
			t.Fatal(err)
		}
		b, err := json.Marshal(items)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	before := snapshot()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	agent := store.Actor{Kind: "agent", ID: "manny"}
	staged, err := e.st.CreateProposal("roadmap_idea", roadmapIdeaPayload(t, map[string]any{"componentRoot": e.root, "title": "a staged ask", "note": "quiet"}), agent, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.CreateProposal("roadmap_idea", roadmapIdeaPayload(t, map[string]any{"componentRoot": e.root, "title": "another staged ask", "note": "quiet"}), agent, now); err != nil {
		t.Fatal(err)
	}
	if err := e.st.DecideProposal(staged.ID, "rejected", store.Actor{Kind: "user", ID: "u1"}, "not now", now); err != nil {
		t.Fatal(err)
	}
	after := snapshot()
	if before != after {
		t.Fatalf("proposals changed the board:\n before %s\n after  %s", before, after)
	}
}

// TestRoadmapDeskKindImportsNoStoreOrDaemon: the desk half of the kind —
// its payload grammar — lives in internal/desk and imports NOTHING of the
// readers or the engines: no internal/store, no internal/daemon, no
// internal/protocol, no internal/roadmap, no aeon package at all. stdlib
// only. The package-wide import guard (imports_test.go) covers the tree;
// this pins the S50 file by name so the seam direction is stated where the
// kind is.
func TestRoadmapDeskKindImportsNoStoreOrDaemon(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "internal", "desk", "roadmap_kinds.go"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	start := strings.Index(body, "import (")
	if start < 0 {
		t.Fatal("no import block found")
	}
	block := body[start:]
	if end := strings.Index(block, "\n)"); end > 0 {
		block = block[:end]
	}
	for _, banned := range []string{"internal/store", "internal/daemon", "internal/protocol", "internal/roadmap", "github.com/cjlapao/aeon/"} {
		if strings.Contains(block, banned) {
			t.Errorf("the desk half of roadmap_idea imports %s — the store and the roadmap plane reach the kind only through the seams", banned)
		}
	}
}

// — helpers —

func (e roadmapPropEnv) createViaSocket(conn, payload string) (protocol.ProposalRow, error) {
	e.t.Helper()
	raw, _ := json.Marshal(map[string]any{"kind": "roadmap_idea", "payload": json.RawMessage(payload)})
	res, err := e.d.Table.Dispatch(context.Background(), conn, protocol.MethodProposalCreate, raw)
	if err != nil {
		return protocol.ProposalRow{}, err
	}
	return res.(protocol.ProposalRow), nil
}

func (e roadmapPropEnv) commit(id int64, payload, byID string) (desk.CommitResult, error) {
	return desk.Commit(context.Background(), cliDecideDeps(e.st), deskEffects, id,
		desk.ProposalPayloadDigest(payload), desk.Actor{Kind: "user", ID: byID})
}

func (e roadmapPropEnv) boardItems() []roadmap.Item {
	e.t.Helper()
	items, err := roadmap.New(e.st).List(e.root, roadmap.ListFilter{})
	if err != nil {
		e.t.Fatal(err)
	}
	return items
}

func (e roadmapPropEnv) proposalCount() int {
	e.t.Helper()
	var n int
	if err := e.st.GormDB().Raw(`SELECT COUNT(*) FROM proposal`).Scan(&n).Error; err != nil {
		e.t.Fatal(err)
	}
	return n
}

func (e roadmapPropEnv) seedOpenIdea(title string) {
	e.t.Helper()
	if ref := e.addByHandVia(title, roadmap.ProvenanceAuthored); ref != nil {
		e.t.Fatal(ref.Reason)
	}
}

func (e roadmapPropEnv) seedRetiredIdea(title string) {
	e.t.Helper()
	it, ref := roadmap.New(e.st).CreateAudited(e.root, roadmap.NewItem{
		Title: title, Provenance: roadmap.ProvenanceAuthored,
	}, roadmap.Actor{Kind: "user", ID: "person"})
	if ref != nil {
		e.t.Fatal(ref.Reason)
	}
	if _, ref := roadmap.New(e.st).CancelAudited(it.ID, roadmap.Actor{Kind: "user", ID: "person"}); ref != nil {
		e.t.Fatal(ref.Reason)
	}
}

func (e roadmapPropEnv) addByHand(title string) *roadmap.Refusal {
	e.t.Helper()
	return e.addByHandVia(title, roadmap.ProvenanceAuthored)
}

func (e roadmapPropEnv) addByHandVia(title string, prov roadmap.Provenance) *roadmap.Refusal {
	e.t.Helper()
	_, ref := roadmap.New(e.st).CreateAudited(e.root, roadmap.NewItem{
		Title: title, Provenance: prov,
	}, roadmap.Actor{Kind: "user", ID: "person"})
	return ref
}
