/**
 * Which minion an agent is. The kit draws three built-in characters — pip, bop
 * and noodle — and every agent is one of them, picked from its slug: "random"
 * across the roster, but the same agent is the same minion on every screen, in
 * every window and after a re-seed. A name that changed between renders would
 * make the badge useless for telling two interleaved agents apart.
 */
export const BUILT_IN_MINIONS = ["pip", "bop", "noodle"] as const;
export type BuiltInMinion = (typeof BUILT_IN_MINIONS)[number];

/** Minions are yellow. An agent with no colour of its own wears this one. */
export const MINION_DEFAULT_COLOR = "yellow";

function hash(s: string): number {
  let h = 2166136261;
  for (let i = 0; i < s.length; i++) h = Math.imul(h ^ s.charCodeAt(i), 16777619);
  return h >>> 0;
}

export function minionNameFor(slug: string): BuiltInMinion {
  return BUILT_IN_MINIONS[hash(slug) % BUILT_IN_MINIONS.length] ?? "pip";
}
