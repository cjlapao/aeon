import { useCallback, useEffect, useMemo, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { Panel } from "../../../components/kit";
import { onEvent, rpc, rpcTyped, toRpcError } from "../../../lib/bridge";
import type { LiveController, LiveState } from "../../../lib/live";
import type { AeonEvent } from "../../../lib/protocol";
import { type BlockedReason, type LadderLane } from "../components/Ladder";
import { Thread } from "../components/Thread";
import { LoopFiles } from "../components/LoopFiles";
import { DodCard } from "../components/DodCard";
import { Workflow } from "../components/Workflow";
import { WorkflowInspector } from "../components/WorkflowInspector";
import { useLoopAnswer } from "../components/useLoopAnswer";

export interface LoopDetail {
  id: string; slug: string; title?: string; blueprint: string; goal: string; root: string; status: string;
  position: { lane: string; step?: string; item?: string }; questionId?: number;
  /** The model the loop runs on and the options in force (empty: the route's own). */
  route?: string; reasoningEffort?: string; thinking?: string;
  gateAnswers?: string[]; gatePrompt?: string; lanes: LadderLane[]; pinnedContent?: string; lastReason?: string;
  startedAt?: string; updatedAt?: string;
  // T10 — eligibility and refusal for the historical missing-handler retry.
  // Both are the DAEMON's computation (loop.show fills them); the screen
  // renders them and adds no opinion of its own about whether a retry
  // would work — only the daemon reads the live registry.
  retryEligible?: boolean; retryAgent?: string;
  retryRefusal?: { kind: string; detail: string };
  // S39 — the run ledger beside the pin (T1). `loop.show` fills them; the
  // screen renders what the daemon said and adds no opinion of its own.
  // `attended` is the CR7 declaration (true/false; absent means never
  // declared), `runSeq` the CURRENT run, `runs` the ledger in run order.
  runSeq?: number; attended?: boolean | null;
  runs?: { runSeq: number; blueprint: string; version: number; state: string; startedByKind: string; goalId?: string; goalVersion?: number; taskId?: string; startedAt: string; endedAt?: string; cost: { usdAtLeast: number; exact: boolean; partial: number; unknown: number; steps: number } }[];
}
interface StepQuestion { id: number; stepId: number; body: string; options?: string[] }

export function LoopScreen({ controller, live, canWrite }: {
  controller: LiveController; live: LiveState; canWrite: boolean;
}) {
  const { id = "" } = useParams();
  const navigate = useNavigate();
  const [detail, setDetail] = useState<LoopDetail | null>(null);
  const [events, setEvents] = useState<AeonEvent[]>([]);
  const [stepQuestion, setStepQuestion] = useState<StepQuestion | null>(null);
  const [error, setError] = useState("");
  const [now, setNow] = useState(() => Date.now());
  const [selection, setSelection] = useState<{ lane: string; step?: string } | null>(null);
  const [retryBusy, setRetryBusy] = useState(false);
  const [retryNote, setRetryNote] = useState("");
  useEffect(() => {
    if (live.state !== "current" || !detail || ["done", "blocked", "failed", "aborted"].includes(detail.status)) return;
    const timer = setInterval(() => setNow(Date.now()), 30_000);
    return () => clearInterval(timer);
  }, [live.state, detail?.status]);
  useEffect(() => { setDetail(null); setEvents([]); setStepQuestion(null); setError(""); setSelection(null); }, [id]);
  const updateQuestion = useCallback(async (result: LoopDetail) => {
    if (result.status !== "waiting" || result.gateAnswers?.length || !result.questionId) { setStepQuestion(null); return; }
    const stepId = result.lanes.flatMap((lane) => [
      ...(lane.steps ?? []), ...(lane.items ?? []).flatMap((item) => item.steps),
    ]).filter((step) => step.stepId).at(-1)?.stepId;
    if (!stepId) { setStepQuestion(null); return; }
    const saved = await rpc<{ question?: StepQuestion }>("step.show", { id: stepId });
    setStepQuestion(saved.question?.id === result.questionId ? saved.question : null);
  }, []);
  const read = useCallback(async () => {
    if (!id) return;
    try {
      const result = await rpc<LoopDetail>("loop.show", { id, includeBlueprint: true });
      setDetail(result); setError("");
      await updateQuestion(result);
    } catch (cause) { setError(toRpcError(cause).message); }
  }, [id, updateQuestion]);
  useEffect(() => controller.registerReader(read), [controller, read]);
  useEffect(() => { if (live.state === "current") void read(); }, [live.state, read]);
  useEffect(() => {
    if (live.state !== "current" || !id) return;
    let disposed = false;
    let stop: (() => void) | undefined;
    let subscriptionId = "";
    void (async () => {
      try {
        stop = await onEvent((raw) => {
          const event = raw as AeonEvent;
          setEvents((old) => old.some((item) => item.seq === event.seq) ? old :
            [...old, event].sort((a, b) => a.seq - b.seq).slice(-500));
          const payload = event.payload as Record<string, unknown> | undefined;
          if (payload?.loopId === id && event.kind.startsWith("loop.")) {
            const snapshot = payload.snapshot as LoopDetail | undefined;
            if (snapshot) {
              setDetail((previous) => ({ ...snapshot, pinnedContent: previous?.pinnedContent }));
              void updateQuestion(snapshot).catch((cause) => setError(toRpcError(cause).message));
            } else void read();
          }
        });
        if (disposed) { stop(); return; }
        const sub = await rpcTyped("subscribe", { after: 0, filter: { kinds: ["loop.*", "step.*", "question.*", "model.*"] } });
        subscriptionId = sub.subscriptionId;
        if (disposed) stop?.();
      } catch (cause) { if (!disposed) setError(toRpcError(cause).message); }
    })();
    return () => { disposed = true; stop?.(); if (subscriptionId) void rpc("unsubscribe", { subscriptionId }); };
  }, [id, live.state === "current" ? live.generation : -1, detail?.root, read, updateQuestion]);

  const loopEvents = useMemo(() => events.filter((event) => {
    const payload = event.payload as Record<string, unknown> | undefined;
    return payload?.loopId === id || (payload?.stepId && detail?.lanes.some((lane) =>
      [...(lane.steps ?? []), ...(lane.items ?? []).flatMap((item) => item.steps)]
        .some((step) => step.stepId === payload.stepId)));
  }), [events, id, detail]);
  const latestBlocked = [...loopEvents].reverse().find((event) => event.kind === "loop.blocked");
  const retryMissingHandler = useCallback(async () => {
    if (!id || retryBusy) return;
    setRetryBusy(true);
    try {
      const result = await rpc<{ retried?: boolean; refused?: { kind: string; detail: string } }>(
        "loop.retry_handler_block", { id });
      if (result.refused) setRetryNote(`${result.refused.kind}: ${result.refused.detail}`);
      else setRetryNote("");
      await read();
    } catch (cause) { setRetryNote(toRpcError(cause).message); }
    finally { setRetryBusy(false); }
  }, [id, retryBusy, read]);
  const reason = detail?.status === "blocked" ?
    ((latestBlocked?.payload as { reason?: BlockedReason } | undefined)?.reason || detail.lastReason) : undefined;
  const selectedLane = detail?.lanes.find((lane) => lane.id === selection?.lane) ??
    detail?.lanes.find((lane) => lane.id === detail.position.lane) ?? detail?.lanes[0];
  const selectedStep = selectedLane?.structure === "direct" ? selectedLane.steps?.[0]?.id :
    selectedLane?.id === selection?.lane ? selection?.step : undefined;
  const answerState = useLoopAnswer(detail, stepQuestion, canWrite, live.state !== "current", read);
  const inspector = detail && selectedLane && selectedStep ?
    <WorkflowInspector key={`${selectedLane.id}/${selectedStep}`} loop={detail} lane={selectedLane} stepId={selectedStep} live={live} /> : undefined;

  return <section className="space-y-5 pt-3" data-testid="product-loop">
    {error && <Panel tone="red"><p role="alert">{error}</p></Panel>}
    {!detail ? <Panel><p>{error ? "Loop unavailable." : "Loading loop…"}</p></Panel> : <>
      <Workflow loop={detail} reason={reason} now={now} current={live.state === "current"}
        onBack={() => navigate(`/components?root=${encodeURIComponent(detail.root)}`)}
        selectedLane={selectedLane?.id}
        onLaneSelect={(lane) => setSelection({ lane })}
        onStepSelect={(lane, step) => setSelection({ lane, step })}
        details={inspector}
        gateBusy={answerState.busy} onGateAnswer={canWrite && live.state === "current" ? (_lane, answer) => void answerState.answer(answer) : undefined} />
      {detail.status === "blocked" && (detail.retryEligible || detail.retryRefusal || retryNote) &&
        <Panel tone={detail.retryEligible ? "amber" : "red"}>
          <div className="flex flex-wrap items-center gap-3" data-testid="loop-retry">
            {detail.retryEligible && <p>Blocked on a handler that now exists (<code>{detail.retryAgent}</code>). One transition, one dispatch — the daemon decides, this button only asks.</p>}
            {detail.retryEligible && canWrite &&
              <button type="button" disabled={retryBusy} onClick={() => void retryMissingHandler()}
                className="rounded-lg border border-white/15 px-3 py-1.5 text-sm disabled:opacity-50">
                {retryBusy ? "Retrying…" : "Retry blocked step"}
              </button>}
            {!detail.retryEligible && detail.retryRefusal &&
              <p data-testid="loop-retry-refusal">{detail.retryRefusal.kind}: {detail.retryRefusal.detail}</p>}
            {retryNote && <p role="status" data-testid="loop-retry-note">{retryNote}</p>}
          </div>
        </Panel>}
      <LoopFiles key={`files:${detail.id}`} id={detail.id} slug={detail.slug} root={detail.root} live={live}
        produced={[...new Set(detail.lanes.flatMap((lane) => lane.produces || []))]} />
      {/* S57 T11: the loop's definition of done — the brief's committed
          checklist, marked by a person through the daemon's dod doors. */}
      <DodCard slug={detail.slug} controller={controller} live={live} canWrite={canWrite} />
      <details className="rounded-xl border border-white/10 p-4"><summary className="cursor-pointer">Pinned blueprint source</summary>
        <pre className="mt-3 overflow-auto whitespace-pre-wrap break-all text-xs">{detail.pinnedContent || "Pinned source unavailable"}</pre></details>
      <div id="loop-decision"><Thread key={detail.id} loop={detail} events={loopEvents} stepQuestion={stepQuestion} canWrite={canWrite} stale={live.state !== "current"} onConfirmed={read}
        answerState={answerState} gateActionsInTracker /></div>
    </>}
  </section>;
}
