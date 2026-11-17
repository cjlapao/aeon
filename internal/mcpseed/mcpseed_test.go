package mcpseed

import (
	"context"
	"database/sql"
	"errors"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/seed"
	"github.com/cjlapao/aeon/internal/store"
)

// openStore opens a fully-migrated temp store (the agentseed/policyseed
// pattern — the tests never point at the real store).
func openStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(store.Options{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func fixedClock() func() time.Time {
	return func() time.Time { return time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC) }
}

// goodServerContent is the shape a REAL record will carry when the first
// default ships — canonicalized through seed.Canonical so the fingerprint
// round-trip is byte-truthful.
func goodServerContent() serverContent {
	return serverContent{
		Name: "gh",
		Tier: "public",
		Transport: transportContent{
			Type: "stdio", Command: "gh-mcp",
			Args: []string{"--mcp"}, Env: []string{"GITHUB_TOKEN"},
		},
		Tools: []toolContent{ // NAME-ORDER — the canonical-at-write convention
			{Name: "create_issue", Rerun: "unsafe", Effect: "outward"},
			{Name: "list_issues", Rerun: "safe", Effect: "local"},
		},
		Enabled: false,
	}
}

func recordOf(t *testing.T, key string, v any) seed.Record {
	t.Helper()
	b, err := seed.Canonical(v)
	if err != nil {
		t.Fatalf("canonical: %v", err)
	}
	return seed.Record{Key: key, Version: 1, Content: b}
}

// passValidator accepts everything (the tests that are not about the
// validator keep it silent; the ones that are pass their own).
func passValidator(context.Context, store.McpAll) error { return nil }

func mustCanonical(t *testing.T, v any) []byte {
	t.Helper()
	b, err := seed.Canonical(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// mcpShipPath is the forbidden import path, built so the token never
// appears contiguous in this file (see the guard below).
const mcpShipPath = "github.com/cjlapao/aeon/internal/" + "mcp"

func TestProductionMcpSetsAreDeclaredAndEmpty(t *testing.T) {
	ds := Declarations()
	if len(ds) != 2 {
		t.Fatalf("two sets are declared, got %d", len(ds))
	}
	byID := map[string]seed.Set{}
	for _, d := range ds {
		byID[d.ID] = d
	}
	srv, ok := byID[SetServers]
	if !ok || srv.Class != seed.ClassInert {
		t.Fatalf("mcp.servers must be declared inert: %+v", srv)
	}
	gr, ok := byID[SetGrants]
	if !ok || gr.Class != seed.ClassGuard {
		t.Fatalf("mcp.grants must be declared guard: %+v", gr)
	}
	if r := srv.Records(); r == nil || len(r) != 0 {
		t.Fatalf("mcp.servers ships %d records — MG10 ships ZERO (an empty non-nil slice)", len(r))
	}
	if r := gr.Records(); r == nil || len(r) != 0 {
		t.Fatalf("mcp.grants ships %d records — MG10 ships ZERO", len(r))
	}
	s := openStore(t)
	sa := NewServersApplier(s, passValidator, fixedClock())
	ga := NewGrantsApplier(s, passValidator, fixedClock())
	if sa.GuardAffecting() {
		t.Error("the servers set reports guard-affecting — rule 6 would refuse it inert, and it IS inert by content")
	}
	if !ga.GuardAffecting() {
		t.Error("the grants set does not report guard-affecting — declaring/enabling widens (SD5)")
	}
	for _, a := range []*Applier{sa, ga} {
		op := a.Operator()
		if op.Edit || !op.Remove {
			t.Errorf("%s operator rights: want {Edit:false Remove:true}, got %+v", a.Set(), op)
		}
	}
}

func TestMcpServersSetRefusesEnabledOrDeclaringRecords(t *testing.T) {
	s := openStore(t)
	ap := NewServersApplier(s, passValidator, fixedClock())
	ctx := context.Background()
	c := goodServerContent()

	enabled := c
	enabled.Enabled = true
	enabledRec := recordOf(t, "gh", enabled)
	private := c
	private.Tier = "private"
	privateRec := recordOf(t, "gh", private)
	mismatchRec := recordOf(t, "other-name", c)

	err := s.McpWithTx(ctx, func(tx *sql.Tx) error {
		return ap.Upsert(ctx, tx, enabledRec, false)
	})
	if err == nil || !strings.Contains(err.Error(), SetGrants) {
		t.Fatalf("an enabled record in the inert set must be refused naming %s, got %v", SetGrants, err)
	}
	err = s.McpWithTx(ctx, func(tx *sql.Tx) error { return ap.Upsert(ctx, tx, privateRec, false) })
	if err == nil || !strings.Contains(err.Error(), "PUBLIC") {
		t.Fatalf("a private definition in the inert set must be refused, got %v", err)
	}
	err = s.McpWithTx(ctx, func(tx *sql.Tx) error { return ap.Upsert(ctx, tx, mismatchRec, false) })
	if err == nil || !strings.Contains(err.Error(), "key and the content") {
		t.Fatalf("a key/content mismatch must be refused, got %v", err)
	}
	// The guard set refuses the shapes that are not widenings.
	ga := NewGrantsApplier(s, passValidator, fixedClock())
	weird := grantContent{Kind: "delete", Server: "gh"}
	err = s.McpWithTx(ctx, func(tx *sql.Tx) error {
		return ga.Upsert(ctx, tx, recordOf(t, enableKey("gh"), weird), false)
	})
	if err == nil || !strings.Contains(err.Error(), "nothing else widens") {
		t.Fatalf("a kind outside the closed sum must be refused, got %v", err)
	}
}

func TestFakeServerRecordAppliesDisabledWithSystemOrigin(t *testing.T) {
	s := openStore(t)
	ap := NewServersApplier(s, passValidator, fixedClock())
	ctx := context.Background()
	rec := recordOf(t, "gh", goodServerContent())
	if err := s.McpWithTx(ctx, func(tx *sql.Tx) error {
		return ap.Upsert(ctx, tx, rec, false)
	}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	row, present, err := s.McpServerGet(ctx, "gh")
	if err != nil || !present {
		t.Fatalf("the system server did not land: present=%v err=%v", present, err)
	}
	if row.Origin != "system" {
		t.Errorf("origin = %q, want system — provenance is the whole MG10 story", row.Origin)
	}
	if row.Enabled {
		t.Error("an inert-set server landed ENABLED — definitions nothing may use")
	}
	if len(row.Tools) != 2 {
		t.Errorf("tools = %d, want 2 classified rows", len(row.Tools))
	}
	// The domain audit beside the engine's seed_event (behaviour 6): op
	// seed, ids-only summary, machine actor.
	changes, err := s.McpChangeList(ctx, "gh", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Op != "seed" || changes[0].ActorKind != "system" || changes[0].ActorID != "seed-engine" {
		t.Fatalf("the seed apply left %+v — want one op=seed audit by system/seed-engine", changes)
	}
	if strings.Contains(changes[0].Summary, "{") || strings.Contains(changes[0].Summary, `"`) {
		t.Errorf("the summary carries payloads, not ids: %q", changes[0].Summary)
	}
	// The fingerprint is the record's own canonical form — divergence
	// checks are content comparisons.
	h, present, err := ap.Fingerprint(ctx, nil, "gh")
	if err != nil || !present {
		t.Fatalf("fingerprint: %v", err)
	}
	if want := seed.Hash(mustCanonical(t, goodServerContent())); h != want {
		t.Errorf("fingerprint %s != record hash %s — the round trip broke", h, want)
	}
}

func TestRecordFailingTheValidatorIsRefusedNamingIt(t *testing.T) {
	s := openStore(t)
	sentence := "refused: server gh tool create_issue effect evil is not a class"
	ap := NewServersApplier(s, func(ctx context.Context, cand store.McpAll) error {
		for _, sv := range cand.Servers {
			if sv.Name == "gh" {
				for _, tl := range sv.Tools {
					if tl.Effect == "evil" {
						return errors.New(sentence)
					}
				}
			}
		}
		return nil
	}, fixedClock())
	ctx := context.Background()
	bad := goodServerContent()
	bad.Tools[1].Effect = "evil" // list_issues becomes un-validatable
	err := s.McpWithTx(ctx, func(tx *sql.Tx) error {
		return ap.Upsert(ctx, tx, recordOf(t, "gh", bad), false)
	})
	if err == nil {
		t.Fatal("a record the one validator refuses was ACCEPTED — the validator was bypassed")
	}
	if err.Error() != sentence {
		t.Fatalf("the validator's sentence must arrive VERBATIM, got %q", err.Error())
	}
	if _, present, _ := s.McpServerGet(ctx, "gh"); present {
		t.Error("a refused record left a row — refusal writes NOTHING")
	}
}

func TestNilValidatorFailsClosed(t *testing.T) {
	s := openStore(t)
	ap := NewServersApplier(s, nil, fixedClock())
	err := s.McpWithTx(context.Background(), func(tx *sql.Tx) error {
		return ap.Upsert(context.Background(), tx, recordOf(t, "gh", goodServerContent()), false)
	})
	if err == nil || !strings.Contains(err.Error(), "no validator") {
		t.Fatalf("without the seam the applier must fail CLOSED, got %v", err)
	}
}

func TestUserServerWithSameNameIsAConflict(t *testing.T) {
	s := openStore(t)
	ap := NewServersApplier(s, passValidator, fixedClock())
	ctx := context.Background()
	userRow := store.McpServerRow{
		Name: "gh", Tier: "public", TransportType: "stdio", Command: "mine",
		Args: "[]", Env: "[]", Enabled: true, Origin: "user",
		CreatedAt: "2026-11-01T00:00:00Z", UpdatedAt: "2026-11-01T00:00:00Z",
	}
	if err := s.McpWithTx(ctx, func(tx *sql.Tx) error {
		_, err := store.McpAddServerTx(ctx, tx, userRow)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if ok, err := ap.Adoptable(ctx, nil, "gh"); err != nil || ok {
		t.Fatalf("a USER row must never be adoptable (MG10: same-name is a conflict), got ok=%v err=%v", ok, err)
	}
	err := s.McpWithTx(ctx, func(tx *sql.Tx) error {
		return ap.Upsert(ctx, tx, recordOf(t, "gh", goodServerContent()), false)
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("the seed overwrote the question: %v — want ErrConflict", err)
	}
	cur, _, err := s.McpServerGet(ctx, "gh")
	if err != nil {
		t.Fatal(err)
	}
	if cur.Command != "mine" || cur.Origin != "user" || !cur.Enabled {
		t.Errorf("the operator's server was touched: %+v", cur)
	}
}

// TestMcpseedImportsNoMcpPackage is MC11 for the seed half: the record
// shapes are local copies; deleting internal/mcp must leave this package
// compiling.
func TestMcpseedImportsNoMcpPackage(t *testing.T) {
	fset := token.NewFileSet()
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("glob: %v (%d files)", err, len(files))
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue // tests may reach sideways; the SHIP code is the claim
		}
		af, err := parser.ParseFile(fset, f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		for _, im := range af.Imports {
			path, err := strconv.Unquote(im.Path.Value)
			if err != nil {
				continue
			}
			// The path is BUILT by concatenation: S12's absent_test walks every
			// Go file for the CONTIGUOUS quoted import path, and this guard
			// test quoting it as data would read as an importer (measured —
			// the same discipline as the policy guard's "fe"+"tch").
			if path == mcpShipPath || strings.HasPrefix(path, mcpShipPath+"/") {
				t.Errorf("%s imports %s — MC11: the seed carries plain local shapes", f, path)
			}
		}
	}
}

// TestEnabledFlipIsNotDivergenceForTheInertSet pins the Fingerprint
// choice: the guard set's enable of a system server must not read as the
// inert set's divergence (a re-apply would then refuse or undo a lawful
// widening).
func TestEnabledFlipIsNotDivergenceForTheInertSet(t *testing.T) {
	s := openStore(t)
	ap := NewServersApplier(s, passValidator, fixedClock())
	ctx := context.Background()
	rec := recordOf(t, "gh", goodServerContent())
	if err := s.McpWithTx(ctx, func(tx *sql.Tx) error { return ap.Upsert(ctx, tx, rec, false) }); err != nil {
		t.Fatal(err)
	}
	before, _, err := ap.Fingerprint(ctx, nil, "gh")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.McpWithTx(ctx, func(tx *sql.Tx) error {
		_, err := store.McpSetEnabledTx(ctx, tx, "gh", true, "2026-11-01T13:00:00Z")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	after, _, err := ap.Fingerprint(ctx, nil, "gh")
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Errorf("the guard set's flip diverged the inert set's row (%s != %s) — enabled is not inert content", before, after)
	}
}

// TestGrantRecordsLandDeclareAndEnable is the guard half of the machinery
// proof: the first real default is two records — a disabled definition and
// its widening — and both land through the same doors.
func TestGrantRecordsLandDeclareAndEnable(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	if err := s.CreateAgentAudited(context.Background(), store.AgentActor{Kind: store.AgentActorUser, ID: "1000"}, store.Agent{Slug: "tester", Name: "tester", Version: "1"}, "2026-11-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	sa := NewServersApplier(s, passValidator, fixedClock())
	ga := NewGrantsApplier(s, passValidator, fixedClock())
	if err := s.McpWithTx(ctx, func(tx *sql.Tx) error { return sa.Upsert(ctx, tx, recordOf(t, "gh", goodServerContent()), false) }); err != nil {
		t.Fatal(err)
	}
	decl := grantContent{Kind: grantDeclare, Agent: "tester", Qualified: "gh.list_issues"}
	if err := s.McpWithTx(ctx, func(tx *sql.Tx) error {
		return ga.Upsert(ctx, tx, recordOf(t, declareKey("tester", "gh.list_issues"), decl), false)
	}); err != nil {
		t.Fatalf("declare record: %v", err)
	}
	enable := grantContent{Kind: grantEnable, Server: "gh"}
	if err := s.McpWithTx(ctx, func(tx *sql.Tx) error {
		return ga.Upsert(ctx, tx, recordOf(t, enableKey("gh"), enable), false)
	}); err != nil {
		t.Fatalf("enable record: %v", err)
	}
	all, err := s.McpRows(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !all.Servers[0].Enabled {
		t.Error("the enable record did not switch the system server on")
	}
	found := false
	for _, d := range all.Declares {
		if d.AgentSlug == "tester" && d.Qualified == "gh.list_issues" {
			found = true
		}
	}
	if !found {
		t.Errorf("the declare record did not land a declaration: %+v", all.Declares)
	}
	// An enable of a USER server is refused (the guard set flips SYSTEM
	// servers only); an enable of a server that does not exist refuses
	// with the zero-fabrication law.
	if err := s.McpWithTx(ctx, func(tx *sql.Tx) error {
		_, err := store.McpAddServerTx(ctx, tx, store.McpServerRow{
			Name: "human", Tier: "public", TransportType: "stdio", Command: "x",
			Args: "[]", Env: "[]", Enabled: false, Origin: "user",
			CreatedAt: "2026-11-01T00:00:00Z", UpdatedAt: "2026-11-01T00:00:00Z",
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	err = s.McpWithTx(ctx, func(tx *sql.Tx) error {
		return ga.Upsert(ctx, tx, recordOf(t, enableKey("human"), grantContent{Kind: grantEnable, Server: "human"}), false)
	})
	if err == nil || !strings.Contains(err.Error(), "SYSTEM") {
		t.Fatalf("enabling a USER server must be refused, got %v", err)
	}
	err = s.McpWithTx(ctx, func(tx *sql.Tx) error {
		return ga.Upsert(ctx, tx, recordOf(t, enableKey("nope"), grantContent{Kind: grantEnable, Server: "nope"}), false)
	})
	if err == nil || !strings.Contains(err.Error(), "never fabricates") {
		t.Fatalf("enabling a missing server must refuse naming zero-fabrication, got %v", err)
	}
	// Key/content mismatch on a grant is refused (one fact per record).
	err = s.McpWithTx(ctx, func(tx *sql.Tx) error {
		return ga.Upsert(ctx, tx, recordOf(t, enableKey("gh"), decl), false)
	})
	if err == nil || !strings.Contains(err.Error(), "one grant") {
		t.Fatalf("key/content mismatch must be refused, got %v", err)
	}
}
