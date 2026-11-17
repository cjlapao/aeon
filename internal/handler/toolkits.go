package handler

import (
	"fmt"
	"sort"
	"strings"

	"github.com/cjlapao/aeon/internal/tools"
)

// toolkits.go — tool KITS: the groups a person picks from when deciding what an agent can do.
//
// About seventy tools in forty families is too fine a grain to choose from one by one, so the
// daemon offers a short list of kits, each a plain-language ability with the registered tools that
// make it up. The list lives HERE, once, and a test holds every name in it against the real tool
// registry — a window never carries a tool name of its own, so it cannot drift from the registry.
//
// A kit is a convenience for picking, not a permission: an agent's tools column is still the list
// of tools it may ASK for, and policy still decides each call.

// ToolKit is one group of tools offered as a single choice.
type ToolKit struct {
	// ID is the stable key (lower-case words).
	ID string `json:"id"`
	// Label is the short name a person sees.
	Label string `json:"label"`
	// Description says what the agent can do with it, in a sentence.
	Description string `json:"description"`
	// Effect is how far the kit reaches: reads | changes | runs | reaches | asks.
	// The window marks a kit that changes things, runs commands or reaches the network.
	Effect string `json:"effect"`
	// Tools are registered tool names, in the order they are shown.
	Tools []string `json:"tools"`
}

var toolKits = []ToolKit{
	{"read-code", "Read the code", "Open and search files in the repository, and search for text in them.", "reads", []string{"read", "list", "glob", "grep"}},
	{"change-files", "Change files", "Create new files and edit existing ones in the repository.", "changes", []string{"write", "edit"}},
	{"run-commands", "Run commands", "Run shell commands, such as builds and tests. Policy decides which commands are allowed.", "runs", []string{"bash"}},
	{"documents", "Documents", "Read, list, write and edit the documents of the work, such as plans and notes.", "changes", []string{"doc_read", "doc_list", "doc_write", "doc_edit"}},
	{"ask-person", "Ask the person", "Stop and ask a human a question when something is unclear.", "asks", []string{"question"}},
	{"plan-work", "Plan the work", "Keep a to-do list for the step it is on.", "reads", []string{"todo"}},
	{"use-web", "Use the web", "Fetch pages from the internet.", "reaches", []string{"webfetch"}},
	{"other-agents", "Work with other agents", "Ask another agent for help and wait for its answer.", "asks", []string{"ask_agent"}},
	{"learn", "Learn and remember", "Look up what earlier work learned and add new lessons.", "changes", []string{"learning_query", "learning_show", "learning_add"}},
}

// ToolKits returns a COPY of the kits, in display order.
func ToolKits() []ToolKit {
	out := make([]ToolKit, len(toolKits))
	for i, k := range toolKits {
		k.Tools = append([]string(nil), k.Tools...)
		out[i] = k
	}
	return out
}

// NormalizeTools checks an agent's tool list against the registry and returns it clean: trimmed,
// de-duplicated and sorted, so the same choice is always stored the same way. A name the registry
// does not have is refused by name — an agent cannot carry a tool nobody compiled (O1), and
// dropping it quietly would hide a typo.
func NormalizeTools(names []string) ([]string, error) {
	seen := map[string]bool{}
	out := make([]string, 0, len(names))
	var unknown []string
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		if !tools.IsToolName(name) {
			unknown = append(unknown, name)
			continue
		}
		out = append(out, name)
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, fmt.Errorf("%s is not a registered tool — the tools an agent may hold are the registry's (O1)", strings.Join(unknown, ", "))
	}
	sort.Strings(out)
	return out, nil
}
