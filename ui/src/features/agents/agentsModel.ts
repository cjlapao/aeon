import type { RpcMethods } from "../../proto/contract";

// The page's own vocabulary: the wire's words, the screen's state shape, and the
// defensive read of an answer. Paging, ordering and filtering are the daemon's
// — nothing here sorts, counts pages or trims (PG1/PG2; the ban is asserted by
// agentsRpc.guard.test.ts). The lists copied from the daemon (sorts, sizes,
// fields, avatar words, ledger ops) and the two refusal sentences are pinned
// against `agent_page.go`, `agentmgr.go` and the migrations by that same guard.

/** One row of `agent.rows`, and the read's params/result, from the generated contract. */
export type AgentRow = RpcMethods["agent.rows"]["result"]["agents"][number];
export type AgentRowsParams = RpcMethods["agent.rows"]["params"];
export type AgentRowsResult = RpcMethods["agent.rows"]["result"];

/** One `agent.changes` ledger row. */
export type AgentChangeRow = RpcMethods["agent.changes"]["result"]["changes"][number];

/** `agent.view`'s answer: the whole content row. The draft type, and AR5's subject. */
export type AgentViewResult = RpcMethods["agent.view"]["result"];
/** `agent.edit`'s params. `fields` is a whole row in the generated arm, which is the point. */
export type AgentEditParams = RpcMethods["agent.edit"]["params"];

/** The seven sort names the daemon publishes — `AgentRowBounds.Sorts`' keys. */
export const AGENT_SORTS = ["name", "slug", "version", "internal", "signed", "createdAt", "updatedAt"] as const;
export type AgentSort = (typeof AGENT_SORTS)[number];

/** The order the daemon answers when the request names none, and its direction. */
export const AGENT_DEFAULT_SORT: AgentSort = "name";
export const AGENT_DEFAULT_DIR = "asc" as const;

/**
 * The page sizes the control offers. The daemon answers 20 for an absent
 * `pageSize` and clamps at 100, so nothing past 100 is offered; the kit's own
 * footer seeds its selector with these same three words.
 */
export const AGENT_PAGE_SIZES = [20, 50, 100] as const;
export const AGENT_DEFAULT_PAGE_SIZE = 20;

/** A direction is two words, and the daemon refuses any other one. */
export type AgentDir = "asc" | "desc";

/** The screen's whole paging state. Nothing else about a window is client state. */
export interface AgentWindow {
  page: number;
  pageSize: number;
  sort: AgentSort;
  dir: AgentDir;
  q: string;
}

export const initialAgentWindow: AgentWindow = {
  page: 1,
  pageSize: AGENT_DEFAULT_PAGE_SIZE,
  sort: AGENT_DEFAULT_SORT,
  dir: AGENT_DEFAULT_DIR,
  q: "",
};

/** The wire params for a window: the daemon's five keys, `q` only when there is a term. */
export function agentRowsParams(w: AgentWindow): AgentRowsParams {
  const params: AgentRowsParams = { page: w.page, pageSize: w.pageSize, sort: w.sort, dir: w.dir };
  if (w.q !== "") params.q = w.q;
  return params;
}

/**
 * The answer's window. `rows` and `total` are the answer's own; the other five
 * fields are the ask as the daemon restated it, and they are what the controls
 * are built from.
 */
export interface AgentWindowAnswer {
  rows: AgentRow[];
  page: number;
  pageSize: number;
  total: number;
  sort: string;
  dir: string;
  q: string;
}

/** The answer, read defensively: an unanswered or partial body is an empty window, never a crash. */
export function agentRowsOf(data: AgentRowsResult | undefined): AgentWindowAnswer {
  const rows = Array.isArray(data?.agents) ? data.agents : [];
  return {
    rows,
    page: typeof data?.page === "number" ? data.page : 1,
    pageSize: typeof data?.pageSize === "number" ? data.pageSize : AGENT_DEFAULT_PAGE_SIZE,
    total: typeof data?.total === "number" ? data.total : 0,
    sort: typeof data?.sort === "string" ? data.sort : AGENT_DEFAULT_SORT,
    dir: typeof data?.dir === "string" ? data.dir : AGENT_DEFAULT_DIR,
    q: typeof data?.q === "string" ? data.q : "",
  };
}

/**
 * Whether an answer has caught up with an ask — the screen's only in-flight
 * signal, built entirely from the daemon's echo (`useRpcQuery`'s `loading`
 * covers the first load and nothing after it).
 */
export function sameWindow(asked: AgentWindow, answered: AgentWindowAnswer): boolean {
  return (
    answered.page === asked.page &&
    answered.pageSize === asked.pageSize &&
    answered.sort === asked.sort &&
    answered.dir === asked.dir &&
    answered.q === asked.q
  );
}

/** The ledger's window, same defensive read. */
export function agentChangesOf(data: RpcMethods["agent.changes"]["result"] | undefined): {
  changes: AgentChangeRow[];
  page: number;
  pageSize: number;
  total: number;
  slug: string;
} {
  const changes = Array.isArray(data?.changes) ? data.changes : [];
  return {
    changes,
    page: typeof data?.page === "number" ? data.page : 1,
    pageSize: typeof data?.pageSize === "number" ? data.pageSize : AGENT_DEFAULT_PAGE_SIZE,
    total: typeof data?.total === "number" ? data.total : 0,
    slug: typeof data?.slug === "string" ? data.slug : "",
  };
}

/** A ledger row's agent, or null for the one row that names none (AR1's NULL slug). */
export function changeSubject(slug: string | null | undefined): string | null {
  return typeof slug === "string" && slug !== "" ? slug : null;
}

/** An actor as the ledger names it — `user:1000`, `cli:cli`, `system:…` — the pair, never folded into one word. */
export function actorLabel(kind: string | undefined, id: string | undefined): string {
  const k = typeof kind === "string" && kind !== "" ? kind : "unknown";
  const i = typeof id === "string" && id !== "" ? id : "—";
  return `${k}:${i}`;
}

/** Whether a row may be changed through this page: the daemon refuses `internal` rows (AM4/AM2). */
export function rowReadOnly(row: Pick<AgentRow, "internal">): boolean {
  return row.internal === true;
}

/**
 * The daemon's own two sentences for a first-party row, quoted verbatim from
 * `internal/agentmgr/agentmgr.go` (`%s` stands where the slug goes) — RM9's law
 * that an authority sentence has exactly one definition. The guard test reads
 * that Go file and reddens if either literal drifts.
 */
export const INTERNAL_EDIT_REFUSAL =
  "refused: %s is internal (first-party) — internal agents refuse edits; duplicate it to make an editable copy";
export const INTERNAL_DELETE_REFUSAL =
  "refused: %s is internal (first-party) — internal agents refuse deletion";

/** One of the two sentences with the row's slug in the daemon's own place. */
export function refusalFor(template: string, slug: string): string {
  return template.replace("%s", slug);
}

// ---------------------------------------------------------------------------
// The row a panel holds, and the one write shape it owes (AR5).
//
// `agentmgr.Edit` REPLACES its nine content fields with whatever the `fields`
// blob carries, so the draft a panel edits IS `agent.view`'s answer, `edit`'s
// `fields` is typed as that same whole row, and the only function that builds
// an edit's params copies the draft it was handed. A field that renders without
// joining the draft is a TypeScript error, not a silent blanking.
// ---------------------------------------------------------------------------

/** What a panel edits: `agent.view`'s answer, whole. Not a subset, ever. */
export type AgentDraft = AgentViewResult;

/** The row's string-valued keys — the only ones a text control can hold. */
type StringDraftKey = { [K in keyof AgentDraft]-?: AgentDraft[K] extends string ? K : never }[keyof AgentDraft];
export type AgentStringField = StringDraftKey;

/**
 * The nine fields `agentmgr.Edit` writes, in the order it writes them, with the
 * control each one needs. The guard test parses `Edit`'s assignments out of the
 * Go source and fails this list if the two disagree.
 */
export interface AgentFieldSpec {
  readonly key: StringDraftKey;
  readonly labelKey: string;
  readonly hintKey?: string;
  readonly control: "text" | "textarea" | "select" | "tools" | "handoff";
  /** Only for `select`: the closed vocabulary, in the order it is offered. */
  readonly options?: readonly string[];
  readonly required?: boolean;
}

/** The avatar vocabularies are the database's CHECKs (migration `20261030130000_agent.sql`), empty included. */
export const AGENT_AVATAR_TYPES = ["", "minion", "cat"] as const;
export const AGENT_AVATAR_MOODS = ["", "idle", "hi", "thinking", "working", "stuck", "done"] as const;

export const AGENT_WRITABLE_FIELDS: readonly AgentFieldSpec[] = [
  { key: "name", labelKey: "agents.field.name", hintKey: "agents.field.nameHint", control: "text", required: true },
  { key: "description", labelKey: "agents.field.description", control: "text" },
  { key: "functionality", labelKey: "agents.field.functionality", control: "text" },
  {
    key: "prompt",
    labelKey: "agents.field.prompt",
    hintKey: "agents.field.promptHint",
    control: "textarea",
  },
  { key: "skills", labelKey: "agents.field.skills", hintKey: "agents.field.skillsHint", control: "textarea" },
  { key: "metadata", labelKey: "agents.field.metadata", hintKey: "agents.field.metadataHint", control: "textarea" },
  {
    key: "avatarType",
    labelKey: "agents.field.avatarType",
    hintKey: "agents.field.avatarHint",
    control: "select",
    options: AGENT_AVATAR_TYPES,
  },
  {
    key: "avatarColor",
    labelKey: "agents.field.avatarColor",
    hintKey: "agents.field.avatarColorHint",
    control: "text",
  },
  {
    key: "avatarMood",
    labelKey: "agents.field.avatarMood",
    hintKey: "agents.field.avatarHint",
    control: "select",
    options: AGENT_AVATAR_MOODS,
  },
  // The tenth: what the agent may ask for. It is drawn by the tools picker (kits first), not by a
  // text control; the form skips it, and the draft carries it as the JSON list the row stores.
  { key: "tools", labelKey: "agents.field.tools", hintKey: "agents.field.toolsHint", control: "tools" },
  // The eleventh: what the agent must report when it finishes. Drawn by the handoff builder, like the
  // tools picker; the draft carries it as the JSON object the row stores.
  { key: "handoff", labelKey: "agents.field.handoff", hintKey: "agents.field.handoffHint", control: "handoff" },
];

/** The nine keys, for the lookups the form, the change-count and the guard all want. */
export const AGENT_WRITABLE_KEYS: readonly AgentStringField[] = AGENT_WRITABLE_FIELDS.map(
  (field) => field.key,
);

/**
 * The nine specs, in the nine's own order, restricted to a tab's keys. The panel's
 * tabs split the same nine controls across four surfaces; this is the one way to
 * slice them, so a tab can never invent a field the door does not write.
 */
export function agentFieldsFor(keys: readonly AgentStringField[]): readonly AgentFieldSpec[] {
  const picked: AgentFieldSpec[] = [];
  for (const spec of AGENT_WRITABLE_FIELDS) {
    for (const key of keys) {
      if (spec.key === key) {
        picked.push(spec);
        break;
      }
    }
  }
  return picked;
}

/** The basics tab's six; the other three writable keys each own a tab. */
export const AGENT_OVERVIEW_KEYS: readonly AgentStringField[] = [
  "name",
  "description",
  "functionality",
  "avatarType",
  "avatarColor",
  "avatarMood",
];

/** `agent.view`'s keys minus the ten the door writes; the guard proves the two sets partition the answer. */
export const AGENT_READ_ONLY_FIELDS: readonly { key: keyof AgentDraft; labelKey: string }[] = [
  { key: "slug", labelKey: "agents.readonly.slug" },
  { key: "id", labelKey: "agents.readonly.id" },
  { key: "version", labelKey: "agents.readonly.version" },
  { key: "temperature", labelKey: "agents.readonly.temperature" },
  { key: "policyAgent", labelKey: "agents.readonly.policyAgent" },
  { key: "bodyHash", labelKey: "agents.readonly.bodyHash" },
  { key: "origin", labelKey: "agents.readonly.origin" },
  { key: "internal", labelKey: "agents.readonly.internal" },
  { key: "signed", labelKey: "agents.readonly.signed" },
  { key: "signature", labelKey: "agents.readonly.signature" },
  { key: "certificate", labelKey: "agents.readonly.certificate" },
  { key: "createdAt", labelKey: "agents.readonly.createdAt" },
  { key: "updatedAt", labelKey: "agents.readonly.updatedAt" },
];

/** The ledger window's page size: the daemon's own default for this read. */
export const AGENT_CHANGES_PAGE_SIZE = 20;

/**
 * An `agent.view` answer, read defensively. A body with no slug is not a row:
 * the panel then shows its own no-row state rather than opening an editor over
 * half an object.
 */
export function agentViewOf(data: AgentViewResult | undefined): AgentDraft | null {
  if (!data || typeof data.slug !== "string" || data.slug === "") return null;
  return data;
}

/** What a person has typed, keyed as the row keys it. Absent means untouched; a draft is deliberately sparse. */
export type AgentTyping = Partial<Record<AgentStringField, string>>;

/** AR5's defence in one spread: the row as loaded, with the person's typing on top. */
export function agentDraftUnder(row: AgentDraft, typing: AgentTyping): AgentDraft {
  return { ...row, ...typing };
}

/** The form's display projection: the row's own values with the typing over them. */
export function agentFormValues(draft: AgentDraft, keys: readonly AgentStringField[]): AgentTyping {
  const values: AgentTyping = {};
  for (const key of keys) values[key] = draft[key];
  return values;
}

/** What the new-agent panel collects, before it becomes the create door's ask. */
export interface AgentCreateDraft {
  name: string;
  description: string;
  prompt: string;
  /** Tech tags from the closed vocabulary. */
  tech: readonly string[];
  /** Registered tool names. */
  tools: readonly string[];
  /** What it must report when it finishes: field name → purpose, a "?" on the name marking it optional. */
  handoff?: Readonly<Record<string, string>>;
  /** The minion's colour name; empty leaves the daemon's default. */
  avatarColor: string;
  /** Which minion character; empty leaves it to be drawn from the name. */
  avatarMinion: string;
}

/**
 * The create door's ask. Only what the person filled in is sent: an empty prompt, no tech, no
 * tools and no colour are the daemon's defaults, so they are left out rather than sent empty.
 * An empty name is refused by the daemon, not by a dead button — its sentence teaches the rule.
 */
export function agentCreateParams(draft: AgentCreateDraft): RpcMethods["agent.create"]["params"] {
  const params: RpcMethods["agent.create"]["params"] = { name: draft.name, description: draft.description };
  if (draft.prompt.trim() !== "") params.prompt = draft.prompt;
  if (draft.tech.length > 0) params.tech = [...draft.tech];
  if (draft.tools.length > 0) params.tools = [...draft.tools];
  if (draft.handoff !== undefined && Object.keys(draft.handoff).length > 0) params.handoff = { ...draft.handoff } as unknown as typeof params.handoff; // the generated type is the fixture's example; the real shape is an open map
  if (draft.avatarColor !== "") params.avatarColor = draft.avatarColor;
  if (draft.avatarMinion !== "") params.avatarMinion = draft.avatarMinion;
  return params;
}

/** The tool names an agent row holds: its `tools` column is a JSON list; anything else holds none. */
export function agentToolList(tools: string | undefined): string[] {
  if (tools === undefined || tools.trim() === "") return [];
  try {
    const parsed: unknown = JSON.parse(tools);
    const names: string[] = [];
    if (Array.isArray(parsed)) for (const name of parsed) if (typeof name === "string") names.push(name);
    return names;
  } catch {
    return [];
  }
}

/** The row's `tools` column for a chosen set: the JSON list, in the order the picker holds it. */
export function toolListJson(names: readonly string[]): string {
  return JSON.stringify([...names]);
}

/** The whole-row edit — AR5's only door out: not the fields that differ, and nothing missing. */
export function agentEditParams(slug: string, draft: AgentDraft): AgentEditParams {
  return { slug, fields: { ...draft } };
}

/** Which of the nine the person actually changed, against the row as loaded (a cleared field IS a change). */
export function agentChangedFields(draft: AgentDraft, loaded: AgentDraft): string[] {
  const changed: string[] = [];
  for (const key of AGENT_WRITABLE_KEYS) {
    const now = draft[key];
    const was = loaded[key];
    if (typeof now === "string" && typeof was === "string" && now !== was) changed.push(key);
  }
  return changed;
}

/** The ledger's ask: the row's slug, or no key at all for everything (the absence IS the second state). */
export function agentChangesParams(slug: string | null, page: number): RpcMethods["agent.changes"]["params"] {
  const params: RpcMethods["agent.changes"]["params"] = { page, pageSize: AGENT_CHANGES_PAGE_SIZE };
  if (slug !== null && slug !== "") params.slug = slug;
  return params;
}

/** The seven ops the ledger's CHECK admits, in the migration's own order. */
export const AGENT_CHANGE_OPS = ["create", "duplicate", "edit", "delete", "import", "migrate", "seed"] as const;

/** The words this page paints with (a subset of the kit's `TrueColor`, kept local so the model stays component-free). */
export type AgentTone = "slate" | "blue" | "green" | "amber" | "red" | "violet";

/** The tone a ledger op carries; the word shown is always the ledger's own. */
export function changeOpTone(op: string): AgentTone {
  switch (op) {
    case "create":
      return "green";
    case "edit":
      return "blue";
    case "delete":
      return "red";
    case "duplicate":
      return "violet";
    case "import":
      return "amber";
    case "migrate":
      return "slate";
    default:
      return "slate";
  }
}

/** The actor's kind, as a tone — the three kinds the CHECK admits are three colours, and anything else reads apart too. */
export function actorKindTone(kind: string | undefined): AgentTone {
  switch (kind) {
    case "user":
      return "blue";
    case "cli":
      return "slate";
    case "system":
      return "violet";
    default:
      return "amber";
  }
}

/** A summary's proposal id, where it carries one (`import x v1 (created, proposal 12)`). Extracted, never rewritten. */
export function changeProposalId(summary: string | undefined): string | null {
  if (typeof summary !== "string") return null;
  const found = /\bproposal ([^\s)]+)/.exec(summary);
  return found?.[1] ?? null;
}

/** The tone an import word carries: the three words AM3 permits, as `store`'s `ImportResult.String()` says them, plus one for the unknown. */
export function importWordTone(word: string): "green" | "blue" | "amber" | "slate" {
  switch (word) {
    case "created":
      return "green";
    case "updated":
      return "blue";
    case "no-op (same version already stored)":
      return "amber";
    default:
      return "slate";
  }
}

/** The `agent.migrate` answer, said back from the response rather than counted here. */
export function migrateCreated(answer: RpcMethods["agent.migrate"]["result"] | undefined): number | null {
  return typeof answer?.created === "number" ? answer.created : null;
}

/**
 * The row shape the detail renderer prints: the keys every content answer
 * carries — `agent.view`'s whole row and the create/duplicate/import answers
 * alike, which are the same row minus the five dispatch columns.
 */
export type AgentDetailRow = Pick<
  AgentDraft,
  | "name"
  | "slug"
  | "version"
  | "signed"
  | "internal"
  | "certificate"
  | "signature"
  | "description"
  | "functionality"
  | "prompt"
  | "skills"
  | "metadata"
  | "avatarType"
  | "avatarColor"
  | "avatarMood"
>;

/** The tech tags in an agent's metadata blob (they ride it under "tech"); none when the blob is empty or not JSON. */
export function agentTech(metadata: string | undefined): string[] {
  if (!metadata || metadata.trim() === "") return [];
  try {
    const blob = JSON.parse(metadata) as { tech?: unknown };
    const tags: string[] = [];
    if (Array.isArray(blob.tech)) for (const tag of blob.tech) if (typeof tag === "string") tags.push(tag);
    return tags;
  } catch {
    return [];
  }
}

/** The metadata blob with its tech tags set, every other key kept. A blob that is not a JSON object is left alone: the person is mid-edit in the raw text. */
export function withAgentTech(metadata: string | undefined, tags: readonly string[]): string {
  let blob: Record<string, unknown> = {};
  if (metadata && metadata.trim() !== "") {
    try {
      const parsed: unknown = JSON.parse(metadata);
      if (parsed === null || typeof parsed !== "object" || Array.isArray(parsed)) return metadata;
      blob = parsed as Record<string, unknown>;
    } catch {
      return metadata;
    }
  }
  if (tags.length === 0) delete blob.tech;
  else blob.tech = [...tags];
  return Object.keys(blob).length === 0 ? "" : JSON.stringify(blob);
}

/** The minion characters an agent may wear: the app's own built-ins, in the order a click goes through them. */
export const AGENT_MINIONS = ["pip", "bop", "noodle"] as const;

/** Which minion an agent wears (it rides the metadata under "minion"); empty when it has not chosen — it is then drawn from its name. */
export function agentMinion(metadata: string | undefined): string {
  if (!metadata || metadata.trim() === "") return "";
  try {
    const blob = JSON.parse(metadata) as { minion?: unknown };
    return typeof blob.minion === "string" && (AGENT_MINIONS as readonly string[]).includes(blob.minion) ? blob.minion : "";
  } catch {
    return "";
  }
}

/** The metadata blob with its minion set (empty removes it), every other key kept. A blob that is not a JSON object is left alone. */
export function withAgentMinion(metadata: string | undefined, minion: string): string {
  let blob: Record<string, unknown> = {};
  if (metadata && metadata.trim() !== "") {
    try {
      const parsed: unknown = JSON.parse(metadata);
      if (parsed === null || typeof parsed !== "object" || Array.isArray(parsed)) return metadata;
      blob = parsed as Record<string, unknown>;
    } catch {
      return metadata;
    }
  }
  if (minion === "") delete blob.minion;
  else blob.minion = minion;
  return Object.keys(blob).length === 0 ? "" : JSON.stringify(blob);
}

/** The next minion after this one, going round: auto (drawn from the name) → pip → bop → noodle → auto. */
export function nextMinion(current: string): string {
  const at = (AGENT_MINIONS as readonly string[]).indexOf(current);
  if (at === -1) return AGENT_MINIONS[0];
  return at + 1 < AGENT_MINIONS.length ? (AGENT_MINIONS[at + 1] as string) : "";
}

/** One declared field of a handoff, as the builder holds it: a name, what it is for, and whether the agent may leave it out. */
export interface HandoffRow {
  name: string;
  purpose: string;
  optional: boolean;
}

/** The rows of a row's `handoff` column (a JSON object of field → purpose, a "?" on the name marking it optional); anything else holds none. */
export function handoffRows(handoff: string | undefined): HandoffRow[] {
  if (handoff === undefined || handoff.trim() === "") return [];
  try {
    const parsed: unknown = JSON.parse(handoff);
    const rows: HandoffRow[] = [];
    if (parsed !== null && typeof parsed === "object" && !Array.isArray(parsed)) {
      for (const [key, purpose] of Object.entries(parsed as Record<string, unknown>)) {
        if (typeof purpose !== "string") continue;
        rows.push({ name: key.replace(/\?$/, ""), purpose, optional: key.endsWith("?") });
      }
    }
    return rows;
  } catch {
    return [];
  }
}

/** The map a set of rows stands for, as the create door takes it. A row with no name is not yet a field and is left out. */
export function handoffMap(rows: readonly HandoffRow[]): Record<string, string> {
  const out: Record<string, string> = {};
  for (const row of rows) {
    const name = row.name.trim();
    if (name !== "") out[row.optional ? `${name}?` : name] = row.purpose;
  }
  return out;
}

/** The row's `handoff` column for a set of rows: "{}" is the explicit empty choice (the daemon keeps an empty string as it was). */
export function handoffJson(rows: readonly HandoffRow[]): string {
  return JSON.stringify(handoffMap(rows));
}
