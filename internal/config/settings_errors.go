package config

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/cjlapao/aeon/internal/errs"
)

// settings_errors.go — the three-part domain refusals S44 speaks in (S23 C16).
// They wrap errs.Error (like creds.go's ErrCredentialInConfig) and expose
// What()/Why()/Do(). They are DOMAIN refusals (DB19), not infrastructure
// errors: a typo or a bad value is the caller's mistake, not a broken machine.
// Defined here so T3 (the write boundary) adds no file to this error family.

// ErrSettingUnknown is the typed refusal for a key that is not a declared
// setting (CS10): an undeclared key is a refusal that names the nearest
// declared keys, never an empty success.
type ErrSettingUnknown struct {
	Key     string
	Nearest []string
	Err     errs.Error
}

// NewSettingUnknown is the only constructor. Nearest are the declared keys
// closest to Key (see Registry.Nearest); when the registry is empty the do is
// the honest "no settings are declared yet" rather than a confident typo.
func NewSettingUnknown(key string, nearest []string) *ErrSettingUnknown {
	what := quote(key) + " is not a declared setting"
	why := "only declared settings can be read or written, so a typo cannot create a silent no-op"
	do := "no settings are declared yet"
	if len(nearest) > 0 {
		do = "use one of: " + strings.Join(nearest, ", ")
	}
	return &ErrSettingUnknown{Key: key, Nearest: nearest, Err: errs.New(what, why, do)}
}

// Error renders the three parts; What/Why/Do expose them for a renderer.
func (e *ErrSettingUnknown) Error() string { return e.Err.Error() }
func (e *ErrSettingUnknown) What() string  { return e.Err.What() }
func (e *ErrSettingUnknown) Why() string   { return e.Err.Why() }
func (e *ErrSettingUnknown) Do() string    { return e.Err.Do() }

// ErrSettingInvalidValue is the typed refusal for a value that fails a
// declared key's type or bounds (CS10): it names the key, the expected form
// and what was given. Got is rendered %q and truncated to 80 bytes so a
// megabyte of input cannot fill a log.
type ErrSettingInvalidValue struct {
	Key      string
	Type     SettingType
	Expected string
	Got      string
	Err      errs.Error
}

// NewSettingInvalidValue is the only constructor; it derives Expected and the
// fix from the setting's type. ParseValue is its only caller in this slice.
func NewSettingInvalidValue(s Setting, got string) *ErrSettingInvalidValue {
	expected := expectedForm(s)
	return &ErrSettingInvalidValue{
		Key:      s.Key,
		Type:     s.Type,
		Expected: expected,
		Got:      got,
		Err: errs.New(
			quote(s.Key)+" cannot be set to "+renderGot(got),
			"expected "+expected,
			fixFor(s),
		),
	}
}

// Error renders the three parts; What/Why/Do expose them for a renderer.
func (e *ErrSettingInvalidValue) Error() string { return e.Err.Error() }
func (e *ErrSettingInvalidValue) What() string  { return e.Err.What() }
func (e *ErrSettingInvalidValue) Why() string   { return e.Err.Why() }
func (e *ErrSettingInvalidValue) Do() string    { return e.Err.Do() }

// ErrSettingNotWritable is reserved for T3's defensive credential-name
// re-check (CS5) and any future per-key write lock; it is defined here so T3
// adds no file to this error family.
type ErrSettingNotWritable struct {
	Key    string
	Reason string
	Err    errs.Error
}

// NewSettingNotWritable is the only constructor; the reason is the why and the
// do names the one fix (route a credential through config.Credential).
func NewSettingNotWritable(key, reason string) *ErrSettingNotWritable {
	return &ErrSettingNotWritable{
		Key:    key,
		Reason: reason,
		Err: errs.New(
			quote(key)+" cannot be written",
			reason,
			"remove "+quote(key)+" from the setting; a credential-shaped name routes through config.Credential",
		),
	}
}

// Error renders the three parts; What/Why/Do expose them for a renderer.
func (e *ErrSettingNotWritable) Error() string { return e.Err.Error() }
func (e *ErrSettingNotWritable) What() string  { return e.Err.What() }
func (e *ErrSettingNotWritable) Why() string   { return e.Err.Why() }
func (e *ErrSettingNotWritable) Do() string    { return e.Err.Do() }

// expectedForm is the Expected text for a key's type (CS10's "expected").
func expectedForm(s Setting) string {
	switch s.Type {
	case TypeBool:
		return `"true" or "false"`
	case TypeInt:
		var b strings.Builder
		b.WriteString("-?integer")
		if s.Min != nil && s.Max != nil {
			fmt.Fprintf(&b, " in [%d, %d]", *s.Min, *s.Max)
		} else if s.Min != nil {
			fmt.Fprintf(&b, " >= %d", *s.Min)
		} else if s.Max != nil {
			fmt.Fprintf(&b, " <= %d", *s.Max)
		}
		return b.String()
	case TypeString:
		cap := s.MaxLen
		if cap == 0 {
			cap = defaultStringCap
		}
		return fmt.Sprintf("at most %d bytes of valid UTF-8, no NUL", cap)
	case TypeEnum:
		return strings.Join(s.EnumValues, "|")
	}
	return "a valid value"
}

// fixFor is the Do text for a key's type (CS10: for an enum the member list,
// for an int the range — one line, never "check your configuration").
func fixFor(s Setting) string {
	switch s.Type {
	case TypeBool:
		return `set it to "true" or "false"`
	case TypeInt:
		return "set an integer in the allowed range"
	case TypeString:
		return "set a value within the length and UTF-8 rules"
	case TypeEnum:
		return "set one of: " + strings.Join(s.EnumValues, ", ")
	}
	return "set a valid value"
}

// renderGot renders a refused value with %q, truncated to 80 bytes at a rune
// boundary with a visible … so a megabyte of input cannot fill a log.
func renderGot(got string) string {
	if len(got) <= 80 {
		return fmt.Sprintf("%q", got)
	}
	cut := 80
	for cut > 0 && !utf8.RuneStart(got[cut]) {
		cut--
	}
	return fmt.Sprintf("%q", got[:cut]+"…")
}
