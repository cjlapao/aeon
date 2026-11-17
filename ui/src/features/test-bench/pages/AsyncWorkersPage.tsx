import { useMemo, useState } from "react";
import { Button, FormField, Input, Panel, Pill, Select } from "../../../components/kit";
import { ErrorPanel } from "../../../components/ErrorPanel";
import { toRpcError, type RpcError } from "../../../lib/bridge";
import {
  WORKER_STATES,
  cancelWorker,
  isLiveState,
  useAsyncWorkers,
  type WorkerState,
  type WorkerView,
} from "../../../lib/asyncworkers";
import { registerBenchPage } from "../lib/benchRegistry";

/**
 * The async-worker ledger (S64) — the daemon's background work, visible
 * where it is consumed.
 *
 * # Why this page exists at all
 *
 * The slice's promise in one screen: a ten-minute job (the code-graph
 * build is the first) runs in the daemon, the ledger row outlives every
 * connection, and **reopening the app during work shows the work** — the
 * mount read answers "is anything of this type running for this root?"
 * from the database, so there is nothing to lose by closing the window.
 * Events repaint rows while you watch; the rows are truth either way.
 *
 * # What this page is NOT
 *
 * It cannot START anything. The ledger holds registered Go functions,
 * and starting means naming one with arguments only the owning service
 * can supply — so `graph.build` starts graph.build workers (the Graph
 * page's button), and this page reads, follows, and cancels. A start
 * form here would be a door the daemon must refuse anyway; it is absent
 * by design, not by omission (`internal/protocol/asyncworkers.go` keeps
 * the matching test).
 *
 * The six states render as six distinct visuals, never two-and-a-guess
 * (P31): `finished` green, `failed` red with the runner's sentence,
 * `cancelled` slate ("who asked" — cancelRequested), `interrupted` amber
 * with its own wording — the daemon died, the work is unknown, retry is
 * the operator's deliberate act — and the two live states (queued,
 * running) amber with progress as *unknown unless reported*.
 */

const STATE_TONES: Record<WorkerState, "green" | "amber" | "slate" | "red"> = {
  queued: "amber",
  running: "amber",
  finished: "green",
  failed: "red",
  cancelled: "slate",
  interrupted: "amber",
};

const STATE_WORDS: Record<WorkerState, string> = {
  queued: "waiting for a slot",
  running: "working",
  finished: "done",
  failed: "the work refused",
  cancelled: "you stopped it",
  interrupted: "the daemon stopped before it finished",
};

function age(when: string, nowMs: number): string {
  const t = Date.parse(when);
  if (Number.isNaN(t)) return when;
  const s = Math.max(0, Math.round((nowMs - t) / 1000));
  if (s < 60) return `${s}s ago`;
  if (s < 3600) return `${Math.round(s / 60)}m ago`;
  return `${Math.round(s / 3600)}h ago`;
}

/** The runner's result, printed only for what the runner actually shaped
 * — today one type, graph.build, whose result IS the BuildReport the
 * socket returned inline before S64. Unknown shapes print as JSON: the
 * ledger is generic, and inventing a second rendering per type would be
 * a second truth waiting to drift. */
function ResultBody({ worker }: { worker: WorkerView }) {
  if (worker.state !== "finished" || worker.result == null) {
    return <span className="muted">no result — {STATE_WORDS[worker.state].toLowerCase()}.</span>;
  }
  const r = worker.result as Record<string, unknown>;
  if (worker.workerType === "graph.build" && typeof r.files === "number") {
    return (
      <>
        {r.files} files ({r.filesSkipped} skipped) · {r.nodes} nodes · {r.edges} edges ·{" "}
        {r.unresolvedEdges} references unresolved
      </>
    );
  }
  return <code className="text-xs break-all">{JSON.stringify(worker.result)}</code>;
}

export function AsyncWorkersPage({ connected = true }: { connected?: boolean } = {}) {
  const [workerType, setWorkerType] = useState("");
  const [componentRoot, setComponentRoot] = useState("");
  const [state, setState] = useState<"" | WorkerState>("");
  const [expanded, setExpanded] = useState<string | null>(null);
  const [actionError, setActionError] = useState<RpcError | null>(null);

  const { workers, types, error: listError } = useAsyncWorkers(connected, {
    workerType: workerType || undefined,
    componentRoot: componentRoot.trim() || undefined,
    states: state ? [state] : undefined,
  });

  const rows = useMemo(() => workers ?? [], [workers]);
  const nowMs = Date.now();

  const cancel = async (workerId: string) => {
    try {
      setActionError(null);
      // The row itself updates by event (the daemon publishes the ending);
      // the promise's answer is the CURRENT row, kept for the queued case
      // where there is no goroutine to publish later.
      const view = await cancelWorker(workerId);
      if (!isLiveState(view.state)) return;
    } catch (e) {
      setActionError(toRpcError(e));
    }
  };

  return (
    <div>
      <Panel title="Async workers — the daemon's background ledger (S64)">
        <div className="muted">
          Long-running work runs here: one row per job, kept in the database, surviving restarts.
          Close the app during a job and reopen — the mount read answers from the ledger, so the
          work is visible again without ever having been watched. This page reads, follows and
          cancels; starting belongs to the owning service (a graph.build worker starts through
          the Graph page&apos;s Build button), because a worker is a registered Go function, not a payload.
        </div>
      </Panel>

      <Panel title="Filters — the page-mount question, narrowed">
        <div className="row">
          <FormField label="worker type" className="grow">
            <Select value={workerType} onChange={(e) => setWorkerType(e.target.value)}>
              <option value="">all types</option>
              {types.map((t) => (
                <option key={t} value={t}>{t}</option>
              ))}
            </Select>
          </FormField>
          <FormField label="component root (exact)" className="grow">
            <Input value={componentRoot} onChange={(e) => setComponentRoot(e.target.value)} placeholder="/home/dev/shop" data-testid="aw-root" />
          </FormField>
          <FormField label="state" className="grow">
            <Select value={state} onChange={(e) => setState(e.target.value as "" | WorkerState)}>
              <option value="">all states</option>
              {WORKER_STATES.map((s) => (
                <option key={s} value={s}>{s}</option>
              ))}
            </Select>
          </FormField>
        </div>
        {(listError || actionError) && <ErrorPanel error={(listError ?? actionError) as RpcError} />}
        {rows.length === 0 && workers != null && (
          <div className="muted" data-testid="aw-empty">
            nothing in the ledger for this filter — which is the row&apos;s answer, not a failed
            read: no worker of this type and root is running, ran, or was refused.
          </div>
        )}
        <ul className="list">
          {rows.map((w) => (
            <li key={w.workerId} className="row" data-testid={`aw-row-${w.state}`}>
              <Pill tone={STATE_TONES[w.state]} size="sm">{w.state}</Pill>
              <code className="text-xs">{w.workerType}</code>
              <span className="grow">
                {w.componentRoot && <span className="opacity-80">{w.componentRoot}</span>}
                {isLiveState(w.state) && (
                  <span>
                    {" "}
                    · {STATE_WORDS[w.state]}
                    {w.progress != null ? ` · ${Math.round(w.progress * 100)}%` : " · progress unknown"}
                    {w.progressNote ? ` — ${w.progressNote}` : ""}
                  </span>
                )}
                {!isLiveState(w.state) && w.error && (
                  <span> · {w.error}</span>
                )}
                {w.state === "cancelled" && w.cancelRequested && (
                  <span className="muted"> · you asked, it stopped</span>
                )}
                {w.state === "interrupted" && (
                  <span className="muted"> · nothing restarted it on purpose — retry from the owning screen</span>
                )}
              </span>
              <span className="muted text-xs">{age(w.finishedAt ?? w.startedAt ?? w.queuedAt, nowMs)}</span>
              {isLiveState(w.state) && (
                w.cancelRequested ? (
                  <Pill tone="slate" size="sm">cancel requested</Pill>
                ) : (
                  <Button size="sm" data-testid={`aw-cancel-${w.workerId}`} onClick={() => void cancel(w.workerId)}>
                    cancel
                  </Button>
                )
              )}
              <Button size="sm" data-testid={`aw-show-${w.workerId}`} onClick={() => setExpanded(expanded === w.workerId ? null : w.workerId)}>
                {expanded === w.workerId ? "hide" : "result"}
              </Button>
            </li>
          ))}
        </ul>
      </Panel>

      {expanded && (
        <Panel title={`worker ${expanded}`}>
          {(() => {
            const w = rows.find((r) => r.workerId === expanded);
            if (!w) return <div className="muted">the row is no longer in this view — the filter moved on.</div>;
            return (
              <div className="text-sm">
                <div><ResultBody worker={w} /></div>
                {w.metadata && Object.keys(w.metadata).length > 0 && (
                  <div className="mt-2 muted text-xs">
                    {Object.entries(w.metadata).map(([k, v]) => (
                      <span key={k} className="mr-3">{k}={v}</span>
                    ))}
                  </div>
                )}
                <div className="mt-2 muted text-xs">
                  queued {w.queuedAt}{w.startedAt ? ` · started ${w.startedAt}` : ""}{w.finishedAt ? ` · finished ${w.finishedAt}` : ""}
                </div>
              </div>
            );
          })()}
        </Panel>
      )}

      <Panel title="What this page does not do">
        <div className="muted">
          It does not start workers (no start door exists on the ledger —
          starting is the owning service&apos;s method), it does not retry
          interrupted work (a crashed ten-minute parse re-fired invisibly is
          a loop nobody agreed to; the row invites a deliberate retry
          instead), and it never invents progress a runner did not report —
          absent means unknown, not zero.
        </div>
      </Panel>
    </div>
  );
}

registerBenchPage({
  id: "async-workers",
  label: "Async Workers",
  path: "/bench/async-workers",
  order: 4400,
  needsConnection: true,
  render: (ctx) => <AsyncWorkersPage connected={ctx.connected} />,
});
