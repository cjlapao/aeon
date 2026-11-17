import { useMemo } from "react";
import type { ChatActions, ChatAttachment, ChatSession, SteerRequest } from "../../../components/agent-chat";
import { rpc, toRpcError } from "../../../lib/bridge";
import { cachedAttachmentData, putAllAttachments, type AttachmentScope } from "../../../lib/attachments";

/**
 * What the chat's buttons need from whoever owns the loop: which loop, who is
 * running which step, and a place to remember steer text. The loop page and
 * the bench's live mode both satisfy it, so there is one copy of the RPC
 * wiring. (Steer, stop and answer are keyed by step id — never by agent — so
 * they hold when several agents run at once, P33.)
 */
export interface ChatActionsHost {
  loopId: string;
  session: ChatSession;
  rememberSteer(steerId: number, text: string): void;
  /** Re-read the loop after something that moves it (a gate answer). */
  refresh?(): Promise<void>;
}

/** One entry of the *Run blueprint…* dialogue — the daemon's verdict, before any start. */
export interface RunOption {
  blueprint: string;
  version?: number;
  runnable: boolean;
  /** The daemon's own words for why not; shown verbatim, never rewritten. */
  reason?: string;
  candidates?: { kind: "goal" | "task"; id: string; title: string; state: string }[];
}

export interface ChatWiring {
  actions: ChatActions;
  onSteer(request: SteerRequest): Promise<void>;
  onStop(agent: string): Promise<void>;
  /** A resting loop's thread (S39 CR3): the person's words, the daemon decides.
   * `skills` (S68 B6) are the chosen NAMES — sent beside the words, never
   * woven into them; the daemon's door validates them or refuses the send
   * naming every offender. */
  onMessage(text: string, route?: string, skills?: readonly string[], files?: readonly ChatAttachment[]): Promise<void>;
  /** What could be run here, with the requires verdict per blueprint (CR5). */
  loadRunOptions(): Promise<RunOption[]>;
  /** Set the loop's thinking switch and/or effort for its later model steps (`loop.options`). */
  onLoopOptions(options: { thinking?: string; reasoningEffort?: string }): Promise<void>;
  onRunBlueprint(req: { blueprint: string; goalId?: string; taskId?: string; message?: string }): Promise<void>;
}

export function useChatActions(host: ChatActionsHost, componentRoot: string, onDiff: (title: string, diff: string) => void,
  trace?: (line: string) => void): ChatWiring {
  const { loopId, session, rememberSteer, refresh } = host;
  return useMemo(() => {
    const call = async <T,>(label: string, method: string, params: unknown): Promise<T> => {
      trace?.(`${method} ${JSON.stringify(params)}`);
      try { const result = await rpc<T>(method, params); trace?.(`  ✓ ${label}`); return result; }
      catch (cause) { const message = toRpcError(cause).message; trace?.(`  ✗ ${message}`); throw new Error(message); }
    };
    // S69 AT2: the scope is the LOOP ROW's own — read once, lazily, the
    // first time a file actually rides (a chat that never attaches pays
    // nothing). A slug typed into a flag would be a claim; this is a fact.
    let scope: AttachmentScope | null = null;
    const scopeOf = async (): Promise<AttachmentScope> => {
      if (!scope) {
        const v = await call<{ slug?: string; componentRoot?: string }>("loop scope", "loop.show", { id: loopId });
        scope = { slug: v.slug ?? "", componentRoot: v.componentRoot || componentRoot };
      }
      return scope;
    };
    /** The agent's step that can still be steered: a running one, else its latest. */
    const runOf = (agent: string) => {
      const run = session.runs.find((r) => r.agent === agent && r.state !== "done") ?? session.runs.find((r) => r.agent === agent);
      if (!run) throw new Error(`${agent} has no running step`);
      return run;
    };
    const actions: ChatActions = {
      onAnswer: async (question, answer) => {
        const questionId = Number(question.id);
        await call("answered", "answer", { stepId: session.questionStep.get(questionId), questionId, answer });
        // The store, not the card, says what is still owed: re-read so an
        // answered question rests answered and the next one of the block shows.
        await refresh?.();
      },
      onGate: async (_gateId, answer, feedback) => {
        await call("gate answered", "loop.answer", { id: loopId, answer, ...(feedback ? { feedback } : {}) });
        await refresh?.();
      },
      onViewDiff: async (file) => {
        const result = await call<{ diff?: string }>("diff", "workspace.git.diff", { componentRoot, path: file.path });
        onDiff(file.path, result.diff ?? "");
      },
      onCancelSteer: async (steerId) => { await call("withdrawn", "step.steer.cancel", { steerId }); },
      attachmentPreview: async (document, rev) => cachedAttachmentData(await scopeOf(), document, rev),
    };
      /** The daemon refuses as DATA; its sentence is the answer — shown verbatim. */
      const refusedSentence = (result: unknown): string | null => {
        const r = (result && typeof result === "object" ? result : {}) as { refused?: { detail?: string; kind?: string } };
        if (!r.refused) return null;
        return r.refused.detail ?? r.refused.kind ?? "refused";
      };
    return {
      actions,
      onSteer: async (request: SteerRequest) => {
        const run = runOf(request.target);
        // S69 AT2: bytes go once, here and nowhere else; the queue carries
        // REFERENCES only, and the field is ABSENT when nothing rode —
        // never []. A refused put throws BEFORE step.steer: the composer
        // keeps its draft, and no half-attached steer is ever queued.
        const refs = request.attachments?.length
          ? await putAllAttachments(await scopeOf(), request.attachments.map((a) => a.file))
          : undefined;
        const result = await call<{ steerId: number; skipped?: string[] }>("queued", "step.steer", {
          stepId: run.stepId,
          ...(request.text ? { text: request.text } : {}),
          ...(request.route ? { route: request.route } : {}),
          ...(request.reasoningEffort ? { reasoningEffort: request.reasoningEffort } : {}),
          ...(request.skills?.length ? { skills: request.skills } : {}),
          ...(refs?.length ? { attachments: refs } : {}),
        });
        if (request.text) rememberSteer(result.steerId, request.text);
        // B6's honest skip: a chosen skill that was known but could not ride
        // is spoken, never swallowed — the same three-state the daemon keeps.
        if (result.skipped?.length) trace?.(`  (skipped: ${result.skipped.join(", ")})`);
      },
      onStop: async (agent: string) => { await call("stopped", "step.stop", { stepId: runOf(agent).stepId }); },
      onMessage: async (text, route, skills, files) => {
        // The same door discipline as a steer: put once, reference forever,
        // absent when none, nothing sent when a put is refused.
        const refs = files?.length ? await putAllAttachments(await scopeOf(), files.map((a) => a.file)) : undefined;
        const result = await call<{ refused?: { detail?: string } }>("loop.message sent", "loop.message", { id: loopId, text, ...(route ? { route } : {}), ...(skills?.length ? { skills } : {}), ...(refs?.length ? { attachments: refs } : {}) });
        const sentence = refusedSentence(result);
        if (sentence) throw new Error(sentence);
      },
      loadRunOptions: async () => {
        const result = await call<{ options?: RunOption[] }>("run options", "loop.run.options", { id: loopId });
        return result.options ?? [];
      },
      onLoopOptions: async (options) => {
        await call("options set", "loop.options", { id: loopId, ...options });
        await refresh?.();
      },
      onRunBlueprint: async (req) => {
        const result = await call<{ refused?: { detail?: string } }>("run started", "loop.run", {
          id: loopId, blueprint: req.blueprint,
          ...(req.goalId ? { goalId: req.goalId } : {}),
          ...(req.taskId ? { taskId: req.taskId } : {}),
          ...(req.message ? { message: req.message } : {}),
        });
        const sentence = refusedSentence(result);
        if (sentence) throw new Error(sentence);
      },
    };
  }, [loopId, session, rememberSteer, refresh, componentRoot, onDiff, trace]);
}
