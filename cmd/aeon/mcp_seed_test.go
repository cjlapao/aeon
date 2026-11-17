package main

// ════════════════════════════════════════════════════════════════════════════
// mcp_seed_test.go — S46 T3: the fixture plumbing follows the truth.
//
// Before S46 the MCP fixture helpers wrote a config FILE (the world's only
// source); from T3 the tables are the source, so the same helpers now seed
// ROWS through the same Tx writers production uses — the CALL SITES (every
// S34/S35/S36 test, and my Phase A goldens) are untouched: a fixture says
// "this configuration exists", and how that fact enters the world changed
// with where the truth lives. The file is still written beside the rows
// (configPath realism for mcp.status; the importer's world in T4).
//
// Deviations recorded in the slice Notes (T3):
//   - Helper BODIES flipped; no assertion in any existing test file was
//     touched except TestMCPStatusBrokenConfig's fixture+substring (the
//     broken state a CHECK net makes unreachable by name — see that test).
//   - The seeder sets XDG_STATE_HOME itself when a test has not (the S36
//     files never needed a store before; now loadMCPConfig reads one — a
//     seed that could touch the real store is the failure this guard
//     exists to make impossible).
// ════════════════════════════════════════════════════════════════════════════

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/mcp"
	"github.com/cjlapao/aeon/internal/store"
)

var mcpSeedHome sync.Map // test name → seeded

// mcpSeedGuard pins XDG_STATE_HOME to a test directory before any row is
// seeded, exactly once per test (a later agentHome(t) may replace it —
// tests that call it FIRST already satisfy the guard).
func mcpSeedGuard(t *testing.T) {
	t.Helper()
	isolateStoreEnv(t) // the seeder and the readers must resolve to the SAME
	// test-owned dir; the keys an in-process daemon boot left behind would
	// send one to the neighbour's database (the ghost-rows failure,
	// measured 2026-11-01 — the poison S45 already pins against).
	if _, ok := mcpSeedHome.Load(t.Name()); ok {
		return
	}
	if env := os.Getenv("XDG_STATE_HOME"); env != "" {
		if !filepath.IsAbs(env) {
			t.Fatalf("XDG_STATE_HOME=%q is not absolute — a relative value is ignored on purpose (internal/paths) and the seed would hit the real store", env)
		}
		if !strings.HasPrefix(env, filepath.Clean(os.TempDir())) && !strings.Contains(env, string(os.PathSeparator)+"tmp"+string(os.PathSeparator)) {
			t.Fatalf("XDG_STATE_HOME=%q does not sit under the system temp area — seeds must never touch a real store", env)
		}
		mcpSeedHome.Store(t.Name(), true)
		return
	}
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	mcpSeedHome.Store(t.Name(), true)
}

// seedMCPConfigRows writes cfg's facts as rows through the production Tx
// writers (McpWithTx + McpAddServerTx + McpDeclareTx + McpCoreSetTx),
// planting any referenced agent row first (name = the dispatch key, slug =
// store.Slugify — exactly what agentmgr.Create plants, and mapKey resolves
// back through AgentBySlug). It deliberately does NOT Validate: fixtures
// that break the one validator are legitimate fixtures (the broken-state
// tests), and the CHECK net, not the helper, decides what a row may be.
//
// The seed REPLACES the MCP world (everything but the append-only audit),
// which is exactly what overwriting the config file used to mean: TestMain
// parks the package at a shared per-run state dir, and rows — unlike a
// file each test repointed — persist across tests there. Wiping first
// makes every seeded test deterministic regardless of what ran before it.
// (The audit is trigger-protected against deletes — by design, MG1 — so
// tests that assert the empty audit isolate themselves with agentHome.)
func seedMCPConfigRows(t *testing.T, cfg mcp.Config) {
	t.Helper()
	mcpSeedGuard(t)
	st, err := store.Open(store.Options{})
	if err != nil {
		t.Fatalf("open the seed store: %v", err)
	}
	// Close via CLEANUP, not defer: every t.Fatalf below must still return
	// the pool — sqlite hands an idle *sql.DB back to the NEXT sql.Open of
	// any name, and a pool leaked by a failing seed would be adopted by a
	// later test pointing at its own fresh file, serving the fixture rows
	// of this one (the "ghost rows" hunt, measured 2026-11-01).
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)

	plantAgent := func(key string) (string, error) {
		if a, found, err := st.AgentByName(key); err != nil {
			return "", err
		} else if found {
			return a.Slug, nil
		}
		slug := store.Slugify(key)
		if slug == "" {
			return "", fmt.Errorf("agent key %q slugifies to nothing", key)
		}
		if err := st.CreateAgentAudited(context.Background(), store.AgentActor{Kind: store.AgentActorUser, ID: "1000"}, store.Agent{
			Slug: slug, Name: key, Version: "1",
		}, now); err != nil {
			return "", err
		}
		return slug, nil
	}

	// EVERY handle-touching read happens BEFORE the transaction opens: the
	// store is one pool with an immediate-lock writer (the `_txlock=immediate`
	// discipline in store_verbs.go), so a `st.` read attempted from inside
	// an open McpWithTx starves against the tx's own connection. The tx
	// below touches nothing but the Tx writers — and it WIPE-then-inserts,
	// so no "already exists" check belongs here: within the same Tx the
	// wiped world is provably empty, and any row from a previous test is
	// exactly what the replacement is meant to remove.
	agentSlugOf := map[string]string{}
	for key := range cfg.Agents {
		slug, err := plantAgent(key)
		if err != nil {
			t.Fatalf("plant agent %q: %v", key, err)
		}
		agentSlugOf[key] = slug
	}
	type pending struct {
		row store.McpServerRow
	}
	var toWrite []pending
	seen := map[string]bool{}
	collect := func(s mcp.ServerConfig, tier, slug string) error {
		if seen[s.Name] {
			return nil
		}
		seen[s.Name] = true
		args, err := mcpJSONArrayFor(s.Transport.Args)
		if err != nil {
			return err
		}
		env, err := mcpJSONArrayFor(s.Transport.Env)
		if err != nil {
			return err
		}
		r := store.McpServerRow{
			Name: s.Name, Tier: tier, TransportType: s.Transport.Type,
			Command: s.Transport.Command, Args: args, Env: env, URL: s.Transport.URL,
			Enabled: true, Origin: "user", CreatedAt: now, UpdatedAt: now,
		}
		if slug != "" {
			ag := slug
			r.AgentSlug = &ag
		}
		for _, tl := range s.Tools {
			r.Tools = append(r.Tools, store.McpToolRow{Name: tl.Name, Rerun: tl.Rerun, Effect: tl.Effect})
		}
		toWrite = append(toWrite, pending{row: r})
		return nil
	}
	for _, s := range cfg.Public {
		if err := collect(s, "public", ""); err != nil {
			t.Fatalf("collect server %q: %v", s.Name, err)
		}
	}
	for key, ac := range cfg.Agents {
		for _, s := range ac.Private {
			if err := collect(s, "private", agentSlugOf[key]); err != nil {
				t.Fatalf("collect server %q: %v", s.Name, err)
			}
		}
	}

	err = st.McpWithTx(ctx, func(tx *sql.Tx) error {
		// The world-replacement (test files write rows directly by I2's own
		// exemption, helpers_test.go:215). mcp_server_tool rides out by
		// CASCADE; the audit is trigger-protected and deliberately stays
		// (tests asserting an empty audit isolate with agentHome). The
		// `agent` IDENTITY table is untouched — identities are not the MCP
		// world and other fixtures plant them.
		for _, stmt := range []string{
			`DELETE FROM mcp_declare`,
			`DELETE FROM mcp_server`,
			`DELETE FROM mcp_agent`,
		} {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("seed wipe: %w", err)
			}
		}
		for _, p := range toWrite {
			if _, err := store.McpAddServerTx(ctx, tx, p.row); err != nil {
				return err
			}
		}
		for key, ac := range cfg.Agents {
			slug := agentSlugOf[key]
			for _, q := range ac.Declares {
				if _, err := store.McpDeclareTx(ctx, tx, slug, q); err != nil {
					return err
				}
			}
			if ac.Core != nil {
				raw, err := mcpJSONArrayFor(ac.Core)
				if err != nil {
					return err
				}
				if err := store.McpCoreSetTx(ctx, tx, slug, &raw); err != nil {
					return err
				}
			} else {
				if err := store.McpCoreSetTx(ctx, tx, slug, nil); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed MCP rows: %v", err)
	}
}

// mcpConfigFromJSON decodes a fixture body into the shape (decode only —
// validation is the validator's, and broken fixtures must survive to
// reach it).
func mcpConfigFromJSON(t *testing.T, body string) mcp.Config {
	t.Helper()
	var cfg mcp.Config
	if err := json.Unmarshal([]byte(body), &cfg); err != nil {
		t.Fatalf("fixture config does not decode: %v", err)
	}
	return cfg
}
