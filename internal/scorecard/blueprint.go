package scorecard

// blueprint.go — the per-blueprint plane: completionByVersion (RT7's
// "rates split by blueprint_version"). One metric, its Detail a small
// table of {version: {loops, done}}; the card's overall value is the
// done rate over rows that carry a version. Two versions of a blueprint
// never blend into one number pretending to be one.

import "github.com/cjlapao/aeon/internal/store"

func (c *Card) onBlueprint(rows []store.RetroOutcome) {
	type vl struct{ loops, done int }
	byVer := map[string]vl{}
	total, done, n := 0, 0, 0
	for _, o := range rows {
		if o.BlueprintVersion == nil {
			continue
		}
		key := itoa(*o.BlueprintVersion)
		v := byVer[key]
		v.loops++
		n++
		total++
		if isDone(o) {
			v.done++
			done++
		}
		byVer[key] = v
	}
	m := metric(n, 0, ratio(float64(done), float64(total)))
	if n > 0 {
		table := map[string]any{}
		for k, v := range byVer {
			table[k] = map[string]any{"loops": v.loops, "done": v.done}
		}
		m.Detail = map[string]any{"byVersion": table}
	}
	c.Metrics["completionByVersion"] = m
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
