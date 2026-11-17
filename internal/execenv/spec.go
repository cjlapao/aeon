// Package execenv is the pure heart of S59: what an execution environment IS,
// what a spec may ask for, what a child process is allowed to see, and whether
// one environment is no weaker than another.
//
// It is deliberately the standard library and nothing else (a test says so, in
// the `internal/records` tradition). Three reasons, each of which fails on its
// own:
//
//   - the daemon's environment is a FUNCTION argument, never os.Getenv inside
//     here, so every decision in this package is testable with a map;
//   - importing `store` or `daemon` would put a database and a socket between a
//     security decision and the test that checks it;
//   - the vocabulary (backends, capabilities, deny lists, the honesty
//     sentences) is served to every surface by Vocabulary(), so there is one
//     list in the product. A second copy in a client is how the two drift.
//
// Nothing in here decides whether a command runs: the policy gate does that
// first and unchanged. This package decides what a command SEES once it runs,
// and refuses to build one it cannot build honestly.
package execenv

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Home modes. `real` is the daemon user's HOME; `scratch` is an empty
// directory owned by the provision. Only `container` gets scratch today
// (D5) — a host scratch directory needs the lifecycle ledger, so Validate
// refuses it there with a sentence rather than half-implementing it.
const (
	HomeReal    = "real"
	HomeScratch = "scratch"
)

// Pull policies for a container image.
const (
	PullNever   = "never"
	PullMissing = "missing"
)

// Spec is the closed shape an environment record carries (EV4). It is stored
// as canonical JSON and re-parsed on read, so a field that is not listed here
// cannot exist: ParseSpec refuses unknown keys rather than ignoring them,
// which is the difference between "we did not think of that field" and "that
// field was silently doing nothing".
//
// No field of a Spec holds a secret VALUE. `env.secrets` and every
// `services[].secrets` hold reference NAMES, and the literal map `env.set` is
// refused for any name that looks like a credential (EV4's sentence points at
// secret references for exactly that reason).
type Spec struct {
	Env      EnvSection `json:"env"`
	Home     string     `json:"home"`
	Limits   Limits     `json:"limits"`
	Egress   Egress     `json:"egress"`
	Image    *Image     `json:"image"`
	Services []Service  `json:"services"`
	Preview  *Preview   `json:"preview"`
}

// EnvSection is the environment half: what is copied by name, what is set
// literally, and which secret references are injected.
type EnvSection struct {
	// Passthrough holds variable NAMES and PREFIX_* patterns copied from the
	// daemon's environment at spawn time. Names only are ever stored.
	Passthrough []string `json:"passthrough"`
	// Set holds literal name→value pairs — for `TERM=dumb`, never for a token.
	Set map[string]string `json:"set"`
	// Secrets holds secret_ref NAMES, resolved at spawn time (EV7).
	Secrets []string `json:"secrets"`
}

// Limits are the resource ceilings. Every pointer is "absent", which is
// different from zero: an absent limit means the backend's own default, and
// `Restricts` reads the difference (adding a limit where none existed is
// TIGHTER, EV16).
type Limits struct {
	WallSeconds *int     `json:"wallSeconds"`
	MemoryMiB   *int     `json:"memoryMiB"`
	CPUs        *float64 `json:"cpus"`
}

// Egress is the network posture. `Allow` is declared so the shape is honest
// and refused when populated: a host allowlist needs an egress proxy, which
// this slice does not build (EV13's staged half).
type Egress struct {
	Mode  string   `json:"mode"`
	Allow []string `json:"allow"`
}

// Image is the container's base image. A digest-pinned ref is recommended; a
// floating tag is allowed and warned about in reads (D8).
type Image struct {
	Ref  string `json:"ref"`
	Pull string `json:"pull"`
}

// Service is a per-loop dependency container (EV14), container backend only.
type Service struct {
	Name    string            `json:"name"`
	Image   string            `json:"image"`
	Port    int               `json:"port"`
	Env     map[string]string `json:"env"`
	Secrets []string          `json:"secrets"`
	Health  *Health           `json:"health"`
}

// Health is how a service is proven up before the loop's first command.
type Health struct {
	TCP            int    `json:"tcp"`
	Command        string `json:"cmd"`
	TimeoutSeconds int    `json:"timeoutSeconds"`
}

// Preview is reserved. It exists so the shape is declared rather than
// improvised later, and Validate refuses any non-null value (EV4).
type Preview struct {
	Build string `json:"build"`
	URL   string `json:"url"`
	TTL   int    `json:"ttlSeconds"`
}

// ParseSpec decodes a spec with the closed-shape rule enforced: an unknown key
// is refused, not dropped. A typo'd field that parsed to its zero value would
// read back as "this environment asks for nothing", which is a permissive
// answer produced by a spelling mistake.
func ParseSpec(raw []byte) (Spec, error) {
	var s Spec
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return Spec{}, fmt.Errorf("the environment spec is refused: %v", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		// Trailing content after the object means two specs in one document,
		// and "the first one won" is not a decision anybody agreed to.
		return Spec{}, fmt.Errorf("the environment spec is refused: unexpected data after the spec object")
	}
	if s.Env.Set == nil {
		s.Env.Set = map[string]string{}
	}
	return s, nil
}

// Canonical renders the spec the way the audit stores it and the way
// Restricts compares it: byte-stable, so two specs that MEAN the same thing
// produce the same bytes and a diff of two revisions is a real diff.
//
// Maps marshal with sorted keys already; what is normalised here is the order
// of the slices, which a person editing JSON can change without changing any
// meaning. Service order is semantic (a dependency graph reads it), so
// services keep theirs and everything name-like is sorted.
func Canonical(s Spec) []byte {
	n := s
	n.Env.Passthrough = sortedCopy(s.Env.Passthrough)
	n.Env.Secrets = sortedCopy(s.Env.Secrets)
	if s.Env.Set == nil {
		n.Env.Set = map[string]string{}
	}
	if s.Egress.Allow != nil {
		a := sortedCopy(s.Egress.Allow)
		n.Egress.Allow = a
	}
	if s.Services != nil {
		sv := make([]Service, len(s.Services))
		copy(sv, s.Services)
		for i := range sv {
			sv[i].Secrets = sortedCopy(s.Services[i].Secrets)
			if s.Services[i].Env == nil {
				sv[i].Env = map[string]string{}
			}
		}
		n.Services = sv
	}
	raw, err := json.Marshal(n)
	if err != nil {
		// Unreachable for a Spec: every field is JSON-encodable, and no field
		// holds a channel, a func or a cyclic pointer. Reaching it means the
		// type changed under this function, which is a compile-time-reviewable
		// event, not a runtime state to recover from.
		panic("execenv: Canonical cannot encode a Spec: " + err.Error())
	}
	return raw
}

func sortedCopy(in []string) []string {
	if in == nil {
		return nil
	}
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

// NamespacedServiceHost is the variable NAME the daemon derives for a service
// (EV14) — `AEON_SERVICE_<NAME>_HOST`. It is a name built here, from the
// service's own name, so no surface lets a person type the variable an
// environment's commands will find the database under.
func NamespacedServiceHost(name string) string {
	return "AEON_SERVICE_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_")) + "_HOST"
}

// NamespacedServicePort is the port twin of NamespacedServiceHost.
func NamespacedServicePort(name string) string {
	return "AEON_SERVICE_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_")) + "_PORT"
}
