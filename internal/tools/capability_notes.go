package tools

import "sort"

// capability_notes.go is S47 T10's half of TC3: the closed vocabulary's
// one-line MEANINGS, as data.
//
// # Why the sentence lives here and not in a screen
//
// TC3 closes the vocabulary in code so that a capability cannot arrive as a name
// somebody typed; T10's panel renders "capabilities with their one-line
// meanings" and the same rule that closes the NAMES closes the SENTENCES. A
// meaning written in the UI would be a second definition of the vocabulary — the
// day a capability's meaning changes, the code would say one thing and the
// screen another, and the disagreement would be invisible, which is the exact
// failure TC11 exists to make findable in the tool rows. So the words are the
// daemon's, sent over `tools.vocabulary`, and the panel is a renderer.
//
// # The sentences are the doc comments, once
//
// Each line below is the capability's own doc comment in `meta.go` carried into
// data — no new claim, no marketing, no restatement of what a tool does beyond
// what its implementation was read for in T1. `TestCapabilityNotesCoverTheVocabulary`
// pins the pair: every capability in `Capabilities()` has exactly one note whose
// meaning is a sentence (not empty, not the capability's own name echoed back),
// and no note names a capability outside the closed set. A tenth capability
// added to `meta.go` without a note here fails that test, which is the point:
// the vocabulary and its meanings cannot drift apart.

// CapabilityNote is one capability of the closed vocabulary with the daemon's
// own one-line meaning of it.
type CapabilityNote struct {
	// Capability is the name as declared in `meta.go`; Valid() is the authority
	// on whether it is in the vocabulary at all.
	Capability Capability
	// Meaning is the one-line sentence a person reads to understand what
	// declaring the capability would let a tool do.
	Meaning string
}

// capabilityNotes is the pair to `Capabilities()`. It is a function-built slice
// for the same reason `AllMeta` and `Registry()` are: a package-level slice
// could be appended to from anywhere, and a capability outside the closed set
// would then be reachable by a caller that trusts this file.
func capabilityNotes() []CapabilityNote {
	return []CapabilityNote{
		{CapWorkspaceRead, "Reads bytes under the step's workspace root through the one resolver."},
		{CapWorkspaceWrite, "Writes bytes under the step's workspace root."},
		{CapSpawnProcess, "Starts a process, and only behind a policy-issued allow token."},
		{CapNetworkEgress, "Bytes leave the process toward the network."},
		{CapAsksHuman, "Suspends the step until a person answers."},
		{CapMessagesAgent, "Files a durable inter-agent message and may suspend the step awaiting the answer."},
		{CapOrgRecordsRead, "Reads the management records."},
		{CapOrgRecordsWrite, "Writes the management records."},
		{CapStartsLoop, "Starts a loop."},
		{CapIndexRead, "Reads the retrieval index."},
		{CapGraphRead, "Reads the code graph."},
	}
}

// CapabilityNotes returns the whole vocabulary with its meanings, sorted by
// capability name — the list a catalog screen renders, and the list the
// completeness test checks against `Capabilities()` pair by pair.
func CapabilityNotes() []CapabilityNote {
	out := capabilityNotes()
	sort.Slice(out, func(i, j int) bool { return out[i].Capability < out[j].Capability })
	return out
}

// CapabilityMeaning returns the note's sentence for one capability, or "" for a
// value outside the vocabulary. An empty answer is honest in both directions: it
// says the capability is not one this build declares, and it never invents a
// meaning for a word it does not know.
func CapabilityMeaning(c Capability) string {
	for _, n := range capabilityNotes() {
		if n.Capability == c {
			return n.Meaning
		}
	}
	return ""
}
