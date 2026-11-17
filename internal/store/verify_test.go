package store

// verify_test.go — S57 T3's shared store helper and the audit-discipline
// tests (P3): one event per changing write, none for reads/refusals/no-ops,
// the same-transaction rollback proof, and SEC-033's fixed-template summaries.

import (
	"errors"
	"strings"
	"testing"

	"gorm.io/gorm"
)

// s57User is the person the gate writes name (VG23: a direct write is a
// person's act).
var s57User = Actor{Kind: "user", ID: "u1"}

// testStore opens a live store on a throwaway directory — the framework's
// own Open, so every migration (including S57's M1) has already been walked,
// and returns the GORM handle beside it for the raw assertions.
func testStore(t *testing.T) (*gorm.DB, *Store) {
	t.Helper()
	s, err := Open(Options{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("open the test store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s.GormDB(), s
}

// s57SeedChild inserts a child component under its parent (seedComponent
// only makes roots).
func s57SeedChild(t *testing.T, g *gorm.DB, id, parent string) {
	t.Helper()
	if err := g.Exec(`INSERT INTO component (id,name,slug,created_at,updated_at,parent_id) VALUES (?,?,?,?,?,?)`,
		id, id, id, "2026-11-02T04:00:00Z", "2026-11-02T04:00:00Z", parent).Error; err != nil {
		t.Fatalf("seed child %s: %v", id, err)
	}
}

// s57ArchiveComponent freezes a node the way the tree does (archived_at set).
func s57ArchiveComponent(t *testing.T, g *gorm.DB, id string) {
	t.Helper()
	if err := g.Exec(`UPDATE component SET archived_at = '2026-11-02T04:00:00Z' WHERE id = ?`, id).Error; err != nil {
		t.Fatalf("archive component %s: %v", id, err)
	}
}

// s57Gate adds a gate through the API and fails the test on a refusal.
func s57Gate(t *testing.T, s *Store, a Actor, in NewVerifyCommand) VerifyCommand {
	t.Helper()
	w, err := s.AddVerifyCommand(a, in)
	if err != nil {
		t.Fatalf("add gate %s: %v", in.Name, err)
	}
	if !w.OK() {
		t.Fatalf("add gate %s refused: %s — %s", in.Name, w.Refusal, w.Reason)
	}
	return w.Command
}

// s57ptr is the tests' local pointer shorthand (the package has no such
// helper; a test-file-local one keeps the refusals table readable).
func s57ptr[T any](v T) *T { return &v }

// s57EventCount is the audit's depth for one command.
func s57EventCount(t *testing.T, s *Store, commandID string) int {
	t.Helper()
	evs, err := s.ListVerifyCommandEvents(VerifyCommandEventFilter{CommandID: commandID})
	if err != nil {
		t.Fatalf("list gate events: %v", err)
	}
	return len(evs)
}

// TestEveryChangingWriteWritesOneEvent — the P3 half: add, update, archive
// and restore each write exactly one event row in the same transaction as
// the state change (the event's `at` is the SAME store-clock reading as the
// row's timestamp), the per-command revisions run 1..4 without a gap, and a
// rolled-back write leaves neither the row nor its event.
func TestEveryChangingWriteWritesOneEvent(t *testing.T) {
	g, s := testStore(t)
	seedComponent(t, g, "ev-root")

	cmd := s57Gate(t, s, s57User, NewVerifyCommand{
		ComponentID: "ev-root", Name: "build", Command: "go build ./...", Requirement: "required", TimeoutS: 300,
	})
	evs, err := s.ListVerifyCommandEvents(VerifyCommandEventFilter{CommandID: cmd.ID})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(evs) != 1 || evs[0].Revision != 1 || evs[0].Op != "add" {
		t.Fatalf("after add: %d events, want one rev-1 add", len(evs))
	}
	if evs[0].BeforeJSON != nil || evs[0].AfterJSON == nil {
		t.Errorf("add event: before must be NULL and after set (the row did not exist)")
	}
	if evs[0].At != cmd.CreatedAt {
		t.Errorf("add event at %s != the row's created_at %s — not the same clock reading", evs[0].At, cmd.CreatedAt)
	}

	next := "go test ./..."
	w, err := s.UpdateVerifyCommand(s57User, cmd.ID, VerifyCommandUpdate{Command: &next})
	if err != nil || !w.OK() {
		t.Fatalf("update: %v %v", w, err)
	}
	evs, _ = s.ListVerifyCommandEvents(VerifyCommandEventFilter{CommandID: cmd.ID})
	if len(evs) != 2 || evs[1].Revision != 2 || evs[1].Op != "update" {
		t.Fatalf("after update: %d events, want rev-2 update", len(evs))
	}
	if evs[1].BeforeJSON == nil || evs[1].AfterJSON == nil {
		t.Errorf("update event: both snapshots must be set")
	}
	if evs[1].At != w.Command.UpdatedAt {
		t.Errorf("update event at %s != updated_at %s", evs[1].At, w.Command.UpdatedAt)
	}

	if w, err := s.ArchiveVerifyCommand(s57User, cmd.ID); err != nil || !w.OK() || w.Command.ArchivedAt == nil {
		t.Fatalf("archive: %v %v", w, err)
	}
	evs, _ = s.ListVerifyCommandEvents(VerifyCommandEventFilter{CommandID: cmd.ID})
	if len(evs) != 3 || evs[2].Revision != 3 || evs[2].Op != "archive" {
		t.Fatalf("after archive: %d events, want rev-3 archive", len(evs))
	}
	if evs[2].AfterJSON != nil {
		t.Errorf("archive event: after must be NULL (the live row no longer exists)")
	}

	if w, err := s.RestoreVerifyCommand(s57User, cmd.ID); err != nil || !w.OK() || w.Command.ArchivedAt != nil {
		t.Fatalf("restore: %v %v", w, err)
	}
	evs, _ = s.ListVerifyCommandEvents(VerifyCommandEventFilter{CommandID: cmd.ID})
	if len(evs) != 4 {
		t.Fatalf("after restore: %d events, want 4", len(evs))
	}
	wantOps := []string{"add", "update", "archive", "restore"}
	for i, e := range evs {
		if e.Revision != i+1 || e.Op != wantOps[i] {
			t.Errorf("event %d: revision %d op %q, want %d %q", i, e.Revision, e.Op, i+1, wantOps[i])
		}
	}

	// A rolled-back write leaves nothing: the row and its event were one
	// transaction, so the rollback undoes both.
	tx, err := s.w.Begin()
	if err != nil {
		t.Fatalf("begin the rollback probe: %v", err)
	}
	now := s.Now()
	probe := VerifyCommand{
		ID: "vc-rollback-probe", ComponentID: "ev-root", Name: "rolled", Command: "echo rollback",
		Requirement: "required", TimeoutS: 60, Origin: "user", Position: 5, CreatedAt: now, UpdatedAt: now,
	}
	if err := insertVerifyCommand(tx, probe); err != nil {
		t.Fatalf("probe insert: %v", err)
	}
	if err := appendGateEvent(tx, s57User, probe, 1, "add", now, nil, &probe); err != nil {
		t.Fatalf("probe event: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("roll the probe back: %v", err)
	}
	if _, err := s.GetVerifyCommand("vc-rollback-probe"); !errors.Is(err, ErrNotFound) {
		t.Errorf("rolled-back gate is visible: %v", err)
	}
	if n := s57EventCount(t, s, "vc-rollback-probe"); n != 0 {
		t.Errorf("rolled-back gate left %d events", n)
	}
	if n := countTable(t, g, "verify_command"); n != 1 {
		t.Errorf("verify_command rows = %d, want 1", n)
	}
}

// TestNoEventForReadsRefusalsNoOps — the other half of P3: a read, a refusal
// and a no-op write each leave the audit exactly as they found it. The
// audit's depth counts only the writes that actually changed the row.
func TestNoEventForReadsRefusalsNoOps(t *testing.T) {
	g, s := testStore(t)
	seedComponent(t, g, "noev-root")
	base := s57Gate(t, s, s57User, NewVerifyCommand{
		ComponentID: "noev-root", Name: "build", Command: "go build ./...", Requirement: "required", TimeoutS: 300,
	})
	if n := s57EventCount(t, s, base.ID); n != 1 {
		t.Fatalf("seed: %d events, want 1", n)
	}

	// Reads: none of them may write.
	if _, err := s.GetVerifyCommand(base.ID); err != nil {
		t.Fatalf("read: %v", err)
	}
	if _, err := s.ListVerifyCommands(VerifyCommandFilter{ComponentID: "noev-root", IncludeArchived: true}); err != nil {
		t.Fatalf("list: %v", err)
	}
	if _, err := s.EffectiveGates("noev-root"); err != nil {
		t.Fatalf("effective: %v", err)
	}
	if _, err := s.ListVerifyRuns(VerifyRunFilter{}); err != nil {
		t.Fatalf("runs: %v", err)
	}
	if _, _, err := s.LatestPassingRun("noev-root", "alpha", "sha"); err != nil {
		t.Fatalf("latest passing: %v", err)
	}
	if _, err := s.ListVerifyTemplates(); err != nil {
		t.Fatalf("templates: %v", err)
	}
	if n := s57EventCount(t, s, base.ID); n != 1 {
		t.Fatalf("reads wrote %d extra events", n-1)
	}

	// Refusals: each refused write leaves no row and no event.
	refusals := []struct {
		name string
		do   func() (VerifyRefusal, string, error)
	}{
		{"add not-a-slug", func() (VerifyRefusal, string, error) {
			w, err := s.AddVerifyCommand(s57User, NewVerifyCommand{ComponentID: "noev-root", Name: "Build", Command: "true", Requirement: "required", TimeoutS: 60})
			return w.Refusal, w.Reason, err
		}},
		{"add empty command", func() (VerifyRefusal, string, error) {
			w, err := s.AddVerifyCommand(s57User, NewVerifyCommand{ComponentID: "noev-root", Name: "build2", Command: "", Requirement: "required", TimeoutS: 60})
			return w.Refusal, w.Reason, err
		}},
		{"add taken name", func() (VerifyRefusal, string, error) {
			w, err := s.AddVerifyCommand(s57User, NewVerifyCommand{ComponentID: "noev-root", Name: "build", Command: "true", Requirement: "required", TimeoutS: 60})
			return w.Refusal, w.Reason, err
		}},
		{"add unknown component", func() (VerifyRefusal, string, error) {
			w, err := s.AddVerifyCommand(s57User, NewVerifyCommand{ComponentID: "ghost", Name: "build", Command: "true", Requirement: "required", TimeoutS: 60})
			return w.Refusal, w.Reason, err
		}},
		{"add by an agent", func() (VerifyRefusal, string, error) {
			w, err := s.AddVerifyCommand(Actor{Kind: "agent", ID: "a1"}, NewVerifyCommand{ComponentID: "noev-root", Name: "build2", Command: "true", Requirement: "required", TimeoutS: 60})
			return w.Refusal, w.Reason, err
		}},
		{"update unknown id", func() (VerifyRefusal, string, error) {
			w, err := s.UpdateVerifyCommand(s57User, "vc-none", VerifyCommandUpdate{})
			return w.Refusal, w.Reason, err
		}},
		{"archive unknown id", func() (VerifyRefusal, string, error) {
			w, err := s.ArchiveVerifyCommand(s57User, "vc-none")
			return w.Refusal, w.Reason, err
		}},
		{"restore unknown id", func() (VerifyRefusal, string, error) {
			w, err := s.RestoreVerifyCommand(s57User, "vc-none")
			return w.Refusal, w.Reason, err
		}},
	}
	for _, r := range refusals {
		refusal, reason, err := r.do()
		if err != nil || refusal == VerifyRefusalNone || reason == "" {
			t.Errorf("%s: refusal %q reason %q err %v — every refusal is a typed sentence, not an error", r.name, refusal, reason, err)
		}
	}
	if n := s57EventCount(t, s, base.ID); n != 1 {
		t.Fatalf("refusals wrote %d extra events", n-1)
	}
	if n := countTable(t, g, "verify_command"); n != 1 {
		t.Fatalf("refusals wrote %d rows", n-1)
	}

	// No-ops: an update that changes nothing, a re-archive and a re-restore
	// each return the row and write nothing.
	if w, err := s.UpdateVerifyCommand(s57User, base.ID, VerifyCommandUpdate{Command: &base.Command}); err != nil || !w.OK() {
		t.Fatalf("no-op update: %v %v", w, err)
	}
	if n := s57EventCount(t, s, base.ID); n != 1 {
		t.Fatalf("no-op update wrote an event")
	}
	if w, err := s.ArchiveVerifyCommand(s57User, base.ID); err != nil || !w.OK() {
		t.Fatalf("archive: %v %v", w, err)
	}
	if n := s57EventCount(t, s, base.ID); n != 2 {
		t.Fatalf("archive: %d events, want 2", n)
	}
	if w, err := s.ArchiveVerifyCommand(s57User, base.ID); err != nil || !w.OK() {
		t.Fatalf("re-archive no-op: %v %v", w, err)
	}
	if n := s57EventCount(t, s, base.ID); n != 2 {
		t.Fatalf("re-archive wrote an event")
	}
	if w, err := s.UpdateVerifyCommand(s57User, base.ID, VerifyCommandUpdate{Command: s57ptr("go vet ./...")}); err != nil || w.Refusal != VerifyRefusalCommandArchived {
		t.Fatalf("update on archived: %v %v", w, err)
	}
	if w, err := s.RestoreVerifyCommand(s57User, base.ID); err != nil || !w.OK() {
		t.Fatalf("restore: %v %v", w, err)
	}
	if n := s57EventCount(t, s, base.ID); n != 3 {
		t.Fatalf("restore: %d events, want 3", n)
	}
	if w, err := s.RestoreVerifyCommand(s57User, base.ID); err != nil || !w.OK() {
		t.Fatalf("re-restore no-op: %v %v", w, err)
	}
	if w, err := s.UpdateVerifyCommand(s57User, base.ID, VerifyCommandUpdate{TimeoutS: s57ptr(300)}); err != nil || !w.OK() {
		t.Fatalf("no-op update (timeout same): %v %v", w, err)
	}
	evs, _ := s.ListVerifyCommandEvents(VerifyCommandEventFilter{CommandID: base.ID})
	if len(evs) != 3 {
		t.Fatalf("final depth %d, want 3 (add, archive, restore)", len(evs))
	}
	for i, op := range []string{"add", "archive", "restore"} {
		if evs[i].Op != op {
			t.Errorf("event %d op %q, want %q", i, evs[i].Op, op)
		}
	}
}

// TestSummariesContainNoUserText — SEC-033: the event summary is a fixed
// template built from the op, the validated slug and the enums. The command
// is free user text and may appear in the snapshot (the diffable data) but
// never in the sentence a person or an agent reads.
func TestSummariesContainNoUserText(t *testing.T) {
	g, s := testStore(t)
	seedComponent(t, g, "sum-root")
	marker := "UNIQUE_USER_TEXT_MKR_01"
	marker2 := "UNIQUE_USER_TEXT_MKR_02"

	cmd := s57Gate(t, s, s57User, NewVerifyCommand{
		ComponentID: "sum-root", Name: "secret", Command: "echo " + marker, Requirement: "required", TimeoutS: 300,
	})
	next := "curl " + marker2
	if w, err := s.UpdateVerifyCommand(s57User, cmd.ID, VerifyCommandUpdate{Command: &next}); err != nil || !w.OK() {
		t.Fatalf("update: %v %v", w, err)
	}
	if w, err := s.ArchiveVerifyCommand(s57User, cmd.ID); err != nil || !w.OK() {
		t.Fatalf("archive: %v %v", w, err)
	}
	if w, err := s.RestoreVerifyCommand(s57User, cmd.ID); err != nil || !w.OK() {
		t.Fatalf("restore: %v %v", w, err)
	}
	// A template whose command also carries user text: applying it must not
	// leak that text into the sentence either.
	if err := g.Exec(`INSERT INTO verify_template (id, stack, name, command, requirement, timeout_s, origin, version, created_at, updated_at)
		VALUES ('vt-marker','go','fromtmpl','true && echo ` + marker2 + `','advisory',60,'system','1','t','t')`).Error; err != nil {
		t.Fatalf("seed the marker template: %v", err)
	}
	ap, err := s.ApplyVerifyTemplates(s57User, "sum-root", []string{"vt-marker"})
	if err != nil || !ap.OK() || len(ap.Commands) != 1 {
		t.Fatalf("apply the template: %v %v", ap, err)
	}

	// Revision is per-command, so check each command's own story: the secret
	// gate runs add→update→archive→restore, the applied gate its one
	// apply_template.
	secret, err := s.ListVerifyCommandEvents(VerifyCommandEventFilter{CommandID: cmd.ID})
	if err != nil {
		t.Fatalf("list the secret events: %v", err)
	}
	wantSecret := []string{
		`add gate "secret" (required, 300 s)`,
		`update gate "secret"`,
		`archive gate "secret"`,
		`restore gate "secret"`,
	}
	if len(secret) != len(wantSecret) {
		t.Fatalf("secret gate: %d events, want %d", len(secret), len(wantSecret))
	}
	for i, e := range secret {
		if e.Revision != i+1 {
			t.Errorf("secret event %d: revision %d, want %d", i, e.Revision, i+1)
		}
		if wantSecret[i] != e.Summary {
			t.Errorf("secret rev %d summary %q, want %q", e.Revision, e.Summary, wantSecret[i])
		}
		for _, m := range []string{marker, marker2} {
			if strings.Contains(e.Summary, m) {
				t.Errorf("secret rev %d summary carries user text: %q", e.Revision, e.Summary)
			}
		}
	}
	applied, err := s.ListVerifyCommandEvents(VerifyCommandEventFilter{CommandID: ap.Commands[0].ID})
	if err != nil {
		t.Fatalf("list the applied events: %v", err)
	}
	if len(applied) != 1 || applied[0].Revision != 1 || applied[0].Summary != `apply template as gate "fromtmpl"` {
		t.Fatalf("applied gate events: %+v — want one rev-1 apply_template", applied)
	}
	for _, m := range []string{marker, marker2} {
		if strings.Contains(applied[0].Summary, m) {
			t.Errorf("applied summary carries user text: %q", applied[0].Summary)
		}
	}
	// The boundary, pinned: the record's fields (including the command) live
	// in the snapshot — the diffable data — and nowhere else in the audit.
	if secret[0].AfterJSON == nil || !strings.Contains(*secret[0].AfterJSON, marker) {
		t.Errorf("the add event's after snapshot must carry the command text (the audit is diffable)")
	}
}
