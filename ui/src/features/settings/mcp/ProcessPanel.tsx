import { useCallback } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, Pill } from "../../../components/kit";
import { rpcTyped } from "../../../lib/bridge";
import { useRpcAction } from "../../../lib/useRpcAction";
import { useMcpLogs, useMcpProcesses, type McpProcess } from "./useMcpProcesses";

/** The pill tone for a supervisor state: running is green, a stop the person asked for is slate, anything else is a problem. */
export function processTone(state: string): "green" | "slate" | "amber" | "rose" {
  if (state === "running") return "green";
  if (state === "stopped") return "slate";
  if (state === "starting") return "amber";
  return "rose";
}

export function ProcessPill({ process }: { process: McpProcess | undefined }) {
  const { t } = useI18n();
  if (!process) return <span className="text-xs opacity-60" data-testid="process-none">{t("settings.mcp.process.unknown")}</span>;
  return (
    <Pill tone={processTone(process.state)} data-testid="process-state">
      {process.state}
      {process.pid ? ` · ${process.pid}` : ""}
    </Pill>
  );
}

/**
 * One server's process: state, PID, start time, restarts and last exit, the
 * Start / Stop / Restart buttons, and the output the daemon captured. A
 * crashed server is not restarted for you — the page shows how it ended and
 * the person decides.
 */
export function ProcessPanel({ name, canWrite }: { name: string; canWrite: boolean }) {
  const { t } = useI18n();
  const action = useRpcAction();
  const { procs, refresh } = useMcpProcesses();
  const lines = useMcpLogs(name);
  const proc = procs[name];
  const running = proc?.state === "running" || proc?.state === "starting";

  const control = useCallback(
    async (verb: "start" | "stop" | "restart") => {
      const call = verb === "start" ? () => rpcTyped("mcp.process.start", { name }) : verb === "stop" ? () => rpcTyped("mcp.process.stop", { name }) : () => rpcTyped("mcp.process.restart", { name });
      await action.run(call, { errorTitle: t("settings.mcp.process.failed", { verb }) });
      await refresh();
    },
    [action, name, refresh, t],
  );

  return (
    <div className="space-y-2" data-testid="process-panel">
      <p className="text-xs uppercase tracking-wide opacity-70">{t("settings.mcp.process.title")}</p>
      {action.error && <Alert intent="danger" title={action.error} />}
      <div className="flex flex-wrap items-center gap-2 text-xs">
        <ProcessPill process={proc} />
        {proc?.pid ? <span data-testid="process-pid">PID {proc.pid}</span> : null}
        {proc?.startedAt ? <span className="opacity-70">{t("settings.mcp.process.since", { at: proc.startedAt })}</span> : null}
        {proc ? <span className="opacity-70">{t("settings.mcp.process.restarts", { count: proc.restarts })}</span> : null}
      </div>
      {proc?.lastExit ? <p className="text-xs opacity-80" data-testid="process-last-exit">{t("settings.mcp.process.lastExit", { exit: proc.lastExit })}</p> : null}
      <div className="flex gap-2">
        <Button size="xs" variant="outline" disabled={!canWrite || action.busy || running} onClick={() => void control("start")} data-testid="process-start">
          {t("settings.mcp.process.start")}
        </Button>
        <Button size="xs" variant="outline" disabled={!canWrite || action.busy || !running} onClick={() => void control("stop")} data-testid="process-stop">
          {t("settings.mcp.process.stop")}
        </Button>
        <Button size="xs" variant="outline" disabled={!canWrite || action.busy} onClick={() => void control("restart")} data-testid="process-restart">
          {t("settings.mcp.process.restart")}
        </Button>
      </div>
      <pre className="max-h-64 overflow-auto rounded-md bg-slate-900 p-2 font-mono text-xs text-slate-100" data-testid="process-logs" aria-label={t("settings.mcp.process.logs")}>
        {lines.length === 0 ? t("settings.mcp.process.noLogs") : lines.map((l) => `${l.stream === "stderr" ? "! " : "  "}${l.text}`).join("\n")}
      </pre>
    </div>
  );
}
