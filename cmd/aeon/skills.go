package main

// skills.go — `aeon skills list|get|validate|install|remove` (S65 T6, P34).
//
// Every verb is a pure CLIENT of the socket's five skills methods — the
// settings.go shape followed exactly: dial(0), c.Call the identical method
// the app calls, print. The CLI opens no catalog of its own (a
// store-opening or disk-walking reader would be the second door the slice
// forbids) and prints ONLY what the wire said: the status the validator
// gave, the issues it listed, the roots it named, the truncated flag it
// set. Three-state honesty (P31) survives to the terminal because this
// file refuses to render absence as a confident answer — a skill that
// never parsed says so, and an empty listing says "both roots are empty",
// not "nothing to see".
//
// The wire structs are the protocol's own params types and internal/skills'
// result types — the same shapes the daemon encodes (the fixture round-trip
// in internal/protocol IS the shape contract), so there is no local mirror
// to drift and no re-derivation to distrust.

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/skills"
)

const (
	skillsMethodList     = protocol.MethodSkillsList
	skillsMethodGet      = protocol.MethodSkillsGet
	skillsMethodValidate = protocol.MethodSkillsValidate
	skillsMethodInstall  = protocol.MethodSkillsInstall
	skillsMethodRemove   = protocol.MethodSkillsRemove
)

func cmdSkills(w io.Writer, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf(`skills needs a verb: list, get, validate, install, remove — try "aeon skills help"`)
	}
	switch args[0] {
	case "help":
		fmt.Fprint(w, skillsUsage)
		return nil
	case "list":
		return skillsList(w, args[1:])
	case "get":
		return skillsGet(w, args[1:])
	case "validate":
		return skillsValidate(w, args[1:])
	case "install":
		return skillsInstall(w, args[1:])
	case "remove":
		return skillsRemove(w, args[1:])
	default:
		return fmt.Errorf("skills verb %q unknown — the verbs are list, get, validate, install, remove", args[0])
	}
}

const skillsUsage = `aeon skills — the Agent Skills catalog, asked of the daemon (S65)

  aeon skills list       [--status valid|warn|invalid] [--source user|managed] [--query TEXT] [--limit N] [--json]
  aeon skills get        NAME [--json]                    one skill in full: frontmatter, issues, body
  aeon skills validate   [NAME] [--json]                  re-read the disk and re-judge (the bench's Validate)
  aeon skills install    SOURCE [--ref REF] [--subpath P] [--force] [--json]
  aeon skills remove     NAME [--json]                    delete a MANAGED skill (the user root is never touched)
  aeon skills help

A pure client of the socket's five skills.* methods — the same door the app
uses (P34); the CLI reads and installs nothing itself. SOURCE is a local
folder or a GitHub locator (owner/repo, owner/repo/sub@ref, a full
github.com URL); install fetches it, validates BEFORE placement, and lands
it in the managed folder named by its frontmatter. Nothing in a skill is
ever executed — scripts are listed, never run. --json prints the raw result
object, one JSON document, so scripts depend on the wire shape and not on
this printer.`

// skillsDialAndCall is the ONE dial-and-call helper (the settings.go shape,
// under its own name).
func skillsDialAndCall(method protocol.MethodName, params, out any) error {
	c, err := dial(0)
	if err != nil {
		return err
	}
	defer c.Close()
	return c.Call(method, params, out)
}

// skillsSeparate splits argv into positionals and flag arguments before
// flag.Parse sees them. The standard library stops parsing at the first
// positional, which would strand `skills install <src> --force` and
// `skills get <name> --json` — the orders a person TYPES (the flag trails
// the thing it modifies). The value-taking flags here are exactly two
// (ref, subpath, path) and all are space-separated, so the walk is
// complete:
// a bool flag never swallows the next token.
func skillsSeparate(args []string) (pos, flags []string, err error) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			pos = append(pos, args[i+1:]...)
			return pos, flags, nil
		case strings.HasPrefix(a, "-") && a != "-":
			flags = append(flags, a)
			name := strings.TrimLeft(strings.SplitN(a, "=", 2)[0], "-")
			if !strings.Contains(a, "=") && (name == "ref" || name == "subpath" || name == "path") {
				if i+1 >= len(args) {
					return nil, nil, fmt.Errorf("--%s needs a value", name)
				}
				i++
				flags = append(flags, args[i])
			}
		default:
			pos = append(pos, a)
		}
	}
	return pos, flags, nil
}

func skillsPrintJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// --- the five verbs ---

func skillsList(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("skills list", flag.ContinueOnError)
	status := fs.String("status", "", "valid, warn or invalid")
	source := fs.String("source", "", "user or managed")
	query := fs.String("query", "", "substring of a name or description")
	limit := fs.Int("limit", 0, "at most N rows")
	jsonFlag := fs.Bool("json", false, "print the raw result object as one JSON document")
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	var res skills.ScanResult
	if err := skillsDialAndCall(skillsMethodList, protocol.SkillsListParams{
		Status: *status, Source: *source, Query: *query, Limit: *limit,
	}, &res); err != nil {
		return err
	}
	if *jsonFlag {
		return skillsPrintJSON(w, res)
	}
	return renderSkillsList(w, &res)
}

// renderSkillsList prints a scan's answer: the counts, both roots named,
// then one row per skill with its issues beneath. Split from the verb so
// the canned-struct tests read the printer directly (the settings_test.go
// lane — the socket is stood in, the behaviour asserted).
func renderSkillsList(w io.Writer, res *skills.ScanResult) error {
	fmt.Fprintf(w, "%d skill(s): %d valid, %d with warnings, %d invalid, %d shadowed\n",
		len(res.Skills), res.Summary.Valid, res.Summary.Warn, res.Summary.Invalid, res.Summary.Shadowed)
	fmt.Fprintf(w, "user    %s\nmanaged %s\n", res.UserRoot, res.ManagedRoot)
	// S66: the watcher's honesty belongs in the terminal too — a listing
	// that does not say whether the disk can still surprise it is the two-
	// state answer P31 forbids. Verbatim daemon words, never a paraphrase.
	if res.Watching {
		if res.WatchError != "" {
			fmt.Fprintf(w, "watch   on — partly blind: %s\n\n", res.WatchError)
		} else {
			fmt.Fprintf(w, "watch   on — folders dropped into either root are noticed without a restart\n\n")
		}
	} else {
		fmt.Fprintf(w, "watch   off — %s\n\n", res.WatchError)
	}
	if len(res.Skills) == 0 {
		fmt.Fprintln(w, "  (no skills matched — both roots as filtered above are empty; the roots being absent is reported above, not hidden)")
		return nil
	}
	for _, s := range res.Skills {
		marker := "ok  "
		switch s.Status {
		case skills.StatusWarn:
			marker = "warn"
		case skills.StatusInvalid:
			marker = "FAIL"
		}
		src := "user"
		if s.Source == skills.SourceManaged {
			src = "mgmt"
		}
		fmt.Fprintf(w, "  %s  %-26s %s  %s\n", marker, s.Name, src, firstLine(s.Description))
		if s.Shadowed {
			fmt.Fprintf(w, "        shadowed by %s (the other root's copy of this name wins)\n", s.ShadowedBy)
		}
		for _, iss := range s.Issues {
			indent := "warning"
			if iss.Severity == skills.SeverityError {
				indent = "  error"
			}
			fmt.Fprintf(w, "        %s %s: %s\n", indent, iss.Field, iss.Detail)
		}
	}
	return nil
}

func skillsGet(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("skills get", flag.ContinueOnError)
	jsonFlag := fs.Bool("json", false, "print the raw result object as one JSON document")
	pathFlag := fs.String("path", "", "read a skill BY DISK PATH — the only address a shadowed copy has")
	addProtocolVersionFlag(fs)
	pos, flagArgs, err := skillsSeparate(args)
	if err != nil {
		return err
	}
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	if *pathFlag != "" {
		if len(pos) != 0 {
			return fmt.Errorf("skills get takes a name OR --path, never both — the path already names the copy")
		}
		var res skills.GetResult
		if err := skillsDialAndCall(skillsMethodGet, protocol.SkillsGetParams{Path: *pathFlag}, &res); err != nil {
			return err
		}
		if *jsonFlag {
			return skillsPrintJSON(w, res)
		}
		return renderSkillGet(w, &res)
	}
	if len(pos) != 1 {
		return fmt.Errorf("skills get needs exactly one name (or --path to a skill folder) — `aeon skills list` shows what exists")
	}
	var res skills.GetResult
	if err := skillsDialAndCall(skillsMethodGet, protocol.SkillsGetParams{Name: pos[0]}, &res); err != nil {
		return err
	}
	if *jsonFlag {
		return skillsPrintJSON(w, res)
	}
	return renderSkillGet(w, &res)
}

func renderSkillGet(w io.Writer, res *skills.GetResult) error {
	s := res.Skill
	fmt.Fprintf(w, "%s  [%s]  %s\n", s.Name, s.Status, s.Path)
	if s.Description != "" {
		fmt.Fprintf(w, "description     %s\n", s.Description)
	}
	if s.License != "" {
		fmt.Fprintf(w, "license         %s\n", s.License)
	}
	if s.Compatibility != "" {
		fmt.Fprintf(w, "compatibility   %s\n", s.Compatibility)
	}
	if len(s.Metadata) > 0 {
		for _, k := range sortedStringKeys(s.Metadata) {
			fmt.Fprintf(w, "metadata        %s = %s\n", k, s.Metadata[k])
		}
	}
	if len(s.Unknown) > 0 {
		for _, k := range sortedStringKeys(s.Unknown) {
			fmt.Fprintf(w, "unknown field   %s = %s (kept and ignored — a newer agent's fields do not invalidate a skill)\n", k, s.Unknown[k])
		}
	}
	fmt.Fprintf(w, "files           scripts=%d references=%d assets=%d other=%d\n",
		s.Files.Scripts, s.Files.References, s.Files.Assets, s.Files.Other)
	for _, iss := range s.Issues {
		fmt.Fprintf(w, "  [%s] %s: %s\n", iss.Severity, iss.Field, iss.Detail)
	}
	fmt.Fprintf(w, "\n--- SKILL.md (%d bytes, %d lines%s) ---\n", s.BodyBytes, s.BodyLines, map[bool]string{true: ", truncated by the daemon's cap", false: ""}[res.Truncated])
	fmt.Fprintln(w, res.Body)
	if res.Truncated {
		fmt.Fprintln(w, "… the body continues past what the wire carried; the file on disk is complete")
	}
	return nil
}

func skillsValidate(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("skills validate", flag.ContinueOnError)
	jsonFlag := fs.Bool("json", false, "print the raw result object as one JSON document")
	addProtocolVersionFlag(fs)
	pos, flagArgs, err := skillsSeparate(args)
	if err != nil {
		return err
	}
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	name := ""
	if len(pos) == 1 {
		name = pos[0]
	} else if len(pos) > 1 {
		return fmt.Errorf("skills validate takes no name or one name, not %d", len(pos))
	}
	var res protocol.SkillsValidateResult
	if err := skillsDialAndCall(skillsMethodValidate, protocol.SkillsValidateParams{Name: name}, &res); err != nil {
		return err
	}
	if *jsonFlag {
		return skillsPrintJSON(w, res)
	}
	return renderValidate(w, name, &res)
}

func renderValidate(w io.Writer, name string, res *protocol.SkillsValidateResult) error {
	if name == "" {
		fmt.Fprintf(w, "re-read the disk at %s: %d valid, %d with warnings, %d invalid, %d shadowed\n",
			res.ScannedAt, res.Summary.Valid, res.Summary.Warn, res.Summary.Invalid, res.Summary.Shadowed)
	}
	for _, s := range res.Skills {
		if s.Status == skills.StatusValid && len(s.Issues) == 0 {
			fmt.Fprintf(w, "  ok    %s\n", s.Name)
			continue
		}
		fmt.Fprintf(w, "  %s  %s\n", strings.ToUpper(string(s.Status)), s.Name)
		for _, iss := range s.Issues {
			fmt.Fprintf(w, "        [%s] %s: %s\n", iss.Severity, iss.Field, iss.Detail)
		}
	}
	if len(res.Skills) == 0 {
		fmt.Fprintln(w, "  (the catalog holds nothing; nothing to validate is reported, not hidden)")
	}
	return nil
}

func skillsInstall(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("skills install", flag.ContinueOnError)
	ref := fs.String("ref", "", "branch or tag for a GitHub source")
	subpath := fs.String("subpath", "", "the folder inside the source that holds SKILL.md")
	force := fs.Bool("force", false, "replace an existing managed skill of this name")
	jsonFlag := fs.Bool("json", false, "print the raw result object as one JSON document")
	addProtocolVersionFlag(fs)
	pos, flagArgs, err := skillsSeparate(args)
	if err != nil {
		return err
	}
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	if len(pos) != 1 {
		return fmt.Errorf(`skills install needs one source — a local folder or a GitHub locator ("aeon skills install help")`)
	}
	var res skills.InstallResult
	if err := skillsDialAndCall(skillsMethodInstall, protocol.SkillsInstallParams{
		Source: pos[0], Ref: *ref, Subpath: *subpath, Force: *force,
	}, &res); err != nil {
		return err
	}
	if *jsonFlag {
		return skillsPrintJSON(w, res)
	}
	return renderInstall(w, &res)
}

func renderInstall(w io.Writer, res *skills.InstallResult) error {
	replaced := ""
	if res.Replaced {
		replaced = " (replaced the existing managed copy)"
	}
	fmt.Fprintf(w, "installed %s [%s] at %s%s\n", res.Skill.Name, res.Skill.Status, res.Skill.Path, replaced)
	for _, iss := range res.Skill.Issues {
		fmt.Fprintf(w, "  [%s] %s: %s\n", iss.Severity, iss.Field, iss.Detail)
	}
	fmt.Fprintln(w, "it is listed on `aeon skills list` now; nothing in it was executed")
	return nil
}

func skillsRemove(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("skills remove", flag.ContinueOnError)
	jsonFlag := fs.Bool("json", false, "print the raw result object as one JSON document")
	addProtocolVersionFlag(fs)
	pos, flagArgs, err := skillsSeparate(args)
	if err != nil {
		return err
	}
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	if len(pos) != 1 {
		return fmt.Errorf("skills remove needs exactly one name")
	}
	var res skills.RemoveResult
	if err := skillsDialAndCall(skillsMethodRemove, protocol.SkillsRemoveParams{Name: pos[0]}, &res); err != nil {
		return err
	}
	if *jsonFlag {
		return skillsPrintJSON(w, res)
	}
	return renderRemove(w, &res)
}

func renderRemove(w io.Writer, res *skills.RemoveResult) error {
	fmt.Fprintf(w, "removed %s (%s)\n", res.Name, res.Path)
	if res.Revealed {
		fmt.Fprintln(w, "a same-named skill in the user root was shadowed by this copy — it is listed again now")
	}
	return nil
}

// --- printer helpers (printers only; the decisions were the daemon's) ---

// firstLine lives in step_run.go (the same printer rule, one definition);
// sortedStringKeys is this file's map[string]string twin of client_verbs.go's
// sortedKeys — Go has no generics-free way to share them and the types
// differ.
func sortedStringKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sortStrings(out)
	return out
}
