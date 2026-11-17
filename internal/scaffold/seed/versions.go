package scaffoldseed

// versions.go — the per-record versions and the golden ritual (P8/GI14:
// "per-record Version guarded by a golden"), the retroseed/verifyseed
// mechanism carried over verbatim. The golden file
// testdata/templates.versions.golden holds `key version hash` per record;
// TestSeededTemplatesGolden fails when a hash moved and the version did
// not, naming the template and the sentence to act on.
//
// The ritual when a shipped template's content changes:
//
//  1. edit the template under internal/scaffold/templates/, run
//     go test ./internal/scaffold/seed/ -run TestSeededTemplatesGolden
//  2. it fails, naming the template: "content changed but version did not — bump it";
//  3. add `"<slug>": N+1` to revised() below (a NEW template's first
//     version is 1 and needs no entry here);
//  4. re-run with -update, and commit the golden's diff — so the review
//     shows exactly which shipped bodies a change rewrote.

// baseVersion is a record's version when the table below says nothing.
const baseVersion = 1

// revised holds the templates whose content has changed since the set
// first shipped. Production is empty today — templates.scaffold ships at
// version 1 across the board.
func revised() map[string]int {
	return map[string]int{}
}

// recordVersion is a record's version: the floor unless revised.
func recordVersion(key string) int {
	if v, ok := revised()[key]; ok && v >= baseVersion {
		return v
	}
	return baseVersion
}

// SetVersion is the set's release number — the maximum over its records
// (SD1), computed rather than remembered.
func SetVersion() int {
	max := baseVersion
	for _, r := range Source() {
		if r.Version > max {
			max = r.Version
		}
	}
	return max
}
