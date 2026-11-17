import { useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Button, EmptyState, FormField, Select } from "../../../components/kit";
import { refusalBody, toRpcError } from "../../../lib/bridge";
import { notifications } from "../../../lib/notifications";
import { OverlaySidePanel } from "../../../components/OverlaySidePanel";
import { indexRootFor } from "../../work/knowledgeScope";
import { GraphExplorer } from "./GraphExplorer";
import { GraphMaintain } from "./GraphMaintain";
import { startGraphBuild } from "./systemRpc";
import { blockFlying, fmtInt, rootsOf, startedWordsOf, type StatusView } from "./systemModel";

/**
 * GraphTab — the code graph: pick a root, explore who calls what, and (from the toolbar) maintain the
 * index. The roots are exactly the ones `platform.status` lists — there is no second root list. The
 * explorer is read-only; the maintenance acts live in a side panel, behind confirms.
 */
export interface GraphTabProps {
  block: StatusView["graph"] | undefined;
  canWrite: boolean;
  onChanged: () => void;
  /**
   * The project this graph is for. When given there is no root to pick: the graph is the one that serves this project
   * (its own, or its parent repository's), and an Index / Reindex button builds it.
   */
  projectRoot?: string;
}

export function GraphTab({ block, canWrite, onChanged, projectRoot }: GraphTabProps) {
  const { t } = useI18n();
  const roots = rootsOf(block);
  const [picked, setPicked] = useState("");
  const [maintaining, setMaintaining] = useState(false);
  const [indexing, setIndexing] = useState(false);
  const flying = blockFlying(block?.state);
  // With a project given there is nothing to pick. Nothing indexed yet is still a graph to build, for the project's root.
  const chosen = projectRoot !== undefined
    ? (indexRootFor(roots, projectRoot) ?? { root: projectRoot, neverBuilt: true as boolean | undefined, files: 0, nodes: 0, edges: 0 })
    : (roots.find((r) => r.root === picked) ?? roots.find((r) => r.neverBuilt !== true) ?? roots[0]);
  const built = chosen !== undefined && chosen.neverBuilt !== true;

  const index = async () => {
    if (chosen === undefined) return;
    setIndexing(true);
    try {
      const starter = startedWordsOf(await startGraphBuild(chosen.root));
      if (starter !== undefined) notifications.push({ title: t("settings.system.graph.alreadyFlying"), body: t("settings.system.startedBy", { by: starter.by, at: starter.at }), tone: "info", source: "local" });
      onChanged();
    } catch (cause) {
      notifications.push({ title: t("settings.system.graph.refused"), body: refusalBody(toRpcError(cause)), tone: "danger", source: "local" });
    } finally { setIndexing(false); }
  };

  if (chosen === undefined) {
    return (
      <div data-testid="graph-no-roots">
        <EmptyState title={t("settings.system.graph.noRoots")} subtitle={t("settings.system.graph.noRootsBody")} />
      </div>
    );
  }

  return (
    <div className="flex min-h-[30rem] min-w-0 flex-1 flex-col gap-3" data-testid="system-graph-tab">
      <div className="flex shrink-0 flex-wrap items-end gap-3">
        {projectRoot === undefined
          ? <div className="w-72 shrink-0">
            <FormField label={t("settings.system.graph.root")}>
              <Select variant="ghost" size="md" aria-label={t("settings.system.graph.root")} value={chosen.root} onChange={(e) => setPicked(e.target.value)} data-testid="graph-root-select">
                {roots.map((r) => <option key={r.root} value={r.root}>{r.root}</option>)}
              </Select>
            </FormField>
          </div>
          : <code className="pb-2 text-xs opacity-70" data-testid="graph-project-root">{chosen.root}</code>}
        <p className="pb-2 text-xs opacity-70" data-testid="graph-root-stats">
          {chosen.neverBuilt === true
            ? t("settings.system.graph.neverBuilt")
            : t("settings.system.graph.countsLine", { files: fmtInt(chosen.files), nodes: fmtInt(chosen.nodes), edges: fmtInt(chosen.edges) })}
        </p>
        <span className="flex-1" />
        {projectRoot !== undefined && <Button size="sm" disabled={!canWrite || flying || indexing} title={!canWrite || flying ? t("settings.system.disabled") : undefined}
          onClick={() => void index()} data-testid="graph-index">{flying || indexing ? t("settings.system.actions.indexing") : built ? t("settings.system.actions.reindexAll") : t("settings.system.actions.indexAll")}</Button>}
        <Button size="sm" variant="ghost" onClick={() => setMaintaining(true)} data-testid="graph-maintain-open">{t("settings.system.graph.maintain")}</Button>
      </div>

      <GraphExplorer key={chosen.root} root={chosen.root} built={chosen.neverBuilt !== true} onMaintain={() => setMaintaining(true)} />

      {maintaining && (
        <OverlaySidePanel title={t("settings.system.graph.maintainTitle")} titleClassName="text-xl font-semibold" variant="simple" width={640} onClose={() => setMaintaining(false)}>
          <GraphMaintain block={block} canWrite={canWrite} onChanged={onChanged} />
        </OverlaySidePanel>
      )}
    </div>
  );
}
