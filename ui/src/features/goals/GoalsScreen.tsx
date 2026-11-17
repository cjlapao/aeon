import { Alert } from "../../components/kit";
import type { LiveController, LiveState } from "../../lib/live";
import { GoalsPanel, type GoalsScope } from "./GoalsPanel";

/**
 * The Goals page — where the goals a brainstorm (or a person) filed wait for a
 * decision, and where an approved goal's documents, loops and tasks are read.
 * It mounts only on a current connection: a reconnecting daemon asks nothing,
 * the rule every management surface keeps.
 */
export function GoalsScreen({ controller, live, canWrite, scope }: { controller: LiveController; live: LiveState; canWrite: boolean; scope?: GoalsScope }) {
  if (live.state !== "current") {
    return <Alert intent="warning" title="The daemon is reconnecting — goals ask nothing until the connection is current." data-testid="goals-reconnecting" />;
  }
  return <GoalsPanel controller={controller} live={live} canWrite={canWrite} scope={scope} />;
}
