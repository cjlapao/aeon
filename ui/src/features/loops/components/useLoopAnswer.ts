import { useCallback, useEffect, useRef, useState } from "react";
import { rpc, toRpcError } from "../../../lib/bridge";
import type { LoopDetail } from "../pages/LoopScreen";

export interface LoopAnswerState {
  busy: boolean;
  error: string;
  accepted: { questionId?: number; value: string } | null;
  answer: (value: string) => Promise<void>;
}

/** One answer path for the tracker quick actions and the thread question. */
export function useLoopAnswer(loop: LoopDetail | null, stepQuestion: { id: number; stepId: number } | null | undefined,
  canWrite: boolean, stale: boolean, onConfirmed: () => Promise<void>): LoopAnswerState {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [accepted, setAccepted] = useState<{ questionId?: number; value: string } | null>(null);
  const pending = useRef(false);
  useEffect(() => { setError(""); setAccepted(null); pending.current = false; setBusy(false); }, [loop?.id]);

  const answer = useCallback(async (value: string) => {
    if (!loop || pending.current || !canWrite || stale || loop.status !== "waiting") return;
    pending.current = true;
    setBusy(true); setError("");
    try {
      if (stepQuestion) await rpc("answer", { stepId: stepQuestion.stepId, questionId: stepQuestion.id, answer: value });
      else await rpc("loop.answer", { id: loop.id, answer: value });
      setAccepted({ questionId: loop.questionId, value });
      await onConfirmed();
    } catch (cause) { setError(toRpcError(cause).message); }
    finally { pending.current = false; setBusy(false); }
  }, [loop, stepQuestion, canWrite, stale, onConfirmed]);

  return { busy, error, accepted, answer };
}
