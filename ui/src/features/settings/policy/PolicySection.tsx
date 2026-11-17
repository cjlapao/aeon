import { useCallback, useEffect, useMemo, useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, ContentActions, EmptyState, Pill, Tabs } from "../../../components/kit";
import { ErrorPanel } from "../../../components/ErrorPanel";
import { RpcError, rpcTyped } from "../../../lib/bridge";
import { useRpcQuery } from "../../../lib/useRpcQuery";
import { useRpcAction } from "../../../lib/useRpcAction";
import { useTypeToConfirm } from "../../../lib/useConfirm";
import { AgentTagsPicker } from "../../../components/AgentTagPicker";
import type { LiveController, LiveState } from "../../../lib/live";
import { PolicyFilters } from "./PolicyFilters";
import { PolicyList } from "./PolicyList";
import { PolicyDetailDrawer } from "./PolicyDetailDrawer";
import { useInRouterContext, useSearchParams } from "react-router-dom";
import { PolicyForm } from "./PolicyForm";
import { OverlaySidePanel } from "../../../components/OverlaySidePanel";
import { ProposalsDesk } from "./ProposalsDesk";
import { InForceExplorer } from "./InForceExplorer";
import { AuthoringAudit } from "./AuthoringAudit";
import {
  applyFilters,
  emptyFilterDraft,
  groupSeedRecordsByAgent,
  seedPreviewRows,
  toolPickerInfo,
  type PolicyFilterDraft,
  type SeedAgentCount,
  type Vocabulary,
} from "./policyModel";
import type { RpcMethods } from "../../../proto/contract";

/**
 * The Policy section of the real Settings screen (PM12) — not a bench tab:
 * `benchRegistry` does not apply and the bench `PolicyPage`/`ProposalsPage`
 * stay untouched as diagnostics.
 *
 * Four tabs: **Rules · Proposals · In force · Audit**. The section holds the
 * `policy.vocabulary` load once per session (cached in state seeded from the
 * response, re-read by the hook's reconnect generation), the filter draft,
 * and the list query — so filters travel as `policy.list` **params** and the
 * rows shown are exactly what the daemon answered. Everything the daemon
 * sends — describe sentences, matrix reasons, before/after `Render()`
 * output, empty statements — renders verbatim; the section hardcodes no
 * closed set, no authorship rule and no sentence. Nothing persists to
 * browser storage (§4c): component state is seeded from the last response
 * only.
 */
export interface PolicySectionProps {
  controller: LiveController;
  live: LiveState;
  /** @default `live.state === "current"` — writes are refused while not current. */
  canWrite?: boolean;
}

type TabId = "rules" | "proposals" | "inforce" | "audit" | "shipped";

/** The S43-extra set id this section is the view of — the task names it;
 *  it is the same set the System records panel lists via `seed.status`. */
const POLICY_CORPUS_SET = "policy.corpus";

/** The per-agent dry-run answer plus the engine's per-record states joined. */
interface SeedPreview {
  perAgent: SeedAgentCount[];
  seeded: number;
  unchanged: number;
}

function SeedCorpusBlock({
  controller,
  live,
  canWrite,
  onChanged,
}: {
  controller: LiveController;
  live: LiveState;
  canWrite: boolean;
  onChanged: () => void;
}) {
  const { t } = useI18n();
  const action = useRpcAction();
  const { confirm, modal } = useTypeToConfirm();
  const [agents, setAgents] = useState<string[]>([]);
  const [preview, setPreview] = useState<SeedPreview | null>(null);
  const [applied, setApplied] = useState<string | undefined>(undefined);

  // Named set, not the whole registry: this block is the view of ONE set
  // (POLICY_CORPUS_SET below picks it out of the answer), and asking for
  // every set costs the engine a status walk of all fifteen — the slowest
  // thing on this page by two orders of magnitude, and the reason the page
  // took seconds to answer for a corpus of a few hundred rows.
  const status = useRpcQuery("seed.status", { set: POLICY_CORPUS_SET }, controller, live);
  const set = useMemo(
    () => status.data?.sets.find((candidateSet) => candidateSet.id === POLICY_CORPUS_SET),
    [status.data],
  );

  const runDry = useCallback(async () => {
    setApplied(undefined);
    setPreview(null);
    const result = await action.run(
      () => rpcTyped("policy.seedCorpus", { agents, dryRun: true }),
      { errorTitle: t("settings.policy.seed.dryRunFailed") },
    );
    if (result) setPreview({ perAgent: result.perAgent ?? [], seeded: result.seeded, unchanged: result.unchanged });
  }, [action, agents, t]);

  const runReal = useCallback(async () => {
    if (!preview) return;
    // The guard confirmation: this is a GUARD set — it adds `allow` rows,
    // so it is never one-clicked (the engine's own class rule, honoured at
    // the human door). Decline sends nothing.
    const confirmed = await confirm({
      title: t("settings.policy.seed.guardTitle", { set: POLICY_CORPUS_SET }),
      description: t("settings.policy.seed.guardBody", { set: POLICY_CORPUS_SET, seeded: preview.seeded, unchanged: preview.unchanged }),
      confirmValue: POLICY_CORPUS_SET,
      confirmValueLabel: t("settings.policy.seed.confirmValueLabel"),
      confirmLabel: t("settings.policy.seed.apply"),
    });
    if (!confirmed) return;
    const result = await action.run(
      () => rpcTyped("policy.seedCorpus", { agents, dryRun: false }),
      { errorTitle: t("settings.policy.seed.applyFailed") },
    );
    if (result) {
      setApplied(t("settings.policy.seed.applied", { seeded: result.seeded, unchanged: result.unchanged }));
      setPreview(null);
      status.refetch();
      onChanged();
    }
  }, [action, agents, confirm, onChanged, preview, status, t]);

  const restore = useCallback(
    async (key: string) => {
      const record = set?.records.find((candidateRecord) => candidateRecord.key === key);
      const confirmed = await confirm({
        title: t("settings.policy.seed.restoreConfirmTitle", { set: POLICY_CORPUS_SET, key }),
        // The engine's own reason for the record, carried verbatim.
        description: `${record?.reason ?? ""}`,
        confirmValue: key,
        confirmValueLabel: t("settings.policy.seed.confirmValueLabel"),
        confirmLabel: t("settings.policy.seed.restore"),
      });
      if (!confirmed) return;
      const result = await action.run(
        () => rpcTyped("seed.restore", { set: POLICY_CORPUS_SET, key }),
        { errorTitle: t("settings.policy.seed.restoreFailed", { key }) },
      );
      if (result) status.refetch();
    },
    [action, confirm, set, status, t],
  );

  const engineStates = set ? groupSeedRecordsByAgent(set.records) : undefined;
  const rows = seedPreviewRows(preview?.perAgent, engineStates);
  const removedRecords = (set?.records ?? []).filter((record) => record.state === "removed");
  const versionStale = set !== undefined && set.availableVersion !== set.appliedVersion;

  return (
    <section className="space-y-3 pb-6" aria-label={t("settings.policy.seed.title")} data-testid="seed-corpus-block">
      <h2 className="text-lg font-semibold">{t("settings.policy.seed.title")}</h2>
      <p className="text-sm opacity-80">{t("settings.policy.seed.subtitle")}</p>
      <div className="space-y-3">
        {action.error && <Alert intent="danger" title={action.error} />}

        {status.error ? (
          <ErrorPanel error={new RpcError(0, status.error)} onRetry={status.refetch} />
        ) : set === undefined && !status.loading ? (
          // The set simply is not declared on this daemon — said as such.
          <EmptyState title={t("settings.policy.seed.noSet", { set: POLICY_CORPUS_SET })} />
        ) : null}

        {set && (
          <div className="flex flex-wrap items-center gap-2 text-xs" data-testid="seed-versions">
            <Pill tone="slate">{t("settings.policy.seed.available", { version: set.availableVersion })}</Pill>
            <Pill tone="slate">{t("settings.policy.seed.appliedVersion", { version: set.appliedVersion })}</Pill>
            {versionStale && (
              <span className="text-amber-600 dark:text-amber-400" data-testid="seed-version-changed">
                {t("settings.policy.seed.versionChanged")}
              </span>
            )}
          </div>
        )}

        <div className="space-y-2">
          <p className="text-xs uppercase tracking-wide opacity-70">{t("settings.policy.seed.agents")}</p>
          <AgentTagsPicker value={agents} onChange={setAgents} controller={controller} live={live} />
          {agents.length === 0 && <p className="text-xs opacity-70">{t("settings.policy.seed.agentsAll")}</p>}
        </div>

        <Button size="sm" disabled={!canWrite || action.busy} onClick={() => void runDry()} title={!canWrite ? t("settings.policy.reconnecting") : undefined}>
          {t("settings.policy.seed.dryRun")}
        </Button>

        {preview && (
          <div className="space-y-2" data-testid="seed-preview">
            <p className="text-sm">{t("settings.policy.seed.previewTitle")}</p>
            <table className="w-full border-collapse text-sm" data-testid="seed-counts-table">
              <thead>
                <tr className="text-left text-xs uppercase tracking-wide opacity-70">
                  <th className="px-2 py-1">{t("settings.policy.seed.col.agent")}</th>
                  <th className="px-2 py-1">{t("settings.policy.seed.col.seeded")}</th>
                  <th className="px-2 py-1">{t("settings.policy.seed.col.unchanged")}</th>
                  <th className="px-2 py-1">{t("settings.policy.seed.col.removed")}</th>
                  <th className="px-2 py-1">{t("settings.policy.seed.col.diverged")}</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((row) => (
                  <tr key={row.agent} className="border-t border-slate-200 dark:border-slate-800" data-testid="seed-count-row">
                    <td className="px-2 py-1 font-mono text-xs">{row.agent}</td>
                    <td className="px-2 py-1">{row.seeded}</td>
                    <td className="px-2 py-1">{row.unchanged}</td>
                    {/* removed/diverged come from the engine's per-record
                        states on seed.status — the daemon's words, counted. */}
                    <td className="px-2 py-1">{row.states["removed"] ?? 0}</td>
                    <td className="px-2 py-1">{row.states["diverged"] ?? 0}</td>
                  </tr>
                ))}
              </tbody>
            </table>
            <Button size="sm" disabled={!canWrite || action.busy} onClick={() => void runReal()} title={!canWrite ? t("settings.policy.reconnecting") : undefined}>
              {t("settings.policy.seed.apply")}
            </Button>
          </div>
        )}

        {applied && <Alert intent="success" title={applied} data-testid="seed-applied" />}

        {removedRecords.length > 0 && (
          <div className="space-y-1" data-testid="seed-removed">
            <p className="text-xs uppercase tracking-wide opacity-70">{t("settings.policy.seed.removedTitle")}</p>
            <ul className="space-y-1 text-sm">
              {removedRecords.map((record) => (
                <li key={record.key} className="flex flex-wrap items-center gap-2" data-testid="seed-removed-row">
                  <span className="font-mono text-xs">{record.key}</span>
                  <span className="text-rose-600 dark:text-rose-400">{t("settings.policy.seed.removedRestore")}</span>
                  <Button
                    size="xs"
                    variant="outline"
                    disabled={!canWrite || action.busy}
                    onClick={() => void restore(record.key)}
                    title={!canWrite ? t("settings.policy.reconnecting") : undefined}
                  >
                    {t("settings.policy.seed.restore")}
                  </Button>
                </li>
              ))}
            </ul>
          </div>
        )}
      </div>
      {modal}
    </section>
  );
}

export function PolicySection({ controller, live, canWrite = live.state === "current" }: PolicySectionProps) {
  const { t } = useI18n();
  const [tab, setTab] = useState<TabId>("rules");
  const [filters, setFilters] = useState<PolicyFilterDraft>(emptyFilterDraft());
  const [selectedId, setSelectedId] = useState<string | undefined>(undefined);
  const [adding, setAdding] = useState(false);
  // S74 T4: "Write a policy for this tool" arrives as /config/policy?tool=<address>.
  const [prefillTool, setPrefillTool] = useState("");
  const inRouter = useInRouterContext();

  // The vocabulary is read once per session and CACHED in component state
  // seeded from the response; the hook's generation-keyed refetch refreshes
  // it after a reconnect, and nothing else re-reads it per render (PM6).
  const vocabularyQuery = useRpcQuery("policy.vocabulary", {}, controller, live);
  const [vocabulary, setVocabulary] = useState<Vocabulary | undefined>(undefined);
  useEffect(() => {
    if (vocabularyQuery.data?.vocabulary) setVocabulary(vocabularyQuery.data.vocabulary);
  }, [vocabularyQuery.data]);

  const listQuery = useRpcQuery("policy.list", applyFilters(filters), controller, live);

  // The origin filter's options: the provenance words the daemon has been
  // seen to send, remembered across reads (never a UI-named set).
  const [seenOrigins, setSeenOrigins] = useState<string[]>([]);
  useEffect(() => {
    const rows = listQuery.data?.policies ?? [];
    setSeenOrigins((current) => {
      const next = [...current];
      for (const row of rows) {
        if (row.origin && !next.includes(row.origin)) next.push(row.origin);
      }
      return next.length === current.length ? current : next;
    });
  }, [listQuery.data]);

  const rows = useMemo(() => listQuery.data?.policies ?? [], [listQuery.data]);
  const refreshList = useCallback(() => {
    listQuery.refetch();
    setSelectedId(undefined);
  }, [listQuery]);

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto" data-testid="policy-section">
      <ContentActions
        className="shrink-0"
        gap="md"
        align="end"
        data-testid="policy-header"
        actions={
          <Button size="sm" disabled={!canWrite || adding} onClick={() => setAdding(true)} title={!canWrite ? t("settings.policy.reconnecting") : undefined} data-testid="policy-grant">
            {t("settings.policy.quick.open")}
          </Button>
        }
      >
        <div>
          <h1 className="text-3xl font-semibold">{t("settings.policy.title")}</h1>
          <p className="mt-1 text-sm opacity-70">{t("settings.policy.subtitle")}</p>
        </div>
      </ContentActions>
      {!canWrite && <Alert className="shrink-0" intent="warning" title={t("settings.policy.reconnecting")} data-testid="policy-reconnecting" />}
      <div className="shrink-0">
        <Tabs
          variant="underline"
          items={[
            { id: "rules", label: t("settings.policy.tabs.rules") },
            { id: "proposals", label: t("settings.policy.tabs.proposals") },
            { id: "inforce", label: t("settings.policy.tabs.inForce") },
            { id: "audit", label: t("settings.policy.tabs.audit") },
            { id: "shipped", label: t("settings.policy.tabs.shipped") },
          ]}
          value={tab}
          onChange={(id) => setTab(id as TabId)}
        />
      </div>

      {tab === "rules" && (
        <>
          <div className="shrink-0" data-testid="policy-action-bar">
            <PolicyFilters
              draft={filters}
              vocabulary={vocabulary}
              originOptions={seenOrigins}
              onChange={setFilters}
              onClear={() => setFilters(emptyFilterDraft())}
              controller={controller}
              live={live}
            />
          </div>

          {inRouter && (
            <RoutePrefill onTool={(tool) => { setPrefillTool(tool); setAdding(true); }} />
          )}

          {vocabulary === undefined && vocabularyQuery.error ? (
            // The form and filters have nothing to be built from — said so,
            // not rendered as an empty dropdown universe.
            <ErrorPanel error={new RpcError(0, vocabularyQuery.error)} onRetry={vocabularyQuery.refetch} />
          ) : null}

          <PolicyList
            rows={rows}
            statement={listQuery.data?.statement}
            loading={listQuery.loading}
            error={listQuery.error}
            onRetry={listQuery.refetch}
            selectedId={selectedId}
            onSelect={(id) => setSelectedId(id)}
          />

          {selectedId && (
            <PolicyDetailDrawer
              id={selectedId}
              controller={controller}
              live={live}
              canWrite={canWrite}
              onClose={() => setSelectedId(undefined)}
              onChanged={refreshList}
            />
          )}
        </>
      )}

      {tab === "proposals" && (
        <div className="min-h-0 flex-1 overflow-y-auto pr-5">
          <ProposalsDesk controller={controller} live={live} canWrite={canWrite} />
        </div>
      )}

      {tab === "inforce" && (
        <div className="min-h-0 flex-1 overflow-y-auto pr-5">
          <InForceExplorer vocabulary={vocabulary} controller={controller} live={live} />
        </div>
      )}

      {tab === "audit" && (
        <div className="min-h-0 flex-1 overflow-y-auto pr-5">
          <AuthoringAudit controller={controller} live={live} />
        </div>
      )}

      {tab === "shipped" && (
        <div className="min-h-0 flex-1 overflow-y-auto pr-5">
          <SeedCorpusBlock controller={controller} live={live} canWrite={canWrite} onChanged={() => listQuery.refetch()} />
        </div>
      )}

      {/* The new-rule panel stands over every tab — the header's button opens it from anywhere. */}
      {adding && (
        <OverlaySidePanel
          title={t("settings.policy.quick.title")}
          titleClassName="text-xl font-semibold"
          variant="simple"
          subtitle={t("settings.policy.quick.subtitle")}
          width={560}
          onClose={() => setAdding(false)}
        >
          <PolicyForm
            quick
            initial={prefillTool === "" ? undefined : toolInitial(vocabulary, prefillTool)}
            vocabulary={vocabulary}
            canWrite={canWrite}
            controller={controller}
            live={live}
            busy={false}
            onCancel={() => setAdding(false)}
            onAdded={() => {
              // Stay open: the result (the daemon's sentence and the before/after) is shown
              // under the form, and closing is the person's choice. The table behind re-reads.
              listQuery.refetch();
            }}
          />
        </OverlaySidePanel>
      )}
    </div>
  );
}

/** The seed door's params typing helper — keeps `agents` an array of real
 *  roster identities (the picker guarantees it). */
export type SeedCorpusParamsWire = RpcMethods["policy.seedCorpus"]["params"];

/** Reads `?tool=` off the route once and hands it up; renders nothing. A
 *  separate component so the router hook is only called where a router exists. */
function RoutePrefill({ onTool }: { onTool: (tool: string) => void }) {
  const [params] = useSearchParams();
  const tool = params.get("tool") ?? "";
  useEffect(() => {
    if (tool !== "") onTool(tool);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tool]);
  return null;
}

/** The tool a "Write a policy for this tool" link carries, as a form draft. */
function toolInitial(vocabulary: Vocabulary | undefined, tool: string) {
  const info = toolPickerInfo(vocabulary);
  return info ? { type: info.type, spec: { [info.field]: tool } } : undefined;
}
