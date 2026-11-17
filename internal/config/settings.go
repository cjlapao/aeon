package config

import (
	"fmt"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// settings.go — the closed key registry (CS3, CS4, CS5, CS6, CS8, CS10). A
// setting is a declared key: code, reviewed, and refused at init (mustDeclare)
// so a bad one never reaches a running daemon. Nothing here touches storage,
// the network or the clock — this is pure Go; the tests inject the values.

// SettingType is the closed set of value types (CS3). No fifth type.
type SettingType string

const (
	TypeBool   SettingType = "bool"
	TypeInt    SettingType = "int"
	TypeString SettingType = "string"
	TypeEnum   SettingType = "enum"
)

// Scope is the single scope this slice supports (CS6): global only.
type Scope string

const ScopeGlobal Scope = "global" // the only member (CS6)

// Setting is one declared key. A declaration is code, reviewed, and refused at
// init (mustDeclare) — so a bad one never reaches a running daemon.
type Setting struct {
	Key         string // dotted camelCase, e.g. "agents.allowUnsignedImport"; grammar below
	Area        string // the grouping the UI shows; defaults to the key's first segment
	Type        SettingType
	Default     string // canonical text form; MUST parse under Type/bounds
	Description string // one or two sentences a person reads in Settings; required, non-empty
	Scope       Scope // ScopeGlobal; the zero value is treated as ScopeGlobal
	Sensitive   bool // CS8: widens a guard; requires a Description saying what the default protects
	// Type-specific bounds (a bound that does not apply to Type is a declaration error):
	Min, Max *int // TypeInt
	MaxLen   int // TypeString; 0 means the default cap 4096; may not exceed 4096
	// TypeEnum; non-empty, no duplicates, each non-empty, each a legal canonical string
	EnumValues []string
}

// Registry is the closed set of declared keys: a map by key, insertion-order
// independent and read-only after construction (NewRegistry).
type Registry struct {
	byKey map[string]Setting
	byEnv map[string]string // env name -> key, for CS4 collision detection
}

// defaultStringCap is the byte cap a TypeString key gets when MaxLen is 0.
const defaultStringCap = 4096

// keyRe is the key grammar (rule 1): a lowercase first segment, then one or
// more "."-prefixed letter-first segments. No empty segment (each starts with
// a letter), no leading dot, at least one dot. Combined with the 3–128 length
// bound it is the whole grammar.
var keyRe = regexp.MustCompile(`^[a-z][A-Za-z0-9]*(\.[A-Za-z][A-Za-z0-9]*)+$`)

// areaRe is the Area grammar (rule 10): a single lowercase-first segment.
var areaRe = regexp.MustCompile(`^[a-z][A-Za-z0-9]*$`)

// intRe is the int canonical form: an optional minus, then 0 or a
// non-zero-padded magnitude. It accepts -0, which validIntForm refuses
// separately (canonical int has no negative zero).
var intRe = regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`)

// NewRegistry is the testable constructor: it returns the refusal, never
// panics. Every declaration rule is enforced here (and mustDeclare panics on
// the same error).
func NewRegistry(decls ...Setting) (*Registry, error) {
	byKey := map[string]Setting{}
	byEnv := map[string]string{}
	for _, s := range decls {
		s.Area = resolveArea(s)
		if err := validateDecl(s); err != nil {
			return nil, err
		}
		// rule 3: duplicate key (NewRegistry just names the key; mustDeclare
		// adds the declaration site via runtime.Caller).
		if _, exists := byKey[s.Key]; exists {
			return nil, fmt.Errorf("duplicate setting key %q", s.Key)
		}
		// rule 4: env-name collision (CS4) — names both keys and the shared
		// env name.
		env, collided, prev := envCollides(s.Key, byEnv)
		if collided {
			return nil, fmt.Errorf("env name collision: %q and %q both map to %s", prev, s.Key, env)
		}
		byKey[s.Key] = s
		byEnv[env] = s.Key
	}
	return &Registry{byKey: byKey, byEnv: byEnv}, nil
}

// DefaultRegistry is the package-global set built from init() declarations.
// T2 shipped it empty (CS3: zero production settings); S44 T10 (S43 adoption)
// added the first via mustDeclare — agents.allowUnsignedImport — so it now
// contains exactly that one key (TestDefaultRegistryContainsExactlyTheAdoptedKey).
func DefaultRegistry() *Registry {
	return defaultRegistry
}

// Declare is the non-panicking half of mustDeclare: it validates one setting
// against the accumulated default declarations and, if legal, adds it,
// returning the refusal otherwise.
func Declare(s Setting) error {
	all := make([]Setting, len(defaultDecls)+1)
	copy(all, defaultDecls)
	all[len(defaultDecls)] = s
	r, err := NewRegistry(all...)
	if err != nil {
		return err
	}
	defaultDecls = append(defaultDecls, s)
	defaultRegistry = r
	return nil
}

// mustDeclare is the package-init helper: it declares s into the default
// registry and panics with the refusal (naming the declaration site via
// runtime.Caller), so a bad declaration is a programmer error caught by go
// test before any daemon starts — the same stance as Table.register panicking
// on a duplicate method.
func mustDeclare(s Setting) {
	if err := Declare(s); err != nil {
		_, file, line, ok := runtime.Caller(1)
		if ok {
			panic(fmt.Sprintf("%s (declared at %s:%d)", err, file, line))
		}
		panic(err)
	}
}

// Lookup returns a declared setting by key. An absent key is (zero, false),
// never an error — absence is a normal answer, not a fault (DB19).
func (r *Registry) Lookup(key string) (Setting, bool) {
	s, ok := r.byKey[key]
	return s, ok
}

// All returns every declared setting, sorted by Area then Key.
func (r *Registry) All() []Setting {
	out := make([]Setting, 0, len(r.byKey))
	for _, s := range r.byKey {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Area != out[j].Area {
			return out[i].Area < out[j].Area
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// Nearest returns up to n declared keys closest to key, ranked by (1) shared
// first segment, (2) case-insensitive prefix match, (3) small edit distance
// (Levenshtein), ties by All() order. It returns an empty slice when the
// registry is empty (no panic). This feeds ErrSettingUnknown's Do.
func (r *Registry) Nearest(key string, n int) []string {
	if len(r.byKey) == 0 || n <= 0 {
		return []string{}
	}
	all := r.All()
	orderIndex := make(map[string]int, len(all))
	for i, s := range all {
		orderIndex[s.Key] = i
	}
	type cand struct {
		key    string
		shared bool
		prefix bool
		dist   int
		order  int
	}
	var cands []cand
	for _, s := range all {
		cands = append(cands, cand{
			key:    s.Key,
			shared: firstSegment(s.Key) == firstSegment(key),
			prefix: caseInsensitivePrefix(s.Key, key),
			dist:   levenshtein(s.Key, key),
			order:  orderIndex[s.Key],
		})
	}
	sort.SliceStable(cands, func(i, j int) bool {
		a, b := cands[i], cands[j]
		if a.shared != b.shared {
			return a.shared // (1) shared first segment ranks first
		}
		if a.prefix != b.prefix {
			return a.prefix // (2) case-insensitive prefix ranks first
		}
		if a.dist != b.dist {
			return a.dist < b.dist // (3) smaller edit distance ranks first
		}
		return a.order < b.order // ties by All() order
	})
	out := make([]string, 0, n)
	for i := 0; i < len(cands) && i < n; i++ {
		out = append(out, cands[i].key)
	}
	return out
}

// ParseValue validates and canonicalises raw to the key's one canonical text
// form (CS3): one spelling per value, so equality is a string compare. It
// never trims or fixes — a leading/trailing space is a refusal, not a
// correction — and returns ErrSettingInvalidValue on a refusal.
func (s Setting) ParseValue(raw string) (string, error) {
	switch s.Type {
	case TypeBool:
		if raw != "true" && raw != "false" {
			return "", NewSettingInvalidValue(s, raw)
		}
		return raw, nil
	case TypeInt:
		if !validIntForm(raw) {
			return "", NewSettingInvalidValue(s, raw)
		}
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil { // an overflow (e.g. 9999999999999999999) is a refusal
			return "", NewSettingInvalidValue(s, raw)
		}
		if s.Min != nil && v < int64(*s.Min) {
			return "", NewSettingInvalidValue(s, raw)
		}
		if s.Max != nil && v > int64(*s.Max) {
			return "", NewSettingInvalidValue(s, raw)
		}
		return raw, nil
	case TypeString:
		if !utf8.ValidString(raw) {
			return "", NewSettingInvalidValue(s, raw)
		}
		if strings.IndexByte(raw, 0) >= 0 { // a NUL byte is a refusal
			return "", NewSettingInvalidValue(s, raw)
		}
		cap := s.MaxLen
		if cap == 0 {
			cap = defaultStringCap
		}
		if len(raw) > cap {
			return "", NewSettingInvalidValue(s, raw)
		}
		return raw, nil
	case TypeEnum:
		for _, m := range s.EnumValues {
			if raw == m { // case-sensitive; a near-miss (Auto for auto) is refused
				return raw, nil
			}
		}
		return "", NewSettingInvalidValue(s, raw)
	}
	return "", NewSettingInvalidValue(s, raw)
}

// FormatDefault returns the key's default in canonical text form. The default
// is stored canonical (rule 5), so this is the identity.
func (s Setting) FormatDefault() string { return s.Default }

// --- declaration rules (each is a refusal NewRegistry returns) ---

// validateDecl enforces the per-declaration rules on one setting. Bounds (6)
// and enum (7) are checked before the default (5) so an out-of-range default
// does not mask a bad bound.
func validateDecl(s Setting) error {
	// rule 1: key grammar
	if !validKey(s.Key) {
		return fmt.Errorf("setting key %q has bad grammar: %s, length 3-128", s.Key, keyRe.String())
	}
	// rule 2: credential-shaped name (CS5, C4)
	if isCredentialName(s.Key) {
		return fmt.Errorf("setting %q is a credential-shaped name (C4: a credential never lives in a config store; route it through config.Credential)", s.Key)
	}
	// rule 6: bounds apply only to their type
	if err := validateBounds(s); err != nil {
		return err
	}
	// rule 7: enum members
	if err := validateEnum(s); err != nil {
		return err
	}
	// rule 5: default must parse under the key's own type and bounds
	if _, err := s.ParseValue(s.Default); err != nil {
		return fmt.Errorf("setting %q default %q does not parse under %s: %v", s.Key, s.Default, s.Type, err)
	}
	// rule 8: description
	if err := validateDescription(s); err != nil {
		return err
	}
	// rule 9: scope
	if err := validateScope(s); err != nil {
		return err
	}
	// rule 10: area
	if err := validateArea(s); err != nil {
		return err
	}
	return nil
}

// resolveArea derives the Area from the key's first segment when empty (rule
// 10); a given area is returned as-is so validateArea can reject a bad one.
func resolveArea(s Setting) string {
	if s.Area != "" {
		return s.Area
	}
	return firstSegment(s.Key)
}

// validKey is the key grammar (rule 1): the regex plus the 3–128 length bound.
func validKey(key string) bool {
	if len(key) < 3 || len(key) > 128 {
		return false
	}
	return keyRe.MatchString(key)
}

// validIntForm is the int canonical form (CS3): the regex, minus -0 (canonical
// int has no negative zero) and the empty string.
func validIntForm(raw string) bool {
	if raw == "" || raw == "-0" {
		return false
	}
	return intRe.MatchString(raw)
}

// validateBounds enforces rule 6: bounds apply only to their type; Min <= Max
// when both set; MaxLen in [0, 4096].
func validateBounds(s Setting) error {
	if s.Min != nil || s.Max != nil {
		if s.Type != TypeInt {
			return fmt.Errorf("setting %q: Min/Max only apply to an int setting", s.Key)
		}
	}
	if s.MaxLen != 0 {
		if s.Type != TypeString {
			return fmt.Errorf("setting %q: MaxLen only applies to a string setting", s.Key)
		}
	}
	if len(s.EnumValues) > 0 {
		if s.Type != TypeEnum {
			return fmt.Errorf("setting %q: EnumValues only apply to an enum setting", s.Key)
		}
	}
	if s.Min != nil && s.Max != nil && *s.Min > *s.Max {
		return fmt.Errorf("setting %q: Min %d must be <= Max %d", s.Key, *s.Min, *s.Max)
	}
	if s.MaxLen < 0 || s.MaxLen > 4096 {
		return fmt.Errorf("setting %q: MaxLen must be in [0, 4096], got %d", s.Key, s.MaxLen)
	}
	return nil
}

// validateEnum enforces rule 7: enum members non-empty, unique, each non-empty
// and with no whitespace. (A default that is not a member is refused by rule 5.)
func validateEnum(s Setting) error {
	if s.Type != TypeEnum {
		return nil // not an enum; misplaced EnumValues are caught by validateBounds
	}
	if len(s.EnumValues) == 0 {
		return fmt.Errorf("setting %q: enum needs at least one member", s.Key)
	}
	seen := map[string]bool{}
	for _, m := range s.EnumValues {
		if m == "" {
			return fmt.Errorf("setting %q: an enum member is empty", s.Key)
		}
		if strings.ContainsAny(m, " \t\n\r") {
			return fmt.Errorf("setting %q: enum member %q has whitespace", s.Key, m)
		}
		if seen[m] {
			return fmt.Errorf("setting %q: enum member %q is duplicated", s.Key, m)
		}
		seen[m] = true
	}
	return nil
}

// validateDescription enforces rule 8: a description is required and non-empty
// after trim; a Sensitive declaration additionally needs 40+ characters (a
// one-word description cannot say what a default protects; the real review is
// human).
func validateDescription(s Setting) error {
	desc := strings.TrimSpace(s.Description)
	if desc == "" {
		return fmt.Errorf("setting %q: a description is required (non-empty after trim)", s.Key)
	}
	if s.Sensitive && utf8.RuneCountInString(desc) < 40 {
		return fmt.Errorf("setting %q: a sensitive setting needs a 40+ character description saying what the default protects (CS8)", s.Key)
	}
	return nil
}

// validateScope enforces rule 9: zero or ScopeGlobal; anything else is refused.
func validateScope(s Setting) error {
	if s.Scope != "" && s.Scope != ScopeGlobal {
		return fmt.Errorf("setting %q: scoped settings are not built — CS6", s.Key)
	}
	return nil
}

// validateArea enforces rule 10: a given area must be a lowercase-first
// segment; the derived area (first segment) is always valid.
func validateArea(s Setting) error {
	if !areaRe.MatchString(s.Area) {
		return fmt.Errorf("setting %q: Area %q must match [a-z][A-Za-z0-9]*", s.Key, s.Area)
	}
	return nil
}

// --- helpers ---

// firstSegment is the part of a key before its first "."; a key with no dot is
// the whole key.
func firstSegment(key string) string {
	if i := strings.IndexByte(key, '.'); i >= 0 {
		return key[:i]
	}
	return key
}

// caseInsensitivePrefix reports whether one key is a case-insensitive prefix
// of the other (either direction) — the Nearest (2) ranking criterion.
func caseInsensitivePrefix(a, b string) bool {
	la, lb := strings.ToLower(a), strings.ToLower(b)
	return strings.HasPrefix(la, lb) || strings.HasPrefix(lb, la)
}

// levenshtein is the edit distance between two strings (the Nearest (3)
// ranking criterion). An inline implementation — no new dependency.
func levenshtein(a, b string) int {
	ar, br := []rune(a), []rune(b)
	m, n := len(ar), len(br)
	d := make([][]int, m+1)
	for i := range d {
		d[i] = make([]int, n+1)
	}
	for i := 0; i <= m; i++ {
		d[i][0] = i
	}
	for j := 0; j <= n; j++ {
		d[0][j] = j
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			d[i][j] = min3(d[i-1][j]+1, d[i][j-1]+1, d[i-1][j-1]+cost)
		}
	}
	return d[m][n]
}

// min3 is the smaller of three.
func min3(x, y, z int) int {
	m := x
	if y < m {
		m = y
	}
	if z < m {
		m = z
	}
	return m
}

// --- the default registry (built by init() declarations) ---

var (
	defaultDecls    []Setting
	defaultRegistry *Registry
)

func init() {
	// In this slice (T2) no production setting is declared (CS3), so the
	// default registry is empty. T10's settings_decl_*.go files add entries
	// through mustDeclare/Declare; a test asserts the empty state here.
	defaultRegistry = &Registry{byKey: map[string]Setting{}, byEnv: map[string]string{}}
}
