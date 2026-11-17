package main

// automation_flags.go — the argv plumbing every automation verb shares
// (S49 T8, split from automation_cmd.go under SIZE1): the flag gatherer
// that lets flags appear anywhere a person puts them, the boolean names it
// must know so `--enabled --yes` is not read as a value, the SOURCE:ID
// split and its unknown-SOURCE refusal (a CLI-only refusal: a SOURCE that
// cannot be a value is not a judgment about a value), the argv reads the
// seatbelt gates use BEFORE anything is parsed, and the one JSON/text
// output helper that prints the door's typed answer identically whichever
// door answered it.

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"
)

// --- flag and name plumbing -------------------------------------------------------------

// automationParseFlags splits flags from positionals exactly as the
// blueprint plane does (one mechanism, reused): flags anywhere, `--` ends
// them.
// automationBoolFlags names this plane's boolean flags. The blueprint
// plane's shared gatherer swallows the next token for any flag it does not
// know is boolean, and `--enabled --yes` is THIS plane's documented
// spelling — so the bool names are pinned here and every bare boolean is
// rewritten to its =true form before the shared gatherer runs. (A person
// who wants a boolean OFF simply omits it; `--enabled false` is not a
// spelling, same as the blueprint plane's `--yes false`.)
var automationBoolFlags = map[string]bool{"json": true, "yes": true, "enabled": true, "clear-window": true}

func automationParseFlags(fs *flag.FlagSet, args []string) ([]string, error) {
	fixed := make([]string, 0, len(args))
	for i, a := range args {
		if a == "--" {
			fixed = append(fixed, args[i:]...)
			break
		}
		if len(a) >= 2 && a[0] == '-' {
			name, _, has := strings.Cut(strings.TrimLeft(a, "-"), "=")
			if automationBoolFlags[name] && !has {
				fixed = append(fixed, a+"=true")
				continue
			}
		}
		fixed = append(fixed, a) // positional or value token: untouched — the gatherer routes it
	}
	return blueprintParseFlags(fs, fixed)
}

func automationJSONFlagOnly(fs *flag.FlagSet) *bool {
	return fs.Bool("json", false, "print the answer as the door's own JSON")
}

func automationJSONFlag(w io.Writer, name string, args []string) (bool, []string, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(w)
	asJSON := automationJSONFlagOnly(fs)
	rest, err := automationParseFlags(fs, args)
	return *asJSON, rest, err
}

// parseAutomationSourceID is the CLI's ONE honest parse: the SOURCE is part
// of the name (AN1) and an unknown SOURCE cannot be a value — everything
// about whether the row EXISTS and what its id shape means is the daemon's.
func parseAutomationSourceID(s string) (source, id string, err error) {
	i := strings.Index(s, ":")
	if i <= 0 || i == len(s)-1 {
		return "", "", fmt.Errorf("expected SOURCE:ID — schedule:nightly, webhook:ci, event:approved-features (the source is part of the name)")
	}
	source, id = s[:i], s[i+1:]
	if err := automationCheckSource(source); err != nil {
		return "", "", err
	}
	return source, id, nil
}

func automationCheckSource(source string) error {
	switch source {
	case "schedule", "webhook", "event":
		return nil
	}
	return fmt.Errorf("unknown SOURCE %q — the three are schedule, webhook, event", source)
}

// automationWantsDraft reports whether `preview` was given a --cron (the
// store-free draft road is chosen BEFORE dialling, so no fallback line can
// misdescribe a walk that opens no store).
func automationWantsDraft(args []string) bool {
	for _, a := range args {
		if a == "--cron" || strings.HasPrefix(a, "--cron=") {
			return true
		}
	}
	return false
}

// automationHasYes / automationHasFlag read the raw argv for the seatbelt
// arms BEFORE full parsing (the gate must fire even when the flags the verb
// would otherwise take are absent — a bare `enable event:x` is exactly the
// typo the seatbelt is for).
func automationHasYes(args []string) bool { return automationHasFlag(args, "--yes") || automationHasFlag(args, "-yes") }

func automationHasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag || strings.HasPrefix(a, flag+"=") {
			return true
		}
	}
	return false
}

// automationMatchGrammarHint is the pointer, not a re-validation: the
// grammar's own words are one --match away and printed by the daemon for
// anything parseable.
func automationMatchGrammarHint() string {
	return "the rule is a JSON object over the vocabulary's keys — see aeon automation vocabulary"
}

func automationOutput(w io.Writer, asJSON bool, res any, text func(io.Writer, any) error) error {
	if asJSON {
		// The door's own typed answer — the same bytes whichever door
		// answered (the result types are the wire types).
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(res)
	}
	return text(w, res)
}
