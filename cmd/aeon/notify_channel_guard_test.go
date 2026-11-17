package main

// S58 T5 — the channel plane's guards, in the S45 T6 / S48 T8 method:
// sibling tests, NO existing guard edited, every negative predicate
// canaried by TestChannelGuardsHaveTeeth. The family's claim is the
// sharpest in the project — SV7's three locks: NO agent tool, the socket
// refuses non-person actors at the door, and the STORE itself refuses a
// non-'user' creator row. A channel is an exfiltration path by
// construction; the guards are why it can only ever be a person's choice.

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3" // the raw handle the CHECK drill needs (same CGO driver GORM opens)

	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/narrow"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/records"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/tools"
)

var channelWriteMethods = []protocol.MethodName{
	protocol.MethodNotifyChannelCreate, protocol.MethodNotifyChannelUpdate,
	protocol.MethodNotifyChannelEnable, protocol.MethodNotifyChannelDisable,
	protocol.MethodNotifyChannelArchive, protocol.MethodNotifyChannelTest,
	protocol.MethodNotifySubscriptionSet,
}

var channelReadMethods = []protocol.MethodName{
	protocol.MethodNotifyChannelList, protocol.MethodNotifyChannelShow,
	protocol.MethodNotifyDeliveryList,
}

// channelWriteNeedles: the dotted wire names and their Go constant
// spellings, banned from the four non-person packages (constants are how
// a smuggler reaches the table without spelling the dot).
var channelWriteNeedles = []string{
	`"notify.channel.create"`, `"notify.channel.update"`, `"notify.channel.enable"`,
	`"notify.channel.disable"`, `"notify.channel.archive"`, `"notify.channel.test"`,
	`"notify.subscription.set"`,
	"MethodNotifyChannelCreate", "MethodNotifyChannelUpdate", "MethodNotifyChannelEnable",
	"MethodNotifyChannelDisable", "MethodNotifyChannelArchive", "MethodNotifyChannelTest",
	"MethodNotifySubscriptionSet",
}

// channelWriteToolNameRe is the tool-name shape a channel write would
// wear if one ever entered the registry.
var channelWriteToolNameRe = regexp.MustCompile(`notify_(channel|subscription)_(create|update|enable|disable|archive|test|set)`)

// channelTwins: NO twin is legal, ever — not even a propose-only one.
// An agent proposing "send my secrets to this URL" is the attack the
// whole slice refuses; there is no benign draft of it.
var channelTwins = map[string]bool{}

func channelGuardDaemon(t *testing.T) *daemon.Daemon {
	t.Helper()
	st, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return daemon.New(daemon.Options{Store: st})
}

// ── guard 1: the human door, positive and negative ─────────────────────────

func TestChannelWritesAreAHumanDoor(t *testing.T) {
	d := channelGuardDaemon(t)
	for _, m := range channelWriteMethods {
		got, ok := d.Table.Lookup(m)
		if !ok {
			t.Fatalf("%s is not registered on a store-backed daemon — the person's door vanished", m)
		}
		if !got.RequiresInitialize {
			t.Errorf("%s does not require Initialize", m)
		}
		if got.Debug {
			t.Errorf("%s is Debug — the human-door exclusion must never rest on a flag", m)
		}
		if su, ok := protocol.SurfaceClassified(m); !ok || !su.HumanDoorOnly {
			t.Errorf("%s is not classified HumanDoorOnly in the surface table — SV7's person gate is the classification, not a handler's manners", m)
		}
	}
	for _, m := range channelReadMethods {
		_, ok := d.Table.Lookup(m)
		if !ok {
			t.Fatalf("read door %s vanished", m)
		}
		if su, ok := protocol.SurfaceClassified(m); ok && su.HumanDoorOnly {
			t.Errorf("%s is classified HumanDoorOnly — reads belong to every surface (the tray and web read the roster)", m)
		}
	}
	// V13: the unwired table leaves all ten ABSENT.
	bare := protocol.NewTable(protocol.NewSessions(), protocol.NewSubscriptions(), protocol.NewEventLog())
	if bare.RegisterNotifyChannels() {
		t.Fatal("RegisterNotifyChannels succeeded unwired — registered-and-refusing is V13's forbidden shape")
	}
	for _, m := range append(append([]protocol.MethodName{}, channelWriteMethods...), channelReadMethods...) {
		if _, ok := bare.Lookup(m); ok {
			t.Errorf("%s registered without the channel seam", m)
		}
		if _, err := bare.Dispatch(context.Background(), "c-ch-absent", m, nil); !errors.As(err, new(protocol.ErrUnknownMethod)) {
			t.Errorf("%s answered %v, want ErrUnknownMethod", m, err)
		}
	}
}

// ── guard 2: no agent tool, no catalog twin ────────────────────────────────

func TestAgentCannotCreateEditEnableOrTestAChannel(t *testing.T) {
	t.Run("ToolRegistry", func(t *testing.T) {
		reg := tools.Registry()
		if len(reg) == 0 {
			t.Fatal("tools.Registry() is empty — the guard would pass on nothing")
		}
		if bad := channelWriteToolsInRegistry(reg); len(bad) != 0 {
			t.Errorf("the tool registry carries %s — an agent may not author an exfiltration path (SV7)", strings.Join(bad, ", "))
		}
	})
	t.Run("RecordsCatalog", func(t *testing.T) {
		specs := records.Catalog()
		if len(specs) == 0 {
			t.Fatal("records.Catalog() is empty")
		}
		if bad := channelCatalogTwins(specs); len(bad) != 0 {
			t.Errorf("the records catalog carries %s — a channel write has NO legal twin, not even propose-only", strings.Join(bad, "\n"))
		}
	})
}

func channelWriteToolsInRegistry(reg map[string]tools.Tool) []string {
	var bad []string
	for name := range reg {
		if channelWriteToolNameRe.MatchString(name) {
			bad = append(bad, name)
		}
	}
	return bad
}

func channelCatalogTwins(specs []records.Spec) []string {
	var bad []string
	want := map[string]bool{}
	for _, m := range channelWriteMethods {
		want[string(m)] = true
	}
	for _, s := range specs {
		if want[s.Method] && !channelTwins[s.Name] {
			bad = append(bad, s.Name+" twin of "+s.Method)
		}
	}
	return bad
}

// ── guard 3: the writes never reach the non-person surfaces ────────────────

func TestChannelWritesNeverReachNarrowOrAgentServices(t *testing.T) {
	rootsFour := []string{
		filepath.Join("..", "..", "internal", "narrow"),
		filepath.Join("..", "..", "internal", "agentsvc"),
		filepath.Join("..", "..", "internal", "tools"),
		filepath.Join("..", "..", "internal", "records"),
	}
	actions := narrow.Actions()
	if len(actions) == 0 {
		t.Fatal("narrow.Actions() is empty — the projection has no table, so this guard would pass on nothing")
	}
	if bad := channelWritesOnNarrow(actions); len(bad) != 0 {
		t.Errorf("the narrow surface projects channel writes through %s", strings.Join(bad, ", "))
	}
	ms := protocol.AgentServiceMethods()
	if len(ms) == 0 {
		t.Fatal("protocol.AgentServiceMethods() is empty")
	}
	if bad := channelWritesOnAgentServices(ms); len(bad) != 0 {
		t.Errorf("the agent-services enumeration names %s", strings.Join(bad, ", "))
	}
	if bad := mcpLiteralGrep(rootsFour, channelWriteNeedles, nil); len(bad) != 0 {
		t.Errorf("the four non-person packages name the channel-write vocabulary: %s", strings.Join(bad, "\n"))
	} else if seen := walkedSources(rootsFour, nil); len(seen) == 0 {
		t.Fatal("the walk saw no non-test files — the grep checked nothing")
	}
}

func channelWritesOnNarrow(actions []narrow.Action) []string {
	var bad []string
	want := map[protocol.MethodName]bool{}
	for _, m := range channelWriteMethods {
		want[m] = true
	}
	for _, a := range actions {
		if want[a.SocketMethod] {
			bad = append(bad, a.Name+"→"+string(a.SocketMethod))
		}
	}
	return bad
}

func channelWritesOnAgentServices(ms []protocol.AgentServiceMethod) []string {
	var bad []string
	want := map[protocol.MethodName]bool{}
	for _, m := range channelWriteMethods {
		want[m] = true
	}
	for _, m := range ms {
		if want[m.Name] {
			bad = append(bad, string(m.Name))
		}
	}
	return bad
}

// ── guard 4: the STORE itself refuses a non-user creator (lock 3) ──────────

func TestChannelCheckRefusesNonUserCreator(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(store.Options{Dir: dir, Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dir, "state.db")
	st.Close()
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, kind := range []string{"agent", "cli", "system", "loop"} {
		_, err := db.Exec(`INSERT INTO notify_channel
			(id, kind, name, enabled, url, secret_ref, allow_private,
			 created_by_kind, created_by_id, created_at, updated_at)
			VALUES ('nc-guard-test', 'webhook', 'x', 0, 'https://x.example.com',
			        'secret:notify/nc-guard-test', 0, ?, 'smuggler',
			        '2026-11-03T09:00:00.000Z', '2026-11-03T09:00:00.000Z')`, kind)
		if err == nil || !strings.Contains(err.Error(), "CHECK") {
			t.Errorf("a channel created_by_kind=%q: err=%v — SV8 lock 3 is a CONSTRAINT, not a handler's manners", kind, err)
		}
	}
	// The positive: 'user' inserts, so the refusal above is about the KIND.
	if _, err := db.Exec(`INSERT INTO notify_channel
		(id, kind, name, enabled, url, secret_ref, allow_private,
		 created_by_kind, created_by_id, created_at, updated_at)
		VALUES ('nc-guard-user', 'webhook', 'ok', 0, 'https://x.example.com',
		        'secret:notify/nc-guard-user', 0, 'user', 'carlos',
		        '2026-11-03T09:00:00.000Z', '2026-11-03T09:00:00.000Z')`); err != nil {
		t.Errorf("the legal creator was refused: %v", err)
	}
}

// ── guard 5: a loop/key surface cannot even see the doors ──────────────────

func TestLoopCannotCreateAChannel(t *testing.T) {
	// The narrow (loop-key) projection and the agent-service enumeration
	// are what a loop and an agent can reach; neither may name a channel
	// write — AND the boundary statement must not DISPLAY one either.
	if bad := channelWritesOnNarrow(narrow.Actions()); len(bad) != 0 {
		t.Fatalf("the loop-key surface reaches %s", strings.Join(bad, ", "))
	}
	stmt := narrow.BoundaryStatement()
	if stmt == "" {
		t.Fatal("no boundary statement to check")
	}
	for _, needle := range []string{"notify.channel.create", "notify.channel.enable", "notify.subscription.set"} {
		if strings.Contains(stmt, needle) {
			t.Errorf("the projection's boundary statement displays %s — the surface would advertise a door it must not hold", needle)
		}
	}
}

// ── guard 6: the teeth ─────────────────────────────────────────────────────

func TestChannelGuardsHaveTeeth(t *testing.T) {
	t.Run("NarrowCanary", func(t *testing.T) {
		bad := channelWritesOnNarrow([]narrow.Action{{Name: "aeon_notify_channel_create", SocketMethod: protocol.MethodNotifyChannelCreate}})
		if len(bad) != 1 {
			t.Error("the narrow predicate missed a planted channel write")
		}
	})
	t.Run("AgentServicesCanary", func(t *testing.T) {
		bad := channelWritesOnAgentServices([]protocol.AgentServiceMethod{{Name: protocol.MethodNotifyChannelEnable}})
		if len(bad) != 1 {
			t.Error("the agent-services predicate missed a planted write")
		}
	})
	t.Run("ToolCanary", func(t *testing.T) {
		reg := map[string]tools.Tool{}
		for _, n := range []string{"notify_channel_create", "notify_subscription_set"} {
			reg[n] = tools.Tool{}
		}
		if bad := channelWriteToolsInRegistry(reg); len(bad) != 2 {
			t.Errorf("the tool predicate found %v of 2 planted names", bad)
		}
	})
	t.Run("CatalogCanary", func(t *testing.T) {
		bad := channelCatalogTwins([]records.Spec{{Name: "notify_channel_create", Method: string(protocol.MethodNotifyChannelCreate)}})
		if len(bad) != 1 {
			t.Error("the catalog predicate missed a planted twin (there is NO legal twin, propose-only included)")
		}
	})
}
