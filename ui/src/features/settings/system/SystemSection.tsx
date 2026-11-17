import { useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, ContentActions, Tabs } from "../../../components/kit";
import { PageTitle } from "../../../components/PageTitle";
import type { LiveController, LiveState } from "../../../lib/live";
import { usePlatformChanges, usePlatformStatus } from "./systemRpc";
import { statusViewOf } from "./systemModel";
import { PlatformStrip } from "./PlatformStrip";
import { GraphRoots } from "./GraphRoots";
import { RetrievalTab } from "./RetrievalTab";
import { ActivityTab } from "./ActivityTab";

/**
 * SystemSection — Configuration → Platform status (S54 T6, SP8), reframed as the health of the WHOLE daemon.
 *
 * Index jobs across every component, what keeping them costs, and the ledger of what people started.
 * The graph and the search a person actually uses (explore, probe) live in each component's Knowledge tab A strip of three stat cards
 * (code graph, retrieval, usage) answers "is it healthy?" at a glance; the tabs below answer the rest:
 * **Code graph** (explore who calls what, and maintain the index), **Retrieval** (index health and a
 * search probe) and **Activity** (the ledger of every operation a person started).
 *
 * Nothing here is state-shaped by this file's opinion: the state words, the counts, the cost states
 * and the starter-naming refusals all ride the answers. The operations are the click-fronted twins of
 * the CLI's `--yes` seatbelt — the reindex confirm shows the daemon's priced plan first, the graph
 * confirms name the root and what the act does to DERIVED rows — and a refusal is a toast in the
 * daemon's own words. SP5's verdict rides the whole folder: the section writes NO setting, and the
 * only reads besides the platform plane's are the lookups the explorer and the search probe make.
 */
export interface SystemSectionProps {
  controller: LiveController;
  live: LiveState;
  /** @default `live.state === "current"` — writes are refused while not current. */
  canWrite?: boolean;
}

type Tab = "graph" | "retrieval" | "activity";

export function SystemSection({ controller, live, canWrite = live.state === "current" }: SystemSectionProps) {
  const { t } = useI18n();
  const [tab, setTab] = useState<Tab>("graph");
  const status = usePlatformStatus(controller, live);
  const changes = usePlatformChanges(controller, live);
  const view = statusViewOf(status.data);

  const title = (
    <PageTitle
      title={t("settings.system.title")}
      purpose={t("settings.system.subtitle")}
      help={{ title: t("settings.system.help.title"), content: t("settings.system.help.body") }}
    />
  );

  if (live.state !== "current") {
    return (
      <div className="flex min-h-0 min-w-0 flex-1 flex-col gap-4" data-testid="system-section">
        <ContentActions className="shrink-0" gap="md" align="end">{title}</ContentActions>
        <Alert intent="warning" title={t("settings.system.reconnecting")} data-testid="system-reconnecting" />
      </div>
    );
  }

  const reload = () => {
    status.refetch();
    changes.refetch();
  };

  return (
    <div className="flex min-h-0 min-w-0 w-full flex-1 flex-col gap-4 overflow-y-auto overflow-x-hidden" data-testid="system-section">
      <ContentActions
        className="shrink-0"
        gap="md"
        align="end"
        actions={
          <span className="flex items-center gap-3">
            {view?.evaluatedAt !== undefined && (
              <span className="text-xs opacity-70" data-testid="system-evaluated">{t("settings.system.evaluated", { at: view.evaluatedAt })}</span>
            )}
            <Button size="sm" variant="ghost" onClick={reload} data-testid="system-refresh">{t("settings.system.actions.refresh")}</Button>
          </span>
        }
      >
        {title}
      </ContentActions>

      {status.error !== undefined && (
        <Alert className="shrink-0" intent="danger" title={t("settings.system.status.failed")} description={status.error} data-testid="system-status-failed">
          <button type="button" onClick={status.refetch} data-testid="system-status-retry">{t("settings.system.actions.retry")}</button>
        </Alert>
      )}
      {status.loading && view === undefined && <p className="shrink-0 text-xs opacity-70" data-testid="system-loading">{t("settings.system.loading")}</p>}

      {view !== undefined && <PlatformStrip view={view} selected={tab} onOpen={setTab} />}

      <div className="shrink-0">
        <Tabs
          variant="underline"
          items={[
            { id: "graph", label: t("settings.system.tab.graph") },
            { id: "retrieval", label: t("settings.system.tab.retrieval") },
            { id: "activity", label: t("settings.system.tab.activity") },
          ]}
          value={tab}
          onChange={(id) => setTab(id as Tab)}
        />
      </div>
      <p className="shrink-0 text-sm opacity-70">{t(`settings.system.hint.${tab}`)}</p>

      {tab === "graph" && <GraphRoots block={view?.graph} canWrite={canWrite} onChanged={reload} />}
      {tab === "retrieval" && <RetrievalTab block={view?.retrieval} canWrite={canWrite} onChanged={reload} hideProbe />}
      {tab === "activity" && <ActivityTab changes={changes.data} error={changes.error} onRetry={changes.refetch} />}
    </div>
  );
}
