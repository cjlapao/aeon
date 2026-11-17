package agentsvc

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/handler"
	"github.com/cjlapao/aeon/internal/tools"
)

// T2 — skill.go: typed requests. A malformed request is refused BEFORE any
// model call (a validation that runs after the model call costs exactly as
// much as no validation), the refusal names the missing field, and an
// unsupported mode names the supported set. The Client spy at zero is the
// whole point: no spy call, no money spent.

type spyClient struct {
	calls  int
	prompt string
}

func (s *spyClient) Complete(_ context.Context, prompt string) (string, error) {
	s.calls++
	s.prompt = prompt
	return "ok", nil
}

func cardWith(t *testing.T, skillsJSON string) *AgentCard {
	t.Helper()
	body := `{"name":"reddit-agent","description":"reads reddit","version":"1.0.0",
	  "defaultInputModes":["text/plain"],"defaultOutputModes":["text/plain"],
	  "skills":[` + skillsJSON + `]}`
	sc, err := Sign([]byte(body), testKey())
	if err != nil {
		t.Fatal(err)
	}
	card, err := Verify(sc, "reddit-agent")
	if err != nil {
		t.Fatal(err)
	}
	return card
}

func oneSkill() string {
	return `{"id":"summarise-sentiment","name":"Summarise sentiment","description":"what people say",
	        "tags":["reddit"],"examples":["what do people think of aeon?"],
	        "inputModes":["text/plain","application/json"],"outputModes":["text/markdown"]}`
}

func TestMissingFieldRefusedBeforeModelCall(t *testing.T) {
	card := cardWith(t, oneSkill())
	base := SkillRequest{Agent: "reddit-agent", Skill: "summarise-sentiment", Text: "what about aeon?", InputMode: "text/plain"}
	for _, test := range []struct {
		name  string
		mut   func(*SkillRequest)
		field string
	}{
		{"no-agent", func(r *SkillRequest) { r.Agent = "" }, "agent"},
		{"no-skill", func(r *SkillRequest) { r.Skill = "" }, "skill"},
		{"no-text", func(r *SkillRequest) { r.Text = "" }, "text"},
	} {
		req := base
		test.mut(&req)
		spy := &spyClient{}
		_, reject := Ask(context.Background(), spy, card, req)
		if reject == nil {
			t.Fatalf("%s: accepted a malformed request", test.name)
		}
		if spy.calls != 0 {
			t.Fatalf("%s: model was called for a malformed request — validation ran after the money", test.name)
		}
		// The refusal names the missing field (and the request's identity).
		if !strings.Contains(reject.Detail, `"`+test.field+`"`) || !strings.Contains(reject.Detail, "required") {
			t.Fatalf("%s: refusal must name the missing field: %s", test.name, reject.Detail)
		}
	}
	// The accepted path runs the client exactly once.
	spy := &spyClient{}
	if _, reject := Ask(context.Background(), spy, card, base); reject != nil {
		t.Fatalf("a well-formed request was refused: %s", reject.Detail)
	}
	if spy.calls != 1 {
		t.Fatalf("the accepted request must run the client once, got %d", spy.calls)
	}
}

func TestUnknownSkillEnumeratesWhatExists(t *testing.T) {
	card := cardWith(t, oneSkill())
	spy := &spyClient{}
	_, reject := Ask(context.Background(), spy, card, SkillRequest{Agent: "reddit-agent", Skill: "translate", Text: "x"})
	if reject == nil || spy.calls != 0 {
		t.Fatalf("an unknown skill must be refused before the model call: %v", reject)
	}
	if !strings.Contains(reject.Detail, "summarise-sentiment") {
		t.Fatalf("the refusal must enumerate what exists: %s", reject.Detail)
	}
	// A card that offers NO skills says so, rather than enumerating nothing into
	// a blank refusal.
	bare := cardWith(t, "")
	_, reject = Ask(context.Background(), &spyClient{}, bare, SkillRequest{Agent: "reddit-agent", Skill: "anything", Text: "x"})
	if reject == nil || !strings.Contains(reject.Detail, "offers no skills") {
		t.Fatalf("a no-skills agent must refuse saying so: %v", reject)
	}
}

func TestUnsupportedInputModeNamesSupportedSet(t *testing.T) {
	card := cardWith(t, oneSkill())
	spy := &spyClient{}
	_, reject := Ask(context.Background(), spy, card, SkillRequest{Agent: "reddit-agent", Skill: "summarise-sentiment", Text: "x", InputMode: "image/png"})
	if reject == nil || spy.calls != 0 {
		t.Fatalf("an unsupported inputMode must be refused before the model call: %v", reject)
	}
	for _, want := range []string{"image/png", "text/plain", "application/json"} {
		if !strings.Contains(reject.Detail, want) {
			t.Fatalf("the refusal must name the unsupported mode AND the supported set, missing %q in %q", want, reject.Detail)
		}
	}
	// Absent inputMode falls back to the skill's first mode (then the card's
	// defaults) — it is a fact about the skill, never a second refusal surface.
	if _, reject := Ask(context.Background(), &spyClient{}, card, SkillRequest{Agent: "reddit-agent", Skill: "summarise-sentiment", Text: "x"}); reject != nil {
		t.Fatalf("an omitted inputMode must fall back, not refuse: %s", reject.Detail)
	}
}

// TestAgentWithNoSkillsStillDispatches — AS1 again: a card with no skills (and
// an agent with no card at all) dispatches exactly as the 25 do.
func TestAgentWithNoSkillsStillDispatches(t *testing.T) {
	bare := cardWith(t, "")
	if len(bare.Skills) != 0 {
		t.Fatalf("a no-skills card grew skills: %#v", bare.Skills)
	}
	if _, ok := handler.Lookup("python-engineer"); !ok {
		t.Fatal("S13's registry must be untouched by skills")
	}
	if _, ok := handler.Build("python-engineer", "write python"); !ok {
		t.Fatal("the 25 must build unchanged with skills existing in this package (AS1)")
	}
}

// TestSkillDoesNotAppearInTheToolRegistry — AS3 vs S12. A tool is a capability;
// a skill is a request for judgement about how to use capabilities. Nothing
// here may register a tool: the registry stays the ten S11 pinned it, and no
// skill's id or name shows up among them.
func TestSkillDoesNotAppearInTheToolRegistry(t *testing.T) {
	card := cardWith(t, oneSkill())
	// The HONEST listing — ToolNames() is what a model is offered; Registry()
	// additionally carries the hidden write_file compatibility alias, which is
	// filtered from every listing and is no skill's hiding place either.
	names := tools.ToolNames()
	byName := map[string]bool{}
	for _, n := range names {
		byName[n] = true
	}
	// 2026-10-27 (S38 T9): added the management catalog (23 names) — OL2's single
	// vocabulary, projected from internal/records; registry is now the honest 33.
	// Count 10 -> 33 DELIBERATE.
	// 2026-10-27 (S38 T10): + task family (6 names) — Count 33 -> 39 DELIBERATE.
	// 2026-11-03 (merge reconciliation): main's 72 + S57 T9's nine gate
	// names + S48 T10's five blueprint names = 86, measured on THIS merged
	// tree; each landing's own arithmetic is dated in the block below.
	if len(names) != 141 {
		// 2026-11-08 (variables): + env_list, env_get — the variable manager's agent
		// tools (tools/env.go): names and descriptions of what a person made
		// agent-visible, and one value by name, recorded under the agent. Registered,
		// never default-granted. Count 139 -> 141 DELIBERATE (+2), measured on this tree.
		// 2026-11-08 (documents): + doc_write, doc_edit, doc_read, doc_list —
		// the deliverables door (tools/doc.go): a loop's plan.md, legal.md,
		// brief.md and tasks/ are rows, filed by these four. Registered, never
		// default-granted. Count 135 -> 139 DELIBERATE (+4), measured on this
		// tree; the 122 -> 135 leg is drift this pin did not record (sibling
		// landings measured their own trees), recorded here rather than silently.
		// 2026-11-05 (S63 T6): + scorecard_mine, scorecard_component,
		// digest_learned — the scorecard family's three safe reads (RT11):
		// own-view cards and the component card WITHOUT the per-child
		// breakdown, plus the learned digest; registered, granted to NO
		// ONE, and no write exists in the family (RT3). Count
		// 119 -> 122 DELIBERATE (+3), measured on this tree.
		// 2026-11-04 (S54 T4): + index_status — the platform family's one
		// read twin over platform.status, scoped to the step's own root
		// (freshness only; no control tool, no proposal kind — SP3/SP4;
		// attribution row lives in cmd/aeon/tools_guard_test.go). Count
		// 118 -> 119 DELIBERATE (+1), measured on this tree.
		// 2026-11-04 (S60 T9): + mergequeue_list, mergequeue_show,
		// mergequeue_status, mergequeue_propose_enqueue, mergequeue_dequeue —
		// the merge queue's agent doors (reads, plus the propose/dequeue
		// pair; the human gate writes are never tools). Count 113 -> 118
		// DELIBERATE (+5), measured on this tree; the pin was left stale by
		// that landing (attribution rows live in cmd/aeon/tools_guard_test.go).
		// 2026-11-04 (S52 T4, recorded at S53 T3's landing): + the memory
		// family (learning_add, learning_propose_retract, learning_query,
		// learning_show) — landed without a bump here, the same silence the
		// 86 -> 104 leg records; named here rather than quietly. Count
		// 107 -> 111 on this tree.
		// 2026-11-04 (S53 T3, HV4): + history_list, history_show — the
		// history family's two READS (twinned to the person's
		// history.list/history.show through the ONE shared answer, rule 4;
		// no export twin — HV5 — and no coverage/vocabulary twin — HV4;
		// attribution row lives in cmd/aeon/tools_guard_test.go). Count
		// 111 -> 113 DELIBERATE (+2), measured on this tree.
		// 2026-10-28 (S39 T10): + run_start — the one agent-runnable loop
		// action behind an operator-added ask row (CR5); Count 39 -> 40
		// DELIBERATE.
		// 2026-10-29 (S24 T5): + report_size — the queue's sizing report
		// (D9); registered, never granted, and no skill's hiding place.
		// Count 40 -> 41 DELIBERATE.
		// 2026-10-31 (S44-extra CT9): + the component family (9); Count -> 50.
		// 2026-10-31 (S43-extra T6): + seed_status; Count -> 51.
		// 2026-11-01 (S45 T7): + policy_propose_add / policy_propose_remove;
		// Count -> 53. (The pin was left stale at 41 across those three landings
		// — repaired by S46 T8 while it counted its own +5, recorded deviation.)
		// 2026-11-01 (S56 T8): + the five autonomy arms; Count -> 58.
		// 2026-11-02 (S46 T8): + the five mcp own-view doors (mcp_servers,
		// mcp_grants, mcp_status, mcp_propose_remove_server,
		// mcp_propose_revoke_grant) — three reads and the two narrowing
		// proposes (MG9), registered and granted to NO ONE. Count -> 61
		// DELIBERATE (unmounted count; graph's tool joins only when its
		// service is wired, as the tools-package listing test measures).
		// 2026-11-02 (S55 merge): S55 T10's seven loop-control / budget /
		// watchdog arms (loop_control_state, budget_effective, budget_list,
		// budget_show, budget_spend, watchdog_list, loop_request_stop) join
		// the merged registry; Count 61 -> 68, measured on the merged tree.
		// (S55's own lineage pinned 44 -> 51 on its side of the fork; the
		// two lineages' arithmetic reconciles here, once.)
		// 2026-11-02 (S47 T7): + tool_show, tools_mine — the agent's own
		// tool view (tools.mine.list / tools.mine.show as tools; OL3: the
		// subject is the step's, never a param); Count 68 -> 70. (Landed
		// after the S55 merge; the pin was left stale — repaired here at
		// S62 T11's acceptance re-run, together with the two S62 landings.)
		// 2026-11-02 (S62 T6): + backup_status — the one read-only backup
		// status door for agents (BK13's twin: read-only, no restore on
		// any surface); Count 70 -> 71.
		// 2026-11-02 (S62 T8): + export_sets — the read-only export
		// catalog door; Count 71 -> 72.
		// 2026-11-03 (S57 T9): + blueprint_list, blueprint_show,
		// blueprint_validate, blueprint_lint, blueprint_propose — the
		// blueprint family's four reads and its ONE propose twin (BP9: an
		// agent proposes, a person commits; there is deliberately no save/
		// duplicate/delete/revert tool, pinned by S48 T8's guard); Count
		// 72 -> 77, measured on the merged tree.
		// 2026-11-03 (S57 T9): + the gate family (9 names, VG19) — six
		// reads (verify_list, verify_show, verify_runs, mergerule_show,
		// mergerule_preview, dod_list) and the three propose-only twins
		// (verify_propose_add, mergerule_propose_tighten, dod_mark) that
		// file INERT proposals or the loop's own DoD ledger (TD2/TD3);
		// registered and granted to NO ONE; the nine human gate writes
		// are never tools (VG17). Count 72 -> 81, measured on the merged
		// tree (on top of S62 T8's export_sets).
		// 2026-11-03 (S48 T10): + blueprint_list, blueprint_show,
		// blueprint_validate, blueprint_lint, blueprint_propose — the
		// blueprint family's four reads and its ONE propose twin (BP9: an
		// agent proposes, a person commits; there is deliberately no save/
		// duplicate/delete/revert tool, pinned by S48 T8's guard); Count
		// 81 -> 86 on this merged tree.
		// 2026-11-04 (S51 T5, TM6, row written at T6's sweep): + bug_list,
		// bug_show, bug_file — the bug family's two reads and its ONE
		// detectable filing twin (an agent files an UNVALIDATED claim
		// filed_by its own slug, capped at eight open; validation,
		// priority, duplicate and handoff are not nameable here — TM6/TM2;
		// attribution row lives in cmd/aeon/tools_guard_test.go). Count
		// 104 -> 107 DELIBERATE (+3), measured on this tree.
		// The 86 -> 104 leg is drift this file never re-ran: sibling
		// landings measured their own trees and bumped the attribution
		// table in cmd/aeon/tools_guard_test.go but not this pin. The
		// drift is recorded here rather than silently, which is the whole
		// purpose of the sentence this check fails with; the names stay
		// attributed where they were claimed, and TestRegistryCount-
		// IsExactlyAccountedFor passes on this tree.
		t.Fatalf("the tool listing changed size (%d) — the registry grows only by named, dated landings (see the block above); nothing joins silently: %v", len(names), names)
	}
	for n := range tools.Registry() {
		byName[n] = true
	}
	for _, sk := range card.Skills {
		if byName[sk.ID] || byName[sk.Name] {
			t.Fatalf("AS3 violated: skill %q is in the tool registry", sk.ID)
		}
	}
}

// TestExamplesRoundTripForDiscovery — examples is what a discovering agent
// reads to decide whether a skill fits (T9). It survives the signed-card
// round trip and is reachable through the lookup this package will offer.
func TestExamplesRoundTripForDiscovery(t *testing.T) {
	card := cardWith(t, oneSkill())
	sk, ok := card.FindSkill("summarise-sentiment")
	if !ok {
		t.Fatal("FindSkill lost the skill the card carries")
	}
	if len(sk.Examples) != 1 || !strings.Contains(sk.Examples[0], "aeon") {
		t.Fatalf("examples lost: %#v", sk.Examples)
	}
	raw, err := json.Marshal(sk)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "examples") {
		t.Fatalf("examples must be readable over the wire too (T9 discovery): %s", raw)
	}
}
