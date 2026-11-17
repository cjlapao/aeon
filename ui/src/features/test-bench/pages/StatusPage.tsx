import { Alert, Button, InfoRow, Panel, Pill } from "../../../components/kit";
import { ConnectionPill } from "../../../components/ConnectionPill";
import type { Connection } from "../../../lib/connection";

/**
 * What the daemon is, and where.
 *
 * The one page that existed at S1, kept because it answers the first question
 * anybody has — *is it there* — and because the disconnected state naming the
 * socket path is the first thing anyone debugging needs. `copyable` on that
 * path is not decoration: comparing it against the daemon's own startup line is
 * how a mismatch gets found.
 *
 * It is also **the way back**. A window that lost its socket had no recovery
 * short of restarting the app, because the handshake was reachable only from a
 * mount that had already happened. Reconnect sits beside the state it changes,
 * because an affordance for a fact belongs next to the fact.
 */
export function StatusPage({
  connection,
  onReconnect,
}: {
  connection: Connection;
  /** Ask the daemon for a fresh session. A new handshake, never a cached state. */
  onReconnect: () => void;
}) {
  const connecting = connection.kind === "connecting";

  return (
    <div className="space-y-4">
      <Panel>
        <div className="flex items-center justify-between gap-4">
          <span className="text-sm opacity-70">daemon</span>
          <div className="flex items-center gap-2">
            <ConnectionPill connection={connection} />

            {connection.kind !== "connected" && (
              // Three states, three renderings — still. While the app is
              // *connecting* the button stays **visible and disabled** rather
              // than hidden: hiding it would make the in-flight state look like
              // a connected one (no pill change, no affordance), and leaving it
              // live would let a second click race the handshake already under
              // way. `disabled` carries the third state's cost without
              // collapsing it into either neighbour.
              <Button
                size="sm"
                variant="outline"
                data-testid="reconnect"
                disabled={connecting}
                onClick={onReconnect}
              >
                {connecting ? "Connecting…" : "Reconnect"}
              </Button>
            )}
          </div>
        </div>

        <div className="mt-4 space-y-1" data-testid="status-rows">
          <InfoRow
            label="endpoint"
            value={connection.kind === "connecting" ? "…" : connection.endpoint}
            mono
            copyable
          />

          {connection.kind === "connected" && (
            <>
              <InfoRow label="daemon version" value={connection.version} mono />
              <InfoRow
                label="protocol"
                value={
                  // Marked as negotiated: a bare number reads as a constant,
                  // and a constant proves nothing about whether a handshake
                  // happened.
                  <span data-testid="protocol-version">
                    <code>{connection.protocolVersion}</code>
                    <Pill tone="green" size="sm" className="ml-2">
                      negotiated
                    </Pill>
                  </span>
                }
              />
            </>
          )}
        </div>
      </Panel>

      {connection.kind === "failed" && (
        // `Alert` renders `title` and `description` and DISCARDS children, so
        // prose goes in `description`. See ui/README.md.
        <Alert
          tone="amber"
          title="Nothing is connected"
          description="The daemon is not answering at the path above. Start it with `aeon serve`, or `make dev-daemon` from the repository."
          data-testid="not-connected"
        />
      )}
    </div>
  );
}
