import { useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { ActionPanel, Alert, EmptyState, Pill, Select } from "../../../components/kit";
import type { LiveController, LiveState } from "../../../lib/live";
import { useComponentList, useComponentShow, useEnvironmentEffective } from "./environmentsRpc";
import { capabilityChips, downgradeNoteOf, listOf, type Vocabulary } from "./environmentsModel";

/**
 * The Effective tab (S59 T11, EV5/EV9): what a step on a chosen component would
 * actually run in. A component picker over `component.list`, an
 * attended/unattended toggle, and `environment.effective`'s answer — the
 * binding that won and therefore WHY (`scopeKind`: component / root / workspace),
 * the backend and its capabilities, the honesty line verbatim, the downgrade
 * note verbatim when an unattended loop would not inherit, and a table of
 * variable NAMES with their SOURCE (`passthrough`, `set`, `secret:<name>` —
 * sources are the daemon's strings, rendered as sent).
 *
 * No value is rendered, and none can be: the door answers names and sources and
 * the conformance scanner proves it on the wire. A test drops a sentinel into a
 * mocked answer and asserts it never reaches the DOM.
 */
export interface EffectiveTabProps {
  controller: LiveController;
  live: LiveState;
  vocabulary: Vocabulary | undefined;
  /** Start on this component (the Work page's scope). */
  initialComponentId?: string;
}

const H = "text-xs font-semibold uppercase tracking-widest opacity-70";

export function EffectiveTab({ controller, live, vocabulary, initialComponentId = "" }: EffectiveTabProps) {
  const { t } = useI18n();
  const components = useComponentList(controller, live);
  const [componentId, setComponentId] = useState(initialComponentId);

  const rows = listOf(components.data?.components);
  const effective = useEnvironmentEffective({ componentId }, controller, live);
  const detail = useComponentShow(componentId, controller, live);
  const answer = effective.data;
  const note = downgradeNoteOf(answer);

  return (
    <div className="flex flex-col gap-8 pb-6">
      <section className="space-y-3">
        <h3 className={H}>{t("settings.environments.q.which")}</h3>
        <div className="w-72" data-testid="environments-effective-picker-label">
          {/* No attended control (S59 T12, G14): the daemon answers attendedness for the scope and the
              panel renders it below; a checkbox here would let the person choose which inheritance
              note they are shown. */}
          <Select variant="ghost" size="md" aria-label={t("settings.environments.effective.component")} value={componentId} onChange={(event) => setComponentId(event.target.value)} data-testid="environments-effective-picker">
            <option value="">{t("settings.environments.effective.workspace")}</option>
            {rows.map((component) => <option key={component.id} value={component.id}>{component.name}</option>)}
          </Select>
        </div>
      </section>

      {effective.error && <Alert intent="danger" title={t("settings.environments.effective.failed")} description={effective.error} data-testid="environments-effective-failed" />}

      {effective.loading && !answer && (
        <EmptyState showIcon={false} title={t("settings.environments.effective.emptyTitle")} subtitle={t("settings.environments.effective.emptyBody")} data-testid="environments-effective-empty" />
      )}

      {answer && (
        <div className="flex flex-col gap-8" data-testid="environments-effective-answer">
          <section className="space-y-3">
            <h3 className={H}>{t("settings.environments.q.got")}</h3>
            <div className="flex flex-wrap items-center gap-2 text-sm">
              <span className="font-semibold" data-testid="environments-effective-environment">{answer.environmentSlug}</span>
              <Pill size="sm" tone="slate" data-testid="environments-effective-backend">{answer.backend}</Pill>
              {answer.legacy === true && <Pill size="sm" tone="amber" data-testid="environments-effective-legacy">{t("settings.environments.row.legacy")}</Pill>}
              <Pill size="sm" tone="slate" data-testid="environments-effective-scope">{t("settings.environments.effective.why", { scope: answer.scopeKind })}</Pill>
              <span className="text-xs opacity-70" data-testid="environments-effective-attended-state">{answer.attended ? t("settings.environments.effective.attendedYes") : t("settings.environments.effective.attendedNo")}</span>
            </div>
            {componentId !== "" && detail.data?.root && <p className="font-mono text-xs opacity-70" data-testid="environments-effective-root">{detail.data.root}</p>}
            {note !== "" && (
              <ActionPanel tone="amber" size="sm" data-testid="environments-effective-note" title={t("settings.environments.effective.downgrade")} description={<span className="whitespace-pre-line">{note}</span>} />
            )}
          </section>

          <section className="space-y-3">
            <h3 className={H}>{t("settings.environments.q.enforces")}</h3>
            <div className="flex flex-wrap gap-1" data-testid="environments-effective-chips">
              {capabilityChips(vocabulary, answer.backend).map((chip) => (
                <span key={chip.capability} className="rounded-full border border-[var(--border)] px-2 py-0.5 text-[11px]" data-testid={`environments-effective-chip-${chip.capability}`} data-enforced={chip.enforced ? "true" : "false"}>
                  {chip.enforced ? "✓" : "✗"} {chip.capability}
                </span>
              ))}
            </div>
            <p className="text-xs italic opacity-80" data-testid="environments-effective-honesty">{answer.honesty}</p>
          </section>

          <section className="space-y-3">
            <h3 className={H}>{t("settings.environments.q.names")}</h3>
            <table className="w-full text-left text-xs" data-testid="environments-effective-variables">
              <thead>
                <tr className="opacity-70">
                  <th className="py-1 font-normal">{t("settings.environments.effective.name")}</th>
                  <th className="py-1 font-normal">{t("settings.environments.effective.source")}</th>
                </tr>
              </thead>
              <tbody>
                {listOf(answer.variables).map((variable) => (
                  <tr key={variable.name} className="border-t border-[var(--border)]" data-testid={`environments-effective-variable-${variable.name}`}>
                    <td className="py-1 font-mono">{variable.name}</td>
                    <td className="py-1 opacity-80">{variable.source}</td>
                  </tr>
                ))}
              </tbody>
            </table>
            {listOf(answer.secretRefNames).length > 0 && (
              <p className="text-xs opacity-80" data-testid="environments-effective-secrets">{t("settings.environments.effective.references")}: {listOf(answer.secretRefNames).join(", ")}</p>
            )}
          </section>
        </div>
      )}
    </div>
  );
}
