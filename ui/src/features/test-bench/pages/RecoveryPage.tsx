import { useCallback, useEffect, useRef, useState } from "react";
import { Alert, Button, FormField, Input, Panel, Pill } from "../../../components/kit";
import { listenerUnavailable, onEvent, rpc, toRpcError, type RpcError } from "../../../lib/bridge";
import type { AeonEvent, StepView, SubscribeResult } from "../../../lib/protocol";
import { METHOD_STEP_SHOW } from "../../../lib/protocol";
import { ErrorPanel } from "../../../components/ErrorPanel";

type RecoveryLine = { seq: number; kind: string; stepId?: number; text: string };

function eventLine(event: AeonEvent): RecoveryLine | null {
  if (!event.kind.startsWith("recovery.")) return null;
  const payload = (event.payload ?? {}) as Record<string, unknown>;
  const stepId = typeof payload.stepId === "number" ? payload.stepId : undefined;
  const progress = typeof payload.index === "number" && typeof payload.total === "number"
    ? `${payload.index} of ${payload.total} · ` : "";
  const failure = payload.failure && typeof payload.failure === "object"
    ? payload.failure as Record<string, unknown> : {};
  const detail = [payload.case, payload.outcome, payload.reason, failure.kind, failure.tool, failure.detail]
    .filter((value) => typeof value === "string").join(" · ");
  return { seq: event.seq, kind: event.kind, stepId,
    text: `${progress}${stepId ? `step ${stepId} · ` : ""}${detail || event.kind}` };
}

export function RecoveryPage() {
  const [stepId, setStepId] = useState("");
  const [step, setStep] = useState<StepView | null>(null);
  const [events, setEvents] = useState<RecoveryLine[]>([]);
  const [error, setError] = useState<RpcError | null>(null);
  const [listening, setListening] = useState(false);
  const [busy, setBusy] = useState(false);
  const watchedId = useRef(0);
  const seen = useRef(new Set<number>());

  const inspect = useCallback(async (id: number) => {
    if (!Number.isSafeInteger(id) || id <= 0) return;
    watchedId.current = id;
    setBusy(true);
    setError(null);
    try {
      const saved = await rpc<StepView>(METHOD_STEP_SHOW, { id });
      setStep(saved);
      setStepId(String(id));
    } catch (cause) {
      setError(toRpcError(cause));
    } finally {
      setBusy(false);
    }
  }, []);

  useEffect(() => {
    let disposed = false;
    let unlisten: (() => void) | undefined;
    let subscriptionId = "";
    void (async () => {
      try {
        unlisten = await onEvent((raw) => {
          const line = eventLine(raw as AeonEvent);
          if (!line || (line.seq && seen.current.has(line.seq))) return;
          if (line.seq) seen.current.add(line.seq);
          setEvents((current) => [...current, line].slice(-50));
          if (line.stepId === watchedId.current && line.kind === "recovery.resolved") void inspect(line.stepId);
        });
        if (disposed) { unlisten(); return; }
        const sub = await rpc<SubscribeResult>("subscribe", { after: 0, filter: { kinds: ["recovery.*"] } });
        subscriptionId = sub.subscriptionId;
        if (!disposed) setListening(true);
      } catch (cause) {
        if (!disposed) setError(listenerUnavailable(cause));
      }
    })();
    return () => {
      disposed = true;
      unlisten?.();
      if (subscriptionId) void rpc("unsubscribe", { subscriptionId });
    };
  }, [inspect]);

  const receipts = step?.receipts ?? [];
  return (
    <div className="space-y-4" data-testid="recovery-page">
      <Panel tone="slate">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <p className="text-xs uppercase tracking-widest text-slate-400">S7 · crash recovery</p>
            <h2 className="mt-1 text-lg font-semibold">Recovery test bench</h2>
          </div>
          <Pill tone={listening ? "green" : "amber"} size="sm" data-testid="recovery-listening">
            {listening ? "watching recovery" : "event stream unavailable"}
          </Pill>
        </div>
        <p className="mt-3 text-sm text-slate-400">
          Restart the daemon after a crash, then inspect the step ID here. The stream replays retained recovery events and follows new ones.
        </p>
      </Panel>

      <Panel>
        <div className="flex flex-wrap items-end gap-3">
          <div className="min-w-40 flex-1">
            <FormField label="step ID">
              <Input data-testid="recovery-step-id" value={stepId} onChange={(event) => setStepId(event.target.value)} placeholder="e.g. 42" />
            </FormField>
          </div>
          <Button size="sm" data-testid="recovery-inspect" disabled={busy || !/^\d+$/.test(stepId)}
            onClick={() => void inspect(Number(stepId))}>{busy ? "loading…" : "inspect"}</Button>
        </div>
        {error && <div className="mt-3"><ErrorPanel error={error} /></div>}
        {step && <div className="mt-4 space-y-3" data-testid="recovery-step">
          <div className="flex flex-wrap items-center gap-2 text-sm">
            <strong>Step {step.id}</strong>
            <Pill tone={step.status === "done" && step.outcome === "failed" ? "red" : step.status === "running" ? "amber" : "green"} size="sm">{step.status}</Pill>
            {step.outcome && <span>outcome: <strong>{step.outcome}</strong></span>}
          </div>
          {step.recovery ? <div data-testid="recovery-record" className="rounded border border-slate-700 p-3 text-sm">
            <p><strong>Case {step.recovery.case}</strong>{step.recovery.reissued ? " · model call reissued" : ""}</p>
            {step.recovery.failure && <p data-testid="recovery-failure" className="mt-1 font-mono text-rose-300">
              {step.recovery.failure.kind}{step.recovery.failure.tool ? ` · ${step.recovery.failure.tool}` : ""}
              {step.recovery.failure.toolUseId ? ` · ${step.recovery.failure.toolUseId}` : ""}
            </p>}
            {step.recovery.reason && <p className="mt-1 whitespace-pre-wrap text-slate-300">{step.recovery.reason}</p>}
            <p className="mt-1 text-xs text-slate-500">recorded {step.recovery.updatedAt}</p>
          </div> : <p className="text-sm text-slate-400">No recovery decision has been saved for this step.</p>}
          <div>
            <h3 className="text-sm font-medium">Unsafe tool receipts · {receipts.length}</h3>
            {receipts.length ? <ul className="mt-2 space-y-2" data-testid="recovery-receipts">
              {receipts.map((receipt) => <li key={receipt.toolUseId} className="rounded border border-slate-700 p-3 text-xs">
                <div className="flex flex-wrap items-center gap-2"><strong>{receipt.tool}</strong><code>{receipt.toolUseId}</code>
                  <Pill tone={receipt.completedAt ? "green" : "amber"} size="sm">{receipt.completedAt ? "completed" : "outcome unknown"}</Pill></div>
                <p className="mt-2 break-all font-mono text-slate-400">digest {receipt.argDigest}</p>
                <p className="mt-1 text-slate-400">started {receipt.startedAt} · completed {receipt.completedAt ?? "—"} · result {receipt.resultCaptured ? "captured" : "absent"}</p>
              </li>)}
            </ul> : <p className="mt-1 text-sm text-slate-400">No unsafe tool receipt was written.</p>}
          </div>
        </div>}
      </Panel>

      <Panel>
        <h3 className="text-sm font-medium">Recovery stream</h3>
        {events.length ? <ol data-testid="recovery-events" className="mt-3 space-y-2 text-sm">
          {events.map((line, index) => <li key={`${line.seq}-${index}`} className="border-l-2 border-slate-600 pl-3">
            <span className="font-mono text-xs text-slate-400">{line.kind}</span><span className="ml-2">{line.text}</span>
          </li>)}
        </ol> : <p className="mt-2 text-sm text-slate-400">No recovery events retained yet. Inspect a saved step above after restart.</p>}
      </Panel>

      <Alert tone="slate" title="Dry run" description="Start the daemon with aeon serve --no-recover to see cases without changing running steps. Restart normally to apply recovery." />
    </div>
  );
}
