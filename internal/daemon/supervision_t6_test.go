package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/stepctx"
	"github.com/cjlapao/aeon/internal/store"
)

// supervision_t6_test.go — S58 T6 at the wiring layer. The two claims only
// the assembled daemon can make:
//
//  1. mission.status's needsYou IS inbox.summary for the same scope — not
//     "equals by careful reading", equals because the daemon injects the
//     SAME service into both doors (the equality by construction the spec
//     demands; this test is the tripwire if someone forks it).
//  2. the five supervision tools answer OWN VIEW: the agent's own root, or
//     the one refusal sentence — never another tree's facts.

// t6SupervisionEnv: one root with a live question, one unrelated root, and
// a running step under the first — the identity every tool call stamps.
func t6SupervisionEnv(t *testing.T) (*Daemon, *store.Store, *RecordsService, string, string, int64) {
	t.Helper()
	st, _ := openInboxStore(t)
	alpha, err := st.CreateComponent(store.NewComponent{Name: "Alpha", Slug: "alpha", Root: "/w/alpha"})
	if err != nil || !alpha.OK() {
		t.Fatalf("alpha: %v", err)
	}
	beta, err := st.CreateComponent(store.NewComponent{Name: "Beta", Slug: "beta", Root: "/w/beta"})
	if err != nil || !beta.OK() {
		t.Fatalf("beta: %v", err)
	}
	if _, err := st.CreateLoop(store.NewLoop{Slug: "demo", ComponentRoot: "/w/alpha", Goal: "a goal"}); err != nil {
		t.Fatalf("loop: %v", err)
	}
	sc := store.Scope{LoopSlug: "demo", ComponentRoot: "/w/alpha"}
	if _, err := st.Ask(sc, store.NewQuestion{Kind: "clarify", Body: "which way?", Options: []string{"approve", "deny"}}); err != nil {
		t.Fatalf("ask: %v", err)
	}
	step := startLaneStep(t, st, "demo", "/w/alpha", "worker")
	d, _ := newTestDaemonWithStore(t, st)
	rs := NewRecordsService(st)
	rs.supervision = &daemonSupervision{d: d}
	return d, st, rs, alpha.Component.ID, beta.Component.ID, step
}

func TestMissionAbsentWithoutAStore(t *testing.T) {
	d, _ := newTestDaemon(t)
	for _, m := range d.Table.PublicMethods() {
		if m == protocol.MethodMissionStatus {
			t.Fatal("mission.status is registered on a storeless daemon (V13: ABSENT, not refusing)")
		}
	}
}

func TestMissionNeedsYouEqualsInboxSummary(t *testing.T) {
	d, _, _, alphaID, _, _ := t6SupervisionEnv(t)
	ctx := context.Background()

	sumRaw, err := d.Table.Dispatch(ctx, "c1", protocol.MethodInboxSummary,
		json.RawMessage(`{"rootId":"`+alphaID+`"}`))
	if err != nil {
		t.Fatalf("inbox.summary: %v", err)
	}
	sum := sumRaw.(protocol.InboxSummaryResult)
	if sum.Total != 1 {
		t.Fatalf("inbox.summary total = %+v, want the one live question", sum)
	}

	misRaw, err := d.Table.Dispatch(ctx, "c1", protocol.MethodMissionStatus,
		json.RawMessage(`{"rootId":"`+alphaID+`"}`))
	if err != nil {
		t.Fatalf("mission.status: %v", err)
	}
	mis := misRaw.(protocol.MissionStatusResult)
	if len(mis.Components) != 1 {
		t.Fatalf("components = %+v, want the one root", mis.Components)
	}
	cs := mis.Components[0]
	// THE equality, field for field (the one map encoder, byte for byte).
	a, err := json.Marshal(cs.NeedsYou)
	if err != nil {
		t.Fatalf("marshal needsYou: %v", err)
	}
	b, err := json.Marshal(sum)
	if err != nil {
		t.Fatalf("marshal summary: %v", err)
	}
	if string(a) != string(b) {
		t.Fatalf("needsYou and inbox.summary drifted for the SAME scope:\n  mission: %s\n  inbox:   %s", a, b)
	}
	// The unwired groups print their names (the fixture pins the same).
	if cs.Cap.State != "not-wired" || cs.NextReady.State != "not-wired" {
		t.Fatalf("cap=%+v nextReady=%+v — the seams are unwired in this build; a zero would LIE (SV11)", cs.Cap, cs.NextReady)
	}
	// And an unknown root is the inbox's OWN typed refusal — one rule,
	// one sentence, both doors.
	_, err = d.Table.Dispatch(ctx, "c1", protocol.MethodMissionStatus,
		json.RawMessage(`{"rootId":"cmp-does-not-exist"}`))
	var perr *protocol.Error
	if !errors.As(err, &perr) || perr.Code != protocol.ErrInboxScope.Code {
		t.Fatalf("unknown root = %v, want ErrInboxScope (-31926)", err)
	}
}

func TestSupervisionToolsAreReadOnlyAndOwnView(t *testing.T) {
	_, _, rs, alphaID, betaID, step := t6SupervisionEnv(t)
	ctx := stepctx.WithStepID(context.Background(), step)
	by := Actor{Kind: "agent", ID: "worker"}

	// The own view answers: no root_id is the agent's OWN tree by stamp.
	out, f, err := rs.Do(ctx, by, "inbox_summary", nil)
	if f != nil || err != nil {
		t.Fatalf("inbox_summary own view: %v %v", f, err)
	}
	sum := out.(protocol.InboxSummaryResult)
	if sum.Total != 1 {
		t.Fatalf("own summary = %+v, want the one question under the agent's root", sum)
	}
	// Naming your own root explicitly is fine…
	in := json.RawMessage(`{"root_id":"` + alphaID + `"}`)
	if _, f, err := rs.Do(ctx, by, "inbox_list", in); f != nil || err != nil {
		t.Fatalf("inbox_list own root: %v %v", f, err)
	}
	// …and another tree is THE refusal, verbatim, before any read happens.
	for _, name := range []string{"inbox_list", "inbox_summary", "mission_status"} {
		steal := json.RawMessage(`{"root_id":"` + betaID + `"}`)
		_, f, err := rs.Do(ctx, by, name, steal)
		if err != nil {
			t.Fatalf("%s cross-tree: error %v, want a Failure", name, err)
		}
		if f == nil {
			t.Fatalf("%s ANSWERED another root's tree — SV16 broken", name)
		}
		if f.Sentence != ownViewSentence {
			t.Fatalf("%s refused with %q, want the own-view sentence verbatim", name, f.Sentence)
		}
	}
	// The five tools are reads by declaration AND by behavior: every spec
	// in the family says Mutates:false / class safe…
	// (the catalog declares it; TestRecordsParitySupervisionFamily pins the
	// catalog; here the behavior: a write name does not exist at all).
	for _, name := range []string{"inbox_list", "inbox_summary", "mission_status", "notify_class_list", "notify_channel_list"} {
		_, f, err := rs.Do(ctx, by, name, nil)
		if f != nil || err != nil {
			t.Fatalf("%s: %v %v — the five own-view reads must all answer over a wired daemon", name, f, err)
		}
	}
}

func TestNoSupervisionWriteToolExists(t *testing.T) {
	_, _, rs, _, _, step := t6SupervisionEnv(t)
	ctx := stepctx.WithStepID(context.Background(), step)
	by := Actor{Kind: "agent", ID: "worker"}
	// Acking is a person's act (SV12); channels are authority (SV7); class
	// edits are the human door (SV4). None of these names is in the catalog,
	// so every call lands on the identity gate's own refusal — and NO
	// refusal there is a write.
	for _, name := range []string{"inbox_ack", "inbox_snooze", "inbox_unack", "notify_class_update",
		"notify_channel_create", "notify_channel_enable", "notify_channel_disable",
		"notify_channel_archive", "notify_channel_test", "notify_subscription_set"} {
		_, f, err := rs.Do(ctx, by, name, nil)
		if err == nil && f == nil {
			t.Fatalf("%s ANSWERED — a supervision write reached the tool plane", name)
		}
		if err != nil && !strings.Contains(err.Error(), "unknown") && !strings.Contains(err.Error(), "refused") {
			t.Fatalf("%s: unexpected error shape %v", name, err)
		}
	}
}

func TestAgentChannelViewCarriesNoSecrets(t *testing.T) {
	d, _, rs, _, _, step := t6SupervisionEnv(t)
	ctx := stepctx.WithStepID(context.Background(), step)
	by := Actor{Kind: "agent", ID: "worker"}

	// A real channel row, secret and all — created through the person's
	// own door, because it is a person's act (SV7).
	secretFile := t.TempDir() + "/whsec_topsecret"
	if err := os.WriteFile(secretFile, []byte("whsec_topsecret_value\n"), 0o600); err != nil {
		t.Fatalf("secret file: %v", err)
	}
	raw, err := d.Table.Dispatch(context.Background(), "c1", protocol.MethodNotifyChannelCreate,
		json.RawMessage(`{"name":"ops","url":"https://hooks.slack.com/services/T00X/B00Y/supersecrettoken","secretFile":"`+secretFile+`"}`))
	if err != nil {
		t.Fatalf("channel create: %v", err)
	}
	view := raw.(protocol.NotifyChannelView)
	if !view.SecretPresent {
		t.Fatal("test setup: the daemon did not take the secret")
	}

	out, f, err := rs.Do(ctx, by, "notify_channel_list", nil)
	if f != nil || err != nil {
		t.Fatalf("notify_channel_list: %v %v", f, err)
	}
	// The agent's answer is the OWN-VIEW projection — a different struct,
	// not a redaction of the person's view (4c's discipline, SV16).
	list := out.([]agentChannelView)
	if len(list) != 1 {
		t.Fatalf("channels = %+v, want the one row", list)
	}
	ch := list[0]
	if ch.Host != "hooks.slack.com" {
		t.Fatalf("host = %q — the agent view carries the HOST, not the URL (SV7)", ch.Host)
	}
	b, err := json.Marshal(list)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, leak := range []string{"T00X", "B00Y", "supersecrettoken", "whsec", "services", secretFile} {
		if strings.Contains(string(b), leak) {
			t.Fatalf("the agent channel view carries %q — a credential goes IN and never comes OUT (4c, SV7)", leak)
		}
	}
	// Structurally: the view type has NO field that could hold a secret, a
	// path, or even the existence of a secret — agents route by name, not
	// by credential facts (SV16).
	typ := reflect.TypeOf(agentChannelView{})
	for i := 0; i < typ.NumField(); i++ {
		tag := strings.ToLower(typ.Field(i).Tag.Get("json"))
		for _, bad := range []string{"secret", "url", "path", "file", "token"} {
			if strings.Contains(tag, bad) {
				t.Errorf("agentChannelView has field %q — the projection itself must be incapable of carrying %q", tag, bad)
			}
		}
	}
}
