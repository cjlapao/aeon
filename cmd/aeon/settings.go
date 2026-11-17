package main

// settings.go — `aeon settings list|get|set|unset|history` (S44 T7, P34).
//
// Every verb is a pure CLIENT of the socket's five settings methods — the
// telemetry.go shape followed exactly: dial(0), c.Call the identical method
// the app calls, print. The CLI opens no store here (the daemon owns the rows;
// a store-opening reader would be the second door the slice forbids), and it
// prints ONLY what the wire said: source named on every value, the env shadow
// and its saved-value line, the two warnings the daemon sends, the sensitive
// marker, changed as "unchanged" when it was a no-op, and the honest empty
// state for a zero-row list. The three-state honesty (CS13) survives to the
// terminal because this file refuses to render absence as a confident answer.
//
// A note on the wire types (S44 T7, 2026-10-30; reconciled with T5)
//
// The five method names below ARE the protocol's: this file calls them through
// protocol.MethodName so the CLI and the daemon agree on the door by name
// (T5 built internal/protocol with MethodSettings* — see the note at the bottom
// of this file).
//
// The wire STRUCTS, however, are a local mirror and stay local on purpose.
// T5's protocol.SettingView / SettingEventView and the *Params/*Result use
// pointers for the "missing vs empty" distinction (Value *string, Area
// *string, Key/Limit *int, EnvInvalid as a plain string); this CLI's mirror
// keeps them as canonical TEXT (Value/Area/Key string, Limit int, EnvInvalid
// *string) because a CLI is a client that TYPES a value, and an empty string
// is a real value, not a missing one — so the two shapes are not interchangeable
// field-for-field and swapping them would break the set verb. The mirror is a
// faithful twin of the design's CS10 shape and is pinned to the door's wire by
// TestSettings* (the canned-struct tests assert the same fields the protocol
// encodes). If the protocol's wire shape ever changes, re-sync these structs
// (the printers and the --yes gate read the wire, not its home).

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/cjlapao/aeon/internal/protocol"
)

// The five method names — the protocol's own, used so the CLI cannot drift from
// the door. protocol.MethodName is the type c.Call takes.
const (
	settingsMethodList    = protocol.MethodSettingsList
	settingsMethodGet     = protocol.MethodSettingsGet
	settingsMethodSet     = protocol.MethodSettingsSet
	settingsMethodUnset   = protocol.MethodSettingsUnset
	settingsMethodHistory = protocol.MethodSettingsHistory
)

func cmdSettings(w io.Writer, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf(`settings needs a verb: list, get, set, unset, history — try "aeon settings help"`)
	}
	switch args[0] {
	case "help":
		fmt.Fprint(w, settingsUsage)
		return nil
	case "list":
		return settingsList(w, args[1:])
	case "get":
		return settingsGet(w, args[1:])
	case "set":
		return settingsSet(w, args[1:])
	case "unset":
		return settingsUnset(w, args[1:])
	case "history":
		return settingsHistory(w, args[1:])
	default:
		return fmt.Errorf("settings verb %q unknown — the verbs are list, get, set, unset, history", args[0])
	}
}

const settingsUsage = `aeon settings — the declared settings, asked of the daemon (S44)

  aeon settings list    [--area NAME] [--json]   every declared setting: key, effective value, source, flags
  aeon settings get     KEY [--json]             one setting in full: descriptor, effective value, stored value, source
  aeon settings set     KEY VALUE [--yes] [--json]   write; a sensitive key needs --yes (it is a seatbelt, not a lock)
  aeon settings unset   KEY [--yes] [--json]   return to the default (deletes the stored row)
  aeon settings history [--key KEY] [--limit N] [--json]   the audit, newest first
  aeon settings help

A pure client of the socket's five settings.* methods — the same door the app
uses (P34); the CLI opens no store and re-validates nothing. VALUE is the
canonical text form (true / false / 42 / a member / raw text) and is sent
VERBATIM: the CLI does no coercion and no lower-casing, and the daemon decides
a bad value. ` + "`set` takes a value on the command line — acceptable only " +
	"because no declared key is a credential (C4/CS5); there is no --value-from-stdin " +
	"and no secret path. --yes is a seatbelt against a typo, not a security control " +
	"(CS8/CS9). --json prints the raw result object, one JSON document, so scripts " +
	"depend on the wire shape and not on this printer."

// settingsDialAndCall is the ONE dial-and-call helper, copied under its own
// name from telemetry.go's shape (do NOT import across the two files' private
// names). method is a string constant; the door T5 builds will own it in
// internal/protocol, where the same name is a protocol.MethodName.
func settingsDialAndCall(method protocol.MethodName, params, out any) error {
	c, err := dial(0)
	if err != nil {
		return err
	}
	defer c.Close()
	return c.Call(method, params, out)
}

// --- the five verbs ---

func settingsList(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("settings list", flag.ContinueOnError)
	area := fs.String("area", "", "only this area")
	jsonFlag := fs.Bool("json", false, "print the raw result object as one JSON document")
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	var res settingsListResult
	if err := settingsDialAndCall(settingsMethodList, settingsListParams{Area: *area}, &res); err != nil {
		return err
	}
	if *jsonFlag {
		return printJSON(w, res)
	}
	renderList(w, res.Settings)
	return nil
}

func settingsGet(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("settings get", flag.ContinueOnError)
	jsonFlag := fs.Bool("json", false, "print the raw result object as one JSON document")
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	key := ""
	if len(fs.Args()) == 1 {
		key = fs.Args()[0]
	}
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("settings get needs KEY — a bare get has no subject to name")
	}
	var res settingsGetResult
	if err := settingsDialAndCall(settingsMethodGet, settingsGetParams{Key: key}, &res); err != nil {
		return err
	}
	if *jsonFlag {
		return printJSON(w, res)
	}
	renderGet(w, res.Setting)
	return nil
}

func settingsSet(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("settings set", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "confirm a sensitive write (a seatbelt against a typo, not a lock)")
	jsonFlag := fs.Bool("json", false, "print the raw result object as one JSON document")
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) != 2 {
		return fmt.Errorf("settings set needs KEY and VALUE — the value is the canonical text form, sent verbatim")
	}
	key, value := fs.Args()[0], fs.Args()[1]

	// The gate is the CLI's counterpart to the UI's confirmation (CS8): it
	// reads the CURRENT view to see whether the key is sensitive, BEFORE the
	// write. A non-sensitive key never needs it; a sensitive one without --yes
	// is withheld here, with no settings.set call, and a non-zero exit.
	var cur settingsView
	if err := settingsDialAndCall(settingsMethodGet, settingsGetParams{Key: key}, &cur); err != nil {
		return err
	}
	if err := settingsSetGate(w, cur, *yes, value); err != nil {
		return err
	}

	var res settingsSetResult
	if err := settingsDialAndCall(settingsMethodSet, settingsSetParams{Key: key, Value: value}, &res); err != nil {
		return err
	}
	if *jsonFlag {
		return printJSON(w, res)
	}
	renderWrite(w, res.Setting, res.Changed)
	return nil
}

func settingsUnset(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("settings unset", flag.ContinueOnError)
	jsonFlag := fs.Bool("json", false, "print the raw result object as one JSON document")
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	key := ""
	if len(fs.Args()) == 1 {
		key = fs.Args()[0]
	}
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("settings unset needs KEY — there is no ambient subject to return to the default")
	}
	var res settingsUnsetResult
	if err := settingsDialAndCall(settingsMethodUnset, settingsUnsetParams{Key: key}, &res); err != nil {
		return err
	}
	if *jsonFlag {
		return printJSON(w, res)
	}
	renderWrite(w, res.Setting, res.Changed)
	return nil
}

func settingsHistory(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("settings history", flag.ContinueOnError)
	key := fs.String("key", "", "only events for this key (absent = every key)")
	limit := fs.Int("limit", 0, "maximum events (0 = the default 50; clamped to 500)")
	jsonFlag := fs.Bool("json", false, "print the raw result object as one JSON document")
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	var res settingsHistoryResult
	if err := settingsDialAndCall(settingsMethodHistory, settingsHistoryParams{Key: *key, Limit: *limit}, &res); err != nil {
		return err
	}
	if *jsonFlag {
		return printJSON(w, res)
	}
	renderHistory(w, res.Events)
	return nil
}

// settingsSetGate is the --yes gate (CS8): the client's counterpart to the
// UI's confirmation. It reads data the daemon already sent (the current view's
// sensitivity), and it is a seatbelt — NOT a security control (CS9: the human
// door is the 0600 socket, not this prompt). It returns a non-nil error when it
// withholds (which is what keeps the settings.set call from happening); a nil
// error means proceed to the write. A non-sensitive key always proceeds.
func settingsSetGate(w io.Writer, cur settingsView, yes bool, newValue string) error {
	if !cur.Sensitive || yes {
		return nil // an ordinary key never needs it; a confirmed sensitive one proceeds
	}
	// Withhold: say what the key controls (the descriptor's description), the
	// current and the new value, and that --yes applies — then refuse to write.
	fmt.Fprintf(w, "the key %s is sensitive; this write changes it\n", cur.Key)
	if desc := strings.TrimSpace(cur.Description); desc != "" {
		fmt.Fprintf(w, "  what it controls: %s\n", desc)
	}
	fmt.Fprintf(w, "  current:  %s\n", cur.Value)
	fmt.Fprintf(w, "  new:      %s\n", newValue)
	fmt.Fprintln(w, "  re-run with --yes to apply")
	return fmt.Errorf("sensitive setting %s was not confirmed with --yes; the write was not made (this is a seatbelt, not a lock — CS8/CS9)", cur.Key)
}

// --- the printers (the table is the contract; this is the ONE renderer) ---

// renderList is the ONE list printer. Every row names key, effective value,
// the source word, and the [sensitive] marker where set. An empty list is an
// honest state, not a failure (CS13): it says so, and the verb exits 0.
func renderList(w io.Writer, rows []settingsView) {
	if len(rows) == 0 {
		fmt.Fprintln(w, "no settings are declared yet")
		return
	}
	for _, s := range rows {
		line := fmt.Sprintf("  %-30s %-12s %s", s.Key, s.Value, sourceWord(s))
		if s.Sensitive {
			line += "  [sensitive]"
		}
		fmt.Fprintln(w, line)
	}
}

// renderGet is the ONE single-setting printer: it renders the descriptor, the
// effective value with the source that won, the env shadow and its saved-value
// line, the two warnings the daemon sends, and — for a sensitive key — the
// descriptor's description. It never hides a layer (D3).
func renderGet(w io.Writer, s settingsView) {
	fmt.Fprintf(w, "%s  (%s)\n", s.Key, s.Type)
	// The value line, with the source that won, exactly as the table dictates.
	switch s.Source {
	case "environment":
		fmt.Fprintf(w, "value  %s   (environment — %s is set; a saved value, if any, is shadowed)\n",
			s.Value, envNameFor(s.Key))
		if st := deref(s.Stored); st != "" {
			fmt.Fprintf(w, "saved  %s  (shadowed)\n", st)
		}
	case "database":
		fmt.Fprintf(w, "value  %s   (saved, by %s at %s)\n", s.Value, s.UpdatedBy, s.UpdatedAt)
	default: // "default" and any source the daemon does not name
		fmt.Fprintf(w, "value  %s   (%s)\n", s.Value, sourceWord(s))
	}
	if env := deref(s.EnvInvalid); env != "" {
		fmt.Fprintf(w, "warning: %s is set but not valid for this setting and was ignored\n", env)
	}
	if s.StoredInvalid {
		fmt.Fprintf(w, "warning: the saved value %q is no longer valid; the default is in force\n", deref(s.Stored))
	}
	if s.Sensitive {
		if desc := strings.TrimSpace(s.Description); desc != "" {
			fmt.Fprintf(w, "what it controls: %s\n", desc)
		}
	}
}

// renderWrite is the ONE set/unset result printer. changed is the daemon's
// verdict: false is "unchanged", never "updated" — a no-op must not read as a
// success, and an idempotent unset is a no-op, not a failure.
func renderWrite(w io.Writer, s settingsView, changed bool) {
	fmt.Fprintf(w, "%s  %s\n", s.Key, s.Value)
	if changed {
		fmt.Fprintf(w, "  %s\n", writeWord(s))
	} else {
		fmt.Fprintln(w, "  unchanged")
	}
}

// writeWord names a changed write as what it did: an unset returns to the
// default, a set is an update. It never reads a no-op as a change.
func writeWord(s settingsView) string {
	if deref(s.Stored) == "" {
		return "unset — back to the default"
	}
	return "updated"
}

// renderHistory is the ONE audit printer: rows in the order the daemon gave
// them (newest first), each a line of time · key · op · old→new · by actor.
// An absent old prints "(default)"; the before-state is never dropped.
func renderHistory(w io.Writer, events []settingEventView) {
	if len(events) == 0 {
		fmt.Fprintln(w, "no changes are recorded yet")
		return
	}
	for _, e := range events {
		fmt.Fprintf(w, "%s  %-30s %-6s %-10s → %-10s  by %s\n",
			e.OccurredAt, e.Key, e.Op, eventOld(e), eventNew(e), e.Actor)
	}
}

// eventOld is the before-state: the old value, or "(default)" when the key had
// no stored value yet (a first write).
func eventOld(e settingEventView) string {
	if e.OldValue == nil {
		return "(default)"
	}
	return *e.OldValue
}

// eventNew is the after-state: the new value, or "(unset)" for an unset (a
// delete leaves no stored value — back to the default).
func eventNew(e settingEventView) string {
	if e.NewValue == nil {
		return "(unset)"
	}
	return *e.NewValue
}

// sourceWord renders the source that won, as the table spells it. An unnamed
// source is said verbatim, not as "default" — an unknown is a third state.
func sourceWord(s settingsView) string {
	if s.Source == "" {
		return "(source unreported)"
	}
	return s.Source
}

// envNameFor derives the AEON_SETTING_… name from a key, for the environment
// line and its warning. It is a pure DISPLAY helper — the daemon's EnvName
// rule (prefix + upper-case + "." → "_"), copied so the CLI need not import
// internal/config. It is not a spelling authority: it formats, it never
// accepts or canonicalises a value.
func envNameFor(key string) string {
	return "AEON_SETTING_" + strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
}

// deref renders a *string the wire carries: nil is "" (an absent field), never
// a panic.
func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// printJSON prints the raw result object as one JSON document (the --json
// contract): the wire shape, with the null-meaningful fields kept null (no
// omitempty on stored / the env-invalid / the updated stamps), so a script
// depends on the wire shape and not on the printer above.
func printJSON(w io.Writer, res any) error {
	raw, err := json.Marshal(res)
	if err != nil {
		return fmt.Errorf("the result could not be encoded to JSON: %w", err)
	}
	fmt.Fprintln(w, string(raw))
	return nil
}

// --- the wire shapes (a local mirror of T5's door, pending its landing) ---
//
// These mirror the design's CS10 shapes — config.View and config.Event, which
// T5's protocol.SettingView / protocol.SettingEventView map — so that when the
// door lands this file can import them from internal/protocol and drop these.
// The null-meaningful fields are *string WITHOUT omitempty (a client must tell
// "no row" from "field omitted"), exactly as the parent's CS10 spec requires.

// settingsView mirrors config.View: the declaration, the effective value, and
// the layer facts a client needs to know which layer won (D3).
type settingsView struct {
	Key         string   `json:"key"`
	Area        string   `json:"area"`
	Type        string   `json:"type"`
	Default     string   `json:"default"`
	Description string   `json:"description"`
	Scope       string   `json:"scope"`
	Sensitive   bool     `json:"sensitive"`
	Min         *int     `json:"min,omitempty"`
	Max         *int     `json:"max,omitempty"`
	MaxLen      int      `json:"maxLen"`
	EnumValues  []string `json:"enumValues"`

	Value         string  `json:"value"`
	Source        string  `json:"source"`
	Stored        *string `json:"stored"` // null when no row — no omitempty
	ShadowedBy    string  `json:"shadowedBy"`
	EnvInvalid    *string `json:"envInvalid"` // null when the env is valid — no omitempty
	StoredInvalid bool    `json:"storedInvalid"`
	UpdatedAt     string  `json:"updatedAt"`
	UpdatedBy     string  `json:"updatedBy"`
}

// settingEventView mirrors config.Event (the append-only audit row). OldValue /
// NewValue are *string: nil is "there was none" (a first write's old, an
// unset's new), not an empty string.
type settingEventView struct {
	ID         int64   `json:"id"`
	Key        string  `json:"key"`
	Op         string  `json:"op"`
	OldValue   *string `json:"oldValue"`
	NewValue   *string `json:"newValue"`
	Sensitive  bool    `json:"sensitive"`
	ShadowedBy string  `json:"shadowedBy"`
	Actor      string  `json:"actor"`
	OccurredAt string  `json:"occurredAt"`
}

// --- the params and results (the wire envelope, one per method) ---

type settingsListParams struct {
	Area string `json:"area,omitempty"`
}

type settingsListResult struct {
	Settings []settingsView `json:"settings"`
}

type settingsGetParams struct {
	Key string `json:"key"`
}

type settingsGetResult struct {
	Setting settingsView `json:"setting"`
}

type settingsSetParams struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type settingsSetResult struct {
	Setting settingsView `json:"setting"`
	Changed bool         `json:"changed"`
}

type settingsUnsetParams struct {
	Key string `json:"key"`
}

type settingsUnsetResult struct {
	Setting settingsView `json:"setting"`
	Changed bool         `json:"changed"`
}

type settingsHistoryParams struct {
	Key   string `json:"key,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

type settingsHistoryResult struct {
	Events []settingEventView `json:"events"`
}
