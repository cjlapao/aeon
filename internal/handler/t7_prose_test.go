package handler

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

// T7 — rewrites 16–20: project-planner, intent-clarifier, investigator,
// technical-writer, memory-steward.
//
// Five handlers that produce DOCUMENTS. The batch's two traps: the file
// schemas (plan.md / prompt.md / discovery.md) drifting into two bodies —
// two sources of truth about one document's shape — and prose handlers
// being "easy to pad", where the temptation to improve rather than port is
// strongest. TestFileSchemasSurvive pins the schemas into exactly one body
// each; TestZeroLinesAdded pins the arithmetic mechanically because A17 is
// unenforceable by reading alone.

var t7Batch = []string{
	"project-planner",
	"intent-clarifier",
	"investigator",
	"technical-writer",
	"memory-steward",
}

// Named TestFiveProseHandlersRunOnce: the spec's gate name
// TestFivePassVerifyAndRunOnce is already taken by T6's batch in this one
// package (recorded in T7.md Notes).
var t7Required = map[string][]string{
	"project-planner":  {"task_count", "sizing"},
	"intent-clarifier": {"open_questions"},
	"investigator":     {"hotspots", "open_questions"},
	"technical-writer": {"doc_validity"},
	"memory-steward":   {"lessons_recorded"},
}

func t7Report(t *testing.T, name string) t4Turn {
	t.Helper()
	fields := map[string]string{
		"summary":          name + " completed the step",
		"task_count":       "6",
		"sizing":           "largest est_changed_lines is 380 (T4, the client adapter — split by section under the ~500 budget)",
		"open_questions":   "None.",
		"hotspots":         "measured: cmd/hello/main.go 120 — none over 400",
		"doc_validity":     "docs build pass; markdownlint pass; lychee 0 dead links; cspell clean; documented samples ran",
		"lessons_recorded": "1 added, 0 strengthened, 7 rejected by triage",
	}
	body := map[string]string{"status": "ok"}
	for _, k := range append([]string{"summary"}, t7Required[name]...) {
		body[k] = fields[k]
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	return t4Turn{Text: string(raw)}
}

func TestFiveProseHandlersRunOnce(t *testing.T) {
	tasks := map[string]string{
		"project-planner":  "plan the health-endpoint feature from prompt.md and discovery.md",
		"intent-clarifier": "clarify: add a health endpoint that k8s can probe",
		"investigator":     "investigate the repo for the health-endpoint feature",
		"technical-writer": "document the new GET /health endpoint",
		"memory-steward":   "harvest this loop's lessons into the Learnings file",
	}
	for _, n := range t7Batch {
		d, rej := LoadDefinition(n)
		if rej != nil {
			t.Fatalf("%s: %v", n, rej)
		}
		if fs := Verify(d, nil); len(fs) > 0 {
			for _, f := range fs {
				t.Errorf("%s", f.String())
			}
		}
		res, _ := dispatchWithRequired(t, n, tasks[n], append([]string{"summary"}, t7Required[n]...),
			t4Turn{ToolUses: []model.Block{writeBlock("NOTES.md", n+" artifact\n")}},
			t7Report(t, n),
		)
		mustOK(t, res, n)
		if got := fmt.Sprint((*res.Handoff)["summary"]); !strings.Contains(got, n) {
			t.Errorf("%s: handoff summary %q did not come from this handler's scripted report", n, got)
		}
	}
}

// ── the file schemas: one owner each ───────────────────────────────────────

// Each schema's DEFINITION anchors. Consumer MENTIONS of a section name are
// legal (the planner sizes from the discovery's hotspot section — its own
// original says so); a second body carrying the schema's definition is a
// second source of truth and fails.
var t7Schemas = map[string][]string{
	"plan.md": {
		"### What we understood",
		"### How we will do it",
		"### What you will have at the end",
		"in_scope", "out_of_scope", "task_count",
		"est_changed_lines",
		"**File(s):**",
	},
	"prompt.md": {
		"## Original request",
		"Original request is verbatim",
		"`Original request`, `Prompt`, `Requirements`, `Open questions`",
	},
	"discovery.md": {
		"summary; codebase findings; external research (with versions); constraints & risks",
		"## Legal review",
		"Decisions (resolved with the user)",
	},
}

var t7Owner = map[string]string{
	"plan.md":      "project-planner",
	"prompt.md":    "intent-clarifier",
	"discovery.md": "investigator",
}

func bodyOf(t *testing.T, name string) string {
	t.Helper()
	d, rej := LoadDefinition(name)
	if rej != nil {
		t.Fatalf("%s: %v", name, rej)
	}
	return d.Body
}

func TestFileSchemasSurvive(t *testing.T) {
	for doc, anchors := range t7Schemas {
		owner := t7Owner[doc]
		b := bodyOf(t, owner)
		for _, a := range anchors {
			if !strings.Contains(b, a) {
				t.Errorf("%s: the %s schema lost %q — the schema is domain content (A5); S8's produces: only proves the file EXISTS, the schema is what makes it the right file", owner, doc, a)
			}
		}
	}
}

func TestNoSchemaAppearsTwice(t *testing.T) {
	// Scoped to the batch: ownership is asserted among the five planning and
	// prose handlers. Outside it, appearances are authored — legal-advisor's
	// own original carries "## Decisions (resolved with the user)" and the
	// "## Legal review" reference verbatim (T8), so a corpus-wide ban would
	// refuse the truth.
	for doc, anchors := range t7Schemas {
		for _, n := range t7Batch {
			if n == t7Owner[doc] {
				continue
			}
			b := bodyOf(t, n)
			for _, a := range anchors {
				if strings.Contains(b, a) {
					t.Errorf("%s carries %s schema anchor %q while %s owns it — a schema appearing in two bodies is two sources of truth about a document's shape (A5); the consumer mentions the section, it does not restate the schema", n, doc, a, t7Owner[doc])
				}
			}
		}
	}
}

// ── memory-steward ─────────────────────────────────────────────────────────

func TestMemoryStewardHasNoMemoryTool(t *testing.T) {
	d, rej := LoadDefinition("memory-steward")
	if rej != nil {
		t.Fatal(rej)
	}
	for _, tool := range d.Tools {
		if strings.Contains(strings.ToLower(tool), "memory") {
			t.Errorf("memory-steward is granted %q — Aeon's registry has no memory tool; inventing a grant for a tool that does not exist is how a handler comes to describe a capability the installation lacks (N6/N9)", tool)
		}
	}
	lower := strings.ToLower(d.Body)
	for _, forbidden := range []string{"memory_append", "`memory`", "call `memory", "memory tool"} {
		if strings.Contains(lower, forbidden) {
			t.Errorf("memory-steward's body names the memory tool (%q) — its job was reshaped to write a file through `write`; the body must not instruct a call to a tool that does not exist", forbidden)
		}
	}
	if !strings.Contains(d.Body, "`write`") {
		t.Error("memory-steward must state it writes the harvest through `write` — the drop record says the change of shape plainly; so does the body")
	}
}

func TestMemoryStewardJudgementSurvives(t *testing.T) {
	b := bodyOf(t, "memory-steward")
	for _, want := range []string{
		"Durable", "Non-obvious", "Actionable",
		"zero lessons",
		"imperative",
		"STRENGTHEN",
		"never two rules in a row",
	} {
		if !strings.Contains(b, want) {
			t.Errorf("memory-steward lost %q — its judgement about what constitutes a durable lesson is the domain that survived; ONLY the append mechanism was replaced", want)
		}
	}
	rec, rej := ParseDropRecord("drops/memory-steward.md")
	if rej != nil {
		t.Fatal(rej)
	}
	joined := strings.ToLower(strings.Join(append(rec.Sections["BECAME MECHANISM"], rec.Sections["DEFERRED"]...), "\n"))
	if !strings.Contains(joined, "write") || !strings.Contains(joined, "produces") {
		t.Errorf("the memory-steward record must state the change of shape PLAINLY — mechanism replaced by a file written through `write` at a produces-declared path; it currently reads as though the handler were unchanged")
	}
}

// ── the padding trap ───────────────────────────────────────────────────────

func TestZeroLinesAdded(t *testing.T) {
	for _, n := range t7Batch {
		rec, rej := ParseDropRecord("drops/" + n + ".md")
		if rej != nil {
			t.Fatalf("%s: %v", n, rej)
		}
		// 2026-10-27 (S38 T10): one AUTHORIZED addition, named and sized
		// here rather than deleted from the batch — the guard stays whole for
		// every other body. The slice spec (S38 T10, OL14) mandates the
		// planner's records paragraph: plan rows filed through task_create/
		// attach after the files exist. Eight lines, measured, recorded in
		// the record's REWORDED ledger at L160.
		if want, ok := map[string]int{"project-planner": 8}[n]; ok {
			if rec.Added != want {
				t.Errorf("%s: the authorized S38 T10 paragraph is %d lines, not %d — additions are named and sized or they are not authorized", n, rec.Added, want)
			}
		} else if rec.Added != 0 {
			t.Errorf("%s: drop record claims %d added lines — prose handlers are where the temptation to IMPROVE rather than port is strongest; A17 allows zero (record the rewording in REWORDED, never additions)", n, rec.Added)
		}
		// The closure carries the added side now that one addition is
		// authorized above: original + added must equal rewrite + removed
		// exactly — with zero additions this reduces to the original pin.
		if rec.OrigLines+rec.Added != rec.RewriteLine+rec.Removed {
			t.Errorf("%s: arithmetic does not close — original %d + added %d ≠ rewrite %d + removed %d", n, rec.OrigLines, rec.Added, rec.RewriteLine, rec.Removed)
		}
	}
}

func TestNoPathInstructions(t *testing.T) {
	// produces: decides placement; the schema decides shape. A body that
	// says where its deliverable lives is a second, unenforced contract.
	forbidden := []string{".projects/loops", "loopSlug", "$ROOT", "file lane", "loop dir", "loop directory"}
	for _, n := range t7Batch {
		b := bodyOf(t, n)
		for _, f := range forbidden {
			if strings.Contains(b, f) {
				t.Errorf("%s still says where to put things (%q) — the blueprint's produces: decides placement now; the body may name its deliverable's SHAPE, not its address", n, f)
			}
		}
		if !strings.Contains(b, "the step's request names") {
			t.Errorf("%s never says its output path comes from the step's request — with no path instruction in the body, the delegation to produces: must be stated, or the handler simply invents a path", n)
		}
	}
}
