package verify

// vocab.go — the closed vocabularies and the record types the runner
// produces. This file is the single source the daemon's verify.vocabulary
// reads (T8): a value nobody compiled here is a value no gate can report.
// The runner itself is runner.go; the best-effort tail redaction is
// redact.go.

import "time"

// States is the closed set of a gate's or a run's state. A gate is "skipped"
// only in the records T8 renders for a gate that exists but is not enabled;
// the runner never produces it (a gate that does not start is unverified
// with the reason that explains why).
var States = []string{"pass", "fail", "unverified", "skipped"}

// Reasons is the closed set of an unverified gate's reason word. A
// policy refusal may carry the verdict sentence after "policy: " — the
// sentence is the one formatter's output, verbatim, and the word stays
// closed.
var Reasons = []string{"policy", "timeout", "killed", "start-failed", "cancelled", "no-gates-configured"}

// Requirements is the closed set of a gate's requirement class.
var Requirements = []string{"required", "advisory"}

// ReasonWord strips the policy sentence: "policy: <sentence>" → "policy".
// For every other reason the reason IS the word, so this is the identity.
func ReasonWord(reason string) string {
	const marker = "policy: "
	if len(reason) > len(marker) && reason[:len(marker)] == marker {
		return "policy"
	}
	return reason
}

// GateSpec is one gate, as the caller describes it. It is the effective
// set's row with nothing the runner does not need: the caller (T6) supplies
// the gates already in position order — the runner runs them in the order
// given and knows nothing of position. MemoryMB is a declaration, not an
// enforcement (v1): the runner runs it and reports what the kernel does.
type GateSpec struct {
	ID          string
	Name        string
	Command     string
	Requirement string // "required" | "advisory"
	TimeoutS    int
	MemoryMB    *int
}

// GateResult is one gate's outcome — the shape of a verify_run_result row
// (VG6), with Seq left for the store's append to assign. Digests and true
// counts are ALWAYS present; the tails (≤ 2 KiB each, through Redact) are
// present only for non-pass results — a pass stores no output text at all.
type GateResult struct {
	Seq int

	CommandID   *string // nil when the gate is not a verify_command row
	Name        string
	Requirement string
	Command     string // SNAPSHOT: history survives a later edit or archive

	// ExitCode is nil when the command never completed a spawn (a policy
	// refusal, a start failure) — a command that could not start has no exit
	// code, and a nil that reads as zero would invent one.
	ExitCode *int
	Signal   string
	TimedOut bool // the timeout specifically, not any cancellation
	WallMS   int

	// The TRUE byte counts, before truncation, and whether either stream was
	// truncated at the capture cap.
	StdoutBytes int
	StderrBytes int
	Truncated   bool

	// SHA256 (hex) of each CAPTURED stream — the bytes the cap kept.
	StdoutSHA256 string
	StderrSHA256 string

	// The tails: at most 2048 bytes each, redacted, non-pass results only.
	StdoutTail string
	StderrTail string

	State  string // "pass" | "fail" | "unverified"
	Reason string // "" | a closed word | "policy: <sentence>"
}

// RunInput is one run's description: the effective gates in run order, the
// identity the policy gate evaluates each command as, the working
// directory, and the total wall budget (0 = none; the caller — T6 — bounds
// it by the loop's remaining step deadline, T0 item 5).
type RunInput struct {
	Gates    []GateSpec
	Agent    string
	Dir      string
	Deadline time.Duration
}

// RunResult is the run's finished evidence: the ordered results, the
// aggregated state, the reason ("" for pass, the summary for fail,
// "unverified: <word>" for unverified), the fixed-template summary, and the
// digest — the sha256 of the canonical JSON of the ordered results, over
// the stable fields only (exit, state, counts, stream digests, identity):
// wall times and timestamps are excluded by construction, so the same
// inputs produce the same digest.
type RunResult struct {
	Results []GateResult
	State   string
	Reason  string
	Summary string
	Digest  string
}
