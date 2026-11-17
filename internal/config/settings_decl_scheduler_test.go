package config

// settings_decl_scheduler_test.go — S27 T8 (CN21): the four numbers the
// scheduler spends are declared, bounded and defaulted exactly as the
// slice's table says, and NOTHING in the registry turns the scheduler on.
// A knob without bounds is the daemon running whatever a fat-fingered
// settings.set typed; an authority behind a settings key is AU1's refusal
// with extra steps.

import (
	"strconv"
	"strings"
	"testing"
)

func TestSchedulerSettingsAreDeclaredAndBounded(t *testing.T) {
	reg := DefaultRegistry()
	type want struct {
		key, def string
		min, max int
	}
	wants := []want{
		{"scheduler.maxAgentSlots", "2", 1, 64},
		{"scheduler.tickSeconds", "15", 5, 300},
		{"scheduler.maxStartsPerRootPerHour", "6", 1, 60},
		{"scheduler.agingHours", "24", 1, 720},
	}
	for _, w := range wants {
		s, ok := reg.Lookup(w.key)
		if !ok {
			t.Fatalf("%s is not declared — the scheduler is reading a number through a key nobody owns", w.key)
		}
		if s.Type != TypeInt {
			t.Errorf("%s is %s, not an int count", w.key, s.Type)
		}
		if s.Default != w.def {
			t.Errorf("%s defaults to %q, want %q (the slice's D-table)", w.key, s.Default, w.def)
		}
		if s.Min == nil || *s.Min != w.min {
			t.Errorf("%s Min = %v, want %d", w.key, s.Min, w.min)
		}
		if s.Max == nil || *s.Max != w.max {
			t.Errorf("%s Max = %v, want %d", w.key, s.Max, w.max)
		}
		if s.Sensitive {
			t.Errorf("%s is Sensitive — it is a count, not a credential", w.key)
		}
		if len(s.Description) < 40 {
			t.Errorf("%s has a %d-character description; a person reads this sentence in the panel", w.key, len(s.Description))
		}
		// The bounds REFUSE: out-of-range is never clamped, never stored.
		for _, bad := range []string{"0", "-1", strconv.Itoa(w.max + 1), "not-a-number", "1.5"} {
			if _, err := s.ParseValue(bad); err == nil {
				t.Errorf("%s accepted %q — an unbounded knob is a knob that runs the machine", w.key, bad)
			}
		}
		for _, good := range []string{strconv.Itoa(w.min), w.def, strconv.Itoa(w.max)} {
			if _, err := s.ParseValue(good); err != nil {
				t.Errorf("%s refused %q: %v", w.key, good, err)
			}
		}
	}
	// The tick's own floor: a five-second tick is the minimum patience.
	if s, ok := reg.Lookup("scheduler.tickSeconds"); ok {
		if _, err := s.ParseValue("4"); err == nil {
			t.Error("a 4-second tick was accepted — the floor is 5")
		}
	}
}

// TestNoAuthorityKeyInTheRegistry names CN21 at the registry: no declared
// key enables, pauses, or otherwise moves the scheduler's AUTHORITY. The
// authority lives in scheduler_state / scheduler_root and is written only
// through the eight human-door controls. A future `scheduler.enabled` key
// would move standing authority behind a generic door that stamps no
// reasoning — which is exactly what this test exists to make impossible.
func TestNoAuthorityKeyInTheRegistry(t *testing.T) {
	authorityWords := []string{"enabled", "enable", "disabled", "disable", "paused", "pause",
		"master", "authority", "stop"}
	for _, s := range DefaultRegistry().All() {
		if !strings.HasPrefix(s.Key, "scheduler.") {
			continue
		}
		low := strings.ToLower(s.Key)
		for _, a := range authorityWords {
			if strings.Contains(low, a) {
				t.Errorf("%s declares an authority word (%s) — the scheduler's master and repositories are authority records, not settings (CN21, AU1)", s.Key, a)
			}
		}
	}
	// And the four that DO exist are the four the slice declared — a fifth
	// key joining the area is a decision this package must see.
	got := 0
	for _, s := range DefaultRegistry().All() {
		if strings.HasPrefix(s.Key, "scheduler.") {
			got++
		}
	}
	if got != 4 {
		t.Errorf("the scheduler area declares %d keys, want exactly 4 (maxAgentSlots, tickSeconds, maxStartsPerRootPerHour, agingHours)", got)
	}
}
