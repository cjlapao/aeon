package main

// blueprint_adopt_test.go — S48 T4 (BP3/BP6): the one-time carry of the
// state-directory files into rows. The tests that matter most here are the
// scary ones: an operator's edited standard copy surviving EXACTLY, a bad
// file neither vanishing nor blocking the others, and a file never being
// written, renamed, or deleted.

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/blueprintseed"
	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

const adoptCustomYAML = "id: my-custom\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: work\n    step: {kind: gate, question: go}\nquestions:\n  go:\n    prompt: go?\n    answers: [yes, no]\n"

const adoptBrokenYAML = "id: broken-one\nschemaVersion: 1\nversion: 1\nlanes: [oops\n"

type adoptEnv struct {
	t   *testing.T
	st  *store.Store
	dir string // the store dir (also <dir>/blueprints is the file dir)
	bps string
}

func newAdoptEnv(t *testing.T) *adoptEnv {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(store.Options{Dir: dir, Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	bps := filepath.Join(dir, "blueprints")
	if err := os.MkdirAll(bps, 0o700); err != nil {
		t.Fatal(err)
	}
	return &adoptEnv{t: t, st: st, dir: dir, bps: bps}
}

func (e *adoptEnv) write(name, content string) string {
	e.t.Helper()
	p := filepath.Join(e.bps, name+".yaml")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		e.t.Fatal(err)
	}
	return p
}

func (e *adoptEnv) adopt() AdoptReport {
	e.t.Helper()
	r, err := adoptFiles(context.Background(), e.st, e.bps)
	if err != nil {
		e.t.Fatal(err)
	}
	return r
}

func embeddedBytes(t *testing.T, name string) string {
	t.Helper()
	raw, ok, err := blueprint.EmbeddedSource().Get(name)
	if err != nil || !ok {
		t.Fatalf("embedded %s: %v", name, err)
	}
	return string(raw)
}

func TestAdoptPristineCurrentStandardIsSystemRow(t *testing.T) {
	e := newAdoptEnv(t)
	e.write("two-step", embeddedBytes(t, "two-step"))
	r := e.adopt()
	if r.Pristine != 1 || r.Edited != 0 || r.Custom != 0 || len(r.Refused) != 0 {
		t.Fatalf("report: %+v", r)
	}
	row, ok, err := e.st.BlueprintGet(context.Background(), "two-step")
	if err != nil || !ok {
		t.Fatalf("no row: %v", err)
	}
	if row.Origin != "system" || row.Content != embeddedBytes(t, "two-step") {
		t.Fatalf("origin %q or content drifted", row.Origin)
	}
	revs, err := e.st.RevisionList(context.Background(), "two-step", 5)
	if err != nil || len(revs) != 1 || revs[0].Op != "adopt" || revs[0].ActorKind != "system" || revs[0].ActorID != "startup" {
		t.Fatalf("revision: %+v %v", revs, err)
	}
	refs, err := e.st.BlueprintOwnRefs(context.Background(), "two-step")
	if err != nil || len(refs) == 0 {
		t.Fatalf("refs empty — the derived table must be filled at adoption: %v", err)
	}
	led, ok, err := e.st.ImportByHash(context.Background(), row.ContentHash)
	if err != nil || !ok || led.Result != "imported" || led.Detail != "pristine" {
		t.Fatalf("ledger: %+v %v %v", led, ok, err)
	}
}

func TestAdoptPristineOlderStandardIsSystemRowAndMarkedForUpdate(t *testing.T) {
	// The historical bytes cannot be reproduced (they are a past release),
	// so the classifier itself is pinned directly: every hash the table
	// lists must classify as pristine, and the table must carry what the
	// comment promises. T5's seed performs the versioned update; adoption
	// only marks it pristine (never edited).
	for _, name := range []string{"two-step", "dev-loop"} {
		list := blueprintseed.PristineHashes(name)
		if len(list) == 0 {
			t.Fatalf("%s: no pristine hashes listed", name)
		}
		for _, h := range list {
			if !adoptHashIsPristine(name, h) {
				t.Fatalf("%s %s: a listed pristine hash is not classified pristine — an old pristine would be called EDITED", name, h)
			}
		}
	}
	if adoptHashIsPristine("two-step", strings.Repeat("0", 64)) {
		t.Fatal("an unknown hash classifies pristine — edited copies would be called pristine")
	}
}

func TestAdoptEditedStandardKeepsUserContent(t *testing.T) {
	e := newAdoptEnv(t)
	edited := embeddedBytes(t, "dev-loop") + "# the operator's own note — survives\n"
	e.write("dev-loop", edited)
	r := e.adopt()
	if r.Edited != 1 || r.Pristine != 0 {
		t.Fatalf("report: %+v", r)
	}
	row, ok, err := e.st.BlueprintGet(context.Background(), "dev-loop")
	if err != nil || !ok {
		t.Fatal("the edited standard was not adopted at all")
	}
	if row.Origin != "system" {
		t.Fatalf("origin %q — an edited standard is still a system row", row.Origin)
	}
	if row.Content != edited {
		t.Fatal("OPERATOR'S EDITS LOST — the row does not hold their bytes")
	}
	// And a re-adoption never replaces them with embedded bytes.
	again := e.adopt()
	if again.Edited != 0 || again.Already != 1 {
		t.Fatalf("second run disturbed anything: %+v", again)
	}
	row2, _, _ := e.st.BlueprintGet(context.Background(), "dev-loop")
	if row2.Content != edited {
		t.Fatal("a re-adoption replaced the operator's text")
	}
}

func TestAdoptCustomIsUserRow(t *testing.T) {
	e := newAdoptEnv(t)
	e.write("my-custom", adoptCustomYAML)
	r := e.adopt()
	if r.Custom != 1 {
		t.Fatalf("report: %+v", r)
	}
	row, ok, _ := e.st.BlueprintGet(context.Background(), "my-custom")
	if !ok || row.Origin != "user" {
		t.Fatalf("provenance wrong: %+v %v", row, ok)
	}
}

func TestAdoptInvalidFileRefusedPerFileWithDiagnostics(t *testing.T) {
	e := newAdoptEnv(t)
	e.write("broken-one", adoptBrokenYAML)
	e.write("my-custom", adoptCustomYAML)
	r := e.adopt()
	if len(r.Refused) != 1 || r.Refused[0].Name != "broken-one" || r.Custom != 1 {
		t.Fatalf("a bad file must not block the others: %+v", r)
	}
	if _, ok, _ := e.st.BlueprintGet(context.Background(), "broken-one"); ok {
		t.Fatal("an invalid file got a row")
	}
	refused, err := e.st.ImportRefusals(context.Background())
	if err != nil || len(refused) != 1 || refused[0].Detail == "" || len(refused[0].Detail) > adoptMaxDiag {
		t.Fatalf("refusal ledger: %+v %v", refused, err)
	}
	if !strings.Contains(refused[0].Detail, "invalid") && !strings.Contains(refused[0].Detail, "yaml") {
		t.Fatalf("diagnostics render empty-ish: %q", refused[0].Detail)
	}
	// doctor reports it (its own store-open path resolves through
	// AEON_STATE_DIR, so point that at the same dir).
	t.Setenv("AEON_STATE_DIR", e.dir)
	out := &strings.Builder{}
	blueprintDoctorLines(out)
	if !strings.Contains(out.String(), "refused at adoption") || !strings.Contains(out.String(), "broken-one") {
		t.Fatalf("doctor line missing:\n%s", out.String())
	}
}

func TestAdoptIsIdempotentByHashOnce(t *testing.T) {
	e := newAdoptEnv(t)
	e.write("my-custom", adoptCustomYAML)
	e.write("two-step", embeddedBytes(t, "two-step"))
	first := e.adopt()
	if first.Custom+first.Pristine != 2 {
		t.Fatalf("first run: %+v", first)
	}
	second := e.adopt()
	if second.Custom != 0 || second.Pristine != 0 || second.Edited != 0 || second.Already != 2 {
		t.Fatalf("second run wrote: %+v", second)
	}
	rows, _ := e.st.BlueprintList(context.Background(), "", "")
	if len(rows) != 2 {
		t.Fatalf("double adoption changed the rows: %d", len(rows))
	}
}

func TestAdoptNeverTouchesFiles(t *testing.T) {
	e := newAdoptEnv(t)
	names := []string{"my-custom", "broken-one", "two-step"}
	contents := map[string]string{"my-custom": adoptCustomYAML, "broken-one": adoptBrokenYAML, "two-step": embeddedBytes(t, "two-step")}
	type stat struct {
		ino   uint64
		size  int64
		mtime time.Time
	}
	before := map[string]stat{}
	for _, n := range names {
		p := e.write(n, contents[n])
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		before[n] = stat{fi.Sys().(*syscall.Stat_t).Ino, fi.Size(), fi.ModTime()}
	}
	e.adopt()
	for _, n := range names {
		fi, err := os.Stat(filepath.Join(e.bps, n+".yaml"))
		if err != nil {
			t.Fatalf("%s disappeared", n)
		}
		got := stat{fi.Sys().(*syscall.Stat_t).Ino, fi.Size(), fi.ModTime()}
		if got != before[n] {
			t.Fatalf("%s was touched (inode/size/mtime changed): %+v vs %+v", n, before[n], got)
		}
		raw, err := os.ReadFile(filepath.Join(e.bps, n+".yaml"))
		if err != nil || string(raw) != contents[n] {
			t.Fatalf("%s bytes changed", n)
		}
	}
}

func TestAdoptIgnoresSymlinksAndSubdirsAndListsThem(t *testing.T) {
	e := newAdoptEnv(t)
	e.write("my-custom", adoptCustomYAML)
	if err := os.Symlink(filepath.Join(e.bps, "my-custom.yaml"), filepath.Join(e.bps, "linked.yaml")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(e.bps, "sub.yaml"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.bps, "notes.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := e.adopt()
	if r.Custom != 1 {
		t.Fatalf("only the one real file imports: %+v", r)
	}
	joined := ""
	for _, s := range r.Skipped {
		joined += s.Name + "|" + s.Reason + "\n"
	}
	for _, want := range []string{"linked|symlink", "sub|not-a-file", "notes.txt|not a .yaml file"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("abuse not listed (%q):\n%s", want, joined)
		}
	}
	if _, ok, _ := e.st.BlueprintGet(context.Background(), "linked"); ok {
		t.Fatal("a symlink was imported — path abuse")
	}
}

func TestAdoptNameCollisionSkips(t *testing.T) {
	e := newAdoptEnv(t)
	keep := "id: my-collide\nschemaVersion: 1\nversion: 4\nlanes:\n  - id: a\n    step: {kind: gate, question: q}\nquestions:\n  q:\n    prompt: q?\n    answers: [yes]\n"
	sum := sha256.Sum256([]byte(keep))
	row := store.BlueprintRow{Name: "my-collide", Version: 4, Content: keep, ContentHash: hex.EncodeToString(sum[:]), Origin: "user", UpdatedBy: "user:keeper"}
	rev := store.RevisionRow{Version: 4, Content: keep, ContentHash: hex.EncodeToString(sum[:]), Op: "save", ActorKind: "user", ActorID: "keeper"}
	if err := e.st.BlueprintWithTx(context.Background(), func(tx *sql.Tx) error {
		return store.BlueprintPutTx(tx, row, rev, nil)
	}); err != nil {
		t.Fatal(err)
	}
	e.write("my-collide", adoptCustomYAML)
	r := e.adopt()
	if len(r.Skipped) != 1 || !strings.Contains(r.Skipped[0].Reason, "never merged") {
		t.Fatalf("collision not skipped: %+v", r)
	}
	got, _, _ := e.st.BlueprintGet(context.Background(), "my-collide")
	if got.Content != keep || got.Version != 4 {
		t.Fatal("the existing row was disturbed — an adopt must never merge or overwrite")
	}
}

func TestAdoptedBehaviourEqualsFileBehaviour(t *testing.T) {
	// The file era answered: list shows the standards then customs, show
	// returns the file's bytes verbatim, validate speaks by name. After
	// adoption the SAME store answers identically through the rows.
	e := newAdoptEnv(t)
	edited := embeddedBytes(t, "two-step") + "# operator note\n"
	e.write("two-step", edited)
	e.write("my-custom", adoptCustomYAML)
	e.adopt()
	d := daemon.New(daemon.Options{Store: e.st})
	listAny, err := d.Table.ListBlueprints(protocol.BlueprintListParams{})
	if err != nil {
		t.Fatal(err)
	}
	names := listAny.Names
	// S48 T7 made list typed; the FILE-ERA shape it pins still holds:
	// every shipped name plus the adopted custom, sorted, nothing else.
	// (New mounts the full embedded set today, which is wider than
	// StandardNames — ask the source, not a remembered list.)
	embedded, err := blueprint.EmbeddedSource().Names()
	if err != nil {
		t.Fatal(err)
	}
	// The T7 typed list sorts ALL names (the store's ORDER BY name) — the
	// pre-T7 seam appended customs after the shipped block. The file-era
	// pin that survives: every shipped name plus the adopted custom, and
	// nothing else. Position is no longer the contract; ordering is.
	if len(names) != len(embedded)+1 {
		t.Fatalf("list shape drifted from the file era: %v (embedded %d)", names, len(embedded))
	}
	joined := strings.Join(names, ",")
	for _, want := range append(append([]string{}, embedded...), "my-custom") {
		if !strings.Contains(joined, want) {
			t.Fatalf("%q missing from the list: %v", want, names)
		}
	}
	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			t.Fatalf("names are not sorted: %v", names)
		}
	}
	showAny, err := d.Table.ShowBlueprint(protocol.BlueprintShowParams{Name: "two-step"})
	if err != nil {
		t.Fatal(err)
	}
	if showAny.Content != edited {
		t.Fatal("the operator's bytes do not survive the round trip through adoption")
	}
	if _, err := d.Table.ShowBlueprint(protocol.BlueprintShowParams{Name: "my-custom"}); err != nil {
		t.Fatalf("the adopted custom is unknown: %v", err)
	}
}

func TestAdoptRunsBeforeSeedAndBeforeLoops(t *testing.T) {
	var order []string
	err := blueprintStartup(
		func() error { order = append(order, "adopt"); return nil },
		func() error { order = append(order, "seed"); return nil },
		func() error { order = append(order, "accept-loops"); return nil },
	)
	if err != nil || strings.Join(order, ",") != "adopt,seed,accept-loops" {
		t.Fatalf("order: %v %v", order, err)
	}
	order = nil
	boom := os.ErrPermission
	err = blueprintStartup(
		func() error { order = append(order, "adopt"); return boom },
		func() error { order = append(order, "seed"); return nil },
		func() error { order = append(order, "accept-loops"); return nil },
	)
	if err != boom || strings.Join(order, ",") != "adopt" {
		t.Fatalf("a failed adoption must stop the walk (loops never accepted on a half-carried store): %v %v", order, err)
	}
}

func TestAdoptOneTransactionPerFile(t *testing.T) {
	e := newAdoptEnv(t)
	e.write("aaa-first", strings.Replace(adoptCustomYAML, "my-custom", "aaa-first", 1))
	e.write("bbb-second", strings.Replace(adoptCustomYAML, "my-custom", "bbb-second", 1))
	t.Cleanup(func() { adoptFailBefore = "" })
	adoptFailBefore = "bbb-second"
	if _, err := adoptFiles(context.Background(), e.st, e.bps); err == nil {
		t.Fatal("the forced failure was swallowed")
	}
	if _, ok, _ := e.st.BlueprintGet(context.Background(), "aaa-first"); !ok {
		t.Fatal("files before N were not imported (per-file transaction broken)")
	}
	if _, ok, _ := e.st.BlueprintGet(context.Background(), "bbb-second"); ok {
		t.Fatal("file N left a row — its transaction was not atomic")
	}
	// N itself is untouched: no ledger row for its hash.
	sum := sha256.Sum256([]byte(strings.Replace(adoptCustomYAML, "my-custom", "bbb-second", 1)))
	if _, ok, _ := e.st.ImportByHash(context.Background(), hex.EncodeToString(sum[:])); ok {
		t.Fatal("file N left a ledger row — the transaction must cover the ledger too")
	}
	adoptFailBefore = ""
	r := e.adopt() // and the retry finishes: N is new, <N is already-imported
	if r.Custom != 1 || r.Already != 1 {
		t.Fatalf("retry did not finish the job: %+v", r)
	}
}
