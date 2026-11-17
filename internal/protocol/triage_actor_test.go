package protocol

// S51 T1 (TM2/TM3) — the triage writes are the person's door at the socket:
// the actor is the CONNECTION's peer, filed_by is DERIVED from it, identity
// params are refused by key, and the one machine kind the audit plane
// admits (agent) appears only through the stamp — never through a param.
// The static contracts live in conformance/triage/file-no-actor.json and
// file-filer-param-refused.json; these are the live gates beside them, the
// roadmap_actor_test.go pattern.

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/triage"
)

// triageActorMount wires a REAL store behind the triage door so the write
// can land and its audit row can be read back — the stamping law is only
// proven when something could have been written.
func triageActorMount(t *testing.T, stamp func(context.Context, *Session) Actor) (*Table, *store.Store, string) {
	t.Helper()
	s, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("open the test store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	root := t.TempDir()
	if _, err := s.CreateComponent(store.NewComponent{Name: "r", Slug: "r", Root: root}); err != nil {
		t.Fatalf("seed the component: %v", err)
	}
	tbl := newTestTable(t)
	tbl.Triage = triage.New(s)
	if stamp != nil {
		tbl.ActorFor = stamp
	}
	return tbl, s, root
}

func TestTriageWritesRefuseIdentityParams(t *testing.T) {
	// Every write verb, every identity key — refused by KEY, before the
	// lenient decode, without an identity seam (the refusal must not
	// depend on whether the write COULD succeed). `filedBy` on every verb
	// is TM3's own claim: even a call that could not file refuses a filer.
	cases := []struct {
		name string
		call func(*Table) error
	}{
		{"file actor", func(tbl *Table) error {
			_, e := tbl.handleTriageFile(context.Background(), nil, json.RawMessage(`{"componentRoot":"/r","title":"t","body":"b","actor":"root"}`))
			return e
		}},
		{"file filedBy", func(tbl *Table) error {
			_, e := tbl.handleTriageFile(context.Background(), nil, json.RawMessage(`{"componentRoot":"/r","title":"t","body":"b","filedBy":"golang-engineer"}`))
			return e
		}},
		{"file actorKind", func(tbl *Table) error {
			_, e := tbl.handleTriageFile(context.Background(), nil, json.RawMessage(`{"componentRoot":"/r","title":"t","body":"b","actorKind":"agent"}`))
			return e
		}},
		{"set-priority actor", func(tbl *Table) error {
			_, e := tbl.handleTriageSetPriority(context.Background(), nil, json.RawMessage(`{"id":1,"componentRoot":"/r","state":"known","actor":"root"}`))
			return e
		}},
		{"set-priority filedBy", func(tbl *Table) error {
			_, e := tbl.handleTriageSetPriority(context.Background(), nil, json.RawMessage(`{"id":1,"componentRoot":"/r","state":"known","filedBy":"person"}`))
			return e
		}},
		{"confirm-duplicate actor", func(tbl *Table) error {
			_, e := tbl.handleTriageConfirmDuplicate(context.Background(), nil, json.RawMessage(`{"id":1,"componentRoot":"/r","of":2,"confirmedBy":"cj","actor":"root"}`))
			return e
		}},
		{"validate actorKind", func(tbl *Table) error {
			_, e := tbl.handleTriageValidate(context.Background(), nil, json.RawMessage(`{"id":1,"actorKind":"user"}`))
			return e
		}},
		{"handoff filedBy", func(tbl *Table) error {
			_, e := tbl.handleTriageHandoff(context.Background(), nil, json.RawMessage(`{"id":1,"filedBy":"person"}`))
			return e
		}},
	}
	tbl, _, _ := triageActorMount(t, nil)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call(tbl)
			var bad ErrBadParams
			if !errors.As(err, &bad) {
				t.Fatalf("%s with an identity param: err = %v, want ErrBadParams", tc.name, err)
			}
			msg := err.Error()
			key := tc.name[strings.LastIndex(tc.name, " ")+1:]
			for _, want := range []string{"refuses the param", `"` + key + `"`} {
				if !strings.Contains(msg, want) {
					t.Fatalf("refusal %q does not say %s", msg, want)
				}
			}
		})
	}
}

func TestTriageWritesRefuseAnUnstampedConnection(t *testing.T) {
	// No ActorFor seam at all — the honest absence: ErrTriageNoActor,
	// nothing moved, before the door reads a row (conformance
	// file-no-actor.json is the static twin).
	tbl, s, root := triageActorMount(t, nil)
	seed, ref := tbl.Triage.Create(root, triage.NewBug{Title: "seed", Body: "b", FiledBy: "person"})
	if ref != nil {
		t.Fatalf("seed the bug: %s", ref.Reason)
	}
	cases := []struct {
		name string
		call func() error
	}{
		{"file", func() error {
			_, e := tbl.handleTriageFile(context.Background(), nil, json.RawMessage(`{"componentRoot":"`+root+`","title":"t","body":"b"}`))
			return e
		}},
		{"set-priority", func() error {
			_, e := tbl.handleTriageSetPriority(context.Background(), nil, json.RawMessage(`{"id":`+itoaP(seed.ID)+`,"componentRoot":"`+root+`","state":"known"}`))
			return e
		}},
		{"confirm-duplicate", func() error {
			_, e := tbl.handleTriageConfirmDuplicate(context.Background(), nil, json.RawMessage(`{"id":`+itoaP(seed.ID)+`,"componentRoot":"`+root+`","of":9,"confirmedBy":"cj"}`))
			return e
		}},
		{"validate", func() error {
			tbl.TriageValidate = func(context.Context, TriageValidateParams) (any, error) {
				return map[string]any{"dispatched": true}, nil
			}
			_, e := tbl.handleTriageValidate(context.Background(), nil, json.RawMessage(`{"id":`+itoaP(seed.ID)+`}`))
			return e
		}},
		{"handoff", func() error {
			tbl.TriageHandoff = func(context.Context, triage.Actor, TriageHandoffParams) (any, error) {
				return map[string]any{"ok": true}, nil
			}
			_, e := tbl.handleTriageHandoff(context.Background(), nil, json.RawMessage(`{"id":`+itoaP(seed.ID)+`}`))
			return e
		}},
	}
	for _, tc := range cases {
		err := tc.call()
		var ae *Error
		if !errors.As(err, &ae) || ae.Code != ErrTriageNoActor.Code {
			t.Fatalf("%s on an unstamped connection: err = %v, want %d", tc.name, err, ErrTriageNoActor.Code)
		}
	}
	// Nothing moved and nothing audited — the refusal is before the pair.
	if rows, err := s.BugChangeList(context.Background(), seed.ID, 50); err != nil || len(rows) != 0 {
		t.Fatalf("an unstamped refusal left %d audit rows (err %v)", len(rows), err)
	}
	// uid 0 is the same honest absence in the other direction.
	tbl.ActorFor = func(context.Context, *Session) Actor { return Actor{Kind: "user", ID: "0"} }
	if _, err := tbl.handleTriageFile(context.Background(), nil, json.RawMessage(`{"componentRoot":"`+root+`","title":"t","body":"b"}`)); err == nil || !strings.Contains(err.Error(), "could not name its author") {
		t.Fatalf("uid 0 filed a bug: %v", err)
	}
	// A kind no lawful triage author wears.
	tbl.ActorFor = func(context.Context, *Session) Actor { return Actor{Kind: "robot", ID: "9"} }
	if _, err := tbl.handleTriageFile(context.Background(), nil, json.RawMessage(`{"componentRoot":"`+root+`","title":"t","body":"b"}`)); err == nil {
		t.Fatal("an actor kind outside person/cli/agent was accepted")
	}
}

func TestFileDerivesFiledByFromTheActorAndRejectsAParam(t *testing.T) {
	// TM3 in both directions: the person at the socket files as 'person',
	// and the stamped AGENT twin files under its own slug — the two and
	// only two filed_by shapes the wire can now produce.
	tbl, s, root := triageActorMount(t, userActorFor("peer-1"))
	raw, err := tbl.handleTriageFile(context.Background(), nil, json.RawMessage(`{"componentRoot":"`+root+`","title":"human claim","body":"b"}`))
	if err != nil {
		t.Fatalf("the person's file: %v", err)
	}
	view := raw.(*TriageView)
	if view.FiledBy != "person" {
		t.Fatalf("the person's filing carries filed_by %q — TM3 derives 'person'", view.FiledBy)
	}
	// …and the param that would have overridden it is refused by KEY (the
	// full census is TestTriageWritesRefuseIdentityParams above).
	if _, err := tbl.handleTriageFile(context.Background(), nil, json.RawMessage(`{"componentRoot":"`+root+`","title":"t","body":"b","filedBy":"nobody"}`)); err == nil {
		t.Fatal("a filedBy param sailed through after the derivation")
	}
	rows, _ := s.BugChangeList(context.Background(), view.ID, 50)
	if len(rows) != 1 || rows[0].ActorKind != "user" || rows[0].ActorID != "peer-1" || rows[0].Op != "file" {
		t.Fatalf("the person's file row: %+v", rows)
	}

	tbl.ActorFor = func(context.Context, *Session) Actor { return Actor{Kind: "agent", ID: "golang-engineer"} }
	raw, err = tbl.handleTriageFile(context.Background(), nil, json.RawMessage(`{"componentRoot":"`+root+`","title":"agent claim","body":"b"}`))
	if err != nil {
		t.Fatalf("the agent twin's file: %v", err)
	}
	view = raw.(*TriageView)
	if view.FiledBy != "golang-engineer" {
		t.Fatalf("the agent's filing carries filed_by %q — D7 files under the agent's own slug", view.FiledBy)
	}
	rows, _ = s.BugChangeList(context.Background(), view.ID, 50)
	if len(rows) != 1 || rows[0].ActorKind != "agent" || rows[0].ActorID != "golang-engineer" || rows[0].Op != "file" {
		t.Fatalf("the agent's file row: %+v", rows)
	}
	// The same agent actor may NOT set priority — the refusal is the
	// store's law spoken through the door (TM6, second line of defense).
	if _, err := tbl.handleTriageSetPriority(context.Background(), nil, json.RawMessage(`{"id":`+itoaP(view.ID)+`,"componentRoot":"`+root+`","state":"known"}`)); err == nil {
		t.Fatal("the agent set a priority through the socket — TM6 has a hole at the top door")
	} else if ae, ok := err.(*Error); !ok || !strings.Contains(ae.Data.What, "agent") {
		t.Fatalf("the agent's priority refusal does not name the kind: %v", err)
	}
}

func TestTriageValidateAuditsAfterTheDispatch(t *testing.T) {
	// The one audit outside a pair, at the door that dispatches: seam
	// success earns the row, seam refusal earns NOTHING — a dispatch that
	// never happened must not read in the history as if it had.
	tbl, s, root := triageActorMount(t, userActorFor("peer-1"))
	b, ref := tbl.Triage.CreateAudited(root, triage.NewBug{Title: "attempt", Body: "b", FiledBy: "person"}, triage.Actor{Kind: "user", ID: "peer-1"})
	if ref != nil {
		t.Fatalf("create: %s", ref.Reason)
	}
	tbl.TriageValidate = func(context.Context, TriageValidateParams) (any, error) {
		return nil, errors.New("the validation seam refused the dispatch")
	}
	if _, err := tbl.handleTriageValidate(context.Background(), nil, json.RawMessage(`{"id":`+itoaP(b.ID)+`}`)); err == nil {
		t.Fatal("a refused dispatch answered success")
	}
	if rows, _ := s.BugChangeList(context.Background(), b.ID, 50); len(rows) != 1 { // still just the file row
		t.Fatalf("a refused dispatch appended audit rows: %+v", rows)
	}
	tbl.TriageValidate = func(context.Context, TriageValidateParams) (any, error) {
		return map[string]any{"dispatched": true}, nil
	}
	if _, err := tbl.handleTriageValidate(context.Background(), nil, json.RawMessage(`{"id":`+itoaP(b.ID)+`}`)); err != nil {
		t.Fatalf("the dispatch: %v", err)
	}
	rows, _ := s.BugChangeList(context.Background(), b.ID, 50)
	if len(rows) != 2 || rows[0].Op != "validate" || rows[0].ActorID != "peer-1" {
		t.Fatalf("after the dispatch: %+v", rows)
	}
	if rows[0].Summary != "started a validation attempt on bug "+itoaP(b.ID) {
		t.Fatalf("summary %q — the fixed template", rows[0].Summary)
	}
}

func TestTriageHandoffCarriesTheStampedActorIntoTheSeam(t *testing.T) {
	// The seam signature IS the law: the daemon's handoff closure receives
	// the stamped actor, so RecordHandoffAudited below it cannot run
	// anonymous (the daemon-side behaviour is pinned in internal/daemon).
	tbl, _, _ := triageActorMount(t, userActorFor("peer-1"))
	var got triage.Actor
	tbl.TriageHandoff = func(_ context.Context, a triage.Actor, _ TriageHandoffParams) (any, error) {
		got = a
		return map[string]any{"ok": true}, nil
	}
	if _, err := tbl.handleTriageHandoff(context.Background(), nil, json.RawMessage(`{"id":3}`)); err != nil {
		t.Fatalf("handoff: %v", err)
	}
	if got.Kind != "user" || got.ID != "peer-1" {
		t.Fatalf("the seam saw actor %s/%s — the handler must pass the stamped peer", got.Kind, got.ID)
	}
}

// itoaP renders an id into the wire JSON of the calls above.
func itoaP(n int64) string { return strconv.FormatInt(n, 10) }
