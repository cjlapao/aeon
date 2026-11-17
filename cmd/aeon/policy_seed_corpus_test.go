package main

// T9 — `aeon policy seed-corpus`: explicit, audited, idempotent, never
// automatic (decision D4). Every test here drives the verb through cmdPolicy
// with the boundary pointed at the spy or a temp store; the one subprocess
// walk proves the built binary seeds from the COMPILED-IN corpus with no
// source tree in reach. Nothing in this file may exist as a helper that
// OTHER tests call to get rows into a store — the seeder is the operator's
// verb, and a test-helper shortcut to it is the implicit caller D4 forbids.

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/policy"
)

// --- independent oracles -------------------------------------------------------
//
// The seeded id and the seeded spec are RE-DERIVED here from the corpus rule,
// not read from the verb's own helpers: a test that verifies an id against
// the code that minted it proves id===id, not that the identity is what the
// criteria pinned (corpus.<agent>.<sha256(pattern)[0:12 hex]> with
// identifier-alphabet separators).

func wantSeedID(agent, pattern string) string {
	sum := sha256.Sum256([]byte(pattern))
	return fmt.Sprintf("corpus.%s.%x", agent, sum[:6])
}

func corpusRules(t *testing.T, agent string) []policy.CorpusRule {
	t.Helper()
	corpus, rej := policy.ParseCorpus(policy.EmbeddedCorpus())
	if rej != nil {
		t.Fatalf("the committed corpus does not parse: %s", rej.Reason)
	}
	rules, found := corpus.RulesFor(agent)
	if !found {
		t.Fatalf("the committed corpus carries no agent %q — the fixture this suite seeds is gone", agent)
	}
	if len(rules) == 0 {
		t.Fatalf("agent %q carries no rules", agent)
	}
	return rules
}

// --- the four properties, each with its test ------------------------------------

// Property 2+3: THROUGH THE BOUNDARY, OPERATOR-AUTHORED. Seed git-loop and
// every row comes back command_access / operator / agent:git-loop with the
// corpus's pattern and verdict — and one authored policy_event row each
// (counted, not assumed).
func TestSeedCorpusWritesOperatorRowsThroughTheBoundary(t *testing.T) {
	spy := &spyRepo{}
	withSpy(t, spy)
	rules := corpusRules(t, "git-loop")

	out := mustPolicy(t, "seed-corpus", "--agent", "git-loop")
	if want := fmt.Sprintf("seeded %d", len(rules)); !strings.Contains(out, want) {
		t.Errorf("the run did not report %q:\n%s", want, out)
	}
	if len(spy.rows) != len(rules) {
		t.Fatalf("the store holds %d rows, want %d", len(spy.rows), len(rules))
	}
	for i, r := range rules {
		got := spy.rows[i]
		if got.Type != "command_access" {
			t.Errorf("rule %d: type %q, want command_access", i+1, got.Type)
		}
		if got.Source != "operator" {
			t.Errorf("rule %d (%s): source %q — a seeded rule is authored by the operator running the "+
				"command (PL1's answer for an allow, and D4's for every verdict)", i+1, r.Pattern, got.Source)
		}
		if got.TargetKind != "agent" || got.TargetID != "git-loop" {
			t.Errorf("rule %d: target %s:%s, want agent:git-loop — a corpus rule belongs to the agent that authored it",
				i+1, got.TargetKind, got.TargetID)
		}
		if got.Action != string(r.Verdict) {
			t.Errorf("rule %d (%s): action %q, want %q", i+1, r.Pattern, got.Action, string(r.Verdict))
		}
		if want := wantSeedID("git-loop", r.Pattern); got.ID != want {
			t.Errorf("rule %d: id %q, want the deterministic %q", i+1, got.ID, want)
		}
		var spec struct {
			Command string `json:"command"`
			State   string `json:"state"`
		}
		if err := json.Unmarshal([]byte(got.SpecJSON), &spec); err != nil {
			t.Fatalf("rule %d: spec %q is not JSON: %v", i+1, got.SpecJSON, err)
		}
		if spec.Command != r.Pattern || spec.State != string(r.Verdict) {
			t.Errorf("rule %d: spec %+v, want command %q state %q", i+1, spec, r.Pattern, string(r.Verdict))
		}
		// stateAgreesWithAction's spelling, made byte-exact on the validation
		// criterion's own row: the seeder writes the spec the same way the
		// --command sugar does, and nothing else can be stored.
		if r.Pattern == "git clean*" {
			if got.SpecJSON != `{"command":"git clean*","state":"deny"}` {
				t.Errorf("the git clean* row stores %q, want the exact spec bytes", got.SpecJSON)
			}
		}
	}

	// One authored policy_event row per rule (counted), naming the rule and
	// the agent. Not a refusal — the audit of an authoring says what happened.
	if len(spy.events) != len(rules) {
		t.Fatalf("the audit holds %d events, want %d — a policy_event row per authored rule",
			len(spy.events), len(rules))
	}
	byRule := map[string]int{}
	for _, e := range spy.events {
		byRule[e.PolicyID]++
		if e.Outcome != "authored" {
			t.Errorf("event for %q has outcome %q, want authored", e.PolicyID, e.Outcome)
		}
		if e.SubjectKind != "agent" || e.SubjectID != "git-loop" {
			t.Errorf("event for %q is subject %s:%s, want agent:git-loop", e.PolicyID, e.SubjectKind, e.SubjectID)
		}
	}
	for _, r := range rules {
		if n := byRule[wantSeedID("git-loop", r.Pattern)]; n != 1 {
			t.Errorf("rule %s has %d audit events, want exactly 1", r.Pattern, n)
		}
	}

	// The rows survive the boundary's OWN re-read (List re-decodes every row
	// through the closed set) and come back through the listing surface.
	listOut := mustPolicy(t, "list", "--type", "command_access")
	if !strings.Contains(listOut, wantSeedID("git-loop", "git clean*")) || !strings.Contains(listOut, "operator") {
		t.Errorf("list lost the seeded rows:\n%s", listOut)
	}
}

// Property 4: IDEMPOTENT. The witness is the row and event COUNT before and
// after the second run — and an operator's edit to a seeded rule survives a
// third run untouched.
func TestSeedCorpusIsIdempotent(t *testing.T) {
	spy := &spyRepo{}
	withSpy(t, spy)
	rules := corpusRules(t, "git-loop")

	mustPolicy(t, "seed-corpus", "--agent", "git-loop")
	rows1, events1 := len(spy.rows), len(spy.events)

	out2 := mustPolicy(t, "seed-corpus", "--agent", "git-loop")
	if rows1 == 0 || events1 == 0 {
		t.Fatalf("the first run wrote nothing to count (%d rows, %d events)", rows1, events1)
	}
	if len(spy.rows) != rows1 || len(spy.events) != events1 {
		t.Errorf("the second run changed the counts: rows %d→%d, events %d→%d — re-running must be a no-op",
			rows1, len(spy.rows), events1, len(spy.events))
	}
	if spy.calls.add != rows1 || spy.calls.eventInsert != events1 {
		t.Errorf("the second run reached the seam again (add calls %d, event inserts %d)",
			spy.calls.add, spy.calls.eventInsert)
	}
	if want := fmt.Sprintf("unchanged %d", len(rules)); !strings.Contains(out2, want) {
		t.Errorf("the second run did not report every rule unchanged (%q):\n%s", want, out2)
	}

	// An operator edit to a seeded rule must survive re-seeding: remove +
	// re-author the git clean* row as an ask (the only remove/add edit the
	// boundary admits), then run a third time and demand the edit still stands.
	editID := wantSeedID("git-loop", "git clean*")
	mustPolicy(t, "remove", "--id", editID)
	mustPolicy(t, "add", "--type", "command_access", "--id", editID, "--target", "agent:git-loop",
		"--action", "ask", "--command", "git clean*")
	rows2, events2 := len(spy.rows), len(spy.events)

	mustPolicy(t, "seed-corpus", "--agent", "git-loop")
	if len(spy.rows) != rows2 || len(spy.events) != events2 {
		t.Errorf("the third run moved the counts around an operator edit: rows %d→%d, events %d→%d",
			rows2, len(spy.rows), events2, len(spy.events))
	}
	for _, r := range spy.rows {
		if r.ID == editID {
			if r.Action != "ask" || r.SpecJSON != `{"command":"git clean*","state":"ask"}` {
				t.Errorf("re-seeding REVERTED the operator's edit to %q (action %q, spec %q) — an existing "+
					"row is LEFT ALONE, that is the whole of the collision decision", r.ID, r.Action, r.SpecJSON)
			}
		}
	}
}

// Property 1 (the input half): the seeder's ONLY input is the compiled-in
// corpus. A run from a tree that has no opencode/ — the built binary in an
// empty directory — still seeds every row.
func TestSeedCorpusNeverReadsOpencode(t *testing.T) {
	base := t.TempDir()
	stateDir := filepath.Join(base, "state")
	workDir := filepath.Join(base, "tree-without-opencode")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("make the empty tree: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workDir, "opencode")); !os.IsNotExist(err) {
		t.Fatalf("the fixture tree unexpectedly HAS an opencode/ directory: %v", err)
	}

	cmd := aeonCommand(t, stateDir, "policy", "seed-corpus", "--agent", "git-loop")
	cmd.Dir = workDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("seeding from a tree without opencode/ failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(mustCombined(t, stateDir, "policy", "list", "--type", "command_access")), wantSeedID("git-loop", "git clean*")) {
		t.Error("the subprocess seed did not reach the store the listing reads")
	}
	// And the empty tree is STILL empty — the verb writes no file anywhere
	// (the corpus is compiled in; there is no path to open).
	entries, err := os.ReadDir(workDir)
	if err != nil {
		t.Fatalf("read the empty tree back: %v", err)
	}
	if len(entries) != 0 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("the seed verb wrote into the working tree: %v", names)
	}
}

// mustCombined runs the real binary against the temp state dir and returns
// its output, failing the test on a non-zero exit.
func mustCombined(t *testing.T, stateDir string, args ...string) []byte {
	t.Helper()
	cmd := aeonCommand(t, stateDir, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("aeon %v: %v\n%s", args, err, out)
	}
	return out
}

// Property 1 (the caller half): NOTHING seeds automatically. No file under
// internal/daemon or internal/store — source or test — names the verb, in
// any of the three spellings a wiring could hide in.
func TestNothingSeedsAutomatically(t *testing.T) {
	// S45 T4/T6 (PM1), supersession by the T6 step-4 pattern (sibling
	// functions, never edits): PM1 put the corpus seed VERB on the socket
	// as the human door, so the daemon-side service implementing it MUST
	// name it — `SeedCorpus` is the seam method's name
	// (protocol.PolicyAuthoring.SeedCorpus), and a method an implementor
	// may not spell is a method the interface does not have. The claim
	// this guard makes — NOTHING seeds without an explicit operator act —
	// is untouched: the door is reachable only by a human dispatching the
	// seed method over the socket (peer-stamped, RequiresInitialize),
	// T12's TestStartupNeverAppliesPolicyCorpus still pins the boot path,
	// and the sibling `policy_authoring_guards_test.go`
	// (TestNothingSeedsAutomaticallyAmended) re-runs the WHOLE walk — same
	// dirs, same three spellings — exempting ONLY the door and its test,
	// and ONLY for the capital spelling (the door file's own comments were
	// scrubbed to "the corpus seed door", so the hyphen and camel spellings
	// stay forbidden even there; the exemption is as narrow as a name
	// gets). Pre-amendment body pinned in T4's Notes (sha256
	// 0687b52806aea2cccb0ded2fae9a4ec3b631ce2e848359626b174c321bd3cb71 of
	// `awk '/func TestNothingSeedsAutomatically/,/^}/'`).
	// T6 (PM10) owns the final positive/negative restatement and consolidates.
	t.Skip("superseded by S45 T4/T6 PM1: the socket's human door must NAME the seed verb to implement the seam — sibling TestNothingSeedsAutomaticallyAmended re-runs the whole walk with the narrow exemption")

	dirs := []string{"../../internal/daemon", "../../internal/store"}
	tokens := []string{"seed-corpus", "SeedCorpus", "seedCorpus"}
	walked := 0
	for _, dir := range dirs {
		err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			src, readErr := os.ReadFile(path)
			if readErr != nil {
				return fmt.Errorf("read %s: %w", path, readErr)
			}
			walked++
			for _, tok := range tokens {
				if strings.Contains(string(src), tok) {
					rel, _ := filepath.Rel("../..", path)
					t.Errorf("%s names %q — no daemon-start loader, no auto-seed-when-empty, no store-side "+
						"helper: the seeder runs when an operator types it and never otherwise (D4)", rel, tok)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
	if walked < 10 {
		t.Fatalf("the walk saw only %d files across the two packages — the grep checked nothing", walked)
	}
}

// The explicit half of D4, both directions: --dry-run writes NOTHING (the spy
// counts are the witness), and an unnamed corpus — no --agent at all — is
// refused as the input error it is.
func TestSeedCorpusDryRunWritesNothing(t *testing.T) {
	spy := &spyRepo{}
	withSpy(t, spy)
	rules := corpusRules(t, "git-loop")

	out := mustPolicy(t, "seed-corpus", "--agent", "git-loop", "--dry-run")
	if spy.calls.add != 0 || spy.calls.eventInsert != 0 || len(spy.rows) != 0 || len(spy.events) != 0 {
		t.Fatalf("the dry run wrote: %d add calls, %d event inserts, %d rows, %d events",
			spy.calls.add, spy.calls.eventInsert, len(spy.rows), len(spy.events))
	}
	if want := fmt.Sprintf("would seed %d", len(rules)); !strings.Contains(out, want) {
		t.Errorf("the dry run did not print the per-agent counts it WOULD write (%q):\n%s", want, out)
	}
	if !strings.Contains(out, "dry-run") {
		t.Errorf("the dry-run output never says it was a dry run:\n%s", out)
	}
	// And the store really is untouched: the run after the dry run seeds ALL
	// the rules (nothing was pre-seeded by the rehearsal).
	if out2 := mustPolicy(t, "seed-corpus", "--agent", "git-loop"); !strings.Contains(out2, fmt.Sprintf("seeded %d", len(rules))) {
		t.Errorf("the run after the dry run did not seed the full set:\n%s", out2)
	}
}

// Three-state honesty: an unknown agent is the FILTER's answer — a refusal
// naming it — never "seeded zero rules", which reads as success. And the
// refusal happens BEFORE any write: a typo beside a good name seeds nothing.
func TestSeedCorpusRefusesUnknownAgent(t *testing.T) {
	spy := &spyRepo{}
	withSpy(t, spy)

	_, err := runPolicy(t, "seed-corpus", "--agent", "agent-that-does-not-exist")
	if err == nil {
		t.Fatal("an agent the corpus does not carry was accepted")
	}
	if !strings.Contains(err.Error(), "agent-that-does-not-exist") || !strings.Contains(err.Error(), "not in the corpus") {
		t.Errorf("the refusal did not name the unknown agent as the filter's answer: %v", err)
	}
	if spy.calls.add != 0 || spy.calls.eventInsert != 0 {
		t.Errorf("the unknown-agent refusal reached the seam: %+v", spy.calls)
	}

	// Mixed run: the known agent gets NOTHING either — a filter refusal is
	// not a partial success wearing a success's exit code.
	if _, err := runPolicy(t, "seed-corpus", "--agent", "git-loop", "--agent", "agent-that-does-not-exist"); err == nil {
		t.Fatal("a mixed known/unknown run was accepted")
	}
	if spy.calls.add != 0 || spy.calls.eventInsert != 0 || len(spy.rows) != 0 {
		t.Errorf("the mixed run wrote rows before refusing the unknown agent: %+v", spy.calls)
	}

	// And naming no agent at all is refused too — "explicit" is the property,
	// not the default.
	if _, err := runPolicy(t, "seed-corpus"); err == nil || !strings.Contains(err.Error(), "--agent") {
		t.Errorf("a seed with no --agent was not refused as the implicit run it is: %v", err)
	}
	if spy.calls.add != 0 {
		t.Errorf("the no-agent refusal reached the seam: %+v", spy.calls)
	}
}

// --- the audit surface under failure ---------------------------------------------

// The existing set could not be read; there is nothing to seed idempotently
// against, and a blind write would be the second writer of a rule that may
// already stand — with an operator's edit on it. The boundary's read refusal
// prints verbatim and the seam sees nothing.
func TestSeedCorpusWithoutTheExistingSetWritesNothing(t *testing.T) {
	spy := &spyRepo{listErr: errors.New("the reader pool is down")}
	withSpy(t, spy)
	_, err := runPolicy(t, "seed-corpus", "--agent", "git-loop")
	if err == nil {
		t.Fatal("a run that could not read the existing set wrote anyway")
	}
	if !strings.Contains(err.Error(), "the reader pool is down") {
		t.Errorf("the boundary's read refusal did not print verbatim: %v", err)
	}
	if spy.calls.add != 0 || spy.calls.eventInsert != 0 {
		t.Errorf("the blind run reached the write seam: %+v", spy.calls)
	}
}

// A rule the write boundary refuses stays REFUSED: the run says which rule
// failed with the boundary's reason verbatim, reports the agent NOT fully
// seeded, and stops instead of continuing to pretend.
func TestSeedCorpusPartialSeedNamesTheFailedRule(t *testing.T) {
	rules := corpusRules(t, "git-loop")
	failing := rules[2] // any rule past the first: two rows must land, the third must not
	spy := &spyRepo{addErrOn: wantSeedID("git-loop", failing.Pattern)}
	withSpy(t, spy)

	_, err := runPolicy(t, "seed-corpus", "--agent", "git-loop")
	if err == nil {
		t.Fatal("a boundary refusal was swallowed")
	}
	for _, want := range []string{"was REFUSED by the write boundary", "NOT fully seeded", failing.Pattern, "the store failed"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal lost %q (boundary reason travels verbatim):\n%v", want, err)
		}
	}
	if len(spy.rows) != 2 {
		t.Errorf("%d rows stored; the run stopped at rule 3, so exactly two stand", len(spy.rows))
	}
	if len(spy.events) != 2 {
		t.Errorf("%d audit events; one per stored row, none for the refused or unreached rules", len(spy.events))
	}
}

// The DEF-041 third state at the authoring door: a stored row whose audit
// event did not land is reported as exactly that — the rule stands, the
// audit is missing it — and the following run heals the audit without
// double-writing the row.
func TestSeedCorpusUnauditedRowIsSaidNotHidden(t *testing.T) {
	rules := corpusRules(t, "git-loop")
	failing := rules[0]
	spy := &spyRepo{eventErrOn: wantSeedID("git-loop", failing.Pattern)}
	withSpy(t, spy)

	_, err := runPolicy(t, "seed-corpus", "--agent", "git-loop")
	if err == nil {
		t.Fatal("the failed audit row was swallowed")
	}
	for _, want := range []string{"IS stored", "audit row was NOT written", "rule stands"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the unaudited report lost %q:\n%v", want, err)
		}
	}
	if len(spy.rows) != 1 || len(spy.events) != 0 {
		t.Fatalf("after the failed event: %d rows, %d events — the run stopped at rule 1, and the failed "+
			"insert landed no row (the store itself would refuse it the same way)", len(spy.rows), len(spy.events))
	}

	// Heal: the next run sees rule 1 EXISTING (unchanged — its row was never
	// doubled) and writes the remaining rows and their events. The gap the
	// failed insert left is NOT papered over: an existing row is left alone
	// INCLUDING its audit, and the listing above said so when it happened.
	// The operator's repair is remove + re-author (through the boundary), or
	// to live with a named unaudited row — the third state, never a silent zero.
	spy.eventErrOn = ""
	out := mustPolicy(t, "seed-corpus", "--agent", "git-loop")
	if !strings.Contains(out, "unchanged 1") {
		t.Errorf("the healed run did not count the already-stored row unchanged:\n%s", out)
	}
	if len(spy.rows) != len(rules) {
		t.Errorf("%d rows after healing, want %d — and never a double of the stored one", len(spy.rows), len(rules))
	}
	seen := map[string]int{}
	for _, e := range spy.events {
		seen[e.PolicyID]++
	}
	for _, r := range rules {
		want := 1
		if r.Pattern == failing.Pattern {
			want = 0 // the honest gap: reported out loud when it happened, never silently re-filled
		}
		if n := seen[wantSeedID("git-loop", r.Pattern)]; n != want {
			t.Errorf("rule %s has %d events after healing, want %d (an existing row is LEFT ALONE — "+
				"including its audit; re-attempting past it would double the events of a clean re-run)",
				r.Pattern, n, want)
		}
	}
}

// --- the list --type filter (the read-back surface the criteria name) --------------

// `aeon policy list --type command_access` is how a seeded row is read back.
// The filter selects on the ONE boundary read, refuses a type outside the
// closed set at the flag (through the boundary's own parser — delegation),
// and reports an empty filtered listing as the FILTER's answer — never the
// whole-table default-deny sentence.
func TestListTypeFilterIsTheFiltersAnswer(t *testing.T) {
	spy := &spyRepo{}
	withSpy(t, spy)
	mustPolicy(t, "seed-corpus", "--agent", "git-loop")
	mustPolicy(t, "add", "--type", "tool_access", "--id", "tool-row-for-filter", "--target", "agent:bob",
		"--action", "deny", "--spec", `{"toolName":"bash","state":"deny"}`)

	filtered := mustPolicy(t, "list", "--type", "command_access")
	if !strings.Contains(filtered, wantSeedID("git-loop", "git clean*")) {
		t.Errorf("the filtered listing lost the seeded rows:\n%s", filtered)
	}
	if strings.Contains(filtered, "tool-row-for-filter") {
		t.Errorf("the filter leaked a tool_access row:\n%s", filtered)
	}
	whole := mustPolicy(t, "list")
	if !strings.Contains(whole, "tool-row-for-filter") || !strings.Contains(whole, wantSeedID("git-loop", "git clean*")) {
		t.Errorf("the unfiltered listing lost a type:\n%s", whole)
	}

	// A filtered-empty is NOT the whole-table sentence (the D4 honesty, the
	// F-14 class): it names the filter and sends the operator to the whole.
	empty := &spyRepo{}
	withSpy(t, empty)
	out := mustPolicy(t, "list", "--type", "route_access")
	if !strings.Contains(out, "the filter's answer") {
		t.Errorf("an empty filtered listing did not say it is the filter's answer:\n%s", out)
	}
	if strings.Contains(out, "default is DENY") {
		t.Errorf("an empty FILTERED listing printed the whole-table default-deny sentence — a claim about "+
			"every row that the filter never looked at:\n%s", out)
	}

	// A type outside the closed set is refused at the flag, by the boundary's
	// own parser, before any store read — the counter must not move from here.
	before := empty.calls.listAll
	if _, err := runPolicy(t, "list", "--type", "not_a_type"); err == nil {
		t.Error("--type outside the closed set was accepted")
	}
	if empty.calls.listAll != before {
		t.Errorf("the --type refusal reached the store (listAll %d→%d) — parse refusals read nothing",
			before, empty.calls.listAll)
	}
}
