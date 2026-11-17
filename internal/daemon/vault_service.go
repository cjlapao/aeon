package daemon

// vault_service.go — the daemon's half of the variable manager: the socket
// adapter (`vault.*`), the agent tools' seam (`env_list`, `env_get`), and the
// resolver the processes' environments are built from. One vault underneath all
// three, so the three doors cannot disagree about what a variable is.

import (
	"context"
	"errors"
	"fmt"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/stepctx"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/tools"
	"github.com/cjlapao/aeon/internal/vault"
)

// vaultAdapter is the protocol seam over the vault.
type vaultAdapter struct {
	v  *vault.Vault
	st *store.Store
}

func (a vaultAdapter) view(x vault.Variable) protocol.VaultVariable {
	out := protocol.VaultVariable{Name: x.Name, Project: x.Project, Description: x.Description,
		Secret: x.Secret, AgentVisible: x.AgentVisible, Value: x.Value, UpdatedAt: x.UpdatedAt}
	if x.Project != "" {
		if c, err := a.st.ReadComponent(x.Project); err == nil {
			out.ProjectName = c.Name
		}
	}
	return out
}

func (a vaultAdapter) List(ctx context.Context) (protocol.VaultListResult, error) {
	vars, err := a.v.List(ctx)
	if err != nil {
		return protocol.VaultListResult{}, err
	}
	out := protocol.VaultListResult{Variables: []protocol.VaultVariable{}}
	for _, x := range vars {
		out.Variables = append(out.Variables, a.view(x))
	}
	return out, nil
}

func (a vaultAdapter) Set(ctx context.Context, p protocol.VaultSetParams, actor protocol.Actor) (protocol.VaultSetResult, error) {
	x, err := a.v.Set(ctx, vault.SetInput{Name: p.Name, Project: p.Project, Description: p.Description,
		Value: p.Value, Secret: p.Secret, AgentVisible: p.AgentVisible}, vaultActor(actor))
	if err != nil {
		return protocol.VaultSetResult{}, err
	}
	return protocol.VaultSetResult{Variable: a.view(x)}, nil
}

func (a vaultAdapter) Delete(ctx context.Context, p protocol.VaultDeleteParams, actor protocol.Actor) (protocol.VaultDeleteResult, error) {
	ok, err := a.v.Delete(ctx, p.Name, p.Project, vaultActor(actor))
	return protocol.VaultDeleteResult{Deleted: ok}, err
}

func (a vaultAdapter) Changes(_ context.Context, p protocol.VaultChangesParams) (protocol.VaultChangesResult, error) {
	rows, err := a.st.EnvVarChanges(p.Name, p.Limit)
	if err != nil {
		return protocol.VaultChangesResult{}, err
	}
	out := protocol.VaultChangesResult{Changes: []protocol.VaultChange{}}
	for _, r := range rows {
		out.Changes = append(out.Changes, protocol.VaultChange{ID: r.ID, Name: r.Name, Project: r.ProjectID, Op: r.Op,
			Summary: r.Summary, ActorKind: r.ActorKind, ActorID: r.ActorID, OccurredAt: r.OccurredAt})
	}
	return out, nil
}

// personActor maps the connection's peer to the ledger's author kinds. The
// socket's humans are "user"; anything the store would not admit is a user too —
// this door is a person's by construction.
func vaultActor(a protocol.Actor) store.Actor {
	kind := a.Kind
	if kind != "user" && kind != "trigger" {
		kind = "user"
	}
	return store.Actor{Kind: kind, ID: a.ID}
}

// EnvService is the agent tools' seam: the step on the context says which agent
// is asking and which project it works in; the model says only a name.
type EnvService struct {
	v     *vault.Vault
	store *store.Store
}

// NewEnvService builds the seam; nil when there is no vault, so the tools stay
// registered-and-unavailable rather than answering from an injected nothing.
func NewEnvService(v *vault.Vault, st *store.Store) *EnvService {
	if v == nil || st == nil {
		return nil
	}
	return &EnvService{v: v, store: st}
}

func envServiceOrNil(s *EnvService) tools.EnvStore {
	if s == nil {
		return nil
	}
	return s
}

func (e *EnvService) who(ctx context.Context) (agent, root string, err error) {
	id, ok := stepctx.StepID(ctx)
	if !ok {
		return "", "", errors.New("variables need a step on the context, and this call carries none")
	}
	st, err := e.store.ReadStep(id)
	if err != nil {
		return "", "", fmt.Errorf("read the step %d that asks for a variable: %w", id, err)
	}
	return st.Agent, vault.RootFor(e.store, st.ComponentRoot), nil
}

// List implements tools.EnvStore: only what is agent-visible, names and flags.
func (e *EnvService) List(ctx context.Context) ([]tools.EnvEntry, error) {
	_, root, err := e.who(ctx)
	if err != nil {
		return nil, err
	}
	vars, err := e.v.AgentList(root)
	if err != nil {
		return nil, err
	}
	out := make([]tools.EnvEntry, 0, len(vars))
	for _, x := range vars {
		out = append(out, tools.EnvEntry{Name: x.Name, Description: x.Description, Secret: x.Secret})
	}
	return out, nil
}

// Get implements tools.EnvStore. A refusal is the tool's answer, and it is the
// same sentence whether the variable is hidden or does not exist.
func (e *EnvService) Get(ctx context.Context, name string) (string, string, error) {
	agent, root, err := e.who(ctx)
	if err != nil {
		return "", "", err
	}
	if agent == "" {
		agent = "unknown"
	}
	val, refusal := e.v.AgentGet(ctx, name, root, store.Actor{Kind: "agent", ID: agent})
	return val, refusal, nil
}
