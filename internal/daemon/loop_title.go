package daemon

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// A loop starts titled with its goal, which can be a paragraph. As soon as it starts, one small step, run like any
// other and visible in the loop's history as `loop-titler`, makes a short title from the request and renames the loop.
// It rides the loop's own route and costs what it costs. When it cannot run, or answers nothing usable, the title is
// the goal cut to a readable length, so a loop never keeps a paragraph for a name.

const (
	// titleKeepAsIs is the longest request kept as its own title without asking a model.
	titleKeepAsIs = 48
	// titleMaxRunes bounds a title however it was made.
	titleMaxRunes = 60
	// titlerLabel is the step's name in the history.
	titlerLabel = "loop-titler"
)

// shortTitle cuts text to a readable single line: whitespace collapsed, at most max runes, broken at a word and ended
// with an ellipsis when it had to be cut.
func shortTitle(text string, max int) string {
	one := strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(one) <= max {
		return one
	}
	cut := []rune(one)[:max-1]
	if i := strings.LastIndex(string(cut), " "); i > max/2 {
		return strings.TrimRight(string(cut)[:i], " ,;:.-") + "…"
	}
	return strings.TrimRight(string(cut), " ,;:.-") + "…"
}

// cleanTitle turns a model's answer into a title: first line, no wrapping quotes or a leading "Title:", bounded.
// Empty means the answer was not usable.
func cleanTitle(answer string) string {
	t := strings.TrimSpace(answer)
	if i := strings.IndexByte(t, '\n'); i >= 0 {
		t = strings.TrimSpace(t[:i])
	}
	for _, prefix := range []string{"Title:", "title:", "TITLE:"} {
		t = strings.TrimSpace(strings.TrimPrefix(t, prefix))
	}
	t = strings.Trim(strings.TrimRight(strings.Trim(t, "\"'`“”‘’ "), ".\"'`“”‘’ "), " ")
	if t == "" {
		return ""
	}
	return shortTitle(t, titleMaxRunes)
}

func titlePrompt(request string) string {
	return "Write a short title for the request below.\n\n" +
		"Reply with the JSON report. Put status \"ok\" and make `summary` ONLY the title: at most 6 words and 48 characters, " +
		"in the same language as the request, no quotes, no trailing punctuation, no prefix such as \"Title:\". " +
		"Use no tool; everything you need is in the request.\n\nRequest:\n" + request
}

// titleLoop makes the loop's short title. It runs after the loop has started, off the caller's path.
func (d *Daemon) titleLoop(st loop.LoopState, request, route string) {
	sc := store.Scope{LoopSlug: st.Slug, ComponentRoot: st.ComponentRoot}
	title := ""
	if d.Table != nil && d.store != nil {
		res, err := d.Table.RunStepForLoop(context.Background(),
			protocol.StepParams{Prompt: titlePrompt(request), Label: titlerLabel, Tools: []string{"list"}, WorkspaceRoot: st.ComponentRoot,
				Route: route, LoopSlug: st.Slug, MaxIterations: 3},
			protocol.LoopStepIdentity{LoopID: st.ID, Step: "title"},
			func(stepID int64) error {
				d.publishLoop(protocol.KindLoopStepDispatched, map[string]any{"loopId": st.ID, "lane": "", "step": "title", "stepId": stepID, "agent": titlerLabel, "route": route})
				return nil
			})
		if err == nil && res.Kind == "suspended" && res.Question != nil {
			// The titler needs no tool. If the model reached for one anyway and the gate asked, nobody should be
			// asked about a step the person never started: it is declined, and the title falls back to the cut goal.
			_, _ = d.Table.AnswerQuestion(protocol.AnswerParams{StepID: res.StepID, Answer: "deny"})
		}
		if err == nil && res.Outcome == "ok" && len(res.Handoff) > 0 {
			var h struct {
				Summary string `json:"summary"`
			}
			if json.Unmarshal(res.Handoff, &h) == nil {
				title = cleanTitle(h.Summary)
			}
		} else if err != nil && d.log != nil {
			d.log.Infof("loop %s: the title step could not run: %v", st.Slug, err)
		}
	}
	if title == "" {
		title = shortTitle(request, titleMaxRunes)
	}
	if d.store == nil {
		return
	}
	if err := d.store.SetLoopTitle(sc, title); err != nil {
		return // the loop was deleted while its title was being made, or the store is gone: nothing to rename
	}
	d.publishLoop("loop.titled", map[string]any{"loopId": st.ID, "title": title})
}
