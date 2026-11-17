// The two halves agreeing: plugin's fake-free lifecycle against the REAL
// migration-0023 rows, opened exactly the way agentsvc's tests open a store
// (store.Open at a t.TempDir — never the real one). If the fake and the
// database ever disagree about what revoked means, this is where it shows.
package plugin

import (
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/store"
)

func openBindingStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("opening the temp store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestStateStoreAgreesWithTheDomain(t *testing.T) {
	st := openBindingStore(t)
	BindStateStore(&StoreBinding{S: st})
	t.Cleanup(func() { BindStateStore(nil); Reset() })

	dir := writePackage(t, "durable-life", "", nil, nil)
	_, rj := ImportChecked(dir, nil, stateAt)
	if rj != nil {
		t.Fatal(rj.Error())
	}
	// The durable record says disabled, and its landing is on the ledger.
	row, found, err := st.PluginAgentState("durable-life")
	if err != nil || !found || row.State != "disabled" {
		t.Fatalf("import wrote a real record: found=%v row=%+v err=%v", found, row, err)
	}
	if _, rj := Enable("durable-life"); rj != nil {
		t.Fatal(rj.Error())
	}
	if _, rj := Disable("durable-life"); rj != nil {
		t.Fatal(rj.Error())
	}
	if _, rj := Revoke("durable-life", "the vendor's card was withdrawn upstream"); rj != nil {
		t.Fatal(rj.Error())
	}
	row, _, err = st.PluginAgentState("durable-life")
	if err != nil || row.State != "revoked" {
		t.Fatalf("the database agrees it is revoked: %+v err=%v", row, err)
	}
	ts, err := st.PluginAgentTransitions("durable-life")
	if err != nil {
		t.Fatal(err)
	}
	if len(ts) != 4 || ts[3].Reason != "the vendor's card was withdrawn upstream" {
		t.Fatalf("the whole life is in the rows: %+v err=%v", ts, err)
	}
	// Revoked is terminal here too — the verbs cannot resurrect it:
	if _, rj := Enable("durable-life"); rj == nil {
		t.Fatal("revoked is terminal with the real store bound")
	}
	// And the durable read is the STATE the verbs consult (not the stale
	// in-memory copy): after revoke, the in-memory agent must read revoked.
	if cur, ok := Lookup("durable-life"); !ok || cur.State != StateRevoked {
		t.Fatalf("in-memory copy follows the durable record: %+v ok=%v", cur, ok)
	}
	// Re-import after revoke: NEW record, old rows intact.
	dir2 := writePackage(t, "durable-life", "", nil, nil)
	a2, rj := ImportChecked(dir2, nil, time.Now().UTC())
	if rj != nil {
		t.Fatalf("re-import after revoke must work (new record): %s", rj.Error())
	}
	if a2.State != StateDisabled {
		t.Fatalf("arrives disabled again: %q", a2.State)
	}
	ts, err = st.PluginAgentTransitions("durable-life")
	if err != nil || len(ts) != 5 {
		t.Fatalf("old life (4) + new landing (1): %d rows err=%v", len(ts), err)
	}
}

func TestPersistedStatesMatchTheMigration(t *testing.T) {
	// One vocabulary, two homes — plugin's closed type and the store's
	// CHECK mirror — with a test that notices drift (the S14 status list
	// pattern): the strings in migration 0023's state CHECK are pinned to
	// BOTH lists here and in the store's own ledger tests.
	ps := PersistedStates()
	if len(ps) != len(store.PluginAgentStates) {
		t.Fatalf("vocabulary drift: plugin %v vs store %v", ps, store.PluginAgentStates)
	}
	for i, s := range ps {
		if string(s) != store.PluginAgentStates[i] {
			t.Fatalf("position %d: plugin %q store %q", i, s, store.PluginAgentStates[i])
		}
	}
}
