package mergequeue

import "fmt"

// describe.go — the sentences that leave the queue: the "next-step
// sentence" `mergequeue.show` renders (T8) and the authority statement a
// settings write to `auto` must show (MQ13). Both are built from ids,
// enums and numbers ONLY (SEC-033): there is no parameter in either
// function through which caller prose could ride, and the test proves the
// builders take nothing but these types.

// Describe is one item's standing in one sentence — status, position, the
// branch and base as refs (branch names are repository facts, not free
// text: they already passed the identity rule in ValidateEnqueue).
func Describe(item Item) string {
	if !item.Status.Valid() {
		return fmt.Sprintf("item %s carries the unknown status %q — the queue does not describe what it cannot name", item.ID, string(item.Status))
	}
	switch item.Status {
	case StatusQueued:
		return fmt.Sprintf("item %s (%s → %s) waits at position %d, %s",
			item.ID, item.Branch, item.BaseBranch, item.Position, attendedWord(item.Attended))
	case StatusCatchingUp:
		return fmt.Sprintf("item %s (%s → %s) is catching up: the landing loop merges %s forward",
			item.ID, item.Branch, item.BaseBranch, item.BaseBranch)
	case StatusVerifying:
		return fmt.Sprintf("item %s (%s → %s) is verifying: the gates run against the integration sha",
			item.ID, item.Branch, item.BaseBranch)
	case StatusReady:
		return fmt.Sprintf("item %s (%s → %s) is ready: waiting for a land — a person's, or the driver's under mode auto",
			item.ID, item.Branch, item.BaseBranch)
	case StatusMerging:
		return fmt.Sprintf("item %s (%s → %s) is merging: the landing step runs",
			item.ID, item.Branch, item.BaseBranch)
	case StatusConflicted:
		return fmt.Sprintf("item %s (%s → %s) is conflicted: the hunks live in the integration worktree, never here",
			item.ID, item.Branch, item.BaseBranch)
	case StatusFailed:
		if item.EndReason != "" {
			return fmt.Sprintf("item %s (%s → %s) failed (%s): a person's retry is its only exit",
				item.ID, item.Branch, item.BaseBranch, item.EndReason)
		}
		return fmt.Sprintf("item %s (%s → %s) failed: a person's retry is its only exit",
			item.ID, item.Branch, item.BaseBranch)
	case StatusMerged:
		return fmt.Sprintf("item %s (%s → %s) is merged", item.ID, item.Branch, item.BaseBranch)
	case StatusRemoved:
		return fmt.Sprintf("item %s (%s → %s) was removed", item.ID, item.Branch, item.BaseBranch)
	}
	return "" // unreachable: the guard above answers unknown statuses
}

func attendedWord(a bool) string {
	if a {
		return "attended (its loop may answer the human doors)"
	}
	return "unattended (asks deny, per the standing rule)"
}

// Settings is the shape a queue-settings change is described from (again:
// enums and numbers). Statement returns the sentence a caller must show
// BEFORE/AFTER a write that widens authority — mode auto is authority-
// adjacent (MQ13), so `mergequeue.settings` renders this verbatim rather
// than paraphrasing it, and the same sentence rides the audit detail.
type Settings struct {
	ComponentRoot  string
	Mode           Mode
	ConflictPolicy ConflictPolicy
	LandMethod     LandMethod
	MaxCatchUps    int
}

// Statement is the fixed sentence for one settings shape. Only the `auto`
// mode adds the authority warning; the branches below are the whole of it —
// nothing caller-supplied enters.
func Statement(s Settings) string {
	base := fmt.Sprintf("queue settings for %s: mode %s, conflict policy %s, land method %s, max catch-ups %d",
		s.ComponentRoot, s.Mode, s.ConflictPolicy, s.LandMethod, s.MaxCatchUps)
	switch s.Mode {
	case ModeOff:
		return base + " — dark: nothing starts and nothing enqueues itself; a person's enqueue still lands and sits queued"
	case ModeManual:
		return base + " — items advance to ready and wait for a person's land"
	case ModeAuto:
		return base + " — AUTHORITY WIDENING: mode auto lets the queue land merges without a person, " +
			"and it is only effective while the S56 `merge` grant for this repository stands and the S57 merge rule is satisfied; " +
			"without the grant the landing step denies at once and the item ends failed(denied)"
	}
	return base
}
