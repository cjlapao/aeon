package policy

// ════════════════════════════════════════════════════════════════════════════
// The single serialization point for policy records.
//
// # The contract this file header is (T7 — acceptance; T8 — events; T10 —
// disagreement)
//
// This file is THE one place policy records are written and read back, and it
// is shared by three tasks, in exactly this order: acceptance (T7, this half),
// events (T8), the disagreement record (T10). Those tasks are strictly
// sequential and never touch this file concurrently, and the edits are
// ADDITIVE ONLY: each arriving task appends its own section below the existing
// ones and rewrites nobody's. A record type rewritten by the next arrival is a
// record type whose already-passing tests the next arrival also silently owns
// — and the acceptance record below is load-bearing for enforcement's security
// argument (read on).
//
// # The one rule that makes this file safe to share: acceptance is not a gate
//
// Acceptance answers a real question — *"which policies is this target under,
// and since when?"* — and it is **one `if` away from being a security hole**
// (the seed's argument, kept here where the next editor will read it):
//
//	**An unaccepted policy is still enforced** (PL10/D13).
//
// If enforcement ever consulted acceptance, a plugin could evade policy by
// declining to accept it — refusal-to-know would become refusal-to-be-bound,
// and the escape route would be one well-meaning `if !accepted { ... }` in
// Evaluate. So the guard is structural, not behavioural: Evaluate takes
// ([]Policy, Request, time.Time) — not this Store, not a Repo, not a row
// count — and `evaluate.go` and `types.go` contain no occurrence of the word
// "acceptance" at all, asserted by TestEvaluateDoesNotReadPolicyAcceptance.
// The next person to touch record.go will not have read the seed; the grep is
// what makes "of course it doesn't" a checked fact.
//
// What acceptance DOES buy: the "since when" answer, and — paired with T10's
// disagreement record — the difference between *"it was not told"* and *"it
// was told and tried anyway."* The row is recorded at the moment a target is
// told; it never authorizes, never filters, never softens a verdict.
//
// # Why ONE file, and why "row-building" is a discipline, not a vibe
//
// Records serialize: a domain value goes in, a row comes out, and a later
// reader must be able to rebuild the meaning from the row alone. Every such
// serialization wants exactly two functions — the **handover** (the one place
// that assembles what crosses the Repo seam from the domain values) and the
// **decode** (the one place that rebuilds the domain record from the stored
// row, re-running every value through the parsers). Acceptance's pair is the
// handover block in RecordAcceptance and decodeAcceptance below. No formatting
// or row-assembly lives anywhere else in this file. That is not tidiness: T8
// arrives with the strictest rule of the slice — `detail` is written by ONE
// function, rendered only from verdict.Sentence() or a fixed reason constant,
// capped by MaxEventDetailRunes on the way in — and a file whose earlier task
// scattered formatting across helpers is a file where T8's single function is
// a refactor instead of an addition. The shape stays one-function-per-record-
// kind, so each later task adds without moving anybody's text.
//
// # What this file does NOT do
//
//   - No SQL (I2): the statements live in internal/store/policy.go; this file
//     imports neither database/sql nor internal/store — the Repo seam is the
//     boundary, RepoFuncs is the wiring's bridge (see store.go).
//   - No vocabulary invention: the target arrives as a Target and is rebuilt
//     through ParseTarget (the same closed-set discipline Add re-runs on
//     whatever it is handed — SEC-015/SEC-007: a check downstream is a check
//     with a bypass, and a hand-cast Target{Kind: "global"} is exactly what
//     walks in).
//   - No existence check: "does the policy exist" is the database's foreign
//     key to answer (ON DELETE CASCADE keeps it true); duplicating it here
//     would be a second authority that can disagree with the first.
//   - No clock reads (D5): every instant arrives as a parameter.
// ════════════════════════════════════════════════════════════════════════════

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Acceptance is one target's record of having been told about one policy, and
// when it was told.
//
// The fields are identifiers and one timestamp — the five columns the table
// declares, rendered through the domain (legal constraint 2: this record has
// no free-text field, so content and secrets have nowhere to be carried and
// nothing to hide in). Target is a parsed Target, not two strings: a record
// whose kind escaped the closed set is refused by decodeAcceptance rather than
// handed to a surface that would render nonsense as fact.
//
// AcceptedAt is the RFC3339Nano rendering of the FIRST record instant, byte
// for byte what the store holds: "since when" answers with the instant the
// first record was made, and a repeated accept moves nothing (PL10's quieter
// half — the idempotence that keeps the audit honest).
//
// It is a value, not an error and not a permission: nothing in the enforcement
// path constructs, reads or consults one.
type Acceptance struct {
	PolicyID   string
	Target     Target
	AcceptedBy string
	AcceptedAt string
}

// RecordAcceptance writes that target was told about that policy at that
// instant — once, idempotently, and with no power over enforcement.
//
// It is the handover for the acceptance record: THE one place this file
// assembles what crosses the Repo seam for `policy_acceptance` (T8's detail
// serialization will be its own single function, and none of this moves).
// The order of checks is Add's discipline applied to the record's fields:
//
//  1. the target, rebuilt through ParseTarget — the caller's Target is a
//     struct, and Go structs carry no guarantees (a casted kind, an id that
//     repeats its kind, a named kind naming nothing: all refused, by name);
//  2. the identifier policy on the three third-party strings — policy id,
//     target id (for named kinds) and accepted_by. They are gated because
//     they are third-party text that reaches an operator's console through
//     every listing of "who knew what since when" (SEC-013); accepted_by is
//     validated as a BARE name, the same rule Target.ID carries — the kind:id
//     rendering belongs to Target, and a second field spelling identities
//     with its own separators is a second identity language nobody audits;
//  3. the instant: a record of "since when" with no instant is the row that
//     answers nothing, and D5 says the clock arrives as a parameter, so a
//     zero `at` is a missing field, not the year 1.
//
// What crosses the seam is exactly those validated values plus nothing else;
// the store layer renders the instant and owns the SQL. Re-recording is not
// an error and does not move the first instant (ON CONFLICT DO NOTHING,
// documented at store.PolicyAcceptanceRecord) — so calling this again after a
// crash, a retry, or a confused loop costs nothing and lies about nothing.
//
// A store failure (disk, foreign-key clash: acceptance of a policy that does
// not exist is a record of nothing) arrives as a *Reject naming the store and
// saying the record was NOT made — Add's single-return-shape discipline, so
// callers of the boundary have exactly one thing to handle and one answer
// ("tell the human; nothing here was recorded").
func (s *Store) RecordAcceptance(ctx context.Context, policyID string, target Target, acceptedBy string, at time.Time) *Reject {
	// 1. The target, through the one parser that can make one.
	t, rej := ParseTarget(target.Kind, target.ID)
	if rej != nil {
		return rej
	}

	// 2. The identifier policy, naming the field it refused (same gate, same
	//    messages as Add — one policy, enforced at every door that third-party
	//    text walks through).
	if rej := checkIdentifier("policy id", policyID); rej != nil {
		return rej
	}
	if t.Kind != AllAgents {
		if rej := checkIdentifier(fmt.Sprintf("target id for kind %q", string(t.Kind)), t.ID); rej != nil {
			return rej
		}
	}
	if rej := checkIdentifier("accepted by", acceptedBy); rej != nil {
		return rej
	}

	// 3. The instant is a parameter or the record has no "since when".
	if at.IsZero() {
		return rejectf("refused: the acceptance of policy %q by %s has no instant; \"since when\" is the "+
			"question the record answers, and a clock read is not this boundary's to take (D5)",
			policyID, t.String())
	}

	// Handover. Exactly these values cross the seam — the validated target
	// split into its two key columns (all-agents carries the empty target id,
	// which is what the acceptance key needs; ParseTarget guarantees no other
	// shape arrives empty), the caller's strings (unchanged: a gate that
	// silently edits its input is the control that fails closed on nobody),
	// and the caller's instant.
	if err := s.repo.PolicyAcceptanceRecord(ctx, policyID, string(t.Kind), t.ID, acceptedBy, at); err != nil {
		return &Reject{Reason: fmt.Sprintf(
			"refused: the acceptance of policy %q by %s was NOT recorded — the store failed: %v",
			policyID, t.String(), err)}
	}
	return nil
}

// AcceptancesFor reads back the records for one target: "which policies is
// this target under, and since when" — asked of the record table, never of
// the enforcement path, which does not know this question exists (PL10).
//
// The target is validated through ParseTarget before anything is asked of the
// repo (the boundary trusts no caller, read or write — SEC-015), and every
// row that comes back is re-read through decodeAcceptance: a row whose
// vocabulary escaped the closed set — a hand-edited table, a row written
// before this boundary existed — REFUSES the whole answer loudly with its
// policy id named, rather than being silently dropped. Dropping a row silently
// is how an audit quietly loses the one acceptance that proved someone knew.
func (s *Store) AcceptancesFor(ctx context.Context, target Target) ([]Acceptance, *Reject) {
	t, rej := ParseTarget(target.Kind, target.ID)
	if rej != nil {
		return nil, rej
	}
	rows, err := s.repo.PolicyAcceptanceListFor(ctx, string(t.Kind), t.ID)
	if err != nil {
		return nil, &Reject{Reason: fmt.Sprintf(
			"refused: the acceptances for %s were NOT read — the store failed: %v", t.String(), err)}
	}
	out := make([]Acceptance, 0, len(rows))
	for _, r := range rows {
		a, rej := decodeAcceptance(r)
		if rej != nil {
			return nil, rej
		}
		out = append(out, a)
	}
	return out, nil
}

// decodeAcceptance is the read-side half of the acceptance serialization: the
// ONE function that rebuilds a domain record from a stored row. Every value
// goes through the parser that owns it — the target through ParseTarget (so a
// tampered kind refuses by name), the instant through the RFC3339Nano parser
// (a timestamp that cannot be read as an instant is the "since when" that
// lies) — and the strings are returned as stored, byte for byte: the record
// shows what the table holds, unedited, because a decode that "fixes" a row
// is a record reader inventing what it thinks the row meant.
func decodeAcceptance(r AcceptanceRow) (Acceptance, *Reject) {
	if r.PolicyID == "" {
		return Acceptance{}, rejectf("refused: a stored acceptance row carries no policy id; an acceptance " +
			"of nothing is a record of nothing, and that row did not come through the record boundary")
	}
	target, rej := ParseTarget(TargetKind(r.TargetKind), r.TargetID)
	if rej != nil {
		return Acceptance{}, rejectf("refused: stored acceptance of policy %q carries %s; that row did not "+
			"come through the record boundary (or the table was edited by hand). Refusing the whole list is "+
			"the fail-closed answer: silently dropping a row is how an audit loses the one acceptance that "+
			"proved someone knew", r.PolicyID, rej.Reason)
	}
	if r.AcceptedBy == "" {
		return Acceptance{}, rejectf("refused: stored acceptance of policy %q by %s has no accepted_by; "+
			"\"who said so\" is the point of the row, and an empty one did not come through the record "+
			"boundary", r.PolicyID, target.String())
	}
	if _, err := time.Parse(time.RFC3339Nano, r.AcceptedAt); err != nil {
		return Acceptance{}, rejectf("refused: stored acceptance of policy %q by %s carries accepted_at %q, "+
			"which does not read as an RFC3339Nano instant; a \"since when\" whose when cannot be read is a "+
			"record that lies, so it is refused rather than shown (%v)", r.PolicyID, target.String(),
			r.AcceptedAt, err)
	}
	return Acceptance{
		PolicyID:   r.PolicyID,
		Target:     target,
		AcceptedBy: r.AcceptedBy,
		AcceptedAt: r.AcceptedAt,
	}, nil
}

// ════════════════════════════════════════════════════════════════════════════
// The events half (T8): every refusal, with its subject — and the ONE function
// that writes detail.
//
// # The legal constraint this section is (constraint 1, verbatim in structure)
//
// policy_event.detail is written by EXACTLY ONE function: recordEvent below,
// unexported, which is the only caller of Repo.PolicyEventInsert anywhere in
// the tree (grep-pinned by TestPolicyEventInsertHasExactlyOneCaller). No
// surface, no future task, no convenience helper reaches that INSERT around
// this file. detail is rendered ONLY from verdict.go's Sentence() (T4, the one
// formatter) or a fixed reason constant — never from tool arguments, prompt
// text, file contents, env values, Spec bytes or caller free text — and it is
// capped to MaxEventDetailRunes (the named 256, legal constraint 1's number)
// ON WRITE, here, at the one door. A cap enforced at the reader is a cap with
// a bypass; a cap at the single writer is a property of every row.
//
// # The reference's TruncateDetail, and the deliberate adaptation
//
// helper/plai-backend (studied, never transcribed) truncates detail to 256
// runes by KEEPING THE HEAD and appending an ellipsis (pkg/policystate/
// types.go). That was right for its detail — an agent error string whose front
// carries the cause. It is WRONG for Aeon's: Aeon's detail is a state-bearing
// SENTENCE, and the clause that tells a reader what actually happened sits at
// the END of it — "default deny (PL5)", the naming of the allow that was
// beaten ("— denied anyway, PL4"), "refusing rather than permitting (D10)".
// A naive head-cut turns an audit row into a misleading one: the row silently
// stops saying which refusal it was (carried HIGH from the T6 security gate,
// SEC-020's lineage). So the cap stays — 256 runes, named const, enforced on
// write, rune-safe, appending NOTHING — but the budget is split: the head
// gets n/4 runes (enough for "refused: <subject> ·" to survive), the tail
// gets the rest, and the tail is what must survive. The budgets are not
// hope: identifiers are capped at MaxIdentifierRunes=64 by Add (T6), so the
// longest legal PL4 override clause (~160 runes), the D10 tail (41) and the
// whole PL5 line (119) all fit inside the 192-rune tail budget — and a
// hand-built verdict trying to smuggle longer prose loses only its MIDDLE,
// never its final clause, which is the last thing on the tail. Pinned by
// TestTruncationPreservesTheStateBearingTail.
//
// # Trim is never on this path
//
// Nothing below deletes. A refusal must not pay for housekeeping (the seed's
// row-count-vs-age argument: trimming belongs to daemon start — the daemon's
// startup path calls TrimEvents, wiring lands with T13/T12 — and to demand
// via `aeon policy events`; the spy in retention_test.go counts ZERO
// PolicyEventTrim calls across fifty RecordRefusal calls).
//
// # Additive only — T10 reads this
//
// T10 appends the disagreement record HERE, below this section: its own
// handover (RecordDisagreement) calling recordEvent with its own kind, plus
// the extension of validEventKind with the new const. Nobody rewrites the
// acceptance half above or this half; the header contract still governs.
// ════════════════════════════════════════════════════════════════════════════

// MaxEventDetailRunes is the cap on policy_event.detail, counted in RUNES —
// the named const legal constraint 1 demands (mirroring the reference's
// TruncateDetail's 256, adapted to keep the tail as above). It is enforced on
// write by recordEvent/RecordRefusal and re-verified on read by decodeEvent:
// a row over the cap did not come through the boundary, and the audit says so.
//
// The sizing budget is T6's measured sentence: with legal field sizes the
// longest Sentence() was ~480 runes (MaxIdentifierRunes' comment in
// store.go); 256 keeps the state-bearing tail and the "refused:" head and
// drops padding. Widening it is a reviewed change to a security bound.
const MaxEventDetailRunes = 256

// The event kinds — the closed set for policy_event.outcome. "refusal" is
// T8's; "disagreement" arrived with T10 additively and "authored" with S11's
// explicit seeding verb (loop T9), each one the new const + the one clause in
// validEventKind the header contract pre-authorized. An outcome outside this
// set on a stored row means the row did not come through recordEvent, and
// ReportEvents refuses it by name rather than rendering an outcome it cannot
// interpret.
const (
	eventKindRefusal = "refusal"
	// eventKindDisagreement marks a refusal of something the AGENT
	// attempted — the gate said no to a tool the model itself asked for.
	// Telemetry before audit: a rising rate is the cheapest signal that the
	// policy text is no longer being read (the seed's argument for the kind
	// existing at all).
	eventKindDisagreement = "disagreement"
	// eventKindAuthored marks the WRITING of a rule, not a decision about a
	// request: "this rule was stored for this subject by an operator's
	// explicit command, at this instant." It arrived with S11's seed-corpus
	// (loop T9, decision D4 — an audited authoring). It is not a refusal's
	// kind in disguise: an audit row claiming a refusal that never happened
	// is the row that lies, which is exactly what RecordRefusal's first
	// check exists to refuse.
	eventKindAuthored = "authored"
)

// validEventKind is the ONE vocabulary check for the outcome column.
func validEventKind(kind string) bool {
	return kind == eventKindRefusal || kind == eventKindDisagreement || kind == eventKindAuthored
}

// The subject kinds — exactly the two renderings Request.Subject() produces
// ("loop:<slug>" inside a loop, "agent:<name>" otherwise; evaluate.go's
// Subject is the one subject renderer, and inventing a third kind here would
// be a second subject language nobody audits — the same rule that keeps the
// accepted_by spelling out its own separator nowhere).
const (
	subjectKindAgent = "agent"
	subjectKindLoop  = "loop"
)

// RecordRefusal records that this subject was refused by this verdict at this
// instant — the audit trail's one write path: "every time a policy refused
// something, with its subject."
//
// It is the handover for the event record, RecordAcceptance's twin for the
// policy_event table (and, like that half, the order of checks is the
// argument):
//
//  1. an Allowed verdict refuses to be recorded — a row whose outcome says
//     "refusal" while the verdict says the thing was permitted is the audit
//     row that lies, and lying is the one thing an audit may never do;
//  2. the subject must be the subject the verdict renders its sentence about
//     (subject == v.Subject): the columns say who, the detail quotes the
//     sentence about who — a record binding X's refusal to Y's name is a
//     forgery, however well-meant;
//  3. detail is rendered by recordableDetail — from the verdict's own
//     rendered fields, no I/O, never Spec bytes — and capped by
//     truncateRunes(…, MaxEventDetailRunes) HERE, on the way into the one
//     writer;
//  4. recordEvent re-checks everything that crosses the seam (kind
//     vocabulary, subject shape, text safety, the instant — D5, the clock is
//     a parameter) and is the sole caller of the store's INSERT.
//
// A store failure (disk, the FK refusing an event that names a policy nobody
// stored) arrives as a *Reject saying the record was NOT made — Add's and
// RecordAcceptance's single-return-shape discipline (DB19's inherited
// exception), so the caller has one thing to handle and one answer: tell the
// human, the audit is missing this row, and nothing here half-recorded it.
//
// Enforcement callers (T9's gate) reach this boundary through the injected
// seam, never the store: the runtime imports no internal/store (K22), and the
// record is written where the verdict is rendered, by the one file that owns
// both records.
func (s *Store) RecordRefusal(ctx context.Context, subject string, v Verdict, at time.Time) *Reject {
	if v.Allowed {
		return rejectf("refused: the verdict for %s is an ALLOW — recording it as a refusal would write an "+
			"audit row that contradicts its own verdict; only refusals belong in policy_event (the outcome "+
			"column is written %q)", quotePreview(subject), eventKindRefusal)
	}
	if subject != v.Subject {
		return rejectf("refused: the refusal for subject %q does not match the subject the verdict renders "+
			"about (%q) — the columns say who, the detail quotes a sentence about who, and binding one to the "+
			"other is a forgery an audit cannot survive", subject, v.Subject)
	}
	detail := recordableDetail(v)
	return s.recordEvent(ctx, eventKindRefusal, subject, policyIDOf(v), truncateRunes(detail, MaxEventDetailRunes), at)
}

// recordEvent is THE only writer to policy_event: unexported, so no caller
// outside this file can reach Repo.PolicyEventInsert (the seam is the
// boundary; the INSERT has exactly one caller — this function — grep-pinned
// by TestPolicyEventInsertHasExactlyOneCaller). Every check a stored row must
// satisfy lives HERE or nowhere: kind vocabulary, subject shape, text safety,
// the instant. What crosses the seam after that is exactly these validated
// values; the store layer renders the instant and owns the SQL.
//
// The policyID may be "": the default deny (PL5) and the unimplemented
// refusal (D10) decide without naming a rule, and the column's NULL means
// "no rule to name" — the store renders it via its NULL rule. A NON-empty
// policyID is text that will be quoted in every listing of this table, so it
// passes the recorded-text gate below (no control bytes, no invisibles); its
// LENGTH is not re-gated here — Add's identifier policy already refused
// over-long ids at creation and the table's FOREIGN KEY is the database's own
// existence check. A record boundary that refused to record a refusal
// because the named rule's name was too long would be a boundary choosing
// which refusals deserve an audit row, and that is not a judgement it owns.
//
// The detail may not be empty (what happened is the point of the row — the
// store fail-fasts on the blank too; belt and braces name it at the door
// closer to the mistake) and passes the same recorded-text gate: an audit row
// with a newline or a terminal control byte in its detail is SEC-013 walking
// back in through the log — describeRune/quotePreview name the offender
// without echoing it.
func (s *Store) recordEvent(ctx context.Context, kind, subject, policyID, detail string, at time.Time) *Reject {
	// 1. The outcome vocabulary — the one check for the column, in one place.
	if !validEventKind(kind) {
		return rejectf("refused: %q is not a policy event the engine records; the kinds are %q, %q and %q — "+
			"an outcome outside the closed set is an audit entry nobody can interpret, so it is refused "+
			"by name", kind, eventKindRefusal, eventKindDisagreement, eventKindAuthored)
	}

	// 2. The subject, rebuilt through the split that owns it — the same
	//    closed-set discipline Add and RecordAcceptance run on whatever they
	//    are handed (SEC-015/SEC-007: a check downstream is a check with a
	//    bypass, and a hand-cast "whoever" is exactly what walks in).
	if _, _, rej := parseSubject(subject); rej != nil {
		return rej
	}

	// 3. The recorded-text gates, each naming the field it refused.
	if policyID != "" {
		if rej := checkRecordedText("policy id", policyID); rej != nil {
			return rej
		}
	}
	if detail == "" {
		return rejectf("refused: the %s event for %s has no detail; the detail is what the row says happened, "+
			"and the sentence is rendered by verdict.go and cannot come back empty", kind, subject)
	}
	if rej := checkRecordedText("detail", detail); rej != nil {
		return rej
	}
	// 3b. The cap is RE-CHECKED here, by the sole writer itself (T8 review,
	//     DEF-029/SEC-032). RecordRefusal truncates on its way in, and a
	//     cap enforced only by one of two callers is a cap with a bypass:
	//     the sibling handover (T10's RecordDisagreement) — or any future
	//     one — that forgot truncateRunes would write an over-cap row this
	//     file's own comment promises cannot exist. decodeEvent re-verifies
	//     the bound on READ; a bound enforced only at the reader is a bound
	//     that certifies garbage before it refuses it. So every row crossing
	//     this seam passes the named const, whoever rendered it.
	if utf8.RuneCountInString(detail) > MaxEventDetailRunes {
		return rejectf("refused: the %s event for %s carries a detail of %d runes, over the named cap of %d; "+
			"the cap is a property of every row, enforced at the single door — truncateRunes before you "+
			"call in", kind, subject, utf8.RuneCountInString(detail), MaxEventDetailRunes)
	}

	// 4. The instant is a parameter or the row is the record that answers
	//    nothing, and D5 says no clock is this boundary's to read.
	if at.IsZero() {
		return rejectf("refused: the %s event for %s has no instant; \"when was it refused\" is the question "+
			"the audit answers, and a clock read is not this boundary's to take (D5)", kind, subject)
	}

	// Handover. Exactly these values cross the seam; nothing is reformatted,
	// nothing is trimmed, the caller's instant travels whole.
	if err := s.repo.PolicyEventInsert(ctx, kind, subject, policyID, detail, at); err != nil {
		return &Reject{Reason: fmt.Sprintf(
			"refused: the %s against %s was NOT recorded — the store failed: %v", kind, subject, err)}
	}
	return nil
}

// parseSubject is the one subject parser in this file: it splits the
// `kind:id` that Request.Subject() renders and re-runs every half through the
// gates that own it — the kind against the two closed renderings (agent, loop)
// and the id through Add's own identifier policy (the alphabet, the cap, the
// control-byte and zero-width refusals, all naming the value; SEC-013's
// discipline, since a subject id is third-party text reaching every audit
// listing). A subject that cannot be split is refused, never stored half.
func parseSubject(subject string) (kind, id string, rej *Reject) {
	k, rest, ok := strings.Cut(subject, ":")
	if !ok {
		return "", "", rejectf("refused: subject %s has no \":\" — a subject is kind:id, and the shapes are "+
			"%q and %q as Request.Subject() renders them; the audit columns are subject_kind and subject_id "+
			"and a half-subject audits nobody", quotePreview(subject), subjectKindLoop+":<slug>", subjectKindAgent+":<name>")
	}
	if k != subjectKindAgent && k != subjectKindLoop {
		return "", "", rejectf("refused: subject %s names kind %q; the only subject kinds are %q and %q — the "+
			"two renderings Request.Subject() produces, and nothing else", quotePreview(subject), k, subjectKindAgent, subjectKindLoop)
	}
	if rej := checkIdentifier("subject id", rest); rej != nil {
		return "", "", rej
	}
	return k, rest, nil
}

// checkRecordedText is the gate on strings the record table stores that are
// NOT bounded identifiers: the detail sentence and a recorded policy id.
// Length is not its business (that belongs to Add and the DB's own FK); what
// is not negotiable is that what reaches an operator's terminal is VISIBLE,
// NON-BLANK text free of control bytes and invisibles — a newline, a
// terminal-escape or a zero-width hidden in an audit row is SEC-013/SEC-020
// arriving through the log. It refuses (naming the offender, never echoing
// it); it never edits.
func checkRecordedText(field, value string) *Reject {
	if value == "" {
		return rejectf("refused: %s is empty; a blank in the audit row reads as silence about the thing "+
			"the row exists to record", field)
	}
	if visibleRuneCount(value) == 0 {
		return rejectf("refused: %s %s holds no visible character — blank to a human, non-empty to TrimSpace "+
			"(the zero-width masquerade, SEC-020); an audit row nobody can read is an audit row that is not there",
			field, quotePreview(value))
	}
	pos := 0
	for _, r := range value {
		if r >= 0x20 && r != 0x7f && !(r > 0x7f && r <= 0x9f) {
			if _, hidden := zeroWidth[r]; !hidden {
				pos++
				continue
			}
		}
		return rejectf("refused: %s %s carries %s at position %d; recorded text reaches an operator's "+
			"console through every audit listing, and terminal-active bytes are refused, not escaped away (SEC-013)",
			field, quotePreview(value), describeRune(r), pos)
	}
	return nil
}

// policyIDOf answers "which rule refused" for the policy_id column: the id of
// the policy that decided, or "" — the column's NULL — when the verdict names
// no rule at all. That is not a gap in the record; it is the record's honest
// answer for the two refusals that decide without a policy: the default deny
// (PL5) and the unimplemented refusal (D10). (By.Type never enters this: the
// column names the rule, not the request.)
func policyIDOf(v Verdict) string {
	if v.By == nil {
		return ""
	}
	return v.By.ID
}

// recordableDetail renders what policy_event.detail will hold — pure
// composition of the verdict's RENDERED fields, no I/O, no clock, and never a
// byte of any Spec. It is the function legal constraint 1 points at.
//
// Its one source is Verdict.Sentence(): the one formatter (verdict.go, T4)
// renders ALL five refusal shapes, INCLUDING the DefaultDeny line ("… no
// allow matched · default deny (PL5)") and the Unimplemented line ("… has no
// evaluator yet · refusing rather than permitting (D10)") with the state-
// bearing clauses the truncation is duty-bound to keep. The spec also names
// Reason — the fixed-string vocabulary Evaluate sets on the unimplemented
// paths ("no evaluator for %q yet") — as an admissible source on those two
// shapes; it is deliberately NOT used, because the sentence is strictly more
// informative than the fixed string (it carries the subject, the type AND the
// (D10) marker the state-bearing tail must keep), and a detail weaker than
// what T4 renders would be a second, worse formatter for the same verdict.
// No other source is admissible: no Spec bytes (the no-content/no-secret test
// proves it), no caller free text, nothing from the request beyond what
// Sentence already renders.
func recordableDetail(v Verdict) string {
	return v.Sentence()
}

// truncateRunes caps s at n runes — rune-safe (the slice is over []rune; a
// multi-byte rune is never split into a broken byte, the reference's own
// guarantee) — appending NOTHING (the cap is a cap, not an ellipsis: an
// appended marker is one rune of nobody's vocabulary entering every long row).
//
// The shape is the carried HIGH from the T6 security gate: KEEPING THE TAIL.
// A naive s[:n] head-cut silently drops the clause that tells a reader what
// actually happened, because in Sentence() the state-bearing clause is the
// LAST segment — "default deny (PL5)", "— denied anyway, PL4" with the
// overriding allow named inside it, "(D10)". The budget is n/4 for the head
// (enough to keep "refused: " plus the subject's front — what kind of row this
// is) and the remainder for the tail (enough to keep the entire final clause
// for every legal field size: MaxIdentifierRunes=64 bounds the PL4 override
// clause under ~160, and the fixed D10/PL5 tails are 41 — measured, and
// pinned by TestTruncationPreservesTheStateBearingTail). A hand-built verdict
// with prose longer than the tail budget still keeps its final clause: the
// cut eats from the middle, and the tail is cut from its front only as a last
// resort, which for the fixed state-bearing endings is unreachable.
//
// n <= 0 is a caller defect (no one passes it — RecordRefusal passes the
// named const), answered with the empty string rather than a panic: the audit
// keeps working when its cap is mis-wired, and refuses to invent content.
func truncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	head := n / 4
	return string(r[:head]) + string(r[len(r)-(n-head):])
}

// ════════════════════════════════════════════════════════════════════════════
// The disagreement record (T10).
//
// A refusal answers "what did the engine say no to". A disagreement answers
// the sharper question beside it: "did the AGENT try anyway?" — the gate
// refused a tool the model itself emitted, after the manifest's `## Policy`
// section (or its absence) had told it where the line was. As telemetry the
// two are different instruments: refusals track the rules, disagreement rate
// tracks whether the instruction is being read at all. That is why the kind
// is distinct and why this half is its own handover rather than a parameter
// on RecordRefusal: a caller cannot forget to mark an attempt as one when
// the only way to record it is to say so.
//
// # The columns it fills, and the two that gate it
//
// policy_event has five columns and legal constraint 2 froze them; this
// record fills exactly the ones refusal fills — outcome ("disagreement"),
// subject (split to kind and id), the policy that fired, the rendered
// sentence, the instant. stepID and agent are NOT columns, and adding one
// would be adding a free-text column to the table every surface renders —
// the schema_test pin and the migration count (16) both say that is not a
// call this task owns. They are instead the two facts that must be TRUE for
// the row to be written at all:
//
//   - stepID > 0: an attempt the record cannot attribute to a step is not an
//     attempt anyone can charge an agent with. The attribution is verifiable
//     from outside the row (the step's transcript carries the tool_use the
//     gate refused, its receipts carry the absence of execution) precisely
//     because nobody had to widen the table to say it.
//   - agent: the name that attempted it. When the subject IS the agent
//     ("agent:<name>"), it must match the subject's own id — binding one
//     agent's attempt to another agent's name is the forgery RecordRefusal's
//     subject==v.Subject check refuses for the same reason. The identifier
//     policy that owns names owns this one too.
//
// # The same discipline as refusal, because it is the same door
//
// detail comes from the same renderers — recordableDetail, verdict.go's
// Sentence(), the one formatter — capped by truncateRunes on the way in and
// re-checked by recordEvent on the way through (DEF-029: the sole writer
// enforces its own bound, whoever called). No free text, no Spec bytes, no
// step prose, nothing a caller can steer. The row differs from a refusal's
// by exactly one column, the kind, which is the whole point: "refusal" and
// "disagreement" must be distinguishable by the outcome value alone.
// ════════════════════════════════════════════════════════════════════════════

// RecordDisagreement records that the agent for this subject ATTEMPTED
// something this verdict refused — the gate's "no" to a model-emitted tool
// request, audited under its own kind. It is RecordRefusal's twin for the
// attempt side of PL9, and it shares the one door: recordEvent.
//
// The order of checks is the argument (same as RecordRefusal's, plus the two
// attribution gates the attempt adds):
//
//  1. an Allowed verdict refuses to be recorded — a "disagreement" row whose
//     verdict permits the thing is an audit row accusing an agent of an
//     attempt nobody refused;
//  2. subject == v.Subject — the detail quotes a sentence about a subject,
//     and binding the row to anyone else is a forgery;
//  3. stepID > 0 — an unattributable attempt is not a chargeable one;
//  4. agent passes the identifier policy that owns names, and when the
//     subject is agent-kind it must BE that agent (no borrowed blame);
//  5. recordEvent re-checks everything else (vocabulary, subject shape, text
//     safety, the cap, the instant — D5's clock is a parameter) and is the
//     sole caller of the store's INSERT.
//
// A store failure arrives as a *Reject saying the record was NOT made — the
// same single-return-shape as RecordRefusal (DB19's inherited exception):
// tell the human the audit is missing this row; nothing half-recorded it, and
// the DENIAL itself never waits on this record (enforcement is not audited
// by the audit).
func (s *Store) RecordDisagreement(ctx context.Context, subject string, stepID int64, agent string, v Verdict, at time.Time) *Reject {
	if v.Allowed {
		return rejectf("refused: the verdict for %s is an ALLOW — recording it as a disagreement would "+
			"accuse an agent of attempting something nobody refused; only refusals belong in policy_event "+
			"(the outcome column is written %q)", quotePreview(subject), eventKindDisagreement)
	}
	if subject != v.Subject {
		return rejectf("refused: the disagreement for subject %q does not match the subject the verdict "+
			"renders about (%q) — the columns say who, the detail quotes a sentence about who, and binding "+
			"one to the other is a forgery an audit cannot survive", subject, v.Subject)
	}
	if stepID <= 0 {
		return rejectf("refused: the disagreement against %s names no step (stepID %d) — an attempt that "+
			"cannot be attributed to a step is not an attempt an agent can be charged with, and a row that "+
			"records an unchargeable attempt is telemetry about nobody", quotePreview(subject), stepID)
	}
	if rej := checkIdentifier("agent", agent); rej != nil {
		return rej
	}
	if kind, id, rej := parseSubject(subject); rej != nil {
		return rej
	} else if kind == subjectKindAgent && id != agent {
		return rejectf("refused: the disagreement is filed against %s but names agent %q — an attempt under "+
			"one agent's name bound to another's subject is borrowed blame, and the audit does not traffic in it",
			subject, agent)
	}
	// Handover: the SAME renderer RecordRefusal uses, capped on the way in as
	// it is, with the kind — the one column that differs — naming this a row
	// about an attempt.
	return s.recordEvent(ctx, eventKindDisagreement, subject, policyIDOf(v),
		truncateRunes(recordableDetail(v), MaxEventDetailRunes), at)
}

// ════════════════════════════════════════════════════════════════════════════
// The authored record (S11 loop T9 — the explicit, audited seeding verb).
//
// # Why an authoring needs its own kind
//
// A refusal answers "what did the engine say no to"; a disagreement answers
// "did the agent try anyway". Neither is the question an authoring asks:
// "which rule entered this table, for whom, when, and under whose hand?"
// D4 — the loop's binding decision on seeding — demands the authoring be
// AUDITED, and the only honest way to audit it is a row that says exactly
// what happened. Recording a seeded rule under eventKindRefusal would write
// an audit row claiming a refusal nobody ever asked about — the hypothetical
// `why` structurally refuses to record, and RecordRefusal's Allowed check is
// the same lie caught at the door. So the authoring gets its own kind,
// arriving the way T10's did: the new const + the one clause in
// validEventKind, additive, the one door (recordEvent) unchanged.
//
// # What fills the five columns
//
// outcome "authored"; subject the rule's target rendered the way Request
// renders subjects ("agent:<name>"); policy_id the rule Add just stored
// (the FOREIGN KEY is the database's existence check — an authored row for a
// rule nobody stored is refused there); detail a FIXED constant — legal
// constraint 1 still binds here: detail comes from Sentence() or a fixed
// reason constant, never Spec bytes, never caller free text. The identity of
// what was authored rides the COLUMNS (policy_id names the rule, the subject
// names who it governs), so no pattern text, spec JSON or operator prose
// ever needs to pass through this row to make it meaningful; at the instant,
// as always a parameter (D5 — the boundary reads no clock).
//
// # What this handover does NOT do
//
// It is not a second Add: the row must already have cleared the write
// boundary (Add's closed sets, PL1 and the per-type validator) before an
// authored record makes sense, and this function stores no policy. It never
// trims (PL11 — a record never pays for housekeeping). And `aeon policy add`
// does not call it: an operator authoring one row at the prompt is already
// the most witnessed writer there is; this kind exists for the bulk authoring
// that a daemon-start loader would have done SILENTLY, which is precisely the
// door D4 welded shut.
// ════════════════════════════════════════════════════════════════════════════

// authoredDetail is policy_event.detail for every authored row: a fixed
// constant (legal constraint 1's second admissible source — no Spec bytes,
// no caller text, nothing steerable). It states what the row is and where
// the identity lives, and it ends on the marker that makes it legible beside
// refusals in an unfiltered listing. The rule and its subject are named by
// the columns, not by this sentence.
const authoredDetail = "authored: the write boundary stored this rule for this subject; " +
	"the policy_id and subject columns name what was authored (PL15 — authoring is CLI-only)"

// RecordAuthored records that the boundary stored this rule for this subject
// at this instant — the audit trail of an authoring, RecordRefusal's twin on
// the writing side. It is the handover for the authored record: THE one
// place this file assembles an authored row, and it calls the same sole
// writer (recordEvent) every other kind passes through.
//
// The order of checks is RecordRefusal's discipline with the verdict checks
// replaced by the two the authoring adds:
//
//  1. policyID must be non-empty — an authored record of no rule is a record
//     of nothing (the FK upstairs refuses the row whose rule does not exist;
//     naming that here beats a raw constraint failure);
//  2. the instant: a record of "when was it authored" with no instant is the
//     row that answers nothing, and D5 says the clock arrives as a parameter;
//  3. recordEvent re-checks everything that crosses the seam (kind
//     vocabulary, subject shape, text safety, the cap, the instant) and is
//     the sole caller of the store's INSERT.
//
// A store failure arrives as a *Reject saying the record was NOT made — the
// same single-return-shape as RecordRefusal's (DB19's inherited exception):
// tell the human the audit is missing this row; nothing half-recorded it.
// The CALLER's rule, stated here so the pairing is never a mystery: author
// through Add FIRST and record SECOND — a stored rule stands whether or not
// its audit row lands (enforcement never waits on the audit), and a refusal
// from this function says precisely which half happened.
func (s *Store) RecordAuthored(ctx context.Context, subject, policyID string, at time.Time) *Reject {
	if policyID == "" {
		return rejectf("refused: the authored record for %s names no rule; an authored row about nothing "+
			"is the audit row that lies, and the policy_id column is where the rule is named — author through "+
			"the boundary's Add first, then record what it stored", quotePreview(subject))
	}
	if at.IsZero() {
		return rejectf("refused: the authored record for %s has no instant; \"when was it authored\" is the "+
			"question the audit answers, and a clock read is not this boundary's to take (D5)",
			quotePreview(subject))
	}
	return s.recordEvent(ctx, eventKindAuthored, subject, policyID, authoredDetail, at)
}

// RecordAuthoredInTx is RecordAuthored's twin on the CALLER's transaction
// (S45 T12, PM8): the seed applier writes a corpus row and its authored
// record on the engine's one transaction, so the row and its audit row
// commit together or vanish together — a seeded rule with no authored event
// is exactly the un-witnessed bulk authoring this kind exists to prevent,
// and on a shared transaction an event that failed must take the row with
// it (the plain door's "the rule stands whether the event lands" is the
// SEQUenced caller's rule; on one transaction, half an authoring is not a
// half-written pair, it is no authoring at all).
//
// It is not a second writer and it holds no second rule: the same four
// gates recordEvent runs (outcome vocabulary, subject shape, recorded text,
// the cap, the instant) are restated here before the seam, and the handover
// is the transaction half of the SAME wiring — RepoFuncs.EventInsertTx.
// recordEvent stays the sole caller of the plain insert (legal constraint
// 1's grep, re-run: the plain-insert call site still counts exactly one), and this door
// refuses a wiring that did not fill the transaction hook, naming what is
// absent instead of writing somewhere else.
func (s *Store) RecordAuthoredInTx(ctx context.Context, tx Tx, subject, policyID string, at time.Time) *Reject {
	if tx == nil {
		return rejectf("refused: the authored record for %s arrived for a transaction write with no "+
			"transaction; the seed door joins the engine's transaction, and a nil one is the missing "+
			"commit, not an absent one", quotePreview(subject))
	}
	if policyID == "" {
		return rejectf("refused: the authored record for %s names no rule; an authored row about nothing "+
			"is the audit row that lies, and the policy_id column is where the rule is named — author "+
			"through the boundary's AddInTx first, then record what it stored", quotePreview(subject))
	}
	// The four gates, restated (see RecordAuthored above): the authored
	// kind is this function's own constant, so only the variable inputs are
	// gated — subject shape, both recorded-text checks, the cap, the clock.
	if _, _, rej := parseSubject(subject); rej != nil {
		return rej
	}
	if rej := checkRecordedText("policy id", policyID); rej != nil {
		return rej
	}
	if rej := checkRecordedText("detail", authoredDetail); rej != nil {
		return rej
	}
	if utf8.RuneCountInString(authoredDetail) > MaxEventDetailRunes {
		return rejectf("refused: the authored detail is over the named cap of %d; it is a constant, so "+
			"this refusal means the const itself changed — fix the const, not this gate", MaxEventDetailRunes)
	}
	if at.IsZero() {
		return rejectf("refused: the authored record for %s has no instant; \"when was it authored\" is "+
			"the question the audit answers, and a clock read is not this boundary's to take (D5)",
			quotePreview(subject))
	}
	etr, ok := s.repo.(eventTxRepo)
	if !ok {
		return rejectf("refused: the authored record for %s was NOT written — this store's wiring carries "+
			"no transaction event add; the seed door is absent here, not bypassed (S45 T12)", quotePreview(subject))
	}
	if err := etr.PolicyEventInsertTx(ctx, tx, eventKindAuthored, subject, policyID, authoredDetail, at); err != nil {
		return &Reject{Reason: fmt.Sprintf(
			"refused: the authored record against %s was NOT recorded — the store failed: %v", subject, err)}
	}
	return nil
}
