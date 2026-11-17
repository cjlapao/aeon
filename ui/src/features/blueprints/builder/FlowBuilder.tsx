import { useMemo, useRef, useState, type DragEvent, Fragment } from "react";
import { Panel, DropdownMenu, IconButton, Pill, Button, TRUE_COLOR_HEX } from "../../../components/kit";
import { useAppSettings } from "../../../lib/appSettings";
import { LiveController, LiveState } from "../../../lib/live";
import { useRpcQuery } from "../../../lib/useRpcQuery";
import { Callout, Busy, KitIcon, Field, RemoveButton, SelectField, Section, Pressable } from "./fields";
import { FlowSettings, LaneInspector, StepInspector, InspectorProps } from "./FlowInspector";
import { FlowGraph } from "./FlowGraph";
import { ProblemList } from "./FlowWorkspace";
import { setQuestion, uniqueName, updateStep, rejectTo, Diagnostic, FlowModel, FlowVocabulary, Selection, describeCondition, laneIds, stepHeadline, blankLadderLane, blankLane, insertLane, moveLane, judgeMove, Lane, OnReject, Step, StepKind, judgeRungMove, addLadderStep, moveLadderStep, removeLadderStep, updateLane, DropVerdict } from "./flowModel";

// ── FlowBuilder.tsx ─────────────────────────────────────────────
/**
 * The flow builder: the lanes as a list you can drag, and an inspector for whatever is
 * selected. It knows nothing of the page around it — it takes a model and a way to
 * change it, and fetches the vocabulary and the agents it offers for itself, so the
 * page that hosts it loads it only when someone starts editing.
 */
export interface FlowBuilderProps {
  model: FlowModel;
  edit: (fn: (m: FlowModel) => FlowModel) => void;
  selection: Selection;
  onSelect: (s: Selection) => void;
  /** What the daemon found on each lane, and the whole list of what it found. */
  issues: Record<string, Diagnostic[]>;
  problems: Diagnostic[];
  /** Why the flow's text could not be read into this model, when it could not. */
  problem?: string;
  controller: LiveController;
  live: LiveState;
}

/** What the inspectors need: the vocabulary and the agents, fetched here, and the contexts built from them. Undefined while loading. */
export function useInspectorProps({ model, edit, selection, onSelect, controller, live }: Pick<FlowBuilderProps, "model" | "edit" | "selection" | "onSelect" | "controller" | "live">): { props: InspectorProps; vocabulary: FlowVocabulary; agents: string[] } | undefined {
  const vocab = useRpcQuery("blueprint.vocabulary" as never, {} as never, controller, live) as unknown as { data?: FlowVocabulary };
  const agentList = useRpcQuery("agent.list", {}, controller, live);
  const agents = useMemo(() => (agentList.data?.agents ?? []).map((a) => a.name).sort(), [agentList.data]);
  const vocabulary = vocab.data;

  const stepCtx = useMemo(() => (vocabulary ? {
    vocabulary, agents, questions: model.questions,
    onNewQuestion: () => {
      const id = uniqueName("question", Object.keys(model.questions));
      edit((m) => {
        const withQ = setQuestion(m, id, { prompt: "", answers: ["approve", "reject"] });
        if (selection.type === "lane") return updateStep(withQ, selection.laneId, null, { question: id });
        if (selection.type === "step") return updateStep(withQ, selection.laneId, selection.index, { question: id });
        return withQ;
      });
    },
    onEditQuestion: (id: string, q: { prompt: string; answers: string[] }) => edit((m) => setQuestion(m, id, q)),
  } : undefined), [vocabulary, model.questions, agents, edit, selection]);

  const condCtx = useMemo(() => {
    const counters = new Set<string>();
    for (const l of model.lanes) for (const r of [l.onReject, l.step?.onReject, ...(l.ladder ?? []).map((s) => s.onReject)]) if (r?.counter) counters.add(r.counter);
    return { fields: vocabulary?.fields ?? [], counters: [...counters], questions: Object.keys(model.questions) };
  }, [vocabulary, model]);

  if (!vocabulary || !stepCtx) return undefined;
  return { props: { model, edit, vocabulary, agents, select: onSelect, stepCtx, condCtx }, vocabulary, agents };
}

/** The inspector for whatever is selected: the flow's settings, a lane, or a step. Shared by the list builder and the graph editor. */
export function InspectorPanel({ selection, inspector }: { selection: Selection; inspector: NonNullable<ReturnType<typeof useInspectorProps>> }) {
  const { surfaceTone } = useAppSettings();
  const { props, vocabulary, agents } = inspector;
  return <Panel variant="simple" transparency="none" tone={surfaceTone} padding="sm" clickable={false} aria-label="Inspector" className="flex h-full min-h-0 flex-col" bodyClassName="min-h-0 flex-1 overflow-y-auto pr-1">
    <h2 className="mb-3 text-xs font-semibold uppercase tracking-widest opacity-70">{selection.type === "settings" ? "Flow settings" : selection.type === "lane" ? "Lane" : "Step"}</h2>
    {selection.type === "settings" && <FlowSettings model={props.model} edit={props.edit} vocabulary={vocabulary} agents={agents} />}
    {selection.type === "lane" && <LaneInspector laneId={selection.laneId} {...props} />}
    {selection.type === "step" && <StepInspector laneId={selection.laneId} index={selection.index} {...props} />}
  </Panel>;
}

export default function FlowBuilder({ model, edit, selection, onSelect, issues, problems, problem, controller, live }: FlowBuilderProps) {
  const inspector = useInspectorProps({ model, edit, selection, onSelect, controller, live });
  if (!inspector) return <Busy label="Loading the builder…" />;
  const focus = (lane: string | undefined) => onSelect(lane ? { type: "lane", laneId: lane } : { type: "settings" });

  return <div className="grid h-full min-h-0 gap-3 lg:grid-cols-[minmax(0,1fr)_minmax(340px,440px)]" data-testid="flow-builder">
    <div className="flex min-h-0 min-w-0 flex-col gap-3">
      {problem && <Callout tone="error">{problem}</Callout>}
      <div className="min-h-0 flex-1"><LaneCanvas model={model} selection={selection} select={onSelect} edit={edit} issues={issues} /></div>
      <ProblemList problems={problems} model={model} onFocus={focus} />
    </div>
    <InspectorPanel selection={selection} inspector={inspector} />
  </div>;
}

/**
 * The flow as the connection graph, editable: click a lane or a step in the graph and its inspector opens beside it, the
 * same inspector the list builder uses, so every change is made on the picture itself. Adding, removing and reordering
 * lanes stay in the list view (the other tab of the editor).
 */
export function FlowGraphEditor({ model, edit, selection, onSelect, issues, problems, problem, controller, live, loading }: FlowBuilderProps & { loading: boolean }) {
  const inspector = useInspectorProps({ model, edit, selection, onSelect, controller, live });
  const focus = (lane: string | undefined) => onSelect(lane ? { type: "lane", laneId: lane } : { type: "settings" });
  return <div className="grid h-full min-h-0 gap-3 lg:grid-cols-[minmax(0,1fr)_minmax(340px,440px)]" data-testid="flow-graph-editor">
    <div className="flex min-h-0 min-w-0 flex-col gap-3">
      {problem && <Callout tone="error">{problem}</Callout>}
      <div className="min-h-0 flex-1"><FlowGraph loading={loading} model={model} selection={selection} select={onSelect} issues={issues}
        onCreate={(placement) => {
          // A new lane from a node's side: before it from the left or top, after it from the right or bottom.
          const from = model.lanes.findIndex((l) => l.id === placement.sourceId.split("/")[0]);
          if (from < 0) return;
          const id = uniqueName("lane", model.lanes.map((l) => l.id));
          edit((m) => insertLane(m, placement.side === "left" || placement.side === "top" ? from : from + 1, blankLane(id, "model-step")));
          onSelect({ type: "lane", laneId: id });
        }}
        onConnect={({ sourceId, targetId }) => {
          // A link drawn from one lane to another is "when this is rejected, go there" (a rung inside a ladder is not a lane: skipped).
          if (sourceId.includes("/") || targetId.includes("/") || sourceId === targetId) return;
          const lane = model.lanes.find((l) => l.id === sourceId);
          if (!lane) return;
          if (lane.step?.onReject && !lane.onReject) edit((m) => updateStep(m, sourceId, null, { onReject: rejectTo(m, sourceId, targetId, lane.step?.onReject) }));
          else edit((m) => updateLane(m, sourceId, { onReject: rejectTo(m, sourceId, targetId, lane.onReject) }));
        }} /></div>
      <ProblemList problems={problems} model={model} onFocus={focus} />
    </div>
    {inspector ? <InspectorPanel selection={selection} inspector={inspector} /> : <Busy label="Loading the editor…" />}
  </div>;
}

// ── LaneCanvas.tsx ──────────────────────────────────────────────
// The flow as a column of lanes in the order they run, each a card that says what
// it does in a few words, with a "+" between any two to add one there. A card is
// clicked to edit it in the inspector, and dragged to move it. Every piece is the
// kit's — Panel, Pill, IconButton, DropdownMenu — in the app's tones.

export const KIND: Record<StepKind, { label: string; icon: string; tone: "sky" | "violet" | "amber"; bar: string }> = {
  "model-step": { label: "Agent step", icon: "Role", tone: "sky", bar: "bg-sky-500" },
  "code-action": { label: "Code action", icon: "Script", tone: "violet", bar: "bg-violet-500" },
  gate: { label: "Approval", icon: "Verified", tone: "amber", bar: "bg-amber-500" },
};

const TEXT: Record<string, string> = { sky: "text-sky-600 dark:text-sky-300", violet: "text-violet-600 dark:text-violet-300", amber: "text-amber-600 dark:text-amber-300", emerald: "text-emerald-600 dark:text-emerald-300" };

/** The kind of a step as a bare icon in its colour — no pill around it. */
export function KindIcon({ kind, ladder, size = "sm" }: { kind: StepKind; ladder?: boolean; size?: "xs" | "sm" }) {
  const label = ladder ? "Repeat for each" : KIND[kind].label;
  return <span role="img" aria-label={label} title={label} className={`inline-flex shrink-0 ${TEXT[ladder ? "emerald" : KIND[kind].tone]}`}><KitIcon name={ladder ? "Restart" : KIND[kind].icon} size={size} /></span>;
}

const NEUTRAL_TONES = new Set(["slate", "gray", "zinc", "neutral", "stone"]);

/**
 * What a selected card wears: the focus ring in the theme's selected colour, as it was, and a
 * tint over the panel's own background. A neutral tone is the surface's own colour, so by its
 * tone alone a selected card could not be told from the rest; the tint is the text colour at
 * a low alpha (darker on a light theme, lighter on a dark one), a coloured tone tints in its
 * own colour. It is a layer over the variant's background, not a replacement for it.
 */
export function selectionStyle(tone: string): { boxShadow: string; backgroundImage: string } {
  const hex = TRUE_COLOR_HEX[tone] ?? "#6366f1";
  const tint = NEUTRAL_TONES.has(tone) ? "color-mix(in srgb, currentColor 9%, transparent)" : `color-mix(in srgb, ${hex} 14%, transparent)`;
  return { boxShadow: `0 0 0 2px ${hex}66`, backgroundImage: `linear-gradient(${tint}, ${tint})` };
}

export type NewLaneKind = StepKind | "ladder";
const ADD_CHOICES: { kind: NewLaneKind; label: string; hint: string }[] = [
  { kind: "model-step", label: "Agent step", hint: "An agent does the work" },
  { kind: "code-action", label: "Code action", hint: "The daemon runs an action" },
  { kind: "gate", label: "Approval", hint: "A person decides" },
  { kind: "ladder", label: "Repeat for each", hint: "A chain of steps per task" },
];

function rejectLine(r: OnReject | undefined, laneId: string, ids: string[]): string {
  if (!r || (!r.goto && !r.then)) return "";
  const back = r.goto ? ids.indexOf(r.goto) <= ids.indexOf(laneId) : false;
  const head = r.goto ? `${back ? "↩ back to" : "↪ on to"} ${r.goto}` : "On rejection";
  const bound = r.goto && back && r.max ? ` · up to ${r.max} times` : "";
  const tail = r.then ? `${r.goto ? ", then" : ":"} ${r.then}` : "";
  return `${head}${bound}${tail}`;
}

export function LaneCanvas({ model, selection, select, edit, issues }: {
  model: FlowModel; selection: Selection; select: (s: Selection) => void; edit: (fn: (m: FlowModel) => FlowModel) => void; issues: Record<string, Diagnostic[]>;
}) {
  const { surfaceTone, controlTone } = useAppSettings();
  const ids = laneIds(model);
  const add = (index: number, kind: NewLaneKind) => {
    const id = uniqueName(kind === "ladder" ? "build" : kind === "gate" ? "approval" : kind === "code-action" ? "action" : "step", ids);
    edit((m) => insertLane(m, index, kind === "ladder" ? blankLadderLane(id) : blankLane(id, kind)));
    select({ type: "lane", laneId: id });
  };
  const drag = useLaneDrag(ids, (from, to) => edit((m) => moveLane(m, from, to)), (from, to) => judgeMove(model, from, to));
  const rest = model.lanes.filter((l) => l.id !== drag.dragId);
  const refused = drag.verdict && !drag.verdict.ok ? drag.verdict.reason : "";
  // The ghost is a lane's worth of space plus the "+" under it, drawn faint, so the gaps
  // read the same as they will once the lane lands.
  const ghost = <div key="ghost" {...drag.ghost}>
    <Panel data-testid="lane-ghost" data-refused={refused ? "true" : undefined} variant="simple" tone={refused ? "rose" : controlTone} padding="sm" clickable={false}
      className={`border-2 border-dashed ${refused ? "cursor-not-allowed" : ""}`} bodyClassName="flex items-center justify-center text-center text-sm" style={{ minHeight: drag.ghostHeight }}>
      {refused && <span role="alert">Can’t go here. {refused}</span>}
    </Panel>
    <Connector ghost onAdd={() => undefined} />
  </div>;
  const settingsOn = selection.type === "settings";
  // The selected thing wears the theme's selected colour; the rest wear the surface tone.
  const selectedRing = selectionStyle(controlTone);
  // A long flow scrolls inside its own box, and a drag near the box's top or bottom
  // edge scrolls it, so a lane can be carried past what is on screen.
  const scroller = (e: DragEvent<HTMLElement>) => {
    if (drag.dragId === null) return;
    const r = e.currentTarget.getBoundingClientRect();
    if (e.clientY < r.top + EDGE) e.currentTarget.scrollBy({ top: -STEP });
    else if (e.clientY > r.bottom - EDGE) e.currentTarget.scrollBy({ top: STEP });
  };
  // The "+" on either side of the carried lane steps aside; the rest stay.
  const carried = drag.dragId === null ? -1 : ids.indexOf(drag.dragId);
  return <div className="h-full min-h-[12rem] w-full overflow-y-auto overscroll-contain [scrollbar-gutter:stable]" data-testid="lane-canvas" onDragOver={scroller}>
    <div className="w-full px-3 py-2">
    <Panel variant="simple" tone={settingsOn ? controlTone : surfaceTone} padding="sm" onClick={() => select({ type: "settings" })} aria-pressed={settingsOn}
      style={settingsOn ? selectedRing : undefined}>
      <div className="flex items-center justify-between gap-3">
        <span><span className="block text-xs font-semibold uppercase tracking-wide opacity-70">Flow</span>
          <span className="block text-sm font-medium">{model.description || "Add a description, questions, deliverables and how the run ends"}</span></span>
        <Pill size="sm" tone="slate">{model.after === "converse" ? "keeps talking" : "finishes"}</Pill>
      </div>
    </Panel>
    <Connector last={model.lanes.length === 0} hidden={carried === 0} onAdd={(k) => add(0, k)} />
    {model.lanes.length === 0 && <Panel variant="outlined" padding="md" clickable={false} className="border-dashed text-center text-sm opacity-70">No lanes yet. Add the first step above.</Panel>}
    {model.lanes.map((lane, i) => {
      const before = drag.dragId !== null && drag.slot !== null && rest[drag.slot]?.id === lane.id;
      return <div key={lane.id}>
        {before && ghost}
        <LaneCard lane={lane} index={i} total={model.lanes.length} ids={ids} model={model} selection={selection} select={select} problems={issues[lane.id] ?? []}
          onMove={(to) => edit((m) => moveLane(m, i, to))} dragging={drag.dragId === lane.id} dragProps={drag.card(lane.id)} />
        <Connector last={i === model.lanes.length - 1} hidden={carried === i || carried === i + 1} onAdd={(k) => add(i + 1, k)} />
      </div>;
    })}
    {drag.dragId !== null && drag.slot !== null && drag.slot >= rest.length && ghost}
    </div>
  </div>;
}

/**
 * One lane as a card. `locked` is a shipped flow's card: it can be selected but not moved — no
 * drag, no arrows — and `routeChip` names the route a person chose for it, when one was chosen.
 */
export function LaneCard({ lane, index, total, ids, model, selection, select, problems, onMove, dragging, dragProps, locked, routeChip }: {
  lane: Lane; index: number; total: number; ids: string[]; model: FlowModel; selection: Selection; select: (s: Selection) => void; problems: Diagnostic[]; onMove: (to: number) => void; dragging: boolean; dragProps?: ReturnType<LaneDrag["card"]>; locked?: boolean; routeChip?: string;
}) {
  const on = selection.type !== "settings" && selection.laneId === lane.id;
  const step = lane.step;
  const kind = step?.kind ?? "model-step";
  const k = KIND[kind];
  const when = describeCondition(lane.when ?? step?.when);
  const reject = rejectLine(lane.onReject ?? step?.onReject, lane.id, ids);
  const invalid = problems.some((p) => p.state === "invalid");
  const { surfaceTone, controlTone } = useAppSettings();
  // The selected card wears the theme's selected colour (a ring in it, and its tone); the rest wear the surface tone.
  const ring = on ? selectionStyle(controlTone) : undefined;
  // The accent is a bar beside the card, short of both corners, so it never runs into the rounded edge.
  return <div className="relative"><span aria-hidden className={`absolute bottom-4 left-0 top-4 z-10 w-1 rounded-full transition-opacity ${dragging ? "opacity-10" : ""} ${lane.ladder ? "bg-emerald-500" : k.bar}`} />
    <Panel variant="simple" tone={invalid ? "rose" : on ? controlTone : surfaceTone} padding="sm" clickable={false} style={ring} data-testid="lane-card" data-lane={lane.id} {...(locked ? {} : dragProps ?? {})}
    aria-current={on && selection.type === "lane" ? "true" : undefined}
    className={`group relative transition ${locked ? "cursor-pointer" : "cursor-grab active:cursor-grabbing"} ${dragging ? "opacity-10" : ""}`}>
    <Pressable  onPress={() => select({ type: "lane", laneId: lane.id })}>
      <div className="flex items-start gap-3">
        <span className="mt-0.5"><KindIcon kind={kind} ladder={!!lane.ladder} /></span>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-mono text-sm font-semibold">{lane.id}</span>
            <span className="text-xs opacity-70">{lane.ladder ? `Repeat for each ${lane.iterate || "item"}` : k.label}</span>
            {lane.advisory && <Pill size="xs" tone="slate" uppercase>advisory</Pill>}
            {routeChip && <Pill size="xs" tone="indigo" data-testid="route-chip">{routeChip}</Pill>}
            {problems.length > 0 && <span className="ml-auto"><Pill size="xs" tone={invalid ? "rose" : "amber"}>{problems.length} {problems.length === 1 ? "issue" : "issues"}</Pill></span>}
          </div>
          {!lane.ladder && <p className="mt-1 truncate text-sm">{stepHeadline(step, model.questions)}</p>}
          {step && <Detail step={step} />}
          {when && <p className="mt-1.5 text-xs opacity-70">Only when {when}</p>}
          {reject && <p className="mt-1.5 text-xs font-medium text-rose-700 dark:text-rose-300">{reject}</p>}
        </div>
      </div>
    </Pressable>
    {lane.ladder && <ol className="mx-2 mb-1 mt-2 space-y-1 border-l pl-3 border-current/10">
      {lane.ladder.map((s, i) => <li key={s.id ?? i}><button type="button" data-testid="rung" onClick={() => select({ type: "step", laneId: lane.id, index: i })}
        className={`flex w-full items-center gap-2 rounded-md px-2 py-1 text-left text-xs transition hover:bg-current/5 ${selection.type === "step" && selection.laneId === lane.id && selection.index === i ? "bg-indigo-500/10 ring-1 ring-indigo-500/40" : ""}`}>
        <KindIcon kind={s.kind} size="xs" />
        <span className="font-mono">{s.id}</span><span className="truncate opacity-60">{stepHeadline(s, model.questions)}</span>
        {lane.rework === s.id && <span className="ml-auto"><Pill size="xs" tone="slate">rework</Pill></span>}
      </button></li>)}
    </ol>}
    {!locked && <div className="absolute right-2 top-2 flex gap-0.5 opacity-0 transition group-focus-within:opacity-100 group-hover:opacity-100">
      <IconButton icon="ArrowUp" size="xs" variant="ghost" srLabel={`Move ${lane.id} up`} tooltip="Move up" disabled={index === 0} onClick={() => onMove(index - 1)} />
      <IconButton icon="ArrowDown" size="xs" variant="ghost" srLabel={`Move ${lane.id} down`} tooltip="Move down" disabled={index === total - 1} onClick={() => onMove(index + 1)} />
    </div>}
  </Panel></div>;
}

function Detail({ step }: { step: Step }) {
  const files = step.produces ?? [];
  const emits = step.emits ?? [];
  if (files.length === 0 && emits.length === 0 && !step.decides && !step.sets) return null;
  return <div className="mt-1.5 flex flex-wrap gap-1.5">
    {files.map((f) => <Pill key={f} size="xs" tone="slate" className="font-mono">writes {f}</Pill>)}
    {emits.map((e, i) => <Pill key={i} size="xs" tone="indigo">files a {e.type}</Pill>)}
    {step.decides && <Pill size="xs" tone="amber">decides the {step.decides}</Pill>}
    {step.sets && <Pill size="xs" tone="slate" className="font-mono">sets {step.sets}</Pill>}
  </div>;
}

/** The line between two cards, with the "+" that adds a lane at that place. */
const EDGE = 56;
const STEP = 18;

function Connector({ onAdd, hidden, ghost, last }: { onAdd: (kind: NewLaneKind) => void; hidden?: boolean; ghost?: boolean; last?: boolean }) {
  const [open, setOpen] = useState(false);
  const anchor = useRef<HTMLSpanElement>(null);
  // A "+" beside the lane being carried steps aside, so only the ghost sits there.
  return <div className={`relative flex flex-col items-center py-1 transition-opacity ${hidden ? "pointer-events-none opacity-0" : ghost ? "pointer-events-none opacity-30" : ""}`} aria-hidden={hidden || ghost || undefined}>
    <span className="h-3 w-px bg-current opacity-20" />
    <span ref={anchor}><IconButton icon="Add" size="xs" variant="soft" rounded="full" srLabel="Add a lane here" tooltip="Add a lane here" aria-expanded={open} onClick={() => setOpen((v) => !v)} /></span>
    {/* The last "+" ends the line: nothing follows it. */}
    {!last && <span className="h-3 w-px bg-current opacity-20" />}
    <DropdownMenu anchorRef={anchor} open={open && !hidden} onClose={() => setOpen(false)} width={256}
      items={ADD_CHOICES.map((c) => ({ value: c.kind, label: c.label, description: c.hint }))}
      onSelect={(item) => { setOpen(false); onAdd(item.value as NewLaneKind); }} />
  </div>;
}

// ── LadderEditor.tsx ────────────────────────────────────────────
// The steps a ladder walks for every item. Each rung is a card you can drag to
// reorder — with the same ghost, and the same refusal of a move that would leave a
// rejection looping with no bound, as the lanes themselves — open to edit, or remove.

const ADD: { kind: StepKind; hint: string }[] = [
  { kind: "model-step", hint: "An agent does the work" },
  { kind: "code-action", hint: "The daemon runs an action" },
  { kind: "gate", hint: "A person decides" },
];

function rejectText(s: Step, ids: string[], index: number): string {
  const r = s.onReject;
  if (!r?.goto) return r?.then ? `on rejection: ${r.then}` : "";
  const back = ids.indexOf(r.goto) <= index;
  return `${back ? "↩ back to" : "↪ on to"} ${r.goto}${back && r.max ? ` · up to ${r.max} times` : ""}`;
}

export function LadderEditor({ lane, edit, vocabulary, select, model }: InspectorProps & { lane: Lane }) {
  const rungs = lane.ladder ?? [];
  const ids = rungs.map((s, i) => s.id ?? String(i));
  const collections = vocabulary.fields.filter((f) => f.type === "collection").map((f) => f.path);
  const { surfaceTone, controlTone } = useAppSettings();
  const [menu, setMenu] = useState(false);
  const anchor = useRef<HTMLSpanElement>(null);
  const drag = useLaneDrag(ids, (from, to) => edit((m) => moveLadderStep(m, lane.id, from, to)), (from, to) => judgeRungMove(rungs, from, to));
  const rest = ids.filter((id) => id !== drag.dragId);
  const refused = drag.verdict && !drag.verdict.ok ? drag.verdict.reason : "";
  const ghost = <li key="ghost" {...drag.ghost}>
    <Panel data-testid="rung-ghost" data-refused={refused ? "true" : undefined} variant="outlined" tone={refused ? "rose" : controlTone} padding="xs" corner="rounded-lg" clickable={false}
      className={`border-2 border-dashed ${refused ? "cursor-not-allowed" : ""}`} bodyClassName="flex items-center justify-center text-center text-xs" style={{ minHeight: drag.ghostHeight }}>
      {refused && <span role="alert">Can’t go here. {refused}</span>}
    </Panel>
  </li>;
  const add = (kind: StepKind) => { edit((m) => addLadderStep(m, lane.id, rungs.length, kind)); select({ type: "step", laneId: lane.id, index: rungs.length }); };

  return <Section title="Steps for each item" hint="Run in order for every item. Drag to reorder; open a step to edit it."
    action={<span ref={anchor}><Button size="xs" variant="outline" leadingIcon="Add" aria-expanded={menu} onClick={() => setMenu((v) => !v)}>Step</Button></span>}>
    <DropdownMenu anchorRef={anchor} open={menu} onClose={() => setMenu(false)} width={240} align="end"
      items={ADD.map((a) => ({ value: a.kind, label: KIND[a.kind].label, description: a.hint }))}
      onSelect={(item) => { setMenu(false); add(item.value as StepKind); }} />
    <Field label="Repeat for each" hint="The collection of items this lane walks.">
      <SelectField label="Collection" value={lane.iterate ?? ""} onChange={(v) => edit((m) => updateLane(m, lane.id, { iterate: v }))} options={(collections.length ? collections : ["tasks"]).map((c) => ({ value: c, label: c }))} />
    </Field>
    <ol className="space-y-1.5" data-testid="rung-list">
      {rungs.map((s, i) => {
        const id = ids[i]!;
        const before = drag.dragId !== null && drag.slot !== null && rest[drag.slot] === id;
        const k = KIND[s.kind];
        const reject = rejectText(s, ids, i);
        return <Fragment key={id}>
          {before && ghost}
          <li {...drag.card(id)}>
            <Panel data-testid="rung" variant="subtle" tone={surfaceTone} padding="xs" corner="rounded-lg" clickable={false}
              className={`group cursor-grab active:cursor-grabbing ${drag.dragId === id ? "opacity-10" : ""}`} bodyClassName="flex items-center gap-2">
              <span aria-hidden className="opacity-50"><KitIcon name="Drag" size="xs" /></span>
              <KindIcon kind={s.kind} />
              <Pressable className="min-w-0 flex-1 text-left" onPress={() => select({ type: "step", laneId: lane.id, index: i })}>
                <span className="flex items-center gap-2"><span className="font-mono text-sm font-medium">{s.id}</span><span className="text-xs opacity-60">{k.label}</span>
                  {lane.rework === s.id && <Pill size="xs" tone="slate">rework</Pill>}</span>
                <span className="block truncate text-xs opacity-80">{stepHeadline(s, model.questions)}</span>
                {reject && <span className="block text-[11px] font-medium text-rose-600 dark:text-rose-300">{reject}</span>}
              </Pressable>
              <IconButton icon="ArrowUp" size="xs" variant="ghost" srLabel={`Move ${s.id} up`} tooltip="Move up" disabled={i === 0} onClick={() => edit((m) => moveLadderStep(m, lane.id, i, i - 1))} />
              <IconButton icon="ArrowDown" size="xs" variant="ghost" srLabel={`Move ${s.id} down`} tooltip="Move down" disabled={i === rungs.length - 1} onClick={() => edit((m) => moveLadderStep(m, lane.id, i, i + 1))} />
              <RemoveButton label={`Remove ${s.id}`} disabled={rungs.length === 1} onClick={() => edit((m) => removeLadderStep(m, lane.id, i))} />
            </Panel>
          </li>
        </Fragment>;
      })}
      {drag.dragId !== null && drag.slot !== null && drag.slot >= rest.length && ghost}
    </ol>
    <Field label="Rework step" hint="Where an item restarts after a rejection. Optional.">
      <SelectField label="Rework step" value={lane.rework ?? ""} onChange={(v) => edit((m) => updateLane(m, lane.id, { rework: v }))} placeholder="None" options={rungs.map((s) => ({ value: s.id ?? "", label: s.id ?? "" }))} />
    </Field>
  </Section>;
}

// ── useLaneDrag.ts ──────────────────────────────────────────────
// Moving a lane by dragging it, the way the kit's grid editor moves a tile: the card
// being moved dims in place, a clone of it follows the pointer, and a ghost the size
// of the card shows where it will land. The ghost owns its own dragover and drop, so
// it does not flicker out from under the pointer when it pushes the cards apart.

export interface LaneDrag {
  dragId: string | null;
  /** Where the ghost sits: before the lane at this index of the lanes without the dragged one (length = the end). */
  slot: number | null;
  ghostHeight: number;
  /** What dropping at the current slot would do; null while there is no slot. */
  verdict: DropVerdict | null;
  card: (id: string) => { draggable: true; onDragStart: (e: DragEvent<HTMLElement>) => void; onDragEnd: () => void; onDragEnter: (e: DragEvent<HTMLElement>) => void; onDragOver: (e: DragEvent<HTMLElement>) => void; onDrop: (e: DragEvent<HTMLElement>) => void };
  ghost: { onDragEnter: (e: DragEvent<HTMLElement>) => void; onDragOver: (e: DragEvent<HTMLElement>) => void; onDrop: (e: DragEvent<HTMLElement>) => void };
}

export function useLaneDrag(ids: string[], onMove: (from: number, to: number) => void, judge: (from: number, to: number) => DropVerdict = () => ({ ok: true })): LaneDrag {
  const [dragId, setDragId] = useState<string | null>(null);
  const [slot, setSlot] = useState<number | null>(null);
  const height = useRef(0);
  const others = ids.filter((id) => id !== dragId);

  const from = dragId === null ? -1 : ids.indexOf(dragId);
  const verdict = slot === null || from < 0 ? null : judge(from, slot);

  const reset = () => { setDragId(null); setSlot(null); };
  const commit = (e: DragEvent<HTMLElement>) => {
    e.preventDefault();
    if (dragId !== null && slot !== null && verdict?.ok) onMove(from, slot);
    reset();
  };

  return {
    dragId, slot, verdict, ghostHeight: height.current,
    card: (id) => ({
      draggable: true,
      onDragStart: (e) => {
        // A press on a control inside the card is a click, not a lift.
        if ((e.target as HTMLElement).closest("input, textarea, select")) { e.preventDefault(); return; }
        const el = e.currentTarget;
        height.current = el.getBoundingClientRect().height;
        if (e.dataTransfer) {
          e.dataTransfer.effectAllowed = "move";
          e.dataTransfer.setData("text/plain", id);
          const clone = el.cloneNode(true) as HTMLElement;
          Object.assign(clone.style, { position: "fixed", top: "-1000px", left: "0", width: `${el.getBoundingClientRect().width}px`, opacity: "0.92", boxShadow: "0 12px 32px rgba(0,0,0,.35)", pointerEvents: "none" });
          document.body.appendChild(clone);
          e.dataTransfer.setDragImage?.(clone, 16, 16);
          setTimeout(() => clone.remove(), 0);
        }
        setDragId(id);
      },
      onDragEnd: reset,
      // WebKit only treats an element as a drop target when dragenter, as well as dragover, is cancelled.
      onDragEnter: (e) => { if (dragId !== null && id !== dragId) e.preventDefault(); },
      onDragOver: (e) => {
        if (dragId === null || id === dragId) return;
        e.preventDefault();
        const r = e.currentTarget.getBoundingClientRect();
        const next = others.indexOf(id) + (e.clientY < r.top + r.height / 2 ? 0 : 1);
        // Where it already is is not a place to go: no ghost there.
        const target = next === from ? null : next;
        if (target !== slot) setSlot(target);
      },
      onDrop: commit,
    }),
    ghost: {
      onDragEnter: (e) => { if (dragId !== null && verdict?.ok) e.preventDefault(); },
      onDragOver: (e) => {
        if (dragId === null) return;
        if (verdict?.ok) e.preventDefault();
        else if (e.dataTransfer) e.dataTransfer.dropEffect = "none";
      },
      onDrop: commit,
    },
  };
}
