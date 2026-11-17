import { useMemo } from "react";
import { Button, Panel, Pill } from "../../../components/kit";
import { useAppSettings } from "../../../lib/appSettings";
import { LiveController, LiveState } from "../../../lib/live";
import { useRpcQuery } from "../../../lib/useRpcQuery";
import { Callout, Field, SelectField } from "./fields";
import { FlowGraph } from "./FlowGraph";
import { ProblemList } from "./FlowWorkspace";
import { stepHeadline, Diagnostic, FlowModel, Selection } from "./flowModel";

// ── LockedFlow.tsx ──────────────────────────────────────────────
// A shipped flow, open to be customised. Its shape is the daemon's and never changes, so
// there is nothing to drag, add or remove: the lanes are listed as they run, and the only
// thing the inspector offers is what the daemon says can be chosen on the lane picked —
// today the route (provider and model) its agent steps run on. A choice is saved the moment
// it is made, and a loop already running keeps the route it started with.

/** What the daemon allows on each lane, and what has been chosen, as `blueprint.show` answers. */
export interface LockedInfo {
  overridable: Record<string, string[]>;
  overrides: Record<string, Record<string, string>>;
}

export interface LockedFlowProps {
  model: FlowModel;
  info: LockedInfo;
  selection: Selection;
  onSelect: (s: Selection) => void;
  issues: Record<string, Diagnostic[]>;
  problems: Diagnostic[];
  /** Sets (or, with "", clears) one field on one lane. */
  onOverride: (lane: string, field: string, value: string) => void;
  busy: boolean;
  /** Opens the copy form: a shipped flow's steps change only in a copy of it. */
  onDuplicate?: () => void;
  controller: LiveController;
  live: LiveState;
}

export function LockedFlow({ model, info, selection, onSelect, issues, problems, onOverride, busy, onDuplicate, controller, live }: LockedFlowProps) {
  const { surfaceTone } = useAppSettings();
  const routeList = useRpcQuery("routes.list", {}, controller, live);
  const routes = useMemo(() => (routeList.data?.routes ?? []).map((r) => ({ value: r.id, label: `${r.displayName || r.id} · ${r.provider}/${r.model}` })), [routeList.data]);
  const chips = useMemo(() => Object.fromEntries(Object.entries(info.overrides).flatMap(([laneId, f]) => (f.route ? [[laneId, f.route]] : []))), [info.overrides]);
  const lane = selection.type === "settings" ? undefined : model.lanes.find((l) => l.id === selection.laneId);
  const fields = lane ? info.overridable[lane.id] ?? [] : [];
  const chosen = lane ? info.overrides[lane.id]?.route ?? "" : "";

  return <div className="grid h-full min-h-0 gap-3 lg:grid-cols-[minmax(0,1fr)_minmax(340px,440px)]" data-testid="locked-flow">
    <div className="flex min-h-0 min-w-0 flex-col gap-3">
      {/* The same connection graph the flow is read on: click a lane (or a step in a ladder) to choose what can change on it. */}
      <div className="min-h-0 flex-1" data-testid="lane-canvas"><FlowGraph loading={false} model={model} selection={selection} select={onSelect} issues={issues} routeChips={chips} /></div>
      <ProblemList problems={problems} model={model} onFocus={(l) => l && onSelect({ type: "lane", laneId: l })} />
    </div>
    <Panel variant="simple" transparency="none" tone={surfaceTone} padding="sm" clickable={false} aria-label="Inspector" className="flex h-full min-h-0 flex-col" bodyClassName="min-h-0 flex-1 overflow-y-auto pr-1">
      <h2 className="mb-3 text-xs font-semibold uppercase tracking-widest opacity-70">{lane ? "Lane" : "Customise"}</h2>
      {!lane && <p className="text-sm opacity-80">This is a shipped flow, so its steps stay as they are. Pick a lane to choose what can change on it — the model its agent steps use, for one.</p>}
      {onDuplicate && <div className="mb-3 space-y-1" data-testid="locked-duplicate">
        <Button size="sm" variant="soft" onClick={onDuplicate} data-testid="locked-duplicate-button">Duplicate to edit the steps</Button>
        <p className="text-xs opacity-70">A copy is yours: its lanes can be added, moved, linked and changed on the graph.</p>
      </div>}
      {lane && <div className="space-y-3" data-testid="locked-inspector">
        <div><span className="block font-mono text-sm font-semibold">{lane.id}</span>
          {lane.step && <span className="block text-sm opacity-80">{stepHeadline(lane.step, model.questions)}</span>}</div>
        {fields.length === 0 && <Callout tone="info">Nothing on this lane can be changed. Duplicate the flow to change its steps.</Callout>}
        {fields.includes("route") && <Field label="Model route" hint="The provider and model this lane's agent steps use. Left on the loop's route, it follows whatever the loop was started with.">
          <SelectField label="Model route" value={chosen} placeholder="The loop's own route" disabled={busy} options={routes} onChange={(v) => onOverride(lane.id, "route", v)} />
        </Field>}
        {chosen !== "" && <Pill size="sm" tone="indigo">Overridden</Pill>}
      </div>}
    </Panel>
  </div>;
}
