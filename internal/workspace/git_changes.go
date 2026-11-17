package workspace

import (
	"bufio"
	"context"
	"os"
	"strconv"
	"strings"

	"github.com/cjlapao/aeon/internal/tools"
)

// ChangedFileStat is one changed path with how many lines it added and removed. Added and Removed are pointers: a
// binary file has no line count, and a count that could not be read is absent, never a plausible zero.
type ChangedFileStat struct {
	Path    string
	Status  string // modified | added | deleted | untracked | renamed | conflicted
	Added   *int
	Removed *int
	Binary  bool
}

// ChangesResult is the three-state answer to `GitChanges`: every changed path of the working tree against HEAD with
// its line counts, and the totals. The totals cover only files whose counts are known.
type ChangesResult struct {
	State  State
	Reason string

	Files        []ChangedFileStat
	TotalAdded   int
	TotalRemoved int
}

// untrackedLineCap bounds how much of an untracked file is read to count its lines.
const untrackedLineCap = 4 << 20

// GitChanges lists what changed in the working tree against HEAD with per-file line counts, the numbers an
// "edited N files +A −R" summary needs. Tracked changes (staged or not) come from `git diff --numstat HEAD`; an
// untracked file is counted by its lines, as git would show it once staged. A repository with no commits has no
// HEAD to count against, so every path is counted as added.
func GitChanges(ctx context.Context, root string) ChangesResult {
	st := GitStatus(ctx, root)
	if st.State != StateOK {
		return ChangesResult{State: st.State, Reason: st.Reason}
	}
	counts := map[string][3]int{} // added, removed, binary(1)
	head, err := run(ctx, root, stderrCapBytes, "--no-optional-locks", "rev-parse", "--verify", "--quiet", "HEAD")
	if err != nil {
		return ChangesResult{State: StateUnavailable, Reason: err.Error()}
	}
	if head.exitCode == 0 {
		out, err := run(ctx, root, statusOutputCapBytes, "--no-optional-locks", "-c", "core.quotePath=false", "diff", "--numstat", "--no-renames", "--no-ext-diff", "-z", "HEAD")
		if err != nil {
			return ChangesResult{State: StateUnavailable, Reason: err.Error()}
		}
		if out.timedOut || out.exitCode != 0 {
			return ChangesResult{State: StateUnavailable, Reason: "git diff --numstat failed: " + tail(out.stderr)}
		}
		for _, rec := range strings.Split(out.stdout, "\x00") {
			parts := strings.SplitN(rec, "\t", 3)
			if len(parts) != 3 {
				continue
			}
			if parts[0] == "-" || parts[1] == "-" {
				counts[parts[2]] = [3]int{0, 0, 1}
				continue
			}
			a, errA := strconv.Atoi(parts[0])
			r, errR := strconv.Atoi(parts[1])
			if errA == nil && errR == nil {
				counts[parts[2]] = [3]int{a, r, 0}
			}
		}
	}
	res := ChangesResult{State: StateOK, Files: []ChangedFileStat{}}
	for _, f := range st.Changed {
		file := ChangedFileStat{Path: f.Path, Status: statusWord(f.Index, f.Worktree)}
		if c, ok := counts[f.Path]; ok {
			if c[2] == 1 {
				file.Binary = true
			} else {
				a, r := c[0], c[1]
				file.Added, file.Removed = &a, &r
			}
		} else if f.Index == "?" || head.exitCode != 0 {
			if n, binary, ok := countLines(root, f.Path); ok {
				file.Binary = binary
				if !binary {
					r := 0
					file.Added, file.Removed = &n, &r
				}
			}
		}
		if file.Added != nil {
			res.TotalAdded += *file.Added
			res.TotalRemoved += *file.Removed
		}
		res.Files = append(res.Files, file)
	}
	return res
}

// statusWord names a porcelain v2 XY pair in one word.
func statusWord(index, worktree string) string {
	switch {
	case index == "?":
		return "untracked"
	case index == "U" || worktree == "U" || (index == "A" && worktree == "A") || (index == "D" && worktree == "D"):
		return "conflicted"
	case index == "A" || worktree == "A":
		return "added"
	case index == "D" || worktree == "D":
		return "deleted"
	case index == "R" || worktree == "R":
		return "renamed"
	}
	return "modified"
}

// countLines counts the lines of a file inside the root, with the NUL heuristic for binary. The path goes through the
// same resolver as every other workspace read.
func countLines(root, path string) (lines int, binary bool, ok bool) {
	r := tools.Resolve(root, path)
	if !r.OK {
		return 0, false, false
	}
	info, err := os.Stat(r.Abs)
	if err != nil || !info.Mode().IsRegular() {
		return 0, false, false
	}
	f, err := os.Open(r.Abs)
	if err != nil {
		return 0, false, false
	}
	defer f.Close()
	sc := bufio.NewScanner(&limitedReader{f: f, left: untrackedLineCap})
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		if strings.IndexByte(sc.Text(), 0) >= 0 {
			return 0, true, true
		}
		lines++
	}
	return lines, false, true
}

type limitedReader struct {
	f    *os.File
	left int
}

func (l *limitedReader) Read(p []byte) (int, error) {
	if l.left <= 0 {
		return 0, os.ErrClosed
	}
	if len(p) > l.left {
		p = p[:l.left]
	}
	n, err := l.f.Read(p)
	l.left -= n
	return n, err
}
