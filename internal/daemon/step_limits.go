package daemon

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/config"
	"github.com/cjlapao/aeon/internal/runtime"
	"github.com/cjlapao/aeon/internal/store"
)

// stepMaxIterationsKey is the declared setting (config/settings_decl_steps.go).
const stepMaxIterationsKey = "step.maxIterations"

// The bound on a step's model calls is optional and unlimited by default: a loop
// runs until it is done. Three layers can set one, most specific first:
//
//  1. the AGENT's own bound (its metadata, key `maxIterations`) — wins;
//  2. the blueprint STEP's `max_iterations`;
//  3. the daemon SETTING `step.maxIterations`.
//
// In each layer, absent means "not set here" and 0 means "no bound". A caller
// that names a bound itself (the CLI's --max-iterations) outranks all three; the
// wall-clock deadline is a separate bound and still applies.

// stepMaxIterationsFrom reads the setting fresh at each step start. An
// unreadable setting answers 0 (no bound): a daemon that cannot read its own
// setting still has to run steps.
func stepMaxIterationsFrom(settings *config.Store) func(context.Context) int {
	// A concrete pointer, not the Reader interface: a nil *Store inside an
	// interface is a non-nil interface that panics on first use.
	if settings == nil {
		return nil
	}
	return func(ctx context.Context) int {
		n, err := settings.Int(ctx, stepMaxIterationsKey)
		if err != nil || n < 0 {
			return 0
		}
		return n
	}
}

// agentMaxIterationsFrom reads an agent's bound off its metadata blob.
func agentMaxIterationsFrom(st *store.Store) func(string) (int, bool) {
	if st == nil {
		return nil
	}
	return func(agent string) (int, bool) {
		a, found, err := st.AgentBySlug(agent)
		if err != nil || !found {
			if a2, found2, err2 := st.AgentByName(agent); err2 == nil && found2 {
				a, found = a2, true
			}
		}
		if !found {
			return 0, false
		}
		return maxIterationsFromMetadata(a.Metadata)
	}
}

// maxIterationsFromMetadata reads `maxIterations` from an agent's metadata blob.
// A missing key, a blank blob, a malformed blob or a non-number all mean "this
// agent sets none" — never a guessed bound.
func maxIterationsFromMetadata(blob string) (int, bool) {
	if blob == "" {
		return 0, false
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(blob), &m); err != nil {
		return 0, false
	}
	raw, ok := m["maxIterations"]
	if !ok {
		return 0, false
	}
	var n int
	if err := json.Unmarshal(raw, &n); err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// resolveMaxIterations applies the layers to one compile. The answer is what
// CompileInput.MaxIterations means: positive is a bound, negative is "no bound",
// 0 leaves the runtime's compiled default (only when nothing here can answer).
func (r StepRunner) resolveMaxIterations(ctx context.Context, in runtime.CompileInput) int {
	pick := func(n int) int {
		if n == 0 {
			return -1
		}
		return n
	}
	if r.AgentMaxIterations != nil {
		if n, ok := r.AgentMaxIterations(in.Handler.Name); ok {
			return pick(n)
		}
	}
	if in.StepMaxIterations != nil {
		return pick(*in.StepMaxIterations)
	}
	if r.MaxIterations != nil {
		return pick(r.MaxIterations(ctx))
	}
	return 0
}

// stepDeadlineKey is the declared setting (config/settings_decl_steps.go).
const stepDeadlineKey = "step.deadline"

// defaultStepDeadline is what an unparseable setting falls back to.
const defaultStepDeadline = 2 * time.Hour

// stepDeadlineFrom reads the working-time window fresh at each step start. "0"
// (or "none") means no deadline; a value that will not parse is the default,
// never a daemon that cannot start a step.
func stepDeadlineFrom(settings *config.Store) func(context.Context) (time.Duration, bool) {
	if settings == nil {
		return nil
	}
	return func(ctx context.Context) (time.Duration, bool) {
		raw, err := settings.String(ctx, stepDeadlineKey)
		if err != nil {
			return defaultStepDeadline, true
		}
		return parseStepDeadline(raw), true
	}
}

// parseStepDeadline reads a duration; zero means no deadline.
func parseStepDeadline(raw string) time.Duration {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "0" || raw == "none" || raw == "off" {
		return 0
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < 0 {
		return defaultStepDeadline
	}
	return d
}
