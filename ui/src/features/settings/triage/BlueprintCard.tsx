import { useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, FormField, Pill } from "../../../components/kit";
import { BlueprintPicker } from "../automation/ScheduleForm";
import { refusalBody, toRpcError } from "../../../lib/bridge";
import { notifications } from "../../../lib/notifications";
import type { LiveController, LiveState } from "../../../lib/live";
import { saveTriageSetting, useSettingView, useTriageHealth } from "./triageRpc";
import { VALIDATION_BLUEPRINT_KEY, healthTone, settingHealth, type HealthResult } from "./triageModel";
import type { RpcQueryState } from "../../../lib/useRpcQuery";

/**
 * BlueprintCard — which blueprint answers validation (S51 T7, TM4/TM10):
 * the one declared setting `triage.validationBlueprint`, its S44
 * `settings.get` View (value, source — the door carries them), the S49
 * `BlueprintPicker` over `blueprint.list`, and a save through
 * `settings.set`, the one settings write this panel has.
 *
 * No blueprint NAME is written anywhere in this folder: TM4's law is that
 * a missing or invalid choice is REFUSED BY NAME and never falls back, and
 * a panel that carried a fallback name would be a second, softer truth
 * about what the daemon will do.
 *
 * Beside the row sits the `triage.health` dot: the daemon's STATE WORD as
 * text, never renamed, and the honest line — the dot is advice, never a
 * gate (the same AN8 shape every health dot carries).
 */
export interface BlueprintCardProps {
  controller: LiveController;
  live: LiveState;
  canWrite: boolean;
}

export function BlueprintCard({ controller, live, canWrite }: BlueprintCardProps) {
  const { t } = useI18n();
  const health = useTriageHealth(controller, live);
  return (
    <div className="space-y-4" data-testid="triage-blueprint-card">
      <p className="text-sm opacity-80">{t("settings.triage.blueprint.intro")}</p>
      <BlueprintRow health={health} controller={controller} live={live} canWrite={canWrite} />
      <p className="text-xs opacity-60" data-testid="triage-health-advice">
        {t("settings.triage.blueprint.healthAdvice")}
        {health.data?.evaluatedAt !== undefined && health.data.evaluatedAt !== "" && (
          <span data-testid="triage-evaluated-at"> · {health.data.evaluatedAt}</span>
        )}
      </p>
      {health.error !== undefined && <Alert intent="danger" title={health.error} data-testid="triage-health-error" />}
    </div>
  );
}

function BlueprintRow({
  health,
  controller,
  live,
  canWrite,
}: {
  health: RpcQueryState<HealthResult>;
  controller: LiveController;
  live: LiveState;
  canWrite: boolean;
}) {
  const { t } = useI18n();
  const key = VALIDATION_BLUEPRINT_KEY;
  const view = useSettingView(key, controller, live);
  const setting = view.data?.setting;
  const [picked, setPicked] = useState<string | undefined>(undefined);
  const [busy, setBusy] = useState(false);
  const [answered, setAnswered] = useState<boolean | undefined>(undefined);

  const current = picked ?? setting?.value ?? "";
  const row = settingHealth(health.data, key);

  async function save() {
    setBusy(true);
    setAnswered(undefined);
    try {
      const result = await saveTriageSetting(key, current.trim());
      // `changed:false` is the daemon's honest no-op — said back as such.
      setAnswered((result as { changed?: boolean }).changed === true);
      setPicked(undefined);
      view.refetch();
      health.refetch();
    } catch (cause) {
      // A refusal is a toast and nothing else: the daemon's sentence, verbatim, and no fallback is offered.
      notifications.push({ title: t("settings.triage.drawer.refused"), body: refusalBody(toRpcError(cause)), tone: "danger", source: "local" });
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="space-y-3" data-testid={`triage-setting-${key}`}>
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-xs font-mono opacity-80" data-testid={`triage-setting-key-${key}`}>{key}</span>
        {setting?.source !== undefined && (
          // The source word is the daemon's (default/setting/env states).
          <Pill size="sm" tone="slate" data-testid={`triage-setting-source-${key}`}>{setting.source}</Pill>
        )}
        {row !== undefined && (
          <Pill size="sm" tone={healthTone(row.state)} data-testid="triage-health-dot">
            {/* State word verbatim — the tone is ours, the word is not. */}
            {row.state}
          </Pill>
        )}
      </div>
      {setting?.description !== undefined && <p className="text-xs opacity-70">{setting.description}</p>}
      <FormField label={t("settings.triage.blueprint.label")} hint={t("settings.triage.blueprint.hint")}>
        <BlueprintPicker selected={current} onChange={setPicked} controller={controller} live={live} />
      </FormField>
      <div className="flex items-center gap-2">
        <Button size="sm" disabled={busy || !canWrite || current.trim() === ""} data-testid="triage-setting-save" onClick={() => void save()}>
          {busy ? t("settings.triage.blueprint.saving") : t("settings.triage.blueprint.save")}
        </Button>
        {answered === true && <p className="text-xs opacity-70" data-testid="triage-setting-saved">{t("settings.triage.blueprint.saved")}</p>}
        {answered === false && <p className="text-xs opacity-70" data-testid="triage-setting-nochange">{t("settings.triage.blueprint.noChange")}</p>}
      </div>
    </div>
  );
}
