package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/runtime"
)

func intp(n int) *int { return &n }

func TestTheLayersResolveAgentOverStepOverSettingAndZeroMeansNoBound(t *testing.T) {
	ctx := context.Background()
	in := func(agent string, step *int) runtime.CompileInput {
		c := runtime.CompileInput{StepMaxIterations: step}
		c.Handler.Name = agent
		return c
	}
	r := StepRunner{
		MaxIterations: func(context.Context) int { return 50 },
		AgentMaxIterations: func(agent string) (int, bool) {
			switch agent {
			case "bounded":
				return 9, true
			case "free":
				return 0, true // explicitly no bound
			}
			return 0, false
		},
	}
	for name, c := range map[string]struct {
		in   runtime.CompileInput
		want int
	}{
		"agent beats the step":            {in("bounded", intp(30)), 9},
		"an agent's 0 beats a step bound": {in("free", intp(30)), -1},
		"the step beats the setting":      {in("none", intp(30)), 30},
		"a step's 0 means no bound":       {in("none", intp(0)), -1},
		"the setting is the floor":        {in("none", nil), 50},
	} {
		if got := r.resolveMaxIterations(ctx, c.in); got != c.want {
			t.Errorf("%s: got %d, want %d", name, got, c.want)
		}
	}
	// A setting of 0 (the default) is no bound; no resolver at all keeps the compiled default.
	zero := StepRunner{MaxIterations: func(context.Context) int { return 0 }}
	if got := zero.resolveMaxIterations(ctx, in("x", nil)); got != -1 {
		t.Errorf("a setting of 0 is no bound, got %d", got)
	}
	if got := (StepRunner{}).resolveMaxIterations(ctx, in("x", nil)); got != 0 {
		t.Errorf("a daemon with nothing to ask keeps the compiled default, got %d", got)
	}
}

func TestAnAgentsBoundIsReadOffItsMetadataAndNothingElseIsGuessed(t *testing.T) {
	for blob, want := range map[string][2]int{
		`{"maxIterations": 25}`:   {25, 1},
		`{"maxIterations": 0}`:    {0, 1},
		`{"other": 1}`:            {0, 0},
		``:                        {0, 0},
		`not json`:                {0, 0},
		`{"maxIterations": -3}`:   {0, 0},
		`{"maxIterations": "12"}`: {0, 0},
	} {
		n, ok := maxIterationsFromMetadata(blob)
		if n != want[0] || (ok && want[1] == 0) || (!ok && want[1] == 1) {
			t.Errorf("%q: got (%d,%v), want (%d,%v)", blob, n, ok, want[0], want[1] == 1)
		}
	}
}

func TestTheDeadlineSettingReadsDurationsZeroAndNonsense(t *testing.T) {
	for raw, want := range map[string]time.Duration{
		"2h": 2 * time.Hour, "90m": 90 * time.Minute, "48h": 48 * time.Hour,
		"0": 0, "none": 0, "off": 0, " 3H ": 3 * time.Hour,
		"a day": defaultStepDeadline, "-5m": defaultStepDeadline, "": defaultStepDeadline,
	} {
		if got := parseStepDeadline(raw); got != want {
			t.Errorf("%q: got %s, want %s", raw, got, want)
		}
	}
}
