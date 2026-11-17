import { useCallback, useEffect, useRef, useState } from "react";
import { rpcTyped } from "../../../lib/bridge";
import type { RpcMethods } from "../../../proto/contract";

export type McpProcess = RpcMethods["mcp.processes"]["result"]["processes"][number];
export type McpLogLine = RpcMethods["mcp.process.logs"]["result"]["lines"][number];

const LOG_KEEP = 500;

/** The supervisor's view of every server, polled: the daemon owns the truth, the page only re-asks. */
export function useMcpProcesses(intervalMs = 3000) {
  const [procs, setProcs] = useState<Record<string, McpProcess>>({});
  const refresh = useCallback(async () => {
    try {
      const answer = await rpcTyped("mcp.processes", {});
      setProcs(Object.fromEntries(answer.processes.map((p) => [p.name, p])));
    } catch {
      // A missed poll keeps the last answer; the next one corrects it.
    }
  }, []);
  useEffect(() => {
    void refresh();
    const id = setInterval(() => void refresh(), intervalMs);
    return () => clearInterval(id);
  }, [refresh, intervalMs]);
  return { procs, refresh };
}

/** One server's captured output, followed by sequence number so no line repeats. */
export function useMcpLogs(name: string, intervalMs = 2000) {
  const [lines, setLines] = useState<McpLogLine[]>([]);
  const next = useRef(0);
  useEffect(() => {
    setLines([]);
    next.current = 0;
    let alive = true;
    const poll = async () => {
      try {
        const answer = await rpcTyped("mcp.process.logs", { name, after: next.current, limit: 200 });
        if (!alive) return;
        next.current = answer.next;
        if (answer.lines.length > 0) setLines((current) => [...current, ...answer.lines].slice(-LOG_KEEP));
      } catch {
        // Logs are best-effort; the next poll retries from the same position.
      }
    };
    void poll();
    const id = setInterval(() => void poll(), intervalMs);
    return () => {
      alive = false;
      clearInterval(id);
    };
  }, [name, intervalMs]);
  return lines;
}
