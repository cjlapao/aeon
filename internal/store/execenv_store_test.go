package store

// execenv_store_test.go — S59 T2's behaviour half: the audit discipline (EV17),
// optimistic concurrency, EV2's resolution order, EV7's archive guard, and
// EV12's settle-once ledger.

import (
	"strings"
	"testing"
)

func countChanges(t *testing.T, s *Store, subjectKind, subjectID string) int {
	t.Helper()
	var n int
	if err := s.r.QueryRow(`SELECT COUNT(*) FROM environment_change WHERE subject_kind = ? AND subject_id = ?`,
		subjectKind, subjectID).Scan(&n); err != nil {
		t.Fatalf("count %s changes of %s: %v", subjectKind, subjectID, err)
	}
	return n
}

func TestEveryChangingWriteAppendsOneAuditRowInTheSameTx(t *testing.T) {
	_, s := testStore(t)

	// --- create: one row, the actor from the door, the template's sentence ---
	env, ref, err := s.CreateEnvironment(execUser, NewEnvironment{
		Slug: "audited", Name: "Audited", Backend: "host-scrubbed", Spec: execSpecBare,
	})
	if ref != nil || err != nil {
		t.Fatalf("create: ref=%v err=%v", ref, err)
	}
	if n := countChanges(t, s, "environment", env.ID); n != 1 {
		t.Errorf("create wrote %d audit rows, want exactly 1", n)
	}
	changes, err := s.ListChanges(EnvironmentChangeFilter{SubjectKind: "environment", SubjectID: env.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 {
		t.Fatalf("ListChanges returned %d rows", len(changes))
	}
	c := changes[0]
	if c.ActorKind != "user" || c.ActorID != "carlos" || c.Action != "create" ||
		c.Revision == nil || *c.Revision != 1 {
		t.Errorf("the audit row does not say who did what: %+v", c)
	}
	if c.SpecAfter == nil || *c.SpecAfter != execSpecBare {
		t.Errorf("the audit row lost the spec that landed: %+v", c)
	}

	// --- every later changing write lands exactly one more row ---------------
	type step struct {
		name   string
		do     func() *EnvironmentRefusal
		expect int
	}
	for _, st := range []step{
		{"update", func() *EnvironmentRefusal {
			_, r, err := s.UpdateEnvironment(execUser, UpdateEnvironment{ID: env.ID, Spec: execSpecJSON, ExpectedRevision: 1})
			if err != nil {
				t.Fatalf("update: %v", err)
			}
			return r
		}, 2},
		{"archive", func() *EnvironmentRefusal {
			_, r, err := s.ArchiveEnvironment(execUser, env.ID)
			if err != nil {
				t.Fatalf("archive: %v", err)
			}
			return r
		}, 3},
		{"restore", func() *EnvironmentRefusal {
			_, r, err := s.RestoreEnvironment(execUser, env.ID)
			if err != nil {
				t.Fatalf("restore: %v", err)
			}
			return r
		}, 4},
	} {
		if r := st.do(); r != nil {
			t.Fatalf("%s refused: %s", st.name, r.Error())
		}
		if n := countChanges(t, s, "environment", env.ID); n != st.expect {
			t.Errorf("after %s there are %d audit rows, want %d", st.name, n, st.expect)
		}
	}

	// --- reads, refusals and no-ops write nothing (P3) -----------------------
	before := countChanges(t, s, "environment", env.ID)
	if _, err := s.GetEnvironment(env.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ListEnvironments(EnvironmentFilter{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ListBindings(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ListChanges(EnvironmentChangeFilter{}); err != nil {
		t.Fatal(err)
	}
	// A refusal: the revision is 4 now, so 1 is stale.
	if _, r, err := s.UpdateEnvironment(execUser, UpdateEnvironment{
		ID: env.ID, Spec: execSpecBare, ExpectedRevision: 1}); r == nil || err != nil {
		t.Fatalf("a stale update was not refused: r=%v err=%v", r, err)
	}
	// A no-op: archiving an already-archived row, twice.
	if _, _, err := s.ArchiveEnvironment(execUser, env.ID); err != nil {
		t.Fatal(err)
	}
	if _, r, err := s.ArchiveEnvironment(execUser, env.ID); r != nil || err != nil {
		t.Fatalf("the second archive should be a silent no-op: r=%v err=%v", r, err)
	}
	if _, _, err := s.RestoreEnvironment(execUser, env.ID); err != nil {
		t.Fatal(err)
	}
	// Of everything above, only TWO were changing writes: the archive and the
	// restore that undid it. The four reads, the refused stale update and the
	// repeated archive write nothing (P3).
	if n := countChanges(t, s, "environment", env.ID); n != before+2 {
		t.Errorf("the reads, the refusal and the no-op together wrote %d audit row(s); only the archive and "+
			"the restore were changing writes — P3: an audit that counts nothing-happened events audits nothing", n-before)
	}

	// --- the same transaction: a failed audit rolls the write back too --------
	tx, err := s.w.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO environment (id,slug,name,backend,spec,origin,legacy,revision,created_at,updated_at)
		VALUES ('env-tx','tx','Tx','host-scrubbed','{}','user',0,1,'2026-11-03T00:00:00Z','2026-11-03T00:00:00Z')`); err != nil {
		tx.Rollback()
		t.Fatalf("insert inside the probe tx: %v", err)
	}
	if err := appendEnvironmentChange(tx, execUser, s.Now(), "not_a_subject", "env-tx", "create",
		nil, nil, "probe"); err == nil {
		tx.Rollback()
		t.Fatal("appendEnvironmentChange accepted a subject kind outside the closed set")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	var probe int
	if err := s.r.QueryRow(`SELECT COUNT(*) FROM environment WHERE id = 'env-tx'`).Scan(&probe); err != nil {
		t.Fatal(err)
	}
	if probe != 0 {
		t.Error("the row survived a rolled-back audit — an audit that can be lost while its write lands " +
			"is the state EV17 forbids")
	}
}

func TestUpdateRefusesAStaleRevision(t *testing.T) {
	_, s := testStore(t)
	env, ref, err := s.CreateEnvironment(execUser, NewEnvironment{
		Slug: "rev", Name: "Rev", Backend: "host-scrubbed", Spec: execSpecBare,
	})
	if ref != nil || err != nil {
		t.Fatalf("create: ref=%v err=%v", ref, err)
	}

	if _, r, err := s.UpdateEnvironment(execUser, UpdateEnvironment{
		ID: env.ID, Spec: execSpecJSON, ExpectedRevision: 1}); r != nil || err != nil {
		t.Fatalf("the first update should have landed: r=%v err=%v", r, err)
	}
	_, r, err := s.UpdateEnvironment(execUser, UpdateEnvironment{
		ID: env.ID, Spec: execSpecBare, ExpectedRevision: 1})
	if err != nil {
		t.Fatalf("a stale update must be a refusal, not an error (DB19): %v", err)
	}
	if r == nil {
		t.Fatal("a stale update was accepted — that is EV17's lost update, the reason the column exists")
	}
	if !strings.Contains(r.Error(), "2") {
		t.Errorf("the refusal does not name the CURRENT revision (2), so the caller cannot re-read and retry: %s", r.Error())
	}
	if !strings.Contains(r.Error(), "refused:") {
		t.Errorf("a refusal that does not say so is indistinguishable from a sentence: %s", r.Error())
	}

	// The revision moves on every changing write and never resets, archive and
	// restore included, so audit rows stay diffable by revision.
	if _, _, err := s.ArchiveEnvironment(execUser, env.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetEnvironment(env.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Revision != 3 {
		t.Errorf("revision after create+update+archive = %d, want 3", got.Revision)
	}
	if _, _, err := s.RestoreEnvironment(execUser, env.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.GetEnvironment(env.ID); got.Revision != 4 {
		t.Errorf("restore reset the revision to %d; a counter that resets lets two states claim one revision", got.Revision)
	}
}

func TestBindingResolutionChildRootWorkspace(t *testing.T) {
	_, s := testStore(t)
	rootW, err := s.CreateComponent(NewComponent{Name: "repo", Slug: "repo", Root: "/r/repo"})
	if err != nil || !rootW.OK() {
		t.Fatalf("root component: %v %+v", err, rootW)
	}
	root := rootW.Component
	childW, err := s.CreateComponent(NewComponent{Name: "child", Slug: "child", Root: "/r/repo/child", ParentID: root.ID})
	if err != nil || !childW.OK() {
		t.Fatalf("child component: %v %+v", err, childW)
	}
	child := childW.Component
	env := func(slug, backend string) Environment {
		t.Helper()
		e, ref, err := s.CreateEnvironment(execUser, NewEnvironment{
			Slug: slug, Name: slug, Backend: backend, Spec: execSpecBare,
		})
		if ref != nil || err != nil {
			t.Fatalf("create %s: ref=%v err=%v", slug, ref, err)
		}
		return e
	}
	ws, rootEnv, childEnv := env("ws", "host-scrubbed"), env("root-env", "host-scrubbed"), env("child-env", "container")

	bind := func(scope, componentID, environmentID string) {
		t.Helper()
		_, r, err := s.BindEnvironment(execUser, NewBinding{
			ScopeKind: scope, ComponentID: componentID, EnvironmentID: environmentID,
		})
		if r != nil || err != nil {
			t.Fatalf("bind %s: ref=%v err=%v", scope, r, err)
		}
	}

	// EV2's order: each level wins only while the level above it is silent, and
	// `source` says which one answered — a step record that names the
	// environment without naming where it came from cannot be debugged.
	bind("workspace", "", ws.ID)
	b, source, err := s.BindingFor(child.ID)
	if err != nil {
		t.Fatal(err)
	}
	if b.EnvironmentID != ws.ID || source != "workspace" {
		t.Errorf("with only a workspace binding, %s/%s answered", b.EnvironmentID, source)
	}

	bind("component", root.ID, rootEnv.ID)
	if b, source, _ = s.BindingFor(child.ID); b.EnvironmentID != rootEnv.ID || source != "root" {
		t.Errorf("with a root binding present, %s/%s answered — want the root (EV2)", b.EnvironmentID, source)
	}

	bind("component", child.ID, childEnv.ID)
	if b, source, _ = s.BindingFor(child.ID); b.EnvironmentID != childEnv.ID || source != "component" {
		t.Errorf("with its own binding, %s/%s answered", b.EnvironmentID, source)
	}

	// The root answers for itself; unbinding it falls back to the workspace.
	if b, source, _ = s.BindingFor(root.ID); b.EnvironmentID != rootEnv.ID || source != "component" {
		t.Errorf("the root resolved to %s/%s — its own binding must win over the workspace", b.EnvironmentID, source)
	}
	if _, _, err := s.UnbindEnvironment(execUser, root.ID); err != nil {
		t.Fatal(err)
	}
	if b, source, _ = s.BindingFor(root.ID); b.EnvironmentID != ws.ID || source != "workspace" {
		t.Errorf("after unbinding the root it resolved to %s/%s", b.EnvironmentID, source)
	}
	if _, _, err := s.UnbindEnvironment(execUser, child.ID); err != nil {
		t.Fatal(err)
	}
	if b, source, err := s.BindingFor("no-such-component"); err != nil || b.EnvironmentID != ws.ID || source != "workspace" {
		t.Errorf("an unknown component fell through oddly: %+v/%s/%v", b, source, err)
	}

	// The workspace binding cannot be removed at any door, by any actor: with no
	// binding every command in the workspace is refused, and "I removed it and
	// now nothing runs" is the ticket this guard prevents.
	if _, r, err := s.UnbindEnvironment(execUser, ""); r == nil || err != nil {
		t.Errorf("the workspace binding was unbound: r=%v err=%v", r, err)
	}
}

func TestCannotArchiveAReferencedSecretRef(t *testing.T) {
	_, s := testStore(t)
	if _, r, err := s.CreateSecretRef(execUser, NewSecretRef{
		Name: "db-url", Description: "the app database", SourceKind: "daemon-env",
		Source: "DATABASE_URL", InjectAs: "DATABASE_URL",
	}, []string{"AEON_FORGE_TOKEN"}); r != nil || err != nil {
		t.Fatalf("create the reference: r=%v err=%v", r, err)
	}
	for _, slug := range []string{"names-it", "also-names"} {
		if _, r, err := s.CreateEnvironment(execUser, NewEnvironment{
			Slug: slug, Name: slug, Backend: "host-scrubbed", Spec: execSpecJSON,
		}); r != nil || err != nil {
			t.Fatalf("create %s: r=%v err=%v", slug, r, err)
		}
	}
	if _, _, err := s.CreateEnvironment(execUser, NewEnvironment{
		Slug: "silent", Name: "Silent", Backend: "host-scrubbed", Spec: execSpecBare,
	}); err != nil {
		t.Fatal(err)
	}

	_, r, err := s.ArchiveSecretRef(execUser, "db-url")
	if err != nil {
		t.Fatalf("archiving a referenced reference must refuse, not fail: %v", err)
	}
	if r == nil {
		t.Fatal("a reference still named by two live environments was archived — EV7 then leaves a bind " +
			"pointing at nothing and a step that cannot resolve its own environment")
	}
	sentence := r.Error()
	for _, want := range []string{"names-it", "also-names"} {
		if !strings.Contains(sentence, want) {
			t.Errorf("the refusal does not name %q, so nobody can go and narrow it: %s", want, sentence)
		}
	}
	if strings.Contains(sentence, "silent") {
		t.Errorf("the refusal names an environment that does not reference it: %s", sentence)
	}

	// A substring is not a reference: the check parses the arrays rather than
	// matching text, or `my-db-url` would hold `db-url` hostage.
	if _, _, err := s.CreateSecretRef(execUser, NewSecretRef{
		Name: "my-db-url", SourceKind: "daemon-env", Source: "OTHER_URL", InjectAs: "OTHER_URL",
	}, nil); err != nil {
		t.Fatal(err)
	}
	if _, r, err := s.ArchiveSecretRef(execUser, "my-db-url"); r != nil || err != nil {
		t.Errorf("an unreferenced reference was refused: r=%v err=%v", r, err)
	}

	// Narrow both environments, and the reference archives — audited.
	for _, slug := range []string{"names-it", "also-names"} {
		if _, _, err := s.ArchiveEnvironment(execUser, slug); err != nil {
			t.Fatalf("archive %s: %v", slug, err)
		}
	}
	if _, r, err := s.ArchiveSecretRef(execUser, "db-url"); r != nil || err != nil {
		t.Errorf("after narrowing both environments the reference still could not archive: r=%v err=%v", r, err)
	}
	changes, err := s.ListChanges(EnvironmentChangeFilter{SubjectKind: "secret_ref"})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) < 3 {
		t.Errorf("only %d secret_ref audit rows exist; create, the no-op-free update and the archive are all "+
			"changing writes: %+v", len(changes), changes)
	}
}

func TestCreateSecretRefRefusesADaemonCredential(t *testing.T) {
	_, s := testStore(t)
	// EV7's write-time door: a reference that reads the daemon's own credential
	// is refused at the write, not at the spawn where it would become a leak.
	_, r, err := s.CreateSecretRef(execUser, NewSecretRef{
		Name: "forge", SourceKind: "daemon-env", Source: "AEON_FORGE_TOKEN", InjectAs: "GITHUB_TOKEN",
	}, []string{"AEON_FORGE_TOKEN", "ANTHROPIC_API_KEY"})
	if err != nil {
		t.Fatalf("a refusal is not an error (DB19): %v", err)
	}
	if r == nil {
		t.Fatal("a reference at the daemon's forge token was accepted")
	}
	if r.Kind != "daemon-credential" {
		t.Errorf("kind = %q, want daemon-credential", r.Kind)
	}
	if !strings.Contains(r.Error(), "AEON_FORGE_TOKEN") {
		t.Errorf("the refusal does not name the variable it refused: %s", r.Error())
	}

	for _, tc := range []struct {
		name, kind, source, wantKind string
	}{
		{"relative key file", "key-file", "secrets/db.key", "key-file-relative"},
		{"traversing key file", "key-file", "/etc/../home/x/.netrc", "key-file-traversal"},
		{"a store source that is a path", "store", "../state.db", "store-source-shape"},
		{"a daemon-env source that is not a name", "daemon-env", "DATABASE URL", "daemon-env-shape"},
		{"an unknown source kind", "vault", "whatever", "source-kind-unknown"},
		{"an empty source", "key-file", "", "source-empty"},
	} {
		_, r, err := s.CreateSecretRef(execUser, NewSecretRef{
			Name: "ref-" + tc.wantKind, SourceKind: tc.kind, Source: tc.source, InjectAs: "APP_VAR",
		}, nil)
		if r == nil || err != nil {
			t.Errorf("%s: r=%v err=%v, want a refusal naming %q", tc.name, r, err, tc.wantKind)
			continue
		}
		if r.Kind != tc.wantKind {
			t.Errorf("%s: kind = %q, want %q (%s)", tc.name, r.Kind, tc.wantKind, r.Error())
		}
	}

	// The `store` kind (A1's approval) is admitted when the source is a
	// credential id rather than a path.
	if _, r, err := s.CreateSecretRef(execUser, NewSecretRef{
		Name: "route-key", SourceKind: "store", Source: "route-42", InjectAs: "ROUTE_KEY",
	}, nil); r != nil || err != nil {
		t.Errorf("the approved store kind was refused: r=%v err=%v", r, err)
	}

	// A name is immutable: renaming would silently unbind every environment
	// whose spec says the old one.
	if _, _, err := s.CreateSecretRef(execUser, NewSecretRef{
		Name: "keepme", SourceKind: "key-file", Source: "/etc/aeon/keepme.key", InjectAs: "KEEPME",
	}, nil); err != nil {
		t.Fatal(err)
	}
	if _, r, err := s.UpdateSecretRef(execUser, UpdateSecretRef{Name: "keepme"}); r != nil || err != nil {
		t.Errorf("a no-change update should be a no-op: r=%v err=%v", r, err)
	}
	if _, r, err := s.UpdateSecretRef(execUser, UpdateSecretRef{Name: "nope"}); r == nil || err != nil {
		t.Errorf("updating a reference that does not exist: r=%v err=%v", r, err)
	}
	// A duplicate name is refused rather than shadowed.
	if _, r, err := s.CreateSecretRef(execUser, NewSecretRef{
		Name: "keepme", SourceKind: "key-file", Source: "/etc/aeon/other.key", InjectAs: "OTHER",
	}, nil); r == nil || err != nil {
		t.Errorf("a second reference named keepme was accepted: r=%v err=%v", r, err)
	}
}

func TestProvisionLedgerIsSettleOnce(t *testing.T) {
	_, s := testStore(t)
	env, r, err := s.CreateEnvironment(execUser, NewEnvironment{
		Slug: "with-services", Name: "With services", Backend: "container",
		Spec: `{"env":{"passthrough":["PATH"]},"home":"scratch","egress":{"mode":"none"},` +
			`"image":{"ref":"app@sha256:aaaa"},"services":[]}`,
	})
	if r != nil || err != nil {
		t.Fatalf("create the environment: r=%v err=%v", r, err)
	}
	newRow := func(owner string) Provision {
		t.Helper()
		p, r, err := s.CreateProvision(NewProvision{
			EnvironmentID: env.ID, ComponentRoot: "/r/app", LoopSlug: "nightly", Backend: "container", Owner: owner,
		})
		if r != nil || err != nil {
			t.Fatalf("create provision %s: r=%v err=%v", owner, r, err)
		}
		return p
	}

	loopRow := newRow("loop")
	svcRow := newRow("service:postgres")
	if loopRow.State != "provisioning" || loopRow.Owner != "loop" {
		t.Errorf("a new row is not provisioning/loop: %+v", loopRow)
	}
	if !loopRow.Live() || !svcRow.Live() {
		t.Error("a fresh row is not live — reconcile would skip it and leak the container")
	}

	// One live row per owner: the second is refused and NAMES the live row.
	_, r, err = s.CreateProvision(NewProvision{
		EnvironmentID: env.ID, ComponentRoot: "/r/app", LoopSlug: "nightly", Backend: "container",
	})
	if r == nil || err != nil {
		t.Fatalf("a second live row for one owner was accepted: r=%v err=%v", r, err)
	}
	if !strings.Contains(r.Error(), loopRow.ID) {
		t.Errorf("the refusal does not name the live row: %s", r.Error())
	}
	if _, r, err := s.CreateProvision(NewProvision{
		EnvironmentID: env.ID, ComponentRoot: "/r/app", LoopSlug: "nightly", Backend: "container",
		Owner: "service:redis",
	}); r != nil || err != nil {
		t.Errorf("a second service was refused (a different owner is a different resource, EV14): r=%v err=%v", r, err)
	}

	// A ready row needs a handle: a ready row with no runtime id is the leak
	// EV12 exists to prevent, wearing the costume of success.
	if _, r, err := s.MarkProvisionReady(loopRow.ID, "  "); r == nil || err != nil {
		t.Errorf("a handle-less ready mark was accepted: r=%v err=%v", r, err)
	}
	if _, r, err := s.MarkProvisionReady(loopRow.ID, "container-a1b2"); r != nil || err != nil {
		t.Fatalf("ready: r=%v err=%v", r, err)
	}
	got, err := s.GetProvision(loopRow.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "ready" || got.Handle != "container-a1b2" || got.ReadyAt == nil {
		t.Errorf("the ready mark did not land: %+v", got)
	}

	// Settle-once: the same mark twice is the crashed-daemon-retrying case, and
	// it is a no-op — a reconcile that reports an error for the thing it just
	// fixed trains its reader to ignore it.
	for i := 0; i < 3; i++ {
		p, r, err := s.MarkProvisionReady(loopRow.ID, "container-a1b2")
		if r != nil || err != nil {
			t.Fatalf("repeat ready mark %d should be a no-op: r=%v err=%v", i, r, err)
		}
		if p.State != "ready" {
			t.Fatalf("a repeated ready mark moved the row to %s", p.State)
		}
	}

	// Backward is a refusal naming the state it found.
	if _, r, err := s.MarkProvisionFailed(loopRow.ID, "the image would not pull"); r != nil || err != nil {
		t.Fatalf("ready → failed should be allowed: r=%v err=%v", r, err)
	}
	if _, r, err := s.MarkProvisionReady(loopRow.ID, "container-a1b2"); r == nil || err != nil {
		t.Errorf("failed → ready was accepted: r=%v err=%v", r, err)
	} else if !strings.Contains(r.Error(), "failed") {
		t.Errorf("the refusal does not name the state it found: %s", r.Error())
	}
	if _, r, err := s.MarkProvisionFailed(svcRow.ID, "   "); r == nil || err != nil {
		t.Errorf("a failure with no sentence was accepted (r=%v err=%v) — EV12's failed row exists to carry the reason", r, err)
	}

	// Teardown → removed, and removal frees the owner's slot (EV14's restart).
	if _, _, err := s.MarkProvisionTearingDown(loopRow.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.MarkProvisionRemoved(loopRow.ID); err != nil {
		t.Fatal(err)
	}
	if again, err := s.GetProvision(loopRow.ID); err != nil {
		t.Fatal(err)
	} else if again.State != "removed" || again.RemovedAt == nil {
		t.Errorf("the settled row is not removed-with-a-time: %+v", again)
	}
	if _, r, err := s.CreateProvision(NewProvision{
		EnvironmentID: env.ID, ComponentRoot: "/r/app", LoopSlug: "nightly", Backend: "container",
	}); r != nil || err != nil {
		t.Errorf("the owner's slot stayed taken after removal: r=%v err=%v — EV14's restart cannot start", r, err)
	}
	if _, r, err := s.MarkProvisionRemoved(loopRow.ID); r != nil || err != nil {
		t.Errorf("a repeat removal should be a no-op: r=%v err=%v", r, err)
	}

	// The two reads reconcile and the panel depend on.
	live, err := s.ListLiveProvisions()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range live {
		if !p.Live() {
			t.Errorf("ListLiveProvisions returned the settled row %s", p.ID)
		}
	}
	if len(live) != 3 { // postgres (failed), redis (provisioning), the loop's new row
		t.Errorf("ListLiveProvisions returned %d rows: %+v", len(live), live)
	}
	all, err := s.ProvisionsFor("/r/app", "nightly")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 4 {
		t.Errorf("ProvisionsFor returned %d rows, want all 4 including the removed one", len(all))
	}
	if _, r, err := s.MarkProvisionRemoved("prov-nope"); r == nil || err != nil {
		t.Errorf("marking a row that does not exist: r=%v err=%v — the ledger is the only thing that knows "+
			"a resource exists, so a mark against nothing means a container created without a row", r, err)
	}
	if _, r, err := s.CreateProvision(NewProvision{
		EnvironmentID: "env-nope", ComponentRoot: "/r", LoopSlug: "l", Backend: "container",
	}); r == nil || err != nil {
		t.Errorf("a provision for an environment that does not exist was accepted: r=%v err=%v", r, err)
	}
	if _, r, _ := s.CreateProvision(NewProvision{
		EnvironmentID: env.ID, ComponentRoot: "/r", LoopSlug: "l", Backend: "container", Owner: "whoever",
	}); r == nil {
		t.Error("an owner that is neither loop nor service:<name> was accepted")
	}
}

func TestRecordStepEnvironmentDedupesDistinctOutcomes(t *testing.T) {
	_, s := testStore(t)
	first, err := s.RecordStepEnvironment(NewStepEnvironment{
		StepID: 77, EnvironmentID: strPtr("env-x"), Backend: strPtr("host-scrubbed"),
		VarNames: []string{"PATH", "CI", "PATH"}, SecretRefs: []string{"db-url"},
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.RecordStepEnvironment(NewStepEnvironment{
		StepID: 77, EnvironmentID: strPtr("env-x"), Backend: strPtr("host-scrubbed"),
		VarNames: []string{"CI", "PATH"}, SecretRefs: []string{"db-url"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Errorf("the same outcome written twice made two rows (%d, %d) — the dedupe is the store's because "+
			"distinctness is over parsed JSON, which an index cannot express", first.ID, second.ID)
	}
	if _, err := s.RecordStepEnvironment(NewStepEnvironment{
		StepID: 77, EnvironmentID: strPtr("env-y"), Backend: strPtr("container"),
		VarNames: []string{"PATH"}, SecretRefs: []string{},
	}); err != nil {
		t.Fatal(err)
	}
	// A refusal that never spawned is recorded rather than dropped (EV11):
	// "why did this step not run" is answered from the row a day later.
	if _, err := s.RecordStepEnvironment(NewStepEnvironment{
		StepID: 77, RefusalKind: strPtr("environment-unbound"),
		Refusal:  strPtr("refused: environment-unbound · no environment is bound"),
		VarNames: []string{}, SecretRefs: []string{},
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := s.StepEnvironments(77)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("StepEnvironments returned %d rows, want 3 distinct outcomes", len(rows))
	}
	if rows[0].ResolvedAt == "" {
		t.Error("the first row has no resolved_at — a step record with no time is not a record")
	}
	if got := strings.Join(rows[0].VarNames, ","); got != "CI,PATH" {
		t.Errorf("names are stored canonical (sorted, deduped), got %q", got)
	}
	if rows[2].RefusalKind == nil || *rows[2].RefusalKind != "environment-unbound" {
		t.Errorf("the refusal row lost its kind: %+v", rows[2])
	}
	// Names only (EV9): the row has no column that could hold a value, and the
	// arrays hold names — a step record that leaked one would be the leak this
	// slice exists to close, committed on purpose.
	var spec string
	if err := s.r.QueryRow(`SELECT var_names || secret_refs FROM step_environment WHERE id = ?`, rows[0].ID).
		Scan(&spec); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(spec, "=") || strings.Contains(spec, "postgres://") {
		t.Errorf("a step_environment array carries something shaped like a value: %s", spec)
	}
}

func strPtr(s string) *string { return &s }
