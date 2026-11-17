package graph

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/cjlapao/aeon/internal/store"
)

// # Build and rebuild: where drafts become rows
//
// The resolver is here and nowhere else: only this file knows whether a
// symbolic reference landed in the same file (extracted) or crossed a
// boundary or had multiple candidates (inferred — D4's tag decided by
// fact, not by guesswork). The walkers emit possibility; the resolver
// writes reality; the query layer reports what it finds, four-states.
//
// # The write-signal subscription is NOT here (CG6, D5)
//
// The hook "the loop already knows it wrote files" lives at the tools/
// daemon layer as S22's own Q7 already ruled (found pre-built as
// daemon.Options.OnWorkspaceWrite — see tasks/T4.md); the daemon
// subscribes this Service to it. internal/graph owns no watcher and no
// signal of its own, and the package boundary test keeps it that way.

// Service is the whole of this slice's write surface over S4's store. The
// daemon builds ONE from its store handle and hands it to the tool seam,
// the socket handlers and the write-hook subscription — one door, every
// surface (P34's "the same function the tool calls" clause).
type Service struct {
	st *store.Store
}

// NewService wires one graph service over an opened store.
func NewService(st *store.Store) *Service { return &Service{st: st} }

// BuildReport is a full build's accounting. The counters are the record
// ("a count nobody re-measures drifts"); UnresolvedEdges is honesty, not
// failure: a call to fmt.Println or a JSX intrinsic has no node to point
// at, and the build says how many references remained wishes rather than
// pretending they became edges.
type BuildReport struct {
	Root            string `json:"root"`
	Files           int    `json:"files"`
	FilesSkipped    int    `json:"filesSkipped"`
	Nodes           int    `json:"nodes"`
	Edges           int    `json:"edges"`
	UnresolvedEdges int    `json:"unresolvedEdges"`
}

// skipDirs are the directories no project-root walk enters: hidden
// directories (git data, editor state), the standard build/dependency
// output trees, and Python's caches. Named here rather than discovered:
// walking node_modules is not a feature, it is an outage (a real one of
// those directories is 200k files, and a rebuild of it would be a
// wall-clock catastrophe nobody asked for). The decision is recorded, not
// silent: FilesSkipped counts what these cost a build.
var skipDirs = map[string]bool{
	"node_modules": true, "dist": true, "build": true, "out": true,
	"target": true, "__pycache__": true, "vendor": true, ".git": true,
}

func skipDir(name string) bool {
	return skipDirs[name] || strings.HasPrefix(name, ".")
}

// Build rebuilds one project root from nothing: every file a registered
// grammar claims is parsed, its rows replaced, its edges resolved. It is
// the door the bench and the CLI dial (graph.build) — the missing entry
// point the pre-flight found: the write signal alone only ever knows about
// files a loop WROTE, so without a build door the graph could never know
// about the project as it already exists.
func (s *Service) Build(ctx context.Context, root string) (*BuildReport, error) {
	rep := &BuildReport{Root: root}
	if root == "" {
		return nil, fmt.Errorf("graph build: project_root is required, never defaulted (T5 §5)")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("graph build %s: %w", root, err)
	}
	if info, err := os.Stat(abs); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("graph build %s: not a directory", root)
	}

	// Every key is the ABSOLUTE root — build, rebuild, delete and every
	// query land on identical rows however the caller spelled the path
	// (measured in T4: the write hook and an operator's relative `build
	// ./proj` had begun keying two different spellings).
	if _, err := s.st.DeleteGraph(abs); err != nil {
		return nil, err
	}

	var pending []*pendingFile

	walkErr := filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entries skip; the report says what got in
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			rel, rerr := filepath.Rel(abs, path)
			if rerr == nil && rel != "." && skipDir(filepath.Base(path)) {
				return fs.SkipDir
			}
			return nil
		}
		if _, ok := LookupByPath(path); !ok {
			return nil
		}
		file, err := os.Stat(path)
		if err != nil || file.Size() > MaxFileBytes {
			rep.FilesSkipped++
			return nil
		}
		// PHASE A — nodes in, edges held. Measured in T4's own gate: a
		// per-file build that resolves edges as it walks loses every
		// reference whose target lives in a file not walked YET
		// (process.go → validateOrder lost the edge purely because
		// validate.go sorts after it). The whole node set is inserted
		// first, so a cross-file name resolves against the finished
		// table regardless of walk order.
		pf, err := s.parseAndInsert(abs, path, rep)
		if err != nil {
			rep.FilesSkipped++ // one bad file does not abandon the build
			return nil
		}
		pending = append(pending, pf)
		rep.Files++
		return nil
	})
	if walkErr != nil {
		return rep, walkErr
	}
	for _, pf := range pending {
		if err := s.resolveEdges(abs, pf, rep); err != nil {
			return rep, err
		}
	}
	return rep, nil
}

// pendingFile is one file between the two build phases: its rows are in,
// its edges wait for the finished node table.
type pendingFile struct {
	fileRel string
	ids     map[string]int64
	drafts  []EdgeDraft
}

// RebuildFile is the incremental half (D5, D6): the file the loop just
// wrote is deleted and re-parsed alone, and only the edges that REFERENCED
// its symbols are re-resolved — never the whole project. The daemon's
// write-signal subscription calls exactly this.
func (s *Service) RebuildFile(ctx context.Context, root, absPath string) (*BuildReport, error) {
	if root == "" {
		return nil, fmt.Errorf("graph rebuild: project_root is required, never defaulted (T5 §5)")
	}
	if _, ok := LookupByPath(absPath); !ok {
		return nil, nil // not a file this slice claims; skip, not error — the hook fires on every write
	}
	rep := &BuildReport{Root: root}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("graph rebuild %s: %w", root, err)
	}
	if err := s.rebuildOne(ctx, abs, absPath, rep, nil); err != nil {
		return nil, err
	}
	rep.Files = 1
	return rep, nil
}

// rebuildOne is a single file's rebuild for RebuildFile (incremental):
//
//  1. Remember the incoming edges (what pointed at this file's symbols,
//     with their symbols — GraphTouchRow's memory), then delete the file's
//     nodes and every edge touching them. One store transaction, so no
//     crash leaves edges to gone nodes.
//  2. Re-parse THIS file alone and insert its nodes.
//  3. Resolve its outgoing edges against the EXISTING table (the rest of
//     the project is already in it — only Build has a build phase).
//  4. Re-link the remembered incoming edges by symbol — the file they
//     pointed at has new ids now; re-resolution by name is a cross-file
//     guess by construction, so they land `inferred` (D4), which is also
//     how a re-link that used to be `extracted` stays honest: what moved
//     across a boundary is the resolution, and the tag says so.
func (s *Service) rebuildOne(ctx context.Context, absRoot, absPath string, rep *BuildReport, actor *store.PlatformActor) error {
	fileRel, err := relTo(absRoot, absPath)
	if err != nil {
		return err
	}

	touches, err := s.st.GraphEdgesTouchingFile(absRoot, fileRel)
	if err != nil {
		return err
	}
	// The S54 SP3 audit rides THIS transaction — the reparse's first
	// mutation — when a person's door composed an actor (nil is the write
	// hook's and the unaudited twin's pass: the daemon's act, not audited).
	var counts store.GraphDeleteCounts
	if actor != nil {
		counts, err = s.st.DeleteGraphFileAudited(absRoot, fileRel, *actor)
	} else {
		counts, err = s.st.DeleteGraphFile(absRoot, fileRel)
	}
	if err != nil {
		return err
	}
	rep.Nodes += counts.NodesDeleted
	rep.Edges += counts.EdgesDeleted

	pf, err := s.parseAndInsert(absRoot, absPath, rep)
	if err != nil {
		return err
	}
	if err := s.resolveEdges(absRoot, pf, rep); err != nil {
		return err
	}

	// Step 4: the re-link (T4 §3). Edges FROM the changed file were just
	// re-emitted from fresh AST; edges INTO it died with its ids and are
	// rebuilt from memory.
	for _, t := range touches {
		if t.FromFile == fileRel {
			continue
		}
		fromID, err := s.nodeIDBySymbol(absRoot, t.FromFile, t.FromSymbol)
		if err != nil || fromID == 0 {
			continue
		}
		cands, err := s.candidatesBySymbol(absRoot, t.ToSymbol)
		if err != nil || len(cands) == 0 {
			rep.UnresolvedEdges++
			continue
		}
		for _, c := range cands {
			if c.id == fromID {
				continue
			}
			if _, err := s.st.InsertGraphEdge(absRoot, fromID, c.id, t.Kind, Inferred); err != nil {
				return err
			}
			rep.Edges++
		}
	}
	return nil
}

func relTo(absRoot, absPath string) (string, error) {
	r, err := filepath.Rel(absRoot, absPath)
	if err != nil || strings.HasPrefix(r, "..") {
		return "", fmt.Errorf("graph rebuild %s: not under project root %s", absPath, absRoot)
	}
	return filepath.ToSlash(r), nil
}

// parseAndInsert is phase A: parse one file and insert its nodes,
// returning the symbol→id map its edges will resolve from.
func (s *Service) parseAndInsert(absRoot, absPath string, rep *BuildReport) (*pendingFile, error) {
	fileRel, err := relTo(absRoot, absPath)
	if err != nil {
		return nil, err
	}
	fp, err := ParseFile(absRoot, absPath)
	if err != nil {
		return nil, err
	}
	ids := map[string]int64{}
	for _, n := range fp.Nodes {
		id, err := s.st.InsertGraphNode(absRoot, n.File, n.StartLine, n.EndLine, n.Kind, n.Symbol, n.Language)
		if err != nil {
			return nil, err
		}
		if _, dup := ids[n.Symbol]; !dup {
			ids[n.Symbol] = id
		}
		rep.Nodes++
	}
	return &pendingFile{fileRel: fileRel, ids: ids, drafts: fp.Edges}, nil
}

// resolveEdges is phase B: turn one file's symbolic drafts into rows,
// deciding the resolution tag here and nowhere else.
func (s *Service) resolveEdges(absRoot string, pf *pendingFile, rep *BuildReport) error {
	for _, ed := range pf.drafts {
		fromID, ok := pf.ids[ed.From]
		if !ok {
			continue // the walker claimed an edge from a scope it never emitted — drop it
		}
		cands, err := s.resolve(absRoot, pf.fileRel, pf.ids, ed.To)
		if err != nil {
			return err
		}
		if len(cands) == 0 {
			rep.UnresolvedEdges++
			continue
		}
		for _, c := range cands {
			if c.id == fromID {
				continue // a symbol relating to itself is the parse, not the graph
			}
			if _, err := s.st.InsertGraphEdge(absRoot, fromID, c.id, ed.Kind, c.resolution); err != nil {
				return err
			}
			rep.Edges++
		}
	}
	return nil
}

// resolved is one resolution outcome: the target's id and the tag the
// resolver decided.
type resolved struct {
	id         int64
	resolution string
}

// resolve turns a symbolic To into rows' worth of targets. Same-file
// exact hit is the extracted case (the relation was literal AND its target
// was literally here); anything found outside this file, or any
// multi-candidate name, is inferred (D4's words: the resolution "crossed
// files or required disambiguation"). Every candidate of an ambiguous
// name becomes its own edge — never one picked silently — because D8's
// ambiguous state is then reproducible from the rows at query time.
func (s *Service) resolve(root, fileRel string, local map[string]int64, to string) ([]resolved, error) {
	var localHits []resolved
	for sym, id := range local {
		if nameMatches(sym, to) {
			localHits = append(localHits, resolved{id: id, resolution: Extracted})
		}
	}
	if len(localHits) == 1 {
		return localHits, nil
	}
	if len(localHits) > 1 {
		for i := range localHits {
			localHits[i].resolution = Inferred // disambiguation happened, so say so
		}
		return localHits, nil
	}
	globals, err := s.candidatesBySymbol(root, to)
	if err != nil {
		return nil, err
	}
	return globals, nil
}

// candidatesBySymbol finds the rows a symbolic To points at. A fully
// qualified To ("order.Validate") hits the EXACT-name lookup first and
// stops there when it lands — qualifying a name is the caller
// disambiguating, and re-answering "ambiguous" to a name that can only
// match one row (T5 §1's answer to a fully qualified name) would refuse
// the very disambiguation the caller performed. Only when the written
// form matches nothing does the final-component sweep run, and its every
// candidate gets an edge (never one picked) with resolution `inferred` —
// reaching this method at all already means the same-file fast path
// missed.
func (s *Service) candidatesBySymbol(root, sym string) ([]resolved, error) {
	if strings.Contains(sym, ".") {
		exact, err := s.st.GraphNodesBySymbol(root, sym)
		if err != nil {
			return nil, err
		}
		if len(exact) == 1 {
			return []resolved{{id: exact[0].ID, resolution: Inferred}}, nil
		}
	}
	rows, err := s.st.GraphNodesByName(root, lastComponent(sym))
	if err != nil {
		return nil, err
	}
	out := make([]resolved, 0, len(rows))
	for _, r := range rows {
		out = append(out, resolved{id: r.ID, resolution: Inferred})
	}
	return out, nil
}

func (s *Service) nodeIDBySymbol(root, file, sym string) (int64, error) {
	rows, err := s.st.GraphNodesByName(root, lastComponent(sym))
	if err != nil {
		return 0, err
	}
	for _, r := range rows {
		if r.FilePath == file && nameMatches(r.Symbol, sym) {
			return r.ID, nil
		}
	}
	return 0, nil
}

func lastComponent(sym string) string {
	if i := strings.LastIndexByte(sym, '.'); i >= 0 {
		return sym[i+1:]
	}
	return sym
}
