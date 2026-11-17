package model

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

// # The trace (test bench)
//
// One request, sent exactly once, with the wire on both sides kept: the JSON body the adapter built and the raw
// bytes the server answered. It exists to answer "what did we actually send, and what did the server actually
// say" when a model behaves oddly (thinking that never shows, a reasoning dial that is not honoured), which no
// structured reply can show because the adapter has already decided what to keep.
//
// Rules this file keeps:
//
//   - **No retry, no tool names rewrite beyond the adapter's own.** A trace that retried would hide the failure it
//     was asked to show.
//   - **The credential is never in the result.** Only the body, the URL without its query, and the response are
//     returned; the Authorization header is built by the adapter and read by nobody here.
//   - **Bounded.** The raw reply is capped; the cut is reported, never silent.

// TraceResult is everything one traced call saw.
type TraceResult struct {
	URL         string          `json:"url"`
	RequestBody json.RawMessage `json:"requestBody,omitempty"`
	// Status is the HTTP status, 0 when the request never got an answer.
	Status      int    `json:"status"`
	ContentType string `json:"contentType,omitempty"`
	// Raw is the response exactly as it arrived (an SSE stream is many `data:` lines), up to TraceRawCap bytes.
	Raw       string `json:"raw,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
	// Thinking and Text are what the ADAPTER read out of that stream: if Raw clearly carries reasoning and
	// ThinkingChars is 0, the adapter is not reading the field the server used.
	Thinking      string `json:"thinking,omitempty"`
	Text          string `json:"text,omitempty"`
	ThinkingChars int    `json:"thinkingChars"`
	TextChars     int    `json:"textChars"`
	ToolCalls     int    `json:"toolCalls"`
	StopReason    string `json:"stopReason,omitempty"`
	Usage         *Usage `json:"usage,omitempty"`
	DurationMs    int64  `json:"durationMs"`
	// Error is the failure in the call's own words (transport, or the non-200 body); the rest of the result is
	// still returned so the request that caused it can be read.
	Error string `json:"error,omitempty"`
}

const (
	// TraceRawCap bounds the raw reply kept.
	TraceRawCap = 256 << 10
	// TraceTimeout bounds the whole call.
	TraceTimeout = 120 * time.Second
	traceTextCap = 8 << 10
)

// Trace sends req once and reports the wire. It never returns an error: every failure is a field of the result.
func (c *Client) Trace(ctx context.Context, req ModelRequest) TraceResult {
	start := time.Now()
	out := TraceResult{}
	// Everything that leaves this function is scrubbed of the credential and of the full request URL (a transport
	// error quotes the URL with its query, and some providers carry the key there; a server can echo a key back).
	fullURL := ""
	scrub := func(text string) string {
		if fullURL != "" {
			text = strings.ReplaceAll(text, fullURL, out.URL)
		}
		if key := req.Route.APIKey; len(key) >= 4 {
			text = strings.ReplaceAll(text, key, "••••")
		}
		return text
	}
	finish := func() TraceResult {
		out.DurationMs = time.Since(start).Milliseconds()
		out.Error, out.Raw = scrub(out.Error), scrub(out.Raw)
		out.Thinking, out.Text = scrub(out.Thinking), scrub(out.Text)
		out.URL = scrub(out.URL)
		return out
	}

	adapter, err := AdapterFor(req.Route.Provider)
	if err != nil {
		out.Error = err.Error()
		return finish()
	}
	if err := req.Validate(); err != nil {
		out.Error = err.Error()
		return finish()
	}
	names := newWireNames(req)
	if names.changes() {
		req = names.encode(req)
	}
	httpReq, err := adapter.ToWire(req)
	if err != nil {
		out.Error = err.Error()
		return finish()
	}
	if httpReq.URL != nil {
		fullURL = httpReq.URL.String()
		u := *httpReq.URL
		u.RawQuery, u.User = "", nil
		out.URL = u.String()
	}
	if httpReq.Body != nil {
		body, rerr := io.ReadAll(httpReq.Body)
		_ = httpReq.Body.Close()
		if rerr != nil {
			out.Error = rerr.Error()
			return finish()
		}
		if json.Valid(body) {
			out.RequestBody = json.RawMessage(scrub(string(body)))
		}
		httpReq.Body = io.NopCloser(bytes.NewReader(body))
		httpReq.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
	}

	tctx, cancel := context.WithTimeout(ctx, TraceTimeout)
	defer cancel()
	resp, err := c.httpClient().Do(httpReq.WithContext(tctx))
	if err != nil {
		out.Error = err.Error()
		return finish()
	}
	defer resp.Body.Close()
	out.Status = resp.StatusCode
	out.ContentType = resp.Header.Get("Content-Type")

	raw, rerr := io.ReadAll(io.LimitReader(resp.Body, TraceRawCap+1))
	if len(raw) > TraceRawCap {
		raw, out.Truncated = raw[:TraceRawCap], true
	}
	out.Raw = string(raw)
	if rerr != nil {
		out.Error = rerr.Error()
	}
	if resp.StatusCode != http.StatusOK {
		if out.Error == "" {
			out.Error = http.StatusText(resp.StatusCode)
		}
		return finish()
	}

	// What the adapter makes of the same bytes: the comparison is the point.
	frames, _ := ReadSSE(tctx, bytes.NewReader(raw))
	var thinking, text strings.Builder
	for d := range adapter.FromWire(tctx, frames) {
		d = names.decode(d)
		switch d.Kind {
		case DeltaThinking:
			out.ThinkingChars += len(d.Text)
			if thinking.Len() < traceTextCap {
				thinking.WriteString(d.Text)
			}
		case DeltaText:
			out.TextChars += len(d.Text)
			if text.Len() < traceTextCap {
				text.WriteString(d.Text)
			}
		case DeltaToolUse:
			out.ToolCalls++
		}
	}
	out.Thinking, out.Text = thinking.String(), text.String()
	usage, stop, _ := adapter.Finalise()
	out.StopReason = stop
	if !usage.Empty() {
		out.Usage = &usage
	}
	return finish()
}
