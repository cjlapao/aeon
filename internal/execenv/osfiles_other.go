//go:build !unix

package execenv

// The FileOpener for platforms Aeon does not run its key-file rules on.
//
// There is no honest O_NOFOLLOW, no `syscall.Stat_t`, and no POSIX mode to
// check on these systems, so the truthful answer is "the facts cannot be
// gathered" — and `ResolveRef` treats an unknown owner (UID -1) as a refusal.
// That is the three-state rule applied to a security check: a platform that
// cannot prove a file is the daemon's does not get to read it.
//
// A Windows port that wants key files implements this file properly; it does
// not get to relax the checks in resolve_secret.go from here.

import "os"

// OSFiles is the non-POSIX FileOpener, inert by construction.
type OSFiles struct{}

var _ FileOpener = OSFiles{}

func (OSFiles) Lstat(path string) (FileFacts, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return FileFacts{}, err
	}
	return FileFacts{
		Regular: fi.Mode().IsRegular(),
		Symlink: fi.Mode()&os.ModeSymlink != 0,
		Perm:    fi.Mode().Perm(),
		Size:    fi.Size(),
		UID:     -1,
	}, nil
}

func (OSFiles) Open(path string) (FileFacts, []byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return FileFacts{}, nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return FileFacts{}, nil, err
	}
	b := make([]byte, 0, 4096)
	buf := make([]byte, 4096)
	for len(b) <= MaxKeyFileBytes {
		n, rerr := f.Read(buf)
		b = append(b, buf[:n]...)
		if rerr != nil {
			break
		}
	}
	return FileFacts{Regular: fi.Mode().IsRegular(), Perm: fi.Mode().Perm(), Size: fi.Size(), UID: -1}, b, nil
}

// CurrentUID reports that the platform cannot say, which fails the owner check
// closed rather than assuming the daemon owns everything it can open.
func CurrentUID() int { return -1 }
