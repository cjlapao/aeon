package daemon

// records_tasks.go — the task arms of the management catalog and the two
// rules only they enforce (S38 T9's one implementation, split for SIZE1 in
// S47 T7 by cohesion: this file is the task family). `ownTask` is OL14 as
// code — a loop task belongs to the loop that planned it, a manual task to a
// loop LINKED to it as a worker — and `pinWorkspaceDocument` is why a step's
// `attach` can pin a file a person cannot: the document is an act of the
// step's workspace, and the path is resolved under the step's own root.

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"

	"github.com/cjlapao/aeon/internal/plan"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/tools"
)

// taskCreate files a task through the step's identity. Inside a loop it is a
// LOOP task — the plan's id, tech, engineer (default: the step's own agent,
// OL14's "assignee that changes as it climbs" starting at the planner's
// hand); `manual: true` exists only in a plain chat (isPlainChat above).
// Dependencies are validated BEFORE the insert against the loop's existing
// tasks, and the refusal speaks plan's own sentence — the one renderer, so
// `aeon loop`, the planner's transcript and this door all say the same words.
func (r *RecordsService) taskCreate(ident Identity, by Actor, input json.RawMessage) (any, *Failure, error) {
	if ident.User {
		// A hand-made task is a person's act (OL14): the person names the
		// component and the assignee — the two things a step never gets to
		// choose — and it arrives manual, because a person hand-making a
		// task IS the manual case. The agent's decode below has no field
		// for any of it, so the same keys arriving from a step are parse
		// refusals (OL3), not overrides.
		return r.taskCreateAsPerson(by, input)
	}
	var in struct {
		ID        string   `json:"id"`
		Title     string   `json:"title"`
		Tech      string   `json:"tech"`
		Engineer  string   `json:"engineer"`
		DependsOn []string `json:"depends_on"`
		Parent    string   `json:"parent"`
		Manual    *bool    `json:"manual"`
		Type      string   `json:"type"`
		BodyNote  string   `json:"body_note"`
	}
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	if in.Title == "" {
		return nil, &Failure{Sentence: "refused: a task needs a title — what it does, not that it exists"}, nil
	}
	sc := store.Scope{LoopSlug: ident.LoopSlug, ComponentRoot: ident.ComponentRoot}
	if in.Manual != nil && *in.Manual {
		if !isPlainChat(ident) {
			return nil, &Failure{Sentence: fmt.Sprintf("refused: task %q asks to be manual, but loop %s runs a blueprint — only a plain chat hand-makes tasks (OL14)", in.ID, ident.LoopSlug)}, nil
		}
		// S40 T4 (T2's flagged line): the agent's write carries its CAUSE.
		// OriginLoopID is the step's own loop — the store door reads that
		// row for depth and trigger (AU5: depth is a property of the loop,
		// never an input number). A person (ident.User) has no cause, and
		// "" means exactly that; an agent whose loop row cannot be read is
		// a plain failure, not a write silently attributed to a person —
		// the depth-lie AU5 forbids starts with a lost origin.
		origin := ""
		if !ident.User {
			lb, lerr := r.st.GetLoop(sc)
			if lerr != nil {
				return nil, nil, fmt.Errorf("records: the cause of task %q is its loop %s, and that loop cannot be read: %w", in.Title, ident.LoopSlug, lerr)
			}
			origin = lb.ID
		}
		t, rf, err := r.st.CreateManualTask(store.NewManualTask{ComponentID: ident.ComponentID, Title: in.Title,
			Type: in.Type, Body: in.BodyNote, ParentTaskID: in.Parent,
			AssigneeKind: "agent", Assignee: orDefaultStr(in.Engineer, ident.Agent),
			By: store.Actor{Kind: by.Kind, ID: by.ID}, SourceStep: &ident.StepID, OriginLoopID: origin})
		if err != nil {
			return nil, nil, err
		}
		if rf != nil {
			return nil, &Failure{Sentence: rf.Sentence}, nil
		}
		if r.taskChanged != nil {
			r.taskChanged(t, "")
		}
		return t, nil, nil
	}
	// A loop task. The plan's id is the engine's key; when omitted the loop's
	// own ordinal continues it (T1, T2…) so a conversational agent can file
	// one task without inventing a namespace.
	taskID := in.ID
	existing, err := r.st.ListTasks(sc)
	if err != nil {
		return nil, nil, err
	}
	if taskID == "" {
		taskID = fmt.Sprintf("T%d", len(existing)+1)
	}
	have := map[string]bool{}
	for _, t := range existing {
		if t.TaskID == taskID {
			return nil, &Failure{Sentence: (&plan.Refusal{Kind: "duplicate-id", Task: taskID, File: "task_create"}).Sentence()}, nil
		}
		have[t.TaskID] = true
	}
	if f := r.checkDepends(taskID, in.DependsOn, have, "task_create"); f != nil {
		return nil, f, nil
	}
	engineer := in.Engineer
	if engineer == "" {
		engineer = ident.Agent // the assignee starts as the agent that planned it
	}
	t, err := r.st.CreateTask(sc, store.NewTask{TaskID: taskID, Ord: len(existing) + 1, Title: in.Title,
		Tech: in.Tech, Engineer: engineer, DependsOn: in.DependsOn, ParentTaskID: in.Parent})
	if err != nil {
		return nil, nil, err
	}
	if r.taskChanged != nil {
		r.taskChanged(t, "")
	}
	return t, nil, nil
}

// checkDepends is plan's rule applied one task at a time, rendered by plan's
// ONE formatter (a self-typed sentence would be a second truth for the same
// refusal).
func (r *RecordsService) checkDepends(taskID string, deps []string, have map[string]bool, file string) *Failure {
	for _, d := range deps {
		if d == taskID {
			return &Failure{Sentence: (&plan.Refusal{Kind: "self-dependency", Task: taskID, Depend: d, File: file}).Sentence()}
		}
		if !have[d] {
			return &Failure{Sentence: (&plan.Refusal{Kind: "depends-on-missing", Task: taskID, Depend: d, File: file}).Sentence()}
		}
	}
	return nil
}

func (r *RecordsService) taskUpdate(ident Identity, input json.RawMessage) (any, *Failure, error) {
	var in struct {
		ID        string   `json:"id"`
		Title     string   `json:"title"`
		Tech      string   `json:"tech"`
		DependsOn []string `json:"depends_on"`
	}
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	t, f := r.ownTask(ident, in.ID)
	if f != nil {
		return nil, f, nil
	}
	if ident.User {
		// The person's edit rule is the mirror of the agent's below: manual
		// rows are the author's fields to edit (and the author is a person);
		// loop rows are the engine's — T7's refusal, same sentence shape.
		if !t.Manual {
			return nil, &Failure{Sentence: fmt.Sprintf("refused: task %s belongs to loop %s — its fields are the engine's door (UpdateLoopTask), not this one", in.ID, t.LoopSlug)}, nil
		}
		edited, rf, err := r.st.UpdateManualTask(store.ManualTaskEdit{ID: t.ID, Title: in.Title})
		if err != nil {
			return nil, nil, err
		}
		if rf != nil {
			return nil, &Failure{Sentence: rf.Sentence}, nil
		}
		if r.taskChanged != nil {
			r.taskChanged(edited, t.Status)
		}
		return edited, nil, nil
	}
	if t.Manual {
		// A manual task this loop WORKS: the person's doors own its fields;
		// the agent keeps only what the engine needs — refuse with the
		// sentence OL14 reserves for people, adapted to name the door.
		return nil, &Failure{Sentence: fmt.Sprintf("refused: task %s is a manual task — its fields are the author's to edit; this loop may only work its status", in.ID)}, nil
	}
	sc := store.Scope{LoopSlug: t.LoopSlug, ComponentRoot: t.ComponentRoot}
	if in.DependsOn != nil {
		existing, err := r.st.ListTasks(sc)
		if err != nil {
			return nil, nil, err
		}
		have := map[string]bool{}
		for _, x := range existing {
			if x.TaskID != t.TaskID { // a dependency on itself is plan's class, refused as such
				have[x.TaskID] = true
			}
		}
		if f := r.checkDepends(t.TaskID, in.DependsOn, have, "task_update"); f != nil {
			return nil, f, nil
		}
	}
	edited, err := r.st.UpdateLoopTask(sc, t.TaskID, store.LoopTaskEdit{Title: in.Title, Tech: in.Tech, DependsOn: in.DependsOn})
	if err != nil {
		return nil, nil, err
	}
	if r.taskChanged != nil {
		r.taskChanged(edited, t.Status)
	}
	return edited, nil, nil
}

func (r *RecordsService) taskSetStatus(ident Identity, input json.RawMessage) (any, *Failure, error) {
	var in struct {
		ID       string `json:"id"`
		Status   string `json:"status"`
		Evidence string `json:"evidence"`
	}
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	t, f := r.ownTask(ident, in.ID)
	if f != nil {
		return nil, f, nil
	}
	if ident.User {
		// A person moves MANUAL rungs, through the manual door — which is
		// the one that speaks T7's sentence for a loop task itself. The
		// engine's SetTaskStatus is not this door's to open.
		res, rf, err := r.st.SetManualTaskStatus(t.ID, store.TaskStatus(in.Status))
		if err != nil {
			return nil, nil, err
		}
		if rf != nil {
			return nil, &Failure{Sentence: rf.Sentence}, nil
		}
		if !res.OK {
			return nil, &Failure{Sentence: res.Transition.Reason}, nil
		}
		if r.taskChanged != nil {
			r.taskChanged(res.Task, t.Status)
		}
		return res.Task, nil, nil
	}
	if t.Manual {
		res, rf, err := r.st.SetManualTaskStatus(t.ID, store.TaskStatus(in.Status))
		if err != nil {
			return nil, nil, err
		}
		if rf != nil {
			return nil, &Failure{Sentence: rf.Sentence}, nil
		}
		if !res.OK {
			return nil, &Failure{Sentence: res.Transition.Reason}, nil
		}
	} else {
		sc := store.Scope{LoopSlug: t.LoopSlug, ComponentRoot: t.ComponentRoot}
		res, err := r.st.SetTaskStatus(sc, t.TaskID, store.TaskStatus(in.Status))
		if err != nil {
			return nil, nil, err
		}
		if !res.OK {
			return nil, &Failure{Sentence: res.Transition.Reason}, nil
		}
	}
	if in.Evidence != "" {
		if err := r.st.SetTaskEvidence(t.ID, in.Evidence); err != nil {
			_ = err // evidence is evidence; the status already moved — say so rather than pretend the rung failed
		}
	}
	fresh, err := r.st.GetTaskByID(t.ID)
	if err != nil {
		return nil, nil, err
	}
	if r.taskChanged != nil {
		r.taskChanged(fresh, t.Status)
	}
	return fresh, nil, nil
}

func (r *RecordsService) taskBump(ident Identity, input json.RawMessage) (any, *Failure, error) {
	var in struct {
		ID      string `json:"id"`
		Counter string `json:"counter"`
	}
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	t, f := r.ownTask(ident, in.ID)
	if f != nil {
		return nil, f, nil
	}
	if t.Manual {
		return nil, &Failure{Sentence: fmt.Sprintf("refused: task %s is a manual task — its ladder's counters belong to the loop that runs it, not to this call", in.ID)}, nil
	}
	sc := store.Scope{LoopSlug: t.LoopSlug, ComponentRoot: t.ComponentRoot}
	value, capped, err := r.st.BumpTaskCounter(sc, t.TaskID, in.Counter, 0)
	if err != nil {
		return nil, &Failure{Sentence: fmt.Sprintf("refused: %v", err)}, nil
	}
	return map[string]any{"counter": in.Counter, "value": value, "capped": capped}, nil, nil
}

// task belongs to the loop that planned it; a manual task may be worked only
// by a loop LINKED to it as a worker. Any other id is refused naming the
// task — the sentence the granted caller needs to stop guessing.
func (r *RecordsService) ownTask(ident Identity, id string) (store.Task, *Failure) {
	t, err := r.st.GetTaskByID(id)
	if err != nil {
		return store.Task{}, notFoundFailure(id, err)
	}
	if ident.User {
		// The person's board rule (OL14) is enforced where the edit happens
		// — task_update and task_set_status answer the manual-only question
		// with T7's sentence. What must NOT happen here is a loop-scope
		// check against a loop no person carries.
		return t, nil
	}
	if !t.Manual {
		if t.LoopSlug != ident.LoopSlug || t.ComponentRoot != ident.ComponentRoot {
			return store.Task{}, &Failure{Sentence: fmt.Sprintf("refused: task %q is not owned by this loop — it belongs to %s", id, t.LoopSlug)}
		}
		return t, nil
	}
	worker, err := r.st.IsLoopWorkerOfTask(id, store.Scope{LoopSlug: ident.LoopSlug, ComponentRoot: ident.ComponentRoot})
	if err != nil {
		return store.Task{}, &Failure{Sentence: fmt.Sprintf("refused: the worker link of %s could not be read (%v) — scope is checked, not assumed", id, err)}
	}
	if !worker {
		return store.Task{}, &Failure{Sentence: fmt.Sprintf("refused: task %q is a manual task and loop %s is not a worker on it — start the loop on the task first (task_loop)", id, ident.LoopSlug)}
	}
	return t, nil
}

// pinWorkspaceDocument is the bridge between the two memories a lane lives
// in (S38 T14): the `write` tool files bytes in the workspace, and an
// `attach` pins a document ROW — and until now nothing carried a lane's own
// file across that gap, so a blueprint whose contract says "attach what you
// wrote" could never pass. The bridge honours the sentence the refusal has
// always told — an attachment pins what exists, never a hope: the row is
// written ONLY from bytes that exist on this loop's workspace disk, read
// through the same resolver every file tool goes through, capped like every
// write, and stamped with the calling agent. A name the document resolver
// refuses, a file that is not there, a file that escapes the workspace or
// outgrows the cap: no row, and the ordinary document-unknown refusal tells
// the person what is missing. The lane acts only on its own loop and
// component — the scope is the identity's, never the argument's.
func (r *RecordsService) pinWorkspaceDocument(sc store.Scope, ident Identity, name, byAgent string) *Failure {
	resolved := store.ResolveDocumentName(name)
	if !resolved.OK() {
		return nil // Attach refuses refused names in its own words.
	}
	head, headErr := r.st.ReadDocument(sc, name)
	abs, ok := workspaceFileUnder(ident.ComponentRoot, resolved.Name)
	if !ok {
		return nil // nothing on disk to pin — the store's refusal is the honest answer.
	}
	raw, err := os.ReadFile(abs)
	if err != nil || len(raw) > maxRecordDocumentBytes {
		return nil
	}
	if headErr == nil && head.SHA256 == fmt.Sprintf("%x", sha256.Sum256(raw)) {
		return nil // the head already pins these exact bytes.
	}
	if _, err := r.st.WriteDocument(sc, resolved.Name, string(raw), byAgent); err != nil {
		return &Failure{Sentence: fmt.Sprintf("refused: %s exists in the workspace but could not be pinned as a document: %v", resolved.Name, err)}
	}
	return nil
}

// maxRecordDocumentBytes matches the write tool's 1 MiB cap — a document a
// step could not have written with its own tools is not a document this door
// pins from disk.
const maxRecordDocumentBytes = 1 << 20

// workspaceFileUnder resolves a document name against the loop's workspace
// through the ONE path resolver the tools use, and requires a regular file.
func workspaceFileUnder(root, name string) (string, bool) {
	res := tools.Resolve(root, name)
	if !res.OK {
		return "", false
	}
	fi, err := os.Stat(res.Abs)
	if err != nil || !fi.Mode().IsRegular() {
		return "", false
	}
	return res.Abs, true
}
