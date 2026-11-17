/**
 * automationModel — the pure half of the S49 Automation section (T9).
 *
 * Everything here is a function over wire data: no fetching, no JSX, no
 * clock of its own. The rules that shape it:
 *
 *  • The section never describes a trigger, parses a cron, matches an event
 *    or writes authority text itself (AN10/AN11/AN13): those sentences come
 *    from the daemon (`automation.show`'s description, `automation.preview`'s
 *    instants, `automation.test`'s per-key answers, `automation.vocabulary`'s
 *    authorityStatement) and this file only decides which daemon-provided
 *    WORD to show where.
 *  • The generated contract is fixture-derived and narrower than the wire on
 *    the unified list's filters and the show result's event fields
 *    (`omitempty` on each in `AutomationListParams` / the views). The same
 *    documented-widening idiom `environmentsModel.ts` and `gatesRpc.ts` use.
 */

import type { RpcMethods } from "../../../proto/contract";

// ── wire types (widened where the fixture-derived contract is narrower) ───

/** One unified-list row. The fixture only showed the schedule shape, so the
 *  event/webhook fields are added here exactly as `AutomationView` (Go) emits
 *  them; nothing in the UI may invent a field not on that struct. */
export type AutomationRow = RpcMethods["automation.list"]["result"]["automations"][number] & {
  paused?: boolean;
  archived?: boolean;
  archivedReason?: string;
  disabledReason?: string;
  secretEnv?: string;
  maxCostPerDayUsd?: number;
  rootId?: string;
  componentId?: string;
  window?: { start?: string; end?: string; days?: string };
};

export type AutomationList = RpcMethods["automation.list"]["result"] & {
  /** The two honest empties (AN5): the daemon adds `statement` to an empty
   *  answer; no fixture carried one, so the generated type misses it. */
  statement?: string;
};
export type ShowResult = RpcMethods["automation.show"]["result"];
export type ShowAutomation = ShowResult["automation"] & Partial<AutomationRow> & {
  /** The plain-words sentence `automationview.Describe` wrote. No conformance
   *  fixture carried it, so the generated type misses it — documented
   *  widening, quoted verbatim in the drawer (AN11), never re-derived. */
  description?: string;
};

/** Health as `automation.health` answers it per trigger. `reasons` widens the
 *  fixture's `Array<unknown>` spelling the show shape shows. */
export type HealthView = RpcMethods["automation.health"]["result"]["health"][number] & {
  reasons?: string[];
};

export type FireRow = RpcMethods["automation.fires"]["result"]["fires"][number];
export type FireCounts = RpcMethods["automation.fires"]["result"]["counts"];
export type RunRow = RpcMethods["automation.runs"]["result"]["loops"][number];
export type ChangeRow = RpcMethods["automation.changes"]["result"]["changes"][number];
export type Vocabulary = RpcMethods["automation.vocabulary"]["result"];
export type EventEntry = Vocabulary["events"][number];
export type PreviewInstant = RpcMethods["automation.preview"]["result"]["instants"][number];
export type TestAnswer = RpcMethods["automation.test"]["result"];
export type DryRunAnswer = RpcMethods["automation.dryrun"]["result"];
export type BlueprintRow = RpcMethods["blueprint.list"]["result"]["blueprints"][number];
export type ProposalRow = RpcMethods["proposal.list"]["result"]["proposals"][number];

/** The store-wide list answer plus the two honest empties, kept distinct:
 *  `statement` is only ever the daemon's own sentence (AN5), never a client
 *  invention — `undefined` means "not empty, or unknown". */
export type EmptyStatement = string | undefined;

export const rowKey = (source: string, id: string): string => `${source}:${id}`;

export const healthKey = (h: HealthView): string => rowKey(h.source, h.id);

// ── state words ────────────────────────────────────────────────────────────

/**
 * The state word for one row. Four distinct words because the states are
 * four distinct facts: an archived row is not "disabled", a paused schedule
 * is not "off" (AN6 keeps archive visible here, engine-invisible elsewhere).
 * `word` is an enum key (i18n-ised at the call site); `tone` drives the pill
 * colour — slate is rest, amber needs attention, blue is armed, green fires.
 */
export type StateWord = "enabled" | "disabled" | "paused" | "archived";

export function stateWord(row: Pick<AutomationRow, "enabled" | "paused" | "archived">): StateWord {
  if (row.archived) return "archived";
  if (row.paused) return "paused";
  return row.enabled ? "enabled" : "disabled";
}

export const stateWordTone: Record<StateWord, "green" | "slate" | "amber" | "blue"> = {
  enabled: "green",
  disabled: "slate",
  paused: "amber",
  archived: "slate",
};

// ── outcome pills (AN16: five outcomes, never merged) ─────────────────────

/**
 * The five outcomes as the seam names them. This set is pinned against the
 * daemon's `automation.vocabulary` answer by the guard test — the words here
 * only choose iconography and tone, never meaning, and `could-not-say` keeps
 * its own pill: it is NOT a skip and must never render as one.
 */
export type Outcome = "started" | "refused" | "limited" | "skipped" | "could-not-say";

const OUTCOME_WORDS: Outcome[] = ["started", "refused", "limited", "skipped", "could-not-say"];

export function isOutcome(word: string): word is Outcome {
  return (OUTCOME_WORDS as string[]).includes(word);
}

export interface OutcomePill {
  tone: "green" | "red" | "amber" | "slate" | "purple";
  /** The i18n key suffix; `settings.automation.outcome.<word>`. */
  key: Outcome;
}

export function outcomePill(word: string): OutcomePill {
  switch (word) {
    case "started": return { tone: "green", key: "started" };
    case "refused": return { tone: "red", key: "refused" };
    case "limited": return { tone: "amber", key: "limited" };
    case "skipped": return { tone: "slate", key: "skipped" };
    default:
      // An unrecognised word reads as could-not-say in colour, but the pill
      // still shows the daemon's word verbatim — the caller passes the wire
      // string to the Pill; guessing here would be a sixth outcome.
      return { tone: "purple", key: "could-not-say" };
  }
}

// ── health chips ───────────────────────────────────────────────────────────

export type BlueprintState = "ok" | "missing" | "invalid" | "unverifiable";

export function blueprintState(state: string | undefined): BlueprintState {
  return state === "ok" || state === "missing" || state === "invalid" ? state : "unverifiable";
}

export interface HealthChip {
  /** `settings.automation.health.<key>`, with the daemon's word/state after. */
  key: "blueprint" | "webhook" | "workspace" | "scope";
  /** For blueprint: the state; for webhook: set|unset; for exists: exists|absent. */
  word: string;
  tone: "green" | "red" | "amber";
}

/**
 * The health chips for one trigger. `describeHealth` NEVER receives or emits
 * an env value — the webhook chip names the variable and whether it is set,
 * because the wire type has only `envName` and `set` (C17; a value has no
 * field to travel in). `undefined` health means no answer yet: render
 * nothing, do not guess `unverifiable` (V13 — absence is not a state).
 */
export function describeHealth(h: HealthView | undefined): HealthChip[] {
  if (!h) return [];
  const chips: HealthChip[] = [];
  const bp = blueprintState(h.blueprint?.state);
  chips.push({
    key: "blueprint",
    word: bp,
    tone: bp === "ok" ? "green" : bp === "unverifiable" ? "amber" : "red",
  });
  if (h.webhook) {
    // NAME + presence on the chip face itself (C17): the wire carries
    // `envName` + `set` and nothing else — a value has no field to travel in.
    chips.push({ key: "webhook", word: `${h.webhook.envName} ${h.webhook.set ? "set" : "unset"}`, tone: h.webhook.set ? "green" : "red" });
  }
  chips.push({ key: "workspace", word: h.workspace?.exists ? "exists" : "absent", tone: h.workspace?.exists ? "green" : "red" });
  chips.push({ key: "scope", word: h.scope?.exists ? "exists" : "absent", tone: h.scope?.exists ? "green" : "red" });
  return chips;
}

/** The row the list keeps: an ENABLED automation the daemon says cannot fire.
 *  Disabled ones are honestly not firing — that is the person's choice, not
 *  attention (AN6: archive reads as paused here, and a paused schedule is
 *  also a choice, not an alarm). */
export function needsAttention(
  h: HealthView | undefined,
  row: Pick<AutomationRow, "source" | "id" | "enabled" | "paused" | "archived">,
): boolean {
  if (!h) return false;
  return row.enabled && !row.paused && !row.archived && !h.canFire;
}

export function needsAttentionCount(
  rows: Array<Pick<AutomationRow, "source" | "id" | "enabled" | "paused" | "archived">>,
  health: Map<string, HealthView>,
): number {
  let n = 0;
  for (const r of rows) {
    if (needsAttention(health.get(rowKey(r.source, r.id)), r)) n += 1;
  }
  return n;
}

// ── match keys (AN9's event grammar) ──────────────────────────────────────

/**
 * The keys a match builder may offer for one event — the vocabulary's own
 * `applicableKeys`, never the full set (offering an inapplicable key is a
 * match that can never fire). Empty event → empty list: the builder shows a
 * key only once its event is chosen.
 */
export function applicableKeys(event: string, vocabulary: Vocabulary | undefined): string[] {
  if (!vocabulary || event === "") return [];
  const entry = vocabulary.events.find((e) => e.event === event);
  return entry ? [...entry.applicableKeys] : [];
}

/** The values a match key may take, from the vocabulary (task types for
 *  task keys, goal types for goal keys) — closed, never free text typed into
 *  a void (the tester's want/have lines come back from `automation.test`). */
export function allowedValuesForKey(key: string, vocabulary: Vocabulary | undefined): string[] {
  if (!vocabulary) return [];
  if (key === "task_type") return [...vocabulary.taskTypes];
  if (key === "goal_type") return [...vocabulary.goalTypes];
  return [];
}

/** The match object as the wire takes it: event always, the narrowed keys
 *  only, empty value-lists dropped (an empty list would match nothing). */
export function buildMatch(event: string, values: Record<string, string[]>): Record<string, unknown> {
  const match: Record<string, unknown> = { event };
  for (const [key, list] of Object.entries(values)) {
    if (list.length > 0) match[key] = list;
  }
  return match;
}

// ── the env-NAME grammar (C17) ────────────────────────────────────────────

/**
 * A webhook secret field holds the NAME of the daemon's own environment
 * variable — never a value. An entry with `=` or whitespace is refused
 * client-side with the sentence below (the daemon validates again; this only
 * stops the keystroke reaching the socket). Deliberately NOT a full regex
 * lecture: the daemon owns truth, the entry owns the obvious typo.
 */
export function envNameOnly(candidate: string): { ok: boolean } {
  const trimmed = candidate.trim();
  if (trimmed === "") return { ok: false };
  if (trimmed.includes("=") || /\s/.test(trimmed)) return { ok: false };
  return { ok: true };
}

// ── drafts & submit gates ─────────────────────────────────────────────────

export type SourceFamily = "schedule" | "webhook" | "event";

export interface ScheduleDraft {
  id: string;
  name: string;
  blueprint: string;
  expr: string;
  zone: string;
  workspace: string;
  goal: string;
  windowStart: string;
  windowEnd: string;
  windowDays: string;
  secretEnv: string;
  /** A webhook's `event` or `event:action,action` (GitHub: `issues:opened`); empty listens to every signed delivery. */
  eventFilter: string;
}

export interface EventDraft {
  id: string;
  name: string;
  blueprint: string;
  event: string;
  values: Record<string, string[]>;
  maxRunsPerHour: string;
  kickoff: string;
  componentId: string;
}

export function emptyScheduleDraft(zone: string): ScheduleDraft {
  return { id: "", name: "", blueprint: "", expr: "", zone, workspace: "", goal: "", windowStart: "", windowEnd: "", windowDays: "", secretEnv: "", eventFilter: "" };
}

export function emptyEventDraft(): EventDraft {
  return { id: "", name: "", blueprint: "", event: "", values: {}, maxRunsPerHour: "", kickoff: "", componentId: "" };
}

/**
 * The gate on the create button. Deliberately thin: the ONE validator is the
 * daemon's (TD3), so this checks only what makes a submission meaningless
 * before it costs a round trip — a name, a blueprint, a cron typed (NOT
 * parsed — parsing is the daemon's, its refusal renders under the field from
 * `automation.preview`), a zone from the vocabulary, and an env name that
 * passes `envNameOnly` for webhooks. Every other rule is the daemon's and
 * arrives verbatim as its refusal text.
 */
export function canSubmit(kind: SourceFamily, draft: ScheduleDraft | EventDraft, vocabulary: Vocabulary | undefined): boolean {
  // The schedule/webhook family has no separate name — the id IS the row's
  // name (`TriggerAddParams` carries no name field); the event family has
  // both and both must be typed.
  const nameNeeded = kind === "event" && draft.name.trim() === "";
  if (nameNeeded || draft.blueprint.trim() === "" || draft.id.trim() === "") return false;
  if (kind === "schedule" || kind === "webhook") {
    const d = draft as ScheduleDraft;
    if (d.zone.trim() === "" || d.workspace.trim() === "") return false;
    if (!vocabulary?.zones.includes(d.zone)) return false;
    if (kind === "webhook") {
      // The webhook family has no cron — the POST is its clock; what it must
      // name is the env var holding the HMAC secret (name only, never a
      // value: C17) and the workspace the loop runs in.
      return envNameOnly(d.secretEnv).ok;
    }
    // The schedule family needs a cron typed (NOT parsed — parsing is the
    // daemon's; its refusal renders under the field from automation.preview)
    // and a goal, because a schedule with no goal starts nothing.
    return d.expr.trim() !== "" && d.goal.trim() !== "";
  }
  const e = draft as EventDraft;
  if (!vocabulary?.events.some((entry) => entry.event === e.event)) return false;
  return true;
}

/** The limit draft → the wire number. Blank means "the daemon's default",
 *  which is why this returns undefined rather than a client-side number. */
export function parseLimit(candidate: string): number | undefined {
  const trimmed = candidate.trim();
  if (trimmed === "") return undefined;
  const n = Number(trimmed);
  return Number.isInteger(n) && n > 0 ? n : undefined;
}

// ── filters (the list's narrowing, as the daemon's OWN params) ────────────

export interface FilterDraft {
  source: string;
  enabled: string; // "" | "true" | "false"
  archived: string; // "" | "exclude" | "include" | "only"
  blueprint: string;
  q: string;
}

export function emptyFilterDraft(): FilterDraft {
  return { source: "", enabled: "", archived: "", blueprint: "", q: "" };
}

/** Filters travel as `automation.list` **params** — the daemon filters, the
 *  UI never sweeps a fetched list (AN10). */
export function listParams(f: FilterDraft): Record<string, unknown> {
  const params: Record<string, unknown> = {};
  if (f.source !== "") params.source = f.source;
  if (f.enabled !== "") params.enabled = f.enabled === "true";
  if (f.archived !== "" && f.archived !== "exclude") params.archived = f.archived;
  if (f.blueprint.trim() !== "") params.blueprint = f.blueprint.trim();
  if (f.q.trim() !== "") params.q = f.q.trim();
  return params;
}

/** Audit text for the AN3 disarm-on-create step — the CLI's own sentence,
 *  kept as a constant because audit rows are not translated: a person
 *  reading them in any UI language reads what the door wrote. */
export const DISARM_ON_CREATE_REASON =
  "created disarmed by the automation form — enabling is a separate deliberate act (AN3)";

// ── proposals (the desk's automation kinds) ───────────────────────────────
/** The kinds the automation plane's proposal path mints (T6): a narrowing
 *  request an agent raised. The tab lists THESE kinds only — the desk is one
 *  queue per plane, not the whole proposal store. */
export const AUTOMATION_PROPOSAL_KINDS = ["automation_disable"] as const;

export function isAutomationProposal(kind: string): boolean {
  return (AUTOMATION_PROPOSAL_KINDS as readonly string[]).includes(kind);
}

// ── fire-log paging ───────────────────────────────────────────────────────

/** The cursor the daemon hands out ("<at>|<source>:<fireId>") and reads back
 *  unchanged. The UI treats it as opaque — inventing a cursor is forging a
 *  page boundary. */
export interface FirePage {
  beforeId: string;
}

export const NO_FIRE_PAGE: FirePage = { beforeId: "" };
