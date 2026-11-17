package envseed

// decl.go — the declaration of `environments.standard` (S59 T6, EV19): one
// INERT set, one record, the environment a fresh install needs in order to run
// anything at all.
//
// # Why a seed and not a code default
//
// EV2 forbids a built-in fallback: a command with no binding is refused, and
// "we quietly used the safe one instead" is the sentence after which a
// workspace runs with something nobody chose. So the safe default has to be a
// RECORD — a row a person can see, edit, archive and audit — and the thing that
// writes it at first start has to be the seed engine and not a door.
// `origin='system'` is what tells a reader which of the two it was, and only
// `internal/store`'s seed write path stamps it.
//
// # Why INERT is enough
//
// The set applies at daemon start with no human in the loop, which is only
// legitimate because what it writes cannot widen anything: it creates a row
// that does not exist, and binds the workspace scope ONLY when nothing is bound
// there (the first-binding rule, `store.SeedWorkspaceBindingTx`). Once a person
// has bound the workspace, the seed has no power over it — a restart does not
// move their choice back to the shipped default. Divergence is reported by the
// ledger and never repaired silently.
//
// # Why a function and not an init()
//
// `agentseed` and `watchdogseed` register at init; `policyseed`, `mcpseed` and
// `toolseed` do not, and this set follows the second precedent (S45 T12, S46
// T10, S47 T3 recorded it three times): the package-global registry is also
// what test engines build against with their own appliers maps, and
// `seed.NewEngine` refuses a registry set with no applier — so a domain that
// widens the global registry from a distance bricks those builds. The one
// engine every door holds is `daemon.NewSeedEngine`, and that is where this
// declaration is mounted.

import "github.com/cjlapao/aeon/internal/seed"

const (
	// SetID is the set's name — the string the ledger, `seed.status`, the
	// Settings panel and every door name it by. The literal lives once, here.
	SetID = "environments.standard"

	// SetVersion is the set's release number. It bumps with ANY content
	// change — the spec is content, so a passthrough edit arriving from
	// `execenv.DefaultPassthrough` is a version bump and a golden-test failure,
	// never a silent change to what a fresh install ships.
	SetVersion = 1

	// SystemScrubbedSlug is the slug the whole plane refers to by: EV6's
	// downgrade target, the resolver's `scrubbedSystem`, the row a person is
	// pointed at when a legacy binding is refused. Exported because the
	// daemon's resolver and this set must agree on it without either importing
	// the other's private constant — the same string in two places is how the
	// safe environment quietly goes missing.
	SystemScrubbedSlug = "host-scrubbed"
)

// Declaration is the set's declaration: INERT, records from `Source`.
//
// `Records` is `Source` itself and not a snapshot: the engine calls it at apply
// time, so the row a start writes is the one compiled into the binary right
// then — which is the difference between "the daemon ships a default" and "the
// daemon remembers a default it once shipped".
func Declaration() seed.Set {
	return seed.Set{
		ID:      SetID,
		Class:   seed.ClassInert,
		Version: SetVersion,
		Records: Source,
	}
}
