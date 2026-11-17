package main

// ════════════════════════════════════════════════════════════════════════════
// mcp_admin.go — S46 T3: the ONE config source after the migration.
//
// # What this file is
//
// `mcpAdmin` is the single door every MCP reader in this package walks
// through: store rows → `internal/mcp`'s row shapes → `mcp.Assemble` → the
// `Config` the unchanged S12 registry consumes. The JSON file is no longer
// a source (MG1): `loadMCPConfig` (the CLI verbs and the step offer) and
// `loadCurrentMCPConfig` (the proposal validators and effect inputs) keep
// their exact signatures — call sites unchanged — and now read rows.
// `internal/mcp` stays store-free (MC11; T2's `Rows`/`Assemble` are pure),
// and this package stays its ONLY importer (absent_test.go still green).
//
// # The three rules this file enforces by construction
//
//   - Level 0 is a state, not an error: empty tables answer
//     `mcp.ErrNoConfig` (the same sentinel the file path used), so
//     `mcpAbsentLines` prints unchanged (MG11's byte promise).
//   - No cache, ever (MG12): every call re-reads the tables in one
//     consistent read (`store.McpRows`, one read-only transaction) and
//     rebuilds; `mcp.New` runs per offer / per call exactly as it did per
//     file-read. A write by any door is visible to the NEXT read — no
//     restart, no bump counter. The tables are small (a dozen servers is a
//     LOT); re-reading is the honest default, not a performance accident.
//   - Env is NAMES, never values (C17/MG13): rows carry the same
//     name-only arrays the file did; `mcp.Assemble` runs the one validator
//     over them (a value-shaped row is refused with the file's own words).
//
// # The landmine (T2 Notes, honored here)
//
// Registry doors read the INCLUDE-DISABLED candidate and `mcp.New` it —
// `New` re-`Validate`s, and the law-2 filtered view is deliberately not
// New-able when a declaration names a disabled server's tool (T2 pinned
// both halves). Enabled-ness is applied at the SEAM (the offer skips
// disabled servers' rows; the registry's own answers stay the validator's),
// and MG4's "declarations stand" holds because the grant outlives the
// outage. `mcpAdmin.disabled` is that seam's input, read from the same
// rows — a second truth is not available here.
// ════════════════════════════════════════════════════════════════════════════

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/cjlapao/aeon/internal/mcp"
	"github.com/cjlapao/aeon/internal/paths"
	"github.com/cjlapao/aeon/internal/store"
)

// mcpAdmin is the rows-backed config source over one store handle. It
// caches NOTHING — st is a handle, not a result (MG12 forbids caching
// config or registry state; the next read sees the last write). mapKey
// translates the store's agent SLUG to the dispatch KEY (MG2: names are
// the key of record — internal/mcp must never see a slug), and now is the
// clock the write doors (T5) stamp audit rows with, injected so those
// doors can be tested at a fixed instant (D5's discipline).
type mcpAdmin struct {
	st     *store.Store
	mapKey func(slug string) (string, bool)
	now    func() time.Time
}

// newMCPAdmin binds an admin to a store handle. The slug→name resolution
// goes through the store's own read (AgentBySlug) every call — agent
// renames are visible to the next offer, and no stale name can outlive a
// row.
func newMCPAdmin(st *store.Store) *mcpAdmin {
	return &mcpAdmin{
		st: st,
		mapKey: func(slug string) (string, bool) {
			a, found, err := st.AgentBySlug(slug)
			if err != nil || !found {
				return "", false
			}
			return a.Name, true
		},
		now: time.Now,
	}
}

// withMCPStore opens the justified second store access (the same one
// `aeon policy` and loadToolAccessPolicies use — WAL serialises behind the
// daemon's writer) for one admin call, for doors that were not handed a
// handle (the CLI verbs, the step offer, the proposal seams). The handle
// is closed before returning; no admin ever outlives the call.
func withMCPStore(ctx context.Context, fn func(a *mcpAdmin) error) error {
	st, err := openStoreDirectly(false)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	return fn(newMCPAdmin(st))
}

// currentConfig is THE read: rows → mcp.Rows → Assemble. Empty tables are
// ErrNoConfig (level 0, MC11), never a zero Config that could be mistaken
// for a real one. includeDisabled:true is the validated candidate (the
// registry doors — see the file header's landmine); includeDisabled:false
// is the law-2 view for renderers that must omit disabled servers (MG4)
// and never hand it to mcp.New.
func (a *mcpAdmin) currentConfig(ctx context.Context, includeDisabled bool) (mcp.Config, error) {
	all, err := a.st.McpRows(ctx)
	if err != nil {
		return mcp.Config{}, fmt.Errorf("read the MCP tables: %w", err)
	}
	if len(all.Servers) == 0 && len(all.Agents) == 0 && len(all.Declares) == 0 {
		return mcp.Config{}, mcp.ErrNoConfig
	}
	rows, err := a.toRows(all)
	if err != nil {
		return mcp.Config{}, err
	}
	return mcp.Assemble(rows, mcp.AssembleOptions{IncludeDisabled: includeDisabled})
}

// config is the include-disabled candidate — what every registry door
// (mcp.New) and every legacy-shape reader (mcp.status, tools.grants, the
// CLI list) consumes, exactly as the file used to be handed them.
func (a *mcpAdmin) config(ctx context.Context) (mcp.Config, error) {
	return a.currentConfig(ctx, true)
}

// disabled answers which configured server names are currently switched
// off — the offer/list seam's filter input (MG4). It is read from the same
// rows as currentConfig; servers absent from the answer are enabled.
func (a *mcpAdmin) disabled(ctx context.Context) (map[string]bool, error) {
	all, err := a.st.McpRows(ctx)
	if err != nil {
		return nil, fmt.Errorf("read the MCP tables: %w", err)
	}
	off := map[string]bool{}
	for _, r := range all.Servers {
		if !r.Enabled {
			off[r.Name] = true
		}
	}
	return off, nil
}

// serverRows exposes the raw table rows for the management reads (T3's
// mcp.servers/mcp.server ServerViews carry origin/enabled/updatedAt — row
// facts that `ServerConfig` itself does not model; the config stays the
// validator's, the row keeps the rest).
func (a *mcpAdmin) serverRows(ctx context.Context) ([]store.McpServerRow, []store.McpAgentRow, error) {
	all, err := a.st.McpRows(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("read the MCP tables: %w", err)
	}
	return all.Servers, all.Agents, nil
}

// toRows maps the store's shapes onto internal/mcp's pure row shapes —
// the ONLY place slugs translate to dispatch keys (MG2) and JSON arrays
// decode. A malformed array is a store bug the CHECK net already forbids
// (T1); the refusal names the column rather than pretending the entry was
// never there.
func (a *mcpAdmin) toRows(all store.McpAll) (mcp.Rows, error) {
	r := mcp.Rows{}
	for _, s := range all.Servers {
		args, err := mcpJSONArray(s.Name, "args", s.Args)
		if err != nil {
			return mcp.Rows{}, err
		}
		env, err := mcpJSONArray(s.Name, "env", s.Env)
		if err != nil {
			return mcp.Rows{}, err
		}
		envFrom := map[string]string{}
		_ = json.Unmarshal([]byte(s.EnvFrom), &envFrom)
		tier := mcp.TierPublic
		agentKey := ""
		if s.Tier == "private" {
			tier = mcp.TierPrivate
			if s.AgentSlug == nil {
				return mcp.Rows{}, fmt.Errorf("mcp: server %q is stored private with no agent — the CHECK net forbids this row", s.Name)
			}
			key, found := a.mapKey(*s.AgentSlug)
			if !found {
				return mcp.Rows{}, fmt.Errorf("mcp: server %q belongs to agent slug %q, which no longer resolves to an agent", s.Name, *s.AgentSlug)
			}
			agentKey = key
		}
		tools := make([]mcp.ToolConfig, 0, len(s.Tools))
		for _, t := range s.Tools {
			tools = append(tools, mcp.ToolConfig{Name: t.Name, Rerun: t.Rerun, Effect: t.Effect})
		}
		r.Servers = append(r.Servers, mcp.ServerRow{
			Name: s.Name, Tier: tier, AgentKey: agentKey, Enabled: s.Enabled,
			Transport: mcp.TransportConfig{
				Type:    s.TransportType,
				Command: s.Command, Args: args, Env: env, EnvFrom: nilIfEmpty(envFrom), URL: s.URL,
			},
			Tools: tools,
		})
	}

	// Agents: every mcp_agent row, plus any slug seen only in a declaration
	// (a declaring agent with no core row is the ordinary case — the row
	// rides out; Validate knows agents by key, not by provenance).
	byKey := map[string]*mcp.AgentRow{}
	order := []string{}
	touch := func(key string) *mcp.AgentRow {
		if row, ok := byKey[key]; ok {
			return row
		}
		row := &mcp.AgentRow{AgentKey: key}
		byKey[key] = row
		order = append(order, key)
		return row
	}
	for _, ar := range all.Agents {
		key, found := a.mapKey(ar.AgentSlug)
		if !found {
			return mcp.Rows{}, fmt.Errorf("mcp: mcp_agent row for slug %q no longer resolves to an agent", ar.AgentSlug)
		}
		row := touch(key)
		if ar.Core != nil {
			core, err := mcpJSONArray("agent "+key, "core", *ar.Core)
			if err != nil {
				return mcp.Rows{}, err
			}
			// The two empties ride: a stored "[]" is NONE (non-nil empty),
			// the absent column is the WHOLE set (nil) — MC's law, T2's
			// rows_test pin.
			row.Core = &core
		}
	}
	for _, d := range all.Declares {
		key, found := a.mapKey(d.AgentSlug)
		if !found {
			return mcp.Rows{}, fmt.Errorf("mcp: declaration %q names agent slug %q, which no longer resolves to an agent", d.Qualified, d.AgentSlug)
		}
		row := touch(key)
		row.Declares = append(row.Declares, d.Qualified)
	}
	// A shared public server is declared by every agent: its classified tools,
	// now and for agents created later. The declaration grants nothing.
	if len(all.Shared) > 0 {
		agents, err := a.st.AgentsAll()
		if err != nil {
			return mcp.Rows{}, err
		}
		for _, name := range all.Shared {
			for _, s := range all.Servers {
				if s.Name != name || s.Tier != "public" {
					continue
				}
				for _, ag := range agents {
					key, found := a.mapKey(ag.Slug)
					if !found {
						continue
					}
					row := touch(key)
					have := map[string]bool{}
					for _, d := range row.Declares {
						have[d] = true
					}
					for _, t := range s.Tools {
						if q := s.Name + "." + t.Name; t.Rerun != "" && t.Effect != "" && !have[q] {
							row.Declares = append(row.Declares, q)
						}
					}
				}
			}
		}
	}
	for _, key := range order {
		row := byKey[key]
		if len(row.Declares) > 1 {
			sort.Strings(row.Declares)
		}
		r.Agents = append(r.Agents, *row)
	}
	return r, nil
}

// mcpJSONArray decodes one stored JSON array ("" and "[]" both mean the
// empty slice — Assemble normalises nil-vs-empty; only Core's emptiness is
// a fact, and that rides on the POINTER, which the caller keeps).
func mcpJSONArray(owner, column, raw string) ([]string, error) {
	if raw == "" {
		return nil, nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("mcp: %s's stored %s is not a JSON array (%v) — a store CHECK violation, not a config mistake", owner, column, err)
	}
	return out, nil
}

// mcpLegacyPath names where a LEGACY config file would live (the T4
// importer's source, the doctor's fact, and mcp.status's `configPath` —
// the operator reading status is the operator deciding whether to import).
// It resolves a path; it never reads the file. From T3 the ONLY sanctioned
// references to that path are here, in T4's importer, and in the doctor
// (TestNoRuntimeReadOfTheConfigFile pins it; the ban that matters is on
// mcp.LoadConfigFile — resolving a path is not consulting it).
func mcpLegacyPath() string {
	path, err := paths.MCPConfigFile()
	if err != nil {
		return ""
	}
	return path
}

// --- the two shared read paths, flipped from file to rows ---------------------
//
// loadMCPConfig (the CLI's mcp.* verbs and the step offer — Q1's pinned
// path) and loadCurrentMCPConfig (the proposal validators and effect
// inputs) keep their EXACT signatures. Their bodies now walk through the
// tables; the sentinel discipline is unchanged (ErrNoConfig is the
// legitimate absence, a broken read is an error), so every caller's
// three-state rendering survives byte for byte. The file itself belongs to
// the importer (T4) and the doctor from here on — TestNoRuntimeReadOfTheCon
// figFile greps for exactly that.

func loadMCPConfig() (mcp.Config, error) {
	var cfg mcp.Config
	err := withMCPStore(context.Background(), func(a *mcpAdmin) error {
		var err error
		// The law-2 CANDIDATE (include-disabled): this exact value is what
		// every door hands to mcp.New, and New re-validates — the filtered
		// view (declares standing over a dropped server) is deliberately
		// NOT New-able (T2's TestAssembleOmitsDisabledForRegistryOnly pins
		// both halves). MG4's drop of disabled servers happens at the
		// offer/list seam AFTER partition (mcp_dispatch_dial.go), never by
		// handing New an un-validatable view.
		cfg, err = a.config(context.Background())
		return err
	})
	return cfg, err
}

// mcpQualifiedAddress is the step gate's address for an MCP tool name, so an
// address pattern (`mcp.<server>.*.*`) can decide a call the way it decides the
// grant view. It reads the configuration per ask; only MCP names reach it.
func mcpQualifiedAddress(qualified string) string {
	cfg, err := loadMCPConfig()
	if err != nil {
		return ""
	}
	return mcp.QualifiedAddress(cfg, qualified)
}

// loadMCPDisabled answers which configured server names are switched off,
// for the offer/list seams' MG4 filter (mcp_dispatch_dial.go, cmdMCPList).
// It is a fact read from the rows, never derived from the config.
func loadMCPDisabled() (map[string]bool, error) {
	var off map[string]bool
	err := withMCPStore(context.Background(), func(a *mcpAdmin) error {
		var err error
		off, err = a.disabled(context.Background())
		return err
	})
	if err != nil && !errors.Is(err, mcp.ErrNoConfig) {
		return nil, err
	}
	return off, nil
}

func loadCurrentMCPConfig() (mcp.Config, bool, error) {
	cfg, err := loadMCPConfig()
	if err != nil {
		if errors.Is(err, mcp.ErrNoConfig) {
			return mcp.Config{}, false, nil
		}
		return mcp.Config{}, false, err
	}
	return cfg, true, nil
}

// --- the transitional effect mirror: retired (S46 T6) ---------------------------------
//
// The three MCP proposal effects now land through T5's audited row doors
// (mcp_admin_writes.go) — the mirror (serverRowFromConfig,
// mirrorServerAddRows, mirrorServerRemoveRows and the SaveConfig file
// write) is deleted, not left as a second path. The tables are the
// configuration; the audit's actor is the committing person, never the
// proposer.

// mcpJSONArrayFor encodes a string slice as the stored JSON array (nil/empty
// both become the array `[]` — the CHECK net wants json_type='array').
func mcpJSONArrayFor(xs []string) (string, error) {
	if len(xs) == 0 {
		return "[]", nil
	}
	b, err := json.Marshal(xs)
	if err != nil {
		return "", fmt.Errorf("encode JSON array: %w", err)
	}
	return string(b), nil
}

// mcpJSONObjectFor renders an env binding as the column's JSON object.
func mcpJSONObjectFor(m map[string]string) (string, error) {
	if len(m) == 0 {
		return "{}", nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "", fmt.Errorf("encode JSON object: %w", err)
	}
	return string(b), nil
}

// nilIfEmpty keeps an unbound server's config byte-identical to what it was.
func nilIfEmpty(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	return m
}
