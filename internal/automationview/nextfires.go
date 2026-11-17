package automationview

import (
	"time"

	"github.com/cjlapao/aeon/internal/trigger"
)

// nextfires.go — the schedule preview. It WRAPS internal/trigger's own walk
// (trigger.PreviewFires, which runs the same expr.Next / collapseDoubled /
// findMissing / InWindow the clock runs and speaks dst.go's sentences
// verbatim); this file only shapes the spec in and the instants out. A
// second implementation of cron arithmetic would be the second opinion the
// design forbids.

// previewMax is the horizon cap, taken from the engine — one number.
const previewMax = trigger.PreviewLimit

// ScheduleSpec is the preview's input: the schedule family's timing fields,
// nothing else.
type ScheduleSpec struct {
	ID          string
	Expr        string
	Zone        string
	WindowStart string
	WindowEnd   string
	WindowDays  string
}

// FireInstant is one previewed instant. Kind is "due" | "window" |
// "dst-collapsed" | "dst-missing"; Note is the engine's own reason sentence
// verbatim ("" on a plain due instant). "due" promises the clock will ASK,
// not that a loop will start — overlap, governance and the starter belong
// to the instant, not the preview.
type FireInstant struct {
	At   time.Time
	Kind string
	Note string
	Wall string
}

// NextFires previews the next n instants strictly after `from` (n ≤ 20).
// Errors pass through from the engine's loader/validator.
func NextFires(s ScheduleSpec, from time.Time, n int) ([]FireInstant, error) {
	inst, err := trigger.PreviewFires(trigger.Schedule{
		ID:          s.ID,
		Kind:        trigger.KindSchedule,
		Expr:        s.Expr,
		Zone:        s.Zone,
		WindowStart: s.WindowStart,
		WindowEnd:   s.WindowEnd,
		WindowDays:  s.WindowDays,
	}, from, n)
	if err != nil {
		return nil, err
	}
	out := make([]FireInstant, len(inst))
	for i, p := range inst {
		out[i] = FireInstant{At: p.At, Kind: string(p.Kind), Note: p.Note, Wall: p.Wall}
	}
	return out, nil
}
