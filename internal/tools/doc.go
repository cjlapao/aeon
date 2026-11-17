package tools

// doc.go — the `doc_*` tools: how a step files, reads and edits its deliverables.
//
// The database is where a loop's output lives (plan.md, legal.md, brief.md,
// tasks/T1.md …); a tool that knows where things are is safer than a model told
// a path. Every call is scoped to the step's own loop by the daemon — the model
// supplies a document NAME and nothing that addresses a loop, a project or a
// goal. The names are the document door's: a known name (plan, legal, …) or a
// file at the top level or one level under tasks/ or scratch/, as .md, .txt or
// .jsonl; a refusal says what is allowed.
//
// `write`, `edit` and `read` keep working on the workspace for source files.
// A deliverable written there is filed into the database by the engine when the
// step ends (loop/produced.go), so a model that reaches for the wrong tool still
// leaves its output in the store.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cjlapao/aeon/internal/model"
)

const docNameDescription = "the document's name: a known name (loop, prompt, state, todo, discovery, legal, plan, security, memory) or a file such as brief.md, or tasks/T1.md / scratch/notes.md — .md, .txt or .jsonl only, never a path"

// DocWrite files a document revision.
func DocWrite() Tool {
	return Tool{
		Class: RerunDetectable,
		Spec: model.ToolSpec{
			Name:        "doc_write",
			Description: "File a document of this loop in the database — plan.md, legal.md, brief.md, tasks/T1.md. Each write is a new revision; the earlier ones are kept. A write whose content matches the current one is dropped and says so. This is where a deliverable goes: not the workspace, which holds source code.",
			Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "name": {"type": "string", "description": "` + docNameDescription + `"},
    "content": {"type": "string", "description": "the whole document"}
  },
  "required": ["name", "content"],
  "additionalProperties": false
}`),
		},
		Run: func(ctx context.Context, tc ToolContext, input json.RawMessage) Result {
			var in struct{ Name, Content string }
			if err := decodeDocInput(input, &in); err != nil {
				return Errorf("%s", err)
			}
			docs, bad := docsOrFail(tc)
			if bad != nil {
				return *bad
			}
			entry, unchanged, err := docs.Write(ctx, in.Name, in.Content)
			if err != nil {
				return docFailure(err)
			}
			msg := "Filed " + describeEntry(entry) + "."
			if unchanged {
				msg = entry.Name + " already holds exactly this content (rev " + fmt.Sprint(entry.Rev) + "); nothing was written."
			}
			return Result{Content: msg, Summary: "doc_write " + entry.Name}
		},
	}
}

// DocRead reads the head of a document.
func DocRead() Tool {
	return Tool{
		Class: RerunSafe,
		Spec: model.ToolSpec{
			Name:        "doc_read",
			Description: "Read a document of this loop from the database. If this loop has not written it, the goal this loop works may own it (a brainstorm's brief.md, a proposal): you get that one and are told whose it is. offset and limit page by line.",
			Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "name": {"type": "string", "description": "` + docNameDescription + `"},
    "offset": {"type": "integer", "minimum": 1, "description": "first line to return (1-based); default 1"},
    "limit": {"type": "integer", "minimum": 1, "description": "most lines to return; default the whole document up to 256 KiB"}
  },
  "required": ["name"],
  "additionalProperties": false
}`),
		},
		Run: func(ctx context.Context, tc ToolContext, input json.RawMessage) Result {
			var in struct {
				Name          string
				Offset, Limit int
			}
			if err := decodeDocInput(input, &in); err != nil {
				return Errorf("%s", err)
			}
			docs, bad := docsOrFail(tc)
			if bad != nil {
				return *bad
			}
			entry, content, err := docs.Read(ctx, in.Name)
			if err != nil {
				return docFailure(err)
			}
			body, note := pageDocument(content, in.Offset, in.Limit)
			head := "# " + describeEntry(entry) + "\n"
			return Result{Content: head + body + note, Summary: "doc_read " + entry.Name}
		},
	}
}

// DocEdit replaces one exact span of a document.
func DocEdit() Tool {
	return Tool{
		Class: RerunDetectable,
		Spec: model.ToolSpec{
			Name:        "doc_edit",
			Description: "Change a document of this loop by replacing one exact piece of text with another. old must occur exactly once unless replace_all is true. The result is a new revision. Only this loop's own document can be edited — a document the goal owns is read with doc_read and written back with doc_write.",
			Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "name": {"type": "string", "description": "` + docNameDescription + `"},
    "old": {"type": "string", "description": "the exact text to replace"},
    "new": {"type": "string", "description": "the text to put in its place"},
    "replace_all": {"type": "boolean", "description": "replace every occurrence; default false"}
  },
  "required": ["name", "old", "new"],
  "additionalProperties": false
}`),
		},
		Run: func(ctx context.Context, tc ToolContext, input json.RawMessage) Result {
			var in struct {
				Name, Old, New string
				ReplaceAll     bool `json:"replace_all"`
			}
			if err := decodeDocInput(input, &in); err != nil {
				return Errorf("%s", err)
			}
			if in.Old == "" || in.Old == in.New {
				return Errorf("old must be non-empty and differ from new")
			}
			docs, bad := docsOrFail(tc)
			if bad != nil {
				return *bad
			}
			entry, content, err := docs.Read(ctx, in.Name)
			if err != nil {
				return docFailure(err)
			}
			if entry.Inherited {
				return Errorf("%s belongs to goal %s and was written by loop %s; read it with doc_read and file this loop's version with doc_write", entry.Name, entry.Goal, entry.Loop)
			}
			updated, rerr := replaceInDocument(entry.Name, content, in.Old, in.New, in.ReplaceAll)
			if rerr != nil {
				return Errorf("%s", rerr)
			}
			written, unchanged, err := docs.Write(ctx, in.Name, updated)
			if err != nil {
				return docFailure(err)
			}
			if unchanged {
				return Result{Content: entry.Name + " is unchanged.", Summary: "doc_edit " + entry.Name}
			}
			return Result{Content: "Edited " + describeEntry(written) + ".", Summary: "doc_edit " + entry.Name}
		},
	}
}

// DocList lists the documents the step can read.
func DocList() Tool {
	return Tool{
		Class: RerunSafe,
		Spec: model.ToolSpec{
			Name:        "doc_list",
			Description: "List the documents of this loop and, under their own heading, those owned by the goal this loop works — each with its revision and size. Use it to see what exists before reading or writing.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
		},
		Run: func(ctx context.Context, tc ToolContext, input json.RawMessage) Result {
			if err := decodeDocInput(input, &struct{}{}); err != nil {
				return Errorf("%s", err)
			}
			docs, bad := docsOrFail(tc)
			if bad != nil {
				return *bad
			}
			entries, err := docs.List(ctx)
			if err != nil {
				return docFailure(err)
			}
			if len(entries) == 0 {
				return Result{Content: "No documents yet.", Summary: "doc_list"}
			}
			var own, goal []string
			for _, e := range entries {
				if e.Inherited {
					goal = append(goal, "  "+describeEntry(e))
				} else {
					own = append(own, "  "+describeEntry(e))
				}
			}
			var b strings.Builder
			if len(own) > 0 {
				b.WriteString("This loop's documents:\n" + strings.Join(own, "\n") + "\n")
			}
			if len(goal) > 0 {
				b.WriteString("Owned by the goal this loop works:\n" + strings.Join(goal, "\n") + "\n")
			}
			return Result{Content: strings.TrimRight(b.String(), "\n"), Summary: fmt.Sprintf("doc_list · %d", len(entries))}
		},
	}
}

// replaceInDocument swaps one exact span (or every occurrence) and says, in the
// words the model can act on, why it could not.
func replaceInDocument(name, content, old, replacement string, replaceAll bool) (string, error) {
	switch n := strings.Count(content, old); {
	case n == 0:
		return "", fmt.Errorf("old was not found in %s; read it again and copy the text exactly", name)
	case n > 1 && !replaceAll:
		return "", fmt.Errorf("old occurs %d times in %s; add surrounding text to make it unique, or set replace_all", n, name)
	}
	limit := 1
	if replaceAll {
		limit = -1
	}
	return strings.Replace(content, old, replacement, limit), nil
}

// decodeDocInput is strict: a field the schema does not name is an error, so a
// model cannot smuggle a loop or a goal in beside the name.
func decodeDocInput(input json.RawMessage, into any) error {
	if len(input) == 0 {
		input = json.RawMessage(`{}`)
	}
	dec := json.NewDecoder(strings.NewReader(string(input)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		return fmt.Errorf("invalid input: %v", err)
	}
	return nil
}

// docFailure turns a refusal into the sentence the model reads, and anything
// else into a plain failure.
func docFailure(err error) Result {
	var refused *DocRefused
	if errors.As(err, &refused) {
		return Errorf("refused: %s", refused.Reason)
	}
	return Errorf("document store: %v", err)
}

// pageDocument returns the lines asked for, bounded by ReadMaxBytes, and a
// trailing note when it stopped early so the model knows where to resume.
func pageDocument(content string, offset, limit int) (body, note string) {
	lines := strings.Split(content, "\n")
	start := 0
	if offset > 1 {
		start = offset - 1
	}
	if start >= len(lines) {
		return "", fmt.Sprintf("\n(the document has %d lines; offset %d is past the end)", len(lines), offset)
	}
	end := len(lines)
	if limit > 0 && start+limit < end {
		end = start + limit
	}
	var b strings.Builder
	size := 0
	for i := start; i < end; i++ {
		if size+len(lines[i])+1 > ReadMaxBytes && i > start {
			end = i
			break
		}
		b.WriteString(lines[i])
		b.WriteByte('\n')
		size += len(lines[i]) + 1
	}
	if end < len(lines) {
		note = fmt.Sprintf("\n(showing lines %d-%d of %d; call again with offset %d for the rest)", start+1, end, len(lines), end+1)
	}
	return strings.TrimSuffix(b.String(), "\n"), note
}
