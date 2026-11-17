package main

// S50 T6 — the parity record between the two doors and the gates the
// dispatch verbs hold. The fixture is T0 §7's table, spelled here as the
// law it was written to be: nine verbs ride the socket alone (S26's
// roads, unchanged), five verbs ride BOTH doors (the gaps S50 closes),
// three verbs are dispatches — socket-only, statement first, --yes or
// nothing. Guards: the direct door copies no logic (every method is one
// call into the daemon's own door constructors), the offline stamp is
// {cli, cli}, the selection refuses rather than routing around a dial
// that merely failed, and a dispatch without --yes writes nothing.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/roadmap"
	"github.com/cjlapao/aeon/internal/store"
)

// roadmapDoorSpec is one row of T0 §7's table.
type roadmapDoorSpec struct {
	verb   string
	method protocol.MethodName
	direct bool // the direct door answers this verb offline
}

// roadmapDoorTable is the fixture: fourteen verbs, the exact road each
// takes. It belongs to this test alone — no production code reads it.
var roadmapDoorTable = []roadmapDoorSpec{
	{"add", protocol.MethodRoadmapAdd, false},
	{"list", protocol.MethodRoadmapList, false},
	{"show", protocol.MethodRoadmapShow, false},
	{"cancel", protocol.MethodRoadmapCancel, false},
	// S27 T3 (CN7): the graph's edges speak verbs too (T0 §7's law held by
	// the fixture). Fourteen become sixteen, deliberate.
	{"depend", protocol.MethodRoadmapDepend, false},
	{"undepend", protocol.MethodRoadmapUndepend, false},
	{"estimate", protocol.MethodRoadmapEstimate, false},
	{"priority", protocol.MethodRoadmapPriority, false},
	{"update", protocol.MethodRoadmapUpdate, true},
	{"history", protocol.MethodRoadmapHistory, true},
	{"board", protocol.MethodRoadmapBoard, true},
	{"health", protocol.MethodRoadmapHealth, true},
	{"vocabulary", protocol.MethodRoadmapVocabulary, true},
	{"ideate", protocol.MethodRoadmapIdeate, false},
	{"plan", protocol.MethodRoadmapPlan, false},
	{"materialize", protocol.MethodRoadmapMaterialize, false},
}

// TestRoadmapDoorTableIsTheTable walks the table in BOTH directions: every
// verb's method is registered on a wired daemon and RequiresInitialize
// (the verb has a door), and every roadmap.* method a wired daemon
// registers has a verb (a T7 that adds a sixteenth door without a CLI
// verb fails HERE, at the fixture, not in a UI ticket).
func TestRoadmapDoorTableIsTheTable(t *testing.T) {
	d := blueprintGuardDaemon(t)
	for _, row := range roadmapDoorTable {
		got, ok := d.Table.Lookup(row.method)
		if !ok {
			t.Errorf("roadmap %s speaks %s, which is not registered — the verb would have no door", row.verb, row.method)
			continue
		}
		if !got.RequiresInitialize {
			t.Errorf("roadmap %s: %s does not RequireInitialize", row.verb, row.method)
		}
	}
	inTable := map[protocol.MethodName]bool{}
	for _, row := range roadmapDoorTable {
		inTable[row.method] = true
	}
	// roadmap.proposeIdea is the plane's one door with no roadmap VERB:
	// it is an AGENT-proposal twin (S50 T5, S45's proposal plane) — the
	// human speaks `proposal decide`, never a roadmap CLI verb, and the
	// agent speaks the agent surface. The exemption is named, not silent.
	noVerb := map[protocol.MethodName]string{
		protocol.MethodRoadmapProposeIdea: "agent-proposal twin; the human door is `aeon proposal decide`",
	}
	for _, m := range d.Table.Methods() {
		if !strings.HasPrefix(string(m), "roadmap.") {
			continue
		}
		if _, exempt := noVerb[m]; exempt {
			continue
		}
		if !inTable[m] {
			t.Errorf("registered method %s has no CLI verb in roadmapDoorTable — T0 §7's law is every door speaks a verb", m)
		}
	}
}

// --- selectRoadmapDoor -----------------------------------------------------

func TestSelectRoadmapDoorFallsBackOnlyWhenNoDaemon(t *testing.T) {
	restore := roadmapDial
	t.Cleanup(func() { roadmapDial = restore })

	// (a) dial answers → socket door, no notice.
	var errOut bytes.Buffer
	roadmapDial = func(int) (*protocol.Client, error) { return &protocol.Client{}, nil }
	if door, err := selectRoadmapDoor(&errOut); err != nil || !strings.Contains(fmt.Sprintf("%T", door), "SocketDoor") || errOut.Len() != 0 {
		t.Fatalf("dial success: door %T err %v notice %q — want the socket door and no notice", door, err, errOut.String())
	}

	// (b) no endpoint → direct door WITH the fixed line, byte for byte.
	errOut.Reset()
	roadmapDial = func(int) (*protocol.Client, error) {
		return nil, fmt.Errorf("no daemon at /nonexistent/aeon.sock: %w", errors.New("connect: no such file or directory"))
	}
	door, err := selectRoadmapDoor(&errOut)
	if err != nil || !strings.Contains(fmt.Sprintf("%T", door), "DirectDoor") {
		t.Fatalf("no daemon: door %T err %v — want the direct door", door, err)
	}
	if errOut.String() != roadmapDirectNotice+"\n" {
		t.Errorf("notice = %q, want the fixed line %q plus a newline", errOut.String(), roadmapDirectNotice)
	}

	// (c) any OTHER dial failure → refusal, no door, no notice.
	errOut.Reset()
	roadmapDial = func(int) (*protocol.Client, error) { return nil, errors.New("dial refused by the endpoint") }
	if door, err := selectRoadmapDoor(&errOut); err == nil || door != nil || errOut.Len() != 0 {
		t.Fatalf("failed dial: door %T err %v notice %q — the CLI never routes around a daemon that merely failed", door, err, errOut.String())
	}
}

// TestRoadmapDirectDoorHasNoLogicCopy reads roadmap_client.go for the
// claim: the direct arm opens the store, hands it to the daemon's door
// constructor, and calls. It holds no update loop, no filter, no refusal
// sentence of its own.
func TestRoadmapDirectDoorHasNoLogicCopy(t *testing.T) {
	src := readFileForGuard(t, "roadmap_client.go")
	i := strings.Index(src, "// --- the direct door")
	if i < 0 {
		t.Fatal("roadmap_client.go lost the direct door section — the guard reads it by section")
	}
	arm := src[i:]
	for _, banned := range []string{"roadmap.New(", "roadmapRefusal", "does not exist", "internal/store", "UPDATE "} {
		if strings.Contains(arm, banned) {
			t.Errorf("the direct door contains %q — the offline arm must call the daemon's answers, not compute them (no logic copy)", banned)
		}
	}
	for _, required := range []string{"openStoreDirectly(false)", "daemon.NewRoadmapManageDoors", "roadmap.Vocabulary()", "roadmapVerbatim(err)"} {
		if !strings.Contains(arm, required) {
			t.Errorf("the direct door no longer reads %q — its one-call shape is what makes a copy impossible", required)
		}
	}
}

// TestRoadmapTakesNoCredentialFlagsCoversThePlane extends S26's guard to
// the files S50 adds: no credential ever becomes a flag value, and the
// store reach stays confined to roadmap_client.go's direct door.
func TestRoadmapTakesNoCredentialFlagsCoversThePlane(t *testing.T) {
	for _, f := range []string{"roadmap.go", "roadmap_manage.go", "roadmap_client.go"} {
		src := readFileForGuard(t, f)
		for _, banned := range []string{`"key"`, `"token"`, `"secret"`, `"apikey"`, `"api-key"`, `"password"`} {
			if strings.Contains(src, banned) {
				t.Errorf("%s registers credential flag %s — the environment, or a path to a file (§4)", f, banned)
			}
		}
	}
	for _, f := range []string{"roadmap.go", "roadmap_manage.go"} {
		src := readFileForGuard(t, f)
		for _, banned := range []string{"store.Open", "internal/store"} {
			if strings.Contains(src, banned) {
				t.Errorf("%s reaches for %s — the roadmap CLI's store reach is confined to roadmap_client.go's direct door (P34)", f, banned)
			}
		}
	}
	client := readFileForGuard(t, "roadmap_client.go")
	if !strings.Contains(client, "openStoreDirectly") {
		t.Error("roadmap_client.go no longer opens the store — if the offline door moved, THIS guard must move with it, not silently pass")
	}
}

// --- two doors, one answer ---------------------------------------------------

// TestRoadmapUpdateRefusalVerbatimBothDoors gives the socket and the
// direct door the SAME illegal edit (an over-long title, RM1's byte law
// decided underneath) and demands the SAME sentence out, byte for byte,
// mapped through the CLI's own verbatim mapper on each road.
func TestRoadmapUpdateRefusalVerbatimBothDoors(t *testing.T) {
	d := startDaemon(t)
	repo := queueRepo(t, d, "rmrefuse")
	st := openFixtureStoreForCLI(t, d)
	defer st.Close()
	seed, refusals := roadmap.New(st).CreateAudited(repo, roadmap.NewItem{
		Title: "refusal pair", Provenance: roadmap.ProvenanceAuthored,
	}, roadmap.Actor{Kind: "user", ID: "carlos"})
	if refusals != nil {
		t.Fatalf("seed refusal: %s", refusals.Reason)
	}
	long := strings.Repeat("x", roadmap.TitleMaxBytes+1)

	conn, err := net.Dial("unix", d.endpoint)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close()
	c := protocol.NewClient(conn, ClientName, Version)
	if _, err := c.Connect(0); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	var sock protocol.RoadmapUpdateResult
	_, _ = seed.ID, sock
	sockErr := c.Call(protocol.MethodRoadmapUpdate, protocol.RoadmapUpdateParams{
		ID: seed.ID, Title: &long,
	}, &sock)
	if sockErr == nil {
		t.Fatal("the socket accepted an over-long title — RM1's law moved up the stack")
	}
	directErr := func() error {
		_, err := roadmapDirectDoor{}.update(context.Background(), roadmapCLIActor,
			protocol.RoadmapUpdateParams{ID: seed.ID, Title: &long})
		return err
	}()
	if directErr == nil {
		t.Fatal("the direct door accepted an over-long title — the offline road re-validates nothing, including nothing refused")
	}
	want, got := roadmapVerbatim(sockErr).Error(), roadmapVerbatim(directErr).Error()
	if want != got {
		t.Errorf("the two roads refuse differently:\n socket: %s\n direct: %s", want, got)
	}
	if !strings.Contains(want, "200") {
		t.Errorf("the shared refusal %q does not name the bound it enforced", want)
	}
}

// TestRoadmapDirectActorIsCli walks the offline update and reads the
// audit row it must leave: the actor is {cli, cli}, named, not guessed.
func TestRoadmapDirectActorIsCli(t *testing.T) {
	d := startDaemon(t)
	repo := queueRepo(t, d, "rmactor")
	st := openFixtureStoreForCLI(t, d)
	defer st.Close()
	seed, refusals := roadmap.New(st).CreateAudited(repo, roadmap.NewItem{
		Title: "offline edit lands", Provenance: roadmap.ProvenanceAuthored,
	}, roadmap.Actor{Kind: "user", ID: "carlos"})
	if refusals != nil {
		t.Fatalf("seed refusal: %s", refusals.Reason)
	}
	newTitle := "offline edit landed"
	res, err := roadmapDirectDoor{}.update(context.Background(), roadmapCLIActor,
		protocol.RoadmapUpdateParams{ID: seed.ID, Title: &newTitle})
	if err != nil {
		t.Fatalf("direct update: %v", err)
	}
	if !res.Changed || res.Item.Title != newTitle {
		t.Fatalf("direct update answered %+v — want the change applied", res.Item)
	}
	page, err := roadmapDirectDoor{}.history(context.Background(), protocol.RoadmapHistoryParams{ID: seed.ID})
	if err != nil {
		t.Fatalf("direct history: %v", err)
	}
	var seen bool
	for _, ch := range page.Changes {
		if ch.Op == "update" {
			seen = true
			if ch.ActorKind != "cli" || ch.ActorID != "cli" {
				t.Errorf("offline update audited by %s:%s — the direct door stamps {cli, cli} and says so", ch.ActorKind, ch.ActorID)
			}
		}
	}
	if !seen {
		t.Fatal("no update row on the trail after an offline edit")
	}
}

// TestRoadmapFourReadsSpeakByteIdenticalThroughBothDoors runs history,
// board, vocabulary and health through both doors against ONE state and
// compares answers — health with its one clock field normalised (two
// moments, one truth; its PRESENCE is asserted, its bytes cannot match).
func TestRoadmapFourReadsSpeakByteIdenticalThroughBothDoors(t *testing.T) {
	d := startDaemon(t)
	repo := queueRepo(t, d, "rmparity")
	conn, err := net.Dial("unix", d.endpoint)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close()
	c := protocol.NewClient(conn, ClientName, Version)
	if _, err := c.Connect(0); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	var add protocol.RoadmapItemResult
	if err := c.Call(protocol.MethodRoadmapAdd, protocol.RoadmapAddParams{
		ComponentRoot: repo, Title: "parity item", Provenance: "authored",
		PriorityState: "known", PriorityLabel: "P2",
	}, &add); err != nil {
		t.Fatalf("seed add over the socket: %v", err)
	}
	st := openFixtureStoreForCLI(t, d)
	defer st.Close()
	direct := roadmapDirectDoor{}

	for _, step := range []struct {
		name   string
		method protocol.MethodName
		params any
		call   func() (any, error)
	}{
		{"history", protocol.MethodRoadmapHistory, protocol.RoadmapHistoryParams{ID: add.Item.ID}, func() (any, error) {
			return direct.history(context.Background(), protocol.RoadmapHistoryParams{ID: add.Item.ID})
		}},
		{"board", protocol.MethodRoadmapBoard, protocol.RoadmapBoardParams{ComponentRoot: repo}, func() (any, error) {
			return direct.board(context.Background(), protocol.RoadmapBoardParams{ComponentRoot: repo})
		}},
		{"vocabulary", protocol.MethodRoadmapVocabulary, struct{}{}, func() (any, error) {
			return direct.vocabulary(context.Background())
		}},
		{"health", protocol.MethodRoadmapHealth, struct{}{}, func() (any, error) {
			h, err := direct.health(context.Background())
			h.EvaluatedAt = ""
			return h, err
		}},
	} {
		var raw json.RawMessage
		if err := c.Call(step.method, step.params, &raw); err != nil {
			t.Fatalf("socket %s: %v", step.name, err)
		}
		offOut, err := step.call()
		if err != nil {
			t.Fatalf("direct %s: %v", step.name, err)
		}
		offRaw, err := json.Marshal(offOut)
		if err != nil {
			t.Fatal(err)
		}
		if step.name == "health" {
			var h protocol.RoadmapHealthResult
			if err := json.Unmarshal(raw, &h); err != nil {
				t.Fatal(err)
			}
			h.EvaluatedAt = ""
			raw, err = json.Marshal(h)
			if err != nil {
				t.Fatal(err)
			}
		}
		if !jsonEquivalent(t, raw, offRaw) {
			t.Errorf("%s: socket %s, direct %s — the two doors must answer the same bytes", step.name, raw, offRaw)
		}
	}

	// update, both doors, one row: the socket's edit changes, the direct
	// door replaying the SAME title changes nothing (changed:false IS the
	// parity of a no-op), and both items agree byte for byte.
	var sock protocol.RoadmapUpdateResult
	newTitle := "parity title"
	if err := c.Call(protocol.MethodRoadmapUpdate, protocol.RoadmapUpdateParams{
		ID: add.Item.ID, Title: &newTitle,
	}, &sock); err != nil {
		t.Fatalf("socket update: %v", err)
	}
	off, err := direct.update(context.Background(), roadmapCLIActor,
		protocol.RoadmapUpdateParams{ID: add.Item.ID, Title: &newTitle})
	if err != nil {
		t.Fatalf("direct update: %v", err)
	}
	if !sock.Changed || off.Changed {
		t.Errorf("changed flags disagree: socket %v direct %v — the edit changed, the echo of it must not", sock.Changed, off.Changed)
	}
	agreeItems(t, "update", off.Item, sock.Item)
}

// --- the verbs print what the daemon said -----------------------------------

func TestRoadmapUpdateAndBoardPrintTheDaemonsWords(t *testing.T) {
	d := startDaemon(t)
	repo := queueRepo(t, d, "rmprint")
	out := d.mustRun("roadmap", "add", "--component", repo, "--title", "print check", "--provenance", "authored")
	id := roadmapCLIID(t, out)

	out = d.mustRun("roadmap", "update", fmt.Sprint(id), "--title", "print check II")
	if !strings.Contains(out, "#"+fmt.Sprint(id)) || !strings.Contains(out, "print check II") {
		t.Fatalf("update print: %q", out)
	}
	out = d.mustRun("roadmap", "update", fmt.Sprint(id), "--title", "print check II")
	if !strings.Contains(out, "unchanged — the edit named nothing that differs") {
		t.Fatalf("changed:false print: %q — the no-op must say nothing was audited", out)
	}
	// No --status flag EXISTS on update (RM1's allow-list is the flags'
	// shape too): the verb refuses the request before any door sees it.
	if _, err := d.run("roadmap", "update", fmt.Sprint(id), "--status", "done"); err == nil {
		t.Fatal("roadmap update --status was accepted — RM1's field set must be shaped into the flags, too")
	}

	out = d.mustRun("roadmap", "board", "--component", repo, "--json")
	var board protocol.RoadmapBoardResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &board); err != nil {
		t.Fatalf("board --json is not the door's bytes and nothing else: %v (%q)", err, out)
	}
	if len(board.Rows) != 1 || board.Rows[0].Item.ID != id {
		t.Fatalf("board --json rows: %+v", board.Rows)
	}
	out = d.mustRun("roadmap", "board", "--component", repo)
	// D4's blocker rides the board verbatim: an idea with no priority
	// reads NOT ready with the daemon's own sentence, and the row prints
	// the daemon's own Describe, not a re-render.
	if !strings.Contains(out, "not ready:") {
		t.Fatalf("board human print: %q — the blocker line is the point of RM6", out)
	}
	if !strings.Contains(out, board.Rows[0].Describe) {
		t.Fatalf("board human print lost the Describe sentence: %q", out)
	}

	out = d.mustRun("roadmap", "history", fmt.Sprint(id))
	for _, want := range []string{"add", "update", "user:"} {
		if !strings.Contains(out, want) {
			t.Errorf("history print lost %q:\n%s", want, out)
		}
	}
}

func TestRoadmapVocabularyAndHealthSpeakThePlane(t *testing.T) {
	d := startDaemon(t)
	out := d.mustRun("roadmap", "vocabulary")
	for _, want := range []string{"idea → ideating | cancelled", "known | assumed | unknown", "dispatch · This starts agents"} {
		if !strings.Contains(out, want) {
			t.Errorf("vocabulary print lost %q:\n%s", want, out)
		}
	}
	out = d.mustRun("roadmap", "health")
	for _, want := range []string{
		"roadmap.ideationBlueprint · brainstorm", "roadmap.planBlueprint · roadmap-plan",
		"evaluated", "advice only — a green line never promises a dispatch",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("health print lost %q:\n%s", want, out)
		}
	}
}

// --- the dispatch gate -------------------------------------------------------

func TestRoadmapDispatchGateWritesNothingWithoutYes(t *testing.T) {
	d := startDaemon(t)
	repo := queueRepo(t, d, "rmgate")
	ws := t.TempDir()
	ideaID := roadmapCLIID(t, d.mustRun("roadmap", "add", "--component", repo,
		"--title", "gated ideate", "--provenance", "authored"))
	planID := roadmapCLIID(t, d.mustRun("roadmap", "add", "--component", repo,
		"--title", "gated plan", "--provenance", "authored"))

	cases := []struct {
		name string
		id   int64
		args []string
	}{
		{"ideate", ideaID, []string{"roadmap", "ideate", fmt.Sprint(ideaID), "--workspace", ws}},
		{"plan", planID, []string{"roadmap", "plan", fmt.Sprint(planID), "--workspace", ws}},
		{"materialize", ideaID, []string{"roadmap", "materialize", fmt.Sprint(ideaID),
			"--blueprint", "worker", "--goal", "gate", "--workspace", ws}},
	}
	for _, c := range cases {
		out, err := d.run(c.args...)
		if err == nil {
			t.Errorf("%s without --yes returned no error:\n%s", c.name, out)
		} else if !strings.Contains(err.Error(), "--yes") && !strings.Contains(out, "--yes") {
			t.Errorf("%s without --yes: err %v out %q — want a refusal naming the --yes re-run", c.name, err, out)
		}
		if !strings.Contains(out, "This starts agents") {
			t.Errorf("%s printed no authority statement:\n%s", c.name, out)
		}
		if strings.Contains(out, "<item>") {
			t.Errorf("%s left the <item> slot unfilled:\n%s", c.name, out)
		}
	}
	st := openFixtureStoreForCLI(t, d)
	defer st.Close()
	r := roadmap.New(st)
	for _, c := range cases {
		it, ok, err := r.Show(c.id)
		if err != nil || !ok {
			t.Fatalf("show %d: %v", c.id, err)
		}
		if it.Status != roadmap.StatusIdea {
			t.Errorf("%s moved the item to %s without --yes — the gate speaks, it does not act", c.name, it.Status)
		}
	}
	hres, err := protocol.AnswerRoadmapHistory(context.Background(), roadmap.New(st), protocol.RoadmapHistoryParams{ID: ideaID})
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	page := hres.(protocol.RoadmapHistoryResult)
	for _, ch := range page.Changes {
		if ch.Op == "dispatch" || ch.Op == "transition" {
			t.Errorf("the gated item carries %q on its trail — a gateless moment was written", ch.Op)
		}
	}
}

// --- helpers ----------------------------------------------------------------

// openFixtureStoreForCLI opens the fixture daemon's state store the way
// the CLI's direct door does — the second access path WAL serialises.
// steerDirectDoorToFixture points the PROCESS at the fixture's state dir
// so in-process direct-door calls open the same store the fixture daemon
// holds. The fixture's own env is XDG_STATE_HOME (cli_integration_test.go
// lines 274; StoreDirFor appends the `aeon` directory); the ambient
// AEON_STATE_DIR and the appenv-exported sqlite paths must NOT win here,
// which is why they are cleared. A developer's real store is never the
// answer's subject.
func steerDirectDoorToFixture(t *testing.T, d *daemonFixture) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", d.stateDir)
	t.Setenv("AEON_STATE_DIR", "")
	t.Setenv("DATABASE_SQLITE_STORAGE_PATH", "")
	t.Setenv("DATABASE_SQLITE_FILE_NAME", "")
	t.Setenv("DATABASE_SQLITE_DSN", "")
}

func openFixtureStoreForCLI(t *testing.T, d *daemonFixture) *store.Store {
	t.Helper()
	steerDirectDoorToFixture(t, d)
	st, err := store.Open(store.Options{
		Dir: filepath.Join(d.stateDir, "aeon"),
		Env: func(string) string { return "" },
	})
	if err != nil {
		t.Fatalf("open the fixture store directly: %v", err)
	}
	return st
}

func jsonEquivalent(t *testing.T, a, b json.RawMessage) bool {
	t.Helper()
	var va, vb any
	if err := json.Unmarshal(a, &va); err != nil {
		t.Fatalf("unmarshal a: %v (%s)", err, a)
	}
	if err := json.Unmarshal(b, &vb); err != nil {
		t.Fatalf("unmarshal b: %v (%s)", err, b)
	}
	return reflect.DeepEqual(va, vb)
}

func agreeItems(t *testing.T, what string, got, want protocol.RoadmapItemView) {
	t.Helper()
	// updatedAt moves with the audit row between the two reads;
	// everything else must agree.
	got.UpdatedAt, want.UpdatedAt = "", ""
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s item disagrees:\n got  %+v\n want %+v", what, got, want)
	}
}
