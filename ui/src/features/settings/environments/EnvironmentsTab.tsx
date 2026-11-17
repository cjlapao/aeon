import { useMemo, useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { ActionPanel, CollapsiblePanel, Select, Toggle } from "../../../components/kit";
import { OverlaySidePanel } from "../../../components/OverlaySidePanel";
import { notifications } from "../../../lib/notifications";
import { useRpcAction } from "../../../lib/useRpcAction";
import type { LiveController, LiveState } from "../../../lib/live";
import {
  useComponentList,
  useEnvironmentChanges,
  useEnvironmentEffective,
  useEnvironmentList,
  writeEnvironmentArchive,
  writeEnvironmentRestore,
} from "./environmentsRpc";
import { EnvironmentRows } from "./EnvironmentRows";
import { EnvironmentDetail } from "./EnvironmentDetail";
import { EnvironmentForm } from "./EnvironmentForm";
import { BindDialog } from "./BindDialog";
import {
  SUBJECT_ENVIRONMENT,
  activeKnownLimits,
  backends,
  changesParamsFor,
  componentNames,
  downgradeNoteOf,
  listOf,
  latestChangeBySubject,
  legacyWorkspaceRow,
  type DisplayRow,
  type Vocabulary,
} from "./environmentsModel";

/**
 * The Environments tab: the list with its daemon-side filters, the two
 * banners whose words are the daemon's, and the doors that change a row.
 *
 * Filters travel as `environment.list` params (backend, origin, include
 * archived) — the rows shown are the daemon's answer, never a local sweep of
 * a bigger one. The EV15 banner renders each `knownLimits` sentence VERBATIM
 * and only while the daemon says it is `active`, and the legacy banner quotes
 * the `downgradeNote` the workspace preview of `environment.effective` speaks
 * when the workspace floor is the inheriting row: the panel holds no status
 * string of its own, so a defect that gets fixed stops being announced the day
 * it is fixed.
 *
 * A write updates its row from the RESPONSE it was given and then refetches;
 * the draft never becomes the row. A refusal renders the daemon's own parts,
 * verbatim, and the draft that provoked it stays on the screen.
 */
export type EnvironmentFormState = { mode: "add" } | { mode: "edit"; row: DisplayRow } | undefined;

export interface EnvironmentsTabProps {
  controller: LiveController;
  live: LiveState;
  canWrite: boolean;
  vocabulary: Vocabulary | undefined;
  form: EnvironmentFormState;
  setForm: (next: EnvironmentFormState) => void;
}

const LIST_LIMIT = 200;

export function EnvironmentsTab({ controller, live, canWrite, vocabulary, form, setForm }: EnvironmentsTabProps) {
  const { t } = useI18n();
  const action = useRpcAction();

  const [backend, setBackend] = useState("");
  const [origin, setOrigin] = useState("");
  const [includeArchived, setIncludeArchived] = useState(false);
  const [openId, setOpenId] = useState("");
  const [bindTarget, setBindTarget] = useState<DisplayRow | undefined>(undefined);
  const [answered, setAnswered] = useState<Record<string, DisplayRow>>({});

  const list = useEnvironmentList({ limit: LIST_LIMIT, backend, origin, includeArchived }, controller, live);
  const changes = useEnvironmentChanges(changesParamsFor(SUBJECT_ENVIRONMENT, LIST_LIMIT), controller, live);
  const components = useComponentList(controller, live);
  // The workspace preview (no component id): the daemon's own words for what a step there would
  // run in, including the legacy downgrade note.
  const workspace = useEnvironmentEffective({ componentId: "" }, controller, live);

  const rows = useMemo(() => mergeAnswered(listOf(list.data), answered), [list.data, answered]);
  const names = componentNames(listOf(components.data?.components));
  const lastChange = latestChangeBySubject(listOf(changes.data));
  const legacyFloor = legacyWorkspaceRow(rows);
  const limits = activeKnownLimits(vocabulary);
  const opened = openId === "" ? undefined : rows.find((candidate) => candidate.id === openId);

  const refresh = () => list.refetch();

  // A refusal is a toast and nothing else; the daemon's sentence is the toast's body.
  const refused = (message: string) => notifications.push({ title: t("settings.environments.bind.refused"), body: message, tone: "danger", source: "local" });

  const run = async (write: () => Promise<DisplayRow>) => {
    const result = await action.run(write, { errorTitle: t("settings.environments.write.failed") });
    if (result) {
      setAnswered((held) => ({ ...held, [result.id]: result })); // the row from the daemon's answer
      refresh();
    }
  };

  const filterBar = (
    <>
      <div className="w-52 shrink-0" data-testid="environments-filter-backend-label">
        <Select variant="ghost" size="md" aria-label={t("settings.environments.filter.backend")} value={backend} onChange={(event) => setBackend(event.target.value)} data-testid="environments-filter-backend">
          <option value="">{t("settings.environments.filter.backend")}: {t("settings.environments.filter.any")}</option>
          {backends(vocabulary).map((entry) => <option key={entry.name} value={entry.name}>{entry.name}</option>)}
        </Select>
      </div>
      <div className="w-52 shrink-0" data-testid="environments-filter-origin-label">
        <Select variant="ghost" size="md" aria-label={t("settings.environments.filter.origin")} value={origin} onChange={(event) => setOrigin(event.target.value)} data-testid="environments-filter-origin">
          <option value="">{t("settings.environments.filter.origin")}: {t("settings.environments.filter.any")}</option>
          {originsSeen(rows, origin).map((word) => <option key={word} value={word}>{word}</option>)}
        </Select>
      </div>
      <div data-testid="environments-filter-archived">
        <Toggle size="sm" aria-label={t("settings.environments.filter.includeArchived")} label={t("settings.environments.filter.includeArchived")} checked={includeArchived} onChange={(event) => setIncludeArchived(event.target.checked)} />
      </div>
    </>
  );

  return (
    <div className="flex min-h-[26rem] flex-1 flex-col gap-3">
      {limits.length > 0 && (
        <CollapsiblePanel className="shrink-0" tone="amber" title={t("settings.environments.banner.limitsTitle", { count: limits.length })} defaultExpanded={false} data-testid="environments-limits">
          <ul className="space-y-2 text-sm">
            {limits.map((limit) => (
              <li key={limit.key} className="whitespace-pre-line" data-testid={`environments-limit-${limit.key}`}>{limit.sentence}</li>
            ))}
          </ul>
        </CollapsiblePanel>
      )}

      {legacyFloor && (
        <ActionPanel
          className="shrink-0"
          tone="amber"
          size="sm"
          data-testid="environments-banner-legacy"
          title={t("settings.environments.banner.legacyTitle", { name: legacyFloor.name })}
          description={downgradeNoteOf(workspace.data) || undefined}
        />
      )}

      <EnvironmentRows
        rows={rows}
        loading={list.loading}
        error={list.error}
        vocabulary={vocabulary}
        names={names}
        changes={lastChange}
        onOpen={setOpenId}
        onRetry={refresh}
        filterBar={filterBar}
      />

      {opened && (
        <OverlaySidePanel title={opened.name} titleClassName="text-xl font-semibold" variant="simple" width={600} onClose={() => setOpenId("")}>
          <EnvironmentDetail
            row={opened}
            vocabulary={vocabulary}
            changes={listOf(changes.data)}
            canWrite={canWrite}
            controller={controller}
            live={live}
            onEdit={(row) => { setOpenId(""); setForm({ mode: "edit", row }); }}
            onBind={(row) => { setOpenId(""); setBindTarget(row); }}
            onArchive={(row) => { setOpenId(""); void run(() => writeEnvironmentArchive(row.id)); }}
            onRestore={(row) => { setOpenId(""); void run(() => writeEnvironmentRestore(row.id)); }}
            onRefusal={refused}
            onUnbound={() => refresh()}
          />
        </OverlaySidePanel>
      )}

      {form && (
        <OverlaySidePanel
          title={form.mode === "add" ? t("settings.environments.form.addTitle") : t("settings.environments.form.editTitle", { name: form.row.name })}
          titleClassName="text-xl font-semibold"
          variant="simple"
          width={560}
          onClose={() => setForm(undefined)}
        >
          <EnvironmentForm
            mode={form.mode}
            row={form.mode === "edit" ? form.row : undefined}
            vocabulary={vocabulary}
            canWrite={canWrite}
            busy={action.busy}
            controller={controller}
            live={live}
            onSaved={(row) => {
              setAnswered((held) => ({ ...held, [row.id]: row }));
              refresh();
              setForm(undefined);
            }}
            onClose={() => setForm(undefined)}
          />
        </OverlaySidePanel>
      )}

      {bindTarget && (
        <OverlaySidePanel title={t("settings.environments.actions.bind")} titleClassName="text-xl font-semibold" variant="simple" width={520} onClose={() => setBindTarget(undefined)}>
          <BindDialog
            row={bindTarget}
            components={listOf(components.data?.components)}
            canWrite={canWrite}
            busy={action.busy}
            vocabulary={vocabulary}
            onBound={(answer) => {
              // The row is not edited from the draft: the list is asked again and the statement
              // that was just applied is said back in the daemon's own words.
              notifications.push({ title: t("settings.environments.bind.applied"), body: answer.statement, tone: "success", source: "local" });
              refresh();
              setBindTarget(undefined);
            }}
            onRefusal={refused}
            onClose={() => setBindTarget(undefined)}
          />
        </OverlaySidePanel>
      )}
    </div>
  );
}

/** The rows the daemon answered, with each written row replaced by its own response. */
function mergeAnswered(rows: readonly DisplayRow[], answered: Record<string, DisplayRow>): DisplayRow[] {
  const merged = rows.map((row) => answered[row.id] ?? row);
  const seen = new Set(rows.map((row) => row.id));
  for (const row of Object.values(answered)) {
    if (!seen.has(row.id)) merged.push(row);
  }
  return merged;
}

/** The origin words the answer itself carries — the filter never types one. */
function originsSeen(rows: readonly DisplayRow[], picked: string): string[] {
  const words = rows.map((row) => row.origin);
  if (picked !== "" && !words.includes(picked)) words.push(picked);
  return [...new Set(words)];
}
