package protocol

// policy_authoring_guards_test.go — S45 T4's registration guard, RESTATED
// for the human door (the T6 PM10 mechanism begins here; T6 finishes it).
//
// Sibling function, never an edit: TestNoPolicyAuthoringMethodIsRegistered
// is skipped WITH a citing reason — its forbidden-scan (add/remove may
// never be registered) and its package source-scan are false by
// construction since PM1 made the three verbs the socket's human door. Its
// pre-amendment body is hash-pinned in T4's Notes. This file re-runs the
// WHOLE claim with the amended truth — nothing weakened, the absence made
// PROVEN in the new direction:
//
//   - the three authoring methods are registered iff Table.PolicyAuthoring
//     is wired (V13: ABSENT, not registered-and-refusing — Dispatch answers
//     ErrUnknownMethod, the sentinel, never a half-live handler);
//   - when wired, they require Initialize and are not Debug (product
//     surface: what keeps them off the narrow projection is T6's PM10
//     negatives, never the Debug accident);
//   - the source still cannot grow policy names in secret: the dotted
//     authoring literals live only in method.go's sanctioned block, the
//     constant tails are exactly the seven reads plus the three authoring
//     tails, and nothing else under `policy.` is registered.

import (
	"context"
	"os"
	"strings"
	"testing"
)

// stubPolicyAuthoring is a marker seam for the registration assertions —
// dispatch never reaches its methods in this file (registration shape is
// the subject, not behaviour; behaviour is daemon/policy_authoring_test.go).
type stubPolicyAuthoring struct{}

func (stubPolicyAuthoring) Add(context.Context, Actor, PolicyAddParams) (PolicyAddResult, error) {
	return PolicyAddResult{}, nil
}
func (stubPolicyAuthoring) Remove(context.Context, Actor, string) (PolicyRemoveResult, error) {
	return PolicyRemoveResult{}, nil
}
func (stubPolicyAuthoring) SeedCorpus(context.Context, Actor, SeedCorpusParams) (SeedCorpusResult, error) {
	return SeedCorpusResult{}, nil
}

var authoringMethodNames = []MethodName{MethodPolicyAdd, MethodPolicyRemove, MethodPolicySeedCorpus}

// policyReadMethodNames is the read window as T3 left it — the seven, no
// more, re-pinned here because the skipped original was their counter.
var policyReadMethodNames = []MethodName{
	MethodPolicyList, MethodPolicyWhy, MethodPolicyInForce, MethodPolicyEvents,
	MethodPolicyGet, MethodPolicyVocabulary, MethodPolicyChanges,
}

func TestPolicyAuthoringRegistrationIsConditional(t *testing.T) {
	// --- ABSENT when unwired (V13: absent, not registered-and-refusing) ----
	tbl := newTestTable(t)
	if tbl.RegisterPolicyAuthoring() {
		t.Fatal("RegisterPolicyAuthoring returned true with a nil seam — registered-and-refusing is V13's forbidden shape")
	}
	for _, name := range authoringMethodNames {
		if _, ok := tbl.Lookup(name); ok {
			t.Fatalf("%s is registered without the authoring seam — nil ⇒ ABSENT (V13)", name)
		}
		// Dispatch cannot find it either: the SENTINEL, not a handler and
		// not an Aeon refusal — the table does not know the name.
		if _, err := tbl.Dispatch(context.Background(), "c-none", name, nil); err == nil {
			t.Fatalf("%s dispatched without the seam", name)
		} else if _, ok := err.(ErrUnknownMethod); !ok {
			t.Fatalf("%s: err = %T (%v), want ErrUnknownMethod — absent means the table never heard of it", name, err, err)
		}
	}
	// And the reads still stand unwired (the human door's absence may not
	// take the read window down with it): exactly the seven.
	if got := countPrefix(tbl, "policy."); got != len(policyReadMethodNames) {
		t.Fatalf("registered policy.* with no authoring seam = %d, want the %d reads — the window and the doors are independent", got, len(policyReadMethodNames))
	}

	// --- REGISTERED when wired, with the right gates --------------------------
	tbl2 := newTestTable(t)
	tbl2.PolicyAuthoring = stubPolicyAuthoring{}
	if !tbl2.RegisterPolicyAuthoring() {
		t.Fatal("RegisterPolicyAuthoring returned false WITH the seam wired — the human door is wired out of existence")
	}
	for _, name := range authoringMethodNames {
		m, ok := tbl2.Lookup(name)
		if !ok {
			t.Fatalf("%s is not registered with the seam wired — PM1's human door is gone", name)
		}
		if !m.RequiresInitialize {
			t.Errorf("%s does not require Initialize — an authoring write on an unnegotiated connection is unthinkable", name)
		}
		if m.Debug {
			t.Errorf("%s is Debug — product surface, like every policy method; the narrow exclusion of Debug must never be what keeps these off HTTP (that absence is T6's PM10 guard, owned there)", name)
		}
	}
	if got := countPrefix(tbl2, "policy."); got != len(policyReadMethodNames)+len(authoringMethodNames) {
		t.Fatalf("registered policy.* with the seam = %d, want %d (seven reads + three doors) — no fourth authoring verb entered the table uninvited", got, len(policyReadMethodNames)+len(authoringMethodNames))
	}
	for _, name := range policyReadMethodNames {
		m, ok := tbl2.Lookup(name)
		if !ok {
			t.Fatalf("read %s vanished when authoring was wired", name)
		}
		if m.Debug {
			t.Errorf("read %s is Debug", name)
		}
	}
}

// TestPolicyAuthoringNamesStayConfined re-runs the skipped original's
// SOURCE scan with the amended truth: the package still cannot grow policy
// names in secret. The dotted authoring literals are confined to their one
// sanctioned site (method.go's S45 block); every MethodPolicy… constant
// tail is either a sanctioned read or one of the three doors; and the
// constants of both sets are pinned PRESENT (a deletion is as loud as an
// addition).
func TestPolicyAuthoringNamesStayConfined(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	constants := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		raw, err := os.ReadFile(e.Name())
		if err != nil {
			t.Fatal(err)
		}
		src := string(raw)

		// The dotted authoring verbs may be NAMED in exactly one file of
		// this package — method.go's sanctioned registration block. A
		// second spelling anywhere (a handler constant, a helper that
		// builds the name from parts someone forgot to review) is a second
		// truth about the door. The forbidden spellings are CONSTRUCTED
		// from parts (the original's `group + "add"` discipline — never one
		// literal in this file, so the scan cannot catch itself).
		if e.Name() != "method.go" {
			const group = "policy." // never one literal in this file
			for _, verb := range []string{"add", "remove", "seedCorpus", "seed-corpus"} {
				forbidden := `"` + group + verb + `"`
				if strings.Contains(src, forbidden) {
					t.Errorf("%s contains a dotted authoring literal — the authoring names are spelled once, in method.go (S45 T4/PM1); a second spelling is a second registration waiting to happen", e.Name())
				}
			}
		}

		for _, rest := range strings.Split(src, "Method"+"Policy") {
			end := 0
			for end < len(rest) && (rest[end] == '_' ||
				rest[end] >= 'A' && rest[end] <= 'Z' ||
				rest[end] >= 'a' && rest[end] <= 'z' ||
				rest[end] >= '0' && rest[end] <= '9') {
				end++
			}
			tail := rest[:end]
			if tail == "" || !isExportedTail(tail) {
				continue
			}
			constants[tail] = true
		}
	}

	sanctioned := map[string]bool{
		"List": true, "Why": true, "InForce": true, "Events": true, // S10
		"Get": true, "Vocabulary": true, // S45 T2 (PM5/PM6)
		"Changes": true, // S45 T3 (PM8)
		// S45 T4 (PM1): the human door's three tails — the ONLY new tails
		// this task sanctions; any other is a verb nobody ruled.
		"Add": true, "Remove": true, "SeedCorpus": true,
		// S45 T7 (PM9): the two propose twins' tails (policy_propose.go) —
		// INERT-row creators the cmd/aeon catalog guard already sanctions
		// by name (its data admits the twins and admits NOTHING else to
		// mutate in these families). Additive data for a sanctioned fact,
		// never a loosened rule: any other new tail is still a verb nobody
		// ruled, and the PRESENT check below keeps deletions as loud as
		// additions (the twins are pinned present too).
		"ProposeAdd": true, "ProposeRemove": true,
	}
	for name := range constants {
		if !sanctioned[name] {
			t.Errorf("a MethodPolicy%s constant exists beyond the sanctioned reads and the three S45 authoring doors (PM1/PM10)", name)
		}
	}
	for _, n := range []string{"List", "Why", "InForce", "Events", "Get", "Vocabulary", "Changes", "Add", "Remove", "SeedCorpus", "ProposeAdd", "ProposeRemove"} {
		if !constants[n] {
			t.Errorf("the MethodPolicy%s constant is missing — a silent deletion is as loud as a silent addition", n)
		}
	}
}

// countPrefix counts registered names under a dotted prefix.
func countPrefix(tbl *Table, prefix string) int {
	var n int
	for _, name := range tbl.Methods() {
		if strings.HasPrefix(string(name), prefix) {
			n++
		}
	}
	return n
}
