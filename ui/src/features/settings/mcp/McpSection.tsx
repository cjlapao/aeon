import { useCallback, useEffect, useMemo, useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, ContentActions, MultiToggle, SearchBar, Select, Tabs } from "../../../components/kit";
import { OverlaySidePanel } from "../../../components/OverlaySidePanel";
import { useRpcQuery } from "../../../lib/useRpcQuery";
import { useRpcAction } from "../../../lib/useRpcAction";
import { rpcTyped } from "../../../lib/bridge";
import type { LiveController, LiveState } from "../../../lib/live";
import { useMcpProcesses } from "./useMcpProcesses";
import { ServerList } from "./ServerList";
import { ServerDrawer } from "./ServerDrawer";
import { ServerForm } from "./ServerForm";
import { GrantsMatrix } from "./GrantsMatrix";
import { AuditList } from "./AuditList";
import { ImportPanel } from "./ImportPanel";
import { ProposalsDesk } from "../policy/ProposalsDesk";
import {
  duplicateDraftFrom,
  emptyServerDraft,
  MCP_PROPOSAL_KINDS,
  type McpVocabulary,
  type ServerDetail,
  type ServerDraft,
  type ServerRow,
  type ServersParamsWire,
} from "./mcpModel";
import type { RpcMethods } from "../../../proto/contract";

/**
 * The MCP section of the real Settings screen (S46 T11, MG5/MG7/MG13/
 * MG14/P34) — four tabs: **Servers · Grants · Proposals · Audit**, and Import as a panel opened from the header.
 * Descriptor-driven: `mcp.vocabulary` (cached in state seeded from the
 * response, the S45 PM6 pattern) builds every select; filters travel as
 * `mcp.servers` **params** (daemon-side); every sentence the daemon sends —
 * reasons, statements, refusals, the level-0 words — renders verbatim; env
 * appears as NAMES only; after any write the rows are re-read from the
 * response, never written optimistically; nothing reaches browser storage.
 *
 * The frame is the Agents page's (docs/aeon/UI-DESIGN-LANGUAGE.md): a header with the
 * primary verb, tabs, an action bar, and a table that fills the height. A server, the add
 * and edit form and the import each open in a side panel over the list, never inline.
 */
export interface McpSectionProps {
  controller: LiveController;
  live: LiveState;
  /** @default `live.state === "current"` — writes are disabled while not current. */
  canWrite?: boolean;
}

type TabId = "servers" | "grants" | "proposals" | "audit";

/** The daemon's level-0 lines (its CLI prints these same words, pinned by
 *  the CLI goldens and the DEMO walk; the socket carries no statement
 *  field, so the section quotes the DAEMON's own lines — recorded
 *  deviation, never a UI-authored replacement). */
const LEVEL0_STATEMENT = "no MCP servers configured. tools available: the core nine only (S11)";

export function McpSection({ controller, live, canWrite = live.state === "current" }: McpSectionProps) {
  const { t } = useI18n();
  const action = useRpcAction();
  const [tab, setTab] = useState<TabId>("servers");
  const [tier, setTier] = useState("");
  const [origin, setOrigin] = useState("");
  const [q, setQ] = useState("");
  const [selected, setSelected] = useState<string | undefined>(undefined);
  const [importing, setImporting] = useState(false);
  const [form, setForm] = useState<{ mode: "add" | "edit"; name?: string; draft: ServerDraft } | undefined>(undefined);

  const vocabularyQuery = useRpcQuery("mcp.vocabulary", {}, controller, live);
  const [vocabulary, setVocabulary] = useState<McpVocabulary | undefined>(undefined);
  useEffect(() => {
    if (vocabularyQuery.data) setVocabulary(vocabularyQuery.data);
  }, [vocabularyQuery.data]);

  const filterParams: ServersParamsWire = {};
  if (tier !== "") filterParams.tier = tier;
  if (origin !== "") filterParams.origin = origin;
  const { procs } = useMcpProcesses();
  const listQuery = useRpcQuery("mcp.servers", filterParams as unknown as RpcMethods["mcp.servers"]["params"], controller, live);
  const all = useMemo(() => listQuery.data?.servers ?? [], [listQuery.data]);
  // The tier and origin travel to the daemon as params; the search only narrows what is on screen.
  const rows = useMemo(() => {
    const term = q.trim().toLowerCase();
    if (term === "") return all;
    return all.filter((row) =>
      [row.name, row.title ?? "", row.description ?? "", row.agent, row.transport.command ?? "", row.transport.url ?? ""].some((field) => field.toLowerCase().includes(term)),
    );
  }, [all, q]);

  const refreshList = useCallback(() => {
    listQuery.refetch();
    setSelected(undefined);
    setForm(undefined);
  }, [listQuery]);

  const toggleEnabled = useCallback(
    async (row: ServerRow, enabled: boolean) => {
      // The write's own response is the fact; the list re-reads after it.
      await action.run(() => rpcTyped("mcp.server.setEnabled", { name: row.name, enabled }), {
        errorTitle: t("settings.mcp.drawer.setEnabledFailed"),
      });
      listQuery.refetch();
    },
    [action, listQuery, t],
  );

  const reason = canWrite ? undefined : t("settings.mcp.reconnecting");

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto" data-testid="mcp-section">
      <ContentActions
        className="shrink-0"
        gap="md"
        align="end"
        data-testid="mcp-header"
        actions={
          <div className="flex items-center gap-2">
            <Button size="sm" variant="ghost" disabled={!canWrite} tooltip={reason} onClick={() => setImporting(true)} data-testid="mcp-import">{t("settings.mcp.tabs.import")}</Button>
            <Button size="sm" disabled={!canWrite} tooltip={reason} onClick={() => { setSelected(undefined); setForm({ mode: "add", draft: emptyServerDraft() }); }} data-testid="add-server">
              {t("settings.mcp.form.open")}
            </Button>
          </div>
        }
      >
        <div>
          <h1 className="text-3xl font-semibold">{t("settings.mcp.title")}</h1>
          <p className="mt-1 text-sm opacity-70">{t("settings.mcp.subtitle")}</p>
        </div>
      </ContentActions>

      {!canWrite && <Alert className="shrink-0" intent="warning" title={t("settings.mcp.reconnecting")} data-testid="mcp-reconnecting" />}
      {action.error && <Alert className="shrink-0" intent="danger" title={action.error} />}

      <div className="shrink-0">
        <Tabs
          variant="underline"
          items={[
            { id: "servers", label: t("settings.mcp.tabs.servers") },
            { id: "grants", label: t("settings.mcp.tabs.grants") },
            { id: "proposals", label: t("settings.mcp.tabs.proposals") },
            { id: "audit", label: t("settings.mcp.tabs.audit") },
          ]}
          value={tab}
          onChange={(id) => setTab(id as TabId)}
        />
      </div>

      {tab === "servers" && (
        <>
          <div className="flex shrink-0 flex-wrap items-center gap-3" data-testid="mcp-action-bar">
            <label className="block w-full max-w-md" data-testid="mcp-search">
              <span className="sr-only">{t("settings.mcp.servers.search")}</span>
              <SearchBar placeholder={t("settings.mcp.servers.search")} debounceMs={200} initialValue={q} onSearch={setQ} onClear={() => setQ("")} />
            </label>
            <MultiToggle
              size="sm"
              aria-label={t("settings.mcp.servers.filter.tier")}
              options={[
                { value: "", label: t("settings.mcp.servers.filter.all") },
                { value: "public", label: t("settings.mcp.tier.public") },
                { value: "private", label: t("settings.mcp.tier.private") },
              ]}
              value={tier}
              onChange={setTier}
            />
            <div className="w-48 shrink-0"><Select variant="ghost" size="md" aria-label={t("settings.mcp.servers.filter.origin")} value={origin} onChange={(event) => setOrigin(event.target.value)}>
              <option value="">{t("settings.mcp.servers.filter.origin")}: {t("settings.mcp.servers.filter.all")}</option>
              <option value="system">{t("settings.mcp.origin.system")}</option>
              <option value="user">{t("settings.mcp.origin.user")}</option>
              <option value="import">{t("settings.mcp.origin.import")}</option>
            </Select></div>
          </div>

          {vocabulary === undefined && vocabularyQuery.error ? (
            <Alert className="shrink-0" intent="warning" title={t("settings.mcp.vocabularyFailed")} />
          ) : null}

          <ServerList
            processes={procs}
            rows={rows}
            loading={listQuery.loading && listQuery.data === undefined}
            error={listQuery.error}
            onRetry={listQuery.refetch}
            canWrite={canWrite}
            emptyStatement={LEVEL0_STATEMENT}
            filtered={q.trim() !== "" || tier !== "" || origin !== ""}
            resetKey={`${q}|${tier}|${origin}|${all.length}`}
            onSelect={(name) => setSelected(name)}
            onToggleEnabled={(row, enabled) => void toggleEnabled(row, enabled)}
          />
        </>
      )}

      {tab === "grants" && (
        <div className="min-h-0 flex-1 overflow-y-auto pr-5">
          <GrantsMatrix controller={controller} live={live} canWrite={canWrite} />
        </div>
      )}

      {tab === "proposals" && (
        <div className="min-h-0 flex-1 overflow-y-auto pr-5">
          {/* S45's ONE desk, reused (not copied), narrowed to the MCP kinds (T6). */}
          <ProposalsDesk controller={controller} live={live} canWrite={canWrite} kinds={MCP_PROPOSAL_KINDS} />
        </div>
      )}

      {tab === "audit" && (
        <div className="min-h-0 flex-1 overflow-y-auto pr-5">
          <AuditList controller={controller} live={live} />
        </div>
      )}

      {/* The three surfaces open over the page, whichever tab is showing. */}
      {selected && !form && (
        <OverlaySidePanel title={t("settings.mcp.drawer.title", { name: selected })} titleClassName="text-xl font-semibold" variant="simple" width={640} onClose={() => setSelected(undefined)}>
          <ServerDrawer
            name={selected}
            controller={controller}
            live={live}
            canWrite={canWrite}
            onClose={() => setSelected(undefined)}
            onChanged={listQuery.refetch}
            onEdit={(detail: ServerDetail) => setForm({ mode: "edit", name: detail.name, draft: draftFromDetail(detail) })}
            onDuplicate={(detail: ServerDetail) => {
              setSelected(undefined);
              setForm({ mode: "add", draft: duplicateDraftFrom(detail) });
            }}
          />
        </OverlaySidePanel>
      )}

      {form && (
        <OverlaySidePanel
          title={form.mode === "add" ? t("settings.mcp.form.addTitle") : t("settings.mcp.form.editTitle", { name: form.name ?? "" })}
          titleClassName="text-xl font-semibold"
          variant="simple"
          width={680}
          onClose={() => setForm(undefined)}
        >
          <ServerForm
            key={`${form.mode}:${form.name ?? "new"}`}
            editingName={form.name}
            initial={form.draft}
            vocabulary={vocabulary}
            canWrite={canWrite}
            controller={controller}
            live={live}
            onCancel={() => setForm(undefined)}
            onSaved={refreshList}
          />
        </OverlaySidePanel>
      )}

      {importing && (
        <OverlaySidePanel title={t("settings.mcp.import.title")} titleClassName="text-xl font-semibold" variant="simple" width={560} onClose={() => setImporting(false)}>
          <ImportPanel canWrite={canWrite} onImported={listQuery.refetch} />
        </OverlaySidePanel>
      )}
    </div>
  );
}

/** An edit draft seeded from the detail row (env arrives as NAMES from the
 *  daemon; the url is not on the detail view — left empty, the daemon's
 *  whole-field replacement semantics make the form's answer the truth). */
function draftFromDetail(detail: ServerDetail): ServerDraft {
  return {
    name: detail.name,
    title: detail.title ?? "",
    description: detail.description ?? "",
    tier: detail.tier,
    agent: detail.tier === "public" ? "" : detail.agent ?? "",
    enabled: detail.enabled,
    transportType: detail.transport.type,
    command: detail.transport.command ?? "",
    args: [...(detail.transport.args ?? [])],
    env: [...(detail.transport.envNames ?? [])],
    // The bindings the daemon holds (variable NAMES) — a typed value is never read back.
    envFrom: { ...((detail.transport as { envFrom?: Record<string, string> }).envFrom ?? {}) },
    envValues: {},
    url: "",
    tools: (detail.tools ?? []).map((tool) => ({ name: tool.name, rerun: tool.rerun ?? "", effect: tool.effect ?? "" })),
  };
}
