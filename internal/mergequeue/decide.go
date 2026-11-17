package mergequeue

import "fmt"

// decide.go — `Next(item, facts)` is the whole of MQ5 as a pure function.
// The reactor GATHERS the facts (loop statuses, behind, config, the S56
// grant, gate evidence, the forge's answers) and this decides; the daemon,
// the CLI and the tools never re-derive anything (MQ3).
//
// Two absolutes, each with its own test:
//   - Unknown or unavailable facts NEVER advance an item (P31's three
//     states — a nil here is a question, not a zero).
//   - The core never invents a denial or a failure: `failed(denied)` happens
//     only when a fact REPORTS the `forge.land` denial; `failed(head-moved)`
//     only when a fact reports the sha mismatch.

// Item is the pure shape of a queue row — the fields a decision reads. The
// adapter fills it from the store's MergeItem; nothing here imports the
// store.
type Item struct {
	ID            string
	ComponentRoot string
	BaseBranch    string
	Branch        string
	Status        Status
	Position      int
	Attended      bool
	// EndReason is the reason_kind a finished item ended with ("" unless
	// failed/removed/merged) — carried so the hold sentence can name it.
	EndReason string
}

// ConflictFacts says the integration merge reported conflicts: the COUNT
// only. Paths never ride a Decision (SEC-033 — the database and the event
// log see counts and enums; the hunks exist only in the worktree).
type ConflictFacts struct {
	Count int
}

// Facts is everything Next reads about the world, gathered by the reactor.
// Pointer fields are three-state: nil means UNKNOWN, and unknown never
// advances an item.
type Facts struct {
	// SlotHolder is the id of the item holding the landing slot on this
	// item's base branch, "" when the slot is free (the caller passes ""
	// when the holder IS this item).
	SlotHolder string
	// HeadOfBase is whether this item is first in its base's queue order.
	// nil = the reactor has not looked.
	HeadOfBase *bool
	// Config is the repository's queue settings (mode etc.).
	Config Config

	// LandingLoop / ResolveLoop are the states of the item's landing and
	// resolution loops. LoopNone means no such loop exists.
	LandingLoop LoopState
	ResolveLoop LoopState

	// Behind is commits on origin/<base> missing from the branch, valid
	// only when BehindState is "ok". Three states (MQ10): ok / unavailable
	// / not-a-repo — and unavailable never renders as 0.
	Behind      *int
	BehindState string
	// CatchUps counts the catch-ups already spent, against MaxCatchUps.
	CatchUps int

	// IntegrationSHA is a clean built integration branch, "" = none yet.
	IntegrationSHA string
	// Conflict is non-nil while the integration merge reports conflicts.
	Conflict *ConflictFacts

	// GateResult is the verify evidence for the item's integration sha:
	// nil = none yet, false = red, true = green.
	GateResult *bool

	// HeadSHAMatches is the MQ11 binding: the PR head the land step will
	// merge equals verified_sha. A fact from the reactor's forge read —
	// this function never calls the forge.
	HeadSHAMatches bool
	// RuleOK is the S57 merge rule's answer; nil = unknown (hold).
	RuleOK *bool
	// GrantMerge says the S56 per-repository `merge` grant exists.
	GrantMerge bool
	// LandRequested is a person's `mergequeue.land` standing.
	LandRequested bool
	// LandDenial is the reason kind the `forge.land` step DENIED with,
	// "" when it has not denied. The only door to failed(denied).
	LandDenial string
	// Landed + Ancestor are the post-guard facts (MQ12.4): the forge
	// reported the merge, and IsAncestor(mergeSHA, origin/<base>).
	Landed   bool
	Ancestor *bool
	// Refused is the reason kind of a landing refusal ("protected",
	// "head-moved", …), "" when nothing refused.
	Refused string
	// Aborted is the person's `abort` answer to a conflict question (MQ9).
	Aborted bool
}

// LoopStart names a loop the decision starts (T7's driver obeys it; Next
// itself starts nothing).
type LoopStart struct {
	Blueprint string
}

// Decision is Next's answer. Advance distinguishes "the item moves to To"
// from "the item stays; this is why" — a Hold ALWAYS echoes the item's
// current status in To, so a caller can never mistake a hold for a move.
// Sentence names the reason in enums and ids only (SEC-033).
type Decision struct {
	To        Status
	Reason    string // a ReasonKind word, "" when not ending the item
	StartLoop *LoopStart
	Sentence  string
	Advance   bool
}

func hold(item Item, format string, args ...any) Decision {
	return Decision{To: item.Status, Sentence: fmt.Sprintf(format, args...)}
}

func move(to Status, sentence string) Decision {
	return Decision{To: to, Sentence: sentence, Advance: true}
}

func end(item Item, kind ReasonKind, sentence string) Decision {
	return Decision{To: StatusFailed, Reason: string(kind), Sentence: sentence, Advance: true}
}

// nextBehindState values — MQ10's three states as words.
const (
	BehindOK          = "ok"
	BehindUnavailable = "unavailable"
	BehindNotARepo    = "not-a-repo"
)

// Next is MQ5's table, row for row. It reads only item and facts; the mode
// check comes first because `off` must stop everything the driver could
// start (MQ13) while a person's own doors (retry/dequeue/land →
// LandRequested) still move through the same table below.
func Next(item Item, f Facts) Decision {
	switch item.Status {

	case StatusQueued:
		if f.Config.Mode == ModeOff {
			return hold(item, "mode is off — nothing starts (the item sits queued until a person turns the mode on)")
		}
		if f.SlotHolder != "" {
			return hold(item, "another item holds the landing slot on %s (item %s)", item.BaseBranch, f.SlotHolder)
		}
		if f.HeadOfBase == nil {
			return hold(item, "not known whether this item is head of %s — the queue will not act on an unknown", item.BaseBranch)
		}
		if !*f.HeadOfBase {
			return hold(item, "not the head of %s — earlier items go first", item.BaseBranch)
		}
		return Decision{To: StatusCatchingUp, Advance: true,
			StartLoop: &LoopStart{Blueprint: "land-feature"},
			Sentence:  fmt.Sprintf("head of %s with the slot free — the landing loop starts", item.BaseBranch)}

	case StatusCatchingUp:
		if f.Conflict != nil {
			d := move(StatusConflicted, fmt.Sprintf("the integration merge reports %d conflicted path(s) — the hunks stay in the worktree", f.Conflict.Count))
			if f.Config.ConflictPolicy == PolicyResolve {
				d.StartLoop = &LoopStart{Blueprint: "resolve-conflict"}
				d.Sentence += " — conflict policy 'resolve' starts the resolution loop"
			} else if f.Config.ConflictPolicy == PolicyAsk && !item.Attended {
				d.Sentence += " — policy 'ask' on an unattended item is blocked-visible: the inbox shows it and nothing else happens"
			}
			return d
		}
		if f.IntegrationSHA != "" {
			return move(StatusVerifying, "the integration branch is built clean — the gates run against its sha")
		}
		switch f.LandingLoop {
		case LoopBlocked, LoopFailed, LoopStopped:
			return hold(item, "the landing loop is %s without a recorded conflict — the queue waits for a person", f.LandingLoop)
		default:
			return hold(item, "the landing loop is %s — catch-up still running", f.LandingLoop)
		}

	case StatusVerifying:
		if f.GateResult == nil {
			return hold(item, "no gate evidence yet for the integration sha")
		}
		if !*f.GateResult {
			return end(item, ReasonGateFailed, "a gate went red against the integration sha")
		}
		return move(StatusReady, "the gates are green for the integration sha the verification is bound to")

	case StatusReady:
		if f.BehindState != BehindOK || f.Behind == nil {
			return hold(item, "staleness is %s — the queue will not land on facts it cannot see", f.BehindState)
		}
		if *f.Behind > 0 {
			if f.CatchUps < f.Config.MaxCatchUps {
				return move(StatusQueued, fmt.Sprintf("the base moved (%d behind) — catch-up %d of %d", *f.Behind, f.CatchUps+1, f.Config.MaxCatchUps))
			}
			return end(item, ReasonStaleExhausted, fmt.Sprintf("the base keeps moving — %d catch-ups is the bound", f.CatchUps))
		}
		if f.Refused != "" {
			// A refusal the reactor reports against a ready item (the land
			// step answered and lost the race to the base) is judged like a
			// merging refusal below.
			return refuse(item, f)
		}
		if f.LandDenial != "" {
			// The only way to failed(denied): the step itself said no.
			return end(item, ReasonKind(f.LandDenial), "the landing step denied — the reason is its own, not this function's invention")
		}
		switch f.Config.Mode {
		case ModeManual:
			if f.LandRequested {
				if !f.HeadSHAMatches {
					return end(item, ReasonHeadMoved, "a person landed, but the PR head no longer matches verified_sha")
				}
				return move(StatusMerging, "a person lands it — the forge.land step runs")
			}
			return hold(item, "ready — mode manual waits for a person's land")
		case ModeAuto:
			if !f.GrantMerge {
				return hold(item, "mode auto without the S56 merge grant — the landing step's denial will end the item; this decision does not invent one")
			}
			if !f.HeadSHAMatches {
				return end(item, ReasonHeadMoved, "the PR head no longer matches verified_sha — a green A can never land a B (MQ11)")
			}
			if f.RuleOK == nil {
				return hold(item, "the merge rule has not answered — reviews and checks gate the landing")
			}
			if !*f.RuleOK {
				return hold(item, "the merge rule is not satisfied yet")
			}
			return move(StatusMerging, "mode auto with the grant, the rule and the sha binding all satisfied — the forge.land step runs")
		default: // ModeOff — a person can still stand behind a ready item.
			if f.LandRequested {
				if !f.HeadSHAMatches {
					return end(item, ReasonHeadMoved, "a person landed, but the PR head no longer matches verified_sha")
				}
				return move(StatusMerging, "a person lands it — mode off stops the DRIVER, never a person")
			}
			return hold(item, "mode is off — nothing the driver does can advance this item")
		}

	case StatusMerging:
		if f.Landed {
			if f.Ancestor == nil {
				return hold(item, "the forge says landed; the ancestor proof against origin/%s has not answered", item.BaseBranch)
			}
			if !*f.Ancestor {
				return end(item, ReasonForge, "the merge sha is not an ancestor of the base tip — a phantom merge is an honest failure (F7)")
			}
			return Decision{To: StatusMerged, Advance: true,
				Sentence: fmt.Sprintf("landed onto %s with the ancestor proof", item.BaseBranch)}
		}
		if f.Refused != "" {
			return refuse(item, f)
		}
		if f.LandDenial != "" {
			return end(item, ReasonKind(f.LandDenial), "the landing step denied — the reason is its own, not this function's invention")
		}
		return hold(item, "the landing step runs")

	case StatusConflicted:
		if f.Aborted {
			return end(item, ReasonConflict, "the person answered abort")
		}
		if f.ResolveLoop == LoopDone {
			return Decision{To: StatusQueued, Advance: true,
				StartLoop: nil,
				Sentence:  "the resolution loop finished done — the item re-enters the queue and re-verifies against a NEW integration sha (MQ9: the resolver's claim is never the evidence)"}
		}
		switch f.Config.ConflictPolicy {
		case PolicyAsk:
			if item.Attended {
				return hold(item, "the gate question (resolve | abort) waits for a person")
			}
			return hold(item, "policy 'ask' on an unattended item is blocked-visible — the inbox shows it and nothing else happens")
		case PolicyResolve:
			if f.ResolveLoop == LoopNone {
				return hold(item, "policy 'resolve' but the resolution loop is gone — the queue waits for a person")
			}
			return hold(item, "the resolution loop is %s", f.ResolveLoop)
		default:
			return hold(item, "policy 'block' — a person must resolve it")
		}

	case StatusFailed:
		if item.EndReason != "" {
			return hold(item, "failed(%s) — only a person's retry moves it", item.EndReason)
		}
		return hold(item, "failed — only a person's retry moves it")

	case StatusMerged:
		return hold(item, "merged — terminal")
	case StatusRemoved:
		return hold(item, "removed — terminal")
	}
	// An unknown status cannot reach here through Valid types; if a caller
	// forces one, the honest answer is a hold, never a confident move.
	return hold(item, "unknown status %s — the queue holds", item.Status)
}

// refuse judges a reported refusal: the base moving is a bounded bounce back
// to queued; every other kind ends the item with its OWN reason kind.
func refuse(item Item, f Facts) Decision {
	kind := ReasonKind(f.Refused)
	if !kind.Valid() {
		kind = ReasonUnknown
	}
	if kind == ReasonHeadMoved {
		if f.BehindState == BehindOK && f.Behind != nil && *f.Behind > 0 {
			if f.CatchUps < f.Config.MaxCatchUps {
				return move(StatusQueued, fmt.Sprintf("the base moved under the landing — catch-up %d of %d", f.CatchUps+1, f.Config.MaxCatchUps))
			}
			return end(item, ReasonStaleExhausted, fmt.Sprintf("the base keeps moving — %d catch-ups is the bound", f.CatchUps))
		}
		// Refused head-moved WITHOUT a visible base move: re-queue anyway
		// (bounded), the mismatch itself is the base-move signal.
		if f.CatchUps < f.Config.MaxCatchUps {
			return move(StatusQueued, fmt.Sprintf("the head moved — catch-up %d of %d", f.CatchUps+1, f.Config.MaxCatchUps))
		}
		return end(item, ReasonStaleExhausted, "the head keeps moving — the catch-up bound is reached")
	}
	return end(item, kind, fmt.Sprintf("the landing was refused (%s) — honest failure, no workaround (N4)", kind))
}
