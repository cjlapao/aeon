package execenv

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
)

// The leak and its cure, in one file. Today a child inherits everything
// (`cmd.Env = nil`); after S59 the child gets what a spec named, and the only
// way to get "everything" is to say `LegacyInherit()` out loud.

// hostileEnv is the daemon environment a real daemon has: the allowlisted
// basics plus every credential the process happens to hold. If a single one of
// these reaches a child, the slice has failed.
func hostileEnv() (DaemonEnv, map[string]string) {
	m := map[string]string{
		"PATH": "/usr/bin:/bin", "HOME": "/home/daemon", "USER": "cjlapao",
		"LANG": "en_US.UTF-8", "LC_ALL": "en_US.UTF-8", "LC_CTYPE": "UTF-8", "TZ": "Europe/London",
		// Everything below must NOT reach a scrubbed child.
		"ANTHROPIC_API_KEY":     "sk-ant-sentinel-not-a-real-key",
		"OPENAI_API_KEY":        "sk-proj-sentinel-not-a-real-key",
		"AEON_FORGE_TOKEN":      "ghp_sentinel_not_a_real_token",
		"GITHUB_TOKEN":          "ghp_sentinel_two",
		"GH_TOKEN":              "ghp_sentinel_three",
		"AEON_HTTP_KEY":         "http-key-sentinel",
		"AEON_CARD_SIGNING_KEY": "card-sentinel",
		"AWS_PROFILE":           "prod",
		"LD_PRELOAD":            "/tmp/evil.so",
		"BASH_ENV":              "/tmp/evil.sh",
		"SSH_AUTH_SOCK":         "/run/user/1000/gnupg/S.gpg-agent.ssh",
		"CURSOR_SIGNING_KEY":    "cursor-sentinel",
		"LCX_NOT_A_LOCALE":      "trap",
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return DaemonEnv{
		Lookup: func(name string) string { return m[name] },
		Names:  func() []string { return keys },
	}, m
}

func TestBuildUsesOnlyAllowlistedAndInjected(t *testing.T) {
	de, hostile := hostileEnv()
	s := Spec{Env: EnvSection{
		Passthrough: []string{"PATH", "HOME", "USER", "LANG", "TZ"},
		Set:         map[string]string{"CI": "1", "TERM": "dumb"},
	}}
	child, r := Build(s, de, nil)
	if r != nil {
		t.Fatalf("Build: %s", r.Sentence())
	}
	got := child.Env()

	want := []string{"CI=1", "HOME=/home/daemon", "LANG=en_US.UTF-8", "PATH=/usr/bin:/bin",
		"TERM=dumb", "TZ=Europe/London", "USER=cjlapao"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("the child got:\n%v\nwant:\n%v", got, want)
	}
	// Sorted, so a diff of what a step saw is readable.
	if !sort.SliceIsSorted(got, func(i, j int) bool {
		return got[i] < got[j]
	}) {
		t.Errorf("Env() is not sorted: %v", got)
	}
	// Every credential the daemon holds is absent — by name, not just by value,
	// because a `FOO=` entry leaks that the daemon has a FOO. The allowlisted
	// basics are asserted by exact value above; the names below are the ones
	// whose presence anywhere is the bug this slice exists to fix.
	for _, name := range credentialNames {
		value := hostile[name]
		for _, kv := range got {
			if strings.HasPrefix(kv, name+"=") {
				t.Errorf("%s reached the child: %q", name, kv)
			}
			if value != "" && strings.Contains(kv, value) {
				t.Errorf("the value of %s reached the child: %q", name, kv)
			}
		}
	}
}

// credentialNames is the part of the hostile environment that must never reach
// a child. It is kept separate from the map so the test says WHICH names it
// means, instead of asserting that the allowlisted basics are absent from a
// child that was built to receive them.
var credentialNames = []string{
	"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "AEON_FORGE_TOKEN", "GITHUB_TOKEN", "GH_TOKEN",
	"AEON_HTTP_KEY", "AEON_CARD_SIGNING_KEY", "AWS_PROFILE", "LD_PRELOAD", "BASH_ENV",
	"SSH_AUTH_SOCK", "CURSOR_SIGNING_KEY",
}

func TestBuildPatternPassthrough(t *testing.T) {
	de, _ := hostileEnv()

	t.Run("LC_* matches LC_ALL and LC_CTYPE and nothing else", func(t *testing.T) {
		s := Spec{Env: EnvSection{Passthrough: []string{"LC_*"}}, Home: HomeReal,
			Egress: Egress{Mode: "unenforced"}}
		child, r := Build(s, de, nil)
		if r != nil {
			t.Fatalf("Build: %s", r.Sentence())
		}
		got := strings.Join(child.Env(), "\n")
		if !strings.Contains(got, "LC_ALL=") || !strings.Contains(got, "LC_CTYPE=") {
			t.Errorf("LC_* did not pass the locales through: %s", got)
		}
		// The trap variable differs from the prefix only by the underscore.
		if strings.Contains(got, "LCX_NOT_A_LOCALE") {
			t.Errorf("LC_* matched LCX_NOT_A_LOCALE — the pattern ignored its own underscore: %s", got)
		}
	})

	t.Run("a pattern with no way to list names is refused, not silently empty", func(t *testing.T) {
		lookupOnly := DaemonEnv{Lookup: func(string) string { return "/usr/bin" }}
		s := Spec{Env: EnvSection{Passthrough: []string{"LC_*"}}}
		_, r := Build(s, lookupOnly, nil)
		if r == nil {
			t.Fatal("a PREFIX_* pattern with DaemonEnv.Names == nil was accepted; it matches nothing, " +
				"which is a locale silently stopped being passed")
		}
		if r.Kind != RefusalEnvironmentUnavailable {
			t.Errorf("kind = %q, want %q", r.Kind, RefusalEnvironmentUnavailable)
		}
	})

	t.Run("an absent variable is absent, not an empty string", func(t *testing.T) {
		s := Spec{Env: EnvSection{Passthrough: []string{"PATH", "USER", "LOGNAME"}}}
		child, r := Build(s, de, nil)
		if r != nil {
			t.Fatalf("Build: %s", r.Sentence())
		}
		for _, kv := range child.Env() {
			if kv == "LOGNAME=" || kv == "LOGNAME" {
				t.Errorf("LOGNAME is unset in the daemon and the child was given an empty one: %v", child.Env())
			}
		}
	})
}

func TestInjectedSecretReplacesSameNamedVariable(t *testing.T) {
	de, _ := hostileEnv()
	s := Spec{Env: EnvSection{
		Passthrough: []string{"PATH"},
		Set:         map[string]string{"DATABASE_URL": "postgres://public"},
		Secrets:     []string{"db-url"},
	}}
	child, r := Build(s, de, []Injection{
		NewInjection("db-url", "DATABASE_URL", newSecret("db-url", []byte("postgres://user:pw@inner"))),
	})
	if r != nil {
		t.Fatalf("Build: %s", r.Sentence())
	}
	if got := strings.Join(child.Env(), "\n"); !strings.Contains(got, "DATABASE_URL=postgres://user:pw@inner") {
		t.Errorf("the injected secret did not replace the literal set value: %s", got)
	}
	if strings.Contains(strings.Join(child.Env(), "\n"), "postgres://public") {
		t.Errorf("the literal value survived beside the injected one: %v", child.Env())
	}
	// The replacement is RECORDED, so the preview can say what shadowed what.
	var found bool
	for _, v := range child.Names() {
		if v.Name == "DATABASE_URL" {
			found = true
			if v.Source != "secret:db-url" {
				t.Errorf("source = %q, want secret:db-url — a silent shadow is the bug this pins", v.Source)
			}
		}
	}
	if !found {
		t.Error("Names() lost the replaced variable entirely")
	}
}

func TestBuildRefusesAnEmptySecretValue(t *testing.T) {
	de, _ := hostileEnv()
	_, r := Build(Spec{}, de, []Injection{NewInjection("blank", "BLANK_VAR", newSecret("blank", nil))})
	if r == nil {
		t.Fatal("an empty secret was injected; it cannot be redacted either (EV7)")
	}
	if r.Kind != RefusalSecretUnresolvable {
		t.Errorf("kind = %q, want %q", r.Kind, RefusalSecretUnresolvable)
	}
}

func TestChildEnvZeroValueIsInert(t *testing.T) {
	var zero ChildEnv
	if !zero.IsZero() {
		t.Error("the zero value does not report IsZero")
	}
	if zero.Legacy() {
		t.Error("the zero value is not the legacy value — the two must not be confusable")
	}
	if _, r := zero.TryEnv(); r == nil {
		t.Error("TryEnv on the zero value returned no refusal; Spawn would spawn with no environment")
	} else if !strings.Contains(r.Sentence(), "no execution environment was resolved") {
		t.Errorf("the refusal does not say what went wrong: %s", r.Sentence())
	}

	defer func() {
		if rec := recover(); rec == nil {
			t.Error("Env() on the inert value returned instead of panicking — a silent empty environment " +
				"is the failure mode this type exists to prevent")
		} else if !strings.Contains(sfmt(rec), "never built") {
			t.Errorf("the panic does not name the bug: %v", rec)
		}
	}()
	_ = zero.Env()
}

func TestLegacyInheritIsNamedAndDistinct(t *testing.T) {
	leg := LegacyInherit()
	if !leg.Legacy() || leg.IsZero() {
		t.Error("LegacyInherit is not a built, legacy value")
	}
	if got := leg.Env(); got != nil {
		t.Errorf("LegacyInherit.Env() = %v, want nil (nil cmd.Env is Go's inherit)", got)
	}
	if _, r := leg.TryEnv(); r != nil {
		t.Errorf("TryEnv refused the legacy value: %s", r.Sentence())
	}
	// Its Names() says "everything, inherited" rather than pretending to know.
	names := leg.Names()
	if len(names) != 1 || names[0].Source != SourceLegacy {
		t.Errorf("legacy Names() = %+v, want one entry sourced %q", names, SourceLegacy)
	}
}

func TestNamesCarryNoValues(t *testing.T) {
	// Names() is what `environment.effective` and the step record print. A
	// value in it is the leak, in the preview, on the way to a screen.
	de, hostile := hostileEnv()
	secret := "sup3rs3cr3t-value-not-a-real-one"
	s := Spec{Env: EnvSection{
		Passthrough: []string{"PATH", "HOME"},
		Set:         map[string]string{"PUBLIC_BANNER": "hello"},
	}}
	child, r := Build(s, de, []Injection{NewInjection("banner", "BANNER", newSecret("banner", []byte(secret)))})
	if r != nil {
		t.Fatalf("Build: %s", r.Sentence())
	}
	raw, err := json.Marshal(child.Names())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	text := string(raw)
	if strings.Contains(text, secret) {
		t.Errorf("Names() JSON carries the injected value: %s", text)
	}
	for name, value := range hostile {
		if value == "" {
			continue
		}
		if strings.Contains(text, value) {
			t.Errorf("Names() JSON carries the value of %s: %s", name, text)
		}
	}
	// The names ARE there — the preview's whole job is to answer "what could
	// this step see?" without answering "to what?". Not even a non-secret
	// literal's value travels: one rule, no exceptions to remember.
	if !strings.Contains(text, "PUBLIC_BANNER") {
		t.Errorf("Names() lost the name of a set variable: %s", text)
	}
	if strings.Contains(text, "hello") {
		t.Errorf("Names() carries the value of a `set` variable; the rule is names-only, with no "+
			"exception for values that look harmless: %s", text)
	}
	if !strings.Contains(text, "secret:banner") {
		t.Errorf("Names() does not record which reference injected BANNER: %s", text)
	}
}

func sfmt(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if e, ok := v.(error); ok {
		return e.Error()
	}
	return jsonMust(v)
}

func jsonMust(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return "<unrenderable>"
	}
	return string(raw)
}
