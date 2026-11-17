package main

import (
	"strings"
	"testing"
)

// The redaction is a backstop for the one mistake that cannot be undone by a
// later commit. It is proven against credential SHAPES — none of these is a
// real key — because the alternative is finding out in a public repository.
func TestRedactRemovesCredentialShapes(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		// mustNotContain is the part that would be the secret.
		mustNotContain string
	}{
		{
			"an OpenAI-style key echoed in an error body",
			`data: {"error":{"message":"Incorrect API key provided: sk-proj-AAAAAAAAAAAAAAAAAAAA"}}`,
			"sk-proj-AAAAAAAAAAAAAAAAAAAA",
		},
		{
			"an Anthropic-style key",
			`data: {"error":{"message":"invalid x-api-key: sk-ant-api03-BBBBBBBBBBBBBBBB"}}`,
			"sk-ant-api03-BBBBBBBBBBBBBBBB",
		},
		{
			"a bearer token in an echoed header",
			`data: {"request":{"authorization":"Bearer abcdefghijklmnopqrstuvwxyz"}}`,
			"abcdefghijklmnopqrstuvwxyz",
		},
		{
			"an x-api-key header line",
			"x-api-key: sk-ant-api03-CCCCCCCCCCCCCCCC\n",
			"sk-ant-api03-CCCCCCCCCCCCCCCC",
		},
		{
			"a long base64-ish run with no recognisable prefix",
			`data: {"token":"QUJDREVGR0hJSktMTU5PUFFSU1RVVldYWVphYmNkZWZnaGlqa2xtbm9w"}`,
			"QUJDREVGR0hJSktMTU5PUFFSU1RVVldYWVphYmNkZWZnaGlqa2xtbm9w",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, hits := redact([]byte(tc.in))
			if hits == 0 {
				t.Fatalf("nothing was redacted from %q", tc.in)
			}
			if strings.Contains(string(out), tc.mustNotContain) {
				t.Fatalf("the credential survived redaction:\n  %s", out)
			}
			if !strings.Contains(string(out), "redacted") {
				t.Errorf("the output does not say something was redacted:\n  %s", out)
			}
		})
	}
}

// Redaction must not destroy the fixture. A stream body is mostly JSON that has
// nothing to do with credentials, and a scanner that mangles it produces a
// fixture that fails for the wrong reason.
func TestRedactLeavesOrdinaryStreamContentAlone(t *testing.T) {
	const body = `event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello, world"}}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}
`
	out, hits := redact([]byte(body))
	if hits != 0 {
		t.Errorf("redacted %d run(s) from an ordinary body:\n%s", hits, out)
	}
	if string(out) != body {
		t.Errorf("an ordinary body was modified:\n  got  %s\n  want %s", out, body)
	}
}

// The script builds a provider request WITHOUT reusing internal/model's
// adapters, deliberately — a fixture built with the code under test would agree
// with that code's assumptions. This asserts the two headers that carry the
// credential are set, so a capture actually authenticates.
func TestBuildRequestSetsTheProviderCredentialHeader(t *testing.T) {
	for _, tc := range []struct {
		provider string
		header   string
	}{
		{"anthropic", "x-api-key"},
		{"openai", "Authorization"},
	} {
		t.Run(tc.provider, func(t *testing.T) {
			req, err := buildRequest(tc.provider, "KEY-PLACEHOLDER", "some-model", "hi", false, false)
			if err != nil {
				t.Fatal(err)
			}
			if req.Header.Get(tc.header) == "" {
				t.Errorf("%s: no %s header, so a capture would not authenticate", tc.provider, tc.header)
			}
			if req.Header.Get("Accept") != "text/event-stream" {
				t.Errorf("%s: Accept is %q, want text/event-stream", tc.provider, req.Header.Get("Accept"))
			}
		})
	}
}

func TestBuildRequestRefusesAnUnknownProvider(t *testing.T) {
	if _, err := buildRequest("gemini", "k", "m", "hi", false, false); err == nil {
		t.Fatal("built a request for an unknown provider")
	}
}

// OpenAI sends no usage unless asked. The capture script must ask, or every
// OpenAI fixture would be a usage-absent one and `known` cost would be
// untestable.
func TestTheOpenAIRequestAsksForUsage(t *testing.T) {
	req, err := buildRequest("openai", "k", "m", "hi", false, false)
	if err != nil {
		t.Fatal(err)
	}
	body := make([]byte, req.ContentLength)
	if _, err := req.Body.Read(body); err != nil && err.Error() != "EOF" {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "include_usage") {
		t.Errorf("the OpenAI request does not set stream_options.include_usage, so no capture "+
			"would ever contain usage:\n%s", body)
	}
}
