// The MCP plane of the surface table (S47 T6): the person's management reads and write
// doors, the deliberate import, and the five agent-scoped own-view twins that refuse a
// person session: registered, and not a person door.
package protocol

// surfaceTableMCP is one slice of the closed classification table, built fresh on every
// call: there is no package-level map to append to, so a method cannot enter the surface
// by mutating shared state — only by editing a table, which the completeness tests then
// check both ways.
func surfaceTableMCP() map[MethodName]Surface {
	return map[MethodName]Surface{
		MethodMCPChanges: {
			Kind:    SurfaceRead,
			Summary: "The append-only MCP audit: which write changed which server, by whom, from which door.",
		},
		MethodMCPCheck: {
			Kind:          SurfaceControl,
			Summary:       "An operator's single-flight dial of one configured server: initialize and tools/list, never tools/call.",
			HumanDoorOnly: true,
		},
		MethodMCPCoreSet: {
			Kind:          SurfaceWrite,
			Summary:       "Sets an agent's core MCP tool set, the one door that narrows what a step may call.",
			HumanDoorOnly: true,
		},
		MethodMCPGrantDeclare: {
			Kind:          SurfaceWrite,
			Summary:       "Declares a public MCP tool for an agent; declaring grants nothing by itself.",
			HumanDoorOnly: true,
		},
		MethodMCPGrantRevoke: {
			Kind:          SurfaceWrite,
			Summary:       "Revokes an agent MCP grant, narrowing only what the person previously opened.",
			HumanDoorOnly: true,
		},
		MethodMCPGrants: {
			Kind:    SurfaceRead,
			Summary: "The per-agent MCP grant view verbatim from the authority, available / ask / unavailable / refused with its reason.",
		},
		MethodMCPImportFile: {
			Kind:          SurfaceWrite,
			Summary:       "Deliberately adopts a legacy mcp.json into the rows, dry-run or replace, all-or-nothing and hash-idempotent.",
			HumanDoorOnly: true,
		},
		MethodMCPMineGrants: {
			Kind:    SurfaceRead,
			Summary: "This agent's own grant view \u2014 a peer private is invisible here, even in an error.",
		},
		MethodMCPMineProposeRemove: {
			Kind:    SurfaceWrite,
			Summary: "Files an inert proposal to remove a server this agent can see; only a person may act on it.",
		},
		MethodMCPMineProposeRevoke: {
			Kind:    SurfaceWrite,
			Summary: "Files an inert proposal to revoke one of this agent own grants; it can narrow, never widen.",
		},
		MethodMCPMineServers: {
			Kind:    SurfaceRead,
			Summary: "This agent's own MCP servers, scoped by the stamped identity; a person session is refused with a pointer.",
		},
		MethodMCPMineStatus: {
			Kind:    SurfaceRead,
			Summary: "This agent's own MCP availability, scoped the same way and by the same stamp.",
		},
		MethodMCPServer: {
			Kind:    SurfaceRead,
			Summary: "One MCP server as configured, with its tools, tier and last probe.",
		},
		MethodMCPServerAdd: {
			Kind:          SurfaceWrite,
			Summary:       "Adds an MCP server row through the human door, the candidate validated before any row is touched.",
			HumanDoorOnly: true,
		},
		MethodMCPServerInspect: {
			Kind:          SurfaceWrite,
			Summary:       "Runs a candidate MCP server's command, lists the tools it offers with their descriptions and schemas, and stops it; nothing is stored.",
			HumanDoorOnly: true,
		},
		MethodMCPProcesses: {
			Kind:    SurfaceRead,
			Summary: "Every MCP server's process: state, PID, start time, restarts, tools, and how it last ended.",
		},
		MethodMCPProcessLogs: {
			Kind:    SurfaceRead,
			Summary: "One MCP server's log: its stderr and the daemon's own lines about it, from a bounded ring, after a sequence number.",
		},
		MethodMCPProcessStart: {
			Kind:          SurfaceWrite,
			Summary:       "Starts one MCP server's process (a stopped, crashed or failed one) as the person's act.",
			HumanDoorOnly: true,
		},
		MethodMCPProcessStop: {
			Kind:          SurfaceWrite,
			Summary:       "Terminates one MCP server's process and keeps it stopped until the person starts it.",
			HumanDoorOnly: true,
		},
		MethodMCPProcessRestart: {
			Kind:          SurfaceWrite,
			Summary:       "Terminates and starts one MCP server's process again.",
			HumanDoorOnly: true,
		},
		MethodMCPServerRemove: {
			Kind:          SurfaceWrite,
			Summary:       "Removes an MCP server and its grant rows with it, the audit recording who asked.",
			HumanDoorOnly: true,
		},
		MethodMCPServerSetEnabled: {
			Kind:          SurfaceWrite,
			Summary:       "Turns an MCP server on or off without touching its configuration.",
			HumanDoorOnly: true,
		},
		MethodMCPServerShare: {
			Kind:          SurfaceWrite,
			Summary:       "Shares a public MCP server with every agent, or stops sharing it; a declaration only, the policy still decides use.",
			HumanDoorOnly: true,
		},
		MethodMCPServerUpdate: {
			Kind:          SurfaceWrite,
			Summary:       "Updates an MCP server row, re-validated whole before the store sees it.",
			HumanDoorOnly: true,
		},
		MethodMCPServers: {
			Kind:    SurfaceRead,
			Summary: "The person's unscoped MCP list with details \u2014 the human door sees everything, agents do not.",
		},
		MethodMCPStatus: {
			Kind:    SurfaceRead,
			Summary: "The MCP configuration as the loader sees it \u2014 absent, valid or broken \u2014 plus the cached probes.",
		},
		MethodMCPVocabulary: {
			Kind:    SurfaceRead,
			Summary: "The MCP vocabulary taken from the validator, so no client hardcodes a tier or a class.",
		},
	}
}
