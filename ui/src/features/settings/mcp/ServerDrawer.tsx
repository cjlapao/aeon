import { useCallback, useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, EmptyState, Pill, Toggle } from "../../../components/kit";
import { ErrorPanel } from "../../../components/ErrorPanel";
import { RpcError, rpcTyped } from "../../../lib/bridge";
import { useRpcAction } from "../../../lib/useRpcAction";
import { useTypeToConfirm } from "../../../lib/useConfirm";
import { useRpcQuery } from "../../../lib/useRpcQuery";
import type { LiveController, LiveState } from "../../../lib/live";
import { AccessPanel } from "./AccessPanel";
import { ProcessPanel } from "./ProcessPanel";
import { describeBeforeAfter, type ServerDetail } from "./mcpModel";

/**
 * The drawer `mcp.server` fills: the full transport, every tool with its
 * classes, who declares what, and the server's own audit — beside the
 * actions. A SYSTEM row hides Edit: "to change a shipped server, duplicate
 * it" (the daemon's law — the inert set's definitions are nobody's to
 * edit); Remove's confirm names the declaring agents and offers
 * `cascadeDeclares` as an explicit, default-off checkbox (MG: revoking
 * nobody's grants silently is not an option).
 */
export interface ServerDrawerProps {
  name: string;
  controller: LiveController;
  live: LiveState;
  canWrite: boolean;
  onClose: () => void;
  onChanged: () => void;
  onEdit: (detail: ServerDetail) => void;
  onDuplicate: (detail: ServerDetail) => void;
}

export function ServerDrawer({ name, controller, live, canWrite, onChanged, onEdit, onDuplicate }: ServerDrawerProps) {
  const { t } = useI18n();
  const action = useRpcAction();
  const { modal } = useTypeToConfirm();
  const [removeResult, setRemoveResult] = useState<string | undefined>(undefined);
  const [cascade, setCascade] = useState(false);
  const [removeTarget, setRemoveTarget] = useState<ServerDetail | undefined>(undefined);

  const detail = useRpcQuery("mcp.server", { name }, controller, live);
  const system = detail.data?.server.origin === "system";

  const toggle = useCallback(async () => {
    if (!detail.data) return;
    const next = !detail.data.server.enabled;
    const result = await action.run(() => rpcTyped("mcp.server.setEnabled", { name, enabled: next }), { errorTitle: t("settings.mcp.drawer.setEnabledFailed") });
    if (result) {
      detail.refetch();
      onChanged();
    }
  }, [action, detail, name, onChanged, t]);

  const askRemove = useCallback(() => {
    if (!detail.data) return;
    setRemoveTarget(detail.data.server);
    setCascade(false);
  }, [detail.data]);

  const doRemove = useCallback(async () => {
    const result = await action.run(
      () => rpcTyped("mcp.server.remove", { name, cascadeDeclares: cascade }),
      { errorTitle: t("settings.mcp.drawer.removeFailed") },
    );
    if (result) {
      const before = (result.grants ?? []).flatMap((entry) => (entry.before ?? []) as Array<{ qualified: string; state: string; tier: string }>);
      const after = (result.grants ?? []).flatMap((entry) => (entry.after ?? []) as Array<{ qualified: string; state: string; tier: string }>);
      const diff = describeBeforeAfter(before, after);
      // The result STATES the grants' before/after verbatim (the daemon's
      // own before/after answer — no rewrite, no toast that disappears).
      setRemoveResult(
        diff.changed
          ? `${t("settings.mcp.drawer.grantsChanged", { added: diff.added.join(", "), removed: diff.removed.join(", ") })}`
          : t("settings.mcp.drawer.grantsUnchanged"),
      );
      setRemoveTarget(undefined);
      onChanged();
    }
  }, [action, cascade, name, onChanged, t]);

  const H = "text-xs font-semibold uppercase tracking-widest opacity-70";
  const server = detail.data?.server;

  return (
    <div className="space-y-8" data-testid="server-drawer">
      {action.error && <Alert intent="danger" title={action.error} />}
      {detail.loading && <EmptyState title={t("settings.mcp.drawer.loading")} />}
      {detail.error && !detail.loading && <ErrorPanel error={new RpcError(0, detail.error)} onRetry={detail.refetch} />}
      {detail.data && server && (
        <>
          {removeResult && <div data-testid="remove-result"><Alert intent="success" title={removeResult} /></div>}

          <section className="space-y-3">
            {(server.title || server.description) && (
              <div>
                {server.title && server.title !== name && <h2 className="text-lg font-semibold">{server.title}</h2>}
                {server.description && <p className="text-sm opacity-80">{server.description}</p>}
              </div>
            )}
            <div className="flex flex-wrap items-center gap-2">
              <Pill tone={server.tier === "public" ? "blue" : "violet"}>{server.tier}</Pill>
              <Pill tone={system ? "blue" : "slate"}>{server.origin}</Pill>
              <code className="text-xs opacity-60">{name}</code>
            </div>
            <Toggle
              size="sm"
              checked={server.enabled}
              disabled={!canWrite || action.busy}
              label={server.enabled ? t("settings.mcp.servers.enabled") : t("settings.mcp.servers.disabled")}
              aria-label={server.enabled ? t("settings.mcp.drawer.disable") : t("settings.mcp.drawer.enable")}
              onChange={() => void toggle()}
              data-testid="drawer-toggle"
            />
            <div className="flex flex-wrap items-center gap-2">
              {!system && (
                <Button size="sm" disabled={!canWrite || action.busy} onClick={() => onEdit(server)} data-testid="drawer-edit">{t("settings.mcp.drawer.edit")}</Button>
              )}
              <Button size="sm" variant="ghost" disabled={!canWrite || action.busy} onClick={() => onDuplicate(server)} data-testid="drawer-duplicate">{t("settings.mcp.drawer.duplicate")}</Button>
              <Button size="sm" variant="ghost" disabled={!canWrite || action.busy} onClick={askRemove} data-testid="drawer-remove">{t("settings.mcp.drawer.remove")}</Button>
              {system && <span className="text-xs opacity-70" data-testid="drawer-system-note">{t("settings.mcp.drawer.systemNoEdit")}</span>}
            </div>
          </section>

          {removeTarget && (
            <section className="space-y-3" data-testid="remove-confirm">
              <h3 className={H}>{t("settings.mcp.drawer.removeQuestion", { name })}</h3>
              {(detail.data.declaredBy ?? []).length > 0 && (
                <p className="text-sm" data-testid="remove-declare-names">
                  {t("settings.mcp.drawer.removeDeclaredBy", { agents: (detail.data.declaredBy ?? []).join(", ") })}
                </p>
              )}
              {(detail.data.declaredBy ?? []).length > 0 && (
                <div data-testid="cascade-checkbox">
                  <Toggle size="sm" checked={cascade} label={t("settings.mcp.drawer.cascadeDeclares")} aria-label={t("settings.mcp.drawer.cascadeDeclares")} onChange={(event) => setCascade(event.target.checked)} />
                </div>
              )}
              <div className="flex gap-2">
                <Button size="sm" disabled={action.busy} onClick={() => void doRemove()}>{t("settings.mcp.drawer.removeConfirm")}</Button>
                <Button size="sm" variant="ghost" onClick={() => setRemoveTarget(undefined)}>{t("settings.mcp.form.cancel")}</Button>
              </div>
            </section>
          )}

          <section className="space-y-3">
            <h3 className={H}>{t("settings.mcp.drawer.runs")}</h3>
            <div className="space-y-2 text-sm">
              <div>
                <span className="text-xs opacity-60">{t("settings.mcp.drawer.transport")}</span>
                <div className="font-mono text-xs">
                  {server.transport.type} {server.transport.command} {(server.transport.args ?? []).join(" ")}
                </div>
              </div>
              <div>
                <span className="text-xs opacity-60">{t("settings.mcp.drawer.envNames")}</span>
                <div className="flex flex-wrap gap-1" data-testid="drawer-env">
                  {(server.transport.envNames ?? []).map((envName) => <Pill key={envName} size="sm" tone="slate">{envName}</Pill>)}
                </div>
              </div>
            </div>
            {server.enabled && <ProcessPanel name={name} canWrite={canWrite} />}
          </section>

          <section className="space-y-3">
            <h3 className={H}>{t("settings.mcp.drawer.tools")}</h3>
            <ul className="space-y-1" data-testid="drawer-tools">
              {(server.tools ?? []).map((tool) => (
                <li key={tool.name} className="flex flex-wrap items-center gap-2 text-xs">
                  <span className="font-mono">{tool.name}</span>
                  <Pill size="sm" tone="slate">{tool.rerun || t("settings.mcp.tool.unclassified")}</Pill>
                  <Pill size="sm" tone="slate">{tool.effect || t("settings.mcp.tool.unclassified")}</Pill>
                  {!tool.classified && <Pill size="sm" tone="amber" data-testid="drawer-unclassified">{t("settings.mcp.tool.refusedUntilClassified")}</Pill>}
                </li>
              ))}
            </ul>
          </section>

          <section className="space-y-3">
            <h3 className={H}>{t("settings.mcp.drawer.who")}</h3>
            {server.tier === "public" && <AccessPanel name={name} shared={(server as { shared?: boolean }).shared === true} canWrite={canWrite} onChanged={() => { detail.refetch(); onChanged(); }} />}
            <div className="space-y-1">
              <span className="text-xs opacity-60">{t("settings.mcp.drawer.declaredBy")}</span>
              <div className="flex flex-wrap gap-1" data-testid="drawer-declaredBy">
                {(detail.data.declaredBy ?? []).length === 0 ? (
                  <span className="text-xs opacity-70">{t("settings.mcp.drawer.noDeclarations")}</span>
                ) : (
                  (detail.data.declaredBy ?? []).map((agent) => <Pill key={agent} size="sm" tone="violet">{agent}</Pill>)
                )}
              </div>
            </div>
          </section>

          <section className="space-y-3" data-testid="drawer-changes">
            <h3 className={H}>{t("settings.mcp.drawer.audit")}</h3>
            <ul className="space-y-3 text-sm">
              {(detail.data.changes ?? []).slice(0, 8).map((row) => (
                <li key={row.id} className="border-l-2 border-white/15 pl-3">
                  <div>{row.summary}</div>
                  <div className="text-xs opacity-60"><time dateTime={row.occurredAt}>{row.occurredAt}</time> · {row.actorKind}/{row.actorId} · {row.op}</div>
                </li>
              ))}
            </ul>
          </section>
        </>
      )}
      {modal}
    </div>
  );
}
