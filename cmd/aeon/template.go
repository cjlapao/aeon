package main

// cmd/aeon/template.go — S61 T10: the scaffold templates as a pure
// client, same law as charter.go above (dial, call, print; no offline
// door; no actor flag; no secret flags; body from a FILE, stdin refused).

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"

	"github.com/cjlapao/aeon/internal/protocol"
)

// templateVerbs names each verb and the socket method it drives.
var templateVerbs = []cliVerb{
	{"list", protocol.MethodTemplateList, false},
	{"show", protocol.MethodTemplateGet, false},
	{"save", protocol.MethodTemplateSave, false},
}

// templateDial is the dial every verb shares; a var for the tests.
var templateDial = dial

func cmdTemplate(w io.Writer, args []string) error {
	if len(args) == 0 {
		printTemplateUsage(w)
		return nil
	}
	verb, rest := args[0], args[1:]
	switch verb {
	case "list":
		return templateVerbList(w, rest)
	case "show":
		return templateVerbShow(w, rest)
	case "save":
		return templateVerbSave(w, rest)
	case "help":
		printTemplateUsage(w)
		return nil
	default:
		printTemplateUsage(w)
		return &usageError{msg: fmt.Sprintf("unknown `template` subcommand %q — `aeon template help` lists the three", verb)}
	}
}

func printTemplateUsage(w io.Writer) {
	fmt.Fprint(w, `usage: aeon template <verb> — the scaffold templates a plan can pick (S61)

  list  [--stack TAG] [--origin system|user] [--json] — the catalogue
  show  SLUG [--files] [--json] — one template whole
  save  --file PATH — file a user template (the body JSON carries the
        slug; '-' (stdin) is refused, as for charters)

  Every verb is a client of the daemon's socket — there is no offline door
  (the recorded P5 deviation). Seeded templates are the system's; a person
  saving a slug that is the system's is the daemon's sentence to answer.
`)
}

func templateVerbList(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("template list", flag.ContinueOnError)
	fs.SetOutput(w)
	stack := fs.String("stack", "", "only templates whose stack tags include this one")
	origin := fs.String("origin", "", "system or user (default: both)")
	asJSON := fs.Bool("json", false, "print the wire result verbatim instead of the table")
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return &usageError{"template list takes no positional arguments — `aeon template list [--stack TAG] [--origin system|user] [--json]`"}
	}
	p := protocol.TemplateListParams{StackTag: *stack, Origin: *origin}
	c, err := templateDial(*want)
	if err != nil {
		return err
	}
	defer c.Close()
	if *asJSON {
		return callJSON(w, c, protocol.MethodTemplateList, p)
	}
	var res protocol.TemplateListResult
	if err := c.Call(protocol.MethodTemplateList, p, &res); err != nil {
		return err
	}
	if len(res.Templates) == 0 {
		fmt.Fprint(w, "no templates answer that filter\n")
		return nil
	}
	for _, t := range res.Templates {
		line := fmt.Sprintf("%s · v%d · %s · %s · %d files", t.Slug, t.Version, t.Origin, t.Title, t.FileCount)
		if len(t.StackTags) > 0 {
			line += " · " + fmt.Sprint(t.StackTags)
		}
		if t.Diverged {
			line += " · diverged from seed"
		}
		fmt.Fprintln(w, line)
	}
	return nil
}

func templateVerbShow(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("template show", flag.ContinueOnError)
	fs.SetOutput(w)
	files := fs.Bool("files", false, "also print each file's path and content")
	asJSON := fs.Bool("json", false, "print the wire result verbatim instead of the table")
	want := addProtocolVersionFlag(fs)
	positional, rest := splitPositional(args, 1)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if len(positional) != 1 || fs.NArg() != 0 {
		return &usageError{"template show names exactly one slug — `aeon template show SLUG [--files] [--json]`"}
	}
	p := protocol.TemplateGetParams{Slug: positional[0]}
	c, err := templateDial(*want)
	if err != nil {
		return err
	}
	defer c.Close()
	if *asJSON {
		return callJSON(w, c, protocol.MethodTemplateGet, p)
	}
	var view protocol.TemplateView
	if err := c.Call(protocol.MethodTemplateGet, p, &view); err != nil {
		return err
	}
	fmt.Fprintf(w, "%s · v%d · %s · %s\n", view.Slug, view.Version, view.Origin, view.Title)
	if view.Description != "" {
		fmt.Fprintf(w, "%s\n", view.Description)
	}
	if len(view.StackTags) > 0 {
		fmt.Fprintf(w, "stack: %v\n", view.StackTags)
	}
	if !*files {
		fmt.Fprintf(w, "%d files — --files prints their paths and content\n", len(view.Files))
		return nil
	}
	for _, f := range view.Files {
		fmt.Fprintf(w, "--- %s\n%s\n", f.Path, f.Content)
	}
	return nil
}

// templateSaveBody is the file's JSON shape — exactly the wire params
// (the CLI adds no fields and checks none beyond parseability; the
// daemon's row laws word every refusal).
type templateSaveBody struct {
	Slug        string                      `json:"slug"`
	Title       string                      `json:"title"`
	Description string                      `json:"description,omitempty"`
	StackTags   []string                    `json:"stackTags,omitempty"`
	Files       []protocol.TemplateFileWire `json:"files"`
	Note        string                      `json:"note,omitempty"`
}

func templateVerbSave(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("template save", flag.ContinueOnError)
	fs.SetOutput(w)
	file := fs.String("file", "", "the template JSON to file (a path; '-' is refused)")
	note := fs.String("note", "", "one line for the record")
	asJSON := fs.Bool("json", false, "print the wire result verbatim instead of the table")
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return &usageError{"template save reads --file PATH — the slug lives in the body"}
	}
	raw, errBody := templateBodyFromPath(*file)
	if errBody != nil {
		return errBody
	}
	var body templateSaveBody
	if err := json.Unmarshal(raw, &body); err != nil {
		return &usageError{msg: fmt.Sprintf("the body file %q does not carry a template object (slug, title, files): %v", *file, err)}
	}
	p := protocol.TemplateSaveParams{
		Slug: body.Slug, Title: body.Title, Description: body.Description,
		StackTags: body.StackTags, Files: body.Files,
	}
	if *note != "" {
		p.Note = *note // the flag's word wins over a note inside the body
	} else {
		p.Note = body.Note
	}
	c, err := templateDial(*want)
	if err != nil {
		return err
	}
	defer c.Close()
	if *asJSON {
		return callJSON(w, c, protocol.MethodTemplateSave, p)
	}
	var view protocol.TemplateView
	if err := c.Call(protocol.MethodTemplateSave, p, &view); err != nil {
		return err
	}
	fmt.Fprintf(w, "saved: %s v%d (%s) — %d files\n", view.Slug, view.Version, view.Origin, len(view.Files))
	return nil
}
