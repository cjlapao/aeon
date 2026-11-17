import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Pill } from "../../components/kit";
import { useRoadmapHistory } from "./roadmapRpc";
import { isSystemActor } from "./roadmapModel";
import type { LiveController, LiveState } from "../../lib/live";

/**
 * HistoryPanel — the item's audit trail (S50 T7, RM6's "the how it got
 * there" door).
 *
 * Rows render newest-first EXACTLY as `roadmap.history` answers them; the
 * panel does not re-sort, re-phase or summarise a `summary` — the daemon
 * composed each line where the change happened, with the actor it recorded.
 *
 * The one visible decision made here: a `system` row (the reactor's
 * transition, the plan-host's stamp) is marked as what it is — a different
 * KIND of line, muted and labelled, never hidden and never dressed as a
 * person's decision. Person rows keep the id the audit carries verbatim
 * (`user:…`, `cli:…`); this file never renames an actor into another.
 */
export interface HistoryPanelProps {
  itemId: number;
  controller: LiveController;
  live: LiveState;
}

export function HistoryPanel({ itemId, controller, live }: HistoryPanelProps) {
  const { t } = useI18n();
  const history = useRoadmapHistory(itemId, controller, live);

  if (history.error !== undefined) {
    return <Alert intent="danger" title={history.error} data-testid="history-error" />;
  }
  if (history.data === undefined) {
    return (
      <p className="text-sm opacity-60" data-testid="history-loading">
        {history.loading ? t("roadmap.manage.history.loading") : t("roadmap.manage.history.empty")}
      </p>
    );
  }
  if (history.data.length === 0) {
    return <p className="text-sm opacity-60" data-testid="history-empty">{t("roadmap.manage.history.empty")}</p>;
  }
  return (
    <ol className="space-y-1.5" data-testid="history-panel">
      {history.data.map((row) => {
        const system = isSystemActor(row);
        return (
          <li
            key={row.id}
            data-testid={`history-row-${row.id}`}
            data-actor={row.actorKind}
            className={`rounded-lg border border-slate-200 p-2 text-xs dark:border-slate-700 ${
              system ? "bg-slate-50 opacity-70 dark:bg-slate-800/40" : ""
            }`}
          >
            <div className="flex flex-wrap items-center gap-2">
              <Pill tone={system ? "slate" : "purple"} data-testid={`history-actor-${row.id}`}>
                {/* The label names the KIND; the id below names the actor. */}
                {system ? t("roadmap.manage.history.systemActor") : row.actorId}
              </Pill>
              <span className="font-mono opacity-70">{row.op}</span>
              <span className="opacity-50">{row.occurredAt}</span>
            </div>
            {/* The daemon's sentence, byte for byte — history is not the
                place where a second voice paraphrases the first. */}
            <p className="pt-1" data-testid={`history-summary-${row.id}`}>{row.summary}</p>
          </li>
        );
      })}
    </ol>
  );
}
