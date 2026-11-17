package scorecard

// component.go — the tree half of the component card (RT8): the card is
// the roll-up over the node's tree (a `component_root IN (…)` over the
// S44-extra resolved roots), `includeChildren=false` is the node alone,
// and the breakdown lists each CHILD's loops/done/blocked counts only —
// counts, never scores (RT11: nothing here can be ranked).

func (c *Card) onComponent(p plan, u unit) {
	if len(p.children) == 0 {
		return
	}
	// Group the card's unit by root: loops are the unit rows, done is
	// this row's kind, blocked follows the EVER rule (same truth the
	// card itself answers with — one rule, two views).
	type acc struct{ loops, done, blocked int }
	byRoot := map[string]*acc{}
	for _, o := range u.rows {
		a := byRoot[o.ComponentRoot]
		if a == nil {
			a = &acc{}
			byRoot[o.ComponentRoot] = a
		}
		a.loops++
		if isDone(o) {
			a.done++
		}
		if isBlocked(o) || u.everBlockedFor(o) {
			a.blocked++
		}
	}
	for _, child := range p.children {
		if child.Root == "" {
			continue
		}
		a := byRoot[child.Root]
		if a == nil {
			a = &acc{}
		}
		c.Breakdown = append(c.Breakdown, BreakdownEntry{
			Component: child.ID, Root: child.Root,
			Loops: a.loops, Done: a.done, Blocked: a.blocked,
		})
	}
}
