import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { Panel, Pill } from "../../components/kit";
import { onEvent, rpc, toRpcError } from "../../lib/bridge";
import { LivenessNotice, RecoveryNotice } from "../../lib/uncertain";
import type { LiveController, LiveState } from "../../lib/live";
import type { AeonEvent, Cost } from "../../lib/protocol";
import { costLine } from "../../lib/cost";
import { WorkspacesPanel } from "./WorkspacesPanel";

interface RecoveryEvent { seq: number; kind: string; stepId?: number; outcome?: string; case?: string;
  total?: number; recovered?: number; failed?: number;
  failure?: { kind?: string; tool?: string; detail?: string };
  priorCost?: Cost; reissueCost?: Cost }

const caseDescriptions: Record<string, { text: string; tone: "green" | "amber" | "red" }> = {
  A: { text: "saved tail completed", tone: "green" },
  B: { text: "model call reissued; earlier attempt may also be billed", tone: "amber" },
  C: { text: "tool receipt reconciliation", tone: "amber" },
};

export function SystemScreen({ controller, live }: { controller: LiveController; live: LiveState }) {
  const [recovery, setRecovery] = useState<RecoveryEvent[]>([]);
  const [error, setError] = useState("");
  useEffect(() => {
    if (live.state !== "current") return;
    let disposed = false, stop: (() => void) | undefined, subscriptionId = "";
    void (async () => {
      try {
        stop = await onEvent((raw) => { const event = raw as AeonEvent;
          if (!event.kind.startsWith("recovery.")) return;
          const payload = event.payload as Record<string, unknown>;
          setRecovery((old) => old.some((item) => item.seq === event.seq) ? old : [...old, {
            seq: event.seq, kind: event.kind, stepId: Number(payload.stepId) || undefined,
            outcome: String(payload.outcome || ""), case: String(payload.case || ""),
            total: typeof payload.total === "number" ? payload.total : undefined,
            recovered: typeof payload.recovered === "number" ? payload.recovered : undefined,
            failed: typeof payload.failed === "number" ? payload.failed : undefined,
            failure: payload.failure as RecoveryEvent["failure"],
            priorCost: payload.priorCost as Cost | undefined, reissueCost: payload.reissueCost as Cost | undefined,
          }].sort((a, b) => a.seq - b.seq).slice(-200));
        });
        const sub = await rpc<{ subscriptionId: string }>("subscribe", { after: 0, filter: { kinds: ["recovery.*"] } });
        subscriptionId = sub.subscriptionId; if (disposed) stop?.();
      } catch (cause) { if (!disposed) setError(toRpcError(cause).message); }
    })();
    return () => { disposed = true; stop?.(); if (subscriptionId) void rpc("unsubscribe", { subscriptionId }); };
  }, [live.state === "current" ? live.generation : -1]);
  const lastStart = recovery.reduce((last, item, index) => item.kind === "recovery.started" ? index : last, -1);
  const latestRecovery = lastStart >= 0 ? recovery.slice(lastStart) : recovery;
  const summary = [...latestRecovery].reverse().find((item) => item.kind === "recovery.finished");
  return <section className="space-y-5" data-testid="product-system"><div><p className="text-xs uppercase tracking-widest aeon-accent-text">Local daemon</p>
    <h1 className="mt-1 text-3xl font-semibold">System</h1></div>
    {error && <Panel tone="red"><p role="alert">{error}</p></Panel>}
    <Panel><h2 className="text-lg font-semibold">Connection</h2>
      <p className="mt-2">State: {live.state === "current" ? <LivenessNotice value={{ state: "current" }} /> :
        live.state === "stale" ? <LivenessNotice value={{ state: "stale", ageSeconds: controller.ageSeconds(), cause: live.cause, endpoint: live.endpoint }} /> :
        live.state === "disconnected" ? <LivenessNotice value={{ state: "disconnected", reason: live.reason, endpoint: live.endpoint }} /> : "connecting"}</p>
      <p className="mt-2 break-all text-sm">Socket: {live.endpoint}</p>
      <p className="text-sm">Daemon {live.state === "current" ? live.daemonVersion : live.state === "stale" ?
        `${live.daemonVersion || "version unknown"} (last confirmed)` : "version unknown"} · protocol {live.state === "current" ? live.protocolVersion :
        live.state === "stale" ? live.protocolVersion ?? "unknown" : "unknown"}</p></Panel>
    <WorkspacesPanel />
    <Panel><h2 className="text-lg font-semibold">Recovery report</h2>
      {summary && <p className="mt-2 text-sm">Last start: {summary.recovered ?? 0} recovered of {summary.total ?? "unknown"} · {summary.failed ?? 0} failed</p>}
      {summary?.total === 0 ? <p className="mt-2 text-sm text-slate-600">No steps needed recovery on the last daemon start.</p> :
        latestRecovery.length === 0 ? <p className="mt-2 text-sm text-slate-600">No retained recovery events on this daemon.</p> :
        <ol className="mt-3 space-y-2">{latestRecovery.map((item) => <li key={item.seq} className="rounded-lg border border-white/10 p-3 text-sm">
          <span className="mr-2 font-mono text-xs">#{item.seq}</span>{item.kind}{item.stepId && <> · <Link className="aeon-accent-text underline" to={`/steps/${item.stepId}`}>step {item.stepId}</Link></>}
          {item.case && <> · <Pill tone={item.case === "C" && item.outcome === "failed" ? "red" : caseDescriptions[item.case]?.tone ?? "slate"}>case {item.case}</Pill>
            <span className="ml-2">{caseDescriptions[item.case]?.text ?? "recovery case"}</span></>}
          {item.case === "B" && (item.priorCost || item.reissueCost) && <div className="mt-1">Before crash: {costLine(item.priorCost).text} · Reissued work: {costLine(item.reissueCost).text}</div>}
          {item.kind === "recovery.resolved" && <div className="mt-1"><RecoveryNotice outcome={item.outcome === "failed" ? "failed" : "recovered"}
            tool={item.failure?.tool} receiptState={item.failure?.kind} detail={item.failure?.detail} /></div>}
        </li>)}</ol>}
    </Panel>
  </section>;
}
