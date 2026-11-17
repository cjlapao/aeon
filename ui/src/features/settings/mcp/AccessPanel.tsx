import { useCallback } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, Pill } from "../../../components/kit";
import { rpcTyped } from "../../../lib/bridge";
import { useRpcAction } from "../../../lib/useRpcAction";
import { useTypeToConfirm } from "../../../lib/useConfirm";

export type AccessKind = "all" | "read";

/** The one allow rule a grant writes: every agent, this server, every tool (or its read tools). */
export function accessRule(server: string, kind: AccessKind) {
  const suffix = kind === "read" ? "read" : "*";
  return {
    id: `mcp-${server}-${kind === "read" ? "read" : "all"}`,
    type: "tool_access",
    target: { kind: "all-agents", id: "" },
    action: "allow",
    source: "operator",
    spec: { toolName: `mcp.${server}.*.${suffix}`, state: "allow" },
    notAfter: null,
  };
}

/**
 * Who may use this server's tools, in one place. "Give every agent access"
 * shares the server (every agent, present and future, declares its tools) and
 * writes one allow rule for all agents — two writes, one confirmation, the
 * person's. Tools whose effect asks at use still ask, whatever the rule.
 */
export function AccessPanel({ name, shared, canWrite, onChanged }: { name: string; shared: boolean; canWrite: boolean; onChanged: () => void }) {
  const { t } = useI18n();
  const action = useRpcAction();
  const { confirm, modal } = useTypeToConfirm();

  const grant = useCallback(
    async (kind: AccessKind) => {
      const confirmed = await confirm({
        title: t("settings.mcp.access.confirmTitle", { name }),
        description: t(kind === "read" ? "settings.mcp.access.confirmRead" : "settings.mcp.access.confirmAll", { name }),
        confirmValue: name,
        confirmValueLabel: t("settings.mcp.access.confirmLabel"),
        confirmLabel: t("settings.mcp.access.confirm"),
      });
      if (!confirmed) return;
      const shareResult = await action.run(() => rpcTyped("mcp.server.share", { name, shared: true }), { errorTitle: t("settings.mcp.access.failed") });
      if (!shareResult) return;
      await action.run(() => rpcTyped("policy.add", accessRule(name, kind)), { errorTitle: t("settings.mcp.access.ruleFailed") });
      onChanged();
    },
    [action, confirm, name, onChanged, t],
  );

  const revoke = useCallback(async () => {
    const confirmed = await confirm({
      title: t("settings.mcp.access.stopTitle", { name }),
      description: t("settings.mcp.access.stopBody", { name }),
      confirmValue: name,
      confirmValueLabel: t("settings.mcp.access.confirmLabel"),
      confirmLabel: t("settings.mcp.access.stop"),
    });
    if (!confirmed) return;
    await action.run(() => rpcTyped("mcp.server.share", { name, shared: false }), { errorTitle: t("settings.mcp.access.failed") });
    // The allow rules this panel wrote go with it; a rule someone else wrote stays.
    for (const kind of ["all", "read"] as const) {
      try {
        await rpcTyped("policy.remove", { id: accessRule(name, kind).id });
      } catch {
        // No such rule: nothing to take back.
      }
    }
    onChanged();
  }, [action, confirm, name, onChanged, t]);

  return (
    <div className="space-y-2" data-testid="access-panel">
      <p className="flex items-center gap-2 text-xs uppercase tracking-wide opacity-70">
        {t("settings.mcp.access.title")}
        <Pill tone={shared ? "green" : "slate"} data-testid="access-state">
          {shared ? t("settings.mcp.access.shared") : t("settings.mcp.access.notShared")}
        </Pill>
      </p>
      {action.error && <Alert intent="danger" title={action.error} />}
      <p className="text-xs opacity-80">{t("settings.mcp.access.hint")}</p>
      <div className="flex flex-wrap gap-2">
        <Button size="xs" variant="outline" disabled={!canWrite || action.busy} onClick={() => void grant("all")} data-testid="access-all">
          {t("settings.mcp.access.all")}
        </Button>
        <Button size="xs" variant="outline" disabled={!canWrite || action.busy} onClick={() => void grant("read")} data-testid="access-read">
          {t("settings.mcp.access.read")}
        </Button>
        {shared && (
          <Button size="xs" variant="outline" disabled={!canWrite || action.busy} onClick={() => void revoke()} data-testid="access-stop">
            {t("settings.mcp.access.stop")}
          </Button>
        )}
      </div>
      {modal}
    </div>
  );
}
