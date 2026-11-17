package tools

import "time"

// The tools' limits, each with its provenance (O4, I6).
//
// # Why these live here and not in `runtime/limits.go`
//
// A cap on `read`'s output belongs where `read` is: S11 will change these
// together with the policy that uses them, and a cap two packages away is one
// nobody finds. The runtime's own bounds — iterations, the deadline — are in
// `runtime/limits.go` for the same reason.
//
// Every number is a GUESS unless it says otherwise, and the QA suite is written
// to change them rather than confirm them.

const (
	// BashTimeout is the mandatory per-command timeout.
	//
	// # Provenance: the agent bodies, counted
	//
	// The superseded 05-tools plan derived it from the handlers themselves:
	// *"they write `timeout 600` twenty-one times, `timeout 300` twenty-one
	// times and `timeout 120` three times, so 600s is the longest budget the
	// handlers themselves chose."*
	//
	// Re-counted today against `opencode/agents/`: **14 bodies** still contain
	// `timeout 600`. The corpus has changed since the number was chosen, which
	// is worth knowing — the number is carried because the reasoning holds (it
	// is the longest budget a handler asked for), not because the count is
	// still 21.
	//
	// It is MANDATORY, not a default to omit: a command with no timeout is a
	// step that hangs, and a hung skateboard is as useless as an unsafe one.
	BashTimeout = 600 * time.Second

	// BashKillGrace is how long a terminated process group has to exit before
	// it is killed.
	//
	// # Provenance: none. Chosen.
	//
	// Two seconds: long enough for a shell to run a trap and flush, short
	// enough that a step being cancelled does not appear to hang. SIGTERM
	// first, because a command that can clean up should get to.
	BashKillGrace = 2 * time.Second

	// BashStdoutCap and BashStderrCap bound captured output, per stream.
	//
	// # Provenance: the superseded 05-tools plan — 65 536 bytes each
	//
	// Its reasoning: *"the per-stream caps match slice 4's maxToolResultBytes
	// so the two layers agree at the boundary rather than fighting."* Keeping
	// both at 64 KiB is what stops one layer truncating what the other just
	// allowed.
	//
	// Truncation is always REPORTED with the true size (D9). A cap that
	// silently cuts output is a model reasoning from half a test run.
	BashStdoutCap = 65536
	BashStderrCap = 65536

	// ReadMaxBytes and ReadMaxLines bound `read`.
	//
	// # Provenance: the superseded 05-tools plan — 2 000 lines, 262 144 bytes
	//
	// Both, because either alone misses a case: a minified file is one line
	// and megabytes, and a log is small lines without end.
	ReadMaxBytes = 262144
	ReadMaxLines = 2000
)

// Shell is the interpreter `bash` runs commands through.
//
// # Provenance: the superseded 05-tools plan, and the flags are the point
//
// *"Bash because the bodies' examples are bash-flavoured and one uses a
// heredoc; `--noprofile --norc` because a login shell sources the operator's
// dotfiles, which is ambient state (I5) and would make tool behaviour depend on
// whose machine it runs on."*
//
// That second half is a correctness property, not tidiness: an alias or a
// `PATH` edit in somebody's `.bashrc` would change what a command does, and the
// step's transcript would not show why.
func Shell() (name string, args []string) {
	return "bash", []string{"--noprofile", "--norc", "-c"}
}

// Limits reports every tool limit with its provenance, for the test that
// asserts each is defined here and labelled.
func Limits() []Limit {
	return []Limit{
		{"BashTimeout", int(BashTimeout / time.Millisecond), true,
			"the agent bodies: `timeout 600` was the longest budget a handler chose (21 occurrences " +
				"when the number was picked; 14 today)"},
		{"BashKillGrace", int(BashKillGrace / time.Millisecond), true,
			"none — chosen: long enough for a trap to run, short enough that cancelling does not look like a hang"},
		{"BashStdoutCap", BashStdoutCap, true,
			"the superseded 05-tools plan, matched to maxToolResultBytes so the two layers agree"},
		{"BashStderrCap", BashStderrCap, true,
			"as BashStdoutCap"},
		{"ReadMaxBytes", ReadMaxBytes, true,
			"the superseded 05-tools plan"},
		{"ReadMaxLines", ReadMaxLines, true,
			"the superseded 05-tools plan; both a line and a byte bound, because either alone misses a case"},
	}
}

// Limit is one bound and where its number came from.
type Limit struct {
	Name       string
	Value      int
	Guess      bool
	Provenance string
}
