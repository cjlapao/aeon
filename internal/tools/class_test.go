package tools

import (
	"encoding/json"
	"os"
	"testing"
)

type classEntry struct {
	Class   RerunClass `json:"class"`
	Pending bool       `json:"pending,omitempty"`
	// RetrievalOnly marks a tool that legitimately DOES NOT EXIST in the
	// no-retrieval build (S22 T6, V13): present when the resolver is
	// wired, absent otherwise — the fixture stays consistent in both,
	// and the marker (not an absent key) is what keeps the committed
	// list from failing where the tool cannot exist.
	RetrievalOnly bool `json:"retrieval_only,omitempty"`
}

func loadClasses(t *testing.T) map[string]classEntry {
	t.Helper()
	b, err := os.ReadFile("testdata/rerun_classes.json")
	if err != nil {
		t.Fatal(err)
	}
	var entries map[string]classEntry
	if err := json.Unmarshal(b, &entries); err != nil {
		t.Fatal(err)
	}
	return entries
}

func TestEveryToolDeclaresARerunClass(t *testing.T) {
	for name, tool := range Registry() {
		if !tool.RerunClass().Valid() {
			t.Errorf("%s has invalid re-run class %q", name, tool.RerunClass())
		}
	}
	if (RerunClass("")).Valid() {
		t.Fatal("empty class became valid")
	}
}

func TestBashIsTheOnlyUnsafeTool(t *testing.T) {
	var unsafe []string
	for name, tool := range Registry() {
		if tool.RerunClass() == RerunUnsafe {
			unsafe = append(unsafe, name)
		}
	}
	if len(unsafe) != 1 || unsafe[0] != "bash" {
		t.Fatalf("unsafe tools = %v", unsafe)
	}
}

func TestRerunClassesMatchCommittedList(t *testing.T) {
	entries := loadClasses(t)
	registry := Registry()
	for name, entry := range entries {
		if !entry.Class.Valid() {
			t.Errorf("%s has invalid class %q", name, entry.Class)
		}
		tool, exists := registry[name]
		if !exists && !entry.Pending && !entry.RetrievalOnly {
			t.Errorf("%s is absent but not marked pending or retrieval-only", name)
		}
		if exists && entry.Pending {
			t.Errorf("%s exists but is still marked pending", name)
		}
		if exists && tool.RerunClass() != entry.Class {
			t.Errorf("%s class %q != committed %q", name, tool.RerunClass(), entry.Class)
		}
	}
	for name := range registry {
		// The hidden compatibility key is not a tool of its own — the SAME
		// write tool answers it, and it is deliberately absent from the
		// committed list (a listing that kept both names is two truths).
		// TestWriteFileAliasStillResolves owns the alias.
		if name == aliasWriteFile {
			continue
		}
		if _, ok := entries[name]; !ok {
			t.Errorf("%s is missing from committed classes", name)
		}
	}
}

func TestFixtureCoversAllCurrentAndPendingTools(t *testing.T) {
	entries := loadClasses(t)
	// S11 T18 DELIBERATELY flipped this count 11 → 10 (Decision D3). The
	// rename retired the eleventh name (the old write tool) from every
	// authored surface, and the six entries S7 committed as "pending" all
	// resolved as their tools landed (T14: write, edit; T15: glob, grep;
	// T16: webfetch; T17: todo) — so the committed list is now the honest
	// ten: every registered tool, no pending, no hidden alias. The alias is
	// absent from the list BY DESIGN; see TestWriteFileAliasStillResolves.
	// 2026-10-27 (S38 T9): added the management catalog (23 names) — OL2's single
	// vocabulary, projected from internal/records; registry is now the honest 33.
	// Count 10 -> 33 DELIBERATE. Reads are safe, writes detectable (a row write
	// never needs the unsafe receipt ceremony — none of them spawns or fetches).
	// 2026-10-27 (S38 T10): + task family (6 names) — reads safe, writes
	// detectable, the ladder reached through SetTaskStatus not around it;
	// Count 33 -> 39 DELIBERATE.
	// 2026-10-28 (S39 T10): + run_start — the one loop action an agent may
	// take, behind an ask row the OPERATOR adds (CR5); Count 39 -> 40 DELIBERATE.
	// 2026-10-29 (S22 T6): + search (safe — a pure read; recovery re-runs
	// it without thought, Q6/W16), marked retrieval_only because the
	// no-retrieval build legitimately has no such tool (V13); Count
	// 40 -> 41 DELIBERATE.
	// 2026-10-29 (S24 T5): + report_size (detectable — the one-shot sizing
	// report; a re-run after a reverted sizing attempt is a NEW step with a
	// fresh closure, and the refusal lives in that closure); Count
	// 41 -> 42 DELIBERATE.
	// 2026-10-31 (S44-extra CT9): the component family is the nine tree
	// operations — the four project tools go, five component tools join;
	// Count 42 -> 43 DELIBERATE (S44-extra CT9, 2026-10-31).
	// 2026-10-31 (S43-extra T6): + seed_status (safe — the ONE seed tool, a
	// pure read projected from the records catalog; recovery re-runs it
	// without thought) — a seed WRITE is a human door, never a tool (SD10).
	// 2026-10-31 (S70 MB1): + ask_agent (detectable — it files a durable
	// agent_message row; a re-run that files the ask twice is detectable in
	// the rows, never silent).
	// Both slices landed a tool the same day; Count 43 -> 45 DELIBERATE.
	// 2026-11-01 (S45 T7): + the policy/proposal family (7 names) — PM9's
	// five reads are safe (a pure read; recovery re-runs them without
	// thought) and the two propose twins are detectable (they file durable
	// PROPOSAL rows — a re-run that files the same proposal twice is
	// detectable in the rows, never silent, and a proposal enforces nothing
	// until a person commits it (TD2)); Count 45 -> 52 DELIBERATE.
	// 2026-11-01 (S56 T8): + the autonomy family (5 names) — the three reads
	// are safe (a pure read; recovery re-runs them without thought) and the
	// two propose twins are detectable (they file durable PROPOSAL rows of
	// the two RESTRICTIVE kinds — a re-run that files the same proposal twice
	// is detectable in the rows, never silent, and a proposal enforces
	// nothing until a person commits it (TD2)); Count 52 -> 57 DELIBERATE.
	// 2026-11-02 (S46 T8): + the mcp own-view family (5 names) — the three
	// reads are safe (scoped cache/row reads with no side effects; the
	// status arm spawns nothing, MG5) and the two propose twins are
	// detectable (durable INERT proposal rows of the two narrowing kinds —
	// a re-run files a duplicate a person sees, never silent, and enforces
	// nothing until committed (TD2/MG9)); Count 57 -> 62 DELIBERATE.
	// 2026-11-02 (S55 merge): S55 T10's seven loop-control / budget /
	// watchdog agent tools (six safe reads + loop_request_stop,
	// detectable) join the merged class list; Count 62 -> 69, measured
	// on the merged tree.
	// 2026-11-02 (S47 T7): + the two own-view tool reads (tools_mine, tool_show) —
	// both safe. Neither writes, probes, spawns or applies: they read the registry,
	// the tool rows, the agent row and the policy set through the SAME
	// toolcatalog.ForAgent the person's tools.byAgent uses, and TC1 holds that S47
	// adds no write tool at all. A re-run of either answers the same way twice
	// because neither caches (N8), which is what makes `safe` the honest class
	// rather than a courtesy; Count 69 -> 71 DELIBERATE.
	// 2026-11-02 (S62 T6): + backup_status — the backup family's one read
	// twin over backup.status (safe, Mutates:false); run/verify/prune are
	// human doors and restore is never a tool (BK9). Count 71 -> 72
	// DELIBERATE, measured on the merged tree.
	// 2026-11-02 (S62 T8): + export_sets — the export family's read twin
	// over export.sets (safe, Mutates:false); export.run is a human door
	// (data leaves the machine) and never a tool (BK14). Count 72 -> 73
	// DELIBERATE, measured on the merged tree (S62 T8, 2026-11-02).
	// 2026-11-03 (S48 T10): + the blueprint family (5 names) — the four
	// safe reads (blueprint_list/show/validate/lint, all re-reads, all
	// persisting nothing) and blueprint_propose (detectable — files the
	// INERT blueprint_save row; there is deliberately no write tool, BP9).
	// Count 73 -> 78 DELIBERATE, measured on the merged tree (2026-11-03).
	// 2026-11-03 (S57 T9): + the gate family (9 names, VG19) — the six
	// reads are safe (verify_list/show/runs, mergerule_show/preview,
	// dod_list — a pure read; a re-run re-reads) and the three
	// propose-only twins are detectable (they file durable INERT proposal
	// rows, or a ledger event for dod_mark — a re-run files a duplicate a
	// person sees, never silent, and enforces nothing until committed
	// (TD2/TD3)); the nine human gate writes are never tools (VG17); on
	// top of S62 T8's export_sets. Count 73 -> 82 DELIBERATE, measured
	// on the merged tree (S57 T9, 2026-11-03).
	// 2026-11-03 (merge reconciliation): both landings stand — 73 + S57 T9's
	// nine gate names + S48 T10's five blueprint names = 87, measured on THIS
	// merged tree (each side's own arithmetic is above).
	// 2026-11-03 (S58 T6 + S59 T7, merged the same day): + the supervision
	// family (5 safe reads: inbox_list, inbox_summary, mission_status,
	// notify_class_list, notify_channel_list — a re-run re-reads; SV12 keeps
	// every supervision WRITE off the tool plane) and the five execution-
	// environment names (four safe reads — environment_list, environment_show,
	// environment_effective, secretref_list: names, never a value, never a
	// spawn — and the one detectable ask, environment_propose_restrict, which
	// files an INERT environment_restrict row a person commits). Count 87 ->
	// 97 DELIBERATE (+5 +5), measured on the merged tree, 2026-11-03.
	// 2026-11-04 (S49 T7): + the automation family (five names) — four safe
	// reads (automation_list, automation_show, automation_fires,
	// automation_health: the socket's own answers through recordTools, the
	// secret env a NAME and a set-bool, never a value) and the ONE
	// detectable ask, automation_propose_disable, which files an INERT
	// automation_disable row a person commits (AN9). Count 97 -> 102
	// DELIBERATE (+5), measured on the merged tree, 2026-11-04.
	// 2026-11-04 (S50 T5, RM8): + the roadmap family (three names) — two safe
	// reads (roadmap_list, roadmap_show: the socket's own answers through
	// recordTools, board and show-with-trail through the SAME shared answers)
	// and the ONE detectable ask, roadmap_propose_idea, which files an INERT
	// roadmap_idea row a person commits (add-only; there is no update/cancel/
	// estimate/priority/dispatch name to file). Count 102 -> 105 DELIBERATE
	// (+3), measured on the merged tree, 2026-11-04.
	// 2026-11-04 (S51 T5, TM6): + the bug family (three names) — two safe
	// reads (bug_list, bug_show: the person's own board and show answers
	// through recordTools, the SAME renderings, rule 4) and the ONE
	// detectable filing, bug_file, which files an UNVALIDATED,
	// priority-unknown claim through the SAME CreateAudited triage.file
	// runs, filed_by the stamped slug and capped per agent
	// (triage.MaxOpenClaimsPerAgent). Count 105 -> 108 DELIBERATE (+3),
	// measured on this tree, 2026-11-04.
	// 2026-11-04 (S52 T4, LM1–LM7): + the memory family (four names) — two
	// safe reads of the agent's OWN readable set (learning_query,
	// learning_show: person word, run-in projects, own slug — LM1's set,
	// the SAME MineQuery the prompt feed uses), the ONE capped self-log
	// write (learning_add: scope stamped, provenance observed/assumed,
	// 32 per 24h counted off the audit ledger) and the ONE detectable ask,
	// learning_propose_retract, which files an INERT learning_retract row a
	// person commits (there is no retract/restore/edit name an agent can
	// file — LM7/LM8). Count 108 -> 112 DELIBERATE (+4), measured on this
	// tree, 2026-11-04.
	// 2026-11-04 (S53 T3, HV4): + the history family (two names) — the two
	// safe reads history_list and history_show, twinned to the person's
	// history.list/history.show through the ONE shared answer (rule 4).
	// No export twin (HV5 — the person's door), no coverage or vocabulary
	// twin (HV4 — the app's census).
	// 2026-11-04 (S60 T9, MQ17): + the merge-queue family (five names) —
	// three safe own-view reads (mergequeue_list, mergequeue_show,
	// mergequeue_status: the person's own door answers through recordTools
	// over the SAME queue service, rule 4, filtered to the repositories the
	// agent's steps ran in), the ONE detectable narrowing write,
	// mergequeue_dequeue (this loop's own queued/conflicted item only — a
	// removal can only narrow what the queue may do), and the ONE
	// detectable ask, mergequeue_propose_enqueue, which files an INERT
	// mergequeue_enqueue row a person commits unattended as {proposal,<id>}
	// (there is no enqueue/reorder/retry/land/settings name an agent can
	// reach — MQ13/MQ17, T8's guards).
	// Each side counted its own bump on its branch (S53 112→114, S60
	// 112→117); the merged truth is one count: 112 -> 119 DELIBERATE (+7),
	// measured on this tree, 2026-11-04.
	// 2026-11-04 (S54 T4): + index_status — the platform family's one read
	// twin over platform.status (safe: freshness re-read, nothing to
	// reconcile). Count 119 -> 120 DELIBERATE (+1), measured on this tree.
	// 2026-11-05 (S63 T6): + the scorecard family (3 names) — RT11's three
	// safe reads (scorecard_mine, scorecard_component, digest_learned),
	// all class safe: a measurement re-answered over the same window
	// changes nothing (RT3). Count 120 -> 123 DELIBERATE (+3).
	// 2026-11-05 (S27 T3, CN7/P6): + the graph's two edges (roadmap_depend,
	// roadmap_undepend), both detectable: each files or removes one EDGE —
	// a durable roadmap_dependency row moving in one direction. An edge is
	// intent only (no rung moves, nothing dispatches because of it), but a
	// re-run touches rows, so it is not a safe re-read: the duplicate and
	// the no-such-edge refusals are what make a double call visible. The
	// LADDER writes (add · update · cancel · estimate · priority · ideate ·
	// plan · materialize) stay off the tool plane whole (RM8 stands for the
	// rung; P6's note: direct, widens nothing). Count 123 -> 125
	// DELIBERATE (+2), measured on this tree, 2026-11-05.
	// 2026-11-05 (S27 T8, CN23): + the scheduler's four reads (all class
	// safe — re-running a status, a preview, a ledger read or a needs-you
	// list reads the same rows and moves nothing; the plan is computed,
	// never executed, by the asking). Count 125 -> 129 DELIBERATE (+4),
	// measured on this tree.
	// 2026-11-05 (S61 T8, GI17): + the goal-intake plane's seven (five safe
	// reads — re-running a charter index, a charter read, a template index,
	// a template read or an intake status reads the same rows and moves
	// nothing — + two detectable asks that file inert proposals and change
	// no charter, no plan, no ledger). Count 129 -> 136 DELIBERATE (+7),
	// measured on this tree.
	// 2026-11-08 (documents): + the deliverables door's four (doc_write and
	// doc_edit detectable — an append-only revision, a replay a no-op or one
	// more; doc_read and doc_list safe). Count 136 -> 140 DELIBERATE (+4),
	// measured on this tree.
	// 2026-11-08 (variables): + the variable manager's two agent tools
	// (env_list safe — a pure read; env_get detectable — it writes the ledger row
	// that names the agent). Count 140 -> 142 DELIBERATE (+2), measured on this tree.
	// 2026-11-15 (loop replay): + loop_context, the loop's index (safe — a pure read). Count 142 -> 143 DELIBERATE (+1).
	if len(entries) != 143 {
		t.Fatalf("committed class list has %d names, want 143 (the honest ten + the catalog + search + report_size + seed_status + ask_agent + the policy/proposal family + the autonomy family + the mcp own-view family + the loop-control/budget/watchdog family + the two own-view tool reads + the backup read twin + the export read twin + the gate family + the blueprint family + the supervision family + the execution-environment family + the automation family + the roadmap family + the bug family + the memory family + the history family + the merge-queue family + the platform read twin + the scorecard family + the graph's two edges + the scheduler's four reads + the goal-intake plane's seven + the deliverables door's four + the variable manager's two, no pending)", len(entries))
	}
	for _, name := range []string{"read", "list", "question", "bash", "glob", "grep", "webfetch", "todo", "write", "edit"} {
		if _, ok := entries[name]; !ok {
			t.Errorf("missing %s", name)
		}
	}
}
