// sentences.go — the one formatter of the human-readable grant sentences
// (AL17). A later surface (the socket, the CLI, the panel) prints them
// verbatim; a second formatter is a second truth. Each names the grant id
// and, for a pinned loop, the pinned revision; a denied action never leaks
// another grant's contents, only the one grant that mattered.
//
// The clauses the DECISION SITE also speaks (veto, revoked, expired) are
// built in the lower package (internal/action) and rendered HERE through
// its builders: the core wraps the registry, never the reverse, so the
// shared source sits where the loop reads it. This file calls; it never
// re-types a word the decision site already says.
package autonomy

import (
	"fmt"
	"time"

	"github.com/cjlapao/aeon/internal/action"
)

// localSentence is the run sentence for a local action: it runs without a
// grant, so no grant id is named.
func localSentence(name string, c action.EffectClass) string {
	return fmt.Sprintf("%s · %s · run — effects confined to the working tree (a local action needs no grant)", name, c)
}

// noGrantSentence is the deny for an action no active allow covers: the
// byte-identical-with-no-grants answer (AL16).
func noGrantSentence(name string, c action.EffectClass) string {
	return fmt.Sprintf("%s denied — no autonomy grant on this root covers it (unattended loops run local actions only)", name)
}

// runSentence is the run for a covered action under a grant; it names the
// grant id and, for a pinned loop, the pinned revision (rev 0 = the
// effective view, which pins nothing).
func runSentence(name string, g Grant, rev int64) string {
	return fmt.Sprintf("%s · %s · run — granted by %s (rev %d)", name, g.Subject.Name, g.ID, rev)
}

// vetoSentence is the deny a veto produces; it names the veto's grant id
// (AL3). A veto overrides every allow on the root and its children. The
// words are the decision site's own (action.VetoSentence) — one builder.
func vetoSentence(veto Grant, name string) string {
	return action.VetoSentence(name, veto.ID)
}

// conditionFailSentence names the grant, the failing condition and (for a
// pinned loop) the pinned revision (AL6). It carries the condition's own
// sentence verbatim where the seam supplied one.
func conditionFailSentence(name string, g Grant, cs action.ConditionStatus, rev int64) string {
	s := cs.Sentence
	if s == "" {
		s = "is not met"
	}
	return fmt.Sprintf("%s denied — grant %s's condition %s %s — the loop was pinned at rev %d", name, g.ID, cs.Name, s, rev)
}

// activeSentence is the live sentence for a grant in force: it names the
// grant, what it authorises and where it sits, and its expiry when it has
// one (an unbounded grant says so rather than implying it).
func activeSentence(g Grant) string {
	what := "vetoes"
	if g.State == StateAllow {
		what = "authorises"
	}
	s := fmt.Sprintf("grant %s is in force: it %s %s on %s", g.ID, what, g.Subject.Name, g.NodeID)
	if g.ExpiresAt != nil {
		s += fmt.Sprintf(" until %s", fmtTime(g.ExpiresAt))
	}
	return s
}

// revokedSentence is the deny a revocation produces at the next action; it
// names the grant, who revoked it and when (AL6). The words are the
// decision site's own clause (action.RevokedClause) — one builder.
func revokedSentence(g Grant) string {
	return action.RevokedClause(g.ID, fmtTime(g.RevokedAt), g.RevokedBy)
}

// expiredSentence is the deny an expiry produces at the next action (AL5),
// the same shared clause as the decision site.
func expiredSentence(g Grant) string {
	return action.ExpiredClause(g.ID)
}

// noAutonomyStatement is the whole of the effective view when nothing is
// granted on a root (AL15).
const NoAutonomyStatement = "No autonomy is granted on this root: unattended loops run local actions only"

// fmtTime renders a *time.Time for a sentence, or "" when absent.
func fmtTime(t *time.Time) string {
	if t == nil {
		return "now"
	}
	return t.UTC().Format("2006-01-02T15:04:05Z")
}
