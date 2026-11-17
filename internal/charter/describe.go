package charter

import "fmt"

// Revision is the service's view of one stored charter revision: the row's
// audit fields plus its body, parsed. It is what every read answers with,
// so a client never parses the body string to count things.
type Revision struct {
	ID             string `json:"id"`
	RootID         string `json:"rootId"`
	Revision       int    `json:"revision"`
	Body           Body   `json:"body"`
	Summary        string `json:"summary"`
	ActorKind      string `json:"actorKind"`
	ActorID        string `json:"actorId"`
	Source         string `json:"source"`
	SourceRef      string `json:"sourceRef"`
	CreatedAt      string `json:"createdAt"`
	TotalRevisions int    `json:"totalRevisions"`
}

// Description is the ONE formatter's answer (PM5 shape): a fixed summary
// line plus fixed template lines. SEC-033 is the law of this file — every
// word here is built from ids, enums and counts; a free-text field from the
// body never travels into it, because a charter written by an interviewed
// person can contain instructions, and the audit trail is not a place to
// render them (GI18). The client shows the BODY itself as data, from the
// Revision, in its own field.
type Description struct {
	Summary string   `json:"summary"`
	Lines   []string `json:"lines"`
}

// summaryTemplate is Behaviour 3's line — ids and enums only, the one
// spelling of a revision's audit headline.
func summaryTemplate(r Revision) string {
	return fmt.Sprintf("charter r%d by %s:%s (%s)", r.Revision, r.ActorKind, r.ActorID, r.Source)
}

// Describe renders a revision for any surface that must SUMMARIZE rather
// than show: proposal cards, ledger lines, the list view's summary column.
func Describe(r Revision) Description {
	b := r.Body
	d := Description{
		Summary: summaryTemplate(r),
		Lines: []string{
			fmt.Sprintf("statement of %d bytes; %d %s, %d %s, %d acceptance, %d stack %s",
				len(r.Body.Goal),
				len(b.Constraints), plural(len(b.Constraints), "constraint", "constraints"),
				len(b.NonGoals), plural(len(b.NonGoals), "non-goal", "non-goals"),
				len(b.Acceptance),
				len(b.Stack), plural(len(b.Stack), "tag", "tags")),
		},
	}
	if n := countedChecks(b); n > 0 {
		d.Lines = append(d.Lines, fmt.Sprintf("acceptance carries %d checked %s", n, plural(n, "check", "checks")))
	}
	if p := b.Proposed; p != nil {
		if p.BudgetUSD != nil {
			d.Lines = append(d.Lines, fmt.Sprintf("proposed: budget %s USD — not applied (a loop budget is S55's human door)", formatNum(*p.BudgetUSD)))
		}
		if p.Autonomy != "" {
			d.Lines = append(d.Lines, fmt.Sprintf("proposed: autonomy %q — not applied (a grant is S56's human door)", p.Autonomy))
		}
		if p.EnvironmentRef != "" {
			d.Lines = append(d.Lines, "proposed: an environment reference is named — not applied (S59's human door shows what it names)")
		}
		if n := len(p.Verify); n > 0 {
			d.Lines = append(d.Lines, fmt.Sprintf("proposed: %d verify command%s — never executed here (S57's gate is the door)", n, plural(n, "", "s")))
		}
	}
	return d
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func countedChecks(b Body) int {
	n := 0
	for _, a := range b.Acceptance {
		if a.Check != "" {
			n++
		}
	}
	return n
}
