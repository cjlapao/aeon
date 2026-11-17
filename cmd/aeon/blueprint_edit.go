package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/cjlapao/aeon/internal/execenv"
	"github.com/cjlapao/aeon/internal/protocol"
)

// blueprint_edit.go — `aeon blueprint edit` (S48 T11, BP12/BP13): the editor
// round-trip for people who live in one. It is a COMPOSITION of two socket
// methods, show and save — never a file write and never a third write path:
//
//	show NAME → temp file (0600) → $VISUAL/$EDITOR → read back → save NAME
//
// Three outcomes, each stated:
//
//   - unchanged bytes: "no changes", NO call to save, temp removed;
//   - changed and valid: one save, the door's own result line;
//   - changed and INVALID: the daemon's report printed, the temp file KEPT
//     and its path named — an edit is never thrown away because it was wrong.
//
// Content never travels on a command line (C17-style hygiene: argv is in the
// process list and in shell history), and with no editor set the verb refuses
// by naming both variables rather than hanging on a prompt.

func runBlueprintEdit(w io.Writer, door blueprintDoor, args []string) error {
	fs := flag.NewFlagSet("blueprint edit", flag.ContinueOnError)
	fs.SetOutput(w)
	note := fs.String("note", "", "a short note stored with the revision")
	rest, err := blueprintParseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return fmt.Errorf("usage: aeon blueprint edit NAME [--note N]")
	}
	name := rest[0]
	editor, err := blueprintEditorArgs()
	if err != nil {
		return err
	}
	row, err := door.show(context.Background(), name)
	if err != nil {
		return err
	}
	path, err := writeBlueprintTempFile(name, row.Content)
	if err != nil {
		return err
	}
	if err := runBlueprintEditor(editor, path); err != nil {
		// The editor itself failed (a bad $EDITOR, a killed session). The
		// file stays: whatever the person wrote is still there to recover.
		fmt.Fprintf(blueprintErrOut, "kept %s\n", path)
		return err
	}
	edited, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read the edited file back: %w", err)
	}
	if string(edited) == row.Content {
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove the unchanged temp file: %w", err)
		}
		fmt.Fprintf(w, "%s: no changes — nothing saved, %s removed\n", name, path)
		return nil
	}
	res, err := door.save(context.Background(), protocol.BlueprintSaveParams{
		Name: name, Content: string(edited), Note: *note,
	})
	if err != nil {
		// Invalid, or any other refusal: the daemon's sentence — report
		// included, verbatim — and the edit survives to be fixed.
		fmt.Fprintf(blueprintErrOut, "the edit was NOT saved; your text is still at %s\n", path)
		return err
	}
	fmt.Fprintf(w, "edited and saved through %s\n", path)
	printBlueprintSaveResult(w, res)
	// The save happened; the scratch file is the mirror and goes.
	if err := os.Remove(path); err != nil {
		fmt.Fprintf(blueprintErrOut, "the temp file %s could not be removed: %v\n", path, err)
	}
	return nil
}

// writeBlueprintTempFile puts the stored bytes in a 0600 scratch file under
// $TMPDIR (os.TempDir honours it), named for the blueprint so an editor
// buffer says what it is. 0600 because a blueprint is the person's own text
// and the mode costs nothing.
func writeBlueprintTempFile(name, content string) (string, error) {
	f, err := os.CreateTemp("", "aeon-blueprint-"+sanitizeBlueprintName(name)+"-*.yaml")
	if err != nil {
		return "", fmt.Errorf("create the temp file: %w", err)
	}
	path := f.Name()
	if _, err := f.WriteString(content); err != nil {
		_ = f.Close()
		return "", fmt.Errorf("write the temp file: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("write the temp file: %w", err)
	}
	return path, nil
}

// sanitizeBlueprintName keeps a name from ever becoming a path: a blueprint
// name is a slug, and anything surprising in it is flattened rather than
// interpreted.
func sanitizeBlueprintName(name string) string {
	flat := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		default:
			return '_'
		}
	}, name)
	if len(flat) > 40 {
		flat = flat[:40]
	}
	if flat == "" || flat == "." || flat == ".." {
		flat = "draft"
	}
	return flat
}

// blueprintEditorArgs resolves the editor: $VISUAL first (the POSIX
// convention: VISUAL for a full-screen editor), then $EDITOR. A command may
// carry arguments (`EDITOR="code -w"`), so the value is split, not treated as
// one binary. With neither set the refusal NAMES both — a guess at `vi` would
// open an editor the person never chose, and a silent exec of an empty
// command would hang the verb.
func blueprintEditorArgs() ([]string, error) {
	for _, key := range []string{"VISUAL", "EDITOR"} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			if parts := strings.Fields(v); len(parts) > 0 {
				return parts, nil
			}
		}
	}
	return nil, fmt.Errorf("no editor is set — set $VISUAL or $EDITOR (or use `aeon blueprint export` plus `aeon blueprint save NAME --file PATH`)")
}

// runBlueprintEditor runs the editor on the file with the person's terminal
// attached. The file path is the ONLY thing passed — never the content.
//
// The child gets `execenv.EditorEnv`, the closed editor allowlist (S59 EV1).
// Spawning with no `cmd.Env` set — the shape this verb had until S59's spawn
// guard caught it — hands the editor whatever the caller's shell holds, and a
// shell is exactly where a `GH_TOKEN` or an `ANTHROPIC_API_KEY` lives. The
// allowlist carries what an editor needs (TERM, DISPLAY, the editor choice)
// and nothing that looks like a credential.
func runBlueprintEditor(editor []string, path string) error {
	cmd := exec.Command(editor[0], append(editor[1:], path)...)
	cmd.Env = execenv.EditorEnv(os.Environ())
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("the editor %q exited badly: %w", strings.Join(editor, " "), err)
	}
	return nil
}
