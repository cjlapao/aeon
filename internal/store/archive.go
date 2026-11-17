package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

// `loop_archive` and `loop_archive_file`: cold storage.
//
// An archived loop is GONE from the live tables — the header plus its files are
// the whole record. Split in two so a manifest can be listed without inflating
// megabytes of content.
//
// # One transaction, because a half-archive has no owner
//
// An archive that copies the documents but leaves the loop live, or the
// reverse, is a state nothing reconciles: the loop list shows a loop whose
// documents are in cold storage, or cold storage holds a record of a loop that
// is still running. So the copy, the deletion and the header are one
// transaction, and a forced failure mid-way leaves the loop live and
// unarchived.

// Archive is an archived loop's header.
type Archive struct {
	ID            string `json:"id"`
	Slug          string `json:"slug"`
	ComponentRoot string `json:"componentRoot"`
	Title         string `json:"title"`
	Goal          string `json:"goal"`
	Status        string `json:"status"`
	StartedAt     string `json:"startedAt,omitempty"`
	EndedAt       string `json:"endedAt,omitempty"`
	ArchivedAt    string `json:"archivedAt"`
	StateBytes    int    `json:"stateBytes"`
	FileCount     int    `json:"fileCount"`
	FileBytes     int    `json:"fileBytes"`
	Note          string `json:"note,omitempty"`
}

// ArchiveFile is one archived document.
type ArchiveFile struct {
	Path     string `json:"path"`
	Bytes    int    `json:"bytes"`
	SHA256   string `json:"sha256"`
	Modified string `json:"modified,omitempty"`

	// Content is present only on a read of one archive, for the same reason as
	// Document.Content: a manifest that inflates every file is a manifest
	// nobody can list.
	Content string `json:"content,omitempty"`
}

// ArchiveLoop moves a loop and its documents into cold storage.
//
// `state` is the loop's live state as JSON — the header plus its tasks and
// phases — so an archive can be read back without the live tables. It is
// assembled here rather than by the caller, because a caller assembling it
// would decide what "the loop's state" means, and two callers would decide
// differently.
func (s *Store) ArchiveLoop(sc Scope, note string) (Archive, error) {
	if !sc.Valid() {
		return Archive{}, fmt.Errorf("store: ArchiveLoop needs both a slug and a component root, got %s", sc)
	}

	l, err := s.GetLoop(sc)
	if err != nil {
		return Archive{}, err
	}
	tasks, err := s.ListTasks(sc)
	if err != nil {
		return Archive{}, err
	}
	phases, err := s.Phases(sc)
	if err != nil {
		return Archive{}, err
	}
	docs, err := s.ListDocuments(sc)
	if err != nil {
		return Archive{}, err
	}

	state, err := json.Marshal(map[string]any{"loop": l, "tasks": tasks, "phases": phases})
	if err != nil {
		return Archive{}, fmt.Errorf("store: encode the state of %s: %w", sc, err)
	}

	// Content is read BEFORE the transaction opens, so the transaction holds
	// the single writer connection for as little time as possible — a loop with
	// a hundred revisions would otherwise block every other write while it
	// decompresses them.
	contents := make([]Document, 0, len(docs))
	fileBytes := 0
	for _, d := range docs {
		full, err := s.ReadDocument(sc, d.File)
		if err != nil {
			return Archive{}, err
		}
		contents = append(contents, full)
		fileBytes += full.Bytes
	}

	tx, err := s.w.Begin()
	if err != nil {
		return Archive{}, fmt.Errorf("store: archive %s: %w", sc, err)
	}
	defer func() { _ = tx.Rollback() }()

	a := Archive{
		ID:            newID(),
		Slug:          l.Slug,
		ComponentRoot: l.ComponentRoot,
		Title:         l.Title,
		Goal:          l.Goal,
		Status:        l.Status,
		StartedAt:     l.StartedAt,
		EndedAt:       s.Now(),
		ArchivedAt:    s.Now(),
		StateBytes:    len(state),
		FileCount:     len(contents),
		FileBytes:     fileBytes,
		Note:          note,
	}

	if _, err := tx.Exec(`
		INSERT INTO loop_archive (id, slug, component_root, title, goal, status, started_at, ended_at,
			archived_at, state, state_bytes, file_count, file_bytes, note)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		a.ID, a.Slug, a.ComponentRoot, a.Title, a.Goal, a.Status, nullable(a.StartedAt), nullable(a.EndedAt),
		a.ArchivedAt, state, a.StateBytes, a.FileCount, a.FileBytes, nullable(a.Note)); err != nil {
		return Archive{}, fmt.Errorf("store: write the archive header for %s: %w", sc, err)
	}

	for _, d := range contents {
		compressed, err := gzipBytes([]byte(d.Content))
		if err != nil {
			return Archive{}, fmt.Errorf("store: compress %s for the archive of %s: %w", d.File, sc, err)
		}
		if _, err := tx.Exec(`
			INSERT INTO loop_archive_file (archive_id, path, bytes, sha256, modified, content)
			VALUES (?,?,?,?,?,?)`,
			a.ID, d.File, d.Bytes, d.SHA256, nullable(d.WrittenAt), compressed); err != nil {
			return Archive{}, fmt.Errorf("store: archive %s of %s: %w", d.File, sc, err)
		}
	}

	// The loop goes last. Its cascades take its tasks, phases, steps, events,
	// memory and questions with it; the documents are deleted explicitly
	// because `loop_document` has no foreign key to `loop` (it outlives the
	// worktree a loop was created in, which is the reason v1 gave it none).
	if _, err := tx.Exec(`DELETE FROM loop_document WHERE component_root = ? AND loop_slug = ?`,
		sc.ComponentRoot, sc.LoopSlug); err != nil {
		return Archive{}, fmt.Errorf("store: remove the documents of %s: %w", sc, err)
	}
	if _, err := tx.Exec(`DELETE FROM loop WHERE component_root = ? AND slug = ?`,
		sc.ComponentRoot, sc.LoopSlug); err != nil {
		return Archive{}, fmt.Errorf("store: remove %s: %w", sc, err)
	}

	if err := tx.Commit(); err != nil {
		return Archive{}, fmt.Errorf("store: commit the archive of %s: %w", sc, err)
	}
	return a, nil
}

// ReadArchive reads an archive header and its manifest.
//
// `withContent` inflates the files, which a listing does not want and a restore
// does. One function with a flag rather than two, because the two would drift
// in what they consider part of an archive.
func (s *Store) ReadArchive(id string, withContent bool) (Archive, []ArchiveFile, error) {
	var a Archive
	var state []byte
	err := s.r.QueryRow(`
		SELECT id, slug, component_root, title, goal, status, COALESCE(started_at,''), COALESCE(ended_at,''),
			archived_at, state, state_bytes, file_count, file_bytes, COALESCE(note,'')
		FROM loop_archive WHERE id = ?`, id).
		Scan(&a.ID, &a.Slug, &a.ComponentRoot, &a.Title, &a.Goal, &a.Status, &a.StartedAt, &a.EndedAt,
			&a.ArchivedAt, &state, &a.StateBytes, &a.FileCount, &a.FileBytes, &a.Note)
	if errors.Is(err, sql.ErrNoRows) {
		return Archive{}, nil, fmt.Errorf("store: archive %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return Archive{}, nil, fmt.Errorf("store: read archive %s: %w", id, err)
	}

	rows, err := s.r.Query(`
		SELECT path, bytes, sha256, COALESCE(modified,''), content
		FROM loop_archive_file WHERE archive_id = ? ORDER BY path`, id)
	if err != nil {
		return Archive{}, nil, fmt.Errorf("store: read the files of archive %s: %w", id, err)
	}
	defer rows.Close()

	var files []ArchiveFile
	for rows.Next() {
		var f ArchiveFile
		var compressed []byte
		if err := rows.Scan(&f.Path, &f.Bytes, &f.SHA256, &f.Modified, &compressed); err != nil {
			return Archive{}, nil, fmt.Errorf("store: read the files of archive %s: %w", id, err)
		}
		if withContent {
			plain, err := ungzipBytes(compressed)
			if err != nil {
				return Archive{}, nil, fmt.Errorf("store: decompress %s of archive %s: %w", f.Path, id, err)
			}
			sum := sha256.Sum256(plain)
			if hex.EncodeToString(sum[:]) != f.SHA256 {
				return Archive{}, nil, fmt.Errorf("store: %s in archive %s does not match its stored sha256",
					f.Path, id)
			}
			f.Content = string(plain)
		}
		files = append(files, f)
	}
	return a, files, rows.Err()
}

// ListArchives lists archive headers, newest first.
func (s *Store) ListArchives(ps ComponentScope) ([]Archive, error) {
	where, args := ps.Where("")
	rows, err := s.r.Query(`
		SELECT id, slug, component_root, title, goal, status, COALESCE(started_at,''), COALESCE(ended_at,''),
			archived_at, state_bytes, file_count, file_bytes, COALESCE(note,'')
		FROM loop_archive WHERE `+where+` ORDER BY archived_at DESC`, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list archives: %w", err)
	}
	defer rows.Close()

	var out []Archive
	for rows.Next() {
		var a Archive
		if err := rows.Scan(&a.ID, &a.Slug, &a.ComponentRoot, &a.Title, &a.Goal, &a.Status,
			&a.StartedAt, &a.EndedAt, &a.ArchivedAt, &a.StateBytes, &a.FileCount, &a.FileBytes, &a.Note); err != nil {
			return nil, fmt.Errorf("store: list archives: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
