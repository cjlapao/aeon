import { Link } from "react-router-dom";
import { Panel } from "../../../components/kit";
import { costLine } from "../../../lib/cost";
import type { AeonEvent } from "../../../lib/protocol";
import type { LoopDetail } from "../pages/LoopScreen";
import { QuestionCard } from "./QuestionCard";
import { useLoopAnswer, type LoopAnswerState } from "./useLoopAnswer";

export function Thread({ loop, events, stepQuestion, canWrite, stale, onConfirmed, answerState, gateActionsInTracker }: {
  loop: LoopDetail; events: AeonEvent[]; stepQuestion?: { id: number; stepId: number; body: string; options?: string[] } | null;
  canWrite: boolean; stale: boolean; onConfirmed: () => Promise<void>;
  answerState?: LoopAnswerState; gateActionsInTracker?: boolean;
}) {
  const localAnswer = useLoopAnswer(loop, stepQuestion, canWrite, stale, onConfirmed);
  const { busy, error, accepted, answer } = answerState ?? localAnswer;
  const askedIds = new Set(events.filter((event) => event.kind === "question.asked")
    .map((event) => (event.payload as Record<string, unknown> | undefined)?.id));
  const ordered = events.filter((event) => {
    if (event.kind === "loop.waiting" && askedIds.has((event.payload as Record<string, unknown> | undefined)?.questionId)) return false;
    return event.kind.startsWith("loop.") || event.kind === "question.asked" || event.kind === "question.answered";
  })
    .sort((a, b) => a.seq - b.seq);
  const currentQuestion = [...ordered].reverse().find((event) => event.kind === "question.asked" &&
    (event.payload as Record<string, unknown>)?.id === loop.questionId) ||
    [...ordered].reverse().find((event) => event.kind === "loop.waiting" &&
      (event.payload as Record<string, unknown>)?.questionId === loop.questionId);
  const payload = currentQuestion?.payload as Record<string, unknown> | undefined;
  const prompt = typeof payload?.body === "string" ? payload.body :
    typeof payload?.prompt === "string" ? payload.prompt : loop.gatePrompt || `Decision needed at ${loop.position.lane}`;
  const waiting = loop.status === "waiting" && (stepQuestion || (loop.gateAnswers?.length ?? 0) > 0);
  const card = () => gateActionsInTracker && !stepQuestion && (loop.gateAnswers?.length ?? 0) > 0 ?
    <div className="rounded-lg border border-amber-500/30 p-3 text-sm"><p>{prompt}</p>
      <a href="#workflow-parts" className="aeon-accent-text mt-2 inline-block underline">Choose an answer in the workflow ↑</a></div> :
    <QuestionCard prompt={stepQuestion?.body || prompt} options={stepQuestion?.options ?? loop.gateAnswers ?? []}
      busy={busy || !canWrite || stale} onAnswer={(value) => void answer(value)} />;
  return <Panel tone="slate" data-testid="loop-thread"><h2 className="text-xl font-semibold">Thread</h2>
    {error && <p role="alert" className="mt-3 text-red-700">{error}</p>}
    {accepted && !ordered.some((event) => (event.payload as Record<string, unknown> | undefined)?.questionId === accepted.questionId &&
      typeof (event.payload as Record<string, unknown> | undefined)?.answer === "string") &&
      (loop.status !== "waiting" || accepted.questionId === loop.questionId) &&
      <p role="status" className="mt-3 text-emerald-700">Answer accepted by the daemon: {accepted.value}</p>}
    {stale && <p className="mt-3 text-amber-800">Connection is not current. Answers are paused until the daemon reconnects.</p>}
    {ordered.length === 0 && loop.status !== "waiting" && <p className="mt-4 text-sm text-slate-600">No events have been recorded for this loop yet.</p>}
    <ol className="mt-4 space-y-3">{ordered.map((event) => {
      const data = (event.payload ?? {}) as Record<string, unknown>;
      const stepId = typeof data.stepId === "number" ? data.stepId : undefined;
      const cost = data.cost && typeof data.cost === "object" ? data.cost as Parameters<typeof costLine>[0] : undefined;
      const answered = typeof data.questionId === "number" && typeof data.answer === "string";
      const label = answered ? "You" : event.kind === "loop.started" ? "Loop started" : event.kind === "loop.advanced" ? `Completed ${String(data.lane || "step")}` :
        event.kind === "loop.waiting" || event.kind === "question.asked" ? "Decision requested" : event.kind === "loop.skipped" ? `Skipped ${String(data.lane || "lane")}` :
        event.kind === "loop.blocked" ? "Loop blocked" : event.kind === "loop.finished" ? "Loop finished" : "Answer accepted";
      return <li key={event.seq} className="rounded-lg border border-white/10 p-3 text-sm">
        <span className="mr-2 font-mono text-xs text-slate-500">#{event.seq}</span><strong>{label}</strong>
        {stepId && <Link to={`/steps/${stepId}`} className="aeon-accent-text ml-3 underline">Step {stepId}</Link>}
        {answered ? <span className="ml-3">{data.answer as string}</span> : typeof data.outcome === "string" && <span className="ml-3">{data.outcome}</span>}
        {cost && <span className="ml-3">{costLine(cost).text}</span>}
        {!waiting && (typeof data.body === "string" || typeof data.prompt === "string") &&
          <p className="mt-2">{String(data.body || data.prompt)}</p>}
        {waiting && currentQuestion?.seq === event.seq && <div className="mt-3">{card()}</div>}
      </li>;
    })}</ol>
    {waiting && !currentQuestion && <div className="mt-4">{card()}</div>}
  </Panel>;
}
