package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/runtime"
	"github.com/cjlapao/aeon/internal/tools"
)

// T6 — rewrites 11–15: code-reviewer, verifier-engineer, qa-engineer,
// qa-test-spec, debugger-engineer.
//
// The batch's trap is length-as-padding: the three largest originals
// (204/287/293 lines) are mostly SCHEMAS and JUDGEMENT, the content A4
// warned rewrites lose because it "reads like incidental detail". These
// tests pin the schemas, the .secrets discipline in both directions, the
// Playwright split, and the absence of send-back prose.

var t6Batch = []string{
	"code-reviewer",
	"verifier-engineer",
	"qa-engineer",
	"qa-test-spec",
	"debugger-engineer",
}

// t6Required is each handler's own handoff shape — per-handler schemas are
// the point of this batch, so the runs use each handler's declared fields
// rather than t4's fixed tests_added spec (recorded as a deviation in
// T6.md's Notes: sharing the dispatch spec would test the wrong schema).
var t6Required = map[string][]string{
	"code-reviewer":     {"findings"},
	"verifier-engineer": {"evidence"},
	"qa-engineer":       {"verdict", "defects", "suites_curated"},
	"qa-test-spec":      {"specs_written", "self_check"},
	"debugger-engineer": {"root_cause", "recommended_owner"},
}

var t6Temps = map[string]float64{
	"code-reviewer":     0.3,
	"verifier-engineer": 0.2,
	"qa-engineer":       0.3,
	"qa-test-spec":      0.4,
	"debugger-engineer": 0.3,
}

// t6Report is the scripted model's final answer carrying this handler's
// own required handoff fields.
func t6Report(t *testing.T, name string) t4Turn {
	t.Helper()
	fields := map[string]string{
		"status": "ok", "summary": name + " completed the step",
		"findings": "none", "evidence": "go test -timeout 120s ./... — 12/12 pass",
		"verdict": "pass", "defects": "none", "suites_curated": "docs/test_suites/health.md",
		"specs_written": "docs/test_suites/health.md", "self_check": "all four questions pass",
		"root_cause": "nil map at internal/foo/bar.go:41", "recommended_owner": "golang-engineer",
	}
	body := map[string]string{"status": "ok", "summary": fields["summary"]}
	for _, k := range append([]string{"summary"}, t6Required[name]...) {
		body[k] = fields[k]
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	return t4Turn{Text: string(raw)}
}

// t6dispatch mirrors t4's dispatch but builds the HandoffSpec from the
// handler's OWN required fields, so RunLoop enforces the schema this body
// actually declares.
func t6dispatch(t *testing.T, handlerName, task string, turns ...t4Turn) (runtime.StepResult, string) {
	t.Helper()
	return dispatchWithRequired(t, handlerName, task, append([]string{"summary"}, t6Required[handlerName]...), turns...)
}

// dispatchWithRequired is t4's dispatch with the handoff shape supplied by
// the caller — each handler's OWN required fields, so RunLoop enforces the
// schema that body actually declares (T6/T7 batches share one engine).
func dispatchWithRequired(t *testing.T, handlerName, task string, required []string, turns ...t4Turn) (runtime.StepResult, string) {
	t.Helper()
	d, rej := LoadDefinition(handlerName)
	if rej != nil {
		t.Fatalf("LoadDefinition(%s): %v", handlerName, rej)
	}
	step, ok := Build(handlerName, task)
	if !ok {
		t.Fatalf("%s is not registered — the body must be IN the registry, not merely on disk", handlerName)
	}
	dir := t.TempDir()
	set, err := tools.BuildToolset(d.Tools, tools.ToolContext{WorkspaceRoot: dir})
	if err != nil {
		t.Fatalf("BuildToolset: %v", err)
	}
	client := &t4Client{turns: turns}
	st := &runtime.LoopState{Request: runtime.StepRequest{
		Handler:       step,
		Messages:      []model.Message{{Role: model.RoleUser, Content: []model.Block{{Kind: model.BlockText, Text: task}}}},
		Manifest:      runtime.ContextManifest{},
		Tools:         set.Specs(),
		WorkspaceRoot: dir,
	}, Manifest: &runtime.ContextManifest{
		MaxIterations: runtime.MaxIterations,
		DeadlineAt:    time.Now().Add(2 * time.Minute),
	}}
	res := runtime.RunLoop(context.Background(), st, runtime.LoopDeps{
		Client: client,
		Tools:  t4Caller{set: set},
		HandoffSpec: runtime.HandoffSpec{
			Required: append([]string{"status", "summary"}, required...),
			Enums:    map[string][]string{"status": {"ok", "reject", "blocked"}},
		},
	})
	if len(client.requests) > 0 {
		joined := step.System + strings.Join(allText(client.requests[0].Messages), "\n")
		if !strings.Contains(joined, d.Body) {
			t.Errorf("%s: the dispatched request did not carry the reviewed body", handlerName)
		}
	}
	return res, dir
}

func TestFivePassVerifyAndRunOnce(t *testing.T) {
	tasks := map[string]string{
		"code-reviewer":     "review the diff that adds the /health endpoint",
		"verifier-engineer": "verify the health endpoint meets its success criteria",
		"qa-engineer":       "run QA on the health feature",
		"qa-test-spec":      "write the test spec for the health endpoint",
		"debugger-engineer": "diagnose the failing TestBar nil map in internal/foo",
	}
	for _, n := range t6Batch {
		d, rej := LoadDefinition(n)
		if rej != nil {
			t.Fatalf("%s: %v", n, rej)
		}
		if fs := Verify(d, nil); len(fs) > 0 {
			for _, f := range fs {
				t.Errorf("%s", f.String())
			}
		}
		res, _ := t6dispatch(t, n, tasks[n],
			t4Turn{ToolUses: []model.Block{writeBlock("NOTES.md", n+" artifact\n")}},
			t6Report(t, n),
		)
		mustOK(t, res, n)
		if got := fmt.Sprint((*res.Handoff)["summary"]); !strings.Contains(got, n) {
			t.Errorf("%s: handoff summary %q did not come from this handler's scripted report", n, got)
		}
	}
}

func TestT6TemperaturesPortedUnchanged(t *testing.T) {
	for _, n := range t6Batch {
		d, rej := LoadDefinition(n)
		if rej != nil {
			t.Fatalf("%s: %v", n, rej)
		}
		if d.Temperature != t6Temps[n] {
			t.Errorf("%s: temperature %v, want the original's %v — a model parameter ports as-is (A20); changing it changes the handler's behaviour without changing its instructions", n, d.Temperature, t6Temps[n])
		}
	}
}

func TestTestSuiteSchemaSurvives(t *testing.T) {
	// Both QA bodies write docs/test_suites/ documents (the originals say so
	// outright: "you both write the SAME schema"), so the schema must be
	// present in each, and the shared shape must not have drifted.
	anchors := []string{
		"Test Case ID: TC-XXX",
		"Priority: High / Medium / Low",
		"Preconditions: <What must be true before running>",
		"Steps: <Numbered execution steps>",
		"Expected Result: <What should happen>",
		"Actual Result: <QA fills this in>",
		"Status: Pass / Fail / Blocked",
		"Notes: <Any observations>",
		"Test Execution Checklist",
		"Environment Setup",
		"Prerequisites",
		"Environment Variables",
		"Test Data Preparation",
		"Quick Reference",
		"Edge Cases",
		"| Suite | Capability | Cases | Last updated | Status |",
	}
	for _, n := range []string{"qa-engineer", "qa-test-spec"} {
		d, rej := LoadDefinition(n)
		if rej != nil {
			t.Fatalf("%s: %v", n, rej)
		}
		for _, a := range anchors {
			if !strings.Contains(d.Body, a) {
				t.Errorf("%s: the docs/test_suites/ schema lost %q — the schema is domain content (A5); this repository's own QA suites follow it, and length is not a reason to cut it", n, a)
			}
		}
	}
	// anti-drift: the shared Test Case Format block is byte-identical in both
	f1 := extractBlock(t, "qa-engineer", "Test Case ID: TC-XXX", "Notes: <Any observations>")
	f2 := extractBlock(t, "qa-test-spec", "Test Case ID: TC-XXX", "Notes: <Any observations>")
	if f1 == "" || f1 != f2 {
		t.Errorf("the Test Case Format block drifted between the two handlers that write the same folder — a reader never meets two document styles; that claim lives in both originals")
	}
}

func extractBlock(t *testing.T, name, from, to string) string {
	t.Helper()
	d, rej := LoadDefinition(name)
	if rej != nil {
		t.Fatalf("%s: %v", name, rej)
	}
	i := strings.Index(d.Body, from)
	if i < 0 {
		return ""
	}
	j := strings.Index(d.Body[i:], to)
	if j < 0 {
		return ""
	}
	return d.Body[i : i+j+len(to)]
}

func TestSecretsDisciplineIsPreserved(t *testing.T) {
	// The most likely rewrite failure: keep "you may read .secrets", lose
	// every clause that constrains what happens next. Both halves present.
	d, rej := LoadDefinition("qa-engineer")
	if rej != nil {
		t.Fatal(rej)
	}
	permission := []string{".secrets", "may read"}
	discipline := []string{
		"by NAME",                   // reference credentials by name
		"never appear",              // values never land in any file or report
		"<redacted>",                // echoed tokens are redacted before they land
		"Do not invent credentials", // no guessing, no brute force
		"blocked",                   // a missing key NAME makes the case blocked
	}
	// The discipline must live WITH the permission — in the Authentication
	// section itself. A body that keeps a stray "never appear" sentence in
	// the boundaries while gutting the section is the failure this guards:
	// a positive control deleting the section must fail, and the original
	// anywhere-in-body check passed it (measured before tightening).
	i := strings.Index(d.Body, "## Authentication")
	if i < 0 {
		t.Fatal("qa-engineer lost the Authentication — the `.secrets` file section entirely; a body permitting the read without the discipline is MORE PERMISSIVE than its original, the worst direction a rewrite can err")
	}
	section := d.Body[i:]
	if j := strings.Index(section[strings.Index(section, "\n")+1:], "\n## "); j >= 0 {
		section = section[:strings.Index(section, "\n")+1+j]
	}
	for _, s := range append(permission, discipline...) {
		if !strings.Contains(strings.ToLower(section), strings.ToLower(s)) {
			t.Errorf("the .secrets section lost %q — a body permitting the read without the discipline is MORE PERMISSIVE than its original, the worst direction a rewrite can err", s)
		}
	}
}

func TestPlaywrightIsDeferredNotDropped(t *testing.T) {
	rec, rej := ParseDropRecord("drops/qa-engineer.md")
	if rej != nil {
		t.Fatal(rej)
	}
	deferred := strings.ToLower(strings.Join(rec.Sections["DEFERRED"], "\n"))
	for _, want := range []string{"playwright", "browser", "mcp"} {
		if !strings.Contains(deferred, want) {
			t.Errorf("the Playwright half must be recorded DEFERRED naming browser automation and the deferred MCP client (A18); missing %q", want)
		}
	}
	domain := strings.ToLower(strings.Join(rec.Sections["DOMAIN"], "\n"))
	if !strings.Contains(domain, "http fallback") {
		t.Errorf("the HTTP fallback is keepable domain (A18) — it must be recorded KEPT, not swept into the deferral")
	}
	d, rej := LoadDefinition("qa-engineer")
	if rej != nil {
		t.Fatal(rej)
	}
	if strings.Contains(strings.ToLower(d.Body), "playwright") {
		t.Errorf("the body names Playwright — DEFERRED means the body does not instruct using a capability the installation may not have; the discipline (fallback honesty) is what the body keeps")
	}
}

func TestNoSendBackProseRemains(t *testing.T) {
	// L4/T3: verifier invalid, security failing, review requesting changes
	// are the SAME event — the handoff's outcome — and the blueprint routes
	// it. Prose about how to signal a send-back is ritual.
	forbidden := []string{
		"changes-requested", "changes requested", "request changes",
		"send back", "send-back", "re-dispatch", "fix loop",
		"escalates to the user", "inner loop",
	}
	for _, n := range t6Batch {
		d, rej := LoadDefinition(n)
		if rej != nil {
			t.Fatalf("%s: %v", n, rej)
		}
		joined := strings.ToLower(d.Body + " " + d.Description)
		for _, f := range forbidden {
			if strings.Contains(joined, f) {
				t.Errorf("%s still carries send-back prose %q — the handoff's outcome is the send-back; the blueprint routes it (L4, T3)", n, f)
			}
		}
	}
}

func TestReviewCriteriaSurvive(t *testing.T) {
	// The 204-line original whose length is judgement, not padding.
	d, rej := LoadDefinition("code-reviewer")
	if rej != nil {
		t.Fatal(rej)
	}
	for _, want := range []string{
		"Success criteria", "Security", "Correctness", "Performance", "Maintainability", "Completeness",
		"integration seams", "orphans",
		"file:line", "never \"improve this.\"",
		"rubber-stamp",
	} {
		if !strings.Contains(d.Body, want) {
			t.Errorf("code-reviewer lost %q — the review dimensions and the finding discipline ARE the domain of a 204-line original", want)
		}
	}
}
