package protocol

// variables.go — the variable manager on the socket.
//
// Four doors over one seam (`Table.Vault`, filled by the daemon):
//
//   - `vault.list` and `vault.changes` (reads). A SECRET is listed by name,
//     scope, description and flags — never its value. A plain variable carries its
//     value, because the person said it is not secret.
//   - `vault.set` and `vault.delete` (writes, human door only). The actor is
//     the connection's peer credential; a params-supplied actor is refused by
//     name. An agent has no road here: what an agent may read is the env_* tools'
//     business, and they read only what a person marked agent-visible.
//
// Nothing on this surface can return a secret's value: the types have no field
// that carries one for a secret, which is a shape, not a redaction.
//
// The seam is absent when the daemon has no store or no key (V13): the methods
// are ABSENT, not registered-and-refusing.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// VaultVariable is one variable on the wire.
type VaultVariable struct {
	Name         string `json:"name"`
	Project      string `json:"project,omitempty"`     // root component id; "" = global
	ProjectName  string `json:"projectName,omitempty"` // that root's name, for display
	Description  string `json:"description,omitempty"`
	Secret       bool   `json:"secret"`
	AgentVisible bool   `json:"agentVisible"`
	// Value is present for a PLAIN variable only.
	Value     string `json:"value,omitempty"`
	UpdatedAt string `json:"updatedAt"`
}

type VaultListResult struct {
	Variables []VaultVariable `json:"variables"`
}

// VaultSetParams is a create or an edit. Pointers: absent keeps what is
// stored (a flags-only edit never re-enters a value) or takes the default on a
// new variable — a secret, hidden from agents.
type VaultSetParams struct {
	Name         string  `json:"name"`
	Project      string  `json:"project,omitempty"`
	Description  *string `json:"description,omitempty"`
	Value        *string `json:"value,omitempty"`
	Secret       *bool   `json:"secret,omitempty"`
	AgentVisible *bool   `json:"agentVisible,omitempty"`
	// Actor is never read; present to be refused by name.
	Actor string `json:"actor,omitempty"`
}

type VaultSetResult struct {
	Variable VaultVariable `json:"variable"`
}

type VaultDeleteParams struct {
	Name    string `json:"name"`
	Project string `json:"project,omitempty"`
	Actor   string `json:"actor,omitempty"`
}

type VaultDeleteResult struct {
	Deleted bool `json:"deleted"`
}

type VaultChangesParams struct {
	Name  string `json:"name,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

// VaultChange is one ledger row: who did what to which variable, and when.
// Names and flags only — a value never rides a ledger row.
type VaultChange struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Project    string `json:"project,omitempty"`
	Op         string `json:"op"`
	Summary    string `json:"summary"`
	ActorKind  string `json:"actorKind"`
	ActorID    string `json:"actorId"`
	OccurredAt string `json:"occurredAt"`
}

type VaultChangesResult struct {
	Changes []VaultChange `json:"changes"`
}

// VaultSeam is the daemon's adapter. A refusal is a plain error whose text is the
// sentence ("refused: …"); the handler carries it verbatim.
type VaultSeam interface {
	List(ctx context.Context) (VaultListResult, error)
	Set(ctx context.Context, p VaultSetParams, actor Actor) (VaultSetResult, error)
	Delete(ctx context.Context, p VaultDeleteParams, actor Actor) (VaultDeleteResult, error)
	Changes(ctx context.Context, p VaultChangesParams) (VaultChangesResult, error)
}

// RegisterVault registers the four doors when — and only when — the seam is wired.
func (t *Table) RegisterVault() bool {
	if t.Vault == nil {
		return false
	}
	t.register(&Method{Name: MethodVaultList, RequiresInitialize: true, Handle: t.handleVaultList})
	t.register(&Method{Name: MethodVaultChanges, RequiresInitialize: true, Handle: t.handleVaultChanges})
	t.register(&Method{Name: MethodVaultSet, RequiresInitialize: true, Handle: t.handleVaultSet})
	t.register(&Method{Name: MethodVaultDelete, RequiresInitialize: true, Handle: t.handleVaultDelete})
	return true
}

func (t *Table) vaultWired() error {
	if t.Vault == nil {
		return ErrNotWired.WithWhat("the variable manager is not wired — the daemon started without a store or a key")
	}
	return nil
}

func (t *Table) handleVaultList(ctx context.Context, _ *Session, _ json.RawMessage) (any, error) {
	if err := t.vaultWired(); err != nil {
		return nil, err
	}
	return t.Vault.List(ctx)
}

func (t *Table) handleVaultChanges(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	if err := t.vaultWired(); err != nil {
		return nil, err
	}
	var p VaultChangesParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	return t.Vault.Changes(ctx, p)
}

func (t *Table) handleVaultSet(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := t.vaultWired(); err != nil {
		return nil, err
	}
	var p VaultSetParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Name) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("name is required — the variable's name")}
	}
	if p.Actor != "" {
		return nil, refuseActor("actor")
	}
	actor, err := t.controlActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	res, err := t.Vault.Set(ctx, p, actor)
	if err != nil {
		return nil, vaultRefusal(err)
	}
	return res, nil
}

func (t *Table) handleVaultDelete(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := t.vaultWired(); err != nil {
		return nil, err
	}
	var p VaultDeleteParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Name) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("name is required — the variable to delete")}
	}
	if p.Actor != "" {
		return nil, refuseActor("actor")
	}
	actor, err := t.controlActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	res, err := t.Vault.Delete(ctx, p, actor)
	if err != nil {
		return nil, vaultRefusal(err)
	}
	return res, nil
}

// vaultRefusal carries a "refused: …" sentence verbatim as a bad-params error
// (the manager judged the request; nothing was written) and lets anything else
// through as the infrastructure failure it is.
func vaultRefusal(err error) error {
	if strings.HasPrefix(err.Error(), "refused:") {
		return ErrBadParams{Err: err}
	}
	return err
}
