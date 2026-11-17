package config

import (
	"fmt"
	"strings"
	"testing"
)

// S44 T2 — the closed key registry and typed validation. These are the gates
// a careless change breaks silently: a declaration rule not enforced, a second
// spelling admitted, a credential-shaped key declared, a collision not caught.

// TestKeyGrammarTable is rule 1's machine pin: a table of >= 14 keys, each
// judged by the key grammar, failing when the grammar is looser or tighter
// than specified.
func TestKeyGrammarTable(t *testing.T) {
	cases := []struct {
		key   string
		valid bool
	}{
		// valid
		{"agents.allowUnsignedImport", true},
		{"a.b", true},
		{"policy.defaults.mode", true},
		{"a.b.c", true},
		{"ab.cd", true},
		// a 128-char key (the upper bound, inclusive)
		{"a." + strings.Repeat("a", 126), true},
		// invalid
		{"", false},
		{"x", false},
		{"x.", false},
		{".x", false},
		{"a..b", false},
		{"A.b", false},
		{"a.1b", false},
		{"a b.c", false},
		{"a.b-c", false},
		{"ab", false}, // too short (no dot, 2 chars)
		{"a.b.", false},
		{"a." + strings.Repeat("a", 127), false}, // 129 chars, over the bound
	}
	if len(cases) < 14 {
		t.Fatalf("the table needs >= 14 keys, got %d", len(cases))
	}
	for _, c := range cases {
		if got := validKey(c.key); got != c.valid {
			t.Errorf("validKey(%q) = %v, want %v", c.key, got, c.valid)
		}
	}
}

// TestGrammarIsNoLooserThanTheSQLCheckNet pins the two layers against each
// other: every key the Go grammar accepts also satisfies T1's CHECK net. The
// Go grammar is stricter (lowercase first char, letter-first segments), so it
// must be a subset of the net; a key it accepts that the net rejects would be
// a looser grammar, a divergence the two layers are not allowed to have.
func TestGrammarIsNoLooserThanTheSQLCheckNet(t *testing.T) {
	for _, key := range grammarCandidates {
		if !validKey(key) {
			continue // only the Go-accepted keys must pass the net
		}
		if !sqlCheckNet(key) {
			t.Errorf("Go grammar accepts %q but T1's CHECK net does not — the Go grammar is looser", key)
		}
	}
}

// sqlCheckNet re-implements T1's CHECK expression as a Go predicate. It mirrors
// the GLOB/LIKE terms of the `setting` table's key CHECK in T1's migration
// (docs/aeon/S44-config-store/tasks/T1.md):
//
//	CHECK (length(key) BETWEEN 3 AND 128
//	     AND key NOT GLOB '*[^A-Za-z0-9.]*'
//	     AND key LIKE '%.%' AND key NOT LIKE '.%' AND key NOT LIKE '%.'
//	     AND key NOT LIKE '%..%')
//
// It is a NET, not the validator: it does not require a lowercase first char
// or letter-first segments (the Go grammar owns those), so the Go grammar is a
// subset of it.
func sqlCheckNet(key string) bool {
	if len(key) < 3 || len(key) > 128 {
		return false
	}
	for i := 0; i < len(key); i++ { // NOT GLOB '*[^A-Za-z0-9.]*'
		c := key[i]
		if !(c == '.' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')) {
			return false
		}
	}
	if !strings.Contains(key, ".") {
		return false // LIKE '%.%'
	}
	if strings.HasPrefix(key, ".") {
		return false // NOT LIKE '.%'
	}
	if strings.HasSuffix(key, ".") {
		return false // NOT LIKE '%.'
	}
	if strings.Contains(key, "..") {
		return false // NOT LIKE '%..%'
	}
	return true
}

// grammarCandidates is the set of keys the grammar test iterates over: the
// table entries plus a spread of valid and invalid keys so both the Go grammar
// and the SQL net are exercised.
var grammarCandidates = []string{
	"agents.allowUnsignedImport", "a.b", "policy.defaults.mode", "a.b.c", "ab.cd",
	"x.y", "z.y", "a.bc", "a.bC", "agent.allow.unsigned.import", "policy.defaults.mode.value",
	"a." + strings.Repeat("a", 126), // 128-char valid key
	// the invalid table entries (the test skips these — validKey is false):
	"", "x", "x.", ".x", "a..b", "A.b", "a.1b", "a b.c", "a.b-c", "ab", "a.b.",
	"a." + strings.Repeat("a", 127), // 129-char key
}

// TestMustDeclareRefusesCredentialShapedKey is rule 2 / CS5's machine pin:
// each credential-named key the REAL predicate (creds.go's isCredentialName)
// matches must panic, naming the key and C4. The names are picked from the
// predicate's word list (key, token, secret, apikey, api_key, password,
// credential), not from memory.
func TestMustDeclareRefusesCredentialShapedKey(t *testing.T) {
	// These names all make isCredentialName true (verified against creds.go):
	// "apikey", "token" and "password" are in the predicate's word list.
	for _, key := range []string{"providers.apiKey", "x.token", "x.password"} {
		t.Run(key, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatalf("mustDeclare did not panic for %q", key)
				}
				msg := fmt.Sprint(r)
				if !strings.Contains(msg, key) {
					t.Fatalf("panic text must name the key %q: %q", key, msg)
				}
				if !strings.Contains(msg, "C4") {
					t.Fatalf("panic text must say C4: %q", msg)
				}
			}()
			mustDeclare(Setting{Key: key, Type: TypeString, Default: "x", Description: "a description"})
		})
	}
}

// TestNewRegistryRefusesDuplicateKey is rule 3's machine pin: two
// declarations of one key are refused, naming the key (NewRegistry just names
// the key; mustDeclare adds the site).
func TestNewRegistryRefusesDuplicateKey(t *testing.T) {
	_, err := NewRegistry(
		Setting{Key: "test.flag", Type: TypeBool, Default: "false", Description: "d"},
		Setting{Key: "test.flag", Type: TypeBool, Default: "false", Description: "d"},
	)
	if err == nil {
		t.Fatal("a duplicate key was accepted")
	}
	if !strings.Contains(err.Error(), "test.flag") {
		t.Fatalf("the error must name the key: %q", err.Error())
	}
}

// TestNewRegistryRefusesEnvNameCollision is rule 4 / CS4's machine pin: two
// keys whose env names collide are refused, naming both and the shared env
// name. a.bC and a.bc both map to AEON_SETTING_A_BC.
func TestNewRegistryRefusesEnvNameCollision(t *testing.T) {
	_, err := NewRegistry(
		Setting{Key: "a.bC", Type: TypeBool, Default: "false", Description: "d"},
		Setting{Key: "a.bc", Type: TypeBool, Default: "false", Description: "d"},
	)
	if err == nil {
		t.Fatal("an env name collision was accepted")
	}
	msg := err.Error()
	if !strings.Contains(msg, "a.bC") || !strings.Contains(msg, "a.bc") {
		t.Fatalf("the error must name both keys: %q", msg)
	}
	if !strings.Contains(msg, "AEON_SETTING_A_BC") {
		t.Fatalf("the error must name the shared env name: %q", msg)
	}
}

// TestNewRegistryRefusesBadDefaults is rule 5's machine pin: a default that
// fails its own type or bounds is refused (a self-invalid default would make
// every read storedInvalid).
func TestNewRegistryRefusesBadDefaults(t *testing.T) {
	cases := []struct {
		name string
		s    Setting
	}{
		{"bool default yes", Setting{Key: "test.x", Type: TypeBool, Default: "yes", Description: "d"}},
		{"int default 0 with Min 1", Setting{Key: "test.y", Type: TypeInt, Min: intPtr(1), Max: intPtr(10), Default: "0", Description: "d"}},
		{"enum default non-member", Setting{Key: "test.z", Type: TypeEnum, EnumValues: []string{"auto", "manual"}, Default: "nope", Description: "d"}},
		{"string default over MaxLen", Setting{Key: "test.w", Type: TypeString, MaxLen: 4, Default: "toolong", Description: "d"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := NewRegistry(c.s); err == nil {
				t.Fatalf("a bad default was accepted: %s", c.name)
			}
		})
	}
}

// TestNewRegistryRefusesMisplacedBounds is rule 6's machine pin: a bound that
// does not apply to a type is refused, Min > Max is refused, MaxLen over 4096
// is refused.
func TestNewRegistryRefusesMisplacedBounds(t *testing.T) {
	cases := []struct {
		name string
		s    Setting
	}{
		{"Min on a string", Setting{Key: "test.x", Type: TypeString, Min: intPtr(1), Default: "x", Description: "d"}},
		{"MaxLen on a bool", Setting{Key: "test.y", Type: TypeBool, MaxLen: 10, Default: "false", Description: "d"}},
		{"EnumValues on an int", Setting{Key: "test.z", Type: TypeInt, EnumValues: []string{"a"}, Default: "1", Description: "d"}},
		{"Min > Max", Setting{Key: "test.w", Type: TypeInt, Min: intPtr(10), Max: intPtr(5), Default: "7", Description: "d"}},
		{"MaxLen 5000", Setting{Key: "test.v", Type: TypeString, MaxLen: 5000, Default: "x", Description: "d"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := NewRegistry(c.s); err == nil {
				t.Fatalf("bad bounds were accepted: %s", c.name)
			}
		})
	}
}

// TestNewRegistryRefusesBadEnum is rule 7's machine pin: an empty member list,
// a duplicate member, a member with a space and an empty member are each
// refused — the enum vocabulary must be closed cleanly.
func TestNewRegistryRefusesBadEnum(t *testing.T) {
	cases := []struct {
		name string
		s    Setting
	}{
		{"empty members", Setting{Key: "test.x", Type: TypeEnum, EnumValues: []string{}, Default: "x", Description: "d"}},
		{"duplicate member", Setting{Key: "test.y", Type: TypeEnum, EnumValues: []string{"a", "a"}, Default: "a", Description: "d"}},
		{"member with a space", Setting{Key: "test.z", Type: TypeEnum, EnumValues: []string{"a b"}, Default: "a b", Description: "d"}},
		{"empty member", Setting{Key: "test.w", Type: TypeEnum, EnumValues: []string{""}, Default: "", Description: "d"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := NewRegistry(c.s); err == nil {
				t.Fatalf("a bad enum was accepted: %s", c.name)
			}
		})
	}
}

// TestSensitiveNeedsARealDescription is rule 8 / CS8's machine pin: a
// sensitive declaration with a one-word (10-char) description is refused; one
// with a 50+ character description is accepted.
func TestSensitiveNeedsARealDescription(t *testing.T) {
	if _, err := NewRegistry(Setting{Key: "test.g", Type: TypeBool, Default: "false", Sensitive: true, Description: "short desc"}); err == nil {
		t.Fatal("a sensitive key with a 10-char description was accepted")
	}
	if _, err := NewRegistry(Setting{Key: "test.g", Type: TypeBool, Default: "false", Sensitive: true, Description: strings.Repeat("x", 50)}); err != nil {
		t.Fatalf("a sensitive key with a 50+ char description was refused: %v", err)
	}
}

// TestNonGlobalScopeRefused is rule 9 / CS6's machine pin: any scope other
// than the zero value or ScopeGlobal is refused with the CS6 sentence.
func TestNonGlobalScopeRefused(t *testing.T) {
	_, err := NewRegistry(Setting{Key: "test.x", Type: TypeBool, Default: "false", Scope: "workspace", Description: "d"})
	if err == nil {
		t.Fatal("a non-global scope was accepted")
	}
	if !strings.Contains(err.Error(), "CS6") {
		t.Fatalf("the error must carry the CS6 sentence: %q", err.Error())
	}
}

// TestParseValueBool is the bool canonical form: exactly "true" or "false",
// round-tripping to itself; every other spelling is refused with
// ErrSettingInvalidValue.
func TestParseValueBool(t *testing.T) {
	s := Setting{Key: "test.flag", Type: TypeBool}
	for _, v := range []string{"true", "false"} {
		if got, err := s.ParseValue(v); err != nil || got != v {
			t.Fatalf("accepted %q: got=%q err=%v", v, got, err)
		}
	}
	for _, v := range []string{"True", "TRUE", "1", "yes", "on", "", " true"} {
		if _, err := s.ParseValue(v); err == nil {
			t.Fatalf("refused %q was accepted", v)
		} else if _, ok := err.(*ErrSettingInvalidValue); !ok {
			t.Fatalf("refused %q returned %T, want *ErrSettingInvalidValue", v, err)
		}
	}
}

// TestParseValueInt is the int canonical form: -?(0|[1-9][0-9]*) within
// Min/Max and int range, round-tripping to itself; +1, 007, 1.0, 1e3, "", -0
// and an overflow are refused.
func TestParseValueInt(t *testing.T) {
	s := Setting{Key: "test.count", Type: TypeInt}
	for _, v := range []string{"0", "1", "-1", "100", "-100"} {
		if got, err := s.ParseValue(v); err != nil || got != v {
			t.Fatalf("accepted %q: got=%q err=%v", v, got, err)
		}
	}
	for _, v := range []string{"+1", "007", "1.0", "1e3", "", "-0", "9999999999999999999"} {
		if _, err := s.ParseValue(v); err == nil {
			t.Fatalf("refused %q was accepted", v)
		} else if _, ok := err.(*ErrSettingInvalidValue); !ok {
			t.Fatalf("refused %q returned %T, want *ErrSettingInvalidValue", v, err)
		}
	}
}

// TestParseValueString is the string canonical form: any valid UTF-8 up to
// MaxLen bytes with no NUL, round-tripping byte for byte; empty is legal.
func TestParseValueString(t *testing.T) {
	s := Setting{Key: "test.label", Type: TypeString, MaxLen: 16}
	for _, v := range []string{"x", "hello", ""} {
		if got, err := s.ParseValue(v); err != nil || got != v {
			t.Fatalf("accepted %q: got=%q err=%v", v, got, err)
		}
	}
	// refused: invalid UTF-8, a NUL, over length
	for _, v := range []string{string([]byte{0xff}), "\x00", strings.Repeat("a", 17)} {
		if _, err := s.ParseValue(v); err == nil {
			t.Fatalf("refused %q was accepted", v)
		} else if _, ok := err.(*ErrSettingInvalidValue); !ok {
			t.Fatalf("refused %q returned %T, want *ErrSettingInvalidValue", v, err)
		}
	}
}

// TestParseValueEnum is the enum canonical form: exactly one member,
// case-sensitive, round-tripping to itself; a near-miss (Auto for auto) is
// refused, the error naming the members.
func TestParseValueEnum(t *testing.T) {
	s := Setting{Key: "test.mode", Type: TypeEnum, EnumValues: []string{"auto", "manual"}}
	for _, v := range []string{"auto", "manual"} {
		if got, err := s.ParseValue(v); err != nil || got != v {
			t.Fatalf("accepted %q: got=%q err=%v", v, got, err)
		}
	}
	for _, v := range []string{"Auto", "manualx", ""} {
		_, err := s.ParseValue(v)
		if err == nil {
			t.Fatalf("refused %q was accepted", v)
		}
		e, ok := err.(*ErrSettingInvalidValue)
		if !ok {
			t.Fatalf("refused %q returned %T, want *ErrSettingInvalidValue", v, err)
		}
		// the error names the members
		if !strings.Contains(e.Expected, "auto") || !strings.Contains(e.Expected, "manual") {
			t.Fatalf("the error must name the members: Expected=%q", e.Expected)
		}
	}
}

// TestParseValueDoesNotTrimOrFix is CS3's machine pin: " true", "true " and
// "True" are all refused (never normalised), and the error quotes the
// offending value.
func TestParseValueDoesNotTrimOrFix(t *testing.T) {
	s := Setting{Key: "test.flag", Type: TypeBool}
	for _, v := range []string{" true", "true ", "True"} {
		_, err := s.ParseValue(v)
		if err == nil {
			t.Fatalf("refused %q was accepted (silent normalisation)", v)
		}
		if !strings.Contains(err.Error(), v) {
			t.Fatalf("the error must quote the offending value %q: %q", v, err.Error())
		}
	}
}

// TestIntBoundsAreInclusive is rule 5's off-by-one pin: Min and Max themselves
// are accepted; Min-1 and Max+1 are refused.
func TestIntBoundsAreInclusive(t *testing.T) {
	s := Setting{Key: "test.count", Type: TypeInt, Min: intPtr(1), Max: intPtr(10)}
	for _, v := range []string{"1", "10"} {
		if _, err := s.ParseValue(v); err != nil {
			t.Fatalf("%s must be accepted (inclusive bound): %v", v, err)
		}
	}
	for _, v := range []string{"0", "11"} {
		if _, err := s.ParseValue(v); err == nil {
			t.Fatalf("%s must be refused (outside the inclusive bound)", v)
		}
	}
}

// TestEnvNameDerivation is CS4's machine pin: the worked example, a key with
// digits and a three-segment key.
func TestEnvNameDerivation(t *testing.T) {
	cases := []struct{ key, env string }{
		{"agents.allowUnsignedImport", "AEON_SETTING_AGENTS_ALLOWUNSIGNEDIMPORT"},
		{"test.c3", "AEON_SETTING_TEST_C3"}, // a key with digits
		{"a.b.c", "AEON_SETTING_A_B_C"},     // a three-segment key
	}
	for _, c := range cases {
		if got := EnvName(c.key); got != c.env {
			t.Errorf("EnvName(%q) = %q, want %q", c.key, got, c.env)
		}
	}
}

// TestNearestRanksSameAreaFirst is the Nearest ranking pin: for
// agents.allowUnsigned with agents.allowUnsignedImport and policy.allowUnsigned
// declared, the key that shares the first segment ranks first.
func TestNearestRanksSameAreaFirst(t *testing.T) {
	r, err := NewRegistry(
		Setting{Key: "agents.allowUnsignedImport", Type: TypeBool, Default: "false", Description: "d"},
		Setting{Key: "policy.allowUnsigned", Type: TypeBool, Default: "false", Description: "d"},
	)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	got := r.Nearest("agents.allowUnsigned", 5)
	if len(got) == 0 {
		t.Fatal("Nearest returned nothing")
	}
	if got[0] != "agents.allowUnsignedImport" {
		t.Fatalf("Nearest[0] = %q, want agents.allowUnsignedImport (same area first)", got[0])
	}
}

// TestNearestOnEmptyRegistry pins the empty state: an empty registry returns
// an empty slice, no panic.
func TestNearestOnEmptyRegistry(t *testing.T) {
	r, err := NewRegistry()
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if got := r.Nearest("agents.x", 5); len(got) != 0 {
		t.Fatalf("Nearest on an empty registry = %v, want empty", got)
	}
}

// TestErrSettingUnknownThreePartsAndEmptyRegistrySentence is the three-part
// pin: what/why/do are all non-empty, and an empty registry reads as "no
// settings are declared yet" (the honest state, not a confident typo).
func TestErrSettingUnknownThreePartsAndEmptyRegistrySentence(t *testing.T) {
	// empty registry
	r, err := NewRegistry()
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	e := NewSettingUnknown("agents.x", r.Nearest("agents.x", 5))
	if e.What() == "" || e.Why() == "" || e.Do() == "" {
		t.Fatalf("a part is empty: what=%q why=%q do=%q", e.What(), e.Why(), e.Do())
	}
	if !strings.Contains(e.Do(), "no settings are declared yet") {
		t.Fatalf("empty registry do must say \"no settings are declared yet\": %q", e.Do())
	}
	// non-empty registry names the nearest key in the do
	r2, err := NewRegistry(Setting{Key: "agents.allowUnsignedImport", Type: TypeBool, Default: "false", Description: "d"})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	e2 := NewSettingUnknown("agents.x", r2.Nearest("agents.x", 5))
	if e2.What() == "" || e2.Why() == "" || e2.Do() == "" {
		t.Fatalf("a part is empty: what=%q why=%q do=%q", e2.What(), e2.Why(), e2.Do())
	}
	if !strings.Contains(e2.Do(), "agents.allowUnsignedImport") {
		t.Fatalf("the do must name the nearest key: %q", e2.Do())
	}
}

// TestDefaultRegistryContainsExactlyTheAdoptedKey is S44 T10's flip of the
// empty-registry pin: T10 adopts the first production setting
// (agents.allowUnsignedImport, from S43), so the default registry now contains
// exactly that one key. CS3's "zero production settings" is now "exactly the
// adopted ones", and T2's rule that a sensitive declaration carries a
// ≥ 40-character description saying what the default protects is asserted here.
// S57 T7 (2026-11-02): the two gates settings (gates.watchWindow,
// gates.watchInterval) join the registry, so the exact key is now three;
// each new declaration carries a ≥ 40-character description (T2's rule).
// S62 T10 (2026-11-03): the ten backup.* keys join the registry, so the exact
// set is now thirteen; each carries a non-empty description, none is
// sensitive (CS5: none is credential-shaped), and the four retention counts
// carry Min 1 (a keep of zero deletes everything it keeps).
// S59 T6 (2026-11-03): execenv.provisionIdleTTL joins the registry, so the
// exact set is now fourteen. It is a duration's text form (S57's shape, not an
// int count), not sensitive (nothing credential-shaped about a sweep interval),
// and its default is the slice's D7 ruling: 24h.
// S50 T3 (2026-11-04): roadmap.ideationBlueprint and roadmap.planBlueprint
// join (RM3), so the exact set is now sixteen. Both string, MaxLen 64, not
// sensitive — blueprint names are not credentials — and their defaults are
// exactly the two constants the dispatch used to carry (brainstorm,
// roadmap-plan), so defaults reproduce today's behaviour (P2). This is an
// S44-owned data edit named in S50's T3 Notes.
// S51 T3 (2026-11-04): triage.validationBlueprint joins (TM4), so the exact
// set is now seventeen — string, MaxLen 64, not sensitive, and its default
// is exactly the constant the validate dispatch used to carry (brainstorm),
// so defaults reproduce today's behaviour (P2). This is an S44-owned data
// edit named in S51's T3 Notes.
// S61 T7 (2026-11-05): intake.allowRemoteCreate and intake.workspaceRoot
// join (GI12, GI9), so the exact set is now nineteen. The bool is the
// outward act's default-deny key (off until the person turns it on), the
// string is the one directory the target guard reads, empty by default —
// a missing workspace root is a refusal with its sentence at the door
// that needs it, never a fallback that invents a directory. Neither is
// sensitive; both descriptions are ≥ 40 characters (T2's rule). This is
// an S44-owned data edit named in S61's T7 Notes.
// S27 T8 (2026-11-05): the scheduler's four tuning knobs join (CN9, CN11,
// CN12, CN18, CN21) — scheduler.maxAgentSlots (1..64, default 2),
// scheduler.tickSeconds (5..300, default 15), scheduler.maxStartsPerRootPerHour
// (1..60, default 6), scheduler.agingHours (1..720, default 24). All four are
// TypeInt counts with declared Min/Max, none Sensitive (a cadence or a slot
// count is not a credential), and NONE of them is an authority: the master,
// the pause, and every repository's enabled row stay store rows moved only by
// human doors (S27 CN2/CN21 — TestNoAuthorityKeyInTheRegistry in
// settings_decl_scheduler_test.go holds that wall). So the exact set is now
// twenty-three.
func TestDefaultRegistryContainsExactlyTheAdoptedKey(t *testing.T) {
	all := DefaultRegistry().All()
	// S75 (2026-11-07): twenty-five — todo.nudgeAfter (the loop todo list's one
	// count) and step.maxIterations (the step's model-call bound, formerly a constant).
	// policy (2026-11-15): twenty-seven — permissions.unmatched, what a tool call or shell command no rule
	// covers becomes (ask by default; deny restores the strict mode). Not an authority: rules always win.
	if len(all) != 27 {
		t.Fatalf("DefaultRegistry().All() = %d keys, want exactly 27 (the adopted key + todo.nudgeAfter + step.maxIterations + step.deadline + the two gates settings + the ten backup keys + execenv.provisionIdleTTL + the two roadmap blueprint choices + triage.validationBlueprint  + the two intake keys + the scheduler's four tuning knobs + permissions.unmatched); got %v", len(all), all)
	}
	for _, tc := range []struct{ key, def, area string }{
		{"intake.allowRemoteCreate", "false", "intake"},
		{"intake.workspaceRoot", "", "intake"},
	} {
		st, ok := DefaultRegistry().Lookup(tc.key)
		if !ok {
			t.Fatalf("DefaultRegistry is missing %s — the intake plane reads its gate and its target root from this registry or from nothing (S61 GI12, GI9)", tc.key)
		}
		if st.Area != tc.area || st.Default != tc.def || st.Sensitive {
			t.Errorf("%s = {Default:%q Area:%q Sensitive:%v}, want %q/%s/false (S61)", tc.key, st.Default, st.Area, st.Sensitive, tc.def, tc.area)
		}
		if len(st.Description) < 40 {
			t.Errorf("%s Description is %d chars, want ≥ 40 (T2's rule)", tc.key, len(st.Description))
		}
	}
	for _, tc := range []struct{ key, def, area string }{
		{"roadmap.ideationBlueprint", "brainstorm", "roadmap"},
		{"roadmap.planBlueprint", "roadmap-plan", "roadmap"},
		{"triage.validationBlueprint", "brainstorm", "triage"},
	} {
		st, ok := DefaultRegistry().Lookup(tc.key)
		if !ok {
			t.Fatalf("DefaultRegistry is missing %s — the dispatch reads its blueprint name from this registry or from nothing (S50 RM3, S51 TM4)", tc.key)
		}
		if st.Type != TypeString || st.Default != tc.def || st.Area != tc.area || st.MaxLen != 64 || st.Sensitive {
			t.Errorf("%s = {Type:%s Default:%q Area:%q MaxLen:%d Sensitive:%v}, want string/%q/%s/64/false (S50 RM3, S51 TM4)", tc.key, st.Type, st.Default, st.Area, st.MaxLen, st.Sensitive, tc.def, tc.area)
		}
		if len(st.Description) < 40 {
			t.Errorf("%s Description is %d chars, want ≥ 40 (T2's rule)", tc.key, len(st.Description))
		}
	}
	for _, tc := range []struct{ key, def string }{
		{"scheduler.maxAgentSlots", "2"},
		{"scheduler.tickSeconds", "15"},
		{"scheduler.maxStartsPerRootPerHour", "6"},
		{"scheduler.agingHours", "24"},
	} {
		st, ok := DefaultRegistry().Lookup(tc.key)
		if !ok {
			t.Fatalf("DefaultRegistry is missing %s — the scheduler reads its tuning from this registry or from nothing (S27 CN21)", tc.key)
		}
		if st.Type != TypeInt || st.Default != tc.def || st.Area != "scheduler" || st.Sensitive {
			t.Errorf("%s = {Type:%s Default:%q Area:%q Sensitive:%v}, want int/%q/scheduler/false (S27 T8)", tc.key, st.Type, st.Default, st.Area, st.Sensitive, tc.def)
		}
		if st.Min == nil || st.Max == nil {
			t.Errorf("%s has no declared bounds — an unbounded tuning knob is a footgun with a settings.write door on it (S27 CN21)", tc.key)
		}
		if len(st.Description) < 40 {
			t.Errorf("%s Description is %d chars, want ≥ 40 (T2's rule)", tc.key, len(st.Description))
		}
	}

	ttl, ok := DefaultRegistry().Lookup("execenv.provisionIdleTTL")
	if !ok {
		t.Fatal("DefaultRegistry is missing execenv.provisionIdleTTL — the sweep interval the panel shows and the reconcile reads is the same declared value or it is a guess (S44)")
	}
	if ttl.Type != TypeString || ttl.Default != "24h" || ttl.Area != "execenv" {
		t.Errorf("execenv.provisionIdleTTL = {Type:%s Default:%q Area:%q}, want string/24h/execenv (D7)", ttl.Type, ttl.Default, ttl.Area)
	}
	if ttl.Sensitive {
		t.Errorf("execenv.provisionIdleTTL is marked Sensitive — a sweep interval is not a credential, and a false Sensitive makes the panel ask for a confirmation that means nothing (CS8)")
	}
	if len(ttl.Description) < 40 {
		t.Errorf("execenv.provisionIdleTTL Description is %d chars, want ≥ 40 (T2's rule)", len(ttl.Description))
	}
	for _, key := range []string{"gates.watchWindow", "gates.watchInterval"} {
		g, ok := DefaultRegistry().Lookup(key)
		if !ok {
			t.Fatalf("DefaultRegistry is missing %s", key)
		}
		if g.Type != TypeString {
			t.Errorf("%s Type = %s, want string (a duration's canonical text form)", key, g.Type)
		}
		if g.Area != "gates" {
			t.Errorf("%s Area = %q, want %q", key, g.Area, "gates")
		}
		if len(g.Description) < 40 {
			t.Errorf("%s Description is %d chars, want ≥ 40 (T2's rule)", key, len(g.Description))
		}
	}
	if got, _ := DefaultRegistry().Lookup("gates.watchWindow"); got.Default != "2h" {
		t.Errorf("gates.watchWindow Default = %q, want %q", got.Default, "2h")
	}
	if got, _ := DefaultRegistry().Lookup("gates.watchInterval"); got.Default != "30s" {
		t.Errorf("gates.watchInterval Default = %q, want %q", got.Default, "30s")
	}
	got, ok := DefaultRegistry().Lookup("agents.allowUnsignedImport")
	if !ok {
		t.Fatalf("DefaultRegistry is missing agents.allowUnsignedImport")
	}
	if got.Type != TypeBool {
		t.Errorf("agents.allowUnsignedImport Type = %s, want bool", got.Type)
	}
	if got.Default != "false" {
		t.Errorf("agents.allowUnsignedImport Default = %q, want %q (the refusal)", got.Default, "false")
	}
	if !got.Sensitive {
		t.Errorf("agents.allowUnsignedImport is not sensitive; the adoption must be sensitive (CS8)")
	}
	if got.Area != "agents" {
		t.Errorf("agents.allowUnsignedImport Area = %q, want %q", got.Area, "agents")
	}
	if len(got.Description) < 40 {
		t.Errorf("agents.allowUnsignedImport Description is %d chars, want ≥ 40 (T2's rule)", len(got.Description))
	}

	// The ten backup keys (S62 T10): type, default, area, a non-empty
	// description, and no Sensitive flag (CS5).
	type wantBackup struct {
		key    string
		typ    SettingType
		def    string
		minOne bool
	}
	for _, w := range []wantBackup{
		{"backup.enabled", TypeBool, "true", false},
		{"backup.dir", TypeString, "", false},
		{"backup.interval", TypeString, "24h", false},
		{"backup.timeout", TypeString, "10m", false},
		{"backup.keep", TypeInt, "5", true},
		{"backup.keepDaily", TypeInt, "30", true},
		{"backup.keepWeekly", TypeInt, "12", true},
		{"backup.keepPreMigration", TypeInt, "5", true},
		{"backup.verifyAfter", TypeBool, "true", false},
		{"backup.staleAfter", TypeString, "48h", false},
	} {
		got, ok := DefaultRegistry().Lookup(w.key)
		if !ok {
			t.Fatalf("DefaultRegistry is missing %s", w.key)
		}
		if got.Type != w.typ {
			t.Errorf("%s Type = %s, want %s", w.key, got.Type, w.typ)
		}
		if got.Default != w.def {
			t.Errorf("%s Default = %q, want %q", w.key, got.Default, w.def)
		}
		if got.Area != "backup" {
			t.Errorf("%s Area = %q, want %q", w.key, got.Area, "backup")
		}
		if got.Description == "" {
			t.Errorf("%s Description is empty; a declaration needs one", w.key)
		}
		if got.Sensitive {
			t.Errorf("%s is sensitive; none of the ten is credential-shaped (CS5)", w.key)
		}
		if w.minOne {
			if got.Min == nil || *got.Min != 1 {
				t.Errorf("%s Min = %v, want 1 (a keep of zero deletes everything it keeps)", w.key, got.Min)
			}
		}
	}
	// backup.dir's description names the pre-migration exception (the one
	// sentence the key exists to keep honest).
	if dir, _ := DefaultRegistry().Lookup("backup.dir"); !strings.Contains(dir.Description, "pre-migration") {
		t.Errorf("backup.dir Description does not name the pre-migration exception: %q", dir.Description)
	}
}

// TestTestRegistryBuilds is a smoke test for the test-only registry: it
// declares the representative keys and they are all present (used by T3–T5).
func TestTestRegistryBuilds(t *testing.T) {
	r := testRegistry(t)
	for _, k := range []string{"test.flag", "test.count", "test.label", "test.mode", "test.guard"} {
		if _, ok := r.Lookup(k); !ok {
			t.Fatalf("testRegistry is missing %q", k)
		}
	}
}
