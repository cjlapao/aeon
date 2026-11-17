package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/execenv"
	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/stepctx"
)

// The `bash` tool: bounds, and the verdict seam (W12 → S11).
//
// # Where the policy lives, and where it does not
//
// S11 brought the command policy, and this file is still not where it
// lives. The matcher, the segmenter and the corpus are `internal/policy`;
// the tool here carries only the SEAM (ToolContext.CommandVerdict) and the
// discipline the seam makes possible: no Spawn without the verdict's token
// (G16), and an honest statement of the matcher's limits in the description
// the model reads (G20). A test in this package still greps for pattern
// matching and fails if any appears — a half-policy in the tool layer would
// look like a boundary and not be one; the boundary is the engine's.
//
// # What makes the shell safe enough to exist, all three load-bearing
//
//   - the tool list is an ALLOW-LIST and `bash` is not in the default grant
//     (D1). A step gets it only through an explicit grant
//   - the BOUNDS (W13): a mandatory timeout, closed stdin, capped output,
//     and the command-length bound (SEC-011) at this boundary, before the
//     engine ever sees the string
//   - the VERDICT: every spawn carries a policy-issued allow token, and a
//     toolset wired without a policy source cannot spawn anything at all
//
// None of that is a sandbox — there is no sandbox (a standing non-goal),
// and the description below says plainly what the matcher does not analyse,
// because a limit the model is not told about is one it may cross without
// meaning to (G20).

// commandMaxLen bounds the command string at the tool boundary (SEC-011).
//
// The engine's splitter is bounded in OUTPUT (32 segments) but its scan
// grows with the input: a 2 MB command costs ~150 ms and a transient
// million-entry slice before the first segment is even matched. The cheap
// bound is at the boundary that receives the string — before Split is
// called — and the refusal goes toward deny: a command nobody can read in
// full is a command nobody should be spawning. 64 KiB is orders of magnitude
// past any real command (the longest corpus-seeded step run measured in
// hundreds of bytes) and orders of magnitude below anything sized to hurt.
const commandMaxLen = 1 << 16

// BashHonesty is the G20 limits string — the matcher's limits, in the words
// the model reads, pinned by TestBashDescriptionTellsTheTruth. One constant
// so the tool description and every surface that quotes the limits cannot
// drift from each other (the one-formatter rule, applied to the one limits
// sentence): the tool description and the CLI's `--allow-bash` help, which
// print it verbatim. (The every-use stderr warning that also used to print it
// was retired at T19 with the apology it belonged to — S11 shipped the
// command policy the warning apologized for; see grantList.) A MISSING
// string here is a test failure even when the bypasses behave as documented:
// the instruction is the only control for the class the mechanism refuses to
// parse.
const BashHonesty = "Command policy is decided by the engine over the WHOLE string: " +
	"it splits on `&&`, `||`, `;` and newlines, and every segment must be allowed. " +
	"It does NOT analyse quoting, `bash -c` (or `sh -c`), command substitution `$()`, " +
	"backticks, heredocs, a single `|`, a single `&`, or `()` subshells — such a " +
	"string is ONE command to the matcher, and evading the policy that way violates " +
	"the policy regardless of whether the matcher catches it."

type bashParams struct {
	Command string `json:"command"`
	// TimeoutSeconds lets a handler shorten the bound. It cannot LENGTHEN it
	// past BashTimeout: a tool that could raise its own ceiling has no ceiling.
	TimeoutSeconds int `json:"timeoutSeconds,omitempty"`
}

// Bash is the `bash` tool.
func Bash() Tool {
	return Tool{
		Class: RerunUnsafe,
		Spec: model.ToolSpec{
			Name: "bash",
			Description: "Run a shell command in the workspace. It runs with a mandatory timeout, " +
				"with stdin closed, and with stdout and stderr capped — truncation is reported with " +
				"the true size. " + BashHonesty + " " + execenv.Honesty + " " +
				execenv.RedactionLimits + " Commands longer than " +
				strconv.Itoa(commandMaxLen) + " bytes are refused at this boundary.",
			Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "command": {"type": "string", "description": "the shell command to run"},
    "timeoutSeconds": {"type": "integer", "description": "shorten the timeout; it cannot be lengthened"}
  },
  "required": ["command"],
  "additionalProperties": false
}`),
		},
		Run: runBash,
	}
}

func runBash(ctx context.Context, tc ToolContext, input json.RawMessage) Result {
	var p bashParams
	if err := json.Unmarshal(input, &p); err != nil {
		return Errorf("the arguments for `bash` are not valid JSON: %v", err)
	}
	if strings.TrimSpace(p.Command) == "" {
		return Errorf("`bash` needs a command")
	}

	// The length bound, FIRST (SEC-011): before the policy seam, because the
	// seam runs the engine's Split and the whole point of bounding here is
	// that the oversized string never reaches it. Refused toward deny — this
	// is the tool boundary declining, not the policy speaking, so the text
	// says which it is.
	if len(p.Command) > commandMaxLen {
		return Errorf("`bash` refused a %d-byte command: the bound at this boundary is %d bytes. "+
			"A command too long to read is refused toward deny rather than split and hoped for; "+
			"run the work in smaller pieces.", len(p.Command), commandMaxLen)
	}

	// The verdict seam (G16). Missing is not permissive: a toolset built
	// without a policy source cannot know whether the command is allowed,
	// and a tool that spawns while unable to ask is the half-policy this
	// slice's header has always refused to be.
	if tc.CommandVerdict == nil {
		return Errorf("bash cannot run the command: this step's toolset was built without a " +
			"policy source (no command-verdict seam), and a bash tool that cannot ask the engine " +
			"whether a command is allowed spawns nothing. The step's caller wires the seam from a " +
			"store; a storeless daemon, by design, has nothing to ask.")
	}
	verdict, ok := tc.CommandVerdict(p.Command)
	if !ok {
		return Errorf("bash cannot run the command: the step's policy source could not answer for " +
			"it (no verdict was available), and a command nobody has vouched for does not spawn. " +
			"This is the same fail-closed the gate takes when the set cannot be read.")
	}
	// A person approved THIS exact call (the permission question was answered approve and the
	// runtime marked this dispatch): a verdict that only ASKED becomes allowed for it, once. A deny
	// is never softened — only the ask tier is — and a different command is not approved.
	if verdict.Asked && !verdict.Allowed && stepctx.ApprovedFor(ctx, "bash", p.Command) {
		verdict = policy.Verdict{Allowed: true, Subject: verdict.Subject, Type: verdict.Type}
	}
	token, ok := verdict.Token(p.Command)
	if !ok {
		// Defense in depth, rendered by the ONE formatter (verbatim, so this
		// second-line refusal cannot drift from the gate's own). The runtime's
		// gate normally refuses before a tool is ever dispatched; a verdict
		// that says no HERE still says no, whatever got the block this far.
		return Result{Content: verdict.Sentence(), IsError: true,
			Summary: "bash refused by the command policy"}
	}

	// The execution seam (EV9). The verdict above answered whether this
	// command may run; this answers with WHAT it runs — the environment bound
	// to the step's component, its secret references resolved now, the
	// redactor that will filter its output. Missing is not permissive, and for
	// the same reason as the verdict above: a tool that cannot ask what a
	// command runs with would run it with whatever the daemon happens to hold.
	if tc.Exec == nil {
		return Errorf("bash cannot run the command: this step's toolset was built without an " +
			"execution-environment seam (no Exec), and a bash tool that cannot ask what environment a " +
			"command would run in spawns nothing. The step's caller wires the seam from a store; a " +
			"storeless daemon, by design, has nothing to ask.")
	}
	ex, err := tc.Exec(ctx)
	if err != nil {
		// A refusal is the environment's own voice, and it goes to the model
		// VERBATIM: the command did not run, and the sentence is the answer to
		// the question the model is about to ask. Paraphrasing it here would
		// put a second formatter beside execenv's one.
		var refusal *execenv.Refusal
		if errors.As(err, &refusal) {
			return Result{Content: refusal.Sentence(), IsError: true,
				Summary: "bash refused: the step's execution environment"}
		}
		return Errorf("bash could not run the command: the step's execution environment could not be "+
			"resolved: %v", err)
	}

	req := ExecRequest{Command: p.Command, Dir: tc.WorkspaceRoot}
	if p.TimeoutSeconds > 0 {
		requested := time.Duration(p.TimeoutSeconds) * time.Second
		// Shortened, never lengthened. A tool that can raise its own ceiling
		// has no ceiling, and the ceiling is the only bound on a command that
		// never exits.
		if requested < BashTimeout {
			req.Timeout = requested
		}
	}

	res, err := ex.Run(ctx, req, token)
	if err != nil {
		// The command could not START — a missing shell, an unusable
		// directory, or a token that does not carry this exact command (the
		// last is a defect, not a policy answer: the refusal above is the
		// policy voice, and it has already spoken). Distinguished from a
		// command that ran and failed, because the two send a model to
		// different places.
		return Errorf("bash could not run the command: %v", err)
	}

	// S33 T6 (D6): the exit code and the spawn's own wall ride the result —
	// set ALWAYS when the command ran, success or failure (an `exit 0` is
	// data, the data D6 counts most), and set HERE only: the paths above
	// that never reached Spawn leave ExitCode nil — a command that could
	// not start is not a run.
	exit := res.ExitCode
	return Result{
		ExitCode: &exit,
		WallMs:   res.Duration.Milliseconds(),
		// The command echo goes through the SAME redactor the output came
		// through: a command that named a secret on its own command line
		// (`curl -H "Authorization: Bearer $TOKEN"`) is exactly as much a
		// transcript as the output it produced.
		Content: renderRun(ex.Redact(p.Command), res),
		// A non-zero exit is NOT an error from the tool's point of view: the
		// command ran, and its exit code is a fact the model asked for. Marking
		// it IsError would make `grep -q` finding nothing look like a broken
		// tool.
		IsError: false,
		Summary: summariseRun(p.Command, res),
	}
}

// renderRun is what the model reads.
//
// Order matters: the EXIT STATUS and any bound that was hit come first, because
// a model handed output first will reason about the output and miss that the
// command was killed half way through producing it.
func renderRun(command string, res RunResult) string {
	var b strings.Builder

	fmt.Fprintf(&b, "$ %s\n", command)

	switch {
	case res.TimedOut:
		fmt.Fprintf(&b, "TIMED OUT after %s. The output below is what it produced before being "+
			"stopped — it is partial, and the command may not have finished what it was doing.\n",
			res.Duration.Round(time.Millisecond))
	case res.OOMKilled:
		// Stated as the heuristic it is. Claiming "out of memory" would be a
		// diagnosis this layer cannot make.
		fmt.Fprintf(&b, "KILLED (exit %d%s). That is what the kernel's OOM killer looks like, and "+
			"also what `kill -9` from anything else looks like — this layer cannot tell them apart.\n",
			res.ExitCode, signalSuffix(res))
	default:
		fmt.Fprintf(&b, "exit %d%s · %s\n", res.ExitCode, signalSuffix(res),
			res.Duration.Round(time.Millisecond))
	}

	writeStream(&b, "stdout", res.Stdout, res.StdoutBytes, res.StdoutTruncated, BashStdoutCap)
	writeStream(&b, "stderr", res.Stderr, res.StderrBytes, res.StderrTruncated, BashStderrCap)

	return b.String()
}

func writeStream(b *strings.Builder, name, content string, trueBytes int, truncated bool, cap int) {
	if content == "" && !truncated {
		return
	}
	fmt.Fprintf(b, "\n--- %s", name)
	if truncated {
		// The TRUE size, every time (D9). "truncated" alone cannot tell a model
		// whether it is missing a line or a megabyte.
		fmt.Fprintf(b, " (first %d bytes of %s)", cap, humanBytes(trueBytes))
	}
	fmt.Fprintf(b, " ---\n%s", content)
	if !strings.HasSuffix(content, "\n") {
		b.WriteString("\n")
	}
}

func summariseRun(command string, res RunResult) string {
	short := command
	if len(short) > 60 {
		short = short[:57] + "…"
	}
	switch {
	case res.TimedOut:
		return fmt.Sprintf("bash %q · TIMED OUT after %s", short, res.Duration.Round(time.Millisecond))
	case res.OOMKilled:
		return fmt.Sprintf("bash %q · killed", short)
	default:
		return fmt.Sprintf("bash %q · exit %d · %s", short, res.ExitCode,
			res.Duration.Round(time.Millisecond))
	}
}

func signalSuffix(res RunResult) string {
	if res.Signal == "" {
		return ""
	}
	return ", signal " + res.Signal
}
