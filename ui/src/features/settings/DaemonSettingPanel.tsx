import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, Input, MultiToggle, Pill, Select } from "../../components/kit";
import { RelativeTime } from "../../components/RelativeTime";
import {
  controlKindFor,
  describeLayers,
  hasEnvInvalid,
  hasStoredInvalid,
  isLocked,
  resetDisabled,
  type SettingView,
} from "./daemonSettings";

/**
 * DaemonSettingPanel — one setting opened, as three questions: **what is it set to?**, **where does
 * that value come from?** and **start over?**. Drawn entirely from the daemon's view: the label, the
 * description, the control and the layer all come from `view`, so a newly declared setting appears with
 * zero UI change (CS10). Nothing is optimistic — the panel shows the value the daemon last answered
 * with, and a write only takes effect once the section replaces `view` with the response's `setting`.
 *
 * A switch or an enum commits when chosen; a number or text is a draft the person saves (Enter or the
 * button), checked against the declared type before it is ever sent. A setting the environment controls
 * is read-only and names the variable. A refusal is a toast (the section owns the write).
 */
export interface DaemonSettingPanelProps {
  view: SettingView;
  /** Writes are refused when the connection is not `current` (offline). */
  canWrite: boolean;
  /** A write is in flight — the controls stay disabled. */
  busy: boolean;
  /** For a text/number control, the draft in the input; else ignored. */
  draft: string;
  onDraftChange: (text: string) => void;
  /** Send the draft (text/number) or a chosen value (switch/select). */
  onCommit: (text: string) => void;
  onReset: () => void;
  /** The malformed-draft reason, shown inline; the section never sent it. */
  parseError?: string;
}

const H = "text-xs font-semibold uppercase tracking-widest opacity-70";
const SOURCE_TONE = { environment: "amber", saved: "blue", default: "slate" } as const;

export function DaemonSettingPanel({ view, canWrite, busy, draft, onDraftChange, onCommit, onReset, parseError }: DaemonSettingPanelProps) {
  const { t } = useI18n();
  const kind = controlKindFor(view);
  const locked = isLocked(view);
  const disabled = !canWrite || locked || busy;
  const layer = describeLayers(view);
  const members = view.enumValues ?? [];

  return (
    <div className="space-y-6 pb-6" data-testid="daemon-setting-panel">
      {view.description !== "" && <p className="text-sm opacity-90">{view.description}</p>}

      <section className="space-y-3">
        <h3 className={H}>{t("settings.daemon.q.value")}</h3>
        {kind === "switch" && (
          <MultiToggle
            aria-label={view.key}
            size="sm"
            indicator="tonal"
            disabled={disabled}
            value={view.value}
            onChange={(value) => onCommit(value)}
            options={[
              { value: "true", label: t("settings.daemon.bool.true") },
              { value: "false", label: t("settings.daemon.bool.false") },
            ]}
          />
        )}
        {kind === "select" && members.length <= 4 && (
          <MultiToggle aria-label={view.key} size="sm" indicator="tonal" disabled={disabled} value={view.value} onChange={(value) => onCommit(value)} options={members.map((m) => ({ value: m, label: m }))} />
        )}
        {kind === "select" && members.length > 4 && (
          <Select variant="ghost" size="md" aria-label={view.key} disabled={disabled} value={view.value} onChange={(e) => onCommit(e.target.value)}>
            {members.map((m) => <option key={m} value={m}>{m}</option>)}
          </Select>
        )}
        {(kind === "number" || kind === "text") && (
          <div className="flex items-start gap-2">
            <div className="min-w-0 flex-1">
              <Input
                variant="ghost"
                aria-label={view.key}
                type="text"
                inputMode={kind === "number" ? "numeric" : "text"}
                value={draft}
                disabled={disabled}
                maxLength={kind === "number" ? undefined : (view.max ?? 4096)}
                onChange={(event) => onDraftChange(event.target.value)}
                onKeyDown={(event) => { if (event.key === "Enter") onCommit(draft); }}
                data-testid="daemon-setting-input"
              />
            </div>
            <Button size="sm" disabled={disabled || draft === view.value} onClick={() => onCommit(draft)} data-testid="daemon-setting-save">{t("settings.daemon.save")}</Button>
          </div>
        )}
        {parseError && <p className="text-sm text-amber-600 dark:text-amber-500" data-testid="daemon-setting-parse-error">{parseError}</p>}
        {kind === "number" && (view.min !== undefined || view.max !== undefined) && (
          <p className="text-xs opacity-70">{t("settings.daemon.range", { min: view.min ?? "—", max: view.max ?? "—" })}</p>
        )}
        {view.sensitive && <p className="text-xs opacity-70" data-testid="daemon-setting-sensitive">{t("settings.daemon.sensitiveNote")}</p>}
        {locked && <p className="text-xs opacity-80" data-testid="daemon-setting-lock">{t("settings.daemon.envLock", { var: layer.envVar })}</p>}
        {!canWrite && <Alert intent="warning" title={t("settings.daemon.reconnecting")} />}
      </section>

      <section className="space-y-2">
        <h3 className={H}>{t("settings.daemon.q.source")}</h3>
        <p className="flex flex-wrap items-center gap-2 text-sm">
          <Pill size="sm" tone={SOURCE_TONE[layer.source]}>{t(`settings.daemon.source.${layer.source}`)}</Pill>
          {layer.source === "environment" && <span className="opacity-80">{t("settings.daemon.layer.environment", { var: layer.envVar })}</span>}
          {layer.source === "saved" && (
            <span className="opacity-80">
              {t("settings.daemon.layer.saved", { by: layer.updatedBy || "—" })}{" "}
              {view.updatedAt !== "" && <RelativeTime iso={view.updatedAt} />}
            </span>
          )}
          {layer.source === "default" && <span className="opacity-80">{t("settings.daemon.layer.default")}</span>}
        </p>
        <p className="text-sm opacity-80">{t("settings.daemon.defaultIs")} <code className="text-xs" data-testid="daemon-setting-default">{layer.defaultValue === "" ? "—" : layer.defaultValue}</code></p>
        {layer.shadowed !== null && <p className="text-xs opacity-70">{t("settings.daemon.shadowedSaved", { value: layer.shadowed })}</p>}
        {hasEnvInvalid(view) && <Alert intent="warning" title={t("settings.daemon.alertEnvInvalid", { var: layer.envVar })} />}
        {hasStoredInvalid(view) && <Alert intent="warning" title={t("settings.daemon.alertStoredInvalid", { value: view.stored ?? "" })} />}
      </section>

      <section className="space-y-2">
        <h3 className={H}>{t("settings.daemon.q.reset")}</h3>
        <p className="text-sm opacity-80">{t("settings.daemon.resetBody")}</p>
        <Button size="sm" variant="ghost" onClick={onReset} disabled={resetDisabled(view) || busy || !canWrite} data-testid="daemon-setting-reset">{t("settings.daemon.reset")}</Button>
      </section>
    </div>
  );
}
