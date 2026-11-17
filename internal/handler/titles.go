package handler

import "strings"

// titles.go — the friendly names of the agents that ship with the daemon.
//
// An agent's name in its body's front matter is its slug (`golang-engineer`):
// what code refers to. What a person reads is its title ("Go Engineer"). The
// bodies are reviewed against their originals line for line, so the titles live
// here, beside the registry, rather than as one more line in every body.

var agentTitles = map[string]string{
	"brainstormer":              "Brainstormer",
	"brief-author":              "Brief Author",
	"chat":                      "Chat",
	"code-reviewer":             "Code Reviewer",
	"debugger-engineer":         "Debugger",
	"docker-engineer":           "Docker Engineer",
	"golang-engineer":           "Go Engineer",
	"intake-architect":          "Intake Architect",
	"intent-clarifier":          "Intent Clarifier",
	"investigator":              "Investigator",
	"javascript-engineer":       "JavaScript Engineer",
	"legal-advisor":             "Legal Advisor",
	"memory-steward":            "Memory Steward",
	"product-manager":           "Product Manager",
	"project-manager":           "Project Manager",
	"project-planner":           "Project Planner",
	"proposal-author":           "Proposal Author",
	"python-engineer":           "Python Engineer",
	"qa-engineer":               "QA Engineer",
	"qa-test-spec":              "QA Test Spec Author",
	"react-typescript-engineer": "React + TypeScript Engineer",
	"rust-engineer":             "Rust Engineer",
	"security-engineer":         "Security Engineer",
	"sizing-estimator":          "Sizing Estimator",
	"sql-engineer":              "SQL Engineer",
	"task-coordinator":          "Task Coordinator",
	"tauri-react-engineer":      "Tauri + React Engineer",
	"tauri-vue-engineer":        "Tauri + Vue Engineer",
	"technical-writer":          "Technical Writer",
	"terraform-engineer":        "Terraform Engineer",
	"ui-design-engineer":        "UI Design Engineer",
	"verifier-engineer":         "Verifier",
	"vue-typescript-engineer":   "Vue + TypeScript Engineer",
	"websocket-engineer":        "WebSocket Engineer",
}

// AgentTitle is the friendly name of a shipped agent by its slug; a slug with no
// entry reads as words ("my-agent" -> "My Agent"), so nothing shows a bare slug.
func AgentTitle(slug string) string {
	if t, ok := agentTitles[slug]; ok {
		return t
	}
	words := strings.FieldsFunc(slug, func(r rune) bool { return r == '-' || r == '_' })
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}
