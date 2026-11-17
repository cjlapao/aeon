/**
 * systemRpc — the ONE file through which Settings → System speaks (S54 T6,
 * SP8/P34).
 *
 * Every method literal the surface uses stands here spelled exactly once,
 * so the guard (`platformRpc.guard.test.ts`) has one surface to scan
 * besides the components that call these wrappers, and the generated
 * contract settles whether a name is registered at all. Same discipline
 * as `memoryRpc.ts` (S52), `triageRpc.ts` (S51), `roadmapRpc.ts` (S50)
 * and `automationRpc.ts` (S49).
 *
 * The doors are the whole surface: the three S54 reads
 * (`platform.status`, `platform.changes`, `retrieval.reindexPlan`), the
 * operation the Reindex confirm launches (`retrieval.reindex`), the
 * graph family's three human doors (`graph.build`, `graph.reparse`,
 * `graph.delete`), and the four read-only lookups the explorer and the search
 * probe make (`graph.query`, `graph.explain`, `graph.path`, `search.query`) —
 * they change nothing. SP5's verdict rides this file's silence: no `settings.*`
 * door appears here, because the panel writes NO setting — an operation is
 * a daemon act, not a preference. The actor is never among the params:
 * these doors take the person from the connection's peer (P26), and the
 * ledger's row names the peer's uid — the CLI's `{cli,cli}` identity is
 * the offline door's, which this surface is not.
 *
 * The writes carry no confirmation field: the confirm IS the click (the
 * modal quotes the daemon's plan or the derived-row statement first, the
 * CLI's twin asks for `--yes`), and a refusal — including "already
 * running", which names its starter — comes back as the daemon's own
 * sentence, shown verbatim.
 */

import { rpcTyped } from "../../../lib/bridge";
import { useRpcQuery, type RpcQueryState } from "../../../lib/useRpcQuery";
import type { LiveController, LiveState } from "../../../lib/live";
import type { RpcMethods } from "../../../proto/contract";
import { CHANGES_LIMIT, changesOf, type PlatformChangesResult, type PlatformStatusResult } from "./systemModel";

// ── reads ───────────────────────────────────────────────────────────────────

/** The composed panel: three subsystem blocks, each with the daemon's own
 *  state word, loaded once per section. Unwired is a state here, never an
 *  empty panel — the daemon said so. */
export function usePlatformStatus(
  controller: LiveController,
  live: LiveState,
): RpcQueryState<PlatformStatusResult | undefined> {
  return useRpcQuery("platform.status", {}, controller, live);
}

/** The operation ledger (`platform.changes`), newest first as the daemon
 *  ordered it, capped by the constant the panel sends verbatim. */
export function usePlatformChanges(
  controller: LiveController,
  live: LiveState,
): RpcQueryState<PlatformChangesResult> {
  const query = useRpcQuery("platform.changes", { limit: CHANGES_LIMIT }, controller, live);
  return {
    data: query.data === undefined ? undefined : changesOf(query.data),
    loading: query.loading,
    error: query.error,
    refetch: query.refetch,
  };
}

/** The plan the Reindex confirm shows BEFORE anything is started (SP7's
 *  seatbelt as a click): the daemon's counts, estimate basis, and cost
 *  with its state kept attached. It is a read — asking never starts
 *  anything, and the modal is the only caller. */
export const fetchReindexPlan = (componentRoot: string, corpus: string) =>
  rpcTyped("retrieval.reindexPlan", { componentRoot, corpus });

// ── writes (the operations; each is preceded by the modal) ─────────────────

/** `retrieval.reindex` — the pass. The answer names what started; when a
 *  pass is already flying the daemon's answer/refusal names its starter
 *  (`startedBy`, T1's additive naming), and the panel prints those words,
 *  never a sentence of its own about who is running. */
export const startReindex = (componentRoot: string, corpus: string) =>
  rpcTyped("retrieval.reindex", { componentRoot, corpus });

/** `graph.build` — regenerate a root's derived graph (a ledger worker;
 *  the answer carries `started`/`workerId`, and a flying pass answers
 *  with its starter). */
export const startGraphBuild = (componentRoot: string) => rpcTyped("graph.build", { componentRoot });

/** `graph.reparse` — replace ONE file's rows; the modal names the path
 *  and says plainly that derived rows are replaced, source files are not
 *  touched. */
export const reparseGraphFile = (componentRoot: string, path: string) =>
  rpcTyped("graph.reparse", { componentRoot, path });

/** `graph.delete` — remove a root's ENTIRE derived graph; the answer
 *  counts the rows it removed. Rebuilding later regenerates them. */
export const deleteGraphRoot = (componentRoot: string) => rpcTyped("graph.delete", { componentRoot });

// ── read-only lookups (the explorer and the search probe) ──────────────────

/** `graph.query` — symbols of a kind whose name starts with a prefix: the explorer's suggestions. */
export const queryGraphSymbols = (componentRoot: string, kind: string, prefix: string, limit: number) =>
  rpcTyped("graph.query", { componentRoot, kind, prefix, limit });

/** `graph.explain` — one symbol with its callers and callees, or the daemon's own other state
 *  (ambiguous, not found, not built). */
export const explainGraphSymbol = (componentRoot: string, symbol: string) =>
  rpcTyped("graph.explain", { componentRoot, symbol });

/** `graph.path` — how one symbol reaches another. */
export const pathBetweenSymbols = (componentRoot: string, from: string, to: string) =>
  rpcTyped("graph.path", { componentRoot, from, to });

/** `search.query` — what an agent would get back for a term: file ranges and scores, never text
 *  the daemon did not send. Read-only. */
export const searchCorpus = (componentRoot: string, corpus: string, query: string) =>
  rpcTyped("search.query", { componentRoot, corpus, query });

export type { RpcMethods };

/** `asyncworkers.get` — one background pass's row: its state, its error if it failed, its outcome when it finished. */
export const fetchWorker = (workerId: string): Promise<{ state?: string; error?: string; result?: { files?: number; chunksWritten?: number; embedded?: number } }> =>
  (rpcTyped("asyncworkers.get", { workerId }) as Promise<unknown>) as Promise<{ state?: string; error?: string; result?: { files?: number; chunksWritten?: number; embedded?: number } }>;

/** `routes.list`, read only to tell whether a model that can embed is set up before a reindex is offered. */
export const fetchEmbeddingRoutes = async (): Promise<string[]> => {
  const res = (await (rpcTyped("routes.list", {} as never) as Promise<unknown>)) as { routes?: { id: string; capabilities?: string[] | null }[] };
  return (res.routes ?? []).filter((r) => (r.capabilities ?? []).includes("embedding")).map((r) => r.id);
};
