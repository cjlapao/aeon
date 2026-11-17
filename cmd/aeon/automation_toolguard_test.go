package main

// automation_toolguard_test.go — S49 T7 (AN9, S40 AU1 lock 1, S11): the
// agent surface of the automation plane, asserted from the outside. T5's
// guard already refuses, by word, any tool named after an automation WRITE;
// what lives here is T7's own three claims:
//
//   - the five that exist are ALL that exist, and none of the write-or-
//     propose-a-write names is in the registry or the catalog (the reads are
//     reads; the one ask files an INERT disable — there is no propose-enable,
//     -update, -archive or -fire, and desk's own kind literals carry exactly
//     one automation kind);
//   - AU1's original lock-1 guard — the forbidden whole-catalog words in
//     internal/records' TestCatalogHasNoAuthorityActions — passes UNEDITED
//     over the five new names (T5's byte-pin holds the body; this re-runs
//     the predicate and names the words, so a collision is a rename of the
//     tool, never an edit of the guard);
//   - the registry grew by exactly the five, every one of them counted in
//     the attribution ledger, and none of them sits in DefaultGrant()
//     (registered is not granted, S11).

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/records"
	"github.com/cjlapao/aeon/internal/tools"
)

var automationAgentNames = []string{
	"automation_list", "automation_show", "automation_fires", "automation_health", "automation_propose_disable",
}

// automationAbsentNames is the surface that must NOT exist: every write verb
// as a tool, and every proposal verb an agent might dream of filing that is
// not a narrowing (rule 2: none can CREATE, ENABLE, UPDATE, ARCHIVE, PAUSE,
// RESUME or FIRE — and none can PROPOSE any of those).
var automationAbsentNames = []string{
	"automation_add", "automation_create", "automation_update", "automation_enable",
	"automation_disable", "automation_pause", "automation_resume", "automation_fire",
	"automation_firenow", "automation_archive", "automation_unarchive", "automation_test", "automation_dryrun",
	"trigger_pause", "trigger_resume", "trigger_firenow",
	"automation_propose_enable", "automation_propose_update", "automation_propose_archive",
	"automation_propose_pause", "automation_propose_resume", "automation_propose_add",
	"automation_propose_create", "automation_propose_fire",
}

// TestNoAutomationToolCanCreateEnableUpdateArchiveOrFire is rule 2 from the
// registry and catalog side, re-running T5's two predicates so the claim is
// checked against the same functions that guard 1 guards with — not against
// a copy of them.
func TestNoAutomationToolCanCreateEnableUpdateArchiveOrFire(t *testing.T) {
	// Exactly the five, from the catalog.
	seen := map[string]bool{}
	for _, sp := range records.Catalog() {
		if sp.Family != "automation" {
			continue
		}
		seen[sp.Name] = true
	}
	if len(seen) != 5 {
		t.Errorf("the catalog's automation family holds %d specs, want exactly 5: %v", len(seen), seen)
	}
	for _, name := range automationAgentNames {
		if !seen[name] {
			t.Errorf("%s is not in the catalog — the surface T7 defines is not there", name)
		}
	}
	// None of the forbidden names exists anywhere, in either projection.
	reg := tools.Registry()
	for _, bad := range automationAbsentNames {
		if _, ok := reg[bad]; ok {
			t.Errorf("the registry carries %q — an agent door onto a write, or onto a proposal that is not a narrowing (AN9)", bad)
		}
		for _, sp := range records.Catalog() {
			if sp.Name == bad {
				t.Errorf("the catalog carries %q — the same refusal, from the data the registry is projected from", bad)
			}
		}
	}
	// T5's predicates, re-run over the live data (the exemption they carry
	// is the propose name; the refusal is every write-verb name).
	if bad := toolsAutomationWriteOffenses(registryNames(reg)); len(bad) > 0 {
		for _, b := range bad {
			t.Error(b)
		}
	}
	if bad := recordsAutomationWriteOffenses(records.Catalog()); len(bad) > 0 {
		for _, b := range bad {
			t.Error(b)
		}
	}
	// And the KIND side of "none can PROPOSE any of those": internal/desk
	// declares exactly one automation proposal kind — automation_disable.
	// A second literal is a kind nobody narrowed (AN9's closed ask), found
	// here by text because the kind vocabulary is a store CHECK, not an
	// exported list.
	kindLit := regexp.MustCompile(`Kind\s*=\s*"automation_[a-z_]+"`)
	kinds := map[string]bool{}
	matches, err := filepath.Glob(filepath.Join("..", "..", "internal", "desk", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range matches {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range kindLit.FindAllString(string(src), -1) {
			q := m[strings.Index(m, `"`) + 1 : len(m)-1]
			kinds[q] = true
		}
	}
	if len(kinds) == 0 || !kinds["automation_disable"] {
		t.Fatalf("internal/desk declares no automation_disable kind literal — the scan reads nothing and passes for nothing: %v", kinds)
	}
	delete(kinds, "automation_disable")
	for k := range kinds {
		t.Errorf("internal/desk declares the automation kind %q — the plane's ONLY agent ask is the disable (AN9: there is no propose-enable, -update or -archive kind to reach)", k)
	}
}

// TestAU1OriginalLockOneGuardStillPasses is rule 7: S40's AU1 lock 1 — no
// agent tool references a trigger write — is internal/records'
// TestCatalogHasNoAuthorityActions, whose forbidden whole-catalog words
// (decide · accept · approve · trigger · mcp) are byte-pinned by T5's
// TestAU1OriginalGuardsAreByteIdentical. What THIS test adds is the
// collision check T7 owes: the five names, run through the same word list,
// match none of it — and the guard stands UNEDITED (this file never touches
// it; a name that collided would be renamed, which is what the five are:
// _list _show _fires _health and _propose_disable, no word of the five in
// any of them, the dot-names included).
func TestAU1OriginalLockOneGuardStillPasses(t *testing.T) {
	// The word list, restated here on purpose: if the original guard's list
	// ever moves, the two lists part company and this test says so.
	forbidden := []string{"decide", "accept", "approve", "trigger", "mcp"}
	src, err := os.ReadFile(filepath.Join("..", "..", "internal", "records", "catalog_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range forbidden {
		if !strings.Contains(string(src), `"`+w+`"`) {
			t.Errorf("the original lock-1 guard no longer carries %q — its list moved, and this restatement must move WITH the amendment that moved it, deliberately", w)
		}
	}
	for _, name := range automationAgentNames {
		low := strings.ToLower(name)
		for _, w := range forbidden {
			if strings.Contains(low, w) {
				t.Errorf("tool %q carries the forbidden word %q — AU1 lock 1 collides, and the answer is a RENAME of the tool, never an edit of the guard", name, w)
			}
		}
	}
	// The dot-names too: the tool's twin is what the model sees beside it.
	for _, sp := range records.Catalog() {
		if sp.Family != "automation" {
			continue
		}
		low := strings.ToLower(sp.Method)
		for _, w := range forbidden {
			if strings.Contains(low, w) {
				t.Errorf("%s's twin %q carries the forbidden word %q — the same rename rule, on the socket half", sp.Name, sp.Method, w)
			}
		}
	}
}

// TestRegistryCountedAndNotDefaultGranted is rule 6's registry half: the
// five stand in the registry (the catalog projection registered them), every
// one is accounted for in the attribution ledger by its exact slice row,
// and DefaultGrant() still hands out only its three — a step reaches the
// automation plane only through a tool_access row an operator added.
func TestRegistryCountedAndNotDefaultGranted(t *testing.T) {
	reg := tools.Registry()
	for _, name := range automationAgentNames {
		if _, ok := reg[name]; !ok {
			t.Errorf("%s is not in the registry — the catalog projects tools; a spec that projects nothing is a promise no step can keep", name)
		}
	}
	granted := map[string]bool{}
	for _, g := range tools.DefaultGrant() {
		granted[g] = true
	}
	for _, name := range automationAgentNames {
		if granted[name] {
			t.Errorf("%s is in DefaultGrant() — registered is not granted (S11); the automation plane reaches a step only through an operator's tool_access row", name)
		}
	}
	// Attribution: every one of the five appears in exactly one ledger row,
	// and the row is S49's.
	hits := map[string]int{}
	for _, row := range guardRegistryAttribution {
		for _, n := range row.Names {
			hits[n]++
		}
	}
	for _, name := range automationAgentNames {
		if hits[name] != 1 {
			t.Errorf("%s is attributed %d times in the ledger, want exactly 1 — the counted bump is a ledger, not a guess", name, hits[name])
		}
	}
	var s49Rows []string
	for _, row := range guardRegistryAttribution {
		for _, n := range row.Names {
			if n == "automation_list" {
				s49Rows = append(s49Rows, row.Slice)
			}
		}
	}
	if len(s49Rows) != 1 || !strings.HasPrefix(s49Rows[0], "S49") {
		t.Errorf("automation_list is attributed by slice %v, want one row opening S49", s49Rows)
	}
}

func registryNames(reg map[string]tools.Tool) []string {
	names := make([]string, 0, len(reg))
	for n := range reg {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
