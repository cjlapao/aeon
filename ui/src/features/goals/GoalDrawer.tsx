import { DetachedNote } from "../../components/DetachedNote";
import { useState } from "react";
import { Alert, Button, FormField, Input, Pill } from "../../components/kit";
import { OverlaySidePanel } from "../../components/OverlaySidePanel";
import { toRpcError } from "../../lib/bridge";
import type { LiveController, LiveState } from "../../lib/live";
import { decideGoal, readGoalDocument, useGoalPicture, useTasks, type GoalDocument } from "./goalsRpc";
import { TaskList } from "./TasksPanel";

/**
 * GoalDrawer — one goal opened: what it is, the documents it owns, the loops
 * working it, its tasks, and the decision that is a person's alone.
 *
 * Nothing here is derived by the page: the execution state and the cost floor
 * are the daemon's (composed from rows, never stored), the documents are the
 * heads the goal owns across every loop that wrote one, and a decision goes
 * out as `goal.decide` with the goal's own version — a stale version is the
 * daemon's refusal to show, not ours to swallow.
 */
export interface GoalDrawerProps {
  goalId: string;
  controller: LiveController;
  live: LiveState;
  canWrite: boolean;
  onClose: () => void;
  onChanged: () => void;
}

export function GoalDrawer({ goalId, controller, live, canWrite, onClose, onChanged }: GoalDrawerProps) {
  const picture = useGoalPicture(goalId, controller, live);
  const tasks = useTasks(goalId, "", controller, live);
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [refusal, setRefusal] = useState<string | undefined>(undefined);
  const [open, setOpen] = useState<{ doc: GoalDocument; text: string } | { doc: GoalDocument; error: string } | null>(null);

  const g = picture.data?.goal;

  async function decide(decision: "approved" | "rejected" | "parked") {
    if (!g) return;
    setBusy(true);
    setRefusal(undefined);
    try {
      await decideGoal(g.id, g.version, decision, reason.trim());
      picture.refetch();
      onChanged();
    } catch (cause) {
      setRefusal(toRpcError(cause).message);
    } finally {
      setBusy(false);
    }
  }

  async function show(doc: GoalDocument) {
    try {
      const read = await readGoalDocument(doc);
      setOpen({ doc, text: read.content });
    } catch (cause) {
      setOpen({ doc, error: toRpcError(cause).message });
    }
  }

  const badges = g && (
    <div className="flex flex-wrap gap-1.5">
      <Pill tone="slate" data-testid="goal-review-state">{g.reviewState}</Pill>
      <Pill tone="slate">{g.type}</Pill>
      <Pill tone="slate">v{g.version}</Pill>
      {picture.data && <Pill tone="purple" data-testid="goal-execution">{picture.data.executionState}</Pill>}
      {picture.data && picture.data.cost.usd > 0 && (
        <Pill tone="slate">${picture.data.cost.usd.toFixed(2)}{picture.data.cost.isFloor ? " (floor)" : ""}</Pill>
      )}
    </div>
  );

  return (
    <OverlaySidePanel title={<span data-testid="goal-title">{g ? g.title : goalId}</span>} titleClassName="text-xl font-semibold" variant="simple" width={640} onClose={onClose} testId="goal-drawer">
      <div className="space-y-3">
        {badges}
        {picture.error !== undefined && <Alert intent="danger" title={picture.error} data-testid="goal-error" />}
        {g && g.bodyNote && <p className="whitespace-pre-wrap text-sm">{g.bodyNote}</p>}
        {g && (
          <p className="text-xs opacity-70">
            filed by {g.createdByKind} {g.createdById} · from {g.sourceKind}{g.sourceRef ? ` ${g.sourceRef}` : ""}
          </p>
        )}
        {g && <DetachedNote kind="goal" id={g.id} />}
        {g && (g as { sourceLoop?: string }).sourceLoop && <DetachedNote kind="loop" slug={(g as { sourceLoop?: string }).sourceLoop} />}

        {picture.data && (
          <section className="space-y-1" data-testid="goal-documents">
            <h4 className="text-xs font-semibold uppercase opacity-70">Documents the goal owns</h4>
            {picture.data.documents.length === 0 && <p className="text-xs opacity-70">None yet.</p>}
            {picture.data.documents.map((doc) => (
              <button
                key={`${doc.loopSlug}/${doc.name}`}
                type="button"
                className="block w-full rounded-lg border border-white/10 p-2 text-left text-xs hover:bg-white/5"
                data-testid={`goal-doc-${doc.name}`}
                onClick={() => void show(doc)}
              >
                <strong>{doc.name}</strong> · rev {doc.rev} · {doc.bytes} bytes · written by {doc.writtenBy || "unknown"} in loop {doc.loopSlug}
              </button>
            ))}
            {open && "error" in open && <Alert intent="danger" title={open.error} data-testid="goal-doc-error" />}
            {open && "text" in open && (
              <pre className="max-h-96 overflow-auto whitespace-pre-wrap rounded-lg bg-white/5 p-3 text-xs" data-testid="goal-doc-text">
                {open.text}
              </pre>
            )}
          </section>
        )}

        {picture.data && picture.data.loops.length > 0 && (
          <section className="space-y-1" data-testid="goal-loops">
            <h4 className="text-xs font-semibold uppercase opacity-70">Loops working it</h4>
            {picture.data.loops.map((loop) => (
              <p key={loop.id} className="text-xs"><strong>{loop.slug}</strong> · {loop.status}{loop.blueprint ? ` · ${loop.blueprint}` : ""}</p>
            ))}
          </section>
        )}

        <section className="space-y-1" data-testid="goal-tasks">
          <h4 className="text-xs font-semibold uppercase opacity-70">Tasks</h4>
          {tasks.error !== undefined && <Alert intent="danger" title={tasks.error} />}
          <TaskList tasks={tasks.data} loading={tasks.loading} emptyText="No tasks filed for this goal yet." />
        </section>

        {picture.data && picture.data.reviews.length > 0 && (
          <section className="space-y-1" data-testid="goal-reviews">
            <h4 className="text-xs font-semibold uppercase opacity-70">Decisions</h4>
            {picture.data.reviews.map((r) => (
              <p key={r.id} className="text-xs">v{r.subjectVersion} {r.decision} by {r.byId} — {r.reason || "no reason given"}</p>
            ))}
          </section>
        )}

        {g && g.reviewState === "in_review" && (
          <section className="space-y-2 border-t border-white/10 pt-3" data-testid="goal-decision">
            <FormField label="Reason (shown in the decision trail)">
              <Input value={reason} aria-label="decision reason" data-testid="goal-reason" onChange={(e) => setReason(e.target.value)} />
            </FormField>
            <div className="flex flex-wrap gap-2">
              <Button size="sm" disabled={!canWrite || busy} data-testid="goal-approve" onClick={() => void decide("approved")}>Approve</Button>
              <Button size="sm" variant="soft" disabled={!canWrite || busy} data-testid="goal-park" onClick={() => void decide("parked")}>Park</Button>
              <Button size="sm" variant="soft" disabled={!canWrite || busy} data-testid="goal-reject" onClick={() => void decide("rejected")}>Reject</Button>
            </div>
            {refusal !== undefined && <Alert intent="danger" title={refusal} data-testid="goal-refusal" />}
          </section>
        )}
      </div>
    </OverlaySidePanel>
  );
}
