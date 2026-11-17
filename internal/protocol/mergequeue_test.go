package protocol

// mergequeue_test.go — S60 T8: the ten merge-queue doors, tested AT THE
// DOOR. Three things a door must get right and a service test cannot prove:
//
//   - **V13 is a registration, not a refusal.** Without its seam the plane
//     is ten ABSENT methods (ErrUnknownMethod), not ten doors that say "no
//     store" — a screen enumerating the surface must not see a half-built
//     queue.
//   - **The actor is the connection's person, and the state is the
//     driver's.** No params field names an author or a status; the raw key
//     bag is checked BEFORE the lenient decode, so a smuggled `actor`
//     cannot arrive as an unknown field and be ignored (MQ13).
//   - **The land binds to the read.** A land without the digest from
//     mergequeue.show is a hope, not an approval, and the door says so
//     before the service runs.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// mqDoorFake records the actor and calls so the door's claims are
// observable rather than assumed.
type mqDoorFake struct {
	calls        map[MethodName]int
	lastActor    Actor
	reached      bool
	lastEnqueue  MergeQueueEnqueueParams
	lastLand     MergeQueueLandParams
	lastSettings MergeQueueSettingsParams
	itemOut      ItemView
	settingsStmt string
	err          error
}

func newMqDoorFake() *mqDoorFake { return &mqDoorFake{calls: map[MethodName]int{}} }

func (f *mqDoorFake) hit(m MethodName) { f.calls[m]++; f.reached = true }

func (f *mqDoorFake) List(context.Context, MergeQueueListParams) ([]ItemView, error) {
	f.hit(MethodMergeQueueList)
	return []ItemView{f.itemOut}, f.err
}
func (f *mqDoorFake) Show(context.Context, MergeQueueShowParams) (ShowResult, error) {
	f.hit(MethodMergeQueueShow)
	return ShowResult{Item: f.itemOut}, f.err
}
func (f *mqDoorFake) Status(context.Context, MergeQueueStatusParams) (StatusResult, error) {
	f.hit(MethodMergeQueueStatus)
	return StatusResult{Counts: map[string]int{"queued": 1}}, f.err
}
func (f *mqDoorFake) Vocabulary(context.Context) (VocabularyResult, error) {
	f.hit(MethodMergeQueueVocabulary)
	return VocabularyResult{States: []StateView{{Name: "queued", Meaning: "waiting", LegalNext: []string{"catching_up", "removed"}}}}, f.err
}
func (f *mqDoorFake) Enqueue(_ context.Context, a Actor, p MergeQueueEnqueueParams) (ItemView, error) {
	f.hit(MethodMergeQueueEnqueue)
	f.lastActor, f.lastEnqueue = a, p
	return f.itemOut, f.err
}
func (f *mqDoorFake) Dequeue(_ context.Context, a Actor, p MergeQueueIDParams) (ItemView, error) {
	f.hit(MethodMergeQueueDequeue)
	f.lastActor = a
	return f.itemOut, f.err
}
func (f *mqDoorFake) Reorder(_ context.Context, a Actor, p MergeQueueReorderParams) (ItemView, error) {
	f.hit(MethodMergeQueueReorder)
	f.lastActor = a
	return f.itemOut, f.err
}
func (f *mqDoorFake) Retry(_ context.Context, a Actor, p MergeQueueIDParams) (ItemView, error) {
	f.hit(MethodMergeQueueRetry)
	f.lastActor = a
	return f.itemOut, f.err
}
func (f *mqDoorFake) Land(_ context.Context, a Actor, p MergeQueueLandParams) (ItemView, error) {
	f.hit(MethodMergeQueueLand)
	f.lastActor, f.lastLand = a, p
	return f.itemOut, f.err
}
func (f *mqDoorFake) Settings(_ context.Context, a Actor, p MergeQueueSettingsParams) (ConfigView, string, error) {
	f.hit(MethodMergeQueueSettings)
	f.lastActor, f.lastSettings = a, p
	return ConfigView{ComponentRoot: p.ComponentRoot, Mode: p.Mode}, f.settingsStmt, f.err
}

func mergeQueueTable(t *testing.T, fake *mqDoorFake, peer string) *Table {
	t.Helper()
	tbl := newTestTable(t)
	tbl.MergeQueue = fake
	if peer != "" {
		tbl.ActorFor = userActorFor(peer)
	}
	if !tbl.RegisterMergeQueue() {
		t.Fatal("RegisterMergeQueue refused a wired seam")
	}
	initialized(t, tbl, "c1", PROTOCOL_VERSION)
	return tbl
}

func TestMergeQueueMethodsRegisteredAndGated(t *testing.T) {
	names := mergequeueMethodNames()
	if len(names) != 10 {
		t.Fatalf("the merge-queue plane is %d names, want exactly ten — count it, do not guess it", len(names))
	}

	// V13: no seam, no doors.
	plain := newTestTable(t)
	if plain.RegisterMergeQueue() {
		t.Fatal("RegisterMergeQueue claimed the methods over a nil seam — V13 wants them absent")
	}
	initialized(t, plain, "c1", PROTOCOL_VERSION)
	for _, m := range names {
		if _, ok := plain.Lookup(m); ok {
			t.Errorf("%s is registered without its seam", m)
		}
		_, err := plain.Dispatch(context.Background(), "c1", m, nil)
		var unknown ErrUnknownMethod
		if !errors.As(err, &unknown) {
			t.Errorf("%s without its seam answered %v — absent is ErrUnknownMethod, not a refusal", m, err)
		}
	}

	// Wired: every name present, RequiresInitialize on all ten, Debug on
	// none, and nothing beyond the ten registered by accident.
	tbl := mergeQueueTable(t, newMqDoorFake(), "peer-1")
	for _, m := range names {
		meth, ok := tbl.Lookup(m)
		if !ok {
			t.Errorf("%s is missing from the table the registration claimed to fill", m)
			continue
		}
		if !meth.RequiresInitialize {
			t.Errorf("%s registers before initialize — queue rows are store rows, and a pre-initialize answer would read a half-open database", m)
		}
		if meth.Debug {
			t.Errorf("%s is Debug — the queue is the person's surface, not a diagnostic", m)
		}
	}
}

func TestMergeQueueParamsRefuseIdentityAndStateKeys(t *testing.T) {
	// Every door, every banned key, refused BEFORE the service. The actor is
	// the connection's; the status is the driver's; the stamped columns are
	// the store's clock. A params field that could name any of them makes
	// the queue's history forgeable (MQ13).
	banned := []string{"actor", "actorKind", "status", "reason", "reasonKind", "enqueuedBy",
		"landedSha", "verifiedSha", "catchUps", "createdAt", "updatedAt", "updatedBy", "position"}
	for _, key := range banned {
		for _, m := range mergequeueMethodNames() {
			fake := newMqDoorFake()
			tbl := mergeQueueTable(t, fake, "peer-1")
			raw, _ := json.Marshal(map[string]any{"id": "mq-x", "componentRoot": "/srv/alpha", key: "whatever"})
			_, err := tbl.Dispatch(context.Background(), "c1", m, raw)
			var bad ErrBadParams
			if !errors.As(err, &bad) {
				t.Errorf("%s accepted params carrying %q — MQ13 wants a parse error before the service", m, key)
				continue
			}
			if !strings.Contains(bad.Err.Error(), `"${key}"`) && !strings.Contains(bad.Err.Error(), "\""+key+"\"") {
				t.Errorf("%s's refusal for %q does not name the key it refused: %v", m, key, bad)
			}
			if fake.reached {
				t.Errorf("%s reached the service on refused keys (%v) — a refusal after the service already touched the data", m, fake.calls)
			}
		}
	}
}

func TestMergeQueueWritesStampThePeerNeverParams(t *testing.T) {
	fake := newMqDoorFake()
	tbl := mergeQueueTable(t, fake, "cjlapao")
	for _, c := range []struct {
		m      MethodName
		params string
	}{
		{MethodMergeQueueEnqueue, `{"componentRoot":"/srv/alpha","featureBranch":"feat-x","headSha":"aaaa1111bbbb2222cccc3333dddd4444eeee5555","attended":true}`},
		{MethodMergeQueueDequeue, `{"id":"mq-1"}`},
		{MethodMergeQueueReorder, `{"id":"mq-1","beforeId":"mq-2"}`},
		{MethodMergeQueueRetry, `{"id":"mq-1"}`},
		{MethodMergeQueueLand, `{"id":"mq-1","digest":"aaaa"}`},
		{MethodMergeQueueSettings, `{"componentRoot":"/srv/alpha","mode":"manual"}`},
	} {
		if _, err := tbl.Dispatch(context.Background(), "c1", c.m, json.RawMessage(c.params)); err != nil {
			t.Fatalf("%s: %v", c.m, err)
		}
		if fake.lastActor.Kind != "user" || fake.lastActor.ID != "cjlapao" {
			t.Errorf("%s stamped %+v — the actor is the connection's person {user, cjlapao}", c.m, fake.lastActor)
		}
	}
}

func TestMergeQueueWritesRefuseAnUnresolvablePeer(t *testing.T) {
	// The honest absence: no identity seam at all. The refusal is the
	// plane's own voice (ErrMergeQueueRefused, -31952) and it lands BEFORE
	// the service — nothing half-wrote anything.
	fake := newMqDoorFake()
	tbl := newTestTable(t)
	tbl.MergeQueue = fake
	if !tbl.RegisterMergeQueue() {
		t.Fatal("registration failed")
	}
	initialized(t, tbl, "c1", PROTOCOL_VERSION)
	_, err := tbl.Dispatch(context.Background(), "c1", MethodMergeQueueEnqueue,
		json.RawMessage(`{"componentRoot":"/srv/alpha","featureBranch":"feat-x","headSha":"aaaa"}`))
	var perr *Error
	if !errors.As(err, &perr) || perr.Code != ErrMergeQueueRefused.Code {
		t.Fatalf("an unresolvable peer answered %v, want the plane's refusal (-31952)", err)
	}
	if fake.reached {
		t.Errorf("the service ran for an anonymous write (%v) — the identity check is the door's first act", fake.calls)
	}
}

func TestMergeQueueLandBindsToTheRead(t *testing.T) {
	fake := newMqDoorFake()
	tbl := mergeQueueTable(t, fake, "cjlapao")
	if _, err := tbl.Dispatch(context.Background(), "c1", MethodMergeQueueLand,
		json.RawMessage(`{"id":"mq-1"}`)); err == nil {
		t.Fatal("a land with no digest sailed through — an unbound approval is a hope, not an approval")
	} else {
		var bad ErrBadParams
		if !errors.As(err, &bad) {
			t.Fatalf("the digestless land answered %T (%v), want a parse error at the door", err, err)
		}
	}
	if fake.calls[MethodMergeQueueLand] != 0 {
		t.Errorf("the digestless land reached the service (%d calls) — the binding is checked before the lane moves", fake.calls[MethodMergeQueueLand])
	}
	if _, err := tbl.Dispatch(context.Background(), "c1", MethodMergeQueueLand,
		json.RawMessage(`{"id":"mq-1","digest":"beef"}`)); err != nil {
		t.Fatalf("a land WITH a digest must reach the service to bind it: %v", err)
	}
	if fake.lastLand.Digest != "beef" {
		t.Errorf("the digest did not survive the door: %q", fake.lastLand.Digest)
	}
}

func TestMergeQueueRefusalsKeepTheirSentences(t *testing.T) {
	// The four codes travel with the SERVICE's sentence in `what` — DB19:
	// nothing paraphrases a refusal.
	cases := []struct {
		err  error
		code int
		what string
	}{
		{ErrMergeQueueRefused.WithWhat("the queue for /srv/alpha is dark (mode off)"), -31952, "dark (mode off)"},
		{ErrMergeQueueNotFound.WithWhat(`no merge_queue_item row answers id "mq-nope"`), -31953, "mq-nope"},
		{ErrMergeQueueBaseUnknown.WithWhat(`base branch "release" is not known for this repository`), -31954, "not known"},
		{ErrMergeQueueConflictState.WithWhat("mergequeue.land belongs to a ready item and mq-1 is verifying"), -31955, "is verifying"},
	}
	for _, tc := range cases {
		fake := newMqDoorFake()
		fake.err = tc.err
		tbl := mergeQueueTable(t, fake, "cjlapao")
		_, err := tbl.Dispatch(context.Background(), "c1", MethodMergeQueueRetry, json.RawMessage(`{"id":"mq-1"}`))
		var perr *Error
		if !errors.As(err, &perr) {
			t.Fatalf("code %d: answered %T (%v)", tc.code, err, err)
		}
		if perr.Code != tc.code {
			t.Errorf("code = %d, want %d", perr.Code, tc.code)
		}
		if !strings.Contains(perr.Data.What, tc.what) {
			t.Errorf("the `what` lost the service's sentence: %q", perr.Data.What)
		}
	}
}

func TestMergeQueueVocabularyAnswersTheDoors(t *testing.T) {
	fake := newMqDoorFake()
	fake.Vocabulary(context.Background()) //nolint — the fake answers without methods
	tbl := mergeQueueTable(t, fake, "cjlapao")
	got, err := tbl.Dispatch(context.Background(), "c1", MethodMergeQueueVocabulary, nil)
	if err != nil {
		t.Fatalf("vocabulary: %v", err)
	}
	res, ok := got.(VocabularyResult)
	if !ok {
		t.Fatalf("vocabulary answered %T", got)
	}
	if len(res.Methods) != 10 {
		t.Fatalf("the methods table is %d doors, want the plane's ten derived from the registration list", len(res.Methods))
	}
	reads := 0
	for _, md := range res.Methods {
		if md.Door == "read" {
			reads++
		}
		if md.Door != "read" && md.Door != "human" {
			t.Errorf("%s carries door %q — the closed pair", md.Name, md.Door)
		}
	}
	if reads != 4 {
		t.Errorf("the plane answers %d reads, want four (list, show, status, vocabulary)", reads)
	}
	// Params are nobody's business: the vocabulary takes none.
	if _, err := tbl.Dispatch(context.Background(), "c1", MethodMergeQueueVocabulary,
		json.RawMessage(`{"componentRoot":"/srv/alpha"}`)); err == nil {
		t.Error("the vocabulary accepted params — the vocabulary is the plane's, not the caller's")
	}
}
