package policy

// The tool-address pattern (S74 TA5–TA9): a `ToolAccess` toolName may be an
// address pattern whose segments are literals or a whole `*`.
//
// The grammar is deliberately smaller than MatchPattern's: one wildcard, whole
// segments only, over a closed vocabulary the daemon computes (the tool
// address, `<category>.<tool>.<access>`, four segments for an MCP tool). No
// `wo*`, no `**`, no classes. The matcher below is the ONE reader of that
// grammar — the gate, the policy.matchTools preview and the in-force explorer
// all call it, and no client matches a pattern itself (TA7).

import (
	"fmt"
	"strings"
)

// IsToolAddressForm reports whether a toolName is an address pattern rather
// than a bare tool name (TA5/TA8). A bare name — `bash`, or an MCP qualified
// name `server.tool` — keeps matching exactly as it always did, including a
// stored row that holds a lone `*` (it names a tool called "*"; the write
// boundary now refuses new ones, see ValidateToolPattern). A name is an address
// pattern when it has at least three segments and either has a `*` segment or
// ends in `.read` or `.write` (a full literal address).
func IsToolAddressForm(name string) bool {
	segs := strings.Split(name, ".")
	if len(segs) < 3 {
		return false
	}
	for _, s := range segs {
		if s == "*" {
			return true
		}
	}
	last := segs[len(segs)-1]
	return last == "read" || last == "write"
}

// ValidateToolPattern returns "" when name is an acceptable toolName and a
// sentence naming the offence otherwise. A bare name is always acceptable here
// (the validator's other checks still apply); an address pattern must follow
// TA6: three segments, or four when the category is `mcp`, each a non-empty
// literal or exactly `*`, and a literal access segment must be read or write.
func ValidateToolPattern(name string) string {
	if !IsToolAddressForm(name) {
		if strings.Contains(name, "*") {
			return fmt.Sprintf("toolName %q uses * but is not an address pattern; write it as "+
				"<category>.<tool>.<access>, for example workspace.*.read", name)
		}
		return ""
	}
	segs := strings.Split(name, ".")
	if len(segs) != 3 && len(segs) != 4 {
		return fmt.Sprintf("toolName %q has %d segments; a tool address pattern is <category>.<tool>.<access> "+
			"(three segments, or four for an MCP tool: mcp.<server>.<tool>.<access>)", name, len(segs))
	}
	if len(segs) == 4 && segs[0] != "mcp" && segs[0] != "*" {
		return fmt.Sprintf("toolName %q has four segments, which only an MCP tool address has (mcp.<server>.<tool>.<access>)", name)
	}
	for i, s := range segs {
		switch {
		case s == "":
			return fmt.Sprintf("segment %d of toolName %q is empty", i+1, name)
		case s != "*" && strings.Contains(s, "*"):
			return fmt.Sprintf("segment %d of toolName %q is a partial wildcard; use a whole * or a literal", i+1, name)
		}
	}
	if acc := segs[len(segs)-1]; acc != "*" && acc != AccessRead && acc != AccessWrite {
		return fmt.Sprintf("the access segment of toolName %q is %q; it must be read, write or *", name, acc)
	}
	return ""
}

// The access vocabulary, spelled here and pinned against internal/tools by a
// test there (policy must not import tools).
const (
	AccessRead  = "read"
	AccessWrite = "write"
)

// MatchToolAddress reports whether address satisfies pattern. Pure and total:
// an empty pattern or address matches nothing, an invalid pattern matches
// nothing, and nothing here can panic.
//
// A three-segment pattern names SYSTEM tools only; it never reaches an MCP
// tool. `*.*.*` is therefore "every system tool", and a grant like it cannot
// silently cover a third-party server (operator ruling 2026-11-07, replacing
// the earlier collapse that let `*.*.read` reach MCP tools). An MCP tool is
// named by a four-segment pattern: `mcp.*.*.*` for all of them,
// `mcp.github.*.read` for one server's reads.
func MatchToolAddress(pattern, address string) bool {
	if pattern == "" || address == "" || ValidateToolPattern(pattern) != "" || !IsToolAddressForm(pattern) {
		return false
	}
	p := strings.Split(pattern, ".")
	a := strings.Split(address, ".")
	if len(p) != len(a) {
		return false
	}
	for i := range p {
		if p[i] != "*" && p[i] != a[i] {
			return false
		}
	}
	return true
}
