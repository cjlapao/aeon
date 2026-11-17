import { useRef, useState, type MutableRefObject } from "react";
import { rpcTyped, toRpcError } from "../../lib/bridge";
import { useAppToast } from "../../lib/useAppToast";
import type { RpcMethods } from "../../proto/contract";

// ---------------------------------------------------------------------------
// The one write lifecycle the bodies share. Each body owns its state — a create
// still flying must not grey out a Save on a different row — and an answer
// whose guard no longer matches is dropped rather than painted over whatever
// the person has moved on to.
// ---------------------------------------------------------------------------

export interface PanelWrites {
  /** A write is out. Every submit control in the body reads this. */
  busy: boolean;
  /** The one disabled rule every control in the body uses. */
  writeDisabled: boolean;
  /** One write, in the panel's one shape: busy, then the answer or the refusal. */
  rpcWrite: <K extends keyof RpcMethods>(
    method: K,
    params: RpcMethods[K]["params"],
  ) => Promise<RpcMethods[K]["result"] | undefined>;
  /** Report a landed write once, even after this body is gone. */
  onChangedRef: MutableRefObject<() => void>;
}

export function usePanelWrites({
  canWrite,
  guard,
  onChanged,
}: {
  canWrite: boolean;
  guard: string;
  onChanged: () => void;
}): PanelWrites {
  const [busy, setBusy] = useState(false);
  const notify = useAppToast();

  const askedFor = useRef(guard);
  askedFor.current = guard;
  const onChangedRef = useRef(onChanged);
  onChangedRef.current = onChanged;

  async function rpcWrite<K extends keyof RpcMethods>(
    method: K,
    params: RpcMethods[K]["params"],
  ): Promise<RpcMethods[K]["result"] | undefined> {
    setBusy(true);
    try {
      const answer = await rpcTyped(method, params);
      if (askedFor.current === guard) setBusy(false);
      return answer;
    } catch (cause) {
      // The refusal whole — `what` / `means` / `todo` beside the daemon's own sentence, printed as
      // written and never paraphrased — in a toast that stays until it is dismissed. It is shown even
      // if the person has moved on: a refused write is not something to lose.
      if (askedFor.current === guard) setBusy(false);
      notify.error(toRpcError(cause));
      return undefined;
    }
  }

  return { busy, writeDisabled: !canWrite || busy, rpcWrite, onChangedRef };
}
