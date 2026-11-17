package main

// policy_authoring_guards_test.go — S45 T4's PL15 walk, RESTATED for the
// human door (the T6 PM10 mechanism begins here; T6 finishes it).
//
// Sibling functions, never edits (S35 T6's amendment method):
// TestNoPolicyAuthoringReachesTheNarrowSurface is skipped WITH a citing
// reason (its clause (a)'s first arm — a compiled regex with no data map to
// consult — is false by construction since PM1 made add/remove/seedCorpus
// the socket's human door; the method table MUST name them). Its pre-
// amendment body is pinned by hash in T4's Notes. Everything that guard
// enforced is re-enforced HERE, with the amended truth — nothing weakened:
//
//   - the authoring literals are confined to their ONE sanctioned
//     registration site (internal/protocol/method.go's S45 block), not set
//     free: any other non-test file naming `policy.<authoring verb>` is
//     still an offender (a comment in a daemon file naming the door is not
//     the method table — the site list is the discipline);
//   - every `"policy.` string literal is still confined to the whitelist
//     (the reads + the three sanctioned authoring names + the corpus set
//     id) — an authoring verb appearing anywhere else under `policy.` is
//     still the ruling refused;
//   - the boundary Add/Remove, PolicyAdd/PolicyRemove and RecordAuthored
//     greps still bind, with the two listed S45 T4 entries (the authoring
//     door and its dispatch) added to the same maps T5/T12 grew;
//   - the api-pin assertion (e) rides along unchanged (the skipped original
//     would otherwise take it down with it).
//
// The daemon-side clause (TestAuthoringServiceIsTheOnlyDaemonCaller…) is the
// task's own named pin: the human door is ONE file.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/bootstrap"
)

func TestNoPolicyAuthoringReachesTheNarrowSurfaceAmended(t *testing.T) {
	// Regexes byte-identical to the skipped original's clauses (a)-(d) —
	// restated as DATA (T6 option 1: the assertion logic is the pinned
	// half, the whitelists are the listed half).
	authoringMethodRef := regexp.MustCompile(`\bpolicy\.(add|remove|seed-corpus|seedCorpus)\b`)
	methodNameLiteral := regexp.MustCompile(`"policy\.[A-Za-z]+`)
	boundaryAuthoringCall := regexp.MustCompile(`\.Add\([^)]*\),\s*policy\.Policy\{|\.Remove\((ctx\b|context\.)`)
	storeAuthoringCall := regexp.MustCompile(`\.(PolicyAdd|PolicyRemove)\((ctx\b|context\.)`)
	authoredRecordCall := regexp.MustCompile(`\.RecordAuthored\((ctx\b|context\.)`)

	// (a-AMENDED) The authoring verbs may be NAMED only where they are
	// registered: internal/protocol/method.go's S45 authoring block. The
	// claim PM15 still makes is REACH, not spelling: no other file spells
	// the dotted authoring names, no surface mounts them, and the literals
	// have exactly one home a reviewer can grep. A second dotted spelling
	// anywhere — a daemon comment that "helpfully" names the method, a
	// handler constant outside the table — is the drift this confines.
	authoringLiteralSites := map[string]bool{
		"internal/protocol/method.go": true, // the sanctioned S45 registration block (PM1)
		// S45 T5: the decision doors' file spells the authoring verb once,
		// in its header — the SAME ruling the authoring doors live under
		// (the preview answers what policy.add would store), sanctioned
		// beside method.go's block, not exempted from the regex.
		"internal/protocol/proposals_decide.go": true,
	}

	// (a2) Every `"policy.` literal outside this set is still the ruling
	// refused (the original map's semantics, T4's three entries cited).
	policyWireNames := map[string]bool{
		`"policy.list`: true, `"policy.why`: true, `"policy.inForce`: true, `"policy.events`: true,
		`"policy.get`:        true, // S45 T2 (PM5)
		`"policy.vocabulary`: true, // S45 T2 (PM5/PM6)
		`"policy.changes`:    true, // S45 T3 (PM8)
		`"policy.add`:        true, // S45 T4 (PM1) — the human door, reach guarded by the clauses below
		`"policy.remove`:     true, // S45 T4 (PM1)
		`"policy.seedCorpus`: true, // S45 T4 (PM1)
		`"policy.corpus`:     true, // S45 T12 (PM15) — the seed SET id, never a method
		// S45 T7 (PM9): the two propose twins (internal/records' catalog
		// rows, registered in protocol/policy_propose.go under the
		// catalog guard's own sanction — its data already admits them as
		// the ONLY mutators of these families). Additive data for a
		// sanctioned fact: the set stays closed against every other
		// `policy.` name, and the reach half (narrow surface, agent
		// services, tool registry, catalog shape) stands unchanged.
		`"policy.proposeAdd`:    true,
		`"policy.proposeRemove`: true,
	}

	allowedBoundary := map[string]bool{
		"cmd/aeon/policy.go":       true, // the CLI door
		"cmd/aeon/proposal.go":     true, // S35 T5's CLI commit door
		"internal/policy/store.go": true, // the boundary calling its own hooks

		// S45 T4 (PM1): the socket's authoring door — the ONE daemon-side
		// caller of the boundary (pinned again, daemon-scoped, by
		// TestAuthoringServiceIsTheOnlyDaemonCallerOfBoundaryAddRemove) —
		// and protocol's dispatch of it, which forwards to the seam and
		// never touches the store.
		"internal/daemon/policy_authoring.go":   true,
		"internal/protocol/policy_authoring.go": true,
		// The agent doors (create · duplicate · edit · delete) author the rules that go with an
		// agent's tools, as the person who pressed the button, THROUGH that same socket door —
		// `Table.PolicyAuthoring`, the interface the daemon's authoring service implements — and
		// never around it: no boundary, no store, no new write path. Reviewed 2026-11-11 (the
		// agent grants: a tool an agent was given needs the rule that lets it be called, PL5).
		"internal/protocol/agent_grants.go": true,
	}
	allowedStoreBridge := map[string]bool{ // wiring bridges only (RepoFuncs hooks)
		"cmd/aeon/policy.go":             true,
		"internal/daemon/policy_gate.go": true,
		"internal/policy/store.go":       true,
	}
	allowedAuthored := map[string]bool{ // the authored audit record's callers
		"cmd/aeon/policy.go": true,
		// S45 T12 (PM15): the engine's policyseed applier (paired per-row
		// authored events for the declared corpus). S45 T4 adds NOTHING
		// here: the socket's seedCorpus reaches the seed through T12's door
		// (policyseed.ApplyCorpus), which owns that pair — a second
		// RecordAuthored caller beside it would be the two-audit seed T12's
		// Notes forbid, and the daemon's authoring service writes only the
		// one policy_change row per direct write (add/remove), never an
		// authored event.
		"internal/policyseed/applier.go": true,
	}

	var walked, offenders int
	err := filepath.WalkDir("../../", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable dirs (dist/, node_modules) hold no Go code we own
		}
		if d.IsDir() {
			switch d.Name() {
			case "dist", "node_modules", "ui", ".git", ".dev":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel := filepath.ToSlash(path)
		rel = strings.TrimPrefix(rel, "../../")
		src, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		walked++
		text := string(src)

		// (a-AMENDED) the dotted authoring names live in exactly one file.
		if method := authoringMethodRef.FindString(text); method != "" && !authoringLiteralSites[rel] {
			offenders++
			t.Errorf("%s references %q — the authoring verbs are named only in method.go's sanctioned S45 block; a second spelling is a second truth about the door (S45 T4/PM1)", rel, method)
		}
		// (a2) the `"policy.` literal set is closed.
		for _, lit := range methodNameLiteral.FindAllString(text, -1) {
			if policyWireNames[lit] {
				continue
			}
			offenders++
			t.Errorf("%s contains a %q method-name string literal — the socket carries exactly the whitelisted policy reads and the three S45 human-door names; anything else under `policy.` is the ruling, not a detail", rel, lit)
		}
		// (b) boundary authoring calls appear only in the whitelisted doors.
		if boundaryAuthoringCall.MatchString(text) && !allowedBoundary[rel] {
			offenders++
			t.Errorf("%s calls the policy boundary's Add/Remove outside the whitelisted doors (cmd/aeon's CLI verbs, the boundary itself, and the ONE S45 T4 socket door) — PL15's reach rule (PM1 amended, PM10's negatives stand)", rel)
		}
		// (c) the raw store authoring methods are reached only through the
		// RepoFuncs bridges.
		if storeAuthoringCall.MatchString(text) && !allowedStoreBridge[rel] {
			offenders++
			t.Errorf("%s calls the store's PolicyAdd/PolicyRemove outside the sanctioned bridges", rel)
		}
		// (d) the authored audit record belongs to the seed doors alone
		// (T4's socket seed reaches it THROUGH the engine applier, never
		// by calling it — an authoring-audit row without the row it records
		// is the audit that lies; D4's never-automatic, still pinned).
		if authoredRecordCall.MatchString(text) && !allowedAuthored[rel] {
			offenders++
			t.Errorf("%s calls the boundary's RecordAuthored — the authored audit record belongs to the seed doors (cmd/aeon's seed verb and the policyseed applier), never a socket handler's seam (D4: explicit, audited, never automatic)", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if walked < 50 {
		t.Fatalf("the walk saw only %d non-test files — the grep checked nothing", walked)
	}

	// Positive teeth against an empty pass: the sanctioned door MUST be
	// findable (a refactor that renamed the file out of the whitelist would
	// turn this walk into a green nothing — the T6 Do-NOT "a guard never
	// passes on an empty walk", made local).
	if !fileContains(t, "../../internal/daemon/policy_authoring.go", boundaryAuthoringCall) {
		t.Error("internal/daemon/policy_authoring.go no longer matches the boundary-call regex — the one whitelisted daemon door is not the one caller (or the door moved: the whitelist drifted)")
	}
	// And the registration site still exists with its three names, else
	// (a-AMENDED) is vacuous on a table someone emptied.
	reg := "../../internal/protocol/method.go"
	raw, err := os.ReadFile(reg)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{`"policy.add"`, `"policy.remove"`, `"policy.seedCorpus"`} {
		if !strings.Contains(string(raw), name) {
			t.Errorf("method.go no longer names %s — the sanctioned registration is gone and the literal confinement is guarding an empty set (PM1)", name)
		}
	}

	// (e) paired IN-SUITE with TestAPIIsPinnedOff (re-asserted from the
	// skipped original, which carried it): no HTTP door exists that a
	// policy handler could ride.
	var sawAPI bool
	for _, p := range bootstrap.Pins() {
		if p.Service == "api" {
			sawAPI = true
			if p.Enabled {
				t.Error("the api service is not pinned OFF — the daemon would be listening (AGENTS §3)")
			}
		}
	}
	if !sawAPI {
		t.Error("bootstrap.Pins() names no api pin — the ForceOff that keeps HTTP closed is gone")
	}
}

// TestAuthoringServiceIsTheOnlyDaemonCallerOfBoundaryAddRemove is the task's
// named pin (Tests table): grep of internal/daemon's NON-TEST files — the
// boundary's Add/Remove, the store's PolicyAdd/PolicyRemove and the
// boundary's RecordAuthored appear ONLY in policy_authoring.go (the human
// door) plus policy_gate.go (the RepoFuncs bridge that was always allowed
// to reach the store hooks). A second daemon file matching any of the three
// regexes is the side door brief §7 forbids having opened; the daemon
// authoring service writes NO RecordAuthored (T12's seed door owns that —
// the task's Behaviour 4 was written before the engine door existed; the
// deviation is recorded in T4's Notes and the regex makes it structural).
func TestAuthoringServiceIsTheOnlyDaemonCallerOfBoundaryAddRemove(t *testing.T) {
	authoringCall := regexp.MustCompile(`\.Add\([^)]*\),\s*policy\.Policy\{|\.Remove\((ctx\b|context\.)`)
	storeAuthoring := regexp.MustCompile(`\.(PolicyAdd|PolicyRemove)\((ctx\b|context\.)`)
	authored := regexp.MustCompile(`\.RecordAuthored\((ctx\b|context\.)`)

	allowed := map[string]bool{
		"policy_authoring.go": true, // the socket's authoring door (S45 T4)
		"policy_gate.go":      true, // the RepoFuncs bridge (hooks into the store; reads + the gate's own rows)
	}

	entries, err := os.ReadDir("../../internal/daemon")
	if err != nil {
		t.Fatal(err)
	}
	var checked int
	hits := map[string]bool{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join("../../internal/daemon", name))
		if err != nil {
			t.Fatal(err)
		}
		checked++
		text := string(src)
		if authoringCall.MatchString(text) || storeAuthoring.MatchString(text) || authored.MatchString(text) {
			hits[name] = true
			if !allowed[name] {
				t.Errorf("internal/daemon/%s calls the policy boundary's Add/Remove, the store's PolicyAdd/PolicyRemove, or RecordAuthored — the authoring door is ONE daemon file (policy_authoring.go) beside the gate's RepoFuncs bridge (policy_gate.go); a second is brief §7's side door", name)
			}
		}
	}
	if checked < 30 {
		t.Fatalf("the daemon grep saw only %d files — it checked nothing", checked)
	}
	// Teeth: the door itself MUST be among the hits, or the grep is
	// vacuously green (the door moved, or stopped calling the boundary —
	// either way the pin stopped pinning).
	if !hits["policy_authoring.go"] {
		t.Error("internal/daemon/policy_authoring.go no longer matches the authoring greps — the single-caller claim is guarding a file that does not author (a moved door is an unguarded door)")
	}
}

// fileContains is the positive-teeth helper for the walk above.
func fileContains(t *testing.T, path string, re *regexp.Regexp) bool {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return re.Match(raw)
}

// TestNothingSeedsAutomaticallyAmended is the D4 caller-half walk RESTATED
// for the human door (sibling of the skipped TestNothingSeedsAutomatically
// in policy_seed_corpus_test.go — same dirs, same three spellings, nothing
// weakened): no file under internal/daemon or internal/store names the
// seed verb in any spelling a wiring could hide in, EXCEPT the socket's
// authoring door and its test, and those only for the capital SeedCorpus
// — the seam method's name (protocol.PolicyAuthoring.SeedCorpus), which an
// implementor cannot avoid spelling. The camel and hyphen spellings stay
// forbidden EVERYWHERE in those trees (the door's own comments were
// scrubbed to "the corpus seed door" precisely so this exemption never
// grows a comment-sized life of its own). The D4 claim survives whole:
// naming is not invoking — the door runs only when a peer dispatches the
// method (TestPolicySeedWritesAndIsIdempotent pins the call path), boot
// applies nothing (T12's TestStartupNeverAppliesPolicyCorpus, untouched,
// and named below so a reader knows the boot half has an owner), and the
// exemption is pinned to the ENGINE door it must route through: the
// exempted service file must reach the corpus ONLY via
// policyseed.ApplyCorpus, which owns the costume gate and the run's single
// audit row; a file that stopped naming ApplyCorpus while still naming the
// verb found a second seed path, and the exemption dies with it.
func TestNothingSeedsAutomaticallyAmended(t *testing.T) {
	dirs := []string{"../../internal/daemon", "../../internal/store"}
	// The narrow exemption: rel path → spellings it may carry. ONLY the
	// capital seam-method spelling.
	exempt := map[string]map[string]bool{
		"internal/daemon/policy_authoring.go":      {"SeedCorpus": true},
		"internal/daemon/policy_authoring_test.go": {"SeedCorpus": true},
		// S45 T6 (reconciliation of T5's file into T4's exemption — the
		// DATA half, the assertions untouched): the DECIDE door's test names
		// the capital spelling for one thing only, the proposal KIND constant
		// `desk.KindSeedCorpus` and the test's own name. Deciding a
		// seed_corpus proposal is the most explicit operator act in the
		// product — a human who read the row and pressed commit — so D4's
		// "never automatic" is not what this file's naming threatens. The
		// teeth below pin that it names the KIND and never the ENGINE.
		"internal/daemon/proposal_decide_test.go": {"SeedCorpus": true},
	}
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
			rel, _ := filepath.Rel("../..", filepath.ToSlash(path))
			may := exempt[rel]
			for _, tok := range tokens {
				if strings.Contains(string(src), tok) && !may[tok] {
					t.Errorf("%s names %q — no daemon-start loader, no auto-seed-when-empty, no store-side "+
						"helper: the seeder runs when an operator types it and never otherwise (D4; S45 T4 amended only the door's capital seam spelling)", rel, tok)
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
	// Teeth on the exemption: the door must be real in BOTH directions —
	// it names the verb (or the exemption guards nothing) and it routes
	// the seed through T12's engine door (or it found a second seed path,
	// and the exemption is exactly the hole D4 refuses).
	door := readGuardFile(t, "../../internal/daemon/policy_authoring.go")
	if !strings.Contains(door, "SeedCorpus") {
		t.Error("internal/daemon/policy_authoring.go no longer names the seed verb — the exemption is protecting an empty set (a moved door is an unguarded door)")
	}
	if !strings.Contains(door, "policyseed.ApplyCorpus") {
		t.Error("internal/daemon/policy_authoring.go no longer routes through policyseed.ApplyCorpus — the seed has a second path and the exemption is covering it (D4: the engine door is the ONLY seed path)")
	}
	// The boot half stays owned by its own pin — assert it exists so a
	// reader knows the claim is not resting on this grep alone.
	// And the decide door's test earns its exemption the same way: it names
	// the KIND, and it never reaches the engine — a test beside the commit
	// door that started calling ApplyCorpus would be a second seed path
	// wearing a decision's clothes.
	decideTest := readGuardFile(t, "../../internal/daemon/proposal_decide_test.go")
	if !strings.Contains(decideTest, "KindSeedCorpus") {
		t.Error("internal/daemon/proposal_decide_test.go no longer names the seed_corpus KIND — its exemption is protecting an empty set; drop the entry")
	}
	if !strings.Contains(decideTest, "desk.Seams{") {
		t.Error("internal/daemon/proposal_decide_test.go no longer wires the seed effect through desk.Seams — its ApplyCorpus call is then a direct engine call from the decide door, which is exactly the second seed path D4 refuses")
	}
	// The engine door, counted rather than assumed: inside the daemon,
	// policyseed.ApplyCorpus may be reached from THREE files and three only,
	// each measured (T6's grep, recorded in T6's Notes) — the authoring door,
	// T12's corpus seed door (the seed.apply routing for the declared set),
	// and the decide test's injected seam (a test mount wiring the effect the
	// composition root wires in production). A fourth name is a second seed
	// path, and this is where it shows up.
	var engineCallers []string
	engErr := filepath.WalkDir("../../internal/daemon", func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		src, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(src), "policyseed.ApplyCorpus") {
			rel, _ := filepath.Rel("../..", filepath.ToSlash(path))
			engineCallers = append(engineCallers, rel)
		}
		return nil
	})
	if engErr != nil {
		t.Fatalf("walk internal/daemon for the engine door: %v", engErr)
	}
	allowedEngine := map[string]bool{
		"internal/daemon/policy_authoring.go":     true, // the product's seed door (T4)
		"internal/daemon/proposal_decide_test.go": true, // the test mount of the same door (T5)
		"internal/daemon/seed.go":                 true, // corpusSeedDoor: T12's routing of seed.apply for the declared policy.corpus set
	}
	for _, f := range engineCallers {
		if !allowedEngine[f] {
			t.Errorf("%s calls policyseed.ApplyCorpus — the engine has exactly the daemon-side doors listed, and a new one is a second seed path (D4)", f)
		}
	}
	for f := range allowedEngine {
		found := false
		for _, g := range engineCallers {
			if g == f {
				found = true
			}
		}
		if !found {
			t.Errorf("%s is listed as an engine-door caller but no longer calls policyseed.ApplyCorpus — drop the entry (an exemption without its reason)", f)
		}
	}
	boot := readGuardFile(t, "../../internal/daemon/policy_seed_test.go")
	if !strings.Contains(boot, "func TestStartupNeverAppliesPolicyCorpus") {
		t.Error("internal/daemon/policy_seed_test.go no longer owns TestStartupNeverAppliesPolicyCorpus — the boot half of D4 lost its owner (say so loudly, do not let the grep alone carry the claim)")
	}
}

// readGuardFile reads a guard's subject file, failing the test on any
// read error — a guard that silently skips an unreadable file is a guard
// that passes on nothing.
func readGuardFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}
