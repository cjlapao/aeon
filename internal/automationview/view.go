// Package automationview is S49's pure read side: the ONE formatter that
// describes an automation in words, the vocabulary the forms render from,
// the next-fire and match PREVIEWS, and the health composer. It is pure —
// no store, no clock, no environment, no files; the daemon (T3) reads the
// facts and hands them in. The rule that keeps it honest is that every
// preview RUNS the production code it previews: the schedule preview is
// internal/trigger's own walk (internal/trigger/preview.go, the engine's
// words), the match tester is the real grammar (internal/match), and the
// differential tests drive the real clock and the real dispatcher paths to
// prove the preview never lies about the engine.
package automationview

import (
	"fmt"
	"strings"
)

// View is what a person sees of one automation, from any family. T3 assembles
// it from rows; Describe renders it. Every string is data — Describe quotes
// each with %q, so control characters render visibly and the whole sentence
// stays one line.
type View struct {
	// Source is "schedule", "webhook" or "event" — the closed three.
	Source string
	ID     string
	// Expr/Zone/Window* are the schedule family; SecretEnv the webhook's
	// ENV NAME (never a value, AN1); Match the event family's parsed rule.
	Expr        string
	Zone        string
	WindowStart string
	WindowEnd   string
	WindowDays  string
	SecretEnv   string
	Match       MatchView
	Blueprint   string
	Workspace   string
	Goal        string
	// Event-family limits; 0 / no-cap means the clause is not printed.
	MaxRunsPerHour   int
	MaxCostPerDayUSD float64
	// State words are appended to any template, never a template change.
	Enabled  bool
	Paused   bool
	Archived bool
	// DisabledReason accompanies Enabled=false when the engine carries one.
	DisabledReason string
}

// MatchView is the event family's rule as parsed values (an absent list
// means the key was absent). Canonical order is the grammar's (AU6): the
// renderer prints keys in MatchKeyNames order, never map order.
type MatchView struct {
	Event      string
	Root       []string
	Component  []string
	GoalType   []string
	TaskType   []string
	SourceKind []string
}

// describeLimit is the length cap the task sets: a description longer than
// this stops being a description.
const describeLimit = 400

// Describe renders one automation as a single sentence, in the fixed
// template for its family, with state words appended. It is TOTAL: every
// family renders, hostile values arrive escaped, and an over-long render is
// cut with "…" rather than truncated into a half-word lie.
func Describe(v View) string {
	var b strings.Builder
	switch v.Source {
	case "schedule":
		b.WriteString(cronWords(v.Expr))
		fmt.Fprintf(&b, " in %q", v.Zone)
		if w := windowWords(v.WindowStart, v.WindowEnd, v.WindowDays); w != "" {
			fmt.Fprintf(&b, "; working hours %s", w)
		}
		writeStart(&b, v)
	case "webhook":
		b.WriteString("on a signed POST; secret from env ")
		fmt.Fprintf(&b, " %q", v.SecretEnv)
		writeStart(&b, v)
	case "event":
		b.WriteString("when ")
		fmt.Fprintf(&b, " %q", v.Match.Event)
		for _, k := range []struct {
			name string
			vals []string
		}{
			{"root", v.Match.Root}, {"component", v.Match.Component},
			{"goal_type", v.Match.GoalType}, {"task_type", v.Match.TaskType},
			{"source_kind", v.Match.SourceKind},
		} {
			if len(k.vals) > 0 {
				fmt.Fprintf(&b, " (%s in %s)", k.name, quotedList(k.vals))
			}
		}
		// The event template reads "when <event> (<keys>) starts blueprint
		// …" — one clause, no semicolon before the verb.
		writeStartSep(&b, v, " ")
		if v.MaxRunsPerHour > 0 {
			fmt.Fprintf(&b, "; at most %d runs/hour", v.MaxRunsPerHour)
		}
		if v.MaxCostPerDayUSD > 0 {
			fmt.Fprintf(&b, ", ≤ $%.2f/day", v.MaxCostPerDayUSD)
		}
	default:
		fmt.Fprintf(&b, "unknown automation %q (source %q is not one of schedule, webhook, event)", v.ID, v.Source)
	}
	// State words: appended, never a template change, and the engine's
	// reason rides along when there is one. Only the family that HAS the
	// switch says the word: "disabled" is the event family's (S20 speaks
	// paused, and archive speaks for both).
	if v.Source == "schedule" || v.Source == "webhook" || v.Source == "event" {
		if v.Archived {
			b.WriteString(" — archived")
			if v.DisabledReason != "" {
				fmt.Fprintf(&b, ": %q", v.DisabledReason)
			}
		} else if v.Source == "event" && !v.Enabled {
			b.WriteString(" — disabled")
			if v.DisabledReason != "" {
				fmt.Fprintf(&b, ": %q", v.DisabledReason)
			}
		}
		if v.Paused {
			b.WriteString(" — paused")
		}
	}
	out := strings.Join(strings.Fields(b.String()), " ") // one line, always
	if len(out) > describeLimit {
		cut := strings.LastIndex(out[:describeLimit-1], " ")
		if cut < describeLimit/2 {
			cut = describeLimit - 1
		}
		out = out[:cut] + "…"
	}
	return out
}

func writeStart(b *strings.Builder, v View) { writeStartSep(b, v, "; ") }

func writeStartSep(b *strings.Builder, v View, sep string) {
	fmt.Fprintf(b, "%sstarts blueprint %q", sep, v.Blueprint)
	if v.Workspace != "" {
		fmt.Fprintf(b, " in %q", v.Workspace)
	}
	if v.Goal != "" {
		fmt.Fprintf(b, " with goal %q", v.Goal)
	}
}

func quotedList(vals []string) string {
	q := make([]string, len(vals))
	for i, s := range vals {
		q[i] = fmt.Sprintf("%q", s)
	}
	return strings.Join(q, ", ")
}
