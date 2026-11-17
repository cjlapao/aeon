package store

// verify_refusals_test.go — S57 T3's DB19 tests: every refusal is a
// discriminated VerifyRefusal with a sentence (never an error, never a
// silent no-write), and the apply-template door is all or nothing.

import (
	"strings"
	"testing"

	"gorm.io/gorm"
)

// s57SameGate compares two gates field for field. The struct's pointer
// fields (MemoryMB, TemplateID, ArchivedAt) would otherwise compare by
// pointer identity, and a freshly scanned row points at freshly scanned
// strings.
func s57SameGate(a, b VerifyCommand) bool {
	if (a.MemoryMB == nil) != (b.MemoryMB == nil) ||
		(a.TemplateID == nil) != (b.TemplateID == nil) ||
		(a.ArchivedAt == nil) != (b.ArchivedAt == nil) {
		return false
	}
	if a.MemoryMB != nil && *a.MemoryMB != *b.MemoryMB {
		return false
	}
	if a.TemplateID != nil && *a.TemplateID != *b.TemplateID {
		return false
	}
	if a.ArchivedAt != nil && *a.ArchivedAt != *b.ArchivedAt {
		return false
	}
	x, y := a, b
	x.MemoryMB, x.TemplateID, x.ArchivedAt = nil, nil, nil
	y.MemoryMB, y.TemplateID, y.ArchivedAt = nil, nil, nil
	return x == y
}

// s57SeedTemplates plants two system templates the apply tests name by id.
func s57SeedTemplates(t *testing.T, g *gorm.DB) {
	t.Helper()
	rows := []string{
		`INSERT INTO verify_template (id, stack, name, command, requirement, timeout_s, origin, version, created_at, updated_at)
		 VALUES ('vt1','go','build','go build ./...','required',300,'system','1','t','t')`,
		`INSERT INTO verify_template (id, stack, name, command, requirement, timeout_s, origin, version, created_at, updated_at)
		 VALUES ('vt2','go','test','go test ./...','advisory',300,'system','1','t','t')`,
	}
	for _, stmt := range rows {
		if err := g.Exec(stmt).Error; err != nil {
			t.Fatalf("seed the templates: %v", err)
		}
	}
}

// TestRefusalsAreDiscriminated — each refusal the spec names answers its
// VerifyRefusal kind with a non-empty sentence and a nil error, and writes
// nothing: no gate row, no event row, on every one of them.
func TestRefusalsAreDiscriminated(t *testing.T) {
	g, s := testStore(t)
	seedComponent(t, g, "rf-root")
	seedComponent(t, g, "rf-dead")
	s57ArchiveComponent(t, g, "rf-dead")
	base := s57Gate(t, s, s57User, NewVerifyCommand{ComponentID: "rf-root", Name: "base", Command: "true", Requirement: "required", TimeoutS: 60})

	longCmd := strings.Repeat("x", 4097)
	memory := -5
	cases := []struct {
		name string
		want VerifyRefusal
		do   func() (VerifyRefusal, string, error)
	}{
		{"add unknown component", VerifyRefusalComponentUnknown, func() (VerifyRefusal, string, error) {
			w, err := s.AddVerifyCommand(s57User, NewVerifyCommand{ComponentID: "ghost", Name: "g", Command: "true", Requirement: "required", TimeoutS: 60})
			return w.Refusal, w.Reason, err
		}},
		{"add on an archived component", VerifyRefusalComponentArchived, func() (VerifyRefusal, string, error) {
			w, err := s.AddVerifyCommand(s57User, NewVerifyCommand{ComponentID: "rf-dead", Name: "g", Command: "true", Requirement: "required", TimeoutS: 60})
			return w.Refusal, w.Reason, err
		}},
		{"add a non-slug name", VerifyRefusalNameNotASlug, func() (VerifyRefusal, string, error) {
			w, err := s.AddVerifyCommand(s57User, NewVerifyCommand{ComponentID: "rf-root", Name: "Build", Command: "true", Requirement: "required", TimeoutS: 60})
			return w.Refusal, w.Reason, err
		}},
		{"add a taken name", VerifyRefusalNameTaken, func() (VerifyRefusal, string, error) {
			w, err := s.AddVerifyCommand(s57User, NewVerifyCommand{ComponentID: "rf-root", Name: "base", Command: "true", Requirement: "required", TimeoutS: 60})
			return w.Refusal, w.Reason, err
		}},
		{"add an empty command", VerifyRefusalCommandEmpty, func() (VerifyRefusal, string, error) {
			w, err := s.AddVerifyCommand(s57User, NewVerifyCommand{ComponentID: "rf-root", Name: "g", Command: "", Requirement: "required", TimeoutS: 60})
			return w.Refusal, w.Reason, err
		}},
		{"add a command over 4096", VerifyRefusalCommandTooLong, func() (VerifyRefusal, string, error) {
			w, err := s.AddVerifyCommand(s57User, NewVerifyCommand{ComponentID: "rf-root", Name: "g", Command: longCmd, Requirement: "required", TimeoutS: 60})
			return w.Refusal, w.Reason, err
		}},
		{"add a NUL command", VerifyRefusalCommandNUL, func() (VerifyRefusal, string, error) {
			w, err := s.AddVerifyCommand(s57User, NewVerifyCommand{ComponentID: "rf-root", Name: "g", Command: "a\x00b", Requirement: "required", TimeoutS: 60})
			return w.Refusal, w.Reason, err
		}},
		{"add a zero timeout", VerifyRefusalTimeoutOutOfRange, func() (VerifyRefusal, string, error) {
			w, err := s.AddVerifyCommand(s57User, NewVerifyCommand{ComponentID: "rf-root", Name: "g", Command: "true", Requirement: "required", TimeoutS: 0})
			return w.Refusal, w.Reason, err
		}},
		{"add an over-range timeout", VerifyRefusalTimeoutOutOfRange, func() (VerifyRefusal, string, error) {
			w, err := s.AddVerifyCommand(s57User, NewVerifyCommand{ComponentID: "rf-root", Name: "g", Command: "true", Requirement: "required", TimeoutS: 601})
			return w.Refusal, w.Reason, err
		}},
		{"add a bad requirement", VerifyRefusalRequirementInvalid, func() (VerifyRefusal, string, error) {
			w, err := s.AddVerifyCommand(s57User, NewVerifyCommand{ComponentID: "rf-root", Name: "g", Command: "true", Requirement: "blocking", TimeoutS: 60})
			return w.Refusal, w.Reason, err
		}},
		{"add a non-positive memory", VerifyRefusalMemoryNonPositive, func() (VerifyRefusal, string, error) {
			w, err := s.AddVerifyCommand(s57User, NewVerifyCommand{ComponentID: "rf-root", Name: "g", Command: "true", Requirement: "required", TimeoutS: 60, MemoryMB: &memory})
			return w.Refusal, w.Reason, err
		}},
		{"add by an agent", VerifyRefusalActorNotHuman, func() (VerifyRefusal, string, error) {
			w, err := s.AddVerifyCommand(Actor{Kind: "agent", ID: "a1"}, NewVerifyCommand{ComponentID: "rf-root", Name: "g", Command: "true", Requirement: "required", TimeoutS: 60})
			return w.Refusal, w.Reason, err
		}},
		{"add by an anonymous user", VerifyRefusalActorNotHuman, func() (VerifyRefusal, string, error) {
			w, err := s.AddVerifyCommand(Actor{Kind: "user", ID: ""}, NewVerifyCommand{ComponentID: "rf-root", Name: "g", Command: "true", Requirement: "required", TimeoutS: 60})
			return w.Refusal, w.Reason, err
		}},
		{"update a negative position", VerifyRefusalPositionOutOfRange, func() (VerifyRefusal, string, error) {
			w, err := s.UpdateVerifyCommand(s57User, base.ID, VerifyCommandUpdate{Position: s57ptr(-1)})
			return w.Refusal, w.Reason, err
		}},
		{"update an unknown gate", VerifyRefusalCommandUnknown, func() (VerifyRefusal, string, error) {
			w, err := s.UpdateVerifyCommand(s57User, "vc-none", VerifyCommandUpdate{})
			return w.Refusal, w.Reason, err
		}},
		{"archive an unknown gate", VerifyRefusalCommandUnknown, func() (VerifyRefusal, string, error) {
			w, err := s.ArchiveVerifyCommand(s57User, "vc-none")
			return w.Refusal, w.Reason, err
		}},
		{"restore an unknown gate", VerifyRefusalCommandUnknown, func() (VerifyRefusal, string, error) {
			w, err := s.RestoreVerifyCommand(s57User, "vc-none")
			return w.Refusal, w.Reason, err
		}},
	}
	for _, c := range cases {
		refusal, reason, err := c.do()
		if err != nil {
			t.Errorf("%s: answered an error %v — a refusal is a typed result", c.name, err)
			continue
		}
		if refusal != c.want {
			t.Errorf("%s: refusal %q, want %q", c.name, refusal, c.want)
		}
		if reason == "" {
			t.Errorf("%s: no sentence to render", c.name)
		}
	}
	// None of the refusals wrote a row or an event.
	if n := countTable(t, g, "verify_command"); n != 1 {
		t.Errorf("verify_command rows = %d, want 1 (the seed gate)", n)
	}
	if n := s57EventCount(t, s, base.ID); n != 1 {
		t.Errorf("seed gate events = %d, want 1 (its add)", n)
	}

	// Update on an archived gate is its own refusal (restore it first).
	if w, err := s.ArchiveVerifyCommand(s57User, base.ID); err != nil || !w.OK() {
		t.Fatalf("archive the seed gate: %v %v", w, err)
	}
	if w, err := s.UpdateVerifyCommand(s57User, base.ID, VerifyCommandUpdate{Command: s57ptr("false")}); err != nil || w.Refusal != VerifyRefusalCommandArchived {
		t.Errorf("update on archived: refusal %q err %v, want %q", w.Refusal, err, VerifyRefusalCommandArchived)
	}
	if w, err := s.RestoreVerifyCommand(s57User, base.ID); err != nil || !w.OK() {
		t.Fatalf("restore the seed gate: %v %v", w, err)
	}
}

// TestApplyTemplateIsAllOrNothing — VG21/VG22: a name collision anywhere in
// the set (against a live gate of the node, or a duplicate inside the set)
// refuses the WHOLE apply with every collision named, and writes nothing;
// the accepted copy is origin='template', points at its template, takes its
// event row, and lands after the node's current last gate.
func TestApplyTemplateIsAllOrNothing(t *testing.T) {
	g, s := testStore(t)
	seedComponent(t, g, "ap-root")
	seedComponent(t, g, "ap-bare")
	s57SeedTemplates(t, g)
	// A live gate whose name collides with template vt1.
	s57Gate(t, s, s57User, NewVerifyCommand{ComponentID: "ap-root", Name: "build", Command: "make build", Requirement: "required", TimeoutS: 300})

	// The collision case: one bad name refuses the whole set, nothing is
	// written, and the sentence names the collision.
	ap, err := s.ApplyVerifyTemplates(s57User, "ap-root", []string{"vt1", "vt2"})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if ap.Refusal != VerifyRefusalTemplateCollision || !strings.Contains(ap.Reason, "build") {
		t.Fatalf("apply refused: %q (%q) — want template-name-collision naming build", ap.Refusal, ap.Reason)
	}
	if n := countTable(t, g, "verify_command"); n != 1 {
		t.Fatalf("the refused apply wrote rows: %d gates", n)
	}

	// An unknown template refuses too.
	if ap, err := s.ApplyVerifyTemplates(s57User, "ap-root", []string{"vt1", "nope"}); err != nil || ap.Refusal != VerifyRefusalTemplateUnknown {
		t.Errorf("unknown template: %v %v", ap, err)
	}

	// The accepted case: vt2 alone copies one gate, template-stamped, with
	// its apply_template event, at the next position.
	ap, err = s.ApplyVerifyTemplates(s57User, "ap-root", []string{"vt2"})
	if err != nil || !ap.OK() || len(ap.Commands) != 1 {
		t.Fatalf("apply vt2: %v %v", ap, err)
	}
	got := ap.Commands[0]
	if got.Name != "test" || got.Origin != "template" || got.TemplateID == nil || *got.TemplateID != "vt2" || got.Position != 1 {
		t.Errorf("applied gate: name %s origin %s template %v position %d — want test/template/vt2/after-the-live-gate", got.Name, got.Origin, got.TemplateID, got.Position)
	}
	if w, err := s.GetVerifyCommand(got.ID); err != nil {
		t.Errorf("the applied gate did not land: %v", err)
	} else if !s57SameGate(w, got) {
		t.Errorf("the applied gate changed on the way to the store: %+v, want %+v", w, got)
	}
	evs, err := s.ListVerifyCommandEvents(VerifyCommandEventFilter{CommandID: got.ID})
	if err != nil || len(evs) != 1 || evs[0].Op != "apply_template" || evs[0].Revision != 1 {
		t.Errorf("applied gate events: %v %v — want one rev-1 apply_template", evs, err)
	}

	// Re-applying the same template now collides with its own copy.
	if ap, err := s.ApplyVerifyTemplates(s57User, "ap-root", []string{"vt2"}); err != nil || ap.Refusal != VerifyRefusalTemplateCollision {
		t.Errorf("re-apply: %v %v — want the collision with its own copy", ap, err)
	}
	if n := countTable(t, g, "verify_command"); n != 2 {
		t.Errorf("gates after the refused re-apply = %d, want 2", n)
	}

	// A duplicate inside the set is a collision with itself: nothing lands
	// even where no live gate is in the way.
	if ap, err := s.ApplyVerifyTemplates(s57User, "ap-bare", []string{"vt2", "vt2"}); err != nil || ap.Refusal != VerifyRefusalTemplateCollision || !strings.Contains(ap.Reason, "twice") {
		t.Errorf("in-set duplicate: %v %v — want the collision named", ap, err)
	}
	if n := countTable(t, g, "verify_command"); n != 2 {
		t.Errorf("the in-set duplicate wrote rows: %d gates", n)
	}

	// The read the picker uses: the live templates, grouped by stack.
	tpls, err := s.ListVerifyTemplates()
	if err != nil || len(tpls) != 2 || tpls[0].Origin != "system" {
		t.Errorf("templates: %v %v — want the two seeded system rows", tpls, err)
	}
}
