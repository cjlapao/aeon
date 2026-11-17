// Command fixture-server replays a committed SSE fixture as if it were a
// provider.
//
// # Why this exists
//
// S3's demo shows `aeon ask` streaming a reply, and the slice must never
// contact a real provider from a test (I7) — nor should walking a demo cost
// money or require a key most readers will not have.
//
// So: a local server that answers `/v1/messages` and `/v1/chat/completions`
// with the bytes of a fixture. **The daemon, the protocol, the adapters, the
// SSE reader, the cost arithmetic and the CLI are all the real ones.** Only the
// provider is replaced, which is the one part a demo cannot honestly include.
//
//	go run ./scripts/fixture-server -fixture internal/model/testdata/anthropic/text-only.sse
//	AEON_ANTHROPIC_BASE_URL=http://127.0.0.1:8123/v1 ANTHROPIC_API_KEY=unused aeon serve
//	aeon ask --route anthropic "anything"
//
// # Several fixtures, one per request (S5)
//
// A STEP is a conversation: the model asks for a tool, gets a result, and
// answers. One fixture replayed for every request cannot show that — it shows
// the same turn until the iteration bound stops it, which is a demo of the
// bound rather than of the loop.
//
// So `-fixture` may be repeated, and each request takes the next one. The last
// one repeats, so a script that runs longer than expected degrades into
// repetition rather than into an error:
//
//	go run ./scripts/fixture-server \
//	  -fixture internal/model/testdata/anthropic/step-read-tool.sse \
//	  -fixture internal/model/testdata/anthropic/step-handoff.sse
//
// It is a demo and test aid, not part of the product: nothing in `internal/`
// imports it.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// fixtureList collects a repeatable -fixture flag.
type fixtureList []string

func (f *fixtureList) String() string { return strings.Join(*f, ",") }

func (f *fixtureList) Set(v string) error {
	*f = append(*f, v)
	return nil
}

func main() {
	var fixtures fixtureList
	flag.Var(&fixtures, "fixture", "path to a .sse fixture to replay; repeatable, one per request (required)")
	addr := flag.String("addr", "127.0.0.1:8123", "address to listen on")
	delay := flag.Duration("delay", 40*time.Millisecond, "pause between frames, so streaming is visible")
	status := flag.Int("status", 200, "status code to answer with, for exercising the retry path")
	retryAfter := flag.String("retry-after", "", "a Retry-After header value, for the retry path")
	flag.Parse()

	if len(fixtures) == 0 {
		fmt.Fprintln(os.Stderr, "fixture-server: -fixture is required")
		os.Exit(2)
	}
	bodies := make([]string, 0, len(fixtures))
	for _, path := range fixtures {
		body, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "fixture-server: %v\n", err)
			os.Exit(1)
		}
		bodies = append(bodies, string(body))
	}

	// Which fixture the next request gets. Guarded, because a step's requests
	// are serial but nothing stops two clients.
	var mu sync.Mutex
	var served int

	handler := func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		index := served
		served++
		mu.Unlock()
		if index >= len(bodies) {
			// The last fixture repeats. A script that runs longer than
			// expected then degrades into repetition rather than into a
			// provider error, which keeps the failure the demo's rather than
			// this aid's.
			index = len(bodies) - 1
		}
		body := bodies[index]

		log.Printf("%s %s  (fixture %d/%d: %s)", r.Method, r.URL.Path,
			index+1, len(bodies), fixtures[index])

		// **Never log a header.** This server is pointed at by a route that
		// carries a credential, and a request log is the most-copied text in
		// any bug report.
		if *retryAfter != "" {
			w.Header().Set("Retry-After", *retryAfter)
		}
		if *status != http.StatusOK {
			w.WriteHeader(*status)
			fmt.Fprintf(w, `{"error":{"message":"fixture-server was asked for %d"}}`, *status)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, canFlush := w.(http.Flusher)
		// Frame by frame, with a pause, so `aeon ask` visibly streams rather
		// than appearing to buffer — which is what D1 is about, and what a
		// single Write would hide.
		for _, frame := range strings.SplitAfter(body, "\n\n") {
			if frame == "" {
				continue
			}
			if _, err := fmt.Fprint(w, frame); err != nil {
				return
			}
			if canFlush {
				flusher.Flush()
			}
			time.Sleep(*delay)
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/messages", handler)
	mux.HandleFunc("/v1/chat/completions", handler)
	// `/v1/responses` is OpenAI's other API — a different request and stream
	// shape, same replay mechanism. Registered so a step on a `responses` route
	// can be walked with no credential.
	mux.HandleFunc("/v1/responses", handler)

	log.Printf("replaying %d fixture(s) on http://%s — one per request, the last repeating",
		len(bodies), *addr)
	server := &http.Server{Addr: *addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
