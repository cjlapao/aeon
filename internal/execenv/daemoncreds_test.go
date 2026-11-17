package execenv

// The daemon's own credentials are not application secrets (EV7). These tests
// are the half of the rule the core can prove on its own — that the names come
// from the caller and are honoured exactly — plus the namespace claim the core
// makes for itself.

import (
	"strings"
	"testing"
)

func TestDaemonCredentialsAreNotReferenceable(t *testing.T) {
	// Exactly the lists the daemon will pass at T6: the provider keys from
	// `daemon`, the forge names from `forge.TokenEnvNames()`, and the extras
	// that live under a vendor-shaped name.
	provider := []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY"}
	forge := []string{"AEON_FORGE_TOKEN", "GITHUB_TOKEN", "GH_TOKEN"}

	for _, name := range []string{
		"AEON_FORGE_TOKEN", "GITHUB_TOKEN", "GH_TOKEN", // the forge identity
		"ANTHROPIC_API_KEY", "OPENAI_API_KEY", // a model provider's key
		"AEON_HTTP_KEY",           // narrow's surface key — in the AEON_ namespace
		"AEON_CARD_SIGNING_KEY",   // the card signer's key — same
		"AEON_ANTHROPIC_BASE_URL", // not a credential, but the daemon's own configuration
	} {
		if !IsDaemonCredentialEnv(name, provider, forge) {
			t.Errorf("%s is referenceable as an application secret; it is the daemon's own variable", name)
		}
		if _, r := ResolveRef(RefSpec{Name: "x", SourceKind: "daemon-env", Source: name, InjectAs: "APP_VAR"},
			ResolveEnv{Lookup: func(string) string { return resolveSentinel }, DaemonUID: 1000,
				Credentials: DaemonCredentials{Provider: provider, Forge: forge}}); r == nil ||
			r.Kind != KindDaemonCredential {
			t.Errorf("a reference at %s was accepted: %v", name, r)
		}
	}

	// An application's own variable is not the daemon's, so the list is a list
	// and not a ban on everything.
	for _, name := range []string{"DATABASE_URL", "APP_TOKEN", "MY_GITHUB_TOKEN", "anthropic_api_key"} {
		if IsDaemonCredentialEnv(name, provider, forge) {
			t.Errorf("%s is refused although it is the application's own variable", name)
		}
	}

	// Case is not a fold: on the platforms Aeon runs on, `anthropic_api_key`
	// and `ANTHROPIC_API_KEY` are different variables, and the child would get
	// the value under the name that was not checked.
	if IsDaemonCredentialEnv("anthropic_api_key", provider, forge) {
		t.Error("a lowercase name matched a provider key")
	}
	if IsDaemonCredentialEnv("ANTHROPIC_API_KEY", provider, forge) != true {
		t.Error("the exact provider name did not match")
	}

	// The empty name is not a credential: ValidateRef refuses it as empty
	// first, and a rule that swallowed "" would report a nonsense name.
	if IsDaemonCredentialEnv("", provider, forge) {
		t.Error("the empty name is not anybody's credential")
	}

	// `Extra` carries a name the daemon holds under a vendor name that is in no
	// package the daemon can name here — the shape `CURSOR_SIGNING_KEY` has.
	if IsDaemonCredentialEnv("CURSOR_SIGNING_KEY", provider, forge) {
		t.Error("an unlisted name matched without being passed in")
	}
	if !IsDaemonCredentialEnv("CURSOR_SIGNING_KEY", provider, forge, "CURSOR_SIGNING_KEY") {
		t.Error("Extra is not consulted, so a vendor-named daemon secret stays referenceable")
	}
}

func TestDaemonCredentialsHoldsMatchesTheFunction(t *testing.T) {
	// Two doors over one rule; if they ever diverge, whichever the daemon uses
	// decides the answer, and the other becomes a decoy.
	creds := DaemonCredentials{Provider: []string{"ANTHROPIC_API_KEY"}, Forge: []string{"GH_TOKEN"},
		Extra: []string{"CURSOR_SIGNING_KEY"}}
	for _, name := range []string{"ANTHROPIC_API_KEY", "GH_TOKEN", "CURSOR_SIGNING_KEY", "AEON_ANYTHING", "APP_VAR", ""} {
		if got, want := creds.Holds(name), IsDaemonCredentialEnv(name, creds.Provider, creds.Forge, creds.Extra...); got != want {
			t.Errorf("%q: Holds=%v, IsDaemonCredentialEnv=%v", name, got, want)
		}
	}
	if !(DaemonCredentials{}).empty() {
		t.Error("an empty DaemonCredentials is not empty, so the guard against an unchecked list would not fire")
	}
	if (DaemonCredentials{Extra: []string{"X"}}).empty() {
		t.Error("a populated Extra counts as no list, which would refuse every daemon-env reference")
	}
}

func TestValidateRefNamesTheVariableItRefused(t *testing.T) {
	creds := DaemonCredentials{Provider: []string{"ANTHROPIC_API_KEY"}}
	_, r := ResolveRef(RefSpec{Name: "the-app-token", SourceKind: "daemon-env", Source: "ANTHROPIC_API_KEY",
		InjectAs: "APP_TOKEN"}, ResolveEnv{Lookup: func(string) string { return "x" }, DaemonUID: 1000,
		Credentials: creds})
	if r == nil {
		t.Fatal("the reference was accepted")
	}
	s := r.Sentence()
	for _, want := range []string{"the-app-token", "ANTHROPIC_API_KEY", "refused:"} {
		if !strings.Contains(s, want) {
			t.Errorf("the refusal does not contain %q, so nobody can act on it: %s", want, s)
		}
	}
	if !strings.Contains(strings.ToLower(s), "reference a value that belongs to the application") {
		t.Errorf("the refusal has no way out in it: %s", s)
	}
}
