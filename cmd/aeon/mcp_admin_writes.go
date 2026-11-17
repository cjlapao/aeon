package main

// mcp_admin_writes.go — S46 T5: the seven human write doors (the eighth
// write, mcp.importFile, lives in mcp_import.go and shares this file's
// discipline). The law every door obeys, from MG5:
//
//	rows := current; cand := rows.With(change)          (T2 — refusals named)
//	_, err := mcp.Assemble(cand, IncludeDisabled:true)  (THE validator; its sentence verbatim)
//	before := grants for every affected agent           (registry from the CURRENT enabled config)
//	one McpWithTx: rows + one mcp_change row            (or nothing — a refusal writes nothing)
//	after  := grants from the new rows
//
// A failed validation writes NOTHING — no rows, no audit row. A no-op
// (changed:false) writes nothing either (MG5 behaviour note 4). Params
// never carry actor/origin/id/timestamps: the protocol edge refuses that
// shape; the actor arrives stamped in the context (dispatch stamps the
// peer credential; a direct-door caller stamps it itself).

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/mcp"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// mcpWrite is the router's entry for the seven write methods: the actor is
// required BEFORE anything is read or judged (an unattributable write does
// not even get to the validator), then each door decodes its own params.
func mcpWrite(ctx context.Context, admin *mcpAdmin, method string, params json.RawMessage) (any, string, error) {
	res, refusal, err := mcpWriteRoute(ctx, admin, method, params)
	if err == nil && refusal == "" {
		mcpReconcileAfter(ctx, method, params)
	}
	return res, refusal, err
}

// mcpReconcileAfter tells the supervisor a server's configuration changed: a new
// enabled server starts, a changed one restarts, a removed or disabled one stops.
func mcpReconcileAfter(ctx context.Context, method string, params json.RawMessage) {
	if mcpSup == nil {
		return
	}
	switch method {
	case "mcp.server.add", "mcp.server.update", "mcp.server.remove", "mcp.server.setEnabled", "mcp.server.share":
		var p struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(params, &p) == nil && p.Name != "" {
			mcpSup.Reconcile(context.WithoutCancel(ctx), p.Name)
		}
	}
}

func mcpWriteRoute(ctx context.Context, admin *mcpAdmin, method string, params json.RawMessage) (any, string, error) {
	kind, id, ok := protocol.MCPActor(ctx)
	if !ok || kind == "" || id == "" || id == "0" {
		return nil, "", protocol.ErrMcpNoActor.WithWhat("%s needs a person, and this call carried no actor stamp — the write is refused before the validator is asked, and nothing is written", method)
	}
	switch method {
	case "mcp.process.start", "mcp.process.stop", "mcp.process.restart":
		return mcpProcessControl(ctx, method, params, kind, id)
	case "mcp.server.add":
		var p protocol.MCPServerAddParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, fmt.Sprintf("refused: mcp.server.add params are malformed (%v)", err), nil
		}
		return admin.writeServerAdd(ctx, p, kind, id)
	case "mcp.server.inspect":
		var p protocol.MCPServerInspectParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, fmt.Sprintf("refused: mcp.server.inspect params are malformed (%v)", err), nil
		}
		return admin.inspectServer(ctx, p)
	case "mcp.server.update":
		var p protocol.MCPServerUpdateParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, fmt.Sprintf("refused: mcp.server.update params are malformed (%v)", err), nil
		}
		return admin.writeServerUpdate(ctx, p, kind, id)
	case "mcp.server.remove":
		var p protocol.MCPServerRemoveParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, fmt.Sprintf("refused: mcp.server.remove params are malformed (%v)", err), nil
		}
		return admin.writeServerRemove(ctx, p, kind, id)
	case "mcp.server.setEnabled":
		var p protocol.MCPServerSetEnabledParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, fmt.Sprintf("refused: mcp.server.setEnabled params are malformed (%v)", err), nil
		}
		return admin.writeServerSetEnabled(ctx, p, kind, id)
	case "mcp.server.share":
		var p protocol.MCPServerShareParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, fmt.Sprintf("refused: mcp.server.share params are malformed (%v)", err), nil
		}
		return admin.writeServerShare(ctx, p, kind, id)
	case "mcp.grant.declare":
		var p protocol.MCPGrantDeclareParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, fmt.Sprintf("refused: mcp.grant.declare params are malformed (%v)", err), nil
		}
		return admin.writeGrantDeclare(ctx, p, kind, id)
	case "mcp.grant.revoke":
		var p protocol.MCPGrantRevokeParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, fmt.Sprintf("refused: mcp.grant.revoke params are malformed (%v)", err), nil
		}
		return admin.writeGrantRevoke(ctx, p, kind, id)
	case "mcp.core.set":
		var p protocol.MCPCoreSetParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, fmt.Sprintf("refused: mcp.core.set params are malformed (%v)", err), nil
		}
		return admin.writeCoreSet(ctx, p, kind, id)
	}
	return nil, "", fmt.Errorf("mcpWrite reached an unrouted method %q", method)
}

// writeSpec is one prepared write: the landing writers, the audit rows it
// earns, and who must be shown a before/after.
type writeSpec struct {
	affected  []string // agent dispatch KEYS, sorted
	land      func(ctx context.Context, tx *sql.Tx) (bool, error)
	audits    func(changed bool) []store.McpChange
	serverRow *string // the server name whose MCPServerView to attach
	warnings  []string
}

// runWrite is the shared pipeline — the one place MG5's law lives.
func (a *mcpAdmin) runWrite(ctx context.Context, kind, id string, all store.McpAll, spec writeSpec) (any, string, error) {
	before, err := a.grantViewsFor(ctx, all, spec.affected)
	if err != nil {
		return nil, "", err
	}
	changed := false
	err = a.st.McpWithTx(ctx, func(tx *sql.Tx) error {
		c, lerr := spec.land(ctx, tx)
		if lerr != nil {
			return lerr
		}
		changed = c
		if changed {
			for _, au := range spec.audits(true) {
				au.ActorKind, au.ActorID, au.OccurredAt = kind, id, mcpStamp(a)
				if aerr := store.McpChangeInsertTx(ctx, tx, au); aerr != nil {
					return aerr
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	res := protocol.MCPWriteResult{Changed: changed, Grants: []protocol.MCPAgentGrants{}}
	if !changed {
		// A no-op answers honestly and writes NOTHING — the before IS the
		// after; show both rather than hiding the fact behind emptiness.
		for _, k := range spec.affected {
			res.Grants = append(res.Grants, protocol.MCPAgentGrants{Agent: k, Before: before[k], After: before[k]})
		}
		res.Warnings = spec.warnings
		return res, "", nil
	}
	all2, err := a.st.McpRows(ctx)
	if err != nil {
		return nil, "", err
	}
	after, err := a.grantViewsFor(ctx, all2, spec.affected)
	if err != nil {
		return nil, "", err
	}
	for _, k := range spec.affected {
		res.Grants = append(res.Grants, protocol.MCPAgentGrants{Agent: k, Before: before[k], After: after[k]})
	}
	if spec.serverRow != nil {
		for _, r := range all2.Servers {
			if r.Name == *spec.serverRow {
				n, _ := declarersOf(a, ctx, r.Name)
				v, verr := serverView(a, r, nil, n)
				if verr != nil {
					return nil, "", verr
				}
				res.Server = &v
				break
			}
		}
	}
	res.Warnings = spec.warnings
	return res, "", nil
}

// grantViewsFor answers the before/after half: the Registry (enabled config
// only — MG4: disabled is not offered) over the one policy set the daemon
// decides with, per affected agent.
func (a *mcpAdmin) grantViewsFor(ctx context.Context, all store.McpAll, agentKeys []string) (map[string][]protocol.MCPGrantView, error) {
	out := map[string][]protocol.MCPGrantView{}
	if len(agentKeys) == 0 {
		return out, nil
	}
	rows, err := a.toRows(all)
	if err != nil {
		return nil, err
	}
	// The New-able config is the include-disabled CANDIDATE (T2's law:
	// the filtered view is deliberately not New-able, and mcp.New
	// re-validates; MG4's drop happens at the offer/list seam, never
	// here — the grant view carries declares over a disabled server as
	// the truth that mcp.grants answers).
	cfg, err := mcp.Assemble(rows, mcp.AssembleOptions{IncludeDisabled: true})
	if err != nil {
		if !isNoConfig(err) {
			return nil, fmt.Errorf("the MCP rows refused their own validator (%v) — grants would be invented", err)
		}
		cfg = mcp.Config{}
	}
	reg, err := mcp.New(cfg)
	if err != nil {
		return nil, err
	}
	ps, frej := policy.NewStore(policyRepo(a.st)).List(ctx, a.now())
	if frej != nil {
		return nil, fmt.Errorf("the policy set could not be read (%s) — grants would be invented", frej.Reason)
	}
	at := a.now()
	for _, k := range agentKeys {
		views := reg.GrantView(k, ps, at)
		gv := make([]protocol.MCPGrantView, 0, len(views))
		for _, g := range views {
			gv = append(gv, protocol.MCPGrantView{Qualified: g.Qualified, Tier: string(g.Tier), State: string(g.State), Reason: g.Reason})
		}
		out[k] = gv
	}
	return out, nil
}

// candidateValidated is the gate every write passes BEFORE a transaction:
// With's own named refusals, then THE validator over the whole candidate
// with disabled rows included. A refusal is the sentence verbatim and
// writes nothing — no rows, no audit row (MG5).
func (a *mcpAdmin) candidateValidated(all store.McpAll, change mcp.Change) (string, error) {
	rows, err := a.toRows(all)
	if err != nil {
		return "", err
	}
	cand, err := rows.With(change)
	if err != nil {
		return err.Error(), nil
	}
	if _, err := mcp.Assemble(cand, mcp.AssembleOptions{IncludeDisabled: true}); err != nil {
		return fmt.Sprintf("%v", err), nil
	}
	return "", nil
}

// mcpStamp renders a.now() the way the store's time columns speak —
// RFC3339 UTC, the same rendering the importer and the seeder use (one
// clock format in the MCP tables, not three).
func mcpStamp(a *mcpAdmin) string { return a.now().UTC().Format(time.RFC3339) }

func ptrStr(s string) *string { return &s }

func slugPtr(slug string) *string {
	if slug == "" {
		return nil
	}
	return &slug
}

func serverPart(qualified string) string {
	if i := strings.Index(qualified, "."); i > 0 {
		return qualified[:i]
	}
	return qualified
}

func unclassifiedWarning(tools []protocol.MCPToolInput) []string {
	n := 0
	for _, t := range tools {
		if t.Rerun == "" || t.Effect == "" {
			n++
		}
	}
	if n == 0 {
		return nil
	}
	return []string{fmt.Sprintf("%d tool(s) are UNCLASSIFIED (rerun/effect not both given) and will be refused until classified (MC6)", n)}
}

// serverRowFromAddParams builds the validator's view of an add — ONE
// construction shared by mcp.server.add's door and the proposal seam
// (mcp_server_add), so the two doors cannot hold two grammars.
func serverRowFromAddParams(p protocol.MCPServerAddParams) mcp.ServerRow {
	srow := mcp.ServerRow{
		Name: p.Name, Tier: mcp.Tier(p.Tier), AgentKey: p.Agent,
		Transport: mcp.TransportConfig{Type: p.Transport.Type, Command: p.Transport.Command, Args: p.Transport.Args, Env: p.Transport.Env, EnvFrom: p.Transport.EnvFrom, URL: p.Transport.URL},
		Enabled:   true, // MC3: config presence = enabled today
	}
	for _, t := range p.Tools {
		srow.Tools = append(srow.Tools, mcp.ToolConfig{Name: t.Name, Rerun: t.Rerun, Effect: t.Effect})
	}
	if p.Enabled != nil {
		srow.Enabled = *p.Enabled
	}
	return srow
}

// mcpJSONArrayOrEmpty renders a params array as the rows' column — the
// proposal seam's identity comparison needs the SAME rendering the writers
// use (one JSON shape, not two).
func mcpJSONArrayOrEmpty(xs []string) string {
	b, err := json.Marshal(xs)
	if err != nil || xs == nil {
		return "[]"
	}
	return string(b)
}

// toolRowsMatch compares stored tool rows against params by (name, rerun,
// effect) as a SET — the rows read back sorted, params arrive in any
// order; identity of a server includes its tools' classification.
func toolRowsMatch(rows []store.McpToolRow, in []protocol.MCPToolInput) bool {
	if len(rows) != len(in) {
		return false
	}
	byName := map[string]store.McpToolRow{}
	for _, r := range rows {
		byName[r.Name] = r
	}
	for _, t := range in {
		r, ok := byName[t.Name]
		if !ok || r.Rerun != t.Rerun || r.Effect != t.Effect {
			return false
		}
	}
	return true
}

func toolsToRows(ts []protocol.MCPToolInput) []store.McpToolRow {
	out := make([]store.McpToolRow, 0, len(ts))
	for _, t := range ts {
		out = append(out, store.McpToolRow{Name: t.Name, Rerun: t.Rerun, Effect: t.Effect})
	}
	return out
}

// currentDeclares answers one agent's declared set (slug in, sorted
// qualified out) from the consistent McpAll read.
func (a *mcpAdmin) currentDeclares(all store.McpAll, slug string) []string {
	var out []string
	for _, d := range all.Declares {
		if d.AgentSlug == slug {
			out = append(out, d.Qualified)
		}
	}
	sort.Strings(out)
	return out
}

// --- mcp.server.add -------------------------------------------------------------------

func (a *mcpAdmin) writeServerAdd(ctx context.Context, p protocol.MCPServerAddParams, kind, id string) (any, string, error) {
	if strings.TrimSpace(p.Name) == "" {
		return nil, "refused: mcp.server.add needs a `name` — it is the identity, and rename is add + remove (MG5)", nil
	}
	tier := mcp.Tier(p.Tier)
	if tier != mcp.TierPublic && tier != mcp.TierPrivate {
		return nil, fmt.Sprintf("refused: mcp.server.add tier must be \"public\" or \"private\" — got %q (the vocabulary is closed)", p.Tier), nil
	}
	if tier == mcp.TierPrivate && strings.TrimSpace(p.Agent) == "" {
		return nil, "refused: a private server names its owner — `agent` is required for tier private (MC2: private is invisible to peers, so its owner must be named)", nil
	}
	if tier == mcp.TierPublic && strings.TrimSpace(p.Agent) != "" {
		return nil, "refused: `agent` on a public server contradicts itself — public servers have no owner (MC5)", nil
	}
	if p.Transport.Type != "stdio" && p.Transport.Type != "http" {
		return nil, fmt.Sprintf("refused: mcp.server.add transport.type must be \"stdio\" or \"http\" — got %q (the vocabulary is closed; the validator's own grammar checks the rest)", p.Transport.Type), nil
	}
	var ownerSlug string
	if tier == mcp.TierPrivate {
		ag, found, err := a.st.AgentByName(p.Agent)
		if err != nil {
			return nil, "", err
		}
		if !found {
			agents, aerr := a.st.AgentsAll()
			if aerr != nil {
				return nil, "", aerr
			}
			var names []string
			for _, x := range agents {
				names = append(names, x.Slug)
			}
			sort.Strings(names)
			return nil, "", protocol.ErrMcpUnknown.WithWhat("unknown agent %q — known agents: %s", p.Agent, strings.Join(names, ", "))
		}
		ownerSlug = ag.Slug
	}
	all, err := a.st.McpRows(ctx)
	if err != nil {
		return nil, "", err
	}
	// Every env name is bound to a stored variable or a typed value — never empty.
	envWrites, envRefusal := bindEnvironment(p.Name, &p.Transport, nil)
	if envRefusal != "" {
		return nil, envRefusal, nil
	}
	srow := serverRowFromAddParams(p)
	if refusal, werr := a.candidateValidated(all, mcp.AddServer{Server: srow}); werr != nil {
		return nil, "", werr
	} else if refusal != "" {
		return nil, refusal, nil
	}
	// A server with no tools offers an agent nothing — the registry refuses any
	// tool this list does not carry — so the door refuses one. After the
	// validator, so a malformed name or transport keeps the validator's own
	// sentence. `Inspect` is how a person gets the list.
	if len(p.Tools) == 0 {
		return nil, "refused: a server needs at least one tool — Inspect runs the command, reads the tools the server offers and stops it, then you add the server with the tools you choose (a server with no tools offers an agent nothing, and the registry refuses any tool this list does not carry)", nil
	}
	// MC5: adding a PUBLIC server grants nothing — nobody is affected, so
	// the result's grants list is empty by construction (a test asserts
	// every agent's view stands). A private server affects its owner
	// alone (MC2 — peers do not even see it).
	var affected []string
	if tier == mcp.TierPrivate {
		affected = []string{p.Agent}
	}
	// Validated: only now does a typed value reach the manager.
	if err := applyEnvValues(ctx, envWrites, id); err != nil {
		return nil, "", err
	}
	toolsN := len(p.Tools)
	enabled := srow.Enabled
	tierWord, agentSlug := p.Tier, ownerSlug
	name := p.Name
	argsJSON, jerr := mcpJSONArrayFor(p.Transport.Args)
	if jerr != nil {
		return nil, "", jerr
	}
	envJSON, jerr := mcpJSONArrayFor(p.Transport.Env)
	if jerr != nil {
		return nil, "", jerr
	}
	envFromJSON, jerr := mcpJSONObjectFor(p.Transport.EnvFrom)
	if jerr != nil {
		return nil, "", jerr
	}
	spec := writeSpec{
		affected:  affected,
		serverRow: ptrStr(name),
		warnings:  unclassifiedWarning(p.Tools),
		land: func(ctx context.Context, tx *sql.Tx) (bool, error) {
			_, aerr := store.McpAddServerTx(ctx, tx, store.McpServerRow{
				Name: name, Title: strings.TrimSpace(p.Title), Description: strings.TrimSpace(p.Description), Tier: tierWord, AgentSlug: slugPtr(agentSlug),
				TransportType: p.Transport.Type, Command: p.Transport.Command,
				Args: argsJSON, Env: envJSON, EnvFrom: envFromJSON,
				URL: p.Transport.URL, Enabled: enabled, Origin: "user",
				CreatedAt: mcpStamp(a), UpdatedAt: mcpStamp(a),
				Tools: toolsToRows(p.Tools),
			})
			return true, aerr
		},
		audits: func(bool) []store.McpChange {
			return []store.McpChange{{
				Op: "server_add", Server: ptrStr(name),
				Summary: fmt.Sprintf("added %s server %s (%s, %d tools)", tierWord, name, p.Transport.Type, toolsN),
			}}
		},
	}
	return a.runWrite(ctx, kind, id, all, spec)
}

// --- mcp.server.update ------------------------------------------------------------------

func (a *mcpAdmin) writeServerUpdate(ctx context.Context, p protocol.MCPServerUpdateParams, kind, id string) (any, string, error) {
	if p.Transport == nil && p.Tools == nil && p.Title == nil && p.Description == nil {
		return nil, "refused: mcp.server.update must carry at least one section to replace (`transport`, `tools`, `title` and/or `description`) — an update that replaces nothing is not a thing (MG5: whole-field replacement of the PROVIDED sections)", nil
	}
	all, err := a.st.McpRows(ctx)
	if err != nil {
		return nil, "", err
	}
	var cur *store.McpServerRow
	for i := range all.Servers {
		if all.Servers[i].Name == p.Name {
			cur = &all.Servers[i]
		}
	}
	if cur == nil {
		return nil, "", unknownServerError(a, ctx, p.Name)
	}
	if cur.Origin == "system" {
		return nil, fmt.Sprintf("refused: server %q is origin=system — a system row is edited by neither door (MG10); if you mean your own variant, mcp.server.add it under a new name", p.Name), nil
	}
	// Merge the provided sections over the current row (UpdateServer
	// replaces whole fields; "provided sections only" lives HERE).
	transport := mcp.TransportConfig{Type: cur.TransportType, Command: cur.Command, URL: cur.URL}
	_ = json.Unmarshal([]byte(cur.Args), &transport.Args)
	_ = json.Unmarshal([]byte(cur.Env), &transport.Env)
	curEnvFrom := map[string]string{}
	_ = json.Unmarshal([]byte(cur.EnvFrom), &curEnvFrom)
	transport.EnvFrom = nilIfEmpty(curEnvFrom)
	tools := make([]mcp.ToolConfig, 0, len(cur.Tools))
	for _, t := range cur.Tools {
		tools = append(tools, mcp.ToolConfig{Name: t.Name, Rerun: t.Rerun, Effect: t.Effect})
	}
	var inTools []protocol.MCPToolInput
	var envWrites []envValueWrite
	if p.Transport != nil {
		if p.Transport.Type != "stdio" && p.Transport.Type != "http" {
			return nil, fmt.Sprintf("refused: transport.type must be \"stdio\" or \"http\" — got %q", p.Transport.Type), nil
		}
		in := *p.Transport
		var bindRefusal string
		envWrites, bindRefusal = bindEnvironment(p.Name, &in, curEnvFrom)
		if bindRefusal != "" {
			return nil, bindRefusal, nil
		}
		transport = mcp.TransportConfig{Type: in.Type, Command: in.Command, Args: in.Args, Env: in.Env, EnvFrom: in.EnvFrom, URL: in.URL}
	}
	if p.Tools != nil {
		tools = tools[:0]
		inTools = *p.Tools
		for _, t := range inTools {
			tools = append(tools, mcp.ToolConfig{Name: t.Name, Rerun: t.Rerun, Effect: t.Effect})
		}
	}
	srow := mcp.ServerRow{Name: p.Name, Tier: mcp.Tier(cur.Tier), Enabled: cur.Enabled, Transport: transport, Tools: tools}
	if cur.AgentSlug != nil {
		if k, ok := a.mapKey(*cur.AgentSlug); ok {
			srow.AgentKey = k
		}
	}
	if refusal, werr := a.candidateValidated(all, mcp.UpdateServer{Server: srow}); werr != nil {
		return nil, "", werr
	} else if refusal != "" {
		return nil, refusal, nil
	}
	if err := applyEnvValues(ctx, envWrites, id); err != nil {
		return nil, "", err
	}
	_, declarers := declarersOf(a, ctx, p.Name)
	affectedMap := map[string]bool{}
	for _, n := range declarers {
		affectedMap[n] = true
	}
	if srow.AgentKey != "" {
		affectedMap[srow.AgentKey] = true
	}
	var affected []string
	for k := range affectedMap {
		affected = append(affected, k)
	}
	sort.Strings(affected)
	sections := []string{}
	if p.Transport != nil {
		sections = append(sections, "transport")
	}
	if p.Tools != nil {
		sections = append(sections, fmt.Sprintf("tools (%d)", len(tools)))
	}
	title, description := cur.Title, cur.Description
	if p.Title != nil {
		title = strings.TrimSpace(*p.Title)
		sections = append(sections, "title")
	}
	if p.Description != nil {
		description = strings.TrimSpace(*p.Description)
		sections = append(sections, "description")
	}
	updateTools, updateTransport, agentSlug, curRow := tools, transport, cur.AgentSlug, *cur
	name := p.Name
	argsJSON, jerr := mcpJSONArrayFor(updateTransport.Args)
	if jerr != nil {
		return nil, "", jerr
	}
	envJSON, jerr := mcpJSONArrayFor(updateTransport.Env)
	if jerr != nil {
		return nil, "", jerr
	}
	envFromJSON, jerr := mcpJSONObjectFor(updateTransport.EnvFrom)
	if jerr != nil {
		return nil, "", jerr
	}
	spec := writeSpec{
		affected:  affected,
		serverRow: ptrStr(name),
		warnings:  unclassifiedWarning(inTools),
		land: func(ctx context.Context, tx *sql.Tx) (bool, error) {
			uerr := store.McpUpdateServerTx(ctx, tx, store.McpServerRow{
				Name: name, Title: title, Description: description, Tier: curRow.Tier, AgentSlug: agentSlug,
				TransportType: updateTransport.Type, Command: updateTransport.Command,
				Args: argsJSON, Env: envJSON, EnvFrom: envFromJSON,
				URL: updateTransport.URL, Enabled: curRow.Enabled, Origin: curRow.Origin,
				UpdatedAt: mcpStamp(a),
				Tools:     toolsFromConfigs(updateTools),
			})
			return true, uerr
		},
		audits: func(bool) []store.McpChange {
			return []store.McpChange{{
				Op: "server_update", Server: ptrStr(name),
				Summary: fmt.Sprintf("updated server %s (%s)", name, strings.Join(sections, ", ")),
			}}
		},
	}
	return a.runWrite(ctx, kind, id, all, spec)
}

func toolsFromConfigs(ts []mcp.ToolConfig) []store.McpToolRow {
	out := make([]store.McpToolRow, 0, len(ts))
	for _, t := range ts {
		out = append(out, store.McpToolRow{Name: t.Name, Rerun: t.Rerun, Effect: t.Effect})
	}
	return out
}

// --- mcp.server.remove -------------------------------------------------------------------

func (a *mcpAdmin) writeServerRemove(ctx context.Context, p protocol.MCPServerRemoveParams, kind, id string) (any, string, error) {
	all, err := a.st.McpRows(ctx)
	if err != nil {
		return nil, "", err
	}
	var cur *store.McpServerRow
	for i := range all.Servers {
		if all.Servers[i].Name == p.Name {
			cur = &all.Servers[i]
		}
	}
	if cur == nil {
		return nil, "", unknownServerError(a, ctx, p.Name)
	}
	if cur.Origin == "system" {
		return nil, fmt.Sprintf("refused: server %q is origin=system — a system row is removed by neither door (MG10)", p.Name), nil
	}
	// Declarations naming this server's tools are refused unless the
	// caller means the cascade — and the refusal NAMES them (never a
	// silent revoke, T2's law 6).
	prefix := p.Name + "."
	var revoked []string
	revokedCount := 0
	declarers := map[string]bool{}
	for _, d := range all.Declares {
		if strings.HasPrefix(d.Qualified, prefix) {
			key := d.AgentSlug
			if k, ok := a.mapKey(d.AgentSlug); ok {
				key = k
			}
			revoked = append(revoked, key+": "+d.Qualified)
			revokedCount++
			declarers[key] = true
		}
	}
	// The gate validates the CANDIDATE (server gone, declares surviving or
	// cascaded per the flag's meaning at the rows); the declares-block-
	// without-cascade law is THIS door's typed ErrMcpInUse below — With's
	// own refusal speaks for the cascade shape the caller must mean.
	if refusal, werr := a.candidateValidated(all, mcp.RemoveServer{Name: p.Name, CascadeDeclares: true}); werr != nil {
		return nil, "", werr
	} else if refusal != "" {
		return nil, refusal, nil
	}
	sort.Strings(revoked)
	if revokedCount > 0 && !p.CascadeDeclares {
		var names []string
		for k := range declarers {
			names = append(names, k)
		}
		sort.Strings(names)
		return nil, "", protocol.ErrMcpInUse.WithWhat("removing %q would revoke declarations held by %s — pass cascadeDeclares:true to mean it (each revoke is then listed in the result and audited as its own grant_revoke row)", p.Name, strings.Join(names, ", "))
	}
	var affected []string
	for k := range declarers {
		affected = append(affected, k)
	}
	if cur.AgentSlug != nil {
		if k, ok := a.mapKey(*cur.AgentSlug); ok {
			affected = append(affected, k)
		}
	}
	sort.Strings(affected)
	toolsN, cascade := len(cur.Tools), p.CascadeDeclares
	revokedCopy := append([]string{}, revoked...)
	name := p.Name
	spec := writeSpec{
		affected: affected,
		land: func(ctx context.Context, tx *sql.Tx) (bool, error) {
			if cascade && revokedCount > 0 {
				if _, uerr := store.McpUndeclarePrefixTx(ctx, tx, prefix); uerr != nil {
					return false, uerr
				}
			}
			_, rerr := store.McpRemoveServerTx(ctx, tx, name)
			return true, rerr
		},
		audits: func(bool) []store.McpChange {
			suffix := ""
			if len(revokedCopy) > 0 {
				suffix = ", declarations cascaded"
			}
			rows := []store.McpChange{{
				Op: "server_remove", Server: ptrStr(name),
				Summary: fmt.Sprintf("removed server %s (%d tools%s)", name, toolsN, suffix),
			}}
			for _, r := range revokedCopy {
				parts := strings.SplitN(r, ": ", 2)
				rows = append(rows, store.McpChange{
					Op: "grant_revoke", Server: ptrStr(name),
					Summary: fmt.Sprintf("cascade removal of %s revoked %s from %s", name, parts[1], parts[0]),
				})
			}
			return rows
		},
	}
	res, refusal, werr := a.runWrite(ctx, kind, id, all, spec)
	if werr != nil || refusal != "" {
		return res, refusal, werr
	}
	out := res.(protocol.MCPWriteResult)
	out.Revoked = revokedCopy
	return out, "", nil
}

// --- mcp.server.setEnabled -----------------------------------------------------------------

func (a *mcpAdmin) writeServerSetEnabled(ctx context.Context, p protocol.MCPServerSetEnabledParams, kind, id string) (any, string, error) {
	all, err := a.st.McpRows(ctx)
	if err != nil {
		return nil, "", err
	}
	var cur *store.McpServerRow
	for i := range all.Servers {
		if all.Servers[i].Name == p.Name {
			cur = &all.Servers[i]
		}
	}
	if cur == nil {
		return nil, "", unknownServerError(a, ctx, p.Name)
	}
	if cur.Origin == "system" {
		return nil, fmt.Sprintf("refused: server %q is origin=system — its enabled-ness is not this door's to change (MG10)", p.Name), nil
	}
	if refusal, werr := a.candidateValidated(all, mcp.SetEnabled{Name: p.Name, Enabled: p.Enabled}); werr != nil {
		return nil, "", werr
	} else if refusal != "" {
		return nil, refusal, nil
	}
	_, declarers := declarersOf(a, ctx, p.Name)
	affectedMap := map[string]bool{}
	for _, n := range declarers {
		affectedMap[n] = true
	}
	if cur.AgentSlug != nil {
		if k, ok := a.mapKey(*cur.AgentSlug); ok {
			affectedMap[k] = true
		}
	}
	var affected []string
	for k := range affectedMap {
		affected = append(affected, k)
	}
	sort.Strings(affected)
	same := cur.Enabled == p.Enabled
	op, verb := "server_enable", "enabled"
	if !p.Enabled {
		op, verb = "server_disable", "disabled"
	}
	enabled, name := p.Enabled, p.Name
	spec := writeSpec{
		affected:  affected,
		serverRow: ptrStr(name),
		land: func(ctx context.Context, tx *sql.Tx) (bool, error) {
			if same {
				return false, nil
			}
			return store.McpSetEnabledTx(ctx, tx, name, enabled, mcpStamp(a))
		},
		audits: func(bool) []store.McpChange {
			summary := fmt.Sprintf("%s server %s", verb, name)
			if !enabled {
				summary += " (rows, tools and declarations STAND — MG4)"
			}
			return []store.McpChange{{Op: op, Server: ptrStr(name), Summary: summary}}
		},
	}
	return a.runWrite(ctx, kind, id, all, spec)
}

// --- mcp.grant.declare / revoke --------------------------------------------------------------

func (a *mcpAdmin) resolveAgent(ctx context.Context, agent string) (slug string, refusal string, err error) {
	if strings.TrimSpace(agent) == "" {
		return "", "refused: `agent` names whose grants change — it is required", nil
	}
	ag, found, aerr := a.st.AgentByName(agent)
	if aerr != nil {
		return "", "", aerr
	}
	if found {
		return ag.Slug, "", nil
	}
	agents, aerr := a.st.AgentsAll()
	if aerr != nil {
		return "", "", aerr
	}
	var names []string
	for _, x := range agents {
		names = append(names, x.Slug)
	}
	sort.Strings(names)
	return "", "", protocol.ErrMcpUnknown.WithWhat("unknown agent %q — known agents: %s", agent, strings.Join(names, ", "))
}

func (a *mcpAdmin) writeGrantDeclare(ctx context.Context, p protocol.MCPGrantDeclareParams, kind, id string) (any, string, error) {
	slug, refusal, err := a.resolveAgent(ctx, p.Agent)
	if err != nil || refusal != "" {
		return nil, refusal, err
	}
	if strings.TrimSpace(p.Qualified) == "" {
		return nil, "refused: mcp.grant.declare needs `qualified` as SERVER.TOOL — declaring is naming a public tool (MC5)", nil
	}
	all, err := a.st.McpRows(ctx)
	if err != nil {
		return nil, "", err
	}
	// With(Declare) then Assemble: THE validator answers "does this
	// qualified name exist, and is it visible to this agent"
	// (validateDeclares — unknown tool, another agent's private tool: the
	// validator's own sentences, verbatim. MC2 has one implementation, not
	// a second here).
	if refusal, werr := a.candidateValidated(all, mcp.Declare{AgentKey: p.Agent, Qualified: p.Qualified}); werr != nil {
		return nil, "", werr
	} else if refusal != "" {
		return nil, refusal, nil
	}
	cur := a.currentDeclares(all, slug)
	already := false
	for _, d := range cur {
		if d == p.Qualified {
			already = true
		}
	}
	qual, agent := p.Qualified, p.Agent
	spec := writeSpec{
		affected: []string{agent},
		land: func(ctx context.Context, tx *sql.Tx) (bool, error) {
			if already {
				return false, nil
			}
			return store.McpDeclareTx(ctx, tx, slug, qual)
		},
		audits: func(bool) []store.McpChange {
			return []store.McpChange{{
				Op: "grant_declare", Server: ptrStr(serverPart(qual)), AgentSlug: ptrStr(slug), Qualified: ptrStr(qual),
				Summary: fmt.Sprintf("declared %s for %s", qual, agent),
			}}
		},
	}
	res, refusal, werr := a.runWrite(ctx, kind, id, all, spec)
	if werr != nil || refusal != "" {
		return res, refusal, werr
	}
	// MG7's honest word: declaring is NOT authoring — the tool stays
	// unavailable until a policy allow exists. The after-view carries the
	// fact; the warning says it in words beside the result.
	if out, ok := res.(protocol.MCPWriteResult); ok && out.Changed && hasUnavailable(out.Grants, qual) {
		out.Warnings = append(out.Warnings, "the declaration is not a permission — the tool stays unavailable until a policy allow exists (MC7/MC10)")
		return out, "", nil
	}
	return res, "", nil
}

func hasUnavailable(gs []protocol.MCPAgentGrants, qual string) bool {
	for _, g := range gs {
		for _, v := range g.After {
			if v.Qualified == qual && v.State == "unavailable" {
				return true
			}
		}
	}
	return false
}

func (a *mcpAdmin) writeGrantRevoke(ctx context.Context, p protocol.MCPGrantRevokeParams, kind, id string) (any, string, error) {
	slug, refusal, err := a.resolveAgent(ctx, p.Agent)
	if err != nil || refusal != "" {
		return nil, refusal, err
	}
	if strings.TrimSpace(p.Qualified) == "" {
		return nil, "refused: mcp.grant.revoke needs `qualified` as SERVER.TOOL", nil
	}
	all, err := a.st.McpRows(ctx)
	if err != nil {
		return nil, "", err
	}
	cur := a.currentDeclares(all, slug)
	wasDeclared := false
	for _, d := range cur {
		if d == p.Qualified {
			wasDeclared = true
		}
	}
	// Revoking what was never declared is a no-op with the honest answer,
	// never an error (MG5's result shape): changed:false, no audit row.
	qual, agent := p.Qualified, p.Agent
	spec := writeSpec{
		affected: []string{agent},
		land: func(ctx context.Context, tx *sql.Tx) (bool, error) {
			if !wasDeclared {
				return false, nil
			}
			return store.McpRevokeTx(ctx, tx, slug, qual)
		},
		audits: func(bool) []store.McpChange {
			return []store.McpChange{{
				Op: "grant_revoke", Server: ptrStr(serverPart(qual)), AgentSlug: ptrStr(slug), Qualified: ptrStr(qual),
				Summary: fmt.Sprintf("revoked %s from %s", qual, agent),
			}}
		},
	}
	return a.runWrite(ctx, kind, id, all, spec)
}

// --- mcp.core.set -----------------------------------------------------------------------------

func (a *mcpAdmin) writeCoreSet(ctx context.Context, p protocol.MCPCoreSetParams, kind, id string) (any, string, error) {
	slug, refusal, err := a.resolveAgent(ctx, p.Agent)
	if err != nil || refusal != "" {
		return nil, refusal, err
	}
	all, err := a.st.McpRows(ctx)
	if err != nil {
		return nil, "", err
	}
	if refusal, werr := a.candidateValidated(all, mcp.SetCore{AgentKey: p.Agent, Core: p.Core}); werr != nil {
		return nil, "", werr
	} else if refusal != "" {
		return nil, refusal, nil
	}
	// The two empties, kept distinct on the way IN as they are kept on the
	// way out: Core nil = the WHOLE core set, pointer-to-empty = none.
	var curCore *string
	for _, ag := range all.Agents {
		if ag.AgentSlug == slug {
			curCore = ag.Core
		}
	}
	var newCore *string
	if p.Core != nil {
		b, jerr := json.Marshal(*p.Core)
		if jerr != nil {
			return nil, "", fmt.Errorf("mcp: encode core: %w", jerr)
		}
		s := string(b)
		newCore = &s
	}
	same := (curCore == nil && newCore == nil) ||
		(curCore != nil && newCore != nil && *curCore == *newCore)
	coreJSON := newCore
	agent := p.Agent
	summary := fmt.Sprintf("core of %s narrowed to %s", agent, strings.Join(derefCore(p.Core), " "))
	if p.Core == nil {
		summary = fmt.Sprintf("core of %s restored to the whole core set", agent)
	}
	spec := writeSpec{
		affected: []string{agent},
		land: func(ctx context.Context, tx *sql.Tx) (bool, error) {
			if same {
				return false, nil
			}
			return true, store.McpCoreSetTx(ctx, tx, slug, coreJSON)
		},
		audits: func(bool) []store.McpChange {
			return []store.McpChange{{
				Op: "core_set", AgentSlug: ptrStr(slug), Summary: summary,
			}}
		},
	}
	return a.runWrite(ctx, kind, id, all, spec)
}

func derefCore(cs *[]string) []string {
	if cs == nil {
		return nil
	}
	return *cs
}
