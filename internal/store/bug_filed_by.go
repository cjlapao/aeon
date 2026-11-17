package store

// ════════════════════════════════════════════════════════════════════════════
// The `filed_by` agent-name → slug migration (S51 T1, TM3).
//
// bug.filed_by was written as whatever free text the caller offered — a
// person path wrote 'person', and the wire param carried an agent NAME that
// nothing checked against agent.name (a forged filer could name anyone).
// S43 T2 established the vocabulary: the agent's identity is its SLUG, and
// references must travel slug-keyed. This method re-points every existing
// name to its slug — the exact MigrateAgentPolicyReferences shape two
// phases deep (agent.go): resolve ALL first, apply NOTHING until all
// resolve; a single name that resolves to no agent refuses the WHOLE
// migration naming every orphan, because half-migrated filed_by is the
// exact orphan the rule exists to prevent.
//
// Why a Go method and not a migration file: the UPDATEs fit SQL, but the
// refuse CANNOT name the refused values from SQL — a CHECK/RAISE message is
// static and a SQLite constraint error never carries a value — and "refuse
// naming every unresolved value" is the spec's requirement. Same answer
// S43 T2 reached for the same reason. It runs at daemon start after
// migrations (the S48 adopt-at-start shape), NOT inside store.Open: a test
// fixture may plant a filed_by name with no agent row, and the S29 store
// tests must keep opening their stores.
//
// The exempt literals are 'person' (the human path, unchanged — TM3) and
// 'system:gates' (the S57 gate filer, filed_by/gatewatch.go's own name):
// both are literals, not names — nothing to resolve, nothing to refuse. A
// filed_by already equal to an agent slug is skipped, which is what makes
// the whole thing idempotent.
// ════════════════════════════════════════════════════════════════════════════

import (
	"fmt"
	"strings"
)

// filedByLiterals are the filed_by values that are not agent references at
// all: 'person' (D7's human path) and 'system:gates' (the S57 filer's own
// identity — a system actor, never an agent row). They stay byte-for-byte.
var filedByLiterals = []string{"person", "system:gates"}

// MigrateBugFiledBySlugs re-points every bug.filed_by agent NAME to its
// S43 slug. All-or-nothing (phase 1 resolves every value, phase 2 is
// reached only when no orphan exists), idempotent (slug-valued rows skip),
// literal-exempt. It returns how many bug rows it re-pointed.
func (s *Store) MigrateBugFiledBySlugs() (int, error) {
	rows, err := s.r.Query(`SELECT DISTINCT filed_by FROM bug
		WHERE filed_by NOT IN ('person', 'system:gates')`)
	if err != nil {
		return 0, err
	}
	var values []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return 0, err
		}
		values = append(values, v)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	// Phase 1: resolve every value to a slug; collect ALL orphans before
	// refusing, so the operator hears the whole list at once — "check first
	// with a single statement, naming everything" carried into Go.
	type repoint struct {
		name string
		slug string
	}
	var toUpdate []repoint
	var orphans []string
	for _, v := range values {
		// Already a slug (a row this migration already did): skip, idempotent.
		if _, isSlug, err := s.AgentBySlug(v); err != nil {
			return 0, err
		} else if isSlug {
			continue
		}
		// A name: resolve it through the agent table.
		a, found, err := s.AgentByName(v)
		if err != nil {
			return 0, err
		}
		if !found {
			orphans = append(orphans, v)
			continue
		}
		toUpdate = append(toUpdate, repoint{name: v, slug: a.Slug})
	}
	if len(orphans) > 0 {
		return 0, fmt.Errorf("refused: bug.filed_by carries %d name(s) that resolve to no agent — %s; a name with no agent row is an orphan, and the migration refuses WHOLE rather than half-migrating (S51 TM3; insert the missing agent rows or correct filed_by, then re-run)",
			len(orphans), strings.Join(quoteAll(orphans), ", "))
	}
	// Phase 2: apply the re-points.
	n := 0
	for _, u := range toUpdate {
		res, err := s.w.Exec(`UPDATE bug SET filed_by = ? WHERE filed_by = ?`, u.slug, u.name)
		if err != nil {
			return n, err
		}
		c, err := res.RowsAffected()
		if err != nil {
			return n, err
		}
		n += int(c)
	}
	return n, nil
}

// quoteAll renders each value quoted, for the refusal that names everything.
func quoteAll(vals []string) []string {
	out := make([]string, len(vals))
	for i, v := range vals {
		out[i] = fmt.Sprintf("%q", v)
	}
	return out
}
