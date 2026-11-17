package daemon

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/tools"
)

const maxLoopFileBytes = 1024 * 1024

func allowedLoopFilePath(path string) bool {
	for _, segment := range strings.Split(strings.ReplaceAll(path, `\`, "/"), "/") {
		if strings.HasPrefix(segment, ".") && segment != "." {
			return false
		}
		if segment == "node_modules" || segment == "vendor" {
			return false
		}
	}
	return true
}

func readableLoopFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".txt", ".json", ".jsonl", ".yaml", ".yml":
		return true
	default:
		return false
	}
}

type loopFileEntry struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}

func (d *Daemon) loopFilePath(p protocol.LoopFileParams) (string, string, error) {
	if d.store == nil {
		return "", "", protocol.ErrStoreUnavailable
	}
	pin, err := d.store.ReadPinnedLoop(p.ID)
	if err != nil {
		return "", "", err
	}
	root := pin.Loop.ComponentRoot
	if err := validateLoopWorkspace(root); err != nil {
		return "", "", err
	}
	path := p.Path
	if path == "" {
		path = "."
	}
	if !allowedLoopFilePath(path) {
		return "", "", protocol.ErrBadParams{Err: fmt.Errorf("hidden or dependency paths are unavailable in the document reader")}
	}
	resolved := tools.Resolve(root, path)
	if !resolved.OK {
		return "", "", protocol.ErrBadParams{Err: fmt.Errorf("%s", resolved.Reason)}
	}
	return root, resolved.Abs, nil
}

func (d *Daemon) listLoopFiles(_ context.Context, p protocol.LoopFileParams) (any, error) {
	root, abs, err := d.loopFilePath(p)
	if err != nil {
		return nil, err
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return nil, err
	}
	files := make([]loopFileEntry, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" {
			continue
		}
		rel, err := filepath.Rel(realRoot, filepath.Join(abs, name))
		if err != nil {
			continue
		}
		resolved := tools.Resolve(root, rel)
		if !resolved.OK {
			continue
		}
		info, err := os.Stat(resolved.Abs)
		if err != nil {
			continue
		}
		kind := "file"
		if info.IsDir() {
			kind = "directory"
		} else if !info.Mode().IsRegular() {
			continue
		} else if !readableLoopFile(name) {
			continue
		}
		files = append(files, loopFileEntry{Path: filepath.ToSlash(rel), Name: name, Kind: kind})
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].Kind != files[j].Kind {
			return files[i].Kind == "directory"
		}
		return strings.ToLower(files[i].Name) < strings.ToLower(files[j].Name)
	})
	return map[string]any{"path": p.Path, "files": files}, nil
}

func (d *Daemon) readLoopFile(_ context.Context, p protocol.LoopFileParams) (any, error) {
	if !readableLoopFile(p.Path) {
		return nil, protocol.ErrBadParams{Err: fmt.Errorf("only Markdown, text, JSON and YAML files can be opened")}
	}
	_, abs, err := d.loopFilePath(p)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, protocol.ErrBadParams{Err: fmt.Errorf("path is not a regular file")}
	}
	if info.Size() > maxLoopFileBytes {
		return nil, protocol.ErrBadParams{Err: fmt.Errorf("file exceeds 1 MiB reader limit")}
	}
	f, err := os.Open(abs)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxLoopFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxLoopFileBytes || !utf8.Valid(raw) || strings.ContainsRune(string(raw), 0) {
		return nil, protocol.ErrBadParams{Err: fmt.Errorf("file is too large or is not UTF-8 text")}
	}
	return map[string]any{"path": p.Path, "content": string(raw), "bytes": len(raw)}, nil
}
