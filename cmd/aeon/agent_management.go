package main

// agent_management.go — S43 T7/T8's ONE production wiring for the
// agent-management seam: the entry point is the only place that may import
// internal/agentmgr (the deletability discipline, like agentsvc and plugin),
// so this builds the closure over agentmgr.Manager and hands it to
// daemon.Options.AgentManage. The daemon and the protocol hold only the plain
// func; a daemon started without this call answers every agent-management
// method with ErrNotWired by name (never silently permissive, P34: the daemon
// is the primary surface).
//
// S44 T10: the config for unsigned import is read PER import call through the
// shared *config.Store (opts.Settings, env → database → default), with S43's old
// env var (AEON_AGENTMANAGE_ALLOW_UNSIGNED_IMPORT) kept as a one-release
// fallback. The read is lazy (opts.Settings is set after this is called), so
// the value is live, never captured at construction (CS7/CS11).

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/cjlapao/aeon/internal/agentcard"
	"github.com/cjlapao/aeon/internal/agentmgr"
	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// agentManageHandleFor builds the closure over the one agent-management
// Manager, dispatched by method name. It is the single place the agent
// operations live; the CLI and the app are secondary clients of these methods
// (P34), never a second implementation.
//
// S44 T10: the unsigned-import override is read PER import call, never captured
// at construction (CS7/CS11). It reads the S44 config store (env → database →
// default) through opts.Settings; S43's old env var
// (AEON_AGENTMANAGE_ALLOW_UNSIGNED_IMPORT — a different name from the config
// key's env, AEON_SETTING_AGENTS_ALLOWUNSIGNEDIMPORT) is kept working as a
// one-release fallback so a deployed script does not silently stop working.
// opts.Settings is set a few lines after this is called (serve.go), so the read
// is lazy: by import time it is set.
func agentManageHandleFor(st *store.Store, opts *daemon.Options) func(ctx context.Context, method string, params json.RawMessage) (any, string, error) {
	// allowUnsignedImport reads the setting live on each import call. The old
	// env var wins for one release (its name differs from the config key's env
	// name); otherwise the config store answers (env → DB → default). A read
	// error or a nil store refuses (the default) — never a silent permit
	// (P31/CS7).
	allowUnsignedImport := func(ctx context.Context) bool {
		if v, ok := os.LookupEnv("AEON_AGENTMANAGE_ALLOW_UNSIGNED_IMPORT"); ok {
			return v == "1" || v == "true" || v == "yes"
		}
		if opts != nil && opts.Settings != nil {
			got, err := opts.Settings.Bool(ctx, "agents.allowUnsignedImport")
			if err == nil {
				return got
			}
		}
		return false
	}
	mgr := agentmgr.NewManager(
		st,
		agentcard.VerifyCardSignature,
		// S48 T6: the AM4 blueprint half is the DERIVED-table query — who
		// uses this agent, by blueprint NAME (the old scan matched pinned
		// loop content text and answered loop ids; the seed-era text scan
		// is retired here, recorded in S48 T6 Notes).
		func(slug string) ([]string, error) { return st.BlueprintRefsFor(context.Background(), "agent", slug) },
		allowUnsignedImport,
	)
	return func(ctx context.Context, method string, params json.RawMessage) (any, string, error) {
		// writeActor reads the peer stamp the protocol's write edge installed for
		// the six write verbs (S73 T4, AR4 paths 1–6). The edge already refuses a
		// nameless connection at dispatch, so finding nothing here means the seam
		// was reached by a road that did not come through it — a hand-built Table, a
		// direct call in a test, or a future registration that forgot the edge. The
		// answer is the protocol's own no-actor error, returned as the ERROR channel
		// (not the refusal string): a missing actor is not the domain declining a
		// request, it is the transport failing to name one, and the wire voice is
		// -31970 either way.
		writeActor := func() (store.AgentActor, error) {
			kind, id, ok := protocol.AgentActor(ctx)
			if !ok {
				return store.AgentActor{}, protocol.ErrAgentNoActor.WithWhat(
					"%s reached the management seam with no actor in its context — the dispatch edge stamps the peer and this request did not come through it", method)
			}
			return store.AgentActor{Kind: kind, ID: id}, nil
		}
		switch method {
		case "agent.rows":
			// S73 T2: the management window, and the daemon's first paged read
			// (docs/aeon/PAGING.md). The `agent.list` branch that used to sit
			// here is gone: agent.list is answered by the ROSTER seam
			// (protocol.handleAgentList → Table.AgentRoster → the CLI's
			// agentRoster), so this branch was unreachable code wearing the
			// name of the busiest read on the agent plane — and mgr.List, the
			// method it called, is deleted with it (see the note where it stood,
			// in internal/agentmgr/agentmgr.go).
			return agentRows(ctx, st, params)
		case "agent.changes":
			// S73 T5: the ledger's window, the convention's second adopter. A
			// read over the same one seam, so it takes no actor (SP6) and
			// answers [] with an honest total for a slug nobody has touched.
			return agentChanges(ctx, st, params)
		case "agent.view":
			var p struct {
				Slug string `json:"slug"`
			}
			if err := json.Unmarshal(params, &p); err != nil || p.Slug == "" {
				return nil, "refused: agent.view needs a slug", nil
			}
			a, found, err := mgr.View(ctx, p.Slug)
			if err != nil {
				return nil, "", err
			}
			if !found {
				return nil, fmt.Sprintf("refused: %s has no agent row", p.Slug), nil
			}
			return a, "", nil
		case "agent.create":
			var p struct {
				Name          string   `json:"name"`
				Description   string   `json:"description"`
				Functionality string   `json:"functionality"`
				Prompt        string   `json:"prompt"`
				Tech          []string `json:"tech"`
				Tools         []string `json:"tools"`
				AvatarColor   string   `json:"avatarColor"`
				AvatarMinion  string   `json:"avatarMinion"`
				// Handoff is field name → purpose; a "?" on the name marks it optional.
				Handoff map[string]string `json:"handoff"`
			}
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, "refused: agent.create needs valid parameters", nil
			}
			actor, err := writeActor()
			if err != nil {
				return nil, "", err
			}
			a, err := mgr.Create(ctx, actor, agentmgr.CreateInput{
				Name: p.Name, Description: p.Description, Functionality: p.Functionality,
				Prompt: p.Prompt, Tech: p.Tech, Tools: p.Tools, AvatarColor: p.AvatarColor, AvatarMinion: p.AvatarMinion, Handoff: p.Handoff,
			})
			if err != nil {
				return nil, err.Error(), nil
			}
			return a, "", nil
		case "agent.import":
			// The card arrives one of two ways: a path on the daemon's machine, or the card's own
			// contents (a window that holds the dropped file). Exactly one, named.
			var p struct {
				Path    string `json:"path"`
				Content string `json:"content"`
				// AcceptUnsigned is the person's yes, for this import, to a card that carries no signature.
				AcceptUnsigned bool `json:"acceptUnsigned"`
			}
			if err := json.Unmarshal(params, &p); err != nil || (p.Path == "") == (p.Content == "") {
				return nil, "refused: agent.import needs a card — either a file path or the card's contents, not both and not neither", nil
			}
			actor, err := writeActor()
			if err != nil {
				return nil, "", err
			}
			var (
				a   store.Agent
				res store.ImportResult
			)
			if p.Content != "" {
				a, res, err = mgr.ImportCard(ctx, actor, []byte(p.Content), agentmgr.ImportOptions{AcceptUnsigned: p.AcceptUnsigned})
			} else {
				a, res, err = mgr.Import(ctx, actor, p.Path, agentmgr.ImportOptions{AcceptUnsigned: p.AcceptUnsigned})
			}
			if err != nil {
				return nil, err.Error(), nil
			}
			return importResult{Agent: a, Result: res.String()}, "", nil
		case "agent.export":
			var p struct {
				Slug string `json:"slug"`
				Path string `json:"path"`
			}
			if err := json.Unmarshal(params, &p); err != nil || p.Slug == "" {
				return nil, "refused: agent.export needs a slug", nil
			}
			a, found, err := mgr.View(ctx, p.Slug)
			if err != nil {
				return nil, "", err
			}
			if !found {
				return nil, fmt.Sprintf("refused: %s has no agent row to export", p.Slug), nil
			}
			out, err := exportCard(a, p.Path)
			if err != nil {
				return nil, err.Error(), nil
			}
			return out, "", nil
		case "agent.duplicate":
			var p struct {
				Slug string `json:"slug"`
				Name string `json:"name"`
			}
			if err := json.Unmarshal(params, &p); err != nil || p.Slug == "" || p.Name == "" {
				return nil, "refused: agent.duplicate needs a slug and a new name", nil
			}
			actor, err := writeActor()
			if err != nil {
				return nil, "", err
			}
			a, err := mgr.Duplicate(ctx, actor, p.Slug, p.Name)
			if err != nil {
				return nil, err.Error(), nil
			}
			return a, "", nil
		case "agent.delete":
			var p struct {
				Slug string `json:"slug"`
			}
			if err := json.Unmarshal(params, &p); err != nil || p.Slug == "" {
				return nil, "refused: agent.delete needs a slug", nil
			}
			actor, aerr := writeActor()
			if aerr != nil {
				return nil, "", aerr
			}
			if err := mgr.Delete(ctx, actor, p.Slug); err != nil {
				return nil, err.Error(), nil
			}
			return map[string]string{"deleted": p.Slug}, "", nil
		case "agent.edit":
			var p struct {
				Slug   string      `json:"slug"`
				Fields store.Agent `json:"fields"`
			}
			if err := json.Unmarshal(params, &p); err != nil || p.Slug == "" {
				return nil, "refused: agent.edit needs a slug and fields", nil
			}
			actor, aerr := writeActor()
			if aerr != nil {
				return nil, "", aerr
			}
			if err := mgr.Edit(ctx, actor, p.Slug, p.Fields, mgr.Now()); err != nil {
				return nil, err.Error(), nil
			}
			return map[string]string{"edited": p.Slug}, "", nil
		case "agent.migrate":
			actor, err := writeActor()
			if err != nil {
				return nil, "", err
			}
			n, err := mgr.Migrate(ctx, actor)
			if err != nil {
				return nil, err.Error(), nil
			}
			return map[string]int{"created": n}, "", nil
		default:
			return nil, "", fmt.Errorf("refused: %q is not a registered agent-management method", method)
		}
	}
}

// importResult is the import's answer — the agent plus the (slug, version)
// idempotency result (AM3): created, updated, or no-op.
type importResult struct {
	Agent  store.Agent `json:"agent"`
	Result string      `json:"result"`
}

// exportCard renders an agent to a card on disk (or, when no path is given,
// the card JSON as the method's result). An export is unsigned — the card is
// the operator's own, and the signature is the signer's, not the exporter's;
// the importer verifies whatever is there.
func exportCard(a store.Agent, path string) (string, error) {
	card, err := json.Marshal(map[string]any{
		"slug":          a.Slug,
		"name":          a.Name,
		"description":   a.Description,
		"functionality": a.Functionality,
		"metadata":      a.Metadata,
		"prompt":        a.Prompt,
		"skills":        a.Skills,
		"version":       a.Version,
		"avatar_type":   a.AvatarType,
		"avatar_color":  a.AvatarColor,
		"avatar_mood":   a.AvatarMood,
	})
	if err != nil {
		return "", fmt.Errorf("refused: cannot marshal the card: %w", err)
	}
	if path == "" {
		return string(card), nil
	}
	if err := os.WriteFile(path, card, 0o600); err != nil {
		return "", fmt.Errorf("refused: cannot write the card to %q: %w", path, err)
	}
	return path, nil
}

// wireAgentManagementServices fills daemon.Options.AgentManage — called once
// from cmd/aeon serve, beside wireAgentServices. An unwired daemon answers
// every agent-management method with ErrNotWired by name, never silently
// permissive (P34).
func wireAgentManagementServices(st *store.Store, opts *daemon.Options) error {
	opts.AgentManage = agentManageHandleFor(st, opts)
	return nil
}
