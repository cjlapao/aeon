package tools

// meta.go is S47 T1 (design TC3): what every tool can DO, declared in code and
// checked by a test, so a new tool cannot ship without saying so.
//
// # What this is, and what it is not
//
// `Tool` (read.go) already carries the two facts the RUNTIME needs: the spec the
// model is shown and the re-run class crash recovery acts on. This file carries
// the facts a PERSON needs when deciding whether to grant a tool: does it write
// the workspace, spawn a process, reach the network, ask a person, write an org
// record. None of it is read by any execution path — `Meta` is catalog data, and
// a tool's behaviour is unchanged by anything in this file (a test of the
// existing suites passing unedited is T1's acceptance).
//
// # The vocabulary is closed
//
// A `Capability` is one of the constants below and nothing else;
// `TestCapabilityVocabularyIsClosed` refuses a free string, so a capability that
// has not been reviewed cannot arrive as a name somebody typed. `Governed` names
// policy types and is checked against `policy.AllPolicyTypes()` rather than a
// restated list, because a policy type that does not exist gates nothing, and a
// catalog claiming it does would be a lie a person decides on.
//
// # Derived, not copied, where it can be
//
// The records-catalog tools (`recordTools`) already declare `Family`, `Mutates`
// and `Class`; their `Meta` is a FUNCTION of those, so the catalog and this file
// cannot drift into two truths — the derivation is asserted in a test. Only the
// twelve hand-written core tools (and the two conditional ones) are written here,
// each from a reading of its implementation, never from its description.
//
// # Function-built, like Registry()
//
// `AllMeta` builds a fresh map on each call. A package-level `map[string]Meta`
// could be appended to from anywhere, which is the reason `Registry()` is a
// function too; `TestMetaIsAFunctionBuiltMapNotAPackageVariable` pins it.

import (
	"sort"

	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/records"
)

// Capability is one declared thing a tool can do. The set is closed: a value
// outside it is a bug, refused by a test rather than by convention.
type Capability string

const (
	// CapWorkspaceRead: reads bytes under the step's workspace root through the
	// one resolver (read, list, glob, grep — and the record tools that pin a
	// workspace document into a row).
	CapWorkspaceRead Capability = "workspace_read"

	// CapWorkspaceWrite: writes bytes under the step's workspace root.
	CapWorkspaceWrite Capability = "workspace_write"

	// CapSpawnProcess: starts a process. Only `bash`, and only behind a
	// policy-issued allow token (spawn.go checks it as its first statement).
	CapSpawnProcess Capability = "spawn_process"

	// CapNetworkEgress: bytes leave the process. Only `webfetch`.
	CapNetworkEgress Capability = "network_egress"

	// CapAsksHuman: suspends the step until a PERSON answers.
	CapAsksHuman Capability = "asks_human"

	// CapMessagesAgent files a durable inter-agent message and may suspend the
	// step awaiting the answer. S47 T0's ONE reviewed addition to the design's
	// vocabulary (TC3 allows exactly one): `ask_agent` addresses a PEER, so
	// `asks_human` would lie, and it is no family's org record.
	CapMessagesAgent Capability = "messages_agent"

	// CapOrgRecordsRead: reads the management records.
	CapOrgRecordsRead Capability = "org_records_read"

	// CapOrgRecordsWrite: writes the management records.
	CapOrgRecordsWrite Capability = "org_records_write"

	// CapStartsLoop: starts a loop. Only `run_start`.
	CapStartsLoop Capability = "starts_loop"

	// CapIndexRead: reads the retrieval index (the conditional `search`).
	CapIndexRead Capability = "index_read"

	// CapGraphRead: reads the code graph (the conditional `graph`).
	CapGraphRead Capability = "graph_read"
)

// Capabilities returns the closed vocabulary, sorted — the list a UI renders as
// the whole of what may be declared, and the list the completeness test checks
// every declared capability against.
func Capabilities() []Capability {
	out := []Capability{
		CapWorkspaceRead,
		CapWorkspaceWrite,
		CapSpawnProcess,
		CapNetworkEgress,
		CapAsksHuman,
		CapMessagesAgent,
		CapOrgRecordsRead,
		CapOrgRecordsWrite,
		CapStartsLoop,
		CapIndexRead,
		CapGraphRead,
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Valid reports whether c is in the closed vocabulary.
func (c Capability) Valid() bool {
	for _, v := range Capabilities() {
		if v == c {
			return true
		}
	}
	return false
}

// Meta is what a tool declares about itself for the catalog.
type Meta struct {
	// Family groups the tool: "workspace", "egress", "process", "interaction",
	// "collaboration", "planning", "sizing", "retrieval", "code-graph" for the
	// hand-written ones; the records `Spec.Family` for the projected ones.
	Family string

	// Capabilities is the declared set, sorted and de-duplicated.
	//
	// It is EMPTY for exactly two tools — `todo` and `report_size` — because
	// neither writes a row, a file, a process, a socket or a message: their whole
	// effect is the step's own transcript or handoff (todo.go's list dies with the
	// toolset; report_size.go touches no store). An empty set is a DECLARATION
	// that the tool reaches nothing outside the step, and
	// `TestEmptyCapabilitySetsAreOnlyTheDeclaredTools` pins which names may carry
	// it, so a new tool arriving empty fails the suite rather than passing as
	// harmless.
	Capabilities []Capability

	// Governed names the policy types that decide this tool per call. Normally
	// just "tool_access"; "command_access" joins for `bash`. The entries are
	// checked against policy.AllPolicyTypes().
	Governed []string

	// Notes is one fixed sentence (≤ 200 chars, no control characters) a person
	// reads next to the grant — the honest qualifier, e.g. that a spawn needs a
	// policy-issued allow token.
	Notes string
}

// MetaFor returns the metadata for a tool name, canonical OR alias (`write_file`
// answers `write`'s Meta, because the gate canonicalises before it judges and a
// second row for the alias would be a second truth). The conditional tools
// (`search`, `graph`) always have a Meta, whether or not their seam is wired —
// the catalog lists what exists in code and marks availability separately.
func MetaFor(name string) (Meta, bool) {
	m := AllMeta()
	mm, ok := m[CanonicalToolName(name)]
	return mm, ok
}

// AllMeta returns a copy of the whole table, built fresh on every call.
func AllMeta() map[string]Meta {
	out := make(map[string]Meta, len(ToolNames()))
	for name, meta := range coreMeta() {
		out[name] = meta
	}
	for name, meta := range recordMeta() {
		out[name] = meta
	}
	return out
}

// coreMeta is the hand-written half: the twelve registry tools plus the two
// conditional ones. Each entry was written from the tool's implementation; the
// evidence line is in the Notes or in the comment above it.
func coreMeta() map[string]Meta {
	return map[string]Meta{
		// The workspace readers. Each resolves through the ONE resolver
		// (paths.go) and nothing else produces a path it will follow, so
		// `workspace_read` is the whole of their reach.
		"read": {
			Family:       "workspace",
			Capabilities: []Capability{CapWorkspaceRead},
			Governed:     []string{string(policy.ToolAccess)},
			Notes:        "Reads one file under the step's workspace root through the one resolver; escapes are refused at that resolver.",
		},
		"list": {
			Family:       "workspace",
			Capabilities: []Capability{CapWorkspaceRead},
			Governed:     []string{string(policy.ToolAccess)},
			Notes:        "Lists one directory under the workspace root; finds files without granting a shell, and skips symlinks.",
		},
		"glob": {
			Family:       "workspace",
			Capabilities: []Capability{CapWorkspaceRead},
			Governed:     []string{string(policy.ToolAccess)},
			Notes:        "Walks the workspace for a name pattern; only the walker produces a path, so a result path is information, never a read.",
		},
		"grep": {
			Family:       "workspace",
			Capabilities: []Capability{CapWorkspaceRead},
			Governed:     []string{string(policy.ToolAccess)},
			Notes:        "Searches file contents under the workspace root; unreadable and oversized files are counted, never silently skipped.",
		},
		// The two writers. `write` reads the previous bytes only to count changed
		// lines and that read never gates the write, so declaring it would
		// overstate what the tool needs; `edit` cannot match without reading.
		"write": {
			Family:       "workspace",
			Capabilities: []Capability{CapWorkspaceWrite},
			Governed:     []string{string(policy.ToolAccess)},
			Notes:        "Writes one file under the workspace root through the one resolver, size-capped; the hidden write_file alias is this tool.",
		},
		"edit": {
			Family:       "workspace",
			Capabilities: []Capability{CapWorkspaceRead, CapWorkspaceWrite},
			Governed:     []string{string(policy.ToolAccess)},
			Notes:        "Reads the file to match an exact unique string, then replaces it atomically; the read is load-bearing, not incidental.",
		},
		// The one egress. The file's own header names the gap: there is no
		// network policy type, so what scopes egress is the grant plus a
		// tool_access deny — saying otherwise here would claim a gate that
		// does not exist.
		"webfetch": {
			Family:       "egress",
			Capabilities: []Capability{CapNetworkEgress},
			Governed:     []string{string(policy.ToolAccess)},
			Notes:        "The loop's one egress: GET-only, no credential header ever; no host scoping exists, so a tool_access deny is the only stop.",
		},
		// The one process. Declared as spawn_process ALONE on purpose: a shell
		// can transitively read, write and fetch, and the control for that
		// transitive surface is command_access plus the one-shot allow token,
		// not this column (see the design's argument and bash.go).
		"bash": {
			Family:       "process",
			Capabilities: []Capability{CapSpawnProcess},
			Governed:     []string{string(policy.ToolAccess), string(policy.CommandAccess)},
			Notes:        "Spawns a process only with a policy-issued allow token for the exact command; a toolset wired without a policy source cannot spawn.",
		},
		// The one human door. Routed to the suspend path BEFORE the policy gate
		// (runtime/loop.go), so no policy type judges a granted `question`
		// call — the honest Governed is empty, with the grant list as its only
		// control.
		"question": {
			Family:       "interaction",
			Capabilities: []Capability{CapAsksHuman},
			Governed:     nil,
			Notes:        "Suspends the step until a person answers; it is routed before the policy gate, so the grant list is its only control.",
		},
		// The peer ask. `messages_agent` is T0's single reviewed vocabulary
		// addition: the addressee is a peer agent, so `asks_human` would be
		// false, and the message is no records family's row.
		"ask_agent": {
			Family:       "collaboration",
			Capabilities: []Capability{CapMessagesAgent},
			Governed:     []string{string(policy.ToolAccess)},
			Notes:        "Files a durable message to another agent of this loop and may suspend the step awaiting the answer.",
		},
		// The step-local checklist. Empty capability set — a declaration, not an
		// omission: the list lives in the per-toolset closure and dies with the
		// step, and no row of any table is ever written (todo_test counts them).
		"todo": {
			Family:       "planning",
			Capabilities: nil,
			Governed:     []string{string(policy.ToolAccess)},
			Notes:        "A within-step checklist held in the toolset and rendered into the transcript; it writes no row, file, or process.",
		},
		// The deliverables door (doc.go). Rows of the step's own loop, never a
		// workspace file: the daemon scopes every call by the step on the
		// context, so the model names a document and nothing that addresses a
		// loop or a goal.
		"doc_write": {
			Family:       "document",
			Capabilities: []Capability{CapOrgRecordsWrite},
			Governed:     []string{string(policy.ToolAccess)},
			Notes:        "Files a revision of one of this loop's documents in the database; append-only by revision, scoped to the step's loop by the daemon.",
		},
		"doc_edit": {
			Family:       "document",
			Capabilities: []Capability{CapOrgRecordsWrite},
			Governed:     []string{string(policy.ToolAccess)},
			Notes:        "Replaces one exact span of this loop's own document and files the result as a new revision; a goal's inherited document is refused.",
		},
		"doc_read": {
			Family:       "document",
			Capabilities: []Capability{CapOrgRecordsRead},
			Governed:     []string{string(policy.ToolAccess)},
			Notes:        "Reads the head of a document of this loop, or of the goal this loop works when the loop wrote none; scoped by the daemon.",
		},
		"loop_context": {
			Family:       "document",
			Capabilities: []Capability{CapOrgRecordsRead},
			Governed:     []string{string(policy.ToolAccess)},
			Notes:        "Shows the loop's index: its documents in the database, its tasks with their spec files, the answers the person gave and the tools the agent holds; scoped to the step's loop by the daemon.",
		},
		"doc_list": {
			Family:       "document",
			Capabilities: []Capability{CapOrgRecordsRead},
			Governed:     []string{string(policy.ToolAccess)},
			Notes:        "Lists the heads of this loop's documents and of those owned by the goal it works; scoped by the daemon.",
		},
		// The variable manager's agent door (env.go). Reads one managed variable
		// the person marked agent-visible; the daemon scopes it by the step.
		"env_list": {
			Family:       "variables",
			Capabilities: []Capability{CapOrgRecordsRead},
			Governed:     []string{string(policy.ToolAccess)},
			Notes:        "Lists the managed variables a person made available to agents, names and descriptions only; scoped to the step's project by the daemon.",
		},
		"env_get": {
			Family:       "variables",
			Capabilities: []Capability{CapOrgRecordsRead},
			Governed:     []string{string(policy.ToolAccess)},
			Notes:        "Reads one agent-visible managed variable's value and records the read under the agent's name; a hidden variable answers as an absent one.",
		},
		// The sizing run's only report. Empty for the same reason: no store, no
		// egress, no process — the handoff rides the step's own result data.
		"report_size": {
			Family:       "sizing",
			Capabilities: nil,
			Governed:     []string{string(policy.ToolAccess)},
			Notes:        "Reports one sizing range into the step's handoff, once per run; it touches no store and only a sizing handler holds it.",
		},
		// The two conditional tools. Seeded and listed always, `available`
		// computed live — absent from the registry is a wiring fact, not a
		// missing declaration (V13).
		"search": {
			Family:       "retrieval",
			Capabilities: []Capability{CapIndexRead},
			Governed:     []string{string(policy.ToolAccess)},
			Notes:        "Searches the retrieval index for file:line locations, never file contents; present only while the resolver seam is wired.",
		},
		"graph": {
			Family:       "code-graph",
			Capabilities: []Capability{CapGraphRead},
			Governed:     []string{string(policy.ToolAccess)},
			Notes:        "Answers structural questions with symbols and locations, never contents; present only while the graph service is wired.",
		},
	}
}

// recordMeta derives the projected half from the records catalog — the one
// table those tools are authored in. Family, class and the read/write half come
// mechanically from the Spec; the two declared deviations are named below.
func recordMeta() map[string]Meta {
	out := make(map[string]Meta)
	for _, sp := range records.Catalog() {
		caps := []Capability{CapOrgRecordsRead}
		if sp.Mutates {
			caps = []Capability{CapOrgRecordsWrite}
			// Deviation 1 (T0's evidence, daemon/records.go's
			// pinWorkspaceDocument): these three arms put a WORKSPACE FILE's
			// bytes into the row they write, reading through the same resolver
			// every file tool uses. Deriving from Mutates alone would hide a
			// real read.
			switch sp.Name {
			case "attach", "goal_create", "brief_create":
				caps = append(caps, CapWorkspaceRead)
			}
			// Deviation 2: the vocabulary's loop term belongs to the one
			// catalog action that starts a loop, which the mechanical rule
			// alone would label only as a row write.
			if sp.Name == "run_start" {
				caps = append(caps, CapStartsLoop)
			}
		}
		sort.Slice(caps, func(i, j int) bool { return caps[i] < caps[j] })
		out[sp.Name] = Meta{
			Family:       sp.Family,
			Capabilities: caps,
			Governed:     []string{string(policy.ToolAccess)},
			Notes:        noteSentence(sp.Description),
		}
	}
	return out
}

// noteSentence folds a catalog description into the one fixed sentence a person
// reads. Descriptions are already sentences; this only bounds the length to the
// schema's 200-char cap and strips control characters, because the field is
// rendered in the app (SEC-033) and the Notes test refuses them.
func noteSentence(description string) string {
	out := make([]rune, 0, len(description))
	for _, r := range description {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			r = ' '
		case r < 0x20 || r == 0x7f:
			continue
		}
		out = append(out, r)
	}
	s := string(out)
	if len([]rune(s)) > 200 {
		s = string([]rune(s)[:200])
	}
	return s
}
