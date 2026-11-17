import { useCallback, useEffect, useRef, useState } from "react";
import { rpcTyped, toRpcError } from "../../lib/bridge";
import type { LiveState } from "../../lib/live";
import { AGENT_DEFAULT_DIR, AGENT_DEFAULT_SORT, agentRowsOf, agentRowsParams, type AgentDir, type AgentRow, type AgentSort } from "./agentsModel";

// agentsRows.ts — the list's data: the daemon's pages APPENDED as the person scrolls.
//
// The daemon still decides everything about a window (the order, the filter, the
// count): this hook only asks for page 1, then 2, then 3 and concatenates what comes
// back. Nothing here sorts, filters or counts. A new search or order replaces the rows
// from page 1 (`resetKey` changes with it); an answer to a window that is no longer the
// one asked for is dropped, so a slow reply never paints over a newer one.

/** One chunk: big enough that a 5,000-row store is a few requests, small enough to answer at once. */
export const AGENT_CHUNK = 50;

export interface AgentRowsState {
  rows: AgentRow[];
  total: number;
  /** More rows exist after the ones loaded. */
  hasMore: boolean;
  /** The first chunk of this window is out (rows are replaced when it lands). */
  loading: boolean;
  /** A later chunk is out. */
  loadingMore: boolean;
  /** The daemon's refusal for the window or a chunk, verbatim. */
  error: string | undefined;
  /** Append the next chunk. Resolves once it is in `rows` (the kit's table waits on it). */
  loadMore: () => Promise<void>;
  /** Ask again from the top — after a write. */
  reload: () => void;
  /** Changes whenever the rows are replaced rather than appended to. */
  resetKey: string;
}

export function useAgentRows(live: LiveState, q: string, sort: AgentSort = AGENT_DEFAULT_SORT, dir: AgentDir = AGENT_DEFAULT_DIR): AgentRowsState {
  const [rows, setRows] = useState<AgentRow[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(0);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string | undefined>();
  const [epoch, setEpoch] = useState(0);
  // The window the rows belong to; an answer for any other is dropped.
  const ticket = useRef(0);
  const current = live.state === "current";
  const resetKey = `${q}|${sort}|${dir}|${epoch}`;

  useEffect(() => {
    if (!current) return;
    const mine = ++ticket.current;
    setLoading(true);
    setLoadingMore(false);
    setError(undefined);
    rpcTyped("agent.rows", agentRowsParams({ page: 1, pageSize: AGENT_CHUNK, sort, dir, q }))
      .then((answer) => {
        if (mine !== ticket.current) return;
        const read = agentRowsOf(answer);
        setRows(read.rows);
        setTotal(read.total);
        setPage(1);
      })
      .catch((cause) => {
        if (mine === ticket.current) setError(toRpcError(cause).message);
      })
      .finally(() => {
        if (mine === ticket.current) setLoading(false);
      });
  }, [current, q, sort, dir, epoch]);

  const loadMore = useCallback(() => {
    if (!current || loading || loadingMore || rows.length >= total) return Promise.resolve();
    const mine = ticket.current;
    setLoadingMore(true);
    setError(undefined);
    return rpcTyped("agent.rows", agentRowsParams({ page: page + 1, pageSize: AGENT_CHUNK, sort, dir, q }))
      .then((answer) => {
        if (mine !== ticket.current) return;
        const read = agentRowsOf(answer);
        // A page that adds nothing ends the list: total said otherwise, the daemon is the one that knows.
        setRows((before) => (read.rows.length === 0 ? before : [...before, ...read.rows]));
        setTotal(read.rows.length === 0 ? rows.length : read.total);
        setPage(page + 1);
      })
      .catch((cause) => {
        if (mine === ticket.current) setError(toRpcError(cause).message);
      })
      .finally(() => {
        if (mine === ticket.current) setLoadingMore(false);
      });
  }, [current, loading, loadingMore, rows.length, total, page, sort, dir, q]);

  const reload = useCallback(() => setEpoch((n) => n + 1), []);
  return { rows, total, hasMore: rows.length < total, loading, loadingMore, error, loadMore, reload, resetKey };
}
