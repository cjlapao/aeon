import { useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Button, EmptyState, Table, type TableColumn } from "../../../components/kit";
import { OverlaySidePanel } from "../../../components/OverlaySidePanel";
import { GraphMaintain } from "./GraphMaintain";
import { fmtInt, rootsOf, type StatusView } from "./systemModel";

/**
 * GraphRoots — every code-graph root the daemon holds, one row each, with the counts it reports and
 * the one door to maintain them (build, reparse, delete — each behind its confirm, in a side panel).
 * This is the whole-daemon view; reading the graph (who calls what) lives in each component's
 * Knowledge tab, so nothing here explores.
 */
type Root = ReturnType<typeof rootsOf>[number];

export function GraphRoots({ block, canWrite, onChanged }: { block: StatusView["graph"] | undefined; canWrite: boolean; onChanged: () => void }) {
  const { t } = useI18n();
  const [maintaining, setMaintaining] = useState(false);
  const roots = rootsOf(block);

  const columns: TableColumn<Root>[] = [
    { id: "root", groupable: false, header: t("settings.system.graph.root"), render: (row) => <span className="truncate font-mono text-sm">{row.root}</span> },
    { id: "files", groupable: false, header: t("settings.system.retrieval.files"), width: 100, render: (row) => <span className="text-sm">{row.neverBuilt === true ? "—" : fmtInt(row.files)}</span> },
    { id: "nodes", groupable: false, header: t("settings.system.graph.nodes"), width: 110, render: (row) => <span className="text-sm">{row.neverBuilt === true ? "—" : fmtInt(row.nodes)}</span> },
    { id: "edges", groupable: false, header: t("settings.system.graph.edges"), width: 110, render: (row) => <span className="text-sm">{row.neverBuilt === true ? "—" : fmtInt(row.edges)}</span> },
    { id: "state", groupable: false, header: t("settings.system.retrieval.state"), width: 160, render: (row) => <span className="text-sm">{row.neverBuilt === true ? t("settings.system.graph.neverBuilt") : t("settings.system.retrieval.current")}</span> },
  ];

  return (
    <div className="flex min-h-[26rem] min-w-0 flex-1 flex-col gap-3" data-testid="system-graph-tab">
      <div className="flex shrink-0 justify-end">
        <Button size="sm" variant="ghost" onClick={() => setMaintaining(true)} data-testid="graph-maintain-open">{t("settings.system.graph.maintain")}</Button>
      </div>
      <div className="min-h-[16rem] min-w-0 flex-1" data-testid="graph-roots">
        <Table<Root>
          variant="outlined"
          data={roots as Root[]}
          columns={columns}
          rowKey={(row) => row.root}
          corner="rounded-sm"
          emptyState={<div data-testid="graph-no-roots"><EmptyState title={t("settings.system.graph.noRoots")} subtitle={t("settings.system.graph.noRootsBody")} /></div>}
          fullHeight
        />
      </div>
      {maintaining && (
        <OverlaySidePanel title={t("settings.system.graph.maintainTitle")} titleClassName="text-xl font-semibold" variant="simple" width={640} onClose={() => setMaintaining(false)}>
          <GraphMaintain block={block} canWrite={canWrite} onChanged={onChanged} />
        </OverlaySidePanel>
      )}
    </div>
  );
}
