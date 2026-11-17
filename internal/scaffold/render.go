// Package scaffold turns a template into files on disk, and nothing else
// (GI9): a closed renderer (the four shipped variables and three functions,
// never the caller's data as code), a preview that covers exactly what the
// write will do, a new-path-only writer, and the target guard. No git, no
// network — those belong to T4's bootstrap actions.
package scaffold

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

// The output ceilings (GI9). A template that cannot render inside these is
// refused before a byte is written.
const (
	MaxFiles     = 200
	MaxTotalByte = 512 * 1024
	MaxFileByte  = 128 * 1024
)

// Vars is the ONLY variable set (GI9): a project's identity words. There
// is deliberately no map for "extra" values — an unknown {{.Foo}} fails at
// render, which is what keeps a template honest about what it may ask for.
type Vars struct {
	Name          string
	Slug          string
	Description   string
	Stack         []string
	DefaultBranch string
}

// File is one rendered file: a validated relative path, a mode from the
// closed pair, content.
type File struct {
	Path    string
	Mode    os.FileMode
	Content []byte
}

// Template is the renderer's input — one row's shipped files, already
// decoded from the store's JSON (DecodeFiles does the shape checks so a
// corrupt row is an error, not a half-render).
type Template struct {
	Slug  string
	Files []TemplateFile
}

// TemplateFile is one shipped file: path and content are TEMPLATE source
// (paths may hold {{.Slug}}), mode is the literal file mode.
type TemplateFile struct {
	Path    string
	Mode    os.FileMode
	Content string
}

// funcs is the closed function map (GI9): exactly lower, upper, join. No
// env, no file read, no exec — the renderer has no road out of strings.
var funcs = template.FuncMap{
	"lower": strings.ToLower,
	"upper": strings.ToUpper,
	"join":  strings.Join,
}

// Render applies Vars to every shipped path and body. missingkey=error is
// belt on suspenders — the variable set is a struct, so a typo is an
// execution error, not a silent empty; a template that asks for more than
// the closed set is REFUSED with its own error.
func Render(t Template, v Vars) ([]File, error) {
	if len(t.Files) == 0 {
		return nil, fmt.Errorf("scaffold: template %q ships no files — there is nothing to write", t.Slug)
	}
	if len(t.Files) > MaxFiles {
		return nil, fmt.Errorf("scaffold: template %q ships %d files, over the %d cap", t.Slug, len(t.Files), MaxFiles)
	}
	var (
		out      []File
		total    int
		seenPath = map[string]string{}
	)
	for _, tf := range t.Files {
		path, err := renderOne("path:"+tf.Path, tf.Path, v)
		if err != nil {
			return nil, fmt.Errorf("scaffold: template %q: %w", t.Slug, err)
		}
		content, err := renderOne("content:"+tf.Path, tf.Content, v)
		if err != nil {
			return nil, fmt.Errorf("scaffold: template %q: %w", t.Slug, err)
		}
		clean, err := validateRelPath(path)
		if err != nil {
			return nil, fmt.Errorf("scaffold: template %q renders %w", t.Slug, err)
		}
		if tf.Mode != 0o644 && tf.Mode != 0o755 {
			return nil, fmt.Errorf("scaffold: template %q: file %q asks for mode %o — the only modes are 0644 and 0755", t.Slug, clean, tf.Mode)
		}
		if len(content) > MaxFileByte {
			return nil, fmt.Errorf("scaffold: template %q: file %q renders %d bytes, over the %d per-file cap", t.Slug, clean, len(content), MaxFileByte)
		}
		total += len(content)
		if total > MaxTotalByte {
			return nil, fmt.Errorf("scaffold: template %q renders %d bytes in total, over the %d cap", t.Slug, total, MaxTotalByte)
		}
		fold := strings.ToLower(clean)
		if prev, dup := seenPath[fold]; dup {
			if prev == clean {
				return nil, fmt.Errorf("scaffold: template %q ships the path %q twice", t.Slug, clean)
			}
			return nil, fmt.Errorf("scaffold: template %q ships %q and %q — a case-fold collision, and a case-insensitive filesystem would lose one", t.Slug, prev, clean)
		}
		seenPath[fold] = clean
		out = append(out, File{Path: clean, Mode: tf.Mode, Content: []byte(content)})
	}
	return out, nil
}

func renderOne(name, text string, v Vars) (string, error) {
	tmpl, err := template.New(name).Funcs(funcs).Option("missingkey=error").Parse(text)
	if err != nil {
		return "", fmt.Errorf("template %q does not parse: %w", name, err)
	}
	var sb strings.Builder
	if err := tmpl.Execute(&sb, v); err != nil {
		return "", fmt.Errorf("template %q does not render: %w", name, err)
	}
	return sb.String(), nil
}

// validateRelPath is GI9's path law over RENDERED output: relative, cleaned,
// no `..`, no `.git` segment, no NUL, ≤ 255 bytes. The store checks a
// template's shipped spelling (T1); this checks what it became after
// rendering, where a variable could have smuggled an escape in.
func validateRelPath(p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("an empty file path")
	}
	if len(p) > 255 {
		return "", fmt.Errorf("path %q is over 255 bytes", p)
	}
	if strings.ContainsRune(p, 0) {
		return "", fmt.Errorf("path %q contains a NUL", p)
	}
	if strings.HasPrefix(p, "/") || filepath.IsAbs(p) {
		return "", fmt.Errorf("path %q is absolute — a template writes inside the target, nowhere else", p)
	}
	if strings.Contains(p, "\\") {
		return "", fmt.Errorf("path %q holds a backslash", p)
	}
	clean := filepath.ToSlash(filepath.Clean(p))
	if clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return "", fmt.Errorf("path %q escapes the target directory", p)
	}
	if clean == "." || clean == "/" {
		return "", fmt.Errorf("path %q names the target directory itself, not a file in it", p)
	}
	for _, seg := range strings.Split(clean, "/") {
		if seg == ".git" {
			return "", fmt.Errorf("path %q carries a .git segment — the scaffold never writes git state; git belongs to the action that runs it", p)
		}
	}
	return clean, nil
}
