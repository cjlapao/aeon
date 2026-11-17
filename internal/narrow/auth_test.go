package narrow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// readModuleFile is the small helper the source-scan gates share.
func readModuleFile(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}

// TestKeyNeverOnACommandLine (O1, I6, A10): a grep over the CLI's own
// source — no flag may take a key VALUE. Only a path. A key in argv is in
// the process list and in every future reader of a shell history; nothing
// ever errors, which is what makes the exposure permanent.
func TestKeyNeverOnACommandLine(t *testing.T) {
	serve := readModuleFile(t, "cmd/aeon/serve.go")
	for _, forbidden := range []string{
		`fs.String("http-key"`,
		`fs.StringVar`, // no string variable bound to a key value
		`fs.String("key"`,
	} {
		if strings.Contains(serve, forbidden) {
			t.Errorf("serve.go defines %s — a key VALUE must never be a flag; only %s (a path) or %s", forbidden, "--http-key-file", KeyEnv)
		}
	}
	if !strings.Contains(serve, `"http-key-file"`) {
		t.Error("serve.go must offer --http-key-file (the out-of-band path source)")
	}
	// And the wiring is real, not merely flag-shaped: the serve path must
	// resolve the key through narrow and run the keyless check before
	// binding anything (T4 consumes what these produce).
	for _, want := range []string{"narrow.ResolveKey", "narrow.CheckKeyless"} {
		if !strings.Contains(serve, want) {
			t.Errorf("serve.go must call %s — the refusal is the serve path's first act, not a listener-side check", want)
		}
	}
	if KeyEnv != "AEON_HTTP_KEY" {
		t.Errorf("the environment source is fixed by the contract, got %q", KeyEnv)
	}
}

// TestKeyNeverLogged (I6, C9) exercises EVERY reachable error path and
// asserts no output contains either key. The one path that leaks is the
// unusual one nobody exercised — S16's T11 shape for a forge token, same
// reason.
func TestKeyNeverLogged(t *testing.T) {
	const expected = "sk-narrow-EXPECTED-do-not-print-9f2c"
	const supplied = "sk-narrow-SUPPLIED-do-not-print-71ab"
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "http.key")
	if err := os.WriteFile(keyFile, []byte(expected+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	k, err := ResolveKey(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	v := NewValidator(k)

	var seen []string
	check := func(where, out string) {
		if strings.Contains(out, expected) {
			t.Errorf("%s leaks the EXPECTED key: %s", where, out)
		}
		if strings.Contains(out, supplied) {
			t.Errorf("%s leaks the SUPPLIED key: %s", where, out)
		}
		seen = append(seen, where)
	}

	// 1. Auth failure — the unusual path itself.
	_, diag := v.ValidateAPIKey(nil, "", supplied)
	if !diag.HasErrors() {
		t.Fatal("a wrong key must fail")
	}
	for _, e := range diag.GetAllErrors() {
		check("ValidateAPIKey error", fmt.Sprintf("%s/%s", e.Code, e.Message))
	}

	// 2. The key type printed any way a log line could print it.
	check("Key %v", fmt.Sprintf("%v", k))
	check("Key %#v", fmt.Sprintf("%#v", k))
	check("Key %s", fmt.Sprintf("%s", k))
	b, err := json.Marshal(k)
	if err != nil {
		t.Fatal(err)
	}
	check("Key JSON", string(b))
	check("Validator %v", fmt.Sprintf("%v", v))

	// 3. A recorded request line (T4's/T7's consumer of Redact): the caller's
	//    OWN correct key echoed into a debug capture — what Redact exists
	//    for. Arbitrary supplied junk is handled by never recording the
	//    Authorization header at all (AGENTS §4c: transports log method
	//    names, never params); Redact is the second door for the configured
	//    value wherever it can appear.
	rec := fmt.Sprintf("POST /v1/loops Authorization: %s", expected)
	red := k.Redact(rec)
	check("recorded request (redacted)", red)
	if !strings.Contains(red, "<redacted>") {
		t.Error("Redact must replace the key with <redacted>, got: " + red)
	}

	// 4. Startup refusal lines and the missing-file path — they name paths
	//    and reasons, never values.
	check("keyless refusal", CheckKeyless("127.0.0.1:7777", Key{}).Error())
	_, err = ResolveKey(filepath.Join(dir, "missing.key"))
	check("missing file error", err.Error())
	os.WriteFile(filepath.Join(dir, "empty.key"), []byte("   \n"), 0o600)
	_, err = ResolveKey(filepath.Join(dir, "empty.key"))
	check("empty key file error", err.Error())
	t.Setenv(KeyEnv, supplied)
	_, err = ResolveKey("")
	check("env key never echoed (empty-after-trim case)", func() string {
		// A whitespace-only env value must refuse by NAME of the variable,
		// and obviously never echo it.
		t.Setenv(KeyEnv, "   ")
		_, err := ResolveKey("")
		if err == nil {
			t.Fatal("a blank env key must refuse")
		}
		return err.Error()
	}())

	if len(seen) < 11 {
		t.Errorf("this gate must exercise every path (%d seen)", len(seen))
	}
}

// TestAddressWithoutKeyRefusesToStart (N15, D7, C7): the verbatim refusal —
// not a warning, not a generated default. A generated default would be worse
// than a refusal because it works, and something that works is something
// nobody revisits.
func TestAddressWithoutKeyRefusesToStart(t *testing.T) {
	if err := CheckKeyless("127.0.0.1:7777", Key{}); err == nil {
		t.Fatal("an address without a key must refuse to start")
	} else if err.Error() != RefusalKeyless {
		t.Errorf("the refusal is contractual, verbatim:\n want %q\n  got %q", RefusalKeyless, err.Error())
	}
	if err := CheckKeyless("", Key{}); err != nil {
		t.Errorf("no address needs no key — the daemon binds nothing by default: %v", err)
	}
	k := Key{secret: []byte("some-configured-key-value")}
	if err := CheckKeyless("0.0.0.0:7777", k); err != nil {
		t.Errorf("address + key must be allowed to proceed to binding: %v", err)
	}
	// The refusal says WHAT and WHY (the refusal names its reason doctrine).
	for _, want := range []string{"refused:", "requires a key", "open to anything on this host"} {
		if !strings.Contains(RefusalKeyless, want) {
			t.Errorf("the refusal must contain %q", want)
		}
	}
}

// TestMissingKeyFileIsANamedError (Q9): the message names the path it
// tried — "no such file" without the path sends someone hunting through
// their whole invocation.
func TestMissingKeyFileIsANamedError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "never-written.key")
	_, err := ResolveKey(path)
	if err == nil {
		t.Fatal("a missing explicit key file must be an error")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("the error must name the path it tried, got: %v", err)
	}
}

// TestAuthErrorIsDiscriminated (O5): auth is distinguishable from not-found
// and from transport. A caller must tell "your key is wrong" from "that loop
// does not exist" from "the daemon is restarting" — and each kind renders
// with its name first.
func TestAuthErrorIsDiscriminated(t *testing.T) {
	k := Key{secret: []byte("a-key-of-some-length-here")}
	v := NewValidator(k)
	_, diag := v.ValidateAPIKey(nil, "", "the-wrong-key-entirely")
	errs := diag.GetAllErrors()
	if len(errs) == 0 || errs[0].Code != string(ErrAuth) {
		t.Fatalf("an auth failure must carry code %q, got %+v", ErrAuth, errs)
	}
	if _, d := v.GetUserByID(nil, "", "anyone"); len(d.GetAllErrors()) == 0 || d.GetAllErrors()[0].Code != string(ErrNotFound) {
		t.Fatalf("the user question must answer %q, not a fabricated user", ErrNotFound)
	}
	if _, err := v.ValidateToken("eyJhbGciOi"); err == nil || !strings.HasPrefix(err.Error(), string(ErrAuth)+":") {
		t.Errorf("a bearer attempt must be an %q failure, got %v", ErrAuth, err)
	}
	// The three kinds are distinct values — collision here would collapse
	// the discrimination the type exists for.
	kinds := map[ErrorKind]bool{ErrAuth: true, ErrNotFound: true, ErrTransport: true}
	if len(kinds) != 3 {
		t.Fatal("the three kinds must be distinct")
	}
	e := NewMsg(ErrTransport, "the daemon is not answering")
	// S23 C16: the error renders its kind first, then the three parts
	// (what carries the message, plus the kind's why and do).
	if !strings.HasPrefix(e.Error(), "transport: ") {
		t.Errorf("a discriminated error renders its kind first, got %q", e.Error())
	}
	if !strings.Contains(e.Error(), "the daemon is not answering") {
		t.Errorf("the message is not in the rendered error, got %q", e.Error())
	}
}

// TestComparisonIsConstantTime: a wrong key whose PREFIX is correct must
// take the same time as one that is wrong from the first character. A naive
// == short-circuits on the first mismatch and hands an attacker a byte-at-
// a-time oracle for the correct prefix; subtle.ConstantTimeCompare does not.
// The bound is deliberately coarse — this gate fails on the ORACLE, not on
// scheduler noise.
func TestComparisonIsConstantTime(t *testing.T) {
	const key = "aeon-narrow-key-0123456789abcdefghijklmnopqrstuvwxyz"
	v := NewValidator(Key{secret: []byte(key)})
	prefixWrong := key[:len(key)-1] + "!"       // right prefix, wrong tail
	firstWrong := "!" + key[1:len(key)-1] + "!" // wrong from byte zero
	if len(prefixWrong) != len(firstWrong) {
		t.Fatal("the two wrong keys must be the same length — length is not what this gate guards")
	}
	timeIt := func(supplied string) float64 {
		const rounds = 4000
		samples := make([]float64, 0, 5)
		for batch := 0; batch < 5; batch++ {
			var bad int
			start := time.Now().UnixNano()
			for i := 0; i < rounds; i++ {
				_, d := v.ValidateAPIKey(nil, "", supplied)
				if d.HasErrors() {
					bad++
				}
			}
			samples = append(samples, float64(time.Now().UnixNano()-start)/float64(rounds))
			if bad != rounds {
				t.Fatal("both keys must be rejected — timing of acceptance is not what is measured")
			}
		}
		sort.Float64s(samples)
		return samples[len(samples)/2] // median of batch means
	}
	tPrefix, tFirst := timeIt(prefixWrong), timeIt(firstWrong)
	ratio := tPrefix / tFirst
	if ratio < 0.34 || ratio > 2.9 {
		t.Errorf("constant-time comparison violated in spirit: correct-prefix/wrong-first ratio = %.2f (%.0fns vs %.0fns per compare) — a naive == is short-circuiting", ratio, tPrefix, tFirst)
	}
}

// TestEnvAndFileBothWork (Q9, stated precedence): both sources resolve, and
// the FILE takes precedence when both are present — a file is the deliberate
// act. And a missing file with the env set is STILL the named error: an
// explicitly requested file that silently fell through to the environment
// would mean the key in force was never the one intended.
func TestEnvAndFileBothWork(t *testing.T) {
	dir := t.TempDir()
	fileKey := filepath.Join(dir, "http.key")
	if err := os.WriteFile(fileKey, []byte("file-key-value-0000\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(KeyEnv, "env-key-value-1111")

	k, err := ResolveKey(fileKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, d := NewValidator(k).ValidateAPIKey(nil, "", "file-key-value-0000"); d.HasErrors() {
		t.Error("the file must win when both sources are present")
	}
	if _, d := NewValidator(k).ValidateAPIKey(nil, "", "env-key-value-1111"); !d.HasErrors() {
		t.Error("the env value must NOT be in force when the file was given")
	}

	// Env alone works.
	k2, err := ResolveKey("")
	if err != nil {
		t.Fatal(err)
	}
	if _, d := NewValidator(k2).ValidateAPIKey(nil, "", "env-key-value-1111"); d.HasErrors() {
		t.Error("AEON_HTTP_KEY alone must authenticate")
	}

	// File requested + missing + env set ⇒ named error, no silent fallback.
	if _, err := ResolveKey(filepath.Join(dir, "absent.key")); err == nil {
		t.Error("a missing explicit file must not fall back to the environment")
	}

	// The empty expected key can never match — not even an empty supplied.
	empty := NewValidator(Key{})
	if _, d := empty.ValidateAPIKey(nil, "", ""); !d.HasErrors() {
		t.Error("an unconfigured validator must answer nothing, not match the empty string")
	}
}

// TestScaffoldingAuthDecisionRecorded (Q5): the scaffolding's auth trio —
// api_keys, role, claim — stays UNREGISTERED. It is built for multi-tenant
// claims, which is not one-key-one-user; configuring a multi-tenant service
// down to one key is a multi-tenant service someone will configure back up
// (S1's removal discipline). The decision lives at the framework's own
// documented seam — WithTokenValidator — and the refusal to register is
// recorded in the allow-list's own file.
func TestScaffoldingAuthDecisionRecorded(t *testing.T) {
	reg := readModuleFile(t, "internal/bootstrap/register.go")
	for _, svc := range []string{"api_keys.New(", "role.New(", "claim.New("} {
		if strings.Contains(reg, "svc.Register("+svc) || strings.Contains(reg, "Register("+svc) {
			t.Errorf("register.go must NOT register %s — Q5's answer is the TokenValidator seam, not a multi-tenant service configured down", svc)
		}
	}
	if !strings.Contains(reg, "api_keys") || !strings.Contains(reg, "role") {
		t.Error("the refusal itself belongs on the record: register.go must name what it refuses (B8 — the allow-list's reasons are its documentation)")
	}
	// And the seam is the documented one (Q3's reuse half).
	if !strings.Contains(reg, "WithTokenValidator") {
		t.Error("register.go records WithTokenValidator as the seam S19 uses — this test pins the record")
	}
}
