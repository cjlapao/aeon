package store

// execenv_schema_test.go — S59 T2's schema half: the six tables exist with the
// shape the design argued for, and the two properties no Go code can be trusted
// to keep are enforced in SQL.
//
// Each test here fails on a state that is silently wrong in production:
// a value column that could hold a credential, an audit row somebody edited, a
// second workspace binding that makes "what does this run in" undefined, or the
// permissive legacy row shipped to a fresh install.

import (
	"database/sql"
	"strings"
	"testing"
)

// execUser is the person behind the socket for these writes (EV18's actor
// discipline). A separate value from s57User so neither slice's edit of its own
// helper can move the other's assertions.
var execUser = Actor{Kind: "user", ID: "carlos"}

// execSpecJSON is a legal environment spec (EV4's shape is execenv's business;
// the store only requires valid JSON — environmentSpecRefusal).
const execSpecJSON = `{"env":{"passthrough":["PATH","HOME"],"set":{"CI":"1"},"secrets":["db-url"]},` +
	`"home":"real","limits":{"wallSeconds":120},"egress":{"mode":"unenforced"},"image":null,` +
	`"services":[],"preview":null}`

// execSpecBare is the same with no secret references, for the cases where the
// reference would only be noise.
const execSpecBare = `{"env":{"passthrough":["PATH"],"set":{},"secrets":[]},"home":"real",` +
	`"limits":{},"egress":{"mode":"unenforced"},"image":null,"services":[],"preview":null}`

func TestMigrationCreatesTheSixTables(t *testing.T) {
	_, s := testStore(t)
	want := []string{
		"environment", "environment_binding", "secret_ref",
		"step_environment", "environment_provision", "environment_change",
	}
	for _, name := range want {
		var got string
		err := s.r.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&got)
		if err == sql.ErrNoRows {
			t.Errorf("table %s does not exist — migration 20261103063823 did not create it", name)
			continue
		}
		if err != nil {
			t.Fatalf("read the schema for %s: %v", name, err)
		}
		if got != name {
			t.Errorf("table %s came back as %q", name, got)
		}
	}

	// The two partial unique indexes EV2 depends on, and the ledger's settle-once
	// index: without them "one workspace binding" and "one live resource per
	// owner" are conventions, and a convention is what a second insert breaks.
	for _, idx := range []struct{ name, mustContain string }{
		{"environment_binding_workspace", "WHERE scope_kind = 'workspace'"},
		{"environment_binding_component", "WHERE component_id IS NOT NULL"},
		{"environment_provision_live", "WHERE removed_at IS NULL"},
	} {
		var sqlText string
		if err := s.r.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'index' AND name = ?`, idx.name).
			Scan(&sqlText); err != nil {
			t.Errorf("index %s is missing: %v", idx.name, err)
			continue
		}
		if !strings.Contains(sqlText, idx.mustContain) {
			t.Errorf("index %s is not partial — %q is absent, so the constraint it exists to state is not stated:\n%s",
				idx.name, idx.mustContain, sqlText)
		}
	}
}

func TestLegacyRowOnlyOnAnUpgradedStore(t *testing.T) {
	// EV20's fork, both branches. A fresh install must NOT get the
	// inherit-everything row — shipping it would defeat the slice — and a store
	// that already has components must get it, or every attended loop on an
	// upgraded box changes behaviour overnight.

	t.Run("fresh store gets neither", func(t *testing.T) {
		_, s := testStore(t)
		var n int
		if err := s.r.QueryRow(`SELECT COUNT(*) FROM environment WHERE id = 'env-host-inherit'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("a fresh store shipped the inherit-everything row; EV19 binds host-scrubbed there instead")
		}
		if err := s.r.QueryRow(`SELECT COUNT(*) FROM environment_binding`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("a fresh store got %d workspace binding(s) it did not ask for", n)
		}
	})

	t.Run("a store with a component gets both", func(t *testing.T) {
		_, s := testStore(t)
		if w, err := s.CreateComponent(NewComponent{Name: "repo", Slug: "repo", Root: "/r/repo"}); err != nil || !w.OK() {
			t.Fatalf("seed a component: %v %+v", err, w)
		}
		// Re-apply the migration's conditional half the way the runner would on
		// an upgrade: the same SQL text from the same file, not a copy a test
		// typed. A test that re-types the INSERTs proves nothing about the file.
		body := execMigrationInserts(t)
		if _, err := s.w.Exec(body); err != nil {
			t.Fatalf("re-apply the conditional inserts: %v", err)
		}
		var (
			id      string
			backend string
			legacy  int
		)
		if err := s.r.QueryRow(`SELECT id, backend, legacy FROM environment WHERE slug = 'host-inherit'`).
			Scan(&id, &backend, &legacy); err != nil {
			t.Fatalf("the upgraded store did not get the legacy row: %v", err)
		}
		if id != "env-host-inherit" || backend != "host-inherit" || legacy != 1 {
			t.Errorf("the legacy row is wrong: id=%s backend=%s legacy=%d", id, backend, legacy)
		}
		var bound string
		if err := s.r.QueryRow(`SELECT environment_id FROM environment_binding WHERE scope_kind = 'workspace'`).
			Scan(&bound); err != nil {
			t.Fatalf("the upgraded store has no workspace binding: %v", err)
		}
		if bound != "env-host-inherit" {
			t.Errorf("the workspace bound %q, want the legacy row it had before S59", bound)
		}
	})

	t.Run("a store with a loop but no component still gets it", func(t *testing.T) {
		_, s := testStore(t)
		// EV20's condition is `EXISTS (component) OR EXISTS (loop)`. The second
		// half is reachable: `loop` is keyed by (project_root, slug) and carries
		// NO component_id, so a store can hold a loop and no component row.
		// Through the store's own writer: `loop` is keyed by (component_root,
		// slug) and carries no component_id, so this is a real state and not a
		// hand-poked row that skips a trigger.
		if _, err := s.CreateLoop(NewLoop{Slug: "legacy", Title: "legacy", Goal: "keep the row honest",
			Root: "/r/only", ComponentRoot: "/r/only", Mode: "solo", Phase: "plan", Status: "idle"}); err != nil {
			t.Fatalf("seed a loop with no component: %v", err)
		}
		if _, err := s.w.Exec(execMigrationInserts(t)); err != nil {
			t.Fatalf("re-apply the conditional inserts: %v", err)
		}
		var n int
		if err := s.r.QueryRow(`SELECT COUNT(*) FROM environment WHERE id = 'env-host-inherit'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("a store with only a loop got %d legacy rows, want 1 (EV20 preserves continuity for "+
				"either kind of existing install)", n)
		}
	})
}

func execMigrationInserts(t *testing.T) string {
	t.Helper()
	for _, m := range Migrations() {
		if m.ID != 20261103063823 {
			continue
		}
		i := strings.Index(m.SQL, "INSERT INTO environment (")
		if i < 0 {
			t.Fatal("the S59 migration has no `INSERT INTO environment (` — where did the conditional rows go?")
		}
		return m.SQL[i:]
	}
	t.Fatal("the S59 migration is not in Migrations(); the file is not being applied at all")
	return ""
}

func TestWorkspaceBindingIsUnique(t *testing.T) {
	_, s := testStore(t)
	if _, _, err := s.CreateSecretRef(execUser, NewSecretRef{
		Name: "db-url", SourceKind: "daemon-env", Source: "DATABASE_URL", InjectAs: "DATABASE_URL",
	}, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	env, ref, err := s.CreateEnvironment(execUser, NewEnvironment{
		Slug: "scrubbed", Name: "Scrubbed", Backend: "host-scrubbed", Spec: execSpecBare,
	})
	if ref != nil || err != nil {
		t.Fatalf("create the environment: ref=%v err=%v", ref, err)
	}
	if _, ref, err := s.BindEnvironment(execUser, NewBinding{ScopeKind: "workspace", EnvironmentID: env.ID}); ref != nil || err != nil {
		t.Fatalf("bind the workspace: ref=%v err=%v", ref, err)
	}

	// A second workspace row must be impossible, and the UPSERT path is what
	// makes it a single statement rather than a delete-then-insert window.
	other, ref, err := s.CreateEnvironment(execUser, NewEnvironment{
		Slug: "container", Name: "Container", Backend: "host-scrubbed", Spec: execSpecBare,
	})
	if ref != nil || err != nil {
		t.Fatalf("create the second environment: ref=%v err=%v", ref, err)
	}
	if _, ref, err := s.BindEnvironment(execUser, NewBinding{ScopeKind: "workspace", EnvironmentID: other.ID}); ref != nil || err != nil {
		t.Fatalf("rebind the workspace: ref=%v err=%v", ref, err)
	}
	var (
		n  int
		id string
	)
	if err := s.r.QueryRow(`SELECT COUNT(*) FROM environment_binding WHERE scope_kind = 'workspace'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("there are %d workspace bindings, want exactly 1 (EV2)", n)
	}
	if err := s.r.QueryRow(`SELECT environment_id FROM environment_binding WHERE scope_kind = 'workspace'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if id != other.ID {
		t.Errorf("the workspace still points at %q; a rebind that silently keeps the old row is EV2's wrong answer", id)
	}

	// The raw statement, bypassing the door, is refused by the index — which is
	// the only reason the property survives a future caller.
	if _, err := s.w.Exec(`INSERT INTO environment_binding (scope_kind, component_id, environment_id, bound_at)
		VALUES ('workspace', NULL, ?, '2026-11-03T00:00:00Z')`, env.ID); err == nil {
		t.Error("a second raw workspace binding was accepted; the partial unique index is not doing its job")
	}
}

func TestComponentBindingIsUnique(t *testing.T) {
	_, s := testStore(t)
	rootW, err := s.CreateComponent(NewComponent{Name: "repo", Slug: "repo", Root: "/r/repo"})
	if err != nil || !rootW.OK() {
		t.Fatalf("seed a component: %v %+v", err, rootW)
	}
	root := rootW.Component
	env, ref, err := s.CreateEnvironment(execUser, NewEnvironment{
		Slug: "e1", Name: "E1", Backend: "host-scrubbed", Spec: execSpecBare,
	})
	if ref != nil || err != nil {
		t.Fatalf("create env: ref=%v err=%v", ref, err)
	}
	if _, ref, err := s.BindEnvironment(execUser, NewBinding{
		ScopeKind: "component", ComponentID: root.ID, EnvironmentID: env.ID,
	}); ref != nil || err != nil {
		t.Fatalf("bind the component: ref=%v err=%v", ref, err)
	}
	if _, err := s.w.Exec(`INSERT INTO environment_binding (scope_kind, component_id, environment_id, bound_at)
		VALUES ('component', ?, ?, '2026-11-03T00:00:00Z')`, root.ID, env.ID); err == nil {
		t.Error("a second raw binding for one component was accepted; EV2 says one binding per component")
	}
	// The shape CHECK: a component scope with no component (or a workspace with
	// one) is not a binding anybody can resolve.
	if _, err := s.w.Exec(`INSERT INTO environment_binding (scope_kind, component_id, environment_id, bound_at)
		VALUES ('component', NULL, ?, '2026-11-03T00:00:00Z')`, env.ID); err == nil {
		t.Error("a component binding with no component id was accepted; the shape CHECK is missing")
	}
}

func TestNoCredentialShapedColumnExists(t *testing.T) {
	// EV7's structural claim, checked against the schema rather than promised
	// by a comment: no column in the six tables could hold a credential value.
	//
	// The marker words are the ones `internal/config/creds.go` uses to decide a
	// config value IS a credential (C4/C17) — the repository's own definition,
	// read from the same file the credential rule reads, plus the spellings a
	// value column would plausibly use.
	marks := []string{"key", "token", "secret", "apikey", "api_key", "password", "passwd",
		"credential", "value", "blob", "payload"}
	// The columns the design itself names, which hold NAMES by contract (EV9):
	// `step_environment.secret_refs` is the JSON array of reference names, and
	// it is the only column in the six tables whose name contains a credential
	// word while being not a value column. The assertion that it holds names and
	// nothing else is TestRecordStepEnvironmentDedupesDistinctOutcomes, which
	// reads the stored text and fails on a `=` or a `postgres://`. An allow-list
	// of one, written out, is the honest shape: any NEW column matching a mark
	// still fails here.
	contracted := map[string]bool{"step_environment.secret_refs": true}
	tables := []string{"environment", "environment_binding", "secret_ref",
		"step_environment", "environment_provision", "environment_change"}
	_, s := testStore(t)
	for _, table := range tables {
		rows, err := s.r.Query(`SELECT name FROM pragma_table_info(?)`, table)
		if err != nil {
			t.Fatalf("pragma_table_info(%s): %v", table, err)
		}
		var cols []string
		for rows.Next() {
			var c string
			if err := rows.Scan(&c); err != nil {
				rows.Close()
				t.Fatalf("scan a column of %s: %v", table, err)
			}
			cols = append(cols, c)
		}
		rows.Close()
		if len(cols) == 0 {
			t.Errorf("%s has no columns — the table is missing, so this test is asserting nothing", table)
			continue
		}
		for _, c := range cols {
			lower := strings.ToLower(c)
			for _, m := range marks {
				if contracted[table+"."+c] {
					continue
				}
				if strings.Contains(lower, m) {
					t.Errorf("%s.%s is credential-shaped (it contains %q) — EV7 says a value column is the bug, "+
						"in this table, at this layer, forever", table, c, m)
				}
			}
		}
	}
}

func TestAuditAndStepEnvironmentAreAppendOnly(t *testing.T) {
	_, s := testStore(t)
	env, ref, err := s.CreateEnvironment(execUser, NewEnvironment{
		Slug: "audited", Name: "Audited", Backend: "host-scrubbed", Spec: execSpecBare,
	})
	if ref != nil || err != nil {
		t.Fatalf("create env: ref=%v err=%v", ref, err)
	}
	if _, err := s.w.Exec(`INSERT INTO step_environment (step_id, environment_id, backend, downgraded,
			var_names, secret_refs, resolved_at)
		VALUES (41, ?, 'host-scrubbed', 0, '[]', '[]', '2026-11-03T00:00:00Z')`, env.ID); err != nil {
		t.Fatalf("seed a step_environment row: %v", err)
	}

	type mutate struct {
		name, stmt string
	}
	mutates := []mutate{
		{"UPDATE environment_change", `UPDATE environment_change SET summary = 'rewritten' WHERE id >= 0`},
		{"DELETE environment_change", `DELETE FROM environment_change WHERE id >= 0`},
		{"UPDATE step_environment", `UPDATE step_environment SET var_names = '[]' WHERE step_id = 41`},
		{"DELETE step_environment", `DELETE FROM step_environment WHERE step_id = 41`},
	}
	for _, m := range mutates {
		if _, err := s.w.Exec(m.stmt); err == nil {
			t.Errorf("%s succeeded — the trigger that makes this table append-only is missing, "+
				"and an editable history is not a history", m.name)
		} else if !strings.Contains(err.Error(), "append-only") {
			t.Errorf("%s failed for the wrong reason: %v (want the trigger's own sentence)", m.name, err)
		}
	}

	// The closed vocabularies in SQL, so a raw statement cannot leak a state the
	// Go doors do not have.
	for _, tc := range []struct{ name, stmt string }{
		{"a backend outside EV3", `INSERT INTO environment (id,slug,name,backend,spec,origin,legacy,revision,created_at,updated_at)
			VALUES ('env-x','x','X','qemu','{}','user',0,1,'2026-11-03T00:00:00Z','2026-11-03T00:00:00Z')`},
		{"an action outside EV17", `INSERT INTO environment_change (at,actor_kind,actor_id,subject_kind,subject_id,action,summary)
			VALUES ('2026-11-03T00:00:00Z','user','u','environment','env-audited','obliterate','x')`},
		{"an actor_kind outside EV18", `INSERT INTO environment_change (at,actor_kind,actor_id,subject_kind,subject_id,action,summary)
			VALUES ('2026-11-03T00:00:00Z','ghost','u','environment','env-audited','update','x')`},
		{"an invalid spec", `INSERT INTO environment (id,slug,name,backend,spec,origin,legacy,revision,created_at,updated_at)
			VALUES ('env-y','y','Y','host-scrubbed','not json','user',0,1,'2026-11-03T00:00:00Z','2026-11-03T00:00:00Z')`},
		{"a provision state outside EV12", `INSERT INTO environment_provision (id,environment_id,component_root,loop_slug,backend,state,created_at)
			VALUES ('prov-x','env-audited','/r','l','host-scrubbed','zombie','2026-11-03T00:00:00Z')`},
		{"an owner that is neither loop nor service", `INSERT INTO environment_provision (id,environment_id,component_root,loop_slug,backend,owner,state,created_at)
			VALUES ('prov-y','env-audited','/r','l','host-scrubbed','whoever','provisioning','2026-11-03T00:00:00Z')`},
		{"a settled row without removed_at", `INSERT INTO environment_provision (id,environment_id,component_root,loop_slug,backend,state,created_at)
			VALUES ('prov-z','env-audited','/r','l','host-scrubbed','removed','2026-11-03T00:00:00Z')`},
		{"a source_kind outside EV7", `INSERT INTO secret_ref (id,name,source_kind,source,inject_as,origin,created_at,updated_at)
			VALUES ('sref-x','x','vault','x','X','user','2026-11-03T00:00:00Z','2026-11-03T00:00:00Z')`},
	} {
		if _, err := s.w.Exec(tc.stmt); err == nil {
			t.Errorf("%s was accepted by SQL — the CHECK that refuses it at the door is the only thing left", tc.name)
		}
	}
}
