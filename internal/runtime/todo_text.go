package runtime

// todo_text.go — S75 T4/T5: the loop's todo list as the runtime speaks it.
//
// Every sentence the runtime says about the list lives in this file, so the
// transcript's reminders are greppable and the compile, the nudge and the
// completion guard cannot drift apart.

import (
	"context"
	"fmt"
	"strings"
)

// TodoSection is the active list rendered for the model (TD7): the tool's own
// rendering, so the model reads the form it writes, plus the open and total
// counts for the step's opening note.
type TodoSection struct {
	Text  string
	Open  int
	Total int
}

// TodoRuleSentence closes the injected section: the rule the model works by.
const TodoRuleSentence = "This is your working todo list for this conversation. Update it with the todo tool as you work, mark items done when they are, and say so if you drop one."

// TodoSectionTitle is the system-half heading of the section.
const TodoSectionTitle = "Open todo list"

// body is the section's text: the render, then the rule.
func (s TodoSection) body() string {
	return strings.TrimRight(s.Text, "\n") + "\n\n" + TodoRuleSentence
}

// note is the step.started line (`todo: 3 open of 5`).
func (s TodoSection) note() string {
	return fmt.Sprintf("todo: %d open of %d", s.Open, s.Total)
}

// TodoAfterCompactionText is the runtime turn that restores the list after a
// compaction (S75 TD7, mid-step).
func TodoAfterCompactionText(rendered string) string {
	return "Your todo list, as it stands now (earlier turns were compacted):\n" + strings.TrimRight(rendered, "\n") + "\n\n" + TodoRuleSentence
}

// TodoNudgeText is the reminder the runtime appends when an agent has gone
// several model calls without touching an open list (TD9).
func TodoNudgeText(open []string) string {
	return fmt.Sprintf("Reminder: your todo list has %d open item(s) (%s). Update it, or say why you are leaving them.", len(open), strings.Join(open, ", "))
}

// TodoCompletionText is the reminder appended when a step tries to finish with
// items still open (TD10).
func TodoCompletionText(open []string) string {
	return fmt.Sprintf("Before you finish: your todo list still has %d open item(s) (%s). Mark each done, or say plainly why it is not.", len(open), strings.Join(open, ", "))
}

// TodoPolicy is the step's door to the nudge and the completion guard (TD9,
// TD10), bound to one step's loop by the daemon. The runtime imports neither the
// store nor the tools package: it asks, the adapter answers. Nil leaves the step
// exactly as it was before S75 (I2).
type TodoPolicy interface {
	// ActiveOpen lists the ids of the open items on the loop's active list; ok is
	// false when no list is active.
	ActiveOpen(ctx context.Context) (open []string, ok bool)
	// NudgeAfter is how many model calls without a `todo` call earn a reminder;
	// 0 turns the nudge off. Read fresh, so a setting change applies at once.
	NudgeAfter() int
	// GuardApplies reports whether a step may be sent back for finishing with
	// open items. A conversation turn (lane type converse) is exempt: it ends
	// every time it answers and its list is meant to outlive it.
	GuardApplies() bool
	// ActiveList renders the loop's active list the way the compile section does
	// (the tool's own renderer); ok is false when none is active. After a
	// compaction folds the transcript, the call that set the list is gone with
	// the rest of the earlier turns, so the list is put back in front of the
	// model — otherwise it re-plans from nothing and overwrites its own progress.
	ActiveList(ctx context.Context) (text string, ok bool)
}

// maxTodoBounces is how many times a step is sent back for finishing with open
// items before it completes anyway (TD10).
const maxTodoBounces = 2
