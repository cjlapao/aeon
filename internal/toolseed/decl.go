package toolseed

// decl.go — the declaration of `tools.core`: one inert set, records from
// `Source`, version the maximum of its records (SD1).
//
// # Why a function and not an init()
//
// `agentseed` and `watchdogseed` register at init, and `policyseed`/`mcpseed`
// do not — S45 T12 and S46 T10 recorded why, and this set follows the second
// precedent: the package-global registry is also what test engines build
// against with their own appliers maps, and `seed.NewEngine` refuses a registry
// set with no applier, so a domain that widens the global registry from a
// distance bricks those builds. `internal/seed`'s own
// `TestDefaultRegistryHasOnlyDeclaredSets` pins that the global registry is
// **empty**, and it must stay empty for a package that has not asked.
//
// The one engine every door holds is `daemon.NewSeedEngine`, and that is where
// this declaration is mounted — which is why
// `internal/daemon/tool_seed_test.go/TestToolsCoreIsDeclaredInEveryDoorsEngine`
// asserts presence through `seed.status` (the person-facing enumeration) rather
// than by importing this package into `internal/seed`, which would be an import
// cycle.

import "github.com/cjlapao/aeon/internal/seed"

// SetID is the set's name — the string the ledger, `seed.status`, the Settings
// panel and every door name it by. The literal lives once, here (the
// policyseed/mcpseed stance).
const SetID = "tools.core"

// Declaration is the set's declaration: **inert**, records from `Source`.
//
// `Records` is `Source` itself and not a snapshot — the engine calls it at apply
// time, so the rows a start writes are the tools compiled in right then. That is
// what makes a sibling slice adding a tool a one-record change with no migration
// and no new mechanism (S43-extra SD12's promise, exercised here).
func Declaration() seed.Set {
	return seed.Set{
		ID:      SetID,
		Class:   seed.ClassInert,
		Version: SetVersion(),
		Records: Source,
	}
}
