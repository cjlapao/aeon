package seed

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// idGrammar is the set ID grammar (rule 1): a lowercase dotted name, the
// first segment starting with a letter, each segment a-z0-9 (a later
// segment may start uppercase, e.g. "agents.firstParty"), 3-64 bytes. It is
// checked once at registration so a malformed ID is a named refusal, not a
// runtime surprise.
var idGrammar = regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z][a-zA-Z0-9]*)+$`)

// Registry is a set of declared seed sets and their appliers, cross-checked
// at build time. It is pure data with no I/O and no clock: the engine reads
// it, the daemon builds it, and a later slice's init() registers into it.
type Registry struct {
	// sets is keyed by Set.ID; never holds a duplicate.
	sets map[string]Set
	// appliers is keyed by Set.ID; a set with none is refused at build.
	appliers map[string]Applier
}

// NewRegistry builds a registry from explicit sets and appliers, refusing
// (never panicking) on any broken declaration: a bad ID grammar, a wrong
// class, a non-positive or out-of-order version, a nil Records, a bad or
// duplicate key, oversized or non-JSON content, a set with no applier, or a
// set whose applier reports GuardAffecting()==true while the set is inert
// (the SD5 cross-check, rule 6). It is the constructor the engine and the
// tests use; the package-global default is DefaultRegistry.
func NewRegistry(sets []Set, appliers []Applier) (*Registry, error) {
	reg := &Registry{
		sets:     make(map[string]Set),
		appliers: make(map[string]Applier),
	}
	for _, a := range appliers {
		if a == nil {
			return nil, fmt.Errorf("seed: a nil applier was given to the registry")
		}
		id := a.Set()
		if id == "" {
			return nil, fmt.Errorf("seed: an applier with an empty set id was given to the registry")
		}
		if _, dup := reg.appliers[id]; dup {
			return nil, fmt.Errorf("seed: applier %q is registered twice", id)
		}
		reg.appliers[id] = a
	}
	for _, s := range sets {
		if err := validateSet(s); err != nil {
			return nil, err
		}
		if _, dup := reg.sets[s.ID]; dup {
			return nil, fmt.Errorf("seed: set %q is registered twice", s.ID)
		}
		reg.sets[s.ID] = s
	}
	// The applier cross-check (rules 6, 7): every set has exactly one
	// applier, and no inert set carries a guard-affecting applier.
	for id, s := range reg.sets {
		a, ok := reg.appliers[id]
		if !ok {
			return nil, fmt.Errorf("seed: set %q has no applier", id)
		}
		if s.Class == ClassInert && a.GuardAffecting() {
			return nil, fmt.Errorf("seed: set %q is inert but its applier is guard-affecting — a guard-affecting set cannot be applied unasked (SD5)", id)
		}
	}
	return reg, nil
}

// Register adds a set and its applier to the package-global registry,
// validating the pair (a broken declaration is a refusal, never a silent
// gap). A domain calls it from its init() (SD2); the panic path is
// mustRegister. It returns the error so a caller may choose to refuse
// rather than panic. A set registered without its applier is refused: a
// set the engine cannot apply is worse than none.
func Register(s Set, a Applier) error {
	if a == nil {
		return fmt.Errorf("seed: set %q was registered with a nil applier", s.ID)
	}
	if a.Set() != s.ID {
		return fmt.Errorf("seed: applier %q does not match set %q", a.Set(), s.ID)
	}
	if err := validateSet(s); err != nil {
		return err
	}
	if globalSets == nil {
		globalSets = map[string]Set{}
	}
	if globalAppliers == nil {
		globalAppliers = map[string]Applier{}
	}
	if _, dup := globalSets[s.ID]; dup {
		return fmt.Errorf("seed: set %q is already registered", s.ID)
	}
	globalSets[s.ID] = s
	globalAppliers[s.ID] = a
	return nil
}

// RegisterApplier adds a set's applier to the package-global registry,
// refusing a duplicate. It is split from Register for the rare domain that
// registers its applier before its set; the set's Register then finds it.
func RegisterApplier(a Applier) error {
	if a == nil || a.Set() == "" {
		return fmt.Errorf("seed: an applier with an empty set id was given to RegisterApplier")
	}
	if globalAppliers == nil {
		globalAppliers = map[string]Applier{}
	}
	if _, dup := globalAppliers[a.Set()]; dup {
		return fmt.Errorf("seed: applier %q is already registered", a.Set())
	}
	globalAppliers[a.Set()] = a
	return nil
}

// mustRegister panics on a broken declaration — the init()-time stance
// (SD2, the bodyEntries panic-at-init, moved to registration). A domain
// that declares a broken set is a build that panics, not a daemon that
// silently misses it. It is the loud twin of Register.
func mustRegister(s Set, a Applier) {
	if err := Register(s, a); err != nil {
		panic(fmt.Sprintf("seed: refusing to register a set: %v", err))
	}
}

// DefaultRegistry returns the package-global registry built from every set
// and applier a domain has registered via Register/RegisterApplier. At
// T1 no domain is registered, so it is empty; T4's agentseed init() flips it
// to contain exactly agents.firstparty. A broken registration panics at
// init (mustRegister), so DefaultRegistry itself never returns a broken set.
func DefaultRegistry() *Registry {
	reg := &Registry{
		sets:     map[string]Set{},
		appliers: map[string]Applier{},
	}
	reg.sets = globalSets
	reg.appliers = globalAppliers
	return reg
}

// globalSets and globalAppliers are the package-global registry a domain's
// init() registers into. They are nil until something registers; a domain
// that registers a broken set panics (mustRegister), so a built binary's
// default registry is always consistent.
var (
	globalSets     map[string]Set
	globalAppliers map[string]Applier
)

// Lookup returns the set by ID (false if none). It is the by-id reader the
// daemon's seed.* methods use.
func (r *Registry) Lookup(id string) (Set, bool) {
	s, ok := r.sets[id]
	return s, ok
}

// Applier returns the applier for a set ID (false if none).
func (r *Registry) Applier(id string) (Applier, bool) {
	a, ok := r.appliers[id]
	return a, ok
}

// All returns every set, sorted by ID. The order is deterministic so status
// and plan are reproducible (SD6).
func (r *Registry) All() []Set {
	out := make([]Set, 0, len(r.sets))
	for _, s := range r.sets {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Len is the number of declared sets.
func (r *Registry) Len() int { return len(r.sets) }

// validateSet checks a set's declaration (rules 1-5) — the checks that need
// only the set, not its applier. A broken set is a named refusal.
func validateSet(s Set) error {
	if !idGrammar.MatchString(s.ID) {
		return fmt.Errorf("seed: set id %q is not valid — it must match %s", s.ID, idGrammar.String())
	}
	if len(s.ID) < 3 || len(s.ID) > 64 {
		return fmt.Errorf("seed: set id %q is not 3-64 bytes", s.ID)
	}
	if s.Class != ClassInert && s.Class != ClassGuard && s.Class != ClassDefault {
		return fmt.Errorf("seed: set %q has an unknown class %q (want %q, %q or %q)", s.ID, s.Class, ClassInert, ClassGuard, ClassDefault)
	}
	if s.Version < 1 {
		return fmt.Errorf("seed: set %q has a non-positive version %d", s.ID, s.Version)
	}
	if s.Records == nil {
		return fmt.Errorf("seed: set %q has a nil Records function", s.ID)
	}
	records := s.Records()
	seen := make(map[string]bool)
	for _, r := range records {
		if strings.TrimSpace(r.Key) == "" {
			return fmt.Errorf("seed: set %q has a record with an empty key", s.ID)
		}
		if len(r.Key) > 128 {
			return fmt.Errorf("seed: set %q has a record key %q longer than 128 bytes", s.ID, r.Key)
		}
		if !utf8.ValidString(r.Key) {
			return fmt.Errorf("seed: set %q has a record key %q with invalid UTF-8", s.ID, r.Key)
		}
		for i := 0; i < len(r.Key); i++ {
			if r.Key[i] < 0x20 || r.Key[i] == 0x7f {
				return fmt.Errorf("seed: set %q has a record key %q with a control character", s.ID, r.Key)
			}
		}
		if seen[r.Key] {
			return fmt.Errorf("seed: set %q has a duplicate record key %q", s.ID, r.Key)
		}
		seen[r.Key] = true
		if r.Version < 1 || r.Version > s.Version {
			return fmt.Errorf("seed: set %q record %q has a version %d out of range [1, %d]", s.ID, r.Key, r.Version, s.Version)
		}
		if len(r.Content) > 1<<20 {
			return fmt.Errorf("seed: set %q record %q has content larger than 1 MiB", s.ID, r.Key)
		}
		if !validJSON(r.Content) {
			return fmt.Errorf("seed: set %q record %q has content that is not valid JSON", s.ID, r.Key)
		}
	}
	return nil
}
