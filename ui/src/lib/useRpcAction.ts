import { useCallback, useState } from "react";
import { RpcError, errorSentence, refusalBody, toRpcError } from "./bridge";
import { notifications } from "./notifications";

export interface RunActionOptions {
  /** Shown as a success toast once the action resolves. Omit for an action where a toast would be pointless — switching workspaces reloads the page immediately after, for instance. */
  successTitle?: string;
  successBody?: string;
  /** @default "Something went wrong" */
  errorTitle?: string;
}

export interface RpcAction {
  busy: boolean;
  /** The last failure's message, or undefined. Cleared at the start of every `run`. */
  error: string | undefined;
  /** S57 T11: the parsed `RpcError` beside the message. A daemon refusal's
   *  concrete sentence is in `data.what`, not in `message` (the fixtures of
   *  S57 prove it: message is one generic sentence per door) — a surface
   *  that shows only the message shows a refusal that names nothing. */
  rpcError: RpcError | undefined;
  /** Runs `action`, managing `busy`/`error` and pushing the success/failure toast — the try/catch/setBusy/setError dance `WorkspacesPanel.tsx` had three copies of before this existed. Resolves to the action's result, or `undefined` on failure (check `error` to tell "failed" apart from "genuinely resolved to undefined"). */
  run<T>(action: () => Promise<T>, options?: RunActionOptions): Promise<T | undefined>;
}

/**
 * Deliberately generic over any async action, not just `rpcTyped` calls:
 * plenty of "do a thing, toast the outcome" moments — adding a workspace,
 * removing one — go through Tauri shell commands, not the daemon protocol.
 * `useRpcQuery` is the read half of this; this is the write half.
 */
export function useRpcAction(): RpcAction {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);
  const [rpcError, setRpcError] = useState<RpcError | undefined>(undefined);

  const run = useCallback(async <T,>(action: () => Promise<T>, options?: RunActionOptions): Promise<T | undefined> => {
    setBusy(true);
    setError(undefined);
    setRpcError(undefined);
    try {
      const result = await action();
      if (options?.successTitle) {
        notifications.push({ title: options.successTitle, body: options.successBody, tone: "success", source: "local" });
      }
      return result;
    } catch (cause) {
      const parsed = toRpcError(cause);
      const message = errorSentence(parsed);
      setError(message);
      setRpcError(parsed);
      // The toast is the whole refusal: the daemon's sentence, then what it means and what to do
      // about it when it said so — no panel stays on the page after the toast is gone.
      notifications.push({ title: options?.errorTitle ?? "Something went wrong", body: refusalBody(parsed), tone: "danger", source: "local" });
      return undefined;
    } finally {
      setBusy(false);
    }
  }, []);

  return { run, busy, error, rpcError };
}
