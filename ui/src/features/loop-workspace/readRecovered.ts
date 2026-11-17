import { rpc } from "../../lib/bridge";
import type { LoopDetail } from "../loops/pages/LoopScreen";
import { stepIdsOf } from "./workspaceModel";
import { NOTHING_RECOVERED, type BlockQuestion, type PendingSteer, type Recovered } from "./pendingQuestion";

// The reads behind a reopened chat (S76). Each one is best-effort and
// independent: a daemon without a method, or a read that fails, recovers
// nothing of that kind and says nothing invented.

interface QuestionBlockResult {
  questions?: { id: number; stepId?: number; kind: string; prompt: string; answers?: string[] | null; answer?: string; toolUseId?: string }[];
}
interface PendingResult {
  pending: boolean;
  question?: { id: number; kind: string; prompt: string; answers: string[] | null; stepId: number };
}
type SteerRows = { steerId: number; state: string; text: string; createdAt: string }[];

async function readBlock(detail: LoopDetail): Promise<BlockQuestion[]> {
  try {
    const block = await rpc<QuestionBlockResult>("loop.questions", { id: detail.id });
    if (Array.isArray(block?.questions)) {
      return block.questions.map((q) => ({ id: q.id, stepId: q.stepId, kind: q.kind, prompt: q.prompt, answers: q.answers ?? [], answer: q.answer }));
    }
  } catch { /* an older daemon: fall back to the one pending question */ }
  try {
    const q = await rpc<PendingResult>("question.pending", { componentRoot: detail.root, slug: detail.slug });
    if (q?.pending && q.question) {
      return [{ id: q.question.id, stepId: q.question.stepId, kind: q.question.kind, prompt: q.question.prompt, answers: q.question.answers ?? [] }];
    }
  } catch { /* none recoverable */ }
  return [];
}

/** Every question the loop asked, answered or not, so a finished chat replays with them. */
async function readPast(detail: LoopDetail): Promise<BlockQuestion[]> {
  try {
    const all = await rpc<QuestionBlockResult>("loop.questions", { id: detail.id, all: true });
    if (!Array.isArray(all?.questions)) return [];
    return all.questions.map((q) => ({ id: q.id, stepId: q.stepId, kind: q.kind, prompt: q.prompt, answers: q.answers ?? [], answer: q.answer, toolUseId: q.toolUseId }));
  } catch { return []; /* an older daemon: the transcript rows stay as they were */ }
}

async function readSteers(detail: LoopDetail): Promise<PendingSteer[]> {
  // The newest steps are where an undelivered instruction can still be: an
  // older step's queue was drained or cancelled when it ended.
  const steps = stepIdsOf(detail).sort((a, b) => b - a).slice(0, 2);
  const out: PendingSteer[] = [];
  for (const stepId of steps) {
    try {
      const rows = await rpc<SteerRows>("step.steers", { stepId });
      if (!Array.isArray(rows)) continue;
      for (const r of rows) if (r.state === "pending" || r.state === "queued") out.push({ steerId: r.steerId, stepId, text: r.text, createdAt: r.createdAt });
    } catch { /* this step's queue is not readable */ }
  }
  return out;
}

export async function readRecovered(detail: LoopDetail): Promise<Recovered> {
  const waiting = detail.status === "waiting";
  const gate = waiting && detail.questionId !== undefined && detail.gateAnswers?.length
    ? { id: detail.questionId, prompt: detail.gatePrompt ?? "", answers: detail.gateAnswers, lane: detail.position.lane } : null;
  const [questions, steers, past] = await Promise.all([
    waiting && !gate ? readBlock(detail) : Promise.resolve([] as BlockQuestion[]),
    detail.status === "running" || waiting ? readSteers(detail) : Promise.resolve([] as PendingSteer[]),
    readPast(detail),
  ]);
  if (questions.length === 0 && !gate && steers.length === 0 && past.length === 0) return NOTHING_RECOVERED;
  return { questions, gate, steers, past };
}
