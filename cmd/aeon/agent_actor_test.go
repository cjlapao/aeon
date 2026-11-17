package main

// agent_actor_test.go — S73 T4: the roads, walked from the management seam down
// to the ledger row each one leaves.
//
// # What this file proves that internal/protocol's door tests cannot
//
// agent_management_test.go proves the protocol stamps the peer and refuses a
// nameless connection. It stops at the seam, because that is where the protocol
// stops. The question left over is the one AR2 actually makes: when the six
// writes do run, does the row in `agent_change` carry the person who pressed the
// button — the same string the peer resolved to, not a paraphrase, not a default,
// not `system`? That is only answerable with a real store under the closure,
// which is what this file wires: `agentManageHandleFor` over `openAgentStore`,
// and the ledger read back out of the table.
//
// # The two roads AR4 listed and the tree does not contain
//
// Design deviation 15 (recorded in T4's Notes): paths 7 and 8 named the CLI's
// `aeon agent import` and the desk's commit of kind `agent_import` as roads that
// end in `ImportAgentCardAudited`. Measured, they do not. Both end in
// `importAgentPackage`, which runs `plugin.ImportChecked` — the LIFECYCLE import
// that registers a `plugin_agent` row plus its `plugin_transition` audit and
// never touches the `agent` content table. `agent_change` is a content ledger, so
// the honest test for those two roads is that they mint NOTHING here while still
// writing their own plane: `TestTheAgentImportRoadsWriteTheLifecycleNotTheContent`
// asserts both halves. The gap AR4 thought it was closing — a CLI or a proposal
// writing an agent row anonymously — does not exist: the only production caller
// of all six content writers is `cmd/aeon/agent_management.go`, reached only over
// the socket, which is paths 1–6.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// agentLedgerRow is one `agent_change` row as the table holds it. Read raw, on
// purpose: `agent.changes` is T5's door, and a T4 test that waited for that door
// could not say what the table contains when the door is wrong.
type agentLedgerRow struct {
	ID        int64
	Op        string
	Slug      sql.NullString
	Summary   string
	ActorKind string
	ActorID   string
}

func countAgentLedger(t *testing.T, s *store.Store) int {
	t.Helper()
	var n int
	if err := s.GormDB().Raw(`SELECT COUNT(*) FROM agent_change`).Scan(&n).Error; err != nil {
		t.Fatalf("count the ledger: %v", err)
	}
	return n
}

func countAgentRows(t *testing.T, s *store.Store) int {
	t.Helper()
	var n int
	if err := s.GormDB().Raw(`SELECT COUNT(*) FROM agent`).Scan(&n).Error; err != nil {
		t.Fatalf("count the agent rows: %v", err)
	}
	return n
}

// agentLedgerTail returns the last n ledger rows, oldest first — exactly the rows
// a dispatch just added, given a count taken before it.
func agentLedgerTail(t *testing.T, s *store.Store, n int) []agentLedgerRow {
	t.Helper()
	if n <= 0 {
		return nil
	}
	var out []agentLedgerRow
	const q = `SELECT id, op, slug, summary, actor_kind, actor_id
	           FROM agent_change ORDER BY id DESC LIMIT ?`
	if err := s.GormDB().Raw(q, n).Scan(&out).Error; err != nil {
		t.Fatalf("read the ledger tail: %v", err)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// personCtx is what the protocol's write edge installs for a real connection: the
// peer credential, kind `user`. These tests stamp it by hand because they drive
// the seam and not a socket — the same channel, the same value, and the closure
// cannot tell the difference (which is the point of the ctx being the channel).
func personCtx() context.Context {
	return protocol.WithAgentActor(context.Background(), "user", "1000")
}

// agentRoad is one write door with the params that make it succeed and the op it
// owes the ledger.
type agentRoad struct {
	name    string
	params  string
	wantOp  string
	wantNil bool // the migrate row carries no slug — AR1's one NULL
	seed    func(t *testing.T, h doorHandle, ctx context.Context)
}

type doorHandle = func(ctx context.Context, method string, params json.RawMessage) (any, string, error)

// TestSixAgentRoadsStampThePersonTheyArrivedOn walks the six writes through the
// one seam onto a real migrated store and reads each ledger row back. The claim
// is small and total: the row names `user:1000`, because that is who pressed it.
func TestSixAgentRoadsStampThePersonTheyArrivedOn(t *testing.T) {
	agentHome(t)
	s := openAgentStore(t)
	h := agentManageHandleFor(s, &daemon.Options{})
	ctx := personCtx()

	// The import road needs the unsigned override (the fixture card carries no
	// signature); the helper is the one S44 T10's tests already use.
	t.Setenv("AEON_AGENTMANAGE_ALLOW_UNSIGNED_IMPORT", "1")
	cardPath := unsignedCardPath(t, "road-import", "Road Import")
	importParams, err := json.Marshal(map[string]string{"path": cardPath})
	if err != nil {
		t.Fatal(err)
	}

	roads := []agentRoad{
		{name: "agent.create", params: `{"name":"Road Create","description":"road test"}`, wantOp: "create"},
		{name: "agent.duplicate", params: `{"slug":"road-create","name":"Road Copy"}`, wantOp: "duplicate"},
		{name: "agent.edit", params: `{"slug":"road-copy","fields":{"description":"edited by the road test"}}`,
			wantOp: "edit"},
		{name: "agent.delete", params: `{"slug":"road-delete"}`, wantOp: "delete",
			seed: func(t *testing.T, h doorHandle, ctx context.Context) {
				t.Helper()
				callDoor(t, h, ctx, "agent.create", `{"name":"Road Delete","description":"doomed"}`)
			}},
		{name: "agent.import", params: string(importParams), wantOp: "import"},
		{name: "agent.migrate", params: `{}`, wantOp: "migrate", wantNil: true},
	}

	for _, r := range roads {
		if r.seed != nil {
			r.seed(t, h, ctx)
		}
		before := countAgentLedger(t, s)
		res, refusal, err := h(ctx, r.name, json.RawMessage(r.params))
		if err != nil || refusal != "" || res == nil {
			t.Fatalf("%s: res=%v refusal=%q err=%v", r.name, res, refusal, err)
		}
		added := agentLedgerTail(t, s, countAgentLedger(t, s)-before)
		if len(added) == 0 {
			t.Fatalf("%s minted no ledger row — AR1 says a change nobody recorded cannot exist", r.name)
		}
		last := added[len(added)-1]
		if last.Op != r.wantOp {
			t.Errorf("%s minted op %q, want %q (summary %q)", r.name, last.Op, r.wantOp, last.Summary)
		}
		if last.ActorKind != "user" || last.ActorID != "1000" {
			t.Errorf("%s minted %s:%s — the ledger carries the peer the request arrived on, byte for byte",
				r.name, last.ActorKind, last.ActorID)
		}
		switch {
		case r.wantNil && last.Slug.Valid:
			t.Errorf("%s minted a row naming slug %q — a migrate run names no single agent (AR1's one NULL)", r.name, last.Slug.String)
		case !r.wantNil && (!last.Slug.Valid || last.Slug.String == ""):
			t.Errorf("%s minted a row with no slug — every per-agent op must name its agent", r.name)
		}
		if strings.TrimSpace(last.Summary) == "" {
			t.Errorf("%s minted an empty summary; the formatters exist so no path invents its own sentence", r.name)
		}
	}
}

// TestAnUnstampedAgentRoadWritesNothing is the closure's own fail-closed half.
// The protocol's edge refuses upstream (internal/protocol's tests cover that
// road); this proves the closure does not NEED the edge to be safe — a caller
// that skips it gets ErrAgentNoActor on the error channel and writes nothing at
// all, which is AR3's "a road that bypassed the door cannot write" made true one
// layer down, where the writing actually happens.
func TestAnUnstampedAgentRoadWritesNothing(t *testing.T) {
	agentHome(t)
	s := openAgentStore(t)
	h := agentManageHandleFor(s, &daemon.Options{})

	agentsBefore := countAgentRows(t, s)
	ledgerBefore := countAgentLedger(t, s)

	for _, name := range []string{"agent.create", "agent.duplicate", "agent.edit", "agent.delete", "agent.import", "agent.migrate"} {
		_, refusal, err := h(context.Background(), name,
			json.RawMessage(`{"name":"Ghost","slug":"ghost","path":"/tmp/x.json"}`))
		var perr *protocol.Error
		if !errors.As(err, &perr) || perr.Code != protocol.ErrAgentNoActor.Code {
			t.Fatalf("%s without a stamp answered refusal=%q err=%v, want ErrAgentNoActor (%d)",
				name, refusal, err, protocol.ErrAgentNoActor.Code)
		}
	}

	if got := countAgentRows(t, s); got != agentsBefore {
		t.Fatalf("the unstamped roads wrote %d agent rows — a refusal must move nothing", got-agentsBefore)
	}
	if got := countAgentLedger(t, s); got != ledgerBefore {
		t.Fatalf("the unstamped roads minted %d ledger rows — the refusal and the row are one transaction", got-ledgerBefore)
	}
}

// TestTheAgentImportRoadsWriteTheLifecycleNotTheContent is design deviation 15's
// evidence. `aeon agent import` and a committed `agent_import` proposal are real
// roads that put a real agent on this machine, and neither of them writes the
// `agent` content table: they run `plugin.ImportChecked`, which registers the
// lifecycle row and its own audit. So `agent_change` — a CONTENT ledger — stays
// empty for both, and that emptiness is a fact about which table the road
// touches, not an audit gap. The test asserts BOTH halves, because "no row here"
// is only honest next to "and the row there".
func TestTheAgentImportRoadsWriteTheLifecycleNotTheContent(t *testing.T) {
	agentHome(t)
	s := openAgentStore(t)

	dir := writeAgentPackage(t, "road-cli-import", nil)
	if err := cmdAgentImport(io.Discard, []string{dir}); err != nil {
		t.Fatalf("the CLI direct door: %v", err)
	}
	dir2 := writeAgentPackage(t, "road-commit-import", nil)
	payload, err := json.Marshal(map[string]string{"pkgPath": dir2})
	if err != nil {
		t.Fatal(err)
	}
	if err := seamApplyAgentImport(context.Background(), io.Discard, payload); err != nil {
		t.Fatalf("the desk commit: %v", err)
	}

	// Half 1: both roads DID something, in their own plane.
	rows, err := s.PluginAgentsAll(true)
	if err != nil {
		t.Fatalf("the plugin ledger: %v", err)
	}
	seen := map[string]bool{}
	for _, r := range rows {
		seen[r.Name] = true
	}
	for _, want := range []string{"road-cli-import", "road-commit-import"} {
		if !seen[want] {
			t.Fatalf("%s never arrived in plugin_agent — the road is broken, which is a different finding", want)
		}
		if ts, terr := s.PluginAgentTransitions(want); terr != nil || len(ts) == 0 {
			t.Errorf("%s has no plugin_transition rows — the lifecycle plane audits this road, and found none (err %v)", want, terr)
		}
	}

	// Half 2: and neither of them is in the content ledger, because neither ever
	// wrote a content row. `agent.migrate` is the road that turns a lifecycle
	// arrival into a content row, and it is path 6 — a person, on the socket.
	if n := countAgentLedger(t, s); n != 0 {
		t.Fatalf("the two lifecycle roads minted %d agent_change rows — AR1's ledger pairs with content writes", n)
	}
	if n := countAgentRows(t, s); n != 0 {
		t.Fatalf("the two lifecycle roads wrote %d agent content rows — measured, they write none", n)
	}
}

// callDoor is the helper the seed steps need: one door call that must succeed,
// with the failure spelled out.
func callDoor(t *testing.T, h doorHandle, ctx context.Context, method, params string) {
	t.Helper()
	res, refusal, err := h(ctx, method, json.RawMessage(params))
	if err != nil || refusal != "" || res == nil {
		t.Fatalf("%s(%s): res=%v refusal=%q err=%v", method, params, res, refusal, err)
	}
}
