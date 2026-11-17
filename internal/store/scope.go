package store

import "fmt"

// Scoping: the mechanism, not the discipline.
//
// # Why a helper rather than a convention
//
// Every scoped row is keyed by `(loop_slug, component_root)` TOGETHER (DB4). A
// query that filters on the slug alone returns another component's rows — and
// slugs are chosen by people and repeat constantly: `refactor` exists in two
// checkouts of the same repository on most machines.
//
// It is also invisible in testing unless a test deliberately creates the SAME
// SLUG IN TWO ROOTS, which is why one does. A test using distinct slugs passes
// against a store with no scoping at all.
//
// So the predicate is produced here and composed by every scoped statement,
// and a test greps the package for a bare `loop_slug = ?` that did not come
// through this file (I4). Discipline is not a mechanism; a grep is.
//
// # What component_root buys for free
//
// A git worktree is a different path for the same repository, so it is a
// different component, gets its own loops and its own concurrency guard, with no
// code that knows what a worktree is. That behaviour is inherited from v1 and
// must not be simplified away.

// Scope identifies one loop in one component.
type Scope struct {
	LoopSlug      string
	ComponentRoot string
}

// Valid reports whether both halves are present.
//
// Both, always. A Scope with one half set is the bug this type exists to
// prevent, and silently treating an empty ComponentRoot as "any" would make
// every read cross-component.
func (sc Scope) Valid() bool {
	return sc.LoopSlug != "" && sc.ComponentRoot != ""
}

// String is for error messages, and names both halves.
func (sc Scope) String() string {
	return fmt.Sprintf("%s in %s", sc.LoopSlug, sc.ComponentRoot)
}

// Where produces the predicate and its bound arguments for a table whose
// columns are named `loop_slug` and `component_root`.
//
// The column prefix is for a join (`l.component_root`); empty for a single table.
func (sc Scope) Where(prefix string) (string, []any) {
	if prefix != "" {
		prefix += "."
	}
	return fmt.Sprintf("%scomponent_root = ? AND %sloop_slug = ?", prefix, prefix),
		[]any{sc.ComponentRoot, sc.LoopSlug}
}

// WhereLoop is the same predicate for the `loop` table, whose slug column is
// `slug` rather than `loop_slug`.
//
// A separate method rather than a parameter, because the asymmetry is real:
// `loop.slug` is the loop's own name and `task.loop_slug` is a reference to it.
// Collapsing them into one call with a column argument would invite a caller to
// pass the wrong one, and the query would still run.
func (sc Scope) WhereLoop(prefix string) (string, []any) {
	if prefix != "" {
		prefix += "."
	}
	return fmt.Sprintf("%scomponent_root = ? AND %sslug = ?", prefix, prefix),
		[]any{sc.ComponentRoot, sc.LoopSlug}
}

// ComponentScope identifies a component without naming a loop, for the reads that
// list loops.
type ComponentScope struct {
	// ComponentRoot is the directory. Empty means EVERY component, which is what
	// `aeon loop list --all` asks for — and it is a separate, explicit flag
	// rather than the default, because a daemon multiplexes every component on
	// the machine and an unscoped default would show a person every
	// repository's loops whenever they were in one. They would learn to ignore
	// the list.
	ComponentRoot string
}

// All reports whether this scope spans every component.
func (ps ComponentScope) All() bool { return ps.ComponentRoot == "" }

// Where produces the predicate, or the always-true one when unscoped.
//
// `1 = 1` rather than an empty string so the caller can always concatenate
// ` WHERE ` + predicate. A caller assembling the WHERE conditionally is a
// caller who will one day get the condition and the arguments out of step.
func (ps ComponentScope) Where(prefix string) (string, []any) {
	if ps.All() {
		return "1 = 1", nil
	}
	if prefix != "" {
		prefix += "."
	}
	return fmt.Sprintf("%scomponent_root = ?", prefix), []any{ps.ComponentRoot}
}
