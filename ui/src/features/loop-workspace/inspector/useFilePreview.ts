import { useEffect, useState } from "react";
import { invokeShell, rpc, toRpcError } from "../../../lib/bridge";

/** What the shell's `component_read` returns. */
export interface Preview { kind: "markdown" | "code" | "image" | "binary"; content: string | null; dataUrl: string | null; reason: string | null }

export interface FilePreview {
  loading: boolean;
  /** The file's diff against HEAD — only when git has one. */
  diff?: string;
  preview?: Preview;
  /** Why there is nothing to show: set only when neither a diff nor a preview could be had. */
  error?: string;
}

/**
 * One file, as the inspector shows it: its git diff when there is one, and
 * its own contents from the shell. The two are read together and either may
 * be missing — a file outside a repository has no diff, a binary has no text
 * — so an error is reported only when there is nothing at all to show.
 */
export function useFilePreview(componentRoot: string, path: string): FilePreview {
  const [state, setState] = useState<FilePreview>({ loading: true });
  useEffect(() => {
    let active = true;
    setState({ loading: true });
    void Promise.all([
      rpc<{ state?: string; diff?: string }>("workspace.git.diff", { componentRoot, path })
        .then((r) => (r.state === "ok" && r.diff ? r.diff : undefined), () => undefined),
      invokeShell<Preview>("component_read", { root: componentRoot, path }).then((preview) => ({ preview }), (cause) => ({ error: toRpcError(cause).message })),
    ]).then(([diff, read]) => {
      if (!active) return;
      const preview = "preview" in read ? read.preview : undefined;
      setState({ loading: false, diff, preview, error: !diff && !preview && "error" in read ? read.error : undefined });
    });
    return () => { active = false; };
  }, [componentRoot, path]);
  return state;
}
