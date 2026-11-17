/**
 * roadmapModel — the pure half of the S50 roadmap management surface (T7).
 *
 * Everything here is a function over wire data: no fetching, no JSX, no
 * clock of its own. The rules that shape it:
 *
 *  • The panel never describes an item, composes an authority sentence or
 *    decides whether a path may be edited itself (RM6/RM7): those sentences
 *    come from the daemon (`roadmap.board`'s and `roadmap.show`'s `describe`,
 *    the blocker sentence, `roadmap.vocabulary`'s dispatchStatement,
 *    pathEditableStatuses and limits) and this file only decides WHICH
 *    daemon-provided word or field to show where.
 *  • The generated contract is fixture-derived and narrower than the wire
 *    on `roadmap.board` (fixture-coarse `Array<unknown>` rows),
 *    `roadmap.history`, `roadmap.health`, the update/ideate/materialize
 *    params (`omitempty` on each field in the Go structs) and
 *    `proposal.list`'s status filter. The same documented-widening idiom
 *    `automationModel.ts` (S49) and `gatesRpc.ts` (S57) use: types are
 *    widened HERE, the assertion lives at the rpc boundary, documented.
 *  • No status list, no limit, no authority sentence is hardcoded. A state
 *    word that arrives is a state word that renders — `healthTone` carries
 *    the one local decision this file makes (a colour per state word), and
 *    the word itself always rides next to the dot.
 */

import type { RpcMethods } from "../../proto/contract";

// ── wire types (widened where the fixture-derived contract is narrower) ───

/** One item, exactly as `RoadmapItemView` (Go) emits it: `priorityState`
 *  is ALWAYS a word, the estimate is range-and-basis or absent together,
 *  paths and loop slug are pointers that ride only when they exist. Taken
 *  from `roadmap.show`'s typed answer — the one door whose fixture carried
 *  the full struct; every other door's rows are this same struct. */
export type ItemView = RpcMethods["roadmap.show"]["result"]["item"] & {
  /** First time the item ran / when it finished (done, else cancelled), from the daemon's audit; absent until it happens. */
  startedAt?: string;
  finishedAt?: string;
  /** The upstream items this one waits for. */
  dependencies?: Array<{ id: number; status: string; title: string }>;
};

/** `roadmap.show`'s answer — typed by the contract already (the fixture
 *  carried the whole struct); re-exported so components never re-widen. */
export type ShowResult = RpcMethods["roadmap.show"]["result"];

/** One board row: the item, the daemon's `Describe` for it, readiness with
 *  its blocker sentence, and the pinned loop when there is one. The board
 *  fixture answered empty arrays, so the generated type says
 *  `Array<unknown>` — this is the documented widening over the Go struct
 *  `RoadmapBoardRowView` (item, describe, ready, blocker omitempty, loop
 *  omitempty). Nothing here may invent a field that struct does not carry. */
export interface BoardRow {
  item: ItemView;
  describe: string;
  ready: boolean;
  blocker?: string;
  loop?: { slug: string; status: string };
}

export interface BoardResult {
  rows: BoardRow[];
  groups: Record<string, number>;
}

/** `roadmap.history`'s rows (fixture-coarse): one audit line. `summary` is
 *  the daemon's sentence, quoted verbatim wherever it renders. */
export interface HistoryRow {
  id: number;
  itemId: number;
  op: string;
  summary: string;
  actorKind: string;
  actorId: string;
  occurredAt: string;
}

export interface HistoryResult {
  changes: HistoryRow[];
}

/** `roadmap.health`'s per-setting row. `state` is one of the daemon's four
 *  words — ok | missing | invalid | unverifiable — and this file never
 *  renames one; `reportCounts` rides only when a report ran (omitempty). */
export interface HealthSetting {
  key: string;
  name: string;
  state: string;
  reportCounts?: { valid: number; invalid: number; unverifiable: number };
}

export interface HealthResult {
  settings: HealthSetting[];
  evaluatedAt: string;
}

/** `roadmap.update`'s wire params (Go `RoadmapUpdateParams`): only touched
 *  fields travel; `clearEpic` is a separate intention from `epic` ("unset"
 *  and "do not change" are different words, one empty string cannot say
 *  both). The contract's params are the fixture's narrow shape. */
export interface UpdateParams {
  id: number;
  title?: string;
  epic?: string;
  briefPath?: string;
  planPath?: string;
  clearEpic?: boolean;
}

export interface UpdateResult {
  item: ItemView;
  changed: boolean;
}

export type Vocabulary = RpcMethods["roadmap.vocabulary"]["result"];
export type StatusEntry = Vocabulary["statuses"][number];

/** The two blueprint settings are named HERE because the panel lists rows,
 *  not enumerates settings; the daemon holds the registry (`settings.list`
 *  is S44's door and the Settings plane owns enumeration). If a third
 *  dispatch setting ever exists, the daemon's registry says so first. */
export const DISPATCH_SETTING_KEYS = ["roadmap.ideationBlueprint", "roadmap.planBlueprint"] as const;

/** One proposal row, read defensively (the contract's rows are one
 *  fixture's shape with policy keys). The strip needs exactly four fields
 *  plus a payload it reads loosely and never rewrites. */
export interface IdeaProposal {
  id: number;
  kind: string;
  status: string;
  createdByKind: string;
  createdById: string;
  createdAt: string;
  payload: Record<string, unknown>;
}

export const PROPOSAL_KIND_ROADMAP_IDEA = "roadmap_idea";

// ── board filters ───────────────────────────────────────────────────────────

export interface FilterDraft {
  componentRoot: string;
  status: string; // "" = the daemon's own "all"
  epic: string;
}

export function emptyFilterDraft(): FilterDraft {
  return { componentRoot: "", status: "", epic: "" };
}

/** Filters travel as `roadmap.board` PARAMS — the daemon filters, the UI
 *  never sweeps a fetched list (RM6). The root is what makes the call
 *  possible at all: an unnamed root answers nothing (P3's shape — the
 *  panel asks for one nameable thing at a time, mirroring the bench page's
 *  workspace-root discipline). */
export function boardParams(f: FilterDraft): Record<string, unknown> {
  const params: Record<string, unknown> = { componentRoot: f.componentRoot.trim() };
  if (f.status !== "") params.status = f.status;
  if (f.epic.trim() !== "") params.epic = f.epic.trim();
  return params;
}

export function boardIsLoadable(f: FilterDraft): boolean {
  return f.componentRoot.trim() !== "";
}

// ── dispatch ────────────────────────────────────────────────────────────────

/** The daemon's authority statement names its subject with an `<item>`
 *  slot; the person's confirmation fills the slot with the daemon's own
 *  describe sentence for THAT item. Substituting is the only edit this
 *  surface ever performs on daemon text — and it replaces, never adds
 *  words around it. */
export const DISPATCH_ITEM_SLOT = "<item>";

export function filledDispatchStatement(statement: string | undefined, describe: string | undefined): string {
  if (statement === undefined) return "";
  if (describe === undefined) return statement;
  return statement.replaceAll(DISPATCH_ITEM_SLOT, describe);
}

// ── health ──────────────────────────────────────────────────────────────────

/** One local tone map, keyed by the daemon's four state words. The colour
 *  is the UI's; the WORD is the daemon's and always renders as text next
 *  to the dot — a state word is never rewritten into another (RM6: health
 *  is advice, and advice keeps its own name). */
export function healthTone(state: string | undefined): "green" | "amber" | "red" | "slate" {
  switch (state) {
    case "ok":
      return "green";
    case "missing":
      return "amber";
    case "invalid":
      return "red";
    case "unverifiable":
      return "slate";
    default:
      // An unknown word is its own honest dot, not a silence.
      return "slate";
  }
}

export function settingHealth(health: HealthResult | undefined, key: string): HealthSetting | undefined {
  return health?.settings?.find((s) => s.key === key);
}

// ── history actors ──────────────────────────────────────────────────────────

/** A system row (the reactor's transitions, the plan-host's stamps) is a
 *  different KIND of line than a person's — visibly, not by hiding it.
 *  Person rows keep the id the audit carries (`user:…`, `cli:…`); nobody
 *  here renames an actor. */
export function isSystemActor(row: Pick<HistoryRow, "actorKind">): boolean {
  return row.actorKind === "system";
}

// ── editability ─────────────────────────────────────────────────────────────

/** The path-editability window is the daemon's array (RM7's D9 rule): the
 *  panel reads `pathEditableStatuses` and hides the path fields when the
 *  item's status is outside it — it never re-derives the window from the
 *  status name or the PathRule sentence. */
export function pathsEditableFor(vocabulary: Vocabulary | undefined, status: string | undefined): boolean | undefined {
  if (vocabulary?.pathEditableStatuses === undefined || status === undefined) return undefined;
  return vocabulary.pathEditableStatuses.includes(status);
}

// ── proposals ───────────────────────────────────────────────────────────────

/** The strip lists `roadmap_idea` rows only. `proposal.list` filters by
 *  STATUS, not kind — the door has no kind filter (measured against
 *  `proposalListParams`); the kind read is honest client-side narrowing of
 *  the daemon's own listing, documented at the rpc boundary. */
export function roadmapIdeaProposals(rows: unknown): IdeaProposal[] {
  if (!Array.isArray(rows)) return [];
  const out: IdeaProposal[] = [];
  for (const raw of rows) {
    if (typeof raw !== "object" || raw === null) continue;
    const row = raw as Partial<IdeaProposal>;
    if (row.kind !== PROPOSAL_KIND_ROADMAP_IDEA) continue;
    out.push({
      id: row.id ?? 0,
      kind: row.kind,
      status: row.status ?? "",
      createdByKind: row.createdByKind ?? "",
      createdById: row.createdById ?? "",
      createdAt: row.createdAt ?? "",
      payload: typeof row.payload === "object" && row.payload !== null ? (row.payload as Record<string, unknown>) : {},
    });
  }
  return out;
}

/** The proposal's own words, read loosely: title/epic/note are the agent's
 *  payload text and render verbatim; an absent field renders as absent. */
export function ideaField(p: IdeaProposal, field: "title" | "epic" | "note"): string {
  const value = p.payload[field];
  return typeof value === "string" ? value : "";
}

// ── defensive board read ────────────────────────────────────────────────────

/** The contract types the board answer's rows and groups as
 *  `Array<unknown>`/`Record<string,never>` (the fixture answered empty).
 *  This is the ONE place the widening assertion runs; components receive
 *  the real shape or nothing at all — a bare `{}` answer survives as no
 *  rows, never a crash and never an invented row. */
export function boardShape(answer: unknown): BoardResult {
  if (typeof answer !== "object" || answer === null) return { rows: [], groups: {} };
  const raw = answer as { rows?: unknown; groups?: unknown };
  const rows: BoardRow[] = [];
  if (Array.isArray(raw.rows)) {
    for (const candidate of raw.rows) {
      if (typeof candidate !== "object" || candidate === null) continue;
      const row = candidate as Partial<BoardRow> & { item?: unknown };
      if (typeof row.item !== "object" || row.item === null) continue;
      rows.push({
        item: row.item as ItemView,
        describe: typeof row.describe === "string" ? row.describe : "",
        ready: row.ready === true,
        ...(typeof row.blocker === "string" && row.blocker !== "" ? { blocker: row.blocker } : {}),
        ...(typeof row.loop === "object" && row.loop !== null ? { loop: row.loop as { slug: string; status: string } } : {}),
      });
    }
  }
  const groups: Record<string, number> = {};
  if (typeof raw.groups === "object" && raw.groups !== null) {
    for (const [key, value] of Object.entries(raw.groups as Record<string, unknown>)) {
      if (typeof value === "number") groups[key] = value;
    }
  }
  return { rows, groups };
}

export function historyShape(answer: unknown): HistoryRow[] {
  if (typeof answer !== "object" || answer === null) return [];
  const raw = (answer as { changes?: unknown }).changes;
  if (!Array.isArray(raw)) return [];
  const out: HistoryRow[] = [];
  for (const candidate of raw) {
    if (typeof candidate !== "object" || candidate === null) continue;
    const row = candidate as Partial<HistoryRow>;
    if (typeof row.id !== "number") continue;
    out.push({
      id: row.id,
      itemId: row.itemId ?? 0,
      op: row.op ?? "",
      summary: row.summary ?? "",
      actorKind: row.actorKind ?? "",
      actorId: row.actorId ?? "",
      occurredAt: row.occurredAt ?? "",
    });
  }
  return out;
}

export function healthShape(answer: unknown): HealthResult {
  const empty: HealthResult = { settings: [], evaluatedAt: "" };
  if (typeof answer !== "object" || answer === null) return empty;
  const raw = answer as { settings?: unknown; evaluatedAt?: unknown };
  const settings: HealthSetting[] = [];
  if (Array.isArray(raw.settings)) {
    for (const candidate of raw.settings) {
      if (typeof candidate !== "object" || candidate === null) continue;
      const row = candidate as Partial<HealthSetting>;
      if (typeof row.key !== "string") continue;
      settings.push({
        key: row.key,
        name: typeof row.name === "string" ? row.name : "",
        state: typeof row.state === "string" ? row.state : "",
        ...(typeof row.reportCounts === "object" && row.reportCounts !== null
          ? { reportCounts: row.reportCounts as HealthSetting["reportCounts"] }
          : {}),
      });
    }
  }
  return { settings, evaluatedAt: typeof raw.evaluatedAt === "string" ? raw.evaluatedAt : "" };
}
