import { useCallback, useEffect, useState } from "react";
import { rpcTyped, toRpcError } from "../../../lib/bridge";
import type { RpcMethods } from "../../../proto/contract";

/**
 * S34 design.md TB2's three-state union, as a type rather than a
 * convention: `{ state: "ok", value: [] }` and "we could not ask" are
 * different pixels, and a renderer that collapses them into one empty
 * state is the plausible-zero bug TB2 exists to rule out. Shared by
 * AgentsPage (T6) and McpPage (T7) — the one place this type is declared.
 */
export type Fact<T> =
  | { state: "loading" }
  | { state: "ok"; value: T }
  | { state: "unwired" }
  | { state: "error"; message: string };

/**
 * A method-not-found RpcError (-32601 — the daemon does not register this
 * method, an old build) is "unwired", distinct from a genuine failure: a
 * page open against an old daemon must say so in the fact's own cell, never
 * render silence as a plausible zero (TB12).
 */
export function classify(error: unknown): Fact<never> {
  const e = toRpcError(error);
  if (e.code === -32601) return { state: "unwired" };
  return { state: "error", message: e.message };
}

export interface FactHandle<T> {
  fact: Fact<T>;
  /** design §3's "Refresh" — refetch, never a dial (TB9). */
  refetch: () => void;
}

/**
 * Loads one RPC method's result as a Fact, re-fetchable on demand. `enabled`
 * lets a caller hold off calling until it has a real param (e.g. an agent
 * name chosen from another fact that hasn't resolved yet) — disabled starts
 * and stays at `{state: "loading"}` rather than firing a call with an empty
 * param that would read as a different agent entirely.
 */
export function useFact<K extends keyof RpcMethods>(
  method: K,
  params: RpcMethods[K]["params"],
  enabled = true,
): FactHandle<RpcMethods[K]["result"]> {
  const [fact, setFact] = useState<Fact<RpcMethods[K]["result"]>>({ state: "loading" });
  const paramsKey = JSON.stringify(params);

  const load = useCallback(() => {
    if (!enabled) return;
    setFact({ state: "loading" });
    void rpcTyped(method, params)
      .then((value) => setFact({ state: "ok", value }))
      .catch((cause) => setFact(classify(cause)));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [method, paramsKey, enabled]);

  useEffect(() => {
    load();
  }, [load]);

  return { fact, refetch: load };
}
