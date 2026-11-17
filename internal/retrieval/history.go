//go:build !noretrieval

package retrieval

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// history.go — the loop-history corpus (V2's second corpus, and the one
// the demo's first question needs: "have we decided this before?").
//
// // ── V15, applied, and PERMANENT (N6) ──────────────────────────────────────
// // Nothing is indexed that has not passed through something that could have
// // redacted it. That is the whole rule, and the raw per-step transcript table fails it outright:
// // a transcript holds raw `bash` output, file contents, whatever a command
// // printed — and no redaction pass has seen any of it. So that table is
// // NEVER indexed. Not behind a flag. Not for a trusted project. Not for a
// // project whose author asserts their transcripts are clean — the author
// // CANNOT know what a command printed before it landed, so a flag would be a
// // promise nobody is in a position to make. The next person who wants this
// // will look for the flag first: this comment is the answer. There is none,
// // and `TestNoFlagEnablesTranscriptIndexing` keeps it true.
// // ───────────────────────────────────────────────────────────────────────────
//
// Q8 — the full list of what the history corpus indexes, and what stood
// between each source and the index (the list is the property; CORPUS.md
// carries the reasoning, and TestEveryIndexedSourceHasARedactionStep holds
// this list and that file to the same standard):
//
//	| source                          | what stood between it and the index            |
//	| brief artefacts (produces:)     | written to a file a person reads and reviews  |
//	| plan/discovery artefacts        | written to a file a person reads and reviews  |
//	| handoffs                        | a declared schema with stated field purposes  |
//	| chat messages (human-authored)  | a human typed them                            |
//	| the per-step transcript table   | NOTHING — never indexed (V15/N6/D7/I4)         |
//
// Redaction itself: history documents come from inside the project's own
// declared schema and a person's review, and the indexer applies
// redactSecrets before any chunk of ANY corpus reaches a store door or an
// embed call — "never indexed unredacted" means both.

// IndexedHistorySources is Q8's list, IN CODE, because a test has to hold
// the indexer to it: every entry names what stood between it and the
// index, and nothing may index a source absent here.
type IndexedHistorySource struct {
	Source   string // human name of the source
	Redactor string // what stood between it and the index (never empty)
}

// IndexedHistorySources is the committed list. Adding a source here
// without filling Redactor fails TestEveryIndexedSourceHasARedactionStep
// at the door, and adding one to the CODE without adding it here fails the
// same test through the grep half.
var IndexedHistorySources = []IndexedHistorySource{
	{Source: "brief-artefacts", Redactor: "written to a file a person reads and reviews"},
	{Source: "plan-artefacts", Redactor: "written to a file a person reads and reviews"},
	{Source: "discovery-artefacts", Redactor: "written to a file a person reads and reviews"},
	{Source: "handoffs", Redactor: "a declared schema with stated field purposes (S13's O6)"},
	{Source: "chat-messages", Redactor: "human-authored"},
}

// ForbiddenHistorySources names what may NEVER reach the index, with its
// refusal stated. The transcript rows exist in the schema forever; they
// index never (N6's permanence lives here and in the block above). The
// Source name is the table's own (migration 0002), not a token any indexer
// file may carry — the ban table in testdata enforces where this one
// states.
var ForbiddenHistorySources = []IndexedHistorySource{
	{Source: "the per-step transcript table", Redactor: "NOTHING — never indexed (V15, N6, D7, I4); transcripts hold tool output no redaction pass has seen"},
}

// HandoffFieldNames is the declared schema's field list (S13's O6: the
// PURPOSES are declared, which is what makes the content reviewable
// before it is searchable). Field names and their purposes ride INTO the
// chunk text — `summary: ...` — so a search hits a handoff on its field,
// not just its prose.
var HandoffFieldNames = []string{"status", "summary", "evidence"}

// IndexHandoff renders ONE handoff into the chunk the history corpus
// stores. The rendering is the redaction step's shape: declared fields
// only, each prefixed by its field name (the field is what makes it
// searchable), any secret pattern scrubbed. Undeclared keys are NOT
// rendered — the schema's boundary is the index's boundary (S13's O6).
//
// It returns the rendered text; the caller feeds it through the same
// chunking and store doors as any other source, under source name
// "handoff:<slug>/<step>".
func (x *Index) IndexHandoff(ctx context.Context, root, loopSlug string, stepID int64, handoff map[string]string) (string, error) {
	if !ValidCorpus(CorpusHistory) { // tautological by construction; the door's habit
		return "", fmt.Errorf("retrieval: internal error: corpus history invalid")
	}
	if root == "" || loopSlug == "" {
		return "", fmt.Errorf("retrieval: refused a handoff at root %q loop %q — a handoff belongs to a loop in a root; anonymous context is not searchable, it is ambient", root, loopSlug)
	}
	declared := map[string]bool{}
	for _, f := range HandoffFieldNames {
		declared[f] = true
	}
	keys := make([]string, 0, len(handoff))
	for k := range handoff {
		if !declared[k] {
			// the schema is the boundary: an undeclared key is either a bug
			// or a smuggle, and the index renders neither
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("handoff")
	for _, k := range keys {
		b.WriteString("\n")
		b.WriteString(k)
		b.WriteString(": ")
		b.WriteString(redactSecrets(handoff[k]))
	}
	return b.String(), nil
}

// IndexChatMessage indexes one HUMAN-authored message (the third row of
// Q8 — the redactor is "a human typed them"). Model turns are excluded on
// purpose: a model turn's content can quote a tool output verbatim, and
// that path would put transcript content in the index through a side door
// (V15's test is about side doors).
func (x *Index) IndexChatMessage(ctx context.Context, root, loopSlug string, role, content string) error {
	if role != "user" {
		return fmt.Errorf("retrieval: refused a history index for role %q at loop %q — only human-authored messages qualify (V15); assistant/tool turns can carry un-redacted tool output through a quote", role, loopSlug)
	}
	body := "chat (human)\n" + redactSecrets(content)
	return x.putHistoryChunk(ctx, root, fmt.Sprintf("chat:%s", loopSlug), 1, body)
}

// IndexHistoryFile indexes one reviewed artefact (a brief, a plan, a
// discovery note — the produced:` files a person reads): prose takes
// T2's line-window path (V11's normal path), and the read applies the
// redaction step before any chunk is stored.
func (x *Index) IndexHistoryFile(ctx context.Context, root, relPath string) (*IndexTreeResult, error) {
	if root == "" || relPath == "" {
		return nil, fmt.Errorf("retrieval: refused a history artefact at root %q path %q — both name the document (V12: the index is per root)", root, relPath)
	}
	abs := filepath.Join(root, relPath)
	// the artefact must live under the root (the S11 resolver's discipline,
	// kept cheap here: a prefix check after cleaning)
	clean := filepath.Clean(abs)
	if !strings.HasPrefix(clean, filepath.Clean(root)+string(os.PathSeparator)) {
		return nil, fmt.Errorf("retrieval: refused history artefact %q — outside the project root %q; the index is per root (V12), a path escape indexes another root's documents into this one", relPath, root)
	}
	info, err := os.Stat(clean)
	if err != nil {
		return nil, fmt.Errorf("retrieval: refused history artefact %q: %w", relPath, err)
	}
	if info.Size() > maxIndexFileBytes {
		return nil, fmt.Errorf("retrieval: refused history artefact %q — larger than %d bytes; a reviewed document is read by a person, this one cannot be", relPath, maxIndexFileBytes)
	}
	raw, err := os.ReadFile(clean)
	if err != nil {
		return nil, fmt.Errorf("retrieval: read history artefact %q: %w", relPath, err)
	}
	content := []byte(redactSecrets(string(raw)))
	plan, err := x.PlanFile(root, CorpusHistory, relPath, content)
	if err != nil {
		return nil, err
	}
	written, deleted, err := x.ApplyChunkPlan(root, CorpusHistory, plan)
	if err != nil {
		return nil, err
	}
	res := &IndexTreeResult{Root: root, Written: written, Deleted: deleted, Unchanged: plan.Unchanged}
	for _, s := range plan.Skips {
		res.Skips = append(res.Skips, IndexSkip{Path: relPath, Reason: s.Reason})
	}
	if written == 0 && deleted == 0 && len(plan.Skips) == 0 {
		res.Indexed++
	}
	return res, nil
}

// putHistoryChunk stores a single rendered chunk (handoffs, chat) through
// the SAME store door a planned file goes through — one door for every
// write, so the redaction and the V4 shape (locations, never body-in-
// results) have one enforcement point.
func (x *Index) putHistoryChunk(ctx context.Context, root, source string, lineNo int, body string) error {
	plan, err := x.PlanFile(root, CorpusHistory, source, []byte(body))
	if err != nil {
		return err
	}
	// the single rendered value becomes exactly the one chunk the door
	// would produce for a one-line body at its line; nothing else for this
	// source is kept (a re-asked question overwrites its own history row
	// set — the except-prune inside PlanFile's apply does it)
	_, _, err = x.ApplyChunkPlan(root, CorpusHistory, plan)
	return err
}

// redactSecrets scrubs the shapes that must never ride into an index or an
// embed call even from a "reviewed" document: a person can paste a token
// into a brief without intending to publish it to an embedding provider.
// It is deliberately narrow (the shapes the house credentials use), and
// deliberately BEFORE any store door — the whole corpus passes through it,
// not only history ("never indexed un-redacted" means both).
func redactSecrets(s string) string {
	out := s
	for _, tok := range secretPrefixes {
		for {
			i := strings.Index(out, tok)
			if i < 0 {
				break
			}
			end := strings.IndexAny(out[i:], " \t\r\n\"',")
			if end < 0 {
				out = out[:i] + "[redacted]"
				break
			}
			out = out[:i] + "[redacted]" + out[i+end:]
		}
	}
	return out
}

// secretPrefixes are the credential shapes the S18 routes themselves speak
// (the credential shapes S18 routes hold): narrow on
// purpose — this is a safety net under
// a review step, not a secret scanner (that job belongs to whoever writes
// the document, and S13's review is the redaction of record).
var secretPrefixes = []string{
	"sk-",                 // OpenAI-style
	"sk-ant-",             // Anthropic-style (shares the sk- rule; listed for clarity)
	"ghp_", "github_pat_", // GitHub
	"xoxb-", "xoxp-", // Slack
	"AIza", // Google
	"AKIA", // AWS access key id
	"-----BEGIN RSA PRIVATE KEY-----",
	"-----BEGIN OPENSSH PRIVATE KEY-----",
	"-----BEGIN PRIVATE KEY-----",
}
