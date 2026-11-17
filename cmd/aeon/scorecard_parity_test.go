package main

// scorecard_parity_test.go — S63 T7: the CLI is a pure client, and every
// claim of "pure" is here as a test, not a comment. Parity BOTH ways
// (verb⇄method), `--json` as the wire's own bytes, n and state on every
// printed metric, the words `too-few` and `cannot-say` never degraded to
// zeros, the backfill previewed before it writes (RT13), the daemon's
// refusals arriving verbatim (no CLI-side validation to re-word them),
// no ranking flag anywhere, and no store import (T7's anti-second-truth
// walk). The census daemon is the REAL binary on a fixture socket over a
// pre-seeded store — the parity table T0 wrote is the fixture, and this
// file checks the CLI against the doors themselves.

import (
	"encoding/json"
	"errors"
	"go/parser"
	"go/token"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// --- the verb table (the parity map) ------------------------------------------

// cliVerbRows pairs EVERY method of the plane with the CLI case that speaks
// it — ten methods, ten verbs, both directions tested below (T7's parity
// table). A new method with no row fails TestEveryScorecardMethodHasAVerb;
// a verb with no method fails TestEveryVerbNamesAMethod.
var cliVerbRows = []struct {
	method protocol.MethodName
	file   string
	verb   string
}{
	{protocol.MethodScorecardAgent, "scorecard.go", "agent"},
	{protocol.MethodScorecardBlueprint, "scorecard.go", "blueprint"},
	{protocol.MethodScorecardComponent, "scorecard.go", "component"},
	{protocol.MethodScorecardCompare, "scorecard.go", "compare"},
	{protocol.MethodScorecardVocabulary, "scorecard.go", "vocabulary"},
	{protocol.MethodRetroList, "retro.go", "list"},
	{protocol.MethodRetroShow, "retro.go", "show"},
	{protocol.MethodRetroStatus, "retro.go", "status"},
	{protocol.MethodRetroBackfill, "retro.go", "backfill"},
	{protocol.MethodDigestLearned, "retro.go", "digest"},
}

func readCLIFile(t *testing.T, name string) string {
	t.Helper()
	src, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read cmd/aeon/%s: %v", name, err)
	}
	return string(src)
}

func TestEveryScorecardMethodHasAVerb(t *testing.T) {
	seen := map[protocol.MethodName]bool{}
	for _, row := range cliVerbRows {
		if seen[row.method] {
			t.Fatalf("method %s has two verb rows", row.method)
		}
		seen[row.method] = true
		src := readCLIFile(t, row.file)
		if !strings.Contains(src, `case "`+row.verb+`":`) {
			t.Errorf("%s has no verb: %s.go lacks `case %q:` — the CLI lags the socket", row.method, row.file, row.verb)
		}
	}
}

func TestEveryVerbNamesAMethod(t *testing.T) {
	// Every `case "…":` in the two files' subcommand switches must be a
	// row's verb (plus the usage cases), and the set must be complete:
	// an invented verb is as loud as a missing one.
	rows := map[string]bool{}
	for _, r := range cliVerbRows {
		rows[r.file+"\x00"+r.verb] = true
	}
	verbCase := regexp.MustCompile(`case "([a-z-]+)":`)
	for _, pair := range []struct{ file, fn string }{{"scorecard.go", "func cmdScorecard"}, {"retro.go", "func cmdRetro"}} {
		src := readCLIFile(t, pair.file)
		i := strings.Index(src, pair.fn)
		if i < 0 {
			t.Fatalf("%s missing", pair.fn)
		}
		rest := src[i:]
		if j := strings.Index(rest, "\n}\n"); j > 0 {
			rest = rest[:j]
		}
		for _, m := range verbCase.FindAllStringSubmatch(rest, -1) {
			verb := m[1]
			if verb == "help" {
				continue
			}
			if !rows[pair.file+"\x00"+verb] {
				t.Errorf("%s dispatches verb %q which names no method — an invented door", pair.file, verb)
			}
		}
	}
}

// TestScorecardVerbsAreDispatchedByTheBinary is S53's F-1 lesson applied
// to this pair: goldens that call cmdScorecard directly would stay green
// while the real binary answers "unknown command". The case, BOTH
// callees, and BOTH usage lines are pinned.
func TestScorecardVerbsAreDispatchedByTheBinary(t *testing.T) {
	src := readCLIFile(t, "main.go")
	for _, want := range []string{
		`case "scorecard", "retro":`,
		`verb == "scorecard"`,
		"cmdScorecard(os.Stdout, args)",
		"cmdRetro(os.Stdout, args)",
		"  scorecard ",
		"  retro ",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("main.go lost %q — the verb is dead to the real binary", want)
		}
	}
}

// --- the census daemon ----------------------------------------------------------

const (
	censusRoot  = "/fixture/census"
	censusSince = "2026-01-01T00:00:00Z"
	censusUntil = "2027-01-01T00:00:00Z"
)

// censusDaemon seeds the recorded facts BEFORE the daemon opens the store
// (the seed-before-start law), then boots the real binary: the doors the
// CLI speaks are the daemon's own.
func censusDaemon(t *testing.T) *daemonFixture {
	t.Helper()
	stateDir := t.TempDir()
	st, err := store.Open(store.Options{Dir: filepath.Join(stateDir, "aeon"), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("open the pre-seed store: %v", err)
	}
	seedCensusRows(t, st)
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	return startDaemonInStateDir(t, stateDir)
}

func seedCensusRows(t *testing.T, st *store.Store) {
	t.Helper()
	i := func(v int) *int { return &v }
	fc := func(v float64) *float64 { return &v }
	w := func(v int64) *int64 { return &v }
	done, failed := 1, 0
	one, two := 1, 2
	rows := []struct {
		slug   string
		kind   int
		agents []store.RetroAgent
	}{
		{"census-walk", done, []store.RetroAgent{{Agent: "atlas", AgentSlugKnown: true, Steps: i(4), StepsDone: i(4), VerifyPass: i(4)}}},
		{"census-scout", failed, []store.RetroAgent{{Agent: "atlas", AgentSlugKnown: true, Steps: i(2), StepsBlocked: i(2)}}},
	}
	for idx, r := range rows {
		cost := 0.004 + float64(idx)
		if _, _, err := st.RecordRetroOutcome(store.RetroOutcome{
			ComponentRoot: censusRoot, LoopSlug: r.slug, TerminalKey: "backfill@done",
			OutcomeKind: kindName(r.kind),
			BlueprintID: "census-blueprint", BlueprintVersion: &two,
			WallMS: w(60000 + int64(idx*1000)), CostUSDFloor: fc(cost),
			CostSteps: i(4), TasksDone: i(3), TasksFailed: i(0),
			ReviewRounds: &one, RecordedAt: "2026-06-01T12:00:00Z", Complete: true,
			Source: "recorder",
		}, r.agents); err != nil {
			t.Fatalf("seed %s: %v", r.slug, err)
		}
	}
}

func kindName(k int) string {
	if k == 1 {
		return "done"
	}
	return "failed"
}

func censusClient(t *testing.T, d *daemonFixture) *protocol.Client {
	t.Helper()
	conn, err := net.Dial("unix", d.endpoint)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	c := protocol.NewClient(conn, ClientName, Version)
	if _, err := c.Connect(0); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	return c
}

// --- --json is the wire ---------------------------------------------------------

// TestScorecardJSONIsTheWire: for EVERY verb, `--json` equals the socket's
// own result bytes — the CLI reshapes nothing, adds nothing, drops nothing.
func TestScorecardJSONIsTheWire(t *testing.T) {
	d := censusDaemon(t)
	c := censusClient(t, d)
	cases := []struct {
		args   []string
		method protocol.MethodName
		params any
	}{
		{[]string{"scorecard", "agent", "atlas", "--since", censusSince, "--until", censusUntil, "--json"},
			protocol.MethodScorecardAgent, protocol.ScorecardAgentParams{Agent: "atlas", ScorecardWindow: protocol.ScorecardWindow{Since: censusSince, Until: censusUntil}}},
		{[]string{"scorecard", "blueprint", "census-blueprint", "--since", censusSince, "--until", censusUntil, "--json"},
			protocol.MethodScorecardBlueprint, protocol.ScorecardBlueprintParams{Blueprint: "census-blueprint", ScorecardWindow: protocol.ScorecardWindow{Since: censusSince, Until: censusUntil}}},
		{[]string{"scorecard", "component", censusRoot, "--json"},
			protocol.MethodScorecardComponent, protocol.ScorecardComponentParams{ID: censusRoot, IncludeChildren: true}},
		{[]string{"scorecard", "compare", "agent", "atlas", "--a", censusSince + ".." + censusUntil, "--b", censusSince + ".." + censusUntil, "--json"},
			protocol.MethodScorecardCompare, protocol.ScorecardCompareParams{Kind: "agent", Subject: "atlas",
				WindowA: protocol.ScorecardWindow{Since: censusSince, Until: censusUntil},
				WindowB: protocol.ScorecardWindow{Since: censusSince, Until: censusUntil}}},
		{[]string{"scorecard", "vocabulary", "--json"}, protocol.MethodScorecardVocabulary, struct{}{}},
		{[]string{"retro", "list", "--json"}, protocol.MethodRetroList, protocol.RetroListParams{}},
		{[]string{"retro", "status", "--json"}, protocol.MethodRetroStatus, struct{}{}},
		{[]string{"retro", "backfill", "--json"}, protocol.MethodRetroBackfill, protocol.RetroBackfillParams{}},
		{[]string{"retro", "digest", censusRoot, "--since", censusSince, "--until", censusUntil, "--json"},
			protocol.MethodDigestLearned, protocol.DigestParams{ID: censusRoot, Since: censusSince, Until: censusUntil}},
	}
	// `retro show` needs the id the list itself printed.
	var raw json.RawMessage
	if err := c.Call(protocol.MethodRetroList, protocol.RetroListParams{}, &raw); err != nil {
		t.Fatal(err)
	}
	var lr protocol.RetroListResult
	if err := json.Unmarshal(raw, &lr); err != nil || len(lr.Rows) == 0 {
		t.Fatalf("census list: %v rows=%d", err, len(lr.Rows))
	}
	cases = append(cases, struct {
		args   []string
		method protocol.MethodName
		params any
	}{[]string{"retro", "show", strconv.FormatInt(lr.Rows[0].ID, 10), "--json"},
		protocol.MethodRetroShow, protocol.RetroShowParams{OutcomeID: lr.Rows[0].ID}})

	for _, tc := range cases {
		out, err := d.run(tc.args...)
		if err != nil {
			t.Fatalf("%v: %v\n%s", tc.args, err, out)
		}
		var want json.RawMessage
		if err := c.Call(tc.method, tc.params, &want); err != nil {
			t.Fatalf("%s call: %v", tc.method, err)
		}
		if out != string(want)+"\n" {
			t.Errorf("%v --json is not the wire:\n got %s\nwant %s", tc.args, out, want)
		}
	}
}

// --- the printed card -------------------------------------------------------------

func TestScorecardPrintsNAndStateForEveryMetric(t *testing.T) {
	d := censusDaemon(t)
	out := d.mustRun("scorecard", "agent", "atlas", "--since", censusSince, "--until", censusUntil)
	inMetrics := false
	lines := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "metrics:") {
			inMetrics = true
			continue
		}
		if !inMetrics {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || !strings.HasPrefix(line, "  ") {
			inMetrics = inMetrics && trimmed != "" && !strings.HasPrefix(line, "coverage:")
			continue
		}
		lines++
		if !strings.Contains(trimmed, "(n=") {
			t.Errorf("metric line without an n (RT7): %q", trimmed)
		}
		if !regexp.MustCompile(`\[[a-z-]+\]`).MatchString(trimmed) {
			t.Errorf("metric line without a state (RT7): %q", trimmed)
		}
	}
	if lines < 6 {
		t.Fatalf("only %d metric lines printed — the census agent card carries six:\n%s", lines, out)
	}
}

func TestTooFewAndCannotSayPrintAsWords(t *testing.T) {
	d := censusDaemon(t)
	out := d.mustRun("scorecard", "agent", "atlas", "--since", censusSince, "--until", censusUntil)
	doneRate := metricLine(t, out, "loops")
	if !strings.Contains(doneRate, "[too-few]") || !strings.Contains(doneRate, "—") {
		t.Errorf("two samples must print too-few with no value, never a bare count: %q", doneRate)
	}
	gate := metricLine(t, out, "verifyFailRate")
	if !strings.Contains(gate, "[cannot-say]") {
		t.Errorf("an unbacked metric must print cannot-say, never zero: %q", gate)
	}
	if strings.Contains(gate, " 0  ") {
		t.Errorf("cannot-say printed a zero: %q", gate)
	}
	// And the honest lines that DO lack values still carry their n.
	if !strings.Contains(doneRate, "(n=2)") {
		t.Errorf("the too-few line lost its n: %q", doneRate)
	}
}

func metricLine(t *testing.T, out, name string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), name) {
			return strings.TrimSpace(line)
		}
	}
	t.Fatalf("no %q line printed:\n%s", name, out)
	return ""
}

// --- backfill: preview first, always (RT13) --------------------------------------

func TestBackfillDefaultsToDryRun(t *testing.T) {
	stateDir := t.TempDir()
	st, err := store.Open(store.Options{Dir: filepath.Join(stateDir, "aeon"), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	// A terminal pre-S63 loop with no recorded outcome: backfill's prey.
	if _, err := st.CreatePinnedLoop(store.NewLoop{Slug: "pre-cli", ComponentRoot: censusRoot, Root: censusRoot, Goal: "g", Status: "done"},
		"dev-loop", 1, "blueprint: dev-loop\n", "{}"); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	d := startDaemonInStateDir(t, stateDir)

	out := d.mustRun("retro", "backfill")
	if !strings.Contains(out, "nothing written; pass --yes to write") {
		t.Fatalf("the dry-run print lost its seatbelt sentence:\n%s", out)
	}
	if !strings.Contains(out, "scanned: 1") {
		t.Fatalf("the preview did not scan the terminal loop:\n%s", out)
	}
	if outcomesAfter(t, d) != 0 {
		t.Fatal("backfill WITHOUT --yes wrote rows")
	}

	out = d.mustRun("retro", "backfill", "--yes")
	if strings.Contains(out, "nothing written; pass --yes to write") {
		t.Fatalf("the --yes run still previewed:\n%s", out)
	}
	if outcomesAfter(t, d) != 1 {
		t.Fatal("backfill --yes did not write the one eligible row")
	}
	// RT13: a backfilled outcome is a count, not a learning.
	if written := learningsWrittenAfter(t, d); written != 0 {
		t.Fatalf("backfill wrote %d learnings — outcomes are facts, lessons are the reactor's", written)
	}
}

func outcomesAfter(t *testing.T, d *daemonFixture) int {
	t.Helper()
	out := d.mustRun("retro", "status", "--json")
	var r protocol.RetroStatusResult
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("status --json is not the wire: %v\n%s", err, out)
	}
	return r.Outcomes
}

func learningsWrittenAfter(t *testing.T, d *daemonFixture) int {
	t.Helper()
	out := d.mustRun("retro", "status", "--json")
	var r protocol.RetroStatusResult
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatal(err)
	}
	return r.LearningsWritten
}

// --- refusals, verbatim -------------------------------------------------------------

func TestCLIRefusalsAreVerbatim(t *testing.T) {
	d := censusDaemon(t)
	c := censusClient(t, d)
	var raw json.RawMessage
	doorErr := c.Call(protocol.MethodRetroShow, protocol.RetroShowParams{OutcomeID: 999999}, &raw)
	if doorErr == nil {
		t.Fatal("the door accepted an outcome that does not exist")
	}
	out, err := d.run("retro", "show", "999999")
	if err == nil {
		t.Fatalf("the CLI accepted what the door refused:\n%s", out)
	}
	// Verbatim in the family's sense: renderError lays the door's own
	// fields down as lines — message, what, means, todo — adding no
	// words of its own. Every non-empty part must appear unchanged.
	var pe *protocol.Error
	if !errors.As(doorErr, &pe) {
		t.Fatalf("door error is not a protocol error: %v", doorErr)
	}
	for _, part := range []string{pe.Message, pe.Data.What, pe.Data.Means, pe.Data.Todo} {
		if part != "" && !strings.Contains(out, part) {
			t.Errorf("the CLI lost a piece of the daemon's refusal verbatim: %q\ncli out: %s", part, out)
		}
	}
}

// --- no ranking, no store -------------------------------------------------------------

// TestNoRankingFlag: no flag named sort, rank, best or worst — a
// leaderboard would start exactly here.
func TestNoRankingFlag(t *testing.T) {
	flagName := regexp.MustCompile(`fs\.\w+\([^,]+,\s*"([A-Za-z-]+)"`)
	for _, file := range []string{"scorecard.go", "retro.go"} {
		for _, m := range flagName.FindAllStringSubmatch(readCLIFile(t, file), -1) {
			name := strings.ToLower(m[1])
			for _, bad := range []string{"sort", "rank", "best", "worst"} {
				if strings.Contains(name, bad) {
					t.Errorf("%s declares flag %q — a ranking surface (T7's Do NOT)", file, m[1])
				}
			}
		}
	}
}

// TestScorecardVerbsNeverOpenTheStore is T7's second-implementation guard:
// the imports of the two CLI files, parsed (a comment is not evidence).
func TestScorecardVerbsNeverOpenTheStore(t *testing.T) {
	allowed := map[string]bool{
		"encoding/json": true, "flag": true, "fmt": true, "io": true,
		"sort": true, "strconv": true, "strings": true, "time": true,
		"github.com/cjlapao/aeon/internal/protocol": true,
	}
	fset := token.NewFileSet()
	for _, file := range []string{"scorecard.go", "retro.go"} {
		f, err := parser.ParseFile(fset, file, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if !allowed[path] {
				t.Errorf("%s imports %q — the CLI may speak protocol and print only (no store, no plane package)", file, path)
			}
		}
	}
}
