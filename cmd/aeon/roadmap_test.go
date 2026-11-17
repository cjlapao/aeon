package main

// S26 T5 — the roadmap CLI against a REAL daemon on a temp socket
// (cli_integration_test.go's fixture). The roadmap's laws are decided
// underneath (internal/roadmap, T2); what these tests pin is that this
// secondary client adds NO second truth: the shape refusals fire before
// any dial, the rows are byte-identical to the socket's (P34/CG9's one
// implementation), and the honest words — "unknown", the estimate as a
// range with its basis or not at all — are the ONE renderer's.

import (
	"io"
	"net"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// TestRoadmapTakesNoCredentialFlags is §4 as a habit and P34 grepped in
// product: no credential ever becomes a flag value here, and the CLI
// never opens a second store handle — it DIALS.
func TestRoadmapTakesNoCredentialFlags(t *testing.T) {
	src := readFileForGuard(t, "roadmap.go")
	for _, banned := range []string{`"key"`, `"token"`, `"secret"`, `"apikey"`, `"api-key"`, `"password"`} {
		if strings.Contains(src, banned) {
			t.Errorf("roadmap.go registers credential flag %s — the environment, or a path to a file (§4)", banned)
		}
	}
	for _, banned := range []string{"store.Open", "internal/store"} {
		if strings.Contains(src, banned) {
			t.Errorf("roadmap.go reaches for %s — the roadmap's CLI is a socket client, never a second store handle (P34/CG9)", banned)
		}
	}
}

// TestRoadmapRefusesShapelesslyBeforeDialling pins the CLI's own shape
// gates run BEFORE the socket is dialed: a half estimate, a missing
// title, a missing provenance (D5 has no default) are answered from the
// verb, not from a daemon that may not exist. (No daemon is started here
// on purpose: if any case reached dial(0) it would fail differently.)
func TestRoadmapRefusesShapelesslyBeforeDialling(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"add without a title", []string{"add", "--component", "/r", "--provenance", "authored"}, "title"},
		{"add without provenance (D5)", []string{"add", "--component", "/r", "--title", "t"}, "provenance"},
		{"add with a bare estimate-min (D3)", []string{"add", "--component", "/r", "--title", "t", "--provenance", "authored", "--estimate-min", "2"}, "estimate"},
		{"add with a range and no basis (D3)", []string{"add", "--component", "/r", "--title", "t", "--provenance", "authored", "--estimate-min", "2", "--estimate-max", "5"}, "basis"},
	}
	for _, c := range cases {
		err := cmdRoadmap(io.Discard, c.args)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want a refusal naming %q", c.name, err, c.want)
		}
	}
}

// TestRoadmapShowRefusesAnIdlessCall is peelRoadmapID's gate: the verbs
// that name an item refuse a missing or unparsable id with a sentence,
// never a call with ID 0 against the door.
func TestRoadmapShowRefusesAnIdlessCall(t *testing.T) {
	if err := cmdRoadmap(io.Discard, []string{"show"}); err == nil || !strings.Contains(err.Error(), "id") {
		t.Errorf("show without an id: %v — must refuse naming the id", err)
	}
	if err := cmdRoadmap(io.Discard, []string{"show", "zero"}); err == nil || !strings.Contains(err.Error(), "item id") {
		t.Errorf("show with an unparsable id: %v — must refuse the id, never call with 0", err)
	}
}

// roadmapCLIID extracts the item id from add's first field ("#3 · …").
func roadmapCLIID(t *testing.T, out string) int64 {
	t.Helper()
	f := strings.Fields(out)
	if len(f) == 0 || !strings.HasPrefix(f[0], "#") {
		t.Fatalf("add did not answer with the id first: %q", out)
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(f[0], "#"), 10, 64)
	if err != nil {
		t.Fatalf("id: %v", err)
	}
	return id
}

// TestRoadmapSocketAndCLIProduceIdenticalRows is the same proof S24 made
// for the queue, for the same reason: the CLI is a SECONDARY CLIENT of
// the socket surface (P34), and the two doors must not diverge row-wise.
// The same add through both doors against ONE store: ID and the two
// timestamps are normalised (two calls, one clock — equality would be a
// race pinned as a gate; their PRESENCE is asserted); every other column
// is byte-identical.
func TestRoadmapSocketAndCLIProduceIdenticalRows(t *testing.T) {
	d := startDaemon(t)
	// queueRepo is the package's generic "component whose root is a fresh
	// directory" seeder — the name is queue's, the helper is not.
	repo := queueRepo(t, d, "rmident")
	conn, err := net.Dial("unix", d.endpoint)
	if err != nil {
		t.Fatalf("connect to the fixture daemon: %v", err)
	}
	defer conn.Close()
	c := protocol.NewClient(conn, ClientName, Version)
	if _, err := c.Connect(0); err != nil {
		t.Fatalf("connect handshake: %v", err)
	}
	var sockRes protocol.RoadmapItemResult
	if err := c.Call(protocol.MethodRoadmapAdd, protocol.RoadmapAddParams{
		ComponentRoot: repo, Title: "ship the export verb", Epic: "billing",
		PriorityState: "known", PriorityLabel: "P1", Provenance: "authored",
		EstimateMin: ptrInt(1), EstimateMax: ptrInt(3), EstimateBasis: "three sessions, sized against plan.md",
	}, &sockRes); err != nil {
		t.Fatalf("socket add: %v", err)
	}
	out := d.mustRun("roadmap", "add", "--component", repo,
		"--title", "ship the export verb", "--epic", "billing",
		"--priority", "known", "--priority-label", "P1",
		"--provenance", "authored",
		"--estimate-min", "1", "--estimate-max", "3",
		"--estimate-basis", "three sessions, sized against plan.md")
	cliID := roadmapCLIID(t, out)

	s, err := store.Open(store.Options{Dir: filepath.Join(d.stateDir, "aeon"), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("open the store: %v", err)
	}
	defer s.Close()
	a, okA, err := s.RoadmapItemByID(sockRes.Item.ID)
	if err != nil || !okA {
		t.Fatalf("socket row missing: %v %v", okA, err)
	}
	b, okB, err := s.RoadmapItemByID(cliID)
	if err != nil || !okB {
		t.Fatalf("CLI row missing: %v %v", okB, err)
	}
	if a.CreatedAt == "" || b.CreatedAt == "" {
		t.Fatal("a row was stamped nothing")
	}
	a.ID, b.ID = 0, 0
	a.CreatedAt, b.CreatedAt, a.UpdatedAt, b.UpdatedAt = "", "", "", ""
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("the two doors wrote different rows — P34's one implementation is two:\nsocket: %+v\n  cli: %+v", a, b)
	}
}

func ptrInt(v int) *int { return &v }

// TestRoadmapPrintsTheHonestWords pins D2/D3 on the CLI's ONE renderer
// through the real binary: a never-prioritized item says the WORD
// "unknown" (never blank, never a zero), and a never-sized item carries
// no range at all — not "0–0", not an empty parenthesis.
func TestRoadmapPrintsTheHonestWords(t *testing.T) {
	d := startDaemon(t)
	repo := queueRepo(t, d, "rmhonest")
	out := d.mustRun("roadmap", "add", "--component", repo,
		"--title", "an idea without a rank", "--provenance", "authored")
	if !strings.Contains(out, "unknown") {
		t.Errorf("a never-prioritized item printed %q — the word `unknown` is D2's honest state", out)
	}
	if strings.Contains(out, "0–0") || strings.Contains(out, " ()") {
		t.Errorf("a never-sized item printed a fake range: %q — D3 is all-three-or-none", out)
	}
	if !strings.Contains(out, "from authored") {
		t.Errorf("provenance vanished from the line: %q — D5 says the writer answers where it came from", out)
	}
	// And list: two rows, each on its own line, header or not — the
	// count sentence must be there for the empty board (D8's honesty).
	empty := d.mustRun("roadmap", "list", "--component", filepath.Join(repo, "-no-such-project"))
	if !strings.Contains(empty, "is EMPTY") {
		t.Errorf("an empty board printed %q — D8: the empty answer is said, not silent", empty)
	}
	// The bytes the CLI prints for the board must include the one item.
	board := d.mustRun("roadmap", "list", "--component", repo)
	if !strings.Contains(board, "an idea without a rank") {
		t.Errorf("the board missed the row: %q", board)
	}
}

// TestRoadmapCancelAndEstimateRoundTripThroughTheSocket exercises the
// remaining CLI verbs against the live daemon through mustRun — the
// estimate sentence and the cancel sentence are the surface's promise.
func TestRoadmapCancelAndEstimateRoundTripThroughTheSocket(t *testing.T) {
	d := startDaemon(t)
	repo := queueRepo(t, d, "rmverbs")
	out := d.mustRun("roadmap", "add", "--component", repo,
		"--title", "size me then cancel me", "--provenance", "authored")
	id := roadmapCLIID(t, out)
	sized := d.mustRun("roadmap", "estimate", strconv.FormatInt(id, 10),
		"--min", "2", "--max", "4", "--basis", "measured against the last slice")
	if !strings.Contains(sized, "2–4") || !strings.Contains(sized, "measured against the last slice") {
		t.Errorf("the estimate did not print as its full sentence: %q", sized)
	}
	prio := d.mustRun("roadmap", "priority", strconv.FormatInt(id, 10), "--state", "assumed", "--label", "soon")
	if !strings.Contains(prio, "assumed") || !strings.Contains(prio, "soon") {
		t.Errorf("the priority did not print as its word and label: %q", prio)
	}
	cancelled := d.mustRun("roadmap", "cancel", strconv.FormatInt(id, 10))
	if !strings.Contains(cancelled, "cancelled") {
		t.Errorf("cancel did not say so: %q", cancelled)
	}
	// Cancel is idempotent (the machine's own rule: a terminal rung
	// re-cancelling answers the same row, it does not refuse and it does
	// not delete — D7). The CLI says so twice, identically.
	again := d.mustRun("roadmap", "cancel", strconv.FormatInt(id, 10))
	if !strings.Contains(again, "cancelled") {
		t.Errorf("the second cancel was not the same honest sentence: %q", again)
	}
}
