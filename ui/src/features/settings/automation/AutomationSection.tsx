import { useMemo, useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, ContentActions, Pill, SearchBar, Select, Tabs } from "../../../components/kit";
import { OverlaySidePanel } from "../../../components/OverlaySidePanel";
import { PageTitle } from "../../../components/PageTitle";
import { RelativeTime } from "../../../components/RelativeTime";
import { ProposalCard } from "../policy/ProposalCard";
import type { ProposalRow } from "../policy/policyModel";
import type { LiveController, LiveState } from "../../../lib/live";
import { AutomationDrawer } from "./AutomationDrawer";
import { AutomationList } from "./AutomationList";
import { EventForm } from "./EventForm";
import { FireLog } from "./FireLog";
import { ScheduleForm } from "./ScheduleForm";
import { WebhookForm } from "./WebhookForm";
import { useAutomationChanges, useAutomationProposals, useAutomationVocabulary, useAutomations } from "./automationRpc";
import {
  emptyFilterDraft,
  isAutomationProposal,
  rowKey,
  type FilterDraft,
  type ShowAutomation,
} from "./automationModel";

/**
 * AutomationSection — Settings → Automation (S49 T9): the one place a person
 * manages everything that starts agents on its own. Four tabs:
 * **Automations · Fire log · Audit · Proposals**.
 *
 * The section loads `automation.vocabulary` once (zones, events, match
 * keys, limits, the authority statement, the outcome words) and hands it to
 * every form; filters travel as `automation.list` **params**; every
 * sentence a row shows — description, refusal, health reason, authority
 * statement — is the daemon's, rendered verbatim. The desk shows archived
 * rows only when the filter asks for them; the empty states quote the
 * daemon's two honest empties (AN5).
 *
 * The bench `AutomationPage` stays untouched as a diagnostic; this mounts
 * in `SettingsScreen` beside Policy/MCP/Tools/Environments.
 */
export interface AutomationSectionProps {
  controller: LiveController;
  live: LiveState;
  /** @default `live.state === "current"` — writes are refused while not current. */
  canWrite?: boolean;
  /**
   * Work page only: show the automations whose workspace is a component root in this set, or
   * inside one. Absent = every automation (the all-projects view). It narrows the view; it does
   * not change what an automation may do or who may change it.
   */
  scopeRoots?: ReadonlySet<string>;
}

/** A workspace path is in scope when it is a scoped root or sits beneath one. */
export function inScope(workspace: string | undefined, roots: ReadonlySet<string>): boolean {
  if (!workspace) return false;
  for (const root of roots) if (workspace === root || workspace.startsWith(`${root}/`)) return true;
  return false;
}

type TabId = "automations" | "firelog" | "audit" | "proposals";
type Family = "schedule" | "webhook" | "event";
type Panelled = { kind: "drawer"; source: string; id: string } | { kind: "form"; family: Family; initial?: ShowAutomation } | undefined;


export function AutomationSection({ controller, live, canWrite, scopeRoots }: AutomationSectionProps) {
  const { t } = useI18n();
  const writable = canWrite ?? live.state === "current";
  const [tab, setTab] = useState<TabId>("automations");
  const [filters, setFilters] = useState<FilterDraft>(emptyFilterDraft);
  const [panel, setPanel] = useState<Panelled>(undefined);

  const vocabulary = useAutomationVocabulary(controller, live);
  const list = useAutomations(filters, controller, live);
  const authorityStatement = vocabulary.data?.authorityStatement ?? "";

  const rows = useMemo(() => (list.data?.automations ?? []).filter((row) => !scopeRoots || inScope(row.workspace, scopeRoots)), [list.data, scopeRoots]);
  const scopedList = useMemo(() => (list.data && scopeRoots ? { ...list.data, automations: rows } : list.data), [list.data, scopeRoots, rows]);

  const reload = () => {
    void list.refetch();
    void vocabulary.refetch();
  };
  const close = () => setPanel(undefined);

  const create = (family: Family) => setPanel({ kind: "form", family });

  const drawer = panel?.kind === "drawer" ? panel : undefined;
  const form = panel?.kind === "form" ? panel : undefined;
  const drawerInitial = drawer ? (rows.find((r) => rowKey(r.source, r.id) === rowKey(drawer.source, drawer.id)) as ShowAutomation | undefined) : undefined;
  const formTitle = form
    ? t(form.initial !== undefined
        ? (form.family === "event" ? "settings.automation.form.editEventTitle" : form.family === "webhook" ? "settings.automation.form.editWebhookTitle" : "settings.automation.form.editTitle")
        : (form.family === "event" ? "settings.automation.form.newEventTitle" : form.family === "webhook" ? "settings.automation.form.newWebhookTitle" : "settings.automation.form.newTitle"))
    : "";

  const commonForm = form && {
    initial: form.initial,
    vocabulary: vocabulary.data,
    authorityStatement,
    controller,
    live,
    canWrite: writable,
    onClose: close,
    onChanged: () => {
      reload();
      close();
    },
  };

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto" data-testid="automation-section">
      <ContentActions
        className="shrink-0"
        gap="md"
        align="end"
        data-testid="automation-header"
        actions={
          tab === "automations" ? (
            <>
              <Button size="sm" variant="ghost" disabled={!writable} onClick={() => create("event")} data-testid="create-event">{t("settings.automation.actions.newEvent")}</Button>
              <Button size="sm" variant="ghost" disabled={!writable} onClick={() => create("webhook")} data-testid="create-webhook">{t("settings.automation.actions.newWebhook")}</Button>
              <Button size="sm" disabled={!writable} onClick={() => create("schedule")} data-testid="create-schedule">{t("settings.automation.actions.newSchedule")}</Button>
            </>
          ) : undefined
        }
      >
        <PageTitle title={t("settings.automation.title")} purpose={t("settings.automation.subtitle")} help={{ title: t("settings.automation.help.title"), content: t("settings.automation.help.body") }} />
      </ContentActions>

      {!writable && <Alert className="shrink-0" intent="warning" title={t("settings.automation.reconnecting")} data-testid="automation-reconnecting" />}

      <div className="shrink-0">
        <Tabs
          variant="underline"
          items={[
            { id: "automations", label: t("settings.automation.tabs.automations") },
            { id: "firelog", label: t("settings.automation.tabs.fireLog") },
            { id: "audit", label: t("settings.automation.tabs.audit") },
            { id: "proposals", label: t("settings.automation.tabs.proposals") },
          ]}
          value={tab}
          onChange={(id) => setTab(id as TabId)}
        />
      </div>
      <p className="shrink-0 text-sm opacity-70">{t(`settings.automation.hint.${tab}`)}</p>

      {tab === "automations" && (
        <div className="flex min-h-[26rem] flex-1 flex-col gap-3">
          <div className="flex shrink-0 flex-wrap items-center gap-3" data-testid="automation-filters">
            <label className="block w-full max-w-sm" data-testid="filter-q">
              <span className="sr-only">{t("settings.automation.filter.q")}</span>
              <SearchBar placeholder={t("settings.automation.filter.qHint")} debounceMs={200} initialValue={filters.q} onSearch={(q) => setFilters((f) => ({ ...f, q }))} onClear={() => setFilters((f) => ({ ...f, q: "" }))} />
            </label>
            <div className="w-48 shrink-0">
              <Select variant="ghost" size="md" aria-label={t("settings.automation.filter.source")} value={filters.source} onChange={(e) => setFilters({ ...filters, source: e.target.value })} data-testid="filter-source">
                <option value="">{t("settings.automation.filter.source")}: {t("settings.automation.filter.all")}</option>
                <option value="schedule">schedule</option>
                <option value="webhook">webhook</option>
                <option value="event">event</option>
              </Select>
            </div>
            <div className="w-44 shrink-0">
              <Select variant="ghost" size="md" aria-label={t("settings.automation.filter.enabled")} value={filters.enabled} onChange={(e) => setFilters({ ...filters, enabled: e.target.value })} data-testid="filter-enabled">
                <option value="">{t("settings.automation.filter.enabled")}: {t("settings.automation.filter.any")}</option>
                <option value="true">enabled</option>
                <option value="false">disabled</option>
              </Select>
            </div>
            {/* Archived is HIDDEN unless asked: exclude is the daemon's own default; include widens; only shows the graveyard. */}
            <div className="w-52 shrink-0">
              <Select variant="ghost" size="md" aria-label={t("settings.automation.filter.archived")} value={filters.archived} onChange={(e) => setFilters({ ...filters, archived: e.target.value })} data-testid="filter-archived">
                <option value="">{t("settings.automation.filter.archived")}: {t("settings.automation.filter.hidden")}</option>
                <option value="include">include</option>
                <option value="only">only</option>
              </Select>
            </div>
            <Button size="sm" variant="ghost" onClick={reload} data-testid="automation-reload">{t("settings.automation.reload")}</Button>
          </div>

          {vocabulary.error !== undefined && <Alert className="shrink-0" intent="danger" title={vocabulary.error} data-testid="vocabulary-error" />}
          <AutomationList list={scopedList} error={list.error} loading={list.loading} onOpen={(source, id) => setPanel({ kind: "drawer", source, id })} controller={controller} live={live} />
        </div>
      )}

      {tab === "firelog" && (
        <div className="pb-6">
          <FireLog controller={controller} live={live} vocabulary={vocabulary.data} />
        </div>
      )}

      {tab === "audit" && <AuditTab rows={rows} controller={controller} live={live} />}

      {tab === "proposals" && <AutomationProposals controller={controller} live={live} canWrite={writable} />}

      {drawer && (
        <OverlaySidePanel title={drawer.id} titleClassName="text-xl font-semibold" variant="simple" width={640} onClose={close}>
          <AutomationDrawer
            source={drawer.source}
            id={drawer.id}
            vocabulary={vocabulary.data}
            authorityStatement={authorityStatement}
            controller={controller}
            live={live}
            canWrite={writable}
            onClose={close}
            onChanged={reload}
            onEdit={() => setPanel({
              kind: "form",
              family: drawer.source as Family,
              initial: { ...(drawerInitial ?? {}), id: drawer.id, name: drawerInitial?.name ?? drawer.id, blueprint: drawerInitial?.blueprint ?? "", source: drawer.source, createdAt: "" } as ShowAutomation,
            })}
          />
        </OverlaySidePanel>
      )}

      {form && commonForm && (
        <OverlaySidePanel title={formTitle} titleClassName="text-xl font-semibold" variant="simple" width={560} onClose={close}>
          {form.family === "event" ? <EventForm {...commonForm} /> : form.family === "webhook" ? <WebhookForm {...commonForm} /> : <ScheduleForm family="schedule" {...commonForm} />}
        </OverlaySidePanel>
      )}
    </div>
  );
}

/** The Audit tab: `automation.changes` per automation — op, summary, the actor `kind:id` the peer
 *  credentials produced, the time. The audit is what happened; the engine's reasons live in the fire log. */
function AuditTab({ rows, controller, live }: {
  rows: Array<{ source: string; id: string }>;
  controller: LiveController;
  live: LiveState;
}) {
  const { t } = useI18n();
  const [picked, setPicked] = useState<string>("");
  const key = picked !== "" ? picked : rows[0] !== undefined ? rowKey(rows[0].source, rows[0].id) : "";
  const [source, id] = key.split(":");
  const changes = useAutomationChanges(source ?? "", id ?? "", controller, live);

  return (
    <div className="flex flex-col gap-3 pb-6" data-testid="audit-panel">
      <div className="w-72" data-testid="audit-picker">
        <Select variant="ghost" size="md" aria-label={t("settings.automation.audit.pick")} value={key} onChange={(e) => setPicked(e.target.value)}>
          {rows.map((row) => <option key={rowKey(row.source, row.id)} value={rowKey(row.source, row.id)}>{rowKey(row.source, row.id)}</option>)}
        </Select>
      </div>
      <ul className="divide-y divide-[var(--border)]">
        {(changes.data?.changes ?? []).map((change) => (
          <li key={change.id} className="flex flex-wrap items-center gap-2 py-2 text-xs" data-testid={`audit-row-${change.id}`}>
            <Pill size="sm" tone="slate">{change.op}</Pill>
            <span className="opacity-80">{change.summary}</span>
            <span className="font-mono opacity-60">{change.actorKind}:{change.actorId}</span>
            <RelativeTime iso={change.occurredAt} />
          </li>
        ))}
        {(changes.data?.changes ?? []).length === 0 && <li className="py-2 text-xs opacity-60">{t("settings.automation.audit.empty")}</li>}
      </ul>
    </div>
  );
}

/** The desk's automation kinds only — `proposal.list` is the shared queue (PM4: one implementation),
 *  this filters the view, never the decisions: commit and reject are S45's ProposalCard doors, reused as-is. */
function AutomationProposals({ controller, live, canWrite }: {
  controller: LiveController;
  live: LiveState;
  canWrite: boolean;
}) {
  const { t } = useI18n();
  const proposals = useAutomationProposals(controller, live);
  const mine = useMemo(
    () => (proposals.data?.proposals ?? []).filter((row) => isAutomationProposal(row.kind)) as unknown as ProposalRow[],
    [proposals.data],
  );

  return (
    <div className="flex flex-col gap-3 pb-6" data-testid="automation-proposals">
      {proposals.error !== undefined ? (
        <Alert intent="danger" title={proposals.error} data-testid="proposals-error" />
      ) : mine.length === 0 ? (
        <p className="text-sm opacity-70" data-testid="proposals-empty">{t("settings.automation.proposals.empty")}</p>
      ) : (
        <div className="space-y-3">
          {mine.map((row) => (
            <ProposalCard key={row.id} row={row} controller={controller} live={live} canWrite={canWrite} onDecided={() => void proposals.refetch()} />
          ))}
        </div>
      )}
    </div>
  );
}
