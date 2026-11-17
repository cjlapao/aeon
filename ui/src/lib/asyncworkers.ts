// The async-worker ledger, in the app (S64).
//
// # What this file is
//
// Types and calls for the daemon's `asyncworkers.list · get · cancel` —
// the ledger the daemon keeps of its background work, and the reason a
// ten-minute code-graph build no longer holds anything open. There is
// deliberately **no start wrapper**: starting belongs to the owning
// service's own door (`graph.build` starts the graph.build worker),
// because starting means naming a Go function only the daemon registered
// (see `internal/protocol/asyncworkers.go` for the full argument, mirrored
// here so the wire and the screen agree about the closed door).
//
// # Rows are truth, events are paint
//
// `useAsyncWorkers` reads the ledger whenever the screen has a daemon
// connection — that is the slice's whole promise at the UI level: open
// the app mid-build and the page asks "is anything of this type running
// for this root?" and the daemon answers from the database, so a reopen
// DURING work knows to come back. Events (`asyncworker.*`) only repaint;
// a screen that raced an event re-reads. A row arriving via neither path
// is impossible (the daemon writes the row before it publishes), which is
// why the read is the source and the stream is decoration.

import { useCallback, useEffect, useState } from "react";
import { onEvent, rpc, toRpcError, type RpcError } from "./bridge";
import type { AeonEvent } from "./protocol";

/** The ledger's closed state vocabulary — the migration's CHECK, mirrored
 * (a seventh state is a schema change, never a string here). */
export const WORKER_STATES = [
  "queued",
  "running",
  "finished",
  "failed",
  "cancelled",
  "interrupted",
] as const;
export type WorkerState = (typeof WORKER_STATES)[number];

/** One `async_worker` row, as `ViewOf` marshals it (camelCase, Go's
 * `json.RawMessage` result arriving as its stored JSON). */
export interface WorkerView {
  workerId: string;
  workerType: string;
  componentRoot?: string;
  state: WorkerState;
  cancelRequested?: boolean;
  metadata?: Record<string, string>;
  /** Present only when the runner reported one — absent is the honest
   * unknown, NOT a 0% (three-state honesty, P31). */
  progress?: number;
  progressNote?: string;
  /** The runner's own result JSON, verbatim (a graph.build carries a
   * BuildReport here); null until the worker finishes successfully. */
  result?: unknown;
  error?: string;
  queuedAt: string;
  startedAt?: string;
  finishedAt?: string;
}

export interface WorkerListResult {
  workers: WorkerView[];
  types: string[];
}

export interface WorkerFilter {
  workerType?: string;
  componentRoot?: string;
  states?: readonly string[];
  limit?: number;
}

/** `asyncworkers.list` — the page-mount question's door. */
export function listWorkers(filter: WorkerFilter = {}): Promise<WorkerListResult> {
  const params: Record<string, unknown> = {};
  if (filter.workerType) params.workerType = filter.workerType;
  if (filter.componentRoot) params.componentRoot = filter.componentRoot;
  if (filter.states && filter.states.length) params.states = [...filter.states];
  if (filter.limit) params.limit = filter.limit;
  return rpc<WorkerListResult>("asyncworkers.list", params);
}

/** One worker by handle — what a follow-loop reads (the CLI does exactly
 * this once a second against the same door). */
export function getWorker(workerId: string): Promise<WorkerView> {
  return rpc<WorkerView>("asyncworkers.get", { workerId });
}

/** Ask the ledger to stop a worker. The answer is the row's CURRENT truth
 * — queued rows come back `cancelled`, running ones `running` with
 * `cancelRequested` until their goroutine lands the ending. */
export function cancelWorker(workerId: string): Promise<WorkerView> {
  return rpc<WorkerView>("asyncworkers.cancel", { workerId });
}

/** True while a row can still move. */
export function isLiveState(state: WorkerState): boolean {
  return state === "queued" || state === "running";
}

/**
 * The ledger for one screen: the rows read whenever `connected` holds
 * (and whenever the filter changes), repainted by `asyncworker.*` events
 * for as long as the connection lives.
 *
 * `connected` is the screen's daemon-connection fact — the bench passes
 * its session state, a product screen would pass `live.state ===
 * "current"`. A missed event converges on the next read; rows are truth,
 * events are paint. `reload` is the explicit "check again" affordance,
 * NOT an optimistic follow-up to a cancel (the event lands that truth).
 */
export function useAsyncWorkers(connected: boolean, filter: WorkerFilter = {}) {
  const [workers, setWorkers] = useState<WorkerView[] | null>(null);
  const [types, setTypes] = useState<string[]>([]);
  const [error, setError] = useState<RpcError | null>(null);

  const typeKey = filter.workerType ?? "";
  const rootKey = filter.componentRoot ?? "";
  const statesKey = (filter.states ?? []).join(",");

  const read = useCallback(async () => {
    try {
      const res = await listWorkers({
        workerType: typeKey || undefined,
        componentRoot: rootKey || undefined,
        states: statesKey ? statesKey.split(",") : undefined,
      });
      // Go marshals a nil slice as null; a page must not crash on an empty
      // ledger (the graph page's roots lesson, carried).
      setWorkers(res.workers ?? []);
      setTypes(res.types ?? []);
      setError(null);
    } catch (e) {
      setError(toRpcError(e));
    }
  }, [typeKey, rootKey, statesKey]);

  useEffect(() => {
    if (!connected) return;
    void read();
  }, [connected, read]);

  // Events repaint: the payload IS the View (the daemon publishes the
  // post-write row), so a merge is a replace-by-id — no re-read per
  // event, and a racing re-read converges to the same shape anyway.
  useEffect(() => {
    if (!connected) return;
    let closed = false;
    let stop: (() => void) | undefined;
    void onEvent((raw) => {
      if (closed) return;
      const event = raw as AeonEvent;
      if (!event.kind.startsWith("asyncworker.")) return;
      const view = event.payload as WorkerView | undefined;
      if (!view || typeof view.workerId !== "string") return;
      if (typeKey && view.workerType !== typeKey) return;
      if (rootKey && (view.componentRoot ?? "") !== rootKey) return;
      setWorkers((prev) => {
        const list = prev ?? [];
        const next = list.filter((w) => w.workerId !== view.workerId);
        next.push(view);
        return next;
      });
    }).then((un) => {
      if (closed) un();
      else stop = un;
    });
    return () => {
      closed = true;
      stop?.();
    };
  }, [connected, typeKey, rootKey]);

  return { workers, types, error, reload: read } as const;
}
