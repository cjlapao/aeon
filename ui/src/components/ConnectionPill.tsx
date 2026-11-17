import { Pill } from "./kit";
import { describe, type Connection } from "../lib/connection";

/**
 * The connection, as a pill.
 *
 * Three states, three tones, three labels. **The tone alone is not the
 * signal** — a colour is unavailable to anybody who cannot distinguish them,
 * and `describe` carries the fact in words.
 */
export function ConnectionPill({ connection }: { connection: Connection }) {
  const tone =
    connection.kind === "connected" ? "green" : connection.kind === "connecting" ? "amber" : "red";

  return (
    <Pill tone={tone} data-testid="connection" data-state={connection.kind}>
      {describe(connection)}
    </Pill>
  );
}
