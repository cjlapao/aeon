import { useMemo, useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, EmptyState, Pill, SearchBar, Select, Table, type TableColumn } from "../../../components/kit";
import { actorWords, type PlatformChangesResult } from "./systemModel";

/**
 * ActivityTab — the operation ledger (S54 T6, SP8): every row `platform.changes` answers, in the daemon's
 * order, with the daemon's own words — operation, subject, summary, who started it, the outcome ref and
 * the timestamp. The two filters narrow the rows already fetched by containment over those same fields;
 * the ledger is never re-queried and a row is never reinterpreted (the ledger's rows name the peer — the
 * person behind the endpoint).
 */
export interface ActivityTabProps {
  changes: PlatformChangesResult | undefined;
  error: string | undefined;
  onRetry: () => void;
}

type Row = PlatformChangesResult[number];

export function ActivityTab({ changes, error, onRetry }: ActivityTabProps) {
  const { t } = useI18n();
  const [q, setQ] = useState("");
  const [op, setOp] = useState("");
  const all = changes ?? [];
  const ops = useMemo(() => [...new Set(all.map((row) => row.op))].sort(), [all]);
  const rows = all.filter((row) => (op === "" || row.op === op) && (q === "" || `${row.subject} ${row.summary}`.toLowerCase().includes(q.toLowerCase())));

  const columns: TableColumn<Row>[] = [
    {
      id: "when",
      groupable: false,
      header: t("settings.system.activity.when"),
      width: 190,
      render: (row) => <span className="whitespace-nowrap text-xs opacity-80" data-testid={`activity-row-${row.id}`}>{row.occurredAt}</span>,
    },
    { id: "op", groupable: false, header: t("settings.system.activity.op"), width: 170, render: (row) => <Pill size="sm" tone="slate">{row.op}</Pill> },
    { id: "subject", groupable: false, header: t("settings.system.activity.subject"), width: 230, render: (row) => <code className="block truncate text-xs">{row.subject}</code> },
    {
      id: "summary",
      groupable: false,
      header: t("settings.system.activity.summary"),
      render: (row) => (
        <span className="block py-1 text-sm">
          {row.summary}
          {row.outcomeRef !== "" && row.outcomeRef !== undefined && (
            <span className="block text-xs opacity-70" data-testid={`activity-outcome-${row.id}`}>{row.outcomeRef}</span>
          )}
        </span>
      ),
    },
    {
      id: "actor",
      groupable: false,
      header: t("settings.system.activity.actor"),
      width: 160,
      render: (row) => <span className="text-xs opacity-80" data-testid={`activity-actor-${row.id}`}>{actorWords(row.actorKind, row.actorId)}</span>,
    },
  ];

  if (error !== undefined) {
    return (
      <Alert intent="danger" title={t("settings.system.activity.failed")} description={error} data-testid="activity-failed">
        <button type="button" onClick={onRetry} data-testid="activity-retry">{t("settings.system.actions.retry")}</button>
      </Alert>
    );
  }

  return (
    <div className="flex min-h-[24rem] min-w-0 flex-1 flex-col gap-3" data-testid="system-activity">
      <div className="flex shrink-0 flex-wrap items-center gap-3">
        <label className="block w-full max-w-xs">
          <span className="sr-only">{t("settings.system.activity.search")}</span>
          <SearchBar placeholder={t("settings.system.activity.search")} debounceMs={200} initialValue={q} onSearch={setQ} onClear={() => setQ("")} />
        </label>
        <div className="w-52 shrink-0">
          <Select variant="ghost" size="md" aria-label={t("settings.system.activity.op")} value={op} onChange={(e) => setOp(e.target.value)} data-testid="activity-filter-op">
            <option value="">{t("settings.system.activity.op")}: {t("settings.system.activity.any")}</option>
            {ops.map((word) => <option key={word} value={word}>{word}</option>)}
          </Select>
        </div>
      </div>
      <div className="min-h-[18rem] min-w-0 flex-1" data-testid="activity-rows">
        <Table<Row>
          variant="outlined"
          data={rows}
          columns={columns}
          rowKey={(row) => String(row.id)}
          corner="rounded-sm"
          loading={changes === undefined}
          emptyState={<div data-testid="activity-empty"><EmptyState title={t("settings.system.activity.empty")} /></div>}
          fullHeight
        />
      </div>
      <p className="shrink-0 text-xs opacity-70">{t("settings.system.activity.footer", { shown: rows.length, total: all.length })}</p>
    </div>
  );
}
