package model

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// collect drains both channels and returns the frames and the error.
func collect(t *testing.T, ctx context.Context, r io.Reader) ([]Frame, error) {
	t.Helper()
	frames, errs := ReadSSE(ctx, r)

	var got []Frame
	for f := range frames {
		got = append(got, f)
	}
	// The error channel is closed after the frame channel, so this cannot
	// block once frames is drained.
	var err error
	for e := range errs {
		if err == nil {
			err = e
		}
	}
	return got, err
}

// chunkedReader delivers a byte slice in fixed-size pieces, which is what a
// network connection does and what no single-Read test exercises.
type chunkedReader struct {
	data []byte
	size int
	pos  int
}

func (c *chunkedReader) Read(p []byte) (int, error) {
	if c.pos >= len(c.data) {
		return 0, io.EOF
	}
	end := c.pos + c.size
	if end > len(c.data) {
		end = len(c.data)
	}
	n := copy(p, c.data[c.pos:end])
	c.pos += n
	return n, nil
}

// # Provider-neutral on purpose
//
// This reader does not know what a provider is — it parses SSE framing, and
// both adapters feed from it (I3). An early version of this fixture used real
// Anthropic event names, which implied the reader cared about them and tripped
// D14's "no provider shape outside adapters/" check for a good reason: the
// shape genuinely did not belong here.
//
// The names below are deliberately not any provider's.
const sampleStream = "event: start\n" +
	"data: {\"kind\":\"start\",\"counts\":{\"in\":12}}\n" +
	"\n" +
	": this is a keep-alive comment\n" +
	"\n" +
	"event: chunk\n" +
	"data: {\"kind\":\"chunk\",\"piece\":{\"text\":\"Hello\"}}\n" +
	"\n" +
	"retry: 3000\n" +
	"\n" +
	"event: chunk\n" +
	"data: {\"kind\":\"chunk\",\"piece\":{\"text\":\" world 🌍\"}}\n" +
	"\n" +
	"event: finish\n" +
	"data: {\"kind\":\"finish\"}\n\n"

// D5 — **the test the dependency was worth having, kept without the
// dependency.** Replay the same bytes chunked at EVERY offset and assert the
// output is identical to the unchunked parse.
//
// An offset bug truncates a tool call under load, which is a bug that appears
// in production and not in a test that reads the whole stream at once.
func TestFramesParseIdenticallyWhenChunkedAtEveryOffset(t *testing.T) {
	ctx := context.Background()

	want, err := collect(t, ctx, strings.NewReader(sampleStream))
	if err != nil {
		t.Fatalf("the unchunked parse errored: %v", err)
	}
	if len(want) != 4 {
		t.Fatalf("the unchunked parse found %d frames, want 4", len(want))
	}

	for size := 1; size <= len(sampleStream); size++ {
		got, err := collect(t, ctx, &chunkedReader{data: []byte(sampleStream), size: size})
		if err != nil {
			t.Fatalf("chunk size %d errored: %v", size, err)
		}
		if len(got) != len(want) {
			t.Fatalf("chunk size %d yielded %d frames, want %d", size, len(got), len(want))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("chunk size %d, frame %d:\n  got  %+v\n  want %+v", size, i, got[i], want[i])
			}
		}
	}
}

// The multi-byte case explicitly: a `data:` line containing an emoji, split
// INSIDE that rune. This is the exact hazard M19 says Go removes — asserted
// rather than assumed.
func TestADataLineSplitInsideAMultiByteRuneReassembles(t *testing.T) {
	const stream = "data: {\"text\":\"🌍\"}\n\n"
	emojiAt := strings.Index(stream, "🌍")
	if emojiAt < 0 {
		t.Fatal("the fixture lost its emoji")
	}

	// Every split point inside the four bytes of the rune.
	for offset := emojiAt + 1; offset < emojiAt+4; offset++ {
		r := io.MultiReader(
			strings.NewReader(stream[:offset]),
			strings.NewReader(stream[offset:]),
		)
		got, err := collect(t, context.Background(), r)
		if err != nil {
			t.Fatalf("split at %d errored: %v", offset, err)
		}
		if len(got) != 1 {
			t.Fatalf("split at %d yielded %d frames, want 1", offset, len(got))
		}
		if !strings.Contains(got[0].Data, "🌍") {
			t.Fatalf("split at %d mangled the rune: %q", offset, got[0].Data)
		}
	}
}

// A reader that handles only `\n\n` works against one provider and truncates
// against a proxy that normalises line endings.
func TestBothSeparatorsSplitFrames(t *testing.T) {
	for _, tc := range []struct {
		name   string
		stream string
		want   int
	}{
		{"LF LF", "data: one\n\ndata: two\n\n", 2},
		{"CRLF CRLF", "data: one\r\n\r\ndata: two\r\n\r\n", 2},
		{"mixed", "data: one\r\n\r\ndata: two\n\n", 2},
		{"CR CR", "data: one\r\rdata: two\r\r", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := collect(t, context.Background(), strings.NewReader(tc.stream))
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != tc.want {
				t.Fatalf("got %d frames, want %d: %+v", len(got), tc.want, got)
			}
			if got[0].Data != "one" || got[1].Data != "two" {
				t.Fatalf("data mangled: %+v", got)
			}
		})
	}
}

// Some servers close without a trailing blank line. Dropping that frame loses
// the last delta — or the usage, which is worse, because the call then reports
// an unknown cost for no reason.
func TestAFinalFrameWithNoTrailingBlankLineIsDelivered(t *testing.T) {
	got, err := collect(t, context.Background(),
		strings.NewReader("data: first\n\ndata: last-with-no-blank-line"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d frames, want 2: %+v", len(got), got)
	}
	if got[1].Data != "last-with-no-blank-line" {
		t.Fatalf("the final frame was lost or mangled: %+v", got)
	}
}

func TestDoneSentinelTerminatesWithoutYieldingAFrame(t *testing.T) {
	got, err := collect(t, context.Background(),
		strings.NewReader("data: one\n\ndata: [DONE]\n\ndata: after-done\n\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d frames, want 1 — [DONE] is a sentinel, not content, and nothing "+
			"after it is read: %+v", len(got), got)
	}
	if got[0].Data != "one" {
		t.Fatalf("wrong frame: %+v", got[0])
	}
}

// Anthropic never sends [DONE]. Its absence must end the stream cleanly rather
// than looking like a truncation.
func TestAStreamWithNoDoneSentinelEndsCleanly(t *testing.T) {
	got, err := collect(t, context.Background(),
		strings.NewReader("event: finish\ndata: {\"kind\":\"finish\"}\n\n"))
	if err != nil {
		t.Fatalf("a stream ending without [DONE] reported an error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d frames, want 1", len(got))
	}
}

func TestCommentLinesAndRetryFieldsAreIgnored(t *testing.T) {
	got, err := collect(t, context.Background(), strings.NewReader(
		": keep-alive\n\nretry: 3000\n\nid: 42\n\ndata: real\n\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d frames, want 1 (only the data frame): %+v", len(got), got)
	}
	if got[0].Data != "real" {
		t.Fatalf("wrong frame: %+v", got[0])
	}
}

// The specification's rule, which OpenAI relies on for multi-line content.
func TestMultipleDataLinesInOneFrameJoinWithNewlines(t *testing.T) {
	got, err := collect(t, context.Background(), strings.NewReader("data: line one\ndata: line two\n\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d frames, want 1", len(got))
	}
	if got[0].Data != "line one\nline two" {
		t.Fatalf("data = %q, want the two lines joined with a newline", got[0].Data)
	}
}

// Exactly one leading space is framing; a second is data. Trimming all
// whitespace corrupts a value that legitimately begins with one.
func TestOnlyOneLeadingSpaceIsStripped(t *testing.T) {
	got, err := collect(t, context.Background(), strings.NewReader("data:  two spaces\n\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Data != " two spaces" {
		t.Fatalf("data = %q, want %q", got[0].Data, " two spaces")
	}
}

// A truncated stream reported as a clean end is a completed call that lost its
// tail — and the caller has no way to tell.
func TestAnErroringReaderPropagatesRatherThanClosingCleanly(t *testing.T) {
	boom := errors.New("connection reset by peer")
	r := io.MultiReader(strings.NewReader("data: first\n\n"), &failingReader{err: boom})

	got, err := collect(t, context.Background(), r)
	if err == nil {
		t.Fatal("a truncated stream ended cleanly; the caller would report a completed call")
	}
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the reader's own error", err)
	}
	// And the frames read before the failure are still delivered.
	if len(got) != 1 {
		t.Fatalf("got %d frames before the error, want 1", len(got))
	}
}

type failingReader struct{ err error }

func (f *failingReader) Read([]byte) (int, error) { return 0, f.err }

// Cancellation must stop promptly and release the body. A leaked HTTP body on a
// daemon meant to run for weeks is a slow file-descriptor leak.
func TestContextCancellationStopsAndReleasesTheReader(t *testing.T) {
	r := &closeTrackingReader{blockFor: 5 * time.Second}
	ctx, cancel := context.WithCancel(context.Background())

	frames, errs := ReadSSE(ctx, r)
	cancel()

	done := make(chan struct{})
	go func() {
		for range frames {
		}
		for range errs {
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("ReadSSE did not stop within 3s of cancellation")
	}
	if !r.closed {
		t.Error("the reader was not closed; a leaked body is a slow fd leak on a long-lived daemon")
	}
}

type closeTrackingReader struct {
	blockFor time.Duration
	closed   bool
	reads    int
}

func (c *closeTrackingReader) Read(p []byte) (int, error) {
	c.reads++
	if c.reads > 1 {
		time.Sleep(c.blockFor)
		return 0, io.EOF
	}
	n := copy(p, []byte("data: one\n\n"))
	return n, nil
}

func (c *closeTrackingReader) Close() error { c.closed = true; return nil }

// An empty stream is not an error. A provider can legitimately answer nothing
// — an empty completion — and that is different from a failure.
func TestAnEmptyStreamIsNotAnError(t *testing.T) {
	got, err := collect(t, context.Background(), strings.NewReader(""))
	if err != nil {
		t.Fatalf("an empty stream reported an error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d frames from an empty stream", len(got))
	}
}

// A frame with an event and no data is legitimate and must be delivered: some
// providers signal state changes that way.
func TestAFrameWithAnEventAndNoDataIsDelivered(t *testing.T) {
	got, err := collect(t, context.Background(), strings.NewReader("event: ping\n\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Event != "ping" {
		t.Fatalf("got %+v, want one frame with Event=ping", got)
	}
}
