import { useI18n } from "@cjlapao/ui-kit";
import { EmptyState, Pill, Table, type TableColumn, type TableSortState } from "../../../components/kit";
import { useEffect, useMemo, useState } from "react";
import { windowOf, type WindowState } from "../tools/clientWindow";
import { ErrorPanel } from "../../../components/ErrorPanel";
import { RelativeTime } from "../../../components/RelativeTime";
import { RpcError } from "../../../lib/bridge";
import type { TrueColor } from "@cjlapao/ui-kit";
import { isExpired, type PolicyRow } from "./policyModel";

/**
 * The Rules list — a real table (headers, one row per policy) rendered from
 * exactly the `policy.list` response the daemon answered. No client-side
 * filtering, no re-sorting, no assembled sentences: the `describe` column is
 * the daemon's one formatter verbatim, the expiry rides `RelativeTime`
 * with the expired state SHOWN (an expired deny is history, not a row to
 * hide), and the origin badge carries the provenance word the daemon sent.
 *
 * The two empties are different facts and render differently, both from the
 * daemon's own sentences (`statement`): the empty store's D4 answer vs the
 * filter's "matched nothing" answer.
 */
export interface PolicyListProps {
  rows: PolicyRow[] | undefined;
  /** The daemon's own empty sentence — D4's or the filter's — verbatim. */
  statement?: string;
  loading: boolean;
  error: string | undefined;
  onRetry: () => void;
  selectedId: string | undefined;
  onSelect: (id: string) => void;
}

/** One action, one distinct colour — text AND colour (a11y: colour never
 *  carries the meaning alone). An action the UI has not seen still renders
 *  its own word in a neutral pill: no action collapses into another. */
export function actionTone(action: string): TrueColor {
  switch (action) {
    case "allow":
      return "emerald";
    case "deny":
      return "red";
    case "ask":
      return "amber";
    default:
      return "neutral";
  }
}

/** The origin badge: the provenance word the daemon sent, distinct tones for
 *  the provenances it ships (system vs user vs import); an unknown word
 *  renders in a neutral badge rather than vanishing. */
function originTone(origin: string): TrueColor {
  switch (origin) {
    case "system":
      return "blue";
    case "user":
      return "slate";
    case "import":
      return "violet";
    default:
      return "neutral";
  }
}

const SORT_VALUE = (row: PolicyRow, column: string): string => {
  switch (column) {
    case "action": return row.action;
    case "target": return row.target;
    case "origin": return row.origin ?? "";
    case "expiry": return row.notAfter ?? "";
    default: return row.describe ?? row.id;
  }
};

/** Rows revealed per scroll step — the daemon sends the whole list, the table reveals it as it is read. */
const STEP = 40;

/** The rules as the Agents page shows its rows: one table that fills the height and loads
 *  more as it is scrolled. Five columns, each a question a person asks of a rule — what it
 *  says (the daemon's sentence, verbatim), who it is about, what it decides, where it came
 *  from, when it stops. */
function PolicyTable({ rows, onSelect, resetKey }: { rows: PolicyRow[]; onSelect: (id: string) => void; resetKey: string }) {
  const { t } = useI18n();
  const [sort, setSort] = useState<{ column: string; dir: "asc" | "desc" }>({ column: "policy", dir: "asc" });
  const [shown, setShown] = useState(STEP);
  // A new answer or a new order starts the reveal again from the top.
  const reset = `${resetKey}|${sort.column}|${sort.dir}`;
  useEffect(() => setShown(STEP), [reset]);

  const win: WindowState = { page: 1, pageSize: shown, sort: sort.column, dir: sort.dir };
  const answer = useMemo(() => windowOf(rows, win, SORT_VALUE), [rows, shown, sort.column, sort.dir]); // eslint-disable-line react-hooks/exhaustive-deps

  const columns: TableColumn<PolicyRow>[] = [
    {
      id: "policy",
      groupable: false,
      header: t("settings.policy.list.col.describe"),
      sortable: true,
      render: (row) => (
        <div className="w-0 min-w-full py-1" data-testid="policy-row" data-policy-id={row.id}>
          <div className="truncate text-sm font-medium" title={row.describe ?? ""}>{row.describe ?? ""}</div>
          <div className="font-mono text-xs opacity-60">{row.id}</div>
        </div>
      ),
    },
    {
      id: "target",
      groupable: false,
      header: t("settings.policy.list.col.target"),
      sortable: true,
      width: 220,
      render: (row) => <Pill size="sm" tone="slate"><span className="font-mono">{row.target}</span></Pill>,
    },
    {
      id: "action",
      groupable: false,
      header: t("settings.policy.list.col.action"),
      sortable: true,
      width: 110,
      render: (row) => <Pill tone={actionTone(row.action)}>{row.action}</Pill>,
    },
    {
      id: "origin",
      groupable: false,
      header: t("settings.policy.list.col.from"),
      sortable: true,
      width: 130,
      render: (row) => (row.origin ? <Pill size="sm" tone={originTone(row.origin)}>{row.origin}</Pill> : <span className="text-xs opacity-60">{row.source}</span>),
    },
    {
      id: "expiry",
      groupable: false,
      header: t("settings.policy.list.col.expiry"),
      sortable: true,
      width: 190,
      render: (row) => (
        <span className="text-xs" data-testid="policy-expiry">
          {row.notAfter ? (
            <>
              <RelativeTime iso={row.notAfter} />
              {isExpired(row.notAfter) && <Pill tone="rose" size="sm" className="ml-1">{t("settings.policy.list.expired")}</Pill>}
            </>
          ) : (
            <span className="opacity-60">{t("settings.policy.list.noExpiry")}</span>
          )}
        </span>
      ),
    },
  ];

  return (
    <div className="flex min-h-[20rem] flex-1 flex-col gap-2" data-testid="policy-list">
      <div className="min-h-[18rem] flex-1">
        <Table<PolicyRow>
          variant="outlined"
          data={answer.rows}
          columns={columns}
          rowKey={(row) => row.id}
          manualSorting
          corner="rounded-sm"
          sortState={{ columnId: sort.column, direction: sort.dir }}
          onSortChange={(next: TableSortState | null) =>
            setSort(next === null ? { column: "policy", dir: "asc" } : { column: next.columnId, dir: next.direction === "desc" ? "desc" : "asc" })
          }
          onRowClick={(row) => onSelect(row.id)}
          fullHeight
          infiniteScroll={{ hasMore: answer.rows.length < answer.total, onLoadMore: async () => setShown((n) => n + STEP), resetKey: reset }}
        />
      </div>
      <div className="shrink-0 text-sm opacity-80" data-testid="policy-count">
        {t("settings.policy.list.footer", { shown: answer.rows.length, total: answer.total })}
      </div>
    </div>
  );
}

export function PolicyList({ rows, statement, loading, error, onRetry, onSelect }: PolicyListProps) {
  const { t } = useI18n();

  if (loading) return <EmptyState title={t("settings.policy.list.loading")} />;
  if (error) return <ErrorPanel error={new RpcError(0, error)} onRetry={onRetry} />;

  const list = rows ?? [];
  if (list.length === 0) {
    // The daemon sends exactly one of its two empty sentences with an empty
    // listing; the UI renders whichever it is, verbatim, and never dresses
    // one up as the other. No statement at all is said to be missing.
    return (
      <EmptyState
        title={statement ?? t("settings.policy.list.noStatement")}
        subtitle={statement === undefined ? t("settings.policy.list.noStatementBody") : undefined}
      />
    );
  }

  return <PolicyTable rows={list} onSelect={onSelect} resetKey={String(list.length)} />;
}
