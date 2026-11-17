import { useCallback, useEffect, useRef, useState } from "react";
import { Alert, Button, FormField, Input, Panel, Pill } from "../../../components/kit";
import { listenerUnavailable, onEvent, rpc, toRpcError, type RpcError } from "../../../lib/bridge";
import type { AeonEvent, SubscribeResult } from "../../../lib/protocol";
import { ErrorPanel } from "../../../components/ErrorPanel";

type Diagnostic = { state: string; path: string; message: string; slice?: string };
type Report = { blueprint: string; validCount: number; diagnostics: Diagnostic[] };
type Lane = { id: string; status: string; condition?: string; iterate?: string; itemCount?: number; produces?: string[];
  steps?: { id: string; kind: string; status: string; outcome?: string; reason?: string; stepId?: number }[] };
type LoopView = { id: string; blueprint: string; version: number; goal: string; root: string; status: string;
  position: { lane: string; item?: string; step?: string }; questionId?: number; gateAnswers?: string[]; lastReason?: string; pinnedContent?: string; lanes: Lane[] };
type Line = { seq: number; kind: string; text: string };

export function BlueprintPage() {
  const [names, setNames] = useState<string[]>([]);
  const [name, setName] = useState("two-step");
  const [path, setPath] = useState("");
  const [content, setContent] = useState("");
  const [report, setReport] = useState<Report | null>(null);
  const [goal, setGoal] = useState("Survey the Go packages and write a short summary.");
  const [root, setRoot] = useState("");
  const [route, setRoute] = useState("");
  const [loopId, setLoopId] = useState("");
  const [loop, setLoop] = useState<LoopView | null>(null);
  const [answer, setAnswer] = useState("approve");
  const [events, setEvents] = useState<Line[]>([]);
  const [error, setError] = useState<RpcError | null>(null);
  const [busy, setBusy] = useState(false);
  const [listening, setListening] = useState(false);
  const watched = useRef("");
  const seen = useRef(new Set<number>());

  const inspect = useCallback(async (id: string) => {
    if (!id.trim()) return;
    try {
      const view = await rpc<LoopView>("loop.show", { id, includeBlueprint: true });
      setLoop(view);
      setLoopId(id);
      watched.current = id;
      if (view.gateAnswers?.length) setAnswer(view.gateAnswers[0] ?? "");
    } catch (cause) { setError(toRpcError(cause)); }
  }, []);

  useEffect(() => {
    let closed = false;
    let unlisten: (() => void) | undefined;
    let subscriptionId = "";
    void (async () => {
      try {
        const listed = await rpc<{ names: string[] }>("blueprint.list", {});
        if (!closed) setNames(listed.names);
        unlisten = await onEvent((raw) => {
          const event = raw as AeonEvent;
          if (!event.kind.startsWith("loop.") || seen.current.has(event.seq)) return;
          seen.current.add(event.seq);
          const payload = (event.payload ?? {}) as Record<string, unknown>;
          const id = typeof payload.loopId === "string" ? payload.loopId : "";
          setEvents((current) => [...current, { seq: event.seq, kind: event.kind,
            text: [id, typeof payload.outcome === "string" ? payload.outcome : "", typeof payload.reason === "string" ? payload.reason : ""].filter(Boolean).join(" · ") }].slice(-50));
          if (id === watched.current) void inspect(id);
        });
        if (closed) { unlisten(); return; }
        const sub = await rpc<SubscribeResult>("subscribe", { after: 0, filter: { kinds: ["loop.*"] } });
        subscriptionId = sub.subscriptionId;
        if (!closed) setListening(true);
      } catch (cause) { if (!closed) setError(listenerUnavailable(cause)); }
    })();
    return () => { closed = true; unlisten?.(); if (subscriptionId) void rpc("unsubscribe", { subscriptionId }); };
  }, [inspect]);

  useEffect(() => {
    let cancelled = false;
    void rpc<{ path: string; content: string }>("blueprint.show", { name }).then((value) => {
      if (!cancelled) { setPath(value.path); setContent(value.content); setReport(null); }
    }).catch((cause) => { if (!cancelled) setError(toRpcError(cause)); });
    return () => { cancelled = true; };
  }, [name]);

  async function run(action: () => Promise<void>) {
    setBusy(true); setError(null);
    try { await action(); } catch (cause) { setError(toRpcError(cause)); } finally { setBusy(false); }
  }
  const validate = () => run(async () => {
    setReport(await rpc<Report>("blueprint.validate", { name, content, mode: "standard" }));
  });
  const save = () => run(async () => {
    // S48 T6: save answers the ROW (revision, provenance, report) — the
    // file path is gone; identical content says unchanged and wrote no
    // revision.
    const saved = await rpc<{ changed: boolean; revision: number; origin: string; report: Report }>("blueprint.save", { name, content, mode: "standard" });
    setPath(saved.changed ? `row ${name} · revision ${saved.revision} (${saved.origin})` : `row ${name} · unchanged at revision ${saved.revision} (${saved.origin})`);
    setReport(saved.report);
  });
  const start = () => run(async () => {
    const started = await rpc<{ id: string }>("loop.start", { blueprint: name, goal, workspaceRoot: root, route, mode: "standard" });
    watched.current = started.id; setLoopId(started.id); await inspect(started.id);
  });
  const sendAnswer = () => run(async () => {
    await rpc("loop.answer", { id: loopId, answer }); await inspect(loopId);
  });

  const diagnostics = report?.diagnostics ?? [];
  const invalid = diagnostics.filter((d) => d.state === "invalid").length;
  const unverifiable = diagnostics.filter((d) => d.state === "unverifiable").length;
  return <div className="space-y-4" data-testid="blueprint-page">
    <Panel tone="slate">
      <div className="flex items-center justify-between gap-3"><div><p className="text-xs uppercase tracking-widest text-slate-400">S8 · blueprints</p>
        <h2 className="mt-1 text-lg font-semibold">Blueprint test bench</h2></div>
        <Pill tone={listening ? "green" : "amber"} size="sm">{listening ? "watching loops" : "event stream unavailable"}</Pill></div>
      <p className="mt-3 text-sm text-slate-400">Validate and edit the source blueprint, then start a loop. Running loops keep the YAML pinned at their start.</p>
    </Panel>
    {error && <ErrorPanel error={error} />}
    <Panel><h3 className="font-medium">Source blueprint</h3>
      <div className="mt-3 flex flex-wrap items-end gap-3"><label className="text-sm">Blueprint<br/><select data-testid="blueprint-name" className="mt-1 rounded bg-slate-800 p-2" value={name} onChange={(e) => setName(e.target.value)}>
        {(names.length ? names : [name]).map((item) => <option key={item} value={item}>{item}</option>)}</select></label>
        <span className="break-all text-xs text-slate-400">{path}</span></div>
      <textarea data-testid="blueprint-yaml" aria-label="Blueprint YAML" spellCheck={false} className="mt-3 h-72 w-full rounded border border-slate-700 bg-slate-950 p-3 font-mono text-xs" value={content} onChange={(e) => setContent(e.target.value)} />
      <div className="mt-3 flex gap-2"><Button size="sm" disabled={busy} onClick={() => void validate()}>Validate</Button><Button size="sm" disabled={busy || !content} onClick={() => void save()}>Save source</Button></div>
      {report && <div className="mt-3 text-sm" data-testid="blueprint-report"><strong>{report.validCount} valid · {invalid} invalid · {unverifiable} unverifiable</strong>
        {diagnostics.length > 0 && <ul className="mt-2 space-y-1">{diagnostics.map((d, index) => <li key={index}><code>{d.path}</code> · {d.state} · {d.message}{d.slice ? ` (${d.slice})` : ""}</li>)}</ul>}</div>}
    </Panel>
    <Panel><h3 className="font-medium">Start and inspect a loop</h3>
      <div className="mt-3 grid gap-3 sm:grid-cols-2"><FormField label="Goal"><Input data-testid="blueprint-goal" value={goal} onChange={(e) => setGoal(e.target.value)} /></FormField>
        <FormField label="Workspace root"><Input data-testid="blueprint-root" value={root} onChange={(e) => setRoot(e.target.value)} placeholder="/absolute/path/to/workspace" /></FormField>
        <FormField label="Route (optional if one is configured)"><Input value={route} onChange={(e) => setRoute(e.target.value)} /></FormField>
        <FormField label="Loop ID"><Input data-testid="blueprint-loop-id" value={loopId} onChange={(e) => setLoopId(e.target.value)} /></FormField></div>
      <div className="mt-3 flex gap-2"><Button size="sm" disabled={busy || !goal || !root} onClick={() => void start()}>Start loop</Button>
        <Button size="sm" disabled={busy || !loopId} onClick={() => void inspect(loopId)}>Refresh ladder</Button></div>
      {loop && <div className="mt-4" data-testid="blueprint-ladder"><p><strong>{loop.blueprint}@{loop.version}</strong> · {loop.status} · {loop.id}</p>
        {loop.lastReason && <Alert tone="amber" title="Loop stopped" description={loop.lastReason} />}
        <ol className="mt-3 space-y-2">{loop.lanes.map((lane) => <li key={lane.id} className="rounded border border-slate-700 p-3 text-sm"><strong>{lane.id}</strong> · {lane.status}
          {lane.condition && <span> · when {lane.condition}</span>}{lane.iterate && <span> · {lane.iterate} ({lane.itemCount ?? 0})</span>}
          {lane.steps?.map((step) => <div key={step.id} className="ml-3 text-slate-400">{step.id} · {step.kind} · {step.status}{step.outcome ? ` · ${step.outcome}` : ""}{step.reason ? ` · ${step.reason}` : ""}</div>)}</li>)}</ol>
        {loop.status === "waiting" && loop.questionId && (loop.gateAnswers?.length ?? 0) > 0 && <div className="mt-3 flex items-end gap-2"><label className="text-sm">Gate answer<br/><select data-testid="blueprint-answer" className="mt-1 rounded bg-slate-800 p-2" value={answer} onChange={(e) => setAnswer(e.target.value)}>
          {loop.gateAnswers?.map((choice) => <option key={choice}>{choice}</option>)}</select></label><Button size="sm" disabled={busy} onClick={() => void sendAnswer()}>Answer gate</Button></div>}
        <details className="mt-3"><summary>Pinned YAML</summary><pre className="mt-2 overflow-auto text-xs" data-testid="pinned-yaml">{loop.pinnedContent}</pre></details>
      </div>}
    </Panel>
    <Panel><h3 className="font-medium">Loop events</h3>{events.length ? <ol data-testid="blueprint-events" className="mt-2 text-sm">{events.map((line) => <li key={line.seq}>{line.kind} · {line.text}</li>)}</ol> : <p className="mt-2 text-sm text-slate-400">No loop events yet.</p>}</Panel>
  </div>;
}
