import { Link } from "react-router-dom";
import { Button, EmptyState, Pill, Table, type TableColumn } from "../../components/kit";
import type { RouteView } from "../../lib/protocol";
import { CAPABILITIES, modelName } from "./modelConfig";
import { ModelIcon } from "./ModelIcon";
import { priceWords } from "./modelsModel";

/** One row per model: who it is, what it can do, what it costs, whether it has a key, and the three things you do to it. */
export function ModelsTable({ rows, canWrite, filtered, onTest, onDelete }: { rows: RouteView[]; canWrite: boolean; filtered: boolean; onTest: (route: RouteView) => void; onDelete: (route: RouteView) => void }) {
  const capWord = (id: string) => CAPABILITIES.find((c) => c.id === id)?.label ?? id;
  const columns: TableColumn<RouteView>[] = [
    {
      id: "model", groupable: false, header: "Model", sortable: true, sortValue: (r) => modelName(r).toLowerCase(),
      render: (r) => (
        <div className="flex min-w-0 items-center gap-3 py-1" data-testid={`model-row-${r.id}`}>
          <ModelIcon icon={r.icon} />
          <div className="min-w-0">
            <p className="truncate text-sm font-semibold">{modelName(r)}</p>
            <p className="truncate font-mono text-xs opacity-60">{r.model}</p>
          </div>
        </div>
      ),
    },
    { id: "provider", groupable: false, header: "Provider", sortable: true, sortValue: (r) => r.provider, width: 130, render: (r) => <span className="text-sm">{r.provider}</span> },
    {
      id: "caps", groupable: false, header: "Can do", width: 300,
      render: (r) => {
        const caps = r.capabilities;
        if (caps == null) return <span className="text-xs opacity-60">not declared</span>;
        if (caps.length === 0) return <span className="text-xs opacity-60">nothing declared</span>;
        return (
          <div className="flex flex-wrap gap-1">
            {caps.slice(0, 3).map((c) => <Pill key={c} size="sm" tone="slate">{capWord(c)}</Pill>)}
            {caps.length > 3 && <Pill size="sm" tone="slate" title={caps.slice(3).map(capWord).join(", ")}>+{caps.length - 3}</Pill>}
          </div>
        );
      },
    },
    { id: "price", groupable: false, header: "Price", width: 190, render: (r) => <span className="text-xs opacity-80" data-testid={`price-${r.id}`}>{priceWords(r)}</span> },
    { id: "key", groupable: false, header: "Key", width: 100, render: (r) => <Pill tone={r.hasKey ? "green" : "slate"} size="sm">{r.hasKey ? "Key set" : "No key"}</Pill> },
    {
      id: "act", groupable: false, header: "", width: 230,
      render: (r) => (
        <div className="flex items-center justify-end gap-2">
          <Button size="sm" variant="ghost" onClick={() => onTest(r)} aria-label={`Test ${modelName(r)}`} data-testid={`test-${r.id}`}>Test</Button>
          {canWrite && <Link to={`/models/${encodeURIComponent(r.id)}/edit`} className="aeon-accent-text text-sm underline" aria-label={`Edit ${modelName(r)}`}>Edit</Link>}
          <Button size="sm" variant="ghost" color="red" disabled={!canWrite} onClick={() => onDelete(r)} aria-label={`Delete ${modelName(r)}`}>Delete</Button>
        </div>
      ),
    },
  ];
  return (
    <Table<RouteView>
      variant="outlined"
      data={rows}
      columns={columns}
      rowKey={(r) => r.id}
      corner="rounded-sm"
      emptyState={<EmptyState title={filtered ? "No model matches" : "Build your model collection"} subtitle={filtered ? "Try another word, or clear the provider filter." : "Add a provider connection and choose the capabilities available on that model."} />}
      fullHeight
    />
  );
}
