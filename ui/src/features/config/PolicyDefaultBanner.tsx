import { useI18n } from "@cjlapao/ui-kit";
import { Alert } from "../../components/kit";
import type { LiveController, LiveState } from "../../lib/live";
import { useRpcQuery } from "../../lib/useRpcQuery";

/**
 * The line above the Policy page's rules saying what happens to a call NO rule covers. That
 * default (`permissions.unmatched`) is not a rule, so it never shows in the table; this banner is where a
 * person learns that an agent without rules asks, and where to change it. When the daemon cannot say, the
 * banner is left out rather than guessed.
 */
export function PolicyDefaultBanner({ controller, live }: { controller: LiveController; live: LiveState }) {
  const { t } = useI18n();
  const setting = useRpcQuery("settings.get", { key: "permissions.unmatched" }, controller, live);
  const value = setting.error === undefined ? setting.data?.setting?.value : undefined;
  if (value === undefined) return null;
  return (
    <Alert
      className="shrink-0"
      intent={value === "ask" ? "info" : "warning"}
      title={value === "ask" ? t("settings.policy.default.ask") : t("settings.policy.default.deny")}
      description={t("settings.policy.default.hint")}
      data-testid="policy-default"
    />
  );
}
