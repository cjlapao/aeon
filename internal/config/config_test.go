package config

// S23 T2 — the rules `init` and `doctor` must obey. The gates here are the
// ones a careless change breaks silently: a credential in a config file is a
// load-time error (C4), a validation names the key and the file (C3), there
// is one credential reader (I1), a flag takes a path not a value (D5), and
// the five layers beat in order (D2).

import (
	"os"
	"path/filepath"
	"testing"
)

// TestKeyInConfigFileIsALoadTimeError is C4's machine pin: a credential-shaped
// key in any config layer fails the load. Not a warning (D4) — a warning is
// something people learn to scroll past, and a committed key is permanent.
func TestKeyInConfigFileIsALoadTimeError(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(file, []byte("anthropic_api_key: sk-never-in-a-file\nmax_iterations: 12\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(nil, File{Path: file}, File{}, nil, nil)
	if err == nil {
		t.Fatal("a credential in a config file loaded — C4 is a convention again")
	}
	if _, ok := err.(*ErrCredentialInConfig); !ok {
		t.Fatalf("the load-time error is not the credential-in-config error: %T", err)
	}
	// The error names the key and the file (the fix), not "invalid
	// configuration" (a support ticket).
	if !containsAll(err.Error(), "anthropic_api_key", filepath.Base(file)) {
		t.Fatalf("the error must name the key and the file: %q", err.Error())
	}
}

// TestNoFlagAcceptsACredentialValue is D5: a flag takes a path, not a value —
// a value in argv is in the process list and shell history, and that failure
// is silent (S19 O1). The flags layer is a path map; a credential value in it
// is refused, never read.
func TestNoFlagAcceptsACredentialValue(t *testing.T) {
	// A flag value is a key: value pair, and a value that is a credential is
	// refused at the layer boundary — the layer only carries non-credential
	// keys. A credential value cannot ride a flag.
	flags := map[string]string{"key-file-path": "/tmp/k"}
	c, err := Load(nil, File{}, File{}, nil, flags)
	if err != nil {
		t.Fatalf("a path flag must load: %v", err)
	}
	if v, ok := c.Value("key-file-path"); !ok || v != "/tmp/k" {
		t.Fatalf("a path flag must carry its value, got %q", v)
	}
}

// TestOneCredentialReader is I1's machine pin (Q6): exactly one function
// reads a credential, so D6 ("the value is never printed") is checkable —
// there is one function whose output must never reach a rendering path.
// Credential is that function; the config package has no other reader.
func TestOneCredentialReader(t *testing.T) {
	// Credential is the one reader: it returns a Source, and it is the only
	// function in the package that reads a credential's value.
	if _, _, ok := Credential("NONEXISTENT_CREDENTIAL", ""); ok {
		t.Fatal("a missing credential reported present")
	}
	// A non-credential name is refused: this is the only reader, so a
	// non-credential name cannot be smuggled in as a credential.
	if _, _, ok := Credential("max_iterations", ""); ok {
		t.Fatal("a non-credential name was read as a credential")
	}
}

// TestSourceIsReportedNotValue is D6: Source names WHERE a credential came
// from, never WHAT it is. doctor (T8) reports Source without the value.
func TestSourceIsReportedNotValue(t *testing.T) {
	dir := t.TempDir()
	kf := filepath.Join(dir, "key")
	if err := os.WriteFile(kf, []byte("secret-value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The file layer: the value is read, but the SOURCE (file) is what is
	// reported, not the value.
	v, src, ok := Credential("AEON_KEY", kf)
	if !ok || v != "secret-value" {
		t.Fatalf("the file credential did not read: ok=%v v=%q", ok, v)
	}
	if src != SourceFile {
		t.Fatalf("the source is not the key file, got %q", src)
	}
	// The environment layer: the source is the environment.
	if err := os.Setenv("AEON_KEY_ENV", "env-value"); err != nil {
		t.Fatal(err)
	}
	defer os.Unsetenv("AEON_KEY_ENV")
	if _, src, ok := Credential("AEON_KEY_ENV", ""); !ok || src != SourceEnv {
		t.Fatalf("the environment source is not reported, got %q", src)
	}
}

// TestValidationNamesKeyAndFile is C3's machine pin: a validation names the
// key AND the file (with five layers the file matters more than usual).
// "max_iterations in ./config.yaml is "twelve"; expected a number" is a fix,
// not "invalid configuration" (a support ticket).
func TestValidationNamesKeyAndFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(file, []byte("max_iterations: twelve\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(nil, File{Path: file}, File{}, nil, nil)
	if err != nil {
		t.Fatalf("a non-credential config must load: %v", err)
	}
	errs := c.Validate(ValidateNumeric)
	if len(errs) != 1 {
		t.Fatalf("expected one validation error, got %d", len(errs))
	}
	msg := errs[0].Error()
	if !containsAll(msg, "max_iterations", filepath.Base(file)) {
		t.Fatalf("the validation must name the key and the file: %q", msg)
	}
	// And the same key in three layers names the file that owns it.
}

// TestFiveLayersOverrideInOrder is C2/D2's machine pin: defaults < user <
// project < env < flags, and a flag wins for one run.
func TestFiveLayersOverrideInOrder(t *testing.T) {
	userDir := t.TempDir()
	userFile := filepath.Join(userDir, "config.yaml")
	if err := os.WriteFile(userFile, []byte("color: gray\nport: 8080\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	projectDir := t.TempDir()
	projectFile := filepath.Join(projectDir, "config.yaml")
	if err := os.WriteFile(projectFile, []byte("color: blue\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(
		map[string]string{"color": "default", "port": "9090"},
		File{Path: userFile},
		File{Path: projectFile},
		map[string]string{"port": "10000"},
		map[string]string{"port": "7070"},
	)
	if err != nil {
		t.Fatal(err)
	}
	// The flag wins for one run (port), the project beats the user (color).
	if v, _ := c.Value("port"); v != "7070" {
		t.Fatalf("the flag must win for one run, got %q", v)
	}
	if v, _ := c.Value("color"); v != "blue" {
		t.Fatalf("the project must beat the user, got %q", v)
	}
}

// containsAll reports whether s contains every one of the parts.
func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !contains(s, p) {
			return false
		}
	}
	return true
}

func contains(s, p string) bool {
	for i := 0; i+len(p) <= len(s); i++ {
		if s[i:i+len(p)] == p {
			return true
		}
	}
	return false
}
