package execenv

// A secret VALUE, and the only two doors it has.
//
// This type exists because a redaction applied on the way out is a redaction
// somebody forgets to call. AGENTS §4c settled the pattern for route keys
// ("a credential goes IN and never comes OUT … not a redaction applied on the
// way out: a different type") and F17 settled it for the narrow key
// (`narrow.Key` prints `<redacted>` from String, GoString and MarshalJSON).
// This widens that pattern to every formatting verb, because `%x` and `%q` and
// a struct literal in `%+v` are all ways a value leaves.
//
// There is NO accessor returning the bytes. They leave this package in exactly
// two places: `ChildEnv.Env` (the child's environment, built once at spawn)
// and `Redactor` (the capture boundary, EV8). Anything else that wants to
// "just log the length" is refused by the absence of a method, not by a
// comment.

import (
	"fmt"
	"log/slog"
)

// redacted is the only thing any formatting path may print.
const redacted = "<redacted>"

// Secret holds the bytes of one resolved secret reference. The zero value is
// an empty secret, which resolution refuses to produce (EV7's minimum length)
// and Build refuses to inject.
type Secret struct {
	name string
	b    []byte
}

// Name is the reference's name — safe to print, log and store, and the label
// redaction reports under.
func (s Secret) Name() string { return s.name }

// Empty reports whether the secret carries nothing. It says nothing about
// length beyond "zero", which is what a caller needs to decide "refuse".
func (s Secret) Empty() bool { return len(s.b) == 0 }

// String, GoString, Format, MarshalJSON, MarshalText and LogValue all answer
// `<redacted>`. The set is exhaustive on purpose: `fmt` reaches a value
// through Stringer, GoStringer, Formatter, json.Marshaler, encoding.TextMarshaler
// and slog.LogValuer, and a type that plugs five of the six leaks through the
// sixth.
func (s Secret) String() string               { return redacted }
func (s Secret) GoString() string             { return "execenv.Secret(" + redacted + ")" }
func (s Secret) MarshalJSON() ([]byte, error) { return []byte(`"` + redacted + `"`), nil }
func (s Secret) MarshalText() ([]byte, error) { return []byte(redacted), nil }
func (s Secret) LogValue() slog.Value         { return slog.StringValue(redacted) }

// Format swallows every verb. A `Secret` holding a `%s` argument must print
// the same thing a `%v` does: the value's only formatting surface is the word
// that says it is not available.
func (s Secret) Format(f fmt.State, verb rune) {
	switch verb {
	case 'v':
		if f.Flag('+') || f.Flag('#') {
			fmt.Fprint(f, "execenv.Secret("+redacted+")")
			return
		}
		fmt.Fprint(f, redacted)
	case 'T':
		fmt.Fprint(f, "execenv.Secret")
	case 'p':
		fmt.Fprint(f, redacted)
	default:
		fmt.Fprint(f, redacted)
	}
}

// newSecret wraps resolved bytes. Package-private, so the only producers are
// ResolveRef (T4) and this package's tests — a caller anywhere else cannot
// mint one from a string it read off a socket.
func newSecret(name string, b []byte) Secret {
	cp := make([]byte, len(b))
	copy(cp, b)
	return Secret{name: name, b: cp}
}

// Injection is one resolved secret reference paired with the variable name it
// is injected as. Like Secret, its bytes are unexported: `Build` and
// `NewRedactor` are the two consumers, and both are in this package.
type Injection struct {
	// Name is the secret_ref name (for the step record and the redaction
	// report). It is safe to print.
	Name string
	// InjectAs is the child's variable name. Validated before anything else
	// looks at it: it must be a legal variable name and must not be a denied
	// one (a secret reference whose inject_as is `AWS_SECRET_ACCESS_KEY` is a
	// credential smuggled in through the field meant to hold its replacement).
	InjectAs string

	value Secret
}

// NewInjection is the test-and-daemon constructor for one injection. It is
// exported only because the daemon resolves references outside this package
// and needs a way to hand the result back; it takes a Secret, which only this
// package can create, so it is not a door — the bytes were already inside.
func NewInjection(name, injectAs string, value Secret) Injection {
	return Injection{Name: name, InjectAs: injectAs, value: value}
}
