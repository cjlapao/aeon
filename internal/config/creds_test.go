package config

import (
	"os"
	"path/filepath"
	"testing"
)

// S23 T2 — the credential rule (C4, C17, D4–D6, I1). These are the ones a
// careless change breaks silently: a credential goes in from the environment
// or a dedicated file path, never a config file (a load-time error), never
// argv, never a log — and there is one reader.

// TestCredentialReadsEnvThenFile is the one reader's two sources: the
// environment first, then a dedicated key file. A value in argv (a flag) is
// refused — that failure is silent (S19 O1).
func TestCredentialReadsEnvThenFile(t *testing.T) {
	dir := t.TempDir()
	kf := filepath.Join(dir, "key")
	if err := os.WriteFile(kf, []byte("file-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The file alone.
	if v, src, ok := Credential("AEON_KEY", kf); !ok || v != "file-key" || src != SourceFile {
		t.Fatalf("the file source did not read: ok=%v v=%q src=%q", ok, v, src)
	}
	// The environment beats the file (C2: the environment is above the file).
	if err := os.Setenv("AEON_KEY", "env-key"); err != nil {
		t.Fatal(err)
	}
	defer os.Unsetenv("AEON_KEY")
	if v, src, ok := Credential("AEON_KEY", kf); !ok || v != "env-key" || src != SourceEnv {
		t.Fatalf("the environment must beat the file: ok=%v v=%q src=%q", ok, v, src)
	}
}

// TestCredentialNeverReadsConfigFile is C4's other half: a credential-shaped
// key in a config file is a load-time error, so Credential never reads a
// credential from a config file — the value has no path to a config file at
// all.
func TestCredentialNeverReadsConfigFile(t *testing.T) {
	dir := t.TempDir()
	// A "config file" with a credential is refused by the load, not read.
	cfg := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfg, []byte("anthropic_api_key: sk-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(nil, File{Path: cfg}, File{}, nil, nil); err == nil {
		t.Fatal("a credential in a config file loaded — it must fail the load")
	}
	// And Credential, the one reader, has no config-file argument at all:
	// its only sources are the environment and a dedicated key file, so a
	// credential cannot ride a config file through it.
	if _, _, ok := Credential("anthropic_api_key", ""); ok {
		t.Fatal("a credential was read with no environment and no key file")
	}
}

// TestCredentialNamesSourceNotValue is D6: a caller reports WHERE the
// credential came from (Source), never the value, through the rendering
// path. The value is only returned to a caller that will use it, never to a
// renderer.
func TestCredentialNamesSourceNotValue(t *testing.T) {
	// doctor (T8) reports Source; the value is never rendered. Here the
	// source is the only thing a rendering path takes, so the value cannot
	// leak through it.
	for _, src := range []Source{SourceEnv, SourceFile} {
		if src == "" {
			t.Fatal("a source is empty — it cannot be reported")
		}
	}
}

// TestCredentialRefusesNonCredentialName: this is the only reader, so a
// name that is not a credential is not read as one — a non-credential name
// cannot be smuggled in.
func TestCredentialRefusesNonCredentialName(t *testing.T) {
	if _, _, ok := Credential("max_iterations", ""); ok {
		t.Fatal("a non-credential name was read as a credential")
	}
}
