// triage_file_as.go — S51 T5 (TM6): the triage plane's ONE agent door.
//
// triage.fileAs files an UNVALIDATED, priority-unknown bug claim through the
// SAME Create the person's triage.file runs — the one filing an agent may
// make of the backlog (T4's guard family pinned priority, duplicate,
// validation and hand-off as the person's doors alone; this is not a way
// around them, the shape itself has nowhere to carry them). The door
// dispatches to the same RecordsAPI.Do the tool door calls, so the socket
// answer and the tool answer are one answer, not two renderings of it; the
// filer is the stamped AGENT SLUG, decided service-side from the step's
// identity — params carry no filedBy and no identity fields (TM3, OL3).
//
// V13: registered when and only when the records service stands — the
// service is what honours what makes the door safe (the stamped agent
// actor, the per-agent open-claims cap, the refuse-not-fallback shape), so
// without Table.Records the method is ABSENT (ErrUnknownMethod), never
// registered-and-refusing.
//
// A PERSON session is refused, by name, with the pointer sentence — a
// person files through triage.file, under themselves, not through a step.

package protocol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cjlapao/aeon/internal/triage"
)

// MethodTriageFileAs is the agent twin's name: file AS the step's agent,
// with the filing shape the agent may carry and nothing else.
const MethodTriageFileAs MethodName = "triage.fileAs"

// triageFileTool is the catalog tool name the twin dispatches under
// (RecordsAPI.Do takes the tool name — the catalog's vocabulary, not a
// second method table).
const triageFileTool = "bug_file"

// TriageFileAsParams is the twin's documented params — the catalog
// schema's shape as a Go type. WHICH CLAIM (the root it belongs to, the
// title, the body) and nothing else: no validation state (the store's
// default is the backstop — an agent's claim is unvalidated by definition),
// no priority (an agent's claim reads unknown, D2's honest absence), no
// filedBy (the stamped slug is the filer; TM3), no duplicate/handoff fields
// (the person's doors, TM6). A smuggled field meets the strict decode and
// its refusal names it.
type TriageFileAsParams struct {
	ComponentRoot string `json:"componentRoot"`
	Title         string `json:"title"`
	Body          string `json:"body"`
}

// RegisterTriageFileAs registers the twin when (and only when) the records
// service is wired — the RegisterRoadmapProposeIdea/V13 rule. Returns true
// when it stands (already or just now); false when the seam is missing.
func (t *Table) RegisterTriageFileAs() bool {
	if t.Records == nil {
		return false
	}
	if _, ok := t.methods[MethodTriageFileAs]; ok {
		return true // already standing (a re-registration is a no-op, not a second door)
	}
	t.register(&Method{
		Name:               MethodTriageFileAs,
		RequiresInitialize: true,
		Handle: func(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
			// One definition, two doors: the SAME RecordsAPI.Do the tool path
			// calls. A person session is refused there — in the service's
			// own words ("triage.file is the person's door") — the actor is
			// the stamped step, never decoded from params (OL3).
			out, sentence, err := t.Records.Do(ctx, sess, triageFileTool, params)
			if err != nil {
				return nil, err
			}
			if sentence != "" {
				return nil, ErrBadParams{Err: errors.New(sentence)}
			}
			return out, nil
		},
	})
	return true
}

// AnswerTriageShow is triage.show's ONE rendering — the shared answer the
// socket door (handleTriageShow) and the records service's bug_show arm
// both run, so the tool answer and the socket answer cannot drift field
// for field (rule 4). The unknown-row refusal rides in the plane's words.
func AnswerTriageShow(tr *triage.Triage, id int64) (any, error) {
	b, ok, err := tr.Show(id)
	if err != nil {
		return nil, asTriageRefusal(err)
	}
	if !ok {
		return nil, triageRefusal(fmt.Sprintf("triage: bug %d does not exist", id))
	}
	return toTriageView(b), nil
}

// AnswerTriageFileAs files the agent's claim through the SAME
// CreateAudited the person's triage.file runs — and ONLY through it. The
// bug arrives unvalidated (the store's default; nothing here chooses a
// validation state) and priority-unknown (triagePriorityState("") is the
// honest absence, D2); filed_by is the stamped actor's id (the agent slug,
// TM3); the audit carries op='file' with actor_kind='agent' (the store's
// actor law, T1's CHECK, is what makes the kind mechanical). The rendering
// is the one view — toTriageView — so the filed row reads exactly as
// triage.show will read it a moment later.
func AnswerTriageFileAs(tr *triage.Triage, actor triage.Actor, p TriageFileAsParams) (any, error) {
	if p.ComponentRoot == "" || p.Title == "" || p.Body == "" {
		return nil, ErrBadParams{Err: errors.New("componentRoot, title and body are required — the agent's claim carries what it SAW and nothing else (TM6)")}
	}
	b, ref := tr.CreateAudited(p.ComponentRoot, triage.NewBug{
		Title:         p.Title,
		Body:          p.Body,
		PriorityState: triagePriorityState(""),
		FiledBy:       actor.ID,
	}, actor)
	if ref != nil {
		return nil, triageRefusal(ref.Reason)
	}
	return toTriageView(b), nil
}
