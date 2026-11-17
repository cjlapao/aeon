import { useEffect, useRef, useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, EmptyState, Tabs } from "../../components/kit";
import { OverlaySidePanel } from "../../components/OverlaySidePanel";
import { useAppToast } from "../../lib/useAppToast";
import type { LiveController, LiveState } from "../../lib/live";
import { useRpcQuery } from "../../lib/useRpcQuery";
import {
  AgentMetadataView,
  AgentOverviewView,
  PromptMarkdown,
} from "./AgentFields";
import { AgentChanges } from "./AgentChanges";
import { AgentCreateBody } from "./AgentCreateBody";
import { AgentImportBody } from "./AgentImportBody";
import { AgentEditAdvanced, AgentEditOverview } from "./AgentEditTabs";
import { usePanelWrites } from "./usePanelWrites";
import {
  INTERNAL_EDIT_REFUSAL,
  agentChangedFields,
  agentDraftUnder,
  agentEditParams,
  agentViewOf,
  refusalFor,
  rowReadOnly,
  type AgentDraft,
  type AgentTyping,
} from "./agentsModel";

// The page's one side panel (CU4): a `SidePanel` the action bar and a row's
// menu both open, holding one of four bodies — create, edit, view, import —
// plus the ledger tab inside the row bodies. The row body is the only
// component on this page that asks `agent.view`: a query asks when it mounts,
// and a create form has no slug to ask with.
//
// AR5 rules the row body: `agentmgr.Edit` REPLACES its nine content fields with
// whatever `fields` carries, so the panel holds only what the person typed and
// posts the loaded row with that spread over it; the form shows the row's own
// values, and there is no path out of this component that builds an edit's
// `fields` from less than `agent.view`'s answer.
//
// A write belongs to the daemon the moment it leaves: a close while one is
// flying keeps it flying, the answer lands only on the surface that asked, and
// a close is never worded as a cancel.

/** The four things a panel can be (CU4). No fifth: a fifth mode is a new page. */
export type AgentPanelMode = "create" | "edit" | "view" | "import";

export interface AgentPanelProps {
  mode: AgentPanelMode;
  /** The row the panel opened on. `""` for create and import, which have none. */
  slug: string;
  controller: LiveController;
  live: LiveState;
  canWrite: boolean;
  onClose: () => void;
  /** A write landed: the screen re-reads the table. Never the other way round. */
  onChanged: () => void;
  /** So the screen can ask before throwing a draft away (the `BlueprintDrawer` pattern). */
  onDirtyChange?: (dirty: boolean) => void;
  /** Open an agent's page (the import's "Open it" button). */
  onOpenAgent?: (slug: string) => void;
}

const MODE_TITLE: Record<AgentPanelMode, string> = {
  create: "agents.panel.create",
  edit: "agents.panel.edit",
  view: "agents.panel.view",
  import: "agents.panel.import",
};

const MODE_SUBTITLE: Record<AgentPanelMode, string> = {
  create: "agents.panel.createSubtitle",
  edit: "agents.panel.editSubtitle",
  view: "agents.panel.viewSubtitle",
  import: "agents.panel.importSubtitle",
};

/** The agent panel: one `SidePanel`, four bodies, one ledger tab. */
export function AgentPanel({
  mode,
  slug,
  controller,
  live,
  canWrite,
  onClose,
  onChanged,
  onDirtyChange,
  onOpenAgent,
}: AgentPanelProps) {
  const { t } = useI18n();
  // The title of a panel about a row says the row's name — the one `agent.view` answered.
  const [loadedName, setLoadedName] = useState<string | undefined>(undefined);
  const opensWithRow = mode === "edit" || mode === "view";
  // A dirty close goes to the screen at once (no slide-out); a clean one slides
  // out first and the screen unmounts the panel after.
  const dirty = useRef(false);
  const trackDirty = (next: boolean) => {
    dirty.current = next;
    onDirtyChange?.(next);
  };
  const beforeClose = () => {
    if (!dirty.current) return true;
    onClose();
    return false;
  };

  return (
    <OverlaySidePanel
      onClose={onClose}
      beforeClose={beforeClose}
      title={opensWithRow ? (loadedName ?? slug) : t(MODE_TITLE[mode])}
      titleClassName="text-xl font-semibold"
      subtitle={t(MODE_SUBTITLE[mode])}
      variant="simple"
      width={720}
      minWidth={420}
    >
      <div className="space-y-4" data-testid={`agent-panel-${mode}`}>
        {!canWrite && (
          <Alert
            intent="warning"
            title={t("agents.reason.notConnectedTitle")}
            description={t("agents.reason.notConnected")}
            data-testid="agent-panel-readonly"
          />
        )}

        {opensWithRow ? (
          <AgentRowBody
            mode={mode}
            slug={slug}
            controller={controller}
            live={live}
            canWrite={canWrite}
            onChanged={onChanged}
            onDirtyChange={trackDirty}
            onLoaded={setLoadedName}
          />
        ) : mode === "create" ? (
          <AgentCreateBody canWrite={canWrite} controller={controller} live={live} onChanged={onChanged} onCancel={onClose} onOpenAgent={onOpenAgent} />
        ) : (
          <AgentImportBody canWrite={canWrite} onChanged={onChanged} onOpenAgent={onOpenAgent} />
        )}
      </div>
    </OverlaySidePanel>
  );
}

export interface BodyProps {
  canWrite: boolean;
  onChanged: () => void;
}

// ---------------------------------------------------------------------------
// The row body: one agent, read from `agent.view` and written back whole.
// `agent.edit` answers only `{"edited": slug}`, so after a save this body
// re-reads `agent.view` — anything else it displayed would be a guess.
// ---------------------------------------------------------------------------

type PanelTab = "overview" | "prompt" | "metadata" | "history";

const PANEL_TABS: readonly PanelTab[] = ["overview", "prompt", "metadata", "history"];

/** The draft, as the page header needs it to draw Save and the "changed" note. */
export interface AgentEditState {
  dirty: boolean;
  busy: boolean;
  /** The changed fields, joined for the note. */
  changed: string;
  canSave: boolean;
}

export function AgentRowBody({
  mode,
  slug,
  controller,
  live,
  canWrite,
  onChanged,
  onDirtyChange,
  onLoaded,
  onRow,
  onEditState,
  saveRef,
}: {
  mode: "edit" | "view";
  slug: string;
  controller: LiveController;
  live: LiveState;
  canWrite: boolean;
  onChanged: () => void;
  onDirtyChange?: (dirty: boolean) => void;
  onLoaded?: (name: string) => void;
  /** The row as `agent.view` answered it, each time it answers (the page above draws its header from it). */
  onRow?: (row: AgentDraft) => void;
  /** What the page's header needs to draw Save: the draft's state, each time it changes. */
  onEditState?: (state: AgentEditState) => void;
  /** Set to the body's own save on every render, so the header's Save runs the write this body owns. */
  saveRef?: { current: () => void };
}) {
  const { t } = useI18n();
  const [tab, setTab] = useState<PanelTab>("overview");
  const [typing, setTyping] = useState<AgentTyping>({});
  const notify = useAppToast();

  const { busy, writeDisabled, rpcWrite, onChangedRef } = usePanelWrites({
    canWrite,
    guard: `${mode}|${slug}`,
    onChanged,
  });

  // The window's row is a summary by construction; seeding a form from it would
  // write back blanks for the fifteen keys it does not carry (AR5).
  const view = useRpcQuery("agent.view", { slug }, controller, live);
  // The closed tech vocabulary, so the form offers the daemon's own list rather than a copy of it.
  const registry = useRpcQuery("handlers.list", {}, controller, live);
  const row = agentViewOf(view.data);
  const values = row === null ? null : agentDraftUnder(row, typing);
  const changed = values === null || row === null ? [] : agentChangedFields(values, row);
  const dirty = mode === "edit" && changed.length > 0;
  // A mode change is a new start: what was typed in one mode is not carried into the other.
  useEffect(() => setTyping({}), [mode]);
  useEffect(() => {
    if (row !== null) onRow?.(row);
    // The row object is rebuilt on every answer; its identity is the answer.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [row]);
  useEffect(() => onDirtyChange?.(dirty), [dirty, onDirtyChange]);
  useEffect(() => {
    if (row !== null) onLoaded?.(row.name);
    // The name is read once per row; typing does not change what the row is.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [row?.name, slug]);

  const save = async () => {
    if (!canWrite || mode !== "edit" || values === null) return;
    const answer = await rpcWrite("agent.edit", agentEditParams(slug, values));
    if (answer === undefined) return;
    notify.success(t("agents.panel.edited", { slug: answer.edited }));
    onChangedRef.current();
    // The draft is gone because the row it was written against may no longer be
    // what it was; the re-read is what puts the panel back in sync.
    setTyping({});
    view.refetch();
  };

  const firstParty = rowReadOnly({ internal: row?.internal === true });

  if (saveRef) saveRef.current = () => void save();
  const changedList = changed.join(", ");
  useEffect(() => {
    onEditState?.({ dirty, busy, changed: changedList, canSave: mode === "edit" && !firstParty && !writeDisabled && dirty });
  }, [dirty, busy, changedList, mode, firstParty, writeDisabled, onEditState]);

  return (
    // Fixed top (errors, the save row, the tab bar) over one body that takes the
    // free height and scrolls its own content; nothing else on the page scrolls.
    <div className="flex min-h-0 min-w-0 flex-1 flex-col gap-4">

      {view.loading && view.data === undefined && (
        <EmptyState title={t("agents.panel.loading")} data-testid="agent-panel-loading" />
      )}

      {view.error !== undefined && (
        <div className="space-y-2" data-testid="agent-panel-read-error">
          <Alert intent="danger" title={t("agents.read.failed")} description={view.error} />
          <Button size="sm" variant="ghost" onClick={view.refetch} data-testid="agent-panel-retry">
            {t("agents.action.retry")}
          </Button>
        </div>
      )}

      {!view.loading && view.data !== undefined && row === null && (
        <EmptyState title={t("agents.panel.noRow")} data-testid="agent-panel-no-row" />
      )}

      {values !== null && (
        <>
          {mode === "edit" && firstParty && (
            // CU6: the reason sits above every tab, in the daemon's own words.
            // Nothing here pre-empts a refusal.
            <Alert
              intent="warning"
              title={t("agents.panel.firstPartyTitle")}
              description={refusalFor(INTERNAL_EDIT_REFUSAL, slug)}
              data-testid="agent-panel-first-party"
            />
          )}

          <div className="shrink-0">
          <Tabs
            variant="underline"
            items={[
              { id: "overview", label: t("agents.panel.tab.overview") },
              { id: "prompt", label: t("agents.panel.tab.prompt") },
              { id: "metadata", label: t("agents.panel.tab.metadata") },
              { id: "history", label: t("agents.panel.tab.history") },
            ]}
            value={tab}
            onChange={(id) => setTab(PANEL_TABS.includes(id as PanelTab) ? (id as PanelTab) : "overview")}
          />
          </div>

          <div className="min-h-0 min-w-0 flex-1 overflow-y-auto overflow-x-hidden pr-5" data-testid="agent-panel-body">
          {tab === "overview" && (
            <div className="space-y-3 pt-3">
              {mode === "view" ? (
                <AgentOverviewView agent={values} />
              ) : (
                <AgentEditOverview
                  values={values}
                  onField={(key, value) => setTyping((prev) => ({ ...prev, [key]: value }))}
                  disabled={writeDisabled || firstParty}
                  registry={registry.data}
                  controller={controller}
                  live={live}
                  slug={row?.slug ?? slug}
                />
              )}
            </div>
          )}

          {tab === "prompt" && (
            <div className="pt-3">
              <PromptMarkdown
                value={values.prompt}
                disabled={writeDisabled || firstParty}
                onChange={
                  mode === "edit" && !firstParty
                    ? (next) => setTyping((prev) => ({ ...prev, prompt: next }))
                    : undefined
                }
              />
            </div>
          )}

          {tab === "metadata" && (
            <div className="space-y-3 pt-3">
              {mode === "edit" ? (
                <AgentEditAdvanced
                  values={values}
                  onField={(key, value) => setTyping((prev) => ({ ...prev, [key]: value }))}
                  disabled={writeDisabled || firstParty}
                  slug={row?.slug ?? slug}
                />
              ) : (
                <AgentMetadataView agent={values} />
              )}
            </div>
          )}

          {tab === "history" && (
            <div className="pt-3">
              <AgentChanges slug={slug} controller={controller} live={live} />
            </div>
          )}
          </div>
        </>
      )}
    </div>
  );
}
