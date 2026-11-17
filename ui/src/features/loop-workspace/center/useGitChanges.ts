import { useEffect, useState } from "react";
import { rpc, toRpcError } from "../../../lib/bridge";

export interface ChangedFileStat { path: string; status: string; added?: number; removed?: number; binary?: boolean }

/** `workspace.git.changes`, three-state like the status: ok, not a repository, or could not tell (and why). */
export interface GitChangesView {
  state: "ok" | "not-a-repo" | "unavailable";
  reason?: string;
  files?: ChangedFileStat[];
  totalAdded: number;
  totalRemoved: number;
}

/** What changed in the project's working tree, with line counts. Re-read when `refreshKey` moves (a call ended). */
export function useGitChanges(componentRoot: string, refreshKey: number): GitChangesView | null {
  const [changes, setChanges] = useState<GitChangesView | null>(null);
  useEffect(() => {
    if (!componentRoot) return;
    let stale = false;
    rpc<GitChangesView>("workspace.git.changes", { componentRoot })
      .then((result) => { if (!stale) setChanges(result); })
      .catch((cause) => { if (!stale) setChanges({ state: "unavailable", reason: toRpcError(cause).message, totalAdded: 0, totalRemoved: 0 }); });
    return () => { stale = true; };
  }, [componentRoot, refreshKey]);
  return changes;
}
