import { useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, FormField, Panel, Pill } from "../../components/kit";
import { BlueprintPicker } from "../settings/automation/ScheduleForm";
import { toRpcError } from "../../lib/bridge";
import type { LiveController, LiveState } from "../../lib/live";
import { saveRoadmapSetting, useRoadmapHealth, useSettingView } from "./roadmapRpc";
import { DISPATCH_SETTING_KEYS, healthTone, settingHealth, type HealthResult } from "./roadmapModel";
import type { RpcQueryState } from "../../lib/useRpcQuery";

/**
 * DispatchSettings — which blueprint answers each dispatch setting (T7,
 * RM8's "the defaults the daemon reads"). Two rows, one per declared key
 * (`roadmap.ideationBlueprint`, `roadmap.planBlueprint`): the daemon's own
 * `settings.get` View (value, source, stored/invalid states — S44's door
 * carries them), the S49 `BlueprintPicker` over `blueprint.list`, and a
 * save through `settings.set`, the one settings write the app has.
 *
 * Next to each row sits that setting's `roadmap.health` dot: the daemon's
 * STATE WORD as text (`ok`, `missing`, `invalid`, `unverifiable` — never
 * renamed), the report counts when the answer carried them, and when the
 * sweep ran. And the honest line the plane insists on: this dot is advice
 * (AN8's shape) — a dispatch re-checks at its own moment, and a green dot
 * promises nothing more than "the sweep saw it fine".
 */
export interface DispatchSettingsProps {
  controller: LiveController;
  live: LiveState;
  canWrite: boolean;
}

function SettingRow({
  index,
  health,
  controller,
  live,
  canWrite,
}: {
  index: number;
  health: RpcQueryState<HealthResult>;
  controller: LiveController;
  live: LiveState;
  canWrite: boolean;
}) {
  const { t } = useI18n();
  const key = DISPATCH_SETTING_KEYS[index]!;
  const view = useSettingView(key, controller, live);
  const setting = view.data?.setting;
  const [picked, setPicked] = useState<string | undefined>(undefined);
  const [busy, setBusy] = useState(false);
  const [refusal, setRefusal] = useState<string | undefined>(undefined);
  const [answered, setAnswered] = useState<boolean | undefined>(undefined);

  const current = picked ?? setting?.value ?? "";
  const row = settingHealth(health.data, key);

  async function save() {
    setBusy(true);
    setRefusal(undefined);
    setAnswered(undefined);
    try {
      const result = await saveRoadmapSetting(key, current.trim());
      // `changed:false` is the daemon's honest no-op — said back as such.
      setAnswered(result.changed === true);
      setPicked(undefined);
      view.refetch();
      health.refetch();
    } catch (cause) {
      setRefusal(toRpcError(cause).message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="space-y-2 rounded-lg border border-slate-200 p-3 dark:border-slate-700" data-testid={`dispatch-setting-${key}`}>
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-xs font-mono opacity-80" data-testid={`dispatch-setting-key-${key}`}>{key}</span>
        {setting?.source !== undefined && (
          // The source word is the daemon's (default/setting/env states).
          <Pill tone="slate" data-testid={`dispatch-setting-source-${key}`}>{setting.source}</Pill>
        )}
        {row !== undefined && (
          <Pill tone={healthTone(row.state)} data-testid={`dispatch-health-${key}`}>
            {/* State word verbatim — the tone is ours, the word is not. */}
            {row.state}
          </Pill>
        )}
        {row?.reportCounts !== undefined && (
          <span className="text-[11px] opacity-70" data-testid={`dispatch-counts-${key}`}>
            {row.reportCounts.valid}/{row.reportCounts.invalid}/{row.reportCounts.unverifiable}
          </span>
        )}
      </div>
      {setting?.description !== undefined && <p className="text-xs opacity-70">{setting.description}</p>}
      <FormField label={t("roadmap.manage.dispatch.blueprint")} hint={t("roadmap.manage.dispatch.blueprintHint")}>
        <BlueprintPicker selected={current} onChange={setPicked} controller={controller} live={live} />
      </FormField>
      <div className="flex items-center gap-2">
        <Button size="sm" disabled={busy || !canWrite || current.trim() === ""} data-testid={`dispatch-save-${key}`} onClick={() => void save()}>
          {busy ? t("roadmap.manage.dispatch.saving") : t("roadmap.manage.dispatch.save")}
        </Button>
        {answered === true && <p className="text-xs opacity-70" data-testid="dispatch-saved">{t("roadmap.manage.dispatch.saved")}</p>}
        {answered === false && <p className="text-xs opacity-70" data-testid="dispatch-nochange">{t("roadmap.manage.dispatch.noChange")}</p>}
      </div>
      {refusal !== undefined && <Alert intent="danger" title={refusal} data-testid={`dispatch-refusal-${key}`} />}
    </div>
  );
}

export function DispatchSettings({ controller, live, canWrite }: DispatchSettingsProps) {
  const { t } = useI18n();
  const health = useRoadmapHealth(controller, live);
  return (
    <Panel tone="slate" data-testid="dispatch-settings">
      <p className="text-xs opacity-70 pb-2">{t("roadmap.manage.dispatch.intro")}</p>
      {DISPATCH_SETTING_KEYS.map((key, index) => (
        <div key={key} className="pb-2">
          <SettingRow index={index} health={health} controller={controller} live={live} canWrite={canWrite} />
        </div>
      ))}
      <p className="text-[11px] opacity-60" data-testid="dispatch-health-advice">
        {t("roadmap.manage.dispatch.healthAdvice")}
        {health.data?.evaluatedAt !== undefined && health.data.evaluatedAt !== "" && (
          <span data-testid="dispatch-evaluated-at"> · {health.data.evaluatedAt}</span>
        )}
      </p>
      {health.error !== undefined && <Alert intent="danger" title={health.error} data-testid="dispatch-health-error" />}
    </Panel>
  );
}
