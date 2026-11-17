import { useI18n } from "@cjlapao/ui-kit";
import type { ReactNode } from "react";
import { Alert, Button, EmptyState, Pill, Table, type TableColumn } from "../../../components/kit";
import { RelativeTime } from "../../../components/RelativeTime";
import type { ChangeRow, DisplayRow, Vocabulary } from "./environmentsModel";
import { backendOf, bindingsOf } from "./environmentsModel";

/**
 * The environments as a quiet table: the name with its slug, the backend with the strength number
 * the daemon gave it, where it is bound, the notes that matter (legacy, archived, origin) and the
 * last change. The capability chips and everything else about a row live in its side panel. What a
 * row says about itself is what `environment.list` answered plus what `environment.vocabulary` says
 * about that row's backend; the legacy note is the row's `legacy` boolean, and the last change is
 * the audit door's `actorId` beside a relative time.
 */
export interface EnvironmentRowsProps {
  rows: readonly DisplayRow[];
  loading: boolean;
  error: string | undefined;
  vocabulary: Vocabulary | undefined;
  /** `component.list`'s ids → names, so a binding names the component. */
  names: Map<string, string>;
  /** The newest audit row per environment id, from `environment.changes`. */
  changes: Map<string, ChangeRow>;
  onOpen: (id: string) => void;
  onRetry: () => void;
  filterBar: ReactNode;
}

export function EnvironmentRows({ rows, loading, error, vocabulary, names, changes, onOpen, onRetry, filterBar }: EnvironmentRowsProps) {
  const { t } = useI18n();

  const columns: TableColumn<DisplayRow>[] = [
    {
      id: "name",
      groupable: false,
      header: t("settings.environments.col.environment"),
      render: (row) => (
        <div className="w-0 min-w-full py-1" data-testid={`environment-row-toggle-${row.id}`}>
          <span className="block truncate text-sm font-semibold" data-testid={`environment-row-${row.id}`}>{row.name}</span>
          <code className="block truncate text-xs opacity-60">{row.slug}</code>
        </div>
      ),
    },
    {
      id: "backend",
      groupable: false,
      header: t("settings.environments.col.backend"),
      width: 170,
      render: (row) => {
        const strength = backendOf(vocabulary, row.backend)?.strength;
        return <Pill size="sm" tone="slate" data-testid={`environment-backend-${row.id}`}>{row.backend}{strength === undefined ? "" : ` · ${strength}`}</Pill>;
      },
    },
    {
      id: "bound",
      groupable: false,
      header: t("settings.environments.col.boundTo"),
      width: 260,
      render: (row) => (
        <span className="flex flex-wrap gap-1 text-xs">
          {bindingsOf(row).map((binding) => (
            <span key={`${binding.scopeKind}-${binding.componentId ?? ""}`} data-testid={`environment-binding-${row.id}`}>
              {binding.componentId
                ? t("settings.environments.row.boundComponent", { scope: binding.scopeKind, component: names.get(binding.componentId) ?? binding.componentId })
                : t("settings.environments.row.boundScope", { scope: binding.scopeKind })}
            </span>
          ))}
        </span>
      ),
    },
    {
      id: "notes",
      groupable: false,
      header: t("settings.environments.col.notes"),
      width: 260,
      render: (row) => (
        <span className="flex flex-wrap gap-1">
          {row.legacy === true && <Pill size="sm" tone="amber" data-testid={`environment-legacy-${row.id}`}>{t("settings.environments.row.legacy")}</Pill>}
          {row.archivedAt !== undefined && <Pill size="sm" tone="slate">{t("settings.environments.row.archived")}</Pill>}
          <Pill size="sm" tone="slate" data-testid={`environment-origin-${row.id}`}>{row.origin}</Pill>
        </span>
      ),
    },
    {
      id: "changed",
      groupable: false,
      header: t("settings.environments.col.changed"),
      width: 190,
      render: (row) => {
        const change = changes.get(row.id);
        return change ? <span className="text-xs opacity-70" data-testid={`environment-changed-${row.id}`}>{change.actorId} <RelativeTime iso={change.at} /></span> : null;
      },
    },
  ];

  return (
    <div className="flex min-h-[20rem] flex-1 flex-col gap-3">
      <div className="flex shrink-0 flex-wrap items-center gap-3" data-testid="environments-filter-bar">{filterBar}</div>

      {error && (
        <Alert className="shrink-0" intent="danger" title={t("settings.environments.list.failed")} description={<span className="whitespace-pre-line">{error}</span>} data-testid="environments-list-failed">
          <Button size="sm" onClick={onRetry}>{t("settings.environments.actions.retry")}</Button>
        </Alert>
      )}

      <div className="min-h-[18rem] flex-1" data-testid="environments-list">
        <Table<DisplayRow>
          variant="outlined"
          data={[...rows]}
          columns={columns}
          rowKey={(row) => row.id}
          corner="rounded-sm"
          loading={loading}
          onRowClick={(row) => onOpen(row.id)}
          emptyState={error ? <span /> : <div data-testid="environments-list-empty"><EmptyState title={t("settings.environments.list.emptyTitle")} subtitle={t("settings.environments.list.emptyBody")} /></div>}
          fullHeight
        />
      </div>
    </div>
  );
}
