package daemon

// notify_classes_test.go — S58 T4 end to end: the class table's doors,
// the seeded rows behind them, and the resolver they feed. A real store,
// the real seed engine (the rows arrive the production way), the real
// table dispatch (the actor from the peer, SV13), and the resolver on
// the live publish path (what a subscriber actually receives).

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/notify"
	"github.com/cjlapao/aeon/internal/notifyseed"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/seed"
	"github.com/cjlapao/aeon/internal/store"
)

// newNotifyClassDaemon is the store-backed daemon with the notify.events
// set applied through the real engine — the state `aeon serve` reaches at
// startup, rebuilt in a test because the promise under test is exactly
// what that state does to events and doors.
func newNotifyClassDaemon(t *testing.T) (*Daemon, *store.Store, *wire) {
	t.Helper()
	st, _ := openInboxStore(t)
	e, err := NewSeedEngine(st, seed.Options{}, seedValidatorPass)
	if err != nil {
		t.Fatalf("seed engine: %v", err)
	}
	if _, err := e.Apply(context.Background(), notifyseed.SetID, nil,
		seed.Actor{Kind: "system", ID: "startup"}, seed.PathStartup); err != nil {
		t.Fatalf("apply notify.events: %v", err)
	}
	d, w := newTestDaemonWithStore(t, st)
	if d.classes == nil {
		t.Fatal("a store-backed daemon built no resolver — publish resolves nothing (SV5 wiring is dead)")
	}
	subscribe(t, d, "c1", protocol.SubscribeParams{})
	return d, st, w
}

func classDispatch(t *testing.T, d *Daemon, method, params string) (any, error) {
	t.Helper()
	return d.Table.Dispatch(context.Background(), "c1", protocol.MethodName(method), json.RawMessage(params))
}

// TestClassDoorsAbsentWithoutStore is V13 on the three new doors: a
// storeless daemon does not register-and-refuse, it does not answer.
func TestClassDoorsAbsentWithoutStore(t *testing.T) {
	d, _ := newTestDaemon(t)
	have := map[string]bool{}
	for _, m := range d.Table.PublicMethods() {
		have[string(m)] = true
	}
	for _, m := range []string{"notify.class.list", "notify.class.update", "notify.vocabulary"} {
		if have[m] {
			t.Fatalf("%s registered on a storeless daemon (V13: ABSENT, not refusing)", m)
		}
	}
}

func TestClassDoorsLiveWithAStore(t *testing.T) {
	d, _, _ := newNotifyClassDaemon(t)
	have := map[string]bool{}
	for _, m := range d.Table.PublicMethods() {
		have[string(m)] = true
	}
	for _, m := range []string{"notify.class.list", "notify.class.update", "notify.vocabulary"} {
		if !have[m] {
			t.Fatalf("%s missing on a store-backed daemon", m)
		}
	}
	res, err := classDispatch(t, d, "notify.class.list", `{}`)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	list := res.(protocol.NotifyClassListResult)
	if len(list.Classes) < 8 {
		t.Fatalf("the seeded table reads %d rows — the apply did not land", len(list.Classes))
	}
	for _, c := range list.Classes {
		if c.Origin != "system" || c.Version != 1 {
			t.Errorf("row %s/%s came from the seed as origin=%q v%d, want system v1", c.Kind, c.Variant, c.Origin, c.Version)
		}
	}
}

func TestClassUpdateChangesOneRowAndBumpsTheVersion(t *testing.T) {
	d, st, _ := newNotifyClassDaemon(t)
	asPerson(t, d, "user", "1000")
	res, err := classDispatch(t, d, "notify.class.update", `{"kind":"loop.finished","enabled":false}`)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	item := res.(protocol.NotifyClassItem)
	if item.Enabled || item.Version != 2 || item.Origin != "system" {
		t.Fatalf("update answered %+v, want enabled=false version=2 origin=system", item)
	}
	row, _, _ := st.GetNotifyClass("loop.finished", "")
	if row.Enabled || row.Version != 2 {
		t.Fatalf("the row reads %+v after the update", row)
	}
	// The same write again is a no-op: the row keeps its version (LC11 —
	// the store writes no audit row for a summary that did not change,
	// asserted byte-for-byte in the store's own test).
	res, err = classDispatch(t, d, "notify.class.update", `{"kind":"loop.finished","enabled":false}`)
	if err != nil {
		t.Fatalf("no-op update: %v", err)
	}
	if again := res.(protocol.NotifyClassItem); again.Version != 2 {
		t.Fatalf("a no-op update bumped the version to %d — it changed nothing", again.Version)
	}
}

func TestClassUpdateRefusesTemplatesOutsideTheClosedSet(t *testing.T) {
	d, st, _ := newNotifyClassDaemon(t)
	asPerson(t, d, "user", "1000")
	_, err := classDispatch(t, d, "notify.class.update",
		`{"kind":"loop.blocked","title":"Loop blocked: {goalText}"}`)
	var perr *protocol.Error
	if !errors.As(err, &perr) || perr.Code != protocol.ErrClassTemplate.Code {
		t.Fatalf("template refusal = %v, want ErrClassTemplate (-31925)", err)
	}
	if !strings.Contains(perr.Data.What, "not a placeholder") {
		t.Fatalf("refusal what = %q, want it to name the closed set", perr.Data.What)
	}
	// Nothing was written: SV4's "nothing invalid is persisted".
	row, _, _ := st.GetNotifyClass("loop.blocked", "")
	if row.TitleTemplate != "Loop blocked" || row.Version != 1 {
		t.Fatalf("the refusal still wrote the row: %+v", row)
	}
}

func TestClassUpdateAcceptsAClosedTemplateAndTheResolverRendersIt(t *testing.T) {
	d, st, _ := newNotifyClassDaemon(t)
	seedQuestion(t, st) // the component row (alpha) behind {rootSlug}
	// A loop events can name is a PINNED loop (ReadPinnedLoop reads the
	// loop_blueprint row the create pins — the gateReactor pinLoop way).
	lp, err := st.CreatePinnedLoop(store.NewLoop{Slug: "keyed", ComponentRoot: "/w/alpha", Root: "/w/alpha", Goal: "a goal", Status: "running"},
		"none", 1, "goal: a goal\n", "{}")
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	asPerson(t, d, "user", "1000")
	// The edit lands on the VARIANT row the emitter will actually reach
	// (loop.blocked/action-denied): the (kind,'') base row is the
	// FALLBACK, not the first stop — resolution is variant first.
	tmpl := "{rootSlug}/{loopSlug} blocked: {reasonKind}"
	res, err := classDispatch(t, d, "notify.class.update",
		`{"kind":"loop.blocked","variant":"action-denied","title":"`+tmpl+`"}`)
	if err != nil {
		t.Fatalf("closed-template update: %v", err)
	}
	if item := res.(protocol.NotifyClassItem); item.TitleTemplate != tmpl || item.Version != 2 {
		t.Fatalf("update answered %+v", item)
	}
	h := d.classes.HintFor(protocol.KindLoopBlocked,
		map[string]any{"loopId": lp.ID, "reason": map[string]any{"kind": "action-denied"}})
	if h == nil {
		t.Fatal("the edited class resolved nil")
	}
	if want := "alpha/keyed blocked: action-denied"; h.Title != want {
		t.Fatalf("rendered title = %q, want %q", h.Title, want)
	}
	// The base row is untouched (byte-identical) — the edit changed ONE
	// row, not the kind.
	base := d.classes.HintFor(protocol.KindLoopBlocked, map[string]any{"loopId": lp.ID})
	if base == nil || base.Title != "Loop blocked" {
		t.Fatalf("the base row drifted: %+v", base)
	}
}

func TestDisabledClassResolvesNoHintEvenOverTheWire(t *testing.T) {
	d, _, w := newNotifyClassDaemon(t)
	asPerson(t, d, "user", "1000")
	if _, err := classDispatch(t, d, "notify.class.update", `{"kind":"loop.finished","enabled":false}`); err != nil {
		t.Fatalf("disable: %v", err)
	}
	before := len(w.delivered())
	d.Table.OnEvent(protocol.KindLoopFinished, map[string]any{"loopId": "demo"})
	var found *protocol.Event
	for _, s := range w.delivered()[before:] {
		if s.Event.Kind == protocol.KindLoopFinished {
			ev := s.Event
			found = &ev
		}
	}
	if found == nil {
		t.Fatal("the event itself must still be published — a disabled class silences the TOAST, never the event (SV4)")
	}
	if found.Notify != nil {
		t.Fatalf("a disabled class still raised a hint: %+v", found.Notify)
	}
}

func TestClassUpdateActorIsThePeerNotParams(t *testing.T) {
	d, st, _ := newNotifyClassDaemon(t)
	_, err := classDispatch(t, d, "notify.class.update", `{"kind":"loop.finished","enabled":false,"actor":"someone-else"}`)
	var bad protocol.ErrBadParams
	if !errors.As(err, &bad) || !strings.Contains(err.Error(), `params carry "actor"`) {
		t.Fatalf("params-named actor = %v, want the identity refusal", err)
	}
	// The agent peer is refused with the sentence; the row never moves.
	asPerson(t, d, "agent", "step-9")
	_, err = classDispatch(t, d, "notify.class.update", `{"kind":"loop.finished","enabled":false}`)
	if !errors.As(err, &bad) || !strings.Contains(err.Error(), "person's decision") {
		t.Fatalf("agent update = %v, want the SV13 refusal", err)
	}
	row, _, _ := st.GetNotifyClass("loop.finished", "")
	if !row.Enabled || row.Version != 1 {
		t.Fatalf("a refused write moved the row: %+v", row)
	}
}

func TestClassUpdateNamesAClassThatExists(t *testing.T) {
	d, _, _ := newNotifyClassDaemon(t)
	asPerson(t, d, "user", "1000")
	_, err := classDispatch(t, d, "notify.class.update", `{"kind":"not.a.kind","enabled":false}`)
	var bad protocol.ErrBadParams
	if !errors.As(err, &bad) || !strings.Contains(err.Error(), "not in the table") {
		t.Fatalf("unknown class = %v, want a refusal naming the row and the list door", err)
	}
}

func TestVocabularyIsTheWholePlane(t *testing.T) {
	d, _, _ := newNotifyClassDaemon(t)
	res, err := classDispatch(t, d, "notify.vocabulary", `{}`)
	if err != nil {
		t.Fatalf("vocabulary: %v", err)
	}
	v := res.(protocol.NotifyVocabularyResult)
	if len(v.Kinds) != 8 || v.Kinds[0] != "budget.hit" {
		t.Errorf("kinds = %v, want the table's eight distinct kinds in order", v.Kinds)
	}
	if strings.Join(v.Placeholders, ",") != "rootSlug,loopSlug,reasonKind,severity,count" {
		t.Errorf("placeholders = %v — the descriptor and the validator disagree", v.Placeholders)
	}
	if len(v.Severities) != 5 || v.Severities[0] != "needs-you" {
		t.Errorf("severities = %v, want the five in rank order", v.Severities)
	}
	if v.Limits.TitleMaxBytes != notify.MaxTitleTemplateBytes || v.Limits.BodyMaxBytes != notify.MaxBodyTemplateBytes {
		t.Errorf("limits = %+v — the descriptor quotes numbers the validator does not keep", v.Limits)
	}
}

// TestQuestionEventsShareOneAttentionKey is SV7's whole dedup story in
// one assertion: the daemon raises TWO events for one question
// (loop.waiting and question.asked); the hints carry the SAME subject
// key the inbox itself mints, so a consumer that dedups by the key it
// was handed delivers ONE notification — and the resolver itself
// deduped nothing, which is the design (the app and the dispatcher
// own the dedup; the key is the handle).
func TestQuestionEventsShareOneAttentionKey(t *testing.T) {
	d, st, _ := newNotifyClassDaemon(t)
	q := seedQuestion(t, st)
	want := questionKey(t, st, q)

	waiting := d.classes.HintFor(protocol.KindLoopWaiting,
		map[string]any{"loopId": "demo", "questionId": q.ID})
	if waiting == nil {
		t.Fatal("loop.waiting resolved nil")
	}
	if waiting.AttentionKey != want {
		t.Fatalf("loop.waiting key = %q, want %q", waiting.AttentionKey, want)
	}
	if waiting.DeepLink != "aeon://loop/demo" {
		t.Errorf("waiting deepLink = %q, want the loop target", waiting.DeepLink)
	}
	// The question.asked emission is the PENDING QUESTION ITSELF
	// (step.go:837 emits result.Question, whose json field for the row id
	// is `id`) — no loopId, so the link is the inbox and the key is the
	// same question.
	asked := d.classes.HintFor(protocol.KindQuestionAsked, map[string]any{"id": q.ID})
	if asked == nil {
		t.Fatal("question.asked resolved nil")
	}
	if asked.AttentionKey != want {
		t.Fatalf("question.asked key = %q, want %q — the dedup handle must match", asked.AttentionKey, want)
	}
	if asked.DeepLink != "aeon://inbox" {
		t.Errorf("asked deepLink = %q, want aeon://inbox (no loop field in the real payload)", asked.DeepLink)
	}
	// A finished loop names no inbox subject: the honest empty, not a
	// fabricated key.
	done := d.classes.HintFor(protocol.KindLoopFinished, map[string]any{"loopId": "demo"})
	if done == nil || done.AttentionKey != "" {
		t.Fatalf("loop.finished = %+v, want a hint with NO attention key", done)
	}
}

// TestDeliveredHintCarriesTheWireFields proves the whole stack end to
// end: the event reaches the subscriber carrying attentionKey and
// deepLink — the fields T8's web adapters dedup and navigate on.
func TestDeliveredHintCarriesTheWireFields(t *testing.T) {
	d, st, w := newNotifyClassDaemon(t)
	seedQuestion(t, st)
	// A loop event's loopId is the loop row's GENERATED id (store
	// newID()), not its slug — the key and the link must speak the id.
	lp, err := st.CreatePinnedLoop(store.NewLoop{Slug: "keyed", ComponentRoot: "/w/alpha", Root: "/w/alpha", Goal: "a goal", Status: "running"},
		"none", 1, "goal: a goal\n", "{}")
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	pin, err := st.ReadPinnedLoop(lp.ID)
	if err != nil {
		t.Fatalf("pin: %v", err)
	}
	before := len(w.delivered())
	d.Table.OnEvent(protocol.KindLoopBlocked, map[string]any{
		"loopId": lp.ID, "reason": map[string]any{"kind": "action-denied", "detail": "user text"}})
	var found *protocol.Event
	for _, s := range w.delivered()[before:] {
		if s.Event.Kind == protocol.KindLoopBlocked {
			ev := s.Event
			found = &ev
		}
	}
	if found == nil {
		t.Fatal("loop.blocked never reached the subscriber")
	}
	if found.Notify == nil {
		t.Fatal("no hint on the delivery")
	}
	if want := "blocked:" + lp.ID + ":" + pin.Loop.UpdatedAt; found.Notify.AttentionKey != want {
		t.Errorf("attentionKey = %q, want %q (the key the blocked source mints)", found.Notify.AttentionKey, want)
	}
	if found.Notify.DeepLink != "aeon://loop/"+lp.ID {
		t.Errorf("deepLink = %q", found.Notify.DeepLink)
	}
	if found.Notify.Title != "Loop blocked" {
		t.Errorf("title = %q, want the byte-identical curated title", found.Notify.Title)
	}
}

// TestSeedApplyIsIdempotentAndNeverResurrects is P8 at the door level:
// the same apply again changes nothing, and a class a person disabled is
// NOT re-enabled by the engine (the disabled flag lives inside the
// fingerprint content, so the row reads diverged and the engine skips
// it — the record deviation this task documents).
func TestSeedApplyIsIdempotentAndNeverResurrects(t *testing.T) {
	d, st, _ := newNotifyClassDaemon(t)
	asPerson(t, d, "user", "1000")
	if _, err := classDispatch(t, d, "notify.class.update", `{"kind":"loop.waiting","enabled":false}`); err != nil {
		t.Fatalf("disable: %v", err)
	}
	e, err := NewSeedEngine(st, seed.Options{}, seedValidatorPass)
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.Apply(context.Background(), notifyseed.SetID, nil,
		seed.Actor{Kind: "system", ID: "startup"}, seed.PathStartup)
	if err != nil {
		t.Fatalf("re-apply: %v", err)
	}
	for _, rec := range res.Records {
		if rec.Key == "loop.waiting|" && rec.Op != seed.OpSkip {
			t.Fatalf("the re-apply chose %q (%s) for a class the operator disabled — the engine wrote over a person (P8)", rec.Op, rec.Reason)
		}
	}
	row, _, _ := st.GetNotifyClass("loop.waiting", "")
	if row.Enabled {
		t.Fatalf("disabled class came back enabled: %+v", row)
	}
}
