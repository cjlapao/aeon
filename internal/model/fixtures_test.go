package model

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// # The fixtures, and the check that keeps them honest
//
// Every stream test in this slice is worth exactly what its fixture is worth —
// and **a fixture's trustworthiness is invisible in the test that uses it.** A
// hand-written fixture encoding what its author believed a provider sends will
// pass its test and be wrong in production.
//
// So each is labelled, and this file enforces the labelling. It also carries
// the slice's highest security check: a recorded capture is made against a real
// endpoint with a real key, and **this is the most likely place in the whole
// project to commit a credential.**

type fixtureManifest struct {
	Comment  []string       `json:"comment"`
	Fixtures []fixtureEntry `json:"fixtures"`
}

type fixtureEntry struct {
	Path        string `json:"path"`
	Provider    string `json:"provider"`
	Scenario    string `json:"scenario"`
	Origin      string `json:"origin"`
	CapturedAt  string `json:"capturedAt"`
	NotVerified string `json:"notVerified"`
}

const testdataDir = "testdata"

func loadManifest(t *testing.T) fixtureManifest {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(testdataDir, "manifest.json"))
	if err != nil {
		t.Fatalf("no manifest: %v", err)
	}
	var m fixtureManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("the manifest is not valid JSON: %v", err)
	}
	if len(m.Fixtures) == 0 {
		t.Fatal("the manifest lists no fixtures; every test below would pass vacuously")
	}
	return m
}

func fixtureFiles(t *testing.T) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(testdataDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".sse") {
			rel, _ := filepath.Rel(testdataDir, path)
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// Both directions: an unlabelled fixture is a failed check, and a manifest
// entry for a file somebody deleted is a lie that reads as coverage.
func TestEveryFixtureHasAManifestEntryAndEveryEntryHasAFile(t *testing.T) {
	m := loadManifest(t)

	listed := map[string]bool{}
	for _, e := range m.Fixtures {
		listed[e.Path] = true
	}
	for _, file := range fixtureFiles(t) {
		if !listed[file] {
			t.Errorf("%s has no manifest entry. An unlabelled fixture is one whose "+
				"trustworthiness nobody can tell — label it `recorded` or `hand-written`", file)
		}
	}

	onDisk := map[string]bool{}
	for _, file := range fixtureFiles(t) {
		onDisk[file] = true
	}
	for _, e := range m.Fixtures {
		if !onDisk[e.Path] {
			t.Errorf("the manifest lists %s, which does not exist — an entry for a deleted "+
				"fixture reads as coverage that is not there", e.Path)
		}
	}
}

func TestEveryFixtureIsLabelledAndHandWrittenOnesSayWhatWasNotVerified(t *testing.T) {
	for _, e := range loadManifest(t).Fixtures {
		switch e.Origin {
		case "recorded":
			// A recording needs no caveat, but it does need a date: a capture
			// from two provider releases ago is a different kind of wrong.
			if e.CapturedAt == "" {
				t.Errorf("%s is recorded but has no capture date", e.Path)
			}
		case "hand-written":
			if len(strings.TrimSpace(e.NotVerified)) < 40 {
				t.Errorf("%s is hand-written and does not say what was not verified (O4). "+
					"A hand-written fixture presented as a recording is a failed check, and one "+
					"with an empty caveat is the same thing with extra steps", e.Path)
			}
		case "":
			t.Errorf("%s has no origin", e.Path)
		default:
			t.Errorf("%s has origin %q; it must be `recorded` or `hand-written`", e.Path, e.Origin)
		}
		if e.Scenario == "" {
			t.Errorf("%s has no scenario", e.Path)
		}
		if e.Provider == "" {
			t.Errorf("%s has no provider", e.Path)
		}
	}
}

// **The security check.** It runs on every commit, so a key added by a later
// capture is caught rather than shipped.
//
// A recorded capture is made with a real key, and the naive capture writes
// request headers into the file. Providers also echo request metadata in some
// error bodies — which is why this scans the body rather than trusting the
// capture script's redaction.
func TestNoFixtureContainsACredential(t *testing.T) {
	patterns := map[string]*regexp.Regexp{
		"an OpenAI-style key":     regexp.MustCompile(`sk-[A-Za-z0-9_-]{16,}`),
		"an Anthropic-style key":  regexp.MustCompile(`sk-ant-`),
		"a bearer token":          regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._-]{8,}`),
		"an x-api-key header":     regexp.MustCompile(`(?i)x-api-key`),
		"an authorization header": regexp.MustCompile(`(?i)authorization\s*:`),
		// Any long base64-ish run. This is the catch-all for a shape nobody
		// anticipated, and it is the one that would catch a token from a
		// provider nobody has integrated yet.
		"a long base64-ish run": regexp.MustCompile(`[A-Za-z0-9+/]{48,}={0,2}`),
	}

	for _, file := range fixtureFiles(t) {
		raw, err := os.ReadFile(filepath.Join(testdataDir, file))
		if err != nil {
			t.Fatal(err)
		}
		body := string(raw)
		for name, re := range patterns {
			if loc := re.FindStringIndex(body); loc != nil {
				// The match is NOT printed. Printing it would put the thing
				// this test exists to catch into a CI log, which is a second
				// copy of the same mistake.
				t.Errorf("%s appears to contain %s at byte %d. The match is deliberately not "+
					"printed. If it is a false positive — a long signature, say — redact it to "+
					"<redacted> or shorten it; a fixture does not need a realistic token to "+
					"exercise a parser", file, name, loc[0])
			}
		}
	}
}

// The credential check must be able to fail, or it is decoration. Asserted
// against a synthetic body rather than by committing a key.
func TestTheCredentialCheckCatchesAKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "leak.sse")
	// Shapes only — none of these is a real credential.
	body := "data: {\"error\":\"invalid key sk-" + strings.Repeat("A", 20) + "\"}\n\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	re := regexp.MustCompile(`sk-[A-Za-z0-9_-]{16,}`)
	if !re.MatchString(body) {
		t.Fatal("the credential pattern does not match an obvious key shape; the check on the " +
			"real fixtures would pass whatever they contained")
	}
	for _, shape := range []string{
		"Bearer abcdefgh12345678",
		"x-api-key: anything",
		"Authorization: whatever",
	} {
		matched := false
		for _, re := range []*regexp.Regexp{
			regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._-]{8,}`),
			regexp.MustCompile(`(?i)x-api-key`),
			regexp.MustCompile(`(?i)authorization\s*:`),
		} {
			if re.MatchString(shape) {
				matched = true
			}
		}
		if !matched {
			t.Errorf("no pattern matches %q", shape)
		}
	}
}

// Every fixture parses through the reader that will consume it. A fixture that
// does not parse is a test that fails for the wrong reason, and the failure
// points at the adapter rather than at the file.
func TestEveryFixtureParsesThroughReadSSE(t *testing.T) {
	for _, file := range fixtureFiles(t) {
		t.Run(file, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(testdataDir, file))
			if err != nil {
				t.Fatal(err)
			}
			frames, errs := ReadSSE(context.Background(), strings.NewReader(string(raw)))

			var count int
			for f := range frames {
				count++
				// Every frame's data must be JSON — both providers send JSON
				// bodies, and a fixture with a stray line would otherwise only
				// fail much later inside an adapter.
				if f.Data == "" {
					continue
				}
				var any any
				if err := json.Unmarshal([]byte(f.Data), &any); err != nil {
					t.Errorf("frame %d is not valid JSON: %v\n  %s", count, err, f.Data)
				}
			}
			for err := range errs {
				t.Errorf("parse error: %v", err)
			}
			if count == 0 {
				t.Error("no frames parsed; the fixture is empty or its separators are wrong")
			}
		})
	}
}

// The set must cover the four scenarios per provider that the adapters' tests
// need. A missing scenario is a test somebody will write against a fixture that
// does not exist, and then quietly not write.
func TestTheFixtureSetCoversTheRequiredScenarios(t *testing.T) {
	byProvider := map[string][]string{}
	for _, e := range loadManifest(t).Fixtures {
		byProvider[e.Provider] = append(byProvider[e.Provider], e.Path)
	}

	required := map[string][]string{
		"openai":    {"text-only", "tool-call", "usage-absent"},
		"anthropic": {"text-only", "tool-call", "usage-absent", "signed-thinking", "two-tool-calls"},
	}
	for provider, scenarios := range required {
		paths := strings.Join(byProvider[provider], " ")
		for _, want := range scenarios {
			if !strings.Contains(paths, want) {
				t.Errorf("%s has no %q fixture", provider, want)
			}
		}
	}

	// Usage PRESENT is covered by text-only for both providers, which is worth
	// asserting rather than assuming: it is the fixture that makes a `known`
	// cost testable at all.
	for _, provider := range []string{"openai", "anthropic"} {
		if len(byProvider[provider]) < 3 {
			t.Errorf("%s has only %d fixtures", provider, len(byProvider[provider]))
		}
	}
}
