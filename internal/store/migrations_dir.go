package store

import (
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
)

// parseTimestampedMigrations reads every `*.sql` file directly under dir in
// fsys and turns it into a Migration — the collision-avoidance half of the
// legacy numbered block in schema.go's own Migrations(). A file's name IS
// its identity: `<14-digit-YYYYMMDDHHMMSS>_<slug>.sql`. Two worktrees adding
// a migration at the same time add two different files, which Git merges
// with no conflict — unlike two edits to the same literal slice.
//
// The timestamp is parsed as the Migration's ID (its real ordering key:
// migrationWorkers sorts by ID, and hands ID to the framework as both Order
// and Version). A file whose name does not start with 14 digits is refused
// loudly rather than silently skipped or sorted by discovery order — a
// migration with no stable ordering key is worse than one that fails to
// build.
//
// A leading `-- ` comment line becomes the Description `aeon db info`
// prints; everything else about the file, comment line included, stays in
// SQL verbatim (SQLite reads a leading `--` line as a no-op comment, so
// nothing is lost by leaving it in).
func parseTimestampedMigrations(fsys fs.FS, dir string) ([]Migration, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("store: read migrations directory %q: %w", dir, err)
	}

	var out []Migration
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		stem := strings.TrimSuffix(entry.Name(), ".sql")
		digits, _, ok := strings.Cut(stem, "_")
		if !ok || len(digits) != 14 {
			return nil, fmt.Errorf("store: migration file %q does not start with a 14-digit YYYYMMDDHHMMSS timestamp", entry.Name())
		}
		id, err := strconv.Atoi(digits)
		if err != nil {
			return nil, fmt.Errorf("store: migration file %q's timestamp is not numeric: %w", entry.Name(), err)
		}

		raw, err := fs.ReadFile(fsys, dir+"/"+entry.Name())
		if err != nil {
			return nil, fmt.Errorf("store: read migration file %q: %w", entry.Name(), err)
		}
		sql := string(raw)

		description := ""
		if firstLine, _, _ := strings.Cut(sql, "\n"); strings.HasPrefix(firstLine, "-- ") {
			description = strings.TrimPrefix(firstLine, "-- ")
		}

		out = append(out, Migration{ID: id, Name: stem, Description: description, SQL: sql})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
