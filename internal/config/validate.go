package config

import (
	"fmt"

	"github.com/cjlapao/aeon/internal/errs"
)

// validate.go — the checks. A check returns whether a value is valid and,
// if not, what it expected; the config names the key AND the file (C3, D3)
// in the error, so the answer is a fix, not "invalid configuration" (a
// support ticket). With five layers the same key can sit in three of them,
// so the file the name points at is the one a person edits.

// ErrInvalid is a validation that could not be named — it names the key and
// the file, never "invalid configuration" (a support ticket). It carries the
// three parts (S23 C16): what, why, do.
type ErrInvalid struct {
	Err errs.Error
}

// NewInvalid is the only constructor. The key names the thing, the file the
// file; the do is the one fix. A validation that names neither is the
// failure C3 was written about.
func NewInvalid(key, file, expected, got string) *ErrInvalid {
	return &ErrInvalid{
		Err: errs.New(
			fmt.Sprintf("%s in %s is %q", quote(key), quote(file), got),
			"expected "+expected,
			"set "+quote(key)+" in "+quote(file)+" to "+expected,
		),
	}
}

// Error renders the three parts; What/Why/Do expose them for a renderer.
func (e *ErrInvalid) Error() string { return e.Err.Error() }
func (e *ErrInvalid) What() string  { return e.Err.What() }
func (e *ErrInvalid) Why() string   { return e.Err.Why() }
func (e *ErrInvalid) Do() string    { return e.Err.Do() }

// Validate names, for every value that fails a check, the key and the file
// (C3, D3). A key with no file layer (an environment or flag value) names no
// file, and says so — never a confident wrong file.
func (c Config) Validate(check func(key, value string) (ok bool, expected string)) []error {
	var out []error
	for k, v := range c.values {
		if ok, expected := check(k, v); !ok {
			f := c.layers[k]
			out = append(out, NewInvalid(k, f.Path, expected, v))
		}
	}
	return out
}

// ValidateNumeric is the one check the task names: a numeric key whose value
// is not a number. "max_iterations … is "twelve"; expected an integer" is
// the example; the check names the key and the file, the fix.
func ValidateNumeric(key, value string) (ok bool, expected string) {
	if value == "" {
		return true, "" // absent is not invalid
	}
	_, err := fmt.Sscanf(value, "%d")
	if err != nil {
		return false, "a number"
	}
	return true, ""
}
