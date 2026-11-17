package execenv

// Tests for the capture-boundary filter (EV8). The shape of every one of them
// is the same: put a value in, put bytes through, and assert the value is
// ABSENT from what came out — never that a marker is present, because a marker
// proves the test ran and nothing else.

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// The sentinels are long, unmistakable, and appear nowhere in the assertions
// except as things that must NOT be found.
const (
	sentinelAlpha = "Alpha9f3xQk2Lm7pRt4Vw8Zy+/Q="
	sentinelBravo = "Bravo5d1nHq6Js3Nb9Kc2Ug7Xe+/R="
	sentinelShort = "shortkey9f3xQ+"
	sentinelLong  = "shortkey9f3xQ+Lm7pRt4Vw"
)

func injectionFor(name, injectAs, value string) Injection {
	return NewInjection(name, injectAs, newSecret(name, []byte(value)))
}

// runThrough writes parts through one Writer and returns what the destination
// received.
func runThrough(t *testing.T, r *Redactor, parts ...[]byte) []byte {
	t.Helper()
	var dst bytes.Buffer
	w := r.Writer(&dst)
	for _, p := range parts {
		if _, err := w.Write(p); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return dst.Bytes()
}

func TestRedactExactAndEncodedForms(t *testing.T) {
	r := NewRedactor([]Injection{injectionFor("db-url", "DATABASE_URL", sentinelAlpha)})
	want := "[secret:db-url]"
	forms := map[string]string{
		"exact":       sentinelAlpha,
		"b64-std":     base64.StdEncoding.EncodeToString([]byte(sentinelAlpha)),
		"b64-std-raw": base64.RawStdEncoding.EncodeToString([]byte(sentinelAlpha)),
		"b64-url":     base64.URLEncoding.EncodeToString([]byte(sentinelAlpha)),
		"b64-url-raw": base64.RawURLEncoding.EncodeToString([]byte(sentinelAlpha)),
		"hex":         hex.EncodeToString([]byte(sentinelAlpha)),
		"percent":     percentEncode([]byte(sentinelAlpha)),
		"in-a-url":    "https://api.example.com/x?token=" + sentinelAlpha,
		"in-json":     `{"token":"` + sentinelAlpha + `"}`,
		"b64-in-json": `{"token":"` + base64.StdEncoding.EncodeToString([]byte(sentinelAlpha)) + `"}`,
	}
	for label, input := range forms {
		got := runThrough(t, r, []byte("prefix "+input+" suffix"))
		if bytes.Contains(got, []byte(sentinelAlpha)) {
			t.Errorf("%s: the value passed through the filter — the matcher does not know this form\n%s", label, got)
		}
		if !bytes.Contains(got, []byte(want)) {
			t.Errorf("%s: nothing was replaced, so the value was not there to replace (%s)", label, got)
		}
		if !bytes.HasPrefix(got, []byte("prefix ")) || !bytes.HasSuffix(got, []byte(" suffix")) {
			t.Errorf("%s: the filter ate bytes around the match: %s", label, got)
		}
	}
}

func TestRedactAcrossChunkBoundaries(t *testing.T) {
	r := NewRedactor([]Injection{injectionFor("route", "ROUTE_KEY", sentinelAlpha)})
	prefix, suffix := "lead:", "-end"
	full := prefix + sentinelAlpha + suffix
	// Every split point, including before the first byte and after the last:
	// a chunk boundary is chosen by the READER's buffer size, which is not a
	// number the redactor gets to know, so "it works except when the chunk
	// boundary lands here" is the failure this loop exists to catch.
	for cut := 0; cut <= len(full); cut++ {
		got := runThrough(t, r, []byte(full[:cut]), []byte(full[cut:]))
		if strings.Contains(string(got), sentinelAlpha) {
			t.Fatalf("split at %d leaked the value: %q", cut, got)
		}
		if want := prefix + "[secret:route]" + suffix; string(got) != want {
			t.Fatalf("split at %d produced %q, want %q", cut, got, want)
		}
	}
	// Byte at a time is the extreme: the carry must survive every state it can
	// be in, not just the ones a 4 KiB reader happens to produce.
	var every []byte
	for i := 0; i < len(full); i++ {
		every = append(every, full[i])
	}
	if got := runThrough(t, r, splitEvery(every, 1)...); strings.Contains(string(got), sentinelAlpha) {
		t.Errorf("byte-at-a-time leaked the value: %q", got)
	}
}

func TestRedactAtTheCapBoundary(t *testing.T) {
	r := NewRedactor([]Injection{injectionFor("db-url", "DATABASE_URL", sentinelAlpha)})
	// The capture reads in blocks and caps the transcript at a fixed size; the
	// secret is placed so it straddles that edge, because that is where a
	// matcher that forgets its carry loses a credential.
	const cap = 65536
	stream := append(append([]byte{}, bytes.Repeat([]byte("a"), cap-len(sentinelAlpha)/2)...),
		[]byte(sentinelAlpha)...)
	stream = append(stream, bytes.Repeat([]byte("b"), 4096)...)

	// ONE writer, many blocks: a fresh writer per block would lose the carry on
	// purpose and prove nothing.
	out := runThrough(t, r, splitEvery(stream, 1024)...)

	if bytes.Contains(out, []byte(sentinelAlpha)) {
		t.Error("the value crossed the cap boundary whole")
	}
	// A fragment is the specific failure: the bytes before the edge kept and the
	// rest redacted leaves half a token, which is still correlatable and still
	// a secret nobody meant to publish.
	if bytes.Contains(out, []byte(sentinelAlpha[:8])) {
		t.Error("the first 8 bytes survived at the cap boundary")
	}
	if bytes.Contains(out, []byte(sentinelAlpha[len(sentinelAlpha)-8:])) {
		t.Error("the last 8 bytes survived at the cap boundary")
	}
	if !bytes.Contains(out, []byte("[secret:db-url]")) {
		t.Error("nothing was redacted, so this test proved nothing about the boundary")
	}
	if bytes.Contains(out, []byte(base64.StdEncoding.EncodeToString([]byte(sentinelAlpha)))) {
		t.Error("the base64 form survived the boundary")
	}
}

// splitEvery cuts b into chunks of n bytes, the way a reader with an n-byte
// buffer hands a stream to a writer.
func splitEvery(b []byte, n int) [][]byte {
	var out [][]byte
	for off := 0; off < len(b); off += n {
		end := off + n
		if end > len(b) {
			end = len(b)
		}
		out = append(out, b[off:end])
	}
	return out
}

func TestRedactLongestFirst(t *testing.T) {
	// sentinelShort is a prefix of sentinelLong. Replacing the short form
	// wherever it appears leaves `ijklmn` behind — a tail of the longer secret
	// that is both a leak and a puzzle.
	r := NewRedactor([]Injection{
		injectionFor("short", "SHORT_KEY", sentinelShort),
		injectionFor("long", "LONG_KEY", sentinelLong),
	})
	got := string(runThrough(t, r, []byte("x="+sentinelLong+" y")))
	if got != "x=[secret:long] y" {
		t.Errorf("got %q, want the long secret matched whole with no tail", got)
	}
	if strings.Contains(got, sentinelShort) {
		t.Errorf("the short form matched inside the long one and left a tail: %q", got)
	}
	got = string(runThrough(t, r, []byte("x="+sentinelShort+" y")))
	if got != "x=[secret:short] y" {
		t.Errorf("the short secret alone should still be redacted, got %q", got)
	}
}

func TestRedactorReportsCountsByNameOnly(t *testing.T) {
	r := NewRedactor([]Injection{
		injectionFor("alpha", "A", sentinelAlpha),
		injectionFor("bravo", "B", sentinelBravo),
	})
	text := sentinelAlpha + " " + sentinelAlpha + " " + base64.StdEncoding.EncodeToString([]byte(sentinelBravo))
	_ = runThrough(t, r, []byte(text))
	got := r.Redactions()
	if len(got) != 2 {
		t.Fatalf("Redactions() = %+v, want one row per secret", got)
	}
	if got[0].Name != "alpha" || got[0].Count != 2 {
		t.Errorf("alpha: %+v, want 2 (the same secret twice is two redactions)", got[0])
	}
	if got[1].Name != "bravo" || got[1].Count != 1 {
		t.Errorf("bravo: %+v, want 1", got[1])
	}
	// The report must not carry a value, in any field, ever.
	for _, nc := range got {
		if strings.Contains(nc.Name, sentinelAlpha) || strings.Contains(nc.Name, sentinelBravo) {
			t.Errorf("the count report carries a value in its name: %q", nc.Name)
		}
	}
}

func TestRedactLimitsAreTrue(t *testing.T) {
	// This test asserts what the filter does NOT catch, so that
	// `RedactionLimits` cannot overclaim. If someone widens the matcher, this
	// fails and the sentence in vocab.go has to be widened with it.
	r := NewRedactor([]Injection{injectionFor("db-url", "DATABASE_URL", sentinelAlpha)})

	reversed := []byte(sentinelAlpha)
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	xored := make([]byte, len(sentinelAlpha))
	for i := range sentinelAlpha {
		xored[i] = sentinelAlpha[i] ^ 0x5a
	}
	split := sentinelAlpha[:len(sentinelAlpha)/2] + " " + sentinelAlpha[len(sentinelAlpha)/2:]

	for _, tc := range []struct{ label, text string }{
		{"reversed", string(reversed)},
		{"xor-0x5a", string(xored)},
		{"split-by-a-space", split},
		{"rot13", rot13(sentinelAlpha)},
	} {
		got := runThrough(t, r, []byte(tc.text))
		if bytes.Contains(got, []byte(tc.text)) {
			continue // untouched, as documented
		}
		t.Errorf("%s was altered by the filter; RedactionLimits does not claim it is caught, "+
			"so either the matcher grew or this test needs its claim widened too: %q", tc.label, got)
	}
	if !strings.Contains(RedactionLimits, "reversed") {
		t.Error("RedactionLimits does not name the transformations it cannot catch")
	}
}

func rot13(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		switch {
		case c >= 'a' && c <= 'z':
			b.WriteByte('a' + (c-'a'+13)%26)
		case c >= 'A' && c <= 'Z':
			b.WriteByte('A' + (c-'A'+13)%26)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

func TestRedactStringMatchesTheWriter(t *testing.T) {
	r := NewRedactor([]Injection{
		injectionFor("alpha", "A", sentinelAlpha),
		injectionFor("bravo", "B", sentinelBravo),
	})
	// The echoed command and the activity text go through RedactString; the
	// transcript goes through Writer. Two paths over one matcher must agree, or
	// the same secret is visible in one surface and masked in another.
	for _, s := range []string{
		"curl -H 'Authorization: Bearer " + sentinelAlpha + "'",
		"echo " + base64.StdEncoding.EncodeToString([]byte(sentinelBravo)),
		"nothing to see here",
		sentinelAlpha + sentinelBravo,
		"",
	} {
		viaString := r.RedactString(s)
		viaWriter := string(runThrough(t, r, []byte(s)))
		if viaString != viaWriter {
			t.Errorf("the two paths disagree on %q:\n  RedactString: %q\n  Writer:       %q", s, viaString, viaWriter)
		}
	}
}

func TestRedactorWithNoSecretsPassesThrough(t *testing.T) {
	r := NewRedactor(nil)
	if !r.Empty() {
		t.Error("a Redactor with no secrets is not Empty, so the capture will wrap writers for nothing")
	}
	payload := []byte("a line of output " + sentinelAlpha + " that nobody injected")
	got := runThrough(t, r, payload)
	if !bytes.Equal(got, payload) {
		t.Errorf("an empty filter changed the bytes: %q", got)
	}
	if len(r.Redactions()) != 0 {
		t.Errorf("an empty filter reported redactions: %+v", r.Redactions())
	}
	// An injection with an empty value contributes no form; without that guard
	// every output would be replaced at every position.
	r2 := NewRedactor([]Injection{{Name: "empty", InjectAs: "E"}})
	if !r2.Empty() {
		t.Error("an empty value compiled a form, which would match everywhere")
	}
}

func TestRedactorIsConcurrencySafe(t *testing.T) {
	// Two Writers from one Redactor is the real shape: the transcript and the
	// activity row are written from different goroutines of the same step, and
	// both tally into the same report the step record prints.
	r := NewRedactor([]Injection{injectionFor("alpha", "A", sentinelAlpha)})
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			var dst bytes.Buffer
			w := r.Writer(&dst)
			for j := 0; j < 20; j++ {
				_, _ = w.Write([]byte(fmt.Sprintf("line %d/%d %s end\n", n, j, sentinelAlpha)))
			}
			_ = w.Close()
			if strings.Contains(dst.String(), sentinelAlpha) {
				t.Errorf("goroutine %d leaked", n)
			}
		}(i)
	}
	wg.Wait()
	got := r.Redactions()
	if len(got) != 1 || got[0].Count != 80 {
		t.Errorf("Redactions() = %+v, want alpha:80 — a tally that loses a concurrent increment "+
			"under-reports how much redaction a step needed", got)
	}
}

func TestRedactWriterCloseIsIdempotentAndFlushes(t *testing.T) {
	r := NewRedactor([]Injection{injectionFor("alpha", "A", sentinelAlpha)})
	var dst bytes.Buffer
	w := r.Writer(&dst)
	// A match held back as a possible prefix has to come out at Close, or the
	// last secret of every step is missing from the transcript.
	if _, err := w.Write([]byte("tail " + sentinelAlpha)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Errorf("a second Close returned %v", err)
	}
	if strings.Contains(dst.String(), sentinelAlpha) {
		t.Errorf("Close did not flush the carry through the matcher: %q", dst.String())
	}
	if !strings.Contains(dst.String(), "tail [secret:alpha]") {
		t.Errorf("Close lost the tail: %q", dst.String())
	}
	if _, err := w.Write([]byte("more")); err == nil {
		t.Error("a write after Close was accepted; those bytes would appear after the stream ended")
	}
}

// TestSecretNeverFormatsItsValue belongs to secret.go but guards this file's
// premise: if a Secret could print its bytes, the filter would be the only
// thing between a value and a log line.
func TestSecretNeverFormatsItsValue(t *testing.T) {
	s := newSecret("db-url", []byte(sentinelAlpha))
	var b strings.Builder
	for _, form := range []string{
		fmt.Sprintf("%v", s), fmt.Sprintf("%+v", s), fmt.Sprintf("%#v", s), fmt.Sprintf("%s", s),
		fmt.Sprintf("%q", s), fmt.Sprintf("%x", s), fmt.Sprintf("%v", struct{ S Secret }{s}),
		fmt.Sprint(s), fmt.Sprintln(s),
	} {
		b.WriteString(form)
		if strings.Contains(form, sentinelAlpha) {
			t.Errorf("a formatting path printed the value: %s", form)
		}
	}
	if j, err := json.Marshal(s); err != nil || strings.Contains(string(j), sentinelAlpha) {
		t.Errorf("json leaked the value: %s (%v)", j, err)
	}
	if txt, err := (s).MarshalText(); err != nil || bytes.Contains(txt, []byte(sentinelAlpha)) {
		t.Errorf("MarshalText leaked the value: %s (%v)", txt, err)
	}
	if s.String() != "<redacted>" {
		t.Errorf("String() = %q, want <redacted>", s.String())
	}
	if strings.Contains(s.GoString(), sentinelAlpha) {
		t.Errorf("GoString printed the value: %q", s.GoString())
	}
	if s.Empty() {
		t.Error("Empty() says a populated secret is empty")
	}
	if !(Secret{}).Empty() {
		t.Error("the zero Secret is not Empty, so a caller cannot use it to mean nothing-resolved")
	}
	if s.Name() != "db-url" {
		t.Errorf("Name() = %q, want db-url — the name is the label redaction reports under", s.Name())
	}
}
