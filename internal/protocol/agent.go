package protocol

// ════════════════════════════════════════════════════════════════════════════
// The agent-services surface (S14, T10): the socket speaks for the agents.
//
// # What this file is, and what it refuses to be
//
// Seven methods, ONE forwarding handler, and a dependency that is a plain
// func (`Table.Agents`) rather than an import: AS11's deletability is the
// whole design — `rm -r internal/agentsvc` must leave this package building,
// with only cmd/aeon ever having imported it. This file therefore contains
// NO agent logic: not a card check, not a policy verdict, not a chain
// figure. Every judgement is agentsvc's door; this file frames it in
// JSON-RPC and fans out the events.
//
// **No second listener** (P25/N7): nothing here binds, and `transport.go`
// beside it holds the same rule — the A2A wire is S21's, with TLS and a
// trust store; a port opened here would be a fourth surface to secure that
// nobody asked for, because every agent in this slice is local.
//
// # The enumeration (S19's N12 discipline: every entry states what it cannot do)
//
// The map below is the ONE enumeration. `TestMethodTableInOnePlace` pins
// that it covers exactly the seven registered names — a method added to the
// table without an entry fails, which is the point: a list that grows by
// accident is how a surface becomes unreviewable.
// ════════════════════════════════════════════════════════════════════════════

import (
	"context"
	"encoding/json"
	"fmt"
)

// AgentServiceMethod documents one registered method. `Cannot` is the entry's
// self-restriction, phrased for the person reading `--print-methods` or the
// app's help drawer — the kind of thing a list states or somebody discovers
// at 2am.
type AgentServiceMethod struct {
	Name     MethodName
	ReadOnly bool
	// Cannot is what the method will never do, and why. It is documentation
	// with a test behind it (the enumeration test), not prose beside it.
	Cannot string
	// Emits names the event kind the handler fans out after a successful
	// call, for the three lifecycle methods; empty for the reads.
	Emits string
}

// agentServiceMethods is THE enumeration. One place, seven entries.
var agentServiceMethods = []AgentServiceMethod{
	{
		Name: MethodAgentDiscover, ReadOnly: true,
		Cannot: "it cannot choose for you — it lists CANDIDATES (tags, examples, the name spelled the way the door reads them); similarity picked what to READ ABOUT at S22, and only a NAME ever picks what to call (A13). It cannot show a card the load path refuses: what is enumerable is what is invokable (AS9), and broken cards surface in `refuses`, not silence.",
	},
	{
		Name: MethodAgentCard, ReadOnly: true,
		Cannot: "it cannot serve `/.well-known/agent.json` — that path is HTTP and HTTP is S19's; locally the card is a file and this read. It cannot present securitySchemes as authority: they ride as opaque bytes nobody here parses (AS8), and first-party is never a field a card claims (AS12) — it is read from what the binary itself compiled.",
	},
	{
		Name:   MethodAgentInvoke,
		Cannot: "it cannot run the callee or wait for an answer — invocation creates a WAITING task and returns (AS4: suspension is rows, not a blocked call; a socket call that waits on a stranger is S21's problem, not this socket's). It cannot accept a policy set from params: the frozen set is read SERVER-side at the instant of creation (D12), because a caller-supplied set is a caller authoring its own permissions. It cannot skip a door: the card, the enumeration, the SkillAccess verdict for BOTH halves of the ask, the caller-identity read and the chain bound all run in agentsvc, and a refusal is that door's own sentence verbatim.",
		Emits:  "task.created",
	},
	{
		Name: MethodAgentTaskList, ReadOnly: true,
		Cannot: "it cannot rank, score or filter by fit — it lists the asks and their chain edges as the rows hold them. It cannot leak a callee's toolset to the caller (MC2): the listing names callees and skills, never what a callee can run.",
	},
	{
		Name: MethodAgentTaskShow, ReadOnly: true,
		Cannot: "it cannot present a rolled-up cost as exact when a child's cost is unknown — the rollup degrades to PARTIAL with the floors kept and the missing reasons named (three-state honesty); `known` appears only when every cost in the chain reported. It cannot re-judge a finished ask: the task carries the hash of the set it was created under (AS13), and this read shows that fact, it does not re-decide it.",
	},
	{
		Name:   MethodAgentTaskComplete,
		Cannot: "it cannot settle a task twice — the task layer answers a settled task with the refusal that names its state, and the answer a first call delivered stands (T3's idempotence, unchanged). It cannot invent a cost: a report with no cost records `unknown · cost-not-reported`, never zero (three-state honesty — zero by absence is the confident wrong answer). It cannot complete by guessing the task: the id is the callee's receipt from `agent.invoke`, and Debug-marking keeps this report on the P26 socket where a report belongs — never the S19 projection (LW8's guard: nobody remote settles a person's work).",
		Emits:  "task.completed",
	},
	{
		Name:   MethodAgentTaskFail,
		Cannot: "it cannot fail a task with a silent reason — the reason is required and it is recorded verbatim in the task's envelope (a refusal you cannot read is a refusal you cannot fix). Like complete, it is socket-only (Debug) and once-only: a settled task answers by naming its state.",
		Emits:  "task.failed",
	},
	{
		Name: MethodAgentPlugins, ReadOnly: true,
		Cannot: "it cannot import — that verb lives ONLY in the CLI and the app, because a socket caller handed the means to install the thing that bypasses the guards is R6 refused three slices running (S10 authoring, S12 MCP). S35 T4 narrowed this line for the other three: enable/disable/revoke now answer their own methods (agent.enable/disable/revoke) because moving an already-imported, already-consented agent starts something already permitted, unlike import. It cannot fetch: a package arrives as a filesystem path and a scheme is refused before any disk is touched (AP10); this read lists what is already here. It cannot set first-party: the flag is the registry's from the compiled bodies, and a package's own claim appears here only as the record that it tried (AP9). It cannot pretend an empty registry is a problem: zero imports is the normal state (AP11), and the listing says so without alarm.",
	},
	{
		Name: MethodAgentList, ReadOnly: true,
		Cannot: "it cannot show what an agent may DO — tools.grants answers that, separately, because conflating identity and authority behind one name is how a page ends up reading a fact through a door that means something else (TB1). It cannot filter: the roster is the whole surface or nothing, the same discipline agent.plugins already states and for the same reason (a filter lets a caller probe for agents it was not shown). It cannot invent a state the real code does not have: the vocabulary is `running`/`disabled`/`enabled`/`revoked`, measured from plugin.Agents() and PluginAgentStates, never a guessed `blocked`/`never_arrived`.",
	},
	// MethodAgentEnable/Disable/Revoke (S35 T4) are deliberately NOT
	// members of this slice, even though their names start with "agent." —
	// agentServiceMethods is the S14/S15/S21 agent-to-agent TASK LIFECYCLE
	// (discover/card/invoke/taskList/taskShow/taskComplete/taskFail, plus
	// agent.plugins/agent.list as read-only exceptions): its own tests
	// (TestMethodTableInOnePlace) require every non-readonly entry to
	// declare an Emits event, because an agent.invoke spawns an ASYNC task
	// a caller must subscribe to find the end of. agent.enable/disable/
	// revoke are synchronous plain writes (the result returns in the RPC
	// response itself, like loop.create) — there is no async task to
	// subscribe to, so forcing an Emits value here would invent a
	// notification no subscriber mechanism expects, purely to satisfy a
	// test literal. Their own Cannot-shaped documentation lives beside the
	// method constants in method.go instead; TestMethodTableInOnePlace
	// excludes these three by name, with this same reasoning repeated
	// there.
	// MethodToolsGrants ("tools.grants") is NOT an "agent.*"-prefixed
	// method, so it is not a member of this agent.* enumeration — it has
	// its own `Cannot`-shaped doc comment on Table.ToolGrants and on the
	// method constant itself (method.go) instead.
}

// AgentServiceMethods returns the enumeration for tests and `--print-methods`
// renderers. A copy, because the table is read by surfaces that must not be
// able to edit it.
func AgentServiceMethods() []AgentServiceMethod {
	return append([]AgentServiceMethod(nil), agentServiceMethods...)
}

// agentServicesCall builds the ONE handler shape the seven registrations
// share — frames, forwards, emits, and checks nothing where an agent
// decision lives. The method name is bound at REGISTRATION time (the
// constant beside the registration is its only source), so a caller can
// never claim to be another method; there is no name in params to trust.
func (t *Table) agentServicesCall(name MethodName) func(context.Context, *Session, json.RawMessage) (any, error) {
	var emits string
	for _, m := range agentServiceMethods {
		if m.Name == name {
			emits = m.Emits
			break
		}
	}
	return func(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
		if t.Agents == nil {
			// Half-wired is visible, never silently permissive (S31's
			// discipline, same ErrNotWired): the method exists, the seam
			// does not, and the answer says so by name.
			return nil, ErrNotWired.WithWhat("agent services are not wired — the daemon started without the entry point's agent-services seam (internal/agentsvc is present in the build but cmd/aeon did not wire Table.Agents); `aeon serve` wires it, a hand-built Table does not")
		}
		result, refusal, err := t.Agents(ctx, string(name), params)
		if err != nil {
			return nil, err // infrastructure: the dispatcher's own band
		}
		if refusal != "" {
			return nil, ErrAgentRefused.WithWhat("%s", refusal)
		}
		if emits != "" {
			// The lifecycle contract, asserted loudly: a lifecycle result
			// without its taskId is a wiring defect, and SKIPPING the event
			// is the silent half of it. Refuse the call instead — a created
			// task nobody can subscribe to is worse than a failed call.
			id, ok := agentTaskID(result)
			if !ok {
				return nil, fmt.Errorf("protocol: agent method %q returned a result with no integer taskId — the event %s cannot be emitted from a result that does not name its subject (wiring defect, refusing rather than dropping the event)", name, emits)
			}
			if t.OnEvent != nil {
				t.OnEvent(emits, map[string]any{"taskId": id})
			}
		}
		return result, nil
	}
}

// agentPluginsCall builds the ONE handler for the plugins read. Like its
// siblings it checks NOTHING about agents — the registry answers, through a
// plain injected func (AS11's deletability again: `rm -r internal/plugin`
// leaves this package building; the method answers ErrNotWired naming the
// slice, never pretending).
func (t *Table) agentPluginsCall() func(context.Context, *Session, json.RawMessage) (any, error) {
	return func(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
		if t.Plugins == nil {
			return nil, ErrNotWired.WithWhat("the plugin registry is not wired — the daemon started without the entry point's plugin seam (cmd/aeon did not wire Table.Plugins); `aeon serve` wires it, a hand-built Table does not")
		}
		result, refusal, err := t.Plugins(ctx, params)
		if err != nil {
			return nil, err
		}
		if refusal != "" {
			return nil, ErrAgentRefused.WithWhat("%s", refusal)
		}
		return result, nil
	}
}

// agentTaskID reads the integer taskId out of a JSON-ready result — through a
// marshal/unmarshal round trip so a typed struct answers too, and so the
// number is checked as JSON sees it.
func agentTaskID(result any) (int64, bool) {
	raw, err := json.Marshal(result)
	if err != nil {
		return 0, false
	}
	var probe struct {
		TaskID *int64 `json:"taskId"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil || probe.TaskID == nil {
		return 0, false
	}
	return *probe.TaskID, true
}
