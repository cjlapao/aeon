package main

// blueprint_adopt.go — S48 T4 (BP3): adoption, the one-time carry of the
// legacy state-directory files into the database. The rules that make it
// safe: per FILE, per TRANSACTION, idempotent by HASH, invalid files are
// REPORTED (a ledger row, never silence), files are NEVER touched, and an
// existing row is never merged into or overwritten. The daemon never reads
// an arbitrary path — this runs at serve, where the directory is Aeon's
// own state directory, and explicit import stays `save --file` (T11).
//
// Classification (BP6 in the file era):
//   - pristine  — a standard name whose bytes hash to the current embedded
//     bytes or a recognised shipped revision ⇒ system row;
//   - edited standard — a standard name, other bytes ⇒ a SYSTEM row holding
//     THE OPERATOR'S TEXT (their edits survive; only seed.restore reverts);
//   - custom — any other valid name ⇒ user row.

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/action"
	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/blueprintseed"
	"github.com/cjlapao/aeon/internal/handler"
	"github.com/cjlapao/aeon/internal/paths"
	"github.com/cjlapao/aeon/internal/store"
)

// adoptFailBefore names a file whose adoption transaction must fail —
// empty in production, set only by TestAdoptOneTransactionPerFile.
var adoptFailBefore string

const (
	adoptMaxFile = 1 << 20 // the schema's own content bound: 1 MiB
	adoptMaxDiag = 4096    // the ledger's detail bound
)

// AdoptNote is one non-imported file with the reason it was not imported.
type AdoptNote struct {
	Name   string
	Reason string
}

// AdoptReport is the adoption run's honest tally.
type AdoptReport struct {
	Pristine int
	Edited   int
	Custom   int
	Already  int
	Refused  []AdoptNote
	Skipped  []AdoptNote
}

// Line is the one log line serve speaks (and doctor paraphrases).
func (r AdoptReport) Line() string {
	return fmt.Sprintf("blueprints: adopted %d (%d pristine, %d edited, %d custom), refused %d, skipped %d",
		r.Pristine+r.Edited+r.Custom, r.Pristine, r.Edited, r.Custom, len(r.Refused), len(r.Skipped))
}

// adoptFiles walks <store dir>/blueprints/ and carries each file into the
// database once. It never writes, renames, or deletes a file.
func adoptFiles(ctx context.Context, st *store.Store, dir string) (AdoptReport, error) {
	r := AdoptReport{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return r, nil // no directory: nothing to adopt, not a fault
		}
		return r, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, e := range entries { // ReadDir sorts by name: deterministic order
		file := filepath.Join(dir, e.Name())
		name := strings.TrimSuffix(e.Name(), ".yaml")
		// (1) only regular *.yaml — abuse is listed, never silently passed.
		if e.IsDir() {
			r.Skipped = append(r.Skipped, AdoptNote{name, "not-a-file (subdirectories are not adopted)"})
			continue
		}
		if fi, err := os.Lstat(file); err != nil {
			r.Skipped = append(r.Skipped, AdoptNote{name, "cannot stat: " + err.Error()})
			continue
		} else if fi.Mode()&os.ModeSymlink != 0 {
			r.Skipped = append(r.Skipped, AdoptNote{name, "symlink (a blueprint file is a regular file)"})
			continue
		} else if !strings.HasSuffix(e.Name(), ".yaml") {
			r.Skipped = append(r.Skipped, AdoptNote{name, "not a .yaml file"})
			continue
		} else if fi.Size() > adoptMaxFile {
			r.Skipped = append(r.Skipped, AdoptNote{name, fmt.Sprintf("oversize (%d bytes > the 1 MiB content bound)", fi.Size())})
			continue
		}
		if !blueprint.ValidBlueprintName(name) {
			r.Skipped = append(r.Skipped, AdoptNote{e.Name(), "name fails the grammar ([a-z0-9][a-z0-9_-]*, ≤64)"})
			continue
		}
		raw, err := os.ReadFile(file)
		if err != nil {
			r.Skipped = append(r.Skipped, AdoptNote{name, "cannot read: " + err.Error()})
			continue
		}
		sum := sha256.Sum256(raw)
		hash := hex.EncodeToString(sum[:])
		// (2) idempotent by hash: a file already seen is never re-adopted.
		if _, ok, err := st.ImportByHash(ctx, hash); err != nil {
			return r, err
		} else if ok {
			r.Already++
			continue
		}
		// (3) invalid ⇒ one refused ledger row with the diagnostics — never
		// silent, and one bad file never blocks the others.
		report := blueprint.Analyze(raw, adoptOptions())
		if !report.OK(false) {
			if err := adoptLedger(st, store.ImportRow{Path: file, SHA256: hash, Name: name,
				Result: "refused", Detail: adoptDiag(report), ImportedAt: now}); err != nil {
				return r, err
			}
			r.Refused = append(r.Refused, AdoptNote{name, "invalid (ledger row written)"})
			continue
		}
		// (5) a file that collides with a row is SKIPPED: no merge, no
		// overwrite — the row is the database's truth, the file is a
		// stranger claiming the same name.
		if _, ok, err := st.BlueprintGet(ctx, name); err != nil {
			return r, err
		} else if ok {
			r.Skipped = append(r.Skipped, AdoptNote{name, "a row already holds this name (never merged)"})
			continue
		}
		// (3') classify.
		origin, updatedBy := "user", "system:adoption"
		pristine := false
		if isStandard(name) {
			origin, updatedBy = "system", "system:adoption"
			pristine = adoptHashIsPristine(name, hash)
		}
		bp, err := blueprint.Load(raw)
		if err != nil {
			// Analyze said ok; Load is the exec-grade reader — a refusal
			// HERE is the same class: reported, ledgered, file untouched.
			if lerr := adoptLedger(st, store.ImportRow{Path: file, SHA256: hash, Name: name,
				Result: "refused", Detail: "load: " + err.Error(), ImportedAt: now}); lerr != nil {
				return r, lerr
			}
			r.Refused = append(r.Refused, AdoptNote{name, "refused at load: " + err.Error()})
			continue
		}
		content := string(raw)
		row := store.BlueprintRow{Name: name, Version: 1, Content: content, ContentHash: hash,
			Origin: origin, UpdatedBy: updatedBy}
		rev := store.RevisionRow{Version: 1, Content: content, ContentHash: hash,
			Op: "adopt", ActorKind: "system", ActorID: "startup", OccurredAt: now}
		var refs []store.RefRow
		for _, ref := range blueprint.References(&bp) {
			refs = append(refs, store.RefRow{Name: name, Kind: ref.Kind, Target: ref.Target})
		}
		// (4) ONE transaction per file: row + revision + refs + ledger.
		// adoptFailBefore is the test seam (TestAdoptOneTransactionPerFile):
		// set to a name, that file's transaction dies before any write.
		imported := false
		if err := st.BlueprintWithTx(ctx, func(tx *sql.Tx) error {
			if adoptFailBefore == name {
				return fmt.Errorf("adopt: forced failure (test)")
			}
			if err := store.BlueprintPutTx(tx, row, rev, refs); err != nil {
				return err
			}
			if err := store.ImportInsertTx(tx, store.ImportRow{Path: file, SHA256: hash, Name: name,
				Result: "imported", Detail: classify(origin, pristine), ImportedAt: now}); err != nil {
				return err
			}
			imported = true
			return nil
		}); err != nil {
			return r, fmt.Errorf("adopting %s: %w", name, err)
		}
		if !imported {
			continue
		}
		switch {
		case origin == "user":
			r.Custom++
		case pristine:
			r.Pristine++
		default:
			r.Edited++
		}
	}
	return r, nil
}

func isStandard(name string) bool {
	for _, n := range blueprint.StandardNames() {
		if n == name {
			return true
		}
	}
	return false
}

// adoptHashIsPristine: current embedded bytes or a recognised shipped
// revision. The list lives in blueprintseed — the set that owns the
// shipped bytes owns the hashes too (S48 T5); this reader never re-hashes
// a guess.
func adoptHashIsPristine(name, hash string) bool {
	if raw, ok, err := blueprint.EmbeddedSource().Get(name); err == nil && ok {
		sum := sha256.Sum256(raw)
		if hex.EncodeToString(sum[:]) == hash {
			return true
		}
	}
	return blueprintseed.KnownPristineHash(hash)
}

// adoptOptions are the save door's options verbatim — adoption admits
// exactly what save admits, never more.
func adoptOptions() blueprint.ValidationOptions {
	return blueprint.ValidationOptions{Mode: "standard", Agents: handler.Names(),
		Resolvers: handler.Resolvers(), Actions: action.Names(), AgentTools: handler.ToolsFor}
}

func adoptDiag(r blueprint.Report) string {
	parts := make([]string, 0, len(r.Diagnostics))
	for _, d := range r.Diagnostics {
		parts = append(parts, fmt.Sprintf("%s %s %s: %s", d.State, d.Kind, d.Path, d.Msg))
	}
	s := strings.Join(parts, "\n")
	if len(s) > adoptMaxDiag {
		s = s[:adoptMaxDiag]
	}
	return s
}

func classify(origin string, pristine bool) string {
	switch {
	case origin == "user":
		return "custom"
	case pristine:
		return "pristine"
	default:
		return "edited-standard"
	}
}

func adoptLedger(st *store.Store, row store.ImportRow) error {
	return st.BlueprintWithTx(context.Background(), func(tx *sql.Tx) error {
		return store.ImportInsertTx(tx, row)
	})
}

// adoptFilesStoreDir adopts from Aeon's own state directory — the ONLY
// directory adoption ever reads; the daemon takes no path from a caller
// (BP3).
func adoptFilesStoreDir(st *store.Store) (AdoptReport, error) {
	dir, err := paths.StoreDir()
	if err != nil {
		return AdoptReport{}, err
	}
	return adoptFiles(context.Background(), st, filepath.Join(dir, "blueprints"))
}

// blueprintStartup is serve's blueprint order, extracted so the ORDER is a
// tested fact: adoption first (files are the old truth), then the seed set
// (T5), and only then loops may be accepted. A step that fails stops the
// walk — loops are never accepted on a half-carried store.
func blueprintStartup(adopt, seed, acceptLoops func() error) error {
	for _, step := range []func() error{adopt, seed, acceptLoops} {
		if step == nil {
			continue
		}
		if err := step(); err != nil {
			return err
		}
	}
	return nil
}
