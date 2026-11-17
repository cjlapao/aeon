package main

// cmd/aeon/charter.go — S61 T10 (standing rule 1, P5's client half). The
// person's doors onto a root's charter, as a pure client: every verb
// dials, calls, prints, and re-validates nothing. The validator's
// sentences — a missing goal, an over-long item, a forbidden autonomy
// verb — come back from the daemon verbatim; the only client-side
// refusals are inputs that cannot be a value at all.
//
// The recorded P5 deviation this slice owns (S60's EV18, S27's precedent):
// there is NO offline door. A charter committed without a daemon is not a
// thing — the record export, the notify and the same-transaction revision
// ride the daemon's writes, and a second process must not file them.
// `charterDial` is the only road out of this file
// (TestS61VerbsAreSocketClients is the teeth).
//
// The actor is never a flag (P4): the connection resolves the person. No
// flag takes a secret (C17), and `save` reads the body from a FILE path —
// stdin is refused, the S45 rule kept by parity ("no stdin payloads for
// authority writes").

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/cjlapao/aeon/internal/protocol"
)

// charterVerbs names each verb and the socket method it drives; the
// parity tests walk both directions against these rows.
var charterVerbs = []cliVerb{
	{"list", protocol.MethodCharterList, false},
	{"show", protocol.MethodCharterGet, false},
	{"history", protocol.MethodCharterRevisions, false},
	{"save", protocol.MethodCharterSave, false},
	{"vocabulary", protocol.MethodCharterVocabulary, false},
}

// charterDial is the dial every verb shares; a var so the in-process
// tests can stand a daemon behind it (the schedDial idiom).
var charterDial = dial

func cmdCharter(w io.Writer, args []string) error {
	if len(args) == 0 {
		printCharterUsage(w)
		return nil
	}
	verb, rest := args[0], args[1:]
	switch verb {
	case "list":
		return charterVerbList(w, rest)
	case "show":
		return charterVerbShow(w, rest)
	case "history":
		return charterVerbHistory(w, rest)
	case "save":
		return charterVerbSave(w, rest)
	case "vocabulary":
		return charterVerbVocabulary(w, rest)
	case "help":
		printCharterUsage(w)
		return nil
	default:
		printCharterUsage(w)
		return &usageError{msg: fmt.Sprintf("unknown `charter` subcommand %q — `aeon charter help` lists the five", verb)}
	}
}

func printCharterUsage(w io.Writer) {
	fmt.Fprint(w, `usage: aeon charter <verb> — a root's charter: what it promises, as the daemon stores it (S61)

  list       [--json] — every chartered root, current revision, one line each
  show       ROOT [--revision N] [--json] — one charter at one revision
  history    ROOT [--limit N] [--cursor REV] [--json] — revisions, newest first
  save       ROOT --file PATH [--note TEXT] — file a new revision (a person's
             decision: the proposal flow is the agent's side, this is yours)
  vocabulary [--json] — the sets and caps the validator ENFORCES

  Every verb is a client of the daemon's socket — there is no offline door
  (the recorded P5 deviation). --file refuses '-': no stdin payloads for
  authority writes. The daemon's own sentences answer every refusal; the
  CLI re-validates nothing except inputs that cannot be a value.
`)
}

// charterBodyFromPath reads the save body from a file path. '-' is
// refused with the standing sentence — a body is not a secret, but the
// rule keeps parity with S45's writes; un-parseable JSON is refused here
// only because it cannot encode as a value at all, never re-validated
// beyond that (the validator's law is the daemon's).
func charterBodyFromPath(path string) (json.RawMessage, error) {
	if path == "" || path == "-" {
		return nil, &usageError{msg: "`save` reads the body from --file PATH — '-' (stdin) is refused: no stdin payloads for authority writes (the S45 rule, kept for charters)"}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("`save` cannot read the body file: %w", err)
	}
	if !json.Valid(raw) {
		return nil, &usageError{msg: fmt.Sprintf("the body file %q is not valid JSON — the daemon's validator words every law on top of a parseable body, but nothing parses bytes that are not JSON", path)}
	}
	return json.RawMessage(raw), nil
}

func charterVerbList(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("charter list", flag.ContinueOnError)
	fs.SetOutput(w)
	asJSON := fs.Bool("json", false, "print the wire result verbatim instead of the table")
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return &usageError{"charter list takes no positional arguments — `aeon charter list [--json]`"}
	}
	c, err := charterDial(*want)
	if err != nil {
		return err
	}
	defer c.Close()
	if *asJSON {
		return callJSON(w, c, protocol.MethodCharterList, struct{}{})
	}
	var res protocol.CharterListResult
	if err := c.Call(protocol.MethodCharterList, struct{}{}, &res); err != nil {
		return err
	}
	if len(res.Charters) == 0 {
		fmt.Fprint(w, "no charters — `aeon charter save ROOT --file BODY.json` files the first\n")
		return nil
	}
	for _, e := range res.Charters {
		line := fmt.Sprintf("%s · rev %d · %s", e.RootID, e.Revision, e.Name)
		if e.Summary != "" {
			line += " · " + e.Summary
		}
		if e.HasProposed {
			line += " · proposal pending"
		}
		fmt.Fprintln(w, line)
	}
	return nil
}

func charterVerbShow(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("charter show", flag.ContinueOnError)
	fs.SetOutput(w)
	asJSON := fs.Bool("json", false, "print the wire result verbatim instead of the table")
	revision := fs.Int("revision", 0, "read this revision out of history (default: the current one)")
	want := addProtocolVersionFlag(fs)
	positional, rest := splitPositional(args, 1)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if len(positional) != 1 || fs.NArg() != 0 {
		return &usageError{"charter show names exactly one root — `aeon charter show ROOT [--revision N] [--json]`"}
	}
	p := protocol.CharterGetParams{RootID: positional[0]}
	if *revision != 0 {
		p.Revision = revision
	}
	c, err := charterDial(*want)
	if err != nil {
		return err
	}
	defer c.Close()
	if *asJSON {
		return callJSON(w, c, protocol.MethodCharterGet, p)
	}
	var view protocol.CharterView
	if err := c.Call(protocol.MethodCharterGet, p, &view); err != nil {
		return err
	}
	fmt.Fprintf(w, "%s · revision %d of %d · %s\n", view.RootID, view.Revision, view.TotalRevisions, view.CreatedAt)
	fmt.Fprintf(w, "actor: %s/%s · source: %s %s\n", view.ActorKind, view.ActorID, view.Source, view.SourceRef)
	if view.Summary != "" {
		fmt.Fprintf(w, "summary: %s\n", view.Summary)
	}
	if len(view.Body) > 0 {
		fmt.Fprintf(w, "body:\n%s\n", string(view.Body))
	}
	if view.Proposed != nil {
		fmt.Fprint(w, "proposal pending (the agent's side — desk list answers for it)\n")
	}
	return nil
}

func charterVerbHistory(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("charter history", flag.ContinueOnError)
	fs.SetOutput(w)
	asJSON := fs.Bool("json", false, "print the wire result verbatim instead of the table")
	limit := fs.Int("limit", 0, "page size (default: the daemon's)")
	cursor := fs.Int("cursor", 0, "page BELOW this revision (newest-first paging)")
	want := addProtocolVersionFlag(fs)
	positional, rest := splitPositional(args, 1)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if len(positional) != 1 || fs.NArg() != 0 {
		return &usageError{"charter history names exactly one root — `aeon charter history ROOT [--limit N] [--cursor REV] [--json]`"}
	}
	p := protocol.CharterRevisionsParams{RootID: positional[0]}
	if *limit != 0 {
		p.Limit = limit
	}
	if *cursor != 0 {
		p.Cursor = cursor
	}
	c, err := charterDial(*want)
	if err != nil {
		return err
	}
	defer c.Close()
	if *asJSON {
		return callJSON(w, c, protocol.MethodCharterRevisions, p)
	}
	var res protocol.CharterRevisionsResult
	if err := c.Call(protocol.MethodCharterRevisions, p, &res); err != nil {
		return err
	}
	for _, v := range res.Revisions {
		note := ""
		if len(v.Body) > 0 {
			var bag struct {
				Note string `json:"note"`
			}
			if json.Unmarshal(v.Body, &bag) == nil && bag.Note != "" {
				note = " · " + bag.Note
			}
		}
		fmt.Fprintf(w, "rev %d · %s · %s/%s · %s%s\n", v.Revision, v.CreatedAt, v.ActorKind, v.ActorID, v.Source, note)
	}
	if res.NextCursor != "" {
		fmt.Fprintf(w, "more below revision %s — pass --cursor %s\n", res.NextCursor, res.NextCursor)
	}
	return nil
}

func charterVerbSave(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("charter save", flag.ContinueOnError)
	fs.SetOutput(w)
	file := fs.String("file", "", "the body JSON to file (a path; '-' is refused)")
	note := fs.String("note", "", "one line for the history: why this revision")
	asJSON := fs.Bool("json", false, "print the wire result verbatim instead of the table")
	want := addProtocolVersionFlag(fs)
	positional, rest := splitPositional(args, 1)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if len(positional) != 1 || fs.NArg() != 0 {
		return &usageError{"charter save names exactly one root — `aeon charter save ROOT --file PATH [--note TEXT] [--json]`"}
	}
	body, err := charterBodyFromPath(*file)
	if err != nil {
		return err
	}
	p := protocol.CharterSaveParams{RootID: positional[0], Body: body, Note: *note}
	c, err := charterDial(*want)
	if err != nil {
		return err
	}
	defer c.Close()
	if *asJSON {
		return callJSON(w, c, protocol.MethodCharterSave, p)
	}
	var view protocol.CharterView
	if err := c.Call(protocol.MethodCharterSave, p, &view); err != nil {
		return err
	}
	fmt.Fprintf(w, "filed: %s revision %d by %s/%s\n", view.RootID, view.Revision, view.ActorKind, view.ActorID)
	return nil
}

func charterVerbVocabulary(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("charter vocabulary", flag.ContinueOnError)
	fs.SetOutput(w)
	asJSON := fs.Bool("json", false, "print the wire result verbatim instead of the table")
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return &usageError{"charter vocabulary takes no positional arguments — `aeon charter vocabulary [--json]`"}
	}
	c, err := charterDial(*want)
	if err != nil {
		return err
	}
	defer c.Close()
	if *asJSON {
		return callJSON(w, c, protocol.MethodCharterVocabulary, struct{}{})
	}
	var res protocol.CharterVocabularyResult
	if err := c.Call(protocol.MethodCharterVocabulary, struct{}{}, &res); err != nil {
		return err
	}
	fmt.Fprintf(w, "check kinds: %v\n", res.CheckKinds)
	fmt.Fprintf(w, "caps: body %d B · goal %d · item %d · list %d · acceptance %d · tags %d (tag %d) · verify %d (%d B) · env-ref %d · note %d\n",
		res.Caps.BodyBytes, res.Caps.Goal, res.Caps.Item, res.Caps.List, res.Caps.Acceptance,
		res.Caps.Tags, res.Caps.Tag, res.Caps.Verify, res.Caps.VerifyBytes, res.Caps.EnvRef, res.Caps.Note)
	fmt.Fprintf(w, "autonomy verbs: %v", res.Autonomy)
	if res.AutonomyNote != "" {
		fmt.Fprintf(w, " — %s", res.AutonomyNote)
	}
	fmt.Fprintln(w)
	return nil
}

// templateBodyFromPath lives beside charterBodyFromPath because template
// save obeys the same law (S61 T10): a path, never stdin; parseable JSON,
// never re-validated client-side.
func templateBodyFromPath(path string) (json.RawMessage, error) {
	return charterBodyFromPath(path)
}
