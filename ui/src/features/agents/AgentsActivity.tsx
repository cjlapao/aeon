import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, EmptyState, Pill, Table, type TableColumn } from "../../components/kit";
import { RelativeTime } from "../../components/RelativeTime";
import type { LiveState } from "../../lib/live";
import { ActorTag, pillTone, useOpLabel } from "./AgentChanges";
import { useAgentLedger } from "./agentsLedger";
import { changeOpTone, changeSubject, type AgentChangeRow } from "./agentsModel";

// The Agents page's Activity view: the whole ledger, newest first, every agent. It is the one
// place the entries that name no single agent appear (a migrate run carries a NULL slug), and
// the way to see what happened across agents without opening each. The daemon's pages are
// appended as the table is scrolled; nothing is sorted, filtered or counted here.

export function AgentsActivity({ live, onOpenAgent }: { live: LiveState; onOpenAgent: (slug: string) => void }) {
  const { t } = useI18n();
  const opLabel = useOpLabel();
  const ledger = useAgentLedger(live, null);
  const failed = ledger.error !== undefined;

  const columns: TableColumn<AgentChangeRow>[] = [
    {
      id: "when",
      header: t("agents.activity.col.when"),
      width: 190,
      groupable: false,
      render: (row) => (
        <div data-testid={`agent-change-${row.id}-at`}>
          <div className="text-sm"><RelativeTime iso={row.occurredAt} /></div>
          <div className="font-mono text-[11px] opacity-60">{row.occurredAt}</div>
        </div>
      ),
    },
    {
      id: "op",
      header: t("agents.activity.col.change"),
      width: 250,
      groupable: false,
      render: (row) => <Pill tone={pillTone(changeOpTone(row.op))} size="sm" data-testid={`agent-change-${row.id}-op`}>{opLabel(row.op)}</Pill>,
    },
    {
      id: "agent",
      header: t("agents.activity.col.agent"),
      width: 220,
      groupable: false,
      render: (row) => {
        const subject = changeSubject(row.slug);
        // AR1's NULL slug: a write about the set, not about one agent — nothing to open.
        if (subject === null) return <span className="text-sm italic opacity-70" data-testid={`agent-change-${row.id}-subject`}>{t("agents.activity.everyAgent")}</span>;
        return (
          <button type="button" className="w-0 min-w-full truncate text-left font-mono text-sm underline decoration-dotted underline-offset-4" data-testid={`agent-change-${row.id}-subject`}
            onClick={(event) => { event.stopPropagation(); onOpenAgent(subject); }}>
            {subject}
          </button>
        );
      },
    },
    {
      id: "summary",
      header: t("agents.activity.col.what"),
      groupable: false,
      render: (row) => <div className="w-0 min-w-full truncate text-sm" title={row.summary} data-testid={`agent-change-${row.id}-summary`}>{row.summary}</div>,
    },
    {
      id: "by",
      header: t("agents.activity.col.by"),
      width: 220,
      groupable: false,
      render: (row) => <ActorTag row={row} />,
    },
  ];

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-2" data-testid="agents-activity">
      {failed && (
        <div className="shrink-0 space-y-2" data-testid="agents-activity-error">
          <Alert intent="danger" title={t("agents.history.failed")} description={ledger.error} />
          <Button size="sm" variant="ghost" onClick={ledger.rows.length > 0 ? ledger.loadMore : ledger.reload} data-testid="agents-activity-retry">{t("agents.action.retry")}</Button>
        </div>
      )}
      <div className="min-h-0 flex-1">
        <Table<AgentChangeRow>
          variant="outlined"
          data={ledger.rows}
          columns={columns}
          rowKey={(row) => String(row.id)}
          manualSorting
          corner="rounded-sm"
          loading={ledger.loading}
          loadingMessage={t("agents.history.loading")}
          emptyState={<EmptyState title={t("agents.activity.empty")} />}
          fullHeight
          infiniteScroll={{ hasMore: ledger.hasMore, onLoadMore: ledger.loadMore, resetKey: ledger.resetKey }}
        />
      </div>
      <div className="flex shrink-0 items-center justify-between gap-3 text-sm opacity-80">
        <span data-testid="agents-activity-count">{t("agents.activity.showing", { shown: ledger.rows.length, total: ledger.total })}</span>
        <span className="text-xs opacity-80">{t("agents.activity.note")}</span>
      </div>
    </div>
  );
}
