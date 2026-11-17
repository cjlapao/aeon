package main

// The `aeon learnings` verb — the classified, queryable record of what an
// agent, a project or a person has learned (S28). This is a CLIENT of the
// daemon's `learnings.*` methods and nothing else: it dials the same socket
// the app speaks (P34's ordering), opens no store, and re-validates no law.
// The scope validation (design §3) and the relayed write-protection (M1)
// are decided once, underneath, at the door and the CHECK; the sentences of
// refusal ride out of here verbatim. `relaxed` is refused at the door — a
// hand-authored board answer makes the label lie about its own source.
//
// What this surface owns: argument shape and the honest print — the
// provenance is ALWAYS its own word, and the observed count rides only with
// an observed learning (M3: the strength is the count, never a collapsed
// score), so a non-observed learning never prints a count or a zero.

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/cjlapao/aeon/internal/protocol"
)

func cmdLearnings(w io.Writer, args []string) error {
	if len(args) == 0 {
		printLearningsUsage(w)
		return nil
	}
	switch args[0] {
	case "add":
		return cmdLearningsAdd(w, args[1:])
	case "list":
		return cmdLearningsList(w, args[1:])
	case "show":
		return cmdLearningsShow(w, args[1:])
	case "query":
		return cmdLearningsQuery(w, args[1:])
	case "embed":
		return cmdLearningsEmbed(w, args[1:])
	case "retract":
		return cmdLearningsRetract(w, args[1:])
	case "restore":
		return cmdLearningsRestore(w, args[1:])
	case "board":
		return cmdLearningsBoard(w, args[1:])
	case "history":
		return cmdLearningsHistoryVerb(w, args[1:])
	case "for-agent":
		return cmdLearningsForAgent(w, args[1:])
	case "vocabulary":
		return cmdLearningsVocabulary(w, args[1:])
	default:
		printLearningsUsage(w)
		return fmt.Errorf("unknown `learnings` subcommand %q", args[0])
	}
}

func printLearningsUsage(w io.Writer) {
	fmt.Fprint(w, `aeon learnings — the classified, queryable record of what an agent, a project or a person has learned (S28)

usage:
  learnings add   --scope person|project|agent --scope-key NAME --body TEXT --provenance told|observed|assumed [--observed-count N] [--tag T ...]
  learnings list  --scope person|project|agent --scope-key NAME
  learnings show  --id ID
  learnings query --scope ... --scope-key ... [--tag T ...] [--prefix-tag T ...] [--query-text TEXT]
  learnings embed --scope person|project|agent --scope-key NAME
  learnings retract ID --reason TEXT --yes
  learnings restore ID --yes
  learnings board [--scope S] [--provenance P] [--tag T] [--agent SLUG] [--project ROOT]
                  [--loop SLUG] [--include-retracted] [--json]
  learnings history ID
  learnings for-agent AGENT [--query TEXT] [--limit N] [--json]
  learnings vocabulary

a learning names its scope (person, project or agent), its body, and how it
came to be (provenance). The agent scope is validated by a Go-level check
against the handler registry or a plugin_agent row (design §3); the scope
vocabulary and the provenance close at the database.

provenance `+"`relaxed`"+` is refused here — it is the board's answer, written only
by the board-answer hook, never hand-authored (M1).

S52 adds the person's memory doors. retract and restore are the person's
alone (LM7/LM8): they print the daemon's own retract law and the named row
FIRST and act only on an explicit --yes. board filters run in the daemon
(LM6); every row prints with the daemon's one Describe sentence, never a
CLI paraphrase. for-agent answers the person's question about ONE agent's
readable set and prints the `+"`why`"+` each hit ranked where it does (LM5).
A daemon that is not running answers the SAME bytes from the store
directly, with a fixed line on stderr naming that road.
`)
}

func cmdLearningsAdd(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("learnings add", flag.ContinueOnError)
	fs.SetOutput(w)
	scope := fs.String("scope", "", "person | project | agent — the learning's owner (D1); required")
	scopeKey := fs.String("scope-key", "", "the user, the root or the agent the learning is scoped to — required")
	body := fs.String("body", "", "what was learned — required")
	prov := fs.String("provenance", "", "told | observed | assumed — required; relayed is refused (M1)")
	count := fs.Int("observed-count", 0, "the observed count (M3's strength) — meaningful only for an observed learning")
	var tags flagList
	fs.Var(&tags, "tag", "a free-form tag (repeatable; exact match); omit for prefix")
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *scope == "" || *scopeKey == "" || *body == "" || *prov == "" {
		return &usageError{"learnings add needs --scope, --scope-key, --body and --provenance — a learning names its owner, its body and how it came to be"}
	}
	p := protocol.LearningsAddParams{
		Scope: *scope, ScopeKey: *scopeKey, Body: *body,
		Provenance: *prov, Tags: []string(tags),
	}
	if *count > 0 {
		n := *count
		p.ObservedCount = &n
	}
	c, err := dial(0)
	if err != nil {
		return err
	}
	var res protocol.LearningsItemResult
	if err := c.Call(protocol.MethodLearningsAdd, p, &res); err != nil {
		return err
	}
	fmt.Fprintf(w, "#%d · ", res.Learning.ID)
	printLearningsItem(w, res.Learning)
	return nil
}

func cmdLearningsList(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("learnings list", flag.ContinueOnError)
	fs.SetOutput(w)
	scope := fs.String("scope", "", "person | project | agent — required")
	scopeKey := fs.String("scope-key", "", "the user, the root or the agent — required")
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *scope == "" || *scopeKey == "" {
		return &usageError{"learnings list needs --scope and --scope-key — a learning is scoped to one owner"}
	}
	c, err := dial(0)
	if err != nil {
		return err
	}
	var res protocol.LearningsListResult
	if err := c.Call(protocol.MethodLearningsList, protocol.LearningsListParams{Scope: *scope, ScopeKey: *scopeKey}, &res); err != nil {
		return err
	}
	if len(res.Learnings) == 0 {
		fmt.Fprintln(w, "no learnings — the record is empty for this scope; nothing was refused")
		return nil
	}
	for _, lr := range res.Learnings {
		fmt.Fprintf(w, "#%d · ", lr.ID)
		printLearningsItem(w, lr)
		fmt.Fprintln(w)
	}
	return nil
}

func cmdLearningsShow(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("learnings show", flag.ContinueOnError)
	fs.SetOutput(w)
	id := fs.Int64("id", 0, "the learning id — required")
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *id <= 0 {
		return &usageError{"learnings show needs --id"}
	}
	c, err := dial(0)
	if err != nil {
		return err
	}
	var res protocol.LearningsItemResult
	if err := c.Call(protocol.MethodLearningsShow, protocol.LearningsShowParams{ID: *id}, &res); err != nil {
		return err
	}
	fmt.Fprintf(w, "#%d · ", res.Learning.ID)
	printLearningsItem(w, res.Learning)
	return nil
}

func cmdLearningsQuery(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("learnings query", flag.ContinueOnError)
	fs.SetOutput(w)
	scope := fs.String("scope", "", "person | project | agent — optional narrowing")
	scopeKey := fs.String("scope-key", "", "the user, the root or the agent — optional narrowing")
	var tags flagList
	fs.Var(&tags, "tag", "a tag to filter by (repeatable; exact match)")
	var prefixTags flagList
	fs.Var(&prefixTags, "prefix-tag", "a tag prefix to filter by (repeatable; LIKE 'tag%')")
	queryText := fs.String("query-text", "", "the text to rank by, if an embedding route is configured (T3)")
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	var p protocol.LearningsQueryParams
	p.Scope = *scope
	p.ScopeKey = *scopeKey
	p.QueryText = *queryText
	for _, tg := range tags {
		p.Tags = append(p.Tags, protocol.LearningsTag{Pattern: tg, Prefix: false})
	}
	for _, tg := range prefixTags {
		p.Tags = append(p.Tags, protocol.LearningsTag{Pattern: tg, Prefix: true})
	}
	c, err := dial(0)
	if err != nil {
		return err
	}
	var res protocol.LearningsQueryResult
	if err := c.Call(protocol.MethodLearningsQuery, p, &res); err != nil {
		return err
	}
	if len(res.Learnings) == 0 {
		fmt.Fprintln(w, "no learnings — the tag filter matched nothing; the set is empty, not refused")
		return nil
	}
	for _, lr := range res.Learnings {
		fmt.Fprintf(w, "#%d · ", lr.ID)
		printLearningsItem(w, lr)
		fmt.Fprintln(w)
	}
	return nil
}

// cmdLearningsEmbed is the embedding-pass door (T3's EmbedPending) as a
// secondary client of the socket — the analogue of `aeon index rebuild`. It
// runs one scope's learnings through EmbedPending: every learning without a
// vector yet gets one, idempotently (a second run with nothing new embeds
// nothing). The no-route case is a refusal the daemon names (D5), not a
// zero — the refusal rides out verbatim, so "no route" is never read as
// "embeddings failed".
func cmdLearningsEmbed(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("learnings embed", flag.ContinueOnError)
	fs.SetOutput(w)
	scope := fs.String("scope", "", "person | project | agent — the learnings to embed (D1); required")
	scopeKey := fs.String("scope-key", "", "the user, the root or the agent the pass runs over — required")
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *scope == "" || *scopeKey == "" {
		return &usageError{"learnings embed needs --scope and --scope-key — an embedding pass runs over one scope's learnings, each named"}
	}
	c, err := dial(0)
	if err != nil {
		return err
	}
	var res protocol.LearningsEmbedResult
	if err := c.Call(protocol.MethodLearningsEmbed, protocol.LearningsEmbedParams{Scope: *scope, ScopeKey: *scopeKey}, &res); err != nil {
		return err
	}
	if res.Embedded == 0 {
		// A zero is a real answer (a second run with nothing new), spoken as
		// such — never a silence the caller reads as "nothing is indexed".
		fmt.Fprintln(w, "embedded 0 — every learning in this scope already has a vector (the pass is idempotent)")
		return nil
	}
	fmt.Fprintf(w, "embedded %d learnings in %s/%s\n", res.Embedded, *scope, *scopeKey)
	return nil
}

// --- S52 T5: the person's memory doors -------------------------------------
//
// retract and restore are LM7/LM8's human doors: the daemon's own retract
// law (learnings.vocabulary's RetractLaw — one string, every surface) and
// the named row print FIRST, and without --yes the verb writes NOTHING,
// not even an audit line. The reason bound (LM2's 1..256 bytes) is NOT
// re-checked here — the plane refuses it with its own sentence, byte-for-byte
// the same over either door (learnings_client.go), and a CLI that
// pre-validated would be a second law.

func cmdLearningsRetract(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("learnings retract", flag.ContinueOnError)
	fs.SetOutput(w)
	reason := fs.String("reason", "", "what the memory got wrong — the plane requires 1..256 bytes; it is stamped into the row and its audit (LM2)")
	yes := fs.Bool("yes", false, "confirm the retraction after reading the retract law and the row above — without --yes this verb prints and writes NOTHING (S52 LM7)")
	addProtocolVersionFlag(fs)
	id, rest := peelLearningsID(args)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if id <= 0 {
		return &usageError{"learnings retract needs the learning id — a retraction names the memory it takes back"}
	}
	door, err := selectLearningsDoor(learningsErrOut)
	if err != nil {
		return err
	}
	if err := learningsAuthorityGate(w, door, "retract", id, *yes); err != nil {
		return err
	}
	res, err := door.retract(context.Background(), learningsCLIActor, protocol.LearningsRetractParams{ID: id, Reason: *reason})
	if err != nil {
		return err
	}
	if !res.Changed {
		fmt.Fprintf(w, "learning %d was already retracted — changed:false, nothing was written, not even an audit line (LM2's idempotence)\n", id)
		return nil
	}
	fmt.Fprint(w, "retracted.\n")
	fmt.Fprintf(w, "#%d · ", res.Learning.ID)
	printLearningsItem(w, res.Learning)
	printLearningsRetraction(w, res.Learning)
	fmt.Fprintln(w)
	return nil
}

func cmdLearningsRestore(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("learnings restore", flag.ContinueOnError)
	fs.SetOutput(w)
	yes := fs.Bool("yes", false, "confirm the restore after reading the retract law and the row above — without --yes this verb prints and writes NOTHING (S52 LM8)")
	addProtocolVersionFlag(fs)
	id, rest := peelLearningsID(args)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if id <= 0 {
		return &usageError{"learnings restore needs the learning id — a restore names the memory it puts back"}
	}
	door, err := selectLearningsDoor(learningsErrOut)
	if err != nil {
		return err
	}
	if err := learningsAuthorityGate(w, door, "restore", id, *yes); err != nil {
		return err
	}
	res, err := door.restore(context.Background(), learningsCLIActor, protocol.LearningsRestoreParams{ID: id})
	if err != nil {
		return err
	}
	if !res.Changed {
		fmt.Fprintf(w, "learning %d was already live — changed:false, nothing was written, not even an audit line (LM2's idempotence)\n", id)
		return nil
	}
	fmt.Fprint(w, "restored.\n")
	fmt.Fprintf(w, "#%d · ", res.Learning.ID)
	printLearningsItem(w, res.Learning)
	fmt.Fprintln(w)
	return nil
}

// learningsAuthorityGate is the S49/S50/S51 gate carried to the memory
// doors (RM9/TM9's law: CLI and UI quote ONE authority sentence, and it
// rides the vocabulary answer): the daemon's retract law, the named row as
// it stands — a retraction already on it printed, never hidden — and only
// then the act, and only on --yes.
func learningsAuthorityGate(w io.Writer, door learningsDoor, verb string, id int64, yes bool) error {
	voc, err := door.vocabulary(context.Background())
	if err != nil {
		return err
	}
	row, err := door.show(context.Background(), protocol.LearningsShowParams{ID: id})
	if err != nil {
		return err
	}
	fmt.Fprintln(w, voc.RetractLaw)
	fmt.Fprintf(w, "#%d · ", id)
	printLearningsItem(w, row.Learning)
	printLearningsRetraction(w, row.Learning)
	fmt.Fprintln(w)
	if !yes {
		return &usageError{fmt.Sprintf("learnings %s %d is the person's door — re-run with --yes once you have read what moves above (S52 LM7/LM8)", verb, id)}
	}
	return nil
}

func cmdLearningsBoard(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("learnings board", flag.ContinueOnError)
	fs.SetOutput(w)
	scope := fs.String("scope", "", "filter: one scope (learnings.vocabulary answers the set)")
	prov := fs.String("provenance", "", "filter: one provenance (the closed words are refused by name, never coerced — LM6)")
	tag := fs.String("tag", "", "filter: one exact tag")
	agent := fs.String("agent", "", "filter: agent-scope rows of one agent slug")
	project := fs.String("project", "", "filter: project-scope rows of one root")
	loop := fs.String("loop", "", "filter: rows sourced from one loop slug")
	incRet := fs.Bool("include-retracted", false, "also show retracted rows — flagged, never silently (LM2)")
	asJSON := fs.Bool("json", false, "print the door's JSON answer and nothing else")
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	door, err := selectLearningsDoor(learningsErrOut)
	if err != nil {
		return err
	}
	rows, err := door.board(context.Background(), protocol.LearningsBoardParams{
		Scope: *scope, Provenance: *prov, Tag: *tag,
		Agent: *agent, Project: *project, Loop: *loop,
		IncludeRetracted: *incRet,
	})
	if err != nil {
		return err
	}
	if *asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(rows)
	}
	if len(rows) == 0 {
		fmt.Fprintln(w, "the board is EMPTY (a filter that matches nothing is the filter's answer, not the store's; --include-retracted shows what memory has taken back)")
		return nil
	}
	for _, row := range rows {
		fmt.Fprintf(w, "#%d · [%s/%s] %s · %s", row.ID, row.Scope, row.ScopeKey, row.Body, row.Describe)
		printLearningsRetraction(w, row.LearningsView)
		fmt.Fprintln(w)
	}
	return nil
}

func cmdLearningsHistoryVerb(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("learnings history", flag.ContinueOnError)
	fs.SetOutput(w)
	addProtocolVersionFlag(fs)
	id, rest := peelLearningsID(args)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if id <= 0 {
		return &usageError{"learnings history needs the learning id — history is asked per memory"}
	}
	door, err := selectLearningsDoor(learningsErrOut)
	if err != nil {
		return err
	}
	changes, err := door.history(context.Background(), protocol.LearningsHistoryParams{ID: id})
	if err != nil {
		return err
	}
	if len(changes) == 0 {
		// The empty career is honest and means one thing only (T2's law,
		// carried to the print): the row predates the audit.
		fmt.Fprintf(w, "learning %d has no audit career — it predates S52's learning_change table; the absence is the truth, not a silence\n", id)
		return nil
	}
	for _, ch := range changes {
		fmt.Fprintf(w, "%s · %s:%s · %s · %s\n", ch.OccurredAt, ch.ActorKind, ch.ActorID, ch.Op, ch.Summary)
	}
	return nil
}

func cmdLearningsForAgent(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("learnings for-agent", flag.ContinueOnError)
	fs.SetOutput(w)
	query := fs.String("query", "", "rank the agent's readable set by this text (the one ranking, LM5)")
	limit := fs.Int("limit", 0, "cap the hits (the plane's default and maximum are learnings.vocabulary's)")
	asJSON := fs.Bool("json", false, "print the door's JSON answer and nothing else")
	addProtocolVersionFlag(fs)
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return &usageError{"learnings for-agent needs the agent slug — the read answers a question about ONE agent"}
	}
	agent := args[0]
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	door, err := selectLearningsDoor(learningsErrOut)
	if err != nil {
		return err
	}
	hits, err := door.forAgent(context.Background(), protocol.LearningsForAgentParams{Agent: agent, Query: *query, Limit: *limit})
	if err != nil {
		return err
	}
	if *asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(hits)
	}
	if len(hits) == 0 {
		fmt.Fprintf(w, "agent %s has no memory to show — the readable set is empty, nothing was refused (LM1/LM5: retracted and out-of-scope memories are absent, as they are from the agent itself)\n", agent)
		return nil
	}
	for _, hit := range hits {
		fmt.Fprintf(w, "#%d · [%s/%s] %s · why: rank %d — %s",
			hit.Learning.ID, hit.Learning.Scope, hit.Learning.ScopeKey, hit.Learning.Body,
			hit.Why.Rank, hit.Why.Basis)
		fmt.Fprintln(w)
	}
	fmt.Fprintf(w, "ordering law: %s\n", hits[0].Why.M1Note)
	return nil
}

func cmdLearningsVocabulary(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("learnings vocabulary", flag.ContinueOnError)
	fs.SetOutput(w)
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	door, err := selectLearningsDoor(learningsErrOut)
	if err != nil {
		return err
	}
	voc, err := door.vocabulary(context.Background())
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "scopes        %s\n", strings.Join(voc.Scopes, " · "))
	fmt.Fprintf(w, "provenances   %s\n", strings.Join(voc.Provenances, " · "))
	fmt.Fprintf(w, "actor kinds   %s\n", strings.Join(voc.ActorKinds, " · "))
	fmt.Fprintf(w, "m1 ordering   %s\n", voc.M1Ordering)
	fmt.Fprintf(w, "retract law   %s\n", voc.RetractLaw)
	fmt.Fprintf(w, "limits        reason %d..%d · audit summary ≤%d · for-agent limit %d default / %d max\n",
		voc.Limits.ReasonMin, voc.Limits.ReasonMax, voc.Limits.SummaryMax,
		voc.Limits.ForAgentLimitDef, voc.Limits.ForAgentLimitMax)
	fmt.Fprint(w, "tag grammar   exact and prefix patterns (learnings query --tag / --prefix-tag)\n")
	return nil
}

// printLearningsRetraction says the retraction state of a row that has one
// — never on a live row (LM2: a retracted memory shows FLAGGED and never
// silently; a live memory says nothing because it has nothing to say).
func printLearningsRetraction(w io.Writer, lr protocol.LearningsView) {
	if !lr.Retracted {
		return
	}
	fmt.Fprintf(w, " · RETRACTED %s by %s — %s", lr.RetractedAt, lr.RetractedBy, lr.RetractedReason)
}

// peelLearningsID lifts a leading positive positional id off the args
// (the triage road's peel — `learnings retract 7 --yes` reads as a verb,
// not as a flag archaeology dig).
func peelLearningsID(args []string) (int64, []string) {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return 0, args
	}
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || id <= 0 {
		return 0, args
	}
	return id, args[1:]
}

// printLearningsItem renders one learning the honest way: the provenance is
// always its own word, and the observed count rides only with an observed
// learning (M3) — a non-observed learning never prints a count, not even a
// zero.
func printLearningsItem(w io.Writer, lr protocol.LearningsView) {
	fmt.Fprintf(w, "[%s/%s] %s", lr.Scope, lr.ScopeKey, lr.Body)
	if lr.ObservedCount != nil {
		fmt.Fprintf(w, " · observed %d times", *lr.ObservedCount)
	}
	fmt.Fprintf(w, " · provenance %s", lr.Provenance)
	if len(lr.Tags) > 0 {
		fmt.Fprintf(w, " · tags %s", strings.Join(lr.Tags, ", "))
	}
	if lr.SourceLoopSlug != "" {
		fmt.Fprintf(w, " · from %s", lr.SourceLoopSlug)
	}
}

// flagList is a repeatable string flag (a tag list, a tag prefix list).
type flagList []string

func (f *flagList) String() string { return strings.Join(*f, ", ") }
func (f *flagList) Set(s string) error {
	*f = append(*f, s)
	return nil
}
