package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// blueprint_parity_fixture_test.go — the fixture the T11 guards stand on: a
// spy door that records what the CLI sent, a state dir whose store holds the
// shipped rows plus a custom one, and the small readers (a golden section,
// one function's source) the guards ask questions with. Nothing here asserts
// anything; the assertions live in blueprint_parity_test.go.

// --- the fixture ---------------------------------------------------------------------------

// blueprintCall is one door method the CLI spoke, with the params it sent
// decoded to strings so a test compares VALUES rather than pointers.
type blueprintCall struct {
	method string
	params map[string]string
}

// blueprintSpyDoor stands in for a door in the verb-level tests: it records
// what the CLI sent and answers with whatever the case needs.
type blueprintSpyDoor struct {
	calls      []blueprintCall
	saveRes    protocol.BlueprintSaveResult
	saveErr    error
	deleteRes  protocol.BlueprintDeleteResult
	showRes    protocol.BlueprintShowResult
	diffRes    protocol.BlueprintDiffResult
	historyRes protocol.BlueprintHistoryResult
	usageRes   protocol.BlueprintUsageResult
	lintRes    protocol.BlueprintLintResult
}

func (s *blueprintSpyDoor) record(method string, params any) {
	raw, _ := json.Marshal(params)
	var m map[string]string
	_ = json.Unmarshal(raw, &m)
	if m == nil {
		m = map[string]string{}
	}
	s.calls = append(s.calls, blueprintCall{method: method, params: m})
}

func (s *blueprintSpyDoor) fields(_ context.Context) (json.RawMessage, error) {
	s.record("fields", nil)
	raw, err := json.Marshal(map[string]any{"fields": blueprint.Fields()})
	return raw, err
}

func (s *blueprintSpyDoor) list(_ context.Context, p protocol.BlueprintListParams) (protocol.BlueprintListResult, error) {
	s.record("list", p)
	return protocol.BlueprintListResult{}, nil
}

func (s *blueprintSpyDoor) show(_ context.Context, name string) (protocol.BlueprintShowResult, error) {
	s.record("show", protocol.BlueprintShowParams{Name: name})
	return s.showRes, nil
}

func (s *blueprintSpyDoor) validate(_ context.Context, p protocol.BlueprintValidateParams) (json.RawMessage, error) {
	s.record("validate", p)
	raw, _ := json.Marshal(blueprint.Report{Blueprint: "x", SchemaVersion: 1, Version: 1})
	return raw, nil
}

func (s *blueprintSpyDoor) save(_ context.Context, p protocol.BlueprintSaveParams) (protocol.BlueprintSaveResult, error) {
	s.record("save", p)
	return s.saveRes, s.saveErr
}

func (s *blueprintSpyDoor) duplicate(_ context.Context, p protocol.BlueprintDuplicateParams) (protocol.BlueprintSaveResult, error) {
	s.record("duplicate", p)
	return s.saveRes, s.saveErr
}

func (s *blueprintSpyDoor) delete(_ context.Context, p protocol.BlueprintDeleteParams) (protocol.BlueprintDeleteResult, error) {
	s.record("delete", p)
	return s.deleteRes, nil
}

func (s *blueprintSpyDoor) override(_ context.Context, p protocol.BlueprintOverrideParams) (protocol.BlueprintOverrideResult, error) {
	s.record("override", p)
	return protocol.BlueprintOverrideResult{Name: p.Name, Lane: p.Lane, Field: p.Field, Value: p.Value, Changed: true}, nil
}

func (s *blueprintSpyDoor) revert(_ context.Context, p protocol.BlueprintRevertParams) (protocol.BlueprintSaveResult, error) {
	s.record("revert", p)
	return s.saveRes, s.saveErr
}

func (s *blueprintSpyDoor) usage(_ context.Context, name string) (protocol.BlueprintUsageResult, error) {
	s.record("usage", protocol.BlueprintUsageParams{Name: name})
	return s.usageRes, nil
}

func (s *blueprintSpyDoor) history(_ context.Context, name string, limit int) (protocol.BlueprintHistoryResult, error) {
	s.record("history", protocol.BlueprintHistoryParams{Name: name, Limit: limit})
	return s.historyRes, nil
}

func (s *blueprintSpyDoor) revision(_ context.Context, name string, rev int) (protocol.BlueprintRevisionResult, error) {
	s.record("revision", protocol.BlueprintRevisionParams{Name: name, Rev: rev})
	return protocol.BlueprintRevisionResult{}, nil
}

func (s *blueprintSpyDoor) diff(_ context.Context, p protocol.BlueprintDiffParams) (protocol.BlueprintDiffResult, error) {
	s.record("diff", p)
	return s.diffRes, nil
}

func (s *blueprintSpyDoor) lint(_ context.Context, p protocol.BlueprintLintParams) (protocol.BlueprintLintResult, error) {
	s.record("lint", p)
	return s.lintRes, nil
}

var _ blueprintDoor = (*blueprintSpyDoor)(nil)

// blueprintCLIStore gives a test a state dir whose store holds the shipped
// rows plus one custom row, and pins the environment so the DIRECT door is
// the road the verbs take (no daemon; the endpoint points at nothing).
func blueprintCLIStore(t *testing.T) *store.Store {
	t.Helper()
	state := t.TempDir()
	// The same two pins the T3 golden test uses: the state dir (which is where
	// the store lives) and an endpoint path that points at nothing (so the
	// dial answers "no daemon" and the DIRECT door is the road). The
	// DATABASE_SQLITE_* variables are deliberately left alone — an empty
	// string is not "unset" to storeOpenOptions, and pinning them to empty
	// sends the door to a different file than the one this helper seeds.
	t.Setenv("AEON_STATE_DIR", state)
	t.Setenv("LOCALRPC_ENDPOINT_PATH", filepath.Join(state, "nothing.sock"))
	st, err := openStoreDirectly(false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	for _, name := range blueprint.StandardNames() {
		raw, ok, err := blueprint.EmbeddedSource().Get(name)
		if err != nil || !ok {
			t.Fatalf("standard %s: ok=%v err=%v", name, ok, err)
		}
		blueprintSeedRow(t, st, ctx, name, string(raw), "system")
	}
	custom := "id: golden-custom\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: work\n    step: {kind: gate, question: go}\nquestions:\n  go:\n    prompt: go?\n    answers: [yes]\n"
	blueprintSeedRow(t, st, ctx, "golden-custom", custom, "user")
	return st
}

// blueprintSeedRow writes a row the way the golden fixture does — same shape,
// so a report the verbs print is the report T3 recorded.
func blueprintSeedRow(t *testing.T, st *store.Store, ctx context.Context, name, content, origin string) {
	t.Helper()
	sum := sha256.Sum256([]byte(content))
	hash := hex.EncodeToString(sum[:])
	kind := "user"
	if origin == "system" {
		kind = "system"
	}
	row := store.BlueprintRow{Name: name, Version: 1, Content: content, ContentHash: hash, Origin: origin, UpdatedBy: kind + ":test"}
	rev := store.RevisionRow{Version: 1, Content: content, ContentHash: hash, Op: "adopt", ActorKind: kind, ActorID: "test"}
	if err := st.BlueprintWithTx(ctx, func(tx *sql.Tx) error {
		return store.BlueprintPutTx(tx, row, rev, nil)
	}); err != nil {
		t.Fatalf("seed %s: %v", name, err)
	}
}

// blueprintRun speaks a verb in-process and returns what the person would see.
func blueprintRun(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var buf bytes.Buffer
	// Run FIRST, then read: the two operands of a `return f(), g()` are
	// evaluated left to right, so returning the buffer beside the call hands
	// back the buffer as it was BEFORE the command ran — empty.
	err := cmdBlueprint(&buf, args)
	return buf.String(), err
}

// blueprintVerb is blueprintRun for the calls that must succeed.
func blueprintVerb(t *testing.T, args ...string) string {
	t.Helper()
	out, err := blueprintRun(t, args...)
	if err != nil {
		t.Fatalf("aeon blueprint %s: %v\n(output: %q)", strings.Join(args, " "), err, out)
	}
	return out
}

// blueprintRunDoor speaks a verb down a named door, so a spy sees exactly the
// calls the CLI made. It routes the same way cmdBlueprint does.
func blueprintRunDoor(t *testing.T, door blueprintDoor, args ...string) (string, error) {
	t.Helper()
	if len(args) == 0 {
		t.Fatal("blueprintRunDoor needs a verb")
	}
	verb, rest := args[0], args[1:]
	var buf bytes.Buffer
	var err error
	switch {
	case isBlueprintDoorVerb(verb):
		err = runBlueprintDoorVerb(&buf, door, verb, rest)
	case verb == "fields":
		err = runBlueprintFields(&buf, door, rest)
	case verb == "edit":
		err = runBlueprintEdit(&buf, door, rest)
	case verb == "export":
		err = runBlueprintExport(&buf, door, rest)
	default:
		t.Fatalf("blueprintRunDoor cannot speak %q (it is not a door verb)", verb)
	}
	return buf.String(), err // read after the call, for the same reason
}

// blueprintCaptureStderr redirects the verbs' side channel for one call —
// the line a person sees about a kept temp file, without a pipe around the
// test binary's own stderr.
func blueprintCaptureStderr(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	restore := blueprintErrOut
	blueprintErrOut = &buf
	defer func() { blueprintErrOut = restore }()
	fn()
	return buf.String()
}

var blueprintKeptRe = regexp.MustCompile(`still at (\S+)`)

// blueprintKeptPath pulls the kept scratch file's path out of the side
// channel's sentence.
func blueprintKeptPath(t *testing.T, side string) string {
	t.Helper()
	m := blueprintKeptRe.FindStringSubmatch(side)
	if m == nil {
		t.Fatalf("the side channel never named a kept file: %q", side)
	}
	return m[1]
}

// blueprintGoldenSections reads T3's CLI golden into label → JSON record.
func blueprintGoldenSections(t *testing.T) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "bpgolden", "cli.txt"))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	label := ""
	var body strings.Builder
	flush := func() {
		if label != "" {
			out[label] = body.String()
		}
		label = ""
		body.Reset()
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "### ") {
			flush()
			label = strings.TrimPrefix(line, "### ")
			continue
		}
		body.WriteString(line)
		body.WriteString("\n")
	}
	flush()
	if len(out) < 10 {
		t.Fatalf("the golden parsed into %d sections — the fixture's shape changed", len(out))
	}
	return out
}

func blueprintGoldenRecord(t *testing.T, golden map[string]string, label string) (rec struct {
	Out   string `json:"out"`
	Error string `json:"error"`
}) {
	t.Helper()
	raw, ok := golden[label]
	if !ok {
		t.Fatalf("the golden has no %q section", label)
	}
	if err := json.Unmarshal([]byte(raw), &rec); err != nil {
		t.Fatalf("golden %q: %v", label, err)
	}
	return rec
}

// blueprintFileSource reads a CLI file by name (the tests run in cmd/aeon).
func blueprintFileSource(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// blueprintFuncSource returns one top-level function's source, braces matched
// — the honest way to say "the body of this function does not do X".
func blueprintFuncSource(t *testing.T, file, name string) string {
	t.Helper()
	src := blueprintFileSource(t, file)
	marker := "func " + name + "("
	at := strings.Index(src, marker)
	if at < 0 {
		t.Fatalf("%s has no func %s", file, name)
	}
	rest := src[at:]
	open := strings.Index(rest, "{")
	if open < 0 {
		t.Fatalf("%s: func %s has no body", file, name)
	}
	depth := 0
	for i := open; i < len(rest); i++ {
		switch rest[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return rest[:i+1]
			}
		}
	}
	t.Fatalf("%s: func %s never closes", file, name)
	return ""
}

func blueprintSorted(lines []string) bool {
	c := append([]string(nil), lines...)
	sort.Strings(c)
	for i := range c {
		if c[i] != lines[i] {
			return false
		}
	}
	return true
}
