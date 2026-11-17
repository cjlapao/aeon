package policy_test

// The authored record (S11 loop T9): the kind, the handover, and the door it
// shares. The guards that make the other kinds safe make this one safe too —
// TestPolicyEventInsertHasExactlyOneCaller counts recordEvent's callers and
// now names RecordAuthored among them; these tests drive the handover itself.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/policy"
)

// The one authored rule these tests record. The id shape mirrors what
// `aeon policy seed-corpus` mints (corpus.<agent>.<hash>) — a legal
// identifier, which is what the seam demands.
const (
	authoredSubject  = "agent:git-loop"
	authoredPolicyID = "corpus.git-loop.3f2a9c0d1e4b"
)

func TestRecordAuthoredWritesTheAuditRow(t *testing.T) {
	// The happy path, field by field: the kind is "authored", the subject
	// travels whole to the seam (recordEvent splits it), the policy_id names
	// the rule, the detail is the FIXED constant (no Spec bytes, no caller
	// text — legal constraint 1 holds for this kind exactly as for the others),
	// and the instant is the caller's (D5).
	repo := &fakeRepo{}
	s := policy.NewStore(repo)
	if rej := s.RecordAuthored(context.Background(), authoredSubject, authoredPolicyID, eventT1); rej != nil {
		t.Fatalf("the honest authored record was refused: %s", rej.Reason)
	}
	if repo.eventInsertCalls != 1 {
		t.Fatalf("the seam saw %d inserts, want 1", repo.eventInsertCalls)
	}
	got := repo.recordedEvents[0]
	if got.kind != "authored" {
		t.Errorf("the outcome is %q, want %q — the row must say an authoring, never a refusal", got.kind, "authored")
	}
	if got.subject != authoredSubject || got.policyID != authoredPolicyID || !got.at.Equal(eventT1) {
		t.Errorf("the row is (%q, %q, %s), want (%q, %q, %s)",
			got.subject, got.policyID, got.at, authoredSubject, authoredPolicyID, eventT1)
	}
	if got.detail == "" {
		t.Fatal("the authored row has no detail")
	}
	for _, want := range []string{"authored:", "PL15"} {
		if !strings.Contains(got.detail, want) {
			t.Errorf("the fixed detail omits %q: %q", want, got.detail)
		}
	}
	// The fixed-constant claim, made literal: two records for two different
	// rules carry BYTE-IDENTICAL details — the identity lives in the columns.
	other := "corpus.git-loop.aabbccddee11"
	if rej := s.RecordAuthored(context.Background(), authoredSubject, other, eventT2); rej != nil {
		t.Fatalf("the second authored record was refused: %s", rej.Reason)
	}
	if repo.recordedEvents[1].detail != got.detail {
		t.Errorf("the detail is not one fixed constant:\n first: %q\nsecond: %q", got.detail, repo.recordedEvents[1].detail)
	}
	if repo.recordedEvents[1].policyID != other {
		t.Errorf("the second row names policy %q, want %q", repo.recordedEvents[1].policyID, other)
	}
}

func TestRecordAuthoredRefusesShapelessRecordsNamingTheField(t *testing.T) {
	// Each refusal names what it refused (DB19), and NOTHING reaches the seam.
	cases := []struct {
		name     string
		subject  string
		policyID string
		wantErr  string
	}{
		{name: "an authored record of no rule", subject: authoredSubject, policyID: "", wantErr: "names no rule"},
		{name: "a subject with no kind", subject: "git-loop", policyID: authoredPolicyID, wantErr: `has no ":"`},
		{name: "a subject kind the audit does not speak", subject: "loop-type:review", policyID: authoredPolicyID, wantErr: "only subject kinds"},
		{name: "a hostile subject id", subject: "agent:bad id", policyID: authoredPolicyID, wantErr: "carries"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repo := &fakeRepo{}
			s := policy.NewStore(repo)
			rej := s.RecordAuthored(context.Background(), c.subject, c.policyID, eventT1)
			if rej == nil || !strings.Contains(rej.Reason, c.wantErr) {
				t.Fatalf("refusal %v did not name %q", rej, c.wantErr)
			}
			if len(repo.recordedEvents) != 0 {
				t.Errorf("a refused record still reached the seam")
			}
		})
	}
	// The zero instant is its own refusal (D5 — the clock is a parameter).
	repo := &fakeRepo{}
	s := policy.NewStore(repo)
	rej := s.RecordAuthored(context.Background(), authoredSubject, authoredPolicyID, time.Time{})
	if rej == nil || !strings.Contains(rej.Reason, "no instant") {
		t.Fatalf("the zero instant escaped as something else: %v", rej)
	}
	if len(repo.recordedEvents) != 0 {
		t.Errorf("a record refused for its instant still reached the seam")
	}
}

func TestRecordAuthoredStoreFailureArrivesAsARefusal(t *testing.T) {
	// The single-return-shape discipline extends to the new handover: the
	// machine breaking says the record was NOT made; nothing half-writes.
	repo := &fakeRepo{eventErr: errors.New("disk on fire")}
	s := policy.NewStore(repo)
	rej := s.RecordAuthored(context.Background(), authoredSubject, authoredPolicyID, eventT1)
	if rej == nil {
		t.Fatal("the store failure was swallowed")
	}
	for _, want := range []string{"was NOT recorded", "disk on fire"} {
		if !strings.Contains(rej.Reason, want) {
			t.Errorf("the fold lost %q: %s", want, rej.Reason)
		}
	}
}

func TestAuthoredIsInTheKindVocabulary(t *testing.T) {
	// Read side: the row an authored record writes survives decodeEvent (the
	// vocabulary extension is one clause, and ReportEvents is where a missing
	// clause would show up — refusing the whole report over its own kind).
	repo := &fakeRepo{}
	s := policy.NewStore(repo)
	if rej := s.RecordAuthored(context.Background(), authoredSubject, authoredPolicyID, eventT1); rej != nil {
		t.Fatal(rej.Reason)
	}
	report, rows, rej := s.ReportEvents(context.Background(), authoredSubject, 10)
	if rej != nil {
		t.Fatalf("ReportEvents refused the row its own boundary wrote: %s", rej.Reason)
	}
	if report.Depth != 1 || len(rows) != 1 || rows[0].Outcome != "authored" {
		t.Fatalf("the listing is depth %d rows %+v, want one authored row", report.Depth, rows)
	}
	if rows[0].PolicyID != authoredPolicyID || rows[0].SubjectKind != "agent" || rows[0].SubjectID != "git-loop" {
		t.Errorf("the columns drifted: %+v", rows[0])
	}
	// And an outcome OUTSIDE the closed set still refuses the report by name —
	// widening to three kinds did not open the door to a fourth.
	fake := &fakeRepo{}
	fake.events = append(fake.events, fakeEvent{
		at: eventT1,
		row: policy.EventRow{ID: 1, SubjectKind: "agent", SubjectID: "git-loop",
			Outcome: "deleted", Detail: "an outcome nobody records", At: eventT1.UTC().Format(fakeEventAtLayout)},
	})
	if _, _, rej := policy.NewStore(fake).ReportEvents(context.Background(), "", 10); rej == nil ||
		!strings.Contains(rej.Reason, "not a kind") {
		t.Errorf("an outcome outside the closed set was not refused by name: %v", rej)
	}
}

func TestAuthoredRowsCoexistWithRefusalsInOneListing(t *testing.T) {
	// The audit's point: one table, three kinds, each legible by the outcome
	// column alone — the same requirement TestRecordDisagreementWritesTheTwinRow
	// pins for the refusal pair, extended over the authoring.
	repo := &fakeRepo{}
	s := policy.NewStore(repo)
	v := denyVerdict(t, s, "no-bash", "boss", "bash")
	mustRecordRefusal(t, s, "agent:boss", v, eventT1)
	if rej := s.RecordAuthored(context.Background(), "agent:boss", "corpus.boss.aabbccddee11", eventT2); rej != nil {
		t.Fatal(rej.Reason)
	}
	_, rows, rej := s.ReportEvents(context.Background(), "agent:boss", 10)
	if rej != nil {
		t.Fatal(rej.Reason)
	}
	kinds := map[string]bool{}
	for _, r := range rows {
		kinds[r.Outcome] = true
	}
	if !kinds["refusal"] || !kinds["authored"] {
		t.Errorf("the reported kinds are %v, want refusal AND authored beside each other", kinds)
	}
}
