// Package config is the rules `init` (S23 T4) and `doctor` (S23 T8) must obey
// before either may exist: the five layers, the validation that names a key
// and a file, and the ONE credential rule.
//
// The whole argument is one sentence (C4): a config file gets committed, so a
// credential in one is a load-time error, not a warning — a warning is
// something people learn to scroll past, and a committed key is permanent and
// public. S19's O1 added the other half: a value in argv is in the process
// list and shell history, and that failure is silent. So a credential comes
// from the environment, or from a dedicated file path, and nowhere else.
//
// There is exactly ONE reader (I1, Q6): Credential. Every other path that
// needs a credential takes it from here, which is also what makes "the value
// is never printed" (D6) checkable — there is one function whose output must
// never reach a rendering path, and one whose Source (where it came from) may.
package config

import (
	"errors"
	"os"
	"strings"

	"github.com/cjlapao/aeon/internal/errs"
)

// Source names WHERE a credential came from, never WHAT it is. doctor (T8)
// reports Source to tell a person where the key is, without echoing the key
// (D6): "the key is in the environment" is fixable; the value is not printed.
type Source string

const (
	// SourceEnv is a credential read from an environment variable.
	SourceEnv Source = "environment"
	// SourceFile is a credential read from a dedicated key file (its path was
	// given; the value is never in argv, never in a config file).
	SourceFile Source = "key-file"
)

// isCredentialName answers the load-time question C4 makes it a load-time
// error for: is this config key a credential? A config file gets committed,
// so a key shaped like a credential is a permanent, public leak — a load-time
// error, not a warning.
func isCredentialName(name string) bool {
	n := strings.ToLower(name)
	for _, mark := range []string{"key", "token", "secret", "apikey", "api_key", "password", "credential"} {
		if strings.Contains(n, mark) {
			return true
		}
	}
	return false
}

// ErrCredentialInConfig is the load-time error C4 makes a credential-shaped
// key in a config file into: not a warning (a warning is something people
// learn to scroll past), a permanent and public failure. It carries the
// three parts (S23 C16): what, why, and an actionable do.
type ErrCredentialInConfig struct {
	Err errs.Error
}

// NewCredentialInConfig is the only constructor. The key names the thing and
// the file; the do is the one fix (remove it, use the environment or a key
// file path). It never names the credential's value — there is none to
// name, because a credential is never read from a config file.
func NewCredentialInConfig(key, file string) *ErrCredentialInConfig {
	return &ErrCredentialInConfig{
		Err: errs.New(
			"the key "+quote(key)+" is a credential and cannot live in "+quote(file),
			"a config file gets committed, so a credential in one is a permanent, public leak — this is a load-time error, not a warning",
			"remove "+quote(key)+" from "+quote(file)+"; put the key in the environment or a dedicated key file (aeon init --key-file)",
		),
	}
}

// Error renders the three parts; What/Why/Do expose them for a renderer.
func (e *ErrCredentialInConfig) Error() string { return e.Err.Error() }
func (e *ErrCredentialInConfig) What() string  { return e.Err.What() }
func (e *ErrCredentialInConfig) Why() string   { return e.Err.Why() }
func (e *ErrCredentialInConfig) Do() string    { return e.Err.Do() }

// Credential is the ONE place a credential is read (I1, Q6). It reads from
// the environment first, then from a dedicated key file whose path is given
// (never a config file, never argv, never a log). It returns Source so a
// caller can report where the key came from (D6) without the value ever
// reaching a rendering path.
//
// A name that is not a credential is refused: this is the only reader, so a
// non-credential name cannot be smuggled in as a credential.
func Credential(name string, keyFile string) (value string, source Source, ok bool) {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v, SourceEnv, true
	}
	if p := strings.TrimSpace(keyFile); p != "" {
		if b, err := os.ReadFile(p); err == nil {
			if v := strings.TrimSpace(string(b)); v != "" {
				return v, SourceFile, true
			}
		}
	}
	return "", "", false
}

// ErrEmpty is what Credential returns, through the error path, when a
// credential is required but neither the environment nor a key file has it.
// It is a domain refusal naming the missing key and the fix, not an
// infrastructure error (DB19).
var ErrEmpty = errors.New("the credential is not set in the environment or a key file — set it (aeon init --key-file)")
