package daemon

// The surface completeness proofs (S47 T6). The protocol package can prove the
// table is internally honest; only the daemon can prove it is COMPLETE, because
// only here does a fully wired table exist — the conditional families (skills,
// settings, retrieval, search) register off seams an entry point supplies, and a
// surface that silently forgot one of them would be a screen with a missing row
// and no failure anywhere.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/config"
	"github.com/cjlapao/aeon/internal/narrow"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/search"
	"github.com/cjlapao/aeon/internal/seed"
	"github.com/cjlapao/aeon/internal/skills"
	"github.com/cjlapao/aeon/internal/store"
)

// surfaceConditionalNames is the closed set of classified names this fixture may
// legitimately not register. It is EMPTY, measured: every one of the 276
// classifications lands on a method this fully wired daemon actually registers,
// (269 at S47 T6 + S47 T7's two own-view reads + S62 T6's five backup doors,
// measured 2026-11-02 on the merged tree),
// which is the strongest form both directions of the completeness check can take.
//
// A name joining it is a decision and not a fix — it says "this family registers
// only off a seam" (Options.Skills, Options.Settings, Options.CorpusStatus,
// Options.Reindex, Options.Search, and the entry point's MCP and agent seams are
// what made it empty), and the test that demands it is the review.
var surfaceConditionalNames = map[protocol.MethodName]string{}

func surfaceDaemonTable(t *testing.T) *protocol.Table {
	t.Helper()
	st, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("open the store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	engine, err := NewSeedEngine(st, seed.Options{})
	if err != nil {
		t.Fatalf("build the seed engine: %v", err)
	}
	ApplyInertAtStart(context.Background(), engine)
	skillSvc, err := skills.NewService(skills.Options{Getenv: func(string) string { return "" }, Clock: time.Now})
	if err != nil {
		t.Fatalf("build the skills service: %v", err)
	}
	echo := func(_ context.Context, _ string, _ json.RawMessage) (any, string, error) {
		return map[string]any{}, "", nil
	}
	echoParams := func(_ context.Context, _ json.RawMessage) (any, string, error) { return map[string]any{}, "", nil }
	d := New(Options{
		Store:           st,
		Seed:            engine,
		Settings:        NewSettings(st, config.Options{Registry: testRegistry(), LookupEnv: func(string) (string, bool) { return "", false }, Now: func() time.Time { return fixedNow() }}),
		Skills:          skillSvc,
		MCP:             echo,
		MCPAdmin:        echo,
		Agents:          echo,
		AgentManage:     echo,
		AgentTransition: echo,
		AgentRoster:     echoParams,
		ToolGrants:      echoParams,
		Plugins:         echoParams,
		AgentImportPreview: func(_ context.Context, _ string) (*protocol.AgentImportPreview, string, error) {
			return &protocol.AgentImportPreview{}, "", nil
		},
		ValidateProposal: func(_ context.Context, _, _ string) error { return nil },
		CorpusStatus:     func(_ context.Context, _ string) (search.StatusAnswer, error) { return search.StatusAnswer{}, nil },
		Reindex: func(_ context.Context, _, _, _ string, _ bool, _ func(done, total int)) (search.ReindexOutcome, error) {
			return search.ReindexOutcome{}, nil
		},
		Search: func(_ context.Context, _, _, _ string, _ int) (search.SearchAnswer, error) {
			return search.SearchAnswer{}, nil
		},
	})
	// The narrow seam, supplied the way cmd/aeon/serve.go supplies it — from
	// narrow.Actions() itself, so this test compares the door with the projection
	// rather than with a remembered copy of it.
	d.Table.NarrowMethods = func() map[protocol.MethodName]string {
		out := make(map[protocol.MethodName]string, 9)
		for _, a := range narrow.Actions() {
			out[a.SocketMethod] = a.Name
		}
		return out
	}
	return d.Table
}

// TestSurfaceEveryRegisteredMethodIsClassified is the direction that catches the
// slice's real risk: a method added by the next slice (or by a merge that brought
// another agent's family in) with no classification, which would reach a person's
// screen as a row with no kind and no sentence.
func TestSurfaceEveryRegisteredMethodIsClassified(t *testing.T) {
	tbl := surfaceDaemonTable(t)
	registered := tbl.Methods()
	if len(registered) < 250 {
		t.Fatalf("this table registers %d methods — too few to be the daemon's surface; the fixture lost a seam", len(registered))
	}
	missing := 0
	for _, n := range registered {
		s, ok := protocol.SurfaceClassified(n)
		if !ok {
			missing++
			t.Errorf("%s is registered on a real daemon and the surface table does not classify it — a person would see a row with no kind and no sentence", n)
			continue
		}
		if !protocol.ValidSurfaceKind(s.Kind) {
			t.Errorf("%s is classified %q, which is not one of the four kinds", n, s.Kind)
		}
		if strings.TrimSpace(s.Summary) == "" {
			t.Errorf("%s is classified with no summary", n)
		}
	}
	if missing == 0 {
		t.Logf("every one of the %d registered methods is classified", len(registered))
	}
	// And the answer is complete in the listing too: no row with an empty kind.
	for _, e := range tbl.Surface() {
		if e.Kind == "" {
			t.Errorf("surface lists %s with the zero kind — it is registered and unclassified", e.Method)
		}
	}
}

// TestSurfaceEveryEntryNamesARegisteredMethod is the other direction: a stale row
// is a row a screen would render for a method that no longer exists, which is how
// a catalog becomes folklore. Anything outside the closed conditional set is a
// method that was renamed or deleted and left its classification behind.
func TestSurfaceEveryEntryNamesARegisteredMethod(t *testing.T) {
	tbl := surfaceDaemonTable(t)
	registered := map[protocol.MethodName]bool{}
	for _, n := range tbl.Methods() {
		registered[n] = true
	}
	for _, n := range protocol.SurfaceClassifiedNames() {
		if registered[n] {
			continue
		}
		if seam, ok := surfaceConditionalNames[n]; ok {
			t.Logf("%s is classified and not registered here — its seam (%s) is unwired in this fixture, which is absence and not a gap", n, seam)
			continue
		}
		t.Errorf("%s is classified in the surface table and no daemon registers it — the row is stale, and a stale row is how a catalog becomes folklore", n)
	}
}

// TestSurfaceNarrowDoorEqualsTheRealProjection is the differential the protocol
// side cannot run: it compares every entry's narrow door with narrow.Actions()
// itself, not with a literal that could agree with a bug.
func TestSurfaceNarrowDoorEqualsTheRealProjection(t *testing.T) {
	tbl := surfaceDaemonTable(t)
	actions := narrow.Actions()
	if len(actions) != 9 {
		t.Fatalf("the narrow projection holds %d actions, the surface was built for 9 — the projection widened and the door computation was not told", len(actions))
	}
	bySocket := map[protocol.MethodName]string{}
	for _, a := range actions {
		if _, ok := tbl.Lookup(a.SocketMethod); !ok {
			t.Errorf("%s projects %s, which this daemon does not register — the projection would 404 its own action", a.Name, a.SocketMethod)
		}
		bySocket[a.SocketMethod] = a.Name
	}
	seen := map[string]int{}
	for _, e := range tbl.Surface() {
		want, projected := bySocket[e.Method]
		if !projected {
			if e.Doors.Narrow != "none" {
				t.Errorf("%s reports narrow %q and the projection does not name it", e.Method, e.Doors.Narrow)
			}
			continue
		}
		if e.Doors.Narrow != want {
			t.Errorf("%s reports narrow %q, narrow.Actions() says %q", e.Method, e.Doors.Narrow, want)
		}
		seen[want]++
		// A projected action is a machine's door onto the method, and a machine's
		// door cannot be a human-only door: the two claims would contradict.
		if e.HumanDoorOnly {
			t.Errorf("%s is HumanDoorOnly and the narrow surface projects it as %s", e.Method, want)
		}
		// And a trigger, webhook or callback is acting on a running thing, which is
		// what `control` means.
		switch a := actionKindFor(actions, want); a {
		case "trigger", "webhook", "callback":
			if e.Kind != protocol.SurfaceControl {
				t.Errorf("%s is projected as a %s action and the table calls it %q", e.Method, a, e.Kind)
			}
		}
	}
	for name, n := range seen {
		if n != 1 {
			t.Errorf("the action %s appears in %d entries", name, n)
		}
	}
	if len(seen) != len(bySocket) {
		t.Errorf("the projection names %d socket methods and the listing carries %d", len(bySocket), len(seen))
	}
}

// TestSurfaceListingIsStableAndFilterable reads the fully wired table through the
// two doors, which is the way a screen reads it: the totals have to add up, and a
// second read has to say the same thing (N8: no cross-call cache to drift).
func TestSurfaceListingIsStableAndFilterable(t *testing.T) {
	tbl := surfaceDaemonTable(t)
	first := tbl.Surface()
	second := tbl.Surface()
	if len(first) != len(second) {
		t.Fatalf("two reads answered %d and %d rows", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("row %d differs between two reads: %+v vs %+v", i, first[i], second[i])
		}
	}
	if len(first) != len(tbl.Methods()) {
		t.Errorf("the listing holds %d rows and the table registers %d — one table, one listing", len(first), len(tbl.Methods()))
	}
	// Every kind is used: a kind nobody classifies is a kind the vocabulary does
	// not need, and a screen would render a bucket that is always empty.
	used := map[protocol.SurfaceKind]int{}
	for _, e := range first {
		used[e.Kind]++
	}
	for _, k := range protocol.AllSurfaceKinds() {
		if used[k] == 0 {
			t.Errorf("no method is classified %q — the closed set has become three", k)
		}
	}
	// And the two doors answer over the wired table, including the narrow filter,
	// which only a wired seam may answer.
	raw, err := json.Marshal(map[string]any{"door": "narrow"})
	if err != nil {
		t.Fatal(err)
	}
	init, err := json.Marshal(protocol.InitializeParams{
		ClientName: "surface", ClientVersion: "0", ProtocolVersion: protocol.PROTOCOL_VERSION,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ierr := tbl.Dispatch(context.Background(), "c-surface-daemon", protocol.MethodInitialize, init); ierr != nil {
		t.Fatalf("initialize: %v", ierr)
	}
	// The doors are RequiresInitialize, and the transport says so before the
	// handler is ever reached.
	if _, err := tbl.Dispatch(context.Background(), "c-surface-uninitialized", protocol.MethodSurfaceList, raw); err == nil {
		t.Error("surface.list answered a connection that never initialized")
	}
	out, err := tbl.Dispatch(context.Background(), "c-surface-daemon", protocol.MethodSurfaceList, raw)
	if err != nil {
		t.Fatalf("surface.list on the wired table: %v", err)
	}
	res, ok := out.(protocol.SurfaceListResult)
	if !ok {
		t.Fatalf("the answer is %T", out)
	}
	if res.Counts.Total != 9 {
		t.Errorf("the narrow door filter answered %d rows, the projection holds 9 actions", res.Counts.Total)
	}
	if res.Counts.Unfiltered != len(tbl.Methods()) {
		t.Errorf("counts.unfiltered says %d and the table registers %d", res.Counts.Unfiltered, len(tbl.Methods()))
	}
}

// actionKindFor is the projection's own word for one action, taken from the live
// set rather than remembered here.
func actionKindFor(actions []narrow.Action, name string) string {
	for _, a := range actions {
		if a.Name == name {
			return string(a.Kind)
		}
	}
	return ""
}
