package policy

// ════════════════════════════════════════════════════════════════════════════
// Retention: the 90-day age cap (PL11/D15) and the report it owes.
//
// # The two jobs this file does, and the one it refuses
//
// policy_event is written every time a policy refuses something — and a loop
// that retries a denied tool twenty times a round writes twenty rows. That is
// unbounded, and "every refusal forever" is how an audit table becomes the
// reason somebody deletes the database instead of reading it (PL11 — decided
// now, not discovered at 2 GB). The answer is an AGE CAP, and this file owns
// it: TrimEvents deletes what is past the cap, and ReportEvents answers "how
// deep is it, how long is the cap, what is the oldest row" — together.
//
// What this file refuses is the trim on the write path. Trimming belongs to
// daemon start (the daemon's startup path calls TrimEvents once — the call
// site itself lands with T13's daemon surface / T12's CLI work) and to demand
// (`aeon policy events`, T12). Every refusal paying for housekeeping is the
// seed's T7 rule, and the spy proves the rule holds: zero PolicyEventTrim
// calls across fifty RecordRefusal calls (retention_test.go).
//
// # The cap must be VISIBLE, not just present (D15)
//
// A silently-trimming log is one that loses the row somebody came looking for
// and never says so. So the cap is not a number in a WHERE clause nobody
// reads: it is a named const (PolicyEventAgeCap), it travels on every report
// (EventReport.Cap is that const, always), and the report carries the depth
// and the oldest retained instant beside it, so "nothing older than this
// exists" is a statement `aeon policy events --depth` prints (T12) and
// `doctor` surfaces (S23) — not an inference from absence.
//
// # No clock reads (D5), no SQL (I2)
//
// `now` arrives as a parameter; the cutoff arithmetic (now − cap) is here
// because the cap is here. The statements themselves live in
// internal/store/policy.go and reach through the Repo seam; this file imports
// neither database/sql nor the store.
// ════════════════════════════════════════════════════════════════════════════

import (
	"context"
	"fmt"
	"time"
	"unicode/utf8"
)

// PolicyEventAgeCap is how long a policy_event row is retained: 90 days.
//
// This is Q2's answer, written down as a judgement rather than left as a
// default nobody chose: ninety days comfortably covers a full quarter of
// operations — the horizon an operator actually reasons over when asking
// "was this loop ever refused, and when" — while keeping the worst-case
// table small enough that `aeon db info` never becomes an apology.
//
// Age, never row count (PL11, the seed's argument kept where the next editor
// reads it): a count cap trims a quiet week and a busy week differently, so
// "what happened in July" would depend on how busy August was. Age is the
// property somebody actually reasons about, and the one ReportEvents can
// state in plain words ("cap 90 days", "oldest …") without caveats.
//
// It is a const so no surface can drift its own copy; S23's `doctor` reports
// depth against THIS cap, and the CLI prints THIS value.
const PolicyEventAgeCap = 90 * 24 * time.Hour

// TrimEvents deletes every policy_event row strictly older than now minus the
// cap, and reports how many went. Exactly at the cutoff a row SURVIVES — the
// boundary is `<`, and retention_test.go calls both sides of it at one second
// in from the edge.
//
// The cutoff arithmetic lives HERE because the cap lives here: `now` is a
// parameter (D5 — no clock is this boundary's to read), and a zero `now` is
// refused with the clock named, not silently read: a trim from a lost clock
// would either delete nothing and answer as if it had, and the one error an
// audit operation may never produce is a quiet wrong zero.
//
// WHO CALLS THIS, and only these two: the daemon's startup path (one call per
// boot — wiring lands with T13/T12), and `aeon policy events` on demand (T12).
// NEVER the record path: recordEvent/RecordRefusal do not reach here (PL11;
// TestTrimmingIsNotInvokedOnInsert holds the spy at zero across fifty
// refusals). A store failure arrives as the plain `error` it is (infrastructure
// vocabulary — this is not a domain refusal; the caller retries at the next
// boot and misses nothing meanwhile).
func (s *Store) TrimEvents(ctx context.Context, now time.Time) (int, error) {
	if now.IsZero() {
		return 0, fmt.Errorf("policy: TrimEvents has no instant to measure the cap from; the clock is a " +
			"parameter the caller supplies (D5), and a lost clock must not read as a trim")
	}
	n, err := s.repo.PolicyEventTrim(ctx, now.Add(-PolicyEventAgeCap))
	if err != nil {
		return 0, fmt.Errorf("policy: trim policy events (cap %v): %w", PolicyEventAgeCap, err)
	}
	return n, nil
}

// EventReport is the visibility half of the cap: what the audit table looks
// like from the outside, as one answer.
//
//   - Depth: rows currently retained FOR THE LISTING THIS REPORT PRICES
//     (F-13). The depth is the listing's, because a footer printed beside a
//     listing that reports somebody else's count is the audit answer that lies:
//     `aeon policy events --subject agent:nobody` printing `depth 1` about
//     another subject's refusals was the gap the slice DEMO wrote down as F-14.
//     The unfiltered call (subject "") still means the WHOLE log — that is the
//     number D15's "a loop that retries a denied tool twenty times a round"
//     argument needs, and it is still what an unfiltered listing prints. So the
//     rule is not "depth is the table" and not "depth is the page": it is
//     **depth is what you asked about**, which is the only meaning that lets
//     one word serve both listings without one of them lying.
//   - Cap: always PolicyEventAgeCap. Not "the store's opinion", not a copy:
//     the const itself, so the CLI line "rows · cap 90 days · oldest …" (D12's
//     sibling D15, printed by T12) can never disagree with what TrimEvents
//     enforces. A type with no other possible value is how a report stops
//     lying.
//   - Oldest: the instant of the oldest retained row AMONG THE ROWS THE LISTING
//     SHOWS (same rule as Depth). The zero time means the answer is EMPTY — the
//     whole table when no subject was given, this subject's slice when one was
//     (a real answer, distinguishable from failure, which is an error/refusal,
//     never a silent zero: P31's honesty applies to the audit too).
type EventReport struct {
	Depth  int
	Cap    time.Duration
	Oldest time.Time
}

// ReportEvents answers D15 — depth, cap, oldest — and lists the recent rows
// beside them, newest first: the read side `aeon policy events --depth` (T12)
// and `policy.events` (T13) print without re-deriving anything.
//
// **The depth, the cap and the oldest describe the rows this call returns**
// (F-13): subject "" answers about the whole log, subject `kind:id` answers
// about that subject's rows — and the listing above the footer is the same
// set. One answer, two facts; there is no second table behind the footer.
//
// subject selects whose rows come back (the shape parseSubject owns; "" is
// every subject — the D15 line is about the log, and an unfiltered listing is
// how the log gets browsed). limit must be positive: a zero-limit listing
// reads as "no refusals happened", which is the audit lie this method exists
// to prevent; both sides fail fast on it.
//
// Every row that comes back is re-read through decodeEvent — the SEC-015
// discipline of AcceptancesFor applied to the audit: a row whose vocabulary
// escaped the record boundary (a hand-edited table, a row predating it)
// REFUSES the whole answer loudly with its id named. Silently dropping a row
// is how an audit loses the one refusal somebody came looking for — and a
// report that quietly drops rows while reporting a depth computed over them
// would be reporting two different tables at once.
func (s *Store) ReportEvents(ctx context.Context, subject string, limit int) (EventReport, []EventRow, *Reject) {
	if limit <= 0 {
		return EventReport{}, nil, rejectf("refused: an events listing with limit %d is not a listing; pass a "+
			"positive depth — a zero-limit read reads as \"no refusals happened\" and that is an audit answer "+
			"nobody asked for", limit)
	}
	if subject != "" {
		if _, _, rej := parseSubject(subject); rej != nil {
			return EventReport{}, nil, rej
		}
	}

	// The depth is read for THE SAME subject the listing below is read for
	// (F-13): the footer and the rows are one answer about one set of rows.
	depth, oldest, err := s.repo.PolicyEventStats(ctx, subject)
	if err != nil {
		return EventReport{}, nil, &Reject{Reason: fmt.Sprintf(
			"refused: the policy events report was NOT read — the store failed on the depth/oldest read: %v", err)}
	}
	rows, err := s.repo.PolicyEventQuery(ctx, subject, limit)
	if err != nil {
		return EventReport{}, nil, &Reject{Reason: fmt.Sprintf(
			"refused: the policy events listing was NOT read — the store failed on the row read: %v", err)}
	}

	out := make([]EventRow, 0, len(rows))
	for _, r := range rows {
		e, rej := decodeEvent(r)
		if rej != nil {
			return EventReport{}, nil, rej
		}
		out = append(out, e)
	}
	return EventReport{Depth: depth, Cap: PolicyEventAgeCap, Oldest: oldest}, out, nil
}

// decodeEvent is the read-side half of the event serialization, twin of
// decodeAcceptance: the ONE function that rebuilds an audit row for a surface,
// re-running every value through the gate that owns it — the outcome through
// the closed kind set, the subject through parseSubject's rules (kind from the
// two renderings, id through the identifier policy), the policy id and detail
// through the recorded-text gate, the detail AGAIN against
// MaxEventDetailRunes (the cap is enforced on write; a row over it did not
// come through the boundary, and the read says so instead of rendering the
// overflow), and the instant through the RFC3339Nano parser (a "when" that
// cannot be read is a row that lies).
//
// The values themselves are returned as stored, byte for byte: a decode that
// "fixes" a row is a record reader inventing what it thinks the row meant.
// Any refusal names the offending row's id — an audit that drops rows silently
// is an audit with a hole shaped like plausible innocence.
func decodeEvent(r EventRow) (EventRow, *Reject) {
	if r.ID <= 0 {
		return EventRow{}, rejectf("refused: a stored policy event carries id %d; rowids start at 1 and an "+
			"event without an identity cannot be named in the refusal it belongs to", r.ID)
	}
	if !validEventKind(r.Outcome) {
		return EventRow{}, rejectf("refused: stored policy event %d carries outcome %q, which is not a kind "+
			"the engine records; that row did not come through the record boundary (or the table was edited "+
			"by hand), and refusing the whole report is the fail-closed answer", r.ID, r.Outcome)
	}
	if r.SubjectKind != subjectKindAgent && r.SubjectKind != subjectKindLoop {
		return EventRow{}, rejectf("refused: stored policy event %d names subject kind %q; the only subject "+
			"kinds are %q and %q — the renderings Request.Subject() produces",
			r.ID, r.SubjectKind, subjectKindAgent, subjectKindLoop)
	}
	if rej := checkIdentifier("subject id", r.SubjectID); rej != nil {
		return EventRow{}, rejectf("refused: stored policy event %d carries %s; that row did not come "+
			"through the record boundary (or the table was edited by hand). Refusing the whole report is the "+
			"fail-closed answer: silently dropping a row is how an audit loses the refusal somebody came "+
			"looking for", r.ID, rej.Reason)
	}
	if r.PolicyID != "" {
		if rej := checkRecordedText("policy id", r.PolicyID); rej != nil {
			return EventRow{}, rejectf("refused: stored policy event %d carries %s; that row did not come "+
				"through the record boundary (or the table was edited by hand)", r.ID, rej.Reason)
		}
	}
	if rej := checkRecordedText("detail", r.Detail); rej != nil {
		return EventRow{}, rejectf("refused: stored policy event %d carries %s; the detail is what the row "+
			"says happened, and one that did not come through the record boundary is refused, not rendered",
			r.ID, rej.Reason)
	}
	if n := utf8.RuneCountInString(r.Detail); n > MaxEventDetailRunes {
		return EventRow{}, rejectf("refused: stored policy event %d carries a detail %d runes long; the cap "+
			"is %d and it is enforced on write — a longer row did not come through the record boundary "+
			"(or the table was edited by hand)", r.ID, n, MaxEventDetailRunes)
	}
	if _, err := time.Parse(time.RFC3339Nano, r.At); err != nil {
		return EventRow{}, rejectf("refused: stored policy event %d carries at %q, which does not read as an "+
			"RFC3339Nano instant; a \"when\" whose when cannot be read is a record that lies, so it is "+
			"refused rather than shown (%v)", r.ID, r.At, err)
	}
	return r, nil
}
