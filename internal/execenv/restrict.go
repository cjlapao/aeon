package execenv

// "No weaker on any axis" (EV16), as a pure function.
//
// This is the validator behind the one thing an agent is allowed to ask for:
// `environment_propose_restrict` may only propose a NARROWER environment. The
// rule cannot be "does the proposal look reasonable" — it is a total function
// over two specs that returns every way the new one is looser, and an empty
// result is the only answer that lets a proposal stand.
//
// It is reflexive and transitive by construction (each axis is a pointwise
// comparison against a fixed order), which is what lets a chain of two
// approved restricts be trusted to be a restrict: tests assert both, over
// generated specs, because an order bug that only shows on the third element
// of a chain is exactly the bug a review misses.

import (
	"fmt"
	"sort"
	"strings"
)

// Widening names one axis and the member that got looser. The axis strings are
// a closed vocabulary so the refusal a person reads matches the axis the panel
// highlights.
type Widening struct {
	Axis   string `json:"axis"`
	Member string `json:"member"`
}

// The axes. `spec.` prefixes keep them distinct from the backend axis, and
// every one of them is an EV16 line.
const (
	AxisPassthrough = "spec.passthrough"
	AxisSet         = "spec.set"
	AxisSecrets     = "spec.secrets"
	AxisServices    = "spec.services"
	AxisLimits      = "spec.limits"
	AxisEgress      = "spec.egress"
	AxisImage       = "spec.image"
	AxisHome        = "spec.home"
	AxisPreview     = "spec.preview"
	AxisBackend     = "backend"
)

// Sentence renders the widening the way a refusal and the panel both print it.
func (w Widening) Sentence() string {
	if w.Member == "" {
		return w.Axis
	}
	return w.Axis + " · " + w.Member
}

// Restricts returns every way `newSpec` is looser than `oldSpec`. Empty means
// the new environment is no weaker on any axis, which is EV16's condition for
// an agent's restrictive proposal to be creatable at all.
func Restricts(oldSpec, newSpec Spec, oldBackend, newBackend string) []Widening {
	var out []Widening

	// passthrough: every name the new environment passes through must already
	// be passed through, and a pattern counts as looser than any name it covers
	// — `LC_*` where the old side had `LC_ALL` lets the next LC_ variable in.
	for _, n := range newSpec.Env.Passthrough {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		if !passthroughCovers(oldSpec.Env.Passthrough, n) {
			out = append(out, Widening{Axis: AxisPassthrough, Member: n})
		}
	}

	// set: a new key is new reach; a changed value is a different value, and
	// there is no honest ordering over arbitrary strings, so any change counts.
	for name, value := range newSpec.Env.Set {
		oldValue, had := oldSpec.Env.Set[name]
		if !had {
			out = append(out, Widening{Axis: AxisSet, Member: name})
			continue
		}
		if oldValue != value {
			out = append(out, Widening{Axis: AxisSet, Member: name + " (value changed)"})
		}
	}

	for _, ref := range newSpec.Env.Secrets {
		if !contains(oldSpec.Env.Secrets, ref) {
			out = append(out, Widening{Axis: AxisSecrets, Member: ref})
		}
	}

	for _, s := range newSpec.Services {
		match, had := findService(oldSpec.Services, s.Name)
		if !had {
			out = append(out, Widening{Axis: AxisServices, Member: s.Name})
			continue
		}
		if match.Image != s.Image || match.Port != s.Port ||
			canonicalStringMap(match.Env) != canonicalStringMap(s.Env) ||
			strings.Join(sortedCopy(match.Secrets), ",") != strings.Join(sortedCopy(s.Secrets), ",") {
			out = append(out, Widening{Axis: AxisServices, Member: s.Name + " (definition changed)"})
		}
	}

	// Limits: higher is looser, and ABSENT is the loosest of all — so a limit
	// the old spec did not have may not disappear, and one it did have may not
	// grow. Adding a limit where none existed is a tightening, not a widening.
	out = append(out, limitWidenings("wallSeconds", oldSpec.Limits.WallSeconds, newSpec.Limits.WallSeconds)...)
	out = append(out, limitWidenings("memoryMiB", oldSpec.Limits.MemoryMiB, newSpec.Limits.MemoryMiB)...)
	out = append(out, limitFloatWidenings("cpus", oldSpec.Limits.CPUs, newSpec.Limits.CPUs)...)

	// Egress: rank, not name. `unenforced` and `unrestricted` sit at the same
	// loose end and `none` is the tight one.
	if egressRank(newSpec.Egress.Mode) < egressRank(oldSpec.Egress.Mode) {
		out = append(out, Widening{Axis: AxisEgress,
			Member: oldSpec.Egress.Mode + " -> " + newSpec.Egress.Mode})
	}
	if len(newSpec.Egress.Allow) > 0 && len(oldSpec.Egress.Allow) == 0 {
		out = append(out, Widening{Axis: AxisEgress, Member: "egress.allow added"})
	}

	// Backend strength.
	if BackendStrength(newBackend) < BackendStrength(oldBackend) {
		out = append(out, Widening{Axis: AxisBackend,
			Member: oldBackend + " -> " + newBackend})
	}

	// The identity fields: any change here is a different environment, and an
	// agent proposal has no business swapping the image a loop builds in.
	if canonicalImage(oldSpec.Image) != canonicalImage(newSpec.Image) {
		out = append(out, Widening{Axis: AxisImage, Member: "image changed"})
	}
	if oldSpec.Home != newSpec.Home {
		out = append(out, Widening{Axis: AxisHome, Member: oldSpec.Home + " -> " + newSpec.Home})
	}
	if (oldSpec.Preview == nil) != (newSpec.Preview == nil) ||
		(oldSpec.Preview != nil && newSpec.Preview != nil && *oldSpec.Preview != *newSpec.Preview) {
		out = append(out, Widening{Axis: AxisPreview, Member: "preview changed"})
	}

	return out
}

// passthroughCovers answers: does the old list already allow this entry?
func passthroughCovers(old []string, entry string) bool {
	for _, o := range old {
		o = strings.TrimSpace(o)
		if o == entry {
			return true
		}
		if strings.HasSuffix(o, "*") && strings.HasSuffix(entry, "*") {
			// new `LC_*` is covered only by an old pattern at least as broad.
			if strings.HasPrefix(strings.TrimSuffix(entry, "*"), strings.TrimSuffix(o, "*")) {
				return true
			}
			continue
		}
		if strings.HasSuffix(o, "*") && !strings.HasSuffix(entry, "*") {
			if strings.HasPrefix(entry, strings.TrimSuffix(o, "*")) {
				return true
			}
		}
	}
	return false
}

func limitWidenings(name string, oldVal, newVal *int) []Widening {
	switch {
	case oldVal == nil && newVal == nil:
		return nil
	case oldVal != nil && newVal == nil:
		return []Widening{{Axis: AxisLimits, Member: name + " limit removed"}}
	case oldVal == nil && newVal != nil:
		return nil // adding a limit is tighter
	case *newVal > *oldVal:
		return []Widening{{Axis: AxisLimits,
			Member: fmt.Sprintf("%s %d -> %d", name, *oldVal, *newVal)}}
	}
	return nil
}

func limitFloatWidenings(name string, oldVal, newVal *float64) []Widening {
	switch {
	case oldVal == nil && newVal == nil:
		return nil
	case oldVal != nil && newVal == nil:
		return []Widening{{Axis: AxisLimits, Member: name + " limit removed"}}
	case oldVal == nil && newVal != nil:
		return nil
	case *newVal > *oldVal+1e-9:
		return []Widening{{Axis: AxisLimits,
			Member: fmt.Sprintf("%s %v -> %v", name, *oldVal, *newVal)}}
	}
	return nil
}

func findService(svcs []Service, name string) (Service, bool) {
	for _, s := range svcs {
		if s.Name == name {
			return s, true
		}
	}
	return Service{}, false
}

func canonicalStringMap(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Sorted, then built: a map's iteration order is not a comparison.
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteString("=")
		b.WriteString(m[k])
		b.WriteString(";")
	}
	return b.String()
}

func canonicalImage(img *Image) string {
	if img == nil {
		return ""
	}
	return img.Ref + "|" + img.Pull
}

// DescribeTightening renders what an environment_restrict proposal actually
// takes away — the axes that get STRONGER — in the same words `Widening`
// renders the loosenings, because there is one formatter for this order and
// a second renderer would be a second truth.
//
// It is `Restricts` run the other way: the ways the CURRENT spec is looser
// than the proposed one are exactly the axes the proposal tightens (the
// order is reflexive and transitive, so running it inverted is honest
// rather than a parallel implementation of "tighter"). An empty answer is
// stated, never silently omitted: a proposal that tightens nothing is a
// no-op, and EV16's rule says a no-op writes no row and no audit entry.
//
// The backend is passed once and compared with itself: an agent's proposal
// carries no backend (a strength change is a person's act), so the backend
// axis can never appear here.
func DescribeTightening(oldSpec, newSpec Spec, backend string) string {
	ways := Restricts(newSpec, oldSpec, backend, backend)
	if len(ways) == 0 {
		return "no axis tightens"
	}
	parts := make([]string, 0, len(ways))
	for _, w := range ways {
		parts = append(parts, w.Sentence())
	}
	return strings.Join(parts, "; ")
}
