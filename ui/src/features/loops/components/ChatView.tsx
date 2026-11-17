import { useState } from "react";
import { AppIcon } from "../../../components/icons";
import { Badge, Button, Select, UserAvatar } from "../../../components/kit";
import { toRpcError } from "../../../lib/bridge";
import { availableEfforts } from "../../models/modelConfig";
import type { PendingQuestion, RouteView } from "../../../lib/protocol";
import { QuestionCard } from "./QuestionCard";

/**
 * The step's chat view (S9+, the "we now have a chat concept in the loop"
 * work). Everything here reuses the S5/S6 transcript and event shapes — a
 * step's `messages` and the `step.*`/`model.delta` events it already emits —
 * so nothing about S5-S10 changed to build this; only how it is RENDERED did.
 */

export interface ChatBlock {
  kind: string;
  text?: string;
  name?: string;
  input?: unknown;
  output?: unknown;
  content?: unknown;
  /** Present on a signed thinking block. Never rendered — see `BlockView`. */
  signature?: string;
  toolUseId?: string;
  id?: string;
  isError?: boolean;
}
export interface ChatMessage { role: string; content: ChatBlock[] }
export interface ChatStreamEntry { seq: number; kind: string; text?: string; tool?: string; summary?: string; result?: string }

function roleLabel(role: string): string {
  return role === "assistant" ? "Aeon" : role === "user" ? "You" : role;
}

/**
 * A thinking block's `signature` exists for S7's replay and is never shown —
 * that much survives from the old Transcript. What changed: the block's own
 * `text`, when the model produced one, is no longer withheld. "Thinking
 * occurred" only remains as the fallback for a signature-only block.
 */
function BlockView({ block }: { block: ChatBlock }) {
  if (block.kind === "thinking") {
    return block.text ? <p className="mb-1 whitespace-pre-wrap break-words text-xs italic opacity-75"><AppIcon name="think" size={12} className="mr-1 inline-block align-[-1px]" />{block.text}</p> :
      <p className="mb-1 text-xs italic opacity-60">Thinking occurred.</p>;
  }
  if (block.kind.includes("tool") || block.name) return <details className="mt-1 rounded-lg border border-white/10 bg-black/20 p-2 text-xs">
    <summary className="cursor-pointer select-none">
      {block.isError ? <AppIcon name="cross" size={12} className="mr-1 inline-block align-[-1px] text-red-500" /> :
        block.kind === "tool_result" ? <AppIcon name="check" size={12} className="mr-1 inline-block align-[-1px] text-emerald-500" /> : "→ "}
      {block.name || block.kind}: {String(block.text || block.content || "tool call").slice(0, 160)}
    </summary>
    <pre className="mt-2 whitespace-pre-wrap break-all">{JSON.stringify({ input: block.input, output: block.output, content: block.content, text: block.text }, null, 2)}</pre>
  </details>;
  return <p className="whitespace-pre-wrap break-words">{block.text ?? JSON.stringify(block.content ?? "")}</p>;
}

function MessageBubble({ message, index }: { message: ChatMessage; index: number }) {
  return <li key={index} className="flex min-w-0 gap-3 rounded-lg border border-white/10 p-3">
    <UserAvatar user={{ name: roleLabel(message.role) }} size="sm" />
    <div className="min-w-0 flex-1">
      <Badge variant="soft" size="sm" tone="neutral">{message.role}</Badge>
      <div className="mt-2 space-y-2 text-sm">{message.content.map((block, i) => <BlockView key={i} block={block} />)}</div>
    </div>
  </li>;
}

type LiveSegment = { kind: "text" | "thinking"; text: string } | { kind: "tool"; label: string };

/**
 * One line per model-delta RUN, not per event — a delta arrives chunk by
 * chunk (K19), and a chat bubble that broke on every chunk boundary would
 * read as a different sentence every few characters.
 */
function groupStream(entries: ChatStreamEntry[]): LiveSegment[] {
  const segments: LiveSegment[] = [];
  for (const entry of entries) {
    if (entry.kind === "model.delta") {
      const kind: "text" | "thinking" = entry.tool === "thinking" ? "thinking" : "text";
      const last = segments[segments.length - 1];
      if (last && last.kind === kind) last.text += entry.text ?? "";
      else segments.push({ kind, text: entry.text ?? "" });
    } else {
      segments.push({ kind: "tool", label: `${entry.tool} ${entry.kind === "step.tool.start" ? "started" : "finished"}: ${entry.summary || entry.result || ""}` });
    }
  }
  return segments;
}

function ContinuePanel({ routes, onContinue }: { routes: RouteView[] | null; onContinue: (route?: string, reasoningEffort?: string) => Promise<void> }) {
  const [route, setRoute] = useState("");
  const [reasoningEffort, setReasoningEffort] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  return <div className="mt-3 rounded-lg border border-amber-300 p-3 text-sm" data-testid="step-continue-panel">
    <p className="font-medium">Stopped. Continue as-is, or change the provider or reasoning effort first.</p>
    {error && <p role="alert" className="mt-1 text-red-700">{error}</p>}
    <div className="mt-2 flex flex-wrap items-end gap-3">
      <label className="text-xs uppercase tracking-wide">Provider
        <Select className="mt-1 block" value={route} onChange={(event) => { setRoute(event.target.value); setReasoningEffort(""); }} data-testid="step-continue-route">
          <option value="">keep current</option>
          {(routes ?? []).map((r) => <option key={r.id} value={r.id}>{r.displayName || r.id} — {r.provider}/{r.model}</option>)}
        </Select>
      </label>
      <label className="text-xs uppercase tracking-wide">Reasoning effort
        <Select className="mt-1 block" value={reasoningEffort} onChange={(event) => setReasoningEffort(event.target.value)} data-testid="step-continue-effort">
          <option value="">keep current</option>
          {availableEfforts(routes?.find((r) => r.id === route)).map((e) => <option key={e} value={e}>{e}</option>)}
        </Select>
      </label>
      <Button size="sm" loading={busy} disabled={busy} data-testid="step-continue-submit"
        onClick={async () => {
          setBusy(true); setError("");
          try { await onContinue(route || undefined, reasoningEffort || undefined); }
          catch (cause) { setError(toRpcError(cause).message); }
          finally { setBusy(false); }
        }}>Continue</Button>
    </div>
  </div>;
}

export function ChatView({ messages, stream, question, status, pauseReason, routes, onStop, onAnswer, onContinue }: {
  messages: ChatMessage[];
  stream: ChatStreamEntry[];
  /** Present only while a REAL model question is pending — a user-initiated stop never sets this. */
  question?: PendingQuestion;
  status: string;
  pauseReason?: string;
  routes: RouteView[] | null;
  onStop: () => Promise<void>;
  onAnswer: (answer: string) => Promise<void>;
  onContinue: (route?: string, reasoningEffort?: string) => Promise<void>;
}) {
  const [stopBusy, setStopBusy] = useState(false);
  const [stopError, setStopError] = useState("");
  const [answerBusy, setAnswerBusy] = useState(false);
  const live = groupStream(stream);
  return <div data-testid="step-chat">
    {status === "running" && <div className="mb-3 flex justify-end">
      <Button size="sm" variant="outline" loading={stopBusy} disabled={stopBusy} data-testid="step-stop"
        onClick={async () => {
          setStopBusy(true); setStopError("");
          try { await onStop(); } catch (cause) { setStopError(toRpcError(cause).message); } finally { setStopBusy(false); }
        }}>Stop</Button>
    </div>}
    {stopError && <p role="alert" className="mb-2 text-red-700">{stopError}</p>}
    {messages.length === 0 && stream.length === 0 ? <p className="text-sm text-slate-600">No transcript entries yet.</p> :
      <ol className="space-y-3">{messages.map((message, index) => <MessageBubble key={index} message={message} index={index} />)}</ol>}
    {stream.length > 0 && <div className="mt-3 rounded-lg border border-amber-300 p-3 text-sm" aria-label="Live transcript events">
      <p className="font-medium">Live activity · awaiting saved transcript</p>
      <ol className="mt-2 space-y-1">{live.map((segment, index) => <li key={index} className="whitespace-pre-wrap break-words">
        {segment.kind === "tool" ? segment.label : segment.kind === "thinking"
          ? <><AppIcon name="think" size={12} className="mr-1 inline-block align-[-1px]" />{segment.text}</> : segment.text}
      </li>)}</ol>
    </div>}
    {question && <div className="mt-3"><QuestionCard prompt={question.body} options={question.options ?? []} busy={answerBusy}
      onAnswer={async (value) => { setAnswerBusy(true); try { await onAnswer(value); } finally { setAnswerBusy(false); } }} /></div>}
    {status === "suspended" && pauseReason === "user" && <ContinuePanel routes={routes} onContinue={onContinue} />}
  </div>;
}
