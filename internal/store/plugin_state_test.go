// plugin_state_test.go is the ledger side of migration 0023: what the
// DATABASE itself refuses — because the four-state story is only honest if
// revoked is terminal even for SQL that skips every Go verb. The domain
// side (the transition table, the verbs, the reason demand before it ever
// reaches here) lives in internal/plugin's state_test.go.
package store

import (
	"strings"
	"testing"
)

func openPluginStore(t *testing.T) *Store { return openTemp(t) }

func TestPluginLedgerShape(t *testing.T) {
	s := openPluginStore(t)
	for _, tbl := range []string{"plugin_agent", "plugin_transition", "plugin_refusal"} {
		var n int
		if err := s.r.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, tbl).Scan(&n); err != nil || n != 1 {
			t.Fatalf("table %s missing from the migrated database", tbl)
		}
	}
	for _, idx := range []string{"plugin_agent_live", "idx_plugin_transition_agent"} {
		var n int
		if err := s.r.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='index' AND name=?`, idx).Scan(&n); err != nil || n != 1 {
			t.Fatalf("index %s missing — one live row per name and the per-agent audit read are both structural", idx)
		}
	}
	var trig int
	if err := s.r.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='trigger' AND name='plugin_agent_no_exit_revoked'`).Scan(&trig); err != nil || trig != 1 {
		t.Fatal("the terminal trigger is missing — revoked must be terminal at the RECORD too")
	}
}

func TestPluginLiveUniqueness(t *testing.T) {
	s := openPluginStore(t)
	if err := s.CreatePluginAgent("groot-translator", "/packages/groot", "2026-10-25T10:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := s.CreatePluginAgent("groot-translator", "/packages/groot-again", "2026-10-25T10:01:00Z"); err == nil {
		t.Fatal("two live records with one name: the partial unique index must refuse")
	} else if !strings.Contains(err.Error(), "live") && err != ErrPluginRecordExists {
		t.Fatalf("the refusal should be the distinguishable one: %v", err)
	}
}

func TestPluginRevokedIsTerminalAtTheDatabase(t *testing.T) {
	s := openPluginStore(t)
	if err := s.CreatePluginAgent("once-ran", "/packages/once", "2026-10-25T10:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := s.TransitionPluginAgent("once-ran", "", "disabled", "", "2026-10-25T10:00:00Z"); err != nil {
		t.Fatal(err) // the import landing
	}
	if err := s.TransitionPluginAgent("once-ran", "disabled", "revoked", "vendor revoked its card", "2026-10-25T11:00:00Z"); err != nil {
		t.Fatal(err)
	}
	// Even raw SQL cannot resurrect the row: the trigger aborts.
	if _, err := s.w.Exec(`UPDATE plugin_agent SET state = 'enabled' WHERE name = 'once-ran'`); err == nil {
		t.Fatal("revoked is terminal AT THE DATABASE — a resurrection by hand-edited SQL is the exact thing AP4 says cannot happen")
	} else if !strings.Contains(err.Error(), "terminal") {
		t.Fatalf("the database names its own reason: %v", err)
	}
	// And the record SURVIVES the attempt:
	a, found, err := s.PluginAgentState("once-ran")
	if err != nil || !found || a.State != "revoked" {
		t.Fatalf("the revoked row persists: found=%v state=%+v err=%v", found, a, err)
	}
}

func TestPluginRevokeNeedsReasonAtTheDatabase(t *testing.T) {
	s := openPluginStore(t)
	if err := s.CreatePluginAgent("reasonless", "/packages/r", "2026-10-25T10:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := s.TransitionPluginAgent("reasonless", "", "disabled", "", "2026-10-25T10:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := s.TransitionPluginAgent("reasonless", "disabled", "revoked", "  ", "2026-10-25T11:00:00Z"); err == nil {
		t.Fatal("a reasonless revoke must be refused")
	}
	// Raw too — the CHECK, not the Go verb, gets the last word:
	if _, err := s.w.Exec(`INSERT INTO plugin_transition (agent, from_state, to_state, reason, at) VALUES ('reasonless','disabled','revoked','','2026-10-25T11:00:00Z')`); err == nil {
		t.Fatal("\"never again\" without a why is UNWRITABLE — a convention would eventually be skipped")
	}
	// Still disabled, no ledger for a revoke that never was:
	a, found, err := s.PluginAgentState("reasonless")
	if err != nil || !found || a.State != "disabled" {
		t.Fatalf("the failed revoke left nothing: %+v found=%v err=%v", a, found, err)
	}
}

func TestPluginTransitionIsAtomicAndGuarded(t *testing.T) {
	s := openPluginStore(t)
	if err := s.CreatePluginAgent("moved-under", "/packages/m", "2026-10-25T10:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := s.TransitionPluginAgent("moved-under", "", "disabled", "", "2026-10-25T10:00:00Z"); err != nil {
		t.Fatal(err)
	}
	// The caller claims the wrong FROM: refused, state unmoved, no row.
	if err := s.TransitionPluginAgent("moved-under", "enabled", "disabled", "", "2026-10-25T10:05:00Z"); err == nil {
		t.Fatal("a transition from a state the record is NOT in must refuse")
	}
	ts, err := s.PluginAgentTransitions("moved-under")
	if err != nil {
		t.Fatal(err)
	}
	if len(ts) != 1 || ts[0].ToState != "disabled" {
		t.Fatalf("the refused transition recorded NOTHING: %+v", ts)
	}
}

func TestPluginFullLifeIsReadable(t *testing.T) {
	s := openPluginStore(t)
	if err := s.CreatePluginAgent("audited", "/packages/a", "2026-10-25T10:00:00Z"); err != nil {
		t.Fatal(err)
	}
	steps := []struct{ from, to, reason, at string }{
		{"", "disabled", "", "2026-10-25T10:00:00Z"},
		{"disabled", "enabled", "", "2026-10-25T10:05:00Z"},
		{"enabled", "disabled", "", "2026-10-25T10:40:00Z"},
		{"disabled", "revoked", "the vendor shipped a data-exfil update", "2026-10-25T11:00:00Z"},
	}
	for _, st := range steps {
		if err := s.TransitionPluginAgent("audited", st.from, st.to, st.reason, st.at); err != nil {
			t.Fatalf("%s→%s: %v", st.from, st.to, err)
		}
	}
	ts, err := s.PluginAgentTransitions("audited")
	if err != nil {
		t.Fatal(err)
	}
	if len(ts) != 4 {
		t.Fatalf("every transition is recorded — four moves, four rows, got %d", len(ts))
	}
	if ts[3].ToState != "revoked" || ts[3].Reason != steps[3].reason || ts[3].At != "2026-10-25T11:00:00Z" {
		t.Fatalf("the WHY and the WHEN survive with the WHAT: %+v", ts[3])
	}
	// Re-import after revocation: the OLD life stays readable and a NEW
	// live record is writable — "somebody put this back" is VISIBLE.
	if err := s.CreatePluginAgent("audited", "/packages/a-v2", "2026-10-25T12:00:00Z"); err != nil {
		t.Fatalf("re-import after revoke creates a NEW record, it does not resurrect: %v", err)
	}
	a, found, err := s.PluginAgentState("audited")
	if err != nil || !found || a.State != "disabled" || a.Source != "/packages/a-v2" {
		t.Fatalf("the new life reads live, the old stays history: %+v err=%v", a, err)
	}
	if ts, err := s.PluginAgentTransitions("audited"); err != nil || len(ts) != 4 {
		t.Fatalf("the old life's rows survive under the new record: %d rows err=%v", len(ts), err)
	}
}
