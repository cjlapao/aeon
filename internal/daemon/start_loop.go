package daemon

// start_loop.go — S40 T4's narrow extraction (AU9). The body below is the
// StartBlueprintLoop closure's own body, moved verbatim out of blueprint.go
// so the socket path and the event path share ONE start door; the closure
// now only lifts the result back to `any`. Nothing here is new policy: the
// workspace trim, the slug default, the mode validation, the With copy,
// the S38 T13 link resolution and the requires check are the code that was
// already running, with four provenance fields added to the store.NewLoop
// literal — and TestTheSocketStartPathSuccessShapeIsRecorded was written
// and green against the unextracted closure precisely so a drift here is a
// failing test, not a rumour.
//
// startExtras is unexported on purpose: no socket client, no protocol type
// and no fixture can reach it (T4's Do-NT — provenance, kickoff and depth
// never sit on a socket-visible type). Only event_fire.go within this
// package hands them over.

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// startEpicID names the epic a start's goal belongs to, for E3's budget set
// (a goal with no epic is a start outside any epic's cap).
func startEpicID(goal *store.Goal) string {
	if goal == nil {
		return ""
	}
	return goal.EpicID
}

// startExtras is what an event start adds to the ordinary start. Every
// field is the zero value on the socket path — which is exactly the
// byte-parity the safety-net test pins.
type startExtras struct {
	StartedBy    string
	TriggerID    string
	Depth        int
	FirstMessage *store.NewChatMessage
	// Autonomy is S56 AL8's pin: the immutable snapshot a policy-resolved
	// start carries. nil (the socket path) means no policy consulted — no pin
	// row, byte-identical to today. It is internal: never on the wire, written
	// once in CreatePinnedLoop's transaction.
	Autonomy *store.AutonomyPin
}

// startLoopWith is the one start door: validate, resolve, pin, run. An
// event fire reaches it through event_fire.go with extras; every other
// caller reaches it with the zero extras.
func (d *Daemon) startLoopWith(ctx context.Context, p protocol.LoopStartParams, x startExtras) (map[string]any, error) {
	if d.loopRunner != nil && d.loopRunner.Draining() {
		// T4 (LC19): a daemon that is going away refuses the start family
		// BEFORE any row exists — a loop born into a draining daemon would die
		// at its first step, and its row would read running for nothing.
		// Refused, not transport-failed: the daemon understood and declined.
		return nil, protocol.ErrLoopControlRefused.WithWhat(
			"loop start refused: the daemon is shutting down and will not accept new loop work — restart it, then start again")
	}
	p.WorkspaceRoot = strings.TrimSpace(p.WorkspaceRoot)
	if err := validateLoopWorkspace(p.WorkspaceRoot); err != nil {
		return nil, err
	}
	slug := p.Slug
	if slug == "" {
		slug = "loop-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	mode := p.Mode
	if mode == "" {
		mode = "standard"
	}
	if mode != "simple" && mode != "standard" && mode != "poc" {
		return nil, protocol.ErrUnsupportedMode.WithWhat("mode %q is not one of simple, standard, poc", mode)
	}
	// The model options are checked before any row exists too: a typo is refused here, naming the accepted values,
	// instead of failing the first model step of a loop that was already created.
	if e := strings.TrimSpace(p.ReasoningEffort); !model.ValidReasoningEffort(e) {
		return nil, protocol.ErrBadParams{Err: fmt.Errorf("reasoningEffort %q is not one of %s", p.ReasoningEffort, strings.Join(model.ReasoningEfforts(), ", "))}
	}
	if p.Thinking != "" && p.Thinking != "on" && p.Thinking != "off" {
		return nil, protocol.ErrBadParams{Err: fmt.Errorf("thinking %q is not on, off, or empty", p.Thinking)}
	}
	// S68 B6: the chosen skills are checked BEFORE any row exists — an
	// unknown name refuses the start naming every offender (the same
	// nothing-half-written rule as the link resolution below), and nothing
	// chosen stays exactly today's shape. The launch's lines live in the
	// per-loop cache; its NAMES ride the kickoff row as the durable
	// receipt, so a restart reloads what this launch rode.
	res, resErr := d.resolveChosenSkills(p.Skills)
	if resErr != nil {
		return nil, protocol.ErrBadParams{Err: resErr}
	}
	// S77: the launch's ATTACHED files are resolved on the same nothing-half-
	// written rule — references resolve against the put scope {slug,
	// workspaceRoot} through the one door `loop.message` uses, and the
	// start's route must DECLARE the capability each type asks of a model
	// before any row exists. A refusal here writes nothing and names every
	// offender; nothing attached (the field absent) is exactly today's start.
	attReceipts, attErr := d.startLaunchAttachments(p.Route, p.Attachments, p.Slug, p.WorkspaceRoot)
	if attErr != nil {
		return nil, attErr
	}
	// p.Attended crosses the socket as the caller's DECLARATION and is
	// passed through verbatim: the daemon neither infers it from socket
	// liveness nor supplies a default it was not given (D2). A client
	// that sends nothing gets never-recorded, not attended.
	var initial map[string]string
	if len(p.With) > 0 {
		initial = map[string]string{}
		for k, v := range p.With {
			initial[k] = v
		}
	}
	if strings.TrimSpace(p.Tech) != "" {
		if initial == nil {
			initial = map[string]string{}
		}
		initial["tech"] = strings.TrimSpace(p.Tech)
	}
	// --- S38 T13 (OL9): the start coordinates. Resolved BEFORE any row
	// exists — an id that resolves to nothing, or a requires clause with
	// nothing behind it, refuses here and leaves the database exactly
	// as it found it.
	var links loop.StartLinks
	startGoal, startTask, ferr := d.resolveLinks(p.GoalID, p.TaskID)
	if ferr != nil {
		return nil, ferr
	}
	goalText := p.Goal
	if startGoal != nil && strings.TrimSpace(p.Goal) == "" {
		goalText = d.goalPromptText(startGoal)
	}
	if startGoal != nil {
		links.GoalID = startGoal.ID
		v := startGoal.Version
		links.GoalVersion = &v
	}
	if bp, bpErr := d.loadStartBlueprint(p.Blueprint); bpErr == nil && len(bp.Requires) > 0 {
		res := loop.CheckRequires(bp, startGoal, startTask, d.candidatesFor(p.WorkspaceRoot))
		if !res.OK {
			pe := protocol.ErrRequiresUnmet.WithWhat("%s", res.Sentence)
			if len(res.Candidates) > 0 {
				parts := make([]string, 0, len(res.Candidates))
				for _, c := range res.Candidates {
					parts = append(parts, fmt.Sprintf("%s %s (%s)", c.Kind, c.ID, c.Title))
				}
				pe.Data.Todo = pe.Data.Todo + " — the workspace already holds: " + strings.Join(parts, ", ")
			}
			return nil, pe
		}
	}
	if res != nil || attReceipts != nil {
		// The receipt row: a human kickoff row carrying the names that
		// rode (skipped ones stay out — the row says what DID ride), and
		// since S77 the receipts of the attached files that rode beside
		// them. With no goal text there is no kickoff to write, and the
		// choice lives only in the cache for this process — a launch with
		// no words to receipt has nothing for the row to honestly carry
		// (recorded deviation, S68 tasks). For ATTACHMENTS that deviation
		// closes as a refusal: references filed on no body would be claims,
		// not receipts (S77).
		if x.FirstMessage != nil {
			x.FirstMessage.Skills = rodeNames(p.Skills, res)
			x.FirstMessage.Attachments = attReceipts
		} else if strings.TrimSpace(goalText) != "" {
			x.FirstMessage = &store.NewChatMessage{Author: "human", Kind: "note", Body: goalText,
				Source: "user", Skills: rodeNames(p.Skills, res), Attachments: attReceipts}
		} else if attReceipts != nil {
			return nil, protocol.ErrBadParams{Err: fmt.Errorf("attached files ride a launch's words: this start has no goal text to receipt them on — send the goal (or a goalId whose prompt speaks), or start without attachments")}
		}
	}
	// S55 T7 (E3): the start-time budget refusal — the only budget check a
	// loop that does not exist yet can trip (its own loop-scope budget is
	// not among them: the row is not there yet). It refuses BEFORE any row
	// exists (the nothing-half-written rule the link resolution above
	// follows), names the exhausted budget, and has no bypass flag: a cap
	// the daemon cannot read also refuses the start (fail toward stop), and
	// a cap that is reached is cleared by raising it, not by starting anyway.
	if verdict, berr := d.startBudgetVerdictFor(p.WorkspaceRoot, startEpicID(startGoal)); berr != nil {
		return nil, protocol.ErrLoopControlRefused.WithWhat(
			"loop start refused: the budgets could not be read (%v) — a start that cannot price itself does not run", berr)
	} else if !verdict.OK {
		hit := verdict.Hits[0]
		if err := d.store.RecordBudgetHit("", hit.BudgetID, hit.Dimension, hit.SpendFloor, hit.Cap, hit.Exact); err != nil {
			return nil, err
		}
		// S55 T8: the refused start is a screen event as well — the cap
		// the start tripped is named in the stream, over the workspace
		// root the start was aimed at (the loop does not exist yet, so it
		// carries no loopId).
		d.publishBudgetEvent(protocol.KindBudgetHit, p.WorkspaceRoot, map[string]any{
			"budgetId": hit.BudgetID, "scope": hit.Scope, "subjectId": hit.SubjectID,
			"dimension": hit.Dimension, "spendFloor": hit.SpendFloor, "cap": hit.Cap,
			"exact": hit.Exact,
		})
		remedy := "raise it (aeon budget set) or start later"
		if hit.Reason == "cost-unproven" {
			// Raising a cap cannot price unpriced work: the remedy for a
			// cost-unproven start is the unknowns being priced, or the
			// budget's unknown policy changing — say that, not the raise.
			remedy = "the unpriced work must be priced (or the budget's unknown policy changed) before a start can be proven under the cap"
		}
		return nil, protocol.ErrLoopControlRefused.WithWhat("%s — %s", hit.Sentence(), remedy)
	}
	st, err := loop.StartWith(ctx, d.store, store.NewLoop{Slug: slug, Title: shortTitle(goalText, titleMaxRunes), Goal: goalText, Root: p.WorkspaceRoot, ComponentRoot: p.WorkspaceRoot, Mode: mode, AllowOutward: p.AllowOutward,
		StartedBy: x.StartedBy, TriggerID: x.TriggerID, AutomationDepth: x.Depth, FirstMessage: x.FirstMessage, AutonomyPin: x.Autonomy}, links, p.Blueprint, p.Attended, initial, p.Route)
	if err != nil {
		return nil, err
	}
	if err := loop.SetModelOptions(ctx, d.store, st.ID, strings.TrimSpace(p.ReasoningEffort), p.Thinking); err != nil {
		return nil, err
	}
	if res != nil {
		d.rememberChosen(st.ID, chosenLaunch{Lines: res.Lines, Note: chosenSkipNote(res.Skipped)})
	}
	d.rememberLaunchAttachments(st.ID, attReceipts)
	// The worker link rides AFTER the row exists — a task_loop row
	// names a loop that is (the store checks), never one that might be.
	if startTask != nil {
		if err := d.store.LinkTaskToLoop(startTask.ID, store.Scope{LoopSlug: slug, ComponentRoot: p.WorkspaceRoot}, "worker"); err != nil {
			return nil, err
		}
	}
	d.publishLoop("loop.started", map[string]any{"loopId": st.ID, "blueprint": st.Blueprint.ID, "version": st.Blueprint.Version})
	// T4 (LC19): the runner is started on the daemon-lifetime loopCtx, so a
	// graceful shutdown's Drain can cancel it with ErrDaemonShutdown (a stop's
	// cause is set separately by Interrupt; the two are distinguishable).
	d.loopRunner.Start(d.loopCtx, st.ID)
	// A request longer than a title gets a short one made for it, by a small step of its own.
	if utf8.RuneCountInString(strings.Join(strings.Fields(goalText), " ")) > titleKeepAsIs {
		go d.titleLoop(st, goalText, p.Route)
	}
	return map[string]any{"id": st.ID, "slug": st.Slug, "blueprint": st.Blueprint.ID, "status": "running"}, nil
}
