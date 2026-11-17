// layers.ts — S44: the folding rule, as pure data, now on the component tree.
//
// The fold folded `project` + `component` into one self-referencing
// `component`: parent_id NULL is a root, set is a child. The screen that
// lists roots shows the root layer only when showing it answers a question
// the person is asking. The rule lives here, apart from the screen, because
// it is the one piece that must be testable without a daemon, and because
// its forbidden state is not "hidden by accident" but COLLAPSE: a screen
// that filters rows OUT of the data has destroyed rows the daemon still
// carries. Folding changes what is drawn, never what is held. (`component.list`
// carries the whole tree; the store test pins it.)

export interface RootRow {
  id: string;
  name: string;
}

export interface Layers {
  /** Whether the root list itself gets a region on the page. */
  showProjects: boolean;
  /** Why, in the sentence the empty state may print verbatim. */
  reason: string;
}

/**
 * The states the root screen can be in, decided from the root rows alone:
 *
 * - no roots at all → there is nothing to fold and nothing to show; the
 *   screen says the tier is empty (and says who would fill it).
 * - exactly one root → a lone root with nothing organised under it is chrome,
 *   not content: the root layer folds, but the ROWS stay (the caller renders
 *   components from their own source).
 * - anything else (two or more roots) → somebody built a root on purpose;
 *   showing it is the point, and a root with children shows its children.
 */
export function visibleLayers(roots: readonly RootRow[]): Layers {
  if (roots.length === 0) {
    return { showProjects: false, reason: 'no roots yet — create one, and name components into it' };
  }
  if (roots.length === 1) {
    return { showProjects: false, reason: 'one root alone — nothing has been organised under it yet, so the root layer folds' };
  }
  return { showProjects: true, reason: 'roots exist as decisions, so the root layer shows' };
}
