import { useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, EmptyState, SearchBar, Select, Table, type TableColumn } from "../../../components/kit";
import { filterDecisions, subjectWords, distinctWords, type DecisionFilters, type SchedulerDecision, type SchedulerDescriptor } from "./continuousModel";

/**
 * DecisionsTab — the ledger, newest first (S27 T10 behaviour 4).
 *
 * The rows are `scheduler.decisions` as answered: the decision word, the reason word, the daemon's
 * `summary` sentence, the actor (`kind:id` — `user:carlos` for a socket act, `daemon:master` for a
 * master flip), the loop slug — all verbatim, newest first as the door answers, never re-sorted and
 * never re-worded.
 *
 * The filters are the honest mixed pair the door makes them: the reason choices are the descriptor's
 * closed set (`descriptor.reasons`), while the repository and decision choices are the words the
 * ANSWERED WINDOW carries, so a pick list can never hold a word no answer has carried. Narrowing is
 * containment over the daemon's own fields — display, not a second ledger query.
 */
export interface DecisionsTabProps {
  descriptor: SchedulerDescriptor | undefined;
  decisions: SchedulerDecision[] | undefined;
  error: string | undefined;
  onRetry: () => void;
  limit: number;
}

export function DecisionsTab({ descriptor, decisions, error, onRetry, limit }: DecisionsTabProps) {
  const { t } = useI18n();
  const [filters, setFilters] = useState<DecisionFilters>({ componentRoot: "", subject: "", decision: "", reason: "" });

  if (error !== undefined) {
    return (
      <Alert intent="danger" title={t("settings.continuous.decisions.failed")} description={error} data-testid="decisions-failed">
        <button type="button" onClick={onRetry} data-testid="decisions-retry">
          {t("settings.continuous.actions.retry")}
        </button>
      </Alert>
    );
  }
  if (decisions === undefined) {
    return (
      <p className="text-xs opacity-70" data-testid="decisions-loading">
        {t("settings.continuous.loading")}
      </p>
    );
  }

  const allRows = Array.isArray(decisions) ? decisions : [];
  const rows = filterDecisions(allRows, filters);
  const decisionWords = distinctWords(allRows.map((row) => row.decision));
  const rootWords = distinctWords(allRows.map((row) => row.componentRoot));
  const set = (patch: Partial<DecisionFilters>) => setFilters((f) => ({ ...f, ...patch }));

  const columns: TableColumn<SchedulerDecision>[] = [
    {
      id: "at",
      groupable: false,
      header: t("settings.continuous.decisions.col.at"),
      width: 170,
      // The instant as answered — no local clock, no re-formatting: the ledger reads in the daemon's words.
      render: (row) => <span className="whitespace-nowrap text-xs opacity-80" data-testid={`decision-at-${row.id}`}>{row.at}</span>,
    },
    {
      id: "repo",
      groupable: false,
      header: t("settings.continuous.decisions.col.repository"),
      width: 180,
      render: (row) => <span className="block truncate text-sm" data-testid={`decision-row-${row.id}`}>{row.componentRoot}</span>,
    },
    {
      id: "subject",
      groupable: false,
      header: t("settings.continuous.decisions.col.subject"),
      width: 120,
      render: (row) => <span className="whitespace-nowrap text-sm opacity-80">{subjectWords(row.subjectKind, row.subjectId)}</span>,
    },
    {
      id: "decision",
      groupable: false,
      header: t("settings.continuous.decisions.col.decision"),
      width: 110,
      render: (row) => <span className="text-sm" data-testid={`decision-word-${row.id}`}>{row.decision}</span>,
    },
    {
      id: "reason",
      groupable: false,
      header: t("settings.continuous.decisions.col.reason"),
      width: 150,
      render: (row) => <span className="text-sm">{row.reason}</span>,
    },
    {
      id: "summary",
      groupable: false,
      header: t("settings.continuous.decisions.col.summary"),
      // CN21: the summary sentence VERBATIM — never composed here from the word.
      render: (row) => <span className="block py-1 text-sm opacity-80">{row.summary}</span>,
    },
    {
      id: "actor",
      groupable: false,
      header: t("settings.continuous.decisions.col.actor"),
      width: 190,
      render: (row) => (
        <span className="text-xs opacity-80">
          {row.actorKind}:{row.actorId}
          {row.loopSlug !== "" && <span className="block opacity-70">{row.loopSlug}</span>}
        </span>
      ),
    },
  ];
  const filtering = filters.componentRoot !== "" || filters.subject !== "" || filters.decision !== "" || filters.reason !== "";
  const ALL = t("settings.continuous.decisions.all");

  return (
    <div className="flex min-h-[26rem] min-w-0 flex-1 flex-col gap-3" data-testid="decisions-tab">
      <div className="flex shrink-0 flex-wrap items-center gap-3" data-testid="decisions-filters">
        <label className="block w-full max-w-xs" data-testid="decisions-filter-subject">
          <span className="sr-only">{t("settings.continuous.decisions.subject")}</span>
          <SearchBar placeholder={t("settings.continuous.decisions.subjectHint")} debounceMs={200} initialValue={filters.subject} onSearch={(subject) => set({ subject })} onClear={() => set({ subject: "" })} />
        </label>
        <div className="w-52 shrink-0">
          <Select variant="ghost" size="md" aria-label={t("settings.continuous.decisions.repository")} value={filters.componentRoot} onChange={(e) => set({ componentRoot: e.target.value })} data-testid="decisions-filter-repository">
            <option value="">{t("settings.continuous.decisions.repository")}: {ALL}</option>
            {rootWords.map((root) => <option key={root} value={root}>{root}</option>)}
          </Select>
        </div>
        <div className="w-44 shrink-0">
          <Select variant="ghost" size="md" aria-label={t("settings.continuous.decisions.decision")} value={filters.decision} onChange={(e) => set({ decision: e.target.value })} data-testid="decisions-filter-decision">
            <option value="">{t("settings.continuous.decisions.decision")}: {ALL}</option>
            {decisionWords.map((word) => <option key={word} value={word}>{word}</option>)}
          </Select>
        </div>
        <div className="w-44 shrink-0">
          <Select variant="ghost" size="md" aria-label={t("settings.continuous.decisions.reason")} value={filters.reason} onChange={(e) => set({ reason: e.target.value })} data-testid="decisions-filter-reason">
            <option value="">{t("settings.continuous.decisions.reason")}: {ALL}</option>
            {(descriptor?.reasons ?? []).map((word) => <option key={word} value={word}>{word}</option>)}
          </Select>
        </div>
        {filtering && (
          <Button size="sm" variant="ghost" onClick={() => setFilters({ componentRoot: "", subject: "", decision: "", reason: "" })} data-testid="decisions-filter-clear">
            {t("settings.continuous.actions.clearFilters")}
          </Button>
        )}
      </div>

      <div className="min-h-[18rem] min-w-0 flex-1" data-testid="decisions-list">
        <Table<SchedulerDecision>
          variant="outlined"
          data={rows}
          columns={columns}
          rowKey={(row) => String(row.id)}
          corner="rounded-sm"
          emptyState={
            <div data-testid="decisions-empty">
              <EmptyState title={t("settings.continuous.decisions.empty")} />
            </div>
          }
          fullHeight
        />
      </div>
      <p className="shrink-0 text-xs opacity-70">{t("settings.continuous.decisions.footer", { shown: rows.length, limit })}</p>
    </div>
  );
}
