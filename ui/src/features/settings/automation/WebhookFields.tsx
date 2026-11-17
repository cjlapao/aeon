import { useI18n } from "@cjlapao/ui-kit";
import { FormField, Input, Select } from "../../../components/kit";
import type { ScheduleDraft } from "./automationModel";

/** The ready-made listeners. A preset sets the event filter and, for an empty (or another preset's) goal, a goal that names the issue. */
export const WEBHOOK_PRESETS = [
  { id: "github-issue", filter: "issues:opened", goal: "Investigate GitHub issue #{issue.number}: {issue.title}", key: "presetGithubIssue" },
  { id: "github-pr", filter: "pull_request:opened", goal: "Review GitHub pull request #{pull_request.number}: {pull_request.title}", key: "presetGithubPr" },
  { id: "any", filter: "", goal: "", key: "presetAny" },
] as const;

const CUSTOM = "custom";
/** Which preset a filter is, or custom when it is none of them. */
export const presetOf = (filter: string): string => WEBHOOK_PRESETS.find((p) => p.filter === filter.trim())?.id ?? CUSTOM;

/** What to give GitHub: the payload address. The host is theirs to fill in; the daemon only knows its local socket. */
export const payloadPath = (id: string): string => `/v1/webhooks/${id.trim() || "<id>"}`;

/**
 * The webhook half of the form: which deliveries start it, how it is authenticated, and what to give the sender. A webhook
 * has no clock, so there is no timezone and no window here (the daemon refuses a webhook that carries either).
 */
export function WebhookFields({ draft, set, secretOk, headingClass }: { draft: ScheduleDraft; set: (patch: Partial<ScheduleDraft>) => void; secretOk: boolean; headingClass: string }) {
  const { t } = useI18n();
  const preset = presetOf(draft.eventFilter);
  const known = new Set<string>(WEBHOOK_PRESETS.map((p) => p.goal));
  const choose = (id: string) => {
    const p = WEBHOOK_PRESETS.find((x) => x.id === id);
    if (!p) { set({ eventFilter: draft.eventFilter || "issues" }); return; }   // custom: start from something editable
    // A goal the person wrote stays; an empty one, or another preset's, is replaced.
    set({ eventFilter: p.filter, ...(draft.goal === "" || known.has(draft.goal) ? { goal: p.goal } : {}) });
  };
  return (
    <>
      <section className="space-y-3" data-testid="webhook-listen">
        <h3 className={headingClass}>{t("settings.automation.webhook.listenTitle")}</h3>
        <FormField label={t("settings.automation.webhook.preset")}>
          <Select variant="ghost" aria-label={t("settings.automation.webhook.preset")} value={preset} onChange={(e) => choose(e.target.value)} data-testid="webhook-preset">
            {WEBHOOK_PRESETS.map((p) => <option key={p.id} value={p.id}>{t(`settings.automation.webhook.${p.key}`)}</option>)}
            <option value={CUSTOM}>{t("settings.automation.webhook.presetCustom")}</option>
          </Select>
        </FormField>
        {preset === CUSTOM && (
          <FormField label={t("settings.automation.webhook.customLabel")}>
            <Input variant="ghost" aria-label={t("settings.automation.webhook.customLabel")} value={draft.eventFilter} placeholder="issues:opened,reopened" onChange={(e) => set({ eventFilter: e.target.value })} data-testid="webhook-filter" />
          </FormField>
        )}
        <p className="text-xs opacity-60">{preset === CUSTOM ? t("settings.automation.webhook.customHint") : t("settings.automation.webhook.ignoredHint")}</p>
      </section>

      <section className="space-y-3" data-testid="secret-env-field">
        <h3 className={headingClass}>{t("settings.automation.webhook.authTitle")}</h3>
        <FormField label={t("settings.automation.form.secretEnv")}>
          <Input variant="ghost" aria-label={t("settings.automation.form.secretEnv")} value={draft.secretEnv} placeholder="AEON_WEBHOOK_SECRET" onChange={(e) => set({ secretEnv: e.target.value })} />
        </FormField>
        {draft.secretEnv !== "" && !secretOk && <p className="text-xs text-rose-500" data-testid="secret-env-refusal">{t("settings.automation.form.secretNameRule")}</p>}
        <p className="text-xs opacity-60">{t("settings.automation.form.secretEnvHint")}</p>
      </section>

      <section className="space-y-2" data-testid="webhook-connect">
        <h3 className={headingClass}>{t("settings.automation.webhook.connectTitle")}</h3>
        <FormField label={t("settings.automation.webhook.connectUrl")}>
          <Input variant="ghost" readOnly aria-label={t("settings.automation.webhook.connectUrl")} value={`https://<your-aeon-host>${payloadPath(draft.id)}`} onFocus={(e) => e.currentTarget.select()} data-testid="webhook-url" />
        </FormField>
        <p className="text-xs opacity-60">{t("settings.automation.webhook.connectSteps")}</p>
      </section>
    </>
  );
}


