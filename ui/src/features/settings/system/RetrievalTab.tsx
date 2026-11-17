import { useEffect, useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Button, EmptyState, Pill, Table, type TableColumn } from "../../../components/kit";
import { refusalBody, toRpcError } from "../../../lib/bridge";
import { notifications } from "../../../lib/notifications";
import { OperationConfirm } from "./OperationConfirm";
import { RetrievalIndexAll } from "./RetrievalIndexAll";
import { RetrievalProbe } from "./RetrievalProbe";
import { fetchReindexPlan, startReindex } from "./systemRpc";
import {
  blockFlying,
  componentsOf,
  corpusFlying,
  costWords,
  fmtInt,
  startedWordsOf,
  type ReindexPlanResult,
  type RetrievalBlock,
  type StatusComponent,
} from "./systemModel";

/**
 * RetrievalTab — the search index agents use to find files by meaning or keyword (S54 T6).
 *
 * One row per corpus the daemon reports, with its counts verbatim. The one act is Reindex, and its
 * confirm first asks `retrieval.reindexPlan` and shows the answer: stale counts, the token estimate
 * with its basis, and the cost WITH its state kept attached (known, partial with the daemon's reason,
 * or unknown — never a bare number, never an estimate dressed as an invoice). Only then does the click
 * dispatch `retrieval.reindex`. An unpriced route refuses on the plan, as a toast, and the pass never
 * starts; a pass already flying answers with its starter's words, shown verbatim, and the control
 * stands down while the daemon's own flag or state says a pass is running.
 *
 * Beside the table, the search probe asks the daemon what an agent would get back for a term.
 */
export interface RetrievalTabProps {
  block: RetrievalBlock | undefined;
  canWrite: boolean;
  onChanged: () => void;
  /** Platform status shows the whole-daemon table only; the search probe lives in a component's Knowledge tab. */
  hideProbe?: boolean;
  /** The project this index is for: when given, one Index / Reindex button covers its whole search index. */
  projectRoot?: string;
}

interface Pending {
  component: string;
  corpus: string;
}

interface CorpusRow {
  component: StatusComponent;
  name: string;
  files: number;
  staleChunks: number;
  flying: boolean;
}

export function RetrievalTab({ block, canWrite, onChanged, hideProbe = false, projectRoot }: RetrievalTabProps) {
  const { t } = useI18n();
  const [pending, setPending] = useState<Pending | null>(null);
  const [plan, setPlan] = useState<ReindexPlanResult | undefined>(undefined);
  const [busy, setBusy] = useState(false);
  const components = componentsOf(block);
  const flying = blockFlying(block?.state);

  // The plan is fetched when the confirm opens and never before — asking a plan starts nothing.
  useEffect(() => {
    if (pending === null) {
      setPlan(undefined);
      return;
    }
    let lived = true;
    void (async () => {
      try {
        const answer = await fetchReindexPlan(pending.component, pending.corpus);
        if (lived) setPlan(answer as ReindexPlanResult);
      } catch (cause) {
        // An unpriced route refuses on the plan, in the daemon's words; nothing is left to confirm.
        if (lived) {
          notifications.push({ title: t("settings.system.retrieval.refused"), body: refusalBody(toRpcError(cause)), tone: "danger", source: "local" });
          setPending(null);
        }
      }
    })();
    return () => {
      lived = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pending === null ? "" : `${pending.component}\u0000${pending.corpus}`]);

  const confirm = async () => {
    if (pending === null) return;
    setBusy(true);
    try {
      const answer = await startReindex(pending.component, pending.corpus);
      // An already-flying pass ANSWERS and names its starter; those words ride the toast verbatim.
      const starter = startedWordsOf(answer);
      if (starter !== undefined) {
        notifications.push({ title: t("settings.system.retrieval.alreadyFlying"), body: t("settings.system.startedBy", { by: starter.by, at: starter.at }), tone: "info", source: "local" });
      }
      setPending(null);
      onChanged();
    } catch (cause) {
      notifications.push({ title: t("settings.system.retrieval.refused"), body: refusalBody(toRpcError(cause)), tone: "danger", source: "local" });
    } finally {
      setBusy(false);
    }
  };

  const rows: CorpusRow[] = components.flatMap((component) =>
    (component.corpora ?? []).map((c) => ({ component, name: c.name, files: c.files, staleChunks: c.staleChunks, flying: corpusFlying(component, c.name) })),
  );
  const cost = costWords(plan?.cost);

  const columns: TableColumn<CorpusRow>[] = [
    {
      id: "index",
      groupable: false,
      header: t("settings.system.retrieval.corpus"),
      render: (row) => (
        <div className="w-0 min-w-full py-1" data-testid={`corpus-row-${row.component.component}-${row.name}`}>
          <span className="block truncate text-sm font-semibold">{row.name}</span>
          <code className="block truncate text-xs opacity-60">{row.component.component}</code>
        </div>
      ),
    },
    { id: "files", groupable: false, header: t("settings.system.retrieval.files"), width: 90, render: (row) => <span className="text-sm">{fmtInt(row.files)}</span> },
    { id: "staleChunks", groupable: false, header: t("settings.system.retrieval.staleChunks"), width: 130, render: (row) => <span className="text-sm">{fmtInt(row.staleChunks)}</span> },
    {
      id: "state",
      groupable: false,
      header: t("settings.system.retrieval.state"),
      width: 130,
      render: (row) =>
        row.flying ? (
          <Pill size="sm" tone="blue" data-testid={`corpus-flying-${row.component.component}-${row.name}`}>{t("settings.system.retrieval.flying")}</Pill>
        ) : row.files === 0 ? (
          <Pill size="sm" tone="slate">{t("settings.system.retrieval.emptyIndex")}</Pill>
        ) : row.staleChunks > 0 ? (
          <Pill size="sm" tone="amber">{t("settings.system.retrieval.hasStale")}</Pill>
        ) : (
          <Pill size="sm" tone="green">{t("settings.system.retrieval.current")}</Pill>
        ),
    },
    {
      id: "act",
      groupable: false,
      header: "",
      width: 120,
      render: (row) =>
        row.flying ? null : (
          <Button
            size="sm"
            variant="ghost"
            disabled={!canWrite || flying}
            title={!canWrite || flying ? t("settings.system.disabled") : undefined}
            onClick={() => setPending({ component: row.component.component, corpus: row.name })}
            data-testid={`reindex-${row.component.component}-${row.name}`}
          >
            {t("settings.system.actions.reindex")}
          </Button>
        ),
    },
  ];

  return (
    <div className={`grid min-h-[26rem] min-w-0 flex-1 gap-4 ${hideProbe ? "" : "lg:grid-cols-[minmax(0,1.3fr)_minmax(0,1fr)]"}`} data-testid="system-retrieval-tab">
      <div className="flex min-h-0 min-w-0 flex-col gap-3">
        {projectRoot !== undefined && <div className="flex justify-end"><RetrievalIndexAll componentRoot={projectRoot} block={block} canWrite={canWrite} onChanged={onChanged} /></div>}
        {components.map((component) => (
          <p key={component.component} className="text-xs opacity-70" data-testid={`retrieval-component-${component.component}`}>
            <span className="font-medium opacity-100">{component.component}</span>{" · "}
            {component.embedRoute !== undefined && component.embedRoute !== "" ? (
              <span data-testid="retrieval-embed">
                {t("settings.system.retrieval.embedRoute", { route: component.embedRoute, model: component.embedModel ?? "—", dim: component.embedDim ?? 0 })}
              </span>
            ) : (
              <span>{t("settings.system.retrieval.noEmbed")}</span>
            )}
          </p>
        ))}
        <div className="min-h-[16rem] min-w-0 flex-1">
          <Table<CorpusRow>
            variant="outlined"
            data={rows}
            columns={columns}
            rowKey={(row) => `${row.component.component}|${row.name}`}
            corner="rounded-sm"
            emptyState={<EmptyState title={t("settings.system.retrieval.none")} subtitle={t("settings.system.retrieval.noneBody")} />}
            fullHeight
          />
        </div>
      </div>

      {!hideProbe && <RetrievalProbe components={components} />}

      <OperationConfirm
        open={pending !== null}
        title={t("settings.system.retrieval.confirm.title", { corpus: pending?.corpus ?? "", root: pending?.component ?? "" })}
        confirmLabel={t("settings.system.actions.confirmReindex")}
        busy={busy || plan === undefined}
        onConfirm={() => void confirm()}
        onCancel={() => setPending(null)}
      >
        <div className="space-y-2 text-sm" data-testid="reindex-plan">
          {plan === undefined && <p className="text-xs opacity-70" data-testid="reindex-plan-loading">{t("settings.system.retrieval.confirm.pricing")}</p>}
          {plan !== undefined && (
            <>
              <p data-testid="reindex-plan-counts">
                {t("settings.system.retrieval.confirm.counts", { files: fmtInt(plan.files), staleFiles: fmtInt(plan.staleFiles), staleChunks: fmtInt(plan.staleChunks) })}
              </p>
              <p data-testid="reindex-plan-estimate">
                {t("settings.system.retrieval.confirm.tokens", { tokens: fmtInt(plan.estimatedInputTokens), basis: plan.estimateBasis })}
              </p>
              <p className="flex items-center gap-2" data-testid="reindex-plan-cost">
                <Pill tone={cost.tone}>{cost.state}</Pill>
                {cost.figure !== undefined && <span>{cost.figure}</span>}
                {cost.reason !== undefined && <span className="text-xs opacity-70">{cost.reason}</span>}
                <span className="text-xs opacity-70">{t("settings.system.retrieval.confirm.estimateNotInvoice")}</span>
              </p>
              {plan.willReplaceEmbeddings && <p className="text-xs opacity-70" data-testid="reindex-plan-replaces">{t("settings.system.retrieval.confirm.replaces")}</p>}
            </>
          )}
        </div>
      </OperationConfirm>
    </div>
  );
}
