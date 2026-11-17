import { useMemo } from "react";
import { ConnectionFlow, type ConnectionFlowConnectRequest, type ConnectionFlowNode, type ConnectionFlowNodeItem, type ConnectionFlowPlacement } from "../../../components/kit";
import { useAppSettings } from "../../../lib/appSettings";
import { describeCondition, laneIds, stepHeadline } from "./flowModel";
import type { Diagnostic, FlowModel, OnReject, Selection, Step, StepKind } from "./flowModel";

// The flow drawn as the kit's ConnectionFlow: lanes left to right in the order they
// run, a ladder's rungs as a chain inside its card, a reject target as a branch
// edge. On its own it is a picture; the editor (FlowGraphEditor) puts the inspector
// beside it so a click on a lane or step edits it in place. Zoom stops at 100%.

const KIND_LABEL: Record<StepKind, string> = { "model-step": "Agent step", "code-action": "Code action", gate: "Approval" };
const KIND_TONE: Record<StepKind, "blue" | "violet" | "amber"> = { "model-step": "blue", "code-action": "violet", gate: "amber" };

function rejectText(r: OnReject | undefined): string {
  if (!r || (!r.goto && !r.then)) return "";
  return [r.goto ? `to ${r.goto}` : "", r.max ? `up to ${r.max} times` : "", r.then ? `then ${r.then}` : ""].filter(Boolean).join(" · ");
}

function stepItems(step: Step | undefined, model: FlowModel): ConnectionFlowNodeItem[] {
  if (!step) return [];
  const items: ConnectionFlowNodeItem[] = [];
  const headline = stepHeadline(step, model.questions);
  if (headline) items.push({ id: "does", title: KIND_LABEL[step.kind], subtitle: headline });
  if (step.produces?.length) items.push({ id: "writes", title: "Writes", subtitle: step.produces.join(", ") });
  if (step.emits?.length) items.push({ id: "files", title: "Files", subtitle: step.emits.map((e) => e.type).join(", ") });
  if (step.decides) items.push({ id: "decides", title: "Decides", subtitle: step.decides });
  return items;
}

function branch(r: OnReject | undefined, known: (id: string) => string | undefined): ConnectionFlowNode["failure"] {
  const to = r?.goto ? known(r.goto) : undefined;
  return to ? { to, label: rejectText(r) } : undefined;
}

export function flowGraphNodes(model: FlowModel, selection: Selection, issues: Record<string, Diagnostic[]>, routeChips: Record<string, string> = {}): ConnectionFlowNode[] {
  const ids = new Set(laneIds(model));
  return model.lanes.map((lane): ConnectionFlowNode => {
    const picked = selection.type !== "settings" && selection.laneId === lane.id;
    const kind = lane.step?.kind;
    const when = describeCondition(lane.when ?? lane.step?.when);
    const items = stepItems(lane.step, model);
    if (when) items.push({ id: "when", title: "Only when", subtitle: when });
    if (routeChips[lane.id]) items.push({ id: "route", title: "Model", subtitle: routeChips[lane.id]! });
    const reject = lane.onReject ?? lane.step?.onReject;
    const text = rejectText(reject);
    if (text) items.push({ id: "reject", title: "On rejection", subtitle: text });
    const bad = (issues[lane.id] ?? []).some((p) => p.state === "invalid");
    const node: ConnectionFlowNode = {
      id: lane.id, title: lane.id, data: { laneId: lane.id }, active: picked,
      subtitle: lane.ladder ? `Repeat for each ${lane.iterate || "item"}` : kind ? KIND_LABEL[kind] : "No step",
      tone: bad ? "rose" : lane.ladder ? "emerald" : kind ? KIND_TONE[kind] : "slate",
      items, failure: branch(reject, (id) => (ids.has(id) ? id : undefined)),
    };
    if (lane.ladder) {
      const rungs = new Set(lane.ladder.map((s) => s.id));
      node.childLayout = "chain";
      node.rows = lane.ladder.map((s, i) => [{
        id: `${lane.id}/${s.id ?? i}`, title: s.id || `Step ${i + 1}`, subtitle: KIND_LABEL[s.kind], tone: KIND_TONE[s.kind],
        active: selection.type === "step" && selection.laneId === lane.id && selection.index === i,
        data: { laneId: lane.id, index: i }, items: stepItems(s, model),
        failure: branch(s.onReject, (id) => (rungs.has(id) ? `${lane.id}/${id}` : undefined)),
      }]);
    }
    return node;
  });
}

export function FlowGraph({ model, selection, select, issues, loading, routeChips, onCreate, onConnect }: {
  model: FlowModel; selection: Selection; select: (s: Selection) => void; issues: Record<string, Diagnostic[]>; loading: boolean;
  /** The model route chosen on a lane (a shipped flow's customisation), shown on its card. */
  routeChips?: Record<string, string>;
  /** Giving either turns the graph's own edit mode on: connectors appear on the nodes, a new lane can be created from one, and a link can be drawn between two. */
  onCreate?: (placement: ConnectionFlowPlacement) => void;
  onConnect?: (request: ConnectionFlowConnectRequest) => void;
}) {
  const { surfaceTone } = useAppSettings();
  const nodes = useMemo(() => flowGraphNodes(model, selection, issues, routeChips), [model, selection, issues, routeChips]);
  return <div className="h-full min-h-72 w-full min-w-0 max-w-full overflow-hidden" data-testid="flow-graph">
    <ConnectionFlow nodes={nodes}
      onNodeClick={(node) => {
        const d = node.data as { laneId?: string; index?: number } | undefined;
        if (!d?.laneId) return;
        select(d.index === undefined ? { type: "lane", laneId: d.laneId } : { type: "step", laneId: d.laneId, index: d.index });
      }}
      variant="simple" tone={surfaceTone} edgeStyle="orthogonal" corner="rounded-md" ringSize="sm" flowState="stopped" progressType="none"
      editMode={onCreate !== undefined || onConnect !== undefined}
      onNodeCreate={onCreate ? (_ghostId, placement) => onCreate(placement) : undefined}
      onConnect={onConnect}
      highlightPath={ false} interactive showControls loading={loading} minZoom={0.3} maxZoom={1} height="100%" maxVisibleItems={3} className="h-full w-full min-w-0 max-w-full" />
  </div>;
}
