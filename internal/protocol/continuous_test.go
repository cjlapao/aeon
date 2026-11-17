package protocol

// S27 T7: the scheduler doors' protocol-side law — V13 absence, the
// identity-key ban, the actor from the connection, the bounded audit read.
// The semantics of the service itself (template validation, the one-time
// tree, pause's reach) are the daemon's own tests; this file pins the DOOR.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// continuousFake is the seam for the census and the door tests. It records
// what each door asked for so a test can assert what the CONNECTION
// supplied, not just that a call answered.
type continuousFake struct {
	statusView     ContinuousStatusView
	previewOut     ContinuousPreview
	rows           []ContinuousDecisionRow
	att            ContinuousAttention
	lastActor      ContinuousActor
	masterEnabled  *bool
	paused         *bool
	pauseReason    string
	rootsWritten   []ContinuousRootInput
	disabledRoot   string
	treeWritten    []string
	stopReason     string
	stopRes        ContinuousStopAllResult
	lastQuery      ContinuousDecisionQuery
	refusal        *ContinuousRefusal
	err            error
	previewCalls   int
	decisionsCalls int
}

func (f *continuousFake) Status(context.Context) (ContinuousStatusView, error) {
	return f.statusView, f.err
}
func (f *continuousFake) Preview(_ context.Context, root string) (ContinuousPreview, error) {
	f.previewCalls++
	f.lastActor = ContinuousActor{ID: root} // records the resolved filter
	return f.previewOut, f.err
}
func (f *continuousFake) Decisions(_ context.Context, q ContinuousDecisionQuery) ([]ContinuousDecisionRow, error) {
	f.decisionsCalls++
	f.lastQuery = q
	return f.rows, f.err
}
func (f *continuousFake) Attention(context.Context, string) (ContinuousAttention, error) {
	return f.att, f.err
}
func (f *continuousFake) SetMaster(_ context.Context, actor ContinuousActor, enabled bool) (*ContinuousRefusal, error) {
	en := enabled
	f.lastActor, f.masterEnabled = actor, &en
	return f.refusal, f.err
}
func (f *continuousFake) SetPaused(_ context.Context, actor ContinuousActor, paused bool, reason string) (*ContinuousRefusal, error) {
	pd := paused
	f.lastActor, f.paused, f.pauseReason = actor, &pd, reason
	return f.refusal, f.err
}
func (f *continuousFake) SetRoot(_ context.Context, actor ContinuousActor, in ContinuousRootInput) (ContinuousRepoView, *ContinuousRefusal, error) {
	f.lastActor = actor
	f.rootsWritten = append(f.rootsWritten, in)
	view := ContinuousRepoView{ComponentRoot: in.ComponentRoot, Enabled: in.Enabled}
	return view, f.refusal, f.err
}
func (f *continuousFake) DisableRoot(_ context.Context, actor ContinuousActor, root string) (*ContinuousRefusal, error) {
	f.lastActor, f.disabledRoot = actor, root
	return f.refusal, f.err
}
func (f *continuousFake) EnableTree(_ context.Context, actor ContinuousActor, _ string, in ContinuousRootInput) ([]string, *ContinuousRefusal, error) {
	f.lastActor = actor
	f.treeWritten = append(f.treeWritten, in.ComponentRoot)
	return f.treeWritten, f.refusal, f.err
}
func (f *continuousFake) StopAll(_ context.Context, actor ContinuousActor, reason string) (ContinuousStopAllResult, *ContinuousRefusal, error) {
	f.lastActor, f.stopReason = actor, reason
	return f.stopRes, f.refusal, f.err
}

func schedulerMethodNames() []MethodName {
	return []MethodName{
		MethodSchedulerStatus, MethodSchedulerPreview, MethodSchedulerDecisions, MethodSchedulerAttention,
		MethodSchedulerEnable, MethodSchedulerDisable, MethodSchedulerPause, MethodSchedulerResume,
		MethodSchedulerRootSet, MethodSchedulerRootDisable, MethodSchedulerEnableTree, MethodSchedulerStopAll,
	}
}

func schedulerTable(t *testing.T, f *continuousFake) *Table {
	t.Helper()
	tbl := newTestTable(t)
	tbl.ActorFor = userActorFor("peer-1")
	tbl.Continuous = f
	if !tbl.RegisterContinuous() {
		t.Fatal("RegisterContinuous refused a wired seam")
	}
	initialized(t, tbl, "c1", PROTOCOL_VERSION)
	return tbl
}

// TestSchedulerMethodsRegistered pins T7's table row: the exact twelve, all
// RequiresInitialize, none Debug, and ABSENT (ErrUnknownMethod, not a
// registered-and-refusing door) without the seam — V13, the settings
// precedent, scheduler.stopAll included (T6's schedulerStopAllWired is the
// same fact at the daemon's end).
func TestSchedulerMethodsRegistered(t *testing.T) {
	names := schedulerMethodNames()
	if len(names) != 12 {
		t.Fatalf("the set is %d names, want exactly twelve — count it, don't guess it", len(names))
	}
	plain := newTestTable(t)
	if plain.RegisterContinuous() {
		t.Fatal("RegisterContinuous over a nil seam claimed the methods — V13 wants them absent, not refusing")
	}
	initialized(t, plain, "c1", PROTOCOL_VERSION)
	for _, m := range names {
		if _, ok := plain.Lookup(m); ok {
			t.Errorf("%s is registered without its seam — a nil Continuous must leave it ABSENT (V13)", m)
		}
		_, err := plain.Dispatch(context.Background(), "c1", m, nil)
		var unknown ErrUnknownMethod
		if !errors.As(err, &unknown) {
			t.Errorf("%s without its seam answered %v — absent is ErrUnknownMethod, not a refusal", m, err)
		}
	}
	tbl := newTestTable(t)
	f := &continuousFake{}
	tbl.ActorFor = userActorFor("peer-1")
	tbl.Continuous = f
	if !tbl.RegisterContinuous() {
		t.Fatal("RegisterContinuous refused a wired seam")
	}
	for _, m := range names {
		meth, ok := tbl.Lookup(m)
		if !ok {
			t.Fatalf("%s missing with the seam wired", m)
		}
		if !meth.RequiresInitialize {
			t.Errorf("%s must require initialize", m)
		}
		if meth.Debug {
			t.Errorf("%s must not be Debug", m)
		}
	}
}

// TestActorIsThePeerNotParams is T7's headline lock (CN2): the authority
// record only hears the person on the connection. An `actor` key is refused
// BY KEY before anything runs; a non-user kind is refused by name; the user
// on the socket arrives as the audit identity untouched.
func TestActorIsThePeerNotParams(t *testing.T) {
	f := &continuousFake{}
	tbl := schedulerTable(t, f)

	_, err := tbl.Dispatch(context.Background(), "c1", MethodSchedulerEnable,
		json.RawMessage(`{"actor":{"kind":"user","id":"someone-else"}}`))
	var bad ErrBadParams
	if !errors.As(err, &bad) {
		t.Fatalf("an actor param answered %v, want a params refusal", err)
	}
	if !strings.Contains(err.Error(), `"actor"`) {
		t.Errorf("the refusal must name the key, got: %v", err)
	}
	if f.masterEnabled != nil {
		t.Error("the fake service ran despite the params refusal")
	}

	// A non-person kind: refused by name, service untouched.
	tbl.ActorFor = func(context.Context, *Session) Actor { return Actor{Kind: "agent", ID: "loop-1"} }
	_, err = tbl.Dispatch(context.Background(), "c1", MethodSchedulerEnable, nil)
	var ae *Error
	if !errors.As(err, &ae) || ae.Code != ErrSchedulerRefused.Code {
		t.Fatalf("agent-kind enable answered %v, want ErrSchedulerRefused", err)
	}
	if f.masterEnabled != nil {
		t.Error("a refused actor reached the service")
	}

	// The person on the socket: the fake sees the peer, exactly. (The
	// KIND check above overwrote ActorFor; restore the peer first — the
	// door reads the connection's identity at dispatch time, which is
	// exactly the point.)
	tbl.ActorFor = userActorFor("peer-1")
	if _, err := tbl.Dispatch(context.Background(), "c1", MethodSchedulerEnable, json.RawMessage(`{}`)); err != nil {
		t.Fatalf("user enable failed: %v", err)
	}
	if f.lastActor != (ContinuousActor{Kind: "user", ID: "peer-1"}) {
		t.Errorf("the service saw %+v — the CONNECTION's peer was want {user peer-1}", f.lastActor)
	}
}

// TestSchedulerReadsBanIdentityKeys: the ban is not control-only — a read
// carrying `status` or `source` is the same forged-history attempt.
func TestSchedulerReadsBanIdentityKeys(t *testing.T) {
	f := &continuousFake{}
	tbl := schedulerTable(t, f)
	for _, pair := range [][2]string{
		{string(MethodSchedulerStatus), `{"source":"automation"}`},
		{string(MethodSchedulerDecisions), `{"actor":"me"}`},
		{string(MethodSchedulerAttention), `{"status":"blocked"}`},
		{string(MethodSchedulerPreview), `{"createdBy":"someone"}`},
	} {
		_, err := tbl.Dispatch(context.Background(), "c1", MethodName(pair[0]), json.RawMessage(pair[1]))
		var bad ErrBadParams
		if !errors.As(err, &bad) {
			t.Errorf("%s accepted %s — identity keys are refused on reads too", pair[0], pair[1])
		}
	}
}

// TestDecisionsFilterAndLimit: the audit read is FILTERED and BOUNDED —
// limit 501 is refused at the door, the wire subject "queue/7" arrives at
// the service split into its parts, and an unparseable subject or since is
// refused rather than ignored.
func TestDecisionsFilterAndLimit(t *testing.T) {
	f := &continuousFake{}
	tbl := schedulerTable(t, f)

	if _, err := tbl.Dispatch(context.Background(), "c1", MethodSchedulerDecisions,
		json.RawMessage(`{"limit":501}`)); err == nil {
		t.Error("limit 501 was accepted — the audit read must be bounded (1..500)")
	} else {
		var bad ErrBadParams
		if !errors.As(err, &bad) {
			t.Errorf("want a params refusal, got %v", err)
		}
	}
	if _, err := tbl.Dispatch(context.Background(), "c1", MethodSchedulerDecisions,
		json.RawMessage(`{"subject":"notakind/7"}`)); err == nil {
		t.Error("subject \"notakind/7\" accepted — kind is the ledger's closed set")
	}
	if _, err := tbl.Dispatch(context.Background(), "c1", MethodSchedulerDecisions,
		json.RawMessage(`{"since":"yesterday"}`)); err == nil {
		t.Error("since \"yesterday\" accepted — the bound is RFC3339 or silence")
	}
	if _, err := tbl.Dispatch(context.Background(), "c1", MethodSchedulerDecisions,
		json.RawMessage(`{"componentRoot":"/srv/alpha","subject":"queue/7","decision":"started","reason":"capacity","limit":20}`)); err != nil {
		t.Fatalf("the clean filter failed: %v", err)
	}
	q := f.lastQuery
	if !q.HasSubject || q.SubjectKind != "queue" || q.SubjectID != 7 ||
		q.ComponentRoot != "/srv/alpha" || q.Decision != "started" || q.Reason != "capacity" || q.Limit != 20 {
		t.Errorf("the service saw %+v — the door must pass the whole filter split and bounded", q)
	}
	// Omitted limit → the service decides its own default (0 = not typed).
	if _, err := tbl.Dispatch(context.Background(), "c1", MethodSchedulerDecisions, json.RawMessage(`{}`)); err != nil {
		t.Fatalf("empty params failed: %v", err)
	}
	if f.lastQuery.Limit != 0 {
		t.Errorf("limit %d — an omitted limit must not be invented at the door", f.lastQuery.Limit)
	}
}

// TestPauseCarriesItsWord: CN18 requires the reason — a blank pause is a
// switch moved for reasons nobody wrote down.
func TestPauseCarriesItsWord(t *testing.T) {
	f := &continuousFake{}
	tbl := schedulerTable(t, f)
	for _, m := range []MethodName{MethodSchedulerPause, MethodSchedulerStopAll} {
		for _, raw := range []string{`{}`, `{"reason":"  "}`} {
			if _, err := tbl.Dispatch(context.Background(), "c1", m, json.RawMessage(raw)); err == nil {
				t.Errorf("%s accepted %s — the reason is required (CN18)", m, raw)
			}
		}
	}
	if _, err := tbl.Dispatch(context.Background(), "c1", MethodSchedulerPause,
		json.RawMessage(`{"reason":"schema migration at 03:00"}`)); err != nil {
		t.Fatalf("the clean pause failed: %v", err)
	}
	if f.pauseReason != "schema migration at 03:00" {
		t.Errorf("the service saw reason %q", f.pauseReason)
	}
}

// TestRootSetCommandsAnExplicitEnabled: an omitted `enabled` would
// silently enable a repository nobody typed yes to — the pointer is the
// door's refusal, not the zero value's permission.
func TestRootSetCommandsAnExplicitEnabled(t *testing.T) {
	f := &continuousFake{}
	tbl := schedulerTable(t, f)
	if _, err := tbl.Dispatch(context.Background(), "c1", MethodSchedulerRootSet,
		json.RawMessage(`{"componentRoot":"/srv/alpha","blueprint":"aeon-build"}`)); err == nil {
		t.Fatal("root.set without `enabled` was accepted")
	}
	if len(f.rootsWritten) != 0 {
		t.Error("the service ran despite the missing-enabled refusal")
	}
	// Both filters named is a request the door cannot honestly answer.
	if _, err := tbl.Dispatch(context.Background(), "c1", MethodSchedulerPreview,
		json.RawMessage(`{"componentRoot":"/srv/alpha","rootId":"cmp_1"}`)); err == nil {
		t.Error("preview accepted componentRoot AND rootId — the filter is one or the other")
	}
}

// TestSchedulerRefusalsRenderTyped: the seam's refusal becomes the typed
// error with its sentence verbatim in `what`; the master-off family gets
// its own code.
func TestSchedulerRefusalsRenderTyped(t *testing.T) {
	f := &continuousFake{refusal: &ContinuousRefusal{Sentence: "blueprint x does not end (CN5)"}}
	tbl := schedulerTable(t, f)
	_, err := tbl.Dispatch(context.Background(), "c1", MethodSchedulerRootSet,
		json.RawMessage(`{"componentRoot":"/srv/alpha","enabled":true,"blueprint":"x"}`))
	var ae *Error
	if !errors.As(err, &ae) || ae.Code != ErrSchedulerRefused.Code {
		t.Fatalf("refusal answered %v, want ErrSchedulerRefused", err)
	}
	if ae.Data.What != "blueprint x does not end (CN5)" {
		t.Errorf("what = %q — the validator's sentence must ride VERBATIM", ae.Data.What)
	}
	f.refusal = &ContinuousRefusal{Sentence: "needs the master on first", NeedsMaster: true}
	_, err = tbl.Dispatch(context.Background(), "c1", MethodSchedulerRootSet,
		json.RawMessage(`{"componentRoot":"/srv/alpha","enabled":true,"blueprint":"x"}`))
	if !errors.As(err, &ae) || ae.Code != ErrSchedulerDisabled.Code {
		t.Fatalf("master-off answered %v, want ErrSchedulerDisabled", err)
	}
}
