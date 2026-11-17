package runtime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/search"
)

// Context assembly: sections in declared order, and a manifest frozen at
// assembly (K13, K23).
//
// # Everything is injected, and that is what makes this testable
//
// `ReadFile`, `Hash` and `Clock` arrive in `ContextDeps` (I5). No `os.ReadFile`,
// no `time.Now`, no `os.Getwd`. Three consequences, and the third is the one
// that matters later:
//
//   - assembly is deterministic, so the same declaration twice produces the
//     same manifest and a test can assert it
//   - the whole of this file is testable with no filesystem
//   - **S6 can assert identity across a suspension** — a resumed step assembles
//     against the same injected reader and compares manifests, which is how
//     "the file changed while you waited" becomes detectable rather than
//     silently acted upon
//
// # A missing declared file is a construction error (D13)
//
// Not an empty section. An empty section reads to a model as *"this file is
// empty"* — a different and more damaging claim than *"this file is missing"*,
// because the model will reason from the absence. A step whose context did not
// assemble should never reach a provider.

// ContextDeps are the three things assembly needs, injected.
type ContextDeps struct {
	// ReadFile reads a declared file. Its paths are WORKSPACE-RELATIVE and the
	// caller supplies the resolution, so this package never joins a path.
	ReadFile func(string) ([]byte, error)

	// Hash digests a file's content for the manifest. Injected so a test can
	// assert manifest shape without depending on SHA-256's output, and so a
	// future slice can change the algorithm in one place.
	Hash func([]byte) string

	// Clock is the source of DeadlineAt. Injected so a test can assert an exact
	// absolute instant.
	Clock func() time.Time

	// DeadlineWindow is how long a step may WORK before it is blocked (0: the
	// compiled DefaultDeadline). NoDeadline turns the bound off. Time spent
	// waiting on a person is not working time: a step resumed from a human
	// answer gets its window again (ResumeStep), so a question may sit for hours
	// or days. A crash recovery does not — a wedged step must still be stopped.
	DeadlineWindow time.Duration
	NoDeadline     bool

	// Search resolves retrieval sections at DECLARATION time (S22 T7, V7/R6):
	// "this step needs the code relevant to X". It arrives as a closure —
	// root-bound by the caller who owns the workspace — because this package
	// may not reach the index (the module walk: the hunter cannot contain the
	// hunted; internal/search's leaf types are the only retrieval vocabulary
	// visible here) and because resolution must exist at EXACTLY one instant
	// (N7): the daemon's Compile closure is that instant, and ResumeStep and
	// RecoverStep never receive this field wired (asserted at zero by
	// TestResumeDoesNotReResolve, the assertion this slice's integrity rests
	// on).
	Search func(ctx context.Context, corpus, query string, limit int) (search.SearchAnswer, error)

	// Policies is the step's EVALUATED policy set — the set the gate decides
	// from, handed to assembly by the caller that fetched it (S10, D12).
	// Assembly is a PASS-THROUGH, never a re-query: it freezes what it is
	// given, the same way it freezes what ReadFile returns. A second fetch at
	// assembly time would be a second instant at which the set could differ
	// from the one enforcement decided against — two answers to "what was in
	// force", which is the drift D12 exists to kill.
	Policies []PolicyEntry
}

// withDefaults fills in what a caller did not supply.
//
// `Hash` and `Clock` have sensible defaults; `ReadFile` deliberately does NOT.
// A default reader would be `os.ReadFile` against the process's working
// directory, which is the ambient state I5 forbids — and the failure would be a
// step that read the daemon's files instead of the workspace's, which works on
// the author's machine.
func (d ContextDeps) withDefaults() ContextDeps {
	if d.Hash == nil {
		d.Hash = HashContent
	}
	if d.Clock == nil {
		d.Clock = time.Now
	}
	return d
}

// HashContent is the default content hash: `sha256:` and the hex digest.
//
// Prefixed with the algorithm, so a manifest written today is still readable
// when a later slice changes it — an unprefixed digest is one nobody can
// identify after the fact.
func HashContent(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// AssembleResult is what assembly produced, or why it could not.
//
// A result rather than `(sections, error)` because the failure is a
// CONSTRUCTION failure with a specific shape — which file, and what was wrong
// with it — and a caller renders that differently from a runtime error.
type AssembleResult struct {
	OK bool

	// Text is the whole assembled context, sections in declared order —
	// the pre-S68 single text, kept as SYSTEM then TRANSCRIPT so any
	// consumer that reads the whole (telemetry, the manifest story) still
	// sees everything. The message split itself is what S68 B1 is about:
	// compile reads SystemText and TranscriptText, not this.
	Text string

	// SystemText is every section whose Destination is SectionSystem, in
	// declared order, plus the spoken `## Policy` section (S68, B2/B5). This
	// rides the system message under the handler's persona.
	SystemText string

	// TranscriptText is every section whose Destination is SectionTranscript,
	// in declared order (S68, B3). This rides the user message — the chat
	// window renders exactly this half as "you".
	TranscriptText string

	// Manifest is frozen: the files read, their hashes, and the bounds.
	Manifest ContextManifest

	// Reason names the failing file and what was wrong, when OK is false.
	Reason string
}

// AssembleContext reads the declared sections and freezes the manifest.
//
// Order is the DECLARATION's, exactly (K13). A model reads top to bottom, so
// re-ordering sections changes what it emphasises — which makes order part of
// the handler's meaning rather than a presentation detail.
func AssembleContext(ctx context.Context, specs []SectionSpec, d ContextDeps) AssembleResult {
	d = d.withDefaults()

	manifest := ContextManifest{
		Files:         []FileHash{},
		MaxIterations: MaxIterations,
		// ABSOLUTE, computed once, here (and again on a human-answer resume, see
		// ResumeStep: waiting on a person is not working time).
		DeadlineAt: d.Clock().Add(deadlineWindow(d)),
	}
	if d.NoDeadline {
		manifest.DeadlineAt = time.Time{}
	}
	manifest.DeadlineWindowMs = deadlineWindow(d).Milliseconds()

	// The policy set freezes BEFORE the sections are rendered: an entry whose
	// spec cannot be canonicalized is a construction error (D13's discipline
	// for policies) — a manifest that digested an unspec-shape nobody can
	// reproduce is a PoliciesHash that replays differently tomorrow, which is
	// exactly the drift D12 exists to kill.
	// The marker first, and on the NIL test rather than the length test
	// (DEF-042): a non-nil empty set is a set that was consulted and found
	// empty, and that is a fact the manifest has to carry.
	// freezePolicyEntries still returns nil for an empty set — the copy stays
	// nothing — so without this line the two cases are indistinguishable on
	// disk. withDefaults() fills Hash and Clock only; it never turns a nil
	// Policies into an empty slice, so a storeless assembly keeps a false
	// marker (pinned by TestAssemblyMarksOnlySuppliedSets).
	manifest.PoliciesFrozen = d.Policies != nil
	if len(d.Policies) > 0 {
		frozen := freezePolicyEntries(d.Policies)
		for _, e := range frozen {
			if _, bad := canonicalSpec(e.Spec); bad != "" {
				return AssembleResult{Reason: fmt.Sprintf(
					"the policy entry %q carries a spec that is not valid JSON (%s); a spec that cannot be "+
						"canonicalized cannot be digested, and a manifest hash nobody can reproduce is the "+
						"replay drift D12 forbids", e.ID, bad)}
			}
		}
		manifest.Policies = frozen
		manifest.PoliciesHash = PoliciesHashOf(frozen)
	}

	var rendered []renderedSection
	for i, spec := range specs {
		// S33 T4 (TL6/D4): Category is REQUIRED, and only the two
		// context-side values are legal on a declaration. Refused at this one
		// gate every section passes, rather than defaulted, because a
		// defaulted category is a guess wearing the readout's clothes and D4
		// exists so the app can say what the model was SHOWN. The refusal
		// names the section, its title and the value it refused.
		if spec.Category != CategorySystemPrompt && spec.Category != CategoryInjectedContext {
			return AssembleResult{Reason: fmt.Sprintf(
				"section %d (%q) declares category %q; a section must be classified as "+
					"system_prompt (the handler's own instructional text) or injected_context "+
					"(a file or a retrieval the step was given) — the transcript categories "+
					"(assistant_output, tool_result) are recorded by the loop, never declared, "+
					"and no fifth value exists",
				i, spec.Title, string(spec.Category))}
		}
		// S68 B1: the destination gate, same discipline as the category gate
		// one line above — required, refused when absent, refused by name.
		// A defaulted destination would re-collapse the very distinction
		// S68 exists to draw: instructions rode the user message for eleven
		// slices precisely because nobody had to say where a section goes.
		if spec.Destination != SectionSystem && spec.Destination != SectionTranscript {
			return AssembleResult{Reason: fmt.Sprintf(
				"section %d (%q) declares destination %q; a section must choose %q (rides the system "+
					"message — instructions) or %q (rides the user message — the person's words and the "+
					"material they handed over) — there is no default, because the pre-S68 bug is exactly "+
					"a defaulted destination: policy prose replayed to the chat as if the operator said it",
				i, spec.Title, string(spec.Destination), string(SectionSystem), string(SectionTranscript))}
		}
		set := 0
		for _, present := range []bool{spec.Text != "", spec.DeclaredFile != "", spec.Retrieval != nil} {
			if present {
				set++
			}
		}
		if set > 1 {
			return AssembleResult{Reason: fmt.Sprintf(
				"section %d (%q) declares more than one kind of content; a section must be text, a "+
					"file, or a retrieval — one or the other, because a reader cannot tell which the model "+
					"was shown",
				i, spec.Title)}
		}

		var body string
		switch {
		case spec.Retrieval != nil:
			rb, why := assembleRetrieval(ctx, i, spec, d, &manifest)
			if why != "" {
				return AssembleResult{Reason: why}
			}
			body = rb

		case spec.DeclaredFile != "":
			if d.ReadFile == nil {
				return AssembleResult{Reason: fmt.Sprintf(
					"section %d (%q) declares the file %q and no ReadFile was injected; assembly reads "+
						"nothing by default, because a default reader would resolve against the "+
						"daemon's working directory rather than the workspace",
					i, spec.Title, spec.DeclaredFile)}
			}
			raw, err := d.ReadFile(spec.DeclaredFile)
			if err != nil {
				// D13. The reason names the file, because "context assembly
				// failed" sends somebody to read the handler rather than to
				// look for a path.
				return AssembleResult{Reason: fmt.Sprintf(
					"the declared file %q could not be read: %v. A missing declared file is a "+
						"construction error rather than an empty section, because an empty section "+
						"reads to a model as \"this file is empty\" and it will reason from that",
					spec.DeclaredFile, err)}
			}
			body = string(raw)
			manifest.Files = append(manifest.Files, FileHash{
				Path:       spec.DeclaredFile,
				Hash:       d.Hash(raw),
				Bytes:      len(raw),
				Resolution: "declared",
			})

		case spec.Text != "":
			body = spec.Text

		default:
			return AssembleResult{Reason: fmt.Sprintf(
				"section %d (%q) is empty: it declares neither text nor a file", i, spec.Title)}
		}

		// The composition record (S33 TL6), taken from the RESOLVED body:
		// the characters the model will be shown, not the ones the
		// declaration spent asking for them.
		manifest.Sections = append(manifest.Sections, SectionRecord{
			Title: spec.Title, Category: spec.Category, Chars: len(body),
		})

		rendered = append(rendered, renderedSection{
			title: spec.Title, body: body, dest: spec.Destination,
		})
	}

	// S68 B1: two halves, each rendered in DECLARED order — K13's argument
	// survives the split intact (a model reads each message top to bottom,
	// and within a half that order is exactly the declaration's). The policy
	// section belongs to the system half (B5): it is an instruction about
	// what binds the step, never the operator's words.
	var sys, tran strings.Builder
	for _, r := range rendered {
		target := &tran
		if r.dest == SectionSystem {
			target = &sys
		}
		renderSection(target, r)
	}
	renderPolicySpoken(&sys, manifest)

	systemText := sys.String()
	transcriptText := tran.String()
	whole := systemText
	if transcriptText != "" {
		if whole != "" && !strings.HasSuffix(whole, "\n") {
			whole += "\n"
		}
		if whole != "" {
			whole += "\n"
		}
		whole += transcriptText
	}
	return AssembleResult{OK: true, Text: whole, SystemText: systemText,
		TranscriptText: transcriptText, Manifest: manifest}
}

// renderedSection is a section whose content is RESOLVED (the file read, the
// retrieval answered) and which now only waits for its half to be rendered.
type renderedSection struct {
	title string
	body  string
	dest  SectionDestination
}

// renderSection writes one resolved section — heading and body, one blank
// line of seam — into its half's builder.
func renderSection(b *strings.Builder, r renderedSection) {
	if b.Len() > 0 {
		b.WriteString("\n")
	}
	if r.title != "" {
		fmt.Fprintf(b, "## %s\n\n", r.title)
	}
	b.WriteString(r.body)
	if !strings.HasSuffix(r.body, "\n") {
		b.WriteString("\n")
	}
}

// maxPromptPolicyRules bounds the `## Policy` prompt section (S10, PL9). The
// audit record is the manifest row — all of it, every entry; the prompt is an
// INSTRUCTION, not a transcript. A model that was handed a hundred rules reads
// the first screenful anyway and pays for the rest on every call; a model
// handed the first thirty-two plus a pointer to the manifest is told the same
// truth about what binds it, and the manifest carries the rest for replay.
const maxPromptPolicyRules = 32

// renderPolicySpoken appends the bounded `## Policy` section to the SYSTEM
// half (S68, B5) — the informing half of PL9 ("the runtime enforces; the
// agent is informed so it can behave well, never so it can be trusted to").
//
// # Spoken sentences, not triple-rows
//
// Before S68 this rendered `- tool_access · agent:boss · allow` — a format
// only the policy engine invented. Enforcement is daemon-side regardless of
// what the model reads (PL9's whole argument), so what the model needs is
// INTENT in sentences it will act on: "You may not run `git push`." Costs
// fewer tokens than the triple and reads like advice rather than a database
// dump. The raw evaluation stays where it belongs — the policy store,
// queryable by `policy.why` and the audit.
//
// One sentence per ENTRY, never merged across entries: a `tool_access` row is
// per-agent, and merging its targets into one list would speak agent ids
// where tool names belong — a plausible wrong answer, the exact failure
// three-state honesty (P31) exists to stop. The entry's SPEC is read for the
// name that matters (toolName, command); a spec missing the key speaks the
// honest literal rather than guessing.
//
// An empty set appends nothing: a `## Policy` heading over silence would read
// as "there are no rules", which is a claim a pre-S10 step never made and a
// storeless assembly cannot vouch for (P31). Unknown rule types are NOT
// dropped — they get the honest fallback sentence naming type, target and
// action, because a silently-skipped rule reads as an absent one.
func renderPolicySpoken(b *strings.Builder, manifest ContextManifest) {
	if len(manifest.Policies) == 0 {
		return
	}
	// The SAME canonical (ID-sorted) order the hash digests: the prompt and
	// the manifest disagreeing about which rules are "the first 32" would be
	// two frozen sets in one step.
	rules := sortedPolicyEntries(manifest.Policies)
	shown := rules
	if len(shown) > maxPromptPolicyRules {
		shown = shown[:maxPromptPolicyRules]
	}

	if b.Len() > 0 {
		b.WriteString("\n")
	}
	b.WriteString("## Policy\n\n")
	for _, e := range shown {
		fmt.Fprintf(b, "- %s\n", policySpokenFor(e))
	}
	if omitted := len(rules) - len(shown); omitted > 0 {
		fmt.Fprintf(b, "- … and %d more (the step's context manifest holds the full set)\n", omitted)
	}
	b.WriteString("\nThe daemon enforces these rules whatever you do — a refused attempt returns an error, so spend your turns on what is allowed.")
	if !strings.HasSuffix(b.String(), "\n") {
		b.WriteString("\n")
	}
}

// policySpokenFor renders ONE entry as its sentence. The unknown shape — a
// type this renderer has never been shown, or a spec whose key is missing —
// speaks the literal triple rather than vanishing or inventing (P31).
func policySpokenFor(e PolicyEntry) string {
	forWhom := ""
	if e.TargetKind == "agent" && e.TargetID != "" {
		forWhom = fmt.Sprintf("For agent %s: ", e.TargetID)
	} else if e.TargetKind == "lane-type" && e.TargetID != "" {
		forWhom = fmt.Sprintf("On lane type %s: ", e.TargetID)
	} else if e.TargetKind == "mcp" && e.TargetID != "" {
		forWhom = fmt.Sprintf("On the %s MCP server: ", e.TargetID)
	}
	// all-agents and every other kind speak unprefixed: the rule is about
	// everyone, and naming "all agents" at the front of every sentence is
	// words the model already read in the heading.
	literal := fmt.Sprintf("Policy says: %s for %s:%s is %s.", e.Type, e.TargetKind, e.TargetID, e.Action)

	switch e.Action {
	case "allow", "deny", "ask":
	default:
		return literal
	}

	switch e.Type {
	case "tool_access":
		name := policySpecString(e.Spec, "toolName")
		if name == "" {
			return literal
		}
		switch e.Action {
		case "allow":
			return forWhom + fmt.Sprintf("You may use the %s tool.", name)
		case "deny":
			return forWhom + fmt.Sprintf("You may not use the %s tool.", name)
		default:
			return forWhom + fmt.Sprintf("The %s tool needs a human's approval before you may use it.", name)
		}
	case "command_access":
		cmd := policySpecString(e.Spec, "command")
		if cmd == "" {
			return literal
		}
		switch e.Action {
		case "allow":
			return forWhom + fmt.Sprintf("You may run the shell command `%s`.", cmd)
		case "deny":
			return forWhom + fmt.Sprintf("You may not run the shell command `%s`.", cmd)
		default:
			return forWhom + fmt.Sprintf("The shell command `%s` needs a human's approval before it runs.", cmd)
		}
	case "route_access":
		name := policySpecString(e.Spec, "route")
		if name == "" {
			name = e.TargetID
		}
		if name == "" {
			return literal
		}
		switch e.Action {
		case "allow":
			return forWhom + fmt.Sprintf("You may use the model route %s.", name)
		case "deny":
			return forWhom + fmt.Sprintf("You may not use the model route %s.", name)
		default:
			return forWhom + fmt.Sprintf("Using the model route %s needs a human's approval.", name)
		}
	case "skill_access":
		name := policySpecString(e.Spec, "skill")
		if name == "" {
			name = e.TargetID
		}
		if name == "" {
			return literal
		}
		switch e.Action {
		case "allow":
			return forWhom + fmt.Sprintf("You may use the %s skill.", name)
		case "deny":
			return forWhom + fmt.Sprintf("You may not use the %s skill.", name)
		default:
			return forWhom + fmt.Sprintf("Using the %s skill needs a human's approval.", name)
		}
	case "trigger_access":
		if e.TargetID == "" {
			return literal
		}
		switch e.Action {
		case "allow":
			return forWhom + fmt.Sprintf("The trigger %s may start work for you.", e.TargetID)
		case "deny":
			return forWhom + fmt.Sprintf("The trigger %s cannot start work for you.", e.TargetID)
		default:
			return forWhom + fmt.Sprintf("The trigger %s needs a human's approval to fire.", e.TargetID)
		}
	case "effect_class":
		cls := policySpecString(e.Spec, "effectClass")
		if cls == "" {
			cls = e.TargetID
		}
		if cls == "" {
			return literal
		}
		switch e.Action {
		case "allow":
			return forWhom + fmt.Sprintf("You may produce effects of class %s.", cls)
		case "deny":
			return forWhom + fmt.Sprintf("You must not produce effects of class %s.", cls)
		default:
			return forWhom + fmt.Sprintf("Effects of class %s need a human's approval.", cls)
		}
	case "cost_cap":
		summary := strings.TrimSpace(string(e.Spec))
		if summary == "" || summary == "null" {
			summary = e.TargetID
		}
		if summary == "" {
			return literal
		}
		return "Your spend for this step is capped: " + summary + "."
	}
	return literal
}

// policySpecString reads one string key out of an entry's spec. A spec that
// is not an object or lacks the key returns "" — the caller speaks the
// literal rather than inventing a subject for the sentence.
func policySpecString(spec json.RawMessage, key string) string {
	if len(spec) == 0 {
		return ""
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(spec, &m); err != nil {
		return ""
	}
	raw, ok := m[key]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

// policyCanonicalLine renders one entry as the hash's digest line. The field
// order and the `|` separator are the frozen shape of a policy as the step
// saw it — changing them silently invalidates every PoliciesHash an earlier
// step stored, so they are a reviewed change, not a gofmt one.
func policyCanonicalLine(e PolicyEntry) string {
	notAfter := ""
	if e.NotAfter != nil {
		notAfter = e.NotAfter.Format(time.RFC3339Nano)
	}
	// AssembleContext pre-validates specs, so Compact cannot fail for a set
	// that reached a manifest; the raw-bytes fallback is here for a future
	// caller that digests without the pre-check. Raw bytes are still
	// deterministic for the same bytes, which is all a digest line demands.
	spec, bad := canonicalSpec(e.Spec)
	if bad != "" {
		spec = string(e.Spec)
	}
	return strings.Join([]string{
		e.ID, e.Type, e.TargetKind, e.TargetID, e.Action, e.Source, notAfter, spec,
	}, "|")
}

// canonicalSpec is `jsonCompact(Spec)` — json.Compact into a FRESH buffer per
// entry. Compact rewrites insignificant whitespace, so the same logical spec
// written with different indentation digests identically — determinism, which
// is the whole point of hashing the set. Empty (a policy with no
// configuration) is canonically `{}`, not the empty string: the field is
// always present in the line, or a trailing `|` is one more shape a reader
// has to special-case.
func canonicalSpec(spec json.RawMessage) (string, string) {
	if len(spec) == 0 {
		return "{}", ""
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, spec); err != nil {
		return "", err.Error()
	}
	return buf.String(), ""
}

// canonicalPolicyLines sorts by ID and renders the digest lines — the ONE
// canonicalization, used both for the hash and for the prompt's rule order.
// The sort is by ID only and stable, so a set that (illegally) repeats an ID
// still produces one deterministic line ordering.
func canonicalPolicyLines(entries []PolicyEntry) []string {
	sorted := sortedPolicyEntries(entries)
	lines := make([]string, 0, len(sorted))
	for _, e := range sorted {
		lines = append(lines, policyCanonicalLine(e))
	}
	return lines
}

// sortedPolicyEntries is the one deterministic order a frozen set is ever
// shown or digested in: by ID, stable, a copy.
func sortedPolicyEntries(entries []PolicyEntry) []PolicyEntry {
	sorted := append([]PolicyEntry(nil), entries...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	return sorted
}

// PoliciesHashOf digests a set the way the manifest digests it — exported for
// the replay tests and for the ONE production comparator,
// daemon.NewStepPolicyGate (DEF-039): that adapter recomputes this digest
// from the entries a frozen manifest carries and refuses the set when it
// contradicts the value recorded beside them. SHA-256 from the stdlib and
// nothing else (legal constraint 6): this file's ONLY hash import is
// crypto/sha256, and no file outside internal/runtime computes a digest.
func PoliciesHashOf(entries []PolicyEntry) string {
	sum := sha256.Sum256([]byte(strings.Join(canonicalPolicyLines(entries), "\n")))
	return hex.EncodeToString(sum[:])
}

// freezePolicyEntries copies a set into the manifest — deep, byte for byte.
// The caller's slice is theirs: a step whose frozen set changed because the
// caller kept appending to the slice it handed in is a manifest that froze
// nothing. NotAfter and Spec get fresh backing; nothing aliases the input.
func freezePolicyEntries(entries []PolicyEntry) []PolicyEntry {
	if len(entries) == 0 {
		return nil
	}
	out := make([]PolicyEntry, 0, len(entries))
	for _, e := range entries {
		f := PolicyEntry{ID: e.ID, Type: e.Type, TargetKind: e.TargetKind,
			TargetID: e.TargetID, Action: e.Action, Source: e.Source}
		if e.NotAfter != nil {
			at := *e.NotAfter
			f.NotAfter = &at
		}
		if len(e.Spec) > 0 {
			f.Spec = append(json.RawMessage(nil), e.Spec...)
		}
		out = append(out, f)
	}
	return out
}

// RehashResult is what Rehash found: two lists, kept apart.
type RehashResult struct {
	// Changed are files whose content differs from the manifest's hash.
	Changed []string

	// Missing are files that no longer exist.
	//
	// **Separate from Changed, deliberately.** They are reported to a model
	// differently — "this file changed while you waited" and "this file no
	// longer exists" call for different responses, and the second may mean the
	// work is moot. Merging them loses that, and a single "stale" list is a
	// list a caller cannot act on.
	Missing []string
}

// Any reports whether anything moved.
func (r RehashResult) Any() bool { return len(r.Changed) > 0 || len(r.Missing) > 0 }

// Rehash compares a frozen manifest against what is on disk NOW.
//
// # Nothing in S5 calls this
//
// It exists because the manifest exists, and S6 needs it: a step that suspended
// overnight resumes against files that may have moved, and "the context you
// were given is no longer what is there" is something the model must be told
// rather than left to discover.
//
// **It never mutates the manifest.** Freezing means freezing: it compares and
// reports, and the decision about what to do belongs to the caller that knows
// whether the step can continue.
func Rehash(m ContextManifest, d ContextDeps) RehashResult {
	d = d.withDefaults()
	out := RehashResult{}
	if d.ReadFile == nil {
		return out
	}

	for _, f := range m.Files {
		raw, err := d.ReadFile(f.Path)
		if err != nil {
			out.Missing = append(out.Missing, f.Path)
			continue
		}
		if d.Hash(raw) != f.Hash {
			out.Changed = append(out.Changed, f.Path)
		}
	}
	return out
}

// DeclaredPaths lists a handler's declared files, in order.
//
// For the test that asserts assembly reads nothing outside the declared set,
// and for the protocol handler, which resolves them before assembly so a bad
// path fails at construction.
func DeclaredPaths(specs []SectionSpec) []string {
	var out []string
	for _, s := range specs {
		if s.DeclaredFile != "" {
			out = append(out, s.DeclaredFile)
		}
	}
	return out
}

// CleanDeclaredPath normalises a declared path for comparison.
//
// Used only for the "nothing outside the declared set" assertion. It does NOT
// resolve or validate — `tools.Resolve` is the one resolver (W2, I1), and a
// second one here would be the defect that invariant exists to prevent.
func CleanDeclaredPath(p string) string { return filepath.ToSlash(filepath.Clean(p)) }

// retrievedFileLimit caps how many retrieved files enter the context per
// retrieval declaration. The cap is small ON PURPOSE: retrieved files are
// guesses (V14), three guesses already crowd a hand-declared bundle, and
// the compaction budget (S9's territory) should not have to absorb a
// similarity sweep. A model that needs more asks `grep` — which is exactly
// what the verify-before-trusting annotation tells it to do.
const retrievedFileLimit = 3

// assembleRetrieval resolves ONE retrieval declaration, at this one instant
// (N7), and renders its half of the assembled text. The second return is the
// construction refusal, empty when the section assembled.
//
// The rule that shapes every branch below: retrieval NEVER fails the step
// (V7's promise is that the optional slice stays optional). What it can do
// is record — the manifest's Retrievals row says the index was asked, what
// it said, and how much of it was usable. A refusal to answer (an empty
// index naming itself, a store that will not open) is recorded as
// found-nothing with the resolver's own sentence in the Note; silently
// continuing as if retrieval had not been asked is the lie O6 forbids.
func assembleRetrieval(ctx context.Context, i int, spec SectionSpec, d ContextDeps, manifest *ContextManifest) (string, string) {
	rec := RetrievalRecord{Corpus: spec.Retrieval.Corpus, Query: spec.Retrieval.Query}
	record := func(body string) (string, string) {
		manifest.Retrievals = append(manifest.Retrievals, rec)
		return body, ""
	}
	if spec.Retrieval.Corpus != "code" && spec.Retrieval.Corpus != "history" {
		return "", fmt.Sprintf(
			"section %d (%q) declares a retrieval over corpus %q; it must name %q or %q — there is no "+
				"default corpus, because a query without one is answered by whichever index felt like it",
			i, spec.Title, spec.Retrieval.Corpus, "code", "history")
	}
	if strings.TrimSpace(spec.Retrieval.Query) == "" {
		return "", fmt.Sprintf(
			"section %d (%q) declares a retrieval with an empty query; there is nothing to match", i, spec.Title)
	}
	if d.Search == nil {
		return "", fmt.Sprintf(
			"section %d (%q) declares a retrieval and no Search seam was injected; retrieval answers come "+
				"from the built index only (V13), and silently skipping the section would make \"the index "+
				"had no opinion\" and \"this binary cannot answer\" one unreadable state", i, spec.Title)
	}
	if d.ReadFile == nil {
		return "", fmt.Sprintf(
			"section %d (%q) declares a retrieval and no ReadFile was injected; hits are LOCATIONS "+
				"(V4, D8) — turning them into context means reading them through the same one resolver a "+
				"declared file goes through", i, spec.Title)
	}

	ans, err := d.Search(ctx, spec.Retrieval.Corpus, spec.Retrieval.Query, retrievedFileLimit)
	if err != nil {
		rec.Note = err.Error()
		return record(fmt.Sprintf(
			"Retrieval over the %q corpus for %q returned nothing the step could use: %s\n"+
				"The step proceeds on its declared context. This is a recorded fact, not an error.\n",
			spec.Retrieval.Corpus, spec.Retrieval.Query, err.Error()))
	}
	rec.Hits = len(ans.Hits)
	rec.KeywordOnly = ans.KeywordOnly
	kw := ""
	if ans.KeywordOnly {
		kw = ", keyword-only"
	}

	var b strings.Builder
	added, skipped := 0, 0
	for _, hit := range ans.Hits {
		if added >= retrievedFileLimit {
			break
		}
		raw, rerr := d.ReadFile(hit.Source)
		if rerr != nil {
			// A hit that cannot be read is NOT a construction error: the
			// index lags the tree (deletes between index and now are V18's
			// async-reindex territory, not this reader's). It is counted,
			// and if nothing survives, the record says so.
			skipped++
			continue
		}
		manifest.Files = append(manifest.Files, FileHash{
			Path:       hit.Source,
			Hash:       d.Hash(raw),
			Bytes:      len(raw),
			Resolution: "retrieved",
			Query:      spec.Retrieval.Query,
			Score:      hit.Score,
		})
		// The annotation is the whole of V14's differentiator: the same
		// file, announced as retrieved rather than merged in as declared,
		// changes what the model does next — it verifies.
		fmt.Fprintf(&b, "> retrieved for query %q (corpus %s, score %.4f%s) — similarity-matched, not declared; verify with `grep` before trusting it\n\n",
			spec.Retrieval.Query, spec.Retrieval.Corpus, hit.Score, kw)
		b.Write(raw)
		if !strings.HasSuffix(string(raw), "\n") {
			b.WriteString("\n")
		}
		added++
	}
	rec.Files = added
	if added == 0 {
		if skipped > 0 {
			rec.Note = fmt.Sprintf("%d hits returned, none readable now — the index lags the tree", skipped)
		}
		return record(fmt.Sprintf(
			"Retrieval searched the %q corpus for %q and found nothing%s. The step proceeds on its "+
				"declared context; the index was asked and had no opinion. This is a recorded fact, not an error.\n",
			spec.Retrieval.Corpus, spec.Retrieval.Query, kw))
	}
	rec.Found = true
	return record(b.String())
}

// deadlineWindow is the working-time bound a step is given.
func deadlineWindow(d ContextDeps) time.Duration {
	if d.DeadlineWindow > 0 {
		return d.DeadlineWindow
	}
	return DefaultDeadline
}
