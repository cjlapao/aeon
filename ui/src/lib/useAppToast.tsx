import { useMemo, type ReactNode } from "react";
import { useToast } from "@cjlapao/ui-kit";
import type { RpcError } from "./bridge";

// useAppToast — how a page tells the person something happened, without taking room on the page.
//
// One rule, two kinds:
//   - an outcome (saved, deleted, copied) is a toast that goes away by itself — the kit's default
//     life — and takes no space where the person is working;
//   - a failure is a toast that STAYS until it is dismissed (`sticky`): a refusal is something to
//     read and act on, so it must not vanish on a timer. The daemon's own words (`what` / `means`
//     / `todo`) ride in the toast, as written.
//
// It needs the app's `ToastProvider` + `ToastViewport` (mounted once, in the app shell); outside
// them the kit throws, on purpose.

export interface AppToast {
  /** Went well. Goes away by itself. */
  success: (title: ReactNode, detail?: ReactNode) => number;
  /** Worth knowing. Goes away by itself. */
  info: (title: ReactNode, detail?: ReactNode) => number;
  /** Needs attention but is not a failure. Stays until dismissed. */
  warning: (title: ReactNode, detail?: ReactNode) => number;
  /** A failure. Stays until dismissed; a daemon refusal prints whole. */
  error: (failure: RpcError | string, title?: ReactNode) => number;
  /** Dismiss one, by the id a call returned. */
  dismiss: (id: number) => void;
}

/** A refusal's parts, as the daemon sent them — the toast's body. */
function FailureDetail({ error }: { error: RpcError }) {
  const data = error.data;
  if (data) {
    return (
      <dl className="mt-1 space-y-1 text-sm" data-testid="toast-error-parts">
        <div><dt className="inline opacity-70">what </dt><dd className="inline">{data.what}</dd></div>
        <div><dt className="inline opacity-70">means </dt><dd className="inline">{data.means}</dd></div>
        <div><dt className="inline opacity-70">todo </dt><dd className="inline font-medium">{data.todo}</dd></div>
        {data.fatal && <div>this connection is unusable — reconnect</div>}
      </dl>
    );
  }
  if (error.kind === "timeout") return <span>The daemon did not answer before the timeout. Its operation may still be running; refresh before retrying.</span>;
  return null;
}

export function useAppToast(): AppToast {
  const { toast } = useToast();
  return useMemo<AppToast>(
    () => ({
      success: (title, detail) => toast.success(title, detail),
      info: (title, detail) => toast.info(title, detail),
      warning: (title, detail) => toast.warning(title, detail, { sticky: true }),
      error: (failure, title) =>
        typeof failure === "string"
          ? toast.danger(title ?? failure, title === undefined ? undefined : failure, { sticky: true })
          : toast.danger(title ?? failure.message, <FailureDetail error={failure} />, { sticky: true }),
      dismiss: (id) => toast.close(id),
    }),
    [toast],
  );
}
