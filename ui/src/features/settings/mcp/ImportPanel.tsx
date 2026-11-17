import { useCallback, useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, EmptyState, FormField, Input, Pill } from "../../../components/kit";
import { rpcTyped } from "../../../lib/bridge";
import { useRpcAction } from "../../../lib/useRpcAction";
import { useTypeToConfirm } from "../../../lib/useConfirm";
import type { ImportAnswer } from "./mcpModel";

/**
 * The Import tab (MG3's door): the legacy file is dry-run FIRST — the
 * diff shows server/tool/declaration NAMES and env NAMES only (the daemon
 * sends names; a refused adoption shows the daemon's sentence); Replace is
 * a typed confirm that names the counts and says, in the person's own
 * language, that it replaces ALL MCP configuration.
 */
export interface ImportPanelProps {
  canWrite: boolean;
  onImported: () => void;
}

export function ImportPanel({ canWrite, onImported }: ImportPanelProps) {
  const { t } = useI18n();
  const action = useRpcAction();
  const { confirm, modal } = useTypeToConfirm();
  const [path, setPath] = useState("");
  const [preview, setPreview] = useState<ImportAnswer | undefined>(undefined);

  const dryRun = useCallback(async () => {
    setPreview(undefined);
    const result = await action.run(
      () => rpcTyped("mcp.importFile", path === "" ? { mode: "dry-run" } : { mode: "dry-run", path }),
      { errorTitle: t("settings.mcp.import.dryFailed") },
    );
    if (result) setPreview(result);
  }, [action, path, t]);

  const replace = useCallback(async () => {
    if (!preview) return;
    const confirmed = await confirm({
      title: t("settings.mcp.import.replaceTitle"),
      // The confirm NAMES the counts and says what replace does (MG3).
      description: t("settings.mcp.import.replaceBody", {
        servers: preview.servers,
        tools: preview.tools,
        declares: preview.declares,
      }),
      confirmValue: "replace",
      confirmValueLabel: t("settings.mcp.import.confirmLabel"),
      confirmLabel: t("settings.mcp.import.replaceButton"),
    });
    if (!confirmed) return;
    const result = await action.run(
      () => rpcTyped("mcp.importFile", path === "" ? { mode: "replace" } : { mode: "replace", path }),
      { errorTitle: t("settings.mcp.import.replaceFailed") },
    );
    if (result) {
      setPreview(result);
      onImported();
    }
  }, [action, confirm, onImported, path, preview, t]);

  const refused = preview !== undefined && preview.outcome === "refused";

  return (
    <div className="space-y-8" data-testid="import-panel">
      <p className="text-sm opacity-80">{t("settings.mcp.import.subtitle")}</p>
      <section className="space-y-3">
        <h3 className="text-xs font-semibold uppercase tracking-widest opacity-70">{t("settings.mcp.import.which")}</h3>
        {action.error && <Alert intent="danger" title={action.error} />}
        <FormField label={t("settings.mcp.import.path")}>
          <Input
            variant="ghost"
            aria-label={t("settings.mcp.import.path")}
            value={path}
            disabled={!canWrite || action.busy}
            placeholder={t("settings.mcp.import.pathHint")}
            onChange={(event) => setPath(event.target.value)}
          />
        </FormField>
        <div className="flex flex-wrap gap-2">
          <Button size="sm" disabled={!canWrite || action.busy} onClick={() => void dryRun()} title={!canWrite ? t("settings.mcp.reconnecting") : undefined} data-testid="import-dry">
            {t("settings.mcp.import.dryRun")}
          </Button>
          <Button size="sm" variant="ghost" disabled={!canWrite || action.busy || preview === undefined} onClick={() => void replace()} data-testid="import-replace">
            {t("settings.mcp.import.replace")}
          </Button>
        </div>
      </section>
      <section className="space-y-3">
        <h3 className="text-xs font-semibold uppercase tracking-widest opacity-70">{t("settings.mcp.import.what")}</h3>
        {preview === undefined && !action.busy ? (
          <EmptyState title={t("settings.mcp.import.noPreview")} />
        ) : preview ? (
          <div className="space-y-2" data-testid="import-preview">
            <div className="flex flex-wrap items-center gap-2 text-sm">
              <Pill tone={refused ? "rose" : preview.mode === "dry-run" ? "slate" : "green"}>{preview.outcome}</Pill>
              <span className="font-mono text-xs opacity-70">{preview.path}</span>
              <span className="text-xs">
                {t("settings.mcp.import.counts", { servers: preview.servers, tools: preview.tools, declares: preview.declares })}
              </span>
            </div>
            {refused && (
              // A refused adoption is SHOWN with the daemon's OWN sentence
              // (the result's `detail` — the wire carries it, the generated
              // contract flattened the omitempty field away, so one honest
              // read at the edge). A silent empty config is the failure MG3
              // refuses, so this Alert is never empty-styled.
              <div data-testid="import-refused"><Alert intent="warning" title={(preview as { detail?: string }).detail ?? t("settings.mcp.import.refused")} className="whitespace-pre-line" /></div>
            )}
            <div className="space-y-1 text-xs" data-testid="import-diff">
              {(preview as { diff?: ImportAnswer["diff"] }).diff && (
                <>
                  <DiffLine label={t("settings.mcp.import.added")} names={preview.diff.serversAdded} />
                  <DiffLine label={t("settings.mcp.import.removed")} names={preview.diff.serversRemoved as string[]} />
                  <DiffLine label={t("settings.mcp.import.toolsAdded")} names={preview.diff.toolsAdded} />
                  <DiffLine label={t("settings.mcp.import.toolsRemoved")} names={preview.diff.toolsRemoved as string[]} />
                  <DiffLine label={t("settings.mcp.import.declaresAdded")} names={preview.diff.declaresAdded} />
                  <DiffLine label={t("settings.mcp.import.declaresRemoved")} names={preview.diff.declaresRemoved as string[]} />
                  <DiffLine label={t("settings.mcp.import.envNames")} names={preview.diff.envNames} />
                </>
              )}
            </div>
          </div>
        ) : null}
      </section>
      {modal}
    </div>
  );
}

function DiffLine({ label, names }: { label: string; names: string[] | undefined }) {
  if (!names || names.length === 0) return null;
  return (
    <p className="flex flex-wrap items-center gap-1">
      <span className="uppercase tracking-wide opacity-70">{label}:</span>
      {names.map((name) => (
        <Pill key={name} tone="slate">
          {name}
        </Pill>
      ))}
    </p>
  );
}
