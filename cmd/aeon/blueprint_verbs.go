package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/protocol"
)

// blueprint_verbs.go — the management verbs of `aeon blueprint` (S48 T11,
// BP12/BP13). Each one is a CLIENT: flags parsed, arguments forwarded, the
// daemon's answer printed. Nothing here validates a blueprint, edits YAML,
// resolves a name against a file, or computes a diff — the daemon answers
// those, and its sentence or report travels verbatim (the CLI's own refusals
// are only about what cannot even be a value).

// runBlueprintDoorVerb is the one-verb-one-method arm.
func runBlueprintDoorVerb(w io.Writer, door blueprintDoor, verb string, args []string) error {
	switch verb {
	case "list":
		return runBlueprintList(w, door, args)
	case "show":
		return runBlueprintShow(w, door, args)
	case "save":
		return runBlueprintSave(w, door, args)
	case "duplicate":
		return runBlueprintDuplicate(w, door, args)
	case "delete":
		return runBlueprintDelete(w, door, args)
	case "revert":
		return runBlueprintRevert(w, door, args)
	case "override":
		return runBlueprintOverride(w, door, args)
	case "history":
		return runBlueprintHistory(w, door, args)
	case "diff":
		return runBlueprintDiff(w, door, args)
	case "usage":
		return runBlueprintUsage(w, door, args)
	case "lint":
		return runBlueprintLint(w, door, args)
	default:
		return fmt.Errorf("unknown blueprint verb %q", verb)
	}
}

// --- list / show ---------------------------------------------------------------------

func runBlueprintList(w io.Writer, door blueprintDoor, args []string) error {
	fs := flag.NewFlagSet("blueprint list", flag.ContinueOnError)
	fs.SetOutput(w)
	origin := fs.String("origin", "", "filter by provenance: system or user")
	q := fs.String("q", "", "case-insensitive substring of the name (filtered by the daemon)")
	asJSON := fs.Bool("json", false, "print the rows as JSON")
	rest, err := blueprintParseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 0 {
		return fmt.Errorf("blueprint list takes no arguments (filters are flags: --origin, --q)")
	}
	if *origin != "" && *origin != "system" && *origin != "user" {
		// The one vocabulary the CLI checks, because the mode flag already
		// does (T3): an origin is a closed pair, and a name outside it cannot
		// be a value — the socket refuses it too, but the refusal a person
		// sees should name the accepted words on both roads, daemon up or
		// down. Everything else about a blueprint is the daemon's judgment.
		return fmt.Errorf("unknown origin %q; accepted: system, user", *origin)
	}
	res, err := door.list(context.Background(), protocol.BlueprintListParams{Origin: *origin, Q: *q})
	if err != nil {
		return err
	}
	if *asJSON {
		return printBlueprintValueJSON(w, res)
	}
	// BP13's one intentional change from T3, now through the door: the names
	// are the ROWS, custom included.
	for _, name := range res.Names {
		fmt.Fprintln(w, name)
	}
	return nil
}

func runBlueprintShow(w io.Writer, door blueprintDoor, args []string) error {
	fs := flag.NewFlagSet("blueprint show", flag.ContinueOnError)
	fs.SetOutput(w)
	asJSON := fs.Bool("json", false, "print the row as JSON")
	rest, err := blueprintParseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return fmt.Errorf("usage: aeon blueprint show NAME [--json]")
	}
	res, err := door.show(context.Background(), rest[0])
	if err != nil {
		return err
	}
	if *asJSON {
		return printBlueprintValueJSON(w, res)
	}
	diverged := ""
	if res.DivergedFromShipped != nil {
		diverged = fmt.Sprintf(" diverged=%t", *res.DivergedFromShipped)
	}
	fmt.Fprintf(w, "%s  origin=%s version=%d revision=%d hash=%s%s\n", res.Name, res.Origin, res.Version, res.Revision, res.ContentHash, diverged)
	fmt.Fprintf(w, "updated %s by %s\n", res.UpdatedAt, res.UpdatedBy)
	renderBlueprintRefs(w, res.References)
	if len(res.Report) > 0 {
		var report blueprint.Report
		if err := json.Unmarshal(res.Report, &report); err != nil {
			return fmt.Errorf("the report could not be read: %w", err)
		}
		renderBlueprintReport(w, report)
	}
	fmt.Fprintln(w)
	fmt.Fprint(w, res.Content) // VERBATIM bytes, exactly as stored (BP-verbatim)
	return nil
}

func renderBlueprintRefs(w io.Writer, refs []protocol.BlueprintRefView) {
	if len(refs) == 0 {
		fmt.Fprintln(w, "references: none")
		return
	}
	var parts []string
	for _, r := range refs {
		parts = append(parts, r.Kind+":"+r.Target)
	}
	fmt.Fprintf(w, "references: %s\n", strings.Join(parts, ", "))
}

// --- the writes: save / duplicate / delete / revert -----------------------------------

func runBlueprintSave(w io.Writer, door blueprintDoor, args []string) error {
	fs := flag.NewFlagSet("blueprint save", flag.ContinueOnError)
	fs.SetOutput(w)
	file := fs.String("file", "", "read the content from this file (required)")
	createOnly := fs.Bool("create-only", false, "create only: a name that exists is a refusal, never an overwrite")
	mode := fs.String("mode", "", "validation mode: simple, standard or poc (absent means standard)")
	note := fs.String("note", "", "a short note stored with the revision")
	rest, err := blueprintParseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return fmt.Errorf("usage: aeon blueprint save NAME --file PATH [--create-only] [--mode M] [--note N]")
	}
	if *file == "" {
		// The one client-side refusal this verb makes: no file, no content.
		// The content is NEVER taken from argv (C17-style hygiene — a
		// blueprint's text belongs in a file, not in the process list).
		return fmt.Errorf("save needs --file PATH: the content is read from a file and sent verbatim")
	}
	content, err := readBlueprintFileArg(*file)
	if err != nil {
		return err
	}
	res, err := door.save(context.Background(), protocol.BlueprintSaveParams{
		Name: rest[0], Content: string(content), CreateOnly: *createOnly, Mode: *mode, Note: *note,
	})
	if err != nil {
		return err
	}
	printBlueprintSaveResult(w, res)
	return nil
}

func runBlueprintDuplicate(w io.Writer, door blueprintDoor, args []string) error {
	fs := flag.NewFlagSet("blueprint duplicate", flag.ContinueOnError)
	fs.SetOutput(w)
	note := fs.String("note", "", "a short note stored with the revision")
	rest, err := blueprintParseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 2 {
		return fmt.Errorf("usage: aeon blueprint duplicate FROM NAME [--note N]")
	}
	res, err := door.duplicate(context.Background(), protocol.BlueprintDuplicateParams{From: rest[0], Name: rest[1], Note: *note})
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "duplicated %s as ", rest[0])
	printBlueprintSaveResult(w, res)
	return nil
}

func runBlueprintDelete(w io.Writer, door blueprintDoor, args []string) error {
	fs := flag.NewFlagSet("blueprint delete", flag.ContinueOnError)
	fs.SetOutput(w)
	yes := fs.Bool("yes", false, "confirm the delete")
	rest, err := blueprintParseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return fmt.Errorf("usage: aeon blueprint delete NAME --yes")
	}
	if !*yes {
		// A seatbelt for typos, not a security control (and it never reaches
		// the daemon, so a mistyped name cannot even be probed by accident).
		return fmt.Errorf("delete refuses without --yes: %s is a row, and the row's history outlives it — re-run with --yes if that is what you mean", rest[0])
	}
	res, err := door.delete(context.Background(), protocol.BlueprintDeleteParams{Name: rest[0]})
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "deleted %s\n", rest[0])
	if res.Note != "" {
		fmt.Fprintln(w, res.Note)
	}
	return nil
}

func runBlueprintRevert(w io.Writer, door blueprintDoor, args []string) error {
	fs := flag.NewFlagSet("blueprint revert", flag.ContinueOnError)
	fs.SetOutput(w)
	rev := fs.Int("rev", 0, "restore this revision's content as a NEW revision (required)")
	yes := fs.Bool("yes", false, "confirm the revert")
	note := fs.String("note", "", "a short note stored with the revision")
	rest, err := blueprintParseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return fmt.Errorf("usage: aeon blueprint revert NAME --rev N --yes [--note N]")
	}
	name := rest[0]
	if !*yes {
		return fmt.Errorf("revert refuses without --yes: %s rev %d becomes the current content as a new revision — re-run with --yes if that is what you mean", name, *rev)
	}
	res, err := door.revert(context.Background(), protocol.BlueprintRevertParams{Name: name, Rev: *rev, Note: *note})
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "reverted %s to revision %d: ", name, *rev)
	printBlueprintSaveResult(w, res)
	return nil
}

func runBlueprintOverride(w io.Writer, door blueprintDoor, args []string) error {
	if len(args) != 3 && len(args) != 4 {
		return fmt.Errorf("usage: aeon blueprint override NAME LANE FIELD [VALUE] — no VALUE clears the override")
	}
	p := protocol.BlueprintOverrideParams{Name: args[0], Lane: args[1], Field: args[2]}
	if len(args) == 4 {
		p.Value = args[3]
	}
	res, err := door.override(context.Background(), p)
	if err != nil {
		return err
	}
	switch {
	case !res.Changed:
		fmt.Fprintf(w, "%s %s %s: unchanged\n", res.Name, res.Lane, res.Field)
	case res.Value == "":
		fmt.Fprintf(w, "%s %s %s: cleared — the lane follows the loop's own again\n", res.Name, res.Lane, res.Field)
	default:
		fmt.Fprintf(w, "%s %s %s: set to %s\n", res.Name, res.Lane, res.Field, res.Value)
	}
	return nil
}

// printBlueprintSaveResult is the ONE sentence for the three write doors
// (save, duplicate, revert): the row's own answer, changed or not.
func printBlueprintSaveResult(w io.Writer, res protocol.BlueprintSaveResult) {
	if !res.Changed {
		fmt.Fprintf(w, "%s: unchanged — the stored bytes already say that, no revision written (origin %s, version %d)\n", res.Name, res.Origin, res.Version)
		return
	}
	fmt.Fprintf(w, "%s: saved revision %d (origin %s, version %d)\n", res.Name, res.Revision, res.Origin, res.Version)
}

// --- history / diff / usage / lint -----------------------------------------------------

func runBlueprintHistory(w io.Writer, door blueprintDoor, args []string) error {
	fs := flag.NewFlagSet("blueprint history", flag.ContinueOnError)
	fs.SetOutput(w)
	limit := fs.Int("limit", 0, "cap the newest-first window (the daemon's bound is 200)")
	rest, err := blueprintParseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return fmt.Errorf("usage: aeon blueprint history NAME [--limit N]")
	}
	res, err := door.history(context.Background(), rest[0], *limit)
	if err != nil {
		return err
	}
	if len(res.Revisions) == 0 {
		fmt.Fprintf(w, "%s has no revisions\n", rest[0])
		return nil
	}
	fmt.Fprintf(w, "%-6s %-9s %-22s %-26s %-14s %s\n", "REV", "OP", "ACTOR", "WHEN", "HASH", "NOTE")
	for _, r := range res.Revisions {
		fmt.Fprintf(w, "%-6d %-9s %-22s %-26s %-14s %s\n", r.Rev, r.Op, r.ActorKind+":"+r.ActorID, r.OccurredAt, shortHash(r.ContentHash), r.Note)
	}
	return nil
}

func shortHash(h string) string {
	if len(h) <= 12 {
		return h
	}
	return h[:12]
}

func runBlueprintDiff(w io.Writer, door blueprintDoor, args []string) error {
	fs := flag.NewFlagSet("blueprint diff", flag.ContinueOnError)
	fs.SetOutput(w)
	a := fs.String("a", "shipped", "the left side: a revision number, `current`, or `shipped`")
	b := fs.String("b", "current", "the right side: a revision number, `current`, or `shipped`")
	rest, err := blueprintParseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return fmt.Errorf("usage: aeon blueprint diff NAME [--a REV|current|shipped] [--b REV|current|shipped]")
	}
	res, err := door.diff(context.Background(), protocol.BlueprintDiffParams{Name: rest[0], A: *a, B: *b})
	if err != nil {
		return err
	}
	if res.Equal {
		// The flag says it, so stdout stays the diff bytes (empty) and the
		// explanation is the side channel — a person piping this to a file
		// gets a valid empty diff either way.
		fmt.Fprintf(blueprintErrOut, "no difference: %s and %s are the same bytes\n", res.ALabel, res.BLabel)
		return nil
	}
	fmt.Fprint(w, res.Diff) // VERBATIM — the daemon computed it (T2's blueprint.Diff)
	if !strings.HasSuffix(res.Diff, "\n") {
		fmt.Fprintln(w)
	}
	return nil
}

func runBlueprintUsage(w io.Writer, door blueprintDoor, args []string) error {
	fs := flag.NewFlagSet("blueprint usage", flag.ContinueOnError)
	fs.SetOutput(w)
	asJSON := fs.Bool("json", false, "print the answer as JSON")
	rest, err := blueprintParseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return fmt.Errorf("usage: aeon blueprint usage NAME [--json]")
	}
	res, err := door.usage(context.Background(), rest[0])
	if err != nil {
		return err
	}
	if *asJSON {
		return printBlueprintValueJSON(w, res)
	}
	fmt.Fprintf(w, "%s is used by:\n", rest[0])
	printBlueprintUsageGroup(w, "schedule", res.Schedules)
	printBlueprintUsageGroup(w, "trigger", res.Triggers)
	printBlueprintUsageGroup(w, "queue item", res.QueueItems)
	fmt.Fprintf(w, "  loops started from it: %d (they carry their pinned content, so they are unaffected by any change here)\n", res.LoopsStarted)
	fmt.Fprintf(w, "it uses:\n")
	printBlueprintUsageNames(w, "agents", res.Agents)
	printBlueprintUsageNames(w, "actions", res.Actions)
	printBlueprintUsageNames(w, "skills", res.Skills)
	return nil
}

func printBlueprintUsageGroup(w io.Writer, kind string, refs []protocol.BlueprintUsageRef) {
	if len(refs) == 0 {
		fmt.Fprintf(w, "  %s: none\n", pluralBlueprint(kind, len(refs)))
		return
	}
	fmt.Fprintf(w, "  %s:\n", pluralBlueprint(kind, len(refs)))
	for _, r := range refs {
		fmt.Fprintf(w, "    %s  %s", r.ID, r.Name)
		if r.Status != "" {
			fmt.Fprintf(w, " [%s]", r.Status)
		}
		fmt.Fprintln(w)
	}
}

func pluralBlueprint(kind string, n int) string {
	if n == 1 {
		return kind
	}
	return kind + "s"
}

func printBlueprintUsageNames(w io.Writer, label string, names []string) {
	if len(names) == 0 {
		fmt.Fprintf(w, "  %s: none\n", label)
		return
	}
	fmt.Fprintf(w, "  %s: %s\n", label, strings.Join(names, ", "))
}

func runBlueprintLint(w io.Writer, door blueprintDoor, args []string) error {
	fs := flag.NewFlagSet("blueprint lint", flag.ContinueOnError)
	fs.SetOutput(w)
	file := fs.String("file", "", "lint this file instead of a stored blueprint")
	asJSON := fs.Bool("json", false, "print the advisories as JSON")
	rest, err := blueprintParseFlags(fs, args)
	if err != nil {
		return err
	}
	if *file == "" && len(rest) == 0 {
		return fmt.Errorf("usage: aeon blueprint lint NAME | --file PATH [--json]")
	}
	if *file != "" && len(rest) != 0 {
		return fmt.Errorf("lint takes a NAME or --file PATH, not both")
	}
	if *file == "" && len(rest) == 1 && looksLikePath(rest[0]) {
		*file = rest[0]
	}
	if *file != "" {
		// The offline tool: no daemon, no store, and no tools catalog — the
		// daemon's OWN unavailable answer, stated the same way (V13). The
		// load check is the daemon's sentence too (BlueprintOfflineLint).
		content, err := readBlueprintFileArg(*file)
		if err != nil {
			return err
		}
		// The notice after the read, as `validate --file` says it: a person
		// who typed a path that does not exist gets the refusal, not a
		// preamble about which agent set validated nothing.
		fmt.Fprintln(blueprintErrOut, blueprintOfflineFileNotice)
		res, err := daemon.BlueprintOfflineLint(content)
		if err != nil {
			return err
		}
		return printBlueprintLint(w, res, *asJSON)
	}
	if len(rest) != 1 {
		return fmt.Errorf("lint takes exactly one blueprint name")
	}
	res, err := door.lint(context.Background(), protocol.BlueprintLintParams{Name: rest[0]})
	if err != nil {
		return err
	}
	return printBlueprintLint(w, res, *asJSON)
}

func printBlueprintLint(w io.Writer, res protocol.BlueprintLintResult, asJSON bool) error {
	if asJSON {
		return printBlueprintValueJSON(w, res)
	}
	if res.Unavailable != "" {
		fmt.Fprintf(w, "advisories unavailable: %s (evaluated %s)\n", res.Unavailable, res.EvaluatedAt)
	}
	if len(res.Advisories) == 0 && res.Unavailable == "" {
		fmt.Fprintf(w, "no advisories (evaluated %s)\n", res.EvaluatedAt)
		return nil
	}
	for _, a := range res.Advisories {
		fmt.Fprintf(w, "%-10s %-14s %s %s %s — %s\n", a.State, a.Lane, a.Step, a.Agent, a.Tool, a.Reason)
	}
	if res.Unavailable == "" {
		fmt.Fprintf(w, "advisory only: lint never changes validate, save or the loop's behaviour (evaluated %s)\n", res.EvaluatedAt)
	}
	return nil
}

// --- export: the disposable mirror ------------------------------------------------------

// runBlueprintExport writes rows out as files for an editor or a git shelf.
// It is a MIRROR and nothing more: no code path reads the directory back
// (TestExportIsAMirrorNeverReadBack is the grep for that claim), and the
// footer says so on every run so the habit of editing there cannot form.
func runBlueprintExport(w io.Writer, door blueprintDoor, args []string) error {
	fs := flag.NewFlagSet("blueprint export", flag.ContinueOnError)
	fs.SetOutput(w)
	out := fs.String("out", filepath.Join(".", "blueprints"), "the directory to write into")
	rest, err := blueprintParseFlags(fs, args)
	if err != nil {
		return err
	}
	names := rest
	if len(names) == 0 {
		res, err := door.list(context.Background(), protocol.BlueprintListParams{})
		if err != nil {
			return err
		}
		names = res.Names
	}
	if len(names) == 0 {
		return fmt.Errorf("there is nothing to export — no names given and the store holds no blueprints")
	}
	if err := os.MkdirAll(*out, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", *out, err)
	}
	for _, name := range names {
		row, err := door.show(context.Background(), name)
		if err != nil {
			return err
		}
		path := filepath.Join(*out, name+".yaml")
		if err := os.WriteFile(path, []byte(row.Content), 0o600); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
		fmt.Fprintln(w, path)
	}
	fmt.Fprintf(w, "exported %d blueprint(s) to %s — a disposable mirror, regenerable from the rows and never read back\n", len(names), *out)
	return nil
}

// printBlueprintValueJSON prints a door's typed answer with the same shape
// every other `--json` verb uses.
func printBlueprintValueJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// renderBlueprintReport is the report's text form — the ONE renderer (T3's
// bytes, shared by validate and show). It never decides an exit code; only
// the validate verb's `report.OK(strict)` does.
func renderBlueprintReport(w io.Writer, report blueprint.Report) {
	valid, invalid, unverifiable := report.Counts()
	fmt.Fprintf(w, "%s schema %d version %d: %d valid checks, %d invalid, %d unverifiable\n", report.Blueprint, report.SchemaVersion, report.Version, valid, invalid, unverifiable)
	for _, d := range report.Diagnostics {
		fmt.Fprintf(w, "  %s %-20s %s: %s", d.State, d.Kind, d.Path, d.Msg)
		if d.Slice != "" {
			fmt.Fprintf(w, " (%s)", d.Slice)
		}
		if d.Hint != "" {
			fmt.Fprintf(w, " %s", d.Hint)
		}
		fmt.Fprintln(w)
	}
}
