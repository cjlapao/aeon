package narrow

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/cjlapao/aeon/internal/errs"
	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// errors.go — the four discriminated kinds (O5), and the machinery that
// keeps them true against a daemon that is alive, sick, or wedged.
//
// A caller polling a loop needs to tell "your key is wrong" from "that
// loop does not exist" from "the daemon is restarting" — a single 400
// makes all three look like the caller's mistake, and the one that is NOT
// (a restarting daemon, where retrying is correct) is the one a merged
// kind would throw away. `refused` and `not-found` are kept apart ON
// PURPOSE: T5 made an absent action a 404 so a locked door never
// advertises itself; a domain refusal on a REAL action is 409. Merging
// them would make T5's absence indistinguishable from a rejection — and
// the difference is the whole of N14's argument on the error surface.
//
// | kind        | HTTP | when                                             |
// |-------------|------|--------------------------------------------------|
// | auth        | 401  | missing or wrong key (T2)                        |
// | not-found   | 404  | no such loop, step, or action (T5's absence)     |
// | refused     | 409  | the daemon understood and declined (domain)      |
// | transport   | 503  | restarting, store unavailable, timed out — retry |

type ErrorKind string

const (
	ErrAuth      ErrorKind = "auth"      // the key did not authenticate
	ErrNotFound  ErrorKind = "not-found" // no such loop, step, or — per N14 — action
	ErrRefused   ErrorKind = "refused"   // understood and declined — a domain refusal
	ErrTransport ErrorKind = "transport" // not answering now — retrying is correct
)

// Error is a discriminated refusal (AGENTS §5: errors are results; error is
// for infrastructure failure). It carries its kind AND the three parts
// (S23 C16): the kind is what a caller branches on, the three parts are
// what a human reads, and the same value holds both. The three parts live
// in an errs.Error, whose fields are unexported, so a narrow.Error cannot
// be built with a missing part any more than a raw literal can.
type Error struct {
	Kind ErrorKind
	Err  errs.Error
}

// New is the only constructor. It takes the kind and the three parts, so
// no narrow.Error can exist with a missing part — the structural rule C16
// demands, not a convention a reader might forget.
func New(kind ErrorKind, what, why, do string) *Error {
	return &Error{Kind: kind, Err: errs.New(what, why, do)}
}

// NewMsg is the convenience constructor for a call site that has one rich
// message: the message is the "what", and the "why"/"do" come from the
// kind's meaning — an actionable, kind-specific next step, never "check
// your configuration" (O4). A message that already names its own action
// keeps it in the "what"; the kind's "do" is the alternative a reader
// without the message would take.
func NewMsg(kind ErrorKind, msg string) *Error {
	why, do := kindParts(kind)
	return &Error{Kind: kind, Err: errs.New(msg, why, do)}
}

// kindParts is the ONE place a kind's meaning becomes an actionable why
// and do. It is table-driven so TestDoIsAnActionNotAdvice reads the same
// table a construction site does, and no site improvises its own advice.
func kindParts(k ErrorKind) (why, do string) {
	switch k {
	case ErrAuth:
		return "the narrow surface is one key, one user, and this key did not authenticate",
			"set the key in the environment or a key file (aeon init --key-file)"
	case ErrNotFound:
		return "nothing with that name exists on the narrow surface",
			"list what exists and name an existing one (aeon list)"
	case ErrRefused:
		return "the daemon understood the call and declined it — a domain refusal, not a transient fault",
			"fix the request so it satisfies the refusal; resending it unchanged will fail again"
	default: // ErrTransport
		return "the daemon could not serve this request now — a transient fault",
			retryHint
	}
}

// Error renders kind first (a caller that ignores kinds still branches on
// the prefix), then the three parts (a human reads them all).
func (e *Error) Error() string { return string(e.Kind) + ": " + e.Err.Error() }

// What/Why/Do expose the three parts a renderer or a caller can reach
// without the kind; the parts ride the same value as the kind.
func (e *Error) What() string { return e.Err.What() }
func (e *Error) Why() string  { return e.Err.Why() }
func (e *Error) Do() string   { return e.Err.Do() }

// statusFor is the ONE kind→code mapping — table-driven so
// TestFourErrorKindsAreDistinct reads the same table a caller can hit,
// and no handler gets to improvise a code.
func statusFor(k ErrorKind) int {
	switch k {
	case ErrAuth:
		return http.StatusUnauthorized
	case ErrNotFound:
		return http.StatusNotFound
	case ErrRefused:
		return http.StatusConflict
	case ErrTransport:
		return http.StatusServiceUnavailable
	}
	// An unclassified kind is a programming fault, and the honest answer
	// for a server's own confusion is on the server's side of the line.
	return http.StatusServiceUnavailable
}

// retryHint rides every transport error: the distinction that justifies
// the taxonomy is that a transport failure is RETRYABLE, and the body
// should say so where the kind already implies it (a caller that ignores
// kinds and greps prose still learns the right behaviour).
const retryHint = "the daemon could not serve this request — retry; if it persists, check `aeon doctor`"

// classifyDaemonError turns what the dispatcher returned into a kind.
//
// The discrimination is honest because the surface and the daemon share a
// process (T4's mounting through Table.Dispatch, not the socket): the
// ORIGINAL error is right here — not the JSON shadow a socket client
// would receive — so errors.As/Is read real type identity, not string
// resemblance. Codes are matched against the protocol package's own
// sentinels, never literals: when S20 allocates a new domain code, the
// default branch (typed refusal) still reads correctly, and only codes
// whose MEANING the table above pins are special-cased.
func classifyDaemonError(err error) *Error {
	// store's own miss first: the O4 property (a read resolves WITHIN this
	// daemon's store and a miss is not-found) is this branch, asserted on
	// the sentinel rather than on prose.
	if errors.Is(err, store.ErrNotFound) {
		return NewMsg(ErrNotFound, err.Error())
	}
	// The dispatcher's params-decode wrapper is TYPED (protocol.ErrBadParams
	// — it exists so the socket answers JSON-RPC's own -32602): the daemon
	// parsed the frame and refused the params. That is understood-and-
	// declined, and the retry the transport bucket promises cannot help —
	// the same bytes fail the same way.
	var bad protocol.ErrBadParams
	if errors.As(err, &bad) {
		return NewMsg(ErrRefused, "the daemon declined the params — " + bad.Error() + " — fix the params; resending them unchanged will fail again")
	}
	// loop.PinRefusal is the pin's own typed refusal (invalid-blueprint,
	// unavailable-skill, pin-unparseable, unavailable-handler): the start
	// request was understood and declined before any row was written.
	var pr loop.PinRefusal
	if errors.As(err, &pr) {
		return NewMsg(ErrRefused, "the loop was not started — " + pr.Error() + " — fix the blueprint and start again; nothing was pinned")
	}
	var pe *protocol.Error
	if errors.As(err, &pe) {
		switch pe.Code {
		case protocol.ErrStoreUnavailable.Code:
			// S21 T9's walk found the flattening: this branch rendered
			// Message + retryHint and DROPPED Data.What — so T6's
			// unwired-by-name answer ("no remote peers are wired in this
			// daemon — completions have nothing to bind to") and S20's
			// wiring answers arrived at the peer as a generic store outage
			// telling them to RETRY a request that will never succeed and
			// to check `aeon doctor` when the store is fine. A wiring fault
			// must say WHICH wiring is absent (a refusal names the thing
			// refused); the hint rides because a genuine store outage shares
			// this code and the retry prose is right for it.
			msg := pe.Message
			if pe.Data.What != "" {
				msg += " — " + pe.Data.What
			}
			return NewMsg(ErrTransport, msg + " — " + retryHint)
		case protocol.ErrStepNotFound.Code:
			return NewMsg(ErrNotFound, pe.Message)
		}
		// Every other typed Aeon error is a domain refusal: the daemon
		// understood the call — its own validator answered — and said no
		// (scope refusals, junk the handlers reject, closed gates). The
		// three-part payload's Todo is the alternative, and O3 says the
		// caller should read it here rather than guess from the code.
		msg := pe.Message
		if pe.Data.What != "" {
			msg += " — " + pe.Data.What
		}
		if pe.Data.Todo != "" {
			msg += " — " + pe.Data.Todo
		}
		return NewMsg(ErrRefused, msg)
	}
	// No type at all: infrastructure failed (a wedged dispatch, a dead
	// pipe). That is the retryable shape, and it says so.
	return NewMsg(ErrTransport, err.Error() + " — " + retryHint)
}

// writeError renders the discriminated shape (O5): kind first, message
// second, never a stack and never the key (I6 — the key never reaches
// here at all, and TestNoErrorBodyCarriesAKey asserts the absence on
// every path rather than trusting the plumbing). The "message" carries
// all three parts (S23 C16): the kind is in the body, the parts in the
// text, and a value field holds no credential (D6, C17).
func writeError(w http.ResponseWriter, e *Error) {
	writeJSON(w, statusFor(e.Kind), map[string]any{
		"error": map[string]string{"kind": string(e.Kind), "message": e.Err.Error()},
	})
}

// writeJSON is the ONE response writer: marshal fully into a buffer
// first, then hand the bytes to the wire under a stalled-write bound.
//
// The bound is T7's abandoned-caller half (D11/N8): a CI job that opens a
// connection, stops reading, and never closes it must not pin a handler
// goroutine indefinitely — a surface holding goroutines for dead readers
// degrades the thing it is watching. The response is complete in memory
// before the first byte goes out (no partials, no headers-then-panic);
// if the wire stalls past the write limit, the connection is hijacked
// and closed, which releases the blocked writer. Hijack-and-close is
// net/http's own handoff: once hijacked, the server will not touch the
// connection again, so the blocked Write erroring out is the designed
// outcome, not a race.
func writeJSON(w http.ResponseWriter, code int, payload any) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(payload); err != nil {
		// Encoding our own map failed — a programming fault, rendered as
		// the server-side kind it is, through the same single door.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"kind":"transport","message":"the narrow surface could not render a response — ` + retryHint + `"}}` + "\n"))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.Copy(w, bytes.NewReader(buf.Bytes()))
	}()
	select {
	case <-done:
	case <-time.After(writeLimit()):
		if h, ok := w.(http.Hijacker); ok {
			if conn, _, herr := h.Hijack(); herr == nil {
				_ = conn.Close() // releases the stalled io.Copy above
			}
		}
		<-done // let the writer unwind before the handler returns
	}
}

// The two surface bounds (W3's discipline, applied here: bounded like
// bash is). Defaults are generous — a healthy loop read answers in
// milliseconds; these exist for the wedged case, not the slow-network
// one. WithSurfaceTimeouts lets the daemon (and the tests) set them.
const (
	defaultCallTimeout = 10 * time.Second
	defaultWriteLimit  = 5 * time.Second
)

var (
	surfaceCallTimeout = defaultCallTimeout
	surfaceWriteLimit  = defaultWriteLimit
)

// WithSurfaceTimeouts sets the two bounds process-wide and returns the
// prior pair. One place because two surfaces (and their tests) must not
// drift on what "wedged" means.
func WithSurfaceTimeouts(callTimeout, writeTimeout time.Duration) (time.Duration, time.Duration) {
	prevCall, prevWrite := surfaceCallTimeout, surfaceWriteLimit
	if callTimeout > 0 {
		surfaceCallTimeout = callTimeout
	}
	if writeTimeout > 0 {
		surfaceWriteLimit = writeTimeout
	}
	return prevCall, prevWrite
}

func callTimeout() time.Duration { return surfaceCallTimeout }
func writeLimit() time.Duration  { return surfaceWriteLimit }
func callerTimedOut(d time.Duration) *Error {
	return NewMsg(ErrTransport, fmt.Sprintf("the daemon did not answer the dispatch within %s — %s", d, retryHint))
}
