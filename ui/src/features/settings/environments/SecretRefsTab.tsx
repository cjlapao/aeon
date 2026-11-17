import { useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, EmptyState, FormField, Input, Pill, Select, Table, type TableColumn } from "../../../components/kit";
import { OverlaySidePanel } from "../../../components/OverlaySidePanel";
import { RelativeTime } from "../../../components/RelativeTime";
import { useRpcAction } from "../../../lib/useRpcAction";
import type { LiveController, LiveState } from "../../../lib/live";
import {
  checkSecretRef,
  useSecretRefList,
  useSecretRefShow,
  writeSecretRefArchive,
  writeSecretRefRestore,
  writeSecretRefCreate,
} from "./environmentsRpc";
import {
  hasRefusal,
  listOf,
  sourceKinds,
  type SecretRefCheck,
  type SecretRefRow,
  type Vocabulary,
} from "./environmentsModel";

/**
 * The Secret references tab (S59 T11, EV7). A reference is a POINTER: a name, a
 * source kind, a source — the LOCATION the value lives at, which is what a
 * human needs to see and what the daemon reads to decide resolvability — plus
 * the name the child will see (`injectAs`) and the environments that name it
 * (`referencedBy`, the delete-safety read).
 *
 * There is no value field here and no value rendered here. `secretref.check`
 * answers two booleans plus the refusal's kind and sentence, and that is all a
 * check shows: not the value, not its length, not a prefix of it. The create
 * form takes a name, a kind and a location, and the door itself refuses a param
 * carrying a credential-shaped key (`conformance/secretref/create-refuses-a-
 * value-field.json`); the refusal renders verbatim and the draft stays.
 */
export interface SecretRefsTabProps {
  controller: LiveController;
  live: LiveState;
  canWrite: boolean;
  vocabulary: Vocabulary | undefined;
  creating: boolean;
  setCreating: (open: boolean) => void;
}

const H = "text-xs font-semibold uppercase tracking-widest opacity-70";

export function SecretRefsTab({ controller, live, canWrite, vocabulary, creating, setCreating }: SecretRefsTabProps) {
  const { t } = useI18n();
  const action = useRpcAction();
  const [sourceKind, setSourceKind] = useState("");
  const [openName, setOpenName] = useState("");
  const [checks, setChecks] = useState<Record<string, SecretRefCheck>>({});

  const list = useSecretRefList({ sourceKind, limit: 200 }, controller, live);
  const rows = listOf(list.data);
  const opened = rows.find((row) => row.name === openName);

  const check = async (row: SecretRefRow) => {
    const answer = await action.run(() => checkSecretRef(row.name), { errorTitle: t("settings.environments.secret.checkFailed") });
    if (answer) setChecks((held) => ({ ...held, [row.name]: answer }));
  };

  const columns: TableColumn<SecretRefRow>[] = [
    {
      id: "name",
      groupable: false,
      header: t("settings.environments.col.reference"),
      render: (row) => (
        <div className="w-0 min-w-full py-1" data-testid={`secretref-toggle-${row.name}`}>
          <span className="block truncate text-sm font-semibold" data-testid={`secretref-row-${row.name}`}>{row.name}</span>
          {/* The LOCATION, not the value: a person needs to know where it lives. */}
          <code className="block truncate text-xs opacity-60" data-testid={`secretref-source-${row.name}`}>{row.source}</code>
        </div>
      ),
    },
    { id: "inject", groupable: false, header: t("settings.environments.col.injectAs"), width: 200, render: (row) => <Pill size="sm" tone="slate" data-testid={`secretref-injectas-${row.name}`}>{row.injectAs}</Pill> },
    { id: "kind", groupable: false, header: t("settings.environments.col.kind"), width: 150, render: (row) => <Pill size="sm" tone="slate" data-testid={`secretref-kind-${row.name}`}>{row.sourceKind}</Pill> },
    { id: "updated", groupable: false, header: t("settings.environments.col.updated"), width: 150, render: (row) => <span className="text-xs opacity-70"><RelativeTime iso={row.updatedAt} /></span> },
  ];

  return (
    <div className="flex min-h-[22rem] flex-1 flex-col gap-3">
      <div className="flex shrink-0 flex-wrap items-center gap-3">
        <div className="w-56 shrink-0" data-testid="environments-secret-filter-label">
          <Select variant="ghost" size="md" aria-label={t("settings.environments.secret.sourceKind")} value={sourceKind} onChange={(event) => setSourceKind(event.target.value)} data-testid="environments-secret-filter">
            <option value="">{t("settings.environments.secret.sourceKind")}: {t("settings.environments.filter.any")}</option>
            {sourceKinds(vocabulary).map((kind) => <option key={kind} value={kind}>{kind}</option>)}
          </Select>
        </div>
      </div>

      {list.error && (
        <Alert className="shrink-0" intent="danger" title={t("settings.environments.secret.listFailed")} description={list.error} data-testid="environments-secret-failed">
          <Button size="sm" onClick={list.refetch}>{t("settings.environments.actions.retry")}</Button>
        </Alert>
      )}

      <div className="min-h-[18rem] flex-1" data-testid="environments-secret-list">
        <Table<SecretRefRow>
          variant="outlined"
          data={rows}
          columns={columns}
          rowKey={(row) => row.id}
          corner="rounded-sm"
          loading={list.loading}
          onRowClick={(row) => setOpenName(row.name)}
          emptyState={list.error ? <span /> : <div data-testid="environments-secret-empty"><EmptyState title={t("settings.environments.secret.emptyTitle")} subtitle={t("settings.environments.secret.emptyBody")} /></div>}
          fullHeight
        />
      </div>

      {opened && (
        <OverlaySidePanel title={opened.name} titleClassName="text-xl font-semibold" variant="simple" width={520} onClose={() => setOpenName("")}>
          <div className="space-y-8" data-testid={`secretref-detail-${opened.name}`}>
            <div className="flex flex-wrap gap-2">
              <Button size="sm" onClick={() => void check(opened)} data-testid={`secretref-check-${opened.name}`}>{t("settings.environments.secret.check")}</Button>
              {canWrite && (
                <Button size="sm" variant="ghost" onClick={() => void action.run(() => writeSecretRefArchive(opened.name), { errorTitle: t("settings.environments.secret.archiveFailed") }).then(() => list.refetch())} data-testid={`secretref-archive-${opened.name}`}>{t("settings.environments.actions.archive")}</Button>
              )}
              {canWrite && (
                <Button size="sm" variant="ghost" onClick={() => void action.run(() => writeSecretRefRestore(opened.name), { errorTitle: t("settings.environments.secret.restoreFailed") }).then(() => list.refetch())} data-testid={`secretref-restore-${opened.name}`}>{t("settings.environments.actions.restore")}</Button>
              )}
            </div>
            <SecretRefDetail name={opened.name} row={opened} controller={controller} live={live} />
            {checks[opened.name] && (
              <section className="space-y-3" data-testid={`secretref-check-result-${opened.name}`}>
                <h3 className={H}>{t("settings.environments.q.resolves")}</h3>
                <p className="text-sm" data-testid={`secretref-check-resolvable-${opened.name}`}>{t("settings.environments.secret.resolvable")}: {String(checks[opened.name]?.resolvable)}</p>
                <p className="text-sm" data-testid={`secretref-check-present-${opened.name}`}>{t("settings.environments.secret.sourcePresent")}: {String(checks[opened.name]?.sourcePresent)}</p>
                {hasRefusal(checks[opened.name]) && <p className="whitespace-pre-line text-xs italic" data-testid={`secretref-check-refusal-${opened.name}`}>{checks[opened.name]?.refusalSentence}</p>}
              </section>
            )}
          </div>
        </OverlaySidePanel>
      )}

      {creating && (
        <OverlaySidePanel title={t("settings.environments.secret.createTitle")} titleClassName="text-xl font-semibold" variant="simple" width={520} onClose={() => setCreating(false)}>
          <SecretRefCreateForm
            vocabulary={vocabulary}
            canWrite={canWrite}
            busy={action.busy}
            onCreated={() => {
              setCreating(false);
              list.refetch();
            }}
            onClose={() => setCreating(false)}
          />
        </OverlaySidePanel>
      )}
    </div>
  );
}

/** The authoritative record of one reference, read when the row is opened. */
function SecretRefDetail({ name, row, controller, live }: { name: string; row: SecretRefRow; controller: LiveController; live: LiveState }) {
  const { t } = useI18n();
  const show = useSecretRefShow(name, controller, live);
  const usedBy = listOf(show.data?.referencedBy);
  return (
    <div className="space-y-8" data-testid={`secretref-show-${name}`}>
      {show.error && <p className="text-xs opacity-70">{show.error}</p>}
      <section className="space-y-3">
        <h3 className={H}>{t("settings.environments.q.lives")}</h3>
        <dl className="space-y-1 text-sm">
          <div><dt className="inline opacity-70">{t("settings.environments.secret.sourceKind")}: </dt><dd className="inline">{row.sourceKind}</dd></div>
          <div><dt className="inline opacity-70">{t("settings.environments.secret.location")}: </dt><dd className="inline break-all font-mono text-xs">{row.source}</dd></div>
          <div><dt className="inline opacity-70">{t("settings.environments.secret.injectAs")}: </dt><dd className="inline">{row.injectAs}</dd></div>
          <div><dt className="inline opacity-70">{t("settings.environments.secret.description")}: </dt><dd className="inline">{show.data?.description ?? ""}</dd></div>
          <div><dt className="inline opacity-70">{t("settings.environments.secret.origin")}: </dt><dd className="inline">{show.data?.origin ?? ""}</dd></div>
        </dl>
      </section>
      <section className="space-y-3">
        <h3 className={H}>{t("settings.environments.q.uses")}</h3>
        <p className="text-sm" data-testid={`secretref-usedby-${name}`}>
          {t("settings.environments.secret.usedBy")}
          {usedBy.length === 0 ? ` ${t("settings.environments.secret.usedByNone")}` : ` ${usedBy.join(", ")}`}
        </p>
      </section>
    </div>
  );
}

/**
 * The create form: a NAME, a KIND and a LOCATION. No field on this form holds a value, and the door
 * refuses one that tries (`secretref.create`'s params have no key a credential could arrive under,
 * checked before anything else). A refusal is the toast; every field is left as typed.
 */
function SecretRefCreateForm({ vocabulary, canWrite, busy, onCreated, onClose }: {
  vocabulary: Vocabulary | undefined;
  canWrite: boolean;
  busy: boolean;
  onCreated: () => void;
  onClose: () => void;
}) {
  const { t } = useI18n();
  const action = useRpcAction();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [kind, setKind] = useState(sourceKinds(vocabulary)[0] ?? "");
  const [location, setLocation] = useState("");
  const [injectAs, setInjectAs] = useState("");
  const off = !canWrite || busy || action.busy;

  const create = async () => {
    const result = await action.run(
      () => writeSecretRefCreate({ name, description, sourceKind: kind, source: location, ...(injectAs === "" ? {} : { injectAs }) }),
      { errorTitle: t("settings.environments.secret.createFailed") },
    );
    if (result) onCreated();
  };

  return (
    <div className="space-y-8" data-testid="secretref-create-form">
      <p className="text-xs italic opacity-80" data-testid="secretref-create-note">{t("settings.environments.secret.noValueNote")}</p>

      <section className="space-y-3">
        <h3 className={H}>{t("settings.environments.q.named")}</h3>
        <div data-testid="secretref-form-name"><FormField label={t("settings.environments.secret.name")}><Input variant="ghost" aria-label={t("settings.environments.secret.name")} value={name} disabled={off} onChange={(event) => setName(event.target.value)} /></FormField></div>
        <div data-testid="secretref-form-description"><FormField label={t("settings.environments.secret.descriptionLabel")}><Input variant="ghost" aria-label={t("settings.environments.secret.descriptionLabel")} value={description} disabled={off} onChange={(event) => setDescription(event.target.value)} /></FormField></div>
        <div data-testid="secretref-form-injectas"><FormField label={t("settings.environments.secret.injectAs")}><Input variant="ghost" aria-label={t("settings.environments.secret.injectAs")} value={injectAs} disabled={off} onChange={(event) => setInjectAs(event.target.value)} /></FormField></div>
      </section>

      <section className="space-y-3">
        <h3 className={H}>{t("settings.environments.q.lives")}</h3>
        <div data-testid="secretref-form-kind">
          <FormField label={t("settings.environments.secret.sourceKind")}>
            <Select variant="ghost" aria-label={t("settings.environments.secret.sourceKind")} value={kind} disabled={off} onChange={(event) => setKind(event.target.value)}>
              {sourceKinds(vocabulary).map((entry) => <option key={entry} value={entry}>{entry}</option>)}
            </Select>
          </FormField>
        </div>
        <div data-testid="secretref-form-location"><FormField label={t("settings.environments.secret.location")}><Input variant="ghost" aria-label={t("settings.environments.secret.location")} value={location} disabled={off} onChange={(event) => setLocation(event.target.value)} /></FormField></div>
      </section>

      <div className="flex gap-2">
        <Button size="sm" disabled={off} onClick={() => void create()} data-testid="secretref-form-save">{t("settings.environments.secret.createSave")}</Button>
        <Button size="sm" variant="ghost" onClick={onClose} data-testid="secretref-form-cancel">{t("settings.environments.actions.cancel")}</Button>
      </div>
    </div>
  );
}
