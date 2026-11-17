import { useCallback, useEffect, useRef, useState } from "react";
import { rpcTyped, toRpcError } from "../../lib/bridge";
import type { LiveState } from "../../lib/live";
import { agentChangesOf, type AgentChangeRow } from "./agentsModel";

// agentsLedger.ts — the ledger's data: the daemon's pages APPENDED, never rebuilt here.
//
// `agent.changes` answers newest first, one window at a time. This hook asks for page 1,
// then 2, then 3 and concatenates what comes back; the order, the count and the slug
// filter are the daemon's (a slug is sent for one agent's page, and left out for the
// whole ledger — the absence IS the second view). Nothing here sorts, filters or counts.

/** One chunk of the ledger. */
export const LEDGER_CHUNK = 50;

export interface AgentLedgerState {
  rows: AgentChangeRow[];
  total: number;
  hasMore: boolean;
  loading: boolean;
  loadingMore: boolean;
  error: string | undefined;
  /** Append the next chunk; resolves once it is in `rows`. */
  loadMore: () => Promise<void>;
  reload: () => void;
  /** Changes when the rows are replaced rather than appended to. */
  resetKey: string;
}

/** `slug` null asks for the whole ledger. */
export function useAgentLedger(live: LiveState, slug: string | null): AgentLedgerState {
  const [rows, setRows] = useState<AgentChangeRow[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(0);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string | undefined>();
  const [epoch, setEpoch] = useState(0);
  const ticket = useRef(0);
  const current = live.state === "current";
  const resetKey = `${slug ?? ""}|${epoch}`;

  const ask = useCallback(
    (n: number) => rpcTyped("agent.changes", { page: n, pageSize: LEDGER_CHUNK, ...(slug !== null && slug !== "" ? { slug } : {}) }),
    [slug],
  );

  useEffect(() => {
    if (!current) return;
    const mine = ++ticket.current;
    setLoading(true);
    setLoadingMore(false);
    setError(undefined);
    ask(1)
      .then((answer) => {
        if (mine !== ticket.current) return;
        const read = agentChangesOf(answer);
        setRows(read.changes);
        setTotal(read.total);
        setPage(1);
      })
      .catch((cause) => {
        if (mine === ticket.current) setError(toRpcError(cause).message);
      })
      .finally(() => {
        if (mine === ticket.current) setLoading(false);
      });
  }, [current, ask, epoch]);

  const loadMore = useCallback(() => {
    if (!current || loading || loadingMore || rows.length >= total) return Promise.resolve();
    const mine = ticket.current;
    setLoadingMore(true);
    setError(undefined);
    return ask(page + 1)
      .then((answer) => {
        if (mine !== ticket.current) return;
        const read = agentChangesOf(answer);
        // A page that adds nothing ends the ledger, whatever `total` said.
        setRows((before) => (read.changes.length === 0 ? before : [...before, ...read.changes]));
        setTotal(read.changes.length === 0 ? rows.length : read.total);
        setPage(page + 1);
      })
      .catch((cause) => {
        if (mine === ticket.current) setError(toRpcError(cause).message);
      })
      .finally(() => {
        if (mine === ticket.current) setLoadingMore(false);
      });
  }, [current, loading, loadingMore, rows.length, total, page, ask]);

  const reload = useCallback(() => setEpoch((n) => n + 1), []);
  return { rows, total, hasMore: rows.length < total, loading, loadingMore, error, loadMore, reload, resetKey };
}

/** Load every remaining chunk, one at a time. The caller decides to (a short list wants all of it). */
export function useLoadAll(ledger: AgentLedgerState): void {
  const { hasMore, loading, loadingMore, error, loadMore } = ledger;
  useEffect(() => {
    if (hasMore && !loading && !loadingMore && error === undefined) void loadMore();
  }, [hasMore, loading, loadingMore, error, loadMore]);
}
