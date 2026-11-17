import { Suspense, lazy, useCallback, useEffect, useMemo, useState, ReactNode } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, Pill, ScrollArea, Tabs, ExpandableButton, IconButton, Textarea, EmptyState, Panel } from "../../../components/kit";
import { ErrorPanel } from "../../../components/ErrorPanel";
import { RpcError, rpcTyped, toRpcError } from "../../../lib/bridge";
import { useRpcQuery } from "../../../lib/useRpcQuery";
import { useTypeToConfirm } from "../../../lib/useConfirm";
import { LiveController, LiveState } from "../../../lib/live";
import { RpcMethods } from "../../../proto/contract";
import { RevisionsPanel } from "../panels/RevisionsPanel";
import { UsagePanel } from "../panels/UsagePanel";
import { canDelete, originBadge, saveOutcome, type BlueprintSaveRequest } from "../blueprintsModel";
import { FlowGraph } from "./FlowGraph";
import { Busy, Callout, Card, Field, TextField, Switch } from "./fields";
import { diagnosticsByLane, Selection, Diagnostic, FlowModel } from "./flowModel";
import { useFlowDraft } from "./useFlowDraft";
import { useAppSettings } from "../../../lib/appSettings";
import { Diagnostics } from "../panels/Diagnostics";
import { LintPanel } from "../panels/LintPanel";

// ── FlowWorkspace.tsx ───────────────────────────────────────────
const FlowBuilder = lazy(() => import("./FlowBuilder"));
const FlowGraphEditor = lazy(() => import("./FlowBuilder").then((m) => ({ default: m.FlowGraphEditor })));
const LockedFlow = lazy(() => import("./LockedFlow").then((m) => ({ default: m.LockedFlow })));

type Tab = "build" | "source" | "history" | "usage";

export interface FlowWorkspaceProps {
  name: string;
  controller: LiveController;
  live: LiveState;
  canWrite: boolean;
  onBack: () => void;
  onChanged?: () => void;
  onDuplicated?: (name: string) => void;
  onDeleted?: (name: string) => void;
  onDirtyChange?: (dirty: boolean) => void;
}

/** One flow, open for editing: its header, the builder, the source, its history and who uses it. */
export function FlowWorkspace({ name, controller, live, canWrite, onBack, onChanged, onDuplicated, onDeleted, onDirtyChange }: FlowWorkspaceProps) {
  const { t } = useI18n();
  const { controlTone } = useAppSettings();
  const show = useRpcQuery("blueprint.show", { name }, controller, live);
  const draft = useFlowDraft(name, show.data?.content ?? "");
  const { confirm, modal } = useTypeToConfirm();

  const [tab, setTab] = useState<Tab>("build");
  // A flow opens to be looked at: the graph, read-only. Edit turns it into the builder.
  const [editing, setEditing] = useState(false);
  // Editing happens on the connection graph (click a lane or step, its inspector opens beside it); the list view is the other way
  // in, and the one that adds, removes and reorders lanes.
  const [editView, setEditView] = useState<"graph" | "list">("graph");
  const [selection, setSelection] = useState<Selection>({ type: "settings" });
  const [note, setNote] = useState("");
  const [strict, setStrict] = useState(false);
  const [outcome, setOutcome] = useState<{ key: "saved" | "unchanged"; revision: number } | undefined>();
  const [writeError, setWriteError] = useState<RpcError | undefined>();
  const [deleteNote, setDeleteNote] = useState<string | undefined>();
  const [copyName, setCopyName] = useState<string | undefined>();
  const [overriding, setOverriding] = useState(false);

  useEffect(() => onDirtyChange?.(draft.dirty), [draft.dirty, onDirtyChange]);

  const origin = show.data?.origin;
  const badge = show.data ? originBadge(origin) : null;
  const model = draft.model;
  // A shipped flow is locked: its shape never changes, only what the daemon lists as overridable.
  const locked = show.data?.locked === true;
  const lockedInfo = useMemo(() => ({
    overridable: Object.fromEntries((show.data?.overridable ?? []).map((o) => [o.lane, o.fields])),
    overrides: (show.data?.overrides ?? []).reduce<Record<string, Record<string, string>>>((acc, o) => ({ ...acc, [o.lane]: { ...acc[o.lane], [o.field]: o.value } }), {}),
  }), [show.data]);

  const override = async (lane: string, field: string, value: string) => {
    if (!canWrite) return;
    setWriteError(undefined);
    setOverriding(true);
    try {
      await rpcTyped("blueprint.override", { name, lane, field, value });
      show.refetch();
    } catch (cause) {
      setWriteError(toRpcError(cause));
    } finally {
      setOverriding(false);
    }
  };

  const problems = (draft.report?.diagnostics ?? []).filter((d) => d.state !== "valid");
  const invalid = problems.filter((d) => d.state === "invalid").length;
  const split = useMemo(() => (model ? diagnosticsByLane(problems, model) : { byLane: {}, flow: [] }), [problems, model]);

  const save = async () => {
    if (!canWrite || !draft.dirty) return;
    setWriteError(undefined);
    try {
      const payload: BlueprintSaveRequest = { name, content: draft.text };
      if (note.trim() !== "") payload.note = note.trim();
      const saved = await rpcTyped("blueprint.save", payload as RpcMethods["blueprint.save"]["params"]);
      setOutcome(saveOutcome(saved));
      setNote("");
      show.refetch();
      onChanged?.();
    } catch (cause) {
      setWriteError(toRpcError(cause));
    }
  };

  const duplicate = async () => {
    const copy = (copyName ?? "").trim();
    if (!canWrite || copy === "") return;
    setWriteError(undefined);
    try {
      const created = await rpcTyped("blueprint.duplicate", { from: name, name: copy });
      setCopyName(undefined);
      onChanged?.();
      onDuplicated?.(created.name);
    } catch (cause) {
      setWriteError(toRpcError(cause));
    }
  };

  const remove = async () => {
    if (!canWrite) return;
    setWriteError(undefined);
    const agreed = await confirm({
      title: `Delete ${name}`,
      description: `${name} is removed from the library. Loops already started from it keep running. A flow that other flows, schedules or queues still use is refused, and the daemon names what uses it.`,
      confirmValue: name, confirmValueLabel: "Flow name", confirmLabel: "Delete flow",
    });
    if (!agreed) return;
    try {
      const result = await rpcTyped("blueprint.delete", { name });
      setDeleteNote(result.loopsStartedFromIt > 0 ? `${result.loopsStartedFromIt} loops already started from it keep what they have — ${result.note}` : undefined);
      onChanged?.();
      onDeleted?.(name);
    } catch (cause) {
      setWriteError(toRpcError(cause));
    }
  };

  const focusProblem = useCallback((lane: string | undefined) => {
    setTab("build");
    if (canWrite) setEditing(true);
    setSelection(lane ? { type: "lane", laneId: lane } : { type: "settings" });
  }, [canWrite]);


  const canSave = canWrite && draft.dirty && draft.state !== "broken" && draft.state !== "working";
  const loading = show.loading && !show.data && !show.error;

  return <div className="flex h-full min-h-0 flex-col gap-3" data-testid="flow-workspace">
    <FlowHeader name={name} title={model?.title || show.data?.title || name} editing={editing} locked={locked} dirty={draft.dirty} canWrite={canWrite} canSave={canSave} canDelete={canDelete({ origin })}
      description={model?.description} revision={`rev ${show.data?.revision} · v${show.data?.version}`} note={note} onNote={setNote}
      badges={<>
        {badge && <Pill size="sm" tone={badge.tone}>{t(badge.key === "shipped" ? "blueprints.drawer.shipped" : "blueprints.drawer.yours")}</Pill>}
        {show.data?.divergedFromShipped && <Pill size="sm" tone="amber">{t("blueprints.list.edited")}</Pill>}
      </>}
      verdict={<Verdict state={draft.state} invalid={invalid} unverified={problems.length - invalid} hasReport={!!draft.report} onOpen={() => setTab("source")} />}
      onBack={onBack} onEdit={() => { setEditing(true); setTab("build"); }} onDone={() => setEditing(false)} onDiscard={draft.discard}
      onSave={() => void save()} onDuplicate={() => setCopyName(`${name}-copy`)} onDelete={() => void remove()} />

    <div className="shrink-0 space-y-2 empty:hidden">
      {!canWrite && <Alert intent="warning" size="sm" title="Reconnecting to the daemon — changes are paused." data-testid="drawer-readonly" />}
      {writeError && <div data-testid="drawer-error"><ErrorPanel error={writeError} /></div>}
      {deleteNote && <Alert intent="info" size="sm" title={deleteNote} />}
      {outcome && <Alert intent="success" size="sm" title={outcome.key === "saved" ? `Saved as revision ${outcome.revision}.` : "Nothing changed — the flow is already saved this way."} data-testid="drawer-outcome" />}
      {copyName !== undefined && <Card><form className="flex flex-wrap items-end gap-2" data-testid="duplicate-form" onSubmit={(e) => { e.preventDefault(); void duplicate(); }}>
        <div className="w-72"><Field label="Name of the copy"><TextField mono value={copyName} onChange={setCopyName} label="Name of the copy" /></Field></div>
        <Button type="submit" size="sm" disabled={!canWrite} data-testid="duplicate-create">Create copy</Button>
        <Button type="button" size="sm" variant="ghost" onClick={() => setCopyName(undefined)}>Cancel</Button>
      </form></Card>}
    </div>

    {/* The kit's Tabs sizes itself to its container, so it gets one that is only as tall as the tab row. */}
    <div className="shrink-0"><Tabs variant="underline" size="sm" color={controlTone} items={[{ id: "build", label: "Flow" }, { id: "source", label: "Source" }, { id: "history", label: "History" }, { id: "usage", label: "Used by" }]} value={tab} onChange={(id) => setTab(id as Tab)} /></div>

    <div className="relative min-h-0 flex-1"><div className="absolute inset-0">
      {show.error && <ErrorPanel error={new RpcError(0, show.error)} onRetry={show.refetch} />}
      {!loading && !show.error && tab === "build" && !model && (draft.problem ? <Callout tone="error">{draft.problem} Fix it in the Source tab.</Callout> : <Busy label="Reading the flow…" />)}

      {!show.error && tab === "build" && model && !editing && <div className="flex h-full min-h-0 flex-col gap-3">
        <div className="min-h-0 flex-1"><FlowGraph loading={loading} model={model} selection={selection} select={setSelection} issues={split.byLane} /></div>
        <ProblemList problems={problems} model={model} onFocus={focusProblem} />
      </div>}

      {/* The builder is its own chunk: nothing of it loads until someone presses Edit. */}
      {!show.error && tab === "build" && model && editing && locked && <Suspense fallback={<Busy label="Loading…" />}>
        <LockedFlow model={model} info={lockedInfo} selection={selection} onSelect={setSelection} issues={split.byLane} problems={problems}
          onOverride={(l, f, v) => void override(l, f, v)} busy={overriding} onDuplicate={canWrite ? () => setCopyName(`${name}-copy`) : undefined} controller={controller} live={live} />
      </Suspense>}

      {!show.error && tab === "build" && model && editing && !locked && <div className="flex h-full min-h-0 flex-col gap-2">
        <div role="group" aria-label="Edit view" className="flex shrink-0 gap-1" data-testid="edit-view">
          {(["graph", "list"] as const).map((v) => <Button key={v} size="xs" variant={editView === v ? "solid" : "ghost"} aria-pressed={editView === v} onClick={() => setEditView(v)} data-testid={`edit-view-${v}`}>{v === "graph" ? "On the graph" : "As a list"}</Button>)}
        </div>
        <div className="min-h-0 flex-1"><Suspense fallback={<Busy label="Loading the editor…" />}>
          {editView === "graph"
            ? <FlowGraphEditor loading={loading} model={model} edit={draft.edit} selection={selection} onSelect={setSelection} issues={split.byLane} problems={problems} problem={draft.problem} controller={controller} live={live} />
            : <FlowBuilder model={model} edit={draft.edit} selection={selection} onSelect={setSelection} issues={split.byLane} problems={problems} problem={draft.problem} controller={controller} live={live} />}
        </Suspense></div>
      </div>}

      {!loading && !show.error && tab === "source" && <FlowSourcePane name={name} locked={locked} draft={draft} strict={strict} onStrict={setStrict} canRun={live.state === "current"} />}

      {!loading && !show.error && tab === "history" && <ScrollArea className="h-full min-h-0"><RevisionsPanel name={name} origin={origin} controller={controller} live={live} canWrite={canWrite} onChanged={() => { show.refetch(); onChanged?.(); }} /></ScrollArea>}
      {!loading && !show.error && tab === "usage" && <ScrollArea className="h-full min-h-0"><UsagePanel name={name} controller={controller} live={live} /></ScrollArea>}
    </div></div>
    {modal}
  </div>;
}

// ── FlowHeader.tsx ──────────────────────────────────────────────
// The part of an open flow that stays put while the panes below it scroll: where it
// is (back, name, badges), what state it is in (the verdict), and what can be done
// to it. The actions are the kit's expandable buttons — an icon at rest, its name on
// hover or focus — so the whole row costs one line.

export function Verdict({ state, invalid, unverified, hasReport, onOpen }: { state: string; invalid: number; unverified: number; hasReport: boolean; onOpen: () => void }) {
  if (state === "working" || !hasReport) return <Pill size="sm" tone="slate" data-testid="verdict">Checking…</Pill>;
  if (state === "broken") return <Button size="xs" color="rose" onClick={onOpen} data-testid="verdict">Cannot be read</Button>;
  if (invalid > 0) return <Button size="xs" color="rose" onClick={onOpen} data-testid="verdict">{invalid} {invalid === 1 ? "problem" : "problems"}</Button>;
  if (unverified > 0) return <Pill size="sm" tone="amber" data-testid="verdict">Valid · {unverified} unverified</Pill>;
  return <Pill size="sm" tone="emerald" data-testid="verdict">✓ Valid</Pill>;
}

export interface FlowHeaderProps {
  name: string;
  /** The friendly name shown as the heading; the slug rides beside it. */
  title: string;
  badges: ReactNode;
  revision: string;
  description?: string;
  verdict: ReactNode;
  editing: boolean;
  /** A shipped flow: customised, not edited. Nothing is saved from the header; each choice saves itself. */
  locked?: boolean;
  dirty: boolean;
  canWrite: boolean;
  canSave: boolean;
  canDelete: boolean;
  note: string;
  onNote: (v: string) => void;
  onBack: () => void;
  onEdit: () => void;
  onDone: () => void;
  onDiscard: () => void;
  onSave: () => void;
  onDuplicate: () => void;
  onDelete: () => void;
}

export function FlowHeader(p: FlowHeaderProps) {
  const { controlTone } = useAppSettings();
  return <header className="shrink-0 space-y-1">
    <div className="flex flex-wrap items-center gap-2">
      <IconButton icon="ArrowLeft" size="sm" variant="ghost" srLabel="All flows" tooltip="All flows" onClick={p.onBack} />
      <h1 className="text-xl font-semibold">{p.title}</h1>
      {p.title !== p.name && <span className="font-mono text-xs opacity-60">{p.name}</span>}
      {p.badges}
      <span className="text-xs opacity-60">{p.revision}</span>
      {p.dirty && <Pill size="sm" tone="amber">Unsaved changes</Pill>}
      <div className="ml-auto flex flex-wrap items-center gap-2">
        {p.verdict}
        {p.dirty && !p.locked && <div className="w-52"><TextField label="What changed" value={p.note} onChange={p.onNote} placeholder="What changed? (optional)" /></div>}
        {p.dirty && <ExpandableButton icon="Revert" label="Discard" size="sm" color={controlTone} onClick={p.onDiscard} data-testid="discard-draft" />}
        {p.editing
          ? <ExpandableButton icon="Check" label="Done" size="sm" color={controlTone} onClick={p.onDone} data-testid="done-editing" />
          : <ExpandableButton icon="Edit" label={p.locked ? "Customize" : "Edit"} size="sm" color={controlTone} disabled={!p.canWrite} onClick={p.onEdit} data-testid="edit-flow" />}
        {(p.editing || p.dirty) && !p.locked && <ExpandableButton icon="Save" label="Save" size="sm" color={controlTone} disabled={!p.canSave} onClick={p.onSave} data-testid="save-blueprint" />}
        {/* Copying or deleting a flow while it is being edited would leave a draft with nowhere to go. */}
        {!p.editing && <>
          <ExpandableButton icon="Copy" label="Duplicate" size="sm" color={controlTone} disabled={!p.canWrite} onClick={p.onDuplicate} data-testid="duplicate-blueprint" />
          <ExpandableButton icon="Trash" label="Delete" size="sm" color="rose" disabled={!p.canWrite || !p.canDelete} onClick={p.onDelete} data-testid="delete-blueprint"
          tooltip={p.canDelete ? undefined : "A shipped flow cannot be deleted; restore it from History instead."} />
        </>}
      </div>
    </div>
    {p.description && <p className="max-w-3xl text-sm opacity-80">{p.description}</p>}
  </header>;
}

// ── FlowSourcePane.tsx ──────────────────────────────────────────
/** The flow's text beside the daemon's verdict on it. */
export function FlowSourcePane({ name, locked, draft, strict, onStrict, canRun }: { name: string; locked?: boolean; draft: ReturnType<typeof useFlowDraft>; strict: boolean; onStrict: (v: boolean) => void; canRun: boolean }) {
  const { controlTone } = useAppSettings();
  return <div className="grid h-full min-h-0 min-w-0 gap-3 xl:grid-cols-2">
    <div className="flex min-h-0 flex-col gap-2">
      <p className="shrink-0 text-xs opacity-70">The flow's text, as it will be saved. Edits here and in the Flow tab stay in step; comments are not kept when the builder rewrites the text.</p>
      <div className="min-h-0 flex-1 [&_textarea]:h-full"><Textarea aria-label="Flow source" readOnly={locked} spellCheck={false} tone={controlTone} className="font-mono text-xs" resize="none" fullWidth value={draft.text} onChange={(e) => draft.editText(e.target.value)} /></div>
      {draft.problem && <Callout tone="error">{draft.problem}</Callout>}
    </div>
    <ScrollArea className="h-full min-h-0"><div className="space-y-4 pr-1">
      <div data-testid="strict-toggle"><Switch checked={strict} onChange={onStrict} label="Count unverifiable findings as failures" /></div>
      {draft.report ? <Diagnostics report={draft.report as never} strict={strict} /> : <EmptyState title="No verdict yet" />}
      <LintPanel name={name} content={draft.dirty ? draft.text : undefined} canRun={canRun} />
    </div></ScrollArea>
  </div>;
}

// ── ProblemList.tsx ─────────────────────────────────────────────
/** What the daemon found, one row each; a row opens the lane it is about. */
export function ProblemList({ problems, model, onFocus }: { problems: Diagnostic[]; model: FlowModel; onFocus: (lane: string | undefined) => void }) {
  if (problems.length === 0) return null;
  return <section className="max-h-40 shrink-0 space-y-1.5 overflow-y-auto" aria-label="Problems" data-testid="problem-list">
    <h3 className="text-xs font-semibold uppercase tracking-widest opacity-70">What the daemon found</h3>
    {problems.map((d, i) => {
      const laneId = Object.keys(diagnosticsByLane([d], model).byLane)[0];
      return <Panel key={i} variant="subtle" tone={d.state === "invalid" ? "rose" : "amber"} padding="xs" corner="rounded-lg" onClick={() => onFocus(laneId)}>
        <span className="block text-sm">{d.message}</span>
        <span className="block font-mono text-xs opacity-70">{d.path}{d.hint ? ` · ${d.hint}` : ""}</span>
      </Panel>;
    })}
  </section>;
}
