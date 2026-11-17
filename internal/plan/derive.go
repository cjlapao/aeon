// Package plan is S17's one new boundary (L31): the plan loader. It
// validates a task plan's depends_on at LOAD and settles the walk order
// there — before S8's iterate sees anything.
//
// Why the load is the boundary: S8's iterate is a monotonic forward walk
// (lane, item, step); tasks have depends_on, and the alternative — skipping
// blocked items and revisiting them — makes the position non-monotonic, the
// very thing the walk's shape was chosen to avoid. So the order is decided
// here, once, and the walk stays a walk (Q4: this package sits IN FRONT of
// advance.go; TestIterateIsUnchanged pins that separation).
//
// Every refusal here is a DOMAIN result, never an error (DB19): a bad plan
// is the loop's own outcome to report, not an infrastructure failure. v1's
// reason, kept because it is the right one — all four refused classes
// "produce a task that can never complete, and catching them here names the
// plan instead of surfacing a mystery at merge time."
package plan

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Task is one node of the plan. Deliberately small: identity, prose, and
// the dependency edges this loader exists to police. Nothing below the
// blueprint's data lives here (N1: no new mechanism).
type Task struct {
	ID        string
	Title     string
	DependsOn []string
	// File records which document the task was loaded from, when it was
	// (LoadDir). A refusal names it, so the author fixes the file pointed at
	// rather than the whole directory.
	File string
}

// Refusal is the domain answer to "this plan cannot run". Kind is one of
// depends-on-missing | self-dependency | duplicate-id | cycle | malformed —
// closed set, so a caller can switch on it without string games; Task is
// the offending task; Cycle names the cycle's members for the cycle kind.
type Refusal struct {
	Kind   string
	Task   string
	Depend string   // the missing dependency, for depends-on-missing
	Cycle  []string // the cycle's members, in cycle order, for cycle
	File   string   // the offending document, for malformed/LoadDir refusals
}

// Sentence is the ONE renderer (the policy formatter's argument, applied):
// every surface speaks a refusal with this, so a paraphrase cannot drift
// into a second truth. It always contains what is wrong, which task, and
// what to do about it — "there is a cycle" is not an answer, and the
// message must not send someone to read a twenty-task plan by hand.
func (r *Refusal) Sentence() string {
	if r == nil {
		return ""
	}
	where := r.File
	if where != "" {
		where = " in " + where
	}
	switch r.Kind {
	case "depends-on-missing":
		return fmt.Sprintf("plan refused: task %q%s depends on %q, which does not exist — nothing will ever satisfy it", r.Task, where, r.Depend)
	case "self-dependency":
		return fmt.Sprintf("plan refused: task %q%s depends on itself — the task can never complete; this usually reads as a typo, and it is refused as its own class so the sentence says which mistake was made", r.Task, where)
	case "duplicate-id":
		return fmt.Sprintf("plan refused: task id %q appears twice%s — two tasks with one identity make every status write ambiguous; give them distinct ids", r.Task, where)
	case "cycle":
		return fmt.Sprintf("plan refused: dependency cycle %s — no ordering exists for these tasks; break the cycle by removing one edge", strings.Join(r.Cycle, " -> "))
	case "malformed":
		return fmt.Sprintf("plan refused: %s%s cannot be read as a task document: %s", r.Task, where, r.Depend)
	default:
		return fmt.Sprintf("plan refused: %s (%s)", r.Kind, r.Task)
	}
}

// Load validates the plan and returns its walk order, or the first refusal.
// Never an error — a bad plan is a domain outcome (DB19).
//
// The four refused classes (D5, C3) run in a FIXED precedence — duplicate,
// self, missing, cycle — so the same broken plan always reports the same
// refusal, and fixing one thing deterministically reveals the next rather
// than racing to a different complaint (TestValidationPrecedesSort).
func Load(tasks []Task) ([]Task, *Refusal) {
	// 1. Duplicate ids first: two entries, one identity, and every check
	// after this would answer the ambiguous case differently depending on
	// which copy it reached.
	seen := map[string]bool{}
	for _, t := range tasks {
		if seen[t.ID] {
			return nil, &Refusal{Kind: "duplicate-id", Task: t.ID, File: t.File}
		}
		seen[t.ID] = true
	}

	// 2. Self-dependencies: refused as its own class, not folded into the
	// cycle finding — "it looks like a typo rather than a cycle", and the
	// sentence must tell the author which mistake they made.
	for _, t := range tasks {
		for _, d := range t.DependsOn {
			if d == t.ID {
				return nil, &Refusal{Kind: "self-dependency", Task: t.ID, Depend: d, File: t.File}
			}
		}
	}

	// 3. Edges to nowhere.
	for _, t := range tasks {
		for _, d := range t.DependsOn {
			if !seen[d] {
				return nil, &Refusal{Kind: "depends-on-missing", Task: t.ID, Depend: d, File: t.File}
			}
		}
	}

	// 4. The cycle — the only class that survives into the walk's own
	// arithmetic, so it is answered last, by the sort that would have run.
	sorted, cycle := stableSort(tasks)
	if len(cycle) > 0 {
		return nil, &Refusal{Kind: "cycle", Task: cycle[0], Cycle: cycle}
	}
	return sorted, nil
}

// Names projects a sorted plan to the identifier list S8's iterate walks
// (st.Tasks). A sorted empty plan projects to an EMPTY slice, never nil:
// the walk over nothing is a legitimate no-op with a real shape.
func Names(sorted []Task) []string {
	out := make([]string, 0, len(sorted))
	for _, t := range sorted {
		out = append(out, t.ID)
	}
	return out
}

// LoadDir reads a plan directory of task documents with `id`, `title` and
// `depends_on` front matter — the same shape this repository's own tasks/
// directory carries, kept on purpose: Aeon plans in the format Aeon's own
// slices are written in. Documents sort by file name before parsing, so the
// DECLARED order (the stability reference) is the directory's name order,
// deterministic and reproducible.
func LoadDir(dir string) ([]Task, *Refusal) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		// os.ReadDir is infrastructure (the disk spoke up) — but the LOOP
		// never sees this: LoadDir is a CLI/daemon boundary, and the one
		// place a real error may surface, so it is reported as a malformed
		// refusal naming the directory rather than a bare error.
		return nil, &Refusal{Kind: "malformed", Task: dir, Depend: err.Error()}
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		files = append(files, e.Name())
	}
	sort.Strings(files)

	var out []Task
	for _, name := range files {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, &Refusal{Kind: "malformed", Task: name, Depend: err.Error(), File: name}
		}
		task, problem := parseTaskDoc(name, string(raw))
		if problem != "" {
			return nil, &Refusal{Kind: "malformed", Task: name, Depend: problem, File: name}
		}
		out = append(out, task)
	}
	return Load(out)
}

// parseTaskDoc reads the front-matter block only: `---` lines bracket a
// region of key: value lines (values are scalars or the [a, b] bracket form
// this repository's task files use). Anything outside the region is prose
// the loader has no business reading.
func parseTaskDoc(name, raw string) (Task, string) {
	lines := strings.Split(raw, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return Task{}, "the document does not open with a --- front-matter block"
	}
	task := Task{File: name}
	sawID := false
	for i := 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "---" {
			if !sawID {
				return Task{}, "front matter has no id:"
			}
			return task, ""
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			// A bare line inside the region is malformed — the strictness is
			// the point: silent acceptance here is how a `depends_on` typo
			// becomes a task that never waits for anything.
			return Task{}, fmt.Sprintf("front-matter line %q is not key: value", line)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		switch key {
		case "id":
			task.ID = value
			sawID = true
		case "title":
			task.Title = value
		case "depends_on":
			deps, problem := parseList(value)
			if problem != "" {
				return Task{}, fmt.Sprintf("depends_on %q: %s", value, problem)
			}
			task.DependsOn = deps
		}
	}
	return Task{}, "the front-matter block is never closed with ---"
}

// parseList accepts `[]`, `[a, b]`, or a bare scalar `a` (a single
// dependency without brackets — this repository's own task files write
// depends_on: [T2] and depends_on: [] only, but the scalar is the shape a
// human reaches for first, and refusing it would name the wrong file's
// problem).
func parseList(value string) ([]string, string) {
	if value == "" || value == "[]" {
		return nil, ""
	}
	if !strings.HasPrefix(value, "[") {
		return []string{value}, ""
	}
	if !strings.HasSuffix(value, "]") {
		return nil, "an opening [ is never closed"
	}
	inner := strings.TrimSpace(value[1 : len(value)-1])
	if inner == "" {
		return nil, ""
	}
	parts := strings.Split(inner, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			return nil, "an empty entry in the list"
		}
		out = append(out, strings.Trim(p, `"'`))
	}
	return out, ""
}
