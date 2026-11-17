// restore_fs.go — the file moves of BK9's step 5 and 6: the stage dir,
// the staged file, the drill copy, and the small filesystem facts they
// need (fsync, digests, same-filesystem). The moves themselves live in
// restore.go; this file is everything that happens to a file, not the
// ordering of the restore.
//
// Every directory this file makes is removed only through the fence
// (removeRestoreStageDir for the stage, removeBackupFile for the drill's
// .tmp- scratch): the restore path owns exactly two deletion sites, both
// documented, both fenced by name.

package backup

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// makeStage mints the stage dir for the swap: .restore-<16 hex> inside
// the state dir, 0700. The stage lives in the state dir on purpose — the
// swap is one rename inside that dir, and a rename that crosses
// filesystems is not atomic (BK17's same-filesystem check is about the
// backup dir, whose archive is what the extraction reads). The returned
// remover is the fence's restore variant, bound to this dir's name.
func makeStage(stateDir string) (dir string, remove func() error, err error) {
	id, err := randomID()
	if err != nil {
		return "", nil, fmt.Errorf("the stage id will not be minted: %s", shortErr(err))
	}
	name := ".restore-" + id
	dir = filepath.Join(stateDir, name)
	if err := os.Mkdir(dir, 0o700); err != nil {
		return "", nil, err
	}
	return dir, func() error { return removeRestoreStageDir(stateDir, name) }, nil
}

// makeDrillCopy puts a copy of the staged file in a .tmp- scratch dir
// INSIDE the state dir and returns the dir to open and the name to fence
// off afterwards. The drill opens the copy, not the staged file: the file
// that will be swapped in must stay byte-for-byte the backup's, and the
// copy is what the drill's migrations are allowed to touch. The scratch
// name is the fence's own .tmp- family, so its removal needs no new
// deletion code.
func makeDrillCopy(stateDir, stagedPath string) (dir, name string, err error) {
	id, err := randomID()
	if err != nil {
		return "", "", fmt.Errorf("the drill id will not be minted: %s", shortErr(err))
	}
	name = ".tmp-" + id
	dir = filepath.Join(stateDir, name)
	if err := os.Mkdir(dir, 0o700); err != nil {
		return "", "", err
	}
	if err := copyFile(stagedPath, filepath.Join(dir, "state.db"), 0o600); err != nil {
		// The scratch is the fence's own .tmp- family: it goes through
		// the fence, which removes the dir whole (any partial copy with it).
		_ = removeBackupFile(stateDir, name)
		return "", "", err
	}
	return dir, name, nil
}

// copyFile copies src to dst with the given mode, and fsyncs the file
// before it returns: the staged file and the drill copy are durability
// points, and a copy that is not synced is not written. On a failed copy
// a partial dst may remain — that is the caller's: both call sites are
// inside a dir (the stage, the drill scratch) that the caller removes
// whole through the fence, so the partial goes with it and this file owns
// no deletion of its own.
func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// (fsyncDir is the package's own, from the space files: unix does the real
// Sync, Windows the stated no-op — the restore reuses it.)

// fileSHA256 is the full digest of a file, hex. The manifest carries the
// archive member's digest, and put-back verification compares digests,
// so the comparison is over the whole value, not a display prefix.
func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
