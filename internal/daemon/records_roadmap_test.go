package daemon

// records_roadmap_test.go — S50 T5 (RM8, rule 4, TD3): the roadmap arms of
// the records service — three at S50, FIVE since S27 T3 (the graph's
// roadmap_depend and roadmap_undepend, written through the same shared
// answers the person's socket doors answer through). What each test pins, in the order the spec
// lists them:
//
//   - the two READ arms answer the SAME bytes the person's socket doors
//     answer, because both run the one rendering protocol.AnswerRoadmap*
//     (rule 4 — roadmap_list twins roadmap.board whole; roadmap_show is
//     compared field for field against show and history, the two doors its
//     one answer composes, so the item and the trail each equal their
//     door's bytes and the pair cannot drift);
//   - unwired, the arms say so by name (V13) instead of answering an empty
//     board that would read as "the roadmap carries nothing" (P3);
//   - the propose arm refuses the unknown root, the identical open twin and
//     the smuggled rung with the seam's own sentence (TD3 — mirrored here
//     exactly as the automation and environment files mirror their seams,
//     because internal/daemon cannot import package main; the parity of the
//     PRODUCTION seam is pinned there, in roadmap_proposal_test.go), and a
//     refused ask stores nothing;
//   - the inert row is stamped with the AGENT the step belongs to (OL3)
//     while the board does not move by one row and the item audit stays
//     exactly as it was (an ask is not a change, and the plane knows no
//     verb for making it otherwise — RM8);
//   - at the 21st ask the counted refusal names 20 and proposal_list, and
//     stores nothing (S45's shared cap carried onto the kind);
//   - a person meets the plane's own standing doors by name.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/roadmap"
	"github.com/cjlapao/aeon/internal/store"
)

// roadmapRecordsEnv is a store-wired daemon over a seeded board —
//
//	/roadmap-ws  component        — the root every ask plans
//	  #health    idea   OPEN      — the duplicate-twin refusal's twin
//	  #retired   cancelled row    — D7: history stays visible on the board
//	/nowhere     (no component)   — the unknown-root refusal's target
//
// The two read arms run over the SAME service instance the person's socket
// doors answer through (wired in initRoadmap, asserted below), and the
// create path runs over an injected validator mirroring cmd/aeon's seam.
// The board is seeded NON-empty — an empty fixture would let "answered
// nothing" pass as "answered".
type roadmapRecordsEnv struct {
	base   recordsEnv // the loop + step every records call stamps from (OL3)
	d      *Daemon
	s      *store.Store
	ctx    context.Context
	root   string
	itemID int64 // the open seeded item's row id (the duplicate twin)
}

func newRoadmapRecordsEnv(t *testing.T) roadmapRecordsEnv {
	t.Helper()
	e := roadmapRecordsEnv{base: newRecordsEnv(t, "")}
	e.s = e.base.s
	e.ctx = e.base.ctx
	ws := t.TempDir()
	e.root = ws
	pc, err := e.s.CreateComponent(store.NewComponent{Name: "roadmap-plane", Slug: "roadmap-plane", Root: ws})
	if err != nil || !pc.OK() {
		t.Fatalf("component: %v", err)
	}
	r := roadmap.New(e.s)
	it, ref := r.CreateAudited(ws, roadmap.NewItem{
		Title: "carry the roadmap health line into the doctor report",
		Epic:  "roadmap honesty", Provenance: roadmap.ProvenanceAuthored,
	}, roadmap.Actor{Kind: "user", ID: "person"})
	if ref != nil {
		t.Fatalf("seed the open item: %s", ref.Reason)
	}
	e.itemID = it.ID
	if _, ref := r.CreateAudited(ws, roadmap.NewItem{
		Title: "an idea the person retired", Provenance: roadmap.ProvenanceAuthored,
	}, roadmap.Actor{Kind: "user", ID: "person"}); ref != nil {
		t.Fatalf("seed the to-be-retired item: %s", ref.Reason)
	}
	db, err := e.s.GormDB().DB()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE roadmap_item SET status = 'cancelled' WHERE title = 'an idea the person retired'`); err != nil {
		t.Fatalf("cancel the retired row: %v", err)
	}
	reg := desk.New(desk.Deps{Boundary: testBoundaryOpener(e.s)}, desk.Seams{
		desk.KindRoadmapIdea: {Validate: mirrorRoadmapIdeaValidate(e.s)},
	})
	e.d = New(Options{Store: e.s, ValidateProposal: reg.ValidateProposal})
	initConn(t, e.d, context.Background(), "c1", "test")
	if e.d.recordsSvc == nil {
		t.Fatal("the daemon built no records service — the arms have nothing to run on")
	}
	if e.d.recordsSvc.roadmapPlane == nil {
		t.Fatal("the daemon did not hand the records service the roadmap plane — the read arms would refuse (V13) on a wired mount")
	}
	if e.d.Table.Roadmap == nil {
		t.Fatal("the socket doors have no roadmap plane either — the byte-for-byte pair would compare nothing")
	}
	return e
}

// mirrorRoadmapIdeaValidate is cmd/aeon's roadmap_idea seam rebuilt from
// pieces these packages own — the desk shape half, then the component read,
// then the open-twin scan — so the arm's verbatim-refusal assertions mean
// what they say about the ARM. The parity of the production seam (create
// and commit calling one function) is pinned in cmd/aeon.
func mirrorRoadmapIdeaValidate(st *store.Store) func(context.Context, []byte) error {
	return func(ctx context.Context, payload []byte) error {
		if err := desk.ValidateRoadmapIdea(ctx, payload); err != nil {
			return err
		}
		var p desk.RoadmapIdeaProposal
		if err := json.Unmarshal(payload, &p); err != nil {
			return err
		}
		if _, cerr := st.ComponentForRoot(p.ComponentRoot); cerr != nil {
			return fmt.Errorf("refused: no component is rooted at %q — roadmap.board shows what roots exist (S50 T5)", p.ComponentRoot)
		}
		items, lerr := roadmap.New(st).List(p.ComponentRoot, roadmap.ListFilter{})
		if lerr != nil {
			return lerr
		}
		for _, it := range items {
			if (it.Status == roadmap.StatusDone || it.Status == roadmap.StatusCancelled) || it.Title != p.Title {
				continue
			}
			return fmt.Errorf("refused: %q is already an open roadmap item under %q — an ask that adds what the board carries changes nothing, and asks that change nothing are refused at create (S50 T5, RM8)", p.Title, p.ComponentRoot)
		}
		return nil
	}
}

func ideaInput(root, title, epic, note string) json.RawMessage {
	m := map[string]string{"componentRoot": root, "title": title, "note": note}
	if epic != "" {
		m["epic"] = epic
	}
	b, _ := json.Marshal(m)
	return b
}

// TestRoadmapToolReadsEqualSocketReads is rule 4 in the direction that
// matters. roadmap_list twins roadmap.board WHOLE — one shared answer, so
// the bytes must match. roadmap_show is the composition case: its item
// field must equal what roadmap.show answers beside, its changes field what
// roadmap.history answers beside, field for field — the tool that saves a
// step a call may not become the place the answer drifts.
func TestRoadmapToolReadsEqualSocketReads(t *testing.T) {
	e := newRoadmapRecordsEnv(t)
	for _, in := range []string{`{}`, fmt.Sprintf(`{"componentRoot":%q}`, e.root), fmt.Sprintf(`{"componentRoot":%q,"status":"idea"}`, e.root), fmt.Sprintf(`{"componentRoot":%q,"epic":"roadmap honesty"}`, e.root)} {
		toolOut, f, err := e.d.recordsSvc.Do(e.ctx, Actor{Kind: "agent", ID: e.base.agent}, "roadmap_list", json.RawMessage(in))
		if f != nil || err != nil {
			t.Fatalf("roadmap_list %s: failure=%v err=%v", in, f, err)
		}
		sockOut, serr := e.d.Table.Dispatch(context.Background(), "c1", protocol.MethodRoadmapBoard, json.RawMessage(in))
		if serr != nil {
			t.Fatalf("roadmap.board %s: %v", in, serr)
		}
		a, _ := json.Marshal(toolOut)
		b, _ := json.Marshal(sockOut)
		if string(a) != string(b) {
			t.Errorf("roadmap_list %s answered\n %s\nand roadmap.board answered\n %s — two renderings of one board (rule 4)", in, a, b)
		}
	}

	in := fmt.Sprintf(`{"id":%d}`, e.itemID)
	fullOut, f, err := e.d.recordsSvc.Do(e.ctx, Actor{Kind: "agent", ID: e.base.agent}, "roadmap_show", json.RawMessage(in))
	if f != nil || err != nil {
		t.Fatalf("roadmap_show: failure=%v err=%v", f, err)
	}
	full, ok := fullOut.(*protocol.RoadmapShowFullResult)
	if !ok {
		t.Fatalf("answered %T, want *protocol.RoadmapShowFullResult", fullOut)
	}
	showOut, serr := e.d.Table.Dispatch(context.Background(), "c1", protocol.MethodRoadmapShow, json.RawMessage(in))
	if serr != nil {
		t.Fatalf("roadmap.show: %v", serr)
	}
	itemBytes, _ := json.Marshal(full.Item)
	var sockShow protocol.RoadmapItemResult
	rawShow, _ := json.Marshal(showOut)
	if err := json.Unmarshal(rawShow, &sockShow); err != nil {
		t.Fatalf("show result: %v (%s)", err, rawShow)
	}
	sockItemBytes, _ := json.Marshal(sockShow.Item)
	if string(itemBytes) != string(sockItemBytes) {
		t.Errorf("roadmap_show's item answered\n %s\nand roadmap.show answered\n %s — the composition drifted from its door", itemBytes, sockItemBytes)
	}
	histOut, herr := e.d.Table.Dispatch(context.Background(), "c1", protocol.MethodRoadmapHistory, json.RawMessage(fmt.Sprintf(`{"id":%d}`, e.itemID)))
	if herr != nil {
		t.Fatalf("roadmap.history: %v", herr)
	}
	trailBytes, _ := json.Marshal(full.Changes)
	var sockHist protocol.RoadmapHistoryResult
	rawHist, _ := json.Marshal(histOut)
	if err := json.Unmarshal(rawHist, &sockHist); err != nil {
		t.Fatalf("history result: %v (%s)", err, rawHist)
	}
	sockTrailBytes, _ := json.Marshal(sockHist.Changes)
	if string(trailBytes) != string(sockTrailBytes) {
		t.Errorf("roadmap_show's trail answered\n %s\nand roadmap.history answered\n %s — the trail is the same append-only rows, byte for byte", trailBytes, sockTrailBytes)
	}
	if len(full.Changes) == 0 {
		t.Error("the seeded item answers with no trail — the 'add' line is the audit (RM2), and an empty fixture proves nothing")
	}
}

// TestRoadmapArmsRefuseAPlanelessMount is the V13 shape carried onto the
// family: a bare service (the unit-test mount, the storeless daemon)
// refuses by name, and never answers an empty board that would read as
// "the roadmap carries nothing".
func TestRoadmapArmsRefuseAPlanelessMount(t *testing.T) {
	e := newRoadmapRecordsEnv(t)
	bare := NewRecordsService(e.s) // no seams assigned at all
	in := fmt.Sprintf(`{"componentRoot":%q}`, e.root)
	for _, tc := range []struct{ tool, input string }{{"roadmap_list", in}, {"roadmap_show", fmt.Sprintf(`{"id":%d}`, e.itemID)}} {
		out, f, err := bare.Do(e.ctx, Actor{Kind: "agent", ID: e.base.agent}, tc.tool, json.RawMessage(tc.input))
		if err != nil {
			t.Fatalf("%s: %v", tc.tool, err)
		}
		if out != nil {
			t.Errorf("%s answered %v over a nil plane — absence must be stated, not answered", tc.tool, out)
		}
		if f == nil || !strings.Contains(f.Sentence, tc.tool) || !strings.Contains(f.Sentence, "V13") {
			t.Errorf("%s's refusal is %v, want one naming the tool and V13", tc.tool, f)
		}
	}
}

// TestProposeIdeaRefusedAtCreateUnknownRootTwinOrRung is TD3 and RM8 at the
// create door: the unknown root, the identical OPEN twin, and a smuggled
// rung each meet the sentence VERBATIM, and nothing is stored — an inert
// row that decides nothing is still a row, and an ask that adds what the
// board carries decides nothing at all. The CANCELLED twin is NOT refused:
// a retired idea re-proposed is a live question again (D7: terminal rows
// are history, not a lock).
func TestProposeIdeaRefusedAtCreateUnknownRootTwinOrRung(t *testing.T) {
	e := newRoadmapRecordsEnv(t)
	cases := []struct{ input, want string }{
		{`{"componentRoot":"/nowhere","title":"a fresh idea","note":"why it matters"}`, `no component is rooted at "/nowhere"`},
		{fmt.Sprintf(`{"componentRoot":%q,"title":"carry the roadmap health line into the doctor report","note":"someone else already asked"}`, e.root), "is already an open roadmap item"},
		{fmt.Sprintf(`{"componentRoot":%q,"title":"a fresh idea","note":"why","status":"ready"}`, e.root), `unknown field "status"`},
		{fmt.Sprintf(`{"componentRoot":%q,"title":"a fresh idea","note":"why","actor":"u1"}`, e.root), `unknown field "actor"`},
		{fmt.Sprintf(`{"componentRoot":%q,"title":"a fresh idea"}`, e.root), "needs a note"},
	}
	for _, tc := range cases {
		_, f, err := e.d.recordsSvc.Do(e.ctx, Actor{Kind: "agent", ID: e.base.agent}, "roadmap_propose_idea", json.RawMessage(tc.input))
		if err != nil {
			t.Fatalf("%s: %v", tc.input, err)
		}
		if f == nil || !strings.Contains(f.Sentence, tc.want) {
			t.Errorf("%s: refusal=%v, want it to carry %q verbatim", tc.input, f, tc.want)
		}
	}
	if n := countProposals(t, e.s, string(desk.KindRoadmapIdea)); n != 0 {
		t.Errorf("%d rows stored after five refusals — what create refuses never becomes a row (TD3)", n)
	}
	items, err := roadmap.New(e.s).List(e.root, roadmap.ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Errorf("the board holds %d rows after five refusals, want the seeded 2 — an ask touches the board never and nowhere", len(items))
	}
}

// TestProposeIdeaCreatesInertProposalByTheAgentActor is RM8 whole: the row,
// the author, the inertness. The ask is the model's (created_by_* carries
// the STEP, never a typed actor — OL3); the board does not move by one row;
// the item audit says nothing happened either (the ask added no
// roadmap_change row — there is no row to audit and no verb that would);
// and the answer says what the row IS (the inert note) and WHO may land it
// (the command). A re-proposed retired idea STANDS — history is not a lock.
func TestProposeIdeaCreatesInertProposalByTheAgentActor(t *testing.T) {
	e := newRoadmapRecordsEnv(t)
	for _, tc := range []struct{ title, note string }{
		{"carry the board filters into the CLI listing", "a step that only sees JSON still wants the epic column; the person owns the tag"},
		{"an idea the person retired", "the retirement was last spring; the cost fell and the question is live again (D7: history, not a lock)"},
	} {
		out, f, err := e.d.recordsSvc.Do(e.ctx, Actor{Kind: "agent", ID: e.base.agent}, "roadmap_propose_idea",
			ideaInput(e.root, tc.title, "", tc.note))
		if f != nil || err != nil {
			t.Fatalf("the ask %q should stand: failure=%v err=%v", tc.title, f, err)
		}
		res, ok := out.(protocol.PolicyProposeResult)
		if !ok {
			t.Fatalf("answered %T, want protocol.PolicyProposeResult", out)
		}
		if res.Note != protocol.PolicyProposeInertNote {
			t.Errorf("the note is %q, not the one inertness sentence the conformance fixture copies", res.Note)
		}
		if res.Proposal.Command == "" {
			t.Error("no decision command on the answer — the person's door would not be named to the agent")
		}
	}
	row, found := store.Proposal{}, false
	rows, err := e.s.ListProposals("proposed")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Kind == string(desk.KindRoadmapIdea) {
			row, found = r, true
		}
	}
	if !found {
		t.Fatal("no roadmap_idea row was stored")
	}
	if row.CreatedByKind != "agent" || row.CreatedByID != e.base.agent {
		t.Errorf("authored by %s/%s, want agent/%s — authorship is the step's, never typed (OL3)", row.CreatedByKind, row.CreatedByID, e.base.agent)
	}
	var payload desk.RoadmapIdeaProposal
	if err := json.Unmarshal([]byte(row.Payload), &payload); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if payload.ComponentRoot != e.root || payload.Title == "" || payload.Note == "" {
		t.Errorf("the payload lost the ask: %+v", payload)
	}
	if strings.Contains(row.Payload, "priorityState") || strings.Contains(row.Payload, `"estimate"`) || strings.Contains(row.Payload, "provenance") {
		t.Errorf("the payload carries a rung or a size — an idea arrived decided (RM8): %s", row.Payload)
	}
	// The board itself did not move: the same two seeded rows, no more.
	items, err := roadmap.New(e.s).List(e.root, roadmap.ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Errorf("the board holds %d rows after two asks, want the seeded 2 — an ask files a row on the DESK, not on the roadmap (TD2, RM8)", len(items))
	}
}

// TestRoadmapProposalLimit is S45's shared cap carried onto the new kind:
// at the limit the ask meets a refusal that NAMES the constant and the read
// that lists what is open, and stores nothing.
func TestRoadmapProposalLimit(t *testing.T) {
	e := newRoadmapRecordsEnv(t)
	for i := 0; i < desk.MaxOpenProposalsPerAgent; i++ {
		if _, f, err := e.d.recordsSvc.Do(e.ctx, Actor{Kind: "agent", ID: e.base.agent}, "roadmap_propose_idea",
			ideaInput(e.root, fmt.Sprintf("cap filler idea %d", i), "", fmt.Sprintf("filler note %d", i))); f != nil || err != nil {
			t.Fatalf("filler %d: failure=%v err=%v", i, f, err)
		}
	}
	_, f, err := e.d.recordsSvc.Do(e.ctx, Actor{Kind: "agent", ID: e.base.agent}, "roadmap_propose_idea",
		ideaInput(e.root, "one too many", "", "the 21st ask"))
	if err != nil {
		t.Fatal(err)
	}
	if f == nil {
		t.Fatalf("the %dth proposal stood — the cap is not applied to this kind (S45's shared rule)", desk.MaxOpenProposalsPerAgent+1)
	}
	if !strings.Contains(f.Sentence, fmt.Sprint(desk.MaxOpenProposalsPerAgent)) || !strings.Contains(f.Sentence, "proposal_list") {
		t.Errorf("the cap refusal does not name the number or the read: %s", f.Sentence)
	}
	if n := countProposals(t, e.s, string(desk.KindRoadmapIdea)); n != desk.MaxOpenProposalsPerAgent {
		t.Errorf("%d rows stored, want the cap's %d — a counted refusal that stores is not counted", n, desk.MaxOpenProposalsPerAgent)
	}
}

// TestRoadmapToolDoorsRefuseAPerson is the shape carried onto the family:
// these three doors answer the step, and a person is pointed at the plane's
// own standing doors by name — the two reads because the socket already
// speaks them unscoped, the ask because an idea LANDS through roadmap.add,
// the person's door, and the kind rides proposal.create like every other.
func TestRoadmapToolDoorsRefuseAPerson(t *testing.T) {
	e := newRoadmapRecordsEnv(t)
	// S27 T3: the graph's two arms carry the same shape — an edge is a
	// person's own act; the agent may read the graph, never draw it.
	for _, tool := range []string{"roadmap_list", "roadmap_show", "roadmap_propose_idea", "roadmap_depend", "roadmap_undepend"} {
		in := json.RawMessage(`{}`)
		if tool == "roadmap_show" {
			in = json.RawMessage(fmt.Sprintf(`{"id":%d}`, e.itemID))
		}
		_, f, err := e.d.recordsSvc.Do(e.ctx, Actor{Kind: "user", ID: "person"}, tool, in)
		if err != nil {
			t.Fatalf("%s: %v", tool, err)
		}
		if f == nil || !strings.Contains(f.Sentence, "person") {
			t.Errorf("%s served a person the agent's door (refusal=%v) — a person holds the unscoped doors", tool, f)
		}
	}
	_, f, err := e.d.recordsSvc.Do(e.ctx, Actor{Kind: "user", ID: "person"}, "roadmap_propose_idea", ideaInput(e.root, "a person's own idea", "", "they can add it themselves"))
	if err != nil {
		t.Fatal(err)
	}
	if f == nil || !strings.Contains(f.Sentence, "roadmap.add") || !strings.Contains(f.Sentence, "roadmap_idea") {
		t.Errorf("the person's refusal should name roadmap.add AND proposal.create's kind, got %v", f)
	}
}

// TestDependToolParity (S27 T3, rule 4): the roadmap_depend /
// roadmap_undepend arms and the person's socket doors run the SAME
// exported answers (protocol.AnswerRoadmapDepend / AnswerRoadmapUndepend),
// so on one store with one graph the two doors' JSON answers are byte
// equal. What intentionally differs is the STAMP — the tool arm writes the
// edge under the step's agent slug {agent, implementer-bot} (OL3: identity
// is the stamped step, never a param), the socket door under the
// connection's person {user, uid} — rule 4 is about the ANSWER, and the
// audit is the DOOR's own, which is the point of stamping it.
func TestDependToolParity(t *testing.T) {
	e := newRoadmapRecordsEnv(t)
	seedAgent(t, e.base, "implementer", "implementer-bot")
	r := roadmap.New(e.s)
	up, ref := r.CreateAudited(e.root, roadmap.NewItem{Title: "the ledger's totals test", Provenance: roadmap.ProvenanceAuthored}, roadmap.Actor{Kind: "user", ID: "person"})
	if ref != nil {
		t.Fatalf("seed the upstream item: %s", ref.Reason)
	}
	in := json.RawMessage(fmt.Sprintf(`{"id":%d,"dependsOn":%d}`, e.itemID, up.ID))

	toolOut, f, err := e.d.recordsSvc.Do(e.ctx, Actor{Kind: "agent", ID: "implementer"}, "roadmap_depend", in)
	if err != nil || f != nil {
		t.Fatalf("roadmap_depend: f=%v err=%v", f, err)
	}
	db, err := e.s.GormDB().DB()
	if err != nil {
		t.Fatal(err)
	}
	var kind, who string
	if err := db.QueryRow(`SELECT created_by_kind, created_by_id FROM roadmap_dependency WHERE item_id = ? AND depends_on_id = ?`, e.itemID, up.ID).Scan(&kind, &who); err != nil {
		t.Fatalf("the edge row: %v", err)
	}
	if kind != "agent" || who != "implementer-bot" {
		t.Errorf("the edge was stamped {%s, %s}, want {agent, implementer-bot} — the step's slug, resolved, never guessed (OL3)", kind, who)
	}
	// The answer itself carries the graph beside the item.
	b, err := json.Marshal(toolOut)
	if err != nil || !strings.Contains(string(b), `"dependsOn"`) == false && !strings.Contains(string(b), `"dependencies"`) {
		t.Fatalf("the depend answer does not carry the graph: %s (%v)", b, err)
	}

	// Undo through the tool — and prove the graph is empty again BEFORE the
	// door redraws it, so the two answers start from the identical state.
	if _, f, err := e.d.recordsSvc.Do(e.ctx, Actor{Kind: "agent", ID: "implementer"}, "roadmap_undepend", in); err != nil || f != nil {
		t.Fatalf("roadmap_undepend: f=%v err=%v", f, err)
	}
	if after, err := r.Dependencies(e.itemID); err != nil || len(after) != 0 {
		t.Fatalf("the tool undepend left the graph: %+v (%v)", after, err)
	}
	e.d.Table.ActorFor = func(context.Context, *protocol.Session) protocol.Actor {
		return protocol.Actor{Kind: "user", ID: "person"} // the stamped peer, the seam's shape
	}
	sockOut, serr := e.d.Table.Dispatch(context.Background(), "c1", protocol.MethodRoadmapDepend, in)
	if serr != nil {
		t.Fatalf("the door: %v", serr)
	}
	sb, err := json.Marshal(sockOut)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != string(sb) {
		t.Errorf("the two doors drifted:\n tool %s\n door %s", b, sb)
	}
}

// TestRoadmapGraphArmsRefuseAPersonByName is the refusal's other half: the
// edge arms must NAME the door a person should speak, not just "no".
func TestRoadmapGraphArmsRefuseAPersonByName(t *testing.T) {
	e := newRoadmapRecordsEnv(t)
	in := json.RawMessage(`{"id":1,"dependsOn":2}`)
	for tool, want := range map[string]string{
		"roadmap_depend":   "roadmap.depend",
		"roadmap_undepend": "roadmap.undepend",
	} {
		_, f, err := e.d.recordsSvc.Do(e.ctx, Actor{Kind: "user", ID: "person"}, tool, in)
		if err != nil || f == nil {
			t.Fatalf("%s served a person (f=%v err=%v)", tool, f, err)
		}
		if !strings.Contains(f.Sentence, want) || !strings.Contains(f.Sentence, "person's own act") {
			t.Errorf("%s refusal does not name %s as the door: %s", tool, want, f.Sentence)
		}
	}
}

// TestRoadmapGraphArmsArePlanelessHonest is V13 carried onto the arms:
// without the roadmap plane the refusal names the TOOL, never answers an
// empty success an unwired mount cannot produce.
func TestRoadmapGraphArmsArePlanelessHonest(t *testing.T) {
	e := newRecordsEnv(t, "") // never wired — the V13 side
	for _, tool := range []string{"roadmap_depend", "roadmap_undepend"} {
		_, f, err := e.svc.Do(e.ctx, Actor{Kind: "agent", ID: "implementer"}, tool, json.RawMessage(`{"id":1,"dependsOn":2}`))
		if err != nil || f == nil {
			t.Fatalf("%s answered without a plane (f=%v err=%v)", tool, f, err)
		}
		if !strings.Contains(f.Sentence, tool) || !strings.Contains(f.Sentence, "V13") {
			t.Errorf("%s planeless refusal does not name itself: %s", tool, f.Sentence)
		}
	}
}
