import { useMemo, useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, EmptyState, Pill, Table, type TableColumn } from "../../../components/kit";
import { ErrorPanel } from "../../../components/ErrorPanel";
import { RpcError } from "../../../lib/bridge";
import { useRpcQuery } from "../../../lib/useRpcQuery";
import { AgentTagPicker } from "../../../components/AgentTagPicker";
import type { LiveController, LiveState } from "../../../lib/live";
import { GrantFlow } from "./GrantFlow";
import { grantCellState, type GrantsAnswer } from "./mcpModel";

/**
 * The Grants tab (MG: the matrix shows S12's states AND reasons — the
 * `GrantView` rows render the daemon's state word and its reason sentence
 * verbatim, `null` core renders "whole core set" — a distinct fact from
 * "none" — and nothing here rewrites a state or composes a reason).
 */
export interface GrantsMatrixProps {
  controller: LiveController;
  live: LiveState;
  canWrite: boolean;
}

export function GrantsMatrix({ controller, live, canWrite }: GrantsMatrixProps) {
  const { t } = useI18n();
  const [agent, setAgent] = useState("");

  const query = useRpcQuery("mcp.grants", { agent }, controller, live);
  const answer = useMemo(() => {
    const data = query.data as unknown as GrantsAnswer | undefined;
    return data && data.agent === agent && agent !== "" ? data : undefined;
  }, [query.data, agent]);

  const H = "text-xs font-semibold uppercase tracking-widest opacity-70";
  const columns: TableColumn<GrantsAnswer["grants"][number]>[] = [
    { id: "tool", groupable: false, header: t("settings.mcp.grants.col.tool"), render: (view) => <code className="text-xs">{view.qualified}</code> },
    { id: "tier", groupable: false, header: t("settings.mcp.grants.col.tier"), width: 110, render: (view) => <span className="text-xs">{view.tier}</span> },
    {
      id: "state",
      groupable: false,
      header: t("settings.mcp.grants.col.state"),
      width: 200,
      render: (view) => {
        const cell = grantCellState(view);
        return (
          <span className="flex flex-wrap items-center gap-1">
            <Pill size="sm" tone={cell.tone} data-testid="grant-state">{cell.state}</Pill>
            {cell.unclassified && <Pill size="sm" tone="amber" data-testid="grant-unclassified">{t("settings.mcp.tool.unclassified")}</Pill>}
          </span>
        );
      },
    },
    // The reason VERBATIM — MC10's "no allow" sentence included, unwritten and unedited here.
    { id: "reason", groupable: false, header: t("settings.mcp.grants.col.reason"), render: (view) => <span className="text-xs" data-testid="grant-reason">{view.reason ?? ""}</span> },
  ];

  return (
    <div className="space-y-8" data-testid="grants-matrix">
      <p className="text-sm opacity-80">{t("settings.mcp.grants.subtitle")}</p>
      {!canWrite && <Alert intent="warning" title={t("settings.mcp.reconnecting")} />}
      <section className="space-y-3">
        <h3 className={H}>{t("settings.mcp.grants.qAgent")}</h3>
        <div className="max-w-md">
          <AgentTagPicker value={agent} onChange={setAgent} controller={controller} live={live} />
        </div>
      </section>

      {agent === "" ? (
        <EmptyState title={t("settings.mcp.grants.pickAgent")} />
      ) : query.loading && !answer ? (
        <EmptyState title={t("settings.mcp.grants.loading")} />
      ) : query.error && !answer ? (
        <ErrorPanel error={new RpcError(0, query.error)} onRetry={query.refetch} />
      ) : answer ? (
        <>
          <section className="space-y-3">
            <h3 className={H}>{t("settings.mcp.grants.qHas")}</h3>
            <div className="flex flex-wrap items-center gap-2 text-sm" data-testid="core-set">
              <span className="text-xs opacity-60">{t("settings.mcp.grants.core.label")}</span>
              {answer.core === null ? (
                <Pill size="sm" tone="blue" data-testid="core-whole">{t("settings.mcp.grants.coreWhole")}</Pill>
              ) : answer.core.length === 0 ? (
                <Pill size="sm" tone="slate" data-testid="core-none">{t("settings.mcp.grants.coreNone")}</Pill>
              ) : (
                answer.core.map((tool) => <Pill key={tool} size="sm" tone="blue">{tool}</Pill>)
              )}
            </div>
            {answer.declares.length > 0 && (
              <div className="flex flex-wrap items-center gap-2 text-sm" data-testid="declares-row">
                <span className="text-xs opacity-60">{t("settings.mcp.grants.declares")}</span>
                {answer.declares.map((qualified) => <Pill key={qualified} size="sm" tone="violet">{qualified}</Pill>)}
              </div>
            )}
            {answer.grants.length === 0 ? (
              // MC5: declaring nothing grants nothing — the emptiness said.
              <EmptyState title={t("settings.mcp.grants.nothingDeclared")} />
            ) : (
              <div data-testid="grant-table">
                <Table<GrantsAnswer["grants"][number]> variant="outlined" data={answer.grants} columns={columns} rowKey={(view) => view.qualified} corner="rounded-sm" />
              </div>
            )}
          </section>
          <GrantFlow agent={agent} answer={answer} canWrite={canWrite} controller={controller} live={live} onChanged={query.refetch} />
        </>
      ) : null}
    </div>
  );
}
