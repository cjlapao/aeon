package daemon

// blueprint_write_test.go — S48 T6: the one write service's contract. The
// Analyze report is the validator verbatim; the transaction moves row,
// next revision and derived refs together (or nothing); identical content
// is not an act; the shipped rows are editable and duplicable but
// undeletable; live automation names every refuser of a delete; pinned
// loops carry their own bytes and no door here can disturb them.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

var bpActor = protocol.Actor{Kind: "user", ID: "operator"}

func bpGate(id, question string) string {
	return fmt.Sprintf("id: %s\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: work\n    step: {kind: gate, question: %s}\nquestions:\n  %s:\n    prompt: %s?\n    answers: [yes]\n", id, question, question, question)
}

func bpWritesEnv(t *testing.T) (*Daemon, *store.Store, *blueprintWrites) {
	t.Helper()
	s := recoveryStore(t)
	d := New(Options{Store: s})
	return d, s, newBlueprintWrites(s)
}

func mustRefused(t *testing.T, err error, kind string) *protocol.Error {
	t.Helper()
	e, ok := err.(*protocol.Error)
	if !ok || e.Code != protocol.ErrBlueprintRefused.Code {
		t.Fatalf("want ErrBlueprintRefused(%s), got %T %v", kind, err, err)
	}
	if kind != "" && e.Data.Kind != kind {
		t.Fatalf("refusal kind %q, want %q (%v)", e.Data.Kind, kind, err)
	}
	return e
}

// BP4: the validator is Analyze and nothing else. An invalid candidate
// refuses with the report VERBATIM — byte-identical to what
// blueprint.validate answers — and no row, revision or ref moved.
func TestSaveValidatesWithAnalyzeVerbatim(t *testing.T) {
	_, s, w := bpWritesEnv(t)
	ctx := context.Background()
	broken := "id: broken-idea\nschemaVersion: 1\nversion: 1\nlanes: []\n"
	_, err := w.Save(ctx, bpActor, protocol.BlueprintSaveParams{Name: "broken-idea", Content: broken})
	e := mustRefused(t, err, "diagnostics")
	want, _ := json.Marshal(blueprint.Analyze([]byte(broken), blueprintValidationOptions("")))
	if e.Data.What != string(want) {
		t.Fatalf("report was paraphrased:\n got %s\nwant %s", e.Data.What, want)
	}
	if _, found, _ := s.BlueprintGet(ctx, "broken-idea"); found {
		t.Fatal("an invalid candidate reached the row table")
	}
	revs, _ := s.RevisionList(ctx, "broken-idea", 10)
	if len(revs) != 0 {
		t.Fatalf("an invalid candidate wrote %d revisions", len(revs))
	}
	refs, _ := s.BlueprintOwnRefs(ctx, "broken-idea")
	if len(refs) != 0 {
		t.Fatalf("an invalid candidate wrote refs: %+v", refs)
	}
}

// Rule drift is the failure this pins: the two sentences the file writer
// refused with still refuse, in the same words, from the row door.
func TestSaveRefusesIdMismatchAndOversize(t *testing.T) {
	_, s, w := bpWritesEnv(t)
	ctx := context.Background()
	_, err := w.Save(ctx, bpActor, protocol.BlueprintSaveParams{Name: "wrong-name", Content: bpGate("right-name", "go")})
	e := mustRefused(t, err, "id-mismatch")
	if want := `source id "right-name" does not match selected blueprint "wrong-name"`; !strings.Contains(e.Data.What, want) {
		t.Fatalf("id-mismatch sentence drifted: %s", e.Data.What)
	}
	_, err = w.Save(ctx, bpActor, protocol.BlueprintSaveParams{Name: "huge", Content: strings.Repeat("a", (1<<20)+1)})
	if err == nil || !strings.Contains(err.Error(), "blueprint source exceeds 1 MiB") {
		t.Fatalf("oversize: %v", err)
	}
	if _, found, _ := s.BlueprintGet(ctx, "wrong-name"); found {
		t.Fatal("an id-mismatched candidate reached the row table")
	}
}

// BP7/BP11: row, next revision and derived refs move in ONE transaction —
// a failure inside the write (a bad ref row) leaves no part of the write
// behind.
func TestSaveWritesRowRevisionAndRefsAtomically(t *testing.T) {
	_, s, w := bpWritesEnv(t)
	ctx := context.Background()
	content := "id: atomic-one\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: survey\n    step: {kind: model-step, agent: default}\n"
	res, err := w.Save(ctx, bpActor, protocol.BlueprintSaveParams{Name: "atomic-one", Content: content})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Revision != 1 || res.Origin != "user" {
		t.Fatalf("first save: %+v", res)
	}
	row, found, _ := s.BlueprintGet(ctx, "atomic-one")
	if !found || row.Content != content {
		t.Fatal("the row is not the saved bytes verbatim")
	}
	revs, _ := s.RevisionList(ctx, "atomic-one", 10)
	if len(revs) != 1 || revs[0].Op != "create" || revs[0].ActorKind != "user" || revs[0].ActorID != "operator" {
		t.Fatalf("revision: %+v", revs)
	}
	own, _ := s.BlueprintOwnRefs(ctx, "atomic-one")
	bp, _ := blueprint.Load([]byte(content))
	want := blueprint.References(&bp)
	if len(own) != len(want) {
		t.Fatalf("stored refs %+v, re-extracted %d", own, len(want))
	}
	// A failure inside the transaction leaves NOTHING: a poisoned ref row
	// (an empty kind fails the CHECK) must roll the row and the revision
	// back with it.
	err = s.BlueprintWithTx(ctx, func(tx *sql.Tx) error {
		return store.BlueprintPutTx(tx,
			store.BlueprintRow{Name: "atomic-two", Version: 1, Content: content, ContentHash: row.ContentHash, Origin: "user", UpdatedBy: "user:operator"},
			store.RevisionRow{Version: 1, Content: content, ContentHash: row.ContentHash, Op: "create", ActorKind: "user", ActorID: "operator"},
			[]store.RefRow{{Name: "atomic-two", Kind: "", Target: "nope"}})
	})
	if err == nil {
		t.Fatal("a poisoned ref row committed")
	}
	if _, found, _ := s.BlueprintGet(ctx, "atomic-two"); found {
		t.Fatal("the row survived a rolled-back transaction")
	}
	if revs, _ := s.RevisionList(ctx, "atomic-two", 10); len(revs) != 0 {
		t.Fatal("the revision survived a rolled-back transaction")
	}
}

// BP7: audit is a record of acts, not of calls — identical content is not
// one and writes no revision.
func TestSaveIdenticalContentIsChangedFalseNoRevision(t *testing.T) {
	_, s, w := bpWritesEnv(t)
	ctx := context.Background()
	content := bpGate("steady", "go")
	if _, err := w.Save(ctx, bpActor, protocol.BlueprintSaveParams{Name: "steady", Content: content}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		res, err := w.Save(ctx, bpActor, protocol.BlueprintSaveParams{Name: "steady", Content: content})
		if err != nil {
			t.Fatal(err)
		}
		if res.Changed || res.Revision != 1 {
			t.Fatalf("no-op answered %+v", res)
		}
	}
	revs, _ := s.RevisionList(ctx, "steady", 10)
	if len(revs) != 1 {
		t.Fatalf("a no-op grew the history to %d", len(revs))
	}
}

func TestSaveCreateOnlyCollisionRefuses(t *testing.T) {
	_, s, w := bpWritesEnv(t)
	ctx := context.Background()
	content := bpGate("only-one", "go")
	if _, err := w.Save(ctx, bpActor, protocol.BlueprintSaveParams{Name: "only-one", Content: content, CreateOnly: true}); err != nil {
		t.Fatal(err)
	}
	// The collision refuses even when the bytes happen to agree — the
	// caller's intent was a CREATE.
	_, err := w.Save(ctx, bpActor, protocol.BlueprintSaveParams{Name: "only-one", Content: content, CreateOnly: true})
	e := mustRefused(t, err, "collision")
	if !strings.Contains(e.Data.What, "only-one") {
		t.Fatalf("collision did not name the existing name: %s", e.Data.What)
	}
	revs, _ := s.RevisionList(ctx, "only-one", 10)
	if len(revs) != 1 {
		t.Fatalf("the collision wrote history: %d", len(revs))
	}
}

// BP8: the copy is TEXT — comments and layout survive; id and version are
// the only lines that move. It then runs the SAME Analyze and write path.
func TestDuplicatePreservesCommentsAndStartsAtVersionOne(t *testing.T) {
	_, s, w := bpWritesEnv(t)
	ctx := context.Background()
	original := "# the operator's note: keep this comment\nid: src-one\nschemaVersion: 1\nversion: 4\nlanes:\n  - id: survey\n    step: {kind: model-step, agent: default}\n"
	if _, err := w.Save(ctx, bpActor, protocol.BlueprintSaveParams{Name: "src-one", Content: original}); err != nil {
		t.Fatal(err)
	}
	res, err := w.Duplicate(ctx, bpActor, protocol.BlueprintDuplicateParams{From: "src-one", Name: "copy-one"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Revision != 1 || res.Version != 1 {
		t.Fatalf("duplicate answered %+v — the copy restarts at version 1", res)
	}
	cp, _, _ := s.BlueprintGet(ctx, "copy-one")
	diffs := 0
	ol, cl := strings.Split(original, "\n"), strings.Split(cp.Content, "\n")
	if len(ol) != len(cl) {
		t.Fatalf("the copy changed the line count — layout is the contract")
	}
	for i := range ol {
		if ol[i] != cl[i] {
			diffs++
			if !strings.HasPrefix(ol[i], "id:") && !strings.HasPrefix(ol[i], "version:") {
				t.Fatalf("the copy changed more than id and version: %q -> %q", ol[i], cl[i])
			}
		}
	}
	if diffs != 2 {
		t.Fatalf("expected exactly id+version to move, got %d changed lines", diffs)
	}
	if !strings.Contains(cp.Content, "# the operator's note") {
		t.Fatal("the comment did not survive the copy")
	}
	revs, _ := s.RevisionList(ctx, "copy-one", 10)
	if len(revs) != 1 || revs[0].Op != "duplicate" {
		t.Fatalf("copy revision: %+v", revs)
	}
	// A taken name refuses; an unknown source names the nearest.
	if _, err := w.Duplicate(ctx, bpActor, protocol.BlueprintDuplicateParams{From: "src-one", Name: "copy-one"}); err == nil {
		t.Fatal("a duplicate merged onto a taken name")
	}
	_, err = w.Duplicate(ctx, bpActor, protocol.BlueprintDuplicateParams{From: "src-on", Name: "typo-copy"})
	e := mustRefused(t, err, "from-unknown")
	if !strings.Contains(e.Data.What, "src-one") {
		t.Fatalf("from-unknown did not name the nearest: %s", e.Data.What)
	}
}

// BP6/BP10: shipped rows are undeletable, and a delete refuses while ANY
// live automation names the blueprint — naming EVERY refuser.
func TestDeleteRefusedForSystemAndWhileReferencedNamingEach(t *testing.T) {
	_, s, w := bpWritesEnv(t)
	ctx := context.Background()
	if _, err := w.Delete(ctx, bpActor, protocol.BlueprintDeleteParams{Name: "two-step"}); err == nil {
		t.Fatal("a shipped blueprint was deleted")
	} else {
		e := mustRefused(t, err, "system")
		if e.Data.What != "a shipped blueprint cannot be deleted; restore or duplicate it" {
			t.Fatalf("system refusal drifted: %s", e.Data.What)
		}
	}
	if _, err := w.Save(ctx, bpActor, protocol.BlueprintSaveParams{Name: "nightly", Content: bpGate("nightly", "go")}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateComponent(store.NewComponent{Name: "g", Slug: "g-g", Root: "/repo/g"}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, id := range []string{"sched-a", "sched-b"} {
		if err := s.SaveTriggerSchedule(ctx, store.TriggerScheduleRow{ID: id, Kind: "schedule", Blueprint: "nightly", Expr: "0 3 * * *", Zone: "UTC", CreatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.CreateEventTrigger(store.Actor{Kind: "user", ID: "1000"}, store.NewEventTrigger{
		Name: "pr-open", Blueprint: "nightly", Match: store.Match{Event: "goal.approved"}, MaxRunsPerHour: 5,
	}); err != nil {
		t.Fatal(err)
	}
	q, err := s.AppendQueueItem(store.NewQueueItemRow{ComponentRoot: "/repo/g", Status: "pending", Blueprint: "nightly", Goal: "clean up", WorkspaceRoot: "/repo/g/work"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = w.Delete(ctx, bpActor, protocol.BlueprintDeleteParams{Name: "nightly"})
	inUse, ok := err.(*protocol.Error)
	if !ok || inUse.Code != protocol.ErrBlueprintInUse.Code {
		t.Fatalf("want ErrBlueprintInUse, got %T %v", err, err)
	}
	for _, want := range []string{"sched-a", "sched-b", "pr-open", "#" + fmt.Sprint(q.ID), "pending"} {
		if !strings.Contains(inUse.Data.What, want) {
			t.Errorf("the in-use refusal did not name %q: %s", want, inUse.Data.What)
		}
	}
	if _, found, _ := s.BlueprintGet(ctx, "nightly"); !found {
		t.Fatal("the refused delete deleted the row")
	}
}

// The over-refusal check: TERMINAL history (a drained queue item) never
// blocks, and started loops only report — they keep their pinned bytes.
func TestDeleteAllowsWhenOnlyHistoryReferences(t *testing.T) {
	_, s, w := bpWritesEnv(t)
	ctx := context.Background()
	if _, err := w.Save(ctx, bpActor, protocol.BlueprintSaveParams{Name: "drained", Content: bpGate("drained", "go")}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateComponent(store.NewComponent{Name: "h", Slug: "h-h", Root: "/repo/h"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendQueueItem(store.NewQueueItemRow{ComponentRoot: "/repo/h", Status: "done", Blueprint: "drained", Goal: "finished", WorkspaceRoot: "/repo/h/work"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePinnedLoop(store.NewLoop{Slug: "drained-1", Title: "started once", Root: "/repo/h", ComponentRoot: "/repo/h", Mode: "standard", Status: "waiting"}, "drained", 1, bpGate("drained", "go"), "{}"); err != nil {
		t.Fatal(err)
	}
	res, err := w.Delete(ctx, bpActor, protocol.BlueprintDeleteParams{Name: "drained"})
	if err != nil {
		t.Fatalf("terminal history blocked the delete: %v", err)
	}
	if !res.Deleted || res.LoopsStartedFromIt != 1 || res.Note != "started loops keep their pinned content" {
		t.Fatalf("delete answer: %+v", res)
	}
	if _, found, _ := s.BlueprintGet(ctx, "drained"); found {
		t.Fatal("the row survived a successful delete")
	}
}

// BP7: history outlives the name. Deleting and re-creating the same
// blueprint CONTINUES the revision numbering — the ledger is the record.
func TestDeleteKeepsRevisionsAndRecreateContinuesNumbering(t *testing.T) {
	_, s, w := bpWritesEnv(t)
	ctx := context.Background()
	a, b := bpGate("phoenix", "one"), bpGate("phoenix", "two")
	if _, err := w.Save(ctx, bpActor, protocol.BlueprintSaveParams{Name: "phoenix", Content: a}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Save(ctx, bpActor, protocol.BlueprintSaveParams{Name: "phoenix", Content: b}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Delete(ctx, bpActor, protocol.BlueprintDeleteParams{Name: "phoenix"}); err != nil {
		t.Fatal(err)
	}
	res, err := w.Save(ctx, bpActor, protocol.BlueprintSaveParams{Name: "phoenix", Content: a})
	if err != nil {
		t.Fatal(err)
	}
	if res.Revision != 3 {
		t.Fatalf("recreate answered revision %d, want 3 — numbering must continue", res.Revision)
	}
	revs, _ := s.RevisionList(ctx, "phoenix", 10)
	if len(revs) != 3 {
		t.Fatalf("history after recreate: %d rows", len(revs))
	}
}

// BP7: revert restores a NAMED revision's content as a NEW revision
// (op=revert); the history is never rewritten.
func TestRevertAppendsANewRevision(t *testing.T) {
	_, s, w := bpWritesEnv(t)
	ctx := context.Background()
	a, b := bpGate("rewind", "one"), bpGate("rewind", "two")
	if _, err := w.Save(ctx, bpActor, protocol.BlueprintSaveParams{Name: "rewind", Content: a}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Save(ctx, bpActor, protocol.BlueprintSaveParams{Name: "rewind", Content: b}); err != nil {
		t.Fatal(err)
	}
	res, err := w.Revert(ctx, bpActor, protocol.BlueprintRevertParams{Name: "rewind", Rev: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Revision != 3 {
		t.Fatalf("revert answered %+v", res)
	}
	row, _, _ := s.BlueprintGet(ctx, "rewind")
	if row.Content != a {
		t.Fatal("the row did not return to revision 1's bytes")
	}
	revs, _ := s.RevisionList(ctx, "rewind", 10)
	if len(revs) != 3 || revs[0].Rev != 3 || revs[0].Op != "revert" {
		t.Fatalf("revert did not append (DESC): %+v", revs)
	}
	// An unknown revision is a named refusal.
	if _, err := w.Revert(ctx, bpActor, protocol.BlueprintRevertParams{Name: "rewind", Rev: 99}); err == nil {
		t.Fatal("revert of a nonexistent revision succeeded")
	} else {
		mustRefused(t, err, "rev-unknown")
	}
}

// A revision that no longer validates (history from before a rule change,
// say) is refused with the report — never silently written back.
func TestRevertOfAnInvalidatedRevisionRefusesWithReport(t *testing.T) {
	_, s, w := bpWritesEnv(t)
	ctx := context.Background()
	good := bpGate("oldie", "go")
	bad := "id: oldie\nschemaVersion: 1\nversion: 1\nlanes: []\n"
	// Seed history DIRECTLY (the store does not Analyze — history can carry
	// bytes that predates today's rules).
	err := s.BlueprintWithTx(ctx, func(tx *sql.Tx) error {
		if err := store.BlueprintPutTx(tx,
			store.BlueprintRow{Name: "oldie", Version: 1, Content: good, ContentHash: strings.Repeat("a", 64), Origin: "user", UpdatedBy: "user:operator"},
			store.RevisionRow{Version: 1, Content: good, ContentHash: strings.Repeat("a", 64), Op: "create", ActorKind: "user", ActorID: "operator"}, nil); err != nil {
			return err
		}
		return store.BlueprintPutTx(tx,
			store.BlueprintRow{Name: "oldie", Version: 1, Content: bad, ContentHash: strings.Repeat("b", 64), Origin: "user", UpdatedBy: "user:operator"},
			store.RevisionRow{Version: 1, Content: bad, ContentHash: strings.Repeat("b", 64), Op: "save", ActorKind: "user", ActorID: "operator"}, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = w.Revert(ctx, bpActor, protocol.BlueprintRevertParams{Name: "oldie", Rev: 2})
	e := mustRefused(t, err, "diagnostics")
	var report blueprint.Report
	if json.Unmarshal([]byte(e.Data.What), &report) != nil || report.Blueprint != "oldie" || report.OK(false) {
		t.Fatalf("the stale-revision refusal did not carry the report: %s", e.Data.What)
	}
	row, _, _ := s.BlueprintGet(ctx, "oldie")
	if row.Content != bad {
		t.Fatal("the refused revert still moved the row")
	}
}

// BP11 is derived, not trusted: every row's refs equal a fresh extraction
// from its own stored bytes.
func TestRefsConsistentWithContentForEveryRow(t *testing.T) {
	d, s, w := bpWritesEnv(t)
	ctx := context.Background()
	if _, err := w.Save(ctx, bpActor, protocol.BlueprintSaveParams{Name: "refs-one", Content: "id: refs-one\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: survey\n    step: {kind: model-step, agent: default}\n"}); err != nil {
		t.Fatal(err)
	}
	list, err := s.BlueprintList(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) < 3 {
		t.Fatalf("expected the shipped pair plus the new row, got %d", len(list))
	}
	for _, row := range list {
		bp, err := blueprint.Load([]byte(row.Content))
		if err != nil {
			t.Fatalf("%s: stored bytes do not load: %v", row.Name, err)
		}
		want := blueprint.References(&bp)
		got, err := s.BlueprintOwnRefs(ctx, row.Name)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != len(want) {
			t.Fatalf("%s: stored %d refs, re-extracted %d", row.Name, len(got), len(want))
		}
		for i, r := range want {
			if got[i].Kind != r.Kind || got[i].Target != r.Target {
				t.Fatalf("%s: ref %d = %+v, want %s/%s", row.Name, i, got[i], r.Kind, r.Target)
			}
		}
	}
	_ = d
}

// BP-live: the reader the next loop.start uses IS the row — save, then
// read, with no restart in between.
func TestWritesAreLiveForNextStart(t *testing.T) {
	d, _, w := bpWritesEnv(t)
	ctx := context.Background()
	if _, err := w.Save(ctx, bpActor, protocol.BlueprintSaveParams{Name: "live-one", Content: bpGate("live-one", "first")}); err != nil {
		t.Fatal(err)
	}
	bp, err := d.loadStartBlueprint("live-one")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := bp.Questions["first"]; !ok {
		t.Fatalf("the start-time reader saw %+v", bp.Questions)
	}
	if _, err := w.Save(ctx, bpActor, protocol.BlueprintSaveParams{Name: "live-one", Content: bpGate("live-one", "second")}); err != nil {
		t.Fatal(err)
	}
	bp, err = d.loadStartBlueprint("live-one")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := bp.Questions["second"]; !ok {
		t.Fatalf("the next start saw stale content: %+v", bp.Questions)
	}
}

// BP-pinned: no door in blueprint_write.go touches loop_blueprint. A pin
// born before any write survives save, duplicate, delete and revert of the
// SAME name with byte-identical content.
func TestPinnedLoopsUntouchedByAnyWrite(t *testing.T) {
	_, s, w := bpWritesEnv(t)
	ctx := context.Background()
	pinned := bpGate("pinme", "go")
	pin, err := s.CreatePinnedLoop(store.NewLoop{Slug: "pinme-1", Title: "pinned", Root: "/repo/p", ComponentRoot: "/repo/p", Mode: "standard", Status: "waiting"}, "pinme", 1, pinned, "{}")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Save(ctx, bpActor, protocol.BlueprintSaveParams{Name: "pinme", Content: bpGate("pinme", "changed")}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Duplicate(ctx, bpActor, protocol.BlueprintDuplicateParams{From: "pinme", Name: "pinme-copy"}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Revert(ctx, bpActor, protocol.BlueprintRevertParams{Name: "pinme", Rev: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Delete(ctx, bpActor, protocol.BlueprintDeleteParams{Name: "pinme"}); err != nil {
		t.Fatal(err)
	}
	pl, err := s.ReadPinnedLoop(pin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pl.Content != pinned {
		t.Fatal("a write door disturbed a pinned loop's bytes")
	}
}

// The T8 guard, pre-positioned: blueprint_write.go is the ONLY daemon file
// that calls store.BlueprintPutTx / store.BlueprintDeleteTx.
func TestStoreWriteServiceIsTheOnlyWriter(t *testing.T) {
	hits := walkRepoGo(t, nil, func(line string) bool {
		return strings.Contains(line, "store.BlueprintPutTx(") || strings.Contains(line, "store.BlueprintDeleteTx(")
	})
	for _, h := range hits {
		rel := strings.SplitN(h, ":", 2)[0]
		if rel == filepath.Join("internal", "daemon", "blueprint_write.go") {
			continue
		}
		if strings.HasPrefix(rel, filepath.Join("internal", "daemon", "")) {
			t.Errorf("a daemon file outside the write service calls the store's write door: %s", h)
		}
	}
	if len(hits) == 0 {
		t.Fatal("the guard found no write-door call at all — the service moved?")
	}
}

// A shipped blueprint is LOCKED: duplicable (the copy is the person's own), not
// deletable and not editable — what a person may still choose rides
// blueprint.override (blueprint_override_test.go).
func TestSystemBlueprintIsLockedDuplicableNotDeletable(t *testing.T) {
	_, s, w := bpWritesEnv(t)
	ctx := context.Background()
	if _, err := w.Delete(ctx, bpActor, protocol.BlueprintDeleteParams{Name: "dev-loop"}); err == nil {
		t.Fatal("a shipped blueprint was deleted")
	}
	res, err := w.Duplicate(ctx, bpActor, protocol.BlueprintDuplicateParams{From: "dev-loop", Name: "dev-loop-fork"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Origin != "user" {
		t.Fatalf("a fork of a shipped row is the person's own: %+v", res)
	}
	fork, _, _ := s.BlueprintGet(ctx, "dev-loop-fork")
	if fork.Origin != "user" {
		t.Fatalf("fork origin %q — a copy is never shipped", fork.Origin)
	}
	revs, _ := s.RevisionList(ctx, "dev-loop", 10)
	before := len(revs)
	_, err = w.Save(ctx, bpActor, protocol.BlueprintSaveParams{Name: "dev-loop", Content: bpGate("dev-loop", "go")})
	mustRefused(t, err, "system")
	revs, _ = s.RevisionList(ctx, "dev-loop", 10)
	if len(revs) != before {
		t.Fatal("a refused save of a shipped row still wrote a revision")
	}
}
