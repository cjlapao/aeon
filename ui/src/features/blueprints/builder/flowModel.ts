

// ── flowTypes.ts ────────────────────────────────────────────────
// The structure a flow is edited as. It is the daemon's own JSON shape for a
// blueprint (blueprint.model answers it, blueprint.render takes it): the page
// never reads or writes the flow's text itself.

export type StepKind = "model-step" | "code-action" | "gate";

export interface Condition {
  field?: string;
  op?: string;
  value?: unknown;
  all?: Condition[];
  any?: Condition[];
  not?: Condition;
}

export interface OnReject {
  goto?: string;
  with?: Record<string, string>;
  counter?: string;
  max?: number;
  then?: string;
}

export interface Emit { type: string; min?: number; attach?: string[] }

export interface Step {
  id?: string;
  kind: StepKind;
  when?: Condition;
  agent?: string;
  skill?: string;
  params?: Record<string, unknown>;
  action?: string;
  question?: string;
  sets?: string;
  produces?: string[];
  emits?: Emit[];
  decides?: string;
  onReject?: OnReject;
  maxIterations?: number;
}

export interface Lane {
  id: string;
  advisory?: boolean;
  when?: Condition;
  step?: Step;
  iterate?: string;
  ladder?: Step[];
  rework?: string;
  onReject?: OnReject;
  parallel?: boolean;
  parallelGroup?: string;
}

export interface Question { prompt: string; answers: string[] }
export interface RequirementTerm { type: string; state: string }
export interface Requirement { anyOf: RequirementTerm[] }

export interface FlowModel {
  id: string;
  title?: string;
  description?: string;
  schemaVersion: number;
  version: number;
  lanes: Lane[];
  questions: Record<string, Question>;
  onAbort?: string;
  after?: string;
  converse?: string;
  kickoff?: string;
  output?: string[];
  requires?: Requirement[];
}

export interface FieldVocabulary { path: string; type: string; enum?: string[]; operators: string[]; doc: string }
export interface ActionArgument { name: string; type: string; required: boolean; source: string }
export interface ActionVocabulary { name: string; args: ActionArgument[] }

/** blueprint.vocabulary's answer: every closed set a form offers. */
export interface FlowVocabulary {
  stepKinds: string[];
  after: string[];
  thenActions: string[];
  operators: string[];
  decides: string[];
  emitTypes: string[];
  requireTypes: string[];
  requireStates: string[];
  fields: FieldVocabulary[];
  actions: ActionVocabulary[];
}

export interface Diagnostic {
  state: "valid" | "invalid" | "unverifiable";
  kind: string;
  path: string;
  message: string;
  hint?: string;
}
export interface FlowReport { blueprint: string; validCount: number; diagnostics: Diagnostic[] }

/** What a selection in the builder points at. */
export type Selection =
  | { type: "settings" }
  | { type: "lane"; laneId: string }
  | { type: "step"; laneId: string; index: number };

// ── flowOps.ts ──────────────────────────────────────────────────
// Pure edits of a flow's structure. Every function returns a new model and
// leaves the argument alone, so the page can keep its baseline and know — by
// comparison, not by flag — whether anything changed.

const ID_RE = /^[a-z0-9][a-z0-9_-]*$/;

/** A lane or step id the validator and the loader accept. */
export function validId(id: string): boolean { return ID_RE.test(id); }

/** The first of `base`, `base-2`, `base-3`… not already taken. */
export function uniqueName(base: string, taken: Iterable<string>): string {
  const used = new Set(taken);
  if (!used.has(base)) return base;
  let n = 2;
  while (used.has(`${base}-${n}`)) n++;
  return `${base}-${n}`;
}

export function blankStep(kind: StepKind): Step {
  if (kind === "gate") return { kind, question: "" };
  if (kind === "code-action") return { kind, action: "" };
  return { kind, agent: "" };
}

/** A step of another kind: what it was keeps (id, when, reject), what belonged to the old kind goes. */
export function changeStepKind(step: Step, kind: StepKind): Step {
  const next: Step = { ...blankStep(kind) };
  if (step.id !== undefined) next.id = step.id;
  if (step.when) next.when = step.when;
  if (step.onReject && kind !== "code-action") next.onReject = step.onReject;
  return next;
}

export function blankLane(id: string, kind: StepKind): Lane { return { id, step: blankStep(kind) }; }

/** A lane that walks a ladder of steps once per item of a collection. */
export function blankLadderLane(id: string): Lane {
  return { id, iterate: "tasks", ladder: [{ id: "step-1", kind: "model-step", agent: "" }] };
}

export const laneIds = (m: FlowModel): string[] => m.lanes.map((l) => l.id);

export function insertLane(m: FlowModel, index: number, lane: Lane): FlowModel {
  const lanes = [...m.lanes];
  lanes.splice(Math.max(0, Math.min(index, lanes.length)), 0, lane);
  return { ...m, lanes };
}

export function moveLane(m: FlowModel, from: number, to: number): FlowModel {
  if (from === to || from < 0 || from >= m.lanes.length || to < 0 || to >= m.lanes.length) return m;
  const lanes = [...m.lanes];
  const [lane] = lanes.splice(from, 1);
  lanes.splice(to, 0, lane!);
  return { ...m, lanes };
}

const mapReject = (r: OnReject | undefined, fn: (r: OnReject) => OnReject | undefined): OnReject | undefined => (r ? fn(r) : undefined);

function mapLanes(m: FlowModel, fn: (lane: Lane) => Lane): FlowModel { return { ...m, lanes: m.lanes.map(fn) }; }

/** Removing a lane takes with it every pointer at it: a reject target, the abort handler. */
export function removeLane(m: FlowModel, id: string): FlowModel {
  const clear = (r: OnReject | undefined) => mapReject(r, (x) => (x.goto === id ? stripGoto(x) : x));
  const next = mapLanes({ ...m, lanes: m.lanes.filter((l) => l.id !== id) }, (l) => withRejects(l, clear));
  if (next.onAbort === id) { const { onAbort: _gone, ...rest } = next; return rest as FlowModel; }
  return next;
}

function stripGoto(r: OnReject): OnReject | undefined {
  const { goto: _g, with: _w, ...rest } = r;
  return Object.keys(rest).length > 0 ? rest : undefined;
}

function withRejects(l: Lane, fn: (r: OnReject | undefined) => OnReject | undefined): Lane {
  const next: Lane = { ...l };
  const own = fn(l.onReject);
  if (own) next.onReject = own; else delete next.onReject;
  if (l.step) next.step = withStepReject(l.step, fn);
  if (l.ladder) next.ladder = l.ladder.map((s) => withStepReject(s, fn));
  return next;
}

function withStepReject(s: Step, fn: (r: OnReject | undefined) => OnReject | undefined): Step {
  const next: Step = { ...s };
  const r = fn(s.onReject);
  if (r) next.onReject = r; else delete next.onReject;
  return next;
}

/** Renaming a lane keeps every pointer at it pointing at it. */
export function renameLane(m: FlowModel, from: number | string, to: string): FlowModel {
  const index = typeof from === "number" ? from : m.lanes.findIndex((l) => l.id === from);
  const old = m.lanes[index]?.id;
  if (old === undefined || old === to) return m;
  const retarget = (r: OnReject | undefined) => mapReject(r, (x) => (x.goto === old ? { ...x, goto: to } : x));
  const next = mapLanes(m, (l) => withRejects(l.id === old ? { ...l, id: to } : l, retarget));
  return next.onAbort === old ? { ...next, onAbort: to } : next;
}

export function updateLane(m: FlowModel, id: string, patch: Partial<Lane>): FlowModel {
  return mapLanes(m, (l) => (l.id === id ? clean({ ...l, ...patch }) : l));
}

/** Patch the single step of a lane (index null) or one rung of its ladder. */
export function updateStep(m: FlowModel, laneId: string, index: number | null, patch: Partial<Step>): FlowModel {
  return mapLanes(m, (l) => {
    if (l.id !== laneId) return l;
    if (index === null && l.step) return { ...l, step: clean({ ...l.step, ...patch }) };
    if (index !== null && l.ladder?.[index]) {
      const ladder = [...l.ladder];
      ladder[index] = clean({ ...ladder[index]!, ...patch });
      return { ...l, ladder };
    }
    return l;
  });
}

export function replaceStep(m: FlowModel, laneId: string, index: number | null, step: Step): FlowModel {
  return mapLanes(m, (l) => {
    if (l.id !== laneId) return l;
    if (index === null) return { ...l, step };
    const ladder = [...(l.ladder ?? [])];
    ladder[index] = step;
    return { ...l, ladder };
  });
}

export function addLadderStep(m: FlowModel, laneId: string, index: number, kind: StepKind = "model-step"): FlowModel {
  return mapLanes(m, (l) => {
    if (l.id !== laneId) return l;
    const ladder = [...(l.ladder ?? [])];
    const id = uniqueName("step", ladder.map((s) => s.id ?? ""));
    ladder.splice(index, 0, { id, ...blankStep(kind) });
    return { ...l, ladder };
  });
}

export function moveLadderStep(m: FlowModel, laneId: string, from: number, to: number): FlowModel {
  return mapLanes(m, (l) => {
    if (l.id !== laneId || !l.ladder || to < 0 || to >= l.ladder.length) return l;
    const ladder = [...l.ladder];
    const [s] = ladder.splice(from, 1);
    ladder.splice(to, 0, s!);
    return { ...l, ladder };
  });
}

export function removeLadderStep(m: FlowModel, laneId: string, index: number): FlowModel {
  return mapLanes(m, (l) => {
    if (l.id !== laneId || !l.ladder) return l;
    const gone = l.ladder[index]?.id;
    const ladder = l.ladder.filter((_, i) => i !== index);
    const next: Lane = { ...l, ladder: ladder.map((s) => (s.onReject?.goto === gone ? withStepReject(s, (r) => mapReject(r, (x) => stripGoto(x))) : s)) };
    if (next.rework === gone) delete next.rework;
    return next;
  });
}

/** Renaming a rung keeps the lane's rework target and the rungs' own reject targets in step. */
export function renameLadderStep(m: FlowModel, laneId: string, index: number, to: string): FlowModel {
  return mapLanes(m, (l) => {
    const old = l.ladder?.[index]?.id;
    if (l.id !== laneId || !l.ladder || old === undefined || old === to) return l;
    const ladder = l.ladder.map((s, i) => {
      const renamed = i === index ? { ...s, id: to } : s;
      return renamed.onReject?.goto === old ? { ...renamed, onReject: { ...renamed.onReject, goto: to } } : renamed;
    });
    return { ...l, ladder, ...(l.rework === old ? { rework: to } : {}) };
  });
}

/**
 * A reject edge that is safe to write. A target at or before the lane is a loop
 * back, and a loop needs a bound and a place to go when it runs out; a target
 * after it is a skip-ahead and needs neither.
 */
export function rejectTo(m: FlowModel, laneId: string, target: string, current?: OnReject): OnReject | undefined {
  if (target === "") return current ? stripGoto(current) : undefined;
  const from = m.lanes.findIndex((l) => l.id === laneId);
  const to = m.lanes.findIndex((l) => l.id === target);
  const back = to <= from;
  const next: OnReject = { ...current, goto: target };
  if (back) {
    next.counter ||= `${laneId}_retries`;
    next.max ||= 2;
    next.then ||= "escalate";
  }
  return next;
}

export function setQuestion(m: FlowModel, id: string, q: Question): FlowModel {
  return { ...m, questions: { ...m.questions, [id]: q } };
}

export function removeQuestion(m: FlowModel, id: string): FlowModel {
  const { [id]: _gone, ...questions } = m.questions;
  return mapSteps({ ...m, questions }, (s) => (s.question === id ? { ...s, question: "" } : s));
}

export function renameQuestion(m: FlowModel, from: string, to: string): FlowModel {
  if (from === to || !(from in m.questions)) return m;
  const questions: Record<string, Question> = {};
  for (const [k, v] of Object.entries(m.questions)) questions[k === from ? to : k] = v;
  return mapSteps({ ...m, questions }, (s) => (s.question === from ? { ...s, question: to } : s));
}

function mapSteps(m: FlowModel, fn: (s: Step) => Step): FlowModel {
  return mapLanes(m, (l) => ({ ...l, ...(l.step ? { step: fn(l.step) } : {}), ...(l.ladder ? { ladder: l.ladder.map(fn) } : {}) }));
}

/** Drop the keys that hold nothing, so the written flow carries only what was said. */
function clean<T extends object>(value: T): T {
  const out: Record<string, unknown> = {};
  for (const [k, v] of Object.entries(value)) {
    if (v === undefined || v === "" || v === false) continue;
    if (Array.isArray(v) && v.length === 0) continue;
    out[k] = v;
  }
  return out as T;
}

// --- conditions ----------------------------------------------------------------------

export function isLeaf(c: Condition): boolean { return !c.all && !c.any && !c.not; }

export function conditionIsEmpty(c: Condition | undefined): boolean {
  if (!c) return true;
  if (c.all) return c.all.every(conditionIsEmpty);
  if (c.any) return c.any.every(conditionIsEmpty);
  if (c.not) return conditionIsEmpty(c.not);
  return !c.field;
}

const OP_WORDS: Record<string, string> = { eq: "is", neq: "is not", in: "is one of", gt: "is above", gte: "is at least", lt: "is below", lte: "is at most" };

/** A condition as a sentence a person reads: "mode is not poc and flags.legal_review is true". */
export function describeCondition(c: Condition | undefined): string {
  if (!c || conditionIsEmpty(c)) return "";
  if (c.all) return c.all.filter((x) => !conditionIsEmpty(x)).map(describeCondition).join(" and ");
  if (c.any) return `(${c.any.filter((x) => !conditionIsEmpty(x)).map(describeCondition).join(" or ")})`;
  if (c.not) return `not (${describeCondition(c.not)})`;
  const value = Array.isArray(c.value) ? c.value.join(", ") : String(c.value ?? "");
  return `${c.field} ${OP_WORDS[c.op ?? ""] ?? c.op ?? "is"} ${value}`.trim();
}

// --- diagnostics ---------------------------------------------------------------------

/** The lane a diagnostic's path belongs to, by id ("lanes[plan].step.agent") or by position ("lanes[2].id"). */
export function laneOfPath(path: string, m: FlowModel): string | undefined {
  const hit = /^lanes\[([^\]]+)\]/.exec(path);
  if (!hit) return undefined;
  const key = hit[1]!;
  if (/^\d+$/.test(key)) return m.lanes[Number(key)]?.id;
  return key;
}

/** Diagnostics grouped by the lane they belong to; the rest are the flow's own. */
export function diagnosticsByLane(diagnostics: Diagnostic[], m: FlowModel): { byLane: Record<string, Diagnostic[]>; flow: Diagnostic[] } {
  const byLane: Record<string, Diagnostic[]> = {};
  const flow: Diagnostic[] = [];
  for (const d of diagnostics) {
    if (d.state === "valid") continue;
    const lane = laneOfPath(d.path, m);
    if (lane) (byLane[lane] ??= []).push(d); else flow.push(d);
  }
  return { byLane, flow };
}

/** What a lane card says about its step, in a few words. */
export function stepHeadline(step: Step | undefined, questions: Record<string, Question>): string {
  if (!step) return "";
  if (step.kind === "gate") return step.question ? questions[step.question]?.prompt || step.question : "Choose the question to ask";
  if (step.kind === "code-action") return step.action || "Choose an action";
  return step.agent || "Choose an agent";
}

// ── dropRules.ts ────────────────────────────────────────────────
// What a drag may do. Moving a lane (or a rung of a ladder) changes which edges point
// backwards, and the daemon refuses a backward reject edge that has no counter and no
// max (an unbounded loop). Rather than let the person drop and then read a red verdict,
// the same rule is asked of the drop before it lands. The daemon still judges the saved
// flow; this only keeps a drag from offering a place it would refuse.

export type DropVerdict = { ok: true } | { ok: false; reason: string };

/** One thing in an ordered list, and the rejection edges it carries. */
interface Item { id: string; edges: (OnReject | undefined)[] }

const unbounded = (r: OnReject | undefined): boolean => !!r?.goto && (!r.counter || !r.max || r.max <= 0);

function unboundedBackEdges(order: string[], items: Item[]): { from: string; to: string }[] {
  const out: { from: string; to: string }[] = [];
  for (const item of items) {
    const at = order.indexOf(item.id);
    for (const r of item.edges) {
      const target = r?.goto ? order.indexOf(r.goto) : -1;
      if (target >= 0 && target <= at && unbounded(r)) out.push({ from: item.id, to: r!.goto! });
    }
  }
  return out;
}

/** `to` is the item's index in the list once it has moved (what the move helpers take). */
function judge(items: Item[], from: number, to: number, noun: string): DropVerdict {
  const before = items.map((i) => i.id);
  if (from === to || from < 0 || to < 0 || from >= before.length || to >= before.length) return { ok: true };
  const after = [...before];
  const [moved] = after.splice(from, 1);
  after.splice(to, 0, moved!);
  const already = new Set(unboundedBackEdges(before, items).map((e) => `${e.from}>${e.to}`));
  const broken = unboundedBackEdges(after, items).filter((e) => !already.has(`${e.from}>${e.to}`));
  if (broken.length === 0) return { ok: true };
  const e = broken[0]!;
  return { ok: false, reason: `“${e.from}” would send the ${noun} back to “${e.to}” with no retry limit. Give that rejection a counter and a maximum first.` };
}

export function judgeMove(model: FlowModel, from: number, to: number): DropVerdict {
  return judge(model.lanes.map((l) => ({ id: l.id, edges: [l.onReject, l.step?.onReject] })), from, to, "work");
}

export function judgeRungMove(ladder: Step[], from: number, to: number): DropVerdict {
  return judge(ladder.map((s, i) => ({ id: s.id ?? String(i), edges: [s.onReject] })), from, to, "item");
}
