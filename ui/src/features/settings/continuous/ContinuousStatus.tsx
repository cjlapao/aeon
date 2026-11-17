import { useI18n } from "@cjlapao/ui-kit";
import { CollapsiblePanel, Pill } from "../../../components/kit";
import { masterTone, type SchedulerStatus } from "./continuousModel";

/**
 * ContinuousStatus — the one-glance answer to "is it running, and is it busy?" (S27 T10).
 *
 * A strip of four plain facts — the master switch, the slots in use, the next tick and how many
 * lines need a person — and, folded under it, the settings the scheduler reads. All of it is the
 * daemon's `scheduler.status` answer: nothing is computed here, and the settings are shown as the
 * daemon sent them (`key = value (source)`), never edited from this page.
 */
export function ContinuousStatus({ view }: { view: SchedulerStatus }) {
  const { t } = useI18n();
  const state = !view.enabled ? "off" : view.paused ? "paused" : "on";
  return (
    <div className="shrink-0 space-y-3">
      <div className="flex flex-wrap items-center gap-x-6 gap-y-2 text-sm" data-testid="continuous-header">
        <span className="flex items-center gap-2" data-testid="continuous-master">
          <span className="opacity-70">{t("settings.continuous.master")}</span>
          <Pill tone={masterTone(view.enabled, view.paused)}>{t(`settings.continuous.state.${state}`)}</Pill>
          {view.s20Paused && (
            <span data-testid="continuous-s20-paused"><Pill tone="amber">{t("settings.continuous.s20Paused")}</Pill></span>
          )}
        </span>
        <span data-testid="continuous-slots">{t("settings.continuous.slots", { used: view.slots.used, cap: view.slots.cap })}</span>
        <span data-testid="continuous-next-tick">{t("settings.continuous.nextTick", { at: view.nextTickAt ?? "" })}</span>
        <span data-testid="continuous-attention-count">{t("settings.continuous.attentionCount", { open: view.attention?.open ?? 0 })}</span>
      </div>
      <p className="text-sm opacity-80" data-testid="continuous-state-line">{t(`settings.continuous.stateLine.${state}`)}</p>
      {view.settings.length > 0 && (
        <CollapsiblePanel
          tone="slate"
          data-testid="continuous-settings"
          title={t("settings.continuous.settings.title")}
          subtitle={t("settings.continuous.settings.subtitle")}
          defaultExpanded={false}
        >
          <ul className="space-y-1 text-xs opacity-80">
            {view.settings.map((s) => (
              <li key={s.key} data-testid={`continuous-setting-${s.key}`}>{s.key} = {s.value} ({s.source})</li>
            ))}
          </ul>
        </CollapsiblePanel>
      )}
    </div>
  );
}
