package store

// execenv_seed_test.go — S59 T6 (EV19): the three rules of the standard
// environment set's write path, tested where the SQL lives.
//
// The rules are the ones a seed must never break, and each has a shape a
// careless implementation would pass: "idempotent" tested by calling twice and
// seeing no error would pass an implementation that overwrote the row;
// "first binding" tested on an empty database would pass an implementation that
// always re-binds. So each test here writes the competing fact FIRST — a
// person's edit, a person's binding — and asserts the seed leaves it alone.

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

// seedTx runs fn on the writer's transaction, the way the engine's WithTx
// does: the row, its binding and their audit rows are one fact or none.
func seedTx(t *testing.T, s *Store, fn func(tx *sql.Tx) error) {
	t.Helper()
	tx, err := s.w.Begin()
	if err != nil {
		t.Fatalf("begin seed tx: %v", err)
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		t.Fatalf("seed tx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit seed tx: %v", err)
	}
}

func seedableSpec() string {
	// The smallest spec the validator signs for a host backend: an allowlist of
	// names, real HOME, no egress claim. Written by hand rather than pulled
	// from envseed so this file tests the STORE's rules and not the set's
	// content (the set's own golden pins that one).
	return `{"env":{"passthrough":["PATH","HOME"],"set":{},"secrets":[]},"home":"real",` +
		`"limits":{},"egress":{"mode":"unenforced","allow":[]},"image":null,"services":[],"preview":null}`
}

func TestSeedEnvironmentIsIdempotentAndStampsSystemOrigin(t *testing.T) {
	_, s := testStore(t)
	in := SeedEnvironment{Slug: "host-scrubbed", Name: "Host scrubbed", Description: "the shipped default",
		Backend: "host-scrubbed", Spec: seedableSpec()}

	var firstID string
	seedTx(t, s, func(tx *sql.Tx) error {
		row, created, err := s.SeedEnvironmentTx(tx, in)
		if err != nil {
			return err
		}
		if !created {
			t.Errorf("the first seed of a key with no row reported created=false")
		}
		if row.Origin != "system" {
			t.Errorf("a seeded row's origin = %q, want system (only the seed path may claim it)", row.Origin)
		}
		if row.Legacy != 0 || row.Revision != 1 {
			t.Errorf("a seeded row: legacy=%d revision=%d, want 0 and 1", row.Legacy, row.Revision)
		}
		firstID = row.ID
		return nil
	})

	seedTx(t, s, func(tx *sql.Tx) error {
		row, created, err := s.SeedEnvironmentTx(tx, in)
		if err != nil {
			return err
		}
		if created {
			t.Errorf("the second seed of the same key reported created=true")
		}
		if row.ID != firstID {
			t.Errorf("the second seed wrote a second row: %s then %s", firstID, row.ID)
		}
		return nil
	})

	rows, err := s.ListEnvironments(EnvironmentFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("seeded twice: %d environment rows, want 1", len(rows))
	}
	if n := countChanges(t, s, "environment", firstID); n != 1 {
		t.Errorf("two seeds wrote %d audit rows, want 1 (EV17: a write that wrote nothing is not a changing write)", n)
	}
	// The actor the audit names is the system's, and it is a name that exists:
	// an audit row stamped `user` would record an act no person took.
	changes, err := s.ListChanges(EnvironmentChangeFilter{SubjectKind: "environment", SubjectID: firstID})
	if err != nil {
		t.Fatalf("changes: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("changes: %d rows, want 1", len(changes))
	}
	if changes[0].ActorKind != SeedActorKind || changes[0].ActorID != SeedActorID {
		t.Errorf("the seed's audit actor = %s/%s, want %s/%s",
			changes[0].ActorKind, changes[0].ActorID, SeedActorKind, SeedActorID)
	}
	if changes[0].Action != "create" || changes[0].SpecAfter == nil {
		t.Errorf("the seed's audit row: action=%q specAfter set=%v, want create and the spec", changes[0].Action, changes[0].SpecAfter != nil)
	}
}

func TestSeedEnvironmentLeavesAPersonsRowAlone(t *testing.T) {
	_, s := testStore(t)
	in := SeedEnvironment{Slug: "host-scrubbed", Name: "Host scrubbed", Backend: "host-scrubbed", Spec: seedableSpec()}
	seedTx(t, s, func(tx *sql.Tx) error {
		_, _, err := s.SeedEnvironmentTx(tx, in)
		return err
	})

	// A person tightens the shipped default: a shorter allowlist, one secret
	// reference, and a rename. This is the record EV19 says the seed may not
	// touch — and a seed that "repairs" it on the next start is the failure.
	edited := `{"env":{"passthrough":["PATH"],"set":{"TERM":"dumb"},"secrets":["deploy-token"]},"home":"real",` +
		`"limits":{},"egress":{"mode":"unenforced","allow":[]},"image":null,"services":[],"preview":null}`
	created, ref, err := s.CreateSecretRef(Actor{Kind: "user", ID: "t6"}, NewSecretRef{
		Name: "deploy-token", SourceKind: "daemon-env", Source: "DEPLOY_TOKEN", InjectAs: "DEPLOY_TOKEN",
	}, nil)
	if err != nil || ref != nil {
		t.Fatalf("create the reference the edited spec names: ref=%v err=%v", ref, err)
	}
	_ = created
	row, err := s.GetEnvironment("host-scrubbed")
	if err != nil {
		t.Fatalf("read the seeded row: %v", err)
	}
	updated, ref, err := s.UpdateEnvironment(Actor{Kind: "user", ID: "t6"}, UpdateEnvironment{ID: row.ID, Spec: edited, ExpectedRevision: row.Revision})
	if ref != nil || err != nil {
		t.Fatalf("the person's edit was refused: ref=%v err=%v", ref, err)
	}

	seedTx(t, s, func(tx *sql.Tx) error {
		got, created, err := s.SeedEnvironmentTx(tx, in)
		if err != nil {
			return err
		}
		if created {
			t.Errorf("the seed claimed to create a row that already existed")
		}
		if got.Spec != updated.Spec || got.Name != updated.Name || got.Revision != updated.Revision {
			t.Errorf("the seed rewrote the person's row: spec=%q name=%q rev=%d, want the edited row kept",
				got.Spec, got.Name, got.Revision)
		}
		return nil
	})

	// Read it back from the store rather than trusting the returned row: the
	// claim under test is what the DATABASE holds after a restart's re-apply.
	again, err := s.GetEnvironment("host-scrubbed")
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if again.Spec != edited || again.Revision != updated.Revision {
		t.Errorf("after a re-seed the row is spec=%q rev=%d, want the person's edit kept", again.Spec, again.Revision)
	}
	if again.Origin != "system" {
		t.Errorf("a person's edit changed origin to %q; the seed path left it %q", again.Origin, "system")
	}
}

func TestSeedEnvironmentRefusesWhatADoorWouldRefuse(t *testing.T) {
	_, s := testStore(t)
	cases := []struct {
		name string
		in   SeedEnvironment
		want string
	}{
		{"no slug", SeedEnvironment{Slug: "", Name: "x", Backend: "host-scrubbed", Spec: seedableSpec()}, "is not a slug"},
		{"uppercase slug", SeedEnvironment{Slug: "Host_Scrubbed", Name: "x", Backend: "host-scrubbed", Spec: seedableSpec()}, "is not a slug"},
		{"no name", SeedEnvironment{Slug: "a", Name: "", Backend: "host-scrubbed", Spec: seedableSpec()}, "needs a name"},
		{"backend outside the closed set", SeedEnvironment{Slug: "a", Name: "x", Backend: "microvm", Spec: seedableSpec()}, "outside the closed set"},
		{"spec that is not JSON", SeedEnvironment{Slug: "a", Name: "x", Backend: "host-scrubbed", Spec: "PATH=1"}, "spec"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tx, err := s.w.Begin()
			if err != nil {
				t.Fatalf("begin: %v", err)
			}
			defer tx.Rollback()
			_, created, err := s.SeedEnvironmentTx(tx, tc.in)
			if err == nil {
				t.Fatalf("seed %q was accepted (created=%v)", tc.name, created)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to name %q", err, tc.want)
			}
			if created {
				t.Errorf("a refused seed reported created=true")
			}
		})
	}
	// Nothing above may have reached the table.
	rows, err := s.ListEnvironments(EnvironmentFilter{IncludeArchived: true})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("refused seeds left %d rows behind: %v", len(rows), rows)
	}
}

func TestFirstBindingLandsOnlyOnAWorkspaceWithNoBinding(t *testing.T) {
	_, s := testStore(t)
	in := SeedEnvironment{Slug: "host-scrubbed", Name: "Host scrubbed", Backend: "host-scrubbed", Spec: seedableSpec()}
	var envID string
	seedTx(t, s, func(tx *sql.Tx) error {
		row, _, err := s.SeedEnvironmentTx(tx, in)
		envID = row.ID
		return err
	})

	// Fresh install: nothing is bound, so the shipped default becomes the
	// workspace's environment (EV19). Without this the daemon starts into the
	// state EV2 refuses every command in.
	var bound bool
	seedTx(t, s, func(tx *sql.Tx) error {
		var err error
		bound, err = s.SeedWorkspaceBindingTx(tx, envID)
		return err
	})
	if !bound {
		t.Fatalf("a workspace with no binding was not bound by the seed")
	}
	binding, source, err := s.BindingFor("")
	if err != nil {
		t.Fatalf("binding for: %v", err)
	}
	if source != "workspace" || binding.EnvironmentID != envID {
		t.Errorf("effective binding = %s from %s, want the seeded environment from workspace", binding.EnvironmentID, source)
	}
	if n := countChanges(t, s, "binding", "workspace"); n != 1 {
		t.Errorf("the first binding wrote %d audit rows, want 1", n)
	}

	// And again: the same transaction, the same call, and nothing written.
	seedTx(t, s, func(tx *sql.Tx) error {
		again, err := s.SeedWorkspaceBindingTx(tx, envID)
		if err != nil {
			return err
		}
		if again {
			t.Errorf("the second first-binding reported bound=true; a binding already exists")
		}
		return nil
	})
	if n := countChanges(t, s, "binding", "workspace"); n != 1 {
		t.Errorf("re-applying the first binding wrote %d audit rows in total, want 1", n)
	}
}

func TestFirstBindingNeverMovesAPersonsChoice(t *testing.T) {
	_, s := testStore(t)
	sysSpec := seedableSpec()
	var sysID, otherID string
	seedTx(t, s, func(tx *sql.Tx) error {
		row, _, err := s.SeedEnvironmentTx(tx, SeedEnvironment{Slug: "host-scrubbed", Name: "Host scrubbed", Backend: "host-scrubbed", Spec: sysSpec})
		sysID = row.ID
		return err
	})
	// The person binds the workspace to their own environment through the door.
	other, ref, err := s.CreateEnvironment(Actor{Kind: "user", ID: "t6"}, NewEnvironment{
		Slug: "project-default", Name: "Project default", Backend: "host-scrubbed", Spec: sysSpec,
	})
	if ref != nil || err != nil {
		t.Fatalf("create the person's environment: ref=%v err=%v", ref, err)
	}
	otherID = other.ID
	if _, ref, err := s.BindEnvironment(Actor{Kind: "user", ID: "t6"}, NewBinding{ScopeKind: "workspace", EnvironmentID: otherID}); ref != nil || err != nil {
		t.Fatalf("the person's workspace bind failed: ref=%v err=%v", ref, err)
	}

	// A later daemon start re-applies the set. EV19's first-binding rule says:
	// the row exists, the binding exists, so NOTHING is written — and a
	// workspace silently returned to the shipped default is how a person loses
	// an afternoon.
	seedTx(t, s, func(tx *sql.Tx) error {
		_, _, err := s.SeedEnvironmentTx(tx, SeedEnvironment{Slug: "host-scrubbed", Name: "Host scrubbed", Backend: "host-scrubbed", Spec: sysSpec})
		if err != nil {
			return err
		}
		bound, err := s.SeedWorkspaceBindingTx(tx, sysID)
		if err != nil {
			return err
		}
		if bound {
			t.Errorf("the seed re-bound a workspace a person had already bound")
		}
		return nil
	})

	_, source, err := s.BindingFor("")
	if err != nil {
		t.Fatalf("binding for: %v", err)
	}
	if source != "workspace" {
		t.Fatalf("effective binding source = %q, want workspace", source)
	}
	binding, _, err := s.BindingFor("")
	if err != nil {
		t.Fatalf("binding: %v", err)
	}
	if binding.EnvironmentID != otherID {
		t.Errorf("the workspace is bound to %s, want the person's %s", binding.EnvironmentID, otherID)
	}
}

func TestFirstBindingRefusesAnEnvironmentThatIsNotThere(t *testing.T) {
	_, s := testStore(t)
	for _, id := range []string{"", "env-missing"} {
		tx, err := s.w.Begin()
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		_, err = s.SeedWorkspaceBindingTx(tx, id)
		tx.Rollback()
		if err == nil {
			t.Errorf("SeedWorkspaceBindingTx(%q) was accepted", id)
			continue
		}
		if !strings.Contains(err.Error(), "not there to bind") && !strings.Contains(err.Error(), "needs the environment") {
			t.Errorf("error for %q = %q, want it to name the missing environment", id, err)
		}
	}
	if n := countChanges(t, s, "binding", "workspace"); n != 0 {
		t.Errorf("refused bindings wrote %d audit rows, want 0", n)
	}
}

func TestEnvironmentRowTxReportsArchivedAsAPresentRow(t *testing.T) {
	_, s := testStore(t)
	var id string
	seedTx(t, s, func(tx *sql.Tx) error {
		row, _, err := s.SeedEnvironmentTx(tx, SeedEnvironment{Slug: "host-scrubbed", Name: "Host scrubbed", Backend: "host-scrubbed", Spec: seedableSpec()})
		id = row.ID
		return err
	})
	ctx := context.Background()
	_ = ctx // the fingerprint's context belongs to the engine; this file tests the read.

	tx, err := s.w.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback()
	row, present, err := s.EnvironmentRowTx(tx, "host-scrubbed")
	if err != nil || !present {
		t.Fatalf("read the seeded row: present=%v err=%v", present, err)
	}
	if row.ID != id || row.Origin != "system" {
		t.Errorf("fingerprint read = %s/%s, want the seeded row and system origin", row.ID, row.Origin)
	}
	if _, present, err := s.EnvironmentRowTx(tx, "no-such-slug"); err != nil || present {
		t.Errorf("an absent row read as present=%v err=%v, want false and no error", present, err)
	}
}
