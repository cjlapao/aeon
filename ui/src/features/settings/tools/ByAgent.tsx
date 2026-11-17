import { useI18n } from "@cjlapao/ui-kit";
import { Alert, EmptyState, Pill } from "../../../components/kit";
import { groupByState, stateBadge, type ByAgentResult } from "./toolsModel";

// One agent's whole tool view, grouped by the state the engine gave, with the engine's sentence on
// every row. The grouping order is the DAEMON's (first appearance wins) and the counts are counted
// from the rows. One state is not a table: an agent the app cannot dispatch (SD7) is stated as such
// and gets no list of grant states — `dispatchable:false` is the daemon's flag; the sentence around
// it is this screen's.

export interface ByAgentProps {
  agent: string;
  result: ByAgentResult | undefined;
  loading: boolean;
  error?: string;
}

const H = "text-xs font-semibold uppercase tracking-widest opacity-70";

export function ByAgent({ agent, result, loading, error }: ByAgentProps) {
  const { t } = useI18n();
  const groups = result ? groupByState(result.tools ?? []) : [];

  return (
    <div className="space-y-8" data-testid="tools-by-agent">
      {!agent ? <EmptyState title={t("settings.tools.byAgent.pick")} subtitle={t("settings.tools.byAgent.pickHint")} /> : null}
      {agent && loading ? <p className="text-sm opacity-70">{t("settings.tools.byAgent.loading")}</p> : null}
      {agent && error ? <Alert intent="danger" title={t("settings.tools.byAgent.failed")} description={error} data-testid="tools-by-agent-error" /> : null}

      {agent && result && !result.dispatchable ? (
        <Alert
          intent="warning"
          title={t("settings.tools.byAgent.notDispatchableTitle", { agent: result.agent })}
          description={t("settings.tools.byAgent.notDispatchableBody")}
          data-testid="tools-by-agent-not-dispatchable"
        />
      ) : null}

      {agent && result && result.dispatchable ? (
        <div className="space-y-8" data-testid="tools-by-agent-groups">
          {groups.length === 0 ? <EmptyState title={t("settings.tools.byAgent.empty")} /> : null}
          {groups.map((group) => {
            const badge = stateBadge(group.state);
            return (
              <section key={group.state} className="space-y-3" data-testid={`tools-by-agent-group-${group.state}`}>
                <h3 className={H}>{group.state} · {t("settings.tools.byAgent.count", { total: group.rows.length })}</h3>
                <ul className="divide-y divide-[var(--border)]">
                  {group.rows.map((row) => (
                    <li key={row.tool} className="space-y-1 py-2 text-sm" data-testid={`tools-by-agent-row-${row.tool}`}>
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="font-semibold">{row.tool}</span>
                        <Pill tone={badge.tone} size="sm">{row.state}</Pill>
                        {(row.granted_by ?? []).map((origin) => <Pill key={origin} tone="slate" size="sm">{origin}</Pill>)}
                      </div>
                      {row.reason ? (
                        <p className="text-xs opacity-80" data-testid={`tools-by-agent-reason-${row.tool}`}>{row.reason}</p>
                      ) : (
                        <p className="text-xs opacity-50">{t("settings.tools.byAgent.noReason")}</p>
                      )}
                    </li>
                  ))}
                </ul>
              </section>
            );
          })}
        </div>
      ) : null}
    </div>
  );
}
