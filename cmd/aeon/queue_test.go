package main

// S24 T6 — the queue CLI against a REAL daemon on a temp socket
// (cli_integration_test.go's fixture): every gate below dials the same
// daemon the app would, through the built binary. The queue's laws are
// decided underneath (internal/queue, T2/T5/T7); what these tests pin is
// that this surface adds NO second truth: identical refusal sentences,
// round-tripped settings, remove-that-cancels, and no credential ever
// reaches a flag (AGENTS §4 as a matter of habit).

import (
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// mkdirAll0700 / readFileForGuard — the two small hands this file's gates
// need (0700 because the daemon must see the directory and nothing more).
func mkdirAll0700(dir string) error { return os.MkdirAll(dir, 0o700) }

func readFileForGuard(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

// queueRepo seeds a component whose root is a fresh directory, and returns
// it — the queue keys ride the component row that owns roots.
func queueRepo(t *testing.T, d *daemonFixture, name string) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := mkdirAll0700(repo); err != nil {
		t.Fatal(err)
	}
	seedComponent(t, d.stateDir, name, repo)
	return repo
}

// queueID extracts the item id from `append`'s first field ("3 queued · …").
func queueID(t *testing.T, out string) string {
	t.Helper()
	f := strings.Fields(out)
	if len(f) < 2 || f[1] != "queued" {
		t.Fatalf("append did not answer with the id first: %q", out)
	}
	return f[0]
}

func TestQueueAppendRejectsUnknownBlueprintTheSameWayLoopStartDoes(t *testing.T) {
	d := startDaemon(t)
	repo := queueRepo(t, d, "qgate1")

	outQ, codeQ := runExit(t, d, "queue", "append", "--component", repo,
		"--blueprint", "nope-not-a-thing", "--goal", "a goal nobody can build")
	if codeQ == 0 {
		t.Fatalf("an unknown blueprint was accepted: %q", outQ)
	}
	// The SAME name through loop.start — the equality the whole no-drift
	// argument rests on is byte-equality of the sentences, both rendered
	// by the same error printer.
	outL, codeL := runExit(t, d, "loop", "start", "--blueprint", "nope-not-a-thing",
		"--component", repo, "a goal nobody can build")
	if codeL == 0 {
		t.Fatal("loop.start accepted a name that does not exist — the fixture lied")
	}
	sentence := func(out string) string {
		for _, line := range strings.Split(out, "\n") {
			if strings.Contains(line, "nope-not-a-thing") {
				return strings.TrimSpace(line)
			}
		}
		t.Fatalf("neither door named the blueprint for %q / %q", outQ, outL)
		return ""
	}
	sQ, sL := sentence(outQ), sentence(outL)
	if sQ != sL {
		t.Fatalf("the two doors drifted on what counts as a valid blueprint:\n queue: %s\n loop:  %s", sQ, sL)
	}
	// The refused append wrote nothing: the list is empty, not haunted.
	list := d.mustRun("queue", "list", "--component", repo)
	if !strings.Contains(list, "EMPTY") || strings.Contains(list, "nope-not-a-thing") {
		t.Fatalf("the refused append disturbed the board: %q", list)
	}
}

func TestQueueAppendListRoundTrip(t *testing.T) {
	d := startDaemon(t)
	repo := queueRepo(t, d, "qgate2")

	out := d.mustRun("queue", "append", "--component", repo,
		"--blueprint", "two-step", "--goal", "Surface the retry counter",
		"--tech", "go", "--with", "ticket=LEDGER-9", "--attended=false")
	id := queueID(t, out)

	list := d.mustRun("queue", "list", "--component", repo)
	for _, want := range []string{"#" + id, "pending", "two-step", "Surface the retry counter", "attended=false", "tech=go"} {
		if !strings.Contains(list, want) {
			t.Fatalf("the board lost %q: %q", want, list)
		}
	}
	// D8: no size was written, so no size renders — and no bare number
	// either ("size" may not appear at all).
	if strings.Contains(list, "size ") {
		t.Fatalf("an unsized item rendered a size: %q", list)
	}
	// The empty board speaks (D4): a different root answers, honestly.
	other := d.mustRun("queue", "list", "--component", filepath.Join(t.TempDir(), "nobody"))
	if !strings.Contains(other, "EMPTY") || !strings.Contains(other, "advance") {
		t.Fatalf("the empty state is not a statement: %q", other)
	}
}

func TestQueueSettingsRoundTrip(t *testing.T) {
	d := startDaemon(t)
	repo := queueRepo(t, d, "qgate3")

	// Bare reads on an UNREGISTERED root answer with the declared
	// defaults, SAID AS SUCH (P31's third state).
	before := d.mustRun("queue", "settings", "--component", filepath.Join(t.TempDir(), "ghost"))
	if !strings.Contains(before, "queue_advance=ask") || !strings.Contains(before, "sizing_mode=manual") || !strings.Contains(before, "UNREGISTERED") {
		t.Fatalf("the unregistered read hid its state: %q", before)
	}
	// Write ONE flag; the untouched one must keep its current value —
	// exactly what was written, including the untouched one (T6's gate).
	d.mustRun("queue", "settings", "--component", repo, "--queue-advance", "auto")
	after := d.mustRun("queue", "settings", "--component", repo)
	if !strings.Contains(after, "queue_advance=auto") || !strings.Contains(after, "sizing_mode=manual") {
		t.Fatalf("the merge lost the untouched knob: %q", after)
	}
	d.mustRun("queue", "settings", "--component", repo, "--sizing-mode", "skill")
	after2 := d.mustRun("queue", "settings", "--component", repo)
	if !strings.Contains(after2, "queue_advance=auto") || !strings.Contains(after2, "sizing_mode=skill") {
		t.Fatalf("the second write clobbered the first: %q", after2)
	}
	// Off-vocabulary is a usage refusal (exit 2), named.
	if out, code := runExit(t, d, "queue", "settings", "--component", repo, "--queue-advance", "sometimes"); code != 2 {
		t.Fatalf("an off-vocabulary advance exited %d, want 2: %q", code, out)
	}
}

func TestQueueRemoveIsCancel(t *testing.T) {
	d := startDaemon(t)
	repo := queueRepo(t, d, "qgate4")
	id := queueID(t, d.mustRun("queue", "append", "--component", repo,
		"--blueprint", "two-step", "--goal", "Retire the legacy exporter"))

	out := d.mustRun("queue", "remove", id)
	if !strings.Contains(out, "cancelled") || !strings.Contains(out, "D12") {
		t.Fatalf("remove did not say what remove MEANS: %q", out)
	}
	list := d.mustRun("queue", "list", "--component", repo)
	if !strings.Contains(list, "#"+id) || !strings.Contains(list, "cancelled") {
		t.Fatalf("the row vanished — remove deleted (D12 says never): %q", list)
	}
}

func TestSetSizeRefusedInSkillMode(t *testing.T) {
	d := startDaemon(t)
	repo := queueRepo(t, d, "qgate5")
	d.mustRun("queue", "settings", "--component", repo, "--sizing-mode", "skill")

	id := queueID(t, d.mustRun("queue", "append", "--component", repo,
		"--blueprint", "two-step", "--goal", "Backfill the audit column"))
	out, code := runExit(t, d, "queue", "set-size", id, "--min", "1", "--max", "2", "--basis", "one column, one door")
	if code == 0 {
		t.Fatalf("a hand-written size landed under skill mode: %q", out)
	}
	// Named refusal, never a silent no-op: report_size is the only writer there (D9).
	if !strings.Contains(out, "report_size") || !strings.Contains(out, "D9") {
		t.Fatalf("the skill-mode refusal was not the named one: %q", out)
	}
	// Under manual the same act lands, size and all (D7's half).
	d.mustRun("queue", "settings", "--component", repo, "--sizing-mode", "manual")
	d.mustRun("queue", "set-size", id, "--min", "1", "--max", "2", "--basis", "one column, one door")
	list := d.mustRun("queue", "list", "--component", repo)
	if !strings.Contains(list, "ready") || !strings.Contains(list, "size 1–2 (one column, one door)") {
		t.Fatalf("the manual size did not land as range-plus-basis: %q", list)
	}
}

func TestQueueReorderMovesTheRowAndNothingElse(t *testing.T) {
	d := startDaemon(t)
	repo := queueRepo(t, d, "qgate6")
	first := queueID(t, d.mustRun("queue", "append", "--component", repo, "--blueprint", "two-step", "--goal", "first intent"))
	second := queueID(t, d.mustRun("queue", "append", "--component", repo, "--blueprint", "two-step", "--goal", "second intent"))

	d.mustRun("queue", "reorder", second, "1")
	lines := strings.Split(strings.TrimSpace(d.mustRun("queue", "list", "--component", repo)), "\n")
	if len(lines) != 2 {
		t.Fatalf("the board grew rows: %q", lines)
	}
	if !strings.Contains(lines[0], "#"+second+" · pos 1") || !strings.Contains(lines[1], "#"+first+" · pos 2") {
		t.Fatalf("the order did not move cleanly (D1): %q", lines)
	}
	if out, code := runExit(t, d, "queue", "reorder", second); code != 2 {
		t.Fatalf("reorder without a position exited %d, want the usage exit 2: %q", code, out)
	}
}

// TestQueueTakesNoCredentialFlags is AGENTS §4 as a habit: a value in
// argv is in the process list and in shell history, and that failure is
// silent. No flag of this verb set may name a credential.
func TestQueueTakesNoCredentialFlags(t *testing.T) {
	src := readFileForGuard(t, "queue.go")
	for _, banned := range []string{`"key"`, `"token"`, `"secret"`, `"apikey"`, `"api-key"`, `"password"`} {
		if strings.Contains(src, banned) {
			t.Errorf("queue.go registers credential flag %s — the environment, or a path to a file (§4)", banned)
		}
	}
	// P34's other half, grepped in-product: the binary's queue path DIALS
	// and opens nothing. (This test file may open the store — seeding and
	// row-level truth are the test's privilege; the CLI's are not.)
	for _, banned := range []string{"store.Open", "internal/store"} {
		if strings.Contains(src, banned) {
			t.Errorf("queue.go reaches for %s — the queue's CLI is a socket client, never a second store handle (P34/CG9)", banned)
		}
	}
}

// TestQueueSocketAndCLIProduceIdenticalRows is CG9's proof, deferred here
// from T7 because it needs BOTH entry points: the same item appended
// through the CLI verb and through the socket (the test dials the SAME
// client the CLI dials, at the fixture's endpoint) against ONE store, and
// the rows come out the same. ID and Position are normalised: two appends
// land at two positions by D1's own law (append never disturbs, so they
// queue behind each other) — every OTHER column is byte-identical, which
// is what "one implementation underneath" means row-wise. Timestamps are
// excluded (two calls, one clock — their equality would be a race pinned
// as a gate) but their PRESENCE is asserted.
func TestQueueSocketAndCLIProduceIdenticalRows(t *testing.T) {
	d := startDaemon(t)
	repo := queueRepo(t, d, "qident")
	// NOT dial(0): that runs appenv.Apply(), which os.Setenvs the whole
	// namespace at the TEST PROCESS — and every later fixture builds its
	// daemon env from os.Environ(), where a leaked first entry outranks
	// the fixture's own appended one (measured: three live-daemon gates
	// downstream bound to my dead socket). The test connects the way
	// dial() connects — same net dial, same client, same Call — to the
	// fixture's endpoint directly.
	conn, err := net.Dial("unix", d.endpoint)
	if err != nil {
		t.Fatalf("connect to the fixture daemon: %v", err)
	}
	defer conn.Close()
	c := protocol.NewClient(conn, ClientName, Version)
	if _, err := c.Connect(0); err != nil {
		t.Fatalf("connect handshake: %v", err)
	}
	var sockRes protocol.QueueItemResult
	if err := c.Call(protocol.MethodQueueAppend, protocol.QueueAppendParams{
		ComponentRoot: repo, Blueprint: "two-step", Goal: "identical intent, two doors",
		Tech: "go", With: map[string]string{"ticket": "X-1"},
	}, &sockRes); err != nil {
		t.Fatalf("socket append: %v", err)
	}
	out := d.mustRun("queue", "append", "--component", repo,
		"--blueprint", "two-step", "--goal", "identical intent, two doors",
		"--tech", "go", "--with", "ticket=X-1")
	cliID, err := strconv.ParseInt(queueID(t, out), 10, 64)
	if err != nil {
		t.Fatalf("id: %v", err)
	}

	s, err := store.Open(store.Options{Dir: filepath.Join(d.stateDir, "aeon"), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("open the store: %v", err)
	}
	defer s.Close()
	a, okA, err := s.QueueItemByID(sockRes.Item.ID)
	if err != nil || !okA {
		t.Fatalf("socket row missing: %v %v", okA, err)
	}
	b, okB, err := s.QueueItemByID(cliID)
	if err != nil || !okB {
		t.Fatalf("CLI row missing: %v %v", okB, err)
	}
	if a.CreatedAt == "" || b.CreatedAt == "" {
		t.Fatal("a row was stamped nothing")
	}
	a.ID, b.ID = 0, 0
	a.Position, b.Position = 0, 0
	a.CreatedAt, b.CreatedAt, a.UpdatedAt, b.UpdatedAt = "", "", "", ""
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("the two doors wrote different rows — CG9's one implementation is two:\nsocket: %+v\n  cli: %+v", a, b)
	}
}
