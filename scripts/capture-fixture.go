// Command capture-fixture records one real streaming response as a test
// fixture.
//
// # This program is the most likely place in the whole project to commit a
// credential
//
// It makes a real call, against a real endpoint, with a real key. The naive
// version of it writes request headers into the file and ships a key to the
// repository. So:
//
//   - **It never writes a request.** The response body is the fixture; the
//     request is not needed to replay one.
//   - It **redacts the body** before writing, because providers echo request
//     metadata in some error bodies.
//   - It reads credentials **only from the environment**, never from a file in
//     the tree — so there is no committed file for a key to end up in.
//
// `internal/model/fixtures_test.go` scans every fixture for credential shapes
// on every commit, which is the backstop for all of the above.
//
// # Usage
//
//	export AEON_CAPTURE_API_KEY=…            # never passed as a flag (C17)
//	export AEON_CAPTURE_MODEL=claude-sonnet-5
//	go run ./scripts/capture-fixture.go -provider anthropic -scenario text-only
//
// The credential is an environment variable rather than a flag because a value
// in argv is in the process list and in shell history, and that failure is
// silent.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

func main() {
	provider := flag.String("provider", "", "anthropic | openai")
	scenario := flag.String("scenario", "", "fixture name, e.g. text-only")
	prompt := flag.String("prompt", "Say hello in five words.", "the prompt to send")
	withTool := flag.Bool("tool", false, "offer a tool, to capture a tool call")
	withThinking := flag.Bool("thinking", false, "ask for extended thinking (anthropic)")
	outDir := flag.String("out", "internal/model/testdata", "where to write the fixture")
	flag.Parse()

	if err := run(*provider, *scenario, *prompt, *outDir, *withTool, *withThinking); err != nil {
		fmt.Fprintf(os.Stderr, "capture-fixture: %v\n", err)
		os.Exit(1)
	}
}

func run(provider, scenario, prompt, outDir string, withTool, withThinking bool) error {
	if provider == "" || scenario == "" {
		return fmt.Errorf("-provider and -scenario are both required")
	}

	key := os.Getenv("AEON_CAPTURE_API_KEY")
	if key == "" {
		return fmt.Errorf("AEON_CAPTURE_API_KEY is not set.\n" +
			"  It is an environment variable rather than a flag on purpose: a credential in " +
			"argv is in the process list and in shell history, and that failure is silent")
	}
	model := os.Getenv("AEON_CAPTURE_MODEL")
	if model == "" {
		return fmt.Errorf("AEON_CAPTURE_MODEL is not set")
	}

	req, err := buildRequest(provider, key, model, prompt, withTool, withThinking)
	if err != nil {
		return err
	}

	resp, err := (&http.Client{Timeout: 120 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("the call failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading the stream: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		// The error body is still written — a 400 from a provider is a useful
		// fixture — but it is the body most likely to echo a key, so the
		// redaction below matters most here.
		fmt.Fprintf(os.Stderr, "note: the provider answered %d; capturing the error body\n",
			resp.StatusCode)
	}

	redacted, hits := redact(body)
	if hits > 0 {
		fmt.Fprintf(os.Stderr, "redacted %d credential-shaped run(s) from the body\n", hits)
	}

	path := filepath.Join(outDir, provider, scenario+".sse")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, redacted, 0o644); err != nil {
		return err
	}

	fmt.Printf("wrote %s (%d bytes)\n", path, len(redacted))
	fmt.Printf("\nNOW DO THREE THINGS:\n")
	fmt.Printf("  1. Add an entry to %s with origin \"recorded\" and today's date.\n",
		filepath.Join(outDir, "manifest.json"))
	fmt.Printf("  2. Read the file. The automated redaction is a backstop, not a guarantee.\n")
	fmt.Printf("  3. Run: git diff --cached | grep -iE 'sk-|bearer |x-api-key'\n")
	return nil
}

// buildRequest constructs the provider's streaming request.
//
// Deliberately NOT reusing `internal/model`'s adapters: this script captures
// what a provider really sends, and building the request with the code under
// test would make a fixture that agrees with the adapter's assumptions. The
// duplication is the point.
func buildRequest(provider, key, model, prompt string, withTool, withThinking bool) (*http.Request, error) {
	tool := map[string]any{
		"name":        "get_weather",
		"description": "Look up the weather for a location.",
		"input_schema": map[string]any{
			"type":       "object",
			"properties": map[string]any{"location": map[string]any{"type": "string"}},
			"required":   []string{"location"},
		},
	}

	switch provider {
	case "anthropic":
		body := map[string]any{
			"model":      model,
			"max_tokens": 1024,
			"stream":     true,
			"messages":   []any{map[string]any{"role": "user", "content": prompt}},
		}
		if withTool {
			body["tools"] = []any{tool}
		}
		if withThinking {
			body["thinking"] = map[string]any{"type": "enabled", "budget_tokens": 1024}
		}
		req, err := jsonRequest("https://api.anthropic.com/v1/messages", body)
		if err != nil {
			return nil, err
		}
		req.Header.Set("x-api-key", key)
		req.Header.Set("anthropic-version", "2023-06-01")
		return req, nil

	case "openai":
		body := map[string]any{
			"model":    model,
			"stream":   true,
			"messages": []any{map[string]any{"role": "user", "content": prompt}},
			// Without this, OpenAI sends no usage at all — which is the
			// `usage-absent` fixture rather than a mistake.
			"stream_options": map[string]any{"include_usage": true},
		}
		if withTool {
			body["tools"] = []any{map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        tool["name"],
					"description": tool["description"],
					"parameters":  tool["input_schema"],
				},
			}}
		}
		req, err := jsonRequest("https://api.openai.com/v1/chat/completions", body)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+key)
		return req, nil

	default:
		return nil, fmt.Errorf("unknown provider %q; known: anthropic, openai", provider)
	}
}

func jsonRequest(url string, body map[string]any) (*http.Request, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	return req, nil
}

// credentialShapes are the patterns replaced on write.
//
// The same set `fixtures_test.go` scans for, deliberately: a shape this misses
// and the test catches is a commit that fails CI, which is the right failure.
// A shape both miss is a committed key, which is why the test's catch-all for
// long base64-ish runs exists.
var credentialShapes = []*regexp.Regexp{
	regexp.MustCompile(`sk-ant-[A-Za-z0-9_-]+`),
	regexp.MustCompile(`sk-[A-Za-z0-9_-]{16,}`),
	regexp.MustCompile(`(?i)(bearer)\s+[A-Za-z0-9._-]{8,}`),
	regexp.MustCompile(`(?i)(x-api-key|authorization)\s*[:=]\s*\S+`),
	regexp.MustCompile(`[A-Za-z0-9+/]{48,}={0,2}`),
}

// redact replaces every credential-shaped run, and reports how many it found.
func redact(body []byte) ([]byte, int) {
	out := body
	hits := 0
	for _, re := range credentialShapes {
		out = re.ReplaceAllFunc(out, func(match []byte) []byte {
			hits++
			// The field name is kept where there is one, so a reader can see
			// WHAT was redacted without seeing the value.
			if i := bytes.IndexAny(match, ":="); i >= 0 {
				return append(match[:i+1], []byte(" <redacted>")...)
			}
			if strings.HasPrefix(strings.ToLower(string(match)), "bearer") {
				return []byte("Bearer <redacted>")
			}
			return []byte("<redacted>")
		})
	}
	return out, hits
}
