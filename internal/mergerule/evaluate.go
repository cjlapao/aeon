// evaluate.go — the one pure function (VG11).
//
// Evaluate(rule, facts) is the whole decision: may this PR merge, on what
// evidence, and if not, why — with the reasons as (code, sentence) pairs
// the surfaces print verbatim. The baseline path reproduces today's
// mergeGate byte for byte (the T1 gate goldens are the contract); the
// extended path adds the rule's requirements on top of the same review
// and check logic.
//
// The sentence constants below are the gate's text as it exists today in
// internal/action (inline in mergeGate); T6 moves the constants to this
// file and leaves action calling them — a single definition, two eras.
// Until T6, the two copies are pinned byte-identical by
// TestBaselineEvaluateEqualsTodaysGate.

package mergerule

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/cjlapao/aeon/internal/forge"
)

// --- the gate's sentences (the baseline contract) ---------------------------

const (
	// gatePrefix and gateSuffix wrap every refusal (today's gate's
	// wrapper, verbatim): the decision is a domain refusal that happens
	// BEFORE the forge is asked.
	gatePrefix = "forge.merge gate: "
	gateSuffix = " — the merge is refused BEFORE the forge is asked (L34/D10)"

	// The review sentences (today's mergeGate, verbatim).
	sentenceNoReview      = "PR #%d has no review at all — nothing has been APPROVED"
	sentenceNotApproved   = "the latest review on PR #%d is %s, not approve (reviewer %s)"
	sentenceOutstandingRC = "PR #%d carries an outstanding request_changes from %s — a later approve by somebody else does not answer their no"

	// The check sentences.
	sentenceCheckNotGreen = "PR #%d check %s is %s, not green"

	// The thread sentence.
	sentenceThreadsOpen = "PR #%d reports %d unresolved review thread(s), the first at %s:%d by %s — honoured, and never resolved by Aeon"

	// The new fixed sentences (VG11): each names the rule field that
	// fired it.
	sentenceCheckMissing   = "required check %q was not reported"
	sentenceCheckPending   = "check %s is pending (still running)"
	sentenceCheckUnknown   = "required check %q is unknown — the forge cannot say, and unknown is not green"
	sentenceNoChecks       = "the forge reports no checks and the rule refuses to merge without them"
	sentenceLocalNotGreen  = "local gate verification is not green for head %s (state: %s)"
	sentenceStale          = "the evidence is for %s, the PR head is %s — new commits invalidate it"
	sentenceDoDNotMet      = "definition of done: %d of %d items not met"
	sentenceDoDNone        = "no acceptance criteria are recorded for this loop"
	sentenceThreadsMissing = "the forge cannot report review threads and the rule requires them resolved"
	sentenceAeonMissing    = "Aeon's own review is required and was not recorded for this head"
)

// The N/A evidence values (today's gate, verbatim) — the honest account of
// an absent signal: absence passes (L34), recorded.
const (
	evidenceChecksNoneReported = "not-configured (N/A — no checks reported is no checks, not a red gate)"
	evidenceChecksNoAPI        = "not-configured (N/A — the server reports no status API; server-agnosticism says absence passes, L34)"
	evidenceThreadsZero        = "reported-resolved (0 unresolved)"
	evidenceThreadsRefused     = "not-reported (server refused the thread query — a missing thread API is not a blocker)"
	evidenceThreadsNoAPI       = "not-reported (the server reports no thread API — L34's threadless reference server)"
)

// wrap applies the gate's wrapper to a detail sentence.
func wrap(detail string) string { return gatePrefix + detail + gateSuffix }

// refusal builds one (code, wrapped sentence).
func refusal(code, sentence string) Refusal {
	return Refusal{Code: code, Sentence: wrap(sentence)}
}

// --- the review logic (today's, verbatim) -----------------------------------

// latestReview is today's gate's convention: the LAST element of the
// server-ordered list.
func latestReview(reviews []forge.Review) forge.Review {
	return reviews[len(reviews)-1]
}

// reviewEvidence is today's three review evidence fields: the LATEST
// review's verdict, author and timestamp — verbatim, including the empty
// string a threadless server leaves (the gate records the absence, never
// invents a clock reading).
func reviewEvidence(reviews []forge.Review) (state, by, at string) {
	if len(reviews) == 0 {
		return "", "", ""
	}
	l := latestReview(reviews)
	return string(l.Verdict), l.Author, l.SubmittedAt
}

// forgeReviewRefusals is today's gate's three review refusals, in
// today's order. The baseline path stops at the first (the gate's
// early-return); the extended path collects them all.
//
//   - no review at all;
//   - the latest review is not approve;
//   - an OUTSTANDING request_changes: one that no LATER review (a higher
//     ID) by the SAME author answers with an approve — a later approve by
//     somebody else does not answer their no.
func forgeReviewRefusals(pr int, reviews []forge.Review) []Refusal {
	if len(reviews) == 0 {
		return []Refusal{refusal(CodeNoReview, fmt.Sprintf(sentenceNoReview, pr))}
	}
	var out []Refusal
	latest := latestReview(reviews)
	if latest.Verdict != forge.VerdictApprove {
		out = append(out, refusal(CodeNotApproved,
			fmt.Sprintf(sentenceNotApproved, pr, latest.Verdict, latest.Author)))
	}
	for _, rc := range reviews {
		if rc.Verdict != forge.VerdictRequestChanges {
			continue
		}
		cleared := false
		for _, later := range reviews {
			if later.ID > rc.ID && later.Author == rc.Author && later.Verdict == forge.VerdictApprove {
				cleared = true
				break
			}
		}
		if !cleared {
			out = append(out, refusal(CodeOutstandingRC,
				fmt.Sprintf(sentenceOutstandingRC, pr, rc.Author)))
			break // one sentence names the problem; the list is evidence, not a docket
		}
	}
	return out
}

// aeonReviewRefusal is the engine's review half (VG9): the token is
// "ok@<sha>" written by the engine when the review step completes ok for
// a head SHA — never model-supplied. Missing or malformed is the miss
// sentence; a token for another SHA is STALE EVIDENCE, named as such
// (both SHAs in the sentence).
func aeonReviewRefusal(rule Rule, facts Facts) Refusal {
	token := facts.AeonReview
	if token == "" || !strings.HasPrefix(token, "ok@") {
		return refusal(CodeAeonReviewMiss, sentenceAeonMissing)
	}
	if tokenSHA := token[len("ok@"):]; tokenSHA != facts.HeadSHA {
		return refusal(CodeEvidenceStale, fmt.Sprintf(sentenceStale, tokenSHA, facts.HeadSHA))
	}
	return Refusal{}
}

// aeonHalfRequired reports whether the rule's mode counts the engine's
// review (aeon and both do; forge and either do not — either needs it
// only when the forge half fails, and the refusal list then carries it).
func aeonHalfRequired(mode string) bool { return mode == ReviewModeAeon || mode == ReviewModeBoth }

// reviewRefusals is the mode's review decision (VG9's truth table):
//
//	forge  the forge half alone (today's logic, verbatim);
//	aeon   the engine half alone (the forge reviews are not the
//	       authority a rule can make them not be);
//	either the forge half OR the engine half; both fail ⇒ both lists;
//	both   both halves, both lists.
func reviewRefusals(rule Rule, facts Facts) []Refusal {
	f := forgeReviewRefusals(facts.PR, facts.Reviews)
	forgeOK := len(f) == 0
	switch rule.ReviewMode {
	case ReviewModeAeon:
		if r := aeonReviewRefusal(rule, facts); r.Code != "" {
			return []Refusal{r}
		}
		return nil
	case ReviewModeEither:
		if forgeOK {
			return nil
		}
		if r := aeonReviewRefusal(rule, facts); r.Code != "" {
			return f // the engine half fails too — but the forge half's
			// reasons are the fuller account of what is wrong
		}
		return nil // the engine half carries it
	case ReviewModeBoth:
		out := f
		if r := aeonReviewRefusal(rule, facts); r.Code != "" {
			out = append(out, r)
		}
		return out
	default: // ReviewModeForge — and an out-of-set value reads as the
		// baseline half (the closed set is enforced at the write
		// door and the migration's CHECK; the pure function stays
		// total)
		return f
	}
}

// --- the check logic ----------------------------------------------------------

// checksEvidence is today's gate's checks evidence, in its four honest
// forms (no API / none reported / the verbatim list).
func checksEvidence(facts Facts) string {
	if !facts.ChecksReadable {
		return evidenceChecksNoAPI
	}
	if len(facts.Checks.Checks) == 0 {
		return evidenceChecksNoneReported
	}
	parts := make([]string, 0, len(facts.Checks.Checks))
	for _, c := range facts.Checks.Checks {
		parts = append(parts, c.Name+"="+c.State)
	}
	return strings.Join(parts, " ")
}

// firstReportedState is the first reported occurrence of a check name
// (a server that reports the same name twice: the first is the
// report; dedupe is the gate's, not the forge's).
func firstReportedState(checks []forge.Check, name string) (string, bool) {
	for _, c := range checks {
		if c.Name == name {
			return c.State, true
		}
	}
	return "", false
}

// checksRefusals is the rule's check decision (VG11), for a non-baseline
// rule. Baseline rules get the baseline path instead, which is today's
// "every reported check must be success" and nothing else.
//
//   - every NAMED check: reported, and green. Missing is the missing
//     sentence; pending is the pending sentence (a check still running
//     is not green, and the sentence says why it might stop not being);
//     unknown is its own sentence (the forge cannot say — never read as
//     green); a reported non-success is today's not-green sentence.
//   - reported_green adds: every REPORTED check must be success (today's
//     rule on top of the named ones).
//   - required_only: an unrelated red is the forge's business —
//     recorded in the evidence, never a refusal.
//   - zero reported: the named refusals above already fire when names
//     exist; with no names, no_checks decides (allow is the baseline —
//     absence passes; refuse says the gate needs a signal).
func checksRefusals(rule Rule, facts Facts) []Refusal {
	var out []Refusal
	reported := facts.Checks.Checks

	for _, name := range rule.RequiredChecks {
		state, ok := firstReportedState(reported, name)
		switch {
		case !ok:
			out = append(out, refusal(CodeCheckMissing, fmt.Sprintf(sentenceCheckMissing, name)))
		case state == forge.CheckPending:
			out = append(out, refusal(CodeCheckPending, fmt.Sprintf(sentenceCheckPending, name)))
		case state == forge.CheckUnknown:
			out = append(out, refusal(CodeCheckUnknown, fmt.Sprintf(sentenceCheckUnknown, name)))
		case state != forge.CheckSuccess:
			out = append(out, refusal(CodeCheckNotGreen,
				fmt.Sprintf(sentenceCheckNotGreen, facts.PR, name, state)))
		}
	}

	if rule.ChecksPolicy == ChecksReported {
		for _, c := range reported {
			if c.State != forge.CheckSuccess {
				out = append(out, refusal(CodeCheckNotGreen,
					fmt.Sprintf(sentenceCheckNotGreen, facts.PR, c.Name, c.State)))
			}
		}
	}

	if len(reported) == 0 && len(rule.RequiredChecks) == 0 && rule.NoChecks == NoChecksRefuse {
		out = append(out, refusal(CodeNoChecks, sentenceNoChecks))
	}

	return dedupeRefusals(out)
}

// dedupeRefusals drops duplicate (code, sentence) pairs, keeping the
// first: a named failure also appears in the reported list under
// reported_green, and the gate says it once.
func dedupeRefusals(in []Refusal) []Refusal {
	seen := make(map[Refusal]bool, len(in))
	out := make([]Refusal, 0, len(in))
	for _, r := range in {
		if seen[r] {
			continue
		}
		seen[r] = true
		out = append(out, r)
	}
	return out
}

// baselineChecks is TODAY's check arm verbatim: every reported check
// must be success; an empty or absent report passes as not-configured.
func baselineChecks(facts Facts) []Refusal {
	var out []Refusal
	for _, c := range facts.Checks.Checks {
		if c.State != forge.CheckSuccess {
			out = append(out, refusal(CodeCheckNotGreen,
				fmt.Sprintf(sentenceCheckNotGreen, facts.PR, c.Name, c.State)))
		}
	}
	return out
}

// --- the thread logic ---------------------------------------------------------

// threadsEvidence is today's four honest forms.
func threadsEvidence(facts Facts) string {
	if !facts.Threads.Available {
		return evidenceThreadsNoAPI
	}
	if !facts.Threads.Readable {
		return evidenceThreadsRefused
	}
	if len(facts.Threads.Unresolved) == 0 {
		return evidenceThreadsZero
	}
	first := facts.Threads.Unresolved[0]
	return fmt.Sprintf("unresolved (%d): first at %s:%d by %s",
		len(facts.Threads.Unresolved), first.Path, first.Line, first.Author)
}

// threadsRefusals is the thread decision (VG8's note, VG11):
//
//   - in forge review mode the baseline already honours unresolved
//     threads (today's gate) — a refusal in every rule with that mode;
//   - in the other modes the report is honoured only when
//     require_threads_resolved says so;
//   - a REQUIRED rule whose forge cannot report threads refuses:
//     unverifiable is not green right before something irreversible.
func threadsRefusals(rule Rule, facts Facts) []Refusal {
	honoured := rule.ReviewMode == ReviewModeForge || rule.RequireThreadsResolved
	if !honoured {
		return nil
	}
	if !facts.Threads.Available || !facts.Threads.Readable {
		if rule.RequireThreadsResolved {
			return []Refusal{refusal(CodeThreadsMissing, sentenceThreadsMissing)}
		}
		return nil // absence recorded in the evidence, passes (L34)
	}
	if n := len(facts.Threads.Unresolved); n > 0 {
		first := facts.Threads.Unresolved[0]
		return []Refusal{refusal(CodeThreadsOpen,
			fmt.Sprintf(sentenceThreadsOpen, facts.PR, n, first.Path, first.Line, first.Author))}
	}
	return nil
}

// --- the run and the DoD -------------------------------------------------------

// localRefusal is the require_local decision (VG10): a PASS run on the PR
// HEAD SHA. A run for another SHA is stale evidence — the refusal names
// both SHAs; a run that is not pass is the state sentence; no run at all
// is the state sentence with state "no run".
func localRefusal(facts Facts) Refusal {
	if facts.LatestRun == nil {
		return refusal(CodeLocalNotGreen,
			fmt.Sprintf(sentenceLocalNotGreen, facts.HeadSHA, "no run"))
	}
	run := facts.LatestRun
	if run.HeadSHA != facts.HeadSHA {
		return refusal(CodeEvidenceStale, fmt.Sprintf(sentenceStale, run.HeadSHA, facts.HeadSHA))
	}
	if run.State != "pass" {
		return refusal(CodeLocalNotGreen,
			fmt.Sprintf(sentenceLocalNotGreen, run.HeadSHA, run.State))
	}
	return Refusal{}
}

// dodRefusal is the require_dod=all_met decision (VG13): every item met
// — zero items is its own refusal (a loop with no criteria has nothing
// to be done against, and "all of zero met" is the vacuous green the
// ledger exists to prevent).
func dodRefusal(facts Facts) Refusal {
	if len(facts.DoD) == 0 {
		return refusal(CodeDoDNone, sentenceDoDNone)
	}
	notMet := 0
	for _, it := range facts.DoD {
		if it.State != "met" {
			notMet++
		}
	}
	if notMet > 0 {
		return refusal(CodeDoDNotMet,
			fmt.Sprintf(sentenceDoDNotMet, notMet, len(facts.DoD)))
	}
	return Refusal{}
}

// --- the evidence ---------------------------------------------------------------

// dodEvidence is the dod value the non-baseline verdict carries: "none"
// when the rule does not require it, "none-recorded" when it does and
// the ledger is empty, otherwise "K of M met".
func dodEvidence(rule Rule, facts Facts) string {
	if rule.RequireDoD != RequireDoDAllMet {
		return "none"
	}
	if len(facts.DoD) == 0 {
		return "none-recorded"
	}
	met := 0
	for _, it := range facts.DoD {
		if it.State == "met" {
			met++
		}
	}
	return fmt.Sprintf("%d of %d met", met, len(facts.DoD))
}

// baselineEvidence is today's five evidence keys — the values the goldens
// pin byte for byte.
func baselineEvidence(facts Facts) map[string]string {
	state, by, at := reviewEvidence(facts.Reviews)
	return map[string]string{
		"review_state": state,
		"review_by":    by,
		"review_at":    at,
		"checks":       checksEvidence(facts),
		"threads":      threadsEvidence(facts),
	}
}

// extendedEvidence is baselineEvidence plus the six keys a non-baseline
// rule adds (VG11): where the rule came from, the run it measured, the
// checks' aggregate state, the DoD's account and the engine's token.
func extendedEvidence(rule Rule, facts Facts) map[string]string {
	ev := baselineEvidence(facts)
	runID, runSHA := "none", ""
	if facts.LatestRun != nil {
		runID, runSHA = facts.LatestRun.ID, facts.LatestRun.HeadSHA
	}
	ev["rule_revision"] = strconv.Itoa(rule.Revision)
	ev["verify_run_id"] = runID
	ev["verify_head_sha"] = runSHA
	ev["dod"] = dodEvidence(rule, facts)
	ev["checks_state"] = ""
	if facts.ChecksReadable {
		ev["checks_state"] = facts.Checks.State
	}
	ev["aeon_review"] = facts.AeonReview
	if ev["aeon_review"] == "" {
		ev["aeon_review"] = "absent"
	}
	return ev
}

// --- the function ----------------------------------------------------------------

// Evaluate is the whole decision (VG11): one pure function of the rule
// and the facts. A disabled rule and a baseline-shaped rule take the
// baseline path — today's gate, verbatim, first refusal wins (the gate's
// early-return order: reviews, checks, threads). Every other rule takes
// the extended path: the same review and check logic with the mode's
// truth table, the named-check semantics, require_local, require_dod and
// the threads requirement, collecting every refusal in a fixed order
// (review, checks, local, DoD, threads).
func Evaluate(rule Rule, facts Facts) Verdict {
	if !rule.Enabled || rule.IsBaseline() {
		// The baseline is the contract: today's gate, whole — a
		// disabled row's own fields do not apply (its review mode in
		// particular: a disabled row is the baseline wearing a row).
		b := Baseline()
		ev := baselineEvidence(facts)
		all := append([]Refusal(nil), forgeReviewRefusals(facts.PR, facts.Reviews)...)
		all = append(all, baselineChecks(facts)...)
		all = append(all, threadsRefusals(b, facts)...)
		for _, r := range all {
			return Verdict{Allowed: false, Refusals: []Refusal{r}, Evidence: ev}
		}
		return Verdict{Allowed: true, Refusals: nil, Evidence: ev}
	}

	ev := extendedEvidence(rule, facts)
	var all []Refusal
	all = append(all, reviewRefusals(rule, facts)...)
	all = append(all, checksRefusals(rule, facts)...)
	if rule.RequireLocal {
		if r := localRefusal(facts); r.Code != "" {
			all = append(all, r)
		}
	}
	if rule.RequireDoD == RequireDoDAllMet {
		if r := dodRefusal(facts); r.Code != "" {
			all = append(all, r)
		}
	}
	all = append(all, threadsRefusals(rule, facts)...)

	if len(all) > 0 {
		return Verdict{Allowed: false, Refusals: all, Evidence: ev}
	}
	return Verdict{Allowed: true, Refusals: nil, Evidence: ev}
}
