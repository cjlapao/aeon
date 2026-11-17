package protocol

// environment_test.go — S59 T6: the twenty doors of the execution-environment
// plane, tested at the door.
//
// Three things a door must get right and a service test cannot prove:
//
//   - **V13 is a registration, not a refusal.** Without its seam the plane is
//     twenty ABSENT methods (`ErrUnknownMethod`, the -32601 the transport
//     answers), not twenty doors that say "no store" — an app enumerating the
//     surface must not see a half-built plane.
//   - **The actor is the connection's person.** No params field can name an
//     author, and the raw key bag is checked BEFORE the lenient decode so a
//     smuggled `actor` cannot arrive as an unknown field and be ignored.
//   - **A value has nowhere to be sent.** The secret-reference params refuse the
//     vocabulary a credential would arrive under — `value`, `token`, `apiKey`,
//     `raw`, `bytes` and the rest — on every one of the seven methods, read or
//     write: a value smuggled into a READ would still be a value the daemon is
//     being asked to fetch.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// --- the fakes --------------------------------------------------------------------

// envFake is the environment seam. It records the actor and the last params of
// each write so the door's own claims are observable rather than assumed.
type envFake struct {
	lastActor  Actor
	calls      map[string]int
	lastCreate EnvironmentCreateParams
	lastUpdate EnvironmentUpdateParams
	lastBind   EnvironmentBindParams
	lastTear   ProvisionIDParams
	createOut  EnvironmentView
	bindOut    BindResult
	err        error
}

func newEnvFake() *envFake { return &envFake{calls: map[string]int{}} }

func (f *envFake) List(context.Context, EnvironmentListParams) ([]EnvironmentView, error) {
	f.calls["list"]++
	return []EnvironmentView{{ID: "env-1", Slug: "host-scrubbed", Backend: "host-scrubbed"}}, f.err
}
func (f *envFake) Show(context.Context, EnvironmentShowParams) (EnvironmentView, error) {
	f.calls["show"]++
	return f.createOut, f.err
}
func (f *envFake) Effective(context.Context, EnvironmentEffectiveParams) (EnvironmentEffectiveView, error) {
	f.calls["effective"]++
	return EnvironmentEffectiveView{Backend: "host-scrubbed"}, f.err
}
func (f *envFake) Vocabulary(context.Context) (EnvironmentVocabularyView, error) {
	f.calls["vocabulary"]++
	return EnvironmentVocabularyView{}, f.err
}
func (f *envFake) Changes(context.Context, EnvironmentChangesParams) ([]EnvironmentChangeView, error) {
	f.calls["changes"]++
	return nil, f.err
}
func (f *envFake) Provisions(context.Context, EnvironmentProvisionsParams) ([]ProvisionView, error) {
	f.calls["provisions"]++
	return nil, f.err
}
func (f *envFake) Create(_ context.Context, a Actor, p EnvironmentCreateParams) (EnvironmentView, error) {
	f.calls["create"]++
	f.lastActor, f.lastCreate = a, p
	return f.createOut, f.err
}
func (f *envFake) Update(_ context.Context, a Actor, p EnvironmentUpdateParams) (EnvironmentView, error) {
	f.calls["update"]++
	f.lastActor, f.lastUpdate = a, p
	return f.createOut, f.err
}
func (f *envFake) Archive(_ context.Context, a Actor, p EnvironmentIDParams) (EnvironmentView, error) {
	f.calls["archive"]++
	f.lastActor = a
	_ = p
	return f.createOut, f.err
}
func (f *envFake) Restore(_ context.Context, a Actor, p EnvironmentIDParams) (EnvironmentView, error) {
	f.calls["restore"]++
	f.lastActor = a
	_ = p
	return f.createOut, f.err
}
func (f *envFake) Bind(_ context.Context, a Actor, p EnvironmentBindParams) (BindResult, error) {
	f.calls["bind"]++
	f.lastActor, f.lastBind = a, p
	return f.bindOut, f.err
}
func (f *envFake) Unbind(_ context.Context, a Actor, p EnvironmentUnbindParams) (UnbindResult, error) {
	f.calls["unbind"]++
	f.lastActor = a
	_ = p
	return UnbindResult{}, f.err
}
func (f *envFake) Teardown(_ context.Context, a Actor, p ProvisionIDParams) (ProvisionView, error) {
	f.calls["teardown"]++
	f.lastActor, f.lastTear = a, p
	return ProvisionView{}, f.err
}

// refFake is the secret-reference seam. It records nothing but the actor and
// the call count: the values it must never see are refused by the door before
// they reach it, which is what the value-key test proves.
type refFake struct {
	lastActor Actor
	calls     map[string]int
	createIn  SecretRefCreateParams
	updateIn  SecretRefUpdateParams
	err       error
}

func newRefFake() *refFake { return &refFake{calls: map[string]int{}} }

func (f *refFake) List(context.Context, SecretRefListParams) ([]SecretRefView, error) {
	f.calls["list"]++
	return nil, f.err
}
func (f *refFake) Show(context.Context, SecretRefShowParams) (SecretRefView, error) {
	f.calls["show"]++
	return SecretRefView{}, f.err
}
func (f *refFake) Check(context.Context, SecretRefCheckParams) (SecretRefCheckResult, error) {
	f.calls["check"]++
	return SecretRefCheckResult{}, f.err
}
func (f *refFake) Create(_ context.Context, a Actor, p SecretRefCreateParams) (SecretRefView, error) {
	f.calls["create"]++
	f.lastActor, f.createIn = a, p
	return SecretRefView{}, f.err
}
func (f *refFake) Update(_ context.Context, a Actor, p SecretRefUpdateParams) (SecretRefView, error) {
	f.calls["update"]++
	f.lastActor, f.updateIn = a, p
	return SecretRefView{}, f.err
}
func (f *refFake) Archive(_ context.Context, a Actor, p SecretRefNameParams) (SecretRefView, error) {
	f.calls["archive"]++
	f.lastActor = a
	_ = p
	return SecretRefView{}, f.err
}
func (f *refFake) Restore(_ context.Context, a Actor, p SecretRefNameParams) (SecretRefView, error) {
	f.calls["restore"]++
	f.lastActor = a
	_ = p
	return SecretRefView{}, f.err
}

// --- the name lists ---------------------------------------------------------------

func environmentMethodNames() []MethodName {
	return []MethodName{
		MethodEnvironmentList, MethodEnvironmentShow, MethodEnvironmentEffective,
		MethodEnvironmentVocabulary, MethodEnvironmentChanges, MethodEnvironmentProvisions,
		MethodEnvironmentCreate, MethodEnvironmentUpdate, MethodEnvironmentArchive,
		MethodEnvironmentRestore, MethodEnvironmentBind, MethodEnvironmentUnbind,
		MethodEnvironmentTeardown,
	}
}

func secretRefMethodNames() []MethodName {
	return []MethodName{
		MethodSecretRefList, MethodSecretRefShow, MethodSecretRefCheck,
		MethodSecretRefCreate, MethodSecretRefUpdate, MethodSecretRefArchive, MethodSecretRefRestore,
	}
}

func environmentTable(t *testing.T, ef *envFake, rf *refFake, actor string) *Table {
	t.Helper()
	tbl := newTestTable(t)
	if actor != "" {
		tbl.ActorFor = userActorFor(actor)
	}
	tbl.Environment = ef
	tbl.SecretRefs = rf
	if !tbl.RegisterEnvironment() {
		t.Fatal("RegisterEnvironment refused a wired seam")
	}
	if !tbl.RegisterSecretRef() {
		t.Fatal("RegisterSecretRef refused a wired seam")
	}
	initialized(t, tbl, "c1", PROTOCOL_VERSION)
	return tbl
}

// --- the tests --------------------------------------------------------------------

// TestEnvironmentMethodsRegistered is the plane's exact set: thirteen
// environment doors and seven reference doors, all behind their seam, all
// RequiresInitialize, none Debug — and absent, not refusing, without it.
func TestEnvironmentMethodsRegistered(t *testing.T) {
	envNames, refNames := environmentMethodNames(), secretRefMethodNames()
	if len(envNames) != 13 {
		t.Fatalf("the environment plane is %d names, want exactly thirteen — count it, do not guess it", len(envNames))
	}
	if len(refNames) != 7 {
		t.Fatalf("the reference plane is %d names, want exactly seven", len(refNames))
	}

	// V13: no seam, no doors.
	plain := newTestTable(t)
	if plain.RegisterEnvironment() {
		t.Fatal("RegisterEnvironment claimed the methods over a nil seam — V13 wants them absent")
	}
	if plain.RegisterSecretRef() {
		t.Fatal("RegisterSecretRef claimed the methods over a nil seam")
	}
	initialized(t, plain, "c1", PROTOCOL_VERSION)
	for _, m := range append(append([]MethodName{}, envNames...), refNames...) {
		if _, ok := plain.Lookup(m); ok {
			t.Errorf("%s is registered without its seam", m)
		}
		_, err := plain.Dispatch(context.Background(), "c1", m, nil)
		var unknown ErrUnknownMethod
		if !errors.As(err, &unknown) {
			t.Errorf("%s without its seam answered %v — absent is ErrUnknownMethod, not a refusal", m, err)
		}
	}

	// Wired: every name present, every flag right, and the registration
	// introduced nothing the lists do not name (a fourteenth door appearing by
	// accident is as much a bug as a thirteenth missing).
	tbl := environmentTable(t, newEnvFake(), newRefFake(), "peer-1")
	for _, m := range append(append([]MethodName{}, envNames...), refNames...) {
		meth, ok := tbl.Lookup(m)
		if !ok {
			t.Errorf("%s is missing from the table the registration claimed to fill", m)
			continue
		}
		if !meth.RequiresInitialize {
			t.Errorf("%s is reachable before initialize — this plane opens on a negotiated connection only", m)
		}
		if meth.Debug {
			t.Errorf("%s is Debug-marked — an environment door is the app's window, never an operator probe", m)
		}
	}
	var extra []MethodName
	for _, m := range tbl.Methods() {
		s := string(m)
		if (strings.HasPrefix(s, "environment.") || strings.HasPrefix(s, "secretref.")) &&
			!containsName(envNames, m) && !containsName(refNames, m) {
			extra = append(extra, m)
		}
	}
	if len(extra) != 0 {
		t.Errorf("the plane registered doors nobody named: %v", extra)
	}
}

func containsName(list []MethodName, m MethodName) bool {
	for _, n := range list {
		if n == m {
			return true
		}
	}
	return false
}

// TestEnvironmentParamsRefuseIdentityKeys: the actor, the origin, the legacy
// flag and the revision are never params (EV17, EV16). Checked on every method,
// read included — a smuggled actor on a READ still forges the identity the
// plane's reads answer as.
func TestEnvironmentParamsRefuseIdentityKeys(t *testing.T) {
	// Each plane checks the caller against the list its OWN params may never
	// name, read from the package rather than copied here — so the day a field
	// is legitimately added, the test and the door move together.
	// `environmentIdentityKeys` is the writes' list; the reads accept `origin`
	// because `environment.list` filters on it (the seed's rows and a person's
	// rows are different things to look at); the reference plane's list leaves
	// out `source` and `sourceKind` because naming where a value lives is what
	// a reference IS — its value keys, tested separately below, are the refusal.
	cases := []struct {
		names []MethodName
		keys  []string
	}{
		{[]MethodName{
			MethodEnvironmentCreate, MethodEnvironmentUpdate, MethodEnvironmentArchive,
			MethodEnvironmentRestore, MethodEnvironmentBind, MethodEnvironmentUnbind,
			MethodEnvironmentTeardown,
		}, environmentIdentityKeys},
		{[]MethodName{
			MethodEnvironmentList, MethodEnvironmentShow, MethodEnvironmentEffective,
			MethodEnvironmentVocabulary, MethodEnvironmentChanges, MethodEnvironmentProvisions,
		}, environmentReadIdentityKeys},
		{secretRefMethodNames(), secretRefIdentityKeys},
	}
	ef, rf := newEnvFake(), newRefFake()
	tbl := environmentTable(t, ef, rf, "peer-1")
	checked := 0
	for _, tc := range cases {
		for _, m := range tc.names {
			for _, key := range tc.keys {
				checked++
				raw := json.RawMessage(fmt.Sprintf(`{"%s":"whatever"}`, key))
				_, err := tbl.Dispatch(context.Background(), "c1", m, raw)
				var bad ErrBadParams
				if !errors.As(err, &bad) {
					t.Errorf("%s accepted params carrying %q (%v) — the actor is the peer's, the origin is the seed's, and a timestamp is the store's", m, key, err)
					continue
				}
				if !strings.Contains(bad.Error(), key) {
					t.Errorf("%s's refusal does not name the key it refused: %q", m, bad.Error())
				}
			}
		}
	}
	if checked < 100 {
		t.Fatalf("only %d (method, key) pairs were checked — the walk is vacuous and a vacuous walk is a pass that proved nothing", checked)
	}
}

// TestSecretRefParamsRefuseValueKeys is EV7 at the wire: no method of the
// reference plane accepts a params field in the vocabulary a credential arrives
// under, on a write OR a read. The complement matters as much — `source` and
// `sourceKind` ARE legal on the reference plane, so this is a refusal of
// values and not a blanket strict decode that would break the next additive
// field.
func TestSecretRefParamsRefuseValueKeys(t *testing.T) {
	tbl := environmentTable(t, newEnvFake(), newRefFake(), "peer-1")
	checked := 0
	for _, m := range secretRefMethodNames() {
		for _, key := range secretRefValueKeys {
			checked++
			raw := json.RawMessage(fmt.Sprintf(`{"name":"deploy-token","%s":"sk-live-1234567890"}`, key))
			_, err := tbl.Dispatch(context.Background(), "c1", m, raw)
			var bad ErrBadParams
			if !errors.As(err, &bad) {
				t.Errorf("%s accepted a params field named %q (%v) — a reference names where a value lives and never carries one", m, key, err)
				continue
			}
			if !strings.Contains(bad.Error(), key) {
				t.Errorf("%s's refusal does not name the key it refused: %q", m, bad.Error())
			}
		}
	}
	if checked < 90 {
		t.Fatalf("only %d (method, key) pairs were checked — a vacuous walk is a pass that proved nothing", checked)
	}
	// The complement: the fields a reference legitimately has are accepted.
	rf := newRefFake()
	tbl = environmentTable(t, newEnvFake(), rf, "peer-1")
	if _, err := tbl.Dispatch(context.Background(), "c1", MethodSecretRefCreate,
		mustJSON(t, SecretRefCreateParams{Name: "deploy-token", SourceKind: "daemon-env", Source: "DEPLOY_TOKEN", InjectAs: "DEPLOY_TOKEN"})); err != nil {
		t.Fatalf("a legitimate reference create was refused: %v", err)
	}
	if rf.createIn.Source != "DEPLOY_TOKEN" || rf.createIn.InjectAs != "DEPLOY_TOKEN" {
		t.Errorf("the create reached the service as %+v", rf.createIn)
	}
	// And an unknown, non-identity, non-value field is IGNORED: the door is a
	// closed list of refusals, not a strict decode (the additive-field promise).
	rf.calls = map[string]int{}
	if _, err := tbl.Dispatch(context.Background(), "c1", MethodSecretRefCreate,
		json.RawMessage(`{"name":"x","sourceKind":"daemon-env","source":"X","someFutureField":{"y":1}}`)); err != nil {
		t.Errorf("an additive unknown field was refused: %v", err)
	}
}

// TestEnvironmentWritesStampThePeer: the actor every write carries is the
// connection's attestation, and a connection the kernel cannot attest for does
// not write at all — an anonymous author in the audit is no author.
func TestEnvironmentWritesStampThePeer(t *testing.T) {
	ef := newEnvFake()
	tbl := environmentTable(t, ef, newRefFake(), "peer-1")
	writes := []struct {
		name MethodName
		raw  json.RawMessage
	}{
		{MethodEnvironmentCreate, mustJSON(t, EnvironmentCreateParams{Slug: "ci", Name: "CI", Backend: "host-scrubbed", Spec: `{"home":"real"}`})},
		{MethodEnvironmentUpdate, mustJSON(t, EnvironmentUpdateParams{ID: "env-1", Spec: `{"home":"real"}`, ExpectedRevision: 1})},
		{MethodEnvironmentArchive, mustJSON(t, EnvironmentIDParams{ID: "env-1"})},
		{MethodEnvironmentRestore, mustJSON(t, EnvironmentIDParams{ID: "env-1"})},
		{MethodEnvironmentBind, mustJSON(t, EnvironmentBindParams{Scope: "component", ComponentID: "cmp-1", EnvironmentID: "env-1"})},
		{MethodEnvironmentUnbind, mustJSON(t, EnvironmentUnbindParams{ComponentID: "cmp-1"})},
		{MethodEnvironmentTeardown, mustJSON(t, ProvisionIDParams{ProvisionID: "prv-1"})},
	}
	for _, w := range writes {
		ef.lastActor = Actor{}
		if _, err := tbl.Dispatch(context.Background(), "c1", w.name, w.raw); err != nil {
			t.Fatalf("%s: %v", w.name, err)
		}
		if ef.lastActor != (Actor{Kind: "user", ID: "peer-1"}) {
			t.Errorf("%s was stamped %v — the actor is ActorFor's answer, never a claim", w.name, ef.lastActor)
		}
	}

	// No attestation, no write (the uid-0 / blank-peer case the daemon refuses).
	efNoActor := newEnvFake()
	noActor := environmentTable(t, efNoActor, newRefFake(), "")
	if _, err := noActor.Dispatch(context.Background(), "c1", MethodEnvironmentCreate,
		mustJSON(t, EnvironmentCreateParams{Slug: "ci", Name: "CI", Backend: "host-scrubbed", Spec: `{"home":"real"}`})); err == nil {
		t.Errorf("create succeeded on a connection with no attestation — every write must name an author that exists (EV17)")
	}
	if efNoActor.calls["create"] != 0 {
		t.Errorf("the service was called %d time(s) without an actor; the door refuses before the service", efNoActor.calls["create"])
	}
}

// TestEnvironmentReadsTakeNoActor: the six reads are the plane's open half —
// they answer whoever is connected and need no author, because they change
// nothing. The writes above prove the other half; this pins that the reads were
// not accidentally built to demand a peer they do not need (which would make
// the panel unreadable from a connection the kernel cannot attest for).
func TestEnvironmentReadsTakeNoActor(t *testing.T) {
	ef := newEnvFake()
	tbl := environmentTable(t, ef, newRefFake(), "")
	reads := []struct {
		name MethodName
		raw  json.RawMessage
	}{
		{MethodEnvironmentList, nil},
		{MethodEnvironmentShow, mustJSON(t, EnvironmentShowParams{Slug: "host-scrubbed"})},
		{MethodEnvironmentEffective, mustJSON(t, EnvironmentEffectiveParams{})},
		{MethodEnvironmentVocabulary, nil},
		{MethodEnvironmentChanges, nil},
		{MethodEnvironmentProvisions, nil},
	}
	for _, r := range reads {
		if _, err := tbl.Dispatch(context.Background(), "c1", r.name, r.raw); err != nil {
			t.Errorf("%s: %v", r.name, err)
		}
	}
	if ef.calls["list"] != 1 || ef.calls["vocabulary"] != 1 {
		t.Errorf("the reads reached the service: %v", ef.calls)
	}
}

// TestEnvironmentRefusalKeepsTheStoresSentence: a refusal is ONE error with the
// store's own words in it (DB19, the verify shape) — a paraphrase would be a
// second authority over a fact the store owns, and the panel prints whatever
// the door says.
func TestEnvironmentRefusalKeepsTheStoresSentence(t *testing.T) {
	ef := newEnvFake()
	refusal := ErrEnvironmentRefused.WithWhat("the validator refused this spec (1 problem(s)): PATH may not be set by a spec")
	refusal.Data.Kind = "spec-invalid"
	ef.err = refusal
	tbl := environmentTable(t, ef, newRefFake(), "peer-1")
	_, err := tbl.Dispatch(context.Background(), "c1", MethodEnvironmentCreate,
		mustJSON(t, EnvironmentCreateParams{Slug: "ci", Name: "CI", Backend: "host-scrubbed", Spec: `{"home":"real"}`}))
	var perr *Error
	if !errors.As(err, &perr) {
		t.Fatalf("the refusal arrived as %T (%v), want the plane's one *protocol.Error", err, err)
	}
	if perr.Code != ErrEnvironmentRefused.Code {
		t.Errorf("code = %d, want %d (one code for the plane; the kind carries the shape)", perr.Code, ErrEnvironmentRefused.Code)
	}
	if !strings.Contains(perr.Data.What, "PATH may not be set by a spec") {
		t.Errorf("the refusal lost the validator's sentence: Data.What=%q message=%q", perr.Data.What, perr.Message)
	}
	// And the sentence survives the wire: the transport renders `data`, and a
	// panel prints what is IN it — a sentence that only lived in the Go value
	// would be a sentence nobody reads.
	blob, err := json.Marshal(perr)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(blob), "PATH may not be set by a spec") {
		t.Errorf("the marshalled refusal dropped the sentence: %s", blob)
	}
	if perr.Data.Kind != "spec-invalid" {
		t.Errorf("Data.Kind = %q, want the closed-set kind the validator spoke", perr.Data.Kind)
	}
}
