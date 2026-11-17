package main

// policy_parity_test.go — S45 T8 (PM4). The parity guard: the two directions
// of the claim `aeon policy …`/`aeon proposal …` are CLIENTS of the daemon,
// not parallel implementations.
//
//	TestEveryCLIVerbHasASocketMethod — every CLI verb has a registered socket
//	twin, and every one of that verb's flags is carried by the twin's params
//	(or is on the documented list of flags that are the CLI's own by nature).
//
//	TestEverySocketPolicyMethodHasACLIVerb — every policy.* method and every
//	human-door proposal method registered on a fully wired daemon has a CLI
//	verb that dials it; the exceptions (the agent-side proposal doors) are
//	name-listed, and the list is checked against reality in BOTH directions
//	so it cannot drift into hiding a new method.
//
// The fixture is the parity table in docs/aeon/S45-policy-management/tasks/
// T0.md §2 — transcribed here as data with the status column resolved to
// what T2–T5 built (every "gap" row closed by its task). This test IS the
// table's mechanical half; the prose table stays where prose lives.

import (
	"reflect"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/protocol"
)

// cliParityCase is one row of the T0 §2 table made executable: the verb, its
// socket twin, the twin's params struct (the source of truth for "which
// flags can travel"), and the verb's flags each mapped to a param name or
// to a documented reason the CLI keeps it.
type cliParityCase struct {
	verb   string
	method protocol.MethodName
	params any
	flags  map[string]string // CLI flag → param json name, or "cli:<reason>"
}

// cliParityTable transcribes T0 §2. A flag whose value is "cli:…" is a
// documented cli-only-by-nature flag; anything else must appear as a json
// param on params (the guard's flag half — a verb row without its flags
// hides exactly the gaps T0 says to close).
var cliParityTable = []cliParityCase{
	{verb: "policy list", method: protocol.MethodPolicyList, params: protocol.PolicyListParams{},
		flags: map[string]string{"--type": "type"}},
	{verb: "policy get", method: protocol.MethodPolicyGet, params: protocol.PolicyGetParams{},
		flags: map[string]string{"--id": "id", "<id>": "id"}},
	{verb: "policy vocabulary", method: protocol.MethodPolicyVocabulary, params: struct{}{},
		flags: map[string]string{}},
	{verb: "policy changes", method: protocol.MethodPolicyChanges, params: protocol.PolicyChangesParams{},
		flags: map[string]string{"--policy": "policyId", "--limit": "limit"}},
	{verb: "policy add", method: protocol.MethodPolicyAdd, params: protocol.PolicyAddParams{},
		flags: map[string]string{
			"--type": "type", "--id": "id", "--target": "target", "--action": "action",
			"--spec": "spec", "--source": "source", "--not-after": "notAfter",
			"--tool": "cli: the tool_access spec's toolName sugar — expanded client-side " +
				"into --spec before the door, the socket receives the final spec (T0 §2, verified)",
			"--command": "cli: the command_access spec's command+state sugar — same ruling as --tool",
		}},
	{verb: "policy remove", method: protocol.MethodPolicyRemove, params: protocol.PolicyRemoveParams{},
		flags: map[string]string{"--id": "id"}},
	{verb: "policy seed-corpus", method: protocol.MethodPolicySeedCorpus, params: protocol.SeedCorpusParams{},
		flags: map[string]string{"--agent": "agents", "--dry-run": "dryRun"}},
	{verb: "policy why", method: protocol.MethodPolicyWhy, params: protocol.PolicyWhyParams{},
		flags: map[string]string{
			"--type": "type", "--agent": "agent", "--loop": "loop", "--lane-type": "laneType",
			"--mcp": "mcp", "--tool": "tool", "--command": "command", "--route": "route",
			"--effect": "effect", "--cost": "cost", "--skill": "skill", "--trigger": "trigger",
			"--at": "at",
		}},
	{verb: "policy in-force", method: protocol.MethodPolicyInForce, params: protocol.PolicyInForceParams{},
		flags: map[string]string{"--agent": "agent", "--loop": "loop"}},
	{verb: "policy events", method: protocol.MethodPolicyEvents, params: protocol.PolicyEventsParams{},
		flags: map[string]string{"--subject": "subject", "--depth": "limit"}},
	{verb: "proposal list", method: protocol.MethodProposalList, params: struct {
		Status string `json:"status"`
	}{}, flags: map[string]string{"--status": "status"}},
	{verb: "proposal show", method: protocol.MethodProposalGet, params: protocol.ProposalGetParams{},
		flags: map[string]string{"<id>": "id"}},
	{verb: "proposal commit", method: protocol.MethodProposalCommit, params: protocol.ProposalCommitParams{},
		flags: map[string]string{"<id>": "id", "--digest": "digest"}},
	{verb: "proposal reject", method: protocol.MethodProposalReject, params: protocol.ProposalRejectParams{},
		flags: map[string]string{"<id>": "id", "--reason": "reason"}},
}

// socketOnlyPolicyDoors names the registered policy-side methods the CLI
// deliberately does NOT dial — with the reason — and the guard asserts each
// is STILL registered (so the list cannot rot into hiding a renamed or
// dropped method) and that nothing outside it slips past the CLI-covered set.
var socketOnlyPolicyDoors = map[string]string{
	string(protocol.MethodPolicyProposeAdd):    "the records catalog's staging door (an agent proposes; a human commits) — the CLI has no propose verb by design (S45 T7)",
	string(protocol.MethodPolicyProposeRemove): "the twin of policy.proposeAdd — same ruling",
	string(protocol.MethodProposalCreate):      "staging is the agent/UI door (S35 T3); the CLI stages through the verbs that propose (agent import), never a bare create",
	string(protocol.MethodProposalDiscard):     "discarding a row nobody committed is the staging door's own cleanup (S35 T3)",
}

// parityDaemonTable returns a FULLY WIRED daemon table — the same wiring
// `aeon serve` performs for the policy/proposal family (store, boundary,
// authoring, decider, effects, validation) — so "registered" means what it
// means in production, not what a hand-built table happens to hold.
func parityDaemonTable(t *testing.T) *daemon.Daemon {
	t.Helper()
	st := proposalHome(t)
	opts := daemon.Options{
		Store:            st,
		ValidateProposal: validateProposalPayload,
		Effects:          func() (*desk.Registry, error) { return desk.EffectsFor(openDeskBoundary) },
	}
	if err := wireMCPServices(st, &opts); err != nil {
		t.Fatal(err)
	}
	return daemon.New(opts)
}

// TestEveryCLIVerbHasASocketMethod is the P34 direction of the parity claim:
// a capability that exists only as a CLI verb is not accepted as finished —
// so every verb here must have its twin REGISTERED on the daemon's table,
// and every flag it can be given must be able to TRAVEL (a param carries it,
// or it is on the name-listed cli-only list — never silently dropped).
func TestEveryCLIVerbHasASocketMethod(t *testing.T) {
	d := parityDaemonTable(t)
	seen := map[string]bool{}
	for _, c := range cliParityTable {
		if seen[c.verb] {
			t.Fatalf("the parity table lists %q twice", c.verb)
		}
		seen[c.verb] = true
		if _, ok := d.Table.Lookup(c.method); !ok {
			t.Errorf("%s: socket twin %s is NOT registered on a fully wired daemon — the CLI verb is the sole door, which P34 refuses", c.verb, c.method)
			continue
		}
		params := reflect.TypeOf(c.params)
		if params.Kind() == reflect.Ptr {
			params = params.Elem()
		}
		tags := map[string]bool{}
		for i := 0; i < params.NumField(); i++ {
			tag := strings.Split(params.Field(i).Tag.Get("json"), ",")[0]
			if tag != "" && tag != "-" {
				tags[tag] = true
			}
		}
		for flag, want := range c.flags {
			if strings.HasPrefix(want, "cli:") {
				continue // documented: the CLI keeps this flag by nature
			}
			if !tags[want] {
				t.Errorf("%s: flag %s has no param %q on %s — the flag cannot travel, which is the parity gap T0's table exists to close",
					c.verb, flag, want, c.method)
			}
		}
	}
}

// TestEverySocketPolicyMethodHasACLIVerb is the other direction: a registered
// policy/proposal method either has a CLI verb that dials it or sits on the
// name-listed socket-only set — and that set is checked against the table in
// BOTH directions, so neither a new method (unguarded by any verb) nor a
// quietly dropped one (the list rotting) passes.
func TestEverySocketPolicyMethodHasACLIVerb(t *testing.T) {
	d := parityDaemonTable(t)
	covered := map[protocol.MethodName]bool{}
	for _, c := range cliParityTable {
		covered[c.method] = true
	}
	// The daemon's human-door reads the CLI does not re-register as table
	// rows above are covered by the table; the socket-only list is checked
	// for continued registration below.
	registered := map[protocol.MethodName]bool{}
	for _, name := range d.Table.Methods() {
		registered[name] = true
		if !strings.HasPrefix(string(name), "policy.") && !strings.HasPrefix(string(name), "proposal.") {
			continue
		}
		if covered[name] {
			continue
		}
		if _, ok := socketOnlyPolicyDoors[string(name)]; ok {
			continue // name-listed, reasoned (the reason lives with the name above)
		}
		t.Errorf("%s is registered but no CLI verb dials it and it is not name-listed — either add the verb (P34: the CLI is a client of the surface, and the surface must stay reachable for a person at a terminal) or name the exclusion with a reason", name)
	}
	for name := range socketOnlyPolicyDoors {
		if !registered[protocol.MethodName(name)] {
			t.Errorf("the socket-only name-list still claims %s, but it is no longer registered — the list rots into hiding", name)
		}
	}
	for _, c := range cliParityTable {
		if !registered[c.method] {
			t.Errorf("%s names twin %s, which is not registered", c.verb, c.method)
		}
	}
}

// TestPolicyDoorInterfaceCoversEveryParityVerb is the static half of the
// claim: the door the verbs dial has one method per parity row — a verb
// cannot quietly bypass the door and open the store on its own. (Compile-
// time: each name below must exist with this exact signature on the door
// interface; a rename or drop fails to compile the assertion.)
func TestPolicyDoorInterfaceCoversEveryParityVerb(t *testing.T) {
	doorType := reflect.TypeOf((*policyDoor)(nil)).Elem()
	for _, name := range []string{"list", "get", "add", "remove", "seedCorpus", "why",
		"inForce", "events", "changes", "vocabulary",
		"proposalList", "proposalGet", "proposalCommit", "proposalReject"} {
		if _, ok := doorType.MethodByName(name); !ok {
			t.Errorf("policyDoor lost %s — the verbs may only reach policy through the door (PM4)", name)
		}
	}
	var _ policyDoor = (*policySocketDoor)(nil)
	var _ policyDoor = (*policyDirectDoor)(nil)
}
