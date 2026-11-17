package history

// ════════════════════════════════════════════════════════════════════════════
// Describe (S53 T1, HV2): the ONE formatter that renders a history row with its
// details — lane label, outcome word, duration, cost with its honesty state,
// and links as DATA. The row (row.go) is the flat line every surface already
// renders; Describe is the same entry's details, produced from the same Entry
// by this one function, so the socket, the CLI and the History screen cannot
// phrase a row differently — the house's one-formatter law (Verdict.Sentence,
// Render, automationview.Describe) applied to history.
//
// HV2's three load-bearing rules, each proven by a named test:
//   - the outcome word is the STORED status verbatim (S25's own law: no surface
//     re-phrases an outcome);
//   - the cost carries its honesty state — known / partial / unknown — never
//     collapsed (S33's three-state rule, read off Rollup by CostHonesty below);
//   - links are {kind, id} data, never a sentence a client has to parse.
// ════════════════════════════════════════════════════════════════════════════

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// Link kinds (HV2): the four things a history row can point at. The set is
// closed; an id the entry does not carry produces NO link — never a link to an
// id nobody has.
const (
	LinkLoop    = "loop"
	LinkTask    = "task"
	LinkGoal    = "goal"
	LinkRoadmap = "roadmap"
)

// Link is one pointer out of a history row: what kind of record, and its id,
// as data.
type Link struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// Links is the link block of a Described: the four kinds in the spec's own
// order (loop, task, goal, roadmap), each present or zero. It is an array of
// exactly four so a Described stays comparable with == — the S25 parity gate
// (TestHistorySocketAndCLIProduceIdenticalRows) compares whole rows between the
// two doors, and an additive field must not break the gate that judges it. On
// the wire it projects to a JSON array of the links that EXIST — a client
// walks `links` and never sees an empty slot — via the (un)marshal below, so
// the Go shape is comparable and the wire shape is the data the spec names.
type Links [4]Link

// linkOrder is the canonical position of each kind (HV2's loop|task|goal|roadmap).
var linkOrder = []string{LinkLoop, LinkTask, LinkGoal, LinkRoadmap}

// linkIndex returns the position of a kind, or -1 for a kind the set excludes.
func linkIndex(kind string) int {
	for i, k := range linkOrder {
		if k == kind {
			return i
		}
	}
	return -1
}

// set places one link at its kind's position. An empty id sets nothing: an
// entry that does not carry the id carries no link, never a link to a hole.
func (ls *Links) set(kind, id string) {
	if i := linkIndex(kind); i >= 0 && id != "" {
		ls[i] = Link{Kind: kind, ID: id}
	}
}

// MarshalJSON projects the links that exist, in canonical order, as a JSON
// array — an empty block is [], never null and never a row of empty slots.
func (ls Links) MarshalJSON() ([]byte, error) {
	out := make([]Link, 0, len(linkOrder))
	for _, l := range ls {
		if l != (Link{}) {
			out = append(out, l)
		}
	}
	if len(out) == 0 {
		return []byte("[]"), nil
	}
	return json.Marshal(out)
}

// UnmarshalJSON reads a links array back into its canonical positions; a kind
// outside the four is refused by name, not dropped in silence.
func (ls *Links) UnmarshalJSON(raw []byte) error {
	var in []Link
	if err := json.Unmarshal(raw, &in); err != nil {
		return err
	}
	*ls = Links{}
	for _, l := range in {
		i := linkIndex(l.Kind)
		if i < 0 {
			return fmt.Errorf("history: link kind %q is not one of loop, task, goal, roadmap", l.Kind)
		}
		ls[i] = l
	}
	return nil
}

// The cost honesty states (S33): the three states a floored cost can be in,
// read off a Rollup by CostHonesty and never collapsed.
const (
	CostKnown   = "known"
	CostPartial = "partial"
	CostUnknown = "unknown"
)

// CostHonesty is the ONE reading of a Rollup's honesty state:
// known   ⇔ !Floor — every contributing step was known, the number is a total;
// partial ⇔ Floor && UnknownCount == 0 — the number is a lower bound built
//
//	from partial floors (a partial HAS a floor, it is just not a total);
//
// unknown ⇔ UnknownCount > 0 — at least one contributing step had no number,
//
//	and the count travels with it.
//
// The three are never collapsed into a boolean: a partial and an unknown floor
// differently (rollup.go's addStep keeps them apart on purpose).
func CostHonesty(r Rollup) string {
	switch {
	case r.UnknownCount > 0:
		return CostUnknown
	case r.Floor:
		return CostPartial
	default:
		return CostKnown
	}
}

// Described is one history entry rendered with its details, through the ONE
// formatter. Every field is derived from the Entry — Describe reads no store,
// issues no second query, and invents nothing the entry does not carry.
type Described struct {
	// Lane is the entry's own kind-word — "live" or "archived" (Kind.String),
	// the label the merged sequence tags each row with.
	Lane string `json:"lane"`
	// Outcome is the STORED status, verbatim (S25 design: no surface re-phrases
	// an outcome). It is a vocabulary word for everything the engine writes and
	// an honest unknown word for anything older — never collapsed.
	Outcome string `json:"outcome"`
	// Duration is the human span from started_at to the entry's timeline;
	// "" when either end is missing or unparseable — the honest absence,
	// never a zero read as "no time passed".
	Duration string `json:"duration,omitempty"`
	// Cost is Rollup.Render verbatim — the floor, never a bare number (H6).
	Cost string `json:"cost"`
	// CostState is the cost's honesty state (CostKnown/CostPartial/CostUnknown),
	// as data beside the rendered string.
	CostState string `json:"costState"`
	// Reason is the stored reason of a blocked loop, verbatim (D4/H4), empty
	// when the loop is not blocked or the reason was never recorded.
	Reason string `json:"reason,omitempty"`
	// Links are the pointers out of the row — loop, task, goal, roadmap — as
	// data. An entry that does not carry an id carries no link: an archived
	// loop's header names no task and no goal, so it links to its loop alone.
	Links Links `json:"links"`
}

// Describe is the ONE conversion from an Entry to its details (HV2). A second
// formatter anywhere is a second truth.
func Describe(e Entry) Described {
	d := Described{
		Lane:      e.Kind.String(),
		Outcome:   e.Status,
		Duration:  durationBetween(e.StartedAt, e.Timeline),
		Cost:      e.Cost.Render(),
		CostState: CostHonesty(e.Cost),
		Reason:    e.Reason,
	}
	// The loop link is never absent: every entry IS a loop (or the archive of
	// one), named by its own id. The other three ride the entry's own
	// coordinates — an id the entry does not carry sets no link.
	d.Links.set(LinkLoop, e.ID)
	d.Links.set(LinkTask, e.NowTask)
	d.Links.set(LinkGoal, e.GoalID)
	d.Links.set(LinkRoadmap, e.RoadmapID)
	return d
}

// durationBetween renders the span between two RFC3339 stamps as compact human
// words (2d 4h, 3h 12m, 45s). "" when either stamp is missing or unparseable,
// or when the span is negative — every one of those is an honest absence,
// never a fabricated "0s".
func durationBetween(started, ended string) string {
	if started == "" || ended == "" {
		return ""
	}
	st, err := time.Parse(time.RFC3339, started)
	if err != nil {
		return ""
	}
	en, err := time.Parse(time.RFC3339, ended)
	if err != nil {
		return ""
	}
	d := en.Sub(st)
	if d < 0 {
		return ""
	}
	if d < time.Minute {
		return strconv.FormatInt(int64(d.Seconds()), 10) + "s"
	}
	if d < 48*time.Hour {
		h := int64(d.Hours())
		m := int64(d.Minutes()) - h*60
		if m == 0 {
			return strconv.FormatInt(h, 10) + "h"
		}
		return strconv.FormatInt(h, 10) + "h " + strconv.FormatInt(m, 10) + "m"
	}
	days := int64(d.Hours()) / 24
	h := int64(d.Hours()) - days*24
	if h == 0 {
		return strconv.FormatInt(days, 10) + "d"
	}
	return strconv.FormatInt(days, 10) + "d " + strconv.FormatInt(h, 10) + "h"
}
