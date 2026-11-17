// The connection's state, in three values.
//
// # Why not the ui-kit's ConnectionState
//
// `@cjlapao/ui-kit` exports `ConnectionState = "flowing" | "stopped" |
// "disabled"`. That belongs to its `ConnectionFlow` diagrams — it describes
// data moving through a pipeline, not a socket's liveness.
//
// Reusing it would be the nearest-existing-type mistake S9's T4 warns about,
// and the one this whole design pays to avoid: `unknown` becomes `—`, `—`
// becomes `0`, and the investment in uncertainty is discarded by accident.
//
// # Why three
//
// **A two-state UI is a UI that lies.** "Connected or not" cannot express the
// interval where the app has asked and does not yet know — and during that
// interval a two-state UI must pick one, so it shows stale data as current or
// a working daemon as down.
//
// S9's T3 extends this with staleness and an age. This skeleton needs only the
// three, and needs them to render distinctly.

export type Connection =
  | { kind: "connecting"; endpoint?: string }
  | {
      kind: "connected";
      /** The DAEMON's build, never the app's. */
      version: string;
      /**
       * The NEGOTIATED protocol version.
       *
       * Only present in the connected state, and that is load-bearing: the type
       * makes it impossible to render a protocol number while disconnected,
       * which is the defect S2's T7 is written against. A number shown with the
       * daemon down means the app is displaying a build-time constant and the
       * negotiation it claims to have done never happened — and that would pass
       * a naive check while the whole protocol layer was broken.
       */
      protocolVersion: number;
      /** Where the daemon was found, so the UI need not resolve it again. */
      endpoint: string;
    }
  | {
      kind: "failed";
      reason: string;
      /** The path the shell tried. The first thing anyone debugging needs. */
      endpoint: string;
    };

export const connecting: Connection = { kind: "connecting" };

/**
 * Connecting, but we already know where we are looking.
 *
 * The path is resolved before the handshake is attempted, so the status page
 * can show it immediately — which matters because the handshake is the part
 * that might hang, and "connecting to *what*" is the question somebody has
 * while it does.
 */
export function connectingTo(endpoint: string): Connection {
  return { kind: "connecting", endpoint };
}

export function connected(version: string, protocolVersion: number, endpoint: string): Connection {
  return { kind: "connected", version, protocolVersion, endpoint };
}

export function failed(reason: string, endpoint: string): Connection {
  return { kind: "failed", reason, endpoint };
}

/** The label a person reads. Each state says something different on purpose. */
export function describe(c: Connection): string {
  switch (c.kind) {
    case "connecting":
      return "connecting…";
    case "connected":
      // Both numbers, and the protocol one marked as negotiated: a bare number
      // reads as a constant, and a constant proves nothing about the socket.
      return `connected · daemon ${c.version} · protocol ${c.protocolVersion} (negotiated)`;
    case "failed":
      return `not connected — ${c.reason}`;
  }
}
