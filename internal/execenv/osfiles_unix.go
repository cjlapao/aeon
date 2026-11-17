//go:build unix

package execenv

// The FileOpener that reads the real filesystem.
//
// It lives here, next to the rules, rather than in the daemon, for one reason:
// the checks and the read have to agree about what a fact means, and a
// re-implementation in the wiring layer is a second definition of "regular
// file" and "0600" that can drift from the first. The daemon still decides
// WHICH paths are referenceable (that is a row in `secret_ref`); this file only
// answers what the operating system says about one.

import (
	"io"
	"os"
	"syscall"
)

// OSFiles is the production FileOpener.
type OSFiles struct{}

var _ FileOpener = OSFiles{}

// Lstat reports the facts of the named inode itself, without following a
// symlink — os.Lstat, not os.Stat, because the difference is the whole of the
// symlink rule.
func (OSFiles) Lstat(path string) (FileFacts, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return FileFacts{}, err
	}
	return factsOf(fi), nil
}

// Open reads the file WITHOUT following a symlink and reports the facts of the
// descriptor it actually ended up reading, which is what makes the second pass
// of the checks in ResolveRef mean something. The read is capped: a caller
// cannot turn the daemon's memory into a function of a path somebody typed,
// even by racing a truncate against the size check.
func (OSFiles) Open(path string) (FileFacts, []byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return FileFacts{}, nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return FileFacts{}, nil, err
	}
	b, err := io.ReadAll(io.LimitReader(f, MaxKeyFileBytes+1))
	if err != nil {
		return factsOf(fi), nil, err
	}
	return factsOf(fi), b, nil
}

func factsOf(fi os.FileInfo) FileFacts {
	f := FileFacts{
		Regular: fi.Mode().IsRegular(),
		Symlink: fi.Mode()&os.ModeSymlink != 0,
		Perm:    fi.Mode().Perm(),
		Size:    fi.Size(),
		// -1 until the platform answers. An unknown owner is a refusal, not a
		// pass — see ownerIsDaemon.
		UID: -1,
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		f.UID = int(st.Uid)
	}
	return f
}

// CurrentUID is the daemon's own user id, which a key file must match. It is
// here so the caller does not have to reach for `os` to wire the resolver and
// can pass -1 deliberately when it wants the owner check to fail closed.
func CurrentUID() int { return os.Getuid() }
