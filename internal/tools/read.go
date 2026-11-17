package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/policy"
)

// The `read` tool.
//
// # A tool result is a STRING with IsError (W14)
//
// Not a typed value. The model reads it, so it is prose — and `IsError`
// distinguishes a tool that FAILED from one that returned the text "error",
// which a string alone cannot.
//
// # Truncation is always reported with the TRUE size (D9)
//
// Never silent. A model handed the first 64 KiB of a file with nothing saying
// so reasons about a complete file it has only part of — and will say "the
// config contains no X" when X is on line 3000.

// ToolContext is everything a tool is allowed to know.
//
// # No ambient state (I5)
//
// No `os.Getenv`, no `os.Getwd`, nothing read from the process. The workspace
// root arrives here, from the validated `StepRequest`, because a tool calling
// `os.Getwd` would resolve against the DAEMON's directory — which is not the
// client's, and which works on the author's machine.
//
// A test greps this package for both calls.
type ToolContext struct {
	// WorkspaceRoot is the only directory a step can address. Absolute.
	WorkspaceRoot string

	// CommandVerdict is the step's policy seam: the closure that answers
	// "what does the engine say about running this exact command" with the
	// verdict IT drew from the step's policy set. Wired by every BuildToolset
	// caller that has a store (daemon.StepRunner.BuildTools, T13) over the
	// SAME gate path the runtime's gate uses — one engine, one selection
	// rule — and left nil by a storeless caller, which makes bash inert:
	// a toolset with no policy source cannot spawn anything (fail-closed).
	//
	// It is a function field and not a *policy.Store on purpose: the tools
	// package must not reach a store (the onion says the tool gets what the
	// caller hands it), and a closure is the smallest seam that lets the
	// daemon answer from the step's frozen-or-live pair without the tools
	// package knowing a store exists. The command argument is the exact
	// string the tool is about to run; the verdict's Token (G16) is the
	// only thing Spawn accepts.
	CommandVerdict func(command string) (policy.Verdict, bool)

	// Exec is the step's execution-environment seam (S59, EV9): the closure
	// that answers "with WHAT would a command of this step run" — a resolved
	// environment, the step's secret redactor, the backend somebody bound to
	// the component. Wired by BuildToolset from the daemon's
	// `StepExecResolver` beside `CommandVerdict`, and left nil by a caller with
	// no store, which makes bash inert: a tool that cannot ask what it would
	// run WITH spawns nothing, exactly as a tool that cannot ask whether it may
	// run spawns nothing (fail-closed, and deliberately the same shape).
	//
	// It is a function returning an executor rather than an executor field
	// because an executor is per-step state — a resolved environment and a
	// redactor counting this step's hits — and a toolset held across steps must
	// not carry one step's secrets into another's output. Asking per command
	// makes "which step is this for?" a question the resolver answers every
	// time. A refusal (`*execenv.Refusal`: unbound environment, unavailable
	// backend, unresolvable secret) travels to the model verbatim, because a
	// command that never ran needs a reason that survives the transcript.
	Exec func(ctx context.Context) (CommandExecutor, error)

	// Records is the step's door to the org records — goals, briefs, tasks,
	// attachments (S38). It is a function field for the same reason
	// CommandVerdict is: the tools package must not reach a store (the onion
	// says the tool gets what the caller hands it), and a closure is the
	// smallest seam that lets the daemon answer from THE STEP whose id rides
	// the context (stepctx) without the tools package knowing a store exists.
	//
	// It is filled by daemon.StepRunner.BuildTools and left nil by a
	// storeless caller, which makes every records tool inert — the same
	// fail-closed discipline that makes bash inert without a verdict source.
	// A storeless runtime must say SO, not pretend the table is empty.
	Records RecordsInvoker

	// Todo is S75 T2's door to the loop's persisted todo list (todo_store.go):
	// filled by daemon.StepRunner.BuildTools, identity read from the stepctx the
	// runtime stamps at dispatch. Nil means no store: the tool keeps the list for
	// this step only and says so.
	Todo TodoStore

	// Docs is the step's door to its loop's documents — the deliverables the
	// `doc_*` tools file, read and edit (doc_store.go). Filled by
	// daemon.StepRunner.BuildTools; nil means no store, and the tools say so.
	Docs DocStore

	// LoopContext renders the loop's index (documents, tasks, answers, tools) for the `loop_context` tool. Filled by
	// daemon.StepRunner.BuildTools; nil means no store, and the tool says so.
	LoopContext LoopContextSource

	// Env is the step's door to the variable manager (env.go): the `env_list` and
	// `env_get` tools. Filled by daemon.StepRunner.BuildTools; nil means no
	// manager, and the tools say so.
	Env EnvStore
}

// RecordsInvoker runs one catalog records action for the step whose id rides
// the context. The contract is a tool result's shape on purpose: the invoker
// answers with the tool result's TEXT and the tool result's error flag — the
// daemon's half turns refusal sentences into content with isError set, so a
// refusal reads to the model as a refusal and not as an empty success.
type RecordsInvoker func(ctx context.Context, tool string, input json.RawMessage) (content string, isError bool)

// recordsOrFail is the fail-closed reading of the seam: a runtime that was
// never given a records door says so in the result, rather than answering
// "no goals" from an injected nothing that the model would file as fact.
func recordsOrFail(tc ToolContext) RecordsInvoker {
	if tc.Records == nil {
		return func(context.Context, string, json.RawMessage) (string, bool) {
			return "records are unavailable in this runtime", true
		}
	}
	return tc.Records
}

// Tool is one thing a model can call.
//
// `Spec` is what the model is told exists; `Run` is what happens. The two are
// together so a tool cannot be declared without being implemented, which is
// the failure mode of a registry keyed by name.
type Tool struct {
	Spec  model.ToolSpec
	Class RerunClass
	Run   func(ctx context.Context, tc ToolContext, input json.RawMessage) Result
}

func (t Tool) RerunClass() RerunClass { return t.Class }

// Result is what a tool returns.
type Result struct {
	// Content is what the model sees.
	Content string

	// IsError marks a failure. A tool that failed must be distinguishable from
	// one that returned the text "error" (W14).
	IsError bool

	// Summary is a short description for the event stream — `read README.md`,
	// `read README.md · 2.1 KiB`. Not the content: a tool result carrying a
	// file's whole text would put it in the event stream as well as the
	// transcript.
	Summary string

	// Path names the workspace-relative file a write-shaped result concerns.
	// Empty when the tool is not about a file.
	Path string

	// LinesAdded and LinesRemoved are LINE-DIFF COUNTS against the previous
	// content (S31 D8): what the write cost the file, as a number the ladder
	// and the chat screen can show without re-reading either version. They
	// ride the event stream only — `Content`, the model-facing text, stays
	// byte-identical (N5: the ten tools' behaviour is frozen; metadata rides
	// NEW fields, never the old ones).
	LinesAdded   int
	LinesRemoved int

	// ExitCode is how a COMMAND finished — set by `bash` on every result
	// where the command actually STARTED, success (0) or failure, and nil
	// everywhere else (S33 T6). nil is load-bearing three-state honesty
	// (P31): "a tool that ran and exited 0" and "a tool that never started"
	// both look like a zero field, and counting a command that never ran as
	// run is exactly the confident wrong answer.
	ExitCode *int

	// WallMs is how long the command ran, measured by the spawn itself —
	// 0 for non-command results (S33 T6, D6). Same S31 discipline: the
	// model-facing Content/Summary are untouched; the fact rides a NEW
	// field onto the loop's choke point, where telemetry is the consumer
	// (N5: the ten tools' behaviour frozen, metadata on new fields).
	WallMs int64
}

// Errorf builds a failed result. A constructor, so `IsError` cannot be
// forgotten — a failure reported as a success is a model that proceeds on a
// result that did not happen.
func Errorf(format string, args ...any) Result {
	return Result{Content: fmt.Sprintf(format, args...), IsError: true}
}

// readParams is `read`'s input.
type readParams struct {
	Path string `json:"path"`
	// Offset is a 1-based line number. 1-based because a model counting lines
	// in a file it was shown counts from 1, and an off-by-one in a tool call is
	// invisible in the transcript.
	Offset int `json:"offset,omitempty"`
	Limit  int `json:"limit,omitempty"`
}

// Read is the `read` tool.
func Read() Tool {
	return Tool{
		Class: RerunSafe,
		Spec: model.ToolSpec{
			Name: "read",
			Description: "Read a UTF-8 text file from the workspace. Paths are relative to the " +
				"workspace root; absolute paths and `..` segments are refused. Output is capped, and " +
				"truncation is reported with the file's true size.",
			Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {"type": "string", "description": "workspace-relative path"},
    "offset": {"type": "integer", "description": "first line to return, 1-based"},
    "limit": {"type": "integer", "description": "how many lines to return"}
  },
  "required": ["path"],
  "additionalProperties": false
}`),
		},
		Run: runRead,
	}
}

func runRead(ctx context.Context, tc ToolContext, input json.RawMessage) Result {
	var p readParams
	if err := json.Unmarshal(input, &p); err != nil {
		// An unparseable argument is a TOOL ERROR, not a step failure (Q6). A
		// model emitting bad arguments is normal, and it can recover from being
		// told — so the loop continues.
		return Errorf("the arguments for `read` are not valid JSON: %v", err)
	}

	resolved := Resolve(tc.WorkspaceRoot, p.Path)
	if !resolved.OK {
		// The named refusal reaches the model verbatim. It can act on the
		// difference: an absolute path is a mistake to correct, an escape is an
		// attempt to stop making.
		return Errorf("read refused %q: %s", p.Path, resolved.Reason)
	}

	// Cancellation checked BEFORE the read, because the read is the expensive
	// part and a cancelled step should not start one.
	if err := ctx.Err(); err != nil {
		return Errorf("read was cancelled before it began: %v", err)
	}
	if docs, name, ok := claimedDocument(ctx, tc, resolved); ok {
		return routedRead(ctx, docs, name, p.Offset, p.Limit)
	}

	info, err := os.Stat(resolved.Abs)
	if err != nil {
		if os.IsNotExist(err) {
			return Errorf("read could not find %s in the workspace; use `list` to find the file", resolved.Rel)
		}
		return Errorf("read could not stat %s: %v", resolved.Rel, err)
	}
	if info.IsDir() {
		return Errorf("%s is a directory. Use `list` to see its entries", resolved.Rel)
	}

	raw, err := os.ReadFile(resolved.Abs)
	if err != nil {
		return Errorf("read could not read %s: %v", resolved.Rel, err)
	}
	if err := ctx.Err(); err != nil {
		return Errorf("read was cancelled after reading %s: %v", resolved.Rel, err)
	}

	content, note := applyReadBounds(string(raw), p.Offset, p.Limit, int(info.Size()))

	summary := fmt.Sprintf("read %s · %s", resolved.Rel, humanBytes(int(info.Size())))
	if note != "" {
		summary += " · truncated"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", resolved.Rel)
	if note != "" {
		// The note comes FIRST, before the content. A truncation notice after
		// 64 KiB of text is a notice nobody reads — and the model needs to know
		// what it is holding before it reasons about it.
		fmt.Fprintf(&b, "%s\n", note)
	}
	b.WriteString("\n")
	b.WriteString(content)

	return Result{Content: b.String(), Summary: summary}
}

// applyReadBounds applies the offset, the limit and the caps, and describes
// what it did.
//
// The note names the TRUE size every time it truncates, because "truncated" on
// its own tells a model nothing about how much it is missing — a file cut from
// 70 KiB and one cut from 70 MiB need different responses.
func applyReadBounds(content string, offset, limit, trueBytes int) (out string, note string) {
	lines := strings.Split(content, "\n")
	totalLines := len(lines)

	start := 0
	if offset > 1 {
		start = offset - 1
		if start >= totalLines {
			return "", fmt.Sprintf("[the file has %d lines; offset %d is past the end]",
				totalLines, offset)
		}
	}

	end := totalLines
	if limit > 0 && start+limit < end {
		end = start + limit
	}

	var notes []string
	if lineCap := start + ReadMaxLines; end > lineCap {
		end = lineCap
		notes = append(notes, fmt.Sprintf("capped at %d lines of %d", ReadMaxLines, totalLines))
	}
	if start > 0 || end < totalLines {
		notes = append(notes, fmt.Sprintf("showing lines %d-%d of %d", start+1, end, totalLines))
	}

	out = strings.Join(lines[start:end], "\n")

	if len(out) > ReadMaxBytes {
		out = out[:ReadMaxBytes]
		notes = append(notes, fmt.Sprintf("cut at %d bytes; the file is %s",
			ReadMaxBytes, humanBytes(trueBytes)))
	}

	if len(notes) > 0 {
		note = "[" + strings.Join(notes, "; ") + "]"
	}
	return out, note
}

func humanBytes(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d bytes", n)
	}
}
