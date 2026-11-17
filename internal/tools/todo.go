package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/cjlapao/aeon/internal/model"
)

// The `todo` tool — the within-step checklist (W20).
//
// # What "within-step" means, mechanically
//
// The list lives in the single `todoList` behind the pointer this function's
// closure holds. `Registry()` calls `Todo()` once per toolset build, so the
// list lives exactly as long as the `Toolset` that holds it — one step — and
// dies with it. Nothing here is package-level: a package map keyed by step
// would be the same thing wearing a different name — a second memory the
// transcript does not show, which is exactly what W20 refuses. That design
// is not tempting, it is forbidden, and `todo_test.go` pins it: an AST walk
// over this file refuses package-level variables and any import that could
// reach the durable layers.
//
// # Why the returned text is the whole record
//
// Every result — `set` and `list` alike — carries the FULL rendered
// checklist. The tool results in the transcript ARE the record of the list:
// replaying the transcript replays the state, and a stale replay shows a
// diverging list rather than silently matching. That is why the declared
// recovery class is `detectable` (class.go's vocabulary): re-running a step
// must be able to DETECT that the todo state it would write differs. The
// row count — not a code review — is the proof (todo_test drives a scripted
// step over a real database and asserts the task and loop tables gained
// nothing).

// todoItem is one checklist entry, exactly the pinned shape: id, content,
// and a status from the closed set below.
type todoItem struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	Status  string `json:"status"`
}

// The closed status vocabulary (W20). A bad status is refused at `set`,
// naming the value, so nothing downstream has to defend against a fourth
// marker.
const (
	todoPending    = "pending"
	todoInProgress = "in_progress"
	todoDone       = "done"
)

func validTodoStatus(status string) bool {
	switch status {
	case todoPending, todoInProgress, todoDone:
		return true
	}
	return false
}

// maxTodoItems bounds a model-authored list.
//
// # Provenance: chosen.
//
// The whole list is rendered into every result, and the results are the
// transcript, and the transcript IS the record — so an unbounded list would
// inflate the record through the one door the model holds open. The bound
// belongs at this boundary for the same reason bash bounds the command
// length before the engine sees it (SEC-011's lesson). 200 is far above any
// honest plan for a step and far below a problem. A refusal names the bound.
const maxTodoItems = 200

// maxTodoInputBytes bounds the raw arguments BEFORE json.Unmarshal
// (SEC-T17-01, the T19 hardening).
//
// # Provenance: chosen at the bound the finding measured — 256 KiB.
//
// The count gate at parseTodo fires only AFTER Unmarshal has already built
// the whole slice: measured, a refused 1M-item payload allocated ~300 MiB
// (~7× the raw bytes) on its way to the refusal. The count bound protects
// the record; a byte bound BEFORE decoding protects the process, and the
// cheap pre-check is the only way to get there — you cannot unmarshal your
// way to deciding not to unmarshal. 256 KiB sits far above any honest list
// at the item bound (200 items with real content) and far below anything
// sized to hurt; a refusal names the bound, like every other bound here.
const maxTodoInputBytes = 256 << 10

// Per-item string bounds (SEC-T17-01's second half: content/id carried no
// length at all).
//
// # Provenance: chosen — id 256 bytes, content 4 KiB.
//
// An id is "a stable label" — 256 bytes is a label with room to breathe and
// nothing honest needs more. A content line is rendered into EVERY result
// (the transcript is the record), so 4 KiB is the per-line form of the same
// arithmetic that produced maxTodoItems: far above a checklist line, far
// below a transcript problem. Together with the item bound the whole
// accepted list sits at ~800 KiB of content — and the input gate above
// already refused anything past 256 KiB raw, so these caps are defense in
// depth against a payload that stays under the raw bound by being small in
// count and enormous in one field. Refusals name which item and which bound.
const (
	maxTodoItemIDBytes      = 256
	maxTodoItemContentBytes = 4 << 10
)

// todoList is the small struct behind the per-toolset pointer the closure
// holds: one toolset build, one list.
type todoList struct {
	items []todoItem
	// S75: the revision of the persisted list this toolset last saw, and
	// whether it has loaded one yet. Per toolset, not package-level (W20).
	revision int
	loaded   bool
}

// todoMarker is the rendering glyph for a status. It is the ONLY place the
// glyphs are written, so the transcript of a step and a re-render of the
// same list cannot disagree about how an item looks.
func todoMarker(status string) string {
	switch status {
	case todoPending:
		return "[ ]"
	case todoInProgress:
		return "[~]"
	default: // todoDone — parseTodo refused every other value.
		return "[x]"
	}
}

// renderTodoList draws the checklist lines plus the counts line. The counts
// line always carries all three terms ("3 pending, 1 in progress, 2 done")
// so a reader — model, replay, or human — never has to guess whether an
// omitted term means zero or was forgotten.
//
// Model-authored text is QUOTED and single-lined (SEC-T17-02, the T19
// hardening): the id is rendered through strconv.Quote and the content
// through todoSingleLine, because an unescaped newline inside content
// rendered an EXTRA authoritative-looking checklist line — the model could
// forge lines into the transcript that looks like Aeon's render, and could
// shadow the counts line with a lookalike. With the escapes, the render is
// structurally items+1 lines whatever any item claims. (Honest scope, as
// the finding recorded: every todo string is the model's own in-band output
// and no security decision parses this render — but a record that can be
// forged by its author is not a record, and the quoting is one line each.)
func renderTodoList(items []todoItem) string {
	lines := make([]string, 0, len(items)+1)
	if len(items) == 0 {
		lines = append(lines, "no items.")
	}
	var pending, inProgress, done int
	for _, item := range items {
		lines = append(lines, todoMarker(item.Status)+" "+strconv.Quote(item.ID)+": "+todoSingleLine(item.Content))
		switch item.Status {
		case todoPending:
			pending++
		case todoInProgress:
			inProgress++
		case todoDone:
			done++
		}
	}
	lines = append(lines, fmt.Sprintf("%d pending, %d in progress, %d done", pending, inProgress, done))
	return strings.Join(lines, "\n")
}

// todoSingleLine is the single-line form for model-authored content:
// backslash doubled FIRST (so an authored `\n` cannot impersonate the
// escape of a real newline), then the two control characters that would end
// the line the renderer owns. A function, not a package-level Replacer —
// this file's W20 guard refuses package-level variables of any kind, stateless
// or not, and the guard is not bent for an escaper.
func todoSingleLine(content string) string {
	s := strings.ReplaceAll(content, `\`, `\\`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return strings.ReplaceAll(s, "\r", `\r`)
}

// todoArgs is the pinned argument shape: {action, items?}. Items is a
// pointer so `set` can tell an absent field from an explicit empty list —
// only the latter clears, and a step is never cleared by an omission.
type todoArgs struct {
	Action string      `json:"action"`
	Items  *[]todoItem `json:"items"`
}

// parseTodo validates every argument before anything changes. It is
// all-or-nothing: a refused `set` leaves the list exactly as it was, because
// a half-replaced list is the state the transcript would then lie about.
// Every refusal names the thing refused.
//
// The byte gate runs BEFORE json.Unmarshal (SEC-T17-01): decoding is what
// costs the memory, so the bound that protects the process must precede it,
// not follow it.
func parseTodo(input json.RawMessage) (todoArgs, error) {
	if len(input) > maxTodoInputBytes {
		return todoArgs{}, fmt.Errorf("todo refused %d bytes of arguments; the whole call is bounded at %d bytes, because every result renders the whole list and the transcript is the record",
			len(input), maxTodoInputBytes)
	}
	var args todoArgs
	if err := json.Unmarshal(input, &args); err != nil {
		return todoArgs{}, fmt.Errorf("todo arguments are not valid JSON: %w", err)
	}
	switch args.Action {
	case "list", "set":
	default:
		return todoArgs{}, fmt.Errorf("todo action %q is invalid; use \"list\" or \"set\"", args.Action)
	}
	if args.Action == "list" {
		if args.Items != nil {
			return todoArgs{}, fmt.Errorf("todo list takes no items; list renders the list already set — send a set call to change it")
		}
		return args, nil
	}
	if args.Items == nil {
		return todoArgs{}, fmt.Errorf("todo set requires an items list; set replaces the whole list, so an omitted items field has no meaning")
	}
	items := *args.Items
	if len(items) > maxTodoItems {
		return todoArgs{}, fmt.Errorf("todo set refused %d items; the list is bounded at %d, because every result renders the whole list and the transcript is the record",
			len(items), maxTodoItems)
	}
	for i, item := range items {
		if !validTodoStatus(item.Status) {
			return todoArgs{}, fmt.Errorf("todo set refused item %d: status %q is invalid; use pending, in_progress, or done", i+1, item.Status)
		}
		if strings.TrimSpace(item.ID) == "" {
			return todoArgs{}, fmt.Errorf("todo set refused item %d: the id is empty (content %q)", i+1, item.Content)
		}
		if strings.TrimSpace(item.Content) == "" {
			return todoArgs{}, fmt.Errorf("todo set refused item %q: the content is empty", item.ID)
		}
		if len(item.ID) > maxTodoItemIDBytes {
			return todoArgs{}, fmt.Errorf("todo set refused item %d: the id is %d bytes and the bound is %d — an id is a label, not an essay",
				i+1, len(item.ID), maxTodoItemIDBytes)
		}
		if len(item.Content) > maxTodoItemContentBytes {
			return todoArgs{}, fmt.Errorf("todo set refused item %d (id %q): the content is %d bytes and the bound is %d — every result renders the whole list, and the transcript is the record",
				i+1, item.ID, len(item.Content), maxTodoItemContentBytes)
		}
	}
	return args, nil
}

// Todo returns the `todo` tool. One call builds one toolset's instance: the
// list it carries is reached by nobody else, and two toolsets never share a
// list (asserted both ways in todo_test — through BuildToolset and through
// two direct Todo() calls).
func Todo() Tool {
	state := &todoList{}
	return Tool{
		Class: RerunDetectable,
		Spec: model.ToolSpec{
			Name:        "todo",
			Description: "Maintain the working checklist for this conversation or loop. action set replaces the whole list with items, each {id, content, status}; status is pending, in_progress, or done. action list renders the checklist back. Both results carry the full rendered list. The list stays active across steps until every item is done, you clear it with an empty set, or the person closes it; keep it current as you work, and mark items done when they are.",
			Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "action": {"type": "string", "enum": ["list", "set"], "description": "set replaces the whole list; list renders it back"},
    "items": {"type": "array", "maxItems": 200, "description": "the whole new list; required for set, forbidden for list (the schema states the same bound parseTodo enforces, so the model sees the limit it will hit)", "items": {
      "type": "object",
      "properties": {
        "id": {"type": "string", "description": "a stable label for the item"},
        "content": {"type": "string", "description": "what the item has to accomplish"},
        "status": {"type": "string", "enum": ["pending", "in_progress", "done"]}
      },
      "required": ["id", "content", "status"],
      "additionalProperties": false
    }}
  },
  "required": ["action"],
  "additionalProperties": false
}`),
		},
		Run: func(ctx context.Context, tc ToolContext, input json.RawMessage) Result {
			args, err := parseTodo(input)
			if err != nil {
				return Errorf("%s", err)
			}
			if tc.Todo == nil {
				return runTodoInStep(state, args)
			}
			return runTodoPersisted(ctx, tc.Todo, state, args)
		},
	}
}

// runTodoInStep is the unwired path: exactly the pre-S75 behaviour, plus the
// sentence that says the list is not kept (TD3).
func runTodoInStep(state *todoList, args todoArgs) Result {
	if args.Action == "set" {
		held := make([]todoItem, len(*args.Items))
		copy(held, *args.Items)
		state.items = held
	}
	// The returned Content IS the record: the full rendered list on every call,
	// so the transcript replays the state.
	// The render stays byte-identical to the pre-S75 result (the transcript
	// is the record and tests pin its shape); the SUMMARY — what the person's
	// chat shows — carries the honest fact that nothing here is kept (TD3).
	return Result{
		Content: renderTodoList(state.items),
		Summary: fmt.Sprintf("todo · %s (%d items) %s", args.Action, len(state.items), todoNotPersisted),
	}
}

// runTodoPersisted reads and writes the loop's list through the step's seam. The
// revision this toolset last saw rides in `state`; a `set` presents it, a stale
// one is refused with the current list in the result (TD12).
func runTodoPersisted(ctx context.Context, door TodoStore, state *todoList, args todoArgs) Result {
	load := func() (Result, bool) {
		entries, revision, err := door.Load(ctx)
		if err != nil {
			return Errorf("todo could not read the loop's list: %v", err), false
		}
		state.items = state.items[:0]
		for _, e := range entries {
			state.items = append(state.items, todoItem{ID: e.ID, Content: e.Content, Status: e.Status})
		}
		state.revision, state.loaded = revision, true
		return Result{}, true
	}
	// A `list` always reads fresh (another step may have changed the list);
	// a `set` needs the revision it is replacing, so it loads once if it has not.
	if args.Action == "list" || !state.loaded {
		if res, ok := load(); !ok {
			return res
		}
	}
	if args.Action == "set" {
		entries := make([]TodoEntry, 0, len(*args.Items))
		for _, it := range *args.Items {
			entries = append(entries, TodoEntry{ID: it.ID, Content: it.Content, Status: it.Status})
		}
		revision, closedBy, err := door.Replace(ctx, state.revision, entries)
		if err != nil {
			var stale *TodoStaleError
			if errors.As(err, &stale) {
				if res, ok := load(); !ok {
					return res
				}
				return Errorf("todo set refused: %s. The list now reads:\n%s", stale.Detail, renderTodoList(state.items))
			}
			return Errorf("todo could not write the loop's list: %v", err)
		}
		held := make([]todoItem, len(*args.Items))
		copy(held, *args.Items)
		state.items, state.revision = held, revision
		content := renderTodoList(state.items)
		switch closedBy {
		case TodoClosedAllDone:
			content += "\n" + todoClosedAllDoneNote
			state.items, state.revision = nil, 0
		case TodoClosedCleared:
			content += "\n" + todoClosedClearedNote
			state.items, state.revision = nil, 0
		}
		return Result{Content: content, Summary: fmt.Sprintf("todo · set (%d items)", len(*args.Items))}
	}
	return Result{Content: renderTodoList(state.items), Summary: fmt.Sprintf("todo · list (%d items)", len(state.items))}
}
