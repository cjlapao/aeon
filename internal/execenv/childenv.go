package execenv

// The child environment: a value that can only be BUILT, never assembled by
// hand (EV5). This type is the whole reason the slice exists.
//
// Before S59 the child got `cmd.Env = nil`, which in Go means "inherit
// everything", which in practice meant a model-authored command run by a
// daemon holding provider keys and a forge token could print them. An options
// struct with a zero-value default is a door — a forgotten field is a
// permissive answer. So: unexported fields, one constructor that validates,
// and a positional parameter at the spawn site (F3's argument applied again).

import (
	"sort"
	"strings"
)

// Var is one variable's NAME and the source it came from, with its value kept
// out of reach. Names() hands these to the step record (EV9) and the effective
// preview (EV18), and neither can print a value because neither has one.
type Var struct {
	Name   string `json:"name"`
	Source string `json:"source"`
	value  string
}

// The source labels. `secret:<name>` is a prefix, not a constant: the record
// says WHICH reference put the variable there.
const (
	SourcePassthrough = "passthrough"
	SourceSet         = "set"
	SourceSecret      = "secret"
	SourceLegacy      = "legacy"
)

// ChildEnv is an opaque, built environment. The zero value is inert: it is
// what a caller who forgot to resolve gets, and Spawn refuses it.
type ChildEnv struct {
	vars   []Var
	legacy bool
	built  bool
}

// DaemonEnv is the daemon's own environment, handed IN as two functions
// (EV5's "never os.Getenv inside").
//
// `Lookup` answers a name. `Names` lists what is there, which a `PREFIX_*`
// pattern needs: an allowlist that can only answer questions cannot discover
// that the daemon has `LC_ALL` and `LC_CTYPE`. Passing a Lookup with no Names
// and asking for a pattern is refused rather than silently matching nothing —
// a locale that quietly stops being passed is a UTF-8 shell turned into ASCII,
// discovered by a user three hours later.
type DaemonEnv struct {
	Lookup func(string) string
	Names  func() []string
}

// Build produces the child environment from a spec: passthrough by name and by
// `PREFIX_*` pattern, then the literal `set`, then the injected secrets. The
// output is sorted, so two runs of the same spec on the same daemon
// environment are byte-identical and a diff of what a step saw is readable.
//
// Every name crosses the deny list HERE, not only in Validate: Validate is
// the door that tells a person their spec is wrong, and Build is the last
// place that can be sure a value that reached a child was never one of these.
// A secret reference whose `inject_as` is `LD_PRELOAD` would otherwise have
// walked straight past a spec that was written before the deny list grew.
func Build(spec Spec, daemonEnv DaemonEnv, injected []Injection) (ChildEnv, *Refusal) {
	gathered := map[string]Var{}

	for _, entry := range spec.Env.Passthrough {
		name := strings.TrimSpace(entry)
		if name == "" {
			continue
		}
		if strings.HasSuffix(name, "*") {
			// A pattern is not a variable name, so it does not cross the NAME
			// rules — only its prefix crosses the deny list, and every match
			// does so individually.
			prefix := strings.TrimSuffix(name, "*")
			if r := refusePrefix(prefix); r != nil {
				return ChildEnv{}, r
			}
			if daemonEnv.Names == nil {
				return ChildEnv{}, NewRefusal(RefusalEnvironmentUnavailable,
					"the passthrough pattern "+name+" cannot be resolved",
					"this daemon was given a lookup function but no way to list the daemon environment's names, and a prefix pattern has nothing to match against",
					"pass DaemonEnv.Names (the daemon's environment is read OUTSIDE this package and handed in), or list the variables explicitly")
			}
			for _, cand := range daemonEnv.Names() {
				if !strings.HasPrefix(cand, prefix) || cand == "" {
					continue
				}
				if r := refuseName(cand, SourcePassthrough, false); r != nil {
					return ChildEnv{}, r
				}
				if v, ok := lookupValue(daemonEnv, cand); ok {
					gathered[cand] = Var{Name: cand, Source: SourcePassthrough, value: v}
				}
			}
			continue
		}
		if r := refuseName(name, SourcePassthrough, false); r != nil {
			return ChildEnv{}, r
		}
		if v, ok := lookupValue(daemonEnv, name); ok {
			gathered[name] = Var{Name: name, Source: SourcePassthrough, value: v}
		}
	}

	for name, value := range spec.Env.Set {
		if r := refuseName(name, SourceSet, true); r != nil {
			return ChildEnv{}, r
		}
		gathered[name] = Var{Name: name, Source: SourceSet, value: value}
	}

	// Secrets last, so an injected reference REPLACES a same-named literal
	// rather than being shadowed by it — and the replacement is RECORDED, so
	// `environment.effective` can say `set` was overridden by `secret:db-url`
	// instead of the two silently disagreeing about what the child has.
	for _, inj := range injected {
		// The injection door's own deny rule (S59 T6): a credential may arrive
		// under a credential-shaped name — that is what it is — while a loader
		// variable, an agent handle, a set-only name or the daemon's own
		// namespace stays refused. See `refuseInjectionName`.
		if r := refuseInjectionName(inj.InjectAs, inj.Name); r != nil {
			return ChildEnv{}, r
		}
		if inj.value.Empty() {
			return ChildEnv{}, NewRefusal(RefusalSecretUnresolvable,
				"the secret reference "+inj.Name+" resolved to nothing",
				"an empty value cannot be injected and cannot be redacted",
				"run secretref.check on it, or point the reference at a variable or file that holds the value")
		}
		gathered[inj.InjectAs] = Var{
			Name:   inj.InjectAs,
			Source: SourceSecret + ":" + inj.Name,
			value:  string(inj.value.b),
		}
	}

	out := make([]Var, 0, len(gathered))
	for _, v := range gathered {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return ChildEnv{vars: out, built: true}, nil
}

// LegacyInherit is the legacy backend's value: "the child inherits the
// daemon's environment", said out loud, marked as such, and reachable only
// from the one backend that means it (EV6 keeps an unattended loop from ever
// getting here).
func LegacyInherit() ChildEnv {
	return ChildEnv{legacy: true, built: true}
}

// Legacy reports whether this is the inherit-everything value.
func (c ChildEnv) Legacy() bool { return c.legacy }

// IsZero reports the inert value — the one a caller who never resolved an
// environment holds. Spawn refuses it; a test asserts it cannot spawn.
func (c ChildEnv) IsZero() bool { return !c.built }

// Env is the ONLY place the values leave this package for a host child. It
// panics on the inert value: reaching it is a programming error, the same
// class as a nil map write, and a panic that names the bug beats a child that
// quietly inherited the daemon's keys.
func (c ChildEnv) Env() []string {
	if !c.built {
		panic("execenv: ChildEnv.Env on a value that was never built — resolve the environment or refuse the command")
	}
	if c.legacy {
		return nil // nil cmd.Env is Go's "inherit"; the legacy backend's whole meaning
	}
	out := make([]string, 0, len(c.vars))
	for _, v := range c.vars {
		out = append(out, v.Name+"="+v.value)
	}
	return out
}

// TryEnv is the production door: the same answer, with a refusal instead of a
// panic, for a caller that has to report rather than crash. Spawn takes this
// one; nothing in a test path needs it.
func (c ChildEnv) TryEnv() ([]string, *Refusal) {
	if !c.built {
		return nil, NewRefusal(RefusalEnvironmentUnavailable,
			"refusing to spawn: no execution environment was resolved",
			"the ChildEnv handed to Spawn is the zero value, which means nothing resolved it",
			"resolve the step's environment (execenv.Resolve + Build) and pass the result as Spawn's fifth argument")
	}
	return c.Env(), nil
}

// Names is the value-free view: every variable's name and where it came from.
// A test asserts its JSON carries no value bytes, because this is what
// `environment.effective` and the step record print, and a preview that
// leaked a value is the leak this slice exists to close.
func (c ChildEnv) Names() []Var {
	if c.legacy {
		return []Var{{Name: "*", Source: SourceLegacy}}
	}
	out := make([]Var, 0, len(c.vars))
	for _, v := range c.vars {
		out = append(out, Var{Name: v.Name, Source: v.Source})
	}
	return out
}

func lookupValue(de DaemonEnv, name string) (string, bool) {
	if de.Lookup == nil {
		return "", false
	}
	v := de.Lookup(name)
	if v == "" {
		return "", false
	}
	return v, true
}
