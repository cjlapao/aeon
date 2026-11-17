package loop

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/store"
)

// pin_check_test.go — CheckBlueprint is loop.Start's own judgement lent to
// S40 T8's trigger writes: same refusal sentences, byte for byte, and the
// check CREATES NOTHING — a refused blueprint must leave no loop row, which
// is what makes it a check rather than a start with amnesia.

func TestCheckBlueprintRefusesWhatStartRefusesAndCreatesNothing(t *testing.T) {
	s := openLoopStore(t)
	// S48 T3 (mechanical): names resolve from rows now (T5's seed feeds
	// production; a test store seeds what it asks about).
	seedRowForTest(t, s, "two-step")
	path := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(path, []byte("schemaVersion: 9"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"no-such-blueprint", path} {
		startErr := startRefusalForTest(t, s, name)
		checkErr := CheckBlueprint(s, name, "standard")
		if checkErr == nil || startErr == nil {
			t.Fatalf("blueprint %q: start said %v, check said %v — both must refuse", name, startErr, checkErr)
		}
		if checkErr.Error() != startErr.Error() {
			t.Fatalf("blueprint %q speaks two sentences:\n start: %v\n check: %v", name, startErr, checkErr)
		}
	}
	if _, err := startRefusalMaybe(t, s, "two-step"); err != nil {
		t.Fatalf("the standard two-step must pass the check: %v", err)
	}
	if err := CheckBlueprint(s, "two-step", "standard"); err != nil {
		t.Fatalf("CheckBlueprint(two-step) = %v — nil was the answer loop start itself gives", err)
	}
	loops, err := s.ListLoops(store.ComponentScope{ComponentRoot: "/repo"})
	if err != nil || len(loops) != 0 {
		t.Fatalf("%d loop rows after the refused checks: %v — a check that pins is a start wearing a disguise (the one accepted start lives under /repo-ok)", len(loops), err)
	}
}

// startRefusalForTest / startRefusalMaybe run the REAL Start so the
// comparison is with the door itself, not with a remembered echo of it.
func startRefusalForTest(t *testing.T, s *store.Store, name string) error {
	t.Helper()
	_, err := Start(context.Background(), s, store.NewLoop{Slug: "chk-" + filepath.Base(name), ComponentRoot: "/repo"}, name, nil, nil)
	return err
}

func startRefusalMaybe(t *testing.T, s *store.Store, name string) (LoopState, error) {
	t.Helper()
	return Start(context.Background(), s, store.NewLoop{Slug: "ok-" + name, ComponentRoot: "/repo-ok"}, name, nil, nil)
}

// seedRowForTest puts the shipped bytes of a standard into the test store
// as a row (the adoption shape), so a NAME asked of the row-backed door
// resolves. S48 T3 collateral: names no longer resolve from files.
func seedRowForTest(t *testing.T, s *store.Store, name string) {
	t.Helper()
	raw, ok, err := blueprint.EmbeddedSource().Get(name)
	if err != nil || !ok {
		t.Fatalf("shipped %s: %v", name, err)
	}
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])
	row := store.BlueprintRow{Name: name, Version: 1, Content: string(raw), ContentHash: hash, Origin: "system", UpdatedBy: "user:test"}
	rev := store.RevisionRow{Version: 1, Content: string(raw), ContentHash: hash, Op: "adopt", ActorKind: "user", ActorID: "test"}
	if err := s.BlueprintWithTx(context.Background(), func(tx *sql.Tx) error {
		return store.BlueprintPutTx(tx, row, rev, nil)
	}); err != nil {
		t.Fatal(err)
	}
}
