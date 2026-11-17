import { useMemo, useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, ContentActions, Tabs } from "../../../components/kit";
import { useRpcQuery } from "../../../lib/useRpcQuery";
import type { LiveController, LiveState } from "../../../lib/live";
import type { RpcMethods } from "../../../proto/contract";
import { DriftBanner } from "./DriftBanner";
import { ToolFilters } from "./ToolFilters";
import { ToolsTable } from "./ToolsTable";
import { AgentRosterTable } from "./AgentRosterTable";
import { DetailSidePanel } from "./DetailSidePanel";
import { ToolDrawer } from "./ToolDrawer";
import { ByAgent } from "./ByAgent";
import { SurfaceDrawer, SurfaceList } from "./SurfaceList";
import {
  buildSurfaceFilter,
  buildToolsFilter,
  driftSummary,
  emptySurfaceFilter,
  emptyToolsFilter,
  filterOptions,
  type SurfaceFilterState,
  type ToolsFilterState,
} from "./toolsModel";

/**
 * The Tools and Surface sections of the real Settings screen (S47 T10,
 * TC9/TC10/TC11/TC12/P34) — three tabs: **Tools · By agent · Surface**.
 *
 * Descriptor-driven the way S46's MCP section is: `tools.vocabulary` builds every
 * filter select, `surface.list`'s own counts build the surface selects, filters
 * travel as **params** (the matching happens on the daemon side, TC10), and every
 * sentence the daemon sends — descriptions, statements, per-agent reasons, the
 * policy rows' own `describe`, the capability meanings, the method summaries —
 * renders verbatim. Nothing is written to browser storage; nothing here opens a
 * transport; and there is no control that changes anything (TC1), because this
 * slice owns the two catalog reads and the surface classification, not a write.
 *
 * The queries live in small host components, one per tab, so a tab the person never
 * opened never asks the daemon a question it does not need — and so the by-agent and
 * detail reads are never sent with an empty subject, which would be the UI asking a
 * question it already knows the answer to.
 */
export interface ToolsSectionProps {
  controller: LiveController;
  live: LiveState;
}

type TabId = "tools" | "agent" | "surface";

export function ToolsSection({ controller, live }: ToolsSectionProps) {
  const { t } = useI18n();
  const [tab, setTab] = useState<TabId>("tools");
  const canRead = live.state === "current";

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto" data-testid="tools-section">
      <ContentActions className="shrink-0" gap="md" align="end" data-testid="tools-header">
        <div>
          <h1 className="text-3xl font-semibold">{t("settings.tools.title")}</h1>
          <p className="mt-1 text-sm opacity-70">{t("settings.tools.subtitle")}</p>
        </div>
      </ContentActions>
      {!canRead && (
        <Alert className="shrink-0" intent="warning" title={t("settings.tools.reconnecting")} description={t("settings.tools.reconnectingBody")} data-testid="tools-reconnecting" />
      )}
      <div className="shrink-0">
        <Tabs
          variant="underline"
          items={[
            { id: "tools", label: t("settings.tools.tabs.tools") },
            { id: "agent", label: t("settings.tools.tabs.byAgent") },
            { id: "surface", label: t("settings.tools.tabs.surface") },
          ]}
          value={tab}
          onChange={(id) => setTab(id as TabId)}
        />
      </div>
      {tab === "tools" && <CatalogTab controller={controller} live={live} />}
      {tab === "agent" && <AgentTab controller={controller} live={live} />}
      {tab === "surface" && <SurfaceTab controller={controller} live={live} />}
    </div>
  );
}

// --- the Tools tab ----------------------------------------------------------------

function CatalogTab({ controller, live }: { controller: LiveController; live: LiveState }) {
  const { t } = useI18n();
  const [filter, setFilter] = useState<ToolsFilterState>(emptyToolsFilter());
  const [selected, setSelected] = useState<string | undefined>(undefined);

  // The filter travels as params. The cast is the S46 T11 / S45 T9 wire-shim
  // idiom: `capability` is a member the door answers and no conformance fixture
  // ever sent, so the generated type does not name it (recorded deviation).
  const params = useMemo(
    () => buildToolsFilter(filter) as unknown as RpcMethods["tools.list"]["params"],
    [filter],
  );
  const list = useRpcQuery("tools.list", params, controller, live);
  const status = useRpcQuery("tools.status", {}, controller, live);
  const vocab = useRpcQuery("tools.vocabulary", {}, controller, live);
  const options = filterOptions(vocab.data);

  return (
    <div className="flex min-h-[26rem] flex-1 flex-col gap-3">
      <DriftBanner summary={driftSummary(status.data)} loading={status.loading} error={status.error} />
      <ToolFilters value={filter} onChange={(next) => setFilter(next)} options={options} loading={list.loading} />
      {list.error ? <Alert className="shrink-0" intent="danger" title={t("settings.tools.listFailed")} description={list.error} data-testid="tools-list-error" /> : null}
      <ToolsTable tools={list.data?.tools ?? []} statement={list.data?.statement} selected={selected} onSelect={(name) => setSelected(name)} loading={list.loading} />
      {selected ? (
        <ToolHost name={selected} vocabulary={vocab.data} controller={controller} live={live} onClose={() => setSelected(undefined)} />
      ) : null}
    </div>
  );
}

/** Mounted only when a tool IS selected, so `tools.get` is never asked for an
 *  empty name — the daemon would answer a refusal and the screen would render a
 *  failure for a question it never meant to ask. */
function ToolHost({ name, vocabulary, controller, live, onClose }: { name: string; vocabulary: import("./toolsModel").CatalogVocabulary | undefined; controller: LiveController; live: LiveState; onClose: () => void }) {
  const params = useMemo(() => ({ name } satisfies RpcMethods["tools.get"]["params"]), [name]);
  const query = useRpcQuery("tools.get", params, controller, live);
  return <ToolDrawer name={name} detail={query.data} loading={query.loading} error={query.error} vocabulary={vocabulary} onClose={onClose} />;
}

// --- the By agent tab --------------------------------------------------------------

function AgentTab({ controller, live }: { controller: LiveController; live: LiveState }) {
  const [agent, setAgent] = useState("");
  const roster = useRpcQuery("agent.list", {}, controller, live);

  return (
    <div className="flex min-h-[22rem] flex-1 flex-col" data-testid="tools-by-agent-tab">
      <AgentRosterTable
        agents={Array.isArray(roster.data?.agents) ? roster.data.agents : []}
        loading={roster.loading}
        error={roster.error}
        selected={agent}
        onSelect={setAgent}
      />
      {agent !== "" ? (
        <DetailSidePanel title={agent} testId="tools-by-agent-panel" onClose={() => setAgent("")}>
          <ByAgentHost agent={agent} controller={controller} live={live} />
        </DetailSidePanel>
      ) : null}
    </div>
  );
}

function ByAgentHost({ agent, controller, live }: { agent: string; controller: LiveController; live: LiveState }) {
  const params = useMemo(() => ({ agent } satisfies RpcMethods["tools.byAgent"]["params"]), [agent]);
  const query = useRpcQuery("tools.byAgent", params, controller, live);
  return <ByAgent agent={agent} result={query.data} loading={query.loading} error={query.error} />;
}

// --- the Surface tab ---------------------------------------------------------------

function SurfaceTab({ controller, live }: { controller: LiveController; live: LiveState }) {
  const { t } = useI18n();
  const [filter, setFilter] = useState<SurfaceFilterState>(emptySurfaceFilter());
  const [selected, setSelected] = useState<string | undefined>(undefined);

  // `humanDoorOnly` is answered by the door and unfixtureed, hence the same cast.
  const params = useMemo(
    () => buildSurfaceFilter(filter) as unknown as RpcMethods["surface.list"]["params"],
    [filter],
  );
  const list = useRpcQuery("surface.list", params, controller, live);
  const surfaceError = list.error;

  return (
    <div className="flex min-h-[26rem] flex-1 flex-col gap-3">
      {selected ? (
        <SurfaceHost method={selected} controller={controller} live={live} onClose={() => setSelected(undefined)} />
      ) : null}
      <SurfaceList
        rows={list.data?.methods ?? []}
        counts={list.data?.counts}
        statement={list.data?.statement}
        filter={filter}
        onFilter={(next) => setFilter(next)}
        selected={selected}
        onSelect={(method) => setSelected(method)}
        loading={list.loading}
        error={surfaceError}
      />
      {surfaceError ? <p className="shrink-0 text-xs opacity-60">{t("settings.tools.surface.errorNote")}</p> : null}
    </div>
  );
}

function SurfaceHost({ method, controller, live, onClose }: { method: string; controller: LiveController; live: LiveState; onClose: () => void }) {
  const params = useMemo(() => ({ method } satisfies RpcMethods["surface.get"]["params"]), [method]);
  const query = useRpcQuery("surface.get", params, controller, live);
  return <SurfaceDrawer method={method} entry={query.data?.entry} loading={query.loading} error={query.error} onClose={onClose} />;
}
