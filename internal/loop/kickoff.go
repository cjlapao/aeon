package loop

import (
	"strings"

	"github.com/cjlapao/aeon/internal/blueprint"
)

// kickoff.go is S39 T7: choosing a run's first message, and saying who wrote
// it. The two halves are deliberately separated — the TEXT is what the model
// sees (modelPrompt reads Kickoff and nothing else), the SOURCE is what the
// thread and the operator see (T8 files the chat_message row with it). A
// typed start and a templated one must read the same to the model (CR6/D7);
// telling them apart is the record's job, never the prompt's.

// KickoffFor picks the run's first message and says where it came from
// (store.ChatMessage.Source values).
//  1. typed (a person wrote it) -> (typed, "user")
//  2. the blueprint's kickoff: template -> (rendered, "template")
//  3. no template: the goal and/or task body -> (body, "template")
//  4. nothing to say -> ("", "")
func KickoffFor(bp *blueprint.Blueprint, d blueprint.KickoffData, typed string) (text, source string, err error) {
	if strings.TrimSpace(typed) != "" {
		return strings.TrimSpace(typed), "user", nil
	}
	if bp != nil && strings.TrimSpace(bp.Kickoff) != "" {
		rendered, err := blueprint.RenderKickoff(bp.Kickoff, d)
		if err != nil {
			return "", "", err
		}
		return rendered, "template", nil
	}
	goal, task := strings.TrimSpace(d.Goal), strings.TrimSpace(d.Task)
	switch {
	case goal != "" && task != "":
		return goal + "\n\n" + task, "template", nil
	case goal != "":
		return goal, "template", nil
	case task != "":
		return task, "template", nil
	}
	return "", "", nil
}

// goalText is the ONE seam modelPrompt reads: the run's kickoff if it has
// one, the goal as it has always been otherwise. It must never read any
// source field — the prompt's byte-identity across provenance (D7) is
// guaranteed by this function having nothing to branch on but emptiness.
func goalText(st LoopState) string {
	if st.Kickoff != "" {
		return st.Kickoff
	}
	return st.Goal
}

// KickoffAuthor maps a source to a chat_message author: a person's typed or
// templated start is theirs (a template is a person's words chosen at the
// moment of starting); automation is the system's.
func KickoffAuthor(source string) string {
	if source == "automation" || source == "blueprint" {
		return "system"
	}
	return "human"
}
