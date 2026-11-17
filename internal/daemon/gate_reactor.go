// gate_reactor.go — S57 T7's terminal gate-failure reactor (VG3, VG16).
// It joins publishLoop's terminal fan-out as the fourth reactor, after
// queue/roadmap/triage, and files ONE deduped unvalidated bug for the two
// loop-side gate failures, terminal only, never a mid-rework red (OD10):
//
//  1. `loop.blocked` whose reason kind is `verify-failed`, or
//     `exhaustion` with Action `verify.run` (the verify rework's ceiling)
//     — subject: the component root (one bug per tree, count bumps).
//  2. `loop.blocked` whose reason is the merge refusal carrying the
//     red-check sentence (the evaluator's fixed template, scoped to
//     kind `action-failed` + Action `forge.merge`) — subject: the root
//     plus the PR.
//
// It reacts to the BlockedReason STRUCT only: publishLoop's pinned golden
// (T1) emits string reasons, and a reactor that answered strings would
// have changed that fixture — the struct-only door keeps the T1 diff at
// none (recorded in T7's Notes). The engine's emit carries the reason as
// a POINTER (`blockTo` passes `&BlockedReason{...}`), so the door accepts
// the pointer (and the value, for a caller that dereferences).
package daemon

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/cjlapao/aeon/internal/action"
	"github.com/cjlapao/aeon/internal/gatewatch"
	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/triage"
)

// notGreenRefusalTail is the red-check sentence's fixed tail. The
// evaluator's template is "PR #%d check %s is %s, not green" — the code
// (T5's refusal code) never reaches the blocked reason, only this
// sentence does, so the reactor matches the sentence, scoped to the
// refusal's kind and action (the transport-refusal prefix, a different
// refusal, must never match).
const notGreenRefusalTail = "is failure, not green"

var prInRefusal = regexp.MustCompile(`PR #(\d+)`)

// blockedReasonOf is the struct-only door: the engine's emit carries the
// reason as a pointer (blockTo passes &BlockedReason{...}), a caller that
// dereferenced hands the value — both answer. Anything else (a string,
// the T1 golden's shape; a nil; an absent field) is not a gate failure
// and is not answered.
func blockedReasonOf(v any) (loop.BlockedReason, bool) {
	switch r := v.(type) {
	case *loop.BlockedReason:
		if r == nil {
			return loop.BlockedReason{}, false
		}
		return *r, true
	case loop.BlockedReason:
		return r, true
	default:
		return loop.BlockedReason{}, false
	}
}

// onGateTerminal is the subscription, called from publishLoop at the
// daemon's own emission point after the triage reactor. It reads the
// reason on the caller's thread (cheap, one type assertion) and dispatch
// to the goroutine is the rest — a gate reaction must never delay the
// step that just finished.
func (d *Daemon) onGateTerminal(kind, componentRoot, loopSlug string, fields map[string]any) {
	if d.store == nil {
		return
	}
	reason, ok := blockedReasonOf(fields["reason"])
	if !ok {
		return // a string reason (the T1 golden's shape) is not a gate failure
	}
	var (
		kindName  string
		exhausted bool
		pr        int
	)
	switch {
	case reason.Kind == "verify-failed":
		kindName = gatewatch.KindVerify
	case reason.Kind == "exhaustion" && reason.Action == action.VerifyRun:
		kindName = gatewatch.KindVerify
		exhausted = true
	case reason.Kind == "action-failed" && reason.Action == action.ForgeMerge &&
		strings.Contains(reason.Detail, notGreenRefusalTail):
		kindName = gatewatch.KindMergeRefused
		if m := prInRefusal.FindStringSubmatch(reason.Detail); m != nil {
			fmt.Sscanf(m[1], "%d", &pr)
		}
	default:
		return
	}
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				d.log.Infof("gate: the terminal reactor for loop %s recovered from %v — the ledger and the bug rows remain the truth (P29)", loopSlug, rec)
			}
		}()
		if kindName == gatewatch.KindMergeRefused {
			if pr == 0 {
				d.log.Infof("gate: the merge refusal for loop %s names no PR — the subject cannot be built, filing skipped", loopSlug)
				return
			}
			d.handleGateRefused(context.Background(), componentRoot, loopSlug, reason, pr)
		} else {
			d.handleGateVerify(context.Background(), componentRoot, loopSlug, reason, exhausted)
		}
	}()
}

// handleGateVerify files the verify side: subject per component root, so
// the same red gate failing across loops of the same tree files ONE bug
// and bumps its count (behaviour 6). The run's own row supplies the ids
// the body names (run id, head sha, the fixed-template summary, the
// failing gate's exit code) — no output text crosses into the body.
func (d *Daemon) handleGateVerify(ctx context.Context, componentRoot, loopSlug string, reason loop.BlockedReason, exhausted bool) {
	facts := gatewatch.Facts{
		Kind:          gatewatch.KindVerify,
		Exhausted:     exhausted,
		ComponentRoot: componentRoot,
		LoopSlug:      loopSlug,
		Detail:        reason.Detail,
	}
	if runs, err := d.store.ListVerifyRuns(store.VerifyRunFilter{
		ComponentRoot: componentRoot,
		LoopSlug:      loopSlug,
		State:         "fail",
		Limit:         1,
	}); err != nil {
		d.log.Infof("gate: the failed-run read for %s failed: %v", loopSlug, err)
	} else if len(runs) > 0 {
		run := runs[0]
		facts.RunID = run.ID
		facts.HeadSHA = run.HeadSHA
		facts.Summary = run.Summary
		if _, results, err := d.store.GetVerifyRun(run.ID); err == nil {
			for _, r := range results {
				failed := r.TimedOut || r.Signal != "" || (r.ExitCode != nil && *r.ExitCode != 0)
				if !failed {
					continue
				}
				facts.GateName = r.Name
				switch {
				case r.TimedOut:
					facts.GateState = "timed-out"
				case r.Signal != "":
					facts.GateState = "signalled " + r.Signal
				default:
					facts.GateState = "failed"
				}
				if r.ExitCode != nil {
					facts.ExitCode = r.ExitCode
				}
				break
			}
		}
	}
	subject := gatewatch.SubjectVerify(componentRoot)
	bugID, _, err := d.fileGateBug(ctx, gatewatch.KindVerify, subject, componentRoot, facts)
	if err != nil {
		d.log.Infof("gate: the verify filing for %s failed: %v", loopSlug, err)
	}
	d.Publish(protocol.KindGateFailed, componentRoot, map[string]any{
		"kind":          gatewatch.KindVerify,
		"componentRoot": componentRoot,
		"loop":          loopSlug,
		"run":           facts.RunID,
		"pr":            0,
		"bugId":         bugID,
	})
}

// handleGateRefused files the merge-refusal side: subject per root plus
// PR. The check name comes out of the refusal's sentence (the fixed
// template's slot), filtered by the filer before it enters the title;
// the rule revision is the component's effective rule at this instant
// (an unreadable rule is an omitted line, never a fabricated number).
func (d *Daemon) handleGateRefused(ctx context.Context, componentRoot, loopSlug string, reason loop.BlockedReason, pr int) {
	facts := gatewatch.Facts{
		Kind:          gatewatch.KindMergeRefused,
		ComponentRoot: componentRoot,
		LoopSlug:      loopSlug,
		PR:            pr,
		Detail:        reason.Detail,
	}
	if comp, err := d.store.ComponentForRoot(componentRoot); err == nil {
		if rule, _, err := d.store.EffectiveMergeRule(comp.ID); err == nil {
			facts.RuleRevision = rule.Revision
		}
	}
	subject := gatewatch.SubjectMergeRefused(componentRoot, pr)
	bugID, _, err := d.fileGateBug(ctx, gatewatch.KindMergeRefused, subject, componentRoot, facts)
	if err != nil {
		d.log.Infof("gate: the merge-refusal filing for %s PR #%d failed: %v", loopSlug, pr, err)
	}
	d.Publish(protocol.KindGateFailed, componentRoot, map[string]any{
		"kind":          gatewatch.KindMergeRefused,
		"componentRoot": componentRoot,
		"loop":          loopSlug,
		"run":           "",
		"pr":            pr,
		"bugId":         bugID,
	})
}

// fileGateBug is the daemon's Filer (VG16): the dedupe claim FIRST, the
// bug second. An existing subject files nothing (the bug already stands)
// and returns its count; a new subject files exactly one bug through the
// triage door (store defaults: unvalidated, priority unknown — the filing
// passes no label) with FiledBy `system:gates`, then stamps the claim. A
// refused or failed create leaves the claim unfilled (count 1, no
// bug_id): the next claim of the same subject re-files and stamps — the
// dedupe is per subject and self-healing, never a silent drop.
func (d *Daemon) fileGateBug(ctx context.Context, kind, subject, componentRoot string, facts gatewatch.Facts) (int64, bool, error) {
	if d.store == nil {
		return 0, false, fmt.Errorf("gate: the daemon has no store")
	}
	existing, count, err := d.store.ClaimGateFiling(subject, kind)
	if err != nil {
		return 0, false, err
	}
	if existing {
		d.log.Infof("gate: the gate failure %s already stands (count %d) — filing nothing", subject, count)
		return 0, false, nil
	}
	if d.Table == nil || d.Table.Triage == nil {
		d.log.Infof("gate: triage is not wired — the claim for %s stands unfilled (the next filing of the same subject heals it)", subject)
		return 0, false, nil
	}
	if comp, err := d.store.ComponentForRoot(componentRoot); err == nil {
		facts.ComponentID = comp.ID
		facts.ComponentSlug = comp.Slug
	}
	title, body := gatewatch.BugFor(kind, facts)
	bug, refusal := d.Table.Triage.Create(componentRoot, triage.NewBug{
		Title:   title,
		Body:    body,
		FiledBy: gatewatch.FiledBy,
	})
	if refusal != nil {
		d.log.Infof("gate: triage refused the filing for %s: %s", subject, refusal.Reason)
		return 0, false, nil
	}
	if err := d.store.SetGateFilingBug(subject, bug.ID); err != nil {
		d.log.Infof("gate: the claim for %s could not be stamped with bug %d: %v", subject, bug.ID, err)
	}
	return bug.ID, true, nil
}

// gateFiler adapts the daemon's fileGateBug to the watcher's Filer seam,
// so the post-merge red and the loop-side reds share the ONE dedupe door.
type gateFiler struct{ d *Daemon }

// FileGateBug implements gatewatch.Filer over the daemon's store and
// triage.
func (f gateFiler) FileGateBug(ctx context.Context, kind, subject, componentRoot string, facts gatewatch.Facts) (int64, bool, error) {
	return f.d.fileGateBug(ctx, kind, subject, componentRoot, facts)
}
