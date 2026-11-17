package store

import (
	"database/sql"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// The schema fingerprint: a normalised, comparable description of the shape a
// database actually has.
//
// # Why not compare `sqlite_master.sql` text
//
// Because `ALTER TABLE … ADD COLUMN` rewrites the stored `CREATE TABLE` text by
// APPENDING to it, so an upgraded database and a fresh one hold different text
// for identical tables:
//
//	fresh     CREATE TABLE step (… cost_detail TEXT, context_manifest TEXT)
//	upgraded  CREATE TABLE step (… cost_detail TEXT) + ", context_manifest TEXT"
//
// A text comparison fails on every database that was ever migrated — which is
// every real one — so it would be switched off within a week. This describes
// the shape instead: columns with their types, defaults and nullability;
// indexes with their columns and uniqueness; foreign keys with their actions;
// and the CHECK expressions, which live nowhere but the text and so are pulled
// out of it and sorted.
//
// A missing UNIQUE, a dropped CASCADE and a lost CHECK are each visible here,
// and those are the three that matter: the first makes a concurrent append
// silently lossy, the second makes a cascade silently not happen, and the third
// makes a typo a policy that matches nothing.

// checkExpr finds every CHECK constraint in a CREATE statement.
var checkExpr = regexp.MustCompile(`(?is)CHECK\s*\(`)

// whitespace collapses runs of space, so formatting is not a difference.
var whitespace = regexp.MustCompile(`\s+`)

// Fingerprint describes the shape of a database, comparably.
type Fingerprint struct {
	// Lines is the normalised description, sorted. Comparing two fingerprints
	// is comparing these slices, and a difference reads as a diff rather than
	// as "not equal".
	Lines []string
}

// String renders the fingerprint for a failure message.
func (f Fingerprint) String() string { return strings.Join(f.Lines, "\n") }

// Equal reports whether two fingerprints describe the same shape.
func (f Fingerprint) Equal(other Fingerprint) bool {
	if len(f.Lines) != len(other.Lines) {
		return false
	}
	for i := range f.Lines {
		if f.Lines[i] != other.Lines[i] {
			return false
		}
	}
	return true
}

// Diff reports the lines present in one fingerprint and not the other, both
// ways round, so a failing test says WHAT drifted.
func (f Fingerprint) Diff(other Fingerprint) []string {
	have := make(map[string]bool, len(f.Lines))
	for _, l := range f.Lines {
		have[l] = true
	}
	theirs := make(map[string]bool, len(other.Lines))
	for _, l := range other.Lines {
		theirs[l] = true
	}
	var out []string
	for _, l := range f.Lines {
		if !theirs[l] {
			out = append(out, "only in the first:  "+l)
		}
	}
	for _, l := range other.Lines {
		if !have[l] {
			out = append(out, "only in the second: "+l)
		}
	}
	sort.Strings(out)
	return out
}

// SchemaFingerprint describes the shape of the store, for D4 and for
// `aeon db info --fingerprint`.
func (s *Store) SchemaFingerprint() (Fingerprint, error) { return fingerprint(s.r) }

// fingerprint walks the catalogue of a live database.
//
// The framework's own `_migrations` table is EXCLUDED: it records which
// migrations ran, so a fresh database and an upgraded one legitimately hold
// different rows, and GORM's AutoMigrate owns its shape rather than Aeon.
// Including it would make D4 compare the ledger instead of the schema.
func fingerprint(db *sql.DB) (Fingerprint, error) {
	var lines []string

	objects, err := catalogue(db)
	if err != nil {
		return Fingerprint{}, err
	}

	for _, obj := range objects {
		switch obj.kind {
		case "table":
			cols, err := columns(db, obj.name)
			if err != nil {
				return Fingerprint{}, err
			}
			lines = append(lines, cols...)

			fks, err := foreignKeys(db, obj.name)
			if err != nil {
				return Fingerprint{}, err
			}
			lines = append(lines, fks...)

			for _, c := range checks(obj.sql) {
				lines = append(lines, fmt.Sprintf("table %s check %s", obj.name, c))
			}

		case "index":
			idx, err := indexShape(db, obj.name)
			if err != nil {
				return Fingerprint{}, err
			}
			lines = append(lines, idx)
		}
	}

	sort.Strings(lines)
	return Fingerprint{Lines: lines}, nil
}

type catalogueEntry struct {
	kind string
	name string
	sql  string
}

func catalogue(db *sql.DB) ([]catalogueEntry, error) {
	rows, err := db.Query(`
		SELECT type, name, COALESCE(sql, '')
		FROM sqlite_master
		WHERE type IN ('table','index')
		  AND name NOT LIKE 'sqlite_%'
		  AND tbl_name <> '_migrations'
		ORDER BY type, name`)
	if err != nil {
		return nil, fmt.Errorf("store: read the catalogue: %w", err)
	}
	defer rows.Close()

	var out []catalogueEntry
	for rows.Next() {
		var e catalogueEntry
		if err := rows.Scan(&e.kind, &e.name, &e.sql); err != nil {
			return nil, fmt.Errorf("store: read the catalogue: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func columns(db *sql.DB, table string) ([]string, error) {
	rows, err := db.Query(fmt.Sprintf("SELECT cid, name, type, \"notnull\", COALESCE(dflt_value,''), pk FROM pragma_table_info(%q)", table))
	if err != nil {
		return nil, fmt.Errorf("store: read the columns of %s: %w", table, err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var cid, notNull, pk int
		var name, typ, dflt string
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			return nil, fmt.Errorf("store: read the columns of %s: %w", table, err)
		}
		// The ORDINAL is included deliberately. Column order is not cosmetic:
		// `INSERT INTO t VALUES (…)` without a column list binds by position,
		// and a migration that adds a column in a different place than a fresh
		// schema declares it would make the two disagree silently.
		out = append(out, fmt.Sprintf("table %s column %d %s %s notnull=%d default=%q pk=%d",
			table, cid, name, strings.ToUpper(typ), notNull, dflt, pk))
	}
	return out, rows.Err()
}

func foreignKeys(db *sql.DB, table string) ([]string, error) {
	rows, err := db.Query(fmt.Sprintf("SELECT id, seq, \"table\", COALESCE(\"from\",''), COALESCE(\"to\",''), on_update, on_delete FROM pragma_foreign_key_list(%q)", table))
	if err != nil {
		return nil, fmt.Errorf("store: read the foreign keys of %s: %w", table, err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var id, seq int
		var target, from, to, onUpdate, onDelete string
		if err := rows.Scan(&id, &seq, &target, &from, &to, &onUpdate, &onDelete); err != nil {
			return nil, fmt.Errorf("store: read the foreign keys of %s: %w", table, err)
		}
		out = append(out, fmt.Sprintf("table %s fk %s(%s) -> %s(%s) on_delete=%s on_update=%s",
			table, table, from, target, to, onDelete, onUpdate))
	}
	return out, rows.Err()
}

func indexShape(db *sql.DB, index string) (string, error) {
	var name, origin string
	var unique, partial int
	// pragma_index_list needs the TABLE, not the index, so the index's own
	// pragma is used and the table comes from the catalogue row.
	row := db.QueryRow(`SELECT l.name, l."unique", l.origin, l.partial, m.tbl_name
		FROM sqlite_master m
		JOIN pragma_index_list(m.tbl_name) l ON l.name = m.name
		WHERE m.name = ?`, index)
	var table string
	if err := row.Scan(&name, &unique, &origin, &partial, &table); err != nil {
		return "", fmt.Errorf("store: read the index %s: %w", index, err)
	}

	rows, err := db.Query(fmt.Sprintf("SELECT seqno, COALESCE(name,'<expr>'), desc FROM pragma_index_xinfo(%q) WHERE key = 1 ORDER BY seqno", index))
	if err != nil {
		return "", fmt.Errorf("store: read the columns of index %s: %w", index, err)
	}
	defer rows.Close()

	var cols []string
	for rows.Next() {
		var seqno, desc int
		var col string
		if err := rows.Scan(&seqno, &col, &desc); err != nil {
			return "", fmt.Errorf("store: read the columns of index %s: %w", index, err)
		}
		direction := "asc"
		if desc == 1 {
			direction = "desc"
		}
		cols = append(cols, col+" "+direction)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}

	return fmt.Sprintf("index %s on %s unique=%d partial=%d (%s)",
		name, table, unique, partial, strings.Join(cols, ", ")), nil
}

// checks pulls the CHECK expressions out of a CREATE statement.
//
// They exist ONLY in the stored text — no pragma reports them — so a CHECK
// silently dropped by a migration is invisible to every structural comparison
// that does not do this. Parentheses are matched by counting rather than by a
// regex, because a CHECK body contains parentheses: `CHECK (kind IN ('a','b'))`.
func checks(createSQL string) []string {
	var out []string
	for _, loc := range checkExpr.FindAllStringIndex(createSQL, -1) {
		depth := 0
		start := -1
		for i := loc[1] - 1; i < len(createSQL); i++ {
			switch createSQL[i] {
			case '(':
				if depth == 0 {
					start = i + 1
				}
				depth++
			case ')':
				depth--
				if depth == 0 && start >= 0 {
					body := whitespace.ReplaceAllString(strings.TrimSpace(createSQL[start:i]), " ")
					out = append(out, body)
					i = len(createSQL)
				}
			}
			if depth == 0 && start >= 0 {
				break
			}
		}
	}
	sort.Strings(out)
	return out
}
