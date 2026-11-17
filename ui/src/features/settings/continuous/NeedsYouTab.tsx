import { useI18n } from "@cjlapao/ui-kit";
import { Alert, EmptyState, Table, type TableColumn } from "../../../components/kit";
import { attentionNote, subjectWords, type SchedulerAttention, type SchedulerAttentionItem } from "./continuousModel";

/**
 * NeedsYouTab — `scheduler.attention` (S27 T10 behaviour 5, CN13/CN21).
 *
 * The blocked loop's KIND is shown verbatim and nothing else about it is decided here: CN21's
 * no-interpretation rule means the page neither maps kinds to explanations nor recognizes one kind
 * specially — the daemon attaches its own `note` to the kinds whose remedy is a different act, which
 * is how S55's raise instruction reaches a loop that hit its cost cap. The page prints that note
 * untouched. The `since` instant is the daemon's stamp as answered, and the loop is named by its
 * slug as the door answers it.
 */
export interface NeedsYouTabProps {
  attention: SchedulerAttention | undefined;
  error: string | undefined;
  onRetry: () => void;
}

export function NeedsYouTab({ attention, error, onRetry }: NeedsYouTabProps) {
  const { t } = useI18n();

  if (error !== undefined) {
    return (
      <Alert intent="danger" title={t("settings.continuous.needsYou.failed")} description={error} data-testid="needs-you-failed">
        <button type="button" onClick={onRetry} data-testid="needs-you-retry">{t("settings.continuous.actions.retry")}</button>
      </Alert>
    );
  }
  if (attention === undefined) {
    return <p className="text-xs opacity-70" data-testid="needs-you-loading">{t("settings.continuous.loading")}</p>;
  }
  const items = Array.isArray(attention.items) ? attention.items : [];

  const columns: TableColumn<SchedulerAttentionItem>[] = [
    {
      id: "repo",
      groupable: false,
      header: t("settings.continuous.col.repository"),
      width: 200,
      render: (item) => (
        <div className="w-0 min-w-full py-1" data-testid={`attention-item-${item.decisionId}`}>
          <span className="block truncate text-sm font-medium">{item.componentRoot}</span>
          <span className="block text-xs opacity-70">{subjectWords(item.subjectKind, item.subjectId)}</span>
        </div>
      ),
    },
    {
      id: "kind",
      groupable: false,
      header: t("settings.continuous.col.kind"),
      width: 150,
      // The kind, verbatim (CN21).
      render: (item) => <span className="text-sm" data-testid={`attention-kind-${item.decisionId}`}>{item.kind}</span>,
    },
    {
      id: "loop",
      groupable: false,
      header: t("settings.continuous.col.loop"),
      width: 150,
      render: (item) => <span className="text-sm opacity-80" data-testid={`attention-loop-${item.decisionId}`}>{item.loopSlug}</span>,
    },
    {
      id: "note",
      groupable: false,
      header: t("settings.continuous.col.note"),
      // The daemon's own remedy sentence, untouched (CN21).
      render: (item) => <span className="block py-1 text-sm opacity-90" data-testid={`attention-note-${item.decisionId}`}>{attentionNote(item) ?? ""}</span>,
    },
    {
      id: "since",
      groupable: false,
      header: t("settings.continuous.col.since"),
      width: 150,
      render: (item) => <span className="text-xs opacity-70" data-testid={`attention-since-${item.decisionId}`}>{t("settings.continuous.needsYou.since", { at: item.since ?? "" })}</span>,
    },
  ];

  return (
    <div className="flex min-h-[20rem] min-w-0 flex-1 flex-col gap-2" data-testid="needs-you-tab">
      <div className="min-h-[16rem] min-w-0 flex-1">
        <Table<SchedulerAttentionItem>
          variant="outlined"
          data={items}
          columns={columns}
          rowKey={(item) => String(item.decisionId)}
          corner="rounded-sm"
          emptyState={
            <div data-testid="needs-you-empty">
              <EmptyState title={t("settings.continuous.needsYou.empty.title")} subtitle={t("settings.continuous.needsYou.empty.body")} />
            </div>
          }
          fullHeight
        />
      </div>
    </div>
  );
}
