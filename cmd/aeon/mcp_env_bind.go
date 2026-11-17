package main

// mcp_env_bind.go — an MCP server's environment variables are never empty.
//
// Every env NAME a server declares is bound, at the door that writes it, to a
// variable of the variable manager: either a stored variable the person picks, or
// a value they type — which the door puts in the manager as a hidden secret and
// binds to, so the configuration only ever holds a binding and never a value. A
// name left without either is refused with the sentence that says what to do.
//
// The side effect (storing a typed value) happens only AFTER the whole candidate
// has been validated, so a refused write leaves the manager as it was.

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/vault"
)

// envValueWrite is one typed value waiting to be stored.
type envValueWrite struct{ envName, variable, value, server string }

// envVariableName is where a typed value for NAME of SERVER lives in the manager.
func envVariableName(server, env string) string {
	clean := func(s string) string {
		var b strings.Builder
		for _, r := range strings.ToUpper(s) {
			if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
				b.WriteRune(r)
			} else {
				b.WriteByte('_')
			}
		}
		return b.String()
	}
	return "MCP_" + clean(server) + "_" + clean(env)
}

// bindEnvironment checks every declared env name has exactly one binding and
// fills t.EnvFrom accordingly. carry is the binding a name already has on the
// stored row (an edit need not retype it). It changes nothing outside t.
func bindEnvironment(server string, t *protocol.MCPTransportInput, carry map[string]string) ([]envValueWrite, string) {
	declared := map[string]bool{}
	for _, n := range t.Env {
		declared[n] = true
	}
	for n := range t.EnvFrom {
		if !declared[n] {
			return nil, fmt.Sprintf("refused: envFrom binds %q, which is not one of the server's env names", n)
		}
	}
	for n := range t.EnvValues {
		if !declared[n] {
			return nil, fmt.Sprintf("refused: a value was given for %q, which is not one of the server's env names", n)
		}
	}
	bound := map[string]string{}
	var writes []envValueWrite
	for _, n := range t.Env {
		from, hasFrom := t.EnvFrom[n]
		val, hasVal := t.EnvValues[n]
		switch {
		case hasFrom && from != "" && hasVal:
			return nil, fmt.Sprintf("refused: env %s has both a stored variable and a typed value — pick one", n)
		case hasVal:
			if val == "" {
				return nil, fmt.Sprintf("refused: env %s has an empty value — an environment variable cannot be empty", n)
			}
			if mcpVault == nil {
				return nil, "refused: the variable manager is off, so a typed value has nowhere to be kept — start the daemon (it opens the manager's key on start) and try again"
			}
			name := envVariableName(server, n)
			bound[n] = name
			writes = append(writes, envValueWrite{envName: n, variable: name, value: val, server: server})
		case hasFrom && from != "":
			// With no manager in this process (the CLI's direct door) a binding cannot
			// be checked here; it is checked when the daemon starts the server, and a
			// variable that is still missing then fails that start by name.
			if mcpVault != nil && !mcpVault.Has(from) {
				return nil, fmt.Sprintf("refused: env %s is bound to variable %s, which does not exist — create it in Variables & secrets, or enter a value here", n, from)
			}
			bound[n] = from
		case carry[n] != "":
			bound[n] = carry[n]
		default:
			return nil, fmt.Sprintf("refused: env %s has no value — bind it to a stored variable or enter a value (an environment variable cannot be empty)", n)
		}
	}
	t.EnvFrom, t.EnvValues = nilIfEmpty(bound), nil // a typed value is never kept on the transport
	return writes, ""
}

// applyEnvValues stores the typed values as hidden secrets, once the candidate has
// passed validation. The variable is hidden from agents and described by what it
// is for; it is the person's, and editable in Variables & secrets.
func applyEnvValues(ctx context.Context, writes []envValueWrite, id string) error {
	sort.Slice(writes, func(i, j int) bool { return writes[i].variable < writes[j].variable })
	secret, visible := true, false
	for _, w := range writes {
		desc := fmt.Sprintf("env %s of MCP server %s", w.envName, w.server)
		val := w.value
		if _, err := mcpVault.Set(ctx, vault.SetInput{Name: w.variable, Value: &val, Description: &desc, Secret: &secret, AgentVisible: &visible},
			store.Actor{Kind: "user", ID: id}); err != nil {
			return fmt.Errorf("could not store the value for env %s: %w", w.envName, err)
		}
	}
	return nil
}
