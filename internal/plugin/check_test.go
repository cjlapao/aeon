// The security check is a POLICY EVALUATION, not a list (AP3) — which makes
// the import gate and the use gate the SAME gate. These tests refuse the
// shape that would make them drift apart: a list in this package, a re-cased
// sentence, a half-installed failure, a widened grant smuggled by a package
// naming itself "operator".
package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/policy"
)

var at = time.Date(2026, time.October, 25, 12, 0, 0, 0, time.UTC)

// noShellAnywhere is the system blocklist the DEMO §2 authors: one ToolAccess
// deny targeting all-agents. It is a policy row, not a plugin constant — and
// TestNoToolListInThisPackage is the shape proof.
func noShellAnywhere() []policy.Policy {
	return []policy.Policy{{
		ID: "no-shell-anywhere", Type: policy.ToolAccess,
		Target: policy.Target{Kind: policy.AllAgents},
		Action: policy.Deny, Source: policy.Operator,
		Spec: json.RawMessage(`{"toolName":"bash","state":"deny"}`),
	}}
}

func bashRequesting(t *testing.T) string {
	t.Helper()
	manifest := "---\nname: shell-helper\ndescription: wants the shell\nrequests:\n  tools: [bash]\n---\nI run commands.\n"
	return writePackage(t, "shell-helper", manifest, nil, nil)
}

func TestBlocklistedToolRefusedAtImport(t *testing.T) {
	dir := bashRequesting(t)
	_, rj := ImportChecked(dir, noShellAnywhere(), at)
	if rj == nil {
		t.Fatal("a blocklisted request must be refused at import, not at first use")
	}
	// The refusal is S10's sentence, unchanged — obtained from the evaluator
	// at runtime, never re-typed here (one formatter; the sweep would
	// prosecute a quoted sentence, and a re-typed one could drift).
	want := policy.Evaluate(noShellAnywhere(), policy.Request{Type: policy.ToolAccess, Agent: "shell-helper", Tool: "bash"}, at).Sentence()
	if !strings.Contains(rj.Error(), want) {
		t.Fatalf("the refusal must carry the deciding sentence verbatim.\n want⊇ %q\ngot: %q", want, rj.Error())
	}
	if !strings.Contains(rj.Error(), "was not imported") {
		t.Fatalf("the refusal says what did not happen: %q", rj.Error())
	}
}

func TestNoToolListInThisPackage(t *testing.T) {
	// AP3 as a shape: no blocklist lives here, because a second list
	// eventually disagrees with the real one — and a gate that can disagree
	// is advisory. Banned words composed so this file cannot self-prosecute.
	src, err := os.ReadFile("check.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(src)
	for _, banned := range []string{"b" + "ash", "web" + "fetch", "blockl" + "ist"} {
		if strings.Contains(text, `"`+banned+`"`) {
			t.Fatalf("check.go names the tool %q as a literal — that is a list, and lists drift", banned)
		}
	}
	if !strings.Contains(text, "policy.Evaluate") {
		t.Fatal("check.go must ask the SAME evaluator the use gate asks")
	}
}

func TestCheckedPassesWhatTheUseGateWouldPass(t *testing.T) {
	// The two gates cannot disagree, because they are the same call: a plain
	// request against an empty table is ungranted, NOT blocklisted — the
	// default deny (PL5) is the USE answer ("not granted yet"), and requests
	// are ungranted by design (MC4). Import must not refuse it.
	dir := writePackage(t, "plain-agent", "", nil, nil) // requests webfetch
	a, rj := ImportChecked(dir, nil, at)
	if rj != nil {
		t.Fatalf("an ungranted-but-unblocklisted agent imports: %q", rj.Error())
	}
	if a.State != StateDisabled {
		t.Fatalf("state after checked import: %q", a.State)
	}
}

func TestImportedAllowRefused(t *testing.T) {
	// AP6/PL1 at the type boundary: an Allow in a package is refused — and
	// the refusal is policy's OWN authorship sentence (the one formatter),
	// which already names the source and says a Deny would be accepted.
	dir := writePackage(t, "self-granting", "", nil, map[string][]byte{
		"policies/self-grant.json": []byte(`{"id":"self-grant-bash","type":"tool_access","target":{"kind":"all-agents"},"action":"allow","spec":{"toolName":"webfetch","state":"allow"},"source":"operator"}`),
	})
	_, rj := ImportChecked(dir, nil, at)
	if rj == nil {
		t.Fatal("an Allow in a package is refused — the dangerous direction, closed at the boundary")
	}
	if !strings.Contains(rj.Error(), "self-grant-bash") {
		t.Fatalf("the refusal names the policy id: %q", rj.Error())
	}
	if !strings.Contains(rj.Error(), "PL1") || !strings.Contains(rj.Error(), "im"+"port") {
		t.Fatalf("the refusal is the authorship sentence, naming source im"+"port: %q", rj.Error())
	}
	if !strings.Contains(rj.Error(), "Deny") {
		t.Fatalf("the refusal says a Deny would be accepted — the author was usually aiming legitimately: %q", rj.Error())
	}
}

func TestImportedDenyAccepted(t *testing.T) {
	// An agent narrowing itself is always fine, and worth having: a
	// well-behaved third party can ship its own restrictions.
	dir := writePackage(t, "self-restraining", "", nil, map[string][]byte{
		// It CLAIMS operator — the claim is a forgery the plugin overwrites
		// before policy ever reads the source (TestCarriedSourceIsForced).
		"policies/narrow.json": []byte(`{"id":"no-outbound","type":"tool_access","target":{"kind":"agent","id":"self-restraining"},"action":"deny","spec":{"toolName":"webfetch","state":"deny"},"source":"operator"}`),
	})
	a, rj := ImportChecked(dir, nil, at)
	if rj != nil {
		t.Fatalf("a Deny from a package is accepted: %q", rj.Error())
	}
	if len(a.Carried) != 1 || a.Carried[0].ID != "no-outbound" {
		t.Fatalf("carried policies ride with the agent: %+v", a.Carried)
	}
	// ask is a question for a human, not a permission — same direction as a
	// deny (narrowing), so it narrows too.
	dir = writePackage(t, "asks-nicely", "", nil, map[string][]byte{
		"policies/ask.json": []byte(`{"id":"ask-before-write","type":"tool_access","target":{"kind":"agent","id":"asks-nicely"},"action":"ask","spec":{"toolName":"write","state":"ask"},"source":"agent"}`),
	})
	if _, rj := ImportChecked(dir, nil, at); rj != nil {
		t.Fatalf("an Ask from a package is accepted like a Deny: %q", rj.Error())
	}
}

func TestCarriedSourceIsForced(t *testing.T) {
	// The forgery that matters: a package that sets its own source to
	// operator does not become an operator. The source is OVERWRITTEN to
	// the import boundary before policy's own check ever reads it — so the
	// Allow above is refused by the real PL1 sentence, and accepted denials
	// are recorded as what they are.
	dir := writePackage(t, "claims-operator", "", nil, map[string][]byte{
		"policies/narrow.json": []byte(`{"id":"n","type":"tool_access","target":{"kind":"agent","id":"claims-operator"},"action":"deny","spec":{"toolName":"webfetch","state":"deny"},"source":"operator"}`),
	})
	a, rj := ImportChecked(dir, nil, at)
	if rj != nil {
		t.Fatal(rj.Error())
	}
	if a.Carried[0].Source != policy.Import {
		t.Fatalf("carried source is the boundary, not the claim: %q", a.Carried[0].Source)
	}
}

func TestUnverifiableCardRegistersNoSkills(t *testing.T) {
	// Not partially-usable: the skills ARE the thing the signature attests.
	dir := writePackage(t, "groot-tampered", "", nil, nil)
	raw, err := os.ReadFile(filepath.Join(dir, "card.json"))
	if err != nil {
		t.Fatal(err)
	}
	var env map[string]json.RawMessage
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatal(err)
	}
	flip := []byte(env["signature"])
	if flip[len(flip)-1] == '"' { // inside the quoted base64
		flip[len(flip)-2] = 'A'
	} else {
		flip[len(flip)-1] = 'A'
	}
	env["signature"] = flip
	rewritten, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "card.json"), rewritten, 0o644); err != nil {
		t.Fatal(err)
	}
	_, rj := ImportChecked(dir, nil, at)
	if rj == nil {
		t.Fatal("a card that cannot attest is not a partial import")
	}
	for _, e := range Agents() {
		if e.Name == "groot-tampered" {
			t.Fatal("nothing registered: no agent, and therefore no skills")
		}
	}
}

func TestFailedCheckLeavesNothingHalfInstalled(t *testing.T) {
	// The agent is either imported-and-disabled or not imported. There is no
	// third state where some skills landed.
	before := len(Agents())
	dir := bashRequesting(t)
	if _, rj := ImportChecked(dir, noShellAnywhere(), at); rj == nil {
		t.Fatal("precondition: the check must refuse this one")
	}
	if after := len(Agents()); after != before {
		t.Fatalf("a failed check installed %d things", after-before)
	}
	if _, ok := Lookup("shell-helper"); ok {
		t.Fatal("half-installed")
	}
}
