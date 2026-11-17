package main

// S52 T5 — the parity record between the two memory doors and the gates
// the retraction verbs hold. Five S28 verbs ride the socket alone (their
// prints and refusals do not move — the door table names them, direct:
// false); six S52 verbs ride BOTH doors (the retract law, the board, the
// history, the agent's readable set, the vocabulary). Guards: the direct
// door copies no logic (every method is one call into
// daemon.NewLearningsManageDoors), the offline stamp is {cli, cli}, the
// selection refuses rather than routing around a dial that merely failed,
// and the --yes gates print and write NOTHING without the confirmation —
// the retract law and the row first, the act only after (S52 LM7/LM8).
// learnings.mine.* has no CLI verb at all (LM1 — the agent's surface; the
// person's view of an agent's memory is `for-agent`, and it answers the
// person's learnings.forAgent door, not the agent's twin).

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/learnings"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// learningsDoorSpec is one row of T5's door table.
type learningsDoorSpec struct {
	verb   string
	method protocol.MethodName
	direct bool // the direct door answers this verb offline
}

// learningsDoorTable is the fixture: every learnings verb, the exact road
// it takes. It belongs to this test alone — no production code reads it.
var learningsDoorTable = []learningsDoorSpec{
	{"add", protocol.MethodLearningsAdd, false},
	{"list", protocol.MethodLearningsList, false},
	{"show", protocol.MethodLearningsShow, false},
	{"query", protocol.MethodLearningsQuery, false},
	{"embed", protocol.MethodLearningsEmbed, false},
	{"retract", protocol.MethodLearningsRetract, true},
	{"restore", protocol.MethodLearningsRestore, true},
	{"board", protocol.MethodLearningsBoard, true},
	{"history", protocol.MethodLearningsHistory, true},
	{"vocabulary", protocol.MethodLearningsVocabulary, true},
	{"for-agent", protocol.MethodLearningsForAgent, true},
}

// TestLearningsDoorTableIsTheTable walks the table in BOTH directions:
// every verb's method is registered on a wired daemon and
// RequiresInitialize, and every learnings.* method a wired daemon
// registers has a verb — learnings.mine.* named as the exemption it is.
func TestLearningsDoorTableIsTheTable(t *testing.T) {
	d := blueprintGuardDaemon(t)
	for _, row := range learningsDoorTable {
		got, ok := d.Table.Lookup(row.method)
		if !ok {
			t.Errorf("learnings %s speaks %s, which is not registered — the verb would have no door", row.verb, row.method)
			continue
		}
		if !got.RequiresInitialize {
			t.Errorf("learnings %s: %s does not RequireInitialize", row.verb, row.method)
		}
	}
	inTable := map[protocol.MethodName]bool{}
	for _, row := range learningsDoorTable {
		inTable[row.method] = true
	}
	// learnings.mine.* are the AGENT's doors (LM1): a person holding an
	// offline store is not an agent with a step, and the person's view of
	// an agent's memory is `for-agent`. Named, not silent.
	noVerb := map[protocol.MethodName]string{
		protocol.MethodLearningsMap:                "the app's knowledge map: 2-D coordinates have no terminal rendering; the terminal's read is board",
		protocol.MethodLearningsMineQuery:          "agent readable-set browse (LM1); the person's read is learnings.forAgent",
		protocol.MethodLearningsMineShow:           "agent by-id read (LM1)",
		protocol.MethodLearningsMineAdd:            "agent self-log (LM1); the person's door is learnings.add",
		protocol.MethodLearningsMineProposeRetract: "agent proposal filing (LM1); the person's door is learnings.retract",
	}
	for _, m := range d.Table.Methods() {
		if !strings.HasPrefix(string(m), "learnings.") {
			continue
		}
		if _, exempt := noVerb[m]; exempt {
			continue
		}
		if !inTable[m] {
			t.Errorf("registered method %s has no CLI verb in learningsDoorTable — every door speaks a verb", m)
		}
	}
}

// --- selectLearningsDoor ------------------------------------------------------

func TestSelectLearningsDoorFallsBackOnlyWhenNoDaemon(t *testing.T) {
	restore := learningsDial
	t.Cleanup(func() { learningsDial = restore })

	// (a) dial answers → socket door, no notice.
	var errOut bytes.Buffer
	learningsDial = func(int) (*protocol.Client, error) { return &protocol.Client{}, nil }
	if door, err := selectLearningsDoor(&errOut); err != nil || !strings.Contains(fmt.Sprintf("%T", door), "SocketDoor") || errOut.Len() != 0 {
		t.Fatalf("dial success: door %T err %v notice %q — want the socket door and no notice", door, err, errOut.String())
	}

	// (b) no endpoint → direct door WITH the fixed line, byte for byte.
	errOut.Reset()
	learningsDial = func(int) (*protocol.Client, error) {
		return nil, fmt.Errorf("no daemon at /nonexistent/aeon.sock: %w", errors.New("connect: no such file or directory"))
	}
	door, err := selectLearningsDoor(&errOut)
	if err != nil || !strings.Contains(fmt.Sprintf("%T", door), "DirectDoor") {
		t.Fatalf("no daemon: door %T err %v — want the direct door", door, err)
	}
	if errOut.String() != learningsDirectNotice+"\n" {
		t.Errorf("notice = %q, want the fixed line %q plus a newline", errOut.String(), learningsDirectNotice)
	}

	// (c) any OTHER dial failure → refusal, no door, no notice.
	errOut.Reset()
	learningsDial = func(int) (*protocol.Client, error) { return nil, errors.New("dial refused by the endpoint") }
	if door, err := selectLearningsDoor(&errOut); err == nil || door != nil || errOut.Len() != 0 {
		t.Fatalf("failed dial: door %T err %v notice %q — the CLI never routes around a daemon that merely failed", door, err, errOut.String())
	}
}

// TestLearningsDirectDoorHasNoLogicCopy reads learnings_client.go for the
// claim: the direct arm opens the store, hands it to the daemon's door
// constructor, and calls. It holds no stamping rule, no filter, no refusal
// sentence of its own.
func TestLearningsDirectDoorHasNoLogicCopy(t *testing.T) {
	src := readFileForGuard(t, "learnings_client.go")
	i := strings.Index(src, "// --- the direct door")
	if i < 0 {
		t.Fatal("learnings_client.go lost the direct door section — the guard reads it by section")
	}
	arm := src[i:]
	for _, banned := range []string{"learnings.New(", "does not exist", "internal/store", "UPDATE ", "ErrLearning", "RetractedAt ="} {
		if strings.Contains(arm, banned) {
			t.Errorf("the direct door contains %q — the offline arm must call the daemon's answers, not compute them (no logic copy)", banned)
		}
	}
	for _, required := range []string{"openStoreDirectly(false)", "daemon.NewLearningsManageDoors", "protocol.AnswerLearningsVocabulary()", "dr.Retract(ctx, actor, p)", "dr.ForAgent(ctx, p)"} {
		if !strings.Contains(arm, required) {
			t.Errorf("the direct door no longer reads %q — its one-call shape is what makes a copy impossible", required)
		}
	}
}

// TestLearningsTakesNoCredentialFlagsCoversThePlane extends the standing
// guard to the files S52 adds: no credential ever becomes a flag value,
// and the store reach stays confined to learnings_client.go's direct door.
// The five S28 verbs keep their socket-only roads: the source says so.
func TestLearningsTakesNoCredentialFlagsCoversThePlane(t *testing.T) {
	for _, f := range []string{"learnings.go", "learnings_client.go"} {
		src := readFileForGuard(t, f)
		for _, banned := range []string{`"key"`, `"token"`, `"secret"`, `"apikey"`, `"api-key"`, `"password"`} {
			if strings.Contains(src, banned) {
				t.Errorf("%s registers credential flag %s — the environment, or a path to a file (§4)", f, banned)
			}
		}
	}
	src := readFileForGuard(t, "learnings.go")
	for _, banned := range []string{"store.Open", "internal/store"} {
		if strings.Contains(src, banned) {
			t.Errorf("learnings.go reaches for %q — the memory CLI's store reach is confined to learnings_client.go's direct door (P34), and add must stay on its S28 socket road", banned)
		}
	}
	client := readFileForGuard(t, "learnings_client.go")
	if !strings.Contains(client, "openStoreDirectly") {
		t.Error("learnings_client.go no longer opens the store — if the offline door moved, THIS guard must move with it, not silently pass")
	}
	// The five S28 verbs dial directly: their bytes and roads do not move
	// (their prints are S28's; T5 adds doors, it does not re-road goldens).
	for _, m := range []string{"c.Call(protocol.MethodLearningsAdd", "c.Call(protocol.MethodLearningsList", "c.Call(protocol.MethodLearningsQuery", "c.Call(protocol.MethodLearningsEmbed"} {
		if !strings.Contains(src, m) {
			t.Errorf("learnings.go no longer contains %q — the S28 verb left its socket-only road", m)
		}
	}
}

// --- two doors, one answer ---------------------------------------------------

// TestLearningsRetractRefusalVerbatimBothDoors gives the socket and the
// direct door the SAME illegal retraction (a 257-byte reason — LM2's byte
// law decided underneath) and demands the SAME sentence out, byte for byte,
// mapped through the CLI's own verbatim mapper on each road. The unknown id
// rides the same pair: its own error, not a softened one.
func TestLearningsRetractRefusalVerbatimBothDoors(t *testing.T) {
	d := startDaemon(t)
	id := learningsCLIID(t, d.mustRun("learnings", "add", "--scope", "person", "--scope-key", "t5refuse",
		"--body", "a memory to refuse to retract badly", "--provenance", "told"))
	long := strings.Repeat("x", 257)

	c := learningsClient(t, d)
	var sock protocol.LearningsActResult
	sockErr := c.Call(protocol.MethodLearningsRetract, protocol.LearningsRetractParams{ID: id, Reason: long}, &sock)
	if sockErr == nil {
		t.Fatal("the socket accepted a 257-character reason — LM2's law moved up the stack")
	}
	steerDirectDoorToFixture(t, d)
	_, directErr := learningsDirectDoor{}.retract(context.Background(), learningsCLIActor,
		protocol.LearningsRetractParams{ID: id, Reason: long})
	if directErr == nil {
		t.Fatal("the direct door accepted a 257-character reason — the offline road re-validates nothing, including nothing refused")
	}
	want, got := learningsVerbatim(sockErr).Error(), learningsVerbatim(directErr).Error()
	if want != got {
		t.Errorf("the two roads refuse differently:\n socket: %s\n direct: %s", want, got)
	}
	if !strings.Contains(want, "256") {
		t.Errorf("the shared refusal %q does not name the bound it enforced", want)
	}

	// The id that never was: its OWN error, the same on both roads.
	var sock2 protocol.LearningsActResult
	sockErr2 := c.Call(protocol.MethodLearningsRetract, protocol.LearningsRetractParams{ID: 999999, Reason: "honest reason"}, &sock2)
	_, directErr2 := learningsDirectDoor{}.retract(context.Background(), learningsCLIActor,
		protocol.LearningsRetractParams{ID: 999999, Reason: "honest reason"})
	if sockErr2 == nil || directErr2 == nil {
		t.Fatalf("a retraction of a row that never was answered nil: socket %v direct %v", sockErr2, directErr2)
	}
	if w2, g2 := learningsVerbatim(sockErr2).Error(), learningsVerbatim(directErr2).Error(); w2 != g2 {
		t.Errorf("the unknown-id refusals disagree:\n socket: %s\n direct: %s", w2, g2)
	} else if !strings.Contains(w2, "does not exist") {
		t.Errorf("the unknown-id refusal lost its own words: %s", w2)
	}
}

// TestLearningsDirectActorIsCli walks the offline retract and reads the
// audit line it must leave: the actor is {cli, cli}, named, not guessed —
// learning_change's CHECK admitted 'cli' the day T1 drew it, FOR this door
// (LM3 bounds the agent kind, never this one).
func TestLearningsDirectActorIsCli(t *testing.T) {
	d := startDaemon(t)
	id := learningsCLIID(t, d.mustRun("learnings", "add", "--scope", "person", "--scope-key", "t5actor",
		"--body", "an offline retraction lands", "--provenance", "told"))
	// The direct door answers when there is NO daemon — the honest
	// production shape (P5). The fixture daemon is stopped before the
	// offline write: a live daemon's writer holds the WAL for a settling
	// moment, and a door that writes beside it tests a state selection can
	// never produce.
	stopFixtureDaemon(t, d)
	steerDirectDoorToFixture(t, d)
	res, err := learningsDirectDoor{}.retract(context.Background(), learningsCLIActor,
		protocol.LearningsRetractParams{ID: id, Reason: "wrong in the field"})
	if err != nil {
		t.Fatalf("direct retract: %v", err)
	}
	if !res.Changed || !res.Learning.Retracted {
		t.Fatalf("direct retract answered %+v — want the stamp on", res)
	}
	rows, err := learningsDirectDoor{}.history(context.Background(), protocol.LearningsHistoryParams{ID: id})
	if err != nil {
		t.Fatalf("direct history: %v", err)
	}
	var seen bool
	for _, ch := range rows {
		if ch.Op == "retract" {
			seen = true
			if ch.ActorKind != "cli" || ch.ActorID != "cli" {
				t.Errorf("offline retract audited by %s:%s — the direct door stamps {cli, cli} and says so", ch.ActorKind, ch.ActorID)
			}
		}
	}
	if !seen {
		t.Fatal("no retract line on the trail after an offline retraction")
	}
	// And the offline restore puts the memory back, still under {cli, cli}.
	back, err := learningsDirectDoor{}.restore(context.Background(), learningsCLIActor,
		protocol.LearningsRestoreParams{ID: id})
	if err != nil || !back.Changed || back.Learning.Retracted {
		t.Fatalf("direct restore: %+v err %v", back, err)
	}
}

// TestLearningsFourReadsSpeakByteIdenticalThroughBothDoors runs history,
// board, vocabulary and forAgent through both doors against ONE state and
// compares answers — no clock field needs normalising, because none of
// these answers carries a now(): every instant on them was written, not
// observed.
func TestLearningsFourReadsSpeakByteIdenticalThroughBothDoors(t *testing.T) {
	d := seedAtlasDaemon(t)
	id := learningsCLIID(t, d.mustRun("learnings", "add", "--scope", "person", "--scope-key", "t5parity",
		"--body", "parity memory", "--provenance", "observed", "--observed-count", "3", "--tag", "ledger"))

	c := learningsClient(t, d)
	steerDirectDoorToFixture(t, d)
	direct := learningsDirectDoor{}

	for _, step := range []struct {
		name   string
		method protocol.MethodName
		params any
		call   func() (any, error)
	}{
		{"history", protocol.MethodLearningsHistory, protocol.LearningsHistoryParams{ID: id}, func() (any, error) {
			return direct.history(context.Background(), protocol.LearningsHistoryParams{ID: id})
		}},
		{"board", protocol.MethodLearningsBoard, protocol.LearningsBoardParams{IncludeRetracted: true}, func() (any, error) {
			return direct.board(context.Background(), protocol.LearningsBoardParams{IncludeRetracted: true})
		}},
		{"vocabulary", protocol.MethodLearningsVocabulary, struct{}{}, func() (any, error) {
			return direct.vocabulary(context.Background())
		}},
		{"forAgent", protocol.MethodLearningsForAgent, protocol.LearningsForAgentParams{Agent: "atlas"}, func() (any, error) {
			return direct.forAgent(context.Background(), protocol.LearningsForAgentParams{Agent: "atlas"})
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
		// The socket answers the RESULT object; the direct arm answers the
		// slice or value the CLI prints — compare through the result shape.
		switch step.name {
		case "board":
			var r protocol.LearningsBoardResult
			if err := json.Unmarshal(raw, &r); err != nil {
				t.Fatal(err)
			}
			if raw, err = json.Marshal(r.Learnings); err != nil {
				t.Fatal(err)
			}
		case "history":
			var r protocol.LearningsHistoryResult
			if err := json.Unmarshal(raw, &r); err != nil {
				t.Fatal(err)
			}
			if raw, err = json.Marshal(r.Changes); err != nil {
				t.Fatal(err)
			}
		case "forAgent":
			var r protocol.LearningsForAgentResult
			if err := json.Unmarshal(raw, &r); err != nil {
				t.Fatal(err)
			}
			if raw, err = json.Marshal(r.Hits); err != nil {
				t.Fatal(err)
			}
		}
		if !jsonEquivalent(t, raw, offRaw) {
			t.Errorf("%s: socket %s, direct %s — the two doors must answer the same bytes", step.name, raw, offRaw)
		}
	}

	// retract, both doors, one stamp: the socket's retraction changes, the
	// direct door replaying it changes nothing (changed:false IS the
	// parity of a no-op), and both answers agree byte for byte. The
	// daemon is stopped before the offline replay — a WRITE on the direct
	// road beside a live daemon is the race P5's selection can never
	// produce, and the plane's own locking says so.
	var sock protocol.LearningsActResult
	if err := c.Call(protocol.MethodLearningsRetract, protocol.LearningsRetractParams{ID: id, Reason: "parity retract"}, &sock); err != nil {
		t.Fatalf("socket retract: %v", err)
	}
	stopFixtureDaemon(t, d)
	off, err := direct.retract(context.Background(), learningsCLIActor, protocol.LearningsRetractParams{ID: id, Reason: "parity retract"})
	if err != nil {
		t.Fatalf("direct replay retract: %v", err)
	}
	if !sock.Changed || off.Changed {
		t.Errorf("changed flags disagree: socket %v direct %v — the stamp changed, the echo of it must not", sock.Changed, off.Changed)
	}
	sockRow, err := json.Marshal(sock.Learning)
	if err != nil {
		t.Fatal(err)
	}
	offRow, err := json.Marshal(off.Learning)
	if err != nil {
		t.Fatal(err)
	}
	if !jsonEquivalent(t, sockRow, offRow) {
		t.Errorf("the row as it stands disagrees after the stamp:\n socket: %s\n direct: %s", sockRow, offRow)
	}
}

// --- the verbs print what the daemon said -----------------------------------

func TestLearningsBoardAndForAgentPrintTheDaemonsWords(t *testing.T) {
	d := seedAtlasDaemon(t)
	learningsCLIID(t, d.mustRun("learnings", "add", "--scope", "person", "--scope-key", "t5print",
		"--body", "a board row with a describe", "--provenance", "assumed"))

	// board --json is the door's bytes and nothing else.
	out := d.mustRun("learnings", "board", "--scope", "person", "--json")
	var rows []protocol.LearningsBoardRow
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &rows); err != nil {
		t.Fatalf("board --json is not the door's bytes and nothing else: %v (%q)", err, out)
	}
	if len(rows) != 1 || rows[0].Describe == "" {
		t.Fatalf("board --json rows: %+v", rows)
	}
	// The human print carries the daemon's ONE Describe sentence verbatim
	// (LM6 — no CLI paraphrase of the provenance claim).
	out = d.mustRun("learnings", "board", "--scope", "person")
	if !strings.Contains(out, rows[0].Describe) || !strings.Contains(out, "a board row with a describe") {
		t.Fatalf("board human print lost the daemon's sentence or the body:\n%s", out)
	}

	// LM6's filters are EQUALITY filters in the plane (T2's Board): a word
	// outside the closed set matches nothing and earns the board's honest
	// empty sentence — the filter's answer, never a coercion and never a
	// CLI-side re-validation of a word the plane itself owns.
	if out, err := d.run("learnings", "board", "--provenance", "banana"); err != nil ||
		!strings.Contains(out, "EMPTY") || strings.Contains(out, "a board row") {
		t.Fatalf("an out-of-set filter word did not earn the board's empty answer: %v (%q)", err, out)
	}

	// for-agent answers a question about ONE agent and prints the why.
	d.mustRun("learnings", "add", "--scope", "agent", "--scope-key", "atlas",
		"--body", "atlas learned it the hard way", "--provenance", "observed", "--observed-count", "2")
	out = d.mustRun("learnings", "for-agent", "atlas")
	if !strings.Contains(out, "atlas learned it the hard way") || !strings.Contains(out, "why: rank") {
		t.Fatalf("for-agent print lost the row or the why column:\n%s", out)
	}
	out = d.mustRun("learnings", "for-agent", "atlas", "--json")
	var hits []protocol.LearningsForAgentHit
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &hits); err != nil {
		t.Fatalf("for-agent --json is not the door's bytes and nothing else: %v (%q)", err, out)
	}
	if len(hits) == 0 {
		t.Fatal("for-agent --json came back empty — the readable set includes the agent's own rows")
	}
	// A slug that is not an agent is refused BY NAME (LM1's authority: the
	// slug is the identity; a name is not a key here) — a refusal, never a
	// confident empty board for a reader that never was.
	if out, err := d.run("learnings", "for-agent", "nobody-at-all"); err == nil {
		t.Fatalf("for-agent of a slug that is no agent answered: %q", out)
	} else if !strings.Contains(err.Error()+out, "nobody-at-all") {
		t.Errorf("the unknown-agent refusal does not name the slug it refused: %v (%q)", err, out)
	}

	// A retracted row shows on the board FLAGGED with --include-retracted
	// (LM2: never silently) and not at all without it.
	rid := learningsCLIID(t, d.mustRun("learnings", "add", "--scope", "person", "--scope-key", "t5print",
		"--body", "doomed memory", "--provenance", "told"))
	d.mustRun("learnings", "retract", strconv.FormatInt(rid, 10), "--reason", "wrong in the field", "--yes")
	out = d.mustRun("learnings", "board", "--scope", "person")
	if strings.Contains(out, "doomed memory") {
		t.Fatalf("a retracted row rode the default board (LM2 violation):\n%s", out)
	}
	out = d.mustRun("learnings", "board", "--scope", "person", "--include-retracted")
	if !strings.Contains(out, "doomed memory") || !strings.Contains(out, "RETRACTED") {
		t.Fatalf("the flagged board lost the row or the flag:\n%s", out)
	}

	// history prints the career the audit holds: the add and the retract,
	// each with its actor.
	out = d.mustRun("learnings", "history", strconv.FormatInt(rid, 10))
	for _, want := range []string{"add", "retract", "user:"} {
		if !strings.Contains(out, want) {
			t.Errorf("history print lost %q:\n%s", want, out)
		}
	}
	// An unknown id answers its OWN refusal — never the empty line an
	// empty career earns.
	if out, err := d.run("learnings", "history", "999999"); err == nil {
		t.Fatalf("history of a memory that never was answered empty: %q", out)
	} else if !strings.Contains(err.Error()+out, "no row answers to that id") {
		t.Errorf("the unknown-id refusal lost its own words: %v (%q)", err, out)
	}
}

func TestLearningsVocabularyPrintsTheSets(t *testing.T) {
	d := startDaemon(t)
	out := d.mustRun("learnings", "vocabulary")
	// The provenances print as their OWN words — five, distinct, none
	// collapsed into a score (M1/M3 on the human print).
	for _, want := range []string{
		"person", "project", "agent",
		"told", "observed", "assumed", "relayed",
		"user", "cli", "system",
		learnings.M1OrderingNote,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("vocabulary print lost %q:\n%s", want, out)
		}
	}
	voc := protocol.AnswerLearningsVocabulary()
	if !strings.Contains(out, voc.RetractLaw) {
		t.Errorf("vocabulary print lost the retract law verbatim:\n%s", out)
	}
	// The FOUR provenances print as distinct words — the door's own join,
	// none collapsed into a score (M3's strength keeps its own line).
	if !strings.Contains(out, strings.Join(voc.Provenances, " · ")) {
		t.Errorf("the provenance line is not the door's set printed word for word:\n%s", out)
	}
}

// --- the --yes gates ----------------------------------------------------------

// TestLearningsYesGatesWriteNothingWithoutYes gives retract and restore
// their confirmation-less run: the retract law and the row print, a
// refusal naming --yes comes back, and the store proves NOTHING moved —
// no stamp, no audit line. The gate speaks; it does not act (LM7/LM8).
func TestLearningsYesGatesWriteNothingWithoutYes(t *testing.T) {
	d := startDaemon(t)
	out := d.mustRun("learnings", "add", "--scope", "person", "--scope-key", "t5gate",
		"--body", "gated memory", "--provenance", "told")
	id := learningsCLIID(t, out)
	idStr := strconv.FormatInt(id, 10)
	voc := protocol.AnswerLearningsVocabulary()

	out, err := d.run("learnings", "retract", idStr, "--reason", "no confirmation")
	if err == nil {
		t.Fatalf("retract without --yes wrote: %q", out)
	}
	if !strings.Contains(out, voc.RetractLaw) || !strings.Contains(out, "gated memory") {
		t.Fatalf("the retract gate printed without the law or the row:\n%s", out)
	}
	if !strings.Contains(err.Error()+out, "--yes") {
		t.Errorf("the gate refusal does not name --yes: %v (%q)", err, out)
	}
	// The store says nothing moved: no stamp on the flagged board, no
	// retract line on the career. (The S28 `show` print is unchanged by T5
	// — the retraction state rides the flagged board and the wire view.)
	if out := d.mustRun("learnings", "board", "--include-retracted"); strings.Contains(out, "RETRACTED") {
		t.Fatalf("the gate retracted anyway:\n%s", out)
	}
	hist := d.mustRun("learnings", "history", idStr)
	if strings.Contains(hist, "retract") {
		t.Fatalf("the gate left an audit line:\n%s", hist)
	}

	// The act, on --yes: the stamp lands, the audit names the peer's kind,
	// and the replay is LM2's no-echo.
	out = d.mustRun("learnings", "retract", idStr, "--reason", "confirmed wrong", "--yes")
	if !strings.Contains(out, "retracted.") {
		t.Fatalf("confirmed retract print: %q", out)
	}
	out = d.mustRun("learnings", "retract", idStr, "--reason", "confirmed wrong", "--yes")
	if !strings.Contains(out, "already retracted") || !strings.Contains(out, "changed:false") {
		t.Fatalf("the idempotent replay must say so:\n%s", out)
	}

	// restore, gated the same way.
	out, err = d.run("learnings", "restore", idStr)
	if err == nil || !strings.Contains(err.Error()+out, "--yes") {
		t.Fatalf("restore without --yes must print the gate and refuse: %v (%q)", err, out)
	}
	if !strings.Contains(d.mustRun("learnings", "board", "--include-retracted"), "RETRACTED") {
		t.Fatal("the gated restore restored anyway — the row lost its stamp before --yes was spoken")
	}
	out = d.mustRun("learnings", "restore", idStr, "--yes")
	if !strings.Contains(out, "restored.") {
		t.Fatalf("confirmed restore print: %q", out)
	}
	hist = d.mustRun("learnings", "history", idStr)
	for _, want := range []string{"retract", "restore"} {
		if !strings.Contains(hist, want) {
			t.Errorf("the career lost %q after the gated pair:\n%s", want, hist)
		}
	}
}

// --- helpers -------------------------------------------------------------------

// learningsClient dials the fixture daemon's socket the way production
// does — a real connection, the real handshake.
func learningsClient(t *testing.T, d *daemonFixture) *protocol.Client {
	t.Helper()
	conn, err := net.Dial("unix", d.endpoint)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	c := protocol.NewClient(conn, ClientName, Version)
	if _, err := c.Connect(0); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	return c
}

// learningsCLIID lifts the leading "#N" off a print (every memory print
// leads with the id it earned).
func learningsCLIID(t *testing.T, out string) int64 {
	t.Helper()
	if !strings.HasPrefix(out, "#") {
		t.Fatalf("learnings print does not lead with an id: %q", out)
	}
	end := strings.Index(out, " ")
	if end <= 1 {
		t.Fatalf("learnings print lost its id line: %q", out)
	}
	id, err := strconv.ParseInt(out[1:end], 10, 64)
	if err != nil {
		t.Fatalf("learnings print id %q: %v", out[1:end], err)
	}
	return id
}

// seedAtlasDaemon stands up the agent row the readable set needs (LM1's
// append-time authority: an agent-scoped write names a real agent) in the
// state directory BEFORE the fixture daemon opens it — the S55 T11 seed-
// before-start pattern, which is the honest shape: writing through a second
// handle while a live daemon holds the WAL writer is the race the direct
// door's law (writes offline only) exists to avoid.
func seedAtlasDaemon(t *testing.T) *daemonFixture {
	t.Helper()
	stateDir := t.TempDir()
	st, err := store.Open(store.Options{Dir: filepath.Join(stateDir, "aeon"), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("open the pre-seed store: %v", err)
	}
	if err := st.CreateAgentAudited(context.Background(), store.AgentActor{Kind: store.AgentActorUser, ID: "1000"}, store.Agent{Name: "Atlas", Slug: "atlas", Description: "d", Prompt: "p", Version: "1"}, "2026-11-04T00:00:00Z"); err != nil {
		st.Close()
		t.Fatalf("seed atlas: %v", err)
	}
	st.Close()
	return startDaemonInStateDir(t, stateDir)
}
