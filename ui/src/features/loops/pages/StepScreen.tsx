import { useCallback, useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { Panel, Pill } from "../../../components/kit";
import { onEvent, rpc, toRpcError } from "../../../lib/bridge";
import { costLine } from "../../../lib/cost";
import { RecoveryNotice } from "../../../lib/uncertain";
import type { LiveController, LiveState } from "../../../lib/live";
import type { AeonEvent, Cost, PendingQuestion, RouteView, RoutesListResult, StepContinueParams, StepStopResult } from "../../../lib/protocol";
import { ChatView, type ChatMessage, type ChatStreamEntry } from "../components/ChatView";

type Message = ChatMessage;
interface Manifest { files?: Array<{ path: string; hash: string; changed?: boolean; missing?: boolean }>; observedChanges?: Array<{ path: string; kind: "changed" | "missing" }>;
  iterationsUsed?: number; maxIterations?: number; deadlineAt?: string;
  compactions?: Array<{ start: number; end: number; estimateBefore: number; estimateAfter: number; summary: string; cost: Cost }> }
interface StepDetail { id: number; status: string; agent: string; phase: string; cost: Cost; messages?: Message[]; contextManifest?: Manifest;
  recovery?: { case: string; reissued: boolean; reason?: string; priorCost?: Cost; reissueCost?: Cost;
    failure?: { kind: string; tool?: string; detail: string } }; outcome?: string; reason?: string;
  /** Present while suspended waiting for a REAL model question. */
  question?: PendingQuestion;
  /** Present while suspended because a PERSON stopped the step — see ChatView. */
  pauseReason?: string }

function transcriptFacts(messages: Message[] = []) {
  const changes: Array<{ kind: "Changed" | "Missing"; text: string }> = [];
  let compactionAt = 0;
  messages.forEach((message, index) => message.content.forEach((block) => {
    if (block.kind !== "text" || typeof block.text !== "string") return;
    if (block.text.startsWith("Earlier step context, compacted:")) compactionAt = index + 1;
    if (message.role !== "user") return;
    for (const line of block.text.split("\n")) {
      if (line.startsWith("Changed while this step waited:")) changes.push({ kind: "Changed", text: line });
      if (line.startsWith("Missing while this step waited:")) changes.push({ kind: "Missing", text: line });
    }
  }));
  return { changes, compactionAt };
}

export function Transcript({ messages = [] }: { messages?: Message[] }) {
  if (!messages.length) return <p className="text-sm text-slate-600">No transcript entries yet.</p>;
  return <ol className="space-y-3">{messages.map((message, index) => <li key={index} className="min-w-0 rounded-lg border border-white/10 p-3">
    <strong className="text-xs uppercase tracking-widest aeon-accent-text">{message.role}</strong>
    {message.content.map((block, position) => <div key={position} className="mt-2 min-w-0 text-sm">
      {block.kind === "thinking" ? <p>Thinking occurred · content withheld</p> :
        block.kind.includes("tool") || block.name ? <details className="rounded-lg bg-black/20 p-2">
          <summary className="cursor-pointer">{block.name || block.kind}: {String(block.text || "tool call").slice(0, 160)}</summary>
          <pre className="mt-2 whitespace-pre-wrap break-all text-xs">{JSON.stringify({ input: block.input, output: block.output, content: block.content, text: block.text }, null, 2)}</pre>
        </details> : <p className="whitespace-pre-wrap break-words">{block.text ?? JSON.stringify(block.content ?? "")}</p>}
    </div>)}</li>)}</ol>;
}

export function StepScreen({ controller, live }: { controller: LiveController; live: LiveState }) {
  const { id = "" } = useParams();
  const [step, setStep] = useState<StepDetail | null>(null);
  const [stream, setStream] = useState<ChatStreamEntry[]>([]);
  const [error, setError] = useState("");
  const [routes, setRoutes] = useState<RouteView[] | null>(null);
  const read = useCallback(async () => {
    const numeric = Number(id);
    if (!Number.isSafeInteger(numeric) || numeric <= 0) { setError("Invalid step ID"); return; }
    try { setStep(await rpc<StepDetail>("step.show", { id: numeric })); setStream([]); setError(""); }
    catch (cause) { setError(toRpcError(cause).message); }
  }, [id]);
  useEffect(() => controller.registerReader(read), [controller, read]);
  useEffect(() => { if (live.state === "current") void read(); }, [live.state, read]);
  // Only needed for the continue panel's provider picker — fetched once, same
  // pattern as the test-bench Step page, and just as harmless to fetch early.
  useEffect(() => {
    void (async () => {
      try { setRoutes((await rpc<RoutesListResult>("routes.list")).routes ?? []); }
      catch { /* the continue panel falls back to "keep current" with no options */ }
    })();
  }, []);
  const stepId = Number(id);
  const onStop = useCallback(async () => { await rpc<StepStopResult>("step.stop", { stepId }); await read(); }, [stepId, read]);
  const onContinue = useCallback(async (route?: string, reasoningEffort?: string) => {
    const params: StepContinueParams = { stepId };
    if (route) params.route = route;
    if (reasoningEffort) params.reasoningEffort = reasoningEffort;
    await rpc("step.continue", params); await read();
  }, [stepId, read]);
  const onAnswer = useCallback(async (answer: string) => {
    if (!step?.question) return;
    await rpc("answer", { stepId: step.id, questionId: step.question.id, answer }); await read();
  }, [step?.id, step?.question, read]);
  useEffect(() => {
    if (live.state !== "current") return;
    let disposed = false, stop: (() => void) | undefined, subscriptionId = "";
    void (async () => {
      try {
        stop = await onEvent((raw) => { const event = raw as AeonEvent; const payload = event.payload as Record<string, unknown> | undefined;
          if (payload?.stepId !== Number(id)) return;
          if (event.kind === "model.delta" || event.kind === "step.tool.start" || event.kind === "step.tool.end") {
            setStream((current) => current.some((entry) => entry.seq === event.seq) ? current : [...current, {
              seq: event.seq, kind: event.kind, text: String(payload.text || ""), tool: String(payload.tool || ""),
              summary: String(payload.summary || ""), result: String(payload.result || ""),
            }].sort((a, b) => a.seq - b.seq));
          } else if (event.kind.startsWith("step.")) void read(); });
        const sub = await rpc<{ subscriptionId: string }>("subscribe", { filter: { kinds: ["step.*", "model.delta"] } });
        subscriptionId = sub.subscriptionId; if (disposed) stop?.();
      } catch (cause) { if (!disposed) setError(toRpcError(cause).message); }
    })();
    return () => { disposed = true; stop?.(); if (subscriptionId) void rpc("unsubscribe", { subscriptionId }); };
  }, [id, live.state === "current" ? live.generation : -1, read]);
  const facts = transcriptFacts(step?.messages);
  const observations = step?.contextManifest?.observedChanges;
  const fileKinds = new Map(observations?.map((change) => [change.path, change.kind]));
  return <section className="space-y-5" data-testid="product-step">
    <Link to="/" className="text-sm aeon-accent-text underline">← Loops</Link>
    {error && <Panel tone="red"><p role="alert">{error}</p></Panel>}
    {!step ? <Panel>{error ? "Step unavailable." : "Loading step…"}</Panel> : <>
      <div className="flex flex-wrap items-center justify-between gap-3"><div><p className="text-xs uppercase tracking-widest aeon-accent-text">{step.agent} · {step.phase}</p>
        <h1 className="mt-1 text-3xl font-semibold">Step {step.id}</h1></div><Pill tone={step.status === "failed" ? "red" : "emerald"}>{step.status}</Pill></div>
      <Panel><h2 className="text-lg font-semibold">Run facts</h2><p className="mt-2">Cost: {costLine(step.cost).text}</p>
        <p>Iterations: {step.contextManifest?.iterationsUsed ?? "unknown"} of {step.contextManifest?.maxIterations ?? "unknown"}</p>
        <p>Deadline: {step.contextManifest?.deadlineAt ?? "not recorded"}</p>
        {step.reason && <p>Stop reason: {step.reason}</p>}
        {step.contextManifest?.compactions?.map((item, index) => <div key={`${item.start}-${item.end}-${index}`} className="mt-2 rounded-lg border border-slate-200 p-2 text-sm">
          <p>Compaction {index + 1}: transcript entries {item.start}–{item.end}; estimate {item.estimateBefore} → {item.estimateAfter} tokens.</p>
          <p>Summary model call: {costLine(item.cost).text}. Included in the step total.</p>
          <details><summary className="cursor-pointer">Show summary</summary><p className="mt-1 whitespace-pre-wrap break-words">{item.summary}</p></details>
        </div>)}
        {!step.contextManifest?.compactions?.length && facts.compactionAt ?
          <p>Earlier context compacted at transcript entry {facts.compactionAt}; the summary model call's cost is included in the step total.</p> : null}
        {step.recovery && <p>Recovery case {step.recovery.case}: <RecoveryNotice outcome={step.recovery.failure ? "failed" : "recovered"}
          tool={step.recovery.failure?.tool} receiptState={step.recovery.failure?.kind} detail={step.recovery.failure?.detail || step.recovery.reason} /></p>}
        {step.recovery?.case === "B" && <div className="mt-2 rounded-lg border border-amber-300 p-2 text-sm">
          <p>Before crash, including an interrupted call that may be billed: {costLine(step.recovery.priorCost).text}</p>
          <p>Reissued work after restart: {costLine(step.recovery.reissueCost).text}</p>
        </div>}
      </Panel>
      <Panel><h2 className="mb-3 text-lg font-semibold">Context manifest</h2>
        {!observations?.length && facts.changes.length > 0 && <ul className="mb-3 space-y-1">{facts.changes.map((change, index) =>
          <li key={index} data-change-kind={change.kind.toLowerCase()} className={change.kind === "Missing" ? "text-red-700" : "text-amber-800"}>{change.text}</li>)}</ul>}
        {!step.contextManifest?.files?.length ? <p className="text-sm text-slate-600">No declared files.</p> :
          <ul>{step.contextManifest.files.map((file) => <li key={file.path} data-change-kind={fileKinds.get(file.path)} className="break-all py-1 text-sm">
            {fileKinds.get(file.path) === "missing" || file.missing ? "Missing" : fileKinds.get(file.path) === "changed" || file.changed ? "Changed while suspended" : "Declared"}: {file.path} · {file.hash}</li>)}</ul>}
      </Panel>
      <Panel><h2 className="mb-3 text-lg font-semibold">Transcript</h2>
        <ChatView messages={step.messages ?? []} stream={stream} question={step.question} status={step.status}
          pauseReason={step.pauseReason} routes={routes} onStop={onStop} onAnswer={onAnswer} onContinue={onContinue} />
      </Panel>
    </>}
  </section>;
}
