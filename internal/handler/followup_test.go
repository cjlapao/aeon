package handler

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

// The follow-up five (T12–T17, added 2026-10-26): kinds S13 named and never
// shipped, whose lanes sit `cannot verify` in shipped blueprints. Each task
// ships one body; these are the tests that were written first.

// followupOriginals are the opencode/agents primaries being adapted — named
// explicitly, not globbed: a glob would silently widen when someone adds a
// document, and the negative control must fail closed on exactly these.
var followupOriginals = []string{"task-coordinator", "proposal-author", "brief-author", "product-manager", "project-manager"}

// TestVerifyFailsOnTheFollowUpOriginals is D1 applied to the five sources.
// Measured 2026-10-26 (probe, then asserted): findings per source are
// task-coordinator 6, proposal-author 5, brief-author 4, product-manager 3,
// project-manager 3 — kinds span forbidden-tool, ritual, skill, and
// description. The floors (≥3, and a skill finding and a ritual finding
// each) are what the probe measured; a rewrite that "forgets" to trip the
// guard has copied, and a guard that stops reporting has stopped measuring.
func TestVerifyFailsOnTheFollowUpOriginals(t *testing.T) {
	for _, name := range followupOriginals {
		f := filepath.Join(originalsDir(t), name+".md")
		fs, rej := VerifyFile(f, true, nil)
		if rej != nil {
			t.Fatalf("%s: original must load (facts, not errors): %v", name, rej)
		}
		if len(fs) < 3 {
			t.Fatalf("%s: only %d findings — verify is not measuring the original (D1)", name, len(fs))
		}
		var kinds []string
		for _, fi := range fs {
			kinds = append(kinds, fi.Kind)
		}
		joined := strings.Join(kinds, ",")
		if !strings.Contains(joined, "skill") || !strings.Contains(joined, "ritual") {
			t.Fatalf("%s: findings %v — the skill and ritual refusals are the ones that must not be ported; their absence here means the guard is not seeing the source", name, kinds)
		}
	}
}

func TestTaskCoordinatorShipsVerifyCleanAndRegistered(t *testing.T) {
	d, rej := LoadDefinition("task-coordinator")
	if rej != nil {
		t.Fatalf("task-coordinator must load as a shipped body: %v", rej)
	}
	if fs := Verify(d, nil); len(fs) > 0 {
		for _, f := range fs {
			t.Errorf("%s", f.String())
		}
	}
	if _, ok := Build("task-coordinator", "triage the request"); !ok {
		t.Fatal("task-coordinator is not registered — the body must be IN the registry, not merely on disk")
	}
	// Read-only by design (boundaries L23–34 of the source): the coordinator
	// routes, relays and verifies — it never writes.
	for _, tool := range d.Tools {
		if tool == "write" || tool == "edit" || tool == "bash" {
			t.Fatalf("task-coordinator carries %q — its boundaries forbid it", tool)
		}
	}
}

func TestTaskCoordinatorTriageRunOnce(t *testing.T) {
	res, _ := dispatchWithRequired(t, "task-coordinator",
		"the dedup check is missing when we save to DB — fix it",
		[]string{"mode", "rationale", "brief"},
		t4Turn{Text: "Triage: Direct\n\n```json\n{\"status\":\"ok\",\"summary\":\"task-coordinator triaged the dedup bug as Direct\",\"mode\":\"direct\",\"rationale\":\"single-domain bug fix; scope clear\",\"brief\":\"Context: DB save path lacks the dedup guard. Ask: add the guard, mirror the existing check in the sibling save path. Non-goals: no schema change.\"}\n```"},
	)
	mustOK(t, res, "task-coordinator")
	// The four-mode triage survives as the OUTCOME (handoff), not a file —
	// the source's Brief lives inline in the delegation (L27, L200–214).
	if got := strings.ToLower(strings.TrimSpace(stringOf((*res.Handoff)["mode"]))); got != "direct" {
		t.Fatalf("mode = %v, want direct — triage must arrive as a handoff outcome", (*res.Handoff)["mode"])
	}
	if brief := strings.TrimSpace(stringOf((*res.Handoff)["brief"])); !strings.Contains(strings.ToLower(brief), "dedup") {
		t.Fatalf("brief = %q, want the inline Brief the Direct lane delegates on", brief)
	}
}

func stringOf(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}

// ── T13: proposal-author ────────────────────────────────────────────────────

func TestProposalAuthorShipsVerifyCleanAndRegistered(t *testing.T) {
	d, rej := LoadDefinition("proposal-author")
	if rej != nil {
		t.Fatalf("proposal-author must load as a shipped body: %v", rej)
	}
	if fs := Verify(d, nil); len(fs) > 0 {
		for _, f := range fs {
			t.Errorf("%s", f.String())
		}
	}
	if _, ok := Build("proposal-author", "turn the upload idea into a proposal"); !ok {
		t.Fatal("proposal-author is not registered — the body must be IN the registry")
	}
	// The corpus binds: internal/policy/testdata/corpus.json carries a
	// proposal-author block (measured by name 2026-10-26) — unlike the three
	// nil-recorded follow-ups, this one DECLARES its policy-agent.
	if d.PolicyAgent != "proposal-author" {
		t.Fatalf("policy-agent = %q, want proposal-author — the corpus block exists and must bind", d.PolicyAgent)
	}
	if len(d.CommandRules) == 0 {
		t.Fatal("the corpus block carries rules; CommandRules is empty — the binding did not reach the handler")
	}
	// The interview is this agent's core instrument (source L25): without
	// question there is no proposal-author.
	var asked bool
	for _, tool := range d.Tools {
		if tool == "question" {
			asked = true
		}
	}
	if !asked {
		t.Fatal("question withheld — L25 names it the core instrument; the waves cannot run without it")
	}
}

func TestProposalAuthorDraftRunOnce(t *testing.T) {
	res, _ := dispatchWithRequired(t, "proposal-author",
		"turn the file-upload idea into a proposal",
		[]string{"sections", "near_miss", "effort"},
		t4Turn{ToolUses: []model.Block{writeBlock("proposal.md", "proposal-author: upload capability\n")}},
		t4Turn{Text: "```json\n{\"status\":\"ok\",\"summary\":\"proposal-author wrote the upload proposal after three waves and a verified exists-today section\",\"sections\":\"why-it-matters quotes the support burden; what-exists-today opens every file named; proposed-shape is one capability; risks names the migration cost\",\"near_miss\":\"media-service already advertises uploads behind an unread config flag — nobody consumes it today\",\"effort\":\"M\"}\n```"},
	)
	mustOK(t, res, "proposal-author")
	h := *res.Handoff
	if !strings.Contains(strings.ToLower(fmt.Sprint(h["near_miss"])), "media-service") {
		t.Fatalf("near_miss = %v — the near-miss is the highest-leverage finding (L162-163) and must arrive as a handoff field", h["near_miss"])
	}
	if fmt.Sprint(h["effort"]) != "M" {
		t.Fatalf("effort = %v, want the one-letter S/M/L (L234: never an estimate, always a letter)", h["effort"])
	}
}

// ── T14: brief-author ───────────────────────────────────────────────────────

func TestBriefAuthorShipsVerifyCleanAndRegistered(t *testing.T) {
	d, rej := LoadDefinition("brief-author")
	if rej != nil {
		t.Fatalf("brief-author must load as a shipped body: %v", rej)
	}
	if fs := Verify(d, nil); len(fs) > 0 {
		for _, f := range fs {
			t.Errorf("%s", f.String())
		}
	}
	if _, ok := Build("brief-author", "turn the approved upload proposal into a brief"); !ok {
		t.Fatal("brief-author is not registered — the body must be IN the registry")
	}
	if d.PolicyAgent != "brief-author" {
		t.Fatalf("policy-agent = %q, want brief-author — the corpus block exists (measured by name) and must bind", d.PolicyAgent)
	}
	if len(d.CommandRules) == 0 {
		t.Fatal("the corpus block carries rules; CommandRules is empty — the binding did not reach the handler")
	}
	granted := map[string]bool{}
	for _, tool := range d.Tools {
		granted[tool] = true
	}
	if !granted["question"] {
		t.Fatal("question withheld — locked decisions the code cannot settle must be ASKED (L199-L204: listing them in the reply does not ask anyone)")
	}
	if granted["webfetch"] {
		t.Fatal("webfetch granted against the original's explicit false — L26: the brief is grounded in this repo, not the web")
	}
}

func TestBriefAuthorBriefRunOnce(t *testing.T) {
	res, _ := dispatchWithRequired(t, "brief-author",
		"the proposal at proposal.md is approved — write the implementation brief",
		[]string{"overrides", "locked_decisions", "open_questions", "selfcheck"},
		t4Turn{ToolUses: []model.Block{writeBlock("brief.md", "brief-author: upload capability\n§7 Overrides\n§13 self-check passed\n")}},
		t4Turn{Text: "```json\n{\"status\":\"ok\",\"summary\":\"brief-author wrote the upload brief; two overrides, one locked decision asked, two open §9.4 questions\",\"overrides\":\"2: (1) proposal says media-store validates types — it only sniffs extensions (media/store.go:88); (2) proposal says queue ships single-firing — pinned docs say concurrent-safe only\",\"locked_decisions\":\"migration posture: clean cut — asked via question, user chose it with the re-authentication cost\",\"open_questions\":\"how does the upload path achieve deduplication across retries — open form, no shortlist\",\"selfcheck\":\"A–F pass; A6 names the production call site for each new interface method; C6 enumerates all 9 contract rows with their asserted outcomes\"}\n```"},
	)
	mustOK(t, res, "brief-author")
	h := *res.Handoff
	if !strings.Contains(fmt.Sprint(h["overrides"]), "media-store") {
		t.Fatalf("overrides = %v — an override without its evidence (L234-L235) is just disagreement", h["overrides"])
	}
	if !strings.Contains(strings.ToLower(fmt.Sprint(h["open_questions"])), "no shortlist") {
		t.Fatalf("open_questions = %v — §9.4 questions must be open form (L172-L175: naming a shortlist tells the planner where not to look)", h["open_questions"])
	}
}

func TestBriefAuthorSelfCheckRunOnce(t *testing.T) {
	// The self-check lane (blueprint :47): the author runs §13 against its
	// OWN finished brief — a step validating the previous step's product,
	// the same relationship `verify` has to a task. No write here: the fix
	// belongs to a re-run of the brief lane (on_reject goto brief).
	res, _ := dispatchWithRequired(t, "brief-author",
		"run the §13 pre-dispatch self-check against the finished brief at brief.md",
		[]string{"overrides", "locked_decisions", "open_questions", "selfcheck"},
		t4Turn{Text: "```json\n{\"status\":\"ok\",\"summary\":\"brief-author ran §13 against brief.md: two rows failed and were fixed\",\"overrides\":\"unchanged: 2 overrides stand, both evidence-backed\",\"locked_decisions\":\"unchanged\",\"open_questions\":\"unchanged: 2 open-form questions\",\"selfcheck\":\"C6 failed on arrival — two behavioural-contract rows had no named test and were listed as NOT IMPLEMENTED silently omitted; restored the enumeration and quoted the asserted outcomes. A6 passed: every new interface method names a production call site. All rows pass now.\"}\n```"},
	)
	mustOK(t, res, "brief-author")
	h := *res.Handoff
	sc := strings.ToLower(fmt.Sprint(h["selfcheck"]))
	if !strings.Contains(sc, "c6") || !strings.Contains(sc, "pass") {
		t.Fatalf("selfcheck = %v — the honest report names the rows that failed, what changed, and the final state (L106-L110)", h["selfcheck"])
	}
}

// ── T15: product-manager ────────────────────────────────────────────────────

func TestProductManagerShipsVerifyCleanAndRegistered(t *testing.T) {
	d, rej := LoadDefinition("product-manager")
	if rej != nil {
		t.Fatalf("product-manager must load as a shipped body: %v", rej)
	}
	if fs := Verify(d, nil); len(fs) > 0 {
		for _, f := range fs {
			t.Errorf("%s", f.String())
		}
	}
	if _, ok := Build("product-manager", "capture the upload feature request as feature.md"); !ok {
		t.Fatal("product-manager is not registered — the body must be IN the registry")
	}
	// NIL recorded as a finding (N1): product-manager has no corpus block
	// (measured by name among the 38, 2026-10-26) — the body declares no
	// policy-agent, the loader's name-fallback finds nothing, and CommandRules
	// is nil — recorded nil, never an empty slice dressed up as rules.
	if d.PolicyAgent != "" {
		t.Fatalf("policy-agent = %q declared for a name with no corpus block — a binding to nothing is the fiction the nil rule refuses", d.PolicyAgent)
	}
	if d.CommandRules != nil {
		t.Fatalf("CommandRules = %v, want nil — nil is recorded; absent is invented", d.CommandRules)
	}
	for _, tool := range d.Tools {
		if tool == "bash" || tool == "webfetch" {
			t.Fatalf("product-manager carries %q — the feature.md producer reads, asks, and writes; the original's gh/bash world is BECAME MECHANISM (drop record), and webfetch was dropped precisely because no body instruction names it", tool)
		}
	}
}

func TestProductManagerFeatureRunOnce(t *testing.T) {
	res, _ := dispatchWithRequired(t, "product-manager",
		"capture this as the feature document: users want file uploads up to 25MB with progress",
		[]string{"slug", "ac_count", "dod", "questions_asked"},
		t4Turn{ToolUses: []model.Block{writeBlock("feature.md", "product-manager: ### Description\n### User Story\n### Acceptance Criteria\n- [ ] `POST /upload` returns 201 with the file URL when a multipart upload succeeds\n### Definition of Done\n- [ ] Code implemented following best practices.\n- [ ] Unit tests written and passing.\n- [ ] Manual verification completed.\n")}},
		t4Turn{Text: "```json\n{\"status\":\"ok\",\"summary\":\"product-manager wrote feature.md for user-upload: 6 AC, defaults intact, one scope question asked and answered\",\"slug\":\"user-upload\",\"ac_count\":\"6\",\"dod\":\"defaults intact — code-follows-best-practices, unit tests passing, manual verification — plus 1 added: uploads over 25MB rejected with a stated error\",\"questions_asked\":\"1: platform target (web/mobile/both) — answered 'web only' via the question tool\"}\n```"},
	)
	mustOK(t, res, "product-manager")
	h := *res.Handoff
	if !strings.Contains(strings.ToLower(fmt.Sprint(h["dod"])), "defaults intact") {
		t.Fatalf("dod = %v — the three standard items are never subtractable (L229); the handoff says so explicitly", h["dod"])
	}
	if !strings.Contains(fmt.Sprint(h["questions_asked"]), "question tool") && !strings.Contains(fmt.Sprint(h["questions_asked"]), "none") {
		t.Fatalf("questions_asked = %v — silence is not consent (L225) and an absent list reads as an unfinished check", h["questions_asked"])
	}
}

// ── T16: project-manager ────────────────────────────────────────────────────

func TestProjectManagerShipsVerifyCleanAndRegistered(t *testing.T) {
	d, rej := LoadDefinition("project-manager")
	if rej != nil {
		t.Fatalf("project-manager must load as a shipped body: %v", rej)
	}
	if fs := Verify(d, nil); len(fs) > 0 {
		for _, f := range fs {
			t.Errorf("%s", f.String())
		}
	}
	if _, ok := Build("project-manager", "break the feature at feature.md into tasks"); !ok {
		t.Fatal("project-manager is not registered — the body must be IN the registry")
	}
	// NIL recorded as a finding (N1), third of the five: project-manager has
	// no corpus block (measured by name among the 38, 2026-10-26).
	if d.PolicyAgent != "" || d.CommandRules != nil {
		t.Fatalf("policy-agent=%q rules=%v — no corpus block exists for this name; nil is recorded, anything else is invented", d.PolicyAgent, d.CommandRules)
	}
	for _, tool := range d.Tools {
		if tool == "bash" || tool == "task" {
			t.Fatalf("project-manager carries %q — Task Issues became task FILES the engine declares; the gh/delegation world is BECAME MECHANISM (drop record)", tool)
		}
	}
	if !containsString(d.Tools, "question") {
		t.Fatal("question withheld — a scope too ambiguous to decompose must be ASKED, never papered over (L153)")
	}
}

func TestProjectManagerDecomposeRunOnce(t *testing.T) {
	res, _ := dispatchWithRequired(t, "project-manager",
		"break the feature document at feature.md into strictly sequential task files under tasks/",
		[]string{"task_count", "task_files", "specialists", "depends"},
		t4Turn{ToolUses: []model.Block{
			writeBlock("tasks/task-01-store.md", "project-manager: [Task 1] upload store\nAssigned Specialist: golang-engineer\nDepends on: none\n"),
			writeBlock("tasks/task-02-routes.md", "project-manager: [Task 2] upload routes\nAssigned Specialist: golang-engineer\nDepends on: task-01-store\n"),
		}},
		t4Turn{Text: "```json\n{\"status\":\"ok\",\"summary\":\"project-manager decomposed the upload feature into two sequential tasks\",\"task_count\":\"2\",\"task_files\":\"tasks/task-01-store.md, tasks/task-02-routes.md\",\"specialists\":\"task-01: golang-engineer; task-02: golang-engineer — both end-to-end single-specialist\",\"depends\":\"task-02 depends on task-01 — declared, never inferred\"}\n```"},
	)
	mustOK(t, res, "project-manager")
	h := *res.Handoff
	if !strings.Contains(fmt.Sprint(h["depends"]), "task-01") {
		t.Fatalf("depends = %v — omitting a real dependency is the named mistake (L187: parallel execution will fail; always declare)", h["depends"])
	}
	if !strings.Contains(fmt.Sprint(h["specialists"]), "golang-engineer") {
		t.Fatalf("specialists = %v — every task carries a canonical name from the registry roster; unassigned is never assigned (L186)", h["specialists"])
	}
}

func containsString(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
