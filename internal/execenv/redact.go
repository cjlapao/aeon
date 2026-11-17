package execenv

// Redaction at the one boundary a command's output crosses (EV8).
//
// A command that has been given a credential will print it: `curl -v`, a
// stack trace, `set -x`, an error handler that quotes the request it failed
// on. Everything a step runs is captured once — the transcript, the activity
// rows and the live events are all downstream of that capture — so the filter
// has to be in the pipe, not at each reader, and it has to work on a STREAM:
// the output arrives in chunks of whatever size the reader chose, and a secret
// that straddles two chunks is still one secret.
//
// Two properties this file exists to keep:
//
//   - A match is found however the bytes are split. The writer holds back the
//     longest tail that could still turn out to be the start of a match and
//     prepends it to the next chunk. Holding back `maxLen-1` bytes is exactly
//     enough, and anything more is a buffer that grows with nothing.
//   - Only names come out. The replacement is `[secret:<name>]`, the count
//     report is per name, and no path here takes a value as an argument after
//     construction — the values arrive as `Injection`s whose bytes are
//     unexported, which is why this type lives in the same package as `Secret`.
//
// What it does NOT do is in `RedactionLimits` (vocab.go), and
// `TestRedactLimitsAreTrue` asserts the limits rather than admiring the
// matcher: a secret the command reversed, XORed or split with a space is not
// redacted, and a claim that this file catches more than it does would be the
// more dangerous bug.

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"sort"
	"sync"
)

// NameCount is one secret's replacement count, for the step record. It has no
// value field: the record's purpose is "this much redaction happened to this
// reference", and a value there would be the leak this file exists to prevent.
type NameCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// candidateForm is one byte sequence to look for, labelled with the reference
// it belongs to. Several forms share a name (the exact bytes, their base64,
// their hex, their percent-encoding); a hit on any of them counts under the
// same name.
type candidateForm struct {
	name string
	form []byte
}

// Redactor is the compiled filter. It is read-only after construction except
// for the counters, so one per spawn is the intended shape: build it from the
// injections the environment was built from, so the set that can leak is
// exactly the set that was injected, by construction rather than by a caller
// keeping two lists in step.
type Redactor struct {
	// forms is sorted longest-first, which is what makes a match prefer the
	// longest form at a position (see TestRedactLongestFirst).
	forms []candidateForm
	// starts is the set of bytes any form can begin with. It is the fast path:
	// a byte outside it can be copied out immediately, which is the common case
	// for command output that contains no secrets at all.
	starts [256]bool
	// maxLen is the longest form; the carry never exceeds maxLen-1 bytes.
	maxLen int

	mu     sync.Mutex
	counts map[string]int
}

// NewRedactor compiles the filter from the injections that were injected.
// Construction takes the whole set at once on purpose: a Redactor you can add
// to later is a Redactor somebody adds to after the first chunk has already
// gone out.
func NewRedactor(secrets []Injection) *Redactor {
	r := &Redactor{counts: make(map[string]int, len(secrets))}
	for _, in := range secrets {
		if len(in.value.b) == 0 {
			continue
		}
		for _, form := range candidateForms(in.value.b) {
			r.forms = append(r.forms, candidateForm{name: in.Name, form: form})
		}
	}
	if len(r.forms) == 0 {
		return r
	}
	sort.SliceStable(r.forms, func(i, j int) bool { return len(r.forms[i].form) > len(r.forms[j].form) })
	for _, c := range r.forms {
		r.starts[c.form[0]] = true
		if len(c.form) > r.maxLen {
			r.maxLen = len(c.form)
		}
	}
	return r
}

// candidateForms is every encoding of a value the matcher will look for: the
// exact bytes, standard and URL-safe base64 in padded and unpadded form,
// lowercase hex, and percent-encoding. Each is a real way a program prints a
// token — a JSON body base64s it, a URL percent-encodes it, a debug dump
// hex-escapes it — and a secret printed in any one of them is the same secret.
//
// A form equal to the exact bytes is dropped (it would double-count), as is an
// empty one.
func candidateForms(value []byte) [][]byte {
	// The exact bytes are always a form. The encodings are candidates, and one
	// equal to the exact bytes (a token of only unreserved characters has no
	// percent-escapes, for instance) is dropped so a hit cannot be counted
	// twice under the same name.
	out := [][]byte{value}
	encoded := [][]byte{
		[]byte(base64.StdEncoding.EncodeToString(value)),
		[]byte(base64.RawStdEncoding.EncodeToString(value)),
		[]byte(base64.URLEncoding.EncodeToString(value)),
		[]byte(base64.RawURLEncoding.EncodeToString(value)),
		[]byte(hex.EncodeToString(value)),
		[]byte(percentEncode(value))}
	for _, f := range encoded {
		if len(f) == 0 || bytes.Equal(f, value) {
			continue
		}
		out = append(out, f)
	}
	return out
}

// percentEncode is RFC 3986's escape: every byte outside the unreserved set
// becomes %XX with uppercase hex, which is what a URL-encoding library emits
// and therefore what ends up in a log line quoting a URL.
func percentEncode(b []byte) string {
	const hexDigits = "0123456789ABCDEF"
	var sb []byte
	for _, c := range b {
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '.', c == '_', c == '~':
			sb = append(sb, c)
		default:
			sb = append(sb, '%', hexDigits[c>>4], hexDigits[c&0xf])
		}
	}
	return string(sb)
}

// Empty reports whether the filter has nothing to look for, which the capture
// uses to skip wrapping a writer at all.
func (r *Redactor) Empty() bool { return len(r.forms) == 0 }

// Redactions is the per-name tally for the step record, sorted by name so two
// runs of the same command produce the same JSON.
func (r *Redactor) Redactions() []NameCount {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]NameCount, 0, len(r.counts))
	for name, n := range r.counts {
		out = append(out, NameCount{Name: name, Count: n})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (r *Redactor) tally(names []string) {
	if len(names) == 0 {
		return
	}
	r.mu.Lock()
	for _, n := range names {
		r.counts[n]++
	}
	r.mu.Unlock()
}

// RedactString is the one-shot filter for text that is already complete: the
// echoed command and the command-activity text. It runs the same matcher with
// nothing held back, so a string and the same bytes through a Writer agree by
// construction, not by two implementations agreeing by luck.
func (r *Redactor) RedactString(s string) string {
	if r.Empty() {
		return s
	}
	out, _, names := r.scan([]byte(s), false)
	r.tally(names)
	return string(out)
}

// Writer wraps dst with the streaming filter. dst is not closed by Close — it
// belongs to the capture, which has its own life cycle — and Close is
// idempotent because a caller that defers Close and also closes on the error
// path is the normal shape of this code.
func (r *Redactor) Writer(dst io.Writer) io.WriteCloser {
	return &redactWriter{r: r, dst: dst}
}

type redactWriter struct {
	r      *Redactor
	dst    io.Writer
	carry  []byte
	closed bool
}

func (w *redactWriter) Write(p []byte) (int, error) {
	if w.closed {
		return 0, fmt.Errorf("execenv: write to a closed redacting writer")
	}
	if w.r.Empty() {
		return w.dst.Write(p)
	}
	// The carry and the new bytes are one buffer: a match that began in the
	// previous chunk can only be found by looking at both.
	buf := make([]byte, 0, len(w.carry)+len(p))
	buf = append(buf, w.carry...)
	buf = append(buf, p...)
	out, keep, names := w.r.scan(buf, true)
	if len(out) > 0 {
		if _, err := w.dst.Write(out); err != nil {
			// The bytes are lost, so the carry is dropped too: holding a tail
			// that was never written would emit it later out of order.
			w.carry = nil
			return 0, err
		}
	}
	w.carry = keep
	w.r.tally(names)
	return len(p), nil
}

func (w *redactWriter) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	if len(w.carry) == 0 || w.r.Empty() {
		return nil
	}
	// The tail that never completed is output verbatim: it was not a secret, it
	// was the end of the stream. It still goes through the matcher, because the
	// carry is exactly where a complete match can be sitting (it was held back
	// as a possible prefix and no further bytes arrived).
	out, _, names := w.r.scan(w.carry, false)
	w.carry = nil
	if len(out) > 0 {
		if _, err := w.dst.Write(out); err != nil {
			return err
		}
	}
	w.r.tally(names)
	return nil
}

// scan replaces every occurrence of a candidate form, longest match first.
//
// keepTail says whether the tail of `data` may still be the beginning of a
// match that continues in the next chunk. When it is set, scanning stops at the
// first position from which the remaining bytes are a proper prefix of some
// form, and those bytes are returned as the carry. When it is not (a complete
// string, or the flush of a closed stream) nothing is held back.
func (r *Redactor) scan(data []byte, keepTail bool) (out []byte, keep []byte, names []string) {
	out = make([]byte, 0, len(data))
	i := 0
	for i < len(data) {
		// Fast path: copy up to the next byte that could begin a form.
		if !r.starts[data[i]] {
			j := i + 1
			for j < len(data) && !r.starts[data[j]] {
				j++
			}
			out = append(out, data[i:j]...)
			i = j
			continue
		}
		if name, n, ok := r.matchAt(data, i); ok {
			out = append(out, "[secret:"+name+"]"...)
			names = append(names, name)
			i += n
			continue
		}
		if keepTail && r.couldContinue(data[i:]) {
			// Everything from here on might yet become a match; hold it and let
			// the next chunk decide.
			break
		}
		out = append(out, data[i])
		i++
	}
	return out, data[i:], names
}

// matchAt returns the longest form that starts at i. The forms are sorted
// longest-first, so the first hit is the longest — and taking a shorter form
// that is a prefix of a longer one would leave the longer one's remainder in
// the output, which is a fragment of a secret in the transcript.
func (r *Redactor) matchAt(data []byte, i int) (string, int, bool) {
	rest := data[i:]
	for _, c := range r.forms {
		if len(c.form) <= len(rest) && bytes.Equal(rest[:len(c.form)], c.form) {
			return c.name, len(c.form), true
		}
	}
	return "", 0, false
}

// couldContinue reports whether b is a proper prefix of some form — the exact
// condition under which holding the bytes back is required rather than merely
// allowed.
func (r *Redactor) couldContinue(b []byte) bool {
	for _, c := range r.forms {
		if len(b) < len(c.form) && bytes.HasPrefix(c.form, b) {
			return true
		}
	}
	return false
}
