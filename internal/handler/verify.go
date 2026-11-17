package handler

// ════════════════════════════════════════════════════════════════════════════
// The mechanical guards — the only automatable check over 2,902 lines of
// prose rewriting, which is why it lands BEFORE any rewrite (R4): 25
// unverified bodies means re-reviewing 25 bodies by hand.
//
// Trust comes from the negative control (D1/C2): verify FAILS on all 26
// originals, and the test suite re-runs that on every build. A guard that
// passes everything proves nothing; this one is measured to reject every
// original, each naming at least three forbidden tools, the skill load and
// the FINAL REPORT (loop_get, memory_append, event_append, dev-loop-protocol
// and FINAL REPORT: 26/26 each, measured 2026-10-25 — not recalled).
//
// The tool set is READ from the registry (O1/I2) through the extraTools
// seam: registering `codesearch` makes a body naming it PASS — the verdict
// follows registry membership, not a hardcoded reject-list, so S22 adding
// `search` cannot orphan a legitimate body.
//
// The `description` gets the SAME checks as the body (A20): the model reads
// it, and a rewrite that strips the body while porting a description that
// says "it declines" ships a handler refusing work from an orchestrator
// that no longer exists. Measured: the caller guard alone fails 25/26
// originals' descriptions.
// ════════════════════════════════════════════════════════════════════════════

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Finding is one refusal, in the refusal style this project requires: the
// thing refused and the reason, actionable without opening the file.
type Finding struct {
	Handler string // handler name or file stem
	Kind    string // forbidden-tool | skill | ritual | front-matter | description
	Ref     string // the token refused
	Msg     string // why, and what to do instead
}

func (f Finding) String() string {
	return fmt.Sprintf("refused: %s · %s · %s · %s", f.Handler, f.Kind, f.Ref, f.Msg)
}

// deadNames were never Aeon tools at all — they are the MCP loop-state
// surface and the v1 memory/event writers. Registry membership cannot
// revive them: Aeon has no loop-state tool and no memory tool, and a name
// for a capability this product does not have is not a pending feature,
// it is a sentence telling the model to do something no tool will answer.
var deadNames = []string{
	"loop_get", "loop_set", "loop_delete", "loop_list",
	"loop_readFile", "loop_readTask", "loop_writeFile", "loop_writeTask",
	"memory_append", "memory_write", "memory_read",
	"event_append", "event_read",
}

// retiredNames are v1 tool names whose capability left with the v1 surface
// (W1/A19). Unlike the dead names these ARE registry-shaped: if the
// registry ever names one again, a body naming it is legitimate — which is
// exactly what the O1 test drives. `task` and `patch` are ordinary English
// words too, so they are refused only in syntactic tool positions (call
// syntax or backticked identifiers), while todowrite/codesearch/lsp are
// unmistakable as words and refused as words.
var (
	retiredWordNames  = []string{"todowrite", "codesearch", "lsp"}
	retiredSyntaxOnly = map[string]bool{"task": true, "patch": true}
)

// ritualPatterns carry D3's three categories plus the state-handling
// residue that only made sense inside the v1 loop. Each entry: pattern,
// human name, and the mechanism that replaced it (the message must say what
// does this now, or a reviewer cannot tell deletion from loss).
var ritualPatterns = []struct {
	re   *regexp.Regexp
	name string
	why  string
}{
	{regexp.MustCompile(`(?i)final report`), "FINAL REPORT line", "the handoff is structured (T3), not a prose line"},
	{regexp.MustCompile(`(?i)if any agent other than`), "caller guard", "the blueprint dispatches; a body that declines its dispatcher contradicts the step that runs it"},
	{regexp.MustCompile(`(?i)invoked (solely|only) by`), "caller guard", "the blueprint dispatches"},
	{regexp.MustCompile(`(?i)\bit declines\b`), "caller guard", "nothing routes to a handler except a step; a model instructed to decline invents a refusal state no outcome has"},
	{regexp.MustCompile(`status=blocked`), "v1 status vocabulary", "handoff outcomes are S8's four, not prose key-values"},
	{regexp.MustCompile(`(?i)hand-?write memory|append (any gotcha|to memory|a gotcha)`), "memory-append instruction", "there is no memory tool; lessons are a file the operator's tooling owns (the write tool writes it)"},
	{regexp.MustCompile(`memory\.md`), "memory.md hand-writing", "the MCP rendered it; nothing renders anything here"},
	{regexp.MustCompile(`agent-return`), "event checkpoint", "the daemon emits step events; a body cannot emit its own timeline row"},
	{regexp.MustCompile(`loopSlug`), "loop state handle", "context arrives resolved in the step request; bodies never address loop state"},
	{regexp.MustCompile(`dev-loops?\b`), "the dev-loops orchestrator", "it does not exist here; the blueprint is the orchestrator"},
}

// skillPatterns implement D2: nothing loads skills. The word alone is the
// tell — domain prose does not discuss skill loading, and a body that does
// is instructing the model to fetch instructions from somewhere the
// runtime will never read.
var skillPatterns = []struct {
	re   *regexp.Regexp
	name string
}{
	{regexp.MustCompile(`dev-loop-protocol`), "the dev-loop-protocol skill"},
	{regexp.MustCompile(`(?i)\bskills?\b`), "a skill reference"},
}

// snakeCall is the v1 MCP call signature: lowercase snake_case followed by
// an open paren. Go calls are camelCase and shell commands do not parenthesise
// their names, so this catches `loop_set(...)`-shaped residue without firing
// on `strings.Join(` or `find (`-free shell.
var snakeCall = regexp.MustCompile(`\b[a-z][a-z0-9]*(?:_[A-Za-z0-9]+)+\(`)

// backtickedId names tool positions in prose: `patch` means the patch tool;
// patch means the English word.
var backtickedId = regexp.MustCompile("(?m)^.*?`([a-z][a-z0-9_]+)`.*$")

// Verify checks one loaded Definition: body AND description (A20).
// extraTools extends the registry set for the O1 test only; production
// callers pass nil.
func Verify(d Definition, extraTools map[string]bool) []Finding {
	var findings []Finding
	reg := toolsOf(extraTools)
	seen := map[string]bool{}
	add := func(kind, ref, msg string) {
		k := kind + "\x00" + ref
		if seen[k] {
			return
		}
		seen[k] = true
		findings = append(findings, Finding{Handler: d.Name, Kind: kind, Ref: ref, Msg: msg})
	}

	checkText := func(kind string, text string) {
		for _, n := range deadNames {
			if wordFound(text, n) {
				add("forbidden-tool", n, "no such tool exists or will — the capability it names has an Aeon mechanism named in the drop record")
			}
		}
		for _, n := range retiredWordNames {
			if wordFound(text, n) && !reg[n] {
				add("forbidden-tool", n, "retired with the v1 surface (W1) — and the registry does not name it (if it ever does, the reference becomes legal, because this check reads the registry, not a list)")
			}
		}
		// Syntactic tool positions catch the English-word-retired names.
		for _, m := range snakeCall.FindAllString(text, -1) {
			name := strings.TrimSuffix(m, "(")
			if !reg[name] {
				add("forbidden-tool", name, "called like a tool and not in the registry — name a registry tool, or record what mechanism covers it")
			}
		}
		for _, m := range backtickedId.FindAllStringSubmatch(text, -1) {
			name := m[1]
			if retiredSyntaxOnly[name] && !reg[name] {
				add("forbidden-tool", name, "named as a tool in backticks but retired with the v1 surface (W1) — the registry does not name it")
			}
		}
		for _, sp := range skillPatterns {
			if sp.re.MatchString(text) {
				add("skill", sp.re.FindString(text), sp.name+" — nothing loads skills here; instructions the model needs are in the body or nowhere")
			}
		}
		for _, rp := range ritualPatterns {
			if rp.re.MatchString(text) {
				add("ritual", rp.re.FindString(text), rp.why)
			}
		}
	}

	checkText("body", d.Body)
	// A20: the model reads the description too. Same checks, distinct Kind
	// so a reviewer sees WHICH field failed.
	descFindingsBefore := len(findings)
	checkText("description-prose", d.Description)
	for i := descFindingsBefore; i < len(findings); i++ {
		findings[i].Kind = "description"
	}
	return findings
}

func wordFound(text, word string) bool {
	re := regexp.MustCompile(`\b` + regexp.QuoteMeta(word) + `\b`)
	return re.MatchString(text)
}

// VerifyFile loads and verifies one file. Original mode applies the body
// checks but not rewrite-loader strictness — the originals are the control
// group, not the patient.
func VerifyFile(path string, original bool, extraTools map[string]bool) ([]Finding, *Reject) {
	var d Definition
	var rej *Reject
	if original {
		d, rej = LoadOriginal(path)
	} else {
		d, rej = Load(path)
	}
	if rej != nil {
		name := strings.TrimSuffix(filepath.Base(path), ".md")
		return []Finding{{Handler: name, Kind: "front-matter", Ref: "front matter", Msg: rej.Error()}}, rej
	}
	return Verify(d, extraTools), nil
}

// ── the drop record ─────────────────────────────────────────────────────────

// DropRecord is a parsed drops/<name>.md (A14/D6/O2). The format exists so
// 2,902 lines of deletions are REVIEWABLE rather than trusted: every entry
// carries line references into the original, the arithmetic must close, and
// the two sections people skip — BECAME MECHANISM and DEFERRED — are
// mandatory even when empty.
type DropRecord struct {
	Handler     string
	Original    string // path as written in the header
	Rewrite     string
	OrigLines   int
	RewriteLine int
	Removed     int
	Added       int
	Sections    map[string][]string // normalised header → bullet lines
	Path        string
}

// MandatorySections are required to EXIST as headers, even when empty —
// their absence is how someone skips a category entirely (A12, A18).
var MandatorySections = []string{"RITUAL", "BECAME MECHANISM", "DOMAIN", "DEFERRED", "REWORDED"}

var (
	dropOriginalRe = regexp.MustCompile(`(?m)^original:\s*(\S+)\s*\((\d+) lines?\)\s*$`)
	dropRewriteRe  = regexp.MustCompile(`(?m)^rewrite:\s*(\S+)\s*\((\d+) lines?\)\s*$`)
	dropDeltaRe    = regexp.MustCompile(`(?m)^delta:\s*(\d+) lines? removed,\s*(\d+) added\s*$`)
	dropSectionRe  = regexp.MustCompile(`(?m)^## (.+?)\s*$`)
	dropEntryRe    = regexp.MustCompile(`^\s*-\s+L\d+(-L\d+)?\b`)
	mcpRefRe       = regexp.MustCompile(`(?i)\bmcp\b`)
)

// ParseDropRecord validates the record's internal claims: every mandatory
// section present, every entry line-referenced, the arithmetic closing
// (original + added == rewrite + removed). It does not read the referenced
// files — checkDropRecordFiles does that when the files exist.
func ParseDropRecord(path string) (*DropRecord, *Reject) {
	raw, err := os.ReadFile(path)
	if err != nil {
		r := reject(path, "cannot read: %v", err)
		r.IO = true
		return nil, r
	}
	text := string(raw)
	r := &DropRecord{Path: path, Sections: map[string][]string{}}
	r.Handler = strings.TrimSuffix(filepath.Base(path), ".md")

	m := dropOriginalRe.FindStringSubmatch(text)
	if m == nil {
		return nil, reject(path, "drop records open with `original: <path> (N lines)` — a record without the original's line count cannot make its delta checkable (D6)")
	}
	r.Original, r.OrigLines = m[1], atoiSafe(m[2])
	m = dropRewriteRe.FindStringSubmatch(text)
	if m == nil {
		return nil, reject(path, "drop records need `rewrite: <path> (M lines)` beside the original for the same reason")
	}
	r.Rewrite, r.RewriteLine = m[1], atoiSafe(m[2])
	m = dropDeltaRe.FindStringSubmatch(text)
	if m == nil {
		return nil, reject(path, "drop records need `delta: X lines removed, Y added` — the arithmetic is the review")
	}
	r.Removed, r.Added = atoiSafe(m[1]), atoiSafe(m[2])
	if r.OrigLines+r.Added != r.RewriteLine+r.Removed {
		return nil, reject(path, "the delta does not add up: original %d + added %d ≠ rewrite %d + removed %d — an unexplained gap is an omission nobody reviewed (D6); measured lines, not remembered ones",
			r.OrigLines, r.Added, r.RewriteLine, r.Removed)
	}

	// Sections.
	locs := dropSectionRe.FindAllStringSubmatchIndex(text, -1)
	for i, loc := range locs {
		// Section identity is the NAME before any parenthetical: the format
		// documents headers like `## RITUAL (deleted — the engine does this)`
		// and the annotation is prose, not key.
		name := strings.ToUpper(strings.TrimSpace(strings.SplitN(text[loc[2]:loc[3]], "(", 2)[0]))
		// Key presence IS section presence — an empty section is a recorded
		// claim, not an absent section, so the key is created even with zero
		// bullets.
		if _, ok := r.Sections[name]; !ok {
			r.Sections[name] = []string{}
		}
		start := loc[1]
		end := len(text)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		for _, line := range strings.Split(text[start:end], "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "-") {
				r.Sections[name] = append(r.Sections[name], strings.TrimSpace(line))
			}
		}
	}
	for _, req := range MandatorySections {
		if _, ok := r.Sections[req]; !ok {
			why := "BECAME MECHANISM is where the got-wrong category lives — 'exit 137 is an OOM kill' reads exactly like domain knowledge and is W5's job now (A12)"
			if req == "DEFERRED" {
				why = "DEFERRED names what a future capability is for; dropping it silently means nobody remembers (A18)"
			} else if req == "REWORDED" {
				why = "REWORDED is what makes A17 (zero added lines) followable — without it every clarity edit looks invented (O3)"
			} else if req == "RITUAL" {
				why = "RITUAL is the ledger of engine-covered deletions; its absence means deletions nobody can attribute"
			} else if req == "DOMAIN" {
				why = "DOMAIN states what survived on purpose; a record that cannot say what it KEPT cannot be reviewed"
			}
			return nil, reject(path, "missing mandatory section %q — required even when empty: %s", req, why)
		}
	}
	// Line references on every entry except REWORDED, whose entries are
	// also line-referenced in the sample format. All five, then.
	for _, sec := range MandatorySections {
		for _, e := range r.Sections[sec] {
			if !dropEntryRe.MatchString(e) {
				return nil, reject(path, "section %s entry %q has no line reference (L<n> / L<n>-L<m>) — 'removed the ritual preamble' is unreviewable; a record that cannot be diffed against the original did not satisfy A4 (O2)", sec, e)
			}
		}
	}
	return r, nil
}

// CheckDropRecordFiles closes the record against the real files: the
// original exists and has the claimed line count (the DEFERRED obligation
// rides on it), and the rewrite exists and matches its count. The original
// side is measured, never trusted to the header.
func CheckDropRecordFiles(r *DropRecord, repoRoot string) *Reject {
	origPath := r.Original
	if !filepath.IsAbs(origPath) {
		origPath = filepath.Join(repoRoot, origPath)
	}
	raw, err := os.ReadFile(origPath)
	if err != nil {
		rj := reject(r.Path, "names original %q which cannot be read: %v", r.Original, err)
		rj.IO = true
		return rj
	}
	if n := countLines(string(raw)); n != r.OrigLines {
		return reject(r.Path, "claims the original has %d lines; it has %d — count from the file, not from memory", r.OrigLines, n)
	}
	rwPath := r.Rewrite
	if !filepath.IsAbs(rwPath) {
		rwPath = filepath.Join(repoRoot, rwPath)
	}
	rawRw, err := os.ReadFile(rwPath)
	if err != nil {
		rj := reject(r.Path, "names rewrite %q which cannot be read: %v", r.Rewrite, err)
		rj.IO = true
		return rj
	}
	if n := countLines(string(rawRw)); n != r.RewriteLine {
		return reject(r.Path, "claims the rewrite has %d lines; it has %d — the delta is the review, and stale counts review nothing", r.RewriteLine, n)
	}
	// A18: an original that mentions MCP forces a NON-EMPTY DEFERRED —
	// 24 of 26 originals do (measured 2026-10-25); the section names what
	// a future MCP client is for rather than vanishing the reference.
	if mcpRefRe.MatchString(string(raw)) && len(r.Sections["DEFERRED"]) == 0 {
		return reject(r.Path, "the original references MCP but DEFERRED is empty — that content is not ritual, not mechanism, and not keepable domain, because the tool does not exist; recording it names what a future MCP client is for (A18)")
	}
	return nil
}

func countLines(s string) int {
	if s == "" {
		return 0
	}
	n := strings.Count(s, "\n")
	if !strings.HasSuffix(s, "\n") {
		n++
	}
	return n
}

func atoiSafe(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return -1
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// VerifyAllBodies sweeps a directory of rewrites; the empty directory is
// the honest state until T4, and the sweep returns none, which is vacuously
// true — TestNoForbiddenToolInAnyBody says so rather than implying more.
func VerifyAllBodies(dir string, extraTools map[string]bool) ([]Finding, []*Reject) {
	var findings []Finding
	var rejects []*Reject
	names, _ := filepath.Glob(filepath.Join(dir, "*.md"))
	sort.Strings(names)
	for _, p := range names {
		fs, rej := VerifyFile(p, false, extraTools)
		findings = append(findings, fs...)
		if rej != nil {
			rejects = append(rejects, rej)
		}
	}
	return findings, rejects
}
