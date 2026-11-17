import { Field, Section, Segmented, Switch, TextField, Chips, NumberField, RemoveButton, SelectField, Card } from "./fields";
import { Button, IconButton, Input } from "../../../components/kit";
import { LadderEditor } from "./FlowBuilder";
import { rejectTo, renameLadderStep, renameLane, replaceStep, updateLane, updateStep, validId, FlowModel, FlowVocabulary, OnReject, Selection, Step, changeStepKind, Emit, Question, StepKind, describeCondition, isLeaf, Condition, FieldVocabulary, removeQuestion, renameQuestion, setQuestion, uniqueName, Requirement } from "./flowModel";
import { useAppSettings } from "../../../lib/appSettings";
import { useState } from "react";

// ── LaneInspector.tsx ───────────────────────────────────────────
export interface InspectorProps {
  model: FlowModel;
  edit: (fn: (m: FlowModel) => FlowModel) => void;
  vocabulary: FlowVocabulary;
  agents: string[];
  select: (s: Selection) => void;
  stepCtx: StepContext;
  condCtx: ConditionContext;
}

/** The lane under the cursor: what it is, when it runs, what happens when it is rejected. */
export function LaneInspector({ laneId, ...p }: InspectorProps & { laneId: string }) {
  const { model, edit, vocabulary } = p;
  const index = model.lanes.findIndex((l) => l.id === laneId);
  const lane = model.lanes[index];
  if (!lane) return <p className="text-sm text-slate-500">This lane is gone.</p>;
  const ladder = lane.ladder !== undefined;
  const others = model.lanes.filter((l) => l.id !== laneId);
  const idTaken = others.some((l) => l.id === laneId);

  const toKind = (kind: string) => {
    if (kind === "ladder" && !ladder) {
      const step: Step = lane.step ? { id: "step-1", ...lane.step } : { id: "step-1", kind: "model-step", agent: "" };
      edit((m) => updateLane({ ...m, lanes: m.lanes.map((l) => (l.id === laneId ? { id: l.id, ...(l.when ? { when: l.when } : {}), ...(l.onReject ? { onReject: l.onReject } : {}), iterate: "tasks", ladder: [step] } : l)) }, laneId, {}));
    } else if (kind === "step" && ladder) {
      const first = lane.ladder?.[0];
      const { id: _drop, ...step } = first ?? { id: "", kind: "model-step" as const, agent: "" };
      edit((m) => ({ ...m, lanes: m.lanes.map((l) => (l.id === laneId ? { id: l.id, ...(l.when ? { when: l.when } : {}), ...(l.onReject ? { onReject: l.onReject } : {}), step: step as Step } : l)) }));
      p.select({ type: "lane", laneId });
    }
  };
  const targets = others.map((l) => ({ value: l.id, label: l.id }));
  const goto = lane.onReject?.goto;
  const loopsBack = goto !== undefined && model.lanes.findIndex((l) => l.id === goto) <= index;
  const stepRejectable = !ladder && lane.step?.kind !== "code-action";

  return <div className="space-y-4" data-testid="lane-inspector">
    <Field label="Lane id" hint="The name other lanes use to point at this one." error={idTaken ? "Another lane has this id." : !validId(laneId) ? "Use lower-case letters, numbers, - or _." : undefined}>
      <TextField value={lane.id} mono onChange={(v) => { if (v !== "") { edit((m) => renameLane(m, index, v)); p.select({ type: "lane", laneId: v }); } }} />
    </Field>
    <Segmented label="Lane shape" value={ladder ? "ladder" : "step"} onChange={toKind} options={[
      { value: "step", label: "One step", hint: "Runs once when the flow reaches it." },
      { value: "ladder", label: "Repeat for each item", hint: "Walks a chain of steps once for every task (or other item) the flow collected." }]} />

    {!ladder && lane.step && <StepForm step={lane.step} ctx={p.stepCtx} onChange={(s) => edit((m) => replaceStep(m, laneId, null, s))} />}
    {ladder && <LadderEditor lane={lane} {...p} />}

    <Section title="Run only when" hint="Skip this lane unless the loop's facts say it applies.">
      <ConditionBuilder label="This lane" value={lane.when} ctx={p.condCtx} onChange={(c) => edit((m) => updateLane(m, laneId, { when: c }))} />
    </Section>

    {(ladder || lane.step?.kind !== "code-action" || lane.step?.action === "verify.run") && (
      <Section title="If it is rejected" hint="Where the flow goes when a person rejects this lane, or its checks fail.">
        <RejectForm value={lane.onReject} targetLabel="Send the flow back to" nowhere="Nowhere — end here" targets={targets} loopsBack={loopsBack}
          thenActions={vocabulary.thenActions} onJump={(t) => edit((m) => updateLane(m, laneId, { onReject: rejectTo(m, laneId, t, lane.onReject) }))}
          onChange={(r: OnReject | undefined) => edit((m) => updateLane(m, laneId, { onReject: r }))} />
      </Section>
    )}
    {stepRejectable && lane.step?.onReject && <p className="text-xs text-amber-700 dark:text-amber-300">This step also carries its own reject rule.</p>}

    <details className="text-sm"><summary className="cursor-pointer text-xs font-semibold uppercase tracking-wide text-slate-500">Advanced</summary>
      <div className="mt-3 space-y-3">
        <Switch checked={lane.advisory === true} onChange={(v) => edit((m) => updateLane(m, laneId, { advisory: v }))} label="Advisory" hint="The lane runs and reports but can never route a rejection." />
        {ladder && <Switch checked={lane.parallel === true} onChange={(v) => edit((m) => updateLane(m, laneId, { parallel: v }))} label="Run ready items in parallel" hint="Items whose dependencies are done start together." />}
        <Field label="Parallel group" hint="Lanes sharing a name may run together; they must not write the same files.">
          <TextField value={lane.parallelGroup ?? ""} onChange={(v) => edit((m) => updateLane(m, laneId, { parallelGroup: v }))} mono />
        </Field>
      </div>
    </details>
  </div>;
}

/** One rung of a ladder, opened from its lane. */
export function StepInspector({ laneId, index, ...p }: InspectorProps & { laneId: string; index: number }) {
  const { model, edit, vocabulary } = p;
  const lane = model.lanes.find((l) => l.id === laneId);
  const step = lane?.ladder?.[index];
  if (!lane || !step) return <p className="text-sm text-slate-500">This step is gone.</p>;
  const ids = (lane.ladder ?? []).map((s) => s.id ?? "");
  const rungTargets = ids.filter((_, i) => i !== index).map((id) => ({ value: id, label: id }));
  const goto = step.onReject?.goto;
  const loopsBack = goto !== undefined && ids.indexOf(goto) <= index;
  const setReject = (r: OnReject | undefined) => edit((m) => updateStep(m, laneId, index, { onReject: r }));
  return <div className="space-y-4" data-testid="step-inspector">
    <Button size="xs" variant="ghost" leadingIcon="ArrowLeft" onClick={() => p.select({ type: "lane", laneId })}>{laneId}</Button>
    <StepForm step={step} ctx={p.stepCtx} idField otherIds={ids.filter((_, i) => i !== index)}
      onChange={(s) => edit((m) => replaceStep(m, laneId, index, s))}
      onRename={(id) => edit((m) => renameLadderStep(m, laneId, index, id))} />
    <Section title="Run only when" hint="Skip this step for items it does not apply to.">
      <ConditionBuilder label="This step" value={step.when} ctx={p.condCtx} onChange={(c) => edit((m) => updateStep(m, laneId, index, { when: c }))} />
    </Section>
    {step.kind !== "code-action" && <Section title="If it is rejected" hint="Where this item goes when the step is rejected.">
      <RejectForm value={step.onReject} targetLabel="Send the item back to" nowhere="Nowhere — end here" targets={rungTargets} loopsBack={loopsBack} thenActions={vocabulary.thenActions}
        onJump={(t) => edit((m) => updateStep(m, laneId, index, { onReject: t === "" ? (step.onReject ? { ...step.onReject, goto: undefined } : undefined) : { ...step.onReject, goto: t, counter: step.onReject?.counter || `${step.id}_retries`, max: step.onReject?.max || 2, then: step.onReject?.then || "escalate" } }))}
        onChange={setReject} />
    </Section>}
  </div>;
}

// ── StepForm.tsx ────────────────────────────────────────────────
const KINDS = [
  { value: "model-step", label: "Agent step", hint: "An agent does the work and writes files." },
  { value: "code-action", label: "Code action", hint: "The daemon runs an action: git, the forge, the checks." },
  { value: "gate", label: "Approval", hint: "A person answers a question; nothing moves until they do." },
];

export interface StepContext {
  vocabulary: FlowVocabulary;
  agents: string[];
  questions: Record<string, Question>;
  /** Questions belong to the flow: a gate can ask for a new one, and edits one in place. */
  onNewQuestion: () => void;
  onEditQuestion: (id: string, q: Question) => void;
}

/** Everything about one step: its kind, what it does, what it produces. Rung ids are edited here; a lane's single step has none. */
export function StepForm({ step, onChange, ctx, idField, onRename, otherIds }: {
  step: Step;
  onChange: (next: Step) => void;
  ctx: StepContext;
  idField?: boolean;
  onRename?: (id: string) => void;
  otherIds?: string[];
}) {
  const patch = (p: Partial<Step>) => {
    const next: Step = { ...step, ...p };
    for (const k of Object.keys(next) as (keyof Step)[]) { const v = next[k]; if (v === undefined || v === "" || (Array.isArray(v) && v.length === 0)) delete next[k]; }
    onChange(next);
  };
  const idTaken = idField && step.id !== undefined && (otherIds ?? []).includes(step.id);
  return <div className="space-y-4" data-testid="step-form">
    {idField && <Field label="Step id" hint="Names this step inside the ladder, so a rework or a rejection can point at it." error={idTaken ? "Another step already has this id." : step.id !== undefined && step.id !== "" && !validId(step.id) ? "Use lower-case letters, numbers, - or _." : undefined}>
      <TextField value={step.id ?? ""} onChange={(v) => (onRename ? onRename(v) : patch({ id: v }))} mono />
    </Field>}
    <Segmented label="What this step is" value={step.kind} options={KINDS} onChange={(k) => onChange(changeStepKind(step, k as StepKind))} />
    {step.kind === "model-step" && <ModelFields step={step} patch={patch} ctx={ctx} />}
    {step.kind === "code-action" && <CodeFields step={step} patch={patch} ctx={ctx} />}
    {step.kind === "gate" && <GateFields step={step} patch={patch} ctx={ctx} />}
  </div>;
}

function ModelFields({ step, patch, ctx }: { step: Step; patch: (p: Partial<Step>) => void; ctx: StepContext }) {
  const emits = step.emits ?? [];
  const setEmit = (i: number, e: Emit) => patch({ emits: emits.map((x, j) => (j === i ? e : x)) });
  return <>
    <Field label="Agent" hint="Who does this step. Agents are managed on the Agents page.">
      <SelectField label="Agent" value={step.agent ?? ""} onChange={(v) => patch({ agent: v })} placeholder="Choose an agent" options={ctx.agents.map((a) => ({ value: a, label: a }))} />
    </Field>
    <Field label="Files it writes" hint="Documents this step must produce before it can finish. Press Enter after each name.">
      <Chips label="Files it writes" values={step.produces ?? []} onChange={(v) => patch({ produces: v })} placeholder="plan.md" />
    </Field>
    <Section title="Records it files" hint="Goals, epics, briefs or tasks this step must create. The step cannot finish until they exist."
      action={<Button size="xs" variant="outline" onClick={() => patch({ emits: [...emits, { type: ctx.vocabulary.emitTypes[0] ?? "goal" }] })}>+ Record</Button>}>
      {emits.length === 0 && <p className="text-xs text-slate-500">None — this step only writes files.</p>}
      {emits.map((e, i) => <div key={i} className="space-y-2 rounded-lg border border-slate-200/70 p-3 dark:border-white/10">
        <div className="flex gap-2">
          <SelectField label="Record type" value={e.type} onChange={(v) => setEmit(i, { ...e, type: v })} options={ctx.vocabulary.emitTypes.map((t) => ({ value: t, label: t }))} />
          <div className="w-24"><NumberField label="At least" min={0} value={e.min ?? 1} onChange={(v) => setEmit(i, { ...e, min: Number(v) })} /></div>
          <RemoveButton label="Remove record" onClick={() => patch({ emits: emits.filter((_, j) => j !== i) })} />
        </div>
        <Field label="Pin these files to each record"><Chips label="Pin files" values={e.attach ?? []} onChange={(v) => setEmit(i, { ...e, attach: v })} suggestions={step.produces} placeholder="brief.md" /></Field>
      </div>)}
    </Section>
    <details className="text-sm"><summary className="cursor-pointer text-xs font-semibold uppercase tracking-wide text-slate-500">Advanced</summary>
      <div className="mt-3 space-y-3">
        <Field label="Most model calls" hint="Leave empty to use the agent's own limit. 0 means no limit.">
          <NumberField label="Most model calls" min={0} value={step.maxIterations ?? ""} onChange={(v) => patch({ maxIterations: v === "" ? undefined : Number(v) })} />
        </Field>
        <Field label="Skill" hint="Call another agent's skill instead: callee/skill-id."><TextField value={step.skill ?? ""} onChange={(v) => patch({ skill: v })} mono placeholder="reviewer/security-pass" /></Field>
      </div>
    </details>
  </>;
}

function CodeFields({ step, patch, ctx }: { step: Step; patch: (p: Partial<Step>) => void; ctx: StepContext }) {
  const action = ctx.vocabulary.actions.find((a) => a.name === step.action);
  const params = step.params ?? {};
  const setParam = (name: string, value: string) => {
    const next = { ...params };
    if (value === "") delete next[name]; else next[name] = value;
    patch({ params: Object.keys(next).length ? next : undefined });
  };
  return <>
    <Field label="Action" hint="What the daemon does. The set is closed: no action can be invented here.">
      <SelectField label="Action" value={step.action ?? ""} onChange={(v) => patch({ action: v, params: undefined })} placeholder="Choose an action" options={ctx.vocabulary.actions.map((a) => ({ value: a.name, label: a.name }))} />
    </Field>
    {action && action.args.length > 0 && <Section title="Arguments" hint="Most are filled by the loop itself; set one only when you need to choose.">
      {action.args.map((a) => <Field key={a.name} label={`${a.name}${a.required ? " · required" : ""}`} hint={`${a.type} · ${a.source === "code" || a.source === "manifest" ? "the loop fills this when empty" : `from ${a.source}`}`}>
        <TextField value={String(params[a.name] ?? "")} onChange={(v) => setParam(a.name, v)} mono />
      </Field>)}
    </Section>}
    <Field label="Sets a flag" hint="Records that this action ran, under a name conditions can read (flags.name)."><TextField value={step.sets ?? ""} onChange={(v) => patch({ sets: v })} mono /></Field>
  </>;
}

function GateFields({ step, patch, ctx }: { step: Step; patch: (p: Partial<Step>) => void; ctx: StepContext }) {
  const ids = Object.keys(ctx.questions);
  const q = step.question ? ctx.questions[step.question] : undefined;
  return <>
    <Field label="Question to ask" hint="Questions are written once for the whole flow and can be asked by several gates.">
      <div className="flex gap-2">
        <SelectField label="Question" value={step.question ?? ""} onChange={(v) => patch({ question: v })} placeholder="Choose a question" options={ids.map((id) => ({ value: id, label: ctx.questions[id]?.prompt ? `${id} — ${ctx.questions[id]!.prompt}` : id }))} />
        <Button size="sm" variant="outline" onClick={ctx.onNewQuestion}>New</Button>
      </div>
    </Field>
    {q && step.question && <div className="space-y-3 rounded-lg border border-slate-200/70 p-3 dark:border-white/10">
      <Field label="The person is asked"><TextField multiline rows={2} value={q.prompt} onChange={(v) => ctx.onEditQuestion(step.question!, { ...q, prompt: v })} placeholder="Approve the plan?" /></Field>
      <Field label="They can answer" hint="The first answer is the one that lets the flow move on; the others reject."><Chips mono={false} label="Answers" values={q.answers} onChange={(v) => ctx.onEditQuestion(step.question!, { ...q, answers: v })} placeholder="approve" /></Field>
    </div>}
    <Field label="What the answer decides" hint="Approving settles this record; rejecting sends it back. Leave empty for a plain question.">
      <SelectField label="Decides" value={step.decides ?? ""} onChange={(v) => patch({ decides: v })} placeholder="Nothing — it only routes the flow" options={ctx.vocabulary.decides.map((d) => ({ value: d, label: `The ${d}` }))} />
    </Field>
  </>;
}

// ── RejectForm.tsx ──────────────────────────────────────────────
const THEN_WORDS: Record<string, { label: string; hint: string }> = {
  escalate: { label: "Escalate to a person", hint: "The loop stops and asks a person what to do." },
  block: { label: "Block the loop", hint: "The loop stops as blocked until someone resumes it." },
  "fail-item": { label: "Fail this item", hint: "The item is marked failed and the loop moves on to the next." },
};

/**
 * What happens when a person (or a check) rejects this step. A jump back to an
 * earlier place is a loop, and a loop needs a bound and an answer for when the
 * bound is reached — the form asks for both so the flow cannot be saved without.
 */
export function RejectForm({ value, onChange, targets, targetLabel, loopsBack, thenActions, nowhere, onJump }: {
  value: OnReject | undefined;
  onChange: (next: OnReject | undefined) => void;
  /** The places a rejection may send the flow back to. */
  targets: { value: string; label: string }[];
  targetLabel: string;
  /** Whether the chosen target is at or before this step: a loop. */
  loopsBack: boolean;
  thenActions: string[];
  nowhere: string;
  /** Choosing a target is the owner's to write (it may add the loop's bound). */
  onJump: (target: string) => void;
}) {
  const r = value ?? {};
  const set = (patch: Partial<OnReject>) => {
    const next: OnReject = { ...r, ...patch };
    for (const k of Object.keys(next) as (keyof OnReject)[]) if (next[k] === undefined || next[k] === "") delete next[k];
    onChange(Object.keys(next).length > 0 ? next : undefined);
  };
  const withEntries = Object.entries(r.with ?? {});
  const setWith = (entries: [string, string][]) => set({ with: entries.length ? Object.fromEntries(entries) : undefined });

  return <div className="space-y-3" data-testid="reject-form">
    <Field label={targetLabel} hint={r.goto ? undefined : "Without a target, a rejection ends here with the outcome below."}>
      <SelectField label={targetLabel} value={r.goto ?? ""} onChange={onJump} placeholder={nowhere} options={targets} />
    </Field>
    {r.goto && loopsBack && <div className="grid gap-3 sm:grid-cols-2">
      <Field label="Try at most" hint="Rejections allowed before the outcome below.">
        <NumberField label="Try at most" min={1} value={r.max ?? 2} onChange={(v) => set({ max: Math.max(1, Number(v) || 1) })} />
      </Field>
      <Field label="Counter name" hint="The tally this loop keeps; conditions can read it.">
        <TextField value={r.counter ?? ""} onChange={(v) => set({ counter: v })} mono />
      </Field>
    </div>}
    {(!r.goto || loopsBack) && <Field label={r.goto ? "When the tries run out" : "When rejected"} hint={THEN_WORDS[r.then ?? ""]?.hint}>
      <SelectField label="Outcome" value={r.then ?? ""} onChange={(v) => set({ then: v })} placeholder={r.goto ? "Choose what happens" : "Nothing special"}
        options={thenActions.map((a) => ({ value: a, label: THEN_WORDS[a]?.label ?? a }))} />
    </Field>}
    {r.goto && <details className="text-sm">
      <summary className="cursor-pointer text-xs font-semibold uppercase tracking-wide text-slate-500">Hand the target a note</summary>
      <div className="mt-2 space-y-2">
        {withEntries.map(([k, v], i) => <div key={i} className="flex gap-2">
          <TextField label="Note key" mono value={k} onChange={(nv) => setWith(withEntries.map((x, j) => (j === i ? [nv, x[1]] : x)))} />
          <TextField label="Note value" value={v} onChange={(nv) => setWith(withEntries.map((x, j) => (j === i ? [x[0], nv] : x)))} />
          <RemoveButton label="Remove note" onClick={() => setWith(withEntries.filter((_, j) => j !== i))} />
        </div>)}
        <Button size="xs" variant="outline" onClick={() => setWith([...withEntries, ["mode", "revise"]])}>+ Note</Button>
      </div>
    </details>}
  </div>;
}

// ── ConditionBuilder.tsx ────────────────────────────────────────
// "Run only when…" — a condition drawn as sentences a person can build: pick a
// fact of the loop, say how it compares, give the value. Rules combine as
// "all of" (and) or "any of" (or), and any rule can be turned into "not".
// What may be named comes from the daemon's vocabulary, never from this file.

const OP_LABEL: Record<string, string> = { eq: "is", neq: "is not", in: "is one of", gt: "is above", gte: "is at least", lt: "is below", lte: "is at most" };

export interface ConditionContext {
  fields: FieldVocabulary[];
  /** Counters the flow's reject edges declare, offered for `counters.*`. */
  counters: string[];
  /** Questions the flow asks, offered for `answers.*`. */
  questions: string[];
}

/** The vocabulary entry a field name belongs to: an exact path, or a `prefix.*` pattern. */
export function fieldEntry(name: string | undefined, fields: FieldVocabulary[]): FieldVocabulary | undefined {
  if (!name) return undefined;
  return fields.find((f) => f.path === name) ?? fields.find((f) => f.path.endsWith(".*") && name.startsWith(f.path.slice(0, -1)));
}

const newLeaf = (fields: FieldVocabulary[]): Condition => {
  const first = fields[0];
  return { field: first && !first.path.endsWith(".*") ? first.path : "", op: first?.operators[0] ?? "eq", value: "" };
};

export function ConditionBuilder({ value, onChange, ctx, label }: { value: Condition | undefined; onChange: (c: Condition | undefined) => void; ctx: ConditionContext; label: string }) {
  if (!value) {
    return <div className="flex items-center justify-between gap-3 rounded-lg border border-dashed border-slate-300 px-3 py-2 dark:border-white/15">
      <span className="text-sm text-slate-600 dark:text-slate-300">{label}: <b>always</b></span>
      <Button size="xs" variant="outline" onClick={() => onChange({ all: [newLeaf(ctx.fields)] })}>Add a condition</Button>
    </div>;
  }
  return <div className="space-y-2" data-testid="condition-builder">
    <Node value={value} onChange={(c) => onChange(c)} onRemove={() => onChange(undefined)} ctx={ctx} root />
    {describeCondition(value) !== "" && <p className="rounded-md bg-slate-100/80 px-3 py-1.5 text-xs text-slate-600 dark:bg-white/5 dark:text-slate-300">Runs only when <b>{describeCondition(value)}</b>.</p>}
  </div>;
}

function Node({ value, onChange, onRemove, ctx, root }: { value: Condition; onChange: (c: Condition) => void; onRemove: () => void; ctx: ConditionContext; root?: boolean }) {
  if (value.not) {
    return <div className="rounded-lg border border-rose-300/50 bg-rose-50/40 p-2 dark:border-rose-400/20 dark:bg-rose-500/5">
      <div className="mb-1.5 flex items-center justify-between"><span className="text-xs font-semibold uppercase tracking-wide text-rose-700 dark:text-rose-300">Not</span>
        <Button size="xs" variant="ghost" color="rose" onClick={() => onChange(value.not!)}>Remove “not”</Button></div>
      <Node value={value.not} onChange={(c) => onChange({ not: c })} onRemove={onRemove} ctx={ctx} />
    </div>;
  }
  if (value.all || value.any) {
    const mode = value.all ? "all" : "any";
    const items = (value.all ?? value.any)!;
    const write = (next: Condition[], m = mode) => onChange(m === "all" ? { all: next } : { any: next });
    return <div className={`space-y-2 rounded-xl border p-3 ${root ? "border-slate-200 dark:border-white/10" : "border-indigo-300/50 bg-indigo-50/30 dark:border-indigo-400/20 dark:bg-indigo-500/5"}`}>
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-xs text-slate-500 dark:text-slate-400">Run when</span>
        <div className="w-56"><SelectField label="Combine rules" value={mode} onChange={(v) => write(items, v as "all" | "any")} options={[{ value: "all", label: "all of these are true" }, { value: "any", label: "any of these is true" }]} /></div>
        <span className="ml-auto"><Button size="xs" variant="ghost" color="rose" onClick={onRemove}>{root ? "Remove condition" : "Remove group"}</Button></span>
      </div>
      {items.map((item, i) => <Row key={i} first={i === 0} mode={mode}>
        <Node value={item} ctx={ctx} onChange={(c) => write(items.map((x, j) => (j === i ? c : x)))} onRemove={() => write(items.filter((_, j) => j !== i))} />
      </Row>)}
      <div className="flex flex-wrap gap-2">
        <Button size="xs" variant="outline" onClick={() => write([...items, newLeaf(ctx.fields)])}>+ Rule</Button>
        <Button size="xs" variant="outline" onClick={() => write([...items, { any: [newLeaf(ctx.fields)] }])}>+ Group</Button>
      </div>
    </div>;
  }
  return <div className="flex items-start gap-2"><div className="min-w-0 flex-1"><Leaf value={value} onChange={onChange} ctx={ctx} /></div>
    <div className="flex shrink-0 flex-col gap-1 pt-1">
      <RemoveButton label="Remove rule" onClick={onRemove} />
      <IconButton icon="Revert" size="xs" variant="ghost" srLabel="Turn this rule around" tooltip="Turn this rule around (not)" onClick={() => onChange({ not: value })} />
    </div></div>;
}

function Row({ children, first, mode }: { children: React.ReactNode; first: boolean; mode: "all" | "any" }) {
  return <div className="space-y-1">{!first && <div className="pl-1 text-[10px] font-semibold uppercase tracking-widest text-slate-400">{mode === "all" ? "and" : "or"}</div>}{children}</div>;
}

function Leaf({ value, onChange, ctx }: { value: Condition; onChange: (c: Condition) => void; ctx: ConditionContext }) {
  const { controlTone } = useAppSettings();
  const entry = fieldEntry(value.field, ctx.fields);
  const wildcard = entry?.path.endsWith(".*") ? entry.path.slice(0, -1) : undefined;
  const suffix = wildcard && value.field ? value.field.slice(wildcard.length) : "";
  const suggestions = entry?.path === "counters.*" ? ctx.counters : entry?.path === "answers.*" ? ctx.questions : [];
  const ops = entry?.operators ?? ["eq", "neq", "in"];

  const setField = (path: string) => {
    const next = ctx.fields.find((f) => f.path === path);
    const field = next?.path.endsWith(".*") ? next.path.slice(0, -1) : path;
    const op = next && !next.operators.includes(value.op ?? "") ? next.operators[0] : value.op;
    onChange({ field, op, value: next?.type === "bool" ? true : next?.type === "int" ? 0 : next?.enum?.[0] ?? "" });
  };

  return <div className="grid gap-2 sm:grid-cols-[minmax(0,1.3fr)_minmax(0,0.9fr)_minmax(0,1fr)]">
    <div className="space-y-1">
      <SelectField label="Fact" value={wildcard ? entry!.path : (value.field ?? "")} onChange={setField} placeholder="Choose a fact"
        options={ctx.fields.map((f) => ({ value: f.path, label: f.path.endsWith(".*") ? `${f.path.slice(0, -1)}…` : f.path }))} />
      {wildcard && <div className="flex items-center gap-1"><span className="font-mono text-xs text-slate-500">{wildcard}</span>
        <Input aria-label="Name" size="sm" tone={controlTone} list={`sugg-${wildcard}`} className="font-mono" value={suffix} placeholder="name"
          onChange={(e) => onChange({ ...value, field: wildcard + e.target.value.trim() })} />
        <datalist id={`sugg-${wildcard}`}>{suggestions.map((s) => <option key={s} value={s} />)}</datalist></div>}
      {entry?.doc && <p className="text-[11px] text-slate-500 dark:text-slate-400">{entry.doc}</p>}
    </div>
    <SelectField label="Comparison" value={value.op ?? ""} onChange={(op) => onChange({ ...value, op, value: op === "in" ? [] : Array.isArray(value.value) ? "" : value.value })}
      options={ops.map((o) => ({ value: o, label: OP_LABEL[o] ?? o }))} />
    <ValueInput entry={entry} op={value.op ?? "eq"} value={value.value} onChange={(v) => onChange({ ...value, value: v })} />
  </div>;
}

function ValueInput({ entry, op, value, onChange }: { entry: FieldVocabulary | undefined; op: string; value: unknown; onChange: (v: unknown) => void }) {
  if (op === "in") {
    const list = Array.isArray(value) ? value.map(String) : [];
    return <Chips label="Values" values={list} onChange={onChange} suggestions={entry?.enum} placeholder="add a value" />;
  }
  if (entry?.type === "enum" && entry.enum) return <SelectField label="Value" value={String(value ?? "")} onChange={onChange} options={entry.enum.map((e) => ({ value: e, label: e }))} />;
  if (entry?.type === "bool") return <SelectField label="Value" value={String(value ?? true)} onChange={(v) => onChange(v === "true")} options={[{ value: "true", label: "true" }, { value: "false", label: "false" }]} />;
  if (entry?.type === "int") return <NumberField label="Value" value={typeof value === "number" ? value : 0} onChange={(v) => onChange(Number(v))} />;
  return <TextField value={String(value ?? "")} onChange={onChange} placeholder="value" />;
}

export { isLeaf };

// ── FlowSettings.tsx ────────────────────────────────────────────
/** Everything about the flow as a whole: what it is, how it ends, what it needs, what it asks. */
export function FlowSettings({ model, edit, vocabulary, agents }: {
  model: FlowModel; edit: (fn: (m: FlowModel) => FlowModel) => void; vocabulary: FlowVocabulary; agents: string[];
}) {
  const patch = (p: Partial<FlowModel>) => edit((m) => {
    const next = { ...m, ...p } as unknown as Record<string, unknown>;
    for (const k of Object.keys(next)) if (next[k] === undefined || next[k] === "") delete next[k];
    return next as unknown as FlowModel;
  });
  const produced = [...new Set(model.lanes.flatMap((l) => [l.step, ...(l.ladder ?? [])].flatMap((s) => s?.produces ?? [])))];
  const requires = model.requires ?? [];
  const setRequires = (r: Requirement[]) => patch({ requires: r.length ? r : undefined });

  return <div className="space-y-5" data-testid="flow-settings">
    <Section title="About this flow">
      <Field label="Title" hint="What people see in the library and the page header. The name below stays the identifier.">
        <TextField value={model.title ?? ""} onChange={(v) => patch({ title: v })} placeholder="Triage Bugs" label="Title" />
      </Field>
      <Field label="Name" hint="The flow's identifier: loops, schedules and queues refer to it, so it cannot change."><TextField value={model.id} onChange={() => undefined} disabled mono /></Field>
      <Field label="Description" hint="One or two sentences on what this flow is for. It is what people read in the library.">
        <TextField multiline rows={3} value={model.description ?? ""} onChange={(v) => patch({ description: v })} placeholder="Turns a rough idea into an approved plan." />
      </Field>
    </Section>

    <Section title="When the run ends" hint="What the loop does once every lane has run.">
      <Segmented label="After the run" value={model.after === "converse" ? "converse" : "finish"} onChange={(v) => patch({ after: v === "converse" ? "converse" : undefined, converse: v === "converse" ? model.converse : undefined })}
        options={[{ value: "finish", label: "Finish", hint: "The loop is done." }, { value: "converse", label: "Keep the conversation open", hint: "The loop rests and the person can keep talking: every message starts a new turn with the agent below." }]} />
      {model.after === "converse" && <Field label="Who answers" hint="Leave empty for the general chat agent.">
        <SelectField label="Conversation agent" value={model.converse ?? ""} onChange={(v) => patch({ converse: v })} placeholder="The chat agent (default)" options={agents.map((a) => ({ value: a, label: a }))} />
      </Field>}
    </Section>

    <Section title="First message" hint="The text a run starts with. Leave empty to use what the person typed.">
      <TextField multiline rows={4} mono value={model.kickoff ?? ""} onChange={(v) => patch({ kickoff: v })} placeholder="Plan {{.Goal}}" />
    </Section>

    <Section title="Deliverables" hint="What the whole run exists to produce. A deliverable no lane can produce is refused when the flow is saved.">
      <Chips label="Deliverables" values={model.output ?? []} onChange={(v) => patch({ output: v })} suggestions={[...vocabulary.emitTypes, ...produced]} placeholder="goal, brief.md …" />
    </Section>

    <Section title="Before it can start" hint="The loop starts only when every requirement below holds; a requirement holds if any one of its options does."
      action={<Button size="xs" variant="outline" onClick={() => setRequires([...requires, { anyOf: [{ type: vocabulary.requireTypes[0] ?? "goal", state: vocabulary.requireStates[0] ?? "approved" }] }])}>+ Requirement</Button>}>
      {requires.length === 0 && <p className="text-xs text-slate-500">None — it can start any time.</p>}
      {requires.map((r, i) => <Card key={i}>
        <p className="text-xs opacity-70">Any one of:</p>
        {r.anyOf.map((t, j) => <div key={j} className="flex items-center gap-2">
          <SelectField label="Type" value={t.type} onChange={(v) => setRequires(requires.map((x, a) => (a === i ? { anyOf: x.anyOf.map((y, b) => (b === j ? { ...y, type: v } : y)) } : x)))} options={vocabulary.requireTypes.map((o) => ({ value: o, label: `a ${o}` }))} />
          <span className="text-sm text-slate-500">that is</span>
          <SelectField label="State" value={t.state} onChange={(v) => setRequires(requires.map((x, a) => (a === i ? { anyOf: x.anyOf.map((y, b) => (b === j ? { ...y, state: v } : y)) } : x)))} options={vocabulary.requireStates.map((o) => ({ value: o, label: o }))} />
          <RemoveButton label="Remove option" onClick={() => setRequires(requires.flatMap((x, a) => (a !== i ? [x] : x.anyOf.length > 1 ? [{ anyOf: x.anyOf.filter((_, b) => b !== j) }] : [])))} />
        </div>)}
        <div className="flex gap-2">
          <Button size="xs" variant="outline" onClick={() => setRequires(requires.map((x, a) => (a === i ? { anyOf: [...x.anyOf, { type: vocabulary.requireTypes[0] ?? "goal", state: vocabulary.requireStates[0] ?? "approved" }] } : x)))}>+ Or</Button>
          <Button size="xs" variant="ghost" onClick={() => setRequires(requires.filter((_, a) => a !== i))}>Remove requirement</Button>
        </div>
      </Card>)}
    </Section>

    <Questions model={model} edit={edit} />

    <Section title="If the run is aborted" hint="A lane that cleans up after an abort, such as removing a half-made branch.">
      <SelectField label="Abort handler" value={model.onAbort ?? ""} onChange={(v) => patch({ onAbort: v })} placeholder="None" options={model.lanes.map((l) => ({ value: l.id, label: l.id }))} />
    </Section>
  </div>;
}

function Questions({ model, edit }: { model: FlowModel; edit: (fn: (m: FlowModel) => FlowModel) => void }) {
  const ids = Object.keys(model.questions);
  // A rename is written when the field loses focus, and only if the new name is free.
  const [drafts, setDrafts] = useState<Record<string, string>>({});
  const commit = (id: string) => {
    const v = (drafts[id] ?? id).trim();
    if (v !== id && validId(v) && !(v in model.questions)) edit((m) => renameQuestion(m, id, v));
    setDrafts(({ [id]: _gone, ...rest }) => rest);
  };
  return <Section title="Questions" hint="What approval gates ask a person. Written once, asked by any gate."
    action={<Button size="xs" variant="outline" onClick={() => edit((m) => setQuestion(m, uniqueName("question", Object.keys(m.questions)), { prompt: "", answers: ["approve", "reject"] }))}>+ Question</Button>}>
    {ids.length === 0 && <p className="text-xs text-slate-500">None yet. A gate step asks one of these.</p>}
    {ids.map((id) => {
      const q = model.questions[id]!;
      const used = model.lanes.some((l) => [l.step, ...(l.ladder ?? [])].some((s) => s?.question === id));
      return <Card key={id}>
        <div className="flex items-center gap-2">
          <div className="w-44" onBlur={() => commit(id)}><TextField label={`Question id ${id}`} mono value={drafts[id] ?? id} onChange={(v) => setDrafts({ ...drafts, [id]: v })} /></div>
          <span className="text-xs text-slate-500">{used ? "asked by a gate" : "not asked yet"}</span>
          <span className="ml-auto"><RemoveButton label={`Remove question ${id}`} onClick={() => edit((m) => removeQuestion(m, id))} /></span>
        </div>
        <TextField multiline rows={2} value={q.prompt} onChange={(v) => edit((m) => setQuestion(m, id, { ...q, prompt: v }))} placeholder="Approve the plan?" />
        <Chips mono={false} label={`Answers for ${id}`} values={q.answers} onChange={(v) => edit((m) => setQuestion(m, id, { ...q, answers: v }))} placeholder="approve" />
      </Card>;
    })}
  </Section>;
}
