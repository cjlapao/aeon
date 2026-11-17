package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/cjlapao/aeon/internal/notify"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// notify_channels_test.go — S58 T5 at the wiring layer: the ten doors
// over the real table and the real store, the person gate driven by the
// socket's own peer stamp (asPerson), and the ONE publish point proven to
// feed both sockets and channels from the same resolution.

// channelDaemon: the class daemon (store + seeded classes + a wire) plus
// a secrets state dir for the test's duration.
func channelDaemon(t *testing.T) (*Daemon, *store.Store, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("AEON_STATE_DIR", home)
	d, st, _ := newNotifyClassDaemon(t)
	if d.channels == nil {
		t.Fatal("a store-backed daemon built no channel seam (V13 wiring is dead)")
	}
	return d, st, home
}

func writeSecretFile(t *testing.T, home, name, value string) string {
	t.Helper()
	p := home + "/" + name
	if err := os.WriteFile(p, []byte(value+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func channelDispatch(t *testing.T, d *Daemon, method, params string) (any, error) {
	t.Helper()
	return d.Table.Dispatch(context.Background(), "c1", protocol.MethodName(method), json.RawMessage(params))
}

func TestChannelDoorsAbsentWithoutAStore(t *testing.T) {
	d, _ := newTestDaemon(t)
	have := map[string]bool{}
	for _, m := range d.Table.PublicMethods() {
		have[string(m)] = true
	}
	for _, m := range []string{
		"notify.channel.create", "notify.channel.update", "notify.channel.enable",
		"notify.channel.disable", "notify.channel.archive", "notify.channel.test",
		"notify.subscription.set", "notify.channel.list", "notify.channel.show",
		"notify.delivery.list",
	} {
		if have[m] {
			t.Fatalf("%s registered on a storeless daemon (V13: ABSENT, not refusing)", m)
		}
	}
}

func TestChannelCreateAnswersHostNeverURLOrSecret(t *testing.T) {
	d, _, home := channelDaemon(t)
	asPerson(t, d, "user", "carlos")
	secretFile := writeSecretFile(t, home, "k.key", "TOP-SECRET-VALUE")
	res, err := channelDispatch(t, d, "notify.channel.create", fmt.Sprintf(
		`{"name":"ops","url":"https://hooks.example.com/deep/PATHSECRET/path","secretFile":%q}`, secretFile))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	view, ok := res.(protocol.NotifyChannelView)
	if !ok {
		t.Fatalf("result type %T", res)
	}
	if view.Enabled {
		t.Error("created enabled")
	}
	if view.Host != "hooks.example.com" {
		t.Errorf("host %q, want the host alone", view.Host)
	}
	if !view.SecretPresent {
		t.Error("secretPresent false after a secretFile create")
	}
	blob, _ := json.Marshal(res)
	for _, banned := range []string{"TOP-SECRET-VALUE", "PATHSECRET", "https://", secretFile} {
		if strings.Contains(string(blob), banned) {
			t.Errorf("the create result carries %q — views answer host and bools, never the url, never the secret", banned)
		}
	}
}

func TestChannelWritesRefuseAgentsAndIdentityKeys(t *testing.T) {
	d, _, _ := channelDaemon(t)
	// The agent peer.
	asPerson(t, d, "agent", "worker-1")
	_, err := channelDispatch(t, d, "notify.channel.create", `{"name":"x","url":"https://a.example.com"}`)
	if err == nil || !strings.Contains(err.Error(), "person's act") {
		t.Fatalf("agent create error = %v", err)
	}
	// The unattributed connection.
	d.Table.ActorFor = nil
	_, err = channelDispatch(t, d, "notify.channel.enable", `{"id":"nc-x"}`)
	if err == nil {
		t.Fatal("an unattributed connection enabled a channel")
	}
	// An identity key in the params — refused even for a person.
	asPerson(t, d, "user", "carlos")
	_, err = channelDispatch(t, d, "notify.channel.create",
		`{"name":"x","url":"https://a.example.com","actor":"carlos"}`)
	if err == nil || !strings.Contains(err.Error(), "SV13") {
		t.Fatalf("identity-key create error = %v", err)
	}
}

func TestEnableIsRefusedUntilTheSecretResolves(t *testing.T) {
	d, _, _ := channelDaemon(t)
	asPerson(t, d, "user", "carlos")
	res, err := channelDispatch(t, d, "notify.channel.create", `{"name":"x","url":"https://a.example.com"}`)
	if err != nil {
		t.Fatal(err)
	}
	id := res.(protocol.NotifyChannelView).ID
	_, err = channelDispatch(t, d, "notify.channel.enable", fmt.Sprintf(`{"id":%q}`, id))
	var perr *protocol.Error
	if !errors.As(err, &perr) || perr.Code != protocol.ErrSecretMissing.Code {
		t.Fatalf("enable without secret = %v, want ErrSecretMissing (%d)", err, protocol.ErrSecretMissing.Code)
	}
}

func TestTestVerbOnADeadReceiverIsErrTestFailed(t *testing.T) {
	d, _, home := channelDaemon(t)
	asPerson(t, d, "user", "carlos")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	secretFile := writeSecretFile(t, home, "k.key", "s")
	res, err := channelDispatch(t, d, "notify.channel.create", fmt.Sprintf(
		`{"name":"x","url":"%s","allowPrivate":true,"secretFile":%q}`, srv.URL, secretFile))
	if err != nil {
		t.Fatal(err)
	}
	id := res.(protocol.NotifyChannelView).ID
	if _, err := channelDispatch(t, d, "notify.channel.enable", fmt.Sprintf(`{"id":%q}`, id)); err != nil {
		t.Fatal(err)
	}
	_, err = channelDispatch(t, d, "notify.channel.test", fmt.Sprintf(`{"id":%q}`, id))
	var perr *protocol.Error
	if !errors.As(err, &perr) || perr.Code != protocol.ErrTestFailed.Code {
		t.Fatalf("failed test = %v, want ErrTestFailed (%d)", err, protocol.ErrTestFailed.Code)
	}
	// The row exists — the test was a delivery, not a rumour.
	rows, _ := d.store.ListDeliveries(id, 10)
	if len(rows) != 1 || rows[0].Outcome != "failed" || rows[0].HTTPStatus != 404 {
		t.Fatalf("delivery rows after a failed test: %+v", rows)
	}
}

func TestTestVerbRefusedForDisabledAndUnknown(t *testing.T) {
	d, _, home := channelDaemon(t)
	asPerson(t, d, "user", "carlos")
	secretFile := writeSecretFile(t, home, "k.key", "s")
	res, err := channelDispatch(t, d, "notify.channel.create", fmt.Sprintf(
		`{"name":"x","url":"https://a.example.com","secretFile":%q}`, secretFile))
	if err != nil {
		t.Fatal(err)
	}
	id := res.(protocol.NotifyChannelView).ID
	if _, err := channelDispatch(t, d, "notify.channel.test", fmt.Sprintf(`{"id":%q}`, id)); err == nil {
		t.Error("a test ran on a DISABLED channel")
	}
	if _, err := channelDispatch(t, d, "notify.channel.test", `{"id":"nc-nope"}`); err == nil {
		t.Error("a test ran on a channel that does not exist")
	}
}

func TestDeliveryListRefusesUnknownAndArchivedWithoutTheFlag(t *testing.T) {
	d, _, home := channelDaemon(t)
	asPerson(t, d, "user", "carlos")
	secretFile := writeSecretFile(t, home, "k.key", "s")
	res, _ := channelDispatch(t, d, "notify.channel.create", fmt.Sprintf(
		`{"name":"x","url":"https://a.example.com","secretFile":%q}`, secretFile))
	id := res.(protocol.NotifyChannelView).ID
	_, err := channelDispatch(t, d, "notify.delivery.list", `{"channelId":"nc-nope"}`)
	var perr *protocol.Error
	if !errors.As(err, &perr) || perr.Code != protocol.ErrDeliveryBad.Code {
		t.Fatalf("unknown channel delivery list = %v, want ErrDeliveryBad", err)
	}
	if _, err := channelDispatch(t, d, "notify.channel.archive", fmt.Sprintf(`{"id":%q,"reason":"done"}`, id)); err != nil {
		t.Fatal(err)
	}
	if _, err := channelDispatch(t, d, "notify.delivery.list", fmt.Sprintf(`{"channelId":%q}`, id)); err == nil {
		t.Error("an archived channel's log answered without includeArchived")
	}
	got, err := channelDispatch(t, d, "notify.delivery.list", fmt.Sprintf(`{"channelId":%q,"includeArchived":true}`, id))
	if err != nil {
		t.Fatalf("includeArchived: %v", err)
	}
	_ = got
}

func TestPublishFeedsSocketsAndChannelsFromOneResolution(t *testing.T) {
	// The whole point of the ONE fan-out point: a single Publish reaches
	// the subscriber with its hint AND the receiver with its envelope —
	// one class resolution, two consumers.
	d, st, home := channelDaemon(t)
	asPerson(t, d, "user", "carlos")
	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(b))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	secretFile := writeSecretFile(t, home, "k.key", "s")
	res, err := channelDispatch(t, d, "notify.channel.create", fmt.Sprintf(
		`{"name":"x","url":"%s","allowPrivate":true,"secretFile":%q}`, srv.URL, secretFile))
	if err != nil {
		t.Fatal(err)
	}
	id := res.(protocol.NotifyChannelView).ID
	if _, err := channelDispatch(t, d, "notify.subscription.set",
		fmt.Sprintf(`{"channelId":%q,"minSeverity":"info"}`, id)); err != nil {
		t.Fatal(err)
	}
	if _, err := channelDispatch(t, d, "notify.channel.enable", fmt.Sprintf(`{"id":%q}`, id)); err != nil {
		t.Fatal(err)
	}
	// Seed a loop so the pin read resolves the slugs the envelope carries.
	if _, err := st.CreateComponent(store.NewComponent{Name: "Alpha", Slug: "alpha", Root: "/w/alpha"}); err != nil {
		t.Fatal(err)
	}
	loop, err := st.CreatePinnedLoop(store.NewLoop{Slug: "demo", ComponentRoot: "/w/alpha",
		Root: "/w/alpha", Goal: "g", Status: "running"}, "none", 1, "[]", "{}")
	if err != nil {
		t.Fatal(err)
	}
	loopID := loop.ID
	// One publish. Both doors must answer; the socket half (the hint on
	// the wire frame) is already paid for in notify_test.go — here the
	// channel half rides the SAME publishWithNotify resolution.
	d.Publish("loop.finished", "", map[string]any{"loopId": loopID})
	// The channel side: drain the dispatcher and read the envelope.
	d.channels.Stop()
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 1 {
		t.Fatalf("receiver got %d envelopes for one event", len(bodies))
	}
	var env map[string]any
	if err := json.Unmarshal([]byte(bodies[0]), &env); err != nil {
		t.Fatal(err)
	}
	if env["event"] != "loop.finished" {
		t.Errorf("envelope event %v", env["event"])
	}
	if env["loopSlug"] != "demo" || env["rootSlug"] != "alpha" {
		t.Errorf("envelope slugs %v/%v, want demo/alpha (the pin read serves the wire too)",
			env["loopSlug"], env["rootSlug"])
	}
}

func TestDigestAtStartCountsTheLiveInbox(t *testing.T) {
	d, st, home := channelDaemon(t)
	asPerson(t, d, "user", "carlos")
	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(b))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	secretFile := writeSecretFile(t, home, "k.key", "s")
	res, _ := channelDispatch(t, d, "notify.channel.create", fmt.Sprintf(
		`{"name":"x","url":"%s","allowPrivate":true,"secretFile":%q}`, srv.URL, secretFile))
	id := res.(protocol.NotifyChannelView).ID
	if _, err := channelDispatch(t, d, "notify.channel.enable", fmt.Sprintf(`{"id":%q}`, id)); err != nil {
		t.Fatal(err)
	}
	// Empty inbox: a digest at start says NOTHING.
	d.NotifyDigestAtStart(context.Background())
	d.channels.dp.Stop()
	mu.Lock()
	if len(bodies) != 0 {
		mu.Unlock()
		t.Fatalf("digest fired on an empty inbox: %v", bodies)
	}
	mu.Unlock()
	// A question exists → exactly one `inbox.digest`, and its title is a count.
	seedQuestion(t, st)
	// A FRESH dispatcher stands in for "a new start" (the digest guard
	// is once per process, so the same seam cannot demonstrate it twice).
	fresh := &notifyChannels{st: st, cs: d.channels.cs,
		inbox: d.channels.inbox, resolver: d.channels.resolver,
		dp: notify.NewDispatcher(st, notify.FileSecrets{}, notify.DispatcherOptions{Version: "test"})}
	d.channels = fresh
	fresh.dp.Start()
	fresh.DigestAtStart(context.Background())
	fresh.dp.Stop()
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 1 {
		t.Fatalf("digests for one item: %d", len(bodies))
	}
	var env map[string]any
	if err := json.Unmarshal([]byte(bodies[0]), &env); err != nil {
		t.Fatal(err)
	}
	if env["event"] != "inbox.digest" || !strings.Contains(fmt.Sprint(env["title"]), "attention") {
		t.Errorf("digest envelope %s", bodies[0])
	}
}

func TestChannelViewCarriesFailureStreak(t *testing.T) {
	d, _, home := channelDaemon(t)
	asPerson(t, d, "user", "carlos")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	secretFile := writeSecretFile(t, home, "k.key", "s")
	res, _ := channelDispatch(t, d, "notify.channel.create", fmt.Sprintf(
		`{"name":"x","url":"%s","allowPrivate":true,"secretFile":%q}`, srv.URL, secretFile))
	id := res.(protocol.NotifyChannelView).ID
	if _, err := channelDispatch(t, d, "notify.subscription.set",
		fmt.Sprintf(`{"channelId":%q,"minSeverity":"info"}`, id)); err != nil {
		t.Fatal(err)
	}
	if _, err := channelDispatch(t, d, "notify.channel.enable", fmt.Sprintf(`{"id":%q}`, id)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		d.Publish("loop.finished", "", map[string]any{"loopId": "l-none"})
	}
	d.channels.Stop()
	got, err := channelDispatch(t, d, "notify.channel.show", fmt.Sprintf(`{"id":%q}`, id))
	if err != nil {
		t.Fatal(err)
	}
	view := got.(protocol.NotifyChannelView)
	if view.LastOutcome != "failed" || view.ConsecutiveFailures != 3 {
		t.Fatalf("view %+v — three dead sends must read as a streak of 3 (T6's doctor warning rides this)", view)
	}
}
