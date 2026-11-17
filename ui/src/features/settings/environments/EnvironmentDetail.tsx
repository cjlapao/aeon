import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, Pill } from "../../../components/kit";
import { RelativeTime } from "../../../components/RelativeTime";
import type { LiveController, LiveState } from "../../../lib/live";
import { useEnvironmentShow, writeEnvironmentUnbind } from "./environmentsRpc";
import {
  bindingsOf, capabilityChips, egressOf, homeOf, honestyLine, imageOf, limitsOf, passthroughOf, secretsOf, setPairsOf, servicesOf, specDiff,
  type ChangeRow, type DisplayRow, type Vocabulary,
} from "./environmentsModel";

/**
 * One environment, as the content of a side panel: the actions first (edit, bind, archive or
 * restore), then sections as questions. Shown are the spec's NAMES — passthrough names, `set`
 * names, secret references by name — the limits, egress, image and services as the daemon
 * recorded them, the revisions with a diff over the audit rows' `specAfter`, and the honesty line
 * verbatim. Nothing here renders a secret value: a reference appears as its NAME, and the `set`
 * contents are the plain variables the person chose (the door refuses a credential-shaped name).
 * The revisions come from the audit rows the tab already read.
 */
export interface EnvironmentDetailProps {
  row: DisplayRow;
  vocabulary: Vocabulary | undefined;
  changes: readonly ChangeRow[];
  canWrite: boolean;
  controller: LiveController;
  live: LiveState;
  onEdit: (row: DisplayRow) => void;
  onBind: (row: DisplayRow) => void;
  onArchive: (row: DisplayRow) => void;
  onRestore: (row: DisplayRow) => void;
  /** A daemon word for the refusal, shown as a toast by the caller. */
  onRefusal: (message: string) => void;
  /** Called with the daemon's answer so the row updates from the RESPONSE. */
  onUnbound: (answer: { unbound: boolean }) => void;
}

const H = "text-xs font-semibold uppercase tracking-widest opacity-70";

export function EnvironmentDetail({ row, vocabulary, changes, canWrite, controller, live, onEdit, onBind, onArchive, onRestore, onRefusal, onUnbound }: EnvironmentDetailProps) {
  const { t } = useI18n();
  const show = useEnvironmentShow(row.slug, controller, live);
  const spec = show.data?.spec ?? row.spec;
  const bindings = show.data?.bindings ?? bindingsOf(row);
  const secretNames = show.data?.secretRefNames ?? secretsOf(spec);
  const revisions = changes.filter((change) => change.subjectId === row.id).slice().sort((a, b) => b.revision - a.revision);
  const none = t("settings.environments.detail.none");

  const unbind = async (componentId: string) => {
    try {
      onUnbound(await writeEnvironmentUnbind(componentId));
    } catch (cause) {
      onRefusal(cause instanceof Error ? cause.message : String(cause));
    }
  };

  return (
    <div className="space-y-8" data-testid={`environment-detail-${row.id}`}>
      {show.error && <Alert intent="danger" title={t("settings.environments.detail.failed")} description={show.error} data-testid={`environment-detail-failed-${row.id}`} />}

      <div className="flex flex-wrap gap-2">
        {canWrite && <Button size="sm" onClick={() => onEdit(row)} data-testid={`environment-edit-${row.id}`}>{t("settings.environments.actions.edit")}</Button>}
        {canWrite && <Button size="sm" variant="ghost" onClick={() => onBind(row)} data-testid={`environment-bind-${row.id}`}>{t("settings.environments.actions.bind")}</Button>}
        {canWrite && row.archivedAt === undefined && <Button size="sm" variant="ghost" onClick={() => onArchive(row)} data-testid={`environment-archive-${row.id}`}>{t("settings.environments.actions.archive")}</Button>}
        {canWrite && row.archivedAt !== undefined && <Button size="sm" variant="ghost" onClick={() => onRestore(row)} data-testid={`environment-restore-${row.id}`}>{t("settings.environments.actions.restore")}</Button>}
      </div>

      <section className="space-y-3">
        <h3 className={H}>{t("settings.environments.q.runs")}</h3>
        <Names testid={`environment-passthrough-${row.id}`} label={t("settings.environments.detail.passthrough")} names={passthroughOf(spec)} empty={none} />
        <div data-testid={`environment-set-${row.id}`}>
          <p className="text-xs opacity-70">{t("settings.environments.detail.set")}</p>
          {setPairsOf(spec).length === 0 ? <p className="text-xs opacity-60">{none}</p> : (
            <ul className="space-y-0.5 text-xs">
              {setPairsOf(spec).map((pair) => <li key={pair.name} className="font-mono" data-testid={`environment-set-name-${row.id}-${pair.name}`}>{pair.name}={pair.content}</li>)}
            </ul>
          )}
        </div>
        <Names testid={`environment-secrets-${row.id}`} label={t("settings.environments.detail.secrets")} names={secretNames} empty={none} />
        <dl className="space-y-1 text-sm">
          <Fact label={t("settings.environments.detail.home")} shown={homeOf(spec)} testid={`environment-home-${row.id}`} />
          <Fact label={t("settings.environments.detail.egress")} shown={`${egressOf(spec).mode}${egressOf(spec).allow.length > 0 ? ` · ${egressOf(spec).allow.join(", ")}` : ""}`} testid={`environment-egress-${row.id}`} />
          <Fact label={t("settings.environments.detail.limits")} shown={limitsOf(spec).map((limit) => `${limit.name}=${limit.shown}`).join(" · ")} testid={`environment-limits-${row.id}`} />
          <Fact label={t("settings.environments.detail.image")} shown={imageOf(spec) ? `${imageOf(spec)?.ref} · ${imageOf(spec)?.pull}` : ""} testid={`environment-image-${row.id}`} />
        </dl>
        <div data-testid={`environment-services-${row.id}`}>
          <p className="text-xs opacity-70">{t("settings.environments.detail.services")}</p>
          {servicesOf(spec).length === 0 ? <p className="text-xs opacity-60">{none}</p> : (
            <ul className="flex flex-wrap gap-1">
              {servicesOf(spec).map((service) => (
                <li key={`${service.name}-${service.port ?? ""}`}><Pill size="sm" tone="slate">{service.name} · {service.image}{service.port === undefined ? "" : `:${service.port}`}</Pill></li>
              ))}
            </ul>
          )}
        </div>
      </section>

      <section className="space-y-3" data-testid={`environment-bindings-${row.id}`}>
        <h3 className={H}>{t("settings.environments.q.where")}</h3>
        {bindings.length === 0 ? <p className="text-sm opacity-60">{t("settings.environments.detail.unbound")}</p> : (
          <ul className="space-y-1 text-sm">
            {bindings.map((binding) => (
              <li key={`${binding.scopeKind}-${binding.componentId ?? ""}`} className="flex flex-wrap items-center gap-2" data-testid={`environment-detail-binding-${row.id}`}>
                <span>{binding.scopeKind}{binding.componentId ? ` · ${binding.componentId}` : ""}</span>
                <span className="text-xs opacity-60"><RelativeTime iso={binding.boundAt} /></span>
                {canWrite && binding.componentId && <Button size="xs" variant="ghost" onClick={() => void unbind(binding.componentId ?? "")} data-testid={`environment-unbind-${row.id}`}>{t("settings.environments.actions.unbind")}</Button>}
              </li>
            ))}
          </ul>
        )}
      </section>

      <section className="space-y-3">
        <h3 className={H}>{t("settings.environments.q.enforces")}</h3>
        <div className="flex flex-wrap gap-1" data-testid={`environment-chips-${row.id}`}>
          {capabilityChips(vocabulary, row.backend).map((chip) => (
            <span key={chip.capability} className="rounded-full border border-[var(--border)] px-2 py-0.5 text-[11px]" data-testid={`environment-chip-${row.id}-${chip.capability}`} data-enforced={chip.enforced ? "true" : "false"}>
              {chip.enforced ? "✓" : "✗"} {chip.capability}
            </span>
          ))}
        </div>
        {/* EV1: the honesty line travels from the vocabulary, one copy, verbatim. */}
        <p className="text-xs italic opacity-80" data-testid={`environment-honesty-${row.id}`}>{honestyLine(vocabulary)}</p>
      </section>

      <section className="space-y-3" data-testid={`environment-revisions-${row.id}`}>
        <h3 className={H}>{t("settings.environments.q.changed")}</h3>
        {revisions.length === 0 ? <p className="text-xs opacity-60" data-testid={`environment-revisions-empty-${row.id}`}>{t("settings.environments.detail.noRevisions")}</p> : (
          <ul className="divide-y divide-[var(--border)] text-xs">
            {revisions.map((change, index) => {
              const diff = specDiff(revisions[index + 1]?.specAfter, change.specAfter);
              return (
                <li key={change.id} className="space-y-0.5 py-2" data-testid={`environment-revision-${row.id}-${change.revision}`}>
                  <p className="font-medium">r{change.revision} · {change.actorKind} {change.actorId} · <RelativeTime iso={change.at} /></p>
                  <p className="opacity-80" data-testid={`environment-revision-summary-${row.id}-${change.revision}`}>{change.action} — {change.summary}</p>
                  <p data-testid={`environment-revision-diff-${row.id}-${change.revision}`}>
                    {diff.added.map((name) => <span key={`+${name}`} className="text-emerald-600 dark:text-emerald-300">+{name} </span>)}
                    {diff.removed.map((name) => <span key={`-${name}`} className="text-rose-600 dark:text-rose-300">−{name} </span>)}
                    {diff.changed.map((name) => <span key={`~${name}`} className="text-amber-600 dark:text-amber-300">~{name} </span>)}
                    {diff.added.length + diff.removed.length + diff.changed.length === 0 ? <span className="opacity-60">{t("settings.environments.detail.noDiff")}</span> : null}
                  </p>
                </li>
              );
            })}
          </ul>
        )}
      </section>
    </div>
  );
}

function Names({ label, names, empty, testid }: { label: string; names: readonly string[]; empty: string; testid: string }) {
  return (
    <div data-testid={testid}>
      <p className="text-xs opacity-70">{label}</p>
      {names.length === 0 ? <p className="text-xs opacity-60">{empty}</p> : (
        <ul className="flex flex-wrap gap-1">{names.map((name) => <li key={name}><Pill size="sm" tone="slate">{name}</Pill></li>)}</ul>
      )}
    </div>
  );
}

function Fact({ label, shown, testid }: { label: string; shown: string; testid: string }) {
  return <div data-testid={testid}><dt className="inline opacity-70">{label}: </dt><dd className="inline">{shown === "" ? "—" : shown}</dd></div>;
}
