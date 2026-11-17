package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/policy"
)

// T7's tests. **The only safety boundary in this slice**, so the important one
// proves refusal BY ATTEMPT: the command is `touch`, and the test looks for the
// file.
//
// "The toolset map does not contain bash" is a much weaker claim. It passes
// against an implementation that builds the tool anyway and checks a flag
// somewhere else.

func TestAStepGetsExactlyTheToolsItWasGranted(t *testing.T) {
	root := workspace(t, nil)

	set, err := BuildToolset([]string{"read"}, ToolContext{WorkspaceRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if !set.Has("read") {
		t.Error("read was granted and is missing")
	}
	if set.Has("bash") {
		t.Error("bash was not granted and is present")
	}
	if set.Has("edit") {
		t.Error("edit was not granted and is present")
	}

	// And the model is told exactly that, because a spec list that disagrees
	// with the toolset is a model calling a tool that does not exist.
	specs := set.Specs()
	if len(specs) != 1 || specs[0].Name != "read" {
		t.Errorf("the model is offered %d tools: %+v", len(specs), specs)
	}
}

func TestAskingForAnUngrantedBashCannotCreateAProcess(t *testing.T) {
	// D1, proven by attempt. The model asks for `bash` with a command whose
	// only effect would be to create a file; the test looks for the file.
	//
	// A refusal that is checked rather than structural would pass the "no such
	// tool" assertion and still have run the command — so the sentinel file is
	// the assertion that matters, and the message is the secondary one.
	root := workspace(t, nil)
	sentinel := filepath.Join(root, "PROOF-BASH-RAN")

	set, err := BuildToolset([]string{"read"}, ToolContext{WorkspaceRoot: root})
	if err != nil {
		t.Fatal(err)
	}

	res := set.Call(context.Background(), "bash",
		json.RawMessage(`{"command":"touch PROOF-BASH-RAN"}`))

	if _, err := os.Stat(sentinel); err == nil {
		t.Fatal("THE COMMAND RAN. A step that was granted only `read` executed a shell command — " +
			"the grant list is not the boundary it is documented to be")
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat the sentinel: %v", err)
	}

	if !res.IsError {
		t.Fatalf("the call was not an error: %s", res.Content)
	}
	// O6: the message names what IS available, because "unknown tool" on its
	// own leaves a model guessing — and a model that guesses tries the same
	// name again.
	if !strings.Contains(res.Content, "read") {
		t.Errorf("the message does not name what is available: %s", res.Content)
	}
	if !strings.Contains(res.Content, "bash") {
		t.Errorf("the message does not name what was asked for: %s", res.Content)
	}
}

func TestAGrantedBashCanCreateAProcessWhichIsTheControl(t *testing.T) {
	// The negative control on the test above. Without it, the refusal test
	// passes against a `Call` that refuses everything — including the tools a
	// step was granted.
	root := workspace(t, nil)
	sentinel := filepath.Join(root, "PROOF-BASH-RAN")

	set, err := BuildToolset([]string{"read", "bash"}, ToolContext{WorkspaceRoot: root, CommandVerdict: allowAllSeam(t), Exec: testExecSeam(t)})
	if err != nil {
		t.Fatal(err)
	}

	res := set.Call(context.Background(), "bash",
		json.RawMessage(`{"command":"touch PROOF-BASH-RAN"}`))
	if res.IsError {
		t.Fatalf("a granted bash was refused: %s", res.Content)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("a granted bash did not run the command: %v", err)
	}
}

func TestBashExistsInTheRegistryAndIsInNoDefaultGrant(t *testing.T) {
	// Two different facts, deliberately separate: the registry says the tool
	// EXISTS, the grant list says this step MAY CALL it. Collapsing them is
	// how a shell ends up granted by default.
	if _, exists := Registry()["bash"]; !exists {
		t.Error("bash is not in the registry; a step that wants it could not be granted it")
	}

	granted := DefaultGrant()
	if strings.Join(granted, ",") != "read,list,write" {
		t.Errorf("the default grant is %v, want read, list, write", granted)
	}
	for _, name := range granted {
		if name == "bash" {
			t.Fatal("bash is granted by default — every example in every document would be an " +
				"invitation, and the person running it would not have chosen anything")
		}
	}
}

func TestTheRegistryContainsTheWorkspaceFileTools(t *testing.T) {
	// W11. A third tool arriving without a decision is how a slice's surface
	// grows past what its tests cover.
	names := ToolNames()
	// W11. A third tool arriving without a decision is how a slice's surface
	// grows past what its tests cover. S11 T14 added `write` and `edit`
	// (D3's honest ten); T18's rename is what made the ten EXACT — see
	// TestWriteFileAliasStillResolves for the one hidden key this listing
	// must NOT show. S11 T15 added `glob` and `grep` (the two search tools,
	// pure reads behind the one resolver); T16 lands `webfetch` (the one
	// egress, GET-only, registered but NOT default-granted); T17 lands
	// `todo` (the within-step checklist — W20: transcript-only state, never
	// rows). This list IS the decision the W11 comment asks for — a tool
	// arriving without a line here is the growth this test exists to stop.
	// The name of this test predates T15/T16: the list is registry-wide, not
	// workspace-file-only, and says so here until cleanup renames it.
	// 2026-10-27 (S38 T9): added the management catalog (23 names) — OL2's single
	// vocabulary, projected from internal/records; registry is now the honest 39.
	// 2026-10-27 (S38 T10): + task family (6 names) — OL14's scoped task surface,
	// the ladder reached THROUGH the machine; Count 10 -> 33 -> 39 DELIBERATE.
	// 2026-10-28 (S39 T10): + run_start — the one loop action an agent may
	// take, behind an ask row the OPERATOR adds (CR5); Count 39 -> 40 DELIBERATE.
	// 2026-10-31 (S44-extra CT9): the component family is the nine tree
	// operations; the four project tools go, five component tools join —
	// Count 41 -> 42 DELIBERATE (S44-extra CT9, 2026-10-31).
	// 2026-10-31 (S43-extra T6): + seed_status — the ONE seed tool, a read
	// projected from the records catalog (family seed, safe, not mutating);
	// registered, never default-granted.
	// 2026-11-02 (S46 T8): + the five mcp own-view doors (mcp_servers,
	// mcp_grants, mcp_status, mcp_propose_remove_server, mcp_propose_revoke_grant)
	// — the catalog's own, projected by the loop that has never taken a
	// hand-written entry: three reads and the two narrowing proposes (MG9).
	// REGISTERED AND GRANTED TO NO ONE — DefaultGrant does not name them;
	// the policy tool_access rows judge every call. Count +5 DELIBERATE.
	// 2026-10-31 (S70 MB1): + ask_agent — the agent↔agent ask, registered and
	// NEVER granted (S11's rule inherited whole: a loop's agents cannot talk
	// to each other until an operator grants tool_access).
	// Both slices landed a tool the same day; Count 42 -> 44 DELIBERATE.
	// 2026-11-01 (S45 T7): + the policy/proposal family (7 names) — PM9's
	// seven: five reads (policy_list, policy_show, policy_why, policy_in_force,
	// proposal_list) and the two propose-only twins (policy_propose_add,
	// policy_propose_remove), which create INERT rows a human decides. All
	// seven registered, granted to NO ONE by default (DefaultGrant unchanged
	// at read, list, write); Count 44 -> 51 DELIBERATE.
	// 2026-11-01 (S56 T8): + the autonomy family (5 names) — AL12's five:
	// three reads (autonomy_list, autonomy_show, autonomy_effective) and the
	// two RESTRICTIVE propose-only twins (autonomy_propose_revoke,
	// autonomy_propose_deny), which create INERT rows a human commits; there
	// is no grant tool. All five registered, granted to NO ONE by default
	// (DefaultGrant unchanged at read, list, write); Count 51 -> 56 DELIBERATE.
	// 2026-11-02 (S55 merge): S55 T10's seven loop-control / budget /
	// watchdog agent tools (six safe reads + loop_request_stop,
	// detectable) join the merged listing; Count 61 -> 68, measured
	// on the merged tree.
	// 2026-11-02 (S47 T7): + the two own-view tool reads — `tools_mine` and
	// `tool_show` (TC9). They arrive the way every catalog tool arrives, through
	// `recordTools`' projection of `records.Catalog()`, not as hand-written
	// entries: the spec is the definition, its `Method` field is the socket twin
	// (`tools.mine.list`, `tools.mine.show`), and `Mutates` is false on both
	// because TC1 says this slice ships zero write tools. REGISTERED AND GRANTED
	// TO NO ONE — `DefaultGrant` still names `read, list, write` and neither name
	// appears in it; the `tool_access` rows judge every call. Both answer SELF
	// from the stamped step (OL3), so no input can ask about another agent, and a
	// person calling either door is refused by name with the unscoped door that
	// does answer them; Count 68 -> 70 DELIBERATE.
	// 2026-11-02 (S62 T6): + backup_status — the backup family's one read
	// twin over backup.status (family backup, class safe, Mutates:false);
	// run/verify/prune are human doors and restore is never a tool (BK9).
	// Count 70 -> 71 DELIBERATE, measured on the merged tree (S62 T6, 2026-11-02).
	// 2026-11-02 (S62 T8): + export_sets — the export family's read twin
	// over export.sets (family backup, class safe, Mutates:false);
	// export.run is a human door (data leaves the machine) and never a tool.
	// Count 71 -> 72 DELIBERATE, measured on the merged tree (S62 T8, 2026-11-02).
	// 2026-11-03 (S48 T10): + the blueprint family (5 names) — BP9's five:
	// four safe reads (blueprint_list, blueprint_show, blueprint_validate,
	// blueprint_lint) twinned to the person's standing doors, and the ONE
	// propose-only twin (blueprint_propose, class detectable, Mutates:true),
	// which files the INERT blueprint_save row a person commits — there is
	// deliberately NO save/delete/duplicate/revert tool (T8's guard refuses
	// any such name). All five arrive through recordTools' projection of
	// records.Catalog(); all five registered, granted to NO ONE by default
	// (DefaultGrant unchanged at read, list, write); Count 72 -> 77
	// DELIBERATE, measured on the merged tree (S48 T10, 2026-11-03).
	// 2026-11-03 (S57 T9): + the nine gate family names (the same
	// projection TestRegistryExactNames counts); on top of S62 T8's
	// export_sets. Count 72 -> 81 DELIBERATE, measured on the merged tree
	// (S57 T9, 2026-11-03).
	// 2026-11-03 (merge reconciliation): both landings stand — main's 72
	// plus S57 T9's nine gate names plus S48 T10's five blueprint names = 86,
	// measured on THIS merged tree (each side's own arithmetic is above).
	// 2026-11-03 (S58 T6): + the supervision family (5 names) — SV16's
	// five safe reads (inbox_list, inbox_summary, mission_status,
	// notify_class_list, notify_channel_list) projected through
	// recordTools' catalog loop: the three scope verbs answer OWN VIEW
	// (a rootId naming another tree is refused, never answered) and the
	// two notify lists carry no URL path, no secret ref, no subscription.
	// No supervision WRITE is in the catalog — acking is a person's act
	// (SV12). All five registered, granted to NO ONE by default
	// (DefaultGrant unchanged at read, list, write).
	// 2026-11-03 (S59 T7): + the five execution-environment names
	// (environment_list, environment_show, environment_effective,
	// secretref_list, environment_propose_restrict) — four safe reads that
	// carry no value and no reference source (EV7) and the ONE tightening
	// ask, all registered and granted to no one.
	// 2026-11-03 (merge reconciliation): both landings stand — main's 86
	// plus S58 T6's five supervision reads plus S59 T7's five environment
	// names = 96, measured on THIS merged tree (each side's own arithmetic
	// is above; the union is what the two merged registrars actually build).
	// 2026-11-04 (S49 T7): + the automation family (5 names) — AN9's five:
	// four safe reads (automation_list, automation_show, automation_fires,
	// automation_health) twinned to T3's standing doors — the tool answer IS
	// the socket answer (rule 4) — and the ONE ask (automation_propose_disable,
	// class detectable, Mutates:true) filing the INERT automation_disable row;
	// there is deliberately NO create/enable/update/archive/pause/resume/fire-now
	// tool (AU1 lock 1 unedited, T5's guard refuses such a name). All five
	// arrive through recordTools' projection of records.Catalog(); all five
	// registered, granted to NO ONE by default (DefaultGrant unchanged at read,
	// list, write). Count 96 -> 101 DELIBERATE, measured on the merged tree.
	// 2026-11-04 (S50 T5, RM8): + the roadmap family (three names) — two safe
	// reads (roadmap_list twinned to roadmap.board, roadmap_show answered with
	// its trail) and the ONE ask (roadmap_propose_idea, class detectable,
	// Mutates:true) filing the INERT roadmap_idea row; there is deliberately
	// NO update/cancel/estimate/priority/ideate/plan/materialize tool (T4's
	// guard family pins those eight doors human-only). All three arrive
	// through recordTools' projection of records.Catalog(); all three
	// registered, granted to NO ONE by default (DefaultGrant unchanged at
	// read, list, write). Count 101 -> 104 DELIBERATE, measured on the
	// merged tree.

	// S52 T4 (LM5, LM7, LM8): the memory four — learning_query and
	// learning_show (the agent's own readable-set reads), learning_add (the
	// agent's ONE memory write, observed-or-assumed, capped per agent off
	// the audit ledger) and learning_propose_retract (the INERT ask; only a
	// person's commit retracts, and then as the committer). All four arrive
	// through recordTools' projection of records.Catalog(); none is granted
	// by default. Count 109 -> 113 DELIBERATE, measured on this tree.

	// S60 T9 (MQ17, 2026-11-04): the queue plane's five — mergequeue_list,
	// _show, _status (the agent's own-view reads of the machine's merge
	// queue), mergequeue_dequeue (the ONE direct write: this loop's own
	// queued/conflicted item only) and mergequeue_propose_enqueue (the
	// INERT ask; only a person's commit enqueues, unattended, stamped
	// {proposal,<id>}). All five arrive through recordTools' projection of
	// records.Catalog(); none is granted by default.
	// 2026-11-04 (T9 landing merge): S53 T3 history_list/history_show landed
	// from the other side the same day; each side counted its own bump on its
	// own branch (S53 +2, S60 +5); the merged truth is one list —
	// Count 116 + 2 = 118 DELIBERATE, measured on THIS merged tree.
	// 2026-11-04 (S54 T4): + index_status — the platform family's one read
	// twin, arriving through recordTools' projection of records.Catalog().
	// It twins `platform.status` for the STEP'S OWN root only (freshness,
	// never the operator fields — S54 T0's visibility ruling), and there is
	// deliberately NO control tool beside it: no reindex, no rebuild, no
	// graph build/reparse/delete, no proposal kind twinned to any operation
	// (SP3/SP4; S54 T3's guards refuse each such name). Registered and
	// GRANTED TO NO ONE — DefaultGrant still names read, list, write.
	// Count 118 -> 119 DELIBERATE, measured on this merged tree.
	// 2026-11-05 (S63 T6): + the scorecard family (3 names) — RT11's three
	// safe reads (scorecard_mine, scorecard_component, digest_learned),
	// twins of the person's scorecard.agent / scorecard.component /
	// digest.learned doors answering through the SAME services (rule 4),
	// subjects STAMPED from the step (the schemas carry no agent, slug,
	// id or root — a self-chosen subject is a parse error), the component
	// twin dropping the person's per-child breakdown so no tool ranks
	// anyone, and NO write in the family at all (retro.backfill stays the
	// person's door). A measurement, not an instruction (RT3); registered
	// and GRANTED TO NO ONE — DefaultGrant still names read, list, write.
	// Count 119 -> 122 DELIBERATE (S63 T6, 2026-11-05), measured on this merged tree.
	// 2026-11-05 (S27 T3, CN7/P6): + the graph's two edges (roadmap_depend,
	// roadmap_undepend) — the roadmap plane's first WRITE tools, and RM8's
	// amendment stands precise: an edge is INTENT, never a rung. Neither
	// moves a status, priority or estimate, nothing dispatches because an
	// edge appeared (readiness is re-read at every attempt, D4), and the
	// eight LADDER writes stay off this plane whole. Arriving through
	// recordTools' projection of records.Catalog(); GRANTED TO NO ONE —
	// DefaultGrant still names read, list, write.
	// Count 122 -> 124 DELIBERATE (S27 T3, 2026-11-05), measured on this tree.
	// 2026-11-05 (S27 T8, CN23): + the scheduler's four reads (scheduler_status,
	// scheduler_preview, scheduler_decisions, scheduler_attention) — the agent's
	// eyes on the continuous scheduler: the plan one tick ahead, the decision
	// ledger, the open needs-you, all scoped to the step's OWN repository (the
	// root is stamped from the store, MC2 — a foreign repository is absent, not
	// refused with a revealing sentence). Class safe, Mutates false, and the
	// eight CONTROLS are absent by construction: enable, disable, pause, resume,
	// root.set, root.disable, enableTree, stopAll are human doors (S27 CN2;
	// cmd/aeon/continuous_guard_test.go refuses every spelling). Arriving
	// through recordTools' projection of records.Catalog(); GRANTED TO NO ONE —
	// DefaultGrant still names read, list, write.
	// Count 124 -> 128 DELIBERATE (S27 T8, 2026-11-05), measured on this tree.
	// 2026-11-05 (S61 T8, GI17): + the goal-intake plane's seven
	// (charter_list · charter_propose · charter_show · intake_propose ·
	// intake_status · template_list · template_show) — five safe reads and
	// two inert propose asks; registered, never granted. Count +7
	// DELIBERATE, measured on this tree.
	want := []string{"ask_agent", "attach", "attachments_list", "automation_fires", "automation_health", "automation_list", "automation_propose_disable", "automation_show", "autonomy_effective", "autonomy_list", "autonomy_propose_deny", "autonomy_propose_revoke", "autonomy_show", "backup_status", "bash", "blueprint_lint", "blueprint_list", "blueprint_propose", "blueprint_show", "blueprint_validate", "brief_create", "brief_show", "budget_effective", "budget_list", "budget_show", "budget_spend", "bug_file", "bug_list", "bug_show", "charter_list", "charter_propose", "charter_show", "component_archive", "component_attach", "component_create", "component_detach", "component_list", "component_promote", "component_restore", "component_show", "component_update", "digest_learned", "doc_edit", "doc_list", "doc_read", "doc_write", "dod_list", "dod_mark", "edit", "env_get", "env_list", "environment_effective", "environment_list", "environment_propose_restrict", "environment_show", "epic_create", "epic_list", "epic_show", "epic_update", "export_sets", "glob", "goal_create", "goal_list", "goal_show", "goal_submit_review", "goal_update", "grep", "history_list", "history_show", "inbox_list", "inbox_summary", "index_status", "intake_propose", "intake_status", "learning_add", "learning_propose_retract", "learning_query", "learning_show", "list", "loop_context", "loop_control_state", "loop_get", "loop_list", "loop_request_stop", "mcp_grants", "mcp_propose_remove_server", "mcp_propose_revoke_grant", "mcp_servers", "mcp_status", "mergequeue_dequeue", "mergequeue_list", "mergequeue_propose_enqueue", "mergequeue_show", "mergequeue_status", "mergerule_preview", "mergerule_propose_tighten", "mergerule_show", "mission_status", "notify_channel_list", "notify_class_list", "policy_in_force", "policy_list", "policy_propose_add", "policy_propose_remove", "policy_show", "policy_why", "proposal_list", "question", "read", "report_size", "roadmap_depend", "roadmap_list", "roadmap_propose_idea", "roadmap_show", "roadmap_undepend", "run_start", "scheduler_attention", "scheduler_decisions", "scheduler_preview", "scheduler_status", "scorecard_component", "scorecard_mine", "secretref_list", "seed_status", "task_bump_counter", "task_create", "task_list", "task_set_status", "task_show", "task_update", "template_list", "template_show", "todo", "tool_show", "tools_mine", "verify_list", "verify_propose_add", "verify_runs", "verify_show", "watchdog_list", "webfetch", "write"}
	if len(names) != len(want) {
		t.Fatalf("the registry holds %v, want %v", names, want)
	}
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	for i := range want {
		if sorted[i] != want[i] {
			t.Errorf("tool %d is %q, want %q", i, sorted[i], want[i])
		}
	}
}

// TestWriteFileAliasStillResolves pins the D3 compatibility seam: the
// hidden alias is HIDDEN (never listed, never granted by default) but
// ACCEPTED (it builds, it dispatches, it classifies) — pinned bytes are
// immutable, so a blueprint or transcript written before the rename must
// keep working while every authored surface says write. The ONLY other
// place the old name may appear in production code is the aliasWriteFile
// constant itself.
func TestWriteFileAliasStillResolves(t *testing.T) {
	reg := Registry()
	alias, ok := reg[aliasWriteFile]
	if !ok {
		t.Fatal("the hidden alias left the registry — a blueprint pinned before the rename would no longer build")
	}
	canonical, ok := reg["write"]
	if !ok {
		t.Fatal("write is not in the registry")
	}
	if alias.Spec.Name != "write" || alias.Spec.Name != canonical.Spec.Name {
		t.Errorf("the alias key holds a tool named %q, want the same name as write (%q)", alias.Spec.Name, canonical.Spec.Name)
	}
	if alias.Run == nil || reflect.ValueOf(alias.Run).Pointer() != reflect.ValueOf(canonical.Run).Pointer() {
		t.Error("the alias does not answer to the one write body")
	}

	// ACCEPTED: a pinned blueprint naming the old name still builds a
	// toolset, and the call dispatches to the write body.
	root := workspace(t, nil)
	set, err := BuildToolset([]string{aliasWriteFile}, ToolContext{WorkspaceRoot: root})
	if err != nil {
		t.Fatalf("a pinned grant naming the alias was refused at build: %v", err)
	}
	got := set.Call(context.Background(), aliasWriteFile, json.RawMessage(`{"path":"a.txt","content":"hello"}`))
	if got.IsError || !strings.Contains(got.Content, "wrote a.txt (5 bytes)") {
		t.Fatalf("the aliased call: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(root, "a.txt")); err != nil {
		t.Fatalf("the aliased call did not write: %v", err)
	}

	// Both directions agree: the recovery classifier and the grant check
	// see the alias exactly as they see write (a pinned transcript asks for
	// the old name; S7's recovery must classify it, not treat it as unknown).
	if set.RerunClass(aliasWriteFile) != RerunDetectable || canonical.RerunClass() != RerunDetectable {
		t.Errorf("the alias classifies as %q, want %q like the write tool (%q)",
			set.RerunClass(aliasWriteFile), RerunDetectable, canonical.RerunClass())
	}
	if !set.Has(aliasWriteFile) {
		t.Error("the granted alias does not register in Has()")
	}

	// HIDDEN: no listing shows it — the honest ten, not eleven.
	for _, name := range ToolNames() {
		if name == aliasWriteFile {
			t.Errorf("ToolNames() lists the hidden alias — listings must show the honest ten")
		}
	}
	for _, name := range DefaultGrant() {
		if name == aliasWriteFile {
			t.Errorf("DefaultGrant() grants the hidden alias — the grant list is an authored surface")
		}
	}
}

// TestRegistryExactNames is T19's registration audit. After every tool task
// (T14–T17 register; T18 renames) the registry must be EXACTLY the honest
// thirty-nine — a name arriving that nobody counted is a capability nobody decided
// — plus the one hidden alias key, and the default grant must stay what the
// audit says it is: the workspace file tools, no shell, no egress.
func TestRegistryExactNames(t *testing.T) {
	// 2026-10-27 (S38 T9): added the management catalog (23 names) — OL2's single
	// vocabulary, projected from internal/records; registry is now the honest 39.
	// 2026-10-27 (S38 T10): + task family (6 names) — OL14's scoped task surface,
	// the ladder reached THROUGH the machine; Count 10 -> 33 -> 39 DELIBERATE.
	// 2026-10-28 (S39 T10): + run_start — the one loop action an agent may
	// take, behind an ask row the OPERATOR adds (CR5); Count 39 -> 40 DELIBERATE.
	// 2026-10-31 (S44-extra CT9): the component family is the nine tree
	// operations; the four project tools go, five component tools join —
	// Count 41 -> 42 DELIBERATE (S44-extra CT9, 2026-10-31).
	// 2026-10-31 (S43-extra T6): + seed_status — the ONE seed tool, a read
	// projected from the records catalog (family seed, safe, not mutating);
	// registered, never default-granted.
	// 2026-11-02 (S46 T8): + the five mcp own-view doors (mcp_servers,
	// mcp_grants, mcp_status, mcp_propose_remove_server, mcp_propose_revoke_grant)
	// — the catalog's own, projected by the loop that has never taken a
	// hand-written entry: three reads and the two narrowing proposes (MG9).
	// REGISTERED AND GRANTED TO NO ONE — DefaultGrant does not name them;
	// the policy tool_access rows judge every call. Count +5 DELIBERATE.
	// 2026-10-31 (S70 MB1): + ask_agent — the agent↔agent ask, registered and
	// NEVER granted (S11's rule inherited whole: a loop's agents cannot talk
	// to each other until an operator grants tool_access).
	// Both slices landed a tool the same day; Count 42 -> 44 DELIBERATE.
	// 2026-11-01 (S45 T7): + the policy/proposal family (7 names) — PM9's
	// seven: five reads (policy_list, policy_show, policy_why, policy_in_force,
	// proposal_list) and the two propose-only twins (policy_propose_add,
	// policy_propose_remove), which create INERT rows a human decides. All
	// seven registered, granted to NO ONE by default (DefaultGrant unchanged
	// at read, list, write); Count 44 -> 51 DELIBERATE.
	// 2026-11-01 (S56 T8): + the autonomy family (5 names) — AL12's five:
	// three reads (autonomy_list, autonomy_show, autonomy_effective) and the
	// two RESTRICTIVE propose-only twins (autonomy_propose_revoke,
	// autonomy_propose_deny), which create INERT rows a human commits; there
	// is no grant tool. All five registered, granted to NO ONE by default
	// (DefaultGrant unchanged at read, list, write); Count 51 -> 56 DELIBERATE.
	// 2026-11-02 (S55 merge): S55 T10's seven loop-control / budget /
	// watchdog agent tools (six safe reads + loop_request_stop,
	// detectable) join the merged listing; Count 61 -> 68, measured
	// on the merged tree.
	// 2026-11-02 (S47 T7): + the two own-view tool reads — `tools_mine` and
	// `tool_show` (TC9). They arrive the way every catalog tool arrives, through
	// `recordTools`' projection of `records.Catalog()`, not as hand-written
	// entries: the spec is the definition, its `Method` field is the socket twin
	// (`tools.mine.list`, `tools.mine.show`), and `Mutates` is false on both
	// because TC1 says this slice ships zero write tools. REGISTERED AND GRANTED
	// TO NO ONE — `DefaultGrant` still names `read, list, write` and neither name
	// appears in it; the `tool_access` rows judge every call. Both answer SELF
	// from the stamped step (OL3), so no input can ask about another agent, and a
	// person calling either door is refused by name with the unscoped door that
	// does answer them; Count 68 -> 70 DELIBERATE.
	// 2026-11-02 (S62 T6): + backup_status — the backup family's one read
	// twin over backup.status (family backup, class safe, Mutates:false);
	// run/verify/prune are human doors and restore is never a tool (BK9).
	// Count 70 -> 71 DELIBERATE, measured on the merged tree (S62 T6, 2026-11-02).
	// 2026-11-02 (S62 T8): + export_sets — the export family's read twin
	// over export.sets (family backup, class safe, Mutates:false);
	// export.run is a human door (data leaves the machine) and never a tool.
	// Count 71 -> 72 DELIBERATE, measured on the merged tree (S62 T8, 2026-11-02).
	// 2026-11-03 (S48 T10): + the blueprint family (5 names) — BP9's five:
	// four safe reads (blueprint_list, blueprint_show, blueprint_validate,
	// blueprint_lint) twinned to the person's standing doors, and the ONE
	// propose-only twin (blueprint_propose, class detectable, Mutates:true),
	// which files the INERT blueprint_save row a person commits — there is
	// deliberately NO save/delete/duplicate/revert tool (T8's guard refuses
	// any such name). All five arrive through recordTools' projection of
	// records.Catalog(); all five registered, granted to NO ONE by default
	// (DefaultGrant unchanged at read, list, write); Count 72 -> 77
	// DELIBERATE, measured on the merged tree (S48 T10, 2026-11-03).
	// 2026-11-03 (S57 T9): + the nine gate family names — the catalog's
	// nine gate specs projected by recordTools' catalog projection, not
	// hand-written entries: six safe reads (verify_list, verify_show,
	// verify_runs, mergerule_show, mergerule_preview, dod_list) over T8's
	// gate seams with the scope stamped from the step (OL3), and the three
	// permitted mutating specs (verify_propose_add,
	// mergerule_propose_tighten, dod_mark) — the two proposes file INERT
	// rows through proposal.create's shared path (TD3) and the mark is the
	// loop's own ledger. All nine registered and GRANTED TO NO ONE —
	// DefaultGrant is still read, list, write, and the tool_access rows
	// judge every call (S57 T9's ruling); on top of S62 T8's export_sets.
	// Count 72 -> 81 DELIBERATE, measured on the merged tree (S57 T9, 2026-11-03).
	// 2026-11-03 (merge reconciliation): both landings stand — main's 72
	// plus S57 T9's nine gate names plus S48 T10's five blueprint names = 86,
	// measured on THIS merged tree (each side's own arithmetic is above).
	// 2026-11-03 (S58 T6 + S59 T7, merged): the same union the registry test
	// above pins — supervision's five safe reads and the environment plane's
	// five names join through recordTools' catalog projection; all ten
	// registered and GRANTED TO NO ONE (DefaultGrant is still read, list,
	// write). Count 86 -> 96 DELIBERATE (+5 +5), MEASURED on this merged
	// tree, 2026-11-03.

	// S52 T4 (LM5, LM7, LM8): the memory four — learning_query and
	// learning_show (the agent's own readable-set reads), learning_add (the
	// agent's ONE memory write, observed-or-assumed, capped per agent off
	// the audit ledger) and learning_propose_retract (the INERT ask; only a
	// person's commit retracts, and then as the committer). All four arrive
	// through recordTools' projection of records.Catalog(); none is granted
	// by default. Count 109 -> 113 DELIBERATE, measured on this tree.

	// S60 T9 (MQ17, 2026-11-04): the queue plane's five — mergequeue_list,
	// _show, _status (the agent's own-view reads of the machine's merge
	// queue), mergequeue_dequeue (the ONE direct write: this loop's own
	// queued/conflicted item only) and mergequeue_propose_enqueue (the
	// INERT ask; only a person's commit enqueues, unattended, stamped
	// {proposal,<id>}). All five arrive through recordTools' projection of
	// records.Catalog(); none is granted by default.
	// 2026-11-04 (T9 landing merge): S53 T3 history_list/history_show landed
	// from the other side the same day; each side counted its own bump on its
	// own branch (S53 +2, S60 +5); the merged truth is one list —
	// Count 116 + 2 = 118 DELIBERATE, measured on THIS merged tree.
	// 2026-11-04 (S54 T4): + index_status — the platform family's one read
	// twin, arriving through recordTools' projection of records.Catalog().
	// It twins `platform.status` for the STEP'S OWN root only (freshness,
	// never the operator fields — S54 T0's visibility ruling), and there is
	// deliberately NO control tool beside it: no reindex, no rebuild, no
	// graph build/reparse/delete, no proposal kind twinned to any operation
	// (SP3/SP4; S54 T3's guards refuse each such name). Registered and
	// GRANTED TO NO ONE — DefaultGrant still names read, list, write.
	// Count 118 -> 119 DELIBERATE, measured on this merged tree.
	// 2026-11-05 (S63 T6): + the scorecard family (3 names) — RT11's three
	// safe reads (scorecard_mine, scorecard_component, digest_learned),
	// twins of the person's scorecard.agent / scorecard.component /
	// digest.learned doors answering through the SAME services (rule 4),
	// subjects STAMPED from the step (the schemas carry no agent, slug,
	// id or root — a self-chosen subject is a parse error), the component
	// twin dropping the person's per-child breakdown so no tool ranks
	// anyone, and NO write in the family at all (retro.backfill stays the
	// person's door). A measurement, not an instruction (RT3); registered
	// and GRANTED TO NO ONE — DefaultGrant still names read, list, write.
	// Count 119 -> 122 DELIBERATE (S63 T6, 2026-11-05), measured on this merged tree.
	// 2026-11-05 (S27 T3, CN7/P6): + the graph's two edges (roadmap_depend,
	// roadmap_undepend) — the roadmap plane's first WRITE tools, and RM8's
	// amendment stands precise: an edge is INTENT, never a rung. Neither
	// moves a status, priority or estimate, nothing dispatches because an
	// edge appeared (readiness is re-read at every attempt, D4), and the
	// eight LADDER writes stay off this plane whole. Arriving through
	// recordTools' projection of records.Catalog(); GRANTED TO NO ONE —
	// DefaultGrant still names read, list, write.
	// Count 122 -> 124 DELIBERATE (S27 T3, 2026-11-05), measured on this tree.
	// 2026-11-05 (S27 T8, CN23): + the scheduler's four reads (scheduler_status,
	// scheduler_preview, scheduler_decisions, scheduler_attention) — the agent's
	// eyes on the continuous scheduler: the plan one tick ahead, the decision
	// ledger, the open needs-you, all scoped to the step's OWN repository (the
	// root is stamped from the store, MC2 — a foreign repository is absent, not
	// refused with a revealing sentence). Class safe, Mutates false, and the
	// eight CONTROLS are absent by construction: enable, disable, pause, resume,
	// root.set, root.disable, enableTree, stopAll are human doors (S27 CN2;
	// cmd/aeon/continuous_guard_test.go refuses every spelling). Arriving
	// through recordTools' projection of records.Catalog(); GRANTED TO NO ONE —
	// DefaultGrant still names read, list, write.
	// Count 124 -> 128 DELIBERATE (S27 T8, 2026-11-05), measured on this tree.
	// 2026-11-05 (S61 T8, GI17): + the goal-intake plane's seven
	// (charter_list · charter_propose · charter_show · intake_propose ·
	// intake_status · template_list · template_show) — five safe reads and
	// two inert propose asks; registered, never granted. Count +7
	// DELIBERATE, measured on this tree.
	want := []string{"ask_agent", "attach", "attachments_list", "automation_fires", "automation_health", "automation_list", "automation_propose_disable", "automation_show", "autonomy_effective", "autonomy_list", "autonomy_propose_deny", "autonomy_propose_revoke", "autonomy_show", "backup_status", "bash", "blueprint_lint", "blueprint_list", "blueprint_propose", "blueprint_show", "blueprint_validate", "brief_create", "brief_show", "budget_effective", "budget_list", "budget_show", "budget_spend", "bug_file", "bug_list", "bug_show", "charter_list", "charter_propose", "charter_show", "component_archive", "component_attach", "component_create", "component_detach", "component_list", "component_promote", "component_restore", "component_show", "component_update", "digest_learned", "doc_edit", "doc_list", "doc_read", "doc_write", "dod_list", "dod_mark", "edit", "env_get", "env_list", "environment_effective", "environment_list", "environment_propose_restrict", "environment_show", "epic_create", "epic_list", "epic_show", "epic_update", "export_sets", "glob", "goal_create", "goal_list", "goal_show", "goal_submit_review", "goal_update", "grep", "history_list", "history_show", "inbox_list", "inbox_summary", "index_status", "intake_propose", "intake_status", "learning_add", "learning_propose_retract", "learning_query", "learning_show", "list", "loop_context", "loop_control_state", "loop_get", "loop_list", "loop_request_stop", "mcp_grants", "mcp_propose_remove_server", "mcp_propose_revoke_grant", "mcp_servers", "mcp_status", "mergequeue_dequeue", "mergequeue_list", "mergequeue_propose_enqueue", "mergequeue_show", "mergequeue_status", "mergerule_preview", "mergerule_propose_tighten", "mergerule_show", "mission_status", "notify_channel_list", "notify_class_list", "policy_in_force", "policy_list", "policy_propose_add", "policy_propose_remove", "policy_show", "policy_why", "proposal_list", "question", "read", "report_size", "roadmap_depend", "roadmap_list", "roadmap_propose_idea", "roadmap_show", "roadmap_undepend", "run_start", "scheduler_attention", "scheduler_decisions", "scheduler_preview", "scheduler_status", "scorecard_component", "scorecard_mine", "secretref_list", "seed_status", "task_bump_counter", "task_create", "task_list", "task_set_status", "task_show", "task_update", "template_list", "template_show", "todo", "tool_show", "tools_mine", "verify_list", "verify_propose_add", "verify_runs", "verify_show", "watchdog_list", "webfetch", "write"}

	// S22 T6: `search` joins the honest list only while the resolver seam
	// is wired — and only the entry point's !noretrieval link ever wires
	// it, so the no-retrieval run of this test pins the without-retrieval
	// registry exactly (V13); Count 40 -> 41 DELIBERATE when wired.
	if searchResolver != nil {
		want = append(want, "search")
		sort.Strings(want)
	}

	// S30 T5: `graph` joins only while its service seam is wired — same
	// conditional shape as `search` above (V13 absent-not-refusing);
	// Count +1 DELIBERATE when wired.
	if graphService != nil {
		want = append(want, "graph")
		sort.Strings(want)
	}

	names := ToolNames()
	sort.Strings(names)
	if len(names) != len(want) {
		t.Fatalf("ToolNames() is %v, want exactly %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("tool %d is %q, want %q", i, names[i], want[i])
		}
	}

	reg := Registry()
	if len(reg) != len(want)+1 {
		t.Fatalf("Registry() holds %d keys, want the ten plus the one hidden alias", len(reg))
	}
	for _, name := range want {
		if _, ok := reg[name]; !ok {
			t.Errorf("the honest name %q left the registry", name)
		}
	}
	// The eleventh key is the alias, and it answers to the same tool.
	if _, ok := reg[aliasWriteFile]; !ok {
		t.Fatalf("the eleventh key is not the hidden alias — Registry holds %v", names)
	}
	if reg[aliasWriteFile].Spec.Name != "write" || reg[aliasWriteFile].Run == nil ||
		reflect.ValueOf(reg[aliasWriteFile].Run).Pointer() != reflect.ValueOf(reg["write"].Run).Pointer() {
		t.Error("the eleventh key is not the write tool under its hidden name")
	}

	// The default grant: a subset of the registry, workspace file tools only.
	// No shell, and — the audit's egress half (SEC-T16-01's precondition) —
	// no webfetch either while the host space is unscoped.
	for _, name := range DefaultGrant() {
		if _, ok := reg[name]; !ok {
			t.Errorf("the default grant names %q, which is not in the registry", name)
		}
		if name == "bash" {
			t.Fatal("the default grant contains a shell")
		}
		if name == "webfetch" {
			t.Fatal("the default grant contains the egress while the host space is unscoped")
		}
	}
	if strings.Join(DefaultGrant(), ",") != "read,list,write" {
		t.Errorf("the default grant is %v, want read, list, write", DefaultGrant())
	}

	// The helpers the T19 rulings route through.
	if !IsToolName("bash") || !IsToolName(aliasWriteFile) || IsToolName("wrtie") || IsToolName("") {
		t.Error("IsToolName answers the wrong yes/no for the honest names, the alias, or a typo")
	}
	if got := CanonicalToolName(aliasWriteFile); got != "write" {
		t.Errorf("CanonicalToolName(write_file) = %q, want write", got)
	}
	if got := CanonicalToolName("grep"); got != "grep" {
		t.Errorf("CanonicalToolName must answer an honest name with itself, got %q", got)
	}
	if got := CanonicalToolName("no-such-tool"); got != "no-such-tool" {
		t.Errorf("CanonicalToolName must pass an unknown name through unchanged, got %q", got)
	}
}

// TestGrantingBothWriteNamesOffersTheToolOnce pins the SEC-051 dedupe half
// that lives in this package: granted=[write, write_file] is ONE capability,
// so the model is offered the write tool once — while BOTH spellings stay
// dispatchable, because the pinned matrix says a call spelled either way
// reaches the shared body.
func TestGrantingBothWriteNamesOffersTheToolOnce(t *testing.T) {
	root := workspace(t, nil)
	set, err := BuildToolset([]string{"read", "write", aliasWriteFile}, ToolContext{WorkspaceRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	specs := set.Specs()
	seen := map[string]int{}
	for _, s := range specs {
		seen[s.Name]++
	}
	if seen["write"] != 1 {
		names := make([]string, 0, len(specs))
		for _, s := range specs {
			names = append(names, s.Name)
		}
		t.Errorf("the write tool was offered %d times, want once; specs: %v", seen["write"], names)
	}
	if seen["read"] != 1 {
		t.Errorf("the dedupe swallowed a distinct tool: %v", seen)
	}
	if !set.Has("write") || !set.Has(aliasWriteFile) {
		t.Error("dedupe touched the dispatch keys — both spellings must stay callable")
	}
	// And the aliased call still works (the matrix's dispatch row).
	res := set.Call(context.Background(), aliasWriteFile, json.RawMessage(`{"path":"once.txt","content":"1"}`))
	if res.IsError {
		t.Errorf("the aliased call on a deduped set failed: %s", res.Content)
	}
}

func TestAnUnknownGrantIsAConstructionErrorRatherThanASilentOmission(t *testing.T) {
	// A typo in a grant list would otherwise produce a step with fewer tools
	// than its author intended, and the model would work around the absence
	// while the author read a list that said the tool was there.
	_, err := BuildToolset([]string{"read", "wrtie"}, ToolContext{WorkspaceRoot: "/workspace"})
	if err == nil {
		t.Fatal("an unknown grant was accepted")
	}
	if !strings.Contains(err.Error(), "wrtie") {
		t.Errorf("the error does not name the typo: %v", err)
	}
	// And it names what DOES exist, so the fix does not need a source read.
	if !strings.Contains(err.Error(), "read") || !strings.Contains(err.Error(), "bash") {
		t.Errorf("the error does not list the real tools: %v", err)
	}
}

func TestEveryUnknownGrantIsNamedAtOnce(t *testing.T) {
	// Not the first one. A list with three typos should take one fix, not
	// three runs.
	_, err := BuildToolset([]string{"aaa", "bbb"}, ToolContext{WorkspaceRoot: "/workspace"})
	if err == nil {
		t.Fatal("unknown grants were accepted")
	}
	for _, name := range []string{"aaa", "bbb"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("the error does not name %q: %v", name, err)
		}
	}
}

func TestAMissingWorkspaceRootPanicsAtConstruction(t *testing.T) {
	// A programmer error, and it panics rather than returning: every tool
	// resolves against this, and a toolset built without one would refuse
	// every path at turn three with a message about the workspace.
	//
	// This is the other half of T7's split — construction is NOT inside the
	// panic boundary, so the stack names the caller.
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("a toolset with no workspace root was built")
		}
		if !strings.Contains(toString(r), "workspace root") {
			t.Errorf("the panic does not say what is missing: %v", r)
		}
	}()
	_, _ = BuildToolset([]string{"read"}, ToolContext{})
}

func TestAPanickingToolBecomesAToolResultAndTheStepContinues(t *testing.T) {
	// O5. A daemon serving three projects should not die because one tool
	// mishandled one file.
	root := workspace(t, nil)
	set, err := BuildToolset(nil, ToolContext{WorkspaceRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	// Installed directly, because the point is a tool that misbehaves — and
	// no real tool is written to.
	set.byName["exploder"] = Tool{
		Spec: Read().Spec,
		Run: func(context.Context, ToolContext, json.RawMessage) Result {
			panic("a nil map in a parser")
		},
	}
	set.granted = append(set.granted, "exploder")

	res := set.Call(context.Background(), "exploder", json.RawMessage(`{}`))
	if !res.IsError {
		t.Fatal("a panicking tool did not produce an error result")
	}
	if !strings.Contains(res.Content, "a nil map in a parser") {
		t.Errorf("the recovered value is missing: %s", res.Content)
	}
	// The STACK is included deliberately: the model does not need it, but the
	// transcript is where somebody will look, and a panic with no stack is a
	// bug report with no information.
	if !strings.Contains(res.Content, "goroutine") && !strings.Contains(res.Content, ".go:") {
		t.Errorf("no stack in the result: %s", res.Content)
	}
	if !strings.Contains(res.Content, "defect in Aeon") {
		t.Errorf("the message does not say it is a defect: %s", res.Content)
	}
	if !strings.Contains(res.Summary, "PANICKED") {
		t.Errorf("the summary hides the panic: %q", res.Summary)
	}

	// And the toolset still works afterwards, which is what "the step
	// continues" means.
	if after := set.Call(context.Background(), "exploder", json.RawMessage(`{}`)); !after.IsError {
		t.Error("the toolset stopped working after a panic")
	}
}

func TestTheGrantedListCannotBeChangedAfterConstruction(t *testing.T) {
	// A caller that could add to the toolset would be a second thing deciding
	// what a step can call, and the grant list would stop being the only one.
	// Asserted on the type: the map is unexported, so this test is the record
	// of why it must stay that way.
	set, err := BuildToolset([]string{"read"}, ToolContext{WorkspaceRoot: "/workspace"})
	if err != nil {
		t.Fatal(err)
	}
	// Granted() hands back a COPY. Mutating it must not change the toolset.
	granted := set.Granted()
	granted[0] = "bash"
	if set.Granted()[0] != "read" {
		t.Error("Granted() hands out the toolset's own slice")
	}
	if set.Has("bash") {
		t.Error("mutating the returned slice granted a tool")
	}
}

func TestSpecsAreOfferedInGrantOrder(t *testing.T) {
	// Grant order rather than sorted, because the order tools are offered in
	// is a weak hint about which to prefer — and the grant list is where
	// somebody expressed an opinion about that.
	set, err := BuildToolset([]string{"bash", "read"}, ToolContext{WorkspaceRoot: "/workspace"})
	if err != nil {
		t.Fatal(err)
	}
	specs := set.Specs()
	if len(specs) != 2 || specs[0].Name != "bash" || specs[1].Name != "read" {
		t.Errorf("the specs are %v, want grant order", specs)
	}
}

func TestAnEmptyGrantIsAStepWithNoTools(t *testing.T) {
	// Legitimate: a step that only needs to think. The refusal message has to
	// cope with it, because "the tools you have are: " with nothing after it
	// reads as a bug.
	set, err := BuildToolset(nil, ToolContext{WorkspaceRoot: "/workspace"})
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Specs()) != 0 {
		t.Errorf("a step with no grant was offered %d tools", len(set.Specs()))
	}

	res := set.Call(context.Background(), "read", json.RawMessage(`{"path":"a"}`))
	if !res.IsError {
		t.Fatal("a step with no tools called one")
	}
	if !strings.Contains(res.Content, "no tools at all") {
		t.Errorf("the message reads badly with no tools: %s", res.Content)
	}
}

// TestThereIsNoCommandPolicyAndTheToolSaysSo was deleted at T13 (S11): the
// sentence it pinned — the pre-S11 apology claiming the command ran as given
// with no policy — became false the moment the verdict seam landed, and a
// test that pins a lie is worse than no test. Its banned-identifier
// blocklist survives VERBATIM in TestBashDescriptionTellsTheTruth below: the
// tools package still implements no pattern matching of its own (the matcher
// is internal/policy's), and `denylist`, `blocklist`, `allowedCommands` and
// `forbiddenCommands` are exactly the identifiers a half-policy in this layer
// would grow. The four names were kept precisely because S11 did not change
// what they warn about — only WHO enforces. Its "timeout/stdin/capped" bound
// words are kept too: the bounds are still the tool's own, and still must be
// said. (T19 retired the last of the apology text from the CLI surfaces; the
// grep for it is empty across aeon/cmd/.)

func TestBashDescriptionTellsTheTruth(t *testing.T) {
	// G20: the model is told the matcher's limits, in the description it
	// receives. A MISSING string is a failure even when the bypasses behave
	// as documented — for the class the mechanism refuses to parse
	// (`bash -c`, substitutions, backticks, heredocs, single | & ()), the
	// instruction IS the control.
	spec := Bash().Spec
	for _, want := range []string{
		"bash -c",              // named by name
		"command substitution", // $()
		"backticks",            // `...`
		"heredoc",              // << — named, not waved at
		"regardless",           // SEC-010: evasion is a violation regardless
	} {
		if !strings.Contains(strings.ToLower(spec.Description), strings.ToLower(want)) {
			t.Errorf("the bash description omits the limit %q: %s", want, spec.Description)
		}
	}
	// The under-split class named as the engine names it: a SINGLE pipe, a
	// single ampersand, subshell parens — one command to the matcher.
	for _, want := range []string{"`|`", "`&`", "()`"} {
		if !strings.Contains(spec.Description, want) {
			t.Errorf("the bash description does not name the unanalysed separator %s: %s", want, spec.Description)
		}
	}
	// The same words the tool's own honesty constant carries, verbatim — a
	// surface that rewords the limits has built a second truth.
	if !strings.Contains(spec.Description, BashHonesty) {
		t.Errorf("the description does not carry the one limits string verbatim: %s", spec.Description)
	}
	// The bounds the tool itself owns are still stated (kept from the deleted
	// test): mandatory timeout, closed stdin, capped output — and the
	// command-length bound (SEC-011), stated as a number the model can plan
	// around.
	for _, want := range []string{"timeout", "stdin", "capped"} {
		if !strings.Contains(strings.ToLower(spec.Description), want) {
			t.Errorf("the description does not mention %q: %s", want, spec.Description)
		}
	}
	if !strings.Contains(spec.Description, strconv.Itoa(commandMaxLen)+" bytes") {
		t.Errorf("the description does not name the command-length bound: %s", spec.Description)
	}

	// And no source in this package implements a policy, so the description
	// is not describing something this layer quietly has (kept verbatim from
	// the deleted test — see the note above).
	for name, src := range packageSources(t) {
		for _, banned := range []string{"denylist", "blocklist", "allowedCommands", "forbiddenCommands"} {
			if strings.Contains(src, banned) {
				t.Errorf("%s contains %s — a policy nobody documented is worse than none", name, banned)
			}
		}
	}
}

func TestExactlyOneRegistryNameMapsToTheBashTool(t *testing.T) {
	// SEC-050's guard, on this side of the boundary: the composition hazard
	// (a tool_access bash deny silently outranked by a coexisting
	// command_access allow) reopens the moment a SECOND registry name maps
	// to Bash() — policy targets "bash", and the alias would answer to no
	// rule. One name, pinned. Compared by the Run function's code pointer,
	// because Tool carries a func field and func values are not `==`.
	bashRun := reflect.ValueOf(Bash().Run).Pointer()
	maps := 0
	for name, tool := range Registry() {
		if tool.Spec.Name == "bash" || reflect.ValueOf(tool.Run).Pointer() == bashRun {
			maps++
			if name != "bash" {
				t.Errorf("registry name %q maps to the bash tool — an alias answers to no policy target", name)
			}
		}
	}
	if maps != 1 {
		t.Errorf("%d registry names map to Bash(); exactly one may", maps)
	}
}

func TestBashWithoutAPolicySourceRefusesToSpawn(t *testing.T) {
	// The fail-closed half of the seam: a toolset built WITHOUT
	// CommandVerdict (a storeless caller) cannot ask the engine, and a bash
	// that cannot ask spawns nothing. The refusal must NAME the missing
	// policy source — "bash could not run" without a why is the message that
	// sends somebody to the wrong file.
	root := t.TempDir()
	set, err := BuildToolset([]string{"bash"}, ToolContext{WorkspaceRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	res := set.Call(context.Background(), "bash", json.RawMessage(`{"command":"touch marker.txt"}`))
	if !res.IsError {
		t.Fatal("bash spawned with no policy source")
	}
	if !strings.Contains(res.Content, "policy source") {
		t.Errorf("the refusal does not name the missing policy source: %s", res.Content)
	}
	if _, err := os.Stat(filepath.Join(root, "marker.txt")); err == nil {
		t.Fatal("the file exists — the no-seam refusal did not precede the spawn")
	}
}

func TestBashRefusesAVerdictThatIsNotAnAllow(t *testing.T) {
	// The seam speaks, and its non-allow verdicts refuse. The gate normally
	// denies before a tool is dispatched; this pins that a verdict reaching
	// the tool WITHOUT the allow still carries no spawn key — and that the
	// refusal is rendered by the ONE formatter, verbatim (the sentence is
	// the whole content; no paraphrase waiting to drift).
	root := t.TempDir()
	set, err := BuildToolset([]string{"bash"}, ToolContext{WorkspaceRoot: root, CommandVerdict: denySeam(t, "touch*"), Exec: testExecSeam(t)})
	if err != nil {
		t.Fatal(err)
	}
	res := set.Call(context.Background(), "bash", json.RawMessage(`{"command":"touch marker.txt"}`))
	if !res.IsError {
		t.Fatal("bash spawned on a deny verdict")
	}
	v := policy.Evaluate(
		[]policy.Policy{
			commandRowT(t, "test-allow-star", "*", "allow"),
			commandRowT(t, "test-deny-exact", "touch*", "deny"),
		},
		policy.Request{Type: policy.CommandAccess, Agent: "tools-test", Tool: "bash", Command: "touch marker.txt"},
		time.Now(),
	)
	if !strings.Contains(res.Content, v.Sentence()) {
		t.Errorf("the refusal did not quote the verdict sentence verbatim: %s", res.Content)
	}
	if _, err := os.Stat(filepath.Join(root, "marker.txt")); err == nil {
		t.Fatal("the denied command created its file")
	}

	// And the same seam on the same command with an allow answers yes —
	// proof the refusals above are the verdict speaking, not the seam.
	allowed, err := BuildToolset([]string{"bash"}, ToolContext{WorkspaceRoot: root, CommandVerdict: allowAllSeam(t), Exec: testExecSeam(t)})
	if err != nil {
		t.Fatal(err)
	}
	res = allowed.Call(context.Background(), "bash", json.RawMessage(`{"command":"touch marker.txt"}`))
	if res.IsError {
		t.Fatalf("the allow refused: %s", res.Content)
	}
	if _, err := os.Stat(filepath.Join(root, "marker.txt")); err != nil {
		t.Fatal("the allowed command did not create its file")
	}
}

func TestBashBoundsCommandLengthAtTheBoundaryAndRefusesTowardDeny(t *testing.T) {
	// SEC-011: the engine's splitter scans the whole string before it
	// truncates, so the bound belongs HERE — before the seam is consulted,
	// so an oversized string never reaches Split. The refusal goes toward
	// deny (the tool declining), and says which of the two it is.
	root := t.TempDir()
	seamCalls := 0
	countingSeam := func(command string) (policy.Verdict, bool) {
		seamCalls++
		return allowAllSeam(t)(command)
	}
	set, err := BuildToolset([]string{"bash"}, ToolContext{WorkspaceRoot: root, CommandVerdict: countingSeam, Exec: testExecSeam(t)})
	if err != nil {
		t.Fatal(err)
	}
	oversized := "echo " + strings.Repeat("x", commandMaxLen)
	res := set.Call(context.Background(), "bash", json.RawMessage(fmt.Sprintf(`{"command":%q}`, oversized)))
	if !res.IsError {
		t.Fatal("an oversized command ran")
	}
	if !strings.Contains(res.Content, strconv.Itoa(commandMaxLen)) || !strings.Contains(res.Content, "refused") {
		t.Errorf("the refusal does not name the bound: %s", firstLines(res.Content, 2))
	}
	if seamCalls != 0 {
		t.Errorf("the oversized string reached the policy seam (%d calls) — the bound is not before Split", seamCalls)
	}

	// One byte under stays the engine's question, not the boundary's.
	res = set.Call(context.Background(), "bash", json.RawMessage(fmt.Sprintf(`{"command":%q}`, "echo "+strings.Repeat("x", commandMaxLen-5))))
	if res.IsError {
		t.Errorf("a command inside the bound was refused at the boundary: %s", firstLines(res.Content, 2))
	}
	if seamCalls == 0 {
		t.Error("the in-bound command never consulted the seam")
	}
}

func TestConstructionIsOutsideThePanicBoundary(t *testing.T) {
	// T7's point 3, asserted on the code: `runSafely` wraps the tool's
	// EXECUTION only. A boundary around construction too would turn every
	// programmer error into "the tool failed", at turn three, with the real
	// cause four frames away.
	src := packageSources(t)["toolset.go"]
	buildAt := strings.Index(src, "func BuildToolset")
	safelyAt := strings.Index(src, "func (t Toolset) runSafely")
	if buildAt < 0 || safelyAt < 0 {
		t.Fatal("the functions this test is about have been renamed")
	}
	build := src[buildAt:safelyAt]
	if strings.Contains(build, "recover()") {
		t.Error("BuildToolset recovers from panics; a programmer error must reach the caller")
	}
	if !strings.Contains(src[safelyAt:], "recover()") {
		t.Error("runSafely no longer recovers; a panicking tool would take the daemon down")
	}
}

func TestEveryToolInTheRegistryIsUsableAndDescribed(t *testing.T) {
	// A tool with no description is a tool the model will not call, and one
	// with invalid parameters makes the provider reject the whole request —
	// including the tools that were fine.
	for name, tool := range Registry() {
		// The hidden compatibility key holds the SAME tool as the write
		// key, so it carries the write tool's own name — exempt from the
		// key-equals-name rule by design, pinned by
		// TestWriteFileAliasStillResolves below.
		if name == aliasWriteFile {
			continue
		}
		if tool.Spec.Name != name {
			t.Errorf("the registry key %q holds a tool named %q", name, tool.Spec.Name)
		}
		if len(tool.Spec.Description) < 40 {
			t.Errorf("%s has a %d-character description", name, len(tool.Spec.Description))
		}
		if tool.Run == nil {
			t.Errorf("%s has no implementation", name)
		}
		var parsed map[string]any
		if err := json.Unmarshal(tool.Spec.Parameters, &parsed); err != nil {
			t.Errorf("%s has invalid parameters: %v", name, err)
		}
		if parsed["additionalProperties"] != false {
			t.Errorf("%s allows additional properties; a model will invent one", name)
		}
	}
}

func TestEveryLimitIsLabelledWithItsProvenance(t *testing.T) {
	// The same discipline as S3's: a number with no provenance is a number
	// nobody can argue with later. `Guess` is the honest field — it says
	// whether anybody measured.
	limits := Limits()
	if len(limits) < 4 {
		t.Fatalf("%d limits reported; the constants are not all described", len(limits))
	}
	for _, l := range limits {
		if l.Name == "" {
			t.Error("a limit has no name")
		}
		if l.Value == 0 {
			t.Errorf("%s has value 0", l.Name)
		}
		if l.Provenance == "" {
			t.Errorf("%s has no provenance", l.Name)
		}
	}
}

// TestNewToolsNotInDefaultGrant is S45 T7's "registered is not granted"
// pin (S11's rule, exercised by the seven policy/proposal names PM9 adds):
// none of the seven may sit in DefaultGrant() — a handler that wants them
// lists them explicitly, and even then the policy tool_access rows decide
// each call. The default grant stays exactly read, list, write.
// 2026-11-01 (S56 T8): the five autonomy names join the pin — registered,
// granted to NO ONE; DefaultGrant() stays read, list, write.
func TestNewToolsNotInDefaultGrant(t *testing.T) {
	seven := []string{"policy_list", "policy_show", "policy_why", "policy_in_force",
		"proposal_list", "policy_propose_add", "policy_propose_remove"}
	// S56 T8: the autonomy family — three reads, two restrictive proposes.
	seven = append(seven, "autonomy_list", "autonomy_show", "autonomy_effective",
		"autonomy_propose_revoke", "autonomy_propose_deny")
	// 2026-11-03 (S58 T6): the five supervision reads join the pin — SV16
	// registers them and grants them to nobody; the inbox is a person's
	// attention, and even reading it sideways is an operator's decision.
	seven = append(seven, "inbox_list", "inbox_summary", "mission_status",
		"notify_class_list", "notify_channel_list")
	// 2026-11-05 (S63 T6, RT11): the scorecard family's three safe reads —
	// registered, granted to NO ONE; a measurement is never a default
	// grant, and DefaultGrant() stays read, list, write.
	seven = append(seven, "scorecard_mine", "scorecard_component", "digest_learned")
	grant := map[string]bool{}
	for _, n := range DefaultGrant() {
		grant[n] = true
	}
	for _, n := range seven {
		if grant[n] {
			t.Errorf("%s is in DefaultGrant() — PM9/AL12 register the policy/proposal/autonomy tools, they grant them to nobody", n)
		}
		if _, ok := Registry()[n]; !ok {
			t.Errorf("%s is not in the registry — the catalog projection lost a row", n)
		}
	}
	if got := strings.Join(DefaultGrant(), ","); got != "read,list,write" {
		t.Errorf("the default grant moved: %v — S45 T7 changes the registry, never the grant", DefaultGrant())
	}
}
