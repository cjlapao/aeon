import { useI18n } from "@cjlapao/ui-kit";
import { EmptyState, Pill, Table, type TableColumn } from "../../../components/kit";
import { RelativeTime } from "../../../components/RelativeTime";
import { formatBytes, schemaLine, type BackupView } from "./backupsModel";

/**
 * The backup rows (T10): `backup.list` answers, one table row each.
 *
 * The row recomputes nothing: the name, the created time, the size, the schema line, the reason badge,
 * the verify pill and the restorability line are the daemon's own fields and words, captioned from the
 * daemon's own enum strings (reason, verified) — a value the daemon can newly report arrives as a
 * missing caption, never as a word the UI invented. Newest first and the limit are the daemon's (T6);
 * the table only shows, and a click opens the copy in its panel.
 */
export interface BackupListProps {
  rows: BackupView[];
  loading: boolean;
  onOpen: (file: string) => void;
}

export function BackupList({ rows, loading, onOpen }: BackupListProps) {
  const { t } = useI18n();

  const columns: TableColumn<BackupView>[] = [
    {
      id: "file",
      groupable: false,
      header: t("settings.backups.col.backup"),
      render: (view) => (
        <div className="w-0 min-w-full py-1" data-testid={`backup-row-${view.file}`}>
          <span className="block truncate font-mono text-sm" title={view.file} data-testid={`backup-row-file-${view.file}`}>{view.file}</span>
          <span className="block truncate text-xs opacity-60" data-testid={`backup-row-created-${view.file}`}><RelativeTime iso={view.createdAt} /> · {view.createdAt}</span>
        </div>
      ),
    },
    { id: "size", groupable: false, header: t("settings.backups.col.size"), width: 100, render: (view) => <span className="text-sm tabular-nums" data-testid={`backup-row-size-${view.file}`}>{formatBytes(view.sizeBytes)}</span> },
    { id: "schema", groupable: false, header: t("settings.backups.col.schema"), width: 230, render: (view) => <span className="text-xs opacity-80" data-testid={`backup-row-schema-${view.file}`}>{schemaLine(view)}</span> },
    { id: "reason", groupable: false, header: t("settings.backups.col.reason"), width: 130, render: (view) => <Pill size="sm" tone="slate" data-testid={`backup-row-reason-${view.file}`}>{t(`settings.backups.reason.${view.reason}`)}</Pill> },
    {
      id: "verified",
      groupable: false,
      header: t("settings.backups.col.verified"),
      width: 150,
      render: (view) => <Pill size="sm" tone={view.verified === "unverified" ? "amber" : "green"} data-testid={`backup-row-verified-${view.file}`}>{t(`settings.backups.verified.${view.verified}`)}</Pill>,
    },
    {
      id: "restorable",
      groupable: false,
      header: t("settings.backups.col.restorable"),
      width: 230,
      render: (view) => (
        <span className={`block py-1 text-xs ${view.canRestoreWithThisBinary ? "opacity-80" : "font-medium text-amber-600 dark:text-amber-400"}`} data-testid={`backup-row-restorable-${view.file}`}>
          {view.canRestoreWithThisBinary ? t("settings.backups.row.restorable") : t("settings.backups.row.notRestorable", { migration: view.lastMigration })}
        </span>
      ),
    },
  ];

  return (
    <div className="min-h-[18rem] min-w-0 flex-1" data-testid="backup-list">
      <Table<BackupView>
        variant="outlined"
        data={rows}
        columns={columns}
        rowKey={(view) => view.file}
        corner="rounded-sm"
        loading={loading && rows.length === 0}
        onRowClick={(view) => onOpen(view.file)}
        emptyState={
          <div data-testid="backup-list-empty">
            <EmptyState title={t("settings.backups.list.emptyTitle")} subtitle={t("settings.backups.list.emptyBody")} />
          </div>
        }
        fullHeight
      />
    </div>
  );
}
