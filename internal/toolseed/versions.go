package toolseed

// versions.go — the per-record versions, and the reason they are a table rather
// than a constant: a tool's description, schema, class, capability set or twin
// is CONTENT, and content that changed without a version bump is the drift the
// whole slice exists to make visible (TC11). The golden file
// `testdata/tools.versions.golden` holds `key version hash` per record;
// `TestToolVersionsBumpWithContent` fails when a hash moved and the version did
// not, naming the tool and the sentence to act on.
//
// The ritual when a tool's content changes (including the day a sibling slice
// adds one, which changes only the *new* tool's line):
//
//  1. edit the tool, run `go test ./internal/toolseed/ -run TestToolVersionsBumpWithContent`
//  2. it fails, naming the tool: "content changed but version did not — bump it";
//  3. add `"<name>": N+1` below (a new tool's first version is 1, so its line is
//     added with 1 and needs no entry here);
//  4. re-run with `-update` and commit the golden's diff — so the review shows
//     exactly which catalog entries a code change rewrote.
//
// SetVersion is the maximum over the records (SD1), which is why no second
// "set release" constant exists to forget.

// baseVersion is a record's version when this table says nothing about it: the
// floor, and the honest answer for a tool nobody has revised yet.
const baseVersion = 1

// revised holds the tools whose content has changed since the set first shipped,
// with the version their content is published under. Production is empty today:
// `tools.core` ships at version 1 across the board, which is what makes the
// golden's first lines worth having — they are the baseline every later bump is
// diffed against.
func revised() map[string]int {
	return map[string]int{
		// S59 T5: bash's model-facing description now carries the execution-
		// environment honesty line and the redaction limits (it says the child
		// runs in a built environment, what a `[secret:<name>]` mark means, and
		// the command-length bound refused at this boundary). That is content a
		// person reading the catalog is entitled to see move.
		"bash": 2,
	}
}

// recordVersion is a record's version: the floor unless the tool was revised.
func recordVersion(name string) int {
	if v, ok := revised()[name]; ok && v >= baseVersion {
		return v
	}
	return baseVersion
}

// SetVersion is the set's release number — the maximum of its records' versions
// (SD1). It is computed, not remembered, so a record that outgrows a constant
// cannot leave the set claiming to be older than its own content.
func SetVersion() int {
	max := baseVersion
	for _, r := range Source() {
		if r.Version > max {
			max = r.Version
		}
	}
	return max
}
