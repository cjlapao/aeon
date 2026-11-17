package triage

// ════════════════════════════════════════════════════════════════════════════
// S51 T2 — the edit, the two sentences, the closed sets, the history.
//
// TM1 is a small law with a long tail: title and body are the only
// editable fields, so THE TEST of that is byte-identity of everything
// else. The describe test is D2 as a property: every combination of the
// two sets renders TWO fragments that never merge. The vocabulary test
// refuses to restate the sets into a second source of truth. The history
// test keeps "no rows" and "no bug" as the two different facts they are.
// ════════════════════════════════════════════════════════════════════════════

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func personActor() Actor { return Actor{Kind: "user", ID: "42"} }

func TestUpdateChangesOnlyTitleAndBody(t *testing.T) {
	tr, _ := openTriageStore(t, "/r")
	bug, r := tr.CreateAudited("/r", NewBug{
		Title: "original title", Body: "original body",
		PriorityState: PriorityKnown, PriorityLabel: "P1", FiledBy: "person",
	}, personActor())
	if r != nil {
		t.Fatalf("seed: %v", r)
	}
	other, r := tr.CreateAudited("/r", NewBug{Title: "the original of the link", Body: "the first report", FiledBy: "person"}, personActor())
	if r != nil {
		t.Fatalf("seed 2: %v", r)
	}
	if _, rr := tr.ConfirmDuplicateAudited(bug.ID, "/r", other.ID, "carlos", personActor()); rr != nil {
		t.Fatalf("link: %v", rr)
	}
	if _, rr := tr.RecordHandoffAudited(bug.ID, "/r", 777, personActor()); rr != nil {
		t.Fatalf("handoff: %v", rr)
	}
	before, ok, err := tr.Show(bug.ID)
	if err != nil || !ok {
		t.Fatalf("show: %v %v", ok, err)
	}

	newTitle := "edited title"
	got, changed, r := tr.Update(bug.ID, UpdateBug{Title: &newTitle}, personActor())
	if r != nil {
		t.Fatalf("update title: %v", r)
	}
	if !changed {
		t.Fatal("an edit that moved the title answered changed:false")
	}
	// Everything TM1 does not own must read EXACTLY as it did — field by
	// field, not vibes. Only Title and UpdatedAt may differ.
	if got.Title != newTitle {
		t.Errorf("title: got %q want %q", got.Title, newTitle)
	}
	if got.Body != before.Body {
		t.Errorf("body moved on a title-only edit: %q", got.Body)
	}
	if got.ComponentRoot != before.ComponentRoot || got.ID != before.ID {
		t.Error("identity fields moved")
	}
	if got.Validation != before.Validation {
		t.Errorf("validation state moved on an edit (D3: an edit never re-opens validation): %q", got.Validation)
	}
	if got.PriorityState != before.PriorityState || got.PriorityLabel != before.PriorityLabel {
		t.Errorf("priority moved on an edit: %q/%q", got.PriorityState, got.PriorityLabel)
	}
	if got.FiledBy != before.FiledBy {
		t.Errorf("filed_by moved on an edit (TM3): %q", got.FiledBy)
	}
	// Pointers print as ADDRESSES, and a re-read is a fresh struct — the
	// comparison must look through them (a test that compared Sprint(ptr)
	// would have failed forever and taught nothing).
	if deref(got.DuplicateOf) != deref(before.DuplicateOf) ||
		deref(got.RoadmapItemID) != deref(before.RoadmapItemID) {
		t.Errorf("the link and the hand-off moved on an edit: %d/%v vs %d/%v",
			got.ID, deref(got.DuplicateOf), deref(got.RoadmapItemID), deref(before.DuplicateOf))
	}
	if got.CreatedAt != before.CreatedAt {
		t.Errorf("created_at moved: %q → %q", before.CreatedAt, got.CreatedAt)
	}
	if got.UpdatedAt == before.CreatedAt && before.CreatedAt == before.UpdatedAt {
		t.Error("updated_at did not move — the edit claimed it never happened")
	}

	// The audit: exactly one new row, op update, the FIELD WORDS in the
	// summary (SEC-033: never the user's title).
	rows, r := tr.History(bug.ID, 0)
	if r != nil {
		t.Fatalf("history: %v", r)
	}
	var updates int
	for _, row := range rows {
		if row.Op == "update" {
			updates++
			if want := fmt.Sprintf("edited bug %d (title)", bug.ID); row.Summary != want {
				t.Errorf("update summary: %q want %q", row.Summary, want)
			}
			if strings.Contains(row.Summary, newTitle) {
				t.Errorf("SEC-033: the summary carried the title: %q", row.Summary)
			}
			if row.ActorKind != "user" || row.ActorID != "42" {
				t.Errorf("the edit audit did not name the person: %s/%s", row.ActorKind, row.ActorID)
			}
		}
	}
	if updates != 1 {
		t.Fatalf("update rows: got %d want 1 (total history %d)", updates, len(rows))
	}

	// The no-op: the same title again answers changed:false and earns
	// NO row — TM2's noise rule over the new op.
	before2, _, _ := tr.Show(bug.ID)
	same, changed, r := tr.Update(bug.ID, UpdateBug{Title: &newTitle}, personActor())
	if r != nil {
		t.Fatalf("no-op update: %v", r)
	}
	if changed {
		t.Error("an identical edit answered changed:true")
	}
	if same.UpdatedAt != before2.UpdatedAt {
		t.Error("the no-op edit moved updated_at — a write happened")
	}
	rows2, _ := tr.History(bug.ID, 0)
	if len(rows2) != len(rows) {
		t.Errorf("the no-op earned %d audit rows — an edit of nothing earns nothing", len(rows2)-len(rows))
	}

	// The refusals, each BY NAME, each without touching a row.
	tooLong := strings.Repeat("t", 201)
	tooBig := strings.Repeat("b", MaxBodyBytes+1)
	for i, c := range []struct {
		in   UpdateBug
		want string
	}{
		{UpdateBug{Title: ptr("")}, "title"},
		{UpdateBug{Title: &tooLong}, "200"},
		{UpdateBug{Body: &tooBig}, "16 KiB"},
		{UpdateBug{Body: ptr("bad\x00byte")}, "control"},
		{UpdateBug{Body: ptr("bad\x07bell")}, "control"},
		{UpdateBug{Title: &newTitle}, "does not exist"}, // id 9999 refused below
	} {
		id := bug.ID
		if i == 5 {
			id = 9999
		}
		_, _, r := tr.Update(id, c.in, personActor())
		if r == nil || !strings.Contains(r.Error(), c.want) {
			t.Errorf("refusal %d: got %v, want %q", i, r, c.want)
		}
	}
	// The legal control characters: newline and tab survive the gate.
	body := "line one\nline two\ttabbed"
	if _, _, r := tr.Update(bug.ID, UpdateBug{Body: &body}, personActor()); r != nil {
		t.Errorf("newline/tab body refused: %v", r)
	}
}

func ptr(s string) *string { return &s }

func deref(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

func TestDescribeNeverMergesTheTwoSets(t *testing.T) {
	// D2 as a property over EVERY combination: the board renders the
	// validation state and the priority state as TWO sentences, from
	// different words, never one badge. If a future edit ever renders a
	// merged "state", a fragment here stops being distinguishable.
	for _, v := range ValidationStates() {
		for _, p := range PriorityStates() {
			b := Bug{Validation: v, PriorityState: p, PriorityLabel: "P2"}
			d := Describe(b)
			if d.ValidationSentence == "" || d.PrioritySentence == "" {
				t.Fatalf("%s/%s: a sentence was blank — a set went unrendered", v, p)
			}
			if d.ValidationSentence == d.PrioritySentence {
				t.Fatalf("%s/%s: the two sentences merged into one: %q", v, p, d.ValidationSentence)
			}
			if !strings.Contains(d.ValidationSentence, string(v)) {
				t.Errorf("%s: validation sentence lost its own word: %q", v, d.ValidationSentence)
			}
			if !strings.Contains(d.PrioritySentence, string(p)) {
				t.Errorf("%s: priority sentence lost its own word: %q", p, d.PrioritySentence)
			}
			if strings.Contains(d.PrioritySentence, string(v)) && string(v) != string(p) {
				t.Errorf("%s/%s: the priority sentence rendered the VALIDATION word", v, p)
			}
		}
	}
	// The specific sentence the design refuses forever: unvalidated and
	// could-not-tell are different facts and must not share a rendering.
	a := Describe(Bug{Validation: ValidationUnvalidated, PriorityState: PriorityUnknown})
	b := Describe(Bug{Validation: ValidationCouldNotTell, PriorityState: PriorityUnknown})
	if a.ValidationSentence == b.ValidationSentence {
		t.Errorf("unvalidated and could-not-tell rendered as one sentence: %q", a.ValidationSentence)
	}
	// The fixed templates, word for word (the board's goldens read these).
	want := `validation: "unvalidated" — no attempt has run (D1)`
	if a.ValidationSentence != want {
		t.Errorf("unvalidated sentence:\n got %q\nwant %q", a.ValidationSentence, want)
	}
	if got, want := b.ValidationSentence, `validation: "could-not-tell" — an attempt ran and was inconclusive (D1)`; got != want {
		t.Errorf("could-not-tell sentence:\n got %q\nwant %q", got, want)
	}
}

func TestVocabularyEqualsTheClosedSets(t *testing.T) {
	tr, _ := openTriageStore(t, "/r")
	v := tr.Vocabulary()
	// Equal to the owners' answers — assembled, NEVER restated. If the
	// door ever keeps its own copy of the sets, this line reddens the day
	// the copy drifts (that is the whole point of this test).
	ownerV := make([]string, 0, 4)
	for _, st := range ValidationStates() {
		ownerV = append(ownerV, string(st))
	}
	if !reflect.DeepEqual(v.ValidationStates, ownerV) {
		t.Errorf("validation states drifted from the owner: %v", v.ValidationStates)
	}
	ownerP := make([]string, 0, 3)
	for _, st := range PriorityStates() {
		ownerP = append(ownerP, string(st))
	}
	if !reflect.DeepEqual(v.PriorityStates, ownerP) {
		t.Errorf("priority states drifted from the owner: %v", v.PriorityStates)
	}
	want := map[string][]string{}
	for from, legal := range AllowedTransitions() {
		word := make([]string, len(legal))
		for i, l := range legal {
			word[i] = string(l)
		}
		want[string(from)] = word
	}
	if !reflect.DeepEqual(v.LegalNext, want) {
		t.Errorf("legal transitions drifted from the owner:\n got %v\nwant %v", v.LegalNext, want)
	}
	// The wire names, since the UI reads these keys (and the UI must not
	// restate the sets either — the fixture and this test are the contract).
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"validationStates"`, `"priorityStates"`, `"legalNext"`} {
		if !strings.Contains(string(raw), key) {
			t.Errorf("vocabulary JSON lost %s: %s", key, raw)
		}
	}
}

func TestHistorySeparatesNoRowsFromNoBug(t *testing.T) {
	tr, _ := openTriageStore(t, "/r")
	if _, r := tr.History(9999, 0); r == nil || r.Error() != "triage: bug 9999 does not exist" {
		t.Fatalf("unknown id: got %v, want the named sentence", r)
	}
	bug, r := tr.Create("/r", NewBug{Title: "no audit yet", Body: "a plain report", FiledBy: "person"})
	if r != nil {
		t.Fatalf("create: %v", r)
	}
	rows, r := tr.History(bug.ID, 0)
	if r != nil {
		t.Fatalf("real bug, no rows: %v", r)
	}
	if len(rows) != 0 {
		t.Fatalf("a bug created without an actor earned %d audit rows", len(rows))
	}
	// Newest first once rows exist: file then update (ids order the tie
	// inside one clock tick — T1's ORDER BY).
	b2, r := tr.CreateAudited("/r", NewBug{Title: "with history", Body: "a report with a history", FiledBy: "person"}, personActor())
	if r != nil {
		t.Fatalf("seed: %v", r)
	}
	n := "renamed"
	if _, _, r := tr.Update(b2.ID, UpdateBug{Title: &n}, personActor()); r != nil {
		t.Fatalf("update: %v", r)
	}
	rows, r = tr.History(b2.ID, 0)
	if r != nil {
		t.Fatalf("history: %v", r)
	}
	if len(rows) != 2 || rows[0].Op != "update" || rows[1].Op != "file" {
		t.Fatalf("history order: %+v — want update then file, newest first", rows)
	}
}
