import { useEffect, useState } from "react";
import { rpc, toRpcError } from "../../../lib/bridge";

/** `workspace.git.status`, three-state: ok, not a repository, or could not tell (and why). */
export interface GitStatusView {
  state: "ok" | "not-a-repo" | "unavailable";
  reason?: string;
  branch?: string;
  detached?: boolean;
  upstream?: string;
  ahead?: number;
  behind?: number;
  changed?: { path: string; index: string; worktree: string }[];
}

/**
 * The project's git state. Re-read whenever `refreshKey` moves (the page bumps
 * it when a tool call ends — a write may have changed the tree). A failed call
 * is `unavailable` with its reason, never a thrown error and never an empty
 * "clean" repository (P31: could-not-tell is its own answer).
 */
export function useGitStatus(componentRoot: string, refreshKey: number): GitStatusView | null {
  const [status, setStatus] = useState<GitStatusView | null>(null);
  useEffect(() => {
    if (!componentRoot) return;
    let stale = false;
    rpc<GitStatusView>("workspace.git.status", { componentRoot })
      .then((result) => { if (!stale) setStatus(result); })
      .catch((cause) => { if (!stale) setStatus({ state: "unavailable", reason: toRpcError(cause).message }); });
    return () => { stale = true; };
  }, [componentRoot, refreshKey]);
  return status;
}
