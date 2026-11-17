package daemon

// chosen_skills.go — S68 T3: the CHOSEN skills' daemon side (B6).
//
// The offer (S66) tells the model what skills EXIST; this half honours the
// operator's explicit CHOICE. One resolver answers all three doors
// (`loop.start`, `loop.message`, `step.steer`), and the rules are B6's:
//
//   - unknown name ⇒ the CALL is refused, naming every offender. A chosen
//     skill that does not exist must never read as one that rode;
//   - known but unpickable at send (invalid, shadowed) ⇒ skipped and
//     REPORTED — the receipt says what actually rode;
//   - the choice is turn-scoped: the names live on the human row, the lines
//     live in the per-launch cache, and a restart (a new launch) forgets
//     both. Nothing chosen sticks to a loop.
//
// # Why the cache exists, and why it is not the truth
//
// `step_started` events and post-restart dispatch must agree about what
// rode (B8's replay honesty). The DURABLE truth is the rows: the kickoff
// row carries the launch's names, the thread row carries a turn's names,
// the steer row carries a steer's. The cache is the restart-cheap fast path
// over that truth; a daemon restart reloads a launch's lines from its own
// kickoff row rather than rendering a step without them.

import (
	"fmt"

	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// chosenLaunch is what one launch rode: the canonical lines in chosen
// order, plus the opening note naming the skills that were CHOSEN but could
// not ride (unpickable at send). Empty note means nothing was skipped.
type chosenLaunch struct {
	Lines []string
	Note  string
}

// chosenForLoop answers "what did this launch choose", consulting the
// in-memory cache first and the durable kickoff row after a restart. A
// launch that chose nothing returns nil — every caller renders nothing for
// nil, which is the pre-S68 shape.
func (d *Daemon) chosenForLoop(st loop.LoopState) *chosenLaunch {
	if d == nil || d.store == nil {
		return nil
	}
	d.chosenMu.Lock()
	c, ok := d.chosen[st.ID]
	d.chosenMu.Unlock()
	if ok {
		if len(c.Lines) == 0 {
			return nil
		}
		return &c
	}
	// The cache miss IS a restart (a live launch always holds its entry).
	// Rebuild from THIS launch's kickoff row — never from an earlier run's:
	// skills chosen for a previous launch are that launch's history (B6).
	names := d.launchChosenNames(st)
	if len(names) == 0 {
		// Cache the absence too: a loop that chose nothing must not re-read
		// the thread on every step dispatch.
		d.rememberChosen(st.ID, chosenLaunch{})
		return nil
	}
	res, err := d.resolveChosenSkills(names)
	if err == nil && res == nil {
		err = fmt.Errorf("the stored choice resolved to nothing")
	}
	if err != nil {
		// The catalog moved since the choice (a skill was deleted, the root
		// unmounted). A REPLAY must not die over it — and it must not lie
		// either: the note says the chosen skills did not ride.
		d.rememberChosen(st.ID, chosenLaunch{Note: fmt.Sprintf("chosen skills could not be re-attached after restart: %v", err)})
		d.log.Infof("chosen skills for loop %s could not be re-attached after restart: %v", st.ID, err)
		return nil
	}
	c = chosenLaunch{Lines: res.Lines, Note: chosenSkipNote(res.Skipped)}
	d.rememberChosen(st.ID, c)
	return &c
}

// launchChosenNames reads the names off the row that IS this launch: the
// current run's linked kickoff row when the run row has one, else the
// thread's first human row (the socket start with chosen skills writes
// exactly that). Any other row is somebody else's turn, not the launch's.
func (d *Daemon) launchChosenNames(st loop.LoopState) []string {
	sc := store.Scope{LoopSlug: st.Slug, ComponentRoot: st.ComponentRoot}
	if run, err := d.store.CurrentRun(st.ID); err == nil && run.KickoffMessageID != nil {
		msg, err := d.store.ReadChatMessage(*run.KickoffMessageID)
		if err == nil && msg.Author == "human" {
			return msg.Skills
		}
		return nil
	}
	rows, err := d.store.Thread(sc, 0)
	if err != nil {
		return nil
	}
	for _, m := range rows {
		if m.Author == "human" {
			return m.Skills
		}
	}
	return nil
}

// rememberChosen stores (or clears, with a zero value) what a launch rode.
func (d *Daemon) rememberChosen(loopID string, c chosenLaunch) {
	d.chosenMu.Lock()
	defer d.chosenMu.Unlock()
	if d.chosen == nil {
		d.chosen = map[string]chosenLaunch{}
	}
	d.chosen[loopID] = c
}

// forgetChosen is the restart door: a restart is a NEW launch, and the old
// launch's choice does not cross into it (B6, turn-scoped).
func (d *Daemon) forgetChosen(loopID string) {
	d.chosenMu.Lock()
	defer d.chosenMu.Unlock()
	delete(d.chosen, loopID)
}

// chosenSkipNote renders the skipped names for the step's opening line.
// Empty in ⇒ empty out, which is what keeps the no-skills wire identical.
func chosenSkipNote(skipped []string) string {
	if len(skipped) == 0 {
		return ""
	}
	return "chosen skills skipped (unpickable at send): " + joinNames(skipped)
}

// joinNames joins with commas; tiny helper so the note renders one way.
func joinNames(xs []string) string {
	out := ""
	for i, x := range xs {
		if i > 0 {
			out += ", "
		}
		out += x
	}
	return out
}

// chosenSkillParams attaches a launch's chosen lines to step params — the
// ONLY place lines reach the wire. Nothing chosen, nothing attached: the
// params stay byte-identical to every pre-S68 step.
func (d *Daemon) chosenSkillParams(st loop.LoopState, params *protocol.StepParams) {
	c := d.chosenForLoop(st)
	if c == nil {
		return
	}
	params.Skills = c.Lines
	params.ChosenSkillsNote = c.Note
}
