// Package blueprintseed is the domain side of the seed engine for the
// shipped blueprints (S48 T5, BP5/BP6): blueprints.standard, an inert set
// whose records are the embedded YAML bytes, applied at daemon start AFTER
// adoption (T4) and before loops are accepted. The engine decides
// apply/adopt/update/skip; this package decides only the domain question —
// what the shipped bytes are and whether a live row is adoptable — and it
// writes rows through exactly one door, store.BlueprintPutTx.
//
// It imports internal/blueprint (the embedded bytes) and internal/seed
// (the contract); it imports no internal/mcp, no policy, no database/sql
// (the daemon adapts the applier, the agentseed precedent).
package blueprintseed

// The previously recognised pristine hashes (S48 T5): moved VERBATIM from
// internal/blueprint/standard.go's pristineUpgrades table, so what that
// table recognised is recognised here too — TestPristineListsMatch is the
// proof nothing was lost in the move. Only bytes a release actually shipped
// (sha256 of a committed embedded revision, never a guess) count as
// pristine: an operator's edit — even one descended from a shipped revision
// — is never replaced (BP6).
//
// This slice is now the production source of the fact; standard.go keeps a
// copy only inside its legacy file resolver until T11 retires the `path`
// verb, and TestNoProductionReaderOfPristineUpgrades pins that no
// seed/store/daemon path reads the old one.
var pristineUpgrades = map[string][]string{
	"two-step": {legacyTwoStepWithoutReworkSHA256, legacyTwoStepWithReworkSHA256},
	"dev-loop": {"8af923d7a96748ed61dbaf9147138753da8ce8076174ffce4ec00143fc88e6ed"},
}

// The two historical two-step revisions, carried byte-for-byte from the
// retired table.
const (
	legacyTwoStepWithoutReworkSHA256 = "ab10e66980f167b64001bbc4bf95628ac22c4ea0d7860eefc121257907ec079e"
	legacyTwoStepWithReworkSHA256    = "4f73e0eef6f4218b28fa78c2cb7732010e0a52ae592c15580b4551e23d491554"
)

// PristineHashes lists the recognised pristine hashes for a standard name —
// the ONE production list now (T4's adoption classifier and this set's
// Adoptable both read it here).
func PristineHashes(name string) []string {
	return append([]string(nil), pristineUpgrades[name]...)
}

// KnownPristineHash reports whether a hash is recognised as pristine for
// ANY shipped name (the classifier's quick membership test).
func KnownPristineHash(hash string) bool {
	for _, list := range pristineUpgrades {
		for _, h := range list {
			if h == hash {
				return true
			}
		}
	}
	return false
}
