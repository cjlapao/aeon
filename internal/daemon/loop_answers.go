package daemon

// loop_answers.go — the person's answers, filed as a document.
//
// An agent that needs a decision asks the person with the `question` tool, and the answer comes back to THAT agent
// only. The next agent (a planner, a builder) starts with a fresh context and no memory of the exchange, so it either
// plans without the answer or reports that the answer round never happened. Filing every answered question as the
// loop's `answers.md` the moment it is answered puts the decisions where every later agent looks: in the database,
// listed in the loop's index. The document is rebuilt from the question rows each time, so it is always the whole
// record, in order, and a rebuild that changes nothing writes nothing.

import (
	"fmt"
	"strings"

	"github.com/cjlapao/aeon/internal/store"
)

type answersLogger interface{ Infof(string, ...any) }

// answersDocName is the document the answers are filed under.
const answersDocName = "answers.md"

// fileLoopAnswers rebuilds answers.md for the loop that owns the step. A failure is logged, never returned: the
// answer is already recorded and the step already resumed; the document is the convenience that failed.
func fileLoopAnswers(s *store.Store, stepID int64, log answersLogger) {
	step, err := s.ReadStep(stepID)
	if err != nil || step.LoopSlug == "" {
		return
	}
	fileAnswersFor(s, store.Scope{LoopSlug: step.LoopSlug, ComponentRoot: step.ComponentRoot}, log)
}

// fileGateAnswers does the same after a person answers a blueprint gate (the plan gate's approve / revise / abort):
// the decision is the loop's, and the planner that is sent back to revise must be able to read that it was asked to.
func fileGateAnswers(s *store.Store, loopID, answer, feedback string, log answersLogger) {
	lp, err := s.ReadLoopByID(loopID)
	if err != nil {
		return
	}
	sc := store.Scope{LoopSlug: lp.Slug, ComponentRoot: lp.ComponentRoot}
	if fb := strings.TrimSpace(feedback); fb != "" {
		// What the person asked to change is kept, one entry per request, and rebuilt into answers.md below.
		prior := ""
		if d, err := s.ReadDocument(sc, revisionNotesDoc); err == nil {
			prior = strings.TrimRight(d.Content, "\n") + "\n\n"
		}
		n := strings.Count(prior, "## Revision request ") + 1
		entry := fmt.Sprintf("## Revision request %d (%s)\n\n%s\n", n, strings.TrimSpace(answer), fb)
		if prior == "" {
			prior = "# What the person asked to change\n\nEach entry is a person's instruction when they sent your work back. Do exactly this; do not revise by your own judgment beyond it.\n\n"
		}
		if _, err := s.WriteDocument(sc, revisionNotesDoc, prior+entry, "operator"); err != nil {
			log.Infof("revision notes for %s not filed: %v", sc.LoopSlug, err)
		}
	}
	fileAnswersFor(s, sc, log)
}

// revisionNotesDoc keeps the person's instructions from each time they sent a gate's work back.
const revisionNotesDoc = "revision-notes.md"

func fileAnswersFor(s *store.Store, sc store.Scope, log answersLogger) {
	qs, err := s.AllLoopQuestions(sc)
	if err != nil {
		log.Infof("answers.md for %s not filed: %v", sc.LoopSlug, err)
		return
	}
	body := renderAnswers(s, qs)
	if d, err := s.ReadDocument(sc, revisionNotesDoc); err == nil && strings.TrimSpace(d.Content) != "" {
		// The person's revision instructions ride in the same file every agent already reads first.
		body = strings.TrimRight(body, "\n") + "\n\n---\n\n" + strings.TrimSpace(d.Content) + "\n"
	}
	if body == "" {
		return
	}
	if _, err := s.WriteDocument(sc, answersDocName, body, "operator"); err != nil {
		log.Infof("answers.md for %s not filed: %v", sc.LoopSlug, err)
	}
}

// renderAnswers is the document text: every answered, non-permission question of the loop, oldest first. Empty when
// there is none.
func renderAnswers(s *store.Store, qs []store.Question) string {
	var b strings.Builder
	n := 0
	for _, q := range qs {
		if q.Kind == "permission" || !q.Answered() {
			continue
		}
		if n == 0 {
			b.WriteString("# Answers\n\nWhat the person decided when an agent asked. Filed automatically as each answer is given. Treat these as settled: do not ask again, and do not plan around a different choice.\n")
		}
		n++
		who := "an agent"
		if q.StepID == nil {
			who = "the blueprint (a gate)"
		}
		if q.StepID != nil {
			if st, err := s.ReadStep(*q.StepID); err == nil && st.Agent != "" {
				who = st.Agent
				if st.Phase != "" {
					who += " (" + st.Phase + ")"
				}
			}
		}
		fmt.Fprintf(&b, "\n## %d. Asked by %s\n\n**Question:** %s\n\n**Answer:** %s\n\n_Answered %s._\n", n, who, strings.TrimSpace(q.Body), strings.TrimSpace(q.Answer), q.AnsweredAt)
	}
	return b.String()
}

// announceResumed tells every subscribed screen the loop is running again the moment its parked step's question is
// answered. Nothing else published a loop event at that point, so a list or a rail that had read `waiting` kept it until
// the next unrelated loop event. The view each reader gets already reads the loop as running (resumedAsRunning).
func (d *Daemon) announceResumed(stepID int64) {
	if d.store == nil {
		return
	}
	step, err := d.store.ReadStep(stepID)
	if err != nil || step.LoopSlug == "" {
		return
	}
	l, err := d.store.GetLoop(store.Scope{LoopSlug: step.LoopSlug, ComponentRoot: step.ComponentRoot})
	if err != nil || l.ID == "" {
		return
	}
	d.publishLoop("loop.resumed", map[string]any{"loopId": l.ID, "slug": l.Slug, "reason": "answered", "stepId": stepID})
}
