package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/cjlapao/aeon/internal/store"
)

// VerifyReport is one verify's honest answer: the level that was asked,
// whether it held, what it saw, and — for a drill — which migrations it
// applied. A report with OK false still carries the level it reached, so a
// surface can say "level 2 failed at the counts" rather than "failed".
type VerifyReport struct {
	FileName string
	Level    int
	OK       bool
	// NewerSchema is true when the manifest names a migration this binary
	// does not know: the file is readable (level 1 is honest about it) but
	// levels 2–3 refuse it, and it is never restorable by this binary.
	NewerSchema bool
	Unknown     []string
	Sentence    string
	// DrillApplied names the migrations the drill ran on the copy.
	DrillApplied []string
	// FileReport is level 2's checks on the extracted copy, when they ran.
	FileReport *store.FileReport
}

// Verify checks one archive at one of the three levels (BK5):
//
//	1 written — the archive opens, the manifest parses and validates, the
//	    members are exactly the two promised, the sizes match, the digest
//	    matches. (Refusals arrive as RunError with the container's sentence.)
//	2 verified — level 1, plus the extracted copy passes store.CheckFile
//	    and agrees with the manifest: integrity ok, no foreign-key
//	    violations, the schema fingerprint's digest equals the manifest's,
//	    and every table's count equals the manifest's.
//	3 drill — level 2, plus the copy is opened as a store with migrations
//	    enabled (a scratch directory, never the live state dir) and checked
//	    again: the backup is restorable by this binary, and the report
//	    names the migrations that ran.
//
// A newer-schema backup is refused at level 2 and 3 with the unknown
// migration named (the sentence says to use a newer aeon); level 1 still
// answers, because readability is a fact the file carries.
func Verify(ctx context.Context, d Deps, path string, level int) (VerifyReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	rep := VerifyReport{FileName: base(path), Level: level}
	if level < 1 || level > 3 {
		return rep, RunError{Kind: ErrKindUnknown, What: fmt.Sprintf("backup: verify level %d is not one of 1 (written), 2 (verified), 3 (drill)", level)}
	}
	if d.CheckFile == nil && level >= 2 {
		return rep, RunError{Kind: ErrKindUnknown, What: "backup: verify level 2 and 3 need Deps.CheckFile"}
	}

	// Level 1 — the container itself.
	r, err := Open(path)
	if err != nil {
		kind := ErrKindIO
		if strings.Contains(err.Error(), "checksum mismatch") {
			kind = ErrKindChecksum
		}
		return rep, RunError{Kind: kind, What: err.Error()}
	}
	defer func() { _ = r.Close() }()
	m := r.Manifest()

	// The newer-schema fact, computed at level 1: it gates levels 2–3 and
	// is a field on every report, so list/status and verify agree.
	rep.NewerSchema, rep.Unknown = newerSchema(m)
	if level >= 2 && rep.NewerSchema {
		rep.OK = false
		rep.Sentence = fmt.Sprintf("refused: this backup is from a newer schema — it names %s, which this binary does not know; levels 2 and 3 will not guess at a downgrade, and restore will not run: use a newer aeon", strings.Join(rep.Unknown, ", "))
		return rep, nil
	}

	if level == 1 {
		rep.OK = true
		rep.Sentence = fmt.Sprintf("ok: level 1 — the archive opens, the manifest validates, and the database member's %d bytes hash to the manifest's digest", m.DBBytes)
		return rep, nil
	}

	// Level 2 — the extracted copy against the manifest.
	rep2, err := d.CheckFile(ctx, r.DBPath())
	if err != nil {
		return rep, RunError{Kind: ErrKindIO, What: fmt.Sprintf("backup: the extracted copy will not check: %s", shortErr(err))}
	}
	rep.FileReport = &rep2
	if !rep2.IntegrityOK {
		first := "unknown"
		if len(rep2.IntegrityDetail) > 0 {
			first = strings.TrimSpace(rep2.IntegrityDetail[0])
		}
		rep.Sentence = fmt.Sprintf("failed: level 2 — the extracted copy failed integrity_check: %s", first)
		return rep, nil
	}
	if rep2.ForeignKeyViolations > 0 {
		rep.Sentence = fmt.Sprintf("failed: level 2 — the extracted copy carries %d foreign-key violations", rep2.ForeignKeyViolations)
		return rep, nil
	}
	fprSum := sha256.Sum256([]byte(rep2.Fingerprint.String()))
	if hex.EncodeToString(fprSum[:]) != m.SchemaFingerprintSHA {
		rep.Sentence = "failed: level 2 — the copy's schema fingerprint does not equal the manifest's: the container and its statement of itself disagree"
		return rep, nil
	}
	if bad, what := countsAgree(m.Tables, rep2.Tables); bad {
		rep.Sentence = fmt.Sprintf("failed: level 2 — %s", what)
		return rep, nil
	}
	rep.Sentence = fmt.Sprintf("ok: level 2 — integrity ok, no foreign-key violations, the fingerprint and all %d table counts equal the manifest's", len(m.Tables))

	if level == 2 {
		rep.OK = true
		return rep, nil
	}

	// Level 3 — the drill: open the copy as a store, migrate it to
	// current, check it again. The copy lives in a scratch directory
	// beside the archive; the live state dir is never opened.
	if d.OpenStore == nil {
		rep.OK = false
		rep.Sentence = "refused: level 3 needs Deps.OpenStore — the drill opens the copy as a store, and this process has no door to do it"
		return rep, nil
	}
	pre := rep2.Applied
	preSet := make(map[string]bool, len(pre))
	for _, n := range pre {
		preSet[n] = true
	}
	ds, oerr := d.OpenStore(r.Dir())
	if oerr != nil {
		rep.OK = false
		rep.Sentence = fmt.Sprintf("failed: level 3 — the drill will not open the copy: %s", shortErr(oerr))
		return rep, nil
	}
	defer func() { _ = ds.Close() }()
	post, perr := d.CheckFile(ctx, r.DBPath())
	if perr != nil || !post.IntegrityOK {
		rep.OK = false
		if perr != nil {
			rep.Sentence = fmt.Sprintf("failed: level 3 — the post-migration check will not run: %s", shortErr(perr))
		} else {
			rep.Sentence = "failed: level 3 — the copy failed integrity_check after the drill's migrations"
		}
		return rep, nil
	}
	if post.ForeignKeyViolations > 0 {
		rep.OK = false
		rep.Sentence = fmt.Sprintf("failed: level 3 — the copy carries %d foreign-key violations after the drill's migrations", post.ForeignKeyViolations)
		return rep, nil
	}
	for _, mig := range store.Migrations() {
		if !preSet[mig.Name] {
			rep.DrillApplied = append(rep.DrillApplied, mig.Name)
		}
	}
	sort.Strings(rep.DrillApplied)
	if len(rep.DrillApplied) == 0 {
		rep.Sentence = "ok: level 3 (drill) — the copy opened, the ledger was already at current, and the post-migration check holds"
	} else {
		rep.Sentence = fmt.Sprintf("ok: level 3 (drill) — the copy opened and the drill applied %s; the post-migration check holds", strings.Join(rep.DrillApplied, ", "))
	}
	rep.OK = true
	return rep, nil
}

// newerSchema answers whether the manifest names a migration this binary
// does not know. The manifest carries the applied count and the last name;
// a last name outside this binary's set, or a count above its total, is a
// schema ahead of this binary. Both answers are returned with the unknown
// names (or the count fact) in the sentence.
func newerSchema(m Manifest) (bool, []string) {
	known := store.KnownMigrationNames()
	if m.LastMigration != "" && !known[m.LastMigration] {
		return true, []string{m.LastMigration}
	}
	if m.AppliedMigrations > len(known) {
		return true, []string{fmt.Sprintf("(applied count %d is above this binary's %d migrations)", m.AppliedMigrations, len(known))}
	}
	return false, nil
}

// countsAgree compares the manifest's table counts with the checked
// copy's: the same set of names, the same count for each. It answers the
// first disagreement, named — a sentence that says "the counts disagree"
// without saying which is one the operator cannot act on.
func countsAgree(man, got map[string]int64) (bool, string) {
	for name, want := range man {
		have, ok := got[name]
		if !ok {
			return true, fmt.Sprintf("table %q is in the manifest (%d rows) but absent from the copy", name, want)
		}
		if have != want {
			return true, fmt.Sprintf("table %q has %d rows in the copy, the manifest says %d", name, have, want)
		}
	}
	for name := range got {
		if _, ok := man[name]; !ok {
			return true, fmt.Sprintf("table %q is in the copy but not in the manifest", name)
		}
	}
	return false, ""
}
