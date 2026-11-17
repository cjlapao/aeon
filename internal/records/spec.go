// Package records is the management catalog as PURE DATA: every action a
// granted agent may take on the org records (projects, components, epics,
// goals, briefs, attachments) and its socket twin's name, in one table the
// daemon implements and the tools project.
//
// It imports the standard library and nothing else — a test asserts the
// dependency list never contains internal/store or internal/tools, so both
// sides may depend on this package and neither can smuggle the other's
// world in. The catalog is data because it must be read three ways (as tool
// specs, as socket methods, as a policy vocabulary) and a function can drift
// between those readings while a table cannot.
package records

import "encoding/json"

// Spec is one catalog action, described once.
type Spec struct {
	// Name is the tool name, snake_case ("goal_create"). The name is the
	// grant vocabulary: a step's tool list speaks these, and a name not in
	// the catalog cannot be granted at all.
	Name string

	// Method is the socket twin's name ("goal.create") — the SAME action
	// through the CLI/daemon socket (T15 projects it; the catalog pins the
	// pairing so the two doors can never drift into two actions).
	Method string

	// Family groups the action for policy and docs.
	Family string

	// Description is what the model reads. The registry test enforces at
	// least 40 characters: a tool the model cannot tell apart from another
	// gets called wrong, and the call is the test.
	Description string

	// Schema is the JSON schema of the input. "additionalProperties": false
	// is not decoration — the identity fields (project, loop, component,
	// author) are NOT in any schema, and a strict schema is what makes an
	// attempt to smuggle one a parse error rather than an ignored field.
	Schema json.RawMessage

	// Class is "safe" or "detectable" — never "unsafe": every catalog action
	// is a row write in a transaction the store owns; none spawns a process
	// or touches the network, so none needs a crash-receipt ceremony.
	Class string

	// Mutates marks the writes, so a read-only projection (and the socket
	// side, T15) can filter without a name list.
	Mutates bool
}
