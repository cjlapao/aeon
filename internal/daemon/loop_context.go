package daemon

// loop_context.go — the loop's index, rendered for an agent.
//
// What a loop holds lives in the database: its documents, its tasks (and each task's spec file), the answers the
// person gave to its questions, the memory it filed. An agent that is not told this looks for plan.md on disk, finds
// nothing, and reports "no plan" — which is how a planner failed on a loop whose discovery and clarified brief were
// both on file. This service renders one deterministic index of all of it. It is injected into every loop step's
// system context at compile (StepRunner.Compile) and served again, fresh, by the `loop_context` tool.

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/stepctx"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/tools"
)

// LoopContextService renders a loop's index. Nil when the daemon has no store.
type LoopContextService struct {
	store *store.Store
	docs  *DocService
}

// NewLoopContextService builds the service; a nil store yields nil.
func NewLoopContextService(s *store.Store, docs *DocService) *LoopContextService {
	if s == nil {
		return nil
	}
	return &LoopContextService{store: s, docs: docs}
}

// loopContextCap bounds the whole index. A loop with hundreds of documents gets the most recent ones and a count of
// the rest, never an unbounded prompt.
const (
	loopContextCap   = 12 << 10
	loopContextItems = 40
)

// Render implements tools.LoopContextSource for the step on the context.
func (l *LoopContextService) Render(ctx context.Context) (string, error) {
	id, ok := stepctx.StepID(ctx)
	if !ok {
		return "", errors.New("the loop's context needs a step on the context, and this call carries none")
	}
	st, err := l.store.ReadStep(id)
	if err != nil {
		return "", fmt.Errorf("read the step %d that asks for its loop's context: %w", id, err)
	}
	return l.RenderFor(ctx, st.LoopSlug, st.ComponentRoot, st.Agent, nil), nil
}

var _ tools.LoopContextSource = (*LoopContextService)(nil)

// RenderFor renders the index for one loop and agent. granted is the agent's tool grant when known (the compile has
// it; the tool re-reads it from nothing and omits the tools section). An empty slug renders nothing.
func (l *LoopContextService) RenderFor(ctx context.Context, slug, root, agent string, granted []string) string {
	if l == nil || slug == "" || root == "" {
		return ""
	}
	sc := store.Scope{LoopSlug: slug, ComponentRoot: root}
	var b strings.Builder
	b.WriteString("This loop's files are in the database, not on disk: deliverables such as plan.md, discovery.md, answers.md and tasks/T1.md are documents. Read one with `doc_read` (name as listed below), list them with `doc_list`, file one with `doc_write`. Do not look for them in the workspace. This index is a snapshot from when your step started; call `loop_context` for a fresh one.\n")

	if lp, err := l.store.GetLoop(sc); err == nil {
		b.WriteString("\n### Loop\n")
		fmt.Fprintf(&b, "- %s (%s) · status %s · phase %s", orDash(lp.Title), slug, orDash(lp.Status), orDash(lp.Phase))
		if lp.Mode != "" {
			fmt.Fprintf(&b, " · mode %s", lp.Mode)
		}
		b.WriteString("\n")
		if lp.FeatureBranch != "" {
			fmt.Fprintf(&b, "- feature branch %s (base %s)\n", lp.FeatureBranch, orDash(lp.BaseBranch))
		}
	}

	if docs, err := l.store.ListDocuments(sc); err == nil {
		if first := l.readFirst(ctx, sc, docs); len(first) > 0 {
			b.WriteString("\n### Read these first (they feed this step)\n")
			for _, f := range first {
				fmt.Fprintf(&b, "- %s\n", f)
			}
		}
		b.WriteString("\n### Documents (name · revision · bytes · written by)\n")
		if len(docs) == 0 {
			b.WriteString("- none yet\n")
		}
		sort.SliceStable(docs, func(i, j int) bool { return docs[i].File < docs[j].File })
		for i, d := range docs {
			if i == loopContextItems {
				fmt.Fprintf(&b, "- … and %d more (`doc_list` shows all)\n", len(docs)-i)
				break
			}
			fmt.Fprintf(&b, "- %s · rev %d · %d bytes · %s\n", d.File, d.Rev, d.Bytes, orDash(d.WrittenBy))
		}
	}

	if tasks, err := l.store.ListTasks(sc); err == nil && len(tasks) > 0 {
		b.WriteString("\n### Tasks (id · status · engineer · depends on · title)\n")
		have := map[string]bool{}
		if docs, err := l.store.ListDocuments(sc); err == nil {
			for _, d := range docs {
				have[d.File] = true
			}
		}
		for i, t := range tasks {
			if i == loopContextItems {
				fmt.Fprintf(&b, "- … and %d more\n", len(tasks)-i)
				break
			}
			deps := "-"
			if len(t.DependsOn) > 0 {
				deps = strings.Join(t.DependsOn, ",")
			}
			spec := ""
			if have["tasks/"+t.TaskID+".md"] {
				spec = " · spec: tasks/" + t.TaskID + ".md"
			}
			fmt.Fprintf(&b, "- %s · %s · %s · %s · %s%s\n", t.TaskID, orDash(t.Status), orDash(t.Engineer), deps, orDash(t.Title), spec)
		}
	}

	if qs, err := l.store.AllLoopQuestions(sc); err == nil {
		var answered, open []store.Question
		for _, q := range qs {
			if q.Kind == "permission" {
				continue
			}
			if q.Answered() {
				answered = append(answered, q)
			} else {
				open = append(open, q)
			}
		}
		if len(answered) > 0 {
			b.WriteString("\n### Answers the person gave (also filed as `answers.md`)\n")
			for i, q := range answered {
				if i == loopContextItems {
					fmt.Fprintf(&b, "- … and %d more\n", len(answered)-i)
					break
				}
				fmt.Fprintf(&b, "- Q: %s\n  A: %s\n", ctxLine(q.Body, 240), ctxLine(q.Answer, 240))
			}
		}
		if len(open) > 0 {
			b.WriteString("\n### Questions still open\n")
			for _, q := range open {
				fmt.Fprintf(&b, "- %s\n", ctxLine(q.Body, 240))
			}
		}
	}

	if n, err := l.store.LearningsScopedTo(slug); err == nil && n > 0 {
		fmt.Fprintf(&b, "\n### Memory\n- %d learning(s) filed for this loop; the loop's `memory.md` document (if listed above) is its working memory.\n", n)
	}

	if len(granted) > 0 {
		b.WriteString("\n### Tools you hold\n")
		names := append([]string(nil), granted...)
		sort.Strings(names)
		reg := tools.Registry()
		for _, name := range names {
			if t, ok := reg[name]; ok {
				fmt.Fprintf(&b, "- %s — %s\n", name, firstSentence(t.Spec.Description, 120))
			} else {
				fmt.Fprintf(&b, "- %s\n", name)
			}
		}
	}

	for _, name := range granted {
		if name == "graph" || name == "search" {
			b.WriteString("\nBefore you edit code, use `graph` (who calls what, the package structure) and `search` (the code corpus) instead of guessing with grep alone, and say what they returned. If either answers that its index is not built, say so to the person.\n")
			break
		}
	}

	out := b.String()
	if len(out) > loopContextCap {
		out = out[:loopContextCap] + "\n… (index cut at its size cap; `loop_context` and `doc_list` show the rest)\n"
	}
	return out
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

func ctxLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}

func firstSentence(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if i := strings.Index(s, ". "); i > 0 {
		s = s[:i+1]
	}
	return ctxLine(s, max)
}

// readFirst names the documents this step is meant to start from: what the lanes before it declared they produce
// and that exist, the person's answers, the clarified brief, and — inside a build lane — the task's own spec. The
// blueprint knows which lane feeds which; an agent that is not told reads whatever it happens to find, or nothing.
func (l *LoopContextService) readFirst(ctx context.Context, sc store.Scope, docs []store.Document) []string {
	have := map[string]bool{}
	for _, d := range docs {
		have[d.File] = true
	}
	lp, err := l.store.GetLoop(sc)
	if err != nil {
		return readFirstFrom(have, nil, "", "")
	}
	st, err := loop.Pinned(ctx, l.store, lp.ID)
	if err != nil {
		return readFirstFrom(have, nil, "", "")
	}
	return readFirstFrom(have, st.Blueprint.Lanes, st.Position.Lane, st.Position.Item)
}

// readFirstFrom is the pure part: the existing documents (have), the blueprint's lanes in order, the current lane
// and task item. A lane's `produces` entry ending in "/" is a directory of documents (tasks/), matched by prefix.
func readFirstFrom(have map[string]bool, lanes []blueprint.Lane, currentLane, item string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(name, why string) {
		if name == "" || seen[name] || !have[name] {
			return
		}
		seen[name] = true
		out = append(out, name+" — "+why)
	}
	add("prompt.md", "the clarified brief: what the person wants")
	add("answers.md", "what the person decided when asked; settled, do not re-open")
	for _, lane := range lanes {
		if lane.ID == currentLane {
			break
		}
		var produced []string
		if lane.Step != nil {
			produced = append(produced, lane.Step.Produces...)
		}
		for _, rung := range lane.Ladder {
			produced = append(produced, rung.Produces...)
		}
		for _, name := range produced {
			if strings.HasSuffix(name, "/") {
				var dir []string
				for file := range have {
					if strings.HasPrefix(file, name) {
						dir = append(dir, file)
					}
				}
				sort.Strings(dir)
				for _, file := range dir {
					if item != "" && file == "tasks/"+item+".md" {
						continue // named last, as this step's own
					}
					add(file, "produced by the "+lane.ID+" lane")
				}
				continue
			}
			add(name, "produced by the "+lane.ID+" lane")
		}
	}
	if item != "" {
		add("tasks/"+item+".md", "THIS task's spec: what you are to build and how it is verified")
	}
	return out
}
