package policy_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/cjlapao/aeon/internal/policy"
)

// T8 — the audit trail: every refusal is recorded with its subject.
//
// The record is the slice's whole product ("an operator must be able to ask
// who was refused for what, and when" — ARCHITECTURE §9), so THE tests here
// are the two named by the criteria: every verdict kind that is a refusal
// leaves exactly one row carrying the subject, and the detail column carries
// only what policy itself rendered — never content, never a secret, never a
// caller's free text (SEC-001/002/020).
//
// The events half of fakeRepo lives here (the acceptance half stays in
// record_test.go): the fake mirrors the table — AUTOINCREMENT ids, the
// subject split at the FIRST colon exactly as the store splits it, the
// fixed-width at rendering, strict `<` at trim — and the real SQL is tested
// against SQLite in internal/store/policy_test.go.

// ── the events half of the fake (T8) ─────────────────────────────────────────

// fakeEventAtLayout is the twin of the store's unexported eventAtLayout:
// RFC3339Nano with the 9-digit fraction FORCED, so lexicographic order IS time
// order (the store test pins the real bytes; this fake renders the same shape
// so a boundary decode that could not read it fails here too).
const fakeEventAtLayout = "2006-01-02T15:04:05.000000000Z07:00"

// fakeEvent is one stored policy_event row plus the instant it went in at —
// the time is kept beside the rendered string so the fake's trim compares
// REAL instants, never string luck (the SQL-side comparison of the fixed-
// width rendering is pinned against SQLite in internal/store).
type fakeEvent struct {
	row policy.EventRow
	at  time.Time
}

// recordedEvent is one RecordRefusal call as the boundary handed it down — the
// forwarding claim ("the seam gets exactly the validated values") is asserted
// against this, not against the stored rendering.
type recordedEvent struct {
	kind     string
	subject  string
	policyID string
	detail   string
	at       time.Time
}

// PolicyEventInsert satisfies the grown Repo. The fake mirrors the table:
// AUTOINCREMENT ids from 1, the subject split at the FIRST colon exactly as
// the store splits it, and the store's own fail-fasts (blank kind/subject/
// detail, unsplit subject, zero instant) answered with an error — a boundary
// test against a double that swallows the shapeless proves the double.
func (f *fakeRepo) PolicyEventInsert(_ context.Context, kind, subject, policyID, detail string, at time.Time) error {
	f.eventInsertCalls++
	f.recordedEvents = append(f.recordedEvents, recordedEvent{kind: kind, subject: subject, policyID: policyID, detail: detail, at: at})
	if f.eventErr != nil {
		return f.eventErr
	}
	if kind == "" || subject == "" || detail == "" {
		return fmt.Errorf("fake: a %q event needs subject and detail (the real store fail-fasts too)", kind)
	}
	if at.IsZero() {
		return fmt.Errorf("fake: an event with no instant (D5) is refused")
	}
	k, id, ok := strings.Cut(subject, ":")
	if !ok || k == "" || id == "" {
		return fmt.Errorf("fake: subject %q does not split into kind and id", subject)
	}
	f.events = append(f.events, fakeEvent{
		at: at,
		row: policy.EventRow{
			ID: int64(len(f.events) + 1), PolicyID: policyID,
			SubjectKind: k, SubjectID: id, Outcome: kind, Detail: detail,
			At: at.UTC().Format(fakeEventAtLayout),
		},
	})
	return nil
}

// PolicyEventQuery satisfies the grown Repo: the newest-first listing with the
// same order (at DESC, id DESC) and the same limit rule the SQL declares.
func (f *fakeRepo) PolicyEventQuery(_ context.Context, subject string, limit int) ([]policy.EventRow, error) {
	f.queryCalls++
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	if limit <= 0 {
		return nil, fmt.Errorf("fake: limit %d is not a listing", limit)
	}
	var wantKind, wantID string
	if subject != "" {
		wantKind, wantID, _ = strings.Cut(subject, ":")
	}
	var hits []fakeEvent
	for _, e := range f.events {
		if subject == "" || (e.row.SubjectKind == wantKind && e.row.SubjectID == wantID) {
			hits = append(hits, e)
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].row.At != hits[j].row.At {
			return hits[i].row.At > hits[j].row.At
		}
		return hits[i].row.ID > hits[j].row.ID
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]policy.EventRow, 0, len(hits))
	for _, e := range hits {
		out = append(out, e.row)
	}
	return out, nil
}

// PolicyEventTrim satisfies the grown Repo: strict `<` against the cutoff
// (a row AT the cutoff survives), reporting the count the SQL's RowsAffected
// would. The cutoff is captured so a test can pin that TrimEvents measured it
// from the SUPPLIED clock minus the named cap.
func (f *fakeRepo) PolicyEventTrim(_ context.Context, olderThan time.Time) (int, error) {
	f.trimCalls++
	f.lastTrim = olderThan
	if f.trimErr != nil {
		return 0, f.trimErr
	}
	var kept []fakeEvent
	n := 0
	for _, e := range f.events {
		if e.at.Before(olderThan) {
			n++
			continue
		}
		kept = append(kept, e)
	}
	f.events = kept
	return n, nil
}

// PolicyEventStats satisfies the grown Repo: the depth and the oldest retained
// instant FOR THE SUBJECT ASKED ABOUT (F-13) — "" is the whole table, a
// `kind:id` is that subject's rows, matched on the same split columns the
// insert wrote. The subject it was handed is recorded so a test can assert the
// filter travelled from the flag to the seam, and statsCalls still counts every
// call (TestTrimmingIsNotInvokedOnInsert reads that counter). The empty answer
// is (0, zero, nil) — the honest "no rows", never an error and never another
// subject's number.
func (f *fakeRepo) PolicyEventStats(_ context.Context, subject string) (int, time.Time, error) {
	f.statsCalls++
	f.lastStatsSubject = subject
	if f.statsErr != nil {
		return 0, time.Time{}, f.statsErr
	}
	var wantKind, wantID string
	if subject != "" {
		wantKind, wantID, _ = strings.Cut(subject, ":")
	}
	depth, oldest := 0, time.Time{}
	for _, e := range f.events {
		if subject != "" && (e.row.SubjectKind != wantKind || e.row.SubjectID != wantID) {
			continue
		}
		depth++
		if oldest.IsZero() || e.at.Before(oldest) {
			oldest = e.at
		}
	}
	return depth, oldest, nil
}

// ── events helpers ───────────────────────────────────────────────────────────

// eventT0 is the fixed "now" every retention test measures its cap from; the
// T-instants are distinct seconds with distinct nanosecond tails (D5: fixed,
// never read).
var (
	eventT0 = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	eventT1 = time.Date(2026, 10, 19, 11, 59, 59, 111000000, time.UTC)
	eventT2 = time.Date(2026, 10, 19, 10, 0, 0, 222000000, time.UTC)
	eventT3 = time.Date(2026, 9, 1, 9, 0, 0, 333000000, time.UTC)
)

// denyVerdict builds a REAL policy-deny verdict the way enforcement will:
// the deny is stored through Add, read back through List, decided by
// Evaluate. (Hand-built verdicts are this file's due for the shapes Evaluate
// cannot produce on demand — the override suffix, D10, long types.)
func denyVerdict(t *testing.T, s *policy.Store, id, agent, tool string) policy.Verdict {
	t.Helper()
	mustAdd(t, s, boundaryPolicy(t, id, policy.AllAgents, "", policy.Deny, policy.Operator,
		spec(fmt.Sprintf(`{"toolName":%q,"state":"deny"}`, tool))))
	ps, rej := s.List(context.Background(), evalAt)
	if rej != nil {
		t.Fatalf("List refused its own row: %s", rej.Reason)
	}
	v := policy.Evaluate(ps, policy.Request{Type: policy.ToolAccess, Agent: agent, Tool: tool}, evalAt)
	if v.Allowed || v.By == nil || v.By.ID != id {
		t.Fatalf("helper did not produce a policy denial naming %q: %+v", id, v)
	}
	return v
}

func mustRecordRefusal(t *testing.T, s *policy.Store, subject string, v policy.Verdict, at time.Time) {
	t.Helper()
	if rej := s.RecordRefusal(context.Background(), subject, v, at); rej != nil {
		t.Fatalf("RecordRefusal(%s) refused: %s", subject, rej.Reason)
	}
}

func refuseRefusal(t *testing.T, s *policy.Store, subject string, v policy.Verdict, at time.Time) string {
	t.Helper()
	rej := s.RecordRefusal(context.Background(), subject, v, at)
	if rej == nil {
		t.Fatalf("RecordRefusal(%s) was ACCEPTED; it must be refused", subject)
	}
	return rej.Reason
}

func mustReport(t *testing.T, s *policy.Store, subject string, limit int) (policy.EventReport, []policy.EventRow) {
	t.Helper()
	report, rows, rej := s.ReportEvents(context.Background(), subject, limit)
	if rej != nil {
		t.Fatalf("ReportEvents(%q, %d) refused: %s", subject, limit, rej.Reason)
	}
	return report, rows
}

// wantTruncated INDEPENDENTLY recomputes the documented truncation (head n/4
// + tail the rest, []rune, nothing appended) so the no-secret test asserts
// detail equals the truncation as DOCUMENTED, not as the production helper
// happens to compute it — a shared helper would let a wrong algorithm
// self-certify.
func wantTruncated(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	head := n / 4
	return string(r[:head]) + string(r[len(r)-(n-head):])
}

// ── THE tests: every refusal, with its subject ───────────────────────────────

func TestEveryRefusalIsRecordedWithItsSubject(t *testing.T) {
	// The audit trail's promise: "every time a policy refused something, with
	// its subject." A deny is stored, a real request is evaluated, the refusal
	// is recorded — and everything the row must carry is checked BOTH at the
	// seam (the forwarding claim) and read back (the storage claim). The
	// default-deny twin proves the NULL policy_id: PL5 refuses without naming
	// a rule, and the honest row says so rather than inventing one.
	repo := &fakeRepo{}
	s := policy.NewStore(repo)
	v := denyVerdict(t, s, "no-bash", "boss", "bash")

	mustRecordRefusal(t, s, "agent:boss", v, eventT1)

	// The seam: exactly these values crossed, unedited, instant whole.
	if repo.eventInsertCalls != 1 {
		t.Fatalf("the seam saw %d inserts, want 1", repo.eventInsertCalls)
	}
	got := repo.recordedEvents[0]
	if got.kind != "refusal" || got.subject != "agent:boss" || got.policyID != "no-bash" || !got.at.Equal(eventT1) {
		t.Fatalf("the seam received %+v", got)
	}
	if got.detail != v.Sentence() {
		t.Errorf("the seam received detail %q; the row must hold the verdict's own sentence verbatim", got.detail)
	}

	// The row, read back: kind, subject kind+id, policy id, instant all land.
	report, rows := mustReport(t, s, "agent:boss", 10)
	if report.Depth != 1 || len(rows) != 1 {
		t.Fatalf("depth %d / %d rows, want 1/1: %+v", report.Depth, len(rows), rows)
	}
	row := rows[0]
	if row.Outcome != "refusal" {
		t.Errorf("outcome %q, want refusal", row.Outcome)
	}
	if row.SubjectKind != "agent" || row.SubjectID != "boss" {
		t.Errorf("subject stored as %q/%q, want agent/boss", row.SubjectKind, row.SubjectID)
	}
	if row.PolicyID != "no-bash" {
		t.Errorf("policy id %q, want no-bash", row.PolicyID)
	}
	if row.Detail != v.Sentence() {
		t.Errorf("detail %q, want the verdict sentence verbatim", row.Detail)
	}
	at, err := time.Parse(time.RFC3339Nano, row.At)
	if err != nil || !at.Equal(eventT1) {
		t.Errorf("at %q does not read back as the recorded instant (%v)", row.At, err)
	}

	// The other subject sees nothing of it (the WHERE is a WHERE).
	if _, other := mustReport(t, s, "agent:other", 10); len(other) != 0 {
		t.Errorf("another subject saw %d rows", len(other))
	}

	// The default-deny twin: no rule to name, so the policy_id column is NULL
	// ("" through the seam) — the record refuses nobody, it just cannot
	// invent a rule the verdict does not name.
	repo2 := &fakeRepo{}
	s2 := policy.NewStore(repo2)
	dv := policy.Evaluate(nil, policy.Request{Type: policy.ToolAccess, Agent: "boss", Tool: "bash"}, evalAt)
	if !dv.DefaultDeny {
		t.Fatalf("the empty set must produce the default deny, got %+v", dv)
	}
	mustRecordRefusal(t, s2, "agent:boss", dv, eventT2)
	if repo2.recordedEvents[0].policyID != "" {
		t.Errorf("the default deny recorded policy id %q; it names no rule — the column is NULL", repo2.recordedEvents[0].policyID)
	}
	_, rows2 := mustReport(t, s2, "agent:boss", 10)
	if len(rows2) != 1 || rows2[0].PolicyID != "" || !strings.HasSuffix(rows2[0].Detail, "default deny (PL5)") {
		t.Errorf("the PL5 row lost its state-bearing clause or grew a policy id: %+v", rows2)
	}
}

func TestDetailNeverCarriesContentOrASecret(t *testing.T) {
	// Legal constraint 1 as a test: the policy's Spec carries a secret token
	// and the policy id is 500 runes long — detail must come out as exactly
	// the documented truncation of v.Sentence() (whose fields are the gated
	// identifiers and the fixed prose), carrying NO byte of the Spec, NO
	// secret, and never over the named cap. The reference's discipline
	// ("NEVER content, never secrets") studied and re-expressed; its head-cut
	// deliberately NOT copied (see truncateRunes' argument).
	const secret = "SECRET-TOKEN-abc"
	longID := strings.Repeat("a", 500)

	repo := &fakeRepo{}
	s := policy.NewStore(repo)
	// Hand-built deliberately (and never through Add): Add's identifier
	// policy refuses a 500-rune id at authoring — this verdict exists only in
	// memory, as a decision the audit still owes a row for. The secret sits
	// inside the toolName — content-shaped bytes the REQUEST carries and the
	// row must never quote; Spec bytes are doubly out (strict decode drops
	// even unknown keys, so this hand-built policy's Spec exists only in
	// memory too, and the sentence is the only admissible source).
	p := policy.Policy{
		ID: longID, Type: policy.ToolAccess, Action: policy.Deny, Source: policy.Operator,
		Target: targetOf(t, policy.AllAgents, ""),
		Spec:   spec(`{"toolName":"bash-` + secret + `","state":"deny"}`),
	}
	v := policy.Evaluate([]policy.Policy{p}, policy.Request{Type: policy.ToolAccess, Agent: "boss", Tool: "bash-" + secret}, evalAt)
	if v.Allowed || v.By == nil {
		t.Fatalf("the hand-built deny did not decide: %+v", v)
	}
	sentence := v.Sentence()
	if utf8.RuneCountInString(sentence) <= policy.MaxEventDetailRunes {
		t.Fatalf("the test built a %d-rune sentence; it must overflow the cap to test the cap",
			utf8.RuneCountInString(sentence))
	}

	mustRecordRefusal(t, s, "agent:boss", v, eventT1)
	_, rows := mustReport(t, s, "agent:boss", 10)
	if len(rows) != 1 {
		t.Fatalf("%d rows, want 1", len(rows))
	}
	detail := rows[0].Detail

	for _, forbidden := range []string{secret, "SECRET-TOKEN", "toolName", `"note"`, `{"toolName":"bash"`} {
		if strings.Contains(detail, forbidden) {
			t.Errorf("detail carries content (%q) — detail derives only from the verdict sentence, never Spec bytes "+
				"or tool arguments: %s", forbidden, detail)
		}
	}
	if n := utf8.RuneCountInString(detail); n > policy.MaxEventDetailRunes {
		t.Errorf("detail is %d runes; the named cap MaxEventDetailRunes=%d is enforced ON WRITE", n, policy.MaxEventDetailRunes)
	}
	if want := wantTruncated(sentence, policy.MaxEventDetailRunes); detail != want {
		t.Errorf("detail is not the documented truncation of the sentence.\n got %q\nwant %q", detail, want)
	}
	// And the truncated row still says what happened: the head keeps the
	// refusal prefix, the tail keeps target/source/expiry — the reader is not
	// left guessing which refusal this was.
	if !strings.HasPrefix(detail, "refused: ") || !strings.HasSuffix(detail, "· source operator · no expiry") {
		t.Errorf("the truncated detail lost its head or its tail: %s", detail)
	}
}

func TestTruncationPreservesTheStateBearingTail(t *testing.T) {
	// The carried HIGH from the T6 security gate: a naive []rune(s)[:256]
	// head-cut silently drops the clause that tells a reader what actually
	// happened — the PL4 override naming, the (D10) marker, the PL5 default.
	// Every overflowing sentence here keeps its FINAL clause and the "refused:"
	// head; the cut eats the middle. And the PL5 line, whose subject is
	// bounded by the identifier policy, fits under the cap BY CONSTRUCTION —
	// the strongest possible preservation: it is never truncated at all.
	cases := []struct {
		name    string
		verdict policy.Verdict
		must    []string // each must survive, present in the stored detail
	}{
		{
			// Long ids on both halves of a PL4 denial push the sentence past 256;
			// the override clause (naming the beaten allow) sits at the tail and
			// the 192-rune tail budget holds it whole — measured, not hoped.
			name: "a PL4 denial over a 64-rune allow names the allow it beat",
			verdict: policy.Verdict{
				Subject: "agent:boss", Type: policy.ToolAccess,
				By: &policy.Policy{ID: strings.Repeat("d", 64), Type: policy.ToolAccess,
					Action: policy.Deny, Source: policy.Operator, Target: targetOf(t, policy.LaneTypeKind, "review")},
				OverriddenAllow: &policy.Policy{ID: strings.Repeat("a", 64), Type: policy.ToolAccess,
					Action: policy.Allow, Source: policy.Operator, Target: targetOf(t, policy.AgentKind, "boss")},
			},
			must: []string{"refused: ", "(an allow exists: \"", strings.Repeat("a", 64), "denied anyway, PL4)"},
		},
		{
			// D10 with a 200-rune type: the "(D10)" marker is the last thing the
			// sentence says, so the tail carries it — a reader can always tell
			// "nobody can answer yet" from "a rule refused you" even truncated.
			name: "an unimplemented refusal keeps its (D10) marker",
			verdict: policy.Verdict{
				Subject: "agent:boss", Type: policy.PolicyType(strings.Repeat("x", 200)), Unimplemented: true,
			},
			must: []string{"refused: ", "has no evaluator yet", "refusing rather than permitting (D10)"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repo := &fakeRepo{}
			s := policy.NewStore(repo)
			sentence := c.verdict.Sentence()
			if utf8.RuneCountInString(sentence) <= policy.MaxEventDetailRunes {
				t.Fatalf("the test built a %d-rune sentence; the case must overflow the cap",
					utf8.RuneCountInString(sentence))
			}
			mustRecordRefusal(t, s, c.verdict.Subject, c.verdict, eventT1)
			_, rows := mustReport(t, s, c.verdict.Subject, 10)
			if len(rows) != 1 {
				t.Fatalf("%d rows, want 1", len(rows))
			}
			detail := rows[0].Detail
			if n := utf8.RuneCountInString(detail); n > policy.MaxEventDetailRunes {
				t.Errorf("detail is %d runes, over the cap", n)
			}
			for _, want := range c.must {
				if !strings.Contains(detail, want) {
					t.Errorf("the state-bearing clause %q was cut — the audit row no longer says what happened: %s", want, detail)
				}
			}
			if !strings.HasPrefix(detail, "refused: ") {
				t.Errorf("the head budget lost the refusal prefix (is this row even a refusal?): %s", detail)
			}
			// The final clause is the LAST thing on the sentence; the tail
			// keeps the sentence's end, so a truncation can never amputate it.
			sr := []rune(sentence)
			if final := string(sr[len(sr)-20:]); !strings.HasSuffix(detail, final) {
				t.Errorf("the sentence's final runes were cut: %s", detail)
			}
		})
	}

	// The PL5 construction property: with the subject bounded by the
	// identifier policy (64 runes) and the rest fixed prose, the default-deny
	// line fits under the cap for ANY legal subject — so its state-bearing
	// clause survives not by truncation luck but by arithmetic. Assert the
	// arithmetic AND the longest-legal instance end to end.
	if 9+len("agent:")+64+len(" · no allow matched · default deny (PL5)") > policy.MaxEventDetailRunes {
		t.Fatal("the PL5 line no longer fits the cap by construction; re-measure the budgets in truncateRunes")
	}
	repo := &fakeRepo{}
	s := policy.NewStore(repo)
	longest := "agent:" + strings.Repeat("b", policy.MaxIdentifierRunes)
	pd := policy.Verdict{Subject: longest, Type: policy.ToolAccess, DefaultDeny: true}
	mustRecordRefusal(t, s, longest, pd, eventT1)
	_, rows := mustReport(t, s, longest, 10)
	if len(rows) != 1 || rows[0].Detail != pd.Sentence() {
		t.Fatalf("the longest-legal PL5 row was not stored verbatim: %+v", rows)
	}
	if !strings.HasSuffix(rows[0].Detail, "default deny (PL5)") {
		t.Errorf("the PL5 clause is gone: %s", rows[0].Detail)
	}
}

func TestTruncationIsRuneSafeNeverABrokenByte(t *testing.T) {
	// The reference's own guarantee, kept: a multi-byte rune is never split.
	// A 500-rune id of 3-byte runes overflows the cap; the stored detail is
	// valid UTF-8, exactly the independent wantTruncated(...) of the sentence,
	// and exactly cap runes — no replacement characters, no torn bytes.
	repo := &fakeRepo{}
	s := policy.NewStore(repo)
	p := policy.Policy{
		ID: strings.Repeat("🐉漢x", 167), Type: policy.ToolAccess, Action: policy.Deny, Source: policy.Operator,
		Target: targetOf(t, policy.AllAgents, ""), Spec: spec(`{"toolName":"bash","state":"deny"}`),
	}
	v := policy.Evaluate([]policy.Policy{p}, policy.Request{Type: policy.ToolAccess, Agent: "boss", Tool: "bash"}, evalAt)
	if v.Allowed || v.By == nil {
		t.Fatalf("the multi-byte deny did not decide: %+v", v)
	}
	mustRecordRefusal(t, s, "agent:boss", v, eventT1)
	_, rows := mustReport(t, s, "agent:boss", 10)
	detail := rows[0].Detail
	if !utf8.ValidString(detail) {
		t.Errorf("detail is not valid UTF-8 — a rune was split on the way in: %q", detail)
	}
	if n := utf8.RuneCountInString(detail); n != policy.MaxEventDetailRunes {
		t.Errorf("detail is %d runes; an overflowing sentence truncates to exactly the cap", n)
	}
	if want := wantTruncated(v.Sentence(), policy.MaxEventDetailRunes); detail != want {
		t.Errorf("rune-safe truncation diverged from the documented shape\n got %q\nwant %q", detail, want)
	}
}

func TestRecordRefusalRefusesToRecordAnAllowedVerdict(t *testing.T) {
	// An audit row whose outcome says "refusal" while the verdict says
	// permitted is the row that lies. Refusal-to-record, loudly, before the
	// seam — an audit keeps every refusal and invents none.
	repo := &fakeRepo{}
	s := policy.NewStore(repo)
	allowed := policy.Verdict{
		Subject: "agent:boss", Type: policy.ToolAccess, Allowed: true,
		By: &policy.Policy{ID: "yes-bash", Type: policy.ToolAccess, Action: policy.Allow,
			Source: policy.Operator, Target: targetOf(t, policy.AgentKind, "boss")},
	}
	got := refuseRefusal(t, s, "agent:boss", allowed, eventT1)
	for _, want := range []string{"ALLOW", "contradicts"} {
		if !strings.Contains(got, want) {
			t.Errorf("the refusal does not name %q: %s", want, got)
		}
	}
	if repo.eventInsertCalls != 0 {
		t.Errorf("the allow reached the seam (%d inserts)", repo.eventInsertCalls)
	}
}

func TestRecordRefusalValidatesBeforeTheSeam(t *testing.T) {
	// The boundary trusts no caller, write included: every refused shape is
	// named, and none of them reaches the repo. The subject forgery case is
	// the audit-forgery guard — the columns say who, the detail quotes a
	// sentence about who, and they must agree.
	repo := &fakeRepo{}
	s := policy.NewStore(repo)
	v := denyVerdict(t, s, "no-bash", "boss", "bash")
	repo.eventInsertCalls = 0

	// Hand-built refusals for the malformed-subject rows: the forgery guard
	// (subject must equal what the verdict renders about) fires FIRST, so a
	// case only reaches parseSubject when its verdict agrees — meaning the
	// hand verdict carries the malformed subject too, exactly as a table
	// edited by hand would. (The PL5 line renders from the fields; the
	// subject travels verbatim into the sentence, which is itself asserted
	// by T4.)
	hand := func(subject string) policy.Verdict {
		return policy.Verdict{Subject: subject, Type: policy.ToolAccess}
	}

	for _, c := range []struct {
		name    string
		subject string
		verdict policy.Verdict // zero Type → the real deny verdict v
		at      time.Time
		must    []string
	}{
		{"subject disagrees with the verdict", "agent:other", policy.Verdict{}, eventT1, []string{"does not match", "forgery"}},
		{"subject with no kind", "boss", hand("boss"), eventT1, []string{`no ":"`}},
		{"subject with an invented kind", "tool:bash", hand("tool:bash"), eventT1, []string{"agent", "loop"}},
		{"subject id over the cap", "agent:" + strings.Repeat("b", 65), hand("agent:" + strings.Repeat("b", 65)), eventT1,
			[]string{"subject id", "65 runes"}},
		{"subject id with a newline", "agent:bo\nss", hand("agent:bo\nss"), eventT1,
			[]string{"subject id", "control character U+000A"}},
		{"no instant at all", "agent:boss", policy.Verdict{}, time.Time{}, []string{"no instant", "D5"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			verdict := c.verdict
			if verdict.Type == "" {
				verdict = v
			}
			got := refuseRefusal(t, s, c.subject, verdict, c.at)
			for _, want := range c.must {
				if !strings.Contains(got, want) {
					t.Errorf("the refusal does not name %q: %s", want, got)
				}
			}
		})
	}
	if repo.eventInsertCalls != 0 {
		t.Errorf("%d refused records reached the repo", repo.eventInsertCalls)
	}
}

func TestRecordRefusalGatesTheRecordedText(t *testing.T) {
	// The two gates that sit between a verdict and the two text columns: a
	// hand-built By can smuggle terminal-active bytes into the policy id or
	// into the sentence (through a field Sentence renders raw — Source).
	// Both are refused naming the column they guard, and neither reaches the
	// seam: an audit row whose text needs an xterm is SEC-013 arriving
	// through the log, and the one writer refuses it at the door.
	for _, c := range []struct {
		name string
		by   policy.Policy
		must []string
	}{
		{"a policy id with a newline",
			policy.Policy{ID: "bad\nid", Type: policy.ToolAccess, Action: policy.Deny,
				Source: policy.Operator, Target: policy.Target{Kind: policy.AgentKind, ID: "boss"}},
			[]string{"policy id", "control character U+000A"}},
		{"a source that hides a newline inside the sentence",
			policy.Policy{ID: "clean", Type: policy.ToolAccess, Action: policy.Deny,
				Source: policy.Source("opera\ntor"), Target: policy.Target{Kind: policy.AgentKind, ID: "boss"}},
			[]string{"detail", "control character U+000A"}},
		{"a policy id of pure zero-widths",
			policy.Policy{ID: "\u200b\u200b", Type: policy.ToolAccess, Action: policy.Deny,
				Source: policy.Operator, Target: policy.Target{Kind: policy.AgentKind, ID: "boss"}},
			[]string{"policy id", "no visible character"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			repo := &fakeRepo{}
			s := policy.NewStore(repo)
			by := c.by
			v := policy.Verdict{Subject: "agent:boss", Type: policy.ToolAccess, By: &by}
			got := refuseRefusal(t, s, "agent:boss", v, eventT1)
			for _, want := range c.must {
				if !strings.Contains(got, want) {
					t.Errorf("the refusal does not name %q: %s", want, got)
				}
			}
			if repo.eventInsertCalls != 0 {
				t.Errorf("the un-gated text reached the seam (%d inserts)", repo.eventInsertCalls)
			}
		})
	}
}

func TestRecordRefusalFoldsAStoreFailureIntoARefusal(t *testing.T) {
	// Add's single-return-shape discipline at the record seam: the machine
	// breaking arrives as a *Reject that says the record was NOT made — the
	// caller's answer is one thing (tell the human; the audit is missing this
	// row) and never a half-written row nobody was told about.
	repo := &fakeRepo{eventErr: errors.New("disk on fire")}
	s := policy.NewStore(repo)
	v := denyVerdict(t, policy.NewStore(&fakeRepo{}), "no-bash", "boss", "bash")
	got := refuseRefusal(t, s, "agent:boss", v, eventT1)
	for _, want := range []string{"was NOT recorded", "disk on fire"} {
		if !strings.Contains(got, want) {
			t.Errorf("the fold lost %q: %s", want, got)
		}
	}
}

// ── the guards the criteria name ─────────────────────────────────────────────

func TestPolicyEventInsertHasExactlyOneCaller(t *testing.T) {
	// Legal constraint 1's structure, as a grep: recordEvent — unexported, in
	// record.go — is the ONLY caller of Repo.PolicyEventInsert. A second
	// caller is a second writer, and a second writer is a second place where
	// the 256-rune no-content bound can be got wrong (the reason the function
	// is unexported is that no caller outside this file can reach it).
	root := moduleRoot(t)
	calls := 0
	var where []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name != root && (strings.HasPrefix(name, ".") || name == "node_modules" || name == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		// Count CALLS (`.PolicyEventInsert(`) — not the definitions, not the
		// interface entry, not the bridge forwarding `f.EventInsert(...)`.
		for _, line := range strings.Split(string(src), "\n") {
			if strings.Contains(line, ".PolicyEventInsert(") {
				calls++
				rel, _ := filepath.Rel(root, path)
				where = append(where, rel)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk the module: %v", err)
	}
	if calls != 1 || len(where) != 1 || !strings.HasSuffix(where[0], "record.go") {
		t.Errorf("PolicyEventInsert has %d call sites %v; the sole writer is record.go's recordEvent — "+
			"a second caller is a second writer (legal constraint 1)", calls, where)
	}

	// And the pin ABOVE IT (T8 review, DEF-029): recordEvent is itself the
	// one door the detail bound is enforced at, so what must be counted is
	// WHO CALLS recordEvent. Exactly three callers, all in record.go —
	// RecordRefusal, RecordDisagreement (T10) and RecordAuthored (S11 loop
	// T9's authored record). A fourth caller outside the file is unreachable
	// by construction (the method is unexported) but an extra in-file caller
	// that skipped the cap is exactly what this grep plus recordEvent's own
	// re-check now makes doubly impossible.
	rcalls := 0
	var rwhere []string
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name != root && (strings.HasPrefix(name, ".") || name == "node_modules" || name == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, line := range strings.Split(string(src), "\n") {
			// The DEFINITION line reads `func (s *Store) recordEvent(` — no
			// dot precedes the name; only method CALLS match this pattern.
			if strings.Contains(line, ".recordEvent(") {
				rcalls++
				rel, _ := filepath.Rel(root, path)
				rwhere = append(rwhere, rel)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk the module for recordEvent callers: %v", err)
	}
	if rcalls != 3 || len(rwhere) != 3 || rwhere[0] != rwhere[1] || rwhere[1] != rwhere[2] ||
		!strings.HasSuffix(rwhere[0], "record.go") {
		t.Errorf("recordEvent has %d call sites %v; the three callers are RecordRefusal, RecordDisagreement "+
			"and RecordAuthored, all in record.go — an extra is a handover nobody bounded (DEF-029)", rcalls, rwhere)
	}
}

func TestTheEnforcementPathReadsNoAcceptanceRows(t *testing.T) {
	// SEC-029's committed probe. The coupling grep (TestEvaluateDoesNotRead
	// PolicyAcceptance) walks evaluate.go and types.go — it cannot see a
	// verdict-neutral acceptance READ arriving through store.go, and until
	// this test no committed test could see that either. So the counting fake
	// is the second lock: with acceptance rows ON the table (the temptation
	// exists), the full enforcement path — List, then Evaluate over what it
	// returns, then RecordRefusal — reads the acceptance seam EXACTLY ZERO
	// times. A future "just check whether they were told" refactor that
	// threads a read through the adapter fails HERE, in the counter, even
	// though it never touches the grep's two files. Enforcement deciding
	// without reading the record is the whole of PL10; this test is the half
	// that the grep structurally cannot cover.
	repo := &fakeRepo{}
	s := policy.NewStore(repo)
	mustAdd(t, s, boundaryPolicy(t, "no-bash", policy.AgentKind, "boss", policy.Deny, policy.Operator, denySpec()))

	// Seed acceptances FIRST — so the rows exist and a coupling has something
	// to read — then prove the probe can count: two reads through the
	// boundary's own acceptance path must move the counter before it is
	// zeroed (a dead counter would certify any amount of reading).
	target := targetOf(t, policy.AgentKind, "boss")
	mustRecord(t, s, "no-bash", target, "operator", acceptT1)
	mustRecord(t, s, "no-bash", targetOf(t, policy.LoopKind, "l"), "boss", acceptT2)
	for i := 0; i < 2; i++ {
		if _, rej := s.AcceptancesFor(context.Background(), target); rej != nil {
			t.Fatalf("AcceptancesFor refused its own rows: %s", rej.Reason)
		}
	}
	if repo.listForCalls != 2 {
		t.Fatalf("the fake is not counting (got %d, want 2) — a probe that cannot count proves nothing", repo.listForCalls)
	}
	repo.listForCalls = 0

	ps, rej := s.List(context.Background(), evalAt)
	if rej != nil {
		t.Fatalf("List refused its own rows: %s", rej.Reason)
	}
	v := policy.Evaluate(ps, policy.Request{Type: policy.ToolAccess, Agent: "boss", Tool: "bash"}, evalAt)
	if v.Allowed || v.By == nil || v.By.ID != "no-bash" {
		t.Fatalf("the unaccepted deny did not bite (PL10): %+v", v)
	}
	if rej := s.RecordRefusal(context.Background(), v.Subject, v, eventT1); rej != nil {
		t.Fatalf("recording the refusal was refused: %s", rej.Reason)
	}

	if repo.listCalls == 0 || repo.eventInsertCalls != 1 {
		t.Fatalf("the enforcement path did not run (policy list %d, event insert %d) — a probe whose path "+
			"never walked would certify silence vacuously", repo.listCalls, repo.eventInsertCalls)
	}
	if repo.listForCalls != 0 {
		t.Errorf("the enforcement path performed %d acceptance reads — a verdict-neutral read through the "+
			"store seam is invisible to the coupling grep, and this counter is the committed probe (SEC-029): "+
			"an unaccepted policy is still enforced, and enforcement must not even LOOK at whether anyone knew",
			repo.listForCalls)
	}
}

// ── the RepoFuncs bridge, events half (what the wiring fills in) ─────────────

func TestRepoFuncsBridgeRecordsAndReportsEvents(t *testing.T) {
	var inserted []recordedEvent
	var trimmed time.Time
	var statsSubject string
	oldest := eventT3
	bridge := policy.RepoFuncs{
		EventInsert: func(_ context.Context, kind, subject, policyID, detail string, at time.Time) error {
			inserted = append(inserted, recordedEvent{kind, subject, policyID, detail, at})
			return nil
		},
		EventQuery: func(_ context.Context, subject string, limit int) ([]policy.EventRow, error) {
			return []policy.EventRow{{ID: 1, SubjectKind: "agent", SubjectID: strings.TrimPrefix(subject, "agent:"),
				Outcome: "refusal", Detail: "refused: bridge · no allow matched", At: eventT1.UTC().Format(fakeEventAtLayout)}}, nil
		},
		EventTrim: func(_ context.Context, olderThan time.Time) (int, error) {
			trimmed = olderThan
			return 7, nil
		},
		// The subject the listing asked for is the subject the depth is read
		// for (F-13) — captured here so the forwarding claim is asserted.
		EventStats: func(_ context.Context, subject string) (int, time.Time, error) {
			statsSubject = subject
			return 9, oldest, nil
		},
	}
	s := policy.NewStore(bridge)
	v := denyVerdict(t, policy.NewStore(&fakeRepo{}), "no-bash", "boss", "bash")
	mustRecordRefusal(t, s, "agent:boss", v, eventT1)
	if len(inserted) != 1 || inserted[0].kind != "refusal" || inserted[0].policyID != "no-bash" || !inserted[0].at.Equal(eventT1) {
		t.Fatalf("the bridge did not forward the insert: %+v", inserted)
	}
	report, rows := mustReport(t, s, "agent:boss", 10)
	if report.Depth != 9 || !report.Oldest.Equal(oldest) || report.Cap != policy.PolicyEventAgeCap || len(rows) != 1 {
		t.Errorf("the bridge did not forward the report: %+v / %d rows", report, len(rows))
	}
	if statsSubject != "agent:boss" {
		t.Errorf("the bridge forwarded the depth read for %q; the listing asked for %q — the footer would "+
			"describe rows the caller did not ask about (F-13)", statsSubject, "agent:boss")
	}
	n, err := s.TrimEvents(context.Background(), eventT0)
	if err != nil || n != 7 || !trimmed.Equal(eventT0.Add(-policy.PolicyEventAgeCap)) {
		t.Errorf("the bridge did not forward the trim: n=%d err=%v cutoff=%v", n, err, trimmed)
	}
}

func TestRepoFuncsBridgeRefusesMissingEventHooks(t *testing.T) {
	// The same rule as the acceptance half: a half-wired bridge names the
	// missing hook (an infrastructure error at the trim/report, a *Reject at
	// the record — the shape each call already returns), never a nil panic
	// mid-record.
	s := policy.NewStore(policy.RepoFuncs{})
	v := denyVerdict(t, policy.NewStore(&fakeRepo{}), "no-bash", "boss", "bash")
	if rej := s.RecordRefusal(context.Background(), "agent:boss", v, eventT1); rej == nil ||
		!strings.Contains(rej.Reason, "no EventInsert function") {
		t.Errorf("the missing EventInsert hook did not name itself: %+v", rej)
	}
	if _, _, rej := s.ReportEvents(context.Background(), "", 10); rej == nil ||
		!strings.Contains(rej.Reason, "no EventStats function") {
		t.Errorf("the missing EventStats hook did not name itself: %+v", rej)
	}
	if _, err := s.TrimEvents(context.Background(), eventT0); err == nil ||
		!strings.Contains(err.Error(), "no EventTrim function") {
		t.Errorf("the missing EventTrim hook did not name itself: %v", err)
	}
	// The query hook hides behind the stats read (ReportEvents reads depth
	// first), so it needs a wiring that answers depth and nothing else.
	s2 := policy.NewStore(policy.RepoFuncs{
		EventStats: func(_ context.Context, _ string) (int, time.Time, error) { return 0, time.Time{}, nil },
	})
	if _, _, rej := s2.ReportEvents(context.Background(), "", 10); rej == nil ||
		!strings.Contains(rej.Reason, "no EventQuery function") {
		t.Errorf("the missing EventQuery hook did not name itself: %+v", rej)
	}
}
