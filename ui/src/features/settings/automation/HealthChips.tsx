import { useI18n } from "@cjlapao/ui-kit";
import { Pill } from "../../../components/kit";
import { describeHealth, type HealthView } from "./automationModel";

/**
 * HealthChips — what the daemon's health answer says about one automation,
 * as chips (AN11: the daemon answers `canFire`, `reasons` and each named
 * check; this only picks the colour).
 *
 * The webhook chip renders the **env NAME** and set/unset — there is no
 * value on the wire and never will be (§4c/C17): the type carries `envName`
 * and `set`, so leaking is not a forgotten call site, it is unrepresentable.
 * `reasons` are quoted verbatim; nothing here rewrites one.
 */
export function HealthChips({ health, showReasons = true }: { health: HealthView | undefined; showReasons?: boolean }) {
  const { t } = useI18n();
  const chips = describeHealth(health);
  if (chips.length === 0) return null;
  return (
    <div className="flex flex-wrap items-center gap-1.5" data-testid="health-chips">
      {chips.map((chip) => (
        <Pill key={chip.key} tone={chip.tone} title={`${chip.key}: ${chip.word}`}>
          {t(`settings.automation.health.${chip.key}`)}: {chip.word}
        </Pill>
      ))}
      {health?.canFire === false && (
        <Pill tone="red" data-testid="health-cannot-fire">{t("settings.automation.health.cannotFire")}</Pill>
      )}
      {showReasons && (health?.reasons?.length ?? 0) > 0 && (
        <ul className="w-full space-y-0.5 pt-1" data-testid="health-reasons">
          {(health?.reasons ?? []).map((reason, index) => (
            // Verbatim daemon text — the one sentence per reason the health
            // door composed; not a paraphrase, not a second opinion.
            <li key={index} className="text-xs opacity-80">{reason}</li>
          ))}
        </ul>
      )}
    </div>
  );
}
