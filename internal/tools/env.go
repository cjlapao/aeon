package tools

// env.go — `env_list` and `env_get`: how an agent reads a managed variable.
//
// The rule is the vault's and it is not negotiable here: an agent sees a
// variable only if a PERSON marked it agent-visible, and nothing is visible by
// default. The model supplies a name and nothing else — the agent and the
// project come from the step the daemon stamps — and a hidden variable answers
// the very sentence an absent one does, so a model cannot probe for what exists.
// Every successful `env_get` is a ledger row naming the agent.
//
// Registered, never granted by default: a body must list these tools, and the
// person's policy still judges each call.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cjlapao/aeon/internal/model"
)

// EnvEntry is one agent-visible variable as listed: never a value.
type EnvEntry struct {
	Name        string
	Description string
	Secret      bool
}

// EnvStore is the step's door to the variable manager.
type EnvStore interface {
	// List answers the variables an agent may read, for the step's own project.
	List(ctx context.Context) ([]EnvEntry, error)
	// Get answers a value, or the refusal sentence (value == "").
	Get(ctx context.Context, name string) (value, refusal string, err error)
}

func envOrFail(tc ToolContext) (EnvStore, *Result) {
	if tc.Env == nil {
		r := Errorf("variables are unavailable in this runtime: this daemon has no variable manager")
		return nil, &r
	}
	return tc.Env, nil
}

// EnvList lists the variables an agent may read.
func EnvList() Tool {
	return Tool{
		Class: RerunSafe,
		Spec: model.ToolSpec{
			Name:        "env_list",
			Description: "List the environment variables and secrets a person has made available to agents — names and what each is for, never a value. A variable that is not listed is not available to you; there is no way to ask for one that is hidden.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
		},
		Run: func(ctx context.Context, tc ToolContext, input json.RawMessage) Result {
			if err := decodeDocInput(input, &struct{}{}); err != nil {
				return Errorf("%s", err)
			}
			env, bad := envOrFail(tc)
			if bad != nil {
				return *bad
			}
			entries, err := env.List(ctx)
			if err != nil {
				return Errorf("variable store: %v", err)
			}
			if len(entries) == 0 {
				return Result{Content: "No variables are available to agents.", Summary: "env_list"}
			}
			var b strings.Builder
			for _, e := range entries {
				kind := "variable"
				if e.Secret {
					kind = "secret"
				}
				fmt.Fprintf(&b, "%s (%s)", e.Name, kind)
				if e.Description != "" {
					b.WriteString(" — " + e.Description)
				}
				b.WriteByte('\n')
			}
			return Result{Content: strings.TrimRight(b.String(), "\n"), Summary: fmt.Sprintf("env_list · %d", len(entries))}
		},
	}
}

// EnvGet reads one variable's value.
func EnvGet() Tool {
	return Tool{
		Class: RerunDetectable,
		Spec: model.ToolSpec{
			Name:        "env_get",
			Description: "Read the value of one environment variable or secret by name. Only variables a person made available to agents can be read; anything else answers that it is not visible, whether it exists or not. Each read is recorded with your name. Treat a secret as one: use it for the task, never repeat it in a message, a file or a document.",
			Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {"name": {"type": "string", "description": "the variable's name, as env_list shows it"}},
  "required": ["name"],
  "additionalProperties": false
}`),
		},
		Run: func(ctx context.Context, tc ToolContext, input json.RawMessage) Result {
			var in struct{ Name string }
			if err := decodeDocInput(input, &in); err != nil {
				return Errorf("%s", err)
			}
			env, bad := envOrFail(tc)
			if bad != nil {
				return *bad
			}
			value, refusal, err := env.Get(ctx, strings.TrimSpace(in.Name))
			if err != nil {
				return Errorf("variable store: %v", err)
			}
			if refusal != "" {
				return Errorf("%s", refusal)
			}
			// Summary names the variable only: the event stream never carries a value.
			return Result{Content: value, Summary: "env_get " + strings.TrimSpace(in.Name)}
		},
	}
}
