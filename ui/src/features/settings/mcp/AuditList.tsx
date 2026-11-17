import { useMemo } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { EmptyState } from "../../../components/kit";
import { ErrorPanel } from "../../../components/ErrorPanel";
import { RpcError } from "../../../lib/bridge";
import { useRpcQuery } from "../../../lib/useRpcQuery";
import type { LiveController, LiveState } from "../../../lib/live";

/**
 * The Audit tab: `mcp.changes`, newest first (the daemon's order — no
 * client-side re-sort), summaries as plain text. The summaries are the
 * daemon's fixed templates (ids, never payloads); nothing here interprets
 * them and nothing renders them as anything but text.
 */
export interface AuditListProps {
  controller: LiveController;
  live: LiveState;
  server?: string;
}

export function AuditList({ controller, live, server }: AuditListProps) {
  const { t } = useI18n();
  const query = useRpcQuery("mcp.changes", server === undefined ? { limit: 50 } : { server, limit: 50 }, controller, live);
  const rows = useMemo(() => query.data?.changes ?? [], [query.data]);

  return (
    <section className="space-y-4" data-testid="audit-list">
      <p className="text-sm opacity-80">{t("settings.mcp.audit.subtitle")}</p>
      {query.loading && rows.length === 0 ? (
        <EmptyState title={t("settings.mcp.audit.loading")} />
      ) : query.error ? (
        <ErrorPanel error={new RpcError(0, query.error)} onRetry={query.refetch} />
      ) : rows.length === 0 ? (
        // The EMPTY TABLE is the daemon's sentence (MCPChangesNoChangesStatement)
        // or the filter's answer — both the daemon's words, never UI prose.
        <EmptyState title={query.data?.statement ?? t("settings.mcp.audit.filteredEmpty")} />
      ) : (
        // A timeline, the way the Agents page lists its changes: what happened first, then when, by whom.
        <ul className="space-y-3 text-sm" data-testid="audit-table">
          {rows.map((row) => (
            <li key={row.id} className="border-l-2 border-white/15 pl-3" data-testid="audit-row">
              {/* Plain text — a summary is never markup. */}
              <div data-testid="audit-summary">
                {row.summary}
                {row.server !== "" && <span className="ml-1 opacity-70">· {row.server}</span>}
                {row.agent !== "" && <span className="ml-1 opacity-70">· {row.agent}</span>}
                {row.qualified !== "" && <span className="ml-1 font-mono opacity-70">· {row.qualified}</span>}
              </div>
              <div className="text-xs opacity-60">
                <time dateTime={row.occurredAt}>{row.occurredAt}</time> · <span className="font-mono" data-testid="audit-op">{row.op}</span> · {row.actorKind}/{row.actorId}
              </div>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
