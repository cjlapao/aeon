package protocol

// ════════════════════════════════════════════════════════════════════════════
// mcp_mine.go — S46 T8 (MG8, MG9, MC2, 2026-11-02): the five `mcp.mine.*`
// socket twins for the agent's OWN MCP view and its narrowing-only proposes.
//
// The catalog rows (internal/records' mcp_servers · mcp_grants · mcp_status ·
// mcp_propose_remove_server · mcp_propose_revoke_grant) are the ONE
// definition; these are the socket's half of the two doors, registered by
// the catalog loop itself (registerRecords) — this file carries the names
// and the wire shapes, the way autonomy_propose.go carries its twins'.
// The `mcp.mine.` namespace is the whole point: a grep and T7's guards can
// tell the AGENT-SCOPED doors apart from the person's unscoped mcp.servers /
// mcp.grants, which stay a separate path with separate callers.
//
// They are agent-scoped in BOTH directions. A tool call carries the step and
// the service stamps identity from it (OL3); a PERSON session reaching these
// names through the socket is refused with a pointer sentence naming the
// door that answers it (the daemon's personDoorRefusal half) — so these can
// never serve as an oracle for anyone but the stamped agent, and MC2's
// peer-blindness has no second door to leak through.
//
// The schemas (the catalog's, additionalProperties:false) carry NO agent,
// agent_slug, actor, source, status or kind field: a param naming another
// agent is a PARSE error, and self arrives only through the stamp. The two
// proposes create INERT rows of the two MCP narrowing kinds through
// proposal.create's own path (the shared caps, the shared validator, TD3)
// — never a config write.
// ════════════════════════════════════════════════════════════════════════════

// The five names live in method.go beside every other mcp.* const (where
// protogen's methodNames reads them — the whole protocol enumeration is one
// list, not one per door).
import (
	"encoding/json"

	"github.com/cjlapao/aeon/internal/records"
)

// RegisterMCPMine registers the five agent-scoped twins when — and only
// when — BOTH services stand: Table.Records (these are catalog actions;
// their Do IS the answer, one definition two doors) and Table.MCPAdmin (the
// rows half their scoped entries forward through). Absent otherwise, never
// registered-and-refusing (V13) — TestMcpAdminAbsentWhenSeamNil counts every
// mcp.* name on a seamless table and finds none. Returns true when all five
// stand (already or just now); false when a service is missing.
func (t *Table) RegisterMCPMine() bool {
	if t.Records == nil || t.MCPAdmin == nil {
		return false
	}
	for _, sp := range records.Catalog() {
		if !mcpMineCatalogNames[sp.Method] {
			continue
		}
		name := MethodName(sp.Method)
		if _, ok := t.methods[name]; ok {
			continue // a re-registration is a no-op, not a second door
		}
		t.register(&Method{Name: name, RequiresInitialize: true, Handle: t.handleRecord(sp)})
	}
	return true
}

// MineProposeReady is the answer of the seam's visibility-and-shape half
// (cmd/aeon's scoped propose entries): the proposal KIND and the canonical
// payload, ready for the daemon's ONE inert-proposal create path (caps, the
// shared validator, the stamped author). The seam writes nothing — an
// invisible server and a nonexistent one both arrive back as the same
// refusal sentence, and a payload is built only from the caller's own view.
type MineProposeReady struct {
	Kind    string          `json:"kind"`
	Payload json.RawMessage `json:"payload"`
}

// MineServersParams — the three reads take nothing: the view IS the caller,
// and an empty strict object is the honest shape (a smuggled agent param
// meets DisallowUnknownFields at the service's decode).
type MineServersParams struct{}

// MineGrantsParams is mcp.mine.grants' params — empty, same reasoning.
type MineGrantsParams struct{}

// MineStatusParams is mcp.mine.status' params — empty; it reads the cached
// probes for exactly the caller's own servers and spawns nothing.
type MineStatusParams struct{}

// MineProposeRemoveParams is mcp.mine.proposeRemove's params: the server and
// the why. No agent field — self is the stamp; a name that is not in the
// caller's own view is refused "no such server", identically whether it
// exists somewhere else or nowhere at all (no existence oracle).
type MineProposeRemoveParams struct {
	Server string `json:"server"`
	Reason string `json:"reason"`
}

// MineProposeRevokeParams is mcp.mine.proposeRevoke's params: the declared
// pair, split (the qualified name is assembled here, never typed whole so
// a dot in a tool name cannot forge a boundary), and the why.
type MineProposeRevokeParams struct {
	Server string `json:"server"`
	Tool   string `json:"tool"`
	Reason string `json:"reason"`
}

// MineTransport is the transport SUMMARY an agent may see of its own
// server: type, command/url, and the env NAMES — the values are a process
// secret and never leave the dispatch side (§4c's rule, MCP's half).
type MineTransport struct {
	Type    string   `json:"type"`
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`
	URL     string   `json:"url,omitempty"`
	Env     []string `json:"env,omitempty"`
}

// MineTool is one tool of a visible server with its classes — the same
// rerun/effect vocabulary the person's reads carry.
type MineTool struct {
	Name   string `json:"name"`
	Rerun  string `json:"rerun"`
	Effect string `json:"effect"`
}

// MineServer is one server in the agent's own view. Enabled is the row's
// state (three-state honesty for the probe lives in MineStatus, not here).
type MineServer struct {
	Name      string        `json:"name"`
	Tier      string        `json:"tier"`
	Transport MineTransport `json:"transport"`
	Enabled   bool          `json:"enabled"`
	Tools     []MineTool    `json:"tools"`
}

// MineServersResult is mcp.mine.servers' answer.
type MineServersResult struct {
	Servers []MineServer `json:"servers"`
}

// MineGrantsResult is mcp.mine.grants' answer — the SAME MCPGrantView shape
// the person's per-agent views use (one view type, two doors; a second
// shape would be a second truth).
type MineGrantsResult struct {
	Agent  string         `json:"agent"`
	Grants []MCPGrantView `json:"grants"`
}

// MineStatus is one server's reachability from the CACHE only: probed
// carries the cached verdict and its time; a server nobody has probed is
// unchecked — its own honest state, never an assumed-down or an assumed-up.
type MineStatus struct {
	Server    string `json:"server"`
	State     string `json:"state"` // "ok" | "down" | "unchecked"
	Error     string `json:"error,omitempty"`
	CheckedAt string `json:"checkedAt,omitempty"`
}

// MineStatusResult is mcp.mine.status' answer.
type MineStatusResult struct {
	Servers []MineStatus `json:"servers"`
}
