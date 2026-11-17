package tools

// The tool address: `<category>.<tool>.<access>` (S74 TA1–TA4). The category is
// the tool's catalog family, the access is derived here from what the tool
// declares it can do, and the whole address is computed — never stored — so it
// cannot drift from the registry it names.

import (
	"sort"

	"github.com/cjlapao/aeon/internal/policy"
)

// The two access values. A policy pattern segment reads exactly these words.
const (
	AccessRead  = policy.AccessRead
	AccessWrite = policy.AccessWrite
)

// writeCapabilities are the declared capabilities that make a tool `write`
// (TA2). Reaching the network is a write by operator ruling (S74 Q1): a fetch
// can leak the workspace even when it changes nothing locally. Asking a person
// is a read: it changes nothing until the person answers.
var writeCapabilities = map[Capability]bool{
	CapWorkspaceWrite:  true,
	CapSpawnProcess:    true,
	CapNetworkEgress:   true,
	CapOrgRecordsWrite: true,
	CapStartsLoop:      true,
	CapMessagesAgent:   true,
}

// AccessOf answers `read` or `write` for a declared capability set. A tool with
// no capabilities (`todo`, `report_size`) reaches nothing outside the step and
// is a read.
func AccessOf(caps []Capability) string {
	for _, c := range caps {
		if writeCapabilities[c] {
			return AccessWrite
		}
	}
	return AccessRead
}

// WriteCapabilities lists the capabilities that make a tool a write, sorted, so
// a screen or a test can show the rule without holding a second copy of it.
func WriteCapabilities() []Capability {
	out := make([]Capability, 0, len(writeCapabilities))
	for c := range writeCapabilities {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// SystemAddress is a system tool's address: family, tool, access.
func SystemAddress(family, tool string, caps []Capability) string {
	return family + "." + tool + "." + AccessOf(caps)
}

// MCPAddress is an MCP tool's address: `mcp.<server>.<tool>.<access>` — four
// segments, because the server is part of what names the tool.
func MCPAddress(server, tool, access string) string {
	return "mcp." + server + "." + tool + "." + access
}
