package blueprint

import "strings"

// labels.go — the friendly names of the flows that ship with the daemon.
//
// A flow's id is its slug; every reference (loops, schedules, queues) uses it.
// What a person reads is its title and description. A flow may carry both in its
// own text (`title:`, `description:`), and then they win. The shipped flows are
// byte-pinned (their text is the seed), so their labels live here, beside the
// code that reads them, rather than in the pinned files: editing a shipped flow's
// text to add a label would be a version bump for no change in behaviour.

// Labels are what the app shows for a flow.
type Labels struct {
	Title       string
	Description string
}

var shippedLabels = map[string]Labels{
	"brainstorm":               {"Brainstorm", "Turns a conversation into a goal: the brainstormer captures the idea and files a proposal and a brief against it."},
	"chat":                     {"Chat", "A plain conversation with an agent about a component. The run never finishes, so you can keep talking."},
	"conversational-map":       {"Conversational Map", "Triages a request, investigates and implements it, then offers an inline review and a security pass."},
	"development-map-direct":   {"Development Map (Direct)", "The short path for one change: triage, investigate, brief, implement, review, then commit and open the pull request."},
	"development-map-pipeline": {"Development Map (Pipeline)", "The full feature pipeline: issue, task breakdown, a branch per task, implementation, review and merge, then feature verification."},
	"dev-loop":                 {"Developer Loop", "Investigates, plans, asks for one approval, then builds, verifies and ships each task, running unattended after the approval."},
	"goal-to-brief":            {"Goal to Brief", "Starts on an approved goal, has the brief author write the brief, and waits for a person to approve it."},
	"proposal-brief":           {"Proposal and Brief", "Drafts a proposal, gets it approved, writes the brief from it, self-checks the brief and asks for approval."},
	"research-map":             {"Research Map", "A planner asks a surveyor for findings and a reporter writes them up: one agent asking another."},
	"roadmap-plan":             {"Roadmap Plan", "One step that turns a roadmap item into a plan."},
	"tag-only":                 {"Tech Tag Rehearsal", "One step that picks its agent by tech tag. Used to try the tag resolver."},
	"two-step":                 {"Two Step", "Two agent steps with a person's approval between them. A small flow for trying the engine."},
	"land-feature":             {"Land Feature", "The merge queue's landing run: catch up with the base, verify, publish, open the pull request and land it."},
	"resolve-conflict":         {"Resolve Conflict", "The merge queue's resolution run for a conflicted item: resolve, conclude and verify."},
	"goal-intake":              {"Goal Intake", "Turns a person's stated want into a charter draft and a project plan."},
}

// ShippedLabels returns the labels of a shipped flow by its slug.
func ShippedLabels(slug string) (Labels, bool) {
	l, ok := shippedLabels[slug]
	return l, ok
}

// TitleFromSlug is the fallback for a flow with no title anywhere: the slug read
// as words ("my-flow" -> "My Flow"). It is a display convenience, never stored.
func TitleFromSlug(slug string) string {
	words := strings.FieldsFunc(slug, func(r rune) bool { return r == '-' || r == '_' })
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}
