package handler

import "testing"

// chat_body_test.go pins the S39 T6 body: the plain conversation's voice is
// ONE general body, its temperature is the ancestor's ported unchanged, and
// its grants are the workspace tools plus the records surface. run_start
// was deliberately absent until T10 — BuildToolset refuses an unknown grant
// name, so declaring it before the catalog row existed would have broken
// every turn (T6's "declarations only" and T10's "tools: GAINS run_start"
// read together in T10's favour); this pin now demands it present, because
// the body that PROPOSES a blueprint must hold the tool that starts one.

func TestChatBodyLoads(t *testing.T) {
	def, rej := LoadDefinition("chat")
	if rej != nil {
		t.Fatalf("the chat body must load, got reject: %+v", rej)
	}
	if def.Name != "chat" {
		t.Fatalf("name=%q", def.Name)
	}
	if def.Temperature != 0.3 {
		t.Fatalf("temperature=%v — the pin reads it from the drop record's named original (0.3, project-planner-loop.md L6)", def.Temperature)
	}
	want := []string{"read", "list", "glob", "grep", "write", "edit", "bash", "todo", "question",
		"task_create", "task_list", "task_show", "goal_list", "component_list", "attach", "run_start"}
	if len(def.Tools) != len(want) {
		t.Fatalf("tools=%v, want the 16 T10 ships", def.Tools)
	}
	have := map[string]bool{}
	for _, n := range def.Tools {
		have[n] = true
	}
	for _, n := range want {
		if !have[n] {
			t.Fatalf("missing grant %q", n)
		}
	}
	if !have["run_start"] {
		t.Fatal("run_start is missing — T10's row stands in the catalog and the body that proposes must be able to start")
	}
	if _, ok := Lookup("chat"); !ok {
		t.Fatal("the registry must serve chat")
	}
	if !RegisteredBodiesContains("chat") {
		t.Fatal("chat must be in RegisteredBodies — the loader's list and the directory are one truth")
	}
}

// TestBrainstormerKeepsItsOtherTools is the S39 T10 guard on the grant edit:
// the brainstormer is reached THROUGH run_start (brief.md) and now also
// holds the tool — the one agent-startable loop action — but a grant edit
// that rewrote its front-matter instead of appending would silently drop the
// S38 catalog set it walks with. Names, not counts: a dropped name must fail
// by name.
func TestBrainstormerKeepsItsOtherTools(t *testing.T) {
	def, rej := LoadDefinition("brainstormer")
	if rej != nil {
		t.Fatalf("the brainstormer body must load, got reject: %+v", rej)
	}
	have := map[string]bool{}
	for _, n := range def.Tools {
		have[n] = true
	}
	// 2026-11-08 (documents): the capture documents are filed with doc_write and
	// read back with doc_read — the workspace `write` is no longer granted, so
	// a deliverable cannot land in the project root by this agent's own hand.
	for _, n := range []string{"read", "doc_write", "doc_read", "glob", "grep", "question", "todo",
		"goal_create", "goal_update", "goal_submit_review", "attach", "run_start"} {
		if !have[n] {
			t.Fatalf("brainstormer lost grant %q to the T10 edit — tools=%v", n, def.Tools)
		}
	}
	if have["write"] || have["edit"] {
		t.Fatalf("brainstormer files documents, it writes no workspace files — tools=%v", def.Tools)
	}
}

func RegisteredBodiesContains(name string) bool {
	for _, n := range RegisteredBodies() {
		if n == name {
			return true
		}
	}
	return false
}
