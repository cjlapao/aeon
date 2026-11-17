package store

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

// `loop_document`: the loop's deliverables as rows, and the one sharp edge in
// this file — path confinement.
//
// # The confinement is a security property, not a convenience
//
// A document name reaching this store COMES FROM A MODEL. An absolute path or a
// `..` segment must be REFUSED, never normalised into range: a silent clamp
// turns an escape attempt into a successful write somewhere unexpected, and it
// looks exactly like the feature working.
//
// Every refusal is a discriminated RESULT (DB19) with its own reason, and there
// is one test per reason, each proving refusal BY ATTEMPT rather than by
// reading the code.
//
// The check runs on the RAW INPUT, before any normalisation. A path cleaned
// first and checked second is a path whose escape has already been resolved
// away — the check then passes and the write lands wherever the cleaning
// decided.
//
// # The store writes no files (DB10)
//
// v1's `loop_writeFile` also mirrored the document to
// `{componentRoot}/.projects/loops/{slug}/`. That mirror is disposable and is not
// built here: the store is the source of truth. A test greps this package for
// filesystem calls and allows them only in open.go, which creates the state
// directory.

// KnownFiles maps a friendly name to its filename, so a caller never types a
// path or an extension. Ported from v1's `known-files.ts`.
//
// A friendly name WINS over anything path-like, so `plan` can never be
// mistaken for a directory called plan.
func KnownFiles() map[string]string {
	return map[string]string{
		"loop":                   "loop.md",
		"prompt":                 "prompt.md",
		"state":                  "state.md",
		"todo":                   "todo.md",
		"discovery":              "discovery.md",
		"legal":                  "legal.md",
		"plan":                   "plan.md",
		"security":               "security.md",
		"memory":                 "memory.md",
		"events":                 "events.jsonl",
		"unresolved-pr-comments": "unresolved-pr-comments.md",
	}
}

// AllowedDirs are the only subdirectories a document may live in. From v1.
func AllowedDirs() []string { return []string{"tasks", "scratch"} }

// AllowedExtensions is the extension set. From v1.
func AllowedExtensions() []string { return []string{".md", ".jsonl", ".txt"} }

// MaxDocumentBytes bounds a document.
//
// Two megabytes, v1's number, carried rather than re-derived. It is a guess
// there too; what it protects against is a model writing a runaway file into a
// row that then has to be read whole on every resume.
const MaxDocumentBytes = 2 * 1024 * 1024

// DocumentRefusal names why a document name was refused.
//
// A string enum rather than an error, so a caller can branch on the REASON and
// the CLI can render each differently. The five values are exactly the five
// cases D14 requires proving by attempt.
type DocumentRefusal string

const (
	// RefusalNone means the name resolved.
	RefusalNone DocumentRefusal = ""
	// RefusalEmpty — no name at all.
	RefusalEmpty DocumentRefusal = "empty"
	// RefusalAbsolute — an absolute path, POSIX or Windows-shaped.
	RefusalAbsolute DocumentRefusal = "absolute"
	// RefusalTraversal — any `..` segment, or a NUL byte.
	RefusalTraversal DocumentRefusal = "traversal"
	// RefusalDirectory — a directory outside AllowedDirs, or nested deeper
	// than one level.
	RefusalDirectory DocumentRefusal = "directory"
	// RefusalExtension — an extension outside AllowedExtensions.
	RefusalExtension DocumentRefusal = "extension"
	// RefusalTooLarge — content over MaxDocumentBytes.
	RefusalTooLarge DocumentRefusal = "too-large"
)

// ResolvedName is the outcome of resolving a document name.
type ResolvedName struct {
	// Name is the stored name — `plan.md`, `tasks/T1.md` — when OK.
	Name string

	// Refusal is RefusalNone when the name resolved.
	Refusal DocumentRefusal

	// Reason says what to do about it. Rendered to a person, so it names the
	// allowed set rather than only the violation.
	Reason string
}

// OK reports whether the name resolved.
func (r ResolvedName) OK() bool { return r.Refusal == RefusalNone }

// ResolveDocumentName resolves a friendly name or a confined path.
//
// Checked in this order, on the RAW input: empty, NUL, absolute, traversal,
// directory, extension. Nothing is cleaned, joined or normalised at any point —
// the returned name is assembled from segments that were each checked.
func ResolveDocumentName(raw string) ResolvedName {
	name := strings.TrimSpace(raw)
	if name == "" {
		return ResolvedName{Refusal: RefusalEmpty,
			Reason: "a document needs a name. Use a known name (" + knownNamesList() + ") or a path like tasks/T1.md"}
	}
	if strings.ContainsRune(name, 0) {
		return ResolvedName{Refusal: RefusalTraversal,
			Reason: "a document name cannot contain a NUL byte"}
	}

	// S69: the attachment namespace is reserved. Attachments are authored by
	// the attachment door (PutAttachment) and read by it (ReadAttachment);
	// the document door neither writes nor reads there, so a receipt's
	// pinned revision can never be edited by somebody who typed a plan.md.
	if strings.HasPrefix(strings.ReplaceAll(name, "\\", "/"), AttachmentPrefix) {
		return ResolvedName{Refusal: RefusalDirectory,
			Reason: name + " lives under " + AttachmentPrefix + ", which is reserved for attachments; the attachment doors own it"}
	}

	// A friendly name wins, before anything path-shaped is considered.
	if known, ok := KnownFiles()[strings.ToLower(name)]; ok {
		return ResolvedName{Name: known}
	}

	// Backslashes are treated as separators as well as slashes, so a Windows
	// path cannot smuggle a segment past a check that only splits on `/`.
	unified := strings.ReplaceAll(name, `\`, "/")

	if strings.HasPrefix(unified, "/") || windowsDriveAbsolute(unified) {
		return ResolvedName{Refusal: RefusalAbsolute,
			Reason: "a document must be a name inside the loop, not an absolute path (got " + quote(raw) +
				"). Use a known name (" + knownNamesList() + ") or a path like tasks/T1.md"}
	}

	segments := strings.Split(unified, "/")
	for _, seg := range segments {
		if seg == ".." {
			return ResolvedName{Refusal: RefusalTraversal,
				Reason: "a document name cannot contain a .. segment (got " + quote(raw) +
					"). It is refused rather than resolved, because resolving it would write somewhere else"}
		}
		if seg == "" || seg == "." {
			return ResolvedName{Refusal: RefusalDirectory,
				Reason: "a document name cannot contain an empty or . segment (got " + quote(raw) + ")"}
		}
	}

	switch len(segments) {
	case 1:
		// A top-level file.
	case 2:
		if !containsString(AllowedDirs(), segments[0]) {
			return ResolvedName{Refusal: RefusalDirectory,
				Reason: "only top-level files and " + strings.Join(AllowedDirs(), "/, ") +
					"/ are addressable (got " + quote(raw) + ")"}
		}
	default:
		return ResolvedName{Refusal: RefusalDirectory,
			Reason: "a document may be nested one level at most, under " + strings.Join(AllowedDirs(), "/, ") +
				"/ (got " + quote(raw) + ")"}
	}

	if !extensionAllowed(segments[len(segments)-1]) {
		return ResolvedName{Refusal: RefusalExtension,
			Reason: "unsupported file type " + quote(raw) + " (allowed: " +
				strings.Join(AllowedExtensions(), ", ") + ")"}
	}
	return ResolvedName{Name: strings.Join(segments, "/")}
}

func windowsDriveAbsolute(s string) bool {
	if len(s) < 3 {
		return false
	}
	c := s[0]
	isLetter := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
	return isLetter && s[1] == ':' && s[2] == '/'
}

func extensionAllowed(name string) bool {
	dot := strings.LastIndex(name, ".")
	if dot <= 0 {
		return false
	}
	return containsString(AllowedExtensions(), strings.ToLower(name[dot:]))
}

func containsString(in []string, want string) bool {
	for _, v := range in {
		if v == want {
			return true
		}
	}
	return false
}

func knownNamesList() string {
	names := make([]string, 0, len(KnownFiles()))
	for k := range KnownFiles() {
		names = append(names, k)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// Document is one revision of one document.
type Document struct {
	ID            string `json:"id"`
	ComponentRoot string `json:"componentRoot"`
	LoopSlug      string `json:"loopSlug"`
	File          string `json:"file"`
	Rev           int    `json:"rev"`
	Bytes         int    `json:"bytes"`
	SHA256        string `json:"sha256"`
	WrittenBy     string `json:"writtenBy,omitempty"`
	WrittenAt     string `json:"writtenAt"`

	// OwnerGoal is the goal that owns this document ("" while no goal does):
	// set when a loop that works a goal writes, and when a loop files a goal
	// (document_owner.go). The loop keeps its own rows either way.
	OwnerGoal string `json:"ownerGoal,omitempty"`

	// Content is the decompressed text, present only on a read of one
	// document. A listing leaves it empty rather than inflating every
	// revision, which is why the manifest and the content are separate reads.
	Content string `json:"content,omitempty"`
}

// WriteResult is the outcome of a write.
//
// A refusal is a result (DB19), and `Unchanged` is a third outcome rather than
// a success with a side note: a write whose content matches the current head is
// DROPPED rather than stored twice, and a caller that believes it created a
// revision would show the wrong history.
type WriteResult struct {
	OK        bool
	Unchanged bool
	Document  Document
	Refusal   DocumentRefusal
	Reason    string
}

// WriteDocument stores a revision, refusing a name it will not accept.
//
// Append-only by revision: `rev` starts at 1, and a write whose sha matches the
// current head is dropped. "What did the plan say before the split" is usually
// the question worth answering, and overwriting makes it unanswerable.
func (s *Store) WriteDocument(sc Scope, rawName, content, writtenBy string) (WriteResult, error) {
	if !sc.Valid() {
		return WriteResult{}, fmt.Errorf("store: WriteDocument needs both a slug and a component root, got %s", sc)
	}

	resolved := ResolveDocumentName(rawName)
	if !resolved.OK() {
		return WriteResult{Refusal: resolved.Refusal, Reason: resolved.Reason}, nil
	}
	if len(content) > MaxDocumentBytes {
		return WriteResult{
			Refusal: RefusalTooLarge,
			Reason:  fmt.Sprintf("%s is %d bytes; the limit is %d", resolved.Name, len(content), MaxDocumentBytes),
		}, nil
	}

	sum := sha256.Sum256([]byte(content))
	digest := hex.EncodeToString(sum[:])

	tx, err := s.w.Begin()
	if err != nil {
		return WriteResult{}, fmt.Errorf("store: write %s in %s: %w", resolved.Name, sc, err)
	}
	defer func() { _ = tx.Rollback() }()

	var headRev int
	var headSHA string
	err = tx.QueryRow(`
		SELECT rev, sha256 FROM loop_document
		WHERE component_root = ? AND loop_slug = ? AND file = ?
		ORDER BY rev DESC LIMIT 1`, sc.ComponentRoot, sc.LoopSlug, resolved.Name).Scan(&headRev, &headSHA)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return WriteResult{}, fmt.Errorf("store: read the head of %s in %s: %w", resolved.Name, sc, err)
	}
	if headSHA == digest {
		doc, readErr := s.ReadDocument(sc, resolved.Name)
		if readErr != nil {
			return WriteResult{}, readErr
		}
		return WriteResult{OK: true, Unchanged: true, Document: doc}, nil
	}

	doc, err := s.writeDocumentInTx(tx, sc, resolved.Name, content, writtenBy)
	if err != nil {
		return WriteResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return WriteResult{}, fmt.Errorf("store: commit %s in %s: %w", resolved.Name, sc, err)
	}
	_ = doc
	doc.Content = content
	return WriteResult{OK: true, Document: doc}, nil
}

// writeDocumentInTx is the insert half of a document write, callable inside a
// caller's transaction (S38 T7: a manual task's body and its attachment row
// must land together, or neither does). The name is resolved and the size
// bounded here too — the door is the same door whoever calls.
func (s *Store) writeDocumentInTx(tx *sql.Tx, sc Scope, rawName, content, writtenBy string) (Document, error) {
	resolved := ResolveDocumentName(rawName)
	if !resolved.OK() {
		return Document{}, fmt.Errorf("store: %s", resolved.Reason)
	}
	if len(content) > MaxDocumentBytes {
		return Document{}, fmt.Errorf("store: %s is %d bytes; the limit is %d", resolved.Name, len(content), MaxDocumentBytes)
	}
	return s.insertDocumentRevision(tx, sc, resolved.Name, content, writtenBy)
}

// insertDocumentRevision is the ONE writer of a loop_document revision:
// head+1, gzip, digest, insert — shared by the document door and the
// attachment door, because a second copy of this is a second truth about
// what "a frozen revision" means. The file name arrives already validated
// by the calling door (the resolver for documents, attachmentNameSafe plus
// the reserved prefix for attachments); caps belong to the doors.
func (s *Store) insertDocumentRevision(tx *sql.Tx, sc Scope, file, content, writtenBy string) (Document, error) {
	sum := sha256.Sum256([]byte(content))
	digest := hex.EncodeToString(sum[:])
	var headRev int
	if err := tx.QueryRow(`SELECT rev FROM loop_document
		WHERE component_root = ? AND loop_slug = ? AND file = ?
		ORDER BY rev DESC LIMIT 1`, sc.ComponentRoot, sc.LoopSlug, file).Scan(&headRev); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Document{}, fmt.Errorf("store: read the head of %s in %s: %w", file, sc, err)
	}
	compressed, err := gzipBytes([]byte(content))
	if err != nil {
		return Document{}, fmt.Errorf("store: compress %s in %s: %w", file, sc, err)
	}

	owner, err := documentOwnerInTx(tx, sc, file)
	if err != nil {
		return Document{}, err
	}
	doc := Document{
		ID:            newID(),
		ComponentRoot: sc.ComponentRoot,
		LoopSlug:      sc.LoopSlug,
		File:          file,
		Rev:           headRev + 1,
		Bytes:         len(content),
		SHA256:        digest,
		WrittenBy:     writtenBy,
		WrittenAt:     s.Now(),
		OwnerGoal:     owner,
	}
	if _, err := tx.Exec(`
		INSERT INTO loop_document (id, component_root, loop_slug, file, rev, content, bytes, sha256, written_by, written_at, owner_goal_id)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		doc.ID, doc.ComponentRoot, doc.LoopSlug, doc.File, doc.Rev, compressed,
		doc.Bytes, doc.SHA256, nullable(doc.WrittenBy), doc.WrittenAt, nullable(owner)); err != nil {
		return Document{}, fmt.Errorf("store: write %s in %s: %w", file, sc, err)
	}
	doc.Content = content
	return doc, nil
}

// ReadDocument reads the head revision of one document.
//
// The name goes through the same resolver as a write, so `plan` reads back what
// `plan` wrote and a refused name is refused on the way in as well as out.
func (s *Store) ReadDocument(sc Scope, rawName string) (Document, error) {
	resolved := ResolveDocumentName(rawName)
	if !resolved.OK() {
		return Document{}, fmt.Errorf("store: %s", resolved.Reason)
	}

	var doc Document
	var compressed []byte
	err := s.r.QueryRow(`
		SELECT id, component_root, loop_slug, file, rev, content, bytes, sha256, COALESCE(written_by,''), written_at, COALESCE(owner_goal_id,'')
		FROM loop_document
		WHERE component_root = ? AND loop_slug = ? AND file = ?
		ORDER BY rev DESC LIMIT 1`, sc.ComponentRoot, sc.LoopSlug, resolved.Name).
		Scan(&doc.ID, &doc.ComponentRoot, &doc.LoopSlug, &doc.File, &doc.Rev, &compressed,
			&doc.Bytes, &doc.SHA256, &doc.WrittenBy, &doc.WrittenAt, &doc.OwnerGoal)
	if errors.Is(err, sql.ErrNoRows) {
		return Document{}, fmt.Errorf("store: %s has no %s: %w", sc, resolved.Name, ErrNotFound)
	}
	if err != nil {
		return Document{}, fmt.Errorf("store: read %s in %s: %w", resolved.Name, sc, err)
	}

	plain, err := ungzipBytes(compressed)
	if err != nil {
		return Document{}, fmt.Errorf("store: decompress %s in %s: %w", resolved.Name, sc, err)
	}
	// The digest covers the RAW bytes, matching loop_archive_file so both
	// stores verify the same way. Checked on read, because a document whose
	// content does not match its hash is corruption and reporting it here is
	// the only place anybody would notice.
	sum := sha256.Sum256(plain)
	if hex.EncodeToString(sum[:]) != doc.SHA256 {
		return Document{}, fmt.Errorf("store: %s in %s does not match its stored sha256; the row is corrupt",
			resolved.Name, sc)
	}
	doc.Content = string(plain)
	return doc, nil
}

// ListDocuments lists the head revision of each document, without content.
func (s *Store) ListDocuments(sc Scope) ([]Document, error) {
	where, args := sc.Where("")
	rows, err := s.r.Query(`
		SELECT d.id, d.component_root, d.loop_slug, d.file, d.rev, d.bytes, d.sha256,
		       COALESCE(d.written_by,''), d.written_at, COALESCE(d.owner_goal_id,'')
		FROM loop_document d
		JOIN (
			SELECT file, MAX(rev) AS rev FROM loop_document
			WHERE `+where+` GROUP BY file
		) head ON head.file = d.file AND head.rev = d.rev
		WHERE `+where+`
		ORDER BY d.file`, append(args, args...)...)
	if err != nil {
		return nil, fmt.Errorf("store: list the documents of %s: %w", sc, err)
	}
	defer rows.Close()

	var out []Document
	for rows.Next() {
		var d Document
		if err := rows.Scan(&d.ID, &d.ComponentRoot, &d.LoopSlug, &d.File, &d.Rev,
			&d.Bytes, &d.SHA256, &d.WrittenBy, &d.WrittenAt, &d.OwnerGoal); err != nil {
			return nil, fmt.Errorf("store: list the documents of %s: %w", sc, err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// DocumentRevisions lists every revision of one document, newest first.
func (s *Store) DocumentRevisions(sc Scope, rawName string) ([]Document, error) {
	resolved := ResolveDocumentName(rawName)
	if !resolved.OK() {
		return nil, fmt.Errorf("store: %s", resolved.Reason)
	}
	rows, err := s.r.Query(`
		SELECT id, component_root, loop_slug, file, rev, bytes, sha256, COALESCE(written_by,''), written_at
		FROM loop_document
		WHERE component_root = ? AND loop_slug = ? AND file = ?
		ORDER BY rev DESC`, sc.ComponentRoot, sc.LoopSlug, resolved.Name)
	if err != nil {
		return nil, fmt.Errorf("store: list the revisions of %s in %s: %w", resolved.Name, sc, err)
	}
	defer rows.Close()

	var out []Document
	for rows.Next() {
		var d Document
		if err := rows.Scan(&d.ID, &d.ComponentRoot, &d.LoopSlug, &d.File, &d.Rev,
			&d.Bytes, &d.SHA256, &d.WrittenBy, &d.WrittenAt); err != nil {
			return nil, fmt.Errorf("store: list the revisions of %s in %s: %w", resolved.Name, sc, err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// gzip, matching v1 so a document written by either is readable by both during
// the cut-over.
func gzipBytes(in []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(in); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func ungzipBytes(in []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(in))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	return io.ReadAll(zr)
}
