package triage

// S51 T1 (TM2/TM6) — the door's audit: exactly one row per write, the fixed
// template on the row, the reactor's own name on the moves nobody person
// made, and TM6's law at the door: the machine kinds touch `file` and
// nothing else. The store-level net (the transaction, the CHECK) is pinned
// in internal/store/bug_change_test.go; these prove the DOOR composes the
// right row and refuses the wrong actors in its own words.

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/store"
)

var auditCLI = Actor{Kind: "cli", ID: "aeon-cli"}

func auditRows(t *testing.T, s *store.Store, bugID int64) []store.BugChange {
	t.Helper()
	rows, err := s.BugChangeList(context.Background(), bugID, 50)
	if err != nil {
		t.Fatalf("list audit for bug %d: %v", bugID, err)
	}
	return rows
}

func TestEveryTriageWriteWritesExactlyOneAuditRow(t *testing.T) {
	tr, s := openTriageStore(t, "/repo/audit-one")
	// A HOSTILE title: the summary templates carry ids and enums only
	// (SEC-033) — nothing a filer typed may travel into the history row.
	b, ref := tr.CreateAudited("/repo/audit-one", NewBug{Title: `drop table bug_change; -- "filed bug 99999"`, Body: "b", FiledBy: "person"}, auditCLI)
	if ref != nil {
		t.Fatalf("create audited: %s", ref.Reason)
	}
	rows := auditRows(t, s, b.ID)
	if len(rows) != 1 {
		t.Fatalf("the create wrote %d rows, want exactly 1", len(rows))
	}
	row := rows[0]
	if row.Op != "file" || row.Summary != "filed bug "+itoa(row.ID) || row.ActorKind != "cli" || row.ActorID != "aeon-cli" {
		t.Fatalf("file row: %s %q by %s/%s", row.Op, row.Summary, row.ActorKind, row.ActorID)
	}

	if _, ref := tr.TransitionAudited(b.ID, ValidationReproduced, SystemReactor); ref != nil {
		t.Fatalf("reactor transition: %s", ref.Reason)
	}
	if _, ref := tr.SetPriorityAudited(b.ID, "/repo/audit-one", PriorityKnown, "P1", Actor{Kind: "user", ID: "42"}); ref != nil {
		t.Fatalf("audited priority: %s", ref.Reason)
	}
	dup, ref := tr.CreateAudited("/repo/audit-one", NewBug{Title: "twin", Body: "b", FiledBy: "person"}, auditCLI)
	if ref != nil {
		t.Fatalf("second create: %s", ref.Reason)
	}
	if _, ref := tr.ConfirmDuplicateAudited(dup.ID, "/repo/audit-one", b.ID, "cjlapao", Actor{Kind: "user", ID: "42"}); ref != nil {
		t.Fatalf("audited confirm-duplicate: %s", ref.Reason)
	}
	if _, ref := tr.RecordHandoffAudited(b.ID, "/repo/audit-one", 7, Actor{Kind: "cli", ID: "aeon-cli"}); ref != nil {
		t.Fatalf("audited handoff: %s", ref.Reason)
	}
	if ref := tr.AppendValidationAudit(b.ID, Actor{Kind: "user", ID: "42"}); ref != nil {
		t.Fatalf("validate dispatch audit: %s", ref.Reason)
	}

	// The file row above already pins the template byte for byte — the
	// hostile title is absent from it by construction.
	rows = auditRows(t, s, b.ID) // re-read after the five later acts
	if len(rows) != 5 {
		t.Fatalf("bug %d carries %d rows, want 5 (file, transition, priority, handoff, validate — the duplicate row belongs to the twin)", b.ID, len(rows))
	}
	want := map[string]string{
		"transition": "moved bug " + itoa(b.ID) + ` from "unvalidated" to "reproduced"`,
		"priority":   "set bug " + itoa(b.ID) + ` priority to "known"`,
		"handoff":    "handed bug " + itoa(b.ID) + " off to roadmap item 7",
		"validate":   "started a validation attempt on bug " + itoa(b.ID),
		"file":       "filed bug " + itoa(b.ID),
		"duplicate":  "", // the second bug's duplicate row rides the twin
	}
	seen := map[string]int{}
	for _, r := range rows {
		seen[r.Op]++
		if r.Op == "duplicate" {
			t.Fatalf("bug %d carries a duplicate row that belongs to bug %d's confirmation", b.ID, dup.ID)
		}
		if w := want[r.Op]; w != "" && r.Summary != w {
			t.Fatalf("op %s summary %q, want the template %q", r.Op, r.Summary, w)
		}
	}
	if seen["file"] != 1 || seen["transition"] != 1 || seen["priority"] != 1 || seen["handoff"] != 1 || seen["validate"] != 1 {
		t.Fatalf("op census %v — every audited op exactly once per act", seen)
	}
	// The reactor's move names the reactor; the rest name their people.
	for _, r := range rows {
		if r.Op == "transition" && (r.ActorKind != "system" || r.ActorID != "reactor") {
			t.Fatalf("the reactor's move carries %s/%s — TM2 says a person must see what moved on its own", r.ActorKind, r.ActorID)
		}
	}
	// The duplicate row is the twin's, with its template.
	dupRows := auditRows(t, s, dup.ID)
	if len(dupRows) != 2 || dupRows[0].Op != "duplicate" ||
		dupRows[0].Summary != "marked bug "+itoa(dup.ID)+" a duplicate of bug "+itoa(b.ID) {
		t.Fatalf("the twin's rows: %+v", dupRows)
	}
}

func TestPlainDoorMethodsEarnNoAuditRows(t *testing.T) {
	// The plain path stays for fixtures and store-level tests — and it
	// audits NOTHING: TM2's guarantee is that production travels the
	// audited twins, and a silent plain write proves the pair is real.
	tr, s := openTriageStore(t, "/repo/audit-plain")
	b, ref := tr.Create("/repo/audit-plain", NewBug{Title: "plain", Body: "b", FiledBy: "person"})
	if ref != nil {
		t.Fatalf("plain create: %s", ref.Reason)
	}
	if _, ref := tr.Transition(b.ID, ValidationReproduced); ref != nil {
		t.Fatalf("plain transition: %s", ref.Reason)
	}
	if _, ref := tr.SetPriority(b.ID, "/repo/audit-plain", PriorityKnown, "P2"); ref != nil {
		t.Fatalf("plain priority: %s", ref.Reason)
	}
	if rows := auditRows(t, s, b.ID); len(rows) != 0 {
		t.Fatalf("plain writes earned %d audit rows — the plain path must audit nothing", len(rows))
	}
}

func TestMachineKindsTouchFileAndNothingElse(t *testing.T) {
	// TM6 at the door: an AGENT may file (D7's self-filing) and may not
	// transition, prioritize, confirm, hand off, or dispatch. The refusal
	// is the door's own Refusal, naming the actor kind.
	tr, s := openTriageStore(t, "/repo/audit-machine")
	agent := Actor{Kind: "agent", ID: "golang-engineer"}
	b, ref := tr.CreateAudited("/repo/audit-machine", NewBug{Title: "self-filed", Body: "b", FiledBy: "golang-engineer"}, agent)
	if ref != nil {
		t.Fatalf("the agent's filing was refused: %s", ref.Reason)
	}
	rows := auditRows(t, s, b.ID)
	if len(rows) != 1 || rows[0].ActorKind != "agent" || rows[0].ActorID != "golang-engineer" {
		t.Fatalf("the agent's file row: %+v", rows)
	}

	cases := []struct {
		name string
		call func() *Refusal
	}{
		{"transition", func() *Refusal { _, r := tr.TransitionAudited(b.ID, ValidationReproduced, agent); return r }},
		{"priority", func() *Refusal {
			_, r := tr.SetPriorityAudited(b.ID, "/repo/audit-machine", PriorityKnown, "P1", agent)
			return r
		}},
		{"confirm-duplicate", func() *Refusal {
			other, _ := tr.CreateAudited("/repo/audit-machine", NewBug{Title: "other", Body: "b", FiledBy: "person"}, auditCLI)
			_, r := tr.ConfirmDuplicateAudited(other.ID, "/repo/audit-machine", b.ID, "golang-engineer", agent)
			return r
		}},
		{"handoff", func() *Refusal { _, r := tr.RecordHandoffAudited(b.ID, "/repo/audit-machine", 9, agent); return r }},
		{"validate", func() *Refusal { return tr.AppendValidationAudit(b.ID, agent) }},
	}
	for _, c := range cases {
		ref := c.call()
		if ref == nil {
			t.Fatalf("the agent's %s was accepted — TM6 says the machine files and does nothing else", c.name)
		}
		if !strings.Contains(ref.Reason, "agent") {
			t.Errorf("%s refusal %q does not name the actor kind", c.name, ref.Reason)
		}
	}
	// The agent's one row is still the only one the bug carries.
	if rows := auditRows(t, s, b.ID); len(rows) != 1 || rows[0].Op != "file" {
		t.Fatalf("refused machine writes left rows: %+v", rows)
	}
}

func TestConfirmDuplicateRefusesSystemKind(t *testing.T) {
	// D5's gate is a PERSON at a door: user and cli pass, and `system` —
	// lawful elsewhere on the audit plane — may not confirm a duplicate
	// even though the schema CHECK would allow the row.
	tr, s := openTriageStore(t, "/repo/audit-system")
	b, _ := tr.CreateAudited("/repo/audit-system", NewBug{Title: "a", Body: "b", FiledBy: "person"}, auditCLI)
	dup, _ := tr.CreateAudited("/repo/audit-system", NewBug{Title: "a twin", Body: "b", FiledBy: "person"}, auditCLI)
	if _, ref := tr.ConfirmDuplicateAudited(dup.ID, "/repo/audit-system", b.ID, "reactor", SystemReactor); ref == nil {
		t.Fatal("the reactor confirmed a duplicate — D5 says a person confirmed it")
	} else if !strings.Contains(ref.Reason, "system") {
		t.Fatalf("refusal %q does not name the kind", ref.Reason)
	}
	if _, ref := tr.ConfirmDuplicateAudited(dup.ID, "/repo/audit-system", b.ID, "cjlapao", Actor{Kind: "user", ID: "42"}); ref != nil {
		t.Fatalf("the person's confirmation was refused: %s", ref.Reason)
	}
	// The confirmedBy human gate stays beside the actor law (unchanged law).
	if _, ref := tr.ConfirmDuplicateAudited(b.ID, "/repo/audit-system", dup.ID, "", Actor{Kind: "user", ID: "42"}); ref == nil {
		t.Fatal("a confirmation with no confirmedBy name was accepted — the S29 gate moved")
	}
	if rows := auditRows(t, s, dup.ID); len(rows) != 2 { // file + duplicate
		t.Fatalf("the twin carries %d rows, want 2", len(rows))
	}
}

func TestGateFilerCarriesSystemGates(t *testing.T) {
	// The S57 filer's audit identity, the twin of the filed_by literal the
	// migration exempts — so gate filings stay re-point-free forever.
	tr, s := openTriageStore(t, "/repo/audit-gates")
	b, ref := tr.CreateAudited("/repo/audit-gates", NewBug{Title: "gate filing", Body: "b", FiledBy: "system:gates"}, SystemGates)
	if ref != nil {
		t.Fatalf("the gate filing was refused: %s", ref.Reason)
	}
	rows := auditRows(t, s, b.ID)
	if len(rows) != 1 || rows[0].ActorKind != "system" || rows[0].ActorID != "gates" {
		t.Fatalf("the gate row: %+v", rows)
	}
}

func TestAuditRefusalOfABuglessIdIsSpoken(t *testing.T) {
	// There is no FK to bug (history outlives the row), so an audit on an
	// absent bug lands anyway — the door's honest answer is the row, not a
	// second invention; but the validate dispatch on an absent row still
	// speaks its row and the SHOW answers nothing. The pin here: a refused
	// transition on an absent bug audits nothing.
	tr, s := openTriageStore(t, "/repo/audit-absent")
	if _, ref := tr.TransitionAudited(9999, ValidationReproduced, SystemReactor); ref == nil {
		t.Fatal("a transition on an absent bug was accepted")
	}
	if rows := auditRows(t, s, 9999); len(rows) != 0 {
		t.Fatalf("a refused transition audited %d rows", len(rows))
	}
}

// itoa renders an id for the fixed templates.
func itoa(n int64) string { return strconv.FormatInt(n, 10) }
