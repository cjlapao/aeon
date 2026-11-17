package export

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MaxFileBytes is the per-file cap for an exported record: 4 MiB. The design
// says a record over the cap is split or refused (BK14); this slice refuses —
// a split would invent a record shape the design never named, and a refusal
// that names the file, its size and the cap is actionable.
const MaxFileBytes = 4 << 20

// File is one exported record: a path relative to the export dir (always
// <set>/<id-or-slug>.json, or <set>/<sub>/<id>.json for a set that spans
// tables) and its encoded bytes.
type File struct {
	Path  string
	Bytes []byte
}

// Encode is the export's stable JSON encoding: sorted object keys (the
// encoding/json map behaviour), two-space indent, LF newlines, one trailing
// newline. It is the whole of the determinism claim for the record files —
// the only bytes allowed to differ between two exports of the same store are
// export.json's generated_at (the manifest is the one place a clock may speak).
func Encode(v any) ([]byte, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// safeRunes is the character set a file name may carry; anything else is
// written as '-' so an id with a slash or a space can never reach the
// filesystem as a path separator.
const safeRunes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._"

// SafeName renders an id or slug as a file-name-safe token, deterministically.
func SafeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune(safeRunes, r) {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	out := b.String()
	if out == "" {
		return "unnamed"
	}
	if out[0] == '.' {
		// A leading dot hides the file from a listing; a leading one is not a
		// name, so it is displaced, not dropped.
		out = "_" + out
	}
	return out
}

// fileOversizeError is the typed half of the per-file cap: the engine maps it
// to its own file-oversize refusal kind (the ledger's disk-full family), and
// the message names the file, its size and the cap.
type fileOversizeError struct {
	file string
	size int
}

func (e *fileOversizeError) Error() string {
	return fmt.Sprintf("export: file %s is %d bytes, over the %d-byte per-file cap — the cap is split-or-refuse and this slice refuses, so the set's rows must be narrowed or the record reduced", e.file, e.size, MaxFileBytes)
}

// CheckFile enforces the file's shape before anything touches the disk: the
// path is a relative, safe, one-to-two-level name under a set directory, and
// the bytes are under the per-file cap (a breach is the typed fileOversizeError).
func CheckFile(f File) error {
	if err := checkRelPath(f.Path); err != nil {
		return err
	}
	if len(f.Bytes) > MaxFileBytes {
		return &fileOversizeError{file: f.Path, size: len(f.Bytes)}
	}
	return nil
}

// checkRelPath is the path half of CheckFile.
func checkRelPath(p string) error {
	if p == "" {
		return fmt.Errorf("export: a record file has no path")
	}
	if filepath.IsAbs(p) || strings.Contains(p, "\\") {
		return fmt.Errorf("export: file path %q must be relative to the export dir, with forward slashes", p)
	}
	parts := strings.Split(p, "/")
	if len(parts) < 2 || len(parts) > 3 {
		return fmt.Errorf("export: file path %q is not <set>/<name>.json or <set>/<sub>/<name>.json", p)
	}
	for i, part := range parts {
		if part == "" || part == "." || part == ".." {
			return fmt.Errorf("export: file path %q carries an unsafe component at position %d", p, i)
		}
		if part != SafeName(part) {
			return fmt.Errorf("export: file path %q carries a file-unsafe name %q (an id must be rendered through SafeName)", p, part)
		}
	}
	base := parts[len(parts)-1]
	if !strings.HasSuffix(base, ".json") || len(base) <= len(".json") {
		return fmt.Errorf("export: file path %q does not end in a named .json record", p)
	}
	return nil
}

// writeExportFile writes one record file into the export dir: the set (and
// sub) directory at 0700, the file at 0600, never following a symlink on the
// way (a pre-planted symlink where a set directory should be is a planted
// target, and the write is refused by the fence's lstat check in
// removePartialExport's twin — here by refusing a non-directory parent).
func writeExportFile(dir string, f File) error {
	p := filepath.Join(dir, filepath.FromSlash(f.Path))
	parent := filepath.Dir(p)
	st, err := os.Lstat(parent)
	if err != nil {
		if os.IsNotExist(err) {
			if err := os.MkdirAll(parent, 0o700); err != nil {
				return err
			}
			st, err = os.Lstat(parent)
		}
		if err != nil {
			return err
		}
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("export: the directory %s for file %s is not a real directory this export created", parent, f.Path)
	}
	return os.WriteFile(p, f.Bytes, 0o600)
}
