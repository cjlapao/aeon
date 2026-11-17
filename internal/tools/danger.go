package tools

// danger.go — which tools ask a person before they run, by default.
//
// The shipped first-party agents hold every tool, with this closed set the exception: each of these
// reaches outside the step (a shell, a file write, the network, another agent) and so asks first. The
// list is code, not a table, because it is a product judgement a reviewer should see in a diff, and
// every consumer (the shipped policy seed, the Tools page) reads it from here rather than keeping a copy.
// MCP tools are all in the set: a third-party server's tool is not something the product can vouch for.

// dangerousTools are the system tools that ask by default. Each name is a registered tool
// (TestDangerousToolsAreRegistered holds that: a rename must not silently drop a tool out of the set).
var dangerousTools = []string{"bash", "write", "edit", "doc_write", "doc_edit", "webfetch", "ask_agent"}

// DangerousTools returns the system tools that ask by default, as a copy.
func DangerousTools() []string { return append([]string(nil), dangerousTools...) }

// MCPDangerousPattern is the ToolAccess address pattern covering every MCP tool.
const MCPDangerousPattern = "mcp.*.*.*"

// IsDangerousTool reports whether a bare system tool name asks by default. An MCP tool is named by its
// server (see MCPDangerousPattern), so a bare name is never an MCP tool here.
func IsDangerousTool(name string) bool {
	canonical := CanonicalToolName(name)
	for _, d := range dangerousTools {
		if d == canonical {
			return true
		}
	}
	return false
}
