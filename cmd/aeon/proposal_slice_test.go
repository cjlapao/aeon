package main

// proposal_slice_test.go — S35 T9, test map #1: the test the whole slice's
// safety case rests on. For each kind: create via the SOCKET → the
// affected subject's verdict is byte-identical to before (TD2's
// guard-blind invariant — proposing changes nothing live) and so is an
// unrelated CONTROL subject's → commit via the CLI (T5's only effect
// door) → exactly the affected verdict flips, the control stays
// byte-identical throughout.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/mcp"
	"github.com/cjlapao/aeon/internal/paths"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// sliceStore wires a fresh daemon with every seam this slice's pages
// exercise — Proposals (via ValidateProposal + Store), PolicyStore (free
// from daemon.New given opts.Store), and MCP, wired the same way
// cmd/aeon serve wires it (wireMCPServices), by hand, since that wiring
// is cmd/aeon-only and daemon.New never calls it on its own.
func sliceStore(t *testing.T) (*store.Store, *daemon.Daemon) {
	t.Helper()
	st := proposalHome(t)
	opts := daemon.Options{Store: st, ValidateProposal: validateProposalPayload}
	if err := wireMCPServices(st, &opts); err != nil {
		t.Fatal(err)
	}
	d := daemon.New(opts)
	return st, d
}

func sliceDispatch(t *testing.T, d *daemon.Daemon, method string, params any) (any, error) {
	t.Helper()
	meth, ok := d.Table.Lookup(protocol.MethodName(method))
	if !ok {
		t.Fatalf("%s is not registered", method)
	}
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	// A nil *protocol.Session panics inside table.ActorFor (daemon.go:422
	// dereferences sess.ConnID unconditionally) — this is a real finding
	// against this session's own earlier note that a nil session was safe;
	// it is not. Every other test dispatching through Table.Lookup passes
	// nil only for methods that never read ActorFor (e.g. proposal.list);
	// proposal.create does, so it needs a real, if empty, session.
	return meth.Handle(context.Background(), &protocol.Session{}, raw)
}

func policyWhySentence(t *testing.T, d *daemon.Daemon, p protocol.PolicyWhyParams) string {
	t.Helper()
	res, err := sliceDispatch(t, d, "policy.why", p)
	if err != nil {
		t.Fatalf("policy.why: %v", err)
	}
	raw, _ := json.Marshal(res)
	var r protocol.PolicyWhyResult
	_ = json.Unmarshal(raw, &r)
	return r.Sentence
}

func mcpServerNames(t *testing.T, d *daemon.Daemon) string {
	t.Helper()
	res, err := sliceDispatch(t, d, "mcp.status", map[string]any{})
	if err != nil {
		t.Fatalf("mcp.status: %v", err)
	}
	raw, _ := json.Marshal(res)
	return string(raw)
}

func sliceWriteMCPConfig(t *testing.T, text string) {
	t.Helper()
	isolateStoreEnv(t) // the leaked-DATABASE-keys pin (policy_golden_test.go:179)
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(paths.MCPConfigEnv, path)
	// S46 T3: the same fixture fact additionally seeds the TABLES — the
	// truth the validators, effects and mcp.status now read (call sites
	// unchanged; the TD2 before/after assertions survive because the
	// commit effect writes rows, so status moves on commit exactly as it
	// did when it read the file).
	seedMCPConfigRows(t, mcpConfigFromJSON(t, text))
}

func TestProposalSliceAcrossAllKinds(t *testing.T) {
	t.Run("policy_add", func(t *testing.T) {
		_, d := sliceStore(t)
		affected := protocol.PolicyWhyParams{Agent: "boss", Type: "tool_access", Tool: "bash"}
		control := protocol.PolicyWhyParams{Agent: "someone-else", Type: "tool_access", Tool: "bash"}

		before := policyWhySentence(t, d, affected)
		controlBefore := policyWhySentence(t, d, control)

		res, err := sliceDispatch(t, d, "proposal.create", map[string]any{
			"kind": "policy_add",
			"payload": policy.Policy{
				ID: "deny-bash-boss", Type: policy.ToolAccess,
				Target: policy.Target{Kind: policy.AgentKind, ID: "boss"}, Action: policy.Deny,
				Source: policy.Operator, Spec: json.RawMessage(`{"toolName":"bash","state":"deny"}`),
			},
		})
		if err != nil {
			t.Fatalf("proposal.create: %v", err)
		}
		raw, _ := json.Marshal(res)
		var row struct{ ID int64 }
		_ = json.Unmarshal(raw, &row)

		if got := policyWhySentence(t, d, affected); got != before {
			t.Fatalf("TD2 violated: proposing changed the affected verdict:\nbefore %q\nafter  %q", before, got)
		}
		if got := policyWhySentence(t, d, control); got != controlBefore {
			t.Fatalf("the control verdict moved just from a create: %q vs %q", got, controlBefore)
		}

		if out, err := runProposal(t, "commit", itoa(row.ID)); err != nil {
			t.Fatalf("commit: %v\n%s", err, out)
		}

		after := policyWhySentence(t, d, affected)
		if after == before {
			t.Fatalf("the affected verdict did not flip after commit: %q", after)
		}
		if got := policyWhySentence(t, d, control); got != controlBefore {
			t.Fatalf("the control verdict moved after commit: %q vs %q", got, controlBefore)
		}
	})

	t.Run("policy_remove", func(t *testing.T) {
		st, d := sliceStore(t)
		boundary := policy.NewStore(policyRepo(st))
		now := time.Now().UTC()
		if rej := boundary.Add(context.Background(), policy.Policy{
			ID: "deny-webfetch-probe", Type: policy.ToolAccess,
			Target: policy.Target{Kind: policy.AgentKind, ID: "probe"}, Action: policy.Deny,
			Source: policy.Operator, Spec: json.RawMessage(`{"toolName":"webfetch","state":"deny"}`),
		}, now); rej != nil {
			t.Fatalf("seed: %s", rej.Reason)
		}
		if rej := boundary.Add(context.Background(), policy.Policy{
			ID: "deny-webfetch-other", Type: policy.ToolAccess,
			Target: policy.Target{Kind: policy.AgentKind, ID: "other-agent"}, Action: policy.Deny,
			Source: policy.Operator, Spec: json.RawMessage(`{"toolName":"webfetch","state":"deny"}`),
		}, now); rej != nil {
			t.Fatalf("seed control: %s", rej.Reason)
		}

		affected := protocol.PolicyWhyParams{Agent: "probe", Type: "tool_access", Tool: "webfetch"}
		control := protocol.PolicyWhyParams{Agent: "other-agent", Type: "tool_access", Tool: "webfetch"}
		before := policyWhySentence(t, d, affected)
		controlBefore := policyWhySentence(t, d, control)

		res, err := sliceDispatch(t, d, "proposal.create", map[string]any{
			"kind":    "policy_remove",
			"payload": policyRemovePayload{ID: "deny-webfetch-probe"},
		})
		if err != nil {
			t.Fatalf("proposal.create: %v", err)
		}
		raw, _ := json.Marshal(res)
		var row struct{ ID int64 }
		_ = json.Unmarshal(raw, &row)

		if got := policyWhySentence(t, d, affected); got != before {
			t.Fatalf("TD2 violated: proposing changed the affected verdict:\nbefore %q\nafter  %q", before, got)
		}
		if got := policyWhySentence(t, d, control); got != controlBefore {
			t.Fatalf("the control verdict moved just from a create: %q vs %q", got, controlBefore)
		}

		if out, err := runProposal(t, "commit", itoa(row.ID)); err != nil {
			t.Fatalf("commit: %v\n%s", err, out)
		}

		after := policyWhySentence(t, d, affected)
		if after == before {
			t.Fatalf("the affected verdict did not flip after commit: %q", after)
		}
		if got := policyWhySentence(t, d, control); got != controlBefore {
			t.Fatalf("the control verdict moved after commit: %q vs %q", got, controlBefore)
		}
	})

	t.Run("seed_corpus", func(t *testing.T) {
		_, d := sliceStore(t)
		// brief-author's embedded corpus rule: pattern "pwd" verdict
		// "allow" (internal/policy/testdata/corpus.json, measured this
		// session) — a real, deterministic flip: before seeding, nothing
		// governs brief-author's "pwd" and the empty set denies (PL5);
		// after, the seeded rule allows it.
		affected := protocol.PolicyWhyParams{Agent: "brief-author", Type: "command_access", Command: "pwd"}
		control := protocol.PolicyWhyParams{Agent: "someone-never-seeded", Type: "command_access", Command: "pwd"}
		before := policyWhySentence(t, d, affected)
		controlBefore := policyWhySentence(t, d, control)

		res, err := sliceDispatch(t, d, "proposal.create", map[string]any{
			"kind":    "seed_corpus",
			"payload": seedCorpusPayload{Agents: []string{"brief-author"}},
		})
		if err != nil {
			t.Fatalf("proposal.create: %v", err)
		}
		raw, _ := json.Marshal(res)
		var row struct{ ID int64 }
		_ = json.Unmarshal(raw, &row)

		if got := policyWhySentence(t, d, affected); got != before {
			t.Fatalf("TD2 violated: proposing changed the affected verdict:\nbefore %q\nafter  %q", before, got)
		}
		if got := policyWhySentence(t, d, control); got != controlBefore {
			t.Fatalf("the control verdict moved just from a create: %q vs %q", got, controlBefore)
		}

		if out, err := runProposal(t, "commit", itoa(row.ID)); err != nil {
			t.Fatalf("commit: %v\n%s", err, out)
		}

		after := policyWhySentence(t, d, affected)
		if after == before {
			t.Fatalf("the affected verdict did not flip after commit: %q", after)
		}
		if got := policyWhySentence(t, d, control); got != controlBefore {
			t.Fatalf("the control verdict moved after commit: %q vs %q", got, controlBefore)
		}
	})

	t.Run("mcp_server_add", func(t *testing.T) {
		_, d := sliceStore(t)
		sliceWriteMCPConfig(t, `{"public":[{"name":"keepme","transport":{"type":"stdio","command":"keep-mcp"}}],"agents":{}}`)

		before := mcpServerNames(t, d)

		res, err := sliceDispatch(t, d, "proposal.create", map[string]any{
			"kind": "mcp_server_add",
			"payload": mcpServerAddPayload{
				Tier:   "public",
				Server: mcp.ServerConfig{Name: "github", Transport: mcp.TransportConfig{Type: "stdio", Command: "gh-mcp"}, Tools: []mcp.ToolConfig{{Name: "list_repos", Rerun: "safe", Effect: "local"}}},
			},
		})
		if err != nil {
			t.Fatalf("proposal.create: %v", err)
		}
		raw, _ := json.Marshal(res)
		var row struct{ ID int64 }
		_ = json.Unmarshal(raw, &row)

		if got := mcpServerNames(t, d); got != before {
			t.Fatalf("TD2 violated: proposing an mcp_server_add changed mcp.status:\nbefore %s\nafter  %s", before, got)
		}

		if out, err := runProposal(t, "commit", itoa(row.ID)); err != nil {
			t.Fatalf("commit: %v\n%s", err, out)
		}

		after := mcpServerNames(t, d)
		if after == before {
			t.Fatal("mcp.status did not change after committing an mcp_server_add")
		}
		if !strings.Contains(after, "github") {
			t.Fatalf("the new server does not appear live after commit: %s", after)
		}
		if !strings.Contains(after, "keepme") {
			t.Fatalf("the control server (keepme) is gone after commit: %s", after)
		}
	})

	t.Run("mcp_server_remove", func(t *testing.T) {
		_, d := sliceStore(t)
		sliceWriteMCPConfig(t, `{"public":[{"name":"github","transport":{"type":"stdio","command":"gh-mcp"}},{"name":"keepme","transport":{"type":"stdio","command":"keep-mcp"}}],"agents":{}}`)

		before := mcpServerNames(t, d)

		res, err := sliceDispatch(t, d, "proposal.create", map[string]any{
			"kind":    "mcp_server_remove",
			"payload": mcpServerRemovePayload{Name: "github"},
		})
		if err != nil {
			t.Fatalf("proposal.create: %v", err)
		}
		raw, _ := json.Marshal(res)
		var row struct{ ID int64 }
		_ = json.Unmarshal(raw, &row)

		if got := mcpServerNames(t, d); got != before {
			t.Fatalf("TD2 violated: proposing an mcp_server_remove changed mcp.status:\nbefore %s\nafter  %s", before, got)
		}

		if out, err := runProposal(t, "commit", itoa(row.ID)); err != nil {
			t.Fatalf("commit: %v\n%s", err, out)
		}

		after := mcpServerNames(t, d)
		if strings.Contains(after, `"name":"github"`) {
			t.Fatalf("github is still live after committing its removal: %s", after)
		}
		if !strings.Contains(after, "keepme") {
			t.Fatalf("the control server (keepme) is gone after commit: %s", after)
		}
	})
}
