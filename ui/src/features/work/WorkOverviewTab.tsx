import { Link } from "react-router-dom";
import { useI18n } from "@cjlapao/ui-kit";
import { Pill, StatCard } from "../../components/kit";
import { ComponentIcon } from "../components/ComponentIcon";
import { NEEDS_YOU_STATUSES, RUNNING_STATUSES, workPath, type TreeNode, type WorkComponent, type WorkLoop } from "./workModel";

/**
 * Overview — the answer to "what is going on here?": four numbers, what is inside, and the latest
 * loops. Every figure is counted from the rows the daemon sent; nothing is estimated.
 */
export function WorkOverviewTab({ scopeLabel, loops, children, components }: {
  scopeLabel: string;
  loops: readonly WorkLoop[];
  children: readonly TreeNode[];
  components: readonly WorkComponent[];
}) {
  const { t } = useI18n();
  const running = loops.filter((loop) => RUNNING_STATUSES.includes(loop.status)).length;
  const needsYou = loops.filter((loop) => NEEDS_YOU_STATUSES.includes(loop.status)).length;
  const recent = loops.slice().sort((a, b) => Date.parse(b.updatedAt) - Date.parse(a.updatedAt)).slice(0, 6);
  return (
    <div className="flex flex-col gap-5" data-testid="work-overview" aria-label={scopeLabel}>
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <StatCard variant="simple" size="sm" padding="sm" icon="Rocket" label={t("work.stat.running")} value={String(running)} subtitle={t("work.stat.runningSub")} data-testid="work-stat-running" />
        <StatCard variant="simple" size="sm" padding="sm" icon="Warning" label={t("work.stat.needsYou")} value={String(needsYou)} subtitle={t("work.stat.needsYouSub")} data-testid="work-stat-needs" />
        <StatCard variant="simple" size="sm" padding="sm" icon="Folder" label={t("work.stat.components")} value={String(children.length)} subtitle={t("work.stat.componentsSub")} data-testid="work-stat-components" />
        <StatCard variant="simple" size="sm" padding="sm" icon="Snapshot" label={t("work.stat.loops")} value={String(loops.length)} subtitle={t("work.stat.loopsSub")} data-testid="work-stat-loops" />
      </div>
      <div className="grid min-w-0 gap-5 lg:grid-cols-2">
        <section className="min-w-0" aria-label={t("work.overview.inside")}>
          <h2 className="mb-2 text-sm font-semibold uppercase tracking-widest opacity-70">{components.length > 0 && children.length > 0 && children[0]!.depth === 0 ? t("work.overview.insideAll") : t("work.overview.inside")}</h2>
          {children.length === 0 ? <p className="text-sm opacity-70" data-testid="work-inside-empty">{t("work.overview.insideEmpty")}</p> : (
            <ul className="divide-y divide-slate-300/20 dark:divide-white/10" data-testid="work-inside">
              {children.map((node) => (
                <li key={node.component.id}>
                  <Link to={workPath(node.component.id)} className="flex items-center gap-3 py-2.5">
                    <ComponentIcon icon={node.component.icon || ""} name={node.component.name} slug={node.component.slug || node.component.id} />
                    <span className="min-w-0 flex-1"><span className="block truncate font-medium">{node.component.name}</span>
                      <span className="block truncate text-xs opacity-60">{node.component.shortDescription || node.component.root}</span></span>
                  </Link>
                </li>
              ))}
            </ul>
          )}
        </section>
        <section className="min-w-0" aria-label={t("work.overview.recent")}>
          <h2 className="mb-2 text-sm font-semibold uppercase tracking-widest opacity-70">{t("work.overview.recent")}</h2>
          {recent.length === 0 ? <p className="text-sm opacity-70" data-testid="work-recent-empty">{t("work.overview.recentEmpty")}</p> : (
            <ul className="divide-y divide-slate-300/20 dark:divide-white/10" data-testid="work-recent">
              {recent.map((loop) => (
                <li key={loop.id}>
                  <Link to={`/loops/${encodeURIComponent(loop.id)}`} className="flex items-center gap-3 py-2.5">
                    <span className="min-w-0 flex-1 truncate">{loop.title || loop.slug}</span>
                    <Pill size="sm" tone={loop.status === "blocked" ? "red" : loop.status === "waiting" ? "amber" : "emerald"}>{loop.status}</Pill>
                  </Link>
                </li>
              ))}
            </ul>
          )}
        </section>
      </div>
    </div>
  );
}
