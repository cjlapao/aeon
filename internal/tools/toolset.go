package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime/debug"
	"sort"
	"strings"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/policy"
)

// The allow-list: **the first safety boundary** (D1), and S11 added the
// second one behind it (the verdict seam — see bash.go).
//
// # Why that sentence is at the top of the file
//
// What stands between a step and an unrestricted shell is now TWO things,
// and this file owns the first: a step gets the tools it was GRANTED, and
// `BuildToolset(["read"], …)` constructs no bash tool at all. The second is
// the step's policy seam (ToolContext.CommandVerdict): a granted bash still
// spawns nothing without a verdict's token (G16), and a toolset built
// without a policy source cannot spawn anything. D1 asserts the first half
// BY ATTEMPT: a scripted model asks for `bash`, and the test observes that
// no process was created.
//
// "The map does not contain bash" is a much weaker claim, and it would pass
// against a loop that happened to construct the tool anyway.
//
// # Two behaviours that keep a step alive rather than correct-and-dead
//
//	a tool the step LACKS (O6)   →  tool_result with IsError, naming what IS
//	                                available. The model can recover from being
//	                                told; it cannot recover from silence
//	a tool that PANICS (O5)      →  tool_result with IsError, and the step
//	                                continues. A panicking tool taking down a
//	                                daemon serving other projects is worse than
//	                                a step reporting a bad result
//
// Construction is NOT wrapped. A nil dependency or an unknown grant must panic
// at build time, where the stack names the caller — rather than being reported
// as a tool failure at turn three, which is where a swallowed programmer error
// surfaces.

// Toolset is what one step can call.
type Toolset struct {
	// byName is the granted tools. Unexported: a caller that could add to it
	// after construction would be a second thing deciding what a step can
	// call, and the grant list would stop being the only one.
	byName map[string]Tool

	// granted is the names, in grant order, for the message a missing tool
	// produces.
	granted []string

	tc ToolContext
}

// aliasWriteFile is the ONE "write_file" string in production code: a
// blueprint pinned before the S11 rename names write_file, and pinned
// bytes are immutable — BuildToolset must still build for it. It is
// HIDDEN: not in ToolNames(), not in any grant list or help text, no
// documentation. Registry()`"write_file"` resolves to the SAME tool
// as Registry()`"write"` — and because policy's ToolAccess matcher
// consumes the mapping through CanonicalToolName (SEC-059), the string
// still appears nowhere else in production.
const aliasWriteFile = "write_file"

const canonicalWrite = "write"

// Registry is every tool that exists, by name.
//
// # Why this is a function and not a package variable
//
// A variable could be appended to from anywhere, and then "what tools exist"
// would have as many answers as there are init functions. S11 and S12 add
// tools by editing this function — one place, reviewable in a diff.
//
// # The S11 facts the registration audit (T19) counts (D1)
//
// The honest ten are exactly `bash, edit, glob, grep, list, question, read,
// todo, webfetch, write` — pinned name-for-name by TestRegistryExactNames.
// `bash` IS in the registry and is NOT in any default grant. Those are
// different facts: the registry says the tool exists, the grant list says this
// step may call it. `webfetch` and `todo` are registered and NOT default
// granted for the same reason in reverse — neither is a workspace file tool.
// A step that wants them is GRANTED them by an author; nothing here hands
// them out.
//
// The map carries the honest ten, S38's management catalog (the
// internal/records projection — 23 actions, 2026-10-27), plus the hidden
// alias documented on
// aliasWriteFile: one key more than the ten, answering to the SAME tool.
// Pinned pre-rename blueprints build through it; every listing shows only
// the ten (ToolNames filters it). Because the two names bind ONE tool, the
// T19 rulings keep them from reading as two: Specs offers a tool once however
// many of its names were granted (SEC-051's dedupe), and the policy gate
// asks its question in the tool's CANONICAL name (CanonicalToolName), so a
// deny row is never name-blind across the alias.
//
// # Where each tool's VOLUME budget lives (SEC-T16-11, stated at the audit)
//
// The list is audited here, so the budgets that bound what a granted tool
// can consume are named here too: each tool owns its per-call bound at its
// own boundary (bash: BashTimeout, BashStdoutCap, commandMaxLen; grep/glob:
// the match/line/byte caps and GrepMaxFileBytes; read: its offset/limit and
// rendered caps; webfetch — the ONE egress: WebfetchBodyCap kept,
// WebfetchBodyCountCeiling counted, WebfetchMaxRedirects hops, WebfetchTimeout
// end to end, WebfetchMaxURLBytes per URL). The NUMBER of calls per step is
// bounded by the step's own limits (runtime/limits.go: MaxIterations turns,
// DefaultDeadline, tools executed serially in emission order), not by a
// separate per-step egress budget — a step flooding fetches pays turns and
// deadline inside a step that is already bounded. If a dedicated per-step
// egress budget ever lands, it belongs beside these facts and this comment
// moves with it.
func Registry() map[string]Tool {
	m := map[string]Tool{
		"read": Read(),
		"list": List(),
		// S11 T15: the two search tools. Pure reads (class "safe"), behind
		// the one resolver — their `path` goes through Resolve and nothing
		// else resolves, so a path in a result is information, never a read.
		"glob": Glob(),
		"grep": Grep(),
		// S11 T16: the one egress. GET-only, no credential header ever, and
		// the redirect policy is the security control (O4). Registered is not
		// granted: DefaultGrant below does NOT contain it.
		"webfetch": Webfetch(),
		// S11 T14 wrote the body; T18's D3 rename made it the one write tool.
		canonicalWrite: Write(),
		"edit":         Edit(),
		"question":     Question(),
		// S70 MB1: the agent↔agent ask. Model-initiated like question (the
		// industry's handoffs are tools to the LLM), recognised by name, and
		// it files in the runtime after the lane check and the frozen-set
		// judgement. Registered is not granted: DefaultGrant below does NOT
		// contain it — a loop's agents cannot talk to each other until an
		// operator says so (S11's rule, inherited whole).
		"ask_agent": AskAgent(),
		// S11 T17: the within-step checklist (W20). Its list lives in the
		// toolset instance and every result carries the render, so the
		// transcript is the record and no row of any table is ever written
		// — proven by a row count in todo_test, not by review. Registered
		// is not granted: DefaultGrant below does NOT contain it.
		"todo": Todo(),
		// S24 T5: the sizing run's only report door (D9). Registered, never granted —
		// only the sizing-estimator body names it; see report_size.go.
		"report_size": ReportSize(),
		"bash":        Bash(),
		// The deliverables door: a loop's plan.md, legal.md, brief.md and tasks/
		// live in the database, filed by these four. Registered, never
		// granted by default — a body names them.
		"doc_write": DocWrite(),
		"doc_read":  DocRead(),
		"doc_edit":  DocEdit(),
		"doc_list":  DocList(),
		// The loop's index: documents, tasks, answers, tools. Registered, and granted to every loop step by the
		// daemon (protocol/step.go), because an agent that cannot see what the loop holds cannot find its inputs.
		"loop_context": LoopContext(),
		// The variable manager's agent door: registered, never granted by default.
		"env_list": EnvList(),
		"env_get":  EnvGet(),
	}
	// S38 T9: the management catalog, projected mechanically from
	// internal/records — the ONE registry entry that is a loop rather than a
	// line, because a hand-written copy of a catalog is a second catalog.
	for k, v := range recordTools() {
		m[k] = v
	}

	// S22 T6: the tenth tool, present ONLY while the retrieval resolver is
	// wired (search.go). The wiring has exactly one production caller —
	// the entry point's !noretrieval link — so the no-retrieval build has
	// no search in the registry: absent, never present-and-empty (V13, D1).
	if searchResolver != nil {
		m["search"] = Search()
	}

	// S30 T5: `graph` joins the registry only while the service seam is
	// wired — the same absent-not-refusing shape as `search` above, and
	// for the same reason: a tool that can only refuse is a door onto
	// nothing (V13).
	if wiredGraphService() != nil {
		m["graph"] = Graph()
	}

	m[aliasWriteFile] = m[canonicalWrite]
	return m
}

// ToolNames lists every tool that exists, sorted — the honest ten. The
// hidden alias is FILTERED: it is a compatibility key for pinned bytes,
// not a tool an author chooses, and a listing that showed both would be a
// second name nobody granted pretending to be a capability.
func ToolNames() []string {
	names := make([]string, 0, len(Registry()))
	for name := range Registry() {
		if name == aliasWriteFile {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// IsToolName reports whether name is a key of Registry() — the hidden
// write_file alias included, because the alias resolves to a real tool and a
// declaration naming it names that tool's capability.
//
// It exists for callers (the handler registry, T19) that must ask "does this
// string name a tool?" without pulling the whole Registry map into hand:
// the question is a yes/no, and answering it should not hand the caller the
// tool values themselves.
func IsToolName(name string) bool {
	_, exists := Registry()[name]
	return exists
}

// CanonicalToolName maps a registered key to the tool's ONE honest name:
// the alias write_file answers "write", every honest name answers itself,
// and an unknown name passes through UNCHANGED — refusing the unknown is
// BuildToolset's job upstream (a grant list naming it is a construction
// error), and on the gate path PL5's default deny answers for it anyway.
//
// The gate asks in canonical names (T19, SEC-051): the two spellings bind
// the same tool, so a deny row naming "write" must reach a call spelled
// "write_file". The dispatcher still executes exactly the name the model
// called; deduping the QUESTION cannot diverge what was answered, because
// both spellings reach one tool body.
func CanonicalToolName(name string) string {
	// Alias resolution is on every ToolAccess match. Constructing all tool
	// schemas here turns each policy comparison into a full registry build.
	if name == aliasWriteFile {
		return canonicalWrite
	}
	return name
}

// init wires the policy engine's ToolAccess matcher to this registry's honest
// names (SEC-059): a row authored write_file and a call spelled write_file
// both decide as "write" on EVERY surface — the gate, `policy why`, the
// socket's policy.why — because they all judge through the same Match
// (evaluate.go:361). This stays the ONE production call (grep-pinned by
// TestTheCanonicalizerWiringAppearsInExactlyOnePlace); the write_file literal
// stays in this file (see aliasWriteFile above) — policy asks, tools answers.
func init() {
	policy.SetToolNameCanonicalizer(CanonicalToolName)
	policy.SetToolAddressResolver(systemToolAddress)
}

// externalToolAddress answers for a name the registry does not know: an MCP tool,
// whose class lives in the operator's configuration. cmd/aeon wires it once.
var externalToolAddress func(string) string

// SetExternalToolAddress is the init-time seam for it.
func SetExternalToolAddress(fn func(string) string) { externalToolAddress = fn }

// systemToolAddress is the policy engine's resolver: a system tool's address
// from its declared meta, "" for a name the registry does not know (an MCP
// tool brings its own address on the Request).
func systemToolAddress(name string) string {
	m, ok := MetaFor(name)
	if !ok {
		if externalToolAddress != nil {
			return externalToolAddress(name)
		}
		return ""
	}
	return SystemAddress(m.Family, CanonicalToolName(name), m.Capabilities)
}

// DefaultGrant is what a step gets when nobody says otherwise.
//
// **Workspace file tools only.** Granting a shell by default would make every example in
// every document an invitation, and the person running it would not have
// chosen anything. The registration audit pins the two halves: this list is
// a subset of the registry and contains no shell and no egress
// (TestRegistryExactNames; runtime.DefaultGrantNames carries the twin the
// runtime hands out, and a test asserts the two agree).
func DefaultGrant() []string { return []string{"read", "list", "write"} }

// BuildToolset constructs exactly the granted tools.
//
// `granted` is the ONLY thing that determines what a step can call. An unknown
// name is a CONSTRUCTION ERROR, not a silent omission: a typo in a grant list
// would otherwise produce a step with fewer tools than its author intended,
// and the model would work around the absence while the author read a list
// that said the tool was there.
func BuildToolset(granted []string, tc ToolContext) (Toolset, error) {
	if tc.WorkspaceRoot == "" {
		// A programmer error, and it panics rather than returning: every tool
		// resolves against this, and a toolset built without one would refuse
		// every path at turn three with a message about the workspace.
		panic("tools: BuildToolset needs a workspace root; it comes from a validated StepRequest")
	}

	registry := Registry()
	// The grant list is validated by the one function that also answers the
	// catalog's "does this agent hold this tool" (S47 T4, TC5) — so the builder
	// and the catalog cannot disagree about what a name means, and the refusal a
	// person reads is the same sentence from both doors. `grantedSet` takes the
	// registry this call already built, and returns the list verbatim: order,
	// duplicates and alias spellings all kept, because `byName` must answer to
	// either spelling and `Granted()` is the author's list.
	names, err := grantedSet(registry, granted)
	if err != nil {
		return Toolset{}, err
	}
	set := Toolset{byName: make(map[string]Tool, len(names)), granted: names, tc: tc}

	for _, name := range names {
		set.byName[name] = registry[name]
	}
	return set, nil
}

// Specs is what the model is told exists, in grant order.
//
// Grant order rather than sorted, because the order tools are offered in is a
// weak hint about which to prefer, and the grant list is where somebody
// expressed an opinion about that.
//
// ONE tool, ONE spec (T19, SEC-051's dedupe half): a grant list naming both
// spellings of the write tool — ["write", "write_file"] — would otherwise
// offer the same tool twice under the same name, and a model shown two
// identical schemas learns nothing except that Aeon cannot count its own
// tools. The first granted spelling wins the slot; the dispatch keys
// (byName) keep BOTH spellings, because the pinned matrix says a call
// spelled either way must reach the shared body.
func (t Toolset) Specs() []model.ToolSpec {
	out := make([]model.ToolSpec, 0, len(t.byName))
	offered := make(map[string]bool, len(t.byName))
	for _, name := range t.granted {
		tool, ok := t.byName[name]
		if !ok || offered[tool.Spec.Name] {
			continue
		}
		offered[tool.Spec.Name] = true
		out = append(out, tool.Spec)
	}
	return out
}

// Granted reports the names this step may call, in order.
func (t Toolset) Granted() []string { return append([]string(nil), t.granted...) }

// RerunClass returns the declared recovery class for a granted tool. Unknown
// names remain invalid; they are not silently treated as safe.
func (t Toolset) RerunClass(name string) RerunClass {
	if tool, ok := t.byName[name]; ok {
		return tool.RerunClass()
	}
	return ""
}

// Has reports whether a tool was granted.
func (t Toolset) Has(name string) bool {
	_, ok := t.byName[name]
	return ok
}

// Call runs a granted tool, or explains that it is not granted.
//
// # Both failure modes produce a RESULT, not an error
//
// A tool the step lacks, and a tool that panicked, both come back as a
// `tool_result` with `IsError: true`. The loop appends it and continues (T8),
// because a model told what went wrong can try something else — and a step
// that died on a bad tool name would be a step that failed for a reason the
// model could have fixed.
func (t Toolset) Call(ctx context.Context, name string, input json.RawMessage) Result {
	tool, granted := t.byName[name]
	if !granted {
		// O6. The message names what IS available, because "unknown tool" on
		// its own leaves a model guessing — and a model that guesses will try
		// the same name again.
		available := "no tools at all"
		if len(t.granted) > 0 {
			available = strings.Join(t.granted, ", ")
		}
		return Errorf("there is no tool called %q in this step. The tools you have are: %s. "+
			"This step was granted those and nothing else.", name, available)
	}
	return t.runSafely(ctx, tool, input)
}

// runSafely is the panic boundary (O5).
//
// # What it catches, and what it must not
//
// It wraps the tool's EXECUTION only. A panic there — a nil map in a parser, an
// index in a formatter — becomes a tool result and the step continues: a
// daemon serving three projects should not die because one tool mishandled one
// file.
//
// It does NOT wrap construction, and that is the other half (T7's point 3). A
// nil dependency or an unknown grant panics at build time, where the stack
// names the caller. A boundary around construction too would turn every
// programmer error into "the tool failed", at turn three, with the real cause
// four frames away.
//
// The recovered value and the STACK both go in the result. The stack is
// included deliberately: the model does not need it, but the transcript is
// where somebody will look, and a panic with no stack is a bug report with no
// information.
func (t Toolset) runSafely(ctx context.Context, tool Tool, input json.RawMessage) (res Result) {
	defer func() {
		if r := recover(); r != nil {
			res = Result{
				IsError: true,
				Content: fmt.Sprintf("the %s tool failed with an internal error: %v\n\n"+
					"This is a defect in Aeon rather than a problem with the request. The step "+
					"continues; report this with the trace below.\n\n%s",
					tool.Spec.Name, r, string(debug.Stack())),
				Summary: fmt.Sprintf("%s · PANICKED: %v", tool.Spec.Name, r),
			}
		}
	}()
	return tool.Run(ctx, t.tc, input)
}
