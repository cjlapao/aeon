// policyModel — the pure, testable half of the Settings Policy section (S45 T9).
//
// No React, no RPC, no English chrome, and — the rule this whole section is
// built on (PM5/PM6) — **no client-side vocabulary**: no policy types, target
// kinds, sources, actions, spec shapes, authorship rule or sentence appears in
// this file. Every closed set arrives from `policy.vocabulary`; the functions
// here only decide *which control* a descriptor means, *whether the daemon's
// own matrix* permits a draft, and *whether two in-force answers differ*. The
// wording is added by the components through i18n; the daemon's sentences
// (describe, matrix reasons, before/after Render() output) are carried
// verbatim, never re-assembled.

import type { RpcMethods } from "../../../proto/contract";

/** One `policy.list` row, as the contract types it. */
export type PolicyRow = RpcMethods["policy.list"]["result"]["policies"][number];
/** The `policy.get` detail row (adds the raw spec and the two records). */
export type PolicyDetailRow = RpcMethods["policy.get"]["result"]["policy"];
/** The vocabulary answer, as `policy.vocabulary` ships it. */
export type Vocabulary = RpcMethods["policy.vocabulary"]["result"]["vocabulary"];
/** One spec-field descriptor (the form's per-type fields, PM6). */
export type SpecField = Vocabulary["specs"][number]["fields"][number];
/** One (source, action) cell of the authorship matrix. */
export type AuthorshipEntry = Vocabulary["authorship"][number];
/** One per-agent line of the seed-corpus answer. */
export type SeedAgentCount = RpcMethods["policy.seedCorpus"]["result"]["perAgent"][number];
/** One in-force answer row — `InForceAnswer.Render()` output, shown verbatim. */
export interface InForceAnswerRow {
  policyID?: string;
  state?: string;
  render?: string;
}
/** One before/after side, as the authoring doors and `policy.inForce` ship it. */
export interface InForceAnswerView {
  target?: string;
  answers?: InForceAnswerRow[];
}
/**
 * `policy.why`'s answer plus the additive fields the daemon carries (T8 made
 * the verdict shape ride the wire). The contract types only the fixture-
 * observed fields — a field a fixture never showed stays uncovered — so the
 * extra reads are optional here and rendered only when the daemon sent them.
 */
export type WhyAnswer = RpcMethods["policy.why"]["result"] & {
  asked?: boolean;
  defaultDeny?: boolean;
  by?: string;
  byTarget?: string;
  unmatchable?: boolean;
};
/**
 * `policy.add`'s params plus the additive `notAfter` the door accepts
 * (protocol's `PolicyAddParams`). The contract's fixture set never showed
 * it; the UI sends `null` for "no expiry" — never a far-future sentinel
 * (PL7).
 */
export type AddParamsWire = RpcMethods["policy.add"]["params"] & {
  notAfter?: string | null;
};
/** `policy.inForce`'s params plus the additive `loop` the door accepts. */
export type InForceParamsWire = RpcMethods["policy.inForce"]["params"] & {
  loop?: string;
};
/**
 * `policy.why`'s params plus the fields the door accepts on the wire
 * (protocol's `PolicyWhyParams`). The contract covers only what a fixture
 * showed; empty fields are omitted, never sent as blanks.
 */
export type WhyParamsWire = RpcMethods["policy.why"]["params"] & {
  laneType?: string;
  mcp?: string;
  route?: string;
  effect?: string;
  cost?: string;
  skill?: string;
  trigger?: string;
  at?: string;
};
/**
 * `proposal.list`'s params plus the `status` filter the door accepts
 * (protocol's `proposalListParams`). The contract types the params as the
 * empty object — no fixture ever carried a filter — so this is declared
 * standalone; the desk passes it through the generic bridge as given.
 */
export type ProposalListParamsWire = { status?: string };
/**
 * A proposal payload as the proposal rows carry it. `policy_add` proposals
 * (which the agent tools create, T7) carry a `reason`; the contract's
 * payload union shows the fixture-observed keys, so `reason` is declared
 * here as the optional wire field it is and rendered verbatim when present.
 */
export interface ProposalPayloadWire {
  action?: string;
  agent?: string;
  id?: string;
  reason?: string;
  server?: unknown;
  source?: string;
  spec?: Record<string, unknown>;
  target?: { id?: string; kind?: string };
  tier?: string;
  type?: string;
  pkgPath?: string;
}
/** One `proposal.list` row with the payload read as the wire shape above. */
export type ProposalRow = Omit<RpcMethods["proposal.list"]["result"]["proposals"][number], "payload"> & {
  payload: ProposalPayloadWire;
};

// --- filters -----------------------------------------------------------------

/** The Rules-tab filter bar's draft state. Empty string means "any" (the
 *  filter is simply not sent) — the LIST filtering happens in the daemon, never here. */
export interface PolicyFilterDraft {
  type: string;
  /** The selected target kind; "" means any target. */
  targetKind: string;
  /** The target id half; used when a kind is chosen. */
  targetId: string;
  source: string;
  action: string;
  origin: string;
  includeExpired: boolean;
}

export function emptyFilterDraft(): PolicyFilterDraft {
  return { type: "", targetKind: "", targetId: "", source: "", action: "", origin: "", includeExpired: true };
}

/**
 * The filter draft → the `policy.list` **params**. This is the one place the
 * filter draft becomes a request; no function in this file filters rows.
 * The target travels in the CLI spelling the door parses (`kind:id`, or the
 * bare kind when the kind names no id — the daemon's `ParseTarget` owns
 * which kinds need one and refuses a wrong spelling verbatim, so the UI
 * never restates the closed set).
 */
export function applyFilters(draft: PolicyFilterDraft): RpcMethods["policy.list"]["params"] {
  const params: RpcMethods["policy.list"]["params"] = {};
  const type = draft.type.trim();
  if (type !== "") params.type = type;
  const kind = draft.targetKind.trim();
  if (kind !== "") {
    const id = draft.targetId.trim();
    params.target = id === "" ? kind : `${kind}:${id}`;
  }
  const source = draft.source.trim();
  if (source !== "") params.source = source;
  const action = draft.action.trim();
  if (action !== "") params.action = action;
  const origin = draft.origin.trim();
  if (origin !== "") params.origin = origin;
  // Absent means true on the door; the UI sends its toggle deliberately
  // either way so the request states what the person asked for.
  params.includeExpired = draft.includeExpired;
  return params;
}

// --- grouping ------------------------------------------------------------------

/**
 * The list rows grouped by their target, **keeping the daemon's order**:
 * groups appear in first-seen order and rows keep the order inside (and
 * across) them. Nothing sorts — a UI that re-sorted would silently disagree
 * with the CLI's listing.
 */
export interface TargetGroup {
  target: string;
  rows: PolicyRow[];
}

export function groupByTarget(rows: PolicyRow[]): TargetGroup[] {
  const order: string[] = [];
  const byTarget = new Map<string, PolicyRow[]>();
  for (const row of rows) {
    const existing = byTarget.get(row.target);
    if (existing) existing.push(row);
    else {
      order.push(row.target);
      byTarget.set(row.target, [row]);
    }
  }
  return order.map((target) => ({ target, rows: byTarget.get(target)! }));
}

/** True when the row's expiry instant is in the past at `now`. An expired
 *  row is SHOWN and marked — expiry is history an operator came to read
 *  (D6/D8), never something the list hides. */
export function isExpired(notAfter: string | null | undefined, now: number = Date.now()): boolean {
  if (!notAfter) return false;
  const then = Date.parse(notAfter);
  return !Number.isNaN(then) && then < now;
}

// --- spec descriptors ------------------------------------------------------------

/** Which control a spec-field descriptor means. */
export interface FieldControl {
  control: "select" | "text" | "numeric";
  /** The options for a select (the descriptor's own `enumValues`). */
  options: string[];
  required: boolean;
  /** The descriptor's bound, when it ships one. */
  maxLength?: number;
  /** True when the daemon sent a kind this UI has no control for; it still
   *  renders as text so the field is never silently dropped. */
  unknownKind: boolean;
}

export function controlFor(field: SpecField): FieldControl {
  const base = { required: field.required, maxLength: (field as { maxLength?: number }).maxLength };
  switch (field.kind) {
    case "enum":
      return { control: "select", options: field.enumValues ?? [], unknownKind: false, ...base };
    case "number":
      return { control: "numeric", options: [], unknownKind: false, ...base };
    case "string":
      return { control: "text", options: [], unknownKind: false, ...base };
    default:
      // An unknown kind renders as text rather than vanishing: a field the
      // UI has no control for is still a field the daemon declared.
      return { control: "text", options: [], unknownKind: true, ...base };
  }
}

/** The spec descriptors the vocabulary carries for a type (never recalled). */
export function specFieldsFor(vocabulary: Vocabulary, type: string): SpecField[] {
  return vocabulary.specs.find((s) => s.type === type)?.fields ?? [];
}

// --- the authoring draft -----------------------------------------------------------

/** The authoring form's draft. `expiry` is the raw input; "" means no expiry. */
export interface PolicyDraft {
  type: string;
  targetKind: string;
  targetId: string;
  action: string;
  source: string;
  id: string;
  expiry: string;
  spec: Record<string, string>;
}

/**
 * The suggested id, deterministic from type+target (the ONLY id the UI
 * invents — the task allows the suggested id and nothing else). Lowercased,
 * punctuation collapsed to hyphens, bounded to the write boundary's id
 * width. It is a SUGGESTION: the field stays editable and the boundary owns
 * validity.
 */
export function suggestedId(type: string, targetKind: string, targetId: string): string {
  const slug = (value: string): string =>
    value
      .trim()
      .toLowerCase()
      .replace(/[^a-z0-9._-]+/g, "-")
      .replace(/^[-._]+|[-._]+$/g, "");
  const parts = [type, targetKind, targetId].map(slug).filter((p) => p !== "");
  return parts.join(".").slice(0, 64);
}

/** The (source, action) cell of the daemon's authorship matrix, or undefined
 *  when the matrix carries no such cell. */
export function authorshipFor(vocabulary: Vocabulary, source: string, action: string): AuthorshipEntry | undefined {
  return vocabulary.authorship.find((e) => e.source === source && e.action === action);
}

export interface SubmitDecision {
  ok: boolean;
  /** The daemon's OWN refusal for the (source, action) cell — rendered
   *  verbatim, never re-worded. Set exactly when the matrix blocks. */
  refusal?: string;
  /** True when the matrix cell says no (Submit disabled + the reason shown). */
  matrixBlocked: boolean;
  /** Field labels still missing from the draft (the form names them; no verdict is implied). */
  missing: string[];
}

/**
 * Whether the draft may be submitted — decided by the daemon's matrix, not
 * by a UI re-derivation of PL1. A non-operator source pairing with the
 * matrix's disallowed action disables Submit and carries the daemon's own
 * PL1 statement (`reason`) out with it; any source pairing with a `deny`
 * (or anything the matrix allows) passes this gate. Completeness is
 * separate: type, action, source, target kind and a non-empty id are the
 * shape `policy.add` needs; the target id itself is deliberately NOT
 * required here — the daemon's `ParseTarget` owns which kinds name an id
 * and its refusal is shown verbatim, so the UI never restates that half of
 * the closed set either.
 */
export function canSubmit(draft: PolicyDraft, vocabulary: Vocabulary): SubmitDecision {
  const entry = authorshipFor(vocabulary, draft.source, draft.action);
  const missing: string[] = [];
  if (draft.type.trim() === "") missing.push("type");
  if (draft.action.trim() === "") missing.push("action");
  if (draft.source.trim() === "") missing.push("source");
  if (draft.targetKind.trim() === "") missing.push("targetKind");
  if (draft.id.trim() === "") missing.push("id");
  for (const field of specFieldsFor(vocabulary, draft.type)) {
    if (field.required && (draft.spec[field.name] ?? "").trim() === "") missing.push(`spec.${field.name}`);
  }
  if (entry === undefined) {
    // The matrix carries no such cell: the draft cannot be blessed from a
    // set the daemon did not state. Disabled, without a UI-invented reason.
    return { ok: false, matrixBlocked: true, missing };
  }
  if (!entry.allowed) {
    return { ok: false, refusal: entry.reason, matrixBlocked: true, missing };
  }
  return { ok: missing.length === 0, matrixBlocked: false, missing };
}

/**
 * The draft → the `policy.add` params. An empty expiry is **null** — never
 * an empty string, never a far-future sentinel (PL7: `NotAfter == nil`
 * means no expiry). Spec values stay strings the boundary decodes.
 */
export function buildAddParams(draft: PolicyDraft): AddParamsWire {
  const spec: AddParamsWire["spec"] = {};
  for (const [name, value] of Object.entries(draft.spec)) {
    const trimmed = value.trim();
    if (trimmed === "") continue;
    // The wire spec is the fixed descriptor object the contract types;
    // the boundary decodes the raw JSON, so declared fields ride as given.
    (spec as Record<string, unknown>)[name] = trimmed;
  }
  const expiry = draft.expiry.trim();
  return {
    id: draft.id.trim(),
    type: draft.type.trim(),
    target: { kind: draft.targetKind.trim(), id: draft.targetId.trim() },
    action: draft.action.trim(),
    source: draft.source.trim() === "" ? undefined : draft.source.trim(),
    spec,
    notAfter: expiry === "" ? null : expiry,
  };
}

// --- before/after ------------------------------------------------------------------

export interface DiffResult {
  changed: boolean;
  /** The sides as received — a changed diff renders both VERBATIM. */
  before?: InForceAnswerView;
  after?: InForceAnswerView;
}

function normalizeAnswers(view: InForceAnswerView | undefined): string {
  if (!view) return "";
  const answers = (view.answers ?? [])
    .map((a) => `${a.policyID ?? ""}|${a.state ?? ""}|${a.render ?? ""}`)
    .sort();
  return `${view.target ?? ""}\u0000${answers.join("\u0001")}`;
}

/**
 * Whether a before/after pair shows a change. Identical sides → not changed
 * (and the UI says "no change" instead of showing two identical blocks);
 * different → both sides come back untouched for verbatim rendering. No
 * hidden effect: a changed diff can never be rendered as one answer.
 */
export function describeDiff(before?: InForceAnswerView, after?: InForceAnswerView): DiffResult {
  if (normalizeAnswers(before) === normalizeAnswers(after)) {
    return { changed: false };
  }
  return { changed: true, before, after };
}

// --- seed corpus ---------------------------------------------------------------

/**
 * The engine's per-record states, tallied per agent for the dry-run preview.
 * The per-agent counts the seedCorpus door ships carry only what the run
 * wrote/left alone; `removed`/`diverged` are the ENGINE's per-record states
 * from `seed.status`, whose record keys carry the agent as a key segment.
 * The tally reads the daemon's states — it never re-derives them — and a
 * key whose shape this UI has never seen simply groups under its full key,
 * so a changed key scheme shows up instead of vanishing.
 */
export interface SeedAgentStates {
  agent: string;
  counts: Record<string, number>;
}

export function groupSeedRecordsByAgent(
  records: Array<{ key: string; state: string }>,
): SeedAgentStates[] {
  const order: string[] = [];
  const byAgent = new Map<string, Record<string, number>>();
  for (const record of records) {
    const segments = record.key.split(".");
    const agent = segments.length >= 3 ? (segments[1] ?? record.key) : record.key;
    let counts = byAgent.get(agent);
    if (!counts) {
      counts = {};
      byAgent.set(agent, counts);
      order.push(agent);
    }
    counts[record.state] = (counts[record.state] ?? 0) + 1;
  }
  return order.map((agent) => ({ agent, counts: byAgent.get(agent)! }));
}

/** The seedCorpus preview table: one row per agent, joining the run's own
 *  per-agent counts with the engine's per-record state tally. Agents from
 *  either side appear; nothing is dropped (a record counted on one side
 *  only is still shown). */
export interface SeedPreviewRow {
  agent: string;
  seeded: number;
  unchanged: number;
  states: Record<string, number>;
}

export function seedPreviewRows(
  perAgent: SeedAgentCount[] | undefined,
  engineStates: SeedAgentStates[] | undefined,
): SeedPreviewRow[] {
  const rows = new Map<string, SeedPreviewRow>();
  const rowFor = (agent: string): SeedPreviewRow => {
    let row = rows.get(agent);
    if (!row) {
      row = { agent, seeded: 0, unchanged: 0, states: {} };
      rows.set(agent, row);
    }
    return row;
  };
  for (const line of perAgent ?? []) {
    const row = rowFor(line.agent);
    row.seeded = line.seeded;
    row.unchanged = line.unchanged;
  }
  for (const group of engineStates ?? []) {
    const row = rowFor(group.agent);
    row.states = group.counts;
  }
  return [...rows.values()];
}

/** The policy type and field that carry the tool-address picker, read off the
 *  daemon's spec descriptors so no type or field name is recalled here. */
export function toolPickerInfo(vocabulary: Vocabulary | undefined): { type: string; field: string } | undefined {
  for (const spec of vocabulary?.specs ?? []) {
    const field = spec.fields.find((f) => f.picker === "tool-address");
    if (field) return { type: spec.type, field: field.name };
  }
  return undefined;
}

/** The enum field a spec repeats the row's action in (`state`): found by kind,
 *  so the guided form can keep it equal to the chosen action. */
export function stateFieldOf(vocabulary: Vocabulary | undefined, type: string): string | undefined {
  return specFieldsFor(vocabulary ?? ({ specs: [] } as unknown as Vocabulary), type).find((f) => f.kind === "enum")?.name;
}

/** The source a person authors as by default: the one whose authorship cells
 *  allow the most actions (the operator), not whatever the daemon happens to
 *  list first — a default that cannot author an allow made every grant start
 *  refused. Read from the daemon's own matrix; no source is named here. */
export function defaultSource(vocabulary: Vocabulary): string {
  const allowed = (source: string) => vocabulary.authorship.filter((c) => c.source === source && c.allowed).length;
  return [...vocabulary.sources].sort((a, b) => allowed(b) - allowed(a))[0] ?? "";
}
