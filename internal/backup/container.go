package backup

import (
	"archive/tar"
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// The archive's two members, in order (BK4). No third, no zero, no other
// name: a container that carries anything else is not this package's.
const (
	memberManifest = "manifest.json"
	memberDB       = "state.db"

	// manifestSizeCap: a manifest is a page of numbers, not a database. A
	// member claiming to be manifest.json and holding a megabyte is hostile.
	manifestSizeCap = 64 << 10
)

// Write assembles the archive on dst: manifest.json first, then state.db,
// streamed. The tar holds no directory entries, no links, no names the
// reader will refuse.
func Write(dst io.Writer, m Manifest, dbPath string) error {
	fi, err := os.Stat(dbPath)
	if err != nil {
		return fmt.Errorf("backup: stat the snapshot for the archive: %w", err)
	}
	man, err := m.Marshal()
	if err != nil {
		return err
	}
	tw := tar.NewWriter(dst)
	if err := tw.WriteHeader(&tar.Header{
		Name:     memberManifest,
		Mode:     0o600,
		Size:     int64(len(man)),
		Typeflag: tar.TypeReg,
	}); err != nil {
		return fmt.Errorf("backup: write the manifest header: %w", err)
	}
	if _, err := tw.Write(man); err != nil {
		return fmt.Errorf("backup: write the manifest: %w", err)
	}
	if err := tw.WriteHeader(&tar.Header{
		Name:     memberDB,
		Mode:     0o600,
		Size:     fi.Size(),
		Typeflag: tar.TypeReg,
	}); err != nil {
		return fmt.Errorf("backup: write the database header: %w", err)
	}
	f, err := os.Open(dbPath)
	if err != nil {
		return fmt.Errorf("backup: open the snapshot for the archive: %w", err)
	}
	defer func() { _ = f.Close() }()
	if _, err := io.Copy(tw, f); err != nil {
		return fmt.Errorf("backup: write the database into the archive: %w", err)
	}
	if err := tw.Close(); err != nil {
		return fmt.Errorf("backup: close the archive: %w", err)
	}
	return nil
}

// OpenHeader reads only the manifest member — the lister's fast path, which
// never extracts the database and therefore never needs scratch space.
func OpenHeader(path string) (Manifest, error) {
	f, err := os.Open(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("backup: open %s: %w", base(path), err)
	}
	defer func() { _ = f.Close() }()
	tr := tar.NewReader(bufio.NewReaderSize(f, 256<<10))
	h, err := tr.Next()
	if err != nil {
		return Manifest{}, fmt.Errorf("refused: %s: not a readable archive (%s)", base(path), shortErr(err))
	}
	if err := checkMember(h, memberManifest, manifestSizeCap); err != nil {
		return Manifest{}, fmt.Errorf("refused: %s: %s", base(path), err)
	}
	b, err := io.ReadAll(io.LimitReader(tr, manifestSizeCap+1))
	if err != nil {
		return Manifest{}, fmt.Errorf("refused: %s: cannot read the manifest member: %s", base(path), shortErr(err))
	}
	if int64(len(b)) > manifestSizeCap {
		return Manifest{}, fmt.Errorf("refused: %s: the manifest member is %d bytes, the cap is %d — a manifest is a page of numbers, not a payload", base(path), len(b), manifestSizeCap)
	}
	return DecodeManifest(b)
}

// Reader is an opened archive: its manifest and the extracted database,
// which lives in a .tmp-* directory beside the archive until Close. The
// directory is in the fence's own family of names, so its removal goes
// through removeBackupFile like everything else this package deletes.
type Reader struct {
	manifest Manifest
	dbPath   string
	tmpDir   string
}

// Manifest is the archive's verified statement of itself.
func (r *Reader) Manifest() Manifest { return r.manifest }

// DBPath is the extracted database's path, for the file checks.
func (r *Reader) DBPath() string { return r.dbPath }

// Dir is the scratch directory the copy was extracted into — the drill's
// open point. Empty after Close.
func (r *Reader) Dir() string { return r.tmpDir }

// Close removes the extracted copy. It is safe to call more than once.
func (r *Reader) Close() error {
	if r.tmpDir == "" {
		return nil
	}
	dir, name := filepath.Split(r.tmpDir)
	if err := removeBackupFile(dir, name); err != nil {
		return fmt.Errorf("backup: remove the extracted copy: %w", err)
	}
	r.tmpDir = ""
	return nil
}

// Open reads the whole archive: manifest, then the database extracted to a
// scratch directory beside it, with the digest verified as the bytes flow.
// It refuses — with a sentence naming the member and the mismatch — any
// hostile shape: a foreign first member, a missing or out-of-order second,
// a symlink or directory where a regular file is promised, a size that
// disagrees with the manifest, a digest that does not match, and any third
// member at all.
func Open(path string) (*Reader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("backup: open %s: %w", base(path), err)
	}
	defer func() { _ = f.Close() }()
	tr := tar.NewReader(bufio.NewReaderSize(f, 256<<10))

	h1, err := tr.Next()
	if err != nil {
		return nil, fmt.Errorf("refused: %s: not a readable archive (%s)", base(path), shortErr(err))
	}
	if err := checkMember(h1, memberManifest, manifestSizeCap); err != nil {
		return nil, fmt.Errorf("refused: %s: %s", base(path), err)
	}
	b, err := io.ReadAll(io.LimitReader(tr, manifestSizeCap+1))
	if err != nil {
		return nil, fmt.Errorf("refused: %s: cannot read the manifest member: %s", base(path), shortErr(err))
	}
	if int64(len(b)) > manifestSizeCap {
		return nil, fmt.Errorf("refused: %s: the manifest member is %d bytes, the cap is %d", base(path), len(b), manifestSizeCap)
	}
	m, err := DecodeManifest(b)
	if err != nil {
		return nil, fmt.Errorf("refused: %s: %s", base(path), err)
	}

	h2, err := tr.Next()
	if err != nil {
		return nil, fmt.Errorf("refused: %s: the archive has no database member after the manifest", base(path))
	}
	if err := checkMember(h2, memberDB, 0); err != nil {
		return nil, fmt.Errorf("refused: %s: %s", base(path), err)
	}
	if h2.Size != m.DBBytes {
		return nil, fmt.Errorf("refused: %s: the database member is %d bytes, the manifest says %d — a member and its manifest disagreeing is a container that was edited", base(path), h2.Size, m.DBBytes)
	}

	// Extract beside the archive, in a fenced name.
	parent := filepath.Dir(path)
	id, err := randomID()
	if err != nil {
		return nil, err
	}
	tmpDir := filepath.Join(parent, ".tmp-"+id)
	if err := os.Mkdir(tmpDir, 0o700); err != nil {
		return nil, fmt.Errorf("backup: make the scratch dir for %s: %w", base(path), err)
	}
	fail := func(cause error) (*Reader, error) {
		_ = removeBackupFile(parent, filepath.Base(tmpDir))
		return nil, cause
	}
	dbPath := filepath.Join(tmpDir, "state.db")
	out, err := os.OpenFile(dbPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fail(fmt.Errorf("backup: open the extraction target: %w", err))
	}
	var dig hash.Hash = sha256.New()
	written, err := io.Copy(io.MultiWriter(out, dig), tr)
	if err != nil {
		_ = out.Close()
		return fail(fmt.Errorf("refused: %s: the database member will not stream (%s) — a truncated member is a backup that stops mid-file", base(path), shortErr(err)))
	}
	if written != m.DBBytes {
		_ = out.Close()
		return fail(fmt.Errorf("refused: %s: the database member stopped at %d of %d bytes", base(path), written, m.DBBytes))
	}
	if err := out.Close(); err != nil {
		return fail(fmt.Errorf("backup: close the extraction target: %w", err))
	}
	if hex.EncodeToString(dig.Sum(nil)) != m.DBSHA256 {
		return fail(fmt.Errorf("refused: %s: checksum mismatch — the database member's sha256 is %s…, the manifest says %s…", base(path), digestPrefix(hex.EncodeToString(dig.Sum(nil))), digestPrefix(m.DBSHA256)))
	}
	if _, err := tr.Next(); err != io.EOF {
		if err == io.ErrUnexpectedEOF {
			// A short final block is a cut file, not a third member: the
			// two refusals name different repairs (a new backup vs a
			// different archive), so the sentences must not blur them.
			return fail(fmt.Errorf("refused: %s: the archive ends before its blocks complete — the file is truncated", base(path)))
		}
		return fail(fmt.Errorf("refused: %s: the archive carries a third member after the manifest and the database — a container is exactly two files", base(path)))
	}

	return &Reader{manifest: m, dbPath: dbPath, tmpDir: tmpDir}, nil
}

// checkMember refuses a header that is not the regular file with exactly
// the promised name. max (when positive) bounds the member's declared size.
// The refusals name the member: a path-traversal name, a link, a directory —
// each is a different hostile act and the sentence says which.
func checkMember(h *tar.Header, want string, max int64) error {
	if strings.Contains(h.Name, "/") || h.Name == "." || h.Name == ".." || strings.HasPrefix(h.Name, "..") {
		return fmt.Errorf("hostile member name %q — an archive member is a bare name, not a path", h.Name)
	}
	if h.Typeflag != tar.TypeReg {
		kind := "a non-regular file"
		switch h.Typeflag {
		case tar.TypeSymlink:
			kind = "a symlink"
		case tar.TypeLink:
			kind = "a hard link"
		case tar.TypeDir:
			kind = "a directory"
		}
		return fmt.Errorf("hostile member %q is %s, and a container holds regular files only", h.Name, kind)
	}
	if h.Name != want {
		return fmt.Errorf("member %q where %q is expected — a container is exactly manifest.json, then state.db", h.Name, want)
	}
	if max > 0 && h.Size > max {
		return fmt.Errorf("member %q declares %d bytes, the cap is %d", h.Name, h.Size, max)
	}
	if h.Size < 0 {
		return fmt.Errorf("member %q declares a negative size", h.Name)
	}
	return nil
}

// base is the file name for sentences: the sentence says what was refused,
// and a full path in an error is a path an operator may not want echoed.
func base(p string) string { return filepath.Base(p) }

// shortErr is the error's own text for a refusal sentence, capped — a raw
// syscall message can be long and is never the point.
func shortErr(err error) string {
	s := strings.ReplaceAll(err.Error(), "\n", " ")
	if len(s) > 120 {
		s = s[:120] + "…"
	}
	return s
}

// digestPrefix shows the first twelve hex characters — enough to identify,
// never enough to reuse.
func digestPrefix(d string) string {
	if len(d) < 12 {
		return d
	}
	return d[:12]
}
