package main

// ════════════════════════════════════════════════════════════════════════════
// `aeon handlers` — list what is registered; verify what would be.
//
// `verify` is the slice's mechanical guard (S13 T2): the only automatable
// check over the 2,902-line prose rewriting, and the reason it exists before
// any rewrite lands. `--file` runs it against ONE file — the negative
// control's door: `aeon handlers verify --file ../opencode/agents/
// golang-engineer-loop.md --original` MUST fail, because a guard that passes
// the originals is measuring nothing (D1/C2).
//
// Exit codes are the contract (so CI parses nothing): 0 nothing refused, 1
// something refused, 2 usage. Findings render in the refusal style — thing
// and remedy — on stdout, where a reviewer reads them; stderr carries only
// usage.
//
// opencode/ is read-only (A10): verify READS originals and writes nothing
// beside them.
// ════════════════════════════════════════════════════════════════════════════

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cjlapao/aeon/internal/handler"
)

// cmdHandlers returns the process exit code itself — the verify contract is
// an exit code, not an error string for renderError to decorate.
func cmdHandlers(stdout, stderr io.Writer, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: aeon handlers list | aeon handlers verify [--file PATH] [--original] [--bodies DIR]")
		return 2
	}
	switch args[0] {
	case "list":
		names := handler.Names()
		sort.Strings(names)
		for _, n := range names {
			fmt.Fprintln(stdout, n)
		}
		return 0
	case "verify":
		return handlersVerify(stdout, stderr, args[1:])
	default:
		fmt.Fprintf(stderr, "aeon handlers: unknown subcommand %q — list or verify\n", args[0])
		return 2
	}
}

func handlersVerify(stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("handlers verify", flag.ContinueOnError)
	fs.SetOutput(stderr)
	file := fs.String("file", "", "verify one handler file instead of the bodies directory")
	original := fs.Bool("original", false, "the --file is an original under opencode/ (read-only inputs; the negative control runs this way)")
	bodies := fs.String("bodies", "", "directory of rewrites to sweep (default: internal/handler/bodies)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	var findings []handler.Finding
	if *file != "" {
		fs2, rej := handler.VerifyFile(*file, *original, nil)
		findings = fs2
		for _, f := range findings {
			fmt.Fprintln(stdout, f.String())
		}
		if rej != nil && len(findings) == 0 {
			fmt.Fprintf(stdout, "refused: %s\n", rej.Error())
			return 1
		}
	} else {
		dir := *bodies
		if dir == "" {
			dir = defaultBodiesDir()
			if dir == "" {
				fmt.Fprintln(stderr, "aeon handlers verify: no --bodies directory given and internal/handler/bodies was not found walking up — name the directory rather than verifying nothing")
				return 2
			}
		}
		fs2, rejects := handler.VerifyAllBodies(dir, nil)
		findings = fs2
		for _, f := range findings {
			fmt.Fprintln(stdout, f.String())
		}
		for _, r := range rejects {
			if r.IO {
				fmt.Fprintf(stderr, "aeon handlers verify: %v\n", r)
				return 2
			}
		}
	}
	if len(findings) > 0 {
		fmt.Fprintf(stdout, "%d refusal(s)\n", len(findings))
		return 1
	}
	return 0
}

// defaultBodiesDir walks up looking for the package's bodies directory —
// the same discovery convention corpusgen and the originals' tests use.
func defaultBodiesDir() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for i := 0; i < 8; i++ {
		cand := filepath.Join(dir, "internal", "handler", "bodies")
		if st, err := os.Stat(cand); err == nil && st.IsDir() {
			return cand
		}
		cand = filepath.Join(dir, "bodies") // already inside internal/handler
		if st, err := os.Stat(cand); err == nil && st.IsDir() && strings.Contains(dir, "handler") {
			return cand
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}
