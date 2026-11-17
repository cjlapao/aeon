package main

// export.go — S62 T8 (BK14): the human door for writing the managed records
// out as a read-only, deterministic, secret-free text snapshot.
//
//	aeon export [--records a,b,c] --dir DIR [--include-memory] [--json]
//	aeon export sets [--json]
//
// Socket-first (P34): with the daemon home both verbs are clients of
// export.run / export.sets, and the run's ledger row is the daemon's.
// "Nobody is home" — the one noDaemonError dial fault, and no other
// (every other dial fault is refused as-is, never a fallback, the S45 T8
// rule) — is the DIRECT door: the store opened read-only (SkipMigrations —
// the direct door never migrates, behaviour 6's spy), the run stamped into
// the manifest as actor {cli, cli}, the fixed stderr line saying where the
// answer came from, and NO ledger row (only the daemon's door records
// runs). Both doors render through the same printer, so the bytes a person
// reads are the door's, not the transport's (P5's parity).

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/export"
	"github.com/cjlapao/aeon/internal/paths"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// exportDirectLine is the direct door's fixed stderr line: which door
// answered (this one, not the daemon's) and the one fact that follows from
// it — the ledger row is the daemon's door's, so a direct run is not in it.
// It is fixed (pinned by test) because a line that varies with the machine
// is a line a script cannot depend on.
const exportDirectLine = "aeon: no daemon — the export door ran directly on the store (read-only, no migrations); the ledger row is written only by the daemon's door"

// cmdExport is the export family's parent verb: a subcommand is the
// listing door, and the rest of the arguments are one run.
func cmdExport(w io.Writer, args []string) error {
	if len(args) == 0 {
		return &usageError{msg: "aeon export: --dir DIR is needed (or `aeon export sets` to list the export sets)"}
	}
	if args[0] == "sets" {
		return cmdExportSets(w, args[1:])
	}
	return cmdExportRun(w, args)
}

// parseRecords is the --records flag as the engine's set list: a
// comma-separated subset of the declared sets, in the order typed,
// duplicates dropped. An empty flag is the default run (every
// non-optional set), not an error — the spelling "none" is the absence.
func parseRecords(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, name := range strings.Split(v, ",") {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

func cmdExportRun(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	fs.SetOutput(w)
	records := fs.String("records", "", "the sets to export, comma-separated (default: every non-optional set)")
	dir := fs.String("dir", "", "the absolute path of the empty (or not-yet-existing) directory to fill")
	includeMemory := fs.Bool("include-memory", false, "include the memory set (the learnings) — opt-in, absent from a default export")
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return &usageError{msg: fmt.Sprintf("aeon export: %q is not a flag — the flags are --records, --dir, --include-memory and --json", fs.Arg(0))}
	}
	if *dir == "" {
		return &usageError{msg: "aeon export: --dir DIR is needed — the absolute path of the empty (or not-yet-existing) directory to fill"}
	}
	sets := parseRecords(*records)

	// P34: the socket first. The daemon's door stamps the actor from the
	// connection and writes the ledger row; the direct door stamps {cli,
	// cli} into the manifest and writes no row.
	c, derr := dial(0)
	if derr == nil {
		defer c.Close()
		var res protocol.ExportRunResult
		if err := c.Call(protocol.MethodExportRun, protocol.ExportRunParams{
			Sets:          sets,
			Dir:           *dir,
			IncludeMemory: *includeMemory,
		}, &res); err != nil {
			return err
		}
		return renderExportRun(w, res, *asJSON)
	}
	var nd *noDaemonError
	if !errors.As(derr, &nd) {
		return derr // not "nobody is home" — refused as-is, never a fallback
	}
	return exportRunDirect(w, sets, *dir, *includeMemory, *asJSON)
}

func cmdExportSets(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("export sets", flag.ContinueOnError)
	fs.SetOutput(w)
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return &usageError{msg: fmt.Sprintf("aeon export sets: %q is not a flag — the only flag is --json", fs.Arg(0))}
	}

	c, derr := dial(0)
	if derr == nil {
		defer c.Close()
		var res protocol.ExportSetsResult
		if err := c.Call(protocol.MethodExportSets, struct{}{}, &res); err != nil {
			return err
		}
		return renderExportSets(w, res, *asJSON)
	}
	var nd *noDaemonError
	if !errors.As(derr, &nd) {
		return derr // not "nobody is home" — refused as-is, never a fallback
	}
	// The direct door over the same probe the daemon's door runs: the
	// schema's table existence, in the registry's own order.
	stateDir, err := paths.StoreDir()
	if err != nil {
		return fmt.Errorf("aeon: the state dir will not resolve: %v", err)
	}
	st, err := store.Open(store.Options{
		Dir:            stateDir,
		SkipMigrations: true, // the direct door never migrates
	})
	if err != nil {
		return fmt.Errorf("aeon: the store will not open for the direct export: %v", err)
	}
	defer st.Close()
	fmt.Fprintln(os.Stderr, exportDirectLine)
	return renderExportSets(w, protocol.ExportSetsFromProbe(st.TableExists), *asJSON)
}

// exportRunDirect is the run's direct door (see the file header): the store
// opened read-only and never migrated, the engine over it with the actor
// stamped {cli, cli}, the fixed stderr line, no ledger row.
func exportRunDirect(w io.Writer, sets []string, dir string, includeMemory, asJSON bool) error {
	stateDir, err := paths.StoreDir()
	if err != nil {
		return fmt.Errorf("aeon: the state dir will not resolve: %v", err)
	}
	st, err := store.Open(store.Options{
		Dir:            stateDir,
		SkipMigrations: true, // the direct door never migrates
	})
	if err != nil {
		return fmt.Errorf("aeon: the store will not open for the direct export: %v", err)
	}
	defer st.Close()

	res, runErr := export.Run(context.Background(), export.Deps{
		Reader:  st,
		Clock:   time.Now,
		Version: Version,
		Schema:  len(store.Migrations()),
	}, export.Opts{
		Dir:           dir,
		Sets:          sets,
		IncludeMemory: includeMemory,
		Actor:         export.Actor{Kind: "cli", ID: "cli"},
	})
	if runErr != nil {
		// The engine's sentence (with its "export: " prefix — the direct
		// door keeps it, where the daemon's door strips it for the wire)
		// is the whole record: nothing changed, so nothing is logged.
		return errors.New(runErr.What)
	}
	fmt.Fprintln(os.Stderr, exportDirectLine)
	view := protocol.ExportRunResultFromEngine(res)
	// view.Recorded stays false: the direct door writes no ledger row.
	return renderExportRun(w, view, asJSON)
}

// --- the one renderer, shared by both doors ----------------------------------
//
// A human rendering and the wire rendering, and nothing else: the --json
// contract is the wire shape verbatim (printJSON's), the human rendering
// is the screen's words (paths may appear here — this is the screen, not
// the log).

func renderExportRun(w io.Writer, res protocol.ExportRunResult, asJSON bool) error {
	if asJSON {
		return printJSON(w, res)
	}
	fmt.Fprintf(w, "export %s — %d files, %d sets\n", res.Dir, res.Files, len(res.Sets))
	for _, s := range res.Sets {
		rows := "— rows"
		if s.Rows != nil {
			rows = fmt.Sprintf("%d rows", *s.Rows)
		}
		fmt.Fprintf(w, "  %-12s %-22s %s\n", s.Name, s.State, rows)
	}
	if res.Recorded {
		fmt.Fprintln(w, "the run is in the ledger (kind export)")
	} else {
		fmt.Fprintln(w, "the run is not in the ledger (direct door)")
	}
	return nil
}

func renderExportSets(w io.Writer, res protocol.ExportSetsResult, asJSON bool) error {
	if asJSON {
		return printJSON(w, res)
	}
	fmt.Fprintf(w, "export sets — %d declared\n", len(res.Sets))
	for _, s := range res.Sets {
		fmt.Fprintf(w, "  %-12s %s\n", s.Name, s.State)
	}
	return nil
}
