import { useCallback, useContext, useEffect, useRef, useState } from "react";
import { rpcTyped, toRpcError } from "./bridge";
import { beginWait, PageWaitOwnerContext } from "./pageWait";
import type { LiveController, LiveState } from "./live";
import type { RpcMethods } from "../proto/contract";

export interface RpcQueryState<T> {
  data: T | undefined;
  /** True only while no data has ever loaded yet — a refetch after reconnect doesn't flip this back on, so a screen doesn't flash back to a loading state it already has an answer for. */
  loading: boolean;
  error: string | undefined;
  refetch: () => void;
}

/**
 * The daemon-data-fetch lifecycle every screen with a single `rpcTyped` call
 * hand-rolled — `ProjectsScreen.tsx`'s original `read()` is where this was
 * lifted from: a `useState` triple, a `controller.registerReader` so the
 * connection's own "every reader runs before I call myself current" promise
 * holds, and a `live.generation`-keyed refetch after a reconnect.
 *
 * Params are compared by their JSON serialization — a fresh object literal
 * each render is the common case, and comparing by reference would refetch
 * on every render for no reason.
 */
export function useRpcQuery<K extends keyof RpcMethods>(
  method: K,
  params: RpcMethods[K]["params"],
  controller: LiveController,
  live: LiveState,
): RpcQueryState<RpcMethods[K]["result"]> {
  const [data, setData] = useState<RpcMethods[K]["result"] | undefined>(undefined);
  const [error, setError] = useState<string | undefined>(undefined);
  const loadedOnce = useRef(false);
  const [loading, setLoading] = useState(true);

  const paramsKey = JSON.stringify(params);
  // The page this query belongs to, if the shell labelled it. A wait is
  // taken against that id for the duration of the call, so the shell's
  // spinner on the page's own row is the same fact this hook's `loading` is —
  // read from outside the page, where the menu is (lib/pageWait).
  const owner = useContext(PageWaitOwnerContext);

  const fetchOnce = useCallback(async () => {
    if (!loadedOnce.current) setLoading(true);
    const settle = beginWait(owner, "data");
    try {
      const result = await rpcTyped(method, params);
      setData(result);
      setError(undefined);
    } catch (cause) {
      setError(toRpcError(cause).message);
    } finally {
      settle();
      loadedOnce.current = true;
      setLoading(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [method, paramsKey, owner]);

  useEffect(() => controller.registerReader(fetchOnce), [controller, fetchOnce]);
  useEffect(() => {
    if (live.state === "current") void fetchOnce();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [live.state === "current" ? live.generation : -1, fetchOnce]);

  return { data, loading, error, refetch: fetchOnce };
}
