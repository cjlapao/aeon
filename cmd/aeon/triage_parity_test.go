package main

// S51 T6 — the parity record between the two triage doors and the gates
// the enabling verbs hold. Nine S29 verbs ride the socket alone (the
// goldens, TM9: their roads and bytes do not move), five S51 verbs ride
// BOTH doors (the gaps S51 closes), and triage.fileAs is the agent's
// filing twin with no CLI verb at all (TM6 — the person's door is
// triage.file). Guards: the direct door copies no logic (every method is
// one call into daemon.NewTriageManageDoors), the offline stamp is
// {cli, cli}, the selection refuses rather than routing around a dial
// that merely failed, and the --yes gates print and write NOTHING
// without the confirmation — the statement first, the act only after.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
	"syscall"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/triage"
)

// triageDoorSpec is one row of T6's door table.
type triageDoorSpec struct {
	verb   string
	method protocol.MethodName
	direct bool // the direct door answers this verb offline
}

// triageDoorTable is the fixture: fourteen methods, the exact road each
// takes. It belongs to this test alone — no production code reads it.
var triageDoorTable = []triageDoorSpec{
	{"file", protocol.MethodTriageFile, false},
	{"list", protocol.MethodTriageList, false},
	{"show", protocol.MethodTriageShow, false},
	{"set-priority", protocol.MethodTriageSetPriority, false},
	{"confirm-duplicate", protocol.MethodTriageConfirmDuplicate, false},
	{"validate", protocol.MethodTriageValidate, false},
	{"handoff", protocol.MethodTriageHandoff, false},
	{"suggest-duplicates", protocol.MethodTriageSuggest, false},
	{"update", protocol.MethodTriageUpdate, true},
	{"history", protocol.MethodTriageHistory, true},
	{"board", protocol.MethodTriageBoard, true},
	{"health", protocol.MethodTriageHealth, true},
	{"vocabulary", protocol.MethodTriageVocabulary, true},
}

// TestTriageDoorTableIsTheTable walks the table in BOTH directions: every
// verb's method is registered on a wired daemon and RequiresInitialize
// (the verb has a door), and every triage.* method a wired daemon
// registers has a verb — triage.fileAs named as the exemption it is.
func TestTriageDoorTableIsTheTable(t *testing.T) {
	d := blueprintGuardDaemon(t)
	for _, row := range triageDoorTable {
		got, ok := d.Table.Lookup(row.method)
		if !ok {
			t.Errorf("triage %s speaks %s, which is not registered — the verb would have no door", row.verb, row.method)
			continue
		}
		if !got.RequiresInitialize {
			t.Errorf("triage %s: %s does not RequireInitialize", row.verb, row.method)
		}
	}
	inTable := map[protocol.MethodName]bool{}
	for _, row := range triageDoorTable {
		inTable[row.method] = true
	}
	// triage.fileAs is the plane's one door with no triage VERB: it is
	// the AGENT's filing twin (S51 T5, TM6) — the person speaks
	// `triage file`, an agent speaks the agent surface. Named, not silent.
	noVerb := map[protocol.MethodName]string{
		protocol.MethodTriageFileAs: "agent filing twin (TM6); the person's door is triage.file",
	}
	for _, m := range d.Table.Methods() {
		if !strings.HasPrefix(string(m), "triage.") {
			continue
		}
		if _, exempt := noVerb[m]; exempt {
			continue
		}
		if !inTable[m] {
			t.Errorf("registered method %s has no CLI verb in triageDoorTable — every door speaks a verb", m)
		}
	}
}

// --- selectTriageDoor ------------------------------------------------------

func TestSelectTriageDoorFallsBackOnlyWhenNoDaemon(t *testing.T) {
	restore := triageDial
	t.Cleanup(func() { triageDial = restore })

	// (a) dial answers → socket door, no notice.
	var errOut bytes.Buffer
	triageDial = func(int) (*protocol.Client, error) { return &protocol.Client{}, nil }
	if door, err := selectTriageDoor(&errOut); err != nil || !strings.Contains(fmt.Sprintf("%T", door), "SocketDoor") || errOut.Len() != 0 {
		t.Fatalf("dial success: door %T err %v notice %q — want the socket door and no notice", door, err, errOut.String())
	}

	// (b) no endpoint → direct door WITH the fixed line, byte for byte.
	errOut.Reset()
	triageDial = func(int) (*protocol.Client, error) {
		return nil, fmt.Errorf("no daemon at /nonexistent/aeon.sock: %w", errors.New("connect: no such file or directory"))
	}
	door, err := selectTriageDoor(&errOut)
	if err != nil || !strings.Contains(fmt.Sprintf("%T", door), "DirectDoor") {
		t.Fatalf("no daemon: door %T err %v — want the direct door", door, err)
	}
	if errOut.String() != triageDirectNotice+"\n" {
		t.Errorf("notice = %q, want the fixed line %q plus a newline", errOut.String(), triageDirectNotice)
	}

	// (c) any OTHER dial failure → refusal, no door, no notice.
	errOut.Reset()
	triageDial = func(int) (*protocol.Client, error) { return nil, errors.New("dial refused by the endpoint") }
	if door, err := selectTriageDoor(&errOut); err == nil || door != nil || errOut.Len() != 0 {
		t.Fatalf("failed dial: door %T err %v notice %q — the CLI never routes around a daemon that merely failed", door, err, errOut.String())
	}
}

// TestTriageDirectDoorHasNoLogicCopy reads triage_client.go for the
// claim: the direct arm opens the store, hands it to the daemon's door
// constructor, and calls. It holds no update loop, no filter, no refusal
// sentence of its own.
func TestTriageDirectDoorHasNoLogicCopy(t *testing.T) {
	src := readFileForGuard(t, "triage_client.go")
	i := strings.Index(src, "// --- the direct door")
	if i < 0 {
		t.Fatal("triage_client.go lost the direct door section — the guard reads it by section")
	}
	arm := src[i:]
	for _, banned := range []string{"triage.New(", "does not exist", "internal/store", "UPDATE ", "ErrTriage"} {
		if strings.Contains(arm, banned) {
			t.Errorf("the direct door contains %q — the offline arm must call the daemon's answers, not compute them (no logic copy)", banned)
		}
	}
	for _, required := range []string{"openStoreDirectly(false)", "daemon.NewTriageManageDoors", "(&triage.Triage{}).Vocabulary()", "dr.Health(ctx)"} {
		if !strings.Contains(arm, required) {
			t.Errorf("the direct door no longer reads %q — its one-call shape is what makes a copy impossible", required)
		}
	}
}

// TestTriageTakesNoCredentialFlagsCoversThePlane extends the standing
// guard to the files S51 adds: no credential ever becomes a flag value,
// and the store reach stays confined to triage_client.go's direct door.
func TestTriageTakesNoCredentialFlagsCoversThePlane(t *testing.T) {
	for _, f := range []string{"triage.go", "triage_client.go"} {
		src := readFileForGuard(t, f)
		for _, banned := range []string{`"key"`, `"token"`, `"secret"`, `"apikey"`, `"api-key"`, `"password"`} {
			if strings.Contains(src, banned) {
				t.Errorf("%s registers credential flag %s — the environment, or a path to a file (§4)", f, banned)
			}
		}
	}
	src := readFileForGuard(t, "triage.go")
	for _, banned := range []string{"store.Open", "internal/store"} {
		if strings.Contains(src, banned) {
			t.Errorf("triage.go reaches for %s — the triage CLI's store reach is confined to triage_client.go's direct door (P34)", banned)
		}
	}
	client := readFileForGuard(t, "triage_client.go")
	if !strings.Contains(client, "openStoreDirectly") {
		t.Error("triage_client.go no longer opens the store — if the offline door moved, THIS guard must move with it, not silently pass")
	}
}

// --- two doors, one answer ---------------------------------------------------

// TestTriageUpdateRefusalVerbatimBothDoors gives the socket and the
// direct door the SAME illegal edit (a 201-character title — TM1's byte
// law decided underneath) and demands the SAME sentence out, byte for
// byte, mapped through the CLI's own verbatim mapper on each road.
func TestTriageUpdateRefusalVerbatimBothDoors(t *testing.T) {
	d := startDaemon(t)
	repo := queueRepo(t, d, "t6refuse")
	id := triageCLIID(t, d.mustRun("triage", "file", "--component", repo,
		"--title", "refusal pair", "--body", "repro: both doors, one sentence"))
	long := strings.Repeat("x", 201)

	conn, err := net.Dial("unix", d.endpoint)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close()
	c := protocol.NewClient(conn, ClientName, Version)
	if _, err := c.Connect(0); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	var sock protocol.TriageUpdateResult
	sockErr := c.Call(protocol.MethodTriageUpdate, protocol.TriageUpdateParams{
		ID: id, Title: &long,
	}, &sock)
	if sockErr == nil {
		t.Fatal("the socket accepted a 201-character title — TM1's law moved up the stack")
	}
	steerDirectDoorToFixture(t, d)
	directErr := func() error {
		_, err := triageDirectDoor{}.update(context.Background(), triageCLIActor,
			protocol.TriageUpdateParams{ID: id, Title: &long})
		return err
	}()
	if directErr == nil {
		t.Fatal("the direct door accepted a 201-character title — the offline road re-validates nothing, including nothing refused")
	}
	want, got := triageVerbatim(sockErr).Error(), triageVerbatim(directErr).Error()
	if want != got {
		t.Errorf("the two roads refuse differently:\n socket: %s\n direct: %s", want, got)
	}
	if !strings.Contains(want, "200") {
		t.Errorf("the shared refusal %q does not name the bound it enforced", want)
	}
}

// TestTriageDirectActorIsCli walks the offline update and reads the
// audit row it must leave: the actor is {cli, cli}, named, not guessed —
// bug_change's CHECK admitted 'cli' the day T1 drew it, FOR this door.
func TestTriageDirectActorIsCli(t *testing.T) {
	d := startDaemon(t)
	repo := queueRepo(t, d, "t6actor")
	id := triageCLIID(t, d.mustRun("triage", "file", "--component", repo,
		"--title", "offline edit lands", "--body", "repro: the stamp is the point"))
	// The direct door answers when there is NO daemon — the honest
	// production shape (P5). The fixture daemon is stopped before the
	// offline write: a live daemon's start-time write sweep holds the one
	// WAL writer for a settling moment, and a door that writes beside it
	// tests a state selection can never produce.
	stopFixtureDaemon(t, d)
	steerDirectDoorToFixture(t, d)
	newTitle := "offline edit landed"
	res, err := triageDirectDoor{}.update(context.Background(), triageCLIActor,
		protocol.TriageUpdateParams{ID: id, Title: &newTitle})
	if err != nil {
		t.Fatalf("direct update: %v", err)
	}
	if !res.Changed || res.Bug == nil || res.Bug.Title != newTitle {
		t.Fatalf("direct update answered %+v — want the change applied", res.Bug)
	}
	rows, err := triageDirectDoor{}.history(context.Background(), protocol.TriageHistoryParams{ID: id})
	if err != nil {
		t.Fatalf("direct history: %v", err)
	}
	var seen bool
	for _, ch := range rows {
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

// TestTriageFourReadsSpeakByteIdenticalThroughBothDoors runs history,
// board, vocabulary and health through both doors against ONE state and
// compares answers — health with its one clock field normalised (two
// moments, one truth; its PRESENCE is asserted, its bytes cannot match).
func TestTriageFourReadsSpeakByteIdenticalThroughBothDoors(t *testing.T) {
	d := startDaemon(t)
	repo := queueRepo(t, d, "t6parity")
	id := triageCLIID(t, d.mustRun("triage", "file", "--component", repo,
		"--title", "parity bug", "--body", "repro: the two doors, one answer"))
	conn, err := net.Dial("unix", d.endpoint)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close()
	c := protocol.NewClient(conn, ClientName, Version)
	if _, err := c.Connect(0); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	steerDirectDoorToFixture(t, d)
	direct := triageDirectDoor{}

	for _, step := range []struct {
		name   string
		method protocol.MethodName
		params any
		call   func() (any, error)
	}{
		{"history", protocol.MethodTriageHistory, protocol.TriageHistoryParams{ID: id}, func() (any, error) {
			return direct.history(context.Background(), protocol.TriageHistoryParams{ID: id})
		}},
		{"board", protocol.MethodTriageBoard, protocol.TriageBoardParams{ComponentRoot: repo}, func() (any, error) {
			return direct.board(context.Background(), protocol.TriageBoardParams{ComponentRoot: repo})
		}},
		{"vocabulary", protocol.MethodTriageVocabulary, struct{}{}, func() (any, error) {
			return direct.vocabulary(context.Background())
		}},
		{"health", protocol.MethodTriageHealth, struct{}{}, func() (any, error) {
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
			var h protocol.TriageHealthResult
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
	// parity of a no-op), and both bugs agree byte for byte.
	var sock protocol.TriageUpdateResult
	newTitle := "parity title"
	if err := c.Call(protocol.MethodTriageUpdate, protocol.TriageUpdateParams{
		ID: id, Title: &newTitle,
	}, &sock); err != nil {
		t.Fatalf("socket update: %v", err)
	}
	off, err := direct.update(context.Background(), triageCLIActor,
		protocol.TriageUpdateParams{ID: id, Title: &newTitle})
	if err != nil {
		t.Fatalf("direct update: %v", err)
	}
	if !sock.Changed || off.Changed {
		t.Errorf("changed flags disagree: socket %v direct %v — the edit changed, the echo of it must not", sock.Changed, off.Changed)
	}
	agreeTriageBugs(t, "update", off.Bug, sock.Bug)
}

// --- the verbs print what the daemon said -----------------------------------

func TestTriageUpdateAndBoardPrintTheDaemonsWords(t *testing.T) {
	d := startDaemon(t)
	repo := queueRepo(t, d, "t6print")
	id := triageCLIID(t, d.mustRun("triage", "file", "--component", repo,
		"--title", "print check", "--body", "repro: print, both state words separate"))

	out := d.mustRun("triage", "update", fmt.Sprint(id), "--title", "print check II")
	if !strings.Contains(out, "#"+fmt.Sprint(id)) || !strings.Contains(out, "print check II") {
		t.Fatalf("update print: %q", out)
	}
	out = d.mustRun("triage", "update", fmt.Sprint(id), "--title", "print check II")
	if !strings.Contains(out, "unchanged — the edit named nothing that differs") {
		t.Fatalf("changed:false print: %q — the no-op must say nothing was audited", out)
	}
	// TM1's allow-list is the FLAGS' shape too: an edit key that smuggles
	// state or priority is refused before any door sees it.
	for _, smuggle := range [][]string{{"--validation", "reproduced"}, {"--state", "known"}} {
		if _, err := d.run("triage", "update", fmt.Sprint(id), smuggle[0], smuggle[1]); err == nil {
			t.Fatalf("triage update %s %s was accepted — the edit touches title and body only (TM1)", smuggle[0], smuggle[1])
		}
	}

	out = d.mustRun("triage", "board", "--component", repo, "--json")
	var board []protocol.TriageBoardRow
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &board); err != nil {
		t.Fatalf("board --json is not the door's bytes and nothing else: %v (%q)", err, out)
	}
	if len(board) != 1 || board[0].Bug == nil || board[0].Bug.ID != id {
		t.Fatalf("board --json rows: %+v", board)
	}
	out = d.mustRun("triage", "board", "--component", repo)
	// The two state sets print as SEPARATE sentences — no single line may
	// carry both sentences (R4's guard, walked on the human print).
	var sawValidation, sawPriority bool
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, board[0].Describe.ValidationSentence) &&
			strings.Contains(line, board[0].Describe.PrioritySentence) {
			t.Fatalf("one line carries BOTH describe sentences — the two sets are never one badge (R4):\n%s", line)
		}
		sawValidation = sawValidation || strings.Contains(line, board[0].Describe.ValidationSentence)
		sawPriority = sawPriority || strings.Contains(line, board[0].Describe.PrioritySentence)
	}
	if !sawValidation || !sawPriority {
		t.Fatalf("board human print lost a describe sentence:\n%s", out)
	}
	if !strings.Contains(out, "attempts:") {
		t.Fatalf("board human print lost the attempt digest:\n%s", out)
	}
	// A closed-set filter word that is not in the set is refused BY NAME,
	// never coerced (the daemon's sentence, verbatim through the CLI).
	if out, err := d.run("triage", "board", "--component", repo, "--validation", "banana"); err == nil {
		t.Fatalf("board --validation banana was accepted: %q", out)
	} else if !strings.Contains(err.Error()+out, "banana") {
		t.Errorf("the filter refusal does not name the word it refused: %v (%q)", err, out)
	}

	out = d.mustRun("triage", "history", fmt.Sprint(id))
	for _, want := range []string{"file", "update", "user:"} {
		if !strings.Contains(out, want) {
			t.Errorf("history print lost %q:\n%s", want, out)
		}
	}
	// An unknown id answers its OWN refusal — never the empty line an
	// empty history earns.
	if out, err := d.run("triage", "history", "999999"); err == nil {
		t.Fatalf("history of a bug that never was answered empty: %q", out)
	} else if !strings.Contains(err.Error()+out, "does not exist") {
		t.Errorf("the unknown-bug refusal lost its own words: %v (%q)", err, out)
	}
}

func TestTriageVocabularyAndHealthSpeakThePlane(t *testing.T) {
	d := startDaemon(t)
	out := d.mustRun("triage", "vocabulary")
	for _, want := range []string{
		"unvalidated · reproduced · not-reproduced · could-not-tell",
		"known · assumed · unknown",
		"terminal",
		triage.ValidateStatement,
		triage.HandoffStatement,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("vocabulary print lost %q:\n%s", want, out)
		}
	}
	out = d.mustRun("triage", "health")
	for _, want := range []string{"triage.validationBlueprint", "evaluated", "ADVICE, never a gate"} {
		if !strings.Contains(out, want) {
			t.Errorf("health print lost %q:\n%s", want, out)
		}
	}
}

// --- the --yes gates ----------------------------------------------------------

// TestTriageYesGatesWriteNothingWithoutYes gives validate, handoff and
// confirm-duplicate their confirmation-less run: the statement and the
// bug print, a refusal naming --yes comes back, and the store proves
// NOTHING moved — no attempt, no roadmap link, no duplicate link. The
// gate speaks; it does not act.
func TestTriageYesGatesWriteNothingWithoutYes(t *testing.T) {
	d := startDaemon(t)
	repo := queueRepo(t, d, "t6gate")
	vID := triageCLIID(t, d.mustRun("triage", "file", "--component", repo,
		"--title", "gated validate", "--body", "repro: the gate prints, never acts"))
	hID := triageCLIID(t, d.mustRun("triage", "file", "--component", repo,
		"--title", "gated handoff", "--body", "repro: no roadmap item without --yes"))
	dA := triageCLIID(t, d.mustRun("triage", "file", "--component", repo,
		"--title", "duplicate original", "--body", "repro: the first of the pair"))
	dB := triageCLIID(t, d.mustRun("triage", "file", "--component", repo,
		"--title", "duplicate copy", "--body", "repro: the second of the pair"))

	cases := []struct {
		name      string
		args      []string
		statement string
		id        int64
	}{
		{"validate", []string{"triage", "validate", fmt.Sprint(vID)}, triage.ValidateStatement, vID},
		{"handoff", []string{"triage", "handoff", fmt.Sprint(hID)}, triage.HandoffStatement, hID},
	}
	for _, c := range cases {
		out, err := d.run(c.args...)
		if err == nil {
			t.Errorf("%s without --yes returned no error:\n%s", c.name, out)
		} else if !strings.Contains(err.Error(), "--yes") && !strings.Contains(out, "--yes") {
			t.Errorf("%s without --yes: err %v out %q — want a refusal naming the --yes re-run", c.name, err, out)
		}
		if !strings.Contains(out, c.statement) {
			t.Errorf("%s printed no authority statement (or not the daemon's own):\n%s", c.name, out)
		}
		if !strings.Contains(out, "#"+fmt.Sprint(c.id)) {
			t.Errorf("%s printed no bug line for #%d:\n%s", c.name, c.id, out)
		}
	}
	out, err := d.run("triage", "confirm-duplicate", fmt.Sprint(dB), "--of", fmt.Sprint(dA), "--confirmed-by", "carlos")
	if err == nil || !strings.Contains(err.Error(), "--yes") && !strings.Contains(out, "--yes") {
		t.Errorf("confirm-duplicate without --yes: err %v out %q — a person's irreversible link needs the flag", err, out)
	}

	st := openFixtureStoreForCLI(t, d)
	defer st.Close()
	r := triage.New(st)
	for _, id := range []int64{vID, hID, dB} {
		b, ok, err := r.Show(id)
		if err != nil || !ok {
			t.Fatalf("show %d: %v", id, err)
		}
		if b.RoadmapItemID != nil {
			t.Errorf("bug #%d carries a roadmap link after a gated handoff — the gate speaks, it does not act", id)
		}
		if b.DuplicateOf != nil {
			t.Errorf("bug #%d carries a duplicate link after a gated confirm-duplicate", id)
		}
	}
	rows, err := protocol.AnswerTriageHistory(r, protocol.TriageHistoryParams{ID: vID})
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	for _, ch := range rows {
		if ch.Op == "validate" || ch.Op == "handoff" || ch.Op == "duplicate" {
			t.Errorf("the gated bug carries %q on its trail — a gateless moment was written", ch.Op)
		}
	}
}

// --- helpers ----------------------------------------------------------------

var triageIDRe = regexp.MustCompile(`#(\d+)`)

// triageCLIID pulls the id out of a `triage file` print ("#7 · filed…").
func triageCLIID(t *testing.T, out string) int64 {
	t.Helper()
	m := triageIDRe.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no id in the print: %q", out)
	}
	var id int64
	if _, err := fmt.Sscanf(m[1], "%d", &id); err != nil {
		t.Fatalf("id %q: %v", m[1], err)
	}
	return id
}

// stopFixtureDaemon shuts the fixture daemon down and waits for its
// exit — the same pattern S4's restart fixture waits for, for the same
// reason: the endpoint claim and the store's one writer are real locks,
// and a direct door that queues behind a live daemon tests behaviour
// the selection law forbids in production.
func stopFixtureDaemon(t *testing.T, d *daemonFixture) {
	t.Helper()
	if err := d.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Logf("signalling the daemon: %v", err) // already gone is not a failure
	}
	<-d.exited
}

func agreeTriageBugs(t *testing.T, what string, got, want *protocol.TriageView) {
	t.Helper()
	// updatedAt moves with the audit row between the two reads;
	// everything else must agree.
	if got == nil || want == nil {
		t.Fatalf("%s: a nil bug view — got %+v want %+v", what, got, want)
	}
	g, w := *got, *want
	g.UpdatedAt, w.UpdatedAt = "", ""
	if fmt.Sprintf("%+v", g) != fmt.Sprintf("%+v", w) {
		t.Errorf("%s bug disagrees:\n got  %+v\n want %+v", what, g, w)
	}
}
