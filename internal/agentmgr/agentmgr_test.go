package agentmgr

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/agentcard"
	"github.com/cjlapao/aeon/internal/store"
)

// openAgentStore opens a migrated store in a temp dir — the same shape the
// store's own tests use (the FTS5 build tag is required by migration 0040,
// so these tests run under -tags sqlite_fts5, like the store's).
func openAgentStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }, Clock: fixedClock()})
	if err != nil {
		t.Fatalf("open a store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func fixedClock() func() time.Time {
	return func() time.Time { return time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC) }
}

// boolOverride is the per-import-call override of agents.allowUnsignedImport the
// suites pass to NewManager (S44 T10: the production read is the config store;
// a test seam is a plain func that returns a fixed value, CS7/CS11).
func boolOverride(b bool) func(context.Context) bool {
	return func(context.Context) bool { return b }
}

// TestImportIsIdempotentBySlugAndVersion is the AM3 acceptance criterion: a
// repeated (slug, version) is a no-op, a higher version updates in place
// (never a second row), a lower version is refused and named.
func TestImportIsIdempotentBySlugAndVersion(t *testing.T) {
	s := openAgentStore(t)
	mgr := NewManager(s, fakeVerifier(agentcard.CardStateVerified), nil, boolOverride(false))
	ctx := context.Background()

	// First import: created.
	a, res, err := mgr.Import(ctx, testPerson, writeCard(t, store.Agent{Slug: "groot", Name: "Groot", Version: "1"}))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res != store.ImportResultCreated {
		t.Fatalf("first import: got %s, want created", res)
	}
	if a.Slug != "groot" {
		t.Fatalf("import: slug = %q, want groot", a.Slug)
	}

	// Same (slug, version): a no-op, not an error, not a second row.
	if _, res, err := mgr.Import(ctx, testPerson, writeCard(t, store.Agent{Slug: "groot", Name: "Groot", Version: "1"})); err != nil || res != store.ImportResultNoOp {
		t.Fatalf("same version: got %s, err %v; want no-op", res, err)
	}
	// Higher version: updated in place, still one row.
	if _, res, err := mgr.Import(ctx, testPerson, writeCard(t, store.Agent{Slug: "groot", Name: "Groot", Version: "2"})); err != nil || res != store.ImportResultUpdated {
		t.Fatalf("higher version: got %s, err %v; want updated", res, err)
	}
	// Lower version: refused, named — never silently accepted or ignored.
	if _, _, err := mgr.Import(ctx, testPerson, writeCard(t, store.Agent{Slug: "groot", Name: "Groot", Version: "1"})); err == nil {
		t.Fatalf("lower version: expected a refusal, got nil")
	}
	// Still exactly one row.
	all, err := s.AgentsAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("expected exactly one agent row, got %d", len(all))
	}
}

// TestTamperedCardIsRefused is the AM3 tamper criterion: a signature that does
// not verify is refused and named, never silently accepted.
func TestTamperedCardIsRefused(t *testing.T) {
	s := openAgentStore(t)
	mgr := NewManager(s, fakeVerifier(agentcard.CardStateTampered), nil, boolOverride(false))
	_, _, err := mgr.Import(context.Background(), testPerson, writeCard(t, store.Agent{Slug: "groot", Name: "Groot", Version: "1"}))
	if err == nil {
		t.Fatalf("tampered card: expected a refusal, got nil")
	}
	if all, _ := s.AgentsAll(); len(all) != 0 {
		t.Fatalf("a tampered card left a row behind: %d", len(all))
	}
}

// TestUnsignedRefusesThenImportsMarkedUnsigned is criterion 8: unsigned is
// refused by default; behind the override it is imported and marked unsigned.
func TestUnsignedRefusesThenImportsMarkedUnsigned(t *testing.T) {
	s := openAgentStore(t)
	// Default: refused.
	mgr := NewManager(s, fakeVerifier(agentcard.CardStateUnsigned), nil, boolOverride(false))
	if _, _, err := mgr.Import(context.Background(), testPerson, writeCard(t, store.Agent{Slug: "groot", Name: "Groot", Version: "1"})); err == nil {
		t.Fatalf("unsigned by default: expected a refusal, got nil")
	}
	// Behind the override: imported, marked unsigned.
	mgr2 := NewManager(s, fakeVerifier(agentcard.CardStateUnsigned), nil, boolOverride(true))
	a, res, err := mgr2.Import(context.Background(), testPerson, writeCard(t, store.Agent{Slug: "groot", Name: "Groot", Version: "1"}))
	if err != nil {
		t.Fatalf("unsigned with override: %v", err)
	}
	if res != store.ImportResultCreated {
		t.Fatalf("unsigned with override: got %s, want created", res)
	}
	if a.Signed {
		t.Fatalf("an unsigned import must be marked unsigned, not signed")
	}
}

// TestInternalRefusesEditAndDeleteButNotDuplication is AM1: an internal row
// refuses edit and delete, but duplication makes a new, editable, non-internal
// copy.
func TestInternalRefusesEditAndDeleteButNotDuplication(t *testing.T) {
	s := openAgentStore(t)
	mgr := NewManager(s, fakeVerifier(agentcard.CardStateVerified), nil, boolOverride(false))
	ctx := context.Background()
	if err := s.CreateAgentAudited(context.Background(), store.AgentActor{Kind: store.AgentActorUser, ID: "1000"}, store.Agent{Slug: "groot", Name: "Groot", Version: "1", Internal: true}, "2026-10-30T12:00:00Z"); err != nil {
		t.Fatal(err)
	}
	// Edit refused.
	if err := mgr.Edit(ctx, testPerson, "groot", store.Agent{Name: "Groot", Description: "changed"}, "2026-10-30T12:00:00Z"); err == nil {
		t.Fatalf("internal edit: expected a refusal, got nil")
	}
	// Delete refused.
	if err := mgr.Delete(ctx, testPerson, "groot"); err == nil {
		t.Fatalf("internal delete: expected a refusal, got nil")
	}
	// Duplication allowed — a new, editable, non-internal copy.
	dup, err := mgr.Duplicate(ctx, testPerson, "groot", "Groot-copy")
	if err != nil {
		t.Fatalf("duplicate: %v", err)
	}
	if dup.Internal {
		t.Fatalf("a duplicate must be non-internal (editable)")
	}
	if dup.Slug == "groot" {
		t.Fatalf("a duplicate must carry its own slug, not the source's")
	}
}

// TestSlugCollisionRefusesNeverSuffixes is AM2: a slug that already lives is
// refused and named, never silent-suffixed.
func TestSlugCollisionRefusesNeverSuffixes(t *testing.T) {
	s := openAgentStore(t)
	if err := s.CreateAgentAudited(context.Background(), store.AgentActor{Kind: store.AgentActorUser, ID: "1000"}, store.Agent{Slug: "groot", Name: "Groot", Version: "1"}, "2026-10-30T12:00:00Z"); err != nil {
		t.Fatal(err)
	}
	// A second agent whose name slugifies to the SAME slug is refused.
	if err := s.CreateAgentAudited(context.Background(), store.AgentActor{Kind: store.AgentActorUser, ID: "1000"}, store.Agent{Slug: "groot", Name: "Groot-again", Version: "1"}, "2026-10-30T12:00:00Z"); err == nil {
		t.Fatalf("slug collision: expected a refusal, got nil — a slug must never be silent-suffixed")
	}
}

// TestMigrateRepointsPolicyNamesToSlugs is moved to the store package
// (store/agent_migration_test.go) because it plants a policy row through the
// store's unexported writer, which the agentmgr package cannot reach. The
// agentmgr's Migrate calls the store's MigrateAgentPolicyReferences; the
// behaviour is proven there, and the wiring is a one-line call.

// fakeVerifier returns a Verifier that answers a fixed state — the test double
// for the card-verification seam (the real one is agentcard.VerifyCardSignature).

// fakeVerifier returns a Verifier that answers a fixed state — the test double
// for the card-verification seam (the real one is agentcard.VerifyCardSignature).
func fakeVerifier(state agentcard.CardState) func(card, certPEM []byte, sig []byte) (*agentcard.CardSignature, error) {
	return func(card, certPEM []byte, sig []byte) (*agentcard.CardSignature, error) {
		return &agentcard.CardSignature{State: state, Detail: "refused: the card was tampered with (test)"}, nil
	}
}

// writeCard writes a card file for the import to read. The envelope is
// S43's: a non-empty card body (with a name, so the import can map it) plus
// signature and certificate bytes — the fake verifier decides the state. The
// temp dir is captured ONCE (t.TempDir() is unique per call), so the write and
// the returned path are the same file.
func writeCard(t *testing.T, a store.Agent) string {
	t.Helper()
	dir := t.TempDir()
	body := struct {
		Slug    string `json:"slug"`
		Name    string `json:"name"`
		Version string `json:"version"`
	}{Slug: a.Slug, Name: a.Name, Version: a.Version}
	raw, err := json.Marshal(struct {
		Card        json.RawMessage `json:"card"`
		Signature   string          `json:"signature"`
		Certificate string          `json:"certificate"`
	}{Card: mustJSON(t, body), Signature: "sig", Certificate: "cert"})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "card.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// testPerson is the actor every write in this file is made by: S73 T3 put an
// actor on the six write methods, and the assertions below are exactly the
// ones S43 wrote — an actor threaded through plumbing, a law restated nowhere.
// (agentmgr_test.go carries the AM refusals; the store's own audited_test.go
// files are where the ledger rows are proved.)
var testPerson = store.AgentActor{Kind: store.AgentActorUser, ID: "1000"}
