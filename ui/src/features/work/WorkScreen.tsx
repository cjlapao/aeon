import { useMemo, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, ContentActions, Tabs, Toggle } from "../../components/kit";
import { PageTitle } from "../../components/PageTitle";
import type { LiveController, LiveState } from "../../lib/live";
import { useLiveSubscription } from "../../lib/useLiveSubscription";
import { useRpcQuery } from "../../lib/useRpcQuery";
import { ComponentFileBrowser } from "../components/ComponentFileBrowser";
import HistoryLazy from "./HistoryLazy";
import { WorkLoopsTab } from "./WorkLoopsTab";
import { WorkGuardrailsTab } from "./WorkGuardrailsTab";
import { WorkKnowledgeTab } from "./WorkKnowledgeTab";
import { AutomationSection } from "../settings/automation/AutomationSection";
import { WorkOverviewTab } from "./WorkOverviewTab";
import { WorkPlanTab } from "./WorkPlanTab";
import {
  buildTree, parseTab, pathTo, scopeRoots, WORK_TABS, workPath,
  type WorkComponent, type WorkLoop, type WorkTab,
} from "./workModel";

/**
 * WorkScreen — `/work/:component?/:tab?`, the one place a person does their work.
 *
 * The component tree lives in the side menu (PageLayout); this is the page for the node picked
 * there, or for "all projects" when none is. Every tab answers its question for the SCOPE: one
 * component, optionally with everything beneath it, or every project at once. The scope is the
 * address, so a link carries it. A component the daemon does not know says so and offers the way
 * back; it never renders as an empty page.
 */
export function WorkScreen({ controller, live, canWrite }: { controller: LiveController; live: LiveState; canWrite: boolean }) {
  const { t } = useI18n();
  const navigate = useNavigate();
  const params = useParams();
  const componentParam = params.component && params.component !== "all" ? decodeURIComponent(params.component) : "";
  const tab: WorkTab = parseTab(params.tab);
  const [includeSub, setIncludeSub] = useState(true);

  const list = useRpcQuery("component.list", {}, controller, live);
  const [showArchived, setShowArchived] = useState(false);
  const loopsQuery = useRpcQuery("loop.list", (showArchived ? { all: true, archived: "include" } : { all: true }) as { all: true }, controller, live);
  const components = useMemo<WorkComponent[]>(() => {
    const rows = (list.data as { components?: unknown } | undefined)?.components;
    return Array.isArray(rows) ? (rows as WorkComponent[]) : [];
  }, [list.data]);
  // Agents finish things on their own: loops move on the event stream, so the counts and the table follow
  // it without a poll and without a refresh button.
  useLiveSubscription(live, ["loop.*"], () => { void loopsQuery.refetch(); });
  const allLoops = useMemo<WorkLoop[]>(() => loopsQuery.data?.loops ?? [], [loopsQuery.data]);

  const tree = useMemo(() => buildTree(components), [components]);
  const current = componentParam ? components.find((component) => component.id === componentParam) : undefined;
  const unknown = componentParam !== "" && list.data !== undefined && current === undefined;
  const roots = useMemo(() => scopeRoots(components, tree, current?.id ?? "", includeSub), [components, tree, current, includeSub]);
  const scopedLoops = useMemo(() => allLoops.filter((loop) => roots.has(loop.componentRoot)), [allLoops, roots]);
  const children = useMemo(() => {
    if (!current) return tree;
    return tree.length === 0 ? [] : (function find(nodes: typeof tree): typeof tree {
      for (const node of nodes) {
        if (node.component.id === current.id) return node.children;
        const inner = find(node.children);
        if (inner.length > 0) return inner;
      }
      return [];
    })(tree);
  }, [tree, current]);

  const trail = current ? pathTo(components, current.id) : [];
  const title = current ? current.name : t("work.all");
  const go = (next: WorkTab) => navigate(workPath(current?.id ?? "", next));

  if (unknown) {
    return (
      <Alert intent="warning" title={t("work.unknown.title")} data-testid="work-unknown">
        {t("work.unknown.body")} <Link className="underline" to="/work">{t("work.unknown.back")}</Link>
      </Alert>
    );
  }

  const fills = tab === "loops";
  return (
    <div className={fills ? "flex h-full min-h-0 flex-col gap-4" : "flex flex-col gap-4"} data-testid="work-screen">
      <nav aria-label={t("work.breadcrumb")} className="shrink-0 text-xs opacity-70" data-testid="work-breadcrumb">
        <Link to="/work" className="hover:underline">{t("work.title")}</Link>
        {trail.map((node, index) => (
          <span key={node.id}> › {index === trail.length - 1 && tab === "overview" ? <span className="font-medium opacity-100">{node.name}</span> : <Link to={workPath(node.id)} className="hover:underline">{node.name}</Link>}</span>
        ))}
        {trail.length === 0 && <span> › <span className="font-medium opacity-100">{t("work.all")}</span></span>}
        {tab !== "overview" && <span> › <span className="font-medium opacity-100">{t(`work.tabs.${tab}`)}</span></span>}
      </nav>

      <ContentActions
        className="shrink-0"
        gap="md"
        align="end"
        actions={current ? (
          <Button size="sm" disabled={!canWrite} onClick={() => navigate(`/?component=${encodeURIComponent(current.root)}`)} data-testid="work-start">{t("work.start")}</Button>
        ) : undefined}
      >
        <PageTitle title={title} purpose={current ? (current.shortDescription || current.root) : t("work.allPurpose")} help={{ title: t("work.help.title"), content: t("work.help.body") }} />
      </ContentActions>

      {(list.error || loopsQuery.error) && <Alert intent="danger" title={t("work.error", { error: list.error ?? loopsQuery.error ?? "" })} className="shrink-0" />}

      <div className="shrink-0">
        <Tabs variant="underline" items={WORK_TABS.map((id) => ({ id, label: t(`work.tabs.${id}`) }))} value={tab} onChange={(id) => go(id as WorkTab)} />
      </div>

      {(tab === "loops" || tab === "plan" || tab === "knowledge") && (
        <div className="flex shrink-0 items-center gap-4">
          {(current ? children.length > 0 : true) && (
            <Toggle label={t("work.includeSub")} aria-label={t("work.includeSub")} checked={includeSub} onChange={(event) => setIncludeSub(event.target.checked)} data-testid="work-include-sub" />
          )}
        </div>
      )}

      <div className={fills ? "flex min-h-0 flex-1 flex-col" : ""}>
        {tab === "overview" && <WorkOverviewTab scopeLabel={title} loops={scopedLoops} children={children} components={components} />}
        {tab === "loops" && <WorkLoopsTab loops={scopedLoops} components={components} showComponent={roots.size > 1} canWrite={canWrite} onChanged={() => { void loopsQuery.refetch(); }} showArchived={showArchived} onShowArchived={setShowArchived} />}
        {tab === "plan" && <WorkPlanTab controller={controller} live={live} canWrite={canWrite} root={current?.root} scope={current ? { rootId: trail[0]?.id ?? current.id, componentIds: new Set(components.filter((c) => roots.has(c.root)).map((c) => c.id)) } : undefined} />}
        {tab === "knowledge" && <WorkKnowledgeTab controller={controller} live={live} canWrite={canWrite} scope={components.filter((c) => roots.has(c.root))} all={!current} root={current?.root} />}
        {tab === "automation" && (
          <div className="flex min-h-0 flex-1 flex-col" data-testid="work-automation">
            <AutomationSection controller={controller} live={live} canWrite={canWrite} scopeRoots={current ? roots : undefined} />
            <p className="mt-2 text-sm"><Link className="aeon-accent-text underline" to="/config/continuous">{t("work.automation.continuous")}</Link></p>
          </div>
        )}
        {tab === "guardrails" && <WorkGuardrailsTab controller={controller} live={live} canWrite={canWrite} component={current} />}
        {tab === "files" && (current ? <ComponentFileBrowser root={current.root} /> : <p className="text-sm opacity-70" data-testid="work-files-needs-one">{t("work.files.needsOne")}</p>)}
        {tab === "history" && <HistoryLazy key={current?.id ?? ""} controller={controller} live={live} canWrite={canWrite} initialRoot={current?.root ?? ""} />}
      </div>
    </div>
  );
}
