import { useI18n } from "@cjlapao/ui-kit";
import { Alert, EmptyState, Pill, Table, type TableColumn } from "../../../components/kit";
import { hasDetail, notStartingLines, subjectWords, verdictTone, type SchedulerPreview, type SchedulerPreviewLine } from "./continuousModel";

/**
 * PreviewTab — "what would the next tick do?" (S27 T10 behaviour 3/5, CN15/CN22).
 *
 * The lines are the daemon's tick run as an answer: the page runs nothing, orders nothing and
 * computes no verdict. Lines that would start come first, then every other decided line with the
 * daemon's own reason — and where the reason has an instant (a window's opening time, a rate
 * limit's reset) it rides inside the daemon's `detail`, printed untouched. The "not starting" half
 * is the answered lines that are not in `wouldStart` (a filter by identity, never a re-decision).
 *
 * The honesty about capacity (rule 3, CN15): the preview answers at most `slots.cap` starts — a
 * COUNT of slots, not a memory measurement — and the footer says so rather than let a short list
 * read as a bound the machine does not have.
 */
export interface PreviewTabProps {
  preview: SchedulerPreview | undefined;
  error: string | undefined;
  onRetry: () => void;
}

type Line = SchedulerPreviewLine & { starting: boolean };

export function PreviewTab({ preview, error, onRetry }: PreviewTabProps) {
  const { t } = useI18n();

  if (error !== undefined) {
    return (
      <Alert intent="danger" title={t("settings.continuous.preview.failed")} description={error} data-testid="preview-failed">
        <button type="button" onClick={onRetry} data-testid="preview-retry">{t("settings.continuous.actions.retry")}</button>
      </Alert>
    );
  }
  if (preview === undefined) {
    return <p className="text-xs opacity-70" data-testid="preview-loading">{t("settings.continuous.loading")}</p>;
  }

  const starting = Array.isArray(preview.wouldStart) ? preview.wouldStart : [];
  const holding = notStartingLines(preview);
  const lines: Line[] = [...starting.map((l) => ({ ...l, starting: true })), ...holding.map((l) => ({ ...l, starting: false }))];

  const columns: TableColumn<Line>[] = [
    {
      id: "verdict",
      groupable: false,
      header: t("settings.continuous.col.verdict"),
      width: 120,
      render: (line) => <Pill size="sm" tone={verdictTone(line.verdict)}>{line.verdict}</Pill>,
    },
    {
      id: "repo",
      groupable: false,
      header: t("settings.continuous.col.repository"),
      render: (line) => <span className="block truncate py-1 text-sm font-medium">{line.componentRoot}</span>,
    },
    {
      id: "subject",
      groupable: false,
      header: t("settings.continuous.col.subject"),
      width: 130,
      render: (line) => <span className="text-sm opacity-80">{subjectWords(line.subjectKind, line.subjectId)}</span>,
    },
    {
      id: "why",
      groupable: false,
      header: t("settings.continuous.col.why"),
      width: 300,
      render: (line) => (
        <span className="block py-1 text-sm" data-testid={`preview-${line.starting ? "start" : "hold"}-line-${line.componentRoot}-${line.subjectKind}-${line.subjectId}`}>
          <span className="opacity-90">{line.reason}</span>
          {hasDetail(line) && <span className="block text-xs opacity-70">{line.detail}</span>}
        </span>
      ),
    },
  ];

  return (
    <div className="flex min-h-[22rem] min-w-0 flex-1 flex-col gap-2" data-testid="preview-tab">
      <p className="shrink-0 text-xs opacity-70" data-testid="preview-now">{t("settings.continuous.preview.now", { now: preview.now ?? "" })}</p>
      <div className="min-h-[18rem] min-w-0 flex-1">
        <Table<Line>
          variant="outlined"
          data={lines}
          columns={columns}
          rowKey={(line) => `${line.componentRoot}|${line.subjectKind}|${line.subjectId}|${line.verdict}`}
          corner="rounded-sm"
          emptyState={
            <div data-testid="preview-would-start-empty">
              <EmptyState title={t("settings.continuous.preview.wouldStartEmpty")} />
            </div>
          }
          fullHeight
        />
      </div>
      <p className="shrink-0 text-xs opacity-70" data-testid="preview-cap-note">
        {t("settings.continuous.preview.footer", { starting: starting.length, holding: holding.length })}
      </p>
    </div>
  );
}
