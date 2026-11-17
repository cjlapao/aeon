package policy_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/policy"
)

// T8 — retention: the 90-day age cap, its trim off the write path, and the
// depth/cap/oldest report that makes the cap VISIBLE (D15/PL11). The events
// half of the fake repo lives in record_test.go; the Verdict.Sentence() the
// rows carry is rendered by verdict.go alone (T4) — this file prints it, it
// never re-formats it.

// ── the cap-boundary tests ───────────────────────────────────────────────────

func TestEventsPastTheCapAreRemoved(t *testing.T) {
	// The boundary call, one second over the edge: a row at
	// now − cap − 1s is past the cap and the trim takes it — and takes ONLY
	// it. The instant the row is removed is decided by the SUPPLIED clock
	// minus the named const (D5: no wall reads); the count the trim reports
	// is the count that left, so "how much did you delete" is an answer,
	// never a guess.
	repo := &fakeRepo{}
	s := policy.NewStore(repo)
	v := denyVerdict(t, s, "no-bash", "boss", "bash")

	mustRecordRefusal(t, s, "agent:boss", v, eventT0.Add(-policy.PolicyEventAgeCap-time.Second)) // past
	mustRecordRefusal(t, s, "agent:boss", v, eventT0.Add(-policy.PolicyEventAgeCap+time.Second)) // within
	mustRecordRefusal(t, s, "agent:boss", v, eventT0.Add(-time.Hour))                            // recent

	n, err := s.TrimEvents(context.Background(), eventT0)
	if err != nil {
		t.Fatalf("TrimEvents: %v", err)
	}
	if n != 1 {
		t.Fatalf("the trim removed %d rows, want exactly 1 (only now-cap-1s is past the cap)", n)
	}
	report, rows := mustReport(t, s, "agent:boss", 10)
	if report.Depth != 2 || len(rows) != 2 {
		t.Fatalf("depth %d / %d rows after the trim, want 2/2: %+v", report.Depth, len(rows), rows)
	}
	for _, r := range rows {
		if at, err := time.Parse(time.RFC3339Nano, r.At); err == nil && !at.After(eventT0.Add(-policy.PolicyEventAgeCap)) {
			t.Errorf("row id %d at %s survived a trim that should have taken it", r.ID, r.At)
		}
	}
}

func TestEventsWithinTheCapSurviveATrim(t *testing.T) {
	// The other side of the edge: now − cap + 1s survives, and so does a row
	// at EXACTLY now − cap — the trim's operator is `<`, and the boundary of
	// a retention rule is where audits are won and lost. Nothing that the
	// report promised to keep may vanish: the trim removes zero rows and
	// reports zero.
	repo := &fakeRepo{}
	s := policy.NewStore(repo)
	v := denyVerdict(t, s, "no-bash", "boss", "bash")

	mustRecordRefusal(t, s, "agent:boss", v, eventT0.Add(-policy.PolicyEventAgeCap))             // at the edge — survives (<)
	mustRecordRefusal(t, s, "agent:boss", v, eventT0.Add(-policy.PolicyEventAgeCap+time.Second)) // inside — survives

	n, err := s.TrimEvents(context.Background(), eventT0)
	if err != nil {
		t.Fatalf("TrimEvents: %v", err)
	}
	if n != 0 {
		t.Fatalf("the trim removed %d rows; within-the-cap rows (and a row AT the cutoff) must survive", n)
	}
	report, rows := mustReport(t, s, "agent:boss", 10)
	if report.Depth != 2 || len(rows) != 2 {
		t.Fatalf("depth %d / %d rows, want 2/2: %+v", report.Depth, len(rows), rows)
	}
}

func TestPolicyEventAgeCapIsNinetyDaysAndTheCutoffIsMeasuredFromTheSuppliedClock(t *testing.T) {
	// Q2, pinned as arithmetic: the cap is the NAMED const 90 * 24h — the
	// number the notes carry, on every report, in every trim. And the cutoff
	// the seam receives is exactly the caller's instant minus that const: no
	// wall clock anywhere (D5), no drift between what TrimEvents deletes and
	// what EventReport.Cap promises.
	if policy.PolicyEventAgeCap != 90*24*time.Hour {
		t.Fatalf("the age cap is %v; the answered number is 90 days", policy.PolicyEventAgeCap)
	}
	repo := &fakeRepo{}
	s := policy.NewStore(repo)
	if _, err := s.TrimEvents(context.Background(), eventT0); err != nil {
		t.Fatalf("TrimEvents: %v", err)
	}
	if !repo.lastTrim.Equal(eventT0.Add(-policy.PolicyEventAgeCap)) {
		t.Errorf("the seam received cutoff %v, want %v", repo.lastTrim, eventT0.Add(-policy.PolicyEventAgeCap))
	}
	if _, err := s.TrimEvents(context.Background(), time.Time{}); err == nil ||
		!strings.Contains(err.Error(), "no instant") {
		t.Errorf("a clockless TrimEvents did not name the missing clock (D5): %v", err)
	}
	if repo.trimCalls != 1 {
		t.Errorf("the clockless call reached the seam (%d calls); it must be refused above it", repo.trimCalls)
	}
}

// ── visibility: the depth, the cap and the oldest instant, together ──────────

func TestDepthCapAndOldestAreReportable(t *testing.T) {
	// D15: "nothing older than this exists" as a STATEMENT. Three refusals
	// go in at three instants; the report answers all three questions in one
	// call — how deep, how long the cap (the const itself, so the printed line
	// can never disagree with what the trim enforces), and what the oldest
	// retained row is. The rows come newest-first with a total order, and the
	// detail on the newest row is the verdict's own sentence — printed, never
	// re-formatted.
	//
	// F-13 changed WHICH rows the depth counts: it is the LISTING's depth, so
	// asking about agent:boss answers three (its own rows) and the loop row is
	// somebody else's answer. The unfiltered call still prices the whole log —
	// asserted below in the same test, because "the D15 number is unchanged
	// when no filter was asked for" is half of this task's claim.
	repo := &fakeRepo{}
	s := policy.NewStore(repo)
	v := denyVerdict(t, s, "no-bash", "boss", "bash")

	mustRecordRefusal(t, s, "agent:boss", v, eventT3) // oldest
	mustRecordRefusal(t, s, "agent:boss", v, eventT2)
	mustRecordRefusal(t, s, "agent:boss", v, eventT1) // newest
	// Another subject, same instant band — a hand-built loop verdict (the
	// engine only renders loop subjects when the REQUEST names a loop; its row
	// belongs to the unfiltered answer, not to boss's).
	loopV := policy.Verdict{Subject: "loop:l", Type: policy.ToolAccess}
	mustRecordRefusal(t, s, "loop:l", loopV, eventT2)

	report, rows := mustReport(t, s, "agent:boss", 10)
	if report.Depth != 3 {
		t.Errorf("depth %d, want 3 — the depth is the listing's (F-13): boss was refused three times, and "+
			"loop:l's row is not in this answer", report.Depth)
	}
	if report.Cap != policy.PolicyEventAgeCap {
		t.Errorf("the report carries cap %v, want the named const %v — the CLI line and the trim must agree",
			report.Cap, policy.PolicyEventAgeCap)
	}
	if !report.Oldest.Equal(eventT3) {
		t.Errorf("oldest %v, want %v — the statement \"nothing older than this exists\" answers with this",
			report.Oldest, eventT3)
	}
	if len(rows) != 3 {
		t.Fatalf("subject listing returned %d rows, want 3: %+v", len(rows), rows)
	}

	// The unfiltered half: subject "" still means the whole log — the D15
	// number, the one "twenty retries a round" is argued from, unchanged by
	// F-13 (four rows here: boss's three plus the loop's).
	all, allRows := mustReport(t, s, "", 10)
	if all.Depth != 4 || len(allRows) != 4 {
		t.Errorf("the unfiltered report answered depth %d / %d rows, want 4/4 — the whole-log reading of "+
			"D15 must survive the listing's depth", all.Depth, len(allRows))
	}
	if rows[0].Detail != v.Sentence() || rows[0].SubjectKind != "agent" || rows[0].SubjectID != "boss" {
		t.Errorf("the newest row is not the refusal as recorded: %+v", rows[0])
	}
	for i := 1; i < len(rows); i++ {
		if rows[i-1].At < rows[i].At {
			t.Fatalf("rows are not newest-first: %s before %s", rows[i-1].At, rows[i].At)
		}
	}

	// An empty table has a real answer (0, cap, zero) — distinguishable from
	// failure, which arrives as a refusal, never a silent zero (P31).
	empty := policy.NewStore(&fakeRepo{})
	er, erows := mustReport(t, empty, "", 10)
	if er.Depth != 0 || !er.Oldest.IsZero() || len(erows) != 0 || er.Cap != policy.PolicyEventAgeCap {
		t.Errorf("the empty report lies: %+v / %d rows", er, len(erows))
	}
}

func TestReportEventsDepthIsTheListingsDepth(t *testing.T) {
	// F-13 at the boundary: ONE answer, two facts. The rows above the footer
	// and the depth/oldest beside it come from the same subject — so a listing
	// can never print a footer about a table the caller did not ask about (the
	// QA suite's QAPL-24 and the slice DEMO's F-14 are the same sentence: a
	// `--subject` matching nothing printed `depth 1` about somebody else).
	// bob is refused three times, ann once, at instants that differ, so a
	// leaked depth or a leaked oldest is visible as a wrong number rather than
	// a plausible one.
	repo := &fakeRepo{}
	s := policy.NewStore(repo)
	// One real verdict per subject — RecordRefusal binds the subject to the one
	// the verdict renders about, so bob's rows need bob's verdict.
	bobV := denyVerdict(t, s, "no-bash-bob", "bob", "bash")
	annV := denyVerdict(t, s, "no-bash-ann", "ann", "bash")

	mustRecordRefusal(t, s, "agent:bob", bobV, eventT1)
	mustRecordRefusal(t, s, "agent:bob", bobV, eventT2)
	mustRecordRefusal(t, s, "agent:bob", bobV, eventT3) // bob's oldest
	mustRecordRefusal(t, s, "agent:ann", annV, eventT2)

	report, rows := mustReport(t, s, "agent:bob", 20)
	if report.Depth != 3 || len(rows) != 3 {
		t.Fatalf("bob's report: depth %d / %d rows, want 3/3 — the footer is the listing's (F-13)",
			report.Depth, len(rows))
	}
	if !report.Oldest.Equal(eventT3) {
		t.Errorf("bob's oldest %v, want %v — ann's rows are not in this answer", report.Oldest, eventT3)
	}
	for _, r := range rows {
		if r.SubjectKind != "agent" || r.SubjectID != "bob" {
			t.Errorf("a row from another subject rode bob's listing: %+v", r)
		}
	}
	if repo.lastStatsSubject != "agent:bob" {
		t.Errorf("the depth read was asked for %q while the listing asked for %q — the seam did not carry "+
			"the filter (F-13)", repo.lastStatsSubject, "agent:bob")
	}
	if repo.statsCalls != 1 || repo.queryCalls != 1 {
		t.Errorf("the report took %d depth reads and %d listing reads for one call; want one of each, "+
			"both filtered", repo.statsCalls, repo.queryCalls)
	}

	// ann's answer is ann's: one row, her own instant.
	annReport, annRows := mustReport(t, s, "agent:ann", 20)
	if annReport.Depth != 1 || len(annRows) != 1 || !annReport.Oldest.Equal(eventT2) {
		t.Errorf("ann's report: depth %d / %d rows / oldest %v, want 1/1/%v",
			annReport.Depth, len(annRows), annReport.Oldest, eventT2)
	}

	// The honest empty: a subject nobody refused is zero rows, depth 0, no
	// oldest — and no error. This is the state that used to print bob's
	// numbers, and it is the reason the CLI's `depth 0` honesty line can now
	// fire on a filtered listing at all (F-14).
	empty, emptyRows := mustReport(t, s, "agent:nobody", 20)
	if empty.Depth != 0 || !empty.Oldest.IsZero() || len(emptyRows) != 0 {
		t.Errorf("a never-refused subject answered depth %d / oldest %v / %d rows; want 0/zero/0",
			empty.Depth, empty.Oldest, len(emptyRows))
	}
	if empty.Cap != policy.PolicyEventAgeCap {
		t.Errorf("even the empty answer carries the cap, got %v", empty.Cap)
	}

	// And the unfiltered call still describes the whole log: four rows here.
	all, allRows := mustReport(t, s, "", 20)
	if all.Depth != 4 || len(allRows) != 4 || !all.Oldest.Equal(eventT3) {
		t.Errorf("the unfiltered report: depth %d / %d rows / oldest %v, want 4/4/%v",
			all.Depth, len(allRows), all.Oldest, eventT3)
	}
	if repo.lastStatsSubject != "" {
		t.Errorf("the unfiltered report asked the seam for %q; want \"\" (the whole log)", repo.lastStatsSubject)
	}
}

func TestTrimmingIsNotInvokedOnInsert(t *testing.T) {
	// The seed's rule with its spy: a refusal must not pay for housekeeping.
	// Fifty RecordRefusal calls — the twenty-retries-a-round loop, two and a
	// half times over — and the trim seam is touched EXACTLY ZERO times.
	// Trimming runs on daemon start and on demand; nothing else may reach it
	// through the boundary (PL11).
	repo := &fakeRepo{}
	s := policy.NewStore(repo)
	v := denyVerdict(t, s, "no-bash", "boss", "bash")

	for i := 0; i < 50; i++ {
		mustRecordRefusal(t, s, "agent:boss", v, eventT0.Add(time.Duration(i)*time.Second))
	}
	if repo.trimCalls != 0 {
		t.Errorf("the insert path invoked the trim %d times; housekeeping belongs to daemon start and "+
			"demand, never to the refusal path (PL11)", repo.trimCalls)
	}
	if repo.eventInsertCalls != 50 {
		t.Errorf("%d inserts for 50 refusals — the refusals must still all land", repo.eventInsertCalls)
	}
	report, _ := mustReport(t, s, "agent:boss", 100)
	if report.Depth != 50 {
		t.Errorf("depth %d, want 50", report.Depth)
	}
}

// ── the read side: validation, tamper refusal, store failure ─────────────────

func TestReportEventsValidatesItsInputsBeforeTheSeam(t *testing.T) {
	// A zero-limit listing is the audit answer that lies ("no refusals
	// happened"), and a shapeless subject audits nobody: both refused by the
	// boundary BEFORE either repo read is issued.
	repo := &fakeRepo{}
	s := policy.NewStore(repo)
	for _, c := range []struct {
		name    string
		subject string
		limit   int
		must    string
	}{
		{"limit zero", "", 0, "positive depth"},
		{"limit negative", "agent:boss", -3, "positive depth"},
		{"subject with no kind", "boss", 10, `no ":"`},
		{"subject kind nobody renders", "tool:bash", 10, "agent"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, _, rej := s.ReportEvents(context.Background(), c.subject, c.limit)
			if rej == nil || !strings.Contains(rej.Reason, c.must) {
				t.Errorf("no refusal naming %q: %+v", c.must, rej)
			}
		})
	}
	if repo.queryCalls != 0 || repo.statsCalls != 0 {
		t.Errorf("invalid inputs reached the seam (%d query, %d stats calls)", repo.queryCalls, repo.statsCalls)
	}
}

func TestReportEventsRefusesTamperedRowsNamingTheRow(t *testing.T) {
	// The SEC-015 read discipline of decodeAcceptance, applied to the audit:
	// a row that escaped the record boundary — hand-edited table, row from
	// before it — REFUSES the whole answer with its id named. Silently
	// dropping it would make the report's depth and its rows describe two
	// different tables, and an audit with a hole shaped like plausible
	// innocence is the failure this slice exists to make impossible.
	cases := []struct {
		name    string
		row     policy.EventRow
		mustHas []string
	}{
		{"an outcome nobody records",
			policy.EventRow{ID: 42, SubjectKind: "agent", SubjectID: "boss", Outcome: "approved",
				Detail: "refused: agent:boss", At: eventT1.UTC().Format(fakeEventAtLayout)},
			[]string{"42", "approved"}},
		{"a subject kind nobody renders",
			policy.EventRow{ID: 42, SubjectKind: "tool", SubjectID: "bash", Outcome: "refusal",
				Detail: "refused: agent:boss", At: eventT1.UTC().Format(fakeEventAtLayout)},
			[]string{"42", "tool"}},
		{"a subject id with a newline",
			policy.EventRow{ID: 42, SubjectKind: "agent", SubjectID: "bo\nss", Outcome: "refusal",
				Detail: "refused: agent:boss", At: eventT1.UTC().Format(fakeEventAtLayout)},
			[]string{"42", "subject id", "control character U+000A"}},
		{"a policy id with a newline",
			policy.EventRow{ID: 42, PolicyID: "rule\nbreak", SubjectKind: "agent", SubjectID: "boss",
				Outcome: "refusal", Detail: "refused: agent:boss", At: eventT1.UTC().Format(fakeEventAtLayout)},
			[]string{"42", "policy id", "control character U+000A"}},
		{"a policy id of pure zero-widths",
			policy.EventRow{ID: 42, PolicyID: "\u200b\u200b", SubjectKind: "agent", SubjectID: "boss",
				Outcome: "refusal", Detail: "refused: agent:boss", At: eventT1.UTC().Format(fakeEventAtLayout)},
			[]string{"42", "policy id", "no visible character"}},
		{"an empty detail",
			policy.EventRow{ID: 42, SubjectKind: "agent", SubjectID: "boss", Outcome: "refusal",
				Detail: "", At: eventT1.UTC().Format(fakeEventAtLayout)},
			[]string{"42", "detail"}},
		{"a detail over the cap",
			policy.EventRow{ID: 42, SubjectKind: "agent", SubjectID: "boss", Outcome: "refusal",
				Detail: strings.Repeat("x", policy.MaxEventDetailRunes+1), At: eventT1.UTC().Format(fakeEventAtLayout)},
			[]string{"42", "257 runes", "256"}},
		{"an `at` that is not an instant",
			policy.EventRow{ID: 42, SubjectKind: "agent", SubjectID: "boss", Outcome: "refusal",
				Detail: "refused: agent:boss", At: "yesterday-ish"},
			[]string{"42", "yesterday-ish", "RFC3339Nano"}},
		{"a row with no identity",
			policy.EventRow{ID: 0, SubjectKind: "agent", SubjectID: "boss", Outcome: "refusal",
				Detail: "refused: agent:boss", At: eventT1.UTC().Format(fakeEventAtLayout)},
			[]string{"id 0"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repo := &fakeRepo{}
			// A healthy row first: the tampered neighbour must refuse the
			// WHOLE answer, healthy rows included.
			repo.events = []fakeEvent{
				{at: eventT2, row: policy.EventRow{ID: 1, SubjectKind: "agent", SubjectID: "boss",
					Outcome: "refusal", Detail: "refused: agent:boss · no allow matched", At: eventT2.UTC().Format(fakeEventAtLayout)}},
				{at: eventT1, row: c.row},
			}
			// The ALL-SUBJECTS listing is the only path that can see a
			// tampered-subject row at all — a WHERE on the honest columns
			// cannot match a row whose kind is "tool" or whose id carries a
			// newline — and it is the path an auditor browses ("who was
			// refused, anyone"). Filtering it away would be the hole.
			_, _, rej := policy.NewStore(repo).ReportEvents(context.Background(), "", 10)
			if rej == nil {
				t.Fatalf("ReportEvents ACCEPTED the tampered row and its neighbours")
			}
			for _, want := range c.mustHas {
				if !strings.Contains(rej.Reason, want) {
					t.Errorf("the refusal does not name %q: %s", want, rej.Reason)
				}
			}
		})
	}
}

func TestReportEventsFoldsStoreFailuresIntoRefusals(t *testing.T) {
	// Both reads fold to a *Reject that says the report was NOT read and
	// which read failed — depth/oldest or the listing. "Empty table" and
	// "the disk broke" must never share an answer.
	repo := &fakeRepo{statsErr: errors.New("reader pool gone")}
	_, _, rej := policy.NewStore(repo).ReportEvents(context.Background(), "agent:boss", 10)
	if rej == nil || !strings.Contains(rej.Reason, "NOT read") || !strings.Contains(rej.Reason, "depth/oldest") ||
		!strings.Contains(rej.Reason, "reader pool gone") {
		t.Errorf("the stats failure did not fold honestly: %+v", rej)
	}

	repo = &fakeRepo{queryErr: errors.New("scan broke")}
	_, _, rej = policy.NewStore(repo).ReportEvents(context.Background(), "agent:boss", 10)
	if rej == nil || !strings.Contains(rej.Reason, "NOT read") || !strings.Contains(rej.Reason, "row read") ||
		!strings.Contains(rej.Reason, "scan broke") {
		t.Errorf("the listing failure did not fold honestly: %+v", rej)
	}
}

func TestTrimEventsFoldsAStoreFailureIntoAnError(t *testing.T) {
	// The trim reports plain infrastructure errors (it is not a domain
	// refusal): the boot that could not trim says so and loses nothing — the
	// rows are still there, and the next boot tries again.
	repo := &fakeRepo{trimErr: errors.New("disk on fire")}
	n, err := policy.NewStore(repo).TrimEvents(context.Background(), eventT0)
	if err == nil || !strings.Contains(err.Error(), "disk on fire") || n != 0 {
		t.Errorf("the trim failure did not surface honestly: n=%d err=%v", n, err)
	}
}
