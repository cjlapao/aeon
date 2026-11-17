package continuous

// Report is what scheduler.preview answers with (CN15), and the reason
// the preview is the tick rather than a description of it: it is built
// from the SAME Plan call, over the same snapshot type the worker feeds
// the same function. A preview that lies would need Plan to lie first,
// and then the tick would have done it too.
type Report struct {
	// Decisions is every line the tick would decide.
	Decisions []Decision
	// WouldStart is the start-verdict subset of Decisions — by
	// construction equal to Plan's starts (TestPreviewIsPlan).
	WouldStart []Decision
}

// Preview runs the tick's own function over the snapshot and arranges the
// answer: the starts, and — the CN15 half that makes it legible — a
// failing reason for EVERY unit that would not start. A unit with its own
// line (its eligibility refusal, its capacity deferral) already has its
// reason; a ready unit that never reached a line of its own inherits its
// repository's blocker — and when the repository would start someone
// else, the honest first-failing reason for the rest is repo-occupied: the
// start that would win the tick is exactly what would occupy the
// repository before the next candidate's turn came.
func Preview(s Snapshot) Report {
	ds := Plan(s)
	r := Report{Decisions: ds}
	for _, d := range ds {
		if d.Verdict == VerdictStart {
			r.WouldStart = append(r.WouldStart, d)
		}
	}
	return r
}

// Failing answers the first failing reason for one subject of one
// repository, in the same strings Plan used — the subject's own decision
// if it has one, the repository's blocker if it does not. Every unit the
// snapshot called ready has an answer here: TestPreviewIsPlan walks the
// whole snapshot and pins that.
func (r Report) Failing(root string, subject Subject) (Decision, bool) {
	var repoBlocker *Decision
	for i, d := range r.Decisions {
		if d.Root != root {
			continue
		}
		if d.Subject == subject {
			if d.Verdict != VerdictStart {
				return d, true
			}
			// The unit itself would start; it has no failing reason.
			return Decision{}, false
		}
		if d.Subject.Kind == "repository" && d.Verdict != VerdictStart && repoBlocker == nil {
			repoBlocker = &r.Decisions[i]
		}
	}
	if repoBlocker != nil {
		return *repoBlocker, true
	}
	// No repository-level line: if this tick would start SOME unit of this
	// repository, the honest first-failing reason for the rest is
	// repo-occupied — the winning start is exactly what occupies the
	// repository before another candidate's turn could come. Only when
	// nobody starts does the candidate's own deferral or refusal become
	// the repository's answer for every unit it left waiting.
	var started, declined, any *Decision
	for i, d := range r.Decisions {
		if d.Root != root {
			continue
		}
		if any == nil {
			any = &r.Decisions[i]
		}
		switch d.Verdict {
		case VerdictStart:
			if started == nil {
				started = &r.Decisions[i]
			}
		case VerdictDefer, VerdictRefuse:
			if declined == nil {
				declined = &r.Decisions[i]
			}
		}
	}
	if started != nil {
		return Decision{
			Root: root, Subject: subject, Verdict: VerdictSkip,
			Reason: ReasonRepoOccupied, Weight: started.Weight,
			Detail: "this tick would start " + started.Subject.Kind + " " + started.Subject.ID +
				" — one candidate per repository (CN6)",
		}, true
	}
	if declined != nil {
		return *declined, true // the candidate's deferral/refusal defers the repository's other units too
	}
	if any != nil {
		return *any, true
	}
	// A repository that decides nothing about a unit it was asked about is
	// a gap, not an answer — and the preview tests walk every ready unit,
	// so the gap cannot hide.
	return Decision{}, false
}
