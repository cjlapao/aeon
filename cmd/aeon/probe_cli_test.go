package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// S18 T3 at the CLI, end to end: `aeon routes probe` is a thin client of the
// socket (P34), the socket pays the real model layer exactly ONE request
// (D7), the verdict lands in the real store, and `routes list` prints it.
// The "provider" is a local SSE server — no test in the slice reaches a
// live provider (I7).

// sseServer answers /chat/completions with a recorded-shape OpenAI stream.
// `usage` decides whether the final usage chunk arrives; `refuseTools` makes
// it answer the probe's tool offer the way a tools-less server does — a 400
// naming them. `hits` counts requests, which is how the ONE-request rule is
// measured at this altitude.
func sseServer(t *testing.T, usage, refuseTools bool) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if refuseTools {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"error":{"message":"Function tools with reasoning_effort are not supported by this model"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		chunk := func(body string) { fmt.Fprintf(w, "data: %s\n\n", body) }
		chunk(`{"id":"probe-1","object":"chat.completion.chunk","created":1760000000,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"OK"},"finish_reason":null}]}`)
		chunk(`{"id":"probe-1","object":"chat.completion.chunk","created":1760000000,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`)
		if usage {
			chunk(`{"id":"probe-1","object":"chat.completion.chunk","created":1760000000,"model":"m","choices":[],"usage":{"prompt_tokens":9,"completion_tokens":1,"total_tokens":10}}`)
		}
		fmt.Fprint(w, "data:")
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// TestRoutesProbePrintsVerdictsAndSavesThem — the whole chain in one walk.
func TestRoutesProbePrintsVerdictsAndSavesThem(t *testing.T) {
	srv, hits := sseServer(t, true, false)
	d := startDaemon(t)

	d.mustRun("routes", "set", "--id", "honest", "--provider", "openai", "--model", "m",
		"--base-url", srv.URL, "--no-price",
		"--capabilities", "tools,streaming-usage,system-role")

	out := d.mustRun("routes", "probe", "honest")
	if n := hits.Load(); n != 1 {
		t.Errorf("the probe sent %d requests for three declared flags, want exactly 1 (D7, end to end)", n)
	}
	for _, want := range []string{"tools", "verified", "streaming-usage", "system-role", "declared", "saved"} {
		if !strings.Contains(out, want) {
			t.Errorf("the probe output omits %q:\n%s", want, out)
		}
	}
	// The earned verdict says WHEN and AGAINST WHICH MODEL (O2) — printed,
	// not merely carried.
	if !strings.Contains(out, "probed") || !strings.Contains(out, "against m") {
		t.Errorf("an earned verdict printed without its timestamp and model:\n%s", out)
	}

	// And the listing reads the cache, not the transport: no new request.
	before := hits.Load()
	list := d.mustRun("routes", "list")
	if n := hits.Load(); n != before {
		t.Errorf("listing routes paid for %d more requests", n-before)
	}
	if !strings.Contains(list, "tools(verified") || !strings.Contains(list, "system-role(declared") {
		t.Errorf("the listing does not print the cached three states:\n%s", list)
	}
}

// TestRoutesProbeContradictionIsPrintedAndBlocks — the CLI is where the
// operator LEARNS about a contradiction, and the same cache then refuses
// the ask: probe on Monday, refused on Friday, both from this one file.
func TestRoutesProbeContradictionIsPrintedAndBlocks(t *testing.T) {
	srv, hits := sseServer(t, false, true)
	d := startDaemon(t)

	d.mustRun("routes", "set", "--id", "wrong", "--provider", "openai", "--model", "m",
		"--base-url", srv.URL, "--no-price", "--capabilities", "tools,streaming-usage")

	out := d.mustRun("routes", "probe", "wrong")
	if !strings.Contains(out, "contradicted") || !strings.Contains(out, "Function tools") {
		t.Errorf("the provider's own refusal is not printed verbatim:\n%s", out)
	}
	// The stream died at a 400: streaming-usage was never observable — the
	// probe must NOT associate the blame.
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "streaming-usage") && !strings.Contains(line, "declared") {
			t.Errorf("blame by association for streaming-usage: %q", line)
		}
	}
	_ = hits

	_, err := d.run("ask", "--route", "wrong", "do the thing")
	if err == nil {
		t.Fatal("the ask went out on a route the probe contradicted")
	}
}

// TestRoutesProbeNothingDeclaredIsQuiet — zero requests, and it says so.
func TestRoutesProbeNothingDeclaredIsQuiet(t *testing.T) {
	srv, hits := sseServer(t, true, false)
	d := startDaemon(t)
	d.mustRun("routes", "set", "--id", "quiet", "--provider", "openai", "--model", "m",
		"--base-url", srv.URL, "--no-price")

	out := d.mustRun("routes", "probe", "quiet")
	if n := hits.Load(); n != 0 {
		t.Errorf("%d requests to probe an empty claim", n)
	}
	if !strings.Contains(strings.ToLower(out), "nothing") {
		t.Errorf("the quiet probe did not say it asked nothing:\n%s", out)
	}
}

// TestRoutesProbeNeedsARouteID — thin client, real argument check.
func TestRoutesProbeNeedsARouteID(t *testing.T) {
	d := startDaemon(t)
	if _, err := d.run("routes", "probe"); err == nil {
		t.Fatal("`routes probe` without an id was accepted")
	}
}
