import { Button, Panel } from "@cjlapao/ui-kit";
import { RpcError } from "../lib/bridge";

/**
 * A failure, in three parts where the daemon sent them.
 *
 * # Why a Panel and not an Alert
 *
 * **`Alert` discards its children**, rendering only `title` and `description`
 * — found by looking at the DOM, after a test could not find content that was
 * definitely in the JSX. An error needs four labelled parts, so it needs a
 * component that renders what it is given.
 *
 * Recorded in `ui/README.md` alongside the kit's other two gaps: this is the
 * same trap as reaching for the nearest existing type, one level up.
 *
 * A transport failure has no three parts. The bridge keeps its kind separate
 * from a timeout and a daemon error that happens to omit `data`.
 */
export function ErrorPanel({ error, onRetry }: { error: RpcError; onRetry?: () => void }) {
  const data = error.data;

  return (
    <Panel tone="red" data-testid="error" variant="simple" corner="rounded-md" padding="sm">
      <div className="space-y-1">
        <div className="font-medium">
          {error.message}
          {error.code !== 0 && error.kind === "rpc" && (
            <span className="ml-2 text-xs opacity-60" data-testid="error-code">
              ({error.code})
            </span>
          )}
        </div>

        {data ? (
          <dl className="mt-2 space-y-1 text-sm" data-testid="error-parts">
            <div className="flex gap-2">
              <dt className="w-14 shrink-0 opacity-60">what</dt>
              <dd>{data.what}</dd>
            </div>
            <div className="flex gap-2">
              <dt className="w-14 shrink-0 opacity-60">means</dt>
              <dd>{data.means}</dd>
            </div>
            <div className="flex gap-2">
              <dt className="w-14 shrink-0 opacity-60">todo</dt>
              <dd className="font-medium">{data.todo}</dd>
            </div>
            {data.fatal && (
              <div className="flex gap-2">
                <dt className="w-14 shrink-0 opacity-60">fatal</dt>
                <dd>this connection is unusable — reconnect</dd>
              </div>
            )}
          </dl>
        ) : error.kind === "timeout" ? (
          <p className="text-sm opacity-80" data-testid="error-timeout">
            The daemon did not answer before the timeout. Its operation may still be running; refresh before retrying.
          </p>
        ) : error.kind === "rpc" ? (
          <p className="text-sm opacity-80" data-testid="error-rpc">
            The daemon refused this request and returned no further detail.
          </p>
        ) : (
          <p className="text-sm opacity-80" data-testid="error-transport">
            The call did not reach the daemon, so there is no further detail to show.
          </p>
        )}

        {onRetry && (
          <Button size="sm" variant="soft" onClick={onRetry}>
            try again
          </Button>
        )}
      </div>
    </Panel>
  );
}
