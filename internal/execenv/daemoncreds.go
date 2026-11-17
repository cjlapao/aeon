package execenv

// The daemon's own credentials, and the rule that they are not application
// secrets (EV7).
//
// The daemon runs with the identities it needs to do its job: a model provider
// key, a forge token, the narrow-surface key, a card-signing key. A secret
// reference is a door from a loop's environment to a value the DAEMON holds, so
// the difference between "the app's database URL" and "the daemon's provider
// key" is the whole of EV7, and it cannot be decided from inside this package:
// which variables the daemon holds is `daemon`, `config`, `forge`, `narrow`,
// `agentsvc` and `secrets` knowledge, and none of those may be imported here
// (the purity guard treats an import of the daemon as a defect).
//
// So the names are PASSED IN. The daemon assembles the list from the constants
// it already exports — `daemon.EnvAnthropicKey`, `daemon.EnvOpenAIKey`,
// `forge.TokenEnvNames()`, `narrow.KeyEnv`, the card-signing name, the cursor
// signing name — and hands it to `ValidateRef`. A caller that passes nothing
// gets a refusal rather than a pass: an empty list checks nothing, and a
// security rule that is silently disabled by a missing argument is not a rule.
//
// The `AEON_` prefix is checked here rather than passed in because it is a
// NAMESPACE claim, not an inventory: every variable Aeon gives itself a name in
// is Aeon's, including ones added tomorrow that nobody has listed yet. The
// names from `narrow` (`AEON_HTTP_KEY`) and `agentsvc` (`AEON_CARD_SIGNING_KEY`)
// are covered by it, which is why they are not special-cased; a name outside the
// namespace — `ANTHROPIC_API_KEY`, `CURSOR_SIGNING_KEY` — must be in the lists,
// and the daemon's tests are where that obligation is pinned (T6).

import "strings"

// DaemonCredentials is the list of variable names the daemon holds for itself,
// in the shape the caller assembles it.
type DaemonCredentials struct {
	// Provider is the model-provider key set (the daemon's EnvAnthropicKey,
	// EnvOpenAIKey and whatever a new adapter adds).
	Provider []string
	// Forge is the forge token set, from forge.TokenEnvNames() (A2's exported
	// accessor, so the list is the SAME list DefaultTokenSource reads rather
	// than a copy that can drift).
	Forge []string
	// Extra is everything outside the AEON_ namespace that is still the
	// daemon's: a signing key read from a vendor-shaped name, a cloud SDK
	// credential the daemon happens to carry.
	Extra []string
}

// aeonNamespace is the prefix every Aeon-owned variable lives under.
const aeonNamespace = "AEON_"

// empty reports whether the caller supplied no list at all. Three empty slices
// is a different state from a populated one and is treated as such by
// ValidateRef, which refuses rather than passing (P3: "not checked" is not
// "checked and clean").
func (c DaemonCredentials) empty() bool {
	return len(c.Provider) == 0 && len(c.Forge) == 0 && len(c.Extra) == 0
}

// Holds reports whether name is one of the daemon's own. Matching is
// case-sensitive: on the platforms Aeon runs on, two names differing in case
// are two variables, and folding them would let `anthropic_api_key` read as
// clean while the child's `ANTHROPIC_API_KEY` still carried the value.
func (c DaemonCredentials) Holds(name string) bool {
	return IsDaemonCredentialEnv(name, c.Provider, c.Forge, c.Extra...)
}

// IsDaemonCredentialEnv is the rule on its own, for callers that hold the lists
// as separate values (the daemon builds them from its own constants and does
// not need a struct to pass them around). `extra` is variadic so a caller with
// no extras can call it with two arguments, which is the common case.
func IsDaemonCredentialEnv(name string, provider []string, forge []string, extra ...string) bool {
	if name == "" {
		return false
	}
	if strings.HasPrefix(name, aeonNamespace) {
		return true
	}
	for _, list := range [][]string{provider, forge, extra} {
		for _, n := range list {
			if n == name {
				return true
			}
		}
	}
	return false
}
