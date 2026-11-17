import { useMemo, useState } from "react";
import { Gantt, useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, Tabs } from "../../components/kit";
import { OverlaySidePanel } from "../../components/OverlaySidePanel";
import type { LiveController, LiveState } from "../../lib/live";
import { useLiveSubscription } from "../../lib/useLiveSubscription";
import { useRpcQuery } from "../../lib/useRpcQuery";
import { DispatchSettings } from "../roadmap/DispatchSettings";
import { ItemDrawer } from "../roadmap/ItemDrawer";
import { ProposalsStrip } from "../roadmap/RoadmapPage";
import { useRoadmapVocabulary } from "../roadmap/roadmapRpc";
import type { ItemView } from "../roadmap/roadmapModel";
import { lanesOf, toBars, toLinks } from "./planModel";

type Axis = "epic" | "priority";

/**
 * Roadmap — the roadmap on the kit's Gantt. Lanes are epics (or priority), bar colour is the item's
 * status, a badge marks items that are not estimated or not started. A bar opens the item in the
 * side panel, where it is edited and where ideate, plan and materialize live behind their confirms.
 * Roadmaps are per component, so "all projects" asks for a pick.
 */
export function WorkRoadmapView({ controller, live, canWrite, root }: { controller: LiveController; live: LiveState; canWrite: boolean; root: string | undefined }) {
  const { t } = useI18n();
  const [axis, setAxis] = useState<Axis>("epic");
  const [openId, setOpenId] = useState<number | null>(null);
  const [settings, setSettings] = useState(false);
  const query = useRpcQuery("roadmap.list", { componentRoot: root ?? "" }, controller, live);
  const vocabulary = useRoadmapVocabulary(controller, live);
  useLiveSubscription(live, ["roadmap.*", "loop.*"], () => { void query.refetch(); });
  const items = useMemo(() => (root ? ((query.data as { items?: ItemView[] } | undefined)?.items ?? []) : []), [query.data, root]);
  const bars = useMemo(() => toBars(items, Date.now(), t("work.timeline.noEpic"), t("work.timeline.notEstimated"), axis), [items, t, axis]);
  const lanes = useMemo(() => lanesOf(bars), [bars]);
  const links = useMemo(() => toLinks(items), [items]);

  if (!root) return <p className="text-sm opacity-70" data-testid="work-timeline-needs-one">{t("work.timeline.needsOne")}</p>;
  return (
    <div className="flex min-h-[28rem] flex-col gap-3" data-testid="work-timeline">
      <div className="flex flex-wrap items-center gap-3">
        <Tabs variant="pill" items={[{ id: "epic", label: t("work.timeline.byEpic") }, { id: "priority", label: t("work.timeline.byPriority") }]} value={axis} onChange={(id) => setAxis(id as Axis)} />
        <p className="flex-1 text-sm opacity-70">{t("work.timeline.hint")}</p>
        <Button size="sm" variant="ghost" onClick={() => setSettings(true)} data-testid="work-roadmap-settings">{t("work.timeline.settings")}</Button>
      </div>
      {query.error && <Alert intent="danger" title={query.error} data-testid="work-timeline-error" />}
      {query.data !== undefined && bars.length === 0 ? (
        <p className="text-sm opacity-70" data-testid="work-timeline-empty">{t("work.timeline.empty")}</p>
      ) : (
        <Gantt
          tasks={bars.map(({ id, name, start, end, lane, color, progress, badge, real }) => ({ id, name, start, end, lane, color, progress, badge: real ? badge : t("work.timeline.notStarted"), locked: true }))}
          lanes={lanes}
          links={links}
          editable={false}
          selectedId={openId === null ? null : String(openId)}
          onSelect={(id) => setOpenId(id === null ? null : Number(id))}
          loading={query.loading && query.data === undefined}
          height={460}
        />
      )}
      {openId !== null && (
        <ItemDrawer itemId={openId} vocabulary={vocabulary.data} controller={controller} live={live} canWrite={canWrite} onClose={() => setOpenId(null)} onChanged={() => { void query.refetch(); }} />
      )}
      {settings && (
        <OverlaySidePanel title={t("work.timeline.settings")} titleClassName="text-xl font-semibold" variant="simple" width={560} onClose={() => setSettings(false)}>
          <div className="flex flex-col gap-4">
            <DispatchSettings controller={controller} live={live} canWrite={canWrite} />
            <ProposalsStrip controller={controller} live={live} canWrite={canWrite} />
          </div>
        </OverlaySidePanel>
      )}
    </div>
  );
}
