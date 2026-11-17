import { useEffect } from "react";
import type { ReactNode } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, EmptyState, Pill, Select, Table, type TableColumn } from "../../../components/kit";
import type { LiveController, LiveState } from "../../../lib/live";
import { useReveal } from "../tools/useReveal";
import { useTriageBoard } from "./triageRpc";
import type { BoardRow, FilterDraft, Vocabulary } from "./triageModel";
import { priorityTone, validationTone } from "./triageModel";

/**
 * TriageBoard — the action bar (S51 T7, TM10). It owns INPUTS and nothing else: it fetches nothing
 * and filters nothing client-side. The draft travels up, the section composes `triage.board`
 * PARAMS from it, and the DAEMON filters — a board that swept a fetched list would be a second,
 * smaller truth about what matches. The bar renders before a root is named (one has to be chosen);
 * the rows are the gated children the section passes.
 */
export interface TriageBoardProps {
  draft: FilterDraft;
  vocabulary: Vocabulary | undefined;
  /** The component roots a person can point the board at (each answered by `component.list`). */
  roots: ReadonlyArray<{ root: string; name: string }>;
  onDraftChange: (next: FilterDraft) => void;
  children?: ReactNode;
}

export function TriageBoard({ draft, vocabulary, roots, onDraftChange, children }: TriageBoardProps) {
  const { t } = useI18n();
  return (
    <div className="flex min-h-[24rem] flex-1 flex-col gap-3" data-testid="triage-board">
      <div className="flex shrink-0 flex-wrap items-center gap-3" data-testid="triage-filters">
        <div className="w-64 shrink-0">
          <Select
            variant="ghost"
            size="md"
            value={draft.componentRoot}
            aria-label={t("settings.triage.filters.root")}
            data-testid="triage-filter-root"
            onChange={(e) => onDraftChange({ ...draft, componentRoot: e.target.value })}
          >
            <option value="">{t("settings.triage.root.none")}</option>
            {roots.map((entry) => <option key={entry.root} value={entry.root}>{entry.name}</option>)}
          </Select>
        </div>
        <div className="w-52 shrink-0">
          <Select variant="ghost" size="md" value={draft.validation} aria-label={t("settings.triage.filters.validation")} data-testid="triage-filter-validation" onChange={(e) => onDraftChange({ ...draft, validation: e.target.value })}>
            <option value="">{t("settings.triage.filters.validation")}: {t("settings.triage.filters.any")}</option>
            {/* The options ARE the daemon's closed set; a word absent from it is not selectable. */}
            {(vocabulary?.validationStates ?? []).map((state) => <option key={state} value={state}>{state}</option>)}
          </Select>
        </div>
        <div className="w-48 shrink-0">
          <Select variant="ghost" size="md" value={draft.priority} aria-label={t("settings.triage.filters.priority")} data-testid="triage-filter-priority" onChange={(e) => onDraftChange({ ...draft, priority: e.target.value })}>
            <option value="">{t("settings.triage.filters.priority")}: {t("settings.triage.filters.any")}</option>
            {(vocabulary?.priorityStates ?? []).map((state) => <option key={state} value={state}>{state}</option>)}
          </Select>
        </div>
        <div className="w-48 shrink-0">
          <Select variant="ghost" size="md" value={draft.handedOff} aria-label={t("settings.triage.filters.handedOff")} data-testid="triage-filter-handed-off" onChange={(e) => onDraftChange({ ...draft, handedOff: e.target.value })}>
            <option value="">{t("settings.triage.filters.handedOff")}: {t("settings.triage.filters.any")}</option>
            <option value="yes">{t("settings.triage.filters.yes")}</option>
            <option value="no">{t("settings.triage.filters.no")}</option>
          </Select>
        </div>
      </div>
      {children}
    </div>
  );
}

/** The root-gate message, kept beside the board it describes. */
export function BoardNeedsRoot() {
  const { t } = useI18n();
  return (
    <div className="min-h-[14rem]">
      <EmptyState showIcon={false} title={t("settings.triage.board.needsRoot")} data-testid="triage-needs-root" />
    </div>
  );
}

const SORT_VALUE = (row: BoardRow, column: string): string | number => {
  switch (column) {
    case "validation": return row.bug.validation;
    case "priority": return row.bug.priorityState;
    case "attempts": return row.attempts;
    default: return row.bug.id;
  }
};

/**
 * The board's data half, mounted only once a root is named: it asks the daemon for THIS root and
 * THESE filters, renders the rows it answers — nothing swept, nothing hidden — and reports the fresh
 * rows up so an open panel never freezes on a stale snapshot.
 *
 * The two state sets render as TWO SEPARATE PILLS per row (TM10): each pill carries its daemon word
 * as TEXT beside the colour, and the daemon's `describe` sentence prints beneath the title.
 */
export function TriageBoardRows({ draft, controller, live, onOpen, onRows }: {
  draft: FilterDraft;
  controller: LiveController;
  live: LiveState;
  onOpen: (id: number) => void;
  onRows: (rows: BoardRow[]) => void;
}) {
  const { t } = useI18n();
  const board = useTriageBoard(draft, controller, live);
  useEffect(() => {
    if (board.data !== undefined) onRows(board.data);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [board.data]);

  const rows = board.data ?? [];
  const view = useReveal(rows, `${draft.componentRoot}|${draft.validation}|${draft.priority}|${draft.handedOff}|${rows.length}`, "id", SORT_VALUE);

  const columns: TableColumn<BoardRow>[] = [
    {
      id: "id",
      groupable: false,
      header: t("settings.triage.col.bug"),
      sortable: true,
      render: (row) => (
        <div
          className="w-0 min-w-full py-1"
          data-testid={`triage-row-${row.bug.id}`}
          data-validation={row.bug.validation}
          data-priority-state={row.bug.priorityState}
        >
          <strong className="block truncate text-sm" data-testid={`triage-row-title-${row.bug.id}`}>#{row.bug.id} {row.bug.title}</strong>
          {/* The daemon's sentences, each on its own line, verbatim (T6 law). */}
          <p className="truncate text-xs opacity-70" title={row.describe.validationSentence} data-testid={`triage-validation-sentence-${row.bug.id}`}>{row.describe.validationSentence}</p>
          <p className="truncate text-xs opacity-70" title={row.describe.prioritySentence} data-testid={`triage-priority-sentence-${row.bug.id}`}>{row.describe.prioritySentence}</p>
        </div>
      ),
    },
    {
      id: "validation",
      groupable: false,
      header: t("settings.triage.col.validation"),
      sortable: true,
      width: 170,
      // Two state sets, two separate pills — text and colour, never one merged word.
      render: (row) => <Pill size="sm" tone={validationTone(row.bug.validation)} data-testid={`triage-validation-${row.bug.id}`}>{row.bug.validation}</Pill>,
    },
    {
      id: "priority",
      groupable: false,
      header: t("settings.triage.col.priority"),
      sortable: true,
      width: 140,
      render: (row) => <Pill size="sm" tone={priorityTone(row.bug.priorityState)} data-testid={`triage-priority-${row.bug.id}`}>{row.bug.priorityState}</Pill>,
    },
    {
      id: "filedBy",
      groupable: false,
      header: t("settings.triage.col.filedBy"),
      width: 180,
      render: (row) => <span className="block truncate text-xs opacity-80" data-testid={`triage-filed-by-${row.bug.id}`}>{t("settings.triage.row.filedBy")} {row.bug.filedBy}</span>,
    },
    {
      id: "attempts",
      groupable: false,
      header: t("settings.triage.col.attempts"),
      sortable: true,
      width: 230,
      render: (row) => (
        <span className="block truncate text-xs opacity-70" data-testid={`triage-attempts-${row.bug.id}`}>
          {row.attempts}{row.lastAttemptAt !== undefined && row.lastAttemptAt !== "" ? ` · ${row.lastAttemptAt}` : ` · ${t("settings.triage.row.neverAttempted")}`}
        </span>
      ),
    },
  ];

  return (
    <div className="flex min-h-[20rem] flex-1 flex-col gap-2" data-testid="triage-rows">
      {board.error !== undefined && <Alert className="shrink-0" intent="danger" title={board.error} data-testid="triage-board-error" />}
      <div className="min-h-[18rem] flex-1">
        <Table<BoardRow>
          variant="outlined"
          data={view.rows}
          columns={columns}
          rowKey={(row) => String(row.bug.id)}
          manualSorting
          corner="rounded-sm"
          sortState={view.sortState}
          onSortChange={view.onSortChange}
          onRowClick={(row) => onOpen(row.bug.id)}
          loading={board.data === undefined && board.loading}
          loadingMessage={t("settings.triage.board.loading")}
          // The empty board speaks, and says which question it answered.
          emptyState={board.error !== undefined ? <span /> : <div data-testid="triage-board-empty"><EmptyState showIcon={false} title={t("settings.triage.board.empty")} /></div>}
          fullHeight
          infiniteScroll={view.infinite}
        />
      </div>
      <div className="shrink-0 text-sm opacity-80" data-testid="triage-count">{t("settings.triage.footer", { shown: view.rows.length, total: view.total })}</div>
    </div>
  );
}
