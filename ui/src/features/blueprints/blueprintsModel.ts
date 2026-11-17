import type { RpcMethods } from "../../proto/contract";

// blueprintsModel.ts — the pure half of blueprint management (S48 T12, BP12/BP14).
//
// Nothing here reads YAML, computes a verdict, extracts a reference, or diffs
// two texts: every one of those is the DAEMON's answer (`blueprint.validate`,
// `blueprint.lint`, `blueprint.diff`, the derived `blueprint_ref` rows behind
// `blueprint.usage`), and a client that recomputed one would be a second truth
// the daemon does not know about. What lives here is presentation arithmetic
// over the daemon's own rows — which badge a summary earns, what a revision is
// called, what order to draw a list in, how to lay out a usage answer — plus
// the one vocabulary this slice owns: the proposal kind its tab filters to.

/** One row of `blueprint.list` — the daemon's summary, filters applied daemon-side. */
export type BlueprintSummary = RpcMethods["blueprint.list"]["result"]["blueprints"][number];
/** `blueprint.usage`'s answer: who references this blueprint, and what uses it. */
export type BlueprintUsage = RpcMethods["blueprint.usage"]["result"];
/** One `blueprint.history` row. */
export type BlueprintRevision = RpcMethods["blueprint.history"]["result"]["revisions"][number];
/** One `blueprint.lint` advisory. */
export type BlueprintAdvisory = RpcMethods["blueprint.lint"]["result"]["advisories"][number];
/** `blueprint.save`'s params as the contract's fixture-derived arm describes them. */
export type BlueprintSaveFields = RpcMethods["blueprint.save"]["params"];
/** `blueprint.list`'s params as the same arm describes them. */
export type BlueprintListFields = RpcMethods["blueprint.list"]["params"];
/** One `blueprint.show` reference (the daemon's derived `blueprint_ref` rows). */
export type BlueprintReference = RpcMethods["blueprint.show"]["result"]["references"][number];

/** The proposal kind `blueprint.propose` files and S45's desk decides. One word,
 *  and it is the daemon's: `internal/desk/blueprint_kinds.go` owns the constant. */
export const BLUEPRINT_PROPOSAL_KINDS = ["blueprint_save"] as const;

/** The shipped set's key, for `seed.restore` — the seed set S48 T5 declared. */
export const BLUEPRINTS_STANDARD_SET = "blueprints.standard";

/** The save request, with the field the fixture-derived contract omits.
 *
 *  `contract.ts` records what the conformance fixtures were seen to send; T6's
 *  wire struct carries `note` (the revision's one-line reason) even where no
 *  fixture exercised it. The cast is the same honest one `ProposalsDesk` uses
 *  for `proposal.list`'s `status`: the wire is the daemon's, the type describes
 *  a fixture, and the field is SENT, never invented client-side. */
export type BlueprintSaveRequest = BlueprintSaveFields & { note?: string };

/** Likewise `blueprint.list`'s `q`: T7's filter, absent from the fixture arm. */
export type BlueprintListRequest = BlueprintListFields & { q?: string };

/** `blueprint.lint`'s answer, with the field the fixture arm never carried.
 *
 *  T7's result struct emits `unavailable` only when there is no tools catalog
 *  to ask — an explicit "advisories could not be computed", which is the third
 *  state BP8 insists on. An empty advisory list without it means the catalog
 *  answered and found nothing; with it, nothing was found because nothing could
 *  be found. The UI must not collapse those, so the field stays visible. */
export type BlueprintLintResponse = RpcMethods["blueprint.lint"]["result"] & { unavailable?: string };

/** One side of `blueprint.diff`: a revision number as a string, or the two
 *  words the daemon resolves itself. Sent as text because the daemon's wire
 *  field is text (`a`/`b` are strings, T7). */
export type DiffSide = string;

/** What a row can be made of. Every field except the name is optional because
 *  the daemon may answer with only `names` — the summaries are additive (T7),
 *  and a row with no origin is a row that shows NO origin badge rather than a
 *  guessed one (BP14: absent is not a verdict). */
export interface BlueprintRow {
  name: string;
  origin?: string;
  version?: number;
  revision?: number;
  usedByCount?: number;
  updatedAt?: string;
  updatedBy?: string;
  divergedFromShipped?: boolean;
  /** The friendly name the app shows; `name` stays the slug every reference uses. */
  title?: string;
  /** The flow's own sentence about what it is for; absent when it has none. */
  description?: string;
}

/** The list answer as rows: the daemon's summaries when it sent them, its bare
 *  names otherwise. Order is `sortDisplay`'s, and it is applied to whichever
 *  shape arrived so the list does not reshuffle between daemons. */
export function listRows(result: { names?: string[]; blueprints?: BlueprintSummary[] } | undefined): BlueprintRow[] {
  if (!result) return [];
  if (result.blueprints && result.blueprints.length > 0) return sortDisplay(result.blueprints);
  return sortDisplay((result.names ?? []).map((name) => ({ name })));
}

/** What to call a flow on screen: its title, or its slug when it has none. */
export function flowTitle(row: { name: string; title?: string }): string {
  return row.title && row.title.trim() !== "" ? row.title : row.name;
}

/** Origin is the daemon's word, not a boolean: `system` rows are the shipped
 *  ones, everything else is a person's. An unknown or absent word is neither,
 *  so it earns no badge. */
export function originBadge(origin?: string): { key: "shipped" | "yours"; tone: "violet" | "green" } | null {
  if (origin === "system") return { key: "shipped", tone: "violet" };
  if (origin === "user") return { key: "yours", tone: "green" };
  return null;
}

/** The `Edited` badge — three-state on purpose.
 *
 *  `true` is the daemon saying "this row's bytes are not the shipped ones",
 *  `false` is the daemon saying they are, and `undefined` is the field ABSENT,
 *  which is neither claim: a daemon that answers with names only, or a summary
 *  carrying no verdict, shows no badge. Rendering absent as "unedited" would
 *  tell a person their edit is a shipped byte — P31's third state, and the trap
 *  BP14 names. */
export function editedBadge(row: Pick<BlueprintRow, "divergedFromShipped">): boolean | undefined {
  return typeof row.divergedFromShipped === "boolean" ? row.divergedFromShipped : undefined;
}

/** Whether the UI should OFFER delete. Advisory, always: the daemon decides in
 *  `blueprint.delete`, and its refusal (system origin, or the references it
 *  names one by one) is shown verbatim. A person can reach a delete the UI hid;
 *  nobody can reach one the daemon refused. */
export function canDelete(row: Pick<BlueprintRow, "origin">): boolean {
  return row.origin !== "system";
}

/** A revision's short handle. The word around it is translated at the call
 *  site; this is the number the daemon numbered. */
export function revisionLabel(rev: number): string {
  return `#${rev}`;
}

/** Display order: shipped rows first, then a person's, each alphabetical.
 *
 *  The daemon's own order is meaningful (it is the order `names` came in) and
 *  is not changed on the wire — this only decides what a person scrolls past
 *  first, and it is stable so a refetch does not reshuffle the list under them. */
export function sortDisplay(rows: readonly BlueprintRow[]): BlueprintRow[] {
  return [...rows].sort((a, b) => {
    const rank = (row: BlueprintRow) => (row.origin === "system" ? 0 : 1);
    return rank(a) - rank(b) || a.name.localeCompare(b.name);
  });
}

/** One block of the usage answer: `key` is the i18n suffix, `items` are labels
 *  to draw (names where the row has one, ids where it does not). */
export interface UsageGroup {
  key: "schedules" | "triggers" | "queueItems" | "agents" | "actions" | "skills";
  count: number;
  items: string[];
}

export interface UsageReport {
  groups: UsageGroup[];
  loopsStarted: number;
  /** Nothing references it and nothing was started from it. */
  empty: boolean;
}

/** Lay out `blueprint.usage` for reading.
 *
 *  The daemon already resolved every reference from the derived
 *  `blueprint_ref` rows and the live schedule/trigger/queue tables; this only
 *  groups them. An empty group is dropped rather than drawn as "0", because a
 *  zero that renders as a heading reads like a thing that exists. */
export function describeUsage(usage: BlueprintUsage): UsageReport {
  const labels = (rows: Array<{ id: string; name?: string }>): string[] =>
    rows.map((row) => row.name || row.id);
  const every: UsageGroup[] = [
    { key: "schedules", count: usage.schedules.length, items: labels(usage.schedules) },
    { key: "triggers", count: usage.triggers.length, items: labels(usage.triggers) },
    { key: "queueItems", count: usage.queueItems.length, items: labels(usage.queueItems) },
    { key: "agents", count: usage.agents.length, items: [...usage.agents] },
    { key: "actions", count: usage.actions.length, items: [...usage.actions] },
    { key: "skills", count: usage.skills.length, items: [...usage.skills] },
  ];
  const groups = every.filter((group) => group.count > 0);
  return {
    groups,
    loopsStarted: usage.loopsStarted,
    empty: groups.length === 0 && usage.loopsStarted === 0,
  };
}

/** The save answer, said back. `changed:false` is not a failure — the stored
 *  bytes already say what the draft said — and the revision shown is the one in
 *  the response, never a local counter (BP12: no optimistic rows). */
export function saveOutcome(result: { changed: boolean; revision: number }): {
  key: "saved" | "unchanged";
  revision: number;
} {
  return { key: result.changed ? "saved" : "unchanged", revision: result.revision };
}
