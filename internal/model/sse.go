package model

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
)

// # Server-sent events, parsed in-house (M19, superseding M7)
//
// The TypeScript slice took a dependency on `eventsource-parser` for a good
// reason: in JavaScript a `TextDecoder` carries state across chunk boundaries,
// and getting that wrong truncates a tool call. **That hazard is a JavaScript
// problem.**
//
// Here the reader accumulates `[]byte` and converts to `string` only when a
// complete frame is in hand, so a multi-byte rune split across two TCP reads
// cannot be mangled — there is no incremental decoder to carry state. What
// remains is a blank-line-delimited `field: value` format, roughly a hundred
// lines, which a dependency would not save us from testing exhaustively anyway.
//
// **Keep the test, not the library.** Offset independence is the property that
// made the dependency worth having, and it is cheap to assert directly: replay
// a fixture chunked at every byte offset and compare against the unchunked
// parse.
//
// # One reader, both providers (I3)
//
// A second framing implementation diverges, only one gets tested, and in a
// stream parser divergence means one path truncates — silently, on long tool
// arguments, under load.

// Frame is one SSE event. `Event` is empty when the stream omits the field,
// which Anthropic does not and OpenAI does.
type Frame struct {
	Event string
	Data  string
}

// doneSentinel ends an OpenAI stream. **Anthropic never sends it**, so its
// absence must end the stream cleanly rather than looking like a truncation.
const doneSentinel = "[DONE]"

// readChunk is the read size. Small enough that a slow stream delivers deltas
// promptly rather than in buffered clumps, which is what D1 asserts.
const readChunk = 4096

// ReadSSE parses frames from r until it ends.
//
// # Why two channels rather than one of (Frame, error) pairs
//
// The error channel is how a **truncated** stream is distinguished from a clean
// end — and that distinction is the difference between reporting a completed
// call and reporting a failed one. A single channel of pairs makes every
// consumer branch on a nil error at every receive, and the one place somebody
// forgets is the place a truncation is reported as success.
//
// Both channels are closed when parsing ends. The error channel carries at most
// one value.
//
// If r is an io.Closer it is closed when parsing ends — a leaked HTTP body on a
// daemon meant to run for weeks is a slow file-descriptor leak.
func ReadSSE(ctx context.Context, r io.Reader) (<-chan Frame, <-chan error) {
	frames := make(chan Frame)
	errs := make(chan error, 1)

	go func() {
		defer close(frames)
		defer close(errs)
		if c, ok := r.(io.Closer); ok {
			defer c.Close()
		}

		var buf []byte
		chunk := make([]byte, readChunk)

		for {
			// Checked between reads rather than only between frames: a stream
			// that has gone quiet must still be cancellable, and a provider
			// holding a connection open with no data is ordinary.
			if err := ctx.Err(); err != nil {
				errs <- err
				return
			}

			n, readErr := r.Read(chunk)
			if n > 0 {
				buf = append(buf, chunk[:n]...)
				var done bool
				buf, done = drain(ctx, buf, frames, errs)
				if done {
					return
				}
			}

			if readErr == nil {
				continue
			}
			if !errors.Is(readErr, io.EOF) {
				// A truncated stream reported as an error, never as a clean
				// end. The alternative is a completed call that lost its tail.
				errs <- readErr
				return
			}

			// EOF. A final frame with no trailing blank line must still be
			// delivered — some servers close without one.
			if len(bytes.TrimSpace(buf)) > 0 {
				frame, ok, isDone := parseFrame(buf)
				if !isDone && ok {
					select {
					case frames <- frame:
					case <-ctx.Done():
						errs <- ctx.Err()
					}
				}
			}
			return
		}
	}()

	return frames, errs
}

// drain emits every complete frame in buf and returns what is left.
//
// The bool reports that the stream is finished — the `[DONE]` sentinel, or a
// cancelled context.
func drain(ctx context.Context, buf []byte, frames chan<- Frame, errs chan<- error) ([]byte, bool) {
	for {
		idx, sepLen := findSeparator(buf)
		if idx < 0 {
			return buf, false
		}
		raw := buf[:idx]
		buf = buf[idx+sepLen:]

		frame, ok, isDone := parseFrame(raw)
		if isDone {
			return buf, true
		}
		if !ok {
			continue // a frame of only comments or ignored fields
		}
		select {
		case frames <- frame:
		case <-ctx.Done():
			errs <- ctx.Err()
			return buf, true
		}
	}
}

// findSeparator returns the earliest frame separator and its length.
//
// **Both `\n\n` and `\r\n\r\n`** (and the mixed forms). A reader that handles
// only the former works against one provider and truncates against a proxy or
// a load balancer that normalises line endings — which is a failure that
// appears only in somebody else's deployment.
func findSeparator(buf []byte) (index, length int) {
	best, bestLen := -1, 0
	for _, sep := range [][]byte{
		[]byte("\r\n\r\n"),
		[]byte("\n\n"),
		[]byte("\r\r"),
		[]byte("\n\r\n"),
		[]byte("\r\n\n"),
	} {
		if i := bytes.Index(buf, sep); i >= 0 {
			// The earliest wins; on a tie the LONGEST wins, so `\r\n\r\n` is
			// not parsed as `\n\r\n` leaving a stray `\n` to start the next
			// frame with an empty line.
			if best < 0 || i < best || (i == best && len(sep) > bestLen) {
				best, bestLen = i, len(sep)
			}
		}
	}
	return best, bestLen
}

// parseFrame turns one frame's bytes into a Frame.
//
// Returns ok=false for a frame carrying nothing a consumer needs — a comment
// line, a lone `retry:` — and done=true for the `[DONE]` sentinel.
func parseFrame(raw []byte) (frame Frame, ok bool, done bool) {
	// The ONE conversion to string, with a whole frame in hand. Everything
	// before this point is bytes, which is what makes a rune split across
	// chunks a non-event (M19).
	text := string(raw)

	var data []string
	var event string
	sawField := false

	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" {
			continue
		}
		// A line beginning with a colon is a comment. Providers send them as
		// keep-alives, and treating one as data yields an empty delta.
		if strings.HasPrefix(line, ":") {
			continue
		}

		field, value := line, ""
		if i := strings.IndexByte(line, ':'); i >= 0 {
			field = line[:i]
			value = line[i+1:]
			// Exactly one leading space is part of the framing, not the value.
			// Trimming all whitespace would corrupt data that begins with one.
			value = strings.TrimPrefix(value, " ")
		}

		switch field {
		case "data":
			data = append(data, value)
			sawField = true
		case "event":
			event = value
			sawField = true
		case "id", "retry":
			// Carried by the format, unused here. Ignored explicitly so a
			// reader of this switch knows they were considered.
		default:
			// An unknown field is ignored, per the SSE specification.
		}
	}

	if !sawField {
		return Frame{}, false, false
	}

	// Multiple `data:` lines in one frame join with a newline — the
	// specification's rule, and OpenAI relies on it for multi-line content.
	joined := strings.Join(data, "\n")
	if strings.TrimSpace(joined) == doneSentinel {
		return Frame{}, false, true
	}
	if joined == "" && event == "" {
		return Frame{}, false, false
	}
	return Frame{Event: event, Data: joined}, true, false
}
