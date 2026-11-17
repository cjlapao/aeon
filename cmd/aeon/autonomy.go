package main

// autonomy.go — S56 T9 (AL13): `aeon autonomy`, the person's terminal door
// onto the autonomy authority. A client of the daemon's ten autonomy.*
// methods (P34), with the recorded offline asymmetry: the reads and the
// restrictive verbs (deny, revoke) fall back to the same service over the
// store when there is no daemon, and grant never runs offline — the audit for
// a widening must carry the peer-attested identity, which only the daemon
// stamps.
//
// # The CLI re-validates nothing and prints what the wire said (the drift rule)
//
// Every rule — the subject, the conditions, the node door, the actor door,
// the expiry bound — is the daemon's, enforced in internal/autonomy and
// internal/store and nowhere else. This file presents the daemon's refusals
// and results VERBATIM (a refusal arrives with its sentence in `what`,
// rendered three-part by renderError) and never re-checks. Flag PARSING
// refuses only what cannot be a value: a non-RFC3339 --expires, a --for that
// is not a duration, a required flag left absent. The --yes gate is a
// SEATBELT against a typo on the two writes that are not reversible in place
// (a grant widens authority; a revoked grant is never re-activated), not a
// security control, and it is the one gate the CLI keeps — mirrored from
// seed, never a second rule.
//
// # The help is generated from the vocabulary, never typed twice
//
// --help's subject and condition lists are built by calling the pure
// internal/autonomy Vocabulary() directly — the SAME function the daemon's
// autonomy.vocabulary maps (zero store access, so it works offline). A
// registry action added later appears in the help and in the grant's rule
// with no edit in this file (TestHelpIsGeneratedFromVocabulary pins it).
//
// No flag, stdin or prompt accepts a secret (C17); nothing here handles one.

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/autonomy"
	"github.com/cjlapao/aeon/internal/protocol"
)

// autonomyVerbs is the CLI↔socket parity table (T0 §3's draft, design AL13):
// every CLI verb names the socket method it is a client of. The parity tests
// walk it both directions against a live method table — a verb with no method
// (the CLI leads the daemon) and a method with no verb (the CLI lags the
// daemon) are both failures, and the closed set has no expected exception.
var autonomyVerbs = []struct {
	verb   string
	method protocol.MethodName
}{
	{"list", protocol.MethodAutonomyList},
	{"show", protocol.MethodAutonomyShow},
	{"vocabulary", protocol.MethodAutonomyVocabulary},
	{"effective", protocol.MethodAutonomyEffective},
	{"pinned", protocol.MethodAutonomyPinned},
	{"changes", protocol.MethodAutonomyChanges},
	{"uses", protocol.MethodAutonomyUses},
	{"grant", protocol.MethodAutonomyGrant},
	{"deny", protocol.MethodAutonomyDeny},
	{"revoke", protocol.MethodAutonomyRevoke},
}

// autonomyDoorFactory is the selection the verbs speak through: dial, and on
// "no daemon" fall back per the verb (grant refuses with the fixed sentence;
// the reads and the restrictive verbs take the direct door and the fixed
// stderr line). A package var so a test can stand in a fake door without
// standing up a daemon or a store.
var autonomyDoorFactory = func(verb string) (autonomyDoor, error) {
	return selectAutonomyDoor(verb, os.Stderr)
}

func cmdAutonomy(w io.Writer, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("autonomy needs a verb: list, show, vocabulary, effective, pinned, changes, uses, grant, deny, revoke — try \"aeon autonomy help\"")
	}
	switch args[0] {
	case "help":
		printAutonomyUsage(w)
		return nil
	case "list":
		return autonomyList(w, args[1:])
	case "show":
		return autonomyShow(w, args[1:])
	case "vocabulary":
		return autonomyVocabulary(w, args[1:])
	case "effective":
		return autonomyEffective(w, args[1:])
	case "pinned":
		return autonomyPinned(w, args[1:])
	case "changes":
		return autonomyChanges(w, args[1:])
	case "uses":
		return autonomyUses(w, args[1:])
	case "grant":
		return autonomyGrant(w, args[1:])
	case "deny":
		return autonomyDeny(w, args[1:])
	case "revoke":
		return autonomyRevoke(w, args[1:])
	default:
		printAutonomyUsage(w)
		return fmt.Errorf("autonomy verb %q unknown — the verbs are list, show, vocabulary, effective, pinned, changes, uses, grant, deny, revoke", args[0])
	}
}

// --- the reads -------------------------------------------------------------

func autonomyList(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("autonomy list", flag.ContinueOnError)
	fs.SetOutput(w)
	root := fs.String("root", "", "only grants under this root component")
	state := fs.String("state", "", "only this polarity: allow or deny (the daemon's closed set)")
	all := fs.Bool("all", false, "include revoked (inactive) grants")
	jsonFlag := fs.Bool("json", false, "print the raw result object as one JSON document")
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	door, err := autonomyDoorFactory("list")
	if err != nil {
		return err
	}
	// Absent stays NIL on the wire: the daemon's "" means "no filter", and a
	// pointer to an empty string would make the state filter answer a refusal
	// for the no-filter ask.
	p := protocol.AutonomyListParams{}
	if strings.TrimSpace(*root) != "" {
		p.RootID = root
	}
	if strings.TrimSpace(*state) != "" {
		p.State = state
	}
	if *all {
		v := true
		p.IncludeInactive = &v
	}
	res, err := door.list(context.Background(), p)
	if err != nil {
		return err
	}
	if *jsonFlag {
		return printJSON(w, res)
	}
	renderAutonomyList(w, res.Grants)
	return nil
}

func autonomyShow(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("autonomy show", flag.ContinueOnError)
	fs.SetOutput(w)
	jsonFlag := fs.Bool("json", false, "print the raw result object as one JSON document")
	addProtocolVersionFlag(fs)
	positional, rest := splitPositional(args, 1)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if len(positional) != 1 || strings.TrimSpace(positional[0]) == "" {
		return &usageError{msg: "`aeon autonomy show` needs a grant id — `aeon autonomy list` names them"}
	}
	door, err := autonomyDoorFactory("show")
	if err != nil {
		return err
	}
	res, err := door.show(context.Background(), protocol.AutonomyShowParams{ID: positional[0]})
	if err != nil {
		return err
	}
	if *jsonFlag {
		return printJSON(w, res)
	}
	renderAutonomyShow(w, res)
	return nil
}

func autonomyVocabulary(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("autonomy vocabulary", flag.ContinueOnError)
	fs.SetOutput(w)
	jsonFlag := fs.Bool("json", false, "print the raw result object as one JSON document")
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	door, err := autonomyDoorFactory("vocabulary")
	if err != nil {
		return err
	}
	res, err := door.vocabulary(context.Background())
	if err != nil {
		return err
	}
	if *jsonFlag {
		return printJSON(w, res)
	}
	renderAutonomyVocabulary(w, res)
	return nil
}

func autonomyEffective(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("autonomy effective", flag.ContinueOnError)
	fs.SetOutput(w)
	root := fs.String("root", "", "the root component the unattended loop would start on (required)")
	origin := fs.String("origin", "", "the start's origin: schedule, webhook, event or scheduler (default: schedule)")
	var labels repeatableString
	fs.Var(&labels, "label", "a label the start would carry (repeat or comma-separated)")
	jsonFlag := fs.Bool("json", false, "print the raw result object as one JSON document")
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*root) == "" {
		return &usageError{msg: "`aeon autonomy effective` needs --root — name the root the unattended loop would start on"}
	}
	door, err := autonomyDoorFactory("effective")
	if err != nil {
		return err
	}
	p := protocol.AutonomyEffectiveParams{RootID: *root, Labels: []string(labels)}
	if strings.TrimSpace(*origin) != "" {
		// A typo'd origin is the daemon's refusal (its closed set), not a
		// parse error — the CLI re-validates nothing.
		p.Origin = origin
	}
	res, err := door.effective(context.Background(), p)
	if err != nil {
		return err
	}
	if *jsonFlag {
		return printJSON(w, res)
	}
	renderAutonomyEffective(w, res)
	return nil
}

func autonomyPinned(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("autonomy pinned", flag.ContinueOnError)
	fs.SetOutput(w)
	jsonFlag := fs.Bool("json", false, "print the raw result object as one JSON document")
	addProtocolVersionFlag(fs)
	positional, rest := splitPositional(args, 1)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if len(positional) != 1 || strings.TrimSpace(positional[0]) == "" {
		return &usageError{msg: "`aeon autonomy pinned` needs a loop id — `aeon loop list` names them"}
	}
	door, err := autonomyDoorFactory("pinned")
	if err != nil {
		return err
	}
	res, err := door.pinned(context.Background(), protocol.AutonomyPinnedParams{LoopID: positional[0]})
	if err != nil {
		return err
	}
	if *jsonFlag {
		return printJSON(w, res)
	}
	renderAutonomyPinned(w, res)
	return nil
}

func autonomyChanges(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("autonomy changes", flag.ContinueOnError)
	fs.SetOutput(w)
	root := fs.String("root", "", "only this root component")
	grant := fs.String("grant", "", "only this grant")
	limit := fs.Int("limit", 0, "the newest N rows (0 = every row)")
	jsonFlag := fs.Bool("json", false, "print the raw result object as one JSON document")
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	door, err := autonomyDoorFactory("changes")
	if err != nil {
		return err
	}
	p := protocol.AutonomyChangesParams{Limit: limit}
	if strings.TrimSpace(*root) != "" {
		p.RootID = root
	}
	if strings.TrimSpace(*grant) != "" {
		p.GrantID = grant
	}
	res, err := door.changes(context.Background(), p)
	if err != nil {
		return err
	}
	if *jsonFlag {
		return printJSON(w, res)
	}
	renderAutonomyChanges(w, res.Changes)
	return nil
}

func autonomyUses(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("autonomy uses", flag.ContinueOnError)
	fs.SetOutput(w)
	root := fs.String("root", "", "only this root component")
	grant := fs.String("grant", "", "only this grant")
	loop := fs.String("loop", "", "only this loop")
	limit := fs.Int("limit", 0, "the newest N rows (0 = every row)")
	jsonFlag := fs.Bool("json", false, "print the raw result object as one JSON document")
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	door, err := autonomyDoorFactory("uses")
	if err != nil {
		return err
	}
	p := protocol.AutonomyUsesParams{Limit: limit}
	if strings.TrimSpace(*root) != "" {
		p.RootID = root
	}
	if strings.TrimSpace(*grant) != "" {
		p.GrantID = grant
	}
	if strings.TrimSpace(*loop) != "" {
		p.LoopID = loop
	}
	res, err := door.uses(context.Background(), p)
	if err != nil {
		return err
	}
	if *jsonFlag {
		return printJSON(w, res)
	}
	renderAutonomyUses(w, res.Uses)
	return nil
}

// --- the writes ------------------------------------------------------------

func autonomyGrant(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("autonomy grant", flag.ContinueOnError)
	fs.SetOutput(w)
	root := fs.String("root", "", "the root component the allow sits on (required)")
	action := fs.String("action", "", "the subject: a registry action name, or the outward class (`aeon autonomy vocabulary` lists them)")
	requireGates := fs.Bool("require-gates", false, "the merge rule must pass at the moment of the merge")
	requireBudget := fs.Bool("require-budget", false, "the loop's governing budgets must not be exhausted (on by default on every allow)")
	var labels repeatableString
	fs.Var(&labels, "label", "a label the start must carry for this grant to apply (repeat)")
	maxPerDay := fs.Int("max-per-day", 0, "no more than this many begun uses in the last 24 hours")
	expires := fs.String("expires", "", "the expiry, RFC3339 (XOR with --for)")
	forDur := fs.String("for", "", "the expiry as a duration from now, e.g. 30d or 72h (XOR with --expires)")
	note := fs.String("note", "", "a free-text note (audited)")
	yes := fs.Bool("yes", false, "confirm the grant (a seatbelt against a typo — a grant widens authority; not a lock)")
	jsonFlag := fs.Bool("json", false, "print the raw result object as one JSON document")
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*root) == "" {
		return &usageError{msg: "`aeon autonomy grant` needs --root — an allow sits on a root component"}
	}
	if strings.TrimSpace(*action) == "" {
		return &usageError{msg: "`aeon autonomy grant` needs --action — name a registry action, or the outward class (`aeon autonomy vocabulary` lists them)"}
	}
	// The seatbelt, checked BEFORE any door: a widening that was not
	// confirmed is not asked at all, so the daemon (and its audit) is never
	// told about it. A plain error, like seed's guard-set gate: exit 1, no
	// door call.
	if !*yes {
		return fmt.Errorf("grant %s on %s: re-run with --yes — a grant widens what unattended loops may do, and the widening should be deliberate (a seatbelt, not a lock)", *action, *root)
	}
	// The two spellings of one value: naming both is the typo the XOR catches
	// (a parse-level refusal — the daemon cannot see which one was meant).
	var expiresSet, forSet bool
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "expires":
			expiresSet = true
		case "for":
			forSet = true
		}
	})
	if expiresSet && forSet {
		return &usageError{msg: "--expires and --for are two spellings of one value — name only one"}
	}
	p := protocol.AutonomyGrantParams{RootID: *root, Subject: *action, Note: *note}
	if forSet {
		d, err := parseGrantDuration(*forDur)
		if err != nil {
			return &usageError{msg: fmt.Sprintf("%q is not a duration — --for takes a Go duration (72h) or whole days (30d)", *forDur)}
		}
		s := time.Now().Add(d).UTC().Format(time.RFC3339)
		p.ExpiresAt = &s
	} else if expiresSet {
		// Parsing refuses only what cannot be a value; the stored grant's
		// expiry rules are the daemon's.
		if _, err := time.Parse(time.RFC3339, *expires); err != nil {
			return &usageError{msg: fmt.Sprintf("%q is not RFC3339 — --expires takes e.g. 2026-12-01T00:00:00Z", *expires)}
		}
		p.ExpiresAt = expires
	}
	// The conditions pointer is the whole-object distinction (AL4): absent
	// (no condition flag named) sends nil and the daemon's default
	// {RequireBudget:true} applies; any one flag named sends the object with
	// the flags' VALUES VERBATIM — a present object is the caller's, even if
	// it turns the default off.
	condsNamed := false
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "require-gates", "require-budget", "max-per-day", "label":
			condsNamed = true
		}
	})
	if condsNamed {
		conds := protocol.AutonomyConditions{
			RequireGates:  *requireGates,
			RequireBudget: *requireBudget,
			Labels:        []string(labels),
			MaxPerDay:     *maxPerDay,
		}
		p.Conditions = &conds
	}
	door, err := autonomyDoorFactory("grant")
	if err != nil {
		return err
	}
	res, err := door.grant(context.Background(), p)
	if err != nil {
		return err
	}
	if *jsonFlag {
		return printJSON(w, res)
	}
	// The daemon's own confirmation, verbatim (AL17: the one formatter).
	fmt.Fprintln(w, res.Confirmation)
	if res.Grant != nil {
		renderAutonomyGrant(w, *res.Grant)
	}
	return nil
}

func autonomyDeny(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("autonomy deny", flag.ContinueOnError)
	fs.SetOutput(w)
	node := fs.String("node", "", "the component the veto sits on — a root or a child (required)")
	action := fs.String("action", "", "the subject: a registry action name, or the outward class (`aeon autonomy vocabulary` lists them)")
	note := fs.String("note", "", "a free-text note (audited)")
	jsonFlag := fs.Bool("json", false, "print the raw result object as one JSON document")
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*node) == "" {
		return &usageError{msg: "`aeon autonomy deny` needs --node — a veto sits on a root or a child"}
	}
	if strings.TrimSpace(*action) == "" {
		return &usageError{msg: "`aeon autonomy deny` needs --action — name a registry action, or the outward class (`aeon autonomy vocabulary` lists them)"}
	}
	// No --yes: a veto only restricts, and a restriction needs no seatbelt.
	door, err := autonomyDoorFactory("deny")
	if err != nil {
		return err
	}
	res, err := door.deny(context.Background(), protocol.AutonomyDenyParams{NodeID: *node, Subject: *action, Note: *note})
	if err != nil {
		return err
	}
	if *jsonFlag {
		return printJSON(w, res)
	}
	fmt.Fprintln(w, res.Confirmation)
	if res.Grant != nil {
		renderAutonomyGrant(w, *res.Grant)
	}
	return nil
}

func autonomyRevoke(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("autonomy revoke", flag.ContinueOnError)
	fs.SetOutput(w)
	reason := fs.String("reason", "", "why (required — a revocation without one cannot be audited)")
	yes := fs.Bool("yes", false, "confirm the revocation (a seatbelt against a typo — a revoked grant is never re-activated; not a lock)")
	jsonFlag := fs.Bool("json", false, "print the raw result object as one JSON document")
	addProtocolVersionFlag(fs)
	positional, rest := splitPositional(args, 1)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if len(positional) != 1 || strings.TrimSpace(positional[0]) == "" {
		return &usageError{msg: "`aeon autonomy revoke` needs a grant id — `aeon autonomy list` names them"}
	}
	if strings.TrimSpace(*reason) == "" {
		return &usageError{msg: "`aeon autonomy revoke` needs --reason — a revocation without one cannot be audited"}
	}
	if !*yes {
		return fmt.Errorf("revoke %s: re-run with --yes — a revoked grant is never re-activated (restoring it is a new grant, a new decision), so the revocation should be deliberate (a seatbelt, not a lock)", positional[0])
	}
	door, err := autonomyDoorFactory("revoke")
	if err != nil {
		return err
	}
	res, err := door.revoke(context.Background(), protocol.AutonomyRevokeParams{ID: positional[0], Reason: *reason})
	if err != nil {
		return err
	}
	if *jsonFlag {
		return printJSON(w, res)
	}
	fmt.Fprintln(w, res.Confirmation)
	if res.Grant != nil {
		renderAutonomyGrant(w, *res.Grant)
	}
	if len(res.AffectedLoops) > 0 {
		fmt.Fprintf(w, "running loops pinned to it: %s\n", strings.Join(res.AffectedLoops, ", "))
		fmt.Fprintln(w, "a revoked grant is never re-activated — restoring it is a new grant, a new decision. The loops keep their pin; stop them with `aeon loop stop` (S55).")
	}
	return nil
}

// parseGrantDuration is --for's parser: a Go duration (72h, 30m) or whole
// days (30d). Parsing refuses only what cannot be a value — what a valid
// duration means for the grant is the daemon's.
func parseGrantDuration(s string) (time.Duration, error) {
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}
	if strings.HasSuffix(s, "d") {
		if n, err := strconv.Atoi(strings.TrimSuffix(s, "d")); err == nil && n > 0 {
			return time.Duration(n) * 24 * time.Hour, nil
		}
	}
	return 0, fmt.Errorf("not a duration")
}

// --- the printers (what the wire said, plainly) -----------------------------

func renderAutonomyList(w io.Writer, grants []protocol.GrantView) {
	if len(grants) == 0 {
		fmt.Fprintln(w, "no grants on record (the filter matched nothing, or none exist)")
		return
	}
	for i, g := range grants {
		if i > 0 {
			fmt.Fprintln(w)
		}
		renderAutonomyGrant(w, g)
	}
}

// renderAutonomyGrant prints one grant's block. The describe sentence is the
// DAEMON's (AL17's one formatter) — printed verbatim, never re-worded; this
// block adds the fields the sentence does not carry (the root, the live
// state, the conditions, the note). The used count is deliberately absent:
// the list wire carries no per-grant count — `show` answers "used N times".
func renderAutonomyGrant(w io.Writer, g protocol.GrantView) {
	fmt.Fprintln(w, g.Describe)
	status := "active"
	if g.Revoked != nil {
		status = "revoked"
	}
	line := fmt.Sprintf("  root %s · %s · %s", g.RootID, g.State, status)
	if g.ExpiresAt != nil {
		line += " · expires " + *g.ExpiresAt
	}
	fmt.Fprintln(w, line)
	if c := conditionsLine(g.Conditions); c != "" {
		fmt.Fprintf(w, "  conditions: %s\n", c)
	}
	if g.Note != "" {
		fmt.Fprintf(w, "  note: %s\n", g.Note)
	}
}

// conditionsLine renders the closed condition set a person can read; an empty
// set is "none" — a grant with no conditions is a fact, not an omission.
func conditionsLine(c protocol.AutonomyConditions) string {
	var parts []string
	if c.RequireGates {
		parts = append(parts, "requireGates")
	}
	if c.RequireBudget {
		parts = append(parts, "requireBudget")
	}
	if len(c.Labels) > 0 {
		parts = append(parts, "labels: "+strings.Join(c.Labels, ","))
	}
	if c.MaxPerDay > 0 {
		parts = append(parts, fmt.Sprintf("maxPerDay: %d", c.MaxPerDay))
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, " · ")
}

func renderAutonomyShow(w io.Writer, res protocol.AutonomyShowResult) {
	renderAutonomyGrant(w, res.Grant)
	fmt.Fprintf(w, "  used %d time(s)\n", res.UsedCount)
	if res.LastChange == nil {
		fmt.Fprintln(w, "  last change: none on the audit")
	} else {
		c := res.LastChange
		fmt.Fprintf(w, "  last change: %s %s by %s:%s — %s\n", c.At, c.ChangeKind, c.ActorKind, c.ActorID, c.Summary)
	}
	if len(res.PinnedLoops) == 0 {
		fmt.Fprintln(w, "  running loops pinned to it: none")
	} else {
		fmt.Fprintf(w, "  running loops pinned to it: %s\n", strings.Join(res.PinnedLoops, ", "))
	}
}

func renderAutonomyVocabulary(w io.Writer, v protocol.VocabularyView) {
	fmt.Fprintln(w, "subjects (a grant or veto names one; the registry is the source — a new action appears here with no edit):")
	for _, s := range v.Subjects {
		line := fmt.Sprintf("  %-28s %s", s.Name, s.Label)
		if s.Kind == "class" {
			line += "  [class: covers " + strings.Join(s.Covers, ", ") + "]"
		} else {
			line += "  [" + s.Class + "]"
			if s.RequiresExpiry {
				line += " · needs an expiry"
			}
			if len(s.ForcedConditions) > 0 {
				line += " · forces " + strings.Join(s.ForcedConditions, ", ")
			}
		}
		fmt.Fprintln(w, line)
	}
	fmt.Fprintln(w, "\nconditions (a grant may carry these; a merge grant only the forced ones):")
	for _, c := range v.Conditions {
		marker := ""
		if c.Forced {
			marker = " (forced)"
		}
		fmt.Fprintf(w, "  %-16s %-5s%s — %s\n", c.Name, c.Type, marker, c.Description)
	}
	fmt.Fprintf(w, "\nlimits: maxMergeGrantDays %d · maxLabels %d · maxLabelLen %d · maxNoteBytes %d · maxReasonBytes %d\n",
		v.Limits.MaxMergeGrantDays, v.Limits.MaxLabels, v.Limits.MaxLabelLen, v.Limits.MaxNoteBytes, v.Limits.MaxReasonBytes)
	fmt.Fprintln(w, "\norigins (--origin names one):")
	for _, o := range v.Origins {
		fmt.Fprintf(w, "  %-12s — %s\n", o.Origin, o.Help)
	}
}

func renderAutonomyEffective(w io.Writer, v protocol.EffectiveView) {
	// The statement first: the plain answer to "what could an unattended
	// loop on this root do, started now", before the per-action detail.
	fmt.Fprintln(w, v.Statement)
	for _, a := range v.Actions {
		fmt.Fprintf(w, "  %-28s %-8s — %s\n", a.Action, a.Verdict, a.Because)
		for _, c := range a.Conditions {
			line := fmt.Sprintf("    condition %-14s %s", c.Name, c.State)
			if c.Sentence != "" {
				line += " — " + c.Sentence
			}
			fmt.Fprintln(w, line)
		}
	}
	fmt.Fprintf(w, "evaluated %s · origin %s\n", v.EvaluatedAt, v.Origin)
}

func renderAutonomyPinned(w io.Writer, v protocol.PinnedView) {
	if !v.Pinned {
		// The honest "started with no autonomy", byte-identical to a
		// no-grant loop (AL8) — the wire's sentence, verbatim.
		fmt.Fprintln(w, v.Statement)
		return
	}
	line := fmt.Sprintf("loop %s is pinned (root %s · rev %d · origin %s", v.LoopID, v.RootID, v.Rev, v.Origin)
	if len(v.Labels) > 0 {
		line += " · labels " + strings.Join(v.Labels, ", ")
	}
	fmt.Fprintln(w, line+")")
	for _, g := range v.Grants {
		fmt.Fprintf(w, "  grant %-16s %s — %s\n", g.GrantID, g.Subject, g.Live.Sentence)
	}
	for _, a := range v.Actions {
		fmt.Fprintf(w, "  %-28s %-8s — %s\n", a.Action, a.Verdict, a.Because)
	}
}

func renderAutonomyChanges(w io.Writer, changes []protocol.ChangeView) {
	if len(changes) == 0 {
		fmt.Fprintln(w, "no changes on the audit (the filter matched nothing, or none exist)")
		return
	}
	for _, c := range changes {
		fmt.Fprintf(w, "%s  %-14s grant %-16s by %s:%s — %s\n",
			c.At, c.ChangeKind, c.GrantID, c.ActorKind, c.ActorID, c.Summary)
	}
}

func renderAutonomyUses(w io.Writer, uses []protocol.UseView) {
	if len(uses) == 0 {
		fmt.Fprintln(w, "no uses on the record (the filter matched nothing, or none exist)")
		return
	}
	for _, u := range uses {
		// The outcome is PAIRED in by the wire: a begun row carries the
		// outcome that closed it, or the fixed "unknown" sentence when the
		// process stopped before recording one — both printed, never hidden.
		line := fmt.Sprintf("%s  loop %-20s grant %-16s rev %d  %-8s %s",
			u.At, u.LoopID, u.GrantID, u.GrantRev, u.Phase, u.Action)
		if u.Outcome != nil {
			line += " — outcome: " + *u.Outcome
		}
		fmt.Fprintln(w, line)
	}
}

// --- the help (generated from the vocabulary) --------------------------------

// printAutonomyUsage prints the verb help. The subject and condition lists
// are BUILT from autonomy.Vocabulary() — the pure function the daemon's
// autonomy.vocabulary maps — never typed a second time: a registry action
// added later appears below with no edit in this file (the test pins it).
func printAutonomyUsage(w io.Writer) {
	v := autonomy.Vocabulary()
	var b strings.Builder
	b.WriteString(`aeon autonomy — the autonomy authority, asked of the daemon (S56, P34)

  aeon autonomy list [--root ID] [--state allow|deny] [--all] [--json]
  aeon autonomy show ID [--json]
  aeon autonomy vocabulary [--json]
  aeon autonomy effective --root ID [--origin schedule|webhook|event|scheduler] [--label L …] [--json]
  aeon autonomy pinned LOOP [--json]
  aeon autonomy changes [--root ID] [--grant ID] [--limit N] [--json]
  aeon autonomy uses [--root ID] [--grant ID] [--loop ID] [--limit N] [--json]
  aeon autonomy grant --root ID --action NAME|outward [--require-gates] [--require-budget]
                      [--label L …] [--max-per-day N] [--expires RFC3339|--for 30d]
                      [--note TEXT] --yes [--json]
  aeon autonomy deny --node ID --action NAME|outward [--note TEXT] [--json]
  aeon autonomy revoke ID --reason TEXT --yes [--json]

A client of the daemon's ten autonomy.* methods — the same door the app uses
(P34). Offline, with no daemon, the reads and the restrictive verbs (deny,
revoke) open the SAME service directly over the store and say so; grant never
runs offline — the audit for a widening must carry the peer-attested identity,
which only the daemon stamps; any other dial fault refuses, never falling back
(the direct door is the service, not a copy). --yes is a seatbelt on grant (it
widens authority) and revoke (a revoked grant is never re-activated): refused
without it, and not a security control. --json prints the wire result verbatim;
refusals arrive as the daemon's own sentence, verbatim. No flag accepts a
secret: an authority record is not a credential.
`)
	b.WriteString("\nSubjects --action may name (the registry is the source — a new action appears here with no edit):\n")
	for _, s := range v.Subjects {
		line := fmt.Sprintf("  %-28s %s", s.Name, s.Label)
		if s.Kind == "class" {
			line += "  [class: covers " + strings.Join(s.Covers, ", ") + "]"
		} else {
			line += "  [" + string(s.Class) + "]"
			if s.RequiresExpiry {
				line += " · needs an expiry"
			}
			if len(s.ForcedConditions) > 0 {
				line += " · forces " + strings.Join(s.ForcedConditions, ", ")
			}
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("\nConditions a grant may carry (a merge grant only the forced ones):\n")
	for _, c := range v.Conditions {
		marker := ""
		if c.Forced {
			marker = " (forced)"
		}
		fmt.Fprintf(&b, "  %-16s %-5s%s — %s\n", c.Name, c.Type, marker, c.Help)
	}
	fmt.Fprint(w, b.String())
}
