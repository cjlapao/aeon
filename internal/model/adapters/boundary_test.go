package adapters

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// D14 / I2 — **no provider shape escapes this directory.**
//
// Asserted rather than intended. The canonical format exists so that nothing
// above `internal/model` has ever seen a provider's JSON; a field name leaking
// upward is how that erodes — one `choices[0]` in a runtime, and the runtime
// now has an opinion about OpenAI.
//
// # What this checks, and the one exception
//
// Non-test source files outside `adapters/`. Two reasons it is scoped that way,
// both learned the hard way in S2:
//
//  1. `fixtures_test.go` must contain `x-api-key` — it is the test that scans
//     fixtures FOR that header, and a credential scanner that cannot name what
//     it looks for is not one.
//  2. A check needing a growing list of exceptions gets weakened until it
//     matches nothing, and then the invariant is unguarded and nobody knows.
//
// A provider shape in a *test* outside `adapters/` is still worth questioning —
// `sse_test.go` had Anthropic event names in its sample stream, and removing
// them made the test better, because that reader genuinely does not know what a
// provider is.
var providerShapes = []string{
	// OpenAI
	"choices", "tool_calls", "stream_options", "max_completion_tokens",
	"prompt_tokens", "completion_tokens", "chat/completions",
	// Anthropic
	"content_block", "input_schema", "anthropic-version", "input_json_delta",
	"message_start", "message_delta", "x-api-key", "cache_creation_input_tokens",
}

func TestNoProviderShapeEscapesTheAdapters(t *testing.T) {
	root := filepath.Join("..", "..", "..")

	var offences []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // an unreadable path is not this test's business
		}
		if info.IsDir() {
			switch info.Name() {
			case "node_modules", "target", "dist", ".git", "testdata", "adapters":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		// `scripts/` is exempt, as a directory rather than as a list of files.
		//
		// # Why a directory and not another filename
		//
		// Two scripts legitimately speak a provider's wire, and for opposite
		// reasons: `capture-fixture` records what a provider really sends (and
		// building its request with the adapters would make fixtures that agree
		// with the code under test), and `fixture-server` impersonates a
		// provider so a demo can be walked without a credential.
		//
		// The first version of this check exempted one filename, and the second
		// script immediately failed it. **A check that grows a filename list
		// gets weakened until it matches nothing** — which is the lesson from
		// S2's grep-based guards. So the exemption is the directory, whose role
		// is "tools that talk to or pretend to be a provider", and
		// `TestNothingImportsScripts` below stops that being a loophole.
		if strings.HasPrefix(path, filepath.Join(root, "scripts")+string(filepath.Separator)) {
			return nil
		}

		src, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		// Comments may discuss a provider's shape — explaining why something is
		// forbidden is the opposite of a violation — so this reads tokens.
		code := codeTokens(t, path, src)
		for _, shape := range providerShapes {
			if strings.Contains(code, shape) {
				rel, _ := filepath.Rel(root, path)
				offences = append(offences, rel+" contains "+shape)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, o := range offences {
		t.Errorf("%s — a provider's field name outside internal/model/adapters/. The canonical "+
			"format exists so nothing above it has seen a provider's JSON (D14)", o)
	}
}

// codeTokens returns a file's content with comments excluded.
//
// Comments may discuss a provider's shape — explaining why something is
// forbidden is the opposite of a violation — so the check must not read them.
func codeTokens(t *testing.T, path string, src []byte) string {
	t.Helper()

	var out strings.Builder
	inBlockComment := false
	for _, line := range strings.Split(string(src), "\n") {
		trimmed := strings.TrimSpace(line)

		if inBlockComment {
			if strings.Contains(trimmed, "*/") {
				inBlockComment = false
			}
			continue
		}
		if strings.HasPrefix(trimmed, "/*") {
			if !strings.Contains(trimmed, "*/") {
				inBlockComment = true
			}
			continue
		}
		if strings.HasPrefix(trimmed, "//") {
			continue
		}
		// An inline comment: keep what precedes it, unless the `//` is inside a
		// string literal — a URL, say.
		if i := strings.Index(line, "//"); i > 0 && strings.Count(line[:i], `"`)%2 == 0 {
			line = line[:i]
		}
		out.WriteString(line)
		out.WriteByte('\n')
	}
	return out.String()
}

// The exemption above is only safe while `scripts/` stays outside the product.
//
// If anything under `internal/` or `cmd/` imported a script package, a provider
// shape could reach the product through it — and D14's check would wave it
// through.
func TestNothingImportsScripts(t *testing.T) {
	root := filepath.Join("..", "..", "..")

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			switch info.Name() {
			case "node_modules", "target", "dist", ".git", "testdata", "scripts":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		// This file names the path in its own failure message, so it flags
		// itself — which it did on the first run. The same self-reference
		// `providerShapes` has, and the same exclusion.
		if strings.HasSuffix(path, "boundary_test.go") {
			return nil
		}
		src, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		if strings.Contains(codeTokens(t, path, src), "aeon/scripts") {
			rel, _ := filepath.Rel(root, path)
			t.Errorf("%s imports a package under scripts/. Those are exempt from the "+
				"provider-shape check because they talk to or impersonate providers — so "+
				"importing one would let a provider shape into the product through the back "+
				"door", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
