import { NEGATIVE_ANSWER, type ChatItem, type ChatQuestion, type QuestionKind, type ToolStatus, type TurnCost } from "./types";
import type { AttachmentReceipt } from "../../lib/attachments";

/**
 * Live daemon events → chat items. Pure: same events in, same items out, which
 * is what lets the bench replay a script through it and the tests pin it.
 *
 * The input is the protocol's event envelope, typed loosely on purpose: this
 * file must keep working when a newer daemon adds fields (the protocol's T3
 * rule) and must ignore kinds it does not know rather than throw.
 */
/** Receipts ride the wire beside the words (S69 AT8). Validate every
 * field or draw nothing from the row: the mapper's job is to never
 * render a half-believed size, type or revision. */
function receiptList(raw: unknown): readonly AttachmentReceipt[] | undefined {
  if (!Array.isArray(raw)) return undefined;
  const out: AttachmentReceipt[] = [];
  for (const r of raw) {
    if (!r || typeof r !== "object") continue;
    const o = r as Record<string, unknown>;
    if (typeof o.name !== "string" || typeof o.document !== "string" || !o.document.startsWith("@attachments/")
      || typeof o.rev !== "number" || o.rev < 1 || typeof o.mediaType !== "string"
      || typeof o.size !== "number" || o.size < 0 || typeof o.sha256 !== "string") continue;
    out.push({ name: o.name, document: o.document, rev: o.rev, mediaType: o.mediaType, size: o.size, sha256: o.sha256 });
  }
  return out.length > 0 ? out : undefined;
}

export interface ChatEventLike {
  seq: number;
  kind: string;
  at?: string;
  payload?: unknown;
}

export interface FromEventsOptions {
  /**
   * Steer text by steer id. `step.steer.queued` deliberately does not carry the
   * text (the row remembers it), so the host supplies it — from `step.steers`
   * or from what the composer just sent.
   */
  steerTexts?: Record<number, string>;
  /** Tools whose allowed verdict also earns a divider ("policy allowed write_file"). */
  policyDividerTools?: string[];
  /** Agent name for an event that carries none and whose step was never introduced. */
  fallbackAgent?: string;
}

type Payload = Record<string, unknown>;

const str = (v: unknown): string | undefined => (typeof v === "string" && v !== "" ? v : undefined);
const num = (v: unknown): number | undefined => (typeof v === "number" && Number.isFinite(v) ? v : undefined);
const strings = (v: unknown): string[] | undefined =>
  Array.isArray(v) ? v.filter((x): x is string => typeof x === "string") : undefined;

function ms(from?: string, to?: string): number | undefined {
  if (!from || !to) return undefined;
  const d = Date.parse(to) - Date.parse(from);
  return Number.isFinite(d) && d >= 0 ? d : undefined;
}

const QUESTION_KINDS: readonly QuestionKind[] = ["clarify", "gate", "permission", "free"];
const questionKind = (v: unknown): QuestionKind =>
  (QUESTION_KINDS as readonly string[]).includes(v as string) ? (v as QuestionKind) : "free";

/** The daemon's literal for a call the policy gate parked on a person. */
const AWAITING_PREFIX = "suspended: awaiting a permission answer";

export function chatItemsFromEvents(events: readonly ChatEventLike[], options: FromEventsOptions = {}): ChatItem[] {
  const items: ChatItem[] = [];
  const policyDividers = new Set(options.policyDividerTools ?? ["write_file"]);
  const agentByStep = new Map<number, string>();
  const toolIndex = new Map<string, number>();
  const toolStartedAt = new Map<string, string | undefined>();
  const steerIndex = new Map<number, number>();
  const questionIndex = new Map<string, { item: number; q: number }>();
  /** Policy dividers waiting for their call to end, by toolUseId. */
  const pendingPolicyDivider = new Map<string, Extract<ChatItem, { kind: "divider" }>>();
  /** The streaming text/thinking item per step, so deltas append to it. */
  const openStream = new Map<number, number>();
  /** Run cards by runSeq, so the finish patches its own start in place. */
  const runIndex = new Map<number, number>();
  /** Message ids already drawn — the replayed ring must not double a row. */
  const seenMessage = new Set<number>();
  // S70 MB6: exchange rows are identified by their row id, live and on
  // replay — one row, one row of the stream, however often the ring
  // replays the announcements.
  const exchangeIndex = new Map<number, number>();
  /** (step, iteration) pairs whose text streamed as chunks, so the finished turn does not draw it a second time. */
  const streamedTurn = new Set<string>();
  /** Answers seen before their question (a window or a recovered row put the ask after it): applied when it appears. */
  const answeredEarly = new Map<string, string>();
  /** The ids of the items each turn's chunks made, so the finished turn can replace them with its whole text. */
  const streamedIds = new Map<string, { thinking: string[]; text: string[] }>();
  /** Chunk items a finished turn replaced; removed once, at the end, so no index held during the walk moves. */
  const superseded = new Set<string>();

  const sorted = [...events].sort((a, b) => a.seq - b.seq);

  const agentOf = (p: Payload): string => {
    const step = num(p.stepId);
    const named = str(p.agent);
    if (named) {
      if (step !== undefined) agentByStep.set(step, named);
      return named;
    }
    return (step !== undefined ? agentByStep.get(step) : undefined) ?? options.fallbackAgent ?? "agent";
  };

  const closeStream = (step?: number) => {
    const close = (i: number) => {
      const it = items[i];
      if (it && (it.kind === "text" || it.kind === "thinking")) items[i] = { ...it, streaming: false };
    };
    if (step === undefined) { openStream.forEach(close); openStream.clear(); return; }
    const i = openStream.get(step);
    if (i !== undefined) { close(i); openStream.delete(step); }
  };

  const patchTool = (key: string, patch: Partial<Extract<ChatItem, { kind: "tool" }>>) => {
    const i = toolIndex.get(key);
    if (i === undefined) return false;
    const it = items[i];
    if (it?.kind === "tool") items[i] = { ...it, ...patch };
    return true;
  };

  for (const ev of sorted) {
    const p: Payload = (ev.payload && typeof ev.payload === "object" ? ev.payload : {}) as Payload;
    const id = `e${ev.seq}`;
    const at = ev.at;
    const step = num(p.stepId);

    switch (ev.kind) {
      case "loop.started":
        items.push({ id, at, kind: "divider", tone: "orchestrator",
          text: `Orchestrator · loop started${str(p.blueprint) ? ` · ${str(p.blueprint)}` : ""}` });
        break;

      case "loop.step.dispatched": {
        const agent = str(p.agent) ?? "agent";
        if (step !== undefined) agentByStep.set(step, agent);
        const stepName = str(p.step) ?? str(p.lane) ?? "step";
        // Two agents sent to the same blueprint step back to back read as ONE
        // move ("dispatched surveyor + linter") — the shape parallel lanes
        // (P33/S32) will produce.
        const last = items[items.length - 1];
        if (last?.kind === "divider" && last.tone === "orchestrator" && last.id.startsWith("dispatch:") &&
            last.id.slice("dispatch:".length).split("|")[0] === stepName) {
          const highlights = [...(last.highlights ?? []), agent];
          items[items.length - 1] = { ...last, id: `${last.id}|${agent}`, highlights,
            text: `Orchestrator → step ${stepName} · dispatched ${highlights.join(" + ")}` };
        } else {
          items.push({ id: `dispatch:${stepName}|${ev.seq}`, at, kind: "divider", tone: "orchestrator", highlights: [agent],
            text: `Orchestrator → step ${stepName} · dispatched ${agent}` });
        }
        break;
      }

      case "loop.advanced": {
        const answer = str(p.answer);
        const lane = str(p.lane) ?? "lane";
        items.push({ id, at, kind: "divider", tone: "neutral",
          text: answer ? `${lane} answered · ${answer}` : `${lane} → ${str(p.outcome) ?? "advanced"}` });
        // A gate answered elsewhere (another window, the CLI) closes here.
        const qid = num(p.questionId);
        if (qid !== undefined) {
          const gi = items.findIndex((it) => it.kind === "gate" && it.id === `gate:${qid}`);
          const g = items[gi];
          if (g?.kind === "gate") items[gi] = { ...g, state: "answered", answer };
        }
        break;
      }

      case "loop.waiting": {
        if (str(p.reason) === "stopped") {
          items.push({ id, at, kind: "divider", tone: "neutral",
            text: `Paused · ${step !== undefined ? agentByStep.get(step) ?? "agent" : "agent"} was stopped` });
          break;
        }
        const answers = strings(p.answers);
        const qid = num(p.questionId);
        // A loop-level question with answers is a gate. One without is a step
        // question, already rendered from its own `question.asked`.
        if (answers && answers.length > 0 && qid !== undefined) {
          items.push({ id: `gate:${qid}`, at, kind: "gate", title: str(p.prompt) ?? "Approval needed",
            stepLabel: str(p.step) ?? str(p.lane), options: answers, state: "awaiting" });
        }
        break;
      }

      case "loop.skipped":
        items.push({ id, at, kind: "divider", tone: "neutral", text: `${str(p.lane) ?? "lane"} skipped${str(p.as) ? ` · ${str(p.as)}` : ""}` });
        break;
      case "loop.finished":
        closeStream();
        items.push({ id, at, kind: "divider", tone: "success", text: `Loop finished · ${str(p.status) ?? "done"}` });
        break;
      case "loop.message.posted": {
        // The agent's reply is already the stream's own text; drawing the
        // mirror too would print every sentence twice.
        if (str(p.author) === "agent") break;
        const messageId = num(p.messageId);
        if (messageId !== undefined) {
          if (seenMessage.has(messageId)) break; // the ring replayed this row
          seenMessage.add(messageId);
        }
        const source = str(p.source);
        // S68 B8: the row's chosen-skills receipt — NAMES only, carried
        // beside the body. The text above stays the person's pure words;
        // these render as chips UNDER the bubble (items.tsx), because the
        // instruction LINES they expand to live in the turn's system half,
        // which the window deliberately does not show.
        const skillNames = Array.isArray(p.skills)
          ? (p.skills as unknown[]).filter((x): x is string => typeof x === "string" && x.length > 0)
          : undefined;
        items.push({ id: messageId !== undefined ? `msg:${messageId}` : id, at, kind: "message",
          author: str(p.author) === "system" ? "system" : "human", text: str(p.body) ?? "",
          ...(messageId !== undefined ? { messageId } : {}),
          ...(skillNames && skillNames.length > 0 ? { skills: skillNames } : {}),
          // S69 AT8: the attachment receipts sit under the bubble too —
          // the person's body stays their pure words; the files are the
          // store's facts rendered as chips (cards.tsx).
          ...(receiptList(p.attachments) ? { attachments: receiptList(p.attachments) } : {}),
          ...(source === "template" || source === "automation" || source === "blueprint" || source === "user" ? { source } : {}) });
        break;
      }

      // ── S70 MB6 — the agents' exchange, drawn from the announcements of
      // the rows. Validate EVERY field or draw nothing: an event whose
      // payload half-believes the row would render a half-sent message.
      case "agent.message.posted": {
        const messageId = num(p.messageId);
        const thread = num(p.thread);
        const from = str(p.from);
        const to = str(p.to);
        const body = str(p.body);
        if (messageId === undefined || thread === undefined || from === undefined || to === undefined || body === undefined) break;
        const expectReply = p.expectReply === true;
        const atExisting = exchangeIndex.get(messageId);
        if (atExisting !== undefined) {
          const existing = items[atExisting];
          if (existing?.kind === "exchange") items[atExisting] = { ...existing, at: at ?? existing.at };
          break;
        }
        exchangeIndex.set(messageId, items.length);
        items.push({ id: `mx:${messageId}`, at, kind: "exchange", messageId, thread, from, to, text: body, expectReply, state: "posted" });
        break;
      }
      case "agent.message.delivered": {
        // The pair stamp is the row's own fact (D4). Announce it on the
        // row already drawn; a delivery whose row the ring has forgotten
        // is the READ's job to restore (agent.messages), not a reason to
        // invent a body from a payload that never carried one.
        const messageId = num(p.messageId);
        if (messageId === undefined) break;
        const i = exchangeIndex.get(messageId);
        if (i === undefined) break;
        const it = items[i];
        if (it?.kind === "exchange" && it.state === "posted") items[i] = { ...it, state: "delivered" };
        break;
      }
      case "agent.message.answered": {
        // The ask now carries the reply's words: the link settled (MB3).
        // The reply row announced itself separately; this event changes
        // only the ask's state, because that is the row the link points at.
        const messageId = num(p.messageId);
        if (messageId === undefined) break;
        const i = exchangeIndex.get(messageId);
        if (i === undefined) break;
        const it = items[i];
        if (it?.kind === "exchange") items[i] = { ...it, state: "answered" };
        break;
      }

      case "loop.run.started": {
        const runSeq = num(p.runSeq);
        if (runSeq === undefined) break;
        items.push({ id: `run:${runSeq}`, at, kind: "run", runSeq, blueprint: str(p.blueprint) ?? "",
          version: num(p.version), state: "running", startedBy: str(p.startedByKind) });
        runIndex.set(runSeq, items.length - 1);
        break;
      }

      case "loop.run.finished": {
        closeStream();
        const runSeq = num(p.runSeq);
        if (runSeq === undefined) break;
        const outcome = str(p.outcome);
        const state = outcome === "failed" ? "failed" : outcome === "cancelled" ? "cancelled" : "done";
        const raw = p.cost && typeof p.cost === "object" ? p.cost as Payload : undefined;
        const cost = raw ? { usdAtLeast: num(raw.usdAtLeast) ?? 0, exact: raw.exact === true } : undefined;
        const i = runIndex.get(runSeq);
        const it = i !== undefined ? items[i] : undefined;
        if (i !== undefined && it?.kind === "run") {
          items[i] = { ...it, state, ...(cost ? { cost } : {}) };
        } else {
          // Its start aged out of the ring — the card is pushed from the
          // finish alone; the blueprint name is not this event's to know.
          items.push({ id: `run:${runSeq}`, at, kind: "run", runSeq, blueprint: "", state, ...(cost ? { cost } : {}) });
          runIndex.set(runSeq, items.length - 1);
        }
        break;
      }

      case "loop.step.recovering": {
        // The step ended without filing what its lane declares: the engine gives it one more go, and says so.
        const missing = str(p.missing);
        items.push({ id, at, kind: "divider", tone: "neutral", text: `Recovering · ${missing ?? "the declared output"} was not filed (the step ended ${str(p.afterOutcome) ?? "early"}) — giving it one more go` });
        break;
      }

      case "loop.blocked": {
        closeStream();
        const reason = p.reason && typeof p.reason === "object" ? p.reason as Payload : undefined;
        const detail = reason ? [str(reason.kind), str(reason.detail)].filter(Boolean).join(" · ") : str(p.reason);
        items.push({ id, at, kind: "divider", tone: "danger", text: `Loop blocked${detail ? ` · ${detail}` : ""}` });
        break;
      }

      case "step.started": {
        agentOf(p);
        // S66: the daemon says which operator skills this step was OFFERED,
        // on the same event that opened the step. Its own row — "which
        // skills" is the question, and a divider tone cannot answer it.
        // Both lanes arrive here: live delivery and `subscribe after:0`
        // replay are the same code, so they cannot drift.
        const sk = p.skills && typeof p.skills === "object" ? (p.skills as Payload) : undefined;
        const skCount = sk ? num(sk.count) : undefined;
        if (sk && skCount !== undefined && skCount > 0 && Array.isArray(sk.names)) {
          const names = (sk.names as unknown[]).filter((n): n is string => typeof n === "string");
          const marker = str(sk.marker) ?? `skills injected: ${skCount} — ${names.join(", ")}`;
          items.push({ id, at, kind: "injection", count: skCount, names,
            ...(sk.truncated === true ? { truncated: true } : {}), marker });
        }
        break;
      }

      case "step.turn.completed": {
        // A model call, finished. When its chunks streamed, the items already hold the text and this only closes
        // them; when they did not (a reopened loop reads the journal without the chunks) the turn is drawn from
        // here, thinking first, so a reopened chat shows what the live one did.
        const agent = agentOf(p);
        const iteration = num(p.iteration) ?? 0;
        const key = `${step ?? -1}:${iteration}`;
        const thinking = str(p.thinking);
        const text = str(p.text);
        if (!streamedTurn.has(key)) {
          if (thinking) items.push({ id: `${id}:t`, at, agent, kind: "thinking", text: thinking });
          if (text) items.push({ id: `${id}:x`, at, agent, kind: "text", text });
        } else {
          // The chunks seen may be only part of the turn (the page joined mid-turn, or the model alternated
          // thinking and text chunk by chunk). The finished turn carries the whole of each, so it is the truth:
          // the first chunk item of that kind takes the whole text and the rest are dropped.
          const made = streamedIds.get(key);
          const whole = (ids: string[] | undefined, full: string | undefined) => {
            if (!ids || ids.length === 0 || !full) return;
            const first = items.findIndex((it) => it.id === ids[0]);
            const it = first >= 0 ? items[first] : undefined;
            if (it && (it.kind === "thinking" || it.kind === "text")) items[first] = { ...it, text: full };
            for (const other of ids.slice(1)) superseded.add(other);
          };
          whole(made?.thinking, thinking);
          whole(made?.text, text);
        }
        closeStream(step ?? -1);
        // The turn's own line: tokens, cost, time, and the step's running total. Shown for every turn, so a cost
        // that arrived with a response is on screen at that response, not only after a reload.
        const usage = p.usage && typeof p.usage === "object" ? (p.usage as Payload) : undefined;
        const money = (v: unknown): TurnCost | undefined => v && typeof v === "object" && ["known", "partial", "unknown"].includes(str((v as Payload).state) ?? "")
          ? { state: str((v as Payload).state) as TurnCost["state"], usd: num((v as Payload).usd), usdAtLeast: num((v as Payload).usdAtLeast), reason: str((v as Payload).reason) } : undefined;
        items.push({ id: `${id}:m`, at, agent, kind: "turn", iteration: iteration || undefined,
          inputTokens: usage ? num(usage.inputTokens) : undefined, outputTokens: usage ? num(usage.outputTokens) : undefined,
          reasoningTokens: usage ? num(usage.reasoningTokens) : undefined, wallMs: num(p.wallMs),
          cost: money(p.turnCost), stepCost: money(p.stepCost) });
        break;
      }

      case "model.delta": {
        const agent = agentOf(p);
        const kind = str(p.tool) === "thinking" ? "thinking" : "text";
        const turnKey = `${step ?? -1}:${num(p.iteration) ?? 0}`;
        streamedTurn.add(turnKey);
        const key = step ?? -1;
        const open = openStream.get(key);
        const it = open !== undefined ? items[open] : undefined;
        // The chunk continues the open row while nothing of THIS agent's has come after it. Another step's rows may
        // sit after it (two steps streaming at once, such as the loop's title step beside its first agent): that
        // is not an interruption, and starting a new row per chunk is what drew one line per word.
        const continues = it !== undefined && open !== undefined && it.kind === kind &&
          !items.slice(open + 1).some((later) => "agent" in later && later.agent === agent);
        if (it && continues && open !== undefined) {
          items[open] = { ...it, text: it.text + (str(p.text) ?? "") };
        } else {
          closeStream(key);
          items.push({ id, at, agent, kind, text: str(p.text) ?? "", streaming: true });
          openStream.set(key, items.length - 1);
          const made = streamedIds.get(turnKey) ?? { thinking: [], text: [] };
          made[kind === "thinking" ? "thinking" : "text"].push(id);
          streamedIds.set(turnKey, made);
        }
        break;
      }

      case "step.tool.start": {
        closeStream(step ?? -1);
        const toolUseId = str(p.toolUseId);
        const key = toolUseId ?? id;
        toolStartedAt.set(key, at);
        // The same call starting again is a resume (an approved permission, a
        // continue): it runs in place, it is not a second call.
        if (toolUseId && patchTool(key, { status: "running" })) break;
        items.push({ id: `tool:${key}`, at, agent: agentOf(p), kind: "tool", tool: str(p.tool) ?? "tool",
          toolUseId, stepId: step, summary: str(p.summary), status: "running", ...(str(p.command) ? { command: str(p.command) } : {}) });
        toolIndex.set(key, items.length - 1);
        break;
      }

      case "step.policy.decided": {
        const verdict = str(p.verdict);
        if (verdict !== "allowed" && verdict !== "denied" && verdict !== "asked") break;
        const key = str(p.toolUseId);
        const by = str(p.approvedBy);
        const approvedBy = by === "person" || by === "auto" || by === "policy" || by === "client" ? by : undefined;
        if (key) patchTool(key, { verdict: { verdict, reason: str(p.reason) }, ...(approvedBy ? { approvedBy } : {}) });
        if (verdict === "allowed" && policyDividers.has(str(p.tool) ?? "")) {
          // Shown after the call's END, so it reads as a verdict on the
          // result the way the design draws it. No key, no call to wait for.
          const divider = { id, at, kind: "divider" as const, tone: "policy" as const, text: `policy allowed ${str(p.tool)}` };
          if (key) pendingPolicyDivider.set(key, divider); else items.push(divider);
        }
        break;
      }

      case "step.tool.end": {
        const key = str(p.toolUseId);
        const result = str(p.result);
        const path = str(p.path);
        const prior = key ? items[toolIndex.get(key) ?? -1] : undefined;
        const denied = prior?.kind === "tool" && prior.verdict?.verdict === "denied";
        const status: ToolStatus = result?.startsWith(AWAITING_PREFIX) ? "awaiting" :
          denied ? "denied" : p.isError === true ? "error" : "done";
        const patch = {
          status, result,
          // The daemon measures a call's duration (it rides the end event, so a reopened loop has it); the gap between
          // the start and end events seen here is the fallback for an older daemon.
          durationMs: num(p.durationMs) ?? ms(key ? toolStartedAt.get(key) : undefined, at),
          ...(str(p.detail) ? { detail: str(p.detail) } : {}),
          ...(str(p.output) !== undefined ? { output: str(p.output), outputTruncated: p.outputTruncated === true } : {}),
          ...(path ? { file: { path, linesAdded: num(p.linesAdded), linesRemoved: num(p.linesRemoved) } } : {}),
        };
        if (!key || !patchTool(key, patch)) {
          // An end with no start we saw (a cursor that began mid-call).
          items.push({ id, at, agent: agentOf(p), kind: "tool", tool: str(p.tool) ?? "tool", toolUseId: key, stepId: step,
            summary: str(p.summary), ...patch });
          if (key) toolIndex.set(key, items.length - 1);
        }
        const divider = key ? pendingPolicyDivider.get(key) : undefined;
        if (key && divider) { pendingPolicyDivider.delete(key); items.push(divider); }
        break;
      }

      case "step.tool.skipped": {
        const key = str(p.toolUseId);
        const patch = { status: "skipped" as const, result: "not run — you steered before this call" };
        if (!key || !patchTool(key, patch)) {
          items.push({ id, at, agent: agentOf(p), kind: "tool", tool: str(p.tool) ?? "tool", toolUseId: key, ...patch });
        }
        break;
      }

      case "question.asked": {
        closeStream(step ?? -1);
        const agent = agentOf(p);
        const qid = num(p.id);
        const question: ChatQuestion = { id: qid !== undefined ? String(qid) : id, prompt: str(p.body) ?? "",
          kind: questionKind(p.kind), options: strings(p.options) };
        const early = answeredEarly.get(question.id);
        if (early !== undefined && question.kind !== "permission") question.answer = early;
        // A permission question about a call we rendered becomes that call's
        // approval card — the decision is about this exact call.
        const parkedOn = str(p.toolUseId);
        if (question.kind === "permission" && parkedOn && patchTool(parkedOn, { approval: question, status: "awaiting" })) {
          questionIndex.set(question.id, { item: toolIndex.get(parkedOn)!, q: -1 });
          break;
        }
        // Consecutive questions from one agent form one card ("4/4 answered").
        const last = items[items.length - 1];
        if (last?.kind === "question" && last.agent === agent && question.kind !== "permission" &&
            !last.questions.some((q) => q.kind === "permission")) {
          items[items.length - 1] = { ...last, questions: [...last.questions, question] };
          questionIndex.set(question.id, { item: items.length - 1, q: last.questions.length });
        } else {
          items.push({ id: `q:${question.id}`, at, agent, kind: "question", questions: [question] });
          questionIndex.set(question.id, { item: items.length - 1, q: 0 });
        }
        break;
      }

      case "question.answered": {
        const qid = num(p.questionId);
        const where = qid !== undefined ? questionIndex.get(String(qid)) : undefined;
        const it = where ? items[where.item] : undefined;
        if (!where && qid !== undefined) answeredEarly.set(String(qid), str(p.answer) ?? "");
        if (where && it?.kind === "tool" && it.approval) {
          // Declined: the call never runs. Approved: the step resumes and the
          // call's own start/end events take it from here.
          const answer = str(p.answer) ?? "";
          const declined = NEGATIVE_ANSWER.test(answer);
          items[where.item] = { ...it, approval: { ...it.approval, answer },
            status: declined ? "denied" : "running", ...(declined ? { result: "declined by you" } : {}),
            ...(!declined ? { approvedBy: str(p.answeredBy) === "client" ? ("client" as const) : ("person" as const) } : {}) };
        } else if (where && it?.kind === "question") {
          const questions = it.questions.map((q, i) => (i === where.q ? { ...q, answer: str(p.answer) ?? "" } : q));
          items[where.item] = { ...it, questions };
        }
        break;
      }

      case "step.steer.queued": {
        const steerId = num(p.steerId);
        items.push({ id: steerId !== undefined ? `steer:${steerId}` : id, at, kind: "steer", target: agentOf(p),
          text: steerId !== undefined ? options.steerTexts?.[steerId] : undefined,
          route: str(p.route), reasoningEffort: str(p.reasoningEffort), state: "queued", steerId,
          ...(receiptList(p.attachments) ? { attachments: receiptList(p.attachments) } : {}) });
        if (steerId !== undefined) steerIndex.set(steerId, items.length - 1);
        break;
      }
      case "step.steer.delivered":
      case "step.steer.cancelled": {
        const steerId = num(p.steerId);
        const i = steerId !== undefined ? steerIndex.get(steerId) : undefined;
        const it = i !== undefined ? items[i] : undefined;
        if (i !== undefined && it?.kind === "steer") {
          items[i] = { ...it, state: ev.kind === "step.steer.delivered" ? "delivered" : "cancelled" };
        }
        break;
      }

      case "step.route.changed":
        items.push({ id, at, kind: "divider", tone: "neutral",
          text: `${agentOf(p)} now on ${str(p.route) ?? "the same route"}${str(p.reasoningEffort) ? ` · effort ${str(p.reasoningEffort)}` : ""}` });
        break;
      case "step.stopped":
        closeStream(step ?? -1);
        items.push({ id, at, kind: "divider", tone: "danger", text: `${agentOf(p)} stopped by you` });
        break;
      case "step.continued":
        items.push({ id, at, kind: "divider", tone: "neutral", text: `${agentOf(p)} continued` });
        break;
      case "step.todo.nudged": {
        const reason = str(p.reason);
        const ids = (str(p.summary) ?? "").split(",").filter(Boolean);
        const what = reason === "completion" ? "before finishing" : reason === "closed-with-open" ? "finished with items open" : "while working";
        items.push({ id, at, kind: "divider", tone: "neutral", text: `reminder sent ${what}: ${ids.length} todo item(s) open` });
        break;
      }
      case "step.compacted":
        items.push({ id, at, kind: "divider", tone: "neutral", text: `${agentOf(p)} compacted its context` });
        break;
      case "step.done": {
        closeStream(step ?? -1);
        const outcome = str(p.outcome) ?? "done";
        items.push({ id, at, kind: "divider", tone: outcome === "ok" ? "success" : outcome === "failed" || outcome === "blocked" ? "danger" : "neutral",
          text: `${agentOf(p)} finished · ${outcome}${str(p.reason) ? ` · ${str(p.reason)}` : ""}`,
          ...(str(p.reason) ? { detail: str(p.reason) } : {}) });
        break;
      }

      default:
        break;
    }
  }
  return superseded.size === 0 ? items : items.filter((it) => !superseded.has(it.id));
}

/** One `agent.messages` row, typed the way the read answers it (MB7). The
 * view carries no nulls the client must invent around: absent optionals
 * are ABSENT (MB8's byte discipline), and 0 reads as "unset" exactly as
 * the store's own zero means it. */
export interface AgentMessageRowLike {
  id: number;
  thread: number;
  fromAgent: string;
  toAgent: string;
  body: string;
  expectReply: boolean;
  resultOf?: number;
  deliveredAt?: string;
  createdAt: string;
}

const stateRank = { posted: 0, delivered: 1, answered: 2 } as const;

/**
 * The fold of the READ into the stream (S70's reload rule, S39's rule
 * again): live events and `agent.messages` are the SAME rows, so they may
 * never appear twice. A row the stream already drew is kept where the
 * stream put it, with the further state (posted < delivered < answered
 * only ever moves forward — the daemon's settlement never rewinds); a row
 * the ring has forgotten joins from the read, verbatim. Fields are
 * validated as strictly as the event arm's: a row that half-believes
 * itself draws nothing.
 */
export function mailboxItemsFromRows(rows: readonly AgentMessageRowLike[]): ChatItem[] {
  const out: ChatItem[] = [];
  const asked = new Set<number>();
  for (const r of rows) if (typeof r.resultOf === "number" && r.resultOf > 0) asked.add(r.resultOf);
  for (const r of rows) {
    if (typeof r.id !== "number" || r.id <= 0) continue;
    if (typeof r.thread !== "number" || r.thread <= 0) continue;
    if (typeof r.fromAgent !== "string" || r.fromAgent === "" || typeof r.toAgent !== "string" || r.toAgent === "") continue;
    if (typeof r.body !== "string") continue;
    if (typeof r.createdAt !== "string" || r.createdAt === "") continue;
    const state: "posted" | "delivered" | "answered" =
      asked.has(r.id) ? "answered" : (r.deliveredAt !== undefined && r.deliveredAt !== "" ? "delivered" : "posted");
    out.push({ id: `mx:${r.id}`, at: r.createdAt, kind: "exchange", messageId: r.id, thread: r.thread,
      from: r.fromAgent, to: r.toAgent, text: r.body, expectReply: r.expectReply === true, state });
  }
  return out;
}

/** Merge the read's rows into items the stream built — one row, one item,
 * the further state wins (see `mailboxItemsFromRows`). */
export function mergeMailboxItems(items: readonly ChatItem[], rows: readonly ChatItem[]): ChatItem[] {
  const index = new Map<number, number>();
  items.forEach((it, i) => { if (it.kind === "exchange") index.set(it.messageId, i); });
  const merged = [...items];
  for (const row of rows) {
    if (row.kind !== "exchange") continue;
    const at = index.get(row.messageId);
    if (at === undefined) { merged.push(row); continue; }
    const existing = merged[at];
    if (existing && existing.kind === "exchange" && stateRank[row.state] > stateRank[existing.state]) {
      merged[at] = { ...existing, state: row.state };
    }
  }
  return merged;
}
