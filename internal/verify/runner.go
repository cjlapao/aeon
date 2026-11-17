package verify

// runner.go — the gate runner (S57 VG4). The only place a gate is spawned:
// policy first (a deny, a no-answer or an unanswered ask spawns nothing),
// then tools.Spawn with the scrubbed environment (VG5) and the gate's own
// timeout, then the VG4 item-4 mapping: exit 0 ⇒ pass, non-zero ⇒ fail, and
// every non-completion — timeout, kill, start failure, cancellation — is
// unverified, because a check that did not complete proves nothing.
//
// The runner imports tools and policy and nothing that reaches a store (the
// onion rule): the records are T3's, supplied through the small seams
// (GateSpec in, GateResult out), and the daemon's T6 wires the Verdict seam
// over CommandVerdictFor with the zero frozen manifest (T0 item 6: the
// zero manifest answers from the live store).

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/cjlapao/aeon/internal/execenv"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/tools"
)

// Runner runs gates. Every seam has a default; a Runner left at its
// defaults is fail-closed on the policy seam (a nil Verdict means no
// verdict, and no verdict spawns nothing).
type Runner struct {
	// Verdict answers the policy question for one command as one agent.
	// ok=false is "no verdict" (the source could not answer) — a refusal,
	// not a default. The daemon passes a closure over CommandVerdictFor.
	Verdict func(agent, command string) (policy.Verdict, bool)

	// Now is the clock the total deadline is measured against.
	Now func() time.Time

	// Spawn is the spawner; the default is tools.Spawn. Tests install a
	// spy to count the calls that must never happen.
	//
	// The fifth argument is the resolved child environment (S59, EV5). A gate
	// has none: it runs with the scrubbed gate allowlist in `SpawnOptions.Env`,
	// which is a complete environment of its own, so the runner passes the
	// INERT `execenv.ChildEnv{}` and `Spawn` takes the slice — the one pairing
	// it accepts without a resolved environment. Passing a built ChildEnv here
	// as well would be two environments for one command, and Spawn refuses
	// that rather than choosing.
	Spawn func(ctx context.Context, command string, opts tools.SpawnOptions, token policy.AllowToken,
		env execenv.ChildEnv) (tools.RunResult, error)
}

// New returns a Runner with the real clock and tools.Spawn, and the policy
// seam unset: fail-closed until the daemon wires it.
func New() Runner {
	return Runner{Now: time.Now, Spawn: tools.Spawn}
}

// noVerdictSentence is what a gate records when the seam cannot answer at
// all — a fixed sentence with nothing in it that came from a spec, because
// the one formatter has no verdict to render.
const noVerdictSentence = "no verdict was available for this command: the gate's policy " +
	"source could not answer, and a command nobody has vouched for does not run"

// RunGate runs ONE gate under the policy gate and the limits (VG4). It
// never returns an error: every outcome — including a start failure — is a
// result, and a result the record can hold.
func (r Runner) RunGate(ctx context.Context, dir, agent string, g GateSpec) GateResult {
	out := GateResult{
		Name:        g.Name,
		Requirement: g.Requirement,
		Command:     g.Command,
	}
	if g.ID != "" {
		id := g.ID
		out.CommandID = &id
	}

	// 1. Policy, first: the verdict before the spawn, always.
	v, ok := r.verdictFor(agent, g.Command)
	if !ok {
		return out.unverified("policy", noVerdictSentence)
	}
	if !v.Allowed || v.Asked {
		// deny, ask-unattended (an ask is a question nobody answered, and a
		// token for one cannot be minted) or default deny: the sentence is
		// the one formatter's, verbatim, and nothing spawns.
		return out.unverified("policy", v.Sentence())
	}
	token, ok := v.Token(g.Command)
	if !ok {
		// An allowed, un-asked verdict mints a token for any non-empty
		// command; reaching here means the command was empty — a gate with
		// nothing to run. Fail-closed, with the verdict's own sentence.
		return out.unverified("policy", v.Sentence())
	}

	// 2. The limits: the gate's own timeout, never past the bash ceiling —
	// a gate that could raise its own ceiling has no ceiling.
	timeout := time.Duration(g.TimeoutS) * time.Second
	if timeout > tools.BashTimeout {
		timeout = tools.BashTimeout
	}

	// 3. The spawn: scrubbed environment, the tool's caps, the group kill.
	res, err := r.spawn(ctx, g.Command, tools.SpawnOptions{
		Dir:     dir,
		Timeout: timeout,
		Env:     tools.GateEnv(os.Environ()),
	}, token)
	if err != nil {
		// A start failure is not a run: no exit code, no streams.
		return out.unverified("start-failed", "")
	}

	// 4. The mapping. Order matters: the timeout is the cause when the
	// group was ours to kill; a caller deadline or cancellation is the
	// cause when the run's context ended; a SIGKILL/137 is a kill. Only a
	// completed command may read as pass or fail.
	if res.TimedOut {
		return out.fill(res).unverified("timeout", "")
	}
	if ctx.Err() != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return out.fill(res).unverified("timeout", "")
		}
		return out.fill(res).unverified("cancelled", "")
	}
	if res.OOMKilled {
		// The heuristic tools/bash.go computes (exit 137 or SIGKILL) —
		// reused, not copied: a kill is not a completed command.
		return out.fill(res).unverified("killed", "")
	}
	exit := res.ExitCode
	if exit == 0 {
		return out.fill(res).done(&exit, "pass", "")
	}
	return out.fill(res).done(&exit, "fail", "")
}

// verdictFor calls the policy seam, treating a nil seam as "no verdict".
func (r Runner) verdictFor(agent, command string) (policy.Verdict, bool) {
	if r.Verdict == nil {
		return policy.Verdict{}, false
	}
	return r.Verdict(agent, command)
}

// spawn calls the spawner, defaulting to tools.Spawn.
func (r Runner) spawn(ctx context.Context, command string, opts tools.SpawnOptions, token policy.AllowToken) (tools.RunResult, error) {
	if r.Spawn == nil {
		return tools.Spawn(ctx, command, opts, token, execenv.ChildEnv{})
	}
	return r.Spawn(ctx, command, opts, token, execenv.ChildEnv{})
}

// now calls the clock, defaulting to time.Now.
func (r Runner) now() time.Time {
	if r.Now == nil {
		return time.Now()
	}
	return r.Now()
}

// unverified marks the result unverified with the closed word (plus the
// sentence, for the policy word). No exit code: the command never
// completed, and a nil that reads as zero would invent one.
func (g GateResult) unverified(word, sentence string) GateResult {
	g.State = "unverified"
	if sentence == "" {
		g.Reason = word
	} else {
		g.Reason = word + ": " + sentence
	}
	return g
}

// done marks a COMPLETED result (an exit code exists) with its state, and
// drops the tails on a pass — VG6: a pass stores no output text, and this
// is the only point where the state is known together with the streams.
func (g GateResult) done(exit *int, state, reason string) GateResult {
	g.ExitCode = exit
	g.State = state
	g.Reason = reason
	if state == "pass" {
		g.StdoutTail = ""
		g.StderrTail = ""
	}
	return g
}

// fill copies the spawn's facts — the true counts, the truncation, the
// digests of the CAPTURED bytes, the wall, the signal — and the redacted
// tails. done removes the tails on a pass.
func (g GateResult) fill(res tools.RunResult) GateResult {
	g.Signal = res.Signal
	g.TimedOut = res.TimedOut
	g.WallMS = int(res.Duration.Milliseconds())
	g.StdoutBytes = res.StdoutBytes
	g.StderrBytes = res.StderrBytes
	g.Truncated = res.StdoutTruncated || res.StderrTruncated
	g.StdoutSHA256 = sha256Hex(res.Stdout)
	g.StderrSHA256 = sha256Hex(res.Stderr)
	g.StdoutTail = tail(res.Stdout)
	g.StderrTail = tail(res.Stderr)
	return g
}

// tail is the last 2048 bytes of the captured stream, redacted — the table
// bound (verify_run_result's CHECK) at the source, so the store's refusal
// is unreachable, not a backstop.
func tail(s string) string {
	const maxTail = 2048
	if len(s) > maxTail {
		s = s[len(s)-maxTail:]
	}
	return Redact(s)
}

// sha256Hex is the hex digest of the CAPTURED bytes — the cap's bytes, which
// is what the digest is OF (VG4 item 4).
func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// Run runs a run: the effective gates sequentially in the order given,
// without short-circuit (a required fail does not stop the round — every
// gate runs so one round of rework sees every red), under the total
// deadline (the gates after it are unverified: timeout), aggregated by the
// VG4 item-5 rules.
func (r Runner) Run(ctx context.Context, in RunInput) RunResult {
	// A run with no required gate proves nothing required: it is unverified,
	// never pass on emptiness. An advisory-only set is the same shape — and
	// the gates do not run, because the run that proves nothing required
	// does not happen.
	if !hasRequired(in.Gates) {
		summary := runSummary(nil)
		return RunResult{
			State:   "unverified",
			Reason:  "unverified: no-gates-configured",
			Summary: summary,
			Digest:  digestOf(nil),
		}
	}

	start := r.now()
	results := make([]GateResult, 0, len(in.Gates))
	for i, g := range in.Gates {
		// The total deadline caps the SUM: the gates that do not start
		// after it are unverified with the timeout word, and the rest are
		// not started. A gate already running finishes under its own
		// timeout — the deadline is a budget on the sum, not a kill.
		if in.Deadline > 0 && r.now().Sub(start) >= in.Deadline {
			results = appendUnstarted(results, in.Gates[i:], "timeout")
			break
		}
		// The run's ctx is the loop's: S55's stop cancels it, and the
		// group kill terminates the running gate. The gates after it are
		// not started, recorded as cancelled.
		if ctx.Err() == context.Canceled {
			results = appendUnstarted(results, in.Gates[i:], "cancelled")
			break
		}
		results = append(results, r.RunGate(ctx, in.Dir, in.Agent, g))
	}

	state, reason := aggregate(results)
	summary := runSummary(results)
	if state == "fail" {
		// The fixed-template summary IS the fail's reason: the counts,
		// never user text.
		reason = summary
	}
	return RunResult{
		Results: results,
		State:   state,
		Reason:  reason,
		Summary: summary,
		Digest:  digestOf(results),
	}
}

// hasRequired reports whether the set has a gate the run must prove.
func hasRequired(gates []GateSpec) bool {
	for _, g := range gates {
		if g.Requirement == "required" {
			return true
		}
	}
	return false
}

// appendUnstarted records the gates that do not run (deadline, cancellation)
// as unverified with the word that explains it — the evidence shows why the
// run is incomplete, and the unstarted gates are named in it.
func appendUnstarted(results []GateResult, gates []GateSpec, word string) []GateResult {
	for _, g := range gates {
		r := GateResult{Name: g.Name, Requirement: g.Requirement, Command: g.Command}
		if g.ID != "" {
			id := g.ID
			r.CommandID = &id
		}
		results = append(results, r.unverified(word, ""))
	}
	return results
}

// aggregate is the VG4 item-5 truth table over the ordered results: any
// REQUIRED fail ⇒ fail; else any required unverified ⇒ unverified (with the
// first one's word); else pass. Advisory results never change the state.
func aggregate(results []GateResult) (state, reason string) {
	state = "pass"
	for _, g := range results {
		if g.Requirement != "required" {
			continue
		}
		switch g.State {
		case "fail":
			return "fail", ""
		case "unverified":
			if state == "pass" {
				state = "unverified"
				reason = "unverified: " + ReasonWord(g.Reason)
			}
		}
	}
	return state, reason
}

// runSummary is the fixed template — ids and counts only, never user text:
// "3 gates: 2 pass, 1 fail (required), 0 unverified". The fail count is the
// REQUIRED fails; the unverified count is every unverified gate, advisory or
// not, so a run that is pass on its required set still shows the advisory
// gap honestly.
func runSummary(results []GateResult) string {
	var n, pass, failRequired, unverified int
	for _, g := range results {
		n++
		switch g.State {
		case "pass":
			pass++
		case "fail":
			if g.Requirement == "required" {
				failRequired++
			}
		case "unverified":
			unverified++
		}
	}
	return fmt.Sprintf("%d gates: %d pass, %d fail (required), %d unverified", n, pass, failRequired, unverified)
}

// digestEntry is the canonical form the digest is computed over: the stable
// fields only — identity, exit, state, the true counts, the truncation and
// the stream digests. Seq, wall times, signal text, tails and the reason
// are excluded: the evidence is reproducible (wall time is not), and the
// reason may carry a policy sentence that names a rule.
type digestEntry struct {
	Name         string `json:"name"`
	Requirement  string `json:"requirement"`
	Command      string `json:"command"`
	ExitCode     *int   `json:"exit_code"`
	State        string `json:"state"`
	StdoutBytes  int    `json:"stdout_bytes"`
	StderrBytes  int    `json:"stderr_bytes"`
	Truncated    bool   `json:"truncated"`
	StdoutSHA256 string `json:"stdout_sha256"`
	StderrSHA256 string `json:"stderr_sha256"`
}

// digestOf is the sha256 of the canonical JSON of the ordered results.
// Struct field order is Go's, so the encoding is deterministic; an empty
// run digests the empty list, not the absence of one.
func digestOf(results []GateResult) string {
	entries := make([]digestEntry, 0, len(results))
	for _, g := range results {
		entries = append(entries, digestEntry{
			Name:         g.Name,
			Requirement:  g.Requirement,
			Command:      g.Command,
			ExitCode:     g.ExitCode,
			State:        g.State,
			StdoutBytes:  g.StdoutBytes,
			StderrBytes:  g.StderrBytes,
			Truncated:    g.Truncated,
			StdoutSHA256: g.StdoutSHA256,
			StderrSHA256: g.StderrSHA256,
		})
	}
	data, err := json.Marshal(entries)
	if err != nil {
		// A fixed struct of strings, ints and a *int cannot fail to
		// marshal; reaching here is a defect, not an outcome — the digest
		// of nothing would read as the digest of a run.
		panic(fmt.Sprintf("verify: the digest's canonical form will not encode: %v", err))
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
