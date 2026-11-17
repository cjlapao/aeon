import { useI18n } from "@cjlapao/ui-kit";
import { EmptyState, Pill } from "../../../components/kit";
import { stateBadge, type AgentGrantRow } from "./toolsModel";

// The per-agent half of the tool panel (TC9/TC12, SD7): one entry per agent that EXISTS — the
// daemon sends every agent it knows, not every agent that holds the tool, which is what makes a
// missing row answerable. Each is: the agent, the state, the REASON the engine gave (verbatim —
// a rewritten reason is a second verdict), and where the grant came from as small chips. An agent
// that is not dispatchable is shown AS such and gets no grant state (SD7).

export interface AgentMatrixProps {
  rows: AgentGrantRow[];
}

export function AgentMatrix({ rows }: AgentMatrixProps) {
  const { t } = useI18n();

  if (rows.length === 0) {
    return <EmptyState title={t("settings.tools.drawer.matrix.empty")} subtitle={t("settings.tools.drawer.matrix.none")} />;
  }

  return (
    <ul className="divide-y divide-[var(--border)]" data-testid="tools-matrix">
      {rows.map((row) => {
        const badge = stateBadge(row.state);
        return (
          <li key={row.agent} className="space-y-1 py-2" data-testid={`tools-matrix-row-${row.agent}`}>
            <div className="flex flex-wrap items-center gap-2">
              <span className="text-sm font-semibold">{row.agent}</span>
              {row.dispatchable ? (
                <Pill tone={badge.tone} size="sm" data-testid={`tools-matrix-state-${row.agent}`}>{badge.text}</Pill>
              ) : (
                <span className="text-xs opacity-70" data-testid={`tools-matrix-undispatchable-${row.agent}`}>{t("settings.tools.drawer.matrix.notDispatchable")}</span>
              )}
              <span className="flex flex-wrap gap-1" data-testid={`tools-matrix-granted-by-${row.agent}`}>
                {(row.granted_by ?? []).map((origin) => <Pill key={origin} tone="slate" size="sm">{origin}</Pill>)}
              </span>
            </div>
            <p className="text-xs opacity-80" data-testid={`tools-matrix-reason-${row.agent}`}>
              {row.reason ? row.reason : <span className="opacity-60">{t("settings.tools.drawer.matrix.noReason")}</span>}
            </p>
          </li>
        );
      })}
    </ul>
  );
}
