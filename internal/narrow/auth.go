package narrow

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"os"
	"strings"

	api_ifaces "github.com/cjlapao/go-backend-scaffolding/framework/api/interfaces"
	api_keys_models "github.com/cjlapao/go-backend-scaffolding/framework/core/api_keys/models"
	"github.com/cjlapao/go-backend-scaffolding/framework/core/appctx"
	auth_models "github.com/cjlapao/go-backend-scaffolding/framework/core/auth/models"
	user_models "github.com/cjlapao/go-backend-scaffolding/framework/core/user/models"
	diagnostics "github.com/cjlapao/go-backend-scaffolding/framework/pkg/diagnostics"
)

// KeyEnv is the out-of-band environment source for the narrow-surface key
// (O1). The other source is --http-key-file. There is no third: never a
// flag value — a key on a command line is in the process list and in shell
// history, a failure that is silent and permanent.
const KeyEnv = "AEON_HTTP_KEY"

// RefusalKeyless is the verbatim refusal (N15, D7): an address without a
// key stops the start. Not a warning, and never a generated default — a
// generated key would be worse than a refusal precisely because it *works*.
const RefusalKeyless = "refused: --http requires a key; a bound port with no key is open to anything on this host"

// Key is the configured narrow-surface key. It is a type rather than a
// string for the §4c reason: a string leaks through whatever %v, log line
// or JSON blob happens to hold it, while this type's String and MarshalJSON
// answer <redacted>, so printing the credential becomes a thing a forgotten
// call site structurally cannot do. Reveal is unexported — the raw bytes
// leave this package only through constant-time comparison.
type Key struct{ secret []byte }

func (k Key) IsEmpty() bool    { return len(k.secret) == 0 }
func (k Key) String() string   { return "<redacted>" }
func (k Key) GoString() string { return "narrow.Key<redacted>" }
func (k Key) MarshalJSON() ([]byte, error) {
	return []byte(`"<redacted>"`), nil
}

// Hint is the LAST four characters. A prefix identifies a key's type and
// sometimes its project; a suffix only answers "which key is this" — which
// is the actual question (AGENTS §4c). It is the only derivative the type
// yields, and the framework's audit writes consume exactly this field
// (SEC-006: "the raw secret must never reach these writes").
func (k Key) Hint() string {
	s := string(k.secret)
	if len(s) <= 4 {
		return "····"
	}
	return "…" + s[len(s)-4:]
}

func (k Key) reveal() []byte { return append([]byte(nil), k.secret...) }

// Redact replaces every occurrence of the key in s. Any line that records a
// request (T4's startup context, T7's error captures) goes through this —
// the one key that ever appears in a log is the one on a path nobody tested,
// so the helper is exported for the mountings' use and the I6 test exercises
// every path they can reach.
func (k Key) Redact(s string) string {
	if k.IsEmpty() {
		return s
	}
	return strings.ReplaceAll(s, string(k.secret), "<redacted>")
}

// ResolveKey answers Q9. Sources, in precedence order — stated, never
// resolved dynamically at request time:
//
//  1. the file at keyFile (the --http-key-file path), if a path was given;
//  2. AEON_HTTP_KEY, when no path was given.
//
// A file that was explicitly asked for and is missing is a NAMED error
// naming the path tried (TestMissingKeyFileIsANamedError) — silently
// falling back to the environment would mean the caller's intended key was
// never the one in force, which is the kind of thing that works until it
// matters. An empty or whitespace-only key is refused at start from either
// source: a key that matches nothing is a misconfiguration, and a start that
// no caller can use is exactly the something-that-works nobody revisits.
func ResolveKey(keyFile string) (Key, error) {
	if keyFile != "" {
		raw, err := os.ReadFile(keyFile)
		if err != nil {
			return Key{}, fmt.Errorf("refused: %s: %w", keyFile, err)
		}
		k := Key{secret: []byte(strings.TrimSpace(string(raw)))}
		if k.IsEmpty() {
			return Key{}, fmt.Errorf("refused: %s is empty — a key that matches nothing is not a key", keyFile)
		}
		return k, nil
	}
	if env := os.Getenv(KeyEnv); env != "" {
		k := Key{secret: []byte(strings.TrimSpace(env))}
		if k.IsEmpty() {
			return Key{}, fmt.Errorf("refused: %s is set but blank after trimming — a key that matches nothing is not a key", KeyEnv)
		}
		return k, nil
	}
	return Key{}, nil // no address ⇒ no key needed; CheckKeyless decides whether that is fatal
}

// CheckKeyless is the N15 gate the daemon runs before binding anything
// (T4 calls it on the serve path; it lives here so the refusal is testable
// without a listener): an explicit address with no key is the verbatim
// refusal, and an absent address needs nothing.
func CheckKeyless(httpAddr string, k Key) error {
	if httpAddr != "" && k.IsEmpty() {
		return errors.New(RefusalKeyless)
	}
	return nil
}

// ErrorKind is the O5 discrimination. A caller polling a loop must tell
// The discriminated Error and its four kinds live in errors.go (T7)
// — where the kind→code table and the daemon-error classification are
// one readable surface rather than three coordinated files.

// Validator is the one-key-one-user check, supplied to the framework at its
// documented seam — api.New(svc).WithTokenValidator(...) (Q5, answered and
// pinned by TestScaffoldingAuthDecisionRecorded).
//
// What the scaffolding provides for auth is api_keys, role and claim —
// scaffolding-internal services built for multi-tenant claims. Registering
// one and configuring it down to a single key would be S1's anti-pattern: a
// multi-tenant auth service is a multi-tenant auth service someone will
// later configure back up. So nothing is registered; the framework's auth
// middleware gates secured routes on THIS four-method surface instead.
// Reused: the middleware, the route security model, the audit plumbing.
// Rebuilt: nothing — the three methods that speak of users, tenants and
// last-used bookkeeping answer honestly that this surface has none of those
// things, rather than simulating tables that do not exist.
type Validator struct{ expected Key }

var _ api_ifaces.TokenValidator = (*Validator)(nil)

// NewValidator returns the validator for the configured key. An empty
// expected key can never match — including an empty supplied one; the
// empty-vs-empty ConstantTimeCompare success is refused by name below, so
// a misconfigured start is inert rather than open.
func NewValidator(k Key) *Validator { return &Validator{expected: k} }

// Expected reports whether this validator holds a key at all (T4's startup
// line prints "key configured" via this, never the value).
func (v *Validator) Expected() bool { return !v.expected.IsEmpty() }

// ValidateAPIKey is the gate the framework's middleware calls for an
// API-key-secured route (the header form is a bare `Authorization: <key>`,
// measured in the middleware source; Bearer on such a route is refused
// upstream before this is reached).
func (v *Validator) ValidateAPIKey(ctx appctx.AppContext, tenantID, apiKey string) (*api_keys_models.ApiKey, *diagnostics.Diagnostics) {
	d := diagnostics.New("narrow.auth.ValidateAPIKey")
	if v.expected.IsEmpty() {
		d.AddError(string(ErrAuth), "no key is configured — this surface answers nothing", "narrow")
		return nil, d
	}
	// Constant time in the CONTENT. Lengths differing is not a leak worth
	// guarding: a key's length is not the secret, and padding to a global
	// maximum would only hide it from the operator who has the file.
	if subtle.ConstantTimeCompare([]byte(apiKey), v.expected.reveal()) != 1 {
		// The message names neither value (I6): the key that was tried and
		// the key that was expected are equally unprintable here.
		d.AddError(string(ErrAuth), "invalid api key", "narrow")
		return nil, d
	}
	return &api_keys_models.ApiKey{
		Name: "aeon-narrow",
		// KeyPrefix is the only field the framework's audit writes are
		// trusted to carry (SEC-006); it holds the suffix hint, §4c's
		// "which key is this", never a prefix of the secret and never
		// more. KeyHash/PlaintextKey carry nothing — their JSON tags are
		// already "-" and this code never populates them.
		KeyPrefix: v.expected.Hint(),
	}, d
}

// ValidateToken: bearer/JWT is not this surface's vocabulary. One key, one
// user — there are no claims to mint. (The framework's own middleware
// refuses Bearer on an API-key route before reaching here; this is the
// second door, so the surface never answers a bearer attempt with a
// plausible wrong yes.)
func (v *Validator) ValidateToken(tokenString string) (*auth_models.AuthClaims, error) {
	return nil, NewMsg(ErrAuth, "bearer tokens are not supported on the narrow surface — one key, one user")
}

// UpdateAPIKeyLastUsed: there is no key store, so there is no last_used
// column to age. The key is one configured value; bookkeeping would be a
// fiction, and an honest no-op answers better than a table nobody reads.
func (v *Validator) UpdateAPIKeyLastUsed(ctx appctx.AppContext, tenantID, apiKeyID string) *diagnostics.Diagnostics {
	return diagnostics.New("narrow.auth.UpdateAPIKeyLastUsed")
}

// GetUserByID: there are no users on this surface — there is a key, and a
// key authenticates a caller without making it a human (N16's framing,
// arriving here as a not-found rather than a fabricated user row).
func (v *Validator) GetUserByID(ctx appctx.AppContext, tenantID, userID string) (*user_models.User, *diagnostics.Diagnostics) {
	d := diagnostics.New("narrow.auth.GetUserByID")
	d.AddError(string(ErrNotFound), "the narrow surface has no users — a key authenticates a caller, it does not create one (N16)", "narrow")
	return nil, d
}
