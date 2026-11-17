import type { ChatItem, ToolStatus } from "./types";
import type { TranscriptLikeBlock } from "./fromTranscript";

/**
 * A `loop.transcript` page → chat items — the cross-step sibling of
 * `chatItemsFromTranscript`. That function renders ONE step, with one shared
 * `agent` option; a loop-wide page spans however many steps and agents a
 * scroll window happens to cover, so each message carries its own `stepId` /
 * `agent` / `phase` rather than the caller supplying one.
 *
 * `at` is real here (`step_message.created_at`), unlike a single step's own
 * `chatItemsFromTranscript`, whose doc comment notes the per-step read has no
 * timestamps — this one does, because it is a different read of the same
 * table (`internal/store/transcript.go`'s `ReadLoopTranscriptPage`).
 */
export interface LoopTranscriptMessageLike {
  id: number;
  stepId: number;
  agent: string;
  phase: string;
  role: string;
  createdAt: string;
  content?: readonly TranscriptLikeBlock[];
  /** Set instead of `content` — a row this build cannot parse. Named, not silently dropped (P31). */
  unreadable?: string;
}

/** The LW2 sentence a steer leaves on every call it closed. */
const STEERED = "not run: the person steered";

/**
 * Turns the RUNTIME wrote into the conversation (internal/runtime/todo_text.go),
 * not the person: drawn as the divider the live chat shows for the same moment,
 * never as something the person said. The openings below are those sentences'.
 */
const NUDGE_TURNS: readonly { opening: string; what: string }[] = [
  { opening: "Reminder: your todo list has ", what: "while working" },
  { opening: "Before you finish: your todo list still has ", what: "before finishing" },
];
const TODO_RESTORED = "Your todo list, as it stands now (earlier turns were compacted)";

export function runtimeTurn(text: string): string | undefined {
  const nudge = NUDGE_TURNS.find((n) => text.startsWith(n.opening));
  if (nudge) {
    const count = /^\D*(\d+) open item/.exec(text)?.[1] ?? "some";
    return `reminder sent ${nudge.what}: ${count} todo item(s) open`;
  }
  if (text.startsWith(TODO_RESTORED)) return "todo list restored after the context was compacted";
  return undefined;
}

/** Time between a call and its result, from the two rows' own timestamps. */
function elapsedMs(from: string, to: string): number | undefined {
  const ms = Date.parse(to) - Date.parse(from);
  return Number.isFinite(ms) && ms >= 0 ? ms : undefined;
}

function summarize(input: unknown): string | undefined {
  if (!input || typeof input !== "object") return undefined;
  const o = input as Record<string, unknown>;
  for (const key of ["description", "path", "command", "pattern", "query", "text"]) {
    if (typeof o[key] === "string" && o[key]) return String(o[key]).split("\n")[0];
  }
  return undefined;
}

const asText = (v: unknown): string | undefined =>
  typeof v === "string" ? v : v === undefined || v === null ? undefined : JSON.stringify(v, null, 2);

export function chatItemsFromLoopTranscriptPage(messages: readonly LoopTranscriptMessageLike[]): ChatItem[] {
  const items: ChatItem[] = [];
  const toolAt = new Map<string, number>();
  const toolStartedAt = new Map<string, string>();
  // The first USER message of a given step is the task it was dispatched
  // with; every one after is a steer or a continue — tracked per step id,
  // never globally, since a page can interleave several steps' turns.
  const userTurnsByStep = new Map<number, number>();

  for (const message of messages) {
    if (message.unreadable) {
      items.push({ id: `m${message.id}`, kind: "divider", tone: "neutral", text: message.unreadable, at: message.createdAt });
      continue;
    }
    (message.content ?? []).forEach((block, bi) => {
      const id = `m${message.id}.${bi}`;
      if (message.role === "assistant") {
        if (block.kind === "thinking") {
          items.push({ id, agent: message.agent, kind: "thinking", text: block.text || "Thinking occurred.", at: message.createdAt });
        } else if (block.kind === "text" && block.text) {
          items.push({ id, agent: message.agent, kind: "text", text: block.text, at: message.createdAt });
        } else if (block.kind === "tool_use") {
          const path = block.input && typeof block.input === "object" ? (block.input as Record<string, unknown>).path : undefined;
          items.push({
            id, agent: message.agent, kind: "tool", tool: block.name ?? "tool", toolUseId: block.id, stepId: message.stepId,
            summary: summarize(block.input), status: "running", detail: asText(block.input), at: message.createdAt,
            ...(block.name === "bash" && block.input && typeof (block.input as Record<string, unknown>).command === "string"
              ? { command: (block.input as Record<string, unknown>).command as string } : {}),
            ...(block.name === "write_file" && typeof path === "string" ? { file: { path } } : {}),
          });
          if (block.id) { toolAt.set(block.id, items.length - 1); toolStartedAt.set(block.id, message.createdAt); }
        }
        return;
      }
      if (message.role === "user") {
        if (block.kind === "tool_result") {
          const i = block.toolUseId ? toolAt.get(block.toolUseId) : undefined;
          const it = i !== undefined ? items[i] : undefined;
          if (i !== undefined && it?.kind === "tool") {
            const content = asText(block.content) ?? "";
            const status: ToolStatus = content.startsWith(STEERED) ? "skipped" : block.isError ? "error" : "done";
            const durationMs = status === "done" && block.toolUseId ? elapsedMs(toolStartedAt.get(block.toolUseId) ?? "", message.createdAt) : undefined;
            items[i] = { ...it, status, ...(durationMs !== undefined ? { durationMs } : {}), result: content.split("\n")[0], detail: content, ...(it.tool === "bash" ? { output: content } : {}) };
          }
          return;
        }
        if (block.kind === "text" && block.text) {
          const runtime = runtimeTurn(block.text);
          if (runtime) {
            items.push({ id, kind: "divider", tone: "neutral", text: runtime, at: message.createdAt });
            return;
          }
          const seen = userTurnsByStep.get(message.stepId) ?? 0;
          const first = seen === 0;
          userTurnsByStep.set(message.stepId, seen + 1);
          // The task is how the orchestrator dispatched the step: the divider the
          // live chat draws from `loop.step.dispatched`, from the same step row.
          if (first) {
            items.push({ id: `dispatch:${message.stepId}`, at: message.createdAt, kind: "divider", tone: "orchestrator", highlights: [message.agent],
              text: `Orchestrator → step ${message.phase || "step"} · dispatched ${message.agent}` });
          }
          // The first user turn of a step is the orchestrator's brief to the agent, not something the person
          // typed: it is drawn as a quiet row under the dispatch divider, never as "you" (which repeated once per
          // agent and sat between their turns). A later user turn is an instruction the person delivered.
          items.push(first
            ? { id, kind: "divider", tone: "neutral", text: `Brief for ${message.agent}: ${briefLine(block.text)}`, detail: block.text, at: message.createdAt }
            : { id, kind: "steer", target: message.agent, text: block.text, state: "delivered", at: message.createdAt });
        }
      }
    });
  }
  // A page from this read is, by construction, never the live tail (the
  // workspace hook only reaches for `loop.transcript` on steps the live
  // event ring has no knowledge of) — so a call with no matching result is
  // parked, same as any other non-running read of a transcript.
  return items.map((it) => (it.kind === "tool" && it.status === "running" ? { ...it, status: "awaiting" as const } : it));
}

/** The first line of a brief, cut to fit a row. The whole text rides `detail`. */
function briefLine(text: string): string {
  const line = text.trim().split("\n")[0] ?? "";
  return line.length > 140 ? `${line.slice(0, 140)}…` : line;
}
