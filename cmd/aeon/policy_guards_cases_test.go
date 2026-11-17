package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/bootstrap"
)

// "no policy authoring reaches the narrow surface" (D16/PL15, adapted: the
// S19 action table does not exist yet, so the grep pairs with the pins).
func TestNoPolicyAuthoringReachesTheNarrowSurface(t *testing.T) {
	// S45 T4/T6 (PM1), supersession by the T6 step-4 pattern (sibling
	// functions, never edits): the operator ruling of 2026-10-30 made
	// `policy.add`/`policy.remove`/`policy.seedCorpus` the socket's HUMAN
	// door, so clause (a)'s first arm — the authoringMethodRef regex, which
	// bans those literals from every non-test file WITHOUT a data map to
	// consult — is now false by construction: the method table MUST name
	// them. No DATA map can rescue a compiled-regex clause, and the body is
	// not edited; the sibling `policy_authoring_guards_test.go`
	// (TestNoPolicyAuthoringReachesTheNarrowSurfaceAmended) re-runs the
	// WHOLE walk with the amended truth — the authoring literals confined
	// to their sanctioned registration site, the `"policy.` literal set,
	// the boundary/PolicyAdd/RecordAuthored whitelists (both extended,
	// listed below), and the api pin — so nothing this guard enforced stops
	// being enforced. Pre-amendment body pinned in T4's Notes (sha256
	// ee916b35bccad0c446c5e026d5e154fed4f17649d2a4153617c1f28325a6e19d of
	// `awk '/func TestNoPolicyAuthoringReachesTheNarrowSurface/,/^}/'`).
	// T6 (PM10) owns the final positive/negative restatement and consolidates.
	t.Skip("superseded by S45 T4/T6 PM1: add/remove/seedCorpus are the socket's human door — sibling TestNoPolicyAuthoringReachesTheNarrowSurfaceAmended re-runs the whole walk with the amended truth")

	// The authoring verbs, as a list: `add` and `remove` from S10, and
	// `seed-corpus` from S11's D4 seeding verb (loop T9). A method or handler
	// named `policy.<verb>` anywhere — dotted or camelCased — is the ruling
	// refused, not a detail.
	authoringMethodRef := regexp.MustCompile(`\bpolicy\.(add|remove|seed-corpus|seedCorpus)\b`)
	methodNameLiteral := regexp.MustCompile(`"policy\.[A-Za-z]+`)
	// (S20 T9 tightened, honestly:) the Add arm requires the POLICY type
	// in the argument list — the guard's subject is the policy Store's
	// authoring, whose signature is `Add(ctx, policy.Policy{…}, now)`. The
	// bare `(ctx` receiver-agnostic form began false-firing the day
	// control.go's own Scheduler.Add reached a daemon seam (trigger
	// registration is not policy authoring; PL15 was never about it). The
	// Remove arm stays as it was — id-only authoring verbs keep the wide
	// net, and a policy Store Remove that hid its type still trips it.
	boundaryAuthoringCall := regexp.MustCompile(`\.Add\([^)]*\),\s*policy\.Policy\{|\.Remove\((ctx\b|context\.)`)
	storeAuthoringCall := regexp.MustCompile(`\.(PolicyAdd|PolicyRemove)\((ctx\b|context\.)`)
	// The authored audit record (S11 loop T9): writing one without writing a
	// rule is the audit row that lies, and a daemon-start caller that reached
	// for it is the automatic seeder D4 welded shut. The CLI's seed verb is
	// its only sanctioned caller.
	authoredRecordCall := regexp.MustCompile(`\.RecordAuthored\((ctx\b|context\.)`)

	// T13 landed the app's read window: exactly these four wire names may
	// appear as method-name literals, and NOTHING else under `policy.` —
	// authoring is CLI-only, forever (R4c/PL15).
	policyReadMethods := map[string]bool{
		`"policy.list`: true, `"policy.why`: true, `"policy.inForce`: true, `"policy.events`: true,
		`"policy.get`:        true, // S45 T2 (PM5): the detail read — whole row, raw spec rides here; still a read, zero writes.
		`"policy.vocabulary`: true, // S45 T2 (PM5/PM6): the vocabulary read — closed sets + computed matrix; still a read, zero writes.
		`"policy.changes`:    true, // S45 T3 (PM8): the authoring audit read — append-only table, reads only; still a read, zero writes.
		// S45 T4 (PM1): the three HUMAN-DOOR authoring methods registered
		// on the socket per the operator ruling of 2026-10-30 — add,
		// remove, seedCorpus. This map is the pre-amendment DATA set T6
		// (PM10) restates positively; admitting the literals admits no
		// reach: PM10's negatives (narrow surface, agent services, tools,
		// records catalog) stand and are re-run by the sibling walk.
		`"policy.add`:        true,
		`"policy.remove`:     true,
		`"policy.seedCorpus`: true,
		// S45 T12 (PM15): "policy.corpus" is the seed SET id the ledger, the
		// events and the registry name the declared corpus by — a data
		// identity declared in internal/policyseed/decl.go, not a socket
		// method (the corpus is applied only through the seed door's
		// explicit path; nothing registers a `policy.corpus` handler, and
		// TestNothingSeedsAutomatically plus the class gate keep it that
		// way). The entry admits the literal; it admits no method.
		`"policy.corpus`: true,
	}

	allowedBoundary := map[string]bool{
		"cmd/aeon/policy.go": true, // this verb — the only authoring surface
		// S35 T5: `aeon proposal commit` applies a policy_add/policy_remove
		// proposal's effect through this SAME boundary — still CLI-only
		// (brief §7), still a human at a terminal, just a second file on
		// the one door. T6 (next in this slice) does the FULL whitelist
		// amendment this test's own sibling grep (internal/protocol's) asks
		// for; this one entry is the minimum needed to keep this test green
		// now that T5 introduces the caller — not a weakening, the same map
		// already admits more than one file for the same reason.
		"cmd/aeon/proposal.go": true,
		// The boundary's own implementation calls its RepoFuncs hooks to
		// cross into storage; excluding the definition file from (b)/(c)
		// does not weaken the claim — any OTHER package reaching Add/Remove
		// still fails, and (a) governs every file without exception.
		"internal/policy/store.go": true,
		// S45 T4 (PM1): the socket's authoring door is the ONE daemon-side
		// caller of the boundary's Add/Remove (every other daemon file
		// reaches policy only as a reader — the sibling walk's own daemon
		// clause pins that, and
		// TestAuthoringServiceIsTheOnlyDaemonCallerOfBoundaryAddRemove
		// greps for a second). It is the human door made wire, not a side
		// door: peer-stamped, audited beside the write, PL1 enforced once at
		// the boundary itself. Listed for T6 (PM10) to restate.
		"internal/daemon/policy_authoring.go": true,
		// S45 T4 (PM1): the protocol DISPATCH of the same door. Its
		// `.Remove(ctx…)` arm matches (b)'s regex, but this file never
		// touches the boundary — it forwards to the PolicyAuthoring seam
		// (the seam is an interface; calling it is not calling the store).
		// Listed so the walk can see the dispatch without flinching; the
		// boundary remains reachable only from the files above.
		"internal/protocol/policy_authoring.go": true,
	}
	allowedStoreBridge := map[string]bool{ // wiring bridges only (RepoFuncs hooks)
		"cmd/aeon/policy.go":             true,
		"internal/daemon/policy_gate.go": true,
		"internal/policy/store.go":       true, // the boundary calling its own hooks (store.go:188/196/422/434)
	}
	allowedAuthored := map[string]bool{ // the seed verb — the only authoring-audit caller
		"cmd/aeon/policy.go": true,
		// S45 T12 (PM15): the engine's policyseed applier is the seed door's
		// SECOND sanctioned caller — it pairs every row the declared
		// policy.corpus set writes through AddInTx with the same authored
		// audit row the CLI verb pairs through Add (same subject, same
		// policy_id). The claim this entry guards is unchanged — an
		// authored row still only ever accompanies a rule the human door
		// seeded, never a daemon-start or socket caller: the class gate
		// refuses the set at startup, ApplyCorpus refuses any actor but
		// the user or the cli, and TestStartupNeverAppliesPolicyCorpus
		// pins the boot path writes nothing.
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

		// (a) `policy.add` / `policy.remove` appear NOWHERE as method or
		// handler names — the socket never carries authoring (T13 lands four
		// READ-ONLY methods and nothing else).
		if method := authoringMethodRef.FindString(text); method != "" {
			offenders++
			t.Errorf("%s references %q — authoring must never reach the method table", rel, method)
		}
		for _, lit := range methodNameLiteral.FindAllString(text, -1) {
			if policyReadMethods[lit] {
				continue
			}
			offenders++
			t.Errorf("%s contains a %q method-name string literal — the socket carries exactly the four read-only policy methods T13 registered; anything else under `policy.` (an authoring verb above all) is the ruling, not a detail", rel, lit)
		}
		// (b) boundary authoring calls (Add/Remove on the policy Store)
		// appear ONLY in this file; the daemon's gate adapter reads (List,
		// RecordRefusal) and never authors.
		if boundaryAuthoringCall.MatchString(text) && !allowedBoundary[rel] {
			offenders++
			t.Errorf("%s calls the policy boundary's Add/Remove — authoring is confined to aeon/cmd/aeon/policy.go (PL15)", rel)
		}
		// (c) the raw store authoring methods are reached only through the
		// RepoFuncs bridges (this verb + the daemon's adapter wiring).
		if storeAuthoringCall.MatchString(text) && !allowedStoreBridge[rel] {
			offenders++
			t.Errorf("%s calls the store's PolicyAdd/PolicyRemove outside the sanctioned bridges", rel)
		}
		// (d) the authored audit record belongs to the seed verb alone: a
		// second caller would be a surface writing authoring-audit rows
		// without an operator typing an authoring command — D4's "never
		// automatic", pinned as a grep beside the authoring greps it sits with.
		if authoredRecordCall.MatchString(text) && !allowedAuthored[rel] {
			offenders++
			t.Errorf("%s calls the boundary's RecordAuthored — the authored audit record belongs to "+
				"aeon/cmd/aeon/policy.go's seed verb (D4: explicit, audited, never automatic)", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if walked < 50 {
		t.Fatalf("the walk saw only %d non-test files — the grep checked nothing", walked)
	}
	if offenders == 0 && walked == 0 {
		t.Fatal("the grep checked nothing")
	}

	// (e) paired IN-SUITE with the pin behind TestAPIIsPinnedOff
	// (internal/bootstrap/register_test.go): the api service is FORCE-OFF,
	// so no HTTP door exists that a policy handler could ride. Asserted here
	// through bootstrap.Pins() — the same fact TestAPIIsPinnedOff asserts
	// there; that test's own name is cited, and this claim is scoped to
	// what is re-asserted (SEC-048: the policy.Store surface itself is
	// unpinned, and this grep is the only authoring pin).
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

func TestOnlyProposalIsSocketWritable(t *testing.T) {
	var walked, offenders int
	err := filepath.WalkDir("../../", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
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
		rel := filepath.ToSlash(strings.TrimPrefix(filepath.ToSlash(path), "../../"))
		src, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		walked++
		for _, reason := range commitEffectOffenses(rel, string(src)) {
			offenders++
			t.Errorf("%s %s", rel, reason)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if walked < 50 {
		t.Fatalf("the walk saw only %d non-test files — the grep checked nothing", walked)
	}
	_ = offenders // errors already reported per-offender above

	// The canary (test map's own "the canary" row): proves this whitelist
	// has teeth — a forbidden call in a file the map does not name is
	// caught, using the EXACT SAME function the real walk above just ran.
	// Never a fixture file, never shipped: a string literal, checked and
	// gone by the time this function returns.
	canaryText := `package main
func sneakyHandler() {
	st.DecideProposal(1, "committed", by, "", at)
}
`
	if got := commitEffectOffenses("internal/protocol/sneaky.go", canaryText); len(got) == 0 {
		t.Fatal("canary: a DecideProposal call in a non-whitelisted file must be caught — the whitelist check has no teeth")
	}
	canarySaveConfig := `package main
func sneakyHandler2() {
	mcp.SaveConfig(path, cfg)
}
`
	if got := commitEffectOffenses("internal/protocol/sneaky2.go", canarySaveConfig); len(got) == 0 {
		t.Fatal("canary: a SaveConfig call in a non-whitelisted file must be caught — the whitelist check has no teeth")
	}
	// And the real door stays clear: the identical function, run against
	// the identical text, under the ACTUAL allowed path, finds nothing.
	if got := commitEffectOffenses("cmd/aeon/proposal.go", canaryText); len(got) != 0 {
		t.Fatalf("canary: cmd/aeon/proposal.go must stay whitelisted, got %v", got)
	}

	// (c), generalized from T3's own standalone TestNoProposalCommitMethodExists
	// into this file's permanent coverage: neither "proposal.commit" nor
	// "proposal.reject" appears as a method-name string literal in any
	// PRODUCTION file — there is no method table entry to register even by
	// typo. Test files are excluded (same convention as the methodNameLiteral
	// scan above): T3's own test legitimately names the literal to prove its
	// ABSENCE from the table, and this file's own prose above does too — a
	// string naming a thing you are checking is not an occurrence of the
	// thing.
	commitRejectLiteral := regexp.MustCompile(`"proposal\.(commit|reject)"`)
	// S45 T5 (PM1/PM7) DATA — the S35 §7 ruling was written for a world
	// where the socket NEVER decides. The operator's amendment mounts the
	// decision doors on the socket (human-only, peer-stamped, digest-bound,
	// agent_import refused by name until T10), so the dotted literals now
	// have homes exactly as sanctioned as the authoring names' one home:
	// the method table's S45 block, and the doors' own file (registration,
	// refusal sentences, docs). Every OTHER file stays refused by the
	// identical regex; the canary above keeps it honest.
	decisionMethodLiteralSites := map[string]bool{
		"../../internal/protocol/method.go":           true, // the sanctioned S45 registration block
		"../../internal/protocol/proposals_decide.go": true, // the doors themselves (T5)
	}
	err = filepath.WalkDir("../../", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
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
		src, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		if lit := commitRejectLiteral.FindString(string(src)); lit != "" && !decisionMethodLiteralSites[filepath.ToSlash(path)] {
			t.Errorf("%s contains %s as a string literal — commit/reject must never be named as a method, by typo or otherwise (brief §7)", filepath.ToSlash(path), lit)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
