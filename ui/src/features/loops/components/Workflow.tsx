import type { ReactNode } from "react";
import { IconButton, Panel, Pill, Textarea, WorkflowTrackerRail, WorkflowTrackerStepDetails, WorkflowTrackerDetails,
  type WorkflowAction, type WorkflowData, type WorkflowStatus } from "../../../components/kit";
import { formatUSD, totalCost } from "../../../lib/cost";
import { formatMs } from "../../../lib/duration";
import { uncertainText } from "../../../lib/uncertain";
import { useAppSettings } from "../../../lib/appSettings";
import type { Cost } from "../../../lib/protocol";
import { blockedLine, type BlockedReason, type LadderLane, type LadderStep } from "./Ladder";

export interface WorkflowLoop {
  blueprint: string;
  slug?: string;
  goal?: string;
  root?: string;
  status: string;
  position: { lane: string; step?: string; item?: string };
  lanes: LadderLane[];
  startedAt?: string;
  updatedAt?: string;
  gateAnswers?: string[];
  gatePrompt?: string;
}

export function statusOf(status: string): WorkflowStatus {
  if (["ok", "done", "accepted", "complete"].includes(status)) return "done";
  if (["running", "active"].includes(status)) return "running";
  // Checked directly against runtime/loop.go: an unattended ask (no
  // st.Request.HasOperator) never suspends — it answers the model a tool
  // error and the step runs to a normal finish, same as any other failed
  // call (loop.go:696-709). Only the ATTENDED path suspends (ResultSuspended,
  // loop.go:674-694), and that is what daemon status "waiting"/"suspended"
  // means: a live step, parked, resumable by answering. "blocked" never
  // suspends anything — it is the loop's own terminal readout once a step
  // already gave up, same as "failed", with nothing left to wait on. Pause
  // (amber) is reserved for the first group; the second gets the alert.
  if (["waiting", "suspended", "advisory", "reject"].includes(status)) return "blocked";
  if (["blocked", "failed"].includes(status)) return "attention";
  if (["skipped", "not-needed"].includes(status)) return "skipped";
  return "not_started";
}

export function flattened(lane: LadderLane): Array<LadderStep & { label: string }> {
  return [
    ...(lane.steps ?? []).map((step) => ({ ...step, label: step.id })),
    ...(lane.items ?? []).flatMap((item) => item.steps.map((step) => ({ ...step, id: `${item.id}/${step.id}`, label: `${item.id} · ${step.id}` }))),
  ];
}

function money(costs: Array<Cost | undefined>): string {
  return uncertainText(totalCost(costs), (usd) => `$${formatUSD(usd)}`);
}

function modelSteps(lanes: LadderLane[]): LadderStep[] {
  return lanes.flatMap(flattened).filter((step) => step.kind !== "gate");
}

function measuredMs(steps: LadderStep[]): number | undefined {
  const values = steps.map((step) => step.wallMs).filter((value): value is number => typeof value === "number" && Number.isFinite(value));
  return values.length ? values.reduce((sum, value) => sum + value, 0) : undefined;
}

function timeBetween(start?: string, end?: string): number | undefined {
  if (!start || !end) return undefined;
  const startMs = Date.parse(start), endMs = Date.parse(end);
  return Number.isFinite(startMs) && Number.isFinite(endMs) ? Math.max(0, endMs - startMs) : undefined;
}

export function workflowData(loop: WorkflowLoop, reason?: string | BlockedReason, options?: {
  activeStepId?: string; details?: ReactNode; gateBusy?: boolean;
}): WorkflowData {
  const active = loop.position.lane;
  return { title: loop.blueprint, eyebrow: "Blueprint workflow", activeStepId: options?.activeStepId ?? active,
    steps: loop.lanes.map((lane) => {
      const current = lane.id === active;
      const laneStatus = current && ["blocked", "waiting", "running"].includes(loop.status) ? loop.status : lane.status;
      const steps = flattened(lane);
      const direct = lane.structure === "direct";
      const directStep = direct ? lane.steps?.[0] : undefined;
      const models = steps.filter((step) => step.kind !== "gate");
      const cost = models.length ? money(models.map((step) => step.cost)) : "No model cost";
      const duration = measuredMs(models);
      const decision = current && loop.status === "waiting";
      const gate = directStep?.kind === "gate" || steps.some((step) => step.kind === "gate");
      const actions: WorkflowAction[] | undefined = decision && gate && loop.gateAnswers?.length ?
        loop.gateAnswers.map((answer) => ({ id: answer, label: answer.charAt(0).toUpperCase() + answer.slice(1),
          tone: /^(approve|accept|yes|continue)$/i.test(answer) ? "emerald" : "rose", disabled: options?.gateBusy })) : undefined;
      const label = laneStatus === "blocked" ? "Blocked" : decision ? (loop.gateAnswers?.length ? "Approval requested" : "Answer requested") : laneStatus;
      const notes = [
        current && loop.status === "blocked" && reason ? blockedLine(reason) : undefined,
        decision ? loop.gatePrompt || "A decision is needed to continue." : undefined,
        directStep && (directStep.kind === "gate" ? "Approval gate" : directStep.kind === "model-step" ? "Model step" : "Code action"),
        directStep?.outcome && `Outcome: ${directStep.outcome}`,
        directStep?.reason,
        lane.condition, lane.iterate && `For each ${lane.iterate}`,
        lane.produces?.length && `Produces: ${lane.produces.join(", ")}`,
      ].filter(Boolean).join(" · ");
      return { id: lane.id, label: lane.id, status: statusOf(laneStatus), badge: label,
        meta: gate && models.length === 0 ? "Approval gate" : `Cost: ${cost} · Step time: ${duration === undefined ? "not measured" : formatMs(duration)}`,
        elapsed: (gate && models.length === 0) || duration === undefined ? undefined : `Step time ${formatMs(duration)}`,
        description: notes,
        actions,
        details: lane.id === (options?.activeStepId ?? active) ? options?.details : undefined,
        subSteps: direct ? undefined : steps.map((step) => {
          const activeDecision = decision && (loop.position.step === "" || !loop.position.step || loop.position.step === step.id.split("/").at(-1)) &&
            (!loop.position.item || step.id.startsWith(`${loop.position.item}/`));
          const stepStatus = activeDecision && step.kind === "gate" ? "waiting" : step.status;
          const outcomeProblem = ["reject", "blocked", "failed"].includes(step.outcome || "");
          // `stepStatus` already reads "waiting" for an active-decision gate
          // (above), so `statusOf` alone — paused unless the outcome was a
          // genuine failure — is both branches, not a separate hardcoded one.
          const visualStatus = outcomeProblem ? statusOf(step.outcome!) : statusOf(stepStatus);
          const stepNote = step.kind === "gate" ? "Approval gate" :
            [step.kind, step.cost ? money([step.cost]) : "Cost not reported"].join(" · ");
          return { id: step.id, label: step.label, status: visualStatus,
            badge: activeDecision && step.kind === "gate" ? "Decision needed" : step.outcome || stepStatus,
            note: [stepNote, step.reason].filter(Boolean).join(" · "),
            duration: step.kind !== "gate" && typeof step.wallMs === "number" ? formatMs(step.wallMs) : undefined };
        }),
      };
    }),
  };
}

export function Workflow({ loop, reason, now, current, onBack, selectedLane, onLaneSelect, onStepSelect,
  details, onGateAnswer, gateBusy }: {
  loop: WorkflowLoop; reason?: string | BlockedReason; now: number; current: boolean; onBack?: () => void;
  selectedLane?: string; onLaneSelect?: (id: string) => void; onStepSelect?: (laneId: string, stepId: string) => void;
  details?: ReactNode; onGateAnswer?: (laneId: string, answer: string) => void; gateBusy?: boolean;
}) {
  const { controlTone, surfaceTone } = useAppSettings();
  const models = modelSteps(loop.lanes);
  const cost = models.length ? money(models.map((step) => step.cost)) : "No model cost";
  const terminal = ["done", "blocked", "failed", "aborted"].includes(loop.status);
  const elapsed = timeBetween(loop.startedAt, terminal ? loop.updatedAt : new Date(now).toISOString());
  const status = loop.status === "waiting" ? (loop.gateAnswers?.length ? "Approval requested" : "Answer requested") : loop.status;
  const data = { ...workflowData(loop, reason, { activeStepId: selectedLane, details, gateBusy }), live: current };
  const action = onGateAnswer && loop.status === "waiting" && current ? (laneId: string, answer: string) => {
    if (laneId === loop.position.lane && loop.gateAnswers?.includes(answer)) onGateAnswer(laneId, answer);
  } : undefined;
  const styling = { variant: "liquid-glass" as const, cardTone: surfaceTone, accentColor: controlTone };
  return <section className="space-y-3" data-testid="workflow-view">
    <Panel variant="liquid-glass" tone={surfaceTone} data-testid="workflow-summary">
      <div className="flex min-w-0 flex-col gap-3 sm:flex-row sm:items-start sm:justify-between" data-testid="workflow-header-row">
        <div className="min-w-0 flex-1" data-testid="workflow-header-identity">
          <div className="flex min-w-0 items-center gap-2">
            {onBack && <IconButton icon="ChevronLeft" srLabel="Back to component loops"
              tooltip="Project loops" variant="glass" color={controlTone} size="sm" onClick={onBack} />}
            <p className="aeon-accent-text min-w-0 text-sm font-semibold uppercase tracking-[.18em]">Blueprint workflow{loop.slug ? ` · ${loop.slug}` : ""}</p>
          </div>
          <h1 className="mt-2 min-w-0 text-xl font-semibold">{loop.blueprint}</h1>
        </div>
        <div className="ml-auto flex min-w-0 flex-col items-end gap-2 text-right sm:max-w-[50%]" data-testid="workflow-header-right">
          <Pill tone={["blocked", "failed", "aborted"].includes(loop.status) ? "red" :
            loop.status === "waiting" ? "amber" : terminal ? "green" : "blue"}>{status}</Pill>
          {loop.root !== undefined && <p className="min-w-0 break-all text-sm text-neutral-600 dark:text-neutral-300">{loop.root}</p>}
        </div>
      </div>
      {loop.goal !== undefined && <div className="mt-4 min-w-0">
        <Textarea label="Goal" aria-label="Loop goal" value={loop.goal} readOnly rows={3}
          size="sm" variant="glass" tone={controlTone} resize="none" className="overflow-y-auto"
          style={{ height: "5.5rem", minHeight: "5.5rem", maxHeight: "5.5rem", lineHeight: "1.5rem" }} />
      </div>}
      <dl className="mt-4 grid gap-3 text-sm sm:grid-cols-3">
        <div><dt className="text-slate-500 dark:text-slate-400">Total model cost</dt><dd className="font-medium">{cost}</dd></div>
        <div><dt className="text-slate-500 dark:text-slate-400">Wall elapsed{!terminal && !current ? " · last confirmed" : ""}</dt>
          <dd className="font-medium">{elapsed === undefined ? "Not available" : formatMs(elapsed)}</dd></div>
        <div><dt className="text-slate-500 dark:text-slate-400">Recorded step time</dt>
          <dd className="font-medium">{measuredMs(models) === undefined ? "Not measured" : formatMs(measuredMs(models)!)}</dd></div>
      </dl>
      <p className="mt-3 text-xs text-slate-500 dark:text-slate-400">Recorded step time sums saved model spans and can include time waiting for a step answer.</p>
    </Panel>
    {loop.status === "blocked" && <div role="alert" className="rounded-xl border border-red-500/40 bg-red-100/75 p-4 text-red-900 dark:bg-red-950/40 dark:text-red-100">
      <strong>Blocked at {loop.position.lane}</strong><p className="mt-1 text-sm">{reason ? blockedLine(reason) : "The loop stopped here. Open the thread and step details for the cause."}</p>
    </div>}
    {loop.status === "waiting" && <div role="status" className="rounded-xl border border-amber-500/40 bg-amber-100/75 p-4 text-amber-900 dark:bg-amber-950/30 dark:text-amber-100">
      <strong>{status} at {loop.position.lane}</strong><p className="mt-1 text-sm">{loop.gatePrompt || "Open the decision card below to continue."}</p>
      <a href={action ? "#workflow-parts" : "#loop-decision"} className="mt-2 inline-block text-sm underline">
        {action ? "Answer in workflow ↓" : "Go to decision ↓"}</a>
    </div>}
    <div id="workflow-parts" className="grid min-w-0 gap-4 lg:grid-cols-[20rem_minmax(0,1fr)] lg:items-start" data-testid="workflow-parts">
      <div className="min-w-0"><WorkflowTrackerRail data={data} {...styling}
        onStepSelect={onLaneSelect} onSubStepSelect={onStepSelect} onStepAction={action} /></div>
      <div className="flex min-w-0 flex-1 flex-col gap-4">
        <WorkflowTrackerStepDetails data={data} {...styling} onSubStepSelect={onStepSelect} onStepAction={action} />
        <WorkflowTrackerDetails data={data} {...styling} />
      </div>
    </div>
  </section>;
}
