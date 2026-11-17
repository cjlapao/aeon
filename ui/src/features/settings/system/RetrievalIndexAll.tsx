import { useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Button, Pill } from "../../../components/kit";
import { refusalBody, toRpcError } from "../../../lib/bridge";
import { notifications } from "../../../lib/notifications";
import { OperationConfirm } from "./OperationConfirm";
import { fetchEmbeddingRoutes, fetchReindexPlan, fetchWorker, startReindex } from "./systemRpc";
import { blockFlying, costWords, fmtInt, startedWordsOf, type ReindexPlanResult, type RetrievalBlock } from "./systemModel";

interface Worker { state?: string; error?: string; result?: { files?: number; chunksWritten?: number; embedded?: number } }
const TERMINAL = new Set(["finished", "failed", "cancelled"]);
const POLL_MS = 2500;
const POLL_LIMIT = 720; // half an hour, the longest a pass may take before the watch gives up

/**
 * A pass runs off the connection, so starting it answers at once and says nothing about how it went. This follows the
 * worker until it ends and says so: what was indexed, or why it failed. Without it a pass that fails the moment it
 * starts (no embedding route, an unreachable provider) looks exactly like one that never started.
 */
function follow(workerId: string, corpus: string, done: (w: Worker) => void, tell: (title: string, body: string, tone: "info" | "danger") => void): void {
  let polls = 0;
  const tick = async () => {
    polls += 1;
    try {
      const w: Worker = await fetchWorker(workerId);
      if (w.state && TERMINAL.has(w.state)) {
        done(w);
        if (w.state === "failed") tell(`Indexing ${corpus} failed`, w.error || "The pass ended with an error and gave no reason.", "danger");
        else if (w.state === "finished") tell(`Indexed ${corpus}`, `${w.result?.files ?? 0} files, ${w.result?.chunksWritten ?? 0} chunks written${w.result?.embedded ? `, ${w.result.embedded} embedded` : ""}.`, "info");
        return;
      }
    } catch { /* the daemon may be busy; try again */ }
    if (polls < POLL_LIMIT) setTimeout(() => void tick(), POLL_MS);
  };
  setTimeout(() => void tick(), POLL_MS);
}

/** The two corpora an index covers: the project's code, and its history (what agents did and learned). */
export const CORPORA = ["code", "history"] as const;

export interface RetrievalIndexAllProps {
  /** The project root whose search index this builds. */
  componentRoot: string;
  block: RetrievalBlock | undefined;
  canWrite: boolean;
  onChanged: () => void;
}

interface Plans { code?: ReindexPlanResult; history?: ReindexPlanResult }

/**
 * One button for the whole search index of a project: Index when nothing has been embedded yet, Reindex when
 * something has. The embedding spends money per corpus, so clicking asks for each corpus's plan first (what is stale,
 * the token estimate, the cost with its state) and shows them together; confirming starts both passes, which record
 * everything.
 */
export function RetrievalIndexAll({ componentRoot, block, canWrite, onChanged }: RetrievalIndexAllProps) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const [plans, setPlans] = useState<Plans | undefined>();
  const [busy, setBusy] = useState(false);
  const rows = (block?.components ?? []).filter((c) => c.component === componentRoot || (c.corpora ?? []).length > 0);
  const indexed = rows.some((c) => (c.corpora ?? []).some((x) => x.files > 0));
  const flying = blockFlying(block?.state) || rows.some((c) => (c as { flying?: boolean }).flying === true);
  const label = indexed ? t("settings.system.actions.reindexAll") : t("settings.system.actions.indexAll");

  const begin = async () => {
    // Indexing embeds, and embedding needs a model that can: say so now, instead of failing in the background.
    try {
      if ((await fetchEmbeddingRoutes()).length === 0) {
        notifications.push({
          title: "No embedding model is set up",
          body: "Indexing the search needs a model with the Embedding capability. Add one in Models (an OpenAI-compatible model that serves an embeddings endpoint), tick Embedding, save it, then Index again.",
          tone: "danger", source: "local",
        });
        return;
      }
    } catch { /* could not tell: let the plan and the pass speak for themselves */ }
    setOpen(true); setPlans(undefined); setBusy(true);
    try {
      const [code, history] = await Promise.all(CORPORA.map((corpus) => fetchReindexPlan(componentRoot, corpus) as Promise<ReindexPlanResult>));
      setPlans({ code, history });
    } catch (cause) {
      // An unpriced route refuses on the plan, in the daemon's words; nothing is left to confirm.
      notifications.push({ title: t("settings.system.retrieval.refused"), body: refusalBody(toRpcError(cause)), tone: "danger", source: "local" });
      setOpen(false);
    } finally { setBusy(false); }
  };

  const confirm = async () => {
    setBusy(true);
    try {
      for (const corpus of CORPORA) {
        const answer = await startReindex(componentRoot, corpus);
        const starter = startedWordsOf(answer);
        if (starter !== undefined) notifications.push({ title: t("settings.system.retrieval.alreadyFlying"), body: t("settings.system.startedBy", { by: starter.by, at: starter.at }), tone: "info", source: "local" });
        const workerId = (answer as { workerId?: string } | undefined)?.workerId;
        if (workerId) {
          notifications.push({ title: `Indexing ${corpus}`, body: "The pass has started; you will be told when it ends.", tone: "info", source: "local" });
          follow(workerId, corpus, () => onChanged(), (title, body, tone) => notifications.push({ title, body, tone, source: "local" }));
        }
      }
      setOpen(false);
      onChanged();
    } catch (cause) {
      notifications.push({ title: t("settings.system.retrieval.refused"), body: refusalBody(toRpcError(cause)), tone: "danger", source: "local" });
    } finally { setBusy(false); }
  };

  return <>
    <Button size="sm" disabled={!canWrite || flying || busy} title={!canWrite || flying ? t("settings.system.disabled") : undefined}
      onClick={() => void begin()} data-testid="search-index-all">{flying ? t("settings.system.actions.indexing") : label}</Button>
    <OperationConfirm open={open} title={t("settings.system.actions.indexAllTitle", { root: componentRoot })} confirmLabel={label}
      busy={busy || plans === undefined} onConfirm={() => void confirm()} onCancel={() => setOpen(false)}>
      <div className="space-y-3 text-sm" data-testid="index-all-plan">
        {plans === undefined && <p className="text-xs opacity-70">{t("settings.system.retrieval.confirm.pricing")}</p>}
        {plans !== undefined && CORPORA.map((corpus) => {
          const plan = plans[corpus];
          if (!plan) return null;
          const cost = costWords(plan.cost);
          return <div key={corpus} data-testid={`index-all-${corpus}`}>
            <p className="font-medium">{corpus}</p>
            <p>{t("settings.system.retrieval.confirm.counts", { files: fmtInt(plan.files), staleFiles: fmtInt(plan.staleFiles), staleChunks: fmtInt(plan.staleChunks) })}</p>
            <p>{t("settings.system.retrieval.confirm.tokens", { tokens: fmtInt(plan.estimatedInputTokens), basis: plan.estimateBasis })}</p>
            <p className="flex items-center gap-2">
              <Pill tone={cost.tone}>{cost.state}</Pill>
              {cost.figure !== undefined && <span>{cost.figure}</span>}
              {cost.reason !== undefined && <span className="text-xs opacity-70">{cost.reason}</span>}
            </p>
          </div>;
        })}
        <p className="text-xs opacity-70">{t("settings.system.retrieval.confirm.estimateNotInvoice")}</p>
      </div>
    </OperationConfirm>
  </>;
}
