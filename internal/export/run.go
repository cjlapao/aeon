package export

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/store"
)

// Actor is who the export is stamped to. The daemon door stamps the
// connection's actor; the CLI's direct door stamps {cli, cli} — the row the
// ledger would carry is NOT written on the direct door (the daemon owns the
// ledger), and the actor still lands in the manifest, so the copy records its
// own origin.
type Actor struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// Deps is the engine's environment: the Reader seam, the clock (the manifest's
// generated_at is the one byte a clock may speak), and the two version facts
// the manifest carries.
type Deps struct {
	Reader  Reader
	Clock   func() time.Time
	Version string
	Schema  int
}

// Opts is one export run.
type Opts struct {
	Dir string
	// Sets is the closed-set subset to export; empty means every non-optional
	// set. memory joins only with IncludeMemory, whether named here or not.
	Sets          []string
	IncludeMemory bool
	Actor         Actor
}

// Set state words (BK14's three states, spelled for the wire and the
// manifest).
const (
	StateAvailable      = "available"
	StateAvailableOptIn = "available (opt-in)"
	StateUnavailable    = "unavailable (slice not built)"
)

// SetResult is one set's place in the manifest: its state, its file count,
// and its row count — null when unavailable, where a number would be a claim
// about a table that is not built (P31).
type SetResult struct {
	Name  string `json:"name"`
	State string `json:"state"`
	Files int    `json:"files"`
	Rows  *int   `json:"rows"`
}

// Result is the engine's answer to a successful run.
type Result struct {
	Dir   string      `json:"dir"`
	Sets  []SetResult `json:"sets"`
	Files int         `json:"files"`
}

// RunError is the engine's refusal: a closed-set Kind a client can branch on
// (the daemon maps it into the ledger's error_kind) and the person's sentence.
type RunError struct {
	Kind string
	What string
}

func (e *RunError) Error() string { return e.What }

// The engine's refusal kinds (closed set). The daemon maps them to the
// backup_run error_kind vocabulary in internal/daemon/export_service.go.
const (
	ErrKindDirRelative   = "dir-relative"
	ErrKindDirSymlink    = "dir-symlink"
	ErrKindDirNonempty   = "dir-nonempty"
	ErrKindDirUnwritable = "dir-unwritable"
	ErrKindUnknownSet    = "unknown-set"
	ErrKindOversize      = "file-oversize"
	ErrKindIO            = "io"
	ErrKindCancelled     = "cancelled"
)

// Run performs one export: gate the dir, read each requested set through the
// allow-listed seam, write the tree 0700/0600, and leave export.json plus the
// README as the manifest. A failure after the dir gate removes the partial
// tree this Run created (fenced; see removePartialExport) and returns the
// refusal.
func Run(ctx context.Context, deps Deps, opts Opts) (Result, *RunError) {
	fail := func(kind, what string) (Result, *RunError) {
		return Result{}, &RunError{Kind: kind, What: what}
	}
	if deps.Reader == nil {
		return fail(ErrKindIO, "export: no reader is wired — the export engine needs the store seam (V13: absent, not a refusal)")
	}
	clock := deps.Clock
	if clock == nil {
		clock = time.Now
	}
	if ctx.Err() != nil {
		return fail(ErrKindCancelled, "export: the context was already done before the run began")
	}

	names, re := resolveSets(opts.Sets, opts.IncludeMemory)
	if re != nil {
		return fail(re.Kind, re.What)
	}

	dir, owned, boundary, re := gateDir(opts.Dir)
	if re != nil {
		return fail(re.Kind, re.What)
	}

	res := Result{Dir: dir, Sets: []SetResult{}}
	failCleanup := func(kind string, err error) (Result, *RunError) {
		what := err.Error()
		if kind == ErrKindIO && !strings.HasPrefix(what, "export:") {
			what = "export: " + what
		}
		if cerr := removePartialExport(dir, owned, boundary); cerr != nil {
			what += " — and the partial tree was NOT removed: " + cerr.Error()
		}
		return res, &RunError{Kind: kind, What: what}
	}

	for _, name := range names {
		ex := registry[name]
		// Availability: every table the set declares must be in the schema.
		// A missing column is the same state — the slice that builds it has
		// not landed — so both read "unavailable (slice not built)".
		built := true
		for _, t := range ex.Tables {
			if !deps.Reader.TableExists(t) {
				built = false
				break
			}
		}
		var files []File
		if built {
			var err error
			files, err = ex.Read(ctx, deps.Reader)
			if err != nil {
				if errors.Is(err, store.ErrColumnsMissing) {
					built = false
				} else {
					if ctx.Err() != nil {
						return failCleanup(ErrKindCancelled, err)
					}
					return failCleanup(ErrKindIO, err)
				}
			}
		}
		if !built {
			res.Sets = append(res.Sets, SetResult{Name: name, State: StateUnavailable, Files: 0, Rows: nil})
			continue
		}
		for _, f := range files {
			if err := CheckFile(f); err != nil {
				kind := ErrKindIO
				var oe *fileOversizeError
				if errors.As(err, &oe) {
					kind = ErrKindOversize
				}
				return failCleanup(kind, err)
			}
			if err := writeExportFile(dir, f); err != nil {
				if ctx.Err() != nil {
					return failCleanup(ErrKindCancelled, err)
				}
				return failCleanup(ErrKindIO, err)
			}
			res.Files++
		}
		n := len(files)
		state := StateAvailable
		if ex.Optional {
			state = StateAvailableOptIn
		}
		res.Sets = append(res.Sets, SetResult{Name: name, State: state, Files: n, Rows: &n})
	}

	manifest := manifestStruct{
		GeneratedAt: clock().UTC().Format(time.RFC3339),
		AeonVersion: deps.Version,
		Schema:      deps.Schema,
		Actor:       opts.Actor,
		Sets:        res.Sets,
	}
	// A manifest that does not encode is a construction error (the fields are
	// JSON-safe by type); it is named, not swallowed.
	b, err := Encode(manifest)
	if err != nil {
		return failCleanup(ErrKindIO, fmt.Errorf("the manifest does not encode: %v", err))
	}
	if err := writeTop(dir, "export.json", b); err != nil {
		return failCleanup(ErrKindIO, err)
	}
	if err := writeTop(dir, "README.md", []byte(readmeText(deps.Version))); err != nil {
		return failCleanup(ErrKindIO, err)
	}
	return res, nil
}

// resolveSets closes the set question before the dir gate: an unknown name is
// a refusal that names it, memory is opt-in by the flag and by the flag alone,
// and the default is every non-optional set in registry order.
func resolveSets(want []string, includeMemory bool) ([]string, *RunError) {
	if len(want) == 0 {
		// The default is every set, where the opt-in sets join only with the
		// flag: --include-memory alone brings memory into the default run.
		var out []string
		for _, name := range registryOrder {
			if registry[name].Optional && !includeMemory {
				continue
			}
			out = append(out, name)
		}
		return out, nil
	}
	seen := map[string]bool{}
	var out []string
	for _, name := range registryOrder {
		requested := false
		for _, w := range want {
			if w == name {
				requested = true
				break
			}
		}
		if !requested {
			continue
		}
		if registry[name].Optional && !includeMemory {
			return nil, &RunError{Kind: ErrKindUnknownSet, What: "export: the set memory is opt-in — --include-memory is its only door, and --records named it without the flag"}
		}
		seen[name] = true
		out = append(out, name)
	}
	for _, w := range want {
		if !seen[w] {
			return nil, &RunError{Kind: ErrKindUnknownSet, What: fmt.Sprintf("export: the set %q is not a declared set — the closed set is %s", w, strings.Join(registryOrder, ", "))}
		}
	}
	return out, nil
}

// gateDir enforces BK14's dir rules: absolute, not a symlink, empty or
// not-yet-existing — never a merge into a worktree that already holds files.
// It reports whether this Run owns the dir (created it) and the boundary —
// the nearest ancestor that existed before the run — which decides how far
// the fenced remover may prune created, emptied parents.
func gateDir(dir string) (string, bool, string, *RunError) {
	fail := func(kind, what string) (string, bool, string, *RunError) {
		return "", false, "", &RunError{Kind: kind, What: what}
	}
	if dir == "" {
		return fail(ErrKindDirRelative, "export: the dir is empty — name the absolute path of the empty (or not-yet-existing) directory to fill")
	}
	if !filepath.IsAbs(dir) {
		return fail(ErrKindDirRelative, "export: the dir must be absolute — "+dir+" is relative, and a relative export could land inside a git worktree by accident")
	}
	dir = filepath.Clean(dir)
	if isSymlink(dir) {
		return fail(ErrKindDirSymlink, "export: the dir "+dir+" is a symlink — the export writes where the name points, and the fence will not follow a planted pointer")
	}
	st, err := os.Lstat(dir)
	if err == nil {
		if !st.IsDir() {
			return fail(ErrKindDirNonempty, "export: the dir "+dir+" exists and is not a directory — the export needs an empty directory or no directory, and this is neither")
		}
		entries, rerr := os.ReadDir(dir)
		if rerr != nil {
			return fail(ErrKindDirUnwritable, "export: the dir "+dir+" cannot be listed: "+rerr.Error())
		}
		if len(entries) > 0 {
			return fail(ErrKindDirNonempty, "export: the dir "+dir+" is not empty — an export refuses to merge into files that were there first (no accidental worktree)")
		}
		return dir, false, dir, nil
	}
	if !os.IsNotExist(err) {
		return fail(ErrKindIO, "export: the dir "+dir+" cannot be probed: "+err.Error())
	}
	// The boundary is the nearest existing ancestor: created parents are
	// pruned back to it on cleanup, never past it.
	boundary := ""
	for p := filepath.Dir(dir); p != "" && p != "/"; p = filepath.Dir(p) {
		if pst, perr := os.Lstat(p); perr == nil && pst.IsDir() {
			boundary = p
			break
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fail(ErrKindDirUnwritable, "export: the dir "+dir+" could not be created: "+err.Error())
	}
	return dir, true, boundary, nil
}

func isSymlink(p string) bool {
	st, err := os.Lstat(p)
	return err == nil && st.Mode()&os.ModeSymlink != 0
}

// writeTop writes a top-level file of the export dir (export.json, README.md)
// at 0600, refusing to follow a planted symlink where the file should stand.
func writeTop(dir, name string, data []byte) error {
	p := filepath.Join(dir, name)
	if isSymlink(p) {
		return fmt.Errorf("export: %s is a symlink — the fence will not write through it", name)
	}
	return os.WriteFile(p, data, 0o600)
}

// manifestStruct is export.json's shape: the one place a clock may speak
// (generated_at), the binary's version, the schema count, the actor that
// asked for the export, and the per-set states.
type manifestStruct struct {
	GeneratedAt string      `json:"generated_at"`
	AeonVersion string      `json:"aeon_version"`
	Schema      int         `json:"schema"`
	Actor       Actor       `json:"actor"`
	Sets        []SetResult `json:"sets"`
}

// readmeText is the export's README: what the tree is, what it is not, and
// the copy-not-scrubber honesty (BK14).
func readmeText(version string) string {
	return `# Aeon export

A read-only TEXT SNAPSHOT of the managed records, for review and git-diff.
It is not a backup: the backup is the whole-database file (aeon backup) and
the restore is its drill (aeon backup restore). Nothing in this tree is read
back by aeon; there is no importer, and a change here changes nothing.

Contents: one directory per declared set, one JSON file per record, stably
encoded (sorted keys, two-space indent, LF). The manifest (export.json)
carries the generation time, the binary's version, the schema count, the
actor, and each set's state — available, available (opt-in) or unavailable
(slice not built), with rows null wherever a table is not built.

What is deliberately absent: credentials (no column shaped like a key,
secret or token is ever selected, and a setting's credential-shaped key is
skipped in name and value), history (no loops, steps or transcripts), and
the loop's scratch memory (the memory set is opt-in).

A word of honesty: the export is a COPY of declared content, not a scrubber.
A credential-looking string that someone stored inside a declared content
field (a prompt, a description, a brief) is copied verbatim. Declared
content is content, and the export does not second-guess it.

Generated by aeon ` + version + ` (export, the S62 T8 engine).
`
}

// removePartialExport is the fenced remover's export variant — the addition
// to T4's documented fence-exception list (T4.md): it removes ONLY what this
// package creates in the export dir — export.json, README.md, and the set
// directories (each holding nothing but .json regular files) — and removes
// the dir itself only when this Run created it, pruning created, emptied
// parents back to the boundary (never past it). A dir that pre-existed, empty,
// is left standing with its contents removed; anything whose name this
// package does not own is left in place and named in the error. No symlink
// is ever followed on the way down.
func removePartialExport(dir string, owned bool, boundary string) error {
	st, err := os.Lstat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // nothing to remove
		}
		return fmt.Errorf("cannot probe %s: %v", dir, err)
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("the export dir %s is not a real directory — nothing removed", dir)
	}
	ownedNames := map[string]bool{"export.json": true, "README.md": true}
	for _, name := range registryOrder {
		ownedNames[name] = true
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("cannot list %s: %v", dir, err)
	}
	// Two phases: a foreign name anywhere in the tree stops the removal
	// BEFORE anything is touched — a scan that removed as it went would have
	// taken the earlier entries and then refused, which is a partial removal
	// wearing an error message.
	for _, e := range entries {
		if !ownedNames[e.Name()] {
			return fmt.Errorf("%s holds %q, which this export did not create — nothing removed", dir, e.Name())
		}
	}
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		fst, err := os.Lstat(p)
		if err != nil {
			return fmt.Errorf("cannot probe %s: %v", p, err)
		}
		if fst.IsDir() {
			if err := removeSetDir(p); err != nil {
				return err
			}
			// removeSetDir empties the set dir; the dir itself comes down now.
			if err := os.Remove(p); err != nil {
				return fmt.Errorf("cannot remove the emptied set dir %s: %v", p, err)
			}
			continue
		}
		if !fst.Mode().IsRegular() {
			return fmt.Errorf("%s is not a regular file this export wrote — nothing removed", p)
		}
		if err := os.Remove(p); err != nil {
			return fmt.Errorf("cannot remove %s: %v", p, err)
		}
	}
	if owned {
		if err := os.Remove(dir); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("cannot remove the created dir %s: %v", dir, err)
		}
		// Prune the created, emptied parents back to the boundary. Each is
		// removed only while it is a real, empty directory; a parent that grew
		// a child in the meantime, or the boundary itself, stops the walk.
		for p := filepath.Dir(dir); p != boundary && p != "" && p != "/"; p = filepath.Dir(p) {
			entries, err := os.ReadDir(p)
			if err != nil || len(entries) > 0 {
				break
			}
			if st, err := os.Lstat(p); err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
				break
			}
			if err := os.Remove(p); err != nil {
				break
			}
		}
	}
	return nil
}

// removeSetDir removes one set (or sub) directory, fenced: checkSetDir holds
// the fence for the WHOLE subtree first — every entry must be a regular
// .json file this package's shape allows, or another such directory (the
// triggers/goals subdirs); a symlink or a foreign file stops the removal and
// says so — and only then does deleteSetDir take the tree down.
func removeSetDir(dir string) error {
	if err := checkSetDir(dir); err != nil {
		return err
	}
	return deleteSetDir(dir)
}

func checkSetDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("cannot list %s: %v", dir, err)
	}
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		fst, err := os.Lstat(p)
		if err != nil {
			return fmt.Errorf("cannot probe %s: %v", p, err)
		}
		if fst.IsDir() {
			if err := checkSetDir(p); err != nil {
				return err
			}
			continue
		}
		if !fst.Mode().IsRegular() || !strings.HasSuffix(e.Name(), ".json") {
			return fmt.Errorf("%s is not a .json file this export wrote — nothing removed", p)
		}
	}
	return nil
}

func deleteSetDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("cannot list %s: %v", dir, err)
	}
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		fst, err := os.Lstat(p)
		if err != nil {
			return fmt.Errorf("cannot probe %s: %v", p, err)
		}
		if fst.IsDir() {
			if err := deleteSetDir(p); err != nil {
				return err
			}
			if err := os.Remove(p); err != nil {
				return fmt.Errorf("cannot remove the emptied dir %s: %v", p, err)
			}
			continue
		}
		if err := os.Remove(p); err != nil {
			return fmt.Errorf("cannot remove %s: %v", p, err)
		}
	}
	return nil
}
