package daemon

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/tools"
	"github.com/cjlapao/aeon/internal/vault"
)

func sp(v string) *string    { return &v }
func vaultBool(v bool) *bool { return &v }

func vaultFixture(t *testing.T) (recordsEnv, *vault.Vault) {
	t.Helper()
	e := newRecordsEnv(t, "")
	v, err := vault.Open(e.s, filepath.Join(t.TempDir(), "vault.key"))
	if err != nil {
		t.Fatal(err)
	}
	return e, v
}

func TestAnAgentReadsOnlyWhatAPersonMadeVisibleAndEveryReadIsRecorded(t *testing.T) {
	e, v := vaultFixture(t)
	person := store.Actor{Kind: "user", ID: "1000"}
	ctx := context.Background()
	v.Set(ctx, vault.SetInput{Name: "DB_PASSWORD", Value: sp("hunter2")}, person)                                 // a secret: hidden by default
	v.Set(ctx, vault.SetInput{Name: "REGION", Value: sp("eu-west-1"), Secret: vaultBool(false)}, person)          // plain: visible
	v.Set(ctx, vault.SetInput{Name: "STRIPE_KEY", Value: sp("sk_live_x"), AgentVisible: vaultBool(true)}, person) // a secret a person shared

	set, err := tools.BuildToolset([]string{"env_list", "env_get"}, tools.ToolContext{WorkspaceRoot: t.TempDir(), Env: NewEnvService(v, e.s)})
	if err != nil {
		t.Fatal(err)
	}
	list := set.Call(e.ctx, "env_list", json.RawMessage(`{}`))
	if list.IsError || !strings.Contains(list.Content, "REGION (variable)") || !strings.Contains(list.Content, "STRIPE_KEY (secret)") || strings.Contains(list.Content, "DB_PASSWORD") {
		t.Fatalf("the agent is told only what is visible: %q", list.Content)
	}
	if strings.Contains(list.Content, "sk_live_x") || strings.Contains(list.Content, "eu-west-1") {
		t.Fatalf("a listing carries no value: %q", list.Content)
	}
	hidden := set.Call(e.ctx, "env_get", json.RawMessage(`{"name":"DB_PASSWORD"}`))
	absent := set.Call(e.ctx, "env_get", json.RawMessage(`{"name":"NOPE"}`))
	if !hidden.IsError || hidden.Content != strings.Replace(absent.Content, "NOPE", "DB_PASSWORD", 1) || strings.Contains(hidden.Content, "hunter2") {
		t.Fatalf("hidden and absent must read the same: %q vs %q", hidden.Content, absent.Content)
	}
	got := set.Call(e.ctx, "env_get", json.RawMessage(`{"name":"STRIPE_KEY"}`))
	if got.IsError || got.Content != "sk_live_x" || strings.Contains(got.Summary, "sk_live") {
		t.Fatalf("a shared secret is read, and the event summary never carries it: %+v", got)
	}
	changes, _ := e.s.EnvVarChanges("STRIPE_KEY", 5)
	if len(changes) == 0 || changes[0].Op != "agent_read" || changes[0].ActorKind != "agent" || changes[0].ActorID != e.agent {
		t.Fatalf("the read names the agent: %+v", changes)
	}
	// A call that smuggles another project or agent in is a parse error: the model supplies a name only.
	if r := set.Call(e.ctx, "env_get", json.RawMessage(`{"name":"REGION","project":"x"}`)); !r.IsError {
		t.Fatalf("an unknown field is refused: %+v", r)
	}
}

func TestAProjectOverrideReachesTheAgentAndTheCommandEnvironmentOfThatProjectOnly(t *testing.T) {
	e, v := vaultFixture(t)
	person := store.Actor{Kind: "user", ID: "1000"}
	ctx := context.Background()
	v.Set(ctx, vault.SetInput{Name: "API_URL", Value: sp("https://global"), Secret: vaultBool(false)}, person)
	// e.proj is the ROOT of the tree the fixture's component alpha belongs to.
	v.Set(ctx, vault.SetInput{Name: "API_URL", Project: e.proj, Value: sp("https://project"), Secret: vaultBool(false)}, person)

	set, _ := tools.BuildToolset([]string{"env_get"}, tools.ToolContext{WorkspaceRoot: t.TempDir(), Env: NewEnvService(v, e.s)})
	if r := set.Call(e.ctx, "env_get", json.RawMessage(`{"name":"API_URL"}`)); r.Content != "https://project" {
		t.Fatalf("the step's project override wins: %+v", r)
	}

	// The command environment: a store-kind reference resolves through the manager, project first.
	r := newStepExecResolver(e.s)
	r.Vault = v
	if got, ok := r.secretLookupFor("/repo/alpha")("API_URL"); !ok || got != "https://project" {
		t.Fatalf("a workspace in the project gets the override: %q %v", got, ok)
	}
	if got, ok := r.secretLookupFor("/somewhere/unregistered")("API_URL"); !ok || got != "https://global" {
		t.Fatalf("an unregistered workspace gets the global value: %q %v", got, ok)
	}
	if _, ok := r.secretLookupFor("/repo/alpha")("NOT_IN_THE_MANAGER"); ok {
		t.Fatal("a name the manager does not hold falls through to the credential records, which also have nothing")
	}
}

func TestTheSocketAdapterNeverReturnsASecretsValueAndCarriesTheActor(t *testing.T) {
	e, v := vaultFixture(t)
	a := vaultAdapter{v: v, st: e.s}
	ctx := context.Background()
	actor := protocol.Actor{Kind: "user", ID: "1000"}
	res, err := a.Set(ctx, protocol.VaultSetParams{Name: "TOKEN", Value: sp("super-secret-value"), Description: sp("ci")}, actor)
	if err != nil {
		t.Fatal(err)
	}
	if res.Variable.Value != "" || !res.Variable.Secret || res.Variable.AgentVisible {
		t.Fatalf("a new variable is a hidden secret and its value is not echoed: %+v", res.Variable)
	}
	if _, err := a.Set(ctx, protocol.VaultSetParams{Name: "SHOWN", Value: sp("v"), Secret: vaultBool(false), Project: e.proj}, actor); err != nil {
		t.Fatal(err)
	}
	list, _ := a.List(ctx)
	raw, _ := json.Marshal(list)
	if strings.Contains(string(raw), "super-secret-value") {
		t.Fatalf("the list leaks a secret: %s", raw)
	}
	var shown protocol.VaultVariable
	for _, x := range list.Variables {
		if x.Name == "SHOWN" {
			shown = x
		}
	}
	if shown.Value != "v" || shown.ProjectName != "org group" {
		t.Fatalf("a plain variable shows its value, and a project override names its project: %+v", shown)
	}
	// A refusal arrives as the manager's own sentence.
	if _, err := a.Set(ctx, protocol.VaultSetParams{Name: "AEON_X", Value: sp("v")}, actor); err == nil || !strings.HasPrefix(err.Error(), "refused:") {
		t.Fatalf("a reserved name is refused in the manager's words: %v", err)
	}
	ch, _ := a.Changes(ctx, protocol.VaultChangesParams{Name: "TOKEN"})
	if len(ch.Changes) != 1 || ch.Changes[0].ActorID != "1000" {
		t.Fatalf("the ledger names the person: %+v", ch)
	}
	if del, _ := a.Delete(ctx, protocol.VaultDeleteParams{Name: "TOKEN"}, actor); !del.Deleted {
		t.Fatal("deleted")
	}
}
