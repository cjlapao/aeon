import { useI18n } from "@cjlapao/ui-kit";
import { EmptyState, Panel } from "../../../components/kit";
import { ErrorPanel } from "../../../components/ErrorPanel";
import { RpcError } from "../../../lib/bridge";
import { useRpcQuery } from "../../../lib/useRpcQuery";
import type { LiveController, LiveState } from "../../../lib/live";
import { describeUsage } from "../blueprintsModel";

// UsagePanel.tsx — what leans on this blueprint.
//
// `blueprint.usage` is the daemon's join across the derived `blueprint_ref`
// rows, the live schedules, triggers and queue, and the count of loops ever
// started from the row. Nothing here searches for references: the panel draws
// the answer T7 computed, in groups, and says plainly when nothing references
// the blueprint at all. That answer is also what makes a delete refusal
// readable — the same references the daemon names when it refuses.

export interface UsagePanelProps {
  name: string;
  controller: LiveController;
  live: LiveState;
}

/** The group headings, spelled out as literals so the i18n guard can see every
 *  one of them in the source (`blueprintsRpc.guard.test.ts`): a key assembled
 *  from a template would be invisible to it, and a missing translation is
 *  exactly the failure that guard exists to catch. */
const GROUP_KEYS = {
  schedules: "blueprints.usage.schedules",
  triggers: "blueprints.usage.triggers",
  queueItems: "blueprints.usage.queueItems",
  agents: "blueprints.usage.agents",
  actions: "blueprints.usage.actions",
  skills: "blueprints.usage.skills",
} as const;

export function UsagePanel({ name, controller, live }: UsagePanelProps) {
  const { t } = useI18n();
  const query = useRpcQuery("blueprint.usage", { name }, controller, live);

  if (query.loading && !query.data) {
    return (
      <Panel title={t("blueprints.usage.title")}>
        <EmptyState title={t("blueprints.usage.loading")} />
      </Panel>
    );
  }
  if (query.error) {
    return (
      <Panel title={t("blueprints.usage.title")}>
        <ErrorPanel error={new RpcError(0, query.error)} onRetry={query.refetch} />
      </Panel>
    );
  }
  const report = query.data ? describeUsage(query.data) : undefined;

  return (
    <Panel title={t("blueprints.usage.title")} subtitle={t("blueprints.usage.subtitle")} data-testid="usage-panel">
      {!report || report.empty ? (
        <EmptyState title={t("blueprints.usage.empty")} />
      ) : (
        <div className="space-y-3">
          {report.loopsStarted > 0 && (
            <div data-testid="usage-loops-started" className="text-sm text-slate-300">
              {t("blueprints.usage.loopsStarted", { count: report.loopsStarted })}
            </div>
          )}
          <dl className="space-y-3">
            {report.groups.map((group) => (
              <div key={group.key} data-testid={`usage-group-${group.key}`}>
                <dt className="text-xs uppercase tracking-wide text-slate-400">
                  {t(GROUP_KEYS[group.key], { count: group.count })}
                </dt>
                <dd className="mt-1 flex flex-wrap gap-1.5">
                  {group.items.map((item) => (
                    <span key={item} className="rounded-md border border-white/10 bg-white/5 px-2 py-0.5 text-xs text-slate-200">
                      {item}
                    </span>
                  ))}
                </dd>
              </div>
            ))}
          </dl>
        </div>
      )}
    </Panel>
  );
}
