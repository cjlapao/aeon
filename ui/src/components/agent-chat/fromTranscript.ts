import { runtimeTurn } from "./fromLoopTranscript";
import type { ChatItem, ToolStatus } from "./types";

/**
 * A saved transcript (`step.show`'s `messages`) → chat items, for a step that
 * is no longer streaming. The transcript has no timestamps and no policy
 * verdicts — those live only on the stream — so this is the history view, not
 * a replacement for `chatItemsFromEvents` while a step runs.
 */
export interface TranscriptLikeBlock {
  kind: string;
  text?: string;
  id?: string;
  name?: string;
  input?: unknown;
  toolUseId?: string;
  content?: unknown;
  isError?: boolean;
}
export interface TranscriptLikeMessage {
  role: string;
  content: TranscriptLikeBlock[];
}

export interface FromTranscriptOptions {
  agent: string;
  /** Show the first user message (the task the step was given). Default true. */
  includePrompt?: boolean;
  /** The step is still running: an unanswered call is in flight, not parked. */
  running?: boolean;
}

/** The LW2 sentence a steer leaves on every call it closed. */
const STEERED = "not run: the person steered";

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

export function chatItemsFromTranscript(messages: readonly TranscriptLikeMessage[], options: FromTranscriptOptions): ChatItem[] {
  const items: ChatItem[] = [];
  const toolAt = new Map<string, number>();
  let userTurns = 0;

  messages.forEach((message, mi) => {
    message.content.forEach((block, bi) => {
      const id = `t${mi}.${bi}`;
      if (message.role === "assistant") {
        if (block.kind === "thinking") {
          // A signature-only block has no text to show; say it happened.
          items.push({ id, agent: options.agent, kind: "thinking", text: block.text || "Thinking occurred." });
        } else if (block.kind === "text" && block.text) {
          items.push({ id, agent: options.agent, kind: "text", text: block.text });
        } else if (block.kind === "tool_use") {
          const path = block.input && typeof block.input === "object" ? (block.input as Record<string, unknown>).path : undefined;
          items.push({ id, agent: options.agent, kind: "tool", tool: block.name ?? "tool", toolUseId: block.id,
            summary: summarize(block.input), status: "running", detail: asText(block.input),
            ...(block.name === "bash" && block.input && typeof (block.input as Record<string, unknown>).command === "string"
              ? { command: (block.input as Record<string, unknown>).command as string } : {}),
            ...(block.name === "write_file" && typeof path === "string" ? { file: { path } } : {}) });
          if (block.id) toolAt.set(block.id, items.length - 1);
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
            items[i] = { ...it, status, result: content.split("\n")[0], detail: content, ...(it.tool === "bash" ? { output: content } : {}) };
          }
          return;
        }
        if (block.kind === "text" && block.text) {
          const first = userTurns === 0;
          if (first && options.includePrompt === false) return;
          // A turn the runtime wrote (a todo reminder) is not the person steering.
          const runtime = first ? undefined : runtimeTurn(block.text);
          if (runtime) { items.push({ id, kind: "divider", tone: "neutral", text: runtime }); return; }
          // After the task, a person's text in a step's transcript is a steer
          // or a continue message — delivered by definition, it is saved.
          items.push(first
            ? { id, agent: "you", kind: "text", text: block.text }
            : { id, kind: "steer", target: options.agent, text: block.text, state: "delivered" });
        }
      }
    });
    if (message.role === "user") userTurns++;
  });
  // A call with no result is in flight on a running step, and parked (on a
  // permission question, or a crash) on any other.
  if (options.running) return items;
  return items.map((it) => (it.kind === "tool" && it.status === "running" ? { ...it, status: "awaiting" as const } : it));
}
