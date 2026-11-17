package store

import "testing"

func TestAttachPinsCurrentRev(t *testing.T) {
	e := newRecordsEnv(t)
	g := mustGoal(t, e, false)
	if _, err := e.s.WriteDocument(e.loop, "plan", "rev one", "agent"); err != nil {
		t.Fatalf("write rev 1: %v", err)
	}
	a, r, err := e.s.Attach(e.loop, "goal", g.ID, "plan", nil)
	if err != nil || r != nil {
		t.Fatalf("attach: %v %+v", err, r)
	}
	if a.Rev != 1 || a.Document != "plan.md" {
		t.Fatalf("pin: rev %d file %q, want rev 1 plan.md", a.Rev, a.Document)
	}
	// A newer revision does NOT move the pin — it makes the pin superseded.
	if _, err := e.s.WriteDocument(e.loop, "plan", "rev two", "agent"); err != nil {
		t.Fatalf("write rev 2: %v", err)
	}
	views, err := e.s.ListAttachments("goal", g.ID)
	if err != nil || len(views) != 1 {
		t.Fatalf("views: %d %v", len(views), err)
	}
	v := views[0]
	if v.Currency != CurrencySuperseded || v.Rev != 1 || v.HeadRev != 2 {
		t.Fatalf("currency after a new rev: %+v, want superseded pin-1 head-2", v)
	}
	// Attaching again while the head is rev 2 is legal — it pins rev 2, a
	// different (subject, document, rev) triple than the rev-1 row.
	a2, r, err := e.s.Attach(e.loop, "goal", g.ID, "plan", nil)
	if err != nil || r != nil || a2.Rev != 2 {
		t.Fatalf("attach rev 2: %+v %v %+v, want rev-2 pin", a2, r, err)
	}
	// Attaching the SAME (subject, document, rev) again is a refusal, not a
	// duplicate row and not a raw constraint error.
	if _, r, err := e.s.Attach(e.loop, "goal", g.ID, "plan", nil); err != nil || r == nil || r.Kind != "already-attached" {
		t.Fatalf("re-attach: %v %+v, want already-attached", err, r)
	}
	views, _ = e.s.ListAttachments("goal", g.ID)
	if len(views) != 2 || views[1].Currency != CurrencyCurrent || views[1].Rev != 2 {
		t.Fatalf("fresh pin must be current: %+v", views)
	}
}

func TestAttachmentCurrencyStates(t *testing.T) {
	e := newRecordsEnv(t)
	if _, err := e.s.WriteDocument(e.loop, "plan", "content", "agent"); err != nil {
		t.Fatalf("write: %v", err)
	}
	// An unknown document is refused before any row.
	if _, r, err := e.s.Attach(e.loop, "goal", "goal-x", "nonexistent-doc", nil); err != nil || r == nil || r.Kind != "document-unknown" {
		t.Fatalf("unknown doc: %v %+v", err, r)
	}
	// A bad subject kind is refused too (the CHECK is the net; this is the door).
	if _, r, err := e.s.Attach(e.loop, "daydream", "x", "plan", nil); err != nil || r == nil || r.Kind != "subject-kind-unknown" {
		t.Fatalf("bad subject kind: %v %+v", err, r)
	}
	a, r, err := e.s.Attach(e.loop, "task", "tsk-1", "plan", nil)
	if err != nil || r != nil {
		t.Fatalf("attach: %v %+v", err, r)
	}
	views, _ := e.s.ListAttachments("task", "tsk-1")
	if len(views) != 1 || views[0].Currency != CurrencyCurrent {
		t.Fatalf("fresh attachment: %+v, want current", views)
	}
	// Archive the loop: the document rows are gone, and the attachment says
	// MISSING — not current, not superseded, not an error.
	if _, err := e.s.ArchiveLoop(e.loop, "done with it"); err != nil {
		t.Fatalf("archive: %v", err)
	}
	views, err = e.s.ListAttachments("task", "tsk-1")
	if err != nil || len(views) != 1 {
		t.Fatalf("views after archive: %d %v", len(views), err)
	}
	if views[0].Currency != CurrencyMissing || views[0].HeadRev != 0 {
		t.Fatalf("archived attachment: %+v, want missing with no head", views[0])
	}
	_ = a
}
