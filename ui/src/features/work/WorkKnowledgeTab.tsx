import { useMemo, useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, FormField, Select, Tabs } from "../../components/kit";
import type { LiveController, LiveState } from "../../lib/live";
import { MemorySection } from "../settings/memory/MemorySection";
import { GraphTab } from "../settings/system/GraphTab";
import { RetrievalTab } from "../settings/system/RetrievalTab";
import { statusViewOf } from "../settings/system/systemModel";
import { usePlatformStatus } from "../settings/system/systemRpc";
import { graphRootsFor, retrievalComponentsFor } from "./knowledgeScope";
import type { WorkComponent } from "./workModel";

type View = "graph" | "search" | "memory";

/**
 * Knowledge — what the daemon knows about the code of the picked scope: the code graph, the search
 * index and the memory agents learned. The sections are the same ones Configuration holds; this tab
 * only narrows them to the scope and says when an index is shared with a parent repository.
 */
export interface WorkKnowledgeTabProps {
  controller: LiveController;
  live: LiveState;
  canWrite: boolean;
  /** The components inside the scope (the picked one, and below it when sub components are included). */
  scope: readonly WorkComponent[];
  /** True for "all projects": nothing is narrowed. */
  all: boolean;
  /** The picked project's own root: the graph and search index are for it, with no root to choose. */
  root?: string;
}

export function WorkKnowledgeTab({ controller, live, canWrite, scope, all, root }: WorkKnowledgeTabProps) {
  const { t } = useI18n();
  const [view, setView] = useState<View>("graph");
  const [memoryRoot, setMemoryRoot] = useState("");
  const status = usePlatformStatus(controller, live);
  const platform = statusViewOf(status.data);
  const scopeRoots = useMemo(() => new Set(scope.map((c) => c.root)), [scope]);

  const graph = useMemo(() => graphRootsFor(platform?.graph.roots ?? [], scopeRoots, all), [platform, scopeRoots, all]);
  const graphBlock = platform ? { ...platform.graph, roots: graph.roots } : undefined;
  const retrievalBlock = platform ? { ...platform.retrieval, components: retrievalComponentsFor(platform.retrieval.components ?? [], scope, all) } : undefined;
  const reload = () => { status.refetch(); };
  const rootList = [...scopeRoots];
  const memoryProject = all ? memoryRoot : rootList.length === 1 ? rootList[0] : memoryRoot;

  return (
    <div className="flex flex-col gap-3" data-testid="work-knowledge">
      <div className="shrink-0">
        <Tabs
          variant="pill"
          items={[{ id: "graph", label: t("work.knowledge.graph") }, { id: "search", label: t("work.knowledge.search") }, { id: "memory", label: t("work.knowledge.memory") }]}
          value={view}
          onChange={(id) => setView(id as View)}
        />
      </div>

      {view !== "memory" && graph.shared.length > 0 && (
        <Alert intent="info" title={t("work.knowledge.shared", { names: scope.filter((c) => graph.shared.includes(c.root)).map((c) => c.name).join(", ") })} data-testid="work-knowledge-shared" />
      )}
      {status.error !== undefined && <Alert intent="danger" title={status.error} data-testid="work-knowledge-failed" />}

      {view === "graph" && <GraphTab block={graphBlock} canWrite={canWrite} onChanged={reload} projectRoot={!all ? root : undefined} />}
      {view === "search" && <RetrievalTab block={retrievalBlock} canWrite={canWrite} onChanged={reload} projectRoot={!all ? root : undefined} />}
      {view === "memory" && (
        <div className="flex flex-col gap-3">
          {(all || rootList.length > 1) && (
            <div className="w-72">
              <FormField label={t("work.knowledge.memoryRoot")}>
                <Select variant="ghost" size="md" aria-label={t("work.knowledge.memoryRoot")} value={memoryRoot} onChange={(e) => setMemoryRoot(e.target.value)} data-testid="work-memory-root">
                  <option value="">{t("work.knowledge.memoryAll")}</option>
                  {rootList.map((root) => <option key={root} value={root}>{scope.find((c) => c.root === root)?.name ?? root}</option>)}
                </Select>
              </FormField>
            </div>
          )}
          <MemorySection key={memoryProject} controller={controller} live={live} canWrite={canWrite} initialProject={memoryProject} />
        </div>
      )}
    </div>
  );
}
