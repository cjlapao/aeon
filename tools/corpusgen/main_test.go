package main

import (
	"crypto/sha256"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// writeFakeAgents builds the corpus source tree the generator reads, at
// <tmp>/repo/opencode/agents so the discovered-source shape matches
// production. It carries the cases the real corpus actually contains:
// an agent with no permission block, one with permission but no bash, one
// whose strict YAML parse fails (an unquoted description containing ": ")
// yet whose permission block is well formed and carries an ask rule, one
// carrying mid-star/exact/conflicting patterns, and one authoring an empty
// bash mapping.
func writeFakeAgents(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "repo", "opencode", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"plain.md": `---
name: Plain
description: an agent with no permission block at all
mode: subagent
---
# body
`,
		"no-bash.md": `---
name: NoBash
permission:
  edit: allow
---
# body
`,
		"ordered.md": `---
name: Ordered
description: primary agent: keeps authored order and carries an ask rule
mode: primary
permission:
  bash:
    "*": allow
    "git reset --hard*": ask
    "git clean*": deny        # the comment must not disturb the pair
    "gh pr merge*": allow
---
# body
`,
		"props.md": `---
name: Props
permission:
  bash:
    "rm * -rf": deny
    "git status": allow
    "git clean*": allow
---
# body
`,
		"empty-bash.md": `---
name: EmptyBash
permission:
  bash: {}
---
# body
`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

type treeEntry struct {
	Path string
	Mode fs.FileMode
	Size int64
	Sum  [sha256.Size]byte
}

// snapshotTree records the sorted path list plus per-file mode, size and
// content hash — so a created file is caught, not just a mutated one.
func snapshotTree(t *testing.T, dir string) []treeEntry {
	t.Helper()
	var entries []treeEntry
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			entries = append(entries, treeEntry{Path: path, Mode: d.Type()})
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		entries = append(entries, treeEntry{Path: path, Mode: info.Mode(), Size: info.Size(), Sum: sha256.Sum256(raw)})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func TestBashRules(t *testing.T) {
	tests := []struct {
		name    string
		front   string
		want    []rule
		wantErr string // substring; empty means expect success
	}{
		{
			name:  "no front matter content",
			front: "",
			want:  nil,
		},
		{
			name:  "permission absent",
			front: "name: x\ndescription: y\n",
			want:  nil,
		},
		{
			name:  "permission without bash",
			front: "permission:\n  edit: allow\n",
			want:  nil,
		},
		{
			name:  "authored order survives, ask accepted",
			front: "permission:\n  bash:\n    \"*\": allow\n    \"git reset --hard*\": ask\n    \"git clean*\": deny\n    \"gh pr merge*\": allow\n",
			want: []rule{
				{Pattern: "*", Verdict: "allow"},
				{Pattern: "git reset --hard*", Verdict: "ask"},
				{Pattern: "git clean*", Verdict: "deny"},
				{Pattern: "gh pr merge*", Verdict: "allow"},
			},
		},
		{
			name:  "empty authored mapping emits empty not omitted",
			front: "permission:\n  bash: {}\n",
			want:  []rule{},
		},
		{
			name:  "empty authored block (null) emits empty not omitted",
			front: "permission:\n  bash:\n",
			want:  []rule{},
		},
		{
			name:    "unknown verdict refused",
			front:   "permission:\n  bash:\n    \"ls*\": allowd\n",
			wantErr: `unknown verdict "allowd"`,
		},
		{
			name:    "non-string pattern refused",
			front:   "permission:\n  bash:\n    1: allow\n",
			wantErr: "non-string pattern",
		},
		{
			name:    "non-string verdict refused",
			front:   "permission:\n  bash:\n    \"ls*\": true\n",
			wantErr: "non-string verdict",
		},
		{
			name:    "sequence verdict refused",
			front:   "permission:\n  bash:\n    \"ls*\":\n      - deny\n",
			wantErr: "non-string verdict",
		},
		{
			name:    "scalar bash refused",
			front:   "permission:\n  bash: false\n",
			wantErr: "must be a mapping",
		},
		{
			name:    "malformed permission block refused",
			front:   "name: x\npermission:\n  bash:\n    \"a\": \"b\": c\n",
			wantErr: "malformed permission block",
		},
		{
			name:  "recovered: broken strict parse keeps well-formed rules",
			front: "name: Ordered\ndescription: primary agent: a plain scalar strict YAML rejects\npermission:\n  bash:\n    \"rm -rf*\": deny\n    \"git status\": allow\n",
			want: []rule{
				{Pattern: "rm -rf*", Verdict: "deny"},
				{Pattern: "git status", Verdict: "allow"},
			},
		},
		{
			name:  "recovered: no permission block means nothing broken",
			front: "description: broken: strictly unparsable, and no permission follows\n",
			want:  nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := bashRules(tc.front)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want error containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("rules = %+v, want %+v", got, tc.want)
			}
			if got != nil && tc.want != nil && len(tc.want) > 0 {
				for i := range got {
					if got[i] != tc.want[i] {
						t.Fatalf("authored order not preserved at %d: %+v vs %+v", i, got[i], tc.want[i])
					}
				}
			}
		})
	}
}

func TestGenerateCorpus(t *testing.T) {
	dir := writeFakeAgents(t)
	out := filepath.Join(t.TempDir(), "corpus.json")
	data, err := generate(dir, out)
	if err != nil {
		t.Fatal(err)
	}
	roundTrip, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(data, roundTrip) {
		t.Error("returned bytes differ from written file")
	}
	var doc corpusDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}

	if doc.GeneratedBy != generatedBy {
		t.Errorf("generated_by = %q, want %q", doc.GeneratedBy, generatedBy)
	}
	if doc.Source != "opencode/agents" {
		t.Errorf("source = %q, want repo-relative %q", doc.Source, "opencode/agents")
	}

	// Agents sorted by name; plain.md and no-bash.md omitted; empty-bash kept
	// with an empty (authored-but-empty) rules array.
	wantAgents := []string{"empty-bash", "ordered", "props"}
	var gotAgents []string
	for _, a := range doc.Agents {
		gotAgents = append(gotAgents, a.Agent)
	}
	if !reflect.DeepEqual(gotAgents, wantAgents) {
		t.Fatalf("agents = %v, want %v", gotAgents, wantAgents)
	}

	byName := map[string]corpusAgent{}
	for _, a := range doc.Agents {
		byName[a.Agent] = a
	}
	wantOrdered := []rule{
		{Pattern: "*", Verdict: "allow"},
		{Pattern: "git reset --hard*", Verdict: "ask"},
		{Pattern: "git clean*", Verdict: "deny"},
		{Pattern: "gh pr merge*", Verdict: "allow"},
	}
	if !reflect.DeepEqual(byName["ordered"].Rules, wantOrdered) {
		t.Errorf("ordered rules = %+v, want authored order %+v", byName["ordered"].Rules, wantOrdered)
	}
	if byName["empty-bash"].Rules == nil || len(byName["empty-bash"].Rules) != 0 {
		t.Errorf("empty-bash rules = %+v, want authored-empty []", byName["empty-bash"].Rules)
	}
	if !strings.Contains(string(data), `"rules": []`) {
		t.Error("authored-empty agent did not render as an empty JSON array")
	}

	// Properties: '*' is the only metacharacter present; "rm * -rf" carries a
	// mid-star pattern; "git status" is exact; "git clean*" is deny under
	// ordered and allow under props.
	props := doc.Properties
	if !reflect.DeepEqual(props.WildcardChars, []string{"*"}) {
		t.Errorf("wildcard_chars = %v, want [*]", props.WildcardChars)
	}
	if !props.HasMidStarPattern {
		t.Error("has_mid_star_pattern = false, want true (rm * -rf)")
	}
	if !props.HasExactPattern {
		t.Error("has_exact_pattern = false, want true (git status)")
	}
	if !reflect.DeepEqual(props.ConflictingPatterns, []string{"git clean*"}) {
		t.Errorf("conflicting_patterns = %v, want [git clean*]", props.ConflictingPatterns)
	}
}

func TestGenerateIsByteDeterministic(t *testing.T) {
	dir := writeFakeAgents(t)
	first, err := generate(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := generate(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Error("two renders of the same tree differ; --check would never pass")
	}
}

func TestExtractorOnlyReadsAgentsDir(t *testing.T) {
	dir := writeFakeAgents(t)
	before := snapshotTree(t, dir)
	out := filepath.Join(t.TempDir(), "corpus.json")
	if _, err := generate(dir, out); err != nil {
		t.Fatal(err)
	}
	if after := snapshotTree(t, dir); !reflect.DeepEqual(before, after) {
		t.Errorf("the extractor changed the corpus source tree:\nbefore %+v\nafter  %+v", before, after)
	}
	// The path list must be IDENTICAL, not merely unchanged in content: no
	// file created either.
	var beforePaths, afterPaths []string
	for _, e := range before {
		beforePaths = append(beforePaths, e.Path)
	}
	for _, e := range snapshotTree(t, dir) {
		afterPaths = append(afterPaths, e.Path)
	}
	if !reflect.DeepEqual(beforePaths, afterPaths) {
		t.Errorf("the extractor created or removed paths:\nbefore %v\nafter  %v", beforePaths, afterPaths)
	}
}

func TestGeneratedCorpusCarriesNoCounts(t *testing.T) {
	dir := writeFakeAgents(t)
	data, err := generate(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{`"count`, `"total`} {
		if strings.Contains(string(data), forbidden) {
			t.Errorf("emitted JSON carries the count-shaped key %q", forbidden)
		}
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	var props map[string]json.RawMessage
	if err := json.Unmarshal(doc["properties"], &props); err != nil {
		t.Fatalf("properties block missing or malformed: %v", err)
	}
	want := []string{"wildcard_chars", "has_mid_star_pattern", "has_exact_pattern", "conflicting_patterns"}
	if len(props) != len(want) {
		t.Fatalf("properties carries %d fields, want exactly %d: %v", len(props), len(want), props)
	}
	for _, key := range want {
		if _, ok := props[key]; !ok {
			t.Errorf("properties missing %q", key)
		}
	}
}

func TestAgentsDirDiscovery(t *testing.T) {
	repo := t.TempDir()
	for _, d := range []string{"aeon", filepath.Join("opencode", "agents")} {
		if err := os.MkdirAll(filepath.Join(repo, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name     string
		explicit string
		cwd      string
		wantDir  string
		wantOk   bool
	}{
		{name: "from the repo root", cwd: repo, wantDir: filepath.Join(repo, "opencode", "agents"), wantOk: true},
		{name: "from aeon/ (Makefile cwd)", cwd: filepath.Join(repo, "aeon"), wantDir: filepath.Join(repo, "opencode", "agents"), wantOk: true},
		{name: "explicit flag used verbatim", explicit: filepath.Join(repo, "opencode", "agents"), cwd: t.TempDir(), wantDir: filepath.Join(repo, "opencode", "agents"), wantOk: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := resolveAgentsDir(tc.explicit, tc.cwd)
			if ok != tc.wantOk || got != tc.wantDir {
				t.Errorf("resolveAgentsDir(%q, %q) = (%q, %v), want (%q, %v)", tc.explicit, tc.cwd, got, ok, tc.wantDir, tc.wantOk)
			}
		})
	}

	t.Run("missing agents directory is a named skip", func(t *testing.T) {
		// repo has aeon + opencode but opencode/agents was never created.
		orphan := t.TempDir()
		if err := os.MkdirAll(filepath.Join(orphan, "aeon"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(orphan, "opencode"), 0o755); err != nil {
			t.Fatal(err)
		}
		got, ok := resolveAgentsDir("", orphan)
		if ok {
			t.Fatalf("found %q, expected a named skip", got)
		}
		if !strings.HasSuffix(got, filepath.Join("opencode", "agents")) {
			t.Errorf("skip names %q; it must name the path tried", got)
		}
	})

	t.Run("explicit missing flag is a named skip", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "nope")
		got, ok := resolveAgentsDir(missing, t.TempDir())
		if ok {
			t.Fatalf("found %q, expected a named skip", got)
		}
		if got != missing {
			t.Errorf("skip names %q, want the flag value %q verbatim", got, missing)
		}
	})
}

func TestCheckAgainstStaleFile(t *testing.T) {
	dir := writeFakeAgents(t)
	out := filepath.Join(t.TempDir(), "corpus.json")
	want, err := generate(dir, out)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fresh, want) {
		t.Fatal("written file differs from rendered bytes")
	}
	// One hand-edited byte must make the bytes differ — the check itself is
	// exercised end-to-end through `make check-corpusgen` in the task record.
	edited := append([]byte(nil), want...)
	edited[len(edited)/2] ^= 0x01
	if reflect.DeepEqual(edited, want) {
		t.Fatal("flip did not change the bytes")
	}
}

// runVerb drives run() with cwd moved into a throwaway directory, so the
// write/check paths exercise the cwd-relative outPath without touching the
// real committed corpus. Each call gets a FRESH cwd; tests that need the
// same cwd across calls call run() directly after one t.Chdir.
func runVerb(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	t.Chdir(t.TempDir())
	var outBuf, errBuf strings.Builder
	code = run(args, &outBuf, &errBuf)
	return code, outBuf.String(), errBuf.String()
}

func TestRunVerb(t *testing.T) {
	t.Run("write then check is green; one hand-edited byte is stale; restoring heals", func(t *testing.T) {
		agents := writeFakeAgents(t)
		cwd := t.TempDir()
		t.Chdir(cwd)
		var out, errb strings.Builder
		if code := run([]string{"--agents", agents}, &out, &errb); code != 0 {
			t.Fatalf("write mode exit %d, stderr %q", code, errb.String())
		}
		committed := filepath.Join(cwd, filepath.FromSlash(outPath))
		if _, err := os.Stat(committed); err != nil {
			t.Fatalf("write mode did not create %s: %v", outPath, err)
		}
		check := func() (int, string) {
			var o, e strings.Builder
			return run([]string{"--check", "--agents", agents}, &o, &e), e.String()
		}
		if code, stderr := check(); code != 0 || stderr != "" {
			t.Fatalf("check on a fresh corpus exit %d, stderr %q", code, stderr)
		}
		original := mustRead(t, committed)
		edited := append([]byte(nil), original...)
		edited[len(edited)/2] ^= 0x01
		if err := os.WriteFile(committed, edited, 0o644); err != nil {
			t.Fatal(err)
		}
		if code, stderr := check(); code != 1 || !strings.Contains(stderr, "corpus.json is stale; run make corpusgen") {
			t.Fatalf("check on a hand-edited corpus exit %d, stderr %q", code, stderr)
		}
		if err := os.WriteFile(committed, original, 0o644); err != nil {
			t.Fatal(err)
		}
		if code, stderr := check(); code != 0 || stderr != "" {
			t.Fatalf("check after restoring the bytes exit %d, stderr %q", code, stderr)
		}
	})

	t.Run("stale by absence: check exits 1 when the file does not exist", func(t *testing.T) {
		agents := writeFakeAgents(t)
		if code, _, stderr := runVerb(t, "--check", "--agents", agents); code != 1 ||
			!strings.Contains(stderr, "corpus.json is stale; run make corpusgen") {
			t.Fatalf("check with no committed file exit %d, stderr %q", code, stderr)
		}
	})

	t.Run("missing agents directory is a named skip in both modes", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "nope")
		for _, args := range [][]string{{"--agents", missing}, {"--check", "--agents", missing}} {
			code, stdout, stderr := runVerb(t, args...)
			if code != 0 || !strings.Contains(stdout, "corpus: skipped (no agents directory at "+missing) {
				t.Errorf("run(%v) = (%d, %q, %q), want exit 0 naming the skipped path", args, code, stdout, stderr)
			}
		}
	})

	t.Run("refused corpus data fails the build naming the file", func(t *testing.T) {
		dir := t.TempDir()
		bad := "---\nname: Bad\npermission:\n  bash:\n    \"ls*\": allowd\n---\n"
		if err := os.WriteFile(filepath.Join(dir, "bad.md"), []byte(bad), 0o644); err != nil {
			t.Fatal(err)
		}
		code, _, stderr := runVerb(t, "--agents", dir)
		if code != 1 || !strings.Contains(stderr, "bad.md") || !strings.Contains(stderr, `unknown verdict "allowd"`) {
			t.Fatalf("run with a typo'd verdict = (%d, stderr %q), want exit 1 naming bad.md", code, stderr)
		}
	})

	t.Run("bad flag exits 1", func(t *testing.T) {
		if code, _, _ := runVerb(t, "--nope"); code != 1 {
			t.Fatalf("bad flag exit %d, want 1", code)
		}
	})
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestFrontMatter(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		want      string
		wantFound bool
	}{
		{name: "no fence", raw: "# just a body\n", wantFound: false},
		{name: "empty front matter", raw: "---\n---\nbody\n", want: "", wantFound: true},
		{name: "closing fence ---", raw: "---\nname: x\n---\nbody\n", want: "name: x", wantFound: true},
		{name: "closing fence ...", raw: "---\nname: x\n...\nbody\n", want: "name: x", wantFound: true},
		{name: "crlf", raw: "---\r\nname: x\r\n---\r\nbody\n", want: "name: x", wantFound: true},
		{name: "bom", raw: "\ufeff---\nname: x\n---\n", want: "name: x", wantFound: true},
		{name: "unclosed fence", raw: "---\nname: x\n", wantFound: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := frontMatter([]byte(tc.raw))
			if ok != tc.wantFound || (ok && got != tc.want) {
				t.Errorf("frontMatter(%q) = (%q, %v), want (%q, %v)", tc.raw, got, ok, tc.want, tc.wantFound)
			}
		})
	}
}

func TestStableSource(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{in: "/some/checkout/repo/opencode/agents", want: "opencode/agents"},
		{in: "opencode/agents", want: "opencode/agents"},
		{in: filepath.Join("a", "b", "opencode", "agents"), want: "opencode/agents"},
		{in: "/operator/elsewhere/agents", want: "/operator/elsewhere/agents"},
	}
	for _, tc := range tests {
		if got := stableSource(tc.in); got != tc.want {
			t.Errorf("stableSource(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRenderErrorsAndEmpties(t *testing.T) {
	t.Run("missing directory is an error for generate", func(t *testing.T) {
		if _, err := generate(filepath.Join(t.TempDir(), "nope"), ""); err == nil {
			t.Fatal("expected a read error, got nil")
		}
	})
	t.Run("tree with no agents renders empty arrays not null", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("ignored"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(dir, "nested.md"), 0o755); err != nil {
			t.Fatal(err) // a DIRECTORY named *.md must be skipped, not read
		}
		data, err := generate(dir, "")
		if err != nil {
			t.Fatal(err)
		}
		s := string(data)
		if !strings.Contains(s, `"agents": []`) || !strings.Contains(s, `"wildcard_chars": []`) || !strings.Contains(s, `"conflicting_patterns": []`) {
			t.Errorf("empty corpus rendered as null instead of []: %s", s)
		}
	})
	t.Run("permission block followed by sibling keys keeps only its own lines", func(t *testing.T) {
		front := "name: Trailing\npermission:\n  bash:\n    \"ls*\": deny\n\n# a comment between keys\nmode: subagent\n"
		got, err := bashRules(front)
		if err != nil {
			t.Fatal(err)
		}
		want := []rule{{Pattern: "ls*", Verdict: "deny"}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("rules = %+v, want %+v", got, want)
		}
	})
}
